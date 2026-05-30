package encode

import "testing"

func TestSampledProbeWindowsUsesFullChunkForShortChunks(t *testing.T) {
	windows := sampledProbeWindows(225, 48, 256)
	if len(windows) != 1 {
		t.Fatalf("len(windows) = %d, want 1", len(windows))
	}
	if windows[0].Offset != 0 || windows[0].Frames != 225 {
		t.Fatalf("window = %+v, want full 225-frame chunk", windows[0])
	}
}

func TestSampledProbeWindowsUsesStartMiddleEndForLongChunks(t *testing.T) {
	windows := sampledProbeWindows(602, 48, 256)
	want := []probeSampleWindow{
		{Offset: 0, Frames: 48},
		{Offset: 277, Frames: 48},
		{Offset: 554, Frames: 48},
	}
	if len(windows) != len(want) {
		t.Fatalf("len(windows) = %d, want %d", len(windows), len(want))
	}
	for i := range want {
		if windows[i] != want[i] {
			t.Fatalf("windows[%d] = %+v, want %+v", i, windows[i], want[i])
		}
	}
}

func TestSampledProbeWindowsAvoidsOverlappingSamples(t *testing.T) {
	windows := sampledProbeWindows(144, 48, 0)
	if len(windows) != 1 {
		t.Fatalf("len(windows) = %d, want 1", len(windows))
	}
	if windows[0].Offset != 0 || windows[0].Frames != 144 {
		t.Fatalf("window = %+v, want full 144-frame chunk", windows[0])
	}
}
