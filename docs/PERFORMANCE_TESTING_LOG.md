# Performance Testing Log (historical record)

This is the chronological, append-only record of Reel performance experiments:
what was tried, the measured results, and whether each change was kept or
reverted. It exists so an agent can see *what has already been done* without
re-running it.

For the current settled defaults, the active strategy, the known bottlenecks,
and the open/next-test list, see `docs/PERFORMANCE_TESTING.md` -- that is the
short doc you should read first. This log is the provenance behind it.

When you run a new test, add a dated `## YYYY-MM-DD <title>` entry here (follow
the "How to record a test" checklist in `docs/PERFORMANCE_TESTING.md`), and if
it changes a default, update the matching row in that doc's "Current defaults"
table. Resolved open questions are indexed at the bottom of this log.

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

> **SUPERSEDED by the 2026-06-16 scoring fix** (see "Cascade root cause + FIX" -> "Impact on prior
> findings"). The GPU-throughput speedup from concurrent metric workers measured here came from
> per-handler CUDA streams, which the single-shared-handler fix removes; metric workers now gate
> decode concurrency only. The numbers were real but the scaling curve no longer applies.

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

## 2026-06-13 4K metric workers 4 vs 6 retest

> **CONTAMINATED + now MOOT** (see "Cascade root cause + FIX" -> "Impact on prior findings"). The
> "mw6 took more probes" result was the handler-corruption cascade (6 handlers cascade more than
> 4), misread here as a prior-cascade *timing* confound. Keeping 4 is still fine, but with one
> shared handler the 4-vs-6 question no longer exists (worker count = decode concurrency only).

Goal: open question 7. The 2026-06-12 metric-worker benchmark set the 4K default to 4, noting 6 was the *metric-only* saturation point but did not help the full pipeline because the adaptive encoder only sustained 2-3 active 4K workers. Since then the concurrency restructure (slot release during scoring, async scoring, decode/GPU overlap) plus the 4K bandwidth-aware ramp raised sustained concurrency, and CVVDP now overlaps frame decode with GPU compute. Retest whether 6 metric workers now beats 4 in a real 4K encode.

Environment: same dev machine (32 logical CPUs, RTX 5060 Ti 16 GB VRAM, dual-channel DDR5).

Method: full `reel encode` runs (default preset, autocrop on), `io-5m-4k-hdr` and `sully-5m-4k-hdr`, `--metric-workers 4` vs `--metric-workers 6`, two interleaved rounds each (8 runs). Wall time measured end to end; peak VRAM sampled every 2s via `nvidia-smi`; probe counts from each `target-quality.json`. Harness/artifacts: `~/testing/perf-ab/mw-retest/` (`orchestrate.sh`, `run-one.sh`, `analyze.py`, per-run `.log`/`.vram`/`-tq.json`).

| Run | mw | elapsed | probes | p/chunk | peak VRAM |
|---|---:|---:|---:|---:|---:|
| io a | 4 | 525s | 56 | 1.56 | 8248 MiB |
| io a | 6 | 873s | 82 | 2.28 | 11618 MiB |
| io b | 4 | 591s | 67 | 1.86 | 7960 MiB |
| io b | 6 | 584s | 68 | 1.89 | 11906 MiB |
| sully a | 4 | 666s | 62 | 1.88 | 5976 MiB |
| sully a | 6 | 765s | 72 | 2.18 | 9074 MiB |
| sully b | 4 | 494s | 50 | 1.52 | 5976 MiB |
| sully b | 6 | 643s | 71 | 2.15 | 8946 MiB |

Confound: metric-worker count is not independent of probe count. More metric workers finish scoring sooner, which shifts when chunks complete and therefore the neighbor/median prior cascade, so mw6 runs happened to take more probes. Raw wall time is contaminated by this. Normalize by seconds-per-probe (scoring + encode work per probe):

| | mean s/probe |
|---|---:|
| mw4 | 9.70 |
| mw6 | 9.73 |

Interpretation:

- Throughput-normalized cost is identical (9.70 vs 9.73 s/probe). Extra metric workers add no scoring throughput in the full pipeline.
- Why: progress logs show the 4K encoder sustains `workers 4->5/32` -- target 5, the `maxWorkers/6` bandwidth cap. Concurrency *did* rise from the old 2-3 to ~5, confirming the retest premise, but it is still capped at 5 by memory bandwidth. With ~5 chunks encoding at once, pending scoring tasks rarely exceed 4, so the 5th and 6th metric workers idle. The metric pool is not on the critical path; the bandwidth-bound encoder is.
- Raw wall time favored mw4 on average (io 558 vs 728s, sully 580 vs 704s) and mw6 produced the worst single run (io a, 873s/82 probes): more metric workers can occasionally worsen the prior cascade, never speed the encode.
- VRAM: mw4 peaked ~6-8.2 GiB, mw6 ~9-11.9 GiB. Both safe on 16 GiB, but 6 leaves ~4 GiB headroom vs ~8 GiB -- consistent with the earlier "7-8 marginal" note.

Conclusion: keep the 4K metric-worker default at 4. No code change. The 2026-06-12 recommendation holds under the higher-concurrency pipeline: until sustained 4K encode concurrency rises well above ~5 (which the bandwidth cap prevents on this class of hardware), 4 metric workers already meet scoring demand. Revisit only if the `maxWorkers/6` cap is lifted on higher-bandwidth hardware.

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

> **CONFOUNDED -- re-test the full-probe-threshold A/B before trusting its magnitude** (see
> "Cascade root cause + FIX" -> "Impact on prior findings"). knobB (threshold 144) full-probes
> fewer chunks, so it had far more sampled chunks and thus far more cascade exposure than base
> (256); its "accuracy collapse" is inflated by the scoring bug. The direction (full-probing is
> more accurate, and is cascade-immune) likely holds; the 0.46 JOD penalty number does not.

Goal: open question 9. Three knobs trade probe accuracy for GPU work -- the 256-frame
full-probe threshold, the 3x48 sampled-window size, and the 12s TQ chunk cap. Decide
each only against `scripts/fullvalidate` ground truth, never sampled scores.

### Cheap accounting first (zero GPU)

Per-chunk GPU-frame counts came straight from the existing `target-quality.json`
artifacts (each chunk carries `frames` and per-probe `windows`), so two of the three
knobs were settled without an encode:

- **Window size (3x48 -> 2x48 / 3x32):** the windowed path only touches chunks above
  the threshold, which are a *minority* today (5-9 of 33-37 chunks). Cutting it saves
  just **4-9%** of total GPU frames. Low payoff.
- **12s chunk cap:** the cap binds on **0-2 of 33-37 chunks** across every artifact
  (max observed chunk 275-287 vs the 287-288 cap). Chunking is already scene-driven;
  raising the cap does nothing and lowering it only fragments coherent scenes (an
  efficiency/parallelism axis, not accuracy). Nothing to validate.
- **Full-probe threshold (256 -> 144):** the median chunk is ~200-253 frames, sitting
  right under 256, so nearly every chunk is full-probed. Naive accounting said sampling
  them with 3x48 windows would cut **~19-26%** of GPU frames. This was the only knob
  worth a real fullvalidate A/B.

### Full-probe threshold A/B (the prize -- decisively rejected)

Built two binaries differing only in `DefaultTargetQualityFullProbeFrames` (256 vs 144),
encoded both 4K clips with `--keep-workdir`, scored each with `fullvalidate`. sully is
low-grain digital (high inter-window content variance); kbv1 is the grainiest 4K clip.

| clip  | variant | thr | probes | GPU frames | true-JOD mean | mean_abs_err | above range | sampled_gap |
|-------|---------|-----|--------|-----------|---------------|--------------|-------------|-------------|
| sully | base    | 256 | 65     | 12474     | 9.425         | 0.075        | 5 / 33      | 0.028       |
| sully | knobB   | 144 | 99     | 16205     | 9.683         | **0.302**    | **22 / 33** | **0.463**   |
| kbv1  | base    | 256 | 52     | 8591      | 9.413         | 0.065        | 0 / 37      | 0.010       |
| kbv1  | knobB   | 144 | 59     | 8579      | 9.474         | 0.099        | 8 / 37      | 0.050       |

The naive prediction inverted on both axes:

- **Accuracy collapsed.** sully's sampled scores lied by a mean 0.46 JOD (vs 0.03 at
  full-probe); e.g. chunk 0031 (254f) sampled 8.86 but its true whole-chunk JOD is 9.34.
  Those artificially-low windows push CRF down, so true JOD overshoots target -- 22/33
  sully chunks landed above range, mean drifting 9.43 -> 9.68. kbv1 degraded too
  (0/37 -> 8/37 above range). The damage tracks *content variance*, not grain: low-grain
  sully was the worse case because its medium chunks span dissimilar shots that a 3-window
  sample misrepresents.
- **GPU went up, not down.** sully probes 65 -> 99 and GPU frames 12474 -> 16205 (+30%).
  The accounting assumed a fixed probe count; in reality a noisier sampled score makes
  the search thrash for more probes, eating the per-probe saving and then some. kbv1 was
  roughly flat (52 -> 59 probes) -- no win there either.

This is exactly the "loses full-first reuse and whole-chunk scoring" cost the design
comment names, now measured. Full-probing chunks up to 256 frames is both more accurate
*and* competitive-to-cheaper on GPU because it converges in fewer probes.

### Change

None. All three knobs are well-set; do not touch them. The 256 full-probe threshold is
confirmed correct by ground truth, the window size is too low-payoff to risk further
sample degradation, and the 12s cap effectively never binds. Source tree unchanged
(A/B used throwaway builds with the constant edited, then reverted). Artifacts in
`~/testing/perf-ab/knobB/`.

Methodology note: this repeats the lp-work lesson -- sampled-score accounting and true
quality are coupled through the search's convergence loop, so only `fullvalidate` ground
truth can judge an accuracy-trading knob. The cheap per-frame model is fine for ranking
*candidates* (it correctly flagged B as the only one worth testing) but cannot predict
the outcome.

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

Goal: step back from individual knobs and ask where the remaining performance is, whether
the testing so far has revealed new avenues, and whether any larger design change is worth
it. This is an analysis entry (code reading + existing artifacts), not a new encode.

### State of play

The tuning is mature. Open questions 6-10 are resolved, and the two bottlenecks are pinned
by measurement, not guess:

- **1080p TQ is GPU-CVVDP-throughput bound.** The clip sits at the GPU floor (~204s lower
  bound vs 220s observed; metric ~65% of busy time). Scheduling cannot help further -- the
  GPU scores at a fixed rate.
- **4K TQ is memory-bandwidth bound on SVT-AV1.** Capped at ~5 active encodes
  (`maxWorkers/6`); pushing higher *raised* per-encode time and worsened wall time.

The cross-cutting fact from the accuracy-knob and metric-worker work: **probe count is the
multiplier on both bottlenecks.** Every probe is both an encode and a CVVDP scoring.
Cold-started chunks cost ~2.25 probes vs ~1.34 for neighbor-seeded; cliff chunks (sully
0002/0020/0029) are where the tail lives. Cutting sampling accuracy to save per-probe work
backfires -- noisier scores make the search thrash *more* probes (knob B, 2026-06-13). So the
remaining lever that helps both bottlenecks at once is **fewer probes per chunk without
trading sampling accuracy.** Scheduling/concurrency is largely spent; work-reduction via
better search seeding is the open territory.

### New avenue surfaced by code review: a free per-chunk content prior

Shot detection already computes a per-frame content-activity score for every frame and then
discards it. `scoreVideo` (`internal/chunkplan/chunkplan.go:168`) produces a 0-1 score per
frame (65% pixel diff + 30% histogram + 5% luma; `signatureChange` at `:357`), uses it only
for boundary placement, and keeps only `Boundaries` in `Result` (`:58`). The TQ search,
meanwhile, has *no* content signal: the first probe is seeded purely from neighbor/median
priors of already-completed chunks, falling back to a blind `(CRFMin+CRFMax)/2` midpoint on
cold start (`internal/encode/target_quality.go:104, 952`).

Idea: aggregate the existing per-frame scores into a cheap per-chunk descriptor (mean/max/
variance) and use it to (a) seed the first probe on cold-start chunks, and (b) distrust the
neighbor prior when a chunk's activity is an outlier vs its neighbors -- the cliff-chunk case
where priors mislead and probes blow up.

Why this is the right shape of bet:

- **Accuracy-safe by construction.** A first-probe seed only changes the starting CRF; the
  search still runs to convergence. Worst case it is neutral (no probe saved); it cannot move
  final quality. This sidesteps the fullvalidate-gating burden every accuracy knob carried.
- **Nearly free.** The signal is already computed during mandatory shot detection.
- **Payoff concentrated where the tail is:** cold starts (24% of soms chunks were
  default-seeded) and content-outlier chunks at any length.

Honest caveat: the shot-activity score measures *temporal change between frames*, not the
spatial complexity/grain that most directly drives JOD-at-CRF. The correlation to optimal CRF
is plausible but **unproven**, so it needs a cheap correlation test before any code (see
recommended next steps).

### Larger design changes considered and rejected

- **GPU-encode the probes (NVENC AV1) to dodge the 4K bandwidth cap.** Dead end: NVENC's
  quality-at-CRF does not transfer to SVT-AV1, so the searched CRF would be invalid for the
  final SVT-AV1 encode.
- **Cheaper/proxy perceptual metric to relieve the 1080p GPU floor.** CVVDP is the chosen
  accuracy ruler and sampling is already at the fullvalidate-allowed limit. High risk, against
  every accuracy finding.
- **Cross-*film* learned CRF model.** The within-run prior plus resume-time seeding
  (`seedTargetQualityPrior` from prior `tq/*.json`) already capture the safe version. A global
  cross-content model is large complexity for uncertain transfer.

The one large structural win that survives scrutiny is already scoped and deferred: streaming
the pre-encode head to overlap shot detection with encoding (item 10), worth it only at 4K
feature length.

### Feature-length: untested, but no structural breakage found

Every conclusion in this doc is validated on 5-minute clips. Code review for feature-length
(90-150 min, thousands of chunks) found nothing that breaks: `flightCap`
(`target_quality.go:255`) is bounded by `min(byPriors, target + max(target, metricWorkers))`,
so in-flight is capped at ~2x the worker target regardless of chunk count -- no concurrency
blowup. The only unbounded accumulations are trivial (the `slopes` array and per-chunk JSON
logs -- a few thousand floats / a few MB; `packShortShots` is O(n^2) but on cheap shot data).
The linear extrapolations should hold. The open uncertainty is the *prior cascade*: at feature
length there are hundreds of neighbors, so cold-start fraction approaches zero and probes/chunk
likely drop, which could shift the bottleneck balance in ways 5m data cannot show.

### Recommended next steps (in order)

Two cheap, high-information experiments before any code change:

1. **Correlation test for the content-prior idea** (no encodes). DONE 2026-06-14 -- rejected;
   see "Content-prior first-probe seed: correlation test" below. The activity-vs-CRF sign flips
   across clips, so the cheap global seed is not viable on this signal.
2. **One full-length 4K encode with attribution**, to confirm the 5m extrapolations or surface
   a feature-length-only effect (especially probes/chunk under a deep prior cascade).

Incremental search wins already listed and worth doing, both pure probe-count reductions:
flat-low-response early stop (item 5) and provisional in-progress priors (item 3).

### Change: None yet (analysis only)

No code or default changed. This entry records the direction and the gating experiment for the
next round of work.

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
transparent-for-streaming. Kept workdir at `~/testing/band-confirm/.reel-Sully_t00-1ce039b19801`.

## 2026-06-14 HDR display peak luminance 1000 vs 1500 (reviewed, tested, reverted)

> **SUSPECT -- re-validate the full-chunk numbers** (see "Cascade root cause + FIX" -> "Impact on
> prior findings"). The "8/33 below band" full-chunk result and the sampling-representativeness gap
> rest on single `fullvalidate` runs on 4K HDR content (near-floor chunks present), which could be
> partly the scoring cascade. Low stakes -- 1000 was kept regardless -- but treat the gap magnitude
> as unconfirmed until rerun on the fixed binary.

Machine: dev box (32 cores, dual-channel DDR5, CUDA GPU). Clip: `sully-5m-4k-hdr`
(7184 frames, 3840x2160 -> cropped 3840x1600, HDR PQ). Chosen because it is the
only 4000-nit master in the test set (mastering-display max 4000 cd/m2, MaxCLL 4000,
MaxFALL 290) -- the others (io, kbv1, ko) are 1000-nit masters where a display-peak
change above 1000 is a no-op.

### Why

Reviewed the CVVDP display model (`internal/quality/defaultDisplayModel`) against the
user's actual screens: LG C2 OLED (~800 nit peak), M4 iPad Pro (~1600), Pixel 10 Pro
(~2000+) -- all OLED, all viewed at normal distances. The HDR default
`max_luminance` was 1000, below all three devices and below cvvdp's own
`standard_hdr_pq` preset (1500). Hypothesis: 1000 under-tests highlight
banding/quantization for 4000-nit-mastered content on bright panels, so the search
might be leaving visible highlight artifacts the user could see.

Other parameters were reviewed and left unchanged:
- **Geometry** (55" @ 1.3 m -> ~76 ppd) already bounds all three devices: the C2 at a
  living-room distance is ~100-150 ppd and the iPad/Pixel are higher still (higher ppd
  = artifacts subtend fewer degrees = less demanding). 76 ppd is near the eye's acuity
  ceiling and is the lowest-ppd (most demanding) realistic case.
- **SDR** (200 nit, 1000:1, 100 lux): even though all three panels are OLED with true
  blacks, at `E_ambient` 100 lux the reflected screen flare (`100 * k_refl / pi` ~ 0.16
  nit) already sits above both the modeled 0.2-nit black and OLED's true black, so
  raising SDR contrast changes almost nothing unless watching in a near-dark room. Not
  worth the bits.

### Method

A/B with the same rebuilt binary, differing only in HDR display `max_luminance`.
Baseline used `--cvvdp-display` pinned to a 1000-nit JSON otherwise identical to the
default HDR model; the new run used the 1500-nit default. Both encodes were then
scored by `scripts/fullvalidate`, which regenerates the display model from the current
binary's default (1500) -- so **both were judged under the same stricter 1500 ruler**.

### Results

| Metric | Baseline (1000-trained) | New (1500-trained) |
|--------|-------------------------|--------------------|
| Video size | 36.86 MB | 36.28 MB |
| Probes/chunk | 1.73 | 1.36 |
| Sampled JOD mean | 9.419 | 9.407 |
| Ground-truth full JOD mean | **9.467** | **9.345** |
| Chunks below band (of 33) | **0** | **8** |
| Sampled-vs-true gap (mean abs) | 0.051 | 0.109 |

### Conclusion: reverted to 1000

Two findings, both against the change:

1. **The 1000 model was not letting artifacts through.** The 1000-trained encode,
   judged by the stricter 1500 ruler, still scores 9.467 mean with **zero** chunks
   below band. There is no highlight-quality deficit for 1500 to fix; existing encodes
   already satisfy a 1500-nit viewer (reassuring for the C2/iPad/Pixel).
2. **The sampled windows under-cover a chunk's brightest frames.** This is the sharp
   mechanism, not just "sampling is noisy." Note what did and did not move: the sampled
   score barely changed (9.419 -> 9.407) and mean CRF barely moved (43.50 -> 44.12), so
   the windows are mostly *not* highlight frames -- raising the display peak to 1500 only
   affects content above 1000 nits, and the 3x48 windows largely sit below that. Yet the
   **full-chunk** truth (which sees every frame, including the >1000-nit highlights the
   windows skipped) dropped 9.467 -> 9.345, with the sampled-vs-true gap **doubling**
   (0.05 -> 0.11) and **8/33 chunks below band**. So the search was blind to those bright
   frames in *both* runs; it only stopped mattering at 1000 because they weren't scored
   low enough to fall below band. The sampled summary looked fine (9.407, all converged,
   fewer probes) -- a textbook "validate against ground truth, not sampled scores" trap.

Net: 1500 gives no quality upside, slightly worse delivered quality, at the same size.
Reverted `internal/quality/display.go` to `max_luminance = 1000`.

Confounds checked and ruled out:
- **Not just CRF-curve steepness.** Higher CRF steepens JOD-vs-CRF, so any sampling error
  maps to a bigger JOD error -- but the gap showed up at *moderate* CRF too (chunk 0030 at
  CRF 38.75: full 9.09, gap -0.40; chunk 0007 at 40.50: gap -0.29), so it is not a
  steepness illusion.
- **Not a one-clip fluke.** `sully-5m` is a known-noisy clip (cliff chunks) and the only
  4000-nit master available, so the absolute 8-below-band count may be partly CRF-divergence
  on cliff chunks. But this is the *second* independent sighting of the same failure mode:
  the feature-length Sully run had chunk 0664 (true 8.689 vs sampled 9.679, ~1.0 JOD miss)
  on a CRF-ceiling chunk -- a worst-case sub-segment the windows did not represent. Same
  story.

Important: this is **latent, not biting at the shipped config.** At 1000 / +-0.20 the
sampled-vs-true gap is 0.05, at the probe noise floor (~0.075), and full-chunk truth of the
shipped encode is 0/33 below band. Today's bits are genuinely fine; the gap only becomes a
delivered-quality problem under a stricter/more-highlight-sensitive model than the sampling
can track. Do not churn the sampler for this alone.

Open idea (do not retry 1500 without this): the remedy is **luminance-aware window
placement**, not just generic "extra windows on high spread." Per-frame luma is already
available from shot detection, so placing one sampled window on the chunk's brightest
segment is nearly free and directly closes the peak-luminance representativeness gap. That
is the concrete prerequisite that would make a stricter HDR display model viable. Until it
exists, keep `max_luminance = 1000`. Artifacts:
`~/testing/hdr-lum-ab/{baseline-1000,new-1500}{,-wd}` plus `*-fullvalidate.txt` and
`*-output.mkv`.

## 2026-06-15 High-variance test clip: the probe tail is within-chunk, not chunk-to-chunk

> **CORRECTION (see "2026-06-15 Cascade root cause + FIX" below).** The headline conclusions of
> this entry were confounded by a GPU-scoring concurrency bug discovered/fixed over 06-15/06-16
> (multiple coexisting VSHIP handlers corrupt CVVDP scores under >1 metric worker). The "2.78
> probes/chunk, IN feature regime" measurement was a single run that hit that cascade; with the
> fix, repeats of the *same* config are identical at ~1.55 -- the same as easy content. So at the
> shipped band, content hardness does **not** raise probes/chunk, and the "within-chunk
> worst-window difficulty drives the tail" claim is unproven (much of the 2.78 run's worst-window
> failure was the scoring bug, not content). What still holds: the clip-build method (mkvmerge,
> hard-content selection), the `sample_frames` gating finding, and that the *original feature*
> 3.11 tail is real (clean run, no artifact).

Goal: the open methodology item -- build a ~15-min clip that reproduces the feature-length
probe tail (so search-layer changes can be A/B'd without a 4-hour encode), since homogeneous
5-20m cuts converge at ~1.7 probes/chunk and hide the tail. Source: Sully full rip (same film
as the feature run, so HDR/color metadata is identical and concatenation is clean). All clip
encodes preset 8 (probes/chunk is preset-independent: the homogeneous `sully-5m` control read
1.67 at preset 8 vs the documented ~1.7 at preset 6, so a faster preset is a valid proxy and
cuts the loop to ~20-30 min). Build with **mkvmerge** `--split parts:A-B,+C-D,...` not ffmpeg
`concat -c copy`: the latter copies a stale per-track Matroska `DURATION` tag equal to the
*full* source, so reel reads the 15-min clip as 95 min and mis-places crop samples.

### The result that corrects the doc: CRF variance is NOT the tail driver

The 2026-06-14 feature entry attributed the probe tail to "far higher chunk-to-chunk CRF
variance." A controlled two-clip A/B falsifies that as the cause:

| clip (preset 8) | selection | chunks | probes/chunk | maxed 6 | converged | final_crf sd |
|---|---|---:|---:|---:|---:|---:|
| `sully-5m` (control) | one homogeneous scene | 33 | 1.67 | 0% | 100% | 8.05 |
| **even-spaced** | 15x60s scenes spread across the film | 112 | 1.64 | 0% | 96% | **11.84** |
| **hard-content** | 15 windows on the film's actual maxed chunks | 110 | **2.78** | **14%** | 51% | 15.54 |
| feature (reference) | full 96-min film | 670 | 3.11* | 21% | 63% | 12.35 |

\*feature 3.11 was at the old tight band (9.25-9.52); at today's default 9.15-9.55 the whole
feature is ~1.46. The hard-content clip's 2.78 above is at the **default** band -- i.e. it is a
*harder* probe stress test than the average feature, because it is concentrated hard content
with the easy 79% removed.

The even-spaced clip **matched the feature's chunk-to-chunk CRF variance** (sd 11.84 vs 12.35)
yet produced **no probe tail** (1.64 probes/chunk, 0% maxing). Tightening it to the feature's
own 9.25-9.52 band barely moved it (1.79, still 0% maxed), where the same tightening doubled
the feature (1.46 -> 3.11). So CRF variance is a *side effect* of hard content, not its cause.

What actually drives the tail is **within-chunk worst-window difficulty**: the feature's hard
chunks have pervasive intra-chunk window spread (~0.49 JOD p50 on the final probe) that trips
the worst-window floor guard, while steady mid-scene grabs are internally clean (~0.25 p50) and
converge in 1-2 probes regardless of how different they are from their neighbors. The
hard-content clip's stop reasons confirm the mechanism firing: monotonicity_guard 23,
bounds_crossed 21, max_probes 10 (vs 0 guard trips on the even-spaced clip). This is the same
representativeness gap the HDR-peak and band-confirm sightings point at -- worst-window coverage,
not boundary placement -- consistent with AGENTS.md's "no cut in the middle of a fade."

### How the hard-content clip is selected (evidence-based, reproducible)

The kept feature workdir (`~/testing/fulllen-attr/.reel-Sully_t00-1ce039b19801/
target-quality.json`) records which chunks maxed the probe budget. Chunks are sequential and
sum to 137,877 frames, so cumulative frames give each chunk's source timestamp (23.976 fps,
feature encoded from frame 0). `build-highvar-clip.py` reads that file, finds the 138 maxed
chunks (21%), clusters them by source time (merge gaps <25s), ranks clusters by hard-chunk
density, and extracts ~48-100s windows around the densest ones until ~15 min is filled. No
film knowledge, no randomness -- the clip is made of the real movie's genuinely hard scenes.

### Deliverables (in `~/testing/`)

- `sullyhv-15m-4k-hdr.mkv` -- the canonical high-variance clip (hard-content selection, 912s,
  4K PQ HDR, 110 chunks, 2.78 probes/chunk at the default band). Runs via the normal harness:
  `./run-test-encode.sh sullyhv-15m-4k-hdr --keep-workdir`.
- `build-highvar-clip.py` -- the canonical builder (evidence-based hard-content selection).
  (The earlier even-spacing builder was deleted -- it wrote the same filename and would clobber
  the canonical clip with the no-tail version; its result is the negative-control row above.)
- `highvar-stats.py <target-quality.json|workdir>` -- prints probes/chunk, maxed-6 %, converged
  %, and final_crf sd against the feature-regime reference, with an IN/NOT-in-regime verdict
  (thresholds probes/chunk >=2.6 and sd >=10).

### What this unblocks

Search-layer A/Bs (first up: the probe-sample frame-count lever) now have a 20-30 min stress
clip with a real probe tail at the shipped band, instead of either a 4-hour feature or a short
clip that hides the tail. Use `sully-5m` as the easy-content control and `sullyhv-15m` as the
hard-content test in the same A/B.

## 2026-06-15 Cascade root cause + FIX: concurrent VSHIP CVVDP handlers corrupt GPU scoring

While setting up a `probe_sample_frames` A/B, two runs of the *identical* config (sf48, default
band, preset 8) on the same clip diverged enormously. Root-caused over 06-15/06-16 to a GPU
concurrency bug in CVVDP scoring; **fixed** by using one shared VSHIP handler. RESOLVED.

### The instability (5 same-config runs, sullyhv-15m, preset 8, default band)

| run | probes/chunk | converged | chunks floored @4.25 | output size |
|---|---:|---:|---:|---:|
| r1 | 2.78 | 51% | 27 | 1.911 GB |
| r2 | 1.74 | 96% | 0 | 0.237 GB |
| r4 | 2.94 | 57% | 14 | 1.627 GB |
| r5 | 1.49 | 98% | 0 | 0.207 GB |

Bimodal: ~40% of runs cascade into mass CRF-flooring; the rest are clean. **9.2x output-size
swing on identical input.** Floored chunks are not higher quality -- they are maxed on a *false*
score, wasting bitrate. The cascade fingerprint is a **constant, CRF-independent worst-window
value** across many chunks (r1: 8.165 on 89 probes; r4: 8.56 on 82) -- impossible for real
scoring (the same 8.16 appears at CRF 6.5 and CRF 32.75), so a scoring fault, not content.

### Root cause: multiple coexisting VSHIP handlers corrupt shared GPU state

The diagnosis ruled out, in order:

- **NOT source frame-misalignment.** `scripts/aligncheck` decodes the concatenated clip
  sequentially as ground truth, then calls `video.ReadFrame(N)` on fresh Sources (the scoring
  access pattern). Result: 0 mismatches sequential, **0/480 mismatches under 8-way concurrency,
  with the autocrop applied** -- source decode is correct and deterministic. (This also kills the
  "xav-style sequential source pairing" fix idea: the layer it changes is already correct.)
- **NOT a concatenation/seek issue.** An earlier disambiguation (continuous hard cut clean) had
  *suggested* concatenation; that was a red herring -- the concatenated clip simply has more
  chunks (110) and so more concurrent-scoring exposure (see trigger below).
- **NOT inherent metric noise.** `fullvalidate` at **workers=1** is byte-identical across two
  passes (mean 9.4757, 0 below) and matches the clean encode; at **workers>1** it corrupts
  (9.17/31-below). So the defect is *concurrency*.
- **It is multiple coexisting CVVDP handlers.** Serializing the GPU *compute* with a mutex (both
  per-call and whole-chunk granularity) reduced but did **not** fix it (~9.41, still off 9.4757,
  still nondeterministic) -- so concurrent compute is not the whole story. Using **one shared
  handler** across 4 workers (compute serialized by the mutex) is **byte-identical to workers=1**
  (9.4757, 0 below, both passes). The differentiator is handler *count*: N coexisting VSHIP CVVDP
  handlers share/clobber device state (`Vship_CVVDPInit2` allocates per handler but the library
  shares global GPU resources), so even serialized compute on >1 handler is wrong.

### Trigger (corrected): concurrency x near-floor content x chunk-count -- NOT concatenation

The cascade needs: (a) >1 metric worker (default 4 for 4K, 8 below), (b) chunks whose true score
sits near the 9.15 floor so a corrupted worst-window dips below it, and (c) enough chunks for the
race to fire. The concatenated `sullyhv` (110 hard chunks) maxes all three. **Production is
exposed too:** a real feature (670 chunks, hard scenes, workers=4) has more exposure, not less --
the single clean feature run was luck / its lower fraction of near-floor chunks, not immunity.
Easy content (sully-5m, mostly high-scoring chunks) stays clean because corruption rarely dips a
9.5 chunk below 9.15.

### Why a wrong score cascades into a whole-encode blow-up (the amplifiers)

1. The false sub-floor worst-window trips the floor guard (`tq.go:184`, a hard 9.15 threshold,
   no noise margin) -> the chunk is driven to the CRF floor (4.25).
2. `InitialCRF()` (`target_quality.go:952`) seeds the next chunk from that floored neighbor, and
   `targetQualityFullFirstProbe` only re-probes upward for **median**-seeded chunks (`:510`), so a
   neighbor-seeded chunk accepts the inherited floor and passes it on -- floors land in contiguous
   runs (r1: chunks 76-82) until a median reseed breaks the chain.

These amplifiers are now untriggered (scoring is correct) but remain latent robustness gaps;
optional future hardening: a noise margin on the floor guard, and not letting floored chunks seed
neighbors at the floor.

### The fix (implemented, validated)

- **One shared VSHIP handler.** `target_quality.go` now creates a single `VshipProcessor` and
  fills the metric pool with that same pointer `MetricWorkers` times, so several workers decode
  concurrently while the GPU compute runs on one handler. `fullvalidate` was fixed the same way
  (it had the identical N-handler bug, which is why ground truth was unreliable on spliced input).
- **`quality.gpuMu` serializes the GPU compute.** Each chunk's whole `ResetCVVDP`->`ComputeCVVDP`
  sequence is held under the mutex as one unit (CVVDP is temporal; interleaving two sequences on
  the shared handler still perturbs it). The per-chunk producer is started *before* the lock, so
  other workers keep decoding while one holds the GPU.
- **Throughput cost (real, accepted).** The old N-handler path overlapped CVVDP compute across
  per-handler CUDA streams -- that overlap was the speedup *and* the corruption. Serializing it
  slows the preset-8 `sullyhv` encode from ~18 min (a pre-fix *clean* run) to ~31 min (~1.7x); the
  fixed run is ~the same as a pre-fix *cascade* run. The share is smaller at slower presets (the
  encoder dominates) and on 4K (bandwidth-bound encoder). This is the price of correctness until
  VSHIP can isolate handlers (separate device buffers + streams) to make concurrent scoring safe;
  revisit if metric wall time becomes the priority.
- **Validation.** `fullvalidate` workers=4 is now byte-identical to workers=1 (9.4757). End-to-end,
  4 fresh `sullyhv` encodes are now **identical**: 1.55 probes/chunk, 0 floored, 0.281 GB each
  (vs before: 1.49-2.94, 0-27 floored, 0.21-1.91 GB / 9.2x). `./check-ci.sh` passes (incl. -race).

### Side conclusions (now reliable)

- Content hardness does **not** raise probes/chunk at the shipped +-0.20 band: the fixed `sullyhv`
  sits at 1.55, same as easy content. The earlier "hard clip reproduces the 2.78 tail" was a
  cascade artifact. The original feature 3.11 tail (tight band, clean run) remains real.
- The probe-sample A/B is unblocked but its earlier numbers were cascade-contaminated; rerun on
  the now-deterministic clip if pursued (still structurally gated -- 66-76% of chunks full-probed).

### Impact on prior findings (this bug was live in all earlier multi-worker testing)

The bug existed in every prior encode run with >1 metric worker (the default: 4 for 4K, 8 below)
*and* in `fullvalidate` (it created one handler per worker too). So any earlier result that leans
on accurate concurrent CVVDP scores can be contaminated. Two earlier entries were already *seeing*
this without naming it: the lp entry's "TQ-mode wall time uninterpretable due to the probe-cascade
confound" and the mw4v6 entry's "mw6 took more probes via the prior cascade" -- both were the
handler-corruption cascade. Discriminator: a finding is **at risk** if it depends on concurrent
CVVDP scores on hard/near-floor content, on more-sampled-chunk configs (only sampled chunks, >=3
windows, can cascade -- full-probed chunks are immune), or on a single fullvalidate run; it is
**low risk** if it used fixed-CRF / byte-identical output (no metric), is architectural, or is an
aggregate "0 below range" on easy high-scoring clips that replicated.

- **2026-06-12 metric-worker scaling benchmark** -- SUPERSEDED. It measured GPU *throughput*
  scaling from N concurrent handlers (1080p saturates ~8, 4K ~6). Those numbers were real but the
  parallelism came from per-handler CUDA streams -- exactly what the fix removes. With one shared
  handler, metric workers no longer parallelize GPU compute (they gate decode only); the worker
  defaults persist but the scaling curve no longer applies.
- **2026-06-13 4K metric workers 4 vs 6** -- CONTAMINATED + now MOOT. "mw6 took more probes"
  (io a: 82 probes / 2.28 per chunk, the worst run) was the handler-corruption cascade (6 handlers
  cascade more than 4), misattributed to prior-cascade *timing*. Keeping 4 is still fine, but the
  4-vs-6 question is moot under one shared handler.
- **2026-06-13 Accuracy-trading knobs (full-probe threshold 256 vs 144)** -- CONFOUNDED, re-test
  before trusting the magnitude. knobB (144) full-probes fewer chunks, so it had far more sampled
  chunks = far more cascade exposure than base (256). Its "accuracy collapse" (22/33 above range,
  sampled gap 0.46) is inflated by the bug, not pure sampling error. The *direction* (full-probing
  is more accurate and cascade-immune) likely holds, but rerun the A/B on the fixed binary before
  citing the 0.46 JOD penalty as real.
- **2026-06-14 HDR display peak 1000 vs 1500** -- **RESOLVED: it was the cascade** (2026-06-18
  re-baseline). Re-scoring new-1500-wd under the 1500-nit ruler with the fixed handler moves it from
  mean 9.345 / 8 below / gap 0.109 to mean 9.448 / **0 below** / gap 0.040; the -0.4 chunks collapse
  to ~0 gap. The "8/33 below band" and "sampling representativeness gap" were the scoring bug, not
  under-sampling. (The revert-to-1000 decision is unaffected -- 1500 still buys no real quality.)
- **Feature run (3.11) + band WIDTH default + monotonicity diagnostic** -- LIKELY SOUND, and the
  adjacent 9.15-9.55 band-confirm feature run is now **CONFIRMED clean**: re-scoring it with the
  fixed ruler is byte-identical to the buggy-ruler run (2026-06-18 re-baseline), so that
  ground-truth pass was never contaminated. The kept feature `target-quality.json` was checked clean
  of the artifact (0% of worst-window in [8.0,8.7], top value 9.23), and the band-width result is a
  *simulation* over those clean probes -- so the shipped 9.15-9.55 default does not rest on corrupted
  data. (The band-confirm 0664 outlier -- full 8.689 vs sampled 9.679 -- survived the re-score
  unchanged: it is a REAL rare max-CRF sampling miss, not the cascade.)
- **Low risk (no re-test):** lp bitstream identity (fixed-CRF), 4K bandwidth ramp (SVT-encoder
  concurrency, not VSHIP), the concurrency-restructure / 4K-ramp fullvalidate checks (easy 5m clips,
  scores ~9.4 far from the 9.15 floor, "0 below range" replicated), and all architectural findings
  (sampled probes, adaptive priors, scheduling, bracket-aware search, chunk-boundary work).

Artifacts: `scripts/aligncheck` (decode-alignment diagnostic), `~/testing/probe-sample-ab/`
(sf*/r*/fixed-r* JSONs), `~/testing/.fv-workers1.log` / `.fv-shared.log` (workers=1 and
shared-handler determinism), `~/testing/.fix-validate.log` (end-to-end), `~/testing/sullyhv-15m-4k-hdr.mkv`.

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

## 2026-06-18 Metric serialization is the preset-6 wall bottleneck (concurrency cost is NOT smaller at slow presets)

Follow-up to the re-baseline, prompted by pushback on the "Restore safe metric concurrency" open
item's claim that the single-handler serialization cost "is smaller at slower presets (encoder
dominates)". Re-read the per-probe `encode_seconds`/`metric_seconds` in the fixed-binary **preset 6**
re-baseline logs (no new encode). Under the single shared handler all CVVDP compute is serialized by
`quality.gpuMu`, so `sum(metric_seconds)` is a hard floor on video-encoding wall time.

| clip | tier | enc_s (sum) | metric_s (sum) | wall_s | metric/wall |
|------|------|-----:|-----:|-----:|-----:|
| im-5m | 1080p SDR | 345 | 128 | 151 | 85% |
| sully-5m | 4K HDR | 917 | 521 | 641 | 81% |
| kbv1-5m | 4K HDR | 837 | 529 | 648 | 82% |
| ko-5m | 4K HDR | 1283 | 641 | 744 | 86% |
| sullyhv-15m | 4K HDR | 3209 | 1796 | 1963 | 91% |

The serialized metric is **81-91% of wall at preset 6, for 4K and 1080p alike.** The "encoder
dominates at slow presets" intuition conflates *compute* share with *wall* share: the encoder's
compute is larger in sum (metric is only ~33-39% of total compute), but encodes run across several
workers in parallel while the metric is pinned to one serial GPU lane -- so the encode work
compresses away and the serialized metric becomes the binding wall-clock constraint. Slowing the
encoder (preset 6 vs 8) does not shrink the metric's wall share, because the encoder parallelizes
and the metric does not. (This also flips the older "4K TQ is memory-bandwidth bound" wall framing:
the bandwidth cap still limits the *encoder*, but post-fix the serialized metric is what binds 4K
wall clock.)

Caveats on what this proves: (1) it is **not** a serial-vs-concurrent A/B -- it bounds the
*potential* win (~80-90% of wall is serialized metric) but does not measure how much concurrency
would actually recover, which depends on how much CVVDP overlaps on one physical GPU. (2) The one
real recovery datapoint remains the pre-fix preset-8 sullyhv (~18 min clean -> ~31 min fixed = 1.7x,
i.e. concurrency recovered ~41% of wall) -- confounded by the cascade and at a different preset, but
it shows the overlap headroom is real and large, not hypothetical. If that ~halving-of-metric-wall
ratio held at preset 6, the penalty would be comparable there (~1.5-1.7x), not smaller.

Outcome: re-framed and **bumped the "Restore safe metric concurrency" open item from medium to
high** -- the serialization tax falls on the large majority of wall at the production preset, not a
slow-preset afterthought. The fix is still VSHIP-side (external); the cheap reel-side next step is a
preset-6 serial-vs-concurrent A/B to quantify the recoverable fraction. (Parked pending user
go-ahead on concurrency work.) Source: `~/testing/rebaseline-20260617/*/.reel-*/target-quality.json`
(`.probes[].encode_seconds` / `.metric_seconds`); wall from `STATUS.txt`.

## Resolved questions (index -- full detail in the dated entries above)

- **Re-baseline accuracy ground truth on the fixed binary** -- done 2026-06-18 (see "Re-baseline
  accuracy ground truth on the fixed binary"). Fresh preset-6 default-band encodes of 5 clips
  (1080p SDR + easy/grainy/mixed 4K HDR + the near-floor sullyhv-15m stress) plus re-scores of the
  flagged suspect workdirs, all on the post-fix ruler. The shipped config is clean everywhere (0
  below band, 0 floored, 100% converged). The two suspect findings split: band-confirm chunk 0664 is
  **real** (byte-identical re-score -- a rare max-CRF sampling miss), the HDR "8/33 below band" was
  the **cascade** (dissolves on the fixed ruler). The accuracy record now rests on a trustworthy
  ruler.
- **GPU-scoring concurrency cascade** -- found + **fixed** 2026-06-16 (see "Cascade root cause +
  FIX"). Multiple coexisting VSHIP CVVDP handlers (one per metric worker) corrupted scores under
  >1 worker, producing a false constant sub-floor worst-window that tripped the floor guard and
  cascaded into ~9.2x output-size swings (~40% of `sullyhv` runs; production features exposed too).
  Fixed with a single shared handler + `quality.gpuMu` serializing GPU compute; fullvalidate fixed
  the same way. Validated byte-identical (workers=4 == workers=1) and end-to-end (4 sullyhv runs
  now identical). Diagnostic tool `scripts/aligncheck` ruled out source frame-misalignment.

- **High-variance test clip (methodology/tooling)** -- built 2026-06-15, usable after the scoring
  fix. `sullyhv-15m-4k-hdr` (+ `build-highvar-clip.py`, `highvar-stats.py`) is now deterministic
  (the concatenated clip exposed the scoring bug; it was never a clip defect). Caveat: at the
  shipped +-0.20 band hard content converges at ~1.55 probes/chunk, same as easy content -- so the
  clip is a valid deterministic asset but shows no probe tail at the shipped band.

- **Worst-window / straddle early-out** -- rejected 2026-06-14 (see "Worst-window / straddle
  early-out: simulated and rejected"). Simulated over all 670 Sully chunks (BestProbe replay
  verified 670/670). Result-safe causal rules save ~0%; the aggressive straddle stop saves 30% but
  changes 32% of picks by ~a full tolerance band. Every changed pick is *more* overshoot (quality-
  safe, never below floor) but ~47% larger encodes -- so it is a speed-vs-size knob (+15% output
  for ~30% fewer probes), not the accuracy-neutral win the diagnostic assumed. Not worth it as a
  default for an archival encoder.
- **monotonicity_guard diagnostic** -- resolved 2026-06-14 (see "monotonicity_guard diagnostic").
  The 147 guard chunks split three ways: 52% worst-window-limited (mean in band, a hard sub-segment
  ~0.02 JOD below floor; overshoot is the correct quality-safe pick, not recoverable), 39%
  mean-bracket but noise-limited (interpolation only ~60% reliable; residuals are noise not
  curvature), 6% all-below, 2% all-above. The guard fires on sub-noise wobbles (0.022 vs 0.075 JOD
  scatter) but is not discarding converge-able probes. Outcome: flat-low gate rejected; the
  accuracy-safe win is a worst-window/straddle early-out (speed only).
- **Flat-low-response early stop** -- rejected 2026-06-14 (folded into the diagnostic above). The
  all-mean-below population it targets is 6% (9/147 guard chunks) at feature length; the overshoot
  tail is an all-above / worst-window problem, so the mirror-of-flat-high framing does not apply.
- **Content-prior first-probe seed** -- rejected 2026-06-14 (see "Content-prior first-probe
  seed: correlation test"). The discarded `scoreVideo` per-frame scores do not robustly
  predict per-chunk CRF: the activity-vs-CRF Spearman sign flips across clips (soms 1080p
  +0.54, io/kbv1 4K -0.63/-0.56), so a global first-probe seed would push cold starts the
  wrong direction on some content. The signal is a temporal-change measure on a 64x36
  downsample, not the spatial detail/grain that drives JOD-at-CRF. A future test would need a
  real spatial-complexity feature; the `RetainScores` hook + chunkbench dump are in place to
  support it.
- **Q0 -- Full-length 4K stage attribution** -- resolved 2026-06-14 (see "Full-length 4K
  encode: stage attribution"). Ran a 96-min Sully 4K encode (4h2m). Shot detection is linear
  (confirmed), crop is constant (not linear, corrected), no memory leak / concurrency
  collapse. But the core prediction was REFUTED: probes/chunk nearly doubled (1.7 -> 3.11)
  because feature content has high chunk-to-chunk CRF variance, so short test clips
  systematically under-state probe cost. Cold starts were eliminated as predicted (3/670
  default) but that saving is swamped. The probe tail (21% maxing probes, 37% not cleanly
  converging, 30% overshooting the band as bit-efficiency loss) is the dominant feature-length
  cost -- it elevated the search-layer tail items above to top priority.
- **Q6 -- 4K adaptive ramp (bandwidth, not capacity)** -- resolved 2026-06-12 (see "4K
  adaptive ramp: bandwidth, not capacity"). 4K was bandwidth-bound, not capacity-bound; the
  fix caps and starts 4K at `maxWorkers/6` rather than ramping higher. (Open sub-item -- the
  divisor retest on other hardware -- moved to the performance/infra list above.)
- **Q7 -- 4K metric workers 4 vs 6** -- resolved 2026-06-13 (see "4K metric workers 4 vs 6
  retest"). Keep 4. Throughput-normalized cost was identical (mw4 9.70 vs mw6 9.73 s/probe)
  because the bandwidth-capped encoder sustains only ~5 active workers, so pending scoring
  tasks rarely exceed 4 and the extra GPU workers idle. 6 added ~3.7 GiB peak VRAM for no
  wall-time gain.
- **Q8 -- SVT-AV1 level_of_parallelism** -- resolved 2026-06-13 (see "SVT-AV1
  level_of_parallelism: bitstream identity and 4K scaling"). lp is byte-identical across
  values, so it is a free throughput knob; lp now scales off the resolution-aware worker
  target (4K -> lp 3, was 2). Fixed-CRF A/B showed a clean ~3-4% 4K throughput gain with
  identical output (TQ-mode wall time was uninterpretable due to the probe-cascade confound).
  New `--level-of-parallelism` flag added for overrides.
- **Q9 -- Accuracy-trading TQ knobs** -- resolved 2026-06-13 (see "Accuracy-trading TQ
  knobs"). Keep all three as-is. fullvalidate A/B showed lowering the 256 full-probe threshold
  to 144 wrecks accuracy (sully mean_abs_error 0.075 -> 0.302, 22/33 chunks above range) *and
  raises* GPU work (+30% probes) because noisier samples force more probes; the window size is
  only a 4-9% GPU lever and not worth more sample degradation; the 12s cap binds on 0-2 of
  ~35 chunks so there is nothing to gain.
- **Q10 -- Overlap pre-encode head with encoding** -- resolved/deferred 2026-06-14 (see
  "Overlapping the pre-encode head (shot detection) with encoding"). Shot detection scales
  linearly (4K ~5ms/frame -> ~14-15 min on a 2hr feature; 1080p ~2.7 min), so the overlap
  prize is real only at 4K feature length -- and the feature run showed the head is only ~5%
  of feature wall (vs ~12% on a 5m clip) because the encode stage grows super-linearly, making
  this even less attractive. Naive front-to-back streaming does not work: scoring is already
  fully parallel across the file and three whole-file statistics (cut threshold, strong-cut
  threshold, merge passes) gate every boundary, so streaming requires online threshold
  estimators that *change the boundaries* (accuracy-affecting; must pass Boundary hash +
  fullvalidate) plus incremental plan/encoder/resume plumbing. Revisit only if 4K
  feature-length wall time becomes the priority.
- **HDR display peak luminance 1000 vs 1500** -- reviewed and rejected 2026-06-14 (see "HDR
  display peak luminance 1000 vs 1500"). Raising the HDR display model to cvvdp's standard 1500
  (to match bright OLED panels) gives no quality upside and slightly *worse* delivered quality on
  4000-nit content: the 1000-trained encode already passes the 1500 ruler (0/33 below band, mean
  9.467). The sharp finding is a **sampling representativeness gap** -- the 3x48 windows under-cover
  a chunk's brightest (>1000-nit) frames, so a highlight-sensitive metric exposes divergence the
  search is blind to (full-chunk truth drops to 9.345, 8/33 below band, while sampled score/CRF
  barely move). Latent at the shipped 1000 config (gap at noise floor, 0/33 below band). Remedy:
  luminance-aware window placement (one window on the brightest segment; luma already available).
  Keep 1000 until that exists.
