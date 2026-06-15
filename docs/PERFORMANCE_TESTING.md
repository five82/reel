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
| Metric (VSHIP/CUDA) workers | 8 below UHD, 4 for UHD | bandwidth-capped encoder sustains ~5 active 4K workers, so scoring is not the critical path; 6 only added VRAM | 2026-06-13 "4K metric workers 4 vs 6 retest" |
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

The components above (sampled probes, adaptive priors, block-32 scheduling,
full-first probe reuse, worst-window protections, conditional extra windows,
bracket-aware search) each have their own provenance under "What has worked" in
the log.

## Bottlenecks and key tradeoffs

The two binding constraints are pinned by measurement, not guess (2026-06-14
"Where to go next"; 2026-06-12 "pipeline bottleneck attribution"):

- **1080p TQ is GPU-CVVDP-throughput bound.** The clip sits at the GPU floor (metric ~65% of busy time). Scheduling/concurrency cannot help further -- the GPU scores at a fixed rate.
- **4K TQ is memory-bandwidth bound on SVT-AV1.** Capped at ~5 active encodes (`maxWorkers/6`); pushing higher *raised* per-encode time and worsened wall time. RAM is not the constraint (tens of GiB stay free).

From there, the load-bearing facts for any future tuning:

- **Probe count is the multiplier on both bottlenecks.** Every probe is both an encode and a CVVDP scoring, so fewer probes/chunk is the one lever that helps both. Cold-started chunks cost ~2.25 probes vs ~1.34 for neighbor-seeded.
- **Band width is the dominant probe-tail lever.** Probe measurement noise is ~0.075 JOD; a half-width tighter than ~2x that (~0.15) makes probes land just outside the band and march to the monotonicity guard. Widening to +-0.20 cut feature-length probe work ~51% (3.11 -> 1.46 probes/chunk) *and* produced smaller files, with no quality center change (2026-06-14 "Target band WIDTH is the real probe-tail lever", validated by a feature-length encode + fullvalidate).
- **Cutting probes via search tweaks has no free lunch.** Three cheap search-layer ideas were each rejected by simulation before any build: content-prior first-probe seed (activity-vs-CRF sign flips across clips), flat-low early stop (targets only ~6% of guard chunks at feature length), and worst-window/straddle early-out (quality-safe but ~15% larger files -- a speed-vs-size knob, not accuracy-neutral). Probe count cannot be cut by search math alone without larger files or noisier measurements. The one untested lever is probe-sample frame count, which costs more GPU per probe (a real-encode A/B; see open items).
- **Accuracy-trading knobs are settled against ground truth.** The 256-frame full-probe threshold, the 3x48 window size, and the 12s chunk cap were each validated/rejected via `scripts/fullvalidate` (2026-06-13 "Accuracy-trading TQ knobs"). Lowering the full-probe threshold *wrecked* accuracy and *raised* GPU work. Do not touch them without new ground-truth evidence.
- **Short clips under-state probe cost.** Feature-length content converges at ~3.1 probes/chunk vs ~1.7 on a homogeneous 5-20m cut, because a full film has much higher chunk-to-chunk CRF variance (2026-06-14 "Full-length 4K encode"). Use short clips for accuracy ground-truth and throughput/bitstream knobs, but validate search-layer changes against high-variance content.
- **`scripts/fullvalidate` is the accuracy ruler.** Sampled scores in `target-quality.json` are what the search *believed*, not ground truth. Judge any accuracy-trading change against full-chunk CVVDP, never sampled scores.

## Open questions / next tests

Layout: **Open** work first (grouped by area, highest value first), then
**Standing guidance** (principles to hold, not to-dos). The resolved questions
are indexed at the bottom of `docs/PERFORMANCE_TESTING_LOG.md`; the dated
entries there carry the full detail. Open items are intentionally unnumbered.

### Open -- search layer (probe-count tail; highest value)

The 2026-06-14 feature-length run showed the probe tail is the dominant real-movie cost
(probes/chunk 3.11, 21% of chunks maxing out, 37% not cleanly converging) and is invisible
on short homogeneous clips. The biggest win for it -- widening the target band -- has now
been taken (default 9.15-9.55). The three obvious cheap search-layer wins (content-prior
seed, flat-low gate, worst-window early-out) are all rejected by simulation. What remains:

- **Probe-sample noise vs sample-frame count** (the one genuine, untested lever). The 39%
  mean-bracket guard group is *measurement-noise*-limited, not search-limited (interpolation only
  ~60% reliable; residuals are noise not curvature). More sample frames per probe would lower that
  noise and could cut probes needed to localise the band -- but it costs more GPU per probe, so the
  net is unknown and needs a real A/B (probe_sample_frames up vs down on a high-variance clip),
  measured as total TQ wall time, not probe count. This is the next real experiment if the tail is
  worth pursuing at all.
- **Provisional priors from in-progress chunks.** Consider only if probe-count tails persist
  across multiple clips (the feature run says they do; worth revisiting).
- **Watch high-spread chunks** like `knives5` chunk `0001` where bracket-aware search can add
  a probe; add targeted handling only if this repeats.
- **Smarter sampling on high-spread chunks.** The one real outlier in the band-confirm encode
  (Sully chunk 0664, true 8.689 vs sampled 9.679) was a sampling miss on a chunk pushed to the CRF
  ceiling, not a band problem. Conditional extra windows on high-spread chunks remain the standing
  remedy (AGENTS.md "How to Improve Target-Quality Results", item 1).

### Open -- performance / infra

- **Re-measure the 4K `maxWorkers/6` bandwidth divisor on non-dev hardware** before trusting
  it there (carried over from the resolved Q6 4K adaptive-ramp work; the divisor is
  calibrated on this box's dual-channel DDR5 + 32 cores).
- **Feature-length fullvalidate ground-truth pass** on the kept baseline Sully workdir
  (`~/testing/fulllen-attr/.reel-Sully_t00-1ce039b19801`) to confirm the 30%-overshoot
  finding of the *old* 9.25-9.52 band against true full-chunk CVVDP for an apples-to-apples
  worst-case comparison. ~1h GPU. (The new 9.15-9.55 band already has its ground-truth pass --
  see 2026-06-14 "Target band WIDTH".)

### Open -- methodology / tooling

- **Build a high-variance test clip** (concatenate several dissimilar scenes from one film)
  so probes/chunk approaches the feature-length regime (~3) without a 4-hour encode. Validate
  every search-layer change against it; single homogeneous 5-20m cuts converge in ~1.7 probes
  and hide the tail entirely (2026-06-14 feature run).

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
