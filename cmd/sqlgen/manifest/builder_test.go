package manifest_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/parser"
)

// --- helpers ---

// baseConfig produces a manifest-enabled postgres RootConfig with the defaults
// the Build path expects. Individual tests mutate the returned cfg before
// passing it into testBuildInput.
func baseConfig() *config.RootConfig {
	enabled := true
	cfg := &config.RootConfig{}
	cfg.Input.Dialect = config.DialectPostgres
	cfg.Output.Driver = config.DriverPgx
	cfg.Output.Package = "models"
	cfg.Output.Client = &config.ClientOutputConfig{Name: "Client", File: "client_gen.go"}
	cfg.Generation.QueryLimit = new(1000)
	cfg.Generation.BatchSize = new(200)
	cfg.Generation.PageSize = new(100)
	cfg.Generation.CursorKeys = []string{"id"}
	cfg.Generation.UUIDVersion = config.UUIDVersionV4
	cfg.Generation.StrictUpdates = new(true)
	cfg.Generation.UpdateColumns = []string{"updated_at"}
	cfg.Generation.SoftDeleteColumns = []config.SoftDeleteConfig{{Name: "deleted_at", Type: config.SoftDeleteTimestamp}}
	cfg.Generation.Manifest = &config.ManifestConfig{Enabled: &enabled}
	cfg.Tables = make(map[string]config.TableConfig)
	cfg.Views = make(map[string]config.ViewConfig)
	cfg.Extras = make(map[string]config.ExtraType)
	cfg.Overrides.Types = make(map[string]config.TypeOverride)
	cfg.Overrides.UsePointers = new(true)
	return cfg
}

// testBuildInput is the standard manifest.BuildInput factory used by tests.
// It builds table + view + enum contexts via the public gen helpers so the
// manifest builder runs against the same data the codegen pipeline does.
//
// Enums have to come through the same door as tables: buildEnums projects
// EnumContext rather than re-deriving names from the parser schema, so a
// factory that left Enums unset would build every test document with `enums:
// []` and quietly stop covering it.
func testBuildInput(t *testing.T, schema *parser.Schema, cfg *config.RootConfig) manifest.BuildInput {
	t.Helper()

	resolver := gotype.NewResolver(cfg.Input.Dialect, true, cfg.Overrides.Types)
	in := &gen.GenerateInput{Schema: schema, Config: cfg, Resolver: resolver}
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	views, err := gen.BuildViewContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildViewContexts: %v", err)
	}
	return manifest.BuildInput{
		Schema:    schema,
		Config:    cfg,
		Tables:    tables,
		Views:     views,
		Enums:     gen.BuildEnumContexts(schema, nil, cfg),
		Version:   "0.42.0",
		Timestamp: "2026-05-16T00:00:00Z",
	}
}

func findEntity(t *testing.T, doc *manifest.Document, name string) manifest.Entity {
	t.Helper()
	for _, e := range doc.Entities {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("entity %q not found in manifest; entities=%v", name, entityNames(doc))
	return manifest.Entity{}
}

func entityNames(doc *manifest.Document) []string {
	names := make([]string, len(doc.Entities))
	for i, e := range doc.Entities {
		names[i] = e.Name
	}
	return names
}

// --- entity-shape tests ---

func TestBuild_SinglePKTable(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:    "products",
				Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "name", Type: "text"}},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "Product")
	if e.Kind != "table" {
		t.Errorf("kind = %q, want %q", e.Kind, "table")
	}
	if e.PK.Kind != "single" {
		t.Errorf("pk.kind = %q, want %q", e.PK.Kind, "single")
	}
	if len(e.PK.Columns) != 1 || e.PK.Columns[0].Name != "id" {
		t.Errorf("pk.columns = %+v, want [{id}]", e.PK.Columns)
	}
}

func TestBuild_CompositePKTable(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "workspace_settings",
				Columns: []parser.Column{
					{Name: "workspace_id", Type: "uuid", PrimaryKey: true},
					{Name: "key", Type: "text", PrimaryKey: true},
					{Name: "value", Type: "text"},
				},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "WorkspaceSetting")
	if e.PK.Kind != "composite" {
		t.Errorf("pk.kind = %q, want composite", e.PK.Kind)
	}
	if e.PK.Struct != "WorkspaceSettingPK" {
		t.Errorf("pk.struct = %q, want WorkspaceSettingPK", e.PK.Struct)
	}
	if len(e.PK.Columns) != 2 {
		t.Errorf("pk.columns len = %d, want 2", len(e.PK.Columns))
	}
}

func TestBuild_TenantedTable(t *testing.T) {
	cfg := baseConfig()
	cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id"}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "documents",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "workspace_id", Type: "uuid", Nullable: false},
					{Name: "title", Type: "text"},
				},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "Document")
	if e.Features.Tenancy == nil {
		t.Fatalf("features.tenancy = nil, want populated")
	}
	if e.Features.Tenancy.Column != "workspace_id" {
		t.Errorf("tenancy.column = %q, want workspace_id", e.Features.Tenancy.Column)
	}
	if !doc.GenerationConfig.Tenancy {
		t.Errorf("generation_config.tenancy = false, want true")
	}
}

func TestBuild_SoftDeleteTable(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "posts",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "title", Type: "text"},
					{Name: "deleted_at", Type: "timestamptz", Nullable: true},
				},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "Post")
	if e.Features.SoftDelete == nil {
		t.Fatalf("features.soft_delete = nil, want populated")
	}
	if e.Features.SoftDelete.Column != "deleted_at" || e.Features.SoftDelete.Type != "timestamp" {
		t.Errorf("soft_delete = %+v", e.Features.SoftDelete)
	}
}

func TestBuild_CacheConfiguredTable(t *testing.T) {
	cfg := baseConfig()
	cfg.Cache = &config.CacheConfig{Enabled: true, TTL: "30s", KeyPrefix: "models", Serializer: config.SerializerJSON, Hydration: &config.HydrationConfig{}}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "email", Type: "text"}}},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "User")
	if e.Features.Cache == nil {
		t.Fatalf("features.cache = nil, want populated")
	}
	if e.Features.Cache.TTLSeconds != 30 {
		t.Errorf("ttl_seconds = %d, want 30", e.Features.Cache.TTLSeconds)
	}
	if e.Features.Cache.KeyPattern != "models:user:{id}" {
		t.Errorf("key_pattern = %q, want models:user:{id}", e.Features.Cache.KeyPattern)
	}
	if got := e.Features.Cache.InvalidatesOn; len(got) == 0 {
		t.Errorf("invalidates_on empty")
	}
}

func TestBuild_EventsTable(t *testing.T) {
	cfg := baseConfig()
	cfg.Events = &config.EventConfig{Enabled: true}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "email", Type: "text"}, {Name: "deleted_at", Type: "timestamptz", Nullable: true}}},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "User")
	if e.Features.Events == nil {
		t.Fatalf("features.events = nil, want populated")
	}
	if !e.Features.Events.Enabled {
		t.Errorf("events.enabled = false")
	}
	// Soft-delete is detected → SoftDeleted + Restored event types should be present.
	want := []string{"UserCreated", "UserDeleted", "UserRestored", "UserSoftDeleted", "UserUpdated"}
	if diff := cmp.Diff(want, e.Features.Events.Types); diff != "" {
		t.Errorf("events.types mismatch (-want +got):\n%s", diff)
	}
	if e.Features.Events.PayloadShape != "models.UserEvent" {
		t.Errorf("payload_shape = %q", e.Features.Events.PayloadShape)
	}
}

func TestBuild_ViewEntity(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
		},
		Views: []parser.View{
			{
				Name: "active_users_view",
				SQL:  "SELECT id, email FROM users WHERE deleted_at IS NULL",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid"},
					{Name: "email", Type: "text"},
				},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "ActiveUsersView")
	if e.Kind != "view" {
		t.Errorf("kind = %q, want view", e.Kind)
	}
	if e.PK.Kind != "none" {
		t.Errorf("view pk.kind = %q, want none", e.PK.Kind)
	}
	// View entities carry a source field (PRD §30.4.2) — the SQL definition
	// text, since no annotation-file path is tracked.
	if want := "SELECT id, email FROM users WHERE deleted_at IS NULL"; e.Source != want {
		t.Errorf("view source = %q, want %q", e.Source, want)
	}
	if len(e.Relationships) != 0 {
		t.Errorf("view relationships should be empty, got %d", len(e.Relationships))
	}
	// Views always emit List + Count.
	if got := len(e.Methods.Query); got < 2 {
		t.Errorf("view query methods = %d, want >= 2", got)
	}
}

func TestBuild_M2MRelationship(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{Name: "roles", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{
				Name: "user_roles",
				Columns: []parser.Column{
					{Name: "user_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "users", Column: "id"}},
					{Name: "role_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "roles", Column: "id"}},
				},
				// Composite PRIMARY KEY constraint is what the parser's
				// junctionConstraint() matches against to flip the table from
				// O2M into M2M (parser/relationship.go).
				Constraints: []parser.Constraint{
					{Type: parser.PrimaryKey, Columns: []string{"user_id", "role_id"}},
				},
			},
		},
	}
	parser.DetectRelationships(schema)
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "User")
	var m2m *manifest.Relationship
	for i := range e.Relationships {
		if e.Relationships[i].Kind == "m2m" {
			m2m = &e.Relationships[i]
			break
		}
	}
	if m2m == nil {
		t.Fatalf("no m2m relationship on User; got=%+v", e.Relationships)
	}
	if m2m.TargetEntity != "Role" {
		t.Errorf("m2m.target = %q, want Role", m2m.TargetEntity)
	}
	if m2m.Junction == nil || m2m.Junction.Table != "user_roles" {
		t.Errorf("m2m.junction = %+v, want user_roles", m2m.Junction)
	}
}

// TestBuild_M2MJunctionSchema pins that a junction carries its schema the way
// an entity does (PRD §30.4.2): two junctions named document_labels, one in
// public and one in audit, must not collapse to the same bare name.
func TestBuild_M2MJunctionSchema(t *testing.T) {
	cfg := baseConfig()
	uuidPK := func(schema, name string) parser.Table {
		return parser.Table{Name: name, Schema: schema, Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}}
	}
	junction := func(schema, targetFK, target string) parser.Table {
		return parser.Table{
			Name:   "document_labels",
			Schema: schema,
			Columns: []parser.Column{
				{Name: "document_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Schema: "public", Table: "documents", Column: "id"}},
				{Name: targetFK, Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Schema: "public", Table: target, Column: "id"}},
			},
			Constraints: []parser.Constraint{
				{Type: parser.PrimaryKey, Columns: []string{"document_id", targetFK}},
			},
		}
	}
	schema := &parser.Schema{
		Tables: []parser.Table{
			uuidPK("public", "documents"),
			uuidPK("public", "labels"),
			uuidPK("public", "tags"),
			junction("public", "label_id", "labels"),
			junction("audit", "tag_id", "tags"),
		},
	}
	parser.DetectRelationships(schema)
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := map[string]*manifest.Junction{}
	for _, r := range findEntity(t, doc, "Document").Relationships {
		if r.Kind == "m2m" {
			got[r.Name] = r.Junction
		}
	}
	want := map[string]*manifest.Junction{
		"labels": {Table: "document_labels", Schema: "public", LocalFK: "document_id", TargetFK: "label_id"},
		"tags":   {Table: "document_labels", Schema: "audit", LocalFK: "document_id", TargetFK: "tag_id"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Document m2m junctions mismatch (-want +got):\n%s", diff)
	}
}

// TestBuild_FKSchema pins that an FK edge carries its table's schema the way
// an entity does (PRD §30.4.2): Document's edge into audit.document_notes and
// Product's edge into public.document_notes must not collapse to the same bare
// name. EventRef's edge into audit.events is belongs-to, so it names the
// table that holds event_id — public.event_refs — not the target.
func TestBuild_FKSchema(t *testing.T) {
	cfg := baseConfig()
	fkCol := func(name, schema, table string, unique bool) parser.Column {
		return parser.Column{Name: name, Type: "uuid", Unique: unique, FKReference: &parser.FKReference{Schema: schema, Table: table, Column: "id"}}
	}
	table := func(schema, name string, cols ...parser.Column) parser.Table {
		return parser.Table{
			Name:    name,
			Schema:  schema,
			Columns: append([]parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}, cols...),
		}
	}
	schema := &parser.Schema{
		Tables: []parser.Table{
			table("public", "documents"),
			table("public", "products"),
			table("audit", "document_notes", fkCol("document_id", "public", "documents", false)),
			table("public", "document_notes", fkCol("product_id", "public", "products", false)),
			table("audit", "events"),
			table("public", "event_refs", fkCol("event_id", "audit", "events", true)),
		},
	}
	parser.DetectRelationships(schema)
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := map[string]*manifest.FK{}
	for _, entity := range []string{"Document", "Product", "EventRef"} {
		for _, r := range findEntity(t, doc, entity).Relationships {
			if r.FK != nil {
				got[entity+"."+r.Name] = r.FK
			}
		}
	}
	want := map[string]*manifest.FK{
		"Document.document_notes": {Table: "document_notes", Schema: "audit", Column: "document_id"},
		"Product.document_notes":  {Table: "document_notes", Schema: "public", Column: "product_id"},
		"EventRef.events":         {Table: "event_refs", Schema: "public", Column: "event_id"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("FK edges mismatch (-want +got):\n%s", diff)
	}
}

// TestBuild_O2MFKNotOnTargetStaysOnTarget pins that an O2M edge names its
// target as the FK table even when FKOnTarget reads false. O2M fixes the
// direction by its type (PRD §13.1: the FK lives on the related table), so
// false there is not belongs-to; only the O2O arm may read it that way
// (gen/nested_names.go). A config `fk:` naming a column the target does not
// have is the shape that reaches it: users.email is on users, not notes.
func TestBuild_O2MFKNotOnTargetStaysOnTarget(t *testing.T) {
	cfg := baseConfig()
	cfg.Tables = map[string]config.TableConfig{
		"users": {Relationships: []config.TableRelationship{
			{Name: "Notes", Type: "one_to_many", Table: "notes", FK: "email"},
		}},
	}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "email", Type: "text"}}},
			{Name: "notes", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "body", Type: "text"}}},
		},
	}
	parser.DetectRelationships(schema)
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var got *manifest.FK
	for _, r := range findEntity(t, doc, "User").Relationships {
		if r.Name == "Notes" {
			got = r.FK
		}
	}
	want := &manifest.FK{Table: "notes", Column: "email"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("User.Notes FK mismatch (-want +got):\n%s", diff)
	}
}

// --- extended metadata tests (PRD §30.7) ---

func TestBuild_ColumnAndTableComments(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:    "users",
				Comment: "Application users.",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Comment: "Primary key."},
					{Name: "email", Type: "text", Comment: "Login identity."},
				},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "User")
	if e.Comment != "Application users." {
		t.Errorf("entity.comment = %q", e.Comment)
	}
	for _, c := range e.Columns {
		if c.Name == "id" && c.Comment != "Primary key." {
			t.Errorf("id.comment = %q", c.Comment)
		}
		if c.Name == "email" && c.Comment != "Login identity." {
			t.Errorf("email.comment = %q", c.Comment)
		}
	}
}

func TestBuild_CheckConstraintFlattened(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "price", Type: "numeric"},
					{Name: "discount", Type: "numeric"},
				},
				Constraints: []parser.Constraint{
					{Name: "price_gt_discount", Type: parser.Check, Columns: []string{"price", "discount"}, CheckExpression: "price > discount"},
				},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "Product")
	gotChecks := map[string]string{}
	for _, c := range e.Columns {
		if c.Check != "" {
			gotChecks[c.Name] = c.Check
		}
	}
	want := map[string]string{
		"price":    "price > discount",
		"discount": "price > discount",
	}
	if diff := cmp.Diff(want, gotChecks); diff != "" {
		t.Errorf("flattened CHECK mismatch (-want +got):\n%s", diff)
	}
}

// TestBuild_CheckConstraintDerivesColumnsFromExpression covers CHECK columns:
// the dialect parsers populate CheckExpression but leave Constraint.Columns
// empty for CHECK constraints, so the builder must derive the participating
// columns from the expression itself. TestBuild_CheckConstraintFlattened
// hand-sets Columns and therefore never exercised this path.
func TestBuild_CheckConstraintDerivesColumnsFromExpression(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "order_items",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "quantity", Type: "integer"},
					{Name: "unit_price", Type: "numeric"},
					{Name: "note", Type: "text", Nullable: true},
				},
				Constraints: []parser.Constraint{
					// Real parser output: CheckExpression set, Columns empty.
					{Name: "price_qty_positive", Type: parser.Check, CheckExpression: "unit_price >= 0 AND quantity > 0"},
				},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "OrderItem")
	gotChecks := map[string]string{}
	for _, c := range e.Columns {
		if c.Check != "" {
			gotChecks[c.Name] = c.Check
		}
	}
	// The CHECK flattens onto both referenced columns and no others — `note`
	// and `id` are not named in the expression.
	want := map[string]string{
		"quantity":   "unit_price >= 0 AND quantity > 0",
		"unit_price": "unit_price >= 0 AND quantity > 0",
	}
	if diff := cmp.Diff(want, gotChecks); diff != "" {
		t.Errorf("derived CHECK columns mismatch (-want +got):\n%s", diff)
	}
}

// TestBuild_CheckConstraintExpressionLiteralNotMatched guards the tokenizer's
// string-literal skip: a column name appearing only inside a quoted literal
// must not be treated as a participating column.
func TestBuild_CheckConstraintExpressionLiteralNotMatched(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "status", Type: "text"},
					{Name: "price", Type: "numeric"},
				},
				Constraints: []parser.Constraint{
					// `price` appears only inside a string literal, so only
					// `status` should carry the CHECK.
					{Name: "status_allowed", Type: parser.Check, CheckExpression: "status IN ('active', 'price')"},
				},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "Product")
	gotChecks := map[string]string{}
	for _, c := range e.Columns {
		if c.Check != "" {
			gotChecks[c.Name] = c.Check
		}
	}
	want := map[string]string{
		"status": "status IN ('active', 'price')",
	}
	if diff := cmp.Diff(want, gotChecks); diff != "" {
		t.Errorf("literal-skip CHECK mismatch (-want +got):\n%s", diff)
	}
}

func TestBuild_DefaultKindClassification(t *testing.T) {
	tests := []struct {
		def  string
		kind string
	}{
		{def: "gen_random_uuid()", kind: "function"},
		{def: "CURRENT_TIMESTAMP", kind: "function"},
		{def: "'pending'", kind: "literal"},
		{def: "0", kind: "literal"},
		{def: "(price * quantity)", kind: "expression"},
		// SQLite parenthesizes function defaults in DDL; a wrapped `(now())`
		// form is also common. Both are function shapes, not expressions.
		{def: "(datetime('now'))", kind: "function"},
		{def: "(now())", kind: "function"},
	}
	for _, tt := range tests {
		t.Run(tt.def, func(t *testing.T) {
			cfg := baseConfig()
			schema := &parser.Schema{
				Tables: []parser.Table{
					{
						Name: "t",
						Columns: []parser.Column{
							{Name: "id", Type: "uuid", PrimaryKey: true},
							{Name: "x", Type: "text", Default: tt.def},
						},
					},
				},
			}
			doc, err := manifest.Build(testBuildInput(t, schema, cfg))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			e := findEntity(t, doc, "T")
			for _, c := range e.Columns {
				if c.Name == "x" && c.DefaultKind != tt.kind {
					t.Errorf("default %q → kind %q, want %q", tt.def, c.DefaultKind, tt.kind)
				}
			}
		})
	}
}

func TestBuild_IndexesIncludePKAndUnique(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "email", Type: "text"},
				},
				Constraints: []parser.Constraint{
					{Name: "users_email_key", Type: parser.Unique, Columns: []string{"email"}},
				},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "User")
	if len(e.Indexes) != 2 {
		t.Fatalf("indexes = %d, want 2 (pk + email); got=%+v", len(e.Indexes), e.Indexes)
	}
	gotNames := []string{e.Indexes[0].Name, e.Indexes[1].Name}
	if diff := cmp.Diff([]string{"users_email_key", "users_pkey"}, gotNames); diff != "" {
		t.Errorf("index names mismatch (-want +got):\n%s", diff)
	}
}

// TestBuild_PKIndexNeedsASchemaDeclaredKey pins the manifest half of the rule:
// indexes[] lists the indexes the database has (PRD §30.7), so a primary-key
// index is listed only for a PRIMARY KEY the schema declared. A key asserted
// through primary_key.columns (§8.6) has none. Measured on PostgreSQL 16:
// `counters (key TEXT NOT NULL)` + `ADD CONSTRAINT counters_key_uq UNIQUE
// (key)` carries only counters_key_uq, and a table with no constraint carries
// no index. The column flags model the schema after
// cli.applyPrimaryKeyOverrides has written the override back.
func TestBuild_PKIndexNeedsASchemaDeclaredKey(t *testing.T) {
	override := func(cols ...string) config.TableConfig {
		return config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: cols}}
	}
	tests := []struct {
		name     string
		table    parser.Table
		tableCfg config.TableConfig
		want     []string
	}{
		{
			name: "schema-declared key lists the PK index",
			table: parser.Table{
				Name:    "probe_keys",
				Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "code", Type: "text"}},
			},
			want: []string{"probe_keys_pkey"},
		},
		{
			name: "app-enforced key with no index lists none",
			table: parser.Table{
				Name:    "probe_keys",
				Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "code", Type: "text"}},
			},
			tableCfg: override("id"),
			want:     []string{},
		},
		{
			name: "app-enforced key backed by a UNIQUE lists only the UNIQUE",
			table: parser.Table{
				Name:    "counters",
				Columns: []parser.Column{{Name: "key", Type: "text", PrimaryKey: true, Unique: true}, {Name: "count", Type: "bigint"}},
				Constraints: []parser.Constraint{
					{Name: "counters_key_uq", Type: parser.Unique, Columns: []string{"key"}},
				},
			},
			tableCfg: override("key"),
			want:     []string{"counters_key_uq"},
		},
		{
			name: "table-level PK restated in config lists the PK index",
			table: parser.Table{
				Name: "order_items",
				Columns: []parser.Column{
					{Name: "order_id", Type: "uuid", PrimaryKey: true},
					{Name: "product_id", Type: "uuid", PrimaryKey: true},
				},
				Constraints: []parser.Constraint{
					{Name: "order_items_pkey", Type: parser.PrimaryKey, Columns: []string{"order_id", "product_id"}},
				},
			},
			tableCfg: override("product_id", "order_id"),
			want:     []string{"order_items_pkey"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			if tt.tableCfg.PrimaryKey != nil {
				cfg.Tables[tt.table.Name] = tt.tableCfg
			}
			schema := &parser.Schema{Tables: []parser.Table{tt.table}}
			doc, err := manifest.Build(testBuildInput(t, schema, cfg))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if len(doc.Entities) != 1 {
				t.Fatalf("entities = %v, want exactly the one table", entityNames(doc))
			}
			got := []string{}
			for _, ix := range doc.Entities[0].Indexes {
				got = append(got, ix.Name)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("index names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestBuild_InlineUniqueIsListed pins the inline-UNIQUE half of that rule: an
// inline column UNIQUE is recorded on the column (InlineUnique), not as a
// Constraint, yet the database builds an index for it (PRD §30.7 "All declared
// indexes"). It is listed as `<table>_<column>_key` on every dialect,
// PostgreSQL's default name, as the PK index is `<table>_pkey` (measured on
// PostgreSQL 16: `email TEXT NOT NULL UNIQUE` on users → users_email_key). A
// column that is Unique only through a table-level constraint, single- or
// multi-column, is listed once, under that constraint.
func TestBuild_InlineUniqueIsListed(t *testing.T) {
	id := parser.Column{Name: "id", Type: "uuid", PrimaryKey: true}
	key := func(name string, cols ...string) manifest.Index {
		return manifest.Index{Name: name, Columns: cols, Unique: true, Method: "btree"}
	}
	tests := []struct {
		name     string
		table    parser.Table
		tableCfg config.TableConfig
		want     []manifest.Index
	}{
		{
			name: "inline UNIQUE beside a table-level UNIQUE on another column",
			table: parser.Table{
				Name: "users",
				Columns: []parser.Column{
					id,
					{Name: "email", Type: "text", Unique: true, InlineUnique: true},
					{Name: "handle", Type: "text", Unique: true},
				},
				Constraints: []parser.Constraint{
					{Name: "users_handle_uq", Type: parser.Unique, Columns: []string{"handle"}},
				},
			},
			want: []manifest.Index{
				key("users_email_key", "email"),
				key("users_handle_uq", "handle"),
				key("users_pkey", "id"),
			},
		},
		{
			name: "inline UNIQUE column inside a multi-column UNIQUE is listed by both",
			table: parser.Table{
				Name: "users",
				Columns: []parser.Column{
					id,
					{Name: "email", Type: "text", Unique: true, InlineUnique: true},
					{Name: "handle", Type: "text"},
				},
				Constraints: []parser.Constraint{
					{Name: "users_email_handle_uq", Type: parser.Unique, Columns: []string{"email", "handle"}},
				},
			},
			want: []manifest.Index{
				key("users_email_handle_uq", "email", "handle"),
				key("users_email_key", "email"),
				key("users_pkey", "id"),
			},
		},
		{
			name: "multi-column UNIQUE alone lists no per-column key",
			table: parser.Table{
				Name: "users",
				Columns: []parser.Column{
					id,
					{Name: "email", Type: "text"},
					{Name: "handle", Type: "text"},
				},
				Constraints: []parser.Constraint{
					{Name: "users_email_handle_uq", Type: parser.Unique, Columns: []string{"email", "handle"}},
				},
			},
			want: []manifest.Index{
				key("users_email_handle_uq", "email", "handle"),
				key("users_pkey", "id"),
			},
		},
		{
			name: "inline UNIQUE backing an app-enforced key is listed in place of the PK index",
			table: parser.Table{
				Name: "counters",
				Columns: []parser.Column{
					{Name: "key", Type: "text", PrimaryKey: true, Unique: true, InlineUnique: true},
					{Name: "count", Type: "bigint"},
				},
			},
			tableCfg: config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"key"}}},
			want:     []manifest.Index{key("counters_key_key", "key")},
		},
	}
	for _, tt := range tests {
		for _, d := range []config.Dialect{config.DialectPostgres, config.DialectMySQL, config.DialectSQLite} {
			t.Run(tt.name+"/"+string(d), func(t *testing.T) {
				cfg := baseConfig()
				cfg.Input.Dialect = d
				if d != config.DialectPostgres {
					cfg.Output.Driver = config.DriverStdlib
				}
				if tt.tableCfg.PrimaryKey != nil {
					cfg.Tables[tt.table.Name] = tt.tableCfg
				}
				schema := &parser.Schema{Tables: []parser.Table{tt.table}}
				doc, err := manifest.Build(testBuildInput(t, schema, cfg))
				if err != nil {
					t.Fatalf("Build: %v", err)
				}
				if len(doc.Entities) != 1 {
					t.Fatalf("entities = %v, want exactly the one table", entityNames(doc))
				}
				if diff := cmp.Diff(tt.want, doc.Entities[0].Indexes); diff != "" {
					t.Errorf("indexes mismatch (-want +got):\n%s", diff)
				}
			})
		}
	}
}

// TestBuild_PartialUniqueIndex covers PRD §30.7 — partial UNIQUE indexes
// surface their predicate in Indexes[].Where (was deferred from §18.2 pending
// parser support for parser.Constraint.Where). Also verifies the manifest
// does NOT emit a FindByX method for the partial index — the predicate
// prevents safe single-row lookup by the indexed columns.
func TestBuild_PartialUniqueIndex(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "email", Type: "text"},
					{Name: "deleted_at", Type: "timestamptz", Nullable: true},
				},
				Constraints: []parser.Constraint{
					{
						Name:    "users_active_email_idx",
						Type:    parser.Unique,
						Columns: []string{"email"},
						Method:  "btree",
						Where:   "deleted_at IS NULL",
					},
				},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "User")

	var partial *manifest.Index
	for i := range e.Indexes {
		if e.Indexes[i].Name == "users_active_email_idx" {
			partial = &e.Indexes[i]
			break
		}
	}
	if partial == nil {
		t.Fatalf("expected users_active_email_idx in indexes; got=%+v", e.Indexes)
	}
	if !partial.Unique {
		t.Errorf("Unique = false, want true")
	}
	if partial.Method != "btree" {
		t.Errorf("Method = %q, want %q", partial.Method, "btree")
	}
	if partial.Where != "deleted_at IS NULL" {
		t.Errorf("Where = %q, want %q", partial.Where, "deleted_at IS NULL")
	}

	// Sanity check: no index, partial or otherwise, backs a method entry.
	// The generator emits no FindBy<Col> lookups at all — methods[] lists the
	// generated client surface and nothing else.
	for _, m := range e.Methods.Query {
		if strings.HasPrefix(m.Name, "FindBy") {
			t.Errorf("query method %q derived from an index; no FindBy* method is generated", m.Name)
		}
	}
}

// TestBuild_GINIndexMethod covers PRD §30.7 — non-btree access methods
// (gin / gist / hash) surface in Indexes[].Method (was deferred from §18.2
// pending parser support for parser.Constraint.Method).
func TestBuild_GINIndexMethod(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "documents",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "tags", Type: "jsonb"},
				},
				Constraints: []parser.Constraint{
					{
						Name:    "documents_tags_gin",
						Type:    parser.Index,
						Columns: []string{"tags"},
						Method:  "gin",
					},
				},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "Document")

	var gin *manifest.Index
	for i := range e.Indexes {
		if e.Indexes[i].Name == "documents_tags_gin" {
			gin = &e.Indexes[i]
			break
		}
	}
	if gin == nil {
		t.Fatalf("expected documents_tags_gin in indexes; got=%+v", e.Indexes)
	}
	if gin.Unique {
		t.Errorf("Unique = true, want false")
	}
	if gin.Method != "gin" {
		t.Errorf("Method = %q, want %q", gin.Method, "gin")
	}
	if gin.Where != "" {
		t.Errorf("Where = %q, want empty", gin.Where)
	}
}

// --- generation_config snapshot ---

func TestBuild_GenerationConfig_OffByDefault(t *testing.T) {
	// baseConfig keeps the soft-delete / update-column detection *candidate*
	// lists non-empty, the same condition production applyDefaults produces.
	// The toggles must still be false: the schema's lone table carries no
	// matching column, so soft_delete / audit_columns reflect actual detection,
	// not the mere presence of a candidate list (PRD §30.4.3).
	cfg := baseConfig()
	doc, err := manifest.Build(testBuildInput(t, &parser.Schema{
		Tables: []parser.Table{{Name: "t", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}}},
	}, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	gc := doc.GenerationConfig
	if gc.Cache || gc.Tenancy || gc.Events || gc.SoftDelete || gc.Views || gc.GraphQL || gc.AuditColumns || gc.GraphTopLevel {
		t.Errorf("generation_config has unexpected truthy toggles: %+v", gc)
	}
}

func TestBuild_GenerationConfig_AllOn(t *testing.T) {
	cfg := baseConfig()
	cfg.Cache = &config.CacheConfig{Enabled: true, TTL: "1m"}
	cfg.Events = &config.EventConfig{Enabled: true}
	cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "tenant_id"}
	// output.dir=models with a top-level sibling graph dir → graph_top_level.
	cfg.Output.Dir = "models"
	cfg.API = &config.APIConfig{Enabled: true, GraphQL: &config.GraphQLAPIConfig{Enabled: true, SchemaDir: "graph", ResolverDir: "graph"}}
	cfg.Views["v"] = config.ViewConfig{}
	schema := &parser.Schema{
		Tables: []parser.Table{{Name: "t", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "tenant_id", Type: "uuid"}, {Name: "deleted_at", Type: "timestamptz", Nullable: true}, {Name: "updated_at", Type: "timestamptz"}}}},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	gc := doc.GenerationConfig
	wantAllTrue := []struct {
		name string
		got  bool
	}{
		{"cache", gc.Cache},
		{"tenancy", gc.Tenancy},
		{"events", gc.Events},
		{"soft_delete", gc.SoftDelete},
		{"views", gc.Views},
		{"graphql", gc.GraphQL},
		{"audit_columns", gc.AuditColumns},
		{"graph_top_level", gc.GraphTopLevel},
	}
	for _, tt := range wantAllTrue {
		if !tt.got {
			t.Errorf("generation_config.%s = false, want true", tt.name)
		}
	}
}

// TestBuild_GenerationConfig_GraphTopLevel asserts graph_top_level is inferred
// purely from the root-relative resolver_dir → output.dir path relationship
// (PRD §30.4.3 / §26.5.8). It is true only when the resolved graph
// tree escapes output.dir — a top-level sibling — and false for nested layouts
// (the default <output.dir>/graph, or an explicit models/graph), independent
// of whether GraphQL is merely enabled.
func TestBuild_GenerationConfig_GraphTopLevel(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{{Name: "t", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}}},
	}
	tests := []struct {
		name        string
		outputDir   string
		resolverDir string
		want        bool
	}{
		{"nested default <output.dir>/graph", "models", "models/graph", false},
		{"nested deeper subdir", "models", "models/api/graph", false},
		{"top-level sibling graph", "models", "graph", true},
		{"graph under empty output.dir stays nested", "", "graph", false},
		{"deeply nested output.dir, top-level graph", "internal/models", "graph", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Output.Dir = tt.outputDir
			cfg.API = &config.APIConfig{Enabled: true, GraphQL: &config.GraphQLAPIConfig{Enabled: true, SchemaDir: tt.resolverDir, ResolverDir: tt.resolverDir}}
			doc, err := manifest.Build(testBuildInput(t, schema, cfg))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if got := doc.GenerationConfig.GraphTopLevel; got != tt.want {
				t.Errorf("graph_top_level = %v, want %v (output.dir=%q resolver_dir=%q)", got, tt.want, tt.outputDir, tt.resolverDir)
			}
			// GraphQL enabled must not by itself imply graph_top_level.
			if !doc.GenerationConfig.GraphQL {
				t.Errorf("graphql = false, want true (API is enabled)")
			}
		})
	}
}

// TestBuild_GenerationConfig_GraphTopLevel_DisabledAPI confirms graph_top_level
// is false when GraphQL is not enabled, regardless of any stale dir values.
func TestBuild_GenerationConfig_GraphTopLevel_DisabledAPI(t *testing.T) {
	cfg := baseConfig()
	cfg.Output.Dir = "models"
	cfg.API = &config.APIConfig{Enabled: false, GraphQL: &config.GraphQLAPIConfig{Enabled: false, SchemaDir: "graph", ResolverDir: "graph"}}
	doc, err := manifest.Build(testBuildInput(t, &parser.Schema{
		Tables: []parser.Table{{Name: "t", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}}},
	}, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if doc.GenerationConfig.GraphTopLevel {
		t.Errorf("graph_top_level = true, want false when GraphQL disabled")
	}
}

// --- sql_bodies per method type ---

func TestBuild_SQLBodies_CoverEveryMethodType(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "email", Type: "text"},
					{Name: "updated_at", Type: "timestamptz"},
					// Soft-delete column → flips Operations.SoftDelete on and
					// triggers HardDelete/SoftDelete naming split (PRD §9.8.8).
					{Name: "deleted_at", Type: "timestamptz", Nullable: true},
				},
				Constraints: []parser.Constraint{
					{Name: "users_email_key", Type: parser.Unique, Columns: []string{"email"}},
				},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "User")

	// Index methods by name for assertion.
	byName := map[string]manifest.Method{}
	for _, m := range append(e.Methods.Query, e.Methods.Mutation...) {
		byName[m.Name] = m
	}

	checks := []struct {
		method string
		want   string // substring expected in the postgres SQL body
	}{
		// Reads carry the soft-delete guard the generated bodies append.
		{"Get", `WHERE "id" = $1 AND "deleted_at" IS NULL LIMIT 1`},
		{"GetMany", "ORDER BY <sort> LIMIT <limit>"},
		{"Count", "SELECT COUNT(*)"},
		{"Exists", `SELECT EXISTS(SELECT 1 FROM "users" WHERE "id" = $1`},
		{"ExistsWhere", "<filter>"},
		// Stream iterates the driver cursor unbounded (PRD §9.4a) — its body
		// must NOT include LIMIT, even though GetMany's does.
		{"Stream", "<filter>"},
		{"Create", `INSERT INTO "users" (<columns>) VALUES (<values>) RETURNING "id"`},
		{"CreateMany", `INSERT INTO "users" (<columns>) VALUES <values> RETURNING "id"`},
		{"Upsert", `ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>`},
		// The by-PK writes carry no soft-delete guard and no RETURNING: the
		// generated bodies pass PK conditions alone and re-Get the row.
		{"Update", `UPDATE "users" SET <set> WHERE "id" = $1`},
		// UpdateWhere and HardDeleteWhere pass filter.ToConditions() straight
		// through — neither carries the read-path soft-delete guard, unlike
		// SoftDeleteWhere and RestoreWhere, which append their own.
		{"UpdateWhere", `UPDATE "users" SET <set> WHERE <filter> RETURNING "id"`},
		{"HardDeleteWhere", `DELETE FROM "users" WHERE <filter> RETURNING "id"`},
		{"SoftDeleteWhere", `WHERE <filter> AND "deleted_at" IS NULL RETURNING "id"`},
		{"HardDelete", `DELETE FROM "users" WHERE "id" = $1`},
		{"HardDeleteMany", `DELETE FROM "users" WHERE "id" IN (<ids>)`},
		{"SoftDelete", `UPDATE "users" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "id" = $1`},
		{"Restore", `UPDATE "users" SET "deleted_at" = $1 WHERE "id" = $2`},
		{"RestoreWhere", `WHERE <filter> AND "deleted_at" IS NOT NULL`},
	}
	for _, tt := range checks {
		m, ok := byName[tt.method]
		if !ok {
			t.Errorf("method %q missing; got methods=%v", tt.method, methodNames(byName))
			continue
		}
		body := m.SQLBodies["postgres"]
		if body == "" {
			t.Errorf("method %q has no postgres sql_body", tt.method)
			continue
		}
		if !strings.Contains(body, tt.want) {
			t.Errorf("method %q sql body missing %q\n--- body ---\n%s", tt.method, tt.want, body)
		}
	}

	// Reinforce the Stream LIMIT-absence contract — easy to regress if a
	// future refactor unifies sqlGetMany / sqlStream.
	if stream, ok := byName["Stream"]; ok {
		if strings.Contains(stream.SQLBodies["postgres"], "LIMIT") {
			t.Errorf("Stream sql_body must not include LIMIT (PRD §9.4a):\n%s", stream.SQLBodies["postgres"])
		}
	}

	// Paginate and Connection issue no statement of their own — they compose
	// Count and GetMany — so they carry no body rather than an invented one.
	for _, name := range []string{"Paginate", "Connection"} {
		m, ok := byName[name]
		if !ok {
			t.Errorf("method %q missing; got methods=%v", name, methodNames(byName))
			continue
		}
		if len(m.SQLBodies) != 0 {
			t.Errorf("%s must carry no sql_bodies (it issues no statement); got %v", name, m.SQLBodies)
		}
	}

	// The names the defect invented must be gone in both directions: not one
	// of them is generated, and every real method it hid is now present.
	for _, gone := range []string{"FindByID", "FindByEmail", "List", "Walk", "Delete", "UpsertByEmail", "UpsertConflictEmail"} {
		if _, ok := byName[gone]; ok {
			t.Errorf("method %q is advertised but generated nowhere", gone)
		}
	}
}

// TestBuild_SQLBodies_RuntimeClausesAreTokens pins the rule that replaced the
// representative expansions the manifest used to publish.
//
// An earlier revision made an invented column list correct: the
// maximal-insertable set with the serial PK stripped, and a DO UPDATE SET with
// the PK and conflict columns removed. Neither describes a statement the
// generated code issues — Create builds its column list from the omittable
// fields the caller actually set, and Upsert's conflict half is chosen by the
// `target` argument at call time — so the placeholder positions were wrong
// too. The manifest now names those clauses with tokens and leaves the
// insertable-column question to the entity's columns[] block, where
// default_kind and auto already answer it. The generator-side exclusions
// themselves are unchanged and covered by the gen package's own tests.
func TestBuild_SQLBodies_RuntimeClausesAreTokens(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "accounts",
				Columns: []parser.Column{
					// serial PK -> PKStrategy == db -> excluded from INSERT.
					{Name: "id", Type: "serial", PrimaryKey: true},
					{Name: "email", Type: "text"},
					{Name: "name", Type: "text"},
				},
				Constraints: []parser.Constraint{
					{Name: "accounts_email_key", Type: parser.Unique, Columns: []string{"email"}},
				},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "Account")

	byName := map[string]manifest.Method{}
	for _, m := range append(e.Methods.Query, e.Methods.Mutation...) {
		byName[m.Name] = m
	}

	// Two conflict targets exist (serial PK "id" + unique "email"), and the
	// generator still names neither: Upsert and UpsertMany are two operations
	// over one ConflictTarget enum, not one method per target. A per-target
	// method would show up here as UpsertPK / UpsertEmail.
	var upserts []string
	for name := range byName {
		if strings.HasPrefix(name, "Upsert") {
			upserts = append(upserts, name)
		}
	}
	slices.Sort(upserts)
	if diff := cmp.Diff([]string{"Upsert", "UpsertMany"}, upserts); diff != "" {
		t.Errorf("upsert methods mismatch (-want +got):\n%s\nthe target is an argument, not a name suffix", diff)
	}

	tests := []struct {
		method string
		want   string
	}{
		{"Create", `INSERT INTO "accounts" (<columns>) VALUES (<values>) RETURNING "id"`},
		{"Upsert", `INSERT INTO "accounts" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"`},
		// UpsertMany carries no RETURNING where Upsert does: it never reads its
		// keys out of the statement, because RETURNING omits every row that
		// took the DO NOTHING branch (PRD §9.5, §9.7).
		{"UpsertMany", `INSERT INTO "accounts" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>`},
		{"Update", `UPDATE "accounts" SET <set> WHERE "id" = $1`},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			m, ok := byName[tt.method]
			if !ok {
				t.Fatalf("method %q missing; got %v", tt.method, methodNames(byName))
			}
			if got := m.SQLBodies["postgres"]; got != tt.want {
				t.Errorf("%s sql body =\n  %s\nwant\n  %s", tt.method, got, tt.want)
			}
		})
	}

	// No body may carry an expanded column list where the statement builds one
	// at call time - that is the shape the tokens exist to prevent.
	for _, name := range []string{"Create", "Upsert", "Update"} {
		body := byName[name].SQLBodies["postgres"]
		for _, col := range []string{`"email"`, `"name"`} {
			if strings.Contains(body, col) {
				t.Errorf("%s sql body names %s, but the generated statement selects its columns at call time:\n%s", name, col, body)
			}
		}
	}
}

func TestBuild_PerTableOptOut(t *testing.T) {
	cfg := baseConfig()
	off := false
	cfg.Tables["audit_logs"] = config.TableConfig{Manifest: &config.TableManifestConfig{Enabled: &off}}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{Name: "audit_logs", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, e := range doc.Entities {
		if e.Name == "AuditLog" {
			t.Errorf("audit_logs should be excluded from manifest, but entity present: %+v", e)
		}
	}
}

func TestBuild_TopLevelEnvelope(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{{Name: "t", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}}},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if doc.SchemaVersion != manifest.SchemaVersion {
		t.Errorf("schema_version = %q", doc.SchemaVersion)
	}
	if doc.Generator.Name != "sqlgen" || doc.Generator.Version != "0.42.0" {
		t.Errorf("generator = %+v", doc.Generator)
	}
	if doc.Dialect != "postgres" {
		t.Errorf("dialect = %q", doc.Dialect)
	}
	if doc.Package != "models" {
		t.Errorf("package = %q", doc.Package)
	}
	if doc.Layout != "single" {
		t.Errorf("layout = %q", doc.Layout)
	}
	if !strings.Contains(doc.Schema, "/sqlgen/v0.42.0/cmd/sqlgen/manifest/schema/v1.json") {
		t.Errorf("$schema URL = %q", doc.Schema)
	}
}

// TestBuild_ConventionsMirrorRuntimeSurface pins the conventions block to
// the actual omittable + comparator package surfaces. The reviewer's first
// pass found drift here (constructors named Set/Omit, not From/Unset/Null),
// which would have shipped incorrect API references into the markdown +
// the runtime embed. Pinning the strings here surfaces a future drift the
// moment the runtime API changes.
func TestBuild_ConventionsMirrorRuntimeSurface(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{{Name: "t", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}}},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	wantOmittable := manifest.OmittableConvention{
		Package: "omittable",
		Type:    "omittable.Value[T]",
		Purpose: "Distinguishes 'not set' from 'set to zero value'. Used in <Entity>Update structs and partial-update contexts.",
		Construction: []string{
			"omittable.Set(value) — set to a value",
			"omittable.Omit[T]() — explicitly unset (zero state)",
		},
		Methods:      []string{"IsSet() bool", "IsZero() bool", "Get() (T, bool)", "MustGet() T"},
		JSONBehavior: "Marshals to the wrapped value when set; omitted when unset (omitzero semantics on the wrapper, via IsZero)",
	}
	if diff := cmp.Diff(wantOmittable, doc.Conventions.Omittable); diff != "" {
		t.Errorf("omittable conventions mismatch (-want +got):\n%s", diff)
	}
	// Comparator families must match the comparator/ package directory; the
	// reviewer caught a "Bytes" entry that doesn't exist.
	wantFamilies := []string{"Bool", "Enum", "ID", "JSON", "JSONB", "Number", "Slice", "String", "Time"}
	if diff := cmp.Diff(wantFamilies, doc.Conventions.Comparator.Families); diff != "" {
		t.Errorf("comparator.families mismatch (-want +got):\n%s", diff)
	}
	// Comparator examples (PRD §30.4.1) must mirror the runtime comparator
	// surface: scalar operators are pointer fields (new(v)), In is a plain
	// slice, generic families carry the [T] type argument, and null checks use
	// the Null *bool field carried by the Nullable* variants — not
	// omittable.Set, not an IsNull operator, and not a Null field on the base
	// families (String/ID/... have none), none of which exist on the comparator
	// structs. <pkg> is the config-dependent generated models package.
	wantExamples := map[string]string{
		"field_filter":  `<pkg>.UserFilter{Email: &comparator.String{Eq: new("a@x.com")}}`,
		"text_contains": `&comparator.String{Contains: new("acme")}`,
		"numeric_range": `&comparator.Number[int]{Between: &comparator.Range[int]{Start: 18, End: 65}}`,
		"time_after":    `&comparator.Time{Gt: new(cutoff)}`,
		"id_in":         `&comparator.ID{In: []string{"u1", "u2", "u3"}}`,
		"is_null":       `&comparator.NullableString{Null: new(true)}`,
		"compound_or":   `<pkg>.UserFilter{Or: []*<pkg>.UserFilter{{Email: &comparator.String{Eq: new("a@x.com")}}, {Email: &comparator.String{Eq: new("b@x.com")}}}}`,
	}
	if diff := cmp.Diff(wantExamples, doc.Conventions.Comparator.Examples); diff != "" {
		t.Errorf("comparator.examples mismatch (-want +got):\n%s", diff)
	}
}

// TestBuild_FilterAndOrAreSlicesOfPointers pins the Filter struct's And/Or
// fields to []*<T>Filter — matches the generated _filter.tmpl shape. The
// reviewer caught a manifest/codegen mismatch here.
func TestBuild_FilterAndOrAreSlicesOfPointers(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "email", Type: "text"}}}},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "User")
	gotTypes := map[string]string{}
	for _, f := range e.Filter.Fields {
		if f.Name == "And" || f.Name == "Or" {
			gotTypes[f.Name] = f.Type
		}
	}
	want := map[string]string{
		"And": "[]*UserFilter",
		"Or":  "[]*UserFilter",
	}
	if diff := cmp.Diff(want, gotTypes); diff != "" {
		t.Errorf("filter And/Or types mismatch (-want +got):\n%s", diff)
	}
}

// TestBuild_O2OSideDisambiguation pins the side contract: the manifest
// distinguishes o2o (parent / FK-holder) from m2o (child / referenced) via
// parser.Relationship.Side instead of the legacy Go-type prefix heuristic.
// The parent-side edge is auto-emitted by DetectRelationships for the
// unique-FK case; the child-side edge is opted into via a config-defined
// inverse relationship with `side: child`.
func TestBuild_O2OSideDisambiguation(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "profiles",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					// Nullable UNIQUE FK — auto-detection drives this through
					// addO2O (Side: SideParent) so the manifest emits "o2o" on
					// the User entity without any config help.
					{
						Name:        "profile_id",
						Type:        "uuid",
						Nullable:    true,
						Unique:      true,
						FKReference: &parser.FKReference{Table: "profiles", Column: "id"},
					},
				},
			},
		},
	}
	parser.DetectRelationships(schema)

	// Inverse view from the referenced side: opt in via config with
	// `side: child`. configRelationshipToContext threads Side through to
	// gen.RelationshipContext, and the manifest builder's relationshipKind
	// switch resolves (OneToOne, SideChild) to "m2o".
	cfg.Tables["profiles"] = config.TableConfig{
		Relationships: []config.TableRelationship{
			{Name: "User", Type: "o2o", Table: "users", FK: "profile_id", Side: "child"},
		},
	}

	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// User entity → exactly one auto-emitted o2o (parent / FK-holder).
	userEnt := findEntity(t, doc, "User")
	userKinds := make([]string, 0, len(userEnt.Relationships))
	for _, r := range userEnt.Relationships {
		userKinds = append(userKinds, r.Kind)
	}
	if diff := cmp.Diff([]string{"o2o"}, userKinds); diff != "" {
		t.Errorf("User relationship kinds mismatch (-want +got):\n%s", diff)
	}

	// Profile entity → exactly one config-defined m2o (child / referenced).
	profileEnt := findEntity(t, doc, "Profile")
	profileKinds := make([]string, 0, len(profileEnt.Relationships))
	for _, r := range profileEnt.Relationships {
		profileKinds = append(profileKinds, r.Kind)
	}
	if diff := cmp.Diff([]string{"m2o"}, profileKinds); diff != "" {
		t.Errorf("Profile relationship kinds mismatch (-want +got):\n%s", diff)
	}
}

func methodNames(m map[string]manifest.Method) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	return names
}

func TestBuild_MaterializedViewEntity(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "orders", Columns: []parser.Column{{Name: "id", Type: "bigint", PrimaryKey: true}}},
		},
		Views: []parser.View{
			{
				Name:                    "order_summary",
				SQL:                     "SELECT customer_id, sum(amount) AS total FROM orders GROUP BY customer_id",
				Materialized:            true,
				ConcurrentlyRefreshable: true,
				Columns: []parser.Column{
					{Name: "customer_id", Type: "bigint", PrimaryKey: true},
					{Name: "total", Type: "numeric", Nullable: true},
				},
			},
			{
				Name:         "daily_rollup",
				SQL:          "SELECT customer_id FROM orders",
				Materialized: true,
				Columns:      []parser.Column{{Name: "customer_id", Type: "bigint"}},
			},
			{
				Name:    "active_summary",
				SQL:     "SELECT id FROM orders",
				Columns: []parser.Column{{Name: "id", Type: "bigint"}},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	mv := findEntity(t, doc, "OrderSummary")
	if !mv.Materialized {
		t.Error("OrderSummary.Materialized = false, want true")
	}
	if got := len(mv.Methods.Mutation); got != 2 {
		t.Fatalf("OrderSummary mutation methods = %d, want 2 (Refresh + RefreshConcurrently)", got)
	}
	refresh := mv.Methods.Mutation[0]
	if refresh.Name != "Refresh" {
		t.Errorf("mutation[0].Name = %q, want Refresh", refresh.Name)
	}
	if want := `REFRESH MATERIALIZED VIEW "order_summary"`; refresh.SQLBodies["postgres"] != want {
		t.Errorf("Refresh sql = %q, want %q", refresh.SQLBodies["postgres"], want)
	}
	conc := mv.Methods.Mutation[1]
	if conc.Name != "RefreshConcurrently" {
		t.Errorf("mutation[1].Name = %q, want RefreshConcurrently", conc.Name)
	}
	if want := `REFRESH MATERIALIZED VIEW CONCURRENTLY "order_summary"`; conc.SQLBodies["postgres"] != want {
		t.Errorf("RefreshConcurrently sql = %q, want %q", conc.SQLBodies["postgres"], want)
	}
	if diff := cmp.Diff([]string{"database.ErrRefreshConcurrentlyInTx"}, conc.Errors); diff != "" {
		t.Errorf("RefreshConcurrently errors mismatch (-want +got):\n%s", diff)
	}
	// The discovered PK flows through like a view @pk.
	if mv.PK.Kind != "single" {
		t.Errorf("OrderSummary pk.kind = %q, want single", mv.PK.Kind)
	}

	plain := findEntity(t, doc, "DailyRollup")
	if !plain.Materialized {
		t.Error("DailyRollup.Materialized = false, want true")
	}
	if got := len(plain.Methods.Mutation); got != 1 {
		t.Fatalf("DailyRollup mutation methods = %d, want 1 (Refresh only — no unique index)", got)
	}
	if plain.Methods.Mutation[0].Name != "Refresh" {
		t.Errorf("DailyRollup mutation[0].Name = %q, want Refresh", plain.Methods.Mutation[0].Name)
	}

	regular := findEntity(t, doc, "ActiveSummary")
	if regular.Materialized {
		t.Error("ActiveSummary.Materialized = true, want false (regular view)")
	}
	if got := len(regular.Methods.Mutation); got != 0 {
		t.Errorf("ActiveSummary mutation methods = %d, want 0", got)
	}
}

func TestBuild_MaterializedMarkerJSONShape(t *testing.T) {
	cfg := baseConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "orders", Columns: []parser.Column{{Name: "id", Type: "bigint", PrimaryKey: true}}},
		},
		Views: []parser.View{
			{
				Name:         "order_summary",
				Materialized: true,
				Columns:      []parser.Column{{Name: "customer_id", Type: "bigint"}},
			},
			{
				Name:    "active_summary",
				Columns: []parser.Column{{Name: "id", Type: "bigint"}},
			},
		},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	mv, err := json.Marshal(findEntity(t, doc, "OrderSummary"))
	if err != nil {
		t.Fatalf("marshal matview entity: %v", err)
	}
	if !strings.Contains(string(mv), `"materialized":true`) {
		t.Errorf("matview entity JSON missing materialized marker: %s", mv)
	}

	regular, err := json.Marshal(findEntity(t, doc, "ActiveSummary"))
	if err != nil {
		t.Fatalf("marshal regular view entity: %v", err)
	}
	if strings.Contains(string(regular), "materialized") {
		t.Errorf("regular view entity JSON must omit the materialized key (omitempty): %s", regular)
	}
}

// The manifest's published enum names must be the identifiers the models
// package actually declares. buildEnums used to re-derive them with
// pascal(e.Name), which diverges from gen.StructName on three axes —
// singularization, the cross-schema collision prefix, and an
// `enums.<name>.struct_name` override (PRD §4.11) — so the manifest advertised
// a go_type no generated file declared.
//
// No example schema reaches any of the three: all three example enums are
// singular, un-prefixed and un-overridden, which is why the goldens cannot
// cover this.
func TestBuildEnums_namesFollowTheGeneratedGoType(t *testing.T) {
	cfg := baseConfig()
	cfg.Enums = map[string]config.EnumConfig{
		"shipping_state": {StructName: "Fulfilment"},
	}
	schema := &parser.Schema{
		Enums: []parser.Enum{
			// Plural: StructName singularizes to "Status", pascal() does not.
			{Name: "statuses", Values: []string{"draft", "live"}},
			// Overridden: neither derivation reaches "Fulfilment" on its own.
			{Name: "shipping_state", Values: []string{"packed", "shipped"}},
		},
		Tables: []parser.Table{
			{
				Name:    "orders",
				Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}},
			},
		},
	}

	doc, err := manifest.Build(testBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := make(map[string]manifest.Enum, len(doc.Enums))
	for _, e := range doc.Enums {
		got[e.DBType] = e
	}
	if len(got) != 2 {
		t.Fatalf("Build().Enums = %d entries, want 2 — an unset BuildInput.Enums empties the section", len(got))
	}

	for _, tt := range []struct{ dbType, want string }{
		{dbType: "statuses", want: "Status"},
		{dbType: "shipping_state", want: "Fulfilment"},
	} {
		e, ok := got[tt.dbType]
		if !ok {
			t.Errorf("no manifest enum for db_type %q", tt.dbType)
			continue
		}
		if e.Name != tt.want || e.GoType != tt.want || e.GraphQLName != tt.want {
			t.Errorf("enum %q = {name:%q go_type:%q graphql_name:%q}, want all %q",
				tt.dbType, e.Name, e.GoType, e.GraphQLName, tt.want)
		}
		if want := tt.want + "Slice"; e.SliceType != want {
			t.Errorf("enum %q slice_type = %q, want %q", tt.dbType, e.SliceType, want)
		}
	}
}

// TestBuildColumns_ComparatorMirrorsFilterFields pins the published
// `columns[].comparator` to the entity's own filter surface.
//
// The builder used to classify columns itself, from the Go type alone and with
// no dialect. That second derivation had drifted: an `interval` column was
// reported as `comparator.Time` when the generated filter declares
// `Number[time.Duration]`, every nullable column got the non-nullable
// spelling, slices lost their element type, a postgres `jsonb` column was
// reported as `JSON` rather than `JSONB`, and enum / `bytea` / `inet` /
// `numeric` columns were reported as nothing at all. Reading FilterFields
// makes the field mean what its name says, and makes it agree with
// `filter.fields` by construction.
//
// The `jsonb[]` row is the load-bearing one: a column with no filter field
// must publish no comparator, where the old classifier answered
// `comparator.Slice` for it.
func TestBuildColumns_ComparatorMirrorsFilterFields(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{{
			Name: "probes",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "label", Type: "text"},
				{Name: "note", Type: "text", Nullable: true},
				{Name: "size", Type: "integer", Nullable: true},
				{Name: "dur", Type: "interval"},
				{Name: "doc", Type: "jsonb"},
				{Name: "payload", Type: "bytea"},
				{Name: "addr", Type: "inet"},
				{Name: "tags", Type: "text[]"},
				{Name: "blobs", Type: "jsonb[]"},
			},
		}},
	}

	doc, err := manifest.Build(testBuildInput(t, schema, baseConfig()))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	probe := findEntity(t, doc, "Probe")

	want := map[string]string{
		"id":      "comparator.ID",
		"label":   "comparator.String",
		"note":    "comparator.NullableString",
		"size":    "comparator.NullableNumber[int32]",
		"dur":     "comparator.Number[time.Duration]",
		"doc":     "comparator.JSONB",
		"payload": "comparator.Opaque[[]byte]",
		"addr":    "comparator.Opaque[net.IP]",
		"tags":    "comparator.Slice[string]",
		"blobs":   "", // non-filterable: no filter field, no published comparator
	}

	got := make(map[string]string, len(probe.Columns))
	for _, c := range probe.Columns {
		got[c.Name] = c.Comparator
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("published column comparators mismatch (-want +got):\n%s", diff)
	}

	// And the two surfaces agree: every column carrying a comparator has a
	// filter field spelling the same family, and the non-filterable one has
	// neither. This is the invariant the single derivation buys.
	filterTypes := make(map[string]string, len(probe.Filter.Fields))
	for _, f := range probe.Filter.Fields {
		filterTypes[f.Name] = strings.TrimPrefix(f.Type, "*")
	}
	for _, c := range probe.Columns {
		ft, inFilter := filterTypes[c.GoField]
		if (c.Comparator != "") != inFilter {
			t.Errorf("column %q: comparator=%q but filter.fields presence=%v", c.Name, c.Comparator, inFilter)
			continue
		}
		if inFilter && ft != c.Comparator {
			t.Errorf("column %q: comparator=%q, filter.fields type=%q", c.Name, c.Comparator, ft)
		}
	}
}

// --- methods[].errors reachability ---

// TestBuild_MethodErrors_MatchTemplateGuards pins the error lists against the
// guards the templates actually carry. The drift test next door proves the
// method *names* match the generated client; this proves each entry advertises
// the sentinels that method can return and no others.
//
// Three shapes are in play and the defect confused all three: Get omitted the
// ErrNotFound it does return, the by-PK deletes claimed one they cannot, and
// Update's was unconditional though the template gates it on strict_updates.
func TestBuild_MethodErrors_MatchTemplateGuards(t *testing.T) {
	softDeleted := []parser.Column{
		{Name: "id", Type: "uuid", PrimaryKey: true},
		{Name: "views", Type: "bigint"},
		{Name: "deleted_at", Type: "timestamptz", Nullable: true},
	}

	tests := []struct {
		name          string
		strictUpdates bool
		want          map[string][]string
	}{
		{
			name:          "strict updates on",
			strictUpdates: true,
			want: map[string][]string{
				"Get":         {"ErrNotFound"},
				"GetMany":     {},
				"Exists":      {},
				"ExistsWhere": {},
				"Count":       {},
				"Connection":  {"ErrInvalidCursor"},
				// §9.4c: every write taking a pointer input can reject a nil one.
				"Create":     {"ErrNilInput", "ErrConstraintViolation"},
				"CreateMany": {"ErrNilInput", "ErrConstraintViolation"},
				"Update":     {"ErrNotFound", "ErrNilInput", "ErrConstraintViolation"},
				// UpdateMany issues Update's statement per item but never
				// inspects RowsAffected, so strict_updates does not reach it —
				// table/update.go.tmpl guards ErrNotFound inside Update alone.
				"UpdateMany":  {"ErrNilInput", "ErrConstraintViolation"},
				"UpdateWhere": {"ErrNilInput", "ErrEmptyFilter", "ErrConstraintViolation"},
				"Increment":   {"ErrNotFound"},
				// Every delete and restore form is idempotent on a missing key.
				"SoftDelete":      {},
				"SoftDeleteMany":  {},
				"SoftDeleteWhere": {"ErrEmptyFilter"},
				"Restore":         {},
				"RestoreMany":     {},
				"RestoreWhere":    {"ErrEmptyFilter"},
				"HardDelete":      {},
				"HardDeleteMany":  {},
				"HardDeleteWhere": {"ErrEmptyFilter"},
			},
		},
		{
			// strict_updates off makes Update and Increment idempotent on a
			// missing PK (PRD §9.5), so neither advertises ErrNotFound. Get is
			// unaffected — its ErrNotFound is not gated on the flag.
			name:          "strict updates off",
			strictUpdates: false,
			want: map[string][]string{
				"Get":    {"ErrNotFound"},
				"Update": {"ErrNilInput", "ErrConstraintViolation"},
				// UpdateMany reads the same with the flag on — it never carried
				// ErrNotFound either way.
				"UpdateMany": {"ErrNilInput", "ErrConstraintViolation"},
				"Increment":  {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Generation.StrictUpdates = new(tt.strictUpdates)
			doc, err := manifest.Build(testBuildInput(t, &parser.Schema{
				Tables: []parser.Table{{Name: "posts", Columns: softDeleted}},
			}, cfg))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			assertMethodErrors(t, findEntity(t, doc, "Post"), tt.want)
		})
	}
}

// TestBuild_MethodErrors_Tenancy covers the tenancy half. increment.go.tmpl
// guards ErrMismatch on `and .Tenancy.InPrimaryKey .CompositePK` with no else
// branch — the by-PK-delete shape, not the create/update/upsert one — so a
// tenanted single-PK entity's Increment reaches ErrMissing alone.
func TestBuild_MethodErrors_Tenancy(t *testing.T) {
	cfg := baseConfig()
	cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id"}
	doc, err := manifest.Build(testBuildInput(t, &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "counters",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "workspace_id", Type: "uuid"},
					{Name: "hits", Type: "bigint"},
				},
			},
		},
	}, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	assertMethodErrors(t, findEntity(t, doc, "Counter"), map[string][]string{
		// Input-carrying mutations compare the caller's tenant in both shapes.
		"Create": {"ErrNilInput", "ErrConstraintViolation", "tenancy.ErrMissing", "tenancy.ErrMismatch"},
		"Update": {"ErrNotFound", "ErrNilInput", "ErrConstraintViolation", "tenancy.ErrMissing", "tenancy.ErrMismatch"},
		"Upsert": {"ErrNilInput", "ErrConstraintViolation", "tenancy.ErrMissing", "tenancy.ErrMismatch"},
		// By-PK methods reach ErrMismatch only in the §29.7 composite shape,
		// which this single-PK table is not.
		"Get":        {"ErrNotFound", "tenancy.ErrMissing"},
		"Exists":     {"tenancy.ErrMissing"},
		"Increment":  {"ErrNotFound", "tenancy.ErrMissing"},
		"HardDelete": {"tenancy.ErrMissing"},
		// Filter-shaped methods compare nothing.
		"GetMany":         {"tenancy.ErrMissing"},
		"Count":           {"tenancy.ErrMissing"},
		"HardDeleteWhere": {"ErrEmptyFilter", "tenancy.ErrMissing"},
		// UpdateWhere carries an UpdateInput and the tenant is not in the key,
		// so its input-side comparison is live (table/update.go.tmpl:719).
		"UpdateWhere": {"ErrNilInput", "ErrEmptyFilter", "ErrConstraintViolation", "tenancy.ErrMissing", "tenancy.ErrMismatch"},
	})
}

func assertMethodErrors(t *testing.T, e manifest.Entity, want map[string][]string) {
	t.Helper()
	byName := map[string]manifest.Method{}
	for _, m := range append(e.Methods.Query, e.Methods.Mutation...) {
		byName[m.Name] = m
	}
	for method, wantErrs := range want {
		m, ok := byName[method]
		if !ok {
			t.Errorf("method %q missing; got %v", method, methodNames(byName))
			continue
		}
		if diff := cmp.Diff(wantErrs, m.Errors); diff != "" {
			t.Errorf("%s errors mismatch (-want +got):\n%s", method, diff)
		}
	}
}

// tenantedViewSchema returns a schema with one table and one view that both
// project the tenant column. pkTenant marks the view's tenant column with the
// @pk annotation the parser surfaces as Column.PrimaryKey.
func tenantedViewSchema(pkTenant bool) *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "documents",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "workspace_id", Type: "uuid"},
					{Name: "title", Type: "text"},
				},
			},
		},
		Views: []parser.View{
			{
				Name: "document_stats",
				SQL:  "SELECT workspace_id, count(*) AS document_count FROM documents GROUP BY workspace_id",
				Columns: []parser.Column{
					{Name: "workspace_id", Type: "uuid", PrimaryKey: pkTenant},
					{Name: "document_count", Type: "bigint"},
				},
			},
		},
	}
}

// TestBuild_TenantedView pins PRD §30.4.2: a tenanted view reports
// features.tenancy with mode "auto-filter" and no mismatch_error. Reporting
// null here would tell every manifest consumer that a scoped view's rows are
// globally visible.
func TestBuild_TenantedView(t *testing.T) {
	cfg := baseConfig()
	cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id"}

	doc, err := manifest.Build(testBuildInput(t, tenantedViewSchema(false), cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "DocumentStat")
	if e.Kind != "view" {
		t.Fatalf("kind = %q, want view", e.Kind)
	}
	if e.Features.Tenancy == nil {
		t.Fatalf("features.tenancy = nil, want populated")
	}
	want := manifest.TenancyFeature{
		Column:               "workspace_id",
		Mode:                 "auto-filter",
		MissingResolverError: "tenancy.ErrMissing",
	}
	if diff := cmp.Diff(want, *e.Features.Tenancy); diff != "" {
		t.Errorf("features.tenancy mismatch (-want +got):\n%s", diff)
	}
	// A view has no write path, no events, and no soft-delete column.
	if e.Features.Events != nil || e.Features.SoftDelete != nil || e.Features.Cache != nil {
		t.Errorf("view features = %+v, want events/soft_delete/cache all nil", e.Features)
	}
}

// TestBuild_TenantedViewPKColumnStaysAutoFilter is the regression this shape
// exists to prevent: @pk on a view selects a Get signature, it does not make
// the column a DDL primary key, so the §29.7 "verify-match" mode — a mutation
// rule with no read-only analogue — must not leak in.
func TestBuild_TenantedViewPKColumnStaysAutoFilter(t *testing.T) {
	cfg := baseConfig()
	cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id"}

	doc, err := manifest.Build(testBuildInput(t, tenantedViewSchema(true), cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "DocumentStat")
	if e.PK.Kind != "single" {
		t.Fatalf("view pk.kind = %q, want single (the @pk annotation is present)", e.PK.Kind)
	}
	if e.Features.Tenancy == nil {
		t.Fatalf("features.tenancy = nil, want populated")
	}
	if got := e.Features.Tenancy.Mode; got != "auto-filter" {
		t.Errorf("tenancy.mode = %q, want auto-filter", got)
	}
	if got := e.Features.Tenancy.MismatchError; got != "" {
		t.Errorf("tenancy.mismatch_error = %q, want empty on a read-only entity", got)
	}
}

// TestBuild_SharedViewHasNoTenancy covers the three ways a view is not scoped.
func TestBuild_SharedViewHasNoTenancy(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.RootConfig)
		schema *parser.Schema
	}{
		{
			name:   "tenancy globally disabled",
			mutate: func(*config.RootConfig) {},
			schema: tenantedViewSchema(false),
		},
		{
			name: "view opted out",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id"}
				cfg.Views["document_stats"] = config.ViewConfig{Tenancy: &config.TableTenancyConfig{Enabled: new(false)}}
			},
			schema: tenantedViewSchema(false),
		},
		{
			name: "view does not project the tenant column",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id"}
			},
			schema: &parser.Schema{
				Tables: []parser.Table{
					{Name: "documents", Columns: []parser.Column{
						{Name: "id", Type: "uuid", PrimaryKey: true},
						{Name: "workspace_id", Type: "uuid"},
					}},
				},
				Views: []parser.View{
					{
						Name:    "document_stats",
						SQL:     "SELECT count(*) AS document_count FROM documents",
						Columns: []parser.Column{{Name: "document_count", Type: "bigint"}},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			tt.mutate(cfg)
			doc, err := manifest.Build(testBuildInput(t, tt.schema, cfg))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			e := findEntity(t, doc, "DocumentStat")
			if e.Features.Tenancy != nil {
				t.Errorf("features.tenancy = %+v, want nil", e.Features.Tenancy)
			}
		})
	}
}

// TestBuild_TenantedViewJSONOmitsMismatchError asserts omitempty is doing the
// work: the key must be absent from the marshalled view entity, not present
// with an empty string. A tenanted table still carries it.
func TestBuild_TenantedViewJSONOmitsMismatchError(t *testing.T) {
	cfg := baseConfig()
	cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id"}

	doc, err := manifest.Build(testBuildInput(t, tenantedViewSchema(false), cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for _, tt := range []struct {
		entity           string
		wantMismatchKey  bool
		wantMismatchText string
	}{
		{"DocumentStat", false, ""},
		{"Document", true, "tenancy.ErrMismatch"},
	} {
		t.Run(tt.entity, func(t *testing.T) {
			e := findEntity(t, doc, tt.entity)
			data, err := json.Marshal(e.Features.Tenancy)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got, ok := decoded["mismatch_error"]
			if ok != tt.wantMismatchKey {
				t.Fatalf("mismatch_error present = %v, want %v (json: %s)", ok, tt.wantMismatchKey, data)
			}
			if tt.wantMismatchKey && got != tt.wantMismatchText {
				t.Errorf("mismatch_error = %v, want %q", got, tt.wantMismatchText)
			}
		})
	}
}

// TestBuild_TenantedViewMethodErrors pins PRD §30.4.2's "errors[] lists the
// sentinels the generated body can actually reach" for a view. Every view
// read resolves the tenant, so under required:true all five carry
// tenancy.ErrMissing — and none carries ErrMismatch, which is a mutation rule.
func TestBuild_TenantedViewMethodErrors(t *testing.T) {
	tests := []struct {
		name     string
		required *bool
		want     map[string][]string
	}{
		{
			name:     "required true — every read can fail closed",
			required: nil, // global default is true
			want: map[string][]string{
				"Get":        {"ErrNotFound", "tenancy.ErrMissing"},
				"GetMany":    {"tenancy.ErrMissing"},
				"Count":      {"tenancy.ErrMissing"},
				"Paginate":   {"tenancy.ErrMissing"},
				"Connection": {"ErrInvalidCursor", "tenancy.ErrMissing"},
			},
		},
		{
			name:     "required false — a zero tenant falls open, so nothing is reachable",
			required: new(false),
			want: map[string][]string{
				"Get":        {"ErrNotFound"},
				"GetMany":    {},
				"Count":      {},
				"Paginate":   {},
				"Connection": {"ErrInvalidCursor"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			// The view projects no id, so Connection is emitted only when the
			// cursor keys resolve against its own columns (PRD §4.13).
			cfg.Generation.CursorKeys = []string{"workspace_id"}
			cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: new(true)}
			if tt.required != nil {
				cfg.Views["document_stats"] = config.ViewConfig{Tenancy: &config.TableTenancyConfig{Required: tt.required}}
			}
			// @pk on the tenant column so Get and Connection are both emitted.
			doc, err := manifest.Build(testBuildInput(t, tenantedViewSchema(true), cfg))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			e := findEntity(t, doc, "DocumentStat")

			got := make(map[string][]string, len(e.Methods.Query))
			for _, m := range e.Methods.Query {
				got[m.Name] = m.Errors
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("view query method errors mismatch (-want +got):\n%s", diff)
			}
			for _, m := range e.Methods.Query {
				if slices.Contains(m.Errors, "tenancy.ErrMismatch") {
					t.Errorf("%s advertises tenancy.ErrMismatch; the §29.7 verify-match has no read-only analogue", m.Name)
				}
			}
		})
	}
}

// TestBuild_ViewTenancyRespectsPerViewOverrides covers findViewConfig's two
// resolution branches — the schema-qualified key and the bare one — and the
// per-view column override, none of which the detection tests exercise.
func TestBuild_ViewTenancyRespectsPerViewOverrides(t *testing.T) {
	// The view projects org_id, not the global workspace_id, so it is scoped
	// only if the per-view column override is resolved.
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "documents", Schema: "public", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "workspace_id", Type: "uuid"},
			}},
		},
		Views: []parser.View{
			{
				Name:   "legacy_rollup",
				Schema: "public",
				SQL:    "SELECT org_id, count(*) AS n FROM documents GROUP BY org_id",
				Columns: []parser.Column{
					{Name: "org_id", Type: "uuid"},
					{Name: "n", Type: "bigint"},
				},
			},
		},
	}

	tests := []struct {
		name       string
		key        string
		wantColumn string
	}{
		{"schema-qualified key", "public.legacy_rollup", "org_id"},
		{"bare key", "legacy_rollup", "org_id"},
		{"no matching key — global column is not projected", "other_view", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id"}
			cfg.Views[tt.key] = config.ViewConfig{Tenancy: &config.TableTenancyConfig{Column: new("org_id")}}

			doc, err := manifest.Build(testBuildInput(t, schema, cfg))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			e := findEntity(t, doc, "LegacyRollup")
			if tt.wantColumn == "" {
				if e.Features.Tenancy != nil {
					t.Errorf("features.tenancy = %+v, want nil", e.Features.Tenancy)
				}
				return
			}
			if e.Features.Tenancy == nil {
				t.Fatalf("features.tenancy = nil, want column %q", tt.wantColumn)
			}
			if got := e.Features.Tenancy.Column; got != tt.wantColumn {
				t.Errorf("tenancy.column = %q, want %q", got, tt.wantColumn)
			}
		})
	}
}

// TestBuild_EntityFilesFollowOutputLayout is the unit half of the files[]
// layout regression cover (the golden-tree half is TestEntityFilesExistOnDisk).
//
// files[] used to be spelled `<snake>_gen.go` unconditionally — the
// file_per_table name — so under the default single_file layout it advertised
// a path gen never wrote. The unset case is not padding: baseConfig leaves
// Output.Layout empty, config defaulting fills it in the real pipeline, and a
// hand-built config reaching Build must still resolve to the single-file name
// rather than a third answer.
func TestBuild_EntityFilesFollowOutputLayout(t *testing.T) {
	tests := []struct {
		name          string
		layout        config.Layout
		wantTableFile string
		wantViewFile  string
	}{
		{
			name:          "single_file concatenates into models_gen.go and views_gen.go",
			layout:        config.LayoutSingleFile,
			wantTableFile: "models_gen.go",
			wantViewFile:  "views_gen.go",
		},
		{
			name:          "file_per_table gives each entity its own snake-cased file",
			layout:        config.LayoutFilePerTable,
			wantTableFile: "product_gen.go",
			wantViewFile:  "product_summary_gen.go",
		},
		{
			name:          "unset layout resolves to the single_file default",
			layout:        "",
			wantTableFile: "models_gen.go",
			wantViewFile:  "views_gen.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Output.Layout = tt.layout
			schema := &parser.Schema{
				Tables: []parser.Table{
					{
						Name:    "products",
						Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "name", Type: "text"}},
					},
				},
				Views: []parser.View{
					{
						Name: "product_summaries",
						SQL:  "SELECT id, name FROM products",
						Columns: []parser.Column{
							{Name: "id", Type: "uuid"},
							{Name: "name", Type: "text"},
						},
					},
				},
			}
			doc, err := manifest.Build(testBuildInput(t, schema, cfg))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}

			table := findEntity(t, doc, "Product")
			if diff := cmp.Diff([]string{tt.wantTableFile}, table.Files); diff != "" {
				t.Errorf("Build(layout=%q) table files mismatch (-want +got):\n%s", tt.layout, diff)
			}
			view := findEntity(t, doc, "ProductSummary")
			if diff := cmp.Diff([]string{tt.wantViewFile}, view.Files); diff != "" {
				t.Errorf("Build(layout=%q) view files mismatch (-want +got):\n%s", tt.layout, diff)
			}

			// file_prefix is layout-independent — it names the per-entity
			// markdown / JSON artifact, not the Go file (PRD §30 "File
			// naming"). A layout-aware files[] must not drag it.
			if table.FilePrefix != "product" {
				t.Errorf("Build(layout=%q) table file_prefix = %q, want %q", tt.layout, table.FilePrefix, "product")
			}
			if view.FilePrefix != "product_summary" {
				t.Errorf("Build(layout=%q) view file_prefix = %q, want %q", tt.layout, view.FilePrefix, "product_summary")
			}
		})
	}
}
