package reqcheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Test homes a registry row may name (CONFORMANCE.md §2).
const (
	HomeConformance = "conformance"
	HomeUnit        = "unit"
	HomeIntegration = "integration"
	HomeGolden      = "golden"
	HomeExample     = "example"
)

// Directories the homes resolve under, relative to the repository root.
const (
	ConformanceSuiteDir = "cmd/sqlgen/testdata/conformance/suite"
	FeaturesSuiteDir    = "cmd/sqlgen/testdata/conformance/features_suite"
	ExamplesDir         = "cmd/sqlgen/testdata/examples"
)

// Suite is a conformance suite library. A variant suite wraps its tree in one subtest named
// after it, so its citations start with that name (CONFORMANCE.md §4.2).
type Suite struct {
	Name string // the root segment; empty for the core suite
	Dir  string
}

// Suites lists the conformance suites, the core suite first.
var Suites = []Suite{
	{Dir: ConformanceSuiteDir},
	{Name: "Features", Dir: FeaturesSuiteDir},
}

// suiteFor returns the suite a conformance test path belongs to: the variant suite its first
// segment names, otherwise the core suite.
func suiteFor(path []string) Suite {
	for _, s := range Suites[1:] {
		if len(path) > 0 && path[0] == s.Name {
			return s
		}
	}
	return Suites[0]
}

// conformanceRoot is the suite function whose subtests are the conformance test paths.
const conformanceRoot = "Run"

// TestRef is a parsed Tests-cell entry.
type TestRef struct {
	Home string
	Dir  string // the package directory holding the test, relative to the repository root
	// Path is the test name split on "/", segments kept exactly as written, empty ones included:
	// `go test` names a subtest "a/" as a and an empty child. Empty for a golden home.
	Path []string
	Raw  string
}

// ParseTestRef parses `conformance: Method/subtest` (`Features/…` for that suite),
// `unit: <dir>: TestX/sub`, `integration: <dir>: TestX`, `example: <example>: TestX` or
// `golden: <example>`.
func ParseTestRef(s string) (TestRef, error) {
	home, rest, ok := strings.Cut(s, ":")
	if !ok {
		return TestRef{}, fmt.Errorf("test %q has no `<home>:` prefix", s)
	}
	home, rest = strings.TrimSpace(home), strings.TrimSpace(rest)
	ref := TestRef{Home: home, Raw: s}
	switch home {
	case HomeConformance:
		ref.Path = splitPath(rest)
		ref.Dir = suiteFor(ref.Path).Dir
	case HomeGolden:
		if rest == "" || strings.Contains(rest, ":") {
			return TestRef{}, fmt.Errorf("test %q: a golden home names one example, `golden: <example>`", s)
		}
		ref.Dir = ExamplesDir + "/" + rest
	case HomeUnit, HomeIntegration, HomeExample:
		dir, name, ok := strings.Cut(rest, ":")
		if !ok {
			return TestRef{}, fmt.Errorf("test %q: %s needs `%s: <dir>: <TestName/…>`", s, home, home)
		}
		dir = strings.Trim(strings.TrimSpace(dir), "/")
		if home == HomeExample {
			dir = ExamplesDir + "/" + dir + "/tests"
		}
		ref.Dir, ref.Path = dir, splitPath(strings.TrimSpace(name))
	default:
		return TestRef{}, fmt.Errorf("test %q: home %q is not one of conformance, unit, integration, golden, example", s, home)
	}
	if home != HomeGolden && len(ref.Path) == 0 {
		return TestRef{}, fmt.Errorf("test %q names no test", s)
	}
	return ref, nil
}

// splitPath splits a test name the way the test tree does, so a reference resolves to exactly
// the test `go test` reports under that name.
func splitPath(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "/")
}

// testNode is one test or subtest.
type testNode struct {
	name     string
	children []*testNode
	// computed is set when some subtest names under this node could not be read statically.
	computed bool
	// run is set on a test go test reports. A subtest name containing "/" also makes a node
	// for each segment before the last, which go test never reports on its own.
	run bool
}

func (n *testNode) child(name string) *testNode {
	for _, c := range n.children {
		if c.name == name {
			return c
		}
	}
	c := &testNode{name: name}
	n.children = append(n.children, c)
	return c
}

// lookup finds a child by its literal name or by the name `go test` reports, spaces as underscores.
func (n *testNode) lookup(seg string) *testNode {
	for _, c := range n.children {
		if c.name == seg || goTestName(c.name) == seg {
			return c
		}
	}
	return nil
}

// testTree is the static subtest tree of one package directory, keyed by top-level function.
type testTree map[string]*testNode

// loadTestTree parses the Go files in dir: _test.go files, or every .go file when allFiles is
// set (the conformance suite is a library). Every package-level function with a *testing.T
// parameter becomes a top-level node.
func loadTestTree(dir string, allFiles bool) (testTree, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading test directory: %w", err)
	}
	fset := token.NewFileSet()
	funcs := map[string]*ast.FuncDecl{}
	vars := map[string]ast.Expr{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || (!allFiles && !strings.HasSuffix(name, "_test.go")) {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", name, err)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Body != nil {
					funcs[d.Name.Name] = d
				}
			case *ast.GenDecl:
				collectBindings(d, vars)
			}
		}
	}

	tree := testTree{}
	for name, fn := range funcs {
		if !takesTestingT(fn.Type) {
			continue
		}
		root := &testNode{name: name, run: true}
		w := walker{funcs: funcs, pkgVars: vars, stack: []string{name}}
		w.walk(fn.Body, root, nil, nil)
		tree[name] = root
	}
	return tree, nil
}

func takesTestingT(ft *ast.FuncType) bool {
	if ft.Params == nil {
		return false
	}
	for _, p := range ft.Params.List {
		if isTestingT(p.Type) {
			return true
		}
	}
	return false
}

func isTestingT(e ast.Expr) bool {
	star, ok := e.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "testing" && sel.Sel.Name == "T"
}

// walker records the t.Run calls under one function body.
type walker struct {
	funcs   map[string]*ast.FuncDecl
	pkgVars map[string]ast.Expr
	stack   []string // functions being walked, to stop helper recursion
}

// walk records every t.Run under body as a child of parent. It follows calls to package-level
// helpers that take a *testing.T, so a subtest declared in a helper belongs to its caller. A
// subtest body sees the bindings of the function enclosing it: outer is nil for a function body.
func (w walker) walk(body *ast.BlockStmt, parent *testNode, outerVars map[string]ast.Expr, outerRanges map[string]rangeBinding) {
	vars := maps.Clone(outerVars)
	if vars == nil {
		vars = map[string]ast.Expr{}
	}
	ranges := maps.Clone(outerRanges)
	if ranges == nil {
		ranges = map[string]rangeBinding{}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && i < len(n.Rhs) {
					vars[id.Name] = n.Rhs[i]
				}
			}
		case *ast.DeclStmt:
			if gd, ok := n.Decl.(*ast.GenDecl); ok {
				collectBindings(gd, vars)
			}
		case *ast.RangeStmt:
			w.bindRange(n, vars, ranges)
		case *ast.CallExpr:
			return w.call(n, parent, vars, ranges)
		}
		return true
	})
}

// bindRange records the key and value variables of a range over a composite literal.
func (w walker) bindRange(n *ast.RangeStmt, vars map[string]ast.Expr, ranges map[string]rangeBinding) {
	lit, ok := w.resolve(n.X, vars).(*ast.CompositeLit)
	if !ok {
		return
	}
	if id, ok := n.Key.(*ast.Ident); ok {
		ranges[id.Name] = rangeBinding{lit: lit, key: true}
	}
	if id, ok := n.Value.(*ast.Ident); ok {
		ranges[id.Name] = rangeBinding{lit: lit}
	}
}

// call records a t.Run subtest or follows a helper call. It reports whether ast.Inspect should
// descend into the call: a subtest's body is walked here, under its own node.
func (w walker) call(n *ast.CallExpr, parent *testNode, vars map[string]ast.Expr, ranges map[string]rangeBinding) bool {
	if fn, ok := subtestCall(n); ok {
		names, ok := w.names(n.Args[0], ranges)
		if !ok {
			parent.computed = true
			return false
		}
		for _, name := range names {
			// go test splits a name on "/" into nested subtests, and so do references.
			node := parent
			for seg := range strings.SplitSeq(name, "/") {
				node = node.child(seg)
			}
			node.run = true
			w.walk(fn.Body, node, vars, ranges)
		}
		return false
	}
	if id, ok := n.Fun.(*ast.Ident); ok {
		helper, found := w.funcs[id.Name]
		if found && takesTestingT(helper.Type) && !slices.Contains(w.stack, id.Name) {
			inner := w
			inner.stack = append(slices.Clone(w.stack), id.Name)
			inner.walk(helper.Body, parent, nil, nil)
		}
	}
	return true
}

// subtestCall matches `x.Run(name, func(t *testing.T) { … })`.
func subtestCall(call *ast.CallExpr) (*ast.FuncLit, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Run" || len(call.Args) != 2 {
		return nil, false
	}
	fn, ok := call.Args[1].(*ast.FuncLit)
	if !ok || fn.Type.Params == nil || len(fn.Type.Params.List) != 1 || !isTestingT(fn.Type.Params.List[0].Type) {
		return nil, false
	}
	return fn, true
}

type rangeBinding struct {
	lit *ast.CompositeLit
	key bool // the range key variable, not the value
}

func (w walker) resolve(e ast.Expr, vars map[string]ast.Expr) ast.Expr {
	if id, ok := e.(*ast.Ident); ok {
		if v, ok := vars[id.Name]; ok {
			return v
		}
		if v, ok := w.pkgVars[id.Name]; ok {
			return v
		}
	}
	return e
}

// names reads the subtest names an expression takes: a string literal, the keys of a ranged
// map literal, or a string field of the elements of a ranged table literal.
func (w walker) names(e ast.Expr, ranges map[string]rangeBinding) ([]string, bool) {
	switch e := e.(type) {
	case *ast.BasicLit:
		s, ok := stringLit(e)
		return []string{s}, ok
	case *ast.Ident:
		b, ok := ranges[e.Name]
		if !ok || !b.key {
			return nil, false
		}
		var out []string
		for _, elt := range b.lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				return nil, false
			}
			s, ok := stringLit(kv.Key)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	case *ast.SelectorExpr:
		id, ok := e.X.(*ast.Ident)
		if !ok {
			return nil, false
		}
		b, ok := ranges[id.Name]
		if !ok || b.key {
			return nil, false
		}
		return fieldValues(b.lit, e.Sel.Name)
	}
	return nil, false
}

// fieldValues returns field's string literal in every element of a table literal, a slice or
// array of structs, or a map whose values are structs.
func fieldValues(table *ast.CompositeLit, field string) ([]string, bool) {
	index := -1
	if st := elemStruct(table.Type); st != nil {
		i := 0
		for _, f := range st.Fields.List {
			if len(f.Names) == 0 { // an embedded field takes one position
				i++
				continue
			}
			for _, n := range f.Names {
				if n.Name == field {
					index = i
				}
				i++
			}
		}
	}
	var out []string
	for _, elt := range table.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			elt = kv.Value
		}
		lit, ok := elt.(*ast.CompositeLit)
		if !ok {
			return nil, false
		}
		var val ast.Expr
		for i, f := range lit.Elts {
			if kv, ok := f.(*ast.KeyValueExpr); ok {
				if k, ok := kv.Key.(*ast.Ident); ok && k.Name == field {
					val = kv.Value
				}
			} else if i == index {
				val = f
			}
		}
		s, ok := stringLit(val)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func elemStruct(t ast.Expr) *ast.StructType {
	switch t := t.(type) {
	case *ast.ArrayType:
		st, _ := t.Elt.(*ast.StructType)
		return st
	case *ast.MapType:
		st, _ := t.Value.(*ast.StructType)
		return st
	}
	return nil
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

func collectBindings(gd *ast.GenDecl, into map[string]ast.Expr) {
	if gd.Tok != token.VAR {
		return
	}
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, n := range vs.Names {
			if i < len(vs.Values) {
				into[n.Name] = vs.Values[i]
			}
		}
	}
}
