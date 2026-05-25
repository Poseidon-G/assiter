// Package normalizer converts raw parse results into UModel graph nodes and edges.
package normalizer

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/quyenluc/assiter/internal/parser"
	"github.com/quyenluc/assiter/internal/umodel"
)

// Normalizer transforms ParseResults into a UModel Graph.
type Normalizer struct{}

// New returns a Normalizer instance.
func New() *Normalizer { return &Normalizer{} }

// Normalize converts a single ParseResult into a Graph fragment.
func (n *Normalizer) Normalize(result *parser.ParseResult) *umodel.Graph {
	g := umodel.NewGraph()

	fileID := nodeID("File", result.FilePath)
	fileNode := &umodel.Node{
		ID:       fileID,
		Type:     umodel.NodeFile,
		Language: string(result.Language),
		Name:     filepath.Base(result.FilePath),
		FilePath: result.FilePath,
		Checksum: checksum(result.Source),
	}
	g.AddNode(fileNode)

	// helper — create a file→X edge with correct labels on both sides
	fileEdge := func(toID string, toType umodel.NodeType, rel umodel.EdgeType) *umodel.Edge {
		return &umodel.Edge{FromID: fileID, ToID: toID, Type: rel,
			FromType: umodel.NodeFile, ToType: toType}
	}

	var packageID string
	var packageType umodel.NodeType = umodel.NodePackage

	for _, raw := range result.Nodes {
		switch raw.Kind {
		case "package":
			packageID = nodeID("Package", raw.Name)
			g.AddNode(&umodel.Node{
				ID:       packageID,
				Type:     umodel.NodePackage,
				Language: string(result.Language),
				Name:     raw.Name,
				FilePath: result.FilePath,
			})
			g.AddEdge(fileEdge(packageID, umodel.NodePackage, umodel.EdgeContains))

		case "import":
			alias := raw.Properties["alias"]
			impID := nodeID("Import", result.FilePath+":"+raw.Name)
			g.AddNode(&umodel.Node{
				ID:        impID,
				Type:      umodel.NodeImport,
				Language:  string(result.Language),
				Name:      raw.Name,
				FilePath:  result.FilePath,
				StartLine: raw.StartLine,
				Alias:     alias,
			})
			g.AddEdge(fileEdge(impID, umodel.NodeImport, umodel.EdgeImports))

		case "struct", "enum":
			sID := nodeID("Struct", result.FilePath+":"+raw.Name)
			g.AddNode(&umodel.Node{
				ID:        sID,
				Type:      umodel.NodeStruct,
				Language:  string(result.Language),
				Name:      raw.Name,
				FilePath:  result.FilePath,
				StartLine: raw.StartLine,
				EndLine:   raw.EndLine,
				Doc:       raw.Doc,
			})
			g.AddEdge(fileEdge(sID, umodel.NodeStruct, umodel.EdgeContains))
			if packageID != "" {
				g.AddEdge(&umodel.Edge{FromID: packageID, ToID: sID, Type: umodel.EdgeDefines,
					FromType: packageType, ToType: umodel.NodeStruct})
			}

		case "interface":
			iID := nodeID("Interface", result.FilePath+":"+raw.Name)
			g.AddNode(&umodel.Node{
				ID:        iID,
				Type:      umodel.NodeInterface,
				Language:  string(result.Language),
				Name:      raw.Name,
				FilePath:  result.FilePath,
				StartLine: raw.StartLine,
				EndLine:   raw.EndLine,
				Doc:       raw.Doc,
			})
			g.AddEdge(fileEdge(iID, umodel.NodeInterface, umodel.EdgeContains))
			if packageID != "" {
				g.AddEdge(&umodel.Edge{FromID: packageID, ToID: iID, Type: umodel.EdgeDefines,
					FromType: packageType, ToType: umodel.NodeInterface})
			}

		case "function":
			fID := nodeID("Function", result.FilePath+":"+raw.Name+fmt.Sprintf(":%d", raw.StartLine))
			g.AddNode(&umodel.Node{
				ID:        fID,
				Type:      umodel.NodeFunction,
				Language:  string(result.Language),
				Name:      raw.Name,
				FilePath:  result.FilePath,
				StartLine: raw.StartLine,
				EndLine:   raw.EndLine,
				Doc:       raw.Doc,
			})
			g.AddEdge(fileEdge(fID, umodel.NodeFunction, umodel.EdgeContains))
			if packageID != "" {
				g.AddEdge(&umodel.Edge{FromID: packageID, ToID: fID, Type: umodel.EdgeDefines,
					FromType: packageType, ToType: umodel.NodeFunction})
			}
			addCallEdges(g, fID, umodel.NodeFunction, raw.Children)

		case "method":
			receiver := raw.Properties["receiver"]
			mID := nodeID("Method", result.FilePath+":"+receiver+"."+raw.Name+fmt.Sprintf(":%d", raw.StartLine))
			g.AddNode(&umodel.Node{
				ID:        mID,
				Type:      umodel.NodeMethod,
				Language:  string(result.Language),
				Name:      raw.Name,
				FilePath:  result.FilePath,
				StartLine: raw.StartLine,
				EndLine:   raw.EndLine,
				Doc:       raw.Doc,
				Receiver:  receiver,
			})
			g.AddEdge(fileEdge(mID, umodel.NodeMethod, umodel.EdgeContains))
			if receiver != "" {
				receiverID := nodeID("Struct", result.FilePath+":"+receiver)
				g.AddEdge(&umodel.Edge{FromID: receiverID, ToID: mID, Type: umodel.EdgeContains,
					FromType: umodel.NodeStruct, ToType: umodel.NodeMethod})
			}
			addCallEdges(g, mID, umodel.NodeMethod, raw.Children)

		case "variable":
			vID := nodeID("Variable", result.FilePath+":"+raw.Name+fmt.Sprintf(":%d", raw.StartLine))
			g.AddNode(&umodel.Node{
				ID:        vID,
				Type:      umodel.NodeVariable,
				Language:  string(result.Language),
				Name:      raw.Name,
				FilePath:  result.FilePath,
				StartLine: raw.StartLine,
			})
			g.AddEdge(fileEdge(vID, umodel.NodeVariable, umodel.EdgeContains))
		}
	}

	return g
}

// NormalizeAll normalizes a slice of ParseResults into a single merged Graph.
func (n *Normalizer) NormalizeAll(results []*parser.ParseResult) *umodel.Graph {
	merged := umodel.NewGraph()
	for _, r := range results {
		merged.Merge(n.Normalize(r))
	}
	return merged
}

func nodeID(nodeType, key string) string {
	h := sha256.Sum256([]byte(nodeType + ":" + key))
	return strings.ToLower(nodeType) + "_" + fmt.Sprintf("%x", h[:8])
}

// symbolID returns a stable, global ID for a named symbol (callee).
// The same name always maps to the same Symbol node across all files.
func symbolID(name string) string {
	h := sha256.Sum256([]byte("Symbol:" + name))
	return "symbol_" + fmt.Sprintf("%x", h[:8])
}

// addCallEdges creates Symbol nodes and CALLS edges for each "call" child of a function/method node.
func addCallEdges(g *umodel.Graph, callerID string, callerType umodel.NodeType, children []*parser.RawNode) {
	for _, child := range children {
		if child.Kind != "call" || child.Name == "" {
			continue
		}
		sID := symbolID(child.Name)
		g.AddNode(&umodel.Node{
			ID:   sID,
			Type: umodel.NodeSymbol,
			Name: child.Name,
		})
		g.AddEdge(&umodel.Edge{
			FromID:   callerID,
			ToID:     sID,
			Type:     umodel.EdgeCalls,
			FromType: callerType,
			ToType:   umodel.NodeSymbol,
		})
	}
}

func checksum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])
}
