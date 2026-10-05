package gen

import (
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/parser"
)

// This file spells the package-scope type names a nested-mutation surface
// declares (PRD §9.9.5) and enumerates the (parent, edge) pairs that can
// declare them. It is the single source of both: the name registry claims the
// names here (resolved_names.go) and the emitters render them.
//
// The names are keyed on the **(parent, edge)** pair, not on a table, which is
// what puts them outside the registry's primary-name scope: a `user_events`
// table and the `User.Events` edge both resolve to `CreateUserEventInput`, and
// the registry's own scope comment pushes that cross-shape class out as "a
// `go build` error". Claiming them needs a rule that reads both shapes at
// once, which is the seeded pass validateResolvedNames runs over
// resolvedNames.nested.

// nestedTypeName is one package-scope type name a nested-mutation surface
// declares, and the relationship field name that spells it. Edge is empty for
// the three per-parent wrapper inputs, which are keyed on the parent alone.
type nestedTypeName struct {
	name string
	edge string
}

// nestedCandidateEdges returns the relationships of tc whose (parent, edge)
// pair can declare a nested-mutation surface, ordered by relationship name the
// way the generated members are (PRD §9.9.5).
//
// The predicate is **structural only** — the parent's primary key flows down
// the edge (PRD §9.9.1 shapes 2, 3 and 4) and the parent has a single-column
// primary key. The rules that narrow eligibility further all read config: the
// global `nested_mutations.enabled` opt-in, a `filter:` on the edge, the §32
// access projection, the junction's shape. None of them is consulted here, for
// the reason PRD §8.5 gives for every other reserved name: conditioning stops
// at structural facts so that enabling a feature later never turns a valid
// config invalid. The emitters ask the narrower question with their own
// eligibility pass; this one deliberately claims the wider set.
//
// The over-claim has two causes and only the first is that argument. The mask,
// the opt-in and a reducible `filter:` are all config the consumer can change.
// An *unreducible* `filter:` is different: PRD §13.7 makes such an edge
// permanently non-nestable, so its names can never be declared under any
// config. They are claimed anyway, because the alternative is a reservation
// rule that parses the raw SQL in `filter:` — which sqlgen deliberately never
// does — or one that answers differently for two configurations of one schema.
//
// A belongs-to O2O edge — the FK on the parent, PRD §9.9.1 shape 1 — is
// excluded: nothing nests under it, so it names nothing.
//
// FKOnTarget is read only inside the O2O arm, never as a general "belongs-to"
// test, because false is three answers in one. M2M is separated by the switch
// before the flag is consulted. The third — the target is absent from the
// parsed schema, where fkColumnOnTarget also returns false — is deliberately
// left to under-claim: with the target unresolved there is no way to tell
// has-one from belongs-to, so whether the parent's key flows down the edge is
// unknown, and claiming a name for an edge that may be shape 1 would reject a
// config over a type that is never declared. An O2M to an unresolved target
// *is* claimed, and the asymmetry is the point rather than an oversight: O2M
// fixes the direction by its type, so nothing is unknown there. The emitters
// need the same distinction for a different purpose and must make it the same
// way — do not read FKOnTarget == false as belongs-to.
func nestedCandidateEdges(tc TableContext) []RelationshipContext {
	if tc.CompositePK || len(tc.PKColumns) != 1 {
		return nil
	}

	candidates := make([]RelationshipContext, 0, len(tc.Relationships))
	for _, rel := range tc.Relationships {
		switch rel.Type {
		case parser.OneToMany, parser.ManyToMany:
			candidates = append(candidates, rel)
		case parser.OneToOne:
			// has-one only: the target holds the FK, so the parent's PK flows
			// down into it. A belongs-to edge holds the FK here and is shape 1.
			if rel.FKOnTarget {
				candidates = append(candidates, rel)
			}
		}
	}

	// Ordered by the relationship's resolved Go field name, which is the
	// spelling PRD §9.9.8 attributes errors with and §26.5.5 puts in
	// `extensions.path`. The declared `Name` is the wrong key: an
	// auto-detected edge carries the pluralized snake form (`documents`) where
	// a config-declared one carries what the consumer wrote (`Attachments`),
	// so ASCII-ordering the two spellings groups by origin rather than
	// alphabetically.
	slices.SortFunc(candidates, func(a, b RelationshipContext) int {
		return strings.Compare(a.FieldName, b.FieldName)
	})
	return candidates
}

// nestedSurfaceNames returns every package-scope type name tc's nested-mutation
// surface declares, in the order PRD §9.9.5 lists the families. A parent with
// no candidate edge declares nothing at all — a wrapper carrying only the flat
// input would be the base operation with extra steps.
func nestedSurfaceNames(tc TableContext) []nestedTypeName {
	edges := nestedCandidateEdges(tc)
	if len(edges) == 0 {
		return nil
	}

	names := make([]nestedTypeName, 0, 3+3*len(edges))
	for _, verb := range []string{"Create", "Update", "Upsert"} {
		names = append(names, nestedTypeName{name: verb + tc.StructName + "WithRelatedInput"})
	}
	for _, rel := range edges {
		names = append(names,
			nestedTypeName{name: nestedBlockName(tc.StructName, rel.FieldName, "Create"), edge: rel.FieldName},
			nestedTypeName{name: nestedBlockName(tc.StructName, rel.FieldName, "Update"), edge: rel.FieldName},
		)
		// M2M takes the target's ordinary Create<Target>Input unchanged —
		// there is no traversed FK on the target to elide, because the link
		// lives in the junction row — so only shapes 2 and 3 declare a nested
		// child input (PRD §9.9.5).
		if rel.Type != parser.ManyToMany {
			names = append(names, nestedTypeName{
				name: nestedChildInputName(tc.StructName, rel.FieldName),
				edge: rel.FieldName,
			})
		}
	}
	return names
}

// nestedBlockName spells `<Parent><Edge>CreateNested` / `<Parent><Edge>UpdateNested`,
// the per-edge verb block.
func nestedBlockName(parentStruct, edgeField, family string) string {
	return parentStruct + edgeField + family + "Nested"
}

// nestedChildInputName spells `<Parent><Edge>CreateInput`, the child's create
// input minus the traversed FK and the edge's discriminator column.
//
// The family marker is a suffix, not a prefix, and that is load-bearing rather
// than cosmetic (PRD §9.9.5). The obvious spelling — `Create<Parent><Edge>Input`
// — reconstructs the *target's* own create input exactly whenever the child
// table is named `<parent>_<edge>`, the conventional name for a dependent
// table: `users.Profile` over `user_profiles` and `binary_keys.Events` over
// `binary_key_events` both land on a name the target table already declares,
// and the two types differ (the nested one has the traversed FK removed). Both
// are live in this repo's own example schemas. With the marker as a suffix the
// nested names share a namespace with nothing a table derives.
func nestedChildInputName(parentStruct, edgeField string) string {
	return parentStruct + edgeField + "CreateInput"
}

// wireNestedMutationSurface is the cross-table pass that resolves what each
// table's entity client needs before a nested-mutation executor can be written
// against it: the clients its executors write through that the relationship
// loaders do not already declare — each M2M edge's junction and each has-one
// edge's target — and whether it carries a nested surface at all.
//
// It runs after wireRelationshipFKMetadata and for the same reason — a
// junction's or target's Go struct name is a property of that table's own
// context, not of the edge that names it — and it resolves only against tables
// that are actually generated. A table excluded from the generated set yields
// no field, because there is no client to point at.
func wireNestedMutationSurface(contexts []TableContext) {
	junctions := newTableIndex(contexts)

	for i := range contexts {
		tc := &contexts[i]
		edges := nestedCandidateEdges(*tc)
		tc.HasNestedSurface = len(edges) > 0

		// Seeded with the target clients so a junction or has-one target that
		// is also an O2M / M2M target of this table emits one field, not two.
		// The entity client's field list and the unified client's wire list
		// are both built from NestedWriteClients, so they cannot disagree.
		seen := make(map[string]bool, len(tc.RelationshipTargetClients))
		for _, name := range tc.RelationshipTargetClients {
			seen[name] = true
		}
		for _, rel := range edges {
			var client *TableContext
			var ok bool
			switch rel.Type {
			case parser.ManyToMany:
				// The link rows go through the junction's client.
				client, ok = junctions.lookup(rel.JunctionSchema, rel.JunctionTable)
			case parser.OneToOne:
				// A has-one target is not an O2M or M2M target, so the loaders
				// never declare its client — but the nested `create` writes
				// through it. The graphql example hides this because its one
				// has-one target is also an O2M target there.
				client, ok = junctions.lookup(rel.TargetSchema, rel.TargetTable)
			default:
				continue
			}
			if !ok || seen[client.StructName] {
				continue
			}
			seen[client.StructName] = true
			tc.NestedWriteClients = append(tc.NestedWriteClients, client.StructName)
		}
		slices.Sort(tc.NestedWriteClients)
	}
}

// tableIndex resolves the table an edge names — a relationship target or an
// M2M junction — to its generated context, by the schema the edge carries.
//
// Every table is indexed under its schema-qualified name, and only that: a
// bare key lets two same-named tables in different schemas collide, with the
// last one silently winning. The lookup needs no bare fallback because every
// edge carries its target's and its junction's schema whenever the dialect has
// one. The parser qualifies an FK-inferred edge, and configRelationshipToContext
// resolves a declared `table:` and `junction:` by PRD
// §5.5's rule, so a schema-qualified miss is a table outside the generated set.
// The bare name is the key only in a dialect without schemas.
type tableIndex map[string]*TableContext

func newTableIndex(contexts []TableContext) tableIndex {
	idx := make(tableIndex, len(contexts))
	for i := range contexts {
		tc := &contexts[i]
		idx[qualifiedOrBare(tc.Schema, tc.TableName)] = tc
	}
	return idx
}

// lookup resolves the table an edge names by (schema, name) exactly.
func (idx tableIndex) lookup(schema, name string) (*TableContext, bool) {
	tc, ok := idx[qualifiedOrBare(schema, name)]
	return tc, ok
}
