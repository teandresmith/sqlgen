package cli

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// newLintCmd creates the lint subcommand that performs static analysis on
// consumer Go code to catch common hook registration mistakes.
func newLintCmd(flags *cliFlags) *cobra.Command {
	var paths string
	var failOn string

	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Static analysis of hook registrations in consumer Go code",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()

			threshold, err := parseSeverity(failOn)
			if err != nil {
				return err
			}

			cfg, schema, err := loadAndValidate(flags, errOut)
			if err != nil {
				return err
			}

			reg, err := buildTypeRegistry(schema, cfg)
			if err != nil {
				_, _ = fmt.Fprintf(errOut, "Error: %v\n", err)
				return &exitError{code: ExitGeneration, err: err}
			}

			// Determine scan directories.
			dirs := []string{cfg.Output.Dir}
			if paths != "" {
				dirs = strings.Split(paths, ",")
				for i := range dirs {
					dirs[i] = strings.TrimSpace(dirs[i])
				}
			}

			issues, err := lintFiles(reg, dirs)
			if err != nil {
				_, _ = fmt.Fprintf(errOut, "Lint error: %v\n", err)
				return &exitError{code: ExitGeneration, err: err}
			}

			if len(issues) == 0 {
				return nil
			}

			sortIssues(issues)
			formatLintOutput(out, issues)

			if hasIssuesAtOrAbove(issues, threshold) {
				return &exitError{
					code: ExitGeneration,
					err:  fmt.Errorf("lint found issues at or above %q severity", failOn),
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&paths, "paths", "", "comma-separated directories to scan (default: output.dir from config)")
	cmd.Flags().StringVar(&failOn, "fail-on", "error", "minimum severity to trigger non-zero exit: error, warning, info")

	return cmd
}

// lintSeverity represents the severity level of a lint issue.
type lintSeverity int

const (
	severityInfo lintSeverity = iota
	severityWarning
	severityError
)

func (s lintSeverity) String() string {
	switch s {
	case severityInfo:
		return "info"
	case severityWarning:
		return "warn"
	case severityError:
		return "error"
	default:
		return "unknown"
	}
}

// parseSeverity converts a string to a lintSeverity.
func parseSeverity(s string) (lintSeverity, error) {
	switch s {
	case "error":
		return severityError, nil
	case "warning":
		return severityWarning, nil
	case "info":
		return severityInfo, nil
	default:
		return 0, fmt.Errorf("invalid severity: %q (valid: error, warning, info)", s)
	}
}

// lintIssue represents a single lint finding.
type lintIssue struct {
	Severity    lintSeverity
	File        string
	Line        int
	Code        string
	Explanation string
}

// tableInfo holds the type registry information for a single table.
type tableInfo struct {
	SQLName       string
	StructName    string
	ConstantName  string // e.g., "TableProducts"
	TypeNames     map[string]bool
	HasSoftDelete bool
	HasIncrement  bool
	Ops           gen.ResolvedOperations
}

// typeRegistry maps table SQL names and constant names to tableInfo.
type typeRegistry struct {
	byConstant map[string]*tableInfo // "TableProducts" → *tableInfo
	byType     map[string]*tableInfo // "CreateProductInput" → *tableInfo
}

// buildTypeRegistry constructs a type registry from the parsed schema and config.
//
// Every fact the rules read is taken off a built gen.TableContext rather than
// re-derived from the raw parser table. The registry sits deep in the
// generator's resolution vocabulary — it needs the names generation emitted,
// the operations it resolved, and the surfaces it actually produced — and each
// fact it derived independently was a fact free to drift from the generator
// that owns it. Increment eligibility drifted three ways before anyone noticed;
// the since-removed operations toggles drifted the other direction and
// produced a false positive. Reading the context makes every rule exact by
// construction and leaves one place to change when eligibility changes.
//
// Building contexts also gives lint the generation-phase error surface it was
// missing. Before this, a config that `generate` and `validate` both rejected
// — a resolved-field-name collision, an un-importable type literal, an
// unresolvable `api.operations` mask — made `sqlgen lint` exit 0 with no output: a
// clean bill of health for a schema that cannot produce code, and therefore
// for generated code that is necessarily stale. `sqlgen validate` used to give
// the same false all-clear, and `lint` was the last command still carrying it.
func buildTypeRegistry(schema *parser.Schema, cfg *config.RootConfig) (*typeRegistry, error) {
	contexts, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		return nil, fmt.Errorf("building type registry: %w", err)
	}

	reg := &typeRegistry{
		byConstant: make(map[string]*tableInfo),
		byType:     make(map[string]*tableInfo),
	}

	// Tables the generator skipped — excluded by `exclude_tables`, or left
	// without a resolved primary key (PRD §9.4b) — are absent from contexts
	// and stay absent here. No client, no `Table<Name>` constant and no input
	// types are emitted for them, so a registry entry would describe types
	// that do not exist.
	for _, tc := range contexts {
		info := &tableInfo{
			SQLName:       tc.TableName,
			StructName:    tc.StructName,
			ConstantName:  tc.TableNameConstant,
			TypeNames:     make(map[string]bool),
			HasSoftDelete: tc.SoftDelete != nil,
			HasIncrement:  len(tc.IncrementColumns) > 0,
			Ops:           tc.Operations,
		}

		pluralName := gen.StructNamePlural(tc.StructName)
		typeNames := []string{
			tc.StructName,
			"Create" + tc.StructName + "Input",
			"Update" + tc.StructName + "Input",
			"Get" + pluralName + "Input",
			tc.StructName + "Filter",
			tc.StructName + "FieldOptions",
		}
		for _, tn := range typeNames {
			info.TypeNames[tn] = true
			reg.byType[tn] = info
		}

		reg.byConstant[tc.TableNameConstant] = info
	}

	return reg, nil
}

// hookCall represents a parsed hook registration call found in Go source.
type hookCall struct {
	File       string
	Line       int
	Code       string // reconstructed source snippet
	IsMutation bool   // true = ForMutation, false = ForQuery
	TypeParams []string
	TableConst string // e.g., "TableProducts"
	OpRefs     []string
	// The parameter name for the context (m for mutation, q for query).
	ctxParamName string
}

// lintFiles scans Go files in the given directories and returns lint issues.
func lintFiles(reg *typeRegistry, dirs []string) ([]lintIssue, error) {
	var issues []lintIssue

	for _, dir := range dirs {
		dirIssues, err := lintDir(reg, dir)
		if err != nil {
			return nil, fmt.Errorf("linting %s: %w", dir, err)
		}
		issues = append(issues, dirIssues...)
	}

	return issues, nil
}

// lintDir scans a single directory for Go files and lints them.
func lintDir(reg *typeRegistry, dir string) ([]lintIssue, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading directory: %w", err)
	}

	var issues []lintIssue

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// Skip generated files — they are produced by sqlgen, not consumer code.
		if strings.HasSuffix(name, "_gen.go") {
			continue
		}

		filePath := filepath.Join(dir, name)
		fileIssues, err := lintFile(reg, filePath)
		if err != nil {
			return nil, fmt.Errorf("linting %s: %w", filePath, err)
		}
		issues = append(issues, fileIssues...)
	}

	return issues, nil
}

// lintFile parses a single Go file and checks hook calls against the registry.
func lintFile(reg *typeRegistry, path string) ([]lintIssue, error) {
	fset := token.NewFileSet()
	f, err := goparser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	calls := findHookCalls(fset, f, path)

	var issues []lintIssue
	for _, call := range calls {
		issues = append(issues, validateHookCall(reg, call)...)
	}

	return issues, nil
}

// findHookCalls walks the AST looking for hook.ForMutation and hook.ForQuery calls.
func findHookCalls(fset *token.FileSet, f *ast.File, filePath string) []hookCall {
	var calls []hookCall

	ast.Inspect(f, func(n ast.Node) bool {
		callExpr, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		call, ok := parseHookCall(fset, callExpr, filePath)
		if !ok {
			return true
		}

		// Walk the callback function literal (second arg) for op references.
		if len(callExpr.Args) >= 2 {
			if funcLit, ok := callExpr.Args[1].(*ast.FuncLit); ok {
				call.ctxParamName = extractCtxParamName(funcLit, call.IsMutation)
				if call.ctxParamName != "" {
					call.OpRefs = findOpReferences(funcLit.Body, call.ctxParamName)
				}
			}
		}

		calls = append(calls, call)
		return true
	})

	return calls
}

// parseHookCall extracts information from a hook.ForMutation or hook.ForQuery call.
func parseHookCall(fset *token.FileSet, callExpr *ast.CallExpr, filePath string) (hookCall, bool) {
	var indexExpr ast.Expr
	var indices []ast.Expr

	switch fun := callExpr.Fun.(type) {
	case *ast.IndexListExpr:
		indexExpr = fun.X
		indices = fun.Indices
	case *ast.IndexExpr:
		indexExpr = fun.X
		indices = []ast.Expr{fun.Index}
	default:
		return hookCall{}, false
	}

	sel, ok := indexExpr.(*ast.SelectorExpr)
	if !ok {
		return hookCall{}, false
	}

	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "hook" {
		return hookCall{}, false
	}

	funcName := sel.Sel.Name
	var isMutation bool
	switch funcName {
	case "ForMutation":
		isMutation = true
	case "ForQuery":
		isMutation = false
	default:
		return hookCall{}, false
	}

	var typeParams []string
	for _, idx := range indices {
		typeParams = append(typeParams, extractTypeName(idx))
	}

	var tableConst string
	if len(callExpr.Args) > 0 {
		tableConst = extractTableConst(callExpr.Args[0])
	}

	pos := fset.Position(callExpr.Pos())
	code := formatCallSnippet(funcName, typeParams, tableConst)

	return hookCall{
		File:       filePath,
		Line:       pos.Line,
		Code:       code,
		IsMutation: isMutation,
		TypeParams: typeParams,
		TableConst: tableConst,
	}, true
}

// extractTypeName extracts the base type name from an AST type expression,
// stripping pointer stars and package qualifiers.
func extractTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return extractTypeName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.ArrayType:
		return "[]" + extractTypeName(e.Elt)
	default:
		return ""
	}
}

// extractTableConst extracts the table constant name from a call argument.
func extractTableConst(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		// e.g., models.TableProducts — use just the selector.
		return e.Sel.Name
	default:
		return ""
	}
}

// extractCtxParamName returns the name of the MutationContext or QueryContext
// parameter in the callback function literal.
func extractCtxParamName(funcLit *ast.FuncLit, isMutation bool) string {
	targetType := "MutationContext"
	if !isMutation {
		targetType = "QueryContext"
	}

	if funcLit.Type == nil || funcLit.Type.Params == nil {
		return ""
	}

	for _, field := range funcLit.Type.Params.List {
		typeName := extractTypeName(field.Type)
		if typeName == targetType && len(field.Names) > 0 {
			return field.Names[0].Name
		}
	}
	return ""
}

// findOpReferences finds references to operation constants in the function body,
// looking for patterns like `m.Op == hook.OpSoftDelete`.
func findOpReferences(body *ast.BlockStmt, ctxParam string) []string {
	var refs []string

	ast.Inspect(body, func(n ast.Node) bool {
		binExpr, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		if binExpr.Op != token.EQL && binExpr.Op != token.NEQ {
			return true
		}

		leftOp := isOpAccess(binExpr.X, ctxParam)
		rightOp := isOpAccess(binExpr.Y, ctxParam)
		leftConst := extractOpConst(binExpr.X)
		rightConst := extractOpConst(binExpr.Y)

		if leftOp && rightConst != "" {
			refs = append(refs, rightConst)
		} else if rightOp && leftConst != "" {
			refs = append(refs, leftConst)
		}

		return true
	})

	return refs
}

// isOpAccess returns true if the expression is {paramName}.Op.
func isOpAccess(expr ast.Expr, paramName string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Op" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == paramName
}

// extractOpConst extracts an operation constant name from an expression like
// hook.OpSoftDelete, returning "OpSoftDelete".
func extractOpConst(expr ast.Expr) string {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "hook" {
		return ""
	}
	name := sel.Sel.Name
	if strings.HasPrefix(name, "Op") {
		return name
	}
	return ""
}

// formatCallSnippet reconstructs a readable code snippet from a parsed hook call.
func formatCallSnippet(funcName string, typeParams []string, tableConst string) string {
	var typeParamStr string
	if len(typeParams) > 0 {
		formatted := make([]string, len(typeParams))
		for i, tp := range typeParams {
			formatted[i] = "*" + tp
		}
		typeParamStr = "[" + strings.Join(formatted, ", ") + "]"
	}
	return fmt.Sprintf("%s%s(%s, ...)", funcName, typeParamStr, tableConst)
}

// validateHookCall checks a single hook call against the type registry and
// returns any issues found.
func validateHookCall(reg *typeRegistry, call hookCall) []lintIssue {
	var issues []lintIssue

	tableInfo := reg.byConstant[call.TableConst]
	if tableInfo == nil {
		return nil
	}

	// Rule 1: Hook table/type mismatch (error).
	//
	// Tables are compared by ConstantName, not by SQLName. The SQL name is not
	// an identity: `public.users` and `audit.users` are both "users", so
	// comparing it made this rule — the only one at severity error, and so the
	// only one `--fail-on error` gates CI on — silently blind to every
	// cross-schema mismatch. `CreatePublicUserInput` passed to
	// `TableAuditUsers` is exactly the defect the rule exists to catch, and it
	// produced no finding at all.
	//
	// ConstantName is the right key because it is the Go identifier the
	// entity claims at package scope, and §8.5 rejects two entities claiming
	// the same one — a config that would collide here fails the context build
	// before the registry exists.
	for _, typeName := range call.TypeParams {
		if typeName == "" {
			continue
		}
		ownerInfo := reg.byType[typeName]
		if ownerInfo == nil {
			continue
		}
		if ownerInfo.ConstantName != tableInfo.ConstantName {
			issues = append(issues, lintIssue{
				Severity:    severityError,
				File:        call.File,
				Line:        call.Line,
				Code:        call.Code,
				Explanation: fmt.Sprintf("type mismatch: %s belongs to %s, not %s", typeName, ownerInfo.ConstantName, call.TableConst),
			})
		}
	}

	// Rule 2: Invalid operation for table (warning).
	//
	// A hook filtered on an operation whose method the table's client does
	// not generate never fires. The client has no operations toggle (PRD
	// §4.6), so every such method is missing for a schema reason, and the
	// message names it.
	for _, opRef := range call.OpRefs {
		if opEnabledForTable(opRef, tableInfo.Ops) {
			continue
		}
		issues = append(issues, lintIssue{
			Severity:    severityWarning,
			File:        call.File,
			Line:        call.Line,
			Code:        call.Code,
			Explanation: fmt.Sprintf("table %q %s — hook will never fire for %s", tableInfo.SQLName, opUnsupportedReason(opRef), opRef),
		})
	}

	return issues
}

// opUnsupportedReason names the schema fact that keeps the generator from
// emitting the method behind opRef (PRD §4.6).
func opUnsupportedReason(opRef string) string {
	switch opRef {
	case "OpSoftDelete", "OpSoftDeleteMany", "OpSoftDeleteWhere",
		"OpRestore", "OpRestoreMany", "OpRestoreWhere":
		return "has no soft delete column"
	case "OpIncrement":
		return "has no incrementable columns"
	case "OpUpsert", "OpUpsertMany":
		return "has no conflict target"
	default:
		return "does not generate the method behind this operation"
	}
}

// opEnabledForTable reports whether the generator emitted the method a given
// operation constant hooks, reading the same gen.ResolvedOperations the
// templates gate on. Those flags are the table's schema facts (PRD §4.6), so a
// rule reading them cannot disagree with the generator about what a table
// produces.
//
// An unrecognized constant is treated as enabled. That covers OpRefresh, which
// is a materialized-view operation with no table-level flag, and keeps a
// newly added constant silent rather than wrongly flagged.
func opEnabledForTable(opRef string, ops gen.ResolvedOperations) bool {
	flag, ok := opResolvedFlag[opRef]
	if !ok {
		return true
	}
	return flag(ops)
}

// opResolvedFlag maps each hook operation constant to the resolved flag that
// decides whether the generator emitted the method behind it.
//
// The soft-delete, restore and hard-delete families share one flag across
// their single / `*Many` / `*Where` forms, since one schema fact gates each
// family. Create and update keep a flag per method.
var opResolvedFlag = map[string]func(gen.ResolvedOperations) bool{
	"OpCreate":          func(o gen.ResolvedOperations) bool { return o.Create },
	"OpCreateMany":      func(o gen.ResolvedOperations) bool { return o.CreateMany },
	"OpUpdate":          func(o gen.ResolvedOperations) bool { return o.Update },
	"OpUpdateMany":      func(o gen.ResolvedOperations) bool { return o.UpdateMany },
	"OpUpdateWhere":     func(o gen.ResolvedOperations) bool { return o.UpdateWhere },
	"OpUpsert":          func(o gen.ResolvedOperations) bool { return o.Upsert },
	"OpUpsertMany":      func(o gen.ResolvedOperations) bool { return o.UpsertMany },
	"OpSoftDelete":      func(o gen.ResolvedOperations) bool { return o.SoftDelete },
	"OpSoftDeleteMany":  func(o gen.ResolvedOperations) bool { return o.SoftDelete },
	"OpSoftDeleteWhere": func(o gen.ResolvedOperations) bool { return o.SoftDelete },
	"OpRestore":         func(o gen.ResolvedOperations) bool { return o.Restore },
	"OpRestoreMany":     func(o gen.ResolvedOperations) bool { return o.Restore },
	"OpRestoreWhere":    func(o gen.ResolvedOperations) bool { return o.Restore },
	"OpHardDelete":      func(o gen.ResolvedOperations) bool { return o.HardDelete },
	"OpHardDeleteMany":  func(o gen.ResolvedOperations) bool { return o.HardDelete },
	"OpHardDeleteWhere": func(o gen.ResolvedOperations) bool { return o.HardDelete },
	"OpIncrement":       func(o gen.ResolvedOperations) bool { return o.Increment },
	"OpGet":             func(o gen.ResolvedOperations) bool { return o.Get },
	"OpGetMany":         func(o gen.ResolvedOperations) bool { return o.GetMany },
	"OpExists":          func(o gen.ResolvedOperations) bool { return o.Exists },
	"OpCount":           func(o gen.ResolvedOperations) bool { return o.Count },
	"OpPaginate":        func(o gen.ResolvedOperations) bool { return o.Paginate },
	"OpConnection":      func(o gen.ResolvedOperations) bool { return o.Connection },
	"OpStream":          func(o gen.ResolvedOperations) bool { return o.Stream },
}

// formatLintOutput writes the lint results in the PRD-specified format.
func formatLintOutput(w io.Writer, issues []lintIssue) {
	for _, issue := range issues {
		sev := issue.Severity.String()
		_, _ = fmt.Fprintf(w, "  %-7s%s:%d  %s\n", sev, issue.File, issue.Line, issue.Code)
		_, _ = fmt.Fprintf(w, "         %s\n\n", issue.Explanation)
	}

	if len(issues) == 0 {
		return
	}

	var errCount, warnCount, infoCount int
	for _, issue := range issues {
		switch issue.Severity {
		case severityError:
			errCount++
		case severityWarning:
			warnCount++
		case severityInfo:
			infoCount++
		}
	}

	var parts []string
	if errCount > 0 {
		parts = append(parts, fmt.Sprintf("%d error", errCount))
		if errCount > 1 {
			parts[len(parts)-1] += "s"
		}
	}
	if warnCount > 0 {
		parts = append(parts, fmt.Sprintf("%d warning", warnCount))
		if warnCount > 1 {
			parts[len(parts)-1] += "s"
		}
	}
	if infoCount > 0 {
		parts = append(parts, fmt.Sprintf("%d info", infoCount))
	}

	_, _ = fmt.Fprintf(w, "%d issues (%s)\n", len(issues), strings.Join(parts, ", "))
}

// hasIssuesAtOrAbove returns true if any issue has severity >= threshold.
func hasIssuesAtOrAbove(issues []lintIssue, threshold lintSeverity) bool {
	for _, issue := range issues {
		if issue.Severity >= threshold {
			return true
		}
	}
	return false
}

// sortIssues sorts issues by severity (highest first), then file, then line.
func sortIssues(issues []lintIssue) {
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Severity != issues[j].Severity {
			return issues[i].Severity > issues[j].Severity
		}
		if issues[i].File != issues[j].File {
			return issues[i].File < issues[j].File
		}
		return issues[i].Line < issues[j].Line
	})
}
