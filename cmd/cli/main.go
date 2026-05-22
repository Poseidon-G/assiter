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

	a := agent.New(cfg.OpenAI, g)
	p := parser.New(cfg.Parser.Languages)
	n := normalizer.New()
	pipe := ingestion.New(p, n, g)

	return cfg, g, a, pipe, nil
}

// --- ingest command ---

func ingestCmd() *cobra.Command {
	var exclude []string
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

			result, err := pipe.Run(ctx, ingestion.IngestOptions{Dir: args[0], Exclude: ex})
			if err != nil {
				return err
			}

			fmt.Printf("✅ Ingestion complete\n")
			fmt.Printf("   Files processed : %d\n", result.FilesProcessed)
			fmt.Printf("   Nodes created   : %d\n", result.NodesCreated)
			fmt.Printf("   Edges created   : %d\n", result.EdgesCreated)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&exclude, "exclude", nil, "directories to exclude (comma-separated)")
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
	cmd.AddCommand(graphStatsCmd(), graphSearchCmd())
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
