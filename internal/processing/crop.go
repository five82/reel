// Package processing provides video processing orchestration.
package processing

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/five82/reel/internal/ffprobe"
)

// cropDetectionConcurrency is the maximum number of concurrent crop detection samples.
const cropDetectionConcurrency = 8

// CropResult contains the result of crop detection.
type CropResult struct {
	CropFilter     string // The crop filter string (e.g., "crop=1920:800:0:140")
	Required       bool   // Whether cropping is required
	MultipleRatios bool   // Whether multiple aspect ratios were detected
	Message        string // Human-readable message about the crop result
}

// cropRegex matches FFmpeg cropdetect output.
var cropRegex = regexp.MustCompile(`crop=(\d+:\d+:\d+:\d+)`)

// DetectCrop performs crop detection on a video file.
// It samples 141 points from 15-85% of the video to detect black bars.
func DetectCrop(inputPath string, props *ffprobe.VideoProperties, disableCrop bool) CropResult {
	if disableCrop {
		return CropResult{
			Required: false,
			Message:  "Skipped",
		}
	}

	// Set threshold based on HDR status
	threshold := uint32(16)
	if props.HDRInfo.IsHDR {
		threshold = 100
	}

	// Sample every 0.5% from 15% to 85% (141 points total)
	var samplePoints []float64
	for i := 30; i <= 170; i++ {
		samplePoints = append(samplePoints, float64(i)/200.0)
	}
	numSamples := len(samplePoints)

	// Process samples in parallel
	cropCounts := make(map[string]int)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Use a semaphore to limit concurrency
	sem := make(chan struct{}, cropDetectionConcurrency)

	for _, position := range samplePoints {
		wg.Add(1)
		go func(pos float64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			startTime := props.DurationSecs * pos
			crop := sampleCropAtPosition(inputPath, startTime, threshold)
			if crop != "" {
				mu.Lock()
				cropCounts[crop]++
				mu.Unlock()
			}
		}(position)
	}

	wg.Wait()

	sampleMsg := fmt.Sprintf("Analyzed %d samples", numSamples)

	// Analyze results
	if len(cropCounts) == 0 {
		return CropResult{
			Required: false,
			Message:  sampleMsg,
		}
	}

	if len(cropCounts) == 1 {
		// Single crop detected
		for crop := range cropCounts {
			if !isEffectiveCrop(crop, props.Width, props.Height) {
				return CropResult{
					Required: false,
					Message:  sampleMsg,
				}
			}
			return CropResult{
				CropFilter: "crop=" + crop,
				Required:   true,
				Message:    "Black bars detected",
			}
		}
	}

	// Multiple crops detected - find the most common
	type cropCount struct {
		crop  string
		count int
	}
	var sorted []cropCount
	totalSamples := 0
	for crop, count := range cropCounts {
		sorted = append(sorted, cropCount{crop, count})
		totalSamples += count
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	mostCommon := sorted[0]
	ratio := float64(mostCommon.count) / float64(totalSamples)

	// If one crop is dominant (>80% of samples), use it
	if ratio > 0.8 {
		if !isEffectiveCrop(mostCommon.crop, props.Width, props.Height) {
			return CropResult{
				Required: false,
				Message:  sampleMsg,
			}
		}
		return CropResult{
			CropFilter: "crop=" + mostCommon.crop,
			Required:   true,
			Message:    "Black bars detected",
		}
	}

	// Multiple significant aspect ratios - don't crop
	return CropResult{
		Required:       false,
		MultipleRatios: true,
		Message:        "Multiple aspect ratios detected",
	}
}

// sampleCropAtPosition samples crop detection at a specific position.
func sampleCropAtPosition(inputPath string, startTime float64, threshold uint32) string {
	cmd := exec.Command("ffmpeg",
		"-hide_banner",
		"-ss", fmt.Sprintf("%.2f", startTime),
		"-i", inputPath,
		"-vframes", "10",
		"-vf", fmt.Sprintf("cropdetect=limit=%d:round=2:reset=1", threshold),
		"-f", "null",
		"-",
	)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return ""
	}

	if err := cmd.Start(); err != nil {
		return ""
	}

	// Parse cropdetect output
	cropCounts := make(map[string]int)
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if matches := cropRegex.FindStringSubmatch(line); len(matches) >= 2 {
			cropValue := matches[1]
			if isValidCropFormat(cropValue) {
				cropCounts[cropValue]++
			}
		}
	}

	_ = cmd.Wait()

	// Return the most common crop value
	if len(cropCounts) == 0 {
		return ""
	}

	var bestCrop string
	bestCount := 0
	for crop, count := range cropCounts {
		if count > bestCount {
			bestCrop = crop
			bestCount = count
		}
	}

	return bestCrop
}

// isValidCropFormat validates that a crop string is in format w:h:x:y.
func isValidCropFormat(crop string) bool {
	parts := strings.Split(crop, ":")
	if len(parts) != 4 {
		return false
	}

	for _, part := range parts {
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return false
		}
	}

	return true
}

// isEffectiveCrop checks if a crop filter actually removes pixels.
func isEffectiveCrop(crop string, sourceWidth, sourceHeight uint32) bool {
	parts := strings.Split(crop, ":")
	if len(parts) < 2 {
		return true // Can't parse, assume effective
	}

	cropWidth, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return true
	}

	cropHeight, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return true
	}

	// If crop dimensions match source, no pixels are removed
	return uint32(cropWidth) != sourceWidth || uint32(cropHeight) != sourceHeight
}

// GetOutputDimensions calculates final output dimensions after crop.
func GetOutputDimensions(originalWidth, originalHeight uint32, cropFilter string) (uint32, uint32) {
	if cropFilter == "" {
		return originalWidth, originalHeight
	}

	// Strip "crop=" prefix if present
	params := strings.TrimPrefix(cropFilter, "crop=")
	parts := strings.Split(params, ":")

	if len(parts) >= 2 {
		if width, err := strconv.ParseUint(parts[0], 10, 32); err == nil {
			if height, err := strconv.ParseUint(parts[1], 10, 32); err == nil {
				return uint32(width), uint32(height)
			}
		}
	}

	return originalWidth, originalHeight
}
