// Package umodel defines the Unified Model (UModel) — a language-agnostic
// graph representation of source code used to build the knowledge graph.
package umodel

// NodeType identifies the kind of a graph node.
type NodeType string

const (
	NodeFile      NodeType = "File"
	NodePackage   NodeType = "Package"
	NodeFunction  NodeType = "Function"
	NodeMethod    NodeType = "Method"
	NodeStruct    NodeType = "Struct"
	NodeInterface NodeType = "Interface"
	NodeVariable  NodeType = "Variable"
	NodeImport    NodeType = "Import"
	NodeSymbol    NodeType = "Symbol"    // named symbol referenced via a call (may or may not be defined in-repo)
	NodeCommit    NodeType = "Commit"    // a git commit that touched source files
	NodeTicket    NodeType = "Ticket"    // a ticket/issue ID extracted from a commit message
)

// EdgeType identifies the kind of a relationship between nodes.
type EdgeType string

const (
	EdgeContains   EdgeType = "CONTAINS"
	EdgeCalls      EdgeType = "CALLS"
	EdgeImplements EdgeType = "IMPLEMENTS"
	EdgeImports    EdgeType = "IMPORTS"
	EdgeDependsOn  EdgeType = "DEPENDS_ON"
	EdgeDefines    EdgeType = "DEFINES"
	EdgeHasCommit  EdgeType = "HAS_COMMIT"       // File → Commit
	EdgeMentions   EdgeType = "MENTIONS_TICKET"  // Commit → Ticket
)

// Node represents a single entity in the knowledge graph.
type Node struct {
	ID         string            `json:"id"`
	Type       NodeType          `json:"type"`
	Language   string            `json:"language"`
	Name       string            `json:"name"`
	FilePath   string            `json:"filePath,omitempty"`
	StartLine  int               `json:"startLine,omitempty"`
	EndLine    int               `json:"endLine,omitempty"`
	Signature  string            `json:"signature,omitempty"`
	Doc        string            `json:"doc,omitempty"`
	Receiver   string            `json:"receiver,omitempty"` // for Method nodes
	Alias      string            `json:"alias,omitempty"`    // for Import nodes
	Scope      string            `json:"scope,omitempty"`    // for Variable nodes
	Checksum   string            `json:"checksum,omitempty"` // for File nodes
	Properties map[string]string `json:"properties,omitempty"`
}

// Edge represents a directed relationship between two nodes.
type Edge struct {
	FromID     string            `json:"fromId"`
	ToID       string            `json:"toId"`
	Type       EdgeType          `json:"type"`
	FromType   NodeType          `json:"fromType,omitempty"` // label of the source node — enables indexed MATCH
	ToType     NodeType          `json:"toType,omitempty"`   // label of the target node — enables indexed MATCH
	Properties map[string]string `json:"properties,omitempty"`
	// IntLists stores integer list properties (e.g., changed line ranges on HAS_COMMIT edges).
	// Values are stored as flat alternating pairs: [start1, end1, start2, end2, ...]
	IntLists   map[string][]int64 `json:"intLists,omitempty"`
}

// Graph holds all extracted nodes and edges for a repository or file.
type Graph struct {
	Nodes []*Node `json:"nodes"`
	Edges []*Edge `json:"edges"`
}

// NewGraph returns an empty Graph.
func NewGraph() *Graph {
	return &Graph{
		Nodes: make([]*Node, 0),
		Edges: make([]*Edge, 0),
	}
}

// AddNode appends a node to the graph.
func (g *Graph) AddNode(n *Node) {
	g.Nodes = append(g.Nodes, n)
}

// AddEdge appends an edge to the graph.
func (g *Graph) AddEdge(e *Edge) {
	g.Edges = append(g.Edges, e)
}

// Merge merges another graph's nodes and edges into this graph.
func (g *Graph) Merge(other *Graph) {
	g.Nodes = append(g.Nodes, other.Nodes...)
	g.Edges = append(g.Edges, other.Edges...)
}
