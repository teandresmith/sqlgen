package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/internal/livedb"
	"github.com/teandresmith/sqlgen/parser"
)

// inwDialect is one dialect's database for the source tests here.
type inwDialect struct {
	dialect config.Dialect
	// newDatabase creates an empty database and returns its connection URL;
	// apply runs statements on it.
	newDatabase func(t *testing.T) string
	apply       func(t *testing.T, conn, statements string)
	// users is a users table with id, a NOT NULL name and a nullable email;
	// posts a posts table with id and title.
	users, posts string
	// addNickname adds a nullable column to users; addBody one to posts.
	addNickname, addBody string
}

// inwPostgres, inwMySQL and inwSQLite are each dialect's database for
// the source tests here.
var inwPostgres = inwDialect{
	dialect:     config.DialectPostgres,
	newDatabase: livedb.NewPostgresDatabase,
	apply:       func(t *testing.T, conn, s string) { t.Helper(); livedb.ExecSQL(t, "pgx", conn, s) },
	users:       "CREATE TABLE users (id integer PRIMARY KEY, name text NOT NULL, email text);\n",
	posts:       "CREATE TABLE posts (id integer PRIMARY KEY, title text NOT NULL);\n",
	addNickname: "ALTER TABLE users ADD COLUMN nickname text;\n",
	addBody:     "ALTER TABLE posts ADD COLUMN body text;\n",
}

var inwMySQL = inwDialect{
	dialect:     config.DialectMySQL,
	newDatabase: livedb.NewMySQLDatabase,
	apply: func(t *testing.T, conn, s string) {
		t.Helper()
		livedb.ExecSQL(t, "mysql", conn+"?multiStatements=true", s)
	},
	users:       "CREATE TABLE users (id int NOT NULL PRIMARY KEY, name varchar(100) NOT NULL, email varchar(100));\n",
	posts:       "CREATE TABLE posts (id int NOT NULL PRIMARY KEY, title varchar(100) NOT NULL);\n",
	addNickname: "ALTER TABLE users ADD COLUMN nickname varchar(50);\n",
	addBody:     "ALTER TABLE posts ADD COLUMN body text;\n",
}

var inwSQLite = inwDialect{
	dialect:     config.DialectSQLite,
	newDatabase: livedb.NewSQLiteDatabase,
	apply:       func(t *testing.T, conn, s string) { t.Helper(); livedb.ExecSQL(t, "sqlite", conn, s) },
	users:       "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT);\n",
	posts:       "CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT NOT NULL);\n",
	addNickname: "ALTER TABLE users ADD COLUMN nickname TEXT;\n",
	addBody:     "ALTER TABLE posts ADD COLUMN body TEXT;\n",
}

// inwConfig returns a config that reads the database at conn under source,
// with the schema files in paths and the view files in views.
func inwConfig(d inwDialect, source config.Source, conn string, paths, views []string) *config.RootConfig {
	return &config.RootConfig{Input: config.InputConfig{
		Dialect:    d.dialect,
		Source:     source,
		Schema:     "*",
		Paths:      paths,
		Views:      views,
		Connection: &config.ConnectionConfig{URL: conn},
	}}
}

// inwViewColumn is what the overlay tests compare of a view column.
type inwViewColumn struct {
	Name          string
	PrimaryKey    bool
	Nullable      bool
	GoTypeLiteral string
}

// inwUserNames returns the columns of view user_names in schema.
func inwUserNames(t *testing.T, schema *parser.Schema) []inwViewColumn {
	t.Helper()
	var view *parser.View
	for i := range schema.Views {
		if schema.Views[i].Name == "user_names" {
			if view != nil {
				t.Fatalf("schema has two views user_names: %+v", schema.Views)
			}
			view = &schema.Views[i]
		}
	}
	if view == nil {
		t.Fatalf("schema has no view user_names: %+v", schema.Views)
	}
	got := make([]inwViewColumn, 0, len(view.Columns))
	for _, c := range view.Columns {
		got = append(got, inwViewColumn{c.Name, c.PrimaryKey, c.Nullable, c.GoTypeLiteral})
	}
	return got
}

const inwUserNamesView = "CREATE VIEW user_names AS SELECT u.id, u.name FROM users u;\n"

// TestParseSchema_ViewAnnotationOverlay: an input.views file for a view the
// database defines gives only the view's name and annotations. The database
// view's columns are used whatever the file's SELECT says, a SELECT the
// view parser could not read is not read, @type overrides the introspected
// column's type and @nullable makes a NOT NULL column nullable.
func TestParseSchema_ViewAnnotationOverlay(t *testing.T) {
	tests := []struct {
		name string
		file string
		want []inwViewColumn
	}{
		{
			name: "no annotation file",
			want: []inwViewColumn{{"id", false, false, ""}, {"name", false, false, ""}},
		},
		{
			name: "SELECT with another column",
			file: "-- @pk: id\nCREATE VIEW user_names AS SELECT u.id, u.name, u.email FROM users u;\n",
			want: []inwViewColumn{{"id", true, false, ""}, {"name", false, false, ""}},
		},
		{
			name: "compound SELECT",
			file: "-- @pk: id\nCREATE VIEW user_names AS SELECT id, name FROM users UNION SELECT id, name FROM users;\n",
			want: []inwViewColumn{{"id", true, false, ""}, {"name", false, false, ""}},
		},
		{
			name: "type override",
			file: "-- @type name: []byte\n" + inwUserNamesView,
			want: []inwViewColumn{{"id", false, false, ""}, {"name", false, false, "[]byte"}},
		},
		{
			name: "nullable",
			file: "-- @nullable: name\n" + inwUserNamesView,
			want: []inwViewColumn{{"id", false, false, ""}, {"name", false, true, ""}},
		},
	}
	for _, tc := range []struct {
		name    string
		dialect inwDialect
	}{
		{name: "postgres", dialect: inwPostgres},
		{name: "mysql", dialect: inwMySQL},
		{name: "sqlite", dialect: inwSQLite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.dialect
			conn := d.newDatabase(t)
			d.apply(t, conn, d.users+inwUserNamesView)
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					var views []string
					if tt.file != "" {
						views = []string{writeFile(t, filepath.Join(t.TempDir(), "views"), "user_names.sql", tt.file)}
					}
					schema, err := parseSchema(t.Context(), inwConfig(d, config.SourceDatabase, conn, nil, views))
					if err != nil {
						t.Fatalf("parseSchema: %v", err)
					}
					if diff := cmp.Diff(tt.want, inwUserNames(t, schema)); diff != "" {
						t.Errorf("user_names columns (-want +got):\n%s", diff)
					}
				})
			}

			// The compound SELECT is one an annotation file alone could
			// not hold.
			t.Run("compound SELECT for a view the database lacks", func(t *testing.T) {
				file := writeFile(t, filepath.Join(t.TempDir(), "views"), "other_names.sql",
					"-- @pk: id\nCREATE VIEW other_names AS SELECT id, name FROM users UNION SELECT id, name FROM users;\n")
				_, err := parseSchema(t.Context(), inwConfig(d, config.SourceDatabase, conn, nil, []string{file}))
				const want = "CREATE VIEW query is not a simple SELECT"
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Errorf("parseSchema: error = %v, want the compound query refused with %q", err, want)
				}
			})
		})
	}
}

// TestParseSchema_BothSourcePendingViewChange: under source: both an
// annotation file whose SELECT changes the view changes nothing until the
// change has run on the database.
func TestParseSchema_BothSourcePendingViewChange(t *testing.T) {
	const changed = "CREATE VIEW user_names AS SELECT u.id, u.name, u.email FROM users u;\n"
	for _, tc := range []struct {
		name    string
		dialect inwDialect
	}{
		{name: "postgres", dialect: inwPostgres},
		{name: "mysql", dialect: inwMySQL},
		{name: "sqlite", dialect: inwSQLite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.dialect
			conn := d.newDatabase(t)
			d.apply(t, conn, d.users+inwUserNamesView)
			dir := t.TempDir()
			paths := []string{writeFile(t, filepath.Join(dir, "migrations"), "001_nickname.sql", d.addNickname)}
			views := []string{writeFile(t, filepath.Join(dir, "views"), "user_names.sql", "-- @pk: id\n"+changed)}
			cfg := inwConfig(d, config.SourceBoth, conn, paths, views)

			schema, err := parseSchema(t.Context(), cfg)
			if err != nil {
				t.Fatalf("parseSchema before the change runs: %v", err)
			}
			if diff := cmp.Diff([]inwViewColumn{{"id", true, false, ""}, {"name", false, false, ""}}, inwUserNames(t, schema)); diff != "" {
				t.Errorf("user_names columns before the change runs (-want +got):\n%s", diff)
			}

			d.apply(t, conn, "DROP VIEW user_names;\n"+changed)
			schema, err = parseSchema(t.Context(), cfg)
			if err != nil {
				t.Fatalf("parseSchema after the change runs: %v", err)
			}
			want := []inwViewColumn{{"id", true, false, ""}, {"name", false, false, ""}, {"email", false, true, ""}}
			if diff := cmp.Diff(want, inwUserNames(t, schema)); diff != "" {
				t.Errorf("user_names columns after the change runs (-want +got):\n%s", diff)
			}
		})
	}
}

// TestParseSchema_BothSourceIntrospectFilters: input.introspect.tables
// narrows the database's tables under source: both as under database, and
// the files apply on top of what it keeps.
func TestParseSchema_BothSourceIntrospectFilters(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dialect inwDialect
	}{
		{name: "postgres", dialect: inwPostgres},
		{name: "mysql", dialect: inwMySQL},
		{name: "sqlite", dialect: inwSQLite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.dialect
			conn := d.newDatabase(t)
			d.apply(t, conn, d.users+d.posts)
			paths := []string{writeFile(t, filepath.Join(t.TempDir(), "migrations"), "001_nickname.sql", d.addNickname)}

			for _, tt := range []struct {
				name   string
				source config.Source
				want   map[string][]string
			}{
				{name: "database", source: config.SourceDatabase, want: map[string][]string{"users": {"id", "name", "email"}}},
				{name: "both", source: config.SourceBoth, want: map[string][]string{"users": {"id", "name", "email", "nickname"}}},
			} {
				t.Run(tt.name, func(t *testing.T) {
					cfg := inwConfig(d, tt.source, conn, paths, nil)
					cfg.Input.Introspect = &config.IntrospectConfig{Tables: []string{"users"}}
					schema, err := parseSchema(t.Context(), cfg)
					if err != nil {
						t.Fatalf("parseSchema: %v", err)
					}
					if diff := cmp.Diff(tt.want, tableColumns(schema)); diff != "" {
						t.Errorf("tables (-want +got):\n%s", diff)
					}
				})
			}
		})
	}
}

// TestParseSchema_BothSourceFileOrder: under source: both the files in paths
// are parsed as under files: a directory's files in lexical order, its down
// migrations skipped, and listed files in the order listed.
func TestParseSchema_BothSourceFileOrder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dialect inwDialect
	}{
		{name: "postgres", dialect: inwPostgres},
		{name: "mysql", dialect: inwMySQL},
		{name: "sqlite", dialect: inwSQLite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.dialect
			conn := d.newDatabase(t)
			d.apply(t, conn, d.users)

			t.Run("directory", func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "migrations")
				// Written out of order: 010 alters the table 002 creates, and
				// the down migration would drop it.
				writeFile(t, dir, "010_body.sql", d.addBody)
				writeFile(t, dir, "002_posts.sql", d.posts)
				writeFile(t, dir, "010_body.down.sql", "DROP TABLE posts;\n")
				schema, err := parseSchema(t.Context(), inwConfig(d, config.SourceBoth, conn, []string{dir}, nil))
				if err != nil {
					t.Fatalf("parseSchema: %v", err)
				}
				want := map[string][]string{"users": {"id", "name", "email"}, "posts": {"id", "title", "body"}}
				if diff := cmp.Diff(want, tableColumns(schema)); diff != "" {
					t.Errorf("tables (-want +got):\n%s", diff)
				}
			})

			t.Run("listed files", func(t *testing.T) {
				dir := t.TempDir()
				// Listed against lexical order: 900 creates the table 100
				// alters.
				create := writeFile(t, dir, "900_posts.sql", d.posts)
				alter := writeFile(t, dir, "100_body.sql", d.addBody)
				schema, err := parseSchema(t.Context(), inwConfig(d, config.SourceBoth, conn, []string{create, alter}, nil))
				if err != nil {
					t.Fatalf("parseSchema: %v", err)
				}
				want := map[string][]string{"users": {"id", "name", "email"}, "posts": {"id", "title", "body"}}
				if diff := cmp.Diff(want, tableColumns(schema)); diff != "" {
					t.Errorf("tables (-want +got):\n%s", diff)
				}
			})
		})
	}
}

// TestGenerate_PostgresVirtualGeneratedColumnHasNoWriteSurface: a
// PostgreSQL 18 VIRTUAL generated column, which only introspection reads,
// is in the model and in no write input.
func TestGenerate_PostgresVirtualGeneratedColumnHasNoWriteSurface(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live-database test in short mode")
	}
	ctx := t.Context()
	container, err := tcpostgres.Run(ctx, "postgres:18-alpine",
		tcpostgres.WithDatabase("sqlgen"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("starting postgres 18: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) }) //nolint:usetesting // t.Context is cancelled before cleanup runs, so terminating with it would leak the container
	conn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres 18 connection string: %v", err)
	}
	// With neither STORED nor VIRTUAL, PostgreSQL 18 makes the column VIRTUAL.
	livedb.ExecSQL(t, "pgx", conn, `CREATE TABLE items (
		id bigserial PRIMARY KEY,
		qty integer NOT NULL,
		total integer GENERATED ALWAYS AS (qty * 2) VIRTUAL,
		vtotal integer GENERATED ALWAYS AS (qty + 1)
	)`)

	dir := t.TempDir()
	t.Chdir(dir)
	outDir := filepath.Join(dir, "models")
	generateSource(t, dir, testSource{dialect: "postgres", driver: "pgx", source: "database", url: conn, outputDir: outDir})
	files := parseGeneratedFiles(t, outDir)

	for _, c := range []struct {
		typeName string
		want     []string
	}{
		{typeName: "Item", want: []string{"ID", "Qty", "Total", "Vtotal"}},
		{typeName: "CreateItemInput", want: []string{"Qty"}},
		{typeName: "UpdateItemInput", want: []string{"Qty"}},
	} {
		if diff := cmp.Diff(c.want, structFieldNames(files, c.typeName)); diff != "" {
			t.Errorf("%s fields mismatch (-want +got):\n%s", c.typeName, diff)
		}
	}
	if diff := cmp.Diff([]string{"qty"}, stringConstValues(files, "ItemIncrementColumn")); diff != "" {
		t.Errorf("ItemIncrementColumn values mismatch (-want +got):\n%s", diff)
	}
}
