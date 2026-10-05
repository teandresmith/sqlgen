package gen_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// Per-view emission — the artifacts PRD §26.4's view row promises, held
// against what the templates actually render.
//
// Views join APIContext.Tables, so every downstream template that iterates
// that slice picks them up with no per-view arm. What that alone cannot do is
// state the emitted set as a closed list and check it:
// `TestAPIViewSchema_emitsReadOnlySurface` asserts the members are present,
// not that nothing else is. These tests close the set from both ends.

// viewEmissionSchema pairs a table with a view rich enough to reach the
// richer comparator families: a schema enum, a jsonb document, and a
// nullable column for the `isNull` half of PRD §26.4 Rule 2.
func viewEmissionSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{{
			Name: "notes",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "kind", Type: "note_kind"},
				{Name: "labels", Type: "jsonb"},
			},
		}},
		Enums: []parser.Enum{{Name: "note_kind", Schema: "public", Values: []string{"draft", "published"}}},
		Views: []parser.View{{
			Name: "note_summary",
			Columns: []parser.Column{
				apiViewCol("id", "uuid", false, true),
				apiViewCol("kind", "note_kind", false, false),
				apiViewCol("labels", "jsonb", false, false),
				apiViewCol("body", "text", true, false),
				apiViewCol("document_count", "integer", false, false),
			},
		}},
	}
}

// buildViewEmissionContext builds the API context for viewEmissionSchema with
// the schema enums wired onto the resolver the way the Generate pipeline's
// registerSchemaTypes does. Without that step an enum column resolves to a
// plain `string` and the filter falls back to StringComparator — the
// advertised-but-dropped shape enum monomorphization removed — so the enum
// assertions below would pass against the wrong thing.
func buildViewEmissionContext(t *testing.T, schema *parser.Schema) *gen.APIContext {
	t.Helper()
	in, enums := enumFixtureInput(t, schema)
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	views, err := gen.BuildViewContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildViewContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, views, enums, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	apiCtx.ModelsPackage = in.Config.Output.Package
	return apiCtx
}

// declaredTypeNames extracts every GraphQL type name a rendered schema
// DECLARES. `extend type Query` is deliberately excluded — extending a type is
// not claiming its name, and Query is one of the four structural names
// shared_gen.graphqls owns for the whole project.
func declaredTypeNames(t *testing.T, schema string) []string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^(type|input|enum|scalar|interface|union) ([A-Za-z_][A-Za-z0-9_]*)`)
	matches := re.FindAllStringSubmatch(schema, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[2])
	}
	slices.Sort(out)
	return out
}

// TestAPIViewSchema_declaresExactlySevenNames closes PRD §26.4's emitted-set
// table from the "and nothing more" side. Seven declarations per view, no
// eighth — an added `Create<V>Input` or a stray helper input would fail here
// even if every `mustContain` in api_view_test.go still passed.
func TestAPIViewSchema_declaresExactlySevenNames(t *testing.T) {
	apiCtx := buildViewEmissionContext(t, viewEmissionSchema())
	v := findAPIEntity(t, apiCtx, "NoteSummary")
	if v == nil {
		t.Fatal("view NoteSummary absent from the API context")
	}

	got := declaredTypeNames(t, renderAPITableSchema(t, *v))
	want := []string{
		"NoteSummary",
		"NoteSummaryConnection",
		"NoteSummaryEdge",
		"NoteSummaryFilter",
		"NoteSummaryListResult",
		"NoteSummarySort",
		"NoteSummarySortField",
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("declared type names = %v, want %v", got, want)
	}
}

// TestAPIViewSchema_emissionMatchesTheClaimSet pins the claim-set
// contract: every name the view emits must be one collectGraphQLTypeNames
// claims for that same view, and every name it claims must be emitted. The two
// lists live in different files derived from different inputs — the schema
// template walks APITableContext, the claim set walks APIContext — and nothing
// but this test forces them to agree. A drift in either direction is a
// name-collision failure: a name emitted but unclaimed collides silently and
// surfaces as gqlparser's `Cannot redeclare type X` naming neither claimant; a
// name claimed but unemitted reserves a word no one uses.
func TestAPIViewSchema_emissionMatchesTheClaimSet(t *testing.T) {
	apiCtx := buildViewEmissionContext(t, viewEmissionSchema())
	v := findAPIEntity(t, apiCtx, "NoteSummary")
	if v == nil {
		t.Fatal("view NoteSummary absent from the API context")
	}

	claims := gen.ClaimedGraphQLNamesForTest(apiCtx)
	emitted := declaredTypeNames(t, renderAPITableSchema(t, *v))

	for _, name := range emitted {
		owner, ok := claims[name]
		if !ok {
			t.Errorf("view emits type %q but collectGraphQLTypeNames claims no such name — a collision on it would name neither claimant", name)
			continue
		}
		if owner != "view note_summary" {
			t.Errorf("type %q is emitted by the view but claimed by %s", name, owner)
		}
	}

	for name, owner := range claims {
		if owner != "view note_summary" {
			continue
		}
		if !slices.Contains(emitted, name) {
			t.Errorf("collectGraphQLTypeNames claims %q for the view, but the schema template emits no such type", name)
		}
	}
}

// TestAPIViewSchema_filterCarriesTheFullFamilySet pins the acceptance criterion
// that a view's <V>Filter reaches the richer comparator families. The
// two that need this fixture to be reached are the enum family
// (monomorphized per enum) and the JSONB family (PostgreSQL-gated); `body` is nullable so the isNull half of Rule 2 shows up
// as the Nullable twin.
func TestAPIViewSchema_filterCarriesTheFullFamilySet(t *testing.T) {
	apiCtx := buildViewEmissionContext(t, viewEmissionSchema())
	v := findAPIEntity(t, apiCtx, "NoteSummary")
	if v == nil {
		t.Fatal("view NoteSummary absent from the API context")
	}
	out := renderAPITableSchema(t, *v)

	for _, want := range []string{
		"kind: NoteKindComparator",
		"labels: JSONBComparator",
		"body: NullableStringComparator",
		"documentCount: NumericComparator",
	} {
		mustContain(t, out, want)
	}
}

// renderViewWalker renders api/field-options over a context that carries both a
// table and a view, and returns the body.
func renderViewWalker(t *testing.T, schema *parser.Schema) string {
	t.Helper()
	apiCtx := buildViewEmissionContext(t, schema)
	apiCtx.ModelsImportPath = "example.com/foo/gen"
	apiCtx.ClientName = "Client"

	tmpl := loadAPIAllTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/field-options", apiCtx); err != nil {
		t.Fatalf("rendering api/field-options: %v", err)
	}
	return buf.String()
}

// TestViewWalker_ColumnsOnly pins PRD §26.4's closing sentence — "a view has no
// relationships, so its object type is columns-only, its walker has no
// relationship cases, and a view read is exactly one query under §25.1".
//
// The walker is where that guarantee is actually made: every relationship case
// in a walker is a fan-out the §25.1 formula charges for, so a view walker that
// grew one would break the one-query claim silently, at runtime, in query
// count only. The assertion is therefore both directions — a case per readable
// column, and no `RelationshipOptions` anywhere in the view's arm.
func TestViewWalker_ColumnsOnly(t *testing.T) {
	// The table half carries a relationship so the test can tell "the view has
	// no relationship cases" from "the template emits no relationship cases".
	schema := viewEmissionSchema()
	schema.Tables = append(schema.Tables, parser.Table{
		Name: "note_comments",
		Columns: []parser.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "note_id", Type: "uuid"},
		},
	})
	schema.Relationships = []parser.Relationship{{
		Name:        "comments",
		Type:        parser.OneToMany,
		SourceTable: "notes",
		TargetTable: "note_comments",
		FKColumn:    "note_id",
	}}

	out := renderViewWalker(t, schema)

	viewArm := walkerFunc(t, out, "noteSummaryFieldOptionsFromCollected")
	for _, want := range []string{
		`case "id":`,
		`case "kind":`,
		`case "labels":`,
		`case "body":`,
		`case "documentCount":`,
	} {
		if !strings.Contains(viewArm, want) {
			t.Errorf("view walker has no %s\n%s", want, viewArm)
		}
	}
	if got, want := strings.Count(viewArm, "case \""), 5; got != want {
		t.Errorf("view walker has %d cases, want %d (one per API-readable column, and nothing else)\n%s", got, want, viewArm)
	}
	if strings.Contains(viewArm, "RelationshipOptions") {
		t.Errorf("view walker descends into a relationship — a view has none (PRD §16.4), and each one would cost a query under §25.1\n%s", viewArm)
	}

	// Control: the table's walker in the same render DOES carry its
	// relationship case, so the absence above is the view's shape and not a
	// template that stopped emitting relationships altogether.
	tableArm := walkerFunc(t, out, "noteFieldOptionsFromCollected")
	if !strings.Contains(tableArm, "RelationshipOptions") {
		t.Errorf("the table walker lost its relationship case, so the view assertion above proves nothing\n%s", tableArm)
	}

	// The view's entry point still exists — columns-only is not "not emitted".
	mustContain(t, out, "func noteSummaryFieldOptionsFromContext(")
}

// walkerFunc slices one generated walker function out of the rendered file so
// an assertion about "the view's arm" cannot accidentally read a table's.
func walkerFunc(t *testing.T, out, name string) string {
	t.Helper()
	start := strings.Index(out, "func "+name+"(")
	if start < 0 {
		t.Fatalf("rendered walker has no func %s\n%s", name, out)
	}
	rest := out[start:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// TestAPIViewSchema_materializedIsIdentical pins PRD §26.4's materialized-view
// sentence as an equality rather than a checklist: a matview emits the
// *identical* read surface, and `Refresh` / `RefreshConcurrently` — which the
// matview's Go client does expose — appear nowhere in it.
//
// The mechanism that makes this true is that APITableContext carries no
// materialization flag at all: `Materialized` stops at ViewContext, which is
// the Go-client half. Rendering the same view both ways is what keeps it true —
// the moment the flag were threaded onto the API context "just for a comment",
// the two outputs would diverge and this test would say so.
func TestAPIViewSchema_materializedIsIdentical(t *testing.T) {
	render := func(materialized bool) string {
		t.Helper()
		schema := viewEmissionSchema()
		schema.Views[0].Materialized = materialized
		schema.Views[0].ConcurrentlyRefreshable = materialized // @pk present

		apiCtx := buildViewEmissionContext(t, schema)
		v := findAPIEntity(t, apiCtx, "NoteSummary")
		if v == nil {
			t.Fatal("view NoteSummary absent from the API context")
		}
		return renderAPITableSchema(t, *v)
	}

	regular, matview := render(false), render(true)
	if regular != matview {
		t.Errorf("a materialized view's read surface differs from a regular view's\n--- regular ---\n%s\n--- materialized ---\n%s", regular, matview)
	}
	if strings.Contains(strings.ToLower(matview), "refresh") {
		t.Errorf("the matview's schema mentions refresh — Refresh / RefreshConcurrently are Go-client-only (PRD §26.4)\n%s", matview)
	}
}
