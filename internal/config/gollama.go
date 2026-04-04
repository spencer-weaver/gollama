package config

type GollamaConfig struct {
	Endpoint         string `json:"endpoint"`
	Model            string `json:"model"`
	APIKeyEnv        string `json:"api_key_env"`
	MaxTokens        int    `json:"max_tokens"`
	SystemPromptBase string `json:"system_prompt_base"`
	GobinPath        string `json:"gobin_path"`
	MaxHistoryTurns  int    `json:"max_history_turns"`
}
