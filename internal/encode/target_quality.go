package encode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
)

type TargetQualityConfig struct {
	Target          float32
	Tolerance       float32
	CRFMin          float32
	CRFMax          float32
	MaxProbes       int
	MetricWorkers   int
	DisplayPath     string
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
	Probes             []quality.Probe    `json:"probes"`
	FinalCRF           float32            `json:"final_crf"`
	FinalScore         float32            `json:"final_score"`
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
	sort.SliceStable(remainingChunks, func(i, j int) bool {
		return remainingChunks[i].Frames() > remainingChunks[j].Frames()
	})

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
		Target:    tq.Target,
		Tolerance: tq.Tolerance,
		CRFMin:    tq.CRFMin,
		CRFMax:    tq.CRFMax,
		MaxProbes: tq.MaxProbes,
	}

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
				result := encodeTargetQualityChunk(ctx, inputPath, inf, cfg, workDir, cropRect, width, height, ch, searchCtx, metricPool, tq.SampleFrames, tq.FullProbeFrames, tq.Verbose)
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
	sampleFrames int,
	fullProbeFrames int,
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
		StartedAt:          time.Now(),
	}
	state := quality.NewSearchState(searchCtx)
	fullProbePaths := make(map[string]string)

	for {
		crf, ok := state.NextCRF(searchCtx)
		if !ok {
			break
		}
		probe, fullProbePath, err := encodeSampledProbe(ctx, inputPath, inf, cfg, workDir, cropRect, width, height, ch, crf, sampleFrames, fullProbeFrames, metricPool)
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
			verbose(fmt.Sprintf("TQ sample chunk=%04d round=%d crf=%s cvvdp=%.4f delta=%+.4f size=%d sample_frames=%d encode=%.1fs metric=%.1fs metric_fps=%.1f", ch.Idx, state.Round, quality.FormatCRF(crf), probe.Score, probe.Score-searchCtx.Target, probe.Size, probe.SampleFrames, probe.EncodeSeconds, probe.MetricSeconds, fps))
		}
		if state.StopReason != quality.StopNone {
			break
		}
	}

	best, ok := state.BestProbe(searchCtx)
	if !ok {
		return targetQualityResult{EncodeResult: worker.EncodeResult{ChunkIdx: ch.Idx, Error: fmt.Errorf("no target-quality probes completed for chunk %04d", ch.Idx)}, Log: log}
	}
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
		verbose(fmt.Sprintf("TQ final chunk=%04d crf=%s cvvdp=%.4f size=%d probes=%d stop=%s", ch.Idx, quality.FormatCRF(best.CRF), best.Score, log.FinalSize, len(log.Probes), log.StopReason))
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
	metricPool chan *quality.VshipProcessor,
) (quality.Probe, string, error) {
	windows := sampledProbeWindows(ch.Frames(), sampleFrames, fullProbeFrames)
	if len(windows) == 0 {
		return quality.Probe{}, "", fmt.Errorf("chunk %04d has no frames", ch.Idx)
	}

	var totalSize uint64
	var encodeSeconds, metricSeconds, weightedScore float64
	var scoredFrames int
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

		metricStart := time.Now()
		var processor *quality.VshipProcessor
		select {
		case processor = <-metricPool:
		case <-ctx.Done():
			return quality.Probe{}, "", ctx.Err()
		}
		metricResult, err := quality.ComputeChunkCVVDP(ctx, quality.CVVDPOptions{
			SourcePath: inputPath,
			ProbePath:  probePath,
			Info:       inf,
			Chunk:      sampleChunk,
			CropRect:   cropRect,
			Width:      width,
			Height:     height,
			Processor:  processor,
		})
		metricPool <- processor
		if err != nil {
			return quality.Probe{}, "", err
		}
		sampleMetricSeconds := metricResult.MetricSeconds
		if sampleMetricSeconds == 0 {
			sampleMetricSeconds = time.Since(metricStart).Seconds()
		}
		metricSeconds += sampleMetricSeconds
		weightedScore += float64(metricResult.Score) * float64(metricResult.Frames)
		scoredFrames += metricResult.Frames

		if isFullChunk {
			fullProbePath = probePath
		} else {
			_ = os.Remove(probePath)
		}
	}
	if scoredFrames == 0 {
		return quality.Probe{}, "", fmt.Errorf("no target-quality sample frames scored for chunk %04d", ch.Idx)
	}

	return quality.Probe{
		CRF:           crf,
		Score:         float32(weightedScore / float64(scoredFrames)),
		Size:          totalSize,
		EncodeSeconds: encodeSeconds,
		MetricSeconds: metricSeconds,
		SampleFrames:  scoredFrames,
	}, fullProbePath, nil
}

func sampledProbePath(workDir string, chunkIdx int, crf float32, sampleIdx int, sample probeSampleWindow, fullChunk bool) string {
	if fullChunk {
		return filepath.Join(workDir, "probes", fmt.Sprintf("%04d_%s.ivf", chunkIdx, quality.FormatCRF(crf)))
	}
	return filepath.Join(workDir, "probes", fmt.Sprintf("%04d_%s_s%d_%d_%d.ivf", chunkIdx, quality.FormatCRF(crf), sampleIdx, sample.Offset, sample.Frames))
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
		ProbeSampleFrames  int              `json:"probe_sample_frames"`
		ProbeFullThreshold int              `json:"probe_full_threshold"`
		Chunks             []chunkTargetLog `json:"chunks"`
	}{
		Target:             tq.Target,
		Tolerance:          tq.Tolerance,
		CRFMin:             tq.CRFMin,
		CRFMax:             tq.CRFMax,
		MetricWorkers:      tq.MetricWorkers,
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
