package tools

import (
	"fmt"
	"os"
)

// ReadFileTool reads a local file and returns its contents.
type ReadFileTool struct{}

func NewReadFileTool() *ReadFileTool { return &ReadFileTool{} }

func (r *ReadFileTool) Name() string { return "read_file" }
func (r *ReadFileTool) Description() string {
	return `Read a local file. Args: {"path": "/path/to/file"}`
}

func (r *ReadFileTool) Execute(args map[string]any) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("path arg required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}
