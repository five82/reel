package encode

import (
	"math"
	"testing"

	"codeberg.org/five82/reel/internal/quality"
)

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

func TestTargetQualityPriorUsesDefaultWithoutHistory(t *testing.T) {
	prior := newTargetQualityPrior(26, 4.25, 63.75)
	crf, source := prior.InitialCRF(100)
	if crf != 26 || source != "default" {
		t.Fatalf("InitialCRF = %g %q, want 26 default", crf, source)
	}
}

func TestTargetQualityPriorUsesWeightedNeighborCRF(t *testing.T) {
	prior := newTargetQualityPrior(26, 4.25, 63.75)
	prior.Add(96, 20)
	prior.Add(104, 28)
	crf, source := prior.InitialCRF(100)
	if crf != 24 || source != "neighbor" {
		t.Fatalf("InitialCRF = %g %q, want 24 neighbor", crf, source)
	}
}

func TestTargetQualityPriorFallsBackToMedianForDistantHistory(t *testing.T) {
	prior := newTargetQualityPrior(26, 4.25, 63.75)
	prior.Add(10, 20)
	prior.Add(20, 30)
	prior.Add(30, 40)
	crf, source := prior.InitialCRF(100)
	if crf != 30 || source != "median" {
		t.Fatalf("InitialCRF = %g %q, want 30 median", crf, source)
	}
}

func TestTargetQualityPriorLearnsJODPerCRF(t *testing.T) {
	prior := newTargetQualityPrior(26, 4.25, 63.75)
	prior.AddResult(10, 25, []quality.Probe{
		{CRF: 20, Score: 9.7},
		{CRF: 30, Score: 9.3},
	})
	if got := prior.JODPerCRF(); math.Abs(float64(got-0.04)) > 0.0001 {
		t.Fatalf("JODPerCRF = %g, want 0.04", got)
	}
}
