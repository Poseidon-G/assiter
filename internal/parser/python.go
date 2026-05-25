package parser

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
)

// PythonExtractor uses Tree-sitter to parse Python source files.
type PythonExtractor struct{}

func (p *PythonExtractor) Language() Language { return LangPython }

func (p *PythonExtractor) Extract(filePath string, source []byte) ([]*RawNode, error) {
	return tsExtract(source, python.GetLanguage(), pythonVisitor)
}

func pythonVisitor(n *sitter.Node, source []byte) []*RawNode {
	switch n.Type() {
	case "import_statement", "import_from_statement":
		name := nodeText(n, source)
		// Simplify: extract the module name
		if mod := childByType(n, "dotted_name"); mod != nil {
			name = nodeText(mod, source)
		} else if mod := childByField(n, "name"); mod != nil {
			name = nodeText(mod, source)
		}
		return []*RawNode{{
			Kind:      "import",
			Name:      name,
			StartLine: startLine(n),
		}}

	case "class_definition":
		nameNode := childByField(n, "name")
		if nameNode == nil {
			return nil
		}
		return []*RawNode{{
			Kind:      "struct",
			Name:      nodeText(nameNode, source),
			StartLine: startLine(n),
			EndLine:   endLine(n),
			Doc:       extractPythonDocstring(n, source),
		}}

	case "function_definition", "async_function_definition":
		nameNode := childByField(n, "name")
		if nameNode == nil {
			return nil
		}
		kind := "function"
		receiver := ""
		if parent := n.Parent(); parent != nil {
			if gp := parent.Parent(); gp != nil && gp.Type() == "class_definition" {
				kind = "method"
				if recvNode := childByField(gp, "name"); recvNode != nil {
					receiver = nodeText(recvNode, source)
				}
			}
		}

		// Collect all function calls made inside this function body.
		var callChildren []*RawNode
		if body := childByField(n, "body"); body != nil {
			for _, callee := range collectCalls(body, source) {
				callChildren = append(callChildren, &RawNode{
					Kind: "call",
					Name: callee,
				})
			}
		}

		return []*RawNode{{
			Kind:      kind,
			Name:      nodeText(nameNode, source),
			StartLine: startLine(n),
			EndLine:   endLine(n),
			Doc:       extractPythonDocstring(n, source),
			Properties: map[string]string{"receiver": receiver},
			Children:  callChildren,
		}}
	}
	return nil
}

// extractPythonDocstring returns the first string literal inside a function/class body.
func extractPythonDocstring(n *sitter.Node, source []byte) string {
	body := childByField(n, "body")
	if body == nil {
		return ""
	}
	for i := uint32(0); i < body.ChildCount(); i++ {
		child := body.Child(int(i))
		if child.Type() == "expression_statement" {
			for j := uint32(0); j < child.ChildCount(); j++ {
				c := child.Child(int(j))
				if c.Type() == "string" {
					return nodeText(c, source)
				}
			}
		}
		break // only check first statement
	}
	return ""
}
