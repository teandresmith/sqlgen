package manifest_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/manifest"
)

// ordersEntity is a second full entity so the lookup map carries more than one
// record.
func ordersEntity() manifest.Entity {
	return manifest.Entity{
		Kind:       manifest.EntityKindTable,
		Name:       "Order",
		Table:      "orders",
		FilePrefix: "orders",
		Files:      []string{"orders_gen.go"},
		PK: manifest.PK{
			Kind:    "single",
			Columns: []manifest.PKColumn{{Name: "id", GoField: "ID", GoType: "int64"}},
		},
		Columns: []manifest.Column{
			{Name: "id", GoField: "ID", GoType: "int64", DBType: "bigint", PK: true, Comment: ""},
			{Name: "user_id", GoField: "UserID", GoType: "uuid.UUID", DBType: "uuid", Comment: ""},
		},
	}
}

// fullDocJSON marshals a single-layout top-level document with full inline
// entities[] — the shape the builder-side emitter writes. It mirrors
// Document's scalar fields but carries []Entity (not []EntityIndex).
func fullDocJSON(t *testing.T, entities []manifest.Entity) []byte {
	t.Helper()
	base := fullDocument()
	aux := struct {
		SchemaVersion    string                    `json:"schema_version"`
		GeneratedAt      string                    `json:"generated_at"`
		Generator        manifest.Generator        `json:"generator"`
		Dialect          manifest.Dialect          `json:"dialect"`
		Package          string                    `json:"package"`
		Layout           manifest.Layout           `json:"layout"`
		Conventions      manifest.Conventions      `json:"conventions"`
		GenerationConfig manifest.GenerationConfig `json:"generation_config"`
		Entities         []manifest.Entity         `json:"entities"`
		Enums            []manifest.Enum           `json:"enums"`
		Extras           []manifest.Extra          `json:"extras"`
	}{
		SchemaVersion:    base.SchemaVersion,
		GeneratedAt:      base.GeneratedAt,
		Generator:        base.Generator,
		Dialect:          base.Dialect,
		Package:          base.Package,
		Layout:           manifest.LayoutSingle,
		Conventions:      base.Conventions,
		GenerationConfig: base.GenerationConfig,
		Entities:         entities,
		Enums:            base.Enums,
		Extras:           base.Extras,
	}
	data, err := json.Marshal(aux)
	if err != nil {
		t.Fatalf("marshal single-layout fixture: %v", err)
	}
	return data
}

// indexDocJSON marshals a per-entity-layout top-level index document (lightweight
// Entities with File pointers).
func indexDocJSON(t *testing.T, index []manifest.EntityIndex) []byte {
	t.Helper()
	doc := fullDocument()
	doc.Layout = manifest.LayoutPerEntity
	doc.Entities = index
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal index fixture: %v", err)
	}
	return data
}

func entityJSON(t *testing.T, e manifest.Entity) []byte {
	t.Helper()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal entity %s: %v", e.Table, err)
	}
	return data
}

// TestLoadInto_SingleLayout resolves full records from the inline entities[]
// while leaving Document.Entities in the lightweight index form.
func TestLoadInto_SingleLayout(t *testing.T) {
	users, orders := fullEntity(), ordersEntity()
	top := fullDocJSON(t, []manifest.Entity{users, orders})

	var doc manifest.Document
	entities := map[string]*manifest.Entity{}
	if err := manifest.LoadInto(top, nil, &doc, entities); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}

	wantIndex := []manifest.EntityIndex{
		{Name: "User", Table: "users", Kind: manifest.EntityKindTable},
		{Name: "Order", Table: "orders", Kind: manifest.EntityKindTable},
	}
	if diff := cmp.Diff(wantIndex, doc.Entities); diff != "" {
		t.Errorf("Document.Entities not lightweight index (-want +got):\n%s", diff)
	}

	if got := entities["users"]; got == nil {
		t.Fatal(`entities["users"] missing`)
	} else if diff := cmp.Diff(users, *got); diff != "" {
		t.Errorf("users full record mismatch (-want +got):\n%s", diff)
	}
	if _, ok := entities["orders"]; !ok {
		t.Error(`entities["orders"] missing`)
	}
}

// TestLoadInto_PerEntityLayout resolves full records from the embedded
// per-entity FS while leaving Document.Entities in the lightweight index form.
func TestLoadInto_PerEntityLayout(t *testing.T) {
	users, orders := fullEntity(), ordersEntity()
	top := indexDocJSON(t, []manifest.EntityIndex{
		{Name: "User", Table: "users", Kind: manifest.EntityKindTable, File: "entities/users.json"},
		{Name: "Order", Table: "orders", Kind: manifest.EntityKindTable, File: "entities/orders.json"},
	})
	entityFS := fstest.MapFS{
		"manifest/entities/users.json":  {Data: entityJSON(t, users)},
		"manifest/entities/orders.json": {Data: entityJSON(t, orders)},
	}

	var doc manifest.Document
	entities := map[string]*manifest.Entity{}
	if err := manifest.LoadInto(top, entityFS, &doc, entities); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}

	if doc.Layout != manifest.LayoutPerEntity {
		t.Errorf("Layout = %q, want per_entity", doc.Layout)
	}
	if got := doc.Entities[0].File; got != "entities/users.json" {
		t.Errorf("index File = %q, want entities/users.json", got)
	}
	if got := entities["orders"]; got == nil {
		t.Fatal(`entities["orders"] missing`)
	} else if diff := cmp.Diff(orders, *got); diff != "" {
		t.Errorf("orders full record mismatch (-want +got):\n%s", diff)
	}
}

// TestLoadInto_SubRootedFS mirrors the real generated init(), which passes
// fs.Sub(sqlgenManifestFS, "manifest/entities") as the per-entity FS. The
// embedded FS also carries the sibling top-level manifest/manifest_gen.json, so
// this test asserts that walking the fs.Sub-rooted "." root ingests ONLY the
// entities/ subtree — the stray top-level JSON must not be decoded into a
// phantom entityMap entry (no entityMap[""], no bogus record).
//
// TestLoadInto_PerEntityLayout passes an un-subbed MapFS keyed
// manifest/entities/*.json, so it never exercises the sub-rooted "." walk nor
// the stray-sibling scoping fs.Sub provides — the fidelity gap this test
// closes.
func TestLoadInto_SubRootedFS(t *testing.T) {
	users, orders := fullEntity(), ordersEntity()
	top := indexDocJSON(t, []manifest.EntityIndex{
		{Name: "User", Table: "users", Kind: manifest.EntityKindTable, File: "entities/users.json"},
		{Name: "Order", Table: "orders", Kind: manifest.EntityKindTable, File: "entities/orders.json"},
	})

	// Mirror the real embed.FS tree: a top-level manifest_gen.json sibling
	// alongside the entities/ subtree, then scope to entities/ exactly as the
	// generated init() does.
	embedded := fstest.MapFS{
		"manifest/manifest_gen.json":    {Data: top},
		"manifest/entities/users.json":  {Data: entityJSON(t, users)},
		"manifest/entities/orders.json": {Data: entityJSON(t, orders)},
	}
	sub, err := fs.Sub(embedded, "manifest/entities")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}

	var doc manifest.Document
	entities := map[string]*manifest.Entity{}
	if err := manifest.LoadInto(top, sub, &doc, entities); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}

	// Only the two real entities load; the stray top-level manifest_gen.json is
	// outside the sub-rooted FS and must not surface as a phantom entry.
	keys := make([]string, 0, len(entities))
	for k := range entities {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if diff := cmp.Diff([]string{"orders", "users"}, keys); diff != "" {
		t.Errorf("entityMap keys (-want +got):\n%s", diff)
	}
	if _, ok := entities[""]; ok {
		t.Error(`entityMap[""] present — a non-entity file was mis-ingested`)
	}
	// A mis-ingested top-level document would decode into an Entity whose Table
	// (absent in the doc) does not match its map key.
	for k, e := range entities {
		if e.Table != k {
			t.Errorf("entityMap[%q] keyed by mismatched entity Table %q", k, e.Table)
		}
	}
}

// TestLoadInto_LayoutAgnostic asserts the same fixture yields deep-equal full
// records whether loaded from single or per-entity layout — the guarantee
// behind the layout-agnostic Client.ManifestEntity lookup (PRD §30.6).
func TestLoadInto_LayoutAgnostic(t *testing.T) {
	users := fullEntity()

	singleTop := fullDocJSON(t, []manifest.Entity{users})
	singleMap := map[string]*manifest.Entity{}
	if err := manifest.LoadInto(singleTop, nil, &manifest.Document{}, singleMap); err != nil {
		t.Fatalf("single LoadInto: %v", err)
	}

	perTop := indexDocJSON(t, []manifest.EntityIndex{{Name: "User", Table: "users", Kind: manifest.EntityKindTable, File: "entities/users.json"}})
	perFS := fstest.MapFS{"manifest/entities/users.json": {Data: entityJSON(t, users)}}
	perMap := map[string]*manifest.Entity{}
	if err := manifest.LoadInto(perTop, perFS, &manifest.Document{}, perMap); err != nil {
		t.Fatalf("per-entity LoadInto: %v", err)
	}

	if diff := cmp.Diff(singleMap["users"], perMap["users"]); diff != "" {
		t.Errorf("layout-agnostic lookup mismatch (-single +per_entity):\n%s", diff)
	}
}

// TestLoadInto_VersionExtraction confirms the schema_version survives into the
// Document — the field the generated ManifestVersion() pre-extracts for the
// drift check against Manifest().SchemaVersion.
func TestLoadInto_VersionExtraction(t *testing.T) {
	top := fullDocJSON(t, []manifest.Entity{fullEntity()})

	var doc manifest.Document
	if err := manifest.LoadInto(top, nil, &doc, map[string]*manifest.Entity{}); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}
	if doc.SchemaVersion != "0.1.0" {
		t.Errorf("SchemaVersion = %q, want 0.1.0", doc.SchemaVersion)
	}
}

// TestLoadInto_ErrorWrapping asserts every parse failure surfaces as
// ErrParseManifest — corrupt top-level bytes and a corrupt per-entity file.
func TestLoadInto_ErrorWrapping(t *testing.T) {
	t.Run("corrupt top-level JSON", func(t *testing.T) {
		err := manifest.LoadInto([]byte("{not json"), nil, &manifest.Document{}, map[string]*manifest.Entity{})
		if !errors.Is(err, manifest.ErrParseManifest) {
			t.Fatalf("err = %v, want ErrParseManifest", err)
		}
	})

	t.Run("corrupt inline entity view", func(t *testing.T) {
		// Valid top-level scalars but entities[] is the wrong JSON type.
		bad := []byte(`{"schema_version":"0.1.0","entities":"nope"}`)
		err := manifest.LoadInto(bad, nil, &manifest.Document{}, map[string]*manifest.Entity{})
		if !errors.Is(err, manifest.ErrParseManifest) {
			t.Fatalf("err = %v, want ErrParseManifest", err)
		}
	})

	t.Run("corrupt per-entity file", func(t *testing.T) {
		top := indexDocJSON(t, []manifest.EntityIndex{{Name: "User", Table: "users", Kind: manifest.EntityKindTable, File: "entities/users.json"}})
		badFS := fstest.MapFS{"manifest/entities/users.json": {Data: []byte("{broken")}}
		err := manifest.LoadInto(top, badFS, &manifest.Document{}, map[string]*manifest.Entity{})
		if !errors.Is(err, manifest.ErrParseManifest) {
			t.Fatalf("err = %v, want ErrParseManifest", err)
		}
	})
}
