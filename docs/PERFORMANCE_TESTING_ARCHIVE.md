# Performance Testing Archive

Raw historical detail moved out of `docs/PERFORMANCE_TESTING_LOG.md` to keep the active log digestible.
These entries are contaminated, superseded, or otherwise not authoritative for current defaults.

## 2026-06-13 4K metric workers 4 vs 6 retest

> **CONTAMINATED -- this raw comparison was the cascade.** The "mw6 took more probes" result was the
> handler-corruption cascade (6 handlers cascade more than 4 on the old async build), misread here as a
> prior-cascade *timing* confound. With concurrency restored 2026-06-19, worker count is again concurrent
> GPU handlers (not "decode only"), so the 4-vs-6 question exists again -- but the 2026-06-12 scaling
> benchmark already answers it (4K saturates ~6, default 4), so keep 4. Treat this entry's numbers as
> cascade-era and do not cite them.

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

## 2026-06-14 HDR display peak luminance 1000 vs 1500 (reviewed, tested, reverted)

> **RESOLVED 2026-06-18: the "8/33 below band" was the scoring cascade, not under-sampling** (see
> "Re-baseline accuracy ground truth on the fixed binary"). Re-scoring the same 1500-nit bits with the
> fixed handler moves it to mean 9.448 / 0 below / gap 0.040 -- the catastrophic chunks collapse to ~0
> gap. So the "sampling representativeness gap" described below is a cascade artifact, not a real
> highlight-coverage deficit. The revert-to-1000 decision is unaffected (1500 still buys no real quality).

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

> ROOT CAUSE CORRECTED + SUPERSEDED 2026-06-19: the cause was the libvship build (the deprecated
> `buildcuda` target's `cudaMallocAsync` device-global pool racing across handlers), NOT an inherent
> Vship limitation. The one-shared-handler + `gpuMu` fix below was a correct workaround; metric
> concurrency has since been RESTORED via a `MITIGATE_MALLOC_ASYNC` libvship rebuild. See
> "2026-06-19 Metric concurrency RESTORED". This entry is kept for the cascade's symptom fingerprint,
> the amplifier mechanism (still-latent robustness gaps), and the diagnostic method -- NOT the fix
> mechanics, which are no longer in the tree.

While setting up a `probe_sample_frames` A/B, two runs of the *identical* config (sf48, default
band, preset 8) on the same clip diverged enormously. Root-caused over 06-15/06-16 to a GPU
concurrency bug in CVVDP scoring (final root cause: the libvship async-allocator race, see 2026-06-19).

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

### Diagnosis (durable method; final attribution corrected 2026-06-19)

The diagnosis ruled out, in order:

- **NOT source frame-misalignment.** `scripts/aligncheck` decodes the concatenated clip
  sequentially as ground truth, then calls `video.ReadFrame(N)` on fresh Sources (the scoring
  access pattern). Result: 0 mismatches sequential, **0/480 mismatches under 8-way concurrency,
  with the autocrop applied** -- source decode is correct and deterministic. (This also kills the
  "xav-style sequential source pairing" fix idea: the layer it changes is already correct.)
- **NOT a concatenation/seek issue.** The concatenated clip simply has more chunks (110) and so
  more concurrent-scoring exposure (see trigger below) -- a red herring, not a cause.
- **NOT inherent metric noise.** `fullvalidate` at **workers=1** is byte-identical across two
  passes (mean 9.4757, 0 below); at **workers>1** it corrupts (9.17/31-below). So the defect is
  *concurrency*. Serializing GPU *compute* with a mutex reduced but did not fix it; only collapsing
  to one handler did.

This entry concluded "N coexisting handlers share/clobber global GPU state." The 2026-06-19
root-cause work pinned it precisely: each handler's per-frame `cudaMallocAsync` draws from a
device-global stream-ordered pool shared across handlers, and concurrent alloc/free races that pool --
which is why even *serialized compute* on >1 handler stayed corrupt (the race is in alloc/free, not
kernels). See "2026-06-19 Metric concurrency RESTORED".

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

### The fix (superseded -- see 2026-06-19 restore)

The original fix serialized scoring behind one shared VSHIP handler + `quality.gpuMu`, which removed the
cascade at a real throughput cost (~1.7x slower preset-8 wall). The 2026-06-19 restore replaced it with N
concurrent handlers on a `MITIGATE_MALLOC_ASYNC` libvship -- same correctness, no serialization tax (~1.5x
faster than the serialized build). Those serialization mechanics are no longer in the tree; see
"2026-06-19 Metric concurrency RESTORED" for the current code state and end-to-end validation.

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

- **2026-06-12 metric-worker scaling benchmark** -- VALID AGAIN post-restore. It measured GPU
  *throughput* scaling from N concurrent handlers (1080p saturates ~8, 4K ~6). The serialization fix
  briefly removed that parallelism; the 2026-06-19 restore put concurrent handlers back, so the curve
  applies again (directional now, since it was measured on the old async build). Worker defaults (8/4)
  stand and were re-validated.
- **2026-06-13 4K metric workers 4 vs 6** -- CONTAMINATED (the raw mw6 comparison was the cascade:
  io a: 82 probes / 2.28 per chunk was 6 handlers cascading more than 4 on the old async build). With
  concurrency restored, the 4-vs-6 question exists again, but the 06-12 benchmark already answers it
  (keep 4); do not cite this entry's raw numbers.
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


## 2026-06-18 Metric serialization was the preset-6 wall bottleneck (motivation for the restore)

> Resolved by the 2026-06-19 concurrency restore; kept as the "before" baseline and one durable lesson.

Under the (since-removed) single-shared-handler serialization, all CVVDP compute ran on one serial GPU
lane, so `sum(metric_seconds)` was a hard floor on wall time. Re-reading the fixed-binary preset-6
re-baseline logs, the serialized metric was **81-91% of wall at preset 6, for 4K and 1080p alike** (e.g.
sullyhv-15m 4K: metric 1796s of 1963s wall = 91%; im-5m 1080p: 128s of 151s = 85%). The **1963s serialized
sullyhv baseline** is the reference the 2026-06-19 restore's "~1.5x faster" speedup is measured against.

Durable lesson: the "encoder dominates at slow presets" intuition was wrong. The encoder's compute is
larger in sum (metric is only ~33-39% of total compute), but encodes parallelize across workers while a
serial metric does not -- so the encode work compresses away and the serialized metric binds wall clock.
Slowing the encoder (preset 6 vs 8) does not shrink the metric's wall share. This motivated restoring
concurrency rather than treating the tax as a slow-preset afterthought.

