package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	gcontext "github.com/spencer-weaver/gollama/internal/context"
	"github.com/spencer-weaver/gollama/internal/history"
	"github.com/spencer-weaver/gollama/internal/llm"
)

const toolCallFormat = `
To call a tool, use this exact format in your response:
<tool>
tool: <toolname>
command: <commandname>
args: --argname value --argname2 value2
</tool>
Wait for the tool result before continuing. Do not guess argument names — use only the argument names shown in the tool specification above.
`

type Agent struct {
	client    *llm.Client
	history   *history.History
	context   *gcontext.ProjectContext
	gobinPath string
	system    string
}

func NewAgent(client *llm.Client, hist *history.History, ctx *gcontext.ProjectContext, gobinPath, systemBase string) *Agent {
	return &Agent{
		client:    client,
		history:   hist,
		context:   ctx,
		gobinPath: gobinPath,
		system:    systemBase,
	}
}

// buildToolSpecs fetches tool descriptions via gobin and returns them as a
// concatenated string of JSON blocks, one per tool. Tools that fail describe
// are silently skipped.
func (a *Agent) buildToolSpecs() string {
	gobin := a.gobinPath + "/gobin"

	listOut, err := runCommand(gobin, "agent", "list")
	if err != nil || len(listOut) == 0 {
		return ""
	}

	var tools []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(listOut), &tools); err != nil {
		return ""
	}

	var sb strings.Builder
	for _, t := range tools {
		spec, err := runCommand(gobin, "agent", "describe", t.Name)
		if err != nil || spec == "" {
			continue
		}
		sb.WriteString(spec)
		sb.WriteString("\n")
	}
	return sb.String()
}

// BuildSystemPrompt constructs the full system prompt by combining
// systemBase + available tools from gobin + project context if loaded.
func (a *Agent) BuildSystemPrompt() (string, error) {
	var sb strings.Builder
	sb.WriteString(a.system)

	// Fetch tool descriptions and append as tools section.
	if specs := a.buildToolSpecs(); specs != "" {
		sb.WriteString("\n\nAvailable tools — call using the <tool> block format shown below.\n")
		sb.WriteString("Each tool's full specification is listed. Use the exact command names and argument names shown.\n\n")
		sb.WriteString(specs)
		sb.WriteString(toolCallFormat)
	}

	// Inject absolute project directory so the model uses absolute paths.
	if a.context != nil {
		sb.WriteString(fmt.Sprintf("\nCurrent project directory: %s\nAlways use this as the base for all file paths when calling tools.\n", a.context.ProjectDir))
	}

	// Append project context if available.
	if a.context != nil && a.context.Exists() {
		readme, ctxContent, err := a.context.Load()
		if err != nil {
			return "", fmt.Errorf("agent: load project context: %w", err)
		}
		if readme != "" || ctxContent != "" {
			sb.WriteString("\n\n## Project Context\n")
			if readme != "" {
				sb.WriteString("\n### README\n")
				sb.WriteString(readme)
			}
			if ctxContent != "" {
				sb.WriteString("\n### .gollama/context.md\n")
				sb.WriteString(ctxContent)
			}
		}
	}

	return sb.String(), nil
}

// Run executes the agent loop for one user input.
// It sends the message to the model, handles any tool calls,
// streams the response to stdout, and saves history.
// Returns the assistant response string and an error.
func (a *Agent) Run(ctx context.Context, userInput string) (string, error) {
	a.history.Add("user", userInput)

	systemPrompt, err := a.BuildSystemPrompt()
	if err != nil {
		return "", err
	}

	messages := buildMessages(systemPrompt, a.history.All())

	// Stream initial response.
	response, err := a.streamResponse(ctx, messages)
	if err != nil {
		return "", err
	}

	// Handle tool calls in a loop until response contains no more tool calls.
	for {
		calls := Parse(response)
		if len(calls) == 0 {
			break
		}
		for _, call := range calls {
			result, execErr := Execute(a.gobinPath, call)
			if execErr != nil {
				result = fmt.Sprintf("error: %v", execErr)
			}
			status := "ok"
			if !strings.Contains(result, `"ok":true`) {
				var res struct {
					Error string `json:"error"`
				}
				if json.Unmarshal([]byte(result), &res) == nil && res.Error != "" {
					status = "failed: " + res.Error
				} else {
					status = "failed"
				}
			}
			fmt.Printf("\n[tool: %s %s] %s\n", call.Tool, call.Command, status)

			messages = append(messages,
				llm.Message{Role: "assistant", Content: response},
				llm.Message{Role: "user", Content: result},
			)
		}
		followUp, err := a.client.Complete(ctx, messages)
		if err != nil {
			return "", fmt.Errorf("agent: follow-up completion: %w", err)
		}
		fmt.Print(followUp)
		response = followUp
		messages = append(messages, llm.Message{Role: "assistant", Content: followUp})
	}

	a.history.Add("assistant", response)
	if err := a.history.Save(); err != nil {
		return response, fmt.Errorf("agent: save history: %w", err)
	}
	return response, nil
}

func (a *Agent) streamResponse(ctx context.Context, messages []llm.Message) (string, error) {
	ch, err := a.client.Stream(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("agent: stream: %w", err)
	}
	var sb strings.Builder
	for event := range ch {
		switch event.Type {
		case "text":
			fmt.Print(event.Content)
			sb.WriteString(event.Content)
		case "error":
			return sb.String(), fmt.Errorf("agent: stream error: %s", event.Content)
		}
	}
	return sb.String(), nil
}

func buildMessages(system string, history []llm.Message) []llm.Message {
	msgs := make([]llm.Message, 0, len(history)+1)
	msgs = append(msgs, llm.Message{Role: "system", Content: system})
	msgs = append(msgs, history...)
	return msgs
}

func runCommand(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
