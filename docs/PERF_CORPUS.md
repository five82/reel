# Performance Test Corpus

The standard clip matrix used for Reel performance and accuracy testing, and the
rule for what lives in this repo versus the local testing directory.

For current tuning guidance read `docs/PERFORMANCE_TESTING.md`; for the dated
experiment record read `docs/PERFORMANCE_TESTING_LOG.md`.

## Where things live

The dividing line: **the repo holds the recipe and the logic; the testing
directory holds the bytes and the run output.**

| In the repo (versioned) | In the testing directory (local, not versioned) |
|-------------------------|--------------------------------------------------|
| `scripts/perf/clips.tsv` -- the clip matrix manifest (recipe) | The clip `*.mkv` files themselves (GB-scale) |
| This doc -- corpus knowledge (tiers, grain, methodology) | The source film rips the clips were cut from (personal media) |
| `scripts/perf/` harness + analyzers (logic) | Per-run output: workdirs, logs, GPU traces, dated A/B dirs |
| Distilled conclusions in `docs/PERFORMANCE_TESTING*.md` | Suite run outputs |

The testing directory root is `$REEL_TESTING_DIR` (default `~/testing`). Repo
scripts resolve clips through that root plus the manifest; they must fail with a
clear "clip not found under $REEL_TESTING_DIR" message rather than silently
skipping a missing clip. No clip paths are hardcoded in repo scripts.

Run evidence (the dated `perf-ab/`, `rebaseline-*`, `fulllen-attr/` directories
catalogued in the `docs/PERFORMANCE_TESTING_LOG.md` artifact map) stays under the
testing directory. Its conclusions are already distilled into the log; the raw
artifacts are local provenance, not versioned.

## The matrix

Clip names follow `<abbr>-<length>-<res>-<range>`, e.g. `io-5m-4k-hdr`,
`bts-20m-1080p-sdr`. Lengths are 5m/10m/20m cuts.

Cuts are taken from the middle 60% of each film (skipping opening/closing
credits and intros) and are non-overlapping: the 5m, 10m, and 20m cuts of the
same film are distinct segments, so a shorter cut is NOT a subset of a longer
one and their results are not directly comparable.

Resolution / range tiers:
- 1080p SDR: air, bts, im, soms
- 4K HDR:   io, kbv1, ko, sully

The standard sweep matrix is `air-5m`, `im-5m`, `bts-5m`, `sully-5m`, `kbv1-5m`,
and the deterministic high-variance stress asset `sullyhv-15m-4k-hdr`.

## Film mapping, format, and grain

Grain profiles researched 2026-06-13 (shooting format + Blu-ray/4K transfer
reviews); use them to pick clips by content type. "Grain" buckets:
clean < light < moderate < heavy.

| abbr | Film                          | Tier      | Format                                   | Grain          |
|------|-------------------------------|-----------|------------------------------------------|----------------|
| io   | Inside Out (2015)             | 4K HDR    | CG animation (Pixar RenderMan, 2K DI)    | clean          |
| sully| Sully (2016)                  | 4K HDR    | Digital, ARRI Alexa 65 (6K large-format) | clean          |
| ko   | Knives Out (2019)             | 4K HDR    | Digital, ARRI Alexa Mini + Alexa 65; intentional film-grain emulation in post | light-moderate |
| kbv1 | Kill Bill Vol 1 (2003)        | 4K HDR    | Super 35mm film (Kodak mix)              | moderate       |
| air  | Air (2023)                    | 1080p SDR | Digital, ARRI Alexa 35 (ARRIRAW)         | clean-light    |
| soms | Secret of My Success (1987)   | 1080p SDR | 35mm film                                | light          |
| im   | Inside Man (2006)             | 1080p SDR | Super 35mm film                          | moderate       |
| bts  | Back to School (1986)         | 1080p SDR | 35mm film                                | moderate (heavier in dark scenes) |

Notes:
- 4K grain spread: io/sully are clean, ko light-moderate (added grain),
  kbv1 the grainiest 4K option -> use kbv1 for a 4K grain stress test.
- 1080p grain spread: air clean-light, soms light, im/bts moderate.
- Grain is uneven within a film (dark interiors grainier than bright
  exteriors); kbv1's B&W opening and anime sequence have atypical grain, so
  pick a standard live-action scene if cutting a new kbv1 clip.
- Knives Out's "35mm" entries in some spec databases are artifacts; capture was
  digital ARRIRAW with grain added in the DI.

## Manifest format and reconstruction

`scripts/perf/clips.tsv` is the recipe. Columns: `abbr`, `minutes`,
`resolution`, `dynamic_range`, `start`, `end`, `clip` (output filename), and
`source`. A clip is the source film stream-copied (or cut) over the `[start,
end]` range into `clip`.

The `source` column records the rip the clip was originally cut from as
historical provenance. Those paths point into the local spindle rip cache, which
may be evicted; the durable identity of each clip is the abbr -> film mapping in
the table above plus the timecodes. Clips are reconstructed by hand from a row
when needed -- there is intentionally no automated builder, because the corpus
already exists and an auto-cutter would rot against the volatile source paths.
