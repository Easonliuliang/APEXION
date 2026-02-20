package tools

import "sort"

// Registry 管理所有已注册的工具
type Registry struct {
	tools map[string]Tool
}

// NewRegistry 创建空的工具注册表
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register 注册一个工具到注册表
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Get 根据名称获取工具
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All 返回所有已注册的工具（按名称排序）
func (r *Registry) All() []Tool {
	result := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name() < result[j].Name()
	})
	return result
}

// ToSchemas 将所有工具转换为 schema 列表
// 格式：[{"name": "...", "description": "...", "input_schema": {"type":"object","properties":{...}}}]
func (r *Registry) ToSchemas() []map[string]any {
	tools := r.All()
	schemas := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		schemas = append(schemas, map[string]any{
			"name":        t.Name(),
			"description": t.Description(),
			"input_schema": map[string]any{
				"type":       "object",
				"properties": t.Parameters(),
			},
		})
	}
	return schemas
}

// WebToolsConfig holds configuration for web-related tools.
type WebToolsConfig struct {
	SearchProvider string // "tavily" or "jina"
	SearchAPIKey   string
}

// DefaultRegistry 创建包含所有内置工具的注册表
func DefaultRegistry(webCfg *WebToolsConfig) *Registry {
	r := NewRegistry()
	r.Register(&ReadFileTool{})
	r.Register(&EditFileTool{})
	r.Register(&WriteFileTool{})
	r.Register(&BashTool{})
	r.Register(&GlobTool{})
	r.Register(&GrepTool{})
	r.Register(&ListDirTool{})
	r.Register(&GitStatusTool{})
	r.Register(&GitDiffTool{})
	r.Register(&GitCommitTool{})
	r.Register(&GitPushTool{})
	r.Register(&TodoWriteTool{})
	r.Register(&TodoReadTool{})
	r.Register(&WebFetchTool{})
	if webCfg != nil {
		r.Register(NewWebSearchTool(webCfg.SearchProvider, webCfg.SearchAPIKey))
	} else {
		r.Register(NewWebSearchTool("", ""))
	}
	return r
}
