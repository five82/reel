package validation

import (
	"math"
	"strings"
	"testing"

	"github.com/five82/reel/internal/media"
)

func TestDisplayAspectUsesSampleAspectRatio(t *testing.T) {
	got := displayAspect(720, 360, 32, 27)
	assertAspect(t, got, [2]uint32{64, 27})
}

func TestDisplayAspectDefaultsMissingSARToSquarePixels(t *testing.T) {
	got := displayAspect(1920, 1080, 0, 0)
	assertAspect(t, got, [2]uint32{16, 9})
}

func TestValidateDisplayAspect(t *testing.T) {
	actual := [2]uint32{64, 27}
	ok, _ := validateDisplayAspect(&actual, [2]uint32{64, 27})
	if !ok {
		t.Fatal("expected matching display aspect to pass")
	}

	ok, _ = validateDisplayAspect(&actual, [2]uint32{16, 9})
	if ok {
		t.Fatal("expected mismatched display aspect to fail")
	}
}

func TestValidateSyncComparesEachTrackOffset(t *testing.T) {
	input := []media.AudioStreamInfo{
		{Index: 0, StartOffsetSecs: 0.501},
		{Index: 1, StartOffsetSecs: 0},
	}
	output := []media.AudioStreamInfo{
		{Index: 0, StartOffsetSecs: 0.5005},
		{Index: 1, StartOffsetSecs: 0.0001},
	}

	ok, drift, message := validateSync(input, output)
	if !ok {
		t.Fatalf("validateSync() passed offsets = false, want true: %s", message)
	}
	if drift == nil || math.Abs(*drift-0.5) > 0.001 {
		t.Fatalf("validateSync() maximum drift = %v, want 0.5ms", drift)
	}
	if !strings.Contains(message, "maximum drift: 0.5ms") {
		t.Fatalf("validateSync() message = %q, want maximum drift", message)
	}
}

func TestValidateSyncIdentifiesDriftedTrack(t *testing.T) {
	input := []media.AudioStreamInfo{
		{Index: 0, StartOffsetSecs: 0.501},
		{Index: 1, StartOffsetSecs: 0},
	}
	output := []media.AudioStreamInfo{
		{Index: 0, StartOffsetSecs: 0},
		{Index: 1, StartOffsetSecs: 0},
	}

	ok, drift, message := validateSync(input, output)
	if ok {
		t.Fatal("validateSync() = true, want false")
	}
	if drift == nil || math.Abs(*drift-501) > 0.001 {
		t.Fatalf("validateSync() maximum drift = %v, want 501ms", drift)
	}
	if !strings.Contains(message, "track 1") || !strings.Contains(message, "maximum drift: 501.0ms") {
		t.Fatalf("validateSync() message = %q, want track and maximum drift", message)
	}
}

func assertAspect(t *testing.T, got *[2]uint32, want [2]uint32) {
	t.Helper()
	if got == nil {
		t.Fatalf("got nil, want %d:%d", want[0], want[1])
	}
	if *got != want {
		t.Fatalf("got %d:%d, want %d:%d", got[0], got[1], want[0], want[1])
	}
}
