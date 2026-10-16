package introspect_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/introspect"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// TestPostgresIntrospect_PartitionsListed pins that the introspector lists
// the partitions it skips, each with its parent, as the file parser does.
func TestPostgresIntrospect_PartitionsListed(t *testing.T) {
	requireDatabases(t)
	const ddl = `
CREATE SCHEMA pgi_plist;
CREATE SCHEMA pgi_plist_other;
CREATE TABLE pgi_plist.p (id int NOT NULL, k int NOT NULL) PARTITION BY RANGE (k);
CREATE TABLE pgi_plist.p1 PARTITION OF pgi_plist.p FOR VALUES FROM (0) TO (10) PARTITION BY RANGE (id);
CREATE TABLE pgi_plist.p1a PARTITION OF pgi_plist.p1 FOR VALUES FROM (0) TO (10);
CREATE TABLE pgi_plist_other.p2 PARTITION OF pgi_plist.p FOR VALUES FROM (10) TO (20);
CREATE INDEX p_k ON pgi_plist.p (k)`
	parsed, introspected := pgParityCase(t, "DROP SCHEMA IF EXISTS pgi_plist CASCADE; DROP SCHEMA IF EXISTS pgi_plist_other CASCADE", ddl, "pgi_plist", "pgi_plist_other")

	want := []parser.Partition{
		{Schema: "pgi_plist", Name: "p1", ParentSchema: "pgi_plist", ParentName: "p"},
		{Schema: "pgi_plist", Name: "p1a", ParentSchema: "pgi_plist", ParentName: "p1"},
		{Schema: "pgi_plist_other", Name: "p2", ParentSchema: "pgi_plist", ParentName: "p"},
	}
	if diff := cmp.Diff(want, introspected.Partitions); diff != "" {
		t.Errorf("introspected partitions mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "postgres", parsed, introspected, pgSkippedStatementExceptions...)
}

// TestPostgresMerge_Indexes pins source: both for CREATE INDEX and DROP INDEX
// in a migration: an index on the database's partition or materialized view
// is skipped, a dropped index leaves the table, a dropped expression index
// frees its name for a derived one (t_c_idx), and the merged schema matches
// the database after the migration runs. An index on the database's plain
// view is an error, as it is in PostgreSQL.
func TestPostgresMerge_Indexes(t *testing.T) {
	requireDatabases(t)
	const (
		reset = "DROP SCHEMA IF EXISTS pgi_mi CASCADE"
		base  = `CREATE SCHEMA pgi_mi;
CREATE TABLE pgi_mi.t (id int PRIMARY KEY, b text, c text);
CREATE UNIQUE INDEX t_b ON pgi_mi.t (b);
CREATE INDEX ON pgi_mi.t ((c::text));
CREATE TABLE pgi_mi.p (id int NOT NULL, k int NOT NULL) PARTITION BY RANGE (k);
CREATE TABLE pgi_mi.p1 PARTITION OF pgi_mi.p FOR VALUES FROM (0) TO (10);
CREATE MATERIALIZED VIEW pgi_mi.mv AS SELECT id, b FROM pgi_mi.t;
CREATE VIEW pgi_mi.v AS SELECT id FROM pgi_mi.t`
		migration = `CREATE INDEX p1_k ON pgi_mi.p1 (k);
CREATE UNIQUE INDEX mv_id ON pgi_mi.mv (id);
DROP INDEX pgi_mi.t_b;
CREATE UNIQUE INDEX t_c ON pgi_mi.t (c);
DROP INDEX pgi_mi.t_c_idx;
CREATE INDEX ON pgi_mi.t (c)`
	)
	applyParityFixture(t, "pgx", pgConnStr, reset, base)
	seeded := introspectPG(t, "", "pgi_mi")

	p := postgres.New("public")
	if err := introspect.Merge(seeded, p, []string{writeMigration(t, migration)}); err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	err := introspect.Merge(introspectPG(t, "", "pgi_mi"), postgres.New("public"), []string{writeMigration(t, "CREATE INDEX v_id ON pgi_mi.v (id)")})
	if err == nil || !strings.Contains(err.Error(), "create index v_id: table pgi_mi.v not found") {
		t.Errorf("Merge(CREATE INDEX on a view) error = %v, want table pgi_mi.v not found", err)
	}

	applyParityFixture(t, "pgx", pgConnStr, "", migration)
	introspected := introspectPG(t, "", "pgi_mi")
	assertSchemaParity(t, "postgres", p.Schema(), introspected,
		parityException{Field: "Schema.Views", Reason: "a pending change to a view does not apply under source: both (PRD §6.1), so mv's ConcurrentlyRefreshable stays the database's"})
}

// TestSQLiteMerge_Indexes pins source: both for CREATE INDEX and DROP INDEX in
// a migration: a dropped index leaves the table, and the merged schema matches
// the database after the migration runs. An index on the database's view is
// an error, as it is in SQLite.
func TestSQLiteMerge_Indexes(t *testing.T) {
	const (
		base = `CREATE TABLE t (id INTEGER PRIMARY KEY, a TEXT, b TEXT);
CREATE UNIQUE INDEX ix_a ON t (a);
CREATE VIEW v AS SELECT id FROM t`
		migration = `DROP INDEX IX_A;
CREATE UNIQUE INDEX ix_b ON t (b)`
	)
	dbPath := applySQLite(t, base)
	p := sqlite.New()
	if err := introspect.Merge(introspectSQLite(t, dbPath, introspect.IntrospectionOptions{}), p, []string{writeMigration(t, migration)}); err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	err := introspect.Merge(introspectSQLite(t, dbPath, introspect.IntrospectionOptions{}), sqlite.New(), []string{writeMigration(t, "CREATE INDEX ix ON v (id)")})
	if err == nil || !strings.Contains(err.Error(), "create index ix: table v not found") {
		t.Errorf("Merge(CREATE INDEX on a view) error = %v, want table v not found", err)
	}

	applyParityFixture(t, "sqlite", dbPath, "", migration)
	assertSchemaParity(t, "sqlite", p.Schema(), introspectSQLite(t, dbPath, introspect.IntrospectionOptions{}))
}
