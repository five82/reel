# Performance Testing

LLM quick-start for Reel performance work. Read this file before proposing a
performance change. Open `docs/PERFORMANCE_TESTING_LOG.md` only when you need the
decision provenance for a specific row below.

## Purpose

These notes exist to stop expensive retesting. Real encodes are slow, GPU/CVVDP
results can be hardware- and build-sensitive, and a later coding session will not
remember what a previous session already tried. The docs should therefore answer:

1. What are the current defaults and why are they that way?
2. What has already been tested, kept, rejected, or marked unsafe?
3. What is still worth testing next?
4. Which artifact should a future agent inspect instead of rerunning an encode?

They are not a raw lab notebook. Large logs, per-run JSON, GPU traces, and
one-off scripts belong under `$REEL_TESTING_DIR` (default `~/testing`) or in git
history, with only the decisive summary kept here.
