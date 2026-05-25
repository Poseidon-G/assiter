// Assiter CLI — interact with the code knowledge graph from the command line.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/quyenluc/assiter/internal/agent"
	"github.com/quyenluc/assiter/internal/api"
	"github.com/quyenluc/assiter/internal/graph"
	"github.com/quyenluc/assiter/internal/ingestion"
	"github.com/quyenluc/assiter/internal/normalizer"
	"github.com/quyenluc/assiter/internal/parser"
	"github.com/quyenluc/assiter/pkg/config"
)

var cfgFile string

func main() {
	root := &cobra.Command{
		Use:   "assiter",
		Short: "Code knowledge graph for AI agents",
		Long: `Assiter parses code repositories into a semantic knowledge graph (Neo4j),
then exposes it to AI agents for context-aware code understanding.`,
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: assiter.yaml)")

	root.AddCommand(
		ingestCmd(),
		queryCmd(),
		graphCmd(),
		serveCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadDeps() (*config.Config, *graph.Client, *agent.Agent, *ingestion.Pipeline, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("config: %w", err)
	}

	g, err := graph.New(cfg.Neo4j)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("neo4j: %w", err)
	}

	a, err := agent.New(cfg.LLM, g)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("agent: %w", err)
	}
	p := parser.New(cfg.Parser.Languages)
	n := normalizer.New()
	pipe := ingestion.New(p, n, g)

	return cfg, g, a, pipe, nil
}

// --- ingest command ---

func ingestCmd() *cobra.Command {
	var exclude []string
	var force bool
	cmd := &cobra.Command{
		Use:   "ingest <path>",
		Short: "Parse and ingest a code repository into the knowledge graph",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, g, _, pipe, err := loadDeps()
			if err != nil {
				return err
			}
			defer g.Close(context.Background())

			ctx := context.Background()
			if err := g.EnsureSchema(ctx); err != nil {
				return fmt.Errorf("schema: %w", err)
			}

			ex := exclude
			if len(ex) == 0 {
				ex = cfg.Parser.Exclude
			}

			result, err := pipe.Run(ctx, ingestion.IngestOptions{Dir: args[0], Exclude: ex, Force: force})
			if err != nil {
				return err
			}

			fmt.Printf("✅ Ingestion complete\n")
			fmt.Printf("   Files processed : %d\n", result.FilesProcessed)
			fmt.Printf("   Files skipped   : %d (unchanged)\n", result.FilesSkipped)
			fmt.Printf("   Nodes written   : %d\n", result.NodesWritten)
			fmt.Printf("   Edges written   : %d\n", result.EdgesWritten)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&exclude, "exclude", nil, "directories to exclude (comma-separated)")
	cmd.Flags().BoolVar(&force, "force", false, "re-ingest all files, ignoring cached checksums")
	return cmd
}

// --- query command ---

func queryCmd() *cobra.Command {
	var symbol string
	cmd := &cobra.Command{
		Use:   "query \"<question>\"",
		Short: "Ask the AI agent a question about the codebase",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, g, a, _, err := loadDeps()
			if err != nil {
				return err
			}
			defer g.Close(context.Background())

			resp, err := a.Query(context.Background(), agent.QueryRequest{
				Question: args[0],
				Symbol:   symbol,
			})
			if err != nil {
				return err
			}

			fmt.Println("### Answer")
			fmt.Println(resp.Answer)
			return nil
		},
	}
	cmd.Flags().StringVar(&symbol, "symbol", "", "symbol name to anchor graph context (optional)")
	return cmd
}

// --- graph command group ---

func graphCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Interact with the knowledge graph",
	}
	cmd.AddCommand(graphStatsCmd(), graphSearchCmd(), graphFileCmd(), graphCallersCmd())
	return cmd
}

func graphStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show knowledge graph statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, g, _, _, err := loadDeps()
			if err != nil {
				return err
			}
			defer g.Close(context.Background())

			stats, err := g.Stats(context.Background())
			if err != nil {
				return err
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(stats)
		},
	}
}

func graphSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <symbol>",
		Short: "Search for a symbol in the knowledge graph",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, g, _, _, err := loadDeps()
			if err != nil {
				return err
			}
			defer g.Close(context.Background())

			nodes, err := g.SearchNodes(context.Background(), args[0], nil)
			if err != nil {
				return err
			}
			if len(nodes) == 0 {
				fmt.Println("No nodes found.")
				return nil
			}
			for _, n := range nodes {
				fmt.Printf("[%s] %s  %s:%d\n", n.Type, n.Name, n.FilePath, n.StartLine)
				if n.Doc != "" {
					fmt.Printf("      %s\n", truncate(n.Doc, 100))
				}
			}
			return nil
		},
	}
}

func graphFileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "file <path>",
		Short: "Check if a source file has been ingested and show its status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, g, _, _, err := loadDeps()
			if err != nil {
				return err
			}
			defer g.Close(context.Background())

			status, err := g.GetFileStatus(context.Background(), args[0])
			if err != nil {
				return err
			}
			if status == nil {
				fmt.Printf("❌ Not ingested: %s\n", args[0])
				return nil
			}
			fmt.Printf("✅ Ingested: %s\n", status.FilePath)
			fmt.Printf("   Name       : %s\n", status.Name)
			fmt.Printf("   Checksum   : %s\n", status.Checksum)
			fmt.Printf("   Child nodes: %d (functions, methods, structs, etc.)\n", status.ChildNodes)
			return nil
		},
	}
}


func graphCallersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "callers <symbol>",
		Short: "Find all functions/methods that call a given symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, g, _, _, err := loadDeps()
			if err != nil {
				return err
			}
			defer g.Close(context.Background())

			callers, err := g.SearchCallers(context.Background(), args[0])
			if err != nil {
				return err
			}
			if len(callers) == 0 {
				fmt.Printf("No callers found for %q\n", args[0])
				return nil
			}
			fmt.Printf("Callers of %q (%d results):\n\n", args[0], len(callers))
			for _, c := range callers {
				fmt.Printf("  [%s] %s  →  %s\n", c.Node.Type, c.Node.Name, c.Callee)
				fmt.Printf("         %s:%d\n", c.Node.FilePath, c.Node.StartLine)
			}
			return nil
		},
	}
}


func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --- serve command ---

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the REST API server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, g, a, pipe, err := loadDeps()
			if err != nil {
				return err
			}
			defer g.Close(context.Background())

			if err := g.EnsureSchema(context.Background()); err != nil {
				return fmt.Errorf("schema: %w", err)
			}

			srv := api.New(cfg, pipe, g, a)
			fmt.Printf("🚀 Assiter server running on %s:%d\n", cfg.Server.Host, cfg.Server.Port)
			return srv.Run()
		},
	}
}
