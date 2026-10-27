package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/mod/modfile"
)

// goSDKModule is the official Go SDK's module path, as §31.4 names it.
const goSDKModule = "github.com/modelcontextprotocol/go-sdk"

// TestNewUsesOfficialSDKServer checks the server side is the official Go SDK's
// server type.
func TestNewUsesOfficialSDKServer(t *testing.T) {
	srv := New("fs-version", nil)
	got := reflect.TypeOf(srv.mcp)
	if got.Kind() != reflect.Pointer || got.Elem().PkgPath() != goSDKModule+"/mcp" || got.Elem().Name() != "Server" {
		t.Errorf("server implementation = %v, want *%s/mcp.Server", got, goSDKModule)
	}
}

// TestGoSDKRequiredOnlyByCLIModule reads the three go.mod files: only the CLI
// module requires the Go SDK.
func TestGoSDKRequiredOnlyByCLIModule(t *testing.T) {
	tests := []struct {
		name        string
		gomod       string
		wantRequire bool
	}{
		{name: "cli module requires go-sdk", gomod: "../go.mod", wantRequire: true},
		{name: "runtime module does not require go-sdk", gomod: "../../../go.mod", wantRequire: false},
		{name: "parser module does not require go-sdk", gomod: "../../../parser/go.mod", wantRequire: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.gomod)
			if err != nil {
				t.Fatalf("read %s: %v", tt.gomod, err)
			}
			f, err := modfile.Parse(tt.gomod, data, nil)
			if err != nil {
				t.Fatalf("parse %s: %v", tt.gomod, err)
			}
			required := false
			for _, r := range f.Require {
				if r.Mod.Path == goSDKModule {
					required = true
				}
			}
			if required != tt.wantRequire {
				t.Errorf("%s (module %s) requires %s = %v, want %v", tt.gomod, f.Module.Mod.Path, goSDKModule, required, tt.wantRequire)
			}
		})
	}
}

// rawInitialize writes one raw initialize request to a fresh server over a
// newline-delimited stream and returns the decoded response.
func rawInitialize(t *testing.T, protocolVersion string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()
	srv := New("fs-version", nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ctx, &mcp.IOTransport{Reader: serverR, Writer: serverW})
	}()
	t.Cleanup(func() {
		cancel()
		_ = clientW.Close()
		_ = clientR.Close()
		<-done
	})

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + protocolVersion +
		`","capabilities":{},"clientInfo":{"name":"fs-raw","version":"0"}}}` + "\n"
	go func() { _, _ = io.WriteString(clientW, req) }()

	line, err := bufio.NewReader(clientR).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read initialize response: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("initialize response is not JSON: %v\n%s", err, line)
	}
	return resp
}

// TestHandshakeNegotiatesProtocolVersion sends initialize with several requested
// protocol versions and checks the version the server answers with.
func TestHandshakeNegotiatesProtocolVersion(t *testing.T) {
	dateVersion := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	tests := []struct {
		name      string
		requested string
		// echo: a supported request is answered with the same version;
		// otherwise the server answers with a version of its own.
		echo bool
	}{
		{name: "supported 2024-11-05 is kept", requested: "2024-11-05", echo: true},
		{name: "supported 2025-03-26 is kept", requested: "2025-03-26", echo: true},
		{name: "supported 2025-06-18 is kept", requested: "2025-06-18", echo: true},
		{name: "unsupported version is answered with a supported one", requested: "1999-01-01", echo: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := rawInitialize(t, tt.requested)
			result, ok := resp["result"].(map[string]any)
			if !ok {
				t.Fatalf("initialize response has no result: %v", resp)
			}
			got, _ := result["protocolVersion"].(string)
			if tt.echo && got != tt.requested {
				t.Errorf("requested %s, server answered protocolVersion %q, want the same", tt.requested, got)
			}
			if !tt.echo && (got == tt.requested || !dateVersion.MatchString(got)) {
				t.Errorf("requested unsupported %s, server answered %q, want a different dated version", tt.requested, got)
			}
		})
	}
}

// startHTTPServer serves the full surface over loopback Streamable HTTP and
// returns the base URL.
func startHTTPServer(t *testing.T) string {
	t.Helper()
	addr := freeLoopbackAddr(t)

	srv := New("fs-version", nil)
	srv.store = newFixtureStore()
	srv.registerTools()
	srv.registerResources()
	srv.registerPrompts()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.ServeHTTP(ctx, addr)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	waitForListen(t, addr)
	return "http://" + addr
}

// postJSONRPC sends one JSON-RPC message and returns the status and the JSON-RPC
// reply, read from either a JSON or an event-stream body.
func postJSONRPC(t *testing.T, url, body string, header map[string]string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	payload := raw
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		payload = nil
		for line := range bytes.SplitSeq(raw, []byte("\n")) {
			if d, ok := bytes.CutPrefix(line, []byte("data:")); ok {
				payload = bytes.TrimSpace(d)
				break
			}
		}
	}
	var msg map[string]any
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("POST %s reply is not JSON-RPC: %v\n%s", url, err, raw)
	}
	return resp.StatusCode, msg
}

const initializeRequestBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"fs-http","version":"0"}}}`

// TestServeHTTPAnswersOnlyMCPPath posts the same initialize to /mcp and to other
// paths: only /mcp answers.
func TestServeHTTPAnswersOnlyMCPPath(t *testing.T) {
	base := startHTTPServer(t)
	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{name: "mcp path serves", path: "/mcp", wantStatus: http.StatusOK},
		{name: "root path does not serve", path: "/", wantStatus: http.StatusNotFound},
		{name: "trailing slash does not serve", path: "/mcp/", wantStatus: http.StatusNotFound},
		{name: "sub path does not serve", path: "/mcp/messages", wantStatus: http.StatusNotFound},
		{name: "sse path does not serve", path: "/sse", wantStatus: http.StatusNotFound},
		{name: "messages path does not serve", path: "/messages", wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, msg := postJSONRPC(t, base+tt.path, initializeRequestBody, nil)
			if status != tt.wantStatus {
				t.Fatalf("POST initialize to %s: status %d, want %d", tt.path, status, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				if result, _ := msg["result"].(map[string]any); result["serverInfo"] == nil {
					t.Errorf("POST initialize to %s: reply %v carries no initialize result", tt.path, msg)
				}
			}
		})
	}
}

// TestMCPServeServerInfoIsCLIVersion builds the CLI with a distinctive version,
// reads `sqlgen --version`, and checks `sqlgen mcp serve` advertises the same
// version as serverInfo.version at initialize.
func TestMCPServeServerInfoIsCLIVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary-building test in short mode")
	}
	const stamped = "7.7.7-fs"
	bin := filepath.Join(t.TempDir(), "sqlgen")
	build := exec.CommandContext(t.Context(), "go", "build", //nolint:gosec // output path is a t.TempDir file
		"-ldflags", "-X github.com/teandresmith/sqlgen/cmd/sqlgen/cli.Version="+stamped, "-o", bin, ".")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building sqlgen: %v\n%s", err, out)
	}

	out, err := exec.CommandContext(t.Context(), bin, "--version").Output() //nolint:gosec // test binary built above
	if err != nil {
		t.Fatalf("sqlgen --version: %v", err)
	}
	binaryVersion, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "sqlgen ")
	if !ok || binaryVersion != stamped {
		t.Fatalf("sqlgen --version = %q, want %q", out, "sqlgen "+stamped)
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	manifest, err := filepath.Abs("testdata/manifest_gen.json")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, bin, "mcp", "serve", "--no-watch", "--manifest", manifest) //nolint:gosec // test binary built above
	client := mcp.NewClient(&mcp.Implementation{Name: "fs-version-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to sqlgen mcp serve: %v", err)
	}
	defer func() { _ = cs.Close() }()

	info := cs.InitializeResult().ServerInfo
	if info == nil || info.Version != binaryVersion {
		t.Errorf("serverInfo = %+v, want version %q (the binary's --version)", info, binaryVersion)
	}
}
