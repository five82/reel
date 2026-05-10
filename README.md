# reel

AV1 encoding tool using SvtAv1EncApp and FFmpeg/libav for parallel chunked encoding. Uses opinionated defaults so you can encode without dealing with encoder complexity.

## Expectations

This repository is shared as is. Reel is a personal encoding tool I built for my own workflow, hardware, and preferences. I've open sourced it because I believe in sharing but I'm not an active maintainer.

- Experimental: This is an incomplete early stage project that is purely experimental at this point. I would recommend looking at av1an or xav for parallel chunked encoding.
- Personal-first: Things will change and break as I iterate.
- Best-effort only: This is a part-time hobby project and I work on it when I'm able to. I may be slow to respond to questions or may not respond at all.
- PRs: Pull requests are welcome if they align with the project's goals but I may be slow to review them or may not accept changes that don't fit my own use case.
- “Vibe coded”: I’m not a Go developer and this project started as (and remains) a vibe-coding experiment. Expect rough edges.

## Features

- Parallel chunked encoding with fixed-length chunks
- Automatic black bar crop detection
- HDR10/HLG metadata preservation
- Multi-track audio transcoding to Opus
- Post-encode validation (codec, dimensions, duration, HDR)
- Library API for embedding

## Requirements

- Go 1.26+
- SvtAv1EncApp (SVT-AV1 standalone encoder)
- FFmpeg executable (for chunk merging and final muxing)
- libopusenc shared library (for Opus audio encoding)
- FFmpeg development libraries: libavformat, libavcodec, libavutil, libswscale, libswresample
- MediaInfo

```bash
# Ubuntu/Debian
sudo apt-get install ffmpeg mediainfo libavformat-dev libavcodec-dev libavutil-dev libswscale-dev libswresample-dev libopusenc0 svt-av1

# Verify libopusenc is available
ldconfig -p | grep opusenc
```

## Install

```bash
go install github.com/five82/reel/cmd/reel@latest
```

Or build from source:

```bash
git clone https://github.com/five82/reel
cd reel
go build -o reel ./cmd/reel
```

## Usage

```bash
reel encode -i input.mkv -o output/
reel encode -i /videos/ -o /encoded/
```

### Options

```
Required:
  -i, --input          Input video file or directory (required)
  -o, --output         Output directory (required)

Quality Settings:
  --crf <VALUE>        CRF quality level (0-63, lower = better quality)
                         Single value: --crf 26 (use for all resolutions)
                         Triple: --crf 24,26,26 (SD,HD,UHD)
  --preset <0-13>      SVT-AV1 preset (default 6, lower = slower/better)

Processing Options:
  --disable-autocrop   Disable black bar detection
  --workers <N>        Parallel encoder workers (default: auto)
  --buffer <N>         Chunks to buffer in memory (default: auto)
  --threads <N>        Threads per worker (SVT-AV1 --lp flag, default: auto)

Output Options:
  -l, --log-dir        Log directory (defaults to ~/.local/state/reel/logs)
  -v, --verbose        Verbose output
  --no-log             Disable log file creation
```

## Library Usage

Reel can be used as a Go library:

```go
import "github.com/five82/reel"

encoder, err := reel.New(
    reel.WithCRF(26),
    reel.WithWorkers(4),
)
if err != nil {
    log.Fatal(err)
}

result, err := encoder.Encode(ctx, "input.mkv", "output/", func(event reel.Event) error {
    switch e := event.(type) {
    case reel.EncodingProgressEvent:
        fmt.Printf("Progress: %.1f%%\n", e.Percent)
    case reel.EncodingCompleteEvent:
        fmt.Printf("Done: %.1f%% reduction\n", e.SizeReductionPercent)
    }
    return nil
})
```

## Project Structure

```
reel/
├── reel.go             # Public API
├── events.go           # Event types for progress callbacks
├── cmd/reel/           # CLI
└── internal/
    ├── config/         # Configuration and defaults
    ├── discovery/      # Video file discovery
    ├── encoder/        # SVT-AV1 command building
    ├── encode/         # Parallel chunk encoding pipeline
    ├── chunk/          # Chunk management
    ├── keyframe/       # Keyframe extraction
    ├── worker/         # Worker pool for parallel encoding
    ├── video/          # FFmpeg/libav video probing and frame extraction
    ├── ffmpeg/         # FFmpeg parameter building
    ├── ffprobe/        # Media analysis
    ├── mediainfo/      # HDR detection
    ├── processing/     # Orchestration, crop detection, audio
    ├── validation/     # Post-encode validation
    ├── reporter/       # Progress reporting (terminal, composite)
    ├── logging/        # File logging
    └── util/           # Formatting, file utils, system info
```

## Development

```bash
go build ./...
go test ./...
golangci-lint run
./check-ci.sh          # Full CI check
```

## Credit

Thanks to xav for the libav-based parallel chunked encoding approach.
