// Package processing provides video processing orchestration.
package processing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	nativeaudio "codeberg.org/five82/reel/internal/audio"
	"codeberg.org/five82/reel/internal/chunk"
	"codeberg.org/five82/reel/internal/config"
	"codeberg.org/five82/reel/internal/encode"
	"codeberg.org/five82/reel/internal/keyframe"
	"codeberg.org/five82/reel/internal/media"
	"codeberg.org/five82/reel/internal/reporter"
	"codeberg.org/five82/reel/internal/video"
	"codeberg.org/five82/reel/internal/worker"
)

// ProcessChunked runs the chunked encoding pipeline for a single file.
// Returns the crop result so the caller can use it for validation.
func ProcessChunked(
	ctx context.Context,
	cfg *config.Config,
	inputPath, outputPath string,
	videoProps *media.VideoProperties,
	audioStreams []media.AudioStreamInfo,
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
	// PHASE 1: Probe video and run luma-based crop detection
	// ========================================================================
	rep.StageProgress(reporter.StageProgress{Stage: "Preparing", Message: "Analyzing video and detecting crop"})

	vidInf, err := video.Probe(inputPath)
	if err != nil {
		return CropResult{}, fmt.Errorf("failed to probe video: %w", err)
	}

	cropResult := DetectCrop(inputPath, vidInf, cfg.CropMode == "none")

	// Report crop detection result
	rep.CropResult(reporter.CropSummary{
		Message:  cropResult.Message,
		Crop:     cropResult.CropFilter,
		Required: cropResult.Required,
		Disabled: cfg.CropMode == "none",
	})

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

	// Convert crop filter to an exact source rectangle.
	var cropRect *video.CropRect
	if cropResult.Required && cropResult.CropFilter != "" {
		cropRect, err = parseCropFilter(cropResult.CropFilter, videoProps.Width, videoProps.Height)
		if err != nil {
			return CropResult{}, fmt.Errorf("invalid crop filter %q: %w", cropResult.CropFilter, err)
		}
		rep.Verbose(fmt.Sprintf("Crop rectangle: %dx%d at +%d+%d", cropRect.Width, cropRect.Height, cropRect.X, cropRect.Y))
	}

	// Setup encode config
	encCfg := &encode.EncodeConfig{
		CRF:                   float32(quality),
		Preset:                cfg.SVTAV1Preset,
		Tune:                  cfg.SVTAV1Tune,
		DecodeMode:            video.DecodeMode(cfg.DecodeMode),
		ACBias:                cfg.SVTAV1ACBias,
		EnableVarianceBoost:   cfg.SVTAV1EnableVarianceBoost,
		VarianceBoostStrength: cfg.SVTAV1VarianceBoostStrength,
		VarianceOctile:        cfg.SVTAV1VarianceOctile,
	}
	rep.Verbose(fmt.Sprintf("Video decode mode: %s", cfg.DecodeMode))
	if cfg.Verbose {
		encCfg.StatusCallback = func(message string) {
			rep.Verbose(message)
		}
	}

	manifest, err := buildResumeManifest(inputPath, vidInf, cfg, chunks, cropResult.CropFilter, chunkDuration, quality)
	if err != nil {
		return CropResult{}, err
	}
	if err := chunk.EnsureResumeManifest(workDir, manifest); err != nil {
		return CropResult{}, err
	}
	resumeInfo, err := chunk.GetResume(workDir)
	if err != nil {
		return CropResult{}, fmt.Errorf("failed to load resume info: %w", err)
	}
	resumedFrames := resumeInfo.Validate(workDir, chunks).TotalEncodedFrames()

	// Adaptive encoding starts conservatively, tests higher worker counts by
	// throughput, and backs off on real RAM/swap pressure.
	maxWorkers := encode.MaxAdaptiveWorkers()
	rep.StageProgress(reporter.StageProgress{
		Stage:   "Encoding",
		Message: fmt.Sprintf("Starting adaptive chunked encoding with up to %d workers", maxWorkers),
	})

	rep.EncodingStarted(uint64(vidInf.Frames))

	startTime := time.Now()
	type speedSample struct {
		at     time.Time
		frames int
	}
	var speedMu sync.Mutex
	var speedSamples []speedSample
	const recentSpeedWindow = 60 * time.Second

	progressCallback := func(progress worker.Progress) {
		// Calculate average speed, recent rolling speed, and ETA.
		elapsed := time.Since(startTime)
		var speed float32
		var recentSpeed float32
		var eta time.Duration

		runFramesComplete := progress.FramesComplete - resumedFrames
		if runFramesComplete < 0 {
			runFramesComplete = 0
		}
		if elapsed.Seconds() > 0 && runFramesComplete > 0 {
			// Speed is based only on frames encoded in this run. Resume frames count
			// toward percent complete, but not toward current encode speed/ETA.
			videoSeconds := float64(runFramesComplete) / fps
			speed = float32(videoSeconds / elapsed.Seconds())

			speedMu.Lock()
			now := time.Now()
			speedSamples = append(speedSamples, speedSample{at: now, frames: runFramesComplete})
			cutoff := now.Add(-recentSpeedWindow)
			first := 0
			for first < len(speedSamples)-1 && speedSamples[first].at.Before(cutoff) {
				first++
			}
			if first > 0 {
				speedSamples = speedSamples[first:]
			}
			if len(speedSamples) >= 2 {
				oldest := speedSamples[0]
				newest := speedSamples[len(speedSamples)-1]
				framesDelta := newest.frames - oldest.frames
				secondsDelta := newest.at.Sub(oldest.at).Seconds()
				if framesDelta > 0 && secondsDelta > 0 {
					recentSpeed = float32((float64(framesDelta) / fps) / secondsDelta)
				}
			}
			speedMu.Unlock()

			if recentSpeed == 0 {
				recentSpeed = speed
			}

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
			RecentSpeed:    recentSpeed,
			ETA:            eta,
			ChunksComplete: progress.ChunksComplete,
			ChunksTotal:    progress.ChunksTotal,
			ActiveWorkers:  progress.ActiveWorkers,
			TargetWorkers:  progress.TargetWorkers,
			MaxWorkers:     progress.MaxWorkers,
		})
	}

	// ========================================================================
	// PHASE 2: Run video encoding and audio encoding in parallel
	// ========================================================================
	encodeCtx, cancelEncode := context.WithCancel(ctx)
	defer cancelEncode()

	var audioErr error
	var encodedAudio []nativeaudio.EncodedStream
	audioDone := make(chan struct{})

	// Start audio encoding in background (only reads source file)
	if len(audioStreams) > 0 {
		go func() {
			defer close(audioDone)
			encodedAudio, audioErr = chunk.ExtractAudio(encodeCtx, inputPath, workDir, audioStreams)
			if audioErr != nil {
				cancelEncode()
			}
		}()
	} else {
		close(audioDone)
	}

	// Run parallel video encode
	_, encodeErr := encode.EncodeAll(
		encodeCtx,
		chunks,
		inputPath,
		vidInf,
		encCfg,
		workDir,
		cropRect,
		progressCallback,
	)

	if encodeErr != nil {
		// Stop audio when video fails so cancellation/memory pressure returns promptly.
		cancelEncode()
		<-audioDone
		// If the context was canceled (e.g., user pressed Ctrl+C), return that directly
		// so the caller can handle it gracefully instead of reporting it as an encoding error.
		if ctx.Err() != nil {
			return CropResult{}, ctx.Err()
		}
		if errors.Is(encodeErr, encode.ErrMemoryPressure) {
			return CropResult{}, fmt.Errorf("chunked encoding failed: %w", encodeErr)
		}
		if audioErr != nil {
			return CropResult{}, fmt.Errorf("audio encoding failed: %w", audioErr)
		}
		return CropResult{}, fmt.Errorf("chunked encoding failed: %w", encodeErr)
	}

	// Merge IVF files
	rep.StageProgress(reporter.StageProgress{Stage: "Merging", Message: "Merging encoded chunks"})
	if err := chunk.MergeOutput(workDir, vidInf, len(chunks)); err != nil {
		<-audioDone
		return CropResult{}, fmt.Errorf("video merge failed: %w", err)
	}

	displayAspect := displayAspectAfterCrop(videoProps, vidInf, cropRect)

	// Wait for audio encoding to complete
	<-audioDone
	if audioErr != nil {
		// If the context was canceled, report cancellation instead of audio error
		if ctx.Err() != nil {
			return CropResult{}, ctx.Err()
		}
		return CropResult{}, fmt.Errorf("audio encoding failed: %w", audioErr)
	}

	// Final mux
	rep.StageProgress(reporter.StageProgress{Stage: "Muxing", Message: "Creating final output"})
	if err := chunk.MuxFinal(inputPath, workDir, outputPath, encodedAudio, displayAspect); err != nil {
		return CropResult{}, fmt.Errorf("final mux failed: %w", err)
	}

	return cropResult, nil
}

// displayAspectAfterCrop returns the display aspect ratio to signal after cropping anamorphic sources.
func displayAspectAfterCrop(props *media.VideoProperties, inf *video.Info, cropRect *video.CropRect) string {
	width, height, sarNum, sarDen := aspectInputs(props, inf)
	if sarNum == 0 || sarDen == 0 || sarNum == sarDen {
		return ""
	}

	if cropRect != nil {
		width = cropRect.Width
		height = cropRect.Height
	}
	if width == 0 || height == 0 {
		return ""
	}

	num := uint64(width) * uint64(sarNum)
	den := uint64(height) * uint64(sarDen)
	g := gcd(num, den)
	return fmt.Sprintf("%d:%d", num/g, den/g)
}

func aspectInputs(props *media.VideoProperties, inf *video.Info) (width, height, sarNum, sarDen uint32) {
	if props != nil {
		width = props.Width
		height = props.Height
		sarNum = props.SampleAspectRatioNum
		sarDen = props.SampleAspectRatioDen
	}
	if inf != nil {
		if width == 0 {
			width = inf.Width
		}
		if height == 0 {
			height = inf.Height
		}
		if sarNum == 0 || sarDen == 0 {
			sarNum = inf.SampleAspectRatioNum
			sarDen = inf.SampleAspectRatioDen
		}
	}
	return width, height, sarNum, sarDen
}

func gcd(a, b uint64) uint64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func buildResumeManifest(
	inputPath string,
	vidInf *video.Info,
	cfg *config.Config,
	chunks []chunk.Chunk,
	cropFilter string,
	chunkDuration float64,
	quality uint32,
) (chunk.ResumeManifest, error) {
	stat, err := os.Stat(inputPath)
	if err != nil {
		return chunk.ResumeManifest{}, fmt.Errorf("failed to stat input for resume manifest: %w", err)
	}
	return chunk.ResumeManifest{
		InputPath:             chunk.CanonicalInputPath(inputPath),
		InputSize:             stat.Size(),
		InputModTimeUnixNano:  stat.ModTime().UnixNano(),
		Width:                 vidInf.Width,
		Height:                vidInf.Height,
		FPSNum:                vidInf.FPSNum,
		FPSDen:                vidInf.FPSDen,
		Frames:                vidInf.Frames,
		CropFilter:            cropFilter,
		DecodeMode:            cfg.DecodeMode,
		Quality:               quality,
		Preset:                cfg.SVTAV1Preset,
		Tune:                  cfg.SVTAV1Tune,
		ACBias:                cfg.SVTAV1ACBias,
		EnableVarianceBoost:   cfg.SVTAV1EnableVarianceBoost,
		VarianceBoostStrength: cfg.SVTAV1VarianceBoostStrength,
		VarianceOctile:        cfg.SVTAV1VarianceOctile,
		ChunkDurationSecs:     chunkDuration,
		ChunkFingerprint:      chunk.ChunkFingerprint(chunks),
	}, nil
}

func parseCropFilter(filter string, srcWidth, srcHeight uint32) (*video.CropRect, error) {
	var w, h, x, y uint32
	_, err := fmt.Sscanf(filter, "crop=%d:%d:%d:%d", &w, &h, &x, &y)
	if err != nil {
		return nil, err
	}
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("width and height must be non-zero")
	}
	if x > srcWidth || w > srcWidth-x || y > srcHeight || h > srcHeight-y {
		return nil, fmt.Errorf("rectangle %dx%d+%d+%d exceeds source %dx%d", w, h, x, y, srcWidth, srcHeight)
	}
	if x%2 != 0 || y%2 != 0 || w%2 != 0 || h%2 != 0 {
		return nil, fmt.Errorf("YUV420 crop offsets and dimensions must be even")
	}

	return &video.CropRect{X: x, Y: y, Width: w, Height: h}, nil
}

// CheckChunkedDependencies verifies that required tools are available.
func CheckChunkedDependencies() error {
	// Check for ffmpeg in PATH (used for chunk merging and final muxing)
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found in PATH (required for muxing)")
	}

	return nil
}
