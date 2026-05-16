package media

import "codeberg.org/five82/reel/internal/video"

// IsAvailable reports whether native media probing is available.
func IsAvailable() bool {
	return true
}

// GetStreamHDRInfo returns HDR-related metadata from container/stream parameters only.
func GetStreamHDRInfo(path string) (*HDRInfo, error) {
	props, err := GetVideoProperties(path)
	if err != nil {
		return nil, err
	}
	hdr := props.HDRInfo
	return &hdr, nil
}

// GetHDRInfo returns HDR-related metadata using native libav probes.
func GetHDRInfo(path string) (*HDRInfo, error) {
	hdr, err := GetStreamHDRInfo(path)
	if err != nil {
		return nil, err
	}

	inf, err := video.Probe(path)
	if err != nil {
		hdr.IsHDR = detectHDR(hdr.ColourPrimaries, hdr.TransferCharacteristics, hdr.MatrixCoefficients)
		return hdr, nil
	}

	if inf.Is10Bit {
		depth := uint8(10)
		hdr.BitDepth = &depth
	}
	if inf.ColorPrimaries != nil {
		if name := colorPrimariesName(*inf.ColorPrimaries); name != "" {
			hdr.ColourPrimaries = name
		}
	}
	if inf.TransferCharacteristics != nil {
		if name := colorTransferName(*inf.TransferCharacteristics); name != "" {
			hdr.TransferCharacteristics = name
		}
	}
	if inf.MatrixCoefficients != nil {
		if name := colorSpaceName(*inf.MatrixCoefficients); name != "" {
			hdr.MatrixCoefficients = name
		}
	}
	hdr.IsHDR = detectHDR(hdr.ColourPrimaries, hdr.TransferCharacteristics, hdr.MatrixCoefficients)
	return hdr, nil
}

func colorPrimariesName(value int32) string {
	switch value {
	case 1:
		return "bt709"
	case 4:
		return "bt470m"
	case 5:
		return "bt470bg"
	case 6:
		return "smpte170m"
	case 7:
		return "smpte240m"
	case 9:
		return "bt2020"
	case 12:
		return "smpte431"
	case 22:
		return "ebu3213"
	default:
		return ""
	}
}

func colorTransferName(value int32) string {
	switch value {
	case 1:
		return "bt709"
	case 4:
		return "gamma22"
	case 5:
		return "gamma28"
	case 6:
		return "smpte170m"
	case 7:
		return "smpte240m"
	case 13:
		return "iec61966-2-1"
	case 14:
		return "bt2020-10"
	case 15:
		return "bt2020-12"
	case 16:
		return "smpte2084"
	case 18:
		return "arib-std-b67"
	default:
		return ""
	}
}

func colorSpaceName(value int32) string {
	switch value {
	case 1:
		return "bt709"
	case 4:
		return "fcc"
	case 5:
		return "bt470bg"
	case 6:
		return "smpte170m"
	case 7:
		return "smpte240m"
	case 9:
		return "bt2020nc"
	case 10:
		return "bt2020c"
	case 12:
		return "chromaticity-derived-nc"
	case 13:
		return "chromaticity-derived-c"
	case 14:
		return "ictcp"
	default:
		return ""
	}
}
