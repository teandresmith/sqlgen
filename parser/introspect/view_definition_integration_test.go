package introspect_test

import (
	"context"
	"database/sql"
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
