package config

import (
	"os"
	"sync"
)

var globalCfg *GollamaConfig
var globalRoot string // absolute path to the project root (bin/..)

func SetGlobal(c *GollamaConfig) { globalCfg = c }
func GetGlobal() *GollamaConfig  { return globalCfg }

func SetGlobalRoot(root string) { globalRoot = root }
func GetGlobalRoot() string     { return globalRoot }

var once sync.Once
var cfgMu sync.Mutex // protects globalCfg during writes

// DefaultConfig returns a GollamaConfig with sensible built-in defaults.
// Any field can be overridden by a config file or CLI flags.
func DefaultConfig() *GollamaConfig {
	return &GollamaConfig{
		Host:        "localhost",
		Port:        "11434",
		Model:       "phi4-mini",
		Temperature: 0.7,
		MaxTokens:   -1,
		History:     false,
		ListenAddr:  ":8080",
		AgentModel:  "ha-agent",
		SearXNGURL:  "http://localhost:8080/search",
		HAHost:      "http://homeassistant.local:8123",
	}
}

// GollamaConfig is read from `config/gollama.json`.
type GollamaConfig struct {
	Host        string  `json:"host"`
	Port        string  `json:"port"`
	Model       string  `json:"model"`       // Model name registered in Ollama
	System      string  `json:"system"`      // Prompt‑level personality / system message
	Temperature float64 `json:"temperature"` // 0.0–1.0
	Token       string  `json:"token"`
	MaxTokens   int     `json:"maxTokens"`
	Stream      bool    `json:"stream"`    // if true, use a streamed response
	History     bool    `json:"history"`   // if true, keep the conversation history
	ModelsDir   string  `json:"modelsDir"` // directory for agent model configs (default: "models")

	// HTTP server settings (used when running as a conversation agent server).
	ListenAddr string `json:"listenAddr"` // e.g. ":8080"
	AgentModel string `json:"agentModel"` // model config name to load for the HA agent

	// External service config.
	SearXNGURL string `json:"searxngURL"` // SearXNG base URL (default http://localhost:8080/search)
	HAHost     string `json:"haHost"`     // Home Assistant base URL (default http://homeassistant.local:8123)
	HAToken    string `json:"haToken"`    // Home Assistant long-lived access token

	HistoryMessages []Msg `json:"historyMessages,omitempty"`
}

// ApplyEnv fills in any fields that are still at their compiled-in default
// with values from environment variables. Priority: config file > env > default.
// The check against DefaultConfig() values is how we detect "not explicitly set
// by the config file" — if the config file wrote the same value as the default
// that's indistinguishable, but in practice users override or leave blank.
func (c *GollamaConfig) ApplyEnv() {
	d := DefaultConfig()
	if v := os.Getenv("HA_TOKEN"); v != "" && c.HAToken == d.HAToken {
		c.HAToken = v
	}
	if v := os.Getenv("HA_HOST"); v != "" && c.HAHost == d.HAHost {
		c.HAHost = v
	}
	if v := os.Getenv("SEARXNG_URL"); v != "" && c.SearXNGURL == d.SearXNGURL {
		c.SearXNGURL = v
	}
}

// Msg is identical to the one used in chat_debug.go.
// We expose it here so that the config package can unmarshal it.
type Msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
