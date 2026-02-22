package tools

import "sort"

// Registry manages all registered tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All returns all registered tools sorted by name.
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

// ToSchemas converts all tools to a list of schema maps.
// Format: [{"name": "...", "description": "...", "input_schema": {"type":"object","properties":{...}}}]
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

// BashToolConfig holds configuration for bash tool sandboxing.
type BashToolConfig struct {
	WorkDir  string // restrict execution to this directory
	AuditLog string // path for logging commands
}

// ReadOnlyRegistry creates a registry with only read-only tools.
// Used by sub-agents that should not modify files or run commands.
func ReadOnlyRegistry() *Registry {
	r := NewRegistry()
	r.Register(&ReadFileTool{})
	r.Register(&GlobTool{})
	r.Register(&GrepTool{})
	r.Register(&ListDirTool{})
	r.Register(&WebFetchTool{})
	r.Register(&TodoReadTool{})
	r.Register(&GitLogTool{})
	return r
}

// CodeRegistry creates a registry with read + write + execute tools.
// Used by code sub-agents that need to modify files and run commands.
// Does NOT include task (no nesting) or question (sub-agents can't prompt).
func CodeRegistry() *Registry {
	r := NewRegistry()
	// Read tools
	r.Register(&ReadFileTool{})
	r.Register(&GlobTool{})
	r.Register(&GrepTool{})
	r.Register(&ListDirTool{})
	// Write tools
	r.Register(&EditFileTool{})
	r.Register(&WriteFileTool{})
	// Execute tools
	r.Register(&BashTool{})
	// Git tools
	r.Register(&GitStatusTool{})
	r.Register(&GitDiffTool{})
	r.Register(&GitLogTool{})
	r.Register(&GitBranchTool{})
	r.Register(&GitCommitTool{})
	return r
}

// DefaultRegistry creates a registry with all built-in tools.
func DefaultRegistry(webCfg *WebToolsConfig, bashCfg *BashToolConfig) *Registry {
	r := NewRegistry()
	r.Register(&ReadFileTool{})
	r.Register(&EditFileTool{})
	r.Register(&WriteFileTool{})
	bashTool := &BashTool{}
	if bashCfg != nil {
		bashTool.WorkDir = bashCfg.WorkDir
		bashTool.AuditLog = bashCfg.AuditLog
	}
	r.Register(bashTool)
	r.Register(&GlobTool{})
	r.Register(&GrepTool{})
	r.Register(&ListDirTool{})
	r.Register(&GitStatusTool{})
	r.Register(&GitDiffTool{})
	r.Register(&GitCommitTool{})
	r.Register(&GitPushTool{})
	r.Register(&GitLogTool{})
	r.Register(&GitBranchTool{})
	r.Register(&QuestionTool{})
	r.Register(&TaskTool{})
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
