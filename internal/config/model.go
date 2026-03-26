package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ModelConfig defines a named agent profile stored in models/<name>.json.
// Fields that are zero/empty do not override the global GollamaConfig.
type ModelConfig struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Model       string   `json:"model,omitempty"`       // overrides global model if set
	System      string   `json:"system,omitempty"`      // replaces global system prompt if set
	Temperature float64  `json:"temperature,omitempty"` // overrides global temperature if non-zero
	MaxTokens   int      `json:"maxTokens,omitempty"`   // overrides global maxTokens if non-zero
	ToolMode    string   `json:"toolMode,omitempty"`    // e.g. "research"
	Tools       []string `json:"tools,omitempty"`       // explicit tool list; overrides toolMode default
}

// LoadModelConfig reads models/<name>.json from the given directory.
func LoadModelConfig(modelsDir, name string) (*ModelConfig, error) {
	path := filepath.Join(modelsDir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("model %q not found at %s: %w", name, path, err)
	}
	var mc ModelConfig
	if err := json.Unmarshal(data, &mc); err != nil {
		return nil, fmt.Errorf("parse model %q: %w", name, err)
	}
	return &mc, nil
}
