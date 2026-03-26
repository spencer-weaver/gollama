package chat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ChatStream streams the model response chunk by chunk, calling fn for each
// content chunk as it arrives from Ollama. <think> blocks are silently
// discarded unless cfg.ShowThinking is true. Return a non-nil error from fn
// to abort the stream early.
func ChatStream(cfg Config, history []Msg, prompt string, fn func(chunk string) error) error {
	payload, err := json.Marshal(buildBody(cfg, history, prompt))
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}

	url := fmt.Sprintf("http://%s:%s/api/chat", cfg.Host, cfg.Port)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("Ollama request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Ollama %d: %s", resp.StatusCode, string(msg))
	}

	return streamChunks(resp.Body, cfg.ShowThinking, fn)
}

// streamChunks assembles the full response from the Ollama stream, strips
// think blocks on the assembled string, then delivers the result via fn.
func streamChunks(r io.Reader, showThinking bool, fn func(string) error) error {
	scanner := bufio.NewScanner(r)
	var sb strings.Builder

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m msgJSON
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		if m.Message.Content != "" {
			sb.WriteString(m.Message.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	result := sb.String()
	if !showThinking {
		result = stripThinkBlocks(result)
	}
	if result == "" {
		return nil
	}
	return fn(result)
}
