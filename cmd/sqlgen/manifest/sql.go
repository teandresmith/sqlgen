package manifest

import (
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	sqlpkg "github.com/teandresmith/sqlgen/sql"
)

// sql.go renders canonical per-method SQL bodies for the manifest. Per
// PRD §30.7 the manifest exposes one SQL string per dialect per method,
// captured from the same runtime sql builders the generated _gen.go code
// calls, so the manifest and the generated code agree by construction.
//
// A body describes the statement the *named method* issues. Methods that
// issue none of their own — Paginate and Connection compose Count + GetMany —
// carry no body at all rather than a plausible-looking invention; sql_bodies
// is optional in the schema and MCP reports -32005 for them.
//
// Clauses the generated code assembles from caller input at call time are
// rendered as placeholder tokens rather than a representative expansion. An
// expansion would name columns and placeholder positions the statement does
// not actually carry, and the real
// column set is already published in the entity's columns[] block.
const (
	tokenFilter         = "<filter>"          // caller-supplied filter conditions
	tokenSort           = "<sort>"            // caller-supplied ORDER BY
	tokenLimit          = "<limit>"           // runtime row limit, interpolated as a literal
	tokenSet            = "<set>"             // UPDATE SET assignments for the fields the caller set
	tokenColumns        = "<columns>"         // INSERT column list for the fields the caller set
	tokenValues         = "<values>"          // INSERT value list (one row, or the row list for CreateMany)
	tokenConflictTarget = "<conflict_target>" // conflict columns the target argument selects
	tokenExcluded       = "<excluded>"        // DO UPDATE SET assignments derived from that target
	tokenColumn         = "<column>"          // Increment's target column
	tokenIDs            = "<ids>"             // batch PK list, single-column PK
	tokenPKs            = "<pks>"             // batch PK list, composite PK
)

// dialectFor maps the config dialect to the runtime sql.Dialect used by the
// builders below.
func dialectFor(cfg *config.RootConfig) sqlpkg.Dialect {
	switch cfg.Input.Dialect {
	case config.DialectPostgres:
		return sqlpkg.NewPostgresDialect()
	case config.DialectMySQL:
		return sqlpkg.NewMySQLDialect()
	case config.DialectSQLite:
		return sqlpkg.NewSQLiteDialect()
	}
	// Unknown dialect — return postgres as a stable fallback so we still
	// produce a SQL body. The builder is config-validated upstream.
	return sqlpkg.NewPostgresDialect()
}

func sqlTable(tc gen.TableContext) sqlpkg.Table {
	return sqlpkg.Table{Schema: tc.Schema, Name: tc.TableName}
}

func columnNames(tc gen.TableContext) []string {
	out := make([]string, 0, len(tc.Columns))
	for _, c := range tc.Columns {
		out = append(out, c.Name)
	}
	return out
}

func placeholderEqCondition(d sqlpkg.Dialect, col string, pos int) sqlpkg.Condition {
	return sqlpkg.Condition{
		Clause: d.QuoteIdentifier(col) + " = " + d.Placeholder(pos),
	}
}

// softDeleteClause returns the literal `<col> IS NULL` predicate appended by
// the generator to every read query when soft delete is configured.
func softDeleteClause(d sqlpkg.Dialect, tc gen.TableContext) string {
	if tc.SoftDelete == nil {
		return ""
	}
	switch tc.SoftDelete.Strategy {
	case config.SoftDeleteTimestamp:
		return d.QuoteIdentifier(tc.SoftDelete.Column) + " IS NULL"
	case config.SoftDeleteBool:
		return d.QuoteIdentifier(tc.SoftDelete.Column) + " = FALSE"
	case config.SoftDeleteInteger:
		return d.QuoteIdentifier(tc.SoftDelete.Column) + " = 0"
	}
	return d.QuoteIdentifier(tc.SoftDelete.Column) + " IS NULL"
}

// notDeletedClause is softDeleteClause's complement — the predicate
// RestoreWhere appends so it only touches rows that are currently deleted
// (table/delete.go.tmpl).
func notDeletedClause(d sqlpkg.Dialect, tc gen.TableContext) string {
	if tc.SoftDelete == nil {
		return ""
	}
	switch tc.SoftDelete.Strategy {
	case config.SoftDeleteBool:
		return d.QuoteIdentifier(tc.SoftDelete.Column) + " = TRUE"
	case config.SoftDeleteInteger:
		return d.QuoteIdentifier(tc.SoftDelete.Column) + " = 1"
	case config.SoftDeleteTimestamp:
		return d.QuoteIdentifier(tc.SoftDelete.Column) + " IS NOT NULL"
	}
	return d.QuoteIdentifier(tc.SoftDelete.Column) + " IS NOT NULL"
}

// pkConditions builds per-PK-column conditions with sequential placeholders
// starting at the given position.
func pkConditions(d sqlpkg.Dialect, tc gen.TableContext, startPos int) []sqlpkg.Condition {
	conds := make([]sqlpkg.Condition, 0, len(tc.PKColumns))
	pos := startPos
	for _, c := range tc.PKColumns {
		conds = append(conds, placeholderEqCondition(d, c.Name, pos))
		pos++
	}
	return conds
}

// batchPKCondition mirrors the predicate the *Many methods build for their PK
// list: sql.Where(pk).In(...) for a single-column PK, and
// sql.BuildCompositePKBatchCondition — tuple-IN where the dialect supports it,
// an expanded OR chain where it does not — for a composite one. The list
// itself is runtime-length, so it stands as a token.
func batchPKCondition(d sqlpkg.Dialect, tc gen.TableContext) sqlpkg.Condition {
	if !tc.CompositePK {
		return sqlpkg.Condition{Clause: d.QuoteIdentifier(tc.PKColumns[0].Name) + " IN (" + tokenIDs + ")"}
	}
	if !d.SupportsTupleIN() {
		return sqlpkg.Condition{Clause: tokenPKs}
	}
	cols := make([]string, 0, len(tc.PKColumns))
	for _, c := range tc.PKColumns {
		cols = append(cols, d.QuoteIdentifier(c.Name))
	}
	return sqlpkg.Condition{Clause: "(" + strings.Join(cols, ", ") + ") IN (" + tokenPKs + ")"}
}

// trailingSoftDelete adds the soft-delete predicate to the conditions slice
// when configured. Reads apply it; the by-PK mutations do not — Update,
// SoftDelete, Restore and HardDelete all pass PK conditions alone.
func trailingSoftDelete(d sqlpkg.Dialect, tc gen.TableContext, conds []sqlpkg.Condition) []sqlpkg.Condition {
	if tc.SoftDelete == nil {
		return conds
	}
	return append(conds, sqlpkg.Condition{Clause: softDeleteClause(d, tc)})
}

// filterConditions returns the read-path WHERE: the caller's filter token plus
// the soft-delete guard the generated reads append when configured.
func filterConditions(d sqlpkg.Dialect, tc gen.TableContext) []sqlpkg.Condition {
	return trailingSoftDelete(d, tc, []sqlpkg.Condition{{Clause: tokenFilter}})
}

// filterOnly is the mutation-path WHERE: the caller's filter and nothing else.
// UpdateWhere and HardDeleteWhere pass filter.ToConditions() straight through,
// so neither carries the read-path soft-delete guard. SoftDeleteWhere and
// RestoreWhere each append their own guard explicitly — see below.
func filterOnly() []sqlpkg.Condition {
	return []sqlpkg.Condition{{Clause: tokenFilter}}
}

// whereClause renders the WHERE suffix for the token conditions above exactly
// as sql.buildWhere does — a plain " AND " join. None of these conditions
// carries a Value, so buildWhere's Range / Subquery / slice expansion never
// applies and the join is the whole of it.
func whereClause(conds []sqlpkg.Condition) string {
	if len(conds) == 0 {
		return ""
	}
	parts := make([]string, 0, len(conds))
	for _, c := range conds {
		parts = append(parts, c.Clause)
	}
	return " WHERE " + strings.Join(parts, " AND ")
}

// insertReturning names the columns the generated INSERT returns. Create,
// CreateMany and Upsert resolve the new row's PK and then re-Get it, so they
// return the PK column alone — never the full row. Composite-PK entities
// resolve nothing (the caller supplied the key) and MySQL resolves through
// LastInsertId, so both yield no RETURNING at all.
func insertReturning(d sqlpkg.Dialect, tc gen.TableContext) []string {
	if tc.CompositePK || !d.SupportsReturning() {
		return nil
	}
	return []string{tc.PKColumns[0].Name}
}

// --- SELECT-shaped bodies ---

// sqlGetByPK renders Get, which delegates to GetMany with a PK filter and a
// limit of one.
func sqlGetByPK(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	conds := trailingSoftDelete(d, tc, pkConditions(d, tc, 1))
	q, _ := sqlpkg.BuildSelect(d, sqlTable(tc), sqlpkg.SelectOptions{
		Columns:    columnNames(tc),
		Conditions: conds,
	})
	return q + " LIMIT 1"
}

// sqlGetMany renders `SELECT cols FROM t WHERE <filter> [AND <soft-delete>]
// ORDER BY <sort> LIMIT <limit>`. The limit is a token rather than a
// placeholder because sql.writeLimitOffset interpolates it as a literal.
func sqlGetMany(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	q, _ := sqlpkg.BuildSelect(d, sqlTable(tc), sqlpkg.SelectOptions{
		Columns:    columnNames(tc),
		Conditions: filterConditions(d, tc),
	})
	return q + " ORDER BY " + tokenSort + " LIMIT " + tokenLimit
}

// sqlStream mirrors sqlGetMany without the LIMIT — Stream iterates the driver
// cursor unbounded (PRD §9.4a).
func sqlStream(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	q, _ := sqlpkg.BuildSelect(d, sqlTable(tc), sqlpkg.SelectOptions{
		Columns:    columnNames(tc),
		Conditions: filterConditions(d, tc),
	})
	return q + " ORDER BY " + tokenSort
}

func sqlCount(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	q, _ := sqlpkg.BuildCount(d, sqlTable(tc), filterConditions(d, tc))
	return q
}

// sqlExistsByPK renders Exists, which checks one primary key.
func sqlExistsByPK(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	conds := trailingSoftDelete(d, tc, pkConditions(d, tc, 1))
	q, _ := sqlpkg.BuildExists(d, sqlTable(tc), conds)
	return q
}

// sqlExistsWhere renders ExistsWhere, the filter-shaped sibling of Exists.
func sqlExistsWhere(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	q, _ := sqlpkg.BuildExists(d, sqlTable(tc), filterConditions(d, tc))
	return q
}

// --- INSERT ---

// sqlCreate renders the single-row INSERT. Column and value lists are tokens:
// the generated Create builds them from the omittable input fields the caller
// actually set, so no static list describes the statement it issues.
func sqlCreate(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	return insertHead(d, tc) + " VALUES (" + tokenValues + ")" + returningSuffix(d, insertReturning(d, tc))
}

// sqlCreateMany renders the multi-row INSERT that CreateMany issues per batch
// via sql.BuildMultiInsert — the same statement with a row list in place of
// the single row.
func sqlCreateMany(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	return insertHead(d, tc) + " VALUES " + tokenValues + returningSuffix(d, insertReturning(d, tc))
}

// sqlUpsert renders INSERT ... ON CONFLICT (...) DO UPDATE SET ... . The
// conflict columns and the assignments derived from them are selected by the
// method's `target` argument at call time, so both are tokens: Upsert is one
// generated method over a ConflictTarget enum, not one method per target.
// MySQL's dialect renders the matching ON DUPLICATE KEY UPDATE head.
func sqlUpsert(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	return insertHead(d, tc) + " VALUES (" + tokenValues + ") " + upsertHead(d) +
		returningSuffix(d, insertReturning(d, tc))
}

// sqlUpsertMany renders the batched INSERT ... ON CONFLICT form. It carries no
// RETURNING, where sqlCreateMany does: UpsertMany never reads its keys out of
// the statement. RETURNING omits every row that took the DO NOTHING branch and
// MySQL has none at all, so the written rows are named from the inputs or read
// back by the conflict target afterwards (PRD §9.5, §9.7).
func sqlUpsertMany(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	return insertHead(d, tc) + " VALUES " + tokenValues + " " + upsertHead(d)
}

// upsertHead renders the dialect's conflict head with the target-dependent
// halves left as tokens. Which form applies is read back off the dialect
// itself rather than hardcoded here, so a dialect that changes its upsert
// syntax changes this with it.
func upsertHead(d sqlpkg.Dialect) string {
	probe := sqlpkg.UpsertClauseOptions{ConflictKeys: []string{"c"}, UpdateColumns: []string{"u"}}
	if strings.HasPrefix(d.UpsertClause(probe), "ON CONFLICT") {
		return "ON CONFLICT (" + tokenConflictTarget + ") DO UPDATE SET " + tokenExcluded
	}
	return "ON DUPLICATE KEY UPDATE " + tokenExcluded
}

func insertHead(d sqlpkg.Dialect, tc gen.TableContext) string {
	return "INSERT INTO " + d.FormatTable(sqlTable(tc)) + " (" + tokenColumns + ")"
}

// returningSuffix defers to the dialect's own ReturningClause so the manifest
// renders RETURNING exactly as sql.writeReturning does.
func returningSuffix(d sqlpkg.Dialect, cols []string) string {
	if !d.SupportsReturning() || len(cols) == 0 {
		return ""
	}
	return " " + d.ReturningClause(cols)
}

// --- UPDATE ---

// sqlUpdateByPK renders Update and, per item, UpdateMany. The SET list is a
// token — only fields where IsSet() reports true reach the statement — and
// there is no RETURNING: the generated Update re-Gets the row instead.
func sqlUpdateByPK(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	return updateHead(d, tc) + whereClause(pkConditions(d, tc, 1))
}

// sqlUpdateWhere renders UpdateWhere, which returns the affected PKs so the
// generated code can re-Get them.
func sqlUpdateWhere(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	return updateHead(d, tc) + whereClause(filterOnly()) + returningSuffix(d, affectedPKColumns(d, tc))
}

// sqlIncrement renders the canonical atomic increment. sql.BuildIncrement
// places the amount at the first placeholder and starts the WHERE at the
// second — the reverse of every other mutation body.
func sqlIncrement(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	col := tokenColumn
	return "UPDATE " + d.FormatTable(sqlTable(tc)) + " SET " + col + " = " + col + " + " + d.Placeholder(1) +
		whereClause(pkConditions(d, tc, 2))
}

func updateHead(d sqlpkg.Dialect, tc gen.TableContext) string {
	return "UPDATE " + d.FormatTable(sqlTable(tc)) + " SET " + tokenSet
}

// affectedPKColumns names what the *Where mutations RETURNING — the PK columns
// they collect to re-Get the affected rows. Empty on dialects without
// RETURNING, where the generator pre-reads the PKs instead.
func affectedPKColumns(d sqlpkg.Dialect, tc gen.TableContext) []string {
	if !d.SupportsReturning() {
		return nil
	}
	out := make([]string, 0, len(tc.PKColumns))
	for _, c := range tc.PKColumns {
		out = append(out, c.Name)
	}
	return out
}

// --- soft delete / restore / hard delete ---

func sqlSoftDelete(cfg *config.RootConfig, tc gen.TableContext, conds []sqlpkg.Condition, returning []string) string {
	d := dialectFor(cfg)
	q, _ := sqlpkg.BuildSoftDelete(d, sqlTable(tc), sqlpkg.SoftDeleteOptions{
		Column:           tc.SoftDelete.Column,
		Type:             sqlpkg.SoftDeleteType(tc.SoftDelete.Strategy),
		Conditions:       conds,
		ReturningColumns: returning,
	})
	return q
}

func sqlSoftDeleteByPK(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	return sqlSoftDelete(cfg, tc, pkConditions(d, tc, 1), nil)
}

func sqlSoftDeleteMany(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	return sqlSoftDelete(cfg, tc, []sqlpkg.Condition{batchPKCondition(d, tc)}, nil)
}

// sqlSoftDeleteWhere carries the extra `IS NULL` guard the generated
// SoftDeleteWhere appends so it never re-deletes an already-deleted row. Spelled
// out rather than borrowed from filterConditions: the predicate coincides with
// the read-path guard, but it is here for its own reason.
func sqlSoftDeleteWhere(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	conds := []sqlpkg.Condition{{Clause: tokenFilter}, {Clause: softDeleteClause(d, tc)}}
	return sqlSoftDelete(cfg, tc, conds, affectedPKColumns(d, tc))
}

// sqlRestore renders the UPDATE that clears the soft-delete column.
func sqlRestore(cfg *config.RootConfig, tc gen.TableContext, conds []sqlpkg.Condition, returning []string) string {
	d := dialectFor(cfg)
	q, _ := sqlpkg.BuildUpdate(d, sqlTable(tc), sqlpkg.UpdateOptions{
		SetClauses:       map[string]any{tc.SoftDelete.Column: nil},
		Conditions:       conds,
		ReturningColumns: returning,
	})
	return q
}

func sqlRestoreByPK(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	return sqlRestore(cfg, tc, pkConditions(d, tc, 2), nil)
}

func sqlRestoreMany(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	return sqlRestore(cfg, tc, []sqlpkg.Condition{batchPKCondition(d, tc)}, nil)
}

// sqlRestoreWhere carries the `IS NOT NULL` guard the generated RestoreWhere
// appends so it only touches rows that are currently deleted.
func sqlRestoreWhere(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	conds := []sqlpkg.Condition{{Clause: tokenFilter}, {Clause: notDeletedClause(d, tc)}}
	return sqlRestore(cfg, tc, conds, affectedPKColumns(d, tc))
}

func sqlHardDelete(cfg *config.RootConfig, tc gen.TableContext, conds []sqlpkg.Condition, returning []string) string {
	d := dialectFor(cfg)
	q, _ := sqlpkg.BuildHardDelete(d, sqlTable(tc), sqlpkg.HardDeleteOptions{
		Conditions:       conds,
		ReturningColumns: returning,
	})
	return q
}

func sqlHardDeleteByPK(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	return sqlHardDelete(cfg, tc, pkConditions(d, tc, 1), nil)
}

func sqlHardDeleteMany(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	return sqlHardDelete(cfg, tc, []sqlpkg.Condition{batchPKCondition(d, tc)}, nil)
}

func sqlHardDeleteWhere(cfg *config.RootConfig, tc gen.TableContext) string {
	d := dialectFor(cfg)
	return sqlHardDelete(cfg, tc, filterOnly(), affectedPKColumns(d, tc))
}

// --- view SQL ---

// sqlViewGetByPK renders a view's Get — emitted only for a view carrying a
// @pk annotation.
func sqlViewGetByPK(cfg *config.RootConfig, vc gen.ViewContext, cols []string) string {
	d := dialectFor(cfg)
	conds := make([]sqlpkg.Condition, 0, len(vc.PKColumns))
	for i, c := range vc.PKColumns {
		conds = append(conds, placeholderEqCondition(d, c.Name, i+1))
	}
	q, _ := sqlpkg.BuildSelect(d, sqlpkg.Table{Schema: vc.Schema, Name: vc.ViewName}, sqlpkg.SelectOptions{
		Columns:    cols,
		Conditions: conds,
	})
	return q + " LIMIT 1"
}

func sqlSelectView(cfg *config.RootConfig, name, schema string, cols []string, withFilter, withPage bool) string {
	d := dialectFor(cfg)
	conds := []sqlpkg.Condition{}
	if withFilter {
		conds = append(conds, sqlpkg.Condition{Clause: tokenFilter})
	}
	q, _ := sqlpkg.BuildSelect(d, sqlpkg.Table{Schema: schema, Name: name}, sqlpkg.SelectOptions{
		Columns:    cols,
		Conditions: conds,
	})
	if withPage {
		q += " ORDER BY " + tokenSort + " LIMIT " + tokenLimit
	}
	return q
}

func sqlCountView(cfg *config.RootConfig, name, schema string) string {
	d := dialectFor(cfg)
	q, _ := sqlpkg.BuildCount(d, sqlpkg.Table{Schema: schema, Name: name}, []sqlpkg.Condition{{Clause: tokenFilter}})
	return q
}

// sqlRefreshView captures the canonical REFRESH MATERIALIZED VIEW body for a
// matview's Refresh / RefreshConcurrently methods.
func sqlRefreshView(cfg *config.RootConfig, name, schema string, concurrently bool) string {
	return sqlpkg.BuildRefreshMaterializedView(dialectFor(cfg), sqlpkg.Table{Schema: schema, Name: name}, concurrently)
}

// --- helpers ---

// ContainsToken reports whether sql carries the named placeholder token. Used
// by tests to assert filter/sort/limit tokens land in expected method bodies.
func ContainsToken(sql, tok string) bool { return strings.Contains(sql, tok) }
