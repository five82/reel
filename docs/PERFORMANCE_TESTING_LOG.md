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
