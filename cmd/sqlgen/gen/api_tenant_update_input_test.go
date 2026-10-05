package gen_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// tenantUpdateSchema covers the shapes the §29.4.2 update-input rule must
// separate:
//
//   - labels          — uuid tenant column, NOT in the PK. The subject.
//   - counters        — bigint tenant column, NOT in the PK. Same rule, plus
//     the `_inc` / `_dec` operator pair an incrementable
//     tenant column would otherwise expose.
//   - tenant_settings — tenant column IN the composite PK. Already excluded
//     from the update input by the PK rule; must not change.
//   - shared_ref      — no tenant column at all. Must not change.
//
// A single tenant Go type across the tenanted tables satisfies §29.2.4, so
// the uuid and bigint cases need separate schemas; tenantUpdateSchema carries
// the uuid ones and counterTenantSchema the bigint one.
func tenantUpdateSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "labels",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()"},
					{Name: "workspace_id", Type: "uuid"},
					{Name: "name", Type: "text"},
					{Name: "sort_order", Type: "integer"},
				},
			},
			{
				Name: "tenant_settings",
				Columns: []parser.Column{
					{Name: "workspace_id", Type: "uuid", PrimaryKey: true},
					{Name: "key", Type: "text", PrimaryKey: true},
					{Name: "value", Type: "text"},
				},
			},
			{
				Name: "shared_ref",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()"},
					{Name: "name", Type: "text"},
				},
			},
		},
	}
}

func counterTenantSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "counters",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()"},
					{Name: "tenant_id", Type: "bigint"},
					{Name: "hits", Type: "bigint"},
				},
			},
		},
	}
}

// tenantAPITables builds the API contexts for a schema with tenancy enabled
// on the given column. `required` drives the §29.4.2 rule under test.
func tenantAPITables(t *testing.T, schema *parser.Schema, column string, required bool) map[string]gen.APITableContext {
	t.Helper()
	in := apiTestInput(t, schema)
	in.Config.Tenancy = &config.TenancyConfig{
		Enabled:  true,
		Column:   column,
		Required: new(required),
	}

	// BuildTableContextsFromSchema runs the full pipeline including tenancy
	// detection and attachment; BuildTableContexts alone leaves
	// TableContext.Tenancy nil.
	tables, err := gen.BuildTableContextsFromSchema(schema, in.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	byName := make(map[string]gen.APITableContext, len(apiCtx.Tables))
	for _, tc := range apiCtx.Tables {
		byName[tc.SQLTable] = tc
	}
	return byName
}

// fieldFlags returns a table's (InUpdateInput, found) for one column.
func fieldFlags(t *testing.T, tc gen.APITableContext, sqlName string) bool {
	t.Helper()
	for _, f := range tc.Fields {
		if f.SQLName == sqlName {
			return f.InUpdateInput
		}
	}
	t.Fatalf("column %q not found on %q", sqlName, tc.SQLTable)
	return false
}

// TestTenantColumn_ExcludedFromUpdateInput pins that under required:true a
// non-PK tenant column leaves the update surface, while every other shape is
// untouched.
func TestTenantColumn_ExcludedFromUpdateInput(t *testing.T) {
	tables := tenantAPITables(t, tenantUpdateSchema(), "workspace_id", true)

	t.Run("non-PK tenant column is dropped", func(t *testing.T) {
		labels := tables["labels"]
		if fieldFlags(t, labels, "workspace_id") {
			t.Error("InUpdateInput = true for the server-owned tenant column, want false")
		}
		// Ordinary columns are unaffected.
		if !fieldFlags(t, labels, "name") {
			t.Error("InUpdateInput = false for an ordinary writable column, want true")
		}
		// The PK is excluded as always.
		if fieldFlags(t, labels, "id") {
			t.Error("InUpdateInput = true for a PK column, want false")
		}

		out := renderAPITableSchema(t, labels)
		updateBlock := inputBlock(t, out, "UpdateLabelInput")
		mustNotContain(t, updateBlock, "workspaceID")
		mustContain(t, updateBlock, "name: String")

		// The create input keeps it — this fix is update-side only.
		createBlock := inputBlock(t, out, "CreateLabelInput")
		mustContain(t, createBlock, "workspaceID")
	})

	t.Run("translator stops referencing it", func(t *testing.T) {
		for _, f := range tables["labels"].UpdateInputFields {
			if f.ModelFieldName == "WorkspaceID" {
				t.Error("UpdateInputFields still carries the tenant column; the translator would reference a field gqlgen no longer emits")
			}
		}
		// The create translator must still carry it.
		var found bool
		for _, f := range tables["labels"].CreateInputFields {
			if f.ModelFieldName == "WorkspaceID" {
				found = true
			}
		}
		if !found {
			t.Error("CreateInputFields lost the tenant column; only the update input should change")
		}
	})

	t.Run("tenant-in-PK table is unchanged", func(t *testing.T) {
		// Already excluded by the PK rule — no behavior change here.
		if fieldFlags(t, tables["tenant_settings"], "workspace_id") {
			t.Error("InUpdateInput = true for a tenant column inside the PK, want false")
		}
		out := renderAPITableSchema(t, tables["tenant_settings"])
		mustContain(t, inputBlock(t, out, "UpdateTenantSettingInput"), "value: String")
	})

	t.Run("untenanted table is unchanged", func(t *testing.T) {
		if !fieldFlags(t, tables["shared_ref"], "name") {
			t.Error("an untenanted table lost an ordinary update field")
		}
	})
}

// TestTenantColumn_KeptWhenNotRequired pins that under required:false the
// caller-supplied value is genuinely written (the documented cross-tenant
// path), so the field must stay.
func TestTenantColumn_KeptWhenNotRequired(t *testing.T) {
	tables := tenantAPITables(t, tenantUpdateSchema(), "workspace_id", false)

	if !fieldFlags(t, tables["labels"], "workspace_id") {
		t.Error("InUpdateInput = false under tenancy.required:false, want true — that config's zero-resolver path honors the caller's value")
	}
	out := renderAPITableSchema(t, tables["labels"])
	mustContain(t, inputBlock(t, out, "UpdateLabelInput"), "workspaceID")
}

// TestTenantColumn_IncrementOperatorsSuppressed pins that a tenant column gets
// no increment operators. An integer tenant column is incrementable, and
// Increment constrains which rows it matches but not which column it targets —
// so `tenantID_inc: 1` would walk a row out of its own tenant. The operator
// pair must not be emitted, on either the schema side or the resolver-side
// UpdateOps list.
func TestTenantColumn_IncrementOperatorsSuppressed(t *testing.T) {
	tables := tenantAPITables(t, counterTenantSchema(), "tenant_id", true)
	counters := tables["counters"]

	out := renderAPITableSchema(t, counters)
	updateBlock := inputBlock(t, out, "UpdateCounterInput")
	mustNotContain(t, updateBlock, "tenantID", "tenantID_inc", "tenantID_dec")
	// A genuine numeric column keeps its operators.
	mustContainAll(t, updateBlock, "hits: Int", "hits_inc: Int", "hits_dec: Int")

	for _, op := range counters.UpdateOps {
		if op.SQLName == "tenant_id" {
			t.Error("UpdateOps still carries the tenant column; the resolver would dispatch an Increment that moves the row between tenants")
		}
	}
}

// TestTenantColumn_OnlyWritableColumnSuppressesUpdateSurface pins the
// HasUpdateInput edge from §3: when the tenant column is the sole writable
// non-PK column, the input would be empty, so the whole update surface goes
// with it rather than emitting `input X {}` (an empty GraphQL input type is
// invalid SDL).
func TestTenantColumn_OnlyWritableColumnSuppressesUpdateSurface(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{{
			Name: "memberships",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()"},
				{Name: "workspace_id", Type: "uuid"},
			},
		}},
	}
	tables := tenantAPITables(t, schema, "workspace_id", true)
	memberships := tables["memberships"]

	if memberships.HasUpdateInput {
		t.Error("HasUpdateInput = true with no updatable column left, want false")
	}
	out := renderAPITableSchema(t, memberships)
	mustNotContain(
		t, out,
		"input UpdateMembershipInput",
		"updateMembership(",
		"updateMemberships(",
	)
	// Create is unaffected — the tenant column is still in that input.
	mustContain(t, out, "input CreateMembershipInput")
}
