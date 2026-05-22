package parser

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/rust"
)

// RustExtractor uses Tree-sitter to parse Rust source files.
type RustExtractor struct{}

func (r *RustExtractor) Language() Language { return LangRust }

func (r *RustExtractor) Extract(filePath string, source []byte) ([]*RawNode, error) {
	return tsExtract(source, rust.GetLanguage(), rustVisitor)
}

func rustVisitor(n *sitter.Node, source []byte) []*RawNode {
	switch n.Type() {
	case "use_declaration":
		arg := childByField(n, "argument")
		name := ""
		if arg != nil {
			name = nodeText(arg, source)
		} else {
			name = nodeText(n, source)
		}
		return []*RawNode{{Kind: "import", Name: name, StartLine: startLine(n)}}

	case "mod_item":
		nameNode := childByField(n, "name")
		if nameNode == nil {
			return nil
		}
		return []*RawNode{{Kind: "package", Name: nodeText(nameNode, source), StartLine: startLine(n)}}

	case "struct_item":
		nameNode := childByField(n, "name")
		if nameNode == nil {
			return nil
		}
		doc := precedingComment(n, source)
		return []*RawNode{{
			Kind:      "struct",
			Name:      nodeText(nameNode, source),
			StartLine: startLine(n),
			EndLine:   endLine(n),
			Doc:       doc,
		}}

	case "enum_item":
		nameNode := childByField(n, "name")
		if nameNode == nil {
			return nil
		}
		doc := precedingComment(n, source)
		return []*RawNode{{
			Kind:      "struct",
			Name:      nodeText(nameNode, source),
			StartLine: startLine(n),
			EndLine:   endLine(n),
			Doc:       doc,
		}}

	case "trait_item":
		nameNode := childByField(n, "name")
		if nameNode == nil {
			return nil
		}
		doc := precedingComment(n, source)
		return []*RawNode{{
			Kind:      "interface",
			Name:      nodeText(nameNode, source),
			StartLine: startLine(n),
			EndLine:   endLine(n),
			Doc:       doc,
		}}

	case "function_item":
		nameNode := childByField(n, "name")
		if nameNode == nil {
			return nil
		}
		doc := precedingComment(n, source)
		// Inside impl block → method
		kind := "function"
		receiver := ""
		if parent := n.Parent(); parent != nil {
			if gp := parent.Parent(); gp != nil && gp.Type() == "impl_item" {
				kind = "method"
				typeNode := childByField(gp, "type")
				if typeNode != nil {
					receiver = nodeText(typeNode, source)
				}
			}
		}
		return []*RawNode{{
			Kind:      kind,
			Name:      nodeText(nameNode, source),
			StartLine: startLine(n),
			EndLine:   endLine(n),
			Doc:       doc,
			Properties: map[string]string{"receiver": receiver},
		}}
	}
	return nil
}
