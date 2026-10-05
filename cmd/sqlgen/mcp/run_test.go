package mcp_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	sqlgenmcp "github.com/teandresmith/sqlgen/cmd/sqlgen/mcp"
)

// nopWriteCloser adapts an io.Writer to io.WriteCloser for stdio stubs.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// freeLoopbackAddr reserves an ephemeral loopback port and returns its address.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("release reserved port: %v", err)
	}
	return addr
}

// waitForListen blocks until addr accepts a TCP connection or the deadline
// passes, confirming a transport bound before the test cancels it.
func waitForListen(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("http transport did not begin listening on %s within 2s", addr)
}

func TestRunNoTransportEnabled(t *testing.T) {
	err := sqlgenmcp.Run(t.Context(), io.NopCloser(strings.NewReader("")), nopWriteCloser{io.Discard}, sqlgenmcp.Options{
		Version: "test",
		Stdio:   false,
		// no HTTPAddr either
	})
	if err == nil {
		t.Fatal("Run() with no transport enabled = nil, want error")
	}
}

func TestRunGracefulShutdown(t *testing.T) {
	tests := []struct {
		name     string
		stdio    bool
		withHTTP bool
	}{
		{name: "stdio only", stdio: true},
		{name: "http only", withHTTP: true},
		{name: "stdio and http coexist", stdio: true, withHTTP: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())

			// A pipe reader blocks until closed — mimics an idle stdin that the
			// server waits on until shutdown.
			inR, inW := io.Pipe()
			t.Cleanup(func() { _ = inW.Close() })

			opts := sqlgenmcp.Options{Version: "test", Stdio: tt.stdio, ManifestPath: "testdata/manifest_gen.json"}
			var addr string
			if tt.withHTTP {
				addr = freeLoopbackAddr(t)
				opts.HTTPAddr = addr
			}

			done := make(chan error, 1)
			go func() {
				done <- sqlgenmcp.Run(ctx, inR, nopWriteCloser{io.Discard}, opts)
			}()

			if tt.withHTTP {
				waitForListen(t, addr)
			}

			cancel()

			select {
			case err := <-done:
				if err != nil {
					t.Errorf("Run() on context cancel = %v, want nil (graceful shutdown)", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Run() did not return within 3s of context cancel")
			}

			if tt.withHTTP {
				// The loopback port is released after graceful shutdown.
				if conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
					_ = conn.Close()
					t.Errorf("http transport still listening on %s after shutdown", addr)
				}
			}
		})
	}
}

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name    string
		level   string
		wantErr bool
	}{
		{name: "debug", level: "debug"},
		{name: "info", level: "info"},
		{name: "warn", level: "warn"},
		{name: "error", level: "error"},
		{name: "empty defaults to info", level: ""},
		{name: "case insensitive", level: "INFO"},
		{name: "padded", level: "  warn  "},
		{name: "invalid level", level: "trace", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger, err := sqlgenmcp.NewLogger(&buf, tt.level)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewLogger(%q) = %v, want error", tt.level, logger)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewLogger(%q) unexpected error: %v", tt.level, err)
			}
			if logger == nil {
				t.Fatal("NewLogger() returned nil logger")
			}
			// Logs route to the provided sink, never stdout.
			logger.Error("boom")
			if !strings.Contains(buf.String(), "boom") {
				t.Errorf("NewLogger(%q) log output = %q, want it to contain %q", tt.level, buf.String(), "boom")
			}
		})
	}
}
