package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

const (
	runCommandMaxOutput  = 32 * 1024 // 32 KB
	runCommandDefaultCwd = "."
	runCommandDefaultTimeout = 30
)

// RunCommandTool executes a shell command and returns combined stdout+stderr.
type RunCommandTool struct{}

func NewRunCommandTool() *RunCommandTool { return &RunCommandTool{} }

func (r *RunCommandTool) Name() string { return "run_command" }
func (r *RunCommandTool) Description() string {
	return `Execute a shell command. Args: {"command": "go test ./...", "cwd": "/path", "timeout_seconds": 30}`
}

func (r *RunCommandTool) Execute(args map[string]any) (string, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command arg required")
	}

	cwd, _ := args["cwd"].(string)
	if cwd == "" {
		cwd = runCommandDefaultCwd
	}

	timeoutSecs := runCommandDefaultTimeout
	if v, ok := args["timeout_seconds"].(float64); ok && v > 0 {
		timeoutSecs = int(v)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = cwd

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	_ = cmd.Run() // capture output even on non-zero exit

	out := buf.Bytes()
	if len(out) > runCommandMaxOutput {
		out = append(out[:runCommandMaxOutput], []byte("\n[output truncated]")...)
	}
	return string(out), nil
}
