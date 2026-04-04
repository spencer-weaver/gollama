package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type StreamEvent struct {
	Type    string // "text", "done", or "error"
	Content string
}

type streamDelta struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// Stream sends a streaming chat completion request.
// Returns a channel of StreamEvents. Closes the channel when done.
// Runs in a goroutine — caller reads from channel until closed.
func (c *Client) Stream(ctx context.Context, messages []Message) (<-chan StreamEvent, error) {
	body, err := json.Marshal(chatRequest{
		Model:     c.Model,
		Messages:  messages,
		MaxTokens: c.MaxTokens,
		Stream:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: do request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("llm: unexpected status %d: %s", resp.StatusCode, string(b))
	}

	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				ch <- StreamEvent{Type: "done"}
				return
			}

			var delta streamDelta
			if err := json.Unmarshal([]byte(data), &delta); err != nil {
				select {
				case ch <- StreamEvent{Type: "error", Content: fmt.Sprintf("llm: parse delta: %v", err)}:
				case <-ctx.Done():
				}
				return
			}

			if len(delta.Choices) > 0 {
				content := delta.Choices[0].Delta.Content
				if content != "" {
					select {
					case ch <- StreamEvent{Type: "text", Content: content}:
					case <-ctx.Done():
						return
					}
				}
			}
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			select {
			case ch <- StreamEvent{Type: "error", Content: fmt.Sprintf("llm: read stream: %v", err)}:
			case <-ctx.Done():
			}
		}
	}()

	return ch, nil
}
