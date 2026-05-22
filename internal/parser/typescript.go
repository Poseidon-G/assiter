package parser

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// TypeScriptExtractor uses Tree-sitter to parse TypeScript/TSX source files.
type TypeScriptExtractor struct{}

func (t *TypeScriptExtractor) Language() Language { return LangTypeScript }

func (t *TypeScriptExtractor) Extract(filePath string, source []byte) ([]*RawNode, error) {
	return tsExtract(source, typescript.GetLanguage(), tsVisitor)
}

func tsVisitor(n *sitter.Node, source []byte) []*RawNode {
	switch n.Type() {
	case "import_statement":
		src := childByField(n, "source")
		name := ""
		if src != nil {
			name = nodeText(src, source)
		} else {
			name = nodeText(n, source)
		}
		return []*RawNode{{Kind: "import", Name: stripQuotes(name), StartLine: startLine(n)}}

	case "class_declaration":
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

	case "interface_declaration":
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

	case "function_declaration":
		nameNode := childByField(n, "name")
		if nameNode == nil {
			return nil
		}
		doc := precedingComment(n, source)
		return []*RawNode{{
			Kind:      "function",
			Name:      nodeText(nameNode, source),
			StartLine: startLine(n),
			EndLine:   endLine(n),
			Doc:       doc,
		}}

	case "method_definition":
		nameNode := childByField(n, "name")
		if nameNode == nil {
			return nil
		}
		receiver := ""
		if parent := n.Parent(); parent != nil {
			if gp := parent.Parent(); gp != nil && gp.Type() == "class_declaration" {
				if rn := childByField(gp, "name"); rn != nil {
					receiver = nodeText(rn, source)
				}
			}
		}
		return []*RawNode{{
			Kind:      "method",
			Name:      nodeText(nameNode, source),
			StartLine: startLine(n),
			EndLine:   endLine(n),
			Properties: map[string]string{"receiver": receiver},
		}}

	case "lexical_declaration", "variable_declaration":
		// Arrow function: const foo = (...) => ...
		for i := uint32(0); i < n.ChildCount(); i++ {
			child := n.Child(int(i))
			if child.Type() == "variable_declarator" {
				val := childByField(child, "value")
				if val != nil && (val.Type() == "arrow_function" || val.Type() == "function") {
					nameNode := childByField(child, "name")
					if nameNode != nil {
						doc := precedingComment(n, source)
						return []*RawNode{{
							Kind:      "function",
							Name:      nodeText(nameNode, source),
							StartLine: startLine(n),
							EndLine:   endLine(n),
							Doc:       doc,
						}}
					}
				}
			}
		}
	}
	return nil
}

func stripQuotes(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'' || s[0] == '`') {
		return s[1 : len(s)-1]
	}
	return s
}
