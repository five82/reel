#!/usr/bin/env bash
#
# Extract short clips around the worst chunks from an existing reel run.
# These clips can be used for fast end-to-end validation (2-3 min encode
# instead of 30-60 min).
#
# Usage:
#   scripts/extract-worst-clips.sh <workdir> <input.mkv> [output_dir]
#
# Example:
#   scripts/extract-worst-clips.sh ~/testing/.reel-sully-29fa06f8601f \
#       ~/testing/sully.mkv ~/testing/sully-clips

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKDIR="${1:-}"
INPUT="${2:-}"
OUTDIR="${3:-${WORKDIR}-clips}"

if [[ -z "$WORKDIR" || -z "$INPUT" ]]; then
    echo "Usage: $(basename "$0") <workdir> <input.mkv> [output_dir]"
    exit 1
fi

if [[ ! -f "$WORKDIR/target-quality.json" ]]; then
    echo "Error: $WORKDIR/target-quality.json not found"
    exit 1
fi

if [[ ! -f "$INPUT" ]]; then
    echo "Error: $INPUT not found"
    exit 1
fi

mkdir -p "$OUTDIR"

# Find the worst chunks by window_spread
python3 -c "
import json, sys
with open('$WORKDIR/target-quality.json') as f:
    log = json.load(f)

chunks = []
for c in log.get('chunks', []):
    windows = c.get('probes', [{}])[-1].get('windows', [])
    if not windows:
        continue
    scores = [w['score'] for w in windows]
    spread = max(scores) - min(scores) if scores else 0
    chunks.append({
        'idx': c['chunk_idx'],
        'frames': c['frames'],
        'spread': spread,
        'probes': len(c.get('probes', [])),
        'final_crf': c.get('final_crf', 0),
        'final_score': c.get('final_sample_score', 0),
    })

chunks.sort(key=lambda x: x['spread'], reverse=True)
# Also include chunks that hit max_probes
max_probe = [c for c in chunks if c['probes'] >= 4]
max_probe.sort(key=lambda x: x['probes'], reverse=True)

# Print worst by spread
for c in chunks[:10]:
    print(f\"SPREAD {c['idx']:04d} {c['frames']} {c['spread']:.4f} {c['probes']} {c['final_crf']}\")

# Print worst by probe count
for c in max_probe[:5]:
    print(f\"PROBES {c['idx']:04d} {c['frames']} {c['spread']:.4f} {c['probes']} {c['final_crf']}\")
" > "$OUTDIR/worst-chunks.txt"

# Extract clips using ffmpeg. We expand by 5 seconds before/after the chunk.
# Chunk offsets are frame-based; we convert to seconds using the FPS from the log.
FPS=$(python3 -c "
import json
with open('$WORKDIR/target-quality.json') as f:
    log = json.load(f)
# Get FPS from first chunk's probe_sample_frames and frames estimate
# Actually just probe the input
import subprocess
result = subprocess.run(['ffprobe', '-v', 'error', '-select_streams', 'v:0',
    '-show_entries', 'stream=r_frame_rate', '-of', 'default=noprint_wrappers=1',
    '$INPUT'], capture_output=True, text=True)
if result.returncode == 0:
    rate = result.stdout.strip().split('=')[1]
    num, den = rate.split('/')
    print(float(num) / float(den))
else:
    print(24.0)
")

# Read chunk boundaries from the chunk-plan.txt or recompute from TQ log
# Actually we need the chunk start/end times. The TQ log doesn't have them.
# But the workdir should have chunk-plan.txt or similar.
CHUNK_PLAN="$WORKDIR/chunk-plan.txt"
if [[ ! -f "$CHUNK_PLAN" ]]; then
    echo "Warning: $CHUNK_PLAN not found. Cannot extract clips without boundary info."
    echo "Worst chunks written to $OUTDIR/worst-chunks.txt"
    exit 0
fi

# Parse chunk-plan.txt to get start frame, end frame for each chunk
# Format is typically: chunk idx=0000 start=0 end=336
awk '/^chunk idx=/ {
    split($0, parts, " ")
    for (i in parts) {
        if (parts[i] ~ /^idx=/) { idx = substr(parts[i], 5) }
        if (parts[i] ~ /^start=/) { start = substr(parts[i], 7) }
        if (parts[i] ~ /^end=/) { end = substr(parts[i], 5) }
    }
    print idx, start, end
}' "$CHUNK_PLAN" > "$OUTDIR/chunk-boundaries.txt"

# Extract worst clips
echo "Extracting clips to $OUTDIR ..."
while read -r kind idx frames spread probes crf; do
    # Find the chunk boundaries
    boundary=$(awk -v idx="$idx" '$1 == idx {print $2, $3}' "$OUTDIR/chunk-boundaries.txt")
    if [[ -z "$boundary" ]]; then
        echo "  Skip $idx: boundary not found"
        continue
    fi
    read -r start_frame end_frame <<< "$boundary"

    # Add 5 seconds padding (in frames)
    pad_frames=$(python3 -c "print(int(5 * $FPS))")
    start_pad=$(( start_frame > pad_frames ? start_frame - pad_frames : 0 ))
    end_pad=$(( end_frame + pad_frames ))

    start_sec=$(python3 -c "print(f\"{${start_pad} / ${FPS}:.3f}\")")
    duration_sec=$(python3 -c "print(f\"{(${end_pad} - ${start_pad}) / ${FPS}:.3f}\")")

    outclip="$OUTDIR/${kind}_${idx}_crf${crf}_spread${spread}.mkv"
    echo "  $outclip (${start_sec}s +${duration_sec}s)"
    ffmpeg -y -hide_banner -loglevel error \
        -ss "$start_sec" -t "$duration_sec" -i "$INPUT" \
        -c copy -map 0:v:0 -map 0:a? \
        "$outclip" 2>/dev/null || true
done < <(grep -E '^(SPREAD|PROBES)' "$OUTDIR/worst-chunks.txt" | head -8)

echo "Done. Clips in $OUTDIR"
