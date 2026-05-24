# Usage Guide

Run `reel encode --help` for the authoritative flag list. The sections below provide practical context.

## CLI Basics

```bash
# Basic encode
reel encode -i input.mkv -o output/

# Batch encode an entire directory
reel encode -i /videos/ -o /encoded/

# Default target-quality encode (CVVDP via VSHIP/CUDA)
reel encode -i input.mkv -o output/

# Fixed CRF mode
reel encode -i input.mkv -o output/ --quality-mode crf --crf 24.25 --preset 6

# Resolution-specific fixed CRF (SD, HD, UHD)
reel encode -i input.mkv -o output/ --crf 24,26.25,26.5

# Verbose output
reel encode -v -i input.mkv -o output/
```

## Frequently Used Options

**Required**
- `-i, --input <PATH>`: Input file or directory containing video files
- `-o, --output <DIR>`: Output directory (or filename when single file)

**Quality Settings**
- `--quality-mode target|crf`: target-quality CVVDP mode is the default in normal builds; `crf` is the default in `no_vship` builds and keeps fixed-CRF behavior
- `--target-quality <LOW-HIGH>`: CVVDP JOD target range (default `9.45-9.55`)
- `--crf-range <LOW-HIGH>`: target-quality search bounds (default `4.25-63.75`)
- `--cvvdp-display <PATH>`: optional VSHIP/CVVDP display JSON; otherwise Reel generates a normal-viewing `reel` model. Custom JSON must contain a top-level `reel` model.
- `--metric-workers <N>`: concurrent VSHIP/CUDA scoring workers (default `1`)
- `--crf <VALUE>`: fixed CRF, `1-70` in `0.25` increments. Supplying `--crf` without `--quality-mode` selects `crf` mode for compatibility.
  - Single value: `--crf 26.25` (use for all resolutions)
  - Triple: `--crf 24,26.25,26.5` (SD,HD,UHD)
- `--preset <0-13>`: SVT-AV1 encoder speed/quality (default `6`, lower is slower but higher quality)

**Processing**
- `--disable-autocrop`: Skip black-bar detection and cropping

**Output**
- `-l, --log-dir <DIR>`: Override the log directory (defaults to `~/.local/state/reel/logs`)
- `-v, --verbose`: Verbose output with detailed status
- `--no-log`: Disable log file creation

## Parallel Chunked Encoding

Reel splits video into fixed-length chunks, encodes chunks in parallel with SVT-AV1, merges the encoded chunks, then muxes video with Opus audio, chapters, and metadata. Worker count adapts during encoding: Reel starts conservatively, tests higher concurrency by recent throughput, and backs off on RAM or swap pressure.

Target-quality mode is enabled in the default build and requires a working `libvship`/CUDA install. Build with `-tags no_vship` to disable target-quality mode entirely and default to fixed-CRF mode.

Interrupted runs can be resumed by running the same command again. Completed chunks are kept in Reel's temporary work directory until the final output is created.

## HDR Support

Reel automatically detects and preserves HDR content using native libav probing for color space analysis:
- Detects HDR based on color primaries (BT.2020, BT.2100)
- Recognizes HDR transfer characteristics (PQ, HLG)
- Adapts processing parameters and metadata handling for HDR sources

## Post-Encode Validation

Validation catches mismatches before you archive or publish results:
- **Video codec**: Ensures AV1 output and 10-bit depth
- **Audio codec**: Confirms all audio streams are transcoded to Opus with the expected track count
- **Dimensions**: Validates crop detection and output dimensions
- **Duration**: Compares input and output durations (±1 second tolerance)
- **HDR / Color space**: Uses native libav probing to verify HDR flags and colorimetry
- **Audio sync**: Verifies audio drift is within 100ms tolerance

## Multi-Stream Audio Handling

- Automatically detects every audio stream with libav and transcodes each to Opus with libopusenc
- Encodes audio streams in parallel while video chunks encode
- Preserves the input channel count; surround channels are reordered for Opus mapping, not downmixed
- Bitrate scales with channel count using `128 * (channelEquivalent / 2)^0.75`:
  - Mono: 76 kbps
  - Stereo: 128 kbps
  - 5.1: 258 kbps
  - 7.1: 331 kbps
  - Other layouts: formula-based by channel count

## Progress Reporting

Foreground runs show real-time progress with ETA, fps, and reduction stats. For automation, use the library API with a custom event handler; see `README.md`, `reel.go`, and `events.go`.

## Environment Variables

- `NO_COLOR`: Disable colored output

## Debugging

```bash
# Verbose logging
reel encode -v -i input.mkv -o output/

# Check log files
ls ~/.local/state/reel/logs/
```
