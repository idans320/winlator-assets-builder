package fex

import (
	"regexp"
	"strings"
)

type ARMOpCategory int

const (
	CatALU ARMOpCategory = iota
	CatBranch
	CatLoadStore
	CatSystem
	CatScalar
	CatASIMD
	CatSVE
	CatMove
	CatSpillFill
	CatLabel
)

func (c ARMOpCategory) String() string {
	return [...]string{"ALU", "Branch", "LoadStore", "System", "Scalar", "ASIMD", "SVE", "Move", "SpillFill", "Label"}[c]
}

type EmissionSite struct {
	File     string
	Line     int
	Category ARMOpCategory
	Call     string
	Args     string
}

type JITHandler struct {
	OpName   string
	File     string
	Lines    int
	Emission []EmissionSite
}

type HotPath struct {
	Name        string
	Description string
	File        string
	Line        int
	Cat         string
	Rank        int
}

var (
	ReALUScalar = regexp.MustCompile(`\b(add|sub|mul|madd|msub|neg|and|orr|eor|bic|orn|eon|adc|sbc|udiv|sdiv|lsl|lsr|asr|ror|uxtb|uxth|uxtw|sxtb|sxth|sxtw|sbfx|ubfx|sbfm|ubfm|bfm|bfi|bfc|csel|csinc|csinv|csneg|ccmn|ccmp|rbit|rev|rev16|rev32|clz|cls)\(`)
	ReMemory    = regexp.MustCompile(`\b(ldr|ldrb|ldrh|ldrsb|ldrsh|ldrsw|ldp|stp|str|strb|strh|ldar|stlr|ldarb|stlrb|ldarh|stlrh|ldaxr|stlxr|ldaxrb|stlxrb|ldaxrh|stlxrh|prfm|prfum|lsl\w*str|str\w*_c)\(`)
	ReBranch    = regexp.MustCompile(`\b(b|bl|blr|br|ret|cbz|cbnz|tbz|tbnz|blraa|braaz|eret|hlt|brk|svc)\(`)
	ReVec       = regexp.MustCompile(`\b(fmov|fabs|fneg|fsqrt|fadd|fsub|fmul|fdiv|fmla|fmls|fmax|fmin|fmaxnm|fminnm|fcsel|fcmp|fcvtzs|fcvtzu|scvtf|ucvtf|frint|ins|umov|smov|dup|mov\w+_v|ld\w+v|st\w+v|sadd|uadd|addp|zip|uzp|trn|shl|ushr|sshr|shll|srshr|urshr|sshr_imm|bic_v|orr_v|and_v|eor_v|not_v|add_v|sub_v|mul_v|cmeq|cmge|cmgt|cmhi|cmhs|cmtst|tbl|cmtst_v)\(`)
	ReMove      = regexp.MustCompile(`\b(mov\w*|fmov\w*|movz|movn|movk)\(`)

	ReSpillGPR = regexp.MustCompile(`\b(stp\s*\(.*x[0-9]+\s*,\s*x[0-9]+)\b`)
	ReFillGPR  = regexp.MustCompile(`\b(ldp\s*\(.*x[0-9]+\s*,\s*x[0-9]+)\b`)
	ReSpillFPR = regexp.MustCompile(`\b(str\s*\(.*[qv][0-9]+)\b`)
	ReFillFPR  = regexp.MustCompile(`\b(ldr\s*\(.*[qv][0-9]+)\b`)

	ReLoadConstant = regexp.MustCompile(`LoadConstant\(`)
	ReTSOEmulation = regexp.MustCompile(`TSOEmulation\|TSOMode\|store_tsoldar\w*\|stlr_enhanced\|str_enhanced`)
)

var JITEmissionCats = map[string]ARMOpCategory{
	"add(": CatALU, "sub(": CatALU, "mul(": CatALU, "and(": CatALU,
	"orr(": CatALU, "eor(": CatALU, "bic(": CatALU,
	"csel(": CatALU, "csinv(": CatALU, "csneg(": CatALU,
	"uxtb(": CatALU, "uxth(": CatALU, "sxtb(": CatALU,
	"lsl(": CatALU, "lsr(": CatALU, "asr(": CatALU,
	"bfm(": CatALU, "ubfm(": CatALU, "sbfm(": CatALU,
	"neg(": CatALU, "adc(": CatALU, "ccmp(": CatALU,

	"b(": CatBranch, "bl(": CatBranch, "blr(": CatBranch,
	"br(": CatBranch, "ret(": CatBranch,
	"cbz(": CatBranch, "cbnz(": CatBranch,
	"tbz(": CatBranch, "tbnz(": CatBranch,

	"ldr(": CatLoadStore, "str(": CatLoadStore,
	"ldp(": CatLoadStore, "stp(": CatLoadStore,
	"ldrb(": CatLoadStore, "strb(": CatLoadStore,
	"ldrh(": CatLoadStore, "strh(": CatLoadStore,
	"ldar(": CatLoadStore, "stlr(": CatLoadStore,
	"ldaxr(": CatLoadStore, "stlxr(": CatLoadStore,
	"prfm(": CatLoadStore, "prfum(": CatLoadStore,

	"msr(": CatSystem, "mrs(": CatSystem,
	"hint(": CatSystem, "nop(": CatSystem,
	"dmb(": CatSystem, "dsb(": CatSystem,
	"isb(": CatSystem, "dc(": CatSystem,
	"ic(": CatSystem, "hvc(": CatSystem,

	"fcmp(": CatScalar, "fadd(": CatScalar,
	"fsub(": CatScalar, "fmul(": CatScalar,
	"fdiv(": CatScalar, "fneg(": CatScalar,
	"fabs(": CatScalar, "fsqrt(": CatScalar,
	"fmov(": CatScalar,
	"fcvt(": CatScalar, "scvtf(": CatScalar,

	"dup(": CatASIMD, "ins(": CatASIMD,
	"umov(": CatASIMD, "smov(": CatASIMD,
	"add v": CatASIMD, "sub v": CatASIMD,
	"mul v": CatASIMD, "mla v": CatASIMD,
	"shl v": CatASIMD, "sshr": CatASIMD,
	"ushl": CatASIMD, "sshll": CatASIMD,

	"mov(": CatMove, "movz(": CatMove,
	"movk(": CatMove, "movn(": CatMove,
}

var (
	ReOpHandler = regexp.MustCompile(`Op_(\w+)`)
	ReEmitCall  = regexp.MustCompile(`^\s*(this->)?(\w+)\(`)
)

var KnownHotPaths = []HotPath{
	{Name: "LoadConstant", Description: "64-bit constant load (movz/movk/literal pool)", Cat: "constant", Rank: 1},
	{Name: "SpillStaticRegs", Description: "Register spill to memory on calls/transitions", Cat: "spill", Rank: 2},
	{Name: "FillStaticRegs", Description: "Register fill from memory on returns/entry", Cat: "spill", Rank: 3},
	{Name: "HandleLoadMemTSO", Description: "TSO-aware memory load + ordering barriers", Cat: "memory", Rank: 4},
	{Name: "HandleStoreMemTSO", Description: "TSO-aware memory store + ordering barriers", Cat: "memory", Rank: 5},
	{Name: "GenerateX87StackOptimization", Description: "x87 FPU stack fixup for SVE hosts", Cat: "x87", Rank: 6},
	{Name: "EmitVectorALU", Description: "Vector ALU op emission (handles elem size)", Cat: "vector", Rank: 7},
	{Name: "EmitMemoryLoadStore", Description: "General load-store with address calc", Cat: "memory", Rank: 8},
	{Name: "PushDynamicRegs", Description: "Push dynamic (RA-allocated) registers", Cat: "spill", Rank: 9},
	{Name: "PopDynamicRegs", Description: "Pop dynamic (RA-allocated) registers", Cat: "spill", Rank: 10},
	{Name: "CompileCode", Description: "IR traversal + JIT dispatch loop", Cat: "compile", Rank: 11},
	{Name: "EmitX87HelperCall", Description: "Helper call for complex x87 ops", Cat: "x87", Rank: 12},
}

func ClassifyEmission(line string) (ARMOpCategory, string) {
	cleaned := strings.TrimSpace(line)

	for prefix, cat := range JITEmissionCats {
		if strings.HasPrefix(cleaned, prefix) {
			return cat, prefix
		}
	}

	if strings.HasPrefix(cleaned, "adr(") || strings.HasPrefix(cleaned, "adrp(") {
		return CatALU, cleaned[:strings.Index(cleaned, "(")]
	}
	if strings.Contains(cleaned, "Label") || strings.Contains(cleaned, "Bind(") {
		return CatLabel, "label"
	}
	if strings.HasPrefix(cleaned, "LoadConstant(") {
		return CatALU, "LoadConstant"
	}
	if strings.Contains(cleaned, "Spill") || strings.Contains(cleaned, "spill") {
		return CatSpillFill, "spill"
	}
	if strings.Contains(cleaned, "Fill") || strings.Contains(cleaned, "fill") || strings.Contains(cleaned, "Pop") {
		return CatSpillFill, "fill"
	}

	return -1, ""
}

type JITOpStats struct {
	Name       string
	File       string
	LineStart  int
	LineEnd    int
	ALULines   int
	BranchLines int
	MemLines   int
	VecLines   int
	MoveLines  int
	SysLines   int
	OtherLines int
	TotalLines int
	LoadConstCalls int
}

func knownEmissionCalls() map[string]bool {
	return map[string]bool{
		"add": true, "sub": true, "mul": true, "and": true, "orr": true, "eor": true, "bic": true,
		"mov": true, "movz": true, "movk": true, "movn": true,
		"csel": true, "csinc": true, "csinv": true, "csneg": true,
		"lsl": true, "lsr": true, "asr": true, "ror": true,
		"uxtb": true, "uxth": true, "sxtb": true, "sxth": true, "sxtw": true,
		"ubfm": true, "sbfm": true, "bfm": true, "bfi": true, "bfc": true,
		"neg": true, "adc": true, "sbc": true, "mvn": true,
		"umulh": true, "smulh": true, "udiv": true, "sdiv": true,
		"madd": true, "msub": true,
		"clz": true, "cls": true, "rbit": true, "rev": true, "rev16": true, "rev32": true,
		"ccmp": true, "ccmn": true,
		"b": true, "bl": true, "blr": true, "br": true, "ret": true,
		"cbz": true, "cbnz": true, "tbz": true, "tbnz": true,
		"ldr": true, "str": true, "ldp": true, "stp": true,
		"ldrb": true, "strb": true, "ldrh": true, "strh": true,
		"ldrsb": true, "ldrsh": true, "ldrsw": true,
		"ldar": true, "stlr": true, "ldaxr": true, "stlxr": true,
		"prfm": true, "prfum": true,
		"fmov": true, "fabs": true, "fneg": true, "fsqrt": true,
		"fadd": true, "fsub": true, "fmul": true, "fdiv": true,
		"fcmp": true, "fcsel": true, "fcvt": true, "scvtf": true, "ucvtf": true,
		"frintm": true, "frintp": true, "frintn": true, "frintz": true,
		"ins": true, "umov": true, "smov": true, "dup": true,
		"hint": true, "nop": true, "msr": true, "mrs": true,
		"dmb": true, "dsb": true, "isb": true,
		"adr": true, "adrp": true,
		"abs": true, "movi": true, "not": true,
		"fmla": true, "fmls": true,
		"ext": true, "zip1": true, "zip2": true,
		"uzp1": true, "uzp2": true, "trn1": true, "trn2": true,
		"shl": true, "ushr": true, "sshr": true, "sli": true, "sri": true,
		"cmhi": true, "cmhs": true, "cmeq": true, "cmge": true, "cmgt": true, "cmtst": true,
		"tbl": true, "tbx": true,
	}
}
