package introspect_test

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/introspect"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// viewDefinitionDialect introspects ddl on one dialect and reads the same
// DDL as files.
type viewDefinitionDialect struct {
	name       string
	p, r       string // table names
	concat     string // a text expression over p.name
	introspect func(t *testing.T, ddl string) *parser.Schema
	fileParser func(t *testing.T) parser.Parser
	parseView  func(sql []byte) (parser.ViewSQL, error)
	// exprColumn is the catalog's column for the text expression, or nil
	// where the catalog has no type for it.
	exprColumn *parser.Column
}

func viewDefinitionDialects() []viewDefinitionDialect {
	return []viewDefinitionDialect{
		{
			name: "postgres", p: "p", r: "r", concat: "p.name || 'x'",
			introspect: func(t *testing.T, ddl string) *parser.Schema {
				// The connection's search_path holds the view's schema, so the
				// catalog's own view text leaves its tables unqualified.
				conn := pgConnStr + "&search_path=view_def"
				execAll(t, "pgx", conn, "DROP SCHEMA IF EXISTS view_def CASCADE; CREATE SCHEMA view_def;"+ddl)
				t.Cleanup(func() { execAll(t, "pgx", conn, "DROP SCHEMA IF EXISTS view_def CASCADE") })
				return introspectWith(t, introspect.NewPostgresIntrospector(), conn, introspect.IntrospectionOptions{Schemas: []string{"view_def"}})
			},
			fileParser: func(*testing.T) parser.Parser { return postgres.New("view_def") },
			parseView:  func(sql []byte) (parser.ViewSQL, error) { return postgres.ParseViewSQL(sql, "view_def") },
			exprColumn: &parser.Column{Name: "label", Type: "text", Nullable: true},
		},
		{
			name: "mysql", p: "vd_p", r: "vd_r", concat: "CONCAT(p.name, 'x')",
			introspect: func(t *testing.T, ddl string) *parser.Schema {
				const reset = "DROP VIEW IF EXISTS v_traced, v_expr, v_union; DROP TABLE IF EXISTS vd_r, vd_p"
				execAll(t, "mysql", mysqlConnStr, reset+";"+ddl)
				t.Cleanup(func() { execAll(t, "mysql", mysqlConnStr, reset) })
				return introspectWith(t, introspect.NewMySQLIntrospector(), mysqlConnStr, introspect.IntrospectionOptions{
					Tables: []string{"vd_p", "vd_r", "v_traced", "v_expr", "v_union"},
				})
			},
			fileParser: func(t *testing.T) parser.Parser {
				p, err := mysql.New()
				if err != nil {
					t.Fatalf("mysql.New() error: %v", err)
				}
				return p
			},
			parseView:  mysql.ParseViewSQL,
			exprColumn: &parser.Column{Name: "label", Type: "varchar(51)", Nullable: true},
		},
		{
			name: "sqlite", p: "p", r: "r", concat: "p.name || 'x'",
			introspect: func(t *testing.T, ddl string) *parser.Schema {
				return introspectSQLite(t, applySQLite(t, ddl), introspect.IntrospectionOptions{})
			},
			fileParser: func(*testing.T) parser.Parser { return sqlite.New() },
			parseView:  sqlite.ParseViewSQL,
		},
	}
}

func execAll(t *testing.T, driver, conn, ddl string) {
	t.Helper()
	db, err := sql.Open(driver, conn)
	if err != nil {
		t.Fatalf("opening %s: %v", driver, err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	for _, stmt := range splitStatements(ddl) {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			t.Fatalf("applying %q: %v", truncate(stmt), err)
		}
	}
}

func introspectWith(t *testing.T, intro introspect.Introspector, conn string, opts introspect.IntrospectionOptions) *parser.Schema {
	t.Helper()
	t.Cleanup(func() { _ = intro.Close() })
	schema := &parser.Schema{}
	if err := intro.Introspect(context.Background(), conn, schema, opts); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	return schema
}

// TestIntrospectViews_ColumnsFromDefinition pins that an introspected view's
// columns are the ones its definition gives as a view file (PRD §16.2), so a
// view over a nullable column or the outer side of a join is nullable on
// every dialect, though PostgreSQL and SQLite catalogs report every view
// column nullable.
func TestIntrospectViews_ColumnsFromDefinition(t *testing.T) {
	requireDatabases(t)
	for _, d := range viewDefinitionDialects() {
		t.Run(d.name, func(t *testing.T) {
			tables := "CREATE TABLE " + d.p + " (id int PRIMARY KEY, name varchar(50) NOT NULL, discount decimal(10,2));\n" +
				"CREATE TABLE " + d.r + " (id int PRIMARY KEY, p_id int NOT NULL, rating int NOT NULL);\n"
			traced := "CREATE VIEW v_traced AS SELECT p.id, p.name, p.discount, r.rating FROM " + d.p + " p LEFT JOIN " + d.r + " r ON r.p_id = p.id"
			expr := "CREATE VIEW v_expr AS SELECT p.id, " + d.concat + " AS label FROM " + d.p + " p"
			union := "CREATE VIEW v_union AS SELECT id FROM " + d.p + " UNION SELECT id FROM " + d.r

			schema := d.introspect(t, tables+traced+";\n"+expr+";\n"+union)

			// v_traced reads as the same view file would.
			files := parseStatements(t, d.fileParser(t), tables)
			parsed, err := d.parseView([]byte(traced))
			if err != nil {
				t.Fatalf("ParseViewSQL() error: %v", err)
			}
			want, err := parser.BuildView(parsed, traced, &parser.ViewAnnotations{}, files)
			if err != nil {
				t.Fatalf("BuildView() error: %v", err)
			}
			got := findView(t, schema, "v_traced")
			if diff := cmp.Diff(want.Columns, got.Columns); diff != "" {
				t.Errorf("v_traced columns differ from the view file's (-files +database):\n%s", diff)
			}
			nullable := map[string]bool{}
			for _, col := range got.Columns {
				nullable[col.Name] = col.Nullable
			}
			if diff := cmp.Diff(map[string]bool{"id": false, "name": false, "discount": true, "rating": true}, nullable); diff != "" {
				t.Errorf("v_traced nullability (-want +got):\n%s", diff)
			}

			// An expression the definition cannot type takes the catalog's
			// column, where the catalog has a type.
			label := findView(t, schema, "v_expr").Columns[1]
			wantLabel := parser.Column{Name: "label", Type: "any", GoTypeLiteral: "any"}
			if d.exprColumn != nil {
				wantLabel = *d.exprColumn
			}
			if diff := cmp.Diff(wantLabel, label); diff != "" {
				t.Errorf("v_expr label (-want +got):\n%s", diff)
			}

			// A definition the view parser cannot read keeps the catalog's
			// columns, with a warning.
			if names := columnNames(findView(t, schema, "v_union").Columns); !cmp.Equal(names, []string{"id"}) {
				t.Errorf("v_union columns = %v, want [id]", names)
			}
			var warned bool
			for _, w := range schema.Warnings {
				if strings.HasPrefix(w, "view ") && strings.Contains(w, "v_union: columns come from the catalog") {
					warned = true
				}
			}
			if !warned {
				t.Errorf("warnings = %q, want one naming v_union", schema.Warnings)
			}
		})
	}
}

// TestSQLiteIntrospect_ViewColumnList checks that a SQLite view with a column
// list reads its definition, named by the list. SQLite stores the original
// view text, so the definition carries the list; PostgreSQL and MySQL store
// a rewritten definition without one.
func TestSQLiteIntrospect_ViewColumnList(t *testing.T) {
	requireDatabases(t)
	schema := introspectSQLite(t, applySQLite(t,
		"CREATE TABLE t (id INTEGER PRIMARY KEY NOT NULL, a INT);\n"+
			"CREATE VIEW v_list (x, y) AS SELECT id, a FROM t"), introspect.IntrospectionOptions{})

	v := findView(t, schema, "v_list")
	if v.Definition == nil {
		t.Fatalf("v_list Definition = nil, want the parsed definition; warnings = %q", schema.Warnings)
	}
	nullable := map[string]bool{}
	for _, col := range v.Columns {
		nullable[col.Name] = col.Nullable
	}
	if diff := cmp.Diff(map[string]bool{"x": false, "y": true}, nullable); diff != "" {
		t.Errorf("v_list column nullability (-want +got):\n%s", diff)
	}
	for _, w := range schema.Warnings {
		if strings.Contains(w, "v_list") {
			t.Errorf("warning %q, want none for v_list", w)
		}
	}
}

// TestSQLiteIntrospect_ViewUnnamedLikeEscape checks that a SQLite view
// selecting an unaliased LIKE … ESCAPE keeps the catalog's columns with a
// warning naming the expression.
func TestSQLiteIntrospect_ViewUnnamedLikeEscape(t *testing.T) {
	requireDatabases(t)
	schema := introspectSQLite(t, applySQLite(t,
		"CREATE TABLE t (id INTEGER PRIMARY KEY NOT NULL, a TEXT NOT NULL);\n"+
			`CREATE VIEW v_like AS SELECT id, a LIKE 'x%' ESCAPE '\' FROM t`), introspect.IntrospectionOptions{})

	if names := columnNames(findView(t, schema, "v_like").Columns); !cmp.Equal(names, []string{"id", `a LIKE 'x%' ESCAPE '\'`}) {
		t.Errorf("v_like columns = %q, want the catalog's", names)
	}
	want := "view v_like: columns come from the catalog, since its definition cannot be read as a view file: " +
		`view v_like selects "a" LIKE 'x%' ESCAPE '\' with no alias and no column name; name it with AS`
	if !slices.Contains(schema.Warnings, want) {
		t.Errorf("warnings = %q, want %q", schema.Warnings, want)
	}
}

// TestMerge_RefreshesViewColumns pins that under source: both a migration
// that changes a table a view reads changes the view's columns too, and
// that a materialized view keeps the primary key its unique index gives.
func TestMerge_RefreshesViewColumns(t *testing.T) {
	requireDatabases(t)
	conn := pgConnStr + "&search_path=view_def"
	execAll(t, "pgx", conn, `DROP SCHEMA IF EXISTS view_def CASCADE; CREATE SCHEMA view_def;
		CREATE TABLE t (id int PRIMARY KEY, name text NOT NULL);
		CREATE MATERIALIZED VIEW mv AS SELECT id, name FROM t;
		CREATE UNIQUE INDEX mv_id ON mv (id)`)
	t.Cleanup(func() { execAll(t, "pgx", conn, "DROP SCHEMA IF EXISTS view_def CASCADE") })
	base := introspectWith(t, introspect.NewPostgresIntrospector(), conn, introspect.IntrospectionOptions{Schemas: []string{"view_def"}})

	p := postgres.New("view_def")
	if err := introspect.Merge(base, p, []string{writeMigration(t, "ALTER TABLE t ALTER COLUMN name DROP NOT NULL;")}); err != nil {
		t.Fatalf("Merge() error: %v", err)
	}

	type col struct {
		Name                 string
		PrimaryKey, Nullable bool
	}
	mv := findView(t, p.Schema(), "mv")
	got := make([]col, 0, len(mv.Columns))
	for _, c := range mv.Columns {
		got = append(got, col{c.Name, c.PrimaryKey, c.Nullable})
	}
	if diff := cmp.Diff([]col{{"id", true, false}, {"name", false, true}}, got); diff != "" {
		t.Errorf("mv columns after the migration (-want +got):\n%s", diff)
	}
}

// TestMerge_ViewColumnRenameAndDrop checks that under source: both a
// migration that renames or drops a table column a view reads is applied
// the way the database applies it: PostgreSQL keeps the view's column name
// and reads the renamed column, SQLite also renames a column named after its
// source, and dropping a column a view reads is an error on both, as the
// database refuses it. MySQL leaves the view invalid on either, so both are
// errors there.
func TestMerge_ViewColumnRenameAndDrop(t *testing.T) {
	requireDatabases(t)
	type col struct {
		Name, Type string
		Nullable   bool
	}
	pgBase := func(t *testing.T) *parser.Schema {
		conn := pgConnStr + "&search_path=view_def"
		execAll(t, "pgx", conn, `DROP SCHEMA IF EXISTS view_def CASCADE; CREATE SCHEMA view_def;
			CREATE TYPE role_t AS ENUM ('a', 'b');
			CREATE TABLE users (id int PRIMARY KEY, role role_t NOT NULL);
			CREATE VIEW v AS SELECT id, role FROM users;
			CREATE VIEW v2 AS SELECT u.id, u.role AS r FROM users u;
			CREATE VIEW vu AS SELECT role FROM users UNION SELECT role FROM users`)
		t.Cleanup(func() { execAll(t, "pgx", conn, "DROP SCHEMA IF EXISTS view_def CASCADE") })
		return introspectWith(t, introspect.NewPostgresIntrospector(), conn, introspect.IntrospectionOptions{Schemas: []string{"view_def"}})
	}
	sqliteBase := func(t *testing.T) *parser.Schema {
		return introspectSQLite(t, applySQLite(t, `CREATE TABLE users (id INTEGER PRIMARY KEY NOT NULL, role TEXT NOT NULL);
			CREATE VIEW v AS SELECT id, role FROM users;
			CREATE VIEW v2 AS SELECT u.id, u.role AS r FROM users u;
			CREATE VIEW v3 (a, b) AS SELECT id, role FROM users`), introspect.IntrospectionOptions{})
	}
	// mysqlBase introspects the table and view.
	mysqlBase := func(view string) func(*testing.T) *parser.Schema {
		return func(t *testing.T) *parser.Schema {
			const reset = "DROP VIEW IF EXISTS vr_v, vr_vl; DROP TABLE IF EXISTS vr_users"
			execAll(t, "mysql", mysqlConnStr, reset+`;
				CREATE TABLE vr_users (id int PRIMARY KEY, role enum('a', 'b') NOT NULL);
				CREATE VIEW vr_v AS SELECT id, role FROM vr_users;
				CREATE VIEW vr_vl (uid, role) AS SELECT id, role FROM vr_users`)
			t.Cleanup(func() { execAll(t, "mysql", mysqlConnStr, reset) })
			return introspectWith(t, introspect.NewMySQLIntrospector(), mysqlConnStr, introspect.IntrospectionOptions{
				Tables: []string{"vr_users", view},
			})
		}
	}
	newPostgres := func(*testing.T) introspect.Seedable { return postgres.New("view_def") }
	newSQLite := func(*testing.T) introspect.Seedable { return sqlite.New() }
	newMySQL := func(t *testing.T) introspect.Seedable {
		p, err := mysql.New()
		if err != nil {
			t.Fatalf("mysql.New() error: %v", err)
		}
		return p
	}
	const stands = ", which the SQL files drop or rename; under source: both the database's view stands, so change it in the database first"
	// A rename migration adds a column under the old name, so the view's
	// columns show which of the two it reads.
	tests := []struct {
		name      string
		base      func(*testing.T) *parser.Schema
		parser    func(*testing.T) introspect.Seedable
		migration string
		want      map[string][]col
		// wantErr is part of the error.
		wantErr string
	}{
		{
			name:   "postgres/rename",
			base:   pgBase,
			parser: newPostgres,
			migration: `ALTER TABLE users RENAME COLUMN role TO kind;
				ALTER TABLE users ALTER COLUMN kind DROP NOT NULL;
				ALTER TABLE users ADD COLUMN role text NOT NULL;`,
			want: map[string][]col{
				"v":  {{"id", "integer", false}, {"role", "role_t", true}},
				"v2": {{"id", "integer", false}, {"r", "role_t", true}},
			},
		},
		{
			name:      "postgres/drop",
			base:      pgBase,
			parser:    newPostgres,
			migration: "ALTER TABLE users DROP COLUMN role;",
			wantErr:   "merge both mode: view view_def.v: column role reads column role of table view_def.users" + stands,
		},
		{
			// The file parser refuses to drop a type a database view uses,
			// as PostgreSQL does.
			name:      "postgres/drop type",
			base:      pgBase,
			parser:    newPostgres,
			migration: "ALTER TABLE users ALTER COLUMN role TYPE text; DROP TYPE role_t;",
			wantErr:   "drop type: cannot drop type view_def.role_t: column view_def.v.role of view uses it",
		},
		{
			name:   "sqlite/rename",
			base:   sqliteBase,
			parser: newSQLite,
			migration: `ALTER TABLE users RENAME COLUMN role TO kind;
				ALTER TABLE users ADD COLUMN role INTEGER;`,
			want: map[string][]col{
				"v":  {{"id", "integer", false}, {"kind", "text", false}},
				"v2": {{"id", "integer", false}, {"r", "text", false}},
				"v3": {{"a", "integer", false}, {"b", "text", false}},
			},
		},
		{
			// SQLite matches the column name ignoring ASCII case.
			name:      "sqlite/rename in another case",
			base:      sqliteBase,
			parser:    newSQLite,
			migration: "ALTER TABLE users RENAME COLUMN ROLE TO kind;",
			want: map[string][]col{
				"v":  {{"id", "integer", false}, {"kind", "text", false}},
				"v2": {{"id", "integer", false}, {"r", "text", false}},
			},
		},
		{
			name:      "sqlite/drop",
			base:      sqliteBase,
			parser:    newSQLite,
			migration: "ALTER TABLE users DROP COLUMN role;",
			wantErr:   "merge both mode: view v: column role reads column role of table users" + stands,
		},
		{
			name:      "mysql/rename",
			base:      mysqlBase("vr_v"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users RENAME COLUMN role TO kind;",
			wantErr:   "merge both mode: view vr_v: column role reads column role of table vr_users" + stands,
		},
		{
			name:      "mysql/drop",
			base:      mysqlBase("vr_v"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			wantErr:   "merge both mode: view vr_v: column role reads column role of table vr_users" + stands,
		},
		{
			name:      "mysql/rename under a view with a column list",
			base:      mysqlBase("vr_vl"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users RENAME COLUMN role TO kind;",
			wantErr:   "merge both mode: view vr_vl: column role reads column role of table vr_users" + stands,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.parser(t)
			err := introspect.Merge(tt.base(t), p, []string{writeMigration(t, tt.migration)})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Merge() error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Merge() error: %v", err)
			}
			got := make(map[string][]col, len(tt.want))
			for name := range tt.want {
				for _, c := range findView(t, p.Schema(), name).Columns {
					got[name] = append(got[name], col{c.Name, c.Type, c.Nullable})
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("view columns after the migration (-want +got):\n%s", diff)
			}
		})
	}
}
