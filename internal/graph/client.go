// Package graph provides Neo4j client and schema management for the knowledge graph.
package graph

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/quyenluc/assiter/internal/umodel"
	"github.com/quyenluc/assiter/pkg/config"
)

// Client wraps a Neo4j driver and provides graph operations.
type Client struct {
	driver neo4j.DriverWithContext
	db     string
}

// New creates a new Neo4j Client and verifies connectivity.
func New(cfg config.Neo4jConfig) (*Client, error) {
	driver, err := neo4j.NewDriverWithContext(
		cfg.URI,
		neo4j.BasicAuth(cfg.Username, cfg.Password, ""),
	)
	if err != nil {
		return nil, fmt.Errorf("creating neo4j driver: %w", err)
	}
	if err := driver.VerifyConnectivity(context.Background()); err != nil {
		return nil, fmt.Errorf("neo4j connectivity: %w", err)
	}
	return &Client{driver: driver, db: cfg.Database}, nil
}

// Close closes the underlying driver.
func (c *Client) Close(ctx context.Context) error {
	return c.driver.Close(ctx)
}

// EnsureSchema creates indexes and constraints required by the graph schema.
func (c *Client) EnsureSchema(ctx context.Context) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.db})
	defer session.Close(ctx)

	constraints := []string{
		"CREATE CONSTRAINT IF NOT EXISTS FOR (n:File) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (n:Package) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (n:Function) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (n:Method) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (n:Struct) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (n:Interface) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (n:Variable) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (n:Import) REQUIRE n.id IS UNIQUE",

		"CREATE INDEX IF NOT EXISTS FOR (n:Function) ON (n.name)",
		"CREATE INDEX IF NOT EXISTS FOR (n:Method) ON (n.name)",
		"CREATE INDEX IF NOT EXISTS FOR (n:Struct) ON (n.name)",
		"CREATE INDEX IF NOT EXISTS FOR (n:Interface) ON (n.name)",
	}

	for _, cql := range constraints {
		if _, err := session.Run(ctx, cql, nil); err != nil {
			return fmt.Errorf("schema: %w", err)
		}
	}
	return nil
}

// UpsertGraph writes all nodes and edges from a UModel Graph into Neo4j.
// Uses MERGE to support incremental updates.
func (c *Client) UpsertGraph(ctx context.Context, g *umodel.Graph) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.db})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		for _, node := range g.Nodes {
			if err := upsertNode(ctx, tx, node); err != nil {
				return nil, err
			}
		}
		for _, edge := range g.Edges {
			if err := upsertEdge(ctx, tx, edge); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	return err
}

func upsertNode(ctx context.Context, tx neo4j.ManagedTransaction, n *umodel.Node) error {
	label := string(n.Type)
	cql := fmt.Sprintf(`
		MERGE (x:%s {id: $id})
		SET x += $props
	`, label)

	props := map[string]any{
		"name":      n.Name,
		"language":  n.Language,
		"filePath":  n.FilePath,
		"startLine": n.StartLine,
		"endLine":   n.EndLine,
		"doc":       n.Doc,
		"signature": n.Signature,
		"receiver":  n.Receiver,
		"alias":     n.Alias,
		"checksum":  n.Checksum,
	}

	_, err := tx.Run(ctx, cql, map[string]any{"id": n.ID, "props": props})
	return err
}

func upsertEdge(ctx context.Context, tx neo4j.ManagedTransaction, e *umodel.Edge) error {
	cql := fmt.Sprintf(`
		MATCH (a {id: $fromId}), (b {id: $toId})
		MERGE (a)-[r:%s]->(b)
	`, string(e.Type))

	_, err := tx.Run(ctx, cql, map[string]any{
		"fromId": e.FromID,
		"toId":   e.ToID,
	})
	return err
}

// SearchNodes finds nodes by name (case-insensitive, partial match).
func (c *Client) SearchNodes(ctx context.Context, name string, nodeTypes []string) ([]*umodel.Node, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.db})
	defer session.Close(ctx)

	cql := `
		MATCH (n)
		WHERE toLower(n.name) CONTAINS toLower($name)
		RETURN n.id AS id, labels(n)[0] AS type, n.name AS name,
		       n.language AS language, n.filePath AS filePath,
		       n.startLine AS startLine, n.doc AS doc
		LIMIT 50
	`
	result, err := session.Run(ctx, cql, map[string]any{"name": name})
	if err != nil {
		return nil, err
	}

	var nodes []*umodel.Node
	for result.Next(ctx) {
		rec := result.Record()
		node := recordToNode(rec)
		nodes = append(nodes, node)
	}
	return nodes, result.Err()
}

// GetNodeWithNeighbors returns a node and its immediate neighbors.
func (c *Client) GetNodeWithNeighbors(ctx context.Context, id string) (*NodeWithNeighbors, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.db})
	defer session.Close(ctx)

	cql := `
		MATCH (n {id: $id})
		OPTIONAL MATCH (n)-[r]->(m)
		RETURN n, collect({rel: type(r), node: m}) AS neighbors
	`
	result, err := session.Run(ctx, cql, map[string]any{"id": id})
	if err != nil {
		return nil, err
	}

	if result.Next(ctx) {
		rec := result.Record()
		nodeVal, _ := rec.Get("n")
		n, _ := nodeVal.(neo4j.Node)
		center := neo4jNodeToUModel(n)

		var neighbors []NeighborEntry
		if nbs, ok := rec.Get("neighbors"); ok {
			if list, ok := nbs.([]any); ok {
				for _, item := range list {
					if m, ok := item.(map[string]any); ok {
						rel, _ := m["rel"].(string)
						if nbNode, ok := m["node"].(neo4j.Node); ok {
							neighbors = append(neighbors, NeighborEntry{
								Relationship: rel,
								Node:         neo4jNodeToUModel(nbNode),
							})
						}
					}
				}
			}
		}
		return &NodeWithNeighbors{Center: center, Neighbors: neighbors}, nil
	}
	return nil, fmt.Errorf("node %q not found", id)
}

// Stats returns basic graph statistics.
func (c *Client) Stats(ctx context.Context) (map[string]int64, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.db})
	defer session.Close(ctx)

	cql := `
		MATCH (n)
		RETURN labels(n)[0] AS label, count(n) AS cnt
		ORDER BY label
	`
	result, err := session.Run(ctx, cql, nil)
	if err != nil {
		return nil, err
	}

	stats := make(map[string]int64)
	for result.Next(ctx) {
		rec := result.Record()
		label, _ := rec.Get("label")
		cnt, _ := rec.Get("cnt")
		if l, ok := label.(string); ok {
			if c, ok := cnt.(int64); ok {
				stats[l] = c
			}
		}
	}
	return stats, result.Err()
}

// NodeWithNeighbors pairs a center node with its outgoing neighbors.
type NodeWithNeighbors struct {
	Center    *umodel.Node    `json:"center"`
	Neighbors []NeighborEntry `json:"neighbors"`
}

// NeighborEntry pairs a relationship type with a neighboring node.
type NeighborEntry struct {
	Relationship string       `json:"relationship"`
	Node         *umodel.Node `json:"node"`
}

func recordToNode(rec *neo4j.Record) *umodel.Node {
	get := func(key string) string {
		v, _ := rec.Get(key)
		if v == nil {
			return ""
		}
		return fmt.Sprintf("%v", v)
	}
	startLine := 0
	if v, ok := rec.Get("startLine"); ok && v != nil {
		if i, ok := v.(int64); ok {
			startLine = int(i)
		}
	}
	return &umodel.Node{
		ID:        get("id"),
		Type:      umodel.NodeType(get("type")),
		Name:      get("name"),
		Language:  get("language"),
		FilePath:  get("filePath"),
		StartLine: startLine,
		Doc:       get("doc"),
	}
}

func neo4jNodeToUModel(n neo4j.Node) *umodel.Node {
	get := func(key string) string {
		if v, ok := n.Props[key]; ok && v != nil {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}
	startLine := 0
	if v, ok := n.Props["startLine"]; ok {
		if i, ok := v.(int64); ok {
			startLine = int(i)
		}
	}
	nodeType := ""
	if len(n.Labels) > 0 {
		nodeType = n.Labels[0]
	}
	return &umodel.Node{
		ID:        get("id"),
		Type:      umodel.NodeType(nodeType),
		Name:      get("name"),
		Language:  get("language"),
		FilePath:  get("filePath"),
		StartLine: startLine,
		Doc:       get("doc"),
	}
}
