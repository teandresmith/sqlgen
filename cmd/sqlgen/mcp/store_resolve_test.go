package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/teandresmith/sqlgen/manifest"
)

// TestResolveEntitiesPerEntity covers the per_entity branch of resolveEntities,
// which reads full records from the sibling entities/ files named by the index.
// The unit-level fixtures elsewhere are single-layout; the per_entity path is
// otherwise only exercised by the graphql example's E2E leg.
func TestResolveEntitiesPerEntity(t *testing.T) {
	dir := t.TempDir()
	entDir := filepath.Join(dir, "entities")
	if err := os.MkdirAll(entDir, 0o750); err != nil {
		t.Fatal(err)
	}
	// A full entity record lives in its own file; the top-level document only
	// carries the lightweight index pointing at it.
	if err := os.WriteFile(filepath.Join(entDir, "user.json"),
		[]byte(`{"kind":"table","name":"User","table":"users","pk":{"kind":"single"},"columns":[{"name":"id","go_field":"ID","go_type":"string","db_type":"uuid"}]}`),
		0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "manifest_gen.json")
	doc := &manifest.Document{
		Layout: manifest.LayoutPerEntity,
		Entities: []manifest.EntityIndex{
			{Name: "User", Table: "users", Kind: manifest.EntityKindTable, File: "entities/user.json"},
		},
	}

	t.Run("resolves full records from sibling files", func(t *testing.T) {
		got, err := resolveEntities(manifestPath, nil, doc)
		if err != nil {
			t.Fatalf("resolveEntities: %v", err)
		}
		if len(got) != 1 || got[0].Name != "User" || got[0].Table != "users" || len(got[0].Columns) != 1 {
			t.Fatalf("expected the full User record, got %+v", got)
		}
	})

	t.Run("missing file reference is a coded error", func(t *testing.T) {
		bad := &manifest.Document{
			Layout:   manifest.LayoutPerEntity,
			Entities: []manifest.EntityIndex{{Name: "Orphan", Table: "orphans", Kind: manifest.EntityKindTable}},
		}
		_, err := resolveEntities(manifestPath, nil, bad)
		if err == nil {
			t.Fatal("expected an error for a missing file reference")
		}
		var e *Error
		if !asStoreError(t, err, &e) || e.Code != CodeManifestInvalid {
			t.Errorf("want CodeManifestInvalid, got %v", err)
		}
	})

	t.Run("unreadable entity file is a coded error", func(t *testing.T) {
		gone := &manifest.Document{
			Layout:   manifest.LayoutPerEntity,
			Entities: []manifest.EntityIndex{{Name: "Ghost", Table: "ghosts", Kind: manifest.EntityKindTable, File: "entities/ghost.json"}},
		}
		_, err := resolveEntities(manifestPath, nil, gone)
		var e *Error
		if err == nil || !asStoreError(t, err, &e) || e.Code != CodeManifestInvalid {
			t.Errorf("want CodeManifestInvalid for a missing entity file, got %v", err)
		}
	})
}

func asStoreError(t *testing.T, err error, target **Error) bool {
	t.Helper()
	return errors.As(err, target)
}
