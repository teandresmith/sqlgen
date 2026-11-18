package introspect_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/introspect"
)

// inwMySQLSessionDDL runs stmts on one MySQL connection, so a SET SESSION
// among them applies to the statements after it.
func inwMySQLSessionDDL(t *testing.T, stmts ...string) {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("mysql", mysqlConnStr)
	if err != nil {
		t.Fatalf("opening mysql: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("mysql connection: %v", err)
	}
	defer conn.Close() //nolint:errcheck // test cleanup
	for _, stmt := range stmts {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("applying %q: %v", truncate(stmt), err)
		}
	}
}

// inwForeignKeys spells each foreign key of table as "<name> [<columns>]".
func inwForeignKeys(t *testing.T, s *parser.Schema, table string) []string {
	t.Helper()
	cs := filterConstraints(findTable(t, s, table).Constraints, parser.ForeignKey)
	got := make([]string, 0, len(cs))
	for _, c := range cs {
		got = append(got, c.Name+" "+fmt.Sprint(c.Columns))
	}
	return got
}

// TestMySQLIntrospect_RealAsFloat: a REAL column created under the
// REAL_AS_FLOAT sql_mode is stored as float, so it introspects as float,
// while the file parser reads the same DDL as the default sql_mode does.
func TestMySQLIntrospect_RealAsFloat(t *testing.T) {
	requireDatabases(t)
	const ddl = "CREATE TABLE inw_real (id int PRIMARY KEY, r REAL)"
	inwMySQLSessionDDL(t,
		"DROP TABLE IF EXISTS inw_real",
		"SET SESSION sql_mode = CONCAT(@@SESSION.sql_mode, ',REAL_AS_FLOAT')",
		ddl,
	)
	t.Cleanup(func() { execAll(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS inw_real") })

	typeOf := func(c parser.Column) string { return c.Type }
	if got := columnField(t, introspectMySQL(t, "inw_real"), "inw_real", typeOf)["r"]; got != "float" {
		t.Errorf("introspected inw_real.r type = %q, want %q", got, "float")
	}
	if got := columnField(t, parseMySQL(t, ddl), "inw_real", typeOf)["r"]; got != "double" {
		t.Errorf("parsed inw_real.r type = %q, want %q", got, "double")
	}
}

// TestMySQLIntrospect_InlineReferencesBySource: MySQL ignores a foreign key
// written on a column, so the database has neither that key nor its place
// in the numbering of unnamed keys, and no relationship through it. The file
// parser keeps it as a foreign key, numbers it with the table-level key and
// gives its relationship.
func TestMySQLIntrospect_InlineReferencesBySource(t *testing.T) {
	requireDatabases(t)
	const reset = "DROP TABLE IF EXISTS inw_ref_c, inw_ref_p, inw_ref_q"
	const ddl = `CREATE TABLE inw_ref_p (id int PRIMARY KEY);
		CREATE TABLE inw_ref_q (id int PRIMARY KEY);
		CREATE TABLE inw_ref_c (
			id int PRIMARY KEY,
			pid int REFERENCES inw_ref_p (id),
			qid int,
			FOREIGN KEY (qid) REFERENCES inw_ref_q (id)
		)`
	applyParityFixture(t, "mysql", mysqlConnStr, reset, ddl)

	tables := []string{"inw_ref_p", "inw_ref_q", "inw_ref_c"}
	parsed, introspected := parseMySQL(t, ddl), introspectMySQL(t, tables...)

	tests := []struct {
		name          string
		schema        *parser.Schema
		wantKeys      []string
		wantRelations []string
	}{
		{
			name:     "files",
			schema:   parsed,
			wantKeys: []string{"inw_ref_c_ibfk_1 [pid]", "inw_ref_c_ibfk_2 [qid]"},
			wantRelations: []string{
				"O2M inw_ref_p -> inw_ref_c inw_ref_c [pid→id] inw_ref_c_ibfk_1",
				"O2M inw_ref_q -> inw_ref_c inw_ref_c [qid→id] inw_ref_c_ibfk_2",
			},
		},
		{
			name:     "database",
			schema:   introspected,
			wantKeys: []string{"inw_ref_c_ibfk_1 [qid]"},
			wantRelations: []string{
				"O2M inw_ref_q -> inw_ref_c inw_ref_c [qid→id] inw_ref_c_ibfk_1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.wantKeys, inwForeignKeys(t, tt.schema, "inw_ref_c")); diff != "" {
				t.Errorf("inw_ref_c foreign keys (-want +got):\n%s", diff)
			}
			parser.ResolveReferenceNames(tt.schema, "mysql")
			tt.schema.Sort()
			parser.DetectRelationships(tt.schema)
			var got []string
			for _, r := range tt.schema.Relationships {
				if r.TargetTable == "inw_ref_c" && r.Type == parser.OneToMany {
					got = append(got, relationshipString(r))
				}
			}
			if diff := cmp.Diff(tt.wantRelations, got, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("relationships into inw_ref_c (-want +got):\n%s", diff)
			}
		})
	}
}

// TestMySQLIntrospect_NotFolding: MySQL folds a NOT into what it negates, so
// a NOT over NOT IN, NOT BETWEEN or IS NOT NULL reads back without either
// NOT, and a NOT over OR is distributed over its operands.
func TestMySQLIntrospect_NotFolding(t *testing.T) {
	requireDatabases(t)
	const ddl = `CREATE TABLE inw_not (
		id int NOT NULL PRIMARY KEY,
		qty int,
		price int,
		CONSTRAINT inw_not_not_in CHECK (not (qty not in (1, 2))),
		CONSTRAINT inw_not_not_between CHECK (not (qty not between 1 and 5)),
		CONSTRAINT inw_not_not_null CHECK (not (qty is not null)),
		CONSTRAINT inw_not_or CHECK (not (qty > 1 or price > 1))
	)`
	applyParityFixture(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS inw_not", ddl)

	got := make(map[string]string)
	for _, c := range filterConstraints(findTable(t, introspectMySQL(t, "inw_not"), "inw_not").Constraints, parser.Check) {
		got[c.Name] = c.CheckExpression
	}
	want := map[string]string{
		"inw_not_not_in":      "qty in (1, 2)",
		"inw_not_not_between": "qty between 1 and 5",
		"inw_not_not_null":    "qty is null",
		"inw_not_or":          "qty <= 1 and price <= 1",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("introspected CHECK text (-want +got):\n%s", diff)
	}
}

// TestSQLiteIntrospect_ImplicitFKArityMismatch: a key with no column list
// whose parent's primary key has another number of columns keeps no parent
// columns, under both sources.
func TestSQLiteIntrospect_ImplicitFKArityMismatch(t *testing.T) {
	requireDatabases(t)
	tests := []struct {
		name string
		ddl  string
	}{
		{
			name: "one column against a two-column key",
			ddl:  `CREATE TABLE u (a INT, b INT, PRIMARY KEY (a, b)); CREATE TABLE p (id INTEGER PRIMARY KEY, x INT REFERENCES u)`,
		},
		{
			name: "two columns against a one-column key",
			ddl:  `CREATE TABLE u (id INTEGER PRIMARY KEY); CREATE TABLE p (id INTEGER PRIMARY KEY, x INT, y INT, FOREIGN KEY (x, y) REFERENCES u)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, introspected := sqliteSchemas(t, tt.ddl)
			for _, s := range []struct {
				source string
				schema *parser.Schema
			}{{"files", parsed}, {"database", introspected}} {
				fks := filterConstraints(findTable(t, s.schema, "p").Constraints, parser.ForeignKey)
				if len(fks) != 1 {
					t.Errorf("%s: p has %d foreign keys, want 1: %+v", s.source, len(fks), fks)
					continue
				}
				if fks[0].ReferenceTable != "u" || len(fks[0].ReferenceColumns) != 0 {
					t.Errorf("%s: p foreign key references %s%v, want u with no columns", s.source, fks[0].ReferenceTable, fks[0].ReferenceColumns)
				}
			}
		})
	}
}

// TestIntrospect_SchemaFiltersIgnoredWithoutSchemas: a MySQL connection
// reads one database and a SQLite file is one database, so
// input.introspect.schemas and exclude_schemas narrow nothing there.
func TestIntrospect_SchemaFiltersIgnoredWithoutSchemas(t *testing.T) {
	requireDatabases(t)
	execAll(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS inw_filtered; CREATE TABLE inw_filtered (id int PRIMARY KEY)")
	t.Cleanup(func() { execAll(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS inw_filtered") })
	sqlitePath := applySQLite(t, "CREATE TABLE inw_filtered (id INTEGER PRIMARY KEY)")

	dialects := []struct {
		name  string
		intro func() introspect.Introspector
		conn  string
		// own is the name the database's one schema answers to.
		own string
	}{
		{name: "mysql", intro: func() introspect.Introspector { return introspect.NewMySQLIntrospector() }, conn: mysqlConnStr, own: "sqlgen_test"},
		{name: "sqlite", intro: func() introspect.Introspector { return introspect.NewSQLiteIntrospector() }, conn: sqlitePath, own: "main"},
	}
	filters := []struct {
		name           string
		schemas        []string
		excludeSchemas []string
	}{
		{name: "schemas lists another schema", schemas: []string{"inw_elsewhere"}},
		{name: "exclude_schemas lists the database", excludeSchemas: []string{"<own>", "*"}},
	}
	for _, d := range dialects {
		t.Run(d.name, func(t *testing.T) {
			for _, f := range filters {
				t.Run(f.name, func(t *testing.T) {
					exclude := make([]string, 0, len(f.excludeSchemas))
					for _, s := range f.excludeSchemas {
						if s == "<own>" {
							s = d.own
						}
						exclude = append(exclude, s)
					}
					schema := introspectWith(t, d.intro(), d.conn, introspect.IntrospectionOptions{
						Schemas:        f.schemas,
						ExcludeSchemas: exclude,
						Tables:         []string{"inw_filtered"},
					})
					if diff := cmp.Diff([]string{"inw_filtered"}, tableNames(schema)); diff != "" {
						t.Errorf("introspected tables (-want +got):\n%s", diff)
					}
				})
			}
		})
	}
}
