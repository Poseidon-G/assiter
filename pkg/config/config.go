// Package config handles application configuration via YAML file and environment variables.
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config holds all application configuration.
type Config struct {
	Server ServerConfig `mapstructure:"server"`
	Neo4j  Neo4jConfig  `mapstructure:"neo4j"`
	LLM    LLMConfig    `mapstructure:"llm"`
	Parser ParserConfig `mapstructure:"parser"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Host string `mapstructure:"host"`
}

type Neo4jConfig struct {
	URI      string `mapstructure:"uri"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`
}

// LLMConfig configures which AI model provider Assiter talks to.
//
//	provider: "openai"  — OpenAI API (api.openai.com). api_key = OpenAI secret key.
//	provider: "copilot" — GitHub Copilot API. api_key = GitHub personal access token.
//	provider: "custom"  — Any OpenAI-compatible endpoint (Ollama, Mistral, Azure…).
//	                      Set base_url to the endpoint root, e.g. http://localhost:11434/v1
type LLMConfig struct {
	Provider string `mapstructure:"provider"` // "openai" | "copilot" | "custom"
	APIKey   string `mapstructure:"api_key"`
	BaseURL  string `mapstructure:"base_url"`
	Model    string `mapstructure:"model"`
	// Name is an optional display label for "custom" providers (e.g. "ollama", "mistral").
	Name string `mapstructure:"name"`
}

type ParserConfig struct {
	Languages []string `mapstructure:"languages"`
	Exclude   []string `mapstructure:"exclude"`
}

// Load reads config from the given file path and overlays environment variables.
// Env vars use the prefix ASSITER_ and double underscores for nesting,
// e.g. ASSITER_NEO4J__URI, ASSITER_LLM__API_KEY, ASSITER_LLM__PROVIDER.
func Load(cfgFile string) (*Config, error) {
	v := viper.New()

	setDefaults(v)

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigName("assiter")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.assiter")
		v.AddConfigPath("/etc/assiter")
	}

	v.SetEnvPrefix("ASSITER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "__"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.host", "0.0.0.0")

	v.SetDefault("neo4j.uri", "bolt://localhost:7687")
	v.SetDefault("neo4j.username", "neo4j")
	v.SetDefault("neo4j.password", "password")
	v.SetDefault("neo4j.database", "neo4j")

	v.SetDefault("llm.provider", "openai")
	v.SetDefault("llm.base_url", "")
	v.SetDefault("llm.model", "gpt-4o")

	v.SetDefault("parser.languages", []string{"go", "python", "typescript", "java", "rust", "cpp"})
	v.SetDefault("parser.exclude", []string{"vendor", "node_modules", ".git", "dist", "build"})
}
