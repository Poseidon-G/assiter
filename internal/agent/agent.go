// Package agent provides an AI agent that uses the knowledge graph as context.
package agent

import (
	"context"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"
	"github.com/quyenluc/assiter/internal/graph"
	"github.com/quyenluc/assiter/pkg/config"
)

// Agent uses the knowledge graph to provide AI-powered code understanding.
type Agent struct {
	client *openai.Client
	graph  *graph.Client
	model  string
}

// New creates an Agent using the provided config and graph client.
func New(cfg config.OpenAIConfig, g *graph.Client) *Agent {
	clientCfg := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		clientCfg.BaseURL = cfg.BaseURL
	}
	return &Agent{
		client: openai.NewClientWithConfig(clientCfg),
		graph:  g,
		model:  cfg.Model,
	}
}

// QueryRequest is the input to a knowledge-graph-augmented AI query.
type QueryRequest struct {
	Question string `json:"question"`
	// Symbol is an optional starting point for graph context (function/class name).
	Symbol string `json:"symbol,omitempty"`
}

// QueryResponse contains the AI's answer and the graph context used.
type QueryResponse struct {
	Answer      string `json:"answer"`
	GraphContext string `json:"graph_context,omitempty"`
}

// Query answers a question using graph context retrieved from Neo4j.
func (a *Agent) Query(ctx context.Context, req QueryRequest) (*QueryResponse, error) {
	graphCtx, err := a.buildGraphContext(ctx, req.Question, req.Symbol)
	if err != nil {
		// degrade gracefully: proceed without graph context
		graphCtx = "(graph context unavailable)"
	}

	systemPrompt := `You are an expert software architect with deep knowledge of the codebase.
You are given a structured knowledge graph of the code (functions, types, dependencies, imports).
Use this context to answer the user's question precisely.
When explaining code impact, reference specific symbols from the graph.
Always mention which files/packages are affected by proposed changes.`

	userPrompt := fmt.Sprintf(`## Knowledge Graph Context
%s

## Question
%s`, graphCtx, req.Question)

	resp, err := a.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: a.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: userPrompt},
		},
		Temperature: 0.2,
	})
	if err != nil {
		return nil, fmt.Errorf("openai completion: %w", err)
	}

	answer := ""
	if len(resp.Choices) > 0 {
		answer = resp.Choices[0].Message.Content
	}

	return &QueryResponse{
		Answer:      answer,
		GraphContext: graphCtx,
	}, nil
}

// buildGraphContext constructs a text summary of relevant graph nodes.
func (a *Agent) buildGraphContext(ctx context.Context, question, symbol string) (string, error) {
	var parts []string

	// Get graph stats for overview
	stats, err := a.graph.Stats(ctx)
	if err == nil {
		parts = append(parts, formatStats(stats))
	}

	// Search by symbol name if provided, otherwise by keywords from question
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
