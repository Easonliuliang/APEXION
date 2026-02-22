package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	searchTimeout     = 30 * time.Second
	defaultMaxResults = 5
	maxResultsBasic   = 20
	maxResultsDeep    = 30
)

// searchResult is the common format for search results across providers.
type searchResult struct {
	Title   string
	URL     string
	Snippet string
}

// WebSearchTool searches the web using a configurable backend.
type WebSearchTool struct {
	Provider string // "tavily", "exa", or "jina"
	APIKey   string
}

// NewWebSearchTool creates a WebSearchTool with the given config.
// Provider priority: explicit > tavily (if key set) > jina (free fallback).
func NewWebSearchTool(provider, apiKey string) *WebSearchTool {
	if provider == "" {
		if apiKey != "" {
			provider = "tavily"
		} else {
			provider = "jina"
		}
	}
	return &WebSearchTool{Provider: provider, APIKey: apiKey}
}

func (t *WebSearchTool) Name() string                     { return "web_search" }
func (t *WebSearchTool) IsReadOnly() bool                 { return true }
func (t *WebSearchTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *WebSearchTool) Description() string {
	return `Search the web for current information, documentation, or solutions.
Returns a list of relevant results with titles, URLs, and snippets.
Use specific, targeted queries for best results.
Review search result snippets before deciding which URLs to fetch with web_fetch.`
}

func (t *WebSearchTool) Parameters() map[string]any {
	return map[string]any{
		"query": map[string]any{
			"type":        "string",
			"description": "The search query",
		},
		"max_results": map[string]any{
			"type":        "integer",
			"description": "Maximum number of results (default 5, max 20)",
		},
		"search_depth": map[string]any{
			"type":        "string",
			"description": "Search depth: \"basic\" (default, max 20 results) or \"deep\" (max 30 results)",
		},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Query       string `json:"query"`
		MaxResults  int    `json:"max_results"`
		SearchDepth string `json:"search_depth"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	if p.Query == "" {
		return ToolResult{}, fmt.Errorf("query is required")
	}
	if p.MaxResults <= 0 {
		p.MaxResults = defaultMaxResults
	}
	cap := maxResultsBasic
	if p.SearchDepth == "deep" {
		cap = maxResultsDeep
	}
	if p.MaxResults > cap {
		p.MaxResults = cap
	}

	switch t.Provider {
	case "tavily":
		return t.searchTavily(ctx, p.Query, p.MaxResults)
	case "exa":
		return t.searchExa(ctx, p.Query, p.MaxResults)
	default:
		return t.searchJina(ctx, p.Query, p.MaxResults)
	}
}

// searchTavily queries the Tavily search API.
func (t *WebSearchTool) searchTavily(ctx context.Context, query string, maxResults int) (ToolResult, error) {
	if t.APIKey == "" {
		return ToolResult{
			Content: "Tavily API key not configured. Set web.search_api_key in config or TAVILY_API_KEY env var, or switch to jina provider.",
			IsError: true,
		}, nil
	}

	reqBody, _ := json.Marshal(map[string]any{
		"query":        query,
		"max_results":  maxResults,
		"search_depth": "basic",
	})

	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.tavily.com/search", strings.NewReader(string(reqBody)))
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Failed to create request: %v", err), IsError: true}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ToolResult{}, fmt.Errorf("cancelled")
		}
		return ToolResult{Content: fmt.Sprintf("Search request failed: %v", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return ToolResult{
			Content: fmt.Sprintf("Tavily API error (HTTP %d): %s", resp.StatusCode, string(body)),
			IsError: true,
		}, nil
	}

	var result struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ToolResult{Content: fmt.Sprintf("Failed to parse response: %v", err), IsError: true}, nil
	}

	results := make([]searchResult, 0, len(result.Results))
	for _, r := range result.Results {
		results = append(results, searchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
		})
	}

	return ToolResult{Content: formatSearchResults(query, results)}, nil
}

// searchJina queries the Jina Search API (free, no key required).
func (t *WebSearchTool) searchJina(ctx context.Context, query string, maxResults int) (ToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	searchURL := "https://s.jina.ai/" + url.PathEscape(query)
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Failed to create request: %v", err), IsError: true}, nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", fetchUserAgent)
	if t.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.APIKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ToolResult{}, fmt.Errorf("cancelled")
		}
		return ToolResult{Content: fmt.Sprintf("Search request failed: %v", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return ToolResult{
			Content: fmt.Sprintf("Jina Search error (HTTP %d): %s", resp.StatusCode, string(body)),
			IsError: true,
		}, nil
	}

	var result struct {
		Data []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
			Content     string `json:"content"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ToolResult{Content: fmt.Sprintf("Failed to parse response: %v", err), IsError: true}, nil
	}

	results := make([]searchResult, 0, maxResults)
	for i, item := range result.Data {
		if i >= maxResults {
			break
		}
		snippet := item.Description
		if snippet == "" {
			snippet = item.Content
			if len(snippet) > 300 {
				snippet = snippet[:300] + "..."
			}
		}
		results = append(results, searchResult{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: snippet,
		})
	}

	return ToolResult{Content: formatSearchResults(query, results)}, nil
}

// searchExa queries the Exa AI search API.
// Free: $10 credits on signup (~2000 searches), no credit card required.
func (t *WebSearchTool) searchExa(ctx context.Context, query string, maxResults int) (ToolResult, error) {
	if t.APIKey == "" {
		return ToolResult{
			Content: "Exa API key not configured. Register at exa.ai for $10 free credits, then set web.search_api_key in config or EXA_API_KEY env var.",
			IsError: true,
		}, nil
	}

	reqBody, _ := json.Marshal(map[string]any{
		"query":      query,
		"numResults": maxResults,
		"type":       "auto",
		"contents": map[string]any{
			"text": map[string]any{
				"maxCharacters": 300,
			},
		},
	})

	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.exa.ai/search", strings.NewReader(string(reqBody)))
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Failed to create request: %v", err), IsError: true}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", t.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ToolResult{}, fmt.Errorf("cancelled")
		}
		return ToolResult{Content: fmt.Sprintf("Search request failed: %v", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return ToolResult{
			Content: fmt.Sprintf("Exa API error (HTTP %d): %s", resp.StatusCode, string(body)),
			IsError: true,
		}, nil
	}

	var result struct {
		Results []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
			Text  string `json:"text"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ToolResult{Content: fmt.Sprintf("Failed to parse response: %v", err), IsError: true}, nil
	}

	results := make([]searchResult, 0, len(result.Results))
	for _, r := range result.Results {
		results = append(results, searchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Text,
		})
	}

	return ToolResult{Content: formatSearchResults(query, results)}, nil
}

// formatSearchResults produces a consistent text output for LLM consumption.
func formatSearchResults(query string, results []searchResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Search results for: %s\n\n", query)

	if len(results) == 0 {
		sb.WriteString("No results found.")
		return sb.String()
	}

	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, r.Title)
		fmt.Fprintf(&sb, "   URL: %s\n", r.URL)
		if r.Snippet != "" {
			fmt.Fprintf(&sb, "   %s\n", r.Snippet)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
