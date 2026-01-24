// Package validation provides post-encode validation checks.
package validation

import (
	"github.com/five82/reel/internal/mediainfo"
)

// HDRValidationResult contains the result of HDR validation.
type HDRValidationResult struct {
	IsValid       bool
	ActualHDR     *bool
	Message       string
	MediaInfoUsed bool
}

// ValidateHDRStatusWithPath validates HDR status using MediaInfo.
// This provides comprehensive HDR detection by checking MediaInfo availability first.
func ValidateHDRStatusWithPath(outputPath string, expectedHDR *bool) HDRValidationResult {
	return validateHDRStatusWithAvailabilityCheck(outputPath, expectedHDR, mediainfo.IsAvailable())
}

// validateHDRStatusWithAvailabilityCheck is the internal validation function.
// This allows for easier testing without depending on actual system MediaInfo installation.
func validateHDRStatusWithAvailabilityCheck(outputPath string, expectedHDR *bool, mediainfoAvailable bool) HDRValidationResult {
	// Check if MediaInfo is available first
	if !mediainfoAvailable {
		return HDRValidationResult{
			IsValid:       true, // Pass validation when MediaInfo not available
			ActualHDR:     nil,
			Message:       "MediaInfo not installed - HDR validation skipped",
			MediaInfoUsed: false,
		}
	}

	// Use MediaInfo for HDR detection
	var actualHDR *bool
	info, err := mediainfo.GetMediaInfo(outputPath)
	if err == nil {
		hdrInfo := mediainfo.DetectHDR(info)
		actualHDR = &hdrInfo.IsHDR
	}

	return validateHDRResult(expectedHDR, actualHDR)
}

// validateHDRResult performs the common HDR validation logic.
func validateHDRResult(expectedHDR, actualHDR *bool) HDRValidationResult {
	result := HDRValidationResult{MediaInfoUsed: true}

	switch {
	case expectedHDR != nil && actualHDR != nil:
		// Both expected and actual are known
		if *expectedHDR == *actualHDR {
			status := "SDR"
			if *actualHDR {
				status = "HDR"
			}
			result.IsValid = true
			result.ActualHDR = actualHDR
			result.Message = status + " preserved"
		} else {
			expectedStr := "SDR"
			if *expectedHDR {
				expectedStr = "HDR"
			}
			actualStr := "SDR"
			if *actualHDR {
				actualStr = "HDR"
			}
			result.IsValid = false
			result.ActualHDR = actualHDR
			result.Message = "Expected " + expectedStr + ", found " + actualStr
		}

	case expectedHDR == nil && actualHDR != nil:
		// No expectation, but we detected the status
		status := "SDR"
		if *actualHDR {
			status = "HDR"
		}
		result.IsValid = true
		result.ActualHDR = actualHDR
		result.Message = "Output is " + status

	case expectedHDR != nil && actualHDR == nil:
		// Had an expectation but couldn't detect
		expectedStr := "SDR"
		if *expectedHDR {
			expectedStr = "HDR"
		}
		result.IsValid = false
		result.ActualHDR = nil
		result.Message = "Expected " + expectedStr + ", but could not detect HDR status"

	default:
		// Neither expected nor actual are available
		result.IsValid = false
		result.ActualHDR = nil
		result.Message = "Could not detect HDR status"
	}

	return result
}

// GetDetailedHDRInfo returns detailed HDR metadata from MediaInfo.
// This is useful for debugging and detailed reporting.
func GetDetailedHDRInfo(path string) (*mediainfo.HDRInfo, error) {
	if !mediainfo.IsAvailable() {
		return nil, nil
	}

	info, err := mediainfo.GetMediaInfo(path)
	if err != nil {
		return nil, err
	}

	hdrInfo := mediainfo.DetectHDR(info)
	return &hdrInfo, nil
}
