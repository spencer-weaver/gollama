package tools

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ListDirTool lists the contents of a directory, optionally recursive.
type ListDirTool struct{}

func NewListDirTool() *ListDirTool { return &ListDirTool{} }

func (l *ListDirTool) Name() string { return "list_dir" }
func (l *ListDirTool) Description() string {
	return `List files in a directory. Args: {"path": "/dir", "recursive": true}`
}

func (l *ListDirTool) Execute(args map[string]any) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	recursive, _ := args["recursive"].(bool)

	var sb strings.Builder
	if recursive {
		err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// Skip hidden directories (e.g. .git) but still show the entry itself.
			if d.IsDir() && strings.HasPrefix(d.Name(), ".") && p != path {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(path, p)
			if d.IsDir() {
				sb.WriteString(fmt.Sprintf("%s/\n", rel))
			} else {
				info, err := d.Info()
				if err != nil {
					sb.WriteString(fmt.Sprintf("%s\n", rel))
					return nil
				}
				sb.WriteString(fmt.Sprintf("%s (%d bytes)\n", rel, info.Size()))
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walk %s: %w", path, err)
		}
	} else {
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", fmt.Errorf("readdir %s: %w", path, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				sb.WriteString(fmt.Sprintf("%s/\n", e.Name()))
			} else {
				info, _ := e.Info()
				if info != nil {
					sb.WriteString(fmt.Sprintf("%s (%d bytes)\n", e.Name(), info.Size()))
				} else {
					sb.WriteString(fmt.Sprintf("%s\n", e.Name()))
				}
			}
		}
	}

	return sb.String(), nil
}
