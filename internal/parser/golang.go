package parser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// GoExtractor uses the standard library go/ast to parse Go source files.
type GoExtractor struct{}

func (g *GoExtractor) Language() Language { return LangGo }

func (g *GoExtractor) Extract(filePath string, source []byte) ([]*RawNode, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, source, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("go/ast parse: %w", err)
	}

	var nodes []*RawNode

	// Package node
	if f.Name != nil {
		nodes = append(nodes, &RawNode{
			Kind: "package",
			Name: f.Name.Name,
		})
	}

	// Import nodes
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		pos := fset.Position(imp.Pos())
		nodes = append(nodes, &RawNode{
			Kind:      "import",
			Name:      path,
			StartLine: pos.Line,
			Properties: map[string]string{"alias": alias},
		})
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.FuncDecl:
			node := extractGoFunc(fset, decl)
			nodes = append(nodes, node)
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					node := extractGoType(fset, decl, s)
					nodes = append(nodes, node)
				case *ast.ValueSpec:
					for _, name := range s.Names {
						pos := fset.Position(name.Pos())
						nodes = append(nodes, &RawNode{
							Kind:      "variable",
							Name:      name.Name,
							StartLine: pos.Line,
						})
					}
				}
			}
		}
		return true
	})

	return nodes, nil
}

func extractGoFunc(fset *token.FileSet, decl *ast.FuncDecl) *RawNode {
	start := fset.Position(decl.Pos())
	end := fset.Position(decl.End())

	doc := ""
	if decl.Doc != nil {
		doc = decl.Doc.Text()
	}

	kind := "function"
	receiver := ""
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		kind = "method"
		switch t := decl.Recv.List[0].Type.(type) {
		case *ast.StarExpr:
			if id, ok := t.X.(*ast.Ident); ok {
				receiver = id.Name
			}
		case *ast.Ident:
			receiver = t.Name
		}
	}

	return &RawNode{
		Kind:      kind,
		Name:      decl.Name.Name,
		StartLine: start.Line,
		EndLine:   end.Line,
		Doc:       strings.TrimSpace(doc),
		Properties: map[string]string{"receiver": receiver},
	}
}

func extractGoType(fset *token.FileSet, decl *ast.GenDecl, spec *ast.TypeSpec) *RawNode {
	start := fset.Position(spec.Pos())
	end := fset.Position(spec.End())

	doc := ""
	if decl.Doc != nil {
		doc = decl.Doc.Text()
	} else if spec.Comment != nil {
		doc = spec.Comment.Text()
	}

	kind := "struct"
	switch spec.Type.(type) {
	case *ast.InterfaceType:
		kind = "interface"
	}

	return &RawNode{
		Kind:      kind,
		Name:      spec.Name.Name,
		StartLine: start.Line,
		EndLine:   end.Line,
		Doc:       strings.TrimSpace(doc),
	}
}
