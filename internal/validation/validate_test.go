package validation

import "testing"

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

func assertAspect(t *testing.T, got *[2]uint32, want [2]uint32) {
	t.Helper()
	if got == nil {
		t.Fatalf("got nil, want %d:%d", want[0], want[1])
	}
	if *got != want {
		t.Fatalf("got %d:%d, want %d:%d", got[0], got[1], want[0], want[1])
	}
}
