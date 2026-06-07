# reel

AV1 encoding tool using the SVT-AV1 encoder library and FFmpeg/libav for parallel chunked encoding. Uses opinionated defaults so you can encode without dealing with encoder complexity.

## Expectations

This repository is shared as is. Reel is a personal encoding tool I built for my own workflow, hardware, and preferences. I've open sourced it because I believe in sharing but I'm not an active maintainer.

- Experimental: This is an incomplete early stage project that is purely experimental at this point. I would recommend looking at [av1an](https://github.com/rust-av/Av1an) or [xav](https://github.com/emrakyz/xav) for parallel chunked encoding.
- Personal-first: Things will change and break as I iterate.
- Best-effort only: This is a part-time hobby project and I work on it when I'm able to. I may be slow to respond to questions or may not respond at all.
- “Vibe coded”: I’m not a Go developer and this project started as (and remains) a vibe-coding experiment. Expect rough edges.

## Features

- Parallel chunked encoding with shot-aware chunk planning
- Default CVVDP target-quality mode with sampled probes and adaptive CRF search
- Automatic black bar crop detection
- HDR10/HLG metadata preservation
- Multi-track audio transcoding to Opus
- Post-encode validation (codec, dimensions, duration, HDR)
- Library API for embedding

## Requirements

- Go 1.26+
- libSvtAv1Enc (SVT-AV1 encoder shared library)
- libopusenc shared library (for Opus audio encoding)
- FFmpeg/libav development libraries: libavformat, libavcodec, libavutil, libswscale, libswresample
- libvship + CUDA for the default CVVDP target-quality build, or build with `-tags no_vship` for fixed-CRF-only use

```bash
# Ubuntu/Debian
sudo apt-get install libavformat-dev libavcodec-dev libavutil-dev libswscale-dev libswresample-dev libopusenc0 libsvtav1enc-dev

# Verify libopusenc is available
ldconfig -p | grep opusenc

# Verify libSvtAv1Enc is available
ldconfig -p | grep SvtAv1Enc
```

## Install

```bash
go install -trimpath codeberg.org/five82/reel/cmd/reel@latest                # default target-quality CVVDP/VSHIP build
go install -trimpath -tags no_vship codeberg.org/five82/reel/cmd/reel@latest # fixed-CRF-only build without VSHIP
```

Or build from source:

```bash
git clone https://codeberg.org/five82/reel
cd reel
go build -trimpath -o reel ./cmd/reel                 # default target-quality CVVDP/VSHIP build
go build -trimpath -tags no_vship -o reel ./cmd/reel  # fixed-CRF-only build without VSHIP
```

## Usage

```bash
reel encode -i input.mkv -o output/
reel encode -i /videos/ -o /encoded/
```

Reel splits each video into chunks, encodes chunks in parallel with SVT-AV1, merges the encoded video, then muxes Opus audio, chapters, and metadata. Fixed-CRF mode keeps simple duration-based chunking. Target-quality mode uses shot detection plus target-aware packing with a shorter 12s maximum chunk cap, so one CRF decision usually covers a visually coherent region without creating unnecessary tiny chunks. Adaptive workers start conservatively, test higher concurrency by recent throughput, and back off on RAM or swap pressure. If a run is interrupted, run the same command again to resume from completed chunks.

Target-quality mode uses CVVDP through [VSHIP](https://codeberg.org/Line-fr/Vship)/CUDA and is enabled in the default build, which requires `libvship`. The default search scores sampled CVVDP windows, starts from adaptive CRF priors, requires every sampled window to meet the lower quality bound, and accepts tiny over-target scores to avoid wasting time chasing metric perfection. Build with `-tags no_vship` to disable target-quality mode entirely and default to fixed-CRF mode.

### Options

```
Required:
  -i, --input          Input video file or directory (required)
  -o, --output         Output directory (required)

Quality Settings:
  --quality-mode MODE  target by default; crf by default in no_vship builds
  --target-quality R   CVVDP JOD target range (default 9.25-9.50)
  --crf-range R        Target-quality CRF search range (default 4.25-63.75)
  --cvvdp-display PATH Optional VSHIP/CVVDP display JSON override
  --metric-workers N   Concurrent VSHIP/CUDA metric workers (default 4)
  --max-probes N       Maximum target-quality probes per chunk (default 6)
  --crf <VALUE>        Fixed CRF mode, 1-70 in 0.25 steps
                         Defaults in CRF mode: SD=24, HD=26, UHD=26
                         Single value: --crf 25.25
                         Triple: --crf 24,26.25,26.5 (SD,HD,UHD)
  --preset <0-13>      SVT-AV1 preset (default 6, lower = slower/better)

Processing Options:
  --disable-autocrop   Disable black bar detection

Output Options:
  -l, --log-dir        Log directory (defaults to ~/.local/state/reel/logs)
  -v, --verbose        Verbose output
  --no-log             Disable log file creation
  --keep-workdir       Keep the .reel work directory after successful encodes
```

## Library Usage

Reel can be used as a Go library:

```go
import "codeberg.org/five82/reel"

encoder, err := reel.New(
    reel.WithCRF(26.25), // fixed-CRF mode; default is target-quality mode
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
    ├── media/          # Native libav media analysis and HDR detection
    ├── processing/     # Orchestration, crop detection, audio
    ├── validation/     # Post-encode validation
    ├── reporter/       # Progress reporting (terminal, composite)
    ├── logging/        # File logging
    └── util/           # Formatting, file utils, system info
```

## Development

```bash
go build -trimpath ./...
go test ./...
golangci-lint run
./check-ci.sh          # Full CI check
```

## Credit

Thanks to [xav](https://github.com/emrakyz/xav) for the libav-based parallel chunked encoding approach.
