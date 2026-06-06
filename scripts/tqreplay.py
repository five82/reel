#!/usr/bin/env python3
"""
Replay target-quality scoring and selection decisions from existing TQ logs.

This lets you evaluate algorithmic changes (tie-break rules, score formulas,
convergence thresholds) without re-encoding.

Usage:
    python3 scripts/tqreplay.py <workdir> [options]

Options:
    --score-formula {mean_worst|mean|worst}
        mean_worst: (mean + worst) / 2   (default, current behavior)
        mean:       use mean_score only
        worst:      use worst_window_score only

    --tie-break {mean|worst}
        mean:  pick probe closest to target mean (old behavior)
        worst: pick probe with highest worst_window when means are tied (new behavior)

    --tolerance TOL
        Override the tolerance from the log (default: use log value)

Examples:
    # Replay with old mean-only tie-break
    python3 scripts/tqreplay.py ~/testing/.reel-soms-e0c9b46af397 --tie-break mean

    # Replay with worst-only scoring
    python3 scripts/tqreplay.py ~/testing/.reel-sully-29fa06f8601f --score-formula worst
"""

import argparse
import json
import math
import os
import sys
from typing import Optional


def load_tq_log(workdir: str) -> dict:
    path = os.path.join(workdir, "target-quality.json")
    if not os.path.exists(path):
        print(f"Error: {path} not found", file=sys.stderr)
        sys.exit(1)
    with open(path, "r") as f:
        return json.load(f)


def compute_probe_score(probe: dict, formula: str) -> float:
    mean = probe.get("mean_score", 0.0)
    worst = probe.get("worst_window_score", 0.0)
    if formula == "mean":
        return mean
    if formula == "worst":
        return worst if worst > 0 else mean
    # mean_worst: current behavior
    if worst > 0:
        return (mean + worst) / 2.0
    return mean


def best_probe(probes: list[dict], target: float, tolerance: float, upper_grace: float, tie_break: str) -> Optional[dict]:
    """Select best probe matching current logic."""
    epsilon = 0.001

    # First pass: only probes passing the worst-window floor
    candidates = []
    for p in probes:
        worst = p.get("worst_window_score", 0.0)
        if worst > 0 and worst < target - tolerance:
            continue  # below floor
        candidates.append(p)

    # If no candidates pass floor, fall back to all probes
    pool = candidates if candidates else probes
    if not pool:
        return None

    best_p = None
    best_err = float("inf")

    for p in pool:
        score = compute_probe_score(p, "mean_worst")  # convergence check always uses mean_worst
        err = abs(score - target)

        if best_p is None:
            best_p = p
            best_err = err
            continue

        if tie_break == "worst":
            # New behavior: within epsilon, prefer higher worst_window
            if err < best_err - epsilon:
                best_p = p
                best_err = err
            elif abs(err - best_err) <= epsilon:
                if p.get("worst_window_score", 0) > best_p.get("worst_window_score", 0):
                    best_p = p
                    best_err = err
        else:
            # Old behavior: closest mean wins
            if err < best_err:
                best_p = p
                best_err = err

    return best_p


def window_spread(windows: list[dict]) -> float:
    if not windows:
        return 0.0
    scores = [w["score"] for w in windows]
    return max(scores) - min(scores)


def replay_chunk(chunk: dict, args) -> dict:
    target = chunk["target"]
    tolerance = args.tolerance if args.tolerance is not None else chunk["tolerance"]
    upper_grace = chunk.get("tolerance", 0.12) * 0.25  # approximates UpperToleranceGrace

    probes = chunk.get("probes", [])
    if not probes:
        return {"chunk_idx": chunk["chunk_idx"], "error": "no probes"}

    selected = best_probe(probes, target, tolerance, upper_grace, args.tie_break)
    if selected is None:
        return {"chunk_idx": chunk["chunk_idx"], "error": "no valid probe"}

    original_crf = chunk.get("final_crf", 0)
    original_score = chunk.get("final_sample_score", 0)
    new_score = compute_probe_score(selected, args.score_formula)

    return {
        "chunk_idx": chunk["chunk_idx"],
        "frames": chunk.get("frames", 0),
        "original_crf": original_crf,
        "new_crf": selected["crf"],
        "original_score": original_score,
        "new_score": new_score,
        "score_formula": args.score_formula,
        "tie_break": args.tie_break,
        "changed": abs(original_crf - selected["crf"]) > 0.001,
        "probes": len(probes),
        "worst_window": selected.get("worst_window_score", 0),
        "window_spread": window_spread(selected.get("windows", [])),
    }


def main():
    parser = argparse.ArgumentParser(description="Replay TQ decisions from logs")
    parser.add_argument("workdir", help="Path to .reel-* work directory")
    parser.add_argument("--score-formula", choices=["mean_worst", "mean", "worst"], default="mean_worst")
    parser.add_argument("--tie-break", choices=["mean", "worst"], default="worst")
    parser.add_argument("--tolerance", type=float, default=None, help="Override tolerance")
    parser.add_argument("--show-diffs-only", action="store_true", help="Only show chunks where selection changed")
    parser.add_argument("--show-all", action="store_true", help="Show every chunk")
    args = parser.parse_args()

    log = load_tq_log(args.workdir)
    chunks = log.get("chunks", [])

    if not chunks:
        print("No chunks found in log")
        return

    diffs = []
    unchanged = 0
    total_chunks = len(chunks)

    for chunk in chunks:
        result = replay_chunk(chunk, args)
        if result.get("error"):
            print(f"chunk={result['chunk_idx']:04d} ERROR: {result['error']}")
            continue

        if result["changed"]:
            diffs.append(result)
        else:
            unchanged += 1

    if args.show_all:
        for r in diffs:
            print(format_result(r))
        print(f"\nTotal: {total_chunks} chunks, {unchanged} unchanged, {len(diffs)} changed")
    elif args.show_diffs_only:
        for r in diffs:
            print(format_result(r))
        print(f"\nTotal: {total_chunks} chunks, {unchanged} unchanged, {len(diffs)} changed")
    else:
        # Summary mode
        print(f"Workdir:    {args.workdir}")
        print(f"Score formula: {args.score_formula}")
        print(f"Tie-break:     {args.tie_break}")
        print(f"Tolerance:     {args.tolerance if args.tolerance is not None else 'from log'}")
        print()
        print(f"Total chunks:  {total_chunks}")
        print(f"Unchanged:     {unchanged} ({unchanged/total_chunks*100:.1f}%)")
        print(f"Changed:       {len(diffs)} ({len(diffs)/total_chunks*100:.1f}%)")

        if diffs:
            crf_deltas = [abs(r["original_crf"] - r["new_crf"]) for r in diffs]
            spreads = [r["window_spread"] for r in diffs]
            print(f"\nAmong changed chunks:")
            print(f"  CRF delta mean: {sum(crf_deltas)/len(crf_deltas):.2f}")
            print(f"  CRF delta max:  {max(crf_deltas):.2f}")
            print(f"  Window spread mean: {sum(spreads)/len(spreads):.4f}")
            print(f"  Window spread max:  {max(spreads):.4f}")
            print(f"\nChanged chunks:")
            for r in diffs:
                print(f"  {format_result(r)}")


def format_result(r: dict) -> str:
    return (f"chunk={r['chunk_idx']:04d} frames={r['frames']} "
            f"crf {r['original_crf']:.2f}→{r['new_crf']:.2f} "
            f"score {r['original_score']:.4f}→{r['new_score']:.4f} "
            f"spread={r['window_spread']:.4f} probes={r['probes']}")


if __name__ == "__main__":
    main()
