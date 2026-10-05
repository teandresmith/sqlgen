package gen

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// This file models the package-scope names the generated code claims, so two
// schema entities that resolve to one name — or one entity that resolves to a
// name sqlgen already owns — are rejected as a config error rather than as an
// opaque redeclaration inside generated code (PRD §4.13, §8.5).
//
// It replaces a lexical approximation in config: validateAutoStructNameCollisions
// compared strings.ToLower(strings.ReplaceAll(name, "_", "")), which is neither
// sound nor complete against the real naming pipeline. It missed every
// collision singularization creates (`users` + `user`, `people` + `person`,
// `osi_layers` + `osi_layer` all resolve to one struct and one file stem), and
// it rejected `user_roles` + `userroles`, which actually resolve to UserRole
// and Userrole and generate fine. It could not be repaired in place: config
// must not import gen, and re-deriving StructName inside config is the
// two-implementations-of-one-rule drift that desyncs generated names.
// The check therefore runs where the names already exist, over built contexts,
// and is reachable from `sqlgen validate` through ValidateGeneration the same
// way the resolved-field-name rules are.
//
// Three keys, because an entity claims three independent things and any one of
// them can collide alone:
//
//   - The Go type name. Every entity declares `type <Name> ...` at package
//     scope; tables and views declare a dozen more identifiers derived from it.
//   - The file stem. Under `layout: file_per_table` the emitted file is
//     `<stem>_gen.go` and the API schema is `<stem>_gen.graphqls`, so a shared
//     stem means one file silently overwrites the other.
//   - The `hook.TableName` constant. It is the plural of the struct name, and
//     pluralization is not injective, so two entities whose struct names differ
//     can still land on one constant.
//
// Scope boundary, the same one the reserved-field rule draws: the check keys on
// each entity's *primary* name. The suffixed identifiers built from it
// (`<T>Filter`, `<T>Client`, `<T>Slice`, …) are injective in the primary name,
// so distinct primaries cannot collide within one shape; a cross-shape
// collision between two different suffix families — an enum literally named
// `order_filter` beside an `orders` table — is not discovered here and remains
// a `go build` error.
//
// One shape is carved out of that boundary, because it is not injective in any
// primary name: the nested-mutation type names (PRD §9.9.5) key on a
// **(parent, edge)** pair. A `user_events` table and the `User.Events` edge
// both resolve to `CreateUserEventInput`, so the fourth key below reads both
// shapes at once — the nested names on one side, the names entities derive on
// the other. Registering the nested names as ordinary entries would not have
// worked; they need a key kind of their own. See nested_names.go for the
// spellings and for what makes a (parent, edge) pair a candidate.

// reservedName is one identifier or file stem the generated package declares on
// its own behalf, and the artifact that declares it.
type reservedName struct {
	name  string
	owner string
}

// generatedPackageTypes are the exported package-scope identifiers every
// generated package declares regardless of what the schema contains. A schema
// entity resolving to one of them redeclares it, and no such name can produce
// compiling code in any circumstance, so rejecting it here strictly replaces
// "undefined: connectionClient" — reported from a file the consumer did not
// write, about a table sqlgen quietly overwrote — with a message that names the
// table and the artifact that took its name.
//
// Conditioning stops at the same place §8.5 stops it for reserved field names:
// a feature-gated artifact (cache, event hooks) reserves its names whether or
// not the feature is on, so enabling caching later never turns a valid config
// invalid. The config-derived entries — the client type and its option type —
// are added by reservedGeneratedTypes.
//
// Pinned against the templates by TestGeneratedPackageNames_MatchTemplates,
// which renders two full packages over disjoint entity names and intersects
// what they declare.
var generatedPackageTypes = []reservedName{
	{name: "Connection", owner: "connection_gen.go"},
	{name: "ConnectionInput", owner: "connection_gen.go"},
	{name: "Edge", owner: "connection_gen.go"},
	{name: "PageInfo", owner: "connection_gen.go"},
	{name: "PaginateInput", owner: "pagination_gen.go"},
	{name: "PaginateResult", owner: "pagination_gen.go"},
	{name: "CallOptions", owner: "shared_types_gen.go"},
	{name: "IncrementInput", owner: "shared_types_gen.go"},
	{name: "ConstraintCheck", owner: "errors_gen.go"},
	{name: "ConstraintError", owner: "errors_gen.go"},
	{name: "ConstraintForeignKey", owner: "errors_gen.go"},
	{name: "ConstraintNotNull", owner: "errors_gen.go"},
	{name: "ConstraintType", owner: "errors_gen.go"},
	{name: "ConstraintUnique", owner: "errors_gen.go"},
	{name: "ErrAlreadyRelated", owner: "errors_gen.go"},
	{name: "ErrAmbiguousFilter", owner: "errors_gen.go"},
	{name: "ErrConnectionFailed", owner: "errors_gen.go"},
	{name: "ErrConstraintViolation", owner: "errors_gen.go"},
	{name: "ErrDeadlock", owner: "errors_gen.go"},
	{name: "ErrEmptyFilter", owner: "errors_gen.go"},
	{name: "ErrNestedVerbConflict", owner: "errors_gen.go"},
	{name: "ErrNilInput", owner: "errors_gen.go"},
	{name: "ErrInvalidCursor", owner: "errors_gen.go"},
	{name: "ErrNotFound", owner: "errors_gen.go"},
	{name: "NestedMutationError", owner: "errors_gen.go"},
	{name: "Cache", owner: "cache_gen.go"},
	{name: "CacheOption", owner: "cache_gen.go"},
	{name: "NewCache", owner: "cache_gen.go"},
	{name: "WithCircuitBreaker", owner: "cache_gen.go"},
	{name: "WithInvalidationSource", owner: "cache_gen.go"},
	{name: "WithMetricsRecorder", owner: "cache_gen.go"},
	{name: "WithOnError", owner: "cache_gen.go"},
	{name: "WithSerializer", owner: "cache_gen.go"},
	{name: "WithEventPublisher", owner: "event_hooks_gen.go"},
	{name: "New", owner: "client_gen.go"},
	{name: "WithCache", owner: "client_gen.go"},
	{name: "WithCallbackMode", owner: "client_gen.go"},
	{name: "WithMutationHook", owner: "client_gen.go"},
	{name: "WithPanicHandler", owner: "client_gen.go"},
	{name: "WithQueryHook", owner: "client_gen.go"},
	{name: "WithTenantResolver", owner: "client_gen.go"},
}

// generatedPackageStems are the file stems the generator writes on its own
// behalf. A table or view whose stem matches one of them is erased by the later
// write under `layout: file_per_table`. The layout-conditional stems (`models`,
// `views`) are reserved in both layouts for the reason above — switching layout
// must not turn a valid config invalid.
//
// One stem spans a second directory: the API pass writes `<stem>_gen.graphqls`
// per table into `api.graphql.schema_dir` and then one fixed
// `shared_gen.graphqls` beside them. A table's SnakeName spells both files, so
// one list covers both namespaces; `shared` is reserved whether or not the API
// is enabled, on the same terms as the rest.
//
// The three configurable filenames (output.client.file, output.enums.file,
// output.types.file) are added by reservedGeneratedStems.
var generatedPackageStems = []reservedName{
	{name: "api_envelopes", owner: "api_envelopes_gen.go"},
	{name: "cache", owner: "cache_gen.go"},
	{name: "connection", owner: "connection_gen.go"},
	{name: "errors", owner: "errors_gen.go"},
	{name: "event_hooks", owner: "event_hooks_gen.go"},
	{name: "manifest_embed", owner: "manifest_embed_gen.go"},
	{name: "models", owner: "models_gen.go"},
	{name: "pagination", owner: "pagination_gen.go"},
	{name: "sets", owner: "sets_gen.go"},
	{name: "shared", owner: "shared_gen.graphqls"},
	{name: "shared_types", owner: "shared_types_gen.go"},
	{name: "sorter", owner: "sorter_gen.go"},
	{name: "tablenames", owner: "tablenames_gen.go"},
	{name: "views", owner: "views_gen.go"},
}

// reservedGeneratedTypes returns the package-scope identifiers no entity may
// resolve to, keyed by name. The client type and its option type carry
// output.client.name, so they are read from the config rather than frozen.
func reservedGeneratedTypes(cfg *config.RootConfig) map[string]string {
	reserved := make(map[string]string, len(generatedPackageTypes)+2)
	for _, r := range generatedPackageTypes {
		reserved[r.name] = r.owner
	}
	if name := clientTypeName(cfg); name != "" {
		file := clientFileName(cfg)
		reserved[name] = file
		reserved[name+"Option"] = file
	}
	return reserved
}

// reservedGeneratedStems returns the file stems no entity may resolve to,
// keyed by stem.
func reservedGeneratedStems(cfg *config.RootConfig) map[string]string {
	reserved := make(map[string]string, len(generatedPackageStems)+3)
	for _, r := range generatedPackageStems {
		reserved[r.name] = r.owner
	}
	for _, file := range []string{clientFileName(cfg), enumsFileName(cfg), typesFileName(cfg)} {
		if stem := goFileStem(file); stem != "" {
			reserved[stem] = file
		}
	}
	return reserved
}

// claimedName is one name a schema entity resolves to, and enough about the
// entity to name it and its escape hatch in an error.
type claimedName struct {
	name string
	by   entityRef
}

// entityRef identifies the schema entity that resolved to a name.
type entityRef struct {
	// kind is the entity category as it reads in an error ("table", "view").
	kind string
	// qualified is the schema-qualified SQL name.
	qualified string
	// escape is the config key that renames the entity, empty when the
	// entity category has no rename override.
	escape string
}

// tableRef and viewRef build the reference an error names an entity by,
// including the config key that renames it.
func tableRef(schema, name string) entityRef {
	qualified := qualifiedOrBare(schema, name)
	return entityRef{kind: "table", qualified: qualified, escape: "tables." + qualified + ".struct_name"}
}

func viewRef(schema, name string) entityRef {
	qualified := qualifiedOrBare(schema, name)
	return entityRef{kind: "view", qualified: qualified, escape: "views." + qualified + ".struct_name"}
}

// relationshipRef names one (parent, edge) pair, and nestedSurfaceRef names a
// parent's nested surface as a whole — the three `…WithRelatedInput` wrappers
// are keyed on the parent alone.
//
// Only the second carries an escape, and the asymmetry is the point. A wrapper
// name is spelled from the parent's struct name alone, so renaming the parent
// fixes it with one key. A per-edge name is spelled from the parent's struct
// name *and* the relationship's field name; the relationship half is renamed by
// declaring it under `tables.<parent>.relationships`, which is a list rather
// than a single key, so naming a key path there would be pointing at something
// that does not exist. pairHint then offers the other side's rename, which for
// the collisions that actually occur is the entity's own `struct_name`.
func relationshipRef(parentSchema, parentTable, edgeField string) entityRef {
	return entityRef{kind: "relationship", qualified: qualifiedOrBare(parentSchema, parentTable) + "." + edgeField}
}

func nestedSurfaceRef(parentSchema, parentTable string) entityRef {
	qualified := qualifiedOrBare(parentSchema, parentTable)
	return entityRef{
		kind:      "nested mutations on table",
		qualified: qualified,
		escape:    "tables." + qualified + ".struct_name",
	}
}

// String renders the entity for an error message.
func (e entityRef) String() string { return e.kind + " " + e.qualified }

// hint renders the actionable half of a reserved-name error.
func (e entityRef) hint() string {
	if e.escape == "" {
		return "rename it in the schema"
	}
	return "set " + e.escape + " to a different Go identifier"
}

// pairHint renders the actionable half of a collision error. Renaming either
// entity resolves it, so both escape hatches are offered — naming only one
// reads as an accusation, and the one worth renaming is the consumer's call.
// A category with no override (a composite type, a built-in registry scalar)
// contributes nothing.
func pairHint(a, b entityRef) string {
	var escapes []string
	for _, e := range []entityRef{a, b} {
		if e.escape != "" && !slices.Contains(escapes, e.escape) {
			escapes = append(escapes, e.escape)
		}
	}
	switch len(escapes) {
	case 0:
		return "rename one of them in the schema"
	case 1:
		return "set " + escapes[0] + " to a different Go identifier"
	default:
		return "set " + escapes[0] + " or " + escapes[1] + " to a different Go identifier"
	}
}

// resolvedNames carries every name the generated package claims on behalf of a
// schema entity, grouped by the kind of name so each is checked against its own
// reserved set.
type resolvedNames struct {
	// types are Go type names, claimed by every entity category.
	types []claimedName
	// stems are file stems, claimed by tables and views only — the other
	// categories share one file each.
	stems []claimedName
	// constants are hook.TableName constants, claimed by tables and views.
	constants []claimedName
	// nested are the nested-mutation type names, claimed by a (parent, edge)
	// pair or by a parent's nested surface as a whole (PRD §9.9.5).
	nested []claimedName
	// writeInputs are the `Create<T>Input` / `Update<T>Input` names each table
	// derives. They are not checked against each other — they are injective in
	// the table's primary name, which the `types` key already guards — and are
	// collected only so the nested names have the other half of their
	// cross-shape comparison to run against.
	writeInputs []claimedName
}

// collectResolvedNames reads the names out of the built contexts. Reading them
// rather than re-deriving them is the point: the contexts already applied every
// rule that decides a name (singularization, the schema prefix, `struct_name`)
// and every rule that drops an entity before it gets one (`exclude_tables`, a
// table with no resolved primary key), so the check cannot disagree with what
// generation emits. Every emitting site reads the same contexts —
// buildTableNameFileContext included — so the three namespaces the check guards
// are exactly the three the package declares.
func collectResolvedNames(ctxs generationContexts) resolvedNames {
	tables, views, enums, sets, types := ctxs.tables, ctxs.views, ctxs.enums, ctxs.sets, ctxs.types

	var out resolvedNames

	for _, tc := range tables {
		ref := tableRef(tc.Schema, tc.TableName)
		out.types = append(out.types, claimedName{name: tc.StructName, by: ref})
		out.stems = append(out.stems, claimedName{name: tc.SnakeName, by: ref})
		out.constants = append(out.constants, claimedName{name: tc.TableNameConstant, by: ref})
		out.writeInputs = append(out.writeInputs,
			claimedName{name: "Create" + tc.StructName + "Input", by: ref},
			claimedName{name: "Update" + tc.StructName + "Input", by: ref},
		)
		for _, n := range nestedSurfaceNames(tc) {
			nestedBy := nestedSurfaceRef(tc.Schema, tc.TableName)
			if n.edge != "" {
				nestedBy = relationshipRef(tc.Schema, tc.TableName, n.edge)
			}
			out.nested = append(out.nested, claimedName{name: n.name, by: nestedBy})
		}
	}
	for _, vc := range views {
		ref := viewRef(vc.Schema, vc.ViewName)
		out.types = append(out.types, claimedName{name: vc.StructName, by: ref})
		out.stems = append(out.stems, claimedName{name: vc.SnakeName, by: ref})
		out.constants = append(out.constants, claimedName{name: vc.TableNameConstant, by: ref})
	}
	for _, ec := range enums {
		out.types = append(out.types, claimedName{name: ec.GoTypeName, by: enumRef(ec.Schema, ec.Name)})
	}
	for _, sc := range sets {
		ref := entityRef{kind: "SET type", qualified: qualifiedOrBare(sc.Schema, sc.Name)}
		// A SET declares TWO package-scope types in sets_gen.go, and both must
		// be claimed. The value type was previously unclaimed, so a MySQL
		// inline ENUM resolving to `<Set>Value` produced a redeclaration the
		// generated package could not compile and `sqlgen validate` did not
		// see. It also silently shadows: the API layer keys its
		// Go-type lookup on the value type name, and the SET entries are merged
		// after the enums, so the last writer would win with no diagnostic.
		out.types = append(out.types, claimedName{name: sc.GoTypeName, by: ref})
		out.types = append(out.types, claimedName{name: sc.ValueGoTypeName, by: ref})
	}
	for _, ct := range types.Composites {
		ref := entityRef{kind: "composite type", qualified: qualifiedOrBare(ct.Schema, ct.Name)}
		out.types = append(out.types, claimedName{name: ct.GoTypeName, by: ref})
	}
	for _, dt := range types.Domains {
		ref := entityRef{kind: "domain type", qualified: qualifiedOrBare(dt.Schema, dt.Name)}
		out.types = append(out.types, claimedName{name: dt.GoTypeName, by: ref})
	}
	for _, et := range types.Extras {
		// An extra is declared in the config rather than the schema, so the
		// rename lives there too.
		ref := entityRef{
			kind:      "extra type",
			qualified: et.GoTypeName,
			escape:    "extras." + et.GoTypeName,
		}
		out.types = append(out.types, claimedName{name: et.GoTypeName, by: ref})
	}

	return out
}

// validateResolvedPackage runs every resolution-level (PRD §4.13 phase 3) rule
// that needs the whole package in view at once, and is the single call each
// context-building entry point makes — buildAllContexts for `generate`,
// ValidateGeneration for `validate`, and BuildEntityContextsFromSchema for
// `graphql gen`. Adding a rule here reaches all three; adding one at the call
// sites reaches whichever were remembered, which is how `graphql gen` once
// became the only path that never checked for a shared file stem.
//
// validateTypeOverrideImports is the one member that reads config text rather
// than the built contexts. It rides here anyway because "all three entry
// points" is the property it needs and this is the only place that has it —
// the per-table seam validateColumnTypeLiterals uses cannot see the global
// `overrides.types` map or `extras`, and cannot see a `tables.<t>` block whose
// key matches no table in the schema.
//
// Errors are joined rather than short-circuited, for the reason validate
// exists: one run answers for the whole config.
func validateResolvedPackage(cfg *config.RootConfig, ctxs generationContexts) error {
	return errors.Join(
		validateResolvedNames(cfg, ctxs),
		validateUUIDLibraries(ctxs),
		validateTypeOverrideImports(cfg),
	)
}

// validateResolvedNames rejects two entities that resolve to one Go type name,
// file stem or table-name constant, and any entity that resolves to a name the
// generated package already declares (PRD §4.13, §8.5).
//
// Every violation is reported, not just the first: `sqlgen validate` answers
// for the whole config in one pass.
func validateResolvedNames(cfg *config.RootConfig, ctxs generationContexts) error {
	claimed := collectResolvedNames(ctxs)

	// Type names and table-name constants share a reserved set because they
	// share a namespace: both are package-scope declarations in one file.
	reservedTypes := reservedGeneratedTypes(cfg)

	// One rename fixes all three keys, so a pair that collides on all three is
	// one problem and reads as one error. Reporting per key turned a single
	// `users` / `user` pair into three near-identical lines.
	dups := newClaimReport()
	dups.collect("Go type name", claimed.types, duplicateClaims)
	dups.collect("file stem", claimed.stems, duplicateClaims)
	dups.collect("table-name constant", claimed.constants, duplicateClaims)
	// The nested names run last and against a seeded set, so every hit has a
	// nested claim on at least one side. Folding them into the first pass
	// instead would re-report a `users` / `user` pair — which already collides
	// on the primary name — a second time under `CreateUserInput`.
	dups.collect("Go type name", claimed.nested,
		seededDuplicateClaims(slices.Concat(claimed.types, claimed.writeInputs)))

	reserved := newClaimReport()
	reserved.collect("Go type name", claimed.types, reservedClaims(reservedTypes))
	reserved.collect("file stem", claimed.stems, reservedClaims(reservedGeneratedStems(cfg)))
	reserved.collect("table-name constant", claimed.constants, reservedClaims(reservedTypes))
	reserved.collect("Go type name", claimed.nested, reservedClaims(reservedTypes))

	return errors.Join(slices.Concat(dups.duplicateErrors(), reserved.reservedErrors())...)
}

// violation is one entity (or pair of entities) failing one of the rules, with
// the claims that failed accumulated across all three keys.
type violation struct {
	// a is the entity at fault; b is the entity it collided with, zero for a
	// reserved-name hit.
	a, b entityRef
	// owner names the generated artifact that already declares the name, empty
	// for an entity-vs-entity collision.
	owner string
	// claims reads as "the Go type name \"User\"", one per key that failed.
	claims []string
}

// claimReport accumulates violations so a pair that fails on more than one key
// is reported once, in the order the keys were collected.
type claimReport struct {
	order []string
	byKey map[string]*violation
}

func newClaimReport() *claimReport {
	return &claimReport{byKey: make(map[string]*violation)}
}

// add records that v's subject failed on `what` with the name `name`, merging
// into an existing violation for the same subject.
func (r *claimReport) add(key, what, name string, v violation) {
	existing, ok := r.byKey[key]
	if !ok {
		v.claims = []string{fmt.Sprintf("the %s %q", what, name)}
		r.byKey[key] = &v
		r.order = append(r.order, key)
		return
	}
	existing.claims = append(existing.claims, fmt.Sprintf("the %s %q", what, name))
}

// ruleHit is what a rule reports for one failing claim. key identifies the
// subject so hits from different rules merge onto one violation.
type ruleHit struct {
	key  string
	name string
	v    violation
}

// collect runs one rule over one key's claims and folds the results in.
func (r *claimReport) collect(what string, claims []claimedName, rule func([]claimedName) []ruleHit) {
	for _, hit := range rule(claims) {
		r.add(hit.key, what, hit.name, hit.v)
	}
}

// duplicateClaims reports every name claimed by more than one entity.
//
// The pair is ordered by name before it becomes a key, so the same two entities
// produce the same key whichever rule found them. They do not arrive in one
// order: the type-name and file-stem claims come from the contexts, sorted by
// (schema, name), while the constant claims come from the schema in parse
// order. Without the canonical ordering a pair colliding on all three keys
// reported as two errors instead of one.
func duplicateClaims(claims []claimedName) []ruleHit {
	return seededDuplicateClaims(nil)(claims)
}

// seededDuplicateClaims is duplicateClaims with a set of names already taken
// before the walk begins. It is what lets one key kind be checked against
// another without merging the two into one namespace: the seed contributes
// collisions but never reports one of its own, so a pre-existing violation
// among the seeded claims stays with the rule that owns it.
func seededDuplicateClaims(prior []claimedName) func([]claimedName) []ruleHit {
	return func(claims []claimedName) []ruleHit {
		first := make(map[string]entityRef, len(prior)+len(claims))
		for _, c := range prior {
			if c.name == "" {
				continue
			}
			if _, seen := first[c.name]; !seen {
				first[c.name] = c.by
			}
		}

		var hits []ruleHit
		for _, c := range claims {
			if c.name == "" {
				continue
			}
			prev, seen := first[c.name]
			if !seen {
				first[c.name] = c.by
				continue
			}
			a, b := prev, c.by
			if b.String() < a.String() {
				a, b = b, a
			}
			hits = append(hits, ruleHit{
				key:  a.String() + "\x00" + b.String(),
				name: c.name,
				v:    violation{a: a, b: b},
			})
		}
		return hits
	}
}

// reservedClaims reports every claim on a name the generated package declares
// itself.
func reservedClaims(reserved map[string]string) func([]claimedName) []ruleHit {
	return func(claims []claimedName) []ruleHit {
		var hits []ruleHit
		for _, c := range claims {
			owner, ok := reserved[c.name]
			if !ok {
				continue
			}
			hits = append(hits, ruleHit{
				key:  c.by.String() + "\x00" + owner,
				name: c.name,
				v:    violation{a: c.by, owner: owner},
			})
		}
		return hits
	}
}

// duplicateErrors renders the entity-vs-entity violations.
func (r *claimReport) duplicateErrors() []error {
	errs := make([]error, 0, len(r.order))
	for _, key := range r.order {
		v := r.byKey[key]
		errs = append(errs, fmt.Errorf(
			"%s and %s both resolve to %s — %s",
			v.a, v.b, joinClaims(v.claims), pairHint(v.a, v.b),
		))
	}
	return errs
}

// reservedErrors renders the entity-vs-sqlgen violations.
func (r *claimReport) reservedErrors() []error {
	errs := make([]error, 0, len(r.order))
	for _, key := range r.order {
		v := r.byKey[key]
		errs = append(errs, fmt.Errorf(
			"%s resolves to %s, which the generated %s already declares — %s",
			v.a, joinClaims(v.claims), v.owner, v.a.hint(),
		))
	}
	return errs
}

// joinClaims renders one to three claims as an English list.
func joinClaims(claims []string) string {
	switch len(claims) {
	case 1:
		return claims[0]
	case 2:
		return claims[0] + " and " + claims[1]
	default:
		return strings.Join(claims[:len(claims)-1], ", ") + " and " + claims[len(claims)-1]
	}
}

// qualifiedOrBare renders a schema-qualified SQL name, dropping the prefix for
// the dialects that have no schemas.
func qualifiedOrBare(schema, name string) string {
	if schema == "" {
		return name
	}
	return schema + "." + name
}

// goFileStem strips the "_gen.go" suffix a generated filename carries, so a
// configured filename can be compared against the stems tables resolve to.
func goFileStem(file string) string {
	stem, ok := strings.CutSuffix(file, "_gen.go")
	if !ok {
		return strings.TrimSuffix(file, ".go")
	}
	return stem
}

// clientTypeName returns the configured client struct name, tolerating a
// config that has not been through LoadConfig's defaults.
func clientTypeName(cfg *config.RootConfig) string {
	if cfg == nil || cfg.Output.Client == nil {
		return "Client"
	}
	if cfg.Output.Client.Name == "" {
		return "Client"
	}
	return cfg.Output.Client.Name
}

func clientFileName(cfg *config.RootConfig) string {
	if cfg == nil || cfg.Output.Client == nil || cfg.Output.Client.File == "" {
		return "client_gen.go"
	}
	return cfg.Output.Client.File
}

func enumsFileName(cfg *config.RootConfig) string {
	if cfg == nil || cfg.Output.Enums == nil || cfg.Output.Enums.File == "" {
		return "enums_gen.go"
	}
	return cfg.Output.Enums.File
}

func typesFileName(cfg *config.RootConfig) string {
	if cfg == nil || cfg.Output.Types == nil || cfg.Output.Types.File == "" {
		return "types_gen.go"
	}
	return cfg.Output.Types.File
}
