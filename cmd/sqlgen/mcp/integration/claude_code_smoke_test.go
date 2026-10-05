package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestClaudeCodeSmoke spawns a real Claude Code client in headless mode
// against the server and asserts the sqlgen tools are actually called and
// their responses surfaced (MCP.md §7.3). Gated on CLAUDE_CODE_BIN — skipped
// by default; runs only where the binary is available.
func TestClaudeCodeSmoke(t *testing.T) {
	claudeBin := os.Getenv("CLAUDE_CODE_BIN")
	if claudeBin == "" {
		t.Skip("skipping real-client smoke test: CLAUDE_CODE_BIN not set")
	}

	// A throwaway project dir whose .mcp.json registers the built server
	// against the fixture manifest — the same committed-file registration
	// shape §3.4 emits.
	projDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "manifest_gen.json")
	raw, err := os.ReadFile("../testdata/manifest_gen.json")
	if err != nil {
		t.Fatalf("reading fixture manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil { //nolint:gosec // path is a t.TempDir file
		t.Fatalf("writing manifest: %v", err)
	}
	mcpConfig := map[string]any{
		"mcpServers": map[string]any{
			"sqlgen": map[string]any{
				"command": sqlgenBin,
				"args":    []string{"mcp", "serve", "--manifest", manifestPath, "--no-watch"},
			},
		},
	}
	cfgRaw, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		t.Fatalf("marshaling .mcp.json: %v", err)
	}
	cfgPath := filepath.Join(projDir, ".mcp.json")
	if err := os.WriteFile(cfgPath, cfgRaw, 0o600); err != nil {
		t.Fatalf("writing .mcp.json: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Canned prompt: answering requires the list_entities tool (the manifest
	// path is outside the project dir, and only the sqlgen MCP tools are
	// allowed, so the entity names are unreachable any other way).
	cmd := exec.CommandContext( //nolint:gosec // gated smoke test, binary comes from the environment
		ctx, claudeBin,
		"-p", "Use the sqlgen MCP tools to list the entities this package exposes. Answer with just the entity names.",
		"--mcp-config", cfgPath,
		"--strict-mcp-config",
		"--allowedTools", "mcp__sqlgen__*",
	)
	cmd.Dir = projDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("claude headless run failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	// Both fixture entities must have surfaced to the LLM's answer — evidence
	// the tool call happened and its response reached the model.
	answer := stdout.String()
	for _, want := range []string{"User", "ActiveUser"} {
		if !strings.Contains(answer, want) {
			t.Errorf("claude answer does not mention %q — tool response did not surface:\n%s", want, answer)
		}
	}
}
