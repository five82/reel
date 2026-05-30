package encode

import (
	"math"
	"testing"

	"codeberg.org/five82/reel/internal/chunk"
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

func TestTargetQualitySampleScoreUsesMeanWhenWindowsMatch(t *testing.T) {
	windows := []quality.ProbeWindow{
		{Offset: 0, Frames: 48, Score: 9.5},
		{Offset: 100, Frames: 48, Score: 9.5},
		{Offset: 200, Frames: 48, Score: 9.5},
	}
	score, meanScore, worstScore, frames := targetQualitySampleScore(windows)
	if score != 9.5 || meanScore != 9.5 || worstScore != 9.5 || frames != 144 {
		t.Fatalf("score=%g mean=%g worst=%g frames=%d, want all 9.5 and 144 frames", score, meanScore, worstScore, frames)
	}
}

func TestTargetQualitySampleScorePenalizesWeakWindow(t *testing.T) {
	windows := []quality.ProbeWindow{
		{Offset: 0, Frames: 48, Score: 9.65},
		{Offset: 100, Frames: 48, Score: 9.58},
		{Offset: 200, Frames: 48, Score: 9.25},
	}
	score, meanScore, worstScore, frames := targetQualitySampleScore(windows)
	if math.Abs(float64(meanScore-9.493333)) > 0.0001 {
		t.Fatalf("meanScore = %g, want 9.493333", meanScore)
	}
	if worstScore != 9.25 || frames != 144 {
		t.Fatalf("worst=%g frames=%d, want 9.25 and 144", worstScore, frames)
	}
	if math.Abs(float64(score-9.371667)) > 0.0001 {
		t.Fatalf("score = %g, want midpoint of mean and worst", score)
	}
}

func TestTargetQualityFullFirstProbeRequiresReliableInitialSource(t *testing.T) {
	if !targetQualityFullFirstProbe("neighbor", 1, 500, 48, 256) {
		t.Fatal("neighbor first probe should use full-first probing")
	}
	if !targetQualityFullFirstProbe("median", 1, 500, 48, 256) {
		t.Fatal("median first probe should use full-first probing")
	}
	if targetQualityFullFirstProbe("default", 1, 500, 48, 256) {
		t.Fatal("default first probe should not use full-first probing")
	}
}

func TestTargetQualityFullFirstProbeOnlyForFirstSampledProbe(t *testing.T) {
	if targetQualityFullFirstProbe("neighbor", 2, 500, 48, 256) {
		t.Fatal("later probes should not use full-first probing")
	}
	if targetQualityFullFirstProbe("neighbor", 1, 225, 48, 256) {
		t.Fatal("chunks already full-probed should not use full-first sampled probing")
	}
	if !targetQualityFullFirstProbe("neighbor", 1, 650, 48, 256) {
		t.Fatal("HD-sized chunks should use full-first probing")
	}
	if targetQualityFullFirstProbe("neighbor", 1, 721, 48, 256) {
		t.Fatal("chunks above the full-first cap should not use full-first probing")
	}
}

func TestOrderTargetQualityChunksSortsLargestFirstWithinTimelineBlocks(t *testing.T) {
	chunks := []chunk.Chunk{
		{Idx: 3, Start: 0, End: 10},
		{Idx: 0, Start: 0, End: 20},
		{Idx: 1, Start: 0, End: 40},
		{Idx: 2, Start: 0, End: 30},
		{Idx: 4, Start: 0, End: 60},
		{Idx: 5, Start: 0, End: 50},
	}
	ordered := orderTargetQualityChunks(chunks, 3)
	got := make([]int, len(ordered))
	for i, ch := range ordered {
		got[i] = ch.Idx
	}
	want := []int{1, 2, 0, 4, 5, 3}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
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
