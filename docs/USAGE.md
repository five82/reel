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
- `--target-quality <LOW-HIGH>`: CVVDP JOD target range (default `9.15-9.55`)
- `--crf-range <LOW-HIGH>`: target-quality search bounds (default `4.25-63.75`)
- `--cvvdp-display <PATH>`: optional VSHIP/CVVDP display JSON; otherwise Reel generates a normal-viewing `reel` model. Custom JSON must contain a top-level `reel` model.
- `--metric-workers <N>`: concurrent VSHIP/CUDA scoring workers (resolution-aware default: `6` below 4K, `4` for 4K/UHD)
- `--max-probes <N>`: maximum target-quality probes per chunk (default `6`)
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
- `--keep-workdir`: Keep the `.reel-*` work directory after successful encodes for probe/log analysis

## Parallel Chunked Encoding

Reel splits video into chunks, encodes chunks in parallel with SVT-AV1, merges the encoded chunks, then muxes video with Opus audio, chapters, and metadata. Fixed-CRF mode keeps simple duration-based chunking. Target-quality mode uses shot detection plus target-aware packing to reduce chunk count while keeping CRF decisions visually coherent.

Worker count adapts during encoding: Reel starts conservatively, tests higher concurrency by recent throughput, and backs off on RAM or swap pressure. Target-quality chunks are scheduled in timeline blocks, largest-first within each block, so nearby completed chunks can seed adaptive CRF priors without creating a long-tail of large chunks.

Target-quality mode is enabled in the default build and requires a working `libvship`/CUDA install. Build with `-tags no_vship` to disable target-quality mode entirely and default to fixed-CRF mode.

Interrupted runs can be resumed by running the same command again. Completed chunks are kept in Reel's temporary work directory until the final output is created. See `docs/RESUME_DURABILITY.md` for the crash-recovery audit and open hardening items.

## Target-Quality Scoring

By default, target-quality mode searches for CVVDP scores in the `9.15-9.55` range and caps planned chunks at 12 seconds. Reel scores three 48-frame windows for larger chunks; chunks up to 256 frames are scored as a whole. When the first adaptive CRF prior comes from the title median, chunks up to 720 frames may be encoded fully for the first probe so that a converged probe can be reused as final output.

The sampled score is intentionally conservative: Reel logs the window mean and worst window, then uses the midpoint of those two values for the search decision. Scores slightly above the upper bound, up to `+0.02` JOD, are accepted so Reel does not spend extra probes shrinking already-excellent chunks. There is no matching lower-side grace, and a probe does not converge if any sampled window falls below the lower bound.

Verbose output includes each sampled window score and `window_spread`; large spreads are a useful signal that a chunk may contain mixed visual complexity and may deserve closer inspection.

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
