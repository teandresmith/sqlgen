package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Options configures a `sqlgen mcp serve` invocation.
type Options struct {
	// Version is the sqlgen binary version reported as the server
	// implementation version at handshake.
	Version string
	// ManifestPath is the --manifest flag value; empty triggers discovery.
	ManifestPath string
	// Stdio enables the stdio transport (default).
	Stdio bool
	// HTTPAddr is the resolved loopback bind address from ResolveHTTPAddr, or
	// "" to disable the HTTP transport.
	HTTPAddr string
	// Watch enables manifest watch mode.
	Watch bool
	// Logger receives server activity (stderr or --log target). Never stdout.
	Logger *slog.Logger
}

// Run serves the MCP server over the transports selected by opts until ctx is
// cancelled (e.g. SIGINT) or a transport fails. stdio and loopback HTTP can run
// concurrently in one process (MCP.md §5.1); the first transport to return
// tears the others down. ctx cancellation is a clean shutdown (returns nil).
func Run(ctx context.Context, in io.ReadCloser, out io.WriteCloser, opts Options) error {
	if !opts.Stdio && opts.HTTPAddr == "" {
		return errors.New("no transport enabled: pass --stdio and/or --http <port>")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	// Resolve and load the manifest before serving — the server has nothing to
	// expose without it, so a missing / invalid manifest fails fast (MCP.md §3.1).
	path, err := resolveManifestPath(opts.ManifestPath)
	if err != nil {
		return err
	}
	store := NewStore(path, logger)
	if err := store.Load(); err != nil {
		return err
	}

	srv := New(opts.Version, logger)
	srv.store = store

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Watch mode reloads the manifest on change; opts.Watch already folds in
	// --no-watch, so a false value creates no watcher and zero fsnotify activity.
	var wg sync.WaitGroup
	if opts.Watch {
		// A watcher-setup failure (e.g. an fsnotify/inotify resource limit) must
		// not sink a server whose manifest already loaded cleanly — degrade to
		// no-watch and keep serving the good manifest.
		if watcher, err := NewWatcher(store, srv.onManifestReload, logger); err != nil {
			logger.Warn("manifest watch disabled: watcher setup failed", "error", err)
		} else {
			srv.watchEnabled = true
			wg.Go(func() {
				if err := watcher.Run(runCtx); err != nil {
					logger.Warn("manifest watcher stopped", "error", err)
				}
			})
		}
	}

	// Register the tool, resource, and prompt surfaces before serving so
	// tools/list, resources/list, and prompts/list are populated at handshake.
	// watchEnabled is now settled, so sqlgen_health reports it correctly. Tools
	// and resources re-register on reload (list_changed); prompts are static.
	srv.registerTools()
	srv.registerResources()
	srv.registerPrompts()

	errCh := make(chan error, 2)
	var n int
	if opts.Stdio {
		n++
		go func() { errCh <- srv.ServeStdio(runCtx, in, out) }()
	}
	if opts.HTTPAddr != "" {
		n++
		go func() { errCh <- srv.ServeHTTP(runCtx, opts.HTTPAddr) }()
	}

	var firstErr error
	for range n {
		if err := cleanShutdownErr(<-errCh); err != nil && firstErr == nil {
			firstErr = err
		}
		// Once any transport returns, tear the siblings down so Run unblocks.
		cancel()
	}
	wg.Wait()
	return firstErr
}

// resolveManifestPath returns the explicit --manifest value, or discovers a
// manifest_gen.json by walking up from the working directory (MCP.md §3.1).
func resolveManifestPath(manifestFlag string) (string, error) {
	if manifestFlag != "" {
		return manifestFlag, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return DiscoverManifest(cwd)
}

// cleanShutdownErr maps a context-cancellation (the SIGINT path) to nil so a
// graceful shutdown exits successfully, while surfacing genuine transport
// errors.
func cleanShutdownErr(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// NewLogger builds the server's slog logger writing to w (stderr or the --log
// file — never stdout, which is reserved for JSON-RPC framing). level is one of
// debug|info|warn|error.
func NewLogger(w io.Writer, level string) (*slog.Logger, error) {
	var lv slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lv = slog.LevelDebug
	case "", "info":
		lv = slog.LevelInfo
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		return nil, fmt.Errorf("invalid log level %q: want debug|info|warn|error", level)
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: lv})), nil
}
