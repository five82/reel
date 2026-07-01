# Performance Testing Log

Compact decision ledger for Reel performance work. This file records enough
provenance to prevent expensive retests; it is intentionally not a raw lab
notebook. For current guidance read `docs/PERFORMANCE_TESTING.md` first.

For a new entry, keep the format short:

- **Question:** what uncertainty was tested?
- **Method/artifacts:** command or harness, machine/build when relevant, and the
  durable artifact directory under `$REEL_TESTING_DIR`.
- **Decisive result:** only the numbers needed to justify the decision.
- **Decision:** kept, rejected, deferred, contaminated, or methodology only.

## Entries

### 2026-06-30 -- Full-scan everywhere below UHD; sampled windows deleted

- **Question:** Are the 256-frame full-probe threshold and 288-frame (12s) chunk
  cap well-grounded? Specifically: is the 257-288 sampled band worth it for
  1080p, and should the chunk max change?
- **Method/artifacts:** Built const-patched binaries (full-probe threshold 256 ->
  100000 to force whole-chunk; max-chunk-duration 8/12/16/24s) and ran a 22-encode
  matrix on this box (RTX 5060 Ti). Batch A: full-scan vs sampled on all four
  1080p clips (air/soms/bts/im) @12s with `scripts/fullvalidate` ground truth.
  Batch B: full-scan chunk-max sweep on 1080p (im/soms) and 4K (sully/kbv1).
  Per-run isolated output dirs (reel's workdir lives in the output dir -- a shared
  dir silently resumes the prior config; caught via a chunks=fullvalidate-N
  integrity check). Artifacts: `band-investigation-20260630/`.
- **Decisive result:** Sampling systematically over-encodes (worst-window pooling
  is pessimistic on the band). 1080p pooled full-scan vs sampled: size -8.3%
  (air +2% clean .. im +22% grain), mean_abs_error 0.082 vs 0.093, over-band
  chunks 0 vs 6, score-lie 0.000 vs ~0.02-0.03, wall +16.6% (1080p is
  metric-bound; 4K unaffected/faster, already full-scan since 06-29). Chunk-max
  sweep: wall flat across 8-24s for both resolutions (metric work ~=
  total_frames x probes/chunk, chunk-size-independent); size/accuracy effects
  small and content-dependent with no consistent optimum.
- **Decision:** Kept full-scan everywhere; deleted the sampled-window /
  extra-window / full-first / worst-window-pooling machinery and the
  full-probe-threshold (~620 lines). Chunk max stays 12s (weak lever). Refactored
  binary reproduced the const-patched full-scan result on im (size + accuracy +
  gap 0.000). Reproducibility note: fresh im runs matched the archived 06-29 A/B
  to the byte (full-scan 178s/90.7MB, sampled 150s/117MB).

## Artifact map

Most large artifacts are local under `$REEL_TESTING_DIR` (default `~/testing`):

| Path | Contents |
|------|----------|
| `perf-ab/post-restore/` | Post-restore metric-worker sweep and bottleneck attribution. |
| `perf-ab/preset-ab/` | Preset 4-8 sweep for 1080p/4K. |
| `perf-ab/preset-1080p-ab/` | 1080p preset 4-vs-6 grain-tier sweep. |
| `perf-ab/cap-lp-retest/` | 4K encode-concurrency ceiling and lp retest. |
| `perf-ab/skipsync/` | Probe-IVF fsync A/B. |
| `rebaseline-20260617/` | Fixed-ruler accuracy re-baseline and suspect re-scores. |
| `vship-concurrency/` | libvship allocator/concurrency diagnosis. |
| `fulllen-attr/` | Feature-length Sully attribution workdir/logs. |
| `perf-ab/lp-retest/` | Fixed-CRF level_of_parallelism retest. |
| `perf-ab/knobB/` | Old full-probe-threshold A/B; magnitude is cascade-confounded. |
| `band-investigation-20260630/` | Full-scan-vs-sampled matrix + chunk-max sweep (results.tsv, per-run logs, fullvalidate). |
