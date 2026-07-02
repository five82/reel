# Reel performance suite

A reproducible harness for running reel over the standard clip matrix and
comparing runs. The repo holds this harness and the contiguous-cut manifest
(`clips.tsv`); the clip bytes and run outputs live under `$REEL_TESTING_DIR`
(default `~/testing`). The `sullyhv` stress clip is a derived local asset rather
than a single-row manifest cut. See `docs/PERFORMANCE_TESTING.md` for the corpus,
artifact boundary, and current tuning guidance.

## Files

| File | Role |
|------|------|
| `clips.tsv` | The clip matrix manifest (recipe): abbr, length, resolution, dynamic range, cut timecodes, source. |
| `run-suite.sh` | Run reel over a set of clips (strict sequential), capturing env metadata, wall, size, GPU util/VRAM, and the per-encode `perf.json` / `target-quality.json`. |
| `analyze.py` | Summarize a run directory: phase timing, worker history, probe histogram, stop reasons, JOD, encode-vs-metric seconds. Writes `summary.json`. |
| `compare-runs.py` | Run-level A/B between two run directories (wall, size, probes, JOD). |

## Usage

```bash
# Build the binary first; the suite uses ./reel by default.
go build -trimpath -o reel ./cmd/reel

# Target-quality run over the standard matrix (default clips):
scripts/perf/run-suite.sh --label baseline

# Wider corpus coverage when changing target-quality behavior:
scripts/perf/run-suite.sh --matrix coverage --label tq-coverage

# Encoder-side A/B on the clips that stress 4K encode throughput:
scripts/perf/run-suite.sh --matrix encoder --label encoder-ab

# Longer clips for serial-phase or baseline-refresh checks:
scripts/perf/run-suite.sh --matrix long --label long-baseline

# Fixed-CRF run over specific clips:
scripts/perf/run-suite.sh --mode crf --crf 30 --label crf30 sully-5m kbv1-5m

# Pass extra flags through to `reel encode` after --:
scripts/perf/run-suite.sh --label mw4 sully-5m -- --metric-workers 4

# Summarize and compare:
scripts/perf/analyze.py "$REEL_TESTING_DIR/perf-runs/<timestamp>-baseline"
scripts/perf/compare-runs.py <run_A> <run_B>
```

By default the bulky `.reel-*` workdir is deleted after the small JSON artifacts
are harvested; pass `--keep-workdirs` to retain it (for example to run
`scripts/fullvalidate` afterward for full-chunk CVVDP ground truth).

## Matrices

| Matrix | Clips | Use |
|--------|-------|-----|
| `default` | `air-5m im-5m bts-5m sully-5m kbv1-5m sullyhv-15m` | Historical A/B anchor; keep using this for continuity unless a change needs more coverage. |
| `coverage` | `air-5m bts-5m im-5m soms-5m io-5m sully-5m kbv1-5m ko-5m sullyhv-15m` | Broad TQ behavior coverage across clean/grainy SDR, clean/grainy 4K, CG, and the stress clip. |
| `encoder` | `sully-5m kbv1-5m ko-5m sullyhv-15m` | Encoder-side A/Bs where 4K encode/memory-bandwidth behavior matters most. |
| `long` | `air-20m bts-20m sully-20m ko-20m` | Baseline refreshes and serial-phase work where 5m clips understate startup/planning cost. |

## Related tools

- `scripts/compare-tq.py` -- per-chunk diff of two target-quality runs.
- `scripts/fullvalidate` -- full-chunk CVVDP ground truth on a kept workdir.
- `scripts/chunkbench` -- shot-detection / chunk-planning benchmark.

## Caveats

- Runs are strictly sequential by design: the single GPU and its CVVDP allocator
  must never host two reel processes at once.
- GPU util/VRAM capture needs `nvidia-smi`; without it those columns are `na`.
- `run-meta.json` records the libvship `.so` path but cannot auto-detect whether
  it was built with `MITIGATE_MALLOC_ASYNC`; ensure the correct build (see
  `docs/VSHIP_CONCURRENCY_BUG.md`) before trusting concurrent CVVDP scores.
- `target-quality.json` scores are the search's whole-chunk probe scores, which
  the converged probe reuses as final output; they match ground truth by
  construction but a separate `scripts/fullvalidate` pass re-scores the muxed
  result for an independent check.
