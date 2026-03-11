package config

import "sync"

var globalCfg *GollamaConfig

func SetGlobal(c *GollamaConfig) { globalCfg = c }
func GetGlobal() *GollamaConfig  { return globalCfg }

var once sync.Once
var cfgMu sync.Mutex // protects globalCfg during writes

// GollamaConfig is read from `config/gollama.json`.
type GollamaConfig struct {
	Host        string  `json:"host"`
	Port        string  `json:"port"`
	Model       string  `json:"model"`       // Model name registered in Ollama
	System      string  `json:"system"`      // Prompt‑level personality / system message
	Temperature float64 `json:"temperature"` // 0.0–1.0
	Token       string  `json:"token"`
	MaxTokens   int     `json:"maxTokens"`
	Stream      bool    `json:"stream"`  // if true, use a streamed response
	History     bool    `json:"history"` // if true, keep the conversation history

	HistoryMessages []Msg `json:"historyMessages,omitempty"`
}

// Msg is identical to the one used in chat_debug.go.
// We expose it here so that the config package can unmarshal it.
type Msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
