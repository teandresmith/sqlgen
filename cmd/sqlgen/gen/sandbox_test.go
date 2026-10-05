package gen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// TestSandboxPath pins the mapping `sqlgen diff` relies on: every directory a
// config can name has to land under the redirect root, and a run that
// redirects nothing has to leave its paths spelled exactly as configured.
func TestSandboxPath(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "tmp", "sqlgen-diff-1")

	tests := []struct {
		name string
		root string
		path string
		want string
	}{
		{
			name: "no root returns the configured spelling verbatim",
			root: "",
			path: "./internal/models",
			want: "./internal/models",
		},
		{
			name: "no root leaves an absolute path alone",
			root: "",
			path: filepath.Join(string(filepath.Separator), "srv", "graph"),
			want: filepath.Join(string(filepath.Separator), "srv", "graph"),
		},
		{
			name: "relative dir mirrors its shape under the root",
			root: root,
			path: "internal/models",
			want: filepath.Join(root, "internal", "models"),
		},
		{
			name: "dot-slash prefix is normalized away",
			root: root,
			path: "./models/graph",
			want: filepath.Join(root, "models", "graph"),
		},
		{
			name: "output dir of . maps to the root itself",
			root: root,
			path: ".",
			want: root,
		},
		{
			name: "absolute dir is anchored under the root, not written through",
			root: root,
			path: filepath.Join(string(filepath.Separator), "srv", "app", "graph"),
			want: filepath.Join(root, "srv", "app", "graph"),
		},
		{
			name: "parent climb cannot escape the root",
			root: root,
			path: filepath.Join("..", "shared", "models"),
			want: filepath.Join(root, "shared", "models"),
		},
		{
			name: "repeated parent climbs cannot escape the root",
			root: root,
			path: filepath.Join("..", "..", "..", "shared", "models"),
			want: filepath.Join(root, "shared", "models"),
		},
		{
			name: "bare parent climb maps to the root itself",
			root: root,
			path: "..",
			want: root,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := gen.SandboxPath(tc.root, tc.path); got != tc.want {
				t.Errorf("SandboxPath(%q, %q) = %q, want %q", tc.root, tc.path, got, tc.want)
			}
		})
	}
}

// TestSandboxPathNeverEscapes is the property the per-case table above encodes
// one example at a time: whatever a config spells, the redirected write stays
// inside the root. A miss here is the class of bug that let `sqlgen diff`
// write into the consumer's working tree.
func TestSandboxPathNeverEscapes(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "tmp", "sqlgen-diff-2")
	prefix := root + string(filepath.Separator)

	paths := []string{
		"models",
		"./models",
		"a/b/c/d",
		"..",
		"../..",
		"../../etc/passwd",
		"./../../../models",
		filepath.Join(string(filepath.Separator), "etc", "passwd"),
		string(filepath.Separator),
		".",
		"",
	}

	for _, p := range paths {
		got := gen.SandboxPath(root, p)
		if got != root && !strings.HasPrefix(got, prefix) {
			t.Errorf("SandboxPath(%q, %q) = %q, which escapes the root", root, p, got)
		}
		if strings.Contains(got, ".."+string(filepath.Separator)) {
			t.Errorf("SandboxPath(%q, %q) = %q, which still contains a parent climb", root, p, got)
		}
	}
}
