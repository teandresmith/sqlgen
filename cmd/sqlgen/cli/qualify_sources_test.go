package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/parser"
)

// qvwGenerate writes the files under dir, a config reading schema.sql and
// the views/ directory under source: files with the manifest on, and extra
// appended to that config, then runs generate. It returns the output dir.
func qvwGenerate(t *testing.T, dialect, driver string, files map[string]string, extra string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	outDir := filepath.Join(dir, "models")
	src := testSource{dialect: dialect, driver: driver, source: "files", outputDir: outDir}
	src.paths = []string{filepath.Join(dir, "schema.sql")}
	for name, content := range files {
		writeFile(t, filepath.Dir(filepath.Join(dir, name)), filepath.Base(name), content)
		if filepath.Dir(name) == "views" && len(src.views) == 0 {
			src.views = []string{filepath.Join(dir, "views")}
		}
	}
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	writeTestConfigWithSource(t, cfgPath, src)
	if extra != "" {
		cfg, err := os.ReadFile(cfgPath) //nolint:gosec // test reads its own temp config
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfgPath, append(cfg, extra...), 0o600); err != nil { //nolint:gosec // test writes its own temp config
			t.Fatal(err)
		}
	}
	if _, stderr, err := executeCommand("generate", "--config", cfgPath, "--manifest-timestamp", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatalf("generate: %v\nstderr: %s", err, stderr)
	}
	return outDir
}

// qvwViewNames returns the Go name the manifest gives each view, by
// schema-qualified SQL name (the bare name when the view has no schema).
func qvwViewNames(t *testing.T, outDir string) map[string]string {
	t.Helper()
	var m struct {
		Entities []struct {
			Kind   string `json:"kind"`
			Name   string `json:"name"`
			Table  string `json:"table"`
			Schema string `json:"schema"`
		} `json:"entities"`
	}
	if err := json.Unmarshal([]byte(readGenerated(t, outDir, "manifest/manifest_gen.json")), &m); err != nil {
		t.Fatalf("decoding manifest: %v", err)
	}
	out := make(map[string]string)
	for _, e := range m.Entities {
		if e.Kind != "view" {
			continue
		}
		key := e.Table
		if e.Schema != "" {
			key = e.Schema + "." + key
		}
		out[key] = e.Name
	}
	return out
}

// TestGenerate_ViewsConfigKeyQualification checks how a views: config key
// finds its view on PostgreSQL: a bare key matches the view in whatever
// schema it is in, and a schema-qualified key matches only the view in that
// schema, leaving a same-named view in another schema alone.
func TestGenerate_ViewsConfigKeyQualification(t *testing.T) {
	const schema = "CREATE SCHEMA reporting;\nCREATE TABLE products (id integer PRIMARY KEY, name text NOT NULL);\n"
	const reportingView = "-- @pk: id\nCREATE VIEW reporting.product_summary AS SELECT id, name FROM products;\n"
	tests := []struct {
		name  string
		files map[string]string
		key   string
		want  map[string]string
	}{
		{
			name: "bare key, view only in a non-public schema",
			files: map[string]string{
				"schema.sql":            schema,
				"views/product_sum.sql": reportingView,
			},
			key:  "product_summary",
			want: map[string]string{"reporting.product_summary": "Rollup"},
		},
		{
			name: "bare key, view in public",
			files: map[string]string{
				"schema.sql":            schema,
				"views/product_sum.sql": "-- @pk: id\nCREATE VIEW product_summary AS SELECT id, name FROM products;\n",
			},
			key:  "product_summary",
			want: map[string]string{"public.product_summary": "Rollup"},
		},
		{
			name: "qualified key beside a same-named view in public",
			files: map[string]string{
				"schema.sql":                schema,
				"views/product_sum.sql":     reportingView,
				"views/public_products.sql": "-- @pk: id\nCREATE VIEW product_summary AS SELECT id FROM products;\n",
			},
			key: "reporting.product_summary",
			want: map[string]string{
				"public.product_summary":    "PublicProductSummary",
				"reporting.product_summary": "Rollup",
			},
		},
		{
			name: "qualified key naming the other schema",
			files: map[string]string{
				"schema.sql":                schema,
				"views/product_sum.sql":     reportingView,
				"views/public_products.sql": "-- @pk: id\nCREATE VIEW product_summary AS SELECT id FROM products;\n",
			},
			key: "public.product_summary",
			want: map[string]string{
				"public.product_summary":    "Rollup",
				"reporting.product_summary": "ReportingProductSummary",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outDir := qvwGenerate(t, "postgres", "pgx", tt.files, "views:\n  "+tt.key+":\n    struct_name: Rollup\n")
			if diff := cmp.Diff(tt.want, qvwViewNames(t, outDir)); diff != "" {
				t.Errorf("views key %q: view Go names (-want +got):\n%s", tt.key, diff)
			}
		})
	}
}

// TestGenerate_UnresolvedForeignKeyNoRelationship checks that on MySQL and
// SQLite a foreign key into a table no file defines gives no relationship,
// while the same table's key into a parsed table gives its edge, and
// generate succeeds with no entity for the missing table.
func TestGenerate_UnresolvedForeignKeyNoRelationship(t *testing.T) {
	tests := []struct {
		name    string
		dialect config.Dialect
		schema  string
	}{
		{
			name:    "mysql",
			dialect: config.DialectMySQL,
			schema: `CREATE TABLE owners (id int NOT NULL PRIMARY KEY);
CREATE TABLE pets (
	id int NOT NULL PRIMARY KEY,
	owner_id int NOT NULL,
	ghost_id int NOT NULL,
	FOREIGN KEY (owner_id) REFERENCES owners (id),
	FOREIGN KEY (ghost_id) REFERENCES ghosts (id)
);
`,
		},
		{
			name:    "sqlite",
			dialect: config.DialectSQLite,
			schema: `CREATE TABLE owners (id INTEGER PRIMARY KEY);
CREATE TABLE pets (
	id INTEGER PRIMARY KEY,
	owner_id INTEGER NOT NULL REFERENCES owners(id),
	ghost_id INTEGER NOT NULL REFERENCES ghosts(id)
);
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := tdfParseFiles(t, config.InputConfig{Dialect: tt.dialect}, map[string]string{"schema.sql": tt.schema}, nil)
			parser.DetectRelationships(schema)
			got := make([]string, 0, len(schema.Relationships))
			for _, r := range schema.Relationships {
				got = append(got, r.SourceTable+" -> "+r.TargetTable)
			}
			slices.Sort(got)
			want := []string{"owners -> pets", "pets -> owners"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("relationships (-want +got):\n%s", diff)
			}

			outDir := qvwGenerate(t, string(tt.dialect), "stdlib", map[string]string{"schema.sql": tt.schema}, "")
			entities := manifestEntities(t, outDir)
			if _, ok := entities["ghosts"]; ok {
				t.Error("ghosts was generated, want no such entity")
			}
			if diff := cmp.Diff([]string{"Pet"}, relationshipTargets(entities["owners"])); diff != "" {
				t.Errorf("owners relationship targets (-want +got):\n%s", diff)
			}
		})
	}
}
