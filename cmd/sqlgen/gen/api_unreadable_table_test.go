package gen_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// A table whose every column an access role drops from the API has nothing a
// GraphQL caller could read, so it gets no API surface of its own — the same
// outcome as `api.enabled: false`. Otherwise the table would emit `type <T>`
// with only relationship fields and an empty `<T>SortField` enum, which
// gqlparser rejects.
//
// A validated config cannot reach this state today: the access rules require
// a primary-key column to stay API-readable, and a keyless table generates no
// entity at all. The generator does not re-run config validation, so the rule
// is pinned here by driving Generate directly with a config that hides the
// key too — the same bypass the relationship-target exposure tests use. The
// first subtest records that the bypass is needed, so the day validation
// admits the shape this test is already exercising the real path.
const urtSchemaDDL = `
CREATE TABLE users (id uuid PRIMARY KEY, email text NOT NULL);
CREATE TABLE audit_notes (
	id uuid PRIMARY KEY,
	user_id uuid REFERENCES users(id),
	note text
);`

const urtConfig = `version: v1
input:
  dialect: postgres
  paths:
    - ./schema.sql
output:
  driver: pgx
  dir: ./models
  package: models
api:
  enabled: true
  graphql:
    enabled: true
    schema_dir: ./models/graph
    resolver_dir: ./models/graph
    package: graph
    field_casing: camel_case
tables:
  audit_notes:
    column_map:
      id:
        access: hidden
      user_id:
        access: hidden
      note:
        access: internal
`

// urtProject chdirs into a fresh module root, writes the schema and config
// there, and returns the root, the parsed schema and the loaded config.
func urtProject(t *testing.T) (string, *parser.Schema, *config.RootConfig) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/shop\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "schema.sql"), []byte(urtSchemaDDL), 0o600); err != nil {
		t.Fatalf("writing schema.sql: %v", err)
	}
	cfgPath := filepath.Join(root, "sqlgen.yml")
	if err := os.WriteFile(cfgPath, []byte(urtConfig), 0o600); err != nil {
		t.Fatalf("writing sqlgen.yml: %v", err)
	}
	t.Chdir(root)
	schema := parseDDLForTest(t, config.DialectPostgres, urtSchemaDDL)
	detectRelationships(schema)
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	return root, schema, cfg
}

// urtGraphQLSchemas returns every generated .graphqls file under root, keyed
// by its slash-separated root-relative path.
func urtGraphQLSchemas(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".graphqls") {
			return nil
		}
		b, err := os.ReadFile(path) //nolint:gosec // a generated file under the test's temp project root
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return out
}

func TestAPIUnreadableTable(t *testing.T) {
	t.Run("the config validator rejects the hidden primary key", func(t *testing.T) {
		_, schema, cfg := urtProject(t)
		_, err := config.ValidatePostParse(cfg, urtSchemaTables(schema), nil)
		if err == nil || !strings.Contains(err.Error(), "audit_notes.column_map.id.access") {
			t.Fatalf("ValidatePostParse() error = %v, want the primary-key readability rule on audit_notes.id", err)
		}
	})

	t.Run("the table has no API context while its parent keeps one", func(t *testing.T) {
		_, schema, cfg := urtProject(t)
		tables, views, err := gen.BuildEntityContextsFromSchema(schema, cfg)
		if err != nil {
			t.Fatalf("BuildEntityContextsFromSchema: %v", err)
		}
		apiCtx, err := gen.BuildAPIContext(tables, views, nil, nil, cfg)
		if err != nil {
			t.Fatalf("BuildAPIContext: %v", err)
		}
		names := make([]string, 0, len(apiCtx.Tables))
		for _, at := range apiCtx.Tables {
			names = append(names, at.StructName)
		}
		if !slices.Contains(names, "User") {
			t.Fatalf("API tables = %v, want User kept", names)
		}
		if slices.Contains(names, "AuditNote") {
			t.Errorf("API tables = %v, want AuditNote absent", names)
		}
	})

	t.Run("no generated schema declares the type or an empty sort enum", func(t *testing.T) {
		root, schema, cfg := urtProject(t)
		if _, err := gen.Generate(schema, cfg, "test"); err != nil {
			t.Fatalf("Generate() error: %v", err)
		}
		files := urtGraphQLSchemas(t, root)
		if _, ok := files["models/graph/user_gen.graphqls"]; !ok {
			names := make([]string, 0, len(files))
			for k := range files {
				names = append(names, k)
			}
			slices.Sort(names)
			t.Fatalf("user_gen.graphqls missing; generated schemas: %v", names)
		}
		if _, ok := files["models/graph/audit_note_gen.graphqls"]; ok {
			t.Error("audit_note_gen.graphqls was written for a table with no readable column")
		}
		emptyEnum := regexp.MustCompile(`enum \w+ \{\s*\}`)
		auditDecl := regexp.MustCompile(`(?m)^(type|input|enum) AuditNote`)
		for path, body := range files {
			if m := emptyEnum.FindString(body); m != "" {
				t.Errorf("%s declares an empty enum %q, which gqlparser rejects", path, m)
			}
			if m := auditDecl.FindString(body); m != "" {
				t.Errorf("%s declares %q for a table with no readable column", path, m)
			}
			if strings.Contains(body, "auditNote") {
				t.Errorf("%s names an auditNote field or root operation:\n%s", path, body)
			}
		}
	})
}

// urtSchemaTables projects the parsed tables onto the lightweight schema
// types the schema-phase validator reads.
func urtSchemaTables(schema *parser.Schema) []config.SchemaTable {
	out := make([]config.SchemaTable, 0, len(schema.Tables))
	for _, tbl := range schema.Tables {
		st := config.SchemaTable{Name: tbl.Name, Schema: tbl.Schema}
		for _, c := range tbl.Columns {
			st.Columns = append(st.Columns, config.SchemaColumn{
				Name:       c.Name,
				Type:       c.Type,
				Nullable:   c.Nullable,
				PrimaryKey: c.PrimaryKey,
				HasDefault: c.Default != "",
			})
		}
		out = append(out, st)
	}
	return out
}
