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
		candidate = InterpolateCRF(s.Probes, ctx.Target, s.Round)
		if candidate < s.SearchMin || candidate > s.SearchMax || math.IsNaN(float64(candidate)) || math.IsInf(float64(candidate), 0) {
			candidate = RoundCRFToQuarter((s.SearchMin + s.SearchMax) / 2)
		}
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
			s.Probes = append(s.Probes, probe)
			s.tried[crfKey(probe.CRF)] = true
			s.StopReason = StopMonotonicity
			return
		}
	}

	s.Probes = append(s.Probes, probe)
	s.tried[crfKey(probe.CRF)] = true

	delta := probe.Score - ctx.Target
	if float32(math.Abs(float64(delta))) <= ctx.Tolerance {
		s.StopReason = StopConverged
		return
	}
	if probe.Score < ctx.Target-ctx.Tolerance {
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
	best := s.Probes[0]
	bestErr := math.Abs(float64(best.Score - ctx.Target))
	for _, probe := range s.Probes[1:] {
		err := math.Abs(float64(probe.Score - ctx.Target))
		if err < bestErr {
			best = probe
			bestErr = err
		}
	}
	return best, true
}

func initialSearchCRF(ctx SearchContext) float32 {
	if ctx.InitialCRF > 0 {
		return RoundCRFToQuarter(ctx.InitialCRF)
	}
	return RoundCRFToQuarter((ctx.CRFMin + ctx.CRFMax) / 2)
}

func secondSearchCRF(ctx SearchContext, probe Probe) float32 {
	const (
		// CVVDP changes by about 0.04 JOD per CRF on typical probes.
		defaultJODPerCRF = 0.04
		maxStep          = 10.0
	)
	jodPerCRF := ctx.JODPerCRF
	if jodPerCRF <= 0 {
		jodPerCRF = defaultJODPerCRF
	}
	delta := probe.Score - ctx.Target
	step := float32(math.Abs(float64(delta / jodPerCRF)))
	if step > maxStep {
		step = maxStep
	}
	if delta > 0 {
		// Quality is higher than needed; raise CRF.
		return RoundCRFToQuarter(probe.CRF + step)
	}
	// Quality is too low; lower CRF.
	return RoundCRFToQuarter(probe.CRF - step)
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

func InterpolateCRF(probes []Probe, target float32, round int) float32 {
	pairs := make([][2]float32, 0, len(probes))
	for _, p := range probes {
		pairs = append(pairs, [2]float32{p.Score, p.CRF})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i][0] < pairs[j][0] })
	x := make([]float32, len(pairs))
	y := make([]float32, len(pairs))
	for i, p := range pairs {
		x[i] = p[0]
		y[i] = p[1]
	}

	var result float32
	switch {
	case len(pairs) == 2 || round == 3:
		result = lerp(x, y, target)
	case len(pairs) == 3 || round == 4:
		result = fritschCarlson(x, y, target)
	default:
		result = pchip(x, y, target)
	}
	return RoundCRFToQuarter(result)
}

func lerp(x, y []float32, xi float32) float32 {
	if len(x) < 2 || x[1] == x[0] {
		return y[0]
	}
	t := (xi - x[0]) / (x[1] - x[0])
	return t*(y[1]-y[0]) + y[0]
}

func pchip(x, y []float32, xi float32) float32 {
	n := len(x)
	if n < 3 {
		return lerp(x, y, xi)
	}
	k := 0
	for i := 0; i < n-1; i++ {
		if xi >= x[i] && xi <= x[i+1] {
			k = i
			break
		}
	}

	s := make([]float32, n-1)
	for i := 0; i < n-1; i++ {
		if x[i+1] == x[i] {
			s[i] = 0
		} else {
			s[i] = (y[i+1] - y[i]) / (x[i+1] - x[i])
		}
	}
	d := make([]float32, n)
	d[0] = s[0]
	d[n-1] = s[n-2]
	for i := 1; i < n-1; i++ {
		prev, next := s[i-1], s[i]
		if prev*next <= 0 {
			d[i] = 0
		} else {
			hPrev := x[i] - x[i-1]
			hNext := x[i+1] - x[i]
			w1 := 2*hNext + hPrev
			w2 := 2*hPrev + hNext
			d[i] = (w1 + w2) / (w1/prev + w2/next)
		}
	}
	const maxTau2 = 9.0
	for i := 0; i < n-1; i++ {
		if s[i] == 0 {
			d[i], d[i+1] = 0, 0
			continue
		}
		alpha := d[i] / s[i]
		beta := d[i+1] / s[i]
		tau := alpha*alpha + beta*beta
		if tau > maxTau2 {
			scale := float32(3.0 / math.Sqrt(float64(tau)))
			d[i] = scale * alpha * s[i]
			d[i+1] = scale * beta * s[i]
		}
	}
	return cubicHermite(x, y, d, k, xi)
}

func fritschCarlson(x, y []float32, xi float32) float32 {
	if len(x) < 3 {
		return lerp(x, y, xi)
	}
	k := 0
	if xi >= x[1] && xi <= x[2] {
		k = 1
	}
	d0 := (y[1] - y[0]) / (x[1] - x[0])
	d1 := (y[2] - y[1]) / (x[2] - x[1])
	m := [3]float32{d0, 0, d1}
	if d0*d1 > 0 {
		h0 := x[1] - x[0]
		h1 := x[2] - x[1]
		w1 := 2*h1 + h0
		w2 := 2*h0 + h1
		m[1] = (w1 + w2) / (w1/d0 + w2/d1)
	}
	return cubicHermite(x, y, m[:], k, xi)
}

func cubicHermite(x, y, d []float32, k int, xi float32) float32 {
	h := x[k+1] - x[k]
	if h == 0 {
		return y[k]
	}
	t := (xi - x[k]) / h
	t2 := t * t
	t3 := t2 * t
	h00 := 2*t3 - 3*t2 + 1
	h10 := t3 - 2*t2 + t
	h01 := -2*t3 + 3*t2
	h11 := t3 - t2
	return h00*y[k] + h10*h*d[k] + h01*y[k+1] + h11*h*d[k+1]
}
