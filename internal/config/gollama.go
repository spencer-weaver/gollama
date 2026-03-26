package config

import "sync"

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

	HistoryMessages []Msg `json:"historyMessages,omitempty"`
}

// Msg is identical to the one used in chat_debug.go.
// We expose it here so that the config package can unmarshal it.
type Msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
