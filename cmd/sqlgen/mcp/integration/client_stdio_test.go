package integration

import (
	"bytes"
	"context"
	"os/exec"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectStdio spawns `sqlgen mcp serve` as a subprocess and connects an SDK
// client over its stdio (MCP.md §7.2). extraArgs append to the serve command;
// opts may carry notification handlers.
func connectStdio(ctx context.Context, t *testing.T, manifestPath string, opts *mcp.ClientOptions, extraArgs ...string) *mcp.ClientSession {
	t.Helper()
	args := append([]string{"mcp", "serve", "--manifest", manifestPath}, extraArgs...)
	cmd := exec.Command(sqlgenBin, args...) //nolint:gosec // test binary built in TestMain
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "sqlgen-integration-test", Version: "0.0.0"}, opts)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting over stdio: %v\nserver stderr:\n%s", err, stderr.String())
	}
	t.Cleanup(func() {
		_ = session.Close()
		if t.Failed() && stderr.Len() > 0 {
			t.Logf("server stderr:\n%s", stderr.String())
		}
	})
	return session
}

// TestStdioLifecycle drives the full §7.2 lifecycle over stdio: initialize
// (inside Connect) → tools/list → tools/call ×11 → resources/list →
// resources/read ×4 → prompts/list → prompts/get ×3 → shutdown (Close).
func TestStdioLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	session := connectStdio(ctx, t, tempManifest(t), nil, "--no-watch")
	driveLifecycle(ctx, t, session)

	if err := session.Close(); err != nil {
		t.Fatalf("closing stdio session (server shutdown): %v", err)
	}
}

// TestStdioWatchReloadNotifiesAndServesNewData covers the §5.2 watch contract
// over stdio and the cross-cutting regen-mid-conversation guarantee: mutating
// the manifest (atomic rename-replace, as a `sqlgen generate` regen does)
// fires tools/list_changed + resources/list_changed on the live session, the
// session survives, and re-queries reflect the new data.
func TestStdioWatchReloadNotifiesAndServesNewData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	manifestPath := tempManifest(t)
	var toolsChanged, resourcesChanged atomic.Bool
	opts := &mcp.ClientOptions{
		ToolListChangedHandler:     func(context.Context, *mcp.ToolListChangedRequest) { toolsChanged.Store(true) },
		ResourceListChangedHandler: func(context.Context, *mcp.ResourceListChangedRequest) { resourcesChanged.Store(true) },
	}
	session := connectStdio(ctx, t, manifestPath, opts) // watch defaults on

	// Pre-mutation sanity: the fixture serves ActiveUser.
	before := entityNames(callTool(ctx, t, session, "sqlgen_list_entities", map[string]any{}))
	if !slices.Contains(before, "ActiveUser") {
		t.Fatalf("pre-mutation entities = %v, want ActiveUser present", before)
	}

	// Regen mid-conversation: rename the ActiveUser view via rename-replace.
	atomicReplace(t, manifestPath, renameEntity(t, manifestPath, "ActiveUser", "ArchivedUser"))

	awaitCondition(t, 10*time.Second, toolsChanged.Load, "notifications/tools/list_changed")
	awaitCondition(t, 10*time.Second, resourcesChanged.Load, "notifications/resources/list_changed")

	// The same session (no reconnect) serves the new data.
	after := entityNames(callTool(ctx, t, session, "sqlgen_list_entities", map[string]any{}))
	if !slices.Contains(after, "ArchivedUser") || slices.Contains(after, "ActiveUser") {
		t.Errorf("post-reload entities = %v, want ArchivedUser present and ActiveUser gone", after)
	}

	healthOut := callTool(ctx, t, session, "sqlgen_health", map[string]any{})
	if healthOut["ok"] != true {
		t.Errorf("health.ok after reload = %v, want true", healthOut["ok"])
	}
	if healthOut["last_reload_at"] == nil {
		t.Errorf("health.last_reload_at absent after a reload: %v", healthOut)
	}
}
