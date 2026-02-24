package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const (
	docContextDefaultResults = 5
	docContextMaxResults     = 10
	docContextDefaultFetch   = 2
	docContextMaxFetch       = 3
	docContextOutputLimit    = 32 * 1024
)

// DocContextTool performs docs-focused search and optional deep fetches.
type DocContextTool struct {
	Provider string
	APIKey   string
}

func NewDocContextTool(provider, apiKey string) *DocContextTool {
	return &DocContextTool{Provider: provider, APIKey: apiKey}
}

func (t *DocContextTool) Name() string                     { return "doc_context" }
func (t *DocContextTool) IsReadOnly() bool                 { return true }
func (t *DocContextTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *DocContextTool) Description() string {
	return "Search official documentation context for APIs/libraries and optionally fetch top pages. " +
		"Use this for framework/library usage questions before broad web_search."
}

func (t *DocContextTool) Parameters() map[string]any {
	return map[string]any{
		"topic": map[string]any{
			"type":        "string",
			"description": "The API/library/topic you need documentation context for.",
		},
		"library": map[string]any{
			"type":        "string",
			"description": "Optional library/framework name to improve targeting.",
		},
		"version": map[string]any{
			"type":        "string",
			"description": "Optional version constraint (e.g. 'v1.24', 'React 19').",
		},
		"max_results": map[string]any{
			"type":        "integer",
			"description": "Search result count (default 5, max 10).",
		},
		"fetch_top": map[string]any{
			"type":        "integer",
			"description": "How many top URLs to fetch for deep context (default 2, max 3). Set 0 to skip.",
		},
	}
}

func (t *DocContextTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Topic      string `json:"topic"`
		Library    string `json:"library"`
		Version    string `json:"version"`
		MaxResults int    `json:"max_results"`
		FetchTop   int    `json:"fetch_top"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	p.Topic = strings.TrimSpace(p.Topic)
	if p.Topic == "" {
		return ToolResult{}, fmt.Errorf("topic is required")
	}
	if p.MaxResults <= 0 {
		p.MaxResults = docContextDefaultResults
	}
	if p.MaxResults > docContextMaxResults {
		p.MaxResults = docContextMaxResults
	}
	if p.FetchTop == 0 {
		p.FetchTop = docContextDefaultFetch
	}
	if p.FetchTop > docContextMaxFetch {
		p.FetchTop = docContextMaxFetch
	}
	if p.FetchTop < 0 {
		p.FetchTop = 0
	}

	query := buildDocQuery(p.Topic, p.Library, p.Version)
	search := NewWebSearchTool(t.Provider, t.APIKey)
	searchRaw, _ := json.Marshal(map[string]any{
		"query":       query,
		"max_results": p.MaxResults,
	})
	searchRes, err := search.Execute(ctx, searchRaw)
	if err != nil {
		return ToolResult{}, err
	}
	if searchRes.IsError {
		return searchRes, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Doc context query: %s\n\n", query)
	sb.WriteString("[Search results]\n")
	sb.WriteString(searchRes.Content)
	sb.WriteString("\n")

	if p.FetchTop > 0 {
		urls := extractSearchURLs(searchRes.Content, p.FetchTop)
		if len(urls) > 0 {
			sb.WriteString("\n[Deep docs]\n")
			fetcher := &WebFetchTool{}
			for i, u := range urls {
				raw, _ := json.Marshal(map[string]any{
					"url":    u,
					"prompt": "Extract API usage patterns, key options, constraints, and gotchas relevant to: " + p.Topic,
				})
				fetchRes, ferr := fetcher.Execute(ctx, raw)
				if ferr != nil {
					fmt.Fprintf(&sb, "%d) %s\nerror: %v\n\n", i+1, u, ferr)
					continue
				}
				if fetchRes.IsError {
					fmt.Fprintf(&sb, "%d) %s\nerror: %s\n\n", i+1, u, fetchRes.Content)
					continue
				}
				fmt.Fprintf(&sb, "%d) %s\n", i+1, u)
				sb.WriteString(truncateDocChunk(fetchRes.Content, 3500))
				sb.WriteString("\n\n")
			}
		}
	}

	out := sb.String()
	truncated := false
	if len(out) > docContextOutputLimit {
		out = truncateHeadTail(out, docContextOutputLimit)
		truncated = true
	}
	return ToolResult{Content: out, Truncated: truncated}, nil
}

func buildDocQuery(topic, library, version string) string {
	parts := []string{strings.TrimSpace(topic)}
	if strings.TrimSpace(library) != "" {
		parts = append(parts, strings.TrimSpace(library))
	}
	if strings.TrimSpace(version) != "" {
		parts = append(parts, strings.TrimSpace(version))
	}
	parts = append(parts, "official documentation API reference examples")
	return strings.Join(parts, " ")
}

func extractSearchURLs(s string, max int) []string {
	re := regexp.MustCompile(`(?m)^\s*URL:\s+(\S+)\s*$`)
	matches := re.FindAllStringSubmatch(s, max*2)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, max)
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		u := strings.TrimSpace(m[1])
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
		if len(out) >= max {
			break
		}
	}
	return out
}

func truncateDocChunk(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n[Truncated]"
}
