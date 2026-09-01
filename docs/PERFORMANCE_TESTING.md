# Performance Testing Decisions

Read this file before proposing or running performance work. It is a decision
register, not a description of the current implementation: inspect the code for
current behavior and defaults, and use this file to learn why the relevant
choices were made, what alternatives already failed, and what evidence would
justify another test.

Real encodes are expensive and later coding sessions do not retain experiment
history. Keep only decisive evidence here. Raw logs, per-run JSON, GPU traces,
and one-off analysis belong under `$REEL_TESTING_DIR` (default `~/testing`) or
in git history.

## Scope and invalidation

Reel has only been tested on a Ryzen 9 7950X with an RTX 5060 Ti 16 GiB.
Worker counts, concurrency ceilings, and CPU/GPU bottlenecks are hardware- and
build-sensitive; reconsider them after a material CPU, memory, GPU, driver,
SVT-AV1, or libvship change. Metric mappings and quality findings
also depend on Reel's CVVDP display model. A display-model change invalidates
the JOD baselines, probe-noise estimate, and SSIMULACRA2 calibration.

The code is the authority for current values. A decision entry may name a value
to identify the experiment, but should not be maintained as a defaults table.
When a test changes a local constant or invariant, keep the concise rationale
beside the code and put the cross-cutting evidence here.

## Before benchmarking

Operational instructions, matrices, and artifact formats live in
`scripts/perf/README.md`. The following rules exist because prior tests failed
without them:

- Benchmark Reel processes sequentially on the single GPU. This isolates the
  A/B from resource contention; it is not a claim that all cross-process VSHIP
  use is inherently incorrect.
- Use a fresh workdir for every variant. Reel resumes completed chunks, so a
  shared output directory can silently reuse the previous variant.
- Record the actual binary hash, source state, SVT/libvship versions, and
  hardware. Comparing labels or git commits alone is insufficient for a dirty
  build.
- Output SHA-256 is not stable between target-quality runs. Chunk completion
  order changes which CRF prior is available, so legitimate probe paths and
  final bits can differ. For score-path correctness compare shared
  `(chunk, CRF)` probe points, then inspect probes/chunk, stop reasons, size,
  and delivered quality.
- Keep the workdir and run `scripts/fullvalidate` when a change can affect
  quality. The source must be byte-identical to the one used for encoding;
  re-cutting or replacing it invalidated a previous fullvalidate corpus and
  manufactured a roughly 0.5 JOD error.
- SDR <=1080p five-minute clips are dominated by the bounded CVVDP calibration
  warmup. Use 20-minute or feature-length inputs to measure steady-state
  SSIMULACRA2 behavior.
- Worker history before 2026-07-01 was sampled at chunk completion and
  under-reports active/in-flight peaks. Later artifacts use an unbiased timer.

There is currently no all-path reference baseline. The last full default matrix,
`$REEL_TESTING_DIR/perf-runs/20260702-124007-tq-baseline-current`, predates both
the SDR SSIMULACRA2 path and the AV1 level/bitrate contract. It is historical
only. Refresh the default matrix plus the long matrix before an A/B that needs a
current baseline; do not call the 2026-07-02 run current.

## Open work

### High - implement the selected grain strategy: gated 4K fftdnoiz + FGS

Direction selected 2026-08-31 after phases 1-5 of the denoise study (decision
below) plus initial living-room viewing: grainy titles get **fftdnoiz +
grain-med FGS table at native resolution**, selected by a bits-at-CRF grain
gate (probe bits at CRF ~26 separate Fargo, ~30 Mbps, from clean titles,
1-5 Mbps, by an order of magnitude); clean titles are untouched. The user
chose native-resolution future-proofing over maximum efficiency (100+ discs,
10/30 TB used - the same rationale as the raised CVVDP target), so the
measured-dominant 1440p+fftdnoiz path (8-21% of baseline bytes, half the
wall) is a documented per-title RESERVE, not a default; resize-without-denoise
is counterproductive (bigger than 4K at target quality) and must never ship.
Viewing so far: grain-med FGS beats bare denoise and the strong table (too
strong); per-tier tables (light/medium) ride the same gate.

Status 2026-08-31: IMPLEMENTED. The gate, fftdnoiz treatment, embedded
light/med FGS tables, reference caching (wall 1.42x recovered of the ~2x
tax), honest ceiling recording, and resume pinning shipped in
`internal/encode/grain.go` + `refcache.go`; Spindle mirrors the verdict into
EncodeStats/metrics/itemaudit/auditgather. Gate constants were calibrated
against 15 UHD rips with known TQ outcomes (denoise study RESULTS.md section
8): treat >= 0.0703 bpp at CRF 22 (12 samples - 5 misclassified two titles
from sampling noise alone), med tier >= 0.105, HD provisional 0.22/0.33.
Known signal limitation: the gate measures grain cost at CRF 22, not at the
JOD target, so dark grainy titles (American Hustle 0.048 bpp yet 18 Mbps
delivered; Alien-class 4.3x TQ/CRF22 ratio) stay untreated. The user chose
native 4K over the measured-dominant 1440p path (future-proofing at 10/30 TB
used); full viewing validation happens on the first real library encodes,
and thresholds/tiers adjust from those observations.

Remaining: watch the first treated library titles (Fargo, Vacation) on the
real fleet; adjust tier tables or cutoffs from what is seen; revisit HD
constants once 1080p titles have real accept/complain verdicts.

The experimental `--denoise` / `--probe-metric` / `--fgs-table` prototype
lives as uncommitted changes on branch `denoise`.

### Medium - validate SDR steady state on a full-length title

The measured `im-20m` wall reduction was about 47%; the roughly 50% feature
projection remains an extrapolation. One full 1080p SDR disc title would close
the throughput and quality projection.

### Medium - retest cross-title pairing in Spindle

The allocator mitigation held across processes (72/72 shared `(chunk, CRF)`
scores matched) and pairing measured 510s versus 670s sequential, but the first
real disc pair OOMed because both processes sized full CVVDP pools independently.
The SDR pool now falls from about 3.3 GiB during warmup to 182 MiB after
calibration.

The remaining gate is solo peak VRAM at real disc resolutions and the worst
overlap (1080p warmup plus 4K). Delaying the 4K partner until calibration locks
is the simple safe option. Evidence is under
`perf-runs/20260703-210442-coex-solo` and
`$REEL_TESTING_DIR/coex-20260704/`; policy belongs in Spindle, not Reel.

### Medium - overlap shot detection with encoding

Logical/2 workers reduced feature detection to about 415s, still roughly 6.7%
of 4K feature wall. Streaming boundaries could hide most of it, but dispatcher
ordering, resume, and prior seeding currently require a complete plan. Require
identical boundary hashes and an explicit design review before changing those
invariants.

### Low - broaden or replace the SDR calibration policy

Current evidence covers a small live-action corpus and only one truly held-out
title; the 2026-08-17 recalibration confirmed the raised 9.55 JOD operating
point on that same corpus but did not broaden it. Add animation/CG, 720p,
restored/denoised material, or a blinded grain A/B only if deciding whether
native SSIMULACRA2 preference should replace CVVDP-denominated policy. If
title-size centering becomes a real problem, first test whether the
largest-first warmup sample is biased before adding estimator complexity.

## Quality policy and probe metrics

### Target band and display model - KEEP

**Why:** The earlier half-width of 0.135 JOD fought roughly 0.075 JOD probe
noise. Widening to half-width 0.20 reduced a full feature from 3.11 to 1.46
probes/chunk and wall by 51%. Later feature validation measured 1.39
probes/chunk with 70% one-probe convergence, leaving little room above the hard
floor of one probe. Further widening lowers the quality floor and increases the
maximum same-shot join step, while offering only a thin probe reduction.

The target center and display geometry are quality/size policy, not throughput
knobs. On 2026-08-17 the user deliberately raised the center from 9.35 to 9.55
JOD while retaining the measured 0.20 half-width, making the default band
9.35-9.75. The 55-inch 4K display at 1.3 m is deliberately near-critical
viewing (about 77 px/degree), providing margin relative to a typical living
room. Changing it invalidates all calibrated JOD and SSIMULACRA2 constants. A
1000 versus 1500 nit HDR test found no quality deficit that the higher peak
fixed; the original alarming result was the old VSHIP scoring cascade and
disappeared on a fixed-ruler rescore.

The CRF search bounds were not an active limit either: the feature's selected
CRFs spanned 15.25-63.75, and its one ceiling chunk still converged in band.
The unused low bound is harmless.

**Retest only if:** a current run shows a material multi-probe tail, repeated
bounds/max-probe stops, or the user chooses a different quality/display policy.
Subjective invisibility was not established by these metric results.
`rate_capped` stops are excluded from that trigger: see the next decision.

### Rate-cap frontier stop - KEEP

**Why:** The level 5.1 bitstream cap (`encoder/svt.go`) rejects probes whose
worst second exceeds it, and on heavy-grain 4K chunks the cap binds below the
band: SVT's regulator holds the rate whatever the CRF, so scores are flat
across the over-rate/rate-legal frontier. Item 15 (Groundhog Day UHD, 609
chunks) missed the band on 36 chunks; all 36 sat at 12-35 Mbps with larger
lower-CRF probes rejected, and 32 burned all six probes because the search
treated the rejections as an ordinary bounds move: an over-rate first probe
produced a 0.25 step (the distorted score drove `secondSearchCRF`), then a
midpoint probe toward CRFMax near CRF 43 that scored 8.0-8.7. Scores on the
worst chunk were 8.72-8.78 from CRF 21 to 32. Those chunks were reported as
`max_probes`, which the item audit read as a search defect.

The search now steps `capFrontierCRF` above a rejected probe when no legal
probe exists, stops with `rate_capped` once the lowest legal below-band probe
is within `capFrontierCRF` of the highest over-rate probe, and relabels any
budget/guard stop after a cap rejection as `rate_capped`. The final pick is
unchanged (best rate-legal probe); only probe count and the stop label move.
Probe lines carry `peak_mbps` and `over_rate=true` so the rejection is visible.

**Update 2026-08-31:** the contract moved from level 5.1 main tier / 40 Mbps
to level 5.2 main tier / 60 Mbps by user decision: playback targets only the
user's own player apps, and the 40 Mbps cap was clipping heavy-grain 4K chunks
to 8.9-9.1 JOD (Fargo: 191/605 chunks). The frontier-stop mechanism is
unchanged and now engages at the 5.2 cap. Device headroom above 60 Mbps
(iPad Pro M4, Pixel 10/10 Pro) has not been measured; test before raising
further.

**Retest only if:** the cap policy changes, or a `rate_capped` chunk is shown
to have had a rate-legal CRF that would have reached the band.

**Provenance:** spindle item 15 log `spindle-20260828T040425.858Z.log`
(1230 `TQ probe` lines), Ryzen 9 7950X / RTX 5060 Ti.

**Provenance:** half-width and feature behavior: commit `714ac5f` and
`perf-runs/20260702-013705-feature-validation`; raised-center policy and SDR
mapping: source state `f1a802b` plus the working recalibration change, Ryzen 9
7950X / RTX 5060 Ti, artifact
`$REEL_TESTING_DIR/tq-recalibration-20260817/`. Older HDR display artifacts were
pruned after their decisive result was recorded in git history.

### Whole-chunk scoring - KEEP; sampled windows REJECTED

**Why:** On the 257-288 frame band, the old worst-window sampling was
systematically pessimistic and over-encoded. Across four 1080p clips,
whole-chunk scoring reduced pooled size 8.3%, improved mean absolute error
0.093 -> 0.082, and eliminated six above-band chunks. The effect was content
dependent: clean content was about 2% larger, while a grainy title was 22%
smaller. The measured 1080p wall cost was 16.6% in the CVVDP-everywhere era;
the SDR SSIMULACRA2 switch later removed that metric bottleneck. Whole-chunk
scores are exact for the encoded IVF and let a converged probe become the final
chunk without re-encoding.

**Retest only if:** a new method preserves whole-output quality and bit cost
without reintroducing a proxy score. Do not resurrect the deleted sampled
pooling, distance-space, Minkowski, or sampled early-out variants; those were
coupled to an obsolete architecture and were individually rejected before the
roughly 620-line sampling path was removed.

**Artifacts:** `$REEL_TESTING_DIR/band-investigation-20260630/`.

### SDR <=1080p SSIMULACRA2 probing - KEEP; VMAF REJECTED

**Why:** CVVDP resizes 1080p input to the 4K display raster and was the dominant
SDR cost. SSIMULACRA2 measured about 8.5x faster per isolated handler and cut an
`im-20m` run from 769s to 406-408s. Full-output CVVDP validation remained near
the then-current policy (mean about 9.39, rare per-chunk outliers down to about
9.07), with title size changes from roughly -2% to +12%.

The 2026-08-17 raised-center recalibration extended the four-title ladder from
CRF 20 down to 14 so all 24 sampled chunks crossed 9.55 JOD. Both metrics were
monotone over all 144 adjacent steps. At 9.55 JOD, the pooled SSIMULACRA2
median was 67.392 and the median local exchange rate was 37.47 points/JOD; the
held-out `bb` ladder independently measured 37.87 points/JOD. Per-title target
simulation delivered mean 9.555 JOD, SD 0.041, and range 9.473-9.650. Reel
therefore uses a rounded 67.4 target, 37.5 points/JOD scale, and +/-7.5
SSIMULACRA2 tolerance for the unchanged +/-0.20 JOD half-width.

End-to-end checks used fresh workdirs and the recalibrated binary. `im-20m`
completed in 316s at 1.24 probes/chunk; independent all-CVVDP validation of
its 135 final chunks measured mean 9.562 JOD (frame-weighted 9.566), SD 0.103,
range 9.255-9.752, with 3 below-band and 1 above-band chunks. The 115 chunks
searched by SSIMULACRA2 alone centered at 9.550 JOD; all 20 CVVDP warmup chunks
were in band. A direct-CVVDP `sully-5m` check measured mean 9.593, range
9.449-9.729, 1.15 probes/chunk, and no misses across 33 chunks. These confirm
centering, not subjective invisibility; the rare SDR mapping outliers remain
the accepted proxy tradeoff.

VMAF was rejected: all tested models saturated near the operating point,
produced 0.19-0.24 JOD delivered spread and real misses down to 8.78-8.91, and
would add a CPU-contending dependency. SSIMULACRA2 was monotone over all 120
ladder steps at Reel's preset and had a stable median exchange rate near 36
points/JOD.

**Retest only if:** the metric library/model changes materially, SDR quality
policy stops being CVVDP-denominated, or a new metric demonstrates both
non-saturating quality consistency and an end-to-end wall win.

**Artifacts:** `$REEL_TESTING_DIR/metric-research-20260710/`,
`$REEL_TESTING_DIR/tq-recalibration-20260817/`, and
`perf-runs/20260710-*-tq-ssimu2-*`.

### Per-title SSIMULACRA2 calibration - KEEP

**Why:** A fixed global SSIMULACRA2 target over-encoded grain badly: the pilot
made `bts` 32% larger, while title offsets at equal CVVDP quality ranged by
several SSIMULACRA2 points. Ten samples were noisy enough to make `air` 17%
larger; the bounded 20-sample median avoided that failure. Independent
reassessment reduced five-title mapping bias from 0.075 to 0.016 JOD. On the
one held-out title, a known-offset A/B attributed 13% size to the missing
correction, while pure global versus the end-to-end calibrated path was 27%
larger.

This validates calibration as a fast surrogate for Reel's existing CVVDP
policy. It does not establish CVVDP as subjective ground truth. Deleting only
the warmup while retaining the corpus-derived target would still be a quality
policy change, not cleanup. Slope fitting and periodic recalibration had no
supporting need.

Closing the CVVDP scorer pool after calibration was kept: an `im-20m` trace fell
from a 3.3 GiB warmup peak to 182 MiB steady-state with unchanged output and
wall, enabling the pairing retest described above.

**Retest only if:** the display/metric policy changes, broader content exposes a
repeatable calibration miss, or subjective testing chooses native
SSIMULACRA2 behavior over CVVDP matching.

**Artifacts:** `$REEL_TESTING_DIR/calibration-evaluation-20260711/` and
`perf-runs/20260711-120257-*`, `20260711-121152-*`,
`20260711-124042-*`.

## Metric pipeline

### Metric workers - KEEP current four-worker policy

**Why:** In the full-scan CVVDP matrix, four below-UHD workers slightly beat six
(683s versus 691s pooled) while using roughly 1-2 GiB less VRAM; eight regressed
to 709s and used substantially more VRAM. UHD was already encoder/memory
bandwidth bound. After the SDR SSIMULACRA2 switch, four workers no longer put
metric decode/scoring on the 1080p critical path, so changing the worker count
still does not improve wall.

**Retest only if:** hardware or libvship changes, pairing changes the critical
path, or `perf.json` shows metric workers saturated while encode slots wait.

**Artifacts:** `perf-runs/20260630-215633-mw4-hd`,
`20260630-220757-mw8-hd`, and the 2026-07-11 hardware-decode A/Bs.

### CVVDP source decoder threads - KEEP two

**Why:** One source-decoder thread starved 4K CVVDP (HEVC10 around 23 fps versus
a roughly 50 fps GPU ceiling). Two threads reduced 4K metric time 12-18% and
wall up to 8.8%, while remaining neutral at 1080p. Encode lanes absorbed the
freed CPU, so escalating further was not justified; the faster AV1 probe decoder
was not the producer bottleneck.

**Retest only if:** CPU allocation or the number of concurrent metric workers
changes enough to alter producer starvation.

**Artifacts:** `perf-runs/20260701-222552-metric-decode-threads`.

### NVDEC metric decoding - REJECTED

**Why:** The implementation proved hardware decode bit-exact for the supported
H.264/HEVC/VC-1 corpus and improved SSIMULACRA2 probe throughput 11-15%, but
end-to-end 1080p wall was neutral/slightly worse because metric work was not the
critical path. CVVDP probes became 5-8% slower and 4K VRAM rose about 1.6 GiB due
to GPU/download contention. A second decode path is not worth carrying without
a wall benefit.

**Retest only if:** metric workers become the measured wall-time bottleneck.
Start from the preserved patch and keep MPEG-2 software-only.

**Artifacts:** `perf-runs/20260711-*hwdec-*` and
`perf-runs/20260711-hwdec-implementation.patch` (applies to `faefa07`).

### CVVDP setup hoist and deeper ring - REJECTED

**Why:** Moving demux/seek/ring setup before scorer checkout and increasing ring
depth from two to three improved pooled wall by at most about 0.5% after probe
noise was removed. GPU utilization did not improve; CRF-search serialization,
not setup, caused the remaining idle.

**Retest only if:** profiling shows setup on the critical path after a major
pipeline change.

**Artifacts:** `perf-runs/20260702-032557-cvvdp-setup-hoist`.

### Mid-pass CVVDP early abort - REJECTED

**Why:** Across 1305 probes, a running score at 50% differed from the final by as
much as 0.469 JOD, wider than the complete target band. A zero-false-abort guard
caught too few doomed probes to save more than about 1-2% of metric work.

**Retest only if:** there is a fundamentally better final-score predictor, not
just another threshold over the same running score.

**Artifacts:** milestone data in
`perf-runs/20260702-013705-feature-validation`.

## Search, scheduling, and chunking

### Search slope - KEEP the shared initial slope

**Why:** The older SDR slope under-stepped clean, high-CRF content. The current
initial slope cut `air` wall 18.5%. Grainy low-CRF content can spend roughly
4-8 extra probes before the first measured slope arrives, but that is a fixed
per-title startup cost; the learned median then takes over. Returning to the old
SDR value would impose a title-long regression to avoid a bounded early cost.

**Retest only if:** feature logs show the early grain window growing with title
length. The next idea would be earlier measured-slope seeding, not restoring the
old tier split.

**Artifacts:** `perf-runs/20260701-234200-sdr-slope-025` and
`$REEL_TESTING_DIR/seedsim-20260701/`.

### Bracketed linear interpolation - KEEP; PCHIP REJECTED

**Why:** A leave-one-out test evaluated monotone cubic/PCHIP interpolation on
the multi-probe tail. Although PCHIP fit the forward CRF-to-score curve slightly
better, Reel needs the steep inverse score-to-CRF prediction: linear error was
0.357 CRF versus 1.221 for PCHIP, and linear won 46/51 paired chunks. The extra
curve shape would add probes, not remove them.

**Retest only if:** search changes direction to model CRF-to-score directly or a
new interpolation method wins the inverse prediction on held-out points. More
complexity applied to the same inverse data is closed.

**Provenance:** 2026-06-28 decision in git history (`82a31ec`); the throwaway
analysis scripts were not retained.

### Dispatch order and CRF priors - KEEP

**Why:** Timeline blocks with largest-first ordering balance neighboring CRF
priors against tail latency. On the complete Sully feature, 510/670 chunks used
a neighbor seed and 467 converged in one probe. Even a perfect order could save
only about 2.5% of probes (roughly 1% wall). Reducing block size previously
raised probes by weakening early priors.

Nearby alternatives were also rejected:

- nearest-completed fallback was flat on `sullyhv` and worsened `ko` probes
  46 -> 50 because a distant single chunk was noisier than the title median;
- staggered prime seeds and a default-CRF sweep only traded wins between the
  corpus's low- and high-CRF modes;
- raising UHD prime concurrency to the slot target added 4-5% probes/chunk and
  1-3% size without a wall win.

**Retest only if:** a content class shows a collapsed neighbor hit rate in
`target-quality.json`, or a scheduler change materially changes completion
order.

**Artifacts:** `perf-runs/20260702-013705-feature-validation`,
`20260702-001943-nearest-seed`, `20260701-230633-prime-slot-target`, and
`$REEL_TESTING_DIR/seedsim-20260701/`.

### Cheap luma content prior - REJECTED

**Why:** Brightness, spatial texture, and temporal activity from six clips/279
chunks achieved only 0.25-0.44 in-sample per-clip R-squared. Leave-one-clip-out
MAE was about 11 CRF versus 4.8 for the neighbor prior, and the features
explained none of the neighbor prior's residual error.

**Retest only if:** a concretely different feature family beats the neighbor
prior in a leave-one-content-out test before any encode A/B. More combinations
of the same cheap luma statistics are closed.

**Provenance:** 2026-06-28 decision in git history (`09ed548`); old raw
`crfcorr` artifacts were pruned.

### Chunk duration - KEEP as a balanced weak lever

**Why:** A whole-scan sweep from 8s to 24s found wall effectively flat at both
1080p and 4K: metric work follows total frames times probes/chunk, not chunk
size. Size and accuracy moved slightly by content with no consistent optimum.
The existing cap is a practical balance for parallelism, resume granularity,
and per-probe latency, not a tuned throughput peak.

**Retest only if:** the scoring model stops processing every frame or resume/
parallelism requirements change.

**Artifacts:** `$REEL_TESTING_DIR/band-investigation-20260630/`.

### Boundary placement as a TQ quality lever - REJECTED

**Why:** On the Sully feature, 190/669 joins were synthetic mid-shot splits.
Their median/p95/max quality step was 0.073/0.248/0.303 JOD, below the target
band's constructive 0.40 bound and smaller than natural-cut steps. CRF could
move by 22.5 while quality remained stable because each chunk independently
searched into the band. Missed cuts may cost bits, but whole-chunk scoring still
protects quality.

**Retest only if:** a viewing complaint identifies quality pumping at a
synthetic join. The relevant fixes would be narrowing the band or tying split
siblings, not making shot detection more elaborate.

**Artifacts:** `boundary-kinds.tsv` in
`perf-runs/20260702-013705-feature-validation`.

### Feature-length probe tail - CLOSED

**Why:** A complete 95m50s 4K HDR feature completed at 1.39 probes/chunk, 100%
convergence, zero max-probe chunks, and all final scores in band. Wall was
6232s (1.08x video runtime), host CPU p90 was 77%, and the GPU remained 53-56 C
over four hours. Feature length did not expose probe-tail, thermal, host-memory,
or scheduler-scaling failures.

**Retest only if:** logs from current code show the tail or resource behavior has
returned. Do not run another 4K feature solely to reconfirm title length.

**Artifacts:** `perf-runs/20260702-013705-feature-validation`.

## Encoder and concurrency

### SVT-AV1 preset - KEEP the measured wall/size knee

**Why:** The historical preset 4-8 sweep found faster presets bought too little
wall for permanent size cost, while slower presets cost substantial wall for
small size savings. A later current-matrix preset-7 A/B was only 1.1% faster
pooled for 10.3% more data; `sullyhv` was both 2.6% slower and 57% larger.
Preset 8 had already crossed the size budget in the broader sweep.

**Retest only if:** a material SVT-AV1 version change alters preset behavior.
Do not add a resolution split: 1080p bound-ness varied by content/bitrate, not
resolution alone.

**Artifacts:** `perf-runs/20260630-231627-preset7`; older preset 4-8 artifacts
were pruned after being recorded in git history.

### 4K encode concurrency and SVT level_of_parallelism - KEEP auto policy

**Why:** 4K was limited by memory bandwidth, not free RAM. Raising active
encodes near nine increased per-probe encode time roughly 27s -> 37s and
worsened wall despite tens of GiB remaining. On the fixed allocator build,
encoder-only throughput improved through cap eight, but target-quality wall was
flat at cap six and slower at cap eight/ten. The existing bandwidth-aware
ceiling was the measured TQ optimum on this machine.

SVT `level_of_parallelism` is bitstream-neutral in Reel's tests. Deriving it
from the resolution-aware worker ceiling lets a few concurrent encoders use
otherwise idle cores. Explicit lp2/lp4 A/Bs were mixed by content and did not
beat auto consistently.

**Retest only if:** CPU/memory topology or SVT changes materially. The
concurrency divisor is a hardware throughput optimum, not a safety limit.

**Artifacts:** historical `perf-ab/cap-lp-retest` data (raw directory pruned),
plus `perf-runs/20260630-222228-lp2-uhd`,
`20260630-223634-lp4-uhd`, and `20260630-225431-lp4-sullyhv`.

### Dev-box power, PCIe, and storage explanations - CLOSED

**Why:** During the metric-bound phase the GPU trained at its full Gen5 x8 link
and drew only 111-126 W of a 180 W limit while SM utilization reached 96%; it
was not power-capped, so raising the power limit was not a throughput lever.
Production and benchmark workdirs were already on local NVMe.

**Retest only if:** the hardware, negotiated link, driver, power state, or
workdir placement changes. Do not use these as generic explanations for a
current-code regression without checking the recorded environment first.

**Provenance:** environment capture in
`perf-runs/20260701-222552-metric-decode-threads`.

### Shot-detection workers - KEEP logical/2; further oversubscription REJECTED

**Why:** Moving from physical/2 to logical/2 used otherwise idle SMT capacity
and reduced Sully feature detection about 575s -> 415s with identical boundary
hashes. A 16/20/24/32-worker sweep produced 414/393/388/394s. The best extra
oversubscription saved only 26s on a 96-minute feature (about 0.4% total wall)
and was hardware-sensitive. Five-minute clips cannot measure this because the
1500-frames-per-worker floor caps them near four workers.

**Retest only if:** decoder behavior or CPU topology changes. Always gate on an
identical `scripts/chunkbench` boundary hash.

**Artifacts:** `$REEL_TESTING_DIR/shotdet-workers-20260702/` and
`perf-runs/20260702-110142-shotdet-logical-workers`.

### Concurrent VSHIP handlers - KEEP only with the allocator mitigation

**Why:** Default libvship `cudaMallocAsync` allocation intermittently corrupted
scores across concurrent handlers and could cascade into multi-gigabyte output
swings. Building with `MITIGATE_MALLOC_ASYNC` made repeated concurrent results
match serial truth and restored roughly 1.5x throughput over the temporary
serialized workaround.

**Retest only if:** libvship, GPU, or driver changes. Run `scripts/handlertest`
before trusting concurrent scores; a couple of clean repetitions did not bound
the historical intermittent failure.

**Provenance:** `docs/VSHIP_CONCURRENCY_BUG.md`, commit `ec7faf7`, and the old
`vship-concurrency` artifacts (since pruned).

## Denoise and film grain

### Pre-encode denoising - NOT ADOPTED as a default; fftdnoiz on coarse grain OPEN

**Why:** An in-process `--denoise` prototype (libavfilter graph applied to both
encoder input and metric reference, filter state reset per chunk in both paths)
was measured end to end in target-quality mode. At the nominal 9.55 target,
hqdn3d=2:1.5:3:2.25 cut grainy 4K titles to 0.48-0.72x bytes, but the decisive
control - re-running the undenoised baseline at the denoised run's honest
source-referenced JOD - showed most of that is bought by quality, not
efficiency: hqdn3d lost to plain target-lowering on vac (+21%) and hustle
(+8%), winning only on fargo (-11%), and its honest quality drop roughly equals
the denoise-only ceiling loss. fftdnoiz (defaults) passed the same control on
both coarse-grain titles (fargo -34%, vac -13% at matched honest JOD) but costs
1.9-2.5x TQ wall (per-probe reference re-filtering, uncached) and has the worse
p5/min tail at matched mean in 5/5 / 4/5 pairs. Denoise never reduced
`rate_capped` chunks (that is the signaled-level peak-rate ceiling - 5.1 /
40 Mbps during this study, raised to 5.2 / 60 Mbps on 2026-08-31 - not
grain), and
clean content is only harmed (sully: 3% bytes for 0.13 JOD).

Denoiser field: hqdn3d is ~free (+0.019 CPU-s per 4K frame, strength-
independent) and fftdnoiz costs +0.316; atadenoise is dominated everywhere and
inflates clean files; nlmeans_vulkan produces corrupt output on this driver;
bilateral_cuda is 8-bit-only; dctdnoiz and CPU nlmeans silently round-trip HDR
through 8 bits; bm3d/vaguedenoiser are far too slow. Strength ceilings (CVVDP
of denoised vs original, no encode) put hqdn3d=2:1.5:3:2.25 at 9.78-9.95 and
anything stronger below the 9.75 band top on grainy content; 60s-sample
ceilings understate full-clip scene tails (vac 9.875 sampled vs 9.780 true).

SVT-internal film grain: `--film-grain N` halves encode speed via grain
estimation regardless of `--film-grain-denoise`, and its denoiser bought only
19% - REJECTED. A prebuilt `--fgs-table` is free (29.67 vs 29.61 fps), verified
present in the bitstream, and intensity-indexed with no spatial anchoring, so
cropping cannot invalidate it - the viable synthesis route if texture is ever
wanted.

1080p SDR: with the reference denoised, per-title SSIMU2 calibration re-locks
correctly (bts offset -8.93 -> -8.18) and delivers within 0.03 JOD of forced
CVVDP, which costs +50% wall and +10% bytes - forcing CVVDP for denoised SDR is
REJECTED.

**Retest only if:** the open grain-strategy item chooses denoise (then add
reference-filter caching first), a driver/filter change alters the
fftdnoiz/hqdn3d frontier or fixes nlmeans_vulkan, or subjective viewing of the
matched pairs contradicts the equal-honest-JOD control.

**Artifacts:** `$REEL_TESTING_DIR/denoise-20260830/` (PLAN.md, RESULTS.md
phases 1-3b, matched viewing pairs under `phase3/runs/`), prototype on branch
`denoise` (uncommitted); Ryzen 9 7950X / RTX 5060 Ti, SVT-AV1 4.2 `0696282`,
ffmpeg git-2026-08-28, libvship per `check-deps.sh`.

## Low-value pipeline work

Existing `perf.json` measurements put media-property probes, validation opens,
stream-byte scans, merge, and mux in the sub-1% tail on local storage. Duplicate
first-frame video and audio probes were removed; the remaining validation opens
were about milliseconds. Pooling `video.Source` objects around metric passes was
also rejected: gross open cost was mostly hidden by the encode/score overlap,
leaving roughly 0.3% of 4K feature wall on the critical path while adding
thread-safety complexity.

Do not optimize this tail from code inspection alone. Reopen an item only when a
representative `perf.json` on large or network media shows that phase has become
material. This rule does not cover shot detection, which remains a measured
serial phase and has its own open item above.
