package ffmpeg

import "math"

// EncodeParams contains parameters for display purposes.
// Only used for showing encoding configuration to the user.
type EncodeParams struct {
	Quality            uint32
	Preset             uint8
	Tune               uint8
	CropFilter         string // Optional crop filter
	PixelFormat        string
	MatrixCoefficients string
}

// CalculateAudioBitrate returns audio bitrate in kbps based on channel count.
func CalculateAudioBitrate(channels uint32) uint32 {
	if channels == 0 {
		return 0
	}
	return uint32(128 * math.Pow(channelEquivalent(channels)/2, 0.75))
}

func channelEquivalent(channels uint32) float64 {
	switch channels {
	case 1, 2:
		return float64(channels)
	case 3:
		return 2.1
	case 4:
		return 3.1
	case 5:
		return 4.1
	case 6:
		return 5.1
	case 7:
		return 6.1
	case 8:
		return 7.1
	default:
		return float64(channels)
	}
}
