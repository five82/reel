// fullvalidate scores a finished Reel encode against its source with
// full-chunk CVVDP, giving ground-truth per-chunk JOD instead of the sampled
// scores the target-quality search reports about itself.
//
// Usage: fullvalidate <source.mkv> <workdir>
//
// The workdir is a kept .reel-* directory from the encode (--keep-workdir).
// Each chunk is scored from its standalone encode/NNNN.ivf (read from frame 0,
// no seek). Those per-chunk IVFs losslessly concatenate into the final muxed
// output, so this measures the same bits as the final file but avoids seeking
// into a muxed AV1, which mis-aligns frames and reports spurious low scores.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"codeberg.org/five82/reel/internal/chunk"
	"codeberg.org/five82/reel/internal/config"
	"codeberg.org/five82/reel/internal/quality"
	"codeberg.org/five82/reel/internal/video"
)

type tqChunkLog struct {
	ChunkIdx   int               `json:"chunk_idx"`
	FinalScore float32           `json:"final_sample_score"`
	FinalCRF   float32           `json:"final_crf"`
	Probes     []json.RawMessage `json:"probes"`
}

type tqLog struct {
	Target    float32      `json:"target"`
	Tolerance float32      `json:"tolerance"`
	Chunks    []tqChunkLog `json:"chunks"`
}

type chunkResult struct {
	idx     int
	frames  int
	full    float32
	sampled float32
	probes  int
	crf     float32
	err     error
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "Usage: fullvalidate <source.mkv> <workdir>")
		os.Exit(1)
	}
	sourcePath, workDir := os.Args[1], os.Args[2]

	srcInfo, err := video.Probe(sourcePath)
	fail(err, "probe source")
	manifest, err := readManifest(filepath.Join(workDir, "resume.json"))
	fail(err, "read resume manifest")

	tq, err := readTQLog(filepath.Join(workDir, "target-quality.json"))
	fail(err, "read target-quality.json")
	sampled := make(map[int]tqChunkLog, len(tq.Chunks))
	for _, c := range tq.Chunks {
		sampled[c.ChunkIdx] = c
	}

	frames := manifest.Frames
	if frames == 0 {
		frames = srcInfo.Frames
	}
	srcInfo.Frames = frames
	segments, err := chunk.LoadSegments(filepath.Join(workDir, "chunk-plan.txt"), frames)
	fail(err, "load chunk plan")
	chunks := chunk.Chunkify(segments)

	var cropRect *video.CropRect
	if manifest.CropFilter != "" {
		var w, h, x, y uint32
		_, err := fmt.Sscanf(manifest.CropFilter, "crop=%d:%d:%d:%d", &w, &h, &x, &y)
		fail(err, "parse crop filter")
		cropRect = &video.CropRect{X: x, Y: y, Width: w, Height: h}
	}
	width, height := video.OutputDimensions(srcInfo, cropRect)

	displayPath, err := quality.EnsureDisplayModel(workDir, srcInfo, "")
	fail(err, "display model")

	workers := config.DefaultMetricWorkersForWidth(width)
	if workers > len(chunks) {
		workers = len(chunks)
	}

	fmt.Printf("Source: %s (%d frames)\nWorkdir: %s\nChunks: %d, output %dx%d, crop %q\nTarget: %.2f +/- %.2f JOD, metric workers %d\n\n",
		sourcePath, frames, workDir, len(chunks), width, height, manifest.CropFilter, tq.Target, tq.Tolerance, workers)

	start := time.Now()
	chunkCh := make(chan chunk.Chunk)
	resultCh := make(chan chunkResult, len(chunks))
	// One VSHIP handler per worker, scored concurrently. CVVDP is per-handler
	// temporal state, so each worker owns a distinct handler. Requires a libvship
	// built with MITIGATE_MALLOC_ASYNC -- the default cudaMallocAsync allocator
	// races across coexisting handlers and corrupts scores (see docs LOG).
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			proc, err := quality.NewVshipProcessor(width, height, srcInfo, displayPath)
			fail(err, "processor")
			defer func() { _ = proc.Close() }()
			for ch := range chunkCh {
				// Score the chunk's standalone encoded IVF from frame 0. This
				// is the exact bitstream that is concatenated into the final
				// muxed output, but reading it without a seek avoids the
				// frame mis-alignment a seek into the muxed AV1 produces.
				res, err := quality.ComputeChunkCVVDP(context.Background(), quality.CVVDPOptions{
					SourcePath:      sourcePath,
					ProbePath:       chunk.IVFPath(workDir, ch.Idx),
					ProbeStartFrame: 0,
					Info:            srcInfo,
					Chunk:           ch,
					CropRect:        cropRect,
					Width:           width,
					Height:          height,
					Processor:       proc,
				})
				s := sampled[ch.Idx]
				resultCh <- chunkResult{idx: ch.Idx, frames: ch.Frames(), full: res.Score, sampled: s.FinalScore, probes: len(s.Probes), crf: s.FinalCRF, err: err}
			}
		}()
	}
	go func() {
		for _, ch := range chunks {
			chunkCh <- ch
		}
		close(chunkCh)
	}()
	go func() { wg.Wait(); close(resultCh) }()

	results := make([]chunkResult, 0, len(chunks))
	for r := range resultCh {
		if r.err != nil {
			fail(r.err, fmt.Sprintf("score chunk %04d", r.idx))
		}
		results = append(results, r)
		fmt.Printf("\rScored %d/%d chunks", len(results), len(chunks))
	}
	fmt.Printf("\rScored %d chunks in %s\n\n", len(results), time.Since(start).Round(time.Second))
	sort.Slice(results, func(i, j int) bool { return results[i].idx < results[j].idx })

	report(results, tq.Target, tq.Tolerance)
}

func report(results []chunkResult, target, tolerance float32) {
	var fullErr, sampledGap float64
	var below, above int
	fulls := make([]float64, 0, len(results))
	for _, r := range results {
		fullErr += abs64(r.full - target)
		sampledGap += abs64(r.full - r.sampled)
		fulls = append(fulls, float64(r.full))
		if r.full < target-tolerance {
			below++
		}
		if r.full > target+tolerance {
			above++
		}
	}
	sort.Float64s(fulls)
	n := float64(len(results))

	fmt.Printf("=== Ground truth (full-chunk CVVDP of final output) ===\n")
	fmt.Printf("Full JOD:     min=%.4f p10=%.4f median=%.4f mean=%.4f max=%.4f\n",
		fulls[0], pct(fulls, 0.10), pct(fulls, 0.50), mean(fulls), fulls[len(fulls)-1])
	fmt.Printf("vs target:    mean_abs_error=%.4f below_range=%d above_range=%d of %d\n", fullErr/n, below, above, len(results))
	fmt.Printf("vs sampled:   mean_abs_gap=%.4f (how much the sampled score lied)\n\n", sampledGap/n)

	type row struct {
		r   chunkResult
		gap float64
	}
	rows := make([]row, 0, len(results))
	for _, r := range results {
		rows = append(rows, row{r, abs64(r.full - r.sampled)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].r.full < rows[j].r.full })
	fmt.Printf("Worst chunks by full JOD:\n")
	fmt.Printf("  chunk  frames  crf     full    sampled  gap      probes\n")
	for i := 0; i < len(rows) && i < 10; i++ {
		r := rows[i]
		fmt.Printf("  %04d   %5d   %5.2f  %.4f  %.4f   %+.4f  %d\n",
			r.r.idx, r.r.frames, r.r.crf, r.r.full, r.r.sampled, float64(r.r.full-r.r.sampled), r.r.probes)
	}
}

func readManifest(path string) (chunk.ResumeManifest, error) {
	var m chunk.ResumeManifest
	data, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(data, &m)
}

func readTQLog(path string) (tqLog, error) {
	var t tqLog
	data, err := os.ReadFile(path)
	if err != nil {
		return t, err
	}
	return t, json.Unmarshal(data, &t)
}

func fail(err error, what string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "fullvalidate: %s: %v\n", what, err)
		os.Exit(1)
	}
}

func abs64(v float32) float64 {
	if v < 0 {
		return float64(-v)
	}
	return float64(v)
}

func mean(v []float64) float64 {
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}
