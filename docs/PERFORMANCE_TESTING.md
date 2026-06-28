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

## Where information belongs

| Put it in | When |
|----------|------|
| Code comments | The why is tied to a constant, invariant, safety condition, or non-obvious control flow. Keep it local and short enough that a maintainer sees it before changing code. |
| This file | Current guidance: defaults, bottlenecks, open work, test procedure, corpus, and the short "do not revisit" list. |
| `PERFORMANCE_TESTING_LOG.md` | Compact dated evidence for decisions: question, method/artifact path, decisive numbers, and outcome. Do not paste full run logs. |
| Both code comments and docs | A current default or safety rule has a non-obvious tradeoff. Code gets the durable why; docs get measured evidence and when to retest. |
| Neither doc | Raw verbose logs, full per-chunk tables, one-off scripts that are no longer needed, and superseded speculation. Leave those in local artifacts/git history. |

## How to record a new test

Prefer unit tests, replay, simulation, `scripts/tqreplay.py`, `scripts/chunkbench`,
and `scripts/perf/analyze.py` before a real encode. If running Reel with a
timeout, use at least 120 seconds.

For every real encode, replay, or benchmark that affects a tuning decision:

1. Add one compact dated entry to `docs/PERFORMANCE_TESTING_LOG.md`.
2. Record the machine/build, input clip, command or harness, variants, artifact
   directory, decisive metrics, and decision (`kept`, `rejected`, `retest`, or
   `investigate`).
3. If a default or active strategy changes, update the tables in this file and
   the relevant code comment.
4. If quality/accuracy is part of the decision, keep the workdir and run
   `scripts/fullvalidate`. `target-quality.json` is the search's sampled belief,
   not ground truth.

Useful artifacts:

- `.reel-*/perf.json`: phase timing and adaptive-worker history (`--keep-workdir`).
- `.reel-*/target-quality.json` and `.reel-*/tq/*.json`: aggregate/per-chunk TQ logs.
- `.reel-*/chunk-plan.json` / `.txt`: chunk boundaries.
- `scripts/perf/run-suite.sh`: standard sequential suite runner.
- `scripts/perf/analyze.py`: run summary from `perf.json` + TQ logs + GPU trace.
- `scripts/perf/compare-runs.py`: run-level A/B comparison.
- `scripts/fullvalidate`: full-chunk CVVDP truth for a kept workdir.
- `scripts/handlertest`: libvship concurrent-handler safety check.

## Current defaults and settled decisions

| Area | Current value | Why | Provenance |
|------|---------------|-----|------------|
| Target-quality band | `9.15-9.55` JOD, accepted literally | Half-width 0.20 is wide enough to avoid fighting ~0.075 JOD probe noise; feature-length Sully dropped from 3.11 to 1.46 probes/chunk and 4h02m to 1h57m without streaming-visible quality loss. | 2026-06-14 target-band entry; 2026-06-18 rebaseline |
| Search | Adaptive priors plus bracket-aware search after first two probes | Fewer probes with no measured accuracy loss; content-prior and early-out shortcuts had no free lunch. | 2026-06-07, 2026-06-12, 2026-06-14 search entries |
| TQ scheduling | Timeline blocks of 32, largest-first within block | Keeps neighboring CRF priors useful while reducing the large-chunk tail; block size 8 raised probes. | 2026-06-07 entries |
| TQ probes | 3 x 48-frame windows; 5 windows after high spread; full-chunk at <=256 frames; full-first reuse at <=720 median-seeded frames | Good current speed/accuracy tradeoff. Lowering the 256-frame full-probe threshold was confounded by the old scoring bug but still points the wrong way; retest before changing. | 2026-06-13 knobs; 2026-06-18 rebaseline |
| Safety floor | Worst sampled window must not fall below the target floor | Protects chunks whose mean is fine but a sampled sub-segment is weak. | 2026-06-14 monotonicity diagnostic |
| Metric workers | 6 below UHD, 4 for UHD | One GPU saturates near 4 CVVDP workers; 6 below UHD keeps margin with less VRAM, while 4K is encoder-bound so extra metric workers barely affect wall. | 2026-06-19 post-restore sweep |
| VSHIP/libvship | One handler per metric worker; libvship must be built with `MITIGATE_MALLOC_ASYNC` | Default `cudaMallocAsync` allocator corrupts concurrent CVVDP scores across handlers. | 2026-06-19 restore; `docs/VSHIP_CONCURRENCY_BUG.md` |
| Shot-detection workers | `cores/2` (8 on the 16-core dev box) | With `decoderThreads()/workers` floored at 2 threads/worker, `cores/2` keeps total decoder threads at the core count while maximizing segment parallelism; boundary output is worker-count invariant. A/B had it beat `cores/4` by 2-21% with identical hashes. | 2026-06-27 shot-detect cap entry |
| 4K encode concurrency | Ceiling `maxWorkers/6` (min 3), start at ceiling | Shipping target-quality mode is flat/slower above cap5 on the dev box; fixed-CRF cap8 is faster but does not convert to TQ wall. | 2026-06-12 ramp; 2026-06-21 cap/lp retest |
| Non-4K encode concurrency | Ramps to full `maxWorkers` | Lower resolutions usually self-limit on GPU CVVDP throughput. | 2026-06-12 ramp; 2026-06-19 attribution |
| SVT-AV1 preset | 6 | Joint wall/size knee for tested 1080p and 4K. Faster costs too many bits; slower costs too much wall. | 2026-06-20 preset entries |
| `level_of_parallelism` | Auto from resolution ramp ceiling (`lp3` for 4K cap5 on the dev box, `lp2` for non-4K) | Bitstream-identical across values; low 4K caps benefit from more encoder-internal parallelism. | 2026-06-13 lp; 2026-06-21 cap/lp retest |
| Pipeline overlap | Release encode slots during scoring, async score probe windows, overlap decode/GPU work, parallel crop/shot analysis | Reduces idle CPU/GPU time without changing quality; guarded by prior-paced flight cap to avoid cold-start probe inflation. | 2026-06-12 concurrency restructure |
| Probe IVF fsync | Skip only for ephemeral sampled-probe IVFs | Byte-identical and safe; durable final chunks/full-chunk probes still fsync for resume safety. | 2026-06-21 fsync entry |

## Current bottlenecks and tradeoffs

| Area | Current finding | Guidance |
|------|-----------------|----------|
| 1080p light/low-bitrate | Often GPU CVVDP-bound; wall flat across extra metric workers. | More metric workers or slower presets rarely help. |
| 1080p heavy/grainy/high-bitrate | Can become encoder-bound; preset-4 wall penalty grew with output size. | Do not assume 1080p encoder changes are free. Preset 6 stands. |
| 4K | Encoder/memory-bandwidth-bound; GPU often idle. | Encoder-side changes matter, but raising the TQ cap above `maxWorkers/6` was not a win on the dev box. |
| Probe count | Every probe costs encode + CVVDP. | Reducing probes helps both lanes, but rejected shortcuts increased file size or relied on bad predictors. |
| Band width | Tighter than about 2x probe noise wastes probes. | Do not tighten the default band without user-approved quality/size/speed tradeoff plus real encode + `fullvalidate`. |
| Accuracy changes | Sampled scores are not truth. | Window count/size, full-probe threshold, chunk cap, display model, and boundary changes require `scripts/fullvalidate`. |
| Feature-length extrapolation | Homogeneous 5m cuts can hide probe tails and stage balance. | Use the standard suite for quick signal, but validate major TQ changes on diverse/high-variance content or a feature. |

## Active open items

| Priority | Item | Next action / reason |
|----------|------|----------------------|
| Low | Direct mux / avoid merged IVF | Defer until `perf.json` shows merge+mux I/O is material. Encode and CVVDP dominate today. |
| Low | Floor-guard + seed amplifier hardening | Replay existing TQ logs before real encodes. Prefer quarantining CRF-min/CRF-max, `max_probes`, or `bounds_crossed` chunks from neighbor seeding over softening quality decisions. |
| Low | Probe-sample noise vs sample-frame count | Structurally gated because current-band clips converge well. Revisit only if multiple current-band clips show recurring probe tails or sampled-vs-full gaps; use `sullyhv-15m-4k-hdr` plus an easy control and `fullvalidate`. |
| Low | Provisional priors from in-progress chunks | Nontrivial scheduling/search change. Consider only if current-band suite runs show recurring probe-count tails across multiple clips. |
| Low | Rare under-represented segments | Known real miss is Sully feature chunk 0664, a max-CRF sampled chunk whose windows missed a hard sub-segment. Do not tune from one sighting; if repeats appear, test one targeted hard/bright/texture window for sampled near-CRF-max chunks. |

## Do not revisit without new evidence

| Topic | Current decision |
|-------|------------------|
| Preset split by resolution | Rejected. Preset 6 is the best joint default for 1080p and 4K. |
| More metric workers | Rejected for current hardware. Extra workers add VRAM and little/no wall benefit. |
| Tight target band | Rejected as a default. It costs probes for consistency finer than the metric/use case needs. |
| Search shortcuts | Content-prior seed, flat-low early stop, and worst-window/straddle early-out were rejected. The early-out is a speed-vs-size knob, not an accuracy-neutral win. |
| Raising 4K TQ cap above `maxWorkers/6` on the dev box | Fixed-CRF throughput improved, but target-quality wall was flat at cap6 and slower at cap8/cap10. |
| More complex chunk boundaries | Avoid unless fullvalidate shows a repeatable failure that sampling/search cannot solve. |
| HDR display peak >1000 nits | Rejected for now. The scary 1500-nit result was scoring-cascade contamination, and the clean re-score showed no need to change. |
| Lowering the 256-frame full-probe threshold | Do not change without a fixed-binary fullvalidate A/B. The old magnitude was confounded, but direction was bad and current config is clean. |
| Overlapping pre-encode head with encoding | Deferred. Requires accuracy-affecting streaming planner changes and only saves the shot-detection head. |
| Shot-detection worker cap of 6 | Rejected. `decoderThreads()/workers` floors at 2 threads/worker, so 6 workers get only 12 total decoder threads vs cap4's 16; 6 is slower than or barely better than 4 on every tested input. `cores/2` is the measured optimum. |
| Consolidating validation / stream-byte-scan probes | Sized 2026-06-28 and left unchanged. Total probe overhead is 0.05-0.42% of wall; only the duplicate `video.Probe` (first-frame decode, ~0.7s on 4K) was material and is now shared once between HDR analysis and the chunked pipeline. Validation re-opens the output but totals ~3ms (container probes are `probesize`-bounded), and `GetVideoStreamBytes` is O(file size) but not duplicated (input vs output). Revisit only if `perf.json` from large/network media shows these phases growing. |

## Standard corpus and local artifact boundary

The repo holds recipes and tools; `$REEL_TESTING_DIR` holds clip bytes and run
outputs.

| In repo | Under `$REEL_TESTING_DIR` |
|---------|---------------------------|
| `scripts/perf/clips.tsv` clip manifest | Clip `*.mkv` files and personal source rips |
| `scripts/perf/` harness and analyzers | Per-run outputs, kept workdirs, verbose logs, GPU traces |
| Distilled conclusions in these docs | Large dated A/B directories and raw artifacts |

The standard suite matrix is below. All clips except `sullyhv-15m-4k-hdr` are
single contiguous cuts described by `scripts/perf/clips.tsv`; `sullyhv` is a
local derived stress asset assembled from multiple hard Sully regions and is
tracked by name rather than by a single manifest row.

| Clip | Type | Use |
|------|------|-----|
| `air-5m-1080p-sdr` | clean-light 1080p SDR | light/GPU-bound control |
| `im-5m-1080p-sdr` | moderate 1080p SDR | normal 1080p control |
| `bts-5m-1080p-sdr` | heavier 1080p SDR | high-bitrate/grainy 1080p stress |
| `sully-5m-4k-hdr` | clean 4K HDR | normal 4K/HDR control |
| `kbv1-5m-4k-hdr` | grainiest 4K HDR option | 4K grain stress |
| `sullyhv-15m-4k-hdr` | deterministic hard-content stress asset | near-floor / sampling stress control |

Other corpus abbreviations in `scripts/perf/clips.tsv`: `io` = clean CG 4K HDR,
`ko` = light-moderate 4K HDR, `soms` = light 1080p SDR. Cuts are taken from the
middle 60% of each film, and 5m/10m/20m cuts of the same film are distinct
segments rather than nested subsets.

Run the suite strictly sequentially. Do not run two Reel/CVVDP processes on the
single GPU at once.

## Standing guidance

- Keep bracket-aware search, conservative high-side jump gating, and block size 32 unless new evidence shows a regression.
- Coordinate target-band changes with the user; they are explicit accuracy/size/speed tradeoffs.
- Test the real artifact you will ship (actual binary/library/config), and replicate intermittent failures enough to bound the rate.
- After libvship, GPU, driver, or VSHIP changes, run `scripts/handlertest` before trusting concurrent CVVDP scores.
- Run `./check-ci.sh` before handing work back.
