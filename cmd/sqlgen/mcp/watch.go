package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchDebounce coalesces a burst of filesystem events — editors (and sqlgen's
// own atomic writer) emit several write/rename events per save — into a single
// reload.
const watchDebounce = 50 * time.Millisecond

// Watcher reloads the manifest store when its file changes on disk. It watches
// the containing directory rather than the file inode directly so it survives
// the write-temp-then-rename save pattern: a rename swaps the inode, which a
// file-level watch would lose track of (MCP.md §6.3). Watch mode is on by
// default; --no-watch skips constructing a Watcher entirely.
type Watcher struct {
	store    *Store
	fsw      *fsnotify.Watcher
	base     string // manifest file base name; events on the watched dir are filtered to it
	onReload func()
	logger   *slog.Logger
	debounce time.Duration
}

// NewWatcher builds a Watcher on the store's manifest file. onReload runs after
// each successful reload (the server wires it to emit tools/resources
// list_changed over stdio; MCP.md §5.2) and is skipped on a failed reload.
// Setup failures — an unwatchable directory — surface here, before the run loop
// starts.
func NewWatcher(store *Store, onReload func(), logger *slog.Logger) (*Watcher, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	target, err := filepath.Abs(store.path)
	if err != nil {
		return nil, fmt.Errorf("resolve manifest path %s: %w", store.path, err)
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create manifest watcher: %w", err)
	}
	if err := fsw.Add(filepath.Dir(target)); err != nil {
		_ = fsw.Close()
		return nil, fmt.Errorf("watch manifest directory %s: %w", filepath.Dir(target), err)
	}
	return &Watcher{
		store:    store,
		fsw:      fsw,
		base:     filepath.Base(target),
		onReload: onReload,
		logger:   logger,
		debounce: watchDebounce,
	}, nil
}

// Run consumes filesystem events until ctx is cancelled, debouncing bursts into
// a single reload. It closes the underlying fsnotify watcher on return. Watcher
// errors are logged and never fatal — the server keeps serving the last good
// manifest.
func (w *Watcher) Run(ctx context.Context) error {
	defer func() { _ = w.fsw.Close() }()

	// A stopped, pre-drained timer we arm on the first relevant event.
	timer := time.NewTimer(w.debounce)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	armed := false

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-w.fsw.Events:
			if !ok {
				return nil
			}
			if !w.relevant(event) {
				continue
			}
			if armed && !timer.Stop() {
				// Drain a fire that landed between Stop and now.
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(w.debounce)
			armed = true
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return nil
			}
			if err != nil {
				w.logger.Warn("manifest watcher error", "error", err)
			}
		case <-timer.C:
			armed = false
			w.reload()
		}
	}
}

// relevant reports whether an event concerns the watched manifest file and
// signals a content change. Chmod-only events are ignored. The file is matched
// by base name: the watched directory holds exactly one manifest_gen.json, and
// base-name matching is robust to the path representation differences fsnotify
// reports across platforms (e.g. macOS /private symlinks).
func (w *Watcher) relevant(e fsnotify.Event) bool {
	if filepath.Base(e.Name) != w.base {
		return false
	}
	return e.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0
}

// reload re-reads the manifest. A failed reload keeps the last good manifest and
// leaves health.ok false; onReload fires only on success.
func (w *Watcher) reload() {
	if err := w.store.Reload(); err != nil {
		w.logger.Warn("manifest reload failed; serving last good manifest", "error", err)
		return
	}
	w.logger.Info("manifest reloaded", "path", w.store.Path())
	if w.onReload != nil {
		w.onReload()
	}
}
