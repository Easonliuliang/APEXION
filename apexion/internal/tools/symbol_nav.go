package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	symbolNavDefaultMax = 60
	symbolNavHardMax    = 200
)

// SymbolNavTool finds symbol definitions and references across source files.
type SymbolNavTool struct{}

func (t *SymbolNavTool) Name() string                     { return "symbol_nav" }
func (t *SymbolNavTool) IsReadOnly() bool                 { return true }
func (t *SymbolNavTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *SymbolNavTool) Description() string {
	return "Navigate a symbol across the codebase with definition/reference awareness. " +
		"Use this before broad grep when you need where a symbol is defined and used."
}

func (t *SymbolNavTool) Parameters() map[string]any {
	return map[string]any{
		"symbol": map[string]any{
			"type":        "string",
			"description": "Symbol name to locate (function/type/variable/class/interface).",
		},
		"path": map[string]any{
			"type":        "string",
			"description": "Root directory or file path to search (default: current directory).",
		},
		"mode": map[string]any{
			"type":        "string",
			"description": "Search mode: 'definitions', 'references', or 'both' (default).",
			"enum":        []string{"definitions", "references", "both"},
		},
		"max_results": map[string]any{
			"type":        "integer",
			"description": "Maximum lines per section (default 60, max 200).",
		},
		"case_insensitive": map[string]any{
			"type":        "boolean",
			"description": "Whether to match symbol case-insensitively.",
		},
	}
}

type symbolHit struct {
	Path string
	Line int
	Text string
}

func (t *SymbolNavTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Symbol          string `json:"symbol"`
		Path            string `json:"path"`
		Mode            string `json:"mode"`
		MaxResults      int    `json:"max_results"`
		CaseInsensitive bool   `json:"case_insensitive"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	p.Symbol = strings.TrimSpace(p.Symbol)
	if p.Symbol == "" {
		return ToolResult{}, fmt.Errorf("symbol is required")
	}
	if p.Path == "" {
		p.Path = "."
	}
	if p.Mode == "" {
		p.Mode = "both"
	}
	if p.Mode != "definitions" && p.Mode != "references" && p.Mode != "both" {
		return ToolResult{}, fmt.Errorf("mode must be one of: definitions, references, both")
	}
	if p.MaxResults <= 0 {
		p.MaxResults = symbolNavDefaultMax
	}
	if p.MaxResults > symbolNavHardMax {
		p.MaxResults = symbolNavHardMax
	}

	root, err := filepath.Abs(p.Path)
	if err != nil {
		return ToolResult{}, fmt.Errorf("resolve path: %w", err)
	}

	defRE := compileDefinitionRegex(p.Symbol, p.CaseInsensitive)
	refRE := compileReferenceRegex(p.Symbol, p.CaseInsensitive)

	defs := make([]symbolHit, 0, p.MaxResults)
	refs := make([]symbolHit, 0, p.MaxResults)

	info, err := os.Stat(root)
	if err != nil {
		return ToolResult{}, fmt.Errorf("stat path: %w", err)
	}

	if !info.IsDir() {
		scanSymbolFile(root, root, p.Mode, defRE, refRE, p.MaxResults, &defs, &refs)
	} else {
		_ = filepath.Walk(root, func(path string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if fi.IsDir() {
				switch fi.Name() {
				case ".git", "node_modules", "vendor", ".next", "dist", "build", "target", "__pycache__":
					return filepath.SkipDir
				}
				if strings.HasPrefix(fi.Name(), ".") && path != root {
					return filepath.SkipDir
				}
				return nil
			}
			if fi.Size() > 1024*1024 || !isSymbolSourceFile(path) {
				return nil
			}
			scanSymbolFile(root, path, p.Mode, defRE, refRE, p.MaxResults, &defs, &refs)
			if len(defs) >= p.MaxResults && len(refs) >= p.MaxResults {
				return filepath.SkipAll
			}
			return nil
		})
	}

	if len(defs) == 0 && len(refs) == 0 {
		return ToolResult{
			Content: fmt.Sprintf("No matches found for symbol %q under %s.", p.Symbol, root),
		}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Symbol navigation for %q\n", p.Symbol)
	fmt.Fprintf(&sb, "Path: %s\n", root)
	fmt.Fprintf(&sb, "Mode: %s\n\n", p.Mode)

	if p.Mode != "references" {
		fmt.Fprintf(&sb, "Definitions (%d):\n", len(defs))
		for i, h := range defs {
			fmt.Fprintf(&sb, "%d. %s:%d: %s\n", i+1, h.Path, h.Line, h.Text)
		}
		if len(defs) == 0 {
			sb.WriteString("none\n")
		}
		sb.WriteString("\n")
	}

	if p.Mode != "definitions" {
		fmt.Fprintf(&sb, "References (%d):\n", len(refs))
		for i, h := range refs {
			fmt.Fprintf(&sb, "%d. %s:%d: %s\n", i+1, h.Path, h.Line, h.Text)
		}
		if len(refs) == 0 {
			sb.WriteString("none\n")
		}
	}

	return ToolResult{Content: sb.String()}, nil
}

func compileDefinitionRegex(symbol string, caseInsensitive bool) *regexp.Regexp {
	s := regexp.QuoteMeta(symbol)
	pattern := strings.Join([]string{
		`^\s*(export\s+)?(async\s+)?function\s+` + s + `\b`,
		`^\s*(export\s+)?class\s+` + s + `\b`,
		`^\s*(export\s+)?interface\s+` + s + `\b`,
		`^\s*(export\s+)?type\s+` + s + `\b`,
		`^\s*(export\s+)?(const|let|var)\s+` + s + `\b`,
		`^\s*func\s+(\([^)]*\)\s*)?` + s + `\b`,
		`^\s*type\s+` + s + `\b`,
		`^\s*(var|const)\s+` + s + `\b`,
		`^\s*(def|class)\s+` + s + `\b`,
		`^\s*(public|private|protected)?\s*(class|struct|enum)\s+` + s + `\b`,
	}, "|")
	if caseInsensitive {
		pattern = "(?i)" + pattern
	}
	return regexp.MustCompile(pattern)
}

func compileReferenceRegex(symbol string, caseInsensitive bool) *regexp.Regexp {
	pattern := `\b` + regexp.QuoteMeta(symbol) + `\b`
	if caseInsensitive {
		pattern = "(?i)" + pattern
	}
	return regexp.MustCompile(pattern)
}

func scanSymbolFile(root, path, mode string, defRE, refRE *regexp.Regexp, max int, defs, refs *[]symbolHit) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	rel := path
	if r, err := filepath.Rel(root, path); err == nil {
		rel = r
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		defMatch := defRE.MatchString(line)
		refMatch := refRE.MatchString(line)

		if mode != "references" && defMatch && len(*defs) < max {
			*defs = append(*defs, symbolHit{
				Path: rel,
				Line: lineNum,
				Text: strings.TrimSpace(line),
			})
		}
		if mode != "definitions" && refMatch && !defMatch && len(*refs) < max {
			*refs = append(*refs, symbolHit{
				Path: rel,
				Line: lineNum,
				Text: strings.TrimSpace(line),
			})
		}

		if len(*defs) >= max && len(*refs) >= max {
			return
		}
	}
}

func isSymbolSourceFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".py", ".ts", ".tsx", ".js", ".jsx", ".java", ".rs", ".rb", ".c", ".h", ".cpp", ".hpp":
		return true
	default:
		return false
	}
}
