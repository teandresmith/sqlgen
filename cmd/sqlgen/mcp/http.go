package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// httpPath is the single Streamable HTTP endpoint (MCP.md §5.1).
const httpPath = "/mcp"

// httpShutdownTimeout bounds graceful HTTP shutdown after ctx cancellation.
const httpShutdownTimeout = 5 * time.Second

// httpReadHeaderTimeout bounds request-header reads to blunt Slowloris-style
// connection holding, even on a loopback-only endpoint.
const httpReadHeaderTimeout = 10 * time.Second

// ResolveHTTPAddr resolves the --http flag value to a loopback bind address,
// refusing any non-loopback host at flag-parse time (MCP.md §3.1, §5.1). The
// Streamable HTTP transport binds 127.0.0.1 only — consumers needing remote
// access put their own auth proxy in front of the loopback endpoint.
//
// Accepted forms: a bare port ("8080" → 127.0.0.1:8080), an empty host
// (":8080" → 127.0.0.1:8080), or an explicit loopback IP literal
// ("127.0.0.1:8080", "[::1]:8080"). Any non-loopback IP (0.0.0.0, an external
// address) or a hostname (which cannot be verified as loopback without DNS
// resolution — the DNS-rebinding surface) is refused.
func ResolveHTTPAddr(flag string) (string, error) {
	flag = strings.TrimSpace(flag)
	if flag == "" {
		return "", errors.New("--http requires a port or loopback address")
	}

	var host, port string
	if strings.Contains(flag, ":") {
		h, p, err := net.SplitHostPort(flag)
		if err != nil {
			return "", fmt.Errorf("--http address %q: %w", flag, err)
		}
		host, port = h, p
		if host == "" {
			host = "127.0.0.1"
		}
	} else {
		host, port = "127.0.0.1", flag
	}

	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("--http port %q must be an integer in [1, 65535]", port)
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return "", fmt.Errorf("--http host %q is not an IP address; only loopback (127.0.0.1, ::1) is allowed", host)
	}
	if !ip.IsLoopback() {
		return "", fmt.Errorf("--http host %q is not a loopback address; sqlgen mcp serve binds 127.0.0.1 only", host)
	}
	return net.JoinHostPort(host, port), nil
}

// ServeHTTP runs the Streamable HTTP transport (stateless, loopback-bound) at
// addr until ctx is cancelled or the listener fails. addr must already be a
// validated loopback address from ResolveHTTPAddr.
//
// Stateless mode means the server cannot push notifications to HTTP clients: on
// a watch-mode reload, tools/list_changed is a no-op here and HTTP clients
// refetch tools/list themselves (MCP.md §5.1).
func (s *Server) ServeHTTP(ctx context.Context, addr string) error {
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return s.mcp },
		&mcp.StreamableHTTPOptions{Stateless: true, Logger: s.logger},
	)
	mux := http.NewServeMux()
	mux.Handle(httpPath, handler)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: httpReadHeaderTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("mcp http transport listening", "addr", addr, "path", httpPath)
		err := httpSrv.ListenAndServe()
		switch {
		case errors.Is(err, http.ErrServerClosed):
			err = nil
		case err != nil:
			err = fmt.Errorf("mcp http listen %s: %w", addr, err)
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
		defer cancel()
		if err := httpSrv.Shutdown(shutCtx); err != nil {
			return fmt.Errorf("mcp http shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}
