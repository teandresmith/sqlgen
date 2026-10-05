package manifest_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
)

var update = flag.Bool("update", false, "update golden files")

// newFixtureDocument returns a deterministic, representative manifest document
// exercising every top-level section plus the PRD §30.7 extended-metadata
// fields (comments, check, default_kind, indexes, sql_bodies). generated_at is
// fixed so emission is byte-stable. The "users" table carries HTML-special
// characters in a column comment to pin the escaping behavior.
func newFixtureDocument() *manifest.Document {
	return &manifest.Document{
		Schema:        "https://raw.githubusercontent.com/teandresmith/sqlgen/v0.42.0/cmd/sqlgen/manifest/schema/v1.json",
		SchemaVersion: manifest.SchemaVersion,
		GeneratedAt:   "2026-05-15T14:22:11Z",
		Generator:     manifest.Generator{Name: "sqlgen", Version: "0.42.0"},
		Dialect:       "postgres",
		Package:       "models",
		Layout:        string(config.JSONLayoutSingle),
		Conventions: manifest.Conventions{
			ClientEntryPoints: manifest.ClientEntryPoints{
				Query:    "client.<Entity>()",
				Mutation: "client.<Entity>()",
			},
			ErrorSentinels: []manifest.ErrorSentinel{
				{Name: "ErrNotFound", GraphQLCode: "NOT_FOUND", Package: "models"},
				{Name: "ErrConstraintViolation", Package: "models"},
				{Name: "ErrMissing", GraphQLCode: "UNAUTHENTICATED", Package: "tenancy"},
			},
			ConstraintErrorCodes: map[string]string{
				"unique":      "CONFLICT",
				"foreign_key": "BAD_REFERENCE",
				"check":       "INVALID_INPUT",
				"not_null":    "INVALID_INPUT",
			},
			FindReturnsNilOnMissing: true,
			Pagination: manifest.PaginationConvention{
				PageType:           "Page",
				ListEnvelopeSuffix: "List",
				CursorEncoding:     "opaque-base64",
			},
			CallOptions: manifest.CallOptionsConvention{
				Type:   "CallOption",
				Fields: []string{"WithTx", "IncludeSoftDeleted"},
			},
			SoftDelete: manifest.SoftDeleteConvention{
				DefaultExcludedFromFinds: true,
				IncludeVia:               "WithSoftDeleted()",
				HardDeleteMethodSuffix:   "HardDelete",
				RestoreMethodSuffix:      "Restore",
			},
			Comparator: manifest.ComparatorConvention{
				Package:     "comparator",
				Families:    []string{"Bool", "Enum", "ID", "JSON", "JSONB", "Number", "Slice", "String", "Time"},
				ShapeNotes:  []string{"Every family exposes Eq/Neq; ordered families add Gt/Gte/Lt/Lte."},
				Composition: map[string]string{"and": "all conditions must match", "or": "any condition matches"},
				Examples:    map[string]string{"eq": "comparator.String.Eq(\"alice\")"},
			},
			Omittable: manifest.OmittableConvention{
				Package:      "omittable",
				Type:         "omittable.Value[T]",
				Purpose:      "Distinguishes 'not set' from 'set to zero value'.",
				Construction: []string{"omittable.Set(value)", "omittable.Omit[T]()"},
				Methods:      []string{"IsSet() bool", "IsZero() bool", "Get() (T, bool)", "MustGet() T"},
				JSONBehavior: "Marshals to the wrapped value when set; omitted when unset.",
			},
		},
		GenerationConfig: manifest.GenerationConfig{
			AuditColumns:  true,
			Cache:         true,
			Events:        true,
			GraphQL:       false,
			GraphTopLevel: false,
			SoftDelete:    false,
			Tenancy:       false,
			Views:         true,
		},
		Entities: []manifest.Entity{
			usersEntity(),
			activeUsersView(),
		},
		Enums: []manifest.Enum{
			{Name: "UserStatus", GoType: "UserStatus", DBType: "user_status", Values: []string{"active", "pending", "closed"}},
		},
		Extras: []manifest.Extra{
			{
				Kind:   "composite",
				Name:   "Address",
				GoType: "Address",
				Fields: []manifest.ExtraField{
					{Name: "Street", GoType: "string", JSON: "street"},
					{Name: "City", GoType: "string", JSON: "city"},
				},
			},
		},
	}
}

func usersEntity() manifest.Entity {
	return manifest.Entity{
		Kind:       "table",
		Name:       "User",
		Table:      "users",
		FilePrefix: "users",
		Files:      []string{"users_gen.go"},
		Comment:    "Application users. Soft-deleted on account closure.",
		Indexes: []manifest.Index{
			{Name: "users_pkey", Columns: []string{"id"}, Unique: true, Method: "btree"},
			{Name: "users_email_key", Columns: []string{"email"}, Unique: true, Method: "btree"},
			{Name: "users_active_idx", Columns: []string{"id"}, Unique: false, Method: "btree", Where: "deleted_at IS NULL"},
		},
		PK: manifest.PK{
			Kind:    "single",
			Columns: []manifest.PKColumn{{Name: "id", GoField: "ID", GoType: "uuid.UUID"}},
		},
		Features: manifest.Features{
			Cache: &manifest.CacheFeature{
				TTLSeconds:    30,
				Hydration:     "lazy",
				KeyPattern:    "models:user:{id}",
				InvalidatesOn: []string{"Create", "Update", "Delete"},
			},
			Events: &manifest.EventsFeature{
				Enabled:      true,
				Types:        []string{"created", "updated", "deleted"},
				PayloadShape: "models.UserEvent",
			},
			AuditColumns: []string{"created_at", "updated_at"},
		},
		Columns: []manifest.Column{
			{
				Name: "id", GoField: "ID", GoType: "uuid.UUID", DBType: "uuid",
				Nullable: false, PK: true, Unique: true,
				Default: "gen_random_uuid()", DefaultKind: "function",
				Comparator: "comparator.ID",
				Comment:    "Primary key. Server-generated UUIDv7.",
			},
			{
				Name: "email", GoField: "Email", GoType: "string", DBType: "text",
				Nullable: false, PK: false, Unique: true,
				Comparator: "comparator.String",
				// HTML-special characters pin the escaping behavior (PRD §5.7).
				Comment: "Login identity. Rendered raw in <table> & lists.",
				Check:   "email <> ''",
			},
			{
				Name: "status", GoField: "Status", GoType: "UserStatus", DBType: "user_status",
				Nullable: false, PK: false, Unique: false,
				Default: "'pending'", DefaultKind: "literal",
				Comparator: "comparator.Enum[UserStatus]",
				Comment:    "",
			},
		},
		Relationships: []manifest.Relationship{
			{
				Name:         "Posts",
				Kind:         "o2m",
				TargetEntity: "Post",
				FK:           &manifest.FK{Table: "posts", Column: "author_id"},
			},
		},
		Methods: manifest.Methods{
			Query: []manifest.Method{
				{
					Name:      "Get",
					Params:    []manifest.MethodParam{{Name: "id", Type: "uuid.UUID"}},
					Returns:   "*User",
					Errors:    []string{"ErrNotFound"},
					Notes:     "Delegates to GetMany with a primary-key filter.",
					SQLBodies: map[string]string{"postgres": "SELECT * FROM \"users\" WHERE \"id\" = $1 AND \"deleted_at\" IS NULL LIMIT 1"},
				},
			},
			Mutation: []manifest.Method{
				{
					Name:      "Create",
					Params:    []manifest.MethodParam{{Name: "input", Type: "*CreateUserInput"}},
					Returns:   "*User",
					Errors:    []string{"ErrConstraintViolation", "tenancy.ErrMissing", "tenancy.ErrMismatch"},
					SQLBodies: map[string]string{"postgres": "INSERT INTO \"users\" (<columns>) VALUES (<values>) RETURNING \"id\""},
				},
				{
					// A void-return entry, so the emitters and the MCP
					// signature renderer are exercised on the shape that has
					// no result but does return error.
					Name:      "HardDelete",
					Params:    []manifest.MethodParam{{Name: "id", Type: "uuid.UUID"}},
					Returns:   "",
					Errors:    []string{"tenancy.ErrMissing"},
					Notes:     "Idempotent — a primary key that does not exist is not an error (§9.5).",
					SQLBodies: map[string]string{"postgres": "DELETE FROM \"users\" WHERE \"id\" = $1"},
				},
			},
		},
		Filter: manifest.Filter{
			Type: "UserFilter",
			Fields: []manifest.FilterField{
				{Name: "ID", Type: "comparator.ID"},
				{Name: "Email", Type: "comparator.String"},
			},
		},
		Sort: manifest.Sort{
			Type:   "UserSort",
			Fields: []string{"ID", "Email", "CreatedAt"},
		},
	}
}

func activeUsersView() manifest.Entity {
	return manifest.Entity{
		Kind:       "view",
		Name:       "ActiveUser",
		Table:      "active_users",
		Source:     "views/active_users.sql",
		FilePrefix: "active_users",
		Files:      []string{"active_users_gen.go"},
		Comment:    "Non-deleted users.",
		Indexes:    []manifest.Index{},
		PK:         manifest.PK{Kind: "none"},
		Features:   manifest.Features{},
		Columns: []manifest.Column{
			{
				Name: "id", GoField: "ID", GoType: "uuid.UUID", DBType: "uuid",
				Nullable: false, PK: false, Unique: false,
				Comparator: "comparator.ID", Comment: "",
			},
		},
		Relationships: []manifest.Relationship{},
		Methods: manifest.Methods{
			Query:    []manifest.Method{},
			Mutation: []manifest.Method{},
		},
		Filter: manifest.Filter{Type: "ActiveUserFilter", Fields: []manifest.FilterField{}},
		Sort:   manifest.Sort{Type: "ActiveUserSort", Fields: []string{}},
	}
}

func manifestCfg(layout config.JSONLayout) *config.ManifestConfig {
	return &config.ManifestConfig{
		JSONFilename:     "manifest_gen.json",
		JSONLayout:       layout,
		JSONPerEntityDir: "entities",
		MarkdownDir:      "manifest",
	}
}

func checkGolden(t *testing.T, goldenPath string, got []byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o750); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o600); err != nil {
			t.Fatalf("write golden %s: %v", goldenPath, err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath) //nolint:gosec // test helper reads a controlled testdata/golden path
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create): %v", goldenPath, err)
	}
	if diff := cmp.Diff(string(want), string(got)); diff != "" {
		t.Errorf("golden %s mismatch (-want +got):\n%s", goldenPath, diff)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test helper reads a controlled t.TempDir / testdata path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func TestEmitJSON_SingleLayout(t *testing.T) {
	doc := newFixtureDocument()
	dir := t.TempDir()

	if err := manifest.EmitJSON(doc, manifestCfg(config.JSONLayoutSingle), dir); err != nil {
		t.Fatalf("EmitJSON(single) error: %v", err)
	}

	got := readFile(t, filepath.Join(dir, "manifest", "manifest_gen.json"))
	checkGolden(t, filepath.Join("testdata", "golden", "single", "manifest_gen.json"), got)
}

func TestEmitJSON_PerEntityLayout(t *testing.T) {
	doc := newFixtureDocument()
	doc.Layout = string(config.JSONLayoutPerEntity)
	dir := t.TempDir()

	if err := manifest.EmitJSON(doc, manifestCfg(config.JSONLayoutPerEntity), dir); err != nil {
		t.Fatalf("EmitJSON(per_entity) error: %v", err)
	}

	index := readFile(t, filepath.Join(dir, "manifest", "manifest_gen.json"))
	checkGolden(t, filepath.Join("testdata", "golden", "per_entity", "manifest_gen.json"), index)

	for _, table := range []string{"users", "active_users"} {
		got := readFile(t, filepath.Join(dir, "manifest", "entities", table+".json"))
		checkGolden(t, filepath.Join("testdata", "golden", "per_entity", "entities", table+".json"), got)
	}
}

func TestEmitJSON_Deterministic(t *testing.T) {
	for _, layout := range []config.JSONLayout{config.JSONLayoutSingle, config.JSONLayoutPerEntity} {
		t.Run(string(layout), func(t *testing.T) {
			doc := newFixtureDocument()
			doc.Layout = string(layout)

			dir1, dir2 := t.TempDir(), t.TempDir()
			if err := manifest.EmitJSON(doc, manifestCfg(layout), dir1); err != nil {
				t.Fatalf("EmitJSON first pass: %v", err)
			}
			if err := manifest.EmitJSON(doc, manifestCfg(layout), dir2); err != nil {
				t.Fatalf("EmitJSON second pass: %v", err)
			}

			a := readFile(t, filepath.Join(dir1, "manifest", "manifest_gen.json"))
			b := readFile(t, filepath.Join(dir2, "manifest", "manifest_gen.json"))
			if !bytes.Equal(a, b) {
				t.Errorf("EmitJSON(%s) not byte-deterministic across runs", layout)
			}
		})
	}
}

func TestEmitJSON_HTMLEscapingDisabled(t *testing.T) {
	doc := newFixtureDocument()
	dir := t.TempDir()
	if err := manifest.EmitJSON(doc, manifestCfg(config.JSONLayoutSingle), dir); err != nil {
		t.Fatalf("EmitJSON error: %v", err)
	}

	got := readFile(t, filepath.Join(dir, "manifest", "manifest_gen.json"))
	// The raw HTML-special characters must survive verbatim...
	if !bytes.Contains(got, []byte("Rendered raw in <table> & lists.")) {
		t.Errorf("EmitJSON did not round-trip HTML-special characters unescaped; output missing raw comment")
	}
	// ...and their JSON unicode-escaped forms (what SetEscapeHTML(true) would
	// emit) must not appear anywhere in the output. "\\u003c" is the literal
	// six-byte escape sequence for '<'.
	for _, esc := range []string{"\\u003c", "\\u003e", "\\u0026"} {
		if bytes.Contains(got, []byte(esc)) {
			t.Errorf("EmitJSON emitted HTML-escaped sequence %q; escaping must be disabled per PRD §5.7", esc)
		}
	}
}

// --- schema validation ---

func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	data := readFile(t, filepath.Join("schema", "v1.json"))
	sch, err := jsonschema.CompileString("v1.json", string(data))
	if err != nil {
		t.Fatalf("compile schema/v1.json: %v", err)
	}
	return sch
}

func decodeJSON(t *testing.T, data []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("unmarshal manifest json: %v", err)
	}
	return v
}

func TestEmitJSON_ValidatesAgainstSchema(t *testing.T) {
	sch := compileSchema(t)

	t.Run("single", func(t *testing.T) {
		doc := newFixtureDocument()
		dir := t.TempDir()
		if err := manifest.EmitJSON(doc, manifestCfg(config.JSONLayoutSingle), dir); err != nil {
			t.Fatalf("EmitJSON: %v", err)
		}
		got := readFile(t, filepath.Join(dir, "manifest", "manifest_gen.json"))
		if err := sch.Validate(decodeJSON(t, got)); err != nil {
			t.Errorf("single-layout manifest failed schema validation: %v", err)
		}
	})

	t.Run("per_entity", func(t *testing.T) {
		doc := newFixtureDocument()
		doc.Layout = string(config.JSONLayoutPerEntity)
		dir := t.TempDir()
		if err := manifest.EmitJSON(doc, manifestCfg(config.JSONLayoutPerEntity), dir); err != nil {
			t.Fatalf("EmitJSON: %v", err)
		}
		index := readFile(t, filepath.Join(dir, "manifest", "manifest_gen.json"))
		if err := sch.Validate(decodeJSON(t, index)); err != nil {
			t.Errorf("per-entity index manifest failed schema validation: %v", err)
		}
		for _, table := range []string{"users", "active_users"} {
			body := readFile(t, filepath.Join(dir, "manifest", "entities", table+".json"))
			if err := sch.Validate(decodeJSON(t, body)); err != nil {
				t.Errorf("per-entity file %s.json failed schema validation: %v", table, err)
			}
		}
	})
}

func TestEmitJSON_SchemaRejectsMissingGenerationConfig(t *testing.T) {
	sch := compileSchema(t)

	doc := newFixtureDocument()
	dir := t.TempDir()
	if err := manifest.EmitJSON(doc, manifestCfg(config.JSONLayoutSingle), dir); err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	got := readFile(t, filepath.Join(dir, "manifest", "manifest_gen.json"))

	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	delete(m, "generation_config")

	if err := sch.Validate(m); err == nil {
		t.Error("schema accepted a manifest missing generation_config; want validation failure")
	}
}

func TestEmitJSON_SchemaToleratesUnknownTopLevelField(t *testing.T) {
	sch := compileSchema(t)

	doc := newFixtureDocument()
	dir := t.TempDir()
	if err := manifest.EmitJSON(doc, manifestCfg(config.JSONLayoutSingle), dir); err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	got := readFile(t, filepath.Join(dir, "manifest", "manifest_gen.json"))

	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	m["future_top_level_field"] = true

	if err := sch.Validate(m); err != nil {
		t.Errorf("schema rejected a manifest with an unknown top-level field; want tolerant: %v", err)
	}
}

// TestEmitJSON_IndexEnvelopeMatchesDocumentTopLevel pins the per-entity index
// envelope's top-level field set to the full document's. indexDocument
// (emit_json.go) hand-mirrors Document, so a new top-level Document field could
// silently drop out of the per-entity index without this guard. Both layouts
// carry an "entities" key (full array vs lightweight pointers), so the key sets
// must be identical.
func TestEmitJSON_IndexEnvelopeMatchesDocumentTopLevel(t *testing.T) {
	single := t.TempDir()
	if err := manifest.EmitJSON(newFixtureDocument(), manifestCfg(config.JSONLayoutSingle), single); err != nil {
		t.Fatalf("EmitJSON(single): %v", err)
	}

	perDoc := newFixtureDocument()
	perDoc.Layout = string(config.JSONLayoutPerEntity)
	per := t.TempDir()
	if err := manifest.EmitJSON(perDoc, manifestCfg(config.JSONLayoutPerEntity), per); err != nil {
		t.Fatalf("EmitJSON(per_entity): %v", err)
	}

	singleKeys := topLevelKeys(t, readFile(t, filepath.Join(single, "manifest", "manifest_gen.json")))
	indexKeys := topLevelKeys(t, readFile(t, filepath.Join(per, "manifest", "manifest_gen.json")))

	if diff := cmp.Diff(singleKeys, indexKeys); diff != "" {
		t.Errorf("per-entity index top-level fields drifted from the full-document envelope (-single +index):\n%s\n"+
			"indexDocument (emit_json.go) hand-mirrors Document — add the missing field there.", diff)
	}
}

func topLevelKeys(t *testing.T, data []byte) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal top-level object: %v", err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
