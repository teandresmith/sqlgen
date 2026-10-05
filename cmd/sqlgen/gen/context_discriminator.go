package gen

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// discriminatorContext renders a config discriminator into the two literal
// forms the read paths need (PRD §13.4.1). A nil input yields nil, which every
// consumer reads as "this edge has no structured predicate".
//
// Both renderings are produced eagerly rather than on demand because the three
// read paths are in three different templates and two different packages'
// worth of context; deciding the spelling once here keeps the O2O and O2M
// forms from drifting apart.
func discriminatorContext(d *config.RelationshipDiscriminator) *DiscriminatorContext {
	if d == nil {
		return nil
	}
	return &DiscriminatorContext{
		Column:     d.Column,
		GoLiteral:  discriminatorGoLiteral(d.Value),
		SQLLiteral: discriminatorSQLLiteral(d.Value),
	}
}

// discriminatorGoLiteral renders a discriminator value as a Go source literal,
// for the paths that bind it as a query parameter.
//
// config.validateDiscriminatorValue restricts the value to the scalar kinds
// below — the same set schema/v1.json declares — so the default arm is reached
// only when a caller builds a context without running validation. It renders as
// a quoted string rather than falling through to a bare token, because an
// unquoted literal would be a compile error in the generated package and a
// quoted one is at worst a wrong value the compiler still accepts.
func discriminatorGoLiteral(v any) string {
	switch t := v.(type) {
	case string:
		return strconv.Quote(t)
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		return strconv.Quote(fmt.Sprint(v))
	}
}

// discriminatorSQLLiteral renders a discriminator value as a SQL literal, for
// the O2O JOIN path that has no parameter channel to bind through.
//
// Strings are single-quoted with embedded single quotes doubled, which every
// supported dialect reads the same way. A backslash does *not* have such a
// spelling — MySQL treats it as an escape inside a string literal unless
// NO_BACKSLASH_ESCAPES is set, PostgreSQL and SQLite take it literally — so
// config.validateDiscriminatorValue rejects one on the O2O edges that reach
// this function rather than letting a dialect-dependent literal through.
//
// Numbers and booleans are bare: `TRUE` / `FALSE` is spelled that way by
// PostgreSQL and MySQL, and SQLite accepts it as an alias for 1 / 0, so one
// spelling serves all three.
func discriminatorSQLLiteral(v any) string {
	switch t := v.(type) {
	case string:
		return sqlStringLiteral(t)
	case bool:
		return strings.ToUpper(strconv.FormatBool(t))
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		return sqlStringLiteral(fmt.Sprint(v))
	}
}

// sqlStringLiteral single-quotes s, doubling any embedded single quote.
func sqlStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// EquivalentFilter returns the `filter:` text this discriminator is the
// structured form of — `<column> = <literal>`, unqualified.
//
// PRD §13.4.1 specifies the discriminator by equivalence ("compiles to exactly
// the equality predicate the equivalent `filter:` produced"), so the O2O path
// hands this string to the same qualifyFilter the `filter:` path uses rather
// than hand-assembling an alias-qualified predicate. That makes byte-identity
// structural instead of something each dialect has to be matched against
// individually — and it has to be, because the three qualifiers disagree:
// pg_query and vitess emit bare identifiers where rqlite/sql emits
// `"alias"."column"`. A hand-built predicate matched two dialects and silently
// changed the third.
//
// The manifest reports the same string, for the same reason.
func (d *DiscriminatorContext) EquivalentFilter() string {
	return d.Column + " = " + d.SQLLiteral
}

// validateDiscriminatorBindings rejects a `discriminator:` whose read path
// cannot work against the column it names (PRD §4.13, §13.4.1 "Validation").
//
// Every read path compares the column with the declared value, bound as the
// constant's Go default type (string, int64, float64, bool) on the loader and
// the relationship-filter subquery, and interpolated as a SQL literal in the
// O2O JOIN. Three shapes of column make that comparison fail, and none of them
// is visible until the first read runs, so each is a config error here rather
// than a runtime surprise:
//
//   - The value's kind differs from a column bound to a predeclared type or a
//     generated enum: a string on an integer column, a number on a text one.
//     PostgreSQL rejects the comparison (SQLSTATE 22P02, or a pgx encode
//     error), and MySQL and SQLite coerce it — MySQL's `int = 'pinned'`
//     matches the rows holding 0.
//   - A PostgreSQL `json` or `jsonb` column: `json` has no equality operator
//     (SQLSTATE 42883), and a `jsonb` operand must itself be JSON text
//     (SQLSTATE 22P02).
//   - A SQLite column bound to `[]byte` or `types.JSON`: the generated client
//     writes it as a BLOB, and SQLite never finds a BLOB equal to the text or
//     number the read compares it with, so the edge reads none of those rows
//     and reports no error.
//
// Anything else is left to the database, where it was measured to work: a
// MySQL `json` column, and PostgreSQL `uuid`, `timestamptz` and `inet` columns
// compared with a string. Their nested `create` is a separate question, which
// §9.9.4's discriminator rule answers for the write surface alone.
func validateDiscriminatorBindings(tables []TableContext, views []ViewContext, dialect config.Dialect, resolver *gotype.Resolver) error {
	byTable := newTableIndex(tables)
	viewColumns := make(map[string][]ColumnContext, len(views)*2)
	for _, v := range views {
		viewColumns[v.ViewName] = v.Columns
		viewColumns[qualifiedOrBare(v.Schema, v.ViewName)] = v.Columns
	}

	var errs []error
	for _, tc := range tables {
		for _, rel := range tc.Relationships {
			d := rel.Discriminator
			if d == nil {
				continue
			}
			var columns []ColumnContext
			if target, ok := byTable.lookup(rel.TargetSchema, rel.TargetTable); ok {
				columns = target.Columns
			} else {
				columns = viewColumns[qualifiedOrBare(rel.TargetSchema, rel.TargetTable)]
			}
			i := slices.IndexFunc(columns, func(c ColumnContext) bool { return c.Name == d.Column })
			if i < 0 {
				// Config validation already reported a column the target does
				// not have (PRD §4.13); an unresolved target is reported where
				// the relationship is built.
				continue
			}
			reason := discriminatorReadRefusal(columns[i], d.GoLiteral, dialect, resolver)
			if reason == "" {
				continue
			}
			errs = append(errs, fmt.Errorf(
				"tables.%s.relationships: %q declares discriminator %s = %s, but %s (PRD §4.13 / §13.4.1)",
				qualifiedOrBare(tc.Schema, tc.TableName), rel.Name, d.Column, d.GoLiteral, reason,
			))
		}
	}
	return errors.Join(errs...)
}

// discriminatorReadRefusal returns why the read paths cannot compare col with
// the declared value, or "" when they can. validateDiscriminatorBindings lists
// the three shapes and the evidence for each.
func discriminatorReadRefusal(col ColumnContext, literal string, dialect config.Dialect, resolver *gotype.Resolver) string {
	bare := strings.TrimPrefix(col.GoType, "*")
	if underlying, ok := gotype.StdNullUnderlying(bare); ok {
		bare = underlying
	}

	if !col.IsSlice && !col.IsSet && (isPredeclaredScalar(bare) || resolver.IsEnumGoType(bare)) &&
		!constantAssignable(bare, literal, resolver) {
		return fmt.Sprintf("the column binds to %s and the value is %s: PostgreSQL rejects that comparison, and MySQL and SQLite coerce it, so a read can match the wrong rows. Write the value in the column's own kind",
			col.GoType, literalKind(literal))
	}

	switch dialect {
	case config.DialectPostgres:
		switch strings.ToLower(col.BaseSQLType) {
		case "json", "jsonb":
			return fmt.Sprintf("the column is PostgreSQL %s, which a read cannot compare with a value: json has no equality operator (SQLSTATE 42883) and a jsonb operand must itself be JSON (SQLSTATE 22P02). Discriminate on a text or enum column",
				strings.ToLower(col.BaseSQLType))
		}
	case config.DialectSQLite:
		switch bare {
		case "[]byte", "types.JSON":
			return fmt.Sprintf("the column binds to %s, which the generated client writes as a BLOB, and SQLite never finds a BLOB equal to %s, which is what a read compares it with, so the edge would silently read none of those rows. Bind the column to string with `column_map`, or discriminate on a TEXT column",
				col.GoType, literalKind(literal))
		}
	case config.DialectMySQL:
		// A MySQL `json` column compares with a string value, and no other
		// binding was measured to fail there.
	}
	return ""
}

// isPredeclaredScalar reports whether goType is one of Go's predeclared
// string, boolean or numeric types.
func isPredeclaredScalar(goType string) bool {
	switch goType {
	case "string", "bool", "byte", "rune",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64":
		return true
	}
	return false
}

// literalKind names the kind of a discriminator value as discriminatorGoLiteral
// renders it, for an error message.
func literalKind(literal string) string {
	switch {
	case strings.HasPrefix(literal, `"`):
		return "a string"
	case literal == "true" || literal == "false":
		return "a boolean"
	case strings.ContainsAny(literal, ".eE"):
		return "a floating-point number"
	}
	return "an integer"
}
