package router

import "strings"

// ClassifyIntent infers high-level intent from user text and attachments.
func ClassifyIntent(userText string, hasImage bool) Intent {
	if hasImage {
		return IntentVision
	}

	s := strings.ToLower(strings.TrimSpace(userText))
	if s == "" {
		return IntentCodebase
	}
	tokens := tokenize(s)

	if containsTokenAny(tokens, "git", "commit", "branch", "rebase", "merge", "push", "diff") ||
		containsAny(s,
			"cherry-pick", "pull request",
			"提交", "分支", "合并", "推送", "拉取请求", "代码评审", "提交记录", "提交历史",
		) {
		return IntentGit
	}

	if containsAny(s,
		"error", "failed", "failing", "failure", "panic", "bug", "stack trace", "exception", "compile", "build fails", "test fails",
		"报错", "异常", "崩溃", "堆栈", "定位根因", "编译失败", "测试失败", "无法复现",
	) {
		return IntentDebug
	}

	// Codebase understanding cues should stay local-first even if text mentions
	// "repository/仓库". This avoids routing local architecture questions to research.
	if containsAny(s,
		"repository architecture", "repo architecture", "key modules", "main entrypoint", "startup flow",
		"架构", "入口", "关键模块", "调用链", "定义在哪里", "在哪定义", "仓库结构", "项目结构",
	) ||
		containsTokenAny(tokens, "architecture", "entrypoint", "startup", "module", "modules", "callchain", "symbol", "defined", "implementation") {
		return IntentCodebase
	}

	if containsAny(s, "github.com/") ||
		containsTokenAny(tokens, "docs", "documentation", "github", "latest", "recent", "official", "repo", "repository", "star", "stars", "compare") ||
		containsAny(s,
			"latest", "recent", "news", "documentation", "docs", "github", "search web", "online", "official",
			"最新", "最近", "官方文档", "文档", "查文档", "联网", "搜索", "官网", "教程", "示例",
			"仓库", "项目地址", "star", "stars", "点赞", "热度", "对比", "优势", "缺点", "门槛",
		) {
		return IntentResearch
	}

	if containsTokenAny(tokens, "ls", "df", "du", "ps", "top", "pwd", "free") ||
		containsAny(s,
			"disk", "hard drive", "cpu", "memory", "process", "terminal", "shell", "system", "environment",
			"磁盘", "硬盘", "内存", "进程", "终端", "命令行", "系统占用", "系统资源", "当前目录",
		) {
		return IntentSystem
	}

	return IntentCodebase
}

func containsAny(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

func tokenize(s string) map[string]bool {
	out := make(map[string]bool)
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out[p] = true
	}
	return out
}

func containsTokenAny(tokens map[string]bool, keywords ...string) bool {
	for _, kw := range keywords {
		if tokens[kw] {
			return true
		}
	}
	return false
}
