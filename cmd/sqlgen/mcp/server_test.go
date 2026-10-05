package mcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	sqlgenmcp "github.com/teandresmith/sqlgen/cmd/sqlgen/mcp"
)

// connect starts the server on one end of an in-memory transport pair and
// returns an initialized client session on the other. It replays the real MCP
// initialize handshake through the SDK.
func connect(ctx context.Context, t *testing.T) (*mcp.ClientSession, <-chan error) {
	t.Helper()
	serverT, clientT := mcp.NewInMemoryTransports()

	srv := sqlgenmcp.New("test-version", nil)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client.Connect() error: %v", err)
	}
	return cs, serveErr
}

func TestHandshakeAdvertisesExactlyThreeCapabilities(t *testing.T) {
	cs, _ := connect(t.Context(), t)
	defer func() { _ = cs.Close() }()

	caps := cs.InitializeResult().Capabilities
	if caps == nil {
		t.Fatal("InitializeResult().Capabilities = nil, want tools+resources+prompts")
	}

	// Exactly the three advertised capabilities are present.
	if caps.Tools == nil {
		t.Error("tools capability not advertised, want advertised")
	}
	if caps.Resources == nil {
		t.Error("resources capability not advertised, want advertised")
	}
	if caps.Prompts == nil {
		t.Error("prompts capability not advertised, want advertised")
	}

	// Nothing else is advertised — notably not logging (the SDK's historical
	// default), completions, subscriptions, experimental, or extensions.
	if caps.Logging != nil {
		t.Errorf("logging capability = %+v, want nil (not advertised)", caps.Logging)
	}
	if caps.Completions != nil {
		t.Errorf("completions capability = %+v, want nil (not advertised)", caps.Completions)
	}
	if len(caps.Experimental) != 0 {
		t.Errorf("experimental capabilities = %v, want none", caps.Experimental)
	}
	if len(caps.Extensions) != 0 {
		t.Errorf("extensions = %v, want none", caps.Extensions)
	}

	// listChanged contract: tools + resources fire on watch reload; prompts
	// never change at runtime.
	if !caps.Tools.ListChanged {
		t.Error("tools.listChanged = false, want true (watch-mode reload)")
	}
	if !caps.Resources.ListChanged {
		t.Error("resources.listChanged = false, want true (watch-mode reload)")
	}
	if caps.Prompts.ListChanged {
		t.Error("prompts.listChanged = true, want false (prompts are static)")
	}
	if caps.Resources.Subscribe {
		t.Error("resources.subscribe = true, want false (subscriptions not advertised in v1)")
	}
}

func TestHandshakeReportsServerVersion(t *testing.T) {
	cs, _ := connect(t.Context(), t)
	defer func() { _ = cs.Close() }()

	info := cs.InitializeResult().ServerInfo
	if info == nil {
		t.Fatal("InitializeResult().ServerInfo = nil")
	}
	if info.Name != "sqlgen" {
		t.Errorf("ServerInfo.Name = %q, want %q", info.Name, "sqlgen")
	}
	if info.Version != "test-version" {
		t.Errorf("ServerInfo.Version = %q, want %q", info.Version, "test-version")
	}
}

func TestServeStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())

	cs, serveErr := connect(ctx, t)

	// Cancelling the context is the graceful-shutdown mechanism SIGINT drives.
	cancel()
	_ = cs.Close()

	select {
	case <-serveErr:
		// Serve returned — the transport shut down on context cancel.
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return within 2s of context cancel")
	}
}
