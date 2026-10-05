package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// reservedSchema returns a single-table schema shaped so every condition the
// reserved-name rule tracks has a column that satisfies it and a column that
// does not:
//
//   - `label` is a plain filterable, non-PK, non-arithmetic column;
//   - `retry_count` is increment-eligible (arithmetic, non-PK, not an FK);
//   - `tags` is a jsonb[] column, which is *not* filterable — its element type
//     fails comparator.Slice[T]'s comparable constraint — so it never reaches
//     <T>Filter and the filter-owned names stay free for it;
//   - `id` is the sole PK column, the one that shares Update<T>Item with the
//     template-owned Input field.
func reservedSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "events",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "label", Type: "text"},
					{Name: "retry_count", Type: "integer"},
					{Name: "tags", Type: "jsonb[]"},
				},
			},
		},
	}
}

// compositeReservedSchema is reservedSchema with a two-column PK, the shape
// that emits <T>Filter.PKs and collapses Update<T>Item onto a `PK` field.
func compositeReservedSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "events",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "seq", Type: "integer", PrimaryKey: true},
					{Name: "label", Type: "text"},
					{Name: "retry_count", Type: "integer"},
					{Name: "tags", Type: "jsonb[]"},
				},
			},
		},
	}
}

func buildReserved(t *testing.T, schema *parser.Schema, overrides map[string]config.ColumnOverride) error {
	t.Helper()
	input := testInput(schema)
	input.Config.Tables["events"] = config.TableConfig{ColumnMap: overrides}
	_, err := gen.BuildTableContexts(input, nil)
	return err
}

// TestReservedFieldName_Columns pins the reserved-name half of PRD §8.5: a
// resolved Go field name that the templates already declare on a struct the
// column lands on is rejected, and only in the shapes where that struct
// actually claims it.
//
// Every reserved name appears twice — once on a column whose shape makes the
// owner claim it, once on a column whose shape does not — so a rule that
// over-rejects fails as loudly as one that under-rejects.
func TestReservedFieldName_Columns(t *testing.T) {
	tests := []struct {
		name      string
		composite bool
		column    string
		fieldName string
		wantErr   string
	}{
		// <T>FieldOptions carries every column, so its three methods are
		// claimed regardless of shape.
		{
			name: "Columns on any column", column: "label", fieldName: "Columns",
			wantErr: "EventFieldOptions (method Columns)",
		},
		{
			name: "ColumnMap on any column", column: "label", fieldName: "ColumnMap",
			wantErr: "EventFieldOptions (method ColumnMap)",
		},
		{
			name: "HasSelectedColumns on any column", column: "label", fieldName: "HasSelectedColumns",
			wantErr: "EventFieldOptions (method HasSelectedColumns)",
		},
		{
			name: "Columns on a non-filterable column", column: "tags", fieldName: "Columns",
			wantErr: "EventFieldOptions (method Columns)",
		},

		// <T>Filter carries only the filterable columns.
		{
			name: "And on a filterable column", column: "label", fieldName: "And",
			wantErr: "EventFilter (field And)",
		},
		{
			name: "Or on a filterable column", column: "label", fieldName: "Or",
			wantErr: "EventFilter (field Or)",
		},
		{
			name: "ToConditions on a filterable column", column: "label", fieldName: "ToConditions",
			wantErr: "EventFilter (method ToConditions)",
		},
		{name: "And on a non-filterable column", column: "tags", fieldName: "And"},
		{name: "Or on a non-filterable column", column: "tags", fieldName: "Or"},
		{name: "ToConditions on a non-filterable column", column: "tags", fieldName: "ToConditions"},

		// PKs exists on <T>Filter only under a composite PK.
		{
			name: "PKs under a composite PK", composite: true, column: "label", fieldName: "PKs",
			wantErr: "EventFilter (field PKs)",
		},
		{name: "PKs under a single PK", column: "label", fieldName: "PKs"},
		{name: "PKs on a non-filterable column", composite: true, column: "tags", fieldName: "PKs"},

		// Input sits on Update<T>Item beside the single PK column's field.
		{
			name: "Input on the sole PK column", column: "id", fieldName: "Input",
			wantErr: "UpdateEventItem (field Input)",
		},
		{name: "Input on a non-PK column", column: "label", fieldName: "Input"},
		{name: "Input on a composite PK column", composite: true, column: "id", fieldName: "Input"},

		// Column would redeclare the <T>IncrementColumn type at package scope.
		{
			name: "Column on an increment-eligible column", column: "retry_count", fieldName: "Column",
			wantErr: "EventIncrementColumn (the increment enum type)",
		},
		{name: "Column on a non-arithmetic column", column: "label", fieldName: "Column"},
		{name: "Column on the PK column", column: "id", fieldName: "Column"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := reservedSchema()
			if tt.composite {
				schema = compositeReservedSchema()
			}
			err := buildReserved(t, schema, map[string]config.ColumnOverride{
				tt.column: {Name: tt.fieldName},
			})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("BuildTableContexts() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("BuildTableContexts() error = nil, want it to name %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("BuildTableContexts() error = %q, want it to contain %q", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), "tables.events.column_map."+tt.column+".name") {
				t.Errorf("BuildTableContexts() error = %q, want it to name the per-column escape hatch", err)
			}
		})
	}
}

// TestReservedFieldName_NotOverrideGated is the semantic that separates this
// rule from the duplicate-name rule beside it: a reserved name is rejected
// wherever it comes from, because no override is needed to produce it and the
// generated code cannot compile either way. A table with a plain `columns`
// column emits `Columns bool` and `func (fo *EventFieldOptions) Columns()` on
// the same struct.
func TestReservedFieldName_NotOverrideGated(t *testing.T) {
	schema := reservedSchema()
	schema.Tables[0].Columns = append(schema.Tables[0].Columns, parser.Column{Name: "columns", Type: "text"})

	err := buildReserved(t, schema, nil)
	if err == nil {
		t.Fatal("BuildTableContexts() error = nil, want the plain column name rejected")
	}
	if !strings.Contains(err.Error(), `column "columns" resolves to Go field name "Columns"`) {
		t.Errorf("BuildTableContexts() error = %q, want it to name the column and its resolved field", err)
	}
}

// TestReservedFieldName_Relationship covers the other half of the ungated
// domain: relationship fields land on the entity struct and on
// <T>FieldOptions, so a relationship whose field name is one of that struct's
// three methods is rejected too. Relationship names are derived rather than
// configurable, so the message points at exclude_relationships.
func TestReservedFieldName_Relationship(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name: "column",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid"},
				},
			},
		},
		Relationships: []parser.Relationship{
			{
				Name:        "column",
				Type:        parser.OneToMany,
				SourceTable: "users",
				TargetTable: "column",
				FKColumn:    "user_id",
			},
		},
	}

	_, err := gen.BuildTableContexts(testInput(schema), nil)
	if err == nil {
		t.Fatal("BuildTableContexts() error = nil, want the relationship field rejected")
	}
	// The context's relationship Name is the pluralized, user-visible form —
	// the same spelling exclude_relationships matches.
	if !strings.Contains(err.Error(), `relationship "columns" resolves to Go field name "Columns"`) {
		t.Errorf("BuildTableContexts() error = %q, want it to name the relationship", err)
	}
	if !strings.Contains(err.Error(), "tables.users.exclude_relationships") {
		t.Errorf("BuildTableContexts() error = %q, want it to name the exclusion escape hatch", err)
	}
}

// TestReservedFieldName_View pins the view half. Views reach <V>Filter and
// <V>FieldOptions through the same two shared templates, and take neither
// column_map nor exclude_columns — so the message points at the view
// definition instead of at config.
func TestReservedFieldName_View(t *testing.T) {
	schema := &parser.Schema{
		Views: []parser.View{
			{
				Name: "event_summaries",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "columns", Type: "text"},
				},
			},
		},
	}

	_, err := gen.BuildViewContexts(testInput(schema), nil)
	if err == nil {
		t.Fatal("BuildViewContexts() error = nil, want the view column rejected")
	}
	if !strings.Contains(err.Error(), "views.event_summaries") {
		t.Errorf("BuildViewContexts() error = %q, want it scoped to the view", err)
	}
	if !strings.Contains(err.Error(), "EventSummaryFieldOptions (method Columns)") {
		t.Errorf("BuildViewContexts() error = %q, want it to name the owning declaration", err)
	}
	if !strings.Contains(err.Error(), "rename the column in the view definition") {
		t.Errorf("BuildViewContexts() error = %q, want it to name the view-side escape hatch", err)
	}
}

// TestBuildTableContexts_ErrorsAccumulate pins the batch report `sqlgen
// validate` depends on: a build that fails on more than one table reports all
// of them, not just the first.
func TestBuildTableContexts_ErrorsAccumulate(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "alphas",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "columns", Type: "text"},
				},
			},
			{
				Name: "betas",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "column_map", Type: "text"},
				},
			},
		},
	}

	_, err := gen.BuildTableContexts(testInput(schema), nil)
	if err == nil {
		t.Fatal("BuildTableContexts() error = nil, want both tables rejected")
	}
	for _, want := range []string{"tables.alphas", "tables.betas"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("BuildTableContexts() error = %q, want it to report %s", err, want)
		}
	}
}
