package cli

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/parser"
)

// sqliteDatabase creates a SQLite database file in dir from ddl and returns
// its path.
func sqliteDatabase(t *testing.T, dir, ddl string) string {
	t.Helper()
	path := filepath.Join(dir, "app.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening sqlite database: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	if _, err := db.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("applying DDL: %v", err)
	}
	return path
}

// sqliteSourceConfig returns a SQLite config that reads dbPath under source
// and parses the files in paths.
func sqliteSourceConfig(source config.Source, dbPath string, paths ...string) *config.RootConfig {
	return &config.RootConfig{Input: config.InputConfig{
		Dialect:    config.DialectSQLite,
		Source:     source,
		Schema:     "*",
		Paths:      paths,
		Connection: &config.ConnectionConfig{Database: dbPath},
	}}
}

func tableColumns(schema *parser.Schema) map[string][]string {
	got := make(map[string][]string, len(schema.Tables))
	for _, tbl := range schema.Tables {
		for _, col := range tbl.Columns {
			got[tbl.Name] = append(got[tbl.Name], col.Name)
		}
	}
	return got
}

const sqliteUsersDDL = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);"

func TestParseSchema_SQLiteDatabaseSource(t *testing.T) {
	tests := []struct {
		name       string
		source     config.Source
		dbDDL      string
		fileDDL    string
		introspect *config.IntrospectConfig
		want       map[string][]string
	}{
		{
			name:    "database ignores paths",
			source:  config.SourceDatabase,
			dbDDL:   sqliteUsersDDL,
			fileDDL: "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT); CREATE TABLE posts (id INTEGER PRIMARY KEY);",
			want:    map[string][]string{"users": {"id", "name"}},
		},
		{
			name:   "both applies ADD COLUMN and a new table",
			source: config.SourceBoth,
			dbDDL:  sqliteUsersDDL,
			fileDDL: "ALTER TABLE users ADD COLUMN nickname TEXT;\n" +
				"CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id));",
			want: map[string][]string{
				"posts": {"id", "user_id"},
				"users": {"id", "name", "nickname"},
			},
		},
		{
			name:       "introspect.tables narrows the database",
			source:     config.SourceDatabase,
			dbDDL:      sqliteUsersDDL + "CREATE TABLE posts (id INTEGER PRIMARY KEY);",
			introspect: &config.IntrospectConfig{Tables: []string{"users"}},
			want:       map[string][]string{"users": {"id", "name"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			dbPath := sqliteDatabase(t, dir, tt.dbDDL)
			sqlDir := filepath.Join(dir, "sql")
			if err := os.Mkdir(sqlDir, 0o750); err != nil {
				t.Fatal(err)
			}
			if tt.fileDDL != "" {
				writeSQLFile(t, sqlDir, tt.fileDDL)
			}
			cfg := sqliteSourceConfig(tt.source, dbPath, sqlDir)
			cfg.Input.Introspect = tt.introspect

			schema, err := parseSchema(t.Context(), cfg)
			if err != nil {
				t.Fatalf("parseSchema: %v", err)
			}
			if diff := cmp.Diff(tt.want, tableColumns(schema)); diff != "" {
				t.Errorf("tables (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseSchema_BothSourceCreateOfExistingTable(t *testing.T) {
	dir := t.TempDir()
	dbPath := sqliteDatabase(t, dir, sqliteUsersDDL)
	sqlDir := filepath.Join(dir, "sql")
	if err := os.Mkdir(sqlDir, 0o750); err != nil {
		t.Fatal(err)
	}
	writeSQLFile(t, sqlDir, "CREATE TABLE users (id INTEGER PRIMARY KEY);")

	schema, err := parseSchema(t.Context(), sqliteSourceConfig(config.SourceBoth, dbPath, sqlDir))
	if err == nil {
		t.Fatalf("parseSchema: error = nil, want a duplicate-table error; tables %v", tableColumns(schema))
	}
	if !strings.Contains(err.Error(), "users") {
		t.Errorf("parseSchema: error = %q, want it to name users", err)
	}
	// A schema error, not a failure to reach the database.
	if _, ok := errors.AsType[*connectionError](err); ok {
		t.Errorf("parseSchema: error %q is a *connectionError, want a schema error", err)
	}
}

func TestParseSchema_IntrospectedViewWithAnnotation(t *testing.T) {
	dir := t.TempDir()
	dbPath := sqliteDatabase(t, dir, sqliteUsersDDL+"CREATE VIEW user_names AS SELECT id, name FROM users;")
	viewPath := filepath.Join(dir, "user_names.sql")
	viewSQL := "-- @pk: id\n-- @type name: string\nCREATE VIEW user_names AS SELECT u.id, u.name FROM users u;\n"
	if err := os.WriteFile(viewPath, []byte(viewSQL), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := sqliteSourceConfig(config.SourceDatabase, dbPath)
	cfg.Input.Views = []string{viewPath}

	schema, err := parseSchema(t.Context(), cfg)
	if err != nil {
		t.Fatalf("parseSchema: %v", err)
	}
	if len(schema.Views) != 1 {
		t.Fatalf("parseSchema: %d views, want 1", len(schema.Views))
	}
	type col struct {
		Name          string
		PrimaryKey    bool
		Nullable      bool
		GoTypeLiteral string
	}
	got := make([]col, 0, len(schema.Views[0].Columns))
	for _, c := range schema.Views[0].Columns {
		got = append(got, col{c.Name, c.PrimaryKey, c.Nullable, c.GoTypeLiteral})
	}
	// A @type column is NOT NULL unless @nullable names it, as for a view file.
	want := []col{{"id", true, false, ""}, {"name", false, false, "string"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("view columns (-want +got):\n%s", diff)
	}
}

func TestParseSchema_SQLiteMissingDatabaseFile(t *testing.T) {
	tests := []struct {
		name     string
		database func(path string) string
	}{
		{"path", func(path string) string { return path }},
		{"path with parameters", func(path string) string { return path + "?_pragma=foreign_keys(1)" }},
		{"file URI", func(path string) string { return "file:" + path }},
		{"file URI with parameters", func(path string) string { return "file:" + path + "?_pragma=foreign_keys(1)" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "missing.db")

			_, err := parseSchema(t.Context(), sqliteSourceConfig(config.SourceDatabase, tt.database(dbPath)))
			if _, ok := errors.AsType[*connectionError](err); !ok {
				t.Fatalf("parseSchema: error = %v, want a *connectionError", err)
			}
			if !strings.Contains(err.Error(), "does not exist") {
				t.Errorf("parseSchema: error = %q, want it to say the file does not exist", err)
			}
			if _, statErr := os.Stat(dbPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("stat %s: %v, want the file not to be created", dbPath, statErr)
			}
		})
	}
}

func TestParseSchema_SQLiteFileURI(t *testing.T) {
	dir := t.TempDir()
	dbPath := sqliteDatabase(t, dir, sqliteUsersDDL)

	schema, err := parseSchema(t.Context(), sqliteSourceConfig(config.SourceDatabase, "file:"+dbPath+"?mode=ro"))
	if err != nil {
		t.Fatalf("parseSchema: %v", err)
	}
	if diff := cmp.Diff(map[string][]string{"users": {"id", "name"}}, tableColumns(schema)); diff != "" {
		t.Errorf("tables (-want +got):\n%s", diff)
	}
}

func TestGenerate_SQLiteDatabaseSource(t *testing.T) {
	dir := t.TempDir()
	dbPath := sqliteDatabase(t, dir, sqliteUsersDDL)
	outDir := filepath.Join(dir, "output")
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	content := "input:\n  dialect: sqlite\n  source: database\n  connection:\n    database: " + dbPath + "\n" +
		"output:\n  driver: stdlib\n  dir: " + outDir + "\n  package: models\n  layout: file_per_table\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("generate: %v\nstderr: %s", err, stderr)
	}
	if _, err := os.Stat(filepath.Join(outDir, "user_gen.go")); err != nil {
		t.Errorf("user_gen.go: %v", err)
	}
}

func TestParseSchema_MalformedPostgresURLHidesPassword(t *testing.T) {
	// A literal '/' or '%' in the password breaks the URL (PRD §4.3), and the
	// driver's parse error would quote the text around it.
	for _, password := range []string{"Sekr3t/tail", "Sekr3t%zz"} {
		t.Run(password, func(t *testing.T) {
			cfg := &config.RootConfig{Input: config.InputConfig{
				Dialect:    config.DialectPostgres,
				Source:     config.SourceDatabase,
				Schema:     "*",
				Connection: &config.ConnectionConfig{URL: "postgres://app:" + password + "@localhost:1/app"},
			}}

			_, err := parseSchema(t.Context(), cfg)
			if _, ok := errors.AsType[*connectionError](err); !ok {
				t.Fatalf("parseSchema: error = %v, want a *connectionError", err)
			}
			if strings.Contains(err.Error(), "Sekr3t") || strings.Contains(err.Error(), "%zz") {
				t.Errorf("parseSchema: error = %q, want no part of the password", err)
			}
		})
	}
}

// The database's view stands, so an annotation file whose SELECT drifted
// from it cannot annotate a column the database view lacks.
func TestParseSchema_AnnotationNamesColumnTheDatabaseViewLacks(t *testing.T) {
	dir := t.TempDir()
	dbPath := sqliteDatabase(t, dir, sqliteUsersDDL+"CREATE VIEW user_names AS SELECT name FROM users;")
	viewPath := filepath.Join(dir, "user_names.sql")
	viewSQL := "-- @pk: id\nCREATE VIEW user_names AS SELECT u.id, u.name FROM users u;\n"
	if err := os.WriteFile(viewPath, []byte(viewSQL), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := sqliteSourceConfig(config.SourceDatabase, dbPath)
	cfg.Input.Views = []string{viewPath}

	_, err := parseSchema(t.Context(), cfg)
	if err == nil || !strings.Contains(err.Error(), `view user_names: @pk references unknown column "id"`) {
		t.Errorf("parseSchema: error = %v, want the unknown @pk column", err)
	}
}

// An annotation file for a view the database defines gives only the view's
// name and annotations, so its query need not be one the view parser reads.
func TestParseSchema_AnnotationFileQueryNotRead(t *testing.T) {
	tests := []struct {
		name, viewSQL, wantErr string
	}{
		{
			name:    "compound query",
			viewSQL: "-- @pk: id\nCREATE VIEW ids AS SELECT id FROM users UNION SELECT id FROM users;\n",
		},
		{
			name:    "query that lacks the annotated column",
			viewSQL: "-- @pk: id\nCREATE VIEW ids AS SELECT name FROM users;\n",
		},
		{
			name:    "compound query for a view the database lacks",
			viewSQL: "-- @pk: id\nCREATE VIEW other_ids AS SELECT id FROM users UNION SELECT id FROM users;\n",
			wantErr: "not a simple SELECT",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			dbPath := sqliteDatabase(t, dir, sqliteUsersDDL+"CREATE VIEW ids AS SELECT id FROM users UNION SELECT id FROM users;")
			viewPath := filepath.Join(dir, "view.sql")
			if err := os.WriteFile(viewPath, []byte(tt.viewSQL), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := sqliteSourceConfig(config.SourceDatabase, dbPath)
			cfg.Input.Views = []string{viewPath}

			schema, err := parseSchema(t.Context(), cfg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("parseSchema: error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSchema: %v", err)
			}
			if len(schema.Views) != 1 || len(schema.Views[0].Columns) != 1 || !schema.Views[0].Columns[0].PrimaryKey {
				t.Errorf("parseSchema: views = %+v, want ids with id as its primary key", schema.Views)
			}
		})
	}
}

// Under both, a pending migration that changes a table a view reads changes
// the view's columns too, as under files.
func TestParseSchema_BothSourceViewReadsOverlaidTable(t *testing.T) {
	dir := t.TempDir()
	dbPath := sqliteDatabase(t, dir, sqliteUsersDDL+"CREATE VIEW user_names AS SELECT u.id, u.name FROM users u;")
	sqlDir := filepath.Join(dir, "sql")
	if err := os.Mkdir(sqlDir, 0o750); err != nil {
		t.Fatal(err)
	}
	writeSQLFile(t, sqlDir, "DROP TABLE users; CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")

	schema, err := parseSchema(t.Context(), sqliteSourceConfig(config.SourceBoth, dbPath, sqlDir))
	if err != nil {
		t.Fatalf("parseSchema: %v", err)
	}
	if len(schema.Views) != 1 {
		t.Fatalf("parseSchema: %d views, want 1", len(schema.Views))
	}
	if name := schema.Views[0].Columns[1]; !name.Nullable {
		t.Errorf("user_names.name Nullable = false, want true: the migration drops its NOT NULL")
	}
}

// TestCommands_ParseExitCode pins that only a failure to reach the database
// or read its catalog exits 4. A schema error exits 3 under every source.
func TestCommands_ParseExitCode(t *testing.T) {
	tests := []struct {
		name     string
		source   config.Source
		database func(t *testing.T, dir string) string
		fileDDL  string
		viewSQL  string
		wantCode int
		wantErr  string
	}{
		{
			name:     "missing database file",
			source:   config.SourceDatabase,
			database: func(_ *testing.T, dir string) string { return filepath.Join(dir, "missing.db") },
			wantCode: ExitConnection,
			wantErr:  "does not exist",
		},
		{
			name:   "file that is not a database",
			source: config.SourceBoth,
			database: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, "app.db")
				if err := os.WriteFile(path, []byte("not a sqlite database, just text that fills the header page"), 0o600); err != nil {
					t.Fatal(err)
				}
				return path
			},
			fileDDL:  "CREATE TABLE posts (id INTEGER PRIMARY KEY);",
			wantCode: ExitConnection,
			wantErr:  "not a database",
		},
		{
			name:     "overlay creates a table the database has",
			source:   config.SourceBoth,
			database: func(t *testing.T, dir string) string { return sqliteDatabase(t, dir, sqliteUsersDDL) },
			fileDDL:  "CREATE TABLE users (id INTEGER PRIMARY KEY);",
			wantCode: ExitSchema,
			wantErr:  "users",
		},
		{
			name:   "stored CREATE TABLE the file parser cannot read",
			source: config.SourceDatabase,
			database: func(t *testing.T, dir string) string {
				return sqliteDatabase(t, dir, "CREATE TABLE t (id INTEGER PRIMARY KEY ASC AUTOINCREMENT);")
			},
			wantCode: ExitSchema,
			wantErr:  "parsing stored CREATE TABLE",
		},
		{
			name:   "view annotation names a column the database view lacks",
			source: config.SourceDatabase,
			database: func(t *testing.T, dir string) string {
				return sqliteDatabase(t, dir, sqliteUsersDDL+"CREATE VIEW user_names AS SELECT name FROM users;")
			},
			viewSQL:  "-- @pk: id\nCREATE VIEW user_names AS SELECT u.id, u.name FROM users u;\n",
			wantCode: ExitSchema,
			wantErr:  `@pk references unknown column "id"`,
		},
	}
	for _, tt := range tests {
		for _, command := range [][]string{{"generate"}, {"validate"}, {"diff"}, {"lint"}, {"graphql", "gen"}} {
			t.Run(tt.name+"/"+strings.Join(command, " "), func(t *testing.T) {
				dir := t.TempDir()
				content := "input:\n  dialect: sqlite\n  source: " + string(tt.source) + "\n" +
					"  connection:\n    database: \"" + filepath.ToSlash(tt.database(t, dir)) + "\"\n"
				if tt.fileDDL != "" {
					sqlDir := filepath.Join(dir, "sql")
					if err := os.Mkdir(sqlDir, 0o750); err != nil {
						t.Fatal(err)
					}
					writeSQLFile(t, sqlDir, tt.fileDDL)
					content += "  paths:\n    - \"" + filepath.ToSlash(sqlDir) + "\"\n"
				}
				if tt.viewSQL != "" {
					viewPath := filepath.Join(dir, "view.sql")
					if err := os.WriteFile(viewPath, []byte(tt.viewSQL), 0o600); err != nil {
						t.Fatal(err)
					}
					content += "  views:\n    - \"" + filepath.ToSlash(viewPath) + "\"\n"
				}
				content += "output:\n  driver: stdlib\n  dir: \"" + filepath.ToSlash(filepath.Join(dir, "output")) +
					"\"\n  package: models\n  layout: file_per_table\n"
				cfgPath := filepath.Join(dir, "sqlgen.yml")
				if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}

				_, stderr, err := executeCommand(append(command, "--config", cfgPath)...)
				ee, ok := errors.AsType[*exitError](err)
				if !ok || ee.code != tt.wantCode {
					got := -1
					if ok {
						got = ee.code
					}
					t.Fatalf("error = %v, exit code %d, want %d\nstderr: %s", err, got, tt.wantCode, stderr)
				}
				if !strings.Contains(stderr, tt.wantErr) {
					t.Errorf("stderr = %q, want it to contain %q", stderr, tt.wantErr)
				}
				prefix := "Schema error: "
				switch {
				case command[0] == "validate":
					prefix = "Error: "
				case tt.wantCode == ExitConnection:
					prefix = "Connection error: "
				}
				if !strings.HasPrefix(stderr, prefix) {
					t.Errorf("stderr = %q, want it to start with %q", stderr, prefix)
				}
			})
		}
	}
}

func TestParseExitCode(t *testing.T) {
	dir := t.TempDir()
	writeSQLFile(t, dir, "CREATE TABLE pets (id uuid PRIMARY KEY, owner_id uuid REFERENCES ghosts(id));")
	_, fkErr := parseSchema(t.Context(), &config.RootConfig{Input: config.InputConfig{
		Dialect: config.DialectPostgres,
		Schema:  "*",
		Paths:   []string{dir},
	}})
	if fkErr == nil {
		t.Fatal("parseSchema: error = nil, want the unresolved-FK error")
	}
	connErr := &connectionError{err: errors.New("dial tcp 127.0.0.1:1: connect: connection refused")}

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"unresolved foreign key", fkErr, ExitSchema},
		{"connection failure", connErr, ExitConnection},
		{"wrapped connection failure", fmt.Errorf("introspect: %w", connErr), ExitConnection},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseExitCode(tt.err); got != tt.want {
				t.Errorf("parseExitCode(%q) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}
