// Package session manages brainstorm session state: messages, completeness score,
// and the final plan. Sessions are persisted as JSON files so that brainstorm
// and plan steps can run in separate processes or be resumed after interruption.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Status tracks which phase the session is in.
type Status string

const (
	StatusBrainstorming Status = "brainstorming"
	StatusComplete      Status = "complete"
	StatusPlanned       Status = "planned"
)

// Message is one turn in a brainstorm conversation.
type Message struct {
	Role    string    `json:"role"`    // "user" | "assistant"
	Content string    `json:"content"`
	Time    time.Time `json:"time"`
}

// Session holds the full state of a brainstorm-to-plan workflow.
type Session struct {
	Topic     string    `json:"topic"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Status    Status    `json:"status"`
	Score     int       `json:"score"`     // last completeness score (0-100)
	Threshold int       `json:"threshold"` // score required to end brainstorm
	Messages  []Message `json:"messages"`
	Plan      string    `json:"plan,omitempty"`
}

// New creates a fresh session for the given topic.
func New(topic string, threshold int) *Session {
	now := time.Now()
	return &Session{
		Topic:     topic,
		CreatedAt: now,
		UpdatedAt: now,
		Status:    StatusBrainstorming,
		Threshold: threshold,
		Messages:  []Message{},
	}
}

// Load reads a session from disk.
func Load(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session %s: %w", path, err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse session %s: %w", path, err)
	}
	return &s, nil
}

// Save writes the session to disk.
func (s *Session) Save(path string) error {
	s.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// AddMessage appends a new turn to the session.
func (s *Session) AddMessage(role, content string) {
	s.Messages = append(s.Messages, Message{
		Role:    role,
		Content: content,
		Time:    time.Now(),
	})
}

// LastAssistantMessage returns the most recent assistant message, or empty string.
func (s *Session) LastAssistantMessage() string {
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Role == "assistant" {
			return s.Messages[i].Content
		}
	}
	return ""
}

// Transcript formats the session as a readable Q&A block for scoring and planning.
func (s *Session) Transcript() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Topic: %s\n\n", s.Topic)
	for _, m := range s.Messages {
		label := "User"
		if m.Role == "assistant" {
			label = "Interviewer"
		}
		fmt.Fprintf(&sb, "%s: %s\n\n", label, m.Content)
	}
	return strings.TrimSpace(sb.String())
}

// DefaultPath returns the default session file path for a topic.
func DefaultPath(sessionsDir, topic string) string {
	return filepath.Join(sessionsDir, slugify(topic)+".json")
}

var reSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(s)
	s = reSlugChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 48 {
		s = s[:48]
	}
	if s == "" {
		s = "session"
	}
	return s
}
