// Package tools provides the tool-use framework for gollama agents.
// Tools are called by the model via <tool_calls> blocks in its response.
// The registry executes them and feeds results back into the conversation.
package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Tool is the interface all tools must implement.
type Tool interface {
	Name() string
	Description() string // one-line description included in system prompt
	Execute(args map[string]any) (string, error)
}

// ToolCall is a single tool invocation decoded from model output.
type ToolCall struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

// Registry holds registered tools by name.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// ToolGuide returns the system prompt prefix describing mandatory tool use.
// Only tools in the allowed list are included. The guide is prepended so it
// receives priority attention from the model.
func (r *Registry) ToolGuide(allowed []string) string {
	var sb strings.Builder
	sb.WriteString("## TOOLS\n")
	sb.WriteString("When you need to use a tool, output ONLY a tool_calls block in this EXACT format and then stop:\n\n")

	sb.WriteString("<tool_calls>\n")
	sb.WriteString("[{\"tool\": \"<tool_name>\", \"args\": {<args>}}]\n")
	sb.WriteString("</tool_calls>\n\n")

	// Concrete example using the first real allowed tool name.
	if len(allowed) > 0 {
		exampleTool := allowed[0]
		sb.WriteString("Example:\n")
		sb.WriteString("<tool_calls>\n")
		sb.WriteString(fmt.Sprintf("[{\"tool\": \"%s\", \"args\": {}}]\n", exampleTool))
		sb.WriteString("</tool_calls>\n\n")
	}

	sb.WriteString("Rules:\n")
	sb.WriteString("- Use ONLY the <tool_calls>[...]</tool_calls> format shown above — no other format\n")
	sb.WriteString("- Output the <tool_calls> block and STOP immediately — do not write anything after it\n")
	sb.WriteString("- Wait for tool results before continuing your response\n")
	sb.WriteString("- Do not guess or fabricate results — always use a tool to retrieve real data\n")
	sb.WriteString("- You may call multiple tools in one batch by adding more objects to the JSON array\n\n")

	sb.WriteString("Available tools:\n")
	for _, name := range allowed {
		if t, ok := r.tools[name]; ok {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", t.Name(), t.Description()))
		}
	}
	sb.WriteString("\n---\n\n")
	return sb.String()
}

// toolCallPattern matches <tool_calls>...</tool_calls> blocks (including newlines).
var toolCallPattern = regexp.MustCompile(`(?s)<tool_calls>\s*(.*?)\s*</tool_calls>`)

// ParseToolCalls extracts tool calls from a model response using the
// <tool_calls>[...]</tool_calls> format. Returns the parsed calls, the response
// with the block stripped, and whether any calls were found.
// This package-level function handles format 1 only. Use Registry.ParseToolCalls
// for multi-format detection including per-tool tags from llama3.2-style models.
func ParseToolCalls(response string) ([]ToolCall, string, bool) {
	match := toolCallPattern.FindStringSubmatch(response)
	if match == nil {
		return nil, response, false
	}
	var calls []ToolCall
	raw := match[1]
	if err := json.Unmarshal([]byte(raw), &calls); err != nil {
		// Model may have truncated the JSON array — try appending the closing bracket.
		if err2 := json.Unmarshal([]byte(strings.TrimSpace(raw)+"]"), &calls); err2 != nil {
			return nil, response, false
		}
	}
	cleaned := strings.TrimSpace(toolCallPattern.ReplaceAllString(response, ""))
	return calls, cleaned, true
}

// ParseToolCalls extracts tool calls from a model response, handling two formats:
//
//  1. <tool_calls>[{"tool": "name", "args": {...}}]</tool_calls>
//     (canonical format, all models)
//
//  2. <tool_name>{...}</tool_name>
//     (per-tool tag format emitted by llama3.2:3b and similar models)
//
// Format 1 is tried first. Format 2 is tried only for tool names registered
// in this registry. Returns the parsed calls, the response with the tool block
// stripped, and whether any calls were found.
func (r *Registry) ParseToolCalls(response string) ([]ToolCall, string, bool) {
	// Format 1 — canonical <tool_calls> block.
	if calls, cleaned, ok := ParseToolCalls(response); ok {
		return calls, cleaned, ok
	}

	// Format 2 — <tool_name>{...}</tool_name> per-tool tag.
	for toolName := range r.tools {
		openTag := "<" + toolName + ">"
		closeTag := "</" + toolName + ">"

		start := strings.Index(response, openTag)
		if start == -1 {
			continue
		}
		rest := response[start+len(openTag):]
		end := strings.Index(rest, closeTag)
		if end == -1 {
			continue
		}

		body := strings.TrimSpace(rest[:end])
		var args map[string]any
		if err := json.Unmarshal([]byte(body), &args); err != nil {
			continue
		}

		before := response[:start]
		after := rest[end+len(closeTag):]
		cleaned := strings.TrimSpace(before + after)
		return []ToolCall{{Tool: toolName, Args: args}}, cleaned, true
	}

	return nil, response, false
}

// Execute runs a single tool call and returns a formatted result string.
func (r *Registry) Execute(call ToolCall) string {
	t, ok := r.tools[call.Tool]
	if !ok {
		return fmt.Sprintf("[tool %q not found]", call.Tool)
	}
	result, err := t.Execute(call.Args)
	if err != nil {
		return fmt.Sprintf("[%s error: %v]", call.Tool, err)
	}
	return result
}

// RunToolLoop executes all tool calls and returns a combined results message
// ready to be sent back to the model as a user turn.
func (r *Registry) RunToolLoop(calls []ToolCall) string {
	var sb strings.Builder
	sb.WriteString("Tool results:\n")
	for i, call := range calls {
		result := r.Execute(call)
		sb.WriteString(fmt.Sprintf("\n[%d] %s:\n%s\n", i+1, call.Tool, result))
	}
	return sb.String()
}
