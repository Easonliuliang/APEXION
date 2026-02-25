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
		{name: "research_url_docs", text: "访问 https://pkg.go.dev/context ，总结 Go context 常见用法", want: IntentResearch},
		{name: "research_github_url", text: "帮我看看这个项目 https://github.com/ErlichLiu/Proma", want: IntentResearch},
		{name: "research_compare", text: "它和我们的项目不同之处，优势等分析", want: IntentResearch},
		{name: "research_stars", text: "它的点赞比我高", want: IntentResearch},
		{name: "codebase_repo_architecture", text: "map this repository architecture and key modules", want: IntentCodebase},
		{name: "codebase_repo_architecture_zh", text: "请先快速理解这个仓库的架构，然后告诉我入口和关键模块", want: IntentCodebase},
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
	if len(plan.Fallback) != 0 {
		t.Fatalf("expected 0 fallback tools after hard filter, got %d", len(plan.Fallback))
	}
	if len(plan.Filtered) == 0 {
		t.Fatalf("expected filtered tools to be populated, got %+v", plan.Filtered)
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
	if plan.ReasonCode == "" {
		t.Fatal("expected reason code for research docs route")
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
	if plan.ReasonCode == "" {
		t.Fatal("expected reason code for research github route")
	}
}

func TestPlanResearchDocsPolicyBlocksPrimitiveFirstTool(t *testing.T) {
	plan := Plan(PlanInput{
		UserText: "查一下 Go context 的最新官方文档和常见用法。",
		Tools: []CandidateTool{
			{Name: "read_file", ReadOnly: true},
			{Name: "glob", ReadOnly: true},
			{Name: "doc_context", ReadOnly: true},
			{Name: "web_fetch", ReadOnly: true},
		},
	}, PlanOptions{})

	if len(plan.Primary) == 0 {
		t.Fatal("expected at least one primary tool")
	}
	if plan.Primary[0].Name == "read_file" || plan.Primary[0].Name == "glob" {
		t.Fatalf("expected non-primitive first tool, got %s", plan.Primary[0].Name)
	}
	if plan.ReasonCode == "" {
		t.Fatal("expected reason code to be populated")
	}
}

func TestPlanCodebasePolicyPromotesSemanticFirstTool(t *testing.T) {
	plan := Plan(PlanInput{
		UserText: "请先快速理解这个仓库的架构，然后告诉我入口和关键模块",
		Tools: []CandidateTool{
			{Name: "bash", ReadOnly: true},
			{Name: "glob", ReadOnly: true},
			{Name: "repo_map", ReadOnly: true},
			{Name: "symbol_nav", ReadOnly: true},
		},
	}, PlanOptions{})

	if len(plan.Primary) == 0 {
		t.Fatal("expected primary tools")
	}
	if plan.Primary[0].Name == "bash" {
		t.Fatalf("expected semantic first tool, got %s", plan.Primary[0].Name)
	}
	if plan.Primary[0].Name != "repo_map" && plan.Primary[0].Name != "symbol_nav" {
		t.Fatalf("expected repo_map/symbol_nav first, got %s", plan.Primary[0].Name)
	}
	for _, p := range plan.Primary {
		if p.Name == "bash" || p.Name == "glob" {
			t.Fatalf("expected hard first-step policy to filter disallowed tools, got %+v", plan.Primary)
		}
	}
	if plan.ReasonCode == "" {
		t.Fatal("expected reason code to be populated")
	}
}

func TestPlanHybridKeepsLegacyPrimaryAndEmitsShadow(t *testing.T) {
	input := PlanInput{
		UserText: "find latest official docs for go context package",
		Tools: []CandidateTool{
			{Name: "doc_context", ReadOnly: true},
			{Name: "web_search", ReadOnly: true},
			{Name: "web_fetch", ReadOnly: true},
		},
	}

	legacy := Plan(input, PlanOptions{Strategy: RoutingLegacy})
	hybrid := Plan(input, PlanOptions{Strategy: RoutingHybrid, ShadowEval: true, ShadowSampleRate: 1.0})

	if len(hybrid.Primary) != len(legacy.Primary) {
		t.Fatalf("hybrid primary count mismatch: got %d want %d", len(hybrid.Primary), len(legacy.Primary))
	}
	for i := range legacy.Primary {
		if hybrid.Primary[i].Name != legacy.Primary[i].Name {
			t.Fatalf("hybrid primary mismatch at %d: got %s want %s", i, hybrid.Primary[i].Name, legacy.Primary[i].Name)
		}
	}
	if hybrid.Shadow == nil {
		t.Fatal("expected hybrid plan shadow to be present")
	}
	if hybrid.Shadow.Strategy != RoutingCapabilityV2 {
		t.Fatalf("expected shadow strategy %s, got %s", RoutingCapabilityV2, hybrid.Shadow.Strategy)
	}
}

func TestPlanCapabilityV2FiltersViewImageWithoutImageSupport(t *testing.T) {
	plan := Plan(PlanInput{
		UserText:            "please view this local image",
		HasImage:            true,
		ModelImageSupported: false,
		Tools: []CandidateTool{
			{Name: "view_image", ReadOnly: true},
			{Name: "read_file", ReadOnly: true},
		},
	}, PlanOptions{Strategy: RoutingCapabilityV2})

	found := false
	for _, f := range plan.Filtered {
		if f.Name == "view_image" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected view_image to be filtered by capability gate, got %+v", plan.Filtered)
	}
}

func TestPlanCapabilityV2EmitsFastPathForRepoOverview(t *testing.T) {
	plan := Plan(PlanInput{
		UserText: "please map this repository architecture and key modules",
		Tools: []CandidateTool{
			{Name: "repo_map", ReadOnly: true},
			{Name: "symbol_nav", ReadOnly: true},
			{Name: "read_file", ReadOnly: true},
		},
	}, PlanOptions{
		Strategy:              RoutingCapabilityV2,
		DeterministicFastpath: true,
		FastpathConfidence:    0.85,
	})

	if plan.FastPath == nil {
		t.Fatal("expected fastpath to be emitted")
	}
	if plan.FastPath.Tool != "repo_map" || plan.FastPath.Task != "repo_overview" {
		t.Fatalf("unexpected fastpath %+v", plan.FastPath)
	}
}

func TestPlanCapabilityV2EmitsFastPathForSymbolLookup(t *testing.T) {
	plan := Plan(PlanInput{
		UserText: "帮我找一下 runAgentLoop 在哪里定义、被哪些地方调用",
		Tools: []CandidateTool{
			{Name: "symbol_nav", ReadOnly: true},
			{Name: "repo_map", ReadOnly: true},
			{Name: "grep", ReadOnly: true},
		},
	}, PlanOptions{
		Strategy:              RoutingCapabilityV2,
		DeterministicFastpath: true,
		FastpathConfidence:    0.85,
	})

	if plan.FastPath == nil {
		t.Fatal("expected fastpath to be emitted")
	}
	if plan.FastPath.Tool != "symbol_nav" || plan.FastPath.Task != "symbol_lookup" {
		t.Fatalf("unexpected fastpath %+v", plan.FastPath)
	}
	if plan.FastPath.InputJSON == "" {
		t.Fatal("expected fastpath input payload")
	}
}

func TestPlanShadowSampleRateZeroDisablesShadow(t *testing.T) {
	plan := Plan(PlanInput{
		UserText: "find latest official docs for go context package",
		Tools: []CandidateTool{
			{Name: "doc_context", ReadOnly: true},
			{Name: "web_search", ReadOnly: true},
			{Name: "web_fetch", ReadOnly: true},
		},
	}, PlanOptions{
		Strategy:         RoutingHybrid,
		ShadowEval:       true,
		ShadowSampleRate: 0,
	})

	if plan.Shadow != nil {
		t.Fatalf("expected no shadow when sample rate is 0, got %+v", plan.Shadow)
	}
}
