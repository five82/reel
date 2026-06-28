package quality

import (
	"math"
	"testing"
)

// Constants are pinned to the libvship reference values; if libvship ever
// changes them these must move together.
func TestCVVDPJODConstantsMatchVship(t *testing.T) {
	if cvvdpJODA != 0.0439569391310215 {
		t.Fatalf("cvvdpJODA = %v, want libvship 0.0439569391310215", cvvdpJODA)
	}
	if cvvdpJODExp != 0.9302042722702026 {
		t.Fatalf("cvvdpJODExp = %v, want libvship 0.9302042722702026", cvvdpJODExp)
	}
}

func TestCVVDPJODBoundaryValues(t *testing.T) {
	if got := CVVDPDistanceToJOD(0); math.Abs(got-10) > 1e-9 {
		t.Fatalf("CVVDPDistanceToJOD(0) = %v, want 10", got)
	}
	if got := CVVDPJODToDistance(10); math.Abs(got) > 1e-9 {
		t.Fatalf("CVVDPJODToDistance(10) = %v, want 0", got)
	}
}

func TestCVVDPJODKinkIsContinuous(t *testing.T) {
	// The power/linear branches must agree at the kink distance.
	want := CVVDPDistanceToJOD(cvvdpJODKink)
	// Evaluate the power branch directly just below the branch boundary.
	powerBranch := 10 - cvvdpJODA*math.Pow(cvvdpJODKink, cvvdpJODExp)
	if math.Abs(want-powerBranch) > 1e-12 {
		t.Fatalf("kink discontinuity: piecewise=%v power=%v", want, powerBranch)
	}
	if math.Abs(want-9.994837938538057) > 1e-9 {
		t.Fatalf("toJOD(kink) = %v, want 9.994837938538057", want)
	}
}

func TestCVVDPJODRoundTrip(t *testing.T) {
	// Across reel's operating range and beyond, the two functions invert each
	// other to float64 precision.
	scores := []float64{10.0, 9.995, 9.9, 9.8, 9.5, 9.35, 9.0, 8.5, 8.0, 7.0}
	for _, s := range scores {
		d := CVVDPJODToDistance(s)
		back := CVVDPDistanceToJOD(d)
		if math.Abs(back-s) > 1e-6 {
			t.Fatalf("round trip score %v: distance=%v -> %v, want %v", s, d, back, s)
		}
	}
}

func TestCVVDPJODDistanceSpaceMeanIsConservative(t *testing.T) {
	// jod(d) = 10 - a*d^exp is CONVEX in d (exp < 1), so by Jensen's inequality
	// jod(mean(distances)) <= mean(jod(distances)). A distance-space mean must
	// therefore never exceed the naive JOD-space mean. This is the property the
	// sample-score aggregation relies on: pooling in distance space yields the
	// same, more-conservative score that full-chunk CVVDP (which pools in
	// distance space internally) reports.
	cases := [][]float64{
		{9.5, 9.5, 9.5},
		{9.65, 9.58, 9.25},
		{9.50, 9.48, 9.46},
		{8.5, 9.8},
		{9.2, 9.35, 9.5},
	}
	for _, windows := range cases {
		var jodMean, distSum float64
		for _, w := range windows {
			jodMean += w
			distSum += CVVDPJODToDistance(w)
		}
		jodMean /= float64(len(windows))
		distMean := CVVDPDistanceToJOD(distSum / float64(len(windows)))
		if distMean > jodMean+1e-9 {
			t.Fatalf("windows=%v: dist-space mean %v > JOD-space mean %v (violates convexity)",
				windows, distMean, jodMean)
		}
	}
}
