package chunk

import (
	"slices"
	"testing"

	nativeaudio "codeberg.org/five82/reel/internal/audio"
	"codeberg.org/five82/reel/internal/media"
)

func TestAudioMetadataArgs(t *testing.T) {
	stream := media.AudioStreamInfo{Language: "jpn", Title: "Commentary"}

	args := audioMetadataArgs(1, stream)
	assertArgValue(t, args, "-metadata:s:a:1", "language=jpn")
	assertArgValue(t, args, "-metadata:s:a:1", "title=Commentary")
}

func TestAudioDispositionValue(t *testing.T) {
	disposition := media.StreamDisposition{Default: 1, Forced: 1, HearingImpaired: 1}

	got := audioDispositionValue(disposition)
	want := "default+forced+hearing_impaired"
	if got != want {
		t.Fatalf("audioDispositionValue() = %q, want %q", got, want)
	}
}

func TestAudioDispositionValueClearsEmptyDisposition(t *testing.T) {
	if got := audioDispositionValue(media.StreamDisposition{}); got != "0" {
		t.Fatalf("audioDispositionValue() = %q, want %q", got, "0")
	}
}

func TestMuxFinalArgsMapsPerStreamOpusFiles(t *testing.T) {
	streams := []nativeaudio.EncodedStream{
		{Info: media.AudioStreamInfo{Language: "eng", Disposition: media.StreamDisposition{Default: 1}}, Path: "/tmp/audio_00.opus"},
		{Info: media.AudioStreamInfo{Language: "jpn", Title: "Commentary"}, Path: "/tmp/audio_01.opus"},
	}

	args := muxFinalArgs("input.mkv", "video.ivf", "output.mkv", streams, "16:9")
	assertArgValue(t, args, "-i", "video.ivf")
	assertArgValue(t, args, "-i", "/tmp/audio_00.opus")
	assertArgValue(t, args, "-i", "/tmp/audio_01.opus")
	assertArgValue(t, args, "-map", "1:a:0")
	assertArgValue(t, args, "-map", "2:a:0")
	assertArgValue(t, args, "-map", "3:s?")
	assertArgValue(t, args, "-map_chapters", "3")
	assertArgValue(t, args, "-aspect:v:0", "16:9")
	assertArgValue(t, args, "-metadata:s:a:1", "title=Commentary")
}

func assertArgValue(t *testing.T, args []string, key, want string) {
	t.Helper()
	idx := slices.Index(args, key)
	for idx != -1 && idx+1 < len(args) {
		if args[idx+1] == want {
			return
		}
		next := slices.Index(args[idx+2:], key)
		if next == -1 {
			break
		}
		idx += next + 2
	}
	t.Fatalf("%s %q not found in args: %v", key, want, args)
}
