package encoder

import (
	"slices"
	"testing"

	"github.com/five82/reel/internal/ffms"
)

func TestBuildSvtArgsIncludesRangeAndChromaPosition(t *testing.T) {
	colorRange := int32(1)
	chromaPosition := int32(2)
	cfg := &EncConfig{
		Inf: &ffms.VidInf{
			FPSNum:               24000,
			FPSDen:               1001,
			ColorRange:           &colorRange,
			ChromaSamplePosition: &chromaPosition,
		},
		Width:  1920,
		Height: 1080,
		Frames: 24,
	}

	args := buildSvtArgs(cfg)
	assertArgValue(t, args, "--color-range", "1")
	assertArgValue(t, args, "--chroma-sample-position", "colocated")
}

func assertArgValue(t *testing.T, args []string, key, want string) {
	t.Helper()
	idx := slices.Index(args, key)
	if idx == -1 || idx+1 >= len(args) {
		t.Fatalf("%s not found in args: %v", key, args)
	}
	if got := args[idx+1]; got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}
