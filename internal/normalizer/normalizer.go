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

	var packageID string

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
			g.AddEdge(&umodel.Edge{
				FromID: fileID,
				ToID:   packageID,
				Type:   umodel.EdgeContains,
			})

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
			g.AddEdge(&umodel.Edge{FromID: fileID, ToID: impID, Type: umodel.EdgeImports})

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
			g.AddEdge(&umodel.Edge{FromID: fileID, ToID: sID, Type: umodel.EdgeContains})
			if packageID != "" {
				g.AddEdge(&umodel.Edge{FromID: packageID, ToID: sID, Type: umodel.EdgeDefines})
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
			g.AddEdge(&umodel.Edge{FromID: fileID, ToID: iID, Type: umodel.EdgeContains})
			if packageID != "" {
				g.AddEdge(&umodel.Edge{FromID: packageID, ToID: iID, Type: umodel.EdgeDefines})
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
			g.AddEdge(&umodel.Edge{FromID: fileID, ToID: fID, Type: umodel.EdgeContains})
			if packageID != "" {
				g.AddEdge(&umodel.Edge{FromID: packageID, ToID: fID, Type: umodel.EdgeDefines})
			}

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
			g.AddEdge(&umodel.Edge{FromID: fileID, ToID: mID, Type: umodel.EdgeContains})
			// Link method to its receiver struct if it exists in this graph
			if receiver != "" {
				receiverID := nodeID("Struct", result.FilePath+":"+receiver)
				g.AddEdge(&umodel.Edge{FromID: receiverID, ToID: mID, Type: umodel.EdgeContains})
			}

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
			g.AddEdge(&umodel.Edge{FromID: fileID, ToID: vID, Type: umodel.EdgeContains})
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

func checksum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])
}
