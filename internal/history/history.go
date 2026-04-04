package history

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/spencer-weaver/gollama/internal/llm"
)

type History struct {
	SessionName string
	Messages    []llm.Message
	path        string
}

func NewHistory(sessionName string) *History {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	path := filepath.Join(home, ".praxis", "sessions", sessionName+".json")
	return &History{
		SessionName: sessionName,
		path:        path,
	}
}

func (h *History) Load() error {
	data, err := os.ReadFile(h.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &h.Messages)
}

func (h *History) Save() error {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(h.Messages, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(h.path, data, 0o644)
}

func (h *History) Add(role, content string) {
	h.Messages = append(h.Messages, llm.Message{Role: role, Content: content})
}

func (h *History) Trim(maxTurns int) {
	limit := maxTurns * 2
	if limit < 2 {
		limit = 2
	}
	if len(h.Messages) > limit {
		h.Messages = h.Messages[len(h.Messages)-limit:]
	}
}

func (h *History) Clear() {
	h.Messages = nil
}

func (h *History) All() []llm.Message {
	out := make([]llm.Message, len(h.Messages))
	copy(out, h.Messages)
	return out
}
