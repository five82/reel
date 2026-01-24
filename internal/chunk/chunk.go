// Package chunk provides types and functions for managing video encoding chunks.
package chunk

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Scene represents a detected scene in the video.
type Scene struct {
	StartFrame int
	EndFrame   int
}

// Chunk represents a video chunk for encoding.
type Chunk struct {
	Idx   int // Chunk index (0-based)
	Start int // Start frame (inclusive)
	End   int // End frame (exclusive)
}

// Frames returns the number of frames in this chunk.
func (c Chunk) Frames() int {
	return c.End - c.Start
}

// ChunkComp represents a completed chunk's information.
type ChunkComp struct {
	Idx    int    // Chunk index
	Frames int    // Number of frames encoded
	Size   uint64 // Output file size in bytes
}

// ResumeInf contains information for resuming an interrupted encode.
type ResumeInf struct {
	ChunksDone []ChunkComp
}

// LoadScenes loads scene boundaries from a file.
// The file format is one frame number per line.
func LoadScenes(path string, totalFrames int) ([]Scene, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open scenes file: %w", err)
	}
	defer func() { _ = file.Close() }()

	var frameNums []int
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		frameNum, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("invalid frame number %q: %w", line, err)
		}
		frameNums = append(frameNums, frameNum)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading scenes file: %w", err)
	}

	// Sort frame numbers
	sort.Ints(frameNums)

	// Ensure we start at frame 0
	if len(frameNums) == 0 || frameNums[0] != 0 {
		frameNums = append([]int{0}, frameNums...)
	}

	// Convert to scenes
	scenes := make([]Scene, 0, len(frameNums))
	for i := 0; i < len(frameNums); i++ {
		start := frameNums[i]
		end := totalFrames
		if i+1 < len(frameNums) {
			end = frameNums[i+1]
		}

		if start < end {
			scenes = append(scenes, Scene{
				StartFrame: start,
				EndFrame:   end,
			})
		}
	}

	return scenes, nil
}

// ValidateScenes checks that scenes are valid and not too long.
func ValidateScenes(scenes []Scene, fpsNum, fpsDen uint32) error {
	if len(scenes) == 0 {
		return fmt.Errorf("no scenes provided")
	}

	// Validate FPS denominator to prevent division by zero
	if fpsDen == 0 {
		return fmt.Errorf("invalid FPS denominator: 0")
	}

	// Calculate max scene length (30 seconds or 1000 frames, whichever is smaller)
	fps := float64(fpsNum) / float64(fpsDen)
	maxFrames := min(int(fps*30), 1000)

	for i, scene := range scenes {
		length := scene.EndFrame - scene.StartFrame
		if length > maxFrames {
			return fmt.Errorf("scene %d is too long: %d frames (max %d)", i, length, maxFrames)
		}
		if length <= 0 {
			return fmt.Errorf("scene %d has invalid length: %d", i, length)
		}
	}

	return nil
}

// Chunkify converts scenes to chunks for encoding.
// Each scene becomes one chunk.
func Chunkify(scenes []Scene) []Chunk {
	chunks := make([]Chunk, len(scenes))
	for i, scene := range scenes {
		chunks[i] = Chunk{
			Idx:   i,
			Start: scene.StartFrame,
			End:   scene.EndFrame,
		}
	}
	return chunks
}

// GetResume loads resume information from the work directory.
func GetResume(workDir string) (*ResumeInf, error) {
	donePath := filepath.Join(workDir, "done.txt")

	file, err := os.Open(donePath)
	if os.IsNotExist(err) {
		return &ResumeInf{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open resume file: %w", err)
	}
	defer func() { _ = file.Close() }()

	var chunks []ChunkComp
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue // Skip malformed lines
		}

		idx, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}

		frames, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}

		size, err := strconv.ParseUint(parts[2], 10, 64)
		if err != nil {
			continue
		}

		chunks = append(chunks, ChunkComp{
			Idx:    idx,
			Frames: frames,
			Size:   size,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading resume file: %w", err)
	}

	return &ResumeInf{ChunksDone: chunks}, nil
}

// AppendDone appends a completed chunk to the resume file.
func AppendDone(chunk ChunkComp, workDir string) error {
	donePath := filepath.Join(workDir, "done.txt")

	file, err := os.OpenFile(donePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open resume file: %w", err)
	}
	defer func() { _ = file.Close() }()

	_, err = fmt.Fprintf(file, "%d %d %d\n", chunk.Idx, chunk.Frames, chunk.Size)
	if err != nil {
		return fmt.Errorf("failed to append resume data: %w", err)
	}

	return nil
}

// DoneSet returns a set of completed chunk indices for quick lookup.
func (r *ResumeInf) DoneSet() map[int]bool {
	done := make(map[int]bool, len(r.ChunksDone))
	for _, c := range r.ChunksDone {
		done[c.Idx] = true
	}
	return done
}

// TotalEncodedSize returns the total size of all completed chunks.
func (r *ResumeInf) TotalEncodedSize() uint64 {
	var total uint64
	for _, c := range r.ChunksDone {
		total += c.Size
	}
	return total
}

// TotalEncodedFrames returns the total frames of all completed chunks.
func (r *ResumeInf) TotalEncodedFrames() int {
	var total int
	for _, c := range r.ChunksDone {
		total += c.Frames
	}
	return total
}

// IVFPath returns the path to a chunk's IVF file.
func IVFPath(workDir string, chunkIdx int) string {
	return filepath.Join(workDir, "encode", fmt.Sprintf("%04d.ivf", chunkIdx))
}

// EnsureEncodeDir ensures the encode directory exists.
func EnsureEncodeDir(workDir string) error {
	encodeDir := filepath.Join(workDir, "encode")
	return os.MkdirAll(encodeDir, 0755)
}

// WorkDirName generates a work directory name from the input file.
func WorkDirName(inputPath string) string {
	// Use a hash of the input path for uniqueness
	base := filepath.Base(inputPath)
	// Remove extension
	ext := filepath.Ext(base)
	name := base[:len(base)-len(ext)]
	return fmt.Sprintf(".reel-%s", name)
}
