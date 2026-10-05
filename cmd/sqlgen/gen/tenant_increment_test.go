package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// The tenant column must be absent from *every* increment surface
// (PRD §8.2, §29.4.2). The API side honored that from the start; the model
// side did not, so `<T>IncrementColumn` carried the tenant column and
// `client.Counters().Increment(ctx, id, CounterIncrementTenantID, 1)` compiled
// and moved a row between tenants — Increment constrains which rows it matches
// but not which column it targets, so the tenant predicate in the WHERE does
// not protect the SET.
//
// The shape is reachable when the tenant column resolves to an arithmetic Go
// type — integer, float, or decimal — and carries no declared FK: the FK
// exclusion masks the common tenant column, and a uuid one is not an
// incrementable Go type at all. Every example's tenant column is uuid / blob /
// char, which is why no golden ever showed this.

// tenantModelTables builds the model-side contexts for a schema with tenancy
// enabled on `tenant_id` — every fixture below names the column that way —
// keyed by SQL table name.
//
// BuildTableContextsFromSchema is the entry point rather than
// BuildTableContexts because the tenant exclusion lands in
// attachTenancyToTables — the tenancy fact is not known while the table
// context is being assembled, so the half-pipeline entry point leaves every
// column looking untenanted.
func tenantModelTables(t *testing.T, schema *parser.Schema, required bool) map[string]gen.TableContext {
	t.Helper()
	cfg := testInput(schema).Config
	cfg.Tenancy = &config.TenancyConfig{
		Enabled:  true,
		Column:   "tenant_id",
		Required: new(required),
	}
	tables, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() unexpected error: %v", err)
	}
	byName := make(map[string]gen.TableContext, len(tables))
	for _, tc := range tables {
		byName[tc.TableName] = tc
	}
	return byName
}

// TestTenantColumn_ExcludedFromIncrementColumns is the model-side regression.
// Both `tenancy.required` settings are covered: the exclusion does not depend
// on it, because required:false's documented cross-tenant path names a target
// tenant through `Update`, which arithmetic cannot do.
func TestTenantColumn_ExcludedFromIncrementColumns(t *testing.T) {
	tests := []struct {
		name     string
		required bool
	}{
		{name: "required tenant", required: true},
		{name: "optional tenant", required: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counters := tenantModelTables(t, counterTenantSchema(), tt.required)["counters"]

			if counters.Tenancy == nil || !counters.Tenancy.Tenanted {
				t.Fatalf("Tenancy = %+v, want a tenanted table", counters.Tenancy)
			}
			got := incrementColumnNames(counters)
			if len(got) != 1 || got[0] != "hits" {
				t.Errorf("IncrementColumns = %v, want [hits] — the tenant column must not reach CounterIncrementColumn", got)
			}

			// The flag the exclusion reads is stamped on the column itself, so
			// every consumer of a resolved ColumnContext sees the same fact.
			for _, c := range counters.Columns {
				if want := c.Name == "tenant_id"; c.Tenant != want {
					t.Errorf("Columns[%s].Tenant = %v, want %v", c.Name, c.Tenant, want)
				}
			}
		})
	}
}

// TestTenantColumn_AbsentFromIncrementTemplate pins the rendered surface, not
// just the context: the enum constant is what makes the cross-tenant call
// compile, so its absence is the property worth holding.
func TestTenantColumn_AbsentFromIncrementTemplate(t *testing.T) {
	counters := tenantModelTables(t, counterTenantSchema(), true)["counters"]
	tmpl := loadTableTemplates(t, "increment.go.tmpl")
	out := renderTableTemplate(t, tmpl, "table/increment", counters)

	if strings.Contains(out, "CounterIncrementTenantID") {
		t.Error("rendered increment.go.tmpl declares CounterIncrementTenantID; Increment would move a row between tenants")
	}
	if !strings.Contains(out, "CounterIncrementHits") {
		t.Error("rendered increment.go.tmpl lost CounterIncrementHits; only the tenant column should be excluded")
	}
}

// TestTenantColumn_ExcludedFromAPIUpdateOps is the API-side half. The skip
// buildUpdateOps used to carry is gone — the model-side exclusion now feeds
// apiIncrementColumns — so this asserts the surface, which is what the skip
// was ever protecting. Under required:false the operators used to be emitted;
// that is the deliberate behavior change, and a non-zero resolver on such a
// config still scopes the WHERE, so the hole was real there too.
func TestTenantColumn_ExcludedFromAPIUpdateOps(t *testing.T) {
	tests := []struct {
		name     string
		required bool
	}{
		{name: "required tenant", required: true},
		{name: "optional tenant", required: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counters := tenantAPITables(t, counterTenantSchema(), "tenant_id", tt.required)["counters"]

			for _, op := range counters.UpdateOps {
				if op.SQLName == "tenant_id" {
					t.Error("UpdateOps carries the tenant column; the resolver would dispatch an Increment that moves the row between tenants")
				}
			}
			updateBlock := inputBlock(t, renderAPITableSchema(t, counters), "UpdateCounterInput")
			mustNotContain(t, updateBlock, "tenantID_inc", "tenantID_dec")
			mustContainAll(t, updateBlock, "hits_inc: Int", "hits_dec: Int")
		})
	}
}

// TestTenantColumn_SoleIncrementableSuppressesSurface is the increment-side
// analogue of TestTenantColumn_OnlyWritableColumnSuppressesUpdateSurface: with
// the tenant column gone there is nothing left to increment, so the enum type
// and the method go together rather than emitting an empty `const ( )` block.
func TestTenantColumn_SoleIncrementableSuppressesSurface(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{{
			Name: "seats",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()"},
				{Name: "tenant_id", Type: "bigint"},
				{Name: "label", Type: "text"},
			},
		}},
	}
	seats := tenantModelTables(t, schema, true)["seats"]

	if got := incrementColumnNames(seats); len(got) != 0 {
		t.Errorf("IncrementColumns = %v, want empty", got)
	}
	tmpl := loadTableTemplates(t, "increment.go.tmpl")
	out := renderTableTemplate(t, tmpl, "table/increment", seats)
	for _, unwanted := range []string{"SeatIncrementColumn", "func (c *seatClient) Increment("} {
		if strings.Contains(out, unwanted) {
			t.Errorf("rendered increment.go.tmpl contains %q with no eligible column", unwanted)
		}
	}
}

// TestTenantColumn_UntenantedTableUnaffected guards the blast radius: an
// integer column named like a tenant on a table the tenancy map does not mark
// keeps its increment surface.
func TestTenantColumn_UntenantedTableUnaffected(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "counters",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()"},
					{Name: "tenant_id", Type: "bigint"},
					{Name: "hits", Type: "bigint"},
				},
			},
			{
				// No tenant_id column, so the tenancy map marks it shared.
				Name: "audit_logs",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()"},
					{Name: "weight", Type: "bigint"},
				},
			},
		},
	}
	tables := tenantModelTables(t, schema, true)

	audit := tables["audit_logs"]
	if audit.Tenancy != nil && audit.Tenancy.Tenanted {
		t.Fatalf("audit_logs Tenancy = %+v, want untenanted", audit.Tenancy)
	}
	if got := incrementColumnNames(audit); len(got) != 1 || got[0] != "weight" {
		t.Errorf("audit_logs IncrementColumns = %v, want [weight]", got)
	}
	for _, c := range audit.Columns {
		if c.Tenant {
			t.Errorf("audit_logs Columns[%s].Tenant = true on an untenanted table", c.Name)
		}
	}
}
