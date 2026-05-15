// Package processing provides video processing orchestration.
package processing

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"codeberg.org/five82/reel/internal/util"
	"codeberg.org/five82/reel/internal/video"
)

const (
	cropSampleCount       = 141
	maxCropWorkers        = 4
	cropSampleStart       = 0.15
	cropSampleEnd         = 0.85
	blackLumaThreshold    = 24
	contrastThreshold     = 10
	minActivePixelsDiv    = 100
	minActiveFrameAreaDiv = 200
)

// CropResult contains the result of crop detection.
type CropResult struct {
	CropFilter     string // The crop filter string (e.g., "crop=1920:800:0:140")
	Required       bool   // Whether cropping is required
	MultipleRatios bool   // Whether multiple aspect ratios were detected
	Message        string // Human-readable message about the crop result
}

type detectedCrop struct {
	Top    uint32
	Bottom uint32
	Left   uint32
	Right  uint32
}

// DetectCrop detects black bars using decoded luma samples.
// It samples the middle 70% of the video, ignores all-black frames, and uses the
// minimum crop seen across valid samples so mixed-aspect content is not over-cropped.
func DetectCrop(inputPath string, inf *video.Info, disableCrop bool) CropResult {
	if disableCrop {
		return CropResult{Required: false, Message: "Skipped"}
	}
	if inputPath == "" || inf == nil || inf.Frames <= 0 || inf.Width == 0 || inf.Height == 0 {
		return CropResult{Required: false, Message: "No crop detected"}
	}

	frames := sampleCropFrames(inf.Frames, cropSampleCount)
	sampleMsg := fmt.Sprintf("Analyzed %d samples", len(frames))
	if len(frames) == 0 {
		return CropResult{Required: false, Message: sampleMsg}
	}

	crops := detectCropSamples(inputPath, inf, frames, cropWorkerCount(len(frames)))
	return cropResultFromSamples(crops, sampleMsg, inf.Width, inf.Height)
}

func cropWorkerCount(sampleCount int) int {
	if sampleCount <= 0 {
		return 0
	}
	workers := min(util.PhysicalCores(), maxCropWorkers)
	if workers < 1 {
		workers = 1
	}
	return min(sampleCount, workers)
}

func detectCropSamples(inputPath string, inf *video.Info, frames []int, workers int) []detectedCrop {
	partitions := partitionCropFrames(frames, workers)
	if len(partitions) == 0 {
		return nil
	}

	results := make(chan detectedCrop, len(frames))
	var wg sync.WaitGroup
	for _, partition := range partitions {
		wg.Add(1)
		go func(frames []int) {
			defer wg.Done()

			src, err := video.Open(inputPath, 1)
			if err != nil {
				return
			}
			defer src.Close()

			for _, frameIdx := range frames {
				frame, err := src.ReadLumaFrame(frameIdx, inf)
				if err != nil {
					continue
				}
				crop, ok := detectLumaCrop(frame.Data, frame.Width, frame.Height, frame.Stride, frame.Is10Bit)
				if ok {
					results <- crop
				}
			}
		}(partition)
	}

	wg.Wait()
	close(results)

	crops := make([]detectedCrop, 0, len(results))
	for crop := range results {
		crops = append(crops, crop)
	}
	return crops
}

func partitionCropFrames(frames []int, workers int) [][]int {
	if len(frames) == 0 || workers <= 0 {
		return nil
	}
	workers = min(workers, len(frames))
	partitions := make([][]int, 0, workers)
	base := len(frames) / workers
	extra := len(frames) % workers
	start := 0
	for i := 0; i < workers; i++ {
		size := base
		if i < extra {
			size++
		}
		end := start + size
		partitions = append(partitions, frames[start:end])
		start = end
	}
	return partitions
}

func cropResultFromSamples(crops []detectedCrop, sampleMsg string, sourceWidth, sourceHeight uint32) CropResult {
	best := detectedCrop{Top: ^uint32(0), Bottom: ^uint32(0), Left: ^uint32(0), Right: ^uint32(0)}
	var reference detectedCrop
	var haveReference, varied bool
	for _, crop := range crops {
		evenCrop := crop.even()
		if !haveReference {
			reference = evenCrop
			haveReference = true
		} else if evenCrop != reference {
			varied = true
		}
		best = minCrop(best, crop)
	}

	if len(crops) == 0 || best.Top == ^uint32(0) {
		return CropResult{Required: false, Message: sampleMsg}
	}

	best = best.even()
	if !best.hasCrop() {
		if varied {
			return CropResult{Required: false, MultipleRatios: true, Message: "Multiple aspect ratios detected"}
		}
		return CropResult{Required: false, Message: sampleMsg}
	}

	filter, ok := cropFilterForDetectedCrop(best, sourceWidth, sourceHeight)
	if !ok || !isEffectiveCrop(strings.TrimPrefix(filter, "crop="), sourceWidth, sourceHeight) {
		return CropResult{Required: false, Message: sampleMsg}
	}

	return CropResult{
		CropFilter: filter,
		Required:   true,
		Message:    "Black bars detected",
	}
}

func sampleCropFrames(totalFrames, sampleCount int) []int {
	if totalFrames <= 0 || sampleCount <= 0 {
		return nil
	}
	if totalFrames <= sampleCount {
		frames := make([]int, totalFrames)
		for i := range frames {
			frames[i] = i
		}
		return frames
	}

	frames := make([]int, 0, sampleCount)
	first := int(float64(totalFrames-1)*cropSampleStart + 0.5)
	last := int(float64(totalFrames-1)*cropSampleEnd + 0.5)
	if last < first {
		first, last = 0, totalFrames-1
	}
	span := last - first
	if sampleCount == 1 || span == 0 {
		return []int{first}
	}

	prev := -1
	for i := 0; i < sampleCount; i++ {
		frame := first + int(float64(span)*float64(i)/float64(sampleCount-1)+0.5)
		if frame != prev {
			frames = append(frames, frame)
			prev = frame
		}
	}
	return frames
}

func detectLumaCrop(data []byte, width, height, stride int, is10Bit bool) (detectedCrop, bool) {
	if width <= 0 || height <= 0 || stride <= 0 {
		return detectedCrop{}, false
	}
	bytesPerSample := 1
	if is10Bit {
		bytesPerSample = 2
	}
	if stride < width*bytesPerSample || len(data) < stride*height {
		return detectedCrop{}, false
	}

	activePixels := countActivePixels(data, width, height, stride, is10Bit)
	if activePixels < minActiveFramePixels(width, height) {
		return detectedCrop{}, false
	}

	top, ok := firstActiveRow(data, width, height, stride, is10Bit)
	if !ok {
		return detectedCrop{}, false
	}
	bottom, ok := lastActiveRow(data, width, height, stride, is10Bit)
	if !ok {
		return detectedCrop{}, false
	}
	left, ok := firstActiveCol(data, width, height, stride, is10Bit)
	if !ok {
		return detectedCrop{}, false
	}
	right, ok := lastActiveCol(data, width, height, stride, is10Bit)
	if !ok {
		return detectedCrop{}, false
	}

	return detectedCrop{
		Top:    uint32(top),
		Bottom: uint32(height - 1 - bottom),
		Left:   uint32(left),
		Right:  uint32(width - 1 - right),
	}, true
}

func firstActiveRow(data []byte, width, height, stride int, is10Bit bool) (int, bool) {
	for row := 0; row < height; row++ {
		if activeRow(data, row, width, stride, is10Bit) {
			return row, true
		}
	}
	return 0, false
}

func lastActiveRow(data []byte, width, height, stride int, is10Bit bool) (int, bool) {
	for row := height - 1; row >= 0; row-- {
		if activeRow(data, row, width, stride, is10Bit) {
			return row, true
		}
	}
	return 0, false
}

func firstActiveCol(data []byte, width, height, stride int, is10Bit bool) (int, bool) {
	for col := 0; col < width; col++ {
		if activeCol(data, col, height, stride, is10Bit) {
			return col, true
		}
	}
	return 0, false
}

func lastActiveCol(data []byte, width, height, stride int, is10Bit bool) (int, bool) {
	for col := width - 1; col >= 0; col-- {
		if activeCol(data, col, height, stride, is10Bit) {
			return col, true
		}
	}
	return 0, false
}

func activeRow(data []byte, row, width, stride int, is10Bit bool) bool {
	minActive := minActiveLinePixels(width)
	active := 0
	minVal := uint8(255)
	maxVal := uint8(0)
	rowOff := row * stride
	for col := 0; col < width; col++ {
		v := readLuma8(data, rowOff, col, is10Bit)
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
		if v > blackLumaThreshold {
			active++
			if active >= minActive {
				return true
			}
		}
	}
	return maxVal-minVal >= contrastThreshold && active > 0
}

func activeCol(data []byte, col, height, stride int, is10Bit bool) bool {
	minActive := minActiveLinePixels(height)
	active := 0
	minVal := uint8(255)
	maxVal := uint8(0)
	for row := 0; row < height; row++ {
		v := readLuma8(data, row*stride, col, is10Bit)
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
		if v > blackLumaThreshold {
			active++
			if active >= minActive {
				return true
			}
		}
	}
	return maxVal-minVal >= contrastThreshold && active > 0
}

func countActivePixels(data []byte, width, height, stride int, is10Bit bool) int {
	active := 0
	for row := 0; row < height; row++ {
		rowOff := row * stride
		for col := 0; col < width; col++ {
			if readLuma8(data, rowOff, col, is10Bit) > blackLumaThreshold {
				active++
			}
		}
	}
	return active
}

func readLuma8(data []byte, rowOff, col int, is10Bit bool) uint8 {
	if !is10Bit {
		return data[rowOff+col]
	}
	sample := binary.LittleEndian.Uint16(data[rowOff+col*2:])
	return uint8(min(sample>>2, 255))
}

func minActiveLinePixels(length int) int {
	minPixels := length / minActivePixelsDiv
	if minPixels < 1 {
		return 1
	}
	return minPixels
}

func minActiveFramePixels(width, height int) int {
	minPixels := width * height / minActiveFrameAreaDiv
	if minPixels < 1 {
		return 1
	}
	return minPixels
}

func minCrop(a, b detectedCrop) detectedCrop {
	return detectedCrop{
		Top:    min(a.Top, b.Top),
		Bottom: min(a.Bottom, b.Bottom),
		Left:   min(a.Left, b.Left),
		Right:  min(a.Right, b.Right),
	}
}

func (c detectedCrop) even() detectedCrop {
	return detectedCrop{Top: c.Top &^ 1, Bottom: c.Bottom &^ 1, Left: c.Left &^ 1, Right: c.Right &^ 1}
}

func (c detectedCrop) hasCrop() bool {
	return c.Top > 0 || c.Bottom > 0 || c.Left > 0 || c.Right > 0
}

func cropFilterForDetectedCrop(c detectedCrop, sourceWidth, sourceHeight uint32) (string, bool) {
	if c.Left >= sourceWidth || c.Right >= sourceWidth-c.Left || c.Top >= sourceHeight || c.Bottom >= sourceHeight-c.Top {
		return "", false
	}
	width := sourceWidth - c.Left - c.Right
	height := sourceHeight - c.Top - c.Bottom
	if width == 0 || height == 0 || width%2 != 0 || height%2 != 0 {
		return "", false
	}
	return fmt.Sprintf("crop=%d:%d:%d:%d", width, height, c.Left, c.Top), true
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
