#!/usr/bin/env python3
"""Diff two TU_DEBUG traces and report what changed.

Use case: compare trace before and after a Mesa patch.
"""
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from trace_parser import parse_trace


def diff_traces(before_path: str, after_path: str) -> dict:
    before = parse_trace(before_path)
    after = parse_trace(after_path)

    regs_before = set(r['reg'] for r in before.get('top_registers', []))
    regs_after = set(r['reg'] for r in after.get('top_registers', []))

    added = sorted(regs_after - regs_before)
    removed = sorted(regs_before - regs_after)

    return {
        'before': before,
        'after': after,
        'writes_delta': after['total_writes'] - before['total_writes'],
        'unique_regs_delta': after['unique_regs'] - before['unique_regs'],
        'barriers_delta': after['barriers'] - before['barriers'],
        'draws_delta': after['draws'] - before['draws'],
        'added_regs': sorted(added),
        'removed_regs': sorted(removed),
    }


def main():
    if len(sys.argv) < 3:
        print(f"Usage: {sys.argv[0]} <before_trace.log> <after_trace.log> [diff.json]")
        sys.exit(1)

    result = diff_traces(sys.argv[1], sys.argv[2])
    out = sys.argv[3] if len(sys.argv) > 3 else 'output/diffs/trace_diff.json'

    import os
    os.makedirs(os.path.dirname(out), exist_ok=True)

    with open(out, 'w') as f:
        json.dump(result, f, indent=2)

    delta = result['writes_delta']
    direction = "fewer" if delta < 0 else "more"
    print(f"  Writes delta:     {delta:+d} ({direction})")
    print(f"  Unique regs delta:{result['unique_regs_delta']:+d}")
    print(f"  Barriers delta:   {result['barriers_delta']:+d}")
    print(f"  Removed regs:     {len(result['removed_regs'])}")
    print(f"  Added regs:       {len(result['added_regs'])}")
    print(f"  Wrote {out}")


if __name__ == '__main__':
    main()
