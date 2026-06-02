package ast

type DataFlowNode struct {
	ID       string            `json:"id"`
	Label    string            `json:"label"`
	Kind     string            `json:"kind"`
	Gen8     bool              `json:"gen8"`
	File     string            `json:"file"`
	Line     int               `json:"line"`
	Children []string          `json:"children,omitempty"`
	Props    map[string]string `json:"props,omitempty"`
}

type DataFlowEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
	Gen8  bool   `json:"gen8"`
}

type DataFlowGraph struct {
	Nodes []DataFlowNode `json:"nodes"`
	Edges []DataFlowEdge `json:"edges"`
}

func NewDataFlowGraph() *DataFlowGraph {
	return &DataFlowGraph{
		Nodes: make([]DataFlowNode, 0),
		Edges: make([]DataFlowEdge, 0),
	}
}

func (g *DataFlowGraph) AddNode(id, label, kind, file string, line int, gen8 bool) {
	for _, n := range g.Nodes {
		if n.ID == id {
			return
		}
	}
	g.Nodes = append(g.Nodes, DataFlowNode{
		ID:    id,
		Label: label,
		Kind:  kind,
		Gen8:  gen8,
		File:  file,
		Line:  line,
		Props: make(map[string]string),
	})
}

func (g *DataFlowGraph) AddEdge(from, to, label string, gen8 bool) {
	edgeKey := from + "->" + to + ":" + label
	for _, e := range g.Edges {
		if e.From+"->"+e.To+":"+e.Label == edgeKey {
			return
		}
	}
	g.Edges = append(g.Edges, DataFlowEdge{
		From:  from,
		To:    to,
		Label: label,
		Gen8:  gen8,
	})
}

func (g *DataFlowGraph) GetNode(id string) *DataFlowNode {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

func (g *DataFlowGraph) GetNodesByKind(kind string) []DataFlowNode {
	var result []DataFlowNode
	for _, n := range g.Nodes {
		if n.Kind == kind {
			result = append(result, n)
		}
	}
	return result
}

func (g *DataFlowGraph) GetGen8Nodes() []DataFlowNode {
	var result []DataFlowNode
	for _, n := range g.Nodes {
		if n.Gen8 {
			result = append(result, n)
		}
	}
	return result
}
