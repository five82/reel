# Performance Testing Log (historical record)

Chronological provenance for Reel performance work: what was tried, measured,
kept, rejected, or superseded. For current defaults, bottlenecks, and open work,
read `docs/PERFORMANCE_TESTING.md` first.

When you run a new test, add a dated `## YYYY-MM-DD <title>` entry here (follow
the checklist in `docs/PERFORMANCE_TESTING.md`). If a default changes, update
that doc's "Current defaults" table too.

## Status legend

| Status | Meaning |
|--------|---------|
| CURRENT | Authoritative for current defaults or active guidance. |
| KEPT | Change or finding is still valid, but mostly historical provenance. |
| REJECTED | Tested and intentionally not adopted. |
| DEFERRED | Valid idea, not worth implementing now. |
| SUPERSEDED | Later evidence replaced this entry's conclusion. Use the newer entry. |
| CONTAMINATED | Results were affected by a known defect; do not cite raw numbers. |
| PARTIAL | Some conclusion survives, but numeric magnitude or mechanism is unsafe. |
| DIRECTIONAL | Useful as supporting evidence, but not the current source of truth. |
| METHODOLOGY | Useful testing method or asset, not a current tuning conclusion. |
| ARCHIVED | Raw detail moved to `docs/PERFORMANCE_TESTING_ARCHIVE.md`. |

## Artifact map

Most large artifacts live under `~/testing/`:

| Path | Contents |
|------|----------|
| `~/testing/perf-ab/post-restore/` | Post-restore metric-worker sweep and bottleneck attribution. |
| `~/testing/perf-ab/preset-ab/` | Preset 4-8 sweep for 1080p/4K. |
| `~/testing/perf-ab/cap-lp-retest/` | 4K encode-concurrency ceiling and lp retest on the fixed MITIGATE build. |
| `~/testing/perf-ab/preset-1080p-ab/` | 1080p preset 4-vs-6 grain-tier sweep. |
| `~/testing/rebaseline-20260617/` | Fixed-ruler accuracy re-baseline and suspect re-scores. |
| `~/testing/vship-concurrency/` | libvship allocator/concurrency diagnosis. |
| `~/testing/fulllen-attr/` | Feature-length Sully attribution workdir. Target-band validation numbers are in the log; the older `~/testing/band-confirm/` path may not be present locally. |
| `~/testing/perf-ab/lp-retest/` | Fixed-CRF level_of_parallelism retest. |
| `~/testing/perf-ab/knobB/` | Confounded full-probe-threshold A/B raw artifacts. |

## Status-tagged index

| Status | Entry | Current use |
|--------|-------|-------------|
| CURRENT | 2026-06-21 4K encode-concurrency ceiling + lp retest | `maxWorkers/6` and 4K lp3 remain current on the dev box. |
| CURRENT | 2026-06-20 1080p preset 4 vs 6 across grain tiers | Preset 6 remains universal; 1080p bound-ness depends on bitrate. |
| CURRENT | 2026-06-20 Preset sweep 4/5/6/7/8 | Preset 6 is the size/wall knee for both resolutions. |
| CURRENT | 2026-06-19 Post-restore re-attribution + metric-worker sweep | Current bottleneck split and metric-worker defaults. |
| CURRENT | 2026-06-19 Metric concurrency RESTORED | Current VSHIP/libvship requirement and concurrency model. |
| CURRENT | 2026-06-18 Re-baseline accuracy ground truth on the fixed binary | Trustworthy current accuracy baseline. |
| CURRENT | 2026-06-14 Target band WIDTH is the real probe-tail lever | Current default band and band-width principle. |
| CURRENT | 2026-06-13 SVT-AV1 level_of_parallelism | lp auto behavior and bitstream-neutrality proof. |
| CURRENT | 2026-06-12 4K adaptive ramp: bandwidth, not capacity | 4K encode-concurrency cap rationale. |
| KEPT | 2026-06-12 concurrency restructure | Pipeline architecture provenance. |
| KEPT | 2026-06-12 TQ search simplification | Search simplification kept. |
| KEPT | 2026-06-07 bracket-aware/search scheduling entries | Bracket-aware search and block size 32 provenance. |
| REJECTED | What did not work or was reverted | Historical rejected approaches. |
| REJECTED | 2026-06-14 Content-prior first-probe seed | Activity signal sign flips across clips. |
| REJECTED | 2026-06-14 monotonicity_guard diagnostic | Flat-low gate rejected; diagnostic was for tight-band behavior. |
| REJECTED | 2026-06-14 Worst-window / straddle early-out | Quality-safe but too bitrate-expensive. |
| REJECTED/PARTIAL | 2026-06-14 HDR display peak 1000 vs 1500 | Keep 1000; scary 1500 result was cascade. Raw detail archived. |
| DEFERRED | 2026-06-14 Overlapping the pre-encode head | Valid but low ROI vs complexity. |
| METHODOLOGY | 2026-06-14 Full-length 4K encode attribution | Feature-scale method; tight-band tail is historical after band widening. |
| METHODOLOGY | 2026-06-15 High-variance test clip | Asset survives; old probe-tail result contaminated. Raw detail archived. |
| SUPERSEDED | 2026-06-18 Metric serialization bottleneck | Motivation for restored concurrency only. Raw detail archived. |
| SUPERSEDED | 2026-06-14 strategic analysis | Later entries resolved the recommendations. Raw detail archived. |
| SUPERSEDED | 2026-06-12 pipeline bottleneck attribution | Pre-restore attribution; use 2026-06-19 for current bottlenecks. Raw detail archived. |
| PARTIAL | 2026-06-13 Accuracy-trading TQ knobs | Current knobs stand; old magnitude confounded. Raw detail archived. |
| CONTAMINATED | 2026-06-13 4K metric workers 4 vs 6 | Do not cite raw numbers; use 2026-06-19. Raw detail archived. |
| SUPERSEDED | 2026-06-15 Cascade root cause + FIX | Temporary workaround superseded by MITIGATE allocator fix. Raw detail archived. |
| DIRECTIONAL | 2026-06-12 metric-worker scaling benchmark | Directional throughput curve; current defaults from 2026-06-19. |

## What has worked

### Sampled probes instead of full probes

Git reference: `e980b91 Use sampled probes for target quality`

Sampling made TQ practical by avoiding whole-chunk CVVDP probes for every search round. This is the foundation of Reel's speed/accuracy tradeoff. The known cost is that sparse windows can miss worst-case portions of long or gradually varying chunks.

### Adaptive CRF priors

Git reference: `a121653 Use adaptive CRF priors for target quality`

Using nearby completed chunks to seed CRF substantially reduces first-probe misses. Scheduling order matters because priors are only useful after representative neighboring chunks complete.

### Timeline-block scheduling with largest-first inside the block

Git reference: `33b6012 Schedule target-quality chunks in timeline blocks`

This balances two competing needs:

- keep chunks close enough to timeline order that neighboring priors are useful,
- process larger chunks early enough to reduce the final long tail.

The current block size is 32. A 2026-06-07 test reducing this to 8 made the early priors worse and increased total probes, so 32 was restored.

### Full-first probe reuse for reliable median-start chunks

Git references: `13c8ef4 Reuse reliable first target-quality probes`, `f8cdb6c Expand full-first target-quality probing`

When the first sampled probe would be unreliable and the chunk is small enough, encoding the full chunk first can let Reel reuse that probe as the final encode if it converges. This avoids duplicate work on successful first probes.

### Worst-window protections

Git references: `a3d481b Penalize weak target-quality sample windows`, `7859c21 Prefer higher worst-window score when tie-breaking converged probes`, `4bb7782 Weight TQ score toward worst window as spread increases`

These changes improved tail quality by making high-variance sampled chunks less mean-dominated. They intentionally spend some bits/time to avoid chunks whose mean looks fine but whose worst sampled window is weak.

### Conditional extra windows after high spread

Git reference: `8cd5a35 Use extra TQ windows after high spread probes`

Switching from 3 windows to 5 windows only after high spread directly targets uncertainty without paying the 5-window cost for every probe. Mixed window counts require care: monotonicity checks should not compare probes with different measurement modes.

### Bracket-aware unbracketed search

Git reference: `a56f241 Improve target-quality probe search`

The old search could waste rounds when all probes were on the same side of the target:

- Flat high-side chunks would creep toward CRF max over many rounds.
- All-low chunks could extrapolate too aggressively toward CRF min.

The bracket-aware change keeps interpolation for bracketed probes, uses midpoint for unbracketed low probes, and accelerates flat unbracketed high probes. On `knives-5min`, it reduced total probes and wall time after retesting with the restored 32-chunk schedule block. A follow-up change made the flat high-side jump more conservative by requiring the highest-CRF probe to remain at least 0.30 JOD above target before jumping 75% across the remaining range; this targets the 4-probe overshoot pattern seen in later tests.

## What did not work or was reverted

### Smaller TQ scheduling blocks: 32 -> 8

Test date: 2026-06-07

Input: `knives-5min.mkv`, 5:50, 3840x2160 HDR, crop 3840x2080.

Command: `./run-test-encode.sh knives5`

Compared to the prior 32-block run, block size 8 improved accuracy but hurt speed:

| Metric | 32-block baseline | 8-block test |
|---|---:|---:|
| Total probes | 66 | 74 |
| Probes/chunk | 1.89 | 2.11 |
| Probe histogram | `{1:17,2:11,3:4,4:1,5:1,6:1}` | `{1:11,2:12,3:9,4:3}` |
| Stop reasons | `{converged:34,max_probes:1}` | `{converged:35}` |
| Mean abs error | 0.0738 | 0.0593 |
| Video encode time | 22m19s | 25m12s |
| Total time | 23m51s | 26m43s |

Conclusion: restored block size 32. The smaller block started earlier timeline chunks before good priors existed, and bad priors propagated into nearby chunks. The improved accuracy did not justify the extra probes/time.

### Full-probe restart after high spread

Git references: `a7e4701 Restart TQ search with full probes after high spread`, reverted by `17007f7 Revert to mid-switch 5w TQ; skip monotonicity on mixed window counts`

Restarting with full probes after high spread was too expensive. Conditional extra sampled windows were kept instead.

### Scaling window count with chunk length

Git references: `c3951a5 Scale TQ window count with chunk length`, reverted by `a59a6cf Revert "Scale TQ window count with chunk length"`

Uniformly increasing probe density for longer chunks raised cost broadly. Prefer conditional extra sampling or targeted full probes for uncertain chunks instead of increasing density for everything.

### Faster first probe using higher preset on 4K HDR

Git references: `6791905 target-quality: use preset+2 for first probe on 4K HDR content`, reverted by `5258a44 Revert "target-quality: use preset+2 for first probe on 4K HDR content"`

Using a different preset for the first probe changed the measurement mode enough to hurt reliability/reuse. Keep probe encodes representative of final encodes unless evidence clearly supports otherwise.

### Half-resolution target-quality probing

Git reference: not found in reachable history; likely an uncommitted/local experiment.

Half-resolution probing was tried as a way to make TQ probes cheaper by encoding/scoring downscaled samples. It was backed out because the probe became less representative of the final full-resolution encode and full-resolution CVVDP behavior. The apparent speed gain was not worth the risk of learning CRFs from a different resolution/metric regime. Keep TQ probes at final output resolution unless a future experiment records strong per-clip and cross-clip evidence.

### More complex chunk-boundary logic

Git references: `d3e7db4 Simplify chunking: remove transitions, weak merges, and score-based splitting`, `f6003cb Restore weak-cut merging in simplified chunking`, `adf4511 Improve target-quality chunk planning`

Experiments with transition detectors, score-based splitting, and more elaborate boundary refinement increased complexity without enough quality gain. Current direction is simple luma-based shot detection plus max-duration splits, with only low-complexity packing/merging where it clearly improves TQ chunk shape.

## 2026-06-07 knives-5min test notes

Environment: local test machine, input `~/testing/knives-5min.mkv`, 5:50, 3840x2160 HDR, crop 3840x2080. Target range 9.25-9.50 JOD.

### Baseline before bracket-aware search

Artifacts: `~/testing/knives5-reellog3`, `.reel-knives-5min-b0cf00328939/target-quality.json` before rerun.

Summary:

- 35 chunks, 66 probes, 1.89 probes/chunk.
- Probe histogram `{1:17,2:11,3:4,4:1,5:1,6:1}`.
- Stops `{converged:34,max_probes:1}`.
- Mean absolute sampled-JOD error 0.0738.
- p90/max window spread 0.2119/0.6929.
- Video encoding time 22m19s; total time 23m51s.

Problem chunks:

- `0002`: 6 probes, all high, flat CRF response, stopped at max probes.
- `0003`: 5 probes, all high until late convergence.
- `0015`: 4 probes, unbracketed low probes caused an over-aggressive CRF 4.5 probe.

### Bracket-aware search plus block size 8

Artifacts: `~/testing/knives5-reellog4`.

Summary:

- 35 chunks, 74 probes, 2.11 probes/chunk.
- Probe histogram `{1:11,2:12,3:9,4:3}`.
- Stops `{converged:35}`.
- Mean absolute sampled-JOD error 0.0593.
- p90/max window spread 0.2127/0.6498.
- Video encoding time 25m12s; total time 26m43s.

Conclusion:

- Search change helped the known tail chunks:
  - `0002`: 6 -> 4 probes.
  - `0003`: 5 -> 3 probes.
  - `0015`: 4 -> 3 probes and avoided CRF 4.5.
- Scheduling block size 8 hurt overall by worsening early priors and increasing total probes.
- Keep bracket-aware search; restore block size 32; retest.

### Bracket-aware search with block size restored to 32

Artifacts: `~/testing/knives5-reellog5`.

Summary:

- 35 chunks, 63 probes, 1.80 probes/chunk.
- Probe histogram `{1:17,2:12,3:3,4:2,5:1}`.
- Stops `{converged:35}`.
- Mean absolute sampled-JOD error 0.0729.
- p90/max window spread 0.2119/0.6929.
- Video encoding time 21m32s; total time 23m03s.

Compared to the original baseline, this kept the same broad accuracy while reducing probes and time:

| Metric | Original baseline | Bracket-aware, block 32 |
|---|---:|---:|
| Total probes | 66 | 63 |
| Probes/chunk | 1.89 | 1.80 |
| Probe histogram | `{1:17,2:11,3:4,4:1,5:1,6:1}` | `{1:17,2:12,3:3,4:2,5:1}` |
| Stop reasons | `{converged:34,max_probes:1}` | `{converged:35}` |
| Mean abs error | 0.0738 | 0.0729 |
| Video encode time | 22m19s | 21m32s |
| Total time | 23m51s | 23m03s |

Tail chunks:

- `0002`: 6 -> 5 probes and converged instead of max-probe stopping.
- `0003`: 5 -> 4 probes.
- `0015`: 4 -> 3 probes and avoided the bad CRF 4.5 jump.
- `0001`: 3 -> 4 probes, a regression caused by the new bounded/bracketed path on a high-spread chunk.

Conclusion: keep bracket-aware search and block size 32. It is a modest but real improvement on this HDR clip. More clips are needed before further tuning.

## 2026-06-07 additional bracket-aware tests

These were run after keeping bracket-aware search and restoring the TQ scheduling block size to 32.

### sully-5min

Environment: local test machine, input `~/testing/sully-5min.mkv`, 5:50, 3840x2160 HDR, crop 3840x1600. Target range 9.25-9.50 JOD.

Artifacts: `~/testing/sully5-reellog1`, `.reel-sully-5min-*/target-quality.json`.

Summary:

- 44 chunks, 86 probes, 1.95 probes/chunk.
- Probe histogram `{1:16,2:19,3:4,4:5}`.
- Stops `{converged:44}`.
- Mean absolute sampled-JOD error 0.0589.
- p90/max window spread 0.1625/0.5462.
- Video encoding time 17m44s; total time 19m40s.

Tail chunks:

- Five chunks needed 4 probes: `0000`, `0001`, `0019`, `0020`, `0028`.
- Several 4-probe chunks were unbracketed high-side searches that overshot low on the third probe, then converged on the fourth.
- Chunk `0019` was a high-spread/default-start chunk and was the slowest chunk.

Conclusion: acceptable. No max-probe chunks, good mean error, and 1.95 probes/chunk on a difficult 4K HDR clip. Watch the 4-probe high-side overshoot pattern, but do not tune from this single clip alone.

### soms

Environment: local test machine, input `~/testing/soms.mkv`, 29:59, 1920x1080 SDR, crop 1920x1046. Target range 9.25-9.50 JOD.

Artifacts: `~/testing/soms-reellog1`, `.reel-soms-*/target-quality.json`.

Summary:

- 209 chunks, 303 probes, 1.45 probes/chunk.
- Probe histogram `{1:140,2:52,3:9,4:8}`.
- Stops `{converged:209}`.
- Mean absolute sampled-JOD error 0.0571.
- p90/max window spread 0.1206/0.3237.
- Video encoding time 23m11s; total time 24m11s.

Tail chunks:

- Eight chunks needed 4 probes: `0000`, `0001`, `0140`, `0148`, `0149`, `0150`, `0151`, `0152`.
- The 4-probe cluster around `0148`-`0152` suggests a local content regime where priors were consistently too low-CRF/high-quality and the high-side search overshot before converging.
- Despite the cluster, aggregate performance was strong: 67% of chunks converged in one probe and all chunks converged.

Conclusion: bracket-aware search looks good on SDR. The remaining opportunity is not broad probe reduction; it is handling local clusters of flat/high-side CRF response without adding extra probes elsewhere.

## 2026-06-07 conservative high-side jump retest

Git reference: `cffa01b Tune unbracketed high-side target search`

This retest required the highest-CRF probe to remain at least 0.30 JOD above target before using the aggressive high-side jump. The goal was to reduce 4-probe high-side overshoots without losing the `knives5` bracket-aware gains.

Artifacts:

- `~/testing/knives5-reellog6`, `.reel-knives-5min-*/target-quality.json`
- `~/testing/sully5-reellog2`, `.reel-sully-5min-*/target-quality.json`
- `~/testing/soms-reellog2`, `.reel-soms-*/target-quality.json`

| Clip | Previous probes | Retest probes | Previous time | Retest time | Mean abs error | Probe histogram |
|---|---:|---:|---:|---:|---:|---|
| `knives5` | 63 | 63 | 23m03s | 23m10s | 0.0749 | `{1:17,2:12,3:3,4:2,5:1}` |
| `sully5` | 86 | 81 | 19m40s | 18m49s | 0.0641 | `{1:18,2:17,3:7,4:2}` |
| `soms` | 303 | 303 | 24m11s | 24m08s | 0.0568 | `{1:138,2:53,3:14,4:3,5:1}` |

Additional details:

- `knives5`: preserved the 63-probe result, all chunks converged, p90/max window spread 0.2463/0.6929. Runtime difference is small enough to treat as noise.
- `sully5`: improved from 86 to 81 probes and reduced 4-probe chunks from five to two (`0000`, `0019`). This is the clearest win for the conservative gate.
- `soms`: total probes stayed flat at 303 and time was effectively unchanged. The old 4-probe cluster around `0148`-`0152` shifted rather than disappeared: only `0149` stayed at 4 probes, but `0152` became a 5-probe chunk.

Conclusion: keep the conservative high-side jump gate. It improved `sully5`, preserved `knives5`, and did not materially regress `soms`. Further search tuning should require new evidence from additional clips, because current probe counts are already near the low end for sampled TQ.

## 2026-06-12 metric-worker scaling benchmark

> **Status (2026-06-19): valid again, with one caveat.** The 2026-06-16 serialization fix briefly made
> metric workers gate decode concurrency only; the 2026-06-19 restore (see "Metric concurrency RESTORED")
> put N concurrent handlers back, so metric workers parallelize GPU compute again and this scaling curve
> applies. Caveat: it was measured on the old async-allocator build, while the shipped build is now
> synchronous (`MITIGATE_MALLOC_ASYNC`), so treat the exact saturation points as directional. The defaults
> it set (8 below-4K / 4 for 4K) stand and were re-validated end-to-end by the restore.

Goal: evaluate whether the current hardcoded 4 CVVDP/VSHIP metric workers should remain fixed for both 1080p and 4K, vary by resolution, or scale like encoding workers. No Reel code changes were made for this test.

Environment: local test machine, AMD Ryzen 9 7950X (32 logical CPUs), NVIDIA GeForce RTX 5060 Ti, 16 GB VRAM, driver 610.43.02.

Artifacts:

- Temporary harness and report: `~/testing/metric-worker-bench/`
- Raw metric-only data: `results-1080p.csv`, `results-4k.csv`, `results-4k-sully-extra.csv`
- In-situ Reel smoke logs: `~/testing/metric-worker-bench/reel-smoke-logs/`

Method:

- A temporary Go harness imported Reel internals and repeatedly ran `quality.ComputeChunkCVVDP` with source and probe path set to the same clip. This isolates metric throughput while still exercising Reel's FFmpeg frame decode plus VSHIP/CVVDP path.
- Each task scored one 48-frame window, matching TQ sample windows.
- 1080p SDR: `air-5m-1080p-sdr.mkv`, `bts-5m-1080p-sdr.mkv`, 16 windows/pass, 2 passes.
- 4K HDR: `io-5m-4k-hdr.mkv`, `sully-5m-4k-hdr.mkv`, 12 windows/pass, 2 passes where safe.
- Also ran short full Reel target-quality smoke tests with `--max-probes 1 --preset 13 --disable-autocrop` to check whether metric-only improvements show up in the adaptive encode pipeline.

Metric-only 1080p results:

| Metric workers | Mean aggregate fps | Speedup vs 1 | Samples |
|---:|---:|---:|---:|
| 1 | 13.62 | 1.00x | 4 |
| 2 | 22.85 | 1.68x | 4 |
| 3 | 28.13 | 2.06x | 4 |
| 4 | 31.98 | 2.35x | 4 |
| 6 | 37.97 | 2.79x | 4 |
| 8 | 42.93 | 3.15x | 4 |
| 10 | 42.03 | 3.09x | 4 |
| 12 | 40.92 | 3.00x | 4 |
| 16 | 44.20 | 3.24x | 4 |

Metric-only 4K HDR results:

| Metric workers | Mean aggregate fps | Speedup vs 1 | Samples |
|---:|---:|---:|---:|
| 1 | 5.29 | 1.00x | 4 |
| 2 | 9.97 | 1.89x | 4 |
| 3 | 13.57 | 2.57x | 4 |
| 4 | 16.97 | 3.21x | 4 |
| 5 | 17.73 | 3.35x | 4 |
| 6 | 21.94 | 4.15x | 4 |
| 7 | 22.28 | 4.21x | 4 |
| 8 | 22.96 | 4.34x | 3 |

4K stress notes: 10 workers failed with VSHIP `OutOfVRAM`; 8 workers also failed once on `sully-5m-4k-hdr`. On this 16 GB GPU, 7-8 workers are marginal for 4K and 10 is not safe.

In-situ smoke results:

| Clip | Workers | Elapsed | Result |
|---|---:|---:|---|
| `air-2m-1080p-sdr` | 4 | 73s | success |
| `air-2m-1080p-sdr` | 8 | 66s | success, about 10% faster |
| `io-5m-4k-hdr` | 4 | 512s | success |
| `io-5m-4k-hdr` | 6 | 512s | success, no wall-time gain |

Interpretation:

- 1080p metric-only throughput improves through 8 workers. Past 8, returns are noisy and not worth the extra workers: 10 and 12 regressed, while 16 only beat 8 by about 3% despite doubling worker count.
- 4K metric-only throughput improves materially through about 6 workers, but higher counts have diminishing returns and VRAM risk.
- Full 4K Reel runs did not benefit from 6 workers because the adaptive encoder only sustained about 2-3 active 4K workers in the smoke test. Extra metric processors were not on the critical path.

Conclusion/recommendation:

- Do not scale metric workers like encoding workers. Encoding workers are CPU/memory/adaptive; metric workers are GPU/VRAM-bound VSHIP processors. Scaling metric workers toward logical CPU count can waste memory and can OOM on 4K.
- Use simple resolution-aware defaults/caps rather than CPU-like scaling:
  - Below 4K/UHD, including SD/DVD, 720p, 1080p, and 1440p: 8 metric workers. SD/DVD content was not directly tested, but should be safe because frames are cheaper than 1080p and there is no evidence supporting a higher untested default.
  - 4K/UHD: 4 metric workers as the safe practical default in the current pipeline. 6 is the metric-only saturation point but did not improve the full 4K smoke test and has less VRAM headroom.
- This recommendation was implemented after the benchmark: automatic metric workers now resolve to 8 below 4K and 4 for 4K/UHD. Explicit `--metric-workers` still overrides the automatic default.
- If future changes increase sustained 4K encode concurrency, retest full-pipeline 4K at 4 vs 6 workers before raising the 4K default.

## 2026-06-12 TQ search simplification

Goal: remove accumulated complexity from incremental TQ tuning without changing performance or accuracy. Three simplifications were made together:

1. **Upper tolerance grace folded into the range.** The search previously accepted scores up to `Target+Tolerance+0.02` via a separate `UpperToleranceGrace` field threaded from `targetQualityUpperGraceJOD`. The grace field and constant are gone; the configured range is now accepted literally and the default range widened from `9.25-9.50` to `9.25-9.52`. The effective acceptance band is unchanged; the search aim point (range midpoint) moves from 9.375 to 9.385. Note for custom `--target-quality` users: the upper bound is now exact, so very tight custom ranges may cost an extra probe where the old grace would have accepted a 0.01-0.02 overshoot.
2. **Interpolation reduced to bracketing-pair linear.** `InterpolateCRF` previously dispatched among lerp, Fritsch-Carlson, and PCHIP based on probe count and a `round` parameter. The dispatch was miswired: with 3 probes it always used lerp over the two *lowest-score* probes (extrapolating from the wrong segment when the target sat in the upper segment), with 4 probes Fritsch-Carlson silently ignored the 4th point, and PCHIP was reachable only when picking the 6th probe of a 6-probe chunk (~3% of chunks historically). Replaced ~120 lines with linear interpolation between the two adjacent-by-score probes that bracket the target. Replaying baseline logs showed the new method picks the same CRF (e.g. soms chunk 0034 4th probe: 31.75 both ways).
3. **Dead plumbing removed.** `TargetQualityConfig.SampleFrames`/`FullProbeFrames` (only ever set to the package constants), `targetQualityPrior.Add` (test-only wrapper), an unused `bestProbeMatching` return value, and the unused `config.ProgressLogIntervalPercent` constant. `scripts/tqreplay.py` was realigned with the Go scoring (its default formula had drifted to a stale `(mean+worst)/2`; it now defaults to the adaptive blend and reproduces Go selections 37/37 on a fresh log).

Validation: `soms-5m-1080p-sdr`, two runs per build to bound scheduling noise.

| Run | Probes | Probes/chunk | Mean abs error | Max sampled JOD | Stops | Total time |
|---|---:|---:|---:|---:|---|---:|
| old code, run 1 | 50 | 1.35 | 0.0671 | 9.5170 | converged:37 | 4m01s |
| old code, run 2 | 52 | 1.41 | 0.0704 | 9.5056 | converged:37 | 4m09s |
| simplified, run 1 | 53 | 1.43 | 0.0650 | 9.5197 | converged:37 | 4m14s |
| simplified, run 2 | 53 | 1.43 | 0.0653 | 9.5197 | converged:37 | 4m13s |

Baseline run 1 had 4 of 37 chunks converge in the old grace band (9.50, 9.52], confirming the headroom is load-bearing and must stay in the range rather than be dropped. Probe counts and timing are within the baseline's own run-to-run noise; accuracy improved slightly.

4K HDR spot-check on `sully-5m-4k-hdr` (one run per build):

| Run | Probes | Probes/chunk | Mean abs error | Min final JOD | Stops | Total time |
|---|---:|---:|---:|---:|---|---:|
| old code | 60 | 1.82 | 0.1020 | 8.5042 | converged:31, bounds_crossed:1, monotonicity:1 | 17m10s |
| simplified | 71 | 2.15 | 0.0949 | 9.1173 | converged:28, max_probes:3, monotonicity:2 | 19m48s |

This clip contains several chunks with cliff-like CRF response where adjacent CRFs swing ~0.5 JOD (e.g. chunk 0020 scores ~9.13 at CRF 34.75 but ~9.69 at CRF 34.25, full-chunk probes, so this is encoder behavior, not sampling noise). On those chunks both builds take different random walks driven by which neighbor prior they start from: the old run's chunk 0020 hit the low side of the cliff twice, tripped the monotonicity guard, and accepted a final score of 8.50 JOD; the simplified run spent 6 probes and finished it at 9.69. The +11 probes and +2m38s in the simplified run are concentrated in three cliff chunks (0002, 0020, 0029) and bought a much better tail (min 9.12 vs 8.50) and slightly better mean error. The search-bound, monotonicity, and worst-window-floor logic these chunks exercise is identical in both builds; no regression signal attributable to the simplification.

Separate observation from chunk 0029 (both runs): on metric-insensitive chunks the low-side midpoint march wastes probes (score moved 9.110 -> 9.147 while CRF marched 42 -> 9.75 across five probes). A flat-low-response early stop, mirroring the existing flat-high gate, is a candidate future improvement; not implemented here.

Artifacts: `~/testing/tq-simplify-ab/{baseline1,baseline2,simplified1,simplified2,sully-baseline1,sully-simplified1}/target-quality.json`.

Conclusion: keep. Net ~125 lines of search logic removed with equivalent measured behavior on SDR and no attributable regression on a difficult 4K HDR clip.

## 2026-06-12 pipeline bottleneck attribution

**Status: SUPERSEDED / ARCHIVED.** This pre-restore attribution explained why
the concurrency restructure was worth doing, but it is not the current
bottleneck model. Use the 2026-06-19 post-restore metric-worker sweep for
current 1080p/4K attribution.

Current use: historical context only. The durable conclusion is that measuring
per-probe encode/metric seconds from `target-quality.json` is the right way to
attribute wall time.

Raw historical detail was moved to
`docs/PERFORMANCE_TESTING_ARCHIVE.md#2026-06-12-pipeline-bottleneck-attribution`.

## 2026-06-12 concurrency restructure (slot release, async scoring, decode overlap, parallel analysis)

Changes, all accuracy-neutral by construction except the dispatch gate (which exists to *protect* accuracy):

1. **Encode slots released during metric scoring.** TQ chunk workers now release their adaptive-limiter slot while waiting on CVVDP and re-acquire it before the next encode. Sample windows of one probe are scored asynchronously so window N+1 encodes while window N scores; full-first probes score all windows concurrently. (`internal/encode/target_quality.go`)
2. **Dispatch flight gate.** With slots released during scoring, dispatch alone would flood chunk starts before any priors exist. First attempt without a gate (soms-5m): probes 53 -> 63, mean abs error 0.0650 -> 0.0872, sources went from 29 neighbor/8 default to 16 default/14 median/7 neighbor, and wall got *worse* (232 -> 262s) because cold-started chunks burned probes (2.25 probes/chunk vs 1.34 for neighbor-seeded; all four >=4-probe blowups were cold starts). Fix: in-flight chunks are capped at the encode-slot count until 4 chunks have seeded the prior, then at `target + metricWorkers`.
3. **CVVDP decode/compute overlap.** `ComputeChunkCVVDP` now decodes source+probe frames in a producer goroutine with two rotating buffer pairs while the GPU computes the previous frame. (`internal/quality/cvvdp.go`)
4. **4K initial workers 2 -> 3.** (`internal/encode/adaptive.go`)
5. **Parallel shot detection.** The luma pass is split into 4 contiguous segments decoded by parallel workers (each decodes one extra leading frame so boundary scores are exact). Verified bit-identical boundaries via a new "Boundary hash" line in `scripts/chunkbench` on `bts-5m-1080p-sdr` and `io-5m-4k-hdr`. 4K: 56.9s -> 34.5s. 8 workers showed no further gain. (`internal/chunkplan/chunkplan.go`)
6. **Crop detection workers 4 -> 8, decode threads 1 -> 2.** soms-5m: 13.1s -> 4.1s. (`internal/processing/crop.go`)
7. **New ground-truth tool `scripts/fullvalidate`.** Scores a finished encode against its source with full-chunk CVVDP per chunk and compares against the sampled scores the search believed. This is the accuracy ruler for all future knee-point tuning.

### Results

`sully-5m-4k-hdr`, restructured build vs same-day baseline (`sully-simplified1`, same search code):

| Metric | Baseline | Restructured |
|---|---:|---:|
| Total time | 19m48s | 9m44s |
| Video encoding stage | 17m54s | 8m48s |
| TQ stage wall (chunk timestamps) | 1074s | 528s |
| Avg chunks in flight | 2.55 | 5.82 |
| Probes | 71 | 59 |
| Stops | converged:28, max_probes:3, monotonicity:2 | converged:32, monotonicity:1 |
| Sampled mean abs error | 0.0949 | 0.0530 |
| Sampled JOD min | 9.1173 | 9.2913 |
| Initial sources | neighbor:30, median:1, default:2 | neighbor:28, median:2, default:3 |
| Crop detection | 43.5s | 17.3s |
| Shot cut detection | 69.5s | 38.5s |

Total wall time halved on 4K HDR. The flight gate kept the prior cascade intact (28 neighbor seeds vs 30), so the speedup came without the cold-start probe inflation seen in the ungated attempt. Probe-count and error improvements on this clip are partly luck on its known cliff chunks; the structural claim is only "no accuracy cost". This clip remains noisy; judge accuracy claims on more clips.

Ground truth (`scripts/fullvalidate`, full-chunk CVVDP of the final 4K output):

- Full-chunk JOD: min 9.2913, median 9.4487, mean 9.4473, max 9.7227.
- vs target (9.39 +/- 0.14): mean abs error 0.0845, **0 chunks below range, 7 above** (overshoot, i.e. spending bits beyond need — a future bit-efficiency target, not a quality risk).
- vs sampled scores: mean abs gap 0.0360 overall, but **0.0000 on the worst (lowest-JOD) chunks** — under the 12s cap those fall at/under the 256-frame full-probe threshold and are scored whole, so sampling has zero error on exactly the tail chunks that drive quality risk. The gap lives entirely in the higher-scoring, larger sampled chunks where it does not threaten the floor.

Both clips: no chunk landed below the target band in ground truth. The concurrency restructure is accuracy-safe by measurement, not just by construction.

`soms-5m-1080p-sdr`, restructured build vs same-day baseline (`simplified1`):

| Metric | Baseline | Restructured |
|---|---:|---:|
| Total time | 4m13s | 3m43s |
| TQ stage wall (chunk timestamps) | 232s | 212s |
| Avg chunks in flight | 6.99 | 10.68 |
| Probes | 53 | 55 |
| Stops | converged:37 | converged:37 |
| Sampled mean abs error | 0.0650 | 0.0584 |
| Initial sources | neighbor:29, default:8 | neighbor:29, default:8 |
| Crop detection | 13.1s | 4.2s |
| Shot cut detection | 7.8s | 7.0s |

The 1080p speedup is modest (232 -> 212s on the TQ stage) because this clip was already at the GPU CVVDP floor: metric is 65% of busy time and aggregate scoring throughput is the ceiling. Chunks in flight rose 7.0 -> 10.7 but the GPU can only score so fast. The pre-encode head shrank from ~21s to ~11s. Identical prior cascade (29 neighbor / 8 default) confirms the flight gate preserves scheduling behavior on SDR too.

### Ground-truth accuracy check (`scripts/fullvalidate`, full-chunk CVVDP of the final soms output)

This is the load-bearing accuracy result for the whole restructure, because the speedups would be worthless if sampled scores diverged from reality:

- Full-chunk JOD: min 9.2629, median 9.4162, mean 9.4160, max 9.5436.
- vs target (9.39 +/- 0.14): mean abs error 0.0585, **0 chunks below range, 1 above** (chunk 0009-adjacent, 9.54).
- vs the search's own sampled scores: **mean abs gap 0.0123** — the 3x48 sampled windows tracked true full-chunk quality to ~0.012 JOD on average on this clip. The worst single-chunk gap was 0.037 (chunk 0005, sampled 9.281 vs full 9.318 — still comfortably in range).

The sampled-probe strategy is not just internally consistent; it matches ground truth. None of the concurrency changes moved final quality out of the target band.

## 2026-06-12 4K adaptive ramp: bandwidth, not capacity (finding + fix)

Goal: the previous "open question 6" claimed 4K was a free speed win held back by a lumpy ramp signal and 6-minute lockouts, citing that sully runs held 47+ GiB free while pinned at 3-4 workers. Investigated whether ramping 4K higher actually helps.

### The premise was a misdiagnosis

It does not. 4K SVT-AV1 encodes are **memory-bandwidth bound, not RAM-capacity bound.** The 47 GiB-free observation was a red herring: the machine was never short of RAM, it was short of memory bandwidth and cache, which does not show up as "available memory." Three `sully-5m-4k-hdr` runs (32 logical cores, 61 GiB RAM), TQ-stage wall from per-chunk timestamps:

| Build | Worker target | ~Active encodes | Per-probe encode | TQ wall | Video encode | Peak mem / min avail |
|---|---:|---:|---:|---:|---:|---|
| Slot-release baseline (committed `b652b74`) | 3 -> 5 (slow climb) | ~3.1 | 27.1s | 528s | 8m47s | -- / -- |
| Memory-informed start (9) + util ramp | 11 | ~9 | 36.8s | 572s | 9m32s | 33.4 GiB / 28.5 GiB |
| **Bandwidth-capped start (5)** | **5 fixed** | **~4** | **28.6s** | **495s** | **8m15s** | 25.1 GiB / 36.8 GiB |

Pushing concurrency from ~3 to ~9 active encodes raised per-probe encode time 27 -> 37s (+36%, far more than the +8% probe count) and made total wall time *worse* (528 -> 572s), with tens of GiB still free and zero memory-pressure events. The throughput-per-encode falls faster than concurrency rises past roughly one active 4K encode per 6 logical cores.

Neither of the signals the open question suggested can find this knee: slot utilization stays ~1.0 (slots read "busy" while each encode crawls), and available memory never drops. Only end-to-end throughput sees it, and that is exactly the noisy signal the old ramp used. So the answer is not "ramp 4K higher with a better signal" -- it is "cap 4K near its bandwidth optimum and start there."

### Fix (kept)

1. **Bandwidth-aware 4K ceiling.** `resolutionRampCeiling` caps the 4K worker target at `maxWorkers/6` (one active encode per ~6 logical cores; 5 on the 32-core dev machine), bounded by RAM on small machines via `workerMemoryUHD`. HD/SD keep the full hardware ceiling -- they are GPU-metric bound and self-limit via utilization. (`internal/encode/adaptive.go`)
2. **Start 4K at the ceiling.** `initialAdaptiveWorkers` starts 4K directly at that ceiling instead of climbing from 3, so a 5-minute clip does not waste its first minutes at low concurrency. This is the entire measured win: `cap5` (start 5) beat the committed build (climb 3->5) by 33s of TQ wall, with fewer probes.
3. **Replaced the throughput ramp with a utilization ramp.** The old frames/s ramp (90s windows, multi-tier modest/blocked verdicts, 3-6 minute lockouts, ~120 lines of state) is gone. The limiter now samples encode-slot utilization each monitor tick and adds a worker only when slots stay saturated (>=85% over a 60s window) and memory is stable, up to the resolution ceiling. This is a maintainability/robustness win (removes the documented lockout pathology) and matters mainly for HD/CRF; 4K starts at its ceiling and does not ramp. Memory-pressure backoff is unchanged -- still the real safety net.
4. **Prior-paced flight gate.** In-flight chunk concurrency now opens as completed chunks accumulate usable CRF priors (`primeConcurrency + 3*(done - primeChunks)`, capped ~2x target) instead of a flat `target + metricWorkers`, keeping encode slots fed across the probe/score duty cycle without flooding cold-started chunks. (`internal/encode/target_quality.go`)

Note the ceiling divisor (6) is calibrated on the dev machine's dual-channel DDR5 + 32 cores. Memory bandwidth per core varies by platform; a future agent on different hardware should re-measure the 4K active-encode knee (run the three-point sweep above) before trusting `maxWorkers/6`. The RAM bound and pressure backoff keep it safe regardless; only the speed optimum is hardware-specific.

### Results (kept build: `cap5`)

| Clip | Metric | Committed `b652b74` | Bandwidth-capped |
|---|---|---:|---:|
| `sully-5m-4k-hdr` | Video encode stage | 8m47s | **8m15s** |
| | TQ wall (chunk timestamps) | 528s | **495s** |
| | Worker target | 3 -> 5 climb | 5 fixed |
| | Probes | 59 | 58 |
| `soms-5m-1080p-sdr` | TQ wall | 212s | 209s (neutral) |
| | Initial sources | neighbor:29, default:8 | neighbor:28, default:8, median:1 |

4K total wall improved ~6% by starting at the bandwidth optimum instead of climbing to it, with fewer probes and no memory pressure. HD is unchanged (the HD path is untouched: floor start 8, GPU-bound, util ramp rarely fires). Both within their clips' run-to-run noise on probes/error.

Ground truth (corrected `scripts/fullvalidate`, full-chunk CVVDP of each chunk's standalone encoded IVF):

- `sully-5m-4k-hdr`: mean 9.4137, **0 chunks below range**, 3 above (overshoot), sampled-vs-full gap 0.0470, worst single gap -0.085. No quality regression from the higher concurrency.
- `soms-5m-1080p-sdr`: mean 9.4231, **0 below**, 2 above, gap 0.0146.

### fullvalidate bug found and fixed during this work

The first 4K ground-truth pass reported 8 chunks at ~8.81 JOD (vs sampled ~9.4, gaps up to -0.72) -- alarming until the clustering of 8 unrelated chunks (CRFs 42-52, different content) within a 0.03 JOD band flagged it as a measurement artifact, not real quality. Those 8 chunks were all `nwin=1` full-probe chunks whose probe bits were reused verbatim as the final output, so the search had already measured them by full-chunk CVVDP at 9.36-9.55 -- the same metric on the same bits. Scoring each chunk's standalone `encode/NNNN.ivf` from frame 0 reproduced the search scores exactly (9.4237 / 9.5042 / 9.3638 vs sampled 9.424 / 9.504 / 9.364).

Root cause: the original fullvalidate seeked to `ch.Start` in the **muxed AV1 output**. Reel's frame seek is timestamp-based (`startTime + frame*tsMul/tsDiv`) and lands a few frames off for some chunks in a long muxed AV1, mis-aligning source vs output and producing a spurious ~8.8 "garbage" score. This is the only code path in the project that seeks into a muxed AV1 output; the encode always scores standalone probe IVFs from frame 0, and source-side seeks are exercised constantly and proven reliable. Fixed by scoring each chunk's `encode/NNNN.ivf` from frame 0 (the IVFs concatenate losslessly into the muxed output, so it is the same bits without the seek). `fullvalidate` now takes `<source> <workdir>` instead of `<source> <encoded> <workdir>`.

Caveat for earlier entries: the soms and sully ground-truth numbers recorded under "2026-06-12 concurrency restructure" came from the pre-fix tool. They happened to land clean (0 below range) because those runs' muxed seeks mostly aligned, and re-scoring soms with the fixed tool agrees closely (gap 0.0146 vs 0.0123). Treat any single pre-fix per-chunk full score with suspicion, but the aggregate "0 below range" conclusions held.

## 2026-06-13 4K metric workers 4 vs 6 retest

**Status: CONTAMINATED / ARCHIVED.** The raw mw4-vs-mw6 comparison was a
scoring-cascade artifact from the old async-allocator libvship build; mw6 simply
hit the corruption more often. Do not cite the raw timings, probe counts, or
seconds/probe normalization from this entry.

Current answer: keep UHD metric workers at 4. The durable evidence is the
2026-06-19 post-restore sweep plus the 2026-06-12 metric-worker scaling curve:
4K is encoder-bound under the `maxWorkers/6` cap, so extra metric workers add
VRAM and little wall-time benefit.

Raw historical detail was moved to
`docs/PERFORMANCE_TESTING_ARCHIVE.md#2026-06-13-4k-metric-workers-4-vs-6-retest`.

## 2026-06-13 SVT-AV1 level_of_parallelism: bitstream identity and 4K scaling

Goal: open question 8. `level_of_parallelism` (lp) was derived from the raw hardware core count via `levelOfParallelismForWorkers(maxWorkers)`, which pins lp=2 on any machine with >5 cores -- including for 4K, where the `maxWorkers/6` bandwidth cap holds sustained concurrency near 5 and the early prime/probe phase runs only 2-4 encoders. The hypothesis: at that low concurrency each encoder should use more internal threads (higher lp), so lp should scale off the resolution-aware worker target, not the hardware max. This is only safe if lp is bitstream-neutral.

Environment: same dev machine (32 logical CPUs, RTX 5060 Ti 16 GB VRAM, dual-channel DDR5).

### Part 1 -- bitstream identity (gating condition)

`TestLevelOfParallelismBitstreamIdentical` (internal/encoder) encodes a deterministic synthetic 10-bit 320x240 clip through the real `EncodeChunkToIVF` path at lp = {1, 2, 3, 4, 6} and SHA256s each output. All five are byte-identical (same 151307 bytes, same hash). lp is purely a throughput knob; it cannot change encode results. Confirmed again end-to-end below: every fixed-CRF run of a given clip produced an identical output hash across lp=2 and lp=3 and across both rounds.

### Part 2 -- throughput (the actual question)

Two instruments. Target-quality (TQ) mode wall time is **uninterpretable** for this question: lp perturbs chunk-completion timing, which shifts the neighbor/median prior cascade, which changes probe counts and final CRFs. The io lp2-vs-lp3 TQ pair differed 106 vs 63 probes with wildly different final CRFs (chunk 32: CRF 4.25 vs 39.75); across rounds the direction even reversed (io lp3 faster, sully lp3 slower) and within-config spread hit 320s (io lp2: 828s vs 508s). Same confound class as the metric-worker retest, but far larger here.

The clean instrument is **fixed-CRF mode** (`--quality-mode crf --crf 32`): no probe search, so lp2 and lp3 do byte-identical work and wall time is pure encoder throughput. Two clips, two interleaved rounds each (8 runs). Harness/artifacts: `~/testing/perf-ab/lp-retest/` (`orchestrate-crf.sh`, `run-one-crf.sh`, per-run `.log`/`.vram`, `results-crf.tsv`; the confounded TQ runs are in `orchestrate.sh`/`results.tsv` for the record).

| clip | lp=2 (s) | lp=3 (s) | lp=3 gain |
|---|---:|---:|---:|
| io-5m-4k-hdr | 245, 244 (mean 244.5) | 237, 236 (mean 236.5) | 3.3% |
| sully-5m-4k-hdr | 236, 234 (mean 235.0) | 226, 226 (mean 226.0) | 3.8% |

Run-to-run variance is +/-1s (vs the TQ test's +/-160s), so the ~3-4% is real, not noise. Note both 4K test clips are low-grain (io clean CG animation, sully clean ARRI Alexa 65 digital -- see `~/testing/README.md`); grain-heavy 4K (e.g. kbv1) was not exercised, but lp is bitstream-neutral regardless of content, so this only affects the magnitude of the throughput delta, not its sign.

Interpretation:

- lp=3 is a consistent ~3-4% throughput win on full 4K encodes, with provably identical output. The gain is modest because most of the encode runs at the ~5-worker bandwidth ceiling where lp matters little; it concentrates in the low-concurrency prime/probe ramp, which is a small fraction of a full encode but is exactly the window the goal flagged.
- Because the change is free (byte-identical, no quality/size/VRAM cost -- fixed-CRF peak VRAM was ~0, no GPU scoring), there is no downside to taking the small win.

### Change

`levelOfParallelismForWorkers` is now fed the resolution-aware ramp ceiling (`resolutionRampCeiling`) instead of `maxWorkers`, at both call sites (`encode.go`, `target_quality.go`). Effect by case (mapping unchanged: <=2->lp4, <=5->lp3, else lp2):

- 4K on this 32-core box: ceiling `max(3, 32/6)=5` -> **lp 3** (was 2).
- 1080p/SD on the same box: ceiling = maxWorkers (32) -> lp 2 (unchanged).
- 4K on a small machine (<=~18 cores): ceiling 3 -> lp 3.
- 4K on a 64-core box: ceiling 10 -> lp 2 (concurrency already fills the cores).

A new `--level-of-parallelism <1-6>` flag (config `SVTAV1LevelOfParallelism`, 0 = auto) overrides it for testing/operator use. Unit test `TestLevelOfParallelismFromRampCeiling` pins the resolution-derived values.

Conclusion: scale lp off the worker target. Keep the change.

## 2026-06-13 Accuracy-trading TQ knobs: full-probe threshold, window size, chunk cap

**Status: PARTIAL / CONFOUNDED / ARCHIVED.** This A/B correctly identified the
256-frame full-probe threshold, 3x48 window size, and 12s chunk cap as
accuracy-trading knobs that must be judged with `scripts/fullvalidate`. But the
reported magnitude for lowering the full-probe threshold to 144 frames was
inflated by the later-discovered scoring cascade: the 144-frame build produced
more sampled chunks and therefore more exposure to the corrupt concurrent
handler path.

Current answer: keep the shipped knobs. The direction of the threshold result
remains unfavorable, the 12s cap rarely binds, and the 2026-06-18 fixed-binary
re-baseline shows the shipped config is clean. Re-test on the fixed MITIGATE
build before citing old numeric penalties or changing the threshold.

Raw historical detail was moved to
`docs/PERFORMANCE_TESTING_ARCHIVE.md#2026-06-13-accuracy-trading-tq-knobs-full-probe-threshold-window-size-chunk-cap`.

## 2026-06-14 Overlapping the pre-encode head (shot detection) with encoding

Open question 10. The claim under test: overlapping shot detection with the start of
encoding is the remaining structural win for long inputs, needs streaming chunk planning,
and is not worth it below feature-length inputs. Investigated by code analysis plus cheap
`scripts/chunkbench` timing (shot detection only, no encode/CVVDP/mux). No full encode was
run -- the prize is bounded by the head's wall time, which `chunkbench` measures directly.

### Sizing the prize: shot detection scales linearly with duration

`chunkbench` on the 5m/10m/20m cuts (this machine, 32 logical CPUs; `chunkplan.Plan` is
not cached, so each run is fresh):

| res   | clip | cut | frames | shot detection | ms/frame |
|-------|------|-----|-------:|---------------:|---------:|
| 1080p | soms | 5m  |   7265 |          6.9s  |    0.95  |
| 1080p | soms | 10m |  14498 |         13.8s  |    0.95  |
| 1080p | soms | 20m |  28772 |         27.2s  |    0.95  |
| 4K    | io   | 5m  |   7222 |         34.1s  |    4.72  |
| 4K    | io   | 10m |  14390 |         66.2s  |    4.60  |
| 4K    | io   | 20m |  28840 |        145.9s  |    5.06  |

Clean linear scaling (4K creeps mildly super-linear, likely deeper-seek/cache effects).
Extrapolated to a 2-hour feature (~173k frames @ 24fps): **1080p ~2.7 min, 4K ~14-15 min**
of shot detection. So the overlap prize is real only at 4K feature length; at 1080p it
stays small (minutes) even for a full movie, and on any 5-20m clip it is tens of seconds --
far below the cost of the rework. This confirms the "long inputs only" half of the claim.

### Why naive streaming does not work: three global-statistics barriers

The one-line claim ("scan front to back, release early chunks as their cuts are known")
mischaracterizes the current algorithm. Two structural facts in `internal/chunkplan/chunkplan.go`:

1. **Raw scoring is already fully parallel across the whole file, not front-to-back.**
   `scoreVideo` splits the file into N contiguous segments scored by concurrent workers with
   a `wg.Wait()` barrier (the back of the film starts decoding at the same instant as the
   front). There is no front-to-back frontier to ride.
2. **Every boundary depends on whole-file statistics.** Three steps each need the complete
   score array before *any* boundary is final:
   - `shotCutThreshold` -- the cut threshold is `median + 6*MAD` clamped to `p95*1.10` over
     *all* per-frame scores. Whether frame 1000 is a cut depends on the score distribution
     of the entire film.
   - `strongCutThreshold` -- a p90 over all cut scores, used to protect strong cuts from merging.
   - `packShortShots` / `packWeakCutsToTarget` -- the merge/pack passes iterate the whole cut
     list to hit min/target chunk sizes.

So streaming chunk planning is not "emit boundaries as you go." It requires:
- replacing the two global thresholds with online/windowed estimators -- **this changes which
  frames become cuts, i.e. it changes the boundaries**, so it is an accuracy-affecting change
  that must pass the `chunkbench` "Boundary hash" check and `scripts/fullvalidate`, not a
  mechanical refactor;
- a bounded-look-ahead commit protocol for the merges (they only need neighbors within
  `maxFrames`, so a boundary can be committed once scoring passes ~one chunk beyond it --
  feasible, but new code);
- making the consumer incremental: today `chunk.LoadSegments` -> `chunk.Chunkify` ->
  `buildResumeManifest` all materialize the *complete* boundary list before the first encode,
  and the resume manifest is built from the full chunk set. Streaming needs an append-safe
  plan file, a growing chunk feed into the dispatcher, and an append-safe resume manifest.

### Why the win is real (when it is worth taking)

Shot detection is CPU + libav-decode bound and uses no GPU. Both 1080p and 4K TQ encoding
are GPU-CVVDP-throughput bound (established above: metric is the ceiling, encoder sustains
only ~5 active workers at 4K). The two workloads are genuinely complementary, so overlapping
shot-detection CPU work with GPU-bound scoring is close to free *in principle* -- the head
time would be hidden rather than traded. Caveat: shot detection's 4 decode workers do contend
for CPU with SVT-AV1, so the real-world overlap efficiency would need measurement; the prize
is an upper bound, not a guarantee.

Crop stays a serial pre-step (correctly out of scope here): it feeds the analysis rectangle
into shot detection *and* the crop filter into every encode, and it is a fixed 141-sample
whole-file vote, so nothing can start before it. It is also the smaller component post the
2026-06-12 parallelization.

### Change: None (deferred with evidence)

The claim holds. The prize is ~14-15 min only on a 4K feature; the cost is an accuracy-
affecting rewrite of cut-threshold statistics plus incremental plan/encoder/resume plumbing.
Not worth it for the 5-20m test clips this project iterates on, and there is no feature-length
clip here to ground-truth-validate an online-threshold change against. Revisit only if 4K
feature-length wall time becomes the priority; the gating prerequisite is an online cut
threshold that reproduces the current "Boundary hash" closely enough to pass `fullvalidate`,
since changing boundaries changes which frames get encoded together.

## 2026-06-14 Where to go next: strategic analysis and recommendations

**Status: SUPERSEDED / ARCHIVED.** This strategic snapshot was useful at the
time, but later entries resolved or overturned its main leads: the content-prior
seed was rejected by correlation test, feature-length behavior was measured, the
target band was widened, metric concurrency was restored, metric workers were
re-tuned, and preset 6 was confirmed.

Current answer: use `docs/PERFORMANCE_TESTING.md` for the active open list. The
only remaining medium-priority throughput item is re-checking the 4K
encode-concurrency ceiling on the fixed build; most other ideas are low-priority
or monitor-only.

Raw historical detail was moved to
`docs/PERFORMANCE_TESTING_ARCHIVE.md#2026-06-14-where-to-go-next-strategic-analysis-and-recommendations`.

## 2026-06-14 Content-prior first-probe seed: correlation test (rejected)

Goal: the cheap gating test for the content-prior idea from the analysis above. Do the
per-frame shot-detection scores that `scoreVideo` already computes (and discards) predict a
chunk's final CRF well enough to seed the first probe? If yes, build the seeder; if no, kill it
for the price of a script. Decided with zero encodes, against existing artifacts.

### Method

Added a zero-cost `RetainScores` option to `chunkplan.Plan` (frees the scores slice as before
when false; `internal/chunkplan/chunkplan.go`) and a frame-scores dump to `scripts/chunkbench`
(`chunkbench <video> [scores-out.txt]`). Ran chunkbench once per source clip (activity is a
property of the source, identical across encode runs) to dump per-frame scores, then a joiner
(`/tmp/activity_corr.py`) reconstructed each artifact's per-chunk frame intervals from
cumulative `frames`, aggregated the interior scores (mean/median/p90/max/std, skipping the
boundary-cut frame), averaged `final_crf` per chunk across all matching runs of that clip to
damp probe-cascade noise, and correlated activity vs CRF. Correlation is measured **within each
clip**: between-clip CRF differences (4K vs 1080p, grain) are exactly what neighbor/median
priors already handle, so the open question is chunk-to-chunk variation inside one film.

Artifacts joined: soms (4 runs), sully (13), io (8), kbv1 (2) from `~/testing/perf-ab/`.

### Result (Spearman rho of per-chunk activity vs averaged final_crf)

| clip | nchunk | runs | crf sd | mean | median | p90 | max | std |
|------|-------:|-----:|-------:|-----:|-------:|----:|----:|----:|
| soms-5m-1080p-sdr | 37 | 4 | 2.12 | **+0.544** | +0.596 | +0.475 | +0.074 | +0.030 |
| sully-5m-4k-hdr | 33 | 13 | 5.00 | -0.127 | -0.197 | -0.120 | -0.247 | -0.212 |
| io-5m-4k-hdr | 36 | 8 | 4.61 | **-0.631** | -0.540 | -0.634 | -0.479 | -0.552 |
| kbv1-5m-4k-hdr | 37 | 2 | 3.16 | **-0.558** | -0.563 | -0.549 | -0.228 | -0.361 |
| POOLED (within-clip z) | 143 | | | -0.147 | -0.162 | -0.139 | -0.209 | -0.265 |

### Verdict: rejected

**The sign flips across clips.** soms (1080p) is strongly *positive* (+0.54: high temporal
activity -> higher CRF, i.e. motion masking lets you spend fewer bits), while io and kbv1 (4K)
are strongly *negative* (-0.63 / -0.56: high activity -> lower CRF). The per-clip correlations
are real and significant (io -0.631 at n=36 is p<0.001), so the signal is being measured
correctly -- but the relationship is clip-specific in both slope and sign, so the pooled
correlation is weak (-0.15 to -0.28) because the clips cancel.

That is fatal for the cheap version of the idea. The whole appeal was a *global* first-probe
seed that fixes cold starts before any chunk of the film has completed. A global activity->CRF
mapping calibrated on one clip would push the first probe the **wrong direction** on others
(seed too low on soms-like content if fit on io, and vice versa), which adds probes rather than
removing them -- the opposite of the goal. It stays accuracy-safe (the search still converges),
but it fails the "can only help or be neutral on probe count" promise that justified it.

Why the signal is weak as a complexity proxy: the shot-change score is a *temporal-change*
measure (65% pixel diff + 30% histogram + 5% luma between consecutive frames, on a 64x36
downsample), not the spatial detail/grain that most directly drives JOD-at-CRF. The 64x36
downsample also cannot see grain. So this particular already-computed signal is the wrong
feature, independent of the sign problem.

The only non-dead remnant: a *within-run* adaptive activity regression (learn this film's sign
and slope from its completed chunks, then refine seeds for not-yet-encoded chunks) could exploit
the moderate per-clip correlations -- but it needs completed chunks to fit, so it does nothing
for cold start (the actual prize), it overlaps the timeline-local neighbor prior that already
captures chunk-to-chunk drift, and sully's weak -0.13 shows it would not even help every film.
Poor expected value vs complexity; not pursued.

### Change

None to the encoder. The `RetainScores` hook (`chunkplan.go`) and the chunkbench frame-scores
dump are kept -- they are zero-cost, reusable, and make this analysis (or a future test with a
proper spatial-complexity feature) reproducible. The joiner is the throwaway `/tmp/activity_corr.py`.

## 2026-06-14 Full-length 4K encode: stage attribution (the first feature-length run)

Goal: open question 0. Every prior conclusion in this doc was validated on 5-20m clips. Run one
real feature and attribute its stages, to confirm the linear extrapolations and the prediction
that probes/chunk *drops* under a deep (feature-length) prior cascade. This is the first
end-to-end feature-length encode the project has measured.

Input: full Sully theatrical rip (`~/.cache/spindle/rips/.../Sully_t00.mkv`), 3840x2160 10-bit
PQ HDR, 23.976 fps, 01:35:50 (137,877 frames -- ~19x the 5m cut). Default pipeline (autocrop on,
preset 6, CVVDP target 9.25-9.52, metric workers 4). Dev machine (32 logical CPU, RTX 5060 Ti
16 GiB, dual-channel DDR5, 62 GiB RAM). Cmd: `reel encode -i <rip> -o sully-full.mkv -v
--keep-workdir`. Artifacts in `~/testing/fulllen-attr/` (encode.log, resource.log, kept workdir
7.9 GiB). Crop resolved to 3840x1600 (2.40:1). Output 2.86 GiB, 93.7% reduction.

### Stage attribution (total wall 4h2m47s)

| Stage | Feature | 5m sully cut | Scaling vs 5m | Verdict |
|---|---:|---:|---|---|
| Crop detection | 17.4s | 17.3s | **constant, NOT linear** | fixed 141-sample vote; negligible at feature length |
| Shot cut detection | 12m0s | 38.5s | linear (predicted 12m19s) | **linear confirmed**; matches the Q10 ~14-15min estimate |
| Chunk planning | <1s | <1s | flat | flat |
| TQ + encode + mux | ~3h50m | ~8m15s | **super-linear (+45% over linear)** | the surprise -- see below |

Crop is a fixed 141-sample whole-file vote, so a 96-min film costs the same ~17s as a 5m cut --
the earlier "linear in duration" note in the 2026-06-12 attribution was wrong. Shot detection is
cleanly linear and validates the streaming-head (Q10) sizing. The encode stage is where the
extrapolation broke.

### Headline finding: short clips badly under-represent feature-length probe cost

A linear-in-frames extrapolation from the 5m cut predicted a ~2.6h encode stage; the actual was
3h50m (+45%). The cause is **probes/chunk nearly doubled**, because a full film has far higher
chunk-to-chunk CRF variance than any single 5-20m cut (which is one homogeneous scene).

| Metric | sully FULL 96min | sully 5m cuts | io 5m cuts |
|---|---:|---:|---:|
| chunks | 670 | 33 | 36 |
| **probes/chunk** | **3.11** | 1.5-1.9 | 1.6-1.9 |
| converged | **63%** | 85-97% | 100% |
| chunks hitting max 6 probes | **21%** | 0-3% | 0% |
| final_crf spread (sd) | **12.35** | 5.7-7.1 | 5.8-6.0 |
| final_crf range | 4.25-59.5 | ~ | ~ |
| initial source = neighbor | 81% | 85-92% | 89-92% |
| initial source = default (cold start) | **3 / 670 (0.4%)** | 3 / 33 (9%) | 3 / 36 (8%) |

What this means:

- **The narrow hypothesis was right: the deep cascade eliminates cold starts.** Only 3 of 670
  chunks fell back to the blind default seed (0.4%, vs ~9% on a 5m clip); 81% got neighbor
  seeds, 19% median. So priors are healthy at depth.
- **But the overall prediction (probes/chunk drops) is REFUTED.** Probes/chunk went *up* 1.7 ->
  3.11. The cold-start saving is real but tiny, and it is swamped by a much larger effect: a
  feature spans dark grainy interiors, bright simple skies, water-rescue action, and credits, so
  adjacent chunks legitimately want very different CRFs (final CRF 4.25-59.5, sd 12.35). The
  neighbor prior is far less predictive when neighbors genuinely differ, so each chunk needs more
  probes to converge -- and 21% exhaust all 6.
- **Therefore every 5-20m-clip result in this doc UNDER-estimates real probe cost and TQ wall
  time.** A single short cut is low-variance by construction (one scene), so it converges in ~1.7
  probes; the real workload converges in ~3.1. This is the most important methodological takeaway
  here: short homogeneous clips are fine for accuracy ground-truth (fullvalidate) and for
  bitstream/throughput knobs, but they systematically flatter the search's probe efficiency.

### The probe tail is the dominant feature-length cost (and it is bit-efficiency, not quality)

Stop reasons at feature length: converged 420 (63%), monotonicity_guard 147 (22%), max_probes 65
(10%), bounds_crossed 38 (6%). So 37% of chunks did not cleanly converge -- vs ~5-15% on 5m
clips. Where do the non-converged chunks land (sampled final score vs band 9.25-9.52)?

- in-band 458 (68%), **above-band 201 (30%)**, below-band 11 (2%, worst 9.191 -- only 0.06 under
  the floor).
- The non-converged chunks overwhelmingly land *above* the band: monotonicity_guard 104/147
  above, max_probes 64/65 above, bounds_crossed 33/38 above.

So when the search cannot converge on hard feature content, it errs **high** -- it overshoots
quality (CRF too low, bits spent beyond need) rather than risking quality. This is a
bit-efficiency cost, not a quality risk (only 2% marginally below band). 30% overshoot at feature
length vs ~7-15% on 5m clips means there is real size headroom being left on the table. (Sampled
scores; a fullvalidate ground-truth pass on the kept workdir would confirm -- not yet run.)

### Robustness: no feature-length structural problems

The code-review predictions from the 2026-06-14 strategic analysis held:

- **No memory leak.** RAM rose to a ~22-24 GiB steady state by chunk ~100 and stayed flat through
  chunk 670 (peak ~24.5 GiB of 62, never under 37 GiB available). The bounded flight cap held.
- **Concurrency did not collapse.** The 4K worker *target* was 5 for 542 of 551 progress ticks
  (~98%); only 2 brief memory-pressure transients early on dipped it to 3 (swap noise, recovered
  immediately). The `maxWorkers/6` bandwidth cap behaves identically at feature scale.
- **GPU VRAM** peaked ~6 GiB (same as the 5m mw4 runs) -- metric-worker sizing holds.
- **enc/met busy-time ratio 1.23** (probe encode 55% / metric 45%) -- 4K stays encode-bound at
  feature length, consistent with the 5m attribution.

### Implications (and the Q10 head-overlap re-read)

1. The **search-layer probe-tail items are now the highest-value performance work**, not a
   "wait for more clips" backlog. The tail (21% maxing probes, 22% stopping on monotonicity) is
   the dominant feature-length cost and is invisible on short clips. Flat-low early stop (search
   item 5) and a look at why monotonicity_guard fires on 22% of chunks are the concrete next
   steps. Reducing average probes/chunk from 3.1 toward 2 would cut feature wall time by roughly
   a third.
2. **Future tuning must validate against high-variance content**, not a single homogeneous cut.
   Cheapest fix: build a "diverse" test clip by concatenating several dissimilar scenes from one
   film, so probes/chunk approaches the feature-length regime without a 4-hour run. Otherwise run
   a real feature.
3. **Q10 (overlap the pre-encode head) is even less attractive than before.** The head was 12.3
   min of a 4h2m wall = ~5%, vs ~12% on a 5m clip -- because shot detection grows linearly while
   the encode stage grows super-linearly. Overlapping the head saves ~5% at feature length;
   reducing the probe tail saves far more. Keep Q10 deferred.

### Change

None. This is a measurement entry. The encoder behaved correctly at feature length; the finding
is methodological (short clips under-state probe cost) and re-prioritizes the search-layer tail
work. Kept workdir at `~/testing/fulllen-attr/.reel-Sully_t00-1ce039b19801` for a possible
feature-length fullvalidate ground-truth pass.

## 2026-06-14 monotonicity_guard diagnostic: why the feature-length probe tail overshoots

The feature run flagged `monotonicity_guard` as the single biggest probe consumer (147/670
chunks, 670 probes = 32% of all probe work, mean 4.56 probes/chunk, and 104/147 land above the
band). Before building any new gate, a diagnostic to disambiguate two populations with opposite
fixes: *genuinely flat* (guard correct, overshoot unavoidable, win = stop sooner) vs
*noise-tripped* (guard premature, one more probe recovers bits). Pure re-analysis of the recorded
probe sequences in the kept Sully `target-quality.json` -- no encodes.

### Method

For each guard chunk, replay reel's exact convergence rule (`tq.go`): a probe converges iff
`target-tol <= mean_score <= target+tol` AND `worst_window >= target-tol`. Decompose *why* no
probe converged, measure the geometry of the miss, and test interpolation reliability empirically
via leave-one-out on real probes (predict each interior probe from its two CRF-neighbours, compare
to its measured score). Scripts kept in `/tmp/guard_*.py`, `/tmp/interp_reliab.py`,
`/tmp/resid_nature.py`.

### Result: it is a three-way split, and neither original hypothesis fits the majority

Authoritative decomposition of the 147 guard chunks (reel-exact criteria):

| why no probe converges | chunks | nature | recoverable? |
|---|---|---|---|
| **worst-window below floor** (mean IS in band) | 77 (52%) | a hard sub-segment fails the floor by ~0.020 JOD (median); conservative overshoot pick is correct | No -- content property, more probing cannot fix it |
| **mean never in band** (probes straddle target) | 58 (39%) | in-band CRF wedged between probes; tightest straddle gap median 0.75 CRF | Only ~60% -- the curve is noise-limited (below) |
| all mean below band (want lower CRF) | 9 (6%) | quality-leaning; BestProbe protects via worst-window floor | n/a |
| all mean above band (want higher CRF) | 3 (2%) | flat-high accelerator's domain | n/a |

Key supporting measurements:

- **Zero** guard chunks left a converge-able probe (mean-in-band AND worst-window-safe) on the
  table. The guard is not discarding good results.
- **Interpolation is only ~60% reliable on this content.** Leave-one-out residuals: median 0.075
  JOD, p90 0.34 JOD (2.5x the +-0.135 tolerance); only 57-63% land within tolerance. So "one more
  probe converges" is not a reliable recovery for the 39% mean-bracket group.
- **The residuals are noise, not curvature** (sign 64/36, magnitude random; only a mild convex
  lean). A better interpolator (quadratic) would not reliably help -- the limit is metric noise on
  the ~48-frame probe sample, not the search math.
- **The guard reacts to sub-noise wobbles.** Its tripping wrong-way move is median 0.022 JOD --
  three times smaller than the curve's own 0.075 JOD scatter. It is genuinely firing on noise.

### Verdict

The dominant guard population (52%) is a category neither hypothesis named: **worst-window-limited**.
The mean score sits in band but a hard sub-segment of the chunk misses the floor by ~0.02 JOD, so
the conservative overshoot is the *correct, quality-safe* pick -- the bits are not recoverable by
more probing. These chunks over-*probe* (the straddle/worst-window state is usually visible by
probe 2-3 but the guard fires at 4.56), so the only win here is **speed**, accuracy-neutral.

The 39% mean-bracket group is the closest to "noise-tripped," but recovery is noise-capped at ~60%.
Softening the guard would mostly convert guard-stops into `max_probes` stops (more probes) for
probabilistic partial recovery on a minority -- likely net-negative. The real lever for this group
is probe-sample noise (more sample frames = more GPU) or a wider tolerance, not the guard.

Consequences for the open-questions list:

- **Flat-low early-stop is the wrong tool and is rejected.** The all-mean-below population it
  targets is 9 chunks (6%) at feature length; the doc's "top priority" framing came from one 1080p
  clip (soms 0013) and does not generalise. The overshoot tail is an all-*above* / worst-window
  problem, not an all-below one.
- **The accuracy-safe win is a worst-window/straddle early-out:** when the mean is in band but the
  worst window is stuck below floor across a CRF bracket (or probes straddle target with the low
  side below floor), stop immediately and take the conservative overshoot instead of probing to the
  guard at 4.56. Estimated ~2 probes/chunk saved on ~52-69% of guard chunks ~= 7-10% of total
  probe work, with no quality or bit change. This is the concrete next build.

### Change

None yet (diagnostic only). A confirmatory targeted re-probe pass (re-encode ~10-15 mean-bracket
chunks at their interpolated CRF, measure real landing rate vs the 60% estimate) is available but
not decision-critical -- the noise-dominated residuals make its outcome predictable and it would
not change the gate choice. Next build is the worst-window/straddle early-out above.

## 2026-06-14 Worst-window / straddle early-out: simulated and rejected

The monotonicity_guard diagnostic recommended a worst-window/straddle early-out as an
"accuracy-neutral" probe-count win (stop once the band is provably unreachable; the conservative
overshoot pick "does not change, only how fast we reach it"). Before building it, I validated that
premise by replaying candidate stop rules over all 670 recorded Sully probe sequences -- truncating
each at the rule's stop point and comparing `BestProbe(prefix)` to `BestProbe(all)`. No encodes.
The Python `BestProbe`/floor reimplementation reproduces reel's recorded `final_crf` on 670/670
chunks, so the simulation is faithful. Scripts: `/tmp/sim_oracle.py`, `/tmp/sim_rule.py`,
`/tmp/direction.py`.

### The premise is false: post-straddle probes refine the pick

- **Oracle ceiling (hindsight):** only 12.7% of all probes (266/2087) are taken *after* the result
  has already stabilised; 208 of those are in guard chunks (31% of guard probes). Mean probes/chunk
  could drop 3.11 -> 2.72 with a perfect oracle.
- **No causal rule reaches it safely.** Two principled result-safe rules -- "best is a safe
  overshoot and a floor-fail sits adjacent on the target side" and "worst-window slope predicts the
  next grid step falls below floor" -- **fire zero times**. An interval-collapse rule saves 1.1% but
  already changes 12 picks. The 12.7% waste is only separable with hindsight: at the moment a
  straddle appears the search genuinely cannot tell whether the next probe will improve the pick.
- **The diagnostic's literal rule trades accuracy.** "Stop when probes straddle the band"
  (mean above-band + mean below-floor) saves 30% of probes -- matching the diagnostic's ~2.2
  probes/chunk estimate -- but **changes the final pick on 216/670 chunks (32%)** by a median 0.126
  JOD, max 0.534 (about a full +-0.135 tolerance band). The worst-window-bracket variant: 34% saved,
  232 picks changed. So post-straddle probes are not waste; they materially reduce overshoot.

### What the trade actually is: quality-safe but bitrate-expensive

Every one of the 216 changed picks lands on a **more-overshooting** probe (higher quality, larger
file); **zero** drop below the floor. So the early-out introduces no quality risk -- but the
early-chosen probe encodes are **~47% larger (median, p90 2.3x)** than the converged pick. Stopping
early spends ~30% fewer probes in exchange for keeping ~0.13 JOD of imperceptible over-quality, at
roughly +15% output size across the whole encode (0.32 of chunks x ~0.47 each). For an archival
encoder whose entire purpose is size-efficiency at a controlled quality floor, that is a bad default
-- it directly undoes what the TQ search exists to do.

### Verdict: rejected

The worst-window/straddle early-out is not an accuracy-neutral speed win; it is a speed-vs-size
knob. As a default it is harmful (~15% larger files for imperceptible over-quality). As an explicit
opt-in "fast" mode it is quality-safe but low value for this encoder, and the user has not asked for
a speed/size tradeoff. Killed for the price of a script -- no code change, no encode. The genuine
remaining lever for the probe tail is *probe-sample measurement noise* (the 39% mean-bracket group
is noise-limited, not search-limited); reducing it needs more sample frames per probe (more GPU
per probe), which is a real-encode experiment, not a free optimization.

### Change

None. Third plausible search-layer idea (after content-prior and flat-low) rejected by simulation
before any build. The probe tail is real but has no free lunch: probe count cannot be cut without
either larger files or noisier measurements.

## 2026-06-14 Target band WIDTH is the real probe-tail lever (and a quality-goal review)

After three search-layer ideas were rejected for having no free lunch, the right question turned
out to be upstream: what CVVDP target range does reel actually need? Reel encodes libraries
(hundreds of titles) for Jellyfin streaming viewed at normal distances on TVs/tablets/phones --
not archival/reference quality. The point of target-quality is consistency + better-than-fixed-CRF
at a speed that scales, so the band should be only as tight as the use case needs.

### The band width, not the search, controls the probe tail

Simulated candidate bands over the recorded feature-length Sully probes: for each chunk, the first
probe that would converge under band [c-w, c+w] (mean in band AND worst_window >= c-w). Script
`/tmp/tol_sweep.py`. The current default band is `9.25-9.52` (center 9.385, half-width +-0.135).

| band | convergence | probes/chunk | maxed | quality landing (sampled) |
|---|---|---|---|---|
| 9.25-9.52 (current, +-0.135) | 63% | 3.11 | 37% | mean 9.49, p10 9.32 |
| 9.20-9.55 (+-0.175) | 99% | 1.82 | 1% | mean 9.37, p10 9.22 |
| 9.15-9.55 (+-0.20) | 100% | 1.51 | 0% | mean 9.33, p10 9.19, none < 9.0 |
| 9.10-9.60 (+-0.25) | 100% | 1.35 | 0% | mean 9.34, p10 9.18, none < 9.0 |

Widening the half-width from 0.135 to 0.20 cuts feature-length probe work ~51% (3.11 -> 1.51) and
takes convergence 63% -> 100%. The cause is exactly the monotonicity_guard diagnostic: probe
measurement noise is ~0.075 JOD and the half-width was 0.135 -- barely 1.8x the noise, so probes
kept landing just outside the band and marching to the guard. At +-0.20 (2.6x noise) they land
in-band on the first or second probe. This dwarfs the 12.7% oracle ceiling the search-layer tweaks
could not even reach -- the probe tail was a too-tight-band artifact all along.

Bonus: a wider band also *reduces* the 30% overshoot tail (chunks now converge instead of being
forced to the conservative overshoot pick), so output is slightly smaller, not larger.

### Why this does not cost streaming quality

CVVDP JOD is anchored at 10 = indistinguishable from source, calibrated so ~1 JOD ~= 75% of
observers pick the reference in a side-by-side 2AFC. Streaming has no reference on screen, so the
2AFC-calibrated JOD is far stricter than no-reference viewing: ~9.0+ is effectively transparent in
normal viewing, 9.25-9.5 is high quality with margin. The current center 9.385 is already
demanding, and reel's display model (55" 4K at 1.3 m ~= 76 px/deg, near the acuity ceiling) is a
conservative viewing calibration on top of that. Widening to `9.15-9.55` keeps the center at ~9.35
(quality intent unchanged) and the floor at 9.15 sampled (and the sampled score is the conservative
mean/worst midpoint) -- still well inside transparent-for-streaming, and far more consistent than
fixed CRF.

### Recommendation

Default band `9.15-9.55` (center 9.35, +-0.20): ~51% less probe work at feature length, no quality
center change, smaller files, nothing below 9.0 in simulation. `9.10-9.60` if more speed is wanted;
lowering the *center* toward ~9.2 is a separate, more aggressive size lever to validate only if file
size becomes a priority. The principle to record: **band width is a speed knob, not just an accuracy
setting -- a band tighter than ~2x probe noise (~0.15 half-width) buys consistency you cannot measure
and pays for it in probes.**

### Confirming feature-length encode + ground truth (2026-06-14)

Re-ran the full 96-min Sully 4K encode at `9.15-9.55` and validated with `scripts/fullvalidate`
(full-chunk CVVDP of the final output). Measured against the baseline `9.25-9.52` run:

| metric | baseline 9.25-9.52 | new 9.15-9.55 |
|---|---|---|
| probes/chunk | 3.11 | **1.46** (-53%) |
| convergence | 63% | **99%** (665/670; 1 guard, 4 bounds_crossed) |
| wall time | 4h2m | **1h57m** (-51%) |
| output size | 2.9 GB | **1.2 GB** |

The probe and wall-time wins match the simulation (predicted 1.51 probes/chunk). The large size drop
is the intended removal of the old narrow band's ~30% overshoot tail -- it was over-spending bitrate
for imperceptible over-quality.

Ground truth (true full-chunk CVVDP of the new output): **mean 9.411, median 9.424, p10 9.257,
min 8.689, max 9.895.** Only **3/670 chunks below the 9.15 floor**; mean abs error vs target 0.104.
Sampling reliability: the sampled scores deviated from true by only **0.026 JOD mean** -- the 3x48
window strategy holds up at the wider band. So the encoder lands where it believes, and where it
believes is high-quality for streaming (true median 9.42, ~1.4 JOD better than the no-reference
transparency floor).

The worst three chunks are content/sampling limits, not band artifacts: 0664 true 8.689 (sampled
said 9.679, a -0.99 gap -- a sampling miss on a chunk pushed to the CRF ceiling; the one real
outlier, a single ~12 s segment); 0022 true 8.984 (intrinsically hard -- cannot reach the band at
any CRF, even 4.2); 0647 true 9.136 (a hair under the floor). 0664 is a data point for the existing
"smarter sampling" open item (conditional extra windows on high-spread chunks), independent of the
band width.

### Change

Flipped `DefaultTargetQuality` 9.25-9.52 -> **9.15-9.55** (`internal/config/config.go`), updated
`docs/USAGE.md`, and recorded the reel target-quality purpose + the band-width-as-speed-lever
principle in AGENTS.md. Validated by the feature-length encode + fullvalidate above. Net: ~2x faster
target-quality encoding and ~2.4x smaller output at a true median JOD of 9.42, well inside
transparent-for-streaming. The original kept workdir was recorded at `~/testing/band-confirm/.reel-Sully_t00-1ce039b19801`; it may not be present in the current local `~/testing/` tree.

## 2026-06-14 HDR display peak luminance 1000 vs 1500 (reviewed, tested, reverted)

**Status: REJECTED / PARTLY CONTAMINATED / ARCHIVED.** The scary 1500-nit
result in the original entry (8/33 chunks below band) was the scoring cascade,
not a real highlight-sampling failure. The 2026-06-18 fixed-ruler re-score of
the same bits moved the 1500-nit run to 0 chunks below band.

Current answer: keep the HDR display peak at 1000. The clean re-score showed no
quality deficit that 1500 fixes, so the change buys no practical upside. The
only surviving sampling concern is the separate, rare Sully feature chunk 0664
max-CRF miss from the target-band confirmation run.

Raw historical detail was moved to
`docs/PERFORMANCE_TESTING_ARCHIVE.md#2026-06-14-hdr-display-peak-luminance-1000-vs-1500-reviewed-tested-reverted`.

## 2026-06-15 High-variance test clip: the probe tail is within-chunk, not chunk-to-chunk

**Status: CONTAMINATED / METHODOLOGY SURVIVES / ARCHIVED.** The headline claim
that the `sullyhv-15m` clip reproduced a shipped-band probe tail was a single
run that hit the scoring cascade. After the allocator fix, the same hard-content
asset converges around 1.55 probes/chunk, like easy content.

Current answer: keep `~/testing/sullyhv-15m-4k-hdr.mkv` as a deterministic
near-floor stress asset and use it with `sully-5m` as the easy control. Do not
cite the old 2.78 probes/chunk result as real current-band behavior.

Raw historical detail was moved to
`docs/PERFORMANCE_TESTING_ARCHIVE.md#2026-06-15-high-variance-test-clip-the-probe-tail-is-within-chunk-not-chunk-to-chunk`.

## 2026-06-15 Cascade root cause + FIX: concurrent VSHIP CVVDP handlers corrupt GPU scoring

**Status: SUPERSEDED WORKAROUND / DIAGNOSTIC SURVIVES / ARCHIVED.** This entry
found the scoring cascade and installed the temporary single-handler
serialization workaround. The final 2026-06-19 root cause corrected the
attribution: the bug was the libvship `cudaMallocAsync` device-global allocator
pool, not an inherent VSHIP design limit. N concurrent handlers are restored on a
`MITIGATE_MALLOC_ASYNC` libvship build.

Current answer: require the MITIGATE libvship build, keep one VSHIP handler per
metric worker, and use `scripts/handlertest` after libvship/GPU/driver changes.
The durable lessons are the cascade fingerprint, the diagnostic method, and the
latent floor/seed amplifiers.

Raw historical detail was moved to
`docs/PERFORMANCE_TESTING_ARCHIVE.md#2026-06-15-cascade-root-cause--fix-concurrent-vship-cvvdp-handlers-corrupt-gpu-scoring`.

## 2026-06-18 Re-baseline accuracy ground truth on the fixed binary

First clean accuracy pass since the 2026-06-16 scoring-cascade fix (one shared VSHIP handler +
`quality.gpuMu`; see "Cascade root cause + FIX"). Every pre-fix `fullvalidate` used the buggy
N-handler ruler, so this re-establishes a trustworthy baseline for the *current shipped config*
(preset 6, band 9.15-9.55, default knobs) and re-confirms the two suspect findings the fix flagged.
Machine: dev box (RTX 5060 Ti, 32 cores). Binary: fresh build at HEAD 18e351a. Ruler:
`scripts/fullvalidate` (shared handler); the 1500-nit HDR re-score used a throwaway `fullvalidate`
variant with an optional display-model override arg (not retained -- reproduce by passing a 1500-nit
model JSON to `quality.EnsureDisplayModel`). All GPU steps strictly serial. Total 239 min.

### Fresh baselines (preset 6, band 9.15-9.55, fixed binary) -- all clean

| clip | tier | chunks | mean | min | below | above | sampled gap | probes/ch | floored |
|------|------|-------:|-----:|----:|------:|------:|------------:|----------:|--------:|
| im-5m | 1080p SDR | 32 | 9.452 | 9.222 | 0 | 2 | 0.027 | 1.28 | 0 |
| sully-5m | 4K HDR | 33 | 9.410 | 9.179 | 0 | 1 | 0.023 | 1.36 | 0 |
| kbv1-5m | 4K HDR (grainiest) | 37 | 9.387 | 9.198 | 0 | 0 | 0.008 | 1.22 | 0 |
| ko-5m | 4K HDR | 30 | 9.459 | 9.190 | 0 | 6 | 0.047 | 1.40 | 0 |
| sullyhv-15m | 4K HDR (near-floor stress) | 110 | 9.433 | 9.169 | 0 | 17 | 0.033 | 1.54 | 0 |

Zero chunks below band, zero CRF-floored, 100% converged on every clip; no false sub-floor
worst-window anywhere. `sullyhv-15m` -- the asset that cascaded ~40% of pre-fix runs (0-27 floored,
9.2x size swings) -- now lands at 1.54 probes/chunk / 0 floored, matching the post-fix 1.55
reference. The grain stress (kbv1, CRFs 22-29) is the tightest of all (gap 0.008, 0/37 out of band):
grain forces low CRF, so there is always bitrate headroom above the floor.

### Suspect-finding re-confirmations (re-score the same bits, fixed ruler)

Re-scoring the *same encoded IVFs* with the fixed ruler isolates the handler fix from the encode.
The two flagged sightings split cleanly:

- **Sully band-confirm chunk 0664: REAL (not cascade).** Re-scoring the 670-chunk feature workdir is
  **byte-identical** to the old buggy-ruler run (mean 9.4110, below 3, above 47, gap 0.0263; 0664
  full 8.6891 vs sampled 9.6794, gap -0.99 -- unchanged to 4 decimals). So (a) that ground-truth run
  was never cascade-contaminated (the fix would have changed it), and (b) 0664 is a genuine
  representativeness gap. Mechanism: 0664 is a 288-frame *sampled* chunk pushed to the **max CRF
  (63.75)** because all three 3x48 windows scored ~9.67-9.70; a hard sub-segment between the windows
  is starved at min bitrate and the sampling misses it. Rare (1/670) and max-CRF-specific -- grainy/
  low-CRF content never reaches it.
- **HDR display-peak "8/33 below band": CASCADE (dissolves on the fixed ruler).** Re-scoring the
  reverted 1500-nit test workdir under the 1500-nit ruler with the fixed handler: new-1500 goes from
  mean 9.345 / **8 below** / gap 0.109 (old) to mean 9.448 / **0 below** / gap 0.040 (fixed); the
  catastrophic chunks (0017 full 9.09 vs sampled 9.55 = -0.45; 0011 -0.41; 0030 -0.40) all collapse
  to ~0 gap. The baseline-1000 workdir under 1500 is byte-identical old->new (9.467->9.468, 0 below),
  as expected for easy content. So the HDR "sampling representativeness gap" was the scoring bug, not
  under-sampling.

### Conclusions

1. **The shipped config is sound on a trustworthy ruler.** Across 1080p SDR, easy/grainy/mixed 4K
   HDR, and the near-floor stress clip, the default band + knobs produce in-band, cascade-free
   encodes (0 below band, 0 floored everywhere). Confirms rather than re-decides -- the band rests on
   a simulation over verified-clean feature probes and the 256-frame full-probe threshold is
   cascade-immune.
2. **The "smarter sampling on under-represented sub-segments" item survives but narrows.** Its
   HDR-test evidence was cascade (now void); its band-confirm 0664 evidence is real but is a single
   rare max-CRF sighting. Still low priority. If ever pursued, the cheap remedy (one window on the
   chunk's hardest segment via the per-frame luma shot detection already computes) targets exactly
   the 0664 failure mode.
3. **The HDR revert-to-1000 decision stands, but for a simpler reason.** The alarming "1500 makes
   8/33 chunks fall below band" was the bug; on the fixed ruler the 1500-trained encode is also clean
   (0 below). 1000 was kept because the change bought no real quality and isn't needed -- unaffected;
   only the scary number was an artifact.

Artifacts: `~/testing/rebaseline-20260617/` (per-clip `fullvalidate.txt` + `probestats.txt` + kept
workdirs; `A1-bandconfirm-0664/`, `A2-hdr-baseline1000/`, `A3-hdr1500/`; `STATUS.txt`, `SUMMARY.md`).

## 2026-06-18 Metric serialization was the preset-6 wall bottleneck (motivation for the restore)

**Status: SUPERSEDED / ARCHIVED.** This short entry was the before-restore
baseline: the temporary single-handler workaround made serialized CVVDP scoring
81-91% of wall at preset 6. The serialization tax was removed by the 2026-06-19
MITIGATE allocator fix and restored concurrent handlers.

Current use: motivation only. Use 2026-06-19 "Metric concurrency RESTORED" and
"Post-restore re-attribution" for current performance behavior.

Raw historical detail was moved to
`docs/PERFORMANCE_TESTING_ARCHIVE.md#2026-06-18-metric-serialization-was-the-preset-6-wall-bottleneck-motivation-for-the-restore`.

## 2026-06-19 Metric concurrency RESTORED: the cascade was a libvship allocator bug, not a Vship design limit

Resolves the "Restore safe metric concurrency" open item and corrects the root-cause framing of the
2026-06-15 "Cascade root cause + FIX" entry. That fix (one shared VSHIP handler + `quality.gpuMu`
serializing GPU compute) was a correct *workaround* but mis-attributed the cause to an inherent Vship
limitation ("the price of correctness until VSHIP supports isolated handlers"). The real cause is the
**libvship build**, it is fixable, and N concurrent handlers are back -- the serialization tax (81-91%
of preset-6 wall) is gone.

### Method
Throwaway harness `scripts/handlertest`: score a kept workdir's chunks two ways in one process -- one
shared handler serial (truth) vs N distinct handlers concurrent (the pre-fix / xav design) -- comparing
below-band count and run-to-run determinism. A/B'd libvship builds via `LD_LIBRARY_PATH` (no /usr/local
clobber). Artifacts: `~/testing/vship-concurrency/`.

### Controlled builds (sullyhv-15m near-floor, 4 handlers)
| libvship build | optimization | allocator | concurrent result |
|---|---|---|---|
| installed (`buildcuda`) | `-g`, none | cudaMallocAsync | cascades: 30 below band, min 7.66, run-to-run delta 2.0 |
| `buildcuda` @HEAD | `-g`, none | cudaMallocAsync | cascades: 2/3 reps below band (56, 3) |
| `build` (proper) | `-O3 -DNDEBUG` | cudaMallocAsync | **4/8 reps cascade** -- NOT fixed by -O3 |
| `build` + MITIGATE | `-O3 -DNDEBUG` | sync cudaMalloc | **8/8 reps byte-identical to truth, 0 below** |
| (1080p/8 handlers, async) | `-g` | cudaMallocAsync | milder but present: 1/8 reps 1-below, nondeterministic |

Root cause: each CVVDP handler allocates per-frame device memory via `cudaMallocAsync` from a
DEVICE-GLOBAL stream-ordered pool shared by all handlers; concurrent alloc/free across handlers' streams
races that pool and aliases memory -- corrupting scores even with serialized compute (which is why the
2026-06-15 diagnostic saw N-handlers-serialized still corrupt, only 1-handler clean). `MITIGATE_MALLOC_ASYNC`
swaps the async pool for synchronous `cudaMalloc`/`cudaFree`, removing the shared pool. Vship's CVVDP +
shared-GPU source is byte-identical from the installed commit (7035000) to HEAD (b8e6a4e) -- only ssimu2
changed -- so this is purely build flags + allocator, not a source regression.

### The false turn (recorded as the AGENTS.md discipline lesson)
A 2-rep run of the proper `-O3` build came back clean and I concluded "the bug was just the deprecated
unoptimized `buildcuda` build -- a build misconfiguration." An 8-rep sweep on that *same* build then
cascaded 4/8. At a ~50%-intermittent failure rate a 2-rep sample proves nothing; only replicating enough
to bound the rate exposed that `-O3` *masks* but does not *fix* the race. Hence the testing-discipline
bullet in AGENTS.md.

### Fix (implemented)
- `build_svt_av1_usr_local.sh` (encodescripts): `VSHIP_BUILD_TARGET` `buildcuda` -> `build BACKEND=Cuda
  MITIGATE_MALLOC_ASYNC=on`. The deprecated unoptimized target was building the racy async allocator.
  System libvship rebuilt + reinstalled to /usr/local with the sync allocator.
- reel: restored ONE VSHIP handler PER metric worker, scored concurrently (`internal/encode/target_quality.go`,
  `scripts/fullvalidate`); removed `quality.gpuMu` -- `ComputeChunkCVVDP` is lock-free. Correctness now
  DEPENDS on the MITIGATE-built lib (documented at each call site, enforced by the build script).

### Validation (end-to-end real encodes, installed MITIGATE lib + restored reel, preset 6 default band)
| clip (handlers) | runs | wall | size | floored | min score |
|---|---|---|---|---|---|
| sullyhv-15m 4K (4) | 3 | 1260/1301/1291s | 177/177/178 MB | 0/0/0 | 9.169/9.157/9.185 |
| im-5m 1080p (8) | 2 | 151/154s | 108/110 MB | 0/0 | 9.222/9.222 |

Deterministic (size flat within ~1 MB, 0 floored every run, all in-band) vs the cascade's 0.21->1.91 GB /
0-27-floored swings. **Speed: sullyhv 1260-1301s vs the serialized 1963s baseline = ~1.5x faster wall**, at
both 4- and 8-handler counts. The few-percent run-to-run probe/score/size jitter is the normal adaptive-
search + SVT-AV1 threading noise (~100x smaller than the cascade it replaced), not corruption.

Outcome: **"Restore safe metric concurrency" RESOLVED** -- a one-flag libvship rebuild, not the external
VSHIP isolation work the 2026-06-15 entry anticipated. `scripts/handlertest` kept as a standing
concurrency-safety check to re-run after any libvship/GPU/driver change. The underlying Vship allocator
race is written up as an upstream finding in `docs/VSHIP_CONCURRENCY_BUG.md` (recorded, not yet filed).

## 2026-06-19 Post-restore re-attribution + metric-worker sweep: 1080p GPU-bound, 4K encoder-bound, below-UHD workers 8->6

The metric-concurrency restore changed the regime (CVVDP scoring parallel again), so the bottleneck
attribution and the 8/4 metric-worker defaults -- both set on the pre-restore / async-allocator build --
needed re-measuring on the shipping sync-allocator build. One sweep answers both: re-attribution falls out
of the default-worker runs, the rest is the saturation curve.

### Method
Harness `~/testing/perf-ab/post-restore/` (`orchestrate.sh`, `run-one.sh`, `analyze.py`). Strictly
sequential (one GPU process at a time -- the shared-allocator invariant), two interleaved rounds. Per run:
wall (external), GPU utilization + VRAM sampled every 2s via `nvidia-smi`, and the kept
`target-quality.json` (which carries per-probe `encode_seconds`/`metric_seconds` and per-chunk
`final_encode_seconds` -- the full encode-vs-metric work split, no verbose log needed). Clips: `im-5m`
(1080p SDR, default mw8), `sully-5m-4k-hdr` (4K HDR, default mw4). Sweep mw4-12 (1080p) / mw3-6 (4K), n=2.
Reel `0442dfa` (restored N-handler build, MITIGATE libvship). All 18 runs rc=0; output sizes deterministic
(4K pinned 57.8-57.9 MB, 1080p within ~3%) -- the fixed allocator confirmed clean end-to-end under the sweep.

### 1080p is GPU-CVVDP-throughput bound
| im-5m mw | 4 | 6 | 8 | 10 | 12 |
|---|---|---|---|---|---|
| wall (s, n=2) | 156 | 152 | 152 | 153 | 154 |
| peak VRAM (GB) | 3.0 | 4.1 | 5.2 | 5.9 | 7.8 |
| GPU p90 | 96 | 96 | 96 | 96 | 97 |

Wall is **flat mw4->mw12** while VRAM climbs linearly and the GPU stays pinned (p90 96-97%). At mw8 the
metric lane is 926s of work; metric wall-equivalent `Sigma_metric/8` = 116s = **76% of the 152s wall**. The
encode lane (472s = 354s probe-window + 118s final) parallelizes across the full worker ramp to a tiny
per-chunk wall, so it is not co-limiting. A single GPU saturates CVVDP scoring at ~4 workers; extra workers
just make each score slower (Sigma_metric grows 515s->1324s as mw 4->12) and burn VRAM. **Conclusion:
1080p is GPU-bound; metric workers past ~4 buy no throughput, only VRAM headroom.**

### 4K is encoder-bound -- and the GPU now visibly sits idle
| sully-5m mw | 3 | 4 | 5 | 6 |
|---|---|---|---|---|
| wall (s, n=2) | 464 | 446 | 444 | 436 |
| GPU util mean | 34 | 37 | 35 | 37 |
| peak VRAM (GB) | 4.1-4.5 | 5.5-5.9 | 6.9-7.0 | 7.9-8.0 |

GPU **mean util only ~35%** (p90 ~85%), encode lane **1.44x** the metric lane (1321s vs 915s), and more
metric workers barely move wall (mw3->mw6 = 464->436s, ~6%). The GPU is starved waiting on the SVT-AV1
encoder. This confirms the prior "4K bandwidth-bound on the encoder" framing (active 4K encodes capped at
~5 via `maxWorkers/6`, RAM tens of GiB free) with the smoking gun: 65% GPU idle. **Conclusion: 4K is
encoder-bound; the throughput lever is the encoder (preset, encode-concurrency), not metric workers.**

### Change kept
`DefaultMetricWorkersBelowUHD` 8 -> **6** (`internal/config/config.go`; UHD stays 4). 6 sits a margin above
the ~4-worker GPU saturation knee (safety for higher-variance content) while shedding ~1 GB VRAM vs 8 at
zero wall cost. Help text and `config_test.go` reference the constant symbolically, so they follow.
`docs/USAGE.md` default corrected (it wrongly said `4`). 4K untouched. The bottleneck section of
`docs/PERFORMANCE_TESTING.md` updated from inferred to measured.

### What this redirects
Metric-worker re-tuning is **not** a throughput win (1080p VRAM-only, 4K negligible) -- it rules out that
avenue cheaply. With 4K confirmed encoder-bound and the GPU 65% idle, the remaining throughput levers are
encoder-side: the preset sweep (a faster encoder converts ~directly to 4K wall) and the 4K
encode-concurrency ceiling. These are now the top open items.

## 2026-06-20 Preset sweep 4/5/6/7/8: preset 6 confirmed optimal for BOTH 1080p and 4K (default unchanged)

The post-restore re-attribution (2026-06-19) showed 4K is encoder-bound with the GPU ~65% idle, so a faster
SVT-AV1 preset should convert almost directly to 4K wall -- making preset the clearest remaining throughput
lever. The user's encode default is preset 6. The open question, and the user's standing hunch, was whether
the optimal preset differs by resolution (4K is encoder-bound so preset moves its wall; 1080p is GPU-bound
so it should not). User-approved, expanded from a 6->7 A/B to a full 4-8 curve to see the whole picture.

### Method
Harness `~/testing/perf-ab/preset-ab/` (`orchestrate.sh`, `run-one.sh`, `analyze.py`). Strictly sequential
(one GPU process at a time). Per run: timed encode (wall external, GPU sampled), then `fullvalidate` on the
KEPT workdir for ground-truth full-chunk CVVDP -- run serially AFTER the encode so only one GPU process is
ever live. reel is target-quality, so every preset aims at the same `9.15-9.55` JOD band; preset shows up as
wall (the gain) vs output size at held quality (the efficiency cost), with `fullvalidate` proving the band +
worst-window floor hold. Clips: `im-5m-1080p-sdr` (GPU-bound control), `sully-5m-4k-hdr` (clean 4K,
encoder-bound), `kbv1-5m-4k-hdr` (grainy 4K -- grain is where fast presets blow up size worst). Presets 8->4
(fast first), 2 rounds. Reel `151d6b1` rebuilt at HEAD (mw6 below-UHD / mw4 UHD default). All 30 runs rc=0;
rounds a/b reproduce within 1-3s wall and a few MB size (deterministic encoder + MITIGATE allocator).

### 1080p (im-5m) -- flat, as the GPU-bound prediction requires
| preset | 4 | 5 | 6 | 7 | 8 |
|---|---|---|---|---|---|
| wall (s) | 160 | 156 | 152 | 152 | 148 |
| vs p6 wall | -5% (slower) | -3% | -- | +0% | +3% (faster) |
| size (MB) | 107 | 112 | 115 | 112 | 118 |
| vs p6 size | -7% | -3% | -- | -3% | +3% |
| full_mean / full_min | 9.434 / 9.152 | 9.440 / 9.192 | 9.450 / 9.222 | 9.425 / 9.176 | 9.415 / 9.208 |
| below-band | 0 | 0 | 0 | 0 | 0 |

The entire 4-8 range moves the wall only **+-4%** (148-160s over a 152s baseline) -- confirms GPU-bound, the
encoder is not the wall so preset barely touches it. Size is non-monotone within ~3-7 MB of noise. Faster
(7/8) buys ~3% wall but costs size and a touch of quality; slower (4/5) costs wall for a marginal, noisy
size gain with no quality improvement (full_min actually drops at p4). **Nothing meaningful on the table
either direction at 1080p.**

### 4K (sully clean + kbv1 grainy) -- steep, as the encoder-bound prediction requires; but faster is a bad bit-trade
| sully-5m clean | 4 | 5 | 6 | 7 | 8 |
|---|---|---|---|---|---|
| wall (s) | 542 | 483 | 444 | 431 | 389 |
| vs p6 wall | -22% | -9% | -- | +3% | **+12% (faster)** |
| size (MB) | 55.3 | 56.4 | 57.9 | 63.0 | 67.1 |
| vs p6 size | -4% | -2% | -- | **+9%** | **+16%** |
| full_min / below | 9.233 / 0 | 9.195 / 0 | 9.179 / 0 | 9.213 / 0 | **9.024 / 1** |

| kbv1-5m grainy | 4 | 5 | 6 | 7 | 8 |
|---|---|---|---|---|---|
| wall (s) | 456 | 440 | 420 | 408 | 366 |
| vs p6 wall | -9% | -5% | -- | +3% | **+13% (faster)** |
| size (MB) | 153.0 | 154.7 | 163.5 | 172.9 | 181.3 |
| vs p6 size | -6% | -5% | -- | **+6%** | **+11%** |
| full_min / below | 9.157 / 0 | 9.179 / 0 | 9.202 / 0 | 9.213 / 0 | 9.186 / 0 |

Preset *does* move the 4K wall (encoder-bound, as predicted) -- but the bit cost is steep and superlinear,
and the user's upfront caveat ("not a free tradeoff, encoder efficiency drops") decides it:
- **Faster is a bad deal.** p7 costs **+6-9% size for only +3% wall**; p8 costs **+11-16% size for +12-13%
  wall**, and on clean 4K p8 **breaks the worst-window floor** (full_min 9.024, 1 chunk below band). You
  trade a one-time ~13% encode speedup for a *permanent* +11-16% on every stored 4K file -- backwards for a
  storage-bound library where files persist and encode time is paid once.
- **Slower does not pay either.** p6->p5/p4 saves only 2-6% size for 9-22% more wall. The size-vs-preset
  curve has a clean **knee at preset 6**: sully size runs 67->63->**58**->56->55 (big drops down to p6, then
  flat); kbv1 181->173->**163**->155->153 likewise. Below 6 you pay lots of wall for almost no bits.
- Grain note: kbv1's files are ~3x sully's (grain costs bits), and the +11% p8 penalty is ~+18 MB absolute.
  Interestingly the band breach showed on the *clean* clip (sully p8), not grainy -- a single hard scene in
  sully, not a grain effect.

### Conclusion -- keep preset 6 for both; do NOT make it resolution-aware
The bottleneck differs by resolution exactly as predicted (1080p flat/GPU-bound, 4K steep/encoder-bound),
but the **optimal preset does not**: preset 6 is the joint optimum. At 1080p the curve is too flat to care;
at 4K preset 6 sits on the efficiency knee where faster presets start costing far more bits than wall (and
p8 breaks quality). The resolution-aware *preset* idea was reasonable but the data closes it. **No default
change** -- the `DefaultSVTAV1Preset = 6` row stands; this entry is the evidence that 6 is confirmed, not
just inherited. Encoder-side throughput hopes now rest on the 4K encode-concurrency ceiling, not preset.

## 2026-06-20 1080p preset 4 vs 6 across grain tiers: bound-ness is content-dependent, preset 6 stands

The preset 4-8 sweep used one 1080p clip (`im-5m`, moderate grain) and found a near-flat wall curve, fitting
the "1080p is GPU-CVVDP-bound" attribution. To check whether that generalizes -- and whether the slower
preset 4 buys free efficiency on GPU-bound 1080p -- ran preset **4 vs 6** on the three remaining 1080p clips
(air clean-light, soms light, bts moderate/heavier-in-dark). First verified the main sweep's resolution
classification was correct: reel reported `im` 1920x1080 -> mw6, `sully`/`kbv1` 3840x2160 -> mw4, classifying
on *input* width (`width>=3840`, before crop; sully/kbv1 crop to 3840x1600 letterbox but stay UHD).

### Method
Harness `~/testing/perf-ab/preset-1080p-ab/` (clone of the preset-ab harness, new OUTBASE). Strictly
sequential, 2 rounds, each run timed encode + `fullvalidate` ground-truth. Reel `52b3735` (mw6 below-UHD).
All 12 runs rc=0; rounds a/b reproduce within ~1-3% wall (bts p6 had the most scatter, 202/190s, but the
p4-vs-p6 gap dwarfs it). Clips are 5m cuts to match `im-5m`.

### Result -- the p4 wall penalty tracks output bitrate, not resolution
Both-round means, preset 4 vs preset-6 baseline:

| clip | grain | p6 size | p4 wall vs p6 | p4 size vs p6 | p4 full_min | below |
|---|---|---|---|---|---|---|
| air-5m | clean-light | 53 MB | **+1%** (1s slower) | -8% | 9.191 (^ vs 9.164) | 0 |
| im-5m* | moderate | 115 MB | +5% (8s) | -7% | 9.208 | 0 |
| soms-5m | light | 338 MB | +13% (27s) | -3% | 9.230 (^) | 0 |
| bts-5m | moderate/dark | 598 MB | **+38%** (75s) | -7% | 9.225 | 0 |

\*im from the 4-8 sweep entry above. The p4 wall penalty is **monotonic in output size**: +1% -> +5% -> +13%
-> +38% as size goes 53 -> 115 -> 338 -> 598 MB. So **"1080p is GPU-bound" is only true for low-bitrate
1080p**: light content (air, 53 MB) is genuinely GPU-bound and the slower preset is ~free (-8% size, +1%
wall, even slightly better quality); heavy 1080p (bts, 598 MB grain-in-dark) is **encoder-bound like 4K**,
where the slower preset costs +38% wall. bts also needs more probes at p4 (1.10 -> 1.43/chunk), compounding
the cost. Quality held everywhere (0 below band, full_min >= 9.164; p4 often marginally *higher* mean).

### Conclusion -- keep preset 6 (no change)
A slower 1080p default pays only on light content, which is exactly where the absolute size saving is
smallest (~4 MB off 53 MB). The trade worsens as content gets heavier (the wall cost is worst precisely
where bitrate is highest -- an unfavorable correlation) while the size gain stays modest (3-8%). Reel cannot
predict per-title which regime a clip is in, so a static preset-4 1080p default would lightly help air-like
titles and heavily punish bts-like ones. **Preset 6 stands across all resolutions and grain tiers** -- no
resolution-aware preset split in either direction. This also refines the "1080p GPU-bound" framing in
`PERFORMANCE_TESTING.md`: the bound depends on content bitrate, not resolution alone.

## 2026-06-21 4K encode-concurrency ceiling + lp retest on the fixed MITIGATE build

**Status: CURRENT / NO DEFAULT CHANGE.** Resolves the medium open item to re-check the
4K `maxWorkers/6` encode-concurrency ceiling and `level_of_parallelism` after the
libvship `MITIGATE_MALLOC_ASYNC` restore.

Environment: dev box (`white`), AMD Ryzen 9 7950X (32 logical CPUs), 61 GiB RAM,
RTX 5060 Ti 16 GB (driver 610.43.02). Reel source `46c7eb3` was copied under
`~/testing/perf-ab/cap-lp-retest/src/` and rebuilt into variant binaries under
`~/testing/perf-ab/cap-lp-retest/bin/`; the main repo was not modified for the
sweep. Variants changed only `uhdCoreDivisor`: cap4/div8, cap5/div6 (current),
cap6/div5, cap8/div4, cap10/div3, and cap16/div2. Target-quality runs used the
current UHD metric-worker default (4) and the current MITIGATE libvship install.
Harness/artifacts: `~/testing/perf-ab/cap-lp-retest/` (`build-variants.sh`,
`run-one.sh`, `orchestrate-crf.sh`, `orchestrate-tq.sh`, per-run logs/GPU logs,
`target-quality.json` snapshots, and `analysis.txt`). Runs were strict sequential
(one Reel/CVVDP process at a time).

### Fixed-CRF encoder-only instrument

Fixed CRF (`--quality-mode crf --crf 32`) removes the target-quality probe cascade
and metric duty-cycle, so it is the clean encoder-throughput instrument. It is
not by itself a sufficient default decision for Reel's target-quality mode, but
it isolates cap/lp effects. All fixed-CRF outputs were byte-identical per clip
across cap/lp variants.

Mean total wall / video-encoding-stage seconds:

| Config | Effective 4K cap / lp | `sully-5m` (n) | `kbv1-5m` (n) |
|--------|-----------------------|----------------|---------------|
| cap4 auto | 4 / lp3 | 235.5 / 179.2 (2) | 234.0 / 180.6 (2) |
| cap5 forced lp2 | 5 / lp2 | 227.0 / 170.2 (2) | 219.0 / 162.7 (2) |
| cap5 auto (current) | 5 / lp3 | 220.0 / 162.9 (2) | 213.5 / 158.9 (2) |
| cap6 auto | 6 / lp2 | 213.0 / 155.7 (2) | 206.0 / 152.9 (2) |
| cap6 forced lp3 | 6 / lp3 | 209.5 / 151.9 (2) | 202.5 / 148.9 (2) |
| cap8 auto | 8 / lp2 | 201.5 / 144.7 (2) | 197.0 / 139.9 (2) |
| cap10 auto | 10 / lp2 | 202.0 / 145.2 (1) | 195.0 / 140.9 (1) |
| cap16 auto | ramped to 11 / lp2 | 203.0 / 145.9 (1) | 195.0 / 141.3 (1) |

Findings from the clean encoder instrument:

- Fixed-CRF throughput improves from cap5 to cap8 and then plateaus. The old
  "higher always regresses" timing does not hold for whole-chunk CRF encodes on
  this current build.
- lp3 still beats lp2 at low 4K caps: cap5 lp3 is ~4-5% faster than cap5 lp2;
  cap6 lp3 is ~2-3% faster than cap6 lp2. This re-confirms the current lp3
  choice for the shipped cap5 path.

### Target-quality full-pipeline confirmation

Target-quality is the shipping workload, and it answers whether the fixed-CRF
throughput gain converts to Reel wall time once sampled probes, CVVDP scoring,
prior timing, and final encodes interact. Two clips, two interleaved rounds,
configs cap5 auto, cap6 auto, cap6 forced lp3, cap8 auto, cap10 auto.

Mean total wall / video-encoding-stage seconds, plus probe count and aggregate
encode-lane work (`sum(probe encode_seconds) + sum(final_encode_seconds)`):

| Config | `sully-5m` wall/video | `sully` probes / enc-lane | `kbv1-5m` wall/video | `kbv1` probes / enc-lane |
|--------|----------------------:|--------------------------:|---------------------:|-------------------------:|
| cap5 auto (current, lp3) | 431.0 / 373.6 | 45 / 1278s | **409.5 / 355.3** | 44 / 1084s |
| cap6 auto (lp2) | 429.5 / 372.4 | 45 / 1433s | 418.0 / 364.1 | 44 / 1207s |
| cap6 forced lp3 | **428.0 / 370.7** | 45 / 1374s | 411.5 / 357.8 | 44 / 1128s |
| cap8 auto (lp2) | 438.0 / 380.4 | 45 / 1589s | 420.0 / 366.7 | 45 / 1345s |
| cap10 auto (lp2) | 438.0 / 381.1 | 45 / 1684s | 423.0 / 369.8 | 45 / 1422s |

TQ summaries:

- `sully-5m`: 33 chunks, 45 probes (1.36/chunk), probe histogram
  `{1:23, 2:9, 4:1}`, all stops `converged`. Sampled JOD across variants stayed
  in-band: min 9.179, mean 9.387-9.392, max 9.548; mean absolute error to the
  9.35 target 0.091-0.099. Window-spread p90/max 0.1595/0.4667; one chunk hit
  4 probes.
- `kbv1-5m`: 37 chunks, 44-46 probes (1.19-1.24/chunk), all stops `converged`.
  The cap5/cap6 runs used histogram `{1:34, 3:2, 4:1}`; cap8/cap10 sometimes
  added one more 3-probe chunk. Sampled JOD stayed in-band: min 9.172-9.202,
  mean 9.371-9.381, max 9.546; mean absolute error 0.080-0.087. Window-spread
  p90/max 0.0/0.224; 3-4 chunks had >=3 probes.
- GPU mean utilization stayed low at ~35-38% and peak VRAM stayed around
  5.3-5.9 GiB, matching the existing 4K encoder-bound attribution.

Interpretation:

- The fixed-CRF cap8 gain does **not** convert to target-quality wall. In TQ,
  higher caps make individual short probe/final encodes slower enough that the
  aggregate encode lane grows sharply (cap5 -> cap8: +24% sully, +24% kbv1;
  cap5 -> cap10: +32% sully, +31% kbv1). The extra active encodes only hide part
  of that work and cap8/cap10 end up slower wall-clock.
- cap6 forced-lp3 is a statistical tie with current cap5: +3s on sully, -2s on
  kbv1, a two-clip average difference under 1s. That is not enough evidence to
  change the default, especially because cap8/cap10 show the curve turning the
  wrong way immediately above it.
- If a future change raises the 4K cap to 6, it should probably also revisit the
  lp mapping (cap6 lp3 beat cap6 lp2 in fixed-CRF and TQ), but this retest does
  not justify raising the cap.

Conclusion: **keep the current defaults** -- 4K target ceiling `maxWorkers/6`
(start at that ceiling) and 4K auto lp3 on this 32-logical-core dev box. No code
change and no fullvalidate were run; this was a performance-only retest and the
final decision preserves the shipped behavior.

## 2026-06-21 Structured phase + worker timing artifact (`perf.json`)

**Status: CURRENT / METHODOLOGY.** Implements the top High open item in
`docs/PERFORMANCE_TESTING.md`: a structured timing artifact so attribution no
longer depends on grepping verbose logs and throwaway scripts. This is
infrastructure, not a tuning conclusion -- no default changed and the encoded
bits are unaffected (no fullvalidate needed).

What landed:

- New leaf package `internal/perf` with a concurrency-safe `Collector` that
  records per-phase wall windows and a sampled adaptive-scheduler history, then
  writes `perf.json` into the work directory.
- Phases timed via a new `startPhase` helper that wraps the existing
  `startVerboseStep` verbose lines: video property analysis, HDR analysis, audio
  analysis, video probe, crop detection, shot cut detection, chunk planning,
  resume setup, audio extraction, video encoding (TQ or fixed-CRF), video merge,
  final mux, output validation, and the two post-encode stream-size scans (input
  and output, timed separately because the input scan dominates). Phases carry
  `start_seconds` + `duration_seconds` relative to run start and may overlap
  (audio extraction runs concurrently with encode/merge), so the phase sum can
  exceed `total_seconds`.
- Worker history sampled from the existing encode progress callback:
  active/target/max workers, in-flight chunks, chunks/frames complete, and
  cumulative encode-slot wait time. `worker.Progress` gained `InFlight` and
  `EncodeSlotWaitSeconds`; the adaptive limiter now accumulates blocked-acquire
  time (`slotWaitNanos`, read lock-free); the TQ `inFlight` counter became an
  `atomic.Int64` so snapshots read it without the dispatch lock. Samples are
  throttled (kept on any worker-count change, else >=2s apart) to bound size.
- The "Audio extraction" phase is recorded from inside the audio goroutine when
  extraction actually returns, not at the orchestration join. Audio runs
  concurrently with the whole video encode/merge, and the join is only reached
  after merge, so a join-stamped duration over-reported audio cost by the entire
  encode window. The verbose start/stop log lines are unchanged (still bracket
  the join); only the perf phase reflects real audio work time.
- `perf.json` is written wherever the work directory will be kept: after a
  successful encode with `--keep-workdir`, and after a failed encode whose
  workdir survives for resume (output not produced). Failure-path timing up to
  the failure is exactly what attribution wants, and matters for the
  hundreds-of-titles batch where resumable failures occur. Always-known worker
  metadata (metric workers, max adaptive workers) is set in the orchestrator so
  it survives even an early failure before chunk planning.

Verification: full `./check-ci.sh` passes (tests, `-race`, build, golangci-lint
0 issues, govulncheck); a focused unit test asserts the limiter charges positive
slot-wait only when an acquire actually blocks. A three-lens adversarial review
(concurrency, completeness, simplicity) confirmed the atomic/cond invariant and
drove the audio-phase and failure-path fixes above. Real-encode spot-check on a
30s 1080p clip (`--keep-workdir`) both modes: fixed-CRF and target-quality
produced well-formed `perf.json` (`schema_version: 1`) with all 15 phases in
order, full metadata, and worker history. Confirmed on the artifact: the audio
phase reads ~0.58s (real work) rather than the ~16s video-encode window, and the
target-quality run shows `in_flight > active` (peak `in_flight=3, active=1` --
chunks scoring with their encode slots released). `encode_slot_wait` stayed 0 on
this tiny clip (4 chunks < 8 slots = no contention; it accumulates only under a
saturated probe/score duty cycle on longer content).

Next: the second High open item (reusable perf suite under `~/testing`) can now
consume `perf.json` for phase attribution instead of bespoke log parsing.

## 2026-06-21 Reusable perf suite under `scripts/perf/`

**Status: CURRENT / METHODOLOGY.** Closes the second High open item. Infrastructure,
not a tuning conclusion -- no default changed, no encoded bits affected.

First established the repo/testing boundary (commit `5584485`): the clip matrix
manifest (`scripts/perf/clips.tsv`) and corpus knowledge (`docs/PERF_CORPUS.md`)
live in the repo; clip bytes and run outputs stay under `$REEL_TESTING_DIR`
(default `~/testing`). Then added the harness:

- `scripts/perf/run-suite.sh` -- runs reel over a clip set strictly sequentially
  (single-GPU CVVDP allocator invariant), into a timestamped run dir. Captures
  `run-meta.json` (git commit/dirty, reel sha + version, SVT-AV1 version, GPU
  name/driver, libvship path), and per clip: wall, output size + sha256, GPU
  util/VRAM via a background `nvidia-smi` sampler, and the harvested `perf.json`
  / `target-quality.json`. Reclaims the bulky `.reel-*` workdir after harvest
  unless `--keep-workdirs` (for a later `fullvalidate`). Clips resolve by glob
  under `$REEL_TESTING_DIR`; nothing machine-specific is hardcoded.
- `scripts/perf/analyze.py` -- per-clip summary from `perf.json` (phase timing,
  worker history) + `target-quality.json` (probe histogram, stop reasons, JOD
  min/mean/max + mean-abs-error, encode-vs-metric seconds, final-probe window
  spread p90/max) + the GPU log. Writes `summary.json`.
- `scripts/perf/compare-runs.py` -- run-level A/B (wall, size, probes/chunk,
  JOD). Distinct from `scripts/compare-tq.py`, which stays the per-chunk diff.

The conventions formalize the bespoke `~/testing/perf-ab/*` harnesses (the
`nvidia-smi util,mem` 2s sampler, strict sequencing, TSV + per-run JSON
sidecars); the window-spread metric matches reel's own `finalProbe` so it lines
up with the `TQ window_spread` verbose line.

Verification: smoke-tested end-to-end on a 30s 1080p clip in both fixed-CRF and
target-quality modes -- env capture, GPU/VRAM sampling, artifact harvest, and
the analyze/compare tables all produced sane output (the TQ run showed
`metric_s > encode_s` and `in_flight > active`, as expected). A three-lens
adversarial review (shell robustness, python field-name/semantics, design/YAGNI)
ran 15 agents; all confirmed findings were fixed (GPU-sampler cleanup trap,
missing-arg and numeric-interval guards, final-probe spread parity, and
dead-code/column/field trims). NOT yet run on a full matrix sweep -- the first
real `air-5m`/`im-5m`/`bts-5m`/`sully-5m`/`kbv1-5m`/`sullyhv-15m` run is the next
step and will be the first dated suite result.

## Resolved questions (compact index)

Use the status-tagged index at the top of this file to decide which dated entry
is authoritative. Compact current answers:

| Question | Current answer | Authoritative entry |
|----------|----------------|---------------------|
| 1080p preset 4 vs 6 across grain tiers | Preset 6 stands; slower presets only help light content and punish heavy/grainy content. | 2026-06-20 1080p preset 4 vs 6 |
| Preset 4/5/6/7/8 | Preset 6 is the joint wall/size knee; no resolution-aware preset split. | 2026-06-20 Preset sweep |
| Fixed-binary accuracy baseline | Shipped config is clean across tested SDR/HDR clips and `sullyhv`. | 2026-06-18 Re-baseline accuracy |
| GPU-scoring cascade | Root cause is libvship async allocator; fixed by `MITIGATE_MALLOC_ASYNC`; N handlers restored. | 2026-06-19 Metric concurrency RESTORED |
| High-variance clip | `sullyhv-15m` is a deterministic stress asset; old 2.78 probes/chunk result was contaminated. | 2026-06-18 Re-baseline; archived 2026-06-15 entry |
| Worst-window / straddle early-out | Rejected as default: saves probes but increases output size materially. | 2026-06-14 Worst-window / straddle early-out |
| Flat-low early stop | Rejected; all-low population was too small. | 2026-06-14 monotonicity_guard diagnostic |
| Content-prior seed | Rejected; activity-vs-CRF sign flips across clips. | 2026-06-14 Content-prior seed |
| Full-length attribution | Feature-scale encode exposed tight-band probe cost; current wider band resolved the main tail. | 2026-06-14 Full-length attribution; 2026-06-14 Target band WIDTH |
| 4K adaptive ramp / encode cap | 4K target-quality still keeps cap/start at `maxWorkers/6`; fixed-CRF cap8 is faster, but shipping TQ is flat/slower above cap5. | 2026-06-21 cap/lp retest; 2026-06-12 ramp |
| 4K metric workers 4 vs 6 | Keep 4 for UHD; old mw4/mw6 A/B was contaminated, current answer from post-restore sweep. | 2026-06-19 Post-restore sweep |
| `level_of_parallelism` | Auto derives from resolution ramp ceiling; 4K lp 3 on the cap5 dev box remains confirmed. | 2026-06-21 cap/lp retest; 2026-06-13 SVT-AV1 level_of_parallelism |
| Accuracy-trading TQ knobs | Keep shipped knobs; re-test on fixed build before changing threshold/window/chunk cap. | 2026-06-18 Re-baseline; archived 2026-06-13 knobs entry |
| Pre-encode head overlap | Deferred; requires accuracy-affecting streaming planner and low ROI. | 2026-06-14 Overlapping pre-encode head |
| HDR display peak 1000 vs 1500 | Keep 1000; clean re-score showed no 1500 benefit. | 2026-06-18 Re-baseline; archived HDR entry |
