// Package perf collects per-file pipeline timing and adaptive-worker history
// and writes a perf.json artifact into the work directory.
//
// The artifact gives performance attribution beyond verbose logs and throwaway
// scripts: it records the wall time of each pipeline phase (analysis, crop,
// shot detection, chunk planning, resume, audio, video encode/target-quality,
// merge, mux, validation, stream-size scans) and a sampled history of the
// adaptive encode scheduler (active/target/max workers, in-flight chunks,
// metric workers, and cumulative encode-slot wait time).
//
// perf.json lives in the work directory, so it is written only when that
// directory will be kept: after a successful encode with --keep-workdir, and
// after a failed encode whose work directory survives for resume (the final
// output was not produced). That matches the performance-testing workflow,
// which keeps the work directory to read target-quality.json and the per-chunk
// logs, and still yields partial timing when an encode fails partway.
//
// Phases may overlap: audio extraction runs concurrently with video encoding
// and merging, so the sum of phase durations can exceed total_seconds. Each
// phase records its real wall-clock window (start_seconds, duration_seconds)
// relative to the run start so analysis tooling can reconstruct the timeline.
package perf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// schemaVersion is bumped when the perf.json layout changes incompatibly.
const schemaVersion = 1

// workerSampleMinInterval throttles unchanged worker-history samples. A sample
// is always recorded when any worker count changes; otherwise samples are kept
// no closer together than this so a feature-length encode stays bounded.
const workerSampleMinInterval = 2.0

// Meta holds the static description of an encoded file. Fields are flattened
// into the top level of perf.json.
type Meta struct {
	SchemaVersion      int     `json:"schema_version"`
	InputFile          string  `json:"input_file"`
	OutputFile         string  `json:"output_file"`
	Width              uint32  `json:"width"`
	Height             uint32  `json:"height"`
	HDR                bool    `json:"hdr"`
	DurationSeconds    float64 `json:"duration_seconds"`
	Frames             int     `json:"frames"`
	QualityMode        string  `json:"quality_mode"`
	TargetQuality      string  `json:"target_quality,omitempty"`
	CRF                float32 `json:"crf,omitempty"`
	Preset             uint8   `json:"preset"`
	MetricWorkers      int     `json:"metric_workers"`
	MaxAdaptiveWorkers int     `json:"max_adaptive_workers"`
	Chunks             int     `json:"chunks"`
	SVTAV1Version      string  `json:"svtav1_version,omitempty"`
	Hostname           string  `json:"hostname,omitempty"`
}

// Phase is the wall-clock window of one pipeline phase, anchored to run start.
type Phase struct {
	Name            string  `json:"name"`
	StartSeconds    float64 `json:"start_seconds"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// WorkerSample is a point-in-time snapshot of the adaptive encode scheduler.
type WorkerSample struct {
	TSeconds              float64 `json:"t_seconds"`
	Active                int     `json:"active"`
	Target                int     `json:"target"`
	Max                   int     `json:"max"`
	InFlight              int     `json:"in_flight"`
	ChunksComplete        int     `json:"chunks_complete"`
	FramesComplete        int     `json:"frames_complete"`
	EncodeSlotWaitSeconds float64 `json:"encode_slot_wait_seconds"`
}

// Collector accumulates phase timings and worker history for one encoded file.
//
// It is safe for concurrent use: phases are recorded from the orchestration
// goroutine while worker samples arrive from the encode progress callback. All
// methods are nil-safe so callers can pass a nil *Collector to disable
// collection without branching at every call site.
type Collector struct {
	mu      sync.Mutex
	start   time.Time
	workDir string
	meta    Meta
	phases  []Phase
	samples []WorkerSample

	haveSample bool
	lastSample WorkerSample
}

// New returns a Collector whose timeline is anchored to now.
func New() *Collector {
	return &Collector{
		start: time.Now(),
		meta:  Meta{SchemaVersion: schemaVersion},
	}
}

// SetWorkDir records where Write should emit perf.json. Until it is set, Write
// is a no-op (there is no artifact directory to anchor to).
func (c *Collector) SetWorkDir(dir string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.workDir = dir
	c.mu.Unlock()
}

// UpdateMeta mutates the file metadata under the collector lock.
func (c *Collector) UpdateMeta(fn func(m *Meta)) {
	if c == nil || fn == nil {
		return
	}
	c.mu.Lock()
	fn(&c.meta)
	c.mu.Unlock()
}

// RecordPhase appends a finished phase. start and stop are absolute timestamps;
// the phase is stored relative to the collector's run start.
func (c *Collector) RecordPhase(name string, start, stop time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.phases = append(c.phases, Phase{
		Name:            name,
		StartSeconds:    start.Sub(c.start).Seconds(),
		DurationSeconds: stop.Sub(start).Seconds(),
	})
	c.mu.Unlock()
}

// RecordWorkerSample appends an adaptive-scheduler snapshot. The caller leaves
// TSeconds zero; the collector stamps it relative to run start. Samples whose
// worker counts are unchanged from the previous sample and that arrive within
// workerSampleMinInterval are dropped to bound the history size.
func (c *Collector) RecordWorkerSample(s WorkerSample) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s.TSeconds = time.Since(c.start).Seconds()
	if c.haveSample {
		last := c.lastSample
		unchanged := s.Active == last.Active && s.Target == last.Target &&
			s.Max == last.Max && s.InFlight == last.InFlight
		if unchanged && s.TSeconds-last.TSeconds < workerSampleMinInterval {
			return
		}
	}
	c.samples = append(c.samples, s)
	c.lastSample = s
	c.haveSample = true
}

// Write emits perf.json into the work directory. It is best-effort: if no work
// directory was set (for example the encode never reached the chunked pipeline)
// it returns nil without writing.
func (c *Collector) Write() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.workDir == "" {
		return nil
	}

	phases := append([]Phase(nil), c.phases...)
	sort.SliceStable(phases, func(i, j int) bool {
		return phases[i].StartSeconds < phases[j].StartSeconds
	})

	out := struct {
		Meta
		TotalSeconds  float64        `json:"total_seconds"`
		Phases        []Phase        `json:"phases"`
		WorkerHistory []WorkerSample `json:"worker_history"`
	}{
		Meta:          c.meta,
		TotalSeconds:  time.Since(c.start).Seconds(),
		Phases:        phases,
		WorkerHistory: c.samples,
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(c.workDir, "perf.json"), data, 0644)
}
