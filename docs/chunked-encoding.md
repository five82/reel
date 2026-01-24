# Chunked Encoding in Reel

This document explains reel's parallel chunked encoding system, covering the complete pipeline from chunking through final muxing.

## Overview

Reel uses a **fixed-length chunked encoding** approach where:

1. The source video is split into fixed-length chunks
2. Multiple chunks are encoded in parallel using SVT-AV1
3. Encoded chunks are concatenated back into a single video
4. Audio is re-encoded and muxed with the final video

This approach enables efficient parallelization with predictable chunk sizes.

## Pipeline Stages

```
Input Video
    │
    ▼
┌─────────────────┐
│  FFMS2 Index    │ ─── Frame-accurate access, HDR metadata extraction
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Chunk Creation  │ ─── Split video into fixed-length chunks
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Crop Detection  │ ─── Automatic black bar removal (optional)
└────────┬────────┘
         │
         ▼
┌─────────────────────────────────────────────┐
│           Parallel Encoding                  │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐     │
│  │Worker 1 │  │Worker 2 │  │Worker N │     │
│  │SVT-AV1  │  │SVT-AV1  │  │SVT-AV1  │     │
│  └─────────┘  └─────────┘  └─────────┘     │
└────────┬────────────────────────────────────┘
         │
         ▼
┌─────────────────┐
│  IVF Concat     │ ─── Merge encoded chunks
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Audio Encoding  │ ─── Extract and re-encode to Opus
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Final Mux      │ ─── Combine video, audio, subtitles, chapters
└────────┬────────┘
         │
         ▼
    Output MKV
```

## Stage 1: Video Indexing (FFMS2)

Before any processing begins, reel creates an FFMS2 index of the source video. This enables:

- **Frame-accurate seeking**: Extract any frame by number without decode overhead
- **Metadata extraction**: Resolution, frame rate, HDR parameters
- **Parallel access**: Multiple workers can decode frames simultaneously

The index is cached as a `.ffindex` file alongside the source, allowing resume without re-indexing.

**Extracted metadata includes:**
- Resolution (width × height)
- Frame rate (numerator/denominator)
- Total frame count
- Bit depth (8-bit or 10-bit)
- Color primaries (BT.709, BT.2020, etc.)
- Transfer characteristics (SDR, PQ/HDR10, HLG)
- Matrix coefficients
- HDR mastering display metadata
- Content light level information

## Stage 2: Chunk Creation

The video is divided into fixed-length chunks for parallel encoding.

### Chunk Size

Chunks use a fixed frame count based on the video's frame rate:

```
chunk_frames = min(fps × 30, 1000)
```

For 24fps content, this is 720 frames (~30 seconds). For 60fps, it's 1000 frames (~17 seconds).

### Example

```
Total frames: 5000
Chunk size: 1000 frames

Result:
  Chunk 0: frames 0-1000
  Chunk 1: frames 1000-2000
  Chunk 2: frames 2000-3000
  Chunk 3: frames 3000-4000
  Chunk 4: frames 4000-5000
```

### Chunk Data Structure

Each chunk contains:
- **Index**: Sequential number (0-based)
- **Start frame**: Inclusive
- **End frame**: Exclusive

## Stage 3: Crop Detection

Automatic black bar detection identifies letterboxing/pillarboxing for removal.

### Detection Process

1. Sample frames throughout the video
2. Analyze edge pixels for consistent black bars
3. Calculate crop dimensions that remove black bars
4. Ensure dimensions are valid for AV1 encoding (mod-2)

### Settings

| Setting | Options | Description |
|---------|---------|-------------|
| `CropMode` | `auto`, `none` | Enable/disable automatic cropping |

When disabled (`--no-crop`), the full frame is encoded.

## Stage 4: Parallel Encoding

The core of reel's performance comes from parallel chunk encoding.

### Architecture

Reel uses a **streaming frame pipeline** where each worker decodes and encodes frames one at a time, dramatically reducing memory usage:

```
┌──────────────────┐
│ Chunk Dispatcher │ ─── Sends chunk metadata (not decoded frames)
└────────┬─────────┘
         │ Chunk
         ▼
┌──────────────────┐
│   Chunk Channel  │
└────────┬─────────┘
         │
    ┌────┼────┬────┐
    ▼    ▼    ▼    ▼
┌──────┐┌──────┐┌──────┐
│Worker││Worker││Worker│  ◄─── Each worker has own VidSrc
│  1   ││  2   ││  N   │
│decode││decode││decode│  ◄─── Decode 1 frame at a time
│encode││encode││encode│  ◄─── Stream to SVT-AV1 stdin
└──┬───┘└──┬───┘└──┬───┘
   │       │       │
   ▼       ▼       ▼
  .ivf    .ivf    .ivf
```

### Streaming Frame Pipeline

Each worker processes chunks using a streaming approach:

1. Receive chunk metadata (index, frame range)
2. Allocate single-frame buffer (~6 MB for 1080p 10-bit)
3. Start SVT-AV1 encoder process
4. Loop through frames:
   - Decode frame into buffer using FFMS2
   - Write frame to encoder stdin
   - Reuse same buffer for next frame
5. Close stdin and wait for encoder to finish

This approach uses **~99% less memory** than buffering all frames:
- Old: ~5 GB per chunk (900 frames × 6 MB)
- New: ~6 MB per worker (single frame buffer)

### Memory Management

With the streaming pipeline, memory usage is dramatically reduced:

1. **Per-worker frame buffer**: Each worker allocates a single-frame buffer (~6 MB for 1080p 10-bit)
2. **Semaphore**: Limits in-flight chunks to `workers + buffer` for orderly processing
3. **Per-worker VidSrc**: Each worker creates its own FFMS2 video source for thread safety
4. **SVT-AV1 overhead**: Each encoder process uses ~1 GB of memory

**Total memory per worker**: ~1 GB (encoder) + ~6 MB (frame buffer) ≈ 1 GB

### Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `Workers` | `numCPU/8` (1-4) | Parallel encoder instances |
| `ChunkBuffer` | `Workers` | Extra chunks to buffer |

Auto-detection allocates 1 worker per 8 CPU cores, minimum 1, maximum 4.

### SVT-AV1 Invocation

Each worker runs SVT-AV1 with YUV piped to stdin:

```
SvtAv1EncApp \
  -i stdin \
  --input-depth 10 \
  --color-format 1 \
  --profile 0 \
  --passes 1 \
  --width {width} \
  --height {height} \
  --fps-num {num} \
  --fps-denom {denom} \
  --frames {count} \
  --keyint 0 \
  --rc 0 \
  --scd 0 \
  --crf {value} \
  --preset {preset} \
  --tune {tune} \
  [HDR parameters] \
  -b output.ivf
```

Key parameters:
- `--keyint 0`: No forced keyframes (each chunk starts fresh)
- `--scd 0`: Disable internal scene detection
- `--passes 1`: Single-pass encoding
- `--rc 0`: CRF (constant quality) mode

### Resume Support

Encoding progress is tracked in `done.txt`:

```
0 847 1234567
1 776 1123456
2 1268 2345678
...
```

Format: `{chunk_index} {frame_count} {file_size}`

On resume, completed chunks are skipped and encoding continues from where it stopped.

## Stage 5: Chunk Concatenation

Encoded IVF files are merged into a single video stream.

### Process

1. List all `.ivf` files in the encode directory
2. Generate `concat.txt` for FFmpeg:
   ```
   file '/path/to/0000.ivf'
   file '/path/to/0001.ivf'
   ...
   ```
3. Run FFmpeg concat:
   ```
   ffmpeg -f concat -safe 0 -i concat.txt \
     -c copy \
     -r {fps} \
     -fflags +genpts+igndts+discardcorrupt+bitexact \
     -avoid_negative_ts make_zero \
     -reset_timestamps 1 \
     video.mkv
   ```

### Large File Handling

For videos with more than 500 chunks, a batched approach is used:

1. Merge chunks in groups of 500 to intermediate files
2. Merge intermediate files to final video

This avoids FFmpeg limitations with very large file lists.

## Stage 6: Audio Encoding

Audio is extracted from the source and re-encoded to Opus.

### Bitrate Calculation

Bitrate scales with channel count:

| Channels | Layout | Bitrate |
|----------|--------|---------|
| 1 | Mono | 64 kbps |
| 2 | Stereo | 128 kbps |
| 6 | 5.1 | 256 kbps |
| 8 | 7.1 | 384 kbps |
| Other | - | 48 kbps/channel |

### Command

```
ffmpeg -i source \
  -vn \
  -c:a libopus \
  -b:a {bitrate} \
  -ac {channels} \
  audio.mka
```

## Stage 7: Final Muxing

The final step combines all components into the output MKV.

### Inputs

1. **video.mkv**: Encoded AV1 video
2. **audio.mka**: Re-encoded Opus audio (if source has audio)
3. **source**: Original file for subtitles and chapters

### Command

```
ffmpeg \
  -i video.mkv \
  -i audio.mka \
  -i source \
  -map 0:v:0 \
  -map 1:a? \
  -map 2:s? \
  -c copy \
  -map_metadata 0 \
  -map_chapters 2 \
  -movflags +faststart \
  output.mkv
```

### Preserved Elements

- Video stream (newly encoded AV1)
- Audio stream (newly encoded Opus)
- Subtitle streams (copied from source)
- Chapter markers (copied from source)
- Container metadata

## Encoding Settings Reference

### Quality Settings

| Setting | CLI Flag | Default | Range | Description |
|---------|----------|---------|-------|-------------|
| CRF | `--crf` | 27 | 0-63 | Quality level (lower = better) |
| Preset | `--preset` | 6 | 0-13 | Speed/quality tradeoff (lower = slower) |
| Tune | `--tune` | 0 | 0+ | Encoder tuning mode |

### Advanced SVT-AV1 Settings

| Setting | CLI Flag | Default | Description |
|---------|----------|---------|-------------|
| AC Bias | `--ac-bias` | 0.1 | Coefficient bias |
| Variance Boost | `--variance-boost` | false | Enable quality boost |
| Variance Strength | `--variance-boost-strength` | 0 | Boost strength (0-255) |
| Variance Octile | `--variance-octile` | 0 | Octile selection |

### Processing Settings

| Setting | CLI Flag | Default | Description |
|---------|----------|---------|-------------|
| Crop Mode | `--no-crop` | auto | Disable auto-cropping |

### Parallel Encoding Settings

| Setting | CLI Flag | Default | Description |
|---------|----------|---------|-------------|
| Workers | `--workers` | auto | Parallel encoders |
| Buffer | `--buffer` | workers | Chunk buffer size |

## Work Directory Structure

```
work_dir/
├── encode/
│   ├── 0000.ivf      # Encoded chunk 0
│   ├── 0001.ivf      # Encoded chunk 1
│   └── ...
├── done.txt          # Completed chunks (for resume)
├── video.mkv         # Concatenated video
├── audio.mka         # Encoded audio
└── concat.txt        # FFmpeg concat file (temporary)
```

## Performance Considerations

### Worker Count

More workers increase parallelism with modest memory impact:
- Each worker uses ~1 GB (SVT-AV1 process + single frame buffer)
- Streaming design eliminates per-chunk YUV buffer overhead
- Auto-detection allocates 1 worker per 8 CPU cores (min 1, max 4)

### Buffer Size

The buffer setting controls how many chunks can be dispatched ahead:
- Default: Equal to worker count
- Increase for smoother worker utilization
- Has minimal memory impact (only chunk metadata is buffered, not frames)

### Chunk Size

Fixed-length chunks provide predictable encoding behavior:
- Consistent memory usage per worker
- Predictable encoding progress
- Simple resume semantics

## Troubleshooting

### Out of Memory

The streaming pipeline uses ~1 GB per worker (mostly SVT-AV1). If still running out of memory:
```bash
reel --workers 1 input.mkv
```

### Slow Encoding

If encoding seems slower than expected, you may be CPU-bound. Each worker runs an SVT-AV1 process:
```bash
reel --workers 2 input.mkv  # Try fewer workers on slower systems
```

### Resume After Crash

Simply re-run the same command. Completed chunks in `done.txt` will be skipped.

### Quality Issues at Chunk Boundaries

With fixed-length chunks, boundaries may occasionally fall mid-scene. SVT-AV1's internal quality management typically handles this well. If visible artifacts occur at chunk boundaries, this is a known limitation of the fixed-length approach.
