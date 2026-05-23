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

type TargetQualityConfig struct {
	Target        float32
	Tolerance     float32
	CRFMin        float32
	CRFMax        float32
	MaxProbes     int
	MetricWorkers int
	DisplayPath   string
	Verbose       func(string)
}

type targetQualityResult struct {
	worker.EncodeResult
	Log chunkTargetLog
}

type chunkTargetLog struct {
	ChunkIdx    int                `json:"chunk_idx"`
	Frames      int                `json:"frames"`
	Target      float32            `json:"target"`
	Tolerance   float32            `json:"tolerance"`
	CRFMin      float32            `json:"crf_min"`
	CRFMax      float32            `json:"crf_max"`
	Probes      []quality.Probe    `json:"probes"`
	FinalCRF    float32            `json:"final_crf"`
	FinalScore  float32            `json:"final_score"`
	FinalSize   uint64             `json:"final_size"`
	StopReason  quality.StopReason `json:"stop_reason"`
	StartedAt   time.Time          `json:"started_at"`
	CompletedAt time.Time          `json:"completed_at"`
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
				result := encodeTargetQualityChunk(ctx, inputPath, inf, cfg, workDir, cropRect, width, height, ch, searchCtx, metricPool, tq.Verbose)
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
	verbose func(string),
) targetQualityResult {
	log := chunkTargetLog{
		ChunkIdx:  ch.Idx,
		Frames:    ch.Frames(),
		Target:    searchCtx.Target,
		Tolerance: searchCtx.Tolerance,
		CRFMin:    searchCtx.CRFMin,
		CRFMax:    searchCtx.CRFMax,
		StartedAt: time.Now(),
	}
	state := quality.NewSearchState(searchCtx)

	for {
		crf, ok := state.NextCRF(searchCtx)
		if !ok {
			break
		}
		probePath := filepath.Join(workDir, "probes", fmt.Sprintf("%04d_%s.ivf", ch.Idx, quality.FormatCRF(crf)))
		encodeStart := time.Now()
		probeResult := encodeProbe(ctx, inputPath, inf, cfg, cropRect, width, height, ch, probePath, crf)
		if probeResult.Error != nil {
			return targetQualityResult{EncodeResult: probeResult, Log: log}
		}
		encodeSeconds := time.Since(encodeStart).Seconds()

		metricStart := time.Now()
		var processor *quality.VshipProcessor
		select {
		case processor = <-metricPool:
		case <-ctx.Done():
			return targetQualityResult{EncodeResult: worker.EncodeResult{ChunkIdx: ch.Idx, Error: ctx.Err()}, Log: log}
		}
		metricResult, err := quality.ComputeChunkCVVDP(ctx, quality.CVVDPOptions{
			SourcePath: inputPath,
			ProbePath:  probePath,
			Info:       inf,
			Chunk:      ch,
			CropRect:   cropRect,
			Width:      width,
			Height:     height,
			Processor:  processor,
		})
		metricPool <- processor
		if err != nil {
			return targetQualityResult{EncodeResult: worker.EncodeResult{ChunkIdx: ch.Idx, Error: err}, Log: log}
		}
		metricSeconds := metricResult.MetricSeconds
		if metricSeconds == 0 {
			metricSeconds = time.Since(metricStart).Seconds()
		}
		probe := quality.Probe{CRF: crf, Score: metricResult.Score, Size: probeResult.Size, EncodeSeconds: encodeSeconds, MetricSeconds: metricSeconds}
		state.AddProbe(searchCtx, probe)
		log.Probes = append(log.Probes, probe)
		if verbose != nil {
			fps := float64(metricResult.Frames) / metricSeconds
			verbose(fmt.Sprintf("TQ probe chunk=%04d round=%d crf=%s cvvdp=%.4f delta=%+.4f size=%d encode=%.1fs metric=%.1fs metric_fps=%.1f", ch.Idx, state.Round, quality.FormatCRF(crf), probe.Score, probe.Score-searchCtx.Target, probe.Size, encodeSeconds, metricSeconds, fps))
		}
		if state.StopReason != quality.StopNone {
			break
		}
	}

	best, ok := state.BestProbe(searchCtx)
	if !ok {
		return targetQualityResult{EncodeResult: worker.EncodeResult{ChunkIdx: ch.Idx, Error: fmt.Errorf("no target-quality probes completed for chunk %04d", ch.Idx)}, Log: log}
	}
	bestPath := filepath.Join(workDir, "probes", fmt.Sprintf("%04d_%s.ivf", ch.Idx, quality.FormatCRF(best.CRF)))
	finalPath := chunk.IVFPath(workDir, ch.Idx)
	if err := copyFile(bestPath, finalPath); err != nil {
		return targetQualityResult{EncodeResult: worker.EncodeResult{ChunkIdx: ch.Idx, Error: err}, Log: log}
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
	verbose(fmt.Sprintf("TQ summary chunks=%d probes=%d final_jod_min=%.4f mean=%.4f max=%.4f mean_abs_error=%.4f common_crf=%s", len(logs), probes, minScore, meanScore, maxScore, meanErr, quality.FormatCRF(commonCRF)))
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
		Target        float32          `json:"target"`
		Tolerance     float32          `json:"tolerance"`
		CRFMin        float32          `json:"crf_min"`
		CRFMax        float32          `json:"crf_max"`
		MetricWorkers int              `json:"metric_workers"`
		Chunks        []chunkTargetLog `json:"chunks"`
	}{
		Target:        tq.Target,
		Tolerance:     tq.Tolerance,
		CRFMin:        tq.CRFMin,
		CRFMax:        tq.CRFMax,
		MetricWorkers: tq.MetricWorkers,
		Chunks:        logs,
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
