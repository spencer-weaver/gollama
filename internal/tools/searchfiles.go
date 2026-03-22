package tools

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SearchFilesTool searches file contents for a regex pattern (grep-like).
type SearchFilesTool struct{}

func NewSearchFilesTool() *SearchFilesTool { return &SearchFilesTool{} }

func (s *SearchFilesTool) Name() string { return "search_files" }
func (s *SearchFilesTool) Description() string {
	return `Search file contents for a regex pattern. Args: {"path": ".", "pattern": "func.*Handler", "glob": "*.go", "max_results": 50}`
}

func (s *SearchFilesTool) Execute(args map[string]any) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return "", fmt.Errorf("pattern arg required")
	}
	glob, _ := args["glob"].(string)

	maxResults := 50
	if v, ok := args["max_results"].(float64); ok && v > 0 {
		maxResults = int(v)
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}

	var sb strings.Builder
	count := 0

	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && p != path {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if glob != "" {
			matched, _ := filepath.Match(glob, d.Name())
			if !matched {
				return nil
			}
		}

		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()

		rel, _ := filepath.Rel(path, p)
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				sb.WriteString(fmt.Sprintf("%s:%d: %s\n", rel, lineNum, line))
				count++
				if count >= maxResults {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk %s: %w", path, err)
	}
	return sb.String(), nil
}
