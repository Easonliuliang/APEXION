package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
)

const (
	fetchTimeout     = 30 * time.Second
	fetchMaxBodySize = 5 * 1024 * 1024 // 5MB
	fetchMaxLines    = 2000            // max lines returned to LLM
	fetchUserAgent   = "aictl/1.0 (AI coding assistant)"
	fetchCacheTTL    = 15 * time.Minute
)

// ---------- URL cache ----------

type fetchCacheEntry struct {
	content   string
	fetchedAt time.Time
}

var (
	fetchCache   = map[string]fetchCacheEntry{}
	fetchCacheMu sync.Mutex
)

func fetchCacheGet(key string) (string, bool) {
	fetchCacheMu.Lock()
	defer fetchCacheMu.Unlock()
	e, ok := fetchCache[key]
	if !ok || time.Since(e.fetchedAt) > fetchCacheTTL {
		if ok {
			delete(fetchCache, key)
		}
		return "", false
	}
	return e.content, true
}

func fetchCacheSet(key, content string) {
	fetchCacheMu.Lock()
	defer fetchCacheMu.Unlock()
	// Evict expired entries when cache grows large.
	if len(fetchCache) > 100 {
		now := time.Now()
		for k, e := range fetchCache {
			if now.Sub(e.fetchedAt) > fetchCacheTTL {
				delete(fetchCache, k)
			}
		}
	}
	fetchCache[key] = fetchCacheEntry{content: content, fetchedAt: time.Now()}
}

// ---------- tool ----------

// WebFetchTool fetches a web page and converts it to markdown.
type WebFetchTool struct{}

func (t *WebFetchTool) Name() string                     { return "web_fetch" }
func (t *WebFetchTool) IsReadOnly() bool                 { return true }
func (t *WebFetchTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *WebFetchTool) Description() string {
	return `Fetch a web page and convert it to markdown for reading.
Use this to read web pages, documentation, GitHub READMEs, blog posts, and other online content.
Always provide a specific prompt describing what information you need from the page.
If the page redirects to a different domain, a new request with the redirect URL is needed.
Results are cached for 15 minutes; repeated fetches of the same URL return cached content.`
}

func (t *WebFetchTool) Parameters() map[string]any {
	return map[string]any{
		"url": map[string]any{
			"type":        "string",
			"description": "The URL to fetch (http or https)",
		},
		"prompt": map[string]any{
			"type":        "string",
			"description": "What information to extract from the page",
		},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		URL    string `json:"url"`
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	if p.URL == "" {
		return ToolResult{}, fmt.Errorf("url is required")
	}
	if p.Prompt == "" {
		return ToolResult{}, fmt.Errorf("prompt is required")
	}

	// Parse and validate URL.
	u, err := url.Parse(p.URL)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Invalid URL: %v", err), IsError: true}, nil
	}

	// Auto-upgrade http → https.
	if u.Scheme == "http" {
		u.Scheme = "https"
	}
	if u.Scheme != "https" {
		return ToolResult{Content: "Only http and https URLs are supported", IsError: true}, nil
	}

	fetchURL := u.String()

	// Check cache first.
	if cached, ok := fetchCacheGet(fetchURL); ok {
		var sb strings.Builder
		fmt.Fprintf(&sb, "URL: %s\nPrompt: %s\n(cached)\n\n", fetchURL, p.Prompt)
		sb.WriteString(cached)
		return ToolResult{Content: sb.String()}, nil
	}

	originalHost := u.Host

	// Build HTTP client that detects cross-domain redirects.
	client := &http.Client{
		Timeout: fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Host == originalHost {
				return nil
			}
			return &crossDomainRedirect{URL: req.URL.String()}
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", fetchURL, nil)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Failed to create request: %v", err), IsError: true}, nil
	}
	req.Header.Set("User-Agent", fetchUserAgent)
	req.Header.Set("Accept", "text/html,text/plain,text/markdown,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		// Check for cross-domain redirect.
		var cdr *crossDomainRedirect
		if errors.As(err, &cdr) {
			return ToolResult{
				Content: fmt.Sprintf("Redirect to different domain detected.\nThe URL redirects to: %s\nMake a new web_fetch request with this URL.", cdr.URL),
			}, nil
		}
		if ctx.Err() != nil {
			return ToolResult{}, fmt.Errorf("cancelled")
		}
		return ToolResult{Content: fmt.Sprintf("HTTP request failed: %v", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return ToolResult{
			Content: fmt.Sprintf("HTTP %d %s for %s", resp.StatusCode, http.StatusText(resp.StatusCode), fetchURL),
			IsError: true,
		}, nil
	}

	// Read body with size limit.
	limited := io.LimitReader(resp.Body, fetchMaxBodySize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Failed to read response: %v", err), IsError: true}, nil
	}

	truncated := false
	if len(body) > fetchMaxBodySize {
		body = body[:fetchMaxBodySize]
		truncated = true
	}

	// Convert based on content type.
	contentType := resp.Header.Get("Content-Type")
	var content string

	switch {
	case strings.Contains(contentType, "text/html"),
		strings.Contains(contentType, "application/xhtml"):
		md, err := htmltomarkdown.ConvertString(string(body))
		if err != nil {
			content = string(body)
		} else {
			content = md
		}
	case strings.Contains(contentType, "text/markdown"),
		strings.Contains(contentType, "text/plain"):
		content = string(body)
	default:
		if len(body) > 0 && isLikelyText(body) {
			content = string(body)
		} else {
			return ToolResult{
				Content: fmt.Sprintf("Unsupported content type: %s", contentType),
				IsError: true,
			}, nil
		}
	}

	// Truncate by line count to avoid blowing up LLM context.
	content = truncateLines(content, fetchMaxLines)

	// Store in cache.
	fetchCacheSet(fetchURL, content)

	var sb strings.Builder
	fmt.Fprintf(&sb, "URL: %s\nPrompt: %s\n\n", fetchURL, p.Prompt)
	sb.WriteString(content)
	if truncated {
		sb.WriteString("\n\n[Content truncated due to size limit]")
	}

	return ToolResult{Content: sb.String()}, nil
}

// truncateLines keeps only the first maxLines lines.
func truncateLines(s string, maxLines int) string {
	idx := 0
	for i := 0; i < maxLines; i++ {
		next := strings.IndexByte(s[idx:], '\n')
		if next == -1 {
			return s // fewer lines than limit
		}
		idx += next + 1
	}
	return s[:idx] + "\n[Content truncated to first 2000 lines]"
}

// crossDomainRedirect is a sentinel error for cross-domain redirect detection.
type crossDomainRedirect struct {
	URL string
}

func (e *crossDomainRedirect) Error() string {
	return fmt.Sprintf("cross-domain redirect to %s", e.URL)
}

// isLikelyText checks if content is likely text (not binary).
func isLikelyText(data []byte) bool {
	check := data
	if len(check) > 512 {
		check = check[:512]
	}
	for _, b := range check {
		if b == 0 {
			return false
		}
	}
	return true
}
