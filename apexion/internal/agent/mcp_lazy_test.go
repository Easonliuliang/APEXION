package agent

import (
	"testing"

	"github.com/apexion-ai/apexion/internal/mcp"
)

func TestDesiredMCPServersForInput(t *testing.T) {
	mgr := mcp.NewManager(&mcp.MCPConfig{
		MCPServers: map[string]mcp.ServerConfig{
			"context7": {},
			"github":   {},
			"minimax":  {},
		},
	})

	tests := []struct {
		name     string
		text     string
		hasImage bool
		wantAny  []string
		wantNone []string
	}{
		{
			name:    "docs query",
			text:    "查一下 Go context 的最新官方文档和常见用法",
			wantAny: []string{"context7"},
		},
		{
			name:    "github url query",
			text:    "帮我看看这个项目 https://github.com/ErlichLiu/Proma",
			wantAny: []string{"github"},
		},
		{
			name:     "image query",
			text:     "请帮我看这个图片",
			hasImage: true,
			wantAny:  []string{"minimax"},
		},
		{
			name:     "github + docs + image",
			text:     "看下 github.com/foo/bar 的文档和用法",
			hasImage: true,
			wantAny:  []string{"github", "context7", "minimax"},
		},
		{
			name:     "plain code query",
			text:     "帮我找 runAgentLoop 在哪里定义",
			wantNone: []string{"github", "context7", "minimax"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := desiredMCPServersForInput(mgr, "minimax", tc.text, tc.hasImage)
			set := make(map[string]bool, len(got))
			for _, s := range got {
				set[s] = true
			}
			for _, w := range tc.wantAny {
				if !set[w] {
					t.Fatalf("expected %q in desired servers, got %v", w, got)
				}
			}
			for _, w := range tc.wantNone {
				if set[w] {
					t.Fatalf("expected %q to be absent, got %v", w, got)
				}
			}
		})
	}
}
