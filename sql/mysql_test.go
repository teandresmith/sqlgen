package sql_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/sql"
)

// Compile-time interface assertion.
var _ sql.Dialect = sql.MySQLDialect{}

func TestMySQLDialect_Name(t *testing.T) {
	d := sql.NewMySQLDialect()
	if got := d.Name(); got != "mysql" {
		t.Errorf("Name() = %q, want %q", got, "mysql")
	}
}

func TestMySQLDialect_Placeholder(t *testing.T) {
	d := sql.NewMySQLDialect()
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

func TestMySQLDialect_PlaceholderList(t *testing.T) {
	d := sql.NewMySQLDialect()
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

func TestMySQLDialect_QuoteIdentifier(t *testing.T) {
	d := sql.NewMySQLDialect()
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"simple", "users", "`users`"},
		{"reserved word", "order", "`order`"},
		{"snake_case", "created_at", "`created_at`"},
		// An embedded backtick is doubled, or it would close the identifier.
		{"embedded backtick", "say`hi", "`say``hi`"},
		{"already quoted", "`title`", "```title```"},
		{"double quote stays literal", `a"b`, "`a\"b`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.QuoteIdentifier(tt.id); got != tt.want {
				t.Errorf("QuoteIdentifier(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestMySQLDialect_FormatTable(t *testing.T) {
	d := sql.NewMySQLDialect()
	tests := []struct {
		name  string
		table sql.Table
		want  string
	}{
		{"without schema", sql.Table{Name: "users"}, "`users`"},
		{"schema ignored", sql.Table{Schema: "public", Name: "users"}, "`users`"},
		{"embedded backtick", sql.Table{Name: "t`y"}, "`t``y`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.FormatTable(tt.table); got != tt.want {
				t.Errorf("FormatTable(%+v) = %q, want %q", tt.table, got, tt.want)
			}
		})
	}
}

func TestMySQLDialect_SupportsReturning(t *testing.T) {
	d := sql.NewMySQLDialect()
	if got := d.SupportsReturning(); got != false {
		t.Errorf("SupportsReturning() = %v, want false", got)
	}
}

func TestMySQLDialect_BackslashEscapes(t *testing.T) {
	d := sql.NewMySQLDialect()
	if got := d.BackslashEscapes(); got != true {
		t.Errorf("BackslashEscapes() = %v, want true", got)
	}
}

func TestMySQLDialect_ReturningClause(t *testing.T) {
	d := sql.NewMySQLDialect()
	if got := d.ReturningClause([]string{"id", "name"}); got != "" {
		t.Errorf("ReturningClause() = %q, want empty string", got)
	}
}

func TestMySQLDialect_UpsertClause(t *testing.T) {
	d := sql.NewMySQLDialect()
	tests := []struct {
		name          string
		conflictKeys  []string
		updateColumns []string
		resolvePK     string
		want          string
	}{
		{
			"single update column",
			[]string{"id"},
			[]string{"name"},
			"",
			"ON DUPLICATE KEY UPDATE `name` = VALUES(`name`)",
		},
		{
			"multiple update columns",
			[]string{"id"},
			[]string{"name", "email"},
			"",
			"ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `email` = VALUES(`email`)",
		},
		{
			"conflict keys ignored",
			[]string{"order_id", "product_id"},
			[]string{"quantity"},
			"",
			"ON DUPLICATE KEY UPDATE `quantity` = VALUES(`quantity`)",
		},
		{
			// Pure link table: nothing to set. MySQL has no DO NOTHING and an
			// empty ON DUPLICATE KEY UPDATE is a syntax error, so a key column
			// is assigned to itself — valid, and leaves the row untouched.
			"no update columns degrades to a no-op self-assignment",
			[]string{"user_id", "category_id"},
			nil,
			"",
			"ON DUPLICATE KEY UPDATE `user_id` = `user_id`",
		},
		{
			"empty (non-nil) update columns degrades to a no-op self-assignment",
			[]string{"user_id", "category_id"},
			[]string{},
			"",
			"ON DUPLICATE KEY UPDATE `user_id` = `user_id`",
		},
		{
			// An ON DUPLICATE KEY UPDATE that modifies no row reports
			// insert_id = 0, so a caller resolving the PK from LastInsertId()
			// loses the conflicting row. Self-assigning the PK through
			// LAST_INSERT_ID() republishes it without touching the row, and it
			// leads the set list so no later assignment can overwrite it.
			"resolve PK prepends the LAST_INSERT_ID self-assignment",
			[]string{"sku"},
			[]string{"name", "price"},
			"id",
			"ON DUPLICATE KEY UPDATE `id` = LAST_INSERT_ID(`id`), `name` = VALUES(`name`), `price` = VALUES(`price`)",
		},
		{
			// The empty-update-columns form loses the PK the same way, and the
			// PK self-assignment is itself a valid no-op set list — the
			// conflict-key self-assignment is not needed on top of it.
			"resolve PK with no update columns emits only the self-assignment",
			[]string{"workspace_id", "name"},
			nil,
			"id",
			"ON DUPLICATE KEY UPDATE `id` = LAST_INSERT_ID(`id`)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := sql.UpsertClauseOptions{
				ConflictKeys:    tt.conflictKeys,
				UpdateColumns:   tt.updateColumns,
				ResolvePKColumn: tt.resolvePK,
			}
			if got := d.UpsertClause(opts); got != tt.want {
				t.Errorf("UpsertClause() = %q, want %q", got, tt.want)
			}
		})
	}
}
