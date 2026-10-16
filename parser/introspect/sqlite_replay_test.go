package introspect

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// TestMerge_SQLiteReplay pins how source: both checks SQLite migrations
// against SQLite itself (PRD §6.1), on a database schema built by hand: a
// statement SQLite refuses and a view a file leaves failing are errors
// naming the file, and a view a file drops stands. A database object that
// names a collation, function or virtual-table module SQLite lacks is
// replayed as the database has it, and a view that fails for a virtual
// table's missing module is left out with a warning. A file that needs
// one is a warning, and it and the files after it are not checked, as is a
// file that runs ATTACH DATABASE or VACUUM, which writes no file; a view
// such a file leaves failing keeps its columns.
func TestMerge_SQLiteReplay(t *testing.T) {
	stored := []parser.SchemaStatement{
		{Type: "table", Name: "users", SQL: "CREATE TABLE users (id INTEGER PRIMARY KEY, role TEXT, email TEXT)"},
		{Type: "table", Name: "tags", SQL: "CREATE TABLE tags (id INTEGER PRIMARY KEY, uid INTEGER)"},
		{Type: "view", Name: "v", SQL: "CREATE VIEW v AS SELECT id, role FROM users"},
		{Type: "view", Name: "vt", SQL: "CREATE VIEW vt AS SELECT u.id FROM users u WHERE u.id IN (SELECT uid FROM tags)"},
	}
	// collated has a collation SQLite lacks on users.email, which a view
	// compares, so the view does not run, and on a unique column.
	collated := []parser.SchemaStatement{
		{Type: "table", Name: "users", SQL: "CREATE TABLE users (id INTEGER PRIMARY KEY, role TEXT, email TEXT COLLATE appcoll, code TEXT UNIQUE COLLATE \"appcoll\")"},
		{Type: "table", Name: "tags", SQL: "CREATE TABLE tags (id INTEGER PRIMARY KEY, uid INTEGER)"},
		{Type: "view", Name: "v", SQL: "CREATE VIEW v AS SELECT id, role FROM users"},
		{Type: "view", Name: "vc", SQL: "CREATE VIEW vc AS SELECT id FROM users WHERE email = 'x'"},
	}
	// collatedIndex also has an index on the collated column.
	collatedIndex := slices.Insert(slices.Clone(collated), 2,
		parser.SchemaStatement{Type: "index", Name: "users_email", SQL: "CREATE INDEX users_email ON users (email)"})
	// indexed has an index on a function SQLite lacks.
	indexed := slices.Insert(slices.Clone(stored), 2,
		parser.SchemaStatement{Type: "index", Name: "ix", SQL: `CREATE INDEX ix ON users ("appfn"(email)) WHERE appfn(role) IS NOT NULL`})
	// virtual has a virtual table whose module SQLite lacks, and a view
	// that reads it.
	virtual := append([]parser.SchemaStatement{
		{Type: "table", Name: "words", SQL: "CREATE VIRTUAL TABLE words USING nosuchmodule(w)"},
		{Type: "view", Name: "vw", SQL: "CREATE VIEW vw AS SELECT w FROM words"},
	}, stored...)
	unchecked := func(name string) []string {
		return []string{"{file} needs " + name + ", which this SQLite lacks, so SQLite does not check it or the SQL files after it, and the views' columns do not follow them"}
	}
	attached := []string{"{file} runs ATTACH DATABASE or VACUUM, which the check does not run, so SQLite does not check it or the SQL files after it, and the views' columns do not follow them"}
	tests := []struct {
		name   string
		stored []parser.SchemaStatement
		// migrations are the SQL files, in order, with {dir} for their
		// directory.
		migrations []string
		// wantErr is part of the error; wantWarnings the schema's warnings
		// when there is none, with {file} for the first file's path.
		wantErr      string
		wantWarnings []string
		// wantColumns, when set, is each view's column names after Merge.
		wantColumns map[string][]string
		// noFile is a file of {dir} Merge must not create.
		noFile string
	}{
		{
			name:       "a column no view reads",
			stored:     stored,
			migrations: []string{"ALTER TABLE users DROP COLUMN email;"},
		},
		{
			name:       "SQLite refuses the drop",
			stored:     stored,
			migrations: []string{"ALTER TABLE users DROP COLUMN role;"},
			wantErr:    ": SQLite refuses it: SQL logic error: error in view v after drop column: no such column: role (1)",
		},
		{
			name:       "the file leaves a view failing",
			stored:     stored,
			migrations: []string{"DROP TABLE tags;"},
			wantErr:    " leaves view vt failing: SQL logic error: no such table: main.tags (1); under source: both the database's view stands",
		},
		{
			name:       "a table created again",
			stored:     stored,
			migrations: []string{"DROP TABLE tags; CREATE TABLE tags (id INTEGER PRIMARY KEY, uid INTEGER);"},
		},
		{
			name:       "the file drops the view",
			stored:     stored,
			migrations: []string{"DROP VIEW vt; DROP TABLE tags;"},
		},
		{
			name:       "a table with a collation SQLite lacks",
			stored:     collated,
			migrations: []string{"ALTER TABLE users ADD COLUMN c TEXT; ALTER TABLE users RENAME COLUMN email TO mail;"},
		},
		{
			name:       "a view that does not run is still checked by SQLite's drop",
			stored:     collated,
			migrations: []string{"ALTER TABLE users DROP COLUMN email;"},
			wantErr:    ": SQLite refuses it: SQL logic error: error in view vc after drop column: no such column: email (1)",
		},
		{
			name:         "an index on a column with a collation SQLite lacks",
			stored:       collatedIndex,
			migrations:   []string{"ALTER TABLE users RENAME COLUMN role TO kind;"},
			wantWarnings: unchecked("collation appcoll"),
		},
		{
			name:       "a view that does not run is not checked after the file",
			stored:     collated,
			migrations: []string{"DROP VIEW v; DROP TABLE users;"},
		},
		{
			name:       "a column an index on a function SQLite lacks reads",
			stored:     indexed,
			migrations: []string{"ALTER TABLE users DROP COLUMN email;"},
			wantErr:    ": SQLite refuses it: SQL logic error: error in index ix after drop column: no such column: email (1)",
		},
		{
			name:       "a column an index on a function SQLite lacks does not read",
			stored:     indexed,
			migrations: []string{"ALTER TABLE users ADD COLUMN c TEXT; ALTER TABLE users RENAME COLUMN email TO mail; ALTER TABLE users DROP COLUMN c;"},
		},
		{
			name:         "a virtual table SQLite has no module for",
			stored:       virtual,
			migrations:   []string{"ALTER TABLE users DROP COLUMN email;"},
			wantWarnings: []string{"virtual tables words: this SQLite has no module for them, so the SQL files are not checked against views vw, which read them"},
		},
		{
			name:       "a file drops a virtual table SQLite has no module for",
			stored:     virtual,
			migrations: []string{"DROP VIEW vw; DROP TABLE words;", "ALTER TABLE users DROP COLUMN role;"},
			wantWarnings: append([]string{"virtual tables words: this SQLite has no module for them, so the SQL files are not checked against views vw, which read them"},
				unchecked("module nosuchmodule")...),
		},
		{
			name:         "a file needs a collation SQLite lacks",
			stored:       stored,
			migrations:   []string{"ALTER TABLE users ADD COLUMN c TEXT COLLATE appcoll; ALTER TABLE users DROP COLUMN role;", "DROP TABLE tags;"},
			wantWarnings: unchecked("collation appcoll"),
		},
		{
			name:         "a file needs a function SQLite lacks",
			stored:       stored,
			migrations:   []string{"CREATE INDEX ix ON users (appfn(email));"},
			wantWarnings: unchecked("function appfn"),
		},
		{
			name:         "a file runs a function SQLite lacks",
			stored:       indexed,
			migrations:   []string{"INSERT INTO users (id, role, email) VALUES (1, 'a', 'b');"},
			wantWarnings: unchecked("function appfn"),
		},
		{
			name:         "a file stops being checked after it breaks a view",
			stored:       indexed,
			migrations:   []string{"DROP TABLE tags; INSERT INTO users (id, role, email) VALUES (1, 'a', 'b');"},
			wantWarnings: unchecked("function appfn"),
			wantColumns:  map[string][]string{"v": {"id", "role"}, "vt": {"stale"}},
		},
		{
			name:         "a later file's rename after a file SQLite cannot check",
			stored:       indexed,
			migrations:   []string{"INSERT INTO users (id, role, email) VALUES (1, 'a', 'b');", "ALTER TABLE users RENAME COLUMN role TO kind;"},
			wantWarnings: unchecked("function appfn"),
			// v is read as the checked files leave it, without the rename.
			wantColumns: map[string][]string{"v": {"id", "role"}, "vt": {"id"}},
		},
		{
			name:         "a file attaches a database",
			stored:       stored,
			migrations:   []string{"ATTACH DATABASE '{dir}/side.db' AS aux; CREATE TABLE aux.t (a int);", "ALTER TABLE users DROP COLUMN role;"},
			wantWarnings: attached,
			noFile:       "side.db",
		},
		{
			name:         "a file vacuums into a database",
			stored:       stored,
			migrations:   []string{"VACUUM INTO '{dir}/copy.db';"},
			wantWarnings: attached,
			noFile:       "copy.db",
		},
		{
			name:         "a file vacuums",
			stored:       stored,
			migrations:   []string{"PRAGMA auto_vacuum = FULL; VACUUM;"},
			wantWarnings: attached,
		},
		{
			name:         "a file needs a module SQLite lacks",
			stored:       stored,
			migrations:   []string{"CREATE VIRTUAL TABLE w USING appmodule(a);"},
			wantWarnings: append([]string{"CREATE VIRTUAL TABLE w skipped — virtual tables are not generated"}, unchecked("module appmodule")...),
		},
		{
			name:         "a file creates a view again with a function SQLite lacks",
			stored:       stored,
			migrations:   []string{"DROP VIEW v; CREATE VIEW v AS SELECT id, appfn(role) AS role FROM users;"},
			wantWarnings: append([]string{"CREATE VIEW v skipped in DDL file — use introspection or define the view in input.views"}, unchecked("function appfn")...),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := &parser.Schema{
				Tables: []parser.Table{
					{Name: "users", Columns: []parser.Column{{Name: "id", Type: "integer", PrimaryKey: true}, {Name: "role", Type: "text", Nullable: true}, {Name: "email", Type: "text", Nullable: true}}},
					{Name: "tags", Columns: []parser.Column{{Name: "id", Type: "integer", PrimaryKey: true}, {Name: "uid", Type: "integer", Nullable: true}}},
				},
				// Stale columns show which views Merge reads again.
				Views:        []parser.View{{Name: "v", Columns: []parser.Column{{Name: "stale"}}}, {Name: "vt", Columns: []parser.Column{{Name: "stale"}}}},
				SQLiteSchema: tt.stored,
			}
			dir := t.TempDir()
			var files []string
			for i, migration := range tt.migrations {
				file := filepath.Join(dir, fmt.Sprintf("%03d.sql", i+1))
				if err := os.WriteFile(file, []byte(strings.ReplaceAll(migration, "{dir}", dir)), 0o600); err != nil {
					t.Fatal(err)
				}
				files = append(files, file)
			}
			p := sqlite.New()
			err := Merge(base, p, files)
			if tt.noFile != "" {
				if _, statErr := os.Stat(filepath.Join(dir, tt.noFile)); !errors.Is(statErr, fs.ErrNotExist) {
					t.Errorf("Merge() created %s (stat error %v), want no file", tt.noFile, statErr)
				}
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Merge() error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Merge() error: %v", err)
			}
			var want []string
			for _, w := range tt.wantWarnings {
				want = append(want, strings.ReplaceAll(w, "{file}", files[0]))
			}
			if diff := cmp.Diff(want, p.Schema().Warnings); diff != "" {
				t.Errorf("warnings (-want +got):\n%s", diff)
			}
			if tt.wantColumns == nil {
				return
			}
			got := make(map[string][]string)
			for _, view := range p.Schema().Views {
				for _, c := range view.Columns {
					got[view.Name] = append(got[view.Name], c.Name)
				}
			}
			if diff := cmp.Diff(tt.wantColumns, got); diff != "" {
				t.Errorf("view columns (-want +got):\n%s", diff)
			}
		})
	}
}
