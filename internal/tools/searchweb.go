package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultSearXNGURL = "http://localhost:8080/search"

// SearchWebTool queries a SearXNG instance and returns the top results.
// The model is expected to use fetch_url to read any result it wants to inspect.
type SearchWebTool struct {
	baseURL string
	client  *http.Client
}

// NewSearchWebTool returns a SearchWebTool pointed at baseURL.
// If baseURL is empty it falls back to defaultSearXNGURL.
func NewSearchWebTool(baseURL string) *SearchWebTool {
	if baseURL == "" {
		baseURL = defaultSearXNGURL
	}
	return &SearchWebTool{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (t *SearchWebTool) Name() string { return "search_web" }
func (t *SearchWebTool) Description() string {
	return `Search the web via SearXNG. Returns titles, URLs, and snippets. Args: {"query": "qwen3 quantization benchmarks"}`
}

type searxResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

type searxResponse struct {
	Results []searxResult `json:"results"`
}

func (t *SearchWebTool) Execute(args map[string]any) (string, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query arg required")
	}

	endpoint := t.baseURL + "?q=" + url.QueryEscape(query) + "&format=json&categories=general"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("search_web: build request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search_web: SearXNG unreachable (%s): %w", t.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("search_web: SearXNG returned %d", resp.StatusCode)
	}

	var sr searxResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return "", fmt.Errorf("search_web: decode response: %w", err)
	}

	if len(sr.Results) == 0 {
		return fmt.Sprintf("No results found for: %s", query), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Results for: %q\n\n", query))
	for i, r := range sr.Results {
		if i >= 8 {
			break
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n", i+1, r.Title, r.URL))
		if r.Content != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", r.Content))
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}
