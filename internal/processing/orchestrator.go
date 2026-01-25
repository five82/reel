package processing

import (
	"context"
	"fmt"
	"time"

	"github.com/five82/reel/internal/config"
	"github.com/five82/reel/internal/encoder"
	"github.com/five82/reel/internal/ffmpeg"
	"github.com/five82/reel/internal/ffprobe"
	"github.com/five82/reel/internal/mediainfo"
	"github.com/five82/reel/internal/reporter"
	"github.com/five82/reel/internal/util"
	"github.com/five82/reel/internal/validation"
)

// EncodeResult contains the result of a single file encode.
type EncodeResult struct {
	Filename          string
	Duration          time.Duration
	InputSize         uint64
	OutputSize        uint64
	VideoDurationSecs float64
	EncodingSpeed     float32
	ValidationPassed  bool
	ValidationSteps   []validation.ValidationStep
}

// ProcessVideos orchestrates encoding for a list of video files.
func ProcessVideos(
	ctx context.Context,
	cfg *config.Config,
	filesToProcess []string,
	targetFilenameOverride string,
	rep reporter.Reporter,
) ([]EncodeResult, error) {
	if rep == nil {
		rep = reporter.NullReporter{}
	}

	var results []EncodeResult

	// Emit hardware information
	sysInfo := util.GetSystemInfo()
	rep.Hardware(reporter.HardwareSummary{
		Hostname: sysInfo.Hostname,
	})

	// Show batch initialization for multiple files
	if len(filesToProcess) > 1 {
		var fileNames []string
		for _, f := range filesToProcess {
			fileNames = append(fileNames, util.GetFilename(f))
		}
		rep.BatchStarted(reporter.BatchStartInfo{
			TotalFiles: len(filesToProcess),
			FileList:   fileNames,
			OutputDir:  cfg.OutputDir,
		})
	}

	for fileIdx, inputPath := range filesToProcess {
		// Check for cancellation before starting each file
		if ctx.Err() != nil {
			rep.Warning(fmt.Sprintf("Encoding cancelled: %v", ctx.Err()))
			break
		}

		fileStartTime := time.Now()

		// Show file progress for multiple files
		if len(filesToProcess) > 1 {
			rep.FileProgress(reporter.FileProgressContext{
				CurrentFile: fileIdx + 1,
				TotalFiles:  len(filesToProcess),
			})
		}

		inputFilename := util.GetFilename(inputPath)

		// Determine output path
		override := ""
		if len(filesToProcess) == 1 && targetFilenameOverride != "" {
			override = targetFilenameOverride
		}
		outputPath := util.ResolveOutputPath(inputPath, cfg.OutputDir, override)

		// Skip if output exists
		if util.FileExists(outputPath) {
			rep.Warning(fmt.Sprintf("Output file already exists: %s. Skipping encode.", outputPath))
			continue
		}

		// Analyze video properties
		videoProps, err := ffprobe.GetVideoProperties(inputPath)
		if err != nil {
			rep.Error(reporter.ReporterError{
				Title:      "Analysis Error",
				Message:    fmt.Sprintf("Could not analyze %s: %v", inputFilename, err),
				Context:    fmt.Sprintf("File: %s", inputPath),
				Suggestion: "Check if the file is a valid video format",
			})
			continue
		}

		// Use mediainfo for HDR detection
		mediaInfoData, err := mediainfo.GetMediaInfo(inputPath)
		if err != nil {
			rep.Error(reporter.ReporterError{
				Title:      "Analysis Error",
				Message:    fmt.Sprintf("Could not get mediainfo for %s: %v", inputFilename, err),
				Context:    fmt.Sprintf("File: %s", inputPath),
				Suggestion: "Check if mediainfo is installed",
			})
			continue
		}
		hdrInfo := mediainfo.DetectHDR(mediaInfoData)

		// Determine quality settings
		quality, _ := determineQualitySettings(videoProps, cfg)
		isHDR := hdrInfo.IsHDR

		// Get audio info
		audioChannels := GetAudioChannels(inputPath)
		audioStreams := GetAudioStreamInfo(inputPath)
		audioDescription := FormatAudioDescription(audioChannels)

		// Emit initialization event
		rep.Initialization(reporter.InitializationSummary{
			InputFile:        inputFilename,
			OutputFile:       util.GetFilename(outputPath),
			Duration:         util.FormatDuration(videoProps.DurationSecs),
			Resolution:       fmt.Sprintf("%dx%d", videoProps.Width, videoProps.Height),
			DynamicRange:     formatDynamicRange(isHDR),
			AudioDescription: audioDescription,
		})

		// Verbose video analysis details
		rep.Verbose(fmt.Sprintf("Video duration: %.2f seconds", videoProps.DurationSecs))
		if isHDR {
			rep.Verbose(fmt.Sprintf("Color primaries: %s, transfer: %s", hdrInfo.ColourPrimaries, hdrInfo.TransferCharacteristics))
		}

		// Setup encode parameters (for display only)
		encodeParams := setupEncodeParams(cfg, quality, hdrInfo)

		// Format audio description for config display
		audioDescConfig := FormatAudioDescriptionConfig(audioChannels, audioStreams)

		// Emit encoding config
		rep.EncodingConfig(reporter.EncodingConfigSummary{
			Encoder:            "SVT-AV1",
			Preset:             fmt.Sprintf("%d", encodeParams.Preset),
			Tune:               fmt.Sprintf("%d", encodeParams.Tune),
			Quality:            formatQualityDescription(videoProps.Width, encodeParams.Quality),
			PixelFormat:        encodeParams.PixelFormat,
			MatrixCoefficients: encodeParams.MatrixCoefficients,
			AudioCodec:         "Opus",
			AudioDescription:   audioDescConfig,
			SVTAV1Params:       encoder.SvtParamsDisplay(cfg.SVTAV1ACBias, cfg.SVTAV1EnableVarianceBoost, cfg.SVTAV1Tune),
		})

		// Run chunked encoding with FFMS2 + SvtAv1EncApp
		cropResult, encodeError := ProcessChunked(ctx, cfg, inputPath, outputPath, videoProps, audioStreams, quality, rep)
		encodeSuccess := encodeError == nil

		if !encodeSuccess {
			rep.Error(reporter.ReporterError{
				Title:      "Encoding Error",
				Message:    fmt.Sprintf("Failed to encode %s: %v", inputFilename, encodeError),
				Context:    fmt.Sprintf("File: %s", inputPath),
				Suggestion: "Check logs for more details",
			})
			continue
		}

		fileElapsedTime := time.Since(fileStartTime)

		inputSize, _ := util.GetFileSize(inputPath)
		outputSize, _ := util.GetFileSize(outputPath)
		encodingSpeed := float32(videoProps.DurationSecs) / float32(fileElapsedTime.Seconds())

		// Calculate expected dimensions after crop
		expectedWidth, expectedHeight := GetOutputDimensions(videoProps.Width, videoProps.Height, cropResult.CropFilter)

		// Validate output
		expectedDims := &[2]uint32{expectedWidth, expectedHeight}
		expectedDuration := videoProps.DurationSecs
		expectedAudioTracks := len(audioChannels)

		validationResult, err := validation.ValidateOutputVideo(inputPath, outputPath, validation.Options{
			ExpectedDimensions:  expectedDims,
			ExpectedDuration:    &expectedDuration,
			ExpectedHDR:         &isHDR,
			ExpectedAudioTracks: &expectedAudioTracks,
		})

		var validationPassed bool
		var validationSteps []validation.ValidationStep
		if err != nil {
			validationPassed = false
			validationSteps = []validation.ValidationStep{
				{Name: "Validation", Passed: false, Details: err.Error()},
			}
		} else {
			validationPassed = validationResult.IsValid()
			for _, step := range validationResult.GetValidationSteps() {
				validationSteps = append(validationSteps, validation.ValidationStep{
					Name:    step.Name,
					Passed:  step.Passed,
					Details: step.Details,
				})
			}
		}

		results = append(results, EncodeResult{
			Filename:          inputFilename,
			Duration:          fileElapsedTime,
			InputSize:         inputSize,
			OutputSize:        outputSize,
			VideoDurationSecs: videoProps.DurationSecs,
			EncodingSpeed:     encodingSpeed,
			ValidationPassed:  validationPassed,
			ValidationSteps:   validationSteps,
		})

		// Emit validation complete
		var repSteps []reporter.ValidationStep
		for _, s := range validationSteps {
			repSteps = append(repSteps, reporter.ValidationStep{
				Name:    s.Name,
				Passed:  s.Passed,
				Details: s.Details,
			})
		}
		rep.ValidationComplete(reporter.ValidationSummary{
			Passed: validationPassed,
			Steps:  repSteps,
		})

		// Emit encoding complete
		rep.EncodingComplete(reporter.EncodingOutcome{
			InputFile:    inputFilename,
			OutputFile:   util.GetFilename(outputPath),
			OriginalSize: inputSize,
			EncodedSize:  outputSize,
			VideoStream:  fmt.Sprintf("AV1 (libsvtav1), %dx%d", expectedWidth, expectedHeight),
			AudioStream:  GenerateAudioResultsDescription(audioChannels, audioStreams),
			TotalTime:    fileElapsedTime,
			AverageSpeed: encodingSpeed,
			OutputPath:   outputPath,
		})

		// Cooldown between encodes
		if len(filesToProcess) > 1 && fileIdx < len(filesToProcess)-1 && cfg.EncodeCooldownSecs > 0 {
			time.Sleep(time.Duration(cfg.EncodeCooldownSecs) * time.Second)
		}
	}

	// Generate summary
	switch len(results) {
	case 0:
		rep.Warning("No files were successfully encoded")
	case 1:
		rep.OperationComplete(fmt.Sprintf("Successfully encoded %s", results[0].Filename))
	default:
		// Calculate totals
		var totalDuration time.Duration
		var totalOriginalSize, totalEncodedSize uint64
		var totalVideoDuration float64
		var fileResults []reporter.FileResult
		validationPassedCount := 0

		for _, r := range results {
			totalDuration += r.Duration
			totalOriginalSize += r.InputSize
			totalEncodedSize += r.OutputSize
			totalVideoDuration += r.VideoDurationSecs
			reduction := util.CalculateSizeReduction(r.InputSize, r.OutputSize)
			fileResults = append(fileResults, reporter.FileResult{
				Filename:  r.Filename,
				Reduction: reduction,
			})
			if r.ValidationPassed {
				validationPassedCount++
			}
		}

		avgSpeed := float32(0)
		if totalDuration.Seconds() > 0 {
			avgSpeed = float32(totalVideoDuration / totalDuration.Seconds())
		}

		rep.BatchComplete(reporter.BatchSummary{
			SuccessfulCount:       len(results),
			TotalFiles:            len(filesToProcess),
			TotalOriginalSize:     totalOriginalSize,
			TotalEncodedSize:      totalEncodedSize,
			TotalDuration:         totalDuration,
			AverageSpeed:          avgSpeed,
			FileResults:           fileResults,
			ValidationPassedCount: validationPassedCount,
			ValidationFailedCount: len(results) - validationPassedCount,
		})
	}

	return results, nil
}

// determineQualitySettings returns the CRF quality setting based on video resolution.
func determineQualitySettings(props *ffprobe.VideoProperties, cfg *config.Config) (uint32, string) {
	crf := cfg.CRFForWidth(props.Width)
	return uint32(crf), ""
}

func formatDynamicRange(isHDR bool) string {
	if isHDR {
		return "HDR"
	}
	return "SDR"
}

func formatQualityDescription(width uint32, crf uint32) string {
	var tier string
	if width >= config.UHDWidthThreshold {
		tier = "UHD"
	} else if width >= config.HDWidthThreshold {
		tier = "HD"
	} else {
		tier = "SD"
	}
	return fmt.Sprintf("CRF %d (%s)", crf, tier)
}

func setupEncodeParams(
	cfg *config.Config,
	quality uint32,
	hdrInfo mediainfo.HDRInfo,
) *ffmpeg.EncodeParams {
	params := &ffmpeg.EncodeParams{
		Quality:     quality,
		Preset:      cfg.SVTAV1Preset,
		Tune:        cfg.SVTAV1Tune,
		PixelFormat: "yuv420p10le",
	}

	// Set matrix coefficients based on HDR
	if hdrInfo.IsHDR {
		params.MatrixCoefficients = hdrInfo.MatrixCoefficients
		if params.MatrixCoefficients == "" {
			params.MatrixCoefficients = "bt2020nc"
		}
	} else {
		params.MatrixCoefficients = "bt709"
	}

	return params
}
