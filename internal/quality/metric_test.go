package quality

import (
	"testing"

	"codeberg.org/five82/reel/internal/video"
)

func trc(v int32) *int32 { return &v }

func TestProbeMetricForSource(t *testing.T) {
	cases := []struct {
		name string
		inf  *video.Info
		want MetricKind
	}{
		{"nil info", nil, MetricCVVDP},
		{"sdr 1080p", &video.Info{Width: 1920, Height: 1080}, MetricSSIMU2},
		{"sdr 1080p scope crop source", &video.Info{Width: 1920, Height: 816}, MetricSSIMU2},
		{"sdr 720p", &video.Info{Width: 1280, Height: 720}, MetricSSIMU2},
		{"hdr pq 1080p", &video.Info{Width: 1920, Height: 1080, TransferCharacteristics: trc(16)}, MetricCVVDP},
		{"hdr hlg 1080p", &video.Info{Width: 1920, Height: 1080, TransferCharacteristics: trc(18)}, MetricCVVDP},
		{"sdr 4k", &video.Info{Width: 3840, Height: 2160}, MetricCVVDP},
		{"sdr 1440p", &video.Info{Width: 2560, Height: 1440}, MetricCVVDP},
	}
	for _, tc := range cases {
		if got := ProbeMetricForSource(tc.inf); got != tc.want {
			t.Errorf("%s: ProbeMetricForSource() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestMetricScaledConstants(t *testing.T) {
	approx := func(got, want float32) bool {
		d := got - want
		return d < 1e-5 && d > -1e-5
	}
	if got := MetricCVVDP.DefaultSlopePerCRF(); !approx(got, 0.025) {
		t.Errorf("CVVDP DefaultSlopePerCRF = %g, want 0.025", got)
	}
	if got := MetricSSIMU2.DefaultSlopePerCRF(); !approx(got, 0.9) {
		t.Errorf("SSIMU2 DefaultSlopePerCRF = %g, want 0.9", got)
	}
	if lo, hi := MetricCVVDP.SlopeClamp(); !approx(lo, 0.005) || !approx(hi, 0.2) {
		t.Errorf("CVVDP SlopeClamp = %g..%g, want 0.005..0.2", lo, hi)
	}
	if lo, hi := MetricSSIMU2.SlopeClamp(); !approx(lo, 0.18) || !approx(hi, 7.2) {
		t.Errorf("SSIMU2 SlopeClamp = %g..%g, want 0.18..7.2", lo, hi)
	}
	// Zero value must behave as CVVDP so existing callers are unchanged.
	if got := MetricKind("").ScoreScale(); got != 1 {
		t.Errorf("zero MetricKind ScoreScale = %g, want 1", got)
	}
}

// The second-probe step logic must scale with the metric: a -10.8 point
// SSIMU2 miss is the same perceptual miss as -0.30 JOD and must produce the
// same CRF step through the scaled slope.
func TestSearchSecondProbeStepSSIMU2Scale(t *testing.T) {
	ctx := SearchContext{
		Metric:     MetricSSIMU2,
		Target:     SSIMU2Target,
		Tolerance:  SSIMU2Tolerance,
		CRFMin:     4.25,
		CRFMax:     63.75,
		MaxProbes:  6,
		InitialCRF: 26,
		JODPerCRF:  MetricSSIMU2.DefaultSlopePerCRF(),
	}
	state := NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 26, Score: SSIMU2Target - 10.8})
	crf, ok := state.NextCRF(ctx)
	if !ok {
		t.Fatal("no second CRF")
	}
	// step = 10.8 / 0.9 = 12, under the 20-CRF cap for a >=0.25-JOD-equivalent miss.
	if crf != 14 {
		t.Fatalf("second CRF = %g, want 14", crf)
	}
}
