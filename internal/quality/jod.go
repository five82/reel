package quality

import "math"

// CVVDP maps an underlying perceptual distance to a JOD score (0-10) through a
// nonlinear psychometric function. These constants are the authoritative values
// from the linked libvship reference (src/HIP/cvvdp/parameters.hpp,
// src/HIP/cvvdp/distmap_specifics.hpp) and must stay in lockstep with it.
const (
	cvvdpJODA   = 0.0439569391310215
	cvvdpJODExp = 0.9302042722702026
)

// cvvdpJODKink is the distance at which Vship's toJOD switches from the power
// branch to its linear extension; toJOD(kink) is the corresponding score.
const cvvdpJODKink = 0.1

// CVVDPDistanceToJOD maps a CVVDP perceptual distance to a JOD score.
// It is a faithful port of Vship's piecewise toJOD: a power law above distance
// 0.1, and a linear extension below it so the score stays continuous and never
// exceeds 10 for tiny (near-transparent) distances.
func CVVDPDistanceToJOD(distance float64) float64 {
	if distance > cvvdpJODKink {
		return 10 - cvvdpJODA*math.Pow(distance, cvvdpJODExp)
	}
	// Linear extension through (0,10) and the kink; C0 but not C1 continuous.
	// This mirrors Vship's jod_a_p formula exactly (a genuine slope kink).
	jodAP := cvvdpJODA * math.Pow(cvvdpJODKink, cvvdpJODExp-1)
	return 10 - jodAP*distance
}

// CVVDPJODToDistance is the inverse of CVVDPDistanceToJOD: it recovers the
// perceptual distance from a JOD score.
func CVVDPJODToDistance(jod float64) float64 {
	// The kink corresponds to a near-10 score; above it (tiny distance) the
	// linear branch applies, below it (normal operating range) the power branch.
	jodAtKink := CVVDPDistanceToJOD(cvvdpJODKink)
	if jod >= jodAtKink {
		jodAP := cvvdpJODA * math.Pow(cvvdpJODKink, cvvdpJODExp-1)
		return (10 - jod) / jodAP
	}
	return math.Pow((10-jod)/cvvdpJODA, 1/cvvdpJODExp)
}
