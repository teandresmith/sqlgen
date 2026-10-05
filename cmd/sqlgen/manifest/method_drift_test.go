package manifest_test

import (
	"encoding/json"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
)

// The three artifacts below are regenerated together by
// `make update-golden-e2e`, so a template that gains or renames a client
// method moves the Go file and this test catches the manifest that did not
// follow.
const (
	generatedModelsPath   = "../testdata/examples/postgres/models/models_gen.go"
	generatedViewsPath    = "../testdata/examples/postgres/models/views_gen.go"
	generatedManifestPath = "../testdata/examples/postgres/models/manifest/manifest_gen.json"
)

// TestMethodNamesMatchGeneratedClients is the drift guard for methods[]
// names against the generated clients.
//
// methods[] is what an agent reads through MCP §31 to write Go against the
// generated package, and what sqlgen_show_sql keys its lookup on. It carried a
// vocabulary taken from a documentation example instead of from the templates:
// FindByID / FindBy<Col> / List / Walk / Delete / Upsert<Target>, none of which
// is generated anywhere, while Increment and thirteen other real methods went
// unmentioned. No test could see it because nothing compared the manifest to
// the thing it describes.
//
// This does the comparison — set equality, both directions, per entity.
func TestMethodNamesMatchGeneratedClients(t *testing.T) {
	declared := clientInterfaceMethods(t, generatedModelsPath, generatedViewsPath)
	if len(declared) == 0 {
		t.Fatalf("parsed no <Entity>Client interfaces out of %s — the fixture moved or the client template changed shape", generatedModelsPath)
	}

	doc := loadGeneratedManifest(t)
	if len(doc.Entities) == 0 {
		t.Fatal("golden manifest carries no entities")
	}

	for _, e := range doc.Entities {
		iface := e.Name + "Client"
		generated, ok := declared[iface]
		if !ok {
			t.Errorf("entity %q: no %s interface in the generated package; interfaces = %v",
				e.Name, iface, slices.Sorted(maps.Keys(declared)))
			continue
		}

		advertised := make([]string, 0, len(e.Methods.Query)+len(e.Methods.Mutation))
		for _, group := range [][]manifest.Method{e.Methods.Query, e.Methods.Mutation} {
			for _, m := range group {
				advertised = append(advertised, m.Name)
			}
		}
		slices.Sort(advertised)

		for _, name := range advertised {
			if !slices.Contains(generated, name) {
				t.Errorf("entity %q advertises method %q, but %s declares no such method; declared = %v",
					e.Name, name, iface, generated)
			}
		}
		for _, name := range generated {
			if !slices.Contains(advertised, name) {
				t.Errorf("%s declares %q but entity %q omits it from methods[]; advertised = %v",
					iface, name, e.Name, advertised)
			}
		}
	}
}

// TestMethodParamTypesResolve checks the other half of the same fidelity rule:
// a method entry whose name is right but whose params name types the package
// does not declare is just as unusable. Every non-builtin, non-generic type
// named by a param or return must be a type the generated package declares —
// the defect shipped CounterInput / CounterUpdate / CounterSort / Page /
// *CounterList, none of which exists.
func TestMethodParamTypesResolve(t *testing.T) {
	declaredTypes := typeNamesDeclaredIn(t, generatedModelsPath, generatedViewsPath)
	doc := loadGeneratedManifest(t)

	var checked int
	for _, e := range doc.Entities {
		for _, group := range [][]manifest.Method{e.Methods.Query, e.Methods.Mutation} {
			for _, m := range group {
				refs := append(typeRefs(m.Returns), paramTypeRefs(m.Params)...)
				for _, ref := range refs {
					checked++
					if !declaredTypes[ref] {
						t.Errorf("entity %q method %q names type %q, which the generated package does not declare",
							e.Name, m.Name, ref)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no method types were checked — the golden manifest stopped carrying params")
	}
}

func paramTypeRefs(params []manifest.MethodParam) []string {
	out := make([]string, 0, len(params))
	for _, p := range params {
		out = append(out, typeRefs(p.Type)...)
	}
	return out
}

// typeRefs pulls the package-declared identifiers out of a Go type expression,
// dropping pointers, slices, generic brackets and anything qualified by an
// import path (uuid.UUID, iter.Seq2, time.Time) or built in (string, int64,
// bool, error). What remains is exactly the set this package must declare.
func typeRefs(expr string) []string {
	if expr == "" {
		return nil
	}
	builtin := map[string]bool{
		"bool": true, "string": true, "int": true, "int8": true, "int16": true,
		"int32": true, "int64": true, "uint": true, "uint8": true, "uint16": true,
		"uint32": true, "uint64": true, "float32": true, "float64": true,
		"byte": true, "rune": true, "any": true, "error": true,
	}
	cleaned := strings.NewReplacer("*", " ", "[", " ", "]", " ", ",", " ", "(", " ", ")", " ").Replace(expr)
	fields := strings.Fields(cleaned)
	out := make([]string, 0, len(fields))
	for _, tok := range fields {
		if tok == "" || builtin[tok] || strings.Contains(tok, ".") {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// clientInterfaceMethods returns, per `type <Entity>Client interface` found in
// the given generated files, the exported method names it declares.
func clientInterfaceMethods(t *testing.T, paths ...string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, path := range paths {
		file, err := goparser.ParseFile(token.NewFileSet(), path, nil, goparser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !strings.HasSuffix(ts.Name.Name, "Client") {
					continue
				}
				iface, ok := ts.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}
				var methods []string
				for _, field := range iface.Methods.List {
					for _, name := range field.Names {
						if name.IsExported() {
							methods = append(methods, name.Name)
						}
					}
				}
				slices.Sort(methods)
				out[ts.Name.Name] = methods
			}
		}
	}
	return out
}

// typeNamesDeclaredIn returns the set of type names the generated package
// declares, so a manifest type reference can be checked against it. It reads
// the two entity files plus the shared-type files their signatures reference
// (CallOptions, PaginateInput, Connection, the enums).
func typeNamesDeclaredIn(t *testing.T, paths ...string) map[string]bool {
	t.Helper()
	dir := filepath.Dir(paths[0])
	files := slices.Clone(paths)
	for _, shared := range []string{"shared_types_gen.go", "pagination_gen.go", "connection_gen.go", "types_gen.go", "enums_gen.go"} {
		path := filepath.Join(dir, shared)
		if _, err := os.Stat(path); err == nil {
			files = append(files, path)
		}
	}

	out := map[string]bool{}
	for _, path := range files {
		file, err := goparser.ParseFile(token.NewFileSet(), path, nil, goparser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					out[ts.Name.Name] = true
				}
			}
		}
	}
	return out
}

func loadGeneratedManifest(t *testing.T) *manifest.Document {
	t.Helper()
	raw, err := os.ReadFile(generatedManifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", generatedManifestPath, err)
	}
	doc := new(manifest.Document)
	if err := json.Unmarshal(raw, doc); err != nil {
		t.Fatalf("decode %s: %v", generatedManifestPath, err)
	}
	return doc
}
