package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

type ToolCall struct {
	Tool    string
	Command string
	Args    []string
}

// Parse scans a model response for tool call blocks in this format:
//
//	<tool>
//	tool: <toolname>
//	command: <commandname>
//	args: --flag value --flag2 value2
//	</tool>
//
// Returns all found tool calls.
func Parse(response string) []ToolCall {
	var calls []ToolCall
	remaining := response
	for {
		start := strings.Index(remaining, "<tool>")
		if start == -1 {
			break
		}
		end := strings.Index(remaining[start:], "</tool>")
		if end == -1 {
			break
		}
		block := remaining[start+len("<tool>") : start+end]
		remaining = remaining[start+end+len("</tool>"):]

		var call ToolCall
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if after, ok := strings.CutPrefix(line, "tool:"); ok {
				call.Tool = strings.TrimSpace(after)
			} else if after, ok := strings.CutPrefix(line, "command:"); ok {
				call.Command = strings.TrimSpace(after)
			} else if after, ok := strings.CutPrefix(line, "args:"); ok {
				raw := strings.TrimSpace(after)
				if raw != "" {
					call.Args = strings.Fields(raw)
				}
			}
		}
		if call.Tool != "" && call.Command != "" {
			calls = append(calls, call)
		}
	}
	return calls
}

// Execute runs a tool call via gobin agent run.
// gobinPath is the path to the gobin binary.
// Returns the raw JSON output string and an error.
func Execute(gobinPath string, call ToolCall) (string, error) {
	cmdArgs := []string{"agent", "run", call.Tool, call.Command}
	cmdArgs = append(cmdArgs, call.Args...)

	cmd := exec.Command(gobinPath+"/gobin", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gobin agent run %s %s: %w: %s", call.Tool, call.Command, err, string(out))
	}
	return string(out), nil
}
