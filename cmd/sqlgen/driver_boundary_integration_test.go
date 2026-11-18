package main_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/cli"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/internal/livedb"
)

// These tests generate a models package for a column retyped through
// column_map, run the generated client against a live database, and check
// whether the column's value crosses the driver boundary. Every case is a
// package of one temp module: its sqlgen.yml and schema.sql, the generated
// models, and a probe test that the module's own `go test` runs with the
// database's connection string in GTW_DSN.

// gtwProbe is one generated package and the test that exercises it.
type gtwProbe struct {
	// pkg is the package directory inside the temp module.
	pkg string
	// dialect is the input.dialect and the database the probe runs against.
	dialect string
	// config is the sqlgen.yml body after the version and input blocks.
	config string
	// schema is applied to the database and parsed by generate.
	schema string
	// test is the probe's _test.go source, package probe.
	test string
	// files are further Go sources the generated package may import, keyed
	// by their path inside the probe's package directory.
	files map[string]string
}

const gtwProbeModule = "example.com/gtwprobe"

// gtwYearValue is a typed JSON element type the probes retype a column to.
const gtwYearValue = `package jsontypes

// YearValue is one element of a JSON array of yearly values.
type YearValue struct {
	Year  int     ` + "`json:\"year\"`" + `
	Value float64 ` + "`json:\"value\"`" + `
}
`

// gtwRunProbes writes the temp module, generates each probe's package, and
// runs its test against a fresh database, one subtest per probe.
func gtwRunProbes(t *testing.T, probes []gtwProbe) {
	t.Helper()
	if testing.Short() {
		t.Skip("generates and runs a module against a live database; skipped in short mode")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	gtwWriteModule(t, root, repo)

	for _, p := range probes {
		t.Run(p.pkg, func(t *testing.T) {
			dir := filepath.Join(root, p.pkg)
			gtwWrite(t, filepath.Join(dir, "schema.sql"), p.schema)
			gtwWrite(t, filepath.Join(dir, "sqlgen.yml"), "version: v1\ninput:\n  dialect: "+p.dialect+"\n  paths:\n    - ./schema.sql\n"+p.config)
			gtwWrite(t, filepath.Join(dir, "probe_test.go"), p.test)
			for path, src := range p.files {
				gtwWrite(t, filepath.Join(dir, path), src)
			}
			gtwGenerate(t, dir)

			var dsn string
			switch p.dialect {
			case "postgres":
				dsn = livedb.NewPostgresDatabase(t)
				livedb.ExecSQL(t, "pgx", dsn, p.schema)
			case "mysql":
				dsn = livedb.NewMySQLDatabase(t)
				livedb.ExecSQL(t, "mysql", dsn+"?multiStatements=true", p.schema)
			default:
				t.Fatalf("no database for dialect %q", p.dialect)
			}

			cmd := exec.CommandContext(t.Context(), goBin, "test", "-count=1", "-v", "./"+p.pkg+"/") //nolint:gosec // the go binary from PATH, run on a test-written temp module
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local", "GTW_DSN="+dsn)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("probe test failed: %v\n%s", err, out)
			}
			if !strings.Contains(string(out), "--- PASS: Test") {
				t.Errorf("probe ran no test:\n%s", out)
			}
		})
	}
}

// gtwWriteModule writes the temp module's go.mod, a go.sum assembled from
// the repo's own, and the jsontypes package. The replaced sqlgen module
// supplies the drivers, google/uuid and go-cmp at the versions the repo
// pins; gofrs/uuid, which only this CLI module depends on, is required at the
// version cmd/sqlgen/go.mod pins.
func gtwWriteModule(t *testing.T, root, repo string) {
	t.Helper()
	const gofrs = "github.com/gofrs/uuid/v5"
	gofrsVersion := gtwRequiredVersion(t, filepath.Join(repo, "cmd", "sqlgen", "go.mod"), gofrs)
	gtwWrite(t, filepath.Join(root, "go.mod"), `module `+gtwProbeModule+`

go 1.27.0

require (
	`+gofrs+` `+gofrsVersion+`
	github.com/teandresmith/sqlgen v0.0.0
)

replace github.com/teandresmith/sqlgen => `+filepath.ToSlash(repo)+"\n")

	sumFiles := []string{
		filepath.Join(repo, "go.sum"),
		filepath.Join(repo, "cmd", "sqlgen", "go.sum"),
	}
	sums := make([]string, 0, 1024)
	for _, f := range sumFiles {
		data, err := os.ReadFile(f) //nolint:gosec // a checked-in go.sum
		if err != nil {
			t.Fatal(err)
		}
		sums = append(sums, strings.Split(strings.TrimSpace(string(data)), "\n")...)
	}
	slices.Sort(sums)
	gtwWrite(t, filepath.Join(root, "go.sum"), strings.Join(slices.Compact(sums), "\n")+"\n")
	gtwWrite(t, filepath.Join(root, "jsontypes", "year_value.go"), gtwYearValue)
}

// gtwRequiredVersion returns the version the go.mod at path requires of mod.
func gtwRequiredVersion(t *testing.T, path, mod string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // a checked-in go.mod
	if err != nil {
		t.Fatal(err)
	}
	f, err := modfile.ParseLax(path, data, nil)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	for _, r := range f.Require {
		if r.Mod.Path == mod {
			return r.Mod.Version
		}
	}
	t.Fatalf("%s requires no %s", path, mod)
	return ""
}

// gtwGenerate runs sqlgen generate on the sqlgen.yml in dir, from dir.
func gtwGenerate(t *testing.T, dir string) {
	t.Helper()
	t.Chdir(dir)
	rootCmd := cli.NewRootCmd()
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"generate", "--config", "sqlgen.yml"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("sqlgen generate: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
}

func gtwWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { //nolint:gosec // path is under the test's temp dir
		t.Fatal(err)
	}
}

// gtwOutput is the output block of a probe's sqlgen.yml.
func gtwOutput(driver string) string {
	return "output:\n  driver: " + driver + "\n  dir: ./models\n  package: models\n"
}

// gtwRefRetype retypes items.ref to uuid.UUID from the library at imp.
func gtwRefRetype(imp string) string {
	return "tables:\n  items:\n    column_map:\n      ref:\n        type: uuid.UUID\n        import: " + imp + "\n"
}

// gtwItemsText is a table whose ref column is text.
const gtwItemsText = "CREATE TABLE items (id bigint PRIMARY KEY, ref text NOT NULL);\n"

// gtwPgxOpen and gtwStdlibOpen are the probe-side client constructors.
const (
	gtwPgxOpen = `
func open(t *testing.T) (*models.Client, func(sql string, args ...any) error) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), os.Getenv("GTW_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	exec := func(sql string, args ...any) error {
		_, err := pool.Exec(context.Background(), sql, args...)
		return err
	}
	return models.New(dbpgx.New(pool)), exec
}
`
	gtwStdlibOpen = `
func open(t *testing.T) (*models.Client, func(sql string, args ...any) error) {
	t.Helper()
	db, err := sql.Open(driverName, os.Getenv("GTW_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	exec := func(query string, args ...any) error {
		_, err := db.ExecContext(context.Background(), query, args...)
		return err
	}
	return models.New(dbstdlib.New(db)), exec
}
`
)

// gtwTextRetypeScans is the probe for a library whose uuid.UUID scans from
// and binds to a text column: Create writes the value as text, and Get reads
// it back, as does a row written with plain text.
func gtwTextRetypeScans(pkg, imp, newUUID, opener, driverImports string) string {
	return `package probe

import (
	"context"
	"database/sql"
	"os"
	"testing"

	uuid "` + imp + `"
` + driverImports + `
	"` + gtwProbeModule + `/` + pkg + `/models"
)

var _ = sql.ErrNoRows

` + opener + `
func TestTextColumnRetypedToUUID(t *testing.T) {
	ctx := context.Background()
	client, exec := open(t)
	want := ` + newUUID + `

	if _, err := client.Items().Create(ctx, &models.CreateItemInput{ID: 1, Ref: want}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := client.Items().Get(ctx, 1)
	if err != nil {
		t.Fatalf("Get of the row Create wrote: %v", err)
	}
	if got.Ref != want {
		t.Errorf("Get of the row Create wrote: Ref = %v, want %v", got.Ref, want)
	}

	if err := exec("INSERT INTO items (id, ref) VALUES (2, '" + want.String() + "')"); err != nil {
		t.Fatalf("insert text: %v", err)
	}
	got, err = client.Items().Get(ctx, 2)
	if err != nil {
		t.Fatalf("Get of a row written as text: %v", err)
	}
	if got.Ref != want {
		t.Errorf("Get of a row written as text: Ref = %v, want %v", got.Ref, want)
	}
}
`
}

const (
	gtwPgxImports    = "\t\"github.com/jackc/pgx/v5/pgxpool\"\n\tdbpgx \"github.com/teandresmith/sqlgen/database/pgx\"\n"
	gtwStdlibImports = "\t_ \"github.com/jackc/pgx/v5/stdlib\"\n\tdbstdlib \"github.com/teandresmith/sqlgen/database/stdlib\"\n"
	gtwStdlibDriver  = "\nconst driverName = \"pgx\"\n"
)

// TestUUIDRetype_TextColumnUnderPgx retypes a PostgreSQL text column to
// uuid.UUID under output.driver: pgx. pgx keys its codec on the column's
// type, so the standard library's uuid.UUID, which ships no Scan, cannot be
// read from text; google's, which ships Scan and Value, can.
func TestUUIDRetype_TextColumnUnderPgx(t *testing.T) {
	gtwRunProbes(t, []gtwProbe{
		{
			pkg:     "pgx_stdlib_uuid",
			dialect: "postgres",
			config:  gtwOutput("pgx") + gtwRefRetype("uuid"),
			schema:  gtwItemsText,
			test: `package probe

import (
	"context"
	"os"
	"strings"
	"testing"

` + gtwPgxImports + `
	"` + gtwProbeModule + `/pgx_stdlib_uuid/models"
)
` + gtwPgxOpen + `
func TestStdlibUUIDFromText(t *testing.T) {
	client, exec := open(t)
	if err := exec("INSERT INTO items (id, ref) VALUES (1, '6ba7b810-9dad-11d1-80b4-00c04fd430c8')"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := client.Items().Get(context.Background(), 1)
	const want = "cannot scan text (OID 25) in text format into *uuid.UUID"
	if err == nil {
		t.Fatalf("Get: Ref = %v, want an error containing %q", got.Ref, want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Get error = %q, want containing %q", err, want)
	}
}
`,
		},
		{
			pkg:     "pgx_google_uuid",
			dialect: "postgres",
			config:  gtwOutput("pgx") + gtwRefRetype("github.com/google/uuid"),
			schema:  gtwItemsText,
			test:    gtwTextRetypeScans("pgx_google_uuid", "github.com/google/uuid", "uuid.New()", gtwPgxOpen, gtwPgxImports),
		},
	})
}

// TestUUIDRetype_TextColumnUnderStdlibDriver retypes a PostgreSQL text
// column to uuid.UUID under output.driver: stdlib, for every UUID
// integration. database/sql converts to the destination type whatever the
// column's type, so each one reads and writes the text column.
func TestUUIDRetype_TextColumnUnderStdlibDriver(t *testing.T) {
	gtwRunProbes(t, []gtwProbe{
		{
			pkg:     "stdlib_stdlib_uuid",
			dialect: "postgres",
			config:  gtwOutput("stdlib") + gtwRefRetype("uuid"),
			schema:  gtwItemsText,
			test:    gtwTextRetypeScans("stdlib_stdlib_uuid", "uuid", "uuid.NewV4()", gtwStdlibDriver+gtwStdlibOpen, gtwStdlibImports),
		},
		{
			pkg:     "stdlib_google_uuid",
			dialect: "postgres",
			config:  gtwOutput("stdlib") + gtwRefRetype("github.com/google/uuid"),
			schema:  gtwItemsText,
			test:    gtwTextRetypeScans("stdlib_google_uuid", "github.com/google/uuid", "uuid.New()", gtwStdlibDriver+gtwStdlibOpen, gtwStdlibImports),
		},
		{
			pkg:     "stdlib_gofrs_uuid",
			dialect: "postgres",
			config:  gtwOutput("stdlib") + gtwRefRetype("github.com/gofrs/uuid/v5"),
			schema:  gtwItemsText,
			test:    gtwTextRetypeScans("stdlib_gofrs_uuid", "github.com/gofrs/uuid/v5", "uuid.Must(uuid.NewV4())", gtwStdlibDriver+gtwStdlibOpen, gtwStdlibImports),
		},
	})
}

// TestUUIDColumn_StdlibUUIDUnderDatabaseSQL generates the default UUID
// binding for a PostgreSQL uuid column under output.driver: stdlib. The
// driver hands database/sql the column as a string, and the standard
// library's uuid.UUID, which ships no Scan or Value, still scans from it and
// binds into it.
func TestUUIDColumn_StdlibUUIDUnderDatabaseSQL(t *testing.T) {
	gtwRunProbes(t, []gtwProbe{
		{
			pkg:     "stdlib_uuid_column",
			dialect: "postgres",
			config:  gtwOutput("stdlib"),
			schema:  "CREATE TABLE items (id bigint PRIMARY KEY, key uuid NOT NULL);\n",
			test: `package probe

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"uuid"

` + gtwStdlibImports + `
	"` + gtwProbeModule + `/stdlib_uuid_column/models"
)
` + gtwStdlibDriver + `
func TestStdlibUUIDOnUUIDColumn(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open(driverName, os.Getenv("GTW_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	client := models.New(dbstdlib.New(db))
	want := uuid.NewV4()

	if _, err := client.Items().Create(ctx, &models.CreateItemInput{ID: 1, Key: want}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The driver value of the column is a string.
	var raw any
	if err := db.QueryRowContext(ctx, "SELECT key FROM items WHERE id = 1").Scan(&raw); err != nil {
		t.Fatalf("select raw: %v", err)
	}
	if s, ok := raw.(string); !ok || s != want.String() {
		t.Fatalf("driver value = %#v (%T), want the string %q", raw, raw, want.String())
	}

	got, err := client.Items().Get(ctx, 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Key != want {
		t.Errorf("Get: Key = %v, want %v", got.Key, want)
	}
}
`,
		},
	})
}

// gtwJSONTypedProbe is the probe for a JSON column retyped to
// []jsontypes.YearValue: a row written as JSON text scans straight into the
// typed slice, and Create writes the slice as the JSON the column reads back.
func gtwJSONTypedProbe(pkg, opener, driverImports string) string {
	return `package probe

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"

` + driverImports + `
	"` + gtwProbeModule + `/jsontypes"
	"` + gtwProbeModule + `/` + pkg + `/models"
)

var _ = sql.ErrNoRows

` + opener + `
func TestJSONColumnRetypedToTypedSlice(t *testing.T) {
	ctx := context.Background()
	client, exec := open(t)
	if err := exec("INSERT INTO assets (id, property_tax_rate) VALUES (1, '[{\"year\": 2024, \"value\": 1.25}, {\"year\": 2025, \"value\": 1.5}]')"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := client.Assets().Get(ctx, 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := []jsontypes.YearValue{{Year: 2024, Value: 1.25}, {Year: 2025, Value: 1.5}}
	if diff := cmp.Diff(want, got.PropertyTaxRate); diff != "" {
		t.Errorf("Get: PropertyTaxRate mismatch (-want +got):\n%s", diff)
	}

	written := []jsontypes.YearValue{{Year: 2026, Value: 2}}
	if _, err := client.Assets().Create(ctx, &models.CreateAssetInput{ID: 2, PropertyTaxRate: written}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err = client.Assets().Get(ctx, 2)
	if err != nil {
		t.Fatalf("Get of the row Create wrote: %v", err)
	}
	if diff := cmp.Diff(written, got.PropertyTaxRate); diff != "" {
		t.Errorf("Get of the row Create wrote: PropertyTaxRate mismatch (-want +got):\n%s", diff)
	}
}
`
}

// gtwJSONRetype retypes assets.property_tax_rate to []jsontypes.YearValue.
const gtwJSONRetype = "tables:\n  assets:\n    column_map:\n      property_tax_rate:\n        type: \"[]jsontypes.YearValue\"\n        import: " + gtwProbeModule + "/jsontypes\n"

// TestJSONTypedAccessOverride_Postgres retypes a jsonb column to a typed
// slice under output.driver: pgx and reads and writes it with no decode step.
func TestJSONTypedAccessOverride_Postgres(t *testing.T) {
	gtwRunProbes(t, []gtwProbe{
		{
			pkg:     "pgx_json_typed",
			dialect: "postgres",
			config:  gtwOutput("pgx") + gtwJSONRetype,
			schema:  "CREATE TABLE assets (id bigint PRIMARY KEY, property_tax_rate jsonb NOT NULL);\n",
			test:    gtwJSONTypedProbe("pgx_json_typed", gtwPgxOpen, gtwPgxImports),
		},
	})
}
