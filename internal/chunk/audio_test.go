package chunk

import (
	"slices"
	"testing"

	"github.com/five82/reel/internal/ffprobe"
)

func TestAudioMetadataArgs(t *testing.T) {
	stream := ffprobe.AudioStreamInfo{Language: "jpn", Title: "Commentary"}

	args := audioMetadataArgs(1, stream)
	assertArgValue(t, args, "-metadata:s:a:1", "language=jpn")
	assertArgValue(t, args, "-metadata:s:a:1", "title=Commentary")
}

func TestAudioDispositionValue(t *testing.T) {
	disposition := ffprobe.StreamDisposition{Default: 1, Forced: 1, HearingImpaired: 1}

	got := audioDispositionValue(disposition)
	want := "default+forced+hearing_impaired"
	if got != want {
		t.Fatalf("audioDispositionValue() = %q, want %q", got, want)
	}
}

func TestAudioDispositionValueClearsEmptyDisposition(t *testing.T) {
	if got := audioDispositionValue(ffprobe.StreamDisposition{}); got != "0" {
		t.Fatalf("audioDispositionValue() = %q, want %q", got, "0")
	}
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
