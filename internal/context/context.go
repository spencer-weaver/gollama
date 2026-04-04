package context

import (
	"errors"
	"os"
	"path/filepath"
)

type ProjectContext struct {
	ProjectDir  string
	ReadmePath  string
	ContextPath string
}

func NewProjectContext(projectDir string) *ProjectContext {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		abs = projectDir
	}
	return &ProjectContext{
		ProjectDir:  abs,
		ReadmePath:  filepath.Join(abs, "README.md"),
		ContextPath: filepath.Join(abs, ".gollama", "context.md"),
	}
}

// Load reads both files. Returns empty strings if files don't exist — not an error.
func (p *ProjectContext) Load() (readme string, context string, err error) {
	readme, err = readOptional(p.ReadmePath)
	if err != nil {
		return "", "", err
	}
	context, err = readOptional(p.ContextPath)
	if err != nil {
		return "", "", err
	}
	return readme, context, nil
}

// SaveReadme writes README.md content to disk.
func (p *ProjectContext) SaveReadme(content string) error {
	return writeFile(p.ReadmePath, content)
}

// SaveContext writes .gollama/context.md content to disk.
func (p *ProjectContext) SaveContext(content string) error {
	return writeFile(p.ContextPath, content)
}

// Exists returns true if README.md exists in the project directory.
func (p *ProjectContext) Exists() bool {
	_, err := os.Stat(p.ReadmePath)
	return err == nil
}

func readOptional(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
