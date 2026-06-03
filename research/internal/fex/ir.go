package fex

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type IROpDef struct {
	Name       string
	Category   string
	RawDef     string
	HasSideEffects bool
	HasDest    bool
	DestSize   string
	DispatchOverride string
}

type IRManifest struct {
	Ops map[string]map[string]json.RawMessage `json:"Ops"`
}

func ParseIRJSON(path string) ([]IROpDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var manifest IRManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse IR.json: %w", err)
	}

	var ops []IROpDef
	for category, opMap := range manifest.Ops {
		for rawDef, meta := range opMap {
			op := IROpDef{
				Category: category,
				RawDef:   rawDef,
			}

			op.Name = parseOpName(rawDef)

			var metaMap map[string]interface{}
			if err := json.Unmarshal(meta, &metaMap); err != nil {
				continue
			}

			if v, ok := metaMap["HasSideEffects"]; ok {
				op.HasSideEffects, _ = v.(bool)
			}
			if v, ok := metaMap["HasDest"]; ok {
				op.HasDest, _ = v.(bool)
			}
			if v, ok := metaMap["DestSize"]; ok {
				op.DestSize, _ = v.(string)
			}
			if v, ok := metaMap["JITDispatchOverride"]; ok {
				op.DispatchOverride, _ = v.(string)
			}
			if v, ok := metaMap["SwitchGen"]; ok {
				b, _ := v.(bool)
				if !b {
					op.DispatchOverride = "NoOp"
				}
			}
			if v, ok := metaMap["RAOverride"]; ok {
				_ = v
				op.DispatchOverride = "NoOp"
			}

			ops = append(ops, op)
		}
	}

	return ops, nil
}

func parseOpName(rawDef string) string {
	parts := strings.Fields(rawDef)
	if len(parts) == 0 {
		return rawDef
	}

	idx := 0
	if strings.Contains(parts[0], "=") && !strings.Contains(parts[0], ":") {
		for i, p := range parts {
			if strings.HasPrefix(p, "=") || (i > 0 && !strings.HasPrefix(p, "SSA") &&
				!strings.HasPrefix(p, "GPR") && !strings.HasPrefix(p, "FPR") &&
				!strings.HasPrefix(p, "u") && !strings.HasPrefix(p, "i") &&
				!strings.HasPrefix(p, "OpSize") && !strings.HasPrefix(p, "Fence") &&
				!strings.HasPrefix(p, "Register") && !strings.HasPrefix(p, "Cond") &&
				!strings.HasPrefix(p, "SHA") && !strings.HasPrefix(p, "Mem") &&
				!strings.HasPrefix(p, "Break") && !strings.HasPrefix(p, "Round") &&
				!strings.HasPrefix(p, "Float") && !strings.HasPrefix(p, "Named") &&
				!strings.HasPrefix(p, "Index") && !strings.HasPrefix(p, "Shift") &&
				!strings.HasPrefix(p, "Branch") && !strings.HasPrefix(p, "Array") &&
				!strings.HasPrefix(p, "Const")) {
				idx = i
				break
			}
		}
	} else {
		idx = 0
	}

	name := parts[idx]
	if strings.Contains(name, "(") {
		name = name[:strings.Index(name, "(")]
	}

	return name
}

type IROpCategoryStats struct {
	Category string
	Count    int
	WithDest int
	WithSide int
	NoDispatch int
}

func IROpCategories(ops []IROpDef) []IROpCategoryStats {
	catMap := map[string]*IROpCategoryStats{}
	for _, op := range ops {
		s, ok := catMap[op.Category]
		if !ok {
			s = &IROpCategoryStats{Category: op.Category}
			catMap[op.Category] = s
		}
		s.Count++
		if op.HasDest {
			s.WithDest++
		}
		if op.HasSideEffects {
			s.WithSide++
		}
		if op.DispatchOverride == "NoOp" {
			s.NoDispatch++
		}
	}

	var result []IROpCategoryStats
	for _, s := range catMap {
		result = append(result, *s)
	}
	return result
}
