package tools

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// SearchWebTool performs a DuckDuckGo web search and returns a list of result URLs.
// The model is expected to use fetch_url to read any result it wants to inspect.
type SearchWebTool struct {
	client *http.Client
}

func NewSearchWebTool() *SearchWebTool {
	return &SearchWebTool{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (t *SearchWebTool) Name() string { return "search_web" }
func (t *SearchWebTool) Description() string {
	return `Search the web for a query. Returns titled URLs — use fetch_url to read any result. Args: {"query": "TMC2209 datasheet site:trinamic.com"}`
}

// reResultHref extracts DuckDuckGo result redirect links (/l/?uddg=...)
var reResultHref = regexp.MustCompile(`href="(/l/\?[^"]+uddg=[^"]+)"`)

// reTitleInner extracts visible anchor text from result links
var reTitleInner = regexp.MustCompile(`class="result__a"[^>]*>([^<]{3,120})<`)

func (t *SearchWebTool) Execute(args map[string]any) (string, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query arg required")
	}

	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search_web: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", fmt.Errorf("search_web read: %w", err)
	}
	html := string(body)

	hrefs := reResultHref.FindAllStringSubmatch(html, 20)
	titles := reTitleInner.FindAllStringSubmatch(html, 20)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Results for: %q\n\n", query))

	count := 0
	for i, h := range hrefs {
		if count >= 8 {
			break
		}
		// Parse uddg= from the redirect path
		raw := strings.TrimPrefix(h[1], "/l/?")
		// The href may be HTML-escaped (&amp;)
		raw = strings.ReplaceAll(raw, "&amp;", "&")
		params, err := url.ParseQuery(raw)
		if err != nil {
			continue
		}
		actualURL := params.Get("uddg")
		if actualURL == "" {
			continue
		}
		title := "(no title)"
		if i < len(titles) {
			title = strings.TrimSpace(titles[i][1])
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n\n", count+1, title, actualURL))
		count++
	}

	if count == 0 {
		return fmt.Sprintf("No results found for: %s", query), nil
	}
	return sb.String(), nil
}
