package introspect_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/introspect"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// pgConstraintNamesFixture leaves constraints and indexes unnamed for
// PostgreSQL to name: clashes, 63-byte cuts, expression index columns, merged
// duplicates, names taken after a rename, and constraints added by ALTER TABLE.
const pgConstraintNamesFixture = `
CREATE SCHEMA probe_names;
CREATE TABLE probe_names.t (a int CHECK (a > 0), b int CHECK (a > b), c int CHECK (length(c::text) > 0), CHECK (1 = 1));
CREATE TABLE probe_names.p (id int PRIMARY KEY);
CREATE TABLE probe_names.f (
	a int REFERENCES probe_names.p (id),
	CONSTRAINT x FOREIGN KEY (a) REFERENCES probe_names.p (id),
	FOREIGN KEY (a) REFERENCES probe_names.p (id)
);
CREATE TABLE probe_names.z_a_key (id int);
CREATE TABLE probe_names.z (a int UNIQUE);
CREATE TABLE probe_names.abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij_ta (
	colabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij int UNIQUE,
	other int PRIMARY KEY
);
CREATE TABLE probe_names."ééééééééééééééééééééééééééééé" ("ççççççççç" int UNIQUE);
CREATE TABLE probe_names.e (x text, a int, b int);
CREATE INDEX ON probe_names.e (a, a);
CREATE INDEX ON probe_names.e (b, lower(x));
CREATE UNIQUE INDEX ON probe_names.e (b);
CREATE TABLE probe_names.m (id int UNIQUE, CONSTRAINT pk PRIMARY KEY (id), CONSTRAINT u2 UNIQUE (id));
CREATE TABLE probe_names.m2 (CONSTRAINT u1 UNIQUE (id), id int PRIMARY KEY);
CREATE TABLE probe_names.pk2_pkey (id int);
CREATE TABLE probe_names.pk2 (id int);
ALTER TABLE probe_names.pk2 ADD PRIMARY KEY (id);
CREATE TABLE probe_names.ck (a int);
ALTER TABLE probe_names.ck ADD CHECK (a > 0), ADD CHECK (a < 10);
ALTER TABLE probe_names.ck ADD COLUMN b int UNIQUE CHECK (b > 0) REFERENCES probe_names.p (id);
CREATE TABLE probe_names.c (email text UNIQUE);
ALTER TABLE probe_names.c ADD UNIQUE (email);
CREATE TABLE probe_names.r (v int UNIQUE);
ALTER TABLE probe_names.r RENAME TO r2;
ALTER TABLE probe_names.r2 RENAME COLUMN v TO w;
CREATE TABLE probe_names.dc (a int UNIQUE, b int, CHECK (b > a));
ALTER TABLE probe_names.dc DROP COLUMN a;
CREATE TABLE probe_names.cp (a int, b int, PRIMARY KEY (a, b));
ALTER TABLE probe_names.cp DROP COLUMN a;
ALTER TABLE probe_names.cp ADD COLUMN c int
`

const pgConstraintNamesFixtureReset = `DROP SCHEMA IF EXISTS probe_names CASCADE`

// mysqlConstraintNamesFixture leaves constraints and keys unnamed for MySQL to
// name, and lets MySQL build, keep and drop the indexes it builds for foreign
// keys.
const mysqlConstraintNamesFixture = `
CREATE TABLE n_p (id int PRIMARY KEY, b int, c int, KEY (b, c));
CREATE TABLE n_k (a int, CHECK (a > 0), CONSTRAINT n_named_ck CHECK (a < 10), CHECK (a <> 5), b int CHECK (b > 0));
CREATE TABLE n_fk (
	a int,
	b int,
	FOREIGN KEY (a) REFERENCES n_p (id),
	CONSTRAINT n_named_fk FOREIGN KEY (b) REFERENCES n_p (id),
	FOREIGN KEY (b) REFERENCES n_p (id)
);
ALTER TABLE n_fk ADD FOREIGN KEY (a) REFERENCES n_p (id);
CREATE TABLE n_fi (a int, b int, FOREIGN KEY my_idx (a) REFERENCES n_p (id), CONSTRAINT n_cn FOREIGN KEY ix2 (b) REFERENCES n_p (id));
CREATE TABLE n_u (a int UNIQUE, UNIQUE KEY (a), UNIQUE KEY a_3 (a), KEY (a));
CREATE TABLE n_up (A int, UNIQUE (a), KEY (A));
CREATE TABLE n_o4 (b int UNIQUE, c int, FOREIGN KEY (c) REFERENCES n_p (id), UNIQUE (c, b), KEY (c));
CREATE TABLE n_pk (a int, CONSTRAINT mypk PRIMARY KEY (a));
CREATE TABLE n_ren (a int, CHECK (a > 0), FOREIGN KEY (a) REFERENCES n_p (id), CONSTRAINT n_ren_ibfk_9 FOREIGN KEY (a) REFERENCES n_p (id));
ALTER TABLE n_ren ADD FOREIGN KEY (a) REFERENCES n_p (id), ADD CHECK (a < 100);
RENAME TABLE n_ren TO n_ren2;
CREATE TABLE n_kc (Price int, CHECK (price > 0));
CREATE TABLE n_gen (a int, b int, FOREIGN KEY (a) REFERENCES n_p (id));
ALTER TABLE n_gen ADD CONSTRAINT n_gen_ab FOREIGN KEY (a, b) REFERENCES n_p (b, c);
ALTER TABLE n_gen ADD INDEX n_gen_b (b);
CREATE TABLE n_dc (a int, b int, UNIQUE (a, b), KEY (b));
ALTER TABLE n_dc DROP COLUMN a;
CREATE TABLE n_ck (a int KEY, b int UNIQUE KEY UNIQUE);
CREATE TABLE n_cs (x int, KEY (a), a int PRIMARY KEY UNIQUE, UNIQUE (a));
CREATE TABLE n_cu (a int UNIQUE KEY KEY);
CREATE TABLE n_ca (a int, b int);
ALTER TABLE n_ca ADD COLUMN c int KEY UNIQUE, MODIFY a int UNIQUE UNIQUE, ADD (d int UNIQUE, e int UNIQUE KEY), CHANGE b f int UNIQUE
`

const mysqlConstraintNamesFixtureReset = `
DROP TABLE IF EXISTS n_ren2;
DROP TABLE IF EXISTS n_ren;
DROP TABLE IF EXISTS n_fk;
DROP TABLE IF EXISTS n_fi;
DROP TABLE IF EXISTS n_o4;
DROP TABLE IF EXISTS n_gen;
DROP TABLE IF EXISTS n_p;
DROP TABLE IF EXISTS n_k;
DROP TABLE IF EXISTS n_u;
DROP TABLE IF EXISTS n_up;
DROP TABLE IF EXISTS n_pk;
DROP TABLE IF EXISTS n_kc;
DROP TABLE IF EXISTS n_dc;
DROP TABLE IF EXISTS n_ck;
DROP TABLE IF EXISTS n_cs;
DROP TABLE IF EXISTS n_cu;
DROP TABLE IF EXISTS n_ca
`

func TestPostgresIntrospect_TableConstraints(t *testing.T) {
	requireDatabases(t)
	ddl := pgParityFixture + ";" + pgConstraintNamesFixture
	applyParityFixture(t, "pgx", pgConnStr, pgParityFixtureReset+";"+pgConstraintNamesFixtureReset, ddl)

	intro := introspect.NewPostgresIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup
	introspected := &parser.Schema{}
	if err := intro.Introspect(context.Background(), pgConnStr, introspected, introspect.IntrospectionOptions{
		Schemas: []string{"probe_app", "probe_other", "probe_names"},
	}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	parsed := parseStatements(t, postgres.New("public"), ddl)

	assertConstraintParity(t, parsed, introspected,
		"probe_app.checks", "probe_app.pairs", "probe_app.users", "probe_other.accounts", "probe_app.UserAccounts",
		"probe_names.t", "probe_names.p", "probe_names.f", "probe_names.z_a_key", "probe_names.z",
		"probe_names.abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij_ta",
		"probe_names.ééééééééééééééééééééééééééééé", "probe_names.e", "probe_names.m", "probe_names.m2",
		"probe_names.pk2_pkey", "probe_names.pk2", "probe_names.ck", "probe_names.c", "probe_names.r2", "probe_names.dc", "probe_names.cp",
	)
}

func TestMySQLIntrospect_TableConstraints(t *testing.T) {
	requireDatabases(t)
	ddl := mysqlParityFixture + ";" + mysqlConstraintNamesFixture
	applyParityFixture(t, "mysql", mysqlConnStr, mysqlConstraintNamesFixtureReset+";"+mysqlParityFixtureReset, ddl)

	tables := []string{
		"MixedCase", "m_pairs", "m_users",
		"n_p", "n_k", "n_fk", "n_fi", "n_u", "n_up", "n_o4", "n_pk", "n_ren2", "n_kc", "n_gen", "n_dc",
		"n_ck", "n_cs", "n_cu", "n_ca",
	}
	intro := introspect.NewMySQLIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup
	introspected := &parser.Schema{}
	if err := intro.Introspect(context.Background(), mysqlConnStr, introspected, introspect.IntrospectionOptions{
		Tables: tables,
	}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	p, err := mysql.New()
	if err != nil {
		t.Fatalf("mysql.New() error: %v", err)
	}
	parsed := parseStatements(t, p, ddl)

	assertConstraintParity(t, parsed, introspected, tables...)
}

func TestSQLiteIntrospect_TableConstraints(t *testing.T) {
	requireDatabases(t)
	dbPath := filepath.Join(t.TempDir(), "constraints.db")
	applyParityFixture(t, "sqlite", dbPath, "", sqliteParityFixture)

	intro := introspect.NewSQLiteIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup
	introspected := &parser.Schema{}
	if err := intro.Introspect(context.Background(), dbPath, introspected, introspect.IntrospectionOptions{
		Tables: []string{"s_users", "s_posts"},
	}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	parsed := parseStatements(t, sqlite.New(), sqliteParityFixture)

	assertConstraintParity(t, parsed, introspected, "s_posts")
}

// tableKeys is the part of a table assertConstraintParity compares.
type tableKeys struct {
	Constraints []parser.Constraint
	Columns     []columnKeys
}

type columnKeys struct {
	Name        string
	PrimaryKey  bool
	Unique      bool
	FKReference *parser.FKReference
}

// assertConstraintParity compares the constraints and column key flags of
// each named table ("schema.table" on PostgreSQL) between the parsed and the
// introspected schema. CHECK text is left out: each catalog rewrites it, and
// it is normalized and compared apart from the constraint shape.
func assertConstraintParity(t *testing.T, parsed, introspected *parser.Schema, tables ...string) {
	t.Helper()
	for _, name := range tables {
		want := keysOf(t, parsed, name)
		got := keysOf(t, introspected, name)
		if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(parser.Constraint{}, "CheckExpression")); diff != "" {
			t.Errorf("%s: introspected keys differ from the file parser's (-parsed +introspected):\n%s", name, diff)
		}
	}
}

func keysOf(t *testing.T, s *parser.Schema, name string) tableKeys {
	t.Helper()
	schema, table, qualified := strings.Cut(name, ".")
	if !qualified {
		schema, table = "", name
	}
	for _, tbl := range s.Tables {
		if tbl.Schema != schema || tbl.Name != table {
			continue
		}
		keys := tableKeys{Constraints: append([]parser.Constraint(nil), tbl.Constraints...)}
		sortConstraints(keys.Constraints)
		for _, col := range tbl.Columns {
			keys.Columns = append(keys.Columns, columnKeys{Name: col.Name, PrimaryKey: col.PrimaryKey, Unique: col.Unique, FKReference: col.FKReference})
		}
		return keys
	}
	t.Fatalf("table %q not found", name)
	return tableKeys{}
}

// parseStatements parses ddl one statement at a time and returns the schema.
func parseStatements(t *testing.T, p parser.Parser, ddl string) *parser.Schema {
	t.Helper()
	for i, stmt := range splitStatements(ddl) {
		if err := p.Parse(fmt.Sprintf("stmt_%02d.sql", i), []byte(stmt)); err != nil {
			t.Fatalf("Parse(%q) error: %v", truncate(strings.Join(strings.Fields(stmt), " ")), err)
		}
	}
	return p.Schema()
}
