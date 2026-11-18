package gen_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	sqlparser "github.com/teandresmith/sqlgen/parser"
)

// gnwParse parses ddl for dialect and runs relationship detection over it,
// as the CLI does before generating.
func gnwParse(t *testing.T, dialect config.Dialect, ddl string) *sqlparser.Schema {
	t.Helper()
	schema := parseDDLForTest(t, dialect, ddl)
	sqlparser.DetectRelationships(schema)
	return schema
}

// gnwGenerate runs the whole generate pipeline over ddl for dialect, with
// extra appended to the config as top-level YAML, and returns the output
// directory.
func gnwGenerate(t *testing.T, dialect config.Dialect, ddl, extra string) string {
	t.Helper()
	outDir := t.TempDir()
	cfg := loadGenerateConfig(t, outDir, dialect, extra)
	if _, err := gen.Generate(gnwParse(t, dialect, ddl), cfg, "test"); err != nil {
		t.Fatalf("Generate(%s) error: %v", dialect, err)
	}
	return outDir
}

// gnwStructFields parses src and returns the named struct's fields as
// "Name Type" strings, in declaration order.
func gnwStructFields(t *testing.T, src, typeName string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "src.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing generated source: %v", err)
	}
	var fields []string
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != typeName {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return false
		}
		found = true
		for _, fld := range st.Fields.List {
			for _, name := range fld.Names {
				fields = append(fields, name.Name+" "+gnwExpr(fld.Type))
			}
		}
		return false
	})
	if !found {
		t.Fatalf("generated source declares no struct %s", typeName)
	}
	return fields
}

// gnwExpr renders a type expression as Go source, enough for the field
// and parameter types the generated-code checks compare.
func gnwExpr(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return gnwExpr(x.X) + "." + x.Sel.Name
	case *ast.StarExpr:
		return "*" + gnwExpr(x.X)
	case *ast.ArrayType:
		return "[]" + gnwExpr(x.Elt)
	case *ast.IndexExpr:
		return gnwExpr(x.X) + "[" + gnwExpr(x.Index) + "]"
	case *ast.IndexListExpr:
		s := gnwExpr(x.X) + "["
		for i, idx := range x.Indices {
			if i > 0 {
				s += ", "
			}
			s += gnwExpr(idx)
		}
		return s + "]"
	}
	return "?"
}

// gnwProbePackage is one generated package of a probe module: the dialect
// and DDL it is generated from, and further top-level config YAML.
type gnwProbePackage struct {
	dir     string
	dialect config.Dialect
	ddl     string
	extra   string
}

// gnwRecordingQuerierSrc is a database.Querier for a generated package's own
// tests: it records every statement it is handed and fails it, so a probe can
// see exactly which statements a method issued, and with which arguments,
// without a database.
const gnwRecordingQuerierSrc = `package db

import (
	"context"
	"errors"
	"sync"

	"github.com/teandresmith/sqlgen/database"
)

var errProbe = errors.New("probe: statement recorded")

type probeStatement struct {
	sql  string
	args []any
}

type probeQuerier struct {
	mu    sync.Mutex
	stmts []probeStatement
}

func (q *probeQuerier) record(sql string, args []any) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.stmts = append(q.stmts, probeStatement{sql: sql, args: args})
}

func (q *probeQuerier) statements() []probeStatement {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]probeStatement(nil), q.stmts...)
}

func (q *probeQuerier) Exec(_ context.Context, sql string, args ...any) (database.Result, error) {
	q.record(sql, args)
	return nil, errProbe
}

func (q *probeQuerier) Query(_ context.Context, sql string, args ...any) (database.Rows, error) {
	q.record(sql, args)
	return nil, errProbe
}

func (q *probeQuerier) QueryRow(_ context.Context, sql string, args ...any) database.Row {
	q.record(sql, args)
	return probeRow{}
}

func (q *probeQuerier) Begin(context.Context, string, ...database.TxOptions) (*database.Tx, error) {
	return nil, errProbe
}

type probeRow struct{}

func (probeRow) Scan(...any) error { return errProbe }
`

// gnwRunGeneratedProbe generates each package into a fresh module that
// resolves the sqlgen runtime to this repository, adds the recording querier
// and probeSrc (a _test.go file in package db) to every package, and runs
// their tests. It fails t with the go test output when any probe fails.
func gnwRunGeneratedProbe(t *testing.T, pkgs []gnwProbePackage, probeSrc string) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs the generated packages; skipped in short mode")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	goSum, err := os.ReadFile(filepath.Join(repo, "go.sum")) //nolint:gosec // the repo's own go.sum
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	module := map[string]string{
		"go.mod": "module example.com/app\n" + goDirective() + "\nrequire github.com/teandresmith/sqlgen v0.0.0\n\nreplace github.com/teandresmith/sqlgen => " + filepath.ToSlash(repo) + "\n",
		"go.sum": string(goSum),
	}
	for name, body := range module {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	for _, p := range pkgs {
		outDir := filepath.Join(root, p.dir)
		cfg := loadGenerateConfig(t, outDir, p.dialect, p.extra)
		if _, err := gen.Generate(gnwParse(t, p.dialect, p.ddl), cfg, "test"); err != nil {
			t.Fatalf("Generate(%s) error: %v", p.dir, err)
		}
		for name, body := range map[string]string{
			"probe_querier_test.go": gnwRecordingQuerierSrc,
			"probe_test.go":         probeSrc,
		} {
			if err := os.WriteFile(filepath.Join(outDir, name), []byte(body), 0o600); err != nil {
				t.Fatalf("writing %s/%s: %v", p.dir, name, err)
			}
		}
	}

	cmd := exec.CommandContext(t.Context(), goBin, "test", "-count=1", "./...") //nolint:gosec // the go binary from PATH, run on a test-written temp module
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go test on the generated packages: %v\n%s", err, out)
	}
}
