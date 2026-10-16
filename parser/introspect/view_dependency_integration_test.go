package introspect_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/parser/introspect"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// oracleMigration is a pending migration the oracle tests run through Merge
// and against the database.
type oracleMigration struct {
	name, sql string
}

// oracleShape is a database view (r, and any view it reads) the oracle tests
// check every migration against. limits names the migrations on which Merge
// and the database are known to disagree, with the reason (PRD §6.1).
type oracleShape struct {
	name, ddl string
	// migrations are run on this shape in addition to the shared ones.
	migrations []oracleMigration
	// tables, when set, is the introspection's table filter, which leaves
	// the shape's views out.
	tables []string
	limits map[string]string
}

// compareVerdicts reports a migration on which Merge and the database
// disagree, other than a known limit, and a known limit on which they now
// agree.
func compareVerdicts(t *testing.T, shape oracleShape, m oracleMigration, dbVerdict string, mergeErr error) {
	t.Helper()
	disagree := (dbVerdict != "") != (mergeErr != nil)
	limit, isLimit := shape.limits[m.name]
	switch {
	case disagree && !isLimit:
		t.Errorf("%s: Merge and the database disagree:\n  Merge: %v\n  database: %q", m.name, mergeErr, dbVerdict)
	case !disagree && isLimit:
		t.Errorf("%s: known limit (%s) no longer disagrees: Merge %v, database %q", m.name, limit, mergeErr, dbVerdict)
	}
}

// TestMerge_ViewDependenciesMatchPostgres runs each pending migration on a
// corpus of PostgreSQL views through Merge and through the database, and
// checks that Merge refuses exactly the migrations PostgreSQL refuses or
// that leave a view failing (PRD §6.1).
func TestMerge_ViewDependenciesMatchPostgres(t *testing.T) {
	requireDatabases(t)
	migrations := []oracleMigration{
		{"drop users.role", "ALTER TABLE users DROP COLUMN role;"},
		{"drop users.email", "ALTER TABLE users DROP COLUMN email;"},
		{"drop users.n", "ALTER TABLE users DROP COLUMN n;"},
		{"drop posts.title", "ALTER TABLE posts DROP COLUMN title;"},
		{"drop posts.uid", "ALTER TABLE posts DROP COLUMN uid;"},
		{"drop posts.email", "ALTER TABLE posts DROP COLUMN email;"},
		{"drop tags.uid", "ALTER TABLE tags DROP COLUMN uid;"},
		{"retype users.role", "ALTER TABLE users ALTER COLUMN role TYPE varchar(40);"},
		{"retype users.role to its type", "ALTER TABLE users ALTER COLUMN role TYPE text;"},
		{"retype posts.uid", "ALTER TABLE posts ALTER COLUMN uid TYPE bigint;"},
		{"rename users.role", "ALTER TABLE users RENAME COLUMN role TO role2;"},
		{"rename tags", "ALTER TABLE tags RENAME TO labels;"},
		{"move tags", "ALTER TABLE tags SET SCHEMA {schema}x;"},
		{"drop tags", "DROP TABLE tags;"},
		{"drop posts", "DROP TABLE posts;"},
		{"drop users", "DROP TABLE users;"},
		{"drop and create tags", "DROP TABLE tags; CREATE TABLE tags (id int PRIMARY KEY, uid int);"},
		{"rename and drop users.role", "ALTER TABLE users RENAME COLUMN role TO r2; ALTER TABLE users DROP COLUMN r2;"},
		{"rename users and drop role", "ALTER TABLE users RENAME TO people; ALTER TABLE people DROP COLUMN role;"},
		{"rename and drop tags", "ALTER TABLE tags RENAME TO labels; DROP TABLE labels;"},
		{"drop type role_t", "ALTER TABLE users DROP COLUMN kind; DROP TYPE role_t;"},
		{"rename and drop type role_t", "ALTER TABLE users DROP COLUMN kind; ALTER TYPE role_t RENAME TO kind_t; DROP TYPE kind_t;"},
		{"drop domain email_d", "ALTER TABLE users DROP COLUMN mail; DROP DOMAIN email_d;"},
		{"drop attribute addr_t.city", "ALTER TYPE addr_t DROP ATTRIBUTE city;"},
		{"drop users_pkey", "ALTER TABLE users DROP CONSTRAINT users_pkey;"},
		{"rename and drop users_pkey", "ALTER TABLE users RENAME CONSTRAINT users_pkey TO users_key; ALTER TABLE users DROP CONSTRAINT users_key;"},
	}
	shapes := []oracleShape{
		{name: "passthrough", ddl: "CREATE VIEW r AS SELECT id, role FROM users"},
		{name: "alias", ddl: "CREATE VIEW r AS SELECT u.id, u.role AS r FROM users u"},
		{name: "compound", ddl: "CREATE VIEW r AS SELECT role FROM users UNION SELECT title FROM posts"},
		{name: "derived", ddl: "CREATE VIEW r AS SELECT r FROM (SELECT role AS r FROM users) d"},
		{name: "CTE chain", ddl: "CREATE VIEW r AS WITH a AS (SELECT role FROM users), c AS (SELECT role AS r FROM a) SELECT r FROM c"},
		{name: "CTE named after its table", ddl: "CREATE VIEW r AS WITH users AS (SELECT email AS role FROM users) SELECT role FROM users"},
		{name: "table beside a CTE of its name", ddl: "CREATE VIEW r AS WITH users AS (SELECT email AS role FROM users) SELECT u2.role FROM users JOIN {schema}.users u2 ON true"},
		{name: "WHERE", ddl: "CREATE VIEW r AS SELECT id FROM users WHERE role = 'a'"},
		{name: "WHERE in a CTE", ddl: "CREATE VIEW r AS WITH c AS (SELECT id FROM users WHERE role = 'a') SELECT id FROM c"},
		{name: "expression in a derived table", ddl: "CREATE VIEW r AS SELECT u FROM (SELECT upper(role) AS u FROM users) d"},
		{name: "USING through a join", ddl: "CREATE VIEW r AS SELECT p.id FROM users u JOIN tags t ON t.uid = u.id JOIN posts p USING (email)"},
		{name: "function in FROM", ddl: "CREATE VIEW r AS SELECT u.id FROM users u, unnest(string_to_array(u.email, ',')) x"},
		{name: "derived table in EXISTS", ddl: "CREATE VIEW r AS SELECT id FROM users u WHERE EXISTS (SELECT 1 FROM (SELECT p.uid FROM posts p WHERE p.uid = u.id AND u.role = 'a') d)"},
		{name: "derived table in a LATERAL one", ddl: "CREATE VIEW r AS SELECT u.id, l.uid FROM users u, LATERAL (SELECT x.uid FROM (SELECT p.uid FROM posts p WHERE p.uid = u.id AND u.role = 'a') x) l"},
		{name: "CTE in EXISTS reads the outer query", ddl: "CREATE VIEW r AS SELECT id FROM users u WHERE EXISTS (WITH c AS (SELECT p.uid FROM posts p WHERE p.uid = u.id AND u.role = 'a') SELECT 1 FROM c)"},
		{name: "GROUP BY a name an alias has", ddl: "CREATE VIEW r AS SELECT max(id) AS role FROM users GROUP BY role"},
		{name: "JOIN ON", ddl: "CREATE VIEW r AS SELECT u.id FROM users u JOIN posts p ON p.uid = u.id AND p.title = u.role"},
		{name: "USING", ddl: "CREATE VIEW r AS SELECT users.id FROM users JOIN posts USING (email)"},
		{name: "LATERAL", ddl: "CREATE VIEW r AS SELECT u.id, l.title FROM users u, LATERAL (SELECT p.title FROM posts p WHERE p.uid = u.id AND u.role = 'a') l"},
		{name: "correlated EXISTS", ddl: "CREATE VIEW r AS SELECT id FROM users u WHERE EXISTS (SELECT 1 FROM posts p WHERE p.uid = u.id AND u.role = 'a')"},
		{name: "IN subquery", ddl: "CREATE VIEW r AS SELECT id FROM users WHERE id IN (SELECT uid FROM posts)"},
		{name: "scalar subquery", ddl: "CREATE VIEW r AS SELECT u.id, (SELECT max(p.title) FROM posts p WHERE p.uid = u.id) AS t FROM users u"},
		{name: "IN UNION", ddl: "CREATE VIEW r AS SELECT u.id FROM users u WHERE u.id IN (SELECT uid FROM posts UNION SELECT uid FROM tags)"},
		{name: "IN INTERSECT", ddl: "CREATE VIEW r AS SELECT u.id FROM users u WHERE u.id IN (SELECT uid FROM posts INTERSECT SELECT uid FROM tags)"},
		{name: "derived UNION", ddl: "CREATE VIEW r AS SELECT x FROM (SELECT role AS x FROM users UNION SELECT title FROM posts) d"},
		{name: "CTE UNION ALL", ddl: "CREATE VIEW r AS WITH c AS (SELECT uid FROM posts UNION ALL SELECT uid FROM tags) SELECT u.id FROM users u JOIN c ON c.uid = u.id"},
		{name: "recursive CTE", ddl: "CREATE VIEW r AS WITH RECURSIVE c(n, e) AS (SELECT id, email FROM users UNION ALL SELECT n + 1, e FROM c WHERE n < 3) SELECT n FROM c"},
		{name: "LATERAL VALUES", ddl: "CREATE VIEW r AS SELECT u.id, v.x FROM users u CROSS JOIN LATERAL (VALUES (u.role)) v(x)"},
		{name: "IN VALUES", ddl: "CREATE VIEW r AS SELECT id FROM users u WHERE 'a' IN (VALUES (u.role))"},
		{name: "LIMIT subquery", ddl: "CREATE VIEW r AS SELECT u.id FROM users u LIMIT (SELECT count(*) FROM tags)"},
		{name: "OFFSET subquery", ddl: "CREATE VIEW r AS SELECT u.id FROM users u OFFSET (SELECT count(uid) FROM tags)"},
		{name: "GROUP BY and HAVING", ddl: "CREATE VIEW r AS SELECT role, count(*) AS c FROM users GROUP BY role HAVING sum(n) > 0"},
		{name: "window", ddl: "CREATE VIEW r AS SELECT id, row_number() OVER (PARTITION BY role ORDER BY email) AS rn FROM users"},
		{name: "DISTINCT ON", ddl: "CREATE VIEW r AS SELECT DISTINCT ON (role) id, role FROM users ORDER BY role, id"},
		{name: "CASE", ddl: "CREATE VIEW r AS SELECT id, CASE WHEN role = 'a' THEN email END AS e FROM users"},
		{name: "unused CTE", ddl: "CREATE VIEW r AS WITH c AS (SELECT email FROM users) SELECT id FROM users"},
		{name: "EXISTS over a view", ddl: "CREATE VIEW iv AS SELECT uid, title AS role FROM posts; CREATE VIEW r AS SELECT u.id FROM users u WHERE EXISTS (SELECT 1 FROM iv WHERE iv.role = 'a')"},
		{name: "view over a view", ddl: "CREATE VIEW iv AS SELECT id, role FROM users; CREATE VIEW r AS SELECT role FROM iv"},
		{name: "select star", ddl: "CREATE VIEW r AS SELECT * FROM users"},
		{name: "whole row", ddl: "CREATE VIEW r AS SELECT row_to_json(u) AS j FROM users u"},
		{name: "count star", ddl: "CREATE VIEW r AS SELECT count(*) AS c FROM users"},
		{name: "enum cast", ddl: "CREATE VIEW r AS SELECT id FROM users WHERE role = ('a'::role_t)::text"},
		{name: "domain cast", ddl: "CREATE VIEW r AS SELECT id, email::email_d AS e FROM users"},
		{name: "composite attribute", ddl: "CREATE VIEW r AS SELECT id, (addr).city AS city FROM users"},
		{name: "GROUP BY a primary key", ddl: "CREATE VIEW r AS SELECT u.id, u.role, count(p.id) AS c FROM users u LEFT JOIN posts p ON p.uid = u.id GROUP BY u.id"},
		{name: "jsonb_each", ddl: "CREATE VIEW r AS SELECT u.id FROM users u, jsonb_each(u.data) j WHERE j.key = u.role"},
		{name: "regclass", ddl: "CREATE VIEW r AS SELECT 'tags'::regclass::text AS t"},
		{name: "materialized", ddl: "CREATE MATERIALIZED VIEW r AS SELECT id, role FROM users"},
		{
			name: "partition",
			ddl:  "CREATE TABLE ev (id int, k int) PARTITION BY LIST (k); CREATE TABLE ev1 PARTITION OF ev FOR VALUES IN (1); CREATE VIEW r AS SELECT id FROM ev1",
			migrations: []oracleMigration{
				{"drop the partition", "DROP TABLE ev1;"},
				{"drop the partitioned table", "DROP TABLE ev;"},
				{"drop a partition's column", "ALTER TABLE ev DROP COLUMN id;"},
			},
		},
		{name: "another schema's view", ddl: "CREATE VIEW {schema}x.ov AS SELECT id, role FROM users"},
		{name: "filtered out", ddl: "CREATE VIEW r AS SELECT id, role FROM users", tables: []string{"users", "posts", "tags"}},
		{
			name: "BEGIN ATOMIC function",
			ddl:  "CREATE FUNCTION g(i int) RETURNS text LANGUAGE sql BEGIN ATOMIC SELECT max(title) FROM posts WHERE uid = i; END; CREATE VIEW r AS SELECT id, g(id) AS t FROM users",
		},
		{
			name: "string-body function",
			ddl:  "CREATE FUNCTION f() RETURNS text LANGUAGE sql AS 'SELECT max(email) FROM users'; CREATE VIEW r AS SELECT f() AS e",
			// PostgreSQL records nothing the body reads, so it accepts the
			// drop or rename, and the view fails when it runs.
			limits: map[string]string{
				"drop users.email":           "a string-body function",
				"drop users":                 "a string-body function",
				"rename users and drop role": "a string-body function",
			},
		},
	}
	// The shapes run one at a time: PostgreSQL can deadlock DDL on two
	// schemas that run at once.
	for i, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			schema := fmt.Sprintf("view_dep%d", i)
			conn := pgConnStr + "&search_path=" + schema
			reset := fmt.Sprintf("DROP SCHEMA IF EXISTS %[1]s CASCADE; DROP SCHEMA IF EXISTS %[1]sx CASCADE", schema)
			execAll(t, "pgx", conn, reset+fmt.Sprintf(`; CREATE SCHEMA %[1]s; CREATE SCHEMA %[1]sx;
				CREATE TYPE role_t AS ENUM ('a', 'b');
				CREATE DOMAIN email_d AS text;
				CREATE TYPE addr_t AS (street text, city text);
				CREATE TABLE users (id int PRIMARY KEY, role text NOT NULL, email text, kind role_t, mail email_d, addr addr_t, data jsonb, n int);
				CREATE TABLE posts (id int PRIMARY KEY, uid int, title text, email text);
				CREATE TABLE tags (id int PRIMARY KEY, uid int)`, schema))
			execScript(t, "pgx", conn, strings.ReplaceAll(shape.ddl, "{schema}", schema))
			t.Cleanup(func() { execAll(t, "pgx", conn, reset) })
			for _, m := range append(migrations, shape.migrations...) {
				m.sql = strings.ReplaceAll(m.sql, "{schema}", schema)
				verdict := postgresVerdict(t, conn, schema, m.sql)
				base := introspectWith(t, introspect.NewPostgresIntrospector(), conn, introspect.IntrospectionOptions{Schemas: []string{schema}, Tables: shape.tables})
				err := introspect.Merge(base, postgres.New(schema), []string{writeMigration(t, m.sql)})
				compareVerdicts(t, shape, m, verdict, err)
			}
		})
	}
}

// postgresVerdict runs migration on PostgreSQL in a transaction it rolls
// back, and returns why the database refuses it or which view then fails,
// or "" when it accepts it and every view in schema and its x schema still
// runs.
func postgresVerdict(t *testing.T, conn, schema, migration string) string {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("pgx", conn)
	if err != nil {
		t.Fatalf("opening postgres: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck // the migration is always undone
	for _, stmt := range splitStatements(migration) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return "refused: " + err.Error()
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT quote_ident(n.nspname) || '.' || quote_ident(c.relname)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('v', 'm') AND n.nspname IN ($1, $2) ORDER BY 1`, schema, schema+"x")
	if err != nil {
		t.Fatalf("listing views: %v", err)
	}
	var views []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("listing views: %v", err)
		}
		views = append(views, v)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("listing views: %v", err)
	}
	for _, v := range views {
		// Run the view, not just plan it: a string-body SQL function is
		// read only when it is called.
		if _, err := tx.ExecContext(ctx, "SELECT * FROM "+v); err != nil { //nolint:gosec // a name quote_ident quoted
			return "view " + v + " fails: " + err.Error()
		}
	}
	return ""
}

// execScript runs ddl on the database as one statement list, so a
// statement can hold semicolons of its own (a function or trigger body).
func execScript(t *testing.T, driver, conn, ddl string) {
	t.Helper()
	db, err := sql.Open(driver, conn)
	if err != nil {
		t.Fatalf("opening %s: %v", driver, err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	if _, err := db.ExecContext(context.Background(), ddl); err != nil {
		t.Fatalf("applying %q: %v", truncate(ddl), err)
	}
}

// TestMerge_ViewDependenciesMatchSQLite runs each pending migration on a
// corpus of SQLite views and triggers through Merge and through the
// database file, and checks that Merge refuses exactly the migrations
// SQLite refuses or that leave a view that ran failing (PRD §6.1).
func TestMerge_ViewDependenciesMatchSQLite(t *testing.T) {
	requireDatabases(t)
	migrations := []oracleMigration{
		{"drop users.role", "ALTER TABLE users DROP COLUMN role;"},
		{"drop users.email", "ALTER TABLE users DROP COLUMN email;"},
		{"drop users.value", "ALTER TABLE users DROP COLUMN value;"},
		{"drop posts.title", "ALTER TABLE posts DROP COLUMN title;"},
		{"drop posts.uid", "ALTER TABLE posts DROP COLUMN uid;"},
		{"drop posts.email", "ALTER TABLE posts DROP COLUMN email;"},
		{"drop tags.uid", "ALTER TABLE tags DROP COLUMN uid;"},
		{"rename users.role", "ALTER TABLE users RENAME COLUMN role TO kind;"},
		{"rename users.email", "ALTER TABLE users RENAME COLUMN email TO mail;"},
		{"rename posts.title", "ALTER TABLE posts RENAME COLUMN title TO headline;"},
		{"rename tags", "ALTER TABLE tags RENAME TO labels;"},
		{"rename posts", "ALTER TABLE posts RENAME TO articles;"},
		{"rename tags under legacy_alter_table", "PRAGMA legacy_alter_table = ON; ALTER TABLE tags RENAME TO labels;"},
		{"drop tags", "DROP TABLE tags;"},
		{"drop posts", "DROP TABLE posts;"},
		{"drop users", "DROP TABLE users;"},
		{"drop and create tags", "DROP TABLE tags; CREATE TABLE tags (id INTEGER PRIMARY KEY NOT NULL, uid INTEGER);"},
		{"rename and drop users.role", "ALTER TABLE users RENAME COLUMN role TO r2; ALTER TABLE users DROP COLUMN r2;"},
	}
	shapes := []oracleShape{
		{name: "passthrough", ddl: "CREATE VIEW r AS SELECT id, role FROM users"},
		{name: "alias", ddl: "CREATE VIEW r AS SELECT u.id, u.role AS r FROM users u"},
		{name: "column list", ddl: "CREATE VIEW r (a, b) AS SELECT id, role FROM users"},
		{name: "compound", ddl: "CREATE VIEW r AS SELECT role FROM users UNION SELECT title FROM posts"},
		{name: "derived", ddl: "CREATE VIEW r AS SELECT r FROM (SELECT role AS r FROM users) d"},
		{name: "unaliased derived", ddl: "CREATE VIEW r AS SELECT role FROM (SELECT role FROM users) d"},
		{name: "derived selecting star", ddl: "CREATE VIEW r AS SELECT role FROM (SELECT * FROM users) d"},
		{name: "derived named after its table", ddl: "CREATE VIEW r AS SELECT users.role FROM (SELECT email AS role FROM users) users"},
		{name: "unaliased derived named after its table", ddl: "CREATE VIEW r AS SELECT users.role FROM (SELECT role FROM users WHERE id > 0) users"},
		{name: "WHERE through an unaliased derived table", ddl: "CREATE VIEW r AS SELECT id FROM (SELECT id, role FROM users) d WHERE d.role = 'a'"},
		{name: "JOIN ON in a derived table", ddl: "CREATE VIEW r AS SELECT id FROM (SELECT p.id FROM posts p JOIN users u ON u.role = p.title) d"},
		{name: "CTE", ddl: "CREATE VIEW r AS WITH c AS (SELECT role FROM users) SELECT role FROM c"},
		{name: "CTE column list", ddl: "CREATE VIEW r AS WITH c (r) AS (SELECT role FROM users) SELECT r FROM c"},
		{name: "CTE named after its table", ddl: "CREATE VIEW r AS WITH users AS (SELECT email AS role FROM main.users) SELECT role FROM users"},
		{name: "expression", ddl: "CREATE VIEW r AS SELECT upper(role) AS u FROM users"},
		{name: "IN subquery", ddl: "CREATE VIEW r AS SELECT id FROM users WHERE id IN (SELECT uid FROM posts)"},
		{name: "USING through a join", ddl: "CREATE VIEW r AS SELECT p.id FROM users u JOIN tags t ON t.uid = u.id JOIN posts p USING (email)"},
		{name: "derived table in EXISTS", ddl: "CREATE VIEW r AS SELECT id FROM users u WHERE EXISTS (SELECT 1 FROM (SELECT p.uid FROM posts p WHERE p.uid = u.id AND u.role = 'a') d)"},
		{name: "table function", ddl: "CREATE VIEW r AS SELECT u.id FROM users u, json_each(u.email) j"},
		{name: "derived table reads a name the outer query has", ddl: "CREATE VIEW r AS SELECT u.id FROM users u JOIN (SELECT uid FROM posts WHERE email <> '') d ON d.uid = u.id"},
		{name: "GROUP BY a scalar alias", ddl: "CREATE VIEW r AS SELECT upper(email) AS role FROM users GROUP BY role"},
		{name: "GROUP BY an aggregate alias", ddl: "CREATE VIEW r AS SELECT max(id) AS role FROM users GROUP BY role"},
		{name: "GROUP BY an outer aggregate's alias", ddl: "CREATE VIEW r AS SELECT (SELECT max(u.id)) AS role FROM users u GROUP BY role"},
		{name: "aggregate alias inside an aggregate", ddl: "CREATE VIEW r AS SELECT max(id) AS role FROM users GROUP BY email HAVING count(role) > 0"},
		{name: "two same-named aliases", ddl: "CREATE VIEW r AS SELECT id FROM users u WHERE EXISTS (SELECT max(u2.id) AS role, upper(u2.email) AS role FROM users u2 GROUP BY u2.email HAVING count(role) > 0)"},
		{name: "HAVING window alias", ddl: "CREATE VIEW r AS SELECT row_number() OVER () AS role FROM users GROUP BY email HAVING role > 0"},
		{name: "WHERE", ddl: "CREATE VIEW r AS SELECT id FROM users WHERE role = 'a'"},
		{name: "JOIN ON", ddl: "CREATE VIEW r AS SELECT u.id FROM users u JOIN posts p ON p.uid = u.id AND p.title = u.role"},
		{name: "USING", ddl: "CREATE VIEW r AS SELECT users.id FROM users JOIN posts USING (email)"},
		{name: "correlated EXISTS", ddl: "CREATE VIEW r AS SELECT id FROM users u WHERE EXISTS (SELECT 1 FROM posts p WHERE p.uid = u.id AND u.role = 'a')"},
		{name: "scalar subquery", ddl: "CREATE VIEW r AS SELECT u.id, (SELECT max(p.title) FROM posts p WHERE p.uid = u.id) AS t FROM users u"},
		{name: "IN UNION", ddl: "CREATE VIEW r AS SELECT u.id FROM users u WHERE u.id IN (SELECT uid FROM posts UNION SELECT uid FROM tags)"},
		{name: "derived UNION", ddl: "CREATE VIEW r AS SELECT x FROM (SELECT role AS x FROM users UNION SELECT title FROM posts) d"},
		{name: "CTE UNION ALL", ddl: "CREATE VIEW r AS WITH c AS (SELECT uid FROM posts UNION ALL SELECT uid FROM tags) SELECT u.id FROM users u JOIN c ON c.uid = u.id"},
		{name: "recursive CTE", ddl: "CREATE VIEW r AS WITH RECURSIVE c(n, e) AS (SELECT id, email FROM users UNION ALL SELECT n + 1, e FROM c WHERE n < 3) SELECT n FROM c"},
		{name: "EXISTS VALUES", ddl: "CREATE VIEW r AS SELECT u.id FROM users u WHERE EXISTS (VALUES (u.role))"},
		{name: "LIMIT subquery", ddl: "CREATE VIEW r AS SELECT u.id FROM users u LIMIT (SELECT count(*) FROM tags)"},
		{name: "OFFSET subquery", ddl: "CREATE VIEW r AS SELECT u.id FROM users u LIMIT 5 OFFSET (SELECT count(uid) FROM tags)"},
		{name: "CTE bound where it is used", ddl: "CREATE VIEW r AS WITH c AS (SELECT uid FROM posts WHERE role = 'a') SELECT u.id FROM users u WHERE EXISTS (SELECT 1 FROM c WHERE c.uid = u.id)"},
		{name: "CTE inside EXISTS", ddl: "CREATE VIEW r AS SELECT u.id FROM users u WHERE EXISTS (WITH c AS (SELECT p.uid FROM posts p WHERE u.role = 'a') SELECT 1 FROM c)"},
		{name: "EXISTS over a view", ddl: "CREATE VIEW iv AS SELECT uid, title AS role FROM posts; CREATE VIEW r AS SELECT u.id FROM users u WHERE EXISTS (SELECT 1 FROM iv WHERE role = 'a')"},
		{name: "IN over a view", ddl: "CREATE VIEW iv AS SELECT uid, title AS role FROM posts; CREATE VIEW r AS SELECT u.id FROM users u WHERE u.id IN (SELECT uid FROM iv WHERE role = 'a')"},
		{name: "EXISTS over json_each", ddl: `CREATE VIEW r AS SELECT u.id FROM users u WHERE EXISTS (SELECT 1 FROM json_each('["a"]') WHERE value = 'a')`},
		{name: "HAVING EXISTS reads an outer alias", ddl: "CREATE VIEW r AS SELECT email, max(id) AS role FROM users GROUP BY email HAVING EXISTS (SELECT 1 FROM posts p WHERE p.title = role)"},
		{name: "GROUP BY alias", ddl: "CREATE VIEW r AS SELECT max(id) AS role FROM users GROUP BY email HAVING role > 0"},
		{name: "ORDER BY alias", ddl: "CREATE VIEW r AS SELECT id, upper(email) AS role FROM users ORDER BY role"},
		{name: "derived table in EXISTS reads a name the outer query has", ddl: "CREATE VIEW r AS SELECT id FROM users u WHERE EXISTS (SELECT 1 FROM (SELECT uid FROM posts WHERE email <> '') d)"},
		{name: "EXISTS reads a name the outer query has", ddl: "CREATE VIEW r AS SELECT id FROM users u WHERE EXISTS (SELECT 1 FROM posts WHERE email <> '')"},
		{name: "view over a view", ddl: "CREATE VIEW iv AS SELECT id, role FROM users; CREATE VIEW r AS SELECT role FROM iv"},
		{name: "select star", ddl: "CREATE VIEW r AS SELECT * FROM users"},
		{name: "count star", ddl: "CREATE VIEW r AS SELECT count(*) AS c FROM users"},
		{name: "window", ddl: "CREATE VIEW r AS SELECT id, row_number() OVER (PARTITION BY role ORDER BY email) AS rn FROM users"},
		{name: "CASE", ddl: "CREATE VIEW r AS SELECT id, CASE WHEN role = 'a' THEN email END AS e FROM users"},
		{name: "filtered out", ddl: "CREATE VIEW r AS SELECT id, role FROM users", tables: []string{"users", "posts", "tags"}},
		{name: "FTS5 table", ddl: "CREATE VIRTUAL TABLE docs USING fts5(body); CREATE VIEW r AS SELECT d.body, u.role FROM docs d, users u"},
		{name: "trigger on the table", ddl: "CREATE TRIGGER tr AFTER UPDATE ON users BEGIN UPDATE posts SET title = NEW.role; END"},
		{
			name: "trigger on another table",
			ddl:  "CREATE TRIGGER tr AFTER INSERT ON posts BEGIN UPDATE users SET role = 'x' WHERE id = NEW.uid; END",
		},
		{
			name: "duplicate rows",
			ddl:  "INSERT INTO users (id, role, email) VALUES (1, 'a', 'x'), (2, 'b', 'x')",
			migrations: []oracleMigration{
				{"unique index on users.email", "CREATE UNIQUE INDEX users_email ON users (email);"},
			},
			limits: map[string]string{"unique index on users.email": "the copy has no rows"},
		},
		{
			name: "generated column",
			ddl:  "CREATE TABLE g (a INTEGER, c INTEGER, b INTEGER GENERATED ALWAYS AS (a * 2))",
			migrations: []oracleMigration{
				{"drop g.a", "ALTER TABLE g DROP COLUMN a;"},
				{"rename g.a", "ALTER TABLE g RENAME COLUMN a TO c;"},
			},
		},
		// The database's objects below name a collation or function only
		// the application registers, as the tool that runs the migrations
		// would; neither SQLite here has it, so each is written into
		// sqlite_master over an object SQLite accepts.
		{
			name: "a collation SQLite lacks, and a view comparing that column",
			ddl: withSchemaSQL("users", "CREATE TABLE users (id INTEGER PRIMARY KEY NOT NULL, role TEXT NOT NULL, email TEXT COLLATE nosuchcoll, value TEXT UNIQUE COLLATE nosuchcoll)") +
				"CREATE VIEW r AS SELECT id, email FROM users WHERE email = 'x'",
		},
		{
			name: "a collation SQLite lacks, and a view reading that column",
			ddl: withSchemaSQL("users", "CREATE TABLE users (id INTEGER PRIMARY KEY NOT NULL, role TEXT NOT NULL, email TEXT COLLATE nosuchcoll, value TEXT)") +
				"CREATE VIEW r AS SELECT id, email, role FROM users",
		},
		{
			name: "an index on a column with a collation SQLite lacks",
			ddl: "CREATE INDEX users_email ON users (email);" +
				withSchemaSQL("users", "CREATE TABLE users (id INTEGER PRIMARY KEY NOT NULL, role TEXT NOT NULL, email TEXT COLLATE nosuchcoll, value TEXT)") +
				"CREATE VIEW r AS SELECT id, role FROM users",
		},
		{
			name: "an index on a function SQLite lacks",
			ddl: "CREATE INDEX ix ON users (email);" + withSchemaSQL("ix", "CREATE INDEX ix ON users (myfn(email)) WHERE myfn(role) IS NOT NULL") +
				"CREATE VIEW r AS SELECT id, email FROM users",
			migrations: []oracleMigration{
				{"insert into users", "INSERT INTO users (id, role, email) VALUES (1, 'a', 'x');"},
			},
		},
		{
			name: "a CHECK on a function SQLite lacks",
			ddl: withSchemaSQL("posts", "CREATE TABLE posts (id INTEGER PRIMARY KEY NOT NULL, uid INTEGER, title TEXT, email TEXT, CHECK (myfn(title)))") +
				"CREATE VIEW r AS SELECT p.title, u.role FROM posts p JOIN users u ON u.id = p.uid",
		},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			dbPath := filepath.Join(t.TempDir(), "schema.db")
			execScript(t, "sqlite", dbPath, `CREATE TABLE users (id INTEGER PRIMARY KEY NOT NULL, role TEXT NOT NULL, email TEXT, value TEXT);
				CREATE TABLE posts (id INTEGER PRIMARY KEY NOT NULL, uid INTEGER, title TEXT, email TEXT);
				CREATE TABLE tags (id INTEGER PRIMARY KEY NOT NULL, uid INTEGER);`+shape.ddl)
			for _, m := range append(migrations, shape.migrations...) {
				verdict := sqliteVerdict(t, dbPath, m.sql)
				base := introspectSQLite(t, dbPath, introspect.IntrospectionOptions{Tables: shape.tables})
				p := sqlite.New()
				err := introspect.Merge(base, p, []string{writeMigration(t, m.sql)})
				if missing := sqliteMissing.FindStringSubmatch(verdict); missing != nil {
					// The database lacks what the migration needs, as Merge's
					// copy does; Merge warns that it cannot check the file.
					if want := "needs " + missing[1] + " " + missing[2] + ","; err != nil || !slices.ContainsFunc(p.Schema().Warnings, func(w string) bool { return strings.Contains(w, want) }) {
						t.Errorf("%s: database %q, so Merge should warn %q: Merge %v, warnings %q", m.name, verdict, want, err, p.Schema().Warnings)
					}
					continue
				}
				compareVerdicts(t, shape, m, verdict, err)
			}
		})
	}
}

// sqliteMissing matches the database's refusal of a migration that needs a
// collation or function it lacks, with the kind and name Merge's warning
// gives.
var sqliteMissing = regexp.MustCompile(`(?:no such |unknown )(collation|function)(?: sequence)?: ([^ (]+)`)

// withSchemaSQL is a script that writes ddl over the stored statement of
// the schema object name, as a database whose application registers a
// collation or function would store it.
func withSchemaSQL(name, ddl string) string {
	return fmt.Sprintf("PRAGMA writable_schema = ON; UPDATE sqlite_master SET sql = '%s' WHERE name = '%s'; PRAGMA writable_schema = RESET;",
		strings.ReplaceAll(ddl, "'", "''"), name)
}

// sqliteVerdict runs migration on a copy of the database file, and returns
// why SQLite refuses it or which view that ran before then fails, or ""
// when it accepts it and every such view still runs.
func sqliteVerdict(t *testing.T, dbPath, migration string) string {
	t.Helper()
	data, err := os.ReadFile(dbPath) //nolint:gosec // the test's own temporary file
	if err != nil {
		t.Fatalf("reading %s: %v", dbPath, err)
	}
	cp := filepath.Join(t.TempDir(), "copy.db")
	if err := os.WriteFile(cp, data, 0o600); err != nil { //nolint:gosec // the test's own temporary file
		t.Fatalf("copying %s: %v", dbPath, err)
	}
	ctx := context.Background()
	db, err := sql.Open("sqlite", cp)
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	views := sqliteRunningViews(t, db)
	if _, err := db.ExecContext(ctx, migration); err != nil {
		return "refused: " + err.Error()
	}
	for _, v := range views {
		if _, err := db.ExecContext(ctx, "SELECT * FROM "+v+" LIMIT 0"); err != nil { //nolint:gosec // a view name the database listed
			return "view " + v + " fails: " + err.Error()
		}
	}
	return ""
}

// sqliteRunningViews returns the views of db that run.
func sqliteRunningViews(t *testing.T, db *sql.DB) []string {
	t.Helper()
	ctx := context.Background()
	rows, err := db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'view' ORDER BY name")
	if err != nil {
		t.Fatalf("listing views: %v", err)
	}
	var names []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("listing views: %v", err)
		}
		names = append(names, v)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("listing views: %v", err)
	}
	var views []string
	for _, v := range names {
		if _, err := db.ExecContext(ctx, "SELECT * FROM "+v+" LIMIT 0"); err == nil { //nolint:gosec // a view name the database listed
			views = append(views, v)
		}
	}
	return views
}

// TestMerge_ViewTablesMatchMySQL runs each pending migration that drops or
// renames a table on a corpus of MySQL views through Merge and through the
// database, and checks that Merge refuses exactly the migrations that leave
// a view failing (PRD §6.1). MySQL records only the tables a view reads
// (VIEW_TABLE_USAGE), so its column checks are not compared here.
func TestMerge_ViewTablesMatchMySQL(t *testing.T) {
	requireDatabases(t)
	migrations := []oracleMigration{
		{"drop tags", "DROP TABLE vo_tags;"},
		{"rename tags", "RENAME TABLE vo_tags TO vo_labels;"},
		{"alter rename tags", "ALTER TABLE vo_tags RENAME TO vo_labels;"},
		{"drop posts", "DROP TABLE vo_posts;"},
		{"drop and create tags", "DROP TABLE vo_tags; CREATE TABLE vo_tags (id int PRIMARY KEY, uid int);"},
	}
	shapes := []oracleShape{
		{name: "passthrough", ddl: "CREATE VIEW vo_r AS SELECT uid FROM vo_tags"},
		{name: "IN UNION", ddl: "CREATE VIEW vo_r AS SELECT u.id FROM vo_users u WHERE u.id IN (SELECT uid FROM vo_posts UNION SELECT uid FROM vo_tags)"},
		{name: "derived UNION", ddl: "CREATE VIEW vo_r AS SELECT x FROM (SELECT id AS x FROM vo_users UNION SELECT uid FROM vo_tags) d"},
		{name: "CTE UNION ALL", ddl: "CREATE VIEW vo_r AS WITH c AS (SELECT uid FROM vo_posts UNION ALL SELECT uid FROM vo_tags) SELECT u.id FROM vo_users u JOIN c ON c.uid = u.id"},
		{name: "EXISTS", ddl: "CREATE VIEW vo_r AS SELECT u.id FROM vo_users u WHERE EXISTS (SELECT 1 FROM vo_tags t WHERE t.uid = u.id)"},
		{name: "compound", ddl: "CREATE VIEW vo_r AS SELECT uid FROM vo_posts UNION SELECT uid FROM vo_tags"},
		{name: "view over a view", ddl: "CREATE VIEW vo_iv AS SELECT uid FROM vo_tags; CREATE VIEW vo_r AS SELECT uid FROM vo_iv"},
		{name: "filtered out", ddl: "CREATE VIEW vo_r AS SELECT uid FROM vo_tags", tables: []string{"vo_users", "vo_posts", "vo_tags"}},
	}
	const reset = "DROP VIEW IF EXISTS vo_r, vo_iv; DROP TABLE IF EXISTS vo_users, vo_posts, vo_tags, vo_labels"
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Cleanup(func() { execAll(t, "mysql", mysqlConnStr, reset) })
			tables := shape.tables
			if tables == nil {
				tables = []string{"vo_users", "vo_posts", "vo_tags", "vo_r", "vo_iv"}
			}
			for _, m := range migrations {
				execAll(t, "mysql", mysqlConnStr, reset+`;
					CREATE TABLE vo_users (id int PRIMARY KEY, role varchar(20));
					CREATE TABLE vo_posts (id int PRIMARY KEY, uid int);
					CREATE TABLE vo_tags (id int PRIMARY KEY, uid int);`+shape.ddl)
				base := introspectWith(t, introspect.NewMySQLIntrospector(), mysqlConnStr, introspect.IntrospectionOptions{Tables: tables})
				p, err := mysql.New()
				if err != nil {
					t.Fatalf("mysql.New() error: %v", err)
				}
				err = introspect.Merge(base, p, []string{writeMigration(t, m.sql)})
				compareVerdicts(t, shape, m, mysqlVerdict(t, m.sql), err)
			}
		})
	}
}

// mysqlVerdict runs migration on MySQL and returns why MySQL refuses it or
// which view then fails, or "" when it accepts it and every view runs.
func mysqlVerdict(t *testing.T, migration string) string {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("mysql", mysqlConnStr)
	if err != nil {
		t.Fatalf("opening mysql: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	for _, stmt := range splitStatements(migration) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return "refused: " + err.Error()
		}
	}
	for _, v := range []string{"vo_iv", "vo_r"} {
		var n int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.views WHERE table_schema = DATABASE() AND table_name = ?", v).Scan(&n); err != nil || n == 0 {
			continue
		}
		if _, err := db.ExecContext(ctx, "SELECT * FROM "+v+" LIMIT 0"); err != nil { //nolint:gosec // a view name the database listed
			return "view " + v + " fails: " + err.Error()
		}
	}
	return ""
}
