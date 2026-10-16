package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/internal/livedb"
)

// dialectFixture is one dialect's setup for the live-database CLI tests.
// A test that does not apply to a dialect skips with the reason held here.
type dialectFixture struct {
	dialect string
	driver  string // output.driver

	// newDatabase creates an empty database and returns its
	// input.connection.url; apply runs SQL on it.
	newDatabase func(t *testing.T) string
	apply       func(t *testing.T, conn, statements string)

	// ddl and view are the representative schema both sources describe. The
	// database gets both; under files, ddl is the schema file and view the
	// input.views file.
	ddl  string
	view string

	// users is a users table with id and name, posts a posts table with id
	// and title, and addNickname a migration that adds a column to users.
	// postsWithUserFK is a posts table whose user_id references users.
	users           string
	posts           string
	addNickname     string
	postsWithUserFK string

	// unreachable lists input.connection.url values sqlgen cannot open.
	unreachable []unreachableDatabase

	// noSchemaFilter and noFKTargetCheck say why the introspect.schemas
	// filter and the check for a foreign key whose target no table defines
	// do not apply.
	noSchemaFilter  string
	noFKTargetCheck string
}

type unreachableDatabase struct {
	name string
	url  func(t *testing.T) string
}

func (f dialectFixture) source(src, conn, outDir string) testSource {
	return testSource{dialect: f.dialect, driver: f.driver, source: src, url: conn, outputDir: outDir}
}

const (
	pgMatchDDL = `CREATE SCHEMA billing;
CREATE TABLE billing.accounts (
	id integer PRIMARY KEY,
	name text NOT NULL
);
CREATE TYPE user_status AS ENUM ('active', 'suspended');
CREATE DOMAIN positive_int AS integer CONSTRAINT positive_int_check CHECK (VALUE > 0);
CREATE TABLE users (
	id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	uid uuid NOT NULL DEFAULT gen_random_uuid(),
	email text NOT NULL UNIQUE,
	status user_status NOT NULL DEFAULT 'active',
	score positive_int,
	account_id integer REFERENCES billing.accounts(id),
	balance numeric(12,2) NOT NULL DEFAULT 0,
	doubled numeric(14,2) GENERATED ALWAYS AS (balance * 2) STORED,
	profile jsonb,
	created_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE users IS 'Application users';
COMMENT ON COLUMN users.email IS 'Login address';
COMMENT ON TYPE user_status IS 'Lifecycle state of a user';
COMMENT ON DOMAIN positive_int IS 'Integer above zero';
CREATE TABLE memberships (
	user_id bigint NOT NULL REFERENCES users(id),
	account_id integer NOT NULL REFERENCES billing.accounts(id),
	role text NOT NULL DEFAULT 'member',
	PRIMARY KEY (account_id, user_id)
);
`
	pgMatchView = `-- @pk: id
CREATE VIEW active_users AS
SELECT u.id, u.email, u.status FROM users u WHERE u.status = 'active';
`

	mysqlMatchDDL = `CREATE TABLE accounts (
	id int unsigned NOT NULL AUTO_INCREMENT PRIMARY KEY,
	name varchar(100) NOT NULL DEFAULT '',
	plan ENUM('free','pro') NOT NULL DEFAULT 'free',
	active BOOLEAN NOT NULL DEFAULT TRUE,
	flags SET('beta','staff') NOT NULL DEFAULT '',
	balance decimal(12,2) NOT NULL DEFAULT 0.00,
	doubled decimal(14,2) GENERATED ALWAYS AS (balance * 2) STORED,
	visits bigint unsigned NOT NULL DEFAULT 0,
	created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
) COMMENT='Customer accounts';
CREATE TABLE members (
	account_id int unsigned NOT NULL,
	user_name varchar(50) NOT NULL COMMENT 'Display name',
	role varchar(20) NOT NULL DEFAULT 'member',
	PRIMARY KEY (account_id, user_name),
	CONSTRAINT members_account_fk FOREIGN KEY (account_id) REFERENCES accounts (id)
);
`
	mysqlMatchView = `CREATE VIEW pro_accounts AS
SELECT a.id, a.name FROM accounts a WHERE a.plan = 'pro';
`

	sqliteMatchDDL = `CREATE TABLE categories (
	id INTEGER PRIMARY KEY,
	parent_id INTEGER REFERENCES categories(id),
	name TEXT NOT NULL UNIQUE,
	position INTEGER NOT NULL DEFAULT 0 CHECK (position >= 0)
);
CREATE TABLE items (
	sku TEXT NOT NULL,
	category_id INTEGER NOT NULL REFERENCES categories(id),
	price REAL NOT NULL,
	doubled REAL GENERATED ALWAYS AS (price * 2) STORED,
	PRIMARY KEY (category_id, sku)
);
`
	sqliteMatchView = `CREATE VIEW top_categories AS
SELECT c.id, c.name FROM categories c WHERE c.parent_id IS NULL;
`
)

func dialectFixtures() []dialectFixture {
	return []dialectFixture{
		{
			dialect:         "postgres",
			driver:          "pgx",
			newDatabase:     livedb.NewPostgresDatabase,
			apply:           func(t *testing.T, conn, s string) { t.Helper(); livedb.ExecSQL(t, "pgx", conn, s) },
			ddl:             pgMatchDDL,
			view:            pgMatchView,
			users:           "CREATE TABLE users (id integer PRIMARY KEY, name text NOT NULL);\n",
			posts:           "CREATE TABLE posts (id integer PRIMARY KEY, title text NOT NULL);\n",
			addNickname:     "ALTER TABLE users ADD COLUMN nickname text;\n",
			postsWithUserFK: "CREATE TABLE posts (id integer PRIMARY KEY, user_id integer REFERENCES users(id));\n",
			unreachable: []unreachableDatabase{{
				name: "unreachable host",
				url: func(*testing.T) string {
					return "postgres://test:test@127.0.0.1:1/app?sslmode=disable&connect_timeout=5"
				},
			}},
		},
		{
			dialect:     "mysql",
			driver:      "stdlib",
			newDatabase: livedb.NewMySQLDatabase,
			apply: func(t *testing.T, conn, s string) {
				t.Helper()
				livedb.ExecSQL(t, "mysql", conn+"?multiStatements=true", s)
			},
			ddl:         mysqlMatchDDL,
			view:        mysqlMatchView,
			users:       "CREATE TABLE users (id int NOT NULL PRIMARY KEY, name varchar(100) NOT NULL);\n",
			posts:       "CREATE TABLE posts (id int NOT NULL PRIMARY KEY, title varchar(100) NOT NULL);\n",
			addNickname: "ALTER TABLE users ADD COLUMN nickname varchar(50);\n",
			// MySQL ignores a REFERENCES written on the column.
			postsWithUserFK: "CREATE TABLE posts (id int NOT NULL PRIMARY KEY, user_id int, FOREIGN KEY (user_id) REFERENCES users (id));\n",
			unreachable: []unreachableDatabase{{
				name: "unreachable host",
				url:  func(*testing.T) string { return "root:test@tcp(127.0.0.1:1)/app?timeout=5s" },
			}},
			noSchemaFilter:  "a MySQL connection introspects one database; introspect.schemas warns and is ignored (PRD §4.4)",
			noFKTargetCheck: "MySQL accepts a REFERENCES target that does not exist (FOREIGN_KEY_CHECKS=0), so sqlgen does not check FK targets (PRD §5.5)",
		},
		{
			dialect:         "sqlite",
			driver:          "stdlib",
			newDatabase:     livedb.NewSQLiteDatabase,
			apply:           func(t *testing.T, conn, s string) { t.Helper(); livedb.ExecSQL(t, "sqlite", conn, s) },
			ddl:             sqliteMatchDDL,
			view:            sqliteMatchView,
			users:           "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);\n",
			posts:           "CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT NOT NULL);\n",
			addNickname:     "ALTER TABLE users ADD COLUMN nickname TEXT;\n",
			postsWithUserFK: "CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));\n",
			unreachable: []unreachableDatabase{
				{
					name: "missing file",
					url:  func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing.db") },
				},
				{
					name: "unreadable file",
					url:  func(t *testing.T) string { return t.TempDir() },
				},
			},
			noSchemaFilter:  "a SQLite file is one database; introspect.schemas warns and is ignored (PRD §4.4)",
			noFKTargetCheck: "SQLite accepts a REFERENCES target that does not exist, so sqlgen does not check FK targets (PRD §5.5)",
		},
	}
}

// writeFile writes content to dir/name and returns the path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { //nolint:gosec // test writes under its own temp dir
		t.Fatal(err)
	}
	return path
}

// runSource writes src to dir/sqlgen.yml and runs command with it. It
// returns stderr and the exit code, 0 on success.
func runSource(t *testing.T, dir string, src testSource, command ...string) (string, int) {
	t.Helper()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	writeTestConfigWithSource(t, cfgPath, src)
	_, stderr, err := executeCommand(append(command, "--config", cfgPath)...)
	if err == nil {
		return stderr, 0
	}
	ee, ok := errors.AsType[*exitError](err)
	if !ok {
		t.Fatalf("%s: %v\nstderr: %s", strings.Join(command, " "), err, stderr)
	}
	return stderr, ee.code
}

// generateSource runs generate with src and fails the test on any error.
// The manifest timestamp is pinned, so two runs can be compared.
func generateSource(t *testing.T, dir string, src testSource) {
	t.Helper()
	if stderr, code := runSource(t, dir, src, "generate", "--manifest-timestamp", "2026-01-01T00:00:00Z"); code != 0 {
		t.Fatalf("generate (source %s) exited %d\nstderr: %s", src.source, code, stderr)
	}
}

// readTree returns every file under root by its slash path relative to root.
func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path) //nolint:gosec // test reads its own temp tree
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}
	return files
}

func readGenerated(t *testing.T, outDir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, name)) //nolint:gosec // test reads its own temp tree
	if err != nil {
		t.Fatalf("reading generated %s: %v", name, err)
	}
	return string(data)
}

func assertNoFile(t *testing.T, outDir, name string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(outDir, name)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s was generated (stat error %v), want it absent", name, err)
	}
}

// manifestEntities returns the manifest's entities by table name.
func manifestEntities(t *testing.T, outDir string) map[string]map[string]any {
	t.Helper()
	var m struct {
		Entities []map[string]any `json:"entities"`
	}
	if err := json.Unmarshal([]byte(readGenerated(t, outDir, "manifest/manifest_gen.json")), &m); err != nil {
		t.Fatalf("decoding manifest: %v", err)
	}
	byTable := make(map[string]map[string]any, len(m.Entities))
	for _, e := range m.Entities {
		table, _ := e["table"].(string)
		byTable[table] = e
	}
	return byTable
}

// TestGenerate_DatabaseSourceMatchesFiles generates the same DDL under
// source: files and source: database, into the same output path with the
// same config path and manifest timestamp, and compares the two trees byte
// for byte, less the livedb.ParityExceptions.
func TestGenerate_DatabaseSourceMatchesFiles(t *testing.T) {
	for _, f := range dialectFixtures() {
		t.Run(f.dialect, func(t *testing.T) {
			conn := f.newDatabase(t)
			f.apply(t, conn, f.ddl+f.view)

			dir := t.TempDir()
			t.Chdir(dir)
			schemaFile := writeFile(t, filepath.Join(dir, "schema"), "schema.sql", f.ddl)
			viewFile := writeFile(t, filepath.Join(dir, "views"), "view.sql", f.view)
			outDir := filepath.Join(dir, "models")

			files := f.source("files", "", outDir)
			files.paths = []string{schemaFile}
			files.views = []string{viewFile}
			generateSource(t, dir, files)
			fromFiles := readTree(t, outDir)
			if err := os.RemoveAll(outDir); err != nil {
				t.Fatal(err)
			}

			database := f.source("database", conn, outDir)
			database.views = []string{viewFile}
			generateSource(t, dir, database)
			fromDatabase := readTree(t, outDir)

			if len(fromFiles) == 0 {
				t.Fatal("source files generated nothing")
			}
			if diff := cmp.Diff(livedb.NormalizeTree(t, f.dialect, fromFiles), livedb.NormalizeTree(t, f.dialect, fromDatabase)); diff != "" {
				t.Errorf("generated tree differs by source (-files +database):\n%s", diff)
			}
		})
	}
}

// TestGenerate_DatabaseSourceCompiles builds each dialect's source: database
// output as a consumer module would, against this checkout's runtime.
func TestGenerate_DatabaseSourceCompiles(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	goSum, err := os.ReadFile(filepath.Join(root, "go.sum")) //nolint:gosec // the repo's own go.sum
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range dialectFixtures() {
		t.Run(f.dialect, func(t *testing.T) {
			conn := f.newDatabase(t)
			f.apply(t, conn, f.ddl+f.view)

			dir := t.TempDir()
			t.Chdir(dir)
			writeFile(t, dir, "go.mod", "module example.com/app\n\ngo 1.27.0\n\n"+
				"require github.com/teandresmith/sqlgen v0.0.0\n\n"+
				"replace github.com/teandresmith/sqlgen => "+filepath.ToSlash(root)+"\n")
			writeFile(t, dir, "go.sum", string(goSum))
			viewFile := writeFile(t, filepath.Join(dir, "views"), "view.sql", f.view)

			src := f.source("database", conn, filepath.Join(dir, "models"))
			src.views = []string{viewFile}
			generateSource(t, dir, src)

			build := exec.CommandContext(t.Context(), "go", "build", "./...")
			build.Dir = dir
			build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("go build: %v\n%s", err, out)
			}
		})
	}
}

// TestGenerate_BothSourceAppliesOverlay applies a pending migration on top
// of the database. A migration that creates a table the database already
// has is a schema error.
func TestGenerate_BothSourceAppliesOverlay(t *testing.T) {
	for _, f := range dialectFixtures() {
		t.Run(f.dialect, func(t *testing.T) {
			conn := f.newDatabase(t)
			f.apply(t, conn, f.users)
			dir := t.TempDir()
			t.Chdir(dir)
			outDir := filepath.Join(dir, "models")

			src := f.source("both", conn, outDir)
			src.paths = []string{writeFile(t, filepath.Join(dir, "migrations"), "002_overlay.sql", f.addNickname+f.posts)}
			generateSource(t, dir, src)
			if user := readGenerated(t, outDir, "user_gen.go"); !strings.Contains(user, "Nickname") {
				t.Errorf("user_gen.go has no Nickname field from the overlay's ADD COLUMN")
			}
			readGenerated(t, outDir, "post_gen.go")

			src.paths = []string{writeFile(t, filepath.Join(dir, "conflict"), "001_init.sql", f.users)}
			stderr, code := runSource(t, dir, src, "generate")
			if code != ExitSchema {
				t.Fatalf("CREATE TABLE of a table the database has: exit %d, want %d\nstderr: %s", code, ExitSchema, stderr)
			}
			if !strings.Contains(stderr, "merge both mode") || !strings.Contains(stderr, "users") {
				t.Errorf("stderr = %q, want the merge error naming users", stderr)
			}
		})
	}
}

// TestGenerate_DatabaseSourceIgnoresPaths: under source: database the schema
// files are not read, even when they disagree with the database.
func TestGenerate_DatabaseSourceIgnoresPaths(t *testing.T) {
	for _, f := range dialectFixtures() {
		t.Run(f.dialect, func(t *testing.T) {
			conn := f.newDatabase(t)
			f.apply(t, conn, f.users)
			dir := t.TempDir()
			t.Chdir(dir)
			outDir := filepath.Join(dir, "models")

			conflicting := strings.Replace(f.users, "name", "email", 1) + f.addNickname + f.posts
			src := f.source("database", conn, outDir)
			src.paths = []string{writeFile(t, filepath.Join(dir, "schema"), "schema.sql", conflicting)}
			generateSource(t, dir, src)

			user := readGenerated(t, outDir, "user_gen.go")
			if !strings.Contains(user, "Name ") || strings.Contains(user, "Email") || strings.Contains(user, "Nickname") {
				t.Errorf("user_gen.go does not reflect the database's users (id, name):\n%s", user)
			}
			assertNoFile(t, outDir, "post_gen.go")
		})
	}
}

// TestGenerate_IntrospectFilters: input.introspect narrows the tables read
// from the database.
func TestGenerate_IntrospectFilters(t *testing.T) {
	for _, f := range dialectFixtures() {
		t.Run(f.dialect, func(t *testing.T) {
			conn := f.newDatabase(t)
			f.apply(t, conn, f.users+f.posts)

			t.Run("tables", func(t *testing.T) {
				dir := t.TempDir()
				t.Chdir(dir)
				outDir := filepath.Join(dir, "models")
				src := f.source("database", conn, outDir)
				src.tables = []string{"posts"}
				generateSource(t, dir, src)
				readGenerated(t, outDir, "post_gen.go")
				assertNoFile(t, outDir, "user_gen.go")
			})

			t.Run("schemas", func(t *testing.T) {
				if f.noSchemaFilter != "" {
					t.Skip(f.noSchemaFilter)
				}
				f.apply(t, conn, "CREATE SCHEMA billing; CREATE TABLE billing.invoices (id integer PRIMARY KEY);")
				for _, tt := range []struct {
					name           string
					schemas        []string
					excludeSchemas []string
					want, notWant  []string
				}{
					{name: "schemas", schemas: []string{"billing"}, want: []string{"invoice_gen.go"}, notWant: []string{"user_gen.go", "post_gen.go"}},
					{name: "exclude_schemas", excludeSchemas: []string{"billing"}, want: []string{"user_gen.go", "post_gen.go"}, notWant: []string{"invoice_gen.go"}},
				} {
					t.Run(tt.name, func(t *testing.T) {
						dir := t.TempDir()
						t.Chdir(dir)
						outDir := filepath.Join(dir, "models")
						src := f.source("database", conn, outDir)
						src.schemas, src.excludeSchemas = tt.schemas, tt.excludeSchemas
						generateSource(t, dir, src)
						for _, name := range tt.want {
							readGenerated(t, outDir, name)
						}
						for _, name := range tt.notWant {
							assertNoFile(t, outDir, name)
						}
					})
				}
			})
		})
	}
}

// TestGenerate_IntrospectedViewWithAnnotation: an input.views file for a view
// the database defines annotates it; it does not add a second view.
func TestGenerate_IntrospectedViewWithAnnotation(t *testing.T) {
	for _, f := range dialectFixtures() {
		t.Run(f.dialect, func(t *testing.T) {
			conn := f.newDatabase(t)
			const view = "CREATE VIEW user_names AS SELECT u.id, u.name FROM users u;\n"
			f.apply(t, conn, f.users+view)
			dir := t.TempDir()
			t.Chdir(dir)
			outDir := filepath.Join(dir, "models")

			src := f.source("database", conn, outDir)
			src.views = []string{writeFile(t, filepath.Join(dir, "views"), "user_names.sql", "-- @pk: id\n"+view)}
			generateSource(t, dir, src)

			entities := manifestEntities(t, outDir)
			views := 0
			for _, e := range entities {
				if e["kind"] == "view" {
					views++
				}
			}
			if views != 1 {
				t.Errorf("manifest has %d views, want 1", views)
			}
			pk, _ := entities["user_names"]["pk"].(map[string]any)
			if pk["kind"] != "single" {
				t.Errorf("user_names pk = %v, want the @pk id applied", pk)
			}
		})
	}
}

// TestGenerate_ConnectionFailureExitCode: a database sqlgen cannot reach
// exits 4 from generate and validate; a schema error in the overlay exits 3.
func TestGenerate_ConnectionFailureExitCode(t *testing.T) {
	commands := [][]string{{"generate"}, {"validate"}}
	for _, f := range dialectFixtures() {
		t.Run(f.dialect, func(t *testing.T) {
			for _, u := range f.unreachable {
				for _, command := range commands {
					t.Run(u.name+"/"+command[0], func(t *testing.T) {
						dir := t.TempDir()
						t.Chdir(dir)
						src := f.source("database", u.url(t), filepath.Join(dir, "models"))
						if stderr, code := runSource(t, dir, src, command...); code != ExitConnection {
							t.Errorf("exit %d, want %d\nstderr: %s", code, ExitConnection, stderr)
						}
					})
				}
			}

			conn := f.newDatabase(t)
			f.apply(t, conn, f.users)
			for _, command := range commands {
				t.Run("overlay schema error/"+command[0], func(t *testing.T) {
					dir := t.TempDir()
					t.Chdir(dir)
					src := f.source("both", conn, filepath.Join(dir, "models"))
					src.paths = []string{writeFile(t, filepath.Join(dir, "migrations"), "002.sql",
						strings.Replace(f.addNickname, "users", "ghosts", 1))}
					if stderr, code := runSource(t, dir, src, command...); code != ExitSchema {
						t.Errorf("exit %d, want %d\nstderr: %s", code, ExitSchema, stderr)
					}
				})
			}
		})
	}
}

// relationshipTargets lists the target entity of each relationship the
// manifest gives entity.
func relationshipTargets(entity map[string]any) []string {
	rels, _ := entity["relationships"].([]any)
	targets := make([]string, 0, len(rels))
	for _, r := range rels {
		if r, ok := r.(map[string]any); ok {
			target, _ := r["target_entity"].(string)
			targets = append(targets, target)
		}
	}
	return targets
}

// TestGenerate_BothSourceForeignKeys: under both, a file's foreign key may
// target a table only the database defines; on PostgreSQL, one whose target
// neither defines is a schema error.
func TestGenerate_BothSourceForeignKeys(t *testing.T) {
	for _, f := range dialectFixtures() {
		t.Run(f.dialect, func(t *testing.T) {
			conn := f.newDatabase(t)
			f.apply(t, conn, f.users)

			t.Run("target only in the database", func(t *testing.T) {
				dir := t.TempDir()
				t.Chdir(dir)
				outDir := filepath.Join(dir, "models")
				src := f.source("both", conn, outDir)
				src.paths = []string{writeFile(t, filepath.Join(dir, "migrations"), "002.sql", f.postsWithUserFK)}
				generateSource(t, dir, src)
				got := relationshipTargets(manifestEntities(t, outDir)["users"])
				if diff := cmp.Diff([]string{"Post"}, got); diff != "" {
					t.Errorf("users relationships (-want +got):\n%s", diff)
				}
			})

			for _, command := range [][]string{{"generate"}, {"validate"}} {
				t.Run("target nowhere/"+command[0], func(t *testing.T) {
					if f.noFKTargetCheck != "" {
						t.Skip(f.noFKTargetCheck)
					}
					dir := t.TempDir()
					t.Chdir(dir)
					src := f.source("both", conn, filepath.Join(dir, "models"))
					src.paths = []string{writeFile(t, filepath.Join(dir, "migrations"), "002.sql",
						"CREATE TABLE posts (id integer PRIMARY KEY, ghost_id integer REFERENCES ghosts(id));")}
					stderr, code := runSource(t, dir, src, command...)
					if code != ExitSchema {
						t.Fatalf("exit %d, want %d\nstderr: %s", code, ExitSchema, stderr)
					}
					if !strings.Contains(stderr, "ghosts") {
						t.Errorf("stderr = %q, want the unresolved target ghosts named", stderr)
					}
				})
			}
		})
	}
}

// TestGenerate_IntrospectTablesCutsForeignKey: under database, an
// introspect.tables filter that leaves out an FK's target is not an error,
// and no relationship into the left-out table is generated (PRD §5.5).
func TestGenerate_IntrospectTablesCutsForeignKey(t *testing.T) {
	for _, f := range dialectFixtures() {
		t.Run(f.dialect, func(t *testing.T) {
			conn := f.newDatabase(t)
			f.apply(t, conn, f.users+f.postsWithUserFK)
			dir := t.TempDir()
			t.Chdir(dir)
			outDir := filepath.Join(dir, "models")

			src := f.source("database", conn, outDir)
			src.tables = []string{"posts"}
			generateSource(t, dir, src)
			entities := manifestEntities(t, outDir)
			if _, ok := entities["users"]; ok {
				t.Error("users was generated, want it filtered out")
			}
			if got := relationshipTargets(entities["posts"]); len(got) != 0 {
				t.Errorf("posts relationships = %v, want none into the filtered-out users", got)
			}
		})
	}
}
