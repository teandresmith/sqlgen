package tests

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/manifest"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// E2E manifest surface. These tests exercise the runtime-facing
// manifest guarantees against the generated Client and the on-disk artifact.
//
// Schema-validation and `sqlgen manifest validate` CLI coverage live in the
// cmd/sqlgen module (cli/manifest_validate_e2e_test.go), where the embedded
// schema/v1.json and the jsonschema library the CLI uses are importable — this
// example module depends only on the root module and cannot reach either.
const (
	manifestWantDialect = "mysql"
	manifestWantLayout  = "single"
	manifestJSONPath    = "../models/manifest/manifest_gen.json"
)

// readOnDiskManifest decodes the emitted manifest_gen.json. The top-level
// entities[] decode to the lightweight index form under both layouts.
func readOnDiskManifest(t *testing.T) manifest.Document {
	t.Helper()
	raw, err := os.ReadFile(manifestJSONPath)
	if err != nil {
		t.Fatalf("reading on-disk manifest: %v", err)
	}
	var doc manifest.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decoding on-disk manifest: %v", err)
	}
	return doc
}

// entityKeys projects the entity index to sorted "name|table|kind" keys so two
// index views can be compared independent of slice ordering.
func entityKeys(entities []manifest.EntityIndex) []string {
	keys := make([]string, 0, len(entities))
	for _, e := range entities {
		keys = append(keys, e.Name+"|"+e.Table+"|"+string(e.Kind))
	}
	slices.Sort(keys)
	return keys
}

// TestManifest_OnDiskShape verifies the emitted manifest_gen.json carries the
// PRD §30.4 top-level fields and that its entity count agrees with the embedded
// Client view.
func TestManifest_OnDiskShape(t *testing.T) {
	doc := readOnDiskManifest(t)

	if doc.SchemaVersion == "" {
		t.Error("schema_version is empty")
	}
	if doc.GeneratedAt == "" {
		t.Error("generated_at is empty")
	}
	if got := string(doc.Dialect); got != manifestWantDialect {
		t.Errorf("dialect = %q, want %q", got, manifestWantDialect)
	}
	if doc.Package != "models" {
		t.Errorf("package = %q, want %q", doc.Package, "models")
	}
	if got := string(doc.Layout); got != manifestWantLayout {
		t.Errorf("layout = %q, want %q", got, manifestWantLayout)
	}
	if len(doc.Entities) == 0 {
		t.Fatal("manifest has no entities")
	}

	client := &models.Client{}
	if got, want := len(client.Manifest().Entities), len(doc.Entities); got != want {
		t.Errorf("client entity count = %d, on-disk = %d", got, want)
	}
}

// TestManifest_ClientRoundTrip asserts the embedded Client manifest matches the
// on-disk artifact (envelope + entity index).
func TestManifest_ClientRoundTrip(t *testing.T) {
	disk := readOnDiskManifest(t)
	doc := (&models.Client{}).Manifest()

	if doc.Package != disk.Package {
		t.Errorf("package: client %q, disk %q", doc.Package, disk.Package)
	}
	if doc.Layout != disk.Layout {
		t.Errorf("layout: client %q, disk %q", doc.Layout, disk.Layout)
	}
	if doc.SchemaVersion != disk.SchemaVersion {
		t.Errorf("schema_version: client %q, disk %q", doc.SchemaVersion, disk.SchemaVersion)
	}
	if doc.Dialect != disk.Dialect {
		t.Errorf("dialect: client %q, disk %q", doc.Dialect, disk.Dialect)
	}
	if got, want := entityKeys(doc.Entities), entityKeys(disk.Entities); !slices.Equal(got, want) {
		t.Errorf("entity index mismatch:\n client %v\n disk   %v", got, want)
	}
}

// TestManifest_EntityLookup asserts ManifestEntity returns the full entity
// record (identically across layouts) and ErrEntityNotFound for unknown tables.
func TestManifest_EntityLookup(t *testing.T) {
	client := &models.Client{}

	e, err := client.ManifestEntity("users")
	if err != nil {
		t.Fatalf("ManifestEntity(users): %v", err)
	}
	if e.Table != "users" {
		t.Errorf("entity table = %q, want %q", e.Table, "users")
	}
	if len(e.Columns) == 0 {
		t.Error("users entity has no columns; expected the full record, not the index form")
	}

	if _, err := client.ManifestEntity("does_not_exist"); !errors.Is(err, manifest.ErrEntityNotFound) {
		t.Errorf("ManifestEntity(unknown) error = %v, want ErrEntityNotFound", err)
	}
}

// TestManifest_VersionAgreement asserts ManifestVersion() equals the document's
// schema_version and is non-empty.
func TestManifest_VersionAgreement(t *testing.T) {
	client := &models.Client{}

	if client.ManifestVersion() == "" {
		t.Fatal("ManifestVersion() is empty")
	}
	if got, want := client.ManifestVersion(), client.Manifest().SchemaVersion; got != want {
		t.Errorf("ManifestVersion() = %q, Manifest().SchemaVersion = %q", got, want)
	}
}
