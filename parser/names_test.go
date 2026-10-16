package parser_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
)

func TestSameName(t *testing.T) {
	tests := []struct {
		dialect    string
		a, b       string
		wantTable  bool
		wantColumn bool
	}{
		{dialect: "postgres", a: "users", b: "users", wantTable: true, wantColumn: true},
		{dialect: "postgres", a: "Users", b: "users", wantTable: false, wantColumn: false},
		{dialect: "mysql", a: "Users", b: "users", wantTable: false, wantColumn: true},
		{dialect: "mysql", a: "É", b: "é", wantTable: false, wantColumn: true},
		{dialect: "mysql", a: "É", b: "e", wantTable: false, wantColumn: false},
		{dialect: "sqlite", a: "Users", b: "users", wantTable: true, wantColumn: true},
		{dialect: "sqlite", a: "É", b: "é", wantTable: false, wantColumn: false},
		{dialect: "sqlite", a: "Äb", b: "ÄB", wantTable: true, wantColumn: true},
	}
	for _, tt := range tests {
		if got := parser.SameTableName(tt.dialect, tt.a, tt.b); got != tt.wantTable {
			t.Errorf("SameTableName(%s, %q, %q) = %v, want %v", tt.dialect, tt.a, tt.b, got, tt.wantTable)
		}
		if got := parser.SameColumnName(tt.dialect, tt.a, tt.b); got != tt.wantColumn {
			t.Errorf("SameColumnName(%s, %q, %q) = %v, want %v", tt.dialect, tt.a, tt.b, got, tt.wantColumn)
		}
	}
}

func TestResolveReferenceNames(t *testing.T) {
	parent := func(name string, cols ...string) parser.Table {
		t := parser.Table{Name: name}
		for _, c := range cols {
			t.Columns = append(t.Columns, parser.Column{Name: c})
		}
		return t
	}
	child := func(refTable string, refCols ...string) parser.Table {
		return parser.Table{
			Name:    "child",
			Columns: []parser.Column{{Name: "x", FKReference: &parser.FKReference{Table: refTable, Column: refCols[0]}}},
			Constraints: []parser.Constraint{{
				Type: parser.ForeignKey, Columns: []string{"x"}, ReferenceTable: refTable, ReferenceColumns: refCols,
			}},
		}
	}
	tests := []struct {
		name      string
		dialect   string
		tables    []parser.Table
		wantTable string
		wantCols  []string
	}{
		{
			name:      "SQLite takes the parent's spelling of table and column",
			dialect:   "sqlite",
			tables:    []parser.Table{parent("Users", "ID"), child("users", "id")},
			wantTable: "Users",
			wantCols:  []string{"ID"},
		},
		{
			name:      "SQLite folds ASCII case only",
			dialect:   "sqlite",
			tables:    []parser.Table{parent("p", "É"), child("p", "é")},
			wantTable: "p",
			wantCols:  []string{"é"},
		},
		{
			name:      "MySQL takes the parent's column spelling, Unicode case included",
			dialect:   "mysql",
			tables:    []parser.Table{parent("rp", "b", "é"), child("rp", "B")},
			wantTable: "rp",
			wantCols:  []string{"b"},
		},
		{
			name:      "MySQL table names compare exactly",
			dialect:   "mysql",
			tables:    []parser.Table{parent("RP", "b"), child("rp", "B")},
			wantTable: "rp",
			wantCols:  []string{"B"},
		},
		{
			name:      "PostgreSQL compares exactly",
			dialect:   "postgres",
			tables:    []parser.Table{parent("Users", "ID"), child("Users", "id")},
			wantTable: "Users",
			wantCols:  []string{"id"},
		},
		{
			name:      "an exact match wins",
			dialect:   "sqlite",
			tables:    []parser.Table{parent("p", "a"), child("p", "a")},
			wantTable: "p",
			wantCols:  []string{"a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &parser.Schema{Tables: tt.tables}
			parser.ResolveReferenceNames(schema, tt.dialect)
			c := schema.Tables[1]
			ref := c.Columns[0].FKReference
			if ref.Table != tt.wantTable || ref.Column != tt.wantCols[0] {
				t.Errorf("FKReference = %s(%s), want %s(%s)", ref.Table, ref.Column, tt.wantTable, tt.wantCols[0])
			}
			fk := c.Constraints[0]
			if fk.ReferenceTable != tt.wantTable {
				t.Errorf("ReferenceTable = %q, want %q", fk.ReferenceTable, tt.wantTable)
			}
			if diff := cmp.Diff(tt.wantCols, fk.ReferenceColumns); diff != "" {
				t.Errorf("ReferenceColumns mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRenameColumn(t *testing.T) {
	tests := []struct {
		name     string
		dialect  string
		cols     []string
		old, new string
		want     []string
		wantErr  string
	}{
		{name: "SQLite matches the old name ignoring ASCII case", dialect: "sqlite", cols: []string{"A", "x"}, old: "a", new: "c", want: []string{"c", "x"}},
		{name: "SQLite rejects a name another column has in another case", dialect: "sqlite", cols: []string{"a", "B"}, old: "a", new: "b", wantErr: "column b already exists in table t"},
		{name: "SQLite renames a column to its own name in another case", dialect: "sqlite", cols: []string{"a", "B"}, old: "a", new: "A", want: []string{"A", "B"}},
		{name: "SQLite keeps é and É apart", dialect: "sqlite", cols: []string{"é", "x"}, old: "É", new: "c", wantErr: "column É not found in table t"},
		{name: "MySQL matches the old name ignoring case", dialect: "mysql", cols: []string{"É", "x"}, old: "é", new: "q", want: []string{"q", "x"}},
		{name: "MySQL rejects a name another column has", dialect: "mysql", cols: []string{"a", "b"}, old: "a", new: "B", wantErr: "column B already exists in table t"},
		{name: "PostgreSQL compares exactly", dialect: "postgres", cols: []string{"A", "x"}, old: "a", new: "c", wantErr: "column a not found in table t"},
		{name: "PostgreSQL rejects a name another column has", dialect: "postgres", cols: []string{"a", "b"}, old: "a", new: "b", wantErr: "column b already exists in table t"},
		{name: "PostgreSQL rejects the column's own name", dialect: "postgres", cols: []string{"a", "b"}, old: "a", new: "a", wantErr: "column a already exists in table t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := &parser.Table{Name: "t"}
			for _, c := range tt.cols {
				table.Columns = append(table.Columns, parser.Column{Name: c})
			}
			err := parser.RenameColumn(table, tt.dialect, tt.old, tt.new)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("RenameColumn(%s, %q, %q) error = %v, want %q", tt.dialect, tt.old, tt.new, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("RenameColumn(%s, %q, %q) error: %v", tt.dialect, tt.old, tt.new, err)
			}
			var got []string
			for _, c := range table.Columns {
				got = append(got, c.Name)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("RenameColumn(%s, %q, %q) columns mismatch (-want +got):\n%s", tt.dialect, tt.old, tt.new, diff)
			}
		})
	}
}

// TestReferencesFollowRenamesInAnotherCase checks that a foreign key written
// in another case than its target follows a table or column rename, as it
// does in the database.
func TestReferencesFollowRenamesInAnotherCase(t *testing.T) {
	newTables := func(refTable, refCol string) []parser.Table {
		return []parser.Table{
			{Name: "Users", Columns: []parser.Column{{Name: "b"}}},
			{
				Name:    "posts",
				Columns: []parser.Column{{Name: "x", FKReference: &parser.FKReference{Table: refTable, Column: refCol}}},
				Constraints: []parser.Constraint{{
					Type: parser.ForeignKey, Columns: []string{"x"}, ReferenceTable: refTable, ReferenceColumns: []string{refCol},
				}},
			},
		}
	}
	tests := []struct {
		name               string
		dialect            string
		refTable, refCol   string
		apply              func([]parser.Table, string)
		wantTable, wantCol string
	}{
		{
			name: "SQLite table rename", dialect: "sqlite", refTable: "users", refCol: "b",
			apply:     func(ts []parser.Table, d string) { parser.RenameTableReferences(ts, d, "", "Users", "Accounts") },
			wantTable: "Accounts", wantCol: "b",
		},
		{
			name: "SQLite column rename", dialect: "sqlite", refTable: "users", refCol: "B",
			apply:     func(ts []parser.Table, d string) { parser.RenameColumnReferences(ts, d, "", "Users", "b", "c") },
			wantTable: "users", wantCol: "c",
		},
		{
			name: "MySQL column rename", dialect: "mysql", refTable: "Users", refCol: "B",
			apply:     func(ts []parser.Table, d string) { parser.RenameColumnReferences(ts, d, "", "Users", "b", "z") },
			wantTable: "Users", wantCol: "z",
		},
		{
			name: "MySQL table names compare exactly", dialect: "mysql", refTable: "users", refCol: "b",
			apply:     func(ts []parser.Table, d string) { parser.RenameTableReferences(ts, d, "", "Users", "Accounts") },
			wantTable: "users", wantCol: "b",
		},
		{
			name: "PostgreSQL column names compare exactly", dialect: "postgres", refTable: "Users", refCol: "B",
			apply:     func(ts []parser.Table, d string) { parser.RenameColumnReferences(ts, d, "", "Users", "b", "z") },
			wantTable: "Users", wantCol: "B",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tables := newTables(tt.refTable, tt.refCol)
			tt.apply(tables, tt.dialect)
			ref := tables[1].Columns[0].FKReference
			if ref.Table != tt.wantTable || ref.Column != tt.wantCol {
				t.Errorf("FKReference = %s(%s), want %s(%s)", ref.Table, ref.Column, tt.wantTable, tt.wantCol)
			}
			fk := tables[1].Constraints[0]
			if fk.ReferenceTable != tt.wantTable || fk.ReferenceColumns[0] != tt.wantCol {
				t.Errorf("FOREIGN KEY references %s(%s), want %s(%s)", fk.ReferenceTable, fk.ReferenceColumns[0], tt.wantTable, tt.wantCol)
			}
		})
	}
}
