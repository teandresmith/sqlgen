package gen

import (
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// EventHooksContext holds pre-computed data for rendering the event_hooks template.
// Generated only when events.enabled is true in the config.
type EventHooksContext struct {
	// Package is the Go package name for generated files.
	Package string
	// ClientName is the unified client struct name (e.g., "Client").
	ClientName string
	// Imports is the deduplicated, sorted list of import paths.
	Imports []string
	// Tables holds the tables with events enabled, sorted by struct name.
	Tables []EventTableContext
	// HasRedactedTables gates the shared redactZero helper — true when any
	// event table emits a create/update redact helper (§32.3 event
	// redaction). Increment redaction blanks a value field directly and
	// does not need redactZero.
	HasRedactedTables bool
	// FilterRedactors holds one entry per table in the §32.3 filter-redaction
	// closure, sorted by struct name — the redact<T>Filter helpers the *Where
	// delete / restore arms call. Independent of Tables: the closure spans
	// every table, including ones whose own events are disabled, because an
	// event-enabled table's filter can point at one of them.
	FilterRedactors []FilterRedactorContext
	// UUIDGenExpr is the Go expression for generating event IDs — the
	// string-form call of the package's selected UUID integration at the
	// configured uuid_version (PRD §28.8, §7.4 "Generating UUID values").
	// An event ID is a string by definition (PRD §28.3), so this is always
	// the string form.
	UUIDGenExpr string
}

// EventTableContext holds per-table data for event hook generation.
type EventTableContext struct {
	// StructName is the Go struct name (e.g., "Product").
	StructName string
	// TableNameConstant is the generated constant name (e.g., "TableProducts").
	TableNameConstant string
	// TableName is the bare SQL table name (e.g., "products"), which the hook
	// publishes as Event.Table (PRD §28.3). It is not string(mc.Table): that
	// is the constant's value, "schema.table" on PostgreSQL (PRD §8.5), and
	// Event carries the schema in its own field — natsbus builds subjects as
	// {prefix}.{schema}.{table}, so a qualified Table would double the schema
	// segment.
	TableName string
	// Schema is the SQL schema name (e.g., "public").
	Schema string
	// Tenanted is true when the table opts into the tenancy auto-filter.
	// Drives the §29.6 Metadata["tenant"] injection — the event hook stamps
	// each fanned-out event from the mutated row itself (§29.6):
	// the PK struct when the tenant column is part of the primary key, the
	// structurally captured MutationContext.AffectedTenants element
	// otherwise — so SkipTenancy mutations publish tenant-stamped events.
	// Structural: non-tenanted tables emit no "tenant" key.
	Tenanted bool
	// TenantInPK is true when the tenant column is part of the primary key
	// (PRD §29.7) — the event hook then reads the tenant from each
	// AffectedPKs element instead of the AffectedTenants carrier.
	TenantInPK bool
	// TenantFieldName is the PascalCase Go field name of the tenant column
	// on the composite PK struct. Empty when Tenanted is false.
	TenantFieldName string
	// CompositePKStructName is the generated XXXPK struct name when the
	// table has a composite primary key; empty for single-column PKs.
	CompositePKStructName string
	// BatchCreateOps names the enabled ops whose §28.6 payload is a
	// []*Create<Struct>Input fanned out one element per affected row —
	// OpCreateMany and OpUpsertMany. They share one case arm because they
	// share a payload type and a fanout rule; the list is resolved here so
	// the template does not have to compose the arm from two booleans.
	// Empty when neither op is enabled, which omits the case entirely so the
	// generated hook never references a type that was never emitted.
	BatchCreateOps []string
	// HasUpdateMany controls the same gating for the OpUpdateMany branch,
	// which references the per-table Update<Struct>Item type.
	HasUpdateMany bool
	// RedactedCreateFields / RedactedUpdateFields are the Go field names the
	// per-table redact helpers clear — input-struct fields whose column
	// carries a write_only / internal access role (§32.2 EventRedacted).
	// Empty for tables with no redacted column, whose hooks stay
	// byte-unchanged (PRD §28.6 same-pointer guarantee).
	RedactedCreateFields []string
	RedactedUpdateFields []string
	// RedactedIncrementConsts holds the generated <Struct>Increment<Field>
	// constant names for redacted incrementable columns — the §28.6
	// increment leg of event redaction blanks the delta (Amount) when the
	// target column carries a write_only / internal role.
	RedactedIncrementConsts []string
	// EmitRedactCreate / EmitRedactUpdate / EmitRedactIncrement gate the
	// redact helpers + the redacting switch arms on (a) having redacted
	// fields in that input shape and (b) the referenced input type actually
	// being generated (the corresponding operations are enabled) —
	// otherwise the emitted hook would reference a type that does not exist.
	EmitRedactCreate    bool
	EmitRedactUpdate    bool
	EmitRedactIncrement bool
	// FilterRedactOps names the *Where delete / restore ops whose published
	// filter this table redacts (§28.6 shape table + §32.3) — empty unless the
	// table is in the filter-redaction closure. Gated per op, since
	// delete.go.tmpl emits each *Where under its base op's flag.
	FilterRedactOps []string
	// PassThroughOps names the ops that must carry mc.Input through an
	// explicit arm because this table is in the closure and its default arm
	// fails closed. Only ops whose §28.6 payload has nothing to redact here
	// appear — e.g. OpIncrement when the redacted column is not incrementable,
	// which would otherwise degrade to nil and regress the shape table.
	PassThroughOps []string
	// FailClosedDefault makes the redaction switch's default arm publish no
	// payload instead of mc.Input. True exactly for closure tables, where every
	// op is enumerated above it, so an op the generator does not yet know about
	// cannot leak a raw input by omission (§32.3). False elsewhere keeps
	// unredacted hooks byte-unchanged and preserves the §28.6 same-pointer
	// guarantee.
	FailClosedDefault bool
}

// BuildEventHooksContext builds the event hooks context from table contexts and config.
// Returns nil when events are not globally enabled.
//
// uuid is the package's selected UUID integration (PRD §7.4), supplying both
// the import the emitted file declares and the expression it generates event
// IDs with. It is a parameter rather than something read from cfg because the
// selection is a property of what the package's columns *resolved* to, which
// only the built contexts know — see selectUUIDIntegration. Passing the
// resolved binding is what keeps the event-hooks import and the column-resolved
// import the same library by construction, so the one-library-per-package rule
// (PRD §7.4, §4.13) cannot be broken by a file validateUUIDLibraries does not
// read.
func BuildEventHooksContext(tables []TableContext, cfg *config.RootConfig, pkg, clientName string, uuid gotype.UUIDIntegration) *EventHooksContext {
	if cfg.Events == nil || !cfg.Events.Enabled {
		return nil
	}

	// The filter-redaction closure is resolved across every table first: it
	// decides both which redact<T>Filter helpers are emitted and, per table,
	// whether the redaction switch fails closed. Membership spans every table,
	// but only a table with a hook of its own can *call* a helper, so
	// event-enablement is resolved up front and passed in as the root filter.
	hasHook := make(map[string]bool, len(tables))
	for i := range tables {
		tc := &tables[i]
		if config.ResolveTableEventsEnabled(findTableConfig(cfg, tc.TableName, tc.Schema), cfg.Events) {
			hasHook[tableKey(tc)] = true
		}
	}
	filterRedactors, inClosure := buildFilterRedactors(tables, hasHook)

	var eventTables []EventTableContext
	hasRedacted := false
	for _, t := range tables {
		if !config.ResolveTableEventsEnabled(findTableConfig(cfg, t.TableName, t.Schema), cfg.Events) {
			continue
		}
		et := buildEventTableContext(t, inClosure[t.StructName])
		hasRedacted = hasRedacted || et.EmitRedactCreate || et.EmitRedactUpdate
		eventTables = append(eventTables, et)
	}

	slices.SortFunc(eventTables, func(a, b EventTableContext) int {
		return strings.Compare(a.StructName, b.StructName)
	})

	imports := []string{
		"context",
		"maps",
		"time",
		uuid.ImportPath,
		"github.com/teandresmith/sqlgen/database",
		"github.com/teandresmith/sqlgen/event",
		"github.com/teandresmith/sqlgen/hook",
	}
	if hasTenantedEventTable(eventTables) {
		// fmt is only needed for the §29.6 tenant stringification.
		imports = append(imports, "fmt")
	}

	return &EventHooksContext{
		Package:           pkg,
		ClientName:        clientName,
		Imports:           imports,
		Tables:            eventTables,
		FilterRedactors:   filterRedactors,
		HasRedactedTables: hasRedacted,
		UUIDGenExpr:       uuid.StringExpr(cfg.Generation.UUIDVersion),
	}
}

// buildEventTableContext resolves one table's event-hook rendering data,
// including the §32.3 redaction gating: the redact helper (and its switch
// arms) is emitted only when the table has redacted fields in that input
// shape AND the referenced input type is actually generated (ops enabled).
//
// inClosure reports membership in the filter-redaction closure, which
// buildFilterRedactors resolves across every table — it cannot be derived from
// t alone, since a table with nothing redacted of its own joins when its filter
// can reach one that has.
func buildEventTableContext(t TableContext, inClosure bool) EventTableContext {
	redactCreate, redactUpdate := redactedInputFields(t)
	redactIncrement := redactedIncrementConsts(t)
	tenanted := t.Tenancy != nil && t.Tenancy.Tenanted
	tenantField := ""
	if tenanted {
		tenantField = t.Tenancy.FieldName
	}
	// UpsertMany is in this list because it publishes Create<T>Input values like
	// the other three. Leaving it out made a table that redacts a create field
	// while generating only UpsertMany resolve failClosed=false, so the §32.3
	// switch's default arm published the whole input slice un-redacted — the
	// exact leak this gate exists to close. It reaches neither default now: it
	// has an unconditional per-row arm via batchCreateOps, so this gate is
	// what decides whether that arm redacts, not whether it exists.
	emitCreate := len(redactCreate) > 0 && (t.Operations.Create || t.Operations.CreateMany || t.Operations.Upsert || t.Operations.UpsertMany)
	emitUpdate := len(redactUpdate) > 0 && (t.Operations.Update || t.Operations.UpdateMany || t.Operations.UpdateWhere)
	emitIncrement := len(redactIncrement) > 0 && t.Operations.Increment
	// A table fails closed once it redacts anything at all — any input helper,
	// or filter-closure membership. Restricting it to the filter closure would
	// leave the tables that only redact a create/update input fail-open for an
	// op added later, which is the class this fix exists to close (§32.3).
	failClosed := emitCreate || emitUpdate || emitIncrement || inClosure
	redactOps := filterRedactOps(t, inClosure)
	return EventTableContext{
		StructName:              t.StructName,
		TableNameConstant:       t.TableNameConstant,
		TableName:               t.TableName,
		Schema:                  t.Schema,
		Tenanted:                tenanted,
		TenantInPK:              tenanted && t.Tenancy.InPrimaryKey,
		TenantFieldName:         tenantField,
		CompositePKStructName:   t.CompositePKStructName,
		BatchCreateOps:          batchCreateOps(t),
		HasUpdateMany:           t.Operations.UpdateMany,
		RedactedCreateFields:    redactCreate,
		RedactedUpdateFields:    redactUpdate,
		RedactedIncrementConsts: redactIncrement,
		EmitRedactCreate:        emitCreate,
		EmitRedactUpdate:        emitUpdate,
		EmitRedactIncrement:     emitIncrement,
		FilterRedactOps:         redactOps,
		PassThroughOps:          passThroughOps(t, failClosed, redactOps, emitCreate, emitUpdate, emitIncrement),
		FailClosedDefault:       failClosed,
	}
}

// batchCreateOps names the enabled ops that publish a []*Create<T>Input and
// fan it out one element per affected row (PRD §28.6). Both index the same
// slice by the same i, so they share one arm.
//
// OpUpsertMany qualifies because its terminal republishes mc.Input as the
// *deduped* slice, which is what stays index-aligned with AffectedPKs — PRD
// §28.9 states that guarantee, and the terminal never sources AffectedPKs from
// RETURNING, so the alignment holds on the DO NOTHING branch too. The arm is
// therefore emitted whether or not the table redacts anything: gating it on
// redaction (the way the single-row OpCreate / OpUpsert arm is gated) would
// leave a non-redacting table publishing the whole slice to every event, and a
// fail-closed one publishing nil — both §28.6 shape violations.
func batchCreateOps(t TableContext) []string {
	var ops []string
	if t.Operations.CreateMany {
		ops = append(ops, "OpCreateMany")
	}
	if t.Operations.UpsertMany {
		ops = append(ops, "OpUpsertMany")
	}
	return ops
}

// filterRedactOps names the *Where delete / restore ops this table's redaction
// switch redacts the published filter for. Empty outside the closure — a table
// with nothing reachable to redact keeps publishing the caller's filter
// unchanged, which is the §28.6 shape and the same-pointer guarantee.
func filterRedactOps(t TableContext, inClosure bool) []string {
	if !inClosure {
		return nil
	}
	return whereDeleteOps(t)
}

// passThroughOps names the ops a fail-closed table must enumerate with an
// explicit inputVal = mc.Input, because their §28.6 payload carries nothing to
// redact on this table and the default arm no longer passes anything through.
//
// Only the ops that would otherwise fall through are listed. The single-row
// SoftDelete / Restore / HardDelete are deliberately absent: they set PK and
// leave Input unset, so the fail-closed default already publishes the nil
// §28.6 mandates for them. Ops that are disabled never fire, so they need no
// arm either.
//
// redacted names the *Where ops filterRedactOps already claimed. Every enabled
// *Where op it did not must be claimed here: a table can redact an input while
// its redacted columns are not filterable (a jsonb[] column, or json on SQLite
// — §11.2), which fails closed without joining the filter closure. Letting
// those ops reach the default would publish nil where §28.6's shape table says
// *<T>Filter — the very shape this fix rejected.
//
// OpCreateMany and OpUpsertMany are deliberately absent, and adding either
// would be a bug rather than an omission: batchCreateOps gives both an
// unconditional arm that indexes the published slice per row, so neither can
// reach the default. A pass-through entry would hand every fanned-out event
// the whole batch — §28.6 mandates the i-th element. The answer is the arm,
// never the pass-through list.
func passThroughOps(t TableContext, failClosed bool, redacted []string, emitCreate, emitUpdate, emitIncrement bool) []string {
	if !failClosed {
		return nil
	}
	var ops []string
	if !emitCreate {
		if t.Operations.Create {
			ops = append(ops, "OpCreate")
		}
		if t.Operations.Upsert {
			ops = append(ops, "OpUpsert")
		}
	}
	if !emitUpdate {
		if t.Operations.Update {
			ops = append(ops, "OpUpdate")
		}
		if t.Operations.UpdateWhere {
			ops = append(ops, "OpUpdateWhere")
		}
	}
	if !emitIncrement && t.Operations.Increment {
		ops = append(ops, "OpIncrement")
	}
	for _, op := range whereDeleteOps(t) {
		if !slices.Contains(redacted, op) {
			ops = append(ops, op)
		}
	}
	return ops
}

// whereDeleteOps names the *Where delete / restore ops this table generates.
// delete.go.tmpl emits each under its base op's flag, so there is no separate
// *_where operation to consult.
func whereDeleteOps(t TableContext) []string {
	var ops []string
	if t.Operations.SoftDelete {
		ops = append(ops, "OpSoftDeleteWhere")
	}
	if t.Operations.HardDelete {
		ops = append(ops, "OpHardDeleteWhere")
	}
	if t.Operations.Restore {
		ops = append(ops, "OpRestoreWhere")
	}
	return ops
}

// redactedIncrementConsts returns the generated increment-column constant
// names (<Struct>Increment<Field>) for incrementable columns carrying an
// event-redacted access role. The increment surface (and its constants) is
// only generated when Operations.Increment is on and IncrementColumns is
// non-empty — the redacted set is a subset, so a non-empty result implies
// the constants exist.
func redactedIncrementConsts(t TableContext) []string {
	var consts []string
	for _, c := range t.IncrementColumns {
		if c.EventRedacted {
			consts = append(consts, t.StructName+"Increment"+c.FieldName)
		}
	}
	return consts
}

// redactedInputFields returns the create- and update-input Go field names
// whose column carries an event-redacted access role (§32.2 write_only /
// internal). Input-field iteration order is preserved, so the emitted
// redactZero calls are deterministic.
func redactedInputFields(t TableContext) (create, update []string) {
	redacted := make(map[string]bool)
	for _, c := range t.Columns {
		if c.EventRedacted {
			redacted[c.Name] = true
		}
	}
	if len(redacted) == 0 {
		return nil, nil
	}
	for _, f := range t.CreateInputFields {
		if redacted[f.ColumnName] {
			create = append(create, f.FieldName)
		}
	}
	for _, f := range t.UpdateInputFields {
		if redacted[f.ColumnName] {
			update = append(update, f.FieldName)
		}
	}
	return create, update
}

// hasTenantedEventTable reports whether any generated event hook needs the
// tenant metadata injection block (and therefore the "fmt" import).
func hasTenantedEventTable(tables []EventTableContext) bool {
	for _, t := range tables {
		if t.Tenanted {
			return true
		}
	}
	return false
}

// findTableConfig returns the TableConfig for a table by name, trying
// schema-qualified first then bare name. Returns zero value if not found.
func findTableConfig(cfg *config.RootConfig, tableName, schema string) config.TableConfig {
	if schema != "" {
		if tc, ok := cfg.Tables[qualifiedTableName(schema, tableName)]; ok {
			return tc
		}
	}
	if tc, ok := cfg.Tables[tableName]; ok {
		return tc
	}
	return config.TableConfig{}
}

// qualifiedTableName returns the schema-qualified table identifier —
// "schema.name" when schema is non-empty, otherwise the bare name. Used
// both as a config lookup key and in user-facing diagnostics.
func qualifiedTableName(schema, name string) string {
	if schema == "" {
		return name
	}
	return schema + "." + name
}

// FilterRedactorContext describes one generated redact<T>Filter helper — the
// §32.3 filter leg of event redaction. The *Where delete / restore ops publish
// the caller's filter as Event.Input (§28.6 shape table), and by the §32.1
// load-bearing principle the Go <T>Filter carries every column, including the
// write_only / internal ones the API filter surface drops. The helper clears
// those comparators on the published clone.
//
// The set of helpers is wider than "tables with a redacted column" in two
// directions — a table whose filter can reach a redacted one through a
// relationship member needs a helper of its own, and a table whose per-table
// events are disabled still needs one when an event-enabled table's filter
// points at it — and narrower in one: a helper is emitted only where something
// calls it, so closure membership alone does not produce one. See
// buildFilterRedactors.
type FilterRedactorContext struct {
	// StructName is the Go struct name, which spells both the helper
	// (redact<T>Filter) and its parameter type (*<T>Filter).
	StructName string
	// RedactedFields are the filter struct's Go field names whose column
	// carries a write_only / internal role, in the filter struct's own field
	// order. Each is a comparator pointer, cleared to nil on the clone.
	RedactedFields []string
	// RelationshipMembers are the relationship filter members whose target is
	// itself in the closure — each is replaced by the target's redacted clone.
	// Members pointing at a table with nothing to redact are left alone, so a
	// filter that cannot reach a redacted column is copied verbatim.
	RelationshipMembers []FilterRedactorMember
}

// FilterRedactorMember is one relationship member a redact<T>Filter helper
// recurses through.
type FilterRedactorMember struct {
	// FieldName is the member's Go field name on the parent filter struct.
	FieldName string
	// TargetStructName spells the member's type (*<T>Filter) and the helper
	// called on it (redact<T>Filter).
	TargetStructName string
}

// buildFilterRedactors returns the §32.3 filter-redaction closure over tables,
// sorted by struct name.
//
// The closure is a least fixed point over two rules:
//
//  1. Seed — a table with at least one write_only / internal column that is
//     also *filterable* is in. A redacted column with no comparator (§11.2)
//     never reaches a filter, so it cannot leak through one.
//  2. Propagate — a table whose filter has a relationship member (PRD §11.1)
//     whose target is in the closure is in, because its helper must recurse
//     through that member to reach the target's redacted comparators.
//
// Rule 2 is what makes this a closure rather than a per-table predicate:
// &UserFilter{Orders: &OrderFilter{Secret: …}} publishes the *orders* table's
// restricted value through a *users* event, so the parent needs a helper even
// with nothing redacted of its own. And / Or need no rule — they are the
// table's own filter type, so a seeded table already covers them.
func buildFilterRedactors(tables []TableContext, hasHook map[string]bool) ([]FilterRedactorContext, map[string]bool) {
	idx := indexTablesForClosure(tables)
	redactedFields, inClosure := seedFilterClosure(tables)
	idx.propagate(tables, inClosure)
	emit := idx.reachableRedactors(tables, inClosure, hasHook)

	out := make([]FilterRedactorContext, 0, len(emit))
	for i := range tables {
		tc := &tables[i]
		if !emit[tableKey(tc)] {
			continue
		}
		out = append(out, FilterRedactorContext{
			StructName:          tc.StructName,
			RedactedFields:      redactedFields[tableKey(tc)],
			RelationshipMembers: idx.closureMembers(tc, inClosure),
		})
	}
	slices.SortFunc(out, func(a, b FilterRedactorContext) int {
		return strings.Compare(a.StructName, b.StructName)
	})

	// Membership is reported by struct name and is *wider* than the emitted
	// set: it also covers a redacted table whose helper nothing calls, which
	// still fails closed on the class argument even though it leaks through no
	// filter.
	member := make(map[string]bool, len(inClosure))
	for i := range tables {
		if inClosure[tableKey(&tables[i])] {
			member[tables[i].StructName] = true
		}
	}
	return out, member
}

// closureIndex resolves a relationship filter's target — which names a table by
// schema and name — to the closure key that table is tracked under.
type closureIndex map[string]*TableContext

// indexTablesForClosure keys every table both bare and schema-qualified, the
// same two spellings wireRelationshipFilters resolves targets through.
func indexTablesForClosure(tables []TableContext) closureIndex {
	idx := make(closureIndex, len(tables)*2)
	for i := range tables {
		tc := &tables[i]
		idx[tc.TableName] = tc
		if tc.Schema != "" {
			idx[qualifiedTableName(tc.Schema, tc.TableName)] = tc
		}
	}
	return idx
}

// target returns the closure key for a relationship filter's target, or "" when
// the target is not a generated table.
func (idx closureIndex) target(schema, name string) string {
	if tc, ok := idx[qualifiedTableName(schema, name)]; ok {
		return tableKey(tc)
	}
	if tc, ok := idx[name]; ok {
		return tableKey(tc)
	}
	return ""
}

// seedFilterClosure applies rule 1: a table with a redacted *filterable* column
// is in, and its field list is what its helper clears.
func seedFilterClosure(tables []TableContext) (map[string][]string, map[string]bool) {
	redactedFields := make(map[string][]string, len(tables))
	inClosure := make(map[string]bool, len(tables))
	for i := range tables {
		tc := &tables[i]
		if f := redactedFilterFields(tc); len(f) > 0 {
			redactedFields[tableKey(tc)] = f
			inClosure[tableKey(tc)] = true
		}
	}
	return redactedFields, inClosure
}

// propagate applies rule 2 to a fixed point, adding every table whose filter
// can reach one already in the closure. Each pass only adds tables and there
// are finitely many, so this terminates even on a cyclic relationship graph —
// which the shipped fixtures produce, since an M2M edge is filterable from both
// sides.
func (idx closureIndex) propagate(tables []TableContext, inClosure map[string]bool) {
	for changed := true; changed; {
		changed = false
		for i := range tables {
			tc := &tables[i]
			if inClosure[tableKey(tc)] {
				continue
			}
			for _, rf := range tc.RelationshipFilters {
				if k := idx.target(rf.TargetSchema, rf.TargetTable); k != "" && inClosure[k] {
					inClosure[tableKey(tc)] = true
					changed = true
					break
				}
			}
		}
	}
}

// reachableRedactors narrows membership to the helpers that are actually
// called. Membership says a filter *can* carry a redacted value; it does not
// say anything invokes the helper, and emitting one nothing calls would be dead
// generated code. The roots are the tables whose own hook calls one — those
// with an event hook (hasHook) *and* a *Where delete / restore op — and
// reachability follows in-closure relationship members from there. A table
// whose per-table events are disabled is not a root, but it is still reachable
// as another table's relationship target, which is why the closure spans every
// table rather than only the event-enabled ones.
func (idx closureIndex) reachableRedactors(tables []TableContext, inClosure, hasHook map[string]bool) map[string]bool {
	emit := make(map[string]bool, len(inClosure))
	var reach func(tc *TableContext)
	reach = func(tc *TableContext) {
		if emit[tableKey(tc)] {
			return
		}
		emit[tableKey(tc)] = true
		for _, rf := range tc.RelationshipFilters {
			if k := idx.target(rf.TargetSchema, rf.TargetTable); k != "" && inClosure[k] {
				reach(idx[k])
			}
		}
	}
	for i := range tables {
		tc := &tables[i]
		if hasHook[tableKey(tc)] && inClosure[tableKey(tc)] && len(whereDeleteOps(*tc)) > 0 {
			reach(tc)
		}
	}
	return emit
}

// closureMembers returns the relationship members whose target is in the
// closure — the ones a helper recurses through. A member pointing at a table
// with nothing to redact is left alone, so a filter that cannot reach a
// redacted column is copied verbatim.
func (idx closureIndex) closureMembers(tc *TableContext, inClosure map[string]bool) []FilterRedactorMember {
	var members []FilterRedactorMember
	for _, rf := range tc.RelationshipFilters {
		if k := idx.target(rf.TargetSchema, rf.TargetTable); k != "" && inClosure[k] {
			members = append(members, FilterRedactorMember{
				FieldName:        rf.FieldName,
				TargetStructName: rf.TargetStructName,
			})
		}
	}
	return members
}

// tableKey identifies a table context across the closure maps. Schema-qualified
// so two same-named tables in different schemas stay distinct.
func tableKey(tc *TableContext) string {
	return qualifiedTableName(tc.Schema, tc.TableName)
}

// redactedFilterFields returns the filter struct's Go field names whose column
// carries an event-redacted access role, in the filter struct's own field
// order. Non-filterable columns are skipped: they have no field to clear.
func redactedFilterFields(tc *TableContext) []string {
	redacted := make(map[string]bool)
	for _, c := range tc.Columns {
		if c.EventRedacted {
			redacted[c.Name] = true
		}
	}
	if len(redacted) == 0 {
		return nil
	}
	var fields []string
	for _, f := range tc.FilterFields {
		if f.Filterable && redacted[f.ColumnName] {
			fields = append(fields, f.FieldName)
		}
	}
	return fields
}
