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
	sel := cv.Select
	if sel == nil {
		return parser.ViewSQL{}, fmt.Errorf("CREATE VIEW has no SELECT statement")
	}

	aliases := make(map[string]string)
	if sel.Source != nil {
		collectSourceAliases(sel.Source, aliases)
	}

	return parser.ViewSQL{
		Name:         rsql.IdentName(cv.Name),
		Columns:      extractResultColumns(sel.Columns),
		TableAliases: aliases,
	}, nil
}

// extractResultColumns converts rqlite ResultColumns into ViewSelectColumns.
func extractResultColumns(cols []*rsql.ResultColumn) []parser.ViewSelectColumn {
	result := make([]parser.ViewSelectColumn, 0, len(cols))
	for _, rc := range cols {
		col := extractResultColumn(rc)
		if col.Alias != "" {
			result = append(result, col)
		}
	}
	return result
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

// collectSourceAliases recursively collects table aliases from a FROM clause Source.
func collectSourceAliases(src rsql.Source, aliases map[string]string) {
	switch s := src.(type) {
	case *rsql.QualifiedTableName:
		tableName := rsql.IdentName(s.Name)
		if s.Alias != nil {
			aliases[rsql.IdentName(s.Alias)] = tableName
		} else {
			aliases[tableName] = tableName
		}

	case *rsql.JoinClause:
		collectSourceAliases(s.X, aliases)
		collectSourceAliases(s.Y, aliases)

	case *rsql.ParenSource:
		if s.X != nil {
			collectSourceAliases(s.X, aliases)
		}
	}
}
