// Package graph provides Neo4j client and schema management for the knowledge graph.
package graph

import (
	"context"
	"fmt"
	"log/slog"

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

		"CREATE CONSTRAINT IF NOT EXISTS FOR (n:Symbol) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (n:Commit) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (n:Ticket) REQUIRE n.id IS UNIQUE",

		"CREATE INDEX IF NOT EXISTS FOR (n:Symbol) ON (n.name)",
		"CREATE INDEX IF NOT EXISTS FOR (n:Method) ON (n.name)",
		"CREATE INDEX IF NOT EXISTS FOR (n:Struct) ON (n.name)",
		"CREATE INDEX IF NOT EXISTS FOR (n:Interface) ON (n.name)",
		"CREATE INDEX IF NOT EXISTS FOR (n:Ticket) ON (n.name)",
		"CREATE INDEX IF NOT EXISTS FOR (n:Commit) ON (n.date)",
	}

	for _, cql := range constraints {
		if _, err := session.Run(ctx, cql, nil); err != nil {
			return fmt.Errorf("schema: %w", err)
		}
	}
	return nil
}

// GetFileChecksums returns a map of filePath → checksum for all File nodes
// currently stored in Neo4j. Used by the pipeline to skip unchanged files.
func (c *Client) GetFileChecksums(ctx context.Context) (map[string]string, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.db})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		`MATCH (f:File) WHERE f.filePath IS NOT NULL AND f.checksum IS NOT NULL
		 RETURN f.filePath AS path, f.checksum AS checksum`,
		nil,
	)
	if err != nil {
		return nil, err
	}

	checksums := make(map[string]string)
	for result.Next(ctx) {
		rec := result.Record()
		path, _ := rec.Get("path")
		cs, _ := rec.Get("checksum")
		if p, ok := path.(string); ok {
			if h, ok := cs.(string); ok {
				checksums[p] = h
			}
		}
	}
	return checksums, result.Err()
}


// UpsertGraph writes all nodes and edges from a UModel Graph into Neo4j.
// Uses UNWIND batch writes (500 per tx) to handle large graphs efficiently.
func (c *Client) UpsertGraph(ctx context.Context, g *umodel.Graph) error {
	slog.Info("writing nodes to Neo4j", "count", len(g.Nodes))
	if err := c.upsertNodesBatched(ctx, g.Nodes); err != nil {
		return fmt.Errorf("upserting nodes: %w", err)
	}
	slog.Info("nodes written, writing edges", "count", len(g.Edges))
	if err := c.upsertEdgesBatched(ctx, g.Edges); err != nil {
		return fmt.Errorf("upserting edges: %w", err)
	}
	return nil
}

const batchSize = 500

func (c *Client) upsertNodesBatched(ctx context.Context, nodes []*umodel.Node) error {
	// Group ALL nodes by label up-front; batch within each label group.
	byLabel := make(map[string][]*umodel.Node)
	for _, n := range nodes {
		byLabel[string(n.Type)] = append(byLabel[string(n.Type)], n)
	}

	for label, group := range byLabel {
		total := len(group)
		for i := 0; i < total; i += batchSize {
			end := i + batchSize
			if end > total {
				end = total
			}
			rows := make([]map[string]any, 0, end-i)
			for _, n := range group[i:end] {
				row := map[string]any{
					"id":        n.ID,
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
				// Merge extra properties (e.g. Commit hash/date/author/message) as top-level fields.
				for k, v := range n.Properties {
					row[k] = v
				}
				rows = append(rows, row)
			}

			// ON CREATE: write all props for new nodes.
			// ON MATCH:  skip write when checksum unchanged (file content didn't change).
			cql := fmt.Sprintf(`
				UNWIND $rows AS row
				MERGE (x:%s {id: row.id})
				ON CREATE SET x = row
				ON MATCH SET x += CASE WHEN x.checksum <> row.checksum THEN row ELSE {} END
			`, label)

			session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.db})
			_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
				_, err := tx.Run(ctx, cql, map[string]any{"rows": rows})
				return nil, err
			})
			session.Close(ctx)
			if err != nil {
				return fmt.Errorf("nodes label=%s batch=%d/%d: %w", label, i/batchSize+1, (total+batchSize-1)/batchSize, err)
			}
			slog.Info("nodes batch done", "label", label,
				"batch", fmt.Sprintf("%d/%d", i/batchSize+1, (total+batchSize-1)/batchSize))
		}
	}
	return nil
}

func (c *Client) upsertEdgesBatched(ctx context.Context, edges []*umodel.Edge) error {
	// Group by (edgeType, fromLabel, toLabel) so MATCH can use constraint indexes.
	type edgeKey struct{ edgeType, fromType, toType string }
	byKey := make(map[edgeKey][]*umodel.Edge)
	for _, e := range edges {
		k := edgeKey{string(e.Type), string(e.FromType), string(e.ToType)}
		byKey[k] = append(byKey[k], e)
	}

	for k, group := range byKey {
		total := len(group)

		// Use labeled MATCH when type is known — this hits the constraint index.
		fromMatch := "a"
		if k.fromType != "" {
			fromMatch = fmt.Sprintf("a:%s", k.fromType)
		}
		toMatch := "b"
		if k.toType != "" {
			toMatch = fmt.Sprintf("b:%s", k.toType)
		}
		cql := fmt.Sprintf(`
			UNWIND $rows AS row
			MATCH (%s {id: row.fromId}), (%s {id: row.toId})
			MERGE (a)-[r:%s]->(b)
			ON CREATE SET r = row.props
			ON MATCH SET r += row.props
		`, fromMatch, toMatch, k.edgeType)

		for i := 0; i < total; i += batchSize {
			end := i + batchSize
			if end > total {
				end = total
			}
			rows := make([]map[string]any, 0, end-i)
			for _, e := range group[i:end] {
				props := map[string]any{}
				for k, v := range e.Properties {
					props[k] = v
				}
				for k, v := range e.IntLists {
					props[k] = v
				}
				rows = append(rows, map[string]any{
					"fromId": e.FromID,
					"toId":   e.ToID,
					"props":  props,
				})
			}

			session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.db})
			_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
				_, err := tx.Run(ctx, cql, map[string]any{"rows": rows})
				return nil, err
			})
			session.Close(ctx)
			if err != nil {
				return fmt.Errorf("edges type=%s from=%s to=%s batch=%d: %w",
					k.edgeType, k.fromType, k.toType, i/batchSize+1, err)
			}
			slog.Info("edges batch done",
				"type", k.edgeType,
				"from", k.fromType, "to", k.toType,
				"batch", fmt.Sprintf("%d/%d", i/batchSize+1, (total+batchSize-1)/batchSize),
			)
		}
	}
	return nil
}

// GetFileStatus returns the stored checksum + node counts for a specific file path.
// Returns nil if the file has not been ingested yet.
func (c *Client) GetFileStatus(ctx context.Context, filePath string) (*FileStatus, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.db})
	defer session.Close(ctx)

	// Get file node
	result, err := session.Run(ctx,
		`MATCH (f:File {filePath: $path})
		 OPTIONAL MATCH (f)-[]->(child)
		 RETURN f.filePath AS path, f.checksum AS checksum, f.name AS name,
		        count(child) AS childCount`,
		map[string]any{"path": filePath},
	)
	if err != nil {
		return nil, err
	}
	if !result.Next(ctx) {
		return nil, nil // not ingested
	}
	rec := result.Record()

	get := func(k string) string {
		v, _ := rec.Get(k)
		if v == nil {
			return ""
		}
		return fmt.Sprintf("%v", v)
	}
	childCount := int64(0)
	if v, ok := rec.Get("childCount"); ok {
		if n, ok := v.(int64); ok {
			childCount = n
		}
	}

	return &FileStatus{
		FilePath:   get("path"),
		Name:       get("name"),
		Checksum:   get("checksum"),
		ChildNodes: int(childCount),
	}, result.Err()
}

// FileStatus holds what the graph knows about an ingested source file.
type FileStatus struct {
	FilePath   string `json:"filePath"`
	Name       string `json:"name"`
	Checksum   string `json:"checksum"`
	ChildNodes int    `json:"childNodes"` // functions, methods, structs, etc. linked from this file
}

// SearchCallers finds all functions/methods that call a symbol matching the given name.
func (c *Client) SearchCallers(ctx context.Context, name string) ([]*CallerEntry, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.db})
	defer session.Close(ctx)

	cql := `
		MATCH (caller)-[:CALLS]->(sym:Symbol)
		WHERE toLower(sym.name) CONTAINS toLower($name)
		RETURN caller.id AS id, labels(caller)[0] AS type, caller.name AS name,
		       caller.language AS language, caller.filePath AS filePath,
		       caller.startLine AS startLine, sym.name AS callee
		ORDER BY filePath, startLine
		LIMIT 100
	`
	result, err := session.Run(ctx, cql, map[string]any{"name": name})
	if err != nil {
		return nil, err
	}

	var callers []*CallerEntry
	for result.Next(ctx) {
		rec := result.Record()
		get := func(k string) string {
			v, _ := rec.Get(k)
			if v == nil {
				return ""
			}
			return fmt.Sprintf("%v", v)
		}
		startLine := 0
		if v, ok := rec.Get("startLine"); ok {
			if i, ok := v.(int64); ok {
				startLine = int(i)
			}
		}
		callers = append(callers, &CallerEntry{
			Node: &umodel.Node{
				ID:        get("id"),
				Type:      umodel.NodeType(get("type")),
				Name:      get("name"),
				Language:  get("language"),
				FilePath:  get("filePath"),
				StartLine: startLine,
			},
			Callee: get("callee"),
		})
	}
	return callers, result.Err()
}

// CallerEntry pairs a caller node with the symbol name it calls.
type CallerEntry struct {
	Node   *umodel.Node `json:"node"`
	Callee string       `json:"callee"`
}

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

// GetFileContext returns all symbols (functions, methods, structs, interfaces, variables)
// defined inside a given file path, ordered by start line.
func (c *Client) GetFileContext(ctx context.Context, filePath string) ([]*umodel.Node, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.db})
	defer session.Close(ctx)

	cql := `
		MATCH (f:File {filePath: $path})-[]->(n)
		WHERE n.filePath = $path AND NOT n:File
		RETURN n.id AS id, labels(n)[0] AS type, n.name AS name,
		       n.language AS language, n.filePath AS filePath,
		       n.startLine AS startLine, n.doc AS doc
		ORDER BY n.startLine
	`
	result, err := session.Run(ctx, cql, map[string]any{"path": filePath})
	if err != nil {
		return nil, err
	}
	var nodes []*umodel.Node
	for result.Next(ctx) {
		nodes = append(nodes, recordToNode(result.Record()))
	}
	return nodes, result.Err()
}

// ── Visualization types ────────────────────────────────────────────────────

// VizNode is a node shaped for vis-network rendering.
type VizNode struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Group    string `json:"group"`
	Title    string `json:"title"`
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
}

// VizEdge is an edge shaped for vis-network rendering.
type VizEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
}

// VizGraph is the payload consumed by the vis-network UI.
type VizGraph struct {
	Nodes []*VizNode `json:"nodes"`
	Edges []*VizEdge `json:"edges"`
}

// GetSubgraph returns a vis-network subgraph centred on a name search (depth=1).
func (c *Client) GetSubgraph(ctx context.Context, name string) (*VizGraph, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.db})
	defer session.Close(ctx)

	cql := `
		MATCH (n)
		WHERE toLower(n.name) CONTAINS toLower($name)
		WITH n LIMIT 20
		OPTIONAL MATCH (n)-[r]->(m)
		RETURN n, collect({rel: type(r), node: m}) AS outs
	`
	result, err := session.Run(ctx, cql, map[string]any{"name": name})
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	vg := &VizGraph{}

	addVizNode := func(n neo4j.Node) {
		u := neo4jNodeToUModel(n)
		if u.ID == "" || seen[u.ID] {
			return
		}
		seen[u.ID] = true
		label := u.Name
		if label == "" {
			label = u.ID
		}
		vg.Nodes = append(vg.Nodes, &VizNode{
			ID:       u.ID,
			Label:    label,
			Group:    string(u.Type),
			Title:    fmt.Sprintf("%s\n%s:%d", u.Type, u.FilePath, u.StartLine),
			FilePath: u.FilePath,
			Line:     u.StartLine,
		})
	}

	for result.Next(ctx) {
		rec := result.Record()
		nv, _ := rec.Get("n")
		n, ok := nv.(neo4j.Node)
		if !ok {
			continue
		}
		addVizNode(n)
		centerID := fmt.Sprintf("%v", n.Props["id"])

		if outs, ok := rec.Get("outs"); ok {
			if list, ok := outs.([]any); ok {
				for _, item := range list {
					if m, ok := item.(map[string]any); ok {
						rel, _ := m["rel"].(string)
						if nbNode, ok := m["node"].(neo4j.Node); ok {
							addVizNode(nbNode)
							toID := fmt.Sprintf("%v", nbNode.Props["id"])
							if centerID != "" && toID != "" && toID != "<nil>" {
								vg.Edges = append(vg.Edges, &VizEdge{From: centerID, To: toID, Label: rel})
							}
						}
					}
				}
			}
		}
	}
	return vg, result.Err()
}

// GetNodeSubgraph returns a vis-network subgraph for a single node and its neighbours.
func (c *Client) GetNodeSubgraph(ctx context.Context, id string) (*VizGraph, error) {
	nwn, err := c.GetNodeWithNeighbors(ctx, id)
	if err != nil {
		return nil, err
	}
	vg := &VizGraph{}
	center := nwn.Center
	vg.Nodes = append(vg.Nodes, &VizNode{
		ID:       center.ID,
		Label:    center.Name,
		Group:    string(center.Type),
		Title:    fmt.Sprintf("%s\n%s:%d", center.Type, center.FilePath, center.StartLine),
		FilePath: center.FilePath,
		Line:     center.StartLine,
	})
	for _, nb := range nwn.Neighbors {
		n := nb.Node
		if n == nil || n.ID == "" {
			continue
		}
		vg.Nodes = append(vg.Nodes, &VizNode{
			ID:       n.ID,
			Label:    n.Name,
			Group:    string(n.Type),
			Title:    fmt.Sprintf("%s\n%s:%d", n.Type, n.FilePath, n.StartLine),
			FilePath: n.FilePath,
			Line:     n.StartLine,
		})
		vg.Edges = append(vg.Edges, &VizEdge{From: center.ID, To: n.ID, Label: nb.Relationship})
	}
	return vg, nil
}

// ── Git history queries ────────────────────────────────────────────────────

// TicketImpact describes all functions/files touched by a ticket.
type TicketImpact struct {
	TicketID string         `json:"ticketId"`
	Commits  []*CommitInfo  `json:"commits"`
	Files    []*umodel.Node `json:"files"`
	// Functions in files touched by this ticket (via File→Function edges).
	Functions []*umodel.Node `json:"functions"`
}

// CommitInfo is a lightweight commit summary for API responses.
type CommitInfo struct {
	Hash    string `json:"hash"`
	Date    string `json:"date"`
	Author  string `json:"author"`
	Message string `json:"message"`
}

// SearchByTicket searches commits by keyword — matches ticket IDs in the structured
// Ticket nodes AND does a full-text substring match on commit messages directly.
// This handles commits that mention "3581" as a ticket ID as well as commits whose
// message simply contains a keyword but no structured ticket ID was extracted.
func (c *Client) SearchByTicket(ctx context.Context, keyword string) (*TicketImpact, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.db})
	defer session.Close(ctx)

	// Union approach: collect commits via ticket nodes OR direct message match.
	// Using a single query with OPTIONAL path to ticket + WHERE filter on message.
	cqlCommits := `
		MATCH (cm:Commit)
		WHERE toLower(cm.message) CONTAINS toLower($kw)
		   OR EXISTS {
		       MATCH (cm)-[:MENTIONS_TICKET]->(t:Ticket)
		       WHERE toLower(t.name) CONTAINS toLower($kw)
		   }
		RETURN cm.hash AS hash, cm.date AS date,
		       cm.author AS author, cm.message AS message
		ORDER BY cm.date DESC
		LIMIT 50
	`
	res, err := session.Run(ctx, cqlCommits, map[string]any{"kw": keyword})
	if err != nil {
		return nil, err
	}
	impact := &TicketImpact{TicketID: keyword}
	seenHash := map[string]bool{}
	for res.Next(ctx) {
		rec := res.Record()
		get := func(k string) string {
			v, _ := rec.Get(k)
			if v == nil {
				return ""
			}
			return fmt.Sprintf("%v", v)
		}
		h := get("hash")
		if seenHash[h] {
			continue
		}
		seenHash[h] = true
		impact.Commits = append(impact.Commits, &CommitInfo{
			Hash:    h,
			Date:    get("date"),
			Author:  get("author"),
			Message: get("message"),
		})
	}
	if err := res.Err(); err != nil {
		return nil, err
	}

	// Files touched by matched commits
	cqlFiles := `
		MATCH (cm:Commit)<-[:HAS_COMMIT]-(f:File)
		WHERE toLower(cm.message) CONTAINS toLower($kw)
		   OR EXISTS {
		       MATCH (cm)-[:MENTIONS_TICKET]->(t:Ticket)
		       WHERE toLower(t.name) CONTAINS toLower($kw)
		   }
		RETURN DISTINCT f.id AS id, labels(f)[0] AS type, f.name AS name,
		       f.language AS language, f.filePath AS filePath,
		       f.startLine AS startLine, f.doc AS doc
		LIMIT 100
	`
	res2, err := session.Run(ctx, cqlFiles, map[string]any{"kw": keyword})
	if err != nil {
		return nil, err
	}
	for res2.Next(ctx) {
		impact.Files = append(impact.Files, recordToNode(res2.Record()))
	}
	if err := res2.Err(); err != nil {
		return nil, err
	}

	// Functions: only those whose startLine–endLine overlaps the changed line ranges
	// stored on the HAS_COMMIT edge. Falls back to all functions if no ranges recorded.
	cqlFns := `
		MATCH (cm:Commit)<-[hc:HAS_COMMIT]-(f:File)-[]->(fn)
		WHERE (fn:Function OR fn:Method OR fn:Struct OR fn:Interface)
		  AND (
		    toLower(cm.message) CONTAINS toLower($kw)
		    OR EXISTS {
		        MATCH (cm)-[:MENTIONS_TICKET]->(t:Ticket)
		        WHERE toLower(t.name) CONTAINS toLower($kw)
		    }
		  )
		  AND (
		    hc.changedRanges IS NULL
		    OR size(hc.changedRanges) = 0
		    OR ANY(i IN range(0, size(hc.changedRanges)-2, 2)
		           WHERE hc.changedRanges[i] <= coalesce(fn.endLine, fn.startLine)
		             AND hc.changedRanges[i+1] >= fn.startLine)
		  )
		RETURN DISTINCT fn.id AS id, labels(fn)[0] AS type, fn.name AS name,
		       fn.language AS language, fn.filePath AS filePath,
		       fn.startLine AS startLine, fn.doc AS doc
		ORDER BY fn.filePath, fn.startLine
		LIMIT 200
	`
	res3, err := session.Run(ctx, cqlFns, map[string]any{"kw": keyword})
	if err != nil {
		return nil, err
	}
	for res3.Next(ctx) {
		impact.Functions = append(impact.Functions, recordToNode(res3.Record()))
	}
	return impact, res3.Err()
}

// FunctionHistory returns all commits that touched the file containing a function.
type FunctionHistory struct {
	Function *umodel.Node  `json:"function"`
	Commits  []*CommitInfo `json:"commits"`
}

// GetFunctionHistory returns the git commit history for a named function/method.
func (c *Client) GetFunctionHistory(ctx context.Context, name string) ([]*FunctionHistory, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.db})
	defer session.Close(ctx)

	cql := `
		MATCH (fn)
		WHERE (fn:Function OR fn:Method) AND toLower(fn.name) CONTAINS toLower($name)
		WITH fn LIMIT 10
		OPTIONAL MATCH (f:File {filePath: fn.filePath})-[:HAS_COMMIT]->(cm:Commit)
		RETURN fn.id AS fnId, labels(fn)[0] AS fnType, fn.name AS fnName,
		       fn.language AS fnLang, fn.filePath AS fnPath, fn.startLine AS fnLine,
		       fn.doc AS fnDoc,
		       cm.hash AS hash, cm.date AS date,
		       cm.author AS author, cm.message AS message,
		       cm.ticketIds AS ticketIds
		ORDER BY date DESC
	`
	res, err := session.Run(ctx, cql, map[string]any{"name": name})
	if err != nil {
		return nil, err
	}

	// Group by function ID
	type key = string
	fnMap := map[key]*FunctionHistory{}
	var order []key

	for res.Next(ctx) {
		rec := res.Record()
		get := func(k string) string {
			v, _ := rec.Get(k)
			if v == nil { return "" }
			return fmt.Sprintf("%v", v)
		}
		startLine := 0
		if v, ok := rec.Get("fnLine"); ok {
			if i, ok := v.(int64); ok { startLine = int(i) }
		}
		fnID := get("fnId")
		if _, exists := fnMap[fnID]; !exists {
			fnMap[fnID] = &FunctionHistory{
				Function: &umodel.Node{
					ID:        fnID,
					Type:      umodel.NodeType(get("fnType")),
					Name:      get("fnName"),
					Language:  get("fnLang"),
					FilePath:  get("fnPath"),
					StartLine: startLine,
					Doc:       get("fnDoc"),
				},
			}
			order = append(order, fnID)
		}
		hash := get("hash")
		if hash != "" {
			fnMap[fnID].Commits = append(fnMap[fnID].Commits, &CommitInfo{
				Hash:    hash,
				Date:    get("date"),
				Author:  get("author"),
				Message: get("message"),
			})
		}
	}
	if err := res.Err(); err != nil {
		return nil, err
	}

	result := make([]*FunctionHistory, 0, len(order))
	for _, id := range order {
		result = append(result, fnMap[id])
	}
	return result, nil
}
