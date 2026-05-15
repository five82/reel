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
│ Video Analysis  │ ─── FFmpeg/libav probing, decode access, HDR metadata extraction
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
│ Audio Encoding  │ ─── Decode with libav and encode to Opus with libopusenc
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

## Stage 1: Video Analysis (FFmpeg/libav)

Before encoding begins, reel probes the source video with FFmpeg/libav. This enables:

- **Decode access**: Seek near requested frame positions and decode forward as needed
- **Metadata extraction**: Resolution, frame rate, HDR parameters
- **Parallel access**: Each worker opens its own decoder instance

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

### Chunk Duration

Chunk duration varies by resolution to balance encoder efficiency with parallelism:

| Resolution | Chunk Duration | Example (24fps) |
|------------|----------------|-----------------|
| SD/720p | 20 seconds | 480 frames |
| 1080p | 30 seconds | 720 frames |
| 4K | 45 seconds | 1080 frames |

Formula: `chunk_frames = fps × chunk_duration`

Longer chunks for higher resolutions provide better encoder warmup and efficiency.

### Example

```
1080p video at 24fps:
Total frames: 5000
Chunk duration: 30 seconds
Chunk size: 720 frames

Result:
  Chunk 0: frames 0-720
  Chunk 1: frames 720-1440
  Chunk 2: frames 1440-2160
  ...
  Chunk 6: frames 4320-5000 (final partial chunk)
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
│Worker││Worker││Worker│  ◄─── Each worker has own decoder
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
3. Start an SVT-AV1 library encoder instance
4. Loop through frames:
   - Decode frame into buffer using FFmpeg/libav
   - Send frame to SVT-AV1
   - Reuse same buffer for next frame
5. Drain packets and close the encoder

This approach uses **~99% less memory** than buffering all frames:
- Old: ~5 GB per chunk (900 frames × 6 MB)
- New: ~6 MB per worker (single frame buffer)

### Memory Management

With the streaming pipeline, memory usage is dramatically reduced:

1. **Per-worker frame buffer**: Each active worker allocates a single-frame buffer (~6 MB for 1080p 10-bit)
2. **Adaptive limiter**: Starts with conservative concurrency and ramps up/down based on real RAM/swap pressure
3. **Per-worker decoder**: Each active worker creates its own FFmpeg/libav decoder for thread safety
4. **SVT-AV1 overhead**: SVT internal memory varies significantly by resolution, preset, content, and SVT version

Reel intentionally avoids fixed per-worker memory estimates. They proved too inaccurate for the SVT-AV1 library API.

### Settings

Adaptive encoding uses the host logical CPU count as its hardware-derived ceiling. It starts below that ceiling and ramps while monitoring `MemAvailable` and swap growth from `/proc/meminfo`.

### SVT-AV1 Configuration

Each active worker owns one SVT-AV1 library encoder instance and writes an IVF chunk.

Key parameters:
- `keyint`: Keyframe interval of 10 seconds (e.g., 240 frames for 24fps)
- `scd=0`: Scene-change keyframe insertion disabled; chunks naturally start at keyframes
- `passes=1`: Single-pass encoding
- `rc=0`: CRF (constant quality) mode
- `level_of_parallelism`: Set internally based on the adaptive worker ceiling so each encoder does not auto-size as if it owns the whole machine

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

Audio streams are decoded from the source with libav/libswresample and encoded to Opus with libopusenc. Each source audio stream is encoded in parallel to a separate `.opus` file. Reel preserves the input channel count; surround layouts may be reordered for Opus mapping but are not downmixed.

### Bitrate Calculation

Bitrate scales with channel count:

| Channels | Layout | Bitrate |
|----------|--------|---------|
| 1 | Mono | 76 kbps |
| 2 | Stereo | 128 kbps |
| 6 | 5.1 | 258 kbps |
| 8 | 7.1 | 331 kbps |
| Other | - | `128 * (channelEquivalent / 2)^0.75` |

### Native pipeline

```
source audio stream N
  -> libavcodec decode
  -> libswresample convert to 48 kHz packed float
  -> optional surround channel reorder for Opus mapping
  -> libopusenc encode
  -> audio_NN.opus
```

## Stage 7: Final Muxing

The final step combines all components into the output MKV.

### Inputs

1. **video.mkv**: Encoded AV1 video
2. **audio_NN.opus**: Per-stream Opus audio files (if source has audio)
3. **source**: Original file for subtitles and chapters

### Command

```
ffmpeg \
  -i video.mkv \
  -i audio_00.opus \
  -i audio_01.opus \
  -i source \
  -map 0:v:0 \
  -map 1:a:0 \
  -map 2:a:0 \
  -map 3:s? \
  -c copy \
  -map_metadata 3 \
  -map_chapters 3 \
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
| CRF | `--crf` | varies | 0-63 | Quality level (lower = better) |
| Preset | `--preset` | 6 | 0-13 | Speed/quality tradeoff (lower = slower) |
| Tune | `--tune` | 0 | 0+ | Encoder tuning mode |

CRF defaults vary by resolution:
- SD (<1920 width): 24
- HD (1920-3839 width): 26
- UHD (≥3840 width): 26

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
| Crop Mode | `--disable-autocrop` | auto | Disable auto-cropping |
| Decode Backend | `--decode` | cuda | Use `software` to force CPU decode |

## Work Directory Structure

```
work_dir/
├── encode/
│   ├── 0000.ivf      # Encoded chunk 0
│   ├── 0001.ivf      # Encoded chunk 1
│   └── ...
├── done.txt          # Completed chunks (for resume)
├── video.mkv         # Concatenated video
├── audio_00.opus     # Encoded audio stream 0
├── audio_01.opus     # Encoded audio stream 1 (if present)
└── concat.txt        # FFmpeg concat file (temporary)
```

## Performance Considerations

### Worker Count

More workers increase parallelism but require more memory. Reel starts conservatively and adjusts active workers dynamically based on real memory pressure. Swap growth is treated as a performance warning and causes Reel to reduce concurrency.

### Chunk Duration

Resolution-based chunk durations balance efficiency and parallelism:
- SD/720p: 20s chunks for faster iteration
- 1080p: 30s chunks for balanced performance
- 4K: 45s chunks for better encoder warmup

## Troubleshooting

### Out of Memory

Reel should reduce adaptive concurrency before swap pressure becomes severe. If memory pressure still cancels the encode, re-run the same command to resume from completed chunks.

### Slow Encoding

If encoding seems slower than expected, watch the progress bar's worker count. Reel may be holding concurrency down to stay inside RAM and avoid swap thrash.

### Resume After Crash

Simply re-run the same command. Completed chunks in `done.txt` will be skipped.

### Quality Issues at Chunk Boundaries

With fixed-length chunks, boundaries may occasionally fall mid-scene. Regular keyframe interval (`keyint` at 10 seconds) helps maintain seekability across chunk boundaries. Visible artifacts at boundaries are rare but possible with very fast motion at chunk edges.
