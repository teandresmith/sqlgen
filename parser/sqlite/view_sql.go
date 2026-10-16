package sqlite

import (
	"fmt"
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
// of the last three nil when it is empty).
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
	return from, nil
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
			col.Alias = rsql.IdentName(names[i])
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
			col.Alias = rsql.IdentName(names[i])
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
			col.Alias = col.SourceColumn
		}

	case *rsql.Ident:
		col.SourceColumn = rsql.IdentName(expr)
		if col.Alias == "" {
			col.Alias = col.SourceColumn
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
