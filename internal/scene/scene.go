// Package scene detects content-aware chunk boundaries for video encoding.
package scene

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"codeberg.org/five82/reel/internal/video"
)

const (
	detectorVersion = "reel-luma-scd-v1"
	metadataVersion = 2

	signatureWidth  = 64
	signatureHeight = 36
	signaturePixels = signatureWidth * signatureHeight
	histogramBins   = 64

	minimumNaturalCutDistance = 6
	baseCutThreshold          = 0.18
	defaultMinimumFrames      = 1
)

// Options controls scene detection.
type Options struct {
	MaxFrames int
	MinFrames int
	CropRect  *video.CropRect
	Progress  func(current, total int)
}

// Result describes detected chunk boundaries.
type Result struct {
	Boundaries       []int
	NaturalCuts      int
	NaturalCutFrames []int
	SyntheticSplits  int
	MergedScenes     int
}

// Metadata records the inputs that produced a scene file.
type Metadata struct {
	Version              int    `json:"version"`
	Detector             string `json:"detector"`
	InputPath            string `json:"input_path"`
	InputSize            int64  `json:"input_size"`
	InputModTimeUnixNano int64  `json:"input_mod_time_unix_nano"`
	Width                uint32 `json:"width"`
	Height               uint32 `json:"height"`
	FPSNum               uint32 `json:"fps_num"`
	FPSDen               uint32 `json:"fps_den"`
	Frames               int    `json:"frames"`
	Crop                 string `json:"crop,omitempty"`
	MaxFrames            int    `json:"max_frames"`
	MinFrames            int    `json:"min_frames"`
	Boundaries           int    `json:"boundaries"`
	NaturalCuts          int    `json:"natural_cuts"`
	SyntheticSplits      int    `json:"synthetic_splits"`
	MergedScenes         int    `json:"merged_scenes"`
	NaturalCutFrames     []int  `json:"natural_cut_frames,omitempty"`
}

type frameSignature struct {
	Samples [signaturePixels]uint8
	Hist    [histogramBins]uint32
	Mean    float64
}

type inputIdentity struct {
	Path            string
	Size            int64
	ModTimeUnixNano int64
}

// DetectToFileIfNeeded detects scenes and writes sceneFile unless a matching cached file exists.
func DetectToFileIfNeeded(ctx context.Context, inputPath, sceneFile, metadataFile string, inf *video.Info, opts Options) (Result, error) {
	if inf == nil {
		return Result{}, fmt.Errorf("nil video info")
	}
	if cached, ok := loadCachedResult(inputPath, sceneFile, metadataFile, inf, opts); ok {
		return cached, nil
	}

	result, err := Detect(ctx, inputPath, inf, opts)
	if err != nil {
		return Result{}, err
	}
	if err := writeSceneFile(sceneFile, result.Boundaries); err != nil {
		return Result{}, err
	}
	if err := writeMetadata(inputPath, metadataFile, inf, opts, result); err != nil {
		return Result{}, err
	}
	return result, nil
}

// Detect analyzes luma frames and returns scene-aware chunk starts.
func Detect(ctx context.Context, inputPath string, inf *video.Info, opts Options) (Result, error) {
	if inf == nil {
		return Result{}, fmt.Errorf("nil video info")
	}
	if inf.Frames <= 0 {
		return Result{Boundaries: []int{0}}, nil
	}
	maxFrames := normalizeMaxFrames(opts.MaxFrames, inf.Frames)
	minFrames := normalizeMinFrames(opts.MinFrames, maxFrames)

	scores, err := scoreVideo(ctx, inputPath, inf, opts)
	if err != nil {
		return Result{}, err
	}
	naturalCuts := detectNaturalCuts(scores)
	boundaries, syntheticSplits, mergedScenes := refineBoundaries(naturalCuts, inf.Frames, maxFrames, minFrames, scores)
	return Result{
		Boundaries:       boundaries,
		NaturalCuts:      max(0, len(naturalCuts)-1),
		NaturalCutFrames: naturalCutFrames(naturalCuts),
		SyntheticSplits:  syntheticSplits,
		MergedScenes:     mergedScenes,
	}, nil
}

func scoreVideo(ctx context.Context, inputPath string, inf *video.Info, opts Options) ([]float64, error) {
	src, err := video.Open(inputPath, 0)
	if err != nil {
		return nil, fmt.Errorf("scene detection: open video: %w", err)
	}
	defer src.Close()

	scores := make([]float64, inf.Frames)
	var previous *frameSignature
	for frameIdx := 0; frameIdx < inf.Frames; frameIdx++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		frame, err := src.ReadLumaFrame(frameIdx, inf)
		if err != nil {
			return nil, fmt.Errorf("scene detection: read frame %d: %w", frameIdx, err)
		}
		sig, err := signatureFromFrame(frame, opts.CropRect)
		if err != nil {
			return nil, fmt.Errorf("scene detection: analyze frame %d: %w", frameIdx, err)
		}
		if previous != nil {
			scores[frameIdx] = signatureChange(previous, sig)
		}
		previous = sig

		if opts.Progress != nil && (frameIdx == inf.Frames-1 || frameIdx%100 == 0) {
			opts.Progress(frameIdx+1, inf.Frames)
		}
	}
	return scores, nil
}

func signatureFromFrame(frame *video.LumaFrame, crop *video.CropRect) (*frameSignature, error) {
	if frame == nil {
		return nil, fmt.Errorf("nil luma frame")
	}
	bytesPerSample := 1
	if frame.Is10Bit {
		bytesPerSample = 2
	}
	if frame.Width <= 0 || frame.Height <= 0 || frame.Stride < frame.Width*bytesPerSample || len(frame.Data) < frame.Stride*frame.Height {
		return nil, fmt.Errorf("invalid luma geometry")
	}

	x, y, width, height, err := analysisRect(frame, crop)
	if err != nil {
		return nil, err
	}

	sig := &frameSignature{}
	var sum uint64
	for gy := 0; gy < signatureHeight; gy++ {
		srcY := y + (gy*height+height/2)/signatureHeight
		if srcY >= y+height {
			srcY = y + height - 1
		}
		rowOff := srcY * frame.Stride
		for gx := 0; gx < signatureWidth; gx++ {
			srcX := x + (gx*width+width/2)/signatureWidth
			if srcX >= x+width {
				srcX = x + width - 1
			}
			value := readLuma8(frame.Data, rowOff, srcX, frame.Is10Bit, frame.LumaShift)
			idx := gy*signatureWidth + gx
			sig.Samples[idx] = value
			sig.Hist[int(value)*histogramBins/256]++
			sum += uint64(value)
		}
	}
	sig.Mean = float64(sum) / signaturePixels
	return sig, nil
}

func analysisRect(frame *video.LumaFrame, crop *video.CropRect) (x, y, width, height int, err error) {
	x, y = 0, 0
	width, height = frame.Width, frame.Height
	if crop != nil {
		x = int(crop.X)
		y = int(crop.Y)
		width = int(crop.Width)
		height = int(crop.Height)
	}
	if x < 0 || y < 0 || width <= 0 || height <= 0 || x > frame.Width || width > frame.Width-x || y > frame.Height || height > frame.Height-y {
		return 0, 0, 0, 0, fmt.Errorf("invalid analysis rectangle %dx%d+%d+%d for frame %dx%d", width, height, x, y, frame.Width, frame.Height)
	}
	return x, y, width, height, nil
}

func readLuma8(data []byte, rowOff, col int, is10Bit bool, lumaShift int) uint8 {
	if !is10Bit {
		return data[rowOff+col]
	}
	if lumaShift == 0 {
		lumaShift = 2
	}
	sample := binary.LittleEndian.Uint16(data[rowOff+col*2:])
	return uint8(min(sample>>lumaShift, 255))
}

func signatureChange(previous, current *frameSignature) float64 {
	var pixelDelta uint64
	for i := range previous.Samples {
		pixelDelta += uint64(absInt(int(previous.Samples[i]) - int(current.Samples[i])))
	}
	pixelScore := float64(pixelDelta) / (signaturePixels * 255)

	var histDelta uint64
	for i := range previous.Hist {
		histDelta += uint64(absInt(int(previous.Hist[i]) - int(current.Hist[i])))
	}
	histScore := float64(histDelta) / (2 * signaturePixels)
	meanScore := math.Abs(previous.Mean-current.Mean) / 255

	return 0.65*pixelScore + 0.30*histScore + 0.05*meanScore
}

func detectNaturalCuts(scores []float64) []int {
	if len(scores) <= 1 {
		return []int{0}
	}
	threshold := sceneCutThreshold(scores)
	cuts := []int{0}

	for i := 1; i < len(scores); {
		if scores[i] < threshold {
			i++
			continue
		}

		start := i
		best := i
		for i < len(scores) && scores[i] >= threshold {
			if scores[i] > scores[best] {
				best = i
			}
			i++
		}
		clusterLen := i - start
		if looksLikeFlashCluster(clusterLen) {
			continue
		}
		if best-cuts[len(cuts)-1] >= minimumNaturalCutDistance {
			cuts = append(cuts, best)
		}
	}
	return cuts
}

func sceneCutThreshold(scores []float64) float64 {
	values := make([]float64, 0, len(scores)-1)
	for _, score := range scores[1:] {
		if score > 0 {
			values = append(values, score)
		}
	}
	if len(values) == 0 {
		return baseCutThreshold
	}
	sort.Float64s(values)
	median := percentileSorted(values, 0.50)
	p95 := percentileSorted(values, 0.95)
	madValues := make([]float64, len(values))
	for i, value := range values {
		madValues[i] = math.Abs(value - median)
	}
	sort.Float64s(madValues)
	mad := percentileSorted(madValues, 0.50)
	adaptive := median + 6*mad
	return maxFloat(baseCutThreshold, minFloat(maxFloat(adaptive, p95*1.10), 0.35))
}

func looksLikeFlashCluster(clusterLen int) bool {
	return clusterLen == 2
}

func refineBoundaries(naturalCuts []int, totalFrames, maxFrames, minFrames int, scores []float64) ([]int, int, int) {
	if totalFrames <= 0 {
		return []int{0}, 0, 0
	}
	maxFrames = normalizeMaxFrames(maxFrames, totalFrames)
	minFrames = normalizeMinFrames(minFrames, maxFrames)

	cuts, mergedScenes := packShortScenes(normalizedNaturalCuts(naturalCuts, totalFrames), totalFrames, maxFrames, minFrames, scores)
	boundaries := make([]int, 0, len(cuts))
	syntheticSplits := 0
	for i, start := range cuts {
		end := totalFrames
		if i+1 < len(cuts) {
			end = cuts[i+1]
		}
		if end <= start {
			continue
		}
		if len(boundaries) == 0 || boundaries[len(boundaries)-1] != start {
			boundaries = append(boundaries, start)
		}
		currentStart := start
		for end-currentStart > maxFrames {
			split := chooseSplitPoint(currentStart, end, maxFrames, scores)
			if split <= 0 || split > maxFrames {
				split = min(maxFrames, (end-currentStart)/2)
			}
			currentStart += split
			boundaries = append(boundaries, currentStart)
			syntheticSplits++
		}
	}
	if len(boundaries) == 0 || boundaries[0] != 0 {
		boundaries = append([]int{0}, boundaries...)
	}
	return dedupeSorted(boundaries), syntheticSplits, mergedScenes
}

func packShortScenes(cuts []int, totalFrames, maxFrames, minFrames int, scores []float64) ([]int, int) {
	if minFrames <= defaultMinimumFrames || len(cuts) <= 1 {
		return cuts, 0
	}

	packed := append([]int(nil), cuts...)
	merged := 0
	for {
		changed := false
		for i := 0; i < len(packed); i++ {
			start := packed[i]
			end := totalFrames
			if i+1 < len(packed) {
				end = packed[i+1]
			}
			if end-start >= minFrames {
				continue
			}

			removeIdx := shortSceneBoundaryToRemove(packed, totalFrames, maxFrames, scores, i)
			if removeIdx <= 0 || removeIdx >= len(packed) {
				continue
			}
			packed = append(packed[:removeIdx], packed[removeIdx+1:]...)
			merged++
			changed = true
			break
		}
		if !changed {
			return packed, merged
		}
	}
}

func shortSceneBoundaryToRemove(cuts []int, totalFrames, maxFrames int, scores []float64, sceneIdx int) int {
	start := cuts[sceneIdx]
	end := totalFrames
	if sceneIdx+1 < len(cuts) {
		end = cuts[sceneIdx+1]
	}

	prevRemoveIdx := -1
	if sceneIdx > 0 && end-cuts[sceneIdx-1] <= maxFrames {
		prevRemoveIdx = sceneIdx
	}

	nextRemoveIdx := -1
	if sceneIdx+1 < len(cuts) {
		nextEnd := totalFrames
		if sceneIdx+2 < len(cuts) {
			nextEnd = cuts[sceneIdx+2]
		}
		if nextEnd-start <= maxFrames {
			nextRemoveIdx = sceneIdx + 1
		}
	}

	switch {
	case prevRemoveIdx < 0:
		return nextRemoveIdx
	case nextRemoveIdx < 0:
		return prevRemoveIdx
	case cutScore(scores, cuts[prevRemoveIdx]) <= cutScore(scores, cuts[nextRemoveIdx]):
		return prevRemoveIdx
	default:
		return nextRemoveIdx
	}
}

func cutScore(scores []float64, cut int) float64 {
	if cut <= 0 || cut >= len(scores) {
		return 0
	}
	return scores[cut]
}

func naturalCutFrames(cuts []int) []int {
	frames := make([]int, 0, len(cuts))
	for _, cut := range cuts {
		if cut > 0 {
			frames = append(frames, cut)
		}
	}
	return frames
}

func normalizedNaturalCuts(cuts []int, totalFrames int) []int {
	out := make([]int, 0, len(cuts)+1)
	out = append(out, 0)
	for _, cut := range cuts {
		if cut > 0 && cut < totalFrames {
			out = append(out, cut)
		}
	}
	sort.Ints(out)
	return dedupeSorted(out)
}

func chooseSplitPoint(start, end, maxFrames int, scores []float64) int {
	distance := end - start
	if distance <= maxFrames {
		return distance
	}
	minimumSplitCount := distance / maxFrames
	middlePoint := distance / (minimumSplitCount + 1)
	minSize := max(1, middlePoint/2)
	maxSize := min(maxFrames, middlePoint+minSize)
	if maxSize <= minSize {
		return middlePoint
	}

	rangeSize := maxSize - minSize
	bestSize := middlePoint
	bestScore := 0.0
	for size := minSize; size <= maxSize; size++ {
		idx := start + size
		if idx <= 0 || idx >= len(scores) {
			continue
		}
		distanceFromMiddle := absInt(middlePoint - size)
		distanceWeight := 1.0 - float64(distanceFromMiddle)/float64(rangeSize)
		weightedScore := scores[idx] * distanceWeight
		if weightedScore > bestScore {
			bestScore = weightedScore
			bestSize = size
		}
	}
	if bestScore <= 0 {
		return middlePoint
	}
	return bestSize
}

func normalizeMaxFrames(maxFrames, totalFrames int) int {
	if maxFrames <= 0 {
		return max(1, totalFrames)
	}
	return max(1, maxFrames)
}

func normalizeMinFrames(minFrames, maxFrames int) int {
	if minFrames <= 0 {
		return defaultMinimumFrames
	}
	return min(max(minFrames, defaultMinimumFrames), maxFrames)
}

func loadCachedResult(inputPath, sceneFile, metadataFile string, inf *video.Info, opts Options) (Result, bool) {
	data, err := os.ReadFile(metadataFile)
	if err != nil {
		return Result{}, false
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return Result{}, false
	}
	if !metadataMatches(inputPath, inf, opts, meta) {
		return Result{}, false
	}
	boundaries, err := readSceneFile(sceneFile)
	if err != nil || len(boundaries) == 0 {
		return Result{}, false
	}
	return Result{Boundaries: boundaries, NaturalCuts: meta.NaturalCuts, NaturalCutFrames: meta.NaturalCutFrames, SyntheticSplits: meta.SyntheticSplits, MergedScenes: meta.MergedScenes}, true
}

func metadataMatches(inputPath string, inf *video.Info, opts Options, meta Metadata) bool {
	id, err := identifyInput(inputPath)
	if err != nil {
		return false
	}
	return meta.Version == metadataVersion &&
		meta.Detector == detectorVersion &&
		meta.InputPath == id.Path &&
		meta.InputSize == id.Size &&
		meta.InputModTimeUnixNano == id.ModTimeUnixNano &&
		meta.Width == inf.Width &&
		meta.Height == inf.Height &&
		meta.FPSNum == inf.FPSNum &&
		meta.FPSDen == inf.FPSDen &&
		meta.Frames == inf.Frames &&
		meta.Crop == cropString(opts.CropRect) &&
		meta.MaxFrames == normalizeMaxFrames(opts.MaxFrames, inf.Frames) &&
		meta.MinFrames == normalizeMinFrames(opts.MinFrames, normalizeMaxFrames(opts.MaxFrames, inf.Frames))
}

func writeMetadata(inputPath, metadataFile string, inf *video.Info, opts Options, result Result) error {
	id, err := identifyInput(inputPath)
	if err != nil {
		return fmt.Errorf("scene detection: stat input: %w", err)
	}
	meta := Metadata{
		Version:              metadataVersion,
		Detector:             detectorVersion,
		InputPath:            id.Path,
		InputSize:            id.Size,
		InputModTimeUnixNano: id.ModTimeUnixNano,
		Width:                inf.Width,
		Height:               inf.Height,
		FPSNum:               inf.FPSNum,
		FPSDen:               inf.FPSDen,
		Frames:               inf.Frames,
		Crop:                 cropString(opts.CropRect),
		MaxFrames:            normalizeMaxFrames(opts.MaxFrames, inf.Frames),
		MinFrames:            normalizeMinFrames(opts.MinFrames, normalizeMaxFrames(opts.MaxFrames, inf.Frames)),
		Boundaries:           len(result.Boundaries),
		NaturalCuts:          result.NaturalCuts,
		SyntheticSplits:      result.SyntheticSplits,
		MergedScenes:         result.MergedScenes,
		NaturalCutFrames:     result.NaturalCutFrames,
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("scene detection: encode metadata: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(metadataFile, data, 0644); err != nil {
		return fmt.Errorf("scene detection: write metadata: %w", err)
	}
	return nil
}

func identifyInput(inputPath string) (inputIdentity, error) {
	abs, err := filepath.Abs(inputPath)
	if err != nil {
		abs = inputPath
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		abs = resolved
	}
	stat, err := os.Stat(inputPath)
	if err != nil {
		return inputIdentity{}, err
	}
	return inputIdentity{Path: abs, Size: stat.Size(), ModTimeUnixNano: stat.ModTime().UnixNano()}, nil
}

func cropString(crop *video.CropRect) string {
	if crop == nil {
		return ""
	}
	return fmt.Sprintf("crop=%d:%d:%d:%d", crop.Width, crop.Height, crop.X, crop.Y)
}

func writeSceneFile(path string, boundaries []int) error {
	var b strings.Builder
	for _, boundary := range boundaries {
		_, _ = fmt.Fprintf(&b, "%d\n", boundary)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("scene detection: write scene file: %w", err)
	}
	return nil
}

func readSceneFile(path string) ([]int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var boundaries []int
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		boundary, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		boundaries = append(boundaries, boundary)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Ints(boundaries)
	return dedupeSorted(boundaries), nil
}

func percentileSorted(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if len(values) == 1 {
		return values[0]
	}
	pos := p * float64(len(values)-1)
	lower := int(math.Floor(pos))
	upper := int(math.Ceil(pos))
	if lower == upper {
		return values[lower]
	}
	weight := pos - float64(lower)
	return values[lower]*(1-weight) + values[upper]*weight
}

func dedupeSorted(values []int) []int {
	if len(values) <= 1 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
