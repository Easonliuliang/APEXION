package repomap

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"regexp"
	"strings"
)

// extractGo uses go/parser for precise symbol extraction from Go files.
func extractGo(path string) (*FileSignatures, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	sigs := &FileSignatures{Path: path, Language: "go"}

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sym := Symbol{
				Name:     d.Name.Name,
				Line:     fset.Position(d.Pos()).Line,
				Exported: d.Name.IsExported(),
			}
			sym.Signature = renderFuncSignature(fset, d)
			sigs.Functions = append(sigs.Functions, sym)

		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					sym := Symbol{
						Name:     ts.Name.Name,
						Line:     fset.Position(ts.Pos()).Line,
						Exported: ts.Name.IsExported(),
					}
					sym.Signature = renderTypeSignature(ts)
					sigs.Types = append(sigs.Types, sym)
				}
			}
		}
	}

	return sigs, nil
}

// renderFuncSignature renders a Go function declaration as a signature string.
func renderFuncSignature(fset *token.FileSet, fd *ast.FuncDecl) string {
	var sb strings.Builder
	sb.WriteString("func ")

	// Receiver
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		sb.WriteString("(")
		var buf strings.Builder
		printer.Fprint(&buf, fset, fd.Recv.List[0].Type)
		sb.WriteString(buf.String())
		sb.WriteString(") ")
	}

	sb.WriteString(fd.Name.Name)

	// Parameters
	sb.WriteString("(")
	if fd.Type.Params != nil {
		params := renderFieldList(fset, fd.Type.Params)
		sb.WriteString(params)
	}
	sb.WriteString(")")

	// Results
	if fd.Type.Results != nil && len(fd.Type.Results.List) > 0 {
		results := renderFieldList(fset, fd.Type.Results)
		if len(fd.Type.Results.List) == 1 && len(fd.Type.Results.List[0].Names) == 0 {
			sb.WriteString(" ")
			sb.WriteString(results)
		} else {
			sb.WriteString(" (")
			sb.WriteString(results)
			sb.WriteString(")")
		}
	}

	return sb.String()
}

// renderFieldList renders a field list (params or results) as a compact string.
func renderFieldList(fset *token.FileSet, fl *ast.FieldList) string {
	var parts []string
	for _, field := range fl.List {
		var buf strings.Builder
		printer.Fprint(&buf, fset, field.Type)
		typeStr := buf.String()

		if len(field.Names) > 0 {
			names := make([]string, len(field.Names))
			for i, n := range field.Names {
				names[i] = n.Name
			}
			parts = append(parts, strings.Join(names, ", ")+" "+typeStr)
		} else {
			parts = append(parts, typeStr)
		}
	}
	return strings.Join(parts, ", ")
}

// renderTypeSignature renders a Go type declaration.
func renderTypeSignature(ts *ast.TypeSpec) string {
	switch ts.Type.(type) {
	case *ast.StructType:
		return "type " + ts.Name.Name + " struct"
	case *ast.InterfaceType:
		return "type " + ts.Name.Name + " interface"
	default:
		return "type " + ts.Name.Name
	}
}

// Language-specific regex patterns for symbol extraction.
var langPatterns = map[string][]*regexp.Regexp{
	".py": {
		regexp.MustCompile(`^(def \w+\([^)]*\))`),
		regexp.MustCompile(`^(class \w+[^:]*)`),
	},
	".ts": {
		regexp.MustCompile(`^(export\s+)?(function \w+[^{]*)`),
		regexp.MustCompile(`^(export\s+)?(class \w+[^{]*)`),
		regexp.MustCompile(`^(export\s+)?(interface \w+[^{]*)`),
		regexp.MustCompile(`^(export\s+)?(type \w+\s*=)`),
	},
	".tsx": {
		regexp.MustCompile(`^(export\s+)?(function \w+[^{]*)`),
		regexp.MustCompile(`^(export\s+)?(class \w+[^{]*)`),
		regexp.MustCompile(`^(export\s+)?(interface \w+[^{]*)`),
	},
	".js": {
		regexp.MustCompile(`^(export\s+)?(function \w+[^{]*)`),
		regexp.MustCompile(`^(export\s+)?(class \w+[^{]*)`),
	},
	".jsx": {
		regexp.MustCompile(`^(export\s+)?(function \w+[^{]*)`),
		regexp.MustCompile(`^(export\s+)?(class \w+[^{]*)`),
	},
	".rs": {
		regexp.MustCompile(`^(pub\s+)?fn \w+[^{]*`),
		regexp.MustCompile(`^(pub\s+)?struct \w+`),
		regexp.MustCompile(`^(pub\s+)?enum \w+`),
		regexp.MustCompile(`^(pub\s+)?trait \w+`),
		regexp.MustCompile(`^impl\s+\w+`),
	},
	".java": {
		regexp.MustCompile(`^\s*(public|private|protected)\s+[\w<>\[\]]+\s+\w+\s*\(`),
		regexp.MustCompile(`^\s*(public|private|protected)?\s*(class|interface|enum)\s+\w+`),
	},
	".rb": {
		regexp.MustCompile(`^\s*def \w+`),
		regexp.MustCompile(`^\s*class \w+`),
		regexp.MustCompile(`^\s*module \w+`),
	},
	".c": {
		regexp.MustCompile(`^[\w\s\*]+\s+\w+\s*\(`),
		regexp.MustCompile(`^typedef\s+struct`),
	},
	".h": {
		regexp.MustCompile(`^[\w\s\*]+\s+\w+\s*\(`),
		regexp.MustCompile(`^typedef\s+`),
		regexp.MustCompile(`^struct\s+\w+`),
	},
	".cpp": {
		regexp.MustCompile(`^[\w\s\*:&]+\s+\w+::\w+\s*\(`),
		regexp.MustCompile(`^class\s+\w+`),
	},
	".hpp": {
		regexp.MustCompile(`^[\w\s\*:&]+\s+\w+\s*\(`),
		regexp.MustCompile(`^class\s+\w+`),
	},
}

// extractGeneric uses regex patterns to extract symbols from non-Go files.
func extractGeneric(path, ext string) (*FileSignatures, error) {
	patterns, ok := langPatterns[ext]
	if !ok {
		return nil, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sigs := &FileSignatures{Path: path, Language: ext[1:]}

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			continue
		}

		for _, re := range patterns {
			if match := re.FindString(trimmed); match != "" {
				match = strings.TrimSpace(match)
				isType := strings.Contains(match, "class ") ||
					strings.Contains(match, "struct ") ||
					strings.Contains(match, "interface ") ||
					strings.Contains(match, "enum ") ||
					strings.Contains(match, "trait ") ||
					strings.Contains(match, "module ") ||
					strings.Contains(match, "typedef ") ||
					strings.Contains(match, "type ")

				sym := Symbol{
					Name:      extractSymbolName(match),
					Signature: match,
					Line:      lineNum,
					Exported:  isExported(match, ext),
				}

				if isType {
					sigs.Types = append(sigs.Types, sym)
				} else {
					sigs.Functions = append(sigs.Functions, sym)
				}
				break // one match per line
			}
		}
	}

	return sigs, scanner.Err()
}

// extractSymbolName extracts the primary identifier from a signature match.
func extractSymbolName(sig string) string {
	nameRe := regexp.MustCompile(`(?:def|func|function|class|struct|enum|trait|interface|module|type|impl)\s+(\w+)`)
	if m := nameRe.FindStringSubmatch(sig); len(m) > 1 {
		return m[1]
	}
	// Fallback: find first word-like token
	words := strings.Fields(sig)
	for _, w := range words {
		if len(w) > 0 && w[0] != '(' && w[0] != '{' {
			return strings.TrimRight(w, "({:=<")
		}
	}
	return sig
}

// isExported determines if a symbol is exported/public based on language conventions.
func isExported(sig, ext string) bool {
	switch ext {
	case ".py":
		return !strings.Contains(sig, "def _") // Python: underscore prefix = private
	case ".ts", ".tsx", ".js", ".jsx":
		return strings.Contains(sig, "export ")
	case ".rs":
		return strings.Contains(sig, "pub ")
	case ".java":
		return strings.Contains(sig, "public ")
	case ".rb":
		return true // Ruby: all methods are effectively accessible
	default:
		return true
	}
}
