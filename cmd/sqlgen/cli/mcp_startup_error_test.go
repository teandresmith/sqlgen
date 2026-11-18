package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ffpBuildSqlgen compiles the CLI binary into a temp dir. The server writes
// JSON-RPC to the process's real stdout, so observing exactly what reaches
// stdout versus stderr needs a separate process.
func ffpBuildSqlgen(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "sqlgen")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".") //nolint:gosec // output path is a t.TempDir file
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building sqlgen binary: %v\n%s", err, out)
	}
	return bin
}

// A manifest that cannot be found or fails validation is a startup failure:
// the server never starts, so no JSON-RPC reply is written. stderr names the
// reserved code and its name ("Error: -32001 MANIFEST_NOT_FOUND: ..."), the
// process exits 1, and stdout stays empty.
func TestMCPServe_StartupFailureReportsReservedCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary-building test in short mode")
	}
	bin := ffpBuildSqlgen(t)

	invalidDir := t.TempDir()
	invalidManifest := filepath.Join(invalidDir, "manifest_gen.json")
	if err := os.WriteFile(invalidManifest, []byte(`{"schema_version":"0.1.0"}`), 0o600); err != nil {
		t.Fatalf("writing schema-invalid manifest: %v", err)
	}

	tests := []struct {
		name     string
		dir      string
		args     []string
		wantCode string
		wantName string
	}{
		{
			name:     "discovery finds no manifest",
			dir:      t.TempDir(),
			args:     []string{"mcp", "serve", "--no-watch"},
			wantCode: "-32001",
			wantName: "MANIFEST_NOT_FOUND",
		},
		{
			name:     "explicit manifest path does not exist",
			dir:      t.TempDir(),
			args:     []string{"mcp", "serve", "--no-watch", "--manifest", filepath.Join(t.TempDir(), "missing", "manifest_gen.json")},
			wantCode: "-32001",
			wantName: "MANIFEST_NOT_FOUND",
		},
		{
			name:     "manifest fails schema validation",
			dir:      invalidDir,
			args:     []string{"mcp", "serve", "--no-watch", "--manifest", invalidManifest},
			wantCode: "-32002",
			wantName: "MANIFEST_INVALID",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, bin, tt.args...) //nolint:gosec // test binary built above
			cmd.Dir = tt.dir
			// A client opens the session with initialize, so a server that does
			// answer on stdio has a request to answer.
			cmd.Stdin = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"startup-test","version":"0"}}}` + "\n")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr

			err := cmd.Run()
			exitCode := 0
			if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
				exitCode = exitErr.ExitCode()
			} else if err != nil {
				t.Fatalf("running sqlgen %v: %v", tt.args, err)
			}
			t.Logf("sqlgen %s: exit %d\nstdout: %q\nstderr: %q", strings.Join(tt.args, " "), exitCode, stdout.String(), stderr.String())

			if exitCode != ExitGeneration {
				t.Errorf("sqlgen %v exit code = %d, want %d on a startup failure", tt.args, exitCode, ExitGeneration)
			}
			wantPrefix := "Error: " + tt.wantCode + " " + tt.wantName + ": "
			if !strings.HasPrefix(stderr.String(), wantPrefix) {
				t.Errorf("sqlgen %v stderr = %q, want prefix %q", tt.args, stderr.String(), wantPrefix)
			}
			if stdout.Len() != 0 {
				t.Errorf("sqlgen %v stdout = %q, want empty (no JSON-RPC reply on a startup failure)", tt.args, stdout.String())
			}
		})
	}
}

// TestMCPServe_StartupFailureStderrFormat is the in-process, short-mode form of
// TestMCPServe_StartupFailureReportsReservedCode: the serve handler run through
// the root command prints "Error: <code> <NAME>: <message>" for a coded
// startup failure and exits with ExitGeneration. Only explicit --manifest
// paths are used, so the load fails before the server touches stdin.
func TestMCPServe_StartupFailureStderrFormat(t *testing.T) {
	invalidManifest := filepath.Join(t.TempDir(), "manifest_gen.json")
	if err := os.WriteFile(invalidManifest, []byte(`{"schema_version":"0.1.0"}`), 0o600); err != nil {
		t.Fatalf("writing schema-invalid manifest: %v", err)
	}
	tests := []struct {
		name       string
		manifest   string
		wantPrefix string
	}{
		{
			name:       "missing manifest",
			manifest:   filepath.Join(t.TempDir(), "missing", "manifest_gen.json"),
			wantPrefix: "Error: -32001 MANIFEST_NOT_FOUND: ",
		},
		{
			name:       "schema-invalid manifest",
			manifest:   invalidManifest,
			wantPrefix: "Error: -32002 MANIFEST_INVALID: ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := executeCommand("mcp", "serve", "--no-watch", "--manifest", tt.manifest)
			exitErr, ok := errors.AsType[*exitError](err)
			if !ok || exitErr.code != ExitGeneration {
				t.Errorf("error = %v, want an exitError with code %d", err, ExitGeneration)
			}
			if !strings.HasPrefix(stderr, tt.wantPrefix) || strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want one line starting %q", stderr, tt.wantPrefix)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
		})
	}
}
