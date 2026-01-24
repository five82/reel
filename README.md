# reel

FFmpeg wrapper for AV1 encoding with SVT-AV1 and Opus audio. Uses opinionated defaults so you can encode without dealing with ffmpeg's complexity.

## Features

- Parallel chunked encoding with scene-based splitting
- Automatic black bar crop detection
- HDR10/HLG metadata preservation
- Multi-track audio transcoding to Opus
- Post-encode validation (codec, dimensions, duration, HDR)
- Library API for embedding

## Requirements

- Go 1.25+
- FFmpeg with `libsvtav1` and `libopus`
- SvtAv1EncApp (SVT-AV1 standalone encoder)
- FFMS2 (for frame-accurate video indexing)
- MediaInfo

```bash
# Ubuntu/Debian
sudo apt-get install ffmpeg mediainfo libffms2-dev svt-av1

# Verify FFmpeg has required encoders
ffmpeg -encoders | grep -E "svtav1|opus"
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
                         Single value: --crf 27 (use for all resolutions)
                         Triple: --crf 25,27,29 (SD,HD,UHD)
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
    reel.WithCRF(27),
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
    ├── keyframe/       # Scene detection and keyframe extraction
    ├── worker/         # Worker pool for parallel encoding
    ├── ffms/           # FFMS2 bindings for frame indexing
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
