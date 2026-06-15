# AGENTS.md

This file provides guidance when working with code in this repository.

## TL;DR

- Do not run `git commit` or `git push` unless explicitly instructed.
- Do not create git branches unless explicitly instructed.
- Run `./check-ci.sh` before handing work back.

## Project

Reel is an **AV1 encoding tool** using the SVT-AV1 and FFmpeg/libav libraries (linked in-process via cgo) for parallel chunked encoding. It provides opinionated defaults, automatic crop detection, HDR preservation, and post-encode validation.

Single-developer hobby project - prefer simple, maintainable solutions over clever abstractions.

## Related Repos

| Repo | Path | Role |
|------|------|------|
| reel | `~/projects/reel/` | AV1 encoding tool (this repo) |
| spindle | `~/projects/spindle/` | Orchestrator that shells out to Reel during ENCODING |
| flyer | `~/projects/flyer/` | Read-only TUI for Spindle |

## Critical Expectations

- Prefer opinionated defaults over exposing more user-facing knobs. Add configuration only when there is a clear recurring need that cannot be handled well by Reel's default behavior.
- Keep the library-first design suitable for Spindle embedding.
- Coordinate major trade-offs with the user; never unilaterally defer functionality.
- Keep edits ASCII unless the file already uses extended characters.
- When troubleshooting, gather evidence and test. Do not blindly guess.
- Prefer unit tests over real encodes; encoding is slow.
- When running Reel with a timeout, use at least 120 seconds.
- When performance tuning, record what was tried, why, measured results, and whether the change was kept or reverted. Add a dated entry to `docs/PERFORMANCE_TESTING_LOG.md` (the historical record), and update `docs/PERFORMANCE_TESTING.md` (the current-guidance summary: defaults table, strategy, open items) if a default or the active strategy changes. Read `docs/PERFORMANCE_TESTING.md` first for current state. This gives future agents context beyond git history.
- When creating or updating open issues, assign a priority (critical, high, medium, low) and a brief reason

## Build, Test, Lint

```bash
go build -trimpath -o reel ./cmd/reel  # Build CLI without local paths
go test ./...                         # Test
go test -race ./...                   # Race detector
golangci-lint run                     # Lint
./check-ci.sh                         # Full CI (recommended before handoff)
```

## Native Library Linking

- Reel should use normal pkg-config/linker resolution for SVT-AV1. On the primary development setup, the newer SVT-AV1 is installed in `/usr/local`.
- Do not add custom rpaths, `LD_LIBRARY_PATH`, or temporary pkg-config hacks for SVT-AV1 unless there is a demonstrated need.
- When changing cgo/pkg-config settings, verify `libSvtAv1Enc` resolves from `/usr/local` and FFmpeg/libav libraries resolve from system paths under `/lib` or `/usr/lib`.

## CLI Output Style

1. Five sections in human mode: Hardware -> Video -> Encoding -> Validation -> Results.
2. Show progress information once: progress bar during encode, summary after validation.
3. Use natural language sentences; reserve emphatic formatting for key values.

## Target-Quality Encoding Philosophy

### What Reel target-quality is for

Reel encodes media **libraries** (potentially hundreds of titles) for **Jellyfin streaming**, watched at normal viewing distances on TVs, tablets, and phones. It is **not** an archival or reference-quality encoder. The job of target-quality mode is to produce encodes that are **more consistent and better quality than a fixed CRF** (which has no quality feedback and swings wildly across content), at a **throughput that scales to a whole library**. Speed is a first-class goal, not an afterthought: when a tradeoff buys meaningful encode-time at a quality cost that is invisible in normal streaming, take it.

This is a deliberately different point on the curve from tools like **xav** and **av1an**, where finding near-optimal per-scene/per-title quality is the priority and much more compute is acceptable. Reel chooses the faster "good enough for streaming at viewing distance" point. Treat accuracy comparisons against those tools as directional guidance, not a hard target; do not adopt their cost to chase quality the use case cannot see.

### Target CVVDP range

Quality is measured in CVVDP JOD (0-10, where 10 = indistinguishable from source). The scale is calibrated so ~1 JOD is roughly where 75% of viewers pick the reference in a **side-by-side** comparison. Streaming has no reference on screen, so this is far stricter than no-reference viewing: **~9.0+ is effectively transparent in normal viewing; the mid-9.x range is high quality with margin.** Reel's default display model (defined in `internal/quality/display.go`) is itself a conservative viewing calibration -- it resolves detail at the eye's acuity ceiling -- so a target landing in the low-to-mid 9.x range is already demanding for the actual use case.

The default target range is set via `DefaultTargetQuality` in `internal/config/config.go` and is expressed as a `LOW-HIGH` band whose center is the target and whose half-width is the tolerance.

**The band width is a speed knob, not just an accuracy setting.** Probe measurement noise on the sampled windows is ~0.075 JOD. A half-width tighter than about 2x that (~0.15) means probes keep landing just outside the band and the search marches to extra probes (the dominant feature-length cost; see `docs/PERFORMANCE_TESTING.md` "Bottlenecks and key tradeoffs", with full detail in `docs/PERFORMANCE_TESTING_LOG.md` "Target band WIDTH is the real probe-tail lever"). A band only as tight as the use case needs converges in 1-2 probes and also reduces overshoot (smaller files). Do not tighten the band to buy consistency finer than the metric can measure or the viewer can see; it is paid for in probes.

When changing the target range, keep it an explicit, evidence-backed decision: it is an accuracy/size/speed tradeoff that needs a confirming real encode + `scripts/fullvalidate` ground truth before the default moves, and it should be coordinated with the user.

### Two-Layer Architecture

The system splits the problem into two independent layers:

1.  **Chunking (where to cut):** Decides how to split the input into independently-encoded chunks. Uses cheap luma-based scene detection plus a maximum duration cap. Complex boundary logic (balanced splits, weak-cut merging, transition detectors) has been tested and found to add code complexity without materially improving chunk homogeneity. Keep chunking simple.

2.  **Target-Quality Search (what CRF to use):** For each chunk, probes at different CRFs until the measured quality is close to the target. Uses **sampled** CVVDP windows rather than full-chunk probes for speed. A worst-window floor guard rejects probes where any sample window falls below tolerance, even if the mean looks good.

### The Fundamental Tradeoff

Within-chunk variance caused by gradual fades, lighting shifts, or slow camera movement cannot be solved by boundary placement: there is no "cut" in the middle of a fade. Detectors (even expensive ones like av-scenechange with motion estimation) do not solve this.

This means the accuracy ceiling is determined by the **probe sampling strategy**, not the scene detector. Sparse sampled windows work well for homogeneous chunks but can miss worst-case segments in long, gradually-varying chunks.

### How to Improve Target-Quality Results

When considering changes, prefer interventions in this order:

1.  **Smarter sampling:** Add conditional extra windows or targeted full probes for chunks where window spread exceeds a threshold. This targets uncertainty directly without paying full-probe cost for every chunk.
2.  **Metric aggregation:** Tune Reel's internal CVVDP aggregation toward the best default behavior (for example, tie-break among converged probes by preferring higher worst-window scores). Avoid exposing mean/percentile/worst knobs unless evidence shows one default cannot work well across common content.
3.  **Probe density:** Scale window count with chunk length so longer chunks get proportionally more coverage.
4.  **Avoid:** Adding more scene-detection heuristics, transition detectors, or chunk-boundary refinement. These layers have shown poor ROI relative to their complexity.

### Reference Point: xav

xav uses a more expensive scene detector and probes the **entire** chunk. It handles within-chunk variance through metric aggregation (worst frames drive the score) rather than more boundaries. Reel chooses the opposite point on the curve: cheap detector + sampled probes. Both approaches hit the same structural limit; the difference is where you pay the cost.

When comparing against xav, measure:
- Total encode time (Reel should be significantly faster)
- Mean absolute JOD error (Reel should be close)
- Tail behavior (p90/max window spread — this is where sampling shows its cost)

### Measuring Accuracy

Sampled scores in `target-quality.json` are what the search *believed*, not ground truth. For any change that trades accuracy (window count/size, probe thresholds, chunk caps), run `scripts/fullvalidate` on a kept workdir: it scores the final output with full-chunk CVVDP and reports true per-chunk JOD plus how far the sampled scores deviated.
