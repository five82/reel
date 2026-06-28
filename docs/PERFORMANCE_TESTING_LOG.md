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
