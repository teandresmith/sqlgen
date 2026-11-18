package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/internal/livedb"
)

// qvwUsersPosts is the PostgreSQL database every case below introspects:
// posts.user_id references users, and every case's introspect.tables keeps
// posts and leaves users out.
const qvwUsersPosts = `CREATE TABLE users (id integer PRIMARY KEY, name text NOT NULL);
CREATE TABLE posts (id integer PRIMARY KEY, title text NOT NULL, user_id integer REFERENCES users(id));
`

// TestGenerate_IntrospectFilterAndFileForeignKeys runs source: both against
// a PostgreSQL database whose posts table references users, with
// introspect.tables keeping only posts, and a SQL file beside it. The key
// the database holds is not checked; a key or ALTER TABLE in the file that
// reaches the left-out users table finds no such table.
func TestGenerate_IntrospectFilterAndFileForeignKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live-database test in short mode")
	}
	conn := livedb.NewPostgresDatabase(t)
	livedb.ExecSQL(t, "pgx", conn, qvwUsersPosts)

	t.Run("database key into the left-out table", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		outDir := filepath.Join(dir, "models")
		src := testSource{dialect: "postgres", driver: "pgx", source: "both", url: conn, outputDir: outDir}
		src.tables = []string{"posts"}
		src.paths = []string{writeFile(t, filepath.Join(dir, "migrations"), "002.sql",
			"CREATE TABLE notes (id integer PRIMARY KEY, body text NOT NULL);\n")}
		if stderr, code := runSource(t, dir, src, "generate", "--manifest-timestamp", "2026-01-01T00:00:00Z"); code != 0 {
			t.Fatalf("generate exited %d, want 0\nstderr: %s", code, stderr)
		}
		entities := manifestEntities(t, outDir)
		if _, ok := entities["users"]; ok {
			t.Error("users was generated, want it filtered out")
		}
		for _, table := range []string{"posts", "notes"} {
			if _, ok := entities[table]; !ok {
				t.Errorf("%s was not generated", table)
			}
		}
		if got := relationshipTargets(entities["posts"]); len(got) != 0 {
			t.Errorf("posts relationships = %v, want none into the filtered-out users", got)
		}
	})

	tests := []struct {
		name    string
		command string
		file    string
		wantErr string
	}{
		{
			name:    "file key into the left-out table under generate",
			command: "generate",
			file:    "CREATE TABLE comments (id integer PRIMARY KEY, user_id integer REFERENCES users(id));\n",
			wantErr: "foreign key public.comments(user_id) references public.users, which no parsed table defines",
		},
		{
			name:    "file key into the left-out table under validate",
			command: "validate",
			file:    "CREATE TABLE comments (id integer PRIMARY KEY, user_id integer REFERENCES users(id));\n",
			wantErr: "foreign key public.comments(user_id) references public.users, which no parsed table defines",
		},
		{
			name:    "file ALTER TABLE on the left-out table",
			command: "generate",
			file:    "ALTER TABLE users ADD COLUMN nickname text;\n",
			wantErr: "table public.users not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			src := testSource{dialect: "postgres", driver: "pgx", source: "both", url: conn, outputDir: filepath.Join(dir, "models")}
			src.tables = []string{"posts"}
			src.paths = []string{writeFile(t, filepath.Join(dir, "migrations"), "002.sql", tt.file)}
			stderr, code := runSource(t, dir, src, tt.command)
			if code != ExitSchema {
				t.Fatalf("%s exited %d, want %d\nstderr: %s", tt.command, code, ExitSchema, stderr)
			}
			if !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("%s stderr = %q, want it to contain %q", tt.command, stderr, tt.wantErr)
			}
		})
	}
}
