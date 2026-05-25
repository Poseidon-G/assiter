// Assiter server entrypoint — starts the REST API directly without CLI flags.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/quyenluc/assiter/internal/agent"
	"github.com/quyenluc/assiter/internal/api"
	"github.com/quyenluc/assiter/internal/graph"
	"github.com/quyenluc/assiter/internal/ingestion"
	"github.com/quyenluc/assiter/internal/normalizer"
	"github.com/quyenluc/assiter/internal/parser"
	"github.com/quyenluc/assiter/pkg/config"
)

func main() {
	cfg, err := config.Load("")
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	g, err := graph.New(cfg.Neo4j)
	if err != nil {
		slog.Error("failed to connect to Neo4j", "err", err)
		os.Exit(1)
	}
	defer g.Close(context.Background())

	if err := g.EnsureSchema(context.Background()); err != nil {
		slog.Error("schema setup failed", "err", err)
		os.Exit(1)
	}

	p := parser.New(cfg.Parser.Languages)
	n := normalizer.New()
	pipe := ingestion.New(p, n, g)
	a, err := agent.New(cfg.LLM, g)
	if err != nil {
		slog.Error("failed to create agent", "err", err)
		os.Exit(1)
	}

	srv := api.New(cfg, pipe, g, a)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	slog.Info("Assiter server starting", "addr", addr)
	if err := srv.Run(); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}
