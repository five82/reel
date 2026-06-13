# Performance Testing Notes

This is a living log for Reel performance tuning, especially target-quality (TQ) probing and chunk scheduling. Update it when a real encode or replay changes the evidence behind Reel's defaults.

## Why this exists

Performance work has a long feedback loop: real encodes are slow, many plausible changes only help one clip, and git history alone does not capture why a change was kept or reverted. Keep enough notes here that a future maintainer or coding agent can understand what was tried, what worked, what failed, and what should be tested next.

## How to record a test

For each real encode or replay that informs a tuning decision, record:

- Date, machine, input clip, duration, resolution, HDR/SDR.
- Command or test script used.
- Git commit or working-tree change being tested.
- Total wall time and video encoding time.
- TQ summary: chunks, total probes, probes/chunk, probe-count histogram, stop reasons.
- Accuracy summary: sampled JOD min/mean/max, mean absolute error, outside-tolerance count if available.
- Tail behavior: max-probe chunks, chunks with >=3 probes, p90/max window spread.
- Conclusion: keep, revert, retest, or investigate.

Useful artifacts:

- Human log: `*-reellog*`
- Aggregate TQ log: `.reel-*/target-quality.json`
- Per-chunk TQ logs: `.reel-*/tq/*.json`
- Chunk plan: `.reel-*/chunk-plan.txt` and `.reel-*/chunk-plan.json`

## Current target-quality strategy

As of this document:

- The configured JOD range is accepted literally; the default range is 9.25-9.52, where the 0.02 above the old 9.50 default carries the overshoot headroom that previously lived in a separate upper-grace constant.
- Chunks are scheduled in timeline blocks of 32 and sorted largest-first within each block.
- TQ probes use sampled windows: normally 3x48 frames, with 5 windows on later probes after high spread.
- Chunks at or below the full-probe threshold are probed whole.
- Search uses adaptive CRF priors from completed chunks.
- Search is bracket-aware after the first two probes:
  - linearly interpolate between the two probes bracketing the target once probes bracket it,
  - use a bounded midpoint for unbracketed low probes,
  - move more aggressively toward high CRF for flat, unbracketed high probes only when the highest-CRF probe is still at least 0.30 JOD above target.
- Scoring is mean/worst blended when sampled-window spread is high.
- A worst-window floor prevents convergence when any sampled window falls below tolerance.

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

Goal: understand where wall time actually goes across the whole pipeline before tuning anything else. Method: parsed per-probe `encode_seconds`/`metric_seconds`/`final_encode_seconds` and per-chunk `started_at`/`completed_at` from the 2026-06-12 tq-simplify artifacts, plus stage durations from verbose logs. No new encodes were needed for the attribution itself.

### Where the time went (pre-restructure builds)

`soms-5m-1080p-sdr` (TQ stage wall ~232s, total ~4m13s):

- Metric scoring: 67% of summed chunk busy time. Probe encodes: 30%. Final encodes: 3-4%.
- Average 6.8-7.0 chunks in flight (initial workers 8); average concurrent encodes only ~2.3 because each chunk worker held its encode slot idle while waiting on GPU scoring.
- The TQ stage was already near the GPU floor: ~8.8k scored frames at the benchmarked ~43 fps aggregate CVVDP throughput = ~204s lower bound vs 220-232s observed. At 1080p the GPU metric is the bottleneck, not SVT-AV1.
- Encode amplification 1.41-1.48x (probe frames + final frames vs source frames).

`sully-5m-4k-hdr` (TQ stage wall 916-1074s, total 17-20m):

- Probe encode 48% / metric 45% / final 7% of busy time, but only 2.55-2.74 chunks in flight on a 32-core machine. The adaptive limiter started at 2, ramped to 3, judged the gain "modest" against the lumpy completed-chunks speed signal, and blocked further ramping for the entire run. Both CPU and GPU sat mostly idle.
- Encode amplification 1.82-2.00x: with the 12s TQ chunk cap, most 4K chunks fall at or under the 256-frame full-probe threshold, so nearly every probe is a full-chunk encode (partly paid back by full-probe reuse: 25/33 finals were reused probes).

Stage timings outside the TQ stage (sequential, before/after encoding):

| Stage | soms-5m 1080p | sully-5m 4K | Scaling |
|---|---:|---:|---|
| Video probe | 0.2s | 0.4s | flat |
| Crop detection | 13.1s | 43.5s | linear in duration (141 seeks, 4 workers, 1 decode thread) |
| Shot cut detection | 7.8s | 69.5s | linear in duration (single sequential decode pass) |
| Merge + mux + validation | <0.5s | <0.2s | flat |

For a 2-hour 4K movie the crop + shot-detect head was on the order of 20+ minutes of dead time before the first encode started.

### Bottleneck summary

1. 1080p TQ: GPU CVVDP throughput. Encoding is a minority cost.
2. 4K TQ: chunks-in-flight starvation (slot held during scoring + slow conservative ramp), with GPU becoming the binding constraint once concurrency is fixed.
3. Pre-encode head: sequential crop + shot detection, ~8% (1080p) to ~12% (4K) of total wall on 5-minute clips, worse relatively on long runs since it cannot overlap encoding.
4. Inside each metric task, source/probe decode (1-thread) was serialized frame-by-frame with the GPU compute call.

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

## Open questions / next tests

Search-layer items (carried forward):

1. Keep bracket-aware search, conservative high-side jump gating, and block size 32 unless another clip shows a clear regression.
2. Watch for high-spread chunks like `knives5` chunk `0001` where bracket-aware search can add a probe; consider targeted handling only if this repeats.
3. Consider provisional priors from in-progress chunks only if probe-count tails remain across multiple clips.
4. Do not revisit chunk-boundary complexity unless sampled-window spread or full validation shows a repeatable failure that cannot be addressed in sampling/search.
5. Flat-low-response early stop (mirror of the flat-high gate): metric-insensitive chunks still march the low side at full probe cost (soms chunk 0013-style, 6 probes). Candidate once more clips confirm.

Performance items, in suggested order (use `scripts/fullvalidate` as the accuracy ruler for anything below the line):

6. (Resolved 2026-06-12 -- see "4K adaptive ramp: bandwidth, not capacity" above.) 4K was bandwidth-bound, not capacity-bound; the fix caps and starts 4K at `maxWorkers/6` rather than ramping higher. Remaining sub-item: re-measure the `maxWorkers/6` divisor on non-dev hardware before trusting it there.
7. Retest 4K metric workers 4 vs 6 now that CVVDP tasks overlap decode with GPU compute and chunk concurrency is higher; 6 was the metric-only saturation point and VRAM-marginal at 7-8.
8. Verify SVT-AV1 `level_of_parallelism` is bitstream-identical across values; if so, scale lp with the current worker target instead of the hardware max (lp=2 on a 32-thread machine starves early 4K probes when only 2-4 encoders are active).
9. Accuracy-trading knobs, only with fullvalidate evidence: probe windows 3x48 -> 2x48 or 3x32 (cuts the 1080p GPU floor proportionally), the 256-frame full-probe threshold at 4K (nearly every 4K chunk full-probes under the 12s cap; sampling them would cut GPU work ~40% but loses full-first reuse and whole-chunk scoring), and the 12s TQ chunk cap itself.
10. Overlapping the pre-encode head (crop + shot detection) with the start of encoding is the remaining structural win for long inputs; it needs streaming chunk planning and is not worth it below feature-length inputs.
