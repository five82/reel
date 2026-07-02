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

### 2026-07-02 -- Current-code baseline refreshed; perf matrices made explicit

- **Question:** After the shot-detection worker default landed, what is the
  current default-matrix baseline, and how should future A/Bs choose clip
  coverage without overloading the historical default matrix?
- **Method/artifacts:** Clean worktree, rebuilt `./reel`, ran
  `scripts/perf/run-suite.sh --label tq-baseline-current` on the historical
  default matrix. Added `run-suite.sh --matrix {default,coverage,encoder,long}`
  and recorded matrix/clip tokens in new `run-meta.json` files; explicit clip
  args still work and are recorded as `custom`. Artifacts:
  `perf-runs/20260702-124007-tq-baseline-current`.
- **Decisive result:** New default matrix: 2556s / 1086.8 MB, 1.36
  probes/chunk (vs previous 2618s / 1090.2 MB at
  `20260702-005326-tq-baseline-decode-slope`). Quality stayed in band;
  sullyhv shot detection now reports 71.1s under the new default. The matrix
  presets keep the default as the continuity anchor while making broad
  coverage (`air/bts/im/soms/io/sully/kbv1/ko/sullyhv`), 4K encoder stress,
  and longer serial-phase checks one-command choices.
- **Decision:** New reference baseline is
  `perf-runs/20260702-124007-tq-baseline-current`. Use named matrices instead
  of ad-hoc clip lists for future non-default coverage.

### 2026-07-02 -- Shot-detection worker oversubscription sweep: no default change

- **Question:** Does pushing shot-detection workers beyond the logical/2
  default recover more of the remaining serial decode cost on a full feature?
- **Method/artifacts:** Built `scripts/chunkbench`, ran the Sully feature with
  `REEL_SHOT_DETECT_WORKERS=16/20/24/32` using chunkbench's no-crop planning
  path (timing is decode-dominated; the boundary hash is only the
  worker-invariance gate). Artifacts: `shotdet-workers-20260702/`.
- **Decisive result:** 16/20/24/32 workers: 414/393/388/394s, all with boundary
  hash `6e860d05af911e7f` and identical 582-chunk chunkbench plan. 24 workers
  is best, but only -26s (-6.3%) vs the current default on a 96m 4K title, and 32
  regresses. The extra workers oversubscribe decoder threads, so the small win
  is hardware-sensitive and not worth changing the default for ~0.4% total wall.
- **Decision:** No code change; keep logical/2 workers. Remaining shot-detection
  wins require a different attack: overlap with encoding, NVDEC decode, or
  folding crop detection into the same pass.

### 2026-07-02 -- Chunk dispatch ordering: assessed, not worth re-examining (no change)

- **Question:** Is the TQ dispatch order (timeline blocks of 32, largest-first
  within each block; prime 4 chunks at the resolution floor, then +3 in-flight
  per completion) leaving probes or wall on the table?
- **Method/artifacts:** No new encodes. Read the ordering/prior code
  (`orderTargetQualityChunks`, `targetQualityPrior.InitialCRF`) and re-cut the
  Sully feature's per-chunk log by initial-CRF source
  (`perf-runs/20260702-013705-feature-validation/Sully_t00/target-quality.json`).
- **Decisive result:** The ordering already feeds the prior well: 510/670
  chunks got a neighbor seed (mean 1.35 probes), 157 the median fallback
  (1.48), 3 the cold default (2.33); 467/670 converged in one probe. Upper
  bound on any ordering improvement: making every median/default seed as good
  as a neighbor seed saves ~24 of 930 probes (2.5%), roughly ~1% of wall --
  inside run noise. Ordering cannot affect delivered quality: every chunk
  converges into the band independently (same argument as the boundary entry
  below). Tail idle is already handled by largest-first-within-block, and the
  adjacent levers were all tested and rejected on 2026-07-01/02: nearest-seed
  fallback, staggered prime seeds, default-CRF sweep, prime at slot target;
  smaller schedule blocks measured slower (worse early priors, per the
  constant's comment).
- **Decision:** No change; ordering is settled. Revisit only if some content
  class shows a collapsed neighbor hit-rate (check `initial_crf_source`
  counts in `target-quality.json`).

### 2026-07-02 -- Boundary quality vs delivered quality: mid-shot joins measured (no change)

- **Question:** Is shot-detection accuracy / chunk uniformity worth improving
  from a quality perspective? The one mechanism unique to chunked TQ: chunks
  are CRF-searched independently, so two adjacent chunks of the *same* shot
  (a synthetic split) can converge to different CRFs, producing a quality step
  at a mid-shot keyframe with no scene cut to mask it.
- **Method/artifacts:** Re-ran chunk planning on the Sully feature with the
  encode's crop (3840:1600:0:280) and dumped per-boundary kinds, then joined
  against the encode's per-chunk final CRF/JOD
  (`perf-runs/20260702-013705-feature-validation/Sully_t00/target-quality.json`).
  Join validated exactly: 670 chunks, zero frame-count mismatches. Artifact:
  `Sully_t00/boundary-kinds.tsv` in the same run dir.
- **Decisive result:** 190 of 669 joins are mid-shot (synthetic split).
  Perceptual step across them: median 0.073 JOD, p95 0.248, max 0.303 --
  bounded by the tolerance band (2 x 0.20 = 0.40) by construction, and
  *smaller* than the steps at natural-cut joins (median 0.088, max 0.371),
  where the scene change masks the transition anyway. CRF can step hugely at
  a mid-shot join (max 22.5; 57/190 stepped >= 4 CRF) while JOD stays flat --
  the per-chunk feedback loop absorbs content drift within long shots.
  Missed cuts *inside* chunks cost bits, not quality: SVT runs scd=0 with
  keyint=10s, so an internal cut becomes an expensive inter frame, but
  whole-chunk full-scan CVVDP still measures the damage and pushes CRF down
  if it matters.
- **Decision:** No change; boundary placement is not a quality lever under
  per-chunk TQ. The only knob that shrinks the mid-shot step is the tolerance
  band itself, which costs probes everywhere; chunk-duration uniformity was
  already shown to be a weak lever (8-24s sweep, 2026-06-30). Revisit only if
  a real viewing complaint points at mid-shot quality pumping; the fix would
  be band narrowing or tying split siblings to one CRF, not better detection.

### 2026-07-02 -- Shot-detection workers: physical/2 -> logical/2 (kept)

- **Question:** The feature run showed shot detection costs 577s (9.3% of
  wall) while host CPU sits at 39% mean / 44% max -- the 8-worker default
  (physical/2, 16 decode threads) leaves the machine mostly idle during a
  fully serial phase. Does scaling workers into that headroom convert?
- **Method/artifacts:** chunkbench sweeps via REEL_SHOT_DETECT_WORKERS
  (boundary hash must be identical -- worker-count invariance): sullyhv at
  8/12/14 workers, Sully feature at 8/16. Then the default changed to
  max(LogicalCores(),1)/2 (16 here; 32 decode threads = logical core count)
  and an end-to-end run-suite check on sullyhv.
- **Decisive result:** sullyhv detection 93.5s -> 75.4s (12) -> 71.9s (14,
  -23%); Sully feature 575s -> 415s (16 workers, -28%, -160s per 96m title).
  Boundary hashes identical everywhere (sullyhv a0d0920628547dad, feature
  6e860d05af911e7f). Scaling is sub-linear (SMT sharing), but the phase runs
  alone so the capacity is free. Note the 1500-frames-per-worker floor already
  caps 5m clips at 4 workers -- this change only helps content longer than
  ~8.3 min (at 24 fps), i.e. exactly the production case. On non-SMT boxes
  logical/2 == physical/2, so the default is unchanged there.
- **Decision:** Kept `workers = max(LogicalCores(),1)/2` in
  chunkplan.shotDetectWorkers. Per-feature serial cost drops from ~9.3% to
  ~6.7% of wall. End-to-end check
  (`perf-runs/20260702-110142-shotdet-logical-workers`, sullyhv):
  phase_shotdet_s 94.8 -> 71.6 (-24.4%), identical 110-chunk plan, quality
  clean; the run's +26s wall is probe-count noise (170 probes vs 163; sullyhv
  has ranged 162-170 across identical-plan runs -- judge worker changes by the
  phase counter and boundary hash, not this clip's wall).

### 2026-07-02 -- Hard-kill resume validated at stress-clip scale

- **Question:** Does chunk-level resume survive a hard SIGKILL mid-encode (the
  batch-crash scenario) outside unit tests?
- **Method/artifacts:** sullyhv encode SIGKILLed at t=240s (4/110 chunks done),
  rerun with the same output dir. Ephemeral test dir (removed after -- stale
  `.test-outputs` workdirs are a resume-contamination trap).
- **Decisive result:** Resume run completed rc=0 in 945s vs ~1156s fresh
  (saving ~= the killed run's banked progress); shot-cut plan reused from cache
  (0s vs 93s), completed chunks skipped immediately (progress started 4/110),
  crop re-ran (~17s, not cached -- known and cheap). Output normal.
- **Decision:** Resume is batch-safe for crash/restart. No changes needed.

### 2026-07-02 -- CVVDP setup hoist + ring depth 3: rejected

- **Question:** Does moving per-pass host setup (demuxer opens, seek preroll,
  ring fill) before the metric-pool checkout, plus a 3-deep buffer ring,
  recover the remaining 1080p GPU idle (audit estimate ~1-2% matrix)?
- **Method/artifacts:** Split Open/Compute/Close API in quality; checkout moved
  after OpenChunkCVVDP in scoreChunkProbe. `run-suite.sh --label
  cvvdp-setup-hoist air/bts/im/sully` vs the 20260702-005326 baseline.
  Artifacts: `perf-runs/20260702-032557-cvvdp-setup-hoist`.
- **Decisive result:** Total wall -1.0% (1084 -> 1073s), but bts's -3.5% came
  with 2 fewer probes (inside the ±2 noise band); excluding that, ~-0.5% --
  below the noise floor. GPU mean unchanged (air 83.6 -> 83.1). Quality
  byte-neutral. The remaining 1080p idle is CRF-search serialization
  (probe N+1 cannot encode until probe N scores), which setup hoisting cannot
  touch.
- **Decision:** Rejected; single-function ComputeChunkCVVDP restored (comment
  at the function documents the outcome). Ring stays 2 pairs.

### 2026-07-02 -- New matrix baseline; full-feature validation (probe-tail item CLOSED); CVVDP early-abort rejected on milestone data

- **Question:** (a) What is the current-code baseline after the kept 2026-07-01
  changes (decoder threads=2, slope 0.025, timer sampling)? (b) Does a real
  full feature reproduce the healthy probe behavior, and what does per-title
  planning look like? (c) Do mid-pass running scores predict final scores well
  enough to abort doomed probes (early-abort step 1)?
- **Method/artifacts:** Full default matrix (`--label tq-baseline-decode-slope`)
  then the complete Sully feature (95m50s 4K HDR, 46 GB rip) through the same
  harness, both with the new host CPU/RAM/IO sampler in run-suite.sh and
  temporary 25/50/75% running-score milestones logged per probe; a 10s
  nvidia-smi dmon log ran across the ~4h for thermal drift. Artifacts:
  `perf-runs/20260702-005326-tq-baseline-decode-slope`,
  `perf-runs/20260702-013705-feature-validation`.
- **Decisive result:** Matrix 2618s / 1090 MB vs the 2026-07-01 baseline's
  2717s / 1064 MB: -3.6% wall, +2.4% size, quality in-band everywhere.
  Feature: wall 6232s (1.08x video runtime for a 96m 4K title -- the batch planning
  number), 670 chunks, 1.39 probes/chunk, 100% converged, ZERO chunks at the
  6-probe budget, jod_min/max 9.151/9.548 (entirely in band) -- the old
  3.11-probes/chunk feature regime is gone; the deferred probe-tail item is
  CLOSED. Host telemetry: CPU 59% mean / 77% p90, min 30 GiB available -- no
  host saturation; GPU 53-56C across 4h continuous load with no clock/power
  decay (no thermal risk for multi-day batches). Shot detection is now the
  largest single serial cost at feature scale: 577s = 9.3% of the feature
  wall. Early-abort: across 1305 milestone-logged probes (357 finished
  out-of-band), running-score error vs final at the 50% milestone is
  median 0.037 but max 0.469 JOD -- wider than the whole 0.4 band -- so a
  zero-false-abort guard aborts only 8% of doomed probes (50%) / 12% (75%),
  saving ~1-2% of metric work. Far below the audit's ~3-4% estimate.
- **Decision:** New reference baseline is
  `perf-runs/20260702-005326-tq-baseline-decode-slope`. Probe-tail item
  closed. Early-abort REJECTED and the milestone instrumentation removed
  (method documented here; re-adding is ~20 lines if a future question needs
  it). Shot-detection acceleration/overlap recorded as the top open item.

### 2026-07-02 -- Nearest-chunk seed fallback: rejected

- **Question:** The replay simulation suggested seeding no-neighbor chunks from
  the nearest completed chunk instead of the median (-4 probes on sullyhv,
  moderate confidence). Real?
- **Method/artifacts:** InitialCRF median fallback -> nearest completed chunk;
  `run-suite.sh --label nearest-seed ko-5m sullyhv-15m` (the two clips with the
  most fallback seeds; anchors: 20260701-222552 for sullyhv,
  20260701-235631 for ko). Artifacts: `perf-runs/20260702-001943-nearest-seed`.
- **Decisive result:** sullyhv flat (probes 163->162, inside the ±2 noise
  band; wall -2.4% is scheduling noise). ko worse: probes 46->50 (+8.7%), wall
  +4.8%, size +3.4%. Mechanism post-mortem: beyond the 8-chunk neighbor cap a
  single distant chunk is a noisier estimator than the median -- ko's converged
  CRFs span 31.75-57.75, so "nearest" carries no locality there.
- **Decision:** Rejected; median fallback restored (comment at the fallback
  site). The seeding bundle is now fully resolved: slope 0.025 kept, default
  sweep / stagger / nearest all rejected.

### 2026-07-02 -- Content-coverage characterization: io/ko/soms first runs under current code

- **Question:** io, ko, and soms had never run under the full-scan/mw4/
  decode-threads/slope-0.025 code. What are their regimes, and does the slope
  change's early-window cost generalize?
- **Method/artifacts:** `run-suite.sh --label content-coverage io-5m ko-5m
  soms-5m`. Characterization, not an A/B. Artifacts:
  `perf-runs/20260701-235631-content-coverage`.
- **Decisive result:** io (clean CG 4K): 484s, 1.39 probes/chunk, median CRF
  38.75. ko (grain-emulation 4K): 610s -- the slowest 5m clip in the corpus,
  encode_lane 1664s, GPU 49%; digitally added grain is the 4K encode stress
  case, ~1.5x a sully-like title for batch planning. soms (light-grain 1080p):
  241s, median CRF 24.75 -- same low-CRF regime as bts and the same slope-0.025
  early-window signature (6 of 37 chunks at 3 probes) with clean quality
  (jod_min 9.169, mae 0.072). Library CRF regimes are bimodal: grainy 1080p
  ~25-26 vs everything else ~39-47.
- **Decision:** Characterization recorded; no defaults changed. ko is the
  preferred 4K encode-stress clip alongside kbv1 (grain-heavy source) for
  future encoder-side A/Bs.

### 2026-07-01 -- One initial JOD/CRF slope (0.025) for all tiers: kept

- **Question:** The replay simulation said the SDR no-information slope 0.04 is
  ~2x steeper than measured slopes (0.02-0.03) and predicted -10 suite probes
  from unifying on 0.025 (the existing HDR/4K value). Does it hold in a real
  run?
- **Method/artifacts:** `targetQualityDefaultJODPerCRF` 0.04 -> 0.025, deleted
  the per-tier split (`targetQualityLargeJODPerCRF`,
  `targetQualityInitialJODPerCRF`, `targetQualityIsHDR`).
  `run-suite.sh --label sdr-slope-025` on air/bts/im. Artifacts:
  `perf-runs/20260701-234200-sdr-slope-025`.
- **Decisive result:** Mixed per clip, positive net, and the regression is a
  proven transient. air: probes 57->46, wall 314->256s (-18.5%), the 3-4-probe
  tail collapsed (probe_hist 3+: 9 -> 1). im: neutral. bts: probes 35->42,
  wall 191->230s (+20%) -- its true slope is 0.06-0.09 (implied by probe pairs),
  so the flat default overshoots. BUT all 4 regressed bts chunks started before
  the first measured slope existed (first multi-probe chunk completed 23:49:14;
  regressed starts 23:46:28-23:47:52); the prior switches to the measured
  median after ONE learned slope, and every later chunk converged in 1 probe.
  The cost is therefore a fixed early-window ~4-8 probes per bts-like title,
  independent of title length, while air-like titles win title-long. Net on
  the set: wall -20s (-2.9%), size +2.0% pooled (in-band CRF wobble; jod_min
  unchanged, all converged). Simulator post-mortem: it missed the bts
  regression because 1-probe chunks gave it no slope data to extrapolate
  overshoots -- treat its extrapolation-certified predictions as optimistic on
  1-probe-dominated content.
- **Decision:** Kept 0.025 for every tier (also -20 lines). If feature-length
  validation shows the early window biting harder than expected, the follow-up
  is faster slope seeding (e.g. pseudo-slopes from default-seed probe + prior
  results), not a return to 0.04.

### 2026-07-01 -- UHD prime concurrency at slot target: rejected

- **Question:** With the metric decoder unthrottled, does raising the 4K
  prime-phase in-flight cap from the resolution floor (3) to the initial slot
  target (5) convert the prime-phase idle into wall? (The audit estimated
  -25-45s per 4K 5m clip, computed against the pre-decode-threads baseline.)
- **Method/artifacts:** `primeConcurrency := initialWorkers` at
  target_quality.go; `run-suite.sh --label prime-slot-target` on
  sully/kbv1/sullyhv vs anchor `perf-runs/20260701-222552-metric-decode-threads`.
  Artifacts: `perf-runs/20260701-230633-prime-slot-target`.
- **Decisive result:** Net wall ~zero: sully -9s and kbv1 -6s (at/inside the
  ~±8s noise band), sullyhv +20s WORSE. Costs were real: probes/chunk +4.7%
  (sully) / +4.3% (sullyhv) from the two extra cold-seeded prime chunks, size
  +1.1% (sully) / +2.8% (sullyhv). The prime-phase GPU idle this targeted was
  measured against the old slow-metric baseline; faster metric passes already
  drained the prime window, so the lever's premise expired.
- **Decision:** Rejected; reverted to the resolution-floor prime. Do not
  retest unless the prime phase reappears as measured idle in worker history
  (now trustworthy via the timer sampler).

### 2026-07-01 -- CVVDP source decoder 2 threads (kept); timer-based worker sampling; environment probes closed; seed-policy simulation

- **Question:** A multi-agent performance audit ranked the remaining levers. Top
  code candidates: (1) the CVVDP metric producer opens both decoders with
  threads=1 while 4K HEVC10 decodes ~23 fps single-thread against a ~50 fps GPU
  CVVDP ceiling (GPU idled at 38-42% during 4K scoring) -- does 2-thread source
  decode convert to wall? (2) perf.json worker history was sampled only at
  chunk-completion callbacks (biased instants; the baseline's 4K
  `max_active 4 < target 5` was suspected to be an artifact). Also: is the GPU
  power-limited, is PCIe degraded, and which first-probe seed policies are
  worth an encode A/B?
- **Method/artifacts:** `run-suite.sh --label metric-decode-threads` on
  air/sully/kbv1/sullyhv vs `perf-runs/20260701-001943-tq-baseline-final`;
  artifacts `perf-runs/20260701-222552-metric-decode-threads`. Correctness by
  cross-run probe comparison (scores at identical (chunk, CRF) probes must be
  bit-identical). Environment: `nvidia-smi -q`/`dmon` during the metric-bound
  air phase. Seeding: offline replay simulator over the baseline
  target-quality.json trajectories (validation-gated: reproduced all 279
  initial CRFs and 384 probe sequences exactly before evaluating policies).
- **Decisive result:** Decode threads=2: 4K metric_s -12.5..-18.1%, per-pass
  fps 10.2->12.7 (kbv1), wall kbv1 -8.8% (396->361s), sully -5.7% (437->412s),
  sullyhv -2.8% (1193->1160s), air -1.9% (noise); encode_lane_s +8-12% (freed
  CPU absorbed by encode lanes -- expected). 192 shared (chunk,CRF) probes all
  scored bit-identical; jod_min unchanged; probes/chunk and size within noise.
  Output sha256 is NOT stable run-to-run (completion-order prior
  nondeterminism), so future A/Bs must gate on probe-score identity + quality
  stats, not hashes. Sampler fix: 4K now reports max_active=5 -- the baseline's
  max_active=4 was confirmed a sampling artifact. Environment: PCIe trains
  Gen5 x8 under load (full width for the card); GPU draws 111-126 W of the
  180 W limit at 96% SM during the metric-bound phase (not power-capped;
  raising to 206 W is pointless); NVDEC unused (dec 0%), decode is CPU-side.
  Spindle stages encodes on local NVMe (same class as baselines). Seed
  simulation: measured JOD/CRF slopes are 0.02-0.03 vs the 0.04 SDR initial;
  slope 0.025 is the only policy positive under optimistic AND worst-case
  accounting (-10 suite probes, air -17%); nearest-chunk fallback adds -4
  (sullyhv). Raised default cold CRF and staggered prime seeds WASH OUT:
  content is bimodal (bts/im/kbv1 converge ~26 first-try; air/sully/sullyhv
  ~43-47), so any raised/spread cold seed trades air's win for losses
  elsewhere.
- **Decision:** Kept metricSourceDecoderThreads=2 (probe decoder stays 1; do
  not escalate to 4 threads or a src/dist producer split while encode lanes
  absorb the freed CPU). Kept the 2s timer-based worker sampler. Closed the
  power-limit/PCIe/workdir environment questions -- none hide a win. Queued
  slope 0.025 (+ optional nearest-seed) for a real A/B; rejected default-CRF
  sweep and staggered prime seeds without encodes. Simulator + report:
  `seedsim-20260701/` (seedsim.py, report.md, results.json).

### 2026-07-01 -- Current-code baseline; metric workers 4 below UHD; preset 7 rejected

- **Question:** After deleting sampled probes, which remaining performance knobs
  are worth changing: metric-worker count, SVT `level_of_parallelism`, preset, or
  target-band/probe-tail behavior?
- **Method/artifacts:** Built current `./reel` and ran `scripts/perf/run-suite.sh`
  on this box (RTX 5060 Ti). Baseline: default 6-clip matrix, then repeated after
  the metric-worker default change for a final baseline. A/Bs: HD metric-workers
  4 and 8 on air/im/bts; 4K `level_of_parallelism` 2 and 4 on sully/kbv1, plus
  lp4 on `sullyhv`; preset 7 on the full default matrix. The final baseline was
  intentionally run from the dirty worktree containing the metric-worker default
  change; `run-meta.json` records the binary SHA and
  `source-diff-internal-config.patch` preserves the source delta. Artifacts:
  `perf-runs/20260701-001943-tq-baseline-final`,
  `perf-runs/20260630-210303-tq-baseline`, `*-mw4-hd`, `*-mw8-hd`, `*-lp2-uhd`,
  `*-lp4-uhd`, `*-lp4-sullyhv`, `*-preset7`.
- **Decisive result:** Final baseline default matrix: 2717s wall, 1064 MB output;
  `sullyhv` was only 1.52 probes/chunk with 0% maxed chunks, so target-band /
  probe-tail tuning is not currently the bottleneck. HD metric-workers: 4 vs old
  6 was 683s vs 691s (-1.2%) with similar size (-0.6%) and much lower VRAM;
  8 regressed to 709s (+2.6%) and used 5.0-7.2 GiB VRAM. lp4 was mixed: -1.9%
  pooled on sully/kbv1, but +1.6% on `sullyhv`; lp2 was +0.8% on sully/kbv1.
  Preset 7 was not a useful trade: full matrix -1.1% wall but +10.3% size;
  `sullyhv` was +2.6% wall and +57.2% size.
- **Decision:** Changed below-UHD metric-worker default 6 -> 4 (UHD remains 4).
  Kept SVT auto `level_of_parallelism` policy. Kept preset 6; do not bother with
  preset 8 unless SVT behavior changes. Deferred target-band/probe-tail work
  until a future full-feature run proves the tail is back.

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
| `perf-runs/20260702-124007-tq-baseline-current/` | Current reference baseline: default matrix after decoder-threads + slope-0.025 + shot-detection logical/2 worker default, with host telemetry. |
| `shotdet-workers-20260702/` | Full-feature shot-detection worker oversubscription sweep at 16/20/24/32 workers (no default change). |
| `perf-runs/20260702-110142-shotdet-logical-workers/` | Shot-detection logical/2 worker default end-to-end check (kept) on sullyhv. |
| `perf-runs/20260702-032557-cvvdp-setup-hoist/` | CVVDP setup hoist + ring-3 A/B (rejected, sub-noise) on air/bts/im/sully. |
| `perf-runs/20260702-005326-tq-baseline-decode-slope/` | Previous reference baseline: full default matrix after decoder-threads + slope-0.025 + timer-sampling changes, with host telemetry. |
| `perf-runs/20260702-013705-feature-validation/` | Complete Sully feature (95m50s 4K HDR) validation run: probe-tail closed, per-title planning numbers, milestone dataset for the early-abort rejection, `boundary-kinds.tsv` for the mid-shot-join quality analysis. |
| `perf-runs/20260702-001943-nearest-seed/` | Nearest-chunk seed fallback A/B (rejected) on ko/sullyhv. |
| `perf-runs/20260701-235631-content-coverage/` | First current-code characterization of io/ko/soms. |
| `perf-runs/20260701-234200-sdr-slope-025/` | Unified initial JOD/CRF slope 0.025 A/B (kept) on air/bts/im. |
| `perf-runs/20260701-230633-prime-slot-target/` | UHD prime concurrency at slot target A/B (rejected) on sully/kbv1/sullyhv. |
| `perf-runs/20260701-222552-metric-decode-threads/` | CVVDP source decoder threads=2 A/B (kept) vs the final baseline; air/sully/kbv1/sullyhv. |
| `seedsim-20260701/` | Offline seed-policy replay simulator + report over the final-baseline trajectories (validation-gated; slope-0.025 winner, sweep/stagger rejected). |
| `perf-runs/20260701-001943-tq-baseline-final/` | Final current-code default baseline after the metric-worker default change; includes `source-diff-internal-config.patch` and `post-change-checks.log`. |
| `perf-runs/20260630-210303-tq-baseline/` | Pre-change default baseline used as the A/B anchor. |
| `perf-runs/20260630-215633-mw4-hd/` | HD metric-workers=4 A/B against baseline. |
| `perf-runs/20260630-220757-mw8-hd/` | HD metric-workers=8 A/B against baseline. |
| `perf-runs/20260630-222228-lp2-uhd/` | 4K `level_of_parallelism=2` A/B on sully/kbv1. |
| `perf-runs/20260630-223634-lp4-uhd/` | 4K `level_of_parallelism=4` A/B on sully/kbv1. |
| `perf-runs/20260630-225431-lp4-sullyhv/` | `level_of_parallelism=4` validation on the sullyhv stress clip. |
| `perf-runs/20260630-231627-preset7/` | Preset 7 A/B over the standard 6-clip matrix. |
| `band-investigation-20260630/` | Full-scan-vs-sampled matrix + chunk-max sweep (results.tsv, per-run logs). |

On 2026-06-30, after the full-scan change, the older raw artifacts (`perf-ab/*`,
`rebaseline-20260617/`, `vship-concurrency/`, `fulllen-attr/`, and the
full-scan/sampled A/B dirs) were pruned from `$REEL_TESTING_DIR` so future perf
testing cannot resume-contaminate on a stale `.reel-*` workdir or compare against
old-code, sampled-mode, or old-schema (`final_sample_score`/`windows`) data. Their
decisive numbers remain in the entries above; the libvship concurrency diagnosis is
in `docs/VSHIP_CONCURRENCY_BUG.md`, and preset/concurrency/metric-worker defaults
are recorded in code comments and `docs/PERFORMANCE_TESTING.md`.
