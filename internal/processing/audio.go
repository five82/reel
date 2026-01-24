package processing

import (
	"fmt"
	"strings"

	"github.com/five82/reel/internal/ffmpeg"
	"github.com/five82/reel/internal/ffprobe"
)

// GetAudioChannels returns audio channel counts for a file.
func GetAudioChannels(inputPath string) []uint32 {
	channels, err := ffprobe.GetAudioChannels(inputPath)
	if err != nil {
		return nil
	}
	return channels
}

// GetAudioStreamInfo returns detailed audio stream information.
func GetAudioStreamInfo(inputPath string) []ffprobe.AudioStreamInfo {
	streams, err := ffprobe.GetAudioStreamInfo(inputPath)
	if err != nil {
		return nil
	}
	return streams
}

// FormatAudioDescription formats a basic audio description.
func FormatAudioDescription(channels []uint32) string {
	if len(channels) == 0 {
		return "No audio"
	}

	if len(channels) == 1 {
		return fmt.Sprintf("%d channels", channels[0])
	}

	var parts []string
	for i, ch := range channels {
		parts = append(parts, fmt.Sprintf("Stream %d (%dch)", i, ch))
	}
	return fmt.Sprintf("%d streams: %s", len(channels), strings.Join(parts, ", "))
}

// FormatAudioDescriptionConfig formats audio description for config display.
func FormatAudioDescriptionConfig(channels []uint32, streams []ffprobe.AudioStreamInfo) string {
	if streams == nil {
		return FormatAudioDescription(channels)
	}

	if len(streams) == 0 {
		return "No audio"
	}

	if len(streams) == 1 {
		stream := streams[0]
		bitrate := ffmpeg.CalculateAudioBitrate(stream.Channels)
		return fmt.Sprintf("%d channels @ %dkbps Opus", stream.Channels, bitrate)
	}

	var parts []string
	for _, stream := range streams {
		bitrate := ffmpeg.CalculateAudioBitrate(stream.Channels)
		parts = append(parts, fmt.Sprintf("Stream %d: %dch [%dkbps Opus]", stream.Index, stream.Channels, bitrate))
	}
	return strings.Join(parts, ", ")
}

// GenerateAudioResultsDescription generates audio description for results.
func GenerateAudioResultsDescription(channels []uint32, streams []ffprobe.AudioStreamInfo) string {
	if len(streams) > 0 {
		if len(streams) == 1 {
			bitrate := ffmpeg.CalculateAudioBitrate(streams[0].Channels)
			return fmt.Sprintf("Opus %dch @ %dkbps", streams[0].Channels, bitrate)
		}

		var parts []string
		for _, stream := range streams {
			bitrate := ffmpeg.CalculateAudioBitrate(stream.Channels)
			parts = append(parts, fmt.Sprintf("%dch@%dk", stream.Channels, bitrate))
		}
		return fmt.Sprintf("Opus (%s)", strings.Join(parts, ", "))
	}

	if len(channels) == 0 {
		return "No audio"
	}

	if len(channels) == 1 {
		bitrate := ffmpeg.CalculateAudioBitrate(channels[0])
		return fmt.Sprintf("Opus %dch @ %dkbps", channels[0], bitrate)
	}

	var parts []string
	for _, ch := range channels {
		bitrate := ffmpeg.CalculateAudioBitrate(ch)
		parts = append(parts, fmt.Sprintf("%dch@%dk", ch, bitrate))
	}
	return fmt.Sprintf("Opus (%s)", strings.Join(parts, ", "))
}
