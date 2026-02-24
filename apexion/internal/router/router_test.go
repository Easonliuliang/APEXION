package router

import "testing"

func TestClassifyIntent(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		hasImage bool
		want     Intent
	}{
		{name: "vision", text: "please analyze this", hasImage: true, want: IntentVision},
		{name: "git", text: "check git diff before commit", want: IntentGit},
		{name: "git_zh", text: "先看 git status 和 diff，再决定是否提交", want: IntentGit},
		{name: "debug", text: "build fails with panic", want: IntentDebug},
		{name: "debug_zh", text: "这个模块报错了，帮我定位根因", want: IntentDebug},
		{name: "research", text: "search latest docs online", want: IntentResearch},
		{name: "research_github", text: "search github for examples", want: IntentResearch},
		{name: "research_zh", text: "查一下 Go context 的最新官方文档和常见用法", want: IntentResearch},
		{name: "research_github_url", text: "帮我看看这个项目 https://github.com/ErlichLiu/Proma", want: IntentResearch},
		{name: "research_compare", text: "它和我们的项目不同之处，优势等分析", want: IntentResearch},
		{name: "research_stars", text: "它的点赞比我高", want: IntentResearch},
		{name: "system", text: "check disk usage", want: IntentSystem},
		{name: "system_zh_disk", text: "我的磁盘快满了，帮我看下系统占用", want: IntentSystem},
		{name: "system_zh_ls", text: "用 ls 看下当前目录有哪些文件", want: IntentSystem},
		{name: "codebase", text: "find handler implementation", want: IntentCodebase},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyIntent(tc.text, tc.hasImage)
			if got != tc.want {
				t.Fatalf("ClassifyIntent(%q, %v) = %q, want %q", tc.text, tc.hasImage, got, tc.want)
			}
		})
	}
}

func TestPlanFiltersWebFetchOnImageTurnForTextOnlyModel(t *testing.T) {
	plan := Plan(PlanInput{
		UserText:            "Please look at this image",
		HasImage:            true,
		ModelImageSupported: false,
		Tools: []CandidateTool{
			{Name: "web_fetch", ReadOnly: true},
			{Name: "mcp__minimax__understand_image", ReadOnly: false},
		},
	}, PlanOptions{})

	if len(plan.Filtered) != 1 || plan.Filtered[0].Name != "web_fetch" {
		t.Fatalf("expected web_fetch to be filtered, got %+v", plan.Filtered)
	}
	if len(plan.Primary) == 0 || plan.Primary[0].Name != "mcp__minimax__understand_image" {
		t.Fatalf("expected image tool to rank first, got %+v", plan.Primary)
	}
}

func TestPlanMaxCandidatesCreatesFallback(t *testing.T) {
	plan := Plan(PlanInput{
		UserText: "search latest docs for this API",
		Tools: []CandidateTool{
			{Name: "web_search", ReadOnly: true},
			{Name: "web_fetch", ReadOnly: true},
			{Name: "read_file", ReadOnly: true},
			{Name: "glob", ReadOnly: true},
		},
	}, PlanOptions{MaxCandidates: 2})

	if len(plan.Primary) != 2 {
		t.Fatalf("expected 2 primary tools, got %d", len(plan.Primary))
	}
	if len(plan.Fallback) != 2 {
		t.Fatalf("expected 2 fallback tools, got %d", len(plan.Fallback))
	}
}

func TestPlanResearchDocsPrefersContext7WhenAvailable(t *testing.T) {
	plan := Plan(PlanInput{
		UserText: "查一下 Go context 的最新官方文档和常见用法。",
		Tools: []CandidateTool{
			{Name: "doc_context", ReadOnly: true},
			{Name: "web_search", ReadOnly: true},
			{Name: "web_fetch", ReadOnly: true},
			{Name: "mcp__context7__resolve-library-id", ReadOnly: false},
		},
	}, PlanOptions{})

	if len(plan.Primary) == 0 || plan.Primary[0].Name != "mcp__context7__resolve-library-id" {
		t.Fatalf("expected context7 tool to rank first for docs query, got %+v", plan.Primary)
	}
}

func TestPlanResearchGitHubPrefersGitHubToolWhenAvailable(t *testing.T) {
	plan := Plan(PlanInput{
		UserText: "帮我看看这个项目 https://github.com/ErlichLiu/Proma",
		Tools: []CandidateTool{
			{Name: "doc_context", ReadOnly: true},
			{Name: "web_search", ReadOnly: true},
			{Name: "web_fetch", ReadOnly: true},
			{Name: "mcp__github__get_file_contents", ReadOnly: false},
		},
	}, PlanOptions{})

	if len(plan.Primary) == 0 || plan.Primary[0].Name != "mcp__github__get_file_contents" {
		t.Fatalf("expected github tool to rank first for github query, got %+v", plan.Primary)
	}
}
