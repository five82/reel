// Package processing provides video processing orchestration.
package processing

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/five82/reel/internal/chunk"
	"github.com/five82/reel/internal/config"
	"github.com/five82/reel/internal/encode"
	"github.com/five82/reel/internal/ffms"
	"github.com/five82/reel/internal/ffprobe"
	"github.com/five82/reel/internal/keyframe"
	"github.com/five82/reel/internal/reporter"
	"github.com/five82/reel/internal/worker"
)

// ProcessChunked runs the chunked encoding pipeline for a single file.
// Returns the crop result so the caller can use it for validation.
func ProcessChunked(
	ctx context.Context,
	cfg *config.Config,
	inputPath, outputPath string,
	videoProps *ffprobe.VideoProperties,
	audioStreams []ffprobe.AudioStreamInfo,
	quality uint32,
	rep reporter.Reporter,
) (CropResult, error) {
	// Create work directory
	workDir := chunk.GetWorkDirPath(inputPath, cfg.GetTempDir())
	if err := chunk.CreateWorkDir(workDir); err != nil {
		return CropResult{}, fmt.Errorf("failed to create work directory: %w", err)
	}

	// Cleanup on completion (unless resuming a failed encode)
	defer func() {
		// Only cleanup if output was successfully created
		if _, err := os.Stat(outputPath); err == nil {
			_ = chunk.CleanupWorkDir(workDir)
		}
	}()

	// ========================================================================
	// PHASE 1: Run FFMS2 indexing and crop detection in parallel
	// ========================================================================
	rep.StageProgress(reporter.StageProgress{Stage: "Preparing", Message: "Indexing video and detecting crop"})

	var idx *ffms.VidIdx
	var cropResult CropResult

	phase1, _ := errgroup.WithContext(ctx)

	// FFMS2 indexing goroutine
	phase1.Go(func() error {
		var err error
		idx, err = ffms.NewVidIdx(inputPath, true)
		if err != nil {
			return fmt.Errorf("failed to create video index: %w", err)
		}
		return nil
	})

	// Crop detection goroutine
	phase1.Go(func() error {
		cropResult = DetectCrop(inputPath, videoProps, cfg.CropMode == "none")
		return nil
	})

	// Wait for phase 1 to complete
	if err := phase1.Wait(); err != nil {
		if idx != nil {
			idx.Close()
		}
		return CropResult{}, err
	}
	defer idx.Close()

	// Report crop detection result
	rep.CropResult(reporter.CropSummary{
		Message:  cropResult.Message,
		Crop:     cropResult.CropFilter,
		Required: cropResult.Required,
		Disabled: cfg.CropMode == "none",
	})

	// Get video info (needs index)
	vidInf, err := ffms.GetVidInf(idx)
	if err != nil {
		return CropResult{}, fmt.Errorf("failed to get video info: %w", err)
	}

	// Generate fixed-length chunks based on resolution (using config values)
	chunkDuration := cfg.ChunkDurationForWidth(vidInf.Width)
	rep.StageProgress(reporter.StageProgress{Stage: "Chunking", Message: fmt.Sprintf("Creating %.0fs chunks", chunkDuration)})
	sceneFile, err := keyframe.ExtractKeyframesIfNeeded(
		inputPath,
		workDir,
		vidInf.FPSNum,
		vidInf.FPSDen,
		vidInf.Frames,
		chunkDuration,
	)
	if err != nil {
		return CropResult{}, fmt.Errorf("chunk generation failed: %w", err)
	}

	// Load scenes
	scenes, err := chunk.LoadScenes(sceneFile, vidInf.Frames)
	if err != nil {
		return CropResult{}, fmt.Errorf("failed to load scenes: %w", err)
	}
	rep.Verbose(fmt.Sprintf("Created %d chunks", len(scenes)))

	// Convert scenes to chunks
	chunks := chunk.Chunkify(scenes)
	rep.StageProgress(reporter.StageProgress{Stage: "Chunking", Message: fmt.Sprintf("Split video into %d chunks", len(chunks))})

	// Calculate average chunk duration for verbose output
	fps := float64(vidInf.FPSNum) / float64(vidInf.FPSDen)
	totalFrames := 0
	for _, c := range chunks {
		totalFrames += int(c.End - c.Start)
	}
	avgChunkFrames := float64(totalFrames) / float64(len(chunks))
	avgChunkDuration := avgChunkFrames / fps
	rep.Verbose(fmt.Sprintf("Average chunk duration: %.1fs (%d frames)", avgChunkDuration, int(avgChunkFrames)))

	// Convert crop filter to cropH/cropV
	var cropH, cropV uint32
	if cropResult.Required && cropResult.CropFilter != "" {
		cropH, cropV = parseCropFilter(cropResult.CropFilter, videoProps.Width, videoProps.Height)
		rep.Verbose(fmt.Sprintf("Crop offsets: horizontal %d, vertical %d", cropH, cropV))
	}

	// Setup encode config
	encCfg := &encode.EncodeConfig{
		Workers:               cfg.Workers,
		ChunkBuffer:           cfg.ChunkBuffer,
		CRF:                   float32(quality),
		Preset:                cfg.SVTAV1Preset,
		Tune:                  cfg.SVTAV1Tune,
		ACBias:                cfg.SVTAV1ACBias,
		EnableVarianceBoost:   cfg.SVTAV1EnableVarianceBoost,
		VarianceBoostStrength: cfg.SVTAV1VarianceBoostStrength,
		VarianceOctile:        cfg.SVTAV1VarianceOctile,
		LogicalProcessors:     cfg.ThreadsPerWorker,
	}

	// Calculate actual workers (may be capped based on resolution and memory)
	actualWorkers, wasCapped := encode.CapWorkers(cfg.Workers, vidInf.Width, vidInf.Height)

	// Show both requested and actual worker counts
	var workerMsg string
	if wasCapped {
		workerMsg = fmt.Sprintf("Starting chunked encoding with %d/%d workers (memory limited)", actualWorkers, cfg.Workers)
	} else {
		workerMsg = fmt.Sprintf("Starting chunked encoding with %d workers", actualWorkers)
	}
	rep.StageProgress(reporter.StageProgress{Stage: "Encoding", Message: workerMsg})

	rep.EncodingStarted(uint64(vidInf.Frames))

	startTime := time.Now()

	progressCallback := func(progress worker.Progress) {
		// Calculate speed and ETA
		elapsed := time.Since(startTime)
		var speed float32
		var eta time.Duration

		if elapsed.Seconds() > 0 && progress.FramesComplete > 0 {
			// Video seconds encoded
			videoSeconds := float64(progress.FramesComplete) / fps
			// Speed = video seconds per real second
			speed = float32(videoSeconds / elapsed.Seconds())

			// ETA based on remaining frames
			if speed > 0 {
				remainingFrames := progress.FramesTotal - progress.FramesComplete
				remainingVideoSeconds := float64(remainingFrames) / fps
				eta = time.Duration(remainingVideoSeconds/float64(speed)) * time.Second
			}
		}

		rep.EncodingProgress(reporter.ProgressSnapshot{
			CurrentFrame:   uint64(progress.FramesComplete),
			TotalFrames:    uint64(progress.FramesTotal),
			Percent:        float32(progress.Percent()),
			Speed:          speed,
			ETA:            eta,
			ChunksComplete: progress.ChunksComplete,
			ChunksTotal:    progress.ChunksTotal,
		})
	}

	// ========================================================================
	// PHASE 2: Run video encoding and audio extraction in parallel
	// ========================================================================
	var audioErr error
	audioDone := make(chan struct{})

	// Start audio extraction in background (only reads source file)
	if len(audioStreams) > 0 {
		go func() {
			defer close(audioDone)
			audioErr = chunk.ExtractAudio(inputPath, workDir, audioStreams)
		}()
	} else {
		close(audioDone)
	}

	// Run parallel video encode
	_, encodeErr := encode.EncodeAll(
		ctx,
		chunks,
		vidInf,
		encCfg,
		idx,
		workDir,
		cropH,
		cropV,
		progressCallback,
	)

	if encodeErr != nil {
		// Wait for audio to finish before returning
		<-audioDone
		return CropResult{}, fmt.Errorf("chunked encoding failed: %w", encodeErr)
	}

	// Merge IVF files
	rep.StageProgress(reporter.StageProgress{Stage: "Merging", Message: "Merging encoded chunks"})
	if len(chunks) > 500 {
		// Use batched merge for large number of chunks
		if err := chunk.MergeBatched(workDir, len(chunks)); err != nil {
			<-audioDone
			return CropResult{}, fmt.Errorf("batched merge failed: %w", err)
		}
	}

	if err := chunk.MergeOutput(workDir, outputPath, vidInf, inputPath); err != nil {
		<-audioDone
		return CropResult{}, fmt.Errorf("video merge failed: %w", err)
	}

	// Wait for audio extraction to complete
	<-audioDone
	if audioErr != nil {
		return CropResult{}, fmt.Errorf("audio extraction failed: %w", audioErr)
	}

	// Final mux
	rep.StageProgress(reporter.StageProgress{Stage: "Muxing", Message: "Creating final output"})
	if err := chunk.MuxFinal(inputPath, workDir, outputPath, audioStreams); err != nil {
		return CropResult{}, fmt.Errorf("final mux failed: %w", err)
	}

	return cropResult, nil
}

// parseCropFilter extracts cropH and cropV from a crop filter string.
// Format: "crop=W:H:X:Y" where X is left offset and Y is top offset.
func parseCropFilter(filter string, srcWidth, srcHeight uint32) (cropH, cropV uint32) {
	// Parse "crop=W:H:X:Y"
	var w, h, x, y uint32
	_, err := fmt.Sscanf(filter, "crop=%d:%d:%d:%d", &w, &h, &x, &y)
	if err != nil {
		return 0, 0
	}

	// cropH = X (horizontal offset from left)
	// cropV = Y (vertical offset from top)
	// These represent how many pixels are cropped from each side
	cropH = x
	cropV = y

	return cropH, cropV
}

// CheckChunkedDependencies verifies that required tools are available.
func CheckChunkedDependencies() error {
	// Check for SvtAv1EncApp in PATH
	if _, err := exec.LookPath("SvtAv1EncApp"); err != nil {
		return fmt.Errorf("SvtAv1EncApp not found in PATH (required for encoding)")
	}

	// Check for ffmpeg in PATH (used for audio extraction)
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found in PATH (required for audio extraction)")
	}

	return nil
}
