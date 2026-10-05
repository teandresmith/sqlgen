package mysql

import (
	"fmt"
	"strings"

	"vitess.io/vitess/go/vt/sqlparser"
)

// QualifyFilterIdentifiers rewrites every bare column reference inside expr to
// be qualified with alias. Already-qualified refs (table.column) are left
// untouched. The expression is parsed as a MySQL WHERE clause and re-emitted
// via vitess/sqlparser, so backtick-quoted identifiers, dialect operators
// (e.g. `:=`), CAST/CASE/function-call expressions, and string-literal
// contents are all handled by the upstream parser.
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

	parser, err := sqlparser.New(sqlparser.Options{})
	if err != nil {
		return "", fmt.Errorf("qualify filter: creating mysql parser: %w", err)
	}

	wrapped := "SELECT 1 FROM dual WHERE " + expr
	stmt, err := parser.Parse(wrapped)
	if err != nil {
		return "", fmt.Errorf("qualify filter: parsing %q: %w", expr, err)
	}

	sel, ok := stmt.(*sqlparser.Select)
	if !ok || sel.Where == nil {
		return "", fmt.Errorf("qualify filter: no WHERE clause parsed from %q", expr)
	}

	qualifierID := sqlparser.NewIdentifierCS(alias)
	rewritten := sqlparser.Rewrite(sel.Where.Expr, func(c *sqlparser.Cursor) bool {
		if cn, ok := c.Node().(*sqlparser.ColName); ok {
			if cn.Qualifier.Name.IsEmpty() {
				cn.Qualifier = sqlparser.TableName{Name: qualifierID}
			}
		}
		return true
	}, nil)

	exprNode, ok := rewritten.(sqlparser.Expr)
	if !ok {
		return "", fmt.Errorf("qualify filter: rewriter returned non-expression for %q", expr)
	}
	return sqlparser.String(exprNode), nil
}
