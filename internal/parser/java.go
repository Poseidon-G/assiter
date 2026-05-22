package parser

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/java"
)

// JavaExtractor uses Tree-sitter to parse Java source files.
type JavaExtractor struct{}

func (j *JavaExtractor) Language() Language { return LangJava }

func (j *JavaExtractor) Extract(filePath string, source []byte) ([]*RawNode, error) {
	return tsExtract(source, java.GetLanguage(), javaVisitor)
}

func javaVisitor(n *sitter.Node, source []byte) []*RawNode {
	switch n.Type() {
	case "package_declaration":
		// package com.example.app;
		for i := uint32(0); i < n.ChildCount(); i++ {
			c := n.Child(int(i))
			if c.Type() == "scoped_identifier" || c.Type() == "identifier" {
				return []*RawNode{{Kind: "package", Name: nodeText(c, source), StartLine: startLine(n)}}
			}
		}

	case "import_declaration":
		for i := uint32(0); i < n.ChildCount(); i++ {
			c := n.Child(int(i))
			if c.Type() == "scoped_identifier" || c.Type() == "identifier" {
				return []*RawNode{{Kind: "import", Name: nodeText(c, source), StartLine: startLine(n)}}
			}
		}

	case "class_declaration", "enum_declaration":
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

	case "method_declaration", "constructor_declaration":
		nameNode := childByField(n, "name")
		if nameNode == nil {
			return nil
		}
		receiver := ""
		if parent := n.Parent(); parent != nil {
			if gp := parent.Parent(); gp != nil &&
				(gp.Type() == "class_declaration" || gp.Type() == "enum_declaration") {
				if rn := childByField(gp, "name"); rn != nil {
					receiver = nodeText(rn, source)
				}
			}
		}
		doc := precedingComment(n, source)
		return []*RawNode{{
			Kind:      "method",
			Name:      nodeText(nameNode, source),
			StartLine: startLine(n),
			EndLine:   endLine(n),
			Doc:       doc,
			Properties: map[string]string{"receiver": receiver},
		}}
	}
	return nil
}
