package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/apexion-ai/apexion/internal/repomap"
)

const defaultRepoMapTokens = 4096

// RepoMapTool builds a compact repository symbol map.
type RepoMapTool struct{}

func (t *RepoMapTool) Name() string                     { return "repo_map" }
func (t *RepoMapTool) IsReadOnly() bool                 { return true }
func (t *RepoMapTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *RepoMapTool) Description() string {
	return "Build a high-level repository map (files + exported types/functions). " +
		"Use this first when exploring an unfamiliar codebase to understand architecture quickly."
}

func (t *RepoMapTool) Parameters() map[string]any {
	return map[string]any{
		"path": map[string]any{
			"type":        "string",
			"description": "Root directory of the repository (default: current directory).",
		},
		"max_tokens": map[string]any{
			"type":        "integer",
			"description": "Approximate token budget for rendered map output (default: 4096).",
		},
		"exclude": map[string]any{
			"type":        "array",
			"description": "Optional glob patterns to exclude files/directories while scanning.",
			"items":       map[string]any{"type": "string"},
		},
	}
}

func (t *RepoMapTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Path      string   `json:"path"`
		MaxTokens int      `json:"max_tokens"`
		Exclude   []string `json:"exclude"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	if p.Path == "" {
		p.Path = "."
	}
	if p.MaxTokens <= 0 {
		p.MaxTokens = defaultRepoMapTokens
	}
	if p.MaxTokens > 16000 {
		p.MaxTokens = 16000
	}

	root, err := filepath.Abs(p.Path)
	if err != nil {
		return ToolResult{}, fmt.Errorf("resolve path: %w", err)
	}

	rm := repomap.New(root, p.MaxTokens, p.Exclude)
	if err := rm.Build(); err != nil {
		return ToolResult{Content: fmt.Sprintf("Failed to build repo map: %v", err), IsError: true}, nil
	}

	rendered := strings.TrimSpace(rm.Render(p.MaxTokens))
	if rendered == "" {
		return ToolResult{
			Content: fmt.Sprintf("Repository map built for %s, but no supported symbols were found.", root),
		}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Repository map: %s\n", root)
	fmt.Fprintf(&sb, "Indexed files: %d\n", rm.FileCount())
	fmt.Fprintf(&sb, "Indexed symbols: %d\n", rm.SymbolCount())
	fmt.Fprintf(&sb, "Max tokens: %d\n\n", p.MaxTokens)
	sb.WriteString(rendered)

	return ToolResult{Content: sb.String()}, nil
}
