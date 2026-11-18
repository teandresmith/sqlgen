package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// mcpProjectConfig is a manifest-enabled, GraphQL-free project config. extra is
// appended verbatim.
const mcpProjectConfig = `version: v1
input:
  dialect: postgres
  paths:
    - ./migrations
output:
  driver: pgx
  dir: ./internal/database
  package: database
generation:
  manifest:
    enabled: true
`

// writeMCPProject writes migrations and sqlgen.yml (mcpProjectConfig + extra)
// into dir, with no go.mod, and returns the config path.
func writeMCPProject(t *testing.T, dir, extra string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "migrations"), 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	writeFileOrFail(t, filepath.Join(dir, "migrations", "001_init.sql"), diffTestSchema)
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	writeFileOrFail(t, cfgPath, mcpProjectConfig+extra)
	return cfgPath
}

// pathExists reports whether path exists.
func pathExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %s: %v", path, err)
	}
	return err == nil
}

// `sqlgen generate` writes .mcp.json into the directory it runs in, with the
// sqlgen entry's manifest path resolved against that same directory. It does
// no go.mod discovery: run in a subdirectory of a module, the file lands in the
// subdirectory, not beside go.mod.
func TestGenerate_EmitsMCPJSONInRunDirectory(t *testing.T) {
	tests := []struct {
		name    string
		goModAt string // relative to the temp root; "" writes none
		runIn   string // relative to the temp root
	}{
		{name: "run at the go.mod directory", goModAt: ".", runIn: "."},
		{name: "run in a subdirectory of a module", goModAt: ".", runIn: "svc"},
		{name: "no go.mod at all", goModAt: "", runIn: "proj"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.goModAt != "" {
				writeFileOrFail(t, filepath.Join(root, tt.goModAt, "go.mod"), "module example.com/proj\n\ngo 1.25\n")
			}
			runDir := filepath.Join(root, tt.runIn)
			cfgPath := writeMCPProject(t, runDir, "")
			t.Chdir(runDir)

			if _, stderr, err := executeCommand("generate", "--no-graphql", "--config", cfgPath); err != nil {
				t.Fatalf("generate failed: %v\nstderr: %s", err, stderr)
			}

			raw, err := os.ReadFile(filepath.Join(runDir, ".mcp.json")) //nolint:gosec // t.TempDir path
			if err != nil {
				t.Fatalf("generate did not write .mcp.json in the directory it ran in: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("decoding .mcp.json: %v", err)
			}
			const manifestRel = "./internal/database/manifest/manifest_gen.json"
			want := map[string]any{"mcpServers": map[string]any{"sqlgen": map[string]any{
				"command":                    "sqlgen",
				"args":                       []any{"mcp", "serve", "--manifest", manifestRel},
				"x-sqlgen-managed":           true,
				"x-sqlgen-manifest":          manifestRel,
				"x-sqlgen-generator-version": Version,
			}}}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf(".mcp.json mismatch (-want +got):\n%s", diff)
			}
			if info, err := os.Stat(filepath.Join(runDir, manifestRel)); err != nil || info.IsDir() {
				t.Errorf("x-sqlgen-manifest %q does not resolve to the generated manifest from %s: %v", manifestRel, runDir, err)
			}
			if runDir != root && pathExists(t, filepath.Join(root, ".mcp.json")) {
				t.Errorf("generate wrote .mcp.json at the go.mod directory %s; want it only in the run directory", root)
			}
		})
	}
}

// Emission is the last manifest-stage step, after breadcrumbs: a breadcrumb
// failure stops the run before .mcp.json is written, and an emission failure
// still leaves the manifest and both breadcrumbs written.
func TestGenerate_MCPEmissionRunsAfterBreadcrumbs(t *testing.T) {
	t.Run("breadcrumb failure stops before emission", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := writeMCPProject(t, dir, "")
		t.Chdir(dir)
		// A directory where the CLAUDE.md breadcrumb goes makes its write fail.
		if err := os.MkdirAll(filepath.Join(dir, "internal", "database", "CLAUDE.md"), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		_, stderr, err := executeCommand("generate", "--no-graphql", "--config", cfgPath)
		if code := exitCodeOf(t, err); code != ExitGeneration {
			t.Fatalf("generate exit code = %d, want %d (breadcrumb write must fail); stderr:\n%s", code, ExitGeneration, stderr)
		}
		if !strings.Contains(stderr, "Manifest error: ") {
			t.Errorf("stderr = %q, want the manifest-stage failure", stderr)
		}
		if !pathExists(t, filepath.Join(dir, "internal", "database", "manifest", "manifest_gen.json")) {
			t.Fatalf("fixture did not reach the breadcrumb step: manifest_gen.json not written")
		}
		if pathExists(t, filepath.Join(dir, ".mcp.json")) {
			t.Errorf(".mcp.json was written although the breadcrumb step failed; emission must come after breadcrumbs")
		}
	})
	t.Run("emission failure leaves manifest and breadcrumbs written", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := writeMCPProject(t, dir, "")
		t.Chdir(dir)
		const invalid = "{not json\n"
		writeFileOrFail(t, filepath.Join(dir, ".mcp.json"), invalid)

		_, stderr, err := executeCommand("generate", "--no-graphql", "--config", cfgPath)
		if code := exitCodeOf(t, err); code != ExitGeneration {
			t.Fatalf("generate exit code = %d, want %d; stderr:\n%s", code, ExitGeneration, stderr)
		}
		if !strings.Contains(stderr, "MCP config error: ") {
			t.Errorf("stderr = %q, want the MCP emission failure", stderr)
		}
		for _, rel := range []string{
			"internal/database/manifest/manifest_gen.json",
			"internal/database/CLAUDE.md",
			"internal/database/AGENTS.md",
		} {
			if !pathExists(t, filepath.Join(dir, rel)) {
				t.Errorf("%s not written; emission must run after the rest of the manifest stage", rel)
			}
		}
		raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json")) //nolint:gosec // t.TempDir path
		if err != nil || string(raw) != invalid {
			t.Errorf(".mcp.json = %q (err %v), want it left as %q", raw, err, invalid)
		}
	})
}

// The mcp block exists only under generation.manifest: a per-table `mcp`
// setting, at the table level or under the table's manifest override, is
// refused when the config is loaded. The control keeps the same table entry
// with only a supported per-table manifest setting.
func TestGenerate_MCPBlockIsGlobalOnly(t *testing.T) {
	tests := []struct {
		name     string
		extra    string
		wantCode int
	}{
		{name: "table-level mcp", extra: "tables:\n  users:\n    mcp:\n      emit_project_config: false\n", wantCode: ExitConfig},
		{name: "mcp under the table manifest override", extra: "tables:\n  users:\n    manifest:\n      mcp:\n        server_key: users\n", wantCode: ExitConfig},
		{name: "control without mcp", extra: "tables:\n  users:\n    manifest:\n      enabled: true\n", wantCode: ExitSuccess},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := writeMCPProject(t, dir, tt.extra)
			t.Chdir(dir)

			_, stderr, err := executeCommand("generate", "--no-graphql", "--config", cfgPath)
			if code := exitCodeOf(t, err); code != tt.wantCode {
				t.Fatalf("generate exit code = %d, want %d; stderr:\n%s", code, tt.wantCode, stderr)
			}
			if tt.wantCode != ExitSuccess && !strings.Contains(stderr, "field mcp not found") {
				t.Errorf("stderr = %q, want the per-table mcp key refused as unknown", stderr)
			}
		})
	}
}

// The relative-path rule for project_configs holds with the manifest
// disabled: an absolute target is refused, and nothing is written there.
func TestGenerate_AbsoluteMCPTargetRefusedWithManifestDisabled(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(t.TempDir(), "abs-mcp.json")
	cfg := strings.Replace(mcpProjectConfig, "enabled: true", "enabled: false", 1) +
		"    mcp:\n      project_configs: [" + strconv.Quote(abs) + "]\n"
	if err := os.MkdirAll(filepath.Join(dir, "migrations"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFileOrFail(t, filepath.Join(dir, "migrations", "001_init.sql"), diffTestSchema)
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	writeFileOrFail(t, cfgPath, cfg)
	t.Chdir(dir)

	_, stderr, err := executeCommand("generate", "--no-graphql", "--config", cfgPath)
	if code := exitCodeOf(t, err); code != ExitConfig {
		t.Fatalf("generate exit code = %d, want %d; stderr:\n%s", code, ExitConfig, stderr)
	}
	want := fmt.Sprintf("manifest.mcp.project_configs: %q must be relative to the module root, not absolute", abs)
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr, want)
	}
	if pathExists(t, abs) {
		t.Errorf("absolute target %s was written", abs)
	}
}

// The serve command's own defaults: --http is off (empty) and --log-level is
// info.
func TestMCPServe_FlagDefaults(t *testing.T) {
	serve, _, err := NewRootCmd().Find([]string{"mcp", "serve"})
	if err != nil {
		t.Fatalf("finding mcp serve: %v", err)
	}
	tests := []struct {
		name string
		flag string
		want string
	}{
		{name: "http off", flag: "http", want: ""},
		{name: "log level info", flag: "log-level", want: "info"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := serve.Flags().Lookup(tt.flag)
			if f == nil {
				t.Fatalf("--%s not defined", tt.flag)
			}
			if f.DefValue != tt.want {
				t.Errorf("--%s default = %q, want %q", tt.flag, f.DefValue, tt.want)
			}
		})
	}
}

// buildSqlgenBinary compiles the CLI binary into a temp dir.
func buildSqlgenBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping binary-building test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses SIGINT and lsof")
	}
	bin := filepath.Join(t.TempDir(), "sqlgen")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".") //nolint:gosec // output path is a t.TempDir file
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building sqlgen binary: %v\n%s", err, out)
	}
	return bin
}

// copyMCPFixtureManifest copies the mcp package fixture manifest into a temp dir.
func copyMCPFixtureManifest(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "mcp", "testdata", "manifest_gen.json"))
	if err != nil {
		t.Fatalf("reading fixture manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "manifest_gen.json")
	writeFileOrFail(t, path, string(raw))
	return path
}

// syncBuffer is a bytes.Buffer safe for one writer goroutine (exec's stderr
// copy) and concurrent readers (the test's failure messages).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// serveProcess is a running `sqlgen mcp serve` subprocess driven over raw stdio.
type serveProcess struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	lines   chan string // stdout lines; closed once the process has exited and its stdout is drained
	stdout  []string    // every stdout line read so far
	stderr  syncBuffer
	waitErr error // cmd.Wait's result; read only after lines is closed
	done    bool  // lines has been drained to its close
}

// startServeProcess starts the binary with args and a piped stdin/stdout. Stdout is
// copied through an io.Pipe so cmd.Wait returns only after every line has been
// handed to the reader; the reader closes lines after Wait, so a closed lines
// channel means the process has exited and waitErr is set. Cleanup kills a
// process the test did not stop.
func startServeProcess(t *testing.T, bin string, args ...string) *serveProcess {
	t.Helper()
	s := &serveProcess{lines: make(chan string, 64)}
	s.cmd = exec.Command(bin, args...) //nolint:gosec,noctx // test binary built above; stopped with SIGINT, killed at cleanup
	s.cmd.Stderr = &s.stderr
	pr, pw := io.Pipe()
	s.cmd.Stdout = pw
	var err error
	if s.stdin, err = s.cmd.StdinPipe(); err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	if err := s.cmd.Start(); err != nil {
		t.Fatalf("starting sqlgen: %v", err)
	}
	exited := make(chan struct{})
	go func() {
		s.waitErr = s.cmd.Wait()
		_ = pw.Close()
		close(exited)
	}()
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
		// Keep draining after a scan error so exec's copy (and so Wait) never
		// blocks on a full pipe.
		_, _ = io.Copy(io.Discard, pr)
		<-exited
		close(s.lines)
	}()
	t.Cleanup(func() {
		if s.done {
			return
		}
		_ = s.cmd.Process.Kill()
		if !s.drain(10 * time.Second) {
			t.Errorf("sqlgen mcp serve did not exit within 10s of SIGKILL")
		}
	})
	return s
}

// drain collects stdout lines until the process has exited and its output is
// read (lines closed), or until timeout. It reports whether lines closed.
func (s *serveProcess) drain(timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				s.done = true
				return true
			}
			s.stdout = append(s.stdout, line)
		case <-deadline:
			return false
		}
	}
}

// send writes one JSON-RPC message line to stdin.
func (s *serveProcess) send(t *testing.T, msg string) {
	t.Helper()
	if _, err := io.WriteString(s.stdin, msg+"\n"); err != nil {
		t.Fatalf("writing to server stdin: %v", err)
	}
}

// await reads stdout lines until match returns true for a decoded message.
func (s *serveProcess) await(t *testing.T, desc string, match func(map[string]any) bool) {
	t.Helper()
	timeout := time.After(15 * time.Second)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				s.done = true
				t.Fatalf("server exited (%v) while waiting for %s; stderr:\n%s", s.waitErr, desc, s.stderr.String())
			}
			s.stdout = append(s.stdout, line)
			var msg map[string]any
			if err := json.Unmarshal([]byte(line), &msg); err == nil && match(msg) {
				return
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %s; stderr:\n%s", desc, s.stderr.String())
		}
	}
}

// initialize performs the MCP handshake over stdio.
func (s *serveProcess) initialize(t *testing.T) {
	t.Helper()
	s.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"fc","version":"0"}}}`)
	s.await(t, "initialize reply", func(m map[string]any) bool { return m["id"] == float64(1) })
	s.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
}

// stop sends SIGINT and waits up to 10s for a clean exit, collecting the
// remaining stdout lines. A server still running then is killed and the test
// fails; stop never blocks past the two bounded waits.
func (s *serveProcess) stop(t *testing.T) {
	t.Helper()
	if err := s.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Errorf("signalling server: %v", err)
	}
	if !s.drain(10 * time.Second) {
		_ = s.cmd.Process.Kill()
		if !s.drain(10 * time.Second) {
			t.Fatalf("server did not exit within 10s of SIGKILL")
		}
		t.Fatalf("server did not exit within 10s of SIGINT and was killed; stderr:\n%s", s.stderr.String())
	}
	if s.waitErr != nil {
		t.Errorf("server exit after SIGINT = %v, want clean exit; stderr:\n%s", s.waitErr, s.stderr.String())
	}
}

// renameManifestEntity rewrites the manifest (rename-replace) with an entity renamed.
func renameManifestEntity(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // t.TempDir path
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}
	mutated := strings.Replace(string(raw), `"name": "ActiveUser"`, `"name": "ArchivedUser"`, 1)
	if mutated == string(raw) {
		t.Fatalf("manifest mutation did not apply")
	}
	writeFileOrFail(t, path+".regen", mutated)
	if err := os.Rename(path+".regen", path); err != nil {
		t.Fatalf("renaming regen file: %v", err)
	}
}

// freeLoopbackPort reserves and releases an ephemeral loopback port.
func freeLoopbackPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving port: %v", err)
	}
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	_ = l.Close()
	return port
}

// waitForListen waits until 127.0.0.1:port accepts connections.
func waitForListen(t *testing.T, port string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := (&net.Dialer{Timeout: 100 * time.Millisecond}).DialContext(t.Context(), "tcp", "127.0.0.1:"+port)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server never listened on port %s", port)
}

// serveInfoLogLines are the server's info-level log events this session triggers:
// the HTTP listener starting and a watch reload.
var serveInfoLogLines = []string{
	`level=INFO msg="mcp http transport listening"`,
	`level=INFO msg="manifest reloaded"`,
}

// A session that logs (HTTP listener start and a watch reload, both at info)
// sends every log line to stderr by default — at the default info level — or
// to the --log file when given, and stdout carries only JSON-RPC messages.
//
// The info lines showing up proves the default is no higher than info. That
// it is not debug cannot be shown from the output: neither the server nor the
// MCP SDK it wraps logs anything at debug level, so a --log-level debug run
// writes the same lines. TestMCPServe_FlagDefaults pins the default value.
func TestMCPServe_LogDestinations(t *testing.T) {
	bin := buildSqlgenBinary(t)
	tests := []struct {
		name    string
		withLog bool
	}{
		{name: "default goes to stderr", withLog: false},
		{name: "log flag redirects to the file", withLog: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := copyMCPFixtureManifest(t)
			port := freeLoopbackPort(t)
			args := []string{"mcp", "serve", "--manifest", manifest, "--http", port}
			logPath := filepath.Join(t.TempDir(), "server.log")
			if tt.withLog {
				args = append(args, "--log", logPath)
			}
			s := startServeProcess(t, bin, args...)
			s.initialize(t)
			waitForListen(t, port)
			renameManifestEntity(t, manifest)
			s.await(t, "notifications/tools/list_changed", func(m map[string]any) bool {
				return m["method"] == "notifications/tools/list_changed"
			})
			s.stop(t)

			for _, line := range s.stdout {
				var msg map[string]any
				if err := json.Unmarshal([]byte(line), &msg); err != nil || msg["jsonrpc"] != "2.0" {
					t.Errorf("stdout line is not a JSON-RPC message: %q", line)
				}
			}
			logText := s.stderr.String()
			if tt.withLog {
				raw, err := os.ReadFile(logPath) //nolint:gosec // t.TempDir path
				if err != nil {
					t.Fatalf("reading --log file: %v", err)
				}
				if s.stderr.String() != "" {
					t.Errorf("stderr = %q, want empty with --log", s.stderr.String())
				}
				logText = string(raw)
			}
			for _, want := range serveInfoLogLines {
				if !strings.Contains(logText, want) {
					t.Errorf("log output lacks %q:\n%s", want, logText)
				}
			}
		})
	}
}

// Without --http no TCP listener is opened; with it, the same probe sees the
// port (the control proving the probe works).
func TestMCPServe_NoHTTPListenerWithoutOptIn(t *testing.T) {
	bin := buildSqlgenBinary(t)
	lsof, err := exec.LookPath("lsof")
	if err != nil {
		t.Skip("lsof not available to list the process's listening sockets")
	}
	listening := func(t *testing.T, pid int) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, lsof, "-nP", "-a", "-p", strconv.Itoa(pid), "-iTCP", "-sTCP:LISTEN").Output() //nolint:gosec // fixed tool and arguments
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == 1 && len(out) == 0 {
			return "" // lsof exits 1 when nothing matches
		} else if err != nil {
			t.Fatalf("lsof: %v", err)
		}
		return string(out)
	}
	tests := []struct {
		name     string
		withHTTP bool
	}{
		{name: "stdio only serves no HTTP", withHTTP: false},
		{name: "control with http opted in", withHTTP: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"mcp", "serve", "--no-watch", "--manifest", copyMCPFixtureManifest(t)}
			port := freeLoopbackPort(t)
			if tt.withHTTP {
				args = append(args, "--http", port)
			}
			s := startServeProcess(t, bin, args...)
			s.initialize(t)
			if tt.withHTTP {
				waitForListen(t, port)
			}
			got := listening(t, s.cmd.Process.Pid)
			s.stop(t)
			if tt.withHTTP && !strings.Contains(got, ":"+port) {
				t.Errorf("lsof listening sockets = %q, want port %s", got, port)
			}
			if !tt.withHTTP && got != "" {
				t.Errorf("server without --http listens on TCP:\n%s", got)
			}
		})
	}
}
