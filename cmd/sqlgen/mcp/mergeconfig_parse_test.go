package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ffpSeedConfig writes content verbatim to <root>/.mcp.json.
func ffpSeedConfig(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(content), 0o600); err != nil {
		t.Fatalf("seeding .mcp.json: %v", err)
	}
}

// ffpAssertConfigUnchanged fails when <root>/.mcp.json no longer holds want
// byte-for-byte.
func ffpAssertConfigUnchanged(t *testing.T, root, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, ".mcp.json")) //nolint:gosec // path is a t.TempDir file
	if err != nil {
		t.Fatalf("re-reading .mcp.json: %v", err)
	}
	if string(got) != want {
		t.Errorf("Merge() rewrote the invalid .mcp.json:\n got: %q\nwant: %q", got, want)
	}
}

// A target holding one valid JSON value followed by more content is not a
// valid JSON document, so it must be refused with the position of the first
// trailing byte and left alone — not parsed as its first value and rewritten.
func TestMergeTrailingContentAfterJSONFailsWithoutOverwrite(t *testing.T) {
	root, opts := mergeDir(t)
	const content = `{"mcpServers":{}} garbage`
	ffpSeedConfig(t, root, content)

	_, err := Merge(opts)
	const wantPrefix = "parse .mcp.json:1:19: "
	if err == nil {
		t.Errorf("Merge() on %q = nil error, want a parse error starting %q", content, wantPrefix)
	} else if !strings.HasPrefix(err.Error(), wantPrefix) || !strings.HasSuffix(err.Error(), "(refusing to overwrite invalid JSON)") {
		t.Errorf("Merge() error = %q, want prefix %q and the refusing-to-overwrite suffix", err, wantPrefix)
	}
	ffpAssertConfigUnchanged(t, root, content)
}

// Every kind of invalid existing target names the failing line and column, not
// only the failures the decoder reports as a syntax error. A document that ends
// early — truncated, empty, or whitespace-only — is reported at the position
// just past its last byte (1:1 for a zero-byte file).
func TestMergeInvalidJSONErrorNamesLineAndColumn(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantLine int
		wantCol  int
	}{
		{name: "truncated object", content: "{\n  \"mcpServers\": {\n", wantLine: 3, wantCol: 1},
		{name: "truncated mid-line", content: "{\"mcpServers\": {", wantLine: 1, wantCol: 17},
		{name: "trailing content after the document", content: `{"mcpServers":{}} garbage`, wantLine: 1, wantCol: 19},
		{name: "trailing content on a later line", content: "{\"mcpServers\":{}}\n\n  {}\n", wantLine: 3, wantCol: 3},
		{name: "syntax error mid-document", content: "{\n  \"mcpServers\": {,}\n}\n", wantLine: 2, wantCol: 18},
		{name: "empty file", content: "", wantLine: 1, wantCol: 1},
		{name: "whitespace-only file", content: "  \n\t ", wantLine: 2, wantCol: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, opts := mergeDir(t)
			ffpSeedConfig(t, root, tt.content)

			_, err := Merge(opts)
			wantPrefix := fmt.Sprintf("parse .mcp.json:%d:%d: ", tt.wantLine, tt.wantCol)
			if err == nil {
				t.Errorf("Merge() on %q = nil error, want a parse error starting %q", tt.content, wantPrefix)
			} else if !strings.HasPrefix(err.Error(), wantPrefix) || !strings.HasSuffix(err.Error(), "(refusing to overwrite invalid JSON)") {
				t.Errorf("Merge() on %q error = %q, want prefix %q and the refusing-to-overwrite suffix", tt.content, err, wantPrefix)
			}
			ffpAssertConfigUnchanged(t, root, tt.content)
		})
	}
}
