// Package chat provides a public API for sending messages to an Ollama server.
// Other Go modules can import this package to call Ollama programmatically
// without touching files, global state, or stdout.
package chat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// reThink strips <think>...</think> blocks emitted by reasoning models (e.g. qwen3).
var reThink = regexp.MustCompile(`(?s)<think>.*?</think>`)

// Config holds the connection and model parameters for an Ollama request.
type Config struct {
	Host         string
	Port         string
	Model        string
	System       string  // system prompt injected as the first message
	Temperature  float64 // 0.0–1.0
	MaxTokens    int     // -1 for unlimited
	ShowThinking bool    // if true, include <think> blocks in the returned text
}

// Msg is one turn in a conversation.
type Msg struct {
	Role    string // "user" | "assistant" | "system"
	Content string
}

// Chat sends a single prompt with no conversation history and returns the
// full assistant reply. It is a convenience wrapper around ChatWithHistory.
func Chat(cfg Config, prompt string) (string, error) {
	return ChatWithHistory(cfg, nil, prompt)
}

// buildMessages assembles the Ollama messages array from a Config, history, and prompt.
func buildMessages(cfg Config, history []Msg, prompt string) []map[string]string {
	messages := []map[string]string{}
	if cfg.System != "" {
		messages = append(messages, map[string]string{"role": "system", "content": cfg.System})
	}
	for _, m := range history {
		messages = append(messages, map[string]string{"role": m.Role, "content": m.Content})
	}
	messages = append(messages, map[string]string{"role": "user", "content": prompt})
	return messages
}

// buildBody assembles the full Ollama request payload.
func buildBody(cfg Config, history []Msg, prompt string) map[string]any {
	return map[string]any{
		"model":       cfg.Model,
		"temperature": cfg.Temperature,
		"max_tokens":  cfg.MaxTokens,
		"stream":      true,
		"think":       cfg.ShowThinking,
		"messages":    buildMessages(cfg, history, prompt),
	}
}

// ChatWithHistory sends a prompt along with prior conversation messages and
// returns the full assistant reply. The system prompt from cfg.System is
// always prepended. No output is written to stdout.
func ChatWithHistory(cfg Config, history []Msg, prompt string) (string, error) {
	body := buildBody(cfg, history, prompt)

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal body: %w", err)
	}

	url := fmt.Sprintf("http://%s:%s/api/chat", cfg.Host, cfg.Port)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(payload))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Ollama request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Ollama %d: %s", resp.StatusCode, string(msg))
	}

	result, err := collectReply(resp.Body)
	if err != nil {
		return "", err
	}
	if !cfg.ShowThinking {
		result = reThink.ReplaceAllString(result, "")
		// Catch any unclosed <think> block (model truncated mid-thought).
		if idx := strings.Index(result, "<think>"); idx >= 0 {
			result = result[:idx]
		}
	}
	return strings.TrimSpace(result), nil
}

// ── internal streaming types ──────────────────────────────────────────────────

type msgJSON struct {
	Message msgBody `json:"message"`
	Done    bool    `json:"done"`
}

type msgBody struct {
	Content string `json:"content"`
}

// collectReply reads the newline-delimited JSON stream from Ollama and
// concatenates all content fragments into a single string.
func collectReply(r io.Reader) (string, error) {
	var sb strings.Builder
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m msgJSON
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		content := m.Message.Content
		if content == "[DONE]" {
			break
		}
		sb.WriteString(content)
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return sb.String(), nil
}
