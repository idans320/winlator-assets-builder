package pm4

// PM4 packet type constants
const (
	Type0Mask = 0x00000000
	Type3Mask = 0xc0000000
	Type4Mask = 0x40000000
	Type7Mask = 0x70000000
)

// Type3 opcodes (CP_*) from adreno_pm4.xml
const (
	CP_ME_INIT                          = 0x48
	CP_NOP                              = 0x10
	CP_INDIRECT_BUFFER                  = 0x3f
	CP_INDIRECT_BUFFER_CHAIN            = 0x57 // A5XX-
	CP_INDIRECT_BUFFER_PFD              = 0x37 // A5XX-
	CP_WAIT_FOR_IDLE                    = 0x26
	CP_WAIT_REG_MEM                     = 0x3c
	CP_WAIT_REG_EQ                      = 0x52
	CP_WAIT_REG_GTE                     = 0x53
	CP_WAIT_UNTIL_READ                  = 0x5c
	CP_WAIT_IB_PFD_COMPLETE             = 0x5d
	CP_REG_RMW                          = 0x21
	CP_REG_TO_MEM                       = 0x3e
	CP_MEM_WRITE                        = 0x3d
	CP_MEM_WRITE_AT                    = 0x6d
	CP_MEM_TO_MEM                       = 0x58 // deprecated
	CP_COND_EXEC                        = 0x44
	CP_COND_WRITE                       = 0x45
	CP_EVENT_WRITE                      = 0x46 // A2XX-A6XX
	CP_EVENT_WRITE7                     = 0x46 // A7XX-
	CP_DRAW_INDX                        = 0x22
	CP_DRAW_INDX_2                      = 0x28 // A5XX-
	CP_DRAW_INDX_BIN                    = 0x23
	CP_DRAW_INDX_BIN_2                  = 0x29 // A5XX-
	CP_VIZ_QUERY                        = 0x24 // A5XX-
	CP_SET_STATE                        = 0x25
	CP_INVALIDATE_STATE                 = 0x3b
	CP_INTERRUPT                        = 0x40
	CP_IM_LOAD                          = 0x27 // A5XX-
	CP_IM_LOAD_IMMEDIATE                = 0x2b // A5XX-
	CP_DRAW_INDIRECT_MULTI              = 0x2a // A6XX-
	CP_BLIT                             = 0x2c // A5XX-
	CP_LOAD_STATE6                      = 0x36 // A6XX-
	CP_LOAD_STATE6_FRAG                 = 0x34 // A6XX-
	CP_LOAD_STATE6_GEOM                 = 0x32 // A6XX-
	CP_EXEC_CS                          = 0x33 // A6XX-
	CP_SET_BIN_DATA5                    = 0x3A // A7XX-
	CP_BV_BRUSH                         = 0x50
	CP_SET_MARKER                       = 0x65 // A8XX-
	CP_SET_PSEUDO_REG                   = 0x56
	CP_SET_DRAW_STATE                   = 0x43
	CP_DRAW_INDX_OFFSET                 = 0x38
	CP_DRAW_INDIRECT                    = 0x2f
	CP_DRAW_INDIRECT_2                  = 0x2e
	CP_DRAW_INDIRECT_MULTI_2            = 0x2d
	CP_DRAW_AUTO                        = 0x24
	CP_DRAW_PRED_ENABLE                 = 0x79
	CP_DRAW_PRED_SET                    = 0x6e
	CP_REG_TEST                         = 0x39
	CP_SET_PROTECTED_MODE               = 0x5f
	CP_CONTEXT_SWITCH                   = 0x54 // A6XX-
	CP_SKIP_IB2_ENABLE_GLOBAL           = 0x1c
	CP_SKIP_IB2_ENABLE_LOCAL            = 0x1d
	CP_SET_SUBDRAW_SIZE                 = 0x35
	CP_BOOTSTRAP_UCODE                  = 0x6f
	CP_MEMORY_MAP_UPDATE                = 0x58 // A8XX-
	CP_BARRIER                          = 0x59 // A8XX-
	CP_SET_AMBIENT                      = 0x5a
	CP_SET_AMBIENT2                     = 0x60
	CP_SET_BIN_MASK                     = 0x5b
	CP_SET_BIN_MASK2                    = 0x61
	CP_REG_WRITE                        = 0x6a // A8XX-
	CP_WAIT_FOR_ME                      = 0x13 // A8XX-
	CP_FIXED_STRIDE_DRAW_TABLE          = 0x6b // A8XX-
	CP_FIXED_STRIDE_DRAW_TABLE_BIN      = 0x6c // A8XX-
	CP_WAIT_MEM_WRITES                  = 0x12 // A8XX-
)

// Event write constants
const (
	LABEL                             = 0 // A8XX-
	DEPTH_BUFFER_FLIP                 = 0x3d // A8XX-
	STORE_ZPASS_MASK_TO_SYSMEM        = 0x1e
	CACHE_FLUSH_TS                    = 0x1f
	WT_DONE_GFX_CORE                  = 0x20
	RB_DONE_TS                        = 0x22
	SUBPASS_SLICE_FENCE               = 0x36 // A8XX-
	CCH_FAST_CLEAR_CLEAN              = 0x1b // A8XX-
)

// PKT4 / PKT7 header helpers
func Pkt4Header(reg uint16, count uint16) uint32 {
	return Type4Mask | (uint32(reg) & 0x3ffff) | ((uint32(count) & 0x3f) << 24)
}

func Pkt7Header(opcode uint32, count uint16) uint32 {
	return Type7Mask | ((opcode & 0x7f) << 23) | (uint32(count) & 0x7fff)
}

func Pkt4Reg(header uint32) uint16 {
	return uint16(header & 0x3ffff)
}

func Pkt4Count(header uint32) uint16 {
	return uint16((header >> 24) & 0x3f)
}

func Pkt7Opcode(header uint32) uint32 {
	return (header >> 23) & 0x7f
}

func Pkt7Count(header uint32) uint16 {
	return uint16(header & 0x7fff)
}

// Opcode name map for human-readable logging.
// Note: some opcodes share the same numeric value across generations.
// The A8XX/current variant takes precedence where conflicts exist.
var OpcodeNames = buildOpcodeNames()

func buildOpcodeNames() map[uint32]string {
	m := map[uint32]string{
		CP_ME_INIT:                          "CP_ME_INIT",
		CP_NOP:                              "CP_NOP",
		CP_INDIRECT_BUFFER:                  "CP_INDIRECT_BUFFER",
		CP_INDIRECT_BUFFER_CHAIN:            "CP_INDIRECT_BUFFER_CHAIN",
		CP_WAIT_FOR_IDLE:                    "CP_WAIT_FOR_IDLE",
		CP_WAIT_REG_MEM:                     "CP_WAIT_REG_MEM",
		CP_WAIT_REG_EQ:                      "CP_WAIT_REG_EQ",
		CP_WAIT_REG_GTE:                     "CP_WAIT_REG_GTE",
		CP_WAIT_UNTIL_READ:                  "CP_WAIT_UNTIL_READ",
		CP_WAIT_IB_PFD_COMPLETE:             "CP_WAIT_IB_PFD_COMPLETE",
		CP_REG_RMW:                          "CP_REG_RMW",
		CP_REG_TO_MEM:                       "CP_REG_TO_MEM",
		CP_MEM_WRITE:                        "CP_MEM_WRITE",
		CP_MEM_WRITE_AT:                     "CP_MEM_WRITE_AT",
		CP_COND_EXEC:                        "CP_COND_EXEC",
		CP_COND_WRITE:                       "CP_COND_WRITE",
		CP_EVENT_WRITE:                      "CP_EVENT_WRITE",
		CP_DRAW_INDX:                        "CP_DRAW_INDX",
		CP_DRAW_INDX_BIN:                    "CP_DRAW_INDX_BIN",
		CP_SET_STATE:                        "CP_SET_STATE",
		CP_INVALIDATE_STATE:                 "CP_INVALIDATE_STATE",
		CP_INTERRUPT:                        "CP_INTERRUPT",
		CP_IM_LOAD:                          "CP_IM_LOAD",
		CP_IM_LOAD_IMMEDIATE:                "CP_IM_LOAD_IMMEDIATE",
		CP_LOAD_STATE6:                      "CP_LOAD_STATE6",
		CP_LOAD_STATE6_FRAG:                 "CP_LOAD_STATE6_FRAG",
		CP_LOAD_STATE6_GEOM:                 "CP_LOAD_STATE6_GEOM",
		CP_EXEC_CS:                          "CP_EXEC_CS",
		CP_MEMORY_MAP_UPDATE:                "CP_MEMORY_MAP_UPDATE",
		CP_BARRIER:                          "CP_BARRIER",
		CP_SET_MARKER:                       "CP_SET_MARKER",
		CP_SET_PSEUDO_REG:                   "CP_SET_PSEUDO_REG",
		CP_SET_DRAW_STATE:                   "CP_SET_DRAW_STATE",
		CP_DRAW_INDX_OFFSET:                 "CP_DRAW_INDX_OFFSET",
		CP_DRAW_INDIRECT:                    "CP_DRAW_INDIRECT",
		CP_DRAW_INDIRECT_2:                  "CP_DRAW_INDIRECT_2",
		CP_DRAW_INDIRECT_MULTI_2:            "CP_DRAW_INDIRECT_MULTI_2",
		CP_DRAW_AUTO:                        "CP_DRAW_AUTO",
		CP_DRAW_PRED_ENABLE:                 "CP_DRAW_PRED_ENABLE",
		CP_DRAW_PRED_SET:                    "CP_DRAW_PRED_SET",
		CP_REG_TEST:                         "CP_REG_TEST",
		CP_SET_PROTECTED_MODE:               "CP_SET_PROTECTED_MODE",
		CP_CONTEXT_SWITCH:                   "CP_CONTEXT_SWITCH",
		CP_SKIP_IB2_ENABLE_GLOBAL:           "CP_SKIP_IB2_ENABLE_GLOBAL",
		CP_SKIP_IB2_ENABLE_LOCAL:            "CP_SKIP_IB2_ENABLE_LOCAL",
		CP_SET_SUBDRAW_SIZE:                 "CP_SET_SUBDRAW_SIZE",
		CP_BOOTSTRAP_UCODE:                  "CP_BOOTSTRAP_UCODE",
		CP_SET_AMBIENT:                      "CP_SET_AMBIENT",
		CP_SET_AMBIENT2:                     "CP_SET_AMBIENT2",
		CP_SET_BIN_MASK:                     "CP_SET_BIN_MASK",
		CP_SET_BIN_MASK2:                    "CP_SET_BIN_MASK2",
		CP_REG_WRITE:                        "CP_REG_WRITE",
		CP_WAIT_FOR_ME:                      "CP_WAIT_FOR_ME",
		CP_FIXED_STRIDE_DRAW_TABLE:          "CP_FIXED_STRIDE_DRAW_TABLE",
		CP_FIXED_STRIDE_DRAW_TABLE_BIN:      "CP_FIXED_STRIDE_DRAW_TABLE_BIN",
		CP_WAIT_MEM_WRITES:                  "CP_WAIT_MEM_WRITES",
	}
	return m
}

// A8XX-specific opcodes only present on gen8+
var A8XXOpcodes = map[uint32]bool{
	CP_SET_MARKER:                     true,
	CP_MEMORY_MAP_UPDATE:              true,
	CP_BARRIER:                        true,
	CP_REG_WRITE:                      true,
	CP_WAIT_FOR_ME:                    true,
	CP_FIXED_STRIDE_DRAW_TABLE:        true,
	CP_FIXED_STRIDE_DRAW_TABLE_BIN:    true,
	CP_WAIT_MEM_WRITES:                true,
}

// Barrier / flush-type opcodes
var BarrierOpcodes = map[uint32]bool{
	CP_WAIT_FOR_IDLE:     true,
	CP_WAIT_FOR_ME:       true,
	CP_WAIT_MEM_WRITES:   true,
	CP_BARRIER:           true,
	CP_INVALIDATE_STATE:  true,
	CP_EVENT_WRITE:       true,
}

// Draw-type opcodes
var DrawOpcodes = map[uint32]bool{
	CP_DRAW_INDX:                     true,
	CP_DRAW_INDX_2:                   true,
	CP_DRAW_INDX_BIN:                 true,
	CP_DRAW_INDX_BIN_2:               true,
	CP_DRAW_INDIRECT_MULTI:           true,
	CP_DRAW_INDIRECT:                 true,
	CP_DRAW_INDIRECT_2:               true,
	CP_DRAW_INDIRECT_MULTI_2:         true,
	CP_DRAW_AUTO:                     true,
	CP_DRAW_INDX_OFFSET:              true,
	CP_EXEC_CS:                       true,
	CP_DRAW_PRED_ENABLE:              true,
	CP_DRAW_PRED_SET:                 true,
	CP_FIXED_STRIDE_DRAW_TABLE:       true,
	CP_FIXED_STRIDE_DRAW_TABLE_BIN:   true,
}

func OpcodeName(opcode uint32) string {
	if name, ok := OpcodeNames[opcode]; ok {
		return name
	}
	return "UNKNOWN"
}
