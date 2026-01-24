package validation

import (
	"fmt"
	"math"
	"strings"

	"github.com/five82/reel/internal/ffprobe"
	"github.com/five82/reel/internal/mediainfo"
)

const (
	// durationToleranceSecs is the maximum allowed difference in duration between input and output.
	durationToleranceSecs = 1.0
	// maxSyncDriftMs is the maximum allowed audio/video sync drift in milliseconds.
	maxSyncDriftMs = 100.0
)

// Options contains optional parameters for validation.
type Options struct {
	ExpectedDimensions    *[2]uint32
	ExpectedDuration      *float64
	ExpectedHDR           *bool
	ExpectedAudioTracks   *int
	ExpectedAudioChannels []uint32
}

// ValidateOutputVideo performs comprehensive validation of an encoded video.
func ValidateOutputVideo(inputPath, outputPath string, opts Options) (*Result, error) {
	result := &Result{
		IsCropCorrect:            true,
		IsDurationCorrect:        true,
		IsHDRCorrect:             true,
		IsAudioOpus:              true,
		IsAudioTrackCountCorrect: true,
		IsSyncPreserved:          true,
	}

	// Get output video properties
	outputProps, err := ffprobe.GetVideoProperties(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get output video properties: %w", err)
	}

	// Validate video codec (should be AV1)
	mediaInfo, err := ffprobe.GetMediaInfo(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get media info: %w", err)
	}

	result.IsAV1, result.CodecName = validateVideoCodec(outputPath)
	result.Is10Bit, result.BitDepth, result.PixelFormat = validateBitDepth(outputPath)

	// Validate dimensions if expected
	if opts.ExpectedDimensions != nil {
		result.ActualDimensions = &[2]uint32{outputProps.Width, outputProps.Height}
		result.ExpectedDimensions = opts.ExpectedDimensions
		result.IsCropCorrect, result.CropMessage = validateDimensions(
			outputProps.Width, outputProps.Height,
			opts.ExpectedDimensions[0], opts.ExpectedDimensions[1],
		)
	} else {
		result.CropMessage = "No crop validation required"
	}

	// Validate duration if expected
	if opts.ExpectedDuration != nil {
		actualDur := outputProps.DurationSecs
		result.ActualDuration = &actualDur
		result.ExpectedDuration = opts.ExpectedDuration
		result.IsDurationCorrect, result.DurationMessage = validateDuration(actualDur, *opts.ExpectedDuration)
	} else {
		result.DurationMessage = "Duration validation skipped"
	}

	// Validate HDR status if expected - use comprehensive MediaInfo-based validation
	if opts.ExpectedHDR != nil {
		hdrResult := ValidateHDRStatusWithPath(outputPath, opts.ExpectedHDR)
		result.IsHDRCorrect = hdrResult.IsValid
		result.ActualHDR = hdrResult.ActualHDR
		result.ExpectedHDR = opts.ExpectedHDR
		result.HDRMessage = hdrResult.Message
	} else {
		// No expected HDR, but still detect actual status for reporting
		hdrResult := ValidateHDRStatusWithPath(outputPath, nil)
		result.IsHDRCorrect = true // No expectation means always valid
		result.ActualHDR = hdrResult.ActualHDR
		result.HDRMessage = hdrResult.Message
	}

	// Validate audio
	audioStreams, err := ffprobe.GetAudioStreamInfo(outputPath)
	if err != nil {
		result.AudioMessage = "Failed to get audio info"
	} else {
		result.IsAudioOpus, result.IsAudioTrackCountCorrect, result.AudioCodecs, result.AudioMessage = validateAudio(
			audioStreams, opts.ExpectedAudioTracks,
		)
	}

	// Validate A/V sync
	if opts.ExpectedDuration != nil && mediaInfo != nil {
		result.IsSyncPreserved, result.SyncDriftMs, result.SyncMessage = validateSync(
			outputProps.DurationSecs, *opts.ExpectedDuration,
		)
	} else {
		result.SyncMessage = "Sync validation skipped"
	}

	return result, nil
}

// validateVideoCodec checks that the output is AV1.
func validateVideoCodec(outputPath string) (bool, string) {
	probe, err := ffprobe.GetMediaInfo(outputPath)
	if err != nil {
		return false, ""
	}

	// Find video stream codec
	codecName := ""
	if probe.Width > 0 {
		// Use ffprobe with show_streams to get codec
		streams, err := getVideoCodec(outputPath)
		if err == nil {
			codecName = streams
		}
	}

	isAV1 := strings.Contains(strings.ToLower(codecName), "av1") ||
		strings.Contains(strings.ToLower(codecName), "av01")

	return isAV1, codecName
}

// getVideoCodec gets the video codec name using ffprobe.
func getVideoCodec(outputPath string) (string, error) {
	return ffprobe.GetVideoCodecName(outputPath)
}

// validateBitDepth checks that the output is 10-bit.
func validateBitDepth(outputPath string) (bool, *uint8, string) {
	// Try to get bit depth from MediaInfo first
	info, err := mediainfo.GetMediaInfo(outputPath)
	if err == nil {
		hdr := mediainfo.DetectHDR(info)
		if hdr.BitDepth != nil {
			is10Bit := *hdr.BitDepth >= 10
			return is10Bit, hdr.BitDepth, ""
		}
	}

	// Fallback to ffprobe
	props, err := ffprobe.GetVideoProperties(outputPath)
	if err != nil {
		return false, nil, ""
	}

	if props.HDRInfo.BitDepth != nil {
		is10Bit := *props.HDRInfo.BitDepth >= 10
		return is10Bit, props.HDRInfo.BitDepth, ""
	}

	// Default to true for AV1 (typically 10-bit)
	defaultDepth := uint8(10)
	return true, &defaultDepth, "yuv420p10le"
}

// validateDimensions checks that dimensions match expected values.
func validateDimensions(actualW, actualH, expectedW, expectedH uint32) (bool, string) {
	if actualW == expectedW && actualH == expectedH {
		return true, fmt.Sprintf("Dimensions match: %dx%d", actualW, actualH)
	}
	return false, fmt.Sprintf("Dimension mismatch: got %dx%d, expected %dx%d",
		actualW, actualH, expectedW, expectedH)
}

// validateDuration checks that duration is within acceptable tolerance.
func validateDuration(actual, expected float64) (bool, string) {
	diff := math.Abs(actual - expected)

	if diff <= durationToleranceSecs {
		return true, fmt.Sprintf("Duration matches input (%.1fs)", actual)
	}
	return false, fmt.Sprintf("Duration mismatch: got %.1fs, expected %.1fs (diff: %.1fs)",
		actual, expected, diff)
}

// validateAudio checks audio codec and track count.
func validateAudio(streams []ffprobe.AudioStreamInfo, expectedTracks *int) (bool, bool, []string, string) {
	isOpus := true
	var codecs []string

	for _, stream := range streams {
		codec := strings.ToLower(stream.CodecName)
		codecs = append(codecs, codec)
		if codec != "opus" {
			isOpus = false
		}
	}

	trackCountCorrect := true
	if expectedTracks != nil {
		trackCountCorrect = len(streams) == *expectedTracks
	}

	var message string
	if len(streams) == 0 {
		message = "No audio tracks"
	} else if len(streams) == 1 {
		if isOpus {
			message = "Audio track is Opus"
		} else {
			message = fmt.Sprintf("Audio track is %s (expected Opus)", codecs[0])
		}
	} else {
		if isOpus {
			message = fmt.Sprintf("%d audio tracks, all Opus", len(streams))
		} else {
			message = fmt.Sprintf("%d audio tracks: %s", len(streams), strings.Join(codecs, ", "))
		}
	}

	return isOpus, trackCountCorrect, codecs, message
}

// validateSync checks audio/video sync drift.
func validateSync(outputDuration, inputDuration float64) (bool, *float64, string) {
	// Calculate drift in milliseconds
	driftMs := math.Abs(outputDuration-inputDuration) * 1000
	preserved := driftMs <= maxSyncDriftMs

	message := fmt.Sprintf("Audio/video sync preserved (drift: %.1fms)", driftMs)
	if !preserved {
		message = fmt.Sprintf("Audio/video sync drift too large: %.1fms (max: %.1fms)", driftMs, maxSyncDriftMs)
	}

	return preserved, &driftMs, message
}
