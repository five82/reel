// Package encoder provides SvtAv1EncApp command building for chunked encoding.
package encoder

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/five82/reel/internal/ffms"
)

const svtEncBinary = "SvtAv1EncApp"

// EncConfig contains configuration for encoding a chunk.
type EncConfig struct {
	Inf        *ffms.VidInf // Video properties
	CRF        float32      // Quality (CRF value)
	Preset     uint8        // SVT-AV1 preset (0-13)
	Tune       uint8        // SVT-AV1 tune
	Output     string       // Output IVF path
	GrainTable *string      // Optional film grain table path
	Width      uint32       // Frame width (after cropping)
	Height     uint32       // Frame height (after cropping)
	Frames     int          // Number of frames to encode

	// Advanced SVT-AV1 parameters
	ACBias                float32
	EnableVarianceBoost   bool
	VarianceBoostStrength uint8
	VarianceOctile        uint8
	LogicalProcessors     int // Threads per worker (--lp flag), 0 = SVT-AV1 default
}

// MakeSvtCmd builds an SvtAv1EncApp command for encoding.
// The command reads raw YUV data from stdin and outputs to an IVF file.
// The command is wrapped with nice -n 19 to keep the system responsive.
func MakeSvtCmd(cfg *EncConfig) *exec.Cmd {
	args := buildSvtArgs(cfg)
	niceArgs := append([]string{"-n", "19", svtEncBinary}, args...)
	return exec.Command("nice", niceArgs...)
}

// buildSvtArgs constructs the argument list for SvtAv1EncApp.
func buildSvtArgs(cfg *EncConfig) []string {
	// Calculate keyint in frames (10 seconds worth)
	fps := float64(cfg.Inf.FPSNum) / float64(cfg.Inf.FPSDen)
	keyintFrames := int(fps * 10)

	args := []string{
		"-i", "stdin",
		"--input-depth", "10", // Always 10-bit input (8-bit sources are converted)
		"--color-format", "1", // YUV420
		"--profile", "0",      // Main profile
		"--passes", "1",
		"--tile-rows", "0",
		"--tile-columns", "0",
		"--width", fmt.Sprintf("%d", cfg.Width),
		"--height", fmt.Sprintf("%d", cfg.Height),
		"--fps-num", fmt.Sprintf("%d", cfg.Inf.FPSNum),
		"--fps-denom", fmt.Sprintf("%d", cfg.Inf.FPSDen),
		"--keyint", fmt.Sprintf("%d", keyintFrames), // Keyframe every 10 seconds
		"--rc", "0",       // CRF mode
		"--scd", "1",      // Enable scene change detection for keyframes within chunks
		"--scm", "0",      // Screen content mode disabled
		"--progress", "2", // Progress to stderr
		"--frames", fmt.Sprintf("%d", cfg.Frames),
		"--crf", fmt.Sprintf("%.0f", cfg.CRF),
		"--preset", fmt.Sprintf("%d", cfg.Preset),
	}

	// Add tune parameter
	args = append(args, "--tune", fmt.Sprintf("%d", cfg.Tune))

	// Add logical processors limit if specified (threads per worker)
	if cfg.LogicalProcessors > 0 {
		args = append(args, "--lp", fmt.Sprintf("%d", cfg.LogicalProcessors))
	}

	// Add color metadata if available
	if cfg.Inf.ColorPrimaries != nil {
		args = append(args, "--color-primaries", fmt.Sprintf("%d", *cfg.Inf.ColorPrimaries))
	}
	if cfg.Inf.TransferCharacteristics != nil {
		args = append(args, "--transfer-characteristics", fmt.Sprintf("%d", *cfg.Inf.TransferCharacteristics))
	}
	if cfg.Inf.MatrixCoefficients != nil {
		args = append(args, "--matrix-coefficients", fmt.Sprintf("%d", *cfg.Inf.MatrixCoefficients))
	}

	// Add mastering display if available
	if cfg.Inf.MasteringDisplay != nil {
		args = append(args, "--mastering-display", *cfg.Inf.MasteringDisplay)
	}
	if cfg.Inf.ContentLight != nil {
		args = append(args, "--content-light", *cfg.Inf.ContentLight)
	}

	// Add film grain table if provided
	if cfg.GrainTable != nil {
		args = append(args, "--fgs-table", *cfg.GrainTable)
	}

	// Add advanced parameters
	if cfg.ACBias != 0 {
		args = append(args, "--ac-bias", fmt.Sprintf("%.2f", cfg.ACBias))
	}

	if cfg.EnableVarianceBoost {
		args = append(args, "--enable-variance-boost", "1")
		args = append(args, "--variance-boost-strength", fmt.Sprintf("%d", cfg.VarianceBoostStrength))
		args = append(args, "--variance-octile", fmt.Sprintf("%d", cfg.VarianceOctile))
	}

	// Output file
	args = append(args, "-b", cfg.Output)

	return args
}

// SvtArgsString returns a human-readable string of the SVT-AV1 arguments.
func SvtArgsString(cfg *EncConfig) string {
	args := buildSvtArgs(cfg)
	return strings.Join(args, " ")
}

// SvtParamsDisplay returns a human-readable colon-separated string of key SVT-AV1 parameters
// for display purposes (similar to FFmpeg's -svtav1-params format).
func SvtParamsDisplay(acBias float32, enableVarianceBoost bool, tune uint8) string {
	params := []string{
		fmt.Sprintf("ac-bias=%g", acBias),
	}

	if enableVarianceBoost {
		params = append(params, "enable-variance-boost=1")
	} else {
		params = append(params, "enable-variance-boost=0")
	}

	params = append(params,
		fmt.Sprintf("tune=%d", tune),
		"keyint=10s",
		"scd=1",
		"scm=0",
	)

	return strings.Join(params, ":")
}

// IsSvtAvailable checks if SvtAv1EncApp is available in PATH.
func IsSvtAvailable() bool {
	_, err := exec.LookPath(svtEncBinary)
	return err == nil
}

// GetSvtPath returns the path to SvtAv1EncApp if available.
func GetSvtPath() (string, error) {
	return exec.LookPath(svtEncBinary)
}
