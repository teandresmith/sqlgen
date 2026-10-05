package mcp

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// fixtureManifest returns the bytes of the committed valid manifest fixture (a
// real single-layout manifest_gen.json).
func fixtureManifest(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/manifest_gen.json")
	if err != nil {
		t.Fatalf("read manifest fixture: %v", err)
	}
	return raw
}

// writeManifest writes content to a manifest_gen.json in a fresh temp dir and
// returns the path.
func writeManifest(t *testing.T, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest_gen.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write temp manifest: %v", err)
	}
	return path
}

func TestStoreLoadValid(t *testing.T) {
	path := writeManifest(t, fixtureManifest(t))
	s := NewStore(path, nil)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if s.Manifest() == nil {
		t.Fatal("Manifest() = nil after successful Load")
	}
	if got := s.Manifest().SchemaVersion; got != "0.1.0" {
		t.Errorf("SchemaVersion = %q, want %q", got, "0.1.0")
	}
	if !bytes.Equal(s.Raw(), fixtureManifest(t)) {
		t.Error("Raw() does not match the loaded fixture bytes")
	}

	h := s.Health()
	if !h.OK {
		t.Error("Health().OK = false after successful Load, want true")
	}
	if h.LoadedAt.IsZero() {
		t.Error("Health().LoadedAt is zero after Load")
	}
	if !h.LastReloadAt.IsZero() {
		t.Error("Health().LastReloadAt is non-zero before any reload")
	}
	if h.ManifestPath != path {
		t.Errorf("Health().ManifestPath = %q, want %q", h.ManifestPath, path)
	}
}

func TestStoreLoadErrors(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte // nil => point at a nonexistent file
		wantCode ErrorCode
	}{
		{
			name:     "missing file",
			content:  nil,
			wantCode: CodeManifestNotFound,
		},
		{
			name:     "malformed json",
			content:  []byte("{not json"),
			wantCode: CodeManifestInvalid,
		},
		{
			name:     "structurally invalid: missing required fields",
			content:  []byte(`{"schema_version":"0.1.0"}`),
			wantCode: CodeManifestInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var path string
			if tt.content == nil {
				path = filepath.Join(t.TempDir(), "manifest_gen.json") // never created
			} else {
				path = writeManifest(t, tt.content)
			}

			s := NewStore(path, nil)
			err := s.Load()
			if err == nil {
				t.Fatalf("Load() = nil, want error with code %d", tt.wantCode)
			}
			var coded *Error
			if !errors.As(err, &coded) {
				t.Fatalf("Load() error %v is not a *mcp.Error", err)
			}
			if coded.Code != tt.wantCode {
				t.Errorf("Load() error code = %d, want %d", coded.Code, tt.wantCode)
			}
			if s.Manifest() != nil {
				t.Error("Manifest() non-nil after a failed Load")
			}
		})
	}
}

func TestStoreLoadReadErrorIsInternal(t *testing.T) {
	// A path that exists but cannot be read as a file (here: a directory) is an
	// internal error (-32603), not a missing manifest (-32001).
	dir := t.TempDir()
	s := NewStore(dir, nil)
	err := s.Load()
	if err == nil {
		t.Fatal("Load() on a directory path = nil, want error")
	}
	var coded *Error
	if !errors.As(err, &coded) {
		t.Fatalf("Load() error %v is not a *mcp.Error", err)
	}
	if coded.Code != CodeInternal {
		t.Errorf("Load() on an unreadable path code = %d, want %d (internal)", coded.Code, CodeInternal)
	}
}

func TestStoreReloadRetainsGoodOnFailure(t *testing.T) {
	path := writeManifest(t, fixtureManifest(t))
	s := NewStore(path, nil)
	if err := s.Load(); err != nil {
		t.Fatalf("initial Load() error: %v", err)
	}
	good := s.Manifest()

	// Overwrite with an invalid manifest and reload.
	if err := os.WriteFile(path, []byte(`{"schema_version":"0.1.0"}`), 0o600); err != nil {
		t.Fatalf("overwrite manifest: %v", err)
	}
	err := s.Reload()
	if err == nil {
		t.Fatal("Reload() on invalid manifest = nil, want error")
	}
	var coded *Error
	if !errors.As(err, &coded) || coded.Code != CodeManifestInvalid {
		t.Errorf("Reload() error = %v, want code %d", err, CodeManifestInvalid)
	}

	// The last good manifest still serves; health flipped to not-ok.
	if s.Manifest() != good {
		t.Error("Reload() failure swapped out the last good manifest")
	}
	h := s.Health()
	if h.OK {
		t.Error("Health().OK = true after a failed reload, want false")
	}
	if h.LastReloadAt.IsZero() {
		t.Error("Health().LastReloadAt is zero after a reload attempt")
	}

	// A subsequent good reload recovers ok=true.
	if err := os.WriteFile(path, fixtureManifest(t), 0o600); err != nil {
		t.Fatalf("restore good manifest: %v", err)
	}
	if err := s.Reload(); err != nil {
		t.Fatalf("recovery Reload() error: %v", err)
	}
	if !s.Health().OK {
		t.Error("Health().OK = false after a good reload, want true")
	}
}

func TestStoreSchemaVersionMismatch(t *testing.T) {
	// A structurally-valid manifest whose schema_version differs from the
	// version sqlgen was built against loads successfully and logs a warning.
	raw := bytes.Replace(fixtureManifest(t),
		[]byte(`"schema_version": "0.1.0"`),
		[]byte(`"schema_version": "0.2.0"`), 1)
	path := writeManifest(t, raw)

	var logs bytes.Buffer
	logger, err := NewLogger(&logs, "warn")
	if err != nil {
		t.Fatalf("NewLogger error: %v", err)
	}
	s := NewStore(path, logger)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() error on version-mismatched manifest: %v", err)
	}

	if got := s.Health().SchemaVersion; got != "0.2.0" {
		t.Errorf("Health().SchemaVersion = %q, want %q", got, "0.2.0")
	}
	if !bytes.Contains(logs.Bytes(), []byte("schema_version")) {
		t.Errorf("expected a schema-version mismatch warning, log output = %q", logs.String())
	}
}

func TestStoreConcurrentReadsDuringReload(t *testing.T) {
	path := writeManifest(t, fixtureManifest(t))
	s := NewStore(path, nil)
	if err := s.Load(); err != nil {
		t.Fatalf("initial Load() error: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Readers hammer the atomic-swap accessors while a writer reloads. The -race
	// detector proves the swap never tears against a concurrent read.
	for range 8 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					_ = s.Manifest()
					_ = s.Raw()
					_ = s.Health()
				}
			}
		})
	}
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				_ = s.Reload()
			}
		}
	})

	// Let the goroutines run briefly, then stop.
	for range 200 {
		_ = s.Manifest()
	}
	close(stop)
	wg.Wait()
}

func TestDiscoverManifest(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifestPath := filepath.Join(root, "a", manifestFilename)
	if err := os.WriteFile(manifestPath, fixtureManifest(t), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	t.Run("walks up to the manifest", func(t *testing.T) {
		got, err := DiscoverManifest(nested)
		if err != nil {
			t.Fatalf("DiscoverManifest(%q) error: %v", nested, err)
		}
		if got != manifestPath {
			t.Errorf("DiscoverManifest = %q, want %q", got, manifestPath)
		}
	})

	t.Run("not found is -32001", func(t *testing.T) {
		empty := t.TempDir()
		_, err := DiscoverManifest(empty)
		var coded *Error
		if !errors.As(err, &coded) || coded.Code != CodeManifestNotFound {
			t.Fatalf("DiscoverManifest(%q) error = %v, want code %d", empty, err, CodeManifestNotFound)
		}
	})
}
