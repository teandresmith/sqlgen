package parser_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// pgwGoMod reads a go.mod file and returns its module path and the module
// paths of its require, replace and exclude directives, single-line and
// block forms alike.
func pgwGoMod(t *testing.T, path string) (module string, deps []string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	block := ""
	for line := range strings.Lines(string(data)) {
		line, _, _ = strings.Cut(line, "//")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if block != "" {
			if fields[0] == ")" {
				block = ""
				continue
			}
			deps = append(deps, fields[0])
			continue
		}
		switch fields[0] {
		case "module":
			if len(fields) > 1 {
				module = fields[1]
			}
		case "require", "replace", "exclude":
			if len(fields) > 1 && fields[1] == "(" {
				block = fields[0]
				continue
			}
			if len(fields) > 1 {
				deps = append(deps, fields[1])
			}
		}
	}
	if module == "" {
		t.Fatalf("%s declares no module", path)
	}
	return module, deps
}

// Each dialect parser is a package of the parser module, which has its own
// go.mod beside the runtime module's, and the runtime module does not
// require the parser module.
func TestDialectParsersLiveInParserModule(t *testing.T) {
	parserModule, _ := pgwGoMod(t, "go.mod")
	if parserModule != "github.com/teandresmith/sqlgen/parser" {
		t.Errorf("parser/go.mod module = %q, want github.com/teandresmith/sqlgen/parser", parserModule)
	}
	runtimeModule, runtimeDeps := pgwGoMod(t, filepath.Join("..", "go.mod"))
	if runtimeModule != "github.com/teandresmith/sqlgen" {
		t.Errorf("go.mod module = %q, want github.com/teandresmith/sqlgen", runtimeModule)
	}
	if slices.Contains(runtimeDeps, "github.com/teandresmith/sqlgen/parser") {
		t.Errorf("the runtime module requires the parser module: %v", runtimeDeps)
	}

	tests := []struct {
		name    string
		dir     string
		typ     reflect.Type
		wantPkg string
	}{
		{name: "postgres", dir: "postgres", typ: reflect.TypeFor[postgres.Parser](), wantPkg: "github.com/teandresmith/sqlgen/parser/postgres"},
		{name: "mysql", dir: "mysql", typ: reflect.TypeFor[mysql.Parser](), wantPkg: "github.com/teandresmith/sqlgen/parser/mysql"},
		{name: "sqlite", dir: "sqlite", typ: reflect.TypeFor[sqlite.Parser](), wantPkg: "github.com/teandresmith/sqlgen/parser/sqlite"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.typ.PkgPath(); got != tt.wantPkg {
				t.Errorf("%s parser package = %q, want %q", tt.name, got, tt.wantPkg)
			}
			// No go.mod of its own: the package belongs to parser/go.mod.
			if _, err := os.Stat(filepath.Join(tt.dir, "go.mod")); !os.IsNotExist(err) {
				t.Errorf("parser/%s/go.mod exists (stat error %v); the dialect parser must be in the parser module", tt.dir, err)
			}
		})
	}
}

// pg_query_go, which needs CGO, is required by the parser module and never
// named by the runtime module's go.mod, the one a consumer requires.
func TestRuntimeGoModOmitsPgQuery(t *testing.T) {
	const pgQuery = "github.com/pganalyze/pg_query_go/v6"
	_, parserDeps := pgwGoMod(t, "go.mod")
	if !slices.Contains(parserDeps, pgQuery) {
		t.Errorf("parser/go.mod does not require %s; deps %v", pgQuery, parserDeps)
	}
	path := filepath.Join("..", "go.mod")
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(data), "pg_query_go") {
		t.Errorf("the runtime module's go.mod names pg_query_go:\n%s", data)
	}
	_, runtimeDeps := pgwGoMod(t, path)
	for _, dep := range runtimeDeps {
		if strings.Contains(dep, "pg_query") {
			t.Errorf("the runtime module's go.mod requires %s", dep)
		}
	}
}
