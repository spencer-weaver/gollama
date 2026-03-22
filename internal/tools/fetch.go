package tools

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	reHTMLTag    = regexp.MustCompile(`<[^>]+>`)
	reWhitespace = regexp.MustCompile(`[ \t]+`)
)

// FetchURLTool fetches the text content of a URL.
type FetchURLTool struct {
	client *http.Client
}

func NewFetchURLTool() *FetchURLTool {
	return &FetchURLTool{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (f *FetchURLTool) Name() string { return "fetch_url" }
func (f *FetchURLTool) Description() string {
	return `Fetch content from a URL. Args: {"url": "https://..."}`
}

func (f *FetchURLTool) Execute(args map[string]any) (string, error) {
	url, ok := args["url"].(string)
	if !ok || url == "" {
		return "", fmt.Errorf("url arg required")
	}
	resp, err := f.client.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	// Limit to 64 KB to avoid flooding context
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml") {
		text := reHTMLTag.ReplaceAllString(string(body), " ")
		text = reWhitespace.ReplaceAllString(text, " ")
		// Collapse runs of blank lines down to one.
		lines := strings.Split(text, "\n")
		var out []string
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l != "" {
				out = append(out, l)
			}
		}
		return strings.Join(out, "\n"), nil
	}
	return string(body), nil
}
