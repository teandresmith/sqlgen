package gen

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// This file models the GraphQL type namespace the generated schema claims, the
// way resolved_names.go models the Go package scope (PRD §26.4 "GraphQL type
// name ownership").
//
// The two are separate rules because the namespaces are separate, and a
// document valid in one can be invalid in the other: a SQL enum named
// `duration` produces a perfectly legal Go `Duration` beside the `Duration`
// scalar an interval column declares, and gqlparser then rejects the schema
// with `Cannot redeclare type Duration.` — an error naming neither the enum nor
// the column. Nothing in the Go rules sees it, because Go has no `Duration`
// scalar to collide with.
//
// GraphQL has ONE flat type namespace — scalars, enums, object types and input
// types all share it — so the check runs over the whole APIContext at once
// rather than per source. That is also why it cannot live at the point each
// name is derived: enumComparatorInput cannot see the scalars, and
// collectComparatorFamilies dedupes an enum-derived family INTO the shipped one
// it collides with, so by the time the family list is built the duplicate is
// already gone.

// graphQLSpecScalars are the scalar names the GraphQL specification reserves.
// sqlgen declares none of them, but an entity resolving onto one still cannot
// be emitted — a `enum String { … }` block redeclares a built-in — so they
// belong to the owned set all the same.
var graphQLSpecScalars = []string{"Boolean", "Float", "ID", "Int", "String"}

// structuralGraphQLTypes are the type names shared_gen.graphqls declares on
// sqlgen's own behalf for every generated project, whatever the schema holds.
var structuralGraphQLTypes = []string{"Mutation", "PageInfo", "Query", "SortDirection"}

// fixedOwnedGraphQLName reports whether name is one sqlgen owns regardless of
// what the consumer's schema contains: a spec built-in, one of the four
// structural types, or any member of the fixed comparator surface — every
// shipped family, its `Nullable` twin (comparatorFamilyByName derives both),
// and its range input.
//
// enumComparatorInput keys its disambiguation on exactly this set and
// deliberately not on the rest of the schema: an emitted input name is part of
// the published API contract, so a name that shifted because an unrelated table
// was added would break persisted client queries (PRD §26.4 "Monomorphized
// names disambiguate against the fixed families").
func fixedOwnedGraphQLName(name string) bool {
	if _, ok := comparatorFamilyByName(name); ok {
		return true
	}
	if slices.Contains(graphQLSpecScalars, name) || slices.Contains(structuralGraphQLTypes, name) {
		return true
	}
	return slices.ContainsFunc(shippedComparatorFamilies, func(f APIComparatorFamily) bool {
		return f.RangeInputName != "" && f.RangeInputName == name
	})
}

// specScalarSliceComparatorName reports whether name is one of the
// `<Elem>SliceComparator` inputs sqlgen derives from a GraphQL SPEC scalar —
// `StringSliceComparator`, `IntSliceComparator`, `FloatSliceComparator`,
// `BooleanSliceComparator`, `IDSliceComparator`, and each one's `Nullable`
// twin.
//
// These are sqlgen's own names in the sense the owned-set table means it (PRD
// §26.4 "GraphQL type name ownership": "each emitted `<X>Comparator` /
// `Nullable<X>Comparator`"), because nothing in the consumer's schema
// contributes a character of them — so an entity landing on one is reported
// against shared_gen.graphqls.
//
// It is deliberately NOT part of fixedOwnedGraphQLName. That predicate is the
// key enumComparatorInput disambiguates against, and PRD §26.4 scopes that key
// to the fixed families, their twins, the range inputs and the four structural
// names — precisely so an emitted name stays stable against the rest of the
// schema. An enum deriving `IntSliceComparator` therefore keeps its spelling
// and is reported as a collision, which is the outcome §26.4 prescribes for two
// claims on one name.
//
// A slice comparator over a schema ENUM is the opposite case — its name comes
// from the consumer's own enum, so it is claimed by that enum in
// collectGraphQLTypeNames and must not be reserved here, or every enum-array
// column would be reported against itself.
func specScalarSliceComparatorName(name string) bool {
	base := strings.TrimPrefix(name, "Nullable")
	elem, ok := strings.CutSuffix(base, "SliceComparator")
	if !ok {
		return false
	}
	return slices.Contains(graphQLSpecScalars, elem)
}

// reservedGraphQLTypes maps every name the emitted schema declares on sqlgen's
// own behalf to the artifact declaring it, so an entity landing on one is
// reported against its owner rather than against another entity.
//
// The comparator surface is read off the built context rather than off
// shippedComparatorFamilies because a family is declared only when a column
// references it (PRD §26.4 Rule 3) — reserving the whole static table would
// reject a `time_comparator` table on a schema that emits no TimeComparator at
// all. The spec built-ins and the structural four are unconditional: the former
// are never sqlgen's to emit, and the latter appear in every generated schema.
//
// A MONOMORPHIZED family in that list is skipped: `OrderStatusComparator` is
// the OrderStatus enum's own claim (collectGraphQLTypeNames), not a name sqlgen
// fixed, and reserving it would report every enum against itself.
// fixedOwnedGraphQLName separates the two exactly — it is true for a shipped
// family and its twin, false for anything derived from the schema.
// specScalarSliceComparatorName extends the reservation (not the
// disambiguation key) to the `<Elem>SliceComparator` inputs whose element is a
// spec scalar, which are sqlgen's names by the same test.
func reservedGraphQLTypes(ctx *APIContext) map[string]string {
	reserved := make(map[string]string, len(ctx.ComparatorFamilies)*2+len(graphQLSpecScalars)+len(structuralGraphQLTypes))
	for _, name := range graphQLSpecScalars {
		reserved[name] = "GraphQL schema (a spec built-in scalar)"
	}
	for _, name := range structuralGraphQLTypes {
		reserved[name] = "shared_gen.graphqls"
	}
	for _, fam := range ctx.ComparatorFamilies {
		if !fixedOwnedGraphQLName(fam.Name) && !specScalarSliceComparatorName(fam.Name) {
			continue
		}
		reserved[fam.Name] = "shared_gen.graphqls"
		if fam.RangeInputName != "" {
			reserved[fam.RangeInputName] = "shared_gen.graphqls"
		}
	}
	return reserved
}

// enumRef, setRef and scalarRef build the reference a GraphQL name-collision
// error names a claimant by. An enum is renameable through the `enums:` block
// (PRD §4.11); a MySQL SET is not — BuildSetContexts names it from the schema
// with no override to read, so offering one would send the consumer to a key
// that changes nothing. A scalar is renameable only when the consumer declared
// it, since a built-in registry entry's name is sqlgen's.
//
// This mirrors collectResolvedNames, which already separates the two on the Go
// side ("SET type", no escape).
func enumRef(schema, name string) entityRef {
	qualified := qualifiedOrBare(schema, name)
	return entityRef{kind: "enum", qualified: qualified, escape: "enums." + qualified + ".struct_name"}
}

func setRef(schema, name string) entityRef {
	return entityRef{kind: "SET type", qualified: qualifiedOrBare(schema, name)}
}

func scalarRef(name string, consumerDeclared bool) entityRef {
	ref := entityRef{kind: "scalar", qualified: name}
	if consumerDeclared {
		ref.escape = "api.graphql.scalars." + name
	}
	return ref
}

// collectGraphQLTypeNames reads every GraphQL type name the emitted schema
// derives from a schema entity or a consumer declaration.
//
// A table claims all nine of its names unconditionally, including the two
// mutation inputs that the empty-input rule and the `api.operations` mask can
// suppress. This is the conditioning boundary resolved_names.go draws for
// reserved field names, for the same reason: enabling an operation later must
// never turn a valid config invalid.
//
// A view claims seven — the same list minus `Create<V>Input` / `Update<V>Input`,
// which it can never emit (PRD §26.4 "Views on the GraphQL surface"). The
// conditioning boundary does NOT extend to those two here, unlike the table
// case: a view acquiring a create input later is not a config change, it is a
// spec change, so reserving the names would only turn valid schemas invalid for
// a surface that does not exist. Its claim is made through viewRef so the error
// offers `views.<name>.struct_name` rather than the `tables.` key that cannot
// rename it.
//
// Without this arm, a view colliding with a scalar, a table, or a comparator
// input — a view named `duration` beside an `interval` column gives `type
// Duration` alongside `scalar Duration` — reaches gqlparser as `Cannot
// redeclare type Duration.`, naming neither claimant. That is the exact failure
// mode this check exists to replace, so it covers views as well as tables.
func collectGraphQLTypeNames(ctx *APIContext, consumerScalars map[string]config.ScalarBinding) []claimedName {
	claims := make([]claimedName, 0, len(ctx.Tables)*9+len(ctx.UsedEnums)+len(ctx.UsedScalars))

	for _, t := range ctx.Tables {
		ref := tableRef(t.Schema, t.SQLTable)
		names := []string{
			t.StructName,
			t.StructName + "Connection",
			t.StructName + "Edge",
			t.StructName + "ListResult",
			t.StructName + "Filter",
			t.StructName + "SortField",
			t.StructName + "Sort",
		}
		if t.IsView {
			ref = viewRef(t.Schema, t.SQLTable)
		} else {
			names = append(names, "Create"+t.StructName+"Input", "Update"+t.StructName+"Input")
		}
		for _, name := range names {
			claims = append(claims, claimedName{name: name, by: ref})
		}
		// The nested-mutation types (PRD §26.5.1) are claimed as emitted,
		// not on the conditioning boundary the nine names above draw. That
		// boundary is already held for them where it can bite: every one of
		// these names is also a Go type the models package declares, and
		// resolved_names.go claims those structurally, feature flags and
		// masks notwithstanding. What only this namespace can see is a
		// GraphQL-only claimant — a consumer scalar — landing on one.
		for _, n := range t.Nested.emittedTypeNames() {
			by := nestedSurfaceRef(t.Schema, t.SQLTable)
			if n.edge != "" {
				by = relationshipRef(t.Schema, t.SQLTable, n.edge)
			}
			claims = append(claims, claimedName{name: n.name, by: by})
		}
	}
	for _, e := range ctx.UsedEnums {
		// A SET projects an enum type into the schema and nothing else: its
		// columns filter through comparator.String (makeEnumTranslator rejects
		// them — the type parameter is not a schema enum), so it claims no
		// comparator input and has no `enums:` override to be pointed at.
		if e.FromSet {
			claims = append(claims, claimedName{name: e.GraphQLName, by: setRef(e.SQLSchema, e.SQLName)})
			continue
		}

		// Both forms of the monomorphized comparator, alongside the enum type
		// itself. They are claimed here rather than left to the family list
		// because collectComparatorFamilies keys on the input NAME: two enums
		// deriving one name — `duration` disambiguated to
		// `DurationEnumComparator` beside `duration_enum` deriving it directly
		// — merge into a single family carrying one enum's operands, and the
		// duplicate is gone before anything downstream can see it.
		//
		// The `Nullable` twin is claimed whether or not a nullable column
		// references it, the same conditioning boundary the table claims draw:
		// Rule 2 makes it part of the enum's published surface, and adding a
		// nullable column later must not turn a valid schema invalid.
		ref := enumRef(e.SQLSchema, e.SQLName)
		base := enumComparatorInput(e.GraphQLName)
		// The slice pair alongside them: a PostgreSQL array of this enum
		// projects onto `<E>SliceComparator` (PRD §26.4 Rule 1), which is
		// derived from the enum's own name and therefore the enum's claim.
		// Claimed on the same conditioning boundary as the Nullable twin —
		// whether or not an array column exists today — so adding one later
		// cannot turn a valid schema invalid.
		sliceBase := sliceComparatorInput(e.GraphQLName)
		for _, name := range []string{
			e.GraphQLName,
			base, nullableComparatorInput(base),
			sliceBase, nullableComparatorInput(sliceBase),
		} {
			claims = append(claims, claimedName{name: name, by: ref})
		}
	}
	for _, s := range ctx.UsedScalars {
		_, consumerDeclared := consumerScalars[s.Name]
		claims = append(claims, claimedName{name: s.Name, by: scalarRef(s.Name, consumerDeclared)})
	}

	return claims
}

// validateGraphQLTypeNames rejects an APIContext in which two claimants land on
// one GraphQL type name, or one claimant lands on a name sqlgen already
// declares (PRD §26.4 "GraphQL type name ownership", §4.13).
//
// It is an error rather than a silent rename because sqlgen owns only one of
// the two spellings in the general case: it can disambiguate a comparator input
// it derives itself (enumComparatorInput), but it cannot rename the enum or
// table the consumer's schema declares, so no rename makes every colliding
// document valid. Every violation is reported, not just the first.
func validateGraphQLTypeNames(ctx *APIContext, consumerScalars map[string]config.ScalarBinding) error {
	claims := collectGraphQLTypeNames(ctx, consumerScalars)

	dups := newClaimReport()
	dups.collect("GraphQL type name", claims, duplicateClaims)

	reserved := newClaimReport()
	reserved.collect("GraphQL type name", claims, reservedClaims(reservedGraphQLTypes(ctx)))

	return errors.Join(slices.Concat(
		graphQLDuplicateErrors(dups),
		graphQLReservedErrors(reserved),
	)...)
}

// graphQLDuplicateErrors and graphQLReservedErrors render the two violation
// shapes. They mirror claimReport's Go-side renderers but say "the generated
// GraphQL schema" instead of naming a _gen.go file, and close with the note
// that the failure would otherwise surface from gqlgen naming neither side.
func graphQLDuplicateErrors(r *claimReport) []error {
	errs := make([]error, 0, len(r.order))
	for _, key := range r.order {
		v := r.byKey[key]
		errs = append(errs, fmt.Errorf(
			"api: %s and %s both resolve to %s — %s (a GraphQL document cannot declare one name twice; PRD §26.4)",
			v.a, v.b, joinClaims(v.claims), pairHint(v.a, v.b),
		))
	}
	return errs
}

func graphQLReservedErrors(r *claimReport) []error {
	errs := make([]error, 0, len(r.order))
	for _, key := range r.order {
		v := r.byKey[key]
		errs = append(errs, fmt.Errorf(
			"api: %s resolves to %s, which the generated %s already declares — %s (PRD §26.4)",
			v.a, joinClaims(v.claims), v.owner, v.a.hint(),
		))
	}
	return errs
}
