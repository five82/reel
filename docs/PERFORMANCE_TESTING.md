# Performance Testing Notes

Current performance guidance for Reel. Read this first; use
`docs/PERFORMANCE_TESTING_LOG.md` only for provenance and raw experiment detail.

## Why this exists

Performance work has a long feedback loop: real encodes are slow, many plausible
changes help only one clip, and git history does not explain why changes were
kept or reverted. This file is the compact current-state brief for future
maintainers and coding agents.

## How to record a test

For each real encode or replay that informs a tuning decision, add a dated
`## YYYY-MM-DD <title>` entry to `docs/PERFORMANCE_TESTING_LOG.md`, and record:

- Date, machine, input clip, duration, resolution, HDR/SDR.
- Command or test script used.
- Git commit or working-tree change being tested.
- Total wall time and video encoding time.
- TQ summary: chunks, total probes, probes/chunk, probe histogram, stop reasons.
- Accuracy summary: sampled JOD min/mean/max, mean absolute error, outside-tolerance count if available.
- Tail behavior: max-probe chunks, chunks with >=3 probes, p90/max window spread.
- Conclusion: keep, revert, retest, or investigate.

If the test changes a default, update the matching row below.

Useful artifacts:

- Human log: `*-reellog*`
- Phase + worker timing: `.reel-*/perf.json` (per-phase wall time and adaptive
  worker history; written only with `--keep-workdir`)
- Aggregate TQ log: `.reel-*/target-quality.json`
- Per-chunk TQ logs: `.reel-*/tq/*.json`
- Chunk plan: `.reel-*/chunk-plan.txt` and `.reel-*/chunk-plan.json`

## Current defaults (settled)

| Knob | Current value | Current reason | Provenance in LOG |
|------|---------------|----------------|-------------------|
| 4K encode concurrency | ceiling `maxWorkers/6` (min 3), start at ceiling | 4K SVT-AV1 is memory-bandwidth/encoder bound; fixed-CRF throughput improves above 5 workers, but full target-quality wall is flat/slower once probe/scoring duty-cycle and per-encode slowdown are included | 2026-06-12 "4K adaptive ramp"; 2026-06-21 cap/lp retest |
| Non-4K encode concurrency | ramps to full `maxWorkers` | Usually metric/GPU-bound on light content; self-limits by utilization | 2026-06-12 "4K adaptive ramp" |
| Metric workers | 6 below UHD, 4 for UHD | Single GPU saturates near 4 workers; 6 below UHD keeps margin with less VRAM; UHD stays 4 because 4K is encoder-bound | 2026-06-19 "Post-restore re-attribution" |
| VSHIP/libvship requirement | one handler per metric worker; libvship must be built with `MITIGATE_MALLOC_ASYNC` | Default async allocator corrupts concurrent CVVDP scores; mitigated build restores safe concurrency | 2026-06-19 "Metric concurrency RESTORED"; `docs/VSHIP_CONCURRENCY_BUG.md` |
| `level_of_parallelism` | auto from resolution ramp ceiling: 4K lp 3, non-4K lp 2 (`--level-of-parallelism` overrides) | Bitstream-neutral; fixed-CRF retests still favor lp3 at 4K cap5/cap6, and target-quality did not justify raising the cap | 2026-06-13 "SVT-AV1 level_of_parallelism"; 2026-06-21 cap/lp retest |
| SVT-AV1 preset | 6 | Joint efficiency knee: faster costs too many bits, slower costs too much wall; no resolution-aware split | 2026-06-20 preset entries |
| TQ scheduling block | 32 chunks, largest-first inside block | Keeps priors useful while reducing long-tail chunks | 2026-06-07 scheduling entries |
| TQ probe windows | 3x48 sampled; 5 windows after high spread; whole-chunk at/below 256 frames | Good speed/accuracy tradeoff; lower full-probe threshold was rejected directionally and needs fixed-binary evidence before revisiting | "What has worked"; 2026-06-13 "Accuracy-trading TQ knobs" caveat |
| JOD target range | 9.15-9.55 (center 9.35, half-width 0.20), accepted literally | Band width is a speed lever; half-width below ~0.15 fights probe noise and wastes probes | 2026-06-14 "Target band WIDTH" |
| CRF search | adaptive priors + bracket-aware search after first two probes | Fewer probes without measured accuracy loss | "What has worked" |
| Pipeline concurrency | slot release during scoring, async scoring, decode/GPU overlap, parallel analysis | Overlaps CPU encode and GPU metric work | 2026-06-12 "concurrency restructure" |

## Current target-quality strategy

| Component | Current behavior |
|-----------|------------------|
| Target band | Use configured range literally; default is `9.15-9.55`. Do not tighten without a real encode plus `scripts/fullvalidate`. |
| Scheduling | Timeline blocks of 32, largest-first within each block. |
| Probing | 3x48 sampled windows normally; 5 windows after high spread; whole-chunk probes at <=256 frames. |
| Search | Adaptive CRF priors from completed chunks; bracket-aware interpolation after two probes; conservative high-side jump only when the highest-CRF probe remains >=0.30 JOD above target. |
| Scoring | Mean/worst blend when sampled-window spread is high. |
| Safety floor | Worst-window floor prevents convergence when any sampled window falls below tolerance. |
| Metric execution | One VSHIP CVVDP handler per metric worker, concurrently, requiring `MITIGATE_MALLOC_ASYNC` libvship. Re-run `scripts/handlertest` after libvship/GPU/driver changes. |

## Current bottlenecks and tradeoffs

| Area | Current finding | Practical guidance | Provenance in LOG |
|------|-----------------|--------------------|-------------------|
| 1080p, light/low-bitrate | GPU CVVDP-throughput bound; wall was flat from metric workers 4->12 while VRAM climbed | Extra metric workers do not improve wall; keep default 6 for margin | 2026-06-19 post-restore sweep |
| 1080p, heavy/grainy/high-bitrate | Can become encoder-bound; preset-4 wall penalty tracked output size (+1% air -> +38% bts) | Do not assume encoder changes are free at 1080p; preset 6 still wins overall | 2026-06-20 1080p grain-tier sweep |
| 4K | Encoder/memory-bandwidth bound; GPU often sits mostly idle; metric workers barely affect wall | Encoder-side defaults have been rechecked: preset 6 stands, and raising encode concurrency above `maxWorkers/6` did not improve target-quality wall | 2026-06-19 post-restore sweep; 2026-06-20 preset sweep; 2026-06-21 cap/lp retest |
| Probe count | Every probe costs both an encode and CVVDP scoring | Reducing probes is the only lever that helps both lanes, but search-only shortcuts had no free lunch | 2026-06-14 search diagnostics and target-band entry |
| Band width | Probe noise is ~0.075 JOD; too-tight bands force extra probes | Keep default half-width 0.20 unless a user-approved quality/size/speed trade is validated | 2026-06-14 target-band entry |
| Accuracy changes | Sampled scores are the search's belief, not ground truth | Use `scripts/fullvalidate` for any window count/size, probe threshold, chunk cap, display model, or boundary change | repeated fullvalidate findings |

## Active open items

The top open work is measurement infrastructure, not another broad default
sweep. Current defaults are well-supported; future changes should make it easier
to attribute performance on the real Reel pipeline and only then test narrow
hypotheses.

| Priority | Item | Why / next action |
|----------|------|-------------------|
| High | Reusable perf suite rooted in `~/testing` | Existing artifacts under `~/testing/` are rich but bespoke. Add a stable harness (for example `scripts/perf/run-suite.sh`, `scripts/perf/analyze-tq.py`, `scripts/perf/compare-runs.py`) that records git commit/binary, SVT-AV1 version, libvship/MITIGATE status, GPU/driver, wall, size, TQ probe histogram, stop reasons, encode-vs-metric seconds, p90/max window spread, GPU util, and VRAM. It can now read `.reel-*/perf.json` for per-phase wall time and worker history instead of parsing verbose logs. Start with the standard matrix: `air-5m`, `im-5m`, `bts-5m`, `sully-5m`, `kbv1-5m`, and `sullyhv-15m`. Reason: high priority because it prevents re-learning old lessons and gives future A/Bs comparable evidence. |
| Medium | A/B disabling per-probe IVF `fsync` | `internal/encoder/encoder.go` calls `out.Sync()` for every IVF, including temporary probe-window files. This may be pure I/O overhead, especially outside fast local NVMe. Test a variant that skips sync for probe IVFs first; if useful, consider whether final chunk IVFs can safely skip sync without weakening resume semantics too much. Run interleaved TQ A/Bs on `sully-5m`, `kbv1-5m`, `im-5m`, and `sullyhv-15m`; compare wall, summed encode seconds, probe counts, output size, and bitstream/size identity where applicable. Fullvalidate is not required unless the encoded bits change. |
| Medium | Shot-detection worker cap A/B | Shot detection is linear and feature-length 4K costs about 12-15 minutes, but `chunkplan.shotDetectWorkers` is capped at 4 workers. Use `scripts/chunkbench` to test cap 4/current vs 6 vs 8 on 20-minute 4K clips and a feature if available. Boundary hash must remain identical; if it does, this is a safe isolated throughput win. Reason: medium priority because the prize is modest but the experiment is cheap and does not need a full encode. |
| Medium | Reduce duplicate media probes and post-encode scans | The orchestration path re-opens/probes the same files several times (`GetVideoProperties`, `GetHDRInfo`, audio stream probes, `video.Probe`, validation probes), and `GetVideoStreamBytes` scans packet payloads for both input and output after encode. First use `perf.json` to size this overhead, especially on large/network media and batches. Then consolidate initial video/HDR/audio analysis into one per-file result, reuse output probe results during validation, and consider making exact stream-byte scans optional or metadata-backed when reliable. |
| Low | Direct mux / avoid merged IVF if I/O shows up | Reel currently writes chunk IVFs, merges them to `video.ivf`, then muxes. Directly muxing chunk IVFs could avoid one full video read/write, but encode/CVVDP dominate today. Keep deferred until structured timing shows merge+mux are material on the target storage. |
| Low | Floor-guard + seed amplifier hardening | The scoring cascade is fixed, but two latent amplifiers remain: a hard 9.15 floor with no noise margin, and extreme/floored/bounds/max-probe chunks seeding neighbors. Prefer prior hardening before softening quality decisions: quarantine CRF-min/CRF-max, `max_probes`, or `bounds_crossed` chunks from neighbor seeding, or clamp/fall back to median. Replay existing `target-quality.json` logs before running real encodes. |
| Low | Probe-sample noise vs sample-frame count | Structurally gated because most chunks are full-probed and the current band converges well. Revisit only if multiple current-band clips show recurring probe tails or sampled-vs-full gaps. Use deterministic `sullyhv-15m-4k-hdr` plus an easy control, and run `scripts/fullvalidate` for any kept sampling change. |
| Low | Provisional priors from in-progress chunks | Nontrivial scheduling/search change. Consider only if the perf suite shows recurring probe-count tails at the current band across multiple clips; otherwise the existing completed-chunk prior plus 32-block schedule is good enough. |
| Low | Monitor high-spread and rare under-represented segments | Merge of the old "watch high-spread chunks" and "smarter sampling" items. Known clean evidence is limited: `knives5` chunk `0001` had high spread, and Sully feature chunk `0664` was one real max-CRF sampled miss (true 8.689 vs sampled 9.679). Do not tune from one-off sightings. If repeats appear, test a narrow rule: only for sampled chunks near CRF max whose sampled windows are all high/low-spread, add one targeted hard/bright/texture window. The old temporal shot-activity score is not a reliable global CRF predictor, so any targeting should use a better cheap spatial/luma/texture signal. |

Completed and removed from the active list: the structured phase and worker
timing artifact shipped as `.reel-*/perf.json` (see the 2026-06-21 `perf.json`
log entry); it records per-phase wall time and adaptive-worker history when the
work directory is kept.

Also removed from the active list: historical-only fullvalidate of the old
`9.25-9.52` band, the fixed-MITIGATE 4K encode-concurrency/lp retest, and the
non-dev hardware 4K cap retest. The shipped `9.15-9.55` band already has feature-length
ground truth, the cap/lp retest is resolved on this dev box, and there is no
near-term intent to encode on other hardware. The old high-spread and
smarter-sampling monitor items are now merged into one narrower low-priority
sampling-watch item.

## Do not revisit without new evidence

| Topic | Current decision |
|-------|------------------|
| Preset split by resolution | Rejected; preset 6 is optimal enough for both 1080p and 4K. |
| More metric workers | Rejected for current hardware; extra workers add VRAM and little/no wall benefit. |
| Tight target band | Rejected as a default; it buys unmeasurable consistency and costs probes. |
| Search shortcuts | Content-prior seed, flat-low early stop, and worst-window/straddle early-out were rejected; the last is a speed-vs-size knob, not an accuracy-neutral win. |
| Raising the 4K encode cap above `maxWorkers/6` on the dev box | Fixed-CRF encoder throughput improved, but target-quality wall was flat at cap6 and slower at cap8/cap10; keep the current cap unless new hardware or feature-scale evidence changes the tradeoff. |
| More complex chunk boundaries | Avoid unless fullvalidate shows a repeatable failure that sampling/search cannot solve. |
| HDR display peak >1000 | Rejected for now; the scary 1500-nit failure was cascade-contaminated, and the clean re-score showed no need to change. |
| Lowering the 256-frame full-probe threshold | Do not change without a fixed-binary fullvalidate A/B; the previous magnitude was cascade-confounded, but the direction was bad and the current shipped config is clean. |

## Standing guidance

- Keep bracket-aware search, conservative high-side jump gating, and block size 32 unless another clip shows a clear regression.
- Changing the target band is an accuracy/size/speed tradeoff and should be coordinated with the user.
- Prefer unit tests and replay/simulation before real encodes; when a real encode informs a default, record it in the log.
- Run `scripts/fullvalidate` on kept workdirs for any accuracy-trading change.
