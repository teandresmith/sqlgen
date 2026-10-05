package gen

import (
	"strings"
	"unicode"
)

// toPascalCase converts a snake_case, camelCase, or hyphenated string to
// PascalCase with acronym detection (e.g., "user_id" → "UserID"). When the
// input begins with a digit, the result is prefixed with "Col" so it is a
// valid Go identifier (e.g., "2010_revenue" → "Col2010Revenue"). See §8.5
// "Digit-Leading Handling".
//
// Spelling comes from identCaser and is sqlgen's own: the acronym set is
// deliberately not shared with gqlgen (PRD §8.5 "Acronym Detection").
// Generated code may nevertheless reference a gqlgen-emitted struct field by
// this name, because sqlgen *dictates* that name rather than predicting it —
// APIGoFieldOverrides (api_field_overrides.go) hands each one to gqlgen as
// `models.<T>.fields.<f>.fieldName` (PRD §26.5.6 "Go field naming").
// Predicting gqlgen's spelling drifts wherever its camelizer disagrees.
func toPascalCase(s string) string {
	return prefixIfDigitLeading(identCaser.ToPascal(normalizeForCasing(s)), "Col")
}

// toCamelCase converts a snake_case, PascalCase, or hyphenated string to
// camelCase with acronym detection (e.g., "user_id" → "userID"). When the
// input begins with a digit, the result is prefixed with "col" so it is a
// valid Go identifier (e.g., "2010_revenue" → "col2010Revenue"). See §8.5
// "Digit-Leading Handling".
//
// Shares identCaser with toPascalCase. Under the default `field_casing:
// camel_case` this spells the GraphQL field name emitted into the schema —
// the join key between the two generators (graphQLFieldName, context_api.go;
// `snake_case` passes the SQL name through instead). It also spells Go-side
// client field and query names, which gqlgen never sees.
//
// Either way, the Go name gqlgen gives a field is stated by sqlgen through the
// `fieldName` overrides (see toPascalCase), not derived back out of the
// camelCase form: camelization is lossy at a digit boundary ("line2_id" →
// "line2ID"), which leaves gqlgen's word walker no seam before the acronym
// (PRD §8.5, §26.5.6 "Go field naming").
func toCamelCase(s string) string {
	return prefixIfDigitLeading(identCaser.ToCamel(normalizeForCasing(s)), "col")
}

// toSnakeCase reads a PascalCase or camelCase Go identifier back as snake_case
// (e.g., "UserID" → "user_id").
//
// This is the inverse direction, and it is a reading rather than an inverse:
// "OSIDValue" is the PascalCase form of both "os_id_value" and "osi_dvalue",
// so no splitter can be right for both. Use toSnakeName instead wherever the
// SQL name is still in hand — it is exact, because the SQL name still carries
// the word boundaries the PascalCase form dropped. The one caller with no SQL
// name to read is the `struct_name` config override (PRD §8.5).
//
// The split is list-driven and takes the longest canonical acronym at each
// position; see splitIdentForSnake for why. gqlgen never observes this
// direction.
func toSnakeCase(s string) string {
	return joinSnakeParts(splitIdentForSnake(s))
}

// toSnakeName returns the snake_case spelling of a SQL name — a table, view,
// column, relationship or enum value as the schema spells it. It is the exact
// parallel of toPascalCase: same delimiters, same case rules, one canonical
// splitter (PRD §8.5), differing only in that it joins the words with "_" in
// lower case instead of concatenating them capitalized.
//
// Deriving the snake form from the SQL name is what makes it exact. The
// PascalCase form has lost the seams the SQL name still has — "osi_layer" and
// "os_ilayer" both spell OSILayer-shaped identifiers — so anything read back
// out of it is a guess.
//
// No digit-leading guard is applied here: callers that need a valid identifier
// add their own prefix, and they do not agree on it ("col" for a Go name,
// "col_" for a GraphQL enum value), so applying one here would silently move a
// published identifier.
func toSnakeName(s string) string {
	return snakeCaser.ToSnake(normalizeForCasing(s))
}

// joinSnakeParts lowercases split words, drops everything that is not a letter
// or a digit, and joins what is left with underscores. Filtering rather than
// splitting on punctuation is what keeps "asset.primary" a single word
// (PRD §8.5, normalizeForCasing).
func joinSnakeParts(parts []string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		var b strings.Builder
		b.Grow(len(part))
		for _, c := range part {
			if unicode.IsLetter(c) || unicode.IsDigit(c) {
				b.WriteRune(c)
			}
		}
		if b.Len() > 0 {
			out = append(out, b.String())
		}
	}
	return strings.ToLower(strings.Join(out, "_"))
}

// toSingular converts a plural English word to its singular form. It is the
// spelling generated error strings and doc comments use for a table
// ("scan user: %w"), reached from the template funcmap as well as from
// StructName and SnakeName.
//
// A name whose last word is a canonical acronym or a known non-plural "-s"
// noun is returned unchanged — an acronym is not an English plural, and the
// generic "-s" rule spelled `user_ips` as `UserIPSIPS` and `lens` as `Len`
// before this guard existed (PRD §8.5). The engine is sqlgen's own;
// see inflect.go.
func toSingular(s string) string {
	return englishInflector.singularize(s)
}

// toPlural converts a singular English word to its plural form.
//
// Unlike toSingular this never returns its input for a frozen word: it appends
// a marker instead. QueryName and QueryNamePlural share one `extend type
// Query` block, and StructName and StructNamePlural name two methods on one
// resolver receiver, so a plural that collided with its singular would emit a
// duplicate SDL field and a duplicate Go method (PRD §8.5).
func toPlural(s string) string {
	return englishInflector.pluralize(s)
}

// gqlEnumIdent returns the GraphQL enum value identifier for a schema-declared
// enum value (PRD §13.7, §26.5.2). Normalizes any input shape — dotted
// (`asset.primary`), snake (`multi_word_value`), hyphenated, bare — straight
// from the declared value. Both the schema declaration in
// `templates/api/shared.graphqls.tmpl` (via `APIEnumValue.GraphQLName` set by
// `buildAPIEnumContext`) AND the runtime MarshalGQL/UnmarshalGQL wire format in
// `templates/enum.go.tmpl` route through this single helper so the two paths
// cannot diverge for any input shape. Result is always a valid GraphQL enum
// value (`[A-Z][A-Z0-9_]*`).
//
// It used to round-trip through toPascalCase and read the result back, which
// published HTTP_SURL for a declared value of `https_url`. The
// declared value is the better source: it still has the word boundaries the
// PascalCase form drops. The "col" prefix (not "col_", which is
// screamingSnakeCase's) is retained from the round trip so digit-leading values
// keep the identifier they already publish.
func gqlEnumIdent(v string) string {
	return strings.ToUpper(prefixIfDigitLeading(toSnakeName(v), "col"))
}

// StructName returns the Go struct name for a table, applying singular
// PascalCase conversion. When the bare name appears in the collisions set,
// the schema is prefixed to disambiguate (e.g., "public.users" → "PublicUser"
// when "users" collides across schemas, plain "User" otherwise). Digit-leading
// table/schema names are normalized by toPascalCase (§8.5).
func StructName(table, schema string, collisions map[string]bool) string {
	singular := toSingular(table)
	name := toPascalCase(singular)

	if collisions[table] && schema != "" {
		prefix := toPascalCase(schema)
		name = prefix + name
	}

	return name
}

// SnakeName returns the snake_case form of the Go struct name StructName
// returns for the same table — the stem of the file that struct is generated
// into, and of the `.graphqls` schema and gqlgen resolver seed that accompany
// it. It mirrors StructName step for step (singularize, then the schema prefix
// on a collision, then the digit-leading guard) but spells each part with
// toSnakeName, so the result is derived from the SQL name rather than read back
// out of the PascalCase one (PRD §8.5).
func SnakeName(table, schema string, collisions map[string]bool) string {
	singular := toSingular(table)
	name := prefixIfDigitLeading(toSnakeName(singular), "col")

	if collisions[table] && schema != "" {
		prefix := prefixIfDigitLeading(toSnakeName(schema), "col")
		name = prefix + "_" + name
	}

	return name
}

// StructNamePlural returns the plural of a Go struct name — the spelling every
// generated identifier that names a set of rows is built from: the
// `Table<T>s` constant, `Get<T>sInput`, `Stream<T>sInput`, `scan<T>s`, the
// client accessor, and the GraphQL Connection query with its resolver method.
//
// **It never returns its input.** An uncountable noun pluralizes to itself, so
// a table named `media` or `series` gave the single-row query and the
// Connection query one name in the same `extend type Query` block, and their
// two resolvers one name on the same receiver — an invalid schema and a
// package that does not compile. Where inflection cannot produce a distinct
// plural, the English marker is appended instead, exactly as it is for a
// frozen acronym (PRD §8.5).
//
// This is the guarantee `toPlural` deliberately does not make: relationship
// field naming pluralizes a snake-case SQL name and *depends* on an
// already-plural name passing through unchanged, so `reviews` must stay
// `reviews` there (`relationshipToContext`). Templates reach this function as
// the `structNamePlural` funcmap entry rather than `toPlural`, so the two
// guarantees cannot be confused for one another.
//
// Exported for `sqlgen lint`, which resolves those identifiers back to their
// table and has to spell them exactly as the generator did. A second private
// deriver there desynced the lint type registry for every name the two
// disagreed on (file stems had the same shape).
func StructNamePlural(structName string) string {
	if plural := toPlural(structName); plural != structName {
		return plural
	}
	return appendPluralMarker(structName)
}

// TableConstantName returns the `hook.TableName` constant a table or view is
// registered under — "Table" followed by the plural of its resolved struct name
// (`TableUsers`, `TableProductSKUs`). Every site that names the constant — the
// table and view contexts, and the tablename file — goes through here.
// Downstream consumers such as `sqlgen lint` read the result off
// TableContext.TableNameConstant rather than re-deriving it.
//
// It follows `struct_name`, because the constant is a third name an entity
// claims at package scope and a collision on it is rejected like any other
// (§8.5). Spelling it from the SQL name while the struct and the file stem
// followed the override left `users` beside a `user` renamed to `LegacyUser`
// with two `TableUsers` constants and no way for the consumer to separate them
// — the documented escape hatch reached two of the three names it had to
// move. The constant's *value* is still the SQL table name; only its Go
// identifier tracks the struct.
func TableConstantName(table, schema, structNameOverride string, collisions map[string]bool) string {
	return "Table" + StructNamePlural(structNameFor(table, schema, structNameOverride, collisions))
}

// structNameFor returns the Go struct name for a table or view, honouring a
// `struct_name` config override. It is the sibling of snakeNameFor: the two
// spell the same entity's type name and file stem, so they resolve the override
// the same way and in one place.
func structNameFor(sqlName, schema, structNameOverride string, collisions map[string]bool) string {
	if structNameOverride != "" {
		return structNameOverride
	}
	return StructName(sqlName, schema, collisions)
}

// snakeNameFor returns the file-stem spelling for a table or view, honouring a
// `struct_name` config override. The override is a Go identifier the user
// supplied, so it is the one place with no SQL name to derive from and the only
// remaining caller of toSnakeCase (PRD §8.5). Pass "" for
// structNameOverride when the table or view has none.
func snakeNameFor(sqlName, schema, structNameOverride string, collisions map[string]bool) string {
	if structNameOverride != "" {
		return toSnakeCase(structNameOverride)
	}
	return SnakeName(sqlName, schema, collisions)
}

// FieldName returns the Go field name for a SQL column name, applying
// PascalCase conversion with acronym detection. Digit-leading column names
// receive a "Col" prefix via toPascalCase so the result is a valid Go
// identifier (§8.5).
func FieldName(column string) string {
	return toPascalCase(column)
}

// goReservedIdents lists every identifier that cannot appear as a Go local
// variable name without producing a compile error or a shadowing warning from
// go vet / revive. Three categories are folded together so the PRD-visible
// rule is one rule ("Go reserved word") rather than three:
//
//   - Keywords (Go spec §Keywords) — using these as identifiers is a compile
//     error.
//   - Predeclared identifiers (types, constants, functions including
//     Go 1.21+ builtins) — shadowing produces vet/revive warnings and silent
//     bugs (e.g., a column named `err` would shadow the local `err` in the
//     same scope).
//   - Generator-reserved locals (`ctx`, `err`, `v`, `ok`) — not language
//     reserved, but reserved by the surrounding template scope. Listed
//     explicitly so a user column never silently shadows them.
//
// See PRD §8.5 (Reserved Word Handling) for the rationale, and
// safety_property_test.go for the property test that pins this set against
// every bare-local emission site.
var goReservedIdents = map[string]bool{
	// Keywords (Go spec §Keywords)
	"break":       true,
	"case":        true,
	"chan":        true,
	"const":       true,
	"continue":    true,
	"default":     true,
	"defer":       true,
	"else":        true,
	"fallthrough": true,
	"for":         true,
	"func":        true,
	"go":          true,
	"goto":        true,
	"if":          true,
	"import":      true,
	"interface":   true,
	"map":         true,
	"package":     true,
	"range":       true,
	"return":      true,
	"select":      true,
	"struct":      true,
	"switch":      true,
	"type":        true,
	"var":         true,

	// Predeclared types
	"any":        true,
	"bool":       true,
	"byte":       true,
	"comparable": true,
	"complex64":  true,
	"complex128": true,
	"error":      true,
	"float32":    true,
	"float64":    true,
	"int":        true,
	"int8":       true,
	"int16":      true,
	"int32":      true,
	"int64":      true,
	"rune":       true,
	"string":     true,
	"uint":       true,
	"uint8":      true,
	"uint16":     true,
	"uint32":     true,
	"uint64":     true,
	"uintptr":    true,

	// Predeclared constants and zero
	"true":  true,
	"false": true,
	"iota":  true,
	"nil":   true,

	// Predeclared functions (including Go 1.21+ builtins min/max/clear)
	"append":  true,
	"cap":     true,
	"clear":   true,
	"close":   true,
	"complex": true,
	"copy":    true,
	"delete":  true,
	"imag":    true,
	"len":     true,
	"make":    true,
	"max":     true,
	"min":     true,
	"new":     true,
	"panic":   true,
	"print":   true,
	"println": true,
	"real":    true,
	"recover": true,

	// Generator-reserved locals — not Go reserved words, but reserved by the
	// surrounding template scope.
	"ctx": true,
	"err": true,
	"v":   true,
	"ok":  true,
}

// safeGoIdent returns name rewritten to a valid, non-shadowing Go local
// identifier. Two rules apply in order:
//
//  1. **Digit-leading guard.** When name begins with a digit, prefix with
//     "col" (e.g., "2010Revenue" → "col2010Revenue"). Go identifiers must
//     start with a letter or underscore; the upstream toCamelCase wrapper
//     already applies this guard at the case-transform boundary, but
//     safeGoIdent re-applies it so the template pipe stays correct even when
//     the camel form was computed elsewhere.
//  2. **Reserved-word suffix.** When the (post-prefix) name collides with a
//     Go keyword, predeclared identifier, or generator-reserved local,
//     append "Val" (e.g., "type" → "typeVal").
//
// Apply at every template site where a column name is lowered into a bare Go
// local variable name (no static suffix already follows). See §8.5
// "Reserved Word Handling" and "Digit-Leading Handling".
func safeGoIdent(name string) string {
	name = prefixIfDigitLeading(name, "col")
	if goReservedIdents[name] {
		return name + "Val"
	}
	return name
}

// prefixIfDigitLeading returns prefix + name when name's first byte is an
// ASCII digit, otherwise returns name unchanged. The prefix carries the
// casing convention of the surrounding context — "Col" for PascalCase
// (toPascalCase output), "col" for camelCase (toCamelCase / safeGoIdent
// output). Idempotent: applying twice does not double-prefix because the
// post-prefix name no longer starts with a digit.
func prefixIfDigitLeading(name, prefix string) string {
	if name == "" || name[0] < '0' || name[0] > '9' {
		return name
	}
	return prefix + name
}

// screamingSnakeCase converts a snake_case identifier to SCREAMING_SNAKE_CASE
// for GraphQL enum values. GraphQL enum values carry the same
// `/[_A-Za-z][_0-9A-Za-z]*/` identifier constraint as Go, so a column like
// `2010_revenue` must emit as `COL_2010_REVENUE` rather than `2010_REVENUE`
// — the digit-leading guard applies before the case conversion. See
// PRD §8.5 "Digit-Leading Handling" ("GraphQL enum values").
//
// This is the single spelling of a sort enum value in the generator
// (PRD §26.5.3). `APIFieldContext.SortEnumValue` is computed from it
// once per column; both the schema enum and the generated
// `<table>SortFieldToColumn` switch read that field rather than
// re-deriving, so the two cannot disagree.
func screamingSnakeCase(s string) string {
	return strings.ToUpper(prefixIfDigitLeading(toSnakeName(s), "col_"))
}
