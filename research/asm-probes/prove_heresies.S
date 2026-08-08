// prove_heresies.S — AArch64 assembly microbenchmarks for Oryon SIMD heresies
//
// Each heresy has a _vanilla (compiler-emitted) and _heresy (injected) variant.
// Every function takes x0 = iteration count, does 4 units of work per iteration,
// and returns nothing. The C harness times both variants and reports the delta.
//
// Build: see build.sh

.arch armv8-a

// ——— macros ———

.macro INT_RCP out_f, in_s
    fmov w13, \in_s
    and  w14, w13, #0x7FFFFFFF
    and  w15, w14, #0x807FFFFF
    ubfx w16, w14, #23, #8
    mov  w17, #253
    sub  w16, w17, w16
    lsl  w16, w16, #23
    orr  w13, w15, w16
    fmov \out_f, w13
.endm

.macro INT_APPROX_MUL out_f, a_s, b_s
    fmov w13, \a_s
    fmov w14, \b_s
    eor  w15, w13, w14
    and  w15, w15, #0x80000000
    bic  w16, w13, #0x80000000
    bic  w17, w14, #0x80000000
    umull x18, w16, w17
    lsr  x18, x18, #23
    mov  w13, w18
    orr  w13, w13, w15
    fmov \out_f, w13
.endm

// ——————————————————————————————————————————————
// Heresy C: Integer reciprocal — fdiv vs bit manipulation
// Turnip: freedreno_guardband.h — guardband reciprocal
// Claim:   integer exponent remap (~3-4 cycles) beats fdiv (~10 cycles)
// ——————————————————————————————————————————————

.section .rodata
.align 4
c_one:       .float 1.0
c_inputs:    .float 3.14159, 2.71828, 1.41421, 0.707106

.text
.align 4
.global heresy_c_vanilla
.type heresy_c_vanilla, %function
heresy_c_vanilla:
    adrp x1, c_one
    add  x1, x1, :lo12:c_one
    ldr  s0, [x1]
    add  x1, x1, #4
1:
    ldp  s1, s2, [x1]
    fdiv s3, s0, s1
    fdiv s4, s0, s2
    ldp  s1, s2, [x1, #8]
    fdiv s5, s0, s1
    fdiv s6, s0, s2
    subs x0, x0, #1
    b.ne 1b
    ret
.size heresy_c_vanilla, .-heresy_c_vanilla

.align 4
.global heresy_c_heresy
.type heresy_c_heresy, %function
heresy_c_heresy:
    adrp x1, c_inputs
    add  x1, x1, :lo12:c_inputs
1:
    ldp  s1, s2, [x1]
    INT_RCP s7, s1
    INT_RCP s7, s2
    ldp  s1, s2, [x1, #8]
    INT_RCP s7, s1
    INT_RCP s7, s2
    subs x0, x0, #1
    b.ne 1b
    ret
.size heresy_c_heresy, .-heresy_c_heresy

// ——————————————————————————————————————————————
// Heresy N: UMULL — integer multiply vs fmul on float bit patterns
// Turnip: freedreno_guardband.h — gb_max_ndc = (diff) * rcp_scale
// Claim:   umull avoids FPU pipeline, runs in integer ALU
// ——————————————————————————————————————————————

.section .rodata
.align 4
n_vals_a:    .float 10.0, 3.0, 0.5, 100.0
n_vals_b:    .float 0.5, 2.0, 0.01, 0.2

.text
.align 4
.global heresy_n_vanilla
.type heresy_n_vanilla, %function
heresy_n_vanilla:
    adrp x1, n_vals_a
    add  x1, x1, :lo12:n_vals_a
    adrp x2, n_vals_b
    add  x2, x2, :lo12:n_vals_b
1:
    ldp  s0, s1, [x1]
    ldp  s4, s5, [x2]
    fmul s2, s0, s4
    fmul s3, s1, s5
    ldp  s0, s1, [x1, #8]
    ldp  s4, s5, [x2, #8]
    fmul s6, s0, s4
    fmul s7, s1, s5
    subs x0, x0, #1
    b.ne 1b
    ret
.size heresy_n_vanilla, .-heresy_n_vanilla

.align 4
.global heresy_n_heresy
.type heresy_n_heresy, %function
heresy_n_heresy:
    adrp x1, n_vals_a
    add  x1, x1, :lo12:n_vals_a
    adrp x2, n_vals_b
    add  x2, x2, :lo12:n_vals_b
1:
    ldp  s0, s1, [x1]
    ldp  s4, s5, [x2]
    INT_APPROX_MUL s7, s0, s4
    INT_APPROX_MUL s7, s1, s5
    ldp  s0, s1, [x1, #8]
    ldp  s4, s5, [x2, #8]
    INT_APPROX_MUL s7, s0, s4
    INT_APPROX_MUL s7, s1, s5
    subs x0, x0, #1
    b.ne 1b
    ret
.size heresy_n_heresy, .-heresy_n_heresy

// ——————————————————————————————————————————————
// Heresy A: rbit+clz — branchless sparse-bit dispatch vs 14-way if chain
// Turnip: tu_cmd_buffer.cc — TU_CMD_FLAG cache-flush dispatch
// Claim:   rbit+clz+switch avoids 14 branch-mispredictable tbnz chains
// ——————————————————————————————————————————————

.section .rodata
.align 3
a_masks:
    .word 0x0001, 0x0004, 0x0040, 0x0100
    .word 0x0002, 0x0200, 0x0008, 0x0401
.equ A_MASK_COUNT, 8

.section .data
a_handler_ct:
    .word 0

.text
.align 4
.global heresy_a_vanilla
.type heresy_a_vanilla, %function
heresy_a_vanilla:
    // Test 14 bit positions (0-13) per mask with tbnz.
    // Each taken branch increments a counter to simulate the body.
    // Masks rotate per iteration to defeat branch-predictor overfitting.
    adrp x7, a_masks
    add  x7, x7, :lo12:a_masks
    mov  x8, #0            // mask index
    mov  x9, #A_MASK_COUNT
1:
    // Load next mask
    ldr  w1, [x7, x8, lsl #2]
    add  x8, x8, #1
    cmp  x8, x9
    csel x8, xzr, x8, eq   // wrap around

    mov  x3, xzr            // handler counter
    tbnz w1, #0,  10f; b 11f; 10: add x3, x3, #1; 11:
    tbnz w1, #1,  12f; b 13f; 12: add x3, x3, #1; 13:
    tbnz w1, #2,  14f; b 15f; 14: add x3, x3, #1; 15:
    tbnz w1, #3,  16f; b 17f; 16: add x3, x3, #1; 17:
    tbnz w1, #4,  18f; b 19f; 18: add x3, x3, #1; 19:
    tbnz w1, #5,  20f; b 21f; 20: add x3, x3, #1; 21:
    tbnz w1, #6,  22f; b 23f; 22: add x3, x3, #1; 23:
    tbnz w1, #7,  24f; b 25f; 24: add x3, x3, #1; 25:
    tbnz w1, #8,  26f; b 27f; 26: add x3, x3, #1; 27:
    tbnz w1, #9,  28f; b 29f; 28: add x3, x3, #1; 29:
    tbnz w1, #10, 30f; b 31f; 30: add x3, x3, #1; 31:
    tbnz w1, #11, 32f; b 33f; 32: add x3, x3, #1; 33:
    tbnz w1, #12, 34f; b 35f; 34: add x3, x3, #1; 35:
    tbnz w1, #13, 36f; b 37f; 36: add x3, x3, #1; 37:

    subs x0, x0, #1
    b.ne 1b

    adrp x2, a_handler_ct
    add  x2, x2, :lo12:a_handler_ct
    str  w3, [x2]
    ret
.size heresy_a_vanilla, .-heresy_a_vanilla

.align 4
.global heresy_a_heresy
.type heresy_a_heresy, %function
heresy_a_heresy:
    // For each mask: while bits remain, rbit+clz to find next set bit,
    // clear it, and invoke the "handler."
    adrp x7, a_masks
    add  x7, x7, :lo12:a_masks
    mov  x8, #0
    mov  x9, #A_MASK_COUNT
1:
    ldr  w1, [x7, x8, lsl #2]
    add  x8, x8, #1
    cmp  x8, x9
    csel x8, xzr, x8, eq

    mov  x3, xzr
    cbz  w1, 2f
3:
    rbit w2, w1
    clz  w2, w2
    mov  w4, #1
    lsl  w4, w4, w2
    bic  w1, w1, w4
    add  x3, x3, #1
    cbnz w1, 3b
2:
    subs x0, x0, #1
    b.ne 1b

    adrp x2, a_handler_ct
    add  x2, x2, :lo12:a_handler_ct
    str  w3, [x2]
    ret
.size heresy_a_heresy, .-heresy_a_heresy

// ——————————————————————————————————————————————
// Heresy DJ: 4-wide horizontal ILP — batched loads/adds/stores
// Turnip: tu_cmd_buffer.cc — UBO VA patching in BindDescriptorSets
// Claim:   grouping loads → adds → stores allows Oryon's 8-wide decode
//          to dispatch all independent ops in parallel
// ——————————————————————————————————————————————

.section .data
.align 3
dj_offsets:
    .word 0x100, 0x200, 0x300, 0x400
    .word 0x500, 0x600, 0x700, 0x800
dj_src:
    .xword 0x7d4000100, 0x7d4000200, 0x7d4000300, 0x7d4000400
    .xword 0x7d4000500, 0x7d4000600, 0x7d4000700, 0x7d4000800
.balign 64
dj_dst:
    .space 256

.text
.align 4
.global heresy_dj_vanilla
.type heresy_dj_vanilla, %function
heresy_dj_vanilla:
    // Sequential: each iteration (load offset, load VA, add, store) has a
    // dependency chain that serializes execution. Next load can't dispatch
    // until previous store's address is resolved.
    adrp x1, dj_offsets
    add  x1, x1, :lo12:dj_offsets
    adrp x2, dj_src
    add  x2, x2, :lo12:dj_src
    adrp x3, dj_dst
    add  x3, x3, :lo12:dj_dst
1:
    ldr  w4, [x1]             // offset 0
    ldr  x5, [x2]             // VA 0
    add  x5, x5, x4
    str  x5, [x3]
    ldr  w4, [x1, #4]         // offset 1
    ldr  x5, [x2, #8]         // VA 1
    add  x5, x5, x4
    str  x5, [x3, #8]
    ldr  w4, [x1, #8]         // offset 2
    ldr  x5, [x2, #16]        // VA 2
    add  x5, x5, x4
    str  x5, [x3, #16]
    ldr  w4, [x1, #12]        // offset 3
    ldr  x5, [x2, #24]        // VA 3
    add  x5, x5, x4
    str  x5, [x3, #24]
    subs x0, x0, #1
    b.ne 1b
    ret
.size heresy_dj_vanilla, .-heresy_dj_vanilla

.align 4
.global heresy_dj_heresy
.type heresy_dj_heresy, %function
heresy_dj_heresy:
    // 4-wide ILP: load all offsets → load all VAs → add all → store all
    // No cross-iteration dependencies — OoO engine can issue all in parallel.
    adrp x1, dj_offsets
    add  x1, x1, :lo12:dj_offsets
    adrp x2, dj_src
    add  x2, x2, :lo12:dj_src
    adrp x3, dj_dst
    add  x3, x3, :lo12:dj_dst
1:
    ldp  w4, w5, [x1]        // offsets 0,1
    ldp  w6, w7, [x1, #8]    // offsets 2,3
    ldp  x8, x9, [x2]        // VAs 0,1
    ldp  x10, x11, [x2, #16] // VAs 2,3
    add  x8, x8, x4
    add  x9, x9, x5
    add  x10, x10, x6
    add  x11, x11, x7
    stp  x8, x9, [x3]
    stp  x10, x11, [x3, #16]
    subs x0, x0, #1
    b.ne 1b
    ret
.size heresy_dj_heresy, .-heresy_dj_heresy

// ——————————————————————————————————————————————
// Heresy K: DC ZVA — cache-line zero vs STP store loop
// Turnip: tu_descriptor_set.cc — descriptor set memset
// Claim:   dc zva zeros in L2 without DRAM fetch, beats STP store pairs
// ——————————————————————————————————————————————

.section .bss
.balign 64
k_buf:
    .space 4096

.text
.align 4
.global heresy_k_vanilla
.type heresy_k_vanilla, %function
heresy_k_vanilla:
    // Zero 128 bytes using 8 stp pairs, rotating through 4KB buffer
    // to defeat L1-cache-hit bias (simulates streaming memset)
    adrp x1, k_buf
    add  x1, x1, :lo12:k_buf
    mov  x10, #0                // buffer offset
    mov  x11, #3968             // 4096 - 128
1:
    add  x2, x1, x10
    stp  xzr, xzr, [x2]
    stp  xzr, xzr, [x2, #16]
    stp  xzr, xzr, [x2, #32]
    stp  xzr, xzr, [x2, #48]
    add  x2, x2, #64
    stp  xzr, xzr, [x2]
    stp  xzr, xzr, [x2, #16]
    stp  xzr, xzr, [x2, #32]
    stp  xzr, xzr, [x2, #48]
    add  x10, x10, #128
    cmp  x10, x11
    csel x10, xzr, x10, gt
    subs x0, x0, #1
    b.ne 1b
    ret
.size heresy_k_vanilla, .-heresy_k_vanilla

.align 4
.global heresy_k_heresy
.type heresy_k_heresy, %function
heresy_k_heresy:
    // Zero 128 bytes using 2 × dc zva, rotating through 4KB buffer
    adrp x1, k_buf
    add  x1, x1, :lo12:k_buf
    mov  x10, #0
    mov  x11, #3968
1:
    add  x2, x1, x10
    dc zva, x2
    add  x2, x2, #64
    dc zva, x2
    add  x10, x10, #128
    cmp  x10, x11
    csel x10, xzr, x10, gt
    subs x0, x0, #1
    b.ne 1b
    ret
.size heresy_k_heresy, .-heresy_k_heresy

// ——————————————————————————————————————————————
// Heresy E: Branchless bitset — single cbnz vs per-bit tbnz chain
// Turnip: tu_cmd_buffer.cc — dynamic state dirty-bitset check
// Claim:   one word-test beats N individual bit-tests for common case
// ——————————————————————————————————————————————

.section .rodata
.align 2
e_masks:
    .word 0x0000, 0x0004, 0x0000, 0x0020, 0x0000, 0x0000, 0x0008, 0x0000
.equ E_MASK_COUNT, 8

.text
.align 4
.global heresy_e_vanilla
.type heresy_e_vanilla, %function
heresy_e_vanilla:
    // 3 individual tbnz checks (bits 2, 3, 5 ≈ PRIMITIVE_RESTART, PROVOKING_VERTEX, etc)
    adrp x6, e_masks
    add  x6, x6, :lo12:e_masks
    mov  x7, #0
    mov  x9, #E_MASK_COUNT
1:
    ldr  w1, [x6, x7, lsl #2]
    add  x7, x7, #1
    cmp  x7, x9
    csel x7, xzr, x7, eq

    tbnz w1, #2, 10f; b 11f; 10: mov w8, #1; b 12f; 11: mov w8, #0; 12:
    tbnz w1, #3, 13f; b 14f; 13: mov w8, #1; b 15f; 14: mov w8, #0; 15:
    tbnz w1, #5, 16f; b 17f; 16: mov w8, #1; b 18f; 17: mov w8, #0; 18:
    subs x0, x0, #1
    b.ne 1b
    ret
.size heresy_e_vanilla, .-heresy_e_vanilla

.align 4
.global heresy_e_heresy
.type heresy_e_heresy, %function
heresy_e_heresy:
    // Single cbnz on the whole word; individual tests move inside the guard
    adrp x6, e_masks
    add  x6, x6, :lo12:e_masks
    mov  x7, #0
    mov  x9, #E_MASK_COUNT
1:
    ldr  w1, [x6, x7, lsl #2]
    add  x7, x7, #1
    cmp  x7, x9
    csel x7, xzr, x7, eq

    cbz  w1, 2f
    tbnz w1, #2, 10f; b 11f; 10: mov w8, #1; b 12f; 11: mov w8, #0; 12:
    tbnz w1, #3, 13f; b 14f; 13: mov w8, #1; b 15f; 14: mov w8, #0; 15:
    tbnz w1, #5, 16f; b 17f; 16: mov w8, #1; b 18f; 17: mov w8, #0; 18:
2:
    subs x0, x0, #1
    b.ne 1b
    ret
.size heresy_e_heresy, .-heresy_e_heresy
