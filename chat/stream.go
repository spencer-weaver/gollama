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

// streamChunks reads the newline-delimited Ollama stream and calls fn for
// each content chunk, optionally filtering <think>…</think> blocks.
func streamChunks(r io.Reader, showThinking bool, fn func(string) error) error {
	scanner := bufio.NewScanner(r)
	inThink := false
	var thinkBuf strings.Builder

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m msgJSON
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		chunk := m.Message.Content
		if chunk == "" {
			continue
		}

		if showThinking {
			if err := fn(chunk); err != nil {
				return err
			}
			continue
		}

		// ── filter <think> blocks ─────────────────────────────────────────
		// The opening/closing tags may arrive split across multiple chunks,
		// so we buffer inside the think block and drain on </think>.
		if inThink {
			thinkBuf.WriteString(chunk)
			combined := thinkBuf.String()
			if idx := strings.Index(combined, "</think>"); idx >= 0 {
				inThink = false
				after := combined[idx+len("</think>"):]
				thinkBuf.Reset()
				if trimmed := strings.TrimLeft(after, "\n"); trimmed != "" {
					if err := fn(trimmed); err != nil {
						return err
					}
				}
			}
		} else {
			if idx := strings.Index(chunk, "<think>"); idx >= 0 {
				// emit anything before the opening tag
				if before := chunk[:idx]; before != "" {
					if err := fn(before); err != nil {
						return err
					}
				}
				inThink = true
				rest := chunk[idx+len("<think>"):]
				thinkBuf.Reset()
				thinkBuf.WriteString(rest)
				// check if </think> is already in this same chunk
				if endIdx := strings.Index(thinkBuf.String(), "</think>"); endIdx >= 0 {
					inThink = false
					after := thinkBuf.String()[endIdx+len("</think>"):]
					thinkBuf.Reset()
					if trimmed := strings.TrimLeft(after, "\n"); trimmed != "" {
						if err := fn(trimmed); err != nil {
							return err
						}
					}
				}
			} else {
				if err := fn(chunk); err != nil {
					return err
				}
			}
		}
	}

	return scanner.Err()
}
