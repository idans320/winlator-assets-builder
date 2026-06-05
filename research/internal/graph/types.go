package graph

type FuncDef struct {
	Name      string   `json:"name"`
	File      string   `json:"file"`
	Line      int      `json:"line"`
	Kind      string   `json:"kind"`
	Signature string   `json:"signature,omitempty"`
	Gen8Site  bool     `json:"gen8_site"`
	Gen8Lines []int    `json:"gen8_lines,omitempty"`
	A8XXRegs  []string `json:"a8xx_regs,omitempty"`
	IsEntry   bool     `json:"is_entry"`
	Callees   []string `json:"callees,omitempty"`
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

type IndexGraph struct {
	Functions   map[string]*FuncDef `json:"functions"`
	Gen8Nodes   []Gen8Node          `json:"gen8_nodes"`
	EntryPoints []string            `json:"entry_points"`
	Edges       map[string][]string `json:"edges"`
	CtagsFile   string              `json:"ctags_file,omitempty"`
	Gen8File    string              `json:"gen8_file,omitempty"`
}
