package chunk

import (
	"testing"

	"codeberg.org/five82/reel/internal/media"
)

func TestAudioDispositionBitmask(t *testing.T) {
	disposition := media.StreamDisposition{Default: 1, Forced: 1, HearingImpaired: 1}

	got := audioDisposition(disposition)
	want := avDispositionDefault | avDispositionForced | avDispositionHearingImpaired
	if got != want {
		t.Fatalf("audioDisposition() = %d, want %d", got, want)
	}
}

func TestParseDisplayAspect(t *testing.T) {
	num, den, err := parseDisplayAspect("16:9")
	if err != nil {
		t.Fatalf("parseDisplayAspect() error = %v", err)
	}
	if num != 16 || den != 9 {
		t.Fatalf("parseDisplayAspect() = %d:%d, want 16:9", num, den)
	}
}
