// Package repomap extracts function/type signatures from a codebase
// and renders them as a compact map for LLM system prompt injection.
package repomap

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Symbol represents a single extracted code symbol.
type Symbol struct {
	Name      string
	Signature string // e.g. "func (s *Server) Start(ctx context.Context) error"
	Line      int
	Exported  bool
}

// FileSignatures holds all extracted symbols for a single file.
type FileSignatures struct {
	Path      string
	Language  string
	Functions []Symbol
	Types     []Symbol
}

// RepoMap scans a codebase and extracts symbol signatures.
type RepoMap struct {
	root      string
	cache     map[string]*FileSignatures
	mu        sync.RWMutex
	maxTokens int
	exclude   []string
	built     bool
}

// New creates a RepoMap for the given root directory.
func New(root string, maxTokens int, exclude []string) *RepoMap {
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	return &RepoMap{
		root:      root,
		cache:     make(map[string]*FileSignatures),
		maxTokens: maxTokens,
		exclude:   exclude,
	}
}

// Build scans the repository and extracts signatures.
func (rm *RepoMap) Build() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.cache = make(map[string]*FileSignatures)

	err := filepath.Walk(rm.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			base := info.Name()
			// Skip common irrelevant directories
			switch base {
			case ".git", "node_modules", "vendor", "__pycache__", ".next", "dist", "build", "target":
				return filepath.SkipDir
			}
			// Check user excludes
			for _, pattern := range rm.exclude {
				if matched, _ := filepath.Match(pattern, base); matched {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Skip large files
		if info.Size() > 512*1024 {
			return nil
		}

		// Check file extension
		ext := filepath.Ext(path)
		if !isSupportedExt(ext) {
			return nil
		}

		// Check exclude patterns
		relPath, _ := filepath.Rel(rm.root, path)
		for _, pattern := range rm.exclude {
			if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
				return nil
			}
			if matched, _ := filepath.Match(pattern, relPath); matched {
				return nil
			}
		}

		var sigs *FileSignatures
		var extractErr error
		if ext == ".go" {
			sigs, extractErr = extractGo(path)
		} else {
			sigs, extractErr = extractGeneric(path, ext)
		}

		if extractErr != nil || sigs == nil {
			return nil
		}

		if len(sigs.Functions) > 0 || len(sigs.Types) > 0 {
			sigs.Path = relPath
			rm.cache[relPath] = sigs
		}

		return nil
	})

	rm.built = true
	return err
}

// Render formats the repo map as text for injection into system prompt.
func (rm *RepoMap) Render(maxTokens int) string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if len(rm.cache) == 0 {
		return ""
	}

	if maxTokens <= 0 {
		maxTokens = rm.maxTokens
	}

	// Sort files by path
	paths := make([]string, 0, len(rm.cache))
	for p := range rm.cache {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var sb strings.Builder
	maxChars := maxTokens * 4 // rough: 1 token ≈ 4 chars

	for _, path := range paths {
		sigs := rm.cache[path]
		section := renderFileSignatures(sigs)
		if section == "" {
			continue
		}
		if sb.Len()+len(section) > maxChars {
			sb.WriteString(fmt.Sprintf("\n... (%d more files)\n", len(paths)-len(paths)))
			break
		}
		sb.WriteString(section)
		sb.WriteString("\n")
	}

	return sb.String()
}

// IsBuilt returns whether Build() has been called.
func (rm *RepoMap) IsBuilt() bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.built
}

// FileCount returns the number of files in the map.
func (rm *RepoMap) FileCount() int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return len(rm.cache)
}

// SymbolCount returns the total number of symbols across all files.
func (rm *RepoMap) SymbolCount() int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	count := 0
	for _, sigs := range rm.cache {
		count += len(sigs.Functions) + len(sigs.Types)
	}
	return count
}

// renderFileSignatures formats a single file's symbols.
func renderFileSignatures(sigs *FileSignatures) string {
	if len(sigs.Functions) == 0 && len(sigs.Types) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## %s\n", sigs.Path))

	for _, t := range sigs.Types {
		sb.WriteString(fmt.Sprintf("  %s\n", t.Signature))
	}

	for _, f := range sigs.Functions {
		if !f.Exported {
			continue // only show exported functions to save space
		}
		sb.WriteString(fmt.Sprintf("  %s\n", f.Signature))
	}

	return sb.String()
}

func isSupportedExt(ext string) bool {
	switch ext {
	case ".go", ".py", ".ts", ".tsx", ".js", ".jsx", ".rs", ".java", ".rb", ".c", ".h", ".cpp", ".hpp":
		return true
	}
	return false
}
