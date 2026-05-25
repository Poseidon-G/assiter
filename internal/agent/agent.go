// Package agent provides an AI agent that uses the knowledge graph as context.
// The agent is provider-agnostic: swap OpenAI, GitHub Copilot, or any
// OpenAI-compatible endpoint (Ollama, Mistral, Azure OpenAI, …) via config.
package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/quyenluc/assiter/internal/graph"
	"github.com/quyenluc/assiter/pkg/config"
)

// ChatMessage is a single turn in a conversation sent to the LLM.
type ChatMessage struct {
	Role    string // "system" | "user" | "assistant"
	Content string
}

// LLMProvider is the interface every AI backend must implement.
// Adding a new backend only requires implementing these two methods.
type LLMProvider interface {
	// Name returns a human-readable label (e.g. "openai", "copilot", "ollama").
	Name() string
	// Ask sends a conversation to the LLM and returns the assistant's reply.
	Ask(ctx context.Context, messages []ChatMessage) (string, error)
}

// Agent uses the knowledge graph to provide AI-powered code understanding.
type Agent struct {
	llm   LLMProvider
	graph *graph.Client
}

// New creates an Agent wired to the LLM provider selected in cfg.
func New(cfg config.LLMConfig, g *graph.Client) (*Agent, error) {
	p, err := NewLLMProvider(cfg)
	if err != nil {
		return nil, err
	}
	return &Agent{llm: p, graph: g}, nil
}

// NewWithProvider creates an Agent with an explicitly supplied LLMProvider.
// Useful for testing or when you construct the provider yourself.
func NewWithProvider(p LLMProvider, g *graph.Client) *Agent {
	return &Agent{llm: p, graph: g}
}

// ProviderName returns the name of the active LLM provider.
func (a *Agent) ProviderName() string { return a.llm.Name() }

// QueryRequest is the input to a knowledge-graph-augmented AI query.
type QueryRequest struct {
	Question string `json:"question"`
	// Symbol is an optional starting point for graph context (function/class name).
	Symbol string `json:"symbol,omitempty"`
}

// QueryResponse contains the AI's answer and the graph context used.
type QueryResponse struct {
	Answer       string `json:"answer"`
	GraphContext string `json:"graph_context,omitempty"`
	Provider     string `json:"provider"`
}

// Query answers a question using graph context retrieved from Neo4j.
func (a *Agent) Query(ctx context.Context, req QueryRequest) (*QueryResponse, error) {
	graphCtx, err := a.buildGraphContext(ctx, req.Question, req.Symbol)
	if err != nil {
		graphCtx = "(graph context unavailable)"
	}

	systemPrompt := `You are an expert software architect with deep knowledge of the codebase.
You are given a structured knowledge graph of the code (functions, types, dependencies, imports).
Use this context to answer the user's question precisely.
When explaining code impact, reference specific symbols from the graph.
Always mention which files/packages are affected by proposed changes.`

	userPrompt := fmt.Sprintf("## Knowledge Graph Context\n%s\n\n## Question\n%s",
		graphCtx, req.Question)

	answer, err := a.llm.Ask(ctx, []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	})
	if err != nil {
		return nil, fmt.Errorf("llm %s: %w", a.llm.Name(), err)
	}

	return &QueryResponse{
		Answer:       answer,
		GraphContext: graphCtx,
		Provider:     a.llm.Name(),
	}, nil
}

// buildGraphContext constructs a text summary of relevant graph nodes.
func (a *Agent) buildGraphContext(ctx context.Context, question, symbol string) (string, error) {
	var parts []string

	stats, err := a.graph.Stats(ctx)
	if err == nil {
		parts = append(parts, formatStats(stats))
	}

	searchTerm := symbol
	if searchTerm == "" {
		searchTerm = extractKeyword(question)
	}

	if searchTerm != "" {
		nodes, err := a.graph.SearchNodes(ctx, searchTerm, nil)
		if err == nil && len(nodes) > 0 {
			var nodeLines []string
			for _, n := range nodes {
				line := fmt.Sprintf("  [%s] %s (file: %s, line: %d)",
					n.Type, n.Name, n.FilePath, n.StartLine)
				if n.Doc != "" {
					line += "\n    doc: " + truncate(n.Doc, 120)
				}
				nodeLines = append(nodeLines, line)
			}
			parts = append(parts, "### Relevant Symbols\n"+strings.Join(nodeLines, "\n"))
		}
	}

	if len(parts) == 0 {
		return "(no graph context found)", nil
	}
	return strings.Join(parts, "\n\n"), nil
}

func formatStats(stats map[string]int64) string {
	var lines []string
	lines = append(lines, "### Graph Overview")
	for label, count := range stats {
		lines = append(lines, fmt.Sprintf("  %s: %d nodes", label, count))
	}
	return strings.Join(lines, "\n")
}

func extractKeyword(question string) string {
	words := strings.Fields(question)
	for _, w := range words {
		clean := strings.Trim(w, `.,;:?!"'`)
		if len(clean) > 4 && strings.ToLower(clean) == clean {
			return clean
		}
	}
	if len(words) > 0 {
		return strings.Trim(words[0], `.,;:?!"'`)
	}
	return ""
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
