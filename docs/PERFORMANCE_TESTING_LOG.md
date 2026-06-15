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
2. **Training the search on 1500 backfires.** Higher display peak makes the metric more
   highlight-sensitive, but Reel's sparse 3x48 sampling cannot track those bright
   moments -- the sampled-vs-true gap **doubled** (0.05 -> 0.11). The search believed
   chunks sat at 9.3-9.5 while ground truth was 9.0-9.1, picked too-high CRFs, and
   **8/33 chunks fell below band** in ground truth at the same file size. The sampled
   summary looked fine (9.407, all converged, fewer probes) -- a textbook "validate
   against ground truth, not sampled scores" trap.

Net: 1500 gives no quality upside, slightly worse delivered quality, at the same size.
Reverted `internal/quality/display.go` to `max_luminance = 1000`.

Caveat: `sully-5m` is a known-noisy clip (cliff-response chunks, noisy probe counts)
and is the only 4000-nit clip available, so the absolute 8-below-band count may be
partly CRF-divergence on cliff chunks. The robust, mechanism-consistent signal is the
**doubling of the sampled-vs-true gap**, which points the right way regardless.

Open idea (do not retry 1500 without this): a higher HDR display peak is only viable
**with highlight-aware sampling** -- conditional extra/brighter-region windows on
high-luminance chunks so the search can track the highlight sensitivity it introduces.
This is a variant of the standing "smarter sampling on high-spread chunks" item. Until
that exists, keep `max_luminance = 1000`. Artifacts:
`~/testing/hdr-lum-ab/{baseline-1000,new-1500}{,-wd}` plus `*-fullvalidate.txt` and
`*-output.mkv`.

## Resolved questions (index -- full detail in the dated entries above)

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
  9.467), while the 1500-trained search overshoots CRF because sparse sampling cannot track the
  added highlight sensitivity (sampled-vs-true gap doubles, 8/33 below band at the same size).
  Keep 1000. Only viable with highlight-aware sampling first.
