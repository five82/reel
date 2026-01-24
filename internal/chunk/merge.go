// Package chunk provides types and functions for managing video encoding chunks.
package chunk

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/five82/reel/internal/ffms"
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
		if _, err := fmt.Fprintf(f, "file '%s'\n", absPath); err != nil {
			return fmt.Errorf("failed to write to concat file: %w", err)
		}
	}

	return nil
}

// MergeOutput concatenates all IVF files into a single video file.
func MergeOutput(workDir, outputPath string, inf *ffms.VidInf, inputPath string) error {
	// Validate FPS to prevent division by zero
	if inf.FPSDen == 0 {
		return fmt.Errorf("invalid video info: FPS denominator is 0")
	}

	encodeDir := filepath.Join(workDir, "encode")

	// Find all IVF files
	ivfFiles, err := filepath.Glob(filepath.Join(encodeDir, "*.ivf"))
	if err != nil {
		return fmt.Errorf("failed to find IVF files: %w", err)
	}

	if len(ivfFiles) == 0 {
		return fmt.Errorf("no IVF files found in %s", encodeDir)
	}

	// Create concat list file
	concatPath := filepath.Join(workDir, "concat.txt")
	if err := writeConcatFile(concatPath, ivfFiles); err != nil {
		return err
	}

	// Calculate FPS for output
	fps := float64(inf.FPSNum) / float64(inf.FPSDen)

	// Create intermediate video file (video only)
	videoPath := filepath.Join(workDir, "video.mkv")

	// Build FFmpeg concat command
	args := []string{
		"-hide_banner",
		"-f", "concat",
		"-safe", "0",
		"-i", concatPath,
		"-c", "copy",
		"-r", fmt.Sprintf("%.6f", fps),
		"-fflags", "+genpts+igndts+discardcorrupt+bitexact",
		"-avoid_negative_ts", "make_zero",
		"-reset_timestamps", "1",
		"-start_at_zero",
		"-y",
		videoPath,
	}

	cmd := exec.Command("ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg concat failed: %w\nOutput: %s", err, string(output))
	}

	// Cleanup concat file
	_ = os.Remove(concatPath)

	return nil
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
	return filepath.Join(workDir, "video.mkv")
}

// GetAudioPath returns the path to the extracted audio file.
func GetAudioPath(workDir string) string {
	return filepath.Join(workDir, "audio.mka")
}
