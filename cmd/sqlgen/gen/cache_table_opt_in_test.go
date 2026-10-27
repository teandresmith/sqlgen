package gen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// TestBuildCacheContext_TableOptInUnderGlobalOff pins the per-table opt-in: a
// table whose cache.enabled is true is cached even when the global
// cache.enabled is false, and a table that does not opt in stays uncached.
func TestBuildCacheContext_TableOptInUnderGlobalOff(t *testing.T) {
	cfg := cacheTestConfig(true)
	cfg.Cache.Enabled = false
	cfg.Tables["products"] = config.TableConfig{Cache: &config.TableCacheConfig{Enabled: new(true)}}

	other := cacheTestTable()
	other.StructName, other.TableName, other.TableNameConstant = "Order", "orders", "TableOrders"

	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable(), other}, nil, cfg, "db", "Client")
	if ctx == nil {
		t.Fatal("BuildCacheContext = nil with a per-table opt-in under global cache.enabled: false, want a context caching products")
	}
	got := make([]string, 0, len(ctx.CachedTables))
	for _, ct := range ctx.CachedTables {
		got = append(got, ct.Name)
	}
	if len(got) != 1 || got[0] != "products" {
		t.Errorf("cached tables = %v, want [products]", got)
	}
}

// TestBuildCacheContext_NoOptInUnderGlobalOff pins the other side of the
// gate: with tables present, a global cache.enabled: false and no table or
// view opting in, nothing is cached and the context is nil, so no cache_gen.go
// is rendered.
func TestBuildCacheContext_NoOptInUnderGlobalOff(t *testing.T) {
	cfg := cacheTestConfig(true)
	cfg.Cache.Enabled = false

	other := cacheTestTable()
	other.StructName, other.TableName, other.TableNameConstant = "Order", "orders", "TableOrders"

	if ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable(), other}, nil, cfg, "db", "Client"); ctx != nil {
		t.Errorf("BuildCacheContext = %+v with no opt-in under global cache.enabled: false, want nil", ctx)
	}
}

// cacheOptInSchemaDDL is two independent tables: products opts into caching,
// orders does not.
const cacheOptInSchemaDDL = `
CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);
CREATE TABLE orders (id uuid PRIMARY KEY, total integer NOT NULL);`

const cacheOptInConfigBase = `version: v1
input:
  dialect: postgres
  paths:
    - ./schema.sql
output:
  driver: pgx
  dir: ./models
  package: models
  layout: file_per_table
tables:
  products:
    cache:
      enabled: true
`

// TestGenerate_CacheTableOptInUnderGlobalOff runs the whole pipeline for a
// per-table cache opt-in with caching globally off — once with a `cache:` block
// that says enabled: false, once with no block at all (PRD §27.2) — and checks
// that cache_gen.go is emitted caching only the opted-in table, that the client
// is wired to it, and that the generated package type-checks.
func TestGenerate_CacheTableOptInUnderGlobalOff(t *testing.T) {
	// The runtime module the generated package imports, resolved before the
	// working directory moves into the temp project.
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		cacheBlock  string
		wantKeyLine string
	}{
		{
			name:        "cache block with enabled false",
			cacheBlock:  "cache:\n  enabled: false\n  key_prefix: app\n",
			wantKeyLine: `prefix:           "app",`,
		},
		{
			name:        "no cache block",
			cacheBlock:  "",
			wantKeyLine: `prefix:           "sqlgen",`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for name, body := range map[string]string{
				"schema.sql": cacheOptInSchemaDDL,
				"sqlgen.yml": cacheOptInConfigBase + tt.cacheBlock,
			} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
					t.Fatalf("writing %s: %v", name, err)
				}
			}
			t.Chdir(root)
			schema := parseDDLForTest(t, config.DialectPostgres, cacheOptInSchemaDDL)
			cfg, err := config.LoadConfig(filepath.Join(root, "sqlgen.yml"))
			if err != nil {
				t.Fatalf("LoadConfig() error: %v", err)
			}
			if _, err := config.ValidatePreParse(cfg); err != nil {
				t.Fatalf("ValidatePreParse() error: %v", err)
			}
			if _, err := gen.Generate(schema, cfg, "test"); err != nil {
				t.Fatalf("Generate() error: %v", err)
			}

			cacheSrc, err := os.ReadFile(filepath.Join(root, "models", "cache_gen.go")) //nolint:gosec // test-written temp module
			if err != nil {
				t.Fatalf("cache_gen.go not emitted for a per-table opt-in under a global off: %v", err)
			}
			got := string(cacheSrc)
			if !strings.Contains(got, "const fingerprintProduct =") {
				t.Error("cache_gen.go does not cache products, the opted-in table")
			}
			if strings.Contains(got, "fingerprintOrder") {
				t.Error("cache_gen.go caches orders, which did not opt in")
			}
			if !strings.Contains(got, tt.wantKeyLine) {
				t.Errorf("cache_gen.go does not contain %q: the key prefix from the global block (or its default) is not baked in", tt.wantKeyLine)
			}
			clientSrc, err := os.ReadFile(filepath.Join(root, "models", "client_gen.go")) //nolint:gosec // test-written temp module
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(clientSrc), "func WithCache(") {
				t.Error("client_gen.go has no WithCache option although a table is cached")
			}

			vetGeneratedModels(t, root, repo)
		})
	}
}

// TestGenerate_CacheViewOptInUnderGlobalOff runs the whole pipeline for a
// view whose cache.enabled is true under a global `cache.enabled: false` with
// no table cached (PRD §27.11): cache_gen.go must be emitted caching that
// view, the client wired to it, and the package must type-check.
func TestGenerate_CacheViewOptInUnderGlobalOff(t *testing.T) {
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	const ddl = `CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);`
	const yml = `version: v1
input:
  dialect: postgres
  paths:
    - ./schema.sql
output:
  driver: pgx
  dir: ./models
  package: models
  layout: file_per_table
cache:
  enabled: false
views:
  product_names:
    struct_name: ProductName
    cache:
      enabled: true
    invalidate_on:
      - products
`
	root := t.TempDir()
	for name, body := range map[string]string{"schema.sql": ddl, "sqlgen.yml": yml} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	t.Chdir(root)
	schema := parseDDLForTest(t, config.DialectPostgres, ddl)
	schema.Views = append(schema.Views, parser.View{
		Name:   "product_names",
		Schema: "public",
		Columns: []parser.Column{
			{Name: "id", Type: "uuid"},
			{Name: "name", Type: "text"},
		},
	})
	cfg, err := config.LoadConfig(filepath.Join(root, "sqlgen.yml"))
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	if _, err := config.ValidatePreParse(cfg); err != nil {
		t.Fatalf("ValidatePreParse() error: %v", err)
	}
	if _, err := gen.Generate(schema, cfg, "test"); err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	cacheSrc, err := os.ReadFile(filepath.Join(root, "models", "cache_gen.go")) //nolint:gosec // test-written temp module
	if err != nil {
		t.Fatalf("cache_gen.go not emitted for a view-only opt-in under a global off: %v", err)
	}
	got := string(cacheSrc)
	if !strings.Contains(got, "const fingerprintProductName =") {
		t.Errorf("cache_gen.go does not cache product_names, the opted-in view:\n%s", got)
	}
	if strings.Contains(got, "fingerprintProduct =") {
		t.Error("cache_gen.go caches products, which did not opt in")
	}
	clientSrc, err := os.ReadFile(filepath.Join(root, "models", "client_gen.go")) //nolint:gosec // test-written temp module
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(clientSrc), "func WithCache(") {
		t.Error("client_gen.go has no WithCache option although a view is cached")
	}

	vetGeneratedModels(t, root, repo)
}

// vetGeneratedModels type-checks root/models as a temp module that requires
// the runtime at repo. It is skipped in short mode and without a go toolchain.
func vetGeneratedModels(t *testing.T, root, repo string) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the generated package; skipped in short mode")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	goSum, err := os.ReadFile(filepath.Join(repo, "go.sum")) //nolint:gosec // the repo's own go.sum
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"go.mod": "module example.com/app\n" + goDirective() + "\nrequire github.com/teandresmith/sqlgen v0.0.0\n\nreplace github.com/teandresmith/sqlgen => " + filepath.ToSlash(repo) + "\n",
		"go.sum": string(goSum),
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	cmd := exec.Command(goBin, "vet", "./models/...") //nolint:gosec // the go binary from PATH, run on a test-written temp module
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("go vet on the generated package: %v\n%s", err, out)
	}
}
