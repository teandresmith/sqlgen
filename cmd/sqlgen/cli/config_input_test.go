package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/parser"
)

// tdfParseFiles writes each name -> body under a fresh temp dir and parses
// the schema with input.paths set to that dir's paths files and input.views
// to its views dir (when views is non-empty).
func tdfParseFiles(t *testing.T, in config.InputConfig, ddl, views map[string]string) *parser.Schema {
	t.Helper()
	dir := t.TempDir()
	for name, body := range ddl {
		in.Paths = append(in.Paths, writeFile(t, dir, name, body))
	}
	if len(views) > 0 {
		viewDir := filepath.Join(dir, "views")
		for name, body := range views {
			writeFile(t, viewDir, name, body)
		}
		in.Views = []string{viewDir}
	}
	if in.Source == "" {
		in.Source = config.SourceFiles
	}
	schema, err := parseSchema(t.Context(), &config.RootConfig{Input: in})
	if err != nil {
		t.Fatalf("parseSchema() error = %v, want nil", err)
	}
	return schema
}

// tdfTableNames returns "schema.name" for every table in schema.
func tdfTableNames(schema *parser.Schema) []string {
	names := make([]string, 0, len(schema.Tables))
	for _, tb := range schema.Tables {
		names = append(names, tb.Schema+"."+tb.Name)
	}
	return names
}

// TestParseSchema_InputSchemaResolvesUnqualifiedNames pins §4.2 input.schema
// on PostgreSQL: an unqualified table name and an unqualified REFERENCES
// target in the SQL files both resolve to the configured schema, not public.
func TestParseSchema_InputSchemaResolvesUnqualifiedNames(t *testing.T) {
	in := config.InputConfig{Dialect: config.DialectPostgres, Schema: "billing"}
	schema := tdfParseFiles(t, in, map[string]string{
		"schema.sql": `CREATE TABLE owners (id uuid PRIMARY KEY);
CREATE TABLE audit.pets (id uuid PRIMARY KEY, owner_id uuid REFERENCES owners(id));`,
	}, nil)

	t.Run("unqualified table name", func(t *testing.T) {
		if diff := cmp.Diff([]string{"audit.pets", "billing.owners"}, tdfTableNames(schema)); diff != "" {
			t.Errorf("parseSchema() tables mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("unqualified references target", func(t *testing.T) {
		var got *parser.FKReference
		for _, tb := range schema.Tables {
			if tb.Name != "pets" {
				continue
			}
			for _, col := range tb.Columns {
				if col.Name == "owner_id" {
					got = col.FKReference
				}
			}
		}
		if got == nil || got.Schema+"."+got.Table != "billing.owners" {
			t.Errorf("parseSchema() audit.pets.owner_id reference = %+v, want billing.owners", got)
		}
	})
}

// TestParseSchema_InputViewsFilesAreNotDDL pins §4.2 input.views: a file
// there is parsed only as a view, so a CREATE TABLE in it adds no table.
func TestParseSchema_InputViewsFilesAreNotDDL(t *testing.T) {
	in := config.InputConfig{Dialect: config.DialectPostgres, Schema: "*"}
	schema := tdfParseFiles(t, in,
		map[string]string{"schema.sql": "CREATE TABLE orders (id uuid PRIMARY KEY);"},
		map[string]string{"order_ids.sql": "CREATE TABLE ghosts (id uuid PRIMARY KEY);\n-- @pk: id\nCREATE VIEW order_ids AS SELECT o.id FROM orders o;\n"},
	)

	t.Run("no table from the views file", func(t *testing.T) {
		if diff := cmp.Diff([]string{"public.orders"}, tdfTableNames(schema)); diff != "" {
			t.Errorf("parseSchema() tables mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("the views file's view", func(t *testing.T) {
		got := make([]string, 0, len(schema.Views))
		for _, v := range schema.Views {
			got = append(got, v.Schema+"."+v.Name)
		}
		if diff := cmp.Diff([]string{"public.order_ids"}, got); diff != "" {
			t.Errorf("parseSchema() views mismatch (-want +got):\n%s", diff)
		}
	})
}

// TestParseSchema_IntrospectIgnoredUnderFileSource pins §4.2: under
// source: files, input.introspect filters nothing, so every table the files
// define is parsed whatever schemas, exclude_schemas and tables say.
func TestParseSchema_IntrospectIgnoredUnderFileSource(t *testing.T) {
	in := config.InputConfig{
		Dialect: config.DialectPostgres,
		Schema:  "*",
		Introspect: &config.IntrospectConfig{
			Schemas:        []string{"billing"},
			ExcludeSchemas: []string{"public"},
			Tables:         []string{"posts"},
		},
	}
	schema := tdfParseFiles(t, in, map[string]string{
		"schema.sql": "CREATE TABLE users (id uuid PRIMARY KEY);\nCREATE TABLE posts (id uuid PRIMARY KEY);",
	}, nil)
	if diff := cmp.Diff([]string{"public.posts", "public.users"}, tdfTableNames(schema)); diff != "" {
		t.Errorf("parseSchema() tables mismatch (-want +got):\n%s", diff)
	}
}

// TestGenerate_GqlgenConfigIsNotExpanded pins §4.1.1 "Only sqlgen.yml is
// expanded": a ${VAR} in the consumer's gqlgen.yml reaches gqlgen as
// written, in the merged config sqlgen hands it, even with VAR set.
func TestGenerate_GqlgenConfigIsNotExpanded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stand-in gqlgen is a POSIX shell script")
	}
	t.Setenv("TDF_GQL_DIR", "expanded")
	const reference = "${TDF_GQL_DIR}"
	gqlgenYML := twrFollowSchemaGqlgenYML + "exec:\n  filename: graph/" + reference + "/generated_gen.go\n"
	cfgPath, _ := twrGraphQLProject(t, "CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);", gqlgenYML, "")
	captured := filepath.Join(t.TempDir(), "merged.yml")
	// gqlgen is invoked as `<bin> generate --config <merged temp file>`.
	appendConfig(t, cfgPath, "    gqlgen_bin: "+twrStandInGqlgen(t, "cp \"$3\" '"+captured+"'\n")+"\n")

	_, stderr, _ := executeCommand("--config", cfgPath)

	merged, err := os.ReadFile(captured) //nolint:gosec // path is under the test's temp dir
	if err != nil {
		t.Fatalf("the stand-in gqlgen saw no merged config: %v\nstderr: %s", err, stderr)
	}
	t.Run("merged config keeps the reference", func(t *testing.T) {
		if !strings.Contains(string(merged), "filename: graph/"+reference+"/generated_gen.go") {
			t.Errorf("merged gqlgen config does not keep %q as written:\n%s", reference, merged)
		}
		if strings.Contains(string(merged), "graph/expanded/") {
			t.Errorf("merged gqlgen config holds the expanded value:\n%s", merged)
		}
	})
	t.Run("consumer gqlgen.yml is unchanged", func(t *testing.T) {
		onDisk, err := os.ReadFile("gqlgen.yml")
		if err != nil {
			t.Fatalf("reading gqlgen.yml: %v", err)
		}
		if string(onDisk) != gqlgenYML {
			t.Errorf("gqlgen.yml = %q, want %q", onDisk, gqlgenYML)
		}
	})
}
