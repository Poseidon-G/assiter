package parser

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/cpp"
)

// CppExtractor uses Tree-sitter to parse C/C++ source files.
type CppExtractor struct{}

func (c *CppExtractor) Language() Language { return LangCPP }

func (c *CppExtractor) Extract(filePath string, source []byte) ([]*RawNode, error) {
	return tsExtract(source, cpp.GetLanguage(), cppVisitor)
}

func cppVisitor(n *sitter.Node, source []byte) []*RawNode {
	switch n.Type() {
	case "preproc_include":
		path := childByField(n, "path")
		name := ""
		if path != nil {
			name = nodeText(path, source)
		} else {
			name = nodeText(n, source)
		}
		return []*RawNode{{Kind: "import", Name: stripAngleBrackets(name), StartLine: startLine(n)}}

	case "namespace_definition":
		nameNode := childByField(n, "name")
		if nameNode == nil {
			return nil
		}
		return []*RawNode{{Kind: "package", Name: nodeText(nameNode, source), StartLine: startLine(n)}}

	case "struct_specifier", "class_specifier":
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

	case "function_definition":
		declarator := childByField(n, "declarator")
		name := extractCppDeclName(declarator, source)
		if name == "" {
			return nil
		}
		doc := precedingComment(n, source)
		return []*RawNode{{
			Kind:      "function",
			Name:      name,
			StartLine: startLine(n),
			EndLine:   endLine(n),
			Doc:       doc,
		}}

	case "declaration":
		// Function declarations (prototypes)
		declarator := childByField(n, "declarator")
		if declarator != nil && (declarator.Type() == "function_declarator" ||
			declarator.Type() == "pointer_declarator") {
			name := extractCppDeclName(declarator, source)
			if name != "" {
				return []*RawNode{{
					Kind:      "function",
					Name:      name,
					StartLine: startLine(n),
				}}
			}
		}
	}
	return nil
}

// extractCppDeclName digs into a declarator node to find the function name.
func extractCppDeclName(decl *sitter.Node, source []byte) string {
	if decl == nil {
		return ""
	}
	switch decl.Type() {
	case "function_declarator":
		inner := childByField(decl, "declarator")
		return extractCppDeclName(inner, source)
	case "pointer_declarator":
		inner := childByField(decl, "declarator")
		return extractCppDeclName(inner, source)
	case "qualified_identifier":
		// Foo::bar → bar
		name := childByField(decl, "name")
		if name != nil {
			return nodeText(name, source)
		}
		return nodeText(decl, source)
	case "identifier":
		return nodeText(decl, source)
	}
	return ""
}

func stripAngleBrackets(s string) string {
	if len(s) >= 2 && s[0] == '<' && s[len(s)-1] == '>' {
		return s[1 : len(s)-1]
	}
	return stripQuotes(s)
}
