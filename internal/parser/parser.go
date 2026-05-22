// Package parser provides Tree-sitter based AST parsing for multiple languages.
package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Language represents a supported programming language.
type Language string

const (
	LangGo         Language = "go"
	LangPython     Language = "python"
	LangTypeScript Language = "typescript"
	LangJava       Language = "java"
	LangRust       Language = "rust"
	LangCPP        Language = "cpp"
)

// RawNode is a language-agnostic representation of a parsed AST node
// before normalization.
type RawNode struct {
	Kind       string
	Name       string
	Text       string
	StartLine  int
	EndLine    int
	StartCol   int
	EndCol     int
	Doc        string
	Children   []*RawNode
	Properties map[string]string
}

// ParseResult holds raw nodes extracted from a single file.
type ParseResult struct {
	FilePath string
	Language Language
	Nodes    []*RawNode
	Source   []byte
}

// Extractor is the interface each language-specific parser must implement.
type Extractor interface {
	Language() Language
	Extract(filePath string, source []byte) ([]*RawNode, error)
}

// Parser dispatches parsing to the correct language extractor.
type Parser struct {
	extractors map[Language]Extractor
}

// New creates a Parser with all built-in language extractors registered.
func New(enabled []string) *Parser {
	p := &Parser{extractors: make(map[Language]Extractor)}

	all := map[Language]Extractor{
		LangGo:         &GoExtractor{},
		LangPython:     &PythonExtractor{},
		LangTypeScript: &TypeScriptExtractor{},
		LangJava:       &JavaExtractor{},
		LangRust:       &RustExtractor{},
		LangCPP:        &CppExtractor{},
	}

	for _, name := range enabled {
		lang := Language(strings.ToLower(name))
		if ext, ok := all[lang]; ok {
			p.extractors[lang] = ext
		}
	}
	return p
}

// ParseFile parses a single file and returns the raw parse result.
func (p *Parser) ParseFile(filePath string) (*ParseResult, error) {
	lang := detectLanguage(filePath)
	if lang == "" {
		return nil, fmt.Errorf("unsupported file type: %s", filePath)
	}

	ext, ok := p.extractors[lang]
	if !ok {
		return nil, fmt.Errorf("language %q not enabled", lang)
	}

	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", filePath, err)
	}

	nodes, err := ext.Extract(filePath, source)
	if err != nil {
		return nil, fmt.Errorf("extracting %s: %w", filePath, err)
	}

	return &ParseResult{
		FilePath: filePath,
		Language: lang,
		Nodes:    nodes,
		Source:   source,
	}, nil
}

// ParseDir recursively parses all supported files under dir.
func (p *Parser) ParseDir(dir string, exclude []string) ([]*ParseResult, error) {
	var results []*ParseResult

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			for _, ex := range exclude {
				if d.Name() == ex {
					return filepath.SkipDir
				}
			}
			return nil
		}

		result, err := p.ParseFile(path)
		if err != nil {
			// skip unsupported or unparseable files
			return nil
		}
		results = append(results, result)
		return nil
	})

	return results, err
}

// detectLanguage infers the Language from the file extension.
func detectLanguage(path string) Language {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return LangGo
	case ".py":
		return LangPython
	case ".ts", ".tsx":
		return LangTypeScript
	case ".java":
		return LangJava
	case ".rs":
		return LangRust
	case ".cpp", ".cc", ".cxx", ".c", ".h", ".hpp":
		return LangCPP
	default:
		return ""
	}
}
