package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/sql"
)

// Required-column regression coverage. `<T>FieldOptions.Columns()` narrows
// the SELECT to what the caller asked for, but five consumers read a column the
// caller never asks for: Connection encodes each edge cursor from the row, the
// O2M / M2M loaders map children back through the parent key, the O2O scan
// decides a LEFT JOIN miss by testing the target PK against its zero value, the
// M2M loader keys its target map on the target PK, and the O2M loader buckets
// children on the child's FK. Each of those columns is unioned into the
// projection at its own emission site; the tests below pin all five, plus the
// guard that keeps an all-false FieldOptions short-circuiting (PRD §9.6).

// TestRequiredColumns_connectionDeclaresCursorKeys pins the table-side half:
// Connection hands its cursor keys to GetMany through the input struct's
// unexported requiredColumns channel, the same way it already hands over
// keyset conditions.
func TestRequiredColumns_connectionDeclaresCursorKeys(t *testing.T) {
	ctx := testPaginationContext_postgres()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, "requiredColumns: c.cursorKeys,") {
		t.Errorf("Connection must declare its cursor keys to GetMany — without them a\n"+
			"narrowed FieldOptions leaves every edge cursor at its zero value\n\nfull output:\n%s", output)
	}
}

// TestRequiredColumns_viewConnectionDeclaresCursorKeys is the view-side half.
// The two pagination templates are the same code twice (design §1.3), so a
// one-sided fix would reintroduce exactly the divergence that was removed.
func TestRequiredColumns_viewConnectionDeclaresCursorKeys(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewPaginationTemplate(t, ctx)

	if !strings.Contains(output, "requiredColumns: c.cursorKeys,") {
		t.Errorf("view Connection must declare its cursor keys to GetMany\n\nfull output:\n%s", output)
	}
}

// TestRequiredColumns_getManyUnionsDeclaredColumns pins the receiving end on
// both templates: a narrowed projection is widened by whatever the caller
// declared, and only when the narrowed set is non-empty — an all-false
// FieldOptions still returns early (PRD §9.6).
func TestRequiredColumns_getManyUnionsDeclaredColumns(t *testing.T) {
	tests := []struct {
		name   string
		output string
	}{
		{"table", executeGetTemplate(t, testGetContext_singlePK())},
		{"view", executeViewGetTemplate(t, testViewContext_withPK())},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.output, "columns = unionColumns(selected, input.requiredColumns...)") {
				t.Errorf("GetMany must union the caller's required columns into a narrowed projection\n\nfull output:\n%s", tt.output)
			}
			// The union sits inside the len(selected) > 0 arm, so an all-false
			// FieldOptions never reaches it and keeps short-circuiting above.
			if !strings.Contains(tt.output, "if fieldOptions != nil && !fieldOptions.HasSelectedColumns() {") {
				t.Errorf("the all-false FieldOptions short-circuit must survive (PRD §9.6)\n\nfull output:\n%s", tt.output)
			}
		})
	}
}

// TestRequiredColumns_getManyUnionsParentKeyForRelationships pins the O2M /
// M2M half: loadRelationships collects the parent side of every mapping off
// the scanned row, so the parent key joins the projection whenever a
// relationship is selected.
func TestRequiredColumns_getManyUnionsParentKeyForRelationships(t *testing.T) {
	output := executeGetTemplate(t, testGetContext_withRelationships())

	wantPatterns := []string{
		"if fieldOptions.loadsRelationships() {",
		`columns = unionColumns(columns, "id")`,
		"func (fo *ProductFieldOptions) loadsRelationships() bool {",
		"if fo.Reviews != nil {",
		"if fo.Tags != nil {",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// TestRequiredColumns_noRelationshipsNoParentKeyUnion is the negative
// complement: a table with no O2M / M2M relationship has no mapping to feed,
// so it emits neither the predicate nor the union and its generated output is
// unchanged.
func TestRequiredColumns_noRelationshipsNoParentKeyUnion(t *testing.T) {
	output := executeGetTemplate(t, testGetContext_singlePK())

	for _, unwanted := range []string{"loadsRelationships()", `unionColumns(columns, "id")`} {
		if strings.Contains(output, unwanted) {
			t.Errorf("relationship-free table should not emit %q\n\nfull output:\n%s", unwanted, output)
		}
	}
}

// TestRequiredColumns_o2oJoinCarriesTargetPK pins the O2O half. The join's
// column list is built from the target's own FieldOptions, and the scan reads
// the target PK to tell a real row from a LEFT JOIN miss — so an unselected PK
// made a present child come back nil.
func TestRequiredColumns_o2oJoinCarriesTargetPK(t *testing.T) {
	output := executeRelationshipsTemplate(t, testRelationshipsContext_basicO2O())

	if !strings.Contains(output, `Columns: unionColumns(fo.Company.Columns(), "id"),`) {
		t.Errorf("O2O join must carry the target PK so NULL detection has a value to test\n\nfull output:\n%s", output)
	}
	// NULL detection is what makes the column load-bearing.
	if !strings.Contains(output, "if cHasData && c.ID != uuid.Nil {") {
		t.Errorf("expected the PK-based LEFT JOIN miss check\n\nfull output:\n%s", output)
	}
}

// TestRequiredColumns_chainedO2OJoinCarriesTargetPK covers the chained shape:
// every level of an O2O chain runs its own NULL detection, so every level's
// join carries its own target PK.
func TestRequiredColumns_chainedO2OJoinCarriesTargetPK(t *testing.T) {
	output := executeRelationshipsTemplate(t, testRelationshipsContext_chained())

	if !strings.Contains(output, `Columns: unionColumns(fo.Company.Country.Columns(), "id"),`) {
		t.Errorf("chained O2O join must carry its own target PK\n\nfull output:\n%s", output)
	}
}

// TestRequiredColumns_m2mDeclaresTargetPK pins the M2M half: step 3 keys every
// loaded target on the target's PK field, so the target fetch declares that
// column through the same requiredColumns channel Connection uses — spelled
// from the target's own PK, not the parent's.
func TestRequiredColumns_m2mDeclaresTargetPK(t *testing.T) {
	output := executeGetTemplate(t, testGetContext_withRelationships())

	wantPatterns := []string{
		`requiredColumns: []string{"id"},`,
		"co.FieldOptions = fo.Tags.FieldOptions",
		"range parentsByTarget[t.ID.String()]",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// TestRequiredColumns_o2mDeclaresChildFK is the O2M half, converted from a
// force-select: the loader buckets children on the child's FK, so
// the child fetch declares that column through the same requiredColumns channel
// Connection and the M2M loader use — spelled with the FK's SQL name, which a
// column_map override can separate from FKFieldName.
func TestRequiredColumns_o2mDeclaresChildFK(t *testing.T) {
	output := executeGetTemplate(t, testGetContext_withRelationships())

	wantPatterns := []string{
		`requiredColumns: []string{"product_id"},`,
		"co.FieldOptions = fo.Reviews.FieldOptions",
		"byFK := make(map[string][]*Review)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// TestRequiredColumns_noFieldOptionsMutation is the design constraint behind
// the mechanism choice: the requirement travels on the input struct, so no
// generated read path writes to a FieldOptions the caller owns. Both list
// loaders would be tempted — two relationships to the same target can share one
// *FieldOptions pointer, and the loaders run concurrently under errgroup, so an
// in-place flag write is a data race as well as a lingering side effect. The
// O2M half held that shape until both consequences were measured against a
// live container; the assertions below are the guard for each loader.
func TestRequiredColumns_noFieldOptionsMutation(t *testing.T) {
	output := executeGetTemplate(t, testGetContext_withRelationships())

	unwanted := []struct {
		loader string
		expr   string
	}{
		{"M2M", "relFieldOptions.ID = true"},
		{"O2M", "relFieldOptions.ProductID = true"},
	}
	for _, u := range unwanted {
		if strings.Contains(output, u.expr) {
			t.Errorf("%s loader must not mutate the caller's FieldOptions (found %q)\n\nfull output:\n%s",
				u.loader, u.expr, output)
		}
	}
	// Neither loader may introduce the local alias the mutation hung off, in
	// any spelling: the child FieldOptions is forwarded to the fetch as the
	// caller handed it over.
	if strings.Contains(output, "relFieldOptions") {
		t.Errorf("no loader may bind a mutable alias of the caller's FieldOptions\n\nfull output:\n%s", output)
	}
}
