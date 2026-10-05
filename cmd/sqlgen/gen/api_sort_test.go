package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

func renderSortTranslate(t *testing.T, ctx *gen.APIContext) string {
	t.Helper()
	tmpl := loadAPIAllTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/sort-translate", ctx); err != nil {
		t.Fatalf("rendering api/sort-translate: %v", err)
	}
	return buf.String()
}

func TestSortTranslator_ColumnLookup(t *testing.T) {
	// PRD §26.5.3 — per-table sort translator + <table>SortFieldToColumn
	// switch. The translator builds []sql.Sort by looking up SQL column
	// names through the generated switch and converting GraphQL
	// SortDirection values to sql.SortDirection.
	apiCtx := translatorAPIContext(t, config.DialectPostgres)
	out := renderSortTranslate(t, apiCtx)

	// Shared direction helper.
	mustContain(t, out, "func directionToSortDirection(d SortDirection) sql.SortDirection {")
	mustContain(t, out, `if string(d) == "DESC" {`)
	mustContain(t, out, "return sql.Desc")
	mustContain(t, out, "return sql.Asc")

	// Per-table translator references the gqlgen-generated input type and
	// dispatches through the per-table SortFieldToColumn switch.
	mustContain(t, out, "func translateProductSort(in []*ProductSort) []sql.Sort {")
	mustContain(t, out, "if len(in) == 0 {")
	mustContain(t, out, "out := make([]sql.Sort, 0, len(in))")
	mustContain(t, out, "for _, s := range in {")
	mustContain(t, out, "if s == nil {")
	mustContain(t, out, "Column:    productSortFieldToColumn(s.Field),")
	mustContain(t, out, "Direction: directionToSortDirection(s.Direction),")

	// Per-table column-resolution switch — every column on the schema
	// produces a case clause matched on the SCREAMING_SNAKE GraphQL enum
	// value and returning the SQL column name.
	mustContain(t, out, "func productSortFieldToColumn(f ProductSortField) string {")
	mustContain(t, out, "switch string(f) {")
	expected := []struct{ enumValue, sqlColumn string }{
		{"ID", "id"},
		{"NAME", "name"},
		{"DESCRIPTION", "description"},
		{"STOCK", "stock"},
		{"DISCOUNT", "discount"},
		{"ACTIVE", "active"},
		{"RELEASED_AT", "released_at"},
		{"DELETED_AT", "deleted_at"},
	}
	for _, e := range expected {
		caseLine := `case "` + e.enumValue + `":`
		retLine := `return "` + e.sqlColumn + `"`
		if !strings.Contains(out, caseLine) {
			t.Errorf("missing switch case %q in productSortFieldToColumn\n%s", caseLine, out)
		}
		if !strings.Contains(out, retLine) {
			t.Errorf("missing return %q in productSortFieldToColumn\n%s", retLine, out)
		}
	}
}

func TestSortTranslator_NoTablesNoEmission(t *testing.T) {
	// Empty APIContext — translator file body is just the shared direction
	// helper; no per-table switches.
	apiCtx := &gen.APIContext{} // zero — no tables
	out := renderSortTranslate(t, apiCtx)

	mustContain(t, out, "func directionToSortDirection")
	if strings.Contains(out, "translateProductSort") {
		t.Errorf("expected no per-table sort translator with empty APIContext\n%s", out)
	}
}
