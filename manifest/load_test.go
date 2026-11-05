package manifest_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"slices"
	"strings"
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
		{Name: "User", Table: "users", Schema: "public", Kind: manifest.EntityKindTable},
		{Name: "Order", Table: "orders", Kind: manifest.EntityKindTable},
	}
	if diff := cmp.Diff(wantIndex, doc.Entities); diff != "" {
		t.Errorf("Document.Entities not lightweight index (-want +got):\n%s", diff)
	}

	want := map[string]*manifest.Entity{"public.users": &users, "orders": &orders}
	if diff := cmp.Diff(want, entities); diff != "" {
		t.Errorf("entity map (-want +got):\n%s", diff)
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
	want := map[string]*manifest.Entity{"public.users": &users, "orders": &orders}
	if diff := cmp.Diff(want, entities); diff != "" {
		t.Errorf("entity map (-want +got):\n%s", diff)
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
	if diff := cmp.Diff([]string{"orders", "public.users"}, keys); diff != "" {
		t.Errorf("entityMap keys (-want +got):\n%s", diff)
	}
	if _, ok := entities[""]; ok {
		t.Error(`entityMap[""] present — a non-entity file was mis-ingested`)
	}
	// A mis-ingested top-level document would decode into an Entity whose Table
	// (absent in the doc) does not match its map key.
	for k, e := range entities {
		if e.Table != k && e.Schema+"."+e.Table != k {
			t.Errorf("entityMap[%q] keyed by mismatched entity %q.%q", k, e.Schema, e.Table)
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

	if singleMap["public.users"] == nil || perMap["public.users"] == nil {
		t.Fatalf("users record missing: single %v, per_entity %v", singleMap["public.users"], perMap["public.users"])
	}
	if diff := cmp.Diff(singleMap["public.users"], perMap["public.users"]); diff != "" {
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

// auditUsers is a full entity whose table name public.users (fullEntity) shares.
func auditUsers() manifest.Entity {
	return manifest.Entity{
		Kind:       manifest.EntityKindTable,
		Name:       "AuditUser",
		Table:      "users",
		Schema:     "audit",
		FilePrefix: "audit_user",
		Files:      []string{"audit_user_gen.go"},
		PK: manifest.PK{
			Kind:    "single",
			Columns: []manifest.PKColumn{{Name: "id", GoField: "ID", GoType: "int64"}},
		},
		Columns: []manifest.Column{{Name: "id", GoField: "ID", GoType: "int64", DBType: "bigint", PK: true}},
	}
}

// TestLoadInto_KeysSchemasApart loads two schemas' tables of one name in both
// layouts and expects each under its own schema.table key, next to a
// schema-less entity under its bare table.
func TestLoadInto_KeysSchemasApart(t *testing.T) {
	users, audit, orders := fullEntity(), auditUsers(), ordersEntity()
	want := map[string]*manifest.Entity{"public.users": &users, "audit.users": &audit, "orders": &orders}

	t.Run("single", func(t *testing.T) {
		got := map[string]*manifest.Entity{}
		top := fullDocJSON(t, []manifest.Entity{users, audit, orders})
		if err := manifest.LoadInto(top, nil, &manifest.Document{}, got); err != nil {
			t.Fatalf("LoadInto: %v", err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("entity map (-want +got):\n%s", diff)
		}
	})

	t.Run("per_entity", func(t *testing.T) {
		got := map[string]*manifest.Entity{}
		top := indexDocJSON(t, []manifest.EntityIndex{
			{Name: "User", Table: "users", Kind: manifest.EntityKindTable, File: "entities/users.json"},
			{Name: "AuditUser", Table: "users", Kind: manifest.EntityKindTable, File: "entities/audit_user.json"},
			{Name: "Order", Table: "orders", Kind: manifest.EntityKindTable, File: "entities/orders.json"},
		})
		entityFS := fstest.MapFS{
			"users.json":      {Data: entityJSON(t, users)},
			"audit_user.json": {Data: entityJSON(t, audit)},
			"orders.json":     {Data: entityJSON(t, orders)},
		}
		if err := manifest.LoadInto(top, entityFS, &manifest.Document{}, got); err != nil {
			t.Fatalf("LoadInto: %v", err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("entity map (-want +got):\n%s", diff)
		}
	})
}

// TestFindEntity resolves names against a map holding a schema-less entity, a
// table name two schemas share, and a table name only one schema has.
func TestFindEntity(t *testing.T) {
	users, audit, orders := fullEntity(), auditUsers(), ordersEntity()
	events := manifest.Entity{Kind: manifest.EntityKindTable, Name: "Event", Table: "events", Schema: "audit"}
	entities := map[string]*manifest.Entity{
		"public.users": &users,
		"audit.users":  &audit,
		"orders":       &orders,
		"audit.events": &events,
	}

	found := []struct {
		name string
		key  string
		want *manifest.Entity
	}{
		{name: "qualified name", key: "audit.users", want: &audit},
		{name: "the other schema's qualified name", key: "public.users", want: &users},
		{name: "bare table no other schema shares", key: "events", want: &events},
		{name: "schema-less entity", key: "orders", want: &orders},
	}
	for _, tt := range found {
		t.Run(tt.name, func(t *testing.T) {
			got, err := manifest.FindEntity(entities, tt.key)
			if err != nil {
				t.Fatalf("FindEntity(%q): %v", tt.key, err)
			}
			if got != tt.want {
				t.Errorf("FindEntity(%q) = %+v, want the record stored for it", tt.key, got)
			}
		})
	}

	notFound := []struct {
		name string
		key  string
	}{
		{name: "unknown table", key: "invoices"},
		{name: "unknown schema", key: "billing.users"},
		{name: "schema-less entity qualified", key: "public.orders"},
	}
	for _, tt := range notFound {
		t.Run(tt.name, func(t *testing.T) {
			got, err := manifest.FindEntity(entities, tt.key)
			if err != manifest.ErrEntityNotFound { //nolint:errorlint // identity: an unknown name returns the sentinel itself
				t.Errorf("FindEntity(%q) err = %v, want manifest.ErrEntityNotFound itself", tt.key, err)
			}
			if got != nil {
				t.Errorf("FindEntity(%q) = %+v, want nil", tt.key, got)
			}
		})
	}

	t.Run("bare table two schemas share", func(t *testing.T) {
		got, err := manifest.FindEntity(entities, "users")
		if got != nil {
			t.Errorf("FindEntity(users) = %+v, want nil", got)
		}
		if !errors.Is(err, manifest.ErrEntityNotFound) {
			t.Fatalf("FindEntity(users) err = %v, want an error wrapping manifest.ErrEntityNotFound", err)
		}
		for _, candidate := range []string{"audit.users", "public.users"} {
			if !strings.Contains(err.Error(), candidate) {
				t.Errorf("FindEntity(users) err = %q, want it to name %s", err, candidate)
			}
		}
	})
}

// TestSentinelErrors pins the two sentinels' text.
func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "entity not found", err: manifest.ErrEntityNotFound, want: "manifest: entity not found"},
		{name: "parse failure", err: manifest.ErrParseManifest, want: "manifest: parse failure (corrupt embed)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}
