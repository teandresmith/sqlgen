package config

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// validateNestedMutations enforces the config-answerable nested-mutation rules
// from PRD §4.13: the depth ceiling, the two closed value sets, and the
// base-operation rule — a `…_with_related` operation enabled without the base
// operation it composes.
//
// The `filter` × `discriminator` exclusion lives in validateRelationships,
// beside the dedup key it shares a subject with; the discriminator's
// column-existence rule needs the parsed schema and so runs post-parse in
// validateRelationshipDiscriminators.
func validateNestedMutations(cfg *RootConfig, errs *[]error) {
	validateNestedMutationsBlock(cfg.Generation.NestedMutations, errs)
	validateNestedWithRelatedBase(cfg, errs)
	validateNestedRequiredKeys(cfg, errs)
}

// validateNestedRequiredKeys enforces the keys PRD §4.8 and §13.4.1 mark
// Required on the two blocks this phase introduces: a discriminator's `column`
// and `value`, and an allowlist entry's `name`.
//
// schema/v1.json already declares all three in its `required` lists, so
// without this the Go loader is laxer than the schema it ships — a file the
// editor paints red and `sqlgen validate` waves through, which is the
// asymmetry TestSchemaV1_acceptsUnexercisedSurface exists to keep out in the
// other direction.
//
// Absence is `Value == nil` — the key missing, or explicitly null — not an
// empty string. `value: ""` is a key the user did write, and an empty-string
// discriminator is odd but expressible; the schema accepts it too. Leaving it
// unchecked also matters for the dedup key, where a nil value renders as
// `<nil>:<nil>` and would make two value-less edges collide as duplicates over
// a value neither of them declares.
func validateNestedRequiredKeys(cfg *RootConfig, errs *[]error) {
	for _, tableName := range sortedTableKeys(cfg.Tables) {
		table := cfg.Tables[tableName]

		for i, r := range table.Relationships {
			if r.Discriminator == nil {
				continue
			}
			if r.Discriminator.Column == "" {
				*errs = append(*errs, fmt.Errorf(
					"tables.%s.relationships[%d] (%q).discriminator.column: required — it names the discriminator column on the related table (PRD §4.8 / §13.4.1)",
					tableName, i, r.Name,
				))
			}
			if r.Discriminator.Value == nil {
				*errs = append(*errs, fmt.Errorf(
					"tables.%s.relationships[%d] (%q).discriminator.value: required — it is the value that identifies this edge, and a nested create writes it into the column (PRD §4.8 / §13.4.1)",
					tableName, i, r.Name,
				))
			}
			validateDiscriminatorValue(tableName, i, r, errs)
		}

		if table.NestedMutations == nil {
			continue
		}
		for i, e := range table.NestedMutations.Relationships {
			if e.Name == "" {
				*errs = append(*errs, fmt.Errorf(
					"tables.%s.nested_mutations.relationships[%d].name: required — it names the relationship's resolved Go field on the entity struct (PRD §4.8)",
					tableName, i,
				))
			}
		}
	}
}

// validateNestedMutationsBlock checks one generation.nested_mutations block:
// max_depth, and the closed sets behind `operations` and `verbs`.
func validateNestedMutationsBlock(nm *NestedMutationsConfig, errs *[]error) {
	if nm == nil {
		return
	}

	// Depth greater than 1 is not implemented in v1, and 0 disables nothing
	// `enabled: false` does not already disable. Rejecting every other value
	// is what keeps a future depth from silently meaning depth 1.
	if nm.MaxDepth != nil && *nm.MaxDepth != NestedMutationsMaxDepth {
		*errs = append(*errs, fmt.Errorf(
			"generation.nested_mutations.max_depth: %d is not supported — %d is the only accepted value in v1 (PRD §4.13 / §9.9); omit the key to take the default",
			*nm.MaxDepth, NestedMutationsMaxDepth,
		))
	}

	// Both are closed sets. A typo'd verb would otherwise silently narrow the
	// generated surface, which is the accept-and-drop shape these rules exist
	// to prevent.
	for _, check := range [...]struct {
		key     string
		got     []string
		allowed []string
	}{
		{"operations", nm.Operations, nestedMutationOperations},
		{"verbs", nm.Verbs, nestedMutationVerbs},
	} {
		for i, v := range check.got {
			if slices.Contains(check.allowed, v) {
				continue
			}
			*errs = append(*errs, fmt.Errorf(
				"generation.nested_mutations.%s[%d]: %q is not a valid value (allowed: %s) (PRD §4.6 / §4.13)",
				check.key, i, v, strings.Join(check.allowed, ", "),
			))
		}
	}
}

// nestedWithRelatedPairs maps each `…_with_related` mask key to the base key it
// composes. On an API mask the nested mutation is conjoined with its masked
// base (PRD §26.5.1), so the pair is the one way to spell a nested key that
// can never take effect (PRD §4.13).
var nestedWithRelatedPairs = [...]struct {
	nestedKey string
	baseKey   string
	nested    func(*Operations) *bool
	base      func(*Operations) *bool
}{
	{
		"create_with_related", "create",
		func(o *Operations) *bool { return o.CreateWithRelated },
		func(o *Operations) *bool { return o.Create },
	},
	{
		"update_with_related", "update",
		func(o *Operations) *bool { return o.UpdateWithRelated },
		func(o *Operations) *bool { return o.Update },
	},
	{
		"upsert_with_related", "upsert",
		func(o *Operations) *bool { return o.UpsertWithRelated },
		func(o *Operations) *bool { return o.Upsert },
	},
}

// validateNestedWithRelatedBase enforces the base-operation rule on every API
// mask — api.operations and tables.<name>.api.operations. The mask conjoins
// each `…_with_related` key with its masked base (PRD §26.5.1), so that a mask
// written to leave one hand-guarded door into a table does not leave a second through the nested
// mutation; `create: false` beside `create_with_related: true` therefore drops
// `create<T>WithRelated` from the API with no signal unless this rejects it.
//
// The Go client has no half of this rule any more: it has no operations toggle
// (PRD §4.6), and its nested methods are governed by `nested_mutations` alone.
//
// The rule fires on an *explicitly set* `…_with_related: true` whose resolved
// base is false — the user set a flag and would get nothing, with no signal. A
// preset never trips it on its own: each arm resolves the nested key to follow
// its base. It also deliberately does not fire when only the base is named —
// `{create: false}` under the default `all` preset says nothing about nested
// mutations, and rejecting it would punish a mask that never asked for them.
func validateNestedWithRelatedBase(cfg *RootConfig, errs *[]error) {
	if cfg.API == nil {
		return
	}
	if cfg.API.Operations != nil {
		checkWithRelatedBases("api.operations", *cfg.API.Operations, errs)
	}
	for _, tableName := range sortedTableKeys(cfg.Tables) {
		api := cfg.Tables[tableName].API
		if api == nil || api.Operations == nil {
			continue
		}
		checkWithRelatedBases(fmt.Sprintf("tables.%s.api.operations", tableName), *api.Operations, errs)
	}
}

// checkWithRelatedBases reports every `…_with_related` key enabled without its
// base in one `api.operations` mask.
func checkWithRelatedBases(path string, ops Operations, errs *[]error) {
	const reason = "the API exposes a nested mutation only beside its flat %s mutation (PRD §4.13 / §26.5.1)"
	resolved, err := ResolveOperations(ops)
	if err != nil {
		// Unknown preset — checkAPIOperationsBlock already reported it, and
		// nothing resolved, so there is no base value to judge against.
		return
	}
	for _, p := range nestedWithRelatedPairs {
		if declared := p.nested(&ops); declared == nil || !*declared {
			continue
		}
		if base := p.base(&resolved); base != nil && *base {
			continue
		}
		*errs = append(*errs, fmt.Errorf(
			"%s.%s: cannot be true while %s resolves to false — %s; enable %s or drop %s",
			path, p.nestedKey, p.baseKey, fmt.Sprintf(reason, p.baseKey), p.baseKey, p.nestedKey,
		))
	}
}

// discriminatorValueKinds is the scalar set schema/v1.json declares for
// `discriminator.value`. Go must agree with the schema it ships: the generator
// renders the value into Go source and, on the O2O path, into SQL text, and it
// has no correct rendering for a sequence or a mapping — it would emit
// `"[a b]"` and generate a predicate matching nothing, with no error at load,
// validate or generate time.
var discriminatorValueKinds = map[reflect.Kind]bool{
	reflect.String: true, reflect.Bool: true,
	reflect.Int: true, reflect.Int64: true, reflect.Uint64: true,
	reflect.Float64: true,
}

// validateDiscriminatorValue rejects the value shapes the generator cannot
// render: a non-scalar kind, and a backslash on an edge whose predicate is
// interpolated rather than bound.
//
// The backslash rule is narrow on purpose. MySQL treats `\` as an escape inside
// a string literal unless NO_BACKSLASH_ESCAPES is set, while PostgreSQL and
// SQLite take it literally, so no single interpolated spelling is correct on all
// three — and vitess parses the malformed result faithfully rather than
// rejecting it, so the codegen qualifier is not a backstop. Only the O2O JOIN
// `ON` clause interpolates (PRD §13.4.1 "Binding"); O2M and M2M bind the value,
// where a backslash is just data, so they are left alone rather than punished
// for a limitation that is not theirs.
func validateDiscriminatorValue(tableName string, i int, r TableRelationship, errs *[]error) {
	v := r.Discriminator.Value
	if v == nil {
		return // the required-key rule above already reported it
	}
	if !discriminatorValueKinds[reflect.ValueOf(v).Kind()] {
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.relationships[%d] (%q).discriminator.value: must be a string, number or boolean (PRD §4.8 / §13.4.1); got %T, which the generator has no way to render",
			tableName, i, r.Name, v,
		))
		return
	}
	str, ok := v.(string)
	if !ok || !strings.Contains(str, `\`) || isM2M(r.Type) || !isO2O(r.Type) {
		return
	}
	*errs = append(*errs, fmt.Errorf(
		"tables.%s.relationships[%d] (%q).discriminator.value: an o2o discriminator is interpolated into the JOIN ON clause, which carries no parameter (PRD §13.4.1), and a backslash has no spelling that is correct on MySQL and PostgreSQL alike — remove it, or express this edge as o2m, where the value is bound",
		tableName, i, r.Name,
	))
}

// isO2O reports whether a relationship type spelling refers to one-to-one.
// Mirrors parseRelationshipType's synonyms, as isM2M does.
func isO2O(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "o2o", "one_to_one":
		return true
	}
	return false
}

// validateRelationshipDiscriminators enforces the §4.13 rule that a
// `discriminator.column` names a real column on the *related* table. The
// discriminator compiles to a predicate on that table and a nested `create`
// sets it; a column that does not exist can do neither.
//
// A relationship whose target table is absent from the parsed schema is
// skipped rather than reported here: the missing-target diagnosis belongs to
// the generator's relationship resolution, and stacking a second error on it
// would name the discriminator for a problem the discriminator does not have.
func validateRelationshipDiscriminators(cfg *RootConfig, tables []SchemaTable, errs *[]error) {
	var parsed map[string][]SchemaColumn

	for _, tableName := range sortedTableKeys(cfg.Tables) {
		for i, r := range cfg.Tables[tableName].Relationships {
			if r.Discriminator == nil || r.Discriminator.Column == "" {
				// An empty column is the missing-key case, which
				// validateNestedRequiredKeys names for what it is. Reporting it
				// a second time as "not a column on the related table" would
				// describe the symptom rather than the cause.
				continue
			}
			if parsed == nil {
				parsed = make(map[string][]SchemaColumn, len(tables)*2)
				for _, t := range tables {
					parsed[t.Name] = t.Columns
					parsed[qualifiedTableName(t)] = t.Columns
				}
			}
			cols, ok := parsed[r.Table]
			if !ok {
				continue
			}
			if slices.ContainsFunc(cols, func(c SchemaColumn) bool { return c.Name == r.Discriminator.Column }) {
				continue
			}
			*errs = append(*errs, fmt.Errorf(
				"tables.%s.relationships[%d] (%q).discriminator.column: %q is not a column on related table %q (PRD §4.13 / §13.4.1)",
				tableName, i, r.Name, r.Discriminator.Column, r.Table,
			))
		}
	}
}
