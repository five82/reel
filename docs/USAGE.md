# Usage Guide

Run `reel encode --help` for the authoritative flag list. The sections below provide practical context.

## CLI Basics

```bash
# Basic encode
reel encode -i input.mkv -o output/

# Batch encode an entire directory
reel encode -i /videos/ -o /encoded/

# Override quality settings
reel encode -i input.mkv -o output/ --crf 24 --preset 6

# Resolution-specific CRF (SD, HD, UHD)
reel encode -i input.mkv -o output/ --crf 25,27,29

# Verbose output
reel encode -v -i input.mkv -o output/
```

## Frequently Used Options

**Required**
- `-i, --input <PATH>`: Input file or directory containing video files
- `-o, --output <DIR>`: Output directory (or filename when single file)

**Quality Settings**
- `--crf <VALUE>`: CRF quality level (0-63, lower is better quality)
  - Single value: `--crf 27` (use for all resolutions)
  - Triple: `--crf 25,27,29` (SD,HD,UHD)
- `--preset <0-13>`: SVT-AV1 encoder speed/quality (default `6`, lower is slower but higher quality)

**Processing**
- `--workers <N>`: Number of parallel encoder workers (auto-detected by default)
- `--buffer <N>`: Extra chunks to buffer in memory (auto-matched to workers)
- `--threads <N>`: Threads per worker (SVT-AV1 --lp flag, auto-detected by default)
- `--disable-autocrop`: Skip black-bar detection and cropping

**Output**
- `-l, --log-dir <DIR>`: Override the log directory (defaults to `~/.local/state/reel/logs`)
- `-v, --verbose`: Verbose output with detailed status
- `--no-log`: Disable log file creation

## Parallel Chunked Encoding

Reel splits videos into fixed-length chunks and encodes them in parallel:

```bash
# Auto-detected parallelism (1 worker per 8 CPU cores, max 4)
reel encode -i input.mkv -o output/

# Manual worker count
reel encode -i input.mkv -o output/ --workers 8 --buffer 8

# Adjust threads per worker
reel encode -i input.mkv -o output/ --workers 2 --threads 4
```

See [docs/chunked-encoding.md](chunked-encoding.md) for details on how chunked encoding works.

## HDR Support

Reel automatically detects and preserves HDR content using MediaInfo for color space analysis:
- Detects HDR based on color primaries (BT.2020, BT.2100)
- Recognizes HDR transfer characteristics (PQ, HLG)
- Adapts processing parameters and metadata handling for HDR sources

## Post-Encode Validation

Validation catches mismatches before you archive or publish results:
- **Video codec**: Ensures AV1 output and 10-bit depth
- **Audio codec**: Confirms all audio streams are transcoded to Opus with the expected track count
- **Dimensions**: Validates crop detection and output dimensions
- **Duration**: Compares input and output durations (±1 second tolerance)
- **HDR / Color space**: Uses MediaInfo to verify HDR flags and colorimetry
- **Audio sync**: Verifies audio drift is within 100ms tolerance

## Multi-Stream Audio Handling

- Automatically detects every audio stream and transcodes each to Opus
- Bitrate allocation per channel layout:
  - Mono: 64 kbps
  - Stereo: 128 kbps
  - 5.1: 256 kbps
  - 7.1: 384 kbps
  - Custom layouts: 48 kbps per channel

## Progress Reporting

Foreground runs show real-time progress with ETA, fps, and reduction stats. For automation, use the library API with a custom event handler (see [docs/spindle-integration.md](spindle-integration.md)).

## Environment Variables

- `NO_COLOR`: Disable colored output

## Debugging

```bash
# Verbose logging
reel encode -v -i input.mkv -o output/

# Check log files
ls ~/.local/state/reel/logs/
```
