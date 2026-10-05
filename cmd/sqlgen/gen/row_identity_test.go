package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// Row-identity codegen coverage. A GraphQL selection set that asks for a row but
// names no column — `__typename` alone, an edge selecting only its `cursor`, a
// `pageInfo` sub-selection — must not walk to an all-false FieldOptions, which
// PRD §9.6 reads as "the caller wants no rows" and short-circuits. The walker
// separates that from the one selection that genuinely wants none (a
// count-only envelope) and projects the row's identity for everything else.
//
// The runtime half lives in the `graphql` example's
// tests/walker_row_identity_test.go, one E2E per measured symptom.

// renderRowIdentityWalker renders api/field-options for a schema whose tables
// and views both matter, so the per-entity emission can be asserted for each.
func renderRowIdentityWalker(t *testing.T, in *gen.GenerateInput) string {
	t.Helper()
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	views, err := gen.BuildViewContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildViewContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, views, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	apiCtx.ModelsPackage = "models"
	apiCtx.ModelsImportPath = "example.com/foo/gen"
	apiCtx.ClientName = "Client"

	tmpl := loadAPIAllTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/field-options", apiCtx); err != nil {
		t.Fatalf("rendering api/field-options: %v", err)
	}
	return buf.String()
}

// walkerBody returns the rendered source of one generated function, so an
// assertion about `productFieldOptionsFromCollected` cannot be satisfied by a
// line that belongs to a different entity's walker.
func walkerBody(t *testing.T, out, funcName string) string {
	t.Helper()
	start := strings.Index(out, "func "+funcName+"(")
	if start < 0 {
		t.Fatalf("function %s absent from rendered walker:\n%s", funcName, out)
	}
	rest := out[start:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end+3]
	}
	return rest
}

// TestRowIdentity_EntryPointSkipsWalkOnCountOnlyEnvelope pins the half of the
// fix that preserves the §25.1 one-query count-only read: when
// unwrapConnectionFields reports that the envelope selected no row-bearing
// field, the entry point returns the all-false FieldOptions without walking, so
// nothing downstream can force a projection onto it.
func TestRowIdentity_EntryPointSkipsWalkOnCountOnlyEnvelope(t *testing.T) {
	out := renderRowIdentityWalker(t, apiTestInput(t, walkerFixtureSchema()))
	body := walkerBody(t, out, "productFieldOptionsFromContext")

	mustContain(t, body, `entityFields, wantsRows := unwrapConnectionFields(ctx, fields, "ProductConnection", "ProductListResult")`)
	mustContain(t, body, "if !wantsRows {")
	mustContain(t, body, "return &models.ProductFieldOptions{}")
	mustContain(t, body, "return productFieldOptionsFromCollected(opCtx, entityFields)")
}

// TestRowIdentity_CollectedProjectsRowIdentity pins the other half: the
// recursive walker — the step that serves nested relationship selections as
// well as the top level — ends by projecting the row identity when the
// selection named no column.
func TestRowIdentity_CollectedProjectsRowIdentity(t *testing.T) {
	out := renderRowIdentityWalker(t, apiTestInput(t, walkerFixtureSchema()))
	body := walkerBody(t, out, "productFieldOptionsFromCollected")

	mustContain(t, body, "if !fo.HasSelectedColumns() {")
	// `fo.ID = true` also appears in the `case "id":` arm, so the count is what
	// distinguishes "the guard projects it" from "only the case arm does".
	if got := strings.Count(body, "fo.ID = true"); got != 2 {
		t.Errorf("expected 2 `fo.ID = true` (the id case arm + the row-identity guard); got %d\n%s", got, body)
	}
	// Only the identity — a non-PK column must not be dragged in with it.
	if strings.Count(body, "fo.Name = true") != 1 {
		t.Errorf("row-identity guard must project the PK only, not every column\n%s", body)
	}
}

// TestRowIdentity_CompositePKProjectsEveryKeyColumn: a composite PK is the
// row's identity as a tuple, so every column of it is projected. A partial
// projection would still clear the short-circuit but would not identify a row.
func TestRowIdentity_CompositePKProjectsEveryKeyColumn(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{{
			Name: "user_categories",
			Columns: []parser.Column{
				{Name: "user_id", Type: "uuid", PrimaryKey: true},
				{Name: "category_id", Type: "uuid", PrimaryKey: true},
				{Name: "note", Type: "text", Nullable: true},
			},
		}},
	}
	out := renderRowIdentityWalker(t, apiTestInput(t, schema))
	body := walkerBody(t, out, "userCategoryFieldOptionsFromCollected")

	mustContain(t, body, "if !fo.HasSelectedColumns() {")
	for _, field := range []string{"fo.UserID = true", "fo.CategoryID = true"} {
		if got := strings.Count(body, field); got != 2 {
			t.Errorf("expected 2 `%s` (case arm + row-identity guard); got %d\n%s", field, got, body)
		}
	}
	if strings.Count(body, "fo.Note = true") != 1 {
		t.Errorf("row-identity guard must project the PK tuple only\n%s", body)
	}
}

// TestRowIdentity_ViewWithoutPKFallsBackToCursorKeys: a view is the one entity
// kind that may have no `@pk` (PRD §16.4), so the PK cannot be the universal
// answer. Its resolved cursor keys are the closest thing it has to an identity.
func TestRowIdentity_ViewWithoutPKFallsBackToCursorKeys(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{{
			Name: "products",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "category_id", Type: "uuid"},
			},
		}},
		Views: []parser.View{{
			Name: "category_price_totals",
			Columns: []parser.Column{
				// No PK column: the view is an aggregate, exactly like the
				// matview fixture in the `graphql` example.
				apiViewCol("category_id", "uuid", false, false),
				apiViewCol("product_count", "integer", false, false),
			},
		}},
	}
	in := apiTestInput(t, schema)
	in.Config.Views["category_price_totals"] = config.ViewConfig{CursorKeys: []string{"category_id"}}

	out := renderRowIdentityWalker(t, in)
	body := walkerBody(t, out, "categoryPriceTotalFieldOptionsFromCollected")

	mustContain(t, body, "if !fo.HasSelectedColumns() {")
	if got := strings.Count(body, "fo.CategoryID = true"); got != 2 {
		t.Errorf("expected 2 `fo.CategoryID = true` (case arm + row-identity guard); got %d\n%s", got, body)
	}
	if strings.Count(body, "fo.ProductCount = true") != 1 {
		t.Errorf("row-identity guard must project the cursor key only\n%s", body)
	}
}

// TestRowIdentity_ViewWithoutPKOrCursorKeysFallsBackToFirstColumn covers the
// last branch of the chain. A view with no `@pk` whose inherited
// `generation.cursor_keys` do not exist on its projection resolves neither of
// the first two answers — §4.13 gives it no Connection either — but it still
// has a `<V>List` whose `items` selection needs a projection.
func TestRowIdentity_ViewWithoutPKOrCursorKeysFallsBackToFirstColumn(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{{
			Name: "products",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "category_id", Type: "uuid"},
			},
		}},
		Views: []parser.View{{
			Name: "category_rollups",
			Columns: []parser.Column{
				// No `id`, so the inherited generation.cursor_keys (["id"] —
				// see testInput) does not resolve, and no `@pk` either.
				apiViewCol("category_id", "uuid", false, false),
				apiViewCol("product_count", "integer", false, false),
			},
		}},
	}
	in := apiTestInput(t, schema)

	views, err := gen.BuildViewContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildViewContexts: %v", err)
	}
	// Guard the premise: if either of the first two branches resolved, this
	// test would silently stop covering the third.
	for _, vc := range views {
		if vc.ViewName != "category_rollups" {
			continue
		}
		if vc.HasPK {
			t.Fatal("fixture is wrong: the view resolved a PK")
		}
		if len(vc.CursorKeys) != 0 {
			t.Fatalf("fixture is wrong: the view resolved cursor keys %v", vc.CursorKeys)
		}
	}

	out := renderRowIdentityWalker(t, in)
	body := walkerBody(t, out, "categoryRollupFieldOptionsFromCollected")

	mustContain(t, body, "if !fo.HasSelectedColumns() {")
	if got := strings.Count(body, "fo.CategoryID = true"); got != 2 {
		t.Errorf("expected 2 `fo.CategoryID = true` (case arm + row-identity guard); got %d\n%s", got, body)
	}
	if strings.Count(body, "fo.ProductCount = true") != 1 {
		t.Errorf("row-identity guard must project one column, not every column\n%s", body)
	}
}

// TestRowIdentity_ConnectionWalkerReportsPageInfo pins the trigger the filed
// entry originally under-specified: `pageInfo` is row-bearing in full, not just
// its two cursor fields. hasNextPage is read off the limit+1 probe row, so a
// `pageInfo`-only connection query that skipped the fetch reported `false` on a
// page that has a successor.
func TestRowIdentity_ConnectionWalkerReportsPageInfo(t *testing.T) {
	out := renderConnectionWalker(t)

	mustContain(t, out, `case "pageInfo":`)
	// Each of the three row-bearing envelope keys sets wantsRows; nothing else
	// in the helper does, so `{ totalCount }` alone still reports false.
	if got := strings.Count(out, "wantsRows = true"); got != 3 {
		t.Errorf("expected 3 `wantsRows = true` (edges, pageInfo, items); got %d\n%s", got, out)
	}
	mustContain(t, out, "wantsRows := false")
	mustContain(t, out, "return entityFields, wantsRows")
}

// TestRowIdentity_EveryAPIEntityCarriesRowIdentity guards the context half: an
// entity with no RowIdentityFields renders a walker that can still produce an
// all-false FieldOptions for a requested row, silently reopening this bug for
// that one entity.
func TestRowIdentity_EveryAPIEntityCarriesRowIdentity(t *testing.T) {
	schema := viewEmissionSchema()
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
	if len(apiCtx.Tables) == 0 {
		t.Fatal("fixture produced no API entities")
	}
	for _, at := range apiCtx.Tables {
		if len(at.RowIdentityFields) == 0 {
			t.Errorf("%s (%s) has no RowIdentityFields", at.StructName, at.SQLTable)
		}
	}
}

// Row-identity lint coverage. rowIdentityFields resolves its answer from PKColumns,
// a view's CursorKeys, then Columns[0] — none of which consults §32.2 — so
// "every field the row-identity block projects is one the walker also emits a
// case for" would hold only by coincidence, not by construction. The lint's third
// direction is what states it.

// TestRowIdentity_LintRejectsRestrictedPK drives the guard through the real
// resolver chain rather than a hand-built context: §32.4 rejects a restricted
// PK at config-validation time, but the context builders do not re-run that
// validation, so `column_map.id.access` reaches rowIdentityFields intact here
// and the walker would otherwise project a column it emits no case for.
func TestRowIdentity_LintRejectsRestrictedPK(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{{
			Name: "widgets",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "label", Type: "text"},
			},
		}},
	}
	in := apiTestInput(t, schema)
	in.Config.Tables["widgets"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			"id": {Access: config.AccessInternal},
		},
	}

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	// Guard the premise: if the builder started filtering the PK by access,
	// this test would stop covering the lint and silently pass.
	for _, tc := range tables {
		for _, col := range tc.Columns {
			if col.Name == "id" && col.APIReadable {
				t.Fatal("fixture is wrong: the restricted PK resolved as API-readable")
			}
		}
	}

	_, err = gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err == nil {
		t.Fatal("BuildAPIContext accepted a walker projecting a restricted PK as row identity")
	}
	for _, want := range []string{"widgets", `"id"`, "row identity"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must mention %s; got %q", want, err)
		}
	}
}

// TestRowIdentity_LintDirections covers the branches the resolver cannot be
// driven into from a config: a view has no `column_map`, so every view column
// is public by construction (context_view.go) and its cursor-key / first-column
// fallbacks can only be made restricted by hand. The entities are built the way
// api_lint_test.go builds them for the filter lint.
func TestRowIdentity_LintDirections(t *testing.T) {
	tableEntity := gen.APIEntityFromTable(gen.TableContext{
		TableName: "users",
		Columns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", Access: "internal"},
			{Name: "email", FieldName: "Email", Access: "public", APIReadable: true},
		},
	})
	viewEntity := gen.APIEntityFromView(gen.ViewContext{
		ViewName:   "category_totals",
		StructName: "CategoryTotal",
		Columns: []gen.ColumnContext{
			{Name: "category_id", FieldName: "CategoryID", Access: "hidden"},
			{Name: "total", FieldName: "Total", Access: "public", APIReadable: true},
		},
	})
	tableFields := []gen.APIFieldContext{
		{SQLName: "id", GoFieldName: "ID", Readable: false},
		{SQLName: "email", GoFieldName: "Email", Readable: true},
	}
	viewFields := []gen.APIFieldContext{
		{SQLName: "category_id", GoFieldName: "CategoryID", Readable: false},
		{SQLName: "total", GoFieldName: "Total", Readable: true},
	}

	tests := []struct {
		name     string
		src      gen.APIEntity
		apiTable gen.APITableContext
		wantErr  string
	}{
		{
			name:     "readable identity passes alongside restricted columns",
			src:      tableEntity,
			apiTable: gen.APITableContext{StructName: "User", Fields: tableFields, RowIdentityFields: []string{"Email"}},
		},
		{
			name:     "restricted PK branch rejected",
			src:      tableEntity,
			apiTable: gen.APITableContext{StructName: "User", Fields: tableFields, RowIdentityFields: []string{"ID"}},
			wantErr:  `projects access-restricted column "id" as its row identity`,
		},
		{
			name:     "restricted view cursor-key branch rejected",
			src:      viewEntity,
			apiTable: gen.APITableContext{StructName: "CategoryTotal", IsView: true, Fields: viewFields, RowIdentityFields: []string{"CategoryID"}},
			wantErr:  `projects access-restricted column "category_id" as its row identity`,
		},
		{
			// A multi-entry identity — a composite PK, or a view whose cursor
			// keys resolve to several columns — must be checked past its first
			// entry, so the restricted one is placed second here.
			name:     "restricted column later in a multi-entry identity rejected",
			src:      viewEntity,
			apiTable: gen.APITableContext{StructName: "CategoryTotal", IsView: true, Fields: viewFields, RowIdentityFields: []string{"Total", "CategoryID"}},
			wantErr:  `projects access-restricted column "category_id" as its row identity`,
		},
		{
			name:     "identity naming no column of the entity rejected",
			src:      tableEntity,
			apiTable: gen.APITableContext{StructName: "User", Fields: tableFields, RowIdentityFields: []string{"Nickname"}},
			wantErr:  `projects row-identity field "Nickname", which names no column of the entity`,
		},
		{
			name:     "empty identity rejected while a column is readable",
			src:      tableEntity,
			apiTable: gen.APITableContext{StructName: "User", Fields: tableFields},
			wantErr:  "has API-readable columns but projects no row identity",
		},
		{
			name: "entity with no readable column is exempt",
			src: gen.APIEntityFromTable(gen.TableContext{
				TableName: "notes",
				Columns:   []gen.ColumnContext{{Name: "id", FieldName: "ID", Access: "hidden"}},
			}),
			apiTable: gen.APITableContext{
				StructName:        "Note",
				Fields:            []gen.APIFieldContext{{SQLName: "id", GoFieldName: "ID", Readable: false}},
				RowIdentityFields: []string{"ID"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gen.ValidateAPIWalkerCompleteness(tt.src, tt.apiTable, nil)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateAPIWalkerCompleteness() unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateAPIWalkerCompleteness() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
