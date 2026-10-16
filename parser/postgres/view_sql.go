package postgres

import (
	"cmp"
	"fmt"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/teandresmith/sqlgen/parser"
)

// ParseViewSQL parses a CREATE VIEW or CREATE MATERIALIZED VIEW statement
// using pg_query and extracts the view name, schema, SELECT column metadata,
// and FROM clause table aliases. CREATE OR REPLACE MATERIALIZED VIEW is not
// valid PostgreSQL and fails at pg_query.Parse.
//
// An unqualified view name resolves to defaultSchema, the schema New gives an
// unqualified CREATE TABLE (input.schema, PRD §5.5); an empty defaultSchema
// means "public". Every PostgreSQL view therefore carries a schema, as every
// table does.
func ParseViewSQL(sql []byte, defaultSchema string) (parser.ViewSQL, error) {
	result, err := pg_query.Parse(string(sql))
	if err != nil {
		return parser.ViewSQL{}, fmt.Errorf("parsing view SQL: %w", err)
	}
	if defaultSchema == "" {
		defaultSchema = "public"
	}

	for _, rawStmt := range result.Stmts {
		switch n := rawStmt.Stmt.Node.(type) {
		case *pg_query.Node_ViewStmt:
			return parseViewStmt(n.ViewStmt, defaultSchema)
		case *pg_query.Node_CreateTableAsStmt:
			// CREATE MATERIALIZED VIEW parses as CreateTableAsStmt with a
			// matview object type; plain CREATE TABLE AS is not a view.
			if n.CreateTableAsStmt.Objtype == pg_query.ObjectType_OBJECT_MATVIEW {
				return parseMatviewStmt(n.CreateTableAsStmt, defaultSchema)
			}
		}
	}

	return parser.ViewSQL{}, fmt.Errorf("no CREATE VIEW statement found")
}

func parseViewStmt(vs *pg_query.ViewStmt, defaultSchema string) (parser.ViewSQL, error) {
	schema := cmp.Or(vs.View.Schemaname, defaultSchema)
	selectStmt, ok := vs.Query.Node.(*pg_query.Node_SelectStmt)
	if !ok {
		return parser.ViewSQL{}, &parser.ViewQueryError{Schema: schema, Name: vs.View.Relname, Err: fmt.Errorf("CREATE VIEW query is not a SELECT statement")}
	}
	sel := selectStmt.SelectStmt
	// A UNION, INTERSECT or EXCEPT has no single SELECT list whose columns
	// can be traced to a table.
	if sel.Op != pg_query.SetOperation_SETOP_NONE {
		return parser.ViewSQL{}, &parser.ViewQueryError{Schema: schema, Name: vs.View.Relname, Err: fmt.Errorf("CREATE VIEW query is not a simple SELECT")}
	}

	columns, err := extractResTargets(vs.View.Relname, sel.TargetList, vs.Aliases)
	if err != nil {
		return parser.ViewSQL{}, &parser.ViewQueryError{Schema: schema, Name: vs.View.Relname, Err: err}
	}

	def := extractFromAliases(sel, defaultSchema)
	def.Name, def.Schema, def.Columns = vs.View.Relname, schema, columns
	return def, nil
}

func parseMatviewStmt(cs *pg_query.CreateTableAsStmt, defaultSchema string) (parser.ViewSQL, error) {
	schema := cmp.Or(cs.Into.Rel.Schemaname, defaultSchema)
	selectStmt, ok := cs.Query.Node.(*pg_query.Node_SelectStmt)
	if !ok {
		return parser.ViewSQL{}, &parser.ViewQueryError{Schema: schema, Name: cs.Into.Rel.Relname, Err: fmt.Errorf("CREATE MATERIALIZED VIEW query is not a SELECT statement")}
	}
	sel := selectStmt.SelectStmt
	// A UNION, INTERSECT or EXCEPT has no single SELECT list whose columns
	// can be traced to a table.
	if sel.Op != pg_query.SetOperation_SETOP_NONE {
		return parser.ViewSQL{}, &parser.ViewQueryError{Schema: schema, Name: cs.Into.Rel.Relname, Err: fmt.Errorf("CREATE MATERIALIZED VIEW query is not a simple SELECT")}
	}

	columns, err := extractResTargets(cs.Into.Rel.Relname, sel.TargetList, cs.Into.ColNames)
	if err != nil {
		return parser.ViewSQL{}, &parser.ViewQueryError{Schema: schema, Name: cs.Into.Rel.Relname, Err: err}
	}

	def := extractFromAliases(sel, defaultSchema)
	def.Name, def.Schema, def.Columns, def.Materialized = cs.Into.Rel.Relname, schema, columns, true
	return def, nil
}

// readSubquery reads the query of a derived table or common table
// expression for parser.ViewSQL.Subqueries or CTEs, its columns named by
// names where it has them. It returns an empty query for one it cannot
// read: one that is not a simple SELECT, or that names * alongside a
// column list.
func readSubquery(node *pg_query.Node, names []*pg_query.Node, defaultSchema string) parser.ViewSQL {
	sel := node.GetSelectStmt()
	if sel == nil || sel.Op != pg_query.SetOperation_SETOP_NONE {
		return parser.ViewSQL{}
	}
	var columns []parser.ViewSelectColumn
	for i, n := range sel.TargetList {
		rt := n.GetResTarget()
		if rt == nil {
			continue
		}
		col := extractColumn(rt)
		if isStar(rt) {
			if len(names) > 0 {
				return parser.ViewSQL{}
			}
			col = parser.ViewSelectColumn{SourceColumn: "*"}
			if fields := rt.Val.GetColumnRef().GetFields(); len(fields) == 2 {
				col.SourceTable = nodeString(fields[0])
			}
		}
		if i < len(names) {
			col.Alias = nodeString(names[i])
		}
		columns = append(columns, col)
	}
	q := extractFromAliases(sel, defaultSchema)
	q.Columns = columns
	return q
}

// extractResTargets converts pg_query ResTarget nodes into ViewSelectColumns,
// named by the view's column list where it has one. PostgreSQL lets the list
// name fewer columns than the SELECT has, and the rest keep their own names.
// Every column must have a name (PRD §16.1), and * is refused, since the
// parser cannot list the columns it selects.
func extractResTargets(view string, targets, names []*pg_query.Node) ([]parser.ViewSelectColumn, error) {
	columns := make([]parser.ViewSelectColumn, 0, len(targets))
	for i, node := range targets {
		rt, ok := node.Node.(*pg_query.Node_ResTarget)
		if !ok {
			continue
		}
		if isStar(rt.ResTarget) {
			return nil, &parser.ViewParseError{
				Reason: fmt.Sprintf("view %s selects %s, whose columns the view parser cannot list", view, exprSQL(rt.ResTarget)),
				Advice: "name each column",
			}
		}
		col := extractColumn(rt.ResTarget)
		if i < len(names) {
			col.Alias = nodeString(names[i])
		}
		if col.Alias == "" {
			return nil, &parser.ViewParseError{
				Reason: fmt.Sprintf("view %s selects %s with no alias and no column name", view, exprSQL(rt.ResTarget)),
				Advice: "name it with AS",
			}
		}
		columns = append(columns, col)
	}
	if len(names) > len(columns) {
		return nil, fmt.Errorf("view %s names %d columns in its column list but selects %d", view, len(names), len(columns))
	}
	return columns, nil
}

// isStar reports whether rt selects * or table.*.
func isStar(rt *pg_query.ResTarget) bool {
	cr, ok := rt.Val.GetNode().(*pg_query.Node_ColumnRef)
	if !ok {
		return false
	}
	for _, f := range cr.ColumnRef.Fields {
		if _, ok := f.Node.(*pg_query.Node_AStar); ok {
			return true
		}
	}
	return false
}

// exprSQL writes rt's expression as SQL, for an error message.
func exprSQL(rt *pg_query.ResTarget) string {
	sel := &pg_query.SelectStmt{
		TargetList:  []*pg_query.Node{{Node: &pg_query.Node_ResTarget{ResTarget: &pg_query.ResTarget{Val: rt.Val}}}},
		Op:          pg_query.SetOperation_SETOP_NONE,
		LimitOption: pg_query.LimitOption_LIMIT_OPTION_DEFAULT,
	}
	tree := &pg_query.ParseResult{Stmts: []*pg_query.RawStmt{{Stmt: &pg_query.Node{Node: &pg_query.Node_SelectStmt{SelectStmt: sel}}}}}
	out, err := pg_query.Deparse(tree)
	if err != nil {
		return "an expression"
	}
	return strings.TrimPrefix(out, "SELECT ")
}

// extractColumn extracts a ViewSelectColumn from a single ResTarget.
func extractColumn(rt *pg_query.ResTarget) parser.ViewSelectColumn {
	col := parser.ViewSelectColumn{}

	// Explicit alias from AS clause.
	if rt.Name != "" {
		col.Alias = rt.Name
	}

	if rt.Val == nil {
		return col
	}

	switch expr := rt.Val.Node.(type) {
	case *pg_query.Node_ColumnRef:
		ref := extractColumnRef(expr.ColumnRef)
		col.SourceTable = ref.SourceTable
		col.SourceColumn = ref.SourceColumn
		if col.Alias == "" {
			col.Alias = ref.SourceColumn
		}

	case *pg_query.Node_FuncCall:
		funcName := extractFuncName(expr.FuncCall)
		if parser.IsAggregateFunction(funcName) {
			col.Aggregate = funcName
			// Extract inner column reference for base type resolution.
			if len(expr.FuncCall.Args) > 0 {
				inner := extractInnerColumnRef(expr.FuncCall.Args[0])
				col.SourceTable = inner.SourceTable
				col.SourceColumn = inner.SourceColumn
			}
		}
	}

	return col
}

// extractColumnRef extracts table alias and column name from a ColumnRef.
func extractColumnRef(cr *pg_query.ColumnRef) parser.ViewSelectColumn {
	fields := cr.Fields
	switch len(fields) {
	case 1:
		if s := nodeString(fields[0]); s != "" {
			return parser.ViewSelectColumn{SourceColumn: s}
		}
	case 2:
		table := nodeString(fields[0])
		col := nodeString(fields[1])
		if table != "" && col != "" {
			return parser.ViewSelectColumn{SourceTable: table, SourceColumn: col}
		}
	}
	return parser.ViewSelectColumn{}
}

// extractInnerColumnRef tries to extract a column reference from a function argument.
func extractInnerColumnRef(node *pg_query.Node) parser.ViewSelectColumn {
	if node == nil {
		return parser.ViewSelectColumn{}
	}
	if cr, ok := node.Node.(*pg_query.Node_ColumnRef); ok {
		return extractColumnRef(cr.ColumnRef)
	}
	return parser.ViewSelectColumn{}
}

// extractFuncName returns the uppercase function name from a FuncCall.
func extractFuncName(fc *pg_query.FuncCall) string {
	var parts []string
	for _, n := range fc.Funcname {
		if s := nodeString(n); s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) > 0 {
		return strings.ToUpper(parts[len(parts)-1])
	}
	return ""
}

// extractFromAliases reads sel's FROM and WITH clauses into a ViewSQL's
// TableAliases, TableSchemas, OuterAliases, QualifiedAliases, Subqueries
// and CTEs (each of the last three nil when it is empty). An unqualified
// table is in defaultSchema, as an unqualified CREATE TABLE is.
func extractFromAliases(sel *pg_query.SelectStmt, defaultSchema string) parser.ViewSQL {
	from := parser.ViewSQL{
		TableAliases: make(map[string]string),
		TableSchemas: make(map[string]string),
		OuterAliases: make(map[string]bool),
	}
	for _, node := range sel.FromClause {
		collectFromAliases(node, defaultSchema, false, &from)
	}
	for _, node := range sel.GetWithClause().GetCtes() {
		cte := node.GetCommonTableExpr()
		if from.CTEs == nil {
			from.CTEs = make(map[string]parser.ViewSQL)
		}
		from.CTEs[cte.Ctename] = readSubquery(cte.GetCtequery(), cte.GetAliascolnames(), defaultSchema)
	}
	return from
}

// collectFromAliases recursively collects table aliases from FROM clause nodes
// into from (handles RangeVar for plain tables, RangeSubselect for derived
// tables and JoinExpr for JOINs). isOuter marks a node on the outer side of
// an enclosing join.
func collectFromAliases(node *pg_query.Node, defaultSchema string, isOuter bool, from *parser.ViewSQL) {
	if node == nil {
		return
	}

	switch n := node.Node.(type) {
	case *pg_query.Node_RangeVar:
		rv := n.RangeVar
		key := rv.Relname
		if rv.Alias != nil && rv.Alias.Aliasname != "" {
			key = rv.Alias.Aliasname
		}
		from.TableSchemas[key] = cmp.Or(rv.Schemaname, defaultSchema)
		addTableAlias(from, key, rv.Relname, isOuter, rv.Schemaname != "")

	case *pg_query.Node_JoinExpr:
		join := n.JoinExpr.Jointype
		left := isOuter || join == pg_query.JoinType_JOIN_RIGHT || join == pg_query.JoinType_JOIN_FULL
		right := isOuter || join == pg_query.JoinType_JOIN_LEFT || join == pg_query.JoinType_JOIN_FULL
		collectFromAliases(n.JoinExpr.Larg, defaultSchema, left, from)
		collectFromAliases(n.JoinExpr.Rarg, defaultSchema, right, from)

	case *pg_query.Node_RangeSubselect:
		rs := n.RangeSubselect
		if rs.Alias == nil || rs.Alias.Aliasname == "" {
			return
		}
		if from.Subqueries == nil {
			from.Subqueries = make(map[string]parser.ViewSQL)
		}
		sub := readSubquery(rs.Subquery, rs.Alias.Colnames, defaultSchema)
		sub.Lateral = rs.Lateral
		from.Subqueries[rs.Alias.Aliasname] = sub
	}
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

// nodeString extracts a string value from a pg_query Node.
func nodeString(node *pg_query.Node) string {
	if node == nil {
		return ""
	}
	if s, ok := node.Node.(*pg_query.Node_String_); ok {
		return s.String_.Sval
	}
	return ""
}
