// Package chunk provides types and functions for managing video encoding chunks.
package chunk

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	nativeaudio "codeberg.org/five82/reel/internal/audio"
	"codeberg.org/five82/reel/internal/media"
)

// ExtractAudio encodes source audio streams to Opus using native libav/libopusenc.
func ExtractAudio(ctx context.Context, inputPath, workDir string, audioStreams []media.AudioStreamInfo) ([]nativeaudio.EncodedStream, error) {
	return nativeaudio.EncodeStreams(ctx, inputPath, workDir, audioStreams)
}

func muxFinalArgs(inputPath, videoPath, outputPath string, audioStreams []nativeaudio.EncodedStream, displayAspect string) []string {
	args := []string{
		"-hide_banner",
		"-i", videoPath,
	}

	for _, stream := range audioStreams {
		args = append(args, "-i", stream.Path)
	}

	sourceInputIdx := len(audioStreams) + 1
	args = append(args, "-i", inputPath)
	args = append(args, "-map", "0:v:0")
	for i := range audioStreams {
		args = append(args, "-map", fmt.Sprintf("%d:a:0", i+1))
	}
	args = append(args, "-c", "copy")
	args = append(args, "-map_metadata", fmt.Sprintf("%d", sourceInputIdx))
	args = append(args, "-map_chapters", fmt.Sprintf("%d", sourceInputIdx))

	if displayAspect != "" {
		args = append(args, "-aspect:v:0", displayAspect)
	}
	for i, stream := range audioStreams {
		args = append(args, audioMetadataArgs(i, stream.Info)...)
		args = append(args, audioDispositionArgs(i, stream.Info.Disposition)...)
	}

	args = append(args, "-movflags", "+faststart")
	args = append(args, "-y", outputPath)
	return args
}

// MuxFinal combines the encoded video with audio, chapters, and metadata.
func MuxFinal(inputPath, workDir, outputPath string, audioStreams []nativeaudio.EncodedStream, displayAspect string) error {
	videoPath := GetVideoPath(workDir)
	if _, err := os.Stat(videoPath); err != nil {
		return fmt.Errorf("video file not found: %w", err)
	}
	for _, stream := range audioStreams {
		if _, err := os.Stat(stream.Path); err != nil {
			return fmt.Errorf("audio file not found: %w", err)
		}
	}

	args := muxFinalArgs(inputPath, videoPath, outputPath, audioStreams, displayAspect)
	cmd := exec.Command("ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("final mux failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

func audioMetadataArgs(outputIndex int, stream media.AudioStreamInfo) []string {
	var args []string
	if stream.Language != "" {
		args = append(args, fmt.Sprintf("-metadata:s:a:%d", outputIndex), "language="+stream.Language)
	}
	if stream.Title != "" {
		args = append(args, fmt.Sprintf("-metadata:s:a:%d", outputIndex), "title="+stream.Title)
	}
	return args
}

func audioDispositionArgs(outputIndex int, disposition media.StreamDisposition) []string {
	return []string{fmt.Sprintf("-disposition:a:%d", outputIndex), audioDispositionValue(disposition)}
}

func audioDispositionValue(d media.StreamDisposition) string {
	flags := audioDispositionFlags(d)
	if len(flags) == 0 {
		return "0"
	}
	return strings.Join(flags, "+")
}

func audioDispositionFlags(d media.StreamDisposition) []string {
	var flags []string
	addFlag := func(enabled int, name string) {
		if enabled != 0 {
			flags = append(flags, name)
		}
	}
	addFlag(d.Default, "default")
	addFlag(d.Dub, "dub")
	addFlag(d.Original, "original")
	addFlag(d.Comment, "comment")
	addFlag(d.Lyrics, "lyrics")
	addFlag(d.Karaoke, "karaoke")
	addFlag(d.Forced, "forced")
	addFlag(d.HearingImpaired, "hearing_impaired")
	addFlag(d.VisualImpaired, "visual_impaired")
	addFlag(d.CleanEffects, "clean_effects")
	addFlag(d.AttachedPic, "attached_pic")
	addFlag(d.TimedThumbnails, "timed_thumbnails")
	return flags
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
