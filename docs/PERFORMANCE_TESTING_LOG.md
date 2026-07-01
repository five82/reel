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

Local durable artifacts under `$REEL_TESTING_DIR` (default `~/testing`):

| Path | Contents |
|------|----------|
| `band-investigation-20260630/` | Full-scan-vs-sampled matrix + chunk-max sweep (results.tsv, per-run logs). |

On 2026-06-30, after the full-scan change, the older raw artifacts (`perf-ab/*`,
`rebaseline-20260617/`, `vship-concurrency/`, `fulllen-attr/`, and the
full-scan/sampled A/B dirs) were pruned from `$REEL_TESTING_DIR` so future perf
testing cannot resume-contaminate on a stale `.reel-*` workdir or compare against
old-code, sampled-mode, or old-schema (`final_sample_score`/`windows`) data. Their
decisive numbers remain in the entries above; the libvship concurrency diagnosis is
in `docs/VSHIP_CONCURRENCY_BUG.md`, and preset/concurrency/metric-worker defaults
are recorded in code comments and `docs/PERFORMANCE_TESTING.md`.
