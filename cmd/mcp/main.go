// Assiter MCP server — exposes the code knowledge graph as MCP tools so that
// GitHub Copilot, Claude, Cursor, and any other MCP-compatible agent can query
// the graph directly from their context window.
//
// Transport: stdio (JSON-RPC 2.0 over stdin/stdout)
//
// Tools exposed:
//   - ingest           — parse and ingest a directory into the graph
//   - search_nodes     — find symbols by name (functions, structs, methods…)
//   - search_callers   — find all callers of a symbol name
//   - get_file_context — list all symbols defined in a source file
//   - get_node_context — get a node plus its direct graph neighbours
//   - graph_stats      — node counts by type
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/quyenluc/assiter/internal/graph"
	"github.com/quyenluc/assiter/internal/ingestion"
	"github.com/quyenluc/assiter/internal/normalizer"
	"github.com/quyenluc/assiter/internal/parser"
	"github.com/quyenluc/assiter/pkg/config"
)

func main() {
	cfgFile := flag.String("config", "", "config file (default: assiter.yaml)")
	flag.Parse()

	cfg, err := config.Load(*cfgFile)
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	g, err := graph.New(cfg.Neo4j)
	if err != nil {
		slog.Error("neo4j connect failed", "err", err)
		os.Exit(1)
	}
	defer g.Close(context.Background())

	ctx := context.Background()
	if err := g.EnsureSchema(ctx); err != nil {
		slog.Error("schema setup failed", "err", err)
		os.Exit(1)
	}

	p := parser.New(cfg.Parser.Languages)
	n := normalizer.New()
	pipe := ingestion.New(p, n, g)

	// Preload: auto-ingest configured directory on startup.
	if cfg.Preload.Dir != "" {
		slog.Info("preload ingestion starting", "dir", cfg.Preload.Dir, "force", cfg.Preload.Force)
		result, err := pipe.Run(ctx, ingestion.IngestOptions{
			Dir:     cfg.Preload.Dir,
			Exclude: cfg.Parser.Exclude,
			Force:   cfg.Preload.Force,
		})
		if err != nil {
			slog.Error("preload ingestion failed", "err", err)
		} else {
			slog.Info("preload ingestion complete",
				"files", result.FilesProcessed,
				"skipped", result.FilesSkipped,
				"nodes", result.NodesWritten,
				"edges", result.EdgesWritten,
			)
		}
	}

	s := server.NewMCPServer(
		"assiter",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	registerTools(s, g, pipe, cfg)

	slog.Info("assiter MCP server ready", "transport", "stdio")
	if err := server.ServeStdio(s); err != nil {
		slog.Error("MCP server error", "err", err)
		os.Exit(1)
	}
}

// ── tool registration ──────────────────────────────────────────────────────

func registerTools(s *server.MCPServer, g *graph.Client, pipe *ingestion.Pipeline, cfg *config.Config) {

	// ── ingest ──────────────────────────────────────────────────────────────
	s.AddTool(
		mcp.NewTool("ingest",
			mcp.WithDescription("Parse and ingest a source code directory into the Assiter knowledge graph. "+
				"Call this once per project, then use other tools to search the graph."),
			mcp.WithString("dir",
				mcp.Required(),
				mcp.Description("Absolute path to the directory to ingest"),
			),
			mcp.WithBoolean("force",
				mcp.Description("Re-ingest all files even if unchanged (default: false)"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			dir := req.GetString("dir", "")
			force := req.GetBool("force", false)
			if dir == "" {
				return mcp.NewToolResultError("dir is required"), nil
			}
			result, err := pipe.Run(ctx, ingestion.IngestOptions{
				Dir:     dir,
				Exclude: cfg.Parser.Exclude,
				Force:   force,
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			out := fmt.Sprintf(
				"Ingestion complete.\nFiles processed: %d\nFiles skipped (unchanged): %d\nNodes written: %d\nEdges written: %d",
				result.FilesProcessed, result.FilesSkipped, result.NodesWritten, result.EdgesWritten,
			)
			return mcp.NewToolResultText(out), nil
		},
	)

	// ── search_nodes ────────────────────────────────────────────────────────
	s.AddTool(
		mcp.NewTool("search_nodes",
			mcp.WithDescription("Search the knowledge graph for symbols matching a name. "+
				"Returns functions, methods, structs, interfaces, variables, and files. "+
				"Use this to find where a symbol is defined and in which file/line."),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("Symbol name to search for (case-insensitive, partial match)"),
			),
			mcp.WithString("type",
				mcp.Description("Optional filter: Function | Method | Struct | Interface | Variable | File"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name := req.GetString("name", "")
			nodeType := req.GetString("type", "")
			if name == "" {
				return mcp.NewToolResultError("name is required"), nil
			}
			var types []string
			if nodeType != "" {
				types = []string{nodeType}
			}
			nodes, err := g.SearchNodes(ctx, name, types)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(nodes)
		},
	)

	// ── search_callers ──────────────────────────────────────────────────────
	s.AddTool(
		mcp.NewTool("search_callers",
			mcp.WithDescription("Find all functions and methods that call a given symbol. "+
				"Useful for impact analysis: 'what code will break if I change X?'"),
			mcp.WithString("symbol",
				mcp.Required(),
				mcp.Description("Symbol name to find callers of (case-insensitive, partial match)"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			symbol := req.GetString("symbol", "")
			if symbol == "" {
				return mcp.NewToolResultError("symbol is required"), nil
			}
			callers, err := g.SearchCallers(ctx, symbol)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(callers)
		},
	)

	// ── get_file_context ────────────────────────────────────────────────────
	s.AddTool(
		mcp.NewTool("get_file_context",
			mcp.WithDescription("List all symbols (functions, methods, structs, interfaces) "+
				"defined in a specific source file, ordered by line number. "+
				"Use this to understand the full structure of a file before editing it."),
			mcp.WithString("file_path",
				mcp.Required(),
				mcp.Description("Absolute or relative path to the source file"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			filePath := req.GetString("file_path", "")
			if filePath == "" {
				return mcp.NewToolResultError("file_path is required"), nil
			}
			nodes, err := g.GetFileContext(ctx, filePath)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(nodes) == 0 {
				return mcp.NewToolResultText("No symbols found. File may not be ingested yet."), nil
			}
			return jsonResult(nodes)
		},
	)

	// ── get_node_context ────────────────────────────────────────────────────
	s.AddTool(
		mcp.NewTool("get_node_context",
			mcp.WithDescription("Get a node and all its direct graph neighbours (calls, belongs-to, contains, imports). "+
				"Use the node ID returned by search_nodes or get_file_context. "+
				"Gives the richest context for understanding a single symbol."),
			mcp.WithString("node_id",
				mcp.Required(),
				mcp.Description("Node ID (the 'id' field from search_nodes results)"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			nodeID := req.GetString("node_id", "")
			if nodeID == "" {
				return mcp.NewToolResultError("node_id is required"), nil
			}
			result, err := g.GetNodeWithNeighbors(ctx, nodeID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(result)
		},
	)

	// ── graph_stats ─────────────────────────────────────────────────────────
	s.AddTool(
		mcp.NewTool("graph_stats",
			mcp.WithDescription("Return node counts by type in the knowledge graph. "+
				"Use this to verify ingestion completed and understand graph coverage."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			stats, err := g.Stats(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			var sb strings.Builder
			sb.WriteString("Knowledge graph node counts:\n")
			for label, count := range stats {
				sb.WriteString(fmt.Sprintf("  %-15s %d\n", label, count))
			}
			return mcp.NewToolResultText(sb.String()), nil
		},
	)

	// ── graph_diagram ────────────────────────────────────────────────────────
	s.AddTool(
		mcp.NewTool("graph_diagram",
			mcp.WithDescription("Generate a Mermaid flowchart diagram for a symbol search. "+
				"GitHub Copilot Chat renders Mermaid diagrams inline. "+
				"Use this to visually understand relationships around a symbol."),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("Symbol name to visualize (partial match)"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name := req.GetString("name", "")
			if name == "" {
				return mcp.NewToolResultError("name is required"), nil
			}
			vg, err := g.GetSubgraph(ctx, name)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(vg.Nodes) == 0 {
				return mcp.NewToolResultText("No nodes found for \"" + name + "\". Run ingest first."), nil
			}
			return mcp.NewToolResultText(toMermaid(vg)), nil
		},
	)
}

// toMermaid converts a VizGraph to a Mermaid LR flowchart string.
func toMermaid(vg *graph.VizGraph) string {
	var sb strings.Builder
	sb.WriteString("```mermaid\nflowchart LR\n")

	for _, n := range vg.Nodes {
		label := strings.ReplaceAll(n.Label, "\"", "'")
		sb.WriteString(fmt.Sprintf("  %s[\"%s\\n%s\"]\n",
			sanitizeID(n.ID), label, n.Group))
	}
	for _, e := range vg.Edges {
		sb.WriteString(fmt.Sprintf("  %s -->|%s| %s\n",
			sanitizeID(e.From), e.Label, sanitizeID(e.To)))
	}
	sb.WriteString("```")
	return sb.String()
}

// sanitizeID makes a Neo4j hash ID safe for Mermaid node identifiers.
func sanitizeID(id string) string {
	// Mermaid IDs can't contain spaces or special chars — use first 8 chars of hash.
	if len(id) > 8 {
		return "n" + id[:8]
	}
	return "n" + id
}

// jsonResult marshals v to pretty JSON and returns it as a tool text result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}
