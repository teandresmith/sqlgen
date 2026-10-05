package gen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// relationshipTargets indexes the entities a relationship may point at — every
// table that generates (it survives `exclude_tables` and has a resolved
// primary key, PRD §6.4 / §9.4b) and every view — under the names a
// relationship spells its target with, each mapped to the schema it lives in
// and the struct name it generates under.
//
// A relationship's field type is that struct name, so it is read off the
// target rather than re-derived from the SQL name. A re-derivation cannot see a
// `struct_name` override, and it cannot see that the target generates nothing
// at all; either way the field names a type that is never declared (PRD
// §8.5).
//
// The schema is read off the target for the same reason. Every other consumer
// of the edge — the O2O join builder, fkColumnOnTarget, the FK metadata wiring,
// the relationship filter and the nested surface — looks the target up by
// (TargetSchema, TargetTable). Carrying the resolved schema makes each of them
// name the table the struct describes, rather than whichever same-named table
// its own bare-name fallback happens to find.
//
// A junction is not a target: an M2M edge reads its junction by name and never
// types a field as it, so an excluded junction keeps working. Its name still
// resolves by the same PRD §5.5 rule, over the same count of parsed entities
// (resolveJunction), so the loader, the relationship filter, the FK metadata
// and the nested link step all name one table.
type relationshipTargets struct {
	qualified map[string]relationshipTarget // "schema.name" → the entity
	bare      map[string]relationshipTarget // "name" → the entity, when one entity has the name
	ambiguous map[string][]string           // bare name → the schemas of every entity answering to it
	schemas   map[string][]string           // bare name → the schema of every parsed entity with the name
}

// relationshipTarget is the entity a relationship's target name resolves to.
type relationshipTarget struct {
	Schema     string // "" in a dialect without schemas
	StructName string
}

// newRelationshipTargets builds the index from the parsed schema and config
// alone, so BuildTableContexts builds it once, before the first table.
//
// Ambiguity counts every parsed table and view, not only the ones that
// generate. With `public.orders` excluded and `audit.orders` generated, a bare
// `orders` is still a name two schemas declare, and PRD §5.5's ambiguity rule
// asks the user to qualify it rather than resolving it to the one that happens
// to generate.
func newRelationshipTargets(input *GenerateInput, collisions map[string]bool) *relationshipTargets {
	t := &relationshipTargets{
		qualified: make(map[string]relationshipTarget),
		bare:      make(map[string]relationshipTarget),
		ambiguous: make(map[string][]string),
		schemas:   make(map[string][]string),
	}
	// add records one parsed entity; structName is "" when it generates nothing.
	add := func(schema, name, structName string) {
		t.schemas[name] = append(t.schemas[name], schema)
		if len(t.schemas[name]) > 1 {
			delete(t.bare, name)
			t.ambiguous[name] = t.schemas[name]
		}
		if structName == "" {
			return
		}
		target := relationshipTarget{Schema: schema, StructName: structName}
		if schema != "" {
			t.qualified[schema+"."+name] = target
		}
		if _, ambiguous := t.ambiguous[name]; !ambiguous {
			t.bare[name] = target
		}
	}
	cfg := input.Config
	for i := range input.Schema.Tables {
		table := &input.Schema.Tables[i]
		_, excluded := config.MatchExcludeTablePattern(table.Name, table.Schema, cfg.ExcludeTables)
		if excluded || !tableHasResolvedPK(cfg, table) {
			add(table.Schema, table.Name, "")
			continue
		}
		override := resolveTableConfig(cfg.Tables, table.Schema, table.Name).StructName
		add(table.Schema, table.Name, structNameFor(table.Name, table.Schema, override, collisions))
	}
	for i := range input.Schema.Views {
		view := &input.Schema.Views[i]
		viewCfg, _ := resolveViewConfig(cfg.Views, view.Schema, view.Name)
		add(view.Schema, view.Name, structNameFor(view.Name, view.Schema, viewCfg.StructName, collisions))
	}
	return t
}

// targetResolution is what a relationship's target name resolves to.
type targetResolution int

const (
	targetGenerated targetResolution = iota
	// targetMissing: no generated entity answers to the name — the table is
	// excluded, has no primary key, or is not in the schema.
	targetMissing
	// targetAmbiguous: a bare name that parsed entities in more than one
	// schema answer to (PRD §5.5's ambiguity rule).
	targetAmbiguous
)

// resolve returns the entity the named target generates as. A
// schema-qualified reference resolves exactly; a bare one resolves when exactly
// one entity has the name. The target is zero unless the result is
// targetGenerated.
func (t *relationshipTargets) resolve(schema, name string) (relationshipTarget, targetResolution) {
	if schema != "" {
		if target, ok := t.qualified[schema+"."+name]; ok {
			return target, targetGenerated
		}
		return relationshipTarget{}, targetMissing
	}
	if len(t.ambiguous[name]) > 0 {
		return relationshipTarget{}, targetAmbiguous
	}
	if target, ok := t.bare[name]; ok {
		return target, targetGenerated
	}
	return relationshipTarget{}, targetMissing
}

// resolveJunction splits a declared `junction:` into the schema and table the
// edge reads its links through. A schema-qualified name is taken as written. A
// bare one takes the schema of the one parsed table or view with that name, and
// is ambiguous when more than one schema has one — PRD §5.5's rule for
// relationship references, counted over the same entities as a `table:`. A bare
// name that no parsed entity has is carried as written, with no schema.
//
// Resolving the schema here is what makes the junction's consumers agree. The
// M2M loader and the relationship filter emit JunctionSchema into the SQL, the
// FK metadata reads the junction's columns by it, and the nested link step
// looks its client up by it. Left empty, the SQL resolved the name through the
// search path while the other two each applied their own bare-name rule, and a
// nested `connect` wrote the link to one table and read it back from another.
func (t *relationshipTargets) resolveJunction(junction string) (schema, name string, ambiguous bool) {
	schema, name = splitQualifiedName(junction)
	if schema != "" {
		return schema, name, false
	}
	switch schemas := t.schemas[name]; len(schemas) {
	case 0:
		return "", name, false
	case 1:
		return schemas[0], name, false
	default:
		return "", name, true
	}
}

// generates reports whether an FK-inferred relationship's target — spelled
// "schema.name", or "name" in a dialect without schemas — resolves to a
// generated entity. The parser qualifies every target whenever the table has a
// schema, so an inferred target is never ambiguous in practice; if one were, it
// is omitted like any other edge that resolves to nothing.
func (t *relationshipTargets) generates(qualifiedTarget string) bool {
	_, res := t.resolve(splitQualifiedName(qualifiedTarget))
	return res == targetGenerated
}

// checkDeclaredTarget rejects a declared relationship whose `table:` does not
// resolve to exactly one generated entity. Unlike an FK-inferred edge, the
// user asked for this one by name, so it fails loudly rather than being
// dropped (PRD §4.13).
//
// A schema-qualified `table:` resolves exactly. A bare one that entities in
// more than one schema answer to falls under PRD §5.5's ambiguity rule —
// relationship references follow the config-key rules — so it is an error that
// asks for the qualified spelling, not a silent choice of one schema.
func checkDeclaredTarget(r config.TableRelationship, targets *relationshipTargets, qualifiedTable string) error {
	schema, name := splitQualifiedName(r.Table)
	_, res := targets.resolve(schema, name)
	switch res {
	case targetMissing:
		return fmt.Errorf(
			"tables.%s.relationships: %q targets %q, which generates no entity — it is excluded by `exclude_tables`, has no primary key, or is not in the schema (PRD §4.13, §6.4, §9.4b)",
			qualifiedTable, r.Name, r.Table,
		)
	case targetAmbiguous:
		return fmt.Errorf(
			"tables.%s.relationships: %q targets %q, %s",
			qualifiedTable, r.Name, r.Table, ambiguityRemedy(targets.ambiguous[name], name),
		)
	default:
		return nil
	}
}

// checkDeclaredJunction rejects a declared M2M relationship whose bare
// `junction:` names a table or view in more than one schema. It is the rule
// checkDeclaredTarget applies to `table:` (PRD §5.5), with the same remedy,
// rather than a silent choice of one schema.
func checkDeclaredJunction(r config.TableRelationship, targets *relationshipTargets, qualifiedTable string) error {
	_, name, ambiguous := targets.resolveJunction(r.Junction)
	if !ambiguous {
		return nil
	}
	return fmt.Errorf(
		"tables.%s.relationships: %q reads through junction %q, %s",
		qualifiedTable, r.Name, r.Junction, ambiguityRemedy(targets.ambiguous[name], name),
	)
}

// ambiguityRemedy spells the tail of a PRD §5.5 ambiguity error: the schemas
// that declare the bare name, and each qualified spelling as the remedy.
func ambiguityRemedy(schemas []string, name string) string {
	spellings := make([]string, len(schemas))
	for i, s := range schemas {
		spellings[i] = strconv.Quote(s + "." + name)
	}
	return fmt.Sprintf(
		"which names an entity in more than one schema (%s); qualify it as one of %s (PRD §5.5)",
		strings.Join(schemas, ", "), strings.Join(spellings, ", "),
	)
}

// checkResolvedDuplicate re-runs PRD §13.7.3's duplicate-relationship check on
// the declared entry at index i of rels, with its target spelled as the
// `schema.table` it resolves to. It records the entry's key in seen and
// returns an error when an earlier entry already holds it.
//
// config.ValidatePreParse runs the same check first, but before the schema is
// parsed it can only compare `table:` as written, so `documents` and
// `public.documents` — the same table — pass it as different targets and would
// generate two fields over one edge. An M2M entry's `junction:` is compared as
// the table it resolves to, for the same reason. Two same-named
// tables in different schemas resolve apart and stay legal. An entry whose
// target does not resolve is skipped here, since checkDeclaredTarget reports
// it; an ambiguous junction is compared as written, since
// checkDeclaredJunction reports it. Like the pre-parse check, this one ignores
// `exclude_relationships`.
func checkResolvedDuplicate(
	r config.TableRelationship, i int, rels []config.TableRelationship,
	targets *relationshipTargets, seen map[string]int, qualifiedTable string,
) error {
	schema, name := splitQualifiedName(r.Table)
	target, res := targets.resolve(schema, name)
	if res != targetGenerated {
		return nil
	}
	resolved := qualifiedTableName(target.Schema, name)
	canonical := r
	canonical.Table = resolved
	if jSchema, jName, ambiguous := targets.resolveJunction(r.Junction); r.Junction != "" && !ambiguous {
		canonical.Junction = qualifiedTableName(jSchema, jName)
	}
	key := config.RelationshipDedupKey(canonical)
	prev, ok := seen[key]
	if !ok {
		seen[key] = i
		return nil
	}
	// Name the junction too: two entries that differ only in how they spell it
	// would otherwise read as `"tags" and "tags"`.
	if r.Junction != "" {
		resolved += " through junction " + canonical.Junction
	}
	return fmt.Errorf(
		"tables.%s.relationships[%d]: duplicate relationship — same target, fk, filter and discriminator as relationships[%d] (PRD §13.7.3): %q and %q both name %s; give each entry a distinct filter or discriminator value to produce sub-categorized fields",
		qualifiedTable, i, prev, r.Table, rels[prev].Table, resolved,
	)
}
