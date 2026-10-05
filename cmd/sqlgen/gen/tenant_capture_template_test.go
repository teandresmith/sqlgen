package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/sql"
)

// Structural tenant capture (PRD §29.5). For tenanted tables whose tenant
// column is NOT part of the primary key, every mutation shape must capture each
// affected row's tenant into the parallel m.AffectedTenants carrier:
// PostgreSQL/SQLite widen the statement's RETURNING projection; MySQL pre-reads
// via captureAffectedTenants / the widened collectAffected* helpers.
// Tenant-in-PK and non-tenanted tables must emit no carrier at all
// (additive-only).

// renderTenantCapture renders one table template body under an explicit
// dialect so the capture emission can be asserted per dialect.
func renderTenantCapture(t *testing.T, tc gen.TableContext, templateName string, dialect sql.Dialect) string {
	t.Helper()
	tmpl := loadAllTableTemplates(t, dialect)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, templateName, tc); err != nil {
		t.Fatalf("executing %s: %v", templateName, err)
	}
	return buf.String()
}

// captureProductContext returns the tenanted products context widened with
// the batch/where mutation shapes the capture tests exercise, adjusted for
// the given dialect/driver pair.
func captureProductContext(dialectName config.Dialect, driver config.Driver) gen.TableContext {
	tc := tenantedProductContext()
	tc.Dialect = dialectName
	tc.Driver = driver
	tc.Operations.UpdateMany = true
	tc.Operations.UpdateWhere = true
	return tc
}

// Update / UpdateMany / UpdateWhere on PostgreSQL and SQLite capture the
// row's tenant via a widened RETURNING projection — no pre-read helper, no
// extra round-trip.
func TestTenantCapture_update_returningDialects(t *testing.T) {
	cases := []struct {
		name    string
		dialect sql.Dialect
		driver  string
	}{
		{"postgres", sql.NewPostgresDialect(), "pgx"},
		{"sqlite", sql.NewSQLiteDialect(), "stdlib"},
	}
	wants := []string{
		// Single-row by-PK: RETURNING only the tenant column.
		`ReturningColumns: []string{"workspace_id"}`,
		`var rowTenant uuid.UUID`,
		`m.AffectedTenants = []any{capturedTenant}`,
		// UpdateWhere: PK + tenant in one projection, filled in lockstep.
		`affectedTenants = append(affectedTenants, rowTenant)`,
		`m.AffectedTenants = affectedTenants`,
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tc := captureProductContext(config.Dialect(tt.name), config.Driver(tt.driver))
			out := renderTenantCapture(t, tc, "table/update", tt.dialect)
			for _, want := range wants {
				if !strings.Contains(out, want) {
					t.Errorf("%s update output missing %q\nfull output:\n%s", tt.name, want, out)
				}
			}
			if strings.Contains(out, "captureAffectedTenants") {
				t.Errorf("%s update must not emit the MySQL pre-read helper (RETURNING covers capture)\nfull output:\n%s", tt.name, out)
			}
		})
	}
}

// Update on MySQL pre-reads the row's tenant before the UPDATE (no
// RETURNING); UpdateMany performs exactly one batched pre-read before the
// per-item loop; UpdateWhere widens the existing
// collectAffectedIDs pre-SELECT to project the tenant column.
func TestTenantCapture_update_mysqlPreRead(t *testing.T) {
	tc := captureProductContext("mysql", "stdlib")
	out := renderTenantCapture(t, tc, "table/update", sql.NewMySQLDialect())

	wants := []string{
		// Single-row by-PK pre-read against the same conditions as the UPDATE.
		"tenantByPK, err := c.captureAffectedTenants(ctx, conn, updateConds)",
		"capturedTenant := tenantByPK[id]",
		"m.AffectedTenants = []any{capturedTenant}",
		// UpdateMany: one batched pre-read, IN over every item PK, tenant
		// filter appended only when tenancy applies.
		`preConds := []sql.Condition{sql.Where(c.dialect.QuoteIdentifier("id")).In(toAnySlice(itemPKs)...)}`,
		"tenantByPK, err = c.captureAffectedTenants(ctx, conn, preConds)",
		"m.AffectedTenants = affectedTenants",
		// UpdateWhere: widened collect returns index-aligned tenants.
		"ids, affectedTenants, err := c.collectAffectedIDs(ctx, conn, conds)",
		// Widened collect projects PK + tenant in one query.
		`Columns:    []string{"id", "workspace_id"}`,
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("mysql update output missing %q\nfull output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ReturningColumns") {
		t.Errorf("mysql update must not emit RETURNING\nfull output:\n%s", out)
	}
	// Exactly one batched pre-read call in UpdateMany — not per-item.
	if got := strings.Count(out, "c.captureAffectedTenants(ctx, conn, preConds)"); got != 1 {
		t.Errorf("mysql UpdateMany batched pre-read count = %d, want 1", got)
	}
}

// HardDelete / HardDeleteMany / HardDeleteWhere capture per dialect: RETURNING
// on PostgreSQL/SQLite, pre-read on MySQL. (Soft delete and restore share the
// same emission sites and are exercised end-to-end by the tenancy example.)
func TestTenantCapture_delete_acrossDialects(t *testing.T) {
	base := func(dialectName config.Dialect, driver config.Driver) gen.TableContext {
		tc := tenantedProductContext()
		tc.Dialect = dialectName
		tc.Driver = driver
		tc.Operations = gen.ResolvedOperations{HardDelete: true}
		return tc
	}

	t.Run("postgres returning", func(t *testing.T) {
		out := renderTenantCapture(t, base("postgres", "pgx"), "table/delete", sql.NewPostgresDialect())
		wants := []string{
			// Single-row: tenant-only RETURNING on the DELETE.
			`ReturningColumns: []string{"workspace_id"}`,
			"m.AffectedTenants = []any{capturedTenant}",
			// Many/Where: PK + tenant projection.
			`"id", "workspace_id"`,
			"m.AffectedTenants = affectedTenants",
		}
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Errorf("postgres delete output missing %q\nfull output:\n%s", want, out)
			}
		}
	})

	t.Run("mysql pre-read", func(t *testing.T) {
		out := renderTenantCapture(t, base("mysql", "stdlib"), "table/delete", sql.NewMySQLDialect())
		wants := []string{
			"tenantByPK, err := c.captureAffectedTenants(ctx, conn, hardConds)",
			"m.AffectedTenants = []any{capturedTenant}",
			"ids, affectedTenants, err := c.collectAffectedIDs(ctx, conn, conds)",
			"m.AffectedTenants = affectedTenants",
		}
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Errorf("mysql delete output missing %q\nfull output:\n%s", want, out)
			}
		}
		if strings.Contains(out, "ReturningColumns") {
			t.Errorf("mysql delete must not emit RETURNING\nfull output:\n%s", out)
		}
	})
}

// Create captures the tenant value actually written to the row — the
// resolved tenant on the normal path, the caller-supplied input value under
// SkipTenancy / required:false — into the same carrier, on every dialect
// (the capture is Go-side; no SQL widening needed).
func TestTenantCapture_create_capturesInsertedValue(t *testing.T) {
	cases := []struct {
		name    string
		dialect sql.Dialect
		driver  string
	}{
		{"postgres", sql.NewPostgresDialect(), "pgx"},
		{"mysql", sql.NewMySQLDialect(), "stdlib"},
		{"sqlite", sql.NewSQLiteDialect(), "stdlib"},
	}
	wants := []string{
		"var capturedTenant any",
		"capturedTenant = resolvedTenant",
		"capturedTenant = v",
		"m.AffectedTenants = []any{capturedTenant}",
		// CreateMany fills the carrier in lockstep inside the input loop.
		"allTenants = append(allTenants, workspaceID)",
		"m.AffectedTenants = allTenants",
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tc := captureProductContext(config.Dialect(tt.name), config.Driver(tt.driver))
			out := renderTenantCapture(t, tc, "table/create", tt.dialect)
			for _, want := range wants {
				if !strings.Contains(out, want) {
					t.Errorf("%s create output missing %q\nfull output:\n%s", tt.name, want, out)
				}
			}
		})
	}
}

// Upsert captures the resulting row's tenant structurally: the widened
// insertUpsertAndResolveID RETURNING on PostgreSQL, a post-statement read on
// MySQL. On the conflict-update path the existing row keeps its tenant, so
// reading it back (rather than trusting the input) is what makes the cache
// key correct.
func TestTenantCapture_upsert_capturesRowTenant(t *testing.T) {
	t.Run("postgres returning", func(t *testing.T) {
		tc := tenantedProductUpsertContext()
		out := renderTenantCapture(t, tc, "table/upsert", sql.NewPostgresDialect())
		wants := []string{
			"id, capturedTenant, err := c.insertUpsertAndResolveID(ctx, conn, columns, args, conflictColumns, updateColumns)",
			"m.AffectedTenants = []any{capturedTenant}",
			") (uuid.UUID, any, error) {",
			`"id", "workspace_id"`,
		}
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Errorf("postgres upsert output missing %q\nfull output:\n%s", want, out)
			}
		}
	})

	t.Run("mysql post-read", func(t *testing.T) {
		tc := tenantedProductUpsertContext()
		tc.Dialect = "mysql"
		tc.Driver = "stdlib"
		out := renderTenantCapture(t, tc, "table/upsert", sql.NewMySQLDialect())
		wants := []string{
			`tenantByPK, err := c.captureAffectedTenants(ctx, conn, []sql.Condition{sql.Where(c.dialect.QuoteIdentifier("id")).Eq(pkValue)})`,
			"capturedTenant := tenantByPK[pkValue]",
			"m.AffectedTenants = []any{capturedTenant}",
		}
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Errorf("mysql upsert output missing %q\nfull output:\n%s", want, out)
			}
		}
	})
}

// Increment captures via BuildIncrementReturning on PostgreSQL/SQLite and the
// pre-read on MySQL.
func TestTenantCapture_increment_acrossDialects(t *testing.T) {
	base := func(dialectName config.Dialect, driver config.Driver) gen.TableContext {
		tc := tenantedProductContext()
		tc.Dialect = dialectName
		tc.Driver = driver
		tc.Operations = gen.ResolvedOperations{Increment: true}
		tc.IncrementColumns = []gen.ColumnContext{
			{Name: "view_count", FieldName: "ViewCount", GoType: "int64", DBTag: "view_count", JSONTag: "view_count"},
		}
		return tc
	}

	t.Run("postgres returning", func(t *testing.T) {
		out := renderTenantCapture(t, base("postgres", "pgx"), "table/increment", sql.NewPostgresDialect())
		wants := []string{
			`query, args := sql.BuildIncrementReturning(c.dialect, c.table, string(input.Column), input.Amount, incConds, []string{"workspace_id"})`,
			"m.AffectedTenants = []any{capturedTenant}",
		}
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Errorf("postgres increment output missing %q\nfull output:\n%s", want, out)
			}
		}
	})

	t.Run("mysql pre-read", func(t *testing.T) {
		out := renderTenantCapture(t, base("mysql", "stdlib"), "table/increment", sql.NewMySQLDialect())
		wants := []string{
			"tenantByPK, err := c.captureAffectedTenants(ctx, conn, incConds)",
			"capturedTenant := tenantByPK[id]",
			"m.AffectedTenants = []any{capturedTenant}",
		}
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Errorf("mysql increment output missing %q\nfull output:\n%s", want, out)
			}
		}
	})
}

// The captureAffectedTenants helper is emitted only where it is needed, on a
// tenanted table whose tenant is outside the PK. Two shapes need it, and every
// other must not carry it:
//
//   - MySQL, which has no RETURNING, for the by-PK mutations that capture a
//     tenant (Update / UpdateMany / Upsert / the deletes / Increment).
//   - UpsertMany on any dialect, but only when the key is caller-known — a
//     conflict leaves the existing row's tenant in place and the DO NOTHING
//     branch returns no row, so RETURNING cannot supply it (PRD §9.5).
//     A database-generated key resolves its tenants inside
//     resolveUpsertManyRows instead, so the helper would be dead code there.
//
// Both UpsertMany subtests below exist because the fixture defaults
// Operations.UpsertMany to false: this file builds its TableContexts by hand,
// so an arm no subtest sets is an arm no subtest covers (the
// hand-built-fixture blind spot).
func TestTenantCapture_client_preReadHelperEmission(t *testing.T) {
	t.Run("mysql tenanted emits helper", func(t *testing.T) {
		tc := captureProductContext("mysql", "stdlib")
		out := renderTenantCapture(t, tc, "table/client", sql.NewMySQLDialect())
		wants := []string{
			"func (c *productClient) captureAffectedTenants(ctx context.Context, conn database.Querier, conds []sql.Condition) (map[uuid.UUID]any, error) {",
			`Columns:    []string{"id", "workspace_id"}`,
		}
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Errorf("mysql client output missing %q\nfull output:\n%s", want, out)
			}
		}
	})

	t.Run("postgres omits helper", func(t *testing.T) {
		tc := captureProductContext("postgres", "pgx")
		out := renderTenantCapture(t, tc, "table/client", sql.NewPostgresDialect())
		if strings.Contains(out, "captureAffectedTenants") {
			t.Errorf("postgres client must not emit the pre-read helper (RETURNING covers capture)\nfull output:\n%s", out)
		}
	})

	t.Run("postgres UpsertMany with a caller-known key emits helper", func(t *testing.T) {
		// PKStrategyApp: the key is minted before the INSERT, so UpsertMany
		// takes it from the inputs and reads the tenants back by PK.
		tc := captureProductContext("postgres", "pgx")
		tc.Operations.UpsertMany = true
		out := renderTenantCapture(t, tc, "table/client", sql.NewPostgresDialect())
		if !strings.Contains(out, "func (c *productClient) captureAffectedTenants(") {
			t.Errorf("postgres client must emit the helper for a caller-known-key UpsertMany (PRD §9.5, §29.5)\nfull output:\n%s", out)
		}
	})

	t.Run("postgres UpsertMany with a database-generated key omits helper", func(t *testing.T) {
		// PKStrategyDB: resolveUpsertManyRows reads the tenant column in the
		// same statement that resolves the key, so the helper has no caller
		// and must not be emitted.
		tc := captureProductContext("postgres", "pgx")
		tc.Operations.UpsertMany = true
		tc.PKStrategy = config.PKStrategyDB
		out := renderTenantCapture(t, tc, "table/client", sql.NewPostgresDialect())
		if strings.Contains(out, "captureAffectedTenants") {
			t.Errorf("a database-generated key resolves its own tenants; the helper would be dead code\nfull output:\n%s", out)
		}
	})

	t.Run("mysql non-tenanted omits helper", func(t *testing.T) {
		tc := captureProductContext("mysql", "stdlib")
		tc.Tenancy = &gen.TableTenancyContext{Tenanted: false}
		out := renderTenantCapture(t, tc, "table/client", sql.NewMySQLDialect())
		if strings.Contains(out, "captureAffectedTenants") {
			t.Errorf("non-tenanted client must not emit the pre-read helper\nfull output:\n%s", out)
		}
	})
}

// Tenant-in-PK tables must emit no carrier and no capture plumbing at all —
// the tenant is read from the PK struct in AffectedPKs, so the
// output stays free of AffectedTenants on every template.
func TestTenantCapture_tenantInPK_noCarrier(t *testing.T) {
	templates := []string{"table/update", "table/delete", "table/create", "table/client"}
	for _, name := range templates {
		t.Run(name, func(t *testing.T) {
			tc := tenantInPKOrderItemContext()
			out := renderTenantCapture(t, tc, name, sql.NewPostgresDialect())
			for _, banned := range []string{"AffectedTenants", "captureAffectedTenants", "capturedTenant"} {
				if strings.Contains(out, banned) {
					t.Errorf("tenant-in-PK %s output unexpectedly contains %q\nfull output:\n%s", name, banned, out)
				}
			}
		})
	}
}

// Non-tenanted tables in a tenancy-enabled project must render byte-identical
// mutation bodies with no capture plumbing (the additive-only invariant that
// keeps non-tenanted goldens unchanged).
func TestTenantCapture_nonTenanted_noCarrier(t *testing.T) {
	templates := []string{"table/update", "table/delete", "table/create"}
	for _, name := range templates {
		t.Run(name, func(t *testing.T) {
			tc := captureProductContext("postgres", "pgx")
			tc.Operations.HardDelete = true
			tc.Tenancy = &gen.TableTenancyContext{Tenanted: false}
			out := renderTenantCapture(t, tc, name, sql.NewPostgresDialect())
			for _, banned := range []string{"AffectedTenants", "captureAffectedTenants", "capturedTenant", "allTenants"} {
				if strings.Contains(out, banned) {
					t.Errorf("non-tenanted %s output unexpectedly contains %q\nfull output:\n%s", name, banned, out)
				}
			}
		})
	}
}

// compileCheckTenantCapture wraps a rendered template body in a package
// preamble and runs it through the goimports parser — the same syntax-level
// compile gate the other *_compilesCleanly tests use. The pgx-batch and
// MySQL capture paths have no compiled E2E example (the tenancy example
// is SQLite), so this pins their syntactic validity
// per dialect.
func compileCheckTenantCapture(t *testing.T, tc gen.TableContext, templateName string, dialect sql.Dialect, filename string) {
	t.Helper()
	out := renderTenantCapture(t, tc, templateName, dialect)
	wrapped := gen.WrapWithPreamble(tc.Package, tc.Imports, []byte(out))
	if _, err := gen.FormatOnly(wrapped, "v0.0.0-test", filename); err != nil {
		t.Fatalf("Format() failed — generated %s code has syntax errors: %v", templateName, err)
	}
}

// The pgx-batch UpdateMany capture block (br.Query scan loop) and the MySQL
// pre-read plumbing are the most intricate new emissions and have no
// compiled example on their dialect yet — pin their syntax on every
// template × dialect combination the capture touches.
func TestTenantCapture_rendersSyntacticallyValidGo(t *testing.T) {
	fullOps := func(dialectName config.Dialect, driver config.Driver) gen.TableContext {
		tc := captureProductContext(dialectName, driver)
		tc.Operations.SoftDelete = true
		tc.Operations.Restore = true
		tc.Operations.HardDelete = true
		tc.Operations.Upsert = true
		tc.Operations.Increment = true
		tc.SoftDelete = &gen.SoftDeleteContext{Column: "deleted_at", FieldName: "DeletedAt", Strategy: "timestamp"}
		tc.ConflictTargets = []gen.ConflictTargetContext{
			{ConstantName: "ProductConflictPK", Columns: []string{"id"}, CoversPK: true, Comment: "PRIMARY KEY (id)"},
		}
		tc.IncrementColumns = []gen.ColumnContext{
			{Name: "view_count", FieldName: "ViewCount", GoType: "int64", DBTag: "view_count", JSONTag: "view_count"},
		}
		return tc
	}
	cases := []struct {
		name    string
		dialect sql.Dialect
		ctx     gen.TableContext
	}{
		{"postgres pgx", sql.NewPostgresDialect(), fullOps("postgres", "pgx")},
		{"mysql stdlib", sql.NewMySQLDialect(), fullOps("mysql", "stdlib")},
		{"sqlite stdlib", sql.NewSQLiteDialect(), fullOps("sqlite", "stdlib")},
	}
	templates := []string{"table/create", "table/update", "table/delete", "table/upsert", "table/increment", "table/client"}
	for _, tt := range cases {
		for _, name := range templates {
			t.Run(tt.name+"/"+name, func(t *testing.T) {
				compileCheckTenantCapture(t, tt.ctx, name, tt.dialect, "products_gen.go")
			})
		}
	}
}

// compositeTenantOutsidePKContext models the remaining PK shape: a composite
// primary key whose columns do NOT include the tenant column (line_items with
// PK (order_id, product_id) and a separate workspace_id). No example schema
// carries this shape, so the render + syntax pin here is its only coverage.
func compositeTenantOutsidePKContext(dialectName config.Dialect, driver config.Driver) gen.TableContext {
	return gen.TableContext{
		StructName:            "LineItem",
		TableName:             "line_items",
		TableNameConstant:     "TableLineItems",
		Schema:                "public",
		Package:               "db",
		VarName:               "l",
		Dialect:               dialectName,
		Driver:                driver,
		PKStrategy:            config.PKStrategyApp,
		UUIDVersion:           "v4",
		PKAutoGenExpr:         "uuid.New()",
		BatchSize:             100,
		QueryLimit:            100,
		CompositePK:           true,
		CompositePKStructName: "LineItemPK",
		Imports: []string{
			"context",
			"fmt",
			"github.com/google/uuid",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
			"github.com/teandresmith/sqlgen/tenancy",
		},
		PKColumns: []gen.ColumnContext{
			{Name: "order_id", FieldName: "OrderID", GoType: "int64", ZeroValue: "0", DBTag: "order_id", JSONTag: "order_id", PrimaryKey: true},
			{Name: "product_id", FieldName: "ProductID", GoType: "int64", ZeroValue: "0", DBTag: "product_id", JSONTag: "product_id", PrimaryKey: true},
		},
		Columns: []gen.ColumnContext{
			{Name: "order_id", FieldName: "OrderID", GoType: "int64", DBTag: "order_id", JSONTag: "order_id", PrimaryKey: true},
			{Name: "product_id", FieldName: "ProductID", GoType: "int64", DBTag: "product_id", JSONTag: "product_id", PrimaryKey: true},
			{Name: "workspace_id", FieldName: "WorkspaceID", GoType: "uuid.UUID", DBTag: "workspace_id", JSONTag: "workspace_id", Import: "github.com/google/uuid"},
			{Name: "quantity", FieldName: "Quantity", GoType: "int64", DBTag: "quantity", JSONTag: "quantity"},
		},
		CreateInputFields: []gen.InputFieldContext{
			{FieldName: "OrderID", GoType: "int64", ColumnName: "order_id", Required: true, JSONTag: "order_id"},
			{FieldName: "ProductID", GoType: "int64", ColumnName: "product_id", Required: true, JSONTag: "product_id"},
			{FieldName: "Quantity", GoType: "int64", ColumnName: "quantity", Required: true, JSONTag: "quantity"},
			{FieldName: "WorkspaceID", GoType: "omittable.Value[uuid.UUID]", ColumnName: "workspace_id", Omittable: true, JSONTag: "workspace_id"},
		},
		UpdateInputFields: []gen.InputFieldContext{
			{FieldName: "Quantity", GoType: "omittable.Value[int64]", ColumnName: "quantity", Omittable: true, JSONTag: "quantity"},
			{FieldName: "WorkspaceID", GoType: "omittable.Value[uuid.UUID]", ColumnName: "workspace_id", Omittable: true, JSONTag: "workspace_id"},
		},
		AllColumnNames: []string{"order_id", "product_id", "quantity", "workspace_id"},
		Operations: gen.ResolvedOperations{
			Get:         true,
			GetMany:     true,
			Create:      true,
			CreateMany:  true,
			Update:      true,
			UpdateMany:  true,
			UpdateWhere: true,
			HardDelete:  true,
		},
		Tenancy: &gen.TableTenancyContext{
			Tenanted:     true,
			Column:       "workspace_id",
			FieldName:    "WorkspaceID",
			GoType:       "uuid.UUID",
			Import:       "github.com/google/uuid",
			Required:     true,
			InPrimaryKey: false,
		},
	}
}

// Composite PK with the tenant OUTSIDE the PK — the capture must key the
// tenant map by the PK struct, widen the composite RETURNING / collect
// projections, and stay syntactically valid on every dialect.
func TestTenantCapture_compositeTenantOutsidePK(t *testing.T) {
	cases := []struct {
		name    string
		dialect sql.Dialect
		ctx     gen.TableContext
	}{
		{"postgres pgx", sql.NewPostgresDialect(), compositeTenantOutsidePKContext("postgres", "pgx")},
		{"mysql stdlib", sql.NewMySQLDialect(), compositeTenantOutsidePKContext("mysql", "stdlib")},
		{"sqlite stdlib", sql.NewSQLiteDialect(), compositeTenantOutsidePKContext("sqlite", "stdlib")},
	}
	templates := []string{"table/create", "table/update", "table/delete", "table/client"}
	for _, tt := range cases {
		for _, name := range templates {
			t.Run(tt.name+"/"+name, func(t *testing.T) {
				compileCheckTenantCapture(t, tt.ctx, name, tt.dialect, "line_items_gen.go")
			})
		}
	}

	// Behavior pins on the composite-specific plumbing.
	pgUpdate := renderTenantCapture(t, compositeTenantOutsidePKContext("postgres", "pgx"), "table/update", sql.NewPostgresDialect())
	for _, want := range []string{
		"tenantByPK := make(map[LineItemPK]any, len(items))",
		"queuedPKs = append(queuedPKs, item.PK)",
		`"order_id", "product_id", "workspace_id"`,
	} {
		if !strings.Contains(pgUpdate, want) {
			t.Errorf("postgres composite update missing %q", want)
		}
	}
	myUpdate := renderTenantCapture(t, compositeTenantOutsidePKContext("mysql", "stdlib"), "table/update", sql.NewMySQLDialect())
	for _, want := range []string{
		"pks, affectedTenants, err := c.collectAffectedPKs(ctx, conn, conds)",
		"sql.BuildCompositePKBatchCondition(c.dialect, []string{\"order_id\", \"product_id\", }, preValues)",
	} {
		if !strings.Contains(myUpdate, want) {
			t.Errorf("mysql composite update missing %q", want)
		}
	}
	myClient := renderTenantCapture(t, compositeTenantOutsidePKContext("mysql", "stdlib"), "table/client", sql.NewMySQLDialect())
	if !strings.Contains(myClient, "map[LineItemPK]any") {
		t.Errorf("mysql composite client helper missing PK-struct-keyed map")
	}
}
