package sql_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/sql"
)

// Compile-time interface assertions.
var _ sql.Dialect = sql.PostgresDialect{}

func TestPostgresDialect_Name(t *testing.T) {
	t.Run("pgx", func(t *testing.T) {
		d := sql.NewPostgresDialect()
		if got := d.Name(); got != "postgres" {
			t.Errorf("Name() = %q, want %q", got, "postgres")
		}
	})

	t.Run("stdlib", func(t *testing.T) {
		d := sql.NewPostgresStdlibDialect()
		if got := d.Name(); got != "postgres" {
			t.Errorf("Name() = %q, want %q", got, "postgres")
		}
	})
}

func TestPostgresDialect_Placeholder(t *testing.T) {
	d := sql.NewPostgresDialect()
	tests := []struct {
		name     string
		position int
		want     string
	}{
		{"first", 1, "$1"},
		{"second", 2, "$2"},
		{"tenth", 10, "$10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.Placeholder(tt.position); got != tt.want {
				t.Errorf("Placeholder(%d) = %q, want %q", tt.position, got, tt.want)
			}
		})
	}
}

func TestPostgresDialect_PlaceholderList(t *testing.T) {
	d := sql.NewPostgresDialect()
	tests := []struct {
		name  string
		start int
		count int
		want  string
	}{
		{"empty", 1, 0, ""},
		{"single", 1, 1, "$1"},
		{"three from start", 1, 3, "$1, $2, $3"},
		{"three from offset", 4, 3, "$4, $5, $6"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.PlaceholderList(tt.start, tt.count); got != tt.want {
				t.Errorf("PlaceholderList(%d, %d) = %q, want %q", tt.start, tt.count, got, tt.want)
			}
		})
	}
}

func TestPostgresDialect_QuoteIdentifier(t *testing.T) {
	d := sql.NewPostgresDialect()
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

func TestPostgresDialect_FormatTable(t *testing.T) {
	d := sql.NewPostgresDialect()
	tests := []struct {
		name  string
		table sql.Table
		want  string
	}{
		{"with schema", sql.Table{Schema: "public", Name: "users"}, `"public"."users"`},
		{"without schema", sql.Table{Name: "users"}, `"users"`},
		{"custom schema", sql.Table{Schema: "billing", Name: "invoices"}, `"billing"."invoices"`},
		{"embedded quote in both parts", sql.Table{Schema: `s"x`, Name: `t"y`}, `"s""x"."t""y"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.FormatTable(tt.table); got != tt.want {
				t.Errorf("FormatTable(%+v) = %q, want %q", tt.table, got, tt.want)
			}
		})
	}
}

func TestPostgresDialect_SupportsReturning(t *testing.T) {
	d := sql.NewPostgresDialect()
	if got := d.SupportsReturning(); got != true {
		t.Errorf("SupportsReturning() = %v, want true", got)
	}
}

func TestPostgresDialect_BackslashEscapes(t *testing.T) {
	d := sql.NewPostgresDialect()
	if got := d.BackslashEscapes(); got != false {
		t.Errorf("BackslashEscapes() = %v, want false", got)
	}
}

func TestPostgresDialect_ReturningClause(t *testing.T) {
	d := sql.NewPostgresDialect()
	tests := []struct {
		name    string
		columns []string
		want    string
	}{
		{"empty", nil, ""},
		{"single column", []string{"id"}, `RETURNING "id"`},
		{"multiple columns", []string{"id", "name", "email"}, `RETURNING "id", "name", "email"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.ReturningClause(tt.columns); got != tt.want {
				t.Errorf("ReturningClause(%v) = %q, want %q", tt.columns, got, tt.want)
			}
		})
	}
}

func TestPostgresDialect_UpsertClause(t *testing.T) {
	d := sql.NewPostgresDialect()
	tests := []struct {
		name          string
		conflictKeys  []string
		updateColumns []string
		want          string
	}{
		{
			"single conflict key",
			[]string{"id"},
			[]string{"name", "email"},
			`ON CONFLICT ("id") DO UPDATE SET "name" = excluded."name", "email" = excluded."email"`,
		},
		{
			"composite conflict keys",
			[]string{"order_id", "product_id"},
			[]string{"quantity"},
			`ON CONFLICT ("order_id", "product_id") DO UPDATE SET "quantity" = excluded."quantity"`,
		},
		{
			// Pure link table: every inserted column is part of the key, so
			// there is nothing to set. An empty DO UPDATE SET is a syntax
			// error; DO NOTHING makes the upsert an idempotent no-op.
			"no update columns degrades to DO NOTHING",
			[]string{"user_id", "category_id"},
			nil,
			`ON CONFLICT ("user_id", "category_id") DO NOTHING`,
		},
		{
			"empty (non-nil) update columns degrades to DO NOTHING",
			[]string{"id"},
			[]string{},
			`ON CONFLICT ("id") DO NOTHING`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := sql.UpsertClauseOptions{
				ConflictKeys:  tt.conflictKeys,
				UpdateColumns: tt.updateColumns,
			}
			if got := d.UpsertClause(opts); got != tt.want {
				t.Errorf("UpsertClause() = %q, want %q", got, tt.want)
			}
			// PostgreSQL cannot preserve a PK through the clause, so
			// ResolvePKColumn must not change the SQL — the generated caller
			// resolves the PK with a follow-up SELECT instead.
			opts.ResolvePKColumn = "id"
			if got := d.UpsertClause(opts); got != tt.want {
				t.Errorf("UpsertClause() with ResolvePKColumn = %q, want %q (unchanged)", got, tt.want)
			}
		})
	}
}

func TestPostgresDialect_SupportsArrayParams(t *testing.T) {
	t.Run("pgx supports array params", func(t *testing.T) {
		d := sql.NewPostgresDialect()
		if got := d.SupportsArrayParams(); got != true {
			t.Errorf("SupportsArrayParams() = %v, want true", got)
		}
	})

	t.Run("stdlib does not support array params", func(t *testing.T) {
		d := sql.NewPostgresStdlibDialect()
		if got := d.SupportsArrayParams(); got != false {
			t.Errorf("SupportsArrayParams() = %v, want false", got)
		}
	})
}
