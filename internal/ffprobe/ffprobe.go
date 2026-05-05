// Package ffprobe provides functions for extracting media information using ffprobe.
package ffprobe

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// MediaInfo contains basic media information.
type MediaInfo struct {
	Duration    float64
	Width       int64
	Height      int64
	TotalFrames uint64
}

// VideoProperties contains video stream properties.
type VideoProperties struct {
	Width                uint32
	Height               uint32
	DurationSecs         float64
	SampleAspectRatioNum uint32
	SampleAspectRatioDen uint32
	HDRInfo              HDRInfo
}

// HDRInfo contains HDR-related information.
type HDRInfo struct {
	IsHDR                   bool
	ColourPrimaries         string
	TransferCharacteristics string
	MatrixCoefficients      string
	BitDepth                *uint8
}

// AudioStreamInfo contains information about an audio stream.
type AudioStreamInfo struct {
	Channels    uint32
	CodecName   string
	Profile     string
	Index       int
	IsSpatial   bool // Always false (spatial support removed)
	Disposition StreamDisposition
}

// StreamDisposition contains stream disposition flags.
type StreamDisposition struct {
	Default         int `json:"default"`
	Dub             int `json:"dub"`
	Original        int `json:"original"`
	Comment         int `json:"comment"`
	Lyrics          int `json:"lyrics"`
	Karaoke         int `json:"karaoke"`
	Forced          int `json:"forced"`
	HearingImpaired int `json:"hearing_impaired"`
	VisualImpaired  int `json:"visual_impaired"`
	CleanEffects    int `json:"clean_effects"`
	AttachedPic     int `json:"attached_pic"`
	TimedThumbnails int `json:"timed_thumbnails"`
}

// ffprobeOutput represents the JSON output from ffprobe.
type ffprobeOutput struct {
	Format  ffprobeFormat   `json:"format"`
	Streams []ffprobeStream `json:"streams"`
}

type ffprobeFormat struct {
	Duration string `json:"duration"`
}

type ffprobeStream struct {
	CodecType         string            `json:"codec_type"`
	CodecName         string            `json:"codec_name"`
	Profile           string            `json:"profile"`
	Width             int64             `json:"width"`
	Height            int64             `json:"height"`
	Channels          int               `json:"channels"`
	NbFrames          string            `json:"nb_frames"`
	PixFmt            string            `json:"pix_fmt"`
	ColorPrimaries    string            `json:"color_primaries"`
	ColorTransfer     string            `json:"color_transfer"`
	ColorSpace        string            `json:"color_space"`
	BitsPerRawSample  string            `json:"bits_per_raw_sample"`
	SampleAspectRatio string            `json:"sample_aspect_ratio"`
	Disposition       StreamDisposition `json:"disposition"`
}

// runFFprobe executes ffprobe and returns the parsed output.
func runFFprobe(inputPath string) (*ffprobeOutput, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		inputPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var result ffprobeOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	return &result, nil
}

// GetMediaInfo returns basic media information for a file.
func GetMediaInfo(inputPath string) (*MediaInfo, error) {
	probe, err := runFFprobe(inputPath)
	if err != nil {
		return nil, err
	}

	info := &MediaInfo{}

	// Parse duration from format
	if probe.Format.Duration != "" {
		if d, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil {
			info.Duration = d
		}
	}

	// Find video stream
	for _, stream := range probe.Streams {
		if stream.CodecType == "video" {
			info.Width = stream.Width
			info.Height = stream.Height
			if stream.NbFrames != "" {
				if frames, err := strconv.ParseUint(stream.NbFrames, 10, 64); err == nil {
					info.TotalFrames = frames
				}
			}
			break
		}
	}

	return info, nil
}

// GetVideoProperties returns video properties including HDR info.
func GetVideoProperties(inputPath string) (*VideoProperties, error) {
	probe, err := runFFprobe(inputPath)
	if err != nil {
		return nil, err
	}

	// Parse duration
	var durationSecs float64
	if probe.Format.Duration != "" {
		if d, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil {
			durationSecs = d
		} else {
			return nil, fmt.Errorf("failed to parse duration")
		}
	}

	// Find video stream
	var videoStream *ffprobeStream
	for i := range probe.Streams {
		if probe.Streams[i].CodecType == "video" {
			videoStream = &probe.Streams[i]
			break
		}
	}

	if videoStream == nil {
		return nil, fmt.Errorf("no video stream found in %s", inputPath)
	}

	if videoStream.Width <= 0 || videoStream.Height <= 0 {
		return nil, fmt.Errorf("invalid dimensions in %s: %dx%d", inputPath, videoStream.Width, videoStream.Height)
	}

	// Parse bit depth
	var bitDepth *uint8
	if videoStream.BitsPerRawSample != "" {
		if bd, err := strconv.ParseUint(videoStream.BitsPerRawSample, 10, 8); err == nil {
			bdVal := uint8(bd)
			bitDepth = &bdVal
		}
	}

	sarNum, sarDen := parseRatio(videoStream.SampleAspectRatio)

	// Detect HDR from color metadata
	hdrInfo := HDRInfo{
		ColourPrimaries:         videoStream.ColorPrimaries,
		TransferCharacteristics: videoStream.ColorTransfer,
		MatrixCoefficients:      videoStream.ColorSpace,
		BitDepth:                bitDepth,
		IsHDR:                   detectHDR(videoStream.ColorPrimaries, videoStream.ColorTransfer, videoStream.ColorSpace),
	}

	return &VideoProperties{
		Width:                uint32(videoStream.Width),
		Height:               uint32(videoStream.Height),
		DurationSecs:         durationSecs,
		SampleAspectRatioNum: sarNum,
		SampleAspectRatioDen: sarDen,
		HDRInfo:              hdrInfo,
	}, nil
}

func parseRatio(ratio string) (uint32, uint32) {
	numStr, denStr, ok := strings.Cut(ratio, ":")
	if !ok {
		return 0, 0
	}
	num, err := strconv.ParseUint(numStr, 10, 32)
	if err != nil || num == 0 {
		return 0, 0
	}
	den, err := strconv.ParseUint(denStr, 10, 32)
	if err != nil || den == 0 {
		return 0, 0
	}
	return uint32(num), uint32(den)
}

// GetAudioChannels returns the channel count for each audio stream.
func GetAudioChannels(inputPath string) ([]uint32, error) {
	probe, err := runFFprobe(inputPath)
	if err != nil {
		return nil, err
	}

	var channels []uint32
	for _, stream := range probe.Streams {
		if stream.CodecType == "audio" && stream.Channels > 0 {
			channels = append(channels, uint32(stream.Channels))
		}
	}

	return channels, nil
}

// GetAudioStreamInfo returns detailed audio stream information.
func GetAudioStreamInfo(inputPath string) ([]AudioStreamInfo, error) {
	probe, err := runFFprobe(inputPath)
	if err != nil {
		return nil, err
	}

	var streams []AudioStreamInfo
	audioIndex := 0

	for _, stream := range probe.Streams {
		if stream.CodecType != "audio" {
			continue
		}

		if stream.Channels <= 0 {
			continue
		}

		streams = append(streams, AudioStreamInfo{
			Channels:    uint32(stream.Channels),
			CodecName:   stream.CodecName,
			Profile:     stream.Profile,
			Index:       audioIndex,
			IsSpatial:   false, // Spatial audio support removed
			Disposition: stream.Disposition,
		})

		audioIndex++
	}

	return streams, nil
}

// detectHDR determines if content is HDR based on color metadata.
func detectHDR(primaries, transfer, matrix string) bool {
	// Check for HDR primaries (BT.2020)
	if containsCI(primaries, "bt2020") || containsCI(primaries, "bt.2020") || containsCI(primaries, "bt2100") {
		return true
	}

	// Check for HDR transfer characteristics (PQ, HLG)
	if containsCI(transfer, "pq") || containsCI(transfer, "smpte2084") || containsCI(transfer, "hlg") || containsCI(transfer, "arib-std-b67") {
		return true
	}

	// Check for HDR matrix coefficients
	if containsCI(matrix, "bt2020") || containsCI(matrix, "bt.2020") {
		return true
	}

	return false
}

// containsCI performs a case-insensitive substring check.
func containsCI(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// GetVideoCodecName returns the video codec name for a file.
func GetVideoCodecName(inputPath string) (string, error) {
	probe, err := runFFprobe(inputPath)
	if err != nil {
		return "", err
	}

	for _, stream := range probe.Streams {
		if stream.CodecType == "video" {
			return stream.CodecName, nil
		}
	}

	return "", fmt.Errorf("no video stream found in %s", inputPath)
}
