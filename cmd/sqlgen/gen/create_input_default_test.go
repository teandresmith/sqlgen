package gen_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
)

// TestCreateInput_EmptyStringDefaultIsOmittable builds a MySQL table as both
// schema sources describe a NOT NULL column whose default is the empty
// string: Default holds the quoted empty literal, not the empty string, so
// the column is omittable in Create<T>Input.
func TestCreateInput_EmptyStringDefaultIsOmittable(t *testing.T) {
	schema := &parser.Schema{Tables: []parser.Table{{
		Name: "accounts",
		Columns: []parser.Column{
			{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
			{Name: "nick", Type: "varchar(50)", Default: "''"},
			{Name: "name", Type: "varchar(50)"},
		},
		Constraints: []parser.Constraint{{Name: "PRIMARY", Type: parser.PrimaryKey, Columns: []string{"id"}}},
	}}}
	in := testInput(schema)
	in.Config.Input.Dialect = config.DialectMySQL
	in.Resolver = gotype.NewResolver(config.DialectMySQL, true, in.Config.Overrides.Types)

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("BuildTableContexts() returned %d tables, want 1", len(tables))
	}

	type field struct {
		Column              string
		Omittable, Required bool
	}
	got := make([]field, 0, len(tables[0].CreateInputFields))
	for _, f := range tables[0].CreateInputFields {
		got = append(got, field{Column: f.ColumnName, Omittable: f.Omittable, Required: f.Required})
	}
	want := []field{
		{Column: "nick", Omittable: true},
		{Column: "name", Required: true},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("CreateAccountInput fields mismatch (-want +got):\n%s", diff)
	}
}
