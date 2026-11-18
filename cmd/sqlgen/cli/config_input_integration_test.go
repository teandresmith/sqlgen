package cli

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/internal/livedb"
)

// tdfManifestTables returns the sorted table names of the manifest's entities.
func tdfManifestTables(t *testing.T, outDir string) []string {
	t.Helper()
	byTable := manifestEntities(t, outDir)
	names := make([]string, 0, len(byTable))
	for name := range byTable {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// TestGenerate_IntrospectEmptySchemasMeansAllNonSystem pins §4.4 on
// PostgreSQL: with introspect.schemas empty, introspection reads every
// non-system schema — tables in public and in another schema are generated,
// and nothing from pg_catalog or information_schema is.
func TestGenerate_IntrospectEmptySchemasMeansAllNonSystem(t *testing.T) {
	conn := livedb.NewPostgresDatabase(t)
	livedb.ExecSQL(t, "pgx", conn, `CREATE TABLE users (id integer PRIMARY KEY, name text NOT NULL);
CREATE SCHEMA billing;
CREATE TABLE billing.invoices (id integer PRIMARY KEY);
CREATE SCHEMA audit;
CREATE TABLE audit.events (id integer PRIMARY KEY);`)

	tests := []struct {
		name           string
		excludeSchemas []string
		want           []string
	}{
		{name: "no introspect block", want: []string{"events", "invoices", "users"}},
		{name: "introspect block without schemas", excludeSchemas: []string{"audit"}, want: []string{"invoices", "users"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			outDir := filepath.Join(dir, "models")
			src := testSource{dialect: "postgres", driver: "pgx", source: "database", url: conn, outputDir: outDir, excludeSchemas: tt.excludeSchemas}
			generateSource(t, dir, src)
			if diff := cmp.Diff(tt.want, tdfManifestTables(t, outDir)); diff != "" {
				t.Errorf("generated tables mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestGenerate_ExcludeTablesAppliesToEverySource pins §4.4: exclude_tables
// drops a matching table whether the schema comes from SQL files or from
// introspection.
func TestGenerate_ExcludeTablesAppliesToEverySource(t *testing.T) {
	fixtures := make(map[string]dialectFixture)
	for _, f := range dialectFixtures() {
		fixtures[f.dialect] = f
	}
	tests := []struct {
		name    string
		dialect string
		source  string
	}{
		{name: "postgres files", dialect: "postgres", source: "files"},
		{name: "postgres database", dialect: "postgres", source: "database"},
		{name: "mysql files", dialect: "mysql", source: "files"},
		{name: "mysql database", dialect: "mysql", source: "database"},
		{name: "sqlite files", dialect: "sqlite", source: "files"},
		{name: "sqlite database", dialect: "sqlite", source: "database"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := fixtures[tt.dialect]
			dir := t.TempDir()
			t.Chdir(dir)
			outDir := filepath.Join(dir, "models")
			src := f.source(tt.source, "", outDir)
			if tt.source == "files" {
				src.paths = []string{writeFile(t, filepath.Join(dir, "schema"), "schema.sql", f.users+f.posts)}
			} else {
				src.url = f.newDatabase(t)
				f.apply(t, src.url, f.users+f.posts)
			}
			cfgPath := filepath.Join(dir, "sqlgen.yml")
			writeTestConfigWithSource(t, cfgPath, src)
			appendConfig(t, cfgPath, "exclude_tables:\n  - posts\n")
			if _, stderr, err := executeCommand("generate", "--config", cfgPath, "--manifest-timestamp", "2026-01-01T00:00:00Z"); err != nil {
				t.Fatalf("generate (source %s): %v\nstderr: %s", tt.source, err, stderr)
			}
			readGenerated(t, outDir, "user_gen.go")
			assertNoFile(t, outDir, "post_gen.go")
		})
	}
}
