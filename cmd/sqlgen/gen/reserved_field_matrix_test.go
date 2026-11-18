package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// rfmCase is one reserved name on one carrier. sqlName is the SQL spelling of
// the column, relationship or view column; override reaches the reserved name
// through config instead (a column_map rename for a column, a declared
// relationship name for a relationship), so the rule is exercised both ways.
type rfmCase struct {
	name string
	row  string
	item string

	shape    string // column carriers: filterable, composite, sole-pk, increment; relationship carriers: composite marks a composite-PK parent
	sqlName  string
	override bool
	reserved string
	owner    string
}

// rfmBuild builds the contexts for one case and returns the build error.
// Tables and views are built the way generation and `sqlgen validate` build
// them, so a rejection here is a rejection of the config.
func rfmBuild(t *testing.T, tt rfmCase) error {
	t.Helper()
	switch tt.item {
	case "column":
		return rfmBuildColumn(t, tt)
	case "relationship", "o2m", "m2m":
		return rfmBuildRelationship(t, tt)
	case "view-column":
		return rfmBuildViewColumn(t, tt)
	}
	t.Fatalf("unknown item %q", tt.item)
	return nil
}

func rfmBuildColumn(t *testing.T, tt rfmCase) error {
	t.Helper()
	var cols []parser.Column
	switch tt.shape {
	case "sole-pk":
		cols = []parser.Column{
			{Name: tt.sqlName, Type: "uuid", PrimaryKey: true},
			{Name: "label", Type: "text"},
		}
	case "composite":
		cols = []parser.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "seq", Type: "integer", PrimaryKey: true},
			{Name: tt.sqlName, Type: "text"},
		}
	case "increment":
		cols = []parser.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: tt.sqlName, Type: "integer"},
		}
	case "filterable":
		cols = []parser.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: tt.sqlName, Type: "text"},
		}
	default:
		t.Fatalf("unknown column shape %q", tt.shape)
	}
	input := testInput(&parser.Schema{Tables: []parser.Table{{Name: "users", Columns: cols}}})
	if tt.override {
		input.Config.Tables["users"] = config.TableConfig{
			ColumnMap: map[string]config.ColumnOverride{tt.sqlName: {Name: tt.reserved}},
		}
	}
	_, err := gen.BuildTableContexts(input, nil)
	return err
}

// rfmBuildRelationship puts one relationship on users. A derived edge is a
// parser relationship named sqlName; an overridden one is declared in config
// under the reserved name.
func rfmBuildRelationship(t *testing.T, tt rfmCase) error {
	t.Helper()
	userCols := []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "profile_id", Type: "uuid"}}
	if tt.shape == "composite" {
		// The edges keep a single-column key into users.id: an edge keyed
		// by more than one column is not generated at all yet.
		userCols = append(userCols, parser.Column{Name: "seq", Type: "integer", PrimaryKey: true})
	}
	localKey := []parser.KeyPair{{Local: "user_id", Referenced: "id"}}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Columns: userCols},
			{Name: "profiles", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{
				Name: "badges",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid", Unique: true},
				},
			},
			{
				Name: "posts",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid"},
				},
			},
			{Name: "groups", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{
				Name: "user_groups",
				Columns: []parser.Column{
					{Name: "user_id", Type: "uuid", PrimaryKey: true},
					{Name: "group_id", Type: "uuid", PrimaryKey: true},
				},
			},
		},
	}
	input := testInput(schema)

	if tt.override {
		rel := config.TableRelationship{Name: tt.reserved}
		switch tt.item {
		case "relationship":
			rel.Type, rel.Table, rel.FK = "one_to_one", "badges", config.KeyColumns{"user_id"}
		case "o2m":
			rel.Type, rel.Table, rel.FK = "one_to_many", "posts", config.KeyColumns{"user_id"}
		case "m2m":
			rel.Type, rel.Table, rel.Junction = "many_to_many", "groups", "user_groups"
			rel.JunctionLocalFK, rel.JunctionReferenceFK = config.KeyColumns{"user_id"}, config.KeyColumns{"group_id"}
		}
		input.Config.Tables["users"] = config.TableConfig{Relationships: []config.TableRelationship{rel}}
	} else {
		rel := parser.Relationship{Name: tt.sqlName, SourceTable: "users"}
		switch tt.item {
		case "relationship":
			rel.Type, rel.TargetTable = parser.OneToOne, "profiles"
			rel.Key = []parser.KeyPair{{Local: "profile_id", Referenced: "id"}}
		case "o2m":
			rel.Type, rel.TargetTable, rel.Key = parser.OneToMany, "posts", localKey
		case "m2m":
			rel.Type, rel.TargetTable, rel.JunctionTable = parser.ManyToMany, "groups", "user_groups"
			rel.JunctionLocal = localKey
			rel.JunctionReference = []parser.KeyPair{{Local: "group_id", Referenced: "id"}}
		}
		schema.Relationships = []parser.Relationship{rel}
	}
	_, err := gen.BuildTableContexts(input, nil)
	return err
}

// rfmBuildViewColumn builds a view carrying a column named sqlName. Views take
// no column_map, so the SQL name is the only way in; composite gives the view
// a two-column key (the @pk annotation).
func rfmBuildViewColumn(t *testing.T, tt rfmCase) error {
	t.Helper()
	cols := []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}
	if tt.shape == "composite" {
		cols = append(cols, parser.Column{Name: "seq", Type: "integer", PrimaryKey: true})
	}
	cols = append(cols, parser.Column{Name: tt.sqlName, Type: "text"})
	input := testInput(&parser.Schema{Views: []parser.View{{Name: "user_summaries", Columns: cols}}})
	_, err := gen.BuildViewContexts(input, nil)
	return err
}

// TestReservedFieldNamesMatrix walks the reserved-field table of PRD §8.5 one
// carrier at a time: a column, relationship or view column that resolves to a
// name the generated code already declares on a struct the field lands on is
// rejected with a message naming the reserved name and its generated owner.
// The rule is not override-gated, so each row is reached through the SQL name
// on some carriers and through config on others.
func TestReservedFieldNamesMatrix(t *testing.T) {
	tests := []rfmCase{
		// Columns: <T>FieldOptions.Columns() — every column, every relationship.
		{
			name: "a column whose SQL name resolves to Columns is rejected naming the field options",
			row:  "reserved-field-columns", item: "column",
			shape: "filterable", sqlName: "columns", reserved: "Columns", owner: "UserFieldOptions",
		},
		{
			name: "a to-one relationship derived as Columns is rejected naming the field options",
			row:  "reserved-field-columns", item: "relationship",
			sqlName: "columns", reserved: "Columns", owner: "UserFieldOptions",
		},
		{
			name: "a one-to-many relationship derived as Columns is rejected naming the field options",
			row:  "reserved-field-columns", item: "o2m",
			sqlName: "columns", reserved: "Columns", owner: "UserFieldOptions",
		},
		{
			name: "a many-to-many relationship declared as Columns is rejected naming the field options",
			row:  "reserved-field-columns", item: "m2m",
			override: true, reserved: "Columns", owner: "UserFieldOptions",
		},
		{
			name: "a view column named columns is rejected naming the view field options",
			row:  "reserved-field-columns", item: "view-column",
			sqlName: "columns", reserved: "Columns", owner: "UserSummaryFieldOptions",
		},

		// ColumnMap: <T>FieldOptions.ColumnMap().
		{
			name: "a column renamed to ColumnMap is rejected naming the field options",
			row:  "reserved-field-columnmap", item: "column",
			shape: "filterable", sqlName: "label", override: true, reserved: "ColumnMap", owner: "UserFieldOptions",
		},
		{
			name: "a to-one relationship declared as ColumnMap is rejected naming the field options",
			row:  "reserved-field-columnmap", item: "relationship",
			override: true, reserved: "ColumnMap", owner: "UserFieldOptions",
		},
		{
			name: "a one-to-many relationship declared as ColumnMap is rejected naming the field options",
			row:  "reserved-field-columnmap", item: "o2m",
			override: true, reserved: "ColumnMap", owner: "UserFieldOptions",
		},
		{
			name: "a many-to-many relationship declared as ColumnMap is rejected naming the field options",
			row:  "reserved-field-columnmap", item: "m2m",
			override: true, reserved: "ColumnMap", owner: "UserFieldOptions",
		},
		{
			name: "a view column named column_map is rejected naming the view field options",
			row:  "reserved-field-columnmap", item: "view-column",
			sqlName: "column_map", reserved: "ColumnMap", owner: "UserSummaryFieldOptions",
		},

		// HasSelectedColumns: <T>FieldOptions.HasSelectedColumns().
		{
			name: "a column whose SQL name resolves to HasSelectedColumns is rejected naming the field options",
			row:  "reserved-field-hasselectedcolumns", item: "column",
			shape: "filterable", sqlName: "has_selected_columns", reserved: "HasSelectedColumns", owner: "UserFieldOptions",
		},
		{
			name: "a to-one relationship derived as HasSelectedColumns is rejected naming the field options",
			row:  "reserved-field-hasselectedcolumns", item: "relationship",
			sqlName: "has_selected_columns", reserved: "HasSelectedColumns", owner: "UserFieldOptions",
		},
		{
			name: "a one-to-many relationship declared as HasSelectedColumns is rejected naming the field options",
			row:  "reserved-field-hasselectedcolumns", item: "o2m",
			override: true, reserved: "HasSelectedColumns", owner: "UserFieldOptions",
		},
		{
			name: "a many-to-many relationship derived as HasSelectedColumns is rejected naming the field options",
			row:  "reserved-field-hasselectedcolumns", item: "m2m",
			sqlName: "has_selected_columns", reserved: "HasSelectedColumns", owner: "UserFieldOptions",
		},
		{
			name: "a view column named has_selected_columns is rejected naming the view field options",
			row:  "reserved-field-hasselectedcolumns", item: "view-column",
			sqlName: "has_selected_columns", reserved: "HasSelectedColumns", owner: "UserSummaryFieldOptions",
		},

		// And: <T>Filter.And — filterable columns, list relationships.
		{
			name: "a filterable column renamed to And is rejected naming the filter",
			row:  "reserved-field-and", item: "column",
			shape: "filterable", sqlName: "label", override: true, reserved: "And", owner: "UserFilter",
		},
		{
			name: "a one-to-many relationship declared as And is rejected naming the filter",
			row:  "reserved-field-and", item: "o2m",
			override: true, reserved: "And", owner: "UserFilter",
		},
		{
			name: "a many-to-many relationship declared as And is rejected naming the filter",
			row:  "reserved-field-and", item: "m2m",
			override: true, reserved: "And", owner: "UserFilter",
		},
		{
			name: "a filterable view column named and is rejected naming the view filter",
			row:  "reserved-field-and", item: "view-column",
			sqlName: "and", reserved: "And", owner: "UserSummaryFilter",
		},

		// Or: <T>Filter.Or.
		{
			name: "a filterable column whose SQL name resolves to Or is rejected naming the filter",
			row:  "reserved-field-or", item: "column",
			shape: "filterable", sqlName: "or", reserved: "Or", owner: "UserFilter",
		},
		{
			name: "a one-to-many relationship declared as Or is rejected naming the filter",
			row:  "reserved-field-or", item: "o2m",
			override: true, reserved: "Or", owner: "UserFilter",
		},
		{
			name: "a many-to-many relationship declared as Or is rejected naming the filter",
			row:  "reserved-field-or", item: "m2m",
			override: true, reserved: "Or", owner: "UserFilter",
		},
		{
			name: "a filterable view column named or is rejected naming the view filter",
			row:  "reserved-field-or", item: "view-column",
			sqlName: "or", reserved: "Or", owner: "UserSummaryFilter",
		},

		// ToConditions: <T>Filter.ToConditions().
		{
			name: "a filterable column renamed to ToConditions is rejected naming the filter",
			row:  "reserved-field-toconditions", item: "column",
			shape: "filterable", sqlName: "label", override: true, reserved: "ToConditions", owner: "UserFilter",
		},
		{
			name: "a one-to-many relationship derived as ToConditions is rejected naming the filter",
			row:  "reserved-field-toconditions", item: "o2m",
			sqlName: "to_conditions", reserved: "ToConditions", owner: "UserFilter",
		},
		{
			name: "a many-to-many relationship derived as ToConditions is rejected naming the filter",
			row:  "reserved-field-toconditions", item: "m2m",
			sqlName: "to_conditions", reserved: "ToConditions", owner: "UserFilter",
		},
		{
			name: "a filterable view column named to_conditions is rejected naming the view filter",
			row:  "reserved-field-toconditions", item: "view-column",
			sqlName: "to_conditions", reserved: "ToConditions", owner: "UserSummaryFilter",
		},

		// PKs: <T>Filter.PKs — only on a composite-PK entity.
		{
			name: "a filterable column whose SQL name resolves to PKs on a composite-PK table is rejected naming the filter",
			row:  "reserved-field-pks", item: "column",
			shape: "composite", sqlName: "p_ks", reserved: "PKs", owner: "UserFilter",
		},
		{
			name: "a one-to-many relationship derived as PKs on a composite-PK parent is rejected naming the filter",
			row:  "reserved-field-pks", item: "o2m",
			shape: "composite", sqlName: "p_ks", reserved: "PKs", owner: "UserFilter",
		},
		{
			name: "a many-to-many relationship derived as PKs on a composite-PK parent is rejected naming the filter",
			row:  "reserved-field-pks", item: "m2m",
			shape: "composite", sqlName: "p_ks", reserved: "PKs", owner: "UserFilter",
		},
		{
			name: "a filterable view column named p_ks on a view with a composite key is rejected naming the view filter",
			row:  "reserved-field-pks", item: "view-column",
			shape: "composite", sqlName: "p_ks", reserved: "PKs", owner: "UserSummaryFilter",
		},

		// Input: Update<T>Item.Input — the sole PK column of a single-PK table.
		{
			name: "the sole PK column whose SQL name resolves to Input is rejected naming the update item",
			row:  "reserved-field-input", item: "column",
			shape: "sole-pk", sqlName: "input", reserved: "Input", owner: "UpdateUserItem",
		},

		// Column: the <T>IncrementColumn type — increment-eligible columns.
		{
			name: "an increment-eligible column renamed to Column is rejected naming the increment enum type",
			row:  "reserved-field-column", item: "column",
			shape: "increment", sqlName: "retry_count", override: true, reserved: "Column", owner: "UserIncrementColumn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rfmBuild(t, tt)
			if err == nil {
				t.Fatalf("build error = nil, want %q rejected as claimed by %s", tt.reserved, tt.owner)
			}
			if !strings.Contains(err.Error(), tt.reserved) {
				t.Errorf("build error = %q, want it to name the reserved name %q", err, tt.reserved)
			}
			if !strings.Contains(err.Error(), tt.owner) {
				t.Errorf("build error = %q, want it to name the owner %s", err, tt.owner)
			}
		})
	}
}
