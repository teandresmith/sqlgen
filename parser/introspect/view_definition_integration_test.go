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
			want, err := parser.BuildView(parsed, traced, &parser.ViewAnnotations{}, files, parser.SearchPath{})
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
		`it selects "a" LIKE 'x%' ESCAPE '\' with no alias and no column name`
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
// migration that renames a table or column a view reads is applied the way
// the database applies it: PostgreSQL keeps the view's column name and reads
// the renamed column, and SQLite also renames a column named after its
// source. MySQL leaves the view invalid on a rename or drop, so both are
// errors there, as is renaming or dropping a table a view reads; a column
// the query reads outside its select list counts the same. Which PostgreSQL
// and SQLite migrations are errors is the database's answer, checked by
// TestMerge_ViewDependenciesMatchPostgres and …MatchSQLite.
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
	// The *Sub bases hold views that read users through a derived table or
	// a common table expression.
	pgSub := func(views string) func(*testing.T) *parser.Schema {
		return func(t *testing.T) *parser.Schema {
			conn := pgConnStr + "&search_path=view_def"
			execAll(t, "pgx", conn, `DROP SCHEMA IF EXISTS view_def CASCADE; CREATE SCHEMA view_def;
				CREATE TYPE role_t AS ENUM ('a', 'b');
				CREATE TABLE users (id int PRIMARY KEY, role role_t NOT NULL, email text);`+views)
			t.Cleanup(func() { execAll(t, "pgx", conn, "DROP SCHEMA IF EXISTS view_def CASCADE") })
			return introspectWith(t, introspect.NewPostgresIntrospector(), conn, introspect.IntrospectionOptions{Schemas: []string{"view_def"}})
		}
	}
	const (
		pgDerived = "CREATE VIEW vd AS SELECT r FROM (SELECT role AS r FROM users) d;"
		// A common table expression that reads another one.
		pgCTE = "CREATE VIEW vc AS WITH a AS (SELECT role FROM users), c AS (SELECT role AS r FROM a) SELECT r FROM c;"
		// A CTE named after the table it reads, whose role is the table's
		// email.
		pgCTENamedAfterTable = "CREATE VIEW vt AS WITH users AS (SELECT email AS role FROM users) SELECT role FROM users;"
	)
	sqliteSub := func(views string) func(*testing.T) *parser.Schema {
		return func(t *testing.T) *parser.Schema {
			return introspectSQLite(t, applySQLite(t, "CREATE TABLE users (id INTEGER PRIMARY KEY NOT NULL, role TEXT NOT NULL, email TEXT);"+views), introspect.IntrospectionOptions{})
		}
	}
	mysqlSub := func(views ...string) func(*testing.T) *parser.Schema {
		return func(t *testing.T) *parser.Schema {
			const reset = "DROP VIEW IF EXISTS vr_vd, vr_vc, vr_vq, vr_vt; DROP TABLE IF EXISTS vr_users"
			ddl := map[string]string{
				"vr_vd": "CREATE VIEW vr_vd AS SELECT r FROM (SELECT role AS r FROM vr_users) d",
				"vr_vc": "CREATE VIEW vr_vc AS WITH c AS (SELECT role FROM vr_users) SELECT role FROM c",
				// A derived table and a CTE named after the table they read,
				// whose role is the table's email.
				"vr_vq": "CREATE VIEW vr_vq AS SELECT vr_users.role FROM (SELECT email AS role FROM vr_users) vr_users",
				"vr_vt": "CREATE VIEW vr_vt AS WITH vr_users AS (SELECT email AS role FROM vr_users) SELECT role FROM vr_users",
			}
			stmts := make([]string, 0, 2+len(views))
			stmts = append(stmts, reset, "CREATE TABLE vr_users (id int PRIMARY KEY, role enum('a', 'b') NOT NULL, email text)")
			for _, v := range views {
				stmts = append(stmts, ddl[v])
			}
			execAll(t, "mysql", mysqlConnStr, strings.Join(stmts, ";"))
			t.Cleanup(func() { execAll(t, "mysql", mysqlConnStr, reset) })
			return introspectWith(t, introspect.NewMySQLIntrospector(), mysqlConnStr, introspect.IntrospectionOptions{
				Tables: append([]string{"vr_users"}, views...),
			})
		}
	}
	// The *Reads bases hold view r, whose query reads columns outside its
	// select list.
	pgReads := func(query string) func(*testing.T) *parser.Schema {
		return func(t *testing.T) *parser.Schema {
			conn := pgConnStr + "&search_path=view_def"
			execAll(t, "pgx", conn, `DROP SCHEMA IF EXISTS view_def CASCADE; CREATE SCHEMA view_def;
				CREATE TABLE users (id int PRIMARY KEY, role text NOT NULL, email text);
				CREATE TABLE posts (id int PRIMARY KEY, uid int, title text, email text);
				CREATE TABLE tags (id int PRIMARY KEY, uid int);
				CREATE VIEW r AS `+query)
			t.Cleanup(func() { execAll(t, "pgx", conn, "DROP SCHEMA IF EXISTS view_def CASCADE") })
			return introspectWith(t, introspect.NewPostgresIntrospector(), conn, introspect.IntrospectionOptions{Schemas: []string{"view_def"}})
		}
	}
	sqliteReads := func(query string) func(*testing.T) *parser.Schema {
		return func(t *testing.T) *parser.Schema {
			return introspectSQLite(t, applySQLite(t, `CREATE TABLE users (id INTEGER PRIMARY KEY NOT NULL, role TEXT NOT NULL, email TEXT);
				CREATE TABLE posts (id INTEGER PRIMARY KEY NOT NULL, uid INTEGER, title TEXT, email TEXT);
				CREATE TABLE tags (id INTEGER PRIMARY KEY NOT NULL, uid INTEGER);
				CREATE VIEW r AS `+query), introspect.IntrospectionOptions{})
		}
	}
	mysqlReads := func(query string) func(*testing.T) *parser.Schema {
		return func(t *testing.T) *parser.Schema {
			const reset = "DROP VIEW IF EXISTS vr_r; DROP TABLE IF EXISTS vr_users, vr_posts"
			execAll(t, "mysql", mysqlConnStr, reset+`;
				CREATE TABLE vr_users (id int PRIMARY KEY, role varchar(20) NOT NULL, email text);
				CREATE TABLE vr_posts (id int PRIMARY KEY, uid int, title varchar(20));
				CREATE VIEW vr_r AS `+query)
			t.Cleanup(func() { execAll(t, "mysql", mysqlConnStr, reset) })
			return introspectWith(t, introspect.NewMySQLIntrospector(), mysqlConnStr, introspect.IntrospectionOptions{
				Tables: []string{"vr_users", "vr_posts", "vr_r"},
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
		// Table renames and drops. A rename migration creates a table under
		// the old name, so the view's columns show which of the two it reads.
		{
			name:   "postgres/rename table",
			base:   pgBase,
			parser: newPostgres,
			migration: `ALTER TABLE users RENAME TO people;
				ALTER TABLE people ALTER COLUMN role DROP NOT NULL;
				CREATE TABLE users (id bigint PRIMARY KEY, role text NOT NULL);`,
			want: map[string][]col{
				"v":  {{"id", "integer", false}, {"role", "role_t", true}},
				"v2": {{"id", "integer", false}, {"r", "role_t", true}},
			},
		},
		{
			name:   "postgres/set schema",
			base:   pgBase,
			parser: newPostgres,
			migration: `ALTER TABLE users SET SCHEMA view_def_other;
				ALTER TABLE view_def_other.users ALTER COLUMN role DROP NOT NULL;
				CREATE TABLE users (id bigint PRIMARY KEY, role text NOT NULL);`,
			want: map[string][]col{
				"v":  {{"id", "integer", false}, {"role", "role_t", true}},
				"v2": {{"id", "integer", false}, {"r", "role_t", true}},
			},
		},
		{
			name:   "sqlite/rename table",
			base:   sqliteBase,
			parser: newSQLite,
			migration: `ALTER TABLE users RENAME TO people;
				CREATE TABLE users (id TEXT PRIMARY KEY NOT NULL, role INTEGER);`,
			want: map[string][]col{
				"v":  {{"id", "integer", false}, {"role", "text", false}},
				"v2": {{"id", "integer", false}, {"r", "text", false}},
				"v3": {{"a", "integer", false}, {"b", "text", false}},
			},
		},
		{
			name:      "mysql/rename table",
			base:      mysqlBase("vr_v"),
			parser:    newMySQL,
			migration: "RENAME TABLE vr_users TO vr_people;",
			wantErr:   "merge both mode: view vr_v reads table vr_users" + stands,
		},
		{
			name:      "mysql/alter table rename",
			base:      mysqlBase("vr_v"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users RENAME TO vr_people;",
			wantErr:   "merge both mode: view vr_v reads table vr_users" + stands,
		},
		{
			name:      "mysql/drop table",
			base:      mysqlBase("vr_v"),
			parser:    newMySQL,
			migration: "DROP TABLE vr_users;",
			wantErr:   "merge both mode: view vr_v reads table vr_users" + stands,
		},
		{
			// MySQL reads a view's tables by name, so a table created under
			// the old name makes the view valid again.
			name:   "mysql/rename table and create one under its name",
			base:   mysqlBase("vr_v"),
			parser: newMySQL,
			migration: `RENAME TABLE vr_users TO vr_people;
				CREATE TABLE vr_users (id bigint PRIMARY KEY, role text NOT NULL);`,
			want: map[string][]col{
				"vr_v": {{"id", "bigint", false}, {"role", "text", false}},
			},
		},
		// A column a view reads through a derived table or a common table
		// expression is followed to its table. Its type stays the catalog's.
		{
			name:      "postgres/rename under a derived table and a CTE",
			base:      pgSub(pgDerived + pgCTE),
			parser:    newPostgres,
			migration: "ALTER TABLE users RENAME COLUMN role TO kind;",
			want: map[string][]col{
				"vd": {{"r", "role_t", true}},
				"vc": {{"r", "role_t", true}},
			},
		},
		{
			name:      "postgres/drop a column no view reads",
			base:      pgSub(pgDerived + pgCTE),
			parser:    newPostgres,
			migration: "ALTER TABLE users DROP COLUMN email;",
			want: map[string][]col{
				"vd": {{"r", "role_t", true}},
				"vc": {{"r", "role_t", true}},
			},
		},
		{
			name:      "postgres/rename table under a derived table and a CTE",
			base:      pgSub(pgDerived + pgCTE),
			parser:    newPostgres,
			migration: "ALTER TABLE users RENAME TO people;",
			want: map[string][]col{
				"vd": {{"r", "role_t", true}},
				"vc": {{"r", "role_t", true}},
			},
		},
		{
			// SQLite renames the column inside the subquery, and the view
			// still reads it under the name the subquery gives it.
			name:      "sqlite/rename under an aliased derived table and a CTE column list",
			base:      sqliteSub("CREATE VIEW vd AS SELECT r FROM (SELECT role AS r FROM users) d; CREATE VIEW vc AS WITH c (r) AS (SELECT role FROM users) SELECT r FROM c;"),
			parser:    newSQLite,
			migration: "ALTER TABLE users RENAME COLUMN role TO kind;",
			want: map[string][]col{
				"vd": {{"r", "text", true}},
				"vc": {{"r", "text", true}},
			},
		},
		{
			name:      "sqlite/rename table under a derived table",
			base:      sqliteSub("CREATE VIEW vd AS SELECT r FROM (SELECT role AS r FROM users) d;"),
			parser:    newSQLite,
			migration: "ALTER TABLE users RENAME TO people;",
			want:      map[string][]col{"vd": {{"r", "text", true}}},
		},
		{
			name:      "mysql/rename under a CTE",
			base:      mysqlSub("vr_vc"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users RENAME COLUMN role TO kind;",
			wantErr:   "merge both mode: view vr_vc: column role reads column role of table vr_users" + stands,
		},
		{
			name:      "mysql/drop under a CTE",
			base:      mysqlSub("vr_vc"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			wantErr:   "merge both mode: view vr_vc: column role reads column role of table vr_users" + stands,
		},
		{
			name:      "mysql/rename under a derived table",
			base:      mysqlSub("vr_vd"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users RENAME COLUMN role TO kind;",
			wantErr:   "merge both mode: view vr_vd: column r reads column role of table vr_users" + stands,
		},
		{
			name:      "mysql/drop under a derived table",
			base:      mysqlSub("vr_vd"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			wantErr:   "merge both mode: view vr_vd: column r reads column role of table vr_users" + stands,
		},
		{
			name:      "mysql/rename table under a derived table",
			base:      mysqlSub("vr_vd"),
			parser:    newMySQL,
			migration: "RENAME TABLE vr_users TO vr_people;",
			wantErr:   "merge both mode: view vr_vd reads table vr_users" + stands,
		},
		// A derived table or CTE named after a table is read before the
		// table, so a column it renames is followed to the table's column.
		{
			// The CTE follows the rename, and the view still reads the CTE.
			name:      "postgres/rename table under a CTE named after it",
			base:      pgSub(pgCTENamedAfterTable),
			parser:    newPostgres,
			migration: "ALTER TABLE users RENAME TO people;",
			want:      map[string][]col{"vt": {{"role", "text", true}}},
		},
		{
			// The compound query cannot be traced, but its role is not the
			// table's, which SQLite drops.
			name:      "sqlite/drop a column a compound derived table named after its table does not read",
			base:      sqliteSub("CREATE VIEW vu AS SELECT users.role FROM (SELECT email AS role FROM users UNION ALL SELECT email FROM users) users;"),
			parser:    newSQLite,
			migration: "ALTER TABLE users DROP COLUMN role;",
			want:      map[string][]col{"vu": {{"role", "text", true}}},
		},
		{
			name:      "mysql/drop under a derived table named after its table",
			base:      mysqlSub("vr_vq"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN email;",
			wantErr:   "merge both mode: view vr_vq: column role reads column email of table vr_users" + stands,
		},
		{
			name:      "mysql/rename under a derived table named after its table",
			base:      mysqlSub("vr_vq"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users RENAME COLUMN email TO kind;",
			wantErr:   "merge both mode: view vr_vq: column role reads column email of table vr_users" + stands,
		},
		{
			name:      "mysql/drop under a CTE named after its table",
			base:      mysqlSub("vr_vt"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN email;",
			wantErr:   "merge both mode: view vr_vt: column role reads column email of table vr_users" + stands,
		},
		{
			name:      "mysql/drop a column no view reads",
			base:      mysqlSub("vr_vd", "vr_vc"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN email;",
			want: map[string][]col{
				"vr_vd": {{"r", "vr_vd__r_enum", false}},
				"vr_vc": {{"role", "vr_vc__role_enum", false}},
			},
		},
		// A column a view reads outside its select list: in WHERE, JOIN ON,
		// an expression or a subquery in an expression.
		{
			// PostgreSQL follows both renames.
			name:   "postgres/rename a column and a table read outside the select list",
			base:   pgReads("SELECT id FROM users u WHERE role = 'a' AND EXISTS (SELECT 1 FROM posts p WHERE p.uid = u.id AND p.title = 'x')"),
			parser: newPostgres,
			migration: `ALTER TABLE users RENAME COLUMN role TO kind;
				ALTER TABLE posts RENAME COLUMN title TO heading;
				ALTER TABLE posts RENAME TO articles;`,
			want: map[string][]col{"r": {{"id", "integer", false}}},
		},
		{
			name:   "sqlite/rename a column and a table read outside the select list",
			base:   sqliteReads("SELECT id FROM users u WHERE role = 'a' AND EXISTS (SELECT 1 FROM posts p WHERE p.uid = u.id AND p.title = 'x')"),
			parser: newSQLite,
			migration: `ALTER TABLE users RENAME COLUMN role TO kind;
				ALTER TABLE posts RENAME COLUMN title TO heading;
				ALTER TABLE posts RENAME TO articles;`,
			want: map[string][]col{"r": {{"id", "integer", false}}},
		},
		{
			// ORDER BY role is the select-list column, not users.role.
			name:      "sqlite/drop a column an ORDER BY alias shadows",
			base:      sqliteReads("SELECT upper(email) AS role FROM users ORDER BY role"),
			parser:    newSQLite,
			migration: "ALTER TABLE users DROP COLUMN role;",
			want:      map[string][]col{"r": {{"role", "any", false}}},
		},
		{
			name:      "mysql/drop a column read in WHERE",
			base:      mysqlReads("SELECT id FROM vr_users WHERE role = 'a'"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			wantErr:   "merge both mode: view vr_r reads column role of table vr_users" + stands,
		},
		{
			name:      "mysql/rename a column read in WHERE",
			base:      mysqlReads("SELECT id FROM vr_users WHERE role = 'a'"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users RENAME COLUMN role TO kind;",
			wantErr:   "merge both mode: view vr_r reads column role of table vr_users" + stands,
		},
		{
			name:      "mysql/rename a column read in JOIN ON",
			base:      mysqlReads("SELECT p.id FROM vr_posts p JOIN vr_users u ON u.role = p.title"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_posts RENAME COLUMN title TO heading;",
			wantErr:   "merge both mode: view vr_r reads column title of table vr_posts" + stands,
		},
		{
			name:      "mysql/drop a column read in an expression under a CTE",
			base:      mysqlReads("WITH c AS (SELECT upper(role) AS u FROM vr_users) SELECT u FROM c"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			wantErr:   "merge both mode: view vr_r reads column role of table vr_users" + stands,
		},
		{
			name:      "mysql/drop a column an IN subquery selects",
			base:      mysqlReads("SELECT id FROM vr_users WHERE id IN (SELECT uid FROM vr_posts)"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_posts DROP COLUMN uid;",
			wantErr:   "merge both mode: view vr_r reads column uid of table vr_posts" + stands,
		},
		{
			name:      "mysql/rename a table read in an EXISTS subquery",
			base:      mysqlReads("SELECT id FROM vr_users u WHERE EXISTS (SELECT 1 FROM vr_posts p WHERE p.uid = u.id)"),
			parser:    newMySQL,
			migration: "RENAME TABLE vr_posts TO vr_articles;",
			wantErr:   "merge both mode: view vr_r reads table vr_posts" + stands,
		},
		{
			// PostgreSQL follows the renames.
			name:   "postgres/rename a column USING and a FROM function read",
			base:   pgReads("SELECT p.id FROM users u JOIN tags t ON t.uid = u.id JOIN posts p USING (email), unnest(string_to_array(u.email, ',')) x"),
			parser: newPostgres,
			migration: `ALTER TABLE users RENAME COLUMN email TO mail;
				ALTER TABLE posts RENAME COLUMN email TO mail;`,
			want: map[string][]col{"r": {{"id", "integer", false}}},
		},
		{
			name:      "sqlite/rename a column a table function reads",
			base:      sqliteReads("SELECT u.id FROM users u, json_each(u.email) j"),
			parser:    newSQLite,
			migration: "ALTER TABLE users RENAME COLUMN email TO mail;",
			want:      map[string][]col{"r": {{"id", "integer", false}}},
		},
		{
			name:      "mysql/drop a column read under a derived table in EXISTS",
			base:      mysqlReads("SELECT id FROM vr_users u WHERE EXISTS (SELECT 1 FROM (SELECT p.uid FROM vr_posts p WHERE p.uid = u.id AND u.role = 'a') d)"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			wantErr:   "merge both mode: view vr_r reads column role of table vr_users" + stands,
		},
		{
			name:      "mysql/rename a column a LATERAL derived table reads",
			base:      mysqlReads("SELECT u.id FROM vr_users u, LATERAL (SELECT p.title FROM vr_posts p WHERE p.uid = u.id AND u.role = 'a') l"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users RENAME COLUMN role TO kind;",
			wantErr:   "merge both mode: view vr_r reads column role of table vr_users" + stands,
		},
		{
			name:      "mysql/drop a column a JSON_TABLE reads",
			base:      mysqlReads("SELECT u.id FROM vr_users u, JSON_TABLE(u.email, '$[*]' COLUMNS (x int PATH '$')) j"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN email;",
			wantErr:   "merge both mode: view vr_r reads column email of table vr_users" + stands,
		},
		{
			name:      "mysql/drop a column no view reads outside its select list",
			base:      mysqlReads("SELECT id FROM vr_users WHERE role = 'a'"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN email;",
			want:      map[string][]col{"vr_r": {{"id", "int", false}}},
		},
		// A derived table that is not LATERAL, and a common table
		// expression, cannot read the FROM clause of the query they are in,
		// so a name there that their own FROM clause lacks is a select-list
		// alias or a string, not that query's column of that name.
		{
			name:      "sqlite/drop a column a CTE's GROUP BY alias shadows",
			base:      sqliteReads("WITH c AS (SELECT upper(title) AS role, count(*) AS n FROM posts GROUP BY role) SELECT u.id FROM users u, c"),
			parser:    newSQLite,
			migration: "ALTER TABLE users DROP COLUMN role;",
			want:      map[string][]col{"r": {{"id", "integer", false}}},
		},
		{
			name:      "sqlite/drop a column a derived table's GROUP BY alias shadows",
			base:      sqliteReads("SELECT u.id FROM users u, (SELECT upper(title) AS role, count(*) AS n FROM posts GROUP BY role) d"),
			parser:    newSQLite,
			migration: "ALTER TABLE users DROP COLUMN role;",
			want:      map[string][]col{"r": {{"id", "integer", false}}},
		},
		{
			name:      "sqlite/drop a column a derived table's double-quoted string names",
			base:      sqliteReads(`SELECT u.id FROM users u JOIN (SELECT uid FROM posts WHERE title <> "role") d ON d.uid = u.id`),
			parser:    newSQLite,
			migration: "ALTER TABLE users DROP COLUMN role;",
			want:      map[string][]col{"r": {{"id", "integer", false}}},
		},
		{
			name:      "sqlite/drop a column a CTE's double-quoted string names",
			base:      sqliteReads(`WITH c AS (SELECT uid FROM posts WHERE title <> "role") SELECT u.id FROM users u JOIN c ON c.uid = u.id`),
			parser:    newSQLite,
			migration: "ALTER TABLE users DROP COLUMN role;",
			want:      map[string][]col{"r": {{"id", "integer", false}}},
		},
		{
			name:      "sqlite/drop a column the GROUP BY alias of a CTE an IN subquery reads shadows",
			base:      sqliteReads("WITH c AS (SELECT upper(title) AS role, count(*) AS n FROM posts GROUP BY role) SELECT u.id FROM users u WHERE u.id IN (SELECT n FROM c)"),
			parser:    newSQLite,
			migration: "ALTER TABLE users DROP COLUMN role;",
			want:      map[string][]col{"r": {{"id", "integer", false}}},
		},
		{
			name:      "mysql/drop a column a CTE's GROUP BY alias shadows",
			base:      mysqlReads("WITH c AS (SELECT upper(title) AS role, count(*) AS n FROM vr_posts GROUP BY role) SELECT u.id FROM vr_users u, c"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			want:      map[string][]col{"vr_r": {{"id", "int", false}}},
		},
		{
			name:      "mysql/rename a column a CTE's GROUP BY alias shadows",
			base:      mysqlReads("WITH c AS (SELECT upper(title) AS role, count(*) AS n FROM vr_posts GROUP BY role) SELECT u.id FROM vr_users u, c"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users RENAME COLUMN role TO kind;",
			want:      map[string][]col{"vr_r": {{"id", "int", false}}},
		},
		{
			name:      "mysql/drop a column a derived table's GROUP BY alias shadows",
			base:      mysqlReads("SELECT u.id FROM vr_users u, (SELECT upper(title) AS role, count(*) AS n FROM vr_posts GROUP BY role) d"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			want:      map[string][]col{"vr_r": {{"id", "int", false}}},
		},
		{
			name:      "mysql/drop a column a derived table's ORDER BY alias shadows",
			base:      mysqlReads("SELECT u.id FROM vr_users u, (SELECT upper(title) AS role FROM vr_posts ORDER BY role LIMIT 5) d"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			want:      map[string][]col{"vr_r": {{"id", "int", false}}},
		},
		{
			name:      "mysql/rename a column a CTE's ORDER BY alias shadows",
			base:      mysqlReads("WITH c AS (SELECT upper(title) AS role FROM vr_posts ORDER BY role LIMIT 5) SELECT u.id FROM vr_users u, c"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users RENAME COLUMN role TO kind;",
			want:      map[string][]col{"vr_r": {{"id", "int", false}}},
		},
		{
			name:      "mysql/drop a column the GROUP BY alias of a CTE an IN subquery reads shadows",
			base:      mysqlReads("WITH c AS (SELECT upper(title) AS role, count(*) AS n FROM vr_posts GROUP BY role) SELECT u.id FROM vr_users u WHERE u.id IN (SELECT n FROM c)"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			want:      map[string][]col{"vr_r": {{"id", "int", false}}},
		},
		// A name in GROUP BY, HAVING or ORDER BY can be a select-list alias.
		// PostgreSQL stores a GROUP BY or ORDER BY alias as the expression,
		// and binds a GROUP BY name to a FROM-clause column first; MySQL
		// stores a name it binds to a column qualified and an alias bare;
		// SQLite binds a GROUP BY or HAVING name to a FROM-clause column
		// first and to the alias once the column is gone, which an aggregate
		// or window alias cannot stand in for everywhere.
		{
			name:      "postgres/drop a column an IN subquery's GROUP BY alias shadows",
			base:      pgReads("SELECT u.id FROM users u WHERE u.id IN (SELECT length(p.title) AS role FROM posts p GROUP BY role)"),
			parser:    newPostgres,
			migration: "ALTER TABLE users DROP COLUMN role;",
			want:      map[string][]col{"r": {{"id", "integer", false}}},
		},
		{
			name:      "sqlite/drop a column an IN subquery's GROUP BY alias shadows",
			base:      sqliteReads("SELECT u.id FROM users u WHERE u.id IN (SELECT length(p.title) AS role FROM posts p GROUP BY role)"),
			parser:    newSQLite,
			migration: "ALTER TABLE users DROP COLUMN role;",
			want:      map[string][]col{"r": {{"id", "integer", false}}},
		},
		{
			name:      "sqlite/drop a column a HAVING alias stands in for",
			base:      sqliteReads("SELECT max(id) AS role FROM users GROUP BY email HAVING role > 0"),
			parser:    newSQLite,
			migration: "ALTER TABLE users DROP COLUMN role;",
			want:      map[string][]col{"r": {{"role", "integer", true}}},
		},
		{
			name:      "sqlite/drop a column an IN subquery's HAVING alias shadows",
			base:      sqliteReads("SELECT u.id FROM users u WHERE u.id IN (SELECT max(p.id) AS role FROM posts p GROUP BY p.uid HAVING role > 0)"),
			parser:    newSQLite,
			migration: "ALTER TABLE users DROP COLUMN role;",
			want:      map[string][]col{"r": {{"id", "integer", false}}},
		},
		{
			name:      "sqlite/drop a column a GROUP BY alias stands in for",
			base:      sqliteReads("SELECT upper(email) AS role FROM users GROUP BY role"),
			parser:    newSQLite,
			migration: "ALTER TABLE users DROP COLUMN role;",
			want:      map[string][]col{"r": {{"role", "any", false}}},
		},
		{
			name:      "sqlite/drop a column an ORDER BY window alias stands in for",
			base:      sqliteReads("SELECT row_number() OVER () AS role FROM users ORDER BY role || ''"),
			parser:    newSQLite,
			migration: "ALTER TABLE users DROP COLUMN role;",
			want:      map[string][]col{"r": {{"role", "any", false}}},
		},
		{
			name:      "mysql/drop a column an IN subquery's GROUP BY alias shadows",
			base:      mysqlReads("SELECT u.id FROM vr_users u WHERE u.id IN (SELECT length(p.title) AS role FROM vr_posts p GROUP BY role)"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			want:      map[string][]col{"vr_r": {{"id", "int", false}}},
		},
		{
			name:      "mysql/drop a column a HAVING alias shadows",
			base:      mysqlReads("SELECT max(id) AS role FROM vr_users GROUP BY email HAVING role > 0"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			want:      map[string][]col{"vr_r": {{"role", "int", true}}},
		},
		{
			name:      "mysql/drop a column an IN subquery's ORDER BY alias shadows",
			base:      mysqlReads("SELECT u.id FROM vr_users u WHERE u.id IN (SELECT length(p.title) AS role FROM vr_posts p ORDER BY role)"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			want:      map[string][]col{"vr_r": {{"id", "int", false}}},
		},
		{
			// GROUP BY role is vr_users.role and HAVING role the alias.
			name:      "mysql/drop a column a GROUP BY name reads before a select-list alias",
			base:      mysqlReads("SELECT count(*) AS role FROM vr_users GROUP BY role HAVING role > 0"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			wantErr:   "merge both mode: view vr_r reads column role of table vr_users" + stands,
		},
		// A LATERAL derived table can read the FROM clause of the query it
		// is in, and so can a derived table inside it.
		{
			// PostgreSQL follows the rename.
			name:      "postgres/rename a column a LATERAL derived table reads",
			base:      pgReads("SELECT u.id, l.t FROM users u, LATERAL (SELECT p.title AS t FROM posts p WHERE p.uid = u.id AND role = 'a') l"),
			parser:    newPostgres,
			migration: "ALTER TABLE users RENAME COLUMN role TO kind;",
			want:      map[string][]col{"r": {{"id", "integer", false}, {"t", "text", true}}},
		},
		{
			name:      "mysql/drop a column a LATERAL derived table reads",
			base:      mysqlReads("SELECT u.id, l.t FROM vr_users u, LATERAL (SELECT p.title AS t FROM vr_posts p WHERE p.uid = u.id AND role = 'a') l"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			wantErr:   "merge both mode: view vr_r reads column role of table vr_users" + stands,
		},
		{
			name:      "mysql/drop a column a derived table inside a LATERAL one reads",
			base:      mysqlReads("SELECT u.id, l.uid FROM vr_users u, LATERAL (SELECT x.uid FROM (SELECT p.uid FROM vr_posts p WHERE p.uid = u.id AND u.role = 'a') x) l"),
			parser:    newMySQL,
			migration: "ALTER TABLE vr_users DROP COLUMN role;",
			wantErr:   "merge both mode: view vr_r reads column role of table vr_users" + stands,
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
