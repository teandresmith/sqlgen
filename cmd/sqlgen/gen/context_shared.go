package gen

import (
	"slices"
	"strings"
)

// BuildSharedTypesContext builds the SharedTypesContext containing all shared
// generic types and helper functions that are generated once per package.
// These types are package-level, not table-specific.
//
// When tenancyEnabled is true, the CallOptions struct gains a SkipTenancy
// field (PRD §29.4.4) and — when the package has at least one tenanted table,
// so a uniform tenant Go type exists (§29.2.4) — a concretely-typed
// `Tenant *<tenant type>` field for the explicit-tenant resolution mode
// (§29.4.4). Projects without tenancy regenerate
// byte-identically.
//
// tenantGoType/tenantImport carry the package's uniform tenant type (empty
// when tenancy is disabled or no table is tenanted).
//
// hasFilterOptions carries the package-level relationship-filter decision: when
// at least one table contributes a relationship filter member (PRD §11.1), the
// FilterOption plumbing every generated ToConditions threads is emitted here.
func BuildSharedTypesContext(pkg string, tenancyEnabled bool, tenantGoType, tenantImport string, hasFilterOptions, hasUpsertMany, hasNested bool) SharedTypesContext {
	hasExplicitTenant := tenancyEnabled && tenantGoType != ""
	imports := []string{"slices", "github.com/teandresmith/sqlgen/sql"}
	if hasExplicitTenant && tenantImport != "" {
		imports = UniqueImports(append(imports, tenantImport))
	}
	decls := filterOptionDecls(hasFilterOptions, tenantGoType)
	if len(decls) > 0 {
		imports = UniqueImports(append(imports, "strconv"))
	}
	if hasUpsertMany {
		// Only the UpsertMany dedupe key needs these (PRD §9.5): the driver's
		// own parameter conversion to normalize a value to what it binds as,
		// and fmt / time to render that normalized form.
		imports = UniqueImports(append(imports, "database/sql/driver", "fmt", "time"))
	}
	return SharedTypesContext{
		Package: pkg,
		Imports: imports,
		Types:   sharedTypeDefinitions(tenancyEnabled, tenantGoType),
		Helpers: sharedHelperDefinitions(hasExplicitTenant, hasUpsertMany, nestedHelperShape{emit: hasNested, tenancyEnabled: tenancyEnabled, explicitTenant: hasExplicitTenant}),
		Decls:   decls,
	}
}

// filterOptionDecls returns the FilterOption plumbing a package needs once its
// filters carry relationship members (PRD §11.1). Two things travel down the
// recursion, and both have to reach an arbitrarily nested member:
//
//   - The subquery depth, which names the target alias. A self-referential
//     relationship filter nested inside itself would otherwise alias both
//     levels the same, and the inner alias would shadow the outer one — the
//     correlation would silently compare the related row to itself (§13.5).
//   - The resolved tenant, when the package is tenanted. ToConditions takes no
//     ctx and holds no client, so the value has to be handed in by the caller
//     that resolved it. An unscoped conversion produces subqueries equivalent
//     to CallOptions.SkipTenancy, which is the behaviour SkipTenancy itself
//     needs.
//
// Returns nil when no relationship filter member exists in the package, leaving
// shared_types_gen.go byte-identical to its form without relationship
// filters.
func filterOptionDecls(hasFilterOptions bool, tenantGoType string) []string {
	if !hasFilterOptions {
		return nil
	}

	scopeFields := "\tdepth int"
	if tenantGoType != "" {
		scopeFields += "\n\ttenant      " + tenantGoType + "\n\tapplyTenant bool"
	}

	decls := []string{
		`// FilterOption carries the per-call data a filter needs, beyond the dialect,
// to compile its relationship members into correlated EXISTS subqueries.
// Generated client methods supply the set; ToConditions threads it through
// And/Or composition and down into each relationship member.
type FilterOption func(*filterScope)`,

		`// filterScope is a resolved FilterOption set.
type filterScope struct {
` + scopeFields + `
}`,

		`// resolveFilterScope applies option functions and returns the resolved scope.
func resolveFilterScope(opts []FilterOption) filterScope {
	var scope filterScope
	for _, fn := range opts {
		fn(&scope)
	}
	return scope
}`,

		`// nestFilterScope returns opts with the subquery depth set to depth, for the
// next level of relationship nesting. The set is copied rather than appended in
// place so two relationship members compiled from the same filter cannot write
// through each other's backing array.
func nestFilterScope(opts []FilterOption, depth int) []FilterOption {
	nested := make([]FilterOption, len(opts), len(opts)+1)
	copy(nested, opts)
	return append(nested, func(s *filterScope) {
		s.depth = depth
	})
}`,

		`// filterSubqueryAlias returns the table alias a relationship filter's subquery
// uses at the given nesting depth. Distinct per level, so an inner subquery
// never shadows the alias its own correlation reference resolves against.
func filterSubqueryAlias(prefix string, depth int) string {
	return prefix + strconv.Itoa(depth)
}`,
	}

	if tenantGoType != "" {
		decls = append(decls, `// WithFilterTenant scopes the relationship subqueries a filter compiles to
// tenant, so a relationship filter reads the related table under the same
// tenant predicate the table's own read path applies. Generated read and filter-accepting mutation methods pass it
// automatically from the resolved tenant; converting a filter without it
// produces subqueries scoped as if CallOptions.SkipTenancy were set.
func WithFilterTenant(tenant `+tenantGoType+`) FilterOption {
	return func(s *filterScope) {
		s.tenant = tenant
		s.applyTenant = true
	}
}`)
	}

	return decls
}

// sharedTypeDefinitions returns the shared generic input type definitions
// matching PRD Sections 9.4 and 9.6. PaginateResult is generated by
// pagination.go.tmpl; Connection types by connection.go.tmpl.
//
// When tenancyEnabled is true, CallOptions gains a SkipTenancy field (PRD
// §29.4.4) and — when tenantGoType is non-empty — an explicit-tenant
// `Tenant *<tenant type>` field (§29.4.4 third resolution mode:
// concretely typed because the tenant type is uniform per package, so no
// generic churn and no runtime type-assert; set via `o.Tenant = new(v)`).
// The fields sit between SkipHooks and FieldOptions — grouped with the other
// tenancy toggles. Both are omitted entirely when tenancy is disabled so
// non-tenancy projects regenerate byte-identically.
func sharedTypeDefinitions(tenancyEnabled bool, tenantGoType string) []SharedTypeDefinition {
	callOptionsFields := []SharedFieldDefinition{
		{Name: "SkipCache", GoType: "bool", JSONTag: "skip_cache", Doc: "bypass cache read-through and write-through"},
		{Name: "SkipEvents", GoType: "bool", JSONTag: "skip_events", Doc: "suppress event publishing for this mutation"},
		{Name: "SkipHooks", GoType: "bool", JSONTag: "skip_hooks", Doc: "bypass all hooks except panic recovery"},
	}
	if tenancyEnabled {
		callOptionsFields = append(callOptionsFields, SharedFieldDefinition{
			Name: "SkipTenancy", GoType: "bool", JSONTag: "skip_tenancy",
			Doc: "bypass tenant auto-filter and mutation mismatch check; orthogonal to SkipHooks",
		})
		if tenantGoType != "" {
			callOptionsFields = append(callOptionsFields, SharedFieldDefinition{
				Name: "Tenant", GoType: "*" + tenantGoType, JSONTag: "tenant",
				Doc: "explicit tenant: resolve tenancy to this value (filter + auto-set) instead of the ctx resolver; wins over SkipTenancy; nil = unset",
			})
		}
	}
	callOptionsFields = append(callOptionsFields, SharedFieldDefinition{
		Name: "FieldOptions", GoType: "*FO", JSONTag: "field_options",
		Doc: "column selection and relationship loading",
	})
	callOptionsFields = append(callOptionsFields, SharedFieldDefinition{
		Name: "LockMode", GoType: "sql.LockMode", JSONTag: "lock_mode",
		Doc: "row-level lock clause for read methods",
	})
	// Emitted unconditionally, beside LockMode: the two read-side knobs sit
	// together. Stream is generated on every table (PRD §4.6), so gating this
	// one on the package emitting a Stream would key CallOptions' shape on
	// whether the package has a table at all — schema shape, not a mode the
	// consumer chose (PRD §9.4a).
	callOptionsFields = append(callOptionsFields, SharedFieldDefinition{
		Name: "AllowInTransaction", GoType: "bool", JSONTag: "allow_in_transaction",
		Doc: "permit Stream inside a transaction; commits the caller to issuing nothing else on that txCtx",
	})

	return []SharedTypeDefinition{
		{
			Name:       "CallOptions",
			TypeParams: "[FO any]",
			Doc:        "CallOptions controls per-call behavior overrides.",
			Fields:     callOptionsFields,
		},
		{
			Name:       "IncrementInput",
			TypeParams: "[C ~string]",
			Doc:        "IncrementInput holds parameters for atomic column increment/decrement.",
			Fields: []SharedFieldDefinition{
				{Name: "Column", GoType: "C", JSONTag: "column"},
				{Name: "Amount", GoType: "int", JSONTag: "amount"},
			},
		},
	}
}

// sharedHelperDefinitions returns the shared helper function definitions
// matching PRD Section 9.8 exactly. hasExplicitTenant adds the §29.4.4
// explicit-tenant precedence normalization to resolveCallOptions;
// hasUpsertMany adds the two helpers the batched upsert dedupe needs (§9.5),
// gated so a package that generates no UpsertMany keeps its previous output
// and does not carry a helper nothing calls.
func sharedHelperDefinitions(hasExplicitTenant, hasUpsertMany bool, nested nestedHelperShape) []SharedHelperDefinition {
	helpers := []SharedHelperDefinition{
		{
			Name:      "excludeColumns",
			Signature: "(columns, exclude []string) []string",
			Doc:       "excludeColumns returns columns with any entries in exclude removed.",
			Body: `	excludeSet := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		excludeSet[e] = true
	}
	var result []string
	for _, c := range columns {
		if !excludeSet[c] {
			result = append(result, c)
		}
	}
	return result`,
		},
		{
			Name:      "resolveCallOptions",
			Signature: "[FO any](opts []func(*CallOptions[FO])) CallOptions[FO]",
			Doc:       "resolveCallOptions applies option functions and returns the resolved struct. SkipHooks implies SkipCache and SkipEvents because cache and events are implemented as hooks.",
			Body:      resolveCallOptionsBody(hasExplicitTenant),
		},
		{
			Name:      "toAnySlice",
			Signature: "[T any](s []T) []any",
			Doc:       "toAnySlice converts a typed slice to []any for variadic query args.",
			Body: `	result := make([]any, len(s))
	for i, v := range s {
		result[i] = v
	}
	return result`,
		},
		{
			// Behavior coverage for the emitted helper lives in
			// cmd/sqlgen/testdata/examples/postgres/models/union_columns_test.go.
			// The body is a source string, so nothing in this module
			// runs it — edit that test alongside any change here.
			Name:      "unionColumns",
			Signature: "(columns []string, required ...string) []string",
			Doc: `unionColumns returns columns plus any required column it does not already
// carry, re-sorted. A narrowed projection reaches this whenever the call needs
// a column the caller did not select — a cursor key, a relationship mapping
// key — and the result is re-sorted because the SQL builder emits columns
// sorted and the scan functions match positionally against this slice.
// Returns columns unchanged when nothing is missing.`,
			Body: `	var missing []string
	for _, r := range required {
		if !slices.Contains(columns, r) && !slices.Contains(missing, r) {
			missing = append(missing, r)
		}
	}
	if len(missing) == 0 {
		return columns
	}
	result := make([]string, 0, len(columns)+len(missing))
	result = append(result, columns...)
	result = append(result, missing...)
	slices.Sort(result)
	return result`,
		},
	}
	if hasUpsertMany {
		helpers = append(helpers, upsertDedupeHelpers()...)
	}
	helpers = append(helpers, nestedMutationHelpers(nested)...)
	// Emitted in name order, which the base list already happened to be in and
	// which the gated groups have to join rather than trail: a helper appended
	// at the end would move on the day a later group is added in front of it.
	slices.SortFunc(helpers, func(a, b SharedHelperDefinition) int {
		return strings.Compare(a.Name, b.Name)
	})
	return helpers
}

// nestedHelperShape carries the two facts the nested-mutation helpers' bodies
// depend on: whether the package emits any nested surface at all, and which
// CallOptions fields exist to copy across a table boundary.
type nestedHelperShape struct {
	emit           bool
	tenancyEnabled bool
	explicitTenant bool
}

// nestedMutationHelpers returns the two package-level helpers every nested
// executor calls (PRD §9.9). Gated so a package that emits no nested surface
// keeps its previous output and carries no helper nothing calls.
//
// nestedChildOptions is generic in BOTH type parameters. CallOptions[FO] is
// generic over the field-options type, so `*o = options` cannot be used across
// tables — and the PARENT's field-options type varies per parent too, so
// pinning the second argument to one parent's CallOptions would make the
// helper compile for that one client alone.
//
// FieldOptions, LockMode and AllowInTransaction deliberately do not propagate:
// the inner calls select nothing (the skip-refetch escape, PRD §9.6), take no
// row lock, and issue no Stream (PRD §9.4a).
func nestedMutationHelpers(shape nestedHelperShape) []SharedHelperDefinition {
	if !shape.emit {
		return nil
	}
	body := "\to.SkipCache, o.SkipEvents = parent.SkipCache, parent.SkipEvents\n\to.SkipHooks = parent.SkipHooks"
	if shape.tenancyEnabled {
		body += "\n\to.SkipTenancy = parent.SkipTenancy"
	}
	if shape.explicitTenant {
		body += "\n\to.Tenant = parent.Tenant"
	}
	return []SharedHelperDefinition{
		{
			Name:      "nestedChildOptions",
			Signature: "[FO, PFO any](o *CallOptions[FO], parent CallOptions[PFO])",
			Doc: `nestedChildOptions copies the option fields that cross a table boundary
// on a nested mutation. FieldOptions, LockMode and AllowInTransaction
// deliberately do not propagate: the inner calls select nothing, take no row
// lock, and issue no Stream.`,
			Body: body,
		},
		{
			Name:      "nestedError",
			Signature: "(edge, verb string, id any, err error) error",
			Doc: `nestedError attributes a nested-mutation failure to its edge and verb.
// The wrapped error is untouched, so errors.Is against the
// sentinels and the *ConstraintError dispatch keep working through it; id is
// nil when the failure is not id-scoped.`,
			Body: "\treturn &NestedMutationError{Edge: edge, Verb: verb, ID: id, Err: err}",
		},
	}
}

// upsertDedupeHelpers returns the two helpers UpsertMany uses to collapse
// inputs that resolve to the same conflict target before the statement is
// built (PRD §9.2, §9.5).
//
// They are split from the generated method because both are pure functions of
// a value row and a column list: the conflict target is a runtime argument, so
// neither the column positions nor the key can be computed at generation time.
//
// Behavior coverage for the emitted bodies lives in
// cmd/sqlgen/testdata/examples/postgres/models/upsert_dedupe_test.go — the
// bodies are source strings, so nothing in this module runs them. Edit that
// test alongside any change here.
func upsertDedupeHelpers() []SharedHelperDefinition {
	return []SharedHelperDefinition{
		{
			Name:      "upsertConflictPositions",
			Signature: "(columns, conflictColumns []string) ([]int, bool)",
			Doc: `upsertConflictPositions maps each column of an upsert conflict target to its
// position in an UpsertMany value row. Reports false when the target is empty
// or names a column the statement does not supply — neither can identify a row,
// so the caller must not dedupe on it and cannot look a written row up by it.`,
			Body: `	if len(conflictColumns) == 0 {
		return nil, false
	}
	positions := make([]int, 0, len(conflictColumns))
	for _, col := range conflictColumns {
		i := slices.Index(columns, col)
		if i < 0 {
			return nil, false
		}
		positions = append(positions, i)
	}
	return positions, true`,
		},
		{
			Name:      "upsertDedupeKey",
			Signature: "(row []any, positions []int) (string, bool)",
			Doc: `upsertDedupeKey builds the identity of one UpsertMany value row over its
// conflict target's columns, for the last-wins dedupe UpsertMany requires.
// Components are length-prefixed so two different column splits cannot produce
// one key.
//
// Reports false when the row carries no definite, non-NULL value for every
// conflict column — a DEFAULT the database has not yet evaluated, a NULL (which
// never matches under a unique index on any of the three dialects), or a Go type
// the driver cannot bind on its own. Such a row cannot be shown to collide
// in-statement, so it is kept rather than deduped.`,
			Body: `	key := ""
	for _, pos := range positions {
		v := row[pos]
		if v == sql.Default {
			return "", false
		}
		if _, ok := v.(sql.DefaultExpr); ok {
			return "", false
		}
		// The driver's own parameter conversion is the right notion of
		// identity: it is what the value binds as, so two values that bind
		// identically key identically. It also dereferences the pointer a
		// nullable column binds through — %v on a *string prints an address —
		// and reports nil for a NULL, which never matches under a unique index.
		bound, err := driver.DefaultParameterConverter.ConvertValue(v)
		if err != nil || bound == nil {
			return "", false
		}
		if t, ok := bound.(time.Time); ok {
			// Drop the monotonic reading and the sub-microsecond tail no
			// dialect stores, so a caller's time.Now() keys the same as the
			// value read back from the row it wrote.
			bound = t.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
		}
		s := fmt.Sprintf("%v", bound)
		key += fmt.Sprintf("%d:%s", len(s), s)
	}
	return key, true`,
		},
	}
}

// resolveCallOptionsBody returns the resolveCallOptions helper body. When the
// package carries the explicit-tenant CallOptions field, the body also
// normalizes the §29.4.4 precedence rule: an explicit
// tenant wins over SkipTenancy — the two only ever co-occur by accident, and
// staying tenant-scoped is the safer resolution than falling open
// cross-tenant. Every downstream `if !options.SkipTenancy` gate then behaves
// correctly without per-site changes.
func resolveCallOptionsBody(hasExplicitTenant bool) string {
	body := `	var options CallOptions[FO]
	for _, fn := range opts {
		fn(&options)
	}
	// SkipHooks implies SkipCache and SkipEvents — cache and events are hooks.
	if options.SkipHooks {
		options.SkipCache = true
		options.SkipEvents = true
	}
`
	if hasExplicitTenant {
		body += `	// Explicit tenant wins over SkipTenancy
	// — the call runs scoped to the explicit tenant.
	if options.Tenant != nil {
		options.SkipTenancy = false
	}
`
	}
	return body + `	return options`
}
