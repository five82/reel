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
	// targetQualityScheduleBlockChunks keeps work close enough to timeline order
	// for neighbor priors while still starting large chunks early enough to avoid
	// a final long-tail. Smaller blocks measured slower because early priors were worse.
	targetQualityScheduleBlockChunks = 32

	// targetQualityPriorPrimeChunks is how many completed chunks must seed the
	// CRF prior before dispatch opens beyond the prime-phase concurrency.
	// Starting too many chunks cold measurably increases probes and error.
	targetQualityPriorPrimeChunks = 4

	// targetQualityFlightGrowth is how many extra in-flight chunks are allowed
	// per completed chunk after priming. Each completed chunk adds a usable
	// CRF prior, so concurrency opens up roughly in step with prior coverage.
	targetQualityFlightGrowth = 3

	// targetQualityProgressSampleInterval paces the timer-based progress
	// sampler. Matches the perf collector's unchanged-sample throttle so the
	// worker history stays bounded.
	targetQualityProgressSampleInterval = 2 * time.Second
)

type TargetQualityConfig struct {
	Target        float32
	Tolerance     float32
	CRFMin        float32
	CRFMax        float32
	MaxProbes     int
	MetricWorkers int
	DisplayPath   string
	InitialCRF    float32
	Verbose       func(string)
}

type targetQualityResult struct {
	worker.EncodeResult
	Log chunkTargetLog
}

type chunkTargetLog struct {
	ChunkIdx         int                `json:"chunk_idx"`
	Frames           int                `json:"frames"`
	Target           float32            `json:"target"`
	Tolerance        float32            `json:"tolerance"`
	CRFMin           float32            `json:"crf_min"`
	CRFMax           float32            `json:"crf_max"`
	InitialCRF       float32            `json:"initial_crf"`
	InitialCRFSource string             `json:"initial_crf_source"`
	Probes           []quality.Probe    `json:"probes"`
	FinalCRF         float32            `json:"final_crf"`
	FinalScore       float32            `json:"final_score"`
	FinalSize        uint64             `json:"final_size"`
	StopReason       quality.StopReason `json:"stop_reason"`
	StartedAt        time.Time          `json:"started_at"`
	CompletedAt      time.Time          `json:"completed_at"`
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
	initialWorkers := initialAdaptiveWorkers(maxWorkers, width, height, availableMemoryBytes())
	// Priming at the UHD slot target (5) instead of the floor (3) was tested
	// and rejected (2026-07-01): once the metric decoder stopped starving the
	// GPU the prime-phase idle it targeted mostly disappeared, so it bought no
	// wall beyond noise and cost extra cold-seeded probes (+4-5% probes/chunk)
	// and +1-3% size on two of three 4K clips. See PERFORMANCE_TESTING_LOG.md.
	primeConcurrency := resolutionWorkerFloor(maxWorkers, width, height)
	rampCeiling := resolutionRampCeiling(maxWorkers, width, height)
	limiter := newAdaptiveLimiter(maxWorkers, initialWorkers, rampCeiling, totalFrames, cfg.StatusCallback)
	cfg.LevelOfParallelism = resolveLevelOfParallelism(cfg.LevelOfParallelism, rampCeiling)

	// One VSHIP/CUDA handler PER metric worker, scored concurrently. CVVDP is a
	// per-handler temporal metric and Vship forbids using one handler from two
	// threads at once, so each worker owns a distinct handler. CORRECTNESS DEPENDS
	// on libvship being built with MITIGATE_MALLOC_ASYNC: the default
	// cudaMallocAsync allocator races across coexisting handlers, silently
	// corrupts scores, trips the floor guard, and can cascade into huge output-size
	// swings. Verify the linked library with scripts/handlertest; see
	// docs/VSHIP_CONCURRENCY_BUG.md.
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

	// inFlight counts chunks currently probing/scoring/encoding. It is mutated
	// only under flightMu (so the dispatch cond stays correct) but stored
	// atomically so progress snapshots can read it lock-free.
	var inFlight atomic.Int64
	snapshotProgress := func() worker.Progress {
		p := progress
		p.ActiveWorkers, p.TargetWorkers, p.MaxWorkers = limiter.stats()
		p.InFlight = int(inFlight.Load())
		p.EncodeSlotWaitSeconds = limiter.slotWaitSeconds()
		return p
	}

	// progressCb is invoked from both the collector goroutine and the sampling
	// ticker; serialize calls so downstream reporters and the perf collector
	// see a single ordered stream.
	var progressCbMu sync.Mutex
	emitProgress := func(p worker.Progress) {
		if progressCb == nil {
			return
		}
		progressCbMu.Lock()
		defer progressCbMu.Unlock()
		progressCb(p)
	}

	var flightMu sync.Mutex
	flightCond := sync.NewCond(&flightMu)

	var encodeErr atomic.Pointer[error]
	setError := func(err error) {
		encodeErr.CompareAndSwap(nil, &err)
		flightCond.Broadcast()
	}
	getError := func() error {
		if p := encodeErr.Load(); p != nil {
			return *p
		}
		return nil
	}

	initialJODPerCRF := float32(targetQualityDefaultJODPerCRF)
	searchCtx := quality.SearchContext{
		Target:     tq.Target,
		Tolerance:  tq.Tolerance,
		CRFMin:     tq.CRFMin,
		CRFMax:     tq.CRFMax,
		MaxProbes:  tq.MaxProbes,
		InitialCRF: tq.InitialCRF,
		JODPerCRF:  initialJODPerCRF,
	}
	prior := newTargetQualityPrior(tq.InitialCRF, tq.CRFMin, tq.CRFMax, tq.Target, initialJODPerCRF)
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
				result := encodeTargetQualityChunk(ctx, inputPath, inf, cfg, workDir, cropRect, width, height, ch, chunkSearchCtx, metricPool, limiter, prior, initialCRFSource, tq.Verbose)
				limiter.release()
				resultChan <- result
			}
		}()
	}

	// Scoring releases encode slots, so slots alone no longer bound how many
	// chunks are in flight. Two concerns are balanced here:
	//   - protect the CRF prior: keep concurrency at the prime floor until a
	//     few chunks complete, so early chunks are not all started cold;
	//   - keep encode slots fed: a chunk spends ~half its time scoring with its
	//     slot released, so in-flight must exceed the slot target (~2x) to keep
	//     every slot busy across the probe/score duty cycle.
	// After priming, the cap opens only as fast as completed chunks accumulate
	// usable priors, so newly started chunks get neighbor/median seeds.
	flightCap := func() int {
		done := prior.Count()
		if done < targetQualityPriorPrimeChunks {
			return primeConcurrency
		}
		_, target, _ := limiter.stats()
		ceil := target + max(target, tq.MetricWorkers)
		byPriors := primeConcurrency + targetQualityFlightGrowth*(done-targetQualityPriorPrimeChunks)
		return min(byPriors, ceil)
	}
	chunkDone := func() {
		flightMu.Lock()
		inFlight.Add(-1)
		flightCond.Broadcast()
		flightMu.Unlock()
	}
	go func() {
		<-ctx.Done()
		flightCond.Broadcast()
	}()

	var logsMu sync.Mutex
	logs := make([]chunkTargetLog, 0, len(remainingChunks))
	var collectorWg sync.WaitGroup
	collectorWg.Add(1)
	go func() {
		defer collectorWg.Done()
		for result := range resultChan {
			chunkDone()
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
			emitProgress(p)
		}
	}()

	// Chunk-completion callbacks alone sample the scheduler at a biased
	// instant -- the finishing worker just released its encode slot -- which
	// under-reports active/in-flight in perf.json and leaves blind windows
	// behind slow chunks. A coarse ticker adds unbiased samples between
	// completions; the perf collector drops unchanged samples, so the history
	// stays small.
	go func() {
		ticker := time.NewTicker(targetQualityProgressSampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				progressMu.Lock()
				p := snapshotProgress()
				progressMu.Unlock()
				emitProgress(p)
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
			flightMu.Lock()
			for int(inFlight.Load()) >= flightCap() && ctx.Err() == nil && getError() == nil {
				flightCond.Wait()
			}
			inFlight.Add(1)
			flightMu.Unlock()
			if ctx.Err() != nil || getError() != nil {
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
	limiter *adaptiveLimiter,
	prior *targetQualityPrior,
	initialCRFSource string,
	verbose func(string),
) targetQualityResult {
	log := chunkTargetLog{
		ChunkIdx:         ch.Idx,
		Frames:           ch.Frames(),
		Target:           searchCtx.Target,
		Tolerance:        searchCtx.Tolerance,
		CRFMin:           searchCtx.CRFMin,
		CRFMax:           searchCtx.CRFMax,
		InitialCRF:       searchCtx.InitialCRF,
		InitialCRFSource: initialCRFSource,
		StartedAt:        time.Now(),
	}
	state := quality.NewSearchState(searchCtx)

	for {
		crf, ok := state.NextCRF(searchCtx)
		if !ok {
			break
		}
		probe, err := encodeChunkProbe(ctx, inputPath, inf, cfg, workDir, cropRect, width, height, ch, crf, metricPool, limiter)
		if err != nil {
			return targetQualityResult{EncodeResult: worker.EncodeResult{ChunkIdx: ch.Idx, Error: err}, Log: log}
		}
		state.AddProbe(searchCtx, probe)
		log.Probes = append(log.Probes, probe)
		if verbose != nil {
			var fps float64
			if probe.MetricSeconds > 0 {
				fps = float64(probe.Frames) / probe.MetricSeconds
			}
			initial := ""
			if state.Round == 1 {
				initial = fmt.Sprintf(" initial_source=%s", initialCRFSource)
			}
			verbose(fmt.Sprintf("TQ probe chunk=%04d round=%d crf=%s%s cvvdp=%.4f delta=%+.4f size=%d frames=%d encode=%.1fs metric=%.1fs metric_fps=%.1f", ch.Idx, state.Round, quality.FormatCRF(crf), initial, probe.Score, probe.Score-searchCtx.Target, probe.Size, probe.Frames, probe.EncodeSeconds, probe.MetricSeconds, fps))
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
	// Every probe is a whole-chunk encode kept at its CRF path, so the converged
	// probe is reused verbatim as the final chunk -- no re-encode. copyFile errors
	// if that IVF is somehow absent.
	finalPath := chunk.IVFPath(workDir, ch.Idx)
	if err := copyFile(probeIVFPath(workDir, ch.Idx, best.CRF), finalPath); err != nil {
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

// encodeChunkProbe encodes the whole chunk at crf to a reusable IVF and scores
// it against the source with a single whole-chunk CVVDP pass. The probe IVF is
// kept (fsynced) so the converged probe can be reused verbatim as the final
// chunk. The encode slot is released while the GPU scores and re-acquired before
// returning, so the caller keeps the invariant that a slot is held on entry and
// exit while other chunks encode during the metric wait.
func encodeChunkProbe(
	ctx context.Context,
	inputPath string,
	inf *video.Info,
	cfg *EncodeConfig,
	workDir string,
	cropRect *video.CropRect,
	width, height uint32,
	ch chunk.Chunk,
	crf float32,
	metricPool chan *quality.VshipProcessor,
	limiter *adaptiveLimiter,
) (quality.Probe, error) {
	if ch.Frames() <= 0 {
		return quality.Probe{}, fmt.Errorf("chunk %04d has no frames", ch.Idx)
	}
	probePath := probeIVFPath(workDir, ch.Idx, crf)

	encodeStart := time.Now()
	// Whole-chunk probe; reusable as the final chunk, so keep the fsync.
	probeResult := encodeProbe(ctx, inputPath, inf, cfg, cropRect, width, height, ch, probePath, crf)
	if probeResult.Error != nil {
		return quality.Probe{}, probeResult.Error
	}
	encodeSeconds := time.Since(encodeStart).Seconds()

	// Release the encode slot while the GPU scores, then re-acquire it so other
	// chunks can encode during the metric wait.
	limiter.release()
	result, scoreErr := scoreChunkProbe(ctx, metricPool, inputPath, probePath, inf, ch, cropRect, width, height)
	if _, err := limiter.acquire(ctx); err != nil && scoreErr == nil {
		scoreErr = err
	}
	if scoreErr != nil {
		return quality.Probe{}, scoreErr
	}
	if result.Frames == 0 {
		return quality.Probe{}, fmt.Errorf("no frames scored for chunk %04d", ch.Idx)
	}

	return quality.Probe{
		CRF:           crf,
		Score:         result.Score,
		Size:          probeResult.Size,
		EncodeSeconds: encodeSeconds,
		MetricSeconds: result.MetricSeconds,
		Frames:        result.Frames,
	}, nil
}

func scoreChunkProbe(
	ctx context.Context,
	metricPool chan *quality.VshipProcessor,
	inputPath string,
	probePath string,
	inf *video.Info,
	ch chunk.Chunk,
	cropRect *video.CropRect,
	width, height uint32,
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
		SourcePath: inputPath,
		ProbePath:  probePath,
		Info:       inf,
		Chunk:      ch,
		CropRect:   cropRect,
		Width:      width,
		Height:     height,
		Processor:  processor,
	})
	if err != nil {
		return quality.CVVDPResult{}, err
	}
	if metricResult.MetricSeconds == 0 {
		metricResult.MetricSeconds = time.Since(metricStart).Seconds()
	}
	return metricResult, nil
}

func probeIVFPath(workDir string, chunkIdx int, crf float32) string {
	return filepath.Join(workDir, "probes", fmt.Sprintf("%04d_%s.ivf", chunkIdx, quality.FormatCRF(crf)))
}

const (
	targetQualityNeighborMaxDistance = 8
	// targetQualityDefaultJODPerCRF is the no-information JOD-per-CRF slope
	// used until measured slopes accumulate. Observed slopes across the 1080p
	// and 4K test corpus cluster at 0.02-0.03; the former SDR-only 0.04 halved
	// the cold second-probe step and cost extra probes on high-CRF SDR content
	// (2026-07-01 replay simulation + A/B, see PERFORMANCE_TESTING_LOG.md), so
	// one slope now serves every tier.
	targetQualityDefaultJODPerCRF   = 0.025
	targetQualityPriorMaxAdjustment = 3.0
)

type targetQualityPrior struct {
	mu            sync.Mutex
	crfs          map[int]float32
	slopes        []float32
	defaultCRF    float32
	minCRF        float32
	maxCRF        float32
	target        float32
	defaultJODCRF float32
}

func newTargetQualityPrior(defaultCRF, minCRF, maxCRF, target, defaultJODPerCRF float32) *targetQualityPrior {
	if defaultJODPerCRF <= 0 {
		defaultJODPerCRF = targetQualityDefaultJODPerCRF
	}
	return &targetQualityPrior{
		crfs:          make(map[int]float32),
		defaultCRF:    clampCRF(defaultCRF, minCRF, maxCRF),
		minCRF:        minCRF,
		maxCRF:        maxCRF,
		target:        target,
		defaultJODCRF: defaultJODPerCRF,
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

func (p *targetQualityPrior) AddResult(chunkIdx int, crf float32, probes []quality.Probe) {
	if crf <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.slopes = append(p.slopes, probeSlopes(probes)...)
	p.crfs[chunkIdx] = p.normalizedCRF(crf, probes)
}

func (p *targetQualityPrior) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.crfs)
}

func (p *targetQualityPrior) JODPerCRF() float32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.jodPerCRFLocked()
}

func (p *targetQualityPrior) jodPerCRFLocked() float32 {
	if len(p.slopes) == 0 {
		return p.defaultJODCRF
	}
	return medianFloat32(append([]float32(nil), p.slopes...))
}

func (p *targetQualityPrior) normalizedCRF(crf float32, probes []quality.Probe) float32 {
	normalized := crf
	if score, ok := probeScoreAtCRF(probes, crf); ok {
		if slope := p.jodPerCRFLocked(); slope > 0 {
			adjust := (score - p.target) / slope
			if adjust > targetQualityPriorMaxAdjustment {
				adjust = targetQualityPriorMaxAdjustment
			} else if adjust < -targetQualityPriorMaxAdjustment {
				adjust = -targetQualityPriorMaxAdjustment
			}
			normalized = crf + adjust
		}
	}
	return clampCRF(normalized, p.minCRF, p.maxCRF)
}

func probeScoreAtCRF(probes []quality.Probe, crf float32) (float32, bool) {
	want := quality.RoundCRFToQuarter(crf)
	for _, probe := range probes {
		if quality.RoundCRFToQuarter(probe.CRF) == want {
			return probe.Score, true
		}
	}
	return 0, false
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
		// Seeding from the nearest completed chunk instead of the median was
		// tested and rejected (2026-07-02): flat on sullyhv, worse on ko.
		// Beyond the neighbor cap a single distant chunk is a noisier
		// estimator than the median. See PERFORMANCE_TESTING_LOG.md.
		return medianCRF(values, p.minCRF, p.maxCRF), "median"
	}
}

func medianCRF(values []float32, minCRF, maxCRF float32) float32 {
	if len(values) == 0 {
		return clampCRF(0, minCRF, maxCRF)
	}
	return clampCRF(medianFloat32(values), minCRF, maxCRF)
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

// encodeProbe encodes a whole-chunk probe or final chunk to outputPath. The IVF
// is always fsynced: every probe is reusable as the final chunk and relied on
// for resume.
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
	sort.Slice(logs, func(i, j int) bool { return logs[i].ChunkIdx < logs[j].ChunkIdx })

	var minScore, maxScore, sumScore, sumAbsErr float32
	var probes int
	crfCounts := make(map[float32]int)
	probeCounts := make(map[int]int)
	stopCounts := make(map[quality.StopReason]int)
	sourceCounts := make(map[string]int)
	var maxProbeChunks []int
	var multiProbeLogs []chunkTargetLog
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
		probeCount := len(log.Probes)
		probes += probeCount
		probeCounts[probeCount]++
		stopCounts[log.StopReason]++
		if log.InitialCRFSource != "" {
			sourceCounts[log.InitialCRFSource]++
		}
		crfCounts[log.FinalCRF]++
		if log.StopReason == quality.StopMaxProbes {
			maxProbeChunks = append(maxProbeChunks, log.ChunkIdx)
		}
		if probeCount >= 3 {
			multiProbeLogs = append(multiProbeLogs, log)
		}
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
	verbose(fmt.Sprintf("TQ summary chunks=%d probes=%d probes_per_chunk=%.2f jod_min=%.4f mean=%.4f max=%.4f mean_abs_error=%.4f common_crf=%s", len(logs), probes, float64(probes)/float64(len(logs)), minScore, meanScore, maxScore, meanErr, quality.FormatCRF(commonCRF)))
	verbose(fmt.Sprintf("TQ decisions stops=%s probe_counts=%s initial_sources=%s", formatStopCounts(stopCounts), formatIntCounts(probeCounts), formatStringCounts(sourceCounts)))
	if len(multiProbeLogs) > 0 {
		verbose(fmt.Sprintf("TQ multi-probe chunks: %s", formatMultiProbeChunks(multiProbeLogs, 8)))
	}
	if len(maxProbeChunks) > 0 {
		verbose(fmt.Sprintf("TQ max-probe chunks: %s", formatChunkList(maxProbeChunks, 12)))
	}
}

func formatIntCounts(counts map[int]int) string {
	if len(counts) == 0 {
		return "{}"
	}
	keys := make([]int, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%d:%d", key, counts[key]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func formatStringCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", key, counts[key]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func formatMultiProbeChunks(logs []chunkTargetLog, limit int) string {
	if len(logs) == 0 {
		return "[]"
	}
	logs = append([]chunkTargetLog(nil), logs...)
	sort.Slice(logs, func(i, j int) bool {
		if len(logs[i].Probes) != len(logs[j].Probes) {
			return len(logs[i].Probes) > len(logs[j].Probes)
		}
		return logs[i].ChunkIdx < logs[j].ChunkIdx
	})
	if limit <= 0 || limit > len(logs) {
		limit = len(logs)
	}
	parts := make([]string, 0, limit+1)
	for _, log := range logs[:limit] {
		first := log.Probes[0]
		last := log.Probes[len(log.Probes)-1]
		parts = append(parts, fmt.Sprintf("%04d:%d probes crf %s->%s jod %.4f->%.4f stop=%s", log.ChunkIdx, len(log.Probes), quality.FormatCRF(first.CRF), quality.FormatCRF(last.CRF), first.Score, last.Score, log.StopReason))
	}
	if limit < len(logs) {
		parts = append(parts, fmt.Sprintf("+%d more", len(logs)-limit))
	}
	return "[" + strings.Join(parts, "; ") + "]"
}

func formatStopCounts(counts map[quality.StopReason]int) string {
	stringsCounts := make(map[string]int, len(counts))
	for reason, count := range counts {
		key := string(reason)
		if key == "" {
			key = "none"
		}
		stringsCounts[key] = count
	}
	return formatStringCounts(stringsCounts)
}

func formatChunkList(chunks []int, limit int) string {
	if len(chunks) == 0 {
		return "[]"
	}
	sort.Ints(chunks)
	if limit <= 0 || limit > len(chunks) {
		limit = len(chunks)
	}
	parts := make([]string, 0, limit+1)
	for _, chunkIdx := range chunks[:limit] {
		parts = append(parts, fmt.Sprintf("%04d", chunkIdx))
	}
	if limit < len(chunks) {
		parts = append(parts, fmt.Sprintf("+%d more", len(chunks)-limit))
	}
	return "[" + strings.Join(parts, ",") + "]"
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
		Target            float32          `json:"target"`
		Tolerance         float32          `json:"tolerance"`
		CRFMin            float32          `json:"crf_min"`
		CRFMax            float32          `json:"crf_max"`
		MetricWorkers     int              `json:"metric_workers"`
		DefaultInitialCRF float32          `json:"default_initial_crf"`
		Chunks            []chunkTargetLog `json:"chunks"`
	}{
		Target:            tq.Target,
		Tolerance:         tq.Tolerance,
		CRFMin:            tq.CRFMin,
		CRFMax:            tq.CRFMax,
		MetricWorkers:     tq.MetricWorkers,
		DefaultInitialCRF: tq.InitialCRF,
		Chunks:            logs,
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
