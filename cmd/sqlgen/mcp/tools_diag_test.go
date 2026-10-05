package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestHealth(t *testing.T) {
	t.Run("fresh load", func(t *testing.T) {
		d := fixtureDeps()
		got, err := health(d, HealthInput{})
		if err != nil {
			t.Fatalf("health: %v", err)
		}
		want := HealthOutput{
			ManifestPath:  "/fixture/manifest/manifest_gen.json",
			SchemaVersion: "0.1.0",
			GeneratedAt:   "2026-05-15T14:22:11Z",
			LoadedAt:      "2026-05-15T14:22:11Z",
			WatchEnabled:  true,
			SqlgenVersion: "9.9.9",
			OK:            true,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("health mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("failed reload flips ok and sets last_reload_at", func(t *testing.T) {
		s := newFixtureStore()
		s.ok = false
		s.lastReloadAt = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		d := &toolDeps{store: s, version: "9.9.9", watchEnabled: true}
		got, err := health(d, HealthInput{})
		if err != nil {
			t.Fatalf("health: %v", err)
		}
		if got.OK {
			t.Errorf("expected ok=false after a failed reload")
		}
		if got.LastReloadAt != "2026-06-01T00:00:00Z" {
			t.Errorf("last_reload_at = %q, want 2026-06-01T00:00:00Z", got.LastReloadAt)
		}
	})

	t.Run("watch off is reported", func(t *testing.T) {
		d := &toolDeps{store: newFixtureStore(), version: "9.9.9", watchEnabled: false}
		got, err := health(d, HealthInput{})
		if err != nil {
			t.Fatalf("health: %v", err)
		}
		if got.WatchEnabled {
			t.Errorf("expected watch_enabled=false")
		}
		if got.LastReloadAt != "" {
			t.Errorf("last_reload_at should be omitted before the first reload, got %q", got.LastReloadAt)
		}
	})
}

// loadRealFixture loads the committed manifest fixture through the real store
// path so Raw() holds a schema-valid document.
func loadRealFixture(t *testing.T) *Store {
	t.Helper()
	s := NewStore("testdata/manifest_gen.json", nil)
	if err := s.Load(); err != nil {
		t.Fatalf("load fixture manifest: %v", err)
	}
	return s
}

func TestValidateManifest(t *testing.T) {
	t.Run("in-memory manifest is valid", func(t *testing.T) {
		d := &toolDeps{store: loadRealFixture(t)}
		got, err := validateManifest(d, ValidateManifestInput{})
		if err != nil {
			t.Fatalf("validateManifest: %v", err)
		}
		if !got.Valid || got.SchemaVersion != "0.1.0" || len(got.Errors) != 0 {
			t.Errorf("expected valid=true, schema_version=0.1.0, no errors; got %+v", got)
		}
	})

	t.Run("path mode validates without swapping the loaded manifest", func(t *testing.T) {
		store := loadRealFixture(t)
		before := store.Raw()
		d := &toolDeps{store: store}
		got, err := validateManifest(d, ValidateManifestInput{Path: "testdata/manifest_gen.json"})
		if err != nil {
			t.Fatalf("validateManifest: %v", err)
		}
		if !got.Valid {
			t.Errorf("expected the on-disk fixture to validate, got %+v", got)
		}
		if &store.Raw()[0] != &before[0] {
			t.Errorf("path-mode validation must not swap the loaded manifest")
		}
	})

	t.Run("invalid manifest reports flattened errors", func(t *testing.T) {
		dir := t.TempDir()
		bad := filepath.Join(dir, "bad.json")
		// Structurally invalid: schema_version present but required fields missing.
		if err := os.WriteFile(bad, []byte(`{"schema_version":"0.1.0"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		d := &toolDeps{store: loadRealFixture(t)}
		got, err := validateManifest(d, ValidateManifestInput{Path: bad})
		if err != nil {
			t.Fatalf("validateManifest: %v", err)
		}
		if got.Valid {
			t.Errorf("expected valid=false for a structurally invalid manifest")
		}
		if len(got.Errors) == 0 {
			t.Errorf("expected at least one validation issue, got none")
		}
		if got.SchemaVersion != "0.1.0" {
			t.Errorf("schema_version probe = %q, want 0.1.0", got.SchemaVersion)
		}
	})

	t.Run("unreadable path yields an internal error", func(t *testing.T) {
		d := &toolDeps{store: loadRealFixture(t)}
		_, err := validateManifest(d, ValidateManifestInput{Path: filepath.Join(t.TempDir(), "does-not-exist.json")})
		assertCode(t, err, CodeInternal)
	})
}
