package mysql

import (
	"fmt"
	"maps"
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

	def := extractTableAliases(sel.From)
	def.Name, def.Columns, def.CTEs = name, columns, readCTEs(sel.With)
	def.Reads, def.Nested = queryReads(sel)
	return def, nil
}

// readSubquery reads the query of a derived table or common table
// expression for parser.ViewSQL.Subqueries or CTEs, its columns named by
// names where it has them. The columns its select list reads are in its
// Reads too, since MySQL resolves each one whether or not the view selects
// it. A compound query is read as its leftmost arm, which names its
// columns, with the other arms in Compound. It returns an empty query for
// one it cannot read: one that is not a SELECT, or that names * alongside
// a column list.
func readSubquery(stmt sqlparser.TableStatement, names sqlparser.Columns) parser.ViewSQL {
	if u, ok := stmt.(*sqlparser.Union); ok {
		return readCompound(u, names)
	}
	sel, ok := stmt.(*sqlparser.Select)
	if !ok || sel.SelectExprs == nil {
		return parser.ViewSQL{}
	}
	var columns []parser.ViewSelectColumn
	for i, expr := range sel.SelectExprs.Exprs {
		var col parser.ViewSelectColumn
		switch e := expr.(type) {
		case *sqlparser.StarExpr:
			if len(names) > 0 {
				return parser.ViewSQL{}
			}
			col = parser.ViewSelectColumn{SourceTable: e.TableName.Name.String(), SourceColumn: "*"}
		case *sqlparser.AliasedExpr:
			col = extractAliasedExpr(e)
		default:
			return parser.ViewSQL{}
		}
		if i < len(names) {
			col.Alias = names[i].String()
		}
		columns = append(columns, col)
	}
	q := extractTableAliases(sel.From)
	q.Columns, q.CTEs = columns, readCTEs(sel.With)
	q.Reads, q.Nested = queryReads(sel)
	for _, c := range columns {
		if c.SourceColumn != "" && c.SourceColumn != "*" {
			q.Reads = append(q.Reads, parser.ViewColumnRef{Table: c.SourceTable, Column: c.SourceColumn})
		}
	}
	return q
}

// readCompound reads a compound query as readSubquery does.
func readCompound(u *sqlparser.Union, names sqlparser.Columns) parser.ViewSQL {
	arms := unionArms(u)
	q := readSubquery(arms[0], names)
	for _, arm := range arms[1:] {
		q.Compound = append(q.Compound, readNested(arm))
	}
	// The compound's own common table expressions are in scope in every arm.
	if ctes := readCTEs(u.With); ctes != nil {
		if q.CTEs == nil {
			q.CTEs = make(map[string]parser.ViewSQL, len(ctes))
		}
		maps.Copy(q.CTEs, ctes)
	}
	return q
}

// unionArms returns the arms of a compound query, leftmost first.
func unionArms(stmt sqlparser.TableStatement) []sqlparser.TableStatement {
	u, ok := stmt.(*sqlparser.Union)
	if !ok {
		return []sqlparser.TableStatement{stmt}
	}
	return append(unionArms(u.Left), unionArms(u.Right)...)
}

// readNested reads a subquery in an expression, or a further arm of a
// compound query, for parser.ViewSQL.Nested or Compound. It has no
// Columns, since no view column reads it.
func readNested(stmt sqlparser.TableStatement) parser.ViewSQL {
	q := readSubquery(stmt, nil)
	q.Columns = nil
	return q
}

// queryReads returns the columns sel reads other than as a bare column of
// its select list, and the subqueries in its expressions, for
// parser.ViewSQL.Reads and Nested.
func queryReads(sel *sqlparser.Select) ([]parser.ViewColumnRef, []parser.ViewSQL) {
	r := &readCollector{}
	aliases := make(map[string]bool)
	for _, expr := range sel.SelectExprs.Exprs {
		ae, ok := expr.(*sqlparser.AliasedExpr)
		if !ok {
			continue
		}
		if !ae.As.IsEmpty() {
			aliases[parser.FoldASCII(ae.As.String())] = true
		}
		if _, bare := ae.Expr.(*sqlparser.ColName); !bare {
			r.walk(ae.Expr)
		}
	}
	for _, te := range sel.From {
		r.walkJoins(te)
	}
	if sel.Where != nil {
		r.walk(sel.Where.Expr)
	}
	// MySQL stores a name in GROUP BY, HAVING or ORDER BY that it binds to
	// a table column qualified and one it binds to a select-list alias
	// bare, so a bare name there that an alias has is not a read. (It
	// stores an ORDER BY alias of the view's own query as the expression.)
	if sel.GroupBy != nil {
		for _, e := range sel.GroupBy.Exprs {
			r.walkAliased(e, aliases)
		}
	}
	if sel.Having != nil {
		r.walkAliased(sel.Having.Expr, aliases)
	}
	r.walk(sel.Windows)
	r.walkAliased(sel.OrderBy, aliases)
	return r.reads, r.nested
}

// readCollector collects the column references and subqueries of
// expressions.
type readCollector struct {
	reads  []parser.ViewColumnRef
	nested []parser.ViewSQL
	// aliases holds the query's select-list aliases while walkAliased
	// walks, and is nil otherwise.
	aliases map[string]bool
}

// walkAliased walks node as walk does, where a bare name can be one of the
// select-list aliases.
func (r *readCollector) walkAliased(node sqlparser.SQLNode, aliases map[string]bool) {
	r.aliases = aliases
	r.walk(node)
	r.aliases = nil
}

// walk records every column reference in node and reads every subquery in
// it.
func (r *readCollector) walk(node sqlparser.SQLNode) {
	// The visit function never returns an error.
	_ = sqlparser.Walk(func(n sqlparser.SQLNode) (bool, error) {
		switch n := n.(type) {
		case *sqlparser.ColName:
			if n.Qualifier.IsEmpty() && r.aliases[parser.FoldASCII(n.Name.String())] {
				return false, nil
			}
			r.reads = append(r.reads, parser.ViewColumnRef{Table: n.Qualifier.Name.String(), Column: n.Name.String()})
			return false, nil
		case *sqlparser.Subquery:
			r.nested = append(r.nested, readNested(n.Select))
			return false, nil
		}
		return true, nil
	}, node)
}

// walkJoins records the columns the ON expressions of a FROM-clause item
// read, and those the JSON_TABLE expression reads. MySQL stores a view's
// USING join as ON.
func (r *readCollector) walkJoins(te sqlparser.TableExpr) {
	switch t := te.(type) {
	case *sqlparser.JSONTableExpr:
		r.walk(t.Expr)
	case *sqlparser.JoinTableExpr:
		r.walkJoins(t.LeftExpr)
		r.walkJoins(t.RightExpr)
		if t.Condition != nil && t.Condition.On != nil {
			r.walk(t.Condition.On)
		}
	case *sqlparser.ParenTableExpr:
		for _, expr := range t.Exprs {
			r.walkJoins(expr)
		}
	}
}

// readCTEs returns the query of each common table expression of with, as
// readSubquery reads it, by its name (nil when there is none).
func readCTEs(with *sqlparser.With) map[string]parser.ViewSQL {
	if with == nil || len(with.CTEs) == 0 {
		return nil
	}
	ctes := make(map[string]parser.ViewSQL, len(with.CTEs))
	for _, cte := range with.CTEs {
		q := readSubquery(cte.Subquery, cte.Columns)
		q.Recursive = with.Recursive
		ctes[cte.ID.String()] = q
	}
	return ctes
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

// extractTableAliases reads a FROM clause into a ViewSQL's TableAliases,
// OuterAliases, QualifiedAliases and Subqueries (each of the last two nil
// when it is empty).
func extractTableAliases(tables []sqlparser.TableExpr) parser.ViewSQL {
	from := parser.ViewSQL{
		TableAliases: make(map[string]string),
		OuterAliases: make(map[string]bool),
	}
	for _, te := range tables {
		collectTableAliases(te, false, &from)
	}
	return from
}

// addTableAlias records in from that FROM-clause key reads table name,
// whether it is on the outer side of a join and whether name is qualified.
func addTableAlias(from *parser.ViewSQL, key, name string, isOuter, qualified bool) {
	from.TableAliases[key] = name
	if isOuter {
		from.OuterAliases[key] = true
	}
	if qualified {
		if from.QualifiedAliases == nil {
			from.QualifiedAliases = make(map[string]bool)
		}
		from.QualifiedAliases[key] = true
	}
}

// collectTableAliases records te's table aliases and derived tables in
// from. isOuter marks a table expression on the outer side of an enclosing
// join.
func collectTableAliases(te sqlparser.TableExpr, isOuter bool, from *parser.ViewSQL) {
	switch t := te.(type) {
	case *sqlparser.AliasedTableExpr:
		switch expr := t.Expr.(type) {
		case sqlparser.TableName:
			key := expr.Name.String()
			if !t.As.IsEmpty() {
				key = t.As.String()
			}
			addTableAlias(from, key, expr.Name.String(), isOuter, !expr.Qualifier.IsEmpty())
		case *sqlparser.DerivedTable:
			if t.As.IsEmpty() {
				return
			}
			if from.Subqueries == nil {
				from.Subqueries = make(map[string]parser.ViewSQL)
			}
			sub := readSubquery(expr.Select, t.Columns)
			sub.Lateral = expr.Lateral
			from.Subqueries[t.As.String()] = sub
		}

	case *sqlparser.JoinTableExpr:
		left := isOuter || t.Join == sqlparser.RightJoinType || t.Join == sqlparser.NaturalRightJoinType
		right := isOuter || t.Join == sqlparser.LeftJoinType || t.Join == sqlparser.NaturalLeftJoinType
		collectTableAliases(t.LeftExpr, left, from)
		collectTableAliases(t.RightExpr, right, from)

	case *sqlparser.ParenTableExpr:
		for _, expr := range t.Exprs {
			collectTableAliases(expr, isOuter, from)
		}
	}
}
