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

- On the primary development setup, prefer the newer SVT-AV1 libraries in `~/.local/` for encoding; Debian's SVT-AV1 is much older and slower. Debian's SVT-AV1 should still work when `~/.local/` SVT libraries are not present.
- Do not let Reel's FFmpeg/libav decode path link against `~/.local/` FFmpeg libraries. Crop detection uses libav sampling, and the `~/.local/` FFmpeg build has caused severe crop-detection performance regressions.
- When changing cgo/pkg-config/linker settings, verify that SVT resolves from `~/.local/` while `libavformat`, `libavcodec`, `libavutil`, `libswscale`, and `libswresample` resolve from the system libraries.

## CLI Output Style

1. Four sections in human mode: Hardware -> Video -> Encoding -> Validation -> Results.
2. Show progress information once: progress bar during encode, summary after validation.
3. Use natural language sentences; reserve emphatic formatting for key values.
