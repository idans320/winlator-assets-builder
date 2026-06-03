# FEXCore JIT Optimization: Game-Heavy Tuning

## What We Did

### Pipeline Mapping

Reverse-engineered the x86→ARM64 JIT in FEX-Emu/FEX:

```
x86 opcode (Dispatcher.cpp)
  → OpcodeDispatcher (x86→IR, driven by IR.json: 419 ops, 14 categories)
    → Arm64JITCore::Op_XXX (DEF_OP macros, 605 handlers in JIT/*.cpp)
      → ARMEmitter::Emitter + .inl files (ARM64 encoding to byte buffer)
        → Executed via 3-level address cache (L1/L2/L3 in LookupCache.h)
```

Static analysis across 12,304 lines of JIT emission code:

- 605 handler implementations (VectorOps: 224, ALUOps: 149, MemoryOps: 90)
- Top ARM64 calls: `mov`(291), `ldr`(120), `fmov`(63), `add`(54), `str`(47)
- 57% of ARM64 instruction sequences are duplicates of another sequence
- 34 TSO barrier sites (ldar/stlr/dmb)
- LoadConstant called 69 times across 32 handlers (each = 3 ARM64 insns)
- SpillStaticRegs/FillStaticRegs cost ~132 insns per call transition

### Workload Model

Built a two-phase simulator (warmup + steady) with configurable reuse rates:

| Config | Block reuse (steady) | Warmup JIT | Pattern |
|---|---|---|---|
| game-heavy | 94% | 43ms (once) | 57% source-level dup |
| light-game | 98% | 11ms (once) | tight loop dominates |
| professional | 45% | 7ms (then keeps JITing) | no benefit |

## Why FEXCore's Current Cache Falls Short for Games

FEXCore has a 3-level **address-level** code cache: GuestToHostMap maps x86 address → ARM64 host code pointer. This works correctly but misses reuse at the IR-pattern level.

Two blocks at different x86 addresses producing the same IR shape (e.g., `ADD r/m32, imm32` at two call sites) get JIT'd twice, generating identical ARM64 instructions with different register assignments and different immediates. This is wasted emission work that repeats thousands of times across a game's code footprint.

A pattern cache that hashes **concrete values** (registers + immediates) will never hit — infinite state fragmentation. A pattern cache that hashes **IR shape** (op enum, data size, operand types), then patches host registers and immediates via bitwise operations, will hit predictably.

## The Four Mechanical Fixes

### 1. Structural Template Cache ("Template + Patch")

**Problem:** Hashed concrete registers/immediates → zero reuse at the host-instruction level. Hashed ARM64 mnemonic sequences (the current tool) → identifies duplication but can't be applied mechanically.

**Fix:** Hash the IR shape before register allocation and immediate embedding.

- **Hash key:** `(IROp_Enum, OpSize, SrcOpClass, DstOpClass)` — from IR.json definitions. No concrete registers, no immediate values.
- **Template payload:** Pre-encoded ARM64 machine-code bytes (max ~128B), plus two metadata arrays:
  - `patch_offsets_reg[]` — byte offsets in the template where host register fields sit (bits 0–4 for Rd, bits 5–9 for Rn, etc.)
  - `patch_offsets_imm[]` — byte offsets where immediate operands sit (bits 5–22 for ALU immediates, bits 5–23 for MOVZ/MOVK, etc.)

**On cache miss (warmup):** FEXCore compiles the IR normally. The emission tracker records the byte offset of every `Encode_rd()`, `Encode_rn()`, `Encode_rm()`, and immediate field write into the buffer. This metadata is stored alongside the pre-encoded bytes.

**On cache hit (steady state):**
```
memcpy(dst, template.bytes, template.size);
for each reg_off in template.patch_offsets_reg:
    *(uint32_t*)(dst + reg_off) |= (ra_reg_id << reg_shift_at_this_offset);
for each imm_off in template.patch_offsets_imm:
    *(uint32_t*)(dst + imm_off) |= (imm_value << imm_shift_at_this_offset);
```
Total cost: one `memcpy` + a handful of bitwise-ORs. No DEF_OP dispatch. No LoadConstant. No encoding logic. Nanoseconds per hit.

**Implementation surface:** ~200 LOC in FEXCore. Add a `struct PrecompiledPattern` array (256 entries), populate during first 120 frames gated by `FEX_PATTERN_CACHE=1`, check before the per-op switch in `CompileCode()`.

### 2. Automatic TSO Annihilation for Thread-Local Stack

**Problem:** FEXCore emits TSO barriers (`dmb`, `ldar`, `stlr`) on every x86 memory access to enforce x86 Total Store Ordering. These barriers cost pipeline stalls on ARM — a cost paid on every load and store, millions of times per frame. Manual overrides exist (`FEX_EXTENDEDVOLATILEMETADATA`) but no game uses them.

**Fix:** The x86 stack (`RSP`/`RBP`-based addressing) is inherently thread-local. No other thread reads another thread's stack during active execution. TSO ordering is unnecessary for stack accesses.

- **Detection:** During IR emission in the Memory op handlers, check if the base register of the memory operand is `RSP` or `RBP`.
- **Action:** Unconditionally replace `ldar`/`stlr`/`dmb` with plain `ldr`/`str` for those accesses.
- **Guard:** Do NOT strip TSO for: (a) global/heap accesses (base != RSP/RBP), (b) stores that escape via pointer aliasing into a non-stack region, (c) atomics.

**Expected yield:** The 34 TSO barrier sites found statically are an undercount — at runtime, every memory op in a function body that uses stack locals triggers this. A game executing 800K x86 ops/s with ~25% memory ops means ~200K TSO barriers per second, each costing 5–20 cycles on Adreno 830. Stripping stack-based barriers alone saves 1–4M cycles/s.

### 3. Dumb Spill/Fill Hoisting for Self-Loops

**Problem:** SpillStaticRegs/FillStaticRegs costs ~132 ARM64 insns per call transition. Full CFG liveness analysis is too expensive to run in the JIT. But games contain tight loops (physics, rendering, input polling) that jump backward to the same block head thousands of times per frame, paying the spill/fill cost on every iteration when a helper call sits inside the loop.

**Fix:** Peephole-level hoisting. No liveness analysis.

- **Trigger:** x86 decoder hits a backward branch whose target is the start of the current block (self-loop detection).
- **Action:** Move `FillStaticRegs` above the loop entry point (emit once, not per iteration). Move `SpillStaticRegs` to exit paths (after the backward branch, before any forward exit).
- **Guard:** If the block contains: (a) a side-exit not through the backward branch, (b) a helper call that might yield the thread (syscall, thunk, exit function), (c) more than one back-edge target — disable hoisting for that block.

**Expected yield:** For a tight 10-instruction loop containing a helper call, the spill/fill overhead is 132/(10+132) = 93% of the loop body. Hoisting cuts this to zero per iteration. On a game running 50K loop iterations/s, savings are substantial.

### 4. Constant Pool De-duplication

**Problem:** `LoadConstant(uint64_t)` emits `movz`+`movk`+`movk` (3 insns, 12 bytes) every time a 64-bit immediate appears in an IR op. Same constants (float bounds, bitmasks, address offsets) repeat across blocks and within blocks. Our analysis found 69 LoadConstant call sites across 32 handlers.

**Fix:** Per-block constant pool at the end of the JIT buffer.

- Allocate a small data island appended to each compiled block's code buffer.
- When a block needs a constant: hash the 64-bit value, check the island. If present, emit a single PC-relative `ldr` (1 insn, 4 bytes). If absent, emit `movz`+`movk`+`movk` AND append the constant to the island.
- 3 insns → 1 insn. 12 bytes → 4 bytes. I-cache pressure drops proportionally.

**Expected yield:** 69 LoadConstant calls × 2 insns saved = 138 fewer emitted insns statically. At runtime, with constant reuse across 94% block reuse in gaming, the dynamic saving is much larger.

## Mechanical Path Forward

### Step 1: Add IR Shape Hash to Analyzer

The current `fex-pattern-cache` tool hashes ARM64 mnemonic sequences from source files — useful for identifying duplication, but not directly implementable. We need to extend it to hash IR shapes from IR.json:

- Parse each op's definition: `"GPR = Add GPR:$Src1, GPR:$Src2"`
- Extract shape: `(Add, i64Bit_or_i32Bit, GPR:GPR:GPR)` → hash to family ID
- Count how many ops share the same shape family
- This gives us a real hit-rate estimate for the structural cache before writing any FEXCore patches

### Step 2: Add RSP/RBP Detection to Bottleneck Analyzer

Extend `fex-bottleneck` to scan MemoryOps.cpp and classify each TSO site by addressing mode:
- `HandleLoadMemTSO` with base = stack register → strippable
- `HandleLoadMemTSO` with base = general register → must keep

This gives us a concrete count of "removable TSO barriers" before touching FEXCore.

### Step 3: Add Self-Loop Detector

Scan the x86 decode tables (OpcodeDispatcher/*Tables.h) and BranchOps.cpp for backward-branch patterns. Classify which branch targets point to the start of the current block vs external targets. Estimate how many spill/fill sites sit inside these loop contexts.

### Step 4: Implement in FEXCore (~400 LOC total)

All four fixes require small, localized changes:

- **Pattern cache:** New file `JIT/PatternCache.h` + hook in `CompileCode()` + emission tracker in `Arm64Emitter::Emitter`
- **TSO strip:** 3-line check in `MemoryOps.cpp` load/store handlers: `if (BaseReg == RSP || BaseReg == RBP) { emit ldr/str; return; }`
- **Spill hoist:** Peephole pass in `CompileCode()` after IR emission, before buffer finalization
- **Constant pool:** New member `fextl::robin_map<uint64_t, ptrdiff_t>` on `Arm64JITCore`, checked in `LoadConstant()`

### Step 5: Test on Adreno 830 (Snapdragon 8 Elite)

Phone accessible at `adb -s 10.0.0.10:42617`. Build FEXCore with the four patches, push `.wcp` to `/sdcard/`, install in Winlator. Test games: Torchlight (D3D9), Skyrim (D3D11), Warcraft 3 (D3D9).

Measure:
- JIT compile time per frame (FEXCore telemetry)
- Frame time delta vs stock FEXCore
- TSO barrier count before/after (perf counter)
- Pattern cache hit rate (telemetry added in Step 4)

### Files

```
research/fex/
├── cmd/fex-analyzer/       # Static IR+ARM64 emission map (runnable)
├── cmd/fex-bottleneck/     # Per-op insn estimate + TSO/spill analysis (runnable)
├── cmd/fex-pattern-cache/  # ARM64 sequence dedup + coverage chart (runnable)
├── cmd/fex-workload/       # Two-phase game vs pro simulator (runnable)
├── idea.md                 # This document
├── analysis.json
├── pattern-cache.json
└── gaming-optimizer.json

research/internal/fex/
├── patterns.go             # ARM64 instruction classification, hot paths, emission regex
├── ir.go                   # IR.json parser (419 ops → categories)
├── jit.go                  # JIT source scanner (DEF_OP detection, emission counting)
└── tuning.go               # Game vs pro configs, template cache model
```

All tools: `cd research && go run ./fex/cmd/<tool>/`
