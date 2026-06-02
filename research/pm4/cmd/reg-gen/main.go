package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type RegisterDef struct {
	Offset   uint32
	Name     string
	Variants string
	IsArray  bool
	Length   int
	Width32  bool
}

type PM4Opcode struct {
	Name     string
	Value    uint32
	Variants string
}

type EnumValue struct {
	Name  string
	Value int
}

func main() {
	mesaFlag := flag.String("mesa", "", "Path to Mesa source root (env: MESA_SRC)")
	outFlag := flag.String("out", "internal/pm4/regmap.go", "Output Go file path")
	flag.Parse()

	mesaRoot := *mesaFlag
	if mesaRoot == "" {
		mesaRoot = os.Getenv("MESA_SRC")
	}
	if mesaRoot == "" {
		fmt.Fprintln(os.Stderr, "Error: --mesa or $MESA_SRC required")
		os.Exit(1)
	}

	regDir := filepath.Join(mesaRoot, "src", "freedreno", "registers", "adreno")

	regs := parseA6xxRegisterFile(filepath.Join(regDir, "a6xx.xml"))
	opcodes := parsePM4File(filepath.Join(regDir, "adreno_pm4.xml"))
	enums := parseEnumFile(filepath.Join(regDir, "a8xx_enums.xml"), "a8xx_statetype_id")

	fmt.Printf("A6XX registers: %d (A8XX variants: %d)\n", len(regs), countA8XX(regs))
	fmt.Printf("PM4 opcodes:    %d\n", len(opcodes))

	outDir := filepath.Dir(*outFlag)
	os.MkdirAll(outDir, 0755)

	out, err := os.Create(*outFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output: %v\n", err)
		os.Exit(1)
	}
	defer out.Close()

	writeGeneratedFile(out, regs, opcodes, enums)
	fmt.Printf("Generated %s\n", *outFlag)
}

func parseA6xxRegisterFile(path string) []RegisterDef {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		return nil
	}
	defer f.Close()

	var regs []RegisterDef
	decoder := xml.NewDecoder(f)

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		switch start.Name.Local {
		case "reg32", "reg64":
			r := RegisterDef{
				Width32: start.Name.Local == "reg32",
			}
			for _, attr := range start.Attr {
				switch attr.Name.Local {
				case "offset":
					r.Offset = parseHex(attr.Value)
				case "name":
					r.Name = attr.Value
				case "variants":
					r.Variants = attr.Value
				}
			}
			if r.Name != "" && r.Offset != 0 {
				regs = append(regs, r)
			}
		case "array":
			r := RegisterDef{
				IsArray: true,
				Length:  1,
			}
			for _, attr := range start.Attr {
				switch attr.Name.Local {
				case "offset":
					r.Offset = parseHex(attr.Value)
				case "name":
					r.Name = attr.Value
				case "variants":
					r.Variants = attr.Value
				case "length":
					r.Length, _ = strconv.Atoi(attr.Value)
				}
			}
			if r.Name != "" {
				for i := 0; i < r.Length; i++ {
					regs = append(regs, RegisterDef{
						Offset:   r.Offset + uint32(i),
						Name:     fmt.Sprintf("%s[%d]", r.Name, i),
						Variants: r.Variants,
					})
				}
			}
		}
	}
	return regs
}

func parsePM4File(path string) []PM4Opcode {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		return nil
	}
	defer f.Close()

	var opcodes []PM4Opcode
	decoder := xml.NewDecoder(f)
	inType3 := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "enum" {
				for _, attr := range t.Attr {
					if attr.Name.Local == "name" && attr.Value == "adreno_pm4_type3_packets" {
						inType3 = true
					}
				}
			}
			if inType3 && t.Name.Local == "value" {
				op := PM4Opcode{}
				for _, attr := range t.Attr {
					switch attr.Name.Local {
					case "name":
						op.Name = attr.Value
					case "value":
						op.Value = parseHex(attr.Value)
					case "variants":
						op.Variants = attr.Value
					}
				}
				if op.Name != "" {
					opcodes = append(opcodes, op)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "enum" {
				inType3 = false
			}
		}
	}
	return opcodes
}

func parseEnumFile(path, enumName string) []EnumValue {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var vals []EnumValue
	decoder := xml.NewDecoder(f)
	inTarget := false
	depth := 0

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "enum" {
				if depth == 0 {
					for _, attr := range t.Attr {
						if attr.Name.Local == "name" && attr.Value == enumName {
							inTarget = true
							break
						}
					}
				}
				depth++
			}
			if inTarget && t.Name.Local == "value" {
				ev := EnumValue{}
				for _, attr := range t.Attr {
					switch attr.Name.Local {
					case "name":
						ev.Name = attr.Value
					case "value":
						ev.Value, _ = strconv.Atoi(attr.Value)
					}
				}
				if ev.Name != "" {
					vals = append(vals, ev)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "enum" {
				depth--
				if depth == 0 {
					inTarget = false
				}
			}
		}
	}
	return vals
}

func parseHex(s string) uint32 {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0
	}
	return uint32(v)
}

func countA8XX(regs []RegisterDef) int {
	n := 0
	for _, r := range regs {
		if isA8XXVariant(r.Variants) {
			n++
		}
	}
	return n
}

func isA8XXVariant(v string) bool {
	return strings.Contains(v, "A8XX") || strings.Contains(v, "A8xx") || strings.Contains(v, "a8xx")
}

func isA7XXOrA8XX(v string) bool {
	return isA8XXVariant(v) || strings.Contains(v, "A7XX")
}

func writeGeneratedFile(out *os.File, regs []RegisterDef, opcodes []PM4Opcode, enums []EnumValue) {
	fmt.Fprintln(out, `// Code generated by cmd/reg-gen from Mesa XML register definitions. DO NOT EDIT.

package pm4

// Generated from Mesa freedreno register XML:
//   src/freedreno/registers/adreno/a6xx.xml
//   src/freedreno/registers/adreno/a8xx_enums.xml
//   src/freedreno/registers/adreno/adreno_pm4.xml

`)

	writeRegNameMap(out, regs)
	writeRegOffsetMap(out, regs)
	writeA8XXRegMap(out, regs)
	writeRegByPrefix(out, regs)
	writePM4OpcodeMap(out, opcodes)
	writeStatetypeIDs(out, enums)
}

func writeRegNameMap(out *os.File, regs []RegisterDef) {
	fmt.Fprintln(out, "// RegNameByOffset maps register offset to human-readable name.")
	fmt.Fprintln(out, "var RegNameByOffset = map[uint32]string{")
	seen := make(map[uint32]bool)
	sort.Slice(regs, func(i, j int) bool { return regs[i].Offset < regs[j].Offset })
	for _, r := range regs {
		if seen[r.Offset] || r.Name == "" {
			continue
		}
		seen[r.Offset] = true
		fmt.Fprintf(out, "\t0x%04x: %q,\n", r.Offset, r.Name)
	}
	fmt.Fprint(out, "}\n")
}

func writeRegOffsetMap(out *os.File, regs []RegisterDef) {
	fmt.Fprintln(out, "// RegOffsetByName maps human-readable register name to offset.")
	fmt.Fprintln(out, "var RegOffsetByName = map[string]uint32{")
	sort.Slice(regs, func(i, j int) bool { return regs[i].Name < regs[j].Name })
	seen := make(map[string]bool)
	for _, r := range regs {
		if seen[r.Name] || r.Name == "" {
			continue
		}
		seen[r.Name] = true
		fmt.Fprintf(out, "\t%q: 0x%04x,\n", r.Name, r.Offset)
	}
	fmt.Fprint(out, "}\n")
}

func writeA8XXRegMap(out *os.File, regs []RegisterDef) {
	fmt.Fprintln(out, "// IsA8XXRegister returns true if the register offset is gen8-specific.")
	fmt.Fprintln(out, "var IsA8XXRegister = map[uint32]bool{")
	seen := make(map[uint32]bool)
	for _, r := range regs {
		if isA8XXVariant(r.Variants) {
			if seen[r.Offset] {
				continue
			}
			seen[r.Offset] = true
			fmt.Fprintf(out, "\t0x%04x: true, // %s\n", r.Offset, r.Name)
		}
	}
	fmt.Fprint(out, "}\n")
}

func writeRegByPrefix(out *os.File, regs []RegisterDef) {
	prefixes := map[string][]RegisterDef{}
	for _, r := range regs {
		parts := strings.SplitN(r.Name, "_", 2)
		if len(parts) > 0 {
			prefixes[parts[0]] = append(prefixes[parts[0]], r)
		}
	}

	fmt.Fprintln(out, "// RegistersByBlock groups register offsets by hardware block prefix.")
	fmt.Fprintln(out, "var RegistersByBlock = map[string][]uint32{")
	for prefix, blockRegs := range prefixes {
		var offsets []uint32
		seen := make(map[uint32]bool)
		for _, r := range blockRegs {
			if r.Offset == 0 || seen[r.Offset] {
				continue
			}
			seen[r.Offset] = true
			offsets = append(offsets, r.Offset)
		}
		if len(offsets) > 0 {
			sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
			fmt.Fprintf(out, "\t%q: {", prefix)
			for i, off := range offsets {
				if i > 0 {
					fmt.Fprint(out, ", ")
				}
				fmt.Fprintf(out, "0x%04x", off)
			}
			fmt.Fprintln(out, "},")
		}
	}
	fmt.Fprint(out, "}\n")
}

func writePM4OpcodeMap(out *os.File, opcodes []PM4Opcode) {
	fmt.Fprintln(out, "// RegisteredPM4Type3Opcodes maps Type3 opcode value to CP_* name.")
	fmt.Fprintln(out, "// Generated from adreno_pm4.xml <enum name=\"adreno_pm4_type3_packets\">.")
	fmt.Fprintln(out, "var RegisteredPM4Type3Opcodes = map[uint32]string{")
	seen := make(map[uint32]bool)
	for _, op := range opcodes {
		if seen[op.Value] {
			continue
		}
		seen[op.Value] = true
		fmt.Fprintf(out, "\t0x%02x: %q,", op.Value, op.Name)
		if isA8XXVariant(op.Variants) {
			fmt.Fprint(out, " // A8XX+")
		}
		fmt.Fprintln(out)
	}
	fmt.Fprint(out, "}\n")
}

func writeStatetypeIDs(out *os.File, enums []EnumValue) {
	if len(enums) == 0 {
		return
	}
	fmt.Fprintln(out, "// A8XXStatetypeIDs maps state type ID to name (from a8xx_enums.xml).")
	fmt.Fprintln(out, "var A8XXStatetypeIDs = map[uint32]string{")
	for _, ev := range enums {
		fmt.Fprintf(out, "\t%d: %q,\n", ev.Value, ev.Name)
	}
	fmt.Fprint(out, "}\n")
}
