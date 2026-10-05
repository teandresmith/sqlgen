package gen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"golang.org/x/tools/imports"
)

// stdSQLAlias is the import alias every generated file gives database/sql.
//
// sqlgen's own runtime package is github.com/teandresmith/sqlgen/sql, so an
// unaliased database/sql binds the same file-local package name and one of the
// two silently loses: goimports will not add a second package named `sql`, so
// every sql.Dialect / sql.Table / sql.Sort / sql.Build* reference resolves
// against database/sql instead and the package does not compile. Under
// overrides.use_pointers: false every nullable column of a nullSQL type
// resolves to a database/sql wrapper, which made that documented setting
// unusable on every dialect.
const stdSQLAlias = "stdsql"

// generatedImportAliases maps an import path to the alias generated files
// import it under. A path belongs here only when its package name collides
// with one sqlgen itself emits references to.
var generatedImportAliases = map[string]string{"database/sql": stdSQLAlias}

// resolvePackageImports runs goimports import-resolution ONCE over combinedBody
// and returns the resolved import path set the package needs, pruned to what
// the tables collectively use.
//
// The file_per_table layout uses this so each per-table file can be emitted
// with the package-wide set and formatted with a normal Format call, which
// prunes per file what that file does not use. Without it, per-file emission
// would ship incomplete imports (a compile failure).
//
// It no longer resolves anything the callers did not already declare: the
// table and view contexts carry modelTemplateImports, so this pass
// finds nothing missing and never reaches goimports' module-cache scan. It is
// kept as the single place the package-wide set is narrowed before seeding.
//
// seed is the union of the tables' declared context imports; combinedBody is
// the concatenation of every table body so the single resolution pass observes
// every referenced package. Only the path is retained: the one alias generated
// code uses (generatedImportAliases) is re-derived from the path by
// WrapWithPreamble, so it survives the round trip without being carried here.
func resolvePackageImports(pkg string, seed []string, combinedBody []byte, version, filename string) ([]string, error) {
	resolved, err := Format(WrapWithPreamble(pkg, seed, combinedBody), version, filename)
	if err != nil {
		return nil, err
	}

	return importPaths(resolved, filename)
}

// importPaths returns the unquoted import paths declared by a Go source file.
func importPaths(src []byte, filename string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("parsing imports of %s: %w", filename, err)
	}

	out := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("unquoting import %s: %w", imp.Path.Value, err)
		}
		out = append(out, path)
	}
	return out, nil
}

// importDriftObserver is a test seam. When non-nil, formatWithOptions reports
// every import path goimports had to resolve because the caller's declared set
// did not already cover it.
//
// Production never sets it, and an undeclared import is deliberately not an
// error: goimports still resolves it and the emitted file is still correct — it
// just pays a full GOMODCACHE scan to get there, which is what made generation
// scale with the size of the consumer's module cache. Keeping that
// back-fill as a safety net is the reason the emitters call Format rather than
// FormatOnly; the observer is what lets TestGeneratedImportsAreDeclared fail
// the drift in CI instead of letting it resurface as a slow suite. Tests that
// install it must not run in parallel.
var importDriftObserver func(filename string, added []string)

// reportImportDrift hands the observer the import paths formatted gained over
// raw. A file that does not parse reports nothing: imports.Process already
// succeeded on it, so an unparseable pair is a bug in this check rather than
// drift worth failing a test over.
func reportImportDrift(filename string, raw, formatted []byte) {
	declared, err := importPaths(raw, filename)
	if err != nil {
		return
	}
	resolved, err := importPaths(formatted, filename)
	if err != nil {
		return
	}

	var added []string
	for _, path := range resolved {
		if !slices.Contains(declared, path) {
			added = append(added, path)
		}
	}
	if len(added) > 0 {
		importDriftObserver(filename, added)
	}
}

// WrapWithPreamble prepends a package declaration and import block to raw
// template body bytes. Used by the orchestrator to produce complete Go files
// from template fragments that contain only code (no package/import).
//
// The import block uses the pre-computed, deduplicated import paths from
// the context — templates never emit imports themselves.
func WrapWithPreamble(pkg string, imports []string, body []byte) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "package %s\n", pkg)
	if len(imports) > 0 {
		buf.WriteString("\nimport (\n")
		for _, imp := range imports {
			if alias, ok := generatedImportAliases[imp]; ok {
				fmt.Fprintf(&buf, "\t%s %q\n", alias, imp)
				continue
			}
			fmt.Fprintf(&buf, "\t%q\n", imp)
		}
		buf.WriteString(")\n")
	}
	buf.WriteByte('\n')
	buf.Write(body)
	return buf.Bytes()
}

// filePermissions is the permission mode for generated files.
const filePermissions = 0o600

// dirPermissions is the permission mode for auto-created directories.
const dirPermissions = 0o755

// GeneratedHeaderPrefix is the version-independent part of the header every
// sqlgen-written file carries. Stale cleanup matches on it to prove a file is
// sqlgen's before deleting it (StaleFiles), so emission and that check cannot
// drift apart — Header is built from this constant.
//
// It deliberately names sqlgen rather than matching Go's generic
// `^// Code generated .* DO NOT EDIT\.$` convention: the whole point is to tell
// sqlgen's own output apart from another generator's, and wire, mockgen and
// stringer all write `_gen.go` files bearing the generic form.
const GeneratedHeaderPrefix = "// Code generated by sqlgen "

// Header returns the generated file header comment.
// The version is the sqlgen CLI binary version (e.g., "v1.2.0").
func Header(version string) string {
	return fmt.Sprintf("%s%s. DO NOT EDIT.", GeneratedHeaderPrefix, version)
}

// ExecuteAndWrite executes a template with the given data, formats the
// output through the formatting pipeline, and writes it to the specified
// file path.
//
// The formatting pipeline:
//  1. Execute the template into a buffer.
//  2. Prepend the generated file header before the content.
//  3. Run goimports to add missing imports, remove unused imports, and format.
//  4. Write to disk with 0o600 permissions.
//
// If goimports fails, the raw template output is included in the error
// message for debugging.
func ExecuteAndWrite(tmpl *template.Template, name string, data any, filePath string, version string) error {
	raw, err := executeTemplate(tmpl, name, data)
	if err != nil {
		return fmt.Errorf("executing template %s: %w", name, err)
	}

	formatted, err := Format(raw, version, filePath)
	if err != nil {
		return err
	}

	return writeFile(filePath, formatted)
}

// Format applies the formatting pipeline to raw template output:
// header injection followed by goimports (with import resolution).
//
// If goimports fails, the error includes the raw template output for debugging.
func Format(raw []byte, version string, filename string) ([]byte, error) {
	return formatWithOptions(raw, version, filename, false)
}

// FormatOnly applies the formatting pipeline without import resolution.
// This is significantly faster than Format because it skips scanning the
// module cache. Use this when imports are already correct (e.g., templates
// provide explicit imports via the shared/imports fragment).
func FormatOnly(raw []byte, version string, filename string) ([]byte, error) {
	return formatWithOptions(raw, version, filename, true)
}

func formatWithOptions(raw []byte, version string, filename string, formatOnly bool) ([]byte, error) {
	withHeader := aliasStdSQLQualifiers(injectHeader(raw, version))

	formatted, err := imports.Process(filename, withHeader, &imports.Options{
		Comments:   true,
		TabIndent:  true,
		TabWidth:   8,
		FormatOnly: formatOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("goimports failed for %s: %w\n\nraw template output:\n%s", filename, err, withHeader)
	}

	if importDriftObserver != nil && !formatOnly {
		reportImportDrift(filename, withHeader, formatted)
	}

	return formatted, nil
}

// executeTemplate executes a named template and returns the raw output.
func executeTemplate(tmpl *template.Template, name string, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("executing template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// injectHeader prepends the generated file header before the template output.
// The header is always the first line, before the package declaration.
func injectHeader(raw []byte, version string) []byte {
	header := Header(version)
	var buf bytes.Buffer
	buf.WriteString(header)
	buf.WriteByte('\n')
	buf.Write(raw)
	return buf.Bytes()
}

// writeFile writes data to the specified path, creating parent directories
// as needed. Files are written with 0o600 permissions.
func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPermissions); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	if err := os.WriteFile(path, data, filePermissions); err != nil {
		return fmt.Errorf("writing file %s: %w", path, err)
	}

	return nil
}

// aliasStdSQLQualifiers rewrites every `sql.NullX` qualifier in a generated
// file to `stdsql.NullX`, matching the alias WrapWithPreamble gives
// database/sql.
//
// The Go type strings sqlgen carries — gotype's `null:` names, the config
// `nullable:` values a consumer writes, builtInScalarRegistry's keys, the
// manifest's published go_type — all stay spelled `sql.NullString`, which is
// what a consumer importing database/sql themselves would write. Only the
// emitted qualifier moves, so the alias is a property of generated files and
// never leaks into sqlgen's own identity, config or documentation surfaces.
//
// `sql.Null…` is unambiguous: the sqlgen runtime sql package exports no
// identifier beginning with "Null", and a method call like
// sql.Where("x").IsNull() has a call expression rather than the `sql`
// identifier on the left of the selector. Files that do not import
// database/sql are left alone — the rewrite would otherwise produce a
// `stdsql` qualifier goimports cannot resolve, since it matches imports by
// package name.
func aliasStdSQLQualifiers(src []byte) []byte {
	if !bytes.Contains(src, []byte("sql.Null")) {
		return src
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		// Leave the source untouched and let imports.Process report the
		// syntax error with its own (better) diagnostics.
		return src
	}
	if !importsPath(file, "database/sql") {
		return src
	}

	var offsets []int
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "sql" || !strings.HasPrefix(sel.Sel.Name, "Null") {
			return true
		}
		offsets = append(offsets, fset.Position(pkg.Pos()).Offset)
		return true
	})
	if len(offsets) == 0 {
		return src
	}

	// Splice from the back so earlier offsets stay valid. Rewriting the
	// identifier's bytes in place keeps the rest of the file byte-identical —
	// reprinting the AST would reflow comments the templates positioned.
	slices.Sort(offsets)
	out := slices.Clone(src)
	for i := len(offsets) - 1; i >= 0; i-- {
		at := offsets[i]
		out = slices.Concat(out[:at], []byte(stdSQLAlias), out[at+len("sql"):])
	}
	return out
}

// importsPath reports whether the parsed file imports the given path.
func importsPath(file *ast.File, path string) bool {
	quoted := strconv.Quote(path)
	return slices.ContainsFunc(file.Imports, func(spec *ast.ImportSpec) bool {
		return spec.Path.Value == quoted
	})
}
