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
	selectStmt, ok := vs.Query.Node.(*pg_query.Node_SelectStmt)
	if !ok {
		return parser.ViewSQL{}, fmt.Errorf("CREATE VIEW query is not a SELECT statement")
	}
	sel := selectStmt.SelectStmt

	return parser.ViewSQL{
		Name:         vs.View.Relname,
		Schema:       cmp.Or(vs.View.Schemaname, defaultSchema),
		Columns:      extractResTargets(sel.TargetList),
		TableAliases: extractFromAliases(sel.FromClause),
	}, nil
}

func parseMatviewStmt(cs *pg_query.CreateTableAsStmt, defaultSchema string) (parser.ViewSQL, error) {
	selectStmt, ok := cs.Query.Node.(*pg_query.Node_SelectStmt)
	if !ok {
		return parser.ViewSQL{}, fmt.Errorf("CREATE MATERIALIZED VIEW query is not a SELECT statement")
	}
	sel := selectStmt.SelectStmt

	return parser.ViewSQL{
		Name:         cs.Into.Rel.Relname,
		Schema:       cmp.Or(cs.Into.Rel.Schemaname, defaultSchema),
		Columns:      extractResTargets(sel.TargetList),
		TableAliases: extractFromAliases(sel.FromClause),
		Materialized: true,
	}, nil
}

// extractResTargets converts pg_query ResTarget nodes into ViewSelectColumns.
func extractResTargets(targets []*pg_query.Node) []parser.ViewSelectColumn {
	columns := make([]parser.ViewSelectColumn, 0, len(targets))
	for _, node := range targets {
		rt, ok := node.Node.(*pg_query.Node_ResTarget)
		if !ok {
			continue
		}
		col := extractColumn(rt.ResTarget)
		if col.Alias != "" {
			columns = append(columns, col)
		}
	}
	return columns
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

// extractFromAliases builds a map of table alias → table name from the FROM clause.
func extractFromAliases(fromClause []*pg_query.Node) map[string]string {
	aliases := make(map[string]string)
	for _, node := range fromClause {
		collectFromAliases(node, aliases)
	}
	return aliases
}

// collectFromAliases recursively collects table aliases from FROM clause nodes
// (handles RangeVar for plain tables and JoinExpr for JOINs).
func collectFromAliases(node *pg_query.Node, aliases map[string]string) {
	if node == nil {
		return
	}

	switch n := node.Node.(type) {
	case *pg_query.Node_RangeVar:
		rv := n.RangeVar
		tableName := rv.Relname
		if rv.Alias != nil && rv.Alias.Aliasname != "" {
			aliases[rv.Alias.Aliasname] = tableName
		} else {
			aliases[tableName] = tableName
		}

	case *pg_query.Node_JoinExpr:
		collectFromAliases(n.JoinExpr.Larg, aliases)
		collectFromAliases(n.JoinExpr.Rarg, aliases)
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
