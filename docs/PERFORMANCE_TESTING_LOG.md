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

## 2026-06-07 bracket-aware search and scheduling

- **Question:** Could target-quality search reduce 4-6 probe tails without hurting
  the useful neighbor-prior cascade?
- **Method/artifacts:** `knives-5min`, `sully-5min`, and `soms`; logs were
  `~/testing/*-reellog*` plus kept `target-quality.json` files.
- **Decisive result:** Bracket-aware search with block size 32 reduced
  `knives5` 66 -> 63 probes and kept similar sampled error. Reducing the
  scheduling block 32 -> 8 raised `knives5` probes 66 -> 74 and wall 23m51s ->
  26m43s by weakening early priors.
- **Decision:** Keep bracket-aware search, conservative high-side jump gate, and
  timeline block size 32. Do not retest block size without a new regression.

## 2026-06-12 metric-worker scaling benchmark

- **Question:** Should CVVDP metric workers scale like CPU encode workers?
- **Method/artifacts:** temporary metric-only harness under
  `~/testing/metric-worker-bench/`; 1080p and 4K window scoring plus short Reel
  smoke runs.
- **Decisive result:** Metric-only throughput rose with workers but saturated and
  increased VRAM/OOM risk. 4K full Reel smoke saw no wall gain from 4 -> 6
  workers.
- **Decision:** Directional evidence only after the allocator fix; current
  defaults come from the 2026-06-19 post-restore sweep.

## 2026-06-12 TQ search simplification

- **Question:** Could accumulated search complexity be removed without changing
  measured behavior?
- **Method/artifacts:** `~/testing/tq-simplify-ab/`, two SDR runs per build plus
  one difficult 4K spot-check.
- **Decisive result:** Removed upper-grace plumbing, unused fields, and complex
  interpolation dispatch. SDR probe counts/timing stayed within run-to-run noise;
  4K differences were attributable to cliff chunks, not simplification.
- **Decision:** Kept. The configured range is accepted literally, and
  interpolation is bracketing-pair linear.

## 2026-06-12 concurrency restructure and fullvalidate tool

- **Question:** Could CPU encode and GPU metric work overlap without damaging
  target-quality priors or final quality?
- **Method/artifacts:** same-day `sully-5m-4k-hdr` and `soms-5m-1080p-sdr` A/Bs;
  `scripts/fullvalidate` added as the ground-truth ruler.
- **Decisive result:** `sully-5m` wall roughly halved (19m48s -> 9m44s) after slot
  release during scoring, async window scoring, CVVDP decode/compute overlap,
  parallel shot/crop work, and a dispatch flight gate. `soms-5m` improved more
  modestly (4m13s -> 3m43s). Ground truth found 0 chunks below range on both.
- **Decision:** Kept. The durable invariant is that scoring may release encode
  slots only with a prior-protecting flight gate.

## 2026-06-12 4K adaptive ramp: bandwidth, not capacity

- **Question:** Was 4K concurrency too low because the ramp misread free RAM?
- **Method/artifacts:** `sully-5m-4k-hdr` variants on the dev box.
- **Decisive result:** Pushing active 4K encodes near 9 left tens of GiB RAM free
  but worsened wall; per-probe encode time rose 27s -> 37s. Cap/start at 5
  (`maxWorkers/6` on 32 logical cores) beat the old slow climb by about 33s TQ
  wall on the 5m clip.
- **Decision:** Keep the bandwidth-aware 4K ceiling and start 4K at the ceiling.
  The divisor is a speed optimum, not a safety limit; retest on very different
  memory-bandwidth hardware.

## 2026-06-13 SVT-AV1 level_of_parallelism

- **Question:** Should `level_of_parallelism` derive from hardware cores or the
  resolution-aware worker target?
- **Method/artifacts:** `~/testing/perf-ab/lp-retest/`; fixed-CRF lp2/lp3 runs
  plus a unit test for bitstream identity.
- **Decisive result:** lp values 1/2/3/4/6 were byte-identical on the test path.
  Fixed-CRF 4K lp3 beat lp2 by about 3-4% at low caps; TQ-mode lp A/B was
  confounded by changed probe cascades.
- **Decision:** Auto-scale lp from the ramp ceiling, not raw hardware max. Keep
  explicit `--level-of-parallelism` for testing.

## 2026-06-13 Accuracy-trading TQ knobs

- **Question:** Should the 256-frame full-probe threshold, 3x48 windows, or 12s
  chunk cap move?
- **Method/artifacts:** accounting from existing TQ logs plus a threshold 256 vs
  144 A/B under `~/testing/perf-ab/knobB/`.
- **Decisive result:** Window-size cuts saved only about 4-9% of GPU frames on
  paper; the 12s cap rarely bound. The threshold A/B's large numeric penalty was
  later found cascade-confounded, but the direction remained unfavorable.
- **Decision:** Keep shipped knobs. Retest on the fixed MITIGATE build with
  `fullvalidate` before changing any accuracy-trading probe knob.

## 2026-06-14 Overlapping the pre-encode head

- **Question:** Is streaming chunk planning worth implementing to overlap shot
  detection with encoding?
- **Method/artifacts:** code analysis plus `scripts/chunkbench` on 5m/10m/20m
  cuts; no encode needed.
- **Decisive result:** Shot detection scales linearly (~14-15 min for a 4K
  feature), but the current planner depends on whole-file statistics and complete
  chunk lists before encoding. Streaming would change boundary decisions and
  resume plumbing.
- **Decision:** Deferred. Revisit only if 4K feature wall time makes the head a
  priority; boundary hash and `fullvalidate` become gating tests.

## 2026-06-14 Content-prior first-probe seed

- **Question:** Can existing shot-detection activity scores seed first CRFs?
- **Method/artifacts:** no encodes; joined retained frame scores with existing
  TQ logs via throwaway scripts.
- **Decisive result:** Activity-vs-final-CRF correlation changed sign by clip
  (`soms` positive, `io`/`kbv1` negative; pooled weak). A global seed would push
  some films the wrong direction.
- **Decision:** Rejected. The retained score-dump hook is useful methodology, but
  this temporal activity signal is not a safe default CRF prior.

## 2026-06-14 Full-length 4K encode attribution

- **Question:** Do 5m clip conclusions extrapolate to a full feature?
- **Method/artifacts:** full Sully 4K HDR encode, `~/testing/fulllen-attr/`,
  default then-current band `9.25-9.52`.
- **Decisive result:** Total wall 4h02m. Shot detection scaled linearly (~12m),
  crop was nearly constant, and the TQ/encode stage was super-linear because
  probes/chunk rose to 3.11 with many max/guard stops. The deep prior cascade did
  eliminate cold starts, but tight-band probe noise dominated.
- **Decision:** Methodology: short homogeneous clips can understate feature-scale
  probe cost. The main tail was later resolved by widening the default band.

## 2026-06-14 monotonicity_guard diagnostic

- **Question:** Why did feature chunks stop on `monotonicity_guard`, and could a
  flat-low early stop help?
- **Method/artifacts:** replay/analysis of recorded feature `target-quality.json`;
  no encodes.
- **Decisive result:** Guard chunks split mostly into worst-window-floor failures
  (mean in band but a sampled window just below floor) and noisy mean-bracket
  cases. The all-low population was only about 6%.
- **Decision:** Flat-low early stop rejected. The guard was not discarding
  converge-able probes; the limit was measurement noise and worst-window floor.

## 2026-06-14 Worst-window / straddle early-out

- **Question:** Could the search stop early once a floor-failing straddle exists?
- **Method/artifacts:** faithful replay simulation over all 670 Sully feature
  probe sequences; no encodes.
- **Decisive result:** Early-out saved about 30% of probes but changed 32% of
  final picks toward more overshoot; median selected probe size was about 47%
  larger for changed chunks. No quality risk, but a large file-size cost.
- **Decision:** Rejected as a default. It is a speed-vs-size knob, not an
  accuracy-neutral optimization.

## 2026-06-14 Target band WIDTH is the real probe-tail lever

- **Question:** Was the default band too tight for Reel's streaming use case?
- **Method/artifacts:** simulated wider bands over the full Sully feature probes,
  then re-encoded the feature at `9.15-9.55` and ran `scripts/fullvalidate`.
- **Decisive result:** Simulation predicted probes/chunk 3.11 -> 1.51; real
  feature landed at 1.46, wall 4h02m -> 1h57m, output 2.9 GB -> 1.2 GB. Ground
  truth mean/median were 9.411/9.424 with 3/670 below the 9.15 floor; only chunk
  0664 was a real sampled miss (true 8.689 vs sampled 9.679).
- **Decision:** Default changed to `9.15-9.55`. Principle: band width is a speed
  knob; half-width tighter than about 2x probe noise (~0.15 JOD) burns probes for
  consistency the metric/use case cannot use.

## 2026-06-14 HDR display peak 1000 vs 1500

- **Question:** Should the HDR display model use 1500 nits instead of 1000?
- **Method/artifacts:** `sully-5m-4k-hdr` A/B and later fixed-ruler re-score
  under `~/testing/rebaseline-20260617/`.
- **Decisive result:** Original 1500-nit result showing 8/33 below band was the
  old scoring cascade. Re-scoring the same bits with the fixed handler produced
  0 below. The 1500 model did not reveal a quality deficit that 1000 misses.
- **Decision:** Keep 1000-nit HDR display peak. Do not retry >1000 without a
  new, repeated sampling failure and `fullvalidate` evidence.

## 2026-06-15 High-variance clip and cascade discovery

- **Question:** Can a 15m clip reproduce feature-like probe tails cheaply?
- **Method/artifacts:** `sullyhv-15m-4k-hdr.mkv` built from hard Sully feature
  regions; repeated identical encodes during a probe-sample A/B.
- **Decisive result:** The apparent 2.78 probes/chunk hard-content tail was
  actually the VSHIP scoring cascade. After the allocator fix, `sullyhv` behaves
  around 1.55 probes/chunk but remains a useful near-floor stress asset.
- **Decision:** Keep `sullyhv-15m-4k-hdr` as deterministic stress/control input;
  do not cite the old 2.78 probes/chunk result as current behavior.

## 2026-06-18 Re-baseline accuracy ground truth on the fixed binary

- **Question:** Which pre-fix accuracy results survive the scoring-cascade fix?
- **Method/artifacts:** `~/testing/rebaseline-20260617/`; fixed binary/ruler on
  1080p SDR, easy/grainy/mixed 4K HDR, `sullyhv`, and suspect old workdirs.
- **Decisive result:** Current defaults were clean: 0 chunks below band and 0
  floored across `im-5m`, `sully-5m`, `kbv1-5m`, `ko-5m`, and `sullyhv-15m`.
  Sully feature chunk 0664 remained a real sampled miss; the HDR 1500-nit
  failure disappeared as cascade contamination.
- **Decision:** Current shipped config is trustworthy on the fixed ruler. The
  rare under-represented-segment item stays low priority.

## 2026-06-19 Metric concurrency RESTORED

- **Question:** Was concurrent CVVDP scoring inherently unsafe, or was the
  cascade caused by the libvship build?
- **Method/artifacts:** `scripts/handlertest` and A/B libvship builds under
  `~/testing/vship-concurrency/`.
- **Decisive result:** `cudaMallocAsync`'s device-global allocator pool corrupted
  concurrent handlers intermittently; `MITIGATE_MALLOC_ASYNC` made 8/8 reps
  byte-identical to serial truth. End-to-end restored runs were deterministic
  and about 1.5x faster than the temporary serialized workaround. Discipline
  lesson: a 2-rep run of one build looked clean, but the same build cascaded 4/8
  reps when repeated; two clean runs did not bound a ~50% intermittent failure.
- **Decision:** Restore one handler per metric worker. Correctness depends on a
  MITIGATE-built libvship. Re-run `scripts/handlertest` after libvship/GPU/driver
  changes. See `docs/VSHIP_CONCURRENCY_BUG.md` for the upstream bug write-up.

## 2026-06-19 Post-restore re-attribution + metric-worker sweep

- **Question:** After restoring safe metric concurrency, where are the bottlenecks
  and metric-worker defaults?
- **Method/artifacts:** `~/testing/perf-ab/post-restore/`, strict sequential
  sweep of `im-5m` (1080p) and `sully-5m` (4K), with GPU logs and TQ timing.
- **Decisive result:** 1080p wall was flat from mw4 to mw12 while VRAM climbed
  and GPU p90 stayed ~96-97%; 4K GPU mean was only ~35% and mw3->mw6 improved
  wall by only about 6% because the encoder was the limit.
- **Decision:** Change below-UHD metric workers 8 -> 6, keep UHD at 4. Treat
  metric-worker tuning as VRAM/headroom, not throughput, on current hardware.

## 2026-06-20 Preset sweep 4/5/6/7/8

- **Question:** Should SVT preset differ by resolution or move from 6?
- **Method/artifacts:** `~/testing/perf-ab/preset-ab/`, presets 4-8 over
  `im-5m`, `sully-5m`, and `kbv1-5m`, two rounds, `fullvalidate` after each.
- **Decisive result:** 1080p wall moved only about +/-4%. 4K wall improved with
  faster presets, but p7 bought only ~3% wall for +6-9% size and p8 bought
  ~12-13% wall for +11-16% size, with one clean-4K below-band chunk. Slower p4/p5
  cost 9-22% wall for only 2-6% size.
- **Decision:** Keep preset 6 for both resolutions; no resolution-aware preset.

## 2026-06-20 1080p preset 4 vs 6 across grain tiers

- **Question:** Is slower preset 4 a free 1080p efficiency win on GPU-bound
  content?
- **Method/artifacts:** `~/testing/perf-ab/preset-1080p-ab/`, preset 4 vs 6 on
  `air-5m`, `soms-5m`, `bts-5m`, plus `im` from the main sweep.
- **Decisive result:** p4 wall penalty tracked output size/content heaviness:
  about +1% on clean-light `air`, +13% on `soms`, and +38% on heavier `bts`, while
  size savings stayed only 3-8%.
- **Decision:** Preset 6 stands. 1080p bound-ness is content/bitrate-dependent,
  not resolution alone.

## 2026-06-21 4K encode-concurrency ceiling + lp retest

- **Question:** On the fixed MITIGATE build, should 4K cap rise above
  `maxWorkers/6`, and should lp change?
- **Method/artifacts:** `~/testing/perf-ab/cap-lp-retest/`; variant binaries
  changed only `uhdCoreDivisor`, with fixed-CRF and target-quality instruments.
- **Decisive result:** Fixed-CRF encoder-only throughput improved through cap8,
  but shipping target-quality did not: cap6 lp3 tied cap5 within noise, while
  cap8/cap10 were slower and aggregate encode-lane work grew about 24-32%.
- **Decision:** Keep cap5 (`maxWorkers/6` on the dev box) and lp3 at that cap. If
  a future change raises cap to 6, retest lp3 vs lp2.

## 2026-06-21 Structured phase + worker timing artifact (`perf.json`)

- **Question:** Can future agents attribute wall time without grepping verbose
  logs and rebuilding throwaway parsers?
- **Method/artifacts:** new `internal/perf` collector; unit/race/full CI; 30s
  real-encode spot checks in fixed-CRF and TQ modes.
- **Decisive result:** Kept workdirs now include `perf.json` with phase windows,
  worker history, metric workers, adaptive worker metadata, in-flight chunks, and
  encode-slot wait time. Failure paths write partial timing when the workdir
  survives for resume.
- **Decision:** Current methodology. Use `perf.json` before proposing broad
  performance changes.

## 2026-06-21 Reusable perf suite under `scripts/perf/`

- **Question:** Can standard sweeps be run/reanalyzed reproducibly without
  bespoke harnesses?
- **Method/artifacts:** `scripts/perf/run-suite.sh`, `analyze.py`,
  `compare-runs.py`, and `clips.tsv`; smoke-tested on a 30s clip in both modes.
- **Decisive result:** The suite captures env metadata, wall/size/hash, GPU util
  and VRAM, harvested `perf.json`, and `target-quality.json`. It runs strictly
  sequentially and deletes bulky workdirs unless `--keep-workdirs` is passed.
- **Decision:** Current methodology. First full matrix run is still a useful
  future baseline, but the harness itself is in place.

## 2026-06-21 Probe-IVF fsync skip

- **Question:** Can ephemeral sampled-probe IVFs skip `fsync` safely?
- **Method/artifacts:** unit test `TestSkipSyncBitstreamIdentical`, microbench,
  interleaved A/B under `~/testing/perf-ab/skipsync/`.
- **Decisive result:** Skipping fsync is byte-identical and safe for files deleted
  after scoring. Local NVMe saving was tiny (~6.2 ms per ephemeral IVF and below
  wall noise), but it scales with probe count and slow/network storage latency.
- **Decision:** Kept only for ephemeral sampled-probe windows. Durable final
  chunks and reusable full-chunk probes still fsync for resume safety.

## 2026-06-27 Shot-detection worker cap 4 vs 6 vs 8

- **Question:** Should the shot-detection worker cap (cores/4 = 4 on the 16-core
  dev box) rise to 6 or 8? Boundaries must stay identical across worker counts.
- **Method/artifacts:** `scripts/chunkbench` with a new
  `REEL_SHOT_DETECT_WORKERS` override (`Options.ShotDetectWorkers`, left 0 by
  Reel) added for the A/B; artifacts under `~/testing/shot-cap-ab/`
  (`summary-20m.txt`, `summary-feature.tsv`, per-run `.log`). Four 4K 20m clips
  (io/kbv1/ko/sully, 2 reps/cell) plus the Sully feature (96m, 1 rep/cell).
  Idle 7950X, `powersave` governor; idle run-to-run variance was <1%, so 2 reps
  bounded the small deltas.
- **Decisive result:** Boundary hash identical across 4/6/8 on all five inputs
  (exactly one unique hash each), so parallelism never perturbs boundaries.
  cap8 (cores/2) beat cap4 (cores/4) on every input: 20m clips -2% to -20%
  (io -15%, kbv1 -8%, ko -2%, sully -20%) and the Sully feature -21%
  (715s -> 565s, ~2.5 min). cap6 was a trap: slower than or barely better than
  cap4 everywhere, because `decoderThreads()/workers` floors at 2 threads/worker,
  so 6 workers get only 12 total decoder threads vs cap4's 16.
- **Decision:** Changed the default cap from cores/4 to cores/2 (8 on this box);
  boundary-equivalent (same hashes). cap6 rejected (do not use).

## 2026-06-28 Content-feature CRF prior is not viable (no free lunch, confirmed)

- **Question:** Can cheap content features predict optimal CRF well enough to
  support a content-adaptive prior that beats or supplements the neighbor prior
  (the only lever left below ~1.4 probes/chunk)?
- **Method/artifacts:** Joined per-chunk final CRF from existing
  `target-quality.json` logs (6 clips, 279 chunks: sullyhv/sully/kbv1/ko 4K HDR,
  im/soms 1080p SDR) with cheap luma features computed by decoding 3 sampled
  frames per chunk (64x36 grid: mean brightness, spatial std, within-chunk
  temporal activity). Tool + data + analysis under `~/testing/crfcorr/`
  (`main.go` drops into `scripts/crfcorr/` to re-run; analyze.py/analyze2.py).
- **Decisive result:** Content features do not predict optimal CRF:
  - In-sample per-clip R^2 (optimistic, overfit ceiling) is only 0.25-0.44.
  - Leave-one-clip-out content-only prediction: MAE 11.1 CRF vs the neighbor
    prior's 4.8 CRF. Per-clip winner is the neighbor prior on all 6 clips.
  - Adding content features to the neighbor prior makes it WORSE
    (MAE 4.78 -> 5.42; in-band rate 41% -> 34%), i.e. content is noise on top
    of neighbor.
  - Content features explain R^2 = -0.02 of the neighbor prior's residual error
    (the direction/magnitude of its misses is uncorrelated with these features).
  - The earlier per-clip Pearson r ~ -0.5 (brightness vs CRF) was a red herring:
    weak within-film structure that the neighbor prior already captures far
    better and that cannot pin down absolute CRF across content.
- **Decision:** Reject the cheap-luma content-adaptive prior. The neighbor prior
  is dramatically better and content features cannot supplement it. An exotic
  (non-luma, expensive: e.g. motion-vector or DCT-domain) predictor is the only
  untested path, but the bar is the neighbor prior's ~4.8 CRF MAE and these
  features already explain <0% of its error variance, so it looks implausible.
  Do not revisit without a concrete, different feature family and a LOCO test
  that beats the neighbor prior.

## 2026-06-28 Duplicate media-probe consolidation

- **Question:** The open item asked to size the repeated media probes
  (`GetVideoProperties` / HDR / audio / validation probes and the exact
  stream-byte scans) and consolidate only where timing showed material overhead.
- **Method/artifacts:** Sized every probe phase from existing `perf.json` across
  the standard corpus; clean single-variable A/B (current vs stashed-original
  binary) on a 12s 4K HDR clip cut with `hevc_nvenc` (`/tmp/dedup-test/`);
  byte-identical output check via `cmp`. Code in `internal/media`,
  `internal/processing`.
- **Decisive result:** Across the corpus total probe overhead is 0.05-0.42% of
  wall. The one material duplicate was `video.Probe` (open + decode first frame,
  ~0.7s on 4K): it ran twice per encode - once inside `GetHDRInfo` ("HDR
  analysis") and again at the top of `ProcessChunked` ("Video probe"). Sharing
  one probe between HDR refinement and the chunked pipeline took the clean 4K
  HDR clip from 0.2296s to 0.1149s analysis-probe time (one first-frame decode
  removed; ~0.7s saved per real 4K encode) with byte-identical AV1 output. A
  second literal duplicate - the orchestrator calling `GetAudioChannels` (which
  internally called `GetAudioStreamInfo`) and then `GetAudioStreamInfo` again -
  was removed by deriving channel counts from the single audio probe. Validation
  re-opens the output several times but the total is ~3ms (container probes are
  bounded by `probesize`), and the `GetVideoStreamBytes` scans are O(file size)
  but not duplicated (input vs output), so both were left alone per the gate.
- **Decision:** Kept the `video.Probe` (x2 -> x1) and audio (x2 -> x1) probe
  dedups; left validation opens and stream-byte scans unchanged. `perf.json`
  shape changes slightly: "Video probe" now records once in the orchestrator and
  "HDR analysis" is near-zero (refinement only); `scripts/perf/analyze.py` reads
  phases nil-safely so it is unaffected. Resolves the open item.

## 2026-06-28 CVVDP distance-space window aggregation (rejected)

- **Question:** Borrowed from xav: aggregate the per-window sampled-score mean
  in CVVDP perceptual-distance space instead of JOD space, matching how
  full-chunk CVVDP pools internally. Hypothesis was that the JOD-space mean is
  optimistically biased vs ground truth.
- **Method/artifacts:** Encode-free. (1) Isolated CRF-selection effect by
  recomputing each probe's chunk score both ways from stored per-window scores
  in existing kept workdirs (`/tmp/jod_ab_isolate.py` over sullyhv-15m and the
  full Sully feature, 670 chunks). (2) Honesty A/B against ground truth by
  re-scoring the already-encoded output with `fullvalidate` on the existing
  sullyhv workdir (`rebaseline-20260617/sullyhv-15m-4k-hdr/`), using the new
  `FULLVALIDATE_JSON` dump, then paired old-vs-new sampled-vs-truth
  (`/tmp/jod_honesty.py`). Sanity: JOD recompute reproduced stored probe scores
  to ~1e-6, validating the harness.
- **Decisive result:** No CRF-selection benefit and slightly worse honesty:
  - Score shift ~0.0001 JOD mean / 0.003 max (15-75x below the ~0.075 JOD probe
    noise); 0/110 CRF flips on sullyhv (1/670 on the feature, a pathological
    below-floor tie-break, not a quality driver).
  - Per-chunk |sampled - truth| mean gap on sullyhv: 0.03330 (old JOD-space) ->
    0.03341 (new distance-space). Paired winner over multi-window chunks: new
    closer 1, old closer 36.
  - The convexity/Jensen math is correct (distance mean IS lower), but the
    premise was wrong: on this clip ground truth runs ABOVE the sampled mean
    for 36/37 multi-window chunks (sparse sampling undershoots by ~+0.099 JOD),
    so pushing the sampled score further down moves it away from truth.
- **Decision:** Rejected (reverted in 7b25d77). The real sampled-vs-full driver
  is sampling sparsity + the arithmetic-vs-CVVDP-Minkowski-norm mismatch, not
  the JOD-vs-distance transform. A genuinely different lever would be pooling
  windows with CVVDP's Minkowski norms (temporal ~2, spatial ~4) in distance
  space -- a larger change needing its own fullvalidate; do not revisit the
  pure space-swap, it is a net negative. *(Correction 2026-06-28, see the
  Minkowski-norm entry below: that proposed lever was also tested and
  rejected -- the bias-compensation insight there supersedes this "next
  lever" note.)* The validation did leave one durable dev-tool improvement:
  `fullvalidate` now supports `FULLVALIDATE_JSON` for all-chunk ground-truth
  dumps (681248d).

## 2026-06-28 Monotone-cubic (PCHIP) CRF interpolation for the probe tail (rejected)

- **Question:** Borrowed from xav: escalate CRF interpolation with probe count
  (2 probes -> lerp, 3 -> Fritsch-Carlson monotone cubic, 4+ -> PCHIP with
  overshoot clamping) instead of reel's fixed linear-on-bracketing-pair, to
  shave probes off the 3+-probe tail. The probe tail is the dominant
  feature-length cost, so even a small tail win matters.
- **Method/artifacts:** Encode-free. (1) Attack surface from existing
  `target-quality.json` logs across 1617 chunks (`/tmp/pchip_surface.py`).
  (2) Leave-one-out curve fidelity: hold out one probe, fit linear vs a faithful
  PCHIP port (xav's `interp/scalar.rs` MAX_TAU2=9.0) on the rest, predict the
  held-out point, average folds (`/tmp/pchip_loo2.py`). Two directions tested:
  crf->score reconstruction (forward curve) and score->crf prediction at the
  target (reel's actual `InterpolateCRF` use). Queries restricted to interior/
  bracketed only, since reel only interpolates when probes bracket the target.
- **Decisive result:** Small surface, and PCHIP is worse at reel's actual use:
  - Surface: 64.9% of chunks converge in 1 probe, 27.4% in 2; only 7.7% reach
    3+ probes (~164 bracketed interpolation decision points total).
  - Test A (crf->score forward fidelity, 127 folds): linear 0.117 vs PCHIP
    0.103 JOD err -- PCHIP 12% better as a *forward* curve model.
  - Test B (score->crf at target, reel's use, 51 chunks): linear 0.357 vs
    PCHIP 1.221 CRF err -- PCHIP 242% worse; paired winner linear 46, PCHIP 5.
  - Mechanism (chunk 82, hold out ideal probe): linear predicts crf 27.22
    (err 0.03) vs PCHIP 28.89 (err 1.64). It is general, not a fluke.
- **Decision:** Rejected, no code change. Reel interpolates in the score->crf
  (inverse) direction, which is steep: scores are squished into ~8.5-9.9 JOD
  across a wide CRF range (13-46), so small forward-curve slope errors amplify
  into large CRF errors. In that steep-inverse regime the bracketing pair is
  the most locally informative data and the far anchors inject cubic wiggle the
  inverse amplifies. Reel's bracket-pair-only linear is therefore a robustness
  feature for the inverse direction, not a limitation; a smoother forward-curve
  fit makes the inverse prediction materially worse and would ADD probes. This
  confirms the doc note that bracket-aware linear is the settled choice; do not
  revisit interpolation unless search direction changes to crf->score.

## 2026-06-28 CVVDP Minkowski-norm window pooling in distance space (rejected)

- **Question:** The distance-space entry above named "pool windows with CVVDP's
  Minkowski norms (temporal ~2, spatial ~4) in distance space" as the next
  lever to test for sampled-vs-full honesty. Pursue it.
- **Method/artifacts:** Encode-free. Reused the sullyhv ground-truth dump from
  the distance-space entry (`/tmp/sullyhv_fullvalidate.json` from fullvalidate on
  `rebaseline-20260617/sullyhv-15m-4k-hdr/`) plus per-window scores. For each
  multi-window final probe, recovered exact window distances via the JOD inverse
  (Vship `toJOD`/`fromJOD`), computed frame-weighted Minkowski-p for p in {1,2,4},
  mapped back to JOD, compared to full-chunk truth (`/tmp/minkowski_sweep.py`).
- **Decisive result:** Higher Minkowski norms monotonically WORSEN honesty:
  per-chunk mean |score - truth| over 37 multi-window chunks:
    JOD-space arithmetic (reel mean): 0.03665 (paired winner 26/37)
    dist p=1 (arithmetic, = distance-space entry): 0.03694 (3)
    dist p=2 (Vship temporal RMS):                  0.04200 (3)
    dist p=4 (Vship spatial):                        0.05870 (4)
  The pure JOD-space arithmetic mean is the best truth predictor; matching
  Vship's norms moves away from truth.
- **Mechanism (the real finding):** If reel's windows partitioned the chunk and
  were pooled with distance-space Minkowski-2, reel would EXACTLY reconstruct
  full-chunk truth (Vship's temporal norm is Minkowski-2 over per-frame
  distances). But reel's windows do not partition -- they deliberately OVER-
  SAMPLE the hard parts (spread-based extra windows + worst-window targeting for
  the floor guard). A faithful distance-space norm therefore OVER-scales that
  sampling bias toward low JOD, moving further from truth. The JOD-space
  arithmetic mean "accidentally" corrects the bias upward. Truth runs ABOVE the
  per-window mean on 26/37 chunks, so any more-conservative aggregation is
  worse. (Side note: the production spread-blend score is 0.099 mean err vs
  truth -- 2.7x the pure mean's 0.037 -- but that cost is deliberate worst-frame
  conservatism, not truth-tracking; truth is at/below the worst window on 0/37
  chunks. It is a speed-vs-conservatism knob, do not touch without a real-encode
  A/B + user coordination.)
- **Decision:** Rejected, no code change. Closes the "next lever" proposed in
  the distance-space entry -- that hypothesis is disproven. Transferable lesson:
  reel's window aggregation and its sampling are co-designed (sampling is biased
  toward worst segments for the floor guard), so any "more faithful to Vship"
  aggregation that assumes uniform-frame pooling will fight the bias rather than
  complement it. Do not revisit aggregation-space/norm swaps. The only honest
  lever left is smarter sampling (cover the chunk instead of biasing to hard
  parts), which is the larger streaming-planner item already flagged as deferred
  and accuracy-affecting in PERFORMANCE_TESTING.md.

## 2026-06-28 Probe-loop video.Open/Probe overhead is overlap-hidden (rejected)

- **Question:** Every sampled-probe window opens the source twice (once in
  `encodeProbe`, once in `ComputeChunkCVVDP`) plus `video.Probe`+`video.Open`
  on the probe IVF, and `video.Open` runs `anchorFrameOrigin` (decode 1 frame +
  seek) plus `avformat_find_stream_info`. This is currently lumped into
  `encode_seconds`/`metric_seconds` in target-quality.json and is unmeasured.
  Is pooling/sharing a `video.Source` across probe windows a material lever?
- **Method/artifacts:** Throwaway `scripts/openbench` (since removed) using the
  real reel `video` package against source files and REAL 48-frame probe IVFs
  from kept workdirs (rebaseline-20260617). Measured PURE Open/Probe cost (no
  frame reads, no encode, no GPU), median of 40 reps, stable across 2 runs.
  Cross-checked wall impact against existing perf.json duty-cycle data instead
  of a real encode.
- **Decisive result:** Per-window overhead is real in isolation but ~90%
  overlap-hidden by the probe/score pipeline, so gross cost does not translate to
  wall:
  - 4K HDR (sully): Open(SOURCE) 11.8 ms, Open(PROBE_IVF) 20.3 ms,
    Probe(PROBE_IVF) 31.2 ms, per-window total 75 ms; ~226 s gross/feature.
  - 1080p SDR (im): 4.3 / 11.9 / 18.6 ms, per-window total 39 ms.
  - But score-goroutine opens overlap the next window's encode and the encode
    slot is released before waiting for scores (gatherWindowScores). perf.json
    confirms the duty cycle: 1080p metric/encode = 2.06x with 8x overlap
    (135 s wall for 1083 s of encode+metric work) -> GPU-bound, every
    source-open buried behind the bottleneck, ~0 wall impact. 4K metric/encode
    = 0.85x -> encoder-bound; only `encodeProbe`'s Open(SOURCE) before each
    window is serial on the critical path, ~24 ms/probe, ~0.3% of feature wall.
  - The probe-IVF open+probe (51 ms/window on 4K) is the larger cost, not the
    source; it is also overlap-hidden today and would only gain relevance if
    full-first generalization removed the hiding encode.
- **Decision:** Rejected, no code change. Source-open overhead is in the
  sub-1% tail and overlap-hidden; falls under the existing "do not propose new
  performance work in the sub-1% tail" rule. Pooling a `video.Source` is not
  worth the thread-safety complexity (Source is not concurrency-safe; the encode
  goroutine and the per-window score goroutine cannot share one). Do not revisit
  standalone source/IVF pooling. Only reconsider probe-IVF reuse if bundled
  into a full-first change that removes the overlap-hiding encode.

## 2026-06-28 Generalize full-first probe to neighbor-seeded chunks (rejected) -- REALISM CHECK CONTAMINATED, see correction below

> **CORRECTION 2026-06-28 (scope check):** The "Realism" bullet below
> (winrescore 100% flip, +0.66 gap, "never converges") was CONTAMINATED by the
> same re-cut-source artifact as the over-encoding entry. winrescore scored
> windows from the rebaseline-20260617 final IVFs (encoded from the ORIGINAL
> 3840x2160/1920x1080 sources) against the RE-CUT sources -- the mismatch
> inflated scores ~+0.5 toward the CVVDP ceiling, manufacturing the 10.000
> window blends and the "100% flip." Re-run on FRESH workdirs (source==IVF,
> valid) over n=6 1-probe neighbor sampled chunks (sully-5m clean, kbv1 grain,
> im 1080p): mean gap +0.056 (WITHIN ~0.075 probe noise), flip rate 1/6 = 17%.
> The gap is not even directionally consistent (sully clean +0.16, kbv1/im
> slightly negative). So full-first convergence ~= sampled convergence; the
> frame-budget replay (ungated coin-flip, gated F<=264 +EV) is roughly
> trustworthy. full-first is NOT rejected -- it is a plausible-but-unverified
> lever (gated F<=264 looks +EV in replay with a small valid flip rate). Not
> pursued now (performance tuning closed); would need a fresh-encode A/B to
> confirm. The "standalone-window pessimism / over-encodes clean content"
> mechanism/corollary below is ALSO void (refuted by the over-encoding scope
> check). Read the corrected conclusion; do not act on the contaminated bullets.

- **Question:** `targetQualityFullFirstProbe` reuses a full-chunk probe as the
  final output when it converges, eliminating the separate final re-encode. It
  is gated to `initial_crf_source == "median"` and <=720 frames, which in
  practice fires on ~0 chunks (after priming nearly all chunks are
  neighbor-seeded). TQ logs show the final re-encode is 9-25% of encode work
  (1080p ~25%, 4K ~18%, feature ~15%), almost all from 1-probe neighbor-seeded
  chunks doing sampled-probe + full final-encode. Could extending full-first to
  neighbor-seeded chunks recover that as a speed win?
- **Method/artifacts:** Encode-free. (1) Frame-budget replay over 36 TQ logs
  (`/tmp/fullfirst_replay.py`, `/tmp/fullfirst_sizegate.py`) modeling the
  per-chunk economics: a 1-probe chunk SAVES W=144 frames (probe reused as
  final); a k>=2 chunk WASTES F-144 (round-1 full encode not reused). (2) The
  decisive realism check: full-first scores the 3 sampled windows FROM the full
  encode (a window-BLEND score, NOT the full-chunk score), so its convergence
  depends on whether mid-GOP window scores match standalone-window scores.
  Measured directly with a throwaway `scripts/winrescore` (since removed) that
  re-scored the same 3 windows from existing final-chunk IVFs in kept workdirs
  (rebaseline-20260617 sully-5m clean and sullyhv hard) using reel's exact
  spread-blend -- CVVDP-only, no encoding.
- **Decisive result:** The replay's optimistic frame budget is illusory because
  the convergence-preservation assumption is FALSE.
  - Frame-budget replay (assuming convergence unchanged): ungated is a coin-flip
    because save (144) ~= waste (F-144) per chunk at the 4K cap F~=264-288;
    gating to F<=264 excludes the 15 max-duration chunks (33% 1-probe rate) and
    looks +EV (+9,896 frames over 173 candidates, the F=264 bucket converges
    68% vs a 45% break-even).
  - Realism (winrescore, the correct method): **100% flip on BOTH clean and hard
    content.** Every 1-probe neighbor sampled chunk scores 10.000 when its 3
    windows are scored from the final IVF, vs ~9.2-9.5 standalone. Gap is +0.66
    mean (sully-5m clean) and +0.66 (sullyhv hard). So full-first's probe-1
    lands ~10.0, far above the 9.35 band, and never converges in 1 probe --
    every projected "save" becomes a multi-probe waste.
  - Mechanism: a standalone 48-frame window starts with a keyframe and has no
    prior references, so at the same CRF it is QUALITY-PESSIMISTIC. The same
    frames mid-GOP in a full encode benefit from inter-frame prediction and go
    near-transparent on compressible content. The sampled search's
    standalone-window convergence does NOT transfer to full-first's
    mid-GOP-window convergence.
  - Corollary (a reframe of the existing "sampled-vs-full" open item, not a new
    lever): the standalone pessimism means the sampled search OVER-ENCODES. At
    the CRF where standalone windows hit target 9.35, the real full encode is
    ~9.9-10.0 (clean) -- fullvalidate on sully-5m showed full-chunk JOD
    median 10.0, 31/33 above band, +0.54 sampled-vs-full gap. Reel is encoding
    clean content at ~9.9 JOD when 9.35 is targeted = larger files than needed.
- **Decision:** Rejected, no code change. full-first generalization is NOT a
  speed lever; it would ADD probes on essentially all content because mid-GOP
  window quality >> standalone window quality. Do not revisit full-first
  generalization. The shipped median-gated full-first is dormant (0 chunks on
  typical runs) so this latent overshoot is unobserved, but it would also
  overshoot if it fired on clean content. The over-encoding corollary is the
  already-open "sampled-vs-full / smarter sampling" accuracy item: it is a
  SIZE lever (and indirectly slight speed via higher CRF), but the Minkowski
  entry showed naive bias corrections fight the floor guard, so it needs a real
  real-encode A/B + user coordination, not tuning. The decisive validation
  method (re-score windows from existing final IVFs) is reusable for that item.

## 2026-06-28 Over-encoding prize measured; fixed offset over-corrects (A/B) -- CONTAMINATED, see correction below

> **CORRECTION 2026-06-28 (scope check):** This entry's "current" measurement
> (/tmp/sully5m_fullvalidate.json, real mean 9.935) was an ARTIFACT. The
> rebaseline-20260617 workdirs were encoded from the ORIGINAL 3840x2160/
> 1920x1080 sources (with crop), but those sources were later RE-CUT to the
> cropped output dims (3840x1600 / 1920x816), and the fullvalidate below scored
> the re-cut sources against IVFs encoded from the original sources. If the
> re-cut is not pixel-faithful to the original crop, the scores are invalid;\> the rebaseline sully-5m scored mean 9.935 (+0.585) this way. A FRESH sully-5m
> encode at 9.35 from the re-cut source (source==IVF throughout, identity crop)\> scored mean 9.435 (+0.085, 29/33 in band) -- WELL-CALIBRATED, not over-encoded.
> The +0.585 "over-encoding" and the 57% size win do not exist; the corrected
> (8.80) encode landed below band only because there was no real over-encoding
> to correct. The fixed-offset "over-correction" finding is also void (it was
> correcting an artifact). See the next entry for the valid corpus-wide scope.
> The mechanism story (standalone-window pessimism) was ALSO refuted: full-chunk-
> probe chunks (no standalone pessimism) showed the same +0.6 artifact, and fresh
> full-chunk-probe chunks show gap 0.000 (production==fullvalidate). Do not act
> on this entry; read the next one.


- **Question:** The full-first entry's corollary -- the sampled search
  over-encodes clean content because standalone-window probes are
  quality-pessimistic (full ~9.9 vs 9.35 target on sully-5m) -- is a SIZE
  lever. Size it with one real encode, and test whether a fixed proxy-target
  offset captures it.
- **Method/artifacts:** Real-encode A/B on `sully-5m-4k-hdr` (clean 4K HDR).
  Current: target `9.15-9.55`. Corrected: target `8.60-9.00` (proxy center
  lowered by the measured +0.585 sampled-vs-full gap). Both fullvalidated
  against the REAL band `9.15-9.55`. Workdir + fullvalidate JSON under
  `~/testing/ab-overencode/` and `/tmp/sully5m_*_fullvalidate.json`.
- **Decisive result:** The prize is real and large, but a fixed offset is
  unsafe.
  - Current: real mean JOD 9.935, 31/33 above band, 0 below; video stream
    33.3 MB. Over-encoding ~+0.585 JOD on clean content.
  - Corrected (proxy target 8.80): real mean JOD 8.909, **29/33 BELOW the
    real 9.15 floor**, 0 above; video stream 14.4 MB (-57%); muxed 58.1 ->
    38.9 MB (-33%).
  - A 0.55 proxy-target cut produced a 1.03 JOD real swing (9.935 -> 8.909):
    the proxy->real mapping is NONLINEAR. Near saturation (baseline at JOD
    10.0) the sampled proxy is nearly CRF-insensitive, so a given proxy cut
    yields a larger real cut. A global offset therefore over-corrects and
    under-encodes -- hard confirmation of the Minkowski entry's "naive bias
    corrections fight the floor guard" warning, with numbers.
  - Capturing the prize the crude way costs speed: probes/chunk 1.36 -> 2.39
    (+76%), wall ~7m -> 9m41s (+40%), because the search marches CRF up through
    the near-saturation flat region where sampled quality barely moves per
    CRF step (3/33 chunks hit the SVT CRF max 63.75). A real per-chunk bias
    model would also correct the prior/initial-CRF, so this march is partly an
    artifact -- but it proves the saturation region is expensive to climb.
- **Decision:** Evidence recorded; no code change. Elevates the existing
  "sampled-vs-full / smarter sampling" open item from low to "the one real
  lever left, and it is a SIZE lever (with a possible speed co-benefit or cost
  depending on prior correction), not a speed knob." The viable path is a
  PER-CHUNK bias model that infers each chunk's proxy->real correction from its
  own probe signal (e.g. how high the standalone score sits / compressibility),
  raises CRF on over-encoded chunks, leaves hard chunks (small bias) alone, and
  keeps the floor guard safe in REAL terms. It is accuracy-trading research,
  not tuning -- gated by fullvalidate across clean + sullyhv + a feature and
  coordinated with the user. Do NOT ship a fixed proxy-target offset: it
  under-encodes. Current "over-encode clean content to near-transparency" is a
  safe, high-quality place to sit until a per-chunk model is validated.

## 2026-06-28 Over-encoding was a measurement artifact; reel is well-calibrated (scope check)

- **Question:** Was the +0.6 over-encoding (prior entry) real across content
  types, or an artifact of limited/invalid testing? Scope it across the corpus.
- **Method/artifacts:** fullvalidate across the standard corpus. Discovered the
  rebaseline-20260617 workdirs are INVALID for fullvalidate: their sources were
  re-cut to cropped dims AFTER encoding, so scoring the re-cut source against
  the (original-source) IVFs is a source mismatch. Re-encoded FRESH from the
  current sources (source==IVF throughout, identity crop) for the clips whose
  rebaseline workdir was stale: sully-5m, air-5m, bts-5m, im-5m, kbv1-5m.
  fullvalidate JSONs under /tmp/scope_*.json and /tmp/sullyhv_fullvalidate.json.
- **Decisive result:** The over-encoding was an artifact. reel is well-calibrated.
  - VALID measurements (fresh encode, source==IVF, or prior-session pre-re-cut):
    sully-5m clean 4K +0.085, air-5m clean-light 1080p +0.019, bts-5m heavy
    1080p +0.050, im-5m mod 1080p +0.099, kbv1-5m mod-grain 4K +0.040, sullyhv
    hard 4K (15m) +0.083. ALL land IN band (9.15-9.55), mean +0.02 to +0.10 over
    the 9.35 target, 0 below, 0 at CVVDP ceiling -- across 1080p clean-light /
    mod / heavy and 4K clean / mod-grain / hard.
  - INVALID (rebaseline, re-cut source vs original-source IVFs): sully-5m
    rebaseline showed +0.585 (vs +0.085 fresh -- a +0.5 artifact inflation),
    ko/kbv1/im rebaseline showed +0.650 capped (artifacts). The artifact
    direction is INFLATION (re-cut source scored the original-source encode as
    nearer-transparent than reality).
  - Refutes the mechanism story too: full-chunk-probe chunks (<=256 frames, no
    standalone-window pessimism) showed the SAME +0.6 artifact as sampled
    chunks, and fresh full-chunk-probe chunks show gap 0.000 (production probe
    score == fullvalidate). So standalone-window pessimism is NOT the driver;
    the prior winrescore +0.66 "flip" was the same source-mismatch artifact
    (winrescore scored final IVFs from the rebaseline workdir against the
    re-cut source).
- **Decision:** No code change. reel is well-calibrated across the tested
  content types (real lands ~+0.05-0.085 over target, in band). There is NO
  +0.6 over-encoding and NO 57% size prize. The fast-TQ sampling foundation is
  sound on accuracy. Corrects the prior over-encoding entry (marked
  CONTAMINATED). Methodology lesson (already in the docs but reinforced): a
  kept workdir is only valid for fullvalidate if the SOURCE file is unchanged
  since the encode; re-cutting a source (e.g. removing letterbox) after encode
  silently invalidates the workdir. The open "sampled-vs-full proxy bias" item
  is real but SMALL (~+0.08, in band), not the +0.6 the artifact suggested; it
  stays low priority. Do NOT pursue a proxy-target offset or per-chunk bias
  model on the basis of the prior entry -- the premise was an artifact.

## 2026-06-28 Probe-preset decoupling (probes faster than final) -- not promising

- **Question:** Probe outputs are thrown away (they only find the CRF); the
  final chunk is re-encoded at preset 6 regardless. Could probes run at a
  faster preset (8-10) to shrink the dominant cost (probes are 82-91% of 4K
  encode work)? Distinct from the 2026-06-20 final-output preset sweep, which
  ran probes+final at one preset. This is untested and tests whether decoupling
  helps where the GPU sits idle (4K, metric/encode ~0.85x).
- **Method/artifacts:** Encode-free, from archived `perf-ab/preset-ab/` all-pX
  runs. (1) CRF offset at fixed JOD: mean final CRF per preset per clip from the
  `-tq.json` logs. (2) Wall: probe vs final encode-seconds breakdown and whole-
  encode p6-vs-p8 elapsed from `results.tsv`. No new encodes.
- **Decisive result:** Modest, content-dependent wall win that costs size on
  clean content, plus a directionally-inconsistent CRF offset.
  - CRF offset p6->p8 at the same 9.35 target: sully clean 4K -1.3 CRF (p8
    converges LOWER), kbv1 grain 4K +1.3, im mod 1080p +1.3. The offset is
    small (~0.03-0.05 JOD, within probe noise) but its SIGN FLIPS by content,
    so a fixed correction model would push ~half the library the wrong way.
  - Wall: whole-encode p8 is 12-13% faster than p6 on 4K (sully 444->389s, kbv1
    419->366s), but probe-decoupling only speeds the probes (final stays p6), so
    its win is less than whole-p8 -- roughly ~7-9% on 4K, ~0 on 1080p (GPU-bound).
  - Size on clean content works AGAINST it: sully's p8 probes converge to a CRF
    ~1.3 LOWER than p6 needs, so the p6 final at that CRF is ~8-10% BIGGER. So
    on clean 4K (the content the 4K win would matter most for), decoupling
    trades ~7-9% wall for a size INCREASE -- the wrong direction for a
    streaming library. Grainy 4K (kbv1) gets wall+size; 1080p gets neither.
  - Recall whole-p8 (the full 12% wall win, quality mostly in band) was already
    rejected for the +11-16% size. Probe-decoupling is a SMALLER wall win than
    that, traded for size that is only preserved on non-clean content.
- **Decision:** Not promising; not pursued. A real decoupled A/B could surprise
  (e.g., if the clean-4K size penalty is smaller than the CRF offset implies),
  but the archived offset+wall data make it a narrow, content-dependent payoff
  that increases size on exactly the content (clean 4K) where the win would
  matter, with no clean correction path (sign-flipping offset). The 2026-06-20
  preset sweep is the final-output preset decision and is unrelated. Recorded
  here so this idea is closed with evidence rather than re-surfacing.
