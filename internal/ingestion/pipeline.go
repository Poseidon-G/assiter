// Package ingestion provides the end-to-end pipeline: scan → parse → normalize → store.
package ingestion

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/quyenluc/assiter/internal/graph"
	"github.com/quyenluc/assiter/internal/normalizer"
	"github.com/quyenluc/assiter/internal/parser"
)

// Pipeline orchestrates the ingestion of a code repository.
type Pipeline struct {
	parser     *parser.Parser
	normalizer *normalizer.Normalizer
	graph      *graph.Client
}

// New creates a Pipeline with the provided dependencies.
func New(p *parser.Parser, n *normalizer.Normalizer, g *graph.Client) *Pipeline {
	return &Pipeline{parser: p, normalizer: n, graph: g}
}

// IngestOptions configures an ingestion run.
type IngestOptions struct {
	Dir     string
	Exclude []string
}

// IngestResult summarises the outcome of an ingestion run.
type IngestResult struct {
	FilesProcessed int
	NodesCreated   int
	EdgesCreated   int
	Errors         []string
}

// Run executes the full ingestion pipeline for the given directory.
func (p *Pipeline) Run(ctx context.Context, opts IngestOptions) (*IngestResult, error) {
	slog.Info("ingestion started", "dir", opts.Dir)

	results, err := p.parser.ParseDir(opts.Dir, opts.Exclude)
	if err != nil {
		return nil, fmt.Errorf("parsing directory %s: %w", opts.Dir, err)
	}

	slog.Info("parsing complete", "files", len(results))

	g := p.normalizer.NormalizeAll(results)

	slog.Info("normalization complete", "nodes", len(g.Nodes), "edges", len(g.Edges))

	if err := p.graph.UpsertGraph(ctx, g); err != nil {
		return nil, fmt.Errorf("upserting graph: %w", err)
	}

	slog.Info("ingestion complete", "nodes", len(g.Nodes), "edges", len(g.Edges))

	return &IngestResult{
		FilesProcessed: len(results),
		NodesCreated:   len(g.Nodes),
		EdgesCreated:   len(g.Edges),
	}, nil
}
