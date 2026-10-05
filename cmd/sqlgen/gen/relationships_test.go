package gen_test

import (
	"os"
	"path/filepath"
	"strconv"
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

func loadRelationshipsTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("relationships.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", "relationships.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing relationships template: %v", err)
	}
	return tmpl
}

func executeRelationshipsTemplate(t *testing.T, ctx gen.TableContext) string {
	t.Helper()
	tmpl := loadRelationshipsTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/relationships", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	body := strings.TrimSpace(buf.String())
	if body == "" {
		return ""
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

func testRelationshipsContext_basicO2O() gen.TableContext {
	return gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Description:       "Catalog of products available for sale.",
		Package:           "db",
		VarName:           "p",
		Imports: []string{
			"context",
			"fmt",
			"github.com/gofrs/uuid/v5",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		Columns: []gen.ColumnContext{
			{Name: "company_id", FieldName: "CompanyID", GoType: "uuid.NullUUID", DBTag: "company_id", JSONTag: "company_id", Nullable: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer, ZeroValue: "uuid.Nil"},
			{Name: "name", FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
			{Name: "price", FieldName: "Price", GoType: "float64", DBTag: "price", JSONTag: "price"},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer, ZeroValue: "uuid.Nil"},
		},
		HasO2ORelationships: true,
		O2ORelationships: []gen.RelationshipContext{
			{
				Name:             "company",
				Type:             parser.OneToOne,
				TargetTable:      "companies",
				TargetStructName: "Company",
				FKColumn:         "company_id",
				FKFieldName:      "CompanyID",
				FieldName:        "Company",
				GoType:           "*Company",
				JSONTag:          "company",
			},
		},
		O2OJoinDetails: []gen.O2OJoinDetail{
			{
				FieldOptionsCheck:   "fo.Company != nil",
				FieldOptionsColumns: "fo.Company.Columns()",
				// PKColumn is what the join's column list unions in so NULL
				// detection always has a scanned PK to test.
				// Production codegen sets it in buildSingleO2ODetail.
				PKColumn:      "id",
				TargetTable:   "companies",
				TargetSchema:  "public",
				Alias:         "c",
				OnLocalAlias:  "p",
				OnLocal:       "company_id",
				OnRemote:      "id",
				StructName:    "Company",
				VarName:       "c",
				HasDataVar:    "cHasData",
				PKFieldName:   "ID",
				PKZeroValue:   "uuid.Nil",
				ParentVarName: "p",
				FieldName:     "Company",
				ScanCases: []gen.O2OScanCase{
					{PrefixedColumn: "c.country", ScanExpr: "&c.Country", Shape: "direct", FieldName: "Country", VarName: "c"},
					{PrefixedColumn: "c.id", ScanExpr: "&c.ID", IsPK: true, Shape: "direct", FieldName: "ID", VarName: "c"},
					{PrefixedColumn: "c.name", ScanExpr: "&c.Name", Shape: "direct", FieldName: "Name", VarName: "c"},
				},
			},
		},
		O2OAllTargets: []gen.O2OJoinDetail{
			{
				StructName:    "Company",
				VarName:       "c",
				HasDataVar:    "cHasData",
				PKFieldName:   "ID",
				PKZeroValue:   "uuid.Nil",
				ParentVarName: "p",
				FieldName:     "Company",
				ScanCases: []gen.O2OScanCase{
					{PrefixedColumn: "c.country", ScanExpr: "&c.Country", Shape: "direct", FieldName: "Country", VarName: "c"},
					{PrefixedColumn: "c.id", ScanExpr: "&c.ID", IsPK: true, Shape: "direct", FieldName: "ID", VarName: "c"},
					{PrefixedColumn: "c.name", ScanExpr: "&c.Name", Shape: "direct", FieldName: "Name", VarName: "c"},
				},
			},
		},
		O2OAssignOrder: []gen.O2OJoinDetail{
			{
				StructName:    "Company",
				VarName:       "c",
				HasDataVar:    "cHasData",
				PKFieldName:   "ID",
				PKZeroValue:   "uuid.Nil",
				ParentVarName: "p",
				FieldName:     "Company",
			},
		},
		O2OParentScanCases: []gen.O2OScanCase{
			{PrefixedColumn: "p.company_id", ScanExpr: "&p.CompanyID", Shape: "direct", FieldName: "CompanyID", VarName: "p"},
			{PrefixedColumn: "p.id", ScanExpr: "&p.ID", Shape: "direct", FieldName: "ID", VarName: "p"},
			{PrefixedColumn: "p.name", ScanExpr: "&p.Name", Shape: "direct", FieldName: "Name", VarName: "p"},
			{PrefixedColumn: "p.price", ScanExpr: "&p.Price", Shape: "direct", FieldName: "Price", VarName: "p"},
		},
		Dialect:    "postgres",
		Driver:     "pgx",
		QueryLimit: 100,
	}
}

func testRelationshipsContext_withSoftDelete() gen.TableContext {
	ctx := testRelationshipsContext_basicO2O()
	ctx.O2OJoinDetails[0].HasSoftDelete = true
	ctx.O2OJoinDetails[0].SoftDeleteColumn = "deleted_at"
	ctx.O2OJoinDetails[0].SoftDeleteTypeConst = "sql.SoftDeleteTimestamp"
	return ctx
}

func testRelationshipsContext_withPolymorphicFilter() gen.TableContext {
	ctx := testRelationshipsContext_basicO2O()
	// TableContext holds the fully-qualified filter; the codegen
	// pre-qualification step that runs in buildSingleO2ODetail is bypassed in
	// this unit test, so we set the already-qualified form directly.
	ctx.O2OJoinDetails[0].Filter = "c.commentable_type = 'products'"
	return ctx
}

func testRelationshipsContext_chained() gen.TableContext {
	ctx := testRelationshipsContext_basicO2O()

	// Add chained: Company → Country
	ctx.O2OJoinDetails[0].ChainedJoins = []gen.O2OJoinDetail{
		{
			FieldOptionsCheck:   "fo.Company.Country != nil",
			FieldOptionsColumns: "fo.Company.Country.Columns()",
			PKColumn:            "id",
			TargetTable:         "countries",
			TargetSchema:        "public",
			Alias:               "c2",
			OnLocalAlias:        "c",
			OnLocal:             "country_id",
			OnRemote:            "id",
			StructName:          "Country",
			VarName:             "c2",
			HasDataVar:          "c2HasData",
			PKFieldName:         "ID",
			PKZeroValue:         "uuid.Nil",
			ParentVarName:       "c",
			FieldName:           "Country",
			ScanCases: []gen.O2OScanCase{
				{PrefixedColumn: "c2.id", ScanExpr: "&c2.ID", IsPK: true, Shape: "direct", FieldName: "ID", VarName: "c2"},
				{PrefixedColumn: "c2.name", ScanExpr: "&c2.Name", Shape: "direct", FieldName: "Name", VarName: "c2"},
			},
		},
	}

	// Add Country to O2OAllTargets
	ctx.O2OAllTargets = append(ctx.O2OAllTargets, gen.O2OJoinDetail{
		StructName:    "Country",
		VarName:       "c2",
		HasDataVar:    "c2HasData",
		PKFieldName:   "ID",
		PKZeroValue:   "uuid.Nil",
		ParentVarName: "c",
		FieldName:     "Country",
		ScanCases: []gen.O2OScanCase{
			{PrefixedColumn: "c2.id", ScanExpr: "&c2.ID", IsPK: true, Shape: "direct", FieldName: "ID", VarName: "c2"},
			{PrefixedColumn: "c2.name", ScanExpr: "&c2.Name", Shape: "direct", FieldName: "Name", VarName: "c2"},
		},
	})

	// Reverse assignment order: Country first, then Company
	ctx.O2OAssignOrder = []gen.O2OJoinDetail{
		{
			StructName:    "Country",
			VarName:       "c2",
			HasDataVar:    "c2HasData",
			PKFieldName:   "ID",
			PKZeroValue:   "uuid.Nil",
			ParentVarName: "c",
			FieldName:     "Country",
		},
		{
			StructName:    "Company",
			VarName:       "c",
			HasDataVar:    "cHasData",
			PKFieldName:   "ID",
			PKZeroValue:   "uuid.Nil",
			ParentVarName: "p",
			FieldName:     "Company",
		},
	}
	return ctx
}

func testRelationshipsContext_selfReferential() gen.TableContext {
	return gen.TableContext{
		StructName:        "Employee",
		TableName:         "employees",
		TableNameConstant: "TableEmployees",
		Schema:            "public",
		Package:           "db",
		VarName:           "e",
		Imports: []string{
			"context",
			"fmt",
			"github.com/gofrs/uuid/v5",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		Columns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer, ZeroValue: "uuid.Nil"},
			{Name: "manager_id", FieldName: "ManagerID", GoType: "*uuid.UUID", DBTag: "manager_id", JSONTag: "manager_id", Nullable: true, Import: "github.com/gofrs/uuid/v5"},
			{Name: "name", FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer, ZeroValue: "uuid.Nil"},
		},
		HasO2ORelationships: true,
		O2ORelationships: []gen.RelationshipContext{
			{
				Name:             "manager",
				Type:             parser.OneToOne,
				TargetTable:      "employees",
				TargetStructName: "Employee",
				FKColumn:         "manager_id",
				FKFieldName:      "ManagerID",
				FieldName:        "Manager",
				GoType:           "*Employee",
				JSONTag:          "manager",
			},
		},
		O2OJoinDetails: []gen.O2OJoinDetail{
			{
				FieldOptionsCheck:   "fo.Manager != nil",
				FieldOptionsColumns: "fo.Manager.Columns()",
				PKColumn:            "id",
				TargetTable:         "employees",
				TargetSchema:        "public",
				Alias:               "m",
				OnLocalAlias:        "e",
				OnLocal:             "manager_id",
				OnRemote:            "id",
				StructName:          "Employee",
				VarName:             "m",
				HasDataVar:          "mHasData",
				PKFieldName:         "ID",
				PKZeroValue:         "uuid.Nil",
				ParentVarName:       "e",
				FieldName:           "Manager",
				ScanCases: []gen.O2OScanCase{
					{PrefixedColumn: "m.id", ScanExpr: "&m.ID", IsPK: true, Shape: "direct", FieldName: "ID", VarName: "m"},
					{PrefixedColumn: "m.manager_id", ScanExpr: "&m.ManagerID", Shape: "direct", FieldName: "ManagerID", VarName: "m"},
					{PrefixedColumn: "m.name", ScanExpr: "&m.Name", Shape: "direct", FieldName: "Name", VarName: "m"},
				},
			},
		},
		O2OAllTargets: []gen.O2OJoinDetail{
			{
				StructName:    "Employee",
				VarName:       "m",
				HasDataVar:    "mHasData",
				PKFieldName:   "ID",
				PKZeroValue:   "uuid.Nil",
				ParentVarName: "e",
				FieldName:     "Manager",
				ScanCases: []gen.O2OScanCase{
					{PrefixedColumn: "m.id", ScanExpr: "&m.ID", IsPK: true, Shape: "direct", FieldName: "ID", VarName: "m"},
					{PrefixedColumn: "m.manager_id", ScanExpr: "&m.ManagerID", Shape: "direct", FieldName: "ManagerID", VarName: "m"},
					{PrefixedColumn: "m.name", ScanExpr: "&m.Name", Shape: "direct", FieldName: "Name", VarName: "m"},
				},
			},
		},
		O2OAssignOrder: []gen.O2OJoinDetail{
			{
				StructName:    "Employee",
				VarName:       "m",
				HasDataVar:    "mHasData",
				PKFieldName:   "ID",
				PKZeroValue:   "uuid.Nil",
				ParentVarName: "e",
				FieldName:     "Manager",
			},
		},
		O2OParentScanCases: []gen.O2OScanCase{
			{PrefixedColumn: "e.id", ScanExpr: "&e.ID", Shape: "direct", FieldName: "ID", VarName: "e"},
			{PrefixedColumn: "e.manager_id", ScanExpr: "&e.ManagerID", Shape: "direct", FieldName: "ManagerID", VarName: "e"},
			{PrefixedColumn: "e.name", ScanExpr: "&e.Name", Shape: "direct", FieldName: "Name", VarName: "e"},
		},
		Dialect:    "postgres",
		Driver:     "pgx",
		QueryLimit: 100,
	}
}

// --- Test: No output when no O2O relationships ---

func TestRelationshipsTemplate_noO2O(t *testing.T) {
	ctx := gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Package:           "db",
		VarName:           "p",
	}
	output := executeRelationshipsTemplate(t, ctx)
	if strings.TrimSpace(output) != "" {
		t.Errorf("expected empty output for no O2O relationships, got:\n%s", output)
	}
}

// --- Test: O2O JOIN scan function with prefix routing ---

func TestRelationshipsTemplate_o2oScanFunction(t *testing.T) {
	ctx := testRelationshipsContext_basicO2O()
	output := executeRelationshipsTemplate(t, ctx)

	wantPatterns := []string{
		"func scanProductsWithOneToOneJoins(rows database.Rows, columns []string) ([]*Product, error)",
		"p := &Product{}",
		"c := &Company{}",
		"cHasData := false",
		`case "p.id":`,
		"targets[idx] = &p.ID",
		`case "p.name":`,
		"targets[idx] = &p.Name",
		`case "c.id":`,
		"targets[idx] = &c.ID",
		"cHasData = true",
		`case "c.name":`,
		"targets[idx] = &c.Name",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: O2O NULL detection for LEFT JOIN miss ---

func TestRelationshipsTemplate_o2oNullDetection(t *testing.T) {
	ctx := testRelationshipsContext_basicO2O()
	output := executeRelationshipsTemplate(t, ctx)

	wantPatterns := []string{
		"if cHasData && c.ID != uuid.Nil {",
		"p.Company = c",
		"p.Company = nil",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing NULL detection: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: O2O chained relationships (multi-JOIN) ---

func TestRelationshipsTemplate_chainedO2O(t *testing.T) {
	ctx := testRelationshipsContext_chained()
	output := executeRelationshipsTemplate(t, ctx)

	wantPatterns := []string{
		// resolveO2OJoins has nested checks
		"if fo.Company != nil {",
		`Schema: "public", Name: "companies"`,
		"if fo.Company.Country != nil {",
		`Schema: "public", Name: "countries"`,
		`Alias:   "c2"`,
		// Scan function has both targets
		"c2 := &Country{}",
		"c2HasData := false",
		`case "c2.id":`,
		"c2HasData = true",
		// Assignment order: Country first, then Company
		"c2HasData && c2.ID != uuid.Nil",
		"c.Country = c2",
		"cHasData && c.ID != uuid.Nil",
		"p.Company = c",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing chained pattern: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Verify Country assignment comes before Company assignment
	countryAssignIdx := strings.Index(output, "c.Country = c2")
	companyAssignIdx := strings.Index(output, "p.Company = c")
	if countryAssignIdx < 0 || companyAssignIdx < 0 {
		t.Fatal("missing assignment expressions")
	}
	if countryAssignIdx > companyAssignIdx {
		t.Error("Country assignment must come before Company assignment (deepest chain first)")
	}
}

// --- Test: O2O with soft delete on related table ---

func TestRelationshipsTemplate_softDeleteOnRelated(t *testing.T) {
	ctx := testRelationshipsContext_withSoftDelete()
	output := executeRelationshipsTemplate(t, ctx)

	wantPatterns := []string{
		"SoftDelete: &sql.SoftDeleteOptions{",
		`Column: "deleted_at"`,
		"Type:   sql.SoftDeleteTimestamp",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing soft delete: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: O2O with polymorphic filter ---

func TestRelationshipsTemplate_polymorphicFilter(t *testing.T) {
	ctx := testRelationshipsContext_withPolymorphicFilter()
	output := executeRelationshipsTemplate(t, ctx)

	wantPatterns := []string{
		` AND `,
		`c.commentable_type = 'products'`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing polymorphic filter: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Self-referential relationship ---

func TestRelationshipsTemplate_selfReferential(t *testing.T) {
	ctx := testRelationshipsContext_selfReferential()
	output := executeRelationshipsTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *employeeClient) resolveO2OJoins(d sql.Dialect, fo *EmployeeFieldOptions) []sql.JoinClause",
		"func scanEmployeesWithOneToOneJoins(rows database.Rows, columns []string) ([]*Employee, error)",
		"if fo.Manager != nil {",
		`Alias:   "m"`,
		"m := &Employee{}",
		"mHasData := false",
		`case "m.id":`,
		"mHasData = true",
		`case "e.id":`,
		"e.Manager = m",
		"e.Manager = nil",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing self-ref pattern: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Self-referential: target struct is same as parent but different alias
	if !strings.Contains(output, `"m.manager_id"`) {
		t.Error("self-referential should include manager's manager_id column")
	}
}

// --- Test: resolveO2OJoins function generation ---

func TestRelationshipsTemplate_resolveO2OJoins(t *testing.T) {
	ctx := testRelationshipsContext_basicO2O()
	output := executeRelationshipsTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *productClient) resolveO2OJoins(d sql.Dialect, fo *ProductFieldOptions) []sql.JoinClause",
		"if fo == nil {",
		"return nil",
		"var joins []sql.JoinClause",
		"if fo.Company != nil {",
		"joins = append(joins, sql.JoinClause{",
		`Table:   sql.Table{Schema: "public", Name: "companies"}`,
		`Alias:   "c"`,
		`d.QuoteIdentifier("id")`,
		`d.QuoteIdentifier("company_id")`,
		"fo.Company.Columns()",
		"return joins",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing resolveO2OJoins pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Parallel loading via errgroup (relationships.tmpl doesn't handle this, but verify it doesn't interfere) ---

func TestRelationshipsTemplate_parallelLoadingNotGenerated(t *testing.T) {
	ctx := testRelationshipsContext_basicO2O()
	output := executeRelationshipsTemplate(t, ctx)

	// relationships.tmpl should NOT generate errgroup or loadRelationships — those are in get.go.tmpl
	if strings.Contains(output, "errgroup") {
		t.Error("relationships.tmpl should not reference errgroup")
	}
	if strings.Contains(output, "loadRelationships") {
		t.Error("relationships.tmpl should not generate loadRelationships")
	}
}

// --- Test: Golden file comparison ---

func TestRelationshipsTemplate_goldenFile_basicO2O(t *testing.T) {
	ctx := testRelationshipsContext_basicO2O()
	output := executeRelationshipsTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_relationships_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "relationships_products_gen.go")

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o750); err != nil { //nolint:gosec // test helper
			t.Fatalf("creating golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, formatted, 0o600); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		t.Log("golden file updated")
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // golden file path is not user-controlled
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create): %v", err)
	}

	if diff := cmp.Diff(string(want), got); diff != "" {
		t.Errorf("relationships_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}

// --- Test: Template compiles cleanly ---

func TestRelationshipsTemplate_compilesCleanly(t *testing.T) {
	ctx := testRelationshipsContext_basicO2O()
	output := executeRelationshipsTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_relationships_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestRelationshipsTemplate_compilesCleanly_chained(t *testing.T) {
	ctx := testRelationshipsContext_chained()
	output := executeRelationshipsTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_relationships_chained_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestRelationshipsTemplate_compilesCleanly_selfRef(t *testing.T) {
	ctx := testRelationshipsContext_selfReferential()
	output := executeRelationshipsTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "employees_relationships_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// TestRelationships_O2OJoinFilterParenthesised pins the O2O half of the
// parenthesised-filter rule across all three dialects.
//
// The JOIN ON predicate is assembled as a string, not as sql.Conditions, so the
// runtime's Raw parenthesisation never reaches it — the template has to wrap it
// itself. Unwrapped, a `filter:` whose top-level operator is OR binds looser
// than the AND joining it to the correlation, so `ON tgt.fk = parent.id AND
// kind = 'x' OR name = 'y'` matches every row satisfying the OR arm regardless
// of parent, fanning the LEFT JOIN out across parents.
//
// Per dialect because each qualifier spells a rewritten identifier its own way
// and, more to the point, because pg_query and vitess *discard* the author's
// own parentheses during qualification — so wrapping at this seam is the only
// place the predicate can be made safe.
func TestRelationships_O2OJoinFilterParenthesised(t *testing.T) {
	for _, dialect := range []config.Dialect{config.DialectPostgres, config.DialectMySQL, config.DialectSQLite} {
		t.Run(string(dialect), func(t *testing.T) {
			tc := discriminatorTables(t, dialect, config.TableRelationship{
				Name: "PrimaryDocument", Type: "one_to_one", Table: "documents", FK: "entity_id",
				Filter: "entity_type = 'asset.primary' OR name = 'leak'",
			})
			got := executeRelationshipsTemplate(t, tc)

			predicate := o2oJoinFilter(t, tc, "PrimaryDocument")
			want := `+ " AND (" + ` + strconv.Quote(predicate) + ` + ")"`
			if !strings.Contains(got, want) {
				t.Errorf("O2O JOIN ON predicate on %s is not parenthesised.\nwant substring: %s\ngot:\n%s", dialect, want, got)
			}
			if strings.Contains(got, `+ " AND " + `+strconv.Quote(predicate)) {
				t.Errorf("O2O JOIN ON predicate on %s still spliced bare:\n%s", dialect, got)
			}
		})
	}
}

// scanShapeSchema is one O2O edge into a target carrying each scan-destination
// shape the JOIN-aware scan has to handle: NOT NULL scalars, a nullable column,
// and an array.
func scanShapeSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "owners",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
			{
				Name: "details",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "owner_id", Type: "uuid"},
					{Name: "label", Type: "text"},
					{Name: "note", Type: "text", Nullable: true},
					{Name: "tags", Type: "text[]"},
				},
			},
		},
	}
}

// TestRelationships_O2OTargetScanAbsorbsJoinMiss pins the LEFT JOIN miss
// scan at the seam that decides it — buildO2OScanCases — from a real table
// context rather than the hand-assembled ScanCases the fixtures above
// carry, which supply ScanExpr themselves and so never reach that code.
//
// A LEFT JOIN miss returns NULL for every one of the target's columns, so a
// NOT NULL column's destination has to absorb it or the scan fails before the
// zero-PK miss detection can run. Two things must both hold: the non-nullable
// scalars are wrapped, and the destinations that already take NULL are not.
// The second half is not symmetry for its own sake — wrapping a slice makes the
// destination a sql.Scanner, which drops pgx off its native array codec and
// then fails on an ordinary non-NULL array ("unsupported Scan, storing
// driver.Value type []uint8 into type *[]string").
func TestRelationships_O2OTargetScanAbsorbsJoinMiss(t *testing.T) {
	in := testInput(scanShapeSchema())
	parser.DetectRelationships(in.Schema)
	in.Config.Tables["owners"] = config.TableConfig{
		Relationships: []config.TableRelationship{
			{Name: "Detail", Type: "one_to_one", Table: "details", FK: "owner_id"},
		},
	}

	tables, err := gen.BuildTableContextsFromSchema(in.Schema, in.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}

	var target gen.O2OJoinDetail
	for _, tc := range tables {
		if tc.TableName != "owners" {
			continue
		}
		for _, d := range tc.O2OAllTargets {
			if d.TargetTable == "details" {
				target = d
			}
		}
	}
	if target.TargetTable == "" {
		t.Fatal("no O2O join target for the details table")
	}

	v := target.VarName
	want := map[string]string{
		// NOT NULL scalars: a JOIN miss hands these a NULL their Go types refuse.
		"id":       "database.NullScan(&" + v + ".ID)",
		"owner_id": "database.NullScan(&" + v + ".OwnerID)",
		"label":    "database.NullScan(&" + v + ".Label)",
		// Nullable: the Go type already takes NULL.
		"note": "&" + v + ".Note",
		// Array: reads a NULL as a nil slice on both drivers unaided.
		"tags": "&" + v + ".Tags",
	}

	got := make(map[string]string, len(target.ScanCases))
	for _, sc := range target.ScanCases {
		got[strings.TrimPrefix(sc.PrefixedColumn, target.Alias+".")] = sc.ScanExpr
	}
	for _, col := range []string{"id", "owner_id", "label", "note", "tags"} {
		if got[col] != want[col] {
			t.Errorf("scan destination for %q = %q, want %q", col, got[col], want[col])
		}
	}
}
