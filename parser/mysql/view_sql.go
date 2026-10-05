package mysql

import (
	"fmt"
	"strings"

	"vitess.io/vitess/go/vt/sqlparser"

	"github.com/teandresmith/sqlgen/parser"
)

// ParseViewSQL parses a CREATE VIEW statement using vitess/sqlparser and
// extracts the view name, SELECT column metadata, and FROM clause table aliases.
func ParseViewSQL(sql []byte) (parser.ViewSQL, error) {
	// Fail with a clear dialect error instead of an opaque syntax error from
	// vitess — materialized views are PostgreSQL-only.
	if createsMaterializedView(string(sql)) {
		return parser.ViewSQL{}, fmt.Errorf("CREATE MATERIALIZED VIEW is not supported for dialect mysql: materialized views are PostgreSQL-only")
	}

	vp, err := sqlparser.New(sqlparser.Options{})
	if err != nil {
		return parser.ViewSQL{}, fmt.Errorf("creating mysql parser: %w", err)
	}

	stmt, err := vp.ParseStrictDDL(string(sql))
	if err != nil {
		return parser.ViewSQL{}, fmt.Errorf("parsing view SQL: %w", err)
	}

	cv, ok := stmt.(*sqlparser.CreateView)
	if !ok {
		return parser.ViewSQL{}, fmt.Errorf("no CREATE VIEW statement found")
	}

	sel, ok := cv.Select.(*sqlparser.Select)
	if !ok {
		return parser.ViewSQL{}, fmt.Errorf("CREATE VIEW query is not a simple SELECT")
	}

	var schema string
	if q := cv.ViewName.Qualifier; !q.IsEmpty() {
		schema = q.String()
	}

	return parser.ViewSQL{
		Name:         cv.ViewName.Name.String(),
		Schema:       schema,
		Columns:      extractSelectExprs(sel.SelectExprs.Exprs),
		TableAliases: extractTableAliases(sel.From),
	}, nil
}

// createsMaterializedView reports whether the first non-comment statement in
// sql begins with CREATE MATERIALIZED — PostgreSQL-only syntax this dialect
// must reject with a clear error (annotation files may lead with -- @directive
// comment lines, which are skipped).
func createsMaterializedView(sql string) bool {
	for line := range strings.SplitSeq(sql, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		fields := strings.Fields(line)
		firstIsCreate := len(fields) >= 2 && strings.EqualFold(fields[0], "CREATE")
		return firstIsCreate && strings.EqualFold(fields[1], "MATERIALIZED")
	}
	return false
}

// extractSelectExprs converts vitess SelectExpr slices into ViewSelectColumns.
func extractSelectExprs(exprs []sqlparser.SelectExpr) []parser.ViewSelectColumn {
	columns := make([]parser.ViewSelectColumn, 0, len(exprs))
	for _, expr := range exprs {
		ae, ok := expr.(*sqlparser.AliasedExpr)
		if !ok {
			continue
		}
		col := extractAliasedExpr(ae)
		if col.Alias != "" {
			columns = append(columns, col)
		}
	}
	return columns
}

// extractAliasedExpr extracts a ViewSelectColumn from a vitess AliasedExpr.
func extractAliasedExpr(ae *sqlparser.AliasedExpr) parser.ViewSelectColumn {
	col := parser.ViewSelectColumn{}

	if !ae.As.IsEmpty() {
		col.Alias = ae.As.String()
	}

	switch expr := ae.Expr.(type) {
	case *sqlparser.ColName:
		extractColName(expr, &col)
		if col.Alias == "" {
			col.Alias = col.SourceColumn
		}

	case *sqlparser.FuncExpr:
		funcName := strings.ToUpper(expr.Name.String())
		if parser.IsAggregateFunction(funcName) {
			col.Aggregate = funcName
			extractFirstColNameArg(expr.Exprs, &col)
		}

	case *sqlparser.CountStar:
		col.Aggregate = "COUNT"

	case sqlparser.AggrFunc:
		funcName := strings.ToUpper(expr.AggrName())
		if parser.IsAggregateFunction(funcName) {
			col.Aggregate = funcName
			extractFirstColNameArg(expr.GetArgs(), &col)
		}
	}

	return col
}

// extractColName populates source table/column from a vitess ColName.
func extractColName(cn *sqlparser.ColName, col *parser.ViewSelectColumn) {
	col.SourceColumn = cn.Name.String()
	if !cn.Qualifier.Name.IsEmpty() {
		col.SourceTable = cn.Qualifier.Name.String()
	}
}

// extractFirstColNameArg finds the first ColName in an expression list and
// populates source table/column from it.
func extractFirstColNameArg(args []sqlparser.Expr, col *parser.ViewSelectColumn) {
	for _, arg := range args {
		if cn, ok := arg.(*sqlparser.ColName); ok {
			extractColName(cn, col)
			return
		}
	}
}

// extractTableAliases builds a map of table alias → table name from FROM clause.
func extractTableAliases(from []sqlparser.TableExpr) map[string]string {
	aliases := make(map[string]string)
	for _, te := range from {
		collectTableAliases(te, aliases)
	}
	return aliases
}

func collectTableAliases(te sqlparser.TableExpr, aliases map[string]string) {
	switch t := te.(type) {
	case *sqlparser.AliasedTableExpr:
		if tn, ok := t.Expr.(sqlparser.TableName); ok {
			tableName := tn.Name.String()
			if !t.As.IsEmpty() {
				aliases[t.As.String()] = tableName
			} else {
				aliases[tableName] = tableName
			}
		}

	case *sqlparser.JoinTableExpr:
		collectTableAliases(t.LeftExpr, aliases)
		collectTableAliases(t.RightExpr, aliases)

	case *sqlparser.ParenTableExpr:
		for _, expr := range t.Exprs {
			collectTableAliases(expr, aliases)
		}
	}
}
