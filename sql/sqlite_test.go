package sql_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/sql"
)

// Compile-time interface assertion.
var _ sql.Dialect = sql.SQLiteDialect{}

func TestSQLiteDialect_Name(t *testing.T) {
	d := sql.NewSQLiteDialect()
	if got := d.Name(); got != "sqlite" {
		t.Errorf("Name() = %q, want %q", got, "sqlite")
	}
}

func TestSQLiteDialect_Placeholder(t *testing.T) {
	d := sql.NewSQLiteDialect()
	tests := []struct {
		name     string
		position int
		want     string
	}{
		{"first", 1, "?"},
		{"second", 2, "?"},
		{"tenth", 10, "?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.Placeholder(tt.position); got != tt.want {
				t.Errorf("Placeholder(%d) = %q, want %q", tt.position, got, tt.want)
			}
		})
	}
}

func TestSQLiteDialect_PlaceholderList(t *testing.T) {
	d := sql.NewSQLiteDialect()
	tests := []struct {
		name  string
		start int
		count int
		want  string
	}{
		{"empty", 1, 0, ""},
		{"single", 1, 1, "?"},
		{"three", 1, 3, "?, ?, ?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.PlaceholderList(tt.start, tt.count); got != tt.want {
				t.Errorf("PlaceholderList(%d, %d) = %q, want %q", tt.start, tt.count, got, tt.want)
			}
		})
	}
}

func TestSQLiteDialect_QuoteIdentifier(t *testing.T) {
	d := sql.NewSQLiteDialect()
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"simple", "users", `"users"`},
		{"reserved word", "order", `"order"`},
		{"snake_case", "created_at", `"created_at"`},
		// An embedded quote is doubled, or it would close the identifier.
		{"embedded double quote", `say"hi`, `"say""hi"`},
		{"already quoted", `"title"`, `"""title"""`},
		{"backtick stays literal", "a`b", `"a` + "`" + `b"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.QuoteIdentifier(tt.id); got != tt.want {
				t.Errorf("QuoteIdentifier(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestSQLiteDialect_FormatTable(t *testing.T) {
	d := sql.NewSQLiteDialect()
	tests := []struct {
		name  string
		table sql.Table
		want  string
	}{
		{"without schema", sql.Table{Name: "users"}, `"users"`},
		{"schema ignored", sql.Table{Schema: "public", Name: "users"}, `"users"`},
		{"embedded quote", sql.Table{Name: `t"y`}, `"t""y"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.FormatTable(tt.table); got != tt.want {
				t.Errorf("FormatTable(%+v) = %q, want %q", tt.table, got, tt.want)
			}
		})
	}
}

func TestSQLiteDialect_SupportsReturning(t *testing.T) {
	d := sql.NewSQLiteDialect()
	if got := d.SupportsReturning(); got != true {
		t.Errorf("SupportsReturning() = %v, want true", got)
	}
}

func TestSQLiteDialect_BackslashEscapes(t *testing.T) {
	d := sql.NewSQLiteDialect()
	if got := d.BackslashEscapes(); got != false {
		t.Errorf("BackslashEscapes() = %v, want false", got)
	}
}

func TestSQLiteDialect_ReturningClause(t *testing.T) {
	d := sql.NewSQLiteDialect()
	tests := []struct {
		name    string
		columns []string
		want    string
	}{
		{"empty", nil, ""},
		{"single column", []string{"id"}, `RETURNING "id"`},
		{"multiple columns", []string{"id", "name"}, `RETURNING "id", "name"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.ReturningClause(tt.columns); got != tt.want {
				t.Errorf("ReturningClause(%v) = %q, want %q", tt.columns, got, tt.want)
			}
		})
	}
}

func TestSQLiteDialect_UpsertClause(t *testing.T) {
	d := sql.NewSQLiteDialect()
	got := d.UpsertClause(sql.UpsertClauseOptions{
		ConflictKeys:  []string{"id"},
		UpdateColumns: []string{"name", "email"},
	})
	want := `ON CONFLICT ("id") DO UPDATE SET "name" = excluded."name", "email" = excluded."email"`
	if got != want {
		t.Errorf("UpsertClause() = %q, want %q", got, want)
	}
}

// A pure link table's columns are exactly its composite key, leaving nothing
// to set. An empty DO UPDATE SET is a syntax error; DO NOTHING makes the
// upsert an idempotent no-op.
func TestSQLiteDialect_UpsertClauseNoUpdateColumns(t *testing.T) {
	d := sql.NewSQLiteDialect()
	got := d.UpsertClause(sql.UpsertClauseOptions{
		ConflictKeys: []string{"user_id", "category_id"},
	})
	want := `ON CONFLICT ("user_id", "category_id") DO NOTHING`
	if got != want {
		t.Errorf("UpsertClause() = %q, want %q", got, want)
	}
}

// TestSQLiteDialect_UpsertClauseResolvePKIgnored: SQLite cannot preserve a PK
// through the clause, so ResolvePKColumn must not change the SQL — the
// generated caller resolves the PK with a follow-up SELECT instead.
func TestSQLiteDialect_UpsertClauseResolvePKIgnored(t *testing.T) {
	d := sql.NewSQLiteDialect()
	got := d.UpsertClause(sql.UpsertClauseOptions{
		ConflictKeys:    []string{"workspace_id", "name"},
		ResolvePKColumn: "id",
	})
	want := `ON CONFLICT ("workspace_id", "name") DO NOTHING`
	if got != want {
		t.Errorf("UpsertClause() = %q, want %q", got, want)
	}
}
