package introspect_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/introspect"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

const inwPGConnSchema = "inw_merge"

// inwPGConn is the connection for schema inw_merge, which unqualified names
// resolve to.
func inwPGConn() string { return pgConnStr + "&search_path=" + inwPGConnSchema }

// inwPGBase creates schema inw_merge from ddl and introspects it with opts,
// whose Schemas it sets.
func inwPGBase(t *testing.T, ddl string, opts introspect.IntrospectionOptions) *parser.Schema {
	t.Helper()
	conn := inwPGConn()
	execAll(t, "pgx", conn, "DROP SCHEMA IF EXISTS inw_merge CASCADE; CREATE SCHEMA inw_merge;"+ddl)
	t.Cleanup(func() { execAll(t, "pgx", conn, "DROP SCHEMA IF EXISTS inw_merge CASCADE") })
	opts.Schemas = []string{inwPGConnSchema}
	return introspectWith(t, introspect.NewPostgresIntrospector(), conn, opts)
}

// inwMySQLBase runs reset and ddl on the MySQL test database, runs reset
// again when the test ends, and introspects it with opts.
func inwMySQLBase(t *testing.T, reset, ddl string, opts introspect.IntrospectionOptions) *parser.Schema {
	t.Helper()
	applyParityFixture(t, "mysql", mysqlConnStr, reset, ddl)
	return introspectWith(t, introspect.NewMySQLIntrospector(), mysqlConnStr, opts)
}

func inwMySQLParser(t *testing.T) introspect.Seedable {
	t.Helper()
	p, err := mysql.New()
	if err != nil {
		t.Fatalf("mysql.New() error: %v", err)
	}
	return p
}

// inwViewColumns spells each column of view name in s as "<name> <type>".
func inwViewColumns(t *testing.T, s *parser.Schema, name string) []string {
	t.Helper()
	v := findView(t, s, name)
	got := make([]string, 0, len(v.Columns))
	for _, c := range v.Columns {
		got = append(got, c.Name+" "+c.Type)
	}
	return got
}

// TestMerge_CreateDomain: under source: both a file's CREATE DOMAIN joins
// the domains the database has.
func TestMerge_CreateDomain(t *testing.T) {
	requireDatabases(t)
	base := inwPGBase(t, `CREATE DOMAIN pos_int AS integer CHECK (VALUE > 0);
		CREATE TABLE t (id int PRIMARY KEY, n pos_int)`, introspect.IntrospectionOptions{})
	migration := writeMigration(t, "CREATE DOMAIN short_text AS varchar(10);\nALTER TABLE t ADD COLUMN s short_text;\n")

	p := postgres.New(inwPGConnSchema)
	if err := introspect.Merge(base, p, []string{migration}); err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	got := make([]string, 0, len(p.Schema().DomainTypes))
	for _, d := range p.Schema().DomainTypes {
		got = append(got, d.Schema+"."+d.Name+" "+d.BaseType)
	}
	want := []string{"inw_merge.pos_int integer", "inw_merge.short_text varchar(10)"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("merged domains (-want +got):\n%s", diff)
	}
}

// TestMerge_OnlyIntrospectedTables: a pending migration reaches only the
// tables input.introspect includes. One that alters a table the filter left
// out is an error naming it, while the same migration applies once the
// filter includes the table.
func TestMerge_OnlyIntrospectedTables(t *testing.T) {
	requireDatabases(t)
	tests := []struct {
		name string
		// base introspects users and posts, with only the given tables.
		base      func(t *testing.T, tables []string) *parser.Schema
		parser    func(t *testing.T) introspect.Seedable
		users     string
		posts     string
		migration string
		// wantErr is the parser's error for the migration with posts left
		// out of the introspection.
		wantErr string
	}{
		{
			name: "postgres",
			base: func(t *testing.T, tables []string) *parser.Schema {
				return inwPGBase(t, "CREATE TABLE users (id int PRIMARY KEY); CREATE TABLE posts (id int PRIMARY KEY)",
					introspect.IntrospectionOptions{Tables: tables})
			},
			parser:    func(*testing.T) introspect.Seedable { return postgres.New(inwPGConnSchema) },
			users:     "users",
			posts:     "posts",
			migration: "ALTER TABLE posts ADD COLUMN title text;",
			wantErr:   "table inw_merge.posts not found",
		},
		{
			name: "mysql",
			base: func(t *testing.T, tables []string) *parser.Schema {
				return inwMySQLBase(t, "DROP TABLE IF EXISTS inw_oi_users, inw_oi_posts",
					"CREATE TABLE inw_oi_users (id int PRIMARY KEY); CREATE TABLE inw_oi_posts (id int PRIMARY KEY)",
					introspect.IntrospectionOptions{Tables: tables})
			},
			parser:    inwMySQLParser,
			users:     "inw_oi_users",
			posts:     "inw_oi_posts",
			migration: "ALTER TABLE inw_oi_posts ADD COLUMN title text;",
			wantErr:   "table inw_oi_posts not found",
		},
		{
			name: "sqlite",
			base: func(t *testing.T, tables []string) *parser.Schema {
				return introspectSQLite(t, applySQLite(t, "CREATE TABLE users (id INTEGER PRIMARY KEY); CREATE TABLE posts (id INTEGER PRIMARY KEY)"),
					introspect.IntrospectionOptions{Tables: tables})
			},
			parser:    func(*testing.T) introspect.Seedable { return sqlite.New() },
			users:     "users",
			posts:     "posts",
			migration: "ALTER TABLE posts ADD COLUMN title TEXT;",
			wantErr:   "table posts not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migration := writeMigration(t, tt.migration)

			filtered := tt.base(t, []string{tt.users})
			err := introspect.Merge(filtered, tt.parser(t), []string{migration})
			want := fmt.Sprintf("merge both mode: parsing %[1]s: parsing %[1]s: %s", migration, tt.wantErr)
			if err == nil || err.Error() != want {
				t.Errorf("Merge() with %s left out: error = %v, want %q", tt.posts, err, want)
			}

			whole := tt.base(t, []string{tt.users, tt.posts})
			p := tt.parser(t)
			if err := introspect.Merge(whole, p, []string{migration}); err != nil {
				t.Fatalf("Merge() with %s included: %v", tt.posts, err)
			}
			if diff := cmp.Diff([]string{"id", "title"}, columnNames(findTable(t, p.Schema(), tt.posts).Columns)); diff != "" {
				t.Errorf("merged %s columns (-want +got):\n%s", tt.posts, diff)
			}
		})
	}
}

// TestMerge_PendingViewChangeIgnored: under source: both a migration that
// drops a view and creates it again with other columns does not change it:
// the database's view, and its columns, stand.
func TestMerge_PendingViewChangeIgnored(t *testing.T) {
	requireDatabases(t)
	tests := []struct {
		name      string
		base      func(t *testing.T) *parser.Schema
		parser    func(t *testing.T) introspect.Seedable
		view      string
		migration string
		want      []string
	}{
		{
			name: "postgres",
			base: func(t *testing.T) *parser.Schema {
				return inwPGBase(t, "CREATE TABLE t (id int PRIMARY KEY, name text NOT NULL); CREATE VIEW v AS SELECT id, name FROM t",
					introspect.IntrospectionOptions{})
			},
			parser:    func(*testing.T) introspect.Seedable { return postgres.New(inwPGConnSchema) },
			view:      "v",
			migration: "DROP VIEW v;\nCREATE VIEW v AS SELECT id FROM t;\n",
			want:      []string{"id integer", "name text"},
		},
		{
			name: "mysql",
			base: func(t *testing.T) *parser.Schema {
				return inwMySQLBase(t, "DROP VIEW IF EXISTS inw_vc_v; DROP TABLE IF EXISTS inw_vc_t",
					"CREATE TABLE inw_vc_t (id int PRIMARY KEY, name varchar(20) NOT NULL); CREATE VIEW inw_vc_v AS SELECT id, name FROM inw_vc_t",
					introspect.IntrospectionOptions{Tables: []string{"inw_vc_t", "inw_vc_v"}})
			},
			parser:    inwMySQLParser,
			view:      "inw_vc_v",
			migration: "DROP VIEW inw_vc_v;\nCREATE VIEW inw_vc_v AS SELECT id FROM inw_vc_t;\n",
			want:      []string{"id int", "name varchar(20)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.parser(t)
			if err := introspect.Merge(tt.base(t), p, []string{writeMigration(t, tt.migration)}); err != nil {
				t.Fatalf("Merge() error: %v", err)
			}
			if diff := cmp.Diff(tt.want, inwViewColumns(t, p.Schema(), tt.view)); diff != "" {
				t.Errorf("merged %s columns (-want +got):\n%s", tt.view, diff)
			}
		})
	}
}

// TestMerge_MySQLForeignKeyIndexKept: MySQL's catalog does not say which
// index it built for a foreign key, so under source: both a pending key
// that would replace that index leaves it in place beside the new one. The
// migrated database keeps only the new one.
func TestMerge_MySQLForeignKeyIndexKept(t *testing.T) {
	requireDatabases(t)
	const reset = "DROP TABLE IF EXISTS inw_fki_c, inw_fki_p"
	const ddl = `CREATE TABLE inw_fki_p (id int PRIMARY KEY);
		CREATE TABLE inw_fki_c (id int PRIMARY KEY, a int, b int, FOREIGN KEY (a) REFERENCES inw_fki_p (id))`
	const pending = "ALTER TABLE inw_fki_c ADD KEY (a, b);"
	tables := []string{"inw_fki_p", "inw_fki_c"}

	base := inwMySQLBase(t, reset, ddl, introspect.IntrospectionOptions{Tables: tables})
	p := inwMySQLParser(t)
	if err := introspect.Merge(base, p, []string{writeMigration(t, pending)}); err != nil {
		t.Fatalf("Merge() error: %v", err)
	}

	indexes := func(s *parser.Schema) []string {
		cs := filterConstraints(findTable(t, s, "inw_fki_c").Constraints, parser.Index)
		got := make([]string, 0, len(cs))
		for _, c := range cs {
			got = append(got, c.Name+" "+strings.Join(c.Columns, ","))
		}
		return got
	}
	if diff := cmp.Diff([]string{"a a", "a_2 a,b"}, indexes(p.Schema())); diff != "" {
		t.Errorf("merged inw_fki_c indexes (-want +got):\n%s", diff)
	}

	execAll(t, "mysql", mysqlConnStr, pending)
	if diff := cmp.Diff([]string{"a a,b"}, indexes(introspectMySQL(t, tables...))); diff != "" {
		t.Errorf("migrated database's inw_fki_c indexes (-want +got):\n%s", diff)
	}
}

// TestMerge_MySQLViewsDiscovered: under source: both the MySQL database's
// views are in the schema, read from the database.
func TestMerge_MySQLViewsDiscovered(t *testing.T) {
	requireDatabases(t)
	base := inwMySQLBase(t, "DROP VIEW IF EXISTS inw_dv_v; DROP TABLE IF EXISTS inw_dv_t",
		"CREATE TABLE inw_dv_t (id int PRIMARY KEY, name varchar(20) NOT NULL); CREATE VIEW inw_dv_v AS SELECT id, name FROM inw_dv_t",
		introspect.IntrospectionOptions{Tables: []string{"inw_dv_t", "inw_dv_v"}})

	p := inwMySQLParser(t)
	if err := introspect.Merge(base, p, []string{writeMigration(t, "ALTER TABLE inw_dv_t ADD COLUMN note text;")}); err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	if len(p.Schema().Views) != 1 {
		t.Fatalf("merged schema has %d views, want 1: %+v", len(p.Schema().Views), p.Schema().Views)
	}
	if diff := cmp.Diff([]string{"id int", "name varchar(20)"}, inwViewColumns(t, p.Schema(), "inw_dv_v")); diff != "" {
		t.Errorf("merged inw_dv_v columns (-want +got):\n%s", diff)
	}
}
