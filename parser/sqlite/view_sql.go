package sqlite

import (
	"fmt"
	"slices"
	"strings"

	rsql "github.com/rqlite/sql"

	"github.com/teandresmith/sqlgen/parser"
)

// ParseViewSQL parses a CREATE VIEW statement using rqlite/sql and extracts
// the view name, SELECT column metadata, and FROM clause table aliases.
func ParseViewSQL(sql []byte) (parser.ViewSQL, error) {
	// Fail with a clear dialect error instead of an opaque syntax error from
	// rqlite/sql — materialized views are PostgreSQL-only.
	if createsMaterializedView(string(sql)) {
		return parser.ViewSQL{}, fmt.Errorf("CREATE MATERIALIZED VIEW is not supported for dialect sqlite: materialized views are PostgreSQL-only")
	}

	rp := rsql.NewParser(strings.NewReader(string(sql)))
	stmts, err := rp.ParseStatements()
	if err != nil {
		return parser.ViewSQL{}, fmt.Errorf("parsing view SQL: %w", err)
	}

	for _, stmt := range stmts {
		cv, ok := stmt.(*rsql.CreateViewStatement)
		if !ok {
			continue
		}
		return parseCreateViewStmt(cv)
	}

	return parser.ViewSQL{}, fmt.Errorf("no CREATE VIEW statement found")
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

func parseCreateViewStmt(cv *rsql.CreateViewStatement) (parser.ViewSQL, error) {
	name := rsql.IdentName(cv.Name)
	sel := cv.Select
	if sel == nil {
		return parser.ViewSQL{}, &parser.ViewQueryError{Name: name, Err: fmt.Errorf("CREATE VIEW has no SELECT statement")}
	}
	// A compound SELECT (UNION, INTERSECT, EXCEPT) has no single column list
	// whose columns can be traced to a table.
	if sel.Compound != nil {
		return parser.ViewSQL{}, &parser.ViewQueryError{Name: name, Err: fmt.Errorf("CREATE VIEW query is not a simple SELECT")}
	}

	def, err := extractSourceAliases(sel)
	if err != nil {
		return parser.ViewSQL{}, &parser.ViewQueryError{Name: name, Err: err}
	}

	columns, err := extractResultColumns(name, sel.Columns, cv.Columns)
	if err != nil {
		return parser.ViewSQL{}, &parser.ViewQueryError{Name: name, Err: err}
	}

	def.Name, def.Columns = name, columns
	return def, nil
}

// extractSourceAliases reads sel's FROM and WITH clauses into a ViewSQL's
// TableAliases, OuterAliases, QualifiedAliases, Subqueries and CTEs (each
// of the last three nil when it is empty), and the columns sel reads
// outside its select list into Reads and Nested (queryReads).
func extractSourceAliases(sel *rsql.SelectStatement) (parser.ViewSQL, error) {
	from := parser.ViewSQL{
		TableAliases: make(map[string]string),
		OuterAliases: make(map[string]bool),
	}
	if sel.Source != nil {
		if err := collectSourceAliases(sel.Source, false, &from); err != nil {
			return parser.ViewSQL{}, err
		}
	}
	if sel.WithClause != nil && len(sel.WithClause.CTEs) > 0 {
		from.CTEs = make(map[string]parser.ViewSQL, len(sel.WithClause.CTEs))
		for _, cte := range sel.WithClause.CTEs {
			from.CTEs[rsql.IdentName(cte.TableName)] = readSubquery(cte.Select, cte.Columns)
		}
	}
	from.Reads, from.Nested = queryReads(sel)
	return from, nil
}

// queryReads returns the columns sel reads other than as a bare column of
// its select list, and the subqueries in its expressions, for
// parser.ViewSQL.Reads and Nested.
func queryReads(sel *rsql.SelectStatement) ([]parser.ViewColumnRef, []parser.ViewSQL) {
	r := &readCollector{skip: make(map[*rsql.Ident]bool)}
	aliases := make(map[string]exprKind)
	for _, rc := range sel.Columns {
		// SQLite binds a name to the first alias it matches.
		if name := parser.FoldASCII(rsql.IdentName(rc.Alias)); rc.Alias != nil && aliases[name] == 0 {
			aliases[name] = kindOf(rc.Expr)
		}
		switch rc.Expr.(type) {
		case *rsql.Ident, *rsql.QualifiedRef, nil:
		default:
			r.walk(rc.Expr)
		}
	}
	if sel.Source != nil {
		r.walkJoins(sel.Source)
	}
	r.walk(sel.WhereExpr)
	// SQLite binds a name in GROUP BY, HAVING or an ORDER BY expression to
	// a FROM-clause column first and to a select-list alias of that name
	// once no such column is left, so a read an alias can stand in for
	// records the alias (ViewColumnRef.Alias): dropping the column leaves
	// the view running, unless a rename has given the name another one.
	for _, e := range sel.GroupByExprs {
		r.walkAliased(e, aliases, groupByClause)
	}
	r.walkAliased(sel.HavingExpr, aliases, havingClause)
	for _, w := range sel.Windows {
		r.walk(w)
	}
	for _, term := range sel.OrderingTerms {
		// A bare ORDER BY name is the select-list column of that name first.
		if id, ok := term.X.(*rsql.Ident); ok && aliases[parser.FoldASCII(id.Name)] != 0 {
			continue
		}
		r.walkAliased(term.X, aliases, orderByClause)
	}
	return r.reads, r.nested
}

// exprKind is what an expression computes, which decides where SQLite
// lets a select-list alias of it stand in for a name: a scalar one in
// GROUP BY, HAVING and ORDER BY; an aggregate one in HAVING and ORDER BY
// outside an aggregate call; a window one in ORDER BY outside an aggregate
// or window call. Elsewhere SQLite fails with "aggregate functions are not
// allowed in the GROUP BY clause", "misuse of aliased aggregate" or
// "misuse of aliased window function".
type exprKind int

const (
	scalarExpr exprKind = iota + 1
	aggregateExpr
	windowExpr
)

// kindOf returns what e computes. An aggregate or window call inside a
// subquery in e counts too, since one that reads only e's query's columns
// aggregates that query.
func kindOf(e rsql.Expr) exprKind {
	v := &exprKindVisitor{kind: scalarExpr}
	if e != nil {
		// The visitor never returns an error.
		_, _ = rsql.Walk(v, e)
	}
	return v.kind
}

// exprKindVisitor finds the calls in an expression for kindOf.
type exprKindVisitor struct{ kind exprKind }

// Visit records what a call computes.
func (v *exprKindVisitor) Visit(n rsql.Node) (rsql.Visitor, rsql.Node, error) {
	if c, ok := n.(*rsql.Call); ok {
		v.kind = max(v.kind, callKind(c))
	}
	return v, n, nil
}

// VisitEnd returns n unchanged.
func (v *exprKindVisitor) VisitEnd(n rsql.Node) (rsql.Node, error) { return n, nil }

// callKind returns what c computes: a window call has OVER; an aggregate
// call has FILTER, * or DISTINCT, or calls one of SQLite's built-in
// aggregate functions (min and max only with a single argument; with more
// they are scalar).
func callKind(c *rsql.Call) exprKind {
	if c.Over != nil {
		return windowExpr
	}
	if c.Filter != nil || c.Star.IsValid() || c.Distinct.IsValid() {
		return aggregateExpr
	}
	switch parser.FoldASCII(rsql.IdentName(c.Name)) {
	case "avg", "count", "group_concat", "string_agg", "sum", "total",
		"json_group_array", "json_group_object", "jsonb_group_array", "jsonb_group_object":
		return aggregateExpr
	case "min", "max":
		if len(c.Args) == 1 {
			return aggregateExpr
		}
	}
	return scalarExpr
}

// readNested reads a subquery in an expression for parser.ViewSQL.Nested:
// the columns its select list reads go to its Reads, since no view column
// reads them.
func readNested(sel *rsql.SelectStatement) parser.ViewSQL {
	q := readSubquery(sel, nil)
	for _, c := range q.Columns {
		if c.SourceColumn != "" && c.SourceColumn != "*" {
			q.Reads = append(q.Reads, parser.ViewColumnRef{Table: c.SourceTable, Column: c.SourceColumn})
		}
	}
	q.Columns = nil
	return q
}

// readCollector collects the column references and subqueries of
// expressions. The walk reaches every identifier, so the ones that name
// something other than a column are marked to skip when their parent is
// visited.
type readCollector struct {
	reads  []parser.ViewColumnRef
	nested []parser.ViewSQL
	skip   map[*rsql.Ident]bool
	// aliases holds the query's select-list aliases while walkAliased
	// walks clause, and is nil otherwise; calls counts the aggregate and
	// window calls the walk is inside, by kind.
	aliases map[string]exprKind
	clause  aliasClause
	calls   [windowExpr + 1]int
}

// aliasClause is a clause where a select-list alias can stand in for a
// name.
type aliasClause int

const (
	groupByClause aliasClause = iota + 1
	havingClause
	orderByClause
)

// walkAliased walks n, an expression of clause, as walk does, where the
// select-list aliases can stand in for a name.
func (r *readCollector) walkAliased(n rsql.Node, aliases map[string]exprKind, clause aliasClause) {
	r.aliases, r.clause = aliases, clause
	r.walk(n)
	r.aliases, r.clause = nil, 0
}

// standsIn reports whether a select-list alias can stand in for the name
// id where the walk is (exprKind).
func (r *readCollector) standsIn(id *rsql.Ident) bool {
	inAggregate := r.calls[aggregateExpr] > 0
	switch r.aliases[parser.FoldASCII(rsql.IdentName(id))] {
	case scalarExpr:
		return true
	case aggregateExpr:
		return r.clause != groupByClause && !inAggregate
	case windowExpr:
		return r.clause == orderByClause && !inAggregate && r.calls[windowExpr] == 0
	}
	return false
}

func (r *readCollector) walk(n rsql.Node) {
	if n == nil {
		return
	}
	// The visitor never returns an error.
	_, _ = rsql.Walk(r, n)
}

// Visit records a column reference and reads a subquery, without entering
// either.
func (r *readCollector) Visit(n rsql.Node) (rsql.Visitor, rsql.Node, error) {
	switch n := n.(type) {
	case rsql.SelectExpr:
		r.nested = append(r.nested, readNested(n.SelectStatement))
		return nil, n, nil
	case *rsql.Exists:
		r.nested = append(r.nested, readNested(n.Select))
		return nil, n, nil
	case *rsql.QualifiedRef:
		if n.Table != nil && n.Column != nil {
			r.reads = append(r.reads, parser.ViewColumnRef{Table: rsql.IdentName(n.Table), Column: rsql.IdentName(n.Column)})
		}
		return nil, n, nil
	case *rsql.Ident:
		if !r.skip[n] && timeKeyword(n) == rsql.Expr(n) {
			ref := parser.ViewColumnRef{Column: rsql.IdentName(n)}
			if r.standsIn(n) {
				ref.Alias = parser.FoldASCII(ref.Column)
			}
			r.reads = append(r.reads, ref)
		}
	case *rsql.Call:
		r.enterCall(n)
	case *rsql.Type:
		r.skip[n.Name] = true
	case *rsql.CollateExpr:
		if n.Collation != nil {
			r.skip[n.Collation.Name] = true
		}
	case *rsql.Window:
		r.skip[n.Name] = true
	}
	return r, n, nil
}

// VisitEnd leaves a call, and returns n unchanged.
func (r *readCollector) VisitEnd(n rsql.Node) (rsql.Node, error) {
	if c, ok := n.(*rsql.Call); ok {
		r.calls[callKind(c)]--
	}
	return n, nil
}

// enterCall marks the names of a call that are not columns, its function's
// and its window's, and counts it in calls.
func (r *readCollector) enterCall(c *rsql.Call) {
	r.calls[callKind(c)]++
	r.skip[c.Name] = true
	if c.Over != nil {
		r.skip[c.Over.Name] = true
	}
}

// walkJoins records the columns the join constraints of a FROM-clause
// source read, an ON expression and each USING column, read through every
// FROM-clause key on both sides, and the columns the arguments of a table
// function read.
func (r *readCollector) walkJoins(src rsql.Source) {
	switch s := src.(type) {
	case *rsql.JoinClause:
		r.walkJoins(s.X)
		r.walkJoins(s.Y)
		switch c := s.Constraint.(type) {
		case *rsql.OnConstraint:
			r.walk(c.X)
		case *rsql.UsingConstraint:
			for _, u := range c.Columns {
				for _, key := range slices.Concat(fromKeys(s.X), fromKeys(s.Y)) {
					r.reads = append(r.reads, parser.ViewColumnRef{Table: key, Column: rsql.IdentName(u), Using: true})
				}
			}
		}
	case *rsql.ParenSource:
		if _, ok := s.X.(*rsql.SelectStatement); !ok && s.X != nil {
			r.walkJoins(s.X)
		}
	case *rsql.QualifiedTableFunctionName:
		for _, arg := range s.Args {
			r.walk(arg)
		}
	}
}

// fromKeys returns the keys a FROM-clause source is read by: a table's
// alias or name, a derived table's alias, or those of each source a join
// joins.
func fromKeys(src rsql.Source) []string {
	switch s := src.(type) {
	case *rsql.QualifiedTableName:
		if s.Alias != nil {
			return []string{rsql.IdentName(s.Alias)}
		}
		return []string{rsql.IdentName(s.Name)}
	case *rsql.ParenSource:
		if _, ok := s.X.(*rsql.SelectStatement); ok {
			if s.Alias != nil {
				return []string{rsql.IdentName(s.Alias)}
			}
			return nil
		}
		return fromKeys(s.X)
	case *rsql.JoinClause:
		return slices.Concat(fromKeys(s.X), fromKeys(s.Y))
	}
	return nil
}

// readSubquery reads the query of a derived table or common table
// expression for parser.ViewSQL.Subqueries or CTEs, its columns named by
// names where it has them. It returns an empty query for one it cannot
// read: a compound one, one with a RIGHT or FULL join, or one that names *
// alongside a column list.
func readSubquery(sel *rsql.SelectStatement, names []*rsql.Ident) parser.ViewSQL {
	if sel == nil || sel.Compound != nil {
		return parser.ViewSQL{}
	}
	columns := make([]parser.ViewSelectColumn, 0, len(sel.Columns))
	for i, rc := range sel.Columns {
		col := extractResultColumn(rc)
		if isStar(rc) {
			if len(names) > 0 {
				return parser.ViewSQL{}
			}
			col = parser.ViewSelectColumn{SourceColumn: "*"}
			if ref, ok := rc.Expr.(*rsql.QualifiedRef); ok && ref.Table != nil {
				col.SourceTable = rsql.IdentName(ref.Table)
			}
		}
		if i < len(names) {
			col.Alias, col.Unaliased = rsql.IdentName(names[i]), false
		}
		columns = append(columns, col)
	}
	q, err := extractSourceAliases(sel)
	if err != nil {
		return parser.ViewSQL{}
	}
	q.Columns = columns
	return q
}

// extractResultColumns converts rqlite ResultColumns into ViewSelectColumns,
// named by the view's column list where it has one; SQLite requires the list
// to name every column. Every column must have a name (PRD §16.1), and * is
// refused, since the parser cannot list the columns it selects.
func extractResultColumns(view string, cols []*rsql.ResultColumn, names []*rsql.Ident) ([]parser.ViewSelectColumn, error) {
	result := make([]parser.ViewSelectColumn, 0, len(cols))
	for i, rc := range cols {
		if isStar(rc) {
			return nil, &parser.ViewParseError{
				Reason: fmt.Sprintf("view %s selects %s, whose columns the view parser cannot list", view, rc.String()),
				Advice: "name each column",
			}
		}
		col := extractResultColumn(rc)
		if i < len(names) {
			col.Alias, col.Unaliased = rsql.IdentName(names[i]), false
		}
		if col.Alias == "" {
			// Name the column by position when rqlite/sql cannot render
			// the expression.
			expr, err := exprString(rc.Expr, "view column")
			if err != nil {
				expr = fmt.Sprintf("column %d", i+1)
			}
			return nil, &parser.ViewParseError{
				Reason: fmt.Sprintf("view %s selects %s with no alias and no column name", view, expr),
				Advice: "name it with AS",
			}
		}
		result = append(result, col)
	}
	if len(names) > 0 && len(names) != len(result) {
		return nil, fmt.Errorf("view %s names %d columns in its column list but selects %d", view, len(names), len(result))
	}
	return result, nil
}

// isStar reports whether rc selects * or table.*.
func isStar(rc *rsql.ResultColumn) bool {
	if rc.Star.IsValid() {
		return true
	}
	ref, ok := rc.Expr.(*rsql.QualifiedRef)
	return ok && ref.Star.IsValid()
}

// extractResultColumn extracts a ViewSelectColumn from a single rqlite ResultColumn.
func extractResultColumn(rc *rsql.ResultColumn) parser.ViewSelectColumn {
	col := parser.ViewSelectColumn{}

	if rc.Alias != nil {
		col.Alias = rsql.IdentName(rc.Alias)
	}

	if rc.Expr == nil {
		return col
	}

	switch expr := rc.Expr.(type) {
	case *rsql.QualifiedRef:
		if expr.Table != nil {
			col.SourceTable = rsql.IdentName(expr.Table)
		}
		if expr.Column != nil {
			col.SourceColumn = rsql.IdentName(expr.Column)
		}
		if col.Alias == "" {
			col.Alias, col.Unaliased = col.SourceColumn, true
		}

	case *rsql.Ident:
		col.SourceColumn = rsql.IdentName(expr)
		if col.Alias == "" {
			col.Alias, col.Unaliased = col.SourceColumn, true
		}

	case *rsql.Call:
		funcName := strings.ToUpper(rsql.IdentName(expr.Name))
		if parser.IsAggregateFunction(funcName) {
			col.Aggregate = funcName
			if len(expr.Args) > 0 {
				extractCallArg(expr.Args[0], &col)
			}
		}
	}

	return col
}

// extractCallArg extracts source table/column from a function argument.
func extractCallArg(arg rsql.Expr, col *parser.ViewSelectColumn) {
	switch a := arg.(type) {
	case *rsql.QualifiedRef:
		if a.Table != nil {
			col.SourceTable = rsql.IdentName(a.Table)
		}
		if a.Column != nil {
			col.SourceColumn = rsql.IdentName(a.Column)
		}
	case *rsql.Ident:
		col.SourceColumn = rsql.IdentName(a)
	}
}

// collectSourceAliases recursively collects table aliases and derived
// tables from a FROM clause Source into from. isOuter marks a source on the
// outer side of an enclosing join.
func collectSourceAliases(src rsql.Source, isOuter bool, from *parser.ViewSQL) error {
	switch s := src.(type) {
	case *rsql.QualifiedTableName:
		key := rsql.IdentName(s.Name)
		if s.Alias != nil {
			key = rsql.IdentName(s.Alias)
		}
		addTableAlias(from, key, rsql.IdentName(s.Name), isOuter, s.Schema != nil)

	case *rsql.JoinClause:
		if s.Operator != nil && (s.Operator.Right.IsValid() || s.Operator.Full.IsValid()) {
			return &parser.ViewParseError{
				Reason: "CREATE VIEW uses a RIGHT or FULL join",
				Advice: "RIGHT and FULL joins are not supported",
			}
		}
		if err := collectSourceAliases(s.X, isOuter, from); err != nil {
			return err
		}
		return collectSourceAliases(s.Y, isOuter || (s.Operator != nil && s.Operator.Left.IsValid()), from)

	case *rsql.ParenSource:
		if sel, ok := s.X.(*rsql.SelectStatement); ok {
			addDerivedTable(sel, s.Alias, from)
			return nil
		}
		if s.X != nil {
			return collectSourceAliases(s.X, isOuter, from)
		}
	}
	return nil
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

// addDerivedTable records the query of a derived table under its alias in
// from, as readSubquery reads it, when it has an alias.
func addDerivedTable(sel *rsql.SelectStatement, alias *rsql.Ident, from *parser.ViewSQL) {
	if alias == nil {
		return
	}
	if from.Subqueries == nil {
		from.Subqueries = make(map[string]parser.ViewSQL)
	}
	from.Subqueries[rsql.IdentName(alias)] = readSubquery(sel, nil)
}
