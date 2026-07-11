# Performance Testing

LLM quick-start for Reel performance work. Read this file before proposing a
performance change. Open `docs/PERFORMANCE_TESTING_LOG.md` only when you need the
decision provenance for a specific row below.

## Purpose

These notes exist to stop expensive retesting. Real encodes are slow, GPU/CVVDP
results can be hardware- and build-sensitive, and a later coding session will not
remember what a previous session already tried. The docs should therefore answer:

1. What are the current defaults and why are they that way?
2. What has already been tested, kept, rejected, or marked unsafe?
3. What is still worth testing next?
4. Which artifact should a future agent inspect instead of rerunning an encode?

They are not a raw lab notebook. Large logs, per-run JSON, GPU traces, and
one-off scripts belong under `$REEL_TESTING_DIR` (default `~/testing`) or in git
history, with only the decisive summary kept here.

## Target-quality probe strategy (current)

Every probe scores the **whole chunk** with a single CVVDP pass, at every
resolution. The converged probe IVF is reused verbatim as the final chunk (no
re-encode). There is no sampled-window mode and no full-probe threshold.

Why (2026-06-30 full-scan-vs-sampled matrix, all 4 1080p clips ground-truth
validated; see LOG): the old 3x48 sampled windows used a worst-window pooling
that was systematically pessimistic on the 257-288 frame band (~25% of chunks),
so it under-reported quality and **over-encoded** -- 6 chunks pushed above the
target band across the set, larger files (pooled -8.3% size for full-scan,
content-dependent: clean +2%, film grain +22%), and worse accuracy
(mean_abs_error 0.093 sampled vs 0.082 full). 4K already full-scanned (the 06-29
change) and showed the same thing harder (sampled +7-32% bigger). Full-scan
costs ~+16.6% pooled 1080p wall (1080p is metric-bound) but is exact (score-lie
0.000 by construction) and far simpler. The wall cost falls only on 1080p, the
faster tier; 4K is unaffected. Deleting sampling removed ~620 lines.

## Target band and CVVDP display model

The default target range is **9.15-9.55 JOD** (center 9.35, half-width 0.20).
Band width is the probe-cost knob: it was widened from +-0.135 on 2026-06-14
(commit 714ac5f, full-feature validated: probes/chunk 3.11 -> 1.46, wall -51%)
because a band narrower than ~2x probe measurement noise (~0.075 JOD) wastes
probes marching the search. The current band is ~5x noise, and the 2026-07-02
feature validation shows 1.39 probes/chunk with 70% one-probe convergence, so
further widening has thin upside (the floor is 1.0 probes/chunk) and a real
cost: the band width is the constructive bound on mid-shot join steps (see
Chunking) and the quality floor drops with it. Do not widen without a measured
probe tail.

The center (9.35) and the display model are quality policy, not speed levers.
The default model scores on a 55" 4K panel at 1.3 m (~77 px/deg --
near-critical viewing, about 2x closer than a typical living room at ~151
px/deg), so the measured floor (feature: mean 9.385, p10 9.234, min 9.151)
carries margin at real viewing distances. Lowering the center or relaxing the
display model would shrink files, not speed up encodes -- treat either as a
user-coordinated policy change, and note a display-model change invalidates
the calibrated constants (probe noise ~0.075, initial slope 0.025) and every
prior baseline artifact. The CRF search range (4.25-63.75) is also fine: the
Sully feature used 15.25-63.75 and its single ceiling chunk converged in-band.

## Chunking

`DefaultTargetQualityMaxChunkDuration = 12s` caps chunk size in TQ mode. It is a
**weak lever**: an 8-24s sweep (2026-06-30, full-scan, 1080p + 4K) found wall
flat across the range -- metric work ~= total_frames x probes-per-chunk, which is
independent of chunk size -- with only small content-dependent size/accuracy
wobble and no consistent optimum. 12s is a balanced default, not a tuned peak;
larger ("much larger") gives no throughput win and occasionally worse accuracy.

Boundary placement is also **not a quality lever** (2026-07-02, measured on the
Sully feature): per-chunk TQ pushes every chunk into the band regardless of
where boundaries fall. The worst case is a mid-shot join (synthetic split, 190
of 669 joins), where same-shot neighbors can converge to CRFs 22 apart -- yet
the perceptual step stayed <= 0.303 JOD, under the 0.40 band width that bounds
it by construction and below the steps at natural cuts. Missed cuts inside a
chunk cost bits, not quality (SVT scd=0; full-scan CVVDP still measures them).
Improving detection accuracy or chunk uniformity buys nothing here; the only
knob on the join step is the tolerance band. See the LOG entry.

Dispatch ordering is likewise settled (2026-07-02, assessed from the Sully
feature artifacts): timeline blocks of 32 with largest-first inside each block
feed the neighbor prior well -- 510/670 chunks got a neighbor seed and 467
converged in one probe. A perfect ordering could save at most ~2.5% of probes
(~1% of wall), inside run noise, and ordering cannot affect quality. The
adjacent levers (nearest-seed fallback, staggered prime seeds, default-CRF
sweep, prime at slot target, smaller blocks) were all tested and rejected.

## Current tuning baseline

Use `scripts/perf/run-suite.sh --label tq-baseline` for a clean current-code
baseline; artifacts live under `$REEL_TESTING_DIR/perf-runs/`. The current
reference baseline on this box (RTX 5060 Ti) is
`perf-runs/20260702-124007-tq-baseline-current`: default matrix wall 2556s,
output 1087 MB (refresh after the shot-detection worker default; previous
reference was 2618s / 1090 MB at
`perf-runs/20260702-005326-tq-baseline-decode-slope`). run-suite.sh also samples
host CPU/RAM/disk into a `.host` log per clip; analyze.py reports
cpu_mean/cpu_p90/mem_avail_min.

Matrix hygiene: keep the default matrix as the historical A/B anchor. Use
`--matrix coverage` for broad target-quality behavior changes, `--matrix
encoder` for 4K encoder-side changes, and `--matrix long` for serial-phase or
baseline-refresh checks where 5m clips understate startup/planning cost. The
matrix definitions are in `scripts/perf/README.md` and recorded in
`run-meta.json` for new runs.

A complete feature (Sully, 95m50s 4K HDR) was validated 2026-07-02
(`perf-runs/20260702-013705-feature-validation`): wall 6232s (1.08x video
runtime -- the per-title planning number for 4K features), 670 chunks at 1.39
probes/chunk, 100% converged, zero maxed chunks, all finals in band. The old
feature-length probe tail is CLOSED, not just deferred. Host CPU peaked at
77% p90 and the GPU held 53-56C over 4 hours of continuous load -- no thermal
or host-saturation risk for long batches.

CVVDP early-abort (stopping doomed probes mid-pass) was evaluated on 1305
milestone-logged probes and REJECTED: mid-pass running scores err vs final by
up to 0.47 JOD (wider than the target band), so a safe guard catches too few
doomed probes to matter (~1-2% of metric work). Do not revisit without a
fundamentally better mid-pass predictor.

Metric workers default to **4 at every resolution**. A 2026-06-30 HD A/B found
4 workers slightly faster than the old 6-worker below-UHD default (683s vs 691s,
-1.2% pooled) while cutting VRAM by roughly 1-2 GiB; 8 workers regressed (709s,
+2.6%) and used much more VRAM. UHD remains at 4 because 4K runs are mostly
encoder/memory-bandwidth bound, so extra metric concurrency is not the wall-time
lever.

The CVVDP **source decoder uses 2 threads** (probe decoder 1). One thread
starved the GPU at 4K (HEVC10 ~23 fps single-thread vs a ~50 fps GPU CVVDP
ceiling): threads=2 cut 4K metric_s 12-18% and wall up to -8.8% (kbv1), neutral
at 1080p (2026-07-01, kept). Do not escalate to 4 threads or a src/dist
producer split without re-measuring: encode lanes already absorb the freed CPU
(encode_lane_s +8-12%).

The initial JOD-per-CRF slope is **0.025 for every tier** (2026-07-01; the old
SDR-only 0.04 under-stepped clean digital content title-long: air -18.5% wall
under 0.025). Grainy low-CRF content (true slope 0.06-0.09, e.g. bts) pays a
fixed early-window cost of ~4-8 extra probes per title until the first measured
slope lands, then the learned median takes over. See the LOG before touching
this constant.

UHD **prime-phase concurrency stays at the resolution floor (3)**. Priming at
the slot target (5) was tested and rejected (2026-07-01): once the metric
decoder was unthrottled the prime-phase idle it targeted disappeared, leaving
only extra cold-seeded probes (+4-5% probes/chunk) and +1-3% size.

**A/B methodology note:** output sha256 is NOT stable run-to-run -- chunk
completion order feeds the CRF prior, so probe paths and outputs legitimately
drift. Gate correctness on probe-score identity at shared (chunk, CRF) points
(bit-identical when frames and scoring are unchanged) plus probes/chunk, size,
jod_mae/jod_min, and stop_reasons. Worker history in perf.json is sampled on a
2s timer as of 2026-07-01; earlier runs' max_active/peak_in_flight are
completion-biased (the old 4K "max_active 4 < target 5" was an artifact).

SVT-AV1 preset stays **6**. Preset 7 was rejected on the same matrix: only -1.1%
pooled wall, +10.3% pooled size, and the `sullyhv` stress clip was both slower
(+2.6%) and much larger (+57%). Do not test preset 8 unless a future encoder
version changes that trade-off; preset 7 already crossed the size budget for too
little wall gain.

SVT `level_of_parallelism` stays on the current auto policy. Explicit lp=4 looked
slightly faster on two homogeneous 5m 4K clips (-1.9% pooled) but lost on
`sullyhv` (+1.6% wall); lp=2 was also slightly slower. Treat lp as tested and
not worth changing until hardware/SVT behavior changes.

## Open items

- **SDR <=1080p SSIMU2 probes -- IMPLEMENTED 2026-07-10 (uncommitted):**
  SDR sources at or below 1080p auto-probe with SSIMULACRA2 after a
  bounded per-title CVVDP warmup (20 dual-scored chunks calibrate the
  title's SSIMU2 offset from the `60.8 + 36*(JOD-9.35)` anchor; later
  chunks wait for the lock, then search at `60.8+offset +/- 7.2`).
  Explicit `--target-quality`/`--cvvdp-display` forces CVVDP; HDR and
  >1080p unchanged. Measured: im-20m wall -47% (769->407s), feature-scale
  projection ~-50% (warmup ~4% of a feature's chunks); ground-truth
  quality mean 9.39-9.40 with rare per-chunk outliers to ~9.07; size
  -2..+12% by title (typical +2-5%). Do NOT benchmark this on 5m clips --
  warmup dominates them. VMAF was tested and rejected. See the two LOG
  2026-07-10 entries and `$REEL_TESTING_DIR/metric-research-20260710/`.
  Follow-ups, none blocking: close the warmup CVVDP scorer pool after
  lock (steady-state VRAM would drop ~3.5GiB -> ~0.4GiB -- directly helps
  cross-title pairing); offset estimator samples the first ~16-20
  dispatched (largest-first) chunks -- add diversity if title size
  centering bites; 1080p metric passes are now DECODE-bound (59-91
  fps/worker vs 395 GPU fps) -- metric decoder threads and NVDEC-assisted
  metric decode are the next 1080p levers (hardware decode is on the
  user's backlog; it pays in the encode phase where decode competes with
  SVT lanes, not in shot detection).
- **Shot detection serial cost (partially addressed 2026-07-02):** was 577s on
  a 95m50s 4K feature (9.3% of wall); the logical/2 worker default cut it to
  ~415s (-28%, now ~6.7% of wall). Detection cost is pure HEVC decode
  throughput -- the per-frame analysis is a trivial 64x36 luma signature. A
  chunkbench 16/20/24/32-worker feature sweep found only a marginal additional
  win (414/393/388/394s, identical boundary hash), so the logical/2 default stays:
  24 workers saves just ~26s per feature and oversubscribes decoder threads in
  a hardware-sensitive way. Remaining attacks, none tested: overlapping
  detection with the start of encoding (streams boundaries into the dispatcher;
  hides most of the remaining ~7 min/feature but is invasive -- dispatcher,
  resume, and prior seeding all assume a complete plan), NVDEC-assisted decode
  (estimated only 1.5-2x: one NVDEC engine vs 16-way software), or folding crop
  detection into the same pass (~17s). Gate anything here on identical
  scripts/chunkbench "Boundary hash". Note: 5m test clips cap at 4 workers via
  the 1500-frames-per-worker floor, so worker-count changes only show on
  sullyhv or feature-length content.
- **Cross-title pairing -- PREREQUISITES MET, implemented consumer-side
  (2026-07-04):** the vship MITIGATE_MALLOC_ASYNC workaround holds
  cross-process: a 1080p and a 4K encode as two concurrent reel processes
  left probe scores bit-identical at all 72 shared (chunk, CRF) points vs
  solo baselines (perf-runs/20260703-210442-coex-solo vs
  ~/testing/coex-20260704/pair), pooled wall 510s vs 670s sequential
  (+23.9%, inside the projected 15-35%). Spindle now schedules one encode
  slot per resolution tier (its task-graph Phase 5), keeping run-suite
  A/B benchmarking strictly sequential as required. Original item kept
  below for provenance.
- **Cross-title pairing (deferred by choice, 2026-07-01):** run one 1080p and
  one 4K reel instance concurrently. The lanes are complementary: 1080p is
  metric-bound (GPU ~86%, encode slots ~2 of 8 busy) while 4K is encode-leaning
  (GPU ~40%), and VRAM peaks (3.9 + 5.9 GiB) fit together on the 16 GiB card.
  A 2026-07-01 audit projected 15-35% library-level throughput depending on
  title mix (air+sully pair: ~475-580s vs 757s serial). Prerequisites before
  piloting: validate the vship MITIGATE_MALLOC_ASYNC workaround holds
  cross-process (run 1080p metric passes while a 4K encode runs; gate on
  per-clip sha256), keep run-suite A/B benchmarking strictly sequential, and
  put the pairing policy in spindle, not reel. Adjacent evidence
  (2026-07-04, spindle task-graph Phase 4c): a continuous WhisperX CUDA
  process (3.3 GiB VRAM) running alongside full reel TQ encodes (air-5m +
  sully-5m) left probe scores bit-identical at all 56 shared (chunk, CRF)
  points -- a foreign CUDA process does not perturb CVVDP scoring. This
  does NOT close the reel-vs-reel cross-process prerequisite (different
  allocator/vship regime); artifacts at perf-runs/20260703-210442-coex-solo
  and 20260703-211638-coex-whisperx. Coexistence wall cost on the encode:
  1080p +29.7%, 4K +7.5%.
- Clean digital 1080p (e.g. ARRI-sourced, grain-free) gains little from
  full-scan (~+2% size) while paying the full wall cost. A variance-triggered
  hybrid was considered and rejected as not worth the complexity; revisit only if
  a 1080p-heavy clean-content batch makes the wall cost bite.
- ~~Probe-tail revalidation~~ CLOSED 2026-07-02: the complete Sully feature ran
  1.39 probes/chunk, 100% converged, zero maxed chunks
  (`perf-runs/20260702-013705-feature-validation`).
