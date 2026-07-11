// metriccompare scores (source, distorted) video pairs with CVVDP and/or
// SSIMU2 through libvship. It is research tooling for comparing the two
// metrics against each other, not a production Reel code path.
//
// Jobs mode scores a batch of (source range, distorted encode) pairs and
// writes a JSON report:
//
//	metriccompare --jobs jobs.json --out results.json [--metrics cvvdp,ssimu2] [--per-frame] [--display display.json]
//
// GPU micro-benchmark mode decodes a fixed set of frames once, then times
// repeated metric compute calls only, to isolate GPU metric throughput from
// decode:
//
//	metriccompare --gpubench --src X --dist Y --start N --frames 48 --reps 20 [--crop W:H:X:Y] [--metrics cvvdp,ssimu2]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codeberg.org/five82/reel/internal/chunk"
	"codeberg.org/five82/reel/internal/quality"
	"codeberg.org/five82/reel/internal/video"
)

type jobSpec struct {
	ID     string `json:"id"`
	Src    string `json:"src"`
	Dist   string `json:"dist"`
	Start  int    `json:"start"`
	Frames int    `json:"frames"`
	Crop   string `json:"crop,omitempty"`
}

type jobsFile struct {
	Jobs []jobSpec `json:"jobs"`
}

type cvvdpOut struct {
	Score         float32 `json:"score"`
	MetricSeconds float64 `json:"metric_seconds"`
}

type ssimu2Out struct {
	Mean          float64   `json:"mean"`
	Min           float64   `json:"min"`
	P5            float64   `json:"p5"`
	P10           float64   `json:"p10"`
	MetricSeconds float64   `json:"metric_seconds"`
	PerFrame      []float64 `json:"per_frame,omitempty"`
}

type jobResult struct {
	ID     string     `json:"id"`
	Frames int        `json:"frames"`
	CVVDP  *cvvdpOut  `json:"cvvdp,omitempty"`
	SSIMU2 *ssimu2Out `json:"ssimu2,omitempty"`
}

type resultsFile struct {
	Jobs []jobResult `json:"jobs"`
}

type metricSet struct {
	cvvdp  bool
	ssimu2 bool
}

func main() {
	jobsPath := flag.String("jobs", "", "jobs.json describing (source, distorted) pairs to score")
	outPath := flag.String("out", "", "path to write results.json (jobs mode)")
	metricsFlag := flag.String("metrics", "cvvdp,ssimu2", "comma-separated metrics to compute: cvvdp,ssimu2")
	perFrame := flag.Bool("per-frame", false, "include per-frame SSIMU2 scores in results.json")
	displayPath := flag.String("display", "", "CVVDP display model JSON (default: generate Reel's default model)")

	gpuBench := flag.Bool("gpubench", false, "run GPU metric micro-benchmark mode instead of jobs mode")
	srcFlag := flag.String("src", "", "gpubench: source video path")
	distFlag := flag.String("dist", "", "gpubench: distorted video path (read from frame 0)")
	startFlag := flag.Int("start", 0, "gpubench: first source frame to sample")
	framesFlag := flag.Int("frames", 48, "gpubench: number of frames to decode and cache in RAM")
	repsFlag := flag.Int("reps", 20, "gpubench: number of timed passes over the cached frames")
	cropFlag := flag.String("crop", "", "gpubench: optional crop rect W:H:X:Y (ffmpeg crop filter order)")

	flag.Parse()

	metrics, err := parseMetrics(*metricsFlag)
	fail(err, "parse --metrics")

	if *gpuBench {
		fail(runGPUBench(*srcFlag, *distFlag, *startFlag, *framesFlag, *repsFlag, *cropFlag, *displayPath, metrics), "gpubench")
		return
	}

	if *jobsPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  metriccompare --jobs jobs.json --out results.json [--metrics cvvdp,ssimu2] [--per-frame] [--display display.json]")
		fmt.Fprintln(os.Stderr, "  metriccompare --gpubench --src X --dist Y --start N --frames 48 --reps 20 [--crop W:H:X:Y]")
		os.Exit(1)
	}
	fail(runJobs(*jobsPath, *outPath, *displayPath, *perFrame, metrics), "run jobs")
}

func parseMetrics(csv string) (metricSet, error) {
	var m metricSet
	for _, tok := range strings.Split(csv, ",") {
		switch strings.TrimSpace(tok) {
		case "cvvdp":
			m.cvvdp = true
		case "ssimu2":
			m.ssimu2 = true
		case "":
			// allow trailing/leading commas
		default:
			return m, fmt.Errorf("unknown metric %q (want cvvdp, ssimu2)", tok)
		}
	}
	if !m.cvvdp && !m.ssimu2 {
		return m, fmt.Errorf("no metrics selected")
	}
	return m, nil
}

// parseCrop parses a ffmpeg crop=w:h:x:y-order rectangle "W:H:X:Y". An empty
// string means no crop.
func parseCrop(spec string) (*video.CropRect, error) {
	if spec == "" {
		return nil, nil
	}
	parts := strings.Split(spec, ":")
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid crop %q: want W:H:X:Y", spec)
	}
	vals := make([]uint32, 4)
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid crop %q: %w", spec, err)
		}
		vals[i] = uint32(n)
	}
	return &video.CropRect{Width: vals[0], Height: vals[1], X: vals[2], Y: vals[3]}, nil
}

func runJobs(jobsPath, outPath, displayPath string, perFrame bool, metrics metricSet) error {
	data, err := os.ReadFile(jobsPath)
	if err != nil {
		return fmt.Errorf("read jobs file: %w", err)
	}
	var jf jobsFile
	if err := json.Unmarshal(data, &jf); err != nil {
		return fmt.Errorf("parse jobs file: %w", err)
	}
	if len(jf.Jobs) == 0 {
		return fmt.Errorf("jobs file has no jobs")
	}

	// Vship processors are fixed-dimension for their whole lifetime, and jobs
	// mode creates exactly one processor per metric up front for clean timing
	// (no per-job init cost). That means every job must share the same
	// (cropped) output dimensions and source color/HDR metadata; the first
	// job establishes that baseline and later jobs are validated against it.
	baseInfo, err := video.Probe(jf.Jobs[0].Src)
	if err != nil {
		return fmt.Errorf("probe job %q source: %w", jf.Jobs[0].ID, err)
	}
	baseCrop, err := parseCrop(jf.Jobs[0].Crop)
	if err != nil {
		return fmt.Errorf("job %q: %w", jf.Jobs[0].ID, err)
	}
	width, height := video.OutputDimensions(baseInfo, baseCrop)

	var cvvdpProc *quality.VshipProcessor
	var ssimu2Proc *quality.SSIMU2Processor

	if metrics.cvvdp {
		workDir := filepath.Dir(outPath)
		resolvedDisplay, err := quality.EnsureDisplayModel(workDir, baseInfo, displayPath)
		if err != nil {
			return fmt.Errorf("CVVDP display model: %w", err)
		}
		cvvdpProc, err = quality.NewVshipProcessor(width, height, baseInfo, resolvedDisplay)
		if err != nil {
			return fmt.Errorf("create CVVDP processor: %w", err)
		}
		defer func() { _ = cvvdpProc.Close() }()
	}
	if metrics.ssimu2 {
		ssimu2Proc, err = quality.NewSSIMU2Processor(width, height, baseInfo)
		if err != nil {
			return fmt.Errorf("create SSIMU2 processor: %w", err)
		}
		defer func() { _ = ssimu2Proc.Close() }()
	}

	results := make([]jobResult, 0, len(jf.Jobs))
	for _, job := range jf.Jobs {
		res, err := scoreJob(job, baseInfo, width, height, cvvdpProc, ssimu2Proc, metrics, perFrame)
		if err != nil {
			return fmt.Errorf("job %q: %w", job.ID, err)
		}
		results = append(results, res)
		fmt.Fprintf(os.Stderr, "scored job %s (%d frames)\n", job.ID, job.Frames)
	}

	out := resultsFile{Jobs: results}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(outPath, b, 0o644); err != nil {
		return fmt.Errorf("write results: %w", err)
	}
	return nil
}

func scoreJob(job jobSpec, baseInfo *video.Info, width, height uint32, cvvdpProc *quality.VshipProcessor, ssimu2Proc *quality.SSIMU2Processor, metrics metricSet, perFrame bool) (jobResult, error) {
	srcInfo, err := video.Probe(job.Src)
	if err != nil {
		return jobResult{}, fmt.Errorf("probe source: %w", err)
	}
	cropRect, err := parseCrop(job.Crop)
	if err != nil {
		return jobResult{}, err
	}
	jw, jh := video.OutputDimensions(srcInfo, cropRect)
	if jw != width || jh != height {
		return jobResult{}, fmt.Errorf("cropped source dimensions %dx%d differ from the first job's %dx%d; metriccompare jobs mode requires consistent dimensions across all jobs because the metric processors are created once up front", jw, jh, width, height)
	}

	distInfo, err := video.Probe(job.Dist)
	if err != nil {
		return jobResult{}, fmt.Errorf("probe distorted: %w", err)
	}
	if distInfo.Width != jw || distInfo.Height != jh {
		return jobResult{}, fmt.Errorf("distorted dimensions %dx%d do not match cropped source dimensions %dx%d", distInfo.Width, distInfo.Height, jw, jh)
	}

	res := jobResult{ID: job.ID, Frames: job.Frames}
	jobChunk := chunk.Chunk{Idx: 0, Start: job.Start, End: job.Start + job.Frames}

	if metrics.cvvdp {
		cr, err := quality.ComputeChunkCVVDP(context.Background(), quality.CVVDPOptions{
			SourcePath: job.Src,
			ProbePath:  job.Dist,
			Info:       srcInfo,
			Chunk:      jobChunk,
			CropRect:   cropRect,
			Width:      width,
			Height:     height,
			Processor:  cvvdpProc,
		})
		if err != nil {
			return jobResult{}, fmt.Errorf("cvvdp: %w", err)
		}
		res.CVVDP = &cvvdpOut{Score: cr.Score, MetricSeconds: cr.MetricSeconds}
	}

	if metrics.ssimu2 {
		sr, err := quality.ComputeChunkSSIMU2(context.Background(), quality.SSIMU2Options{
			SourcePath: job.Src,
			ProbePath:  job.Dist,
			Info:       srcInfo,
			Chunk:      jobChunk,
			CropRect:   cropRect,
			Width:      width,
			Height:     height,
			Processor:  ssimu2Proc,
		})
		if err != nil {
			return jobResult{}, fmt.Errorf("ssimu2: %w", err)
		}
		out := &ssimu2Out{Mean: sr.Mean, Min: sr.Min, P5: sr.P5, P10: sr.P10, MetricSeconds: sr.MetricSeconds}
		if perFrame {
			out.PerFrame = sr.PerFrame
		}
		res.SSIMU2 = out
	}

	return res, nil
}

// runGPUBench decodes the requested frame range once into RAM, then times
// repeated metric compute passes over the cached frames only, isolating GPU
// metric throughput from CPU decode.
func runGPUBench(srcPath, distPath string, start, frames, reps int, cropSpec, displayPath string, metrics metricSet) error {
	if srcPath == "" || distPath == "" {
		return fmt.Errorf("--gpubench requires --src and --dist")
	}
	if frames <= 0 || reps <= 0 {
		return fmt.Errorf("--frames and --reps must be positive")
	}

	srcInfo, err := video.Probe(srcPath)
	if err != nil {
		return fmt.Errorf("probe source: %w", err)
	}
	cropRect, err := parseCrop(cropSpec)
	if err != nil {
		return err
	}
	width, height := video.OutputDimensions(srcInfo, cropRect)

	distInfo, err := video.Probe(distPath)
	if err != nil {
		return fmt.Errorf("probe distorted: %w", err)
	}
	if distInfo.Width != width || distInfo.Height != height {
		return fmt.Errorf("distorted dimensions %dx%d do not match cropped source dimensions %dx%d", distInfo.Width, distInfo.Height, width, height)
	}

	fmt.Fprintf(os.Stderr, "decoding %d frames at %dx%d into RAM...\n", frames, width, height)
	srcPlanes, distPlanes, err := decodeFramesToRAM(srcPath, distPath, srcInfo, distInfo, cropRect, start, frames, width, height)
	if err != nil {
		return err
	}

	workDir := os.TempDir()

	if metrics.cvvdp {
		resolvedDisplay, err := quality.EnsureDisplayModel(workDir, srcInfo, displayPath)
		if err != nil {
			return fmt.Errorf("CVVDP display model: %w", err)
		}
		proc, err := quality.NewVshipProcessor(width, height, srcInfo, resolvedDisplay)
		if err != nil {
			return fmt.Errorf("create CVVDP processor: %w", err)
		}
		defer func() { _ = proc.Close() }()

		var lastScore float32
		t0 := time.Now()
		for r := 0; r < reps; r++ {
			if err := proc.ResetCVVDP(); err != nil {
				return fmt.Errorf("cvvdp reset: %w", err)
			}
			for i := 0; i < frames; i++ {
				lastScore, err = proc.ComputeCVVDP(srcPlanes[i], distPlanes[i])
				if err != nil {
					return fmt.Errorf("cvvdp compute: %w", err)
				}
			}
		}
		elapsed := time.Since(t0).Seconds()
		fps := float64(frames*reps) / elapsed
		fmt.Printf("cvvdp:  %.2f frames/sec  (last score %.4f JOD, %d frames x %d reps in %.2fs)\n", fps, lastScore, frames, reps, elapsed)
	}

	if metrics.ssimu2 {
		proc, err := quality.NewSSIMU2Processor(width, height, srcInfo)
		if err != nil {
			return fmt.Errorf("create SSIMU2 processor: %w", err)
		}
		defer func() { _ = proc.Close() }()

		var lastScore float64
		t0 := time.Now()
		for r := 0; r < reps; r++ {
			for i := 0; i < frames; i++ {
				lastScore, err = proc.ComputeSSIMU2(srcPlanes[i], distPlanes[i])
				if err != nil {
					return fmt.Errorf("ssimu2 compute: %w", err)
				}
			}
		}
		elapsed := time.Since(t0).Seconds()
		fps := float64(frames*reps) / elapsed
		fmt.Printf("ssimu2: %.2f frames/sec  (last score %.4f, %d frames x %d reps in %.2fs)\n", fps, lastScore, frames, reps, elapsed)
	}

	return nil
}

func decodeFramesToRAM(srcPath, distPath string, srcInfo, distInfo *video.Info, cropRect *video.CropRect, start, frames int, width, height uint32) ([]quality.FramePlanes, []quality.FramePlanes, error) {
	src, err := video.Open(srcPath, 2)
	if err != nil {
		return nil, nil, fmt.Errorf("open source: %w", err)
	}
	defer src.Close()
	dist, err := video.Open(distPath, 1)
	if err != nil {
		return nil, nil, fmt.Errorf("open distorted: %w", err)
	}
	defer dist.Close()

	srcPlanes := make([]quality.FramePlanes, frames)
	distPlanes := make([]quality.FramePlanes, frames)
	frameSize := int(width) * int(height) * 3 // 10-bit YUV420: Y(2B/px) + U/V(0.5B/px each *2B)

	for i := 0; i < frames; i++ {
		srcBuf := make([]byte, frameSize)
		if err := src.ReadFrame(start+i, srcBuf, srcInfo, cropRect); err != nil {
			return nil, nil, fmt.Errorf("read source frame %d: %w", start+i, err)
		}
		planes, err := quality.PlanesFromYUV420P10(srcBuf, width, height)
		if err != nil {
			return nil, nil, err
		}
		srcPlanes[i] = planes

		distBuf := make([]byte, frameSize)
		if err := dist.ReadFrame(i, distBuf, distInfo, nil); err != nil {
			return nil, nil, fmt.Errorf("read distorted frame %d: %w", i, err)
		}
		planes, err = quality.PlanesFromYUV420P10(distBuf, width, height)
		if err != nil {
			return nil, nil, err
		}
		distPlanes[i] = planes
	}

	return srcPlanes, distPlanes, nil
}

func fail(err error, what string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "metriccompare: %s: %v\n", what, err)
		os.Exit(1)
	}
}
