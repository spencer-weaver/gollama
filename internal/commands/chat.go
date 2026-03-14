package commands

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spencer-weaver/gollama/internal/config"
)

// 2️⃣  Main handler – builds the request, calls Ollama, updates history.
func ChatHandler(args []string) error {
	// --- 2️⃣.1  Parse command‑line flags ------------------------------------------------
	if len(args) == 0 {
		return fmt.Errorf("no user prompt supplied")
	}
	userPrompt := strings.Join(args, " ")

	cfg := config.GetGlobal()

	// --- 2️⃣.2  Build messages slice ---------------------------------------------------
	messages := []config.Msg{}

	// system prompt
	messages = append(messages, config.Msg{Role: "system", Content: cfg.System})

	// stored conversation history (if any)
	if cfg.History && len(cfg.HistoryMessages) > 0 {
		messages = append(messages, cfg.HistoryMessages...)
	}

	// --- 2️⃣.3  Append the new user message ---------------------------------------------
	messages = append(messages, config.Msg{Role: "user", Content: userPrompt})

	// --- 2️⃣.3  Build the request payload -----------------------------------------------
	bodyMap := map[string]any{
		"model":       cfg.Model,
		"temperature": cfg.Temperature,
		"max_tokens":  cfg.MaxTokens,
		"stream":      true,
		"messages":    messages,
	}

	// --- 2️⃣.4  Send request to Ollama ----------------------------------------------------
	assistantMsg, err := callOllama(bodyMap, cfg)
	if err != nil {
		return err
	}

	// --- 2️⃣.5  Persist history (if enabled) ---------------------------------------------
	if cfg.History {
		// Append the user prompt
		cfg.HistoryMessages = append(cfg.HistoryMessages, config.Msg{Role: "user", Content: userPrompt})
		// Append the agent reply
		cfg.HistoryMessages = append(cfg.HistoryMessages, config.Msg{Role: "assistant", Content: assistantMsg})

		if err := config.SaveConfigFile(config.GetGlobalPath(), cfg); err != nil {
			return fmt.Errorf("failed to persist history: %w", err)
		}
	}

	return nil
}

// 2️⃣  Call Ollama and stream the reply -----------------------------------------------
func callOllama(body map[string]any, cfg *config.GollamaConfig) (string, error) {
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

	// streamReply will echo to stdout and return the assistant text.
	return streamReply(resp.Body)
}

type msgJSON struct {
	Model     string  `json:"model"`
	CreatedAt string  `json:"created_at"`
	Message   msgBody `json:"message"`
	Done      bool    `json:"done"`
}
type msgBody struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// streamReply reads the server‑sent event stream from Ollama and
// concatenates all "content" fragments that appear in the
// "delta" objects.  It also stops when the stream emits "[DONE]".
func streamReply(r io.Reader) (string, error) {
	var assistant strings.Builder
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			// Empty line – separator between events
			continue
		}

		// Try to decode the line as JSON
		var m msgJSON
		if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
			// If it isn’t JSON, ignore or log it
			fmt.Printf("non‑JSON line: %q\n", trimmed)
			continue
		}

		// Skip empty content (e.g. the server sometimes sends “” or “ ”)
		if strings.TrimSpace(m.Message.Content) == "" {
			continue
		}

		// Strip the prefix and any surrounding whitespace.
		raw := m.Message.Content

		// Ollama signals stream end with "[DONE]".
		if raw == "[DONE]" {
			break
		}

		// (Optional) print the raw JSON for debugging; you can remove it if you
		// prefer the final answer only.
		fmt.Print(raw)
		assistant.WriteString(raw)

		// All choice objects are in the same order as sent by Ollama; we just
		// care about the textual payload.
		// for _, c := range m. {
		// 	if c.Delta.Content != "" {
		// 		assistant.WriteString(c.Delta.Content)
		// 	}
		// }
	}

	// Return any scanner error that happened while reading.
	if err := scanner.Err(); err != nil {
		return "", err
	}

	fmt.Println()

	// Finally, return the collected assistant text.
	return assistant.String(), nil
}
