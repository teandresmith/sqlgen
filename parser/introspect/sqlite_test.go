package introspect

import (
	"errors"
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/parser"
)

func TestQuoteSQLiteIdent(t *testing.T) {
	tests := []struct {
		name  string
		ident string
		want  string
	}{
		{name: "plain", ident: "users", want: `"users"`},
		{name: "embedded quote", ident: `Quoted"Name`, want: `"Quoted""Name"`},
		{name: "backslash", ident: `back\slash`, want: `"back\slash"`},
		{name: "space and mixed case", ident: "My Table", want: `"My Table"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quoteSQLiteIdent(tt.ident); got != tt.want {
				t.Errorf("quoteSQLiteIdent(%q) = %s, want %s", tt.ident, got, tt.want)
			}
		})
	}
}

func TestSQLiteGeneratedStorage(t *testing.T) {
	tests := []struct {
		name        string
		hidden      int
		wantStorage string
		wantOK      bool
	}{
		{name: "ordinary column", hidden: 0, wantOK: true},
		{name: "virtual table hidden column", hidden: 1},
		{name: "virtual generated column", hidden: 2, wantStorage: "VIRTUAL", wantOK: true},
		{name: "stored generated column", hidden: 3, wantStorage: "STORED", wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage, ok := sqliteGeneratedStorage(tt.hidden)
			if storage != tt.wantStorage || ok != tt.wantOK {
				t.Errorf("sqliteGeneratedStorage(%d) = (%q, %t), want (%q, %t)", tt.hidden, storage, ok, tt.wantStorage, tt.wantOK)
			}
		})
	}
}

// A stored CREATE TABLE the file parser cannot read is a schema error, not a
// failure to read the catalog.
func TestParseStoredTable_SchemaError(t *testing.T) {
	_, err := parseStoredTable("CREATE TABLE t (id INTEGER PRIMARY KEY ASC AUTOINCREMENT)")
	if _, ok := errors.AsType[*SchemaError](err); !ok {
		t.Errorf("parseStoredTable() error = %v, want a *SchemaError", err)
	}
}

// A foreign key takes the name declared for the key with its parent columns,
// whatever order the catalog lists keys over the same columns in.
func TestNameDeclaredConstraints_ForeignKeyParentColumns(t *testing.T) {
	declared := &parser.Table{Constraints: []parser.Constraint{
		{Name: "fa", Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"x"}},
		{Name: "fb", Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"y"}},
	}}
	table := &parser.Table{Constraints: []parser.Constraint{
		{Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"Y"}},
		{Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"X"}},
	}}
	if err := nameDeclaredConstraints(table, declared); err != nil {
		t.Fatalf("nameDeclaredConstraints() unexpected error: %v", err)
	}
	got := []string{table.Constraints[0].Name, table.Constraints[1].Name}
	if want := []string{"fb", "fa"}; !slices.Equal(got, want) {
		t.Errorf("nameDeclaredConstraints() names for keys to p(Y), p(X) = %q, want %q", got, want)
	}
}
