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

func TestInterpolateCRF(t *testing.T) {
	probes := []Probe{{CRF: 20, Score: 9.8}, {CRF: 30, Score: 9.2}}
	got := InterpolateCRF(probes, 9.5, 2)
	if got != 25 {
		t.Fatalf("InterpolateCRF = %g, want 25", got)
	}
}
