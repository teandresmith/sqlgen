package mcp

import (
	"context"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServeStdio runs the server over newline-delimited JSON-RPC 2.0 on the given
// reader/writer — os.Stdin/os.Stdout in production, injectable streams in
// tests. This is the default transport: the agent client spawns the server as
// a subprocess and drives it over the pipe (MCP.md §5.1, §5.2).
//
// stdout carries only JSON-RPC framing; all logging is routed to the server's
// logger (stderr or --log target), so the two streams never interleave.
func (s *Server) ServeStdio(ctx context.Context, in io.ReadCloser, out io.WriteCloser) error {
	return s.Serve(ctx, &mcp.IOTransport{Reader: in, Writer: out})
}
