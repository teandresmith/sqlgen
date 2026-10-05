package sql

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// SoftDeleteType identifies the column strategy used for soft deletion.
type SoftDeleteType string

const (
	// SoftDeleteTimestamp sets the column to CURRENT_TIMESTAMP on delete.
	SoftDeleteTimestamp SoftDeleteType = "timestamp"
	// SoftDeleteBool sets the column to TRUE on delete.
	SoftDeleteBool SoftDeleteType = "bool"
	// SoftDeleteInteger sets the column to 1 on delete.
	SoftDeleteInteger SoftDeleteType = "integer"
)

// SelectOptions configures a SELECT statement.
//
// Tenancy, soft-delete, and user-supplied filters are all standard [Condition]
// values appended to [SelectOptions.Conditions] by the generator before the
// [BuildSelect] / [BuildSelectJoin] call — the builder remains
// tenancy-agnostic. Placeholder positions follow the Conditions slice order.
type SelectOptions struct {
	Columns    []string    // column names to select; empty means SELECT *
	Conditions []Condition // WHERE conditions
	OrderBy    []Sort      // ORDER BY clauses
	Limit      *int        // LIMIT value
	Offset     *int        // OFFSET value
	Alias      string      // primary table alias for JOIN queries; empty for non-JOIN
	LockMode   LockMode    // row-level lock clause appended after LIMIT/OFFSET (see [LockMode])
}

// InsertOptions configures an INSERT statement.
type InsertOptions struct {
	Columns             []string // column names
	Values              []any    // values in same order as Columns
	ReturningColumns    []string // columns to RETURNING
	UpsertConflictKeys  []string // conflict target columns for upsert
	UpsertUpdateColumns []string // columns to update on conflict

	// UpsertResolvePKColumn names the single-column primary key the caller
	// must read back from an upsert, or "" when the caller already knows it.
	// See [UpsertClauseOptions.ResolvePKColumn]; it is passed straight
	// through, and only MySQL acts on it.
	UpsertResolvePKColumn string
}

// MultiInsertOptions configures a multi-row INSERT statement.
//
// The three Upsert fields mirror [InsertOptions] and carry the same meaning:
// one conflict clause governs the whole statement, not a row of it.
type MultiInsertOptions struct {
	Columns             []string // column names
	ValueRows           [][]any  // each row's values in same order as Columns
	ReturningColumns    []string // columns to RETURNING
	UpsertConflictKeys  []string // conflict target columns for upsert
	UpsertUpdateColumns []string // columns to update on conflict

	// UpsertResolvePKColumn names the single-column primary key the caller
	// must read back from an upsert, or "" when the caller already knows it.
	// See [UpsertClauseOptions.ResolvePKColumn]; it is passed straight
	// through, and only MySQL acts on it.
	//
	// It resolves nothing past a single-row batch, which is where it parts
	// company with [InsertOptions.UpsertResolvePKColumn]. MySQL evaluates
	// LAST_INSERT_ID(pk) once per conflicting row, so the OK packet carries
	// the last conflicting row's id; after a batch that mixed inserts with
	// conflicts it names neither the caller's row nor the first inserted one.
	// A batched caller must therefore take its PKs from the inputs, never
	// from the statement (PRD 9.7).
	UpsertResolvePKColumn string
}

// UpdateOptions configures an UPDATE statement.
//
// Tenancy filters belong in [UpdateOptions.Conditions] (the WHERE clause), not
// in [UpdateOptions.SetClauses] — a row's tenant is not a mutable attribute
// under normal operation. The generator composes tenancy, soft-delete, and
// user-supplied predicates into the Conditions slice before the [BuildUpdate]
// call; the builder remains tenancy-agnostic.
type UpdateOptions struct {
	SetClauses       map[string]any // column → value pairs for SET
	Conditions       []Condition    // WHERE conditions
	ReturningColumns []string       // columns to RETURNING
}

// SoftDeleteOptions configures a soft delete UPDATE statement.
type SoftDeleteOptions struct {
	Column           string         // column name (e.g. "deleted_at", "is_deleted", "deleted")
	Type             SoftDeleteType // deletion strategy: timestamp, bool, or integer
	Conditions       []Condition    // WHERE conditions
	ReturningColumns []string       // columns to RETURNING (PostgreSQL/SQLite only)
}

// JoinClause configures a LEFT JOIN in a select-join query.
//
// The [JoinClause.On] string is treated as opaque, pre-formatted SQL: builders
// do not parse or number placeholders inside it. Tenant filters on a tenanted
// child table MUST be appended to the outer [SelectOptions.Conditions] (e.g.
// `{childAlias}.{tenantCol} = $`), not spliced into On — that convention keeps
// placeholder numbering deterministic and driven entirely by Conditions order
// (see [BuildSelectJoin]).
type JoinClause struct {
	Table      Table              // joined table
	Alias      string             // table alias
	On         string             // ON condition (pre-formatted SQL; no placeholders)
	Columns    []string           // columns to select from joined table
	SoftDelete *SoftDeleteOptions // if non-nil, adds exclusion filter to ON condition
}

// BuildSelect generates a SELECT statement.
func BuildSelect(d Dialect, t Table, opts SelectOptions) (string, []any) {
	var b strings.Builder
	b.WriteString("SELECT ")
	writeSelectColumns(&b, d, opts.Columns)
	b.WriteString(" FROM ")
	table := d.FormatTable(t)
	b.WriteString(table)

	// The statement does not alias its table, so a correlated [Exists]
	// condition qualifies against the table reference itself (PRD 11.5).
	pos := 1
	args := appendWhere(&b, d, opts.Conditions, pos, table)
	writeOrderBy(&b, d, opts.OrderBy, "")
	writeLimitOffset(&b, opts.Limit, opts.Offset)
	writeLockClause(&b, d, opts.LockMode)

	return b.String(), args
}

// BuildSelectJoin generates a SELECT with LEFT JOIN clauses for O2O relationships.
// Column aliases use the "alias.column" pattern for prefix-based scanning.
//
// Placeholder numbering is driven entirely by opts.Conditions in slice order;
// JOIN On strings contribute nothing (they are opaque SQL without
// placeholders). This is the contract tenant-filter emission relies on: the
// generator can append `{childAlias}.{tenantCol} = $` to Conditions and know
// the placeholder will land at position N+1 relative to the previous N
// conditions, regardless of how many JOINs precede it.
//
// Each [Sort] names a column of the primary table, as it does for
// [BuildSelect]. The builder qualifies it with the same qualifier the
// correlated [Exists] uses (`ORDER BY p."price" DESC`): a bare column that a
// joined table also has would otherwise be ambiguous.
func BuildSelectJoin(d Dialect, t Table, joins []JoinClause, opts SelectOptions) (string, []any) {
	var b strings.Builder
	b.WriteString("SELECT ")
	writeJoinColumns(&b, d, opts.Alias, opts.Columns, joins)
	b.WriteString(" FROM ")
	b.WriteString(d.FormatTable(t))
	b.WriteString(" ")
	b.WriteString(opts.Alias)
	writeJoinClauses(&b, d, joins)

	// The statement aliases its table, so a correlated [Exists] condition
	// qualifies against that alias rather than the table name (PRD 11.5).
	qualifier := opts.Alias
	if qualifier == "" {
		qualifier = d.FormatTable(t)
	}

	pos := 1
	args := appendWhere(&b, d, opts.Conditions, pos, qualifier)
	writeOrderBy(&b, d, opts.OrderBy, qualifier)
	writeLimitOffset(&b, opts.Limit, opts.Offset)
	writeLockClause(&b, d, opts.LockMode)

	return b.String(), args
}

// BuildInsert generates a single-row INSERT statement.
//
// With no column supplied it emits the dialect's [Dialect.DefaultValuesClause]
// so every column takes its default, and it omits the conflict clause: SQLite
// accepts no upsert clause after DEFAULT VALUES, and with no column supplied
// the conflict target names no column the statement writes. The same holds,
// and the clause is omitted too, when the target names any column the
// statement does not write.
func BuildInsert(d Dialect, t Table, opts InsertOptions) (string, []any) {
	cols, vals := sortColumnsWithValues(opts.Columns, opts.Values)

	var b strings.Builder
	b.WriteString("INSERT INTO ")
	b.WriteString(d.FormatTable(t))
	if len(cols) == 0 {
		b.WriteString(" ")
		b.WriteString(d.DefaultValuesClause())
		writeReturning(&b, d, opts.ReturningColumns)
		return b.String(), vals
	}
	b.WriteString(" (")
	writeQuotedList(&b, d, cols)
	b.WriteString(") VALUES (")
	b.WriteString(d.PlaceholderList(1, len(cols)))
	b.WriteString(")")

	// A conflict target naming a column the statement does not write is not
	// one the caller can match on: the column takes a database-generated value
	// or its default. MySQL's ON DUPLICATE KEY UPDATE, which names no target,
	// would then update whichever row a different unique index collided with,
	// so the clause is dropped and the write is a plain insert on every
	// dialect, exactly as a write that supplies no column is (PRD 9.5).
	if len(opts.UpsertConflictKeys) > 0 && suppliesAll(cols, opts.UpsertConflictKeys) {
		b.WriteString(" ")
		b.WriteString(d.UpsertClause(UpsertClauseOptions{
			ConflictKeys:    opts.UpsertConflictKeys,
			UpdateColumns:   opts.UpsertUpdateColumns,
			ResolvePKColumn: opts.UpsertResolvePKColumn,
		}))
	}
	writeReturning(&b, d, opts.ReturningColumns)

	return b.String(), vals
}

// suppliesAll reports whether every key is one of cols.
func suppliesAll(cols, keys []string) bool {
	for _, k := range keys {
		if !slices.Contains(cols, k) {
			return false
		}
	}
	return true
}

// BuildMultiInsert generates a multi-row INSERT statement.
// Returns empty string and nil args when ValueRows is empty.
//
// When a value in a row is [Default], the SQL DEFAULT keyword is emitted
// instead of a placeholder, and the value is not included in the args slice.
// This enables multi-row INSERTs where omittable fields differ per row while
// keeping all rows in the same column list.
//
// When [MultiInsertOptions.UpsertConflictKeys] is non-empty the statement
// carries the dialect's conflict clause after the last value row and before
// any RETURNING, byte-identical to the one [BuildInsert] emits for the same
// target when every target column is written: the clause applies to every
// value row, which is what makes a one-statement batched upsert possible.
// Unlike [BuildInsert] it never drops the clause; UpsertMany refuses or
// proceeds on an unwritten target itself (PRD 9.5).
func BuildMultiInsert(d Dialect, t Table, opts MultiInsertOptions) (string, []any) {
	if len(opts.ValueRows) == 0 {
		return "", nil
	}

	cols, colOrder := sortedColumnsOrder(opts.Columns)

	var b strings.Builder
	b.WriteString("INSERT INTO ")
	b.WriteString(d.FormatTable(t))
	b.WriteString(" (")
	writeQuotedList(&b, d, cols)
	b.WriteString(") VALUES ")

	var args []any
	pos := 1
	for i, row := range opts.ValueRows {
		if i > 0 {
			b.WriteString(", ")
		}
		reordered := reorderRow(row, colOrder)
		b.WriteString("(")
		for j, v := range reordered {
			if j > 0 {
				b.WriteString(", ")
			}
			if isDefault(v) {
				b.WriteString("DEFAULT")
			} else if de, ok := isDefaultExpr(v); ok {
				b.WriteString(de.Expr)
			} else {
				b.WriteString(d.Placeholder(pos))
				args = append(args, v)
				pos++
			}
		}
		b.WriteString(")")
	}

	if len(opts.UpsertConflictKeys) > 0 {
		b.WriteString(" ")
		b.WriteString(d.UpsertClause(UpsertClauseOptions{
			ConflictKeys:    opts.UpsertConflictKeys,
			UpdateColumns:   opts.UpsertUpdateColumns,
			ResolvePKColumn: opts.UpsertResolvePKColumn,
		}))
	}
	writeReturning(&b, d, opts.ReturningColumns)

	return b.String(), args
}

// BuildUpdate generates an UPDATE statement with sorted SET columns.
func BuildUpdate(d Dialect, t Table, opts UpdateOptions) (string, []any) {
	setCols := sortedKeys(opts.SetClauses)

	var b strings.Builder
	table := d.FormatTable(t)
	b.WriteString("UPDATE ")
	b.WriteString(table)
	b.WriteString(" SET ")

	pos := 1
	args := make([]any, 0, len(setCols)+len(opts.Conditions))
	for i, col := range setCols {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(d.QuoteIdentifier(col))
		b.WriteString(" = ")
		b.WriteString(d.Placeholder(pos))
		args = append(args, opts.SetClauses[col])
		pos++
	}

	if len(opts.Conditions) > 0 {
		where, whereArgs, _ := buildWhere(d, opts.Conditions, pos, table)
		b.WriteString(" WHERE ")
		b.WriteString(where)
		args = append(args, whereArgs...)
	}
	writeReturning(&b, d, opts.ReturningColumns)

	return b.String(), args
}

// BuildCount generates a SELECT COUNT(*) statement.
func BuildCount(d Dialect, t Table, conditions []Condition) (string, []any) {
	var b strings.Builder
	table := d.FormatTable(t)
	b.WriteString("SELECT COUNT(*) FROM ")
	b.WriteString(table)
	args := appendWhere(&b, d, conditions, 1, table)
	return b.String(), args
}

// BuildRefreshMaterializedView generates a REFRESH MATERIALIZED VIEW
// statement, optionally with CONCURRENTLY. Materialized views are a
// PostgreSQL-only feature; the statement carries no values, only the
// dialect-quoted view identifier.
func BuildRefreshMaterializedView(d Dialect, t Table, concurrently bool) string {
	var b strings.Builder
	b.WriteString("REFRESH MATERIALIZED VIEW ")
	if concurrently {
		b.WriteString("CONCURRENTLY ")
	}
	b.WriteString(d.FormatTable(t))
	return b.String()
}

// BuildExists generates a SELECT EXISTS(SELECT 1 ...) statement.
func BuildExists(d Dialect, t Table, conditions []Condition) (string, []any) {
	var b strings.Builder
	table := d.FormatTable(t)
	b.WriteString("SELECT EXISTS(SELECT 1 FROM ")
	b.WriteString(table)
	args := appendWhere(&b, d, conditions, 1, table)
	b.WriteString(")")
	return b.String(), args
}

// BuildIncrement generates an UPDATE SET col = col + $N statement.
func BuildIncrement(d Dialect, t Table, column string, amount any, conditions []Condition) (string, []any) {
	var b strings.Builder
	table := d.FormatTable(t)
	b.WriteString("UPDATE ")
	b.WriteString(table)
	b.WriteString(" SET ")
	b.WriteString(d.QuoteIdentifier(column))
	b.WriteString(" = ")
	b.WriteString(d.QuoteIdentifier(column))
	b.WriteString(" + ")
	b.WriteString(d.Placeholder(1))

	args := []any{amount}
	if len(conditions) > 0 {
		where, whereArgs, _ := buildWhere(d, conditions, 2, table)
		b.WriteString(" WHERE ")
		b.WriteString(where)
		args = append(args, whereArgs...)
	}

	return b.String(), args
}

// BuildIncrementReturning generates the same UPDATE SET col = col + $N
// statement as [BuildIncrement] with a RETURNING clause appended
// (PostgreSQL/SQLite only; no-op on dialects without RETURNING support).
// It is a separate function rather than an options field so existing
// BuildIncrement call sites keep the positional signature.
func BuildIncrementReturning(d Dialect, t Table, column string, amount any, conditions []Condition, returningColumns []string) (string, []any) {
	query, args := BuildIncrement(d, t, column, amount, conditions)
	var b strings.Builder
	b.WriteString(query)
	writeReturning(&b, d, returningColumns)
	return b.String(), args
}

// BuildSoftDelete generates an UPDATE SET statement for soft deletion.
func BuildSoftDelete(d Dialect, t Table, opts SoftDeleteOptions) (string, []any) {
	var b strings.Builder
	table := d.FormatTable(t)
	b.WriteString("UPDATE ")
	b.WriteString(table)
	b.WriteString(" SET ")
	b.WriteString(d.QuoteIdentifier(opts.Column))

	switch opts.Type {
	case SoftDeleteTimestamp:
		b.WriteString(" = CURRENT_TIMESTAMP")
	case SoftDeleteBool:
		b.WriteString(" = TRUE")
	case SoftDeleteInteger:
		b.WriteString(" = 1")
	}

	args := appendWhere(&b, d, opts.Conditions, 1, table)
	writeReturning(&b, d, opts.ReturningColumns)
	return b.String(), args
}

// HardDeleteOptions configures a DELETE FROM statement.
//
// Tenancy, soft-delete, and user-supplied filters are all standard [Condition]
// values appended to [HardDeleteOptions.Conditions] by the generator before
// the [BuildHardDelete] call — the builder remains tenancy-agnostic.
type HardDeleteOptions struct {
	Conditions       []Condition // WHERE conditions
	ReturningColumns []string    // columns to RETURNING (PostgreSQL/SQLite only)
}

// BuildHardDelete generates a DELETE FROM statement.
func BuildHardDelete(d Dialect, t Table, opts HardDeleteOptions) (string, []any) {
	var b strings.Builder
	table := d.FormatTable(t)
	b.WriteString("DELETE FROM ")
	b.WriteString(table)
	args := appendWhere(&b, d, opts.Conditions, 1, table)
	writeReturning(&b, d, opts.ReturningColumns)
	return b.String(), args
}

// BuildColumnsFromFieldOptions extracts and sorts column names from a field options map.
func BuildColumnsFromFieldOptions(fieldOptionsMap map[string]string) []string {
	cols := make([]string, 0, len(fieldOptionsMap))
	for col := range fieldOptionsMap {
		cols = append(cols, col)
	}
	sort.Strings(cols)
	return cols
}

// BuildCompositePKConditions returns conditions for a single composite PK row lookup.
// Each column gets a "col = $" condition joined by AND in buildWhere.
func BuildCompositePKConditions(d Dialect, columns []string, values []any) []Condition {
	conds := make([]Condition, len(columns))
	for i, col := range columns {
		conds[i] = Condition{
			Clause: d.QuoteIdentifier(col) + " = $",
			Value:  values[i],
		}
	}
	return conds
}

// BuildCompositePKBatchCondition returns a condition for batch composite PK lookup.
// Dialects with tuple IN support produce: ("c1","c2") IN (($,$),($,$)).
// Others produce: (c1=$ AND c2=$) OR (c1=$ AND c2=$).
func BuildCompositePKBatchCondition(d Dialect, columns []string, valueSets [][]any) Condition {
	if len(valueSets) == 0 {
		return Condition{Clause: "1 = 0"}
	}
	if d.SupportsTupleIN() {
		return buildTupleIN(d, columns, valueSets)
	}
	return buildExpandedOR(d, columns, valueSets)
}

// --- Internal helpers ---

func buildTupleIN(d Dialect, columns []string, valueSets [][]any) Condition {
	var b strings.Builder
	b.WriteString("(")
	for i, col := range columns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(d.QuoteIdentifier(col))
	}
	b.WriteString(") IN (")

	args := make([]any, 0, len(columns)*len(valueSets))
	for i, vals := range valueSets {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(")
		for j := range columns {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString("$")
		}
		b.WriteString(")")
		args = append(args, vals...)
	}
	b.WriteString(")")

	return Condition{Clause: b.String(), Value: args}
}

func buildExpandedOR(d Dialect, columns []string, valueSets [][]any) Condition {
	orConds := make([]Condition, len(valueSets))
	for i, vals := range valueSets {
		andConds := make([]Condition, len(columns))
		for j, col := range columns {
			andConds[j] = Condition{
				Clause: d.QuoteIdentifier(col) + " = $",
				Value:  vals[j],
			}
		}
		orConds[i] = And(andConds...)
	}
	return Or(orConds...)
}

// buildWhere generates a WHERE clause from conditions, joining them with AND.
// Returns the clause SQL (no "WHERE" prefix), args, and next placeholder position.
//
// qualifier is the reference a correlated [Exists] condition qualifies its
// correlation column with — the enclosing statement's table alias when it has
// one, its formatted table name otherwise. It is unused by every other
// condition kind.
//
//nolint:unparam // pos return enables future composition of WHERE with trailing clauses
func buildWhere(d Dialect, conditions []Condition, pos int, qualifier string) (string, []any, int) {
	if len(conditions) == 0 {
		return "", nil, pos
	}

	parts := make([]string, 0, len(conditions))
	var args []any

	for _, cond := range conditions {
		clause, condArgs, nextPos := expandCondition(d, cond, pos, qualifier)
		if clause != "" {
			parts = append(parts, clause)
			args = append(args, condArgs...)
			pos = nextPos
		}
	}

	return strings.Join(parts, " AND "), args, pos
}

// expandCondition resolves a single Condition into SQL, replacing $ tokens
// with dialect-specific positional placeholders. qualifier is the enclosing
// statement's reference to its own table, used only by [Exists].
//
// A [Condition.Raw] clause is parenthesised on the way out, because callers
// join conditions with AND and AND binds tighter than OR: an opaque predicate
// whose top-level operator is OR would otherwise pull its neighbours into the
// disjunction. Wrapping here rather than in [Raw] keeps
// [PrefixConditions] working against the unwrapped clause.
func expandCondition(d Dialect, cond Condition, pos int, qualifier string) (string, []any, int) {
	clause, args, next := expandConditionClause(d, cond, pos, qualifier)
	if cond.Raw && clause != "" {
		clause = "(" + clause + ")"
	}
	return clause, args, next
}

// expandConditionClause is [expandCondition] without the Raw parenthesisation,
// split out so the wrap has a single exit to apply itself to.
func expandConditionClause(d Dialect, cond Condition, pos int, qualifier string) (string, []any, int) {
	if andConds, ok := cond.IsAnd(); ok {
		return expandComposition(d, andConds, " AND ", pos, qualifier)
	}
	if orConds, ok := cond.IsOr(); ok {
		return expandComposition(d, orConds, " OR ", pos, qualifier)
	}
	if cond.Value == nil {
		return cond.Clause, nil, pos
	}
	if e, ok := cond.Value.(existsCondition); ok {
		return expandExists(d, e, pos, qualifier)
	}
	if r, ok := cond.Value.(Range); ok {
		return expandRange(d, cond.Clause, r, pos)
	}
	if sq, ok := cond.Value.(Subquery); ok {
		return expandSubquery(d, cond.Clause, sq, pos)
	}
	if vals, ok := cond.Value.([]any); ok {
		return expandSlice(d, cond.Clause, vals, pos)
	}
	// Scalar: one arg, read in either PRD 11.5 token form, like a
	// multi-arg clause or a Subquery.
	return renumberTokens(d, cond.Clause, []any{cond.Value}, pos)
}

func expandComposition(d Dialect, conds []Condition, sep string, pos int, qualifier string) (string, []any, int) {
	parts := make([]string, 0, len(conds))
	var args []any

	for _, c := range conds {
		clause, condArgs, nextPos := expandCondition(d, c, pos, qualifier)
		if clause != "" {
			parts = append(parts, clause)
			args = append(args, condArgs...)
			pos = nextPos
		}
	}

	return "(" + strings.Join(parts, sep) + ")", args, pos
}

func expandRange(d Dialect, clause string, r Range, pos int) (string, []any, int) {
	vals := []any{r.Start, r.End}
	result, _, nextPos := replaceDollars(d, clause, vals, pos)
	return result, vals, nextPos
}

// expandExists renders a correlated EXISTS predicate. Every
// [CorrelationToken] in the subquery becomes the enclosing statement's
// qualifier plus the quoted correlation column, and the subquery's $ tokens
// are renumbered into the enclosing statement's positional sequence, so its
// args participate in Conditions order like any other multi-arg condition.
func expandExists(d Dialect, e existsCondition, pos int, qualifier string) (string, []any, int) {
	// Renumber before substituting, so a qualifier that happens to contain a $
	// cannot be mistaken for a placeholder token.
	rendered, args, nextPos := renumberTokens(d, e.Subquery.SQL, e.Subquery.Args, pos)
	rendered = strings.ReplaceAll(rendered, CorrelationToken, qualifier+"."+d.QuoteIdentifier(e.CorrelationColumn))
	return "EXISTS (" + rendered + ")", args, nextPos
}

// expandSubquery splices a nested SELECT into the clause's "$" token. The
// subquery's own "$" tokens are renumbered into the enclosing statement's
// positional sequence first, exactly as [expandExists] does, so its args take
// their place in Conditions order and the caller never needs to know the
// subquery's position.
func expandSubquery(d Dialect, clause string, sq Subquery, pos int) (string, []any, int) {
	rendered, args, nextPos := renumberTokens(d, sq.SQL, sq.Args, pos)
	return strings.Replace(clause, "$", "("+rendered+")", 1), args, nextPos
}

func expandSlice(d Dialect, clause string, vals []any, pos int) (string, []any, int) {
	// Check NOT IN before IN — " NOT IN $" also has suffix " IN $"
	if strings.HasSuffix(clause, " NOT IN $") {
		return expandIN(d, clause[:len(clause)-len(" NOT IN $")], vals, pos, "NOT IN")
	}
	if strings.HasSuffix(clause, " IN $") {
		return expandIN(d, clause[:len(clause)-len(" IN $")], vals, pos, "IN")
	}
	// Multi-placeholder: bare "$" tokens in order, or "$1"…"$k".
	return renumberTokens(d, clause, vals, pos)
}

func expandIN(d Dialect, prefix string, vals []any, pos int, keyword string) (string, []any, int) {
	if len(vals) == 0 {
		if keyword == "IN" {
			return "1 = 0", nil, pos // always false
		}
		return "1 = 1", nil, pos // always true
	}
	placeholders := d.PlaceholderList(pos, len(vals))
	result := prefix + " " + keyword + " (" + placeholders + ")"
	return result, vals, pos + len(vals)
}

// renumberTokens numbers the "$" tokens of a caller-written [Subquery]'s
// SQL, or of a scalar or multi-arg condition clause such as [Raw]'s,
// into the enclosing statement's positional sequence from pos.
//
// PRD §11.5 allows two token forms, not mixed in one text: one bare "$" per
// arg, numbered in order, or "$1"…"$k", where "$N" is vals[N-1] and may
// repeat or appear out of order. A text with any "$N" token is in the
// numbered form. PostgreSQL renders "$N" as its absolute position in the
// enclosing statement, so a repeated "$N" reuses one position and the args
// stay as written; a "?" dialect emits one "?" per token and lists the args
// in token order, so a repeated one is duplicated and one no token
// references is left out.
//
// Nothing is checked or repaired: SQL outside those forms is the caller's to
// fix and reaches the database as written, where most shapes fail and some
// run (PRD §11.5). The bare form numbers the first len(vals) tokens, leaves
// any further "$" as written and returns every arg. The numbered form copies
// a token it cannot read as one of the args (a bare "$", "$0", a "$N" past
// len(vals)) through as written.
func renumberTokens(d Dialect, text string, vals []any, pos int) (string, []any, int) {
	tokens := placeholderTokens(d, text)
	ends := make([]int, len(tokens))
	numbered := false
	for i, at := range tokens {
		end := at + 1
		for end < len(text) && text[end] >= '0' && text[end] <= '9' {
			end++
		}
		ends[i] = end
		numbered = numbered || end > at+1
	}
	if !numbered {
		return replaceTokens(d, text, tokens, vals, pos)
	}

	// A dialect whose placeholder is the same at every position ("?") binds
	// args in token order; a numbered one ("$n") binds them by position.
	// [SubqueryWhere]'s bareDialect is the former, so a numbered subquery
	// nested in a relationship filter becomes bare "$" tokens in token order,
	// which the enclosing [Exists] then numbers.
	positional := d.Placeholder(1) == d.Placeholder(2)
	args := vals
	if positional {
		args = make([]any, 0, len(tokens))
	}
	var b strings.Builder
	b.Grow(len(text) + len(tokens)*4)
	last := 0
	for i, at := range tokens {
		n, err := strconv.Atoi(text[at+1 : ends[i]])
		if err != nil || n < 1 || n > len(vals) {
			continue
		}
		b.WriteString(text[last:at])
		if positional {
			b.WriteString(d.Placeholder(pos + len(args)))
			args = append(args, vals[n-1])
		} else {
			b.WriteString(d.Placeholder(pos + n - 1))
		}
		last = ends[i]
	}
	b.WriteString(text[last:])
	return b.String(), args, pos + len(args)
}

// replaceDollars numbers the first len(vals) "$" placeholder tokens in clause
// (see [placeholderTokens]) and copies everything else through.
func replaceDollars(d Dialect, clause string, vals []any, pos int) (string, []any, int) {
	return replaceTokens(d, clause, placeholderTokens(d, clause), vals, pos)
}

// replaceTokens replaces the first len(vals) of tokens, byte offsets of "$"
// in clause, with the dialect's placeholders numbered from pos.
func replaceTokens(d Dialect, clause string, tokens []int, vals []any, pos int) (string, []any, int) {
	var b strings.Builder
	b.Grow(len(clause) + len(vals)*4)
	last := 0
	for i, at := range tokens {
		if i == len(vals) {
			break
		}
		b.WriteString(clause[last:at])
		b.WriteString(d.Placeholder(pos))
		pos++
		last = at + 1
	}
	b.WriteString(clause[last:])
	return b.String(), vals, pos
}

// placeholderTokens returns the byte offset of every "$" placeholder token in
// text: each "$" outside a string literal ('...'), a quoted identifier ("..."
// or `...`), a comment (-- or /* */) and a PostgreSQL dollar-quoted string
// ($$...$$ or $tag$...$tag$). A "$" inside one of those is the SQL's own text
// — a JSON path such as '$.tier', a column named "price$usd" — not a token.
//
// Quotes are matched the SQL-standard way, a doubled quote escaping itself.
// When [Dialect.BackslashEscapes] reports true (MySQL), a backslash also
// escapes the byte after it inside a string literal ('...' or "..."), as
// MySQL's default sql_mode reads it and as the MySQL filter qualifier writes a
// quote (it deparses a doubled one as \'). PostgreSQL and SQLite read a
// backslash there literally ('C:\' is a whole literal), so it escapes nothing
// on them.
func placeholderTokens(d Dialect, text string) []int {
	backslash := d.BackslashEscapes()
	var tokens []int
	for i := 0; i < len(text); i++ {
		switch c := text[i]; c {
		case '\'', '"', '`':
			i = quotedEnd(text, i, c, backslash && c != '`')
		case '-':
			if i+1 < len(text) && text[i+1] == '-' {
				i = commentEnd(text, i+2, "\n")
			}
		case '/':
			if i+1 < len(text) && text[i+1] == '*' {
				i = commentEnd(text, i+2, "*/")
			}
		case '$':
			if end, ok := dollarQuoteEnd(text, i); ok {
				i = end
				continue
			}
			tokens = append(tokens, i)
		}
	}
	return tokens
}

// quotedEnd returns the offset of the quote closing the literal or identifier
// that opens at text[start], or the last offset when it is unterminated. When
// backslash is set, a backslash escapes the byte after it.
func quotedEnd(text string, start int, quote byte, backslash bool) int {
	for i := start + 1; i < len(text); i++ {
		if backslash && text[i] == '\\' {
			i++
			continue
		}
		if text[i] != quote {
			continue
		}
		if i+1 < len(text) && text[i+1] == quote {
			i++
			continue
		}
		return i
	}
	return len(text) - 1
}

// commentEnd returns the offset of the last byte of closer at or after from,
// or the last offset when the comment runs to the end of text.
func commentEnd(text string, from int, closer string) int {
	if j := strings.Index(text[from:], closer); j >= 0 {
		return from + j + len(closer) - 1
	}
	return len(text) - 1
}

// dollarQuoteEnd reports whether text[start] opens a PostgreSQL dollar-quoted
// string — "$$" or "$tag$", the tag starting with a letter or underscore —
// that is closed later in text, and returns the offset of its closing "$". A
// "$" followed by a digit never opens one: that is a positional parameter.
func dollarQuoteEnd(text string, start int) (int, bool) {
	i := start + 1
	if i < len(text) && isIdentifierByte(text[i]) && (text[i] < '0' || text[i] > '9') {
		for i < len(text) && isIdentifierByte(text[i]) {
			i++
		}
	}
	if i >= len(text) || text[i] != '$' {
		return 0, false
	}
	opener := text[start : i+1]
	j := strings.Index(text[i+1:], opener)
	if j < 0 {
		return 0, false
	}
	return i + 1 + j + len(opener) - 1, true
}

// appendWhere writes " WHERE ..." to b if conditions are non-empty, returning args.
// qualifier is passed through to [buildWhere] for correlated [Exists] conditions.
//
//nolint:unparam // pos kept for consistency with buildWhere; callers may need non-1 start in future builders
func appendWhere(b *strings.Builder, d Dialect, conditions []Condition, pos int, qualifier string) []any {
	if len(conditions) == 0 {
		return nil
	}
	where, args, _ := buildWhere(d, conditions, pos, qualifier)
	b.WriteString(" WHERE ")
	b.WriteString(where)
	return args
}

func writeSelectColumns(b *strings.Builder, d Dialect, columns []string) {
	if len(columns) == 0 {
		b.WriteString("*")
		return
	}
	cols := sortedCopy(columns)
	for i, col := range cols {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(d.QuoteIdentifier(col))
	}
}

func writeJoinColumns(b *strings.Builder, d Dialect, alias string, primaryCols []string, joins []JoinClause) {
	first := true
	for _, col := range sortedCopy(primaryCols) {
		if !first {
			b.WriteString(", ")
		}
		first = false
		b.WriteString(alias)
		b.WriteString(".")
		b.WriteString(d.QuoteIdentifier(col))
		b.WriteString(" AS ")
		b.WriteString(d.QuoteIdentifier(alias + "." + col))
	}
	for _, join := range joins {
		for _, col := range sortedCopy(join.Columns) {
			if !first {
				b.WriteString(", ")
			}
			first = false
			b.WriteString(join.Alias)
			b.WriteString(".")
			b.WriteString(d.QuoteIdentifier(col))
			b.WriteString(" AS ")
			b.WriteString(d.QuoteIdentifier(join.Alias + "." + col))
		}
	}
}

func writeJoinClauses(b *strings.Builder, d Dialect, joins []JoinClause) {
	for _, join := range joins {
		b.WriteString(" LEFT JOIN ")
		b.WriteString(d.FormatTable(join.Table))
		b.WriteString(" ")
		b.WriteString(join.Alias)
		b.WriteString(" ON ")
		b.WriteString(join.On)
		if join.SoftDelete != nil {
			writeJoinSoftDeleteFilter(b, d, join.Alias, join.SoftDelete)
		}
	}
}

func writeJoinSoftDeleteFilter(b *strings.Builder, d Dialect, alias string, sd *SoftDeleteOptions) {
	col := alias + "." + d.QuoteIdentifier(sd.Column)
	b.WriteString(" AND ")
	switch sd.Type {
	case SoftDeleteTimestamp:
		b.WriteString(col)
		b.WriteString(" IS NULL")
	case SoftDeleteBool:
		b.WriteString(col)
		b.WriteString(" = FALSE")
	case SoftDeleteInteger:
		b.WriteString(col)
		b.WriteString(" = 0")
	}
}

// writeOrderBy writes the ORDER BY clause. Every column is quoted; a non-empty
// qualifier (a JOIN's table alias) is prepended to each, unquoted.
func writeOrderBy(b *strings.Builder, d Dialect, sorts []Sort, qualifier string) {
	if len(sorts) == 0 {
		return
	}
	b.WriteString(" ORDER BY ")
	for i, s := range sorts {
		if i > 0 {
			b.WriteString(", ")
		}
		if qualifier != "" {
			b.WriteString(qualifier)
			b.WriteString(".")
		}
		b.WriteString(d.QuoteIdentifier(s.Column))
		b.WriteString(" ")
		b.WriteString(string(s.Direction))
	}
}

func writeLimitOffset(b *strings.Builder, limit, offset *int) {
	if limit != nil {
		fmt.Fprintf(b, " LIMIT %d", *limit)
	}
	if offset != nil {
		fmt.Fprintf(b, " OFFSET %d", *offset)
	}
}

// writeLockClause appends the dialect-specific row-level lock clause when the
// mode is not [LockNone]. Dialects that do not support a given mode return an
// empty fragment, in which case nothing is appended.
func writeLockClause(b *strings.Builder, d Dialect, mode LockMode) {
	if mode == LockNone {
		return
	}
	clause := d.LockClause(mode)
	if clause == "" {
		return
	}
	b.WriteString(" ")
	b.WriteString(clause)
}

func writeReturning(b *strings.Builder, d Dialect, columns []string) {
	if d.SupportsReturning() && len(columns) > 0 {
		b.WriteString(" ")
		b.WriteString(d.ReturningClause(columns))
	}
}

func writeQuotedList(b *strings.Builder, d Dialect, columns []string) {
	for i, col := range columns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(d.QuoteIdentifier(col))
	}
}

func sortedCopy(s []string) []string {
	cp := make([]string, len(s))
	copy(cp, s)
	sort.Strings(cp)
	return cp
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortColumnsWithValues(columns []string, values []any) ([]string, []any) {
	type pair struct {
		col string
		val any
	}
	pairs := make([]pair, len(columns))
	for i := range columns {
		pairs[i] = pair{col: columns[i], val: values[i]}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].col < pairs[j].col
	})
	sortedCols := make([]string, len(pairs))
	sortedVals := make([]any, len(pairs))
	for i, p := range pairs {
		sortedCols[i] = p.col
		sortedVals[i] = p.val
	}
	return sortedCols, sortedVals
}

// sortedColumnsOrder returns the sorted columns and an index mapping from
// sorted position to original position.
func sortedColumnsOrder(columns []string) ([]string, []int) {
	type indexed struct {
		col string
		idx int
	}
	items := make([]indexed, len(columns))
	for i, col := range columns {
		items[i] = indexed{col: col, idx: i}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].col < items[j].col
	})
	sorted := make([]string, len(items))
	order := make([]int, len(items))
	for i, item := range items {
		sorted[i] = item.col
		order[i] = item.idx
	}
	return sorted, order
}

func reorderRow(row []any, order []int) []any {
	reordered := make([]any, len(order))
	for i, origIdx := range order {
		reordered[i] = row[origIdx]
	}
	return reordered
}

// SubqueryWhere renders conditions as an AND-joined SQL fragment for embedding
// in the WHERE clause of an [Exists] subquery. It returns the fragment (no
// "WHERE" prefix) and the args it consumes, in fragment order.
//
// Two properties make the result composable inside an Exists subquery, which
// is why the ordinary builders cannot stand in for it:
//
//   - Every column reference is qualified with alias. Unqualified names inside
//     a correlated subquery silently bind to the enclosing statement's table
//     when the subquery's own table lacks the column, turning a filter on the
//     related row into a filter on the parent one.
//   - Every placeholder renders as the bare "$" token rather than a positional
//     one, because [Exists] renumbers the subquery's tokens into the enclosing
//     statement's sequence. Rendering positions here would number them twice.
//
// A nested Exists among the conditions resolves its own correlation against
// alias, so a filter one hop deeper correlates to this subquery's row rather
// than to the outermost statement's.
func SubqueryWhere(d Dialect, alias string, conditions []Condition) (string, []any) {
	if len(conditions) == 0 {
		return "", nil
	}
	clause, args, _ := buildWhere(bareDialect{d}, PrefixConditions(alias, conditions), 1, alias)
	return clause, args
}

// bareDialect wraps a Dialect so placeholders render as the bare "$" token.
// Everything else — quoting, table formatting, IN/NOT IN shape — delegates to
// the wrapped dialect, so a fragment differs from a top-level clause only in
// how its placeholders are spelled.
type bareDialect struct {
	Dialect
}

// Placeholder returns the bare "$" token regardless of position.
func (bareDialect) Placeholder(int) string { return "$" }

// PlaceholderList returns count bare "$" tokens, comma-separated.
func (bareDialect) PlaceholderList(_, count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("$, ", count), ", ")
}
