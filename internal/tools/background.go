package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/spencer-weaver/gollama/internal/tasks"
)

// SpawnBackgroundTool runs a shell command as a non-blocking background task.
// Returns a task ID immediately; result is delivered via the task Manager.
type SpawnBackgroundTool struct{ mgr *tasks.Manager }

func NewSpawnBackgroundTool(m *tasks.Manager) *SpawnBackgroundTool {
	return &SpawnBackgroundTool{mgr: m}
}

func (t *SpawnBackgroundTool) Name() string { return "spawn_background" }
func (t *SpawnBackgroundTool) Description() string {
	return `Run a shell command as a background task (non-blocking). Args: {"label": "build", "command": "go build ./..."}`
}

func (t *SpawnBackgroundTool) Execute(args map[string]any) (string, error) {
	label, _ := args["label"].(string)
	command, _ := args["command"].(string)
	if command == "" {
		return "", fmt.Errorf("command required")
	}
	if label == "" {
		label = command
	}
	id := t.mgr.Spawn(label, func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		cmd.Run()
		out := buf.String()
		if len(out) > 500 {
			out = out[:500] + "\n[truncated]"
		}
		return out, nil
	})
	return fmt.Sprintf("started task %s", id), nil
}

// AskClaudeTool delegates a prompt to the claude CLI as a background task.
// Uses claude -p (print/non-interactive mode) with a 3-minute timeout.
type AskClaudeTool struct{ mgr *tasks.Manager }

func NewAskClaudeTool(m *tasks.Manager) *AskClaudeTool {
	return &AskClaudeTool{mgr: m}
}

func (t *AskClaudeTool) Name() string { return "ask_claude" }
func (t *AskClaudeTool) Description() string {
	return `Delegate a complex reasoning or code task to Claude. Runs in background. Args: {"prompt": "..."}`
}

func (t *AskClaudeTool) Execute(args map[string]any) (string, error) {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return "", fmt.Errorf("prompt required")
	}
	label := "claude: " + prompt
	if len(label) > 50 {
		label = label[:50] + "..."
	}
	id := t.mgr.Spawn(label, func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "claude", "-p", "--dangerously-skip-permissions", prompt)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		cmd.Run()
		out := buf.String()
		if len(out) > 800 {
			out = out[:800] + "\n[truncated]"
		}
		return out, nil
	})
	return fmt.Sprintf("delegated to Claude, task %s", id), nil
}
