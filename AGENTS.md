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

Spindle pins reel in its `go.mod` by commit. After pushing reel changes, bump the dependency in spindle (`go get codeberg.org/five82/reel@main && go mod tidy`), run spindle's `./check-ci.sh`, and commit/push spindle.

## Critical Expectations

- Apply YAGNI ("You Aren't Gonna Need It") and KISS ("Keep It Simple, Stupid"). Build only what the current task requires -- do not add abstractions, generality, or "future-proofing" for needs that do not yet exist. When two approaches work, take the simpler one. (Configuration/knobs are covered by the next bullet.)
- Prefer self-documenting code and local comments over separate documentation. Comments should explain the non-obvious why: constraints, tradeoffs, invariants, historical context, or surprising decisions that cannot be understood from reading the code alone. Avoid comments that merely restate what the code does. Use separate docs only for cross-cutting design notes, user-facing behavior, or information that would make the code noisy.
- Prefer opinionated defaults over exposing more user-facing knobs. Add configuration only when there is a clear recurring need that cannot be handled well by Reel's default behavior.
- Keep the library-first design suitable for Spindle embedding.
- Coordinate major trade-offs with the user; never unilaterally defer functionality.
- Keep edits ASCII unless the file already uses extended characters.
- When troubleshooting, gather evidence and test. Do not blindly guess.
- Prefer unit tests over real encodes; encoding is slow.
- When running Reel with a timeout, use at least 120 seconds.
- When performance tuning, record what was tried, why, measured results, and whether the change was kept or reverted. Add a compact dated entry to `docs/PERFORMANCE_TESTING_LOG.md` (the decision ledger), and update `docs/PERFORMANCE_TESTING.md` (the current-guidance summary: defaults table, strategy, open items) if a default or the active strategy changes. Read `docs/PERFORMANCE_TESTING.md` first for current state. This gives future agents context beyond git history.
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
