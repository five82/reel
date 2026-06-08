package quality

import "testing"

func TestSearchConverges(t *testing.T) {
	ctx := SearchContext{Target: 9.5, Tolerance: 0.05, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6}
	state := NewSearchState(ctx)
	crf, ok := state.NextCRF(ctx)
	if !ok {
		t.Fatal("no first CRF")
	}
	state.AddProbe(ctx, Probe{CRF: crf, Score: 9.51})
	if state.StopReason != StopConverged {
		t.Fatalf("stop reason = %q, want converged", state.StopReason)
	}
	best, ok := state.BestProbe(ctx)
	if !ok || best.CRF != crf {
		t.Fatalf("best probe = %+v ok=%v", best, ok)
	}
}

func TestSearchAcceptsUpperGrace(t *testing.T) {
	ctx := SearchContext{Target: 9.5, Tolerance: 0.1, UpperToleranceGrace: 0.02, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6}
	state := NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 20, Score: 9.6062})
	if state.StopReason != StopConverged {
		t.Fatalf("stop reason = %q, want converged", state.StopReason)
	}
}

func TestSearchDoesNotApplyLowerGrace(t *testing.T) {
	ctx := SearchContext{Target: 9.5, Tolerance: 0.1, UpperToleranceGrace: 0.02, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6}
	state := NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 20, Score: 9.3938})
	if state.StopReason == StopConverged {
		t.Fatal("low score should not converge with upper-only grace")
	}
	if state.SearchMax != 19.75 {
		t.Fatalf("SearchMax = %g, want 19.75", state.SearchMax)
	}
}

func TestSearchRequiresSampledWindowFloor(t *testing.T) {
	ctx := SearchContext{Target: 9.5, Tolerance: 0.1, UpperToleranceGrace: 0.02, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6}
	state := NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 24, Score: 9.51, WorstWindowScore: 9.28})
	if state.StopReason == StopConverged {
		t.Fatal("probe with weak sampled window should not converge")
	}
	if state.SearchMax != 23.75 {
		t.Fatalf("SearchMax = %g, want 23.75", state.SearchMax)
	}
}

func TestBestProbePrefersSampledWindowFloor(t *testing.T) {
	ctx := SearchContext{Target: 9.5, Tolerance: 0.1, UpperToleranceGrace: 0.02, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6}
	state := NewSearchState(ctx)
	state.Probes = []Probe{
		{CRF: 31.5, Score: 9.5123, WorstWindowScore: 9.2786},
		{CRF: 27, Score: 9.6577, WorstWindowScore: 9.4947},
	}
	best, ok := state.BestProbe(ctx)
	if !ok || best.CRF != 27 {
		t.Fatalf("best probe = %+v ok=%v, want CRF 27", best, ok)
	}
}

func TestBestProbeFallsBackWhenAllSampledWindowsAreWeak(t *testing.T) {
	ctx := SearchContext{Target: 9.5, Tolerance: 0.1, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6}
	state := NewSearchState(ctx)
	state.Probes = []Probe{
		{CRF: 20, Score: 9.7, WorstWindowScore: 9.3},
		{CRF: 24, Score: 9.52, WorstWindowScore: 9.2},
	}
	best, ok := state.BestProbe(ctx)
	if !ok || best.CRF != 24 {
		t.Fatalf("best probe = %+v ok=%v, want fallback closest score", best, ok)
	}
}

func TestSearchStartsAtInitialCRF(t *testing.T) {
	ctx := SearchContext{Target: 9.5, Tolerance: 0.05, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6, InitialCRF: 26}
	state := NewSearchState(ctx)
	crf, ok := state.NextCRF(ctx)
	if !ok {
		t.Fatal("no first CRF")
	}
	if crf != 26 {
		t.Fatalf("first CRF = %g, want 26", crf)
	}
}

func TestSearchSecondProbeUsesEstimatedStep(t *testing.T) {
	ctx := SearchContext{Target: 9.5, Tolerance: 0.05, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6, InitialCRF: 26}
	state := NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 26, Score: 9.3})
	crf, ok := state.NextCRF(ctx)
	if !ok {
		t.Fatal("no second CRF")
	}
	if crf != 21 {
		t.Fatalf("second CRF = %g, want 21", crf)
	}
}

func TestSearchSecondProbeUsesLargerStepForLargeMiss(t *testing.T) {
	ctx := SearchContext{Target: 9.5, Tolerance: 0.05, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6, InitialCRF: 26, JODPerCRF: 0.04}
	state := NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 26, Score: 9.1})
	crf, ok := state.NextCRF(ctx)
	if !ok {
		t.Fatal("no second CRF")
	}
	if crf != 16 {
		t.Fatalf("second CRF = %g, want 16", crf)
	}
}

func TestSearchSecondProbeCapsExtremeMiss(t *testing.T) {
	ctx := SearchContext{Target: 9.5, Tolerance: 0.05, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6, InitialCRF: 26, JODPerCRF: 0.02}
	state := NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 26, Score: 10.1})
	crf, ok := state.NextCRF(ctx)
	if !ok {
		t.Fatal("no second CRF")
	}
	if crf != 56 {
		t.Fatalf("second CRF = %g, want 56", crf)
	}
}

func TestSearchUsesMidpointForUnbracketedLowProbes(t *testing.T) {
	ctx := SearchContext{Target: 9.375, Tolerance: 0.125, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6, InitialCRF: 39.75, JODPerCRF: 0.04}
	state := NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 39.75, Score: 9.1704})
	state.AddProbe(ctx, Probe{CRF: 34.5, Score: 9.2008})
	crf, ok := state.NextCRF(ctx)
	if !ok {
		t.Fatal("no third CRF")
	}
	if crf != 19.25 {
		t.Fatalf("third CRF = %g, want bounded midpoint 19.25", crf)
	}
}

func TestSearchAcceleratesFlatUnbracketedHighProbes(t *testing.T) {
	ctx := SearchContext{Target: 9.375, Tolerance: 0.125, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6, InitialCRF: 27, JODPerCRF: 0.025}
	state := NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 27, Score: 9.9255})
	state.AddProbe(ctx, Probe{CRF: 41.5, Score: 9.8751})
	crf, ok := state.NextCRF(ctx)
	if !ok {
		t.Fatal("no third CRF")
	}
	if crf != 58.25 {
		t.Fatalf("third CRF = %g, want aggressive high-side probe 58.25", crf)
	}
}

func TestSearchUsesMidpointForMildFlatUnbracketedHighProbes(t *testing.T) {
	ctx := SearchContext{Target: 9.375, Tolerance: 0.125, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6, InitialCRF: 24.75, JODPerCRF: 0.04}
	state := NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 24.75, Score: 9.677})
	state.AddProbe(ctx, Probe{CRF: 28.75, Score: 9.563})
	crf, ok := state.NextCRF(ctx)
	if !ok {
		t.Fatal("no third CRF")
	}
	if crf != 46.5 {
		t.Fatalf("third CRF = %g, want bounded midpoint 46.5", crf)
	}
}

func TestSearchInterpolatesBracketedProbes(t *testing.T) {
	ctx := SearchContext{Target: 9.5, Tolerance: 0.01, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6}
	state := NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 20, Score: 9.8})
	state.AddProbe(ctx, Probe{CRF: 30, Score: 9.2})
	crf, ok := state.NextCRF(ctx)
	if !ok {
		t.Fatal("no third CRF")
	}
	if crf != 25 {
		t.Fatalf("third CRF = %g, want interpolated 25", crf)
	}
}

func TestSearchBoundsUpdateForCVVDP(t *testing.T) {
	ctx := SearchContext{Target: 9.5, Tolerance: 0.05, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6}
	state := NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 34, Score: 9.0})
	if state.SearchMax != 33.75 {
		t.Fatalf("low score SearchMax = %g, want 33.75", state.SearchMax)
	}
	state = NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 34, Score: 9.9})
	if state.SearchMin != 34.25 {
		t.Fatalf("high score SearchMin = %g, want 34.25", state.SearchMin)
	}
}

func TestSearchMonotonicityGuard(t *testing.T) {
	ctx := SearchContext{Target: 9.5, Tolerance: 0.01, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6}
	state := NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 20, Score: 9.8})
	state.AddProbe(ctx, Probe{CRF: 30, Score: 9.9})
	if state.StopReason != StopMonotonicity {
		t.Fatalf("stop reason = %q, want monotonicity", state.StopReason)
	}
}

func TestSearchMonotonicitySkippedForDifferentWindowCounts(t *testing.T) {
	ctx := SearchContext{Target: 9.5, Tolerance: 0.01, CRFMin: 4.25, CRFMax: 63.75, MaxProbes: 6}
	state := NewSearchState(ctx)
	state.AddProbe(ctx, Probe{CRF: 20, Score: 9.8, Windows: []ProbeWindow{{Offset: 0, Frames: 48, Score: 9.8}}})
	state.AddProbe(ctx, Probe{CRF: 30, Score: 9.9, Windows: []ProbeWindow{
		{Offset: 0, Frames: 48, Score: 9.9},
		{Offset: 100, Frames: 48, Score: 9.9},
	}})
	if state.StopReason != StopNone {
		t.Fatalf("stop reason = %q, want none when window counts differ", state.StopReason)
	}
}

func TestInterpolateCRF(t *testing.T) {
	probes := []Probe{{CRF: 20, Score: 9.8}, {CRF: 30, Score: 9.2}}
	got := InterpolateCRF(probes, 9.5, 2)
	if got != 25 {
		t.Fatalf("InterpolateCRF = %g, want 25", got)
	}
}
