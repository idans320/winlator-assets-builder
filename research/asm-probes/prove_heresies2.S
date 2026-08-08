// prove_heresies2.S — Oryon uarch probes: RSB depth, StoreQ, L1D
.arch armv8-a

// ——— Heresy Q: Return Stack Buffer depth ———
// 60 nested call levels. Each level: stp fp,lr ; bl next ; ldp fp,lr ; ret

.text
.align 4

.global q_call_chain
.type q_call_chain, %function
q_call_chain:
    adr  x1, q_entries
    sub  x0, x0, #1
    ldr  x1, [x1, x0, lsl #3]
    isb
    mrs  x9, cntvct_el0
    br   x1
q_stop:
    isb
    mrs  x10, cntvct_el0
    sub  x0, x10, x9
    ret

// 60 call levels, hand-expanded (LLVM as doesn't do macro arithmetic)

q_level_1:   stp x29,x30,[sp,#-16]!; bl q_level_2;   ldp x29,x30,[sp],#16; ret
q_level_2:   stp x29,x30,[sp,#-16]!; bl q_level_3;   ldp x29,x30,[sp],#16; ret
q_level_3:   stp x29,x30,[sp,#-16]!; bl q_level_4;   ldp x29,x30,[sp],#16; ret
q_level_4:   stp x29,x30,[sp,#-16]!; bl q_level_5;   ldp x29,x30,[sp],#16; ret
q_level_5:   stp x29,x30,[sp,#-16]!; bl q_level_6;   ldp x29,x30,[sp],#16; ret
q_level_6:   stp x29,x30,[sp,#-16]!; bl q_level_7;   ldp x29,x30,[sp],#16; ret
q_level_7:   stp x29,x30,[sp,#-16]!; bl q_level_8;   ldp x29,x30,[sp],#16; ret
q_level_8:   stp x29,x30,[sp,#-16]!; bl q_level_9;   ldp x29,x30,[sp],#16; ret
q_level_9:   stp x29,x30,[sp,#-16]!; bl q_level_10;  ldp x29,x30,[sp],#16; ret
q_level_10:  stp x29,x30,[sp,#-16]!; bl q_level_11;  ldp x29,x30,[sp],#16; ret
q_level_11:  stp x29,x30,[sp,#-16]!; bl q_level_12;  ldp x29,x30,[sp],#16; ret
q_level_12:  stp x29,x30,[sp,#-16]!; bl q_level_13;  ldp x29,x30,[sp],#16; ret
q_level_13:  stp x29,x30,[sp,#-16]!; bl q_level_14;  ldp x29,x30,[sp],#16; ret
q_level_14:  stp x29,x30,[sp,#-16]!; bl q_level_15;  ldp x29,x30,[sp],#16; ret
q_level_15:  stp x29,x30,[sp,#-16]!; bl q_level_16;  ldp x29,x30,[sp],#16; ret
q_level_16:  stp x29,x30,[sp,#-16]!; bl q_level_17;  ldp x29,x30,[sp],#16; ret
q_level_17:  stp x29,x30,[sp,#-16]!; bl q_level_18;  ldp x29,x30,[sp],#16; ret
q_level_18:  stp x29,x30,[sp,#-16]!; bl q_level_19;  ldp x29,x30,[sp],#16; ret
q_level_19:  stp x29,x30,[sp,#-16]!; bl q_level_20;  ldp x29,x30,[sp],#16; ret
q_level_20:  stp x29,x30,[sp,#-16]!; bl q_level_21;  ldp x29,x30,[sp],#16; ret
q_level_21:  stp x29,x30,[sp,#-16]!; bl q_level_22;  ldp x29,x30,[sp],#16; ret
q_level_22:  stp x29,x30,[sp,#-16]!; bl q_level_23;  ldp x29,x30,[sp],#16; ret
q_level_23:  stp x29,x30,[sp,#-16]!; bl q_level_24;  ldp x29,x30,[sp],#16; ret
q_level_24:  stp x29,x30,[sp,#-16]!; bl q_level_25;  ldp x29,x30,[sp],#16; ret
q_level_25:  stp x29,x30,[sp,#-16]!; bl q_level_26;  ldp x29,x30,[sp],#16; ret
q_level_26:  stp x29,x30,[sp,#-16]!; bl q_level_27;  ldp x29,x30,[sp],#16; ret
q_level_27:  stp x29,x30,[sp,#-16]!; bl q_level_28;  ldp x29,x30,[sp],#16; ret
q_level_28:  stp x29,x30,[sp,#-16]!; bl q_level_29;  ldp x29,x30,[sp],#16; ret
q_level_29:  stp x29,x30,[sp,#-16]!; bl q_level_30;  ldp x29,x30,[sp],#16; ret
q_level_30:  stp x29,x30,[sp,#-16]!; bl q_level_31;  ldp x29,x30,[sp],#16; ret
q_level_31:  stp x29,x30,[sp,#-16]!; bl q_level_32;  ldp x29,x30,[sp],#16; ret
q_level_32:  stp x29,x30,[sp,#-16]!; bl q_level_33;  ldp x29,x30,[sp],#16; ret
q_level_33:  stp x29,x30,[sp,#-16]!; bl q_level_34;  ldp x29,x30,[sp],#16; ret
q_level_34:  stp x29,x30,[sp,#-16]!; bl q_level_35;  ldp x29,x30,[sp],#16; ret
q_level_35:  stp x29,x30,[sp,#-16]!; bl q_level_36;  ldp x29,x30,[sp],#16; ret
q_level_36:  stp x29,x30,[sp,#-16]!; bl q_level_37;  ldp x29,x30,[sp],#16; ret
q_level_37:  stp x29,x30,[sp,#-16]!; bl q_level_38;  ldp x29,x30,[sp],#16; ret
q_level_38:  stp x29,x30,[sp,#-16]!; bl q_level_39;  ldp x29,x30,[sp],#16; ret
q_level_39:  stp x29,x30,[sp,#-16]!; bl q_level_40;  ldp x29,x30,[sp],#16; ret
q_level_40:  stp x29,x30,[sp,#-16]!; bl q_level_41;  ldp x29,x30,[sp],#16; ret
q_level_41:  stp x29,x30,[sp,#-16]!; bl q_level_42;  ldp x29,x30,[sp],#16; ret
q_level_42:  stp x29,x30,[sp,#-16]!; bl q_level_43;  ldp x29,x30,[sp],#16; ret
q_level_43:  stp x29,x30,[sp,#-16]!; bl q_level_44;  ldp x29,x30,[sp],#16; ret
q_level_44:  stp x29,x30,[sp,#-16]!; bl q_level_45;  ldp x29,x30,[sp],#16; ret
q_level_45:  stp x29,x30,[sp,#-16]!; bl q_level_46;  ldp x29,x30,[sp],#16; ret
q_level_46:  stp x29,x30,[sp,#-16]!; bl q_level_47;  ldp x29,x30,[sp],#16; ret
q_level_47:  stp x29,x30,[sp,#-16]!; bl q_level_48;  ldp x29,x30,[sp],#16; ret
q_level_48:  stp x29,x30,[sp,#-16]!; bl q_level_49;  ldp x29,x30,[sp],#16; ret
q_level_49:  stp x29,x30,[sp,#-16]!; bl q_level_50;  ldp x29,x30,[sp],#16; ret
q_level_50:  stp x29,x30,[sp,#-16]!; bl q_level_51;  ldp x29,x30,[sp],#16; ret
q_level_51:  stp x29,x30,[sp,#-16]!; bl q_level_52;  ldp x29,x30,[sp],#16; ret
q_level_52:  stp x29,x30,[sp,#-16]!; bl q_level_53;  ldp x29,x30,[sp],#16; ret
q_level_53:  stp x29,x30,[sp,#-16]!; bl q_level_54;  ldp x29,x30,[sp],#16; ret
q_level_54:  stp x29,x30,[sp,#-16]!; bl q_level_55;  ldp x29,x30,[sp],#16; ret
q_level_55:  stp x29,x30,[sp,#-16]!; bl q_level_56;  ldp x29,x30,[sp],#16; ret
q_level_56:  stp x29,x30,[sp,#-16]!; bl q_level_57;  ldp x29,x30,[sp],#16; ret
q_level_57:  stp x29,x30,[sp,#-16]!; bl q_level_58;  ldp x29,x30,[sp],#16; ret
q_level_58:  stp x29,x30,[sp,#-16]!; bl q_level_59;  ldp x29,x30,[sp],#16; ret
q_level_59:  stp x29,x30,[sp,#-16]!; bl q_level_60;  ldp x29,x30,[sp],#16; ret
q_level_60:  stp x29,x30,[sp,#-16]!;            ldp x29,x30,[sp],#16; ret

.section .rodata
.align 3
q_entries:
    .xword q_level_1,  q_level_2,  q_level_3,  q_level_4
    .xword q_level_5,  q_level_6,  q_level_7,  q_level_8
    .xword q_level_9,  q_level_10, q_level_11, q_level_12
    .xword q_level_13, q_level_14, q_level_15, q_level_16
    .xword q_level_17, q_level_18, q_level_19, q_level_20
    .xword q_level_21, q_level_22, q_level_23, q_level_24
    .xword q_level_25, q_level_26, q_level_27, q_level_28
    .xword q_level_29, q_level_30, q_level_31, q_level_32
    .xword q_level_33, q_level_34, q_level_35, q_level_36
    .xword q_level_37, q_level_38, q_level_39, q_level_40
    .xword q_level_41, q_level_42, q_level_43, q_level_44
    .xword q_level_45, q_level_46, q_level_47, q_level_48
    .xword q_level_49, q_level_50, q_level_51, q_level_52
    .xword q_level_53, q_level_54, q_level_55, q_level_56
    .xword q_level_57, q_level_58, q_level_59, q_level_60

// ——— Heresy S: Store Queue pressure ———
// x0 = iters, x1 = 0 vanilla (pure stores), x1 = 1 heresy (stores+loads)
// Returns total CNTVCT_EL0 ticks

.text
.align 4
.global s_store_burst
.type s_store_burst, %function
s_store_burst:
    sub  sp, sp, #256
    add  x2, sp, #0
    isb
    mrs  x9, cntvct_el0
    cbz  x1, 2f

1:  // HERESY: 16 stores + 2 dummy loads (drain SQ)
    stp  xzr,xzr,[x2]; stp xzr,xzr,[x2,#16]; stp xzr,xzr,[x2,#32]; stp xzr,xzr,[x2,#48]
    ldr  x3,[x2]
    stp  xzr,xzr,[x2,#64]; stp xzr,xzr,[x2,#80]; stp xzr,xzr,[x2,#96]; stp xzr,xzr,[x2,#112]
    ldr  x3,[x2,#64]
    subs x0,x0,#1; b.ne 1b
    b 4f

2:  // VANILLA: 16 pure stores
    stp  xzr,xzr,[x2]; stp xzr,xzr,[x2,#16]; stp xzr,xzr,[x2,#32]; stp xzr,xzr,[x2,#48]
    stp  xzr,xzr,[x2,#64]; stp xzr,xzr,[x2,#80]; stp xzr,xzr,[x2,#96]; stp xzr,xzr,[x2,#112]
    subs x0,x0,#1; b.ne 2b

4:
    isb
    mrs  x10, cntvct_el0
    sub  x0, x10, x9
    add  sp, sp, #256
    ret

// ——— Heresy V: L1D footprint pointer-chase ———
// x0 = head pointer, x1 = access count, returns total ticks

.global v_pointer_chase
.type v_pointer_chase, %function
v_pointer_chase:
    isb
    mrs  x9, cntvct_el0
    mov  x2, x0

1:
    ldr  x2, [x2]
    subs x1, x1, #1
    b.ne 1b

    isb
    mrs  x10, cntvct_el0
    sub  x0, x10, x9
    ret
