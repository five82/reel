// Package chunk provides types and functions for managing video encoding chunks.
package chunk

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/five82/reel/internal/ffprobe"
)

// ExtractAudio extracts audio streams from the source video.
// The audio is encoded to Opus with bitrates determined by channel count.
func ExtractAudio(inputPath, workDir string, audioStreams []ffprobe.AudioStreamInfo) error {
	if len(audioStreams) == 0 {
		return nil // No audio to extract
	}

	audioPath := GetAudioPath(workDir)

	args := []string{
		"-hide_banner",
		"-i", inputPath,
		"-vn", // No video
		"-map_metadata", "0",
	}

	// Map each audio stream and set encoding parameters
	for i, stream := range audioStreams {
		args = append(args, "-map", fmt.Sprintf("0:a:%d", stream.Index))
		args = append(args, fmt.Sprintf("-c:a:%d", i), "libopus")
		bitrate := calculateAudioBitrate(stream.Channels)
		args = append(args, fmt.Sprintf("-b:a:%d", i), fmt.Sprintf("%dk", bitrate))
		args = append(args, fmt.Sprintf("-filter:a:%d", i), "aformat=channel_layouts=7.1|5.1|stereo|mono")
	}

	args = append(args, "-y", audioPath)

	cmd := exec.Command("ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("audio extraction failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// calculateAudioBitrate returns audio bitrate in kbps based on channel count.
func calculateAudioBitrate(channels uint32) uint32 {
	switch channels {
	case 1:
		return 64 // Mono
	case 2:
		return 128 // Stereo
	case 6:
		return 256 // 5.1 surround
	case 8:
		return 384 // 7.1 surround
	default:
		return channels * 48 // ~48 kbps per channel for non-standard configs
	}
}

// MuxFinal combines the encoded video with audio and other streams.
func MuxFinal(inputPath, workDir, outputPath string, audioStreams []ffprobe.AudioStreamInfo) error {
	videoPath := GetVideoPath(workDir)
	audioPath := GetAudioPath(workDir)

	// Check if video exists
	if _, err := os.Stat(videoPath); err != nil {
		return fmt.Errorf("video file not found: %w", err)
	}

	args := []string{
		"-hide_banner",
		"-i", videoPath, // Encoded video
	}

	// Add audio if it exists
	hasAudio := false
	if _, err := os.Stat(audioPath); err == nil && len(audioStreams) > 0 {
		args = append(args, "-i", audioPath)
		hasAudio = true
	}

	// Add original input for subtitles and chapters
	args = append(args, "-i", inputPath)

	// Map video
	args = append(args, "-map", "0:v:0")

	// Map audio if available
	if hasAudio {
		args = append(args, "-map", "1:a?")
	}

	// Map subtitles from original
	subtitleInputIdx := 2
	if !hasAudio {
		subtitleInputIdx = 1
	}
	args = append(args, "-map", fmt.Sprintf("%d:s?", subtitleInputIdx))

	// Copy all streams
	args = append(args, "-c", "copy")

	// Copy metadata and chapters
	args = append(args, "-map_metadata", "0")
	args = append(args, "-map_chapters", fmt.Sprintf("%d", subtitleInputIdx))

	// Faststart for web playback
	args = append(args, "-movflags", "+faststart")

	args = append(args, "-y", outputPath)

	cmd := exec.Command("ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("final mux failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// CleanupWorkDir removes the work directory and all its contents.
func CleanupWorkDir(workDir string) error {
	return os.RemoveAll(workDir)
}

// CreateWorkDir creates the work directory structure.
func CreateWorkDir(workDir string) error {
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}
	return EnsureEncodeDir(workDir)
}

// WorkDirExists checks if the work directory exists.
func WorkDirExists(workDir string) bool {
	_, err := os.Stat(workDir)
	return err == nil
}

// GetWorkDirPath returns the full path to the work directory for a given input file.
func GetWorkDirPath(inputPath, tempDir string) string {
	dirName := WorkDirName(inputPath)
	return filepath.Join(tempDir, dirName)
}
