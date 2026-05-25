package agent

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
	"github.com/quyenluc/assiter/pkg/config"
)

// Provider name constants — used in assiter.yaml under llm.provider.
const (
	ProviderOpenAI  = "openai"
	ProviderCopilot = "copilot"
	ProviderCustom  = "custom" // any OpenAI-compatible endpoint: Ollama, Mistral, Azure…
)

// NewLLMProvider constructs the correct LLMProvider based on cfg.Provider.
func NewLLMProvider(cfg config.LLMConfig) (LLMProvider, error) {
	switch cfg.Provider {
	case ProviderOpenAI, "":
		return newOpenAIProvider(cfg), nil
	case ProviderCopilot:
		return newCopilotProvider(cfg), nil
	case ProviderCustom:
		return newCustomProvider(cfg), nil
	default:
		return nil, fmt.Errorf("unknown llm.provider %q (supported: openai, copilot, custom)", cfg.Provider)
	}
}

// ---------------------------------------------------------------------------
// OpenAI provider
// ---------------------------------------------------------------------------

type openAIProvider struct {
	client *openai.Client
	model  string
}

func newOpenAIProvider(cfg config.LLMConfig) LLMProvider {
	c := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		c.BaseURL = cfg.BaseURL
	}
	return &openAIProvider{client: openai.NewClientWithConfig(c), model: cfg.Model}
}

func (p *openAIProvider) Name() string { return ProviderOpenAI }

func (p *openAIProvider) Ask(ctx context.Context, messages []ChatMessage) (string, error) {
	return chatComplete(ctx, p.client, p.model, messages)
}

// ---------------------------------------------------------------------------
// GitHub Copilot provider
//
// Copilot exposes an OpenAI-compatible chat completions API.
// Auth:     Bearer <GITHUB_TOKEN>  (personal access token with copilot scope)
// Endpoint: https://api.githubcopilot.com
// Models:   gpt-4o, claude-3.5-sonnet, o3-mini, …
// ---------------------------------------------------------------------------

const copilotEndpoint = "https://api.githubcopilot.com"
const copilotDefaultModel = "gpt-4o"

type copilotProvider struct {
	client *openai.Client
	model  string
}

func newCopilotProvider(cfg config.LLMConfig) LLMProvider {
	c := openai.DefaultConfig(cfg.APIKey) // api_key = GitHub token
	endpoint := cfg.BaseURL
	if endpoint == "" {
		endpoint = copilotEndpoint
	}
	c.BaseURL = endpoint

	model := cfg.Model
	if model == "" {
		model = copilotDefaultModel
	}
	return &copilotProvider{client: openai.NewClientWithConfig(c), model: model}
}

func (p *copilotProvider) Name() string { return ProviderCopilot }

func (p *copilotProvider) Ask(ctx context.Context, messages []ChatMessage) (string, error) {
	return chatComplete(ctx, p.client, p.model, messages)
}

// ---------------------------------------------------------------------------
// Custom provider — any OpenAI-compatible endpoint
// Examples: Ollama (http://localhost:11434/v1), Mistral, Azure OpenAI, LM Studio
// ---------------------------------------------------------------------------

type customProvider struct {
	client *openai.Client
	model  string
	name   string // display name from llm.name in config
}

func newCustomProvider(cfg config.LLMConfig) LLMProvider {
	c := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		c.BaseURL = cfg.BaseURL
	}
	name := cfg.Name
	if name == "" {
		name = ProviderCustom
	}
	return &customProvider{
		client: openai.NewClientWithConfig(c),
		model:  cfg.Model,
		name:   name,
	}
}

func (p *customProvider) Name() string { return p.name }

func (p *customProvider) Ask(ctx context.Context, messages []ChatMessage) (string, error) {
	return chatComplete(ctx, p.client, p.model, messages)
}

// ---------------------------------------------------------------------------
// Shared HTTP call — all three providers use the same OpenAI wire format
// ---------------------------------------------------------------------------

func chatComplete(ctx context.Context, client *openai.Client, model string, messages []ChatMessage) (string, error) {
	var msgs []openai.ChatCompletionMessage
	for _, m := range messages {
		msgs = append(msgs, openai.ChatCompletionMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    msgs,
		Temperature: 0.2,
	})
	if err != nil {
		return "", fmt.Errorf("chat completion: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response from LLM")
	}
	return resp.Choices[0].Message.Content, nil
}
