package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config is the top-level LiteClaw configuration.
type Config struct {
	Providers  ProviderConfig   `koanf:"providers"`
	Agent      AgentConfig      `koanf:"agent"`
	Memory     MemoryConfig     `koanf:"memory"`
	Cost       CostConfig       `koanf:"cost"`
	Security   SecurityConfig   `koanf:"security"`
	MCP        []MCPServerConfig `koanf:"mcp"`
	Scheduler  SchedulerConfig  `koanf:"scheduler"`
	Discord    *DiscordConfig   `koanf:"discord"`
	Slack      *SlackConfig     `koanf:"slack"`
	WhatsApp   *WhatsAppConfig  `koanf:"whatsapp"`
}

// ProviderConfig holds LLM provider settings.
type ProviderConfig struct {
	Default   string                     `koanf:"default"`
	Anthropic *AnthropicProviderConfig   `koanf:"anthropic"`
	OpenAI    *OpenAIProviderConfig      `koanf:"openai"`
	Ollama    *OllamaProviderConfig      `koanf:"ollama"`
}

type AnthropicProviderConfig struct {
	APIKeyEnv    string `koanf:"api_key_env"`
	DefaultModel string `koanf:"default_model"`
}

type OpenAIProviderConfig struct {
	APIKeyEnv    string `koanf:"api_key_env"`
	BaseURL      string `koanf:"base_url"`
	DefaultModel string `koanf:"default_model"`
}

type OllamaProviderConfig struct {
	BaseURL      string `koanf:"base_url"`
	DefaultModel string `koanf:"default_model"`
}

// AgentConfig controls the agent loop.
type AgentConfig struct {
	MaxTurns                int           `koanf:"max_turns"`
	MaxConsecutiveToolCalls int           `koanf:"max_consecutive_tool_calls"`
	MaxInputTokens          int           `koanf:"max_input_tokens"`
	MaxOutputTokens         int           `koanf:"max_output_tokens"`
	LLMTimeout              time.Duration `koanf:"llm_timeout"`
	ToolTimeout             time.Duration `koanf:"tool_timeout"`
	MaxRetries              int           `koanf:"max_retries"`
	EnableStreaming          bool          `koanf:"enable_streaming"`
	Temperature             float64       `koanf:"temperature"`
}

// MemoryConfig controls bounded memory.
type MemoryConfig struct {
	EpisodicMaxChars  int `koanf:"episodic_max_chars"`
	SemanticMaxChars  int `koanf:"semantic_max_chars"`
	ProceduralMaxChars int `koanf:"procedural_max_chars"`
	ConsolidateAt     int `koanf:"consolidate_at"` // percent
}

// CostConfig controls cost tracking.
type CostConfig struct {
	InlineDisplay bool    `koanf:"inline_display"`
	DailyBudget   float64 `koanf:"daily_budget"`
	MonthlyBudget float64 `koanf:"monthly_budget"`
}

// SecurityConfig controls safety rails.
type SecurityConfig struct {
	StrictApproval     bool     `koanf:"strict_approval"`
	ScanCredentials    bool     `koanf:"scan_credentials"`
	PathTraversalGuard bool     `koanf:"path_traversal_guard"`
	AllowedDirs        []string `koanf:"allowed_dirs"`
}

// MCPServerConfig defines an MCP server connection.
type MCPServerConfig struct {
	Name    string   `koanf:"name"`
	Command string   `koanf:"command"`
	Args    []string `koanf:"args"`
	Env     []string `koanf:"env"`
}

// SchedulerConfig holds scheduled task definitions.
type SchedulerConfig struct {
	Tasks []ScheduledTaskConfig `koanf:"tasks"`
}

// ScheduledTaskConfig defines a single scheduled task.
type ScheduledTaskConfig struct {
	ID       string `koanf:"id"`
	Name     string `koanf:"name"`
	Schedule string `koanf:"schedule"` // "every 30m" or "0 9 * * 1-5"
	Prompt   string `koanf:"prompt"`
	Channel  string `koanf:"channel"` // "telegram:chatid", "discord:channelid", "log"
	Enabled  bool   `koanf:"enabled"`
}

// DiscordConfig configures the Discord bot.
type DiscordConfig struct {
	Token string `koanf:"token"`
}

// SlackConfig configures the Slack bot.
type SlackConfig struct {
	BotToken string `koanf:"bot_token"`
	AppToken string `koanf:"app_token"`
}

// WhatsAppConfig configures the WhatsApp bot.
type WhatsAppConfig struct {
	PhoneNumberID string `koanf:"phone_number_id"`
	AccessToken   string `koanf:"access_token"`
	VerifyToken   string `koanf:"verify_token"`
}

// defaults returns the default config as a map.
func defaults() map[string]any {
	return map[string]any{
		"providers.default":              "anthropic",
		"providers.anthropic.api_key_env": "ANTHROPIC_API_KEY",
		"providers.anthropic.default_model": "claude-sonnet-4-20250514",

		"agent.max_turns":                  25,
		"agent.max_consecutive_tool_calls": 15,
		"agent.max_input_tokens":           180000,
		"agent.max_output_tokens":          16384,
		"agent.llm_timeout":                "120s",
		"agent.tool_timeout":               "60s",
		"agent.max_retries":                3,
		"agent.enable_streaming":           true,
		"agent.temperature":                0.0,

		"memory.episodic_max_chars":   8000,
		"memory.semantic_max_chars":   4000,
		"memory.procedural_max_chars": 2000,
		"memory.consolidate_at":       80,

		"cost.inline_display": true,
		"cost.daily_budget":   0.0,
		"cost.monthly_budget": 0.0,

		"security.strict_approval":      false,
		"security.scan_credentials":     true,
		"security.path_traversal_guard": true,
	}
}

// Load reads configuration from defaults, YAML file, and environment.
// Priority: env > file > defaults.
func Load(path string) (*Config, error) {
	k := koanf.New(".")

	// 1. Defaults.
	if err := k.Load(confmap.Provider(defaults(), "."), nil); err != nil {
		return nil, fmt.Errorf("loading defaults: %w", err)
	}

	// 2. Config file (optional).
	if path == "" {
		path = defaultConfigPath()
	}
	if _, err := os.Stat(path); err == nil {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("loading config file %s: %w", path, err)
		}
	}

	// 3. Environment variables (LITECLAW_AGENT_MAX_TURNS → agent.max_turns).
	if err := k.Load(env.Provider("LITECLAW_", ".", func(s string) string {
		s = strings.TrimPrefix(s, "LITECLAW_")
		s = strings.ToLower(s)
		s = strings.ReplaceAll(s, "_", ".")
		return s
	}), nil); err != nil {
		return nil, fmt.Errorf("loading env vars: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	return &cfg, nil
}

func defaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".liteclaw", "config.yaml")
}
