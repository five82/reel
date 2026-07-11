package processing

import (
	"codeberg.org/five82/reel/internal/config"
	"codeberg.org/five82/reel/internal/quality"
	"codeberg.org/five82/reel/internal/video"
)

// probeMetricFor picks the target-quality probe metric for a source. Explicit
// user quality settings are CVVDP-denominated (--target-quality is a JOD
// range, --cvvdp-display overrides the display model), so either forces
// CVVDP; otherwise SDR sources at or below 1080p probe with SSIMULACRA2 (see
// quality.ProbeMetricForSource for the rationale and calibration provenance).
func probeMetricFor(cfg *config.Config, inf *video.Info) quality.MetricKind {
	if cfg.TargetQuality != config.DefaultTargetQuality || cfg.CVVDPDisplay != "" {
		return quality.MetricCVVDP
	}
	return quality.ProbeMetricForSource(inf)
}
