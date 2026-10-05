package sqlite

import (
	"fmt"
	"strings"

	rsql "github.com/rqlite/sql"
)

// QualifyFilterIdentifiers rewrites every bare column reference inside expr to
// be qualified with alias. Already-qualified refs (table.column) are left
// untouched. The expression is parsed via rqlite/sql and re-emitted via the
// AST node's String() method, so quoted identifiers, SQLite operators
// (GLOB, REGEXP, ->, ->>), and string-literal contents are all handled by
// the upstream parser.
//
// It performs the codegen-time qualification of sub-categorized o2o
// `filter:` predicates.
func QualifyFilterIdentifiers(expr, alias string) (string, error) {
	if strings.TrimSpace(expr) == "" {
		return "", fmt.Errorf("qualify filter: empty expression")
	}
	if strings.TrimSpace(alias) == "" {
		return "", fmt.Errorf("qualify filter: empty alias")
	}

	parsed, err := rsql.ParseExprString(expr)
	if err != nil {
		return "", fmt.Errorf("qualify filter: parsing %q: %w", expr, err)
	}
	if parsed == nil {
		return "", fmt.Errorf("qualify filter: empty parse for %q", expr)
	}

	qualified := qualifyExpr(parsed, alias)
	return qualified.String(), nil
}

// qualifyExpr returns expr with every bare identifier replaced by a
// QualifiedRef using alias. The walker handles each Expr type explicitly so
// identifiers that are not column references (function names, already-
// qualified refs) are not touched.
func qualifyExpr(expr rsql.Expr, alias string) rsql.Expr {
	if expr == nil {
		return nil
	}
	if ident, ok := expr.(*rsql.Ident); ok {
		return &rsql.QualifiedRef{
			Table:  &rsql.Ident{Name: alias},
			Column: ident,
		}
	}
	if _, ok := expr.(*rsql.QualifiedRef); ok {
		return expr
	}
	qualifyExprChildren(expr, alias)
	return expr
}

// qualifyExprChildren mutates the operand fields of compound Expr nodes by
// running each operand through qualifyExpr. Leaf node types and literals are
// no-ops.
func qualifyExprChildren(expr rsql.Expr, alias string) {
	switch e := expr.(type) {
	case *rsql.BinaryExpr:
		e.X = qualifyExpr(e.X, alias)
		e.Y = qualifyExpr(e.Y, alias)
	case *rsql.UnaryExpr:
		e.X = qualifyExpr(e.X, alias)
	case *rsql.ParenExpr:
		e.X = qualifyExpr(e.X, alias)
	case *rsql.Range:
		e.X = qualifyExpr(e.X, alias)
		e.Y = qualifyExpr(e.Y, alias)
	case *rsql.ExprList:
		for i, x := range e.Exprs {
			e.Exprs[i] = qualifyExpr(x, alias)
		}
	case *rsql.Call:
		for i, arg := range e.Args {
			e.Args[i] = qualifyExpr(arg, alias)
		}
	case *rsql.CaseExpr:
		e.Operand = qualifyExpr(e.Operand, alias)
		for _, blk := range e.Blocks {
			blk.Condition = qualifyExpr(blk.Condition, alias)
			blk.Body = qualifyExpr(blk.Body, alias)
		}
		e.ElseExpr = qualifyExpr(e.ElseExpr, alias)
	case *rsql.CastExpr:
		e.X = qualifyExpr(e.X, alias)
	case *rsql.CollateExpr:
		e.X = qualifyExpr(e.X, alias)
	case *rsql.Null:
		e.X = qualifyExpr(e.X, alias)
	}
}
