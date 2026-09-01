package encode

import (
	"context"
	"encoding/binary"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/five82/reel/internal/chunk"
	"github.com/five82/reel/internal/config"
	"github.com/five82/reel/internal/encoder"
	"github.com/five82/reel/internal/perf"
	"github.com/five82/reel/internal/video"
)

func TestSelectGrainSampleChunksSpacesAcrossTheMiddle(t *testing.T) {
	// 100 chunks of 120 frames: 12000 frames, middle 60% is 2400-9600.
	chunks := makeChunks(100, 120)
	samples := selectGrainSampleChunks(chunks, 12000)

	if len(samples) != grainGateSamples {
		t.Fatalf("samples = %d, want %d", len(samples), grainGateSamples)
	}
	for _, ch := range samples {
		if ch.Start < 2400 || ch.End > 9600 {
			t.Errorf("chunk %04d (%d-%d) is outside the middle 60%%", ch.Idx, ch.Start, ch.End)
		}
	}
	for i := 1; i < len(samples); i++ {
		if samples[i].Idx <= samples[i-1].Idx {
			t.Fatalf("samples are not in increasing order: %v", sampleIndices(samples))
		}
	}
	// Evenly spaced means the first and last eligible chunks are the endpoints.
	if samples[0].Idx != 20 || samples[len(samples)-1].Idx != 79 {
		t.Errorf("endpoints = %d..%d, want 20..79", samples[0].Idx, samples[len(samples)-1].Idx)
	}
}

func TestSelectGrainSampleChunksShortTitles(t *testing.T) {
	// Two long chunks, neither inside the middle 60%: the fallback still
	// measures them rather than skipping the gate.
	chunks := []chunk.Chunk{
		{Idx: 0, Start: 0, End: 200},
		{Idx: 1, Start: 200, End: 400},
	}
	samples := selectGrainSampleChunks(chunks, 400)
	if len(samples) != 2 {
		t.Fatalf("samples = %v, want both chunks", sampleIndices(samples))
	}

	// One eligible chunk takes the middle of the eligible list, not an endpoint.
	one := selectGrainSampleChunks([]chunk.Chunk{{Idx: 7, Start: 0, End: 150}}, 150)
	if len(one) != 1 || one[0].Idx != 7 {
		t.Fatalf("single-chunk selection = %v", sampleIndices(one))
	}

	// Nothing long enough to measure: no samples, so the title goes untreated.
	short := selectGrainSampleChunks(makeChunks(40, grainGateMinChunkFrames-1), 40*(grainGateMinChunkFrames-1))
	if len(short) != 0 {
		t.Fatalf("short chunks should not be sampled, got %v", sampleIndices(short))
	}
	if len(selectGrainSampleChunks(nil, 0)) != 0 {
		t.Error("no chunks should produce no samples")
	}
}

func TestGrainTierThresholds(t *testing.T) {
	const uhdWidth, hdWidth, sdWidth = 3840, 1920, 1280

	uhdLight, uhdMed := bppCutoffs(uhdWidth)
	if uhdLight != uhdLightBPP || uhdMed != uhdMedBPP {
		t.Fatalf("UHD cutoffs = %v/%v", uhdLight, uhdMed)
	}
	hdLight, hdMed := bppCutoffs(hdWidth)
	if hdLight != hdLightBPP || hdMed != hdMedBPP {
		t.Fatalf("HD cutoffs = %v/%v, want the calibrated HD constants", hdLight, hdMed)
	}
	if light, med := bppCutoffs(sdWidth); light != 0 || med != 0 {
		t.Fatalf("SD cutoffs = %v/%v, want none", light, med)
	}

	cases := []struct {
		name string
		bpp  float64
		want string
	}{
		{"far above med", 0.30, grainTierMed},
		{"exactly med", uhdMedBPP, grainTierMed},
		{"just below med", uhdMedBPP - 1e-6, grainTierLight},
		{"exactly light", uhdLightBPP, grainTierLight},
		{"just below light", uhdLightBPP - 1e-6, grainTierNone},
		{"clean", 0.01, grainTierNone},
	}
	for _, tc := range cases {
		if got := grainTierFor(tc.bpp, uhdLight, uhdMed); got != tc.want {
			t.Errorf("%s: tier(%.6f) = %q, want %q", tc.name, tc.bpp, got, tc.want)
		}
	}

	// SD has no cutoffs, so nothing can be treated there.
	if got := grainTierFor(10, 0, 0); got != grainTierNone {
		t.Errorf("SD tier = %q, want none", got)
	}
}

// TestGrainCutoffsMatchTheirMbpsLandmarks pins the constants to the bitrates
// their comments quote, which is how the study reported them.
func TestGrainCutoffsMatchTheirMbpsLandmarks(t *testing.T) {
	inf := &video.Info{FPSNum: 24000, FPSDen: 1001}
	for _, tc := range []struct {
		bpp  float64
		mbps float64
	}{
		{uhdLightBPP, 14},
		{uhdMedBPP, 21},
	} {
		got := mbpsFromBPP(tc.bpp, 3840, 2160, inf)
		if math.Abs(got-tc.mbps) > 0.2 {
			t.Errorf("%.4f bpp = %.2f Mbps at 3840x2160@23.976, want %.0f", tc.bpp, got, tc.mbps)
		}
	}
}

func TestMedian(t *testing.T) {
	if got := median([]float64{3, 1, 2}); got != 2 {
		t.Errorf("odd median = %v, want 2", got)
	}
	if got := median([]float64{4, 1, 3, 2}); got != 2.5 {
		t.Errorf("even median = %v, want 2.5", got)
	}
	if got := median(nil); got != 0 {
		t.Errorf("empty median = %v, want 0", got)
	}
}

// TestEmbeddedGrainTablesEncode proves the shipped tables parse through the
// cgo "filmgrn1" reader in the SVT wrapper: a table it rejects fails the
// encode, so a successful encode is the parse.
func TestEmbeddedGrainTablesEncode(t *testing.T) {
	for _, tier := range []string{grainTierLight, grainTierMed} {
		t.Run(tier, func(t *testing.T) {
			workDir := t.TempDir()
			path, err := writeGrainTable(workDir, tier)
			if err != nil {
				t.Fatalf("writeGrainTable(%q): %v", tier, err)
			}
			if err := encodeWithGrainTable(path); err != nil {
				t.Fatalf("encode with %s table: %v", tier, err)
			}
		})
	}

	if _, err := writeGrainTable(t.TempDir(), "nonexistent"); err == nil {
		t.Error("unknown tier should not resolve to a table")
	}

	// Control: a table the reader rejects must fail, otherwise the tests above
	// would pass even if the table were silently ignored.
	bogus := filepath.Join(t.TempDir(), "bogus.tbl")
	if err := os.WriteFile(bogus, []byte("not a grain table\n"), 0644); err != nil {
		t.Fatalf("write bogus table: %v", err)
	}
	if err := encodeWithGrainTable(bogus); err == nil {
		t.Error("a malformed grain table should fail the encode")
	}
}

func encodeWithGrainTable(tablePath string) error {
	out := filepath.Join(os.TempDir(), "reel-grain-table-test.ivf")
	defer func() { _ = os.Remove(out) }()

	const width, height = 64, 64
	cfg := &encoder.EncConfig{
		Inf:        &video.Info{FPSNum: 24, FPSDen: 1},
		CRF:        40,
		Preset:     12,
		Output:     out,
		GrainTable: &tablePath,
		Width:      width,
		Height:     height,
		Frames:     2,
	}
	rng := rand.New(rand.NewSource(1))
	readFrame := func(buf []byte) error {
		for i := 0; i+1 < len(buf); i += 2 {
			binary.LittleEndian.PutUint16(buf[i:], uint16(448+rng.Intn(64)))
		}
		return nil
	}
	return encoder.EncodeChunkToIVF(context.Background(), cfg, readFrame, nil)
}

// TestRecordedVerdictIsReusedNotRegated is the resume contract: once a work
// directory has a verdict, both entry points return it without touching the
// source (these calls would fail if they tried to encode sample chunks: the
// input path does not exist).
func TestRecordedVerdictIsReusedNotRegated(t *testing.T) {
	workDir := t.TempDir()
	recorded := &perf.GrainTreatmentStats{
		Mode:            config.GrainTreatmentAuto,
		Treated:         true,
		Tier:            grainTierMed,
		ResolutionClass: "uhd",
		Denoise:         grainDenoiseFilter,
		GrainTable:      "grain-med",
		MedianBPP:       0.1904,
		SampleChunks:    []int{20, 34, 49, 64, 79},
		SampleBPP:       []float64{0.18, 0.19, 0.1904, 0.20, 0.21},
	}
	if err := saveGrainVerdict(workDir, recorded); err != nil {
		t.Fatalf("saveGrainVerdict: %v", err)
	}

	in := GrainGateInput{InputPath: filepath.Join(workDir, "missing.mkv"), WorkDir: workDir, Info: uhdInfo(), Chunks: makeChunks(100, 120)}
	cfg := &EncodeConfig{}

	got, err := ResolveGrainTreatment(context.Background(), config.GrainTreatmentAuto, cfg, in)
	if err != nil {
		t.Fatalf("ResolveGrainTreatment: %v", err)
	}
	if got.Denoise != grainDenoiseFilter || got.Stats.Tier != grainTierMed {
		t.Fatalf("resumed treatment = %+v", got)
	}
	if got.Stats.MedianBPP != recorded.MedianBPP || len(got.Stats.SampleChunks) != 5 {
		t.Errorf("recorded measurements were not carried through: %+v", got.Stats)
	}
	if _, err := os.Stat(got.TablePath); err != nil {
		t.Errorf("grain table was not materialized: %v", err)
	}

	// The manifest builder's view must agree without materializing anything.
	pre, err := RecordedGrainTreatment(config.GrainTreatmentAuto, cfg, in)
	if err != nil {
		t.Fatalf("RecordedGrainTreatment: %v", err)
	}
	if pre.Denoise != grainDenoiseFilter || pre.Stats.GrainTable != "grain-med" {
		t.Errorf("recorded treatment = %+v", pre)
	}
	if pre.TablePath != "" {
		t.Error("RecordedGrainTreatment must not materialize a table")
	}
}

func TestGrainTreatmentModesWithoutGate(t *testing.T) {
	in := GrainGateInput{WorkDir: t.TempDir(), Info: uhdInfo()}

	off, err := ResolveGrainTreatment(context.Background(), config.GrainTreatmentOff, &EncodeConfig{}, in)
	if err != nil {
		t.Fatalf("off: %v", err)
	}
	if off.Denoise != "" || off.Stats.Treated || off.Stats.Mode != config.GrainTreatmentOff {
		t.Errorf("off mode = %+v", off.Stats)
	}

	table := "/tmp/custom.tbl"
	override, err := ResolveGrainTreatment(context.Background(), config.GrainTreatmentAuto,
		&EncodeConfig{Denoise: "hqdn3d", GrainTable: &table}, in)
	if err != nil {
		t.Fatalf("override: %v", err)
	}
	if override.Denoise != "hqdn3d" || override.TablePath != table || override.Stats.Mode != "override" {
		t.Errorf("override mode = %+v", override.Stats)
	}
	if _, err := os.Stat(filepath.Join(in.WorkDir, "grain-gate.json")); !os.IsNotExist(err) {
		t.Error("explicit overrides must not record a gate verdict")
	}
}

func TestGrainTreatmentSummary(t *testing.T) {
	if GrainTreatmentSummary(nil) != nil {
		t.Error("nil stats should produce no summary")
	}

	clean := GrainTreatmentSummary(&perf.GrainTreatmentStats{
		Mode: config.GrainTreatmentAuto, GateCRF: 22,
		SampleBPP: []float64{0.01, 0.02}, MedianBPP: 0.015, LightBPPCutoff: uhdLightBPP,
	})
	if len(clean) != 1 || !strings.Contains(clean[0], "untreated") {
		t.Errorf("clean summary = %q", clean)
	}

	mean, worst := 9.71, 9.62
	treated := GrainTreatmentSummary(&perf.GrainTreatmentStats{
		Mode: config.GrainTreatmentAuto, GateCRF: 22, Treated: true, Tier: grainTierMed,
		Denoise: grainDenoiseFilter, SampleBPP: []float64{0.19}, MedianBPP: 0.19,
		LightBPPCutoff: uhdLightBPP, DenoiseCeilingJODMean: &mean, DenoiseCeilingJODMin: &worst,
	})
	if len(treated) != 2 || !strings.Contains(treated[1], "fftdnoiz") || !strings.Contains(treated[1], "9.71") {
		t.Errorf("treated summary = %q", treated)
	}
}

func makeChunks(count, frames int) []chunk.Chunk {
	chunks := make([]chunk.Chunk, count)
	for i := range chunks {
		chunks[i] = chunk.Chunk{Idx: i, Start: i * frames, End: (i + 1) * frames}
	}
	return chunks
}

func sampleIndices(chunks []chunk.Chunk) []int {
	idx := make([]int, len(chunks))
	for i, ch := range chunks {
		idx[i] = ch.Idx
	}
	return idx
}

func uhdInfo() *video.Info {
	return &video.Info{Width: 3840, Height: 2160, FPSNum: 24000, FPSDen: 1001, Frames: 12000}
}
