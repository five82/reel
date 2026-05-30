// Package config provides configuration types and defaults for reel.
package config

import (
	"fmt"
	"os"

	"codeberg.org/five82/reel/internal/quality"
)

// Default constants
const (
	// DefaultCRFSD is the default fixed-CRF quality setting for SD content (<1920 width).
	DefaultCRFSD float32 = 24

	// DefaultCRFHD is the default fixed-CRF quality setting for HD content (>=1920, <3840 width).
	DefaultCRFHD float32 = 26

	// DefaultCRFUHD is the default fixed-CRF quality setting for UHD content (>=3840 width).
	DefaultCRFUHD float32 = 26

	// Quality mode constants.
	QualityModeTarget = "target"
	QualityModeCRF    = "crf"

	// DefaultTargetQuality is the default CVVDP JOD target range.
	DefaultTargetQuality = "9.25-9.50"

	// DefaultCRFSearchRange is the default target-quality CRF search range.
	DefaultCRFSearchRange = "4.25-63.75"

	// DefaultMetricWorkers limits concurrent VSHIP/CUDA scoring by default.
	DefaultMetricWorkers = 4

	// DefaultTargetQualityMaxProbes caps per-chunk target-quality probes.
	DefaultTargetQualityMaxProbes = 6

	// HDWidthThreshold is the minimum width for HD resolution.
	HDWidthThreshold uint32 = 1920

	// UHDWidthThreshold is the minimum width for UHD resolution.
	UHDWidthThreshold uint32 = 3840

	// DefaultSVTAV1Preset is the SVT-AV1 preset (0-13, lower is slower/better).
	DefaultSVTAV1Preset uint8 = 6

	// DefaultSVTAV1Tune is the SVT-AV1 tune parameter.
	DefaultSVTAV1Tune uint8 = 0

	// DefaultSVTAV1ACBias is the SVT-AV1 ac-bias parameter.
	DefaultSVTAV1ACBias float32 = 0.1

	// DefaultSVTAV1EnableVarianceBoost is whether variance boost is enabled.
	DefaultSVTAV1EnableVarianceBoost bool = false

	// DefaultSVTAV1VarianceBoostStrength is the variance boost strength.
	DefaultSVTAV1VarianceBoostStrength uint8 = 0

	// DefaultSVTAV1VarianceOctile is the variance octile parameter.
	DefaultSVTAV1VarianceOctile uint8 = 0

	// DefaultCropMode is the crop mode for the main encode.
	DefaultCropMode string = "auto"

	// DefaultEncodeCooldownSecs is the cooldown period between encodes.
	DefaultEncodeCooldownSecs uint64 = 3

	// ProgressLogIntervalPercent is the progress logging interval.
	ProgressLogIntervalPercent uint8 = 5

	// Chunk duration defaults by resolution.
	// Longer chunks provide better encoder efficiency and reduce concatenation overhead.
	DefaultChunkDurationSD  float64 = 20.0 // SD/720p: faster encode, can use shorter chunks
	DefaultChunkDurationHD  float64 = 30.0 // 1080p: balanced
	DefaultChunkDurationUHD float64 = 45.0 // 4K: slower encode, needs longer warmup

)

// Config holds all configuration for video processing.
type Config struct {
	// Input/output paths
	InputDir  string
	OutputDir string
	LogDir    string
	LogFile   string
	TempDir   string // Optional, defaults to OutputDir

	// SVT-AV1 parameters
	SVTAV1Preset                uint8
	SVTAV1Tune                  uint8
	SVTAV1ACBias                float32
	SVTAV1EnableVarianceBoost   bool
	SVTAV1VarianceBoostStrength uint8
	SVTAV1VarianceOctile        uint8

	// Quality mode and settings.
	QualityMode            string  // "target" or "crf"
	TargetQuality          string  // CVVDP JOD target range, LOW-HIGH
	TargetQualityMin       float32 // Parsed CVVDP target low bound
	TargetQualityMax       float32 // Parsed CVVDP target high bound
	TargetQualityTarget    float32 // Parsed CVVDP target midpoint
	TargetQualityTolerance float32 // Parsed CVVDP tolerance
	CRFSearchRange         string  // Target-quality search bounds, LOW-HIGH
	CRFSearchMin           float32 // Parsed target-quality CRF low bound
	CRFSearchMax           float32 // Parsed target-quality CRF high bound
	CVVDPDisplay           string  // Optional display JSON override
	MetricWorkers          int     // Concurrent VSHIP/CUDA scoring workers
	TargetQualityMaxProbes int     // Per-chunk target-quality probe cap

	// Fixed-CRF settings by resolution.
	CRFSD  float32 // CRF for SD content (<1920 width)
	CRFHD  float32 // CRF for HD content (>=1920, <3840 width)
	CRFUHD float32 // CRF for UHD content (>=3840 width)

	// Processing options
	CropMode           string // "auto" or "none"
	EncodeCooldownSecs uint64 // Cooldown between batch encodes

	// Chunk duration settings by resolution (seconds)
	ChunkDurationSD  float64 // Chunk duration for SD content (<1920 width)
	ChunkDurationHD  float64 // Chunk duration for HD content (>=1920, <3840 width)
	ChunkDurationUHD float64 // Chunk duration for UHD content (>=3840 width)

	// Debug options
	Verbose     bool // Enable verbose output
	KeepWorkDir bool // Keep .reel work directory after successful encodes
}

// NewConfig creates a new Config with default values.
func NewConfig(inputDir, outputDir, logDir string) *Config {
	return &Config{
		InputDir:                    inputDir,
		OutputDir:                   outputDir,
		LogDir:                      logDir,
		SVTAV1Preset:                DefaultSVTAV1Preset,
		SVTAV1Tune:                  DefaultSVTAV1Tune,
		SVTAV1ACBias:                DefaultSVTAV1ACBias,
		SVTAV1EnableVarianceBoost:   DefaultSVTAV1EnableVarianceBoost,
		SVTAV1VarianceBoostStrength: DefaultSVTAV1VarianceBoostStrength,
		SVTAV1VarianceOctile:        DefaultSVTAV1VarianceOctile,
		QualityMode:                 defaultQualityMode(),
		TargetQuality:               DefaultTargetQuality,
		CRFSearchRange:              DefaultCRFSearchRange,
		MetricWorkers:               DefaultMetricWorkers,
		TargetQualityMaxProbes:      DefaultTargetQualityMaxProbes,
		CRFSD:                       DefaultCRFSD,
		CRFHD:                       DefaultCRFHD,
		CRFUHD:                      DefaultCRFUHD,
		CropMode:                    DefaultCropMode,
		EncodeCooldownSecs:          DefaultEncodeCooldownSecs,
		ChunkDurationSD:             DefaultChunkDurationSD,
		ChunkDurationHD:             DefaultChunkDurationHD,
		ChunkDurationUHD:            DefaultChunkDurationUHD,
	}
}

func defaultQualityMode() string {
	if quality.VshipBuildEnabled() {
		return QualityModeTarget
	}
	return QualityModeCRF
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.SVTAV1Preset > 13 {
		return fmt.Errorf("svt_av1_preset must be 0-13, got %d", c.SVTAV1Preset)
	}

	switch c.QualityMode {
	case QualityModeTarget, QualityModeCRF:
	case "":
		c.QualityMode = defaultQualityMode()
	default:
		return fmt.Errorf("quality-mode must be %q or %q, got %q", QualityModeTarget, QualityModeCRF, c.QualityMode)
	}
	if c.QualityMode == QualityModeTarget && !quality.VshipBuildEnabled() {
		return fmt.Errorf("quality-mode %q is not available in no_vship builds; rebuild with VSHIP or use %q", QualityModeTarget, QualityModeCRF)
	}

	if err := quality.ValidateCRF(c.CRFSD); err != nil {
		return fmt.Errorf("crf-sd: %w", err)
	}
	if err := quality.ValidateCRF(c.CRFHD); err != nil {
		return fmt.Errorf("crf-hd: %w", err)
	}
	if err := quality.ValidateCRF(c.CRFUHD); err != nil {
		return fmt.Errorf("crf-uhd: %w", err)
	}

	if c.TargetQuality == "" {
		c.TargetQuality = DefaultTargetQuality
	}
	low, high, target, tolerance, err := quality.ParseTargetQualityRange(c.TargetQuality)
	if err != nil {
		return err
	}
	c.TargetQualityMin = low
	c.TargetQualityMax = high
	c.TargetQualityTarget = target
	c.TargetQualityTolerance = tolerance

	if c.CRFSearchRange == "" {
		c.CRFSearchRange = DefaultCRFSearchRange
	}
	searchMin, searchMax, err := quality.ParseCRFSearchRange(c.CRFSearchRange)
	if err != nil {
		return err
	}
	c.CRFSearchMin = searchMin
	c.CRFSearchMax = searchMax

	if c.MetricWorkers < 1 {
		return fmt.Errorf("metric-workers must be >= 1, got %d", c.MetricWorkers)
	}
	if c.TargetQualityMaxProbes < 1 {
		return fmt.Errorf("target-quality-max-probes must be >= 1, got %d", c.TargetQualityMaxProbes)
	}
	if c.CVVDPDisplay != "" {
		if _, err := os.Stat(c.CVVDPDisplay); err != nil {
			return fmt.Errorf("cvvdp-display is not readable: %w", err)
		}
	}

	// Validate chunk durations
	for _, cd := range []struct {
		name  string
		value float64
	}{
		{"chunk_duration_sd", c.ChunkDurationSD},
		{"chunk_duration_hd", c.ChunkDurationHD},
		{"chunk_duration_uhd", c.ChunkDurationUHD},
	} {
		if cd.value < 1 || cd.value > 120 {
			return fmt.Errorf("%s must be between 1 and 120 seconds, got %g", cd.name, cd.value)
		}
	}

	return nil
}

// GetTempDir returns the temp directory, falling back to OutputDir if not set.
func (c *Config) GetTempDir() string {
	if c.TempDir != "" {
		return c.TempDir
	}
	return c.OutputDir
}

// CRFForWidth returns the appropriate CRF value based on video width.
func (c *Config) CRFForWidth(width uint32) float32 {
	if width >= UHDWidthThreshold {
		return c.CRFUHD
	}
	if width >= HDWidthThreshold {
		return c.CRFHD
	}
	return c.CRFSD
}

// ChunkDurationForWidth returns the appropriate chunk duration based on video width.
func (c *Config) ChunkDurationForWidth(width uint32) float64 {
	if width >= UHDWidthThreshold {
		return c.ChunkDurationUHD
	}
	if width >= HDWidthThreshold {
		return c.ChunkDurationHD
	}
	return c.ChunkDurationSD
}
