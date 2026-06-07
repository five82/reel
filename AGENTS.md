# AGENTS.md

This file provides guidance when working with code in this repository.

## TL;DR

- Do not run `git commit` or `git push` unless explicitly instructed.
- Run `./check-ci.sh` before handing work back.

## Project

Reel is an **AV1 encoding tool** using FFmpeg/libav + SvtAv1EncApp for parallel chunked encoding. It provides opinionated defaults, automatic crop detection, HDR preservation, and post-encode validation.

Single-developer hobby project - avoid over-engineering.

## Related Repos

| Repo | Path | Role |
|------|------|------|
| reel | `~/projects/reel/` | AV1 encoding tool (this repo) |
| spindle | `~/projects/spindle/` | Orchestrator that shells out to Reel during ENCODING |
| flyer | `~/projects/flyer/` | Read-only TUI for Spindle |

## Critical Expectations

- Prefer simple, maintainable solutions over clever abstractions.
- Prefer opinionated defaults over exposing more user-facing knobs. Add configuration only when there is a clear recurring need that cannot be handled well by Reel's default behavior.
- Keep the library-first design suitable for Spindle embedding.
- Coordinate major trade-offs with the user; never unilaterally defer functionality.
- Keep edits ASCII unless the file already uses extended characters.
- When troubleshooting, gather evidence and test. Do not blindly guess.
- Prefer unit tests over real encodes; encoding is slow.
- When running Reel with a timeout, use at least 120 seconds.

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

1. Four sections in human mode: Hardware -> Video -> Encoding -> Validation -> Results.
2. Show progress information once: progress bar during encode, summary after validation.
3. Use natural language sentences; reserve emphatic formatting for key values.

## Target-Quality Encoding Philosophy

Reel's target-quality mode trades some accuracy for significantly faster encoding time compared to full-probe approaches (e.g., xav). It is not a pursuit of perfect per-frame quality matching; the goal is to land in a good spot on the speed/accuracy curve before hitting diminishing returns. Treat accuracy comparisons as directional guidance, not a hard target.

### Two-Layer Architecture

The system splits the problem into two independent layers:

1.  **Chunking (where to cut):** Decides how to split the input into independently-encoded chunks. Uses cheap luma-based scene detection plus a maximum duration cap. Complex boundary logic (balanced splits, weak-cut merging, transition detectors) has been tested and found to add code complexity without materially improving chunk homogeneity. Keep chunking simple.

2.  **Target-Quality Search (what CRF to use):** For each chunk, probes at different CRFs until the measured quality is close to the target. Uses **sampled** CVVDP windows (default: 3 windows of 48 frames each) rather than full-chunk probes for speed. A worst-window floor guard rejects probes where any sample window falls below tolerance, even if the mean looks good.

### The Fundamental Tradeoff

Within-chunk variance caused by gradual fades, lighting shifts, or slow camera movement cannot be solved by boundary placement: there is no "cut" in the middle of a fade. Detectors (even expensive ones like av-scenechange with motion estimation) do not solve this.

This means the accuracy ceiling is determined by the **probe sampling strategy**, not the scene detector. Three 48-frame windows work well for homogeneous chunks but can miss worst-case segments in long, gradually-varying chunks.

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
