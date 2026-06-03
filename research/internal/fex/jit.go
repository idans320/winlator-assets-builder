package fex

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type JITFileInfo struct {
	Path       string
	TotalLines int
	OpDefs     int
}

type JITFuncInfo struct {
	Name        string
	LineStart   int
	IsOpHandler bool
	IRName      string
}

type EmissionCount struct {
	Name  string
	Count int
	Cat   ARMOpCategory
}

var (
	reFuncDef = regexp.MustCompile(`^\s*(?:static\s+|inline\s+|virtual\s+|const\s+|override\s+|constexpr\s+)*(?:void|bool|int|uint\w+|size_t|auto|[A-Z]\w+)\s+(?:Arm64JITCore::)?(\w+)\s*\([^)]*\)`)
	reOpFunc  = regexp.MustCompile(`Op_(\w+)`)
	reDefOp   = regexp.MustCompile(`(?:DEF_OP|DEF_BINOP_WITH_CONSTANT|DEF_COND_WITH_CONSTANT|DEF_UNARY_WITH_CONSTANT|DEF_FOR_EACH_OP|DEF_FOR_EACH_COMPARISON|DEF_FOR_EACH)\s*\(\s*(\w+)\s*[),]`)
	reCall    = regexp.MustCompile(`^\s*(?:\w+\.)?(\w+)\(`)
)

func ScanFuncs(lines []string) []JITFuncInfo {
	var funcs []JITFuncInfo

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		m := reOpFunc.FindStringSubmatch(trimmed)
		if len(m) >= 2 {
			funcs = append(funcs, JITFuncInfo{
				Name: "Op_" + m[1], LineStart: i + 1,
				IsOpHandler: true, IRName: m[1],
			})
			continue
		}
		m = reDefOp.FindStringSubmatch(trimmed)
		if len(m) >= 2 {
			funcs = append(funcs, JITFuncInfo{
				Name: "Op_" + m[1], LineStart: i + 1,
				IsOpHandler: true, IRName: m[1],
			})
			continue
		}
	}

	return funcs
}

func CountEmissions(lines []string) []EmissionCount {
	counts := make(map[string]*EmissionCount)
	known := knownEmissionCalls()

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		m := reCall.FindStringSubmatch(trimmed)
		if len(m) < 2 {
			continue
		}
		call := m[1]
		if !known[call] {
			continue
		}

		cat := ClassifyCall(call)
		if s, ok := counts[call]; ok {
			s.Count++
		} else {
			counts[call] = &EmissionCount{Name: call, Count: 1, Cat: cat}
		}
	}

	var result []EmissionCount
	for _, c := range counts {
		result = append(result, *c)
	}
	return result
}

func ClassifyCall(name string) ARMOpCategory {
	switch {
	case strings.HasPrefix(name, "b") || strings.HasPrefix(name, "bl") ||
		strings.HasPrefix(name, "cb") || strings.HasPrefix(name, "tb"):
		return CatBranch
	case strings.HasPrefix(name, "ld") || strings.HasPrefix(name, "st") ||
		strings.HasPrefix(name, "prf"):
		return CatLoadStore
	case strings.HasPrefix(name, "f") && len(name) > 1 && !strings.HasPrefix(name, "fmo"):
		return CatScalar
	case strings.HasPrefix(name, "ms") || strings.HasPrefix(name, "mr") ||
		strings.HasPrefix(name, "ds") || strings.HasPrefix(name, "dm") ||
		strings.HasPrefix(name, "is") || strings.HasPrefix(name, "hi") ||
		strings.HasPrefix(name, "no") || strings.HasPrefix(name, "dc"):
		return CatSystem
	case strings.Contains(name, "mov"):
		return CatMove
	default:
		return CatALU
	}
}

type CompilationModel struct {
	IRName              string
	File                string
	Line                int
	Lines               int
	LoadConstCalls      int
	MemoryEmits         int
	ALUEmits            int
	BranchEmits         int
	VecEmits            int
	SpillEmits          int
	EstimatedARM64Insns int
}

func AnalyzeOpFile(filePath string, irName string) (*CompilationModel, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	funcRe := regexp.MustCompile(fmt.Sprintf(`(?:Op_%s\b|DEF_[A-Z_]*\s*\(\s*%s\s*[),])`, irName, irName))

	model := &CompilationModel{IRName: irName, File: filePath}
	inFunc := false
	braceDepth := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !inFunc {
			if funcRe.MatchString(trimmed) {
				inFunc = true
				model.Line = i + 1
				braceDepth = strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
			}
			continue
		}

		model.Lines++
		opens := strings.Count(trimmed, "{")
		closes := strings.Count(trimmed, "}")
		braceDepth += opens - closes

		if braceDepth <= 0 {
			break
		}

		if strings.Contains(trimmed, "LoadConstant(") {
			model.LoadConstCalls++
		}
		cat, _ := ClassifyEmission(trimmed)
		switch cat {
		case CatALU:
			model.ALUEmits++
		case CatBranch:
			model.BranchEmits++
		case CatLoadStore:
			model.MemoryEmits++
		case CatASIMD, CatScalar, CatSVE:
			model.VecEmits++
		case CatSpillFill:
			model.SpillEmits++
		}
	}

	model.EstimatedARM64Insns = model.ALUEmits + model.BranchEmits + model.MemoryEmits +
		model.VecEmits + model.SpillEmits + model.LoadConstCalls*3

	return model, nil
}

func ParseInt(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}
