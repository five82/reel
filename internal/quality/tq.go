package quality

import (
	"math"
	"sort"
)

type StopReason string

const (
	StopNone          StopReason = ""
	StopConverged     StopReason = "converged"
	StopBoundsCrossed StopReason = "bounds_crossed"
	StopMonotonicity  StopReason = "monotonicity_guard"
	StopMaxProbes     StopReason = "max_probes"
	StopNoCandidates  StopReason = "no_candidates"
)

// ProbeWindow records one sampled metric window within a target-quality probe.
type ProbeWindow struct {
	Offset int     `json:"offset"`
	Frames int     `json:"frames"`
	Score  float32 `json:"score"`
}

// Probe records one target-quality probe encode and metric result.
type Probe struct {
	CRF              float32       `json:"crf"`
	Score            float32       `json:"score"`
	MeanScore        float32       `json:"mean_score,omitempty"`
	WorstWindowScore float32       `json:"worst_window_score,omitempty"`
	Size             uint64        `json:"size"`
	EncodeSeconds    float64       `json:"encode_seconds,omitempty"`
	MetricSeconds    float64       `json:"metric_seconds,omitempty"`
	SampleFrames     int           `json:"sample_frames,omitempty"`
	Windows          []ProbeWindow `json:"windows,omitempty"`
}

// SearchContext configures per-chunk target-quality search.
type SearchContext struct {
	Target     float32
	Tolerance  float32
	CRFMin     float32
	CRFMax     float32
	MaxProbes  int
	InitialCRF float32
	JODPerCRF  float32
}

// SearchState tracks target-quality search for one chunk.
type SearchState struct {
	Probes     []Probe    `json:"probes"`
	SearchMin  float32    `json:"search_min"`
	SearchMax  float32    `json:"search_max"`
	Round      int        `json:"round"`
	StopReason StopReason `json:"stop_reason,omitempty"`
	tried      map[int]bool
}

func NewSearchState(ctx SearchContext) *SearchState {
	return &SearchState{
		SearchMin: RoundCRFToQuarter(ctx.CRFMin),
		SearchMax: RoundCRFToQuarter(ctx.CRFMax),
		tried:     make(map[int]bool),
	}
}

func (s *SearchState) NextCRF(ctx SearchContext) (float32, bool) {
	if s.StopReason != StopNone {
		return 0, false
	}
	if ctx.MaxProbes > 0 && len(s.Probes) >= ctx.MaxProbes {
		s.StopReason = StopMaxProbes
		return 0, false
	}
	if s.SearchMin > s.SearchMax {
		s.StopReason = StopBoundsCrossed
		return 0, false
	}

	var candidate float32
	switch len(s.Probes) {
	case 0:
		candidate = initialSearchCRF(ctx)
	case 1:
		candidate = secondSearchCRF(ctx, s.Probes[0])
	default:
		candidate = nextCRFWithHistory(ctx, s)
	}

	candidate, ok := s.firstUntriedInBounds(candidate)
	if !ok {
		s.StopReason = StopNoCandidates
		return 0, false
	}
	s.Round++
	return candidate, true
}

func (s *SearchState) AddProbe(ctx SearchContext, probe Probe) {
	probe.CRF = RoundCRFToQuarter(probe.CRF)
	for _, old := range s.Probes {
		if old.CRF == probe.CRF {
			return
		}
		if (old.CRF-probe.CRF)*(old.Score-probe.Score) >= 0 {
			// Probes scored with different window counts are incomparable;
			// don't flag monotonicity on a measurement-mode change.
			if len(old.Windows) != len(probe.Windows) {
				continue
			}
			s.Probes = append(s.Probes, probe)
			s.tried[crfKey(probe.CRF)] = true
			s.StopReason = StopMonotonicity
			return
		}
	}

	s.Probes = append(s.Probes, probe)
	s.tried[crfKey(probe.CRF)] = true

	if targetQualityProbeConverged(ctx, probe) {
		s.StopReason = StopConverged
		return
	}
	if targetQualityWindowBelowFloor(ctx, probe) || probe.Score < ctx.Target-ctx.Tolerance {
		// Quality too low: lower CRF.
		s.SearchMax = RoundCRFToQuarter(probe.CRF - 0.25)
	} else if probe.Score > ctx.Target+ctx.Tolerance {
		// Quality higher than needed: raise CRF.
		s.SearchMin = RoundCRFToQuarter(probe.CRF + 0.25)
	}
	if s.SearchMin > s.SearchMax {
		s.StopReason = StopBoundsCrossed
		return
	}
	if ctx.MaxProbes > 0 && len(s.Probes) >= ctx.MaxProbes {
		s.StopReason = StopMaxProbes
	}
}

func (s *SearchState) BestProbe(ctx SearchContext) (Probe, bool) {
	if len(s.Probes) == 0 {
		return Probe{}, false
	}
	best, found := bestProbeMatching(ctx, s.Probes, func(probe Probe) bool {
		return !targetQualityWindowBelowFloor(ctx, probe)
	})
	if found {
		return best, true
	}
	best, _ = bestProbeMatching(ctx, s.Probes, func(Probe) bool { return true })
	return best, true
}

func bestProbeMatching(ctx SearchContext, probes []Probe, keep func(Probe) bool) (Probe, bool) {
	const errEpsilon = 0.001
	var best Probe
	bestErr := float64(0)
	found := false
	for _, probe := range probes {
		if !keep(probe) {
			continue
		}
		err := math.Abs(float64(probe.Score - ctx.Target))
		if !found || err < bestErr-errEpsilon || (math.Abs(err-bestErr) <= errEpsilon && probe.WorstWindowScore > best.WorstWindowScore) {
			best = probe
			bestErr = err
			found = true
		}
	}
	return best, found
}

func targetQualityProbeConverged(ctx SearchContext, probe Probe) bool {
	return targetQualityConverged(ctx, probe.Score) && !targetQualityWindowBelowFloor(ctx, probe)
}

func targetQualityConverged(ctx SearchContext, score float32) bool {
	return score >= ctx.Target-ctx.Tolerance && score <= ctx.Target+ctx.Tolerance
}

func targetQualityWindowBelowFloor(ctx SearchContext, probe Probe) bool {
	return probe.WorstWindowScore > 0 && probe.WorstWindowScore < ctx.Target-ctx.Tolerance
}

func initialSearchCRF(ctx SearchContext) float32 {
	if ctx.InitialCRF > 0 {
		return RoundCRFToQuarter(ctx.InitialCRF)
	}
	return RoundCRFToQuarter((ctx.CRFMin + ctx.CRFMax) / 2)
}

func nextCRFWithHistory(ctx SearchContext, state *SearchState) float32 {
	if probesBracketTarget(ctx, state.Probes) {
		candidate := InterpolateCRF(state.Probes, ctx.Target)
		if candidate >= state.SearchMin && candidate <= state.SearchMax {
			return candidate
		}
	}
	if probesAreAllAboveTarget(ctx, state.Probes) {
		return unbracketedHighCRF(ctx, state)
	}
	return RoundCRFToQuarter((state.SearchMin + state.SearchMax) / 2)
}

func probesBracketTarget(ctx SearchContext, probes []Probe) bool {
	below := false
	above := false
	for _, probe := range probes {
		side := probeTargetSide(ctx, probe)
		below = below || side < 0
		above = above || side > 0
	}
	return below && above
}

func probesAreAllAboveTarget(ctx SearchContext, probes []Probe) bool {
	if len(probes) == 0 {
		return false
	}
	for _, probe := range probes {
		if probeTargetSide(ctx, probe) <= 0 {
			return false
		}
	}
	return true
}

func probeTargetSide(ctx SearchContext, probe Probe) int {
	if targetQualityWindowBelowFloor(ctx, probe) || probe.Score < ctx.Target {
		return -1
	}
	if probe.Score > ctx.Target {
		return 1
	}
	return 0
}

func unbracketedHighCRF(ctx SearchContext, state *SearchState) float32 {
	candidate := (state.SearchMin + state.SearchMax) / 2
	if shouldUseAggressiveHighCRFJump(ctx, state.Probes) {
		candidate = state.SearchMin + (state.SearchMax-state.SearchMin)*0.75
	}
	return RoundCRFToQuarter(candidate)
}

func shouldUseAggressiveHighCRFJump(ctx SearchContext, probes []Probe) bool {
	const minHighSideDelta = 0.30
	score, ok := highestCRFScore(probes)
	if !ok || score-ctx.Target < minHighSideDelta {
		return false
	}
	slope, ok := highestCRFSlope(probes)
	return ok && slope < searchJODPerCRF(ctx)*0.5
}

func highestCRFScore(probes []Probe) (float32, bool) {
	if len(probes) == 0 {
		return 0, false
	}
	best := probes[0]
	for _, probe := range probes[1:] {
		if probe.CRF > best.CRF {
			best = probe
		}
	}
	return best.Score, true
}

func highestCRFSlope(probes []Probe) (float32, bool) {
	if len(probes) < 2 {
		return 0, false
	}
	probes = append([]Probe(nil), probes...)
	sort.Slice(probes, func(i, j int) bool { return probes[i].CRF < probes[j].CRF })
	for i := len(probes) - 1; i > 0; i-- {
		crfDelta := probes[i].CRF - probes[i-1].CRF
		scoreDrop := probes[i-1].Score - probes[i].Score
		if crfDelta > 0 && scoreDrop > 0 {
			return scoreDrop / crfDelta, true
		}
	}
	return 0, false
}

func searchJODPerCRF(ctx SearchContext) float32 {
	const defaultJODPerCRF = 0.04
	if ctx.JODPerCRF > 0 {
		return ctx.JODPerCRF
	}
	return defaultJODPerCRF
}

func secondSearchCRF(ctx SearchContext, probe Probe) float32 {
	jodPerCRF := searchJODPerCRF(ctx)
	delta := probe.Score - ctx.Target
	step := float32(math.Abs(float64(delta / jodPerCRF)))
	if maxStep := secondSearchMaxStep(delta); step > maxStep {
		step = maxStep
	}
	if delta > 0 {
		// Quality is higher than needed; raise CRF.
		return RoundCRFToQuarter(probe.CRF + step)
	}
	// Quality is too low; lower CRF.
	return RoundCRFToQuarter(probe.CRF - step)
}

func secondSearchMaxStep(delta float32) float32 {
	absDelta := float32(math.Abs(float64(delta)))
	switch {
	case absDelta >= 0.40:
		return 30
	case absDelta >= 0.25:
		return 20
	default:
		return 10
	}
}

func (s *SearchState) firstUntriedInBounds(candidate float32) (float32, bool) {
	candidate = RoundCRFToQuarter(candidate)
	if candidate < s.SearchMin {
		candidate = s.SearchMin
	}
	if candidate > s.SearchMax {
		candidate = s.SearchMax
	}
	if !s.tried[crfKey(candidate)] {
		return candidate, true
	}

	for step := float32(0.25); s.SearchMin <= candidate-step || candidate+step <= s.SearchMax; step += 0.25 {
		lo := RoundCRFToQuarter(candidate - step)
		if lo >= s.SearchMin && !s.tried[crfKey(lo)] {
			return lo, true
		}
		hi := RoundCRFToQuarter(candidate + step)
		if hi <= s.SearchMax && !s.tried[crfKey(hi)] {
			return hi, true
		}
	}
	return 0, false
}

func crfKey(crf float32) int {
	return int(math.Round(float64(crf * 4)))
}

// InterpolateCRF linearly interpolates a CRF for the target score using the
// pair of adjacent probes (ordered by score) whose scores bracket the target.
// If the target falls outside all probe scores, the nearest segment
// extrapolates, matching the prior linear behavior at two probes.
func InterpolateCRF(probes []Probe, target float32) float32 {
	if len(probes) == 0 {
		return 0
	}
	if len(probes) == 1 {
		return RoundCRFToQuarter(probes[0].CRF)
	}
	sorted := append([]Probe(nil), probes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Score < sorted[j].Score })
	k := len(sorted) - 2
	for i := 0; i < len(sorted)-1; i++ {
		if target <= sorted[i+1].Score {
			k = i
			break
		}
	}
	lo, hi := sorted[k], sorted[k+1]
	if hi.Score == lo.Score {
		return RoundCRFToQuarter((lo.CRF + hi.CRF) / 2)
	}
	t := (target - lo.Score) / (hi.Score - lo.Score)
	return RoundCRFToQuarter(lo.CRF + t*(hi.CRF-lo.CRF))
}
