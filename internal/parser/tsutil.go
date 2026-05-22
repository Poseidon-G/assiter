package parser

import (
	"context"
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
)

// tsExtract is a shared helper that parses source with a Tree-sitter grammar
// and walks the resulting AST to call visitor for each node.
func tsExtract(
	source []byte,
	lang *sitter.Language,
	visitor func(n *sitter.Node, source []byte) []*RawNode,
) ([]*RawNode, error) {
	p := sitter.NewParser()
	p.SetLanguage(lang)

	tree, err := p.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse: %w", err)
	}
	defer tree.Close()

	return walkTree(tree.RootNode(), source, visitor), nil
}

// walkTree does a depth-first traversal and accumulates results from visitor.
func walkTree(root *sitter.Node, source []byte, visitor func(*sitter.Node, []byte) []*RawNode) []*RawNode {
	var results []*RawNode
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if nodes := visitor(n, source); len(nodes) > 0 {
			results = append(results, nodes...)
		}
		for i := uint32(0); i < n.ChildCount(); i++ {
			walk(n.Child(int(i)))
		}
	}
	walk(root)
	return results
}

// nodeText returns the source text spanned by a Tree-sitter node.
func nodeText(n *sitter.Node, source []byte) string {
	if n == nil {
		return ""
	}
	return string(source[n.StartByte():n.EndByte()])
}

// childByType returns the first direct child with the given node type.
func childByType(n *sitter.Node, kind string) *sitter.Node {
	for i := uint32(0); i < n.ChildCount(); i++ {
		c := n.Child(int(i))
		if c.Type() == kind {
			return c
		}
	}
	return nil
}

// childByField returns the child node for a named field.
func childByField(n *sitter.Node, field string) *sitter.Node {
	return n.ChildByFieldName(field)
}

// precedingComment looks upward in the sibling chain for a comment node.
func precedingComment(n *sitter.Node, source []byte) string {
	prev := n.PrevNamedSibling()
	if prev == nil {
		return ""
	}
	t := prev.Type()
	if t == "comment" || t == "line_comment" || t == "block_comment" ||
		t == "doc_comment" {
		return nodeText(prev, source)
	}
	return ""
}

// startLine returns 1-based line number of a node.
func startLine(n *sitter.Node) int {
	return int(n.StartPoint().Row) + 1
}

// endLine returns 1-based line number of a node.
func endLine(n *sitter.Node) int {
	return int(n.EndPoint().Row) + 1
}
