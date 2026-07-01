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

## Open items

- Clean digital 1080p (e.g. ARRI-sourced, grain-free) gains little from
  full-scan (~+2% size) while paying the full wall cost. A variance-triggered
  hybrid was considered and rejected as not worth the complexity; revisit only if
  a 1080p-heavy clean-content batch makes the wall cost bite.
