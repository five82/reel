package encode

import (
	"context"
	"errors"
	"math"
	"testing"

	"codeberg.org/five82/reel/internal/chunk"
	"codeberg.org/five82/reel/internal/quality"
	"codeberg.org/five82/reel/internal/video"
)

func TestSampledProbeWindowsUsesFullChunkForShortChunks(t *testing.T) {
	windows := sampledProbeWindows(225, 48, 256, false)
	if len(windows) != 1 {
		t.Fatalf("len(windows) = %d, want 1", len(windows))
	}
	if windows[0].Offset != 0 || windows[0].Frames != 225 {
		t.Fatalf("window = %+v, want full 225-frame chunk", windows[0])
	}
}

func TestSampledProbeWindowsUsesStartMiddleEndForLongChunks(t *testing.T) {
	windows := sampledProbeWindows(602, 48, 256, false)
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

func TestSampledProbeWindowsUsesFiveWindowsWhenExtraSampling(t *testing.T) {
	windows := sampledProbeWindows(602, 48, 256, true)
	want := []probeSampleWindow{
		{Offset: 0, Frames: 48},
		{Offset: 139, Frames: 48},
		{Offset: 277, Frames: 48},
		{Offset: 416, Frames: 48},
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
	windows := sampledProbeWindows(144, 48, 0, false)
	if len(windows) != 1 {
		t.Fatalf("len(windows) = %d, want 1", len(windows))
	}
	if windows[0].Offset != 0 || windows[0].Frames != 144 {
		t.Fatalf("window = %+v, want full 144-frame chunk", windows[0])
	}
}

func TestSampledProbeWindowsFullProbesWhenExtraSamplesWouldOverlap(t *testing.T) {
	windows := sampledProbeWindows(220, 48, 0, true)
	if len(windows) != 1 {
		t.Fatalf("len(windows) = %d, want 1", len(windows))
	}
	if windows[0].Offset != 0 || windows[0].Frames != 220 {
		t.Fatalf("window = %+v, want full 220-frame chunk", windows[0])
	}
}

func TestFormatProbeWindowScoresShowsOffsetAndScore(t *testing.T) {
	windows := []quality.ProbeWindow{
		{Offset: 0, Frames: 48, Score: 9.65},
		{Offset: 277, Frames: 48, Score: 9.58},
		{Offset: 554, Frames: 48, Score: 9.25},
	}
	if got := formatProbeWindowScores(windows); got != "[0:9.6500,277:9.5800,554:9.2500]" {
		t.Fatalf("formatProbeWindowScores() = %q", got)
	}
}

func TestTargetQualityWindowSpread(t *testing.T) {
	windows := []quality.ProbeWindow{
		{Offset: 0, Frames: 48, Score: 9.65},
		{Offset: 277, Frames: 48, Score: 9.58},
		{Offset: 554, Frames: 48, Score: 9.25},
	}
	if got := targetQualityWindowSpread(windows); math.Abs(float64(got-0.4)) > 0.0001 {
		t.Fatalf("targetQualityWindowSpread() = %g, want 0.4", got)
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
	// Spread = 0.40, weight = min(0.40/0.30, 0.70) = 0.70
	// score = mean*0.30 + worst*0.70 = 9.4933*0.30 + 9.25*0.70 = 9.323
	wantScore := float32(9.493333*0.30 + 9.25*0.70)
	if math.Abs(float64(score-wantScore)) > 0.0001 {
		t.Fatalf("score = %g, want %g (weighted toward worst for high spread)", score, wantScore)
	}
}

func TestTargetQualitySampleScoreUsesMeanForLowSpread(t *testing.T) {
	windows := []quality.ProbeWindow{
		{Offset: 0, Frames: 48, Score: 9.50},
		{Offset: 100, Frames: 48, Score: 9.48},
		{Offset: 200, Frames: 48, Score: 9.46},
	}
	score, meanScore, worstScore, frames := targetQualitySampleScore(windows)
	if frames != 144 {
		t.Fatalf("frames = %d, want 144", frames)
	}
	// Spread = 0.04, weight = 0.04/0.30 = 0.133
	// score should be close to mean, not strongly pulled toward worst
	if score <= worstScore || score >= meanScore {
		t.Fatalf("score = %g, want between worst=%g and mean=%g for low spread", score, worstScore, meanScore)
	}
}

func TestFormatMultiProbeChunksShowsFirstAndLastProbe(t *testing.T) {
	logs := []chunkTargetLog{
		{
			ChunkIdx:   2,
			StopReason: quality.StopMaxProbes,
			Probes: []quality.Probe{
				{CRF: 27, Score: 9.9255},
				{CRF: 41.5, Score: 9.8751},
				{CRF: 58.5, Score: 9.7},
			},
		},
	}
	got := formatMultiProbeChunks(logs, 8)
	want := "[0002:3 probes crf 27->58.5 jod 9.9255->9.7000 stop=max_probes]"
	if got != want {
		t.Fatalf("formatMultiProbeChunks() = %q, want %q", got, want)
	}
}

func TestTargetQualityInitialJODPerCRFUsesLowerSlopeForLargeOrHDR(t *testing.T) {
	hdrTransfer := int32(16)
	if got := targetQualityInitialJODPerCRF(1920, 1080, &video.Info{}); got != targetQualityDefaultJODPerCRF {
		t.Fatalf("SDR HD JOD/CRF = %g, want %g", got, targetQualityDefaultJODPerCRF)
	}
	if got := targetQualityInitialJODPerCRF(3840, 1600, &video.Info{}); got != targetQualityLargeJODPerCRF {
		t.Fatalf("large-frame JOD/CRF = %g, want %g", got, targetQualityLargeJODPerCRF)
	}
	if got := targetQualityInitialJODPerCRF(1920, 1080, &video.Info{TransferCharacteristics: &hdrTransfer}); got != targetQualityLargeJODPerCRF {
		t.Fatalf("HDR JOD/CRF = %g, want %g", got, targetQualityLargeJODPerCRF)
	}
}

func TestTargetQualityFullFirstProbeRequiresMedianInitialSource(t *testing.T) {
	if targetQualityFullFirstProbe("neighbor", 1, 500, 48, 256) {
		t.Fatal("neighbor first probe should not use full-first probing")
	}
	if !targetQualityFullFirstProbe("median", 1, 500, 48, 256) {
		t.Fatal("median first probe should use full-first probing")
	}
	if targetQualityFullFirstProbe("default", 1, 500, 48, 256) {
		t.Fatal("default first probe should not use full-first probing")
	}
}

func TestTargetQualityFullFirstProbeOnlyForFirstSampledProbe(t *testing.T) {
	if targetQualityFullFirstProbe("median", 2, 500, 48, 256) {
		t.Fatal("later probes should not use full-first probing")
	}
	if targetQualityFullFirstProbe("median", 1, 225, 48, 256) {
		t.Fatal("chunks already full-probed should not use full-first sampled probing")
	}
	if !targetQualityFullFirstProbe("median", 1, 650, 48, 256) {
		t.Fatal("HD-sized chunks should use full-first probing")
	}
	if targetQualityFullFirstProbe("median", 1, 721, 48, 256) {
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
	prior := newTargetQualityPrior(26, 4.25, 63.75, 9.5, targetQualityDefaultJODPerCRF)
	crf, source := prior.InitialCRF(100)
	if crf != 26 || source != "default" {
		t.Fatalf("InitialCRF = %g %q, want 26 default", crf, source)
	}
}

func TestTargetQualityPriorUsesWeightedNeighborCRF(t *testing.T) {
	prior := newTargetQualityPrior(26, 4.25, 63.75, 9.5, targetQualityDefaultJODPerCRF)
	prior.AddResult(96, 20, nil)
	prior.AddResult(104, 28, nil)
	crf, source := prior.InitialCRF(100)
	if crf != 24 || source != "neighbor" {
		t.Fatalf("InitialCRF = %g %q, want 24 neighbor", crf, source)
	}
}

func TestTargetQualityPriorFallsBackToMedianForDistantHistory(t *testing.T) {
	prior := newTargetQualityPrior(26, 4.25, 63.75, 9.5, targetQualityDefaultJODPerCRF)
	prior.AddResult(10, 20, nil)
	prior.AddResult(20, 30, nil)
	prior.AddResult(30, 40, nil)
	crf, source := prior.InitialCRF(100)
	if crf != 30 || source != "median" {
		t.Fatalf("InitialCRF = %g %q, want 30 median", crf, source)
	}
}

func TestTargetQualityPriorLearnsJODPerCRF(t *testing.T) {
	prior := newTargetQualityPrior(26, 4.25, 63.75, 9.5, targetQualityDefaultJODPerCRF)
	prior.AddResult(10, 25, []quality.Probe{
		{CRF: 20, Score: 9.7},
		{CRF: 30, Score: 9.3},
	})
	if got := prior.JODPerCRF(); math.Abs(float64(got-0.04)) > 0.0001 {
		t.Fatalf("JODPerCRF = %g, want 0.04", got)
	}
}

func TestTargetQualityPriorNormalizesCRFToTarget(t *testing.T) {
	prior := newTargetQualityPrior(26, 4.25, 63.75, 9.5, targetQualityDefaultJODPerCRF)
	prior.AddResult(10, 25, []quality.Probe{
		{CRF: 20, Score: 9.7},
		{CRF: 25, Score: 9.58},
		{CRF: 30, Score: 9.3},
	})
	crf, source := prior.InitialCRF(11)
	if crf != 27 || source != "neighbor" {
		t.Fatalf("InitialCRF = %g %q, want 27 neighbor", crf, source)
	}
}

func TestTargetQualityPriorNormalizesCRFWithAdjustmentCap(t *testing.T) {
	prior := newTargetQualityPrior(26, 4.25, 63.75, 9.5, targetQualityDefaultJODPerCRF)
	prior.AddResult(10, 25, []quality.Probe{{CRF: 25, Score: 9.9}})
	crf, source := prior.InitialCRF(11)
	if crf != 28 || source != "neighbor" {
		t.Fatalf("InitialCRF = %g %q, want 28 neighbor", crf, source)
	}
}

func TestGatherWindowScoresOrdersAndReacquires(t *testing.T) {
	limiter := newAdaptiveLimiter(2, 2, 2, 0, nil)
	if _, err := limiter.acquire(context.Background()); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	scoreCh := make(chan windowScore, 3)
	scoreCh <- windowScore{idx: 2, offset: 200}
	scoreCh <- windowScore{idx: 0, offset: 0}
	scoreCh <- windowScore{idx: 1, offset: 100}
	scores, err := gatherWindowScores(context.Background(), limiter, scoreCh, 3)
	if err != nil {
		t.Fatalf("gatherWindowScores: %v", err)
	}
	for i, ws := range scores {
		if ws.idx != i {
			t.Fatalf("scores out of order: got idx %d at position %d", ws.idx, i)
		}
	}
	active, _, _ := limiter.stats()
	if active != 1 {
		t.Fatalf("expected slot re-acquired (active=1), got active=%d", active)
	}
}

func TestGatherWindowScoresReturnsFirstError(t *testing.T) {
	limiter := newAdaptiveLimiter(2, 2, 2, 0, nil)
	if _, err := limiter.acquire(context.Background()); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	scoreCh := make(chan windowScore, 2)
	scoreCh <- windowScore{idx: 0}
	scoreCh <- windowScore{idx: 1, err: errors.New("boom")}
	if _, err := gatherWindowScores(context.Background(), limiter, scoreCh, 2); err == nil {
		t.Fatal("expected error from failed window score")
	}
}
