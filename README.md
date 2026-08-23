# reel

AV1 encoding tool using the SVT-AV1 encoder library and FFmpeg/libav for parallel chunked encoding. Uses opinionated defaults so you can encode without dealing with encoder complexity.

## Expectations

This repository is shared as is. reel is a personal encoding tool I built for my own workflow, hardware, and preferences. I've open sourced it because I believe in sharing but I'm not an active maintainer.

- Experimental: This is an early stage project. I would recommend looking at [av1an](https://github.com/rust-av/Av1an) or [xav](https://github.com/emrakyz/xav) for parallel chunked encoding.
- Personal-first: Things will change and break as I iterate.
- Best-effort only: This is a part-time hobby project and I work on it when I'm able to. I may be slow to respond to questions or may not respond at all.

## Features

- Parallel chunked encoding with shot-aware chunk planning
- Default target-quality mode with whole-chunk probes and adaptive CRF search (CVVDP for HDR and above-1080p sources; SSIMULACRA2 with per-title CVVDP calibration for SDR at or below 1080p)
- Automatic black bar crop detection
- HDR10/HLG metadata preservation
- Multi-track audio transcoding to Opus
- Post-encode validation (codec, dimensions, duration, HDR)
- Library API for embedding

## Design Goals

reel encodes media libraries for home streaming watched at normal viewing distances. It is not an archival or reference quality encoding tool. The aim is "fast" target quality encodes that have more consistent quality across varied content compared to fixed CRF. Speed is a first class goal. When a tradeoff buys meaningful encode time at a quality cost that is invisible in normal viewing, reel takes it.

This is a deliberately different point on the speed/quality/size curve from typical target quality encoding tools which chase near optimal per-scene quality at the expense of more compute.

## Requirements

- Go 1.27.0+
- libSvtAv1Enc (SVT-AV1 encoder shared library)
- libopusenc shared library (for Opus audio encoding)
- FFmpeg/libav development libraries: libavformat, libavcodec, libavutil, libswscale, libswresample
- libvship + CUDA for the default VSHIP target-quality build, or build with `-tags no_vship` for fixed-CRF-only use. **Build libvship with `MITIGATE_MALLOC_ASYNC=on`** (e.g. `make build BACKEND=Cuda MITIGATE_MALLOC_ASYNC=on`): reel scores probes with one VSHIP handler per metric worker concurrently, and libvship's default `cudaMallocAsync` allocator races across coexisting handlers and silently corrupts scores without this flag.

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
go install -trimpath github.com/five82/reel/cmd/reel@latest                # default VSHIP/CUDA target-quality build
go install -trimpath -tags no_vship github.com/five82/reel/cmd/reel@latest # fixed-CRF-only build without VSHIP
```

Or build from source:

```bash
git clone https://github.com/five82/reel
cd reel
go build -trimpath -o reel ./cmd/reel                 # default VSHIP/CUDA target-quality build
go build -trimpath -tags no_vship -o reel ./cmd/reel  # fixed-CRF-only build without VSHIP
```

To deploy a source checkout over the `reel` on `PATH`:

```bash
./check-ci.sh
./deploy.sh
```

The deploy script builds with CGO and VSHIP support, keeps the previous binary
beside the installed one, and verifies the installed copy.

## Usage

```bash
reel encode -i input.mkv -o output/
reel encode -i /videos/ -o /encoded/
```

reel splits each video into chunks, encodes chunks in parallel with SVT-AV1, merges the encoded video, then muxes Opus audio, chapters, and metadata. Fixed-CRF mode keeps simple duration-based chunking. Target-quality mode uses shot detection plus target-aware packing with a shorter 12s maximum chunk cap, so one CRF decision usually covers a visually coherent region without creating unnecessary tiny chunks. Adaptive workers start conservatively, test higher concurrency by recent throughput, and back off on RAM or swap pressure. If a run is interrupted, run the same command again to resume from completed chunks.

Target-quality mode scores probes through [VSHIP](https://codeberg.org/Line-fr/Vship)/CUDA and is enabled in the default build, which requires `libvship`. HDR and above-1080p sources score with CVVDP; SDR sources at or below 1080p score with the much faster SSIMULACRA2 after a short per-title CVVDP warmup that calibrates the title's SSIMU2 target (SSIMU2 values do not transfer across content, so each title measures its own offset). The search scores each probe over the whole chunk, starts from adaptive CRF priors, and requires the score to land inside the target band. The converged probe is reused as the final chunk. Build with `-tags no_vship` to disable target-quality mode entirely and default to fixed-CRF mode.

Run `reel encode --help` for the full flag list, or see [docs/USAGE.md](docs/USAGE.md).

## Library Usage

reel can be used as a Go library:

```go
import "github.com/five82/reel"

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

## Development

```bash
go build -trimpath ./...
go test ./...
golangci-lint run
./check-ci.sh          # Full CI check
```

## Credit

Thanks to [xav](https://github.com/emrakyz/xav) for the libav-based parallel chunked encoding approach.
