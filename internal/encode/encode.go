// Package encode provides the parallel chunk encoding pipeline.
package encode

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/five82/reel/internal/chunk"
	"github.com/five82/reel/internal/encoder"
	"github.com/five82/reel/internal/ffms"
	"github.com/five82/reel/internal/util"
	"github.com/five82/reel/internal/worker"
)

// EncodeConfig contains configuration for the parallel encode pipeline.
type EncodeConfig struct {
	Workers           int     // Number of parallel encoder workers
	ChunkBuffer       int     // Extra chunks to buffer in memory
	CRF               float32 // Quality (CRF value)
	Preset            uint8   // SVT-AV1 preset
	Tune              uint8   // SVT-AV1 tune
	GrainTable        *string // Optional film grain table path
	LogicalProcessors int     // Threads per worker (--lp flag), calculated if 0

	// Advanced SVT-AV1 parameters
	ACBias                float32
	EnableVarianceBoost   bool
	VarianceBoostStrength uint8
	VarianceOctile        uint8
}

// ProgressCallback is called to report encoding progress.
type ProgressCallback func(progress worker.Progress)

// EncodeAll runs the parallel encoding pipeline.
// Uses streaming frame pipeline: each worker decodes and encodes one frame at a time,
// avoiding the need to hold all frames in memory at once.
//
// Returns (actualWorkers, error) where actualWorkers is the number of workers used
// (may be less than cfg.Workers if capped due to memory constraints).
func EncodeAll(
	ctx context.Context,
	chunks []chunk.Chunk,
	inf *ffms.VidInf,
	cfg *EncodeConfig,
	idx *ffms.VidIdx,
	workDir string,
	cropH, cropV uint32,
	progressCb ProgressCallback,
) (int, error) {
	// Ensure encode directory exists
	if err := chunk.EnsureEncodeDir(workDir); err != nil {
		return 0, fmt.Errorf("failed to create encode directory: %w", err)
	}

	// Load resume information
	resume, err := chunk.GetResume(workDir)
	if err != nil {
		return 0, fmt.Errorf("failed to load resume info: %w", err)
	}
	doneSet := resume.DoneSet()

	// Count remaining chunks
	remainingChunks := make([]chunk.Chunk, 0, len(chunks))
	totalFrames := 0
	for _, ch := range chunks {
		totalFrames += ch.Frames()
		if !doneSet[ch.Idx] {
			remainingChunks = append(remainingChunks, ch)
		}
	}

	if len(remainingChunks) == 0 {
		return cfg.Workers, nil // All chunks already done
	}

	// Determine decode strategy
	strat, cropCalc, err := ffms.GetDecodeStrat(idx, inf, cropH, cropV)
	if err != nil {
		return 0, fmt.Errorf("failed to determine decode strategy: %w", err)
	}

	// Calculate effective dimensions
	width := inf.Width
	height := inf.Height
	if cropCalc != nil {
		width = cropCalc.NewW
		height = cropCalc.NewH
	}

	// Cap workers based on resolution and available memory
	actualWorkers, _ := CapWorkers(cfg.Workers, width, height)

	// Calculate optimal threads per worker if not explicitly set
	if cfg.LogicalProcessors == 0 {
		cfg.LogicalProcessors = calculateThreadsPerWorker(actualWorkers, width)
	}

	// Calculate permits for actual worker count
	permits := CalculatePermits(actualWorkers, cfg.ChunkBuffer)
	sem := worker.NewSemaphore(permits)

	// Chunk channel - workers receive chunk metadata (not decoded frames)
	chunkChan := make(chan chunk.Chunk, permits)

	// Results channel
	resultChan := make(chan worker.EncodeResult, len(remainingChunks))

	// Progress tracking
	var progressMu sync.Mutex
	progress := worker.Progress{
		ChunksTotal:    len(chunks),
		ChunksComplete: len(chunks) - len(remainingChunks),
		FramesTotal:    totalFrames,
		FramesComplete: resume.TotalEncodedFrames(),
		BytesComplete:  resume.TotalEncodedSize(),
	}

	// Error handling with atomic pointer for thread-safe access
	var encodeErr atomic.Pointer[error]
	setError := func(err error) {
		encodeErr.CompareAndSwap(nil, &err)
	}
	getError := func() error {
		if p := encodeErr.Load(); p != nil {
			return *p
		}
		return nil
	}

	// Start streaming workers - each creates its own VidSrc for thread safety
	var workerWg sync.WaitGroup
	for i := 0; i < actualWorkers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			streamingWorker(ctx, idx, chunkChan, resultChan, sem, cfg, inf, strat, cropCalc, workDir, width, height, setError, getError)
		}()
	}

	// Start result collector
	var collectorWg sync.WaitGroup
	collectorWg.Add(1)
	go func() {
		defer collectorWg.Done()
		for result := range resultChan {
			if result.Error != nil {
				setError(result.Error)
				continue
			}

			// Update progress
			progressMu.Lock()
			progress.ChunksComplete++
			progress.FramesComplete += result.Frames
			progress.BytesComplete += result.Size
			progressMu.Unlock()

			// Append to done file (ignore errors, resume will handle incomplete state)
			_ = chunk.AppendDone(chunk.ChunkComp{
				Idx:    result.ChunkIdx,
				Frames: result.Frames,
				Size:   result.Size,
			}, workDir)

			// Report progress
			if progressCb != nil {
				progressMu.Lock()
				p := progress
				progressMu.Unlock()
				progressCb(p)
			}
		}
	}()

	// Chunk dispatcher goroutine
	go func() {
		defer close(chunkChan)

		for _, ch := range remainingChunks {
			// Check for cancellation
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Check for error (atomic read)
			if getError() != nil {
				return
			}

			// Acquire semaphore with context cancellation support
			select {
			case <-sem.Chan():
				// Permit acquired
			case <-ctx.Done():
				return
			}

			// Send chunk metadata to worker
			select {
			case chunkChan <- ch:
				// Successfully sent
			case <-ctx.Done():
				// Context cancelled while waiting to send
				sem.Release()
				return
			}
		}
	}()

	// Wait for workers to finish
	workerWg.Wait()
	close(resultChan)

	// Wait for result collector
	collectorWg.Wait()

	return actualWorkers, getError()
}

// streamingWorker runs in a goroutine and processes chunks using streaming decode/encode.
// Each worker creates its own VidSrc for thread safety, then streams frames one at a time.
func streamingWorker(
	ctx context.Context,
	idx *ffms.VidIdx,
	chunkChan <-chan chunk.Chunk,
	resultChan chan<- worker.EncodeResult,
	sem *worker.Semaphore,
	cfg *EncodeConfig,
	inf *ffms.VidInf,
	strat ffms.DecodeStrat,
	cropCalc *ffms.CropCalc,
	workDir string,
	width, height uint32,
	setError func(error),
	getError func() error,
) {
	// Create per-worker video source (single-threaded, thread-safe)
	src, err := ffms.ThrVidSrc(idx, 1)
	if err != nil {
		setError(fmt.Errorf("failed to create video source for worker: %w", err))
		// Drain chunks and release permits
		for range chunkChan {
			sem.Release()
		}
		return
	}
	defer src.Close()

	for ch := range chunkChan {
		// Check for cancellation
		select {
		case <-ctx.Done():
			sem.Release()
			resultChan <- worker.EncodeResult{
				ChunkIdx: ch.Idx,
				Error:    ctx.Err(),
			}
			continue
		default:
		}

		// Check for error from other workers
		if getError() != nil {
			sem.Release()
			continue
		}

		// Encode the chunk using streaming (decode one frame, encode, repeat)
		result := encodeChunkStreaming(ctx, src, ch, inf, strat, cropCalc, cfg, workDir, width, height)

		// Release semaphore
		sem.Release()

		// Send result
		resultChan <- result
	}
}

// encodeChunkStreaming decodes and encodes frames one at a time, reusing a single frame buffer.
// This dramatically reduces memory usage compared to decoding all frames upfront.
// Memory per worker: ~6 MB (single frame) instead of ~5 GB (all frames in chunk).
func encodeChunkStreaming(
	ctx context.Context,
	src *ffms.VidSrc,
	ch chunk.Chunk,
	inf *ffms.VidInf,
	strat ffms.DecodeStrat,
	cropCalc *ffms.CropCalc,
	cfg *EncodeConfig,
	workDir string,
	width, height uint32,
) worker.EncodeResult {
	frameCount := ch.Frames()
	frameSize := ffms.CalcFrameSize(inf, cropCalc)

	// Single frame buffer, reused for each frame (~6 MB for 1080p 10-bit)
	frameBuf := make([]byte, frameSize)

	outputPath := chunk.IVFPath(workDir, ch.Idx)

	encCfg := &encoder.EncConfig{
		Inf:                   inf,
		CRF:                   cfg.CRF,
		Preset:                cfg.Preset,
		Tune:                  cfg.Tune,
		Output:                outputPath,
		GrainTable:            cfg.GrainTable,
		Width:                 width,
		Height:                height,
		Frames:                frameCount,
		ACBias:                cfg.ACBias,
		EnableVarianceBoost:   cfg.EnableVarianceBoost,
		VarianceBoostStrength: cfg.VarianceBoostStrength,
		VarianceOctile:        cfg.VarianceOctile,
		LogicalProcessors:     cfg.LogicalProcessors,
	}

	cmd := encoder.MakeSvtCmd(encCfg)

	// Setup stdin pipe
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return worker.EncodeResult{
			ChunkIdx: ch.Idx,
			Error:    fmt.Errorf("failed to create stdin pipe: %w", err),
		}
	}

	// Start encoder
	if err := cmd.Start(); err != nil {
		return worker.EncodeResult{
			ChunkIdx: ch.Idx,
			Error:    fmt.Errorf("failed to start encoder: %w", err),
		}
	}

	// Stream frames one at a time: decode -> write to encoder -> repeat
	var writeErr error
	for i := 0; i < frameCount; i++ {
		// Check for cancellation
		if ctx.Err() != nil {
			_ = stdin.Close()
			_ = cmd.Wait()
			return worker.EncodeResult{
				ChunkIdx: ch.Idx,
				Error:    ctx.Err(),
			}
		}

		// Decode frame into reusable buffer
		frameIdx := ch.Start + i
		if err := ffms.ExtractFrame(src, frameIdx, frameBuf, inf, strat, cropCalc); err != nil {
			_ = stdin.Close()
			_ = cmd.Wait()
			return worker.EncodeResult{
				ChunkIdx: ch.Idx,
				Error:    fmt.Errorf("failed to extract frame %d: %w", frameIdx, err),
			}
		}

		// Write frame to encoder stdin
		_, writeErr = stdin.Write(frameBuf)
		if writeErr != nil {
			break
		}
	}

	_ = stdin.Close()

	if writeErr != nil {
		_ = cmd.Wait()
		return worker.EncodeResult{
			ChunkIdx: ch.Idx,
			Error:    fmt.Errorf("failed to write frame data: %w", writeErr),
		}
	}

	// Wait for encoder to finish
	if err := cmd.Wait(); err != nil {
		return worker.EncodeResult{
			ChunkIdx: ch.Idx,
			Error:    fmt.Errorf("encoder failed: %w", err),
		}
	}

	// Get output file size
	stat, err := os.Stat(outputPath)
	if err != nil {
		return worker.EncodeResult{
			ChunkIdx: ch.Idx,
			Error:    fmt.Errorf("failed to stat output: %w", err),
		}
	}

	return worker.EncodeResult{
		ChunkIdx: ch.Idx,
		Frames:   frameCount,
		Size:     uint64(stat.Size()),
	}
}

// calculateThreadsPerWorker determines optimal threads per worker based on CPU topology and resolution.
// Uses physical cores as the base and adds an SMT bonus when hyperthreading is available.
// Resolution affects max threads: larger frames parallelize better in SVT-AV1.
func calculateThreadsPerWorker(workers int, width uint32) int {
	if workers <= 0 {
		return 1
	}

	physical := util.PhysicalCores()
	logical := util.LogicalCores()
	hasSMT := logical > physical

	// Resolution-based max threads (SVT-AV1 scaling limits)
	var maxThreads int
	switch {
	case width >= 3840: // 4K - larger frames parallelize better
		maxThreads = 16
	case width >= 1920: // 1080p
		maxThreads = 10
	default: // SD/720p
		maxThreads = 6
	}

	// Base calculation on physical cores
	threadsPerWorker := physical / workers

	// Add SMT bonus (hyperthreads provide ~20% additional throughput)
	if hasSMT && threadsPerWorker < maxThreads {
		threadsPerWorker++
	}

	return max(1, min(threadsPerWorker, maxThreads))
}
