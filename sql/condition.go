package sql

// andConditions represents a logical AND composition of conditions.
// When encountered as a Condition.Value, builders join the contained
// conditions with " AND " and wrap them in parentheses.
type andConditions []Condition

// orConditions represents a logical OR composition of conditions.
// When encountered as a Condition.Value, builders join the contained
// conditions with " OR " and wrap them in parentheses.
type orConditions []Condition

// Subquery represents a nested SELECT used as a condition value.
//
// SQL marks the args with bare "$" tokens, one per arg in order, or with
// "$1"…"$k", where "$N" is Args[N-1] and may repeat or appear out of order;
// one form per subquery. Builders number either form into the enclosing
// statement's positional sequence: PostgreSQL renders "$N" at its absolute
// position, and MySQL and SQLite emit one "?" per token with the args in
// token order. A "$" inside a string literal, a quoted identifier, a comment
// or a PostgreSQL dollar-quoted string is part of the SQL, not a token;
// quotes match the SQL-standard way, a doubled quote escaping itself. On
// MySQL a backslash also escapes the character after it inside a '...' or
// "..." literal; PostgreSQL and SQLite read a backslash literally, so escape a
// quote there by doubling it.
//
// SQL outside both forms is not checked or repaired: it reaches the database
// as written, and some shapes run without an error (PRD §11.5).
type Subquery struct {
	SQL  string
	Args []any
}

// Range represents a BETWEEN clause value with start and end bounds.
type Range struct {
	Start any
	End   any
}

// existsCondition is the structured value behind a correlated EXISTS
// predicate. It carries the parent-side correlation column and never a
// qualifier: the qualifier is path-dependent — the plain read path selects
// from an unaliased table, the O2O-join path aliases it — so builders resolve
// it at render time. See [Exists].
type existsCondition struct {
	CorrelationColumn string
	Subquery          Subquery
}

// CorrelationToken marks the parent-side reference inside an [Exists]
// subquery. Builders replace every occurrence with the enclosing statement's
// qualifier followed by the quoted correlation column. Generated code
// concatenates this constant rather than spelling the token:
//
//	sql.Exists("id", sql.Subquery{
//		SQL: `SELECT 1 FROM "comments" tgt WHERE tgt."post_id" = ` +
//			sql.CorrelationToken + ` AND tgt."deleted_at" IS NULL`,
//	})
const CorrelationToken = "{{parent}}"

// ConditionBuilder provides a fluent API for constructing Condition values.
// Create one with Where(column).
type ConditionBuilder struct {
	column string
}

// Where begins building a condition for the given column.
func Where(column string) ConditionBuilder {
	return ConditionBuilder{column: column}
}

// Eq produces a "col = $" condition.
func (b ConditionBuilder) Eq(v any) Condition {
	return Condition{Clause: b.column + " = $", Value: v, Column: b.column}
}

// Neq produces a "col != $" condition.
func (b ConditionBuilder) Neq(v any) Condition {
	return Condition{Clause: b.column + " != $", Value: v, Column: b.column}
}

// Gt produces a "col > $" condition.
func (b ConditionBuilder) Gt(v any) Condition {
	return Condition{Clause: b.column + " > $", Value: v, Column: b.column}
}

// Gte produces a "col >= $" condition.
func (b ConditionBuilder) Gte(v any) Condition {
	return Condition{Clause: b.column + " >= $", Value: v, Column: b.column}
}

// Lt produces a "col < $" condition.
func (b ConditionBuilder) Lt(v any) Condition {
	return Condition{Clause: b.column + " < $", Value: v, Column: b.column}
}

// Lte produces a "col <= $" condition.
func (b ConditionBuilder) Lte(v any) Condition {
	return Condition{Clause: b.column + " <= $", Value: v, Column: b.column}
}

// In produces a "col IN $" condition. The values are stored as []any and
// builders expand them to one placeholder each, "col IN ($1, $2, ...)", on
// every dialect.
//
// A single [Subquery] argument is the subquery form instead:
// "col IN (SELECT ...)", with the subquery's "$" tokens numbered into the
// enclosing statement's positional sequence.
func (b ConditionBuilder) In(vals ...any) Condition {
	return Condition{Clause: b.column + " IN $", Value: listOrSubquery(vals), Column: b.column}
}

// Nin produces a "col NOT IN $" condition. It is [ConditionBuilder.In]'s
// negation, including the single-[Subquery] form.
func (b ConditionBuilder) Nin(vals ...any) Condition {
	return Condition{Clause: b.column + " NOT IN $", Value: listOrSubquery(vals), Column: b.column}
}

// listOrSubquery unwraps a lone [Subquery] so the builder renders it as a
// nested SELECT. Left wrapped in []any it would be bound as a parameter value.
func listOrSubquery(vals []any) any {
	if len(vals) == 1 {
		if sq, ok := vals[0].(Subquery); ok {
			return sq
		}
	}
	return vals
}

// Like produces a "col LIKE $" condition.
func (b ConditionBuilder) Like(v any) Condition {
	return Condition{Clause: b.column + " LIKE $", Value: v, Column: b.column}
}

// NLike produces a "col NOT LIKE $" condition.
func (b ConditionBuilder) NLike(v any) Condition {
	return Condition{Clause: b.column + " NOT LIKE $", Value: v, Column: b.column}
}

// Between produces a "col BETWEEN $ AND $" condition with a Range value.
func (b ConditionBuilder) Between(start, end any) Condition {
	return Condition{Clause: b.column + " BETWEEN $ AND $", Value: Range{Start: start, End: end}, Column: b.column}
}

// IsNull produces a "col IS NULL" condition with no value.
func (b ConditionBuilder) IsNull() Condition {
	return Condition{Clause: b.column + " IS NULL", Value: nil, Column: b.column}
}

// IsNotNull produces a "col IS NOT NULL" condition with no value.
func (b ConditionBuilder) IsNotNull() Condition {
	return Condition{Clause: b.column + " IS NOT NULL", Value: nil, Column: b.column}
}

// And returns a Condition whose Value is an AND composition of the given
// conditions. Builders expand this as "(cond1 AND cond2 AND ...)".
func And(conditions ...Condition) Condition {
	return Condition{Value: andConditions(conditions)}
}

// Or returns a Condition whose Value is an OR composition of the given
// conditions. Builders expand this as "(cond1 OR cond2 OR ...)".
func Or(conditions ...Condition) Condition {
	return Condition{Value: orConditions(conditions)}
}

// Exists returns a correlated EXISTS condition. The subquery references the
// enclosing statement's row through [CorrelationToken], which builders replace
// with the statement's qualifier and the quoted correlationColumn: the table
// alias when the statement aliased its table (the O2O-join read path), the
// formatted table name otherwise (the plain read path). One emitted value is
// therefore correct on both paths, which is why the qualifier is resolved by
// the builder instead of being baked in at generation time.
//
// The subquery's "$" or "$N" tokens, in either form [Subquery] describes, are
// renumbered into the enclosing statement's positional sequence in Conditions
// order, exactly as any other multi-arg condition and as a bare [Subquery]
// condition value.
//
// The subquery is the caller's to build and must reference [CorrelationToken]:
// one that does not is an uncorrelated EXISTS, which matches on the existence
// of any row the subquery returns rather than on the enclosing row's own.
func Exists(correlationColumn string, subquery Subquery) Condition {
	return Condition{Value: existsCondition{CorrelationColumn: correlationColumn, Subquery: subquery}}
}

// IsAnd reports whether the condition's value is an AND composition and
// returns the contained conditions.
func (c Condition) IsAnd() ([]Condition, bool) {
	v, ok := c.Value.(andConditions)
	return []Condition(v), ok
}

// IsOr reports whether the condition's value is an OR composition and
// returns the contained conditions.
func (c Condition) IsOr() ([]Condition, bool) {
	v, ok := c.Value.(orConditions)
	return []Condition(v), ok
}

// PrefixConditions prepends a table alias to each condition's column reference.
// This is used when building JOIN queries where bare column names would be
// ambiguous (e.g., "id" → "u.id" when alias is "u").
func PrefixConditions(alias string, conds []Condition) []Condition {
	if alias == "" {
		return conds
	}
	out := make([]Condition, len(conds))
	for i, c := range conds {
		out[i] = prefixCondition(alias, c)
	}
	return out
}

// prefixCondition qualifies the column reference inside a single-leaf
// Condition with alias. AND/OR compositions are descended recursively.
//
// Two paths, chosen by whether the condition names its column:
//
//   - Condition.Column set — the named token is qualified where it sits, so a
//     clause whose column is not the leading token stays well-formed.
//     `JSON_CONTAINS(metadata, $)` becomes `JSON_CONTAINS(u.metadata, $)`
//     rather than `u.JSON_CONTAINS(metadata, $)`, which MySQL reads as a
//     stored routine in schema `u`.
//   - Condition.Column empty — the historical prepend. This is correct only
//     for a clause whose column identifier is its leading token, which is what
//     [Raw] and hand-built conditions are expected to produce. Multi-identifier
//     expressions — anything with AND/OR/operator boundaries between columns —
//     must be qualified at codegen time before they reach the runtime. That
//     path lives in `parser/<dialect>/filter_qualifier.go` and is dispatched
//     from `cmd/sqlgen/gen/filter_qualifier.go`.
//
// A reference to the *enclosing* statement's own table is the exception: it
// must NOT be qualified at codegen time, because the qualifier differs between
// the plain read path and the aliased O2O-join path. [Exists] is the supported
// mechanism. Being a structured value rather than a leaf clause, it is passed
// through untouched here — including inside the And/Or recursion below — and
// the builder resolves its qualifier at render time (PRD 11.5).
func prefixCondition(alias string, c Condition) Condition {
	// Handle AND/OR compositions recursively
	if andConds, ok := c.IsAnd(); ok {
		return And(PrefixConditions(alias, andConds)...)
	}
	if orConds, ok := c.IsOr(); ok {
		return Or(PrefixConditions(alias, orConds)...)
	}
	if c.Clause == "" {
		return c
	}
	qualified := alias + "." + c.Column
	if c.Column == "" {
		c.Clause = alias + "." + c.Clause
		return c
	}
	c.Clause = qualifyColumn(c.Clause, c.Column, qualified)
	c.Column = qualified
	return c
}

// qualifyColumn replaces the first whole-token occurrence of column in clause
// with qualified. "Whole-token" means the match is not bounded on either side
// by an identifier character, so a column named `one` is matched as the
// argument in `JSON_CONTAINS_PATH(one, 'one', $)` and not as part of a longer
// identifier in `one_more = $`.
//
// The first occurrence is the right one for every clause the comparators build:
// each names its column once, ahead of any literal that might repeat it.
// A clause whose column does not appear at all is returned unchanged rather
// than mangled.
func qualifyColumn(clause, column, qualified string) string {
	for i := 0; i+len(column) <= len(clause); i++ {
		if clause[i:i+len(column)] != column {
			continue
		}
		if i > 0 && isIdentifierByte(clause[i-1]) {
			continue
		}
		if end := i + len(column); end < len(clause) && isIdentifierByte(clause[end]) {
			continue
		}
		return clause[:i] + qualified + clause[i+len(column):]
	}
	return clause
}

// isIdentifierByte reports whether b can appear inside a SQL identifier, which
// is what makes a candidate match a fragment of a longer name rather than the
// column itself. The dialects' quoting characters (" and `) are deliberately
// excluded: a quoted identifier's boundary ends the token.
func isIdentifierByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// Raw creates a Condition from an arbitrary SQL fragment using "$" placeholders.
// The clause marks its args in either PRD §11.5 form, like a [Subquery]'s SQL:
// one bare "$" per arg, matched in order, or "$1"…"$k", where "$N" is
// args[N-1] and may repeat or appear out of order. A "$" inside a string
// literal, a quoted identifier, a comment or a PostgreSQL dollar-quoted
// string is the SQL's own text; quotes, and the MySQL backslash escape, are
// read as for a [Subquery].
//
// The fragment is opaque to the builder, so the returned condition carries
// [Condition.Raw] and renders parenthesised — without it a clause whose
// top-level operator is OR would break out of its own term once a caller
// AND-joined it with others.
func Raw(clause string, args ...any) Condition {
	switch len(args) {
	case 0:
		return Condition{Clause: clause, Value: nil, Raw: true}
	case 1:
		return Condition{Clause: clause, Value: args[0], Raw: true}
	default:
		return Condition{Clause: clause, Value: args, Raw: true}
	}
}
