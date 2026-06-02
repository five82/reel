// Package processing provides video processing orchestration.
package processing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	nativeaudio "codeberg.org/five82/reel/internal/audio"
	"codeberg.org/five82/reel/internal/chunk"
	"codeberg.org/five82/reel/internal/chunkplan"
	"codeberg.org/five82/reel/internal/config"
	"codeberg.org/five82/reel/internal/encode"
	"codeberg.org/five82/reel/internal/media"
	"codeberg.org/five82/reel/internal/quality"
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
	qualitySetting float32,
	rep reporter.Reporter,
) (CropResult, error) {
	if cfg.QualityMode == config.QualityModeTarget && !quality.VshipBuildEnabled() {
		return CropResult{}, fmt.Errorf("target-quality mode is not available in this build; rebuild without -tags no_vship and with libvship installed, or use --quality-mode crf")
	}

	// Create work directory
	workDir := chunk.GetWorkDirPath(inputPath, cfg.GetTempDir())
	if err := chunk.CreateWorkDir(workDir); err != nil {
		return CropResult{}, fmt.Errorf("failed to create work directory: %w", err)
	}

	if cfg.KeepWorkDir {
		rep.Verbose(fmt.Sprintf("Work directory: %s", workDir))
	}

	// Cleanup on completion (unless resuming a failed encode or explicitly keeping the work directory).
	defer func() {
		// Only cleanup if output was successfully created.
		if cfg.KeepWorkDir {
			return
		}
		if _, err := os.Stat(outputPath); err == nil {
			_ = chunk.CleanupWorkDir(workDir)
		}
	}()

	// ========================================================================
	// PHASE 1: Probe video and run luma-based crop detection
	// ========================================================================
	rep.StageProgress(reporter.StageProgress{Stage: "Preparing", Message: "Analyzing video and detecting crop"})

	finishStep := startVerboseStep(rep, "Video probe")
	vidInf, err := video.Probe(inputPath)
	finishStep()
	if err != nil {
		return CropResult{}, fmt.Errorf("failed to probe video: %w", err)
	}

	finishStep = startVerboseStep(rep, "Crop detection")
	cropResult := DetectCrop(inputPath, vidInf, cfg.CropMode == "none")
	finishStep()

	// Report crop detection result
	rep.CropResult(reporter.CropSummary{
		Message:  cropResult.Message,
		Crop:     cropResult.CropFilter,
		Required: cropResult.Required,
		Disabled: cfg.CropMode == "none",
	})

	// Convert crop filter to an exact source rectangle for shot detection and encoding.
	var cropRect *video.CropRect
	if cropResult.Required && cropResult.CropFilter != "" {
		cropRect, err = parseCropFilter(cropResult.CropFilter, videoProps.Width, videoProps.Height)
		if err != nil {
			return CropResult{}, fmt.Errorf("invalid crop filter %q: %w", cropResult.CropFilter, err)
		}
		rep.Verbose(fmt.Sprintf("Crop rectangle: %dx%d at +%d+%d", cropRect.Width, cropRect.Height, cropRect.X, cropRect.Y))
	}

	// Generate shot-aware chunks based on resolution (using config values).
	chunkDuration := cfg.ChunkDurationForWidth(vidInf.Width)
	fps := float64(vidInf.FPSNum) / float64(vidInf.FPSDen)
	maxChunkFrames := int(math.Ceil(fps * chunkDuration))
	if maxChunkFrames < 1 {
		maxChunkFrames = 1
	}
	minChunkFrames := int(math.Ceil(fps * 2))
	if minChunkFrames < 1 {
		minChunkFrames = 1
	}
	targetChunkFrames := 0
	if cfg.QualityMode == config.QualityModeTarget {
		targetChunkFrames = int(math.Ceil(fps * 6))
	}
	rep.StageProgress(reporter.StageProgress{Stage: "Chunking", Message: "Detecting shot cuts"})
	chunkPlanFile := filepath.Join(workDir, "chunk-plan.txt")
	chunkPlanMetadataFile := filepath.Join(workDir, "chunk-plan.json")
	lastPlanProgress := time.Now().Add(-10 * time.Second)
	lastPlanPercent := -1
	finishStep = startVerboseStep(rep, "Shot cut detection")
	planResult, err := chunkplan.PlanToFileIfNeeded(ctx, inputPath, chunkPlanFile, chunkPlanMetadataFile, vidInf, chunkplan.Options{
		MaxFrames:    maxChunkFrames,
		MinFrames:    minChunkFrames,
		TargetFrames: targetChunkFrames,
		CropRect:     cropRect,
		Progress: func(current, total int) {
			if total <= 0 {
				return
			}
			percent := int(float64(current) * 100 / float64(total))
			now := time.Now()
			if current == total || percent >= lastPlanPercent+5 || now.Sub(lastPlanProgress) >= 10*time.Second {
				rep.Verbose(fmt.Sprintf("Shot cut detection progress: %d%% (%d/%d frames)", percent, current, total))
				lastPlanPercent = percent
				lastPlanProgress = now
			}
		},
	})
	finishStep()
	if err != nil {
		return CropResult{}, fmt.Errorf("shot cut detection failed: %w", err)
	}
	if planResult.Frames > 0 && planResult.Frames != vidInf.Frames {
		rep.Verbose(fmt.Sprintf("Video frame count adjusted after decode: probed %d, decoded %d", vidInf.Frames, planResult.Frames))
		vidInf.Frames = planResult.Frames
	}
	if planResult.MergedWeakCuts > 0 {
		rep.Verbose(fmt.Sprintf("Detected %d natural shot cuts, merged %d short shots, merged %d weak cuts, and added %d duration splits", planResult.NaturalCuts, planResult.MergedShortShots, planResult.MergedWeakCuts, planResult.SyntheticSplits))
	} else {
		rep.Verbose(fmt.Sprintf("Detected %d natural shot cuts, merged %d short shots, and added %d duration splits", planResult.NaturalCuts, planResult.MergedShortShots, planResult.SyntheticSplits))
	}
	retainedNaturalCuts := 0
	for _, kind := range planResult.BoundaryKinds {
		if kind == chunkplan.BoundaryKindNaturalShotCut {
			retainedNaturalCuts++
		}
	}
	rep.Verbose(fmt.Sprintf("Chunk boundaries: %d natural shot cuts, %d duration splits", retainedNaturalCuts, planResult.SyntheticSplits))

	// Load planned chunk boundaries
	finishStep = startVerboseStep(rep, "Chunk planning")
	segments, err := chunk.LoadSegments(chunkPlanFile, vidInf.Frames)
	if err != nil {
		finishStep()
		return CropResult{}, fmt.Errorf("failed to load chunk boundaries: %w", err)
	}
	rep.Verbose(fmt.Sprintf("Created %d content-aware chunks", len(segments)))

	// Convert planned segments to chunks
	chunks := chunk.Chunkify(segments)
	rep.StageProgress(reporter.StageProgress{Stage: "Chunking", Message: fmt.Sprintf("Split video into %d chunks", len(chunks))})

	// Calculate average chunk duration for verbose output
	totalFrames := 0
	for _, c := range chunks {
		totalFrames += int(c.End - c.Start)
	}
	avgChunkFrames := float64(totalFrames) / float64(len(chunks))
	avgChunkDuration := avgChunkFrames / fps
	rep.Verbose(fmt.Sprintf("Average chunk duration: %.1fs (%d frames)", avgChunkDuration, int(avgChunkFrames)))
	rep.Verbose(chunkDistributionSummary(chunks, fps))
	finishStep()

	// Setup encode config
	encCfg := &encode.EncodeConfig{
		CRF:                   qualitySetting,
		Preset:                cfg.SVTAV1Preset,
		Tune:                  cfg.SVTAV1Tune,
		ACBias:                cfg.SVTAV1ACBias,
		EnableVarianceBoost:   cfg.SVTAV1EnableVarianceBoost,
		VarianceBoostStrength: cfg.SVTAV1VarianceBoostStrength,
		VarianceOctile:        cfg.SVTAV1VarianceOctile,
	}
	if cfg.Verbose {
		encCfg.StatusCallback = func(message string) {
			rep.Verbose(message)
		}
	}

	finishStep = startVerboseStep(rep, "Resume setup")
	manifest, err := buildResumeManifest(inputPath, vidInf, cfg, chunks, cropResult.CropFilter, chunkDuration, qualitySetting)
	if err != nil {
		finishStep()
		return CropResult{}, err
	}
	if err := chunk.EnsureResumeManifest(workDir, manifest); err != nil {
		finishStep()
		return CropResult{}, err
	}
	resumeInfo, err := chunk.GetResume(workDir)
	if err != nil {
		finishStep()
		return CropResult{}, fmt.Errorf("failed to load resume info: %w", err)
	}
	finishStep()
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

	finishAudioStep := func() {}

	// Start audio encoding in background (only reads source file)
	if len(audioStreams) > 0 {
		finishAudioStep = startVerboseStep(rep, "Audio extraction")
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
	finishStep = startVerboseStep(rep, "Video encoding")
	var encodeErr error
	if cfg.QualityMode == config.QualityModeTarget {
		displayPath, err := quality.EnsureDisplayModel(workDir, vidInf, cfg.CVVDPDisplay)
		if err != nil {
			finishStep()
			cancelEncode()
			<-audioDone
			finishAudioStep()
			return CropResult{}, err
		}
		rep.Verbose(fmt.Sprintf("Target-quality CVVDP: target %.2f +/- %.2f JOD, CRF range %s, initial CRF %s with adaptive priors, sampled probes 3x%d frames (full <=%d), metric workers %d, display %s", cfg.TargetQualityTarget, cfg.TargetQualityTolerance, cfg.CRFSearchRange, quality.FormatCRF(qualitySetting), encode.DefaultTargetQualitySampleFrames, encode.DefaultTargetQualityFullProbeFrames, cfg.MetricWorkers, displayPath))
		rep.Verbose(cvvdpDisplaySummary(cfg, vidInf))
		_, encodeErr = encode.EncodeTargetQuality(
			encodeCtx,
			chunks,
			inputPath,
			vidInf,
			encCfg,
			workDir,
			cropRect,
			progressCallback,
			encode.TargetQualityConfig{
				Target:          cfg.TargetQualityTarget,
				Tolerance:       cfg.TargetQualityTolerance,
				CRFMin:          cfg.CRFSearchMin,
				CRFMax:          cfg.CRFSearchMax,
				MaxProbes:       cfg.TargetQualityMaxProbes,
				MetricWorkers:   cfg.MetricWorkers,
				DisplayPath:     displayPath,
				InitialCRF:      qualitySetting,
				SampleFrames:    encode.DefaultTargetQualitySampleFrames,
				FullProbeFrames: encode.DefaultTargetQualityFullProbeFrames,
				Verbose: func(message string) {
					rep.Verbose(message)
				},
			},
		)
	} else {
		_, encodeErr = encode.EncodeAll(
			encodeCtx,
			chunks,
			inputPath,
			vidInf,
			encCfg,
			workDir,
			cropRect,
			progressCallback,
		)
	}

	finishStep()

	if encodeErr != nil {
		// Stop audio when video fails so cancellation/memory pressure returns promptly.
		cancelEncode()
		<-audioDone
		finishAudioStep()
		return CropResult{}, encodePipelineError(ctx.Err(), encodeErr, audioErr)
	}

	// Merge IVF files
	rep.StageProgress(reporter.StageProgress{Stage: "Merging", Message: "Merging encoded chunks"})
	finishStep = startVerboseStep(rep, "Video merge")
	if err := chunk.MergeOutput(workDir, vidInf, len(chunks)); err != nil {
		finishStep()
		<-audioDone
		finishAudioStep()
		return CropResult{}, fmt.Errorf("video merge failed: %w", err)
	}
	finishStep()

	displayAspect := displayAspectAfterCrop(videoProps, vidInf, cropRect)

	// Wait for audio encoding to complete
	<-audioDone
	finishAudioStep()
	if audioErr != nil {
		// If the context was canceled, report cancellation instead of audio error
		if ctx.Err() != nil {
			return CropResult{}, ctx.Err()
		}
		return CropResult{}, fmt.Errorf("audio encoding failed: %w", audioErr)
	}

	// Final mux
	rep.StageProgress(reporter.StageProgress{Stage: "Muxing", Message: "Creating final output"})
	finishStep = startVerboseStep(rep, "Final mux")
	if err := chunk.MuxFinal(inputPath, workDir, outputPath, encodedAudio, displayAspect); err != nil {
		finishStep()
		return CropResult{}, fmt.Errorf("final mux failed: %w", err)
	}
	finishStep()

	return cropResult, nil
}

func cvvdpDisplaySummary(cfg *config.Config, inf *video.Info) string {
	if cfg.CVVDPDisplay != "" {
		return fmt.Sprintf("CVVDP display model: model=%s, override=%s", quality.DisplayModelKey, cfg.CVVDPDisplay)
	}
	if inf != nil && inf.TransferCharacteristics != nil && (*inf.TransferCharacteristics == 16 || *inf.TransferCharacteristics == 18) {
		return fmt.Sprintf("CVVDP display model: model=%s, generated HDR normal-viewing model, ppd~75, resolution=3840x2160, peak=1000 nits, contrast=1000000, ambient=10 lux", quality.DisplayModelKey)
	}
	return fmt.Sprintf("CVVDP display model: model=%s, generated SDR normal-viewing model, ppd~75, resolution=3840x2160, peak=200 nits, contrast=1000, ambient=100 lux", quality.DisplayModelKey)
}

func encodePipelineError(parentErr, encodeErr, audioErr error) error {
	// If the parent context was canceled (e.g. user pressed Ctrl+C), return that
	// directly so the caller can handle it gracefully.
	if parentErr != nil {
		return parentErr
	}
	if errors.Is(encodeErr, encode.ErrMemoryPressure) {
		return fmt.Errorf("chunked encoding failed: %w", encodeErr)
	}

	// Audio runs under the same child context as video. When video fails first,
	// Reel cancels that child context and audio often reports only "context canceled".
	// Do not let that cleanup error hide the real video failure.
	if audioErr != nil && errors.Is(encodeErr, context.Canceled) && !errors.Is(audioErr, context.Canceled) {
		return fmt.Errorf("audio encoding failed: %w", audioErr)
	}
	return fmt.Errorf("chunked encoding failed: %w", encodeErr)
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

	aspect := displayAspectRatio(width, height, sarNum, sarDen)
	if aspect == nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", aspect[0], aspect[1])
}

func expectedDisplayAspectAfterCrop(props *media.VideoProperties, width, height uint32) *[2]uint32 {
	if props == nil || props.SampleAspectRatioNum == 0 || props.SampleAspectRatioDen == 0 || props.SampleAspectRatioNum == props.SampleAspectRatioDen {
		return nil
	}
	return displayAspectRatio(width, height, props.SampleAspectRatioNum, props.SampleAspectRatioDen)
}

func displayAspectRatio(width, height, sarNum, sarDen uint32) *[2]uint32 {
	if width == 0 || height == 0 || sarNum == 0 || sarDen == 0 {
		return nil
	}
	num := uint64(width) * uint64(sarNum)
	den := uint64(height) * uint64(sarDen)
	g := gcd(num, den)
	return &[2]uint32{uint32(num / g), uint32(den / g)}
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
	qualitySetting float32,
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
		QualityMode:           cfg.QualityMode,
		Quality:               qualitySetting,
		TargetQuality:         cfg.TargetQuality,
		CRFSearchRange:        cfg.CRFSearchRange,
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
	return nil
}
