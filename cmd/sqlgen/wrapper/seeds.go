package wrapper

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/imports"
	"gopkg.in/yaml.v3"
)

// modelLayout captures the relevant `model:` block fields parsed out of the
// consumer's gqlgen.yml. Defaults match gqlgen's own — `filename:
// graph/model/models_gen.go` and `package: model` — when keys are absent.
//
// The gqlgen-emitted input types (Create<T>Input, Update<T>Input,
// <T>Filter, <T>Sort, scalar comparator inputs) live in the package
// gqlgen writes to (driven by `model:`). The seed files plus the per-table
// input/filter/sort/comparator translators reference these types, so the
// rendered import block must include the consumer's gqlgen-model package
// under a sqlgen-chosen alias (`gqlmodel`) — collision-proof regardless of
// what the consumer names their own runtime models package.
type modelLayout struct {
	// Filename is gqlgen's `model.filename` value (used to derive the
	// directory under the consumer's module via `filepath.Dir`). Defaults to
	// "graph/model/models_gen.go" when absent.
	Filename string
	// Package is gqlgen's `model.package` value. Defaults to "model" when
	// absent. Sqlgen does not emit this name as the import alias — it uses
	// the controlled `gqlmodel` alias instead — but the field is parsed for
	// completeness and surfaces in tests.
	Package string
}

const (
	defaultModelFilename = "graph/model/models_gen.go"
	defaultModelPackage  = "model"
	// GqlgenModelImportAlias is the sqlgen-controlled Go import alias for
	// the consumer's gqlgen-emitted model package. Chosen so that no
	// reasonable cfg.Output.Package value can collide with it (see
	// `LoadGqlgenModelLayout`'s validator). Templates that reference
	// gqlgen-emitted input types prefix every reference with this alias.
	GqlgenModelImportAlias = "gqlmodel"
)

// parseModelLayout extracts the model block from the consumer's gqlgen.yml.
// Empty/missing keys fall through to gqlgen's documented defaults so the
// wrapper behaves the same as a freshly-scaffolded gqlgen.yml that omits
// explicit model fields.
func parseModelLayout(consumerYAML []byte) (modelLayout, error) {
	var doc struct {
		Model struct {
			Filename string `yaml:"filename"`
			Package  string `yaml:"package"`
		} `yaml:"model"`
	}
	if err := yaml.Unmarshal(consumerYAML, &doc); err != nil {
		return modelLayout{}, fmt.Errorf("unmarshalling model block: %w", err)
	}
	out := modelLayout{
		Filename: doc.Model.Filename,
		Package:  doc.Model.Package,
	}
	if out.Filename == "" {
		out.Filename = defaultModelFilename
	}
	if out.Package == "" {
		out.Package = defaultModelPackage
	}
	return out, nil
}

// GqlgenModelInfo reports the resolved gqlgen model package import path and
// import alias for use in the rendered seed / translator file preambles. Both
// fields are empty when the layout cannot be derived (missing gqlgen.yml,
// missing module path) — callers gate the corresponding import line on
// ImportPath != "".
type GqlgenModelInfo struct {
	// ImportPath is the full Go import path for the consumer's gqlgen-emitted
	// model package (e.g. "example.com/foo/graph/model"). Empty when the
	// gqlgen.yml is missing or the consumer module path cannot be resolved.
	ImportPath string
	// Alias is always GqlgenModelImportAlias when ImportPath is non-empty,
	// or empty when ImportPath is empty.
	Alias string
}

// LoadGqlgenModelInfo reads the gqlgen.yml at gqlgenConfigPath, parses the
// model block, and returns the resolved import path + alias for the consumer
// module modulePath. Returns a zero-value GqlgenModelInfo (no error) when
// either input is empty or the file is missing — the caller fail-softs
// without the gqlgen import, matching the existing ModelsImportPath path
// when go.mod is unresolvable.
func LoadGqlgenModelInfo(gqlgenConfigPath, modulePath string) (GqlgenModelInfo, error) {
	if gqlgenConfigPath == "" || modulePath == "" {
		return GqlgenModelInfo{}, nil
	}
	data, err := os.ReadFile(filepath.Clean(gqlgenConfigPath))
	if err != nil {
		if os.IsNotExist(err) {
			return GqlgenModelInfo{}, nil
		}
		return GqlgenModelInfo{}, fmt.Errorf("reading %s: %w", gqlgenConfigPath, err)
	}
	layout, err := parseModelLayout(data)
	if err != nil {
		return GqlgenModelInfo{}, err
	}
	dir := filepath.ToSlash(filepath.Dir(layout.Filename))
	dir = strings.TrimPrefix(dir, "./")
	dir = strings.TrimPrefix(dir, "/")
	if dir == "" || dir == "." {
		return GqlgenModelInfo{ImportPath: modulePath, Alias: GqlgenModelImportAlias}, nil
	}
	return GqlgenModelInfo{
		ImportPath: modulePath + "/" + dir,
		Alias:      GqlgenModelImportAlias,
	}, nil
}

// resolverLayout captures the relevant `resolver:` block fields parsed out
// of the consumer's gqlgen.yml. Defaults are filled in when keys are absent:
// `layout: follow-schema` and `filename_template: "{name}.resolvers.go"`.
// gqlgen v0.17.95 itself defaults an absent layout to single-file.
type resolverLayout struct {
	// Layout is gqlgen's `resolver.layout` value: "follow-schema" emits one
	// resolver file per .graphqls schema source; "single-file" emits one
	// combined file under `resolver.filename`.
	Layout string
	// Dir is the absolute or working-dir-relative path to the consumer's
	// resolver directory (gqlgen's `resolver.dir`). Empty falls back to
	// "graph" — gqlgen's documented default. Used under follow-schema
	// only: gqlgen ignores it under single-file (see Filename).
	Dir string
	// FilenameTemplate is gqlgen's `resolver.filename_template` (used under
	// follow-schema layout). Defaults to "{name}.resolvers.go" when absent.
	// `{name}` is the schema source's basename without extension.
	FilenameTemplate string
	// Filename is gqlgen's `resolver.filename` (used under single-file
	// layout). Defaults to "resolver.go" when absent. Like gqlgen, sqlgen
	// resolves it against the working directory, not against Dir
	// (singleFileResolverPath).
	Filename string
}

const (
	layoutFollowSchema    = "follow-schema"
	layoutSingleFile      = "single-file"
	defaultFilenameTpl    = "{name}.resolvers.go"
	defaultSingleFilename = "resolver.go"
	defaultResolverDir    = "graph"
)

// parseResolverLayout extracts the resolver block from the consumer's
// gqlgen.yml. Empty/missing keys fall through to gqlgen's documented
// defaults so the wrapper behaves the same as a freshly-scaffolded
// gqlgen.yml that omits explicit resolver fields.
func parseResolverLayout(consumerYAML []byte) (resolverLayout, error) {
	var doc struct {
		Resolver struct {
			Layout           string `yaml:"layout"`
			Dir              string `yaml:"dir"`
			FilenameTemplate string `yaml:"filename_template"`
			Filename         string `yaml:"filename"`
		} `yaml:"resolver"`
	}
	if err := yaml.Unmarshal(consumerYAML, &doc); err != nil {
		return resolverLayout{}, fmt.Errorf("unmarshalling resolver block: %w", err)
	}
	out := resolverLayout{
		Layout:           doc.Resolver.Layout,
		Dir:              doc.Resolver.Dir,
		FilenameTemplate: doc.Resolver.FilenameTemplate,
		Filename:         doc.Resolver.Filename,
	}
	if out.Layout == "" {
		out.Layout = layoutFollowSchema
	}
	if out.Dir == "" {
		out.Dir = defaultResolverDir
	}
	if out.FilenameTemplate == "" {
		out.FilenameTemplate = defaultFilenameTpl
	}
	if out.Filename == "" {
		out.Filename = defaultSingleFilename
	}
	return out, nil
}

// writeSeedFiles writes per-table delegation files into the resolver dir,
// using the layout extracted from the consumer's gqlgen.yml. Each file is
// written ONLY IF ABSENT: gqlgen owns the file after the first run and
// preserves method bodies verbatim across regenerations.
//
// Under follow-schema layout we emit one seed per table at
// `<resolver_dir>/<table>_gen.resolvers.go`, matching the schema-side
// `<table>_gen.graphqls` filename so gqlgen's resolvergen plugin pairs
// them. We ALSO pre-write a slim `resolver.go` containing ONLY the
// `type Resolver struct { Client; Q; M }` declaration (no `Query()` /
// `Mutation()` interface methods, no `queryResolver` / `mutationResolver`
// type defs). gqlgen detects the existing file and preserves it; the
// `Query()` / `Mutation()` methods plus `queryResolver` / `mutationResolver`
// type defs land in the schema-source resolver file (typically
// `shared_gen.resolvers.go`, since the shared schema declares the root
// `type Query` / `type Mutation`). Pre-populating the struct fields up
// front is REQUIRED because gqlgen runs a package-typecheck pass before
// returning — seeds that reference `r.Q.<Field>` / `r.M.<Field>` only
// compile when the Resolver struct already declares those fields.
// Emitting the methods or type defs here would collide with gqlgen's
// per-schema emission ("method redeclared"); emitting only the struct
// avoids that collision while still satisfying the typechecker.
//
// Under single-file layout we concatenate every table's body and write to
// `<resolver.filename>`, resolved against the working directory the way
// gqlgen resolves it (singleFileResolverPath). Under
// single-file gqlgen targets the same destination and preserves bodies
// verbatim, so the wrapper inlines the Resolver / Query / Mutation
// scaffold alongside seeds (no separate file to collide with).
func writeSeedFiles(opts GenOptions, layout resolverLayout) ([]string, error) {
	if len(opts.Seeds) == 0 {
		return nil, nil
	}
	if layout.Layout == layoutSingleFile {
		path := singleFileResolverPath(opts.WorkingDir, layout)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("creating resolver dir %s: %w", filepath.Dir(path), err)
		}
		return writeSingleFileSeed(path, opts)
	}

	dir := resolveDirAgainstWD(opts.WorkingDir, layout.Dir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("creating resolver dir %s: %w", dir, err)
	}
	scaffoldPath, err := writeResolverScaffold(dir, opts)
	if err != nil {
		return nil, err
	}
	seedPaths, err := writeFollowSchemaSeeds(dir, layout.FilenameTemplate, opts)
	if err != nil {
		return nil, err
	}
	written := seedPaths
	if scaffoldPath != "" {
		written = append([]string{scaffoldPath}, seedPaths...)
	}
	return written, nil
}

// singleFileResolverPath returns the single-file layout's resolver file:
// `resolver.filename` resolved against the working directory. gqlgen
// v0.17.95 makes that filename absolute from its own working directory,
// which the wrapper sets to opts.WorkingDir, and ignores `resolver.dir` in
// this layout (codegen/config/resolver.go: Check, Dir). Joining the two
// would write the seed somewhere gqlgen never reads, and fails outright for
// the usual `dir: graph` + `filename: graph/resolver.go`.
func singleFileResolverPath(wd string, layout resolverLayout) string {
	return resolveDirAgainstWD(wd, layout.Filename)
}

// writeResolverScaffold writes a slim `resolver.go`
// when no file exists at that path, OR AST-merges any missing
// sqlgen-required fields (Q / M / Client) into an existing Resolver struct.
// The scaffold contains ONLY the struct declaration with Client / Q / M
// fields — no `Query()` / `Mutation()` interface methods, no
// `queryResolver` / `mutationResolver` type defs, since gqlgen v0.17.x
// emits those into the schema-source resolver file (typically
// `shared_gen.resolvers.go`). Pre-emitting them here would collide with
// gqlgen's per-schema emission ("method redeclared"); the slim scaffold
// avoids the collision while still letting seeds typecheck against
// `r.Q.<Field>` / `r.M.<Field>` during gqlgen's pre-return validation
// pass.
//
// Migration path. Consumers running `sqlgen graphql gen` against an
// existing graph package (left over from an older sqlgen run that
// emitted Query/Mutation methods + type defs in resolver.go, or from
// running `gqlgen generate` directly) need the AST merge: it adds any
// missing fields without disturbing consumer-authored fields, methods,
// or imports. The older method declarations remain — they will
// collide with gqlgen's emission unless the consumer removes them
// manually (a one-time migration step documented in `sqlgen graphql init`
// stdout).
//
// Returns the path that was touched, or empty string when the file
// existed and the merge was a no-op.
func writeResolverScaffold(dir string, opts GenOptions) (string, error) {
	path := filepath.Join(dir, "resolver.go")
	hasMutations := len(opts.SqlgenManagedFields.MutationFields) > 0

	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("checking %s: %w", path, err)
		}
		content := buildResolverScaffold(opts.SeedPackage, opts.SeedImports, hasMutations)
		// Sort the hand-built import block with goimports so a fresh
		// scaffold matches the merge path's imports.Process output byte-for-byte.
		// buildResolverScaffold emits imports in a fixed order (models, then
		// sqlgenresolver); that order equals goimports' alphabetical sort only
		// when sqlgenresolver nests under the models tree (e.g. models/graph/…).
		// For a resolver dir OUTSIDE the models tree — a top-level `graph/`
		// sibling, where `graph/sqlgenresolver` sorts before `models` — the two
		// diverge, so a fresh scaffold and a later merge (which does run
		// imports.Process) would produce different byte output, making
		// generation non-idempotent. FormatOnly:true sorts the existing imports
		// WITHOUT triggering goimports' module-cache scan, so it stays safe on a
		// fresh run where the consumer's models / sqlgenresolver packages are not
		// yet on disk (the same constraint that keeps the block hand-built).
		formatted, err := imports.Process(path, []byte(content), &imports.Options{
			Comments:   true,
			TabIndent:  true,
			TabWidth:   8,
			FormatOnly: true,
		})
		if err != nil {
			return "", fmt.Errorf("formatting %s: %w", path, err)
		}
		if err := os.WriteFile(path, formatted, 0o600); err != nil {
			return "", fmt.Errorf("writing %s: %w", path, err)
		}
		return path, nil
	}

	merged, err := mergeResolverScaffold(path, opts.SeedImports, hasMutations)
	if err != nil {
		return "", err
	}
	if merged {
		return path, nil
	}
	return "", nil
}

// mergeResolverScaffold reads an existing resolver.go, AST-merges any
// missing sqlgen-required fields (Q / M / Client) into the Resolver struct,
// adds the sqlgenresolver import when needed, and rewrites the file in
// place. Consumer-added fields, methods, and imports are preserved. Returns
// true when the file was rewritten so callers can attribute it to the
// generate report.
func mergeResolverScaffold(path string, imps SeedImports, hasMutations bool) (bool, error) {
	src, err := os.ReadFile(path) //nolint:gosec // path is the consumer's own resolver.go under their resolver dir
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return false, fmt.Errorf("parsing %s: %w", path, err)
	}

	resolver := findResolverStruct(file)
	if resolver == nil {
		// No `type Resolver struct { ... }` — leave the file alone. The
		// consumer is responsible for the unusual shape; the scaffold path
		// only knows how to extend a recognisable Resolver struct.
		return false, nil
	}

	addedField := ensureResolverFields(resolver, imps, hasMutations)
	addedImport := false
	if imps.SqlgenResolverImportPath != "" && !hasImport(file, imps.SqlgenResolverImportPath) {
		addImport(file, imps.SqlgenResolverImportPath)
		addedImport = true
	}
	if !addedField && !addedImport {
		return false, nil
	}

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, file); err != nil {
		return false, fmt.Errorf("printing %s: %w", path, err)
	}
	// Under single-file layout the struct AST has zero-position
	// field nodes (newly synthesised by ensureResolverFields), so the bare
	// printer.Fprint output interleaves comments awkwardly between the
	// `*` and the type name. imports.Process re-formats to gofumpt-clean
	// shape and as a bonus drops any imports left unreferenced after the
	// merge. Idempotent under follow-schema where the field nodes already
	// have stable positions.
	formatted, err := imports.Process(path, buf.Bytes(), &imports.Options{
		Comments:   true,
		TabIndent:  true,
		TabWidth:   8,
		FormatOnly: false,
	})
	if err != nil {
		return false, fmt.Errorf("formatting %s: %w", path, err)
	}
	if err := os.WriteFile(path, formatted, 0o600); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

// findResolverStruct returns the *ast.StructType for `type Resolver struct
// { ... }` in the given file, or nil if not found.
func findResolverStruct(file *ast.File) *ast.StructType {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "Resolver" {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if ok {
				return st
			}
		}
	}
	return nil
}

// ensureResolverFields adds Q / M / Client fields to the Resolver struct
// when absent. Returns true when at least one field was added — the caller
// uses this signal to decide whether to rewrite the file.
func ensureResolverFields(st *ast.StructType, imps SeedImports, hasMutations bool) bool {
	existing := make(map[string]bool)
	for _, f := range st.Fields.List {
		for _, name := range f.Names {
			existing[name.Name] = true
		}
	}
	added := false
	if !existing["Client"] && imps.ModelsAlias != "" {
		st.Fields.List = append(st.Fields.List, &ast.Field{
			Names: []*ast.Ident{ast.NewIdent("Client")},
			Type: &ast.StarExpr{
				X: &ast.SelectorExpr{
					X:   ast.NewIdent(imps.ModelsAlias),
					Sel: ast.NewIdent("Client"),
				},
			},
		})
		added = true
	}
	if !existing["Q"] {
		st.Fields.List = append(st.Fields.List, &ast.Field{
			Names: []*ast.Ident{ast.NewIdent("Q")},
			Type: &ast.StarExpr{
				X: &ast.SelectorExpr{
					X:   ast.NewIdent("sqlgenresolver"),
					Sel: ast.NewIdent("Q"),
				},
			},
		})
		added = true
	}
	if hasMutations && !existing["M"] {
		st.Fields.List = append(st.Fields.List, &ast.Field{
			Names: []*ast.Ident{ast.NewIdent("M")},
			Type: &ast.StarExpr{
				X: &ast.SelectorExpr{
					X:   ast.NewIdent("sqlgenresolver"),
					Sel: ast.NewIdent("M"),
				},
			},
		})
		added = true
	}
	return added
}

// hasImport reports whether the file already imports the given path.
func hasImport(file *ast.File, importPath string) bool {
	quoted := `"` + importPath + `"`
	for _, imp := range file.Imports {
		if imp.Path != nil && imp.Path.Value == quoted {
			return true
		}
	}
	return false
}

// addImport appends a new import to the file's first import declaration.
// Creates a new import block when the file has none.
func addImport(file *ast.File, importPath string) {
	newSpec := &ast.ImportSpec{
		Path: &ast.BasicLit{
			Kind:  token.STRING,
			Value: `"` + importPath + `"`,
		},
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		gen.Specs = append(gen.Specs, newSpec)
		gen.Lparen = token.Pos(1) // force parenthesised form so the new spec renders correctly
		file.Imports = append(file.Imports, newSpec)
		return
	}
	gen := &ast.GenDecl{
		Tok:    token.IMPORT,
		Lparen: token.Pos(1),
		Specs:  []ast.Spec{newSpec},
	}
	file.Decls = append([]ast.Decl{gen}, file.Decls...)
	file.Imports = append(file.Imports, newSpec)
}

// buildResolverScaffold produces the slim resolver.go body — the
// `type Resolver struct { Client; Q; M }` declaration plus the matching
// imports. NO `Query()` / `Mutation()` interface methods, NO
// `queryResolver` / `mutationResolver` type defs: gqlgen v0.17.x emits
// those into the schema-source resolver file (e.g.
// `shared_gen.resolvers.go`) for the schema that declares the root
// `type Query` / `type Mutation`. Pre-emitting them here would collide
// with gqlgen's per-schema emission and the package would fail to
// compile with "method redeclared".
//
// `M` is gated on `hasMutations` so read-only schemas (no mutation ops)
// don't reference `*sqlgenresolver.M` (gqlgen does not generate the
// MutationResolver interface in that case). `Client` is gated on a
// resolvable models alias.
func buildResolverScaffold(pkg string, imps SeedImports, hasMutations bool) string {
	var b strings.Builder
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\nimport (\n")
	if imps.ModelsImportPath != "" {
		b.WriteString("\t")
		if imps.ModelsAlias != "" {
			b.WriteString(imps.ModelsAlias)
			b.WriteString(" ")
		}
		b.WriteString("\"")
		b.WriteString(imps.ModelsImportPath)
		b.WriteString("\"\n")
	}
	if imps.SqlgenResolverImportPath != "" {
		b.WriteString("\t\"")
		b.WriteString(imps.SqlgenResolverImportPath)
		b.WriteString("\"\n")
	}
	b.WriteString(")\n\n")

	b.WriteString("// Resolver is the root resolver struct. The Client / Q / M fields are\n")
	b.WriteString("// pre-populated by sqlgen — initialize them at app boot before\n")
	b.WriteString("// serving traffic so the per-table delegations have a Client to dispatch\n")
	b.WriteString("// through. Add additional dependency fields (loggers, auth clients, etc.)\n")
	b.WriteString("// below the Q / M lines as your consumer code requires; sqlgen never\n")
	b.WriteString("// overwrites resolver.go after the first run, so consumer edits are\n")
	b.WriteString("// preserved verbatim.\n")
	b.WriteString("//\n")
	b.WriteString("// gqlgen owns the Query() / Mutation() interface methods and the\n")
	b.WriteString("// queryResolver / mutationResolver type defs — those are emitted into the\n")
	b.WriteString("// schema-source resolver file (typically shared_gen.resolvers.go).\n")
	b.WriteString("type Resolver struct {\n")
	if imps.ModelsAlias != "" {
		b.WriteString("\tClient *")
		b.WriteString(imps.ModelsAlias)
		b.WriteString(".Client\n")
	}
	b.WriteString("\tQ      *sqlgenresolver.Q\n")
	if hasMutations {
		b.WriteString("\tM      *sqlgenresolver.M\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// writeFollowSchemaSeeds emits one seed file per table (one per schema
// source, matching gqlgen's `{name}.resolvers.go` filename template).
// Returns the paths newly written this run; pre-existing files are skipped
// and excluded from the result.
func writeFollowSchemaSeeds(dir, filenameTpl string, opts GenOptions) ([]string, error) {
	written := make([]string, 0, len(opts.Seeds))
	for _, seed := range opts.Seeds {
		name := strings.ReplaceAll(filenameTpl, "{name}", seed.SnakeName+"_gen")
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("checking seed file %s: %w", path, err)
		}
		content := buildSeedFileContents(opts.SeedPackage, opts.SeedImports, seed.Body)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return nil, fmt.Errorf("writing seed file %s: %w", path, err)
		}
		written = append(written, path)
	}
	return written, nil
}

// writeSingleFileSeed concatenates every table's delegation body into one
// file at path (singleFileResolverPath). Only writes when the destination is
// absent — gqlgen owns the file after the first run.
//
// Under single-file layout the `resolver.filename` (default `resolver.go`)
// is where gqlgen would have scaffolded Resolver / queryResolver /
// mutationResolver type defs alongside per-field methods. Sqlgen's seed
// file MUST include those type defs so the per-field methods reference a
// defined receiver type — `scaffoldBodyOnly` produces exactly that, then
// every table's delegation body is appended. (Under follow-schema layout
// gqlgen emits the type defs and Query() / Mutation() methods into the
// schema-source resolver file instead, and the wrapper's post-emit
// `mergeResolverFields` step adds Q / M / Client fields to gqlgen's
// scaffolded `resolver.go` — see writeSeedFiles for the layout split.)
//
// gqlgen v0.17.90's resolvergen plugin OWNS the single-file
// destination and rewrites it on every subsequent regeneration — wiping
// the seeded `Resolver struct { Client; Q; M }` back to the gqlgen-default
// `Resolver struct{}` while preserving the per-table method bodies. The
// post-gqlgen `mergeSingleFileResolver` step re-adds Client / Q / M to
// the wiped struct so the preserved `r.Q.X(...)` / `r.M.X(...)` bodies
// compile against the regenerated Resolver type.
func writeSingleFileSeed(path string, opts GenOptions) ([]string, error) {
	if _, err := os.Stat(path); err == nil {
		return nil, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("checking seed file %s: %w", path, err)
	}
	hasMutations := len(opts.SqlgenManagedFields.MutationFields) > 0
	scaffoldBody := scaffoldBodyOnly(hasMutations, opts.SeedImports.ModelsAlias)

	var combined strings.Builder
	combined.WriteString(scaffoldBody)
	combined.WriteString("\n")
	for _, seed := range opts.Seeds {
		combined.WriteString(seed.Body)
		combined.WriteString("\n")
	}
	content := buildSeedFileContents(opts.SeedPackage, opts.SeedImports, combined.String())
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return nil, fmt.Errorf("writing seed file %s: %w", path, err)
	}
	return []string{path}, nil
}

// mergeSingleFileResolver runs AFTER the gqlgen subprocess under single-file
// layout. gqlgen v0.17.90's resolvergen plugin owns the single-file
// destination and rewrites the Resolver struct back to `Resolver struct{}`
// on every run, wiping the seeded Client / Q / M fields. The preserved
// per-table method bodies still reference `r.Q.X(...)` / `r.M.X(...)`, so
// the package fails to compile until those fields are restored. The merge
// re-uses `mergeResolverScaffold` (the same helper that handles a consumer
// migrating from an older sqlgen layout under follow-schema) to AST-merge the
// missing fields without disturbing gqlgen-emitted type defs, methods, or
// imports.
//
// Returns the file path when the merge rewrote it (so the caller can fold
// it into GenResult.SeedFiles), or empty string when the file is absent or
// the struct already has every required field.
//
// No-op under follow-schema: the merge for that layout runs BEFORE the
// gqlgen subprocess (via writeResolverScaffold) because gqlgen does not
// own resolver.go there and preserves the seeded struct through the
// subprocess unchanged.
func mergeSingleFileResolver(opts GenOptions, layout resolverLayout) (string, error) {
	if layout.Layout != layoutSingleFile {
		return "", nil
	}
	path := singleFileResolverPath(opts.WorkingDir, layout)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("checking %s: %w", path, err)
	}
	hasMutations := len(opts.SqlgenManagedFields.MutationFields) > 0
	merged, err := mergeResolverScaffold(path, opts.SeedImports, hasMutations)
	if err != nil {
		return "", err
	}
	if !merged {
		return "", nil
	}
	return path, nil
}

// scaffoldBodyOnly returns the Resolver / queryResolver / mutationResolver
// type defs and Query() / Mutation() interface methods — without a package
// declaration or import block. Used by single-file layout, where the seed
// file IS the resolver.go gqlgen would otherwise scaffold and the type defs
// share a file with the per-field method bodies.
func scaffoldBodyOnly(hasMutations bool, modelsAlias string) string {
	var b strings.Builder
	b.WriteString("// Resolver is the gqlgen-scaffolded root resolver struct. The Client / Q / M\n")
	b.WriteString("// fields are pre-populated by sqlgen.\n")
	b.WriteString("type Resolver struct {\n")
	if modelsAlias != "" {
		b.WriteString("\tClient *")
		b.WriteString(modelsAlias)
		b.WriteString(".Client\n")
	}
	b.WriteString("\tQ      *sqlgenresolver.Q\n")
	if hasMutations {
		b.WriteString("\tM      *sqlgenresolver.M\n")
	}
	b.WriteString("}\n\n")
	b.WriteString("func (r *Resolver) Query() QueryResolver { return &queryResolver{r} }\n")
	if hasMutations {
		b.WriteString("func (r *Resolver) Mutation() MutationResolver { return &mutationResolver{r} }\n")
	}
	b.WriteString("\ntype queryResolver struct{ *Resolver }\n")
	if hasMutations {
		b.WriteString("\ntype mutationResolver struct{ *Resolver }\n")
	}
	return b.String()
}

// buildSeedFileContents wraps a rendered delegation body in a Go source
// preamble — package declaration plus the seed-specific import block.
//
// When the gqlgen model import is known, the seed body references
// gqlgen-emitted input types as `<GqlgenModelAlias>.Create<T>Input` etc.
// The aliased import line is emitted alongside the consumer models import so
// the rendered file resolves the type references correctly.
func buildSeedFileContents(pkg string, imps SeedImports, body string) string {
	var b strings.Builder
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\nimport (\n")
	b.WriteString("\t\"context\"\n")
	if imps.ModelsImportPath != "" {
		b.WriteString("\n\t")
		if imps.ModelsAlias != "" {
			b.WriteString(imps.ModelsAlias)
			b.WriteString(" ")
		}
		b.WriteString("\"")
		b.WriteString(imps.ModelsImportPath)
		b.WriteString("\"\n")
	}
	if imps.GqlgenModelImportPath != "" && imps.GqlgenModelAlias != "" {
		b.WriteString("\t")
		b.WriteString(imps.GqlgenModelAlias)
		b.WriteString(" \"")
		b.WriteString(imps.GqlgenModelImportPath)
		b.WriteString("\"\n")
	}
	if imps.SqlgenResolverImportPath != "" {
		b.WriteString("\n\t\"")
		b.WriteString(imps.SqlgenResolverImportPath)
		b.WriteString("\"\n")
	}
	b.WriteString(")\n\n")
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// resolveDirAgainstWD joins a relative path against the working directory.
// Absolute paths pass through unchanged. Empty wd falls back to ".".
func resolveDirAgainstWD(wd, dir string) string {
	if filepath.IsAbs(dir) {
		return dir
	}
	if wd == "" {
		wd = "."
	}
	return filepath.Join(wd, dir)
}

// rewritePanicStubs walks every gqlgen-owned file in the resolver dir and
// AST-rewrites panic-stub method bodies on *queryResolver / *mutationResolver
// when the method name matches a sqlgen-managed field. The replacement body
// is a single `return r.Q.<Field>(...)` / `return r.M.<Field>(...)` statement
// (this handles the case where an existing seed file gains a new sqlgen
// operation, e.g. after a config flip).
//
// The rewriter is bounded by the SqlgenManagedFieldSet: only methods whose
// names appear in the set get rewritten. Consumer-authored fields are never
// touched. Non-panic bodies are skipped — the rewriter only transforms
// `panic(...)` statements (gqlgen's documented stub shape).
func rewritePanicStubs(opts GenOptions, layout resolverLayout) error {
	if len(opts.SqlgenManagedFields.QueryFields) == 0 && len(opts.SqlgenManagedFields.MutationFields) == 0 {
		return nil
	}
	files, err := gqlgenOwnedFiles(opts.WorkingDir, layout)
	if err != nil {
		return err
	}
	seedBodies := seedMethodBodies(opts.Seeds)
	for _, path := range files {
		if err := rewriteOneFile(path, opts.SqlgenManagedFields, seedBodies); err != nil {
			return fmt.Errorf("rewriting %s: %w", path, err)
		}
	}
	return nil
}

// seedMethodBodies indexes this run's rendered seed delegations by
// "<receiverType>.<MethodName>" so the panic-stub rewriter can splice in the
// body sqlgen would have written, rather than synthesising one.
//
// This matters because seed files are write-once: once
// `<table>_gen.resolvers.go` exists, gqlgen owns it, and a table that later
// gains a new sqlgen-managed operation gets a gqlgen panic stub instead of
// the seeded body. The seeded body is the only one that knows to route the
// argument through `translate<T>Filter` / `translateCreate<T>Input` / etc.,
// so reusing it keeps a newly-added operation identical to one that was
// present when the file was first seeded.
//
// A seed body that fails to parse is skipped rather than fatal — the
// rewriter falls back to the synthesised delegation, which is what it did
// before this index existed.
func seedMethodBodies(seeds []TableSeed) map[string]string {
	out := make(map[string]string)
	for _, seed := range seeds {
		src := "package p\n" + seed.Body
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "seed.go", src, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 || fn.Body == nil {
				continue
			}
			recv := receiverTypeName(fn.Recv.List[0])
			if recv == "" {
				continue
			}
			// Keep the body as source text including its braces. Splicing
			// the parsed nodes instead would carry positions from this
			// FileSet into the target file, where they alias unrelated
			// offsets and the printer reattaches the target's own comments
			// inside the spliced block.
			lo := fset.Position(fn.Body.Pos()).Offset
			hi := fset.Position(fn.Body.End()).Offset
			if lo < 0 || hi > len(src) || lo >= hi {
				continue
			}
			out[recv+"."+fn.Name.Name] = src[lo:hi]
		}
	}
	return out
}

// gqlgenOwnedFiles returns the list of resolver files gqlgen owns under the
// active layout, resolving paths against the working directory wd.
// follow-schema returns every file in the resolver dir matching the
// filename_template (which uses `{name}` as a wildcard); single-file returns
// just the singleFileResolverPath file.
func gqlgenOwnedFiles(wd string, layout resolverLayout) ([]string, error) {
	if layout.Layout == layoutSingleFile {
		path := singleFileResolverPath(wd, layout)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("stat %s: %w", path, err)
		}
		return []string{path}, nil
	}
	dir := resolveDirAgainstWD(wd, layout.Dir)
	pattern := strings.ReplaceAll(layout.FilenameTemplate, "{name}", "*")
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", pattern, err)
	}
	slices.Sort(matches)
	return matches, nil
}

// rewriteOneFile parses a single Go file and rewrites every panic-stub
// method on *queryResolver / *mutationResolver whose name is in the managed
// set. Returns nil with no work when the file has no qualifying methods.
func rewriteOneFile(path string, managed SqlgenManagedFieldSet, seedBodies map[string]string) error {
	src, err := os.ReadFile(path) //nolint:gosec // path comes from filepath.Glob under resolver dir
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	queryNames := stringSet(managed.QueryFields)
	mutationNames := stringSet(managed.MutationFields)

	// Replacements are applied to the source text rather than to the AST:
	// the replacement body comes from a different file (the rendered seed),
	// and re-printing a tree that mixes positions from two FileSets makes
	// the printer reattach this file's comments inside the spliced blocks.
	type replacement struct {
		lo, hi int
		body   string
	}
	var reps []replacement

	for _, decl := range file.Decls {
		fn, body, ok, err := seedOrSynthBody(decl, queryNames, mutationNames, seedBodies)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		reps = append(reps, replacement{
			lo:   fset.Position(fn.Body.Pos()).Offset,
			hi:   fset.Position(fn.Body.End()).Offset,
			body: body,
		})
	}
	if len(reps) == 0 {
		return nil
	}

	// Apply back-to-front so earlier offsets stay valid.
	out := src
	for _, r := range slices.Backward(reps) {
		if r.lo < 0 || r.hi > len(out) || r.lo >= r.hi {
			return fmt.Errorf("rewriting %s: body offsets out of range", path)
		}
		out = slices.Concat(out[:r.lo], []byte(r.body), out[r.hi:])
	}

	// Run goimports to drop any imports that are now unused — the
	// panic-stub bodies typically reference `fmt.Errorf("not implemented:
	// …")`, so rewriting EVERY stub in a file leaves `fmt` imported but
	// unreferenced, which is a Go compile error. It also adds imports the
	// spliced seed body newly references, and re-groups the import block to
	// gofumpt-compatible style.
	formatted, err := imports.Process(path, out, &imports.Options{
		Comments:   true,
		TabIndent:  true,
		TabWidth:   8,
		FormatOnly: false,
	})
	if err != nil {
		return fmt.Errorf("formatting %s: %w", path, err)
	}
	// G703: `path` is a filepath.Glob match under the resolver dir and the
	// content is that same file's own source with managed method bodies
	// substituted — the write target is the file just read, so no external
	// path or data reaches this call.
	if err := os.WriteFile(path, formatted, 0o600); err != nil { //nolint:gosec // see above
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// maybeRewriteFuncDecl checks one top-level declaration for the rewriter's
// match conditions: function declaration, panic-stub body, receiver on
// *queryResolver / *mutationResolver, name in the managed set. Returns true
// when the body was rewritten in place.
func seedOrSynthBody(decl ast.Decl, queryNames, mutationNames map[string]bool, seedBodies map[string]string) (*ast.FuncDecl, string, bool, error) {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
		return nil, "", false, nil
	}
	recvType := receiverTypeName(fn.Recv.List[0])
	if !managedMatch(recvType, fn.Name.Name, queryNames, mutationNames) {
		return nil, "", false, nil
	}
	if !isPanicStubBody(fn.Body) {
		return nil, "", false, nil
	}
	// Prefer the body sqlgen rendered for this method. The synthesised
	// delegation below passes every parameter straight through, which is
	// only correct for operations whose arguments need no translation
	// (PK-addressed get / delete). Anything taking a filter, sort, or
	// create/update input must route through its translate<T> helper first,
	// and only the seed body knows that.
	if seeded, ok := seedBodies[recvType+"."+fn.Name.Name]; ok {
		return fn, seeded, true, nil
	}
	body, err := buildDelegationText(recvType, fn)
	if err != nil {
		return nil, "", false, fmt.Errorf("building delegation for %s: %w", fn.Name.Name, err)
	}
	return fn, body, true, nil
}

// managedMatch reports whether (recvType, fnName) corresponds to a
// sqlgen-managed field. Receivers other than *queryResolver / *mutationResolver
// are non-matches by construction (consumer-authored helpers don't carry
// either receiver).
func managedMatch(recvType, fnName string, queryNames, mutationNames map[string]bool) bool {
	switch recvType {
	case "queryResolver":
		return queryNames[fnName]
	case "mutationResolver":
		return mutationNames[fnName]
	default:
		return false
	}
}

// receiverTypeName returns the bare type name for a receiver field
// (`(r *queryResolver)` → "queryResolver"). Returns empty string when the
// receiver is not a pointer to a named type.
func receiverTypeName(field *ast.Field) string {
	star, ok := field.Type.(*ast.StarExpr)
	if !ok {
		if id, ok := field.Type.(*ast.Ident); ok {
			return id.Name
		}
		return ""
	}
	id, ok := star.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return id.Name
}

// isPanicStubBody reports whether a function body matches the gqlgen
// panic-stub shape: a single statement of the form `panic(<expr>)`. Both
// `panic("not implemented")` and `panic(fmt.Errorf("not implemented: …"))`
// shapes match — the rewriter only cares that the body is structurally a
// panic call, not what argument the panic carries.
func isPanicStubBody(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) != 1 {
		return false
	}
	expr, ok := body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := expr.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	return id.Name == "panic"
}

// buildDelegationBody constructs `{ return r.Q.<Field>(<args>) }` or
// `{ return r.M.<Field>(<args>) }` for the given function declaration. The
// argument list is sourced from the function's parameter names so the
// rewritten body matches the method signature without further plumbing.
func buildDelegationText(recvType string, fn *ast.FuncDecl) (string, error) {
	var helperField string
	switch recvType {
	case "queryResolver":
		helperField = "Q"
	case "mutationResolver":
		helperField = "M"
	default:
		return "", fmt.Errorf("unknown receiver %q", recvType)
	}
	args := make([]string, 0, paramCount(fn))
	for _, p := range fn.Type.Params.List {
		for _, name := range p.Names {
			args = append(args, name.Name)
		}
	}
	return "{\n\treturn r." + helperField + "." + fn.Name.Name +
		"(" + strings.Join(args, ", ") + ")\n}", nil
}

// paramCount returns the total parameter count for a function (sums named
// idents across each parameter group).
func paramCount(fn *ast.FuncDecl) int {
	if fn.Type == nil || fn.Type.Params == nil {
		return 0
	}
	n := 0
	for _, p := range fn.Type.Params.List {
		n += len(p.Names)
	}
	return n
}

// stringSet builds a presence map from a slice for O(1) lookups.
func stringSet(xs []string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		out[x] = true
	}
	return out
}
