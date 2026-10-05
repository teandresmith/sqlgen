// Package mcp implements the `sqlgen mcp serve` Model Context Protocol server.
// It is a read-only consumer of the generated manifest (manifest_gen.json),
// exposing it to AI agents as tools, resources, and prompts over stdio
// (default) and loopback-bound Streamable HTTP.
//
// The JSON-RPC 2.0 framing, capability handshake, version negotiation, and
// notification semantics come from the official Go SDK
// (github.com/modelcontextprotocol/go-sdk); this package owns the sqlgen
// surface (tools, resources, prompts, manifest store, watch, .mcp.json merge).
// The dependency is scoped to the CLI module only — the runtime (./) and
// parser (parser/) modules stay untouched (MCP.md §6.4, CLAUDE.md).
package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverName is the MCP implementation name advertised at handshake and used
// by agent clients for tool-list namespacing (MCP.md §3.3, §4.4).
const serverName = "sqlgen"

// Server wraps the SDK MCP server with the sqlgen surface. A single Server can
// serve stdio and loopback HTTP concurrently in one process (MCP.md §5.1); both
// transports share the same underlying *mcp.Server.
type Server struct {
	logger  *slog.Logger
	mcp     *mcp.Server
	store   *Store // manifest source; set by Run after discovery + initial load
	version string // sqlgen binary version, reported by sqlgen_health
	// watchEnabled reports whether a manifest watcher is actually running,
	// surfaced by sqlgen_health. Run sets it true only after the watcher starts,
	// so a --no-watch run or a degraded watcher-setup failure reads as false.
	watchEnabled bool
}

// New builds a Server advertising exactly the tools, resources, and prompts
// capabilities (MCP.md §5.1). version is the sqlgen binary version reported as
// the server implementation version at handshake; logger receives server
// activity (never stdout, which is reserved for JSON-RPC framing). A nil logger
// discards output.
func New(version string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	impl := &mcp.Implementation{Name: serverName, Version: version}
	ms := mcp.NewServer(impl, &mcp.ServerOptions{
		Logger:       logger,
		Capabilities: advertisedCapabilities(),
	})
	return &Server{logger: logger, mcp: ms, version: version}
}

// advertisedCapabilities pins the handshake to exactly tools + resources +
// prompts (MCP.md §5.1). A non-nil ServerCapabilities suppresses the SDK's
// historical default logging capability, and each explicit field overrides the
// value the SDK would otherwise infer when features are registered:
//
//   - tools / resources advertise listChanged — the watch-mode reload emits
//     notifications/tools/list_changed and notifications/resources/list_changed
//     (MCP.md §5.2, wired in 19.2).
//   - prompts advertise the capability without listChanged: prompts never
//     change at runtime, so no prompts/list_changed is ever emitted (MCP.md
//     §4.3, §5.2).
//
// Subscriptions, sampling, and logging capabilities are deliberately not
// advertised in v1.
func advertisedCapabilities() *mcp.ServerCapabilities {
	return &mcp.ServerCapabilities{
		Tools:     &mcp.ToolCapabilities{ListChanged: true},
		Resources: &mcp.ResourceCapabilities{ListChanged: true},
		Prompts:   &mcp.PromptCapabilities{},
	}
}

// Serve runs the MCP server over the given transport until ctx is cancelled or
// the peer disconnects. Cancelling ctx closes the connection cleanly; the
// resulting context.Canceled is unwrapped to a clean shutdown by the caller.
func (s *Server) Serve(ctx context.Context, t mcp.Transport) error {
	if err := s.mcp.Run(ctx, t); err != nil {
		return fmt.Errorf("mcp serve: %w", err)
	}
	return nil
}

// onManifestReload is invoked by the watcher after a successful manifest reload
// (MCP.md §5.2). The store has already swapped in the new manifest; the tool
// and resource registries re-register their surfaces here so the
// SDK emits notifications/tools/list_changed and
// notifications/resources/list_changed to connected stdio clients. Stateless
// HTTP clients receive nothing and refetch on their own (MCP.md §5.1). Prompts
// are static, so no prompts/list_changed is ever emitted (MCP.md §4.3).
func (s *Server) onManifestReload() {
	path := ""
	if s.store != nil {
		path = s.store.Path()
	}
	s.logger.Info("manifest changed; clients will refresh tool + resource lists", "path", path)
	// Re-registering the (unchanged) tool + resource sets fires
	// notifications/tools/list_changed and notifications/resources/list_changed via
	// the SDK's changeAndNotify path, so connected stdio clients re-query and pick
	// up the reloaded manifest data (MCP.md §5.2). The surfaces themselves are
	// static — only their responses change — but the notification is the contract.
	// Prompts are static and are never re-registered (no prompts/list_changed).
	s.registerTools()
	s.registerResources()
}
