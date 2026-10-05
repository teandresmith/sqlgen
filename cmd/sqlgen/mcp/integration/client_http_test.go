package integration

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os/exec"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// freePort reserves an ephemeral loopback port and releases it for the
// subprocess to bind. The tiny reuse race is absorbed by the connect retry
// loop in connectHTTP.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving loopback port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// connectHTTP spawns `sqlgen mcp serve --stdio=false --http <port>` and
// connects an SDK client to the loopback Streamable HTTP endpoint, retrying
// until the subprocess is listening.
func connectHTTP(ctx context.Context, t *testing.T, manifestPath string, opts *mcp.ClientOptions, extraArgs ...string) *mcp.ClientSession {
	t.Helper()
	port := freePort(t)
	args := append([]string{"mcp", "serve", "--manifest", manifestPath, "--stdio=false", "--http", strconv.Itoa(port)}, extraArgs...)
	cmd := exec.Command(sqlgenBin, args...) //nolint:gosec // test binary built in TestMain
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting HTTP server subprocess: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() && stderr.Len() > 0 {
			t.Logf("server stderr:\n%s", stderr.String())
		}
	})

	client := mcp.NewClient(&mcp.Implementation{Name: "sqlgen-integration-test", Version: "0.0.0"}, opts)
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
	var session *mcp.ClientSession
	var connectErr error
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		session, connectErr = client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
		if connectErr == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if connectErr != nil {
		t.Fatalf("connecting to %s: %v\nserver stderr:\n%s", endpoint, connectErr, stderr.String())
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// TestHTTPLifecycle drives the full §7.2 lifecycle over loopback Streamable
// HTTP against a --stdio=false subprocess.
func TestHTTPLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	session := connectHTTP(ctx, t, tempManifest(t), nil, "--no-watch")
	driveLifecycle(ctx, t, session)
}

// TestHTTPWatchReloadNoPushClientRefetches covers the §5.1/§3.1 HTTP caveat:
// the stateless HTTP transport pushes no notifications on a watch reload —
// the client's own next call surfaces the change instead.
func TestHTTPWatchReloadNoPushClientRefetches(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	manifestPath := tempManifest(t)
	var pushed atomic.Bool
	opts := &mcp.ClientOptions{
		ToolListChangedHandler:     func(context.Context, *mcp.ToolListChangedRequest) { pushed.Store(true) },
		ResourceListChangedHandler: func(context.Context, *mcp.ResourceListChangedRequest) { pushed.Store(true) },
	}
	session := connectHTTP(ctx, t, manifestPath, opts) // watch defaults on

	before := entityNames(callTool(ctx, t, session, "sqlgen_list_entities", map[string]any{}))
	if !slices.Contains(before, "ActiveUser") {
		t.Fatalf("pre-mutation entities = %v, want ActiveUser present", before)
	}

	atomicReplace(t, manifestPath, renameEntity(t, manifestPath, "ActiveUser", "ArchivedUser"))

	// Wait until the server has observably reloaded (its next responses carry
	// the new data), polling via fresh calls — the refetch contract itself.
	awaitCondition(t, 10*time.Second, func() bool {
		out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "sqlgen_list_entities", Arguments: map[string]any{}})
		if err != nil || out.IsError {
			return false
		}
		return slices.Contains(entityNames(structuredTree(t, "sqlgen_list_entities", out)), "ArchivedUser")
	}, "HTTP re-query to reflect the reloaded manifest")

	// No notification was pushed at any point (stateless HTTP, §5.1).
	if pushed.Load() {
		t.Errorf("HTTP client received a list_changed notification, want none (stateless no-push contract)")
	}
}
