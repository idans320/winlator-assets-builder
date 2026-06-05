#!/usr/bin/env python3
"""Parse TU_DEBUG=trace logs from Turnip into structured records.

Input: TU_DEBUG log file with lines like:
  [TU_PKT4] reg=0x0800 func=tu6_emit_binning_pass val=0x00000001
  [TU_PKT7] op=0x26 func=tu_cs_emit_wfi
  [TU_DRAW]  func=tu_CmdDraw

Output: JSON with per-submit breakdown.
"""
import re
import json
import sys
from collections import defaultdict
from pathlib import Path

RE_PKT4 = re.compile(r'\[TU_TRACE\]\s+PKT4\s+reg=(0x[0-9a-fA-F]+)\s+cnt=(\d+)')
RE_WRITE_REG = re.compile(r'\[TU_TRACE\]\s+WRITE_REG\s+reg=(0x[0-9a-fA-F]+)\s+val=(0x[0-9a-fA-F]+)')
RE_PKT7 = re.compile(r'\[TU_TRACE\]\s+PKT7\s+op=(0x[0-9a-fA-F]+)\s+cnt=(\d+)')
RE_DRAW = re.compile(r'\[TU_TRACE\]\s+DRAW')
RE_BARRIER = re.compile(r'\[TU_TRACE\]\s+BARRIER')


def parse_trace(path: str) -> dict:
    records = []

    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue

            m = RE_WRITE_REG.search(line)
            if m:
                records.append({
                    'type': 'PKT4',
                    'reg': int(m.group(1), 16),
                    'func': 'tu_cs_emit_write_reg',
                    'value': int(m.group(2), 16),
                })
                continue

            m = RE_PKT4.search(line)
            if m:
                cnt = int(m.group(2))
                reg_base = int(m.group(1), 16)
                for i in range(cnt):
                    records.append({
                        'type': 'PKT4',
                        'reg': reg_base + i,
                        'func': 'tu_cs_emit_pkt4',
                        'value': None,
                    })
                continue

            m = RE_PKT7.search(line)
            if m:
                records.append({
                    'type': 'PKT7',
                    'opcode': int(m.group(1), 16),
                    'func': 'tu_cs_emit_pkt7',
                })
                continue

    return compute_stats(records)


def compute_stats(records: list) -> dict:
    reg_counts = defaultdict(int)
    func_writes = defaultdict(lambda: defaultdict(int))
    draws = 0
    barriers = 0

    for r in records:
        if r['type'] == 'PKT4':
            reg_counts[r['reg']] += 1
            func_writes[r['func']][r['reg']] += 1
        elif r['type'] == 'DRAW':
            draws += 1
        elif r['type'] == 'BARRIER':
            barriers += 1

    top_regs = sorted(reg_counts.items(), key=lambda x: -x[1])[:10]
    top_funcs = sorted(func_writes.items(), key=lambda x: -sum(x[1].values()))[:10]

    return {
        'total_pkts': len(records),
        'total_writes': sum(reg_counts.values()),
        'unique_regs': len(reg_counts),
        'draws': draws,
        'barriers': barriers,
        'barrier_per_draw': barriers / max(draws, 1),
        'top_registers': [
            {'reg': f'0x{r:04X}', 'writes': c} for r, c in top_regs
        ],
        'top_emitters': [
            {
                'func': f,
                'writes': sum(regs.values()),
                'unique_regs': len(regs),
                'top_reg': f'0x{max(regs, key=regs.get):04X}' if regs else 'N/A',
            }
            for f, regs in top_funcs
        ],
    }


def main():
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <trace.log> [output.json]")
        sys.exit(1)

    trace_path = sys.argv[1]
    out_path = sys.argv[2] if len(sys.argv) > 2 else f"{Path(trace_path).stem}_analysis.json"

    stats = parse_trace(trace_path)

    with open(out_path, 'w') as f:
        json.dump(stats, f, indent=2)

    print(f"  Total packets:   {stats['total_pkts']}")
    print(f"  Total writes:    {stats['total_writes']}")
    print(f"  Unique registers:{stats['unique_regs']}")
    print(f"  Draws:           {stats['draws']}")
    print(f"  Barriers:        {stats['barriers']}")
    print(f"  Barriers/draw:   {stats['barrier_per_draw']:.1f}")
    if stats['top_registers']:
        print(f"  Top register:    {stats['top_registers'][0]['reg']} ({stats['top_registers'][0]['writes']} writes)")
    if stats['top_emitters']:
        print(f"  Top emitter:     {stats['top_emitters'][0]['func']} ({stats['top_emitters'][0]['writes']} writes)")
    print(f"  Wrote {out_path}")


if __name__ == '__main__':
    main()
