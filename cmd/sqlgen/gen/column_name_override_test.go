package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// overrideSchema returns a single-table schema carrying every surface a
// `column_map.<col>.name` override has to reach: the entity/filter/input
// field, the soft-delete guard, the auto-set update column, and the tenant
// column.
func overrideSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "events",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "osi_layer", Type: "text"},
					{Name: "retry_count", Type: "integer"},
					{Name: "tenant_id", Type: "text"},
					{Name: "updated_at", Type: "timestamptz", Nullable: true},
					{Name: "deleted_at", Type: "timestamptz", Nullable: true},
				},
			},
		},
	}
}

func overrideInput(t *testing.T, overrides map[string]config.ColumnOverride) *gen.GenerateInput {
	t.Helper()
	input := testInput(overrideSchema())
	input.Config.Tables["events"] = config.TableConfig{ColumnMap: overrides}
	return input
}

func buildOverrideTable(t *testing.T, overrides map[string]config.ColumnOverride) gen.TableContext {
	t.Helper()
	contexts, err := gen.BuildTableContexts(overrideInput(t, overrides), nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("BuildTableContexts() returned %d contexts, want 1", len(contexts))
	}
	return contexts[0]
}

func columnByName(t *testing.T, cols []gen.ColumnContext, name string) gen.ColumnContext {
	t.Helper()
	for _, c := range cols {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("column %q not found", name)
	return gen.ColumnContext{}
}

// TestColumnNameOverride_ResolvesOnColumnContext pins the single resolution
// point: FieldName carries the override and FieldNameOverridden records where
// it came from, while every SQL-derived spelling (Name, db/json tags) is
// untouched (PRD §8.5).
func TestColumnNameOverride_ResolvesOnColumnContext(t *testing.T) {
	tc := buildOverrideTable(t, map[string]config.ColumnOverride{
		"osi_layer": {Name: "OSILayer"},
	})

	got := columnByName(t, tc.Columns, "osi_layer")
	if got.FieldName != "OSILayer" {
		t.Errorf("FieldName = %q, want %q", got.FieldName, "OSILayer")
	}
	if !got.FieldNameOverridden {
		t.Error("FieldNameOverridden = false, want true")
	}
	if got.DBTag != "osi_layer" || got.JSONTag != "osi_layer" {
		t.Errorf("tags = (%q, %q), want both %q — the override renames the Go identifier only",
			got.DBTag, got.JSONTag, "osi_layer")
	}

	// An un-overridden column keeps the naming engine's spelling and reports
	// no override, which is what gates the gqlgen row-type entry.
	plain := columnByName(t, tc.Columns, "updated_at")
	if plain.FieldName != "UpdatedAt" || plain.FieldNameOverridden {
		t.Errorf("un-overridden column = (%q, %v), want (%q, false)",
			plain.FieldName, plain.FieldNameOverridden, "UpdatedAt")
	}
}

// TestColumnNameOverride_ReachesDerivedSurfaces walks the context surfaces
// that carry a per-column Go identifier. Each one used to derive its own
// spelling from the SQL name, or reads the column context that now does.
func TestColumnNameOverride_ReachesDerivedSurfaces(t *testing.T) {
	tc := buildOverrideTable(t, map[string]config.ColumnOverride{
		"osi_layer":  {Name: "OSILayer"},
		"deleted_at": {Name: "ArchivedAt"},
		"updated_at": {Name: "TouchedAt"},
	})

	if tc.SoftDelete == nil {
		t.Fatal("SoftDelete should be detected (deleted_at)")
	}
	if tc.SoftDelete.FieldName != "ArchivedAt" {
		t.Errorf("SoftDelete.FieldName = %q, want %q", tc.SoftDelete.FieldName, "ArchivedAt")
	}
	if tc.SoftDelete.Column != "deleted_at" {
		t.Errorf("SoftDelete.Column = %q, want %q — the SQL name is unaffected", tc.SoftDelete.Column, "deleted_at")
	}

	if len(tc.UpdateColumns) != 1 {
		t.Fatalf("UpdateColumns = %v, want one entry", tc.UpdateColumns)
	}
	if tc.UpdateColumns[0].Name != "updated_at" || tc.UpdateColumns[0].FieldName != "TouchedAt" {
		t.Errorf("UpdateColumns[0] = %+v, want {Name: updated_at, FieldName: TouchedAt}", tc.UpdateColumns[0])
	}

	for _, f := range tc.FilterFields {
		if f.ColumnName == "osi_layer" && f.FieldName != "OSILayer" {
			t.Errorf("FilterFields[osi_layer].FieldName = %q, want %q", f.FieldName, "OSILayer")
		}
	}
	for _, f := range tc.CreateInputFields {
		if f.ColumnName == "osi_layer" && f.FieldName != "OSILayer" {
			t.Errorf("CreateInputFields[osi_layer].FieldName = %q, want %q", f.FieldName, "OSILayer")
		}
	}
	for _, f := range tc.UpdateInputFields {
		if f.ColumnName == "osi_layer" && f.FieldName != "OSILayer" {
			t.Errorf("UpdateInputFields[osi_layer].FieldName = %q, want %q", f.FieldName, "OSILayer")
		}
	}
}

// TestColumnNameOverride_TenantFieldName covers the tenancy surface, which
// resolves its field name in a post-pass over the built tables and therefore
// has to read the column context rather than re-derive from the column name.
func TestColumnNameOverride_TenantFieldName(t *testing.T) {
	input := overrideInput(t, map[string]config.ColumnOverride{
		"tenant_id": {Name: "OrgID"},
	})
	input.Config.Tenancy = &config.TenancyConfig{
		Enabled: true,
		Column:  "tenant_id",
	}

	contexts, err := gen.BuildTableContextsFromSchema(input.Schema, input.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	tc := contexts[0]
	if tc.Tenancy == nil || !tc.Tenancy.Tenanted {
		t.Fatalf("Tenancy = %+v, want a tenanted table", tc.Tenancy)
	}
	if tc.Tenancy.FieldName != "OrgID" {
		t.Errorf("Tenancy.FieldName = %q, want %q", tc.Tenancy.FieldName, "OrgID")
	}
}

// TestColumnNameOverride_RelationshipFKFieldName is the cross-table case: the
// O2M loader spells the FK field on the *target* struct, so a rename on the
// child table has to reach the parent's relationship context. Resolving it
// from the relationship's own FK column name — as the code once did
// — silently produces a field the target struct does not have.
func TestColumnNameOverride_RelationshipFKFieldName(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name: "orders",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid"},
				},
			},
		},
		Relationships: []parser.Relationship{
			{
				Name:        "order",
				Type:        parser.OneToMany,
				SourceTable: "users",
				TargetTable: "orders",
				FKColumn:    "user_id",
			},
		},
	}
	input := testInput(schema)
	// The rename lives on the child (orders) table; the assertion is on the
	// parent (users) table's relationship.
	input.Config.Tables["orders"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			"user_id": {Name: "OwnerID"},
		},
	}

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	var users gen.TableContext
	for _, tc := range contexts {
		if tc.TableName == "users" {
			users = tc
		}
	}
	if len(users.O2MRelationships) == 0 {
		t.Fatalf("users has no O2M relationships; got %d table contexts", len(contexts))
	}
	rel := users.O2MRelationships[0]
	if rel.FKColumn != "user_id" {
		t.Fatalf("FKColumn = %q, want %q", rel.FKColumn, "user_id")
	}
	if rel.FKFieldName != "OwnerID" {
		t.Errorf("FKFieldName = %q, want %q — the FK field lives on the target table, which renamed it",
			rel.FKFieldName, "OwnerID")
	}
}

// TestColumnNameOverride_CollisionRejected pins the resolved-name collision
// rule. Only collisions an override introduces are reported, so a schema that
// already produced duplicate field names keeps generating as before.
func TestColumnNameOverride_CollisionRejected(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]config.ColumnOverride
		wantErr   string
	}{
		{
			name:      "distinct names accepted",
			overrides: map[string]config.ColumnOverride{"osi_layer": {Name: "OSILayer"}},
		},
		{
			name:      "collides with another column's default name",
			overrides: map[string]config.ColumnOverride{"osi_layer": {Name: "UpdatedAt"}},
			wantErr:   `Go field name "UpdatedAt" is claimed by both`,
		},
		{
			name: "two overrides collide",
			overrides: map[string]config.ColumnOverride{
				"osi_layer": {Name: "Layer"},
				"tenant_id": {Name: "Layer"},
			},
			wantErr: `Go field name "Layer" is claimed by both`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gen.BuildTableContexts(overrideInput(t, tt.overrides), nil)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("BuildTableContexts() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("BuildTableContexts() error = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("BuildTableContexts() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestColumnNameOverride_CollidesWithRelationshipField covers the other half of
// the collision domain: relationship fields land on the same entity struct as
// columns (templates/table/model.go.tmpl), so an override may not claim one.
func TestColumnNameOverride_CollidesWithRelationshipField(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "label", Type: "text"},
				},
			},
			{
				Name: "orders",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid"},
				},
			},
		},
		Relationships: []parser.Relationship{
			{
				Name:        "order",
				Type:        parser.OneToMany,
				SourceTable: "users",
				TargetTable: "orders",
				FKColumn:    "user_id",
			},
		},
	}
	input := testInput(schema)
	// The O2M relationship emits the field `Orders` (pluralized); claiming it
	// for a column would produce a duplicate field on the entity struct.
	input.Config.Tables["users"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			"label": {Name: "Orders"},
		},
	}

	_, err := gen.BuildTableContexts(input, nil)
	want := `Go field name "Orders" is claimed by both`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("BuildTableContexts() error = %v, want it to contain %q", err, want)
	}
}

// TestColumnNameOverride_UpdateOps pins the `_inc` / `_dec` operator surface,
// which builds three Go identifiers off the column and used to re-PascalCase
// the SQL name for them. The increment *constant* name follows the rename too:
// the generated enum (`increment.go.tmpl`) and the resolver constant
// (`APIUpdateOp.IncColumnConst`) both derive from FieldName and must agree, or
// the resolver references a constant that does not exist.
func TestColumnNameOverride_UpdateOps(t *testing.T) {
	in := apiTestInput(t, overrideSchema())
	in.Config.Tables["events"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			"retry_count": {Name: "Attempts"},
		},
	}

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext() error: %v", err)
	}

	var op gen.APIUpdateOp
	for _, o := range apiCtx.Tables[0].UpdateOps {
		if o.SQLName == "retry_count" {
			op = o
		}
	}
	if op.SQLName == "" {
		t.Fatalf("no UpdateOps entry for retry_count; got %+v", apiCtx.Tables[0].UpdateOps)
	}

	for _, tt := range []struct {
		field string
		got   string
		want  string
	}{
		{"SetGoField", op.SetGoField, "Attempts"},
		{"IncGoField", op.IncGoField, "AttemptsInc"},
		{"DecGoField", op.DecGoField, "AttemptsDec"},
		{"IncColumnConst", op.IncColumnConst, "EventIncrementAttempts"},
		// The GraphQL operator names stay derived from the SQL column.
		{"IncGraphQLName", op.IncGraphQLName, "retryCount_inc"},
		{"DecGraphQLName", op.DecGraphQLName, "retryCount_dec"},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.field, tt.got, tt.want)
		}
	}

	// The generated increment enum spells the same identifier.
	for _, c := range tables[0].IncrementColumns {
		if c.Name == "retry_count" && c.FieldName != "Attempts" {
			t.Errorf("IncrementColumns[retry_count].FieldName = %q, want %q", c.FieldName, "Attempts")
		}
	}
}
