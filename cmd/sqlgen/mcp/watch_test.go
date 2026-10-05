package mcp

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// startWatcher boots a Watcher on store with the given debounce, returning a
// channel that receives once per successful reload and a stop func that cancels
// the run loop and waits for it to exit.
func startWatcher(t *testing.T, store *Store, debounce time.Duration) (<-chan struct{}, func()) {
	t.Helper()
	reloaded := make(chan struct{}, 64)
	w, err := NewWatcher(store, func() { reloaded <- struct{}{} }, nil)
	if err != nil {
		t.Fatalf("NewWatcher error: %v", err)
	}
	w.debounce = debounce

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(done)
	}()
	return reloaded, func() {
		cancel()
		<-done
	}
}

// waitForReload blocks until a reload signal arrives or the deadline passes.
func waitForReload(t *testing.T, reloaded <-chan struct{}) {
	t.Helper()
	select {
	case <-reloaded:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher did not reload within 3s of a manifest change")
	}
}

// mutatedManifest returns the fixture with a distinct generated_at so a reload
// is observably different from the original.
func mutatedManifest(t *testing.T) []byte {
	t.Helper()
	return bytes.Replace(fixtureManifest(t),
		[]byte(`"generated_at": "2026-05-15T14:22:11Z"`),
		[]byte(`"generated_at": "2027-01-02T03:04:05Z"`), 1)
}

func TestWatcherReloadsOnWrite(t *testing.T) {
	path := writeManifest(t, fixtureManifest(t))
	store := NewStore(path, nil)
	if err := store.Load(); err != nil {
		t.Fatalf("initial Load: %v", err)
	}

	reloaded, stop := startWatcher(t, store, 20*time.Millisecond)
	defer stop()

	if err := os.WriteFile(path, mutatedManifest(t), 0o600); err != nil {
		t.Fatalf("rewrite manifest: %v", err)
	}
	waitForReload(t, reloaded)

	if got := store.Manifest().GeneratedAt; got != "2027-01-02T03:04:05Z" {
		t.Errorf("after reload GeneratedAt = %q, want the mutated value", got)
	}
}

func TestWatcherSurvivesRenameReplace(t *testing.T) {
	path := writeManifest(t, fixtureManifest(t))
	store := NewStore(path, nil)
	if err := store.Load(); err != nil {
		t.Fatalf("initial Load: %v", err)
	}

	reloaded, stop := startWatcher(t, store, 20*time.Millisecond)
	defer stop()

	// Atomic-save pattern: write a sibling temp file, then rename it over the
	// target. Watching the directory (not the inode) survives the swap.
	tmp := filepath.Join(filepath.Dir(path), "manifest_gen.json.tmp")
	if err := os.WriteFile(tmp, mutatedManifest(t), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("rename over target: %v", err)
	}
	waitForReload(t, reloaded)

	if got := store.Manifest().GeneratedAt; got != "2027-01-02T03:04:05Z" {
		t.Errorf("after rename-replace GeneratedAt = %q, want the mutated value", got)
	}
}

func TestWatcherRetainsGoodOnInvalidReload(t *testing.T) {
	path := writeManifest(t, fixtureManifest(t))
	store := NewStore(path, nil)
	if err := store.Load(); err != nil {
		t.Fatalf("initial Load: %v", err)
	}
	good := store.Manifest()

	reloaded, stop := startWatcher(t, store, 20*time.Millisecond)
	defer stop()

	if err := os.WriteFile(path, []byte(`{"schema_version":"0.1.0"}`), 0o600); err != nil {
		t.Fatalf("write invalid manifest: %v", err)
	}

	// A failed reload must NOT signal onReload; the last good manifest keeps
	// serving and health flips to not-ok.
	select {
	case <-reloaded:
		t.Fatal("watcher signaled a reload for an invalid manifest, want none")
	case <-time.After(500 * time.Millisecond):
	}

	if store.Manifest() != good {
		t.Error("invalid reload swapped out the last good manifest")
	}
	if store.Health().OK {
		t.Error("Health().OK = true after an invalid reload, want false")
	}
}

func TestWatcherDebounceCoalesces(t *testing.T) {
	path := writeManifest(t, fixtureManifest(t))
	store := NewStore(path, nil)
	if err := store.Load(); err != nil {
		t.Fatalf("initial Load: %v", err)
	}

	// A debounce well above the burst duration collapses many events into one
	// reload.
	reloaded, stop := startWatcher(t, store, 200*time.Millisecond)
	defer stop()

	const writes = 6
	for range writes {
		if err := os.WriteFile(path, mutatedManifest(t), 0o600); err != nil {
			t.Fatalf("burst write: %v", err)
		}
	}

	// Wait past the debounce window, then count the coalesced reloads.
	waitForReload(t, reloaded)
	time.Sleep(300 * time.Millisecond)
	count := 1
	for {
		select {
		case <-reloaded:
			count++
		default:
			if count >= writes {
				t.Errorf("burst of %d writes produced %d reloads, want them coalesced", writes, count)
			}
			return
		}
	}
}
