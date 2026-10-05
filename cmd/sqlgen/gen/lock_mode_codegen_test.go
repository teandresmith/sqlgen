package gen_test

import (
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/sql"
)

// loadTableTemplates parses the shared/* fragments and a single per-table
// template (e.g. "table/get") so the lock-mode emission can be exercised
// without spinning up the full orchestrator.
func loadTableTemplates(t *testing.T, tablePath string) *template.Template {
	t.Helper()
	tmpl, err := template.New(filepath.Base(tablePath)).
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", tablePath))
	if err != nil {
		t.Fatalf("parsing table template %s: %v", tablePath, err)
	}
	return tmpl
}

// renderTableTemplate wraps the given table template body with the package +
// import preamble so the output matches what consumers see in generated files.
func renderTableTemplate(t *testing.T, tmpl *template.Template, name string, ctx gen.TableContext) string {
	t.Helper()
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, name, ctx); err != nil {
		t.Fatalf("executing template %s: %v", name, err)
	}
	return string(gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String())))
}

// buildLockGuardTableContext returns a minimal table context covering the
// surface needed to exercise the LockMode guard emission for each dialect.
// The context is intentionally small — most relationship/joined-load fields
// are zero so the focus stays on the guard fragment.
func buildLockGuardTableContext(dialect string) gen.TableContext {
	return gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Package:           "db",
		VarName:           "p",
		Imports: []string{
			"context",
			"fmt",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		Columns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "int64", DBTag: "id", JSONTag: "id", PrimaryKey: true},
			{Name: "name", FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "int64", DBTag: "id", JSONTag: "id", PrimaryKey: true},
		},
		AllColumnNames: []string{"id", "name"},
		ScanShapes: []gen.ScanShapeContext{
			{ColumnName: "id", FieldName: "ID", Shape: "direct", ScanExpr: "&p.ID"},
			{ColumnName: "name", FieldName: "Name", Shape: "direct", ScanExpr: "&p.Name"},
		},
		Operations: gen.ResolvedOperations{
			Get: true, GetMany: true, Count: true,
			Create: true, Update: true, Upsert: true,
			Connection: true,
		},
		Dialect:    config.Dialect(dialect),
		Driver:     "pgx",
		QueryLimit: 100,
		PageSize:   20,
		CursorKeys: []string{"id"},
	}
}

// TestLockModeGuard_GetMethod_postgres pins the guard shape emitted into the
// Postgres `Get` method body: the precondition + SkipCache force fire, no
// MySQL version branch, no SQLite rejection branch.
func TestLockModeGuard_GetMethod_postgres(t *testing.T) {
	tmpl := loadTableTemplates(t, "get.go.tmpl")
	ctx := buildLockGuardTableContext("postgres")
	out := renderTableTemplate(t, tmpl, "table/get", ctx)

	mustContainAll(
		t, out,
		"if options.LockMode != sql.LockNone {",
		"if !database.InTransaction(ctx) {",
		`return nil, fmt.Errorf("get product: LockMode requires an active transaction")`,
		"options.SkipCache = true",
	)
	mustNotContain(
		t, out,
		"c.mysqlVersion.get(ctx)",
		"requires MySQL 8.0+",
		"is unsupported on sqlite dialect",
	)
}

// TestLockModeGuard_GetMethod_mysql confirms the Postgres-style guard body PLUS
// the version-aware branch that rejects NoWait/SkipLocked on server < 8.0.
func TestLockModeGuard_GetMethod_mysql(t *testing.T) {
	tmpl := loadTableTemplates(t, "get.go.tmpl")
	ctx := buildLockGuardTableContext("mysql")
	out := renderTableTemplate(t, tmpl, "table/get", ctx)

	mustContainAll(
		t, out,
		"if options.LockMode != sql.LockNone {",
		"if !database.InTransaction(ctx) {",
		`return nil, fmt.Errorf("get product: LockMode requires an active transaction")`,
		"options.SkipCache = true",
		"if options.LockMode == sql.LockForUpdateNoWait || options.LockMode == sql.LockForUpdateSkipLocked {",
		"version, err := c.mysqlVersion.get(ctx)",
		"if version.Major < 8 {",
		`requires MySQL 8.0+ (server reports %s)`,
	)
	mustNotContain(
		t, out,
		"is unsupported on sqlite dialect",
	)
}

// TestLockModeGuard_GetMethod_sqlite verifies SQLite returns the dialect
// rejection error before any SQL round-trip — no transaction check, no
// SkipCache mutation, no MySQL version branch.
func TestLockModeGuard_GetMethod_sqlite(t *testing.T) {
	tmpl := loadTableTemplates(t, "get.go.tmpl")
	ctx := buildLockGuardTableContext("sqlite")
	out := renderTableTemplate(t, tmpl, "table/get", ctx)

	mustContainAll(
		t, out,
		"if options.LockMode != sql.LockNone {",
		`return nil, fmt.Errorf("get product: LockMode is unsupported on sqlite dialect")`,
	)
	mustNotContain(
		t, out,
		"database.InTransaction(ctx)",
		"options.SkipCache = true",
		"c.mysqlVersion.get(ctx)",
	)
}

// TestLockModeGuard_GetManyMethod_allDialects confirms GetMany carries the
// per-dialect guard at method entry. Same shape as Get; one merged test
// keeps coverage compact.
func TestLockModeGuard_GetManyMethod_allDialects(t *testing.T) {
	cases := []struct {
		dialect      string
		mustContain  []string
		mustNotHave  []string
		errorMessage string
	}{
		{
			dialect: "postgres",
			mustContain: []string{
				"if options.LockMode != sql.LockNone {",
				`return nil, fmt.Errorf("get products: LockMode requires an active transaction")`,
				"options.SkipCache = true",
			},
			mustNotHave: []string{"c.mysqlVersion.get(ctx)", "is unsupported on sqlite dialect"},
		},
		{
			dialect: "mysql",
			mustContain: []string{
				"if options.LockMode != sql.LockNone {",
				`return nil, fmt.Errorf("get products: LockMode requires an active transaction")`,
				"options.SkipCache = true",
				"version, err := c.mysqlVersion.get(ctx)",
				"if version.Major < 8 {",
			},
			mustNotHave: []string{"is unsupported on sqlite dialect"},
		},
		{
			dialect: "sqlite",
			mustContain: []string{
				`return nil, fmt.Errorf("get products: LockMode is unsupported on sqlite dialect")`,
			},
			mustNotHave: []string{
				"database.InTransaction(ctx)",
				"c.mysqlVersion.get(ctx)",
			},
		},
	}

	tmpl := loadTableTemplates(t, "get.go.tmpl")
	for _, tc := range cases {
		t.Run(tc.dialect, func(t *testing.T) {
			ctx := buildLockGuardTableContext(tc.dialect)
			out := renderTableTemplate(t, tmpl, "table/get", ctx)
			mustContainAll(t, out, tc.mustContain...)
			mustNotContain(t, out, tc.mustNotHave...)
		})
	}
}

// TestLockModeGuard_ConnectionMethod_allDialects pins the same guard shape
// onto Connection. Connection delegates to Count + GetMany, but its own
// method body must short-circuit for unsupported dialects before any SQL.
func TestLockModeGuard_ConnectionMethod_allDialects(t *testing.T) {
	tmpl := loadTableTemplates(t, "pagination.go.tmpl")
	cases := []struct {
		dialect     string
		mustContain []string
		mustNotHave []string
	}{
		{
			dialect: "postgres",
			mustContain: []string{
				"if options.LockMode != sql.LockNone {",
				`return nil, fmt.Errorf("connection products: LockMode requires an active transaction")`,
				"options.SkipCache = true",
			},
			mustNotHave: []string{"c.mysqlVersion.get(ctx)"},
		},
		{
			dialect: "mysql",
			mustContain: []string{
				`return nil, fmt.Errorf("connection products: LockMode requires an active transaction")`,
				"version, err := c.mysqlVersion.get(ctx)",
			},
			mustNotHave: []string{"is unsupported on sqlite dialect"},
		},
		{
			dialect: "sqlite",
			mustContain: []string{
				`return nil, fmt.Errorf("connection products: LockMode is unsupported on sqlite dialect")`,
			},
			mustNotHave: []string{"database.InTransaction(ctx)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.dialect, func(t *testing.T) {
			ctx := buildLockGuardTableContext(tc.dialect)
			out := renderTableTemplate(t, tmpl, "table/pagination", ctx)
			mustContainAll(t, out, tc.mustContain...)
			mustNotContain(t, out, tc.mustNotHave...)
		})
	}
}

// TestLockModeThreading_GetMany pins that GetMany passes options.LockMode into
// sql.SelectOptions for the BuildSelect path. The JOIN-builder path is
// covered indirectly via the example regression tests; here we keep the
// generator unit slim.
func TestLockModeThreading_GetMany(t *testing.T) {
	tmpl := loadTableTemplates(t, "get.go.tmpl")
	ctx := buildLockGuardTableContext("postgres")
	out := renderTableTemplate(t, tmpl, "table/get", ctx)

	if !strings.Contains(out, "LockMode:   options.LockMode,") {
		t.Errorf("GetMany must thread options.LockMode into sql.SelectOptions; got:\n%s", out)
	}
}

// TestChainedGet_LockNoneForced asserts that every chained Get / GetMany site
// in write-method templates passes LockMode: LockNone alongside the existing
// SkipHooks: true. Read-method templates (get, pagination) must NOT carry the
// LockNone literal in their internalOpts blocks — they propagate caller-set
// LockMode to the inner read.
func TestChainedGet_LockNoneForced(t *testing.T) {
	writeTemplates := []struct {
		path string
		name string
	}{
		{"create.go.tmpl", "table/create"},
		{"update.go.tmpl", "table/update"},
		{"upsert.go.tmpl", "table/upsert"},
		{"delete.go.tmpl", "table/delete"},
	}
	for _, w := range writeTemplates {
		t.Run(w.path, func(t *testing.T) {
			tmpl := loadTableTemplates(t, w.path)
			ctx := buildLockGuardTableContext("postgres")
			// Restore + SoftDelete need a soft-delete column to emit. Add it
			// when exercising delete.go.tmpl so all chained-Get branches fire.
			if strings.HasPrefix(w.path, "delete") {
				ctx.SoftDelete = &gen.SoftDeleteContext{Column: "deleted_at", FieldName: "DeletedAt", Strategy: "timestamp"}
				ctx.Operations.SoftDelete = true
				ctx.Operations.Restore = true
				ctx.Operations.HardDelete = true
				ctx.Columns = append(ctx.Columns, gen.ColumnContext{
					Name: "deleted_at", FieldName: "DeletedAt", GoType: "*time.Time",
					DBTag: "deleted_at", JSONTag: "deleted_at", Nullable: true,
				})
				ctx.AllColumnNames = append(ctx.AllColumnNames, "deleted_at")
			}
			out := renderTableTemplate(t, tmpl, w.name, ctx)
			if !strings.Contains(out, "o.LockMode = sql.LockNone") {
				t.Errorf("%s must force LockMode = sql.LockNone in chained Get sites; got:\n%s", w.path, out)
			}
			// Pin the ordering: SkipHooks then LockNone, on consecutive lines.
			// A drift that splits these is the bug we want to catch early.
			lines := strings.Split(out, "\n")
			pairs := 0
			for i, l := range lines {
				if !strings.Contains(l, "o.SkipHooks = true") {
					continue
				}
				if i+1 >= len(lines) || !strings.Contains(lines[i+1], "o.LockMode = sql.LockNone") {
					t.Errorf("%s: SkipHooks at line %d not immediately followed by LockMode = sql.LockNone:\n  %s\n  %s",
						w.path, i+1, l, lines[i+1])
				}
				pairs++
			}
			if pairs == 0 {
				t.Errorf("%s: no SkipHooks/LockNone pairs found", w.path)
			}
		})
	}
}

// TestChainedGet_PaginationDoesNotForceLockNone verifies pagination internal
// calls (Paginate → Count + GetMany; Connection → Count + GetMany) propagate
// the caller-set LockMode rather than zeroing it. Read-method internalOpts
// blocks intentionally lack the o.LockMode = sql.LockNone line.
func TestChainedGet_PaginationDoesNotForceLockNone(t *testing.T) {
	tmpl := loadTableTemplates(t, "pagination.go.tmpl")
	ctx := buildLockGuardTableContext("postgres")
	out := renderTableTemplate(t, tmpl, "table/pagination", ctx)

	if strings.Contains(out, "o.LockMode = sql.LockNone") {
		t.Errorf("pagination.go.tmpl must NOT zero LockMode in its internalOpts (read-method path); got:\n%s", out)
	}
}

// TestMySQLVersionCache_emittedOnlyForMySQL confirms the unified-client
// template emits the mysqlVersionCache type + probe only when the project
// dialect is MySQL. Postgres and SQLite must regenerate identically without
// any version-cache plumbing.
func TestMySQLVersionCache_emittedOnlyForMySQL(t *testing.T) {
	cases := []struct {
		dialect     string
		mustContain []string
		mustNotHave []string
		caseLabel   string
	}{
		{
			dialect:   "mysql",
			caseLabel: "mysql_emits_cache",
			mustContain: []string{
				"type mysqlVersionCache struct",
				"sync.Once",
				`"SELECT VERSION()"`,
				"newMySQLVersionCache",
			},
		},
		{
			dialect:     "postgres",
			caseLabel:   "postgres_no_cache",
			mustNotHave: []string{"mysqlVersionCache", "SELECT VERSION()", "newMySQLVersionCache"},
		},
		{
			dialect:     "sqlite",
			caseLabel:   "sqlite_no_cache",
			mustNotHave: []string{"mysqlVersionCache", "SELECT VERSION()", "newMySQLVersionCache"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.caseLabel, func(t *testing.T) {
			tmpl, err := template.New("client.go.tmpl").
				Funcs(gen.FuncMap(sql.NewPostgresDialect())).
				ParseFiles(filepath.Join("templates", "client.go.tmpl"))
			if err != nil {
				t.Fatalf("parsing client template: %v", err)
			}
			ctx := gen.ClientContext{
				ClientName: "Client",
				Package:    "db",
				Dialect:    tc.dialect,
				Entities: []gen.EntityClientContext{
					{StructName: "Product", InterfaceName: "ProductClient", FieldName: "product", AccessorName: "Products"},
				},
			}
			var buf strings.Builder
			if err := tmpl.ExecuteTemplate(&buf, "client", ctx); err != nil {
				t.Fatalf("executing client template: %v", err)
			}
			out := buf.String()
			mustContainAll(t, out, tc.mustContain...)
			mustNotContain(t, out, tc.mustNotHave...)
		})
	}
}

// mustContainAll fails the test if any expected substring is absent.
func mustContainAll(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("output missing %q\n--- output ---\n%s", w, got)
		}
	}
}

// mustNotContain fails the test if any forbidden substring is present.
func mustNotContain(t *testing.T, got string, forbidden ...string) {
	t.Helper()
	for _, f := range forbidden {
		if strings.Contains(got, f) {
			t.Errorf("output unexpectedly contains %q\n--- output ---\n%s", f, got)
		}
	}
}

// silence unused-import warnings in the test helper when buildLockGuardTableContext
// is the only consumer of gotype.
var _ = gotype.FKStringStringer
