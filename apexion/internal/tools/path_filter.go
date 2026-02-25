package tools

import (
	"os"
	"path/filepath"
	"strings"
)

var defaultSkipDirNames = map[string]bool{
	".git":          true,
	"node_modules":  true,
	"vendor":        true,
	"__pycache__":   true,
	".next":         true,
	"dist":          true,
	"build":         true,
	"target":        true,
	".venv":         true,
	".tox":          true,
	".mypy_cache":   true,
	".pytest_cache": true,
}

func shouldSkipDir(path, name string) bool {
	if defaultSkipDirNames[name] {
		return true
	}
	if strings.HasPrefix(name, ".") && name != "." {
		return true
	}
	return isBenchmarkArtifactPath(path)
}

func shouldSkipFilePath(path string, info os.FileInfo) bool {
	if isBenchmarkArtifactPath(path) {
		return true
	}
	if info != nil {
		// Keep existing default behavior: don't scan very large files.
		if info.Size() > 1024*1024 {
			return true
		}
	}
	return false
}

func isBenchmarkArtifactPath(path string) bool {
	p := filepath.ToSlash(path)
	if strings.Contains(p, "/benchmark/ab/results/") {
		return true
	}
	return strings.HasSuffix(p, "/benchmark/ab/results")
}
