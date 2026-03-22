package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileTool writes or appends content to a local file.
type WriteFileTool struct{}

func NewWriteFileTool() *WriteFileTool { return &WriteFileTool{} }

func (w *WriteFileTool) Name() string { return "write_file" }
func (w *WriteFileTool) Description() string {
	return `Write content to a local file. Args: {"path": "/path/to/file", "content": "...", "append": false}`
}

func (w *WriteFileTool) Execute(args map[string]any) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("path arg required")
	}
	content, ok := args["content"].(string)
	if !ok {
		return "", fmt.Errorf("content arg required")
	}
	append_, _ := args["append"].(bool)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	if append_ {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return "", fmt.Errorf("open %s: %w", path, err)
		}
		defer f.Close()
		n, err := f.WriteString(content)
		if err != nil {
			return "", fmt.Errorf("write %s: %w", path, err)
		}
		return fmt.Sprintf("appended %d bytes to %s", n, path), nil
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}
