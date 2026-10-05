package gen_test

import (
	goparser "go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

// Go `CreateWithRelated` (PRD §9.9, §29.4, §32.5).
//
// The eligibility matrix (§9.9.3) is the normative artifact this feature
// implements, so the tests below assert against it cell by cell rather than
// against the rendered text wherever they can: which edges carry a nested
// member at all, and which verbs each member admits. The rendered-text
// assertions are reserved for the facts a context field cannot express — the
// discriminator elision, the elided FK, the `Limit: new(0)` on the visibility
// read.

// --- fixture ---

// nestedSchema is the smallest schema carrying all four §9.9.1 write shapes
// plus the two negatives. `users` is the parent throughout:
//
//   - Events     — O2M on a NULLABLE FK: create + connect (§9.9.3 row 1)
//   - Orders     — O2M on a NOT NULL FK: create only (row 2)
//   - Categories — M2M through a pure link table (row 3)
//   - Profile    — has-one on a NOT NULL FK: create only (shape 2)
//   - Bio        — has-one on a NULLABLE FK: create + connect, the one shape
//     that renders the pointer `Connect` branch
//   - Blobs      — O2M whose target has a `bytea` primary key: `create`
//     survives, `connect` cannot be rendered and says so
//   - Tagged     — O2M made write-eligible by `discriminator:`
//   - Archived   — O2M carrying a `filter:`, which makes it ineligible
//   - NamedTags  — M2M carrying a `discriminator:`, which also makes it
//     ineligible: §9.9.5 and setting the discriminator on `create` cannot
//     both hold on that shape
//
// `metrics` carries no relationship at all and is the no-eligible-edge
// fixture.
func nestedSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users", Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "email", Type: "text"},
				},
			},
			{
				Name: "events", Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid", Nullable: true},
					{Name: "action", Type: "text"},
				},
			},
			{
				Name: "orders", Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid"},
					{Name: "notes", Type: "text", Nullable: true},
				},
			},
			{
				Name: "profiles", Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid"},
					{Name: "bio", Type: "text", Nullable: true},
				},
			},
			{
				// The has-one shape with a NULLABLE traversed FK — the only
				// combination that renders the pointer `Connect` branch. No
				// example schema has one, so without it that branch is emitted
				// by nothing and checked by nothing.
				Name: "bios", Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid", Nullable: true},
					{Name: "text", Type: "text"},
				},
			},
			{
				Name: "categories", Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
					{Name: "name", Type: "text"},
				},
			},
			{
				Name: "user_categories", Schema: "public",
				Columns: []parser.Column{
					{Name: "user_id", Type: "uuid", PrimaryKey: true},
					{Name: "category_id", Type: "bigint", PrimaryKey: true},
				},
			},
			{
				// A junction carrying a soft-delete column. No example schema
				// has one, and neither mechanism is correct on it: a hard
				// delete destroys a row the schema marked soft-deletable, and a
				// soft delete leaves the link visible because the M2M loader's
				// junction query carries no soft-delete predicate. `create` and
				// `connect` are unaffected.
				Name: "user_tags", Schema: "public",
				Columns: []parser.Column{
					{Name: "user_id", Type: "uuid", PrimaryKey: true},
					{Name: "tag_id", Type: "bigint", PrimaryKey: true},
					{Name: "deleted_at", Type: "timestamptz", Nullable: true},
				},
			},
			{
				// A `bytea` primary key resolves to []byte, which filters
				// through comparator.Opaque and is not a valid Go map key — so
				// the edge below keeps `create` and loses `connect` alone.
				Name: "blobs", Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bytea", PrimaryKey: true},
					{Name: "user_id", Type: "uuid", Nullable: true},
					{Name: "data", Type: "bytea"},
				},
			},
			{
				Name: "metrics", Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "value", Type: "bigint"},
				},
			},
		},
	}
}

func nestedUserRelationships() []config.TableRelationship {
	return []config.TableRelationship{
		{Name: "Events", Type: "one_to_many", Table: "events", FK: "user_id"},
		{Name: "Orders", Type: "one_to_many", Table: "orders", FK: "user_id"},
		{Name: "Profile", Type: "one_to_one", Table: "profiles", FK: "user_id"},
		{Name: "Bio", Type: "one_to_one", Table: "bios", FK: "user_id"},
		{Name: "Blobs", Type: "one_to_many", Table: "blobs", FK: "user_id"},
		{
			Name: "Categories", Type: "many_to_many", Table: "categories",
			Junction: "user_categories", JunctionLocalFK: "user_id", JunctionReferenceFK: "category_id",
		},
		{
			Name: "Tagged", Type: "one_to_many", Table: "events", FK: "user_id",
			Discriminator: &config.RelationshipDiscriminator{Column: "action", Value: "signup"},
		},
		{Name: "Archived", Type: "one_to_many", Table: "events", FK: "user_id", Filter: "action = 'archived'"},
		{
			Name: "NamedTags", Type: "many_to_many", Table: "categories",
			Junction: "user_categories", JunctionLocalFK: "user_id", JunctionReferenceFK: "category_id",
			Discriminator: &config.RelationshipDiscriminator{Column: "name", Value: "tag"},
		},
		{
			Name: "SoftTags", Type: "many_to_many", Table: "categories",
			Junction: "user_tags", JunctionLocalFK: "user_id", JunctionReferenceFK: "tag_id",
		},
	}
}

// buildNestedContexts runs the production context pipeline over nestedSchema
// with nested mutations enabled, applying mutate to the config first.
//
// It goes through BuildTableContextsFromSchema rather than hand-building a
// TableContext because nested eligibility reads the *target's* resolved
// operations mask, create input, filter members and conflict targets — a
// hand-built fixture would be asserting against its own assumptions.
func buildNestedContexts(t *testing.T, mutate func(*config.RootConfig)) map[string]gen.TableContext {
	t.Helper()
	input := testInput(nestedSchema())
	cfg := input.Config
	cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
	cfg.Tables["users"] = config.TableConfig{Relationships: nestedUserRelationships()}
	if mutate != nil {
		mutate(cfg)
	}

	contexts, err := gen.BuildTableContextsFromSchema(input.Schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	byName := make(map[string]gen.TableContext, len(contexts))
	for _, tc := range contexts {
		byName[tc.TableName] = tc
	}
	return byName
}

func nestedEdge(t *testing.T, tc gen.TableContext, field string) gen.NestedEdgeContext {
	t.Helper()
	if tc.Nested == nil {
		t.Fatalf("table %q carries no nested surface", tc.TableName)
	}
	for _, e := range tc.Nested.Edges {
		if e.FieldName == field {
			return e
		}
	}
	t.Fatalf("table %q has no nested edge %q (edges: %v)", tc.TableName, field, nestedEdgeNames(tc))
	return gen.NestedEdgeContext{}
}

func nestedEdgeNames(tc gen.TableContext) []string {
	if tc.Nested == nil {
		return nil
	}
	names := make([]string, 0, len(tc.Nested.Edges))
	for _, e := range tc.Nested.Edges {
		names = append(names, e.FieldName)
	}
	return names
}

// --- the eligibility matrix ---

// TestNestedEligibility_VerbSetPerShape pins PRD §9.9.3 cell by cell. The verb
// set is computed from the FK's nullability and the edge's shape, never from
// the relationship type: Events and Orders are the same `one_to_many` type and
// admit different verbs.
func TestNestedEligibility_VerbSetPerShape(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]

	type verbs struct{ Create, Connect bool }
	tests := []struct {
		edge  string
		shape string
		want  verbs
	}{
		{edge: "Bio", shape: "has_one", want: verbs{Create: true, Connect: true}},
		{edge: "Blobs", shape: "o2m", want: verbs{Create: true}},
		{edge: "Categories", shape: "m2m", want: verbs{Create: true, Connect: true}},
		{edge: "Events", shape: "o2m", want: verbs{Create: true, Connect: true}},
		{edge: "Orders", shape: "o2m", want: verbs{Create: true}},
		{edge: "Profile", shape: "has_one", want: verbs{Create: true}},
		{edge: "SoftTags", shape: "m2m", want: verbs{Create: true, Connect: true}},
		{edge: "Tagged", shape: "o2m", want: verbs{Create: true, Connect: true}},
	}
	for _, tt := range tests {
		t.Run(tt.edge, func(t *testing.T) {
			e := nestedEdge(t, users, tt.edge)
			if e.Shape != tt.shape {
				t.Errorf("Shape = %q, want %q", e.Shape, tt.shape)
			}
			got := verbs{Create: e.HasCreate, Connect: e.HasConnect}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("verb set mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestNestedEligibility_EdgesAreAlphabetical pins the §9.9.5 ordering. It is
// contractual in three places — error attribution, event enqueue order, and the
// order of the generated members — and it is spelled by the relationship's Go
// field name, the same string §9.9.8 attributes errors with.
func TestNestedEligibility_EdgesAreAlphabetical(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	want := []string{"Bio", "Blobs", "Categories", "Events", "Orders", "Profile", "SoftTags", "Tagged"}
	if diff := cmp.Diff(want, nestedEdgeNames(users)); diff != "" {
		t.Errorf("edge order mismatch (-want +got):\n%s", diff)
	}
}

// TestNestedEligibility_FilterEdgeEmitsNoMember pins that a `filter:` edge
// carries no nested member. Auto-included, so it is silently omitted rather
// than a build failure.
func TestNestedEligibility_FilterEdgeEmitsNoMember(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	for _, name := range nestedEdgeNames(users) {
		if name == "Archived" {
			t.Fatalf("`filter:` edge Archived is write-eligible; PRD §9.9.4 forbids it (edges: %v)", nestedEdgeNames(users))
		}
	}
}

// TestNestedEligibility_M2MDiscriminatorIsNotWriteEligible pins the one shape
// where two PRD rules contradict each other, and pins the fail-closed answer.
//
// §9.9.5 makes an M2M `create` take `Create<Target>Input` unchanged — there is
// no traversed FK to elide — while §13.4.1 rule 1 makes a nested create SET the
// discriminator column. Emitting the edge under §9.9.5 alone inserts a target
// the edge's own loader can never return and then links it, which is the silent
// mislink the visibility read's discriminator predicate closes on the connect
// side. The read path supports the shape, so only the write surface is refused.
func TestNestedEligibility_M2MDiscriminatorIsNotWriteEligible(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	for _, name := range nestedEdgeNames(users) {
		if name == "NamedTags" {
			t.Fatalf("an M2M edge with a `discriminator:` is write-eligible (edges: %v); its nested create would silently insert a target this edge cannot read back", nestedEdgeNames(users))
		}
	}

	err := nestedEligibilityError(t, func(cfg *config.RootConfig) {
		tc := cfg.Tables["users"]
		tc.NestedMutations = &config.TableNestedMutationsConfig{
			Relationships: []config.TableNestedRelationship{{Name: "NamedTags"}},
		}
		cfg.Tables["users"] = tc
	})
	if err == nil {
		t.Fatal("ValidateNestedWriteEligibility() = nil, want an error naming NamedTags")
	}
	for _, want := range []string{"NamedTags", "many-to-many", "discriminator"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestNestedEligibility_DiscriminatorTheCreateCannotSetIsIneligible pins the
// discriminator binding rule (PRD §9.9.4, §13.4.1 rule 1). A nested `create`
// sets the discriminator column from the declared value, an untyped Go
// constant, and a constant is assignable only to a type whose underlying type
// is a basic type of its own kind. A column bound to anything else used to keep
// `create` and emit `Tag: "pinned"` into a `types.JSON` field, which does not
// compile. Such an edge is not write-eligible: omitted when auto-included,
// reported when listed.
//
// Some of those columns cannot be read through either: PRD §13.4.1
// "Validation" refuses them before any edge is resolved. For those the
// auto-included half checks that refusal instead, since no nested surface is
// built, and the listed half still pins the rule on the eligibility lint.
func TestNestedEligibility_DiscriminatorTheCreateCannotSetIsIneligible(t *testing.T) {
	type fixture struct {
		dialect  config.Dialect
		relType  string
		colType  string
		nullable bool
		typeMap  map[string]string
		value    any
	}
	// build returns the users context the full pipeline attaches, the
	// eligibility lint's error, and the pipeline's own error.
	build := func(t *testing.T, f fixture, listed bool) (users gen.TableContext, lint, pipeline error) {
		t.Helper()
		schema := nestedSchema()
		schema.Enums = append(schema.Enums, parser.Enum{Name: "pin_kind", Schema: "public", Values: []string{"pinned", "other"}})
		schema.Tables = append(schema.Tables, parser.Table{
			Name: "pins", Schema: "public",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "user_id", Type: "uuid"},
				{Name: "tag", Type: f.colType, Nullable: f.nullable},
				{Name: "label", Type: "text"},
			},
		})
		input := testInput(schema)
		cfg := input.Config
		if f.dialect != "" && f.dialect != config.DialectPostgres {
			cfg.Input.Dialect = f.dialect
			cfg.Output.Driver = "stdlib"
			input.Resolver = gotype.NewResolver(f.dialect, true, cfg.Overrides.Types)
		}
		cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
		cfg.Tables["pins"] = config.TableConfig{TypeMap: f.typeMap}
		tc := config.TableConfig{Relationships: append(nestedUserRelationships(), config.TableRelationship{
			Name: "Pinned", Type: f.relType, Table: "pins", FK: "user_id",
			Discriminator: &config.RelationshipDiscriminator{Column: "tag", Value: f.value},
		})}
		if listed {
			tc.NestedMutations = &config.TableNestedMutationsConfig{
				Relationships: []config.TableNestedRelationship{{Name: "Pinned"}},
			}
		}
		cfg.Tables["users"] = tc
		contexts, err := gen.BuildTableContexts(input, nil)
		if err != nil {
			t.Fatalf("BuildTableContexts() error: %v", err)
		}
		if lint = gen.ValidateNestedWriteEligibility(contexts, cfg, input.Resolver); lint != nil {
			return gen.TableContext{}, lint, nil
		}
		attached, err := gen.BuildTableContextsFromSchema(input.Schema, cfg)
		if err != nil {
			return gen.TableContext{}, nil, err
		}
		for _, c := range attached {
			if c.TableName == "users" {
				return c, nil, nil
			}
		}
		t.Fatal("no users context")
		return gen.TableContext{}, nil, nil
	}

	tests := []struct {
		name string
		fixture
		// wantAssign is the rendered assignment on an eligible edge; empty
		// means the edge must be refused.
		wantAssign string
		// readRefused marks a column the read-path validation refuses first.
		readRefused bool
	}{
		{name: "json", relType: "one_to_many", colType: "json", value: "pinned", readRefused: true},
		{name: "nullable jsonb, the omittable form", relType: "one_to_many", colType: "jsonb", nullable: true, value: "pinned", readRefused: true},
		{name: "has-one over json", relType: "one_to_one", colType: "json", value: "primary", readRefused: true},
		{name: "a string on an integer column", relType: "one_to_many", colType: "integer", value: "pinned", readRefused: true},
		{name: "mysql json", dialect: config.DialectMySQL, relType: "one_to_many", colType: "json", value: "pinned"},
		{name: "mysql has-one over json", dialect: config.DialectMySQL, relType: "one_to_one", colType: "json", value: "primary"},
		{name: "uuid", relType: "one_to_many", colType: "uuid", value: "7f1c3a52-3b7e-4c5e-9f61-0c1b2d3e4f50"},
		{name: "has-one over uuid", relType: "one_to_one", colType: "uuid", value: "7f1c3a52-3b7e-4c5e-9f61-0c1b2d3e4f50"},
		{name: "timestamptz", relType: "one_to_many", colType: "timestamptz", value: "2026-01-01T00:00:00Z"},
		{name: "inet", relType: "one_to_many", colType: "inet", value: "10.0.0.1"},
		{name: "enum array", relType: "one_to_many", colType: "pin_kind[]", value: "pinned"},
		{name: "a type_map struct in the same package", relType: "one_to_many", colType: "text", typeMap: map[string]string{"tag": "Address"}, value: "pinned"},
		{name: "text", relType: "one_to_many", colType: "text", value: "pinned", wantAssign: `"pinned"`},
		{name: "nullable text, the omittable form", relType: "one_to_many", colType: "text", nullable: true, value: "pinned", wantAssign: `omittable.Set[*string](new(string("pinned")))`},
		{name: "enum", relType: "one_to_many", colType: "pin_kind", value: "pinned", wantAssign: `"pinned"`},
		{name: "integer", relType: "one_to_many", colType: "integer", value: 3, wantAssign: "3"},
		{name: "boolean", relType: "one_to_many", colType: "boolean", value: true, wantAssign: "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users, lint, pipeline := build(t, tt.fixture, false)
			if lint != nil {
				t.Fatalf("auto-included: ValidateNestedWriteEligibility() = %v, want nil", lint)
			}
			if tt.readRefused {
				if pipeline == nil || !strings.Contains(pipeline.Error(), "discriminator tag") {
					t.Errorf("auto-included: pipeline error = %v, want the read-path refusal of the discriminator", pipeline)
				}
			} else if pipeline != nil {
				t.Fatalf("auto-included: BuildTableContextsFromSchema() = %v, want nil", pipeline)
			}
			var edge *gen.NestedEdgeContext
			if users.Nested != nil {
				for i := range users.Nested.Edges {
					if users.Nested.Edges[i].FieldName == "Pinned" {
						edge = &users.Nested.Edges[i]
					}
				}
			}

			if tt.wantAssign != "" {
				if edge == nil {
					t.Fatalf("Pinned, discriminated on a %q column by %v, is not write-eligible (edges: %v)", tt.colType, tt.value, nestedEdgeNames(users))
				}
				if edge.DiscAssignExpr != tt.wantAssign {
					t.Errorf("Pinned DiscAssignExpr = %q, want %q", edge.DiscAssignExpr, tt.wantAssign)
				}
				if _, lint, pipeline := build(t, tt.fixture, true); lint != nil || pipeline != nil {
					t.Errorf("listed: lint = %v, pipeline = %v, want nil", lint, pipeline)
				}
				return
			}

			if edge != nil {
				t.Errorf("Pinned, discriminated on a %q column by %v, is write-eligible with DiscAssignExpr %q; its nested create does not compile", tt.colType, tt.value, edge.DiscAssignExpr)
			}
			_, lint, _ = build(t, tt.fixture, true)
			if lint == nil {
				t.Fatal("listed: ValidateNestedWriteEligibility() = nil, want the edge reported")
			}
			for _, want := range []string{`"Pinned"`, `"tag"`, "cannot be assigned to", "§9.9.4"} {
				if !strings.Contains(lint.Error(), want) {
					t.Errorf("listed: error %q does not mention %q", lint, want)
				}
			}
		})
	}
}

// TestNestedEligibility_UnrenderableConnectIsReportedNotDropped pins the
// difference between a verb the §4.6 `verbs` mask turned off and a verb the
// emitter cannot render. The mask is the consumer asking for a narrower
// surface and is silent by design; an unrenderable verb is a generator
// limitation the consumer cannot see from the config, so an explicitly-listed
// edge hears about it — and it keeps the verbs that do work rather than going
// away entirely.
//
// `blobs` is the shape: a `bytea` primary key filters through
// comparator.Opaque and is not a valid Go map key, so the visibility read can
// neither filter on it nor bucket its rows by it, while the nested `create`
// over the same edge is unaffected.
func TestNestedEligibility_UnrenderableConnectIsReportedNotDropped(t *testing.T) {
	e := nestedEdge(t, buildNestedContexts(t, nil)["users"], "Blobs")
	if e.HasConnect {
		t.Error("connect survived on a target whose primary key cannot key a map")
	}
	if !e.HasCreate {
		t.Error("create was dropped alongside connect; an unrenderable verb takes only itself")
	}

	err := nestedEligibilityError(t, func(cfg *config.RootConfig) {
		tc := cfg.Tables["users"]
		tc.NestedMutations = &config.TableNestedMutationsConfig{
			Relationships: []config.TableNestedRelationship{{Name: "Blobs"}},
		}
		cfg.Tables["users"] = tc
	})
	if err == nil {
		t.Fatal("ValidateNestedWriteEligibility() = nil, want a diagnostic for the dropped connect")
	}
	for _, want := range []string{"Blobs", "`connect`", "[]byte"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "not write-eligible") {
		t.Errorf("the edge was reported as fully ineligible, but only its connect verb is: %v", err)
	}
}

// TestNestedEligibility_AllowReparentNeedsNoNullGuard pins the second half of
// §9.9.6's re-parenting rule. The adoption deliberately carries no `fk IS NULL`
// term under `allow_reparent`, so the filter member that term would need is not
// required either — and the post-UPDATE shortfall means "stopped being visible"
// rather than "parented in between", so it reports ErrNotFound.
func TestNestedEligibility_AllowReparentNeedsNoNullGuard(t *testing.T) {
	contexts := buildNestedContexts(t, func(cfg *config.RootConfig) {
		tc := cfg.Tables["users"]
		tc.NestedMutations = &config.TableNestedMutationsConfig{
			Relationships: []config.TableNestedRelationship{{Name: "Events", AllowReparent: true}},
		}
		cfg.Tables["users"] = tc
	})
	users := contexts["users"]
	e := nestedEdge(t, users, "Events")
	if !e.AllowReparent || !e.HasConnect {
		t.Fatalf("Events: AllowReparent=%v HasConnect=%v, want both true", e.AllowReparent, e.HasConnect)
	}
	// The FK's filter member itself is still resolved — `disconnect`, `clear`
	// and the adoption's verify read scope themselves with it whatever
	// allow_reparent says. What the flag removes is the `IS NULL` term the
	// adoption would run under.
	if e.FKFilterNullExpr != "" {
		t.Errorf("FKFilterNullExpr = %q, want empty — the adoption carries no IS NULL guard under allow_reparent", e.FKFilterNullExpr)
	}

	out := renderNested(t, users)
	if strings.Contains(out, "UserID: &comparator.NullableID{Null: new(true)}") {
		t.Error("the adoption UPDATE still carries the `fk IS NULL` guard under allow_reparent")
	}
	if strings.Contains(out, "ErrAlreadyRelated") {
		t.Error("the re-parenting branch still reports ErrAlreadyRelated; with no IS NULL term the only remaining cause is a row that stopped being visible")
	}
	if !strings.Contains(out, `nestedError("Events", "connect", nil, ErrNotFound)`) {
		t.Errorf("the post-UPDATE shortfall does not report ErrNotFound under allow_reparent:\n%s", nestedExcerpt(out, "applyUserEventsNested"))
	}
}

// TestNestedEligibility_NoEligibleEdgeEmitsNothing pins that a parent with zero
// eligible edges carries no nested surface at all. A wrapper holding only the
// flat input would be the base operation with extra steps.
func TestNestedEligibility_NoEligibleEdgeEmitsNothing(t *testing.T) {
	contexts := buildNestedContexts(t, nil)
	for _, table := range []string{"bios", "blobs", "metrics", "orders", "profiles", "user_categories"} {
		if tc := contexts[table]; tc.Nested != nil {
			t.Errorf("table %q carries a nested surface with edges %v, want none", table, nestedEdgeNames(tc))
		}
	}
}

// TestNestedEligibility_ListedShapeFailureIsReported pins the listed half of
// §9.9.4 for the two rules nestedCandidateEdges answers before any edge is
// resolved: a belongs-to edge, whose FK sits on the parent, and a parent with a
// composite primary key. Both used to be filtered out ahead of the lint, so a
// listed edge failing either generated nothing and reported nothing — the
// outcome §9.9.4 calls worse than a build failure. The auto-included half must
// stay silent: the same edge, declared but not listed, fails neither path and
// carries no surface.
//
// Both entry points are checked: `sqlgen validate` calls the lint directly,
// and the generate path runs it inside BuildTableContextsFromSchema.
//
// Verified failing-first: without listedShapeFailures, both subtests got a
// nil error.
func TestNestedEligibility_ListedShapeFailureIsReported(t *testing.T) {
	tests := []struct {
		name   string
		table  string
		rel    config.TableRelationship
		reason string
	}{
		{
			name:   "belongs-to edge",
			table:  "profiles",
			rel:    config.TableRelationship{Name: "Owner", Type: "one_to_one", Table: "users", FK: "user_id"},
			reason: "the edge is belongs-to",
		},
		{
			name:   "composite-PK parent",
			table:  "user_categories",
			rel:    config.TableRelationship{Name: "Events", Type: "one_to_many", Table: "events", FK: "user_id"},
			reason: "the parent has a composite primary key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listed := func(cfg *config.RootConfig) {
				cfg.Tables[tt.table] = config.TableConfig{
					Relationships: []config.TableRelationship{tt.rel},
					NestedMutations: &config.TableNestedMutationsConfig{
						Relationships: []config.TableNestedRelationship{{Name: tt.rel.Name}},
					},
				}
			}
			// The lint itself, as `sqlgen validate` calls it.
			lintErr := nestedEligibilityError(t, listed)

			// The generate path, which runs the same lint before rendering.
			input := testInput(nestedSchema())
			input.Config.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
			listed(input.Config)
			_, buildErr := gen.BuildTableContextsFromSchema(input.Schema, input.Config)

			for path, err := range map[string]error{"validate": lintErr, "generate": buildErr} {
				if err == nil {
					t.Errorf("%s: error = nil, want one naming %s", path, tt.rel.Name)
					continue
				}
				for _, want := range []string{tt.table, `"` + tt.rel.Name + `"`, tt.reason, "§9.9.4"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("%s: error %q does not mention %q", path, err, want)
					}
				}
			}

			// Auto-included: the same edge, declared but not listed.
			declared := func(cfg *config.RootConfig) {
				cfg.Tables[tt.table] = config.TableConfig{Relationships: []config.TableRelationship{tt.rel}}
			}
			if err := nestedEligibilityError(t, declared); err != nil {
				t.Errorf("auto-included: ValidateNestedWriteEligibility() = %v, want nil", err)
			}
			if tc := buildNestedContexts(t, declared)[tt.table]; tc.Nested != nil {
				t.Errorf("auto-included: table %q carries a nested surface with edges %v, want none", tt.table, nestedEdgeNames(tc))
			}
		})
	}
}

// TestNestedEligibility_DisabledEmitsNothing pins the §4.6 opt-in: with
// `generation.nested_mutations.enabled` unset, nothing in §9.9 is emitted
// anywhere.
func TestNestedEligibility_DisabledEmitsNothing(t *testing.T) {
	contexts := buildNestedContexts(t, func(cfg *config.RootConfig) {
		cfg.Generation.NestedMutations = nil
	})
	for name, tc := range contexts {
		if tc.Nested != nil {
			t.Errorf("table %q carries a nested surface with the feature off", name)
		}
	}
}

// TestNestedEligibility_JunctionWithoutAConflictTarget pins the junction
// conflict-target rule on a junction whose key is app-enforced through
// `primary_key.columns`. Such a junction emits no conflict-target constant (PRD
// §9.5), and its upsert operations resolve off with it, so the §9.9.6 link step
// has neither a target to pass nor an UpsertMany to call.
func TestNestedEligibility_JunctionWithoutAConflictTarget(t *testing.T) {
	appEnforced := func(cfg *config.RootConfig) {
		cfg.Tables["user_categories"] = config.TableConfig{
			PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"user_id", "category_id"}},
		}
	}
	contexts := buildNestedContexts(t, appEnforced)
	if n := len(contexts["user_categories"].ConflictTargets); n != 0 {
		t.Fatalf("fixture is wrong: the junction still emits %d conflict target(s)", n)
	}
	for _, name := range nestedEdgeNames(contexts["users"]) {
		if name == "Categories" {
			t.Error("M2M edge survived over a junction with no conflict-target constant for the link step to pass")
		}
	}

	err := nestedEligibilityError(t, func(cfg *config.RootConfig) {
		appEnforced(cfg)
		tc := cfg.Tables["users"]
		tc.NestedMutations = &config.TableNestedMutationsConfig{
			Relationships: []config.TableNestedRelationship{{Name: "Categories"}},
		}
		cfg.Tables["users"] = tc
	})
	if err == nil {
		t.Fatal("ValidateNestedWriteEligibility() = nil, want an error naming Categories")
	}
	for _, want := range []string{"conflict-target constant", "§9.9.4"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "upsert_many") {
		t.Errorf("error %q blames `upsert_many`, which the config never disabled", err)
	}
}

// TestNestedEligibility_JunctionPayloadColumnIsIneligible pins the
// pure-junction rule in its strict form: an M2M junction may carry no column
// beyond its two foreign keys, optional ones included.
//
// The optional case is the one that fails silently. The link step upserts on
// the junction's key, and UpsertMany puts every other insert column in the
// update half, so re-linking an existing pair resets the column to its default
// and reports success. Measured on the mysql example: re-connecting a linked
// category took `user_categories.slot` from 7 to NULL, and the call returned
// nil.
//
// Two optional columns are allowed. The soft-delete column is reset on a
// re-link, and resetting it restores the link; `user_tags` covers that half,
// and the soft-delete junction test already requires it to keep `create` and
// `connect`. The tenant column is never in the update half at all (PRD
// §29.4.2), and a tenanted junction carries it as an optional create-input
// field, which is what the third subtest reaches.
//
// Verified failing-first: with the optional-column clause removed from
// resolveNestedJunction, `Categories` survives with a payload column and the
// listed half returns no error; with the tenant clause removed from
// junctionColumnSurvivesRelink, the tenanted junction loses the edge.
func TestNestedEligibility_JunctionPayloadColumnIsIneligible(t *testing.T) {
	withSlot := func() *parser.Schema {
		schema := nestedSchema()
		for i := range schema.Tables {
			if schema.Tables[i].Name == "user_categories" {
				schema.Tables[i].Columns = append(schema.Tables[i].Columns, parser.Column{Name: "slot", Type: "integer", Nullable: true})
			}
		}
		return schema
	}
	cfgFor := func(listed bool) *config.RootConfig {
		cfg := testInput(nestedSchema()).Config
		cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
		tc := config.TableConfig{Relationships: nestedUserRelationships()}
		if listed {
			tc.NestedMutations = &config.TableNestedMutationsConfig{
				Relationships: []config.TableNestedRelationship{{Name: "Categories"}},
			}
		}
		cfg.Tables["users"] = tc
		return cfg
	}

	t.Run("auto-included edge is omitted", func(t *testing.T) {
		contexts, err := gen.BuildTableContextsFromSchema(withSlot(), cfgFor(false))
		if err != nil {
			t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
		}
		for _, tc := range contexts {
			if tc.TableName != "users" {
				continue
			}
			for _, name := range nestedEdgeNames(tc) {
				if name == "Categories" {
					t.Error("Categories survived with a payload column on its junction; the link step would reset `slot` on every re-link")
				}
			}
			// The soft-delete junction is the control: its only extra column
			// is the one a re-link is allowed to reset.
			nestedEdge(t, tc, "SoftTags")
		}
	})

	t.Run("listed edge is a hard error naming the column", func(t *testing.T) {
		input := testInput(withSlot())
		cfg := cfgFor(true)
		input.Config = cfg
		contexts, err := gen.BuildTableContexts(input, nil)
		if err != nil {
			t.Fatalf("BuildTableContexts() error: %v", err)
		}
		err = gen.ValidateNestedWriteEligibility(contexts, cfg, input.Resolver)
		if err == nil {
			t.Fatal("ValidateNestedWriteEligibility() = nil, want a payload-column error for Categories")
		}
		for _, want := range []string{`"Categories"`, "beyond its two foreign keys", `"slot"`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not name %s", err, want)
			}
		}
	})

	t.Run("a tenanted junction keeps the edge", func(t *testing.T) {
		users := nestedContextsWithTenantOn(t, "user_categories")["users"]
		nestedEdge(t, users, "Categories")
	})
}

// TestNestedEligibility_AllowlistNarrowsAndHardErrors pins both halves of
// §9.9.4's lint. An explicitly-listed edge that fails a rule is a hard error —
// an edge the config asked for by name that silently generates nothing is worse
// than a build failure — while an auto-included one is silently omitted so it
// cannot break an unrelated build.
func TestNestedEligibility_AllowlistNarrowsAndHardErrors(t *testing.T) {
	t.Run("allowlist narrows the reachable set", func(t *testing.T) {
		contexts := buildNestedContexts(t, func(cfg *config.RootConfig) {
			tc := cfg.Tables["users"]
			tc.NestedMutations = &config.TableNestedMutationsConfig{
				Relationships: []config.TableNestedRelationship{{Name: "Events"}},
			}
			cfg.Tables["users"] = tc
		})
		if diff := cmp.Diff([]string{"Events"}, nestedEdgeNames(contexts["users"])); diff != "" {
			t.Errorf("allowlisted edge set mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("an explicitly-listed ineligible edge is a hard error", func(t *testing.T) {
		err := nestedEligibilityError(t, func(cfg *config.RootConfig) {
			tc := cfg.Tables["users"]
			tc.NestedMutations = &config.TableNestedMutationsConfig{
				Relationships: []config.TableNestedRelationship{{Name: "Archived"}},
			}
			cfg.Tables["users"] = tc
		})
		if err == nil {
			t.Fatal("ValidateNestedWriteEligibility() = nil, want an error naming Archived")
		}
		for _, want := range []string{"Archived", "§9.9.4", "filter"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
	})

	t.Run("an allowlist entry naming no relationship is a hard error", func(t *testing.T) {
		err := nestedEligibilityError(t, func(cfg *config.RootConfig) {
			tc := cfg.Tables["users"]
			tc.NestedMutations = &config.TableNestedMutationsConfig{
				Relationships: []config.TableNestedRelationship{{Name: "Nonexistent"}},
			}
			cfg.Tables["users"] = tc
		})
		if err == nil || !strings.Contains(err.Error(), "Nonexistent") {
			t.Fatalf("ValidateNestedWriteEligibility() = %v, want an error naming Nonexistent", err)
		}
	})

	t.Run("an auto-included ineligible edge is silent", func(t *testing.T) {
		if err := nestedEligibilityError(t, nil); err != nil {
			t.Fatalf("ValidateNestedWriteEligibility() = %v, want nil — Archived is auto-included", err)
		}
	})
}

// TestNestedEligibility_ValidateReportsEachViolationOnce pins the orchestration
// rather than the rule. ValidateGeneration resolves the nested surface twice —
// once through the lint and once through the pass that attaches it — and both
// see the same rejected edge, so reporting from both prints every violation
// twice under `sqlgen validate`. Only the lint reports.
func TestNestedEligibility_ValidateReportsEachViolationOnce(t *testing.T) {
	input := testInput(nestedSchema())
	cfg := input.Config
	cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
	cfg.Tables["users"] = config.TableConfig{
		Relationships: nestedUserRelationships(),
		NestedMutations: &config.TableNestedMutationsConfig{
			Relationships: []config.TableNestedRelationship{{Name: "Archived"}},
		},
	}

	_, err := gen.ValidateGeneration(input.Schema, cfg)
	if err == nil {
		t.Fatal("ValidateGeneration() = nil, want an error naming Archived")
	}
	if got := strings.Count(err.Error(), `relationship "Archived" is not write-eligible`); got != 1 {
		t.Errorf("the violation is reported %d times, want 1:\n%s", got, err)
	}
}

// nestedEligibilityError runs the §9.9.4 lint over the fixture, which is the
// same entry point `sqlgen validate` calls.
func nestedEligibilityError(t *testing.T, mutate func(*config.RootConfig)) error {
	t.Helper()
	input := testInput(nestedSchema())
	cfg := input.Config
	cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
	cfg.Tables["users"] = config.TableConfig{Relationships: nestedUserRelationships()}
	if mutate != nil {
		mutate(cfg)
	}
	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}
	return gen.ValidateNestedWriteEligibility(contexts, cfg, input.Resolver)
}

// --- resolved operations ---

// TestNestedOperations_FollowNestedMutationsAlone pins that the three
// nested flags on the resolved operations are exactly what was emitted, and
// `generation.nested_mutations` alone decides it. There is no per-table toggle
// and no base-operation conjunction, since the client always has the base.
func TestNestedOperations_FollowNestedMutationsAlone(t *testing.T) {
	users := buildNestedContexts(t, func(cfg *config.RootConfig) {
		cfg.Generation.NestedMutations.Operations = []string{"update"}
	})["users"]
	if users.Nested == nil {
		t.Fatal("the update family is on, so the parent keeps its nested surface")
	}
	got := [3]bool{users.Operations.CreateWithRelated, users.Operations.UpdateWithRelated, users.Operations.UpsertWithRelated}
	want := [3]bool{users.Nested.EmitCreate, users.Nested.EmitUpdate, users.Nested.EmitUpsert}
	if got != want || got != [3]bool{false, true, false} {
		t.Errorf("Operations Create/Update/UpsertWithRelated = %v, emitted %v, want both [false true false]", got, want)
	}

	// A table with no eligible edge emits nothing, and its flags say so.
	for name, tc := range buildNestedContexts(t, nil) {
		if tc.Nested == nil && (tc.Operations.CreateWithRelated || tc.Operations.UpdateWithRelated || tc.Operations.UpsertWithRelated) {
			t.Errorf("table %q has no nested surface but its resolved operations claim a nested method: %+v", name, tc.Operations)
		}
	}
}

// --- rendered output ---

// renderNested renders the nested surface for one table and checks the output
// parses as Go before returning it.
//
// The parse is not decoration: several branches of this template are rendered
// by no example schema at all — the pointer `Connect` of a has-one edge on a
// nullable FK, the `allow_reparent` fork — so a syntax error in one of them
// would otherwise reach a consumer before it reached any gate here
// (guidelines/TESTING.md §11).
func renderNested(t *testing.T, tc gen.TableContext) string {
	t.Helper()
	tmpl, err := template.New("nested.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseFiles(filepath.Join("templates", "table", "nested.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing nested template: %v", err)
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/nested", tc); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	out := buf.String()
	wrapped := gen.WrapWithPreamble(tc.Package, tc.Imports, []byte(out))
	if _, err := goparser.ParseFile(token.NewFileSet(), "x.go", wrapped, 0); err != nil {
		t.Fatalf("rendered nested surface for %s does not parse: %v\n\n--- output ---\n%s", tc.TableName, err, out)
	}
	return out
}

// TestNestedTemplate_EveryEmittedSurfaceParses renders every parent the fixture
// produces, including the two shapes no example schema reaches: a has-one edge
// on a nullable FK (`Bio`) and the `allow_reparent` fork.
func TestNestedTemplate_EveryEmittedSurfaceParses(t *testing.T) {
	for _, mutate := range []func(*config.RootConfig){
		nil,
		func(cfg *config.RootConfig) {
			tc := cfg.Tables["users"]
			tc.NestedMutations = &config.TableNestedMutationsConfig{
				Relationships: []config.TableNestedRelationship{
					{Name: "Bio", AllowReparent: true},
					{Name: "Events", AllowReparent: true},
				},
			}
			cfg.Tables["users"] = tc
		},
	} {
		for _, tc := range buildNestedContexts(t, mutate) {
			if tc.Nested == nil {
				continue
			}
			renderNested(t, tc)
		}
	}
}

// TestNestedWiring_EveryExecutorClientIsDeclaredAndWired pins the property the
// parse check above cannot see: every `c.<x>Client` a nested executor writes
// through is a field the entity client declares **and** one the unified client
// wires at construction. A missing declaration is a compile error in the
// consumer's package; a missing wire is a nil pointer at the first nested
// write. renderNested parses without type-checking, so neither reaches it.
//
// The has-one edges are the case that motivated it. A has-one target
// is not an O2M or M2M target, so the loaders' field list never names it, and
// the executor's `create` called a field nothing declared. The graphql
// example's only has-one edge, `assets.PrimaryDocument`, hid it: `documents` is
// also the target of the O2M `Attachments`, which declares the field on its
// behalf. The mysql example's `users.Profile` is the first to reach it, and
// `sqlgen generate` failed there on `c.profileClient undefined`.
func TestNestedWiring_EveryExecutorClientIsDeclaredAndWired(t *testing.T) {
	contexts := buildNestedContexts(t, nil)
	tables := make([]gen.TableContext, 0, len(contexts))
	for _, tc := range contexts {
		tables = append(tables, tc)
	}
	client := gen.BuildClientContext(tables, nil, "models", "Client", false, nil, nil, nil)
	wired := make(map[string]map[string]bool, len(client.Entities))
	for _, e := range client.Entities {
		wired[e.StructName] = make(map[string]bool, len(e.ClientWires))
		for _, w := range e.ClientWires {
			wired[e.StructName][w.EntityField] = true
		}
	}
	camel := gen.FuncMap(sql.NewPostgresDialect())["toCamelCase"].(func(string) string)

	checked := 0
	for _, tc := range contexts {
		if tc.Nested == nil {
			continue
		}
		declared := make(map[string]bool)
		for _, name := range tc.RelationshipTargetClients {
			declared[camel(name)+"Client"] = true
		}
		for _, name := range tc.NestedWriteClients {
			declared[camel(name)+"Client"] = true
		}
		for _, edge := range tc.Nested.Edges {
			fields := []string{edge.TargetClientField}
			if edge.Shape == "m2m" {
				fields = append(fields, edge.JunctionClientField)
			}
			for _, field := range fields {
				checked++
				if !declared[field] {
					t.Errorf("%s.%s writes through c.%s, which the %s entity client does not declare", tc.StructName, edge.FieldName, field, tc.StructName)
				}
				if !wired[tc.StructName][field] {
					t.Errorf("%s.%s writes through c.%s, which the unified client never wires — a nil pointer at the first nested write", tc.StructName, edge.FieldName, field)
				}
			}
		}
	}
	// Floor, so a fixture change that leaves no nested edge cannot make this
	// pass vacuously. users alone has two has-one edges and two M2M edges.
	if checked < 6 {
		t.Fatalf("checked %d executor client references; the fixture is not reaching the nested surface", checked)
	}
}

// TestNestedTemplate_DiscriminatorIsSetAndElided pins the discriminator's
// write-side rules together, because they are halves of one decision: the
// executor sets the discriminator from the edge's declared value, so the
// caller's input has no field for it. Leaving the field would let a caller
// write a row this edge can never read back.
func TestNestedTemplate_DiscriminatorIsSetAndElided(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	edge := nestedEdge(t, users, "Tagged")

	// The column is absent from the nested child input.
	for _, f := range edge.ChildFields {
		if f.ColumnName == "action" {
			t.Errorf("nested child input %s declares the discriminator column %q; the edge sets it itself", edge.ChildInputName, f.ColumnName)
		}
		if f.ColumnName == "user_id" {
			t.Errorf("nested child input %s declares the traversed FK %q; the edge sets it itself", edge.ChildInputName, f.ColumnName)
		}
	}

	// The executor sets it to the edge's declared value.
	out := renderNested(t, users)
	if !strings.Contains(out, `Action:      "signup",`) && !strings.Contains(out, `Action: "signup",`) {
		t.Errorf("rendered executor does not set the discriminator to the declared value.\n%s", nestedExcerpt(out, edge.ExecutorName))
	}

	// The same predicate joins the connect visibility read.
	if !strings.Contains(out, `QuoteIdentifier("action")+" = $", "signup"`) {
		t.Errorf("connect visibility read does not carry the discriminator predicate.\n%s", nestedExcerpt(out, edge.ExecutorName))
	}
}

// TestNestedTemplate_ConnectVisibilityReadIsUnbounded pins the `Limit: new(0)`
// rule (PRD §9.9.6). It is normative, not decoration: a nil Limit would let the
// client's default query_limit truncate the read, so a connect naming more
// targets than the page size would find the tail of its own list absent and
// report real, visible rows as NOT_FOUND. The failure scales with input size.
func TestNestedTemplate_ConnectVisibilityReadIsUnbounded(t *testing.T) {
	out := renderNested(t, buildNestedContexts(t, nil)["users"])
	reads := strings.Count(out, "GetMany(ctx, &Get")
	if reads == 0 {
		t.Fatal("no visibility read was emitted")
	}
	if got := strings.Count(out, "Limit: new(0),"); got != reads {
		t.Errorf("%d of %d visibility reads carry `Limit: new(0)`; PRD §9.9.6 makes it normative on every one", got, reads)
	}
}

// TestNestedTemplate_CreateBlockHasNoUnlinkVerbs pins that `disconnect` and
// `clear` are meaningless on a row that does not exist yet, so the create-side
// block is a narrower TYPE rather than the same type with rules. An accepted-
// and-ignored field would be exactly the failure the separate input types in
// §9.2 exist to prevent.
func TestNestedTemplate_CreateBlockHasNoUnlinkVerbs(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	out := renderNested(t, users)
	for _, edge := range users.Nested.Edges {
		block := nestedTypeDecl(t, out, edge.CreateBlockName)
		for _, forbidden := range []string{"Disconnect ", "Clear "} {
			if strings.Contains(block, forbidden) {
				t.Errorf("%s declares %q; the create-side block must not express it\n%s", edge.CreateBlockName, strings.TrimSpace(forbidden), block)
			}
		}
		// The same two verbs must be expressible on the update-side type, or
		// the narrowing above is vacuous rather than load-bearing.
		update := nestedTypeDecl(t, out, edge.UpdateBlockName)
		if edge.HasDisconnect && !strings.Contains(update, "Disconnect ") {
			t.Errorf("%s admits `disconnect` but %s declares no member for it\n%s", edge.FieldName, edge.UpdateBlockName, update)
		}
		if edge.HasClear && !strings.Contains(update, "Clear ") {
			t.Errorf("%s admits `clear` but %s declares no member for it\n%s", edge.FieldName, edge.UpdateBlockName, update)
		}
	}
}

// nestedTypeDecl returns the source of one emitted type declaration, so an
// assertion about a struct's members cannot be satisfied — or broken — by a
// sibling struct in the same file.
func nestedTypeDecl(t *testing.T, out, name string) string {
	t.Helper()
	start := strings.Index(out, "type "+name+" struct {")
	if start < 0 {
		t.Fatalf("no declaration of type %q in the rendered output", name)
	}
	end := strings.Index(out[start:], "\n}")
	if end < 0 {
		t.Fatalf("declaration of type %q is unterminated", name)
	}
	return out[start : start+end+2]
}

// TestNestedTemplate_ParentWriteStripsEveryRelationship pins §9.9.6's first
// step. Loading a relationship on the parent write would run before the nested
// rows exist and return a stale set — and the stripping covers every
// relationship member, not only the nestable ones, because the ineligible ones
// are just as stale.
func TestNestedTemplate_ParentWriteStripsEveryRelationship(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	out := renderNested(t, users)
	for _, rel := range users.Nested.RelationshipFieldNames {
		if !strings.Contains(out, "scalars."+rel+" = nil") {
			t.Errorf("relationship %q is not stripped from the parent write's FieldOptions", rel)
		}
	}
	if !strings.Contains(out, "scalars.ID = true") {
		t.Error("the parent write does not force its primary key on; every nested write keys on it")
	}
	// A nil selection selects no relationship, so the terminal re-read
	// does not run and the parent comes back with its members unpopulated.
	if !strings.Contains(out, "func userNestedSelectsRelationship(fo *UserFieldOptions) bool {\n\tif fo == nil {\n\t\treturn false\n\t}") {
		t.Errorf("the relationship-selection helper does not return false for a nil FieldOptions.\n%s", nestedExcerpt(out, "userNestedSelectsRelationship"))
	}
}

// TestNestedTemplate_M2MLinkGoesThroughUpsertMany pins the §9.9.6 link step.
// Re-adding an existing link is a set operation whose answer is "linked", and
// CreateMany raises a primary-key violation on it (companion §2.4 probe A).
func TestNestedTemplate_M2MLinkGoesThroughUpsertMany(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	edge := nestedEdge(t, users, "Categories")
	out := renderNested(t, users)
	if !strings.Contains(out, "c.userCategoryClient.UpsertMany(ctx, links, UserCategoryConflictPK") {
		t.Errorf("the M2M link step does not route through the junction's UpsertMany:\n%s", nestedExcerpt(out, edge.ExecutorName))
	}
	if strings.Contains(out, "c.userCategoryClient.CreateMany") {
		t.Errorf("the M2M link step uses CreateMany, which raises a PK violation on an existing link:\n%s", nestedExcerpt(out, edge.ExecutorName))
	}
}

// nestedExcerpt returns the emitted function named by fn, for failure output.
// It anchors on the declaration rather than on the bare name, which also
// appears at every call site.
func nestedExcerpt(out, fn string) string {
	idx := strings.Index(out, ") "+fn+"(")
	if idx < 0 {
		idx = strings.Index(out, "func "+fn+"(")
	}
	if idx < 0 {
		return out
	}
	// To the closing brace at column zero, so an assertion about one function's
	// body cannot be satisfied — or truncated — by the next one. A fixed
	// character window did both once the executors grew their unlink half.
	if end := strings.Index(out[idx:], "\n}\n"); end >= 0 {
		return out[idx : idx+end+3]
	}
	return out[idx:]
}

// TestNestedSharedHelpers_BodiesVaryWithTenancy pins the two package-level
// helpers every nested executor calls, in all three shapes the package can be
// in. Without it they are exercised by an E2E golden alone — and only by the
// *tenanted* one, since `graphql` is the single example that enables the
// feature, so the non-tenanted body of nestedChildOptions is rendered by
// nothing. That is a gate hole: a
// conditional branch whose only exercising fixture is an E2E golden sits
// outside `make check`, which runs `go test -short`.
func TestNestedSharedHelpers_BodiesVaryWithTenancy(t *testing.T) {
	tests := []struct {
		name           string
		tenancyEnabled bool
		tenantGoType   string
		wantInBody     []string
		wantNotInBody  []string
	}{
		{
			name:          "no tenancy — only the four hook-level toggles cross",
			wantInBody:    []string{"o.SkipCache, o.SkipEvents = parent.SkipCache, parent.SkipEvents", "o.SkipHooks = parent.SkipHooks"},
			wantNotInBody: []string{"SkipTenancy", "o.Tenant"},
		},
		{
			name:           "tenancy on, no tenanted table — SkipTenancy crosses, Tenant does not exist",
			tenancyEnabled: true,
			wantInBody:     []string{"o.SkipTenancy = parent.SkipTenancy"},
			wantNotInBody:  []string{"o.Tenant"},
		},
		{
			name:           "tenancy on with a uniform tenant type — the explicit tenant crosses too",
			tenancyEnabled: true,
			tenantGoType:   "uuid.UUID",
			wantInBody:     []string{"o.SkipTenancy = parent.SkipTenancy", "o.Tenant = parent.Tenant"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := gen.BuildSharedTypesContext("db", tt.tenancyEnabled, tt.tenantGoType, "", false, false, true)
			child, ok := sharedHelperByName(ctx, "nestedChildOptions")
			if !ok {
				t.Fatal("nestedChildOptions is absent from a package that emits a nested surface")
			}
			// Generic in BOTH parameters: CallOptions is generic over the
			// field-options type, and the PARENT's varies per parent too, so
			// pinning the second argument would compile for one client alone.
			if want := "[FO, PFO any](o *CallOptions[FO], parent CallOptions[PFO])"; child.Signature != want {
				t.Errorf("signature = %q, want %q", child.Signature, want)
			}
			for _, want := range tt.wantInBody {
				if !strings.Contains(child.Body, want) {
					t.Errorf("body does not copy %q:\n%s", want, child.Body)
				}
			}
			for _, absent := range tt.wantNotInBody {
				if strings.Contains(child.Body, absent) {
					t.Errorf("body copies %q, which this package's CallOptions does not declare:\n%s", absent, child.Body)
				}
			}
			// FieldOptions, LockMode and AllowInTransaction never cross: the
			// inner calls select nothing (the skip-refetch escape), take no row
			// lock, and issue no Stream (PRD §9.4a).
			for _, never := range []string{"FieldOptions", "LockMode", "AllowInTransaction"} {
				if strings.Contains(child.Body, never) {
					t.Errorf("body propagates %s across a table boundary; PRD §9.9.6 sets it per inner call", never)
				}
			}

			errHelper, ok := sharedHelperByName(ctx, "nestedError")
			if !ok {
				t.Fatal("nestedError is absent from a package that emits a nested surface")
			}
			if !strings.Contains(errHelper.Body, "&NestedMutationError{Edge: edge, Verb: verb, ID: id, Err: err}") {
				t.Errorf("nestedError does not build the §22.2 structured error:\n%s", errHelper.Body)
			}
		})
	}
}

// TestNestedSharedHelpers_AbsentWithoutANestedSurface pins the gate: a package
// that emits no nested surface carries neither helper, so its shared_types_gen.go
// is byte-identical to its form without nested mutations.
func TestNestedSharedHelpers_AbsentWithoutANestedSurface(t *testing.T) {
	ctx := gen.BuildSharedTypesContext("db", false, "", "", false, false, false)
	for _, name := range []string{"nestedChildOptions", "nestedError"} {
		if _, ok := sharedHelperByName(ctx, name); ok {
			t.Errorf("%s is emitted into a package with no nested surface", name)
		}
	}
}

func sharedHelperByName(ctx gen.SharedTypesContext, name string) (gen.SharedHelperDefinition, bool) {
	for _, h := range ctx.Helpers {
		if h.Name == name {
			return h, true
		}
	}
	return gen.SharedHelperDefinition{}, false
}

// tenantedTargetNestedContexts builds the nested fixture with tenancy enabled
// and the tenant column on `orders` ONLY — so `users`, the nested parent, is a
// shared table writing into a tenanted child.
//
// That combination is PRD §29.10's explicitly-legitimate shape ("un-tenanted
// children under tenanted parents is a common, legitimate schema pattern" — and
// its converse), and no example schema has it: the one tenanted parent with
// nested edges, `workspace_notes`, is self-referential, so its target is
// itself. Without this fixture the shared-parent arm of the tenant-resolve
// hoist is emitted by nothing and checked by nothing.
func tenantedTargetNestedContexts(t *testing.T) map[string]gen.TableContext {
	t.Helper()
	return nestedContextsWithTenantOn(t, "orders")
}

// nestedContextsWithTenantOn builds the nested fixture with tenancy enabled and
// the tenant column on exactly the named tables.
func nestedContextsWithTenantOn(t *testing.T, tenanted ...string) map[string]gen.TableContext {
	t.Helper()
	want := make(map[string]bool, len(tenanted))
	for _, n := range tenanted {
		want[n] = true
	}
	schema := nestedSchema()
	for i := range schema.Tables {
		if want[schema.Tables[i].Name] {
			schema.Tables[i].Columns = append(schema.Tables[i].Columns,
				parser.Column{Name: "workspace_id", Type: "uuid"})
		}
	}

	input := testInput(schema)
	cfg := input.Config
	cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
	cfg.Tables["users"] = config.TableConfig{Relationships: nestedUserRelationships()}
	cfg.Tenancy = &config.TenancyConfig{
		Enabled:  true,
		Column:   "workspace_id",
		Required: new(true),
	}

	contexts, err := gen.BuildTableContextsFromSchema(input.Schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	byName := make(map[string]gen.TableContext, len(contexts))
	for _, tc := range contexts {
		byName[tc.TableName] = tc
	}
	return byName
}

// TestNestedTenancy_SharedParentWithTenantedChildResolvesOnce pins the shared-
// parent arm of the tenant-resolve hoist.
//
// PRD §29.6 bounds the resolver to one invocation per logical mutation and
// §29.10 extends that to nested children, so the parent has to hoist the
// resolve onto the transaction ctx — including when the parent itself carries
// no tenant column and would otherwise never resolve anything. The hoist is
// therefore gated on a fact about the *edges*, not about the table.
//
// The gate is also why the hoist caches the resolver's raw answer rather than
// resolveTenant's (value, apply) pair: a shared table's resolveTenant is
// emitted in its required:false form, so hoisting a decision here would hand
// apply=false to a required:true child and skip the tenancy.ErrMissing it owes.
func TestNestedTenancy_SharedParentWithTenantedChildResolvesOnce(t *testing.T) {
	contexts := tenantedTargetNestedContexts(t)
	users, orders := contexts["users"], contexts["orders"]

	if orders.Tenancy == nil || !orders.Tenancy.Tenanted {
		t.Fatal("fixture is wrong: orders must be tenanted for this test to mean anything")
	}
	if users.Tenancy != nil && users.Tenancy.Tenanted {
		t.Fatal("fixture is wrong: users must be shared for this test to mean anything")
	}

	if !users.HasTenantedNestedEdge() {
		t.Errorf("shared parent with a tenanted nested edge: HasTenantedNestedEdge() = false, want true (edges: %v)", nestedEdgeNames(users))
	}
	if !users.NeedsTenantResolver() {
		t.Error("shared parent with a tenanted nested edge: NeedsTenantResolver() = false, want true — the hoist has no resolver to call")
	}
	if users.Tenancy == nil || users.Tenancy.GoType == "" {
		t.Fatal("shared parent needs a backfilled tenant Go type or the emitted resolver spells tenancy.TenantResolver[]")
	}

	// The edge that makes it true is the tenanted one, and only that one.
	if e := nestedEdge(t, users, "Orders"); !e.TargetResolvesTenant {
		t.Error("Orders edge targets the tenanted table but TargetResolvesTenant = false")
	}
	if e := nestedEdge(t, users, "Events"); e.TargetResolvesTenant {
		t.Error("Events edge targets a table that resolves no tenant but TargetResolvesTenant = true")
	}

	out := renderNested(t, users)
	for _, want := range []string{
		"if options.SkipTenancy || options.Tenant != nil {",
		"resolvedTenant, err := c.tenantValue(ctx)",
		"return tenancy.WithResolvedTenant(ctx, resolvedTenant), nil",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("shared parent's nested surface is missing the §29.6 hoist %q\nfull output:\n%s", want, out)
		}
	}
	// One hoist, called by every family — the three methods cannot disagree
	// about how many times the consumer's resolver runs.
	for _, method := range []string{"CreateWithRelated", "UpdateWithRelated", "UpsertWithRelated"} {
		if !strings.Contains(nestedExcerpt(out, method), "c.nestedResolvedTenantCtx(ctx, options)") {
			t.Errorf("%s does not take the §29.6 hoist\n%s", method, nestedExcerpt(out, method))
		}
	}
}

// TestNestedTenancy_SharedParentWithSharedChildrenHoistsNothing is the negative
// half: the hoist costs a resolver call, so a parent whose edges reach no
// tenanted client must not pay it. Without this, gating the hoist on "tenancy is
// enabled" instead of "an edge target is tenanted" would pass the test above.
func TestNestedTenancy_SharedParentWithSharedChildrenHoistsNothing(t *testing.T) {
	contexts := tenantedTargetNestedContexts(t)
	for name, tc := range contexts {
		if tc.Nested == nil || name == "users" {
			continue
		}
		if tc.NeedsTenantResolver() {
			continue
		}
		if out := renderNested(t, tc); strings.Contains(out, "c.tenantValue(ctx)") {
			t.Errorf("table %q reaches no tenanted client through its edges but still hoists a resolve", name)
		}
	}
}

// TestNestedTenancy_TenantedParentWithSharedChildrenStillHoists is the other
// residual §29.6 gap of the tenant-resolve hoist.
//
// Gating the hoist on "an edge target is tenanted" looks sufficient and is not:
// `CreateWithRelated` issues a terminal re-read of the *parent* whenever the
// caller selected a relationship, and on a tenanted parent that read resolves
// too. `c.Create`'s own stash is scoped to its executor closure and never
// reaches the transaction body, so without the hoist that re-read is a second
// resolve — §29.6 violated by a parent whose children are all shared.
//
// The gate is therefore "some client this method reaches resolves a tenant",
// which is exactly NeedsTenantResolver.
func TestNestedTenancy_TenantedParentWithSharedChildrenStillHoists(t *testing.T) {
	users := nestedContextsWithTenantOn(t, "users")["users"]

	if users.Tenancy == nil || !users.Tenancy.Tenanted {
		t.Fatal("fixture is wrong: users must be tenanted for this test to mean anything")
	}
	if users.HasTenantedNestedEdge() {
		t.Fatal("fixture is wrong: no edge target may resolve a tenant, or this test cannot fail")
	}
	if !users.NeedsTenantResolver() {
		t.Fatal("a tenanted table must need a resolver")
	}

	if out := renderNested(t, users); !strings.Contains(out, "resolvedTenant, err := c.tenantValue(ctx)") {
		t.Errorf("tenanted parent with only shared children does not hoist; its terminal re-read resolves a second time\nfull output:\n%s", out)
	}
}

// --- the update-side verbs (PRD §9.9) ---
//
// `disconnect` and `clear` are the two verbs that only exist once the parent
// does, so this block is where the §9.9.3 matrix is finally asserted whole. The
// runtime halves — that the FK actually lands NULL, that the batching survives
// a list past the SQLite bind ceiling — live in the example's E2E suite; what a
// unit test can pin is which verbs an edge admits and what the emitted
// statements are scoped by.

// TestNestedEligibility_UnlinkVerbSetPerShape pins the §9.9.3 rows for
// `disconnect` and `clear`, which are decided by the same two facts `connect`
// is — the FK's nullability on a to-many edge, the junction's own surface on an
// M2M one — plus a soft-delete column on the junction, which takes both verbs
// and leaves the linking half alone.
func TestNestedEligibility_UnlinkVerbSetPerShape(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]

	type verbs struct{ Disconnect, Clear bool }
	tests := []struct {
		edge string
		want verbs
		why  string
	}{
		{edge: "Bio", want: verbs{Disconnect: true, Clear: true}, why: "has-one on a nullable FK"},
		{edge: "Blobs", want: verbs{Clear: true}, why: "a PK that cannot key a Go map takes `connect` and `disconnect`; `clear` names no ids and survives"},
		{edge: "Categories", want: verbs{Disconnect: true, Clear: true}, why: "M2M through a junction with hard_delete and no soft-delete column"},
		{edge: "Events", want: verbs{Disconnect: true, Clear: true}, why: "O2M on a nullable FK"},
		{edge: "Orders", want: verbs{}, why: "nothing on a NOT NULL FK can be unlinked"},
		{edge: "Profile", want: verbs{}, why: "nothing on a NOT NULL FK can be unlinked, on the has-one shape"},
		{edge: "SoftTags", want: verbs{}, why: "the junction carries a soft-delete column"},
		{edge: "Tagged", want: verbs{Disconnect: true, Clear: true}, why: "a discriminator edge unlinks within its own value"},
	}
	for _, tt := range tests {
		t.Run(tt.edge, func(t *testing.T) {
			e := nestedEdge(t, users, tt.edge)
			got := verbs{Disconnect: e.HasDisconnect, Clear: e.HasClear}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("unlink verb set mismatch (%s) (-want +got):\n%s", tt.why, diff)
			}
		})
	}
}

// TestNestedEligibility_JunctionSoftDeleteKeepsTheLinkingHalf is the
// soft-delete junction rule's other half, and it is the one that makes the rule
// a narrowing rather than a rejection: a junction the unlink verbs cannot touch
// still takes `create` and `connect`, because neither of those deletes
// anything.
func TestNestedEligibility_JunctionSoftDeleteKeepsTheLinkingHalf(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	soft, plain := nestedEdge(t, users, "SoftTags"), nestedEdge(t, users, "Categories")

	if !soft.HasCreate || !soft.HasConnect {
		t.Errorf("SoftTags: HasCreate=%v HasConnect=%v, want both true — the rule scopes to the unlink verbs", soft.HasCreate, soft.HasConnect)
	}
	if soft.JunctionPKStructName != "" || soft.JunctionLocalFilterField != "" {
		t.Errorf("SoftTags still carries unlink expressions (pk=%q filter=%q)", soft.JunctionPKStructName, soft.JunctionLocalFilterField)
	}
	// The sibling junction differs in exactly one column, so the comparison
	// says the rule fired on the soft-delete column and not on something else.
	if plain.JunctionPKStructName == "" || plain.JunctionLocalFilterField == "" {
		t.Errorf("Categories: the control edge lost its unlink expressions too (pk=%q filter=%q)", plain.JunctionPKStructName, plain.JunctionLocalFilterField)
	}

	out := renderNested(t, users)
	softBlock := nestedTypeDecl(t, out, soft.UpdateBlockName)
	for _, forbidden := range []string{"Disconnect ", "Clear "} {
		if strings.Contains(softBlock, forbidden) {
			t.Errorf("%s declares %q, which a soft-deletable junction refuses\n%s", soft.UpdateBlockName, strings.TrimSpace(forbidden), softBlock)
		}
	}
}

// TestNestedEligibility_UpsertNeedsAConflictTarget pins the upsert
// conflict-target rule.
//
// `UpsertWithRelated` takes a `<Parent>ConflictTarget` argument, and a table
// whose uniqueness is app-enforced through `primary_key.columns` emits no such
// constant (PRD §9.5) — so there is nothing the caller could pass. The surface
// is omitted rather than emitted uncallable, and the other two families are
// untouched: the rule is about the argument, not about nesting.
func TestNestedEligibility_UpsertNeedsAConflictTarget(t *testing.T) {
	users := buildNestedContexts(t, func(cfg *config.RootConfig) {
		tc := cfg.Tables["users"]
		tc.PrimaryKey = &config.TablePrimaryKeyConfig{Columns: []string{"id"}}
		cfg.Tables["users"] = tc
	})["users"]

	if len(users.ConflictTargets) != 0 {
		t.Fatalf("fixture is wrong: users still emits %d conflict target(s), so this test cannot fail", len(users.ConflictTargets))
	}
	if users.Nested == nil {
		t.Fatal("the parent lost its whole nested surface; the rule scopes to the upsert family")
	}
	if users.Nested.EmitUpsert {
		t.Error("EmitUpsert = true with no conflict-target constant; UpsertWithRelated would take an argument that does not exist")
	}
	if !users.Nested.EmitCreate || !users.Nested.EmitUpdate {
		t.Errorf("EmitCreate=%v EmitUpdate=%v, want both true", users.Nested.EmitCreate, users.Nested.EmitUpdate)
	}

	out := renderNested(t, users)
	if strings.Contains(out, "func (c *userClient) UpsertWithRelated(") {
		t.Error("UpsertWithRelated is emitted for a parent with no conflict-target constant")
	}
	if !strings.Contains(out, "func (c *userClient) UpdateWithRelated(") {
		t.Error("UpdateWithRelated went missing with the upsert family")
	}
}

// TestNestedEligibility_VerbsMaskNarrowsTheUnlinkHalf pins the §4.6 `verbs`
// mask over the two new verbs. A verb the mask turned off is dropped silently —
// the config already says why — which is what separates it from a verb the
// emitter could not render.
func TestNestedEligibility_VerbsMaskNarrowsTheUnlinkHalf(t *testing.T) {
	users := buildNestedContexts(t, func(cfg *config.RootConfig) {
		cfg.Generation.NestedMutations.Verbs = []string{"create", "connect", "disconnect"}
	})["users"]

	e := nestedEdge(t, users, "Events")
	if !e.HasDisconnect {
		t.Error("Events lost `disconnect`, which the mask names")
	}
	if e.HasClear {
		t.Error("Events kept `clear`, which the mask omits")
	}
	if err := nestedEligibilityError(t, func(cfg *config.RootConfig) {
		cfg.Generation.NestedMutations.Verbs = []string{"create", "connect", "disconnect"}
		tc := cfg.Tables["users"]
		tc.NestedMutations = &config.TableNestedMutationsConfig{
			Relationships: []config.TableNestedRelationship{{Name: "Events"}},
		}
		cfg.Tables["users"] = tc
	}); err != nil {
		t.Errorf("a verb the mask turned off was reported: %v — §9.9.4 makes a config-visible drop silent", err)
	}
}

// --- rendered output: the update-side statements ---

// TestNestedTemplate_UnlinkSetsNullRatherThanOmitting pins set-NULL-not-omit at
// the template level, which is where the two spellings are still
// distinguishable.
//
// `omittable.Set[*T](nil)` puts the column in the SET list with a NULL value;
// the zero `omittable.Value[*T]{}` leaves it out of the SET list entirely. Since
// nullable UUIDs bind to `*uuid.UUID`, the compiler accepts both in
// this position, so the wrong one matches its rows, reports success and unlinks
// nothing. The runtime half of this pin lives in the example suite.
func TestNestedTemplate_UnlinkSetsNullRatherThanOmitting(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	out := renderNested(t, users)
	body := nestedExcerpt(out, nestedEdge(t, users, "Events").ExecutorName)

	if !strings.Contains(body, "UserID: omittable.Set[*uuid.UUID](nil)") {
		t.Errorf("the unlink assignment does not set the FK NULL:\n%s", body)
	}
	if strings.Contains(body, "omittable.Value[*uuid.UUID]{}") {
		t.Errorf("the unlink assignment omits the FK from the SET list instead of setting it NULL:\n%s", body)
	}
	// Both unlink verbs take the same assignment — clear is disconnect with
	// the id list dropped, so a divergence here would be a second spelling.
	if got := strings.Count(body, "omittable.Set[*uuid.UUID](nil)"); got != 2 {
		t.Errorf("the NULL assignment appears %d times, want 2 (clear and disconnect):\n%s", got, body)
	}
}

// TestNestedUnlink_NullWrapperFKSetsTheWrapperInvalid covers the other
// nullable-FK binding. A nullable FK bound to a Null wrapper rather than a
// pointer needs `omittable.Set(uuid.NullUUID{Valid: false})`, and the validity
// field is written explicitly rather than left to the zero value — a wrapper
// whose predicate is inverted has a zero value that means *valid*.
//
// No example schema reaches this arm, so without the fixture below it is
// emitted by nothing and checked by nothing.
func TestNestedUnlink_NullWrapperFKSetsTheWrapperInvalid(t *testing.T) {
	users := buildNestedContexts(t, func(cfg *config.RootConfig) {
		cfg.Overrides.Types["uuid"] = config.TypeOverride{
			Type:   "uuid.UUID",
			Import: "github.com/gofrs/uuid/v5",
			Nullable: config.NullableVariant{
				Type:            "uuid.NullUUID",
				Import:          "github.com/gofrs/uuid/v5",
				UnderlyingField: "UUID",
			},
		}
	})["users"]

	e := nestedEdge(t, users, "Events")
	if !e.HasDisconnect {
		t.Fatal("fixture is wrong: the Null-wrapper FK edge must keep `disconnect` or this test cannot fail")
	}
	if want := "omittable.Set(uuid.NullUUID{Valid: false})"; e.FKUnlinkAssignExpr != want {
		t.Errorf("FKUnlinkAssignExpr = %q, want %q", e.FKUnlinkAssignExpr, want)
	}
	if body := nestedExcerpt(renderNested(t, users), e.ExecutorName); !strings.Contains(body, "omittable.Set(uuid.NullUUID{Valid: false})") {
		t.Errorf("the emitted unlink does not set the wrapper invalid:\n%s", body)
	}
}

// TestNestedTemplate_ClearRunsFirstAndSupersedesDisconnect pins both halves of
// `clear`. The ordering is load-bearing — running clear after create and
// connect would unlink the rows they had just linked — and a disconnect under a
// clear is ignored rather than rejected, because every id it names is already
// unlinked.
func TestNestedTemplate_ClearRunsFirstAndSupersedesDisconnect(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	out := renderNested(t, users)

	for _, edge := range []string{"Events", "Categories"} {
		t.Run(edge, func(t *testing.T) {
			body := nestedExcerpt(out, nestedEdge(t, users, edge).ExecutorName)
			clear := strings.Index(body, "if nested.Clear {\n")
			if clear < 0 {
				t.Fatalf("no clear branch in the executor:\n%s", body)
			}
			for _, later := range []string{`"create"`, `"connect"`, `"disconnect"`} {
				at := strings.Index(body, "nestedError(\""+edge+"\", "+later)
				if at < 0 {
					t.Fatalf("no %s statement in the executor:\n%s", later, body)
				}
				if at < clear {
					t.Errorf("the %s statement is emitted before `clear`, which must run first", later)
				}
			}
			// The second half: the id list is dropped, so nothing downstream —
			// the verb-conflict check included — can fire on a verb that will
			// not run.
			if !strings.Contains(body, "disconnectIDs = nil") {
				t.Errorf("a disconnect under a clear is not ignored:\n%s", body)
			}
		})
	}
}

// TestNestedTemplate_UnlinkScopesToTheEdgeDiscriminator pins discriminator
// scoping on the unlink half.
//
// `clear` unlinks every row on *this* edge, and a polymorphic edge is
// `fk = parent AND <discriminator> = <value>`. Without the second term a clear
// on one edge unlinks every sibling edge's rows over the same foreign key — a
// silent over-unlink rather than a wrong error, and the sharpest form of the
// failure discriminator scoping already closes on the other two verbs.
func TestNestedTemplate_UnlinkScopesToTheEdgeDiscriminator(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	out := renderNested(t, users)

	tagged := nestedEdge(t, users, "Tagged")
	body := nestedExcerpt(out, tagged.ExecutorName)
	const want = `Action: &comparator.String{Eq: new("signup")}`
	if got := strings.Count(body, want); got != 4 {
		t.Errorf("the discriminator scope appears %d times in the Tagged executor, want 4 (clear, the adoption UPDATE, its verify read and disconnect):\n%s", got, body)
	}

	// The control: `Events` traverses the same column on the same table and
	// declares no discriminator, so its unlink is parent-scoped and nothing
	// more. Without this the assertion above could pass on a predicate the
	// emitter attaches to every edge.
	plain := nestedExcerpt(out, nestedEdge(t, users, "Events").ExecutorName)
	if strings.Contains(plain, "Action: &comparator.") {
		t.Errorf("an edge with no `discriminator:` carries one anyway:\n%s", plain)
	}
	// `Action` still appears there as an ordinary copied field on the create
	// input, which is what makes the narrower spelling above the right test.
	if !strings.Contains(plain, "Action: n.Action") {
		t.Errorf("the control edge stopped copying the column, so the assertion above no longer discriminates:\n%s", plain)
	}
}

// TestNestedTemplate_AdoptionCountsThroughAVerifyRead pins how the `connect`
// adoption learns which rows it adopted (PRD §9.9.6).
//
// The shortfall check used to count the rows UpdateWhere handed back. On MySQL
// those are the rows its pre-SELECT found, and under REPEATABLE READ that read
// answers from the transaction's snapshot while the UPDATE reads the latest
// commit. A row another connection parented in between was therefore counted as
// adopted while the `fk IS NULL` guard skipped it, and the call returned nil
// with the row under the other parent. The adoption now selects nothing
// from its UPDATE and counts a verify read instead: the chunk's ids that now
// carry `fk = parent`, plus the edge's discriminator where it declares one. A
// plain read inside the transaction sees the transaction's own writes, so that
// is the adopted set on every dialect, with no lock taken. The runtime half of
// this pin is the mysql example's `a lost race is ErrAlreadyRelated`.
//
// The last case is a `verbs` mask that leaves `connect` without either unlink
// verb. The `pid` local the verify read filters on used to be declared for the
// unlink verbs alone, and renderNested parses without type-checking, so an
// undeclared `pid` would pass every other assertion here.
func TestNestedTemplate_AdoptionCountsThroughAVerifyRead(t *testing.T) {
	reparent := func(cfg *config.RootConfig) {
		tc := cfg.Tables["users"]
		tc.NestedMutations = &config.TableNestedMutationsConfig{
			Relationships: []config.TableNestedRelationship{{Name: "Events", AllowReparent: true}},
		}
		cfg.Tables["users"] = tc
	}
	connectOnly := func(cfg *config.RootConfig) {
		cfg.Generation.NestedMutations.Verbs = []string{"create", "connect"}
	}
	tests := []struct {
		name   string
		mutate func(*config.RootConfig)
		edge   string
	}{
		{name: "O2M", edge: "Events"},
		{name: "O2M with a discriminator", edge: "Tagged"},
		{name: "has-one on a nullable FK", edge: "Bio"},
		{name: "allow_reparent", mutate: reparent, edge: "Events"},
		{name: "connect with no unlink verb", mutate: connectOnly, edge: "Events"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := buildNestedContexts(t, tt.mutate)["users"]
			e := nestedEdge(t, users, tt.edge)
			if !e.HasConnect || e.ParentFilterExpr == "" {
				t.Fatalf("%s: HasConnect=%v ParentFilterExpr=%q, want connect with an `fk = parent` term", tt.edge, e.HasConnect, e.ParentFilterExpr)
			}
			body := nestedExcerpt(renderNested(t, users), e.ExecutorName)

			start := strings.Index(body, "for start := 0; start < len(adopt); start += c.batchSize {")
			end := strings.Index(body, "if adoptedCount != len(adopt) {")
			if start < 0 || end < start {
				t.Fatalf("no chunked adoption in %s:\n%s", e.ExecutorName, body)
			}
			loop := body[start:end]
			update := strings.Index(loop, "c."+e.TargetClientField+".UpdateWhere(")
			verify := strings.Index(loop, "adopted, err := c."+e.TargetClientField+".GetMany(")
			if update < 0 || verify < update {
				t.Fatalf("the adoption UPDATE is not followed by a verify read that the count reads:\n%s", loop)
			}

			// The UPDATE selects nothing: what it reports is not what is counted.
			if write := loop[update:verify]; !strings.Contains(write, "o.FieldOptions = &"+e.TargetStructName+"FieldOptions{}") {
				t.Errorf("the adoption UpdateWhere still selects columns:\n%s", write)
			}

			// nestedChildOptions carries the caller's Tenant / SkipTenancy, so
			// without it the read would be scoped differently from the UPDATE
			// it verifies and could miss rows that UPDATE wrote.
			read := loop[verify:]
			wants := []string{
				e.TargetPKFieldName + ": " + e.AdoptFilterExpr,
				e.FKFilterField + ": " + e.ParentFilterExpr,
				"nestedChildOptions(o, options)",
				"Limit: new(0)",
				"o.SkipHooks = true",
				"o.LockMode = sql.LockNone",
				"adoptedCount += len(adopted)",
			}
			if e.DiscFilterField != "" {
				wants = append(wants, e.DiscFilterField+": "+e.DiscFilterExpr)
			}
			for _, want := range wants {
				if !strings.Contains(read, want) {
					t.Errorf("the verify read lacks %q:\n%s", want, read)
				}
			}
			if !strings.Contains(body[:start], "pid := "+e.ParentKeyExpr) {
				t.Errorf("the verify read filters on `pid`, which %s never declares:\n%s", e.ExecutorName, body)
			}
		})
	}
}

// TestNestedTemplate_BatchesEveryIDListStatement pins that every
// id-list-bearing statement is chunked at `c.batchSize`.
//
// The bind-parameter ceiling is dialect-dependent and lowest on SQLite (32766),
// and no dialect's error for it names a table, column, edge or verb — so
// §9.9.8 cannot attribute it and batching removes the ceiling rather than
// relocating or documenting it. `clear` is the one verb that is exempt, because
// it names no ids at all.
func TestNestedTemplate_BatchesEveryIDListStatement(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	out := renderNested(t, users)

	// Every statement that reads a caller-supplied id list, by the local it
	// builds: the connect visibility read, the adoption UPDATE, the O2M
	// disconnect UPDATE and the M2M junction delete.
	tests := []struct {
		edge  string
		loops []string
	}{
		{edge: "Events", loops: []string{
			"for start := 0; start < len(connectIDs); start += c.batchSize {",
			"for start := 0; start < len(adopt); start += c.batchSize {",
			"for start := 0; start < len(disconnectIDs); start += c.batchSize {",
		}},
		{edge: "Categories", loops: []string{
			"for start := 0; start < len(connectIDs); start += c.batchSize {",
			"for start := 0; start < len(pks); start += c.batchSize {",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.edge, func(t *testing.T) {
			body := nestedExcerpt(out, nestedEdge(t, users, tt.edge).ExecutorName)
			for _, loop := range tt.loops {
				if !strings.Contains(body, loop) {
					t.Errorf("missing batch loop %q:\n%s", loop, body)
				}
			}
			// The shortfall check has to sum across chunks: a row lost in one
			// chunk is a lost race whichever chunk it landed in, and comparing
			// a single chunk's count against the whole queue would report a
			// conflict on every input past the batch size.
			if tt.edge == "Events" && !strings.Contains(body, "if adoptedCount != len(adopt) {") {
				t.Errorf("the adoption shortfall is not summed across chunks:\n%s", body)
			}
		})
	}

	// `clear` is exempt by construction — it names no ids — so it must not be
	// inside a batch loop at all.
	body := nestedExcerpt(out, nestedEdge(t, users, "Events").ExecutorName)
	clearAt := strings.Index(body, "if nested.Clear {")
	firstLoop := strings.Index(body, "for start := 0;")
	if clearAt > firstLoop {
		t.Error("the clear statement sits after the first batch loop; it names no ids and is always exactly one statement")
	}
}

// TestNestedTemplate_UpsertReusesTheUpdateBlock pins block reuse. Each verb
// means the same thing on either branch, so one block is correct on both the
// insert and the update branch — there is no branch detection, and the wrapper
// members are the update-side type rather than a third one.
func TestNestedTemplate_UpsertReusesTheUpdateBlock(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	out := renderNested(t, users)

	upsert := nestedTypeDecl(t, out, users.Nested.UpsertInputName)
	update := nestedTypeDecl(t, out, users.Nested.UpdateInputName)
	for _, e := range users.Nested.Edges {
		member := e.FieldName + " *" + e.UpdateBlockName
		if !strings.Contains(upsert, member) {
			t.Errorf("%s does not carry %q; upsert reuses the update-side block verbatim\n%s", users.Nested.UpsertInputName, member, upsert)
		}
		if !strings.Contains(update, member) {
			t.Errorf("%s does not carry %q\n%s", users.Nested.UpdateInputName, member, update)
		}
	}
	// The flat half is the create input on the upsert side and the update
	// input on the update side — the same asymmetry Upsert and Update have.
	if !strings.Contains(upsert, "User CreateUserInput") {
		t.Errorf("%s does not contain Create<T>Input:\n%s", users.Nested.UpsertInputName, upsert)
	}
	if !strings.Contains(update, "User UpdateUserInput") {
		t.Errorf("%s does not contain Update<T>Input:\n%s", users.Nested.UpdateInputName, update)
	}

	// One executor per edge, called by all three families, which is what
	// makes the reuse compiler-enforced rather than a convention.
	for _, e := range users.Nested.Edges {
		if got := strings.Count(out, "c."+e.ExecutorName+"(ctx, parent,"); got != 3 {
			t.Errorf("edge %s: executor called %d times, want 3 (one per family)", e.FieldName, got)
		}
		if got := strings.Count(out, "func (c *userClient) "+e.ExecutorName+"("); got != 1 {
			t.Errorf("edge %s: %d executor definitions, want 1", e.FieldName, got)
		}
	}
}

// TestNestedTemplate_RefusesOneTargetUnderTwoVerbs pins the verb-conflict
// check. The order would decide the outcome and there is no defensible default,
// so the check runs before any statement — and it is skipped under `clear`,
// which drops the disconnect list and leaves nothing to contradict.
func TestNestedTemplate_RefusesOneTargetUnderTwoVerbs(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	out := renderNested(t, users)
	e := nestedEdge(t, users, "Events")
	body := nestedExcerpt(out, e.ExecutorName)

	check := strings.Index(body, "named := make(map[uuid.UUID]bool")
	if check < 0 {
		t.Fatalf("no verb-conflict check in the executor:\n%s", body)
	}
	if first := strings.Index(body, "c.eventClient."); first >= 0 && first < check {
		t.Error("a statement is issued before the verb-conflict check; §9.9.6 returns the conflict before any statement runs")
	}
	if !strings.Contains(body, `nestedError("Events", "disconnect", v, ErrNestedVerbConflict)`) {
		t.Errorf("the conflict is not attributed to `disconnect` with the offending id:\n%s", body)
	}
	// The create half — a create entry carrying an explicit key that the
	// disconnect list also names. Only reachable when the create input
	// declares the key, which is why the intersection above is the usual case.
	if e.ChildCreatePKField != "ID" || !e.ChildCreatePKOmittable {
		t.Fatalf("fixture is wrong: ChildCreatePKField=%q omittable=%v, want ID/true", e.ChildCreatePKField, e.ChildCreatePKOmittable)
	}
	if !strings.Contains(body, "if v, ok := n.ID.Get(); ok && named[v] {") {
		t.Errorf("the `create` ∩ `disconnect` half of the verb-conflict check is not emitted:\n%s", body)
	}

	// A target whose create input declares no key cannot carry the create
	// half, and must not pretend to: Category's key is generated.
	m2m := nestedEdge(t, users, "Categories")
	if m2m.ChildCreatePKField != "" {
		t.Errorf("Categories: ChildCreatePKField = %q, want empty — the target's create input declares no key", m2m.ChildCreatePKField)
	}
}

// TestNestedTemplate_SavepointNamesAreBareIdentifiers pins the composition half
// of PRD §18.3 for all three methods.
//
// The name each method hands database.WithTransaction doubles as the SAVEPOINT
// identifier the moment ctx already carries a transaction, so a prose name with
// spaces in it works at the top level — where it is only a label — and
// hard-errors on every nested call. Nothing but a nested call renders it into
// SQL, which is why the spelling needs its own pin.
func TestNestedTemplate_SavepointNamesAreBareIdentifiers(t *testing.T) {
	out := renderNested(t, buildNestedContexts(t, nil)["users"])
	for _, want := range []string{
		`database.WithTransaction(ctx, c.querier, "create_user_with_related",`,
		`database.WithTransaction(ctx, c.querier, "update_user_with_related",`,
		`database.WithTransaction(ctx, c.querier, "upsert_user_with_related",`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing transaction name %q", want)
		}
	}
	for _, name := range regexpAllTransactionNames(out) {
		if strings.ContainsAny(name, " \t\"") {
			t.Errorf("transaction name %q is not a bare SQL identifier; it hard-errors as a SAVEPOINT name", name)
		}
	}
}

// regexpAllTransactionNames extracts every WithTransaction name literal in out.
func regexpAllTransactionNames(out string) []string {
	const anchor = `database.WithTransaction(ctx, c.querier, "`
	var names []string
	for rest := out; ; {
		i := strings.Index(rest, anchor)
		if i < 0 {
			return names
		}
		rest = rest[i+len(anchor):]
		j := strings.Index(rest, `"`)
		if j < 0 {
			return names
		}
		names = append(names, rest[:j])
		rest = rest[j:]
	}
}

// TestNestedEligibility_IDListVerbsNeedAKeyableTarget is the invariant behind
// the `Blobs` row above, asserted over every edge the fixture produces rather
// than on the one that happens to exercise it.
//
// Three separate pieces of the executor bucket the caller's ids in a Go map —
// the verb-conflict check, the M2M link step's dedupe, and the `connect` owner
// map — and the self-reference guard compares one against the parent's key with
// `==`. The comparator.Opaque family (PRD §11.2) can do none of them: `[]byte`,
// `net.IP` and `net.HardwareAddr` are slices and `net.IPNet` holds two.
//
// This is a *context*-level assertion deliberately. `renderNested` parses the
// emitted Go but does not type-check it, so an edge that reached this shape
// emitted `map[[]byte]bool` and every gate in the repo stayed green — the
// example schemas have no such edge, so nothing ever compiled it.
func TestNestedEligibility_IDListVerbsNeedAKeyableTarget(t *testing.T) {
	var sawOne bool
	for _, tc := range buildNestedContexts(t, nil) {
		if tc.Nested == nil {
			continue
		}
		for _, e := range tc.Nested.Edges {
			keyable := gen.GoTypeIsValidMapKey(e.ConnectIDGoType)
			if !keyable {
				sawOne = true
			}
			for _, v := range []struct {
				name string
				on   bool
			}{
				{"connect", e.HasConnect},
				{"disconnect", e.HasDisconnect},
			} {
				if v.on && !keyable {
					t.Errorf("%s.%s admits `%s` with a target key of %s, which cannot key a Go map — the emitted executor would not compile",
						tc.StructName, e.FieldName, v.name, e.ConnectIDGoType)
				}
			}
		}
	}
	if !sawOne {
		t.Fatal("no fixture edge has a non-keyable target key, so this test cannot fail — `blobs` carries a bytea primary key for exactly this reason")
	}
}

// TestNestedEligibility_DiscriminatorScopeIsAllOrNothing pins the consistency
// discriminator scoping needs across the three statements that carry the edge's
// predicate.
//
// They are not spelled alike: the `connect` visibility read's is raw SQL keyed
// on the column, while the adoption UPDATE's and both unlink verbs' are
// filter members. So the read can be renderable where the writes are not, and
// a verb kept on that footing would read one set of rows and write another —
// the mislink discriminator scoping exists to close, one statement later.
func TestNestedEligibility_DiscriminatorScopeIsAllOrNothing(t *testing.T) {
	var sawOne bool
	for _, tc := range buildNestedContexts(t, nil) {
		if tc.Nested == nil {
			continue
		}
		for _, e := range tc.Nested.Edges {
			if e.DiscColumn == "" {
				continue
			}
			sawOne = true
			for _, v := range []struct {
				name string
				on   bool
			}{
				{"connect", e.HasConnect},
				{"disconnect", e.HasDisconnect},
				{"clear", e.HasClear},
			} {
				if v.on && e.DiscFilterExpr == "" {
					t.Errorf("%s.%s admits `%s` on a `discriminator:` edge with no renderable filter term; its statement would not be scoped to this edge",
						tc.StructName, e.FieldName, v.name)
				}
			}
		}
	}
	if !sawOne {
		t.Fatal("no fixture edge declares a `discriminator:`, so this test cannot fail")
	}
}

// TestNestedTemplate_HasOneReadsForAnExistingChild pins the executor half of
// the has-one guard (PRD §9.9.3, §9.9.6). A has-one edge holds at most one
// child, and only a UNIQUE FK makes the database refuse a second one, so
// `create` and `connect` read the edge first. The read has to come after
// `clear`, which is what makes `clear` plus `create` the replace spelling, and
// before the write it guards. A has-one block naming both verbs asks for two
// children and is refused at validation, before any statement runs.
func TestNestedTemplate_HasOneReadsForAnExistingChild(t *testing.T) {
	users := buildNestedContexts(t, nil)["users"]
	out := renderNested(t, users)

	const read = "occupied, err := c."
	for _, tt := range []struct {
		edge, write string
		clear       bool
	}{
		{edge: "Profile", write: "c.profileClient.Create(ctx"},
		{edge: "Bio", write: "c.bioClient.Create(ctx", clear: true},
	} {
		t.Run(tt.edge, func(t *testing.T) {
			body := nestedExcerpt(out, nestedEdge(t, users, tt.edge).ExecutorName)
			at := strings.Index(body, read)
			if at < 0 {
				t.Fatalf("no occupancy read in the has-one executor:\n%s", body)
			}
			if w := strings.Index(body, tt.write); w < at {
				t.Errorf("the create write (at %d) is not emitted after the occupancy read (at %d):\n%s", w, at, body)
			}
			if tt.clear {
				if c := strings.Index(body, "if nested.Clear {\n"); c < 0 || c > at {
					t.Errorf("`clear` (at %d) does not run before the occupancy read (at %d), so clear plus create would be refused:\n%s", c, at, body)
				}
			}
			for _, want := range []string{
				"UserID: &comparator.",
				"Limit: new(0),",
				`nestedError("` + tt.edge + `", verb, row.ID, ErrAlreadyRelated)`,
			} {
				if !strings.Contains(body[at:], want) {
					t.Errorf("the occupancy read lacks %q:\n%s", want, body)
				}
			}
		})
	}

	t.Run("create and connect in one block", func(t *testing.T) {
		body := nestedExcerpt(out, nestedEdge(t, users, "Bio").ExecutorName)
		guard := strings.Index(body, "if nested.Create != nil && nested.Connect != nil {")
		if guard < 0 {
			t.Fatalf("a has-one block naming both create and connect is not refused:\n%s", body)
		}
		if !strings.Contains(body[guard:], `nestedError("Bio", "connect", *nested.Connect, ErrNestedVerbConflict)`) {
			t.Errorf("the create+connect refusal is not ErrNestedVerbConflict on connect:\n%s", body)
		}
		if at := strings.Index(body, read); at >= 0 && at < guard {
			t.Errorf("the create+connect refusal (at %d) runs after the occupancy read (at %d); it is a validation step", guard, at)
		}
	})

	// With `create` masked off by the verbs mask, `connect` alone runs the
	// read, and it is attributed to `connect`.
	t.Run("connect only", func(t *testing.T) {
		masked := buildNestedContexts(t, func(cfg *config.RootConfig) {
			cfg.Generation.NestedMutations.Verbs = []string{"connect", "disconnect", "clear"}
		})["users"]
		edge := nestedEdge(t, masked, "Bio")
		if edge.HasCreate || !edge.HasConnect {
			t.Fatalf("Bio verbs = {create: %v, connect: %v}, want connect only", edge.HasCreate, edge.HasConnect)
		}
		body := nestedExcerpt(renderNested(t, masked), edge.ExecutorName)
		at := strings.Index(body, read)
		if at < 0 {
			t.Fatalf("no occupancy read in the connect-only has-one executor:\n%s", body)
		}
		for _, want := range []string{
			"if nested.Connect != nil {",
			`verb := "connect"`,
		} {
			if !strings.Contains(body[:at], want) {
				t.Errorf("the connect-only occupancy read is not guarded by %q:\n%s", want, body)
			}
		}
		if !strings.Contains(body[at:], `nestedError("Bio", verb, row.ID, ErrAlreadyRelated)`) {
			t.Errorf("the connect-only occupancy read does not refuse an occupied edge:\n%s", body)
		}
	})

	t.Run("an O2M edge holds many children and reads nothing", func(t *testing.T) {
		body := nestedExcerpt(out, nestedEdge(t, users, "Events").ExecutorName)
		if strings.Contains(body, read) {
			t.Errorf("an O2M executor carries the has-one occupancy read:\n%s", body)
		}
	})
}

// TestNestedEligibility_HasOneUnscopableCreateIsReported pins where a has-one
// edge over a discriminator the occupancy read cannot scope is refused. The
// fixture is MySQL `json`: its column carries no `Eq` filter member, so
// §9.9.4's has-one occupancy clause would drop `create`, and every other verb
// with it (PRD §9.9.4). It never gets that far. The column binds to
// `types.JSON`, which the declared value cannot be assigned to, so the
// discriminator binding rule refuses the whole edge before any verb is
// considered: a listed edge is told so, an auto-included one is omitted, and
// masking `create` off changes nothing.
//
// Every binding that rule accepts, a predeclared string, integer, float or
// boolean type or a generated enum, carries an `Eq` member, so no built-in
// binding reaches the has-one clause any more. The clause stays as defence for
// a binding a later change lets through, and this test is what shows it is
// unreachable today. MySQL is the dialect because the read path there works on
// a `json` column (§13.4.1 "Validation" refuses it on PostgreSQL), which keeps
// the auto-included half on the full pipeline.
func TestNestedEligibility_HasOneUnscopableCreateIsReported(t *testing.T) {
	build := func(t *testing.T, nullable, listed bool, verbs []string) (gen.TableContext, error) {
		t.Helper()
		schema := nestedSchema()
		schema.Tables = append(schema.Tables, parser.Table{
			Name: "notes", Schema: "public",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "user_id", Type: "uuid", Nullable: nullable},
				{Name: "kind", Type: "json"},
			},
		})
		input := testInput(schema)
		cfg := input.Config
		cfg.Input.Dialect = config.DialectMySQL
		cfg.Output.Driver = "stdlib"
		input.Resolver = gotype.NewResolver(config.DialectMySQL, true, cfg.Overrides.Types)
		cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true), Verbs: verbs}
		tc := config.TableConfig{Relationships: append(nestedUserRelationships(), config.TableRelationship{
			Name: "Pinned", Type: "one_to_one", Table: "notes", FK: "user_id",
			Discriminator: &config.RelationshipDiscriminator{Column: "kind", Value: "pinned"},
		})}
		if listed {
			tc.NestedMutations = &config.TableNestedMutationsConfig{
				Relationships: []config.TableNestedRelationship{{Name: "Pinned"}},
			}
		}
		cfg.Tables["users"] = tc
		// The lint runs over the bare contexts, as `sqlgen validate` does.
		contexts, err := gen.BuildTableContexts(input, nil)
		if err != nil {
			t.Fatalf("BuildTableContexts() error: %v", err)
		}
		lint := gen.ValidateNestedWriteEligibility(contexts, cfg, input.Resolver)
		if lint != nil {
			return gen.TableContext{}, lint
		}
		// Only the full pipeline attaches the surface (see buildNestedContexts),
		// and it hard-errors where the lint does, so it runs on a clean lint.
		attached, err := gen.BuildTableContextsFromSchema(input.Schema, cfg)
		if err != nil {
			t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
		}
		for _, c := range attached {
			if c.TableName == "users" {
				if c.Nested == nil {
					t.Fatal("users carries no nested surface, so the edge check cannot fail")
				}
				return c, nil
			}
		}
		t.Fatal("no users context")
		return gen.TableContext{}, nil
	}

	// wantBindingRefusal checks a listed edge's error is the discriminator
	// binding refusal, and that nothing reached a per-verb note.
	wantBindingRefusal := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("listed: ValidateNestedWriteEligibility() = nil, want the edge reported")
		}
		for _, want := range []string{`"Pinned" is not write-eligible`, `"kind"`, "types.JSON", "cannot be assigned to", "§9.9.4"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
		for _, unwanted := range []string{"occupancy read", "cannot carry its"} {
			if strings.Contains(err.Error(), unwanted) {
				t.Errorf("error %q mentions %q; the binding rule refuses the edge before any verb is considered", err, unwanted)
			}
		}
	}

	for _, tt := range []struct {
		name     string
		nullable bool
	}{
		{name: "NOT NULL FK", nullable: false},
		{name: "nullable FK", nullable: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			users, err := build(t, tt.nullable, false, nil)
			if err != nil {
				t.Errorf("auto-included: ValidateNestedWriteEligibility() = %v, want nil", err)
			}
			for _, name := range nestedEdgeNames(users) {
				if name == "Pinned" {
					t.Errorf("Pinned is write-eligible (edges: %v); its create could not set the discriminator", nestedEdgeNames(users))
				}
			}

			_, err = build(t, tt.nullable, true, nil)
			wantBindingRefusal(t, err)
		})
	}

	// With `create` masked off, the has-one clause would have reported the lost
	// `connect`. The binding rule refuses the edge first, so the error is the
	// same one.
	t.Run("create masked off", func(t *testing.T) {
		_, err := build(t, true, true, []string{"connect"})
		wantBindingRefusal(t, err)
	})
}
