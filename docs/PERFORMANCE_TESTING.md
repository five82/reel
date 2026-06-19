# Performance Testing Notes

Current performance guidance for Reel: the settled defaults, the active
target-quality (TQ) strategy, the known bottlenecks and tradeoffs, and the
open/next-test list. Read this first -- it is the short doc.

The full historical record (every dated test entry, the experiments that were
reverted, and the resolved open questions, with measured numbers and git refs)
lives in `docs/PERFORMANCE_TESTING_LOG.md`. This doc summarizes; the log is the
provenance. When a row or claim here cites a dated entry by name, that entry is
in the log.

## Why this exists

Performance work has a long feedback loop: real encodes are slow, many plausible changes only help one clip, and git history alone does not capture why a change was kept or reverted. Keep enough notes here that a future maintainer or coding agent can understand what was tried, what worked, what failed, and what should be tested next.

## How to record a test

For each real encode or replay that informs a tuning decision, add a dated
`## YYYY-MM-DD <title>` entry to `docs/PERFORMANCE_TESTING_LOG.md` (not this
doc), and record:

- Date, machine, input clip, duration, resolution, HDR/SDR.
- Command or test script used.
- Git commit or working-tree change being tested.
- Total wall time and video encoding time.
- TQ summary: chunks, total probes, probes/chunk, probe-count histogram, stop reasons.
- Accuracy summary: sampled JOD min/mean/max, mean absolute error, outside-tolerance count if available.
- Tail behavior: max-probe chunks, chunks with >=3 probes, p90/max window spread.
- Conclusion: keep, revert, retest, or investigate.

If the test changes a default, also update the matching row in "Current
defaults (settled)" below.

Useful artifacts:

- Human log: `*-reellog*`
- Aggregate TQ log: `.reel-*/target-quality.json`
- Per-chunk TQ logs: `.reel-*/tq/*.json`
- Chunk plan: `.reel-*/chunk-plan.txt` and `.reel-*/chunk-plan.json`

## Current defaults (settled)

Quick reference for the tuned knobs and why they are where they are. The
"Provenance" column names the dated entry in `docs/PERFORMANCE_TESTING_LOG.md`
with the full evidence. Update a row when its value changes.

| Knob | Current value | Why | Provenance (in LOG) |
|------|---------------|-----|------------|
| 4K encode concurrency | ceiling `maxWorkers/6` (min 3), start at ceiling | 4K is memory-bandwidth bound, not RAM-capacity bound; higher concurrency does not help and the slow ramp wasted time | 2026-06-12 "4K adaptive ramp: bandwidth, not capacity" |
| Non-4K encode concurrency | ramps to full `maxWorkers` | GPU-metric bound, self-limits via utilization | 2026-06-12 "4K adaptive ramp" |
| Metric (VSHIP/CUDA) workers | 8 below UHD, 4 for UHD | each worker runs its OWN VSHIP CVVDP handler, scored concurrently (restored 2026-06-19). REQUIRES a libvship built with `MITIGATE_MALLOC_ASYNC` (the build script enforces it); without it, coexisting handlers race the CUDA async allocator and corrupt scores. **Count (8/4) is from the 2026-06-12 saturation benchmark on the OLD async-allocator build; the restore spot-confirmed 8 and 4 are correct and ~1.5x faster but did not re-sweep the curve on the sync allocator -- a re-tune candidate (see open items).** | 2026-06-12 "metric-worker scaling"; 2026-06-19 "Metric concurrency RESTORED" |
| `level_of_parallelism` | auto from resolution ramp ceiling → 4K lp 3, non-4K lp 2 (`--level-of-parallelism` overrides) | lp is bitstream-neutral; higher lp fills cores when concurrency is low (~3-4% 4K gain) | 2026-06-13 "SVT-AV1 level_of_parallelism" |
| TQ scheduling block | 32 chunks, largest-first within block | smaller blocks (8) regressed; 32 keeps priors useful | 2026-06-07 entries; "What did not work" |
| TQ probe windows | 3×48 sampled; 5 windows on later probes after high spread; whole-chunk at/below full-probe threshold | sampled probes match full-probe accuracy at lower GPU cost | "Current target-quality strategy"; "What has worked" |
| JOD target range (default) | 9.15-9.55 (center 9.35, half-width 0.20), accepted literally | band width is a speed lever: a half-width below ~2x probe noise (~0.15) wastes probes; +-0.20 converges in 1-2 probes with no streaming-visible loss | 2026-06-14 "Target band WIDTH is the real probe-tail lever" |
| CRF search | adaptive priors from completed chunks + bracket-aware after first two probes | fewer probes without accuracy loss | "What has worked" |
| Pipeline concurrency | slot release during scoring, async scoring, decode/GPU overlap, parallel analysis | overlaps GPU and CPU work for faster TQ | 2026-06-12 "concurrency restructure" |

## Current target-quality strategy

As of this document:

- The configured JOD range is accepted literally; the default range is 9.15-9.55 (center 9.35, half-width 0.20). The band width is a speed lever, not just an accuracy setting -- see "Bottlenecks and key tradeoffs" below and AGENTS.md "Target CVVDP range".
- Chunks are scheduled in timeline blocks of 32 and sorted largest-first within each block.
- TQ probes use sampled windows: normally 3x48 frames, with 5 windows on later probes after high spread.
- Chunks at or below the full-probe threshold (256 frames) are probed whole.
- Search uses adaptive CRF priors from completed chunks.
- Search is bracket-aware after the first two probes:
  - linearly interpolate between the two probes bracketing the target once probes bracket it,
  - use a bounded midpoint for unbracketed low probes,
  - move more aggressively toward high CRF for flat, unbracketed high probes only when the highest-CRF probe is still at least 0.30 JOD above target.
- Scoring is mean/worst blended when sampled-window spread is high.
- A worst-window floor prevents convergence when any sampled window falls below tolerance.
- GPU CVVDP scoring runs **one VSHIP handler per metric worker, concurrently** (restored 2026-06-19; `ComputeChunkCVVDP` is lock-free). This REQUIRES a libvship built with `MITIGATE_MALLOC_ASYNC`: with the default `cudaMallocAsync` allocator, coexisting handlers race a device-global memory pool and silently corrupt scores ~50% of runs (cascading into ~9x output-size swings; 2026-06-19 "Metric concurrency RESTORED"). The build script (`build_svt_av1_usr_local.sh`) enforces the flag, and `scripts/handlertest` re-checks concurrency safety after any libvship/GPU/driver change.

The components above (sampled probes, adaptive priors, block-32 scheduling,
full-first probe reuse, worst-window protections, conditional extra windows,
bracket-aware search) each have their own provenance under "What has worked" in
the log.

## Bottlenecks and key tradeoffs

**Caveat (2026-06-19): the attribution below predates the metric-concurrency restore and is
the top thing to re-confirm.** The 1080p/4K bottleneck split was measured when CVVDP scoring was
either buggy-concurrent or serialized; with N concurrent handlers restored on a clean allocator,
the current binding constraint has not been freshly measured. Treat the two bullets below as the
*last known* attribution, not the present one, until the post-restore re-attribution run (see
"Open questions / next tests") replaces them.

The two constraints as last measured (2026-06-14 "Where to go next"; 2026-06-12 "pipeline
bottleneck attribution"):

- **1080p TQ is GPU-CVVDP-throughput bound** *(pre-restore measurement).* The clip sat at the GPU floor (metric ~65% of busy time). Scheduling/concurrency could not help further -- the GPU scores at a fixed rate. Re-confirm post-restore.
- **4K TQ is memory-bandwidth bound *on the SVT-AV1 encoder*** *(pre-restore measurement).* Active 4K encodes were capped at ~5 (`maxWorkers/6`); pushing higher *raised* per-encode time and worsened wall time. RAM is not the constraint (tens of GiB stay free). Re-confirm post-restore.
- **The metric is no longer serialized (concurrency restored 2026-06-19).** Through 2026-06-18 the single-shared-handler scoring fix serialized CVVDP, making it 81-91% of preset-6 wall (2026-06-18 "Metric serialization is the preset-6 wall bottleneck"). That was undone by restoring N concurrent handlers on a `MITIGATE_MALLOC_ASYNC` libvship, cutting sullyhv-15m wall ~1.5x (1963s -> ~1290s) with no quality change (2026-06-19 "Metric concurrency RESTORED"). **Consequence for the model:** the metric lane now carries *only probe scores*, while the encode lane carries *both* probe-window encodes *and* every chunk's final whole-chunk encode. While the metric was serialized it dominated wall and masked the encode lane; with it parallel again, the fixed per-chunk final preset-6 encodes (which no probe-tuning touches) are an unmasked and likely larger share of wall than the pre-restore attribution reflects. Whether wall is now encoder-bound or still GPU-bound -- and at what worker count the GPU saturates -- is an open re-measurement, not a settled inference. Probe count remains the shared multiplier on both lanes.

From there, the load-bearing facts for any future tuning:

- **Probe count is the multiplier on both bottlenecks.** Every probe is both an encode and a CVVDP scoring, so fewer probes/chunk is the one lever that helps both. Cold-started chunks cost ~2.25 probes vs ~1.34 for neighbor-seeded.
- **Band width is the dominant probe-tail lever.** Probe measurement noise is ~0.075 JOD; a half-width tighter than ~2x that (~0.15) makes probes land just outside the band and march to the monotonicity guard. Widening to +-0.20 cut feature-length probe work ~51% (3.11 -> 1.46 probes/chunk) *and* produced smaller files, with no quality center change (2026-06-14 "Target band WIDTH is the real probe-tail lever", validated by a feature-length encode + fullvalidate).
- **Cutting probes via search tweaks has no free lunch.** Three cheap search-layer ideas were each rejected by simulation before any build: content-prior first-probe seed (activity-vs-CRF sign flips across clips), flat-low early stop (targets only ~6% of guard chunks at feature length), and worst-window/straddle early-out (quality-safe but ~15% larger files -- a speed-vs-size knob, not accuracy-neutral). Probe count cannot be cut by search math alone without larger files or noisier measurements. The one untested lever is probe-sample frame count, which costs more GPU per probe (a real-encode A/B; see open items).
- **Accuracy-trading knobs are settled against ground truth.** The 256-frame full-probe threshold, the 3x48 window size, and the 12s chunk cap were each validated/rejected via `scripts/fullvalidate` (2026-06-13 "Accuracy-trading TQ knobs"). Lowering the full-probe threshold *wrecked* accuracy and *raised* GPU work. Do not touch them without new ground-truth evidence.
- **Short clips under-state probe cost (feature 3.1 vs ~1.7), but the cause is NOT content hardness.** The 2026-06-14 feature run converges at ~3.1 probes/chunk (tight band) vs ~1.7 on a homogeneous cut. The 2026-06-15 work first attributed this to within-chunk worst-window difficulty, but that was a single run that hit the scoring-cascade bug (now fixed, LOG "Cascade root cause + FIX"). **With the fix, the hard-content `sullyhv-15m` clip converges at 1.55 probes/chunk -- the same as easy content -- so at the shipped +-0.20 band content hardness does not raise probes/chunk.** The feature's 3.1 is real *tight-band* behavior. Use `sullyhv-15m-4k-hdr` (deterministic now) as a high-variance test asset and `sully-5m` as the easy control; expect a real probe tail only at a tighter band.
- **`scripts/fullvalidate` is the accuracy ruler.** Sampled scores in `target-quality.json` are what the search *believed*, not ground truth. Judge any accuracy-trading change against full-chunk CVVDP, never sampled scores.

## Open questions / next tests

Layout: **Open** work first (grouped by area, highest value first), then
**Standing guidance** (principles to hold, not to-dos). The resolved questions
are indexed at the bottom of `docs/PERFORMANCE_TESTING_LOG.md`; the dated
entries there carry the full detail. Open items are intentionally unnumbered.

Each open item carries a priority (critical / high / medium / low) with a brief
reason, per AGENTS.md. The highest-value open work is the **post-restore re-baseline**
below: the 2026-06-19 metric-concurrency restore changed the regime, and the current
bottleneck attribution, metric-worker counts, and 4K encode-concurrency ceiling were all
set on the *pre-restore / old-allocator* build. Two **high** items (re-attribute the
bottleneck; re-tune metric workers on the fixed build) re-baseline those cheaply and
unblock the rest. No critical items are open. (The former high item, restore safe metric
concurrency, was **resolved 2026-06-19**: the GPU-scoring cascade was root-caused to the
libvship `cudaMallocAsync` allocator and fixed by a `MITIGATE_MALLOC_ASYNC` rebuild, so N
concurrent handlers are back and the serialization tax is gone -- ~1.5x faster wall; see
LOG "Metric concurrency RESTORED".)

### Open -- post-restore re-baseline (highest value)

The metric-concurrency restore made CVVDP scoring parallel again, so the serialized-metric
bottleneck (81-91% of wall) is gone. But the *current* binding constraint has not been
re-measured -- the "Bottlenecks" attribution is inference carried over from the pre-restore
era, and the worker-count and encode-concurrency defaults were tuned on the OLD
async-allocator build. Re-baselining these is cheap (a couple of instrumented real encodes)
and gates every downstream tuning decision, so it is now the top of the list.

- **Re-attribute the bottleneck on the fixed build.** Instrument one 1080p and one 4K real
  encode: total wall, sum(probe `encode_seconds`), sum(`metric_seconds`), final whole-chunk
  encode time, GPU utilization (`nvidia-smi` sampled), and active encode/metric workers over
  time. The per-probe `encode`/`metric` seconds are already logged per chunk in
  `target-quality.json`; the missing pieces are the final-encode share, GPU saturation, and
  live lane overlap. Output: a present-day breakdown of where wall goes at 1080p vs 4K that
  replaces the inferred attribution in "Bottlenecks and key tradeoffs".
  _Priority: high -- prerequisite for the worker / concurrency / preset decisions below, and
  cheap (two real encodes plus sampling)._
- **Re-tune metric worker count on the sync-allocator (MITIGATE) build.** The 8/4 defaults come
  from the 2026-06-12 saturation benchmark on the async-allocator build; the restore only
  confirmed 8 (1080p) and 4 (4K) are correct and ~1.5x faster -- it did not re-sweep the curve.
  The sync `cudaMalloc`/`cudaFree` allocator has different per-frame overhead, so the saturation
  point may have moved. Sweep worker count (e.g. 4/6/8/10/12 at 1080p, 3/4/5/6 at 4K) on the
  shipping build, measuring wall + GPU util; fold the sweep into the re-attribution run above so
  one harness answers both.
  _Priority: high -- directly tunes throughput on the real build; cheap and high-confidence._
- **Re-check the 4K encode-concurrency ceiling (`maxWorkers/6`) and lp on the fixed build.** Same
  rationale -- the bandwidth-cap divisor and lp auto-derivation were calibrated pre-restore. If
  re-attribution shows the 4K encoder is a larger wall share now, re-sweep the ceiling.
  _Priority: medium -- gated on the re-attribution result; act only if 4K is encoder-bound now._
- **Preset 6 -> 7 A/B (user-approved to test 2026-06-19; default move still needs sign-off).** With
  the metric parallel again, the encode lane carries both probe-window encodes and every chunk's
  final whole-chunk encode, so at preset 6 the final encodes are a larger, now-unmasked share of
  wall -- making preset the biggest remaining throughput lever. Run a preset 6 vs 7 A/B on
  representative 1080p + 4K HDR clips: measure wall / throughput and output size, and gate quality
  on `scripts/fullvalidate` full-chunk CVVDP (not sampled scores) -- preset 7 must hold the JOD
  center and the worst-window floor to qualify. The user has approved *running* the test; moving
  the default still needs the ground-truth result reviewed with the user (AGENTS.md
  "Target-Quality Encoding Philosophy"). Best run after the re-attribution confirms the encoder is
  a meaningful wall share, so the measured throughput win is real and not masked by the GPU.
  _Priority: medium -- potentially the biggest single throughput win; approved to test, but the
  default move stays gated on fullvalidate ground truth + user review of results._

### Latent (fixed root, optional hardening)

- **Floor-guard + seed amplifiers.** The scoring cascade above was amplified by two unguarded
  behaviors that are now untriggered but remain latent: the worst-window floor guard
  (`tq.go:184`) is a hard 9.15 threshold with no noise margin, and a CRF-floored chunk seeds its
  neighbors at the floor (only median-seeded chunks re-probe upward, `target_quality.go:510`).
  Optional defense-in-depth: a small noise margin/hysteresis on the floor guard, and not letting
  floored chunks propagate the floor via priors.
  _Priority: low -- the root scoring bug is fixed, so these never fire today; cheap insurance only._

### Open -- search layer (probe-count tail)

The 2026-06-14 feature-length run showed the probe tail is the dominant real-movie cost
(probes/chunk 3.11, 21% of chunks maxing out, 37% not cleanly converging) and is invisible
on short homogeneous clips. The biggest win for it -- widening the target band -- has now
been taken (default 9.15-9.55), so what remains here is genuinely low-value next to the
post-restore re-baseline above. The three obvious cheap search-layer wins (content-prior
seed, flat-low gate, worst-window early-out) are all rejected by simulation. What remains:

- **Probe-sample noise vs sample-frame count** (unblocked now the scoring bug is fixed, but still
  low value). The idea: the 39% mean-bracket guard group is measurement-noise-limited, so more
  sample frames per probe might cut probes. But the lever is **structurally gated** -- 66-76% of
  chunks are <=256 frames and full-probed, so `sample_frames` never applies to them; raising it to
  96 lifts the full-probe threshold to ~the chunk cap and disables sampling rather than denoising
  it. The 2026-06-15 A/B numbers (sf24 1.54 / sf48 2.78 / sf96 2.75) were cascade-contaminated;
  rerun on the now-deterministic `sullyhv-15m` if pursued.
  _Priority: low -- gated to ~1/3 of chunks; the one remaining probe-tail lever but small reach._
- **Provisional priors from in-progress chunks.** Consider only if probe-count tails persist
  across multiple clips (the feature run says they do; worth revisiting).
  _Priority: low -- gated on the tail recurring across multiple clips; a nontrivial change for a
  speculative gain._
- **Watch high-spread chunks** like `knives5` chunk `0001` where bracket-aware search can add
  a probe; add targeted handling only if this repeats.
  _Priority: low -- single sighting; monitor-only, no action unless it recurs._
- **Smarter sampling on under-represented sub-segments -- RE-CONFIRMED post-fix, now narrow.**
  Re-baselined on the fixed binary 2026-06-18 (LOG "Re-baseline accuracy ground truth on the fixed
  binary"). The two motivating sightings split: the HDR display-peak "bright-frame under-coverage"
  was the **cascade** (dissolves on the fixed ruler -- void), but band-confirm Sully chunk 0664
  (true 8.689 vs sampled 9.679) is **real** -- a byte-identical re-score confirms it. So a genuine
  representativeness gap exists but is rare and narrow: one chunk in 670, specific to a *sampled*
  chunk driven to max CRF where a hard sub-segment between the 3x48 windows is starved and missed.
  The cheap remedy if ever pursued is worst-segment-aware window placement (one window on the
  chunk's hardest/brightest segment, using the per-frame luma shot detection already computes); it
  also stays the prerequisite for any HDR display-peak bump above 1000.
  _Priority: low -- now a single real, rare, max-CRF sighting (the broader HDR evidence was the
  bug); not worth building for one near-invisible chunk per feature until it recurs more widely._

### Open -- performance / infra

- **Restore safe metric concurrency -- RESOLVED 2026-06-19.** Root-caused to the libvship
  `cudaMallocAsync` device-global allocator pool racing across coexisting handlers (NOT an inherent
  Vship design limit, as the 2026-06-16 fix assumed), and fixed by a `MITIGATE_MALLOC_ASYNC` rebuild
  of libvship. N concurrent handlers restored, `quality.gpuMu` removed, ~1.5x faster wall, validated
  end-to-end on 4K + 1080p. Full detail in LOG "Metric concurrency RESTORED".
  _No action -- pointer only; `scripts/handlertest` is the standing re-check after libvship changes._
- **Re-measure the 4K `maxWorkers/6` bandwidth divisor on non-dev hardware** before trusting
  it there (carried over from the resolved Q6 4K adaptive-ramp work; the divisor is
  calibrated on this box's dual-channel DDR5 + 32 cores). Distinct from the post-restore
  re-check of the same divisor on *this* box under the fixed build (see "post-restore re-baseline"
  above) -- this item is specifically about a *different* encode host.
  _Priority: low -- single-user project that runs only on the calibrated box; matters only if the
  encode host changes._
- **Feature-length fullvalidate ground-truth pass** on the kept baseline Sully workdir
  (`~/testing/fulllen-attr/.reel-Sully_t00-1ce039b19801`) to confirm the 30%-overshoot
  finding of the *old* 9.25-9.52 band against true full-chunk CVVDP for an apples-to-apples
  worst-case comparison. ~1h GPU. (The new 9.15-9.55 band already has its ground-truth pass --
  see 2026-06-14 "Target band WIDTH".)
  _Priority: low -- validates a superseded band for the historical record only; the shipped band
  already has its ground-truth pass._

### Standing guidance (hold unless new evidence overrides)

- Keep bracket-aware search, conservative high-side jump gating, and block size 32 unless
  another clip shows a clear regression.
- Do not revisit chunk-boundary complexity unless sampled-window spread or full validation
  shows a repeatable failure that cannot be addressed in sampling/search.
- Use `scripts/fullvalidate` as the accuracy ruler for any accuracy-trading change (window
  count/size, probe thresholds, chunk caps, shot-detection boundaries) -- never sampled scores.
- Changing the target band is an accuracy/size/speed tradeoff: it needs a confirming real
  encode + `scripts/fullvalidate` ground truth before the default moves, and should be
  coordinated with the user (AGENTS.md "Target CVVDP range").
