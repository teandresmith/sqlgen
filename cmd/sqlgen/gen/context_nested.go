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
			if _, listed := allow[r.edge]; listed {
				errs = append(errs, fmt.Errorf("tables.%s.nested_mutations: %s", table, r))
			}
		}
	}
	return errors.Join(errs...)
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
// only source there. The traversed FK's *column* type is the owner map's
// value, which is the Null-wrapper or pointer form rather than the input form.
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
	if col, ok := columnBySQLName(target.Columns, rel.FKColumn); ok {
		add(col.Import)
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
func resolveNestedTarget(rel RelationshipContext, byTable tableIndex) (*TableContext, *nestedIneligible) {
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

	target, ineligible := resolveNestedTarget(rel, byTable)
	if ineligible != nil {
		return NestedEdgeContext{}, []nestedIneligible{*ineligible}
	}

	parentPK := tc.PKColumns[0]
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
	if isM2M {
		if err := resolveNestedM2M(&edge, rel, target, byTable, parentPK, want, resolver); err != nil {
			return NestedEdgeContext{}, []nestedIneligible{*err}
		}
		edge.ChildCreateType = "Create" + target.StructName + "Input"
	} else {
		note := resolveNestedToMany(&edge, rel, target, parentPK, want, resolver)
		if note != nil {
			if note.verb == "" {
				return NestedEdgeContext{}, []nestedIneligible{*note}
			}
			notes = append(notes, *note)
		}
		edge.ChildInputName = nestedChildInputName(tc.StructName, rel.FieldName)
		edge.ChildCreateType = edge.ChildInputName
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

// resolveNestedToMany fills the shape-2 and shape-3 half of an edge: the child
// input's surviving fields, the traversed FK assignment, the discriminator
// assignment, and the expressions the `connect` adoption needs.
func resolveNestedToMany(
	edge *NestedEdgeContext,
	rel RelationshipContext,
	target *TableContext,
	parentPK ColumnContext,
	want nestedVerbMask,
	resolver *gotype.Resolver,
) *nestedIneligible {
	reject := func(reason string) *nestedIneligible {
		return &nestedIneligible{edge: rel.FieldName, reason: reason}
	}

	fkField, ok := inputFieldByColumn(target.CreateInputFields, rel.FKColumn)
	if !ok {
		return reject(fmt.Sprintf("the traversed FK column %q is absent from the target's create input, so a nested create cannot set it", rel.FKColumn))
	}
	edge.FKFieldName = fkField.FieldName
	edge.FKAssignExpr = nestedAssignExpr(fkField.GoType, fkField.Required, parentPK.GoType, "parent."+parentPK.FieldName, resolver)
	if edge.FKAssignExpr == "" {
		return reject(fmt.Sprintf("the traversed FK column %q binds to %s, which sqlgen cannot construct from the parent's key", rel.FKColumn, fkField.GoType))
	}

	drop := map[string]bool{rel.FKColumn: true}
	if rel.Discriminator != nil {
		// The discriminator is set by the executor from the edge's declared
		// value, so it is elided from the caller's input the same way the
		// traversed FK is.
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

	edge.ConnectIDGoType = target.PKColumns[0].GoType

	// §9.9.4's has-one occupancy clause — the occupancy read `create` runs
	// first. Every other verb that would survive needs the same two terms for
	// its own statements, so losing them here leaves the edge nothing: the note
	// names `create`, and the caller reports the empty edge on top of it. With
	// `create` already masked off, the verbs below report their own losses.
	if edge.Shape == "has_one" && edge.HasCreate {
		if reason := resolveNestedHasOneScope(edge, rel, target, parentPK); reason != "" {
			edge.HasCreate = false
			return &nestedIneligible{edge: rel.FieldName, verb: "create", reason: reason}
		}
	}

	// `connect`, `disconnect` and `clear` all need a nullable FK: nothing
	// on a NOT NULL edge is ever unparented, and unlinking would violate the
	// constraint. The generator declining to emit the fields is the schema
	// saying so at compile time (PRD §9.9.3).
	if !rel.FKNullable {
		return nil
	}

	// Every statement below scopes itself the same two ways — to this parent
	// and, on a polymorphic edge, to this edge's own discriminator value — so
	// the terms are resolved once here rather than per verb.
	resolveNestedEdgeScope(edge, rel, target, parentPK)

	if note := resolveNestedUnlink(edge, rel, target, want, resolver); note != nil {
		return note
	}
	if !want.connect {
		return nil
	}
	edge.HasConnect = true
	return resolveNestedAdoption(edge, rel, target, parentPK, resolver)
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
func resolveNestedHasOneScope(edge *NestedEdgeContext, rel RelationshipContext, target *TableContext, parentPK ColumnContext) string {
	resolveNestedEdgeScope(edge, rel, target, parentPK)
	if edge.ParentFilterExpr == "" {
		return fmt.Sprintf("the traversed FK column %q carries no `= parent` filter term, so the occupancy read cannot find the child this parent already has", rel.FKColumn)
	}
	if rel.Discriminator != nil && edge.DiscFilterExpr == "" {
		return fmt.Sprintf("the discriminator column %q carries no `Eq` filter term, so the occupancy read cannot scope itself to this edge rather than to every edge over %q", rel.Discriminator.Column, rel.FKColumn)
	}
	return ""
}

// resolveNestedEdgeScope fills the two filter terms every parent-scoped
// statement on a shape-2 or shape-3 edge carries: `fk = parent`, and the
// discriminator equality that separates one polymorphic edge from its siblings
// over the same foreign key.
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
func resolveNestedEdgeScope(edge *NestedEdgeContext, rel RelationshipContext, target *TableContext, parentPK ColumnContext) {
	if ff, found := filterFieldByColumn(target.FilterFields, rel.FKColumn); found {
		edge.FKFilterField = ff.FieldName
		edge.ParentKeyExpr = nestedParentKeyExpr(rel.FKGoType, parentPK)
		edge.ParentFilterExpr = nestedEqFilterExpr(ff.ComparatorType, "pid")
		if edge.ParentFilterExpr == "" {
			edge.ParentKeyExpr = ""
		}
	}
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

	updField, found := inputFieldByColumn(target.UpdateInputFields, rel.FKColumn)
	if !found {
		return drop(fmt.Sprintf("the traversed FK column %q is absent from the target's update input, so an unlink cannot set it NULL", rel.FKColumn))
	}
	edge.FKUnlinkAssignExpr = nestedNullAssignExpr(updField.GoType, resolver)
	if edge.FKUnlinkAssignExpr == "" {
		// The wrong spelling is the one failure a rows-affected check
		// cannot catch, because the rows do match. Refusing to emit an unlink
		// sqlgen cannot spell as "set this column NULL" is the fail-closed
		// direction.
		return drop(fmt.Sprintf("the traversed FK column %q binds to %s on the target's update input, which sqlgen cannot set to NULL rather than omit", rel.FKColumn, updField.GoType))
	}

	if edge.ParentFilterExpr == "" {
		return drop(fmt.Sprintf("the traversed FK column %q carries no `= parent` filter term, so an unlink cannot scope itself to this parent", rel.FKColumn))
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
	parentPK ColumnContext,
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

	updField, found := inputFieldByColumn(target.UpdateInputFields, rel.FKColumn)
	if !found {
		return dropConnect(fmt.Sprintf("the traversed FK column %q is absent from the target's update input, so an adoption cannot set it", rel.FKColumn))
	}
	edge.FKUpdateAssignExpr = nestedAssignExpr(updField.GoType, false, parentPK.GoType, "parent."+parentPK.FieldName, resolver)
	if edge.FKUpdateAssignExpr == "" {
		return dropConnect(fmt.Sprintf("the traversed FK column %q binds to %s on the target's update input, which sqlgen cannot construct from the parent's key", rel.FKColumn, updField.GoType))
	}

	// The unwrap has to render before the three-way split can: a nullable FK
	// is always a pointer or a Null wrapper, so the guard is non-empty in
	// every reachable case, and an empty one would emit a guard that is always
	// true rather than a split.
	ext := resolver.DeriveScalarExtraction(rel.FKColumnGoType)
	edge.FKOwnerGoType = rel.FKColumnGoType
	edge.FKOwnerGuard = strings.ReplaceAll(ext.GuardExpr, "$v", "cur")
	edge.FKOwnerKey = strings.ReplaceAll(ext.UnwrapExpr, "$v", "cur")
	if edge.FKOwnerGuard == "" {
		return dropConnect(fmt.Sprintf("the traversed FK column binds to %s on the target entity, which carries no null guard for the three-way adoption split", rel.FKColumnGoType))
	}

	// The verify read after each adoption chunk counts the chunk's ids that
	// now carry `fk = parent`, and that count is the shortfall check on every
	// dialect (PRD §9.9.6). It is required under `allow_reparent` too: the
	// flag removes the guard, not the check.
	if edge.ParentFilterExpr == "" {
		return dropConnect(fmt.Sprintf("the traversed FK column %q carries no `= parent` filter term, so the adoption cannot verify which rows it adopted", rel.FKColumn))
	}

	// The `fk IS NULL` term is what makes the adoption atomic against a
	// concurrent connect. Under `allow_reparent` the adoption deliberately
	// carries no such term (PRD §9.9.6), so the filter member it would need is
	// not required either — demanding it anyway would drop `connect` from the
	// one edge that explicitly opted into not using it.
	if edge.AllowReparent {
		return nil
	}
	ff, found := filterFieldByColumn(target.FilterFields, rel.FKColumn)
	if !found {
		return dropConnect(fmt.Sprintf("the traversed FK column %q is absent from the target's filter, so the adoption has no `IS NULL` guard to run under", rel.FKColumn))
	}
	edge.FKFilterNullExpr = nestedNullFilterExpr(ff.ComparatorType)
	if edge.FKFilterNullExpr == "" {
		return dropConnect(fmt.Sprintf("the traversed FK column %q filters through %s, which has no `Null` member for the adoption guard", rel.FKColumn, ff.ComparatorType))
	}
	return nil
}

// resolveNestedM2M fills the shape-4 half: the junction the link rows go
// through and the two FK members they carry.
//
// Every M2M verb routes through the junction's own client — `create` and
// `connect` both end in the §9.9.6 link step, which is a batched upsert on the
// junction — so an unresolvable junction makes the whole edge ineligible
// rather than only its connect half.
func resolveNestedM2M(
	edge *NestedEdgeContext,
	rel RelationshipContext,
	target *TableContext,
	byTable tableIndex,
	parentPK ColumnContext,
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

	junction, ineligible := resolveNestedJunction(rel, byTable)
	if ineligible != nil {
		return ineligible
	}
	conflict := ""
	for _, ct := range junction.ConflictTargets {
		if ct.CoversPK {
			conflict = ct.ConstantName
			break
		}
	}
	if conflict == "" {
		// The upsert conflict-target rule one level down: a table whose
		// uniqueness is app-enforced through `primary_key.columns` emits no
		// conflict-target constant (PRD §9.5), and the link step has to pass
		// one.
		return reject("the junction table emits no PK-covering conflict-target constant for the link step to pass")
	}
	localField, _ := inputFieldByColumn(junction.CreateInputFields, rel.JunctionLocalFK)
	refField, _ := inputFieldByColumn(junction.CreateInputFields, rel.JunctionReferenceFK)

	edge.JunctionStructName = junction.StructName
	edge.JunctionClientField = toCamelCase(junction.StructName) + "Client"
	// The link step writes through the junction's own client, so a junction that
	// resolves a tenant counts just as the target does — both feed the parent's
	// single-resolve hoist (PRD §29.6).
	edge.TargetResolvesTenant = edge.TargetResolvesTenant || junction.resolvesTenantDirectly()
	edge.JunctionConflictTarget = conflict
	edge.JunctionLocalField = localField.FieldName
	edge.JunctionReferenceField = refField.FieldName
	edge.JunctionLocalAssignExpr = nestedAssignExpr(localField.GoType, localField.Required, parentPK.GoType, "parent."+parentPK.FieldName, resolver)
	if edge.JunctionLocalAssignExpr == "" {
		return reject(fmt.Sprintf("the junction's local FK %q binds to %s, which sqlgen cannot construct from the parent's key", rel.JunctionLocalFK, localField.GoType))
	}
	if !GoTypeIsValidMapKey(target.PKColumns[0].GoType) {
		// The §9.9.6 link step dedupes `created ∪ connected` through a set
		// keyed on the target id, so a PK that is not a valid Go map key makes
		// every M2M verb unrenderable rather than only the connect half.
		return reject(fmt.Sprintf("the target's primary key binds to %s, which is not a valid Go map key for the link step's dedupe", target.PKColumns[0].GoType))
	}
	edge.JunctionReferenceAssignExpr = nestedAssignExpr(refField.GoType, refField.Required, target.PKColumns[0].GoType, "tid", resolver)
	if edge.JunctionReferenceAssignExpr == "" {
		return reject(fmt.Sprintf("the junction's reference FK %q binds to %s, which sqlgen cannot construct from the target's key", rel.JunctionReferenceFK, refField.GoType))
	}
	edge.ConnectIDGoType = target.PKColumns[0].GoType
	// An M2M `connect` writes its link row through the junction's own
	// UpsertMany and is unlinked again through its HardDelete surface; the
	// client generates both on every junction that emits a conflict target
	// (PRD §4.6), so no rule has to require them.
	edge.HasConnect = want.connect
	resolveNestedM2MUnlink(edge, rel, junction, parentPK, want)
	return nil
}

// resolveNestedM2MUnlink fills the shape-4 half of `disconnect` and `clear`:
// the `<Junction>PK` pair HardDeleteMany takes, and the junction filter term
// HardDeleteWhere scopes a clear with (PRD §9.9.6).
//
// Nothing here reports. Every reason a verb is lost on this shape is a
// config-visible fact — a soft-delete column on the junction, a junction keyed
// on something other than its two foreign keys — so §9.9.4's rule makes the
// drop silent.
func resolveNestedM2MUnlink(
	edge *NestedEdgeContext,
	rel RelationshipContext,
	junction *TableContext,
	parentPK ColumnContext,
	want nestedVerbMask,
) {
	if !want.disconnect && !want.clear {
		return
	}
	// With a soft-delete column on the junction neither mechanism is
	// correct: a hard delete destroys a row the schema marked soft-deletable,
	// and a soft delete leaves the link visible, because the M2M loader's
	// junction query carries no soft-delete predicate (PRD §13.2). Refusing is
	// the fail-closed direction.
	if junction.SoftDelete != nil {
		return
	}

	if want.clear {
		if ff, found := filterFieldByColumn(junction.FilterFields, rel.JunctionLocalFK); found {
			edge.JunctionLocalFilterField = ff.FieldName
			edge.ParentKeyExpr = nestedParentKeyExpr(rel.FKColumnGoType, parentPK)
			edge.ParentFilterExpr = nestedEqFilterExpr(ff.ComparatorType, "pid")
			edge.HasClear = edge.ParentFilterExpr != ""
		}
	}

	if !want.disconnect {
		return
	}
	// HardDeleteMany takes the junction's own key, so the pair is expressible
	// only when that key IS the two foreign keys. A junction with a surrogate
	// key takes an id list the executor has no way to build.
	if !junction.CompositePK || len(junction.PKColumns) != 2 {
		return
	}
	local, okLocal := columnBySQLName(junction.PKColumns, rel.JunctionLocalFK)
	ref, okRef := columnBySQLName(junction.PKColumns, rel.JunctionReferenceFK)
	if !okLocal || !okRef {
		return
	}
	// The PK struct carries the columns' own Go types rather than the create
	// input's bindings, so the two assignments are bare or the verb is off.
	if local.GoType != parentPK.GoType || ref.GoType != edge.ConnectIDGoType {
		return
	}
	edge.JunctionPKStructName = junction.CompositePKStructName
	edge.JunctionPKLocalField = local.FieldName
	edge.JunctionPKReferenceField = ref.FieldName
	edge.JunctionPKLocalAssignExpr = "parent." + parentPK.FieldName
	edge.JunctionPKReferenceAssignExpr = "tid"
	edge.HasDisconnect = true
}

// resolveNestedJunction answers the eligibility rules that depend on the M2M
// junction alone: it is generated, it emits the conflict target the link step's
// UpsertMany takes, it declares both FK members, and it carries no column
// beyond them that a link row could not populate or a re-link would reset.
func resolveNestedJunction(rel RelationshipContext, byTable tableIndex) (*TableContext, *nestedIneligible) {
	reject := func(reason string) (*TableContext, *nestedIneligible) {
		return nil, &nestedIneligible{edge: rel.FieldName, reason: reason}
	}

	junction, ok := byTable.lookup(rel.JunctionSchema, rel.JunctionTable)
	if !ok {
		return reject(fmt.Sprintf("the junction table %q is not in the generated set, so there is no client to write link rows through", rel.JunctionTable))
	}
	if len(junction.ConflictTargets) == 0 {
		// The link step is `<Junction>().UpsertMany(dedup(created ∪ connected))`
		// (PRD §9.9.6), and a junction with no conflict-target constant has
		// its upsert operations resolved off (PRD §9.5), so there is neither a
		// target to pass nor a method to call. It runs for `create` as much as
		// for `connect`, which is why it takes the whole edge.
		return reject("the junction table emits no PK-covering conflict-target constant for the link step to pass")
	}
	if _, ok := inputFieldByColumn(junction.CreateInputFields, rel.JunctionLocalFK); !ok {
		return reject(fmt.Sprintf("the junction's local FK %q is absent from its create input", rel.JunctionLocalFK))
	}
	if _, ok := inputFieldByColumn(junction.CreateInputFields, rel.JunctionReferenceFK); !ok {
		return reject(fmt.Sprintf("the junction's reference FK %q is absent from its create input", rel.JunctionReferenceFK))
	}

	// The junction carries no column beyond its two FKs. A required one
	// is a column a nested link row cannot populate. An optional one is
	// worse, because it fails silently: the link step upserts on the
	// junction's key and UpsertMany puts every other insert column in the
	// update half, so re-linking an existing pair resets the column to its
	// default and reports success. Measured on the mysql example:
	// re-connecting a linked category took `user_categories.slot` from 7 to
	// NULL.
	for _, f := range junction.CreateInputFields {
		if f.ColumnName == rel.JunctionLocalFK || f.ColumnName == rel.JunctionReferenceFK {
			continue
		}
		if f.Required {
			return reject(fmt.Sprintf("the junction requires column %q beyond its two foreign keys, which a nested link row cannot populate", f.ColumnName))
		}
		if junctionColumnSurvivesRelink(junction, f.ColumnName) {
			continue
		}
		return reject(fmt.Sprintf("the junction carries column %q beyond its two foreign keys, which the link step's upsert would reset to its default on every re-link", f.ColumnName))
	}
	return junction, nil
}

// junctionColumnSurvivesRelink reports whether an optional junction column is
// safe under the link step's upsert.
//
// Two kinds are never reset: the primary-key columns, because a row's identity
// is not a mutable attribute (PRD §9.5), and the tenant column (PRD §29.4.2).
// UpsertMany keeps both out of its conflict clause's update half.
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
	lit := nullWrapperLiteral(inner, parentExpr, resolver)
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
	// No schema reaches the other members through an FK: applyConfigDeclaredFKs
	// stamps a synthetic FKReference on every config-declared `fk:`, so an
	// edge's traversed column always classifies as a key and lands on the ID,
	// Number or Opaque family. The widening is therefore defensive; what makes
	// a residual miss loud rather than silent is the per-verb diagnostic the
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
	base := strings.TrimPrefix(comparatorType, "*")
	inner, nullable := strings.CutPrefix(base, "comparator.Nullable")
	family := inner
	if !nullable {
		family = strings.TrimPrefix(base, "comparator.")
	}
	if name, _, generic := strings.Cut(family, "["); generic {
		family = name
	}
	switch family {
	case "Bool", "Enum", "ID", "Number", "Opaque", "String", "Time":
	default:
		return ""
	}
	if !nullable {
		return fmt.Sprintf("&%s{Eq: new(%s)}", base, valueExpr)
	}
	embedded, _, _ := strings.Cut(inner, "[")
	return fmt.Sprintf("&%s{%s: comparator.%s{Eq: new(%s)}}", base, embedded, inner, valueExpr)
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

// nestedParentKeyExpr converts the parent's primary key into the element type
// the FK's comparator takes — a string on the comparator.ID family, the key's
// own Go type on comparator.Number and comparator.Opaque (PRD §11.2).
//
// fkGoType is the FK's *value* type as the relationship builder resolved it,
// which is the parent's key type by construction; it is what decides the
// family, exactly as it does for the read loader's `IN` term.
func nestedParentKeyExpr(fkGoType string, parentPK ColumnContext) string {
	if funcPKIsStringType(strings.TrimPrefix(fkGoType, "*")) {
		return funcFKToString(parentPK, "parent."+parentPK.FieldName)
	}
	return "parent." + parentPK.FieldName
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
