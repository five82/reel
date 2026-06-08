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

- Chunks are scheduled in timeline blocks of 32 and sorted largest-first within each block.
- TQ probes use sampled windows: normally 3x48 frames, with 5 windows on later probes after high spread.
- Chunks at or below the full-probe threshold are probed whole.
- Search uses adaptive CRF priors from completed chunks.
- Search is bracket-aware after the first two probes:
  - interpolate only once probes bracket the target,
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

## Open questions / next tests

1. Keep bracket-aware search, conservative high-side jump gating, and block size 32 unless another clip shows a clear regression.
2. Watch for high-spread chunks like `knives5` chunk `0001` where bracket-aware search can add a probe; consider targeted handling only if this repeats.
3. Consider provisional priors from in-progress chunks only if probe-count tails remain across multiple clips.
4. Do not revisit chunk-boundary complexity unless sampled-window spread or full validation shows a repeatable failure that cannot be addressed in sampling/search.
