// Package ingestion provides the end-to-end pipeline: scan → parse → normalize → store.
package ingestion

import (
	"context"
	"crypto/sha256"
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
	Force   bool // skip checksum dedup and re-ingest all files
}

// IngestResult summarises the outcome of an ingestion run.
type IngestResult struct {
	FilesProcessed int
	FilesSkipped   int // files whose content hasn't changed since last ingestion
	NodesWritten   int
	EdgesWritten   int
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

	// Fetch checksums of files already stored in Neo4j.
	// If this fails (e.g. first run, empty graph) we simply ingest everything.
	existingChecksums := map[string]string{}
	if !opts.Force {
		existingChecksums, err = p.graph.GetFileChecksums(ctx)
		if err != nil {
			slog.Warn("could not fetch existing checksums, ingesting all files", "err", err)
			existingChecksums = map[string]string{}
		}
	}

	// Filter to only files that are new or whose content has changed.
	var changed []*parser.ParseResult
	skipped := 0
	for _, r := range results {
		cs := fileChecksum(r.Source)
		if existingChecksums[r.FilePath] == cs {
			skipped++
			continue
		}
		changed = append(changed, r)
	}
	slog.Info("change detection complete",
		"total", len(results),
		"changed", len(changed),
		"skipped_unchanged", skipped,
	)

	if len(changed) == 0 {
		slog.Info("nothing to ingest — all files are up to date")
		return &IngestResult{
			FilesProcessed: len(results),
			FilesSkipped:   skipped,
		}, nil
	}

	g := p.normalizer.NormalizeAll(changed)
	slog.Info("normalization complete", "nodes", len(g.Nodes), "edges", len(g.Edges))

	slog.Info("storing graph", "nodes", len(g.Nodes), "edges", len(g.Edges))
	if err := p.graph.UpsertGraph(ctx, g); err != nil {
		return nil, fmt.Errorf("upserting graph: %w", err)
	}

	slog.Info("ingestion complete",
		"files_changed", len(changed),
		"files_skipped", skipped,
		"nodes", len(g.Nodes),
		"edges", len(g.Edges),
	)

	return &IngestResult{
		FilesProcessed: len(results),
		FilesSkipped:   skipped,
		NodesWritten:   len(g.Nodes),
		EdgesWritten:   len(g.Edges),
	}, nil
}

func fileChecksum(src []byte) string {
	h := sha256.Sum256(src)
	return fmt.Sprintf("%x", h[:])
}
