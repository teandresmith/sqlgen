package postgres

import (
	"fmt"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// QualifyFilterIdentifiers rewrites every bare column reference inside expr to
// be qualified with alias. Already-qualified refs (table.column) are left
// untouched. The expression is parsed as a PostgreSQL WHERE clause and
// re-emitted via pg_query's deparser, so quoted identifiers, JSONB operators,
// CAST/CASE/function-call expressions, and string-literal contents are all
// handled by the upstream parser rather than custom tokenization.
//
// It performs the codegen-time qualification of sub-categorized o2o `filter:`
// predicates so the JOIN ON clause binds bare identifiers to the relationship
// target instead of the parent table.
func QualifyFilterIdentifiers(expr, alias string) (string, error) {
	if strings.TrimSpace(expr) == "" {
		return "", fmt.Errorf("qualify filter: empty expression")
	}
	if strings.TrimSpace(alias) == "" {
		return "", fmt.Errorf("qualify filter: empty alias")
	}

	wrapped := "SELECT 1 WHERE " + expr
	result, err := pg_query.Parse(wrapped)
	if err != nil {
		return "", fmt.Errorf("qualify filter: parsing %q: %w", expr, err)
	}
	if len(result.Stmts) == 0 {
		return "", fmt.Errorf("qualify filter: no statement parsed from %q", expr)
	}

	sel, ok := result.Stmts[0].Stmt.Node.(*pg_query.Node_SelectStmt)
	if !ok || sel.SelectStmt == nil || sel.SelectStmt.WhereClause == nil {
		return "", fmt.Errorf("qualify filter: no WHERE clause parsed from %q", expr)
	}

	qualifyColumnRefs(sel.SelectStmt.WhereClause, alias)

	out, err := deparseWhereExpr(sel.SelectStmt.WhereClause)
	if err != nil {
		return "", fmt.Errorf("qualify filter: deparsing %q: %w", expr, err)
	}
	return out, nil
}

// qualifyColumnRefs walks the expression tree rooted at node, prepending alias
// to every ColumnRef whose Fields list is a single bare String identifier.
func qualifyColumnRefs(node *pg_query.Node, alias string) {
	if node == nil {
		return
	}

	if cr, ok := node.Node.(*pg_query.Node_ColumnRef); ok && cr.ColumnRef != nil {
		prependAlias(cr.ColumnRef, alias)
		return
	}

	for _, child := range exprChildren(node) {
		qualifyColumnRefs(child, alias)
	}
}

// prependAlias mutates cr in place to qualify a single-token bare column
// reference with alias. Already-qualified refs (≥ 2 fields) are left alone.
func prependAlias(cr *pg_query.ColumnRef, alias string) {
	if len(cr.Fields) != 1 {
		return
	}
	s, ok := cr.Fields[0].Node.(*pg_query.Node_String_)
	if !ok || s.String_ == nil {
		return
	}
	cr.Fields = []*pg_query.Node{
		{Node: &pg_query.Node_String_{String_: &pg_query.String{Sval: alias}}},
		{Node: &pg_query.Node_String_{String_: &pg_query.String{Sval: s.String_.Sval}}},
	}
}

// exprChildren returns the expression children of a pg_query node that may
// contain column references inside a WHERE-clause expression. The list is
// intentionally narrow: it covers comparison/boolean/list/function shapes that
// PostgreSQL's grammar admits inside a predicate, and skips statement-level or
// schema-level constructs.
func exprChildren(node *pg_query.Node) []*pg_query.Node {
	if children := comparisonChildren(node); children != nil {
		return children
	}
	if children := callOrCastChildren(node); children != nil {
		return children
	}
	return listOrSubChildren(node)
}

// comparisonChildren covers binary/boolean/null/boolean-test predicates.
func comparisonChildren(node *pg_query.Node) []*pg_query.Node {
	switch n := node.Node.(type) {
	case *pg_query.Node_AExpr:
		if n.AExpr != nil {
			return []*pg_query.Node{n.AExpr.Lexpr, n.AExpr.Rexpr}
		}
	case *pg_query.Node_BoolExpr:
		if n.BoolExpr != nil {
			return n.BoolExpr.Args
		}
	case *pg_query.Node_NullTest:
		if n.NullTest != nil {
			return []*pg_query.Node{n.NullTest.Arg}
		}
	case *pg_query.Node_BooleanTest:
		if n.BooleanTest != nil {
			return []*pg_query.Node{n.BooleanTest.Arg}
		}
	}
	return nil
}

// callOrCastChildren covers function calls, casts, and CASE forms.
func callOrCastChildren(node *pg_query.Node) []*pg_query.Node {
	switch n := node.Node.(type) {
	case *pg_query.Node_FuncCall:
		if n.FuncCall != nil {
			return n.FuncCall.Args
		}
	case *pg_query.Node_TypeCast:
		if n.TypeCast != nil {
			return []*pg_query.Node{n.TypeCast.Arg}
		}
	case *pg_query.Node_CaseExpr:
		if n.CaseExpr != nil {
			out := append([]*pg_query.Node{n.CaseExpr.Arg}, n.CaseExpr.Args...)
			return append(out, n.CaseExpr.Defresult)
		}
	case *pg_query.Node_CaseWhen:
		if n.CaseWhen != nil {
			return []*pg_query.Node{n.CaseWhen.Expr, n.CaseWhen.Result}
		}
	case *pg_query.Node_CoalesceExpr:
		if n.CoalesceExpr != nil {
			return n.CoalesceExpr.Args
		}
	case *pg_query.Node_MinMaxExpr:
		if n.MinMaxExpr != nil {
			return n.MinMaxExpr.Args
		}
	}
	return nil
}

// listOrSubChildren covers list-shaped constructs (IN, arrays, rows) and the
// indirection/sublink forms.
func listOrSubChildren(node *pg_query.Node) []*pg_query.Node {
	switch n := node.Node.(type) {
	case *pg_query.Node_List:
		if n.List != nil {
			return n.List.Items
		}
	case *pg_query.Node_AArrayExpr:
		if n.AArrayExpr != nil {
			return n.AArrayExpr.Elements
		}
	case *pg_query.Node_RowExpr:
		if n.RowExpr != nil {
			return n.RowExpr.Args
		}
	case *pg_query.Node_AIndirection:
		if n.AIndirection != nil {
			return []*pg_query.Node{n.AIndirection.Arg}
		}
	case *pg_query.Node_SubLink:
		if n.SubLink != nil {
			return []*pg_query.Node{n.SubLink.Testexpr}
		}
	}
	return nil
}

// deparseWhereExpr serializes a WHERE-clause expression node back to SQL text
// by wrapping it as the value of a SELECT target and stripping the SELECT
// prefix. Mirrors the trick used by deparseExpr in postgres.go.
func deparseWhereExpr(expr *pg_query.Node) (string, error) {
	tree := &pg_query.ParseResult{
		Stmts: []*pg_query.RawStmt{
			{
				Stmt: &pg_query.Node{
					Node: &pg_query.Node_SelectStmt{
						SelectStmt: &pg_query.SelectStmt{
							TargetList: []*pg_query.Node{
								{
									Node: &pg_query.Node_ResTarget{
										ResTarget: &pg_query.ResTarget{
											Val: expr,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	out, err := pg_query.Deparse(tree)
	if err != nil {
		return "", fmt.Errorf("pg_query deparse: %w", err)
	}
	return strings.TrimPrefix(out, "SELECT "), nil
}
