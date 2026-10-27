package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/internal/livedb"
)

// generateSQLiteManifest generates a one-table SQLite package with the manifest
// enabled into a temp dir, passing args after the config flag, and returns
// the output dir and the command error.
func generateSQLiteManifest(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	sqlDir := filepath.Join(dir, "migrations")
	outDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatal(err)
	}
	writeSQLFile(t, sqlDir, "CREATE TABLE products (id integer PRIMARY KEY, name text NOT NULL);")
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	body := "version: v1\n" +
		"input:\n  dialect: sqlite\n  paths:\n    - " + filepath.ToSlash(sqlDir) + "\n" +
		"output:\n  driver: stdlib\n  dir: " + filepath.ToSlash(outDir) + "\n  package: models\n" +
		"generation:\n  manifest:\n    enabled: true\n    mcp:\n      emit_project_config: false\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := executeCommand(append([]string{"--config", cfgPath}, args...)...)
	return outDir, stderr, err
}

// readGeneratedAt reads generated_at from the emitted manifest under outDir.
func readGeneratedAt(t *testing.T, outDir string) string {
	t.Helper()
	var doc struct {
		GeneratedAt *string `json:"generated_at"`
	}
	if err := json.Unmarshal([]byte(readGenerated(t, outDir, "manifest/manifest_gen.json")), &doc); err != nil {
		t.Fatalf("decoding manifest: %v", err)
	}
	if doc.GeneratedAt == nil {
		t.Fatal("emitted manifest has no generated_at")
	}
	return *doc.GeneratedAt
}

// TestGenerate_PinnedManifestTimestampIsGeneratedAt pins PRD §30.4 / §30.3: with
// --manifest-timestamp set to an RFC3339 value, the emitted generated_at is
// that value and still parses as RFC3339.
func TestGenerate_PinnedManifestTimestampIsGeneratedAt(t *testing.T) {
	tests := []struct {
		name string
		flag string
	}{
		{name: "UTC timestamp", flag: "2026-07-09T12:34:56Z"},
		{name: "offset timestamp", flag: "2026-07-09T12:34:56+02:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outDir, stderr, err := generateSQLiteManifest(t, "--manifest-timestamp", tt.flag)
			if err != nil {
				t.Fatalf("generate: %v\nstderr: %s", err, stderr)
			}
			got := readGeneratedAt(t, outDir)
			if got != tt.flag {
				t.Errorf("generated_at = %q, want the pinned %q", got, tt.flag)
			}
			if _, err := time.Parse(time.RFC3339, got); err != nil {
				t.Errorf("generated_at %q is not RFC3339: %v", got, err)
			}
		})
	}
}

// fixtureManifestSchemaURL returns the $schema of the valid manifest fixture.
func fixtureManifestSchemaURL(t *testing.T) (string, map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(validFixture)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	url, _ := m["$schema"].(string)
	if url == "" {
		t.Fatalf("fixture %s has no $schema", validFixture)
	}
	return url, m
}

// TestManifestValidate_UsesSchemaAtManifestURL pins PRD §30.8 / §30.5: `manifest
// validate` validates against the JSON Schema the manifest's `$schema` URL
// references — consumers pin through that URL. The stubbed fetch serves a
// schema that disagrees with the embedded one in each direction, so the exit
// code shows which schema was applied, and the fetched URL must be exactly
// the manifest's `$schema`.
func TestManifestValidate_UsesSchemaAtManifestURL(t *testing.T) {
	url, fixture := fixtureManifestSchemaURL(t)
	withoutGenerationConfig := func(t *testing.T) string {
		t.Helper()
		m := make(map[string]any, len(fixture))
		for k, v := range fixture {
			m[k] = v
		}
		delete(m, "generation_config") // the embedded schema requires it
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return writeTempManifest(t, "no_generation_config.json", data)
	}
	tests := []struct {
		name     string
		manifest func(t *testing.T) string
		served   string
		wantCode int
	}{
		{
			name:     "a manifest the embedded schema accepts fails the schema at its URL",
			manifest: func(*testing.T) string { return validFixture },
			served:   `{"$schema": "http://json-schema.org/draft-07/schema#", "type": "object", "required": ["fj_pinned_marker"]}`,
			wantCode: manifestExitFail,
		},
		{
			name:     "a manifest the embedded schema rejects passes the schema at its URL",
			manifest: withoutGenerationConfig,
			served:   `{"$schema": "http://json-schema.org/draft-07/schema#", "type": "object"}`,
			wantCode: manifestExitClean,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fetched []string
			stubSchemaFetcher(t, func(_ context.Context, u string) ([]byte, error) {
				fetched = append(fetched, u)
				return []byte(tt.served), nil
			})
			_, stderr, err := executeCommand("manifest", "validate", tt.manifest(t))
			if code := exitCodeOf(t, err); code != tt.wantCode {
				t.Errorf("exit code = %d, want %d\nstderr: %s", code, tt.wantCode, stderr)
			}
			if diff := cmp.Diff([]string{url}, fetched); diff != "" {
				t.Errorf("fetched schema URLs mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestManifestDiff_ReadsOnlyTheTwoFiles pins PRD §30.8: `manifest diff`
// operates on the on-disk JSON alone. It runs from an empty working directory
// with no sqlgen.yml, no database and no generated package, and still reports
// a difference (exit 1) and no difference (exit 0).
func TestManifestDiff_ReadsOnlyTheTwoFiles(t *testing.T) {
	oldM := baseManifest()
	newM := cloneManifest(t, oldM)
	newM.Entities[0].Columns = newM.Entities[0].Columns[:len(newM.Entities[0].Columns)-1]
	oldPath := writeManifest(t, t.TempDir(), oldM)
	newPath := writeManifest(t, t.TempDir(), newM)

	t.Chdir(t.TempDir())
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("working directory is not empty: %v", entries)
	}

	tests := []struct {
		name     string
		old, new string
		wantCode int
	}{
		{name: "changed manifests", old: oldPath, new: newPath, wantCode: manifestExitFail},
		{name: "identical manifests", old: oldPath, new: oldPath, wantCode: manifestExitClean},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := executeCommand("manifest", "diff", tt.old, tt.new)
			if code := exitCodeOf(t, err); code != tt.wantCode {
				t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s", code, tt.wantCode, stdout, stderr)
			}
			if tt.wantCode == manifestExitFail && strings.TrimSpace(stdout) == "" {
				t.Error("stdout is empty, want the diff")
			}
		})
	}
}

// emittedIndex is the emitted shape of one indexes[] entry.
type emittedIndex struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
	Method  string   `json:"method"`
	Where   string   `json:"where,omitempty"`
}

// emittedTableMetadata is the slice of one emitted table entity the source-mode
// tests compare: its indexes, in emitted order, and each column's check.
type emittedTableMetadata struct {
	Indexes []emittedIndex
	Checks  map[string]string
}

// readEmittedTableMetadata decodes the emitted manifest under outDir, by table name.
func readEmittedTableMetadata(t *testing.T, outDir string) map[string]emittedTableMetadata {
	t.Helper()
	var doc struct {
		Entities []struct {
			Table   string         `json:"table"`
			Kind    string         `json:"kind"`
			Indexes []emittedIndex `json:"indexes"`
			Columns []struct {
				Name  string `json:"name"`
				Check string `json:"check"`
			} `json:"columns"`
		} `json:"entities"`
	}
	if err := json.Unmarshal([]byte(readGenerated(t, outDir, "manifest/manifest_gen.json")), &doc); err != nil {
		t.Fatalf("decoding manifest: %v", err)
	}
	out := make(map[string]emittedTableMetadata, len(doc.Entities))
	for _, e := range doc.Entities {
		if e.Kind != "table" {
			continue
		}
		md := emittedTableMetadata{Indexes: e.Indexes, Checks: map[string]string{}}
		for _, c := range e.Columns {
			if c.Check != "" {
				md.Checks[c.Name] = c.Check
			}
		}
		out[e.Table] = md
	}
	return out
}

// sortedIndexes returns indexes sorted by name.
func sortedIndexes(in []emittedIndex) []emittedIndex {
	out := slices.Clone(in)
	slices.SortFunc(out, func(a, b emittedIndex) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// sourceParityFixture is one dialect's schema for the files-against-database
// comparison of the index list and the column checks.
type sourceParityFixture struct {
	dialect     string
	driver      string
	newDatabase func(t *testing.T) string
	apply       func(t *testing.T, conn, statements string)
	ddl         string
	// wantIndexes is each table's index list, sorted by name, under every
	// source; wantChecks names the columns that carry a check.
	wantIndexes map[string][]emittedIndex
	wantChecks  map[string][]string
}

func uniqueIndex(name string, cols ...string) emittedIndex {
	return emittedIndex{Name: name, Columns: cols, Unique: true, Method: "btree"}
}

func sourceParityFixtures() []sourceParityFixture {
	return []sourceParityFixture{
		{
			dialect:     "postgres",
			driver:      "pgx",
			newDatabase: livedb.NewPostgresDatabase,
			apply:       func(t *testing.T, conn, s string) { t.Helper(); livedb.ExecSQL(t, "pgx", conn, s) },
			ddl: `CREATE TABLE orders (
	id integer PRIMARY KEY,
	email text NOT NULL CONSTRAINT orders_email_uq UNIQUE,
	handle text NOT NULL,
	placed_at timestamptz NOT NULL,
	qty integer NOT NULL CHECK (qty > 0),
	addr inet,
	CONSTRAINT orders_handle_uq UNIQUE (handle)
);
CREATE INDEX ON orders (placed_at);
CREATE INDEX orders_lower_email_idx ON orders (lower(email));
CREATE INDEX orders_qty_plus_idx ON orders ((qty + 1));
CREATE INDEX orders_addr_gist ON orders USING gist (addr inet_ops);
CREATE TABLE dups (id integer PRIMARY KEY, CONSTRAINT dups_u UNIQUE (id));
ALTER TABLE dups ADD UNIQUE (id);
CREATE TABLE codes (id integer PRIMARY KEY, code text NOT NULL, CONSTRAINT codes_a UNIQUE (code), CONSTRAINT codes_b UNIQUE (code));
`,
			wantIndexes: map[string][]emittedIndex{
				"orders": {
					{Name: "orders_addr_gist", Columns: []string{"addr"}, Method: "gist"},
					uniqueIndex("orders_email_uq", "email"),
					uniqueIndex("orders_handle_uq", "handle"),
					uniqueIndex("orders_pkey", "id"),
					{Name: "orders_placed_at_idx", Columns: []string{"placed_at"}, Method: "btree"},
				},
				// One CREATE TABLE merges the duplicate into the primary
				// key under the declared name; the later ALTER TABLE adds a
				// second index under the derived name.
				"dups":  {uniqueIndex("dups_id_key", "id"), uniqueIndex("dups_u", "id")},
				"codes": {uniqueIndex("codes_a", "code"), uniqueIndex("codes_pkey", "id")},
			},
			wantChecks: map[string][]string{"orders": {"qty"}},
		},
		{
			dialect:     "mysql",
			driver:      "stdlib",
			newDatabase: livedb.NewMySQLDatabase,
			apply: func(t *testing.T, conn, s string) {
				t.Helper()
				livedb.ExecSQL(t, "mysql", conn+"?multiStatements=true", s)
			},
			ddl: `CREATE TABLE orders (
	id int NOT NULL PRIMARY KEY,
	email varchar(255) NOT NULL UNIQUE,
	handle varchar(255) NOT NULL,
	placed_at datetime NOT NULL,
	qty int NOT NULL CHECK (qty > 0),
	CONSTRAINT orders_handle_uq UNIQUE (handle)
);
CREATE INDEX orders_placed_at_idx ON orders (placed_at);
CREATE INDEX orders_lower_email_idx ON orders ((lower(email)));
CREATE INDEX orders_qty_plus_idx ON orders ((qty + 1));
`,
			wantIndexes: map[string][]emittedIndex{
				"orders": {
					uniqueIndex("PRIMARY", "id"),
					uniqueIndex("email", "email"),
					uniqueIndex("orders_handle_uq", "handle"),
					{Name: "orders_placed_at_idx", Columns: []string{"placed_at"}, Method: "btree"},
				},
			},
			wantChecks: map[string][]string{"orders": {"qty"}},
		},
		{
			dialect:     "sqlite",
			driver:      "stdlib",
			newDatabase: livedb.NewSQLiteDatabase,
			apply:       func(t *testing.T, conn, s string) { t.Helper(); livedb.ExecSQL(t, "sqlite", conn, s) },
			ddl: `CREATE TABLE orders (
	id INTEGER PRIMARY KEY,
	email TEXT NOT NULL CONSTRAINT orders_email_uq UNIQUE,
	handle TEXT NOT NULL,
	placed_at INTEGER NOT NULL,
	qty INTEGER NOT NULL CHECK (qty > 0),
	CONSTRAINT orders_handle_uq UNIQUE (handle)
);
CREATE INDEX orders_placed_at_idx ON orders (placed_at);
CREATE INDEX orders_lower_email_idx ON orders (lower(email));
CREATE INDEX orders_qty_plus_idx ON orders (qty + 1);
CREATE TABLE dups (code TEXT PRIMARY KEY, CONSTRAINT dups_u UNIQUE (code));
CREATE TABLE rowid_dups (id INTEGER PRIMARY KEY, CONSTRAINT rowid_dups_u UNIQUE (id));
`,
			wantIndexes: map[string][]emittedIndex{
				"orders": {
					uniqueIndex("orders_email_uq", "email"),
					uniqueIndex("orders_handle_uq", "handle"),
					uniqueIndex("orders_pkey", "id"),
					{Name: "orders_placed_at_idx", Columns: []string{"placed_at"}, Method: "btree"},
				},
				// The UNIQUE repeats the key's own index, so SQLite builds
				// none for it; a rowid-alias key has no index, so the UNIQUE
				// on it is kept.
				"dups":       {uniqueIndex("dups_pkey", "code")},
				"rowid_dups": {uniqueIndex("rowid_dups_pkey", "id"), uniqueIndex("rowid_dups_u", "id")},
			},
			wantChecks: map[string][]string{"orders": {"qty"}},
		},
	}
}

// TestGenerate_ManifestIndexesAndChecksMatchAcrossSources pins PRD §30.7 under every
// input.source: one schema generated from files and from the database it
// built gives the same indexes[] and the same per-column check. The fixtures
// hold the shapes that rule turns on — an unnamed PostgreSQL CREATE INDEX
// listed under the name PostgreSQL gives it, declared names inline and
// table-level, a GiST index, expression indexes left out, and a duplicate
// UNIQUE listed only where the database builds an index for it.
func TestGenerate_ManifestIndexesAndChecksMatchAcrossSources(t *testing.T) {
	for _, f := range sourceParityFixtures() {
		t.Run(f.dialect, func(t *testing.T) {
			conn := f.newDatabase(t)
			f.apply(t, conn, f.ddl)

			dir := t.TempDir()
			t.Chdir(dir)
			schemaFile := writeFile(t, filepath.Join(dir, "schema"), "schema.sql", f.ddl)
			outDir := filepath.Join(dir, "models")

			files := testSource{dialect: f.dialect, driver: f.driver, source: "files", paths: []string{schemaFile}, outputDir: outDir}
			generateSource(t, dir, files)
			fromFiles := readEmittedTableMetadata(t, outDir)
			if err := os.RemoveAll(outDir); err != nil {
				t.Fatal(err)
			}
			database := testSource{dialect: f.dialect, driver: f.driver, source: "database", url: conn, outputDir: outDir}
			generateSource(t, dir, database)
			fromDatabase := readEmittedTableMetadata(t, outDir)

			for table, want := range f.wantIndexes {
				filesMD, ok := fromFiles[table]
				if !ok {
					t.Errorf("table %s missing from the files manifest", table)
					continue
				}
				dbMD, ok := fromDatabase[table]
				if !ok {
					t.Errorf("table %s missing from the database manifest", table)
					continue
				}
				if diff := cmp.Diff(filesMD.Indexes, dbMD.Indexes); diff != "" {
					t.Errorf("%s indexes differ by source (-files +database):\n%s", table, diff)
				}
				if diff := cmp.Diff(want, sortedIndexes(filesMD.Indexes)); diff != "" {
					t.Errorf("%s indexes under files (-want +got):\n%s", table, diff)
				}
				if diff := cmp.Diff(want, sortedIndexes(dbMD.Indexes)); diff != "" {
					t.Errorf("%s indexes under database (-want +got):\n%s", table, diff)
				}
				if diff := cmp.Diff(filesMD.Checks, dbMD.Checks); diff != "" {
					t.Errorf("%s column checks differ by source (-files +database):\n%s", table, diff)
				}
				checked := slices.Sorted(maps.Keys(filesMD.Checks))
				if diff := cmp.Diff(f.wantChecks[table], checked); diff != "" {
					t.Errorf("%s columns carrying a check under files (-want +got):\n%s", table, diff)
				}
			}
		})
	}
}

// storedViewDefinition returns the definition the database stores for
// view: information_schema.views.view_definition on PostgreSQL and MySQL,
// the sqlite_master statement on SQLite.
func storedViewDefinition(t *testing.T, dialect, conn, view string) string {
	t.Helper()
	driver, query := "pgx", "SELECT view_definition FROM information_schema.views WHERE table_schema = 'public' AND table_name = $1"
	switch dialect {
	case "mysql":
		driver, query = "mysql", "SELECT view_definition FROM information_schema.views WHERE table_schema = DATABASE() AND table_name = ?"
	case "sqlite":
		driver, query = "sqlite", "SELECT sql FROM sqlite_master WHERE type = 'view' AND name = ?"
	}
	db, err := sql.Open(driver, conn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	var def string
	if err := db.QueryRowContext(t.Context(), query, view).Scan(&def); err != nil {
		t.Fatalf("reading the stored definition of %s: %v", view, err)
	}
	if def == "" {
		t.Fatalf("the database stores no definition for %s", view)
	}
	return def
}

// TestGenerate_ManifestViewSourceIsStoredDefinitionUnderDatabase pins PRD §30.4.2: under
// `input.source: database`, a view's `source` is the definition the database
// stores, not a re-rendering of it.
func TestGenerate_ManifestViewSourceIsStoredDefinitionUnderDatabase(t *testing.T) {
	views := map[string]string{"postgres": "active_users", "mysql": "pro_accounts", "sqlite": "top_categories"}
	for _, f := range dialectFixtures() {
		t.Run(f.dialect, func(t *testing.T) {
			conn := f.newDatabase(t)
			f.apply(t, conn, f.ddl+f.view)
			view := views[f.dialect]
			want := storedViewDefinition(t, f.dialect, conn, view)

			dir := t.TempDir()
			t.Chdir(dir)
			outDir := filepath.Join(dir, "models")
			generateSource(t, dir, f.source("database", conn, outDir))

			entity, ok := manifestEntities(t, outDir)[view]
			if !ok {
				t.Fatalf("view %s missing from the manifest", view)
			}
			if entity["kind"] != "view" {
				t.Fatalf("%s kind = %v, want view", view, entity["kind"])
			}
			if got := entity["source"]; got != want {
				t.Errorf("%s source = %q, want the stored definition %q", view, got, want)
			}
		})
	}
}

// sentStatementsProbeSource is a test file dropped into a copy of an example's models
// package. It runs each named Article method against a mock querier and
// prints, per method, the statements the generated code handed it.
const sentStatementsProbeSource = `package models_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/database/mock"

	"MODULE/models"
)

// zeroKey returns the zero value of get's key parameter, so the probe needs
// no knowledge of the example's primary-key type.
func zeroKey[K any](get func(context.Context, K, ...func(*models.CallOptions[models.ArticleFieldOptions])) (*models.Article, error)) K {
	var k K
	return k
}

func TestProbeSentStatements(t *testing.T) {
	var sent []string
	q := &mock.Querier{
		ExecFn: func(_ context.Context, sql string, _ ...any) (database.Result, error) {
			sent = append(sent, sql)
			return &mock.Result{RowsAffectedFn: func() (int64, error) { return 1, nil }}, nil
		},
		QueryFn: func(_ context.Context, sql string, _ ...any) (database.Rows, error) {
			sent = append(sent, sql)
			return &mock.Rows{}, nil
		},
		QueryRowFn: func(_ context.Context, sql string, _ ...any) database.Row {
			sent = append(sent, sql)
			return &mock.Row{ScanFn: func(...any) error { return nil }}
		},
	}
	articles := models.New(q).Articles()
	ctx := context.Background()
	id := zeroKey(articles.Get)
	calls := []struct {
		name string
		call func()
	}{
		{"Get", func() { _, _ = articles.Get(ctx, id) }},
		{"Exists", func() { _, _ = articles.Exists(ctx, id) }},
		{"SoftDelete", func() { _, _ = articles.SoftDelete(ctx, id) }},
		{"HardDelete", func() { _ = articles.HardDelete(ctx, id) }},
	}
	out := map[string][]string{}
	for _, c := range calls {
		sent = nil
		c.call()
		out[c.name] = sent
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("SENT_STATEMENTS:%s\n", data)
}
`

// publishedMethod is the slice of a manifest method the sql_bodies probe reads.
type publishedMethod struct {
	Name      string            `json:"name"`
	SQLBodies map[string]string `json:"sql_bodies"`
}

// publishedArticleBodies reads the Article methods' sql_bodies for dialect
// from an example's expected manifest.
func publishedArticleBodies(t *testing.T, example, dialect string) map[string]string {
	t.Helper()
	var doc struct {
		Entities []struct {
			Name    string `json:"name"`
			Methods struct {
				Query    []publishedMethod `json:"query"`
				Mutation []publishedMethod `json:"mutation"`
			} `json:"methods"`
		} `json:"entities"`
	}
	data, err := os.ReadFile(filepath.Join(example, "expected", "manifest", "manifest_gen.json")) //nolint:gosec // a checked-in golden
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	published := map[string]string{}
	for _, e := range doc.Entities {
		if e.Name != "Article" {
			continue
		}
		for _, m := range slices.Concat(e.Methods.Query, e.Methods.Mutation) {
			published[m.Name] = m.SQLBodies[dialect]
		}
	}
	return published
}

// probeSentStatements copies example to a temp module, runs sentStatementsProbeSource in its
// models package, and returns the statements each probed method sent.
func probeSentStatements(t *testing.T, goBin, repo, example string) map[string][]string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(example)); err != nil {
		t.Fatalf("copying %s: %v", example, err)
	}
	modPath := filepath.Join(root, "go.mod")
	mod, err := os.ReadFile(modPath) //nolint:gosec // the temp copy's go.mod
	if err != nil {
		t.Fatal(err)
	}
	modText := strings.ReplaceAll(string(mod), "=> ../../../../..", "=> "+filepath.ToSlash(repo))
	if err := os.WriteFile(modPath, []byte(modText), 0o600); err != nil { //nolint:gosec // the temp copy's go.mod under t.TempDir
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(modText, "\n")
	module := strings.TrimSpace(strings.TrimPrefix(first, "module "))
	probe := strings.ReplaceAll(sentStatementsProbeSource, "MODULE", module)
	if err := os.WriteFile(filepath.Join(root, "models", "sent_statements_probe_test.go"), []byte(probe), 0o600); err != nil { //nolint:gosec // the probe file inside the temp copy under t.TempDir
		t.Fatal(err)
	}

	cmd := exec.Command(goBin, "test", "-count=1", "-run", "^TestProbeSentStatements$", "-v", "./models/") //nolint:gosec // the go binary from PATH, run on a test-written temp module
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running the probe: %v\n%s", err, out)
	}
	var sent map[string][]string
	for line := range strings.SplitSeq(string(out), "\n") {
		if rest, ok := strings.CutPrefix(line, "SENT_STATEMENTS:"); ok {
			if err := json.Unmarshal([]byte(rest), &sent); err != nil {
				t.Fatalf("decoding the probe output %q: %v", rest, err)
			}
		}
	}
	if sent == nil {
		t.Fatalf("the probe printed no statements:\n%s", out)
	}
	return sent
}

// TestGenerate_ManifestSQLBodiesAreTheStatementsMethodsSend pins PRD §30.4.2 / §30.7:
// sql_bodies is captured from the SQL-build pipeline that emits the _gen.go
// query bodies — it is the statement the generated method sends. Each
// dialect example's generated package runs against a querier that records
// every statement, and each by-key Article method with no call-time clause
// (so no placeholder token) must send exactly the statement its manifest
// entry publishes. SoftDelete returns the changed row, so it also reads that
// row (before the write on MySQL, after it elsewhere); the published
// statement is its one write.
func TestGenerate_ManifestSQLBodiesAreTheStatementsMethodsSend(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the example modules; skipped in short mode")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, dialect := range []struct{ name string }{{name: "postgres"}, {name: "mysql"}, {name: "sqlite"}} {
		t.Run(dialect.name, func(t *testing.T) {
			example, err := filepath.Abs(filepath.Join("..", "testdata", "examples", dialect.name))
			if err != nil {
				t.Fatal(err)
			}
			published := publishedArticleBodies(t, example, dialect.name)
			sent := probeSentStatements(t, goBin, repo, example)

			tests := []struct {
				method string
				// readBack is set when the method also reads the changed
				// row to return it.
				readBack bool
			}{
				{method: "Get"},
				{method: "Exists"},
				{method: "SoftDelete", readBack: true},
				{method: "HardDelete"},
			}
			for _, tt := range tests {
				t.Run(tt.method, func(t *testing.T) {
					body := published[tt.method]
					if body == "" {
						t.Fatalf("Article.%s publishes no %s sql_bodies entry", tt.method, dialect.name)
					}
					got := sent[tt.method]
					if tt.readBack {
						got = slices.DeleteFunc(slices.Clone(got), func(s string) bool { return strings.HasPrefix(s, "SELECT ") })
					}
					if diff := cmp.Diff([]string{body}, got); diff != "" {
						t.Errorf("Article.%s: statements sent (+) differ from its sql_bodies (-):\n%s\nall statements sent: %q", tt.method, diff, sent[tt.method])
					}
				})
			}
		})
	}
}
