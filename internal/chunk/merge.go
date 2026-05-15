// Package chunk provides types and functions for managing video encoding chunks.
package chunk

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"codeberg.org/five82/reel/internal/video"
)

// writeConcatFile writes a FFmpeg concat file with the given paths.
// Uses defer for proper resource cleanup.
func writeConcatFile(concatPath string, paths []string) (err error) {
	f, err := os.Create(concatPath)
	if err != nil {
		return fmt.Errorf("failed to create concat file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close concat file: %w", cerr)
		}
	}()

	for _, p := range paths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("failed to get absolute path for %s: %w", p, err)
		}
		if _, err := fmt.Fprintf(f, "file '%s'\n", escapeConcatPath(absPath)); err != nil {
			return fmt.Errorf("failed to write to concat file: %w", err)
		}
	}

	return nil
}

func escapeConcatPath(path string) string {
	return strings.ReplaceAll(path, "'", "'\\''")
}

// MergeOutput concatenates all chunk IVF files into a single IVF video file.
func MergeOutput(workDir string, inf *video.Info, numChunks int) error {
	if numChunks <= 0 {
		return fmt.Errorf("no chunks to merge")
	}
	const maxUint32 = int64(^uint32(0))
	if inf.Frames < 0 || int64(inf.Frames) > maxUint32 {
		return fmt.Errorf("invalid video info: frame count %d cannot be stored in IVF header", inf.Frames)
	}

	ivfFiles := make([]string, numChunks)
	for i := range numChunks {
		ivfFiles[i] = IVFPath(workDir, i)
	}

	if err := concatIVF(ivfFiles, GetVideoPath(workDir), uint32(inf.Frames)); err != nil {
		return fmt.Errorf("ivf concat failed: %w", err)
	}
	return nil
}

func concatIVF(files []string, outputPath string, totalFrames uint32) (err error) {
	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create merged IVF: %w", err)
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close merged IVF: %w", cerr)
		}
	}()

	var frameIdx uint32
	for i, path := range files {
		if err := appendIVF(out, path, i == 0, totalFrames, &frameIdx); err != nil {
			return err
		}
	}
	if frameIdx != totalFrames {
		return fmt.Errorf("merged IVF contains %d frames, expected %d", frameIdx, totalFrames)
	}
	return nil
}

func appendIVF(out io.Writer, path string, includeHeader bool, totalFrames uint32, frameIdx *uint32) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open IVF chunk %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	header := make([]byte, 32)
	if _, err := io.ReadFull(file, header); err != nil {
		return fmt.Errorf("failed to read IVF header from %s: %w", path, err)
	}
	if string(header[:4]) != "DKIF" || binary.LittleEndian.Uint16(header[6:8]) != 32 {
		return fmt.Errorf("invalid IVF header in %s", path)
	}

	if includeHeader {
		binary.LittleEndian.PutUint32(header[24:28], totalFrames)
		if _, err := out.Write(header); err != nil {
			return fmt.Errorf("failed to write IVF header: %w", err)
		}
	}
	for {
		frameHeader := make([]byte, 12)
		if _, err := io.ReadFull(file, frameHeader); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("failed to read IVF frame header from %s: %w", path, err)
		}
		frameSize := binary.LittleEndian.Uint32(frameHeader[:4])
		binary.LittleEndian.PutUint64(frameHeader[4:12], uint64(*frameIdx))
		if _, err := out.Write(frameHeader); err != nil {
			return fmt.Errorf("failed to write IVF frame header: %w", err)
		}
		if _, err := io.CopyN(out, file, int64(frameSize)); err != nil {
			return fmt.Errorf("failed to append IVF frame from %s: %w", path, err)
		}
		*frameIdx = *frameIdx + 1
	}
}

// MergeBatched handles large numbers of IVF files by merging in batches.
// This is necessary because FFmpeg's concat demuxer can have issues with
// very large numbers of files.
func MergeBatched(workDir string, numChunks int) error {
	const batchSize = 500

	if numChunks <= batchSize {
		return nil // No batching needed, MergeOutput handles it
	}

	encodeDir := filepath.Join(workDir, "encode")
	tempDir := filepath.Join(workDir, "temp_merge")

	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp merge dir: %w", err)
	}

	// Process in batches
	batchNum := 0
	for start := 0; start < numChunks; start += batchSize {
		end := start + batchSize
		if end > numChunks {
			end = numChunks
		}

		// Create concat list for this batch
		concatPath := filepath.Join(tempDir, fmt.Sprintf("batch_%04d.txt", batchNum))
		batchPaths := make([]string, 0, end-start)
		for i := start; i < end; i++ {
			batchPaths = append(batchPaths, IVFPath(workDir, i))
		}
		if err := writeConcatFile(concatPath, batchPaths); err != nil {
			return fmt.Errorf("failed to create batch %d concat file: %w", batchNum, err)
		}

		// Merge this batch
		batchOut := filepath.Join(tempDir, fmt.Sprintf("batch_%04d.ivf", batchNum))
		args := []string{
			"-hide_banner",
			"-f", "concat",
			"-safe", "0",
			"-i", concatPath,
			"-c", "copy",
			"-y",
			batchOut,
		}

		cmd := exec.Command("ffmpeg", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("batch merge failed: %w\nOutput: %s", err, string(output))
		}

		_ = os.Remove(concatPath)
		batchNum++
	}

	// Now merge all batch outputs
	finalConcatPath := filepath.Join(tempDir, "final.txt")
	finalBatchPaths := make([]string, batchNum)
	for i := 0; i < batchNum; i++ {
		finalBatchPaths[i] = filepath.Join(tempDir, fmt.Sprintf("batch_%04d.ivf", i))
	}
	if err := writeConcatFile(finalConcatPath, finalBatchPaths); err != nil {
		return fmt.Errorf("failed to create final concat file: %w", err)
	}

	// Final merge to encode directory
	finalOut := filepath.Join(encodeDir, "merged.ivf")
	args := []string{
		"-hide_banner",
		"-f", "concat",
		"-safe", "0",
		"-i", finalConcatPath,
		"-c", "copy",
		"-y",
		finalOut,
	}

	cmd := exec.Command("ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("final merge failed: %w\nOutput: %s", err, string(output))
	}

	// Move merged file to replace individual IVFs
	for i := 0; i < numChunks; i++ {
		_ = os.Remove(IVFPath(workDir, i))
	}

	// Rename merged to 0000.ivf so MergeOutput can find it
	if err := os.Rename(finalOut, IVFPath(workDir, 0)); err != nil {
		return fmt.Errorf("failed to rename merged file: %w", err)
	}

	// Cleanup temp dir
	_ = os.RemoveAll(tempDir)

	return nil
}

// GetVideoPath returns the path to the merged video file.
func GetVideoPath(workDir string) string {
	return filepath.Join(workDir, "video.ivf")
}
