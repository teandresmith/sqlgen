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

	// MySQL names are always bare (PRD §5.5): the qualifier is dropped so the
	// file's view keys the same as the database view it overlays.
	name := cv.ViewName.Name.String()
	sel, ok := cv.Select.(*sqlparser.Select)
	if !ok {
		return parser.ViewSQL{}, &parser.ViewQueryError{Name: name, Err: fmt.Errorf("CREATE VIEW query is not a simple SELECT")}
	}

	columns, err := extractSelectExprs(name, sel.SelectExprs.Exprs, cv.Columns)
	if err != nil {
		return parser.ViewSQL{}, &parser.ViewQueryError{Name: name, Err: err}
	}

	aliases, outer := extractTableAliases(sel.From)

	return parser.ViewSQL{
		Name:         name,
		Columns:      columns,
		TableAliases: aliases,
		OuterAliases: outer,
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

// extractSelectExprs converts vitess SelectExpr slices into ViewSelectColumns,
// named by the view's column list where it has one; MySQL requires the list
// to name every column. Every column must have a name (PRD §16.1), and * is
// refused, since the parser cannot list the columns it selects.
func extractSelectExprs(view string, exprs []sqlparser.SelectExpr, names sqlparser.Columns) ([]parser.ViewSelectColumn, error) {
	columns := make([]parser.ViewSelectColumn, 0, len(exprs))
	for i, expr := range exprs {
		ae, ok := expr.(*sqlparser.AliasedExpr)
		if !ok {
			return nil, &parser.ViewParseError{
				Reason: fmt.Sprintf("view %s selects %s, whose columns the view parser cannot list", view, sqlparser.String(expr)),
				Advice: "name each column",
			}
		}
		col := extractAliasedExpr(ae)
		if i < len(names) {
			col.Alias = names[i].String()
		}
		if col.Alias == "" {
			return nil, &parser.ViewParseError{
				Reason: fmt.Sprintf("view %s selects %s with no alias and no column name", view, sqlparser.String(ae.Expr)),
				Advice: "name it with AS",
			}
		}
		columns = append(columns, col)
	}
	if len(names) > 0 && len(names) != len(columns) {
		return nil, fmt.Errorf("view %s names %d columns in its column list but selects %d", view, len(names), len(columns))
	}
	return columns, nil
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

// extractTableAliases builds a map of table alias → table name from FROM
// clause, and the set of aliases on the outer side of a join.
func extractTableAliases(from []sqlparser.TableExpr) (aliases map[string]string, outer map[string]bool) {
	aliases = make(map[string]string)
	outer = make(map[string]bool)
	for _, te := range from {
		collectTableAliases(te, false, aliases, outer)
	}
	return aliases, outer
}

// collectTableAliases records te's table aliases. isOuter marks a table
// expression on the outer side of an enclosing join.
func collectTableAliases(te sqlparser.TableExpr, isOuter bool, aliases map[string]string, outer map[string]bool) {
	switch t := te.(type) {
	case *sqlparser.AliasedTableExpr:
		if tn, ok := t.Expr.(sqlparser.TableName); ok {
			key := tn.Name.String()
			if !t.As.IsEmpty() {
				key = t.As.String()
			}
			aliases[key] = tn.Name.String()
			if isOuter {
				outer[key] = true
			}
		}

	case *sqlparser.JoinTableExpr:
		left := isOuter || t.Join == sqlparser.RightJoinType || t.Join == sqlparser.NaturalRightJoinType
		right := isOuter || t.Join == sqlparser.LeftJoinType || t.Join == sqlparser.NaturalLeftJoinType
		collectTableAliases(t.LeftExpr, left, aliases, outer)
		collectTableAliases(t.RightExpr, right, aliases, outer)

	case *sqlparser.ParenTableExpr:
		for _, expr := range t.Exprs {
			collectTableAliases(expr, isOuter, aliases, outer)
		}
	}
}
