package encode

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/five82/reel/internal/chunk"
	"github.com/five82/reel/internal/config"
	"github.com/five82/reel/internal/encoder"
	"github.com/five82/reel/internal/perf"
	"github.com/five82/reel/internal/quality"
	"github.com/five82/reel/internal/video"
)

// Grain treatment: grainy titles are encoded from a denoised source with a
// film grain table attached, so the decoder re-synthesizes texture instead of
// the encoder spending bits coding noise. Clean titles are left alone, because
// denoising them only costs quality (measured: 3% bytes for 0.13 JOD).
//
// Which titles are grainy is decided by bits-at-CRF, not by any grain
// estimator: a fixed-CRF probe encode of a few sample chunks separates heavy
// grain from clean content by roughly an order of magnitude (a grainy 4K disc
// title costs ~30 Mbps at CRF 26 where clean titles cost 1-5 Mbps). See
// docs/PERFORMANCE_TESTING.md, "Denoise and film grain".
const (
	// grainGateCRF is the fixed quality the sample chunks are probed at. It is
	// a measuring stick, not a quality decision: the same CRF for every title
	// is what makes the measured bitrates comparable across titles.
	grainGateCRF float32 = 22

	// grainGateSamples is how many chunks are probed. Within-title spread is
	// large (the 2026-08-31 calibration measured 2x-24x between a title's own
	// samples), and at five samples the median misclassified two of twelve
	// ground-truth titles purely from sampling noise; twelve samples cut the
	// false-positive rate from ~7% to ~1% while still costing only a few
	// minutes (~2-3% of a 4K feature encode). Variance is shot-to-shot, so
	// more short samples beat fewer long ones.
	grainGateSamples = 12

	// grainGateMinChunkFrames skips chunks too short for a stable measurement
	// (keyframe overhead dominates a very short chunk's bitrate).
	grainGateMinChunkFrames = 100

	// grainGateMiddleFraction restricts sampling to the middle 60% of the
	// title, avoiding titles/credits/logos, which are not representative.
	grainGateMiddleFraction = 0.6
)

// Bits per pixel per frame thresholds, measured from the coded (post-crop)
// geometry so letterbox bars cannot deflate them. Calibrated 2026-08-31
// against 15 UHD rips with known target-quality outcomes (RESULTS.md section
// 8 in the denoise study): the accepted and complained-about classes separate
// 2x at a stable estimator (0.055 vs 0.111 median bpp), so anything in
// 0.060-0.090 classifies identically. For intuition at 3840x2160@23.976:
//
//	0.0703 bpp = 14 Mbps, 0.105 bpp = 21 Mbps.
//
// At or above the med cutoff a title gets the medium grain table; between the
// two cutoffs the light table; below, no treatment at all. Known accepted
// miss: American Hustle (0.048 bpp measured, 18 Mbps delivered) sits below
// the line under any resampling - a dark grainy title is cheap at the gate
// CRF but expensive at the JOD target, so this signal will keep missing that
// class; a fatter-than-expected library title is the adjustment trigger.
const (
	uhdLightBPP float64 = 0.0703
	// uhdMedBPP was 0.1205 (24 Mbps) pre-calibration; only Fargo ever
	// reached it. 0.105 lets Vacation-class fine pervasive grain reach the
	// medium table. A title near the line flips light/med run to run, which
	// swaps the synthesis table, never the treat/no-treat verdict.
	uhdMedBPP float64 = 0.105

	// HD cutoffs are PROVISIONAL, low confidence: five 1080p sources, one
	// with target-quality ground truth. 0.22 bpp (10.9 Mbps at
	// 1920x1080@23.976) is the geometric center of the only gap in the
	// measured data (soms 0.174 -> Mary Poppins 0.271) and matches grain
	// reputations; the med cutoff carries the UHD med/light ratio. Revisit
	// once a few 1080p titles have real accept/complain verdicts.
	hdLightBPP float64 = 0.22
	hdMedBPP   float64 = 0.33
)

// grainDenoiseFilter is the denoiser every treated title runs through.
// fftdnoiz at defaults was the only denoiser that survived the honest control
// (re-running the undenoised baseline at the denoised run's source-referenced
// JOD): -34% and -13% bytes at matched honest quality on two coarse-grain 4K
// titles, where hqdn3d lost to simply lowering the quality target. It costs
// about 0.3 CPU-seconds per 4K frame, which the reference cache amortizes.
const grainDenoiseFilter = "fftdnoiz"

// Grain tiers. The tables are prebuilt libaom "filmgrn1" tables: SVT's own
// grain estimation halves encode speed, while a prebuilt table is free
// (29.67 vs 29.61 fps) and is applied by the playback decoder, so it changes
// no encoded pixel. Viewing picked medium over both the strong table (too
// strong) and bare denoise with no table.
const (
	grainTierNone  = ""
	grainTierLight = "light"
	grainTierMed   = "med"

	// grainModeOverride is the recorded mode when the experimental
	// --denoise/--fgs-table flags decide the treatment instead of the gate.
	grainModeOverride = "override"
)

//go:embed graintables/grain-light.tbl graintables/grain-med.tbl
var grainTables embed.FS

// GrainTreatment is the resolved per-title treatment plus the record of how it
// was decided.
type GrainTreatment struct {
	// Denoise is the libavfilter graph to run every encoder input and metric
	// reference frame through; empty means untreated.
	Denoise string
	// TablePath is the film grain table to attach, materialized in the work
	// directory; empty means none.
	TablePath string
	Stats     *perf.GrainTreatmentStats
}

// GrainGateInput describes the title the gate measures.
type GrainGateInput struct {
	InputPath string
	WorkDir   string
	Info      *video.Info
	Chunks    []chunk.Chunk
	CropRect  *video.CropRect
	// DisplayPath is the CVVDP display model used to score the denoise
	// ceiling; empty skips the ceiling measurement.
	DisplayPath string
	// BandTopJOD is the top of the configured target-quality band, recorded
	// in the stats for consumers judging the measured ceiling.
	BandTopJOD float64
	Verbose    func(string)
}

// RecordedGrainTreatment returns the treatment this work directory has
// already settled on, without running the gate: an explicit override, the
// verdict a previous run recorded, or nothing decided yet. The chunked
// pipeline builds the resume manifest from it, so a work directory can never
// be resumed under a treatment its finished chunks were not encoded with.
func RecordedGrainTreatment(mode string, cfg *EncodeConfig, in GrainGateInput) (GrainTreatment, error) {
	return resolveGrainTreatment(context.Background(), mode, cfg, in, false)
}

// ResolveGrainTreatment decides how a title is treated and materializes what
// the encode needs. mode is config.GrainTreatmentAuto or GrainTreatmentOff.
// Explicit experimental cfg.Denoise/cfg.GrainTable win over the gate.
//
// A verdict recorded by an earlier run of the same work directory is reused
// verbatim: re-running the gate could disagree with the treatment the already
// encoded chunks were produced under. The resume manifest discards the verdict
// along with the rest of the state when the input or encode settings change.
func ResolveGrainTreatment(ctx context.Context, mode string, cfg *EncodeConfig, in GrainGateInput) (GrainTreatment, error) {
	return resolveGrainTreatment(ctx, mode, cfg, in, true)
}

// resolveGrainTreatment is the shared decision. With gate false it reports
// only what is already known and materializes nothing.
func resolveGrainTreatment(ctx context.Context, mode string, cfg *EncodeConfig, in GrainGateInput, gate bool) (GrainTreatment, error) {
	if cfg.Denoise != "" || (cfg.GrainTable != nil && *cfg.GrainTable != "") {
		table := ""
		if cfg.GrainTable != nil {
			table = *cfg.GrainTable
		}
		return GrainTreatment{
			Denoise:   cfg.Denoise,
			TablePath: table,
			Stats: &perf.GrainTreatmentStats{
				Mode:            grainModeOverride,
				Treated:         cfg.Denoise != "" || table != "",
				ResolutionClass: resolutionClass(in.codedWidth()),
				Denoise:         cfg.Denoise,
				GrainTable:      table,
				Reason:          "explicit --denoise/--fgs-table overrides the grain gate",
			},
		}, nil
	}
	if mode != config.GrainTreatmentAuto {
		return GrainTreatment{Stats: &perf.GrainTreatmentStats{
			Mode:            config.GrainTreatmentOff,
			ResolutionClass: resolutionClass(in.codedWidth()),
			Reason:          "grain treatment disabled",
		}}, nil
	}

	stats := loadGrainVerdict(in.WorkDir)
	if stats == nil {
		if !gate {
			// Nothing decided yet, and this caller must not decide.
			return GrainTreatment{}, nil
		}
		var err error
		stats, err = runGrainGate(ctx, cfg, in)
		if err != nil {
			return GrainTreatment{}, err
		}
		stats.BandTopJOD = in.BandTopJOD
		// Record the verdict before the ceiling measurement: the verdict is
		// what an interrupted run must resume with, the ceiling is only
		// observability.
		if err := saveGrainVerdict(in.WorkDir, stats); err != nil {
			return GrainTreatment{}, err
		}
		if stats.Treated {
			measureDenoiseCeiling(ctx, in, stats)
			if err := saveGrainVerdict(in.WorkDir, stats); err != nil {
				return GrainTreatment{}, err
			}
		}
	} else {
		// A replayed verdict's timings describe the run that measured them,
		// not this one; Reused keeps stats consumers from reading
		// GateSeconds/CeilingSeconds as fresh wall time. Only the in-memory
		// copy is marked: the recorded verdict stays as first written.
		stats.Reused = true
		if gate && in.Verbose != nil {
			in.Verbose("Grain gate: reusing the verdict recorded in the work directory")
		}
	}

	treatment := GrainTreatment{Stats: stats}
	if stats.Treated {
		treatment.Denoise = stats.Denoise
		if gate {
			path, err := writeGrainTable(in.WorkDir, stats.Tier)
			if err != nil {
				return GrainTreatment{}, err
			}
			treatment.TablePath = path
		}
	}
	return treatment, nil
}

func (in GrainGateInput) codedWidth() uint32 {
	width, _ := video.OutputDimensions(in.Info, in.CropRect)
	return width
}

// runGrainGate probes sample chunks at a fixed CRF and converts the bits they
// cost into a treatment verdict.
func runGrainGate(ctx context.Context, cfg *EncodeConfig, in GrainGateInput) (*perf.GrainTreatmentStats, error) {
	width, height := video.OutputDimensions(in.Info, in.CropRect)
	stats := &perf.GrainTreatmentStats{
		Mode:            config.GrainTreatmentAuto,
		ResolutionClass: resolutionClass(width),
		GateCRF:         float64(grainGateCRF),
	}
	if stats.ResolutionClass == "sd" {
		// SD sources are too small to spend bits on grain synthesis and too
		// far from the measured corpus to trust a cutoff on.
		stats.Reason = "SD sources are never treated"
		return stats, nil
	}
	stats.LightBPPCutoff, stats.MedBPPCutoff = bppCutoffs(width)

	samples := selectGrainSampleChunks(in.Chunks, in.Info.Frames)
	if len(samples) == 0 {
		stats.Reason = "no chunk long enough to measure"
		return stats, nil
	}

	gateDir := filepath.Join(in.WorkDir, "gate")
	if err := os.MkdirAll(gateDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create grain gate directory: %w", err)
	}
	// The sample encodes are measurements, not output: remove them (and the
	// directory) as soon as their bitrates are read.
	defer func() { _ = os.RemoveAll(gateDir) }()

	gateCfg := *cfg
	gateCfg.CRF = grainGateCRF
	gateCfg.Denoise = ""
	gateCfg.GrainTable = nil

	start := time.Now()
	for _, ch := range samples {
		bpp, err := measureChunkBPP(ctx, &gateCfg, in, ch, filepath.Join(gateDir, fmt.Sprintf("%04d.ivf", ch.Idx)), width, height)
		if err != nil {
			return nil, err
		}
		stats.SampleChunks = append(stats.SampleChunks, ch.Idx)
		stats.SampleBPP = append(stats.SampleBPP, bpp)
		if in.Verbose != nil {
			in.Verbose(fmt.Sprintf("Grain gate sample chunk=%04d frames=%d crf=%s bpp=%.4f (%.1f Mbps)", ch.Idx, ch.Frames(), quality.FormatCRF(grainGateCRF), bpp, mbpsFromBPP(bpp, width, height, in.Info)))
		}
	}
	stats.GateSeconds = time.Since(start).Seconds()
	stats.MedianBPP = median(stats.SampleBPP)
	stats.Tier = grainTierFor(stats.MedianBPP, stats.LightBPPCutoff, stats.MedBPPCutoff)
	if stats.Tier != grainTierNone {
		stats.Treated = true
		stats.Denoise = grainDenoiseFilter
		stats.GrainTable = grainTableName(stats.Tier)
	}
	return stats, nil
}

// measureChunkBPP encodes one sample chunk and returns its bits per pixel per
// frame over the coded (post-crop) geometry.
func measureChunkBPP(ctx context.Context, gateCfg *EncodeConfig, in GrainGateInput, ch chunk.Chunk, outputPath string, width, height uint32) (float64, error) {
	src, err := video.Open(in.InputPath, 1)
	if err != nil {
		return 0, fmt.Errorf("failed to open source for grain gate: %w", err)
	}
	defer src.Close()

	result := encodeChunkStreaming(ctx, src.FrameReader(in.Info, in.CropRect), ch, in.Info, gateCfg, outputPath, gateCfg.CRF, width, height, nil)
	if result.Error != nil {
		return 0, fmt.Errorf("grain gate sample chunk %04d failed: %w", ch.Idx, result.Error)
	}
	f, err := os.Open(outputPath)
	if err != nil {
		return 0, fmt.Errorf("failed to open grain gate sample: %w", err)
	}
	defer func() { _ = f.Close() }()
	videoBytes, err := encoder.IVFVideoBytes(bufio.NewReaderSize(f, 1<<20))
	if err != nil {
		return 0, fmt.Errorf("failed to measure grain gate sample: %w", err)
	}
	pixels := float64(width) * float64(height) * float64(ch.Frames())
	if pixels == 0 {
		return 0, fmt.Errorf("grain gate sample chunk %04d has no pixels", ch.Idx)
	}
	return float64(videoBytes) * 8 / pixels, nil
}

// measureDenoiseCeiling scores the denoised source against the real source on
// the same sample chunks. Target-quality scores are measured against the
// denoised reference, so without this the run would report quality it does not
// deliver. Best effort: a ceiling failure must not fail the encode.
func measureDenoiseCeiling(ctx context.Context, in GrainGateInput, stats *perf.GrainTreatmentStats) {
	if in.DisplayPath == "" || len(stats.SampleChunks) == 0 {
		stats.CeilingError = "no display model or sample chunks"
		return
	}
	width, height := video.OutputDimensions(in.Info, in.CropRect)
	proc, err := quality.NewVshipProcessor(width, height, in.Info, in.DisplayPath)
	if err != nil {
		stats.CeilingError = err.Error()
		if in.Verbose != nil {
			in.Verbose(fmt.Sprintf("Grain gate: denoise ceiling not measured: %v", err))
		}
		return
	}
	defer func() { _ = proc.Close() }()

	byIdx := make(map[int]chunk.Chunk, len(in.Chunks))
	for _, ch := range in.Chunks {
		byIdx[ch.Idx] = ch
	}
	start := time.Now()
	var scores []float64
	for _, idx := range stats.SampleChunks {
		ch, ok := byIdx[idx]
		if !ok {
			continue
		}
		res, err := quality.ComputeChunkDenoiseCeiling(ctx, quality.DenoiseCeilingOptions{
			SourcePath: in.InputPath,
			Info:       in.Info,
			Chunk:      ch,
			CropRect:   in.CropRect,
			Width:      width,
			Height:     height,
			Denoise:    stats.Denoise,
			Processor:  proc,
		})
		if err != nil {
			stats.CeilingError = err.Error()
			if in.Verbose != nil {
				in.Verbose(fmt.Sprintf("Grain gate: denoise ceiling not measured: %v", err))
			}
			return
		}
		scores = append(scores, float64(res.Score))
		if in.Verbose != nil {
			in.Verbose(fmt.Sprintf("Grain gate ceiling chunk=%04d denoise_ceiling_jod=%.4f", ch.Idx, res.Score))
		}
	}
	if len(scores) == 0 {
		stats.CeilingError = "no sample chunks scored"
		return
	}
	stats.CeilingSeconds = time.Since(start).Seconds()
	mean, minScore := 0.0, scores[0]
	for _, s := range scores {
		mean += s
		if s < minScore {
			minScore = s
		}
	}
	mean /= float64(len(scores))
	stats.DenoiseCeilingJODMean = &mean
	stats.DenoiseCeilingJODMin = &minScore
	stats.CeilingMeasured = true
}

// selectGrainSampleChunks picks evenly spaced chunks that lie entirely within
// the title's middle and are long enough to measure. Short titles fall back to
// any long-enough chunk, and give up rather than measure noise.
func selectGrainSampleChunks(chunks []chunk.Chunk, frames int) []chunk.Chunk {
	margin := int(float64(frames) * (1 - grainGateMiddleFraction) / 2)
	low, high := margin, frames-margin
	eligible := longChunksWithin(chunks, low, high)
	if len(eligible) == 0 {
		eligible = longChunksWithin(chunks, 0, frames)
	}
	if len(eligible) == 0 {
		return nil
	}
	count := min(grainGateSamples, len(eligible))
	if count == 1 {
		return []chunk.Chunk{eligible[len(eligible)/2]}
	}
	samples := make([]chunk.Chunk, 0, count)
	for i := 0; i < count; i++ {
		samples = append(samples, eligible[i*(len(eligible)-1)/(count-1)])
	}
	return samples
}

func longChunksWithin(chunks []chunk.Chunk, low, high int) []chunk.Chunk {
	var out []chunk.Chunk
	for _, ch := range chunks {
		if ch.Start >= low && ch.End <= high && ch.Frames() >= grainGateMinChunkFrames {
			out = append(out, ch)
		}
	}
	return out
}

func grainTierFor(medianBPP, lightCutoff, medCutoff float64) string {
	switch {
	case medCutoff > 0 && medianBPP >= medCutoff:
		return grainTierMed
	case lightCutoff > 0 && medianBPP >= lightCutoff:
		return grainTierLight
	default:
		return grainTierNone
	}
}

func bppCutoffs(width uint32) (light, med float64) {
	switch resolutionClass(width) {
	case "uhd":
		return uhdLightBPP, uhdMedBPP
	case "hd":
		return hdLightBPP, hdMedBPP
	default:
		return 0, 0
	}
}

func resolutionClass(width uint32) string {
	switch {
	case width >= config.UHDWidthThreshold:
		return "uhd"
	case width >= config.HDWidthThreshold:
		return "hd"
	default:
		return "sd"
	}
}

func grainTableName(tier string) string {
	return "grain-" + tier
}

// writeGrainTable materializes the tier's embedded table in the work
// directory and returns its path; the SVT wrapper reads a table from a file.
func writeGrainTable(workDir, tier string) (string, error) {
	data, err := grainTables.ReadFile(filepath.Join("graintables", grainTableName(tier)+".tbl"))
	if err != nil {
		return "", fmt.Errorf("unknown grain tier %q: %w", tier, err)
	}
	path := filepath.Join(workDir, grainTableName(tier)+".tbl")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write grain table: %w", err)
	}
	return path, nil
}

// grainVerdictPath is the recorded gate decision. It lives in the work
// directory root, so chunk.EnsureResumeManifest's reset removes it with the
// rest of the stale state.
func grainVerdictPath(workDir string) string {
	return filepath.Join(workDir, "grain-gate.json")
}

// loadGrainVerdict returns the recorded verdict, or nil when there is none.
// An unreadable or corrupt verdict is treated as none: re-running the gate is
// always a valid answer, and the manifest catches a disagreement.
func loadGrainVerdict(workDir string) *perf.GrainTreatmentStats {
	data, err := os.ReadFile(grainVerdictPath(workDir))
	if err != nil {
		return nil
	}
	var stats perf.GrainTreatmentStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return nil
	}
	return &stats
}

func saveGrainVerdict(workDir string, stats *perf.GrainTreatmentStats) error {
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode grain gate verdict: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(grainVerdictPath(workDir), data, 0644); err != nil {
		return fmt.Errorf("failed to write grain gate verdict: %w", err)
	}
	return nil
}

// GrainTreatmentSummary is the human-readable verdict for the Encoding
// section: what was measured and what it bought.
func GrainTreatmentSummary(stats *perf.GrainTreatmentStats) []string {
	if stats == nil {
		return nil
	}
	switch stats.Mode {
	case grainModeOverride:
		return []string{"Grain treatment is off because an explicit denoise or film grain table was given."}
	case config.GrainTreatmentOff:
		return []string{"Grain treatment is off."}
	}
	if len(stats.SampleBPP) == 0 {
		return []string{fmt.Sprintf("Grain gate did not run (%s); encoding the source untreated.", stats.Reason)}
	}

	measured := fmt.Sprintf("Grain gate measured %.4f bits per pixel across %d sample chunks at CRF %s (treat above %.4f).",
		stats.MedianBPP, len(stats.SampleBPP), quality.FormatCRF(float32(stats.GateCRF)), stats.LightBPPCutoff)
	if !stats.Treated {
		return []string{measured + " This title is clean, so it encodes untreated."}
	}
	treated := fmt.Sprintf("This title is grainy, so it encodes denoised with %s and the %s film grain table.", stats.Denoise, stats.Tier)
	if stats.DenoiseCeilingJODMean != nil && stats.DenoiseCeilingJODMin != nil {
		treated += fmt.Sprintf(" Denoising itself costs quality: measured against the real source the samples top out at %.2f JOD on average and %.2f at worst.",
			*stats.DenoiseCeilingJODMean, *stats.DenoiseCeilingJODMin)
	}
	return []string{measured, treated}
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// mbpsFromBPP restates a bits-per-pixel measurement as the bitrate it would be
// at this title's geometry and frame rate, which is the unit the study and the
// level cap are quoted in.
func mbpsFromBPP(bpp float64, width, height uint32, inf *video.Info) float64 {
	if inf == nil || inf.FPSDen == 0 {
		return 0
	}
	fps := float64(inf.FPSNum) / float64(inf.FPSDen)
	return bpp * float64(width) * float64(height) * fps / 1e6
}
