package gen

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
)

// This file resolves the nested-mutation surface (PRD §9.9): which (parent,
// edge) pairs are write-eligible, which verbs each one admits, and every Go
// expression the emitted executor needs. It is the narrower half of the pair
// nested_names.go opens — that file claims the *names* on structural facts
// alone so that enabling the feature later can never turn a valid config
// invalid, and this one asks whether the emitters can actually render them.
//
// ValidateNestedWriteEligibility is the lint half (PRD §9.9.4): an edge
// explicitly listed in `tables.<t>.nested_mutations.relationships` that fails a
// rule is a hard error, and an auto-included one that fails is silently
// omitted. Both halves are load-bearing — an explicitly requested edge that
// generates nothing is worse than a build failure, and an auto-included one
// must not break an unrelated build.

// nestedMutationsEnabled reports the package-wide opt-in (PRD §4.6). With it
// off nothing in §9.9 is emitted anywhere. It is the only client-side control
// over the nested methods: the client has no operations toggle (PRD §4.6).
func nestedMutationsEnabled(cfg *config.RootConfig) bool {
	nm := cfg.Generation.NestedMutations
	return nm != nil && nm.Enabled != nil && *nm.Enabled
}

// nestedMutationAllowed reports whether name is in the configured closed set,
// treating an unset slice as the full default set rather than the empty one —
// omitting the key is not a silent narrowing (PRD §4.6).
func nestedMutationAllowed(configured []string, name string) bool {
	if len(configured) == 0 {
		return true
	}
	return slices.Contains(configured, name)
}

// nestedIneligible is one thing an edge could not carry, with the reason the
// lint names it by and the resolver's choice between a hard error and a silent
// omission.
//
// verb is empty when the whole edge is out and names a single verb when the
// edge survives without it. The second kind exists because a verb the emitter
// cannot render is not the same fact as a verb the §4.6 `verbs` mask turned
// off: the mask is the consumer asking for a narrower surface and is silent by
// design, while an unrenderable verb is a generator limitation the consumer
// cannot see from the config. Reporting it keeps the failure loud for an edge
// the config asked for by name, without taking the verbs that do work.
type nestedIneligible struct {
	edge   string
	verb   string
	reason string
	// unattributed marks a refusal no single §9.9.4 rule is responsible for.
	unattributed bool
}

// String renders the rejection for the lint's error text.
//
// Deliberately not named Error: nestedIneligible is a rejection *record* the
// resolvers return alongside a result, not an error value, and the six
// resolvers that return `*nestedIneligible` would otherwise be the exact
// `(*T, *ConcreteError)` shape guidelines/GO.md forbids for the typed-nil trap
// it sets. Keeping it a Stringer makes that trap unrepresentable: nothing can
// assign it to an `error` in the first place.
func (n nestedIneligible) String() string {
	// An unattributed refusal is one no single §9.9.4 rule is responsible for —
	// the edge simply ends up with an empty verb set, which §9.9.3's matrix
	// governs. Citing §9.9.4 anyway would attribute the refusal to a rule that
	// did not fire.
	ref := "PRD §9.9.4"
	if n.unattributed {
		ref = "PRD §9.9.3"
	}
	if n.verb != "" {
		return fmt.Sprintf("relationship %q cannot carry its `%s` verb: %s (%s)", n.edge, n.verb, n.reason, ref)
	}
	return fmt.Sprintf("relationship %q is not write-eligible: %s (%s)", n.edge, n.reason, ref)
}

// wireNestedMutations attaches the resolved nested-mutation surface to every
// table context.
//
// It is a post-build pass for the same reason wireRelationshipFilters is:
// eligibility reads the *target's* primary key, create input and conflict
// targets, so every table context must exist first.
//
// It reports nothing. ValidateNestedWriteEligibility owns the §9.9.4 hard
// error and runs ahead of it on every entry point — reporting from both would
// print each violation twice, which is what the two passes doing the same
// resolution for different purposes makes easy to get wrong.
func wireNestedMutations(contexts []TableContext, cfg *config.RootConfig, resolver *gotype.Resolver) {
	if !nestedMutationsEnabled(cfg) {
		return
	}

	byTable := newTableIndex(contexts)

	nm := cfg.Generation.NestedMutations

	for i := range contexts {
		tc := &contexts[i]
		edges, _ := resolveNestedEdges(tc, byTable, cfg, resolver)
		if len(edges) == 0 {
			continue
		}

		nested := &NestedContext{
			CreateInputName:         "Create" + tc.StructName + "WithRelatedInput",
			UpdateInputName:         "Update" + tc.StructName + "WithRelatedInput",
			UpsertInputName:         "Upsert" + tc.StructName + "WithRelatedInput",
			ParentFieldOptionsFunc:  toCamelCase(tc.StructName) + "NestedParentFieldOptions",
			SelectsRelationshipFunc: toCamelCase(tc.StructName) + "NestedSelectsRelationship",
			RelationshipFieldNames:  relationshipFieldNames(tc),
			Edges:                   edges,
			ParentKeyFields:         nestedParentKeyFields(tc, edges),
			EmitCreate:              nestedMutationAllowed(nm.Operations, "create"),
			EmitUpdate:              nestedMutationAllowed(nm.Operations, "update"),
			// The upsert conflict-target rule: UpsertWithRelated takes a
			// `<Parent>ConflictTarget` argument, and a table whose uniqueness
			// is app-enforced through `primary_key.columns` emits no such
			// constant (PRD §9.5) — so there is nothing to pass and the
			// surface is omitted rather than emitted uncallable.
			EmitUpsert: nestedMutationAllowed(nm.Operations, "upsert") && len(tc.ConflictTargets) > 0,
		}
		// The no-eligible-edge rule one level down: every family off leaves the
		// wrapper types and the executors with no caller, so the parent emits
		// nothing at all.
		if !nested.EmitCreate && !nested.EmitUpdate && !nested.EmitUpsert {
			continue
		}
		// The resolved operations carry the nested methods as emitted, so
		// every reader of the client method set — the API mask, the
		// over-reach warning — sees exactly what was generated.
		tc.Operations.CreateWithRelated = nested.EmitCreate
		tc.Operations.UpdateWithRelated = nested.EmitUpdate
		tc.Operations.UpsertWithRelated = nested.EmitUpsert
		tc.Nested = nested
		tc.Imports = UniqueImports(append(tc.Imports, nestedEdgeImports(edges)...))
	}
}

// nestedParentKeyFields unions the parent fields tc's edges reference, leaving
// out the primary key, which every parent write selects.
func nestedParentKeyFields(tc *TableContext, edges []NestedEdgeContext) []string {
	var fields []string
	for _, e := range edges {
		for _, f := range e.ParentKeyFields {
			if f != tc.PKColumns[0].FieldName && !slices.Contains(fields, f) {
				fields = append(fields, f)
			}
		}
	}
	return fields
}

// ValidateNestedWriteEligibility reports every edge a table's
// `nested_mutations.relationships` allowlist names that cannot carry a nested
// write (PRD §9.9.4).
//
// It is the same resolution wireNestedMutations performs, exposed so
// `sqlgen validate` answers the rule without rendering a template, and so the
// two can never disagree about what "eligible" means. An allowlist entry that
// names no relationship at all is reported here too: the config asked for an
// edge that does not exist, which no other pass catches.
func ValidateNestedWriteEligibility(contexts []TableContext, cfg *config.RootConfig, resolver *gotype.Resolver) error {
	return validateNestedWriteEligibility(contexts, cfg, resolver, nil)
}

// validateNestedWriteEligibility is ValidateNestedWriteEligibility over the
// tables that built when others failed (failed holds their qualifiedOrBare
// keys). An edge whose target or junction failed is not reported: that table's
// own error already is, and "not in the generated set" would misstate why.
func validateNestedWriteEligibility(contexts []TableContext, cfg *config.RootConfig, resolver *gotype.Resolver, failed map[string]bool) error {
	if !nestedMutationsEnabled(cfg) {
		return nil
	}

	byTable := newTableIndex(contexts)

	var errs []error
	for i := range contexts {
		tc := &contexts[i]
		allow := tableNestedAllowlist(cfg, tc)
		if len(allow) == 0 {
			continue
		}
		table := qualifiedOrBare(tc.Schema, tc.TableName)

		declared := make(map[string]bool, len(tc.Relationships))
		for _, rel := range tc.Relationships {
			declared[rel.FieldName] = true
		}
		listed := make([]string, 0, len(allow))
		for name := range allow {
			listed = append(listed, name)
		}
		slices.Sort(listed)
		for _, name := range listed {
			if !declared[name] {
				errs = append(errs, fmt.Errorf(
					"tables.%s.nested_mutations.relationships: %q names no relationship on this table (PRD §4.8)",
					table, name))
			}
		}

		rejected := listedShapeFailures(tc, allow, byTable)
		_, resolveRejected := resolveNestedEdges(tc, byTable, cfg, resolver)
		rejected = append(rejected, resolveRejected...)
		for _, r := range rejected {
			if edgeIntoFailedTable(tc, r.edge, failed) {
				continue
			}
			if _, listed := allow[r.edge]; listed {
				errs = append(errs, fmt.Errorf("tables.%s.nested_mutations: %s", table, r))
			}
		}
	}
	return errors.Join(errs...)
}

// edgeIntoFailedTable reports whether the named edge of tc targets, or links
// through, a table whose context failed to build.
func edgeIntoFailedTable(tc *TableContext, edge string, failed map[string]bool) bool {
	if len(failed) == 0 {
		return false
	}
	for _, rel := range tc.Relationships {
		if rel.FieldName != edge {
			continue
		}
		if failed[qualifiedOrBare(rel.TargetSchema, rel.TargetTable)] {
			return true
		}
		if rel.JunctionTable != "" && failed[qualifiedOrBare(rel.JunctionSchema, rel.JunctionTable)] {
			return true
		}
	}
	return false
}

// listedShapeFailures reports the listed edges that nestedCandidateEdges
// filters out before any edge is resolved: every edge of a composite-PK parent
// and a one-to-one edge whose FK is not on the target. They never
// reach resolveNestedEdges, so without this pass a listed edge failing either
// rule generated nothing and reported nothing — the outcome PRD §9.9.4's
// listed-versus-auto rule exists to prevent.
//
// The O2O arm mirrors nestedCandidateEdges' three-answers-in-one reading of
// FKOnTarget: an unresolved target is named as such rather than as belongs-to,
// since with no target there is no telling which side holds the FK.
func listedShapeFailures(tc *TableContext, allow map[string]config.TableNestedRelationship, byTable tableIndex) []nestedIneligible {
	candidate := make(map[string]bool, len(tc.Relationships))
	for _, rel := range nestedCandidateEdges(*tc) {
		candidate[rel.FieldName] = true
	}

	var out []nestedIneligible
	for _, rel := range tc.Relationships {
		if _, listed := allow[rel.FieldName]; !listed || candidate[rel.FieldName] {
			continue
		}
		switch {
		case tc.CompositePK || len(tc.PKColumns) != 1:
			out = append(out, nestedIneligible{
				edge:   rel.FieldName,
				reason: "the parent has a composite primary key, which the link step and the visibility read cannot key on",
			})
		default:
			reason := "the edge is belongs-to: its foreign key is on this table, so the parent's key does not flow down into the target"
			if _, ok := byTable.lookup(rel.TargetSchema, rel.TargetTable); !ok {
				reason = "the target table is not in the generated set, so there is no client to write through"
			}
			out = append(out, nestedIneligible{edge: rel.FieldName, reason: reason})
		}
	}
	slices.SortFunc(out, func(a, b nestedIneligible) int { return strings.Compare(a.edge, b.edge) })
	return out
}

// tableNestedAllowlist returns the `tables.<t>.nested_mutations.relationships`
// entries for tc, keyed by the relationship field name they name. An absent
// block yields an empty map, which every caller reads as "no allowlist" —
// distinct from an empty list, which config validation rejects for having no
// `name`.
func tableNestedAllowlist(cfg *config.RootConfig, tc *TableContext) map[string]config.TableNestedRelationship {
	tableCfg := resolveTableConfig(cfg.Tables, tc.Schema, tc.TableName)
	if tableCfg.NestedMutations == nil {
		return nil
	}
	allow := make(map[string]config.TableNestedRelationship, len(tableCfg.NestedMutations.Relationships))
	for _, entry := range tableCfg.NestedMutations.Relationships {
		allow[entry.Name] = entry
	}
	return allow
}

// relationshipFieldNames returns every relationship member on the parent's
// FieldOptions struct, in declaration order. The parent write strips all of
// them — not just the nestable ones — because loading any relationship there
// would run before the nested rows exist and return a stale set (PRD §9.9.6),
// and the terminal re-read fires when the caller selected any of them.
func relationshipFieldNames(tc *TableContext) []string {
	names := make([]string, 0, len(tc.Relationships))
	for _, rel := range tc.Relationships {
		names = append(names, rel.FieldName)
	}
	return names
}

// nestedEdgeImports collects the imports the nested child inputs need. The
// child input restates the *target's* create-input field types, so under the
// file_per_table layout the parent's file would otherwise carry none of them.
func nestedEdgeImports(edges []NestedEdgeContext) []string {
	var imports []string
	for _, e := range edges {
		imports = append(imports, e.Imports...)
	}
	return imports
}

// nestedEdgeTypeImports collects every import the emitted executor needs
// beyond the parent's own columns.
//
// Three sources, and only the first is obvious. The nested child input
// restates the target's create-input field types. The target's primary key
// spells the `connect` list, the link step's dedupe set and the key of the
// owner map — and on an M2M edge there are no child fields, so that is the
// only source there. The traversed FK's *column* types are the owner map's
// value, which is the Null-wrapper or pointer form rather than the input form.
// A key holding the tenant column also names tenancy.ErrMismatch.
func nestedEdgeTypeImports(edge *NestedEdgeContext, rel RelationshipContext, target *TableContext) []string {
	var imports []string
	add := func(path string) {
		if path != "" {
			imports = append(imports, path)
		}
	}
	for _, f := range edge.ChildFields {
		add(f.Import)
	}
	add(target.PKColumns[0].Import)
	for _, name := range keyLocals(rel) {
		if col, ok := columnBySQLName(target.Columns, name); ok {
			add(col.Import)
		}
	}
	if edge.TenantKeyMember {
		add("errors")
		add("github.com/teandresmith/sqlgen/tenancy")
	}
	return imports
}

// columnBySQLName finds a column context by its SQL name.
func columnBySQLName(cols []ColumnContext, name string) (ColumnContext, bool) {
	for _, c := range cols {
		if c.Name == name {
			return c, true
		}
	}
	return ColumnContext{}, false
}

// resolveNestedEdges returns tc's write-eligible edges in relationship-name
// order, plus one nestedIneligible per candidate edge that failed a rule.
//
// The candidate set is nestedCandidateEdges — PRD §9.9.4's edge-shape and
// single-column parent key rules, the two purely structural ones — narrowed
// here by every rule that reads config or the target's resolved context.
func resolveNestedEdges(
	tc *TableContext,
	byTable tableIndex,
	cfg *config.RootConfig,
	resolver *gotype.Resolver,
) ([]NestedEdgeContext, []nestedIneligible) {
	allow := tableNestedAllowlist(cfg, tc)
	nm := cfg.Generation.NestedMutations

	var edges []NestedEdgeContext
	var rejected []nestedIneligible
	for _, rel := range nestedCandidateEdges(*tc) {
		// An allowlist narrows which edges are reachable at all (PRD §4.8). An
		// absent block includes every eligible edge.
		if len(allow) > 0 {
			if _, listed := allow[rel.FieldName]; !listed {
				continue
			}
		}
		edge, notes := buildNestedEdge(tc, rel, byTable, nm, allow[rel.FieldName], resolver)
		rejected = append(rejected, notes...)
		// A note naming no verb is fatal: the edge carries nothing.
		if slices.ContainsFunc(notes, func(n nestedIneligible) bool { return n.verb == "" }) {
			continue
		}
		edges = append(edges, edge)
	}
	return edges, rejected
}

// resolveNestedTarget answers the eligibility rules that depend only on the
// edge's declaration and on the target table: no `filter:`, a generated and
// single-keyed target, and a non-empty create input. No rule requires the
// target's Create and CreateMany: the client has no operations toggle (PRD
// §4.6), so every generated target has both.
func resolveNestedTarget(tc *TableContext, rel RelationshipContext, byTable tableIndex) (*TableContext, *nestedIneligible) {
	reject := func(reason string) (*TableContext, *nestedIneligible) {
		return nil, &nestedIneligible{edge: rel.FieldName, reason: reason}
	}

	// A `filter:` edge is not write-eligible. A `discriminator:` is, and
	// is the only way a polymorphic edge becomes one (PRD §13.4.1); config
	// validation already rejects an edge declaring both.
	if rel.Filter != "" {
		return reject("the edge declares a `filter:`, whose predicate sqlgen cannot reduce to column assignments")
	}

	target, ok := byTable.lookup(rel.TargetSchema, rel.TargetTable)
	if !ok {
		// Both relationship builders carry the target's schema in TargetSchema
		// — a declared `table:` is split and resolved in
		// configRelationshipToContext — so the lookup is exact.
		return reject("the target table is not in the generated set, so there is no client to write through")
	}
	if len(target.PKColumns) != 1 {
		return reject("the target has a composite primary key, which the link and visibility reads cannot key on")
	}
	for _, key := range nestedReferencedColumns(tc, rel, target) {
		if key.Nullable {
			return reject(fmt.Sprintf("the edge's key references the nullable column %q, which a nested write cannot copy into the key", key.Name))
		}
	}

	// A write surface into a table with nothing writable writes nothing.
	//
	// This is the Go half of the rule, and it is the only half reachable here:
	// PRD §32.1 keeps every column on the Go create input, so `access` can
	// never empty it. The rule's §32.5 wording is about the *projected* input,
	// which is the API surface — apiNestedFamiliesFor asks the same question of
	// that one.
	if len(target.CreateInputFields) == 0 {
		return reject("the target's create input is empty")
	}
	return target, nil
}

// buildNestedEdge resolves one candidate edge into its emitted shape.
//
// The notes it returns are of two kinds. One naming no verb is fatal — the
// edge carries nothing and the caller drops it. One naming a verb means the
// edge survives without that verb, which the caller keeps and the lint reports
// only for an explicitly-listed edge.
func buildNestedEdge(
	tc *TableContext,
	rel RelationshipContext,
	byTable tableIndex,
	nm *config.NestedMutationsConfig,
	entry config.TableNestedRelationship,
	resolver *gotype.Resolver,
) (NestedEdgeContext, []nestedIneligible) {
	isM2M := rel.Type == parser.ManyToMany
	hasOne := rel.Type == parser.OneToOne

	target, ineligible := resolveNestedTarget(tc, rel, byTable)
	if ineligible != nil {
		return NestedEdgeContext{}, []nestedIneligible{*ineligible}
	}

	parentKey := nestedParentKey(tc, rel)
	discColumn, discLiteral := "", ""
	if rel.Discriminator != nil {
		discColumn, discLiteral = rel.Discriminator.Column, rel.Discriminator.GoLiteral
	}
	edge := NestedEdgeContext{
		DiscColumn:        discColumn,
		DiscGoLiteral:     discLiteral,
		FieldName:         rel.FieldName,
		JSONTag:           rel.JSONTag,
		Description:       rel.Description,
		CreateBlockName:   nestedBlockName(tc.StructName, rel.FieldName, "Create"),
		UpdateBlockName:   nestedBlockName(tc.StructName, rel.FieldName, "Update"),
		ExecutorName:      "apply" + tc.StructName + rel.FieldName + "Nested",
		WidenFuncName:     toCamelCase(tc.StructName) + rel.FieldName + "NestedFromCreate",
		TargetStructName:  target.StructName,
		TargetPlural:      StructNamePlural(target.StructName),
		TargetClientField: toCamelCase(target.StructName) + "Client",
		TargetPKFieldName: target.PKColumns[0].FieldName,
		TargetPKGoType:    target.PKColumns[0].GoType,
		AllowReparent:     entry.AllowReparent,
	}
	edge.TargetPKIsString = funcPKIsStringType(target.PKColumns[0].GoType)
	for _, key := range nestedParentKeys(tc, rel) {
		edge.ParentKeyFields = append(edge.ParentKeyFields, key.FieldName)
	}
	edge.SelfReferential = target.Schema == tc.Schema && target.TableName == tc.TableName
	edge.TargetResolvesTenant = target.resolvesTenantDirectly()

	switch {
	case isM2M:
		edge.Shape = "m2m"
	case hasOne:
		edge.Shape = "has_one"
	default:
		edge.Shape = "o2m"
	}

	// The verb set is the intersection of what the edge admits (PRD §9.9.3)
	// and the package-wide `verbs` mask (PRD §4.6).
	edge.HasCreate = nestedMutationAllowed(nm.Verbs, "create")
	want := nestedVerbMask{
		connect:    nestedMutationAllowed(nm.Verbs, "connect"),
		disconnect: nestedMutationAllowed(nm.Verbs, "disconnect"),
		clear:      nestedMutationAllowed(nm.Verbs, "clear"),
	}

	var notes []nestedIneligible
	var note *nestedIneligible
	if isM2M {
		note = resolveNestedM2M(&edge, rel, tc, target, byTable, parentKey, want, resolver)
		edge.ChildCreateType = "Create" + target.StructName + "Input"
	} else {
		note = resolveNestedToMany(&edge, rel, nestedParentKeys(tc, rel), target, want, resolver)
		edge.ChildInputName = nestedChildInputName(tc.StructName, rel.FieldName)
		edge.ChildCreateType = edge.ChildInputName
	}
	if note != nil {
		if note.verb == "" {
			return NestedEdgeContext{}, []nestedIneligible{*note}
		}
		notes = append(notes, *note)
	}

	notes = append(notes, resolveNestedConnectKey(&edge, target, rel.FieldName)...)
	resolveNestedCreatePK(&edge, target)
	edge.Imports = nestedEdgeTypeImports(&edge, rel, target)

	if !edge.HasCreate && !edge.HasConnect && !edge.HasDisconnect && !edge.HasClear {
		// No single rule: the causes compose. `create` is lost only to the §4.6
		// `verbs` mask, while `connect` can be lost to that mask, to the
		// nullable-FK rule, or to an unrenderable key — so citing any one rule
		// here would misattribute the refusal in every combination but one. The
		// edge is still reported when the config listed it by name: §9.9.4's
		// reason for the hard-error half is that an explicitly requested edge
		// generating nothing is worse than a build failure, and that holds
		// however the verb set emptied.
		notes = append(notes, nestedIneligible{
			edge:         rel.FieldName,
			reason:       "no verb survives: the edge admits none of `create`, `connect`, `disconnect` or `clear`",
			unattributed: true,
		})
	}
	return edge, notes
}

// resolveNestedConnectKey fills the expressions every id-list-bearing verb
// shares: the element type the `ids` local takes, the conversion into it, and
// the two `IN` terms.
//
// All of them key on the target's own PK comparator rather than on the caller's
// list type (PRD §11.2), so a PK with no filter member carries neither
// `connect` nor `disconnect`. `connect` needs one thing more — bucketing the
// visibility read's rows in a map keyed on the PK — and so does a `disconnect`
// on a self-referential edge, whose self-reference guard compares each named id
// against the parent's own key. The comparator.Opaque family is exactly the set
// that can do neither (PRD §11.2).
//
// Returns one note per verb actually lost, which is why it is a slice: a
// `connect` and a `disconnect` dropped by the same fact are still two verbs
// the consumer asked for, and a note naming one of them would leave the other
// missing with no explanation.
func resolveNestedConnectKey(edge *NestedEdgeContext, target *TableContext, field string) []nestedIneligible {
	pk := target.PKColumns[0]
	edge.ConnectKeyGoType = edge.TargetPKGoType
	edge.ConnectKeyExpr = "v"
	if edge.TargetPKIsString {
		edge.ConnectKeyGoType = "string"
		edge.ConnectKeyExpr = funcFKToString(pk, "v")
	}

	var notes []nestedIneligible
	drop := func(verb *bool, name, reason string) {
		wanted := *verb
		*verb = false
		if wanted {
			notes = append(notes, nestedIneligible{edge: field, verb: name, reason: reason})
		}
	}

	if _, hasPKFilter := filterFieldByColumn(target.FilterFields, pk.Name); !hasPKFilter {
		reason := fmt.Sprintf("the target's primary key binds to %s, which carries no `IN` comparator for the id list", edge.TargetPKGoType)
		drop(&edge.HasConnect, "connect", reason)
		drop(&edge.HasDisconnect, "disconnect", reason)
		return notes
	}
	if !GoTypeIsValidMapKey(edge.TargetPKGoType) {
		drop(&edge.HasConnect, "connect", fmt.Sprintf(
			"the target's primary key binds to %s, which the visibility read cannot bucket its rows by", edge.TargetPKGoType))
		// `disconnect` needs the same property, for three separate reasons and
		// unconditionally: the verb-conflict check buckets the named ids in a
		// set, the M2M link step dedupes the pairs through one, and the
		// self-reference guard compares each id against the parent's own key
		// with `==`. The comparator.Opaque family can do none of the three —
		// its members are slices, or structs holding them — so the edge keeps
		// `create` and `clear`, which name no ids at all.
		drop(&edge.HasDisconnect, "disconnect", fmt.Sprintf(
			"the target's primary key binds to %s, which the verb-conflict check cannot bucket and the self-reference guard cannot compare", edge.TargetPKGoType))
	}
	base := strings.TrimPrefix(edge.TargetPKGoType, "*")
	edge.ConnectFilterExpr = pkFilterInExpr(base, "ids", false)
	// Over `adopting`, the per-chunk slice — not over `adopt`, which is the
	// whole queue. The adoption is batched at c.batchSize like every other
	// id-list statement, so the term has to name the chunk.
	edge.AdoptFilterExpr = pkFilterInExpr(base, "adopting", false)
	return notes
}

// nestedVerbMask is the package-wide §4.6 `verbs` mask, resolved once per edge.
// Only `create` is absent: it is admitted by every shape, so it needs no
// per-shape narrowing and is set directly on the edge.
type nestedVerbMask struct {
	connect    bool
	disconnect bool
	clear      bool
}

// resolveNestedCreatePK records the target's primary-key member on the `create`
// verb's element type, which the verb-conflict check reads to catch a `create`
// entry carrying an explicit key that `disconnect` also names (PRD §9.9.6).
//
// A create input with no key member leaves it empty and the check is skipped.
// That is not a gap: such an entry always mints a new row, so the intersection
// it names is empty by construction — which is exactly why the check is spelled
// as `connect` ∩ `disconnect` first and this half second.
func resolveNestedCreatePK(edge *NestedEdgeContext, target *TableContext) {
	if !edge.HasCreate {
		return
	}
	pk := target.PKColumns[0]
	f, ok := inputFieldByColumn(target.CreateInputFields, pk.Name)
	if !ok {
		return
	}
	if edge.Shape != "m2m" {
		// Shapes 2 and 3 take the nested child input, which drops the
		// traversed FK and the discriminator; the key survives both elisions
		// unless it *is* one of them, in which case ChildFields says so.
		if _, kept := inputFieldByColumn(edge.ChildFields, pk.Name); !kept {
			return
		}
	}
	inner, omittable := omittableInner(f.GoType)
	if omittable && inner != edge.TargetPKGoType {
		// A key the caller supplies through a different binding than the one
		// the disconnect list carries cannot be compared against it.
		return
	}
	if !omittable && f.GoType != edge.TargetPKGoType {
		return
	}
	edge.ChildCreatePKField = f.FieldName
	edge.ChildCreatePKOmittable = omittable
}

// nestedParentKey is the parent side of an edge: the column its key
// references, the parent's PK unless the key references a UNIQUE column (PRD
// §13.1).
func nestedParentKey(tc *TableContext, rel RelationshipContext) ColumnContext {
	if rel.ParentKey.Name != "" {
		return rel.ParentKey
	}
	return tc.PKColumns[0]
}

// nestedParentKeys is nestedParentKey for every pair of the edge's key: the
// junction's key into the parent on M2M.
func nestedParentKeys(tc *TableContext, rel RelationshipContext) []ColumnContext {
	pairs := rel.Key
	if rel.Type == parser.ManyToMany {
		pairs = rel.JunctionLocal
	}
	return keyParents(tc, pairs, nestedParentKey(tc, rel))
}

// nestedReferencedColumns lists every column the edge's key references: the
// parent's, and on M2M the target's, whichever side supplies a junction column
// the two keys share. An M2M edge with no reference pairs references the
// target's primary key, which is never nullable.
func nestedReferencedColumns(tc *TableContext, rel RelationshipContext, target *TableContext) []ColumnContext {
	columns := nestedParentKeys(tc, rel)
	for _, p := range rel.JunctionReference {
		if col, ok := columnBySQLName(target.Columns, p.Referenced); ok {
			columns = append(columns, col)
		}
	}
	return columns
}

// splitNestedKeyMembers records the traversed key's pinned and free members on
// the edge, and rejects it when a member is absent from the target's create
// input: a nested create sets every member, and a generated one is not there.
func splitNestedKeyMembers(edge *NestedEdgeContext, rel RelationshipContext, target *TableContext, members []keyMember) *nestedIneligible {
	for _, m := range members {
		if _, ok := inputFieldByColumn(target.CreateInputFields, m.column); !ok {
			return &nestedIneligible{edge: rel.FieldName, reason: fmt.Sprintf(
				"the traversed FK column %q is absent from the target's create input, so a nested create cannot set it", m.column)}
		}
		if m.pinned {
			edge.PinnedMembers = append(edge.PinnedMembers, m.column)
		} else {
			edge.FreeMembers = append(edge.FreeMembers, m.column)
		}
		edge.TenantKeyMember = edge.TenantKeyMember || m.tenant
	}
	return nil
}

// resolveNestedKeyAssign fills edge.KeyMembers with the assignment that sets
// each member from the parent column it references.
func resolveNestedKeyAssign(edge *NestedEdgeContext, rel RelationshipContext, target *TableContext, members []keyMember, parents []ColumnContext, resolver *gotype.Resolver) *nestedIneligible {
	edge.KeyMembers = make([]NestedKeyMemberContext, 0, len(members))
	for i, m := range members {
		field, _ := inputFieldByColumn(target.CreateInputFields, m.column)
		expr := nestedAssignExpr(field.GoType, field.Required, parents[i].GoType, "parent."+parents[i].FieldName, resolver)
		if expr == "" {
			return &nestedIneligible{edge: rel.FieldName, reason: fmt.Sprintf(
				"the traversed FK column %q binds to %s, which sqlgen cannot construct from the parent's key", m.column, field.GoType)}
		}
		edge.KeyMembers = append(edge.KeyMembers, NestedKeyMemberContext{
			Column: m.column, FieldName: field.FieldName, Pinned: m.pinned, AssignExpr: expr,
		})
		if m.tenant && !parents[i].Tenant {
			edge.TenantKeyParent, edge.TenantKeyClient = "parent."+parents[i].FieldName, edge.TargetClientField
		}
	}
	return nil
}

// resolveNestedChildInput fills the nested child input's fields and the
// discriminator assignment. The input drops every member of the traversed
// key, the tenant column included, since the parent's referenced columns set
// them all (PRD §9.9.5), and the discriminator, which the executor sets from
// the edge's declared value.
func resolveNestedChildInput(edge *NestedEdgeContext, rel RelationshipContext, target *TableContext, members []keyMember, resolver *gotype.Resolver) *nestedIneligible {
	reject := func(reason string) *nestedIneligible {
		return &nestedIneligible{edge: rel.FieldName, reason: reason}
	}
	drop := make(map[string]bool, len(members)+1)
	for _, m := range members {
		drop[m.column] = true
	}
	if rel.Discriminator != nil {
		discField, found := inputFieldByColumn(target.CreateInputFields, rel.Discriminator.Column)
		if !found {
			return reject(fmt.Sprintf("the discriminator column %q is absent from the target's create input", rel.Discriminator.Column))
		}
		drop[rel.Discriminator.Column] = true
		edge.DiscFieldName = discField.FieldName
		edge.DiscAssignExpr = nestedLiteralExpr(discField.GoType, discField.Required, rel.Discriminator.GoLiteral, resolver)
		if edge.DiscAssignExpr == "" {
			return reject(fmt.Sprintf("the discriminator column %q binds to %s, which the declared value %s cannot be assigned to, so a nested create cannot set it", rel.Discriminator.Column, discField.GoType, rel.Discriminator.GoLiteral))
		}
	}

	edge.ChildFields = make([]InputFieldContext, 0, len(target.CreateInputFields))
	for _, f := range target.CreateInputFields {
		if drop[f.ColumnName] {
			continue
		}
		edge.ChildFields = append(edge.ChildFields, f)
	}
	return nil
}

// resolveNestedToMany fills the shape-2 and shape-3 half of an edge: the child
// input's surviving fields, the traversed FK assignment, the discriminator
// assignment, and the expressions the `connect` adoption needs.
func resolveNestedToMany(
	edge *NestedEdgeContext,
	rel RelationshipContext,
	parents []ColumnContext,
	target *TableContext,
	want nestedVerbMask,
	resolver *gotype.Resolver,
) *nestedIneligible {
	members := edgeKeyMembers(rel, target)
	if note := splitNestedKeyMembers(edge, rel, target, members); note != nil {
		return note
	}
	if note := resolveNestedKeyAssign(edge, rel, target, members, parents, resolver); note != nil {
		return note
	}
	edge.CompositeKey = len(members) > 1
	edge.LoadKey = rel.LoadKey

	if note := resolveNestedChildInput(edge, rel, target, members, resolver); note != nil {
		return note
	}

	edge.ConnectIDGoType = target.PKColumns[0].GoType
	edge.ConnectBucketGoType = nestedBucketGoType(edge.ConnectIDGoType)

	// §9.9.4's has-one occupancy clause — the occupancy read `create` runs
	// first. Every other verb that would survive needs the same two terms for
	// its own statements, so losing them here leaves the edge nothing: the note
	// names `create`, and the caller reports the empty edge on top of it. With
	// `create` already masked off, the verbs below report their own losses.
	if edge.Shape == "has_one" && edge.HasCreate {
		if reason := resolveNestedHasOneScope(edge, rel, target, parents); reason != "" {
			edge.HasCreate = false
			return &nestedIneligible{edge: rel.FieldName, verb: "create", reason: reason}
		}
	}

	// `connect`, `disconnect` and `clear` all need a nullable FK, one with a free
	// member and every free member nullable: nothing on a NOT NULL edge is ever
	// unparented, and unlinking would violate the constraint. Under MATCH FULL a
	// pinned member narrows them further. The generator declining to emit the
	// fields is the schema saying so at compile time (PRD §9.9.3, §9.9.4).
	rewrite := keyRewriteVerbs(members, rel.Match, edge.AllowReparent)
	if !rewrite.connect && !rewrite.unlink {
		return nil
	}

	// Every statement below scopes itself the same two ways — to this parent
	// and, on a polymorphic edge, to this edge's own discriminator value — so
	// the terms are resolved once here rather than per verb.
	resolveNestedEdgeScope(edge, rel, target, parents)

	if rewrite.unlink {
		if note := resolveNestedUnlink(edge, rel, target, want, resolver); note != nil {
			return note
		}
	}
	if !want.connect || !rewrite.connect {
		return nil
	}
	edge.HasConnect = true
	return resolveNestedAdoption(edge, rel, target, parents, resolver)
}

// resolveNestedHasOneScope resolves the scope a has-one edge's occupancy read
// carries, and returns why it cannot render, or "" when it can.
//
// A has-one `create` or `connect` first reads the edge for a child the parent
// already has, whatever the FK's nullability, because only a UNIQUE
// constraint would make the database refuse a second one (PRD §9.9.6). The
// read needs the same two terms the unlink verbs do, and an edge that cannot
// render them cannot run it. So it loses `create` rather than writing a second
// child. `connect` needs the same terms for its own verify read and is dropped
// there, under the same rule.
func resolveNestedHasOneScope(edge *NestedEdgeContext, rel RelationshipContext, target *TableContext, parents []ColumnContext) string {
	resolveNestedEdgeScope(edge, rel, target, parents)
	if column := unscopedKeyMember(edge, target); column != "" {
		return fmt.Sprintf("the traversed FK column %q carries no `= parent` filter term, so the occupancy read cannot find the child this parent already has", column)
	}
	if rel.Discriminator != nil && edge.DiscFilterExpr == "" {
		return fmt.Sprintf("the discriminator column %q carries no `Eq` filter term, so the occupancy read cannot scope itself to this edge rather than to every edge over %q", rel.Discriminator.Column, rel.FKColumn)
	}
	return ""
}

// resolveNestedEdgeScope fills the filter terms every parent-scoped statement
// on a shape-2 or shape-3 edge carries: `fk = parent`, one term per key member,
// and the discriminator equality that separates one polymorphic edge from its
// siblings over the same foreign key.
//
// Each of the three consumers reads them differently, which is why they are
// resolved unconditionally and gated at the call site: the two unlink verbs
// cannot run without the parent term, the `connect` adoption's UPDATE already
// carries its own id list and takes the discriminator term alone while the
// verify read after it takes both, and an edge with no `discriminator:` leaves
// that half empty.
//
// An unrenderable term leaves the field empty rather than rejecting anything.
// The verbs that need it decide for themselves whether that is fatal.
func resolveNestedEdgeScope(edge *NestedEdgeContext, rel RelationshipContext, target *TableContext, parents []ColumnContext) {
	columns := make([]string, len(edge.KeyMembers))
	for i, m := range edge.KeyMembers {
		columns[i] = m.Column
	}
	edge.ParentScope = nestedParentScope(columns, parents, target.FilterFields)
	if rel.Discriminator == nil {
		return
	}
	df, found := filterFieldByColumn(target.FilterFields, rel.Discriminator.Column)
	if !found {
		return
	}
	expr := nestedEqFilterExpr(df.ComparatorType, comparatorOperandLiteral(df.ComparatorType, rel.Discriminator.GoLiteral))
	if expr == "" {
		return
	}
	edge.DiscFilterField, edge.DiscFilterExpr = df.FieldName, expr
}

// nestedParentScope renders the `fk = parent` term of each column against
// fields, or nil when any of them cannot render. parents[i] is the parent
// column that columns[i] references.
func nestedParentScope(columns []string, parents []ColumnContext, fields []FilterFieldContext) []NestedScopeTerm {
	terms := make([]NestedScopeTerm, 0, len(columns))
	for i, column := range columns {
		v := "pid"
		if len(columns) > 1 {
			v = fmt.Sprintf("pid%d", i)
		}
		ff, found := filterFieldByColumn(fields, column)
		if !found {
			return nil
		}
		expr := nestedEqFilterExpr(ff.ComparatorType, v)
		if expr == "" {
			return nil
		}
		terms = append(terms, NestedScopeTerm{
			FilterField:   ff.FieldName,
			ParentVar:     v,
			ParentKeyExpr: nestedParentKeyExpr(ff.ComparatorType, parents[i]),
			FilterExpr:    expr,
		})
	}
	return terms
}

// unscopedKeyMember names the first key member with no `= parent` term, or ""
// when the edge's scope rendered.
func unscopedKeyMember(edge *NestedEdgeContext, target *TableContext) string {
	if len(edge.ParentScope) > 0 {
		return ""
	}
	for _, m := range edge.KeyMembers {
		ff, found := filterFieldByColumn(target.FilterFields, m.Column)
		if !found || nestedEqFilterExpr(ff.ComparatorType, "pid") == "" {
			return m.Column
		}
	}
	return ""
}

// resolveNestedUnlink fills the expressions `disconnect` and `clear` share on a
// shape-2 or shape-3 edge: the parent-scoped `fk = parent` term both carry, the
// assignment that actually sets the column NULL, and the discriminator
// predicate that keeps one polymorphic edge's unlink off its siblings' rows.
//
// A note here names no verb and takes the whole edge only when the edge would
// otherwise emit an unlink that writes nothing or writes too much; the ordinary
// outcome is that both verbs are simply off.
func resolveNestedUnlink(
	edge *NestedEdgeContext,
	rel RelationshipContext,
	target *TableContext,
	want nestedVerbMask,
	resolver *gotype.Resolver,
) *nestedIneligible {
	if !want.disconnect && !want.clear {
		return nil
	}
	drop := func(reason string) *nestedIneligible {
		// Reported rather than dropped silently, on §9.9.4's rule: the
		// consumer cannot see a binding the emitter cannot render from their
		// own config, where a NOT NULL foreign key or a `verbs` mask entry is
		// right there in it. The note names `disconnect`; `clear` is the same
		// statement with the id list dropped and is lost with it, which the
		// reason says.
		edge.HasDisconnect, edge.HasClear = false, false
		return &nestedIneligible{
			edge: rel.FieldName, verb: "disconnect",
			reason: reason + " — `clear` is the same statement with the id list dropped, so it is lost with it",
		}
	}

	for i := range edge.KeyMembers {
		m := &edge.KeyMembers[i]
		if m.Pinned {
			continue
		}
		updField, found := inputFieldByColumn(target.UpdateInputFields, m.Column)
		if !found {
			return drop(fmt.Sprintf("the traversed FK column %q is absent from the target's update input, so an unlink cannot set it NULL", m.Column))
		}
		m.UnlinkAssignExpr = nestedNullAssignExpr(updField.GoType, resolver)
		if m.UnlinkAssignExpr == "" {
			// The wrong spelling is the one failure a rows-affected check
			// cannot catch, because the rows do match. Refusing to emit an
			// unlink sqlgen cannot spell as "set this column NULL" is the
			// fail-closed direction.
			return drop(fmt.Sprintf("the traversed FK column %q binds to %s on the target's update input, which sqlgen cannot set to NULL rather than omit", m.Column, updField.GoType))
		}
	}

	if column := unscopedKeyMember(edge, target); column != "" {
		return drop(fmt.Sprintf("the traversed FK column %q carries no `= parent` filter term, so an unlink cannot scope itself to this parent", column))
	}
	// The discriminator rule applied to the unlink half. `clear` unlinks every
	// row *on this edge*, and the edge is `fk = parent AND <disc> = <value>`;
	// without the second term it unlinks every sibling edge's rows over the
	// same foreign key too — a silent over-unlink, not a wrong error. The same
	// term makes a `disconnect` naming a row of another kind a no-op, which is
	// what §9.9.6 already says a `disconnect` miss is.
	if rel.Discriminator != nil && edge.DiscFilterExpr == "" {
		return drop(fmt.Sprintf("the discriminator column %q carries no `Eq` filter term, so an unlink cannot scope itself to this edge rather than to every edge over %q", rel.Discriminator.Column, rel.FKColumn))
	}

	edge.HasDisconnect = want.disconnect
	edge.HasClear = want.clear
	return nil
}

// resolveNestedAdoption fills the expressions the `connect` adoption needs on a
// shape-2 or shape-3 edge, and reports the verb rather than dropping it when
// one of them cannot be rendered.
//
// Nothing here is an eligibility rule: the nullable-FK rule has already said
// the edge admits `connect`, and these checks ask whether the adoption can be
// *rendered*. A failure is a generator limitation the consumer cannot see from
// the config, so it is reported for an explicitly-listed edge instead of
// applied silently.
func resolveNestedAdoption(
	edge *NestedEdgeContext,
	rel RelationshipContext,
	target *TableContext,
	parents []ColumnContext,
	resolver *gotype.Resolver,
) *nestedIneligible {
	dropConnect := func(reason string) *nestedIneligible {
		edge.HasConnect = false
		return &nestedIneligible{edge: rel.FieldName, verb: "connect", reason: reason}
	}

	// The visibility read carries the edge's discriminator and the
	// adoption has to carry it too, or a target whose value changed between the
	// two statements is adopted into an edge that can never read it back. The
	// two predicates are not spelled the same way: the read's is raw SQL keyed
	// on the column, the UPDATE's is a filter member, so they can disagree
	// about whether the edge is renderable at all. Refusing `connect` is the
	// fail-closed direction and is what keeps the pair consistent — the unlink
	// half refuses on the identical condition.
	if rel.Discriminator != nil && edge.DiscFilterExpr == "" {
		return dropConnect(fmt.Sprintf("the discriminator column %q carries no `Eq` filter term, so the adoption cannot scope itself to this edge where the visibility read already does", rel.Discriminator.Column))
	}

	for i := range edge.KeyMembers {
		m := &edge.KeyMembers[i]
		if m.Pinned {
			continue
		}
		updField, found := inputFieldByColumn(target.UpdateInputFields, m.Column)
		if !found {
			return dropConnect(fmt.Sprintf("the traversed FK column %q is absent from the target's update input, so an adoption cannot set it", m.Column))
		}
		m.UpdateAssignExpr = nestedAssignExpr(updField.GoType, false, parents[i].GoType, "parent."+parents[i].FieldName, resolver)
		if m.UpdateAssignExpr == "" {
			return dropConnect(fmt.Sprintf("the traversed FK column %q binds to %s on the target's update input, which sqlgen cannot construct from the parent's key", m.Column, updField.GoType))
		}
	}

	if reason := resolveNestedOwnerRead(edge, rel, parents[0], resolver); reason != "" {
		return dropConnect(reason)
	}

	// The verify read after each adoption chunk counts the chunk's ids that
	// now carry `fk = parent`, and that count is the shortfall check on every
	// dialect (PRD §9.9.6). It is required under `allow_reparent` too: the
	// flag removes the guard, not the check.
	if column := unscopedKeyMember(edge, target); column != "" {
		return dropConnect(fmt.Sprintf("the traversed FK column %q carries no `= parent` filter term, so the adoption cannot verify which rows it adopted", column))
	}

	// The `fk IS NULL` term is what makes the adoption atomic against a
	// concurrent connect. Under `allow_reparent` the adoption deliberately
	// carries no such term (PRD §9.9.6), so the filter member it would need is
	// not required either — demanding it anyway would drop `connect` from the
	// one edge that explicitly opted into not using it.
	if edge.AllowReparent {
		return nil
	}
	for i := range edge.KeyMembers {
		m := &edge.KeyMembers[i]
		if m.Pinned {
			continue
		}
		ff, found := filterFieldByColumn(target.FilterFields, m.Column)
		if !found {
			return dropConnect(fmt.Sprintf("the traversed FK column %q is absent from the target's filter, so the adoption has no `IS NULL` guard to run under", m.Column))
		}
		m.FilterField = ff.FieldName
		m.NullFilterExpr = nestedNullFilterExpr(ff.ComparatorType)
		if m.NullFilterExpr == "" {
			return dropConnect(fmt.Sprintf("the traversed FK column %q filters through %s, which has no `Null` member for the adoption guard", m.Column, ff.ComparatorType))
		}
	}
	return nil
}

// resolveNestedOwnerRead fills the expressions that read the key back off a
// visibility-read row, and returns why it cannot, or "" when it can.
//
// The unwrap has to render before the three-way split can: a nullable FK is
// always a pointer or a Null wrapper, so the guard is non-empty in every
// reachable case, and an empty one would emit a guard that is always true
// rather than a split.
func resolveNestedOwnerRead(edge *NestedEdgeContext, rel RelationshipContext, parentKey ColumnContext, resolver *gotype.Resolver) string {
	if edge.CompositeKey {
		if column := resolveNestedOwnerTuple(edge, rel, resolver); column != "" {
			return fmt.Sprintf("the traversed FK column %q carries no null guard for the three-way adoption split", column)
		}
		return ""
	}
	ext := resolver.DeriveScalarExtraction(rel.FKColumnGoType)
	edge.FKOwnerGoType = rel.FKColumnGoType
	edge.FKOwnerGuard = strings.ReplaceAll(ext.GuardExpr, "$v", "cur")
	edge.FKOwnerIsParent = nestedOwnerIsParentExpr(strings.ReplaceAll(ext.UnwrapExpr, "$v", "cur"), parentKey)
	if edge.FKOwnerGuard == "" {
		return fmt.Sprintf("the traversed FK column binds to %s on the target entity, which carries no null guard for the three-way adoption split", rel.FKColumnGoType)
	}
	return ""
}

// nestedOwnerIsParentExpr compares a row's unwrapped key with the parent's. A
// time key compares as an instant: `==` on a time.Time also compares the zone
// pointer, which differs between two rows read back with the same non-hour
// offset.
func nestedOwnerIsParentExpr(key string, parentKey ColumnContext) string {
	parent := nestedKeyValueExpr(parentKey.GoType, "parent."+parentKey.FieldName)
	if parentKey.GoType == "time.Time" || parentKey.GoType == "types.DateTime" {
		return key + ".Equal(" + parent + ")"
	}
	return key + " == " + parent
}

// resolveNestedOwnerTuple fills the expressions that read a composite key off a
// visibility-read row and split the row three ways (PRD §9.9.6). A pinned
// member must equal the parent's value, and a key is held when any free member
// holds a value. NULL is tracked apart from the tuple, because a NULL member
// and an empty string stringify alike. It returns a free member with no null
// guard, which would read NULL as held, or "" when every one has one.
func resolveNestedOwnerTuple(edge *NestedEdgeContext, rel RelationshipContext, resolver *gotype.Resolver) string {
	loadKey, guard := funcFKLoadKey(resolver), funcFKLoadGuard(resolver)
	var mismatch, held []string
	for i, m := range rel.LoadKey {
		goType, expr := keyRead(m, "holder", "row")
		member := NestedOwnerMember{Index: i, Guard: guard(goType, expr), Key: loadKey(goType, expr)}
		edge.OwnerMembers = append(edge.OwnerMembers, member)
		switch {
		case !edge.KeyMembers[i].Pinned:
			if member.Guard == "" {
				return m.Column
			}
			held = append(held, fmt.Sprintf("!null[%d]", i))
		case member.Guard != "":
			mismatch = append(mismatch, fmt.Sprintf("null[%d] || cur[%d] != want[%d]", i, i, i))
		default:
			mismatch = append(mismatch, fmt.Sprintf("cur[%d] != want[%d]", i, i))
		}
	}
	edge.OwnerPinnedMismatch = strings.Join(mismatch, " || ")
	edge.OwnerAllHeld = strings.Join(held, " && ")
	edge.OwnerAnyHeld = strings.Join(held, " || ")
	return ""
}

// junctionLink is one column of a link row and where its value comes from.
type junctionLink struct {
	column, fieldName string
	// sourceGoType / source spell the value, read off the parent or the target.
	sourceGoType, source string
	// fromRow reports a value on the target beyond its primary key.
	fromRow bool
}

// junctionKeyPairs returns an M2M edge's two junction keys. An edge built
// without key pairs has its one column each.
func junctionKeyPairs(rel RelationshipContext, parentKey ColumnContext, target *TableContext) (local, reference []parser.KeyPair) {
	local, reference = rel.JunctionLocal, rel.JunctionReference
	if len(local) == 0 {
		local = []parser.KeyPair{{Local: rel.JunctionLocalFK, Referenced: parentKey.Name}}
	}
	if len(reference) == 0 {
		reference = []parser.KeyPair{{Local: rel.JunctionReferenceFK, Referenced: target.PKColumns[0].Name}}
	}
	return local, reference
}

// junctionLinks lists the union of a junction's two keys. A column in both
// takes the parent's value, and `connect` and `create` refuse a target that
// disagrees (resolveJunctionSharedRead).
func junctionLinks(local, reference []parser.KeyPair, parents []ColumnContext, target *TableContext, fields []InputFieldContext) []junctionLink {
	links := make([]junctionLink, 0, len(local)+len(reference))
	seen := make(map[string]bool, len(local))
	for i, p := range local {
		seen[p.Local] = true
		f, _ := inputFieldByColumn(fields, p.Local)
		links = append(links, junctionLink{
			column: p.Local, fieldName: f.FieldName,
			sourceGoType: parents[i].GoType, source: "parent." + parents[i].FieldName,
		})
	}
	pk := target.PKColumns[0]
	for _, p := range reference {
		if seen[p.Local] {
			continue
		}
		f, _ := inputFieldByColumn(fields, p.Local)
		link := junctionLink{column: p.Local, fieldName: f.FieldName, sourceGoType: pk.GoType, source: "tid"}
		if col, ok := columnBySQLName(target.Columns, p.Referenced); ok && col.Name != pk.Name {
			link.sourceGoType, link.source, link.fromRow = col.GoType, "row."+col.FieldName, true
		}
		links = append(links, link)
	}
	return links
}

// resolveJunctionSharedRead fills what `connect` and `create` need to compare
// each column both junction keys share with the target's own value (PRD
// §9.9.6). The link row takes such a column from the parent, so without this
// nothing reads the target's side, and only a foreign key the database enforces
// would notice. Neither side can be NULL: the edge's referenced columns are NOT
// NULL (PRD §9.9.4).
func resolveJunctionSharedRead(edge *NestedEdgeContext, local, reference []parser.KeyPair, parents []ColumnContext, target *TableContext, resolver *gotype.Resolver) {
	loadKey := funcFKLoadKey(resolver)
	var agrees []string
	for _, p := range reference {
		i := slices.IndexFunc(local, func(l parser.KeyPair) bool { return l.Local == p.Local })
		if i < 0 {
			continue
		}
		col, ok := columnBySQLName(target.Columns, p.Referenced)
		if !ok {
			continue
		}
		if col.Name != target.PKColumns[0].Name {
			edge.JunctionSharedFields = append(edge.JunctionSharedFields, col.FieldName)
		}
		agrees = append(agrees, loadKey(col.GoType, "row."+col.FieldName)+" == "+loadKey(parents[i].GoType, "parent."+parents[i].FieldName))
	}
	edge.JunctionSharedAgrees = strings.Join(agrees, " && ")
}

// setJunctionTenantParent records the parent column the junction's tenant column
// takes through the local key. A tenant column only in the reference key comes
// off the target and cannot contradict the tenant filter on an unlink.
func setJunctionTenantParent(edge *NestedEdgeContext, junction *TableContext, local []parser.KeyPair, parents []ColumnContext) {
	if junction.Tenancy == nil || !junction.Tenancy.Tenanted {
		return
	}
	for i, p := range local {
		if p.Local == junction.Tenancy.Column && !parents[i].Tenant {
			edge.TenantKeyParent, edge.TenantKeyClient = "parent."+parents[i].FieldName, edge.JunctionClientField
		}
	}
}

// resolveNestedM2M fills the shape-4 half: the junction the link rows go
// through and the union of its two keys.
//
// Every M2M verb routes through the junction's own client — `create` and
// `connect` both end in the §9.9.6 link step, which is a batched upsert on the
// junction — so an unresolvable junction makes the whole edge ineligible
// rather than only its connect half. A note naming a verb means the edge
// survives without that verb.
func resolveNestedM2M(
	edge *NestedEdgeContext,
	rel RelationshipContext,
	tc *TableContext,
	target *TableContext,
	byTable tableIndex,
	parentKey ColumnContext,
	want nestedVerbMask,
	resolver *gotype.Resolver,
) *nestedIneligible {
	reject := func(reason string) *nestedIneligible {
		return &nestedIneligible{edge: rel.FieldName, reason: reason}
	}

	// An M2M edge with a `discriminator:` is not write-eligible (PRD §9.9.4,
	// §9.9.5, §13.4.1). §9.9.5 makes the M2M `create` take
	// `Create<Target>Input` unchanged, so there is no elided column for the
	// executor to set: honouring §9.9.5 alone inserts a target the edge's own
	// loader then filters out and links it anyway — a link the caller can never
	// read back, reported as success — and setting the discriminator instead
	// overwrites a field the input type still advertises, which is §9.9.3's
	// accept-and-drop. Narrowing the M2M input the way shapes 2 and 3 are
	// narrowed would resolve it and is deliberately out of scope in v1
	// (§26.12). The read path is untouched: get.go.tmpl's M2M loader applies
	// the predicate exactly as it does on any other polymorphic edge.
	if rel.Discriminator != nil {
		return reject("the edge is many-to-many and declares a `discriminator:`; PRD §9.9.5 makes an M2M `create` take the target's create input unchanged, which leaves the executor no way to set the discriminator column to the edge's declared value")
	}

	local, reference := junctionKeyPairs(rel, parentKey, target)
	junction, ineligible := resolveNestedJunction(rel, local, reference, byTable)
	if ineligible != nil {
		return ineligible
	}
	conflict := junctionLinkTarget(junction, local, reference)
	if conflict == "" {
		// The link step is `<Junction>().UpsertMany(dedup(created ∪ connected))`
		// (PRD §9.9.6), and it runs for `create` as much as for `connect`, which
		// is why this takes the whole edge rather than a verb.
		return reject("the junction table emits no conflict-target constant equal to the union of its two keys for the link step to pass")
	}

	edge.JunctionStructName = junction.StructName
	edge.JunctionClientField = toCamelCase(junction.StructName) + "Client"
	// The link step writes through the junction's own client, so a junction that
	// resolves a tenant counts just as the target does — both feed the parent's
	// single-resolve hoist (PRD §29.6).
	edge.TargetResolvesTenant = edge.TargetResolvesTenant || junction.resolvesTenantDirectly()
	edge.JunctionConflictTarget = conflict

	parents := keyParents(tc, local, parentKey)
	links := junctionLinks(local, reference, parents, target, junction.CreateInputFields)
	setJunctionTenantParent(edge, junction, local, parents)
	if !GoTypeIsValidMapKey(target.PKColumns[0].GoType) {
		// The §9.9.6 link step dedupes `created ∪ connected` through a set
		// keyed on the target id, so a PK that is not a valid Go map key makes
		// every M2M verb unrenderable rather than only the connect half.
		return reject(fmt.Sprintf("the target's primary key binds to %s, which is not a valid Go map key for the link step's dedupe", target.PKColumns[0].GoType))
	}
	for _, link := range links {
		f, _ := inputFieldByColumn(junction.CreateInputFields, link.column)
		expr := nestedAssignExpr(f.GoType, f.Required, link.sourceGoType, link.source, resolver)
		if expr == "" {
			key, from := "reference", "the target's key"
			if slices.ContainsFunc(local, func(p parser.KeyPair) bool { return p.Local == link.column }) {
				key, from = "local", "the parent's key"
			}
			return reject(fmt.Sprintf("the junction's %s FK %q binds to %s, which sqlgen cannot construct from %s", key, link.column, f.GoType, from))
		}
		edge.JunctionLinkFields = append(edge.JunctionLinkFields, NestedFieldAssign{FieldName: link.fieldName, AssignExpr: expr})
		if junction.Tenancy != nil && junction.Tenancy.Tenanted && junction.Tenancy.Column == link.column {
			edge.TenantKeyMember = true
		}
		if link.fromRow {
			edge.TargetReadFields = append(edge.TargetReadFields, strings.TrimPrefix(link.source, "row."))
		}
	}
	resolveJunctionSharedRead(edge, local, reference, parents, target, resolver)
	edge.ConnectIDGoType = target.PKColumns[0].GoType
	edge.ConnectBucketGoType = nestedBucketGoType(edge.ConnectIDGoType)
	// An M2M `connect` writes its link row through the junction's own
	// UpsertMany and is unlinked again through its HardDelete surface; the
	// client generates both on every junction that emits a conflict target
	// (PRD §4.6), so no rule has to require them.
	edge.HasConnect = want.connect
	return resolveNestedM2MUnlink(edge, rel, junction, local, parents, links, want)
}

// resolveNestedM2MUnlink fills the shape-4 half of `disconnect` and `clear`:
// the `<Junction>PK` value HardDeleteMany takes, and the junction filter terms
// HardDeleteWhere scopes a clear with (PRD §9.9.6).
//
// Every reason but one that a verb is lost on this shape is a config-visible
// fact — a soft-delete column on the junction, a junction keyed on something
// other than its two foreign keys — so §9.9.4's rule makes the drop silent. The
// exception is a junction key `disconnect` cannot build: the verb takes target
// ids and reads no row, which is a generator limitation and is reported.
func resolveNestedM2MUnlink(
	edge *NestedEdgeContext,
	rel RelationshipContext,
	junction *TableContext,
	local []parser.KeyPair,
	parents []ColumnContext,
	links []junctionLink,
	want nestedVerbMask,
) *nestedIneligible {
	if !want.disconnect && !want.clear {
		return nil
	}
	// With a soft-delete column on the junction neither mechanism is
	// correct: a hard delete destroys a row the schema marked soft-deletable,
	// and a soft delete leaves the link visible, because the M2M loader's
	// junction query carries no soft-delete predicate (PRD §13.2). Refusing is
	// the fail-closed direction.
	if junction.SoftDelete != nil {
		return nil
	}

	if want.clear {
		columns := make([]string, len(local))
		for i, p := range local {
			columns[i] = p.Local
		}
		edge.ParentScope = nestedParentScope(columns, parents, junction.FilterFields)
		edge.HasClear = len(edge.ParentScope) > 0
	}

	if !want.disconnect {
		return nil
	}
	// HardDeleteMany takes the junction's own key, so the value is expressible
	// only when that key IS the union of the two foreign keys. A junction with a
	// surrogate key takes an id list the executor has no way to build.
	if len(junction.PKColumns) != len(links) {
		return nil
	}
	fields := make([]NestedFieldAssign, 0, len(links))
	for _, link := range links {
		col, ok := columnBySQLName(junction.PKColumns, link.column)
		if !ok {
			return nil
		}
		if link.fromRow {
			return &nestedIneligible{edge: rel.FieldName, verb: "disconnect", reason: fmt.Sprintf(
				"the junction's key holds %q, a target column beyond its primary key, and a disconnect reads no target to take it from", link.column)}
		}
		// The PK struct carries the columns' own Go types rather than the create
		// input's bindings, so the assignment is bare or the verb is off.
		if col.GoType != link.sourceGoType {
			return nil
		}
		fields = append(fields, NestedFieldAssign{FieldName: col.FieldName, AssignExpr: link.source})
	}
	edge.JunctionPKStructName = junction.CompositePKStructName
	edge.JunctionPKFields = fields
	edge.HasDisconnect = true
	return nil
}

// junctionLinkTarget returns the junction's conflict target whose columns are
// exactly the union of its two keys, or "" when it has none (PRD §9.9.4).
//
// A link row's keys are that union. A wider target names a column the row does
// not supply, which is what a surrogate primary key is: UpsertMany refuses it at
// run time or, on an application-generated key, inserts a fresh row per link. A
// narrower one fires on any row sharing part of the pair, and its update half
// rewrites that row into the new link.
func junctionLinkTarget(junction *TableContext, local, reference []parser.KeyPair) string {
	union := make(map[string]bool, len(local)+len(reference))
	for _, p := range slices.Concat(local, reference) {
		union[p.Local] = true
	}
	for _, ct := range junction.ConflictTargets {
		if len(ct.Columns) == len(union) &&
			!slices.ContainsFunc(ct.Columns, func(c string) bool { return !union[c] }) {
			return ct.ConstantName
		}
	}
	return ""
}

// resolveNestedJunction answers the eligibility rules that depend on the M2M
// junction's columns: it is generated, it declares every member of both keys,
// and it carries no column beyond them that a link row could not populate or a
// re-link would reset.
func resolveNestedJunction(rel RelationshipContext, local, reference []parser.KeyPair, byTable tableIndex) (*TableContext, *nestedIneligible) {
	reject := func(reason string) (*TableContext, *nestedIneligible) {
		return nil, &nestedIneligible{edge: rel.FieldName, reason: reason}
	}

	junction, ok := byTable.lookup(rel.JunctionSchema, rel.JunctionTable)
	if !ok {
		return reject(fmt.Sprintf("the junction table %q is not in the generated set, so there is no client to write link rows through", rel.JunctionTable))
	}
	union := make(map[string]bool, len(local)+len(reference))
	for _, p := range local {
		if _, ok := inputFieldByColumn(junction.CreateInputFields, p.Local); !ok {
			return reject(fmt.Sprintf("the junction's local FK %q is absent from its create input", p.Local))
		}
		union[p.Local] = true
	}
	for _, p := range reference {
		if _, ok := inputFieldByColumn(junction.CreateInputFields, p.Local); !ok {
			return reject(fmt.Sprintf("the junction's reference FK %q is absent from its create input", p.Local))
		}
		union[p.Local] = true
	}

	// The junction carries no column beyond its two keys. A required one
	// is a column a nested link row cannot populate. An optional one is
	// worse, because it fails silently: the link step upserts on the
	// union of both keys and UpsertMany puts every other insert column but a
	// sequence column in the update half, so re-linking an existing pair
	// resets the column to its default and reports success. Measured on the
	// mysql example: re-connecting a linked category took
	// `user_categories.slot` from 7 to NULL.
	for _, f := range junction.CreateInputFields {
		if union[f.ColumnName] {
			continue
		}
		if f.Required {
			return reject(fmt.Sprintf("the junction requires column %q beyond its two foreign keys, which a nested link row cannot populate", f.ColumnName))
		}
		if junctionColumnSurvivesRelink(junction, f.ColumnName) {
			continue
		}
		if f.AutoIncrement {
			// UpsertMany leaves a sequence column out of the update half, so
			// a re-link does not reset it, but a nested M2M edge still needs a
			// junction with no column beyond its two FKs (PRD §9.9.4).
			return reject(fmt.Sprintf("the junction carries sequence column %q beyond its two foreign keys, and a nested M2M edge needs a junction with no column beyond them", f.ColumnName))
		}
		return reject(fmt.Sprintf("the junction carries column %q beyond its two foreign keys, which the link step's upsert would reset to its default on every re-link", f.ColumnName))
	}
	return junction, nil
}

// junctionColumnSurvivesRelink reports whether an optional junction column is
// safe under the link step's upsert.
//
// Two kinds are admitted because they are never reset: the primary-key
// columns, because a row's identity is not a mutable attribute (PRD §9.5), and
// the tenant column (PRD §29.4.2). UpsertMany keeps both out of its conflict
// clause's update half. It keeps a sequence column out too, but the junction
// rule (PRD §9.9.4) does not exempt one, so resolveNestedJunction still
// rejects it.
//
// The soft-delete column is reset, and resetting it is correct. Its default is
// the not-deleted state, so re-linking a soft-deleted pair restores the link.
// That is the end state `create` and `connect` ask for, and it is why the
// soft-delete junction rule withholds only the unlink verbs from such a
// junction.
func junctionColumnSurvivesRelink(junction *TableContext, column string) bool {
	for _, pk := range junction.PKColumns {
		if pk.Name == column {
			return true
		}
	}
	if junction.Tenancy != nil && junction.Tenancy.Tenanted && junction.Tenancy.Column == column {
		return true
	}
	return junction.SoftDelete != nil && junction.SoftDelete.Column == column
}

// inputFieldByColumn finds the input field bound to a SQL column.
func inputFieldByColumn(fields []InputFieldContext, column string) (InputFieldContext, bool) {
	for _, f := range fields {
		if f.ColumnName == column {
			return f, true
		}
	}
	return InputFieldContext{}, false
}

// filterFieldByColumn finds the filter member bound to a SQL column.
func filterFieldByColumn(fields []FilterFieldContext, column string) (FilterFieldContext, bool) {
	for _, f := range fields {
		if f.ColumnName == column {
			return f, true
		}
	}
	return FilterFieldContext{}, false
}

// nestedAssignExpr renders the right-hand side that sets an input field of
// type goType from the parent's primary key.
//
// The assignment is type-dependent and the emitter cannot write `UserID:
// parent.ID` and hope: `events.user_id` is nullable and `orders.user_id` is
// NOT NULL — same shape, same relationship type, different spelling. Four
// bindings reach here (design §5.5): a bare column, a pointer, a Null wrapper,
// and the omittable form of each.
//
// Returns "" when the binding is one sqlgen cannot construct, which makes the
// edge ineligible rather than emitting code that does not compile.
func nestedAssignExpr(goType string, required bool, pkGoType, parentExpr string, resolver *gotype.Resolver) string {
	if required {
		if goType == pkGoType {
			return parentExpr
		}
		return ""
	}
	inner, ok := omittableInner(goType)
	if !ok {
		// A non-omittable, non-required field is a bare column the create
		// input declares directly.
		if goType == pkGoType {
			return parentExpr
		}
		return ""
	}
	switch inner {
	case pkGoType:
		return "omittable.Set(" + parentExpr + ")"
	case "*" + pkGoType:
		return "omittable.Set(&" + parentExpr + ")"
	}
	if !nullWrapperHolds(inner, pkGoType) {
		return ""
	}
	lit := nullWrapperLiteral(inner, nestedKeyValueExpr(pkGoType, parentExpr), resolver)
	if lit == "" {
		return ""
	}
	return "omittable.Set(" + lit + ")"
}

// nullWrapperLiteral builds a valid Null-wrapper value carrying parentExpr,
// e.g. `uuid.NullUUID{UUID: parent.ID, Valid: true}`.
//
// The wrapper's two field names are read back off the ScalarExtraction the
// read path already uses (gotype.DeriveScalarExtraction), so the construction
// and the destructuring cannot name different fields. A wrapper whose
// extraction is not a plain `$v.<field>` selector — a pointer, a bare scalar —
// is not a wrapper and returns "".
func nullWrapperLiteral(goType, valueExpr string, resolver *gotype.Resolver) string {
	ext := resolver.DeriveScalarExtraction(goType)
	underlying, ok := strings.CutPrefix(ext.UnwrapExpr, "$v.")
	if !ok || underlying == "" {
		return ""
	}
	guard := ext.GuardExpr
	valid := "true"
	if inverted, negated := strings.CutPrefix(guard, "!"); negated {
		guard, valid = inverted, "false"
	}
	validField, ok := strings.CutPrefix(guard, "$v.")
	if !ok || validField == "" {
		return ""
	}
	// A validity predicate spelled as a method call (`valid_method:`) has no
	// assignable field behind it, so the wrapper cannot be constructed.
	if strings.HasSuffix(validField, "()") {
		return ""
	}
	return fmt.Sprintf("%s{%s: %s, %s: %s}", goType, underlying, valueExpr, validField, valid)
}

// nestedLiteralExpr renders a constant value into an input field of type
// goType — the discriminator assignment every nested create on a polymorphic
// edge carries.
//
// The literal is an untyped Go constant, so it converts to a named string,
// integer or boolean type on assignment, and to nothing else: pointerWrapped
// refuses the rest. The omittable form needs the type argument spelled out:
// omittable.Set would otherwise infer the constant's default type rather than
// the field's.
func nestedLiteralExpr(goType string, required bool, literal string, resolver *gotype.Resolver) string {
	if required {
		return pointerWrapped(goType, literal, resolver)
	}
	inner, ok := omittableInner(goType)
	if !ok {
		return pointerWrapped(goType, literal, resolver)
	}
	value := pointerWrapped(inner, literal, resolver)
	if value == "" {
		return ""
	}
	return fmt.Sprintf("omittable.Set[%s](%s)", inner, value)
}

// pointerWrapped renders a constant into a field of type goType, taking the
// constant's address when the binding is a pointer. `new(v)` is what makes a
// nullable column's declared discriminator expressible at all: the untyped
// constant converts to the pointee on the way in.
//
// Returns "" for a binding the constant cannot be assigned to, which makes the
// edge ineligible rather than emitting code that does not compile.
func pointerWrapped(goType, literal string, resolver *gotype.Resolver) string {
	bare, pointer := strings.CutPrefix(goType, "*")
	if !constantAssignable(bare, literal, resolver) {
		return ""
	}
	if pointer {
		return "new(" + bare + "(" + literal + "))"
	}
	return literal
}

// constantAssignable reports whether literal, a discriminator value as
// discriminatorGoLiteral renders it, is assignable to the Go type goType.
//
// An untyped constant is assignable only to a type whose underlying type is a
// basic type of the constant's own kind. The generator cannot resolve the
// underlying type of an arbitrary named type, so it accepts the two sets it can
// vouch for, the predeclared types and the schema enums it generates into the
// same package, and refuses everything else. That covers a binding no declared
// value can reach (`types.JSON`, `uuid.UUID`, `time.Time`, `net.IP`, a Null
// wrapper, a slice or enum array, a MySQL SET, a same-package struct a
// `type_map` names, a pointer to a pointer) and a value of the wrong kind for
// its column, such as a string declared on an integer column. Each would
// otherwise emit a nested `create` that does not compile.
//
// The enum test asks the resolver rather than isEnumLikeType: that one accepts
// every unqualified exported name, including a `type_map` struct and a named
// slice, and a constant is assignable to neither.
func constantAssignable(goType, literal string, resolver *gotype.Resolver) bool {
	switch {
	case strings.HasPrefix(literal, `"`):
		return goType == "string" || resolver.IsEnumGoType(goType)
	case literal == "true" || literal == "false":
		return goType == "bool"
	case strings.ContainsAny(literal, ".eE"):
		return goType == "float32" || goType == "float64"
	}
	switch goType {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"byte", "rune", "float32", "float64":
		return true
	}
	return false
}

// omittableInner unwraps `omittable.Value[T]` to T.
func omittableInner(goType string) (string, bool) {
	inner, ok := strings.CutPrefix(goType, "omittable.Value[")
	if !ok {
		return "", false
	}
	inner, ok = strings.CutSuffix(inner, "]")
	return inner, ok
}

// nestedNullFilterExpr renders the `IS NULL` term the adoption UPDATE's guard
// carries, for a filter member of the given comparator type. The guard is what
// makes the adoption atomic against a concurrent connect that parented the row
// between the visibility read and the UPDATE (PRD §9.9.6).
//
// Returns "" for a comparator with no Null member, which drops the verb rather
// than emitting an unguarded adoption.
func nestedNullFilterExpr(comparatorType string) string {
	// FilterFieldContext.ComparatorType carries the member's declared type,
	// which is already a pointer (`*comparator.NullableID`); the literal takes
	// its address instead.
	//
	// The test is the whole `comparator.Nullable…` family rather than the three
	// spellings a foreign key resolves to today. Every member of it embeds its
	// non-nullable base and adds `Null *bool`, so the literal below compiles
	// against all of them, and naming three was a narrower rule than the fact
	// it encodes.
	//
	// A one-column FK lands on Enum when its column is an enum, and otherwise on
	// the ID, Number or Opaque family, because applyConfigDeclaredFKs stamps a synthetic FKReference on every
	// config-declared `fk:` and the column classifies as a key. A composite-key
	// member classifies by its column and can land on the others. What makes a
	// residual miss loud rather than silent is the per-verb diagnostic the
	// caller returns, not this list.
	base := strings.TrimPrefix(comparatorType, "*")
	if strings.HasPrefix(base, "comparator.Nullable") {
		return "&" + base + "{Null: new(true)}"
	}
	return ""
}

// nestedEqFilterExpr renders the `= <value>` term a parent-scoped unlink or a
// discriminator scope carries, for a filter member of the given comparator
// type. valueExpr must already be spelled in the comparator's own operand type.
//
// Every nullable comparator embeds its non-nullable base and adds
// `Null *bool`, so the nullable form is the base literal assigned into the
// embedded field — the same shape pkFilterInExpr renders for `In`.
//
// Returns "" for a family with no `Eq` member (comparator.JSON,
// comparator.JSONB, comparator.Slice), which drops the verb rather than
// emitting an unlink with no scope.
func nestedEqFilterExpr(comparatorType, valueExpr string) string {
	switch nestedComparatorFamily(comparatorType) {
	case "Bool", "Enum", "ID", "Number", "Opaque", "String", "Time":
	default:
		return ""
	}
	base := strings.TrimPrefix(comparatorType, "*")
	inner, nullable := strings.CutPrefix(base, "comparator.Nullable")
	if !nullable {
		return fmt.Sprintf("&%s{Eq: new(%s)}", base, valueExpr)
	}
	embedded, _, _ := strings.Cut(inner, "[")
	return fmt.Sprintf("&%s{%s: comparator.%s{Eq: new(%s)}}", base, embedded, inner, valueExpr)
}

// nestedComparatorFamily names a filter field's comparator family, without the
// pointer, the Nullable prefix or a type argument.
func nestedComparatorFamily(comparatorType string) string {
	family := strings.TrimPrefix(strings.TrimPrefix(comparatorType, "*"), "comparator.")
	family, _, _ = strings.Cut(strings.TrimPrefix(family, "Nullable"), "[")
	return family
}

// comparatorOperandLiteral spells a Go source literal in the operand type a
// comparator's `Eq` member takes.
//
// A discriminator value arrives as an untyped constant, which assigns to a
// named string, integer or boolean type on its own — but `new()` infers the
// constant's *default* type, so an enum-bound column needs the conversion
// written out or the generated code does not compile. The generic families
// carry their operand as the type argument; the rest take it as spelled.
func comparatorOperandLiteral(comparatorType, literal string) string {
	base := strings.TrimPrefix(strings.TrimPrefix(comparatorType, "*"), "comparator.")
	_, arg, generic := strings.Cut(base, "[")
	if !generic {
		return literal
	}
	arg = strings.TrimSuffix(arg, "]")
	if arg == "" {
		return literal
	}
	return arg + "(" + literal + ")"
}

// nestedParentKeyExpr spells the parent's referenced column in the operand type
// of the comparator filtering the key member: a string on comparator.ID and
// comparator.String, the column's own value on every other family (PRD §11.2).
//
// The filter field decides, not the parent's Go type. A key member classifies
// by the column holding it, so an enum, timestamp or boolean member need not
// land on the family a primary key of that type would.
func nestedParentKeyExpr(comparatorType string, parentKey ColumnContext) string {
	expr := "parent." + parentKey.FieldName
	switch nestedComparatorFamily(comparatorType) {
	case "ID", "String":
		return funcFKToString(parentKey, expr)
	}
	return nestedKeyValueExpr(parentKey.GoType, expr)
}

// builtinWrapperValues is the Go type each built-in integration's Null wrapper
// holds; database/sql's come from gotype.StdNullUnderlying.
var builtinWrapperValues = map[string]string{
	"types.NullDateTime":  "time.Time",
	"uuid.NullUUID":       "uuid.UUID",
	"decimal.NullDecimal": "decimal.Decimal",
}

// nullWrapperHolds reports whether a Null wrapper can carry the parent's key,
// read as nestedKeyValueExpr reads it. A built-in wrapper holds one Go type:
// sql.NullInt32 cannot carry an int64 key, nor types.NullDateTime a string. A
// user-declared wrapper's value type is the consumer's, so it is not refused.
func nullWrapperHolds(wrapper, pkGoType string) bool {
	held, ok := gotype.StdNullUnderlying(wrapper)
	if !ok {
		held, ok = builtinWrapperValues[wrapper]
	}
	if !ok {
		return true
	}
	if pkGoType == dateTimeGoType {
		pkGoType = "time.Time"
	}
	return held == pkGoType
}

// nestedKeyValueExpr reads a key column as the value comparator.Time and
// types.NullDateTime hold: types.DateTime embeds a time.Time and is not one.
func nestedKeyValueExpr(goType, expr string) string {
	if goType == "types.DateTime" {
		return expr + ".Time"
	}
	return expr
}

// nestedBucketGoType is the map key type the nested executor buckets a target
// key of goType on (NestedEdgeContext.ConnectBucketGoType). A time key buckets
// as a time.Time in UTC, see funcNestedBucketKey; every other key buckets as
// itself.
func nestedBucketGoType(goType string) string {
	if isTimeKeyGoType(goType) {
		return "time.Time"
	}
	return goType
}

// nestedNullAssignExpr renders the right-hand side that sets a nullable FK to
// NULL through the target's update input.
//
// This is the one assignment in the feature where the wrong spelling compiles.
// `omittable.Set[*T](nil)` puts the column in the SET list with a NULL value;
// the zero `omittable.Value[*T]{}` leaves it out of the SET list entirely, and
// since nullable UUIDs bind to `*uuid.UUID` both are accepted in the
// same position. The second matches its rows, reports success and unlinks
// nothing — the one failure shape a rows-affected check cannot catch, because
// the rows do match.
//
// Returns "" for a binding that is neither a pointer nor a Null wrapper, which
// drops the verb rather than emitting an unlink that writes the wrong thing.
func nestedNullAssignExpr(goType string, resolver *gotype.Resolver) string {
	inner, ok := omittableInner(goType)
	if !ok {
		// A non-omittable update field is always in the SET list, so there is
		// no "omit" to distinguish NULL from — but there is also no way to
		// spell a value for a type sqlgen did not choose. Only the omittable
		// form reaches an update input on a nullable column (PRD §10.1).
		return ""
	}
	if strings.HasPrefix(inner, "*") {
		return "omittable.Set[" + inner + "](nil)"
	}
	lit := nullWrapperNullLiteral(inner, resolver)
	if lit == "" {
		return ""
	}
	return "omittable.Set(" + lit + ")"
}

// nullWrapperNullLiteral builds the NULL value of a Null-wrapper type, e.g.
// `uuid.NullUUID{Valid: false}`.
//
// It is nullWrapperLiteral's inverse and reads the same ScalarExtraction the
// read path destructures with, so the two cannot name different fields. The
// validity field is written explicitly rather than relying on the zero value:
// a wrapper whose predicate is inverted — `!$v.Invalid` — has a zero value
// that means *valid*, and emitting a bare `T{}` there would set the column to
// the type's zero rather than to NULL.
func nullWrapperNullLiteral(goType string, resolver *gotype.Resolver) string {
	ext := resolver.DeriveScalarExtraction(goType)
	guard := ext.GuardExpr
	invalid := "false"
	if inverted, negated := strings.CutPrefix(guard, "!"); negated {
		guard, invalid = inverted, "true"
	}
	validField, ok := strings.CutPrefix(guard, "$v.")
	if !ok || validField == "" || strings.HasSuffix(validField, "()") {
		return ""
	}
	return fmt.Sprintf("%s{%s: %s}", goType, validField, invalid)
}

// GoTypeIsValidMapKey reports whether a Go type can key a map. The
// comparator.Opaque family (PRD §11.2) is exactly the set of key types that
// cannot: []byte, net.IP and net.HardwareAddr are slices, and net.IPNet is a
// struct holding two of them.
//
// Four places in a nested executor need the property — the `connect` owner map,
// the verb-conflict set, the M2M link step's dedupe, and the self-reference
// guard's `==` comparison — so an edge into such a table carries neither
// `connect` nor `disconnect`, and an M2M one carries no verb at all.
//
// Exported for the codegen invariant test. renderNested parses the emitted Go
// but does not type-check it, so an edge that reached this shape emitted
// `map[[]byte]bool` with every gate in the repo green; the test asserts against
// this function rather than restating §11.2's family, which would be a second
// derivation of the same fact.
func GoTypeIsValidMapKey(goType string) bool {
	base := strings.TrimPrefix(goType, "*")
	family, _ := pkComparatorFamily(base)
	return family != "Opaque"
}
