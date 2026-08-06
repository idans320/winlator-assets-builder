package ast

import (
	"regexp"
	"strings"
)

var funcDefRe = regexp.MustCompile(`(?m)^(?:static\s+|inline\s+|constexpr\s+|virtual\s+|explicit\s+|template\s*<[^>]*>\s*)*((?:[\w:*]+\s+)+?)([\w:~]+)\s*\(([^)]*)\)\s*(?:const\s*)?\s*(?:override\s*)?\s*(?:noexcept\s*)?\{`)

var funcCallRe = regexp.MustCompile(`\b([\w:]+)\s*\(`)

var gen8PatternRe = regexp.MustCompile(`\b(CHIP\s*[<>=!]+\s*A8XX|A8XX_\w+|gen8\b|a8xx)`)
var gen8RegWriteRe = regexp.MustCompile(`(?:A8XX_\w+|a8xx_\w+)\s*\|=\s*|tu_cs_emit.*A8XX`)
var gen8ConditionalRe = regexp.MustCompile(`if\s*\(.*(?:CHIP\s*[<>=!]+\s*A8XX|A8XX).*\)`)

var vulkanEntryRe = regexp.MustCompile(`\b(vkCmd\w+|vkCreate\w+|vkDestroy\w+|vkAllocate\w+|vkFree\w+|vkBind\w+|vkGet\w+|vkMap\w+|vkUnmap\w+|vkQueue\w+|vkEnd\w+|vkBegin\w+|vkWait\w+|vkReset\w+|vkUpdate\w+|vkSet\w+)\b`)
var turnipEntryRe = regexp.MustCompile(`\b(tu_Cmd\w+|tu_Create\w+|tu_Destroy\w+|tu_Get\w+|tu_Alloc\w+|tu_Queue\w+|tu_Device\w+|tu_Enumerate\w+|tu_Init\w+|tu_Wait\w+)\b`)

type FuncDef struct {
	File      string   `json:"file"`
	Name      string   `json:"name"`
	Line      int      `json:"line"`
	Signature string   `json:"signature"`
	Gen8Site  bool     `json:"gen8_site"`
	Gen8Lines []int    `json:"gen8_lines,omitempty"`
	A8XXRegs  []string `json:"a8xx_regs,omitempty"`
	IsEntry   bool     `json:"is_entry"`
	IsVKEntry bool     `json:"is_vk_entry"`
	Callees   []string `json:"callees"`
}

type CallGraph struct {
	Functions   map[string]*FuncDef     `json:"functions"`
	Gen8Nodes   map[string]*Gen8Node    `json:"gen8_nodes"`
	EntryPoints []string                `json:"entry_points"`
	Edges       map[string][]string     `json:"edges"`
	FileDefs    map[string][]*FuncDef   `json:"file_defs"`
}

type Gen8Node struct {
	ID       string   `json:"id"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Kind     string   `json:"kind"`
	Content  string   `json:"content"`
	Regs     []string `json:"regs,omitempty"`
	Function string   `json:"function"`
}

func NewCallGraph() *CallGraph {
	return &CallGraph{
		Functions: make(map[string]*FuncDef),
		Gen8Nodes: make(map[string]*Gen8Node),
		Edges:     make(map[string][]string),
		FileDefs:  make(map[string][]*FuncDef),
	}
}

func (cg *CallGraph) AddGen8Node(fn *FuncDef, line int, kind, content string, regs []string) {
	id := fn.File + ":" + fn.Name + ":g8:" + itoa(line)
	if _, exists := cg.Gen8Nodes[id]; exists {
		return
	}
	cg.Gen8Nodes[id] = &Gen8Node{
		ID:       id,
		File:     fn.File,
		Line:     line,
		Kind:     kind,
		Content:  content,
		Regs:     regs,
		Function: fn.Name,
	}
	fn.Gen8Site = true
	fn.Gen8Lines = append(fn.Gen8Lines, line)
}

func (cg *CallGraph) AddEdge(caller, callee string) {
	for _, e := range cg.Edges[caller] {
		if e == callee {
			return
		}
	}
	cg.Edges[caller] = append(cg.Edges[caller], callee)
}

func ParseFile(file string, source []byte, knownCallees map[string]*FuncDef) *CallGraph {
	cg := NewCallGraph()
	src := string(source)

	lines := strings.Split(src, "\n")

	allMatches := funcDefRe.FindAllStringSubmatchIndex(src, -1)

	type rawFuncDef struct {
		nameStart int
		nameEnd   int
		bodyOpen  int
	}
	var rawDefs []rawFuncDef

	for _, m := range allMatches {
		if len(m) < 6 {
			continue
		}

		nameStart := m[4]
		nameEnd := m[5]
		if nameStart < 0 || nameEnd < 0 {
			continue
		}

		rawDefs = append(rawDefs, rawFuncDef{
			nameStart: nameStart,
			nameEnd:   nameEnd,
			bodyOpen:  m[1],
		})
	}

	lineStarts := make([]int, len(lines)+1)
	pos := 0
	for i, l := range lines {
		lineStarts[i] = pos
		pos += len(l) + 1
	}
	lineStarts[len(lines)] = pos

	for _, rd := range rawDefs {
		name := strings.TrimSpace(src[rd.nameStart:rd.nameEnd])
		if len(name) == 0 || (name[0] < 'a' && name[0] > 'z' && name[0] < 'A' && name[0] > 'Z' && name[0] != '_' && name[0] != '~') {
			continue
		}

		lineNum := 1
		for i := 1; i < len(lineStarts); i++ {
			if rd.bodyOpen >= lineStarts[i-1] && rd.bodyOpen < lineStarts[i] {
				lineNum = i
				break
			}
		}

		bodyStart := rd.bodyOpen
		bodyEnd := findClosingBrace(src, bodyStart)
		if bodyEnd < 0 {
			bodyEnd = len(src)
		}
		body := src[bodyStart+1 : bodyEnd]

		fn := &FuncDef{
			File:    file,
			Name:    name,
			Line:    lineNum,
			Callees: make([]string, 0),
		}

		fn.IsVKEntry = vulkanEntryRe.MatchString(name)
		fn.IsEntry = fn.IsVKEntry || turnipEntryRe.MatchString(name)

		calleeMap := make(map[string]bool)
		callMatches := funcCallRe.FindAllStringSubmatch(body, -1)
		for _, cm := range callMatches {
			if len(cm) < 2 {
				continue
			}
			callName := cm[1]
			if callName == "if" || callName == "for" || callName == "while" || callName == "switch" || callName == "return" ||
				callName == "sizeof" || callName == "decltype" || callName == "alignof" || callName == "static_cast" ||
				callName == "reinterpret_cast" || callName == "dynamic_cast" || callName == "const_cast" ||
				callName == "MIN2" || callName == "MAX2" || callName == "assert" || callName == "memset" ||
				callName == "memcpy" || callName == "strlen" || callName == "malloc" || callName == "free" ||
				callName == "COND" || callName == "UNREACHABLE" || callName == "MIN" || callName == "MAX" ||
				callName == "ARRAY_SIZE" || strings.HasPrefix(callName, "__") {
				continue
			}
			if calleeMap[callName] {
				continue
			}
			calleeMap[callName] = true
			fn.Callees = append(fn.Callees, callName)
			cg.AddEdge(name, callName)
		}

		if knC, ok := knownCallees[name]; ok {
			for _, c := range knC.Callees {
				fn.Callees = append(fn.Callees, c)
			}
		}

		gen8Hits := gen8PatternRe.FindAllStringIndex(body, -1)
		for _, hit := range gen8Hits {
			matchLine := lineNum
			pos := bodyStart + 1 + hit[0]
			for i := 1; i < len(lineStarts); i++ {
				if pos >= lineStarts[i-1] && pos < lineStarts[i] {
					matchLine = i
					break
				}
			}

			kind := "gen8-reference"
			content := body[max(0, hit[0]-20):min(len(body), hit[1]+60)]

			var regs []string
			regMatches := regexp.MustCompile(`A8XX_\w+`).FindAllString(body, -1)
			regs = dedup(regMatches)

			if gen8ConditionalRe.MatchString(content) {
				kind = "gen8-conditional"
			}
			if gen8RegWriteRe.MatchString(content) {
				kind = "gen8-register-write"
			}

			cg.AddGen8Node(fn, matchLine, kind, strings.TrimSpace(content), regs)
		}

		if fn.IsEntry {
			cg.EntryPoints = append(cg.EntryPoints, name)
		}

		cg.Functions[name] = fn
		cg.FileDefs[file] = append(cg.FileDefs[file], fn)
	}

	return cg
}

func findClosingBrace(src string, openPos int) int {
	if openPos >= len(src) || src[openPos] != '{' {
		return -1
	}
	depth := 1
	for i := openPos + 1; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		case '"':
			i++
			for i < len(src) && src[i] != '"' {
				i++
			}
		case '\'':
			i++
			if i < len(src) && src[i] == '\\' {
				i++
			}
			i++
		case '/':
			if i+1 < len(src) {
				if src[i+1] == '/' {
					i++
					for i < len(src) && src[i] != '\n' {
						i++
					}
				} else if src[i+1] == '*' {
					i += 2
					for i < len(src)-1 && !(src[i] == '*' && src[i+1] == '/') {
						i++
					}
					i++
				}
			}
		}
	}
	return -1
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func dedup(slice []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func (cg *CallGraph) Merge(other *CallGraph) {
	for name, fn := range other.Functions {
		if existing, ok := cg.Functions[name]; ok {
			for _, c := range fn.Callees {
				found := false
				for _, ec := range existing.Callees {
					if ec == c {
						found = true
						break
					}
				}
				if !found {
					existing.Callees = append(existing.Callees, c)
				}
			}
			existing.Gen8Site = existing.Gen8Site || fn.Gen8Site
			existing.Gen8Lines = append(existing.Gen8Lines, fn.Gen8Lines...)
			existing.A8XXRegs = append(existing.A8XXRegs, fn.A8XXRegs...)
			existing.IsEntry = existing.IsEntry || fn.IsEntry
			existing.IsVKEntry = existing.IsVKEntry || fn.IsVKEntry
		} else {
			cg.Functions[name] = fn
		}
	}

	for from, tos := range other.Edges {
		for _, to := range tos {
			cg.AddEdge(from, to)
		}
	}

	for id, node := range other.Gen8Nodes {
		cg.Gen8Nodes[id] = node
	}

	for _, ep := range other.EntryPoints {
		found := false
		for _, existingEP := range cg.EntryPoints {
			if existingEP == ep {
				found = true
				break
			}
		}
		if !found {
			cg.EntryPoints = append(cg.EntryPoints, ep)
		}
	}

	for file, defs := range other.FileDefs {
		cg.FileDefs[file] = append(cg.FileDefs[file], defs...)
	}
}

func (cg *CallGraph) TraceBackFromGen8() *TracedGraph {
	tg := &TracedGraph{
		Paths:  make([]*CallPath, 0),
		Nodes:  make(map[string]*PathNode),
		Edges:  make(map[string]bool),
	}

	tg.AddNode("entry", "Vulkan API Entry Points", "entry", "", 0, false)
	tg.AddNode("turnip", "Turnip Driver Core", "layer", "", 0, false)
	tg.AddNode("gen8_gpu", "Adreno 8xx GPU", "hardware", "", 0, true)

	for _, ep := range cg.EntryPoints {
		id := "func:" + ep
		fn := cg.Functions[ep]
		if fn == nil {
			fn = &FuncDef{Name: ep, File: "unknown", Line: 0}
		}
		tg.AddNode(id, ep+"()", "entry_point", fn.File, fn.Line, false)
		tg.AddEdge("entry", id, "vk entry")
	}

	visited := make(map[string]bool)
	for _, gn := range cg.Gen8Nodes {
		if cg.Functions[gn.Function] != nil {
			if tg.gen8NodeCount >= 150 {
				break
			}
			gen8ID := "gen8:" + gn.ID
			if _, exists := tg.Nodes[gen8ID]; exists {
				continue
			}
			tg.gen8NodeCount++

			label := gn.Content
			if len(label) > 60 {
				label = label[:57] + "..."
			}
			tg.AddNode(gen8ID, label, "gen8_"+gn.Kind, gn.File, gn.Line, true)

			fn2 := cg.Functions[gn.Function]
			funcID := "func:" + gn.Function
			tg.AddNode(funcID, gn.Function+"()", "function", fn2.File, fn2.Line, fn2.Gen8Site)
			tg.AddEdge(funcID, gen8ID, "contains gen8")

			if len(gn.Regs) > 0 {
				for _, reg := range gn.Regs {
					regID := "reg:" + reg
					tg.AddNode(regID, reg, "register", gn.File, gn.Line, true)
					tg.AddEdge(gen8ID, regID, "writes")
					tg.AddEdge(regID, "gen8_gpu", "programs")
				}
			}

			traceUp(cg, tg, gn.Function, 0, 3, visited)
		}
	}

	return tg
}

func traceUp(cg *CallGraph, tg *TracedGraph, funcName string, depth int, maxDepth int, visited map[string]bool) {
	if depth >= maxDepth || visited[funcName] {
		return
	}
	visited[funcName] = true

	callers := findCallers(cg, funcName)
	for _, caller := range callers {
		callerID := "func:" + caller
		fn := cg.Functions[caller]
		if fn == nil {
			fn = &FuncDef{Name: caller, File: "unknown", Line: 0}
		}
		isEntry := fn.IsEntry
		tg.AddNode(callerID, caller+"()", "function", fn.File, fn.Line, isEntry)
		tg.AddEdge(callerID, "func:"+funcName, "calls")

		if isEntry {
			tg.AddEdge("entry", callerID, "vk/tu entry")
		}

		traceUp(cg, tg, caller, depth+1, maxDepth, visited)
	}
}

func findCallers(cg *CallGraph, funcName string) []string {
	var callers []string
	for caller, callees := range cg.Edges {
		for _, callee := range callees {
			if callee == funcName {
				callers = append(callers, caller)
				break
			}
		}
	}
	return callers
}

type TracedGraph struct {
	Paths         []*CallPath           `json:"paths"`
	Nodes         map[string]*PathNode  `json:"nodes"`
	Edges         map[string]bool       `json:"edges"`
	gen8NodeCount int
}

type CallPath struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Hops []string `json:"hops"`
}

type PathNode struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Kind   string `json:"kind"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Gen8   bool   `json:"gen8"`
}

func (tg *TracedGraph) AddNode(id, label, kind, file string, line int, gen8 bool) {
	if _, exists := tg.Nodes[id]; exists {
		return
	}
	tg.Nodes[id] = &PathNode{
		ID:    id,
		Label: label,
		Kind:  kind,
		File:  file,
		Line:  line,
		Gen8:  gen8,
	}
}

func (tg *TracedGraph) AddEdge(from, to, label string) {
	key := from + "->" + to
	if tg.Edges[key] {
		return
	}
	tg.Edges[key] = true
}
