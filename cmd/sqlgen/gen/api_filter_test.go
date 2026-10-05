package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

func renderFilterTranslate(t *testing.T, ctx *gen.APIContext) string {
	t.Helper()
	tmpl := loadAPIAllTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/filter-translate", ctx); err != nil {
		t.Fatalf("rendering api/filter-translate: %v", err)
	}
	return buf.String()
}

func TestFilterTranslator_AndOrNotNesting(t *testing.T) {
	// PRD §26.5.3 — and / or sub-filter recursion. The acceptance criterion
	// pins "3-level-deep" nesting works correctly. The generated translator
	// is recursive — it calls itself on each sub-filter — so any depth is
	// supported by construction. The codegen-layer assertion verifies the
	// structural shape: the make() pre-allocations, the for-loop bodies,
	// and the recursive translateProductFilter calls. End-to-end runtime
	// nesting is exercised in the §16.8 E2E example.
	apiCtx := translatorAPIContext(t, config.DialectPostgres)
	out := renderFilterTranslate(t, apiCtx)

	// Structural shape — entry point handles nil + initialises a non-nil
	// model filter so callers can rely on out being writable.
	mustContain(t, out, "func translateProductFilter(in *ProductFilter) *models.ProductFilter {")
	mustContain(t, out, "if in == nil {")
	mustContain(t, out, "return nil")
	mustContain(t, out, "out := &models.ProductFilter{}")

	// AND recursion — pre-allocates the slice and recurses on each sub-filter.
	mustContain(t, out, "if len(in.And) > 0 {")
	mustContain(t, out, "out.And = make([]*models.ProductFilter, 0, len(in.And))")
	mustContain(t, out, "for _, sub := range in.And {")
	mustContain(t, out, "out.And = append(out.And, translateProductFilter(sub))")

	// OR recursion — same shape as AND.
	mustContain(t, out, "if len(in.Or) > 0 {")
	mustContain(t, out, "out.Or = make([]*models.ProductFilter, 0, len(in.Or))")
	mustContain(t, out, "for _, sub := range in.Or {")
	mustContain(t, out, "out.Or = append(out.Or, translateProductFilter(sub))")

	// Defensive nil-skip on sub-entries (gqlgen permits nil within a slice).
	if got := strings.Count(out, "if sub == nil {"); got != 2 {
		t.Errorf("expected exactly 2 nil-skip guards (one per And/Or loop); got %d", got)
	}
}

func TestFilterTranslator_DeletedAtIsRegularFilterField(t *testing.T) {
	// The curated GraphQL surface does NOT carry a dedicated `includeDeleted`
	// flag — the soft-delete column is exposed in the filter input as a
	// regular comparator field. The runtime's existing soft-delete inject
	// (see template/table/get.go.tmpl:184) already short-circuits the
	// auto-injected "is not deleted" condition whenever the caller passes
	// any non-nil comparator on that column, so callers who want to include
	// deleted rows simply pass `deletedAt: {}` (or any operator they choose).
	// This test pins the filter translator surface so a future change can't
	// silently reintroduce a redundant flag.
	apiCtx := translatorAPIContext(t, config.DialectPostgres)
	out := renderFilterTranslate(t, apiCtx)

	// deletedAt is a regular dispatch — the same pattern as every other
	// nullable Time column.
	mustContain(t, out, "if in.DeletedAt != nil {")
	mustContain(t, out, "out.DeletedAt = translateNullableTimeComparator(in.DeletedAt)")

	// No bespoke includeDeleted handling.
	forbidden := []string{
		"in.IncludeDeleted",
		"out.IncludeDeleted",
		"&comparator.NullableTime{}",
	}
	for _, sym := range forbidden {
		if strings.Contains(out, sym) {
			t.Errorf("filter translator must not reference %q (soft-delete bypass is delegated to the runtime via the regular DeletedAt comparator field)\n%s", sym, out)
		}
	}
}

func TestFilterTranslator_SkipsPKAndUnsupportedComparators(t *testing.T) {
	// PRD §26.5.3 — PK columns excluded from the GraphQL filter input
	// (per the schema template) so the translator must skip them. The
	// schema's `id` column is the PK; no `out.ID = ...` should appear.
	apiCtx := translatorAPIContext(t, config.DialectPostgres)
	out := renderFilterTranslate(t, apiCtx)

	if strings.Contains(out, "out.ID = translate") {
		t.Errorf("PK column ID must not appear in filter translator body\n%s", out)
	}

	// Per-column field-level translation — non-PK columns dispatch through
	// the appropriate per-family translator.
	mustContain(t, out, "if in.Name != nil {")
	mustContain(t, out, "out.Name = translateStringComparator(in.Name)")
	mustContain(t, out, "if in.Description != nil {")
	mustContain(t, out, "out.Description = translateNullableStringComparator(in.Description)")
	mustContain(t, out, "if in.Stock != nil {")
	mustContain(t, out, "if in.Active != nil {")
	mustContain(t, out, "out.Active = translateBooleanComparator(in.Active)")
	mustContain(t, out, "if in.ReleasedAt != nil {")
	mustContain(t, out, "out.ReleasedAt = translateTimeComparator(in.ReleasedAt)")
	mustContain(t, out, "if in.DeletedAt != nil {")
	mustContain(t, out, "out.DeletedAt = translateNullableTimeComparator(in.DeletedAt)")
}

func TestFilterTranslator_NoSoftDeleteWorksUnchanged(t *testing.T) {
	// Schema with no soft-delete column — the translator's body remains the
	// same shape (per-column dispatch + And/Or recursion). No special-casing
	// for soft-delete columns is needed since they're absent.
	in := apiTestInput(t, resolverTestSchema()) // schema without deleted_at
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	apiCtx.ModelsPackage = "models"
	apiCtx.ModelsImportPath = "example.com/foo/gen"

	out := renderFilterTranslate(t, apiCtx)

	mustContain(t, out, "func translateProductFilter(in *ProductFilter) *models.ProductFilter {")
	if strings.Contains(out, "IncludeDeleted") {
		t.Errorf("filter translator must not reference IncludeDeleted (curated surface does not expose the flag)\n%s", out)
	}
}
