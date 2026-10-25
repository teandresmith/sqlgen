package config

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// postgresTypeSynonyms maps each PostgreSQL type spelling the parser and the
// introspector never write to the one they write for it. The parser's own
// table is catalogSpellings in parser/postgres/postgres.go, applied after
// pg_query reads the grammar's synonyms (int, decimal, character varying)
// as their pg_catalog names; the introspector's is normalizePostgresType in
// parser/introspect/postgres.go. config does not import the parser module, so
// the spellings are held here, and gen's TestPostgresTypeKey_MatchesParser
// keeps them in step with what the parser writes.
//
// The serial types are listed because the parser writes the integer type
// they expand to: a serial column's type is integer, never serial.
var postgresTypeSynonyms = map[string]string{
	"int":                         "integer",
	"int4":                        "integer",
	"serial":                      "integer",
	"serial4":                     "integer",
	"int2":                        "smallint",
	"smallserial":                 "smallint",
	"serial2":                     "smallint",
	"int8":                        "bigint",
	"bigserial":                   "bigint",
	"serial8":                     "bigint",
	"float4":                      "real",
	"float8":                      "double precision",
	"float":                       "double precision",
	"bool":                        "boolean",
	"bpchar":                      "char",
	"character":                   "char",
	"nchar":                       "char",
	"national character":          "char",
	"national char":               "char",
	"character varying":           "varchar",
	"char varying":                "varchar",
	"national character varying":  "varchar",
	"national char varying":       "varchar",
	"decimal":                     "numeric",
	"dec":                         "numeric",
	"timestamp with time zone":    "timestamptz",
	"timestamp without time zone": "timestamp",
	"time with time zone":         "timetz",
	"time without time zone":      "time",
	"bit varying":                 "varbit",
}

// postgresModifierTypes are the written spellings that carry type modifiers,
// as in varchar(10) or timestamptz(3). A synonym of any other type written
// with modifiers is not a type PostgreSQL accepts, so it is left alone.
var postgresModifierTypes = map[string]bool{
	"char": true, "varchar": true, "numeric": true, "timestamp": true,
	"timestamptz": true, "time": true, "timetz": true, "varbit": true,
}

// PostgresTypeSynonyms returns the PostgreSQL type synonyms PostgresTypeKey
// recognizes, bare (no modifiers, no array suffix), sorted.
func PostgresTypeSynonyms() []string {
	return slices.Sorted(maps.Keys(postgresTypeSynonyms))
}

// PostgresTypeKey returns the spelling the PostgreSQL parser writes for key,
// an overrides.types key, and true when key is a synonym it never writes:
// int4 is integer, character varying(10) is varchar(10), timestamp(3) with
// time zone is timestamptz(3), and an array keeps its [] suffix. float(p) is
// real for p up to 24 and double precision above. It returns "", false for a
// key the parser can write, or one that is no synonym.
func PostgresTypeKey(key string) (string, bool) {
	base, isArray := strings.CutSuffix(key, "[]")
	suffix := ""
	if isArray {
		suffix = "[]"
	}

	// A modifier sits after the first word of a multi-word name
	// (timestamp(3) with time zone) or at the end (character varying(10)).
	name, mods := base, ""
	if open := strings.IndexByte(base, '('); open >= 0 {
		end := strings.IndexByte(base[open:], ')')
		if end < 0 {
			return "", false
		}
		name = base[:open] + base[open+end+1:]
		mods = base[open : open+end+1]
	}

	if name == "float" && mods != "" {
		p, err := strconv.Atoi(mods[1 : len(mods)-1])
		switch {
		case err != nil || p < 1 || p > 53:
			return "", false
		case p <= 24:
			return "real" + suffix, true
		default:
			return "double precision" + suffix, true
		}
	}

	written, ok := postgresTypeSynonyms[name]
	if !ok {
		return "", false
	}
	if mods != "" && !postgresModifierTypes[written] {
		return "", false
	}
	return written + mods + suffix, true
}

// validatePostgresTypeKeys rejects an overrides.types key, global or
// table-scoped, that spells a PostgreSQL type as a synonym the parser never
// writes (PRD §4.13). Keys match the type as written (PRD §7.1), so such a
// key matches no column and its override would be dropped without a word.
// The error names the spelling to key it by. MySQL's stored-form spellings
// are a separate rule; this one applies under input.dialect: postgres only.
func validatePostgresTypeKeys(cfg *RootConfig, errs *[]error) {
	if cfg.Input.Dialect != DialectPostgres {
		return
	}
	check := func(scope string, types map[string]TypeOverride) {
		for _, key := range slices.Sorted(maps.Keys(types)) {
			if written, ok := PostgresTypeKey(key); ok {
				*errs = append(*errs, fmt.Errorf("%s.%s: PostgreSQL writes this type as %q; key it %q", scope, key, written, written))
			}
		}
	}
	check("overrides.types", cfg.Overrides.Types)
	for _, tableName := range sortedTableKeys(cfg.Tables) {
		if table := cfg.Tables[tableName]; table.Overrides != nil {
			check("tables."+tableName+".overrides.types", table.Overrides.Types)
		}
	}
}
