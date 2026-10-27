package gen_test

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// A relationship load is part of the GetMany that triggered it, so the hook
// toggles the caller resolved for the parent reach each child read: a caller
// that skips hooks, the cache or events for a read skips them for its
// relationship loads too. Two fields stay behind: FieldOptions, where each
// child takes its own branch of the parent's selection, and LockMode — the
// child and junction reads take no row lock, whatever the parent's.
//
// The schema carries one edge of each kind. O2O is loaded by a JOIN inside the
// parent's own statement, so it issues no child read; O2M and M2M each issue a
// child GetMany through the target's client, and those closures are what this
// test inspects. The generated package is also compiled, so a field copied
// from a name that does not exist fails here rather than in a consumer build.
const rcoSchemaDDL = `
CREATE TABLE users (id uuid PRIMARY KEY, email text NOT NULL);
CREATE TABLE profiles (
	id uuid PRIMARY KEY,
	user_id uuid NOT NULL UNIQUE REFERENCES users(id),
	bio text
);
CREATE TABLE posts (
	id uuid PRIMARY KEY,
	user_id uuid NOT NULL REFERENCES users(id),
	title text NOT NULL
);
CREATE TABLE tags (id uuid PRIMARY KEY, name text NOT NULL);
CREATE TABLE post_tags (
	post_id uuid NOT NULL REFERENCES posts(id),
	tag_id uuid NOT NULL REFERENCES tags(id),
	PRIMARY KEY (post_id, tag_id)
);`

const rcoConfig = `version: v1
input:
  dialect: postgres
  paths:
    - ./schema.sql
output:
  driver: pgx
  dir: ./models
  package: models
  layout: file_per_table
`

// rcoPropagated is every assignment a relationship child closure must make
// from the parent's resolved options, keyed by the child field.
// The hook toggles come from the caller record rather than the loader's own
// options, which an internal hop (Get's inner GetMany) has set SkipHooks on.
var rcoPropagated = map[string]string{
	"SkipCache":  "options.caller.skipCache",
	"SkipEvents": "options.caller.skipEvents",
	"SkipHooks":  "options.caller.skipHooks",
}

// rcoSelectOptionsKeys returns the keys of every sql.SelectOptions literal
// inside fn — the M2M junction read is built from one.
func rcoSelectOptionsKeys(fn *ast.FuncDecl) [][]string {
	var out [][]string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || types.ExprString(lit.Type) != "sql.SelectOptions" {
			return true
		}
		var keys []string
		for _, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				keys = append(keys, types.ExprString(kv.Key))
			}
		}
		out = append(out, keys)
		return true
	})
	return out
}

// rcoGenerate writes the schema and config into a fresh module root, runs
// Generate, and returns the root.
func rcoGenerate(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{
		"schema.sql": rcoSchemaDDL,
		"sqlgen.yml": rcoConfig,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	t.Chdir(root)
	schema := parseDDLForTest(t, config.DialectPostgres, rcoSchemaDDL)
	detectRelationships(schema)
	cfg, err := config.LoadConfig(filepath.Join(root, "sqlgen.yml"))
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	if _, err := gen.Generate(schema, cfg, "test"); err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	return root
}

// rcoParse parses one generated file.
func rcoParse(t *testing.T, root, name string) *ast.File {
	t.Helper()
	path := filepath.Join(root, "models", name)
	f, err := goparser.ParseFile(token.NewFileSet(), path, nil, goparser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}
	return f
}

// rcoMethod returns the method named name, or nil.
func rcoMethod(f *ast.File, name string) *ast.FuncDecl {
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv != nil && fd.Name.Name == name {
			return fd
		}
	}
	return nil
}

// rcoChildClosures returns, for every CallOptions closure inside fn, the
// option type it configures mapped to the co.<Field> = <expr> assignments it
// makes.
func rcoChildClosures(fn *ast.FuncDecl) map[string]map[string]string {
	out := map[string]map[string]string{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok || len(lit.Type.Params.List) != 1 {
			return true
		}
		param := lit.Type.Params.List[0]
		typ := types.ExprString(param.Type)
		if len(param.Names) != 1 || !strings.HasPrefix(typ, "*CallOptions[") {
			return true
		}
		co := param.Names[0].Name
		assigns := map[string]string{}
		for _, stmt := range lit.Body.List {
			as, ok := stmt.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != len(as.Rhs) {
				continue
			}
			for i, lhs := range as.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == co {
					assigns[sel.Sel.Name] = types.ExprString(as.Rhs[i])
				}
			}
		}
		out[typ] = assigns
		return true
	})
	return out
}

func TestRelationshipCallOptions(t *testing.T) {
	// The runtime module the generated package imports, resolved before
	// rcoGenerate moves the working directory into the temp project.
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	root := rcoGenerate(t)

	t.Run("each O2M and M2M child read inherits the parent's per-operation options", func(t *testing.T) {
		tests := []struct {
			file       string
			parentOpts string
			children   []string
		}{
			{file: "user_gen.go", parentOpts: "CallOptions[UserFieldOptions]", children: []string{"*CallOptions[PostFieldOptions]"}},
			{file: "post_gen.go", parentOpts: "CallOptions[PostFieldOptions]", children: []string{"*CallOptions[TagFieldOptions]"}},
			{file: "tag_gen.go", parentOpts: "CallOptions[TagFieldOptions]", children: []string{"*CallOptions[PostFieldOptions]"}},
		}
		for _, tt := range tests {
			f := rcoParse(t, root, tt.file)
			loader := rcoMethod(f, "loadRelationships")
			if loader == nil {
				t.Errorf("%s: no loadRelationships method", tt.file)
				continue
			}

			// The loader's options parameter is the parent's resolved CallOptions.
			var paramType string
			for _, p := range loader.Type.Params.List {
				for _, n := range p.Names {
					if n.Name == "options" {
						paramType = types.ExprString(p.Type)
					}
				}
			}
			if paramType != tt.parentOpts {
				t.Errorf("%s: loadRelationships options parameter type = %q, want %q", tt.file, paramType, tt.parentOpts)
			}

			closures := rcoChildClosures(loader)
			for _, child := range tt.children {
				assigns, ok := closures[child]
				if !ok {
					t.Errorf("%s: loadRelationships has no %s closure; closures: %v", tt.file, child, closures)
					continue
				}
				for field, want := range rcoPropagated {
					if got := assigns[field]; got != want {
						t.Errorf("%s: %s closure sets co.%s = %q, want %q", tt.file, child, field, got, want)
					}
				}
				// A relationship load takes no row lock, whatever the parent's.
				if got, ok := assigns["LockMode"]; ok {
					t.Errorf("%s: %s closure sets co.LockMode = %q; relationship loads must not inherit a lock mode", tt.file, child, got)
				}
			}
		}
	})

	t.Run("the M2M junction read takes no lock clause", func(t *testing.T) {
		for _, file := range []string{"post_gen.go", "tag_gen.go"} {
			loader := rcoMethod(rcoParse(t, root, file), "loadRelationships")
			if loader == nil {
				t.Fatalf("%s: no loadRelationships method", file)
			}
			selects := rcoSelectOptionsKeys(loader)
			if len(selects) == 0 {
				t.Errorf("%s: loadRelationships builds no junction read from sql.SelectOptions", file)
			}
			for _, keys := range selects {
				if slices.Contains(keys, "LockMode") {
					t.Errorf("%s: junction read sets LockMode (keys %v); relationship loads must not take a row lock", file, keys)
				}
			}
		}
	})

	t.Run("GetMany hands its resolved options to the loader", func(t *testing.T) {
		for _, file := range []string{"user_gen.go", "post_gen.go", "tag_gen.go"} {
			getMany := rcoMethod(rcoParse(t, root, file), "GetMany")
			if getMany == nil {
				t.Fatalf("%s: no GetMany method", file)
			}
			var args []string
			ast.Inspect(getMany.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "loadRelationships" {
					for _, a := range call.Args {
						args = append(args, types.ExprString(a))
					}
				}
				return true
			})
			if len(args) == 0 {
				t.Errorf("%s: GetMany never calls loadRelationships", file)
				continue
			}
			if got := args[len(args)-1]; got != "options" {
				t.Errorf("%s: loadRelationships receives %q, want the resolved options", file, got)
			}
		}
	})

	t.Run("an O2O edge issues no child read of its own", func(t *testing.T) {
		// profiles owns the O2O key to users. Its edge is JOINed into the
		// profiles statement, so no method issues a users read with options of
		// its own; one that did would need the same propagation as above.
		profiles := rcoParse(t, root, "profile_gen.go")
		if rcoMethod(profiles, "resolveO2OJoins") == nil {
			t.Fatalf("profile_gen.go: no resolveO2OJoins; the O2O users edge is no longer JOIN-loaded")
		}
		for _, decl := range profiles.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if _, ok := rcoChildClosures(fd)["*CallOptions[UserFieldOptions]"]; ok {
				t.Errorf("profile_gen.go: %s issues a users read for the O2O edge; it must inherit the parent's options like the O2M and M2M reads", fd.Name.Name)
			}
		}
	})

	t.Run("Get keeps the caller record when a hook replaces its options", func(t *testing.T) {
		// A hook in another package cannot set the unexported caller record,
		// so Get restores it before its internal GetMany forces SkipHooks;
		// otherwise every relationship load under that Get would skip hooks.
		for _, file := range []string{"user_gen.go", "post_gen.go", "tag_gen.go"} {
			get := rcoMethod(rcoParse(t, root, file), "Get")
			if get == nil {
				t.Fatalf("%s: no Get method", file)
			}
			var restored bool
			ast.Inspect(get.Body, func(n ast.Node) bool {
				ifs, ok := n.(*ast.IfStmt)
				if !ok || types.ExprString(ifs.Cond) != "!qOpts.caller.set" {
					return true
				}
				for _, stmt := range ifs.Body.List {
					as, ok := stmt.(*ast.AssignStmt)
					if ok && len(as.Lhs) == 1 && types.ExprString(as.Lhs[0]) == "qOpts.caller" && types.ExprString(as.Rhs[0]) == "options.caller" {
						restored = true
					}
				}
				return true
			})
			if !restored {
				t.Errorf("%s: Get does not restore options.caller onto a hook-replaced CallOptions", file)
			}
		}
	})

	t.Run("the generated package compiles", func(t *testing.T) {
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
	})
}
