package gen

import (
	"fmt"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// qualifyFilter rewrites every bare column reference in filter to be qualified
// with alias, dispatching to the dialect's native SQL parser. Already-qualified
// refs are left untouched.
//
// o2o `filter:` JOIN ON predicates need codegen-time qualification so
// multi-column filters bind every identifier to the relationship target
// alias rather than only the first token.
func qualifyFilter(filter, alias string, dialect config.Dialect) (string, error) {
	var (
		out string
		err error
	)
	switch dialect {
	case config.DialectPostgres:
		out, err = postgres.QualifyFilterIdentifiers(filter, alias)
	case config.DialectMySQL:
		out, err = mysql.QualifyFilterIdentifiers(filter, alias)
	case config.DialectSQLite:
		out, err = sqlite.QualifyFilterIdentifiers(filter, alias)
	default:
		return "", fmt.Errorf("qualify filter: unsupported dialect %q", dialect)
	}
	if err != nil {
		return "", fmt.Errorf("qualify filter (%s): %w", dialect, err)
	}
	return out, nil
}

// aliasProbeColumn is the throwaway column name qualifiedAliasPrefix sends
// through a dialect's qualifier to read back how that dialect spells a
// qualified reference. Any bare identifier would do; this one is unreserved on
// all three dialects, so none of them quotes it for its own reasons.
const aliasProbeColumn = "sqlgencol"

// qualifiedAliasPrefix returns the dialect qualifier's own spelling of alias
// followed by its separator — "sqlgenrel." on PostgreSQL and MySQL,
// `"sqlgenrel".` on SQLite.
//
// The spelling is discovered by round-tripping a one-column probe through the
// *same* QualifyFilterIdentifiers that produced the predicate, rather than
// assumed. It is not a contract: pg_query and vitess deparse a qualified
// reference with bare identifiers, while rqlite/sql's Ident.String() quotes
// every identifier unconditionally, with no flag and no alias name that avoids
// it. A hardcoded "sqlgenrel." would never match SQLite's output: the splice
// would silently find nothing, and the placeholder reached the
// generated SQL as a table name nothing defines.
//
// Deriving it here means a future parser that changes its deparsed spelling
// moves the splice with it instead of breaking it.
func qualifiedAliasPrefix(alias string, dialect config.Dialect) (string, error) {
	probe, err := qualifyFilter(aliasProbeColumn, alias, dialect)
	if err != nil {
		return "", fmt.Errorf("deriving alias prefix (%s): %w", dialect, err)
	}
	// Neither the alias nor the probe column contains a separator, and no
	// dialect's quoting characters do either, so the last one is the
	// separator the qualifier emitted.
	i := strings.LastIndex(probe, ".")
	if i < 0 {
		return "", fmt.Errorf(
			"deriving alias prefix (%s): qualifier returned %q for a bare column, which carries no qualified reference",
			dialect, probe,
		)
	}
	return probe[:i+1], nil
}
