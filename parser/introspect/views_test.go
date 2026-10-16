package introspect

import (
	"cmp"
	"testing"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// TestViewFallbackWarning checks that the warning for a view whose columns
// come from the catalog gives why its definition cannot be read, without the
// advice the view parser gives a view file's author and without naming the
// view twice.
func TestViewFallbackWarning(t *testing.T) {
	pg := func(sql []byte) (parser.ViewSQL, error) { return postgres.ParseViewSQL(sql, "public") }
	tests := []struct {
		name  string
		parse viewSQLParser
		sql   string
		view  string
		want  string
	}{
		{name: "sqlite star", parse: sqlite.ParseViewSQL, sql: "CREATE VIEW v AS SELECT * FROM t", want: "it selects *, whose columns the view parser cannot list"},
		{name: "sqlite table star", parse: sqlite.ParseViewSQL, sql: "CREATE VIEW v AS SELECT t.* FROM t", want: `it selects "t".*, whose columns the view parser cannot list`},
		{name: "sqlite unaliased expression", parse: sqlite.ParseViewSQL, sql: "CREATE VIEW v AS SELECT id, a || 'x' FROM t", want: `it selects "a" || 'x' with no alias and no column name`},
		{name: "sqlite right join", parse: sqlite.ParseViewSQL, sql: "CREATE VIEW v AS SELECT t.id FROM t RIGHT JOIN u ON u.t_id = t.id", want: "CREATE VIEW uses a RIGHT or FULL join"},
		{name: "sqlite compound", parse: sqlite.ParseViewSQL, sql: "CREATE VIEW v AS SELECT id FROM t UNION SELECT id FROM u", want: "CREATE VIEW query is not a simple SELECT"},
		{name: "postgres star", parse: pg, sql: `CREATE VIEW "public"."v" AS SELECT * FROM t`, view: "public.v", want: "it selects *, whose columns the view parser cannot list"},
		{name: "postgres unaliased expression", parse: pg, sql: `CREATE VIEW "public"."v" AS SELECT id, a || 'x' FROM t`, view: "public.v", want: "it selects a || 'x' with no alias and no column name"},
		{name: "mysql star", parse: mysql.ParseViewSQL, sql: "CREATE VIEW v AS SELECT * FROM t", want: "it selects *, whose columns the view parser cannot list"},
		{name: "mysql unaliased expression", parse: mysql.ParseViewSQL, sql: "CREATE VIEW v AS SELECT id, a + 1 FROM t", want: "it selects a + 1 with no alias and no column name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.parse([]byte(tt.sql))
			if err == nil {
				t.Fatalf("ParseViewSQL(%q) error = nil, want one", tt.sql)
			}
			view := cmp.Or(tt.view, "v")
			want := "view " + view + ": columns come from the catalog, since its definition cannot be read as a view file: " + tt.want
			if got := viewFallbackWarning(view, err); got != want {
				t.Errorf("viewFallbackWarning() = %q, want %q", got, want)
			}
		})
	}
}
