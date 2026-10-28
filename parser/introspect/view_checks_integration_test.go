package introspect_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser/introspect"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

const inwStands = ", which the SQL files drop or rename; under source: both the database's view stands, so change it in the database first"

// TestMerge_MySQLColumnCheckIntrospectedViewsOnly: MySQL's column check
// reads the definitions of the views input.introspect includes, so dropping
// a column a view the filter leaves out reads is not an error, though it is
// once the filter includes the view. The table check still covers that
// view.
func TestMerge_MySQLColumnCheckIntrospectedViewsOnly(t *testing.T) {
	requireDatabases(t)
	const reset = "DROP VIEW IF EXISTS inw_ci_v; DROP TABLE IF EXISTS inw_ci_t"
	const ddl = "CREATE TABLE inw_ci_t (id int PRIMARY KEY, name varchar(20)); CREATE VIEW inw_ci_v AS SELECT id, name FROM inw_ci_t"
	tests := []struct {
		name      string
		tables    []string
		migration string
		// wantErr is the whole error; empty for none.
		wantErr string
	}{
		{
			name:      "column of a view left out",
			tables:    []string{"inw_ci_t"},
			migration: "ALTER TABLE inw_ci_t DROP COLUMN name;",
		},
		{
			name:      "column of a view included",
			tables:    []string{"inw_ci_t", "inw_ci_v"},
			migration: "ALTER TABLE inw_ci_t DROP COLUMN name;",
			wantErr:   "merge both mode: view inw_ci_v: column name reads column name of table inw_ci_t" + inwStands,
		},
		{
			name:      "table of a view left out",
			tables:    []string{"inw_ci_t"},
			migration: "DROP TABLE inw_ci_t;",
			wantErr:   "merge both mode: view inw_ci_v reads table inw_ci_t" + inwStands,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := inwMySQLBase(t, reset, ddl, introspect.IntrospectionOptions{Tables: tt.tables})
			err := introspect.Merge(base, inwMySQLParser(t), []string{writeMigration(t, tt.migration)})
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tt.wantErr {
				t.Errorf("Merge() error = %q, want %q", got, tt.wantErr)
			}
		})
	}
}

// TestMerge_MySQLColumnReadOutsideSelectList: dropping a column a MySQL
// view's query reads in HAVING, ORDER BY or a window is an error naming the
// view and the table column.
func TestMerge_MySQLColumnReadOutsideSelectList(t *testing.T) {
	requireDatabases(t)
	const reset = "DROP VIEW IF EXISTS inw_rd_v; DROP TABLE IF EXISTS inw_rd_t"
	tests := []struct {
		name  string
		query string
	}{
		{name: "HAVING", query: "SELECT grp FROM inw_rd_t GROUP BY grp HAVING max(score) > 0"},
		{name: "ORDER BY", query: "SELECT id FROM inw_rd_t ORDER BY score"},
		{name: "window", query: "SELECT id, row_number() OVER (PARTITION BY grp ORDER BY score) AS rn FROM inw_rd_t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := inwMySQLBase(t, reset,
				"CREATE TABLE inw_rd_t (id int PRIMARY KEY, grp int, score int); CREATE VIEW inw_rd_v AS "+tt.query,
				introspect.IntrospectionOptions{Tables: []string{"inw_rd_t", "inw_rd_v"}})
			err := introspect.Merge(base, inwMySQLParser(t), []string{writeMigration(t, "ALTER TABLE inw_rd_t DROP COLUMN score;")})
			want := "merge both mode: view inw_rd_v reads column score of table inw_rd_t" + inwStands
			if err == nil || err.Error() != want {
				t.Errorf("Merge() error = %v, want %q", err, want)
			}
		})
	}
}

// TestMerge_SQLiteViewBreakNamesFile: a SQLite view a migration breaks is an
// error naming the migration file.
func TestMerge_SQLiteViewBreakNamesFile(t *testing.T) {
	requireDatabases(t)
	base := introspectSQLite(t, applySQLite(t, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT); CREATE VIEW v AS SELECT id, name FROM t"),
		introspect.IntrospectionOptions{})
	migration := writeMigration(t, "ALTER TABLE t DROP COLUMN name;")

	err := introspect.Merge(base, sqlite.New(), []string{migration})
	if err == nil {
		t.Fatal("Merge() error = nil, want the view-break error")
	}
	for _, want := range []string{migration, "no such column: name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Merge() error = %q, want it to contain %q", err, want)
		}
	}
}

// TestMerge_SQLiteViewColumnsFromCopy: SQLite's views are read from the copy
// the files ran against, so a file that drops a view and creates it again
// gives it the new query's columns.
func TestMerge_SQLiteViewColumnsFromCopy(t *testing.T) {
	requireDatabases(t)
	base := introspectSQLite(t, applySQLite(t, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT NOT NULL); CREATE VIEW v AS SELECT id, name FROM t"),
		introspect.IntrospectionOptions{})
	if diff := cmp.Diff([]string{"id integer", "name text"}, inwViewColumns(t, base, "v")); diff != "" {
		t.Fatalf("introspected v columns (-want +got):\n%s", diff)
	}

	p := sqlite.New()
	if err := introspect.Merge(base, p, []string{writeMigration(t, "DROP VIEW v;\nCREATE VIEW v AS SELECT id FROM t;\n")}); err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	if diff := cmp.Diff([]string{"id integer"}, inwViewColumns(t, p.Schema(), "v")); diff != "" {
		t.Errorf("merged v columns (-want +got):\n%s", diff)
	}
}

// TestMerge_SQLiteLegacyAlterOnConnectionMissed: the copy runs with SQLite's
// default settings, so a table rename the migrating connection runs under
// PRAGMA legacy_alter_table, which leaves a view reading the old name, is
// not caught.
func TestMerge_SQLiteLegacyAlterOnConnectionMissed(t *testing.T) {
	requireDatabases(t)
	const ddl = "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT); CREATE VIEW v AS SELECT id, name FROM t"
	const rename = "ALTER TABLE t RENAME TO t2;"
	dbPath := applySQLite(t, ddl)

	if err := introspect.Merge(introspectSQLite(t, dbPath, introspect.IntrospectionOptions{}), sqlite.New(), []string{writeMigration(t, rename)}); err != nil {
		t.Errorf("Merge() error = %v, want nil: the check does not see the connection's PRAGMA", err)
	}

	// The same rename run on a connection with the PRAGMA set leaves the
	// view broken.
	ctx := context.Background()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("sqlite connection: %v", err)
	}
	defer conn.Close() //nolint:errcheck // test cleanup
	for _, stmt := range []string{"PRAGMA legacy_alter_table = ON", rename} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("applying %q: %v", stmt, err)
		}
	}
	rows, err := conn.QueryContext(ctx, "SELECT * FROM v")
	if err == nil {
		_ = rows.Close()
		t.Fatal("SELECT from v after the legacy rename: error = nil, want the view broken")
	}
	if !strings.Contains(err.Error(), "no such table: main.t") {
		t.Errorf("SELECT from v after the legacy rename: error = %q, want it to name the old table", err)
	}
}

// TestMerge_CatalogViewTypeDropRefused: a view whose columns come from the
// catalog, here a compound query, is checked by its columns' types. On
// PostgreSQL the DROP TYPE that would remove an enum one of its columns has
// is refused first, by the check on what the view depends on, so the error
// names the view's column.
func TestMerge_CatalogViewTypeDropRefused(t *testing.T) {
	requireDatabases(t)
	base := inwPGBase(t, "CREATE TYPE role_t AS ENUM ('a', 'b'); CREATE TABLE users (id int PRIMARY KEY, role text NOT NULL); "+
		"CREATE VIEW vu AS SELECT id, role::role_t AS r FROM users UNION SELECT id, 'a'::role_t FROM users", introspect.IntrospectionOptions{})
	if v := findView(t, base, "vu"); v.Definition != nil {
		t.Fatalf("vu has a definition, want its columns from the catalog")
	}
	if diff := cmp.Diff([]string{"id integer", "r role_t"}, inwViewColumns(t, base, "vu")); diff != "" {
		t.Fatalf("introspected vu columns (-want +got):\n%s", diff)
	}

	err := introspect.Merge(base, postgres.New(inwPGConnSchema), []string{writeMigration(t, "DROP TYPE role_t;")})
	const want = "drop type: cannot drop type inw_merge.role_t: column inw_merge.vu.r of view uses it"
	if err == nil || !strings.HasSuffix(err.Error(), want) {
		t.Errorf("Merge() error = %v, want it to end %q", err, want)
	}
}
