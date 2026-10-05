package gen

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/teandresmith/sqlgen/parser"
)

// RelationshipFilterAliasToken stands in for the subquery's target alias inside
// a relationship filter's codegen-qualified static predicate. The real alias is
// depth-dependent (tgt0, tgt1, …) and therefore unknown until the filter is
// compiled, so the predicate is qualified against this token at generation time
// and the token is spliced out into a Go concatenation by the template.
const RelationshipFilterAliasToken = "sqlgenrel"

// aliasCollisionProbe is a second, deliberately unrelated alias used to tell a
// real column reference apart from a filter's own text. It must not contain
// [RelationshipFilterAliasToken] as a substring, or qualifying against it would
// reproduce the very prefix the check is looking for.
const aliasCollisionProbe = "sqlgenaliasprobe"

// wireRelationshipFilters fills RelationshipFilters on every table context and
// resolves the two package-level flags the filter template reads
// (HasFilterOptions, HasTenantedRelationshipFilter).
//
// It runs as a late cross-table pass, after tenancy has been attached, because
// each entry carries facts that live on the *target's* context — its single PK
// column, its soft-delete column, and its tenant column. PRD §11.1 requires the
// target's own scoping rules inside the subquery, and those rules are the
// target's to state, not the parent's.
//
// Views are passed through so their filters carry the same ToConditions
// signature as tables'. A view has no relationships, so it never contributes an
// entry of its own.
func wireRelationshipFilters(tables []TableContext, views []ViewContext) error {
	targets := make(map[string]*TableContext, len(tables)*2)
	for i := range tables {
		tc := &tables[i]
		targets[tc.TableName] = tc
		if tc.Schema != "" {
			targets[qualifiedTableName(tc.Schema, tc.TableName)] = tc
		}
	}

	var errs []error
	for i := range tables {
		rfs, err := buildRelationshipFilters(&tables[i], targets)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		tables[i].RelationshipFilters = rfs
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}

	hasAny := slices.ContainsFunc(tables, func(tc TableContext) bool {
		return len(tc.RelationshipFilters) > 0
	})
	for i := range tables {
		tables[i].HasFilterOptions = hasAny
	}
	for i := range views {
		views[i].HasFilterOptions = hasAny
	}

	resolveTenantedRelationshipFilters(tables, targets)
	backfillRelationshipFilterTenancy(tables)
	return nil
}

// backfillRelationshipFilterTenancy gives a table that must resolve a tenant —
// but carries no tenant column of its own — the metadata the client template
// needs to render `tenancy.TenantResolver[T]`, resolveTenant, and filterOptions.
//
// A shared lookup table with a list relationship to a tenanted one is the case:
// it is untenanted, so attachTenancyToTables left its Tenancy either nil or
// zero-valued, yet compiling its relationship filter injects the *target's*
// tenant predicate and therefore needs a resolver. Without the backfill the
// template emits `tenancy.TenantResolver[]` and omits the tenancy import.
//
// This mirrors the identical backfill annotateO2OChildTenancy performs for
// HasTenantedO2OChild (context_tenancy.go), and draws the type from any
// tenanted table rather than from the tenancy map: §29.2.4 makes the tenant Go
// type uniform across every tenanted entity, so any of them is the right
// answer.
func backfillRelationshipFilterTenancy(tables []TableContext) {
	var goType, importPath string
	for i := range tables {
		if tc := tables[i].Tenancy; tc != nil && tc.Tenanted && tc.GoType != "" {
			goType, importPath = tc.GoType, tc.Import
			break
		}
	}
	if goType == "" {
		return
	}

	for i := range tables {
		tc := &tables[i]
		if !tc.HasTenantedRelationshipFilter {
			continue
		}
		tc.Imports = UniqueImports(append(tc.Imports, "github.com/teandresmith/sqlgen/tenancy"))
		if tc.Tenancy != nil && tc.Tenancy.Tenanted {
			continue
		}
		if tc.Tenancy == nil {
			tc.Tenancy = &TableTenancyContext{Tenanted: false}
		}
		tc.Tenancy.GoType = goType
		tc.Tenancy.Import = importPath
		if importPath != "" {
			tc.Imports = UniqueImports(append(tc.Imports, importPath))
		}
	}
}

// buildRelationshipFilters returns one entry per list relationship on tc that
// can be compiled into a correlated EXISTS.
//
// A relationship is skipped — rather than emitted half-formed — when the shape
// cannot be expressed by [sql.Exists], which carries exactly one correlation
// column: a parent without a single-column PK has no single column to
// correlate on, and an M2M whose target has no single-column PK has nothing to
// join the junction to. The same applies to a target outside the generated set
// (excluded by `exclude_tables`, or in another package), which has no filter
// type to reference.
func buildRelationshipFilters(tc *TableContext, targets map[string]*TableContext) ([]RelationshipFilterContext, error) {
	if len(tc.PKColumns) != 1 {
		return nil, nil
	}

	taken := make(map[string]bool, len(tc.FilterFields)+3)
	for _, f := range tc.FilterFields {
		if f.Filterable {
			taken[f.FieldName] = true
		}
	}
	taken["And"] = true
	taken["Or"] = true
	taken["PKs"] = true
	// The filter type's own method would be shadowed by a field of the same
	// name, so a relationship named ToConditions cannot contribute a member.
	taken["ToConditions"] = true

	rels := make([]RelationshipContext, 0, len(tc.O2MRelationships)+len(tc.M2MRelationships))
	rels = append(rels, tc.O2MRelationships...)
	rels = append(rels, tc.M2MRelationships...)

	out := make([]RelationshipFilterContext, 0, len(rels))
	var errs []error
	for _, rel := range rels {
		// A relationship whose Go field name is already spoken for by a column
		// comparator cannot add a second struct field under the same name. The
		// column wins, matching the model struct's existing behaviour.
		if taken[rel.FieldName] {
			continue
		}
		target, ok := lookupTarget(targets, rel)
		if !ok {
			continue
		}
		entry, ok := relationshipFilterEntry(tc, rel, target)
		if !ok {
			continue
		}
		staticFilter, err := relationshipStaticFilter(tc, rel)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		entry.StaticFilterExpr = staticFilter
		entry.Discriminator = rel.Discriminator
		taken[rel.FieldName] = true
		out = append(out, entry)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	slices.SortFunc(out, func(a, b RelationshipFilterContext) int {
		return strings.Compare(a.FieldName, b.FieldName)
	})
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// relationshipStaticFilter returns the Go expression that rebuilds the
// relationship's config `filter:` predicate (PRD §13.7.1) with the subquery's
// runtime alias spliced in, or "" when the relationship declares none.
//
// Qualification is what the O2M/M2M *loader* does not need and this does: the
// loader runs a standalone query over one table, so a bare column can only mean
// that table. Inside the subquery there are two other relations in scope — the
// junction, for an M2M, and the enclosing statement — so a bare name is either
// ambiguous (MySQL and PostgreSQL both reject it) or silently binds to the
// parent row. context_table.go qualifies the O2O JOIN-ON predicate for the same
// reason.
//
// The expression is built here rather than by a funcmap function because both
// halves of the splice need facts a template function cannot reach: the dialect,
// which decides how the qualifier spells the alias, and an error channel for
// when it cannot be derived. guidelines/TEMPLATES.md §5 requires funcmap
// functions to be pure, I/O-free and error-free, so this follows PKAutoGenExpr's
// precedent — a value a funcmap's minimal input cannot compute is a context
// field, resolved in the pass that has the facts.
func relationshipStaticFilter(tc *TableContext, rel RelationshipContext) (string, error) {
	if strings.TrimSpace(rel.Filter) == "" {
		return "", nil
	}
	// A "$" would be consumed as a placeholder when Exists renumbers the
	// subquery, shifting every caller-supplied arg by one. The loader path is
	// immune because sql.Raw carries no args, so this only bites here — and a
	// silent arg shift is far worse than a codegen error.
	if strings.Contains(rel.Filter, "$") {
		return "", fmt.Errorf(
			"table %s: relationship %s: filter %q contains %q, which a relationship filter subquery would consume as a placeholder",
			tc.TableName, rel.FieldName, rel.Filter, "$",
		)
	}
	// The alias token is spliced out of the qualified text by matching it, so
	// the filter may not carry one of its own. Rejected in any case: a column
	// named for the token would in fact survive the splice, but a *string
	// literal* containing it would be rewritten into the runtime alias and
	// silently change the value compared — the same class of defect as the "$"
	// rejection above, and the reason the substitution below can be total
	// rather than merely careful.
	if strings.Contains(strings.ToLower(rel.Filter), RelationshipFilterAliasToken) {
		return "", fmt.Errorf(
			"table %s: relationship %s: filter %q contains %q, the placeholder a relationship filter subquery splices its target alias in through",
			tc.TableName, rel.FieldName, rel.Filter, RelationshipFilterAliasToken,
		)
	}
	qualified, err := qualifyFilter(rel.Filter, RelationshipFilterAliasToken, tc.Dialect)
	if err != nil {
		return "", fmt.Errorf("table %s: relationship %s: filter %q: %w", tc.TableName, rel.FieldName, rel.Filter, err)
	}
	prefix, err := qualifiedAliasPrefix(RelationshipFilterAliasToken, tc.Dialect)
	if err != nil {
		return "", fmt.Errorf("table %s: relationship %s: filter %q: %w", tc.TableName, rel.FieldName, rel.Filter, err)
	}
	// A predicate can synthesize the prefix in its *deparsed* text without ever
	// spelling it in the config, which the raw-text guard above cannot see:
	// MySQL folds adjacent string literals, so `'sqlgen' 'rel.x'` deparses to
	// `'sqlgenrel.x'`, and PostgreSQL decodes escapes, so `E'sqlgen\162el.x'`
	// does the same. The splice would then rewrite that literal's own contents
	// into the runtime alias and silently change the value compared — and the
	// assertion below could not catch it, because a consumed token leaves
	// nothing behind to find.
	//
	// Qualifying the same predicate against a *different* alias isolates what
	// the filter itself contributes: the real references move to the other
	// alias, so anything still spelling this prefix is the filter's own content.
	collision, err := qualifyFilter(rel.Filter, aliasCollisionProbe, tc.Dialect)
	if err != nil {
		return "", fmt.Errorf("table %s: relationship %s: filter %q: %w", tc.TableName, rel.FieldName, rel.Filter, err)
	}
	if strings.Contains(collision, prefix) {
		return "", fmt.Errorf(
			"table %s: relationship %s: filter %q deparses to text containing %q outside a column reference, which a relationship filter subquery would rewrite into its target alias; a string literal reaches this state without spelling the token (MySQL folds adjacent literals, PostgreSQL decodes escapes)",
			tc.TableName, rel.FieldName, rel.Filter, prefix,
		)
	}
	expr := spliceRelationshipFilterAlias(qualified, prefix)
	// The splice is the only thing standing between the token and the emitted
	// SQL, where it would name a relation the subquery never declares. Failing
	// here costs a generation; letting it through costs a query error on the
	// one dialect whose spelling moved.
	if strings.Contains(expr, RelationshipFilterAliasToken) {
		return "", fmt.Errorf(
			"table %s: relationship %s: filter %q: alias placeholder %q survived qualification as %q — the %s qualifier spells a qualified reference in a way the splice did not match",
			tc.TableName, rel.FieldName, rel.Filter, RelationshipFilterAliasToken, qualified, tc.Dialect,
		)
	}
	return expr, nil
}

// spliceRelationshipFilterAlias rewrites a codegen-qualified predicate into a Go
// expression that concatenates the subquery's runtime `tgt` variable where the
// qualifier wrote the alias.
//
// The real alias is depth-dependent — tgt0 at the top level, tgt1 one hop deeper
// — because a relationship filter nested inside itself is legal for a
// self-referential relationship (PRD §13.5), and a fixed alias would let the
// inner subquery shadow the reference its own correlation resolves against. So
// the predicate is qualified against a placeholder at generation time and the
// placeholder is spliced out here; the generated code does no string surgery of
// its own.
//
// prefix is the dialect's own spelling of that placeholder, from
// qualifiedAliasPrefix, and carries the separator. A predicate with no bare
// identifier to qualify (`1 = 1`) contains no prefix and comes back as a plain
// quoted literal.
func spliceRelationshipFilterAlias(qualified, prefix string) string {
	parts := strings.Split(qualified, prefix)
	if len(parts) == 1 {
		return strconv.Quote(qualified)
	}
	var b strings.Builder
	if parts[0] != "" {
		b.WriteString(strconv.Quote(parts[0]))
		b.WriteString(" + ")
	}
	for i, part := range parts[1:] {
		if i > 0 {
			b.WriteString(" + ")
		}
		b.WriteString("tgt + ")
		b.WriteString(strconv.Quote("." + part))
	}
	return b.String()
}

// lookupTarget resolves a relationship's target context by the schema the
// edge carries, so two same-named tables in different schemas stay distinct.
// Every edge carries its target's schema whenever the dialect has one (the
// parser qualifies an FK-inferred target, and configRelationshipToContext
// resolves a declared one), so a schema-qualified miss is a target
// with no table context (a view), never a reason to try another schema's
// table of the same name. The bare name is the key only without a schema.
func lookupTarget(targets map[string]*TableContext, rel RelationshipContext) (*TableContext, bool) {
	target, ok := targets[qualifiedTableName(rel.TargetSchema, rel.TargetTable)]
	return target, ok
}

// relationshipFilterEntry builds the entry for one list relationship, or
// reports false when the edge's join metadata is incomplete for its shape.
func relationshipFilterEntry(tc *TableContext, rel RelationshipContext, target *TableContext) (RelationshipFilterContext, bool) {
	entry := RelationshipFilterContext{
		FieldName:         rel.FieldName,
		SQLName:           rel.Name,
		JSONTag:           rel.JSONTag,
		TargetStructName:  target.StructName,
		TargetTable:       target.TableName,
		TargetSchema:      target.Schema,
		HelperName:        toCamelCase(tc.StructName) + rel.FieldName + "FilterExists",
		CorrelationColumn: tc.PKColumns[0].Name,
		SoftDelete:        relationshipFilterSoftDelete(target),
		TenantColumn:      relationshipFilterTenantColumn(target),
	}

	if rel.Type == parser.ManyToMany {
		if len(target.PKColumns) != 1 || rel.JunctionTable == "" ||
			rel.JunctionLocalFK == "" || rel.JunctionReferenceFK == "" {
			return RelationshipFilterContext{}, false
		}
		entry.IsM2M = true
		entry.JunctionTable = rel.JunctionTable
		entry.JunctionSchema = rel.JunctionSchema
		entry.JunctionLocalFK = rel.JunctionLocalFK
		entry.JunctionReferenceFK = rel.JunctionReferenceFK
		entry.TargetPKColumn = target.PKColumns[0].Name
		return entry, true
	}

	if rel.FKColumn == "" {
		return RelationshipFilterContext{}, false
	}
	entry.FKColumn = rel.FKColumn
	return entry, true
}

// relationshipFilterSoftDelete returns the soft-delete predicate the subquery
// must inject for target, or nil when it must inject none. `exclude_deleted:
// false` turns the default off on the target's own read path (PRD §17.3), and
// a relationship filter is a read of the target, so it follows the same rule.
func relationshipFilterSoftDelete(target *TableContext) *SoftDeleteContext {
	if target.SoftDelete == nil || !target.ExcludeDeleted {
		return nil
	}
	return target.SoftDelete
}

// relationshipFilterTenantColumn returns the target's tenant column, or "" when
// the target is not tenanted. The column is read from the target's own per-table
// tenancy config, never inherited from the parent (PRD §29.10).
func relationshipFilterTenantColumn(target *TableContext) string {
	if target.Tenancy == nil || !target.Tenancy.Tenanted {
		return ""
	}
	return target.Tenancy.Column
}

// resolveTenantedRelationshipFilters marks every table whose relationship
// filters can reach a tenanted target, following the relationship-filter graph
// to a fixpoint.
//
// The closure is what makes the transitive case correct: an untenanted target
// that itself carries a relationship filter on a tenanted table still needs the
// resolved tenant threaded down to it, so the parent has to resolve one even
// though nothing it points at directly is tenanted.
func resolveTenantedRelationshipFilters(tables []TableContext, targets map[string]*TableContext) {
	// Keyed on the qualified name so two same-named tables in different
	// schemas stay distinct, the way the target lookup already is.
	tenanted := make(map[string]bool, len(tables))
	for i := range tables {
		tc := &tables[i]
		for _, rf := range tc.RelationshipFilters {
			if rf.TenantColumn != "" {
				tenanted[qualifiedTableName(tc.Schema, tc.TableName)] = true
				break
			}
		}
	}

	for changed := true; changed; {
		changed = false
		for i := range tables {
			tc := &tables[i]
			key := qualifiedTableName(tc.Schema, tc.TableName)
			if tenanted[key] {
				continue
			}
			for _, rf := range tc.RelationshipFilters {
				target, ok := lookupTarget(targets, RelationshipContext{
					TargetSchema: rf.TargetSchema,
					TargetTable:  rf.TargetTable,
				})
				if !ok {
					continue
				}
				if tenanted[qualifiedTableName(target.Schema, target.TableName)] {
					tenanted[key] = true
					changed = true
					break
				}
			}
		}
	}

	for i := range tables {
		tables[i].HasTenantedRelationshipFilter = tenanted[qualifiedTableName(tables[i].Schema, tables[i].TableName)]
	}
}
