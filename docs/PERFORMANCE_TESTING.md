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

## Chunking

`DefaultTargetQualityMaxChunkDuration = 12s` caps chunk size in TQ mode. It is a
**weak lever**: an 8-24s sweep (2026-06-30, full-scan, 1080p + 4K) found wall
flat across the range -- metric work ~= total_frames x probes-per-chunk, which is
independent of chunk size -- with only small content-dependent size/accuracy
wobble and no consistent optimum. 12s is a balanced default, not a tuned peak;
larger ("much larger") gives no throughput win and occasionally worse accuracy.

## Current tuning baseline

Use `scripts/perf/run-suite.sh --label tq-baseline` for a clean current-code
baseline; artifacts live under `$REEL_TESTING_DIR/perf-runs/`. The final
2026-07-01 baseline on this box (RTX 5060 Ti) is
`perf-runs/20260701-001943-tq-baseline-final`: default matrix wall 2717s,
output 1064 MB, with `sullyhv-15m` at 1.52 probes/chunk and no chunks maxing the
6-probe budget. That means the old feature-length probe tail is not currently an
active bottleneck on the stress clip.

Metric workers default to **4 at every resolution**. A 2026-06-30 HD A/B found
4 workers slightly faster than the old 6-worker below-UHD default (683s vs 691s,
-1.2% pooled) while cutting VRAM by roughly 1-2 GiB; 8 workers regressed (709s,
+2.6%) and used much more VRAM. UHD remains at 4 because 4K runs are mostly
encoder/memory-bandwidth bound, so extra metric concurrency is not the wall-time
lever.

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

- Clean digital 1080p (e.g. ARRI-sourced, grain-free) gains little from
  full-scan (~+2% size) while paying the full wall cost. A variance-triggered
  hybrid was considered and rejected as not worth the complexity; revisit only if
  a 1080p-heavy clean-content batch makes the wall cost bite.
- If a future full-feature encode shows the old probe-tail behavior again, build
  a new stress clip or revalidate `sullyhv`; under current full-scan/wide-band
  search it is **not** in the old feature regime (1.52 probes/chunk, 0% maxed).
