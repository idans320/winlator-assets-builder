# Turnip gen8 (Adreno 8xx) Performance Research

Tools and analysis for optimizing the Mesa Turnip Vulkan driver on Adreno 8xx GPUs.

## Documentation

- **[The Mapper](docs/mapper.md)** — How static analysis traces from C++ source to PKT4/PKT7 data flow paths (3-layer architecture: function discovery → gen8 site detection → neuron path tracing)
- **[LD_PRELOAD Status](docs/ld-preload.md)** — Running the real Turnip driver with the GPU I/O stub (what worked, what didn't, and what's needed to get the full chain working)

## Quick Start

```bash
# Enter devbox environment
devbox shell

# Full analysis pipeline → diagrams + reports
./run-analysis.sh

# Or individual steps:
go run ./analysis/cmd/ast-analyzer --mesa ../mesa/workdir/mesa --out output/analysis.json
go run ./analysis/cmd/graph-gen --input output/analysis.json --outdir output/
go run ./analysis/cmd/analyze-bottleneck --input output/analysis.json
```

## Directory Structure

```
research/
├── analysis/          # Source-level AST neuron tracing & bottleneck discovery
│   ├── cmd/
│   │   ├── ast-analyzer/        # C++ source → call graph + gen8 code sites
│   │   ├── graph-gen/          # DOT/SVG data flow diagram generation
│   │   └── analyze-bottleneck/ # FPS optimization ranking report
│   └── internal/               # (see internal/ below)
│
├── pm4/                # PM4 command stream toolkit
│   ├── cmd/reg-gen/           # Register XML → Go code generator
│   └── internal/              # (see internal/ below)
│
├── gpu-stub/           # Fake GPU I/O engine & fuzzing
│   ├── cmd/
│   │   ├── gpu-stub/           # Unix socket RPC server (fake GPU)
│   │   ├── gpu-fuzz/           # PM4 mutation fuzzer
│   │   ├── stim-test/          # Workload stimulation & optimization measurement
│   │   └── bench-dirty/        # Dirty-shadow vs baseline benchmark
│   └── internal/              # (see internal/ below)
│
├── internal/           # Shared packages
│   ├── ast/                    # C++ tokenizer, function parser, neuron tracer
│   ├── graph/                  # Graphviz DOT file generator
│   ├── mesa/                   # Turnip source file discovery
│   ├── pm4/                    # PM4 opcodes, register map (generated), decoder
│   └── stub/                   # GPU engine, memory tracker, experiments, RPC
│
├── c-stub/             # C LD_PRELOAD ioctl interceptor
│   ├── stub_gpu_client.c       # Intercepts KGSL ioctls → Unix socket → Go engine
│   ├── build-stub.sh           # Cross-compile for aarch64 (NDK)
│   └── fuzz_vulkan_test.c      # Direct driver fuzz harness
│
├── patches/            # Mesa optimization patches
│   ├── 0003-tu-cs-reg-cache.patch     # Two-tier dirty-bit shadow register system
│   └── 0004-barrier-coalesce.patch    # Barrier coalescing (opt-in)
│
├── output/             # Generated artifacts
│   ├── analysis.json             # Full neuron call graph (20MB)
│   ├── bottleneck_report.json    # Ranked FPS optimization targets
│   ├── fuzz_*.json              # Fuzzer results
│   ├── *.dot / *.svg            # Data flow diagrams
│   └── freedreno_icd.x86_64.json
│
├── devbox.json         # Hermetic Go + Graphviz + clang environment
├── go.mod
└── run-analysis.sh     # One-shot analysis pipeline
```

## Tools Reference

### Analysis Pipeline

| Tool | Command | Purpose |
|------|---------|---------|
| ast-analyzer | `go run ./analysis/cmd/ast-analyzer --mesa <path>` | Parse Turnip C++ → call graph + 3550 gen8 code sites |
| graph-gen | `go run ./analysis/cmd/graph-gen --input output/analysis.json` | Generate 3 Graphviz diagrams (arch, neurons, call graph) |
| analyze-bottleneck | `go run ./analysis/cmd/analyze-bottleneck` | Ranked FPS optimization targets with impact estimates |

### PM4 Toolkit

| Tool | Command | Purpose |
|------|---------|---------|
| reg-gen | `go run ./pm4/cmd/reg-gen --mesa <path>` | Parse Mesa XML → generate 3666 register + 141 opcode Go map |
| Decoder (lib) | `import "research/internal/pm4"` | Parse PM4 dword streams into packets (PKT4/PKT7/IB) |

### GPU Stub & Fuzzing

| Tool | Command | Purpose |
|------|---------|---------|
| gpu-stub | `go run ./gpu-stub/cmd/gpu-stub` | Fake GPU RPC server on Unix socket |
| gpu-fuzz | `go run ./gpu-stub/cmd/gpu-fuzz` | Mutate PM4 streams, detect crashes and anomalies |
| stim-test | `go run ./gpu-stub/cmd/stim-test` | Generate realistic workloads, measure optimization gains |
| bench-dirty | `go run ./gpu-stub/cmd/bench-dirty` | Dirty-shadow vs baseline PKT4 dword count benchmark |

## Findings

### Fuzz Campaign (207 cases, 3 seeds)

| Finding | Severity | Cases |
|---------|----------|-------|
| Unknown PM4 opcodes (0x01, 0x66) | CRITICAL | 2 |
| Barrier-per-draw (2:1 ratio) | WARNING | 208 |
| Redundant register writes (60-84/case) | ANOMALY | 189 |

### Optimization Impact (Stimulation Tested)

| Optimization | Light (100d) | Heavy (1000d) | Blit (500b) | Barrier | State |
|-------------|-------------|--------------|------------|---------|-------|
| Redundant write filter | 11.4% | 11.4% | 33.5% | 11.0% | 11.0% |
| Barrier coalescing | 130 skip | 1600 skip | 230 skip | 1250 skip | 330 skip |
| **Combined** | **~15%** | **~15%** | **~33%** | **~17%** | **~14%** |

## C Stub Integration

```bash
# Build stub for aarch64
./c-stub/build-stub.sh

# Run Turnip driver with fake GPU
export TU_STUB_GPU=1
export TU_STUB_SOCKET=/tmp/tu_stub_gpu.sock
export LD_PRELOAD=./output/libstub_gpu_client.so
./vulkan.turnip.so
```

## Hack Flags (patched driver)

| Flag | Effect |
|------|--------|
| `TU_YOLO_SYNC=1` | Strip per-draw barriers, mega-flush at EndRenderPass |
| `TU_SKIP_STATE=N` | Flush dirty registers only every N draws |
| `TU_REG_BLAST=1` | Dump 64-register blocks (skip ffs scan, brute-force) |
| `TU_BARRIER_COALESCE=1` | Skip flush emission when no bits accumulated |
