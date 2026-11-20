package gen_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

const limSchemaDDL = `
CREATE TABLE companies (id uuid PRIMARY KEY, name text NOT NULL);
CREATE TABLE products (
	id uuid PRIMARY KEY,
	name text NOT NULL,
	company_id uuid NOT NULL REFERENCES companies(id)
);`

// limConfig is a sqlgen.yml with the GraphQL API switched by graphqlEnabled;
// limits is appended under `api.graphql` (keys indented four spaces).
func limConfig(graphqlEnabled bool, limits string) string {
	enabled := "false"
	if graphqlEnabled {
		enabled = "true"
	}
	return `version: v1
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
    enabled: ` + enabled + `
    schema_dir: ./models/graph
    resolver_dir: ./models/graph
    package: graph
    field_casing: camel_case
` + limits
}

// limProject chdirs into a fresh module root and returns it with the parsed
// schema.
func limProject(t *testing.T) (string, *parser.Schema) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/shop\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	t.Chdir(root)
	schema := parseDDLForTest(t, config.DialectPostgres, limSchemaDDL)
	detectRelationships(schema)
	return root, schema
}

// limLoad writes body as sqlgen.yml in a temp dir and loads it through the
// production loader so defaults apply.
func limLoad(t *testing.T, body string) *config.RootConfig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sqlgen.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	return cfg
}

// limGenerate runs Generate and fails the test on error.
func limGenerate(t *testing.T, schema *parser.Schema, cfg *config.RootConfig) *gen.GenerateResult {
	t.Helper()
	res, err := gen.Generate(schema, cfg, "test")
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	return res
}

// limRead returns the file at root/rel, failing when it is absent.
func limRead(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel)) //nolint:gosec // a generated file under the test's temp project root
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(b)
}

// limReported reports whether the run listed a file ending in suffix.
func limReported(res *gen.GenerateResult, suffix string) bool {
	return slices.ContainsFunc(res.Files, func(f string) bool {
		return strings.HasSuffix(filepath.ToSlash(f), suffix)
	})
}

const (
	limServerRel = "models/graph/server.go"
	limLimitsRel = "models/graph/sqlgenresolver/limits_gen.go"
)

func TestGenerate_GraphQLQueryLimits(t *testing.T) {
	t.Run("server.go is written when absent", func(t *testing.T) {
		root, schema := limProject(t)
		res := limGenerate(t, schema, limLoad(t, limConfig(true, "")))

		if !limReported(res, limServerRel) {
			t.Errorf("Generate() files do not list %s:\n%s", limServerRel, strings.Join(res.Files, "\n"))
		}
		server := limRead(t, root, limServerRel)
		for _, want := range []string{
			"package graph",
			"func NewServer(client *models.Client) http.Handler",
			"handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{",
			"sqlgenresolver.ApplyQueryLimits(srv)",
			"return sqlgenresolver.WithCallOptionsMiddleware(srv)",
		} {
			if !strings.Contains(server, want) {
				t.Errorf("server.go missing %q:\n%s", want, server)
			}
		}
		if strings.Contains(server, gen.GeneratedHeaderPrefix) {
			t.Errorf("server.go is consumer-owned but carries the generated header:\n%s", server)
		}
	})

	t.Run("an existing server.go is left untouched", func(t *testing.T) {
		root, schema := limProject(t)
		const own = "package graph\n\n// consumer-owned bootstrap\n"
		path := filepath.Join(root, limServerRel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("creating graph dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(own), 0o600); err != nil {
			t.Fatalf("writing server.go: %v", err)
		}

		res := limGenerate(t, schema, limLoad(t, limConfig(true, "")))

		if got := limRead(t, root, limServerRel); got != own {
			t.Errorf("server.go = %q, want it left as %q", got, own)
		}
		if limReported(res, limServerRel) {
			t.Errorf("Generate() files list %s, which it did not write", limServerRel)
		}
	})

	t.Run("the limits file is regenerated with new values on a second run", func(t *testing.T) {
		root, schema := limProject(t)
		limGenerate(t, schema, limLoad(t, limConfig(true, "    max_depth: 3\n    max_complexity: 77\n")))
		firstServer := limRead(t, root, limServerRel)

		limGenerate(t, schema, limLoad(t, limConfig(true, "    max_depth: 4\n    max_complexity: 88\n")))

		limits := limRead(t, root, limLimitsRel)
		for _, want := range []string{"const MaxQueryDepth = 4", "const MaxQueryComplexity = 88"} {
			if !strings.Contains(limits, want) {
				t.Errorf("limits_gen.go missing %q after the second run:\n%s", want, limits)
			}
		}
		if got := limRead(t, root, limServerRel); got != firstServer {
			t.Errorf("server.go changed on the second run:\n%s", got)
		}
	})

	t.Run("defaults are depth 5 and complexity 1000 when unset", func(t *testing.T) {
		root, schema := limProject(t)
		res := limGenerate(t, schema, limLoad(t, limConfig(true, "")))

		if !limReported(res, limLimitsRel) {
			t.Errorf("Generate() files do not list %s", limLimitsRel)
		}
		limits := limRead(t, root, limLimitsRel)
		for _, want := range []string{"const MaxQueryDepth = 5", "const MaxQueryComplexity = 1000"} {
			if !strings.Contains(limits, want) {
				t.Errorf("limits_gen.go missing %q:\n%s", want, limits)
			}
		}
	})

	t.Run("relationship fields are the ones that count toward depth", func(t *testing.T) {
		root, schema := limProject(t)
		limGenerate(t, schema, limLoad(t, limConfig(true, "")))

		limits := limRead(t, root, limLimitsRel)
		if !strings.Contains(limits, `"Company.products"`) {
			t.Errorf("isRelationshipField does not list \"Company.products\":\n%s", limits)
		}
		for _, scalar := range []string{`"Company.name"`, `"Product.name"`} {
			if strings.Contains(limits, scalar) {
				t.Errorf("isRelationshipField lists the scalar field %s:\n%s", scalar, limits)
			}
		}
	})

	t.Run("server_scaffold false writes no server.go", func(t *testing.T) {
		root, schema := limProject(t)
		res := limGenerate(t, schema, limLoad(t, limConfig(true, "    server_scaffold: false\n")))

		if limReported(res, limServerRel) {
			t.Errorf("Generate() files list %s with server_scaffold false", limServerRel)
		}
		if _, err := os.Stat(filepath.Join(root, limServerRel)); !os.IsNotExist(err) {
			t.Errorf("%s exists with server_scaffold false (stat error = %v)", limServerRel, err)
		}
	})

	t.Run("server_scaffold false still writes the limits file", func(t *testing.T) {
		root, schema := limProject(t)
		res := limGenerate(t, schema, limLoad(t, limConfig(true, "    server_scaffold: false\n    max_depth: 3\n    max_complexity: 77\n")))

		if !limReported(res, limLimitsRel) {
			t.Errorf("Generate() files do not list %s", limLimitsRel)
		}
		limits := limRead(t, root, limLimitsRel)
		for _, want := range []string{"const MaxQueryDepth = 3", "const MaxQueryComplexity = 77", "func ApplyQueryLimits("} {
			if !strings.Contains(limits, want) {
				t.Errorf("limits_gen.go missing %q:\n%s", want, limits)
			}
		}
	})

	t.Run("server_scaffold false leaves an existing server.go untouched", func(t *testing.T) {
		root, schema := limProject(t)
		const own = "package graph\n\n// consumer-owned bootstrap\n"
		path := filepath.Join(root, limServerRel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("creating graph dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(own), 0o600); err != nil {
			t.Fatalf("writing server.go: %v", err)
		}

		res := limGenerate(t, schema, limLoad(t, limConfig(true, "    server_scaffold: false\n")))

		if got := limRead(t, root, limServerRel); got != own {
			t.Errorf("server.go = %q, want it left as %q", got, own)
		}
		if limReported(res, limServerRel) {
			t.Errorf("Generate() files list %s, which it did not write", limServerRel)
		}
	})

	t.Run("nothing is emitted when api.graphql.enabled is false", func(t *testing.T) {
		root, schema := limProject(t)
		res := limGenerate(t, schema, limLoad(t, limConfig(false, "    max_depth: 3\n")))

		for _, rel := range []string{limServerRel, limLimitsRel} {
			if limReported(res, rel) {
				t.Errorf("Generate() files list %s with the GraphQL API disabled", rel)
			}
			if _, err := os.Stat(filepath.Join(root, rel)); !os.IsNotExist(err) {
				t.Errorf("%s exists with the GraphQL API disabled (stat error = %v)", rel, err)
			}
		}
	})
}
