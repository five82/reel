#!/usr/bin/env python3
"""
Summarize a perf run directory produced by run-suite.sh.

For each clip it reads perf.json (phase wall time + adaptive-worker history),
target-quality.json (probe histogram, stop reasons, JOD, encode-vs-metric
seconds, window spread), the GPU sample log, and the results.tsv row (wall time,
output size, GPU summary). It prints a per-clip table and writes summary.json
into the run directory for compare-runs.py.

Usage:
    scripts/perf/analyze.py <run_dir>
    scripts/perf/analyze.py <clip_dir>        # a single clip's harvested dir

For a per-chunk diff of two target-quality runs, use scripts/compare-tq.py.
For full-chunk CVVDP ground truth, use scripts/fullvalidate.
"""

import json
import os
import re
import statistics
import sys


def _load_json(path):
    try:
        with open(path) as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def _find_json(clip_dir, name):
    """A harvested artifact sits in the clip dir; with --keep-workdirs it is
    still inside the .reel-* workdir."""
    direct = os.path.join(clip_dir, name)
    if os.path.exists(direct):
        return _load_json(direct)
    for entry in sorted(os.listdir(clip_dir)):
        if entry.startswith(".reel-"):
            p = os.path.join(clip_dir, entry, name)
            if os.path.exists(p):
                return _load_json(p)
    return None


def _percentile(values, p):
    if not values:
        return None
    s = sorted(values)
    i = int(p * (len(s) - 1))
    return s[max(0, min(i, len(s) - 1))]


def _final_probe(probes, final_crf):
    """The chunk's chosen probe -- the one whose CRF matches final_crf, mirroring
    reel's finalProbe (target_quality.go). Falls back to the last probe."""
    if not probes:
        return None
    if final_crf is not None:
        for p in probes:
            if round((p.get("crf") or 0) * 4) == round(final_crf * 4):
                return p
    return probes[-1]


def perf_stats(perf):
    if not perf:
        return {}
    phases = {ph["name"]: ph["duration_seconds"] for ph in perf.get("phases", [])}
    hist = perf.get("worker_history", [])
    out = {
        "total_s": perf.get("total_seconds"),
        "frames": perf.get("frames"),
        "chunks_meta": perf.get("chunks"),
        "metric_workers": perf.get("metric_workers"),
        "max_adaptive_workers": perf.get("max_adaptive_workers"),
        "phase_crop_s": phases.get("Crop detection"),
        "phase_shotdet_s": phases.get("Shot cut detection"),
        "phase_encode_s": phases.get("Video encoding"),
        "phase_audio_s": phases.get("Audio extraction"),
        "phase_merge_s": phases.get("Video merge"),
        "phase_mux_s": phases.get("Final mux"),
        "phase_validate_s": phases.get("Output validation"),
        "phase_scan_in_s": phases.get("Stream size scan (input)"),
    }
    if hist:
        out["max_active"] = max(s["active"] for s in hist)
        out["max_target"] = max(s["target"] for s in hist)
        out["peak_in_flight"] = max(s["in_flight"] for s in hist)
        # encode_slot_wait_seconds is cumulative; the last sample is the total.
        out["slot_wait_s"] = hist[-1].get("encode_slot_wait_seconds")
    return out


def tq_stats(tq):
    if not tq:
        return {}
    target = tq.get("target")
    chunks = tq.get("chunks", [])
    if not chunks:
        return {"chunks": 0}
    scores = [c["final_sample_score"] for c in chunks]
    probe_counts = [len(c.get("probes", [])) for c in chunks]
    total_probes = sum(probe_counts)
    hist = {}
    for n in probe_counts:
        hist[n] = hist.get(n, 0) + 1
    stops = {}
    for c in chunks:
        key = c.get("stop_reason") or "none"
        stops[key] = stops.get(key, 0) + 1

    probe_encode_s = 0.0
    metric_s = 0.0
    final_encode_s = 0.0
    spreads = []
    for c in chunks:
        final_encode_s += c.get("final_encode_seconds", 0.0) or 0.0
        probes = c.get("probes", [])
        for p in probes:
            probe_encode_s += p.get("encode_seconds", 0.0) or 0.0
            metric_s += p.get("metric_seconds", 0.0) or 0.0
        # Window spread is taken from the chunk's chosen probe so it matches
        # reel's own "TQ window_spread" summary line.
        fp = _final_probe(probes, c.get("final_crf"))
        wins = [w["score"] for w in fp.get("windows", [])] if fp else []
        if wins:
            spreads.append(max(wins) - min(wins))

    mae = None
    if target is not None:
        mae = statistics.mean(abs(s - target) for s in scores)

    return {
        "target": target,
        "metric_workers": tq.get("metric_workers"),
        "chunks": len(chunks),
        "probes": total_probes,
        "probes_per_chunk": total_probes / len(chunks),
        "jod_min": min(scores),
        "jod_mean": statistics.mean(scores),
        "jod_max": max(scores),
        "jod_mae": mae,
        "encode_lane_s": probe_encode_s + final_encode_s,
        "metric_s": metric_s,
        "spread_p90": _percentile(spreads, 0.90),
        "spread_max": max(spreads) if spreads else None,
        "ge3_probe_chunks": sum(1 for n in probe_counts if n >= 3),
        "probe_hist": {str(k): hist[k] for k in sorted(hist)},
        "stop_reasons": stops,
    }


def load_results_tsv(run_dir):
    path = os.path.join(run_dir, "results.tsv")
    rows = {}
    if not os.path.exists(path):
        return rows
    with open(path) as f:
        header = f.readline().rstrip("\n").split("\t")
        for line in f:
            parts = line.rstrip("\n").split("\t")
            if len(parts) != len(header):
                continue
            row = dict(zip(header, parts))
            rows[row.get("clip", "")] = row
    return rows


def summarize_clip(clip_dir, results_row=None):
    perf = _find_json(clip_dir, "perf.json")
    tq = _find_json(clip_dir, "target-quality.json")
    out = {"clip": os.path.basename(clip_dir.rstrip("/"))}
    if results_row:
        out["rc"] = int(results_row.get("rc", -1))
        out["wall_s"] = float(results_row.get("elapsed_s", 0) or 0)
        out["out_bytes"] = int(results_row.get("out_bytes", 0) or 0)
        for k in ("gpu_mean", "gpu_p90", "vram_peak_mib"):
            v = results_row.get(k, "na")
            out[k] = None if v in ("na", "") else float(v)
    out.update(perf_stats(perf))
    if tq:
        out.update(tq_stats(tq))
    return out


def summarize_run(run_dir):
    """Return a list of per-clip summary dicts for a run dir (or a single clip
    dir / workdir)."""
    results = load_results_tsv(run_dir)
    clip_dirs = []
    # A run dir holds clip subdirs; a clip dir holds perf.json directly or a
    # .reel-* workdir.
    if os.path.exists(os.path.join(run_dir, "perf.json")) or \
            any(e.startswith(".reel-") for e in os.listdir(run_dir)):
        clip_dirs = [run_dir]
    else:
        for entry in sorted(os.listdir(run_dir)):
            full = os.path.join(run_dir, entry)
            if os.path.isdir(full):
                clip_dirs.append(full)
    summaries = []
    for d in clip_dirs:
        row = results.get(os.path.basename(d.rstrip("/")))
        s = summarize_clip(d, row)
        if s.get("total_s") is not None or s.get("chunks") is not None or s.get("wall_s") is not None:
            summaries.append(s)
    return summaries


def _fmt(v, spec):
    if isinstance(v, (int, float)):
        return format(v, spec)
    m = re.match(r"[+\-]?0?(\d+)", spec)
    width = int(m.group(1)) if m else 2
    return format("na", f">{width}")


def print_table(summaries):
    cols = ["clip", "wall_s", "MB", "gpu%", "vram", "chunks", "p/ch",
            "jod_mean", "mae", "enc_s", "met_s", "spr_p90", "encode_s", "shot_s"]
    print(f"{cols[0]:<26} {cols[1]:>7} {cols[2]:>7} {cols[3]:>5} {cols[4]:>6} "
          f"{cols[5]:>6} {cols[6]:>5} {cols[7]:>8} {cols[8]:>6} {cols[9]:>7} "
          f"{cols[10]:>7} {cols[11]:>7} {cols[12]:>8} {cols[13]:>7}")
    print("-" * 126)
    for s in summaries:
        mb = (s.get("out_bytes") or 0) / 1e6
        print(f"{s['clip']:<26} "
              f"{_fmt(s.get('wall_s'), '7.0f')} "
              f"{mb:7.1f} "
              f"{_fmt(s.get('gpu_mean'), '5.0f')} "
              f"{_fmt(s.get('vram_peak_mib'), '6.0f')} "
              f"{_fmt(s.get('chunks'), '6d')} "
              f"{_fmt(s.get('probes_per_chunk'), '5.2f')} "
              f"{_fmt(s.get('jod_mean'), '8.4f')} "
              f"{_fmt(s.get('jod_mae'), '6.4f')} "
              f"{_fmt(s.get('encode_lane_s'), '7.0f')} "
              f"{_fmt(s.get('metric_s'), '7.0f')} "
              f"{_fmt(s.get('spread_p90'), '7.4f')} "
              f"{_fmt(s.get('phase_encode_s'), '8.0f')} "
              f"{_fmt(s.get('phase_shotdet_s'), '7.1f')}")


def main():
    if len(sys.argv) != 2:
        print(__doc__)
        sys.exit(2)
    run_dir = sys.argv[1]
    if not os.path.isdir(run_dir):
        print(f"analyze: not a directory: {run_dir}", file=sys.stderr)
        sys.exit(1)
    summaries = summarize_run(run_dir)
    if not summaries:
        print(f"analyze: no perf.json / target-quality.json found under {run_dir}", file=sys.stderr)
        sys.exit(1)
    print_table(summaries)
    out_path = os.path.join(run_dir, "summary.json")
    try:
        with open(out_path, "w") as f:
            json.dump(summaries, f, indent=2)
        print(f"\nwrote {out_path}")
    except OSError:
        pass


if __name__ == "__main__":
    main()
