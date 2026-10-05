package gen

import (
	"os"
	"path/filepath"
	"strings"
)

// ReadModulePath reads the module declaration from the go.mod file in dir
// and returns the consumer's module path (e.g. "example.com/foo"). Returns
// an empty string when go.mod is missing, unreadable, or has no module
// directive — callers are responsible for falling back gracefully.
func ReadModulePath(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod")) //nolint:gosec // reading go.mod from the working directory to derive the consumer module path
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "module ") || trimmed == "module" {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "module"))
			rest = strings.Trim(rest, "\"")
			return rest
		}
	}
	return ""
}

// JoinModulePath builds a Go import path by joining a module path with a
// relative directory (e.g. "example.com/foo" + "./gen" → "example.com/foo/gen").
// Returns an empty string when modulePath is empty so callers can detect the
// "no go.mod" path and skip emitting templates that depend on the import.
func JoinModulePath(modulePath, dir string) string {
	if modulePath == "" {
		return ""
	}
	rel := strings.TrimPrefix(filepath.ToSlash(dir), "./")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || rel == "." {
		return modulePath
	}
	return modulePath + "/" + rel
}
