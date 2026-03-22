package tools

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// FindFilesTool finds files matching a glob pattern under a directory.
type FindFilesTool struct{}

func NewFindFilesTool() *FindFilesTool { return &FindFilesTool{} }

func (f *FindFilesTool) Name() string { return "find_files" }
func (f *FindFilesTool) Description() string {
	return `Find files by name pattern. Args: {"path": ".", "pattern": "*.go"}`
}

func (f *FindFilesTool) Execute(args map[string]any) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return "", fmt.Errorf("pattern arg required")
	}

	var sb strings.Builder
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && p != path {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			matched, _ := filepath.Match(pattern, d.Name())
			if matched {
				rel, _ := filepath.Rel(path, p)
				sb.WriteString(rel + "\n")
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk %s: %w", path, err)
	}
	return sb.String(), nil
}
