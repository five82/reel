package encode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"codeberg.org/five82/reel/internal/chunk"
	"codeberg.org/five82/reel/internal/quality"
	"codeberg.org/five82/reel/internal/video"
	"codeberg.org/five82/reel/internal/worker"
)

const (
	// DefaultTargetQualitySampleFrames is the per-window probe length for target-quality search.
	DefaultTargetQualitySampleFrames = 48

	// DefaultTargetQualityFullProbeFrames is the largest chunk size probed as a whole.
	DefaultTargetQualityFullProbeFrames = 256

	// DefaultTargetQualityFullFirstFrames is the largest sampled chunk to fully encode for a reliable first probe.
	DefaultTargetQualityFullFirstFrames = 720

	// targetQualityScheduleBlockChunks keeps target-quality work near timeline order while balancing chunk sizes.
	targetQualityScheduleBlockChunks = 32

	// targetQualityUpperGraceJOD avoids extra probes for tiny over-target CVVDP overshoots.
	targetQualityUpperGraceJOD = 0.02
)

type TargetQualityConfig struct {
	Target          float32
	Tolerance       float32
	CRFMin          float32
	CRFMax          float32
	MaxProbes       int
	MetricWorkers   int
	DisplayPath     string
	InitialCRF      float32
	SampleFrames    int
	FullProbeFrames int
	Verbose         func(string)
}

type targetQualityResult struct {
	worker.EncodeResult
	Log chunkTargetLog
}

type chunkTargetLog struct {
	ChunkIdx           int                `json:"chunk_idx"`
	Frames             int                `json:"frames"`
	Target             float32            `json:"target"`
	Tolerance          float32            `json:"tolerance"`
	CRFMin             float32            `json:"crf_min"`
	CRFMax             float32            `json:"crf_max"`
	ProbeSampleFrames  int                `json:"probe_sample_frames"`
	ProbeFullThreshold int                `json:"probe_full_threshold"`
	InitialCRF         float32            `json:"initial_crf"`
	InitialCRFSource   string             `json:"initial_crf_source"`
	Probes             []quality.Probe    `json:"probes"`
	FinalCRF           float32            `json:"final_crf"`
	FinalScore         float32            `json:"final_sample_score"`
	FinalSize          uint64             `json:"final_size"`
	FinalEncodeSeconds float64            `json:"final_encode_seconds,omitempty"`
	StopReason         quality.StopReason `json:"stop_reason"`
	StartedAt          time.Time          `json:"started_at"`
	CompletedAt        time.Time          `json:"completed_at"`
}

func EncodeTargetQuality(
	ctx context.Context,
	chunks []chunk.Chunk,
	inputPath string,
	inf *video.Info,
	cfg *EncodeConfig,
	workDir string,
	cropRect *video.CropRect,
	progressCb ProgressCallback,
	tq TargetQualityConfig,
) (int, error) {
	if tq.MetricWorkers < 1 {
		tq.MetricWorkers = 1
	}
	if tq.MaxProbes < 1 {
		tq.MaxProbes = 1
	}
	if tq.SampleFrames < 1 {
		tq.SampleFrames = DefaultTargetQualitySampleFrames
	}
	if tq.FullProbeFrames < 1 {
		tq.FullProbeFrames = DefaultTargetQualityFullProbeFrames
	}
	if tq.InitialCRF <= 0 {
		tq.InitialCRF = (tq.CRFMin + tq.CRFMax) / 2
	}
	if err := chunk.EnsureEncodeDir(workDir); err != nil {
		return 0, fmt.Errorf("failed to create encode directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, "probes"), 0755); err != nil {
		return 0, fmt.Errorf("failed to create probe directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, "tq"), 0755); err != nil {
		return 0, fmt.Errorf("failed to create target-quality log directory: %w", err)
	}

	resume, err := chunk.GetResume(workDir)
	if err != nil {
		return 0, fmt.Errorf("failed to load resume info: %w", err)
	}
	resume = resume.Validate(workDir, chunks)
	doneSet := resume.DoneSet()

	remainingChunks := make([]chunk.Chunk, 0, len(chunks))
	totalFrames := 0
	for _, ch := range chunks {
		totalFrames += ch.Frames()
		if !doneSet[ch.Idx] {
			remainingChunks = append(remainingChunks, ch)
		}
	}
	if len(remainingChunks) == 0 {
		return MaxAdaptiveWorkers(), nil
	}
	remainingChunks = orderTargetQualityChunks(remainingChunks, targetQualityScheduleBlockChunks)

	if cropRect != nil {
		if err := video.ValidateCropRect(inf, *cropRect); err != nil {
			return 0, fmt.Errorf("invalid crop rectangle: %w", err)
		}
	}
	width, height := video.OutputDimensions(inf, cropRect)

	maxWorkers := MaxAdaptiveWorkers()
	initialWorkers := initialAdaptiveWorkers(maxWorkers, width, height)
	limiter := newAdaptiveLimiter(maxWorkers, initialWorkers, totalFrames, cfg.StatusCallback)
	if cfg.LevelOfParallelism == 0 {
		cfg.LevelOfParallelism = levelOfParallelismForWorkers(maxWorkers)
	}

	metricPool := make(chan *quality.VshipProcessor, tq.MetricWorkers)
	for i := 0; i < tq.MetricWorkers; i++ {
		processor, err := quality.NewVshipProcessor(width, height, inf, tq.DisplayPath)
		if err != nil {
			for j := 0; j < i; j++ {
				_ = (<-metricPool).Close()
			}
			return 0, err
		}
		metricPool <- processor
	}
	defer func() {
		for i := 0; i < tq.MetricWorkers; i++ {
			_ = (<-metricPool).Close()
		}
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	chunkChan := make(chan chunk.Chunk, maxWorkers)
	resultChan := make(chan targetQualityResult, len(remainingChunks))

	var progressMu sync.Mutex
	progress := worker.Progress{
		ChunksTotal:    len(chunks),
		ChunksComplete: len(chunks) - len(remainingChunks),
		FramesTotal:    totalFrames,
		FramesComplete: resume.TotalEncodedFrames(),
		BytesComplete:  resume.TotalEncodedSize(),
	}
	limiter.observeProgress(progress.FramesComplete)
	snapshotProgress := func() worker.Progress {
		p := progress
		p.ActiveWorkers, p.TargetWorkers, p.MaxWorkers = limiter.stats()
		return p
	}

	var encodeErr atomic.Pointer[error]
	setError := func(err error) { encodeErr.CompareAndSwap(nil, &err) }
	getError := func() error {
		if p := encodeErr.Load(); p != nil {
			return *p
		}
		return nil
	}

	searchCtx := quality.SearchContext{
		Target:              tq.Target,
		Tolerance:           tq.Tolerance,
		UpperToleranceGrace: targetQualityUpperGraceJOD,
		CRFMin:              tq.CRFMin,
		CRFMax:              tq.CRFMax,
		MaxProbes:           tq.MaxProbes,
		InitialCRF:          tq.InitialCRF,
		JODPerCRF:           targetQualityDefaultJODPerCRF,
	}
	prior := newTargetQualityPrior(tq.InitialCRF, tq.CRFMin, tq.CRFMax)
	seedTargetQualityPrior(workDir, doneSet, prior)

	var workerWg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for ch := range chunkChan {
				select {
				case <-ctx.Done():
					limiter.release()
					resultChan <- targetQualityResult{EncodeResult: worker.EncodeResult{ChunkIdx: ch.Idx, Error: ctx.Err()}}
					continue
				default:
				}
				if getError() != nil {
					limiter.release()
					continue
				}
				initialCRF, initialCRFSource := prior.InitialCRF(ch.Idx)
				chunkSearchCtx := searchCtx
				chunkSearchCtx.InitialCRF = initialCRF
				chunkSearchCtx.JODPerCRF = prior.JODPerCRF()
				result := encodeTargetQualityChunk(ctx, inputPath, inf, cfg, workDir, cropRect, width, height, ch, chunkSearchCtx, metricPool, prior, tq.SampleFrames, tq.FullProbeFrames, initialCRFSource, tq.Verbose)
				limiter.release()
				resultChan <- result
			}
		}()
	}

	var logsMu sync.Mutex
	logs := make([]chunkTargetLog, 0, len(remainingChunks))
	var collectorWg sync.WaitGroup
	collectorWg.Add(1)
	go func() {
		defer collectorWg.Done()
		for result := range resultChan {
			if result.Error != nil {
				setError(result.Error)
				continue
			}
			progressMu.Lock()
			progress.ChunksComplete++
			progress.FramesComplete += result.Frames
			progress.BytesComplete += result.Size
			p := snapshotProgress()
			limiter.observeProgress(p.FramesComplete)
			progressMu.Unlock()

			_ = chunk.AppendDone(chunk.ChunkComp{Idx: result.ChunkIdx, Frames: result.Frames, Size: result.Size}, workDir)
			logsMu.Lock()
			logs = append(logs, result.Log)
			logsMu.Unlock()
			if progressCb != nil {
				progressCb(p)
			}
		}
	}()

	go limiter.monitor(ctx, cancel, setError)
	go func() {
		<-ctx.Done()
		limiter.wake()
	}()

	go func() {
		defer close(chunkChan)
		for _, ch := range remainingChunks {
			if getError() != nil {
				return
			}
			if _, err := limiter.acquire(ctx); err != nil {
				return
			}
			select {
			case chunkChan <- ch:
			case <-ctx.Done():
				limiter.release()
				return
			}
		}
	}()

	workerWg.Wait()
	close(resultChan)
	collectorWg.Wait()
	writeAggregateTargetLog(workDir, logs, tq)
	logTargetAggregate(logs, tq.Verbose)
	return maxWorkers, getError()
}

func orderTargetQualityChunks(chunks []chunk.Chunk, blockSize int) []chunk.Chunk {
	ordered := append([]chunk.Chunk(nil), chunks...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Idx < ordered[j].Idx
	})
	if blockSize <= 0 {
		blockSize = len(ordered)
	}
	for start := 0; start < len(ordered); start += blockSize {
		end := min(start+blockSize, len(ordered))
		sort.SliceStable(ordered[start:end], func(i, j int) bool {
			return ordered[start+i].Frames() > ordered[start+j].Frames()
		})
	}
	return ordered
}

func encodeTargetQualityChunk(
	ctx context.Context,
	inputPath string,
	inf *video.Info,
	cfg *EncodeConfig,
	workDir string,
	cropRect *video.CropRect,
	width, height uint32,
	ch chunk.Chunk,
	searchCtx quality.SearchContext,
	metricPool chan *quality.VshipProcessor,
	prior *targetQualityPrior,
	sampleFrames int,
	fullProbeFrames int,
	initialCRFSource string,
	verbose func(string),
) targetQualityResult {
	log := chunkTargetLog{
		ChunkIdx:           ch.Idx,
		Frames:             ch.Frames(),
		Target:             searchCtx.Target,
		Tolerance:          searchCtx.Tolerance,
		CRFMin:             searchCtx.CRFMin,
		CRFMax:             searchCtx.CRFMax,
		ProbeSampleFrames:  sampleFrames,
		ProbeFullThreshold: fullProbeFrames,
		InitialCRF:         searchCtx.InitialCRF,
		InitialCRFSource:   initialCRFSource,
		StartedAt:          time.Now(),
	}
	state := quality.NewSearchState(searchCtx)
	fullProbePaths := make(map[string]string)

	for {
		crf, ok := state.NextCRF(searchCtx)
		if !ok {
			break
		}
		fullFirst := targetQualityFullFirstProbe(initialCRFSource, state.Round, ch.Frames(), sampleFrames, fullProbeFrames)
		probe, fullProbePath, err := encodeSampledProbe(ctx, inputPath, inf, cfg, workDir, cropRect, width, height, ch, crf, sampleFrames, fullProbeFrames, fullFirst, metricPool)
		if err != nil {
			return targetQualityResult{EncodeResult: worker.EncodeResult{ChunkIdx: ch.Idx, Error: err}, Log: log}
		}
		if fullProbePath != "" {
			fullProbePaths[quality.FormatCRF(probe.CRF)] = fullProbePath
		}
		state.AddProbe(searchCtx, probe)
		log.Probes = append(log.Probes, probe)
		if verbose != nil {
			fps := float64(probe.SampleFrames) / probe.MetricSeconds
			initial := ""
			if state.Round == 1 {
				initial = fmt.Sprintf(" initial_source=%s", initialCRFSource)
			}
			probeKind := ""
			if fullFirst {
				probeKind = " probe=full_first"
			}
			verbose(fmt.Sprintf("TQ sample chunk=%04d round=%d crf=%s%s%s sampled_cvvdp=%.4f mean_cvvdp=%.4f worst_window=%.4f window_spread=%.4f windows=%s delta=%+.4f size=%d sample_frames=%d encode=%.1fs metric=%.1fs metric_fps=%.1f", ch.Idx, state.Round, quality.FormatCRF(crf), initial, probeKind, probe.Score, probe.MeanScore, probe.WorstWindowScore, targetQualityWindowSpread(probe.Windows), formatProbeWindowScores(probe.Windows), probe.Score-searchCtx.Target, probe.Size, probe.SampleFrames, probe.EncodeSeconds, probe.MetricSeconds, fps))
		}
		if state.StopReason != quality.StopNone {
			break
		}
	}

	best, ok := state.BestProbe(searchCtx)
	if !ok {
		return targetQualityResult{EncodeResult: worker.EncodeResult{ChunkIdx: ch.Idx, Error: fmt.Errorf("no target-quality probes completed for chunk %04d", ch.Idx)}, Log: log}
	}
	prior.AddResult(ch.Idx, best.CRF, log.Probes)
	finalPath := chunk.IVFPath(workDir, ch.Idx)
	if bestPath := fullProbePaths[quality.FormatCRF(best.CRF)]; bestPath != "" {
		if err := copyFile(bestPath, finalPath); err != nil {
			return targetQualityResult{EncodeResult: worker.EncodeResult{ChunkIdx: ch.Idx, Error: err}, Log: log}
		}
	} else {
		encodeStart := time.Now()
		finalResult := encodeProbe(ctx, inputPath, inf, cfg, cropRect, width, height, ch, finalPath, best.CRF)
		if finalResult.Error != nil {
			return targetQualityResult{EncodeResult: finalResult, Log: log}
		}
		log.FinalEncodeSeconds = time.Since(encodeStart).Seconds()
	}
	stat, err := os.Stat(finalPath)
	if err != nil {
		return targetQualityResult{EncodeResult: worker.EncodeResult{ChunkIdx: ch.Idx, Error: err}, Log: log}
	}
	log.FinalCRF = best.CRF
	log.FinalScore = best.Score
	log.FinalSize = uint64(stat.Size())
	log.StopReason = state.StopReason
	log.CompletedAt = time.Now()
	_ = writeChunkTargetLog(workDir, log)
	if verbose != nil {
		verbose(fmt.Sprintf("TQ final chunk=%04d crf=%s sampled_cvvdp=%.4f size=%d probes=%d stop=%s", ch.Idx, quality.FormatCRF(best.CRF), best.Score, log.FinalSize, len(log.Probes), log.StopReason))
	}
	return targetQualityResult{EncodeResult: worker.EncodeResult{ChunkIdx: ch.Idx, Frames: ch.Frames(), Size: uint64(stat.Size())}, Log: log}
}

type probeSampleWindow struct {
	Offset int
	Frames int
}

func sampledProbeWindows(chunkFrames, sampleFrames, fullProbeFrames int) []probeSampleWindow {
	if chunkFrames <= 0 {
		return nil
	}
	if sampleFrames <= 0 {
		return []probeSampleWindow{{Frames: chunkFrames}}
	}
	fullThreshold := max(fullProbeFrames, 3*sampleFrames)
	if chunkFrames <= fullThreshold {
		return []probeSampleWindow{{Frames: chunkFrames}}
	}

	middle := (chunkFrames - sampleFrames) / 2
	return []probeSampleWindow{
		{Offset: 0, Frames: sampleFrames},
		{Offset: middle, Frames: sampleFrames},
		{Offset: chunkFrames - sampleFrames, Frames: sampleFrames},
	}
}

func targetQualityFullFirstProbe(initialCRFSource string, round int, chunkFrames int, sampleFrames int, fullProbeFrames int) bool {
	if round != 1 || chunkFrames > DefaultTargetQualityFullFirstFrames {
		return false
	}
	if len(sampledProbeWindows(chunkFrames, sampleFrames, fullProbeFrames)) <= 1 {
		return false
	}
	switch initialCRFSource {
	case "neighbor", "median":
		return true
	default:
		return false
	}
}

func encodeSampledProbe(
	ctx context.Context,
	inputPath string,
	inf *video.Info,
	cfg *EncodeConfig,
	workDir string,
	cropRect *video.CropRect,
	width, height uint32,
	ch chunk.Chunk,
	crf float32,
	sampleFrames int,
	fullProbeFrames int,
	fullFirst bool,
	metricPool chan *quality.VshipProcessor,
) (quality.Probe, string, error) {
	windows := sampledProbeWindows(ch.Frames(), sampleFrames, fullProbeFrames)
	if len(windows) == 0 {
		return quality.Probe{}, "", fmt.Errorf("chunk %04d has no frames", ch.Idx)
	}
	if fullFirst && len(windows) > 1 {
		return encodeFullFirstSampledProbe(ctx, inputPath, inf, cfg, workDir, cropRect, width, height, ch, crf, windows, metricPool)
	}

	var totalSize uint64
	var encodeSeconds, metricSeconds float64
	var windowsScored []quality.ProbeWindow
	var fullProbePath string

	for sampleIdx, sample := range windows {
		sampleChunk := chunk.Chunk{Idx: ch.Idx, Start: ch.Start + sample.Offset, End: ch.Start + sample.Offset + sample.Frames}
		isFullChunk := len(windows) == 1 && sample.Offset == 0 && sample.Frames == ch.Frames()
		probePath := sampledProbePath(workDir, ch.Idx, crf, sampleIdx, sample, isFullChunk)

		encodeStart := time.Now()
		probeResult := encodeProbe(ctx, inputPath, inf, cfg, cropRect, width, height, sampleChunk, probePath, crf)
		if probeResult.Error != nil {
			return quality.Probe{}, "", probeResult.Error
		}
		encodeSeconds += time.Since(encodeStart).Seconds()
		totalSize += probeResult.Size

		metricResult, err := scoreProbeWindow(ctx, metricPool, inputPath, probePath, inf, sampleChunk, cropRect, width, height, 0)
		if err != nil {
			return quality.Probe{}, "", err
		}
		metricSeconds += metricResult.MetricSeconds
		windowsScored = append(windowsScored, quality.ProbeWindow{
			Offset: sample.Offset,
			Frames: metricResult.Frames,
			Score:  metricResult.Score,
		})

		if isFullChunk {
			fullProbePath = probePath
		} else {
			_ = os.Remove(probePath)
		}
	}
	score, meanScore, worstScore, scoredFrames := targetQualitySampleScore(windowsScored)
	if scoredFrames == 0 {
		return quality.Probe{}, "", fmt.Errorf("no target-quality sample frames scored for chunk %04d", ch.Idx)
	}

	return quality.Probe{
		CRF:              crf,
		Score:            score,
		MeanScore:        meanScore,
		WorstWindowScore: worstScore,
		Size:             totalSize,
		EncodeSeconds:    encodeSeconds,
		MetricSeconds:    metricSeconds,
		SampleFrames:     scoredFrames,
		Windows:          windowsScored,
	}, fullProbePath, nil
}

func encodeFullFirstSampledProbe(
	ctx context.Context,
	inputPath string,
	inf *video.Info,
	cfg *EncodeConfig,
	workDir string,
	cropRect *video.CropRect,
	width, height uint32,
	ch chunk.Chunk,
	crf float32,
	windows []probeSampleWindow,
	metricPool chan *quality.VshipProcessor,
) (quality.Probe, string, error) {
	fullProbePath := sampledProbePath(workDir, ch.Idx, crf, 0, probeSampleWindow{Frames: ch.Frames()}, true)

	encodeStart := time.Now()
	probeResult := encodeProbe(ctx, inputPath, inf, cfg, cropRect, width, height, ch, fullProbePath, crf)
	if probeResult.Error != nil {
		return quality.Probe{}, "", probeResult.Error
	}
	encodeSeconds := time.Since(encodeStart).Seconds()

	var metricSeconds float64
	var windowsScored []quality.ProbeWindow
	for _, sample := range windows {
		sampleChunk := chunk.Chunk{Idx: ch.Idx, Start: ch.Start + sample.Offset, End: ch.Start + sample.Offset + sample.Frames}
		metricResult, err := scoreProbeWindow(ctx, metricPool, inputPath, fullProbePath, inf, sampleChunk, cropRect, width, height, sample.Offset)
		if err != nil {
			return quality.Probe{}, "", err
		}
		metricSeconds += metricResult.MetricSeconds
		windowsScored = append(windowsScored, quality.ProbeWindow{
			Offset: sample.Offset,
			Frames: metricResult.Frames,
			Score:  metricResult.Score,
		})
	}
	score, meanScore, worstScore, scoredFrames := targetQualitySampleScore(windowsScored)
	if scoredFrames == 0 {
		return quality.Probe{}, "", fmt.Errorf("no target-quality sample frames scored for chunk %04d", ch.Idx)
	}

	return quality.Probe{
		CRF:              crf,
		Score:            score,
		MeanScore:        meanScore,
		WorstWindowScore: worstScore,
		Size:             probeResult.Size,
		EncodeSeconds:    encodeSeconds,
		MetricSeconds:    metricSeconds,
		SampleFrames:     scoredFrames,
		Windows:          windowsScored,
	}, fullProbePath, nil
}

func formatProbeWindowScores(windows []quality.ProbeWindow) string {
	if len(windows) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, window := range windows {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%d:%.4f", window.Offset, window.Score)
	}
	b.WriteByte(']')
	return b.String()
}

func targetQualityWindowSpread(windows []quality.ProbeWindow) float32 {
	if len(windows) == 0 {
		return 0
	}
	minScore := windows[0].Score
	maxScore := windows[0].Score
	for _, window := range windows[1:] {
		if window.Score < minScore {
			minScore = window.Score
		}
		if window.Score > maxScore {
			maxScore = window.Score
		}
	}
	return maxScore - minScore
}

func targetQualitySampleScore(windows []quality.ProbeWindow) (score, meanScore, worstScore float32, frames int) {
	if len(windows) == 0 {
		return 0, 0, 0, 0
	}
	worstScore = windows[0].Score
	var weightedScore float64
	for _, window := range windows {
		if window.Frames <= 0 {
			continue
		}
		if window.Score < worstScore {
			worstScore = window.Score
		}
		weightedScore += float64(window.Score) * float64(window.Frames)
		frames += window.Frames
	}
	if frames == 0 {
		return 0, 0, 0, 0
	}
	meanScore = float32(weightedScore / float64(frames))
	return (meanScore + worstScore) / 2, meanScore, worstScore, frames
}

func scoreProbeWindow(
	ctx context.Context,
	metricPool chan *quality.VshipProcessor,
	inputPath string,
	probePath string,
	inf *video.Info,
	sampleChunk chunk.Chunk,
	cropRect *video.CropRect,
	width, height uint32,
	probeStartFrame int,
) (quality.CVVDPResult, error) {
	var processor *quality.VshipProcessor
	select {
	case processor = <-metricPool:
	case <-ctx.Done():
		return quality.CVVDPResult{}, ctx.Err()
	}
	defer func() { metricPool <- processor }()

	metricStart := time.Now()
	metricResult, err := quality.ComputeChunkCVVDP(ctx, quality.CVVDPOptions{
		SourcePath:      inputPath,
		ProbePath:       probePath,
		ProbeStartFrame: probeStartFrame,
		Info:            inf,
		Chunk:           sampleChunk,
		CropRect:        cropRect,
		Width:           width,
		Height:          height,
		Processor:       processor,
	})
	if err != nil {
		return quality.CVVDPResult{}, err
	}
	if metricResult.MetricSeconds == 0 {
		metricResult.MetricSeconds = time.Since(metricStart).Seconds()
	}
	return metricResult, nil
}

func sampledProbePath(workDir string, chunkIdx int, crf float32, sampleIdx int, sample probeSampleWindow, fullChunk bool) string {
	if fullChunk {
		return filepath.Join(workDir, "probes", fmt.Sprintf("%04d_%s.ivf", chunkIdx, quality.FormatCRF(crf)))
	}
	return filepath.Join(workDir, "probes", fmt.Sprintf("%04d_%s_s%d_%d_%d.ivf", chunkIdx, quality.FormatCRF(crf), sampleIdx, sample.Offset, sample.Frames))
}

const (
	targetQualityNeighborMaxDistance = 8
	targetQualityDefaultJODPerCRF    = 0.04
)

type targetQualityPrior struct {
	mu         sync.Mutex
	crfs       map[int]float32
	slopes     []float32
	defaultCRF float32
	minCRF     float32
	maxCRF     float32
}

func newTargetQualityPrior(defaultCRF, minCRF, maxCRF float32) *targetQualityPrior {
	return &targetQualityPrior{
		crfs:       make(map[int]float32),
		defaultCRF: clampCRF(defaultCRF, minCRF, maxCRF),
		minCRF:     minCRF,
		maxCRF:     maxCRF,
	}
}

func seedTargetQualityPrior(workDir string, doneSet map[int]bool, prior *targetQualityPrior) {
	for idx := range doneSet {
		path := filepath.Join(workDir, "tq", fmt.Sprintf("%04d.json", idx))
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var log chunkTargetLog
		if err := json.Unmarshal(data, &log); err != nil {
			continue
		}
		prior.AddResult(idx, log.FinalCRF, log.Probes)
	}
}

func (p *targetQualityPrior) Add(chunkIdx int, crf float32) {
	p.AddResult(chunkIdx, crf, nil)
}

func (p *targetQualityPrior) AddResult(chunkIdx int, crf float32, probes []quality.Probe) {
	if crf <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.crfs[chunkIdx] = clampCRF(crf, p.minCRF, p.maxCRF)
	p.slopes = append(p.slopes, probeSlopes(probes)...)
}

func (p *targetQualityPrior) JODPerCRF() float32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.slopes) == 0 {
		return targetQualityDefaultJODPerCRF
	}
	return medianFloat32(append([]float32(nil), p.slopes...))
}

func (p *targetQualityPrior) InitialCRF(chunkIdx int) (float32, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.crfs) == 0 {
		return p.defaultCRF, "default"
	}

	lowerIdx, upperIdx := 0, 0
	lowerCRF, upperCRF := float32(0), float32(0)
	lowerOK, upperOK := false, false
	values := make([]float32, 0, len(p.crfs))
	for idx, crf := range p.crfs {
		values = append(values, crf)
		switch {
		case idx == chunkIdx:
			return crf, "same"
		case idx < chunkIdx && (!lowerOK || idx > lowerIdx):
			lowerIdx, lowerCRF, lowerOK = idx, crf, true
		case idx > chunkIdx && (!upperOK || idx < upperIdx):
			upperIdx, upperCRF, upperOK = idx, crf, true
		}
	}

	lowerDist, upperDist := targetQualityNeighborMaxDistance+1, targetQualityNeighborMaxDistance+1
	if lowerOK {
		lowerDist = chunkIdx - lowerIdx
	}
	if upperOK {
		upperDist = upperIdx - chunkIdx
	}
	lowerNear := lowerOK && lowerDist <= targetQualityNeighborMaxDistance
	upperNear := upperOK && upperDist <= targetQualityNeighborMaxDistance
	switch {
	case lowerNear && upperNear:
		crf := (lowerCRF*float32(upperDist) + upperCRF*float32(lowerDist)) / float32(lowerDist+upperDist)
		return clampCRF(crf, p.minCRF, p.maxCRF), "neighbor"
	case lowerNear:
		return clampCRF(lowerCRF, p.minCRF, p.maxCRF), "neighbor"
	case upperNear:
		return clampCRF(upperCRF, p.minCRF, p.maxCRF), "neighbor"
	default:
		return medianCRF(values, p.minCRF, p.maxCRF), "median"
	}
}

func probeSlopes(probes []quality.Probe) []float32 {
	if len(probes) < 2 {
		return nil
	}
	probes = append([]quality.Probe(nil), probes...)
	sort.Slice(probes, func(i, j int) bool { return probes[i].CRF < probes[j].CRF })
	slopes := make([]float32, 0, len(probes)-1)
	for i := 0; i < len(probes)-1; i++ {
		left := probes[i]
		right := probes[i+1]
		crfDelta := right.CRF - left.CRF
		scoreDelta := right.Score - left.Score
		if crfDelta <= 0 || scoreDelta >= 0 {
			continue
		}
		slope := -scoreDelta / crfDelta
		if slope >= 0.005 && slope <= 0.2 {
			slopes = append(slopes, slope)
		}
	}
	return slopes
}

func medianCRF(values []float32, minCRF, maxCRF float32) float32 {
	if len(values) == 0 {
		return clampCRF(0, minCRF, maxCRF)
	}
	return clampCRF(medianFloat32(values), minCRF, maxCRF)
}

func medianFloat32(values []float32) float32 {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	mid := len(values) / 2
	if len(values)%2 == 1 {
		return values[mid]
	}
	return (values[mid-1] + values[mid]) / 2
}

func clampCRF(crf, minCRF, maxCRF float32) float32 {
	crf = quality.RoundCRFToQuarter(crf)
	if crf < minCRF {
		return quality.RoundCRFToQuarter(minCRF)
	}
	if crf > maxCRF {
		return quality.RoundCRFToQuarter(maxCRF)
	}
	return crf
}

func encodeProbe(ctx context.Context, inputPath string, inf *video.Info, cfg *EncodeConfig, cropRect *video.CropRect, width, height uint32, ch chunk.Chunk, outputPath string, crf float32) worker.EncodeResult {
	src, err := video.Open(inputPath, 1)
	if err != nil {
		return worker.EncodeResult{ChunkIdx: ch.Idx, Error: fmt.Errorf("failed to open source for probe: %w", err)}
	}
	defer src.Close()
	return encodeChunkStreaming(ctx, src, ch, inf, cropRect, cfg, outputPath, crf, width, height, nil)
}

func logTargetAggregate(logs []chunkTargetLog, verbose func(string)) {
	if verbose == nil || len(logs) == 0 {
		return
	}
	var minScore, maxScore, sumScore, sumAbsErr float32
	var probes int
	crfCounts := make(map[float32]int)
	for i, log := range logs {
		if i == 0 || log.FinalScore < minScore {
			minScore = log.FinalScore
		}
		if i == 0 || log.FinalScore > maxScore {
			maxScore = log.FinalScore
		}
		sumScore += log.FinalScore
		if log.FinalScore > log.Target {
			sumAbsErr += log.FinalScore - log.Target
		} else {
			sumAbsErr += log.Target - log.FinalScore
		}
		probes += len(log.Probes)
		crfCounts[log.FinalCRF]++
	}
	meanScore := sumScore / float32(len(logs))
	meanErr := sumAbsErr / float32(len(logs))
	commonCRF := logs[0].FinalCRF
	commonCount := 0
	for crf, count := range crfCounts {
		if count > commonCount {
			commonCRF = crf
			commonCount = count
		}
	}
	verbose(fmt.Sprintf("TQ summary chunks=%d probes=%d sampled_jod_min=%.4f mean=%.4f max=%.4f mean_abs_error=%.4f common_crf=%s", len(logs), probes, minScore, meanScore, maxScore, meanErr, quality.FormatCRF(commonCRF)))
}

func writeChunkTargetLog(workDir string, log chunkTargetLog) error {
	path := filepath.Join(workDir, "tq", fmt.Sprintf("%04d.json", log.ChunkIdx))
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

func writeAggregateTargetLog(workDir string, logs []chunkTargetLog, tq TargetQualityConfig) {
	sort.Slice(logs, func(i, j int) bool { return logs[i].ChunkIdx < logs[j].ChunkIdx })
	data, err := json.MarshalIndent(struct {
		Target             float32          `json:"target"`
		Tolerance          float32          `json:"tolerance"`
		CRFMin             float32          `json:"crf_min"`
		CRFMax             float32          `json:"crf_max"`
		MetricWorkers      int              `json:"metric_workers"`
		DefaultInitialCRF  float32          `json:"default_initial_crf"`
		ProbeSampleFrames  int              `json:"probe_sample_frames"`
		ProbeFullThreshold int              `json:"probe_full_threshold"`
		Chunks             []chunkTargetLog `json:"chunks"`
	}{
		Target:             tq.Target,
		Tolerance:          tq.Tolerance,
		CRFMin:             tq.CRFMin,
		CRFMax:             tq.CRFMax,
		MetricWorkers:      tq.MetricWorkers,
		DefaultInitialCRF:  tq.InitialCRF,
		ProbeSampleFrames:  tq.SampleFrames,
		ProbeFullThreshold: tq.FullProbeFrames,
		Chunks:             logs,
	}, "", "  ")
	if err != nil {
		return
	}
	data = append(data, '\n')
	_ = os.WriteFile(filepath.Join(workDir, "target-quality.json"), data, 0644)
}

func copyFile(srcPath, dstPath string) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}
	if err := os.Link(srcPath, dstPath); err == nil {
		return nil
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}
