package gen

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// This file projects the nested-mutation surface (PRD §9.9) onto the GraphQL
// API (§26.5.1). It decides nothing the Go surface has not already decided:
// every edge, every verb and every family starts from the table's resolved
// NestedContext, and the API can only ever narrow it — the resolvers delegate
// to the Go client, so the schema can never advertise a verb the executor
// does not have.
//
// Three things narrow it, and each is a fact the consumer's config states:
//
//   - An edge whose target is `api.enabled: false` gets no member. The nested
//     input would otherwise be a door into an entity the schema deliberately
//     closed — the write-side mirror of the §26.5.3 read side-channel rule.
//   - An edge whose target has no API-writable column left after the §32
//     access projection gets no member.
//   - The target's `api.operations` mask, for the verbs that write the target
//     itself (PRD §9.9.4). `create` needs the target's API `create`, and
//     `create_many` wherever the executor routes through CreateMany. An O2M /
//     has-one `connect` / `disconnect` / `clear` rewrites the target row's
//     foreign key by filter, through its UpdateWhere, so it needs the target's
//     API-masked `update_where` — the entry that also gates the target's own
//     `update<T>s(filter, input)`. A mask written to leave a single,
//     hand-guarded door into a table would otherwise leave a second,
//     generated one through every parent that nests into it. The M2M link and
//     unlink verbs write only the junction row and are gated by the target's
//     `api.enabled` alone.
//
// A fourth is a fact about GraphQL rather than about the config: a nested
// child input left with no member cannot be declared, so that edge's GraphQL
// blocks carry no `create` (projectAPINestedCreate).
//
// The three config facts are dropped silently rather than reported, for the
// reason PRD §9.9.4 gives for every config-visible fact: the config already
// says why. A hard error would also be wrong on its own terms — `api` gates
// the API and never the client, so an edge the Go surface carries correctly
// must not fail generation over a GraphQL projection it cannot have.

// APINestedContext is the GraphQL projection of one parent's nested-mutation
// surface (PRD §9.9.5, §26.5.1). It is nil on every entity with nothing to
// project — no Go nested surface, a view, or an API that narrowed every family
// or every edge away (a wrapper carrying only the flat input would be the
// flat mutation with extra steps).
type APINestedContext struct {
	// EmitCreate / EmitUpdate / EmitUpsert gate the three mutations. Each is
	// the Go family's own gate, conjoined with the API-masked `…_with_related`
	// operation (which implies its base — maskAPIOperations), with the flat
	// input the wrapper contains actually being emitted, and with at
	// least one edge carrying a verb in that family's block.
	EmitCreate bool
	EmitUpdate bool
	EmitUpsert bool
	// CreateInputName / UpdateInputName / UpsertInputName are the three
	// wrapper input types — spelled exactly as the Go wrappers are, since the
	// GraphQL types are projected from them.
	CreateInputName string
	UpdateInputName string
	UpsertInputName string
	// ParentGraphQLName is the wrapper member holding the flat input (`user`),
	// and ParentFieldName its Go field — `User`, on the model wrapper and on
	// the gqlgen one alike, the latter dictated through a fieldName override.
	ParentGraphQLName string
	ParentFieldName   string
	// Edges are the edges that reach at least one emitted wrapper, in the Go
	// surface's order — byte-wise by relationship Go field name (§9.9.5).
	Edges []APINestedEdge
	// ConflictEnumName is the GraphQL `<Parent>ConflictTarget` enum the upsert
	// mutation takes, ConflictArgName its argument, ConflictTargets its values
	// in the Go constants' order, and ConflictDefault `PK` when the table has
	// a primary-key target — empty otherwise, which leaves the argument
	// required (PRD §26.5.1). All empty unless EmitUpsert.
	ConflictEnumName string
	ConflictArgName  string
	ConflictTargets  []APINestedConflictTarget
	ConflictDefault  string
	// IncrementTxName names the transaction the update mutation opens when the
	// flat input carries `_inc` / `_dec` operators: UpdateWithRelated runs
	// inside it as a savepoint, followed by the increments and the read-back,
	// so the whole mutation commits or rolls back as one (PRD §9.9, §26.5.4).
	// Distinct from the Go method's own `update_<snake>_with_related`,
	// because it doubles as a SAVEPOINT name under a caller's transaction and
	// MySQL replaces a savepoint that reuses a live name.
	IncrementTxName string
	// IncrementOpsFunc is the input-translate helper that collects those
	// operators off the wrapper, rejecting an illegal pairing with
	// INVALID_INPUT. Empty when the parent has no incrementable column.
	IncrementOpsFunc string
	// ConflictTranslateFunc maps the GraphQL enum onto the model's
	// `<Parent>ConflictTarget` constant. Empty unless EmitUpsert.
	ConflictTranslateFunc string
}

// APINestedEdge is one (parent, edge) pair's GraphQL projection.
type APINestedEdge struct {
	// FieldName is the relationship's Go field name. It names the member on
	// the model wrapper and — dictated through a fieldName override — on the
	// gqlgen one, and it is the `Edge` a failure is attributed to and the
	// value `extensions.path` carries (§9.9.8, §26.5.5).
	FieldName string
	// GraphQLName is the member's schema name — the same spelling the
	// relationship field on the object type carries, so reading an edge and
	// writing it use one name.
	GraphQLName string
	// CreateBlockName / UpdateBlockName are the two verb blocks, spelled as
	// their Go types are.
	CreateBlockName string
	UpdateBlockName string
	// EmitCreateBlock / EmitUpdateBlock report whether each block is declared
	// and referenced: the create-side one when the create mutation is emitted
	// and the edge keeps a create-side verb, the update-side one whenever the
	// update or upsert mutation is (§9.9.5 — upsert reuses the update block).
	EmitCreateBlock bool
	EmitUpdateBlock bool
	// HasCreate … HasClear are the verbs the API advertises: the Go edge's,
	// narrowed by the target's `api.operations` mask. A verb absent here is
	// absent from the input type, never present and ignored (§9.9.3).
	HasCreate     bool
	HasConnect    bool
	HasDisconnect bool
	HasClear      bool
	// Singular reports a has-one edge (§9.9.1 shape 2), whose verbs take one
	// value rather than a list — the Go block's `Create` / `Connect` /
	// `Disconnect` are pointers there, not slices.
	Singular bool
	// ChildTypeName is the element type of `create` — this edge's own
	// `<Parent><Edge>CreateInput` on shapes 2 and 3, the target's
	// `Create<Target>Input` on M2M — and ChildTranslateFunc translates one
	// element of it. CreateGraphQLType is the verb's schema type: a list of
	// that element, or the element itself on a has-one edge.
	ChildTypeName      string
	ChildTranslateFunc string
	CreateGraphQLType  string
	// ChildInputName is the nested child input this edge declares, empty on
	// M2M; ChildFields are its members — the target's projected create input
	// minus the traversed FK and the discriminator column, which is exactly
	// the Go child input intersected with the access projection (§32.5).
	ChildInputName string
	ChildFields    []APINestedChildField
	// IDGraphQLType is the schema type of `connect` / `disconnect`: a list of
	// the target's primary-key scalar, or the scalar itself on a has-one edge.
	IDGraphQLType string
	// CreateIDVerbs / UpdateIDVerbs are the id-list verbs each block carries,
	// pre-split so both block translators render them from one fragment.
	CreateIDVerbs []APINestedIDVerb
	UpdateIDVerbs []APINestedIDVerb
	// CreateBlockTranslateFunc / UpdateBlockTranslateFunc translate the two
	// blocks.
	CreateBlockTranslateFunc string
	UpdateBlockTranslateFunc string
}

// APINestedChildField is one member of a nested child input: its schema type,
// read off the target's own `Create<Target>Input` so the two cannot disagree,
// and the translator binding the target's create-input translator uses.
type APINestedChildField struct {
	GraphQLType string
	Input       APIInputField
}

// APINestedIDVerb is one id-list verb (`connect` / `disconnect`) as the block
// translator renders it.
type APINestedIDVerb struct {
	// GoField is the member name on both the gqlgen block and the model block.
	GoField string
	// Cast is the model element type when gqlgen binds the scalar to a
	// different Go type — `int64` for an `Int` key gqlgen binds to `int` — and
	// empty when the value threads through unchanged.
	Cast string
	// Singular reports a has-one verb: one optional id, a pointer on both
	// sides, rather than a list.
	Singular bool
}

// APINestedConflictTarget is one value of the `<Parent>ConflictTarget` enum.
type APINestedConflictTarget struct {
	// EnumValue is `PK` for the primary-key target and the SCREAMING_SNAKE
	// form of the target's columns otherwise (`EMAIL`, `SKU_WAREHOUSE_ID`).
	EnumValue string
	// ConstantName is the model constant it translates to.
	ConstantName string
}

// emittedTypeNames returns every GraphQL type the nested schema template
// declares for this parent, keyed by the edge that spells it (empty for the
// per-parent wrappers and the conflict-target enum). It reads the same gates
// the template does, so the name registry claims exactly what is emitted.
func (n *APINestedContext) emittedTypeNames() []nestedTypeName {
	if n == nil {
		return nil
	}
	var names []nestedTypeName
	if n.EmitCreate {
		names = append(names, nestedTypeName{name: n.CreateInputName})
	}
	if n.EmitUpdate {
		names = append(names, nestedTypeName{name: n.UpdateInputName})
	}
	if n.EmitUpsert {
		names = append(names, nestedTypeName{name: n.UpsertInputName}, nestedTypeName{name: n.ConflictEnumName})
	}
	for _, e := range n.Edges {
		if e.EmitCreateBlock {
			names = append(names, nestedTypeName{name: e.CreateBlockName, edge: e.FieldName})
		}
		if e.EmitUpdateBlock {
			names = append(names, nestedTypeName{name: e.UpdateBlockName, edge: e.FieldName})
		}
		if e.ChildInputName != "" {
			names = append(names, nestedTypeName{name: e.ChildInputName, edge: e.FieldName})
		}
	}
	return names
}

// wireAPINestedMutations attaches each table's nested-mutation projection to
// its API context and runs the completeness lint over it.
//
// A post-build pass for the reason wireNestedMutations is one: an edge is
// gated on its *target's* API context — whether it exists, its projected
// create input and its masked operations — so every table has to have
// been built first. apiTables holds tables only when this runs; views carry no
// nested surface (§16.4).
func wireAPINestedMutations(apiTables []APITableContext, tables []TableContext, cfg *config.RootConfig) error {
	targets := newAPINestedTargets(apiTables)
	casing := cfg.API.GraphQL.FieldCasing
	byStruct := targets.byStruct

	var errs []error
	for i := range tables {
		tc := &tables[i]
		if tc.Nested == nil {
			continue
		}
		idx, ok := byStruct[tc.StructName]
		if !ok {
			continue
		}
		at := &apiTables[idx]
		at.Nested = buildAPINested(tc, at, targets, casing)
		if at.Nested == nil {
			continue
		}
		if err := ValidateAPINestedCompleteness(*tc, *at); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// apiNestedTargets is what an edge is gated on: its target's API context,
// whose masked operations decide which of the verbs that write the target
// survive.
//
// Those operations are the client's own intersected with the mask, and for
// every verb the intersection and the mask agree: the client generates
// Create, CreateMany and UpdateWhere on every table (PRD §4.6), so the mask
// alone decides.
type apiNestedTargets struct {
	tables   []APITableContext
	byStruct map[string]int
}

func newAPINestedTargets(apiTables []APITableContext) apiNestedTargets {
	out := apiNestedTargets{
		tables:   apiTables,
		byStruct: make(map[string]int, len(apiTables)),
	}
	for i := range apiTables {
		if !apiTables[i].IsView {
			out.byStruct[apiTables[i].StructName] = i
		}
	}
	return out
}

// buildAPINested projects one parent's nested surface, or returns nil when
// nothing survives the projection.
func buildAPINested(tc *TableContext, at *APITableContext, targets apiNestedTargets, casing string) *APINestedContext {
	fam := apiNestedFamiliesFor(tc.Nested, at)
	// The §32 projection can leave the enum with nothing to declare — a
	// parent whose only targets are unique ones over access-restricted
	// columns. That is a config-visible fact like every other narrowing
	// here, so the upsert mutation is dropped rather than emitted with an
	// argument no caller can pass.
	conflictTargets, conflictDefault := apiNestedConflictTargets(tc)
	fam.upsert = fam.upsert && len(conflictTargets) > 0
	if !fam.any() {
		return nil
	}
	edges := buildAPINestedEdges(tc.Nested, at, targets)
	fam = fam.narrowTo(edges)
	if !fam.any() {
		return nil
	}

	out := &APINestedContext{
		EmitCreate:        fam.create,
		EmitUpdate:        fam.update,
		EmitUpsert:        fam.upsert,
		CreateInputName:   tc.Nested.CreateInputName,
		UpdateInputName:   tc.Nested.UpdateInputName,
		UpsertInputName:   tc.Nested.UpsertInputName,
		ParentGraphQLName: graphQLFieldName(tc.SnakeName, casing),
		ParentFieldName:   tc.StructName,
		Edges:             fam.referencedEdges(edges),
	}
	if fam.update && at.HasIncrementColumns {
		out.IncrementTxName = "update_" + tc.SnakeName + "_with_related_inc"
		out.IncrementOpsFunc = "update" + tc.StructName + "WithRelatedIncrementOps"
	}
	if fam.upsert {
		out.ConflictEnumName = tc.StructName + "ConflictTarget"
		// A literal, like `input` / `inputs` / `filter`, rather than a cased
		// column name: it is derived from nothing the consumer wrote, and it is
		// the spelling PRD §26.12 names.
		out.ConflictArgName = "conflictTarget"
		out.ConflictTargets, out.ConflictDefault = conflictTargets, conflictDefault
		out.ConflictTranslateFunc = "translate" + out.ConflictEnumName
	}
	return out
}

// apiNestedFamilies is which of the three `…WithRelated` mutations the API
// emits for one parent.
type apiNestedFamilies struct {
	create, update, upsert bool
}

// apiNestedFamiliesFor gates each family on the Go family, the API-masked
// `…_with_related` operation (which implies its base — maskAPIOperations), and
// the flat input the wrapper contains being declared at all: an empty flat
// input is suppressed, and a wrapper cannot reference a type that is never
// declared.
func apiNestedFamiliesFor(n *NestedContext, at *APITableContext) apiNestedFamilies {
	ops := at.Operations
	return apiNestedFamilies{
		create: n.EmitCreate && ops.CreateWithRelated && at.HasCreateInput,
		update: n.EmitUpdate && ops.UpdateWithRelated && at.HasUpdateInput,
		// n.EmitUpsert already requires a conflict target, so the flat
		// mutation's PK-only gate does not apply: on a table with no PK target
		// the nested upsert takes a required `conflictTarget` (§26.5.1).
		upsert: n.EmitUpsert && ops.UpsertWithRelated && at.HasCreateInput,
	}
}

func (f apiNestedFamilies) any() bool { return f.create || f.update || f.upsert }

// narrowTo drops a family whose wrapper no surviving edge would reach, as the
// Go surface drops a parent with no eligible edge. The create wrapper needs an
// edge with a create-side verb; the update-side one reaches every surviving
// edge.
func (f apiNestedFamilies) narrowTo(edges []APINestedEdge) apiNestedFamilies {
	f.create = f.create && slices.ContainsFunc(edges, func(e APINestedEdge) bool { return e.HasCreate || e.HasConnect })
	f.update = f.update && len(edges) > 0
	f.upsert = f.upsert && len(edges) > 0
	return f
}

// referencedEdges marks which blocks each edge declares under the settled
// families and keeps only the edges some emitted wrapper references — an edge
// left with `disconnect` alone reaches nothing when only the create mutation
// survives.
func (f apiNestedFamilies) referencedEdges(edges []APINestedEdge) []APINestedEdge {
	kept := make([]APINestedEdge, 0, len(edges))
	for _, e := range edges {
		e.EmitCreateBlock = f.create && (e.HasCreate || e.HasConnect)
		e.EmitUpdateBlock = f.update || f.upsert
		if e.EmitCreateBlock || e.EmitUpdateBlock {
			kept = append(kept, e)
		}
	}
	return kept
}

// buildAPINestedEdges projects every Go edge whose target is on the API and
// keeps the ones with a verb left.
func buildAPINestedEdges(n *NestedContext, at *APITableContext, targets apiNestedTargets) []APINestedEdge {
	graphQLNames := make(map[string]string, len(at.Relationships))
	for _, r := range at.Relationships {
		graphQLNames[r.GoFieldName] = r.GraphQLName
	}
	var edges []APINestedEdge
	for _, e := range n.Edges {
		idx, ok := targets.byStruct[e.TargetStructName]
		if !ok {
			// The target is not on the API at all.
			continue
		}
		if edge, ok := buildAPINestedEdge(e, &targets.tables[idx], graphQLNames[e.FieldName]); ok {
			edges = append(edges, edge)
		}
	}
	return edges
}

// buildAPINestedEdge projects one Go edge onto the API, returning false when
// no verb survives.
func buildAPINestedEdge(e NestedEdgeContext, target *APITableContext, graphQLName string) (APINestedEdge, bool) {
	// An empty projected create input: a write surface into a table the API can
	// write nothing to is surface area with no behavior (§32.5).
	if len(target.CreateInputFields) == 0 {
		return APINestedEdge{}, false
	}

	out := APINestedEdge{
		FieldName:                e.FieldName,
		GraphQLName:              graphQLName,
		CreateBlockName:          e.CreateBlockName,
		UpdateBlockName:          e.UpdateBlockName,
		CreateBlockTranslateFunc: "translate" + e.CreateBlockName,
		UpdateBlockTranslateFunc: "translate" + e.UpdateBlockName,
		Singular:                 e.Shape == "has_one",
	}
	projectAPINestedCreate(&out, e, target)

	// On O2M / has-one the other three verbs rewrite the target row's foreign
	// key by filter, through its UpdateWhere, so they follow the target's
	// API-masked `update_where` — the entry that also gates
	// `update<T>s(filter, input)`. On M2M they write only the junction row.
	relinkAllowed := e.Shape == "m2m" || target.Operations.UpdateWhere
	out.HasConnect = e.HasConnect && relinkAllowed
	out.HasDisconnect = e.HasDisconnect && relinkAllowed
	out.HasClear = e.HasClear && relinkAllowed

	if !out.HasCreate && !out.HasConnect && !out.HasDisconnect && !out.HasClear {
		return APINestedEdge{}, false
	}
	projectAPINestedIDVerbs(&out, target)
	return out, true
}

// projectAPINestedCreate decides the `create` verb. It inserts target rows, so
// it needs the target's own API create — and create_many wherever the Go
// executor routes through CreateMany: a has-one edge inserts its one row
// through Create.
func projectAPINestedCreate(out *APINestedEdge, e NestedEdgeContext, target *APITableContext) {
	tops := target.Operations
	if !e.HasCreate || !tops.Create || (!out.Singular && !tops.CreateMany) {
		return
	}
	switch e.Shape {
	case "m2m":
		// The target's own input and translator, both emitted because the
		// target's create survives the mask (apiInputEmission).
		if !target.HasCreateInput {
			return
		}
		out.ChildTypeName = "Create" + target.StructName + "Input"
		out.ChildTranslateFunc = "translateCreate" + target.StructName + "Input"
	default:
		// An input type needs at least one field, so a child input
		// whose every API-writable member was the FK or the discriminator
		// cannot carry `create` on this surface.
		fields := apiNestedChildFields(e, target)
		if len(fields) == 0 {
			return
		}
		out.ChildInputName = e.ChildInputName
		out.ChildTypeName = e.ChildInputName
		out.ChildTranslateFunc = "translate" + e.ChildInputName
		out.ChildFields = fields
	}
	out.HasCreate = true
	out.CreateGraphQLType = apiNestedVerbType(out.ChildTypeName, out.Singular)
}

// projectAPINestedIDVerbs fills the schema type and translator bindings of the
// id-carrying verbs. The element is the target's primary-key scalar, converted
// onto the model's key type wherever gqlgen binds it to another — the rule
// pkConvert applies to a PK argument.
func projectAPINestedIDVerbs(out *APINestedEdge, target *APITableContext) {
	if !out.HasConnect && !out.HasDisconnect {
		return
	}
	pk := target.PKArgs[0]
	out.IDGraphQLType = apiNestedVerbType(pk.GraphQLType, out.Singular)
	cast := ""
	if pk.ModelGoType != pk.GoType && pk.ModelGoType != "" {
		cast = pk.ModelGoType
	}
	if out.HasConnect {
		verb := APINestedIDVerb{GoField: "Connect", Cast: cast, Singular: out.Singular}
		out.CreateIDVerbs = append(out.CreateIDVerbs, verb)
		out.UpdateIDVerbs = append(out.UpdateIDVerbs, verb)
	}
	if out.HasDisconnect {
		out.UpdateIDVerbs = append(out.UpdateIDVerbs, APINestedIDVerb{GoField: "Disconnect", Cast: cast, Singular: out.Singular})
	}
}

// apiNestedVerbType spells a verb's schema type: a list of non-null elements,
// or — on a has-one edge, where the Go block takes one value — the element
// alone, nullable because every verb is optional.
func apiNestedVerbType(elem string, singular bool) string {
	if singular {
		return elem
	}
	return "[" + elem + "!]"
}

// apiNestedChildFields returns the nested child input's members: the target's
// projected create input, in its schema order, restricted to the columns the
// Go child input keeps. Intersecting the two is what makes both elisions —
// the traversed FK and the discriminator — and the access
// projection hold at once without restating any of them (§32.5).
func apiNestedChildFields(e NestedEdgeContext, target *APITableContext) []APINestedChildField {
	keep := make(map[string]bool, len(e.ChildFields))
	for _, f := range e.ChildFields {
		keep[f.FieldName] = true
	}
	inputs := make(map[string]APIInputField, len(target.CreateInputFields))
	for _, in := range target.CreateInputFields {
		inputs[in.GraphQLName] = in
	}
	var out []APINestedChildField
	for _, f := range target.Fields {
		if !f.InCreateInput {
			continue
		}
		in, ok := inputs[f.GraphQLName]
		if !ok || !keep[in.ModelFieldName] {
			continue
		}
		out = append(out, APINestedChildField{GraphQLType: f.GraphQLType, Input: in})
	}
	return out
}

// apiNestedConflictTargets returns the `<Parent>ConflictTarget` enum values in
// the Go constants' order, and `PK` as the default when the table has a
// primary-key target.
//
// A unique target is named after its columns rather than after its Go
// constant's suffix: the columns are what the caller reasons about, and the
// SCREAMING_SNAKE form of them is the §26.4 enum-value convention every other
// generated enum follows, including the `col_` guard for a digit-leading name.
//
// A unique target is declared only when every one of its columns is
// API-writable (PRD §32.3). ON CONFLICT fires only on a value the INSERT
// carries, and a caller supplies a value only through a column the create
// input declares. On a `read_only`, `hidden` or `internal` column the INSERT
// carries NULL, which never conflicts, the column default, or (for a
// generated column) a value computed from other columns; the caller writes
// none of them through this column, so it cannot use the target to pick a row. A
// `hidden` or `internal` target would also name its column on the public
// schema. A `write_only` target stays, because the caller can supply it. The
// PK target is exempt: it is the argument's default, and §32.4 allows a
// `read_only` PK column. Only a parent without a PK target can therefore be
// left with no value.
func apiNestedConflictTargets(tc *TableContext) (values []APINestedConflictTarget, def string) {
	writable := make(map[string]bool, len(tc.Columns))
	for _, c := range tc.Columns {
		writable[c.Name] = columnAccessCapabilities(c).APIWritable
	}
	pkConst := tc.StructName + "ConflictPK"
	for _, ct := range tc.ConflictTargets {
		isPK := ct.ConstantName == pkConst
		if !isPK && slices.ContainsFunc(ct.Columns, func(col string) bool { return !writable[col] }) {
			continue
		}
		value := screamingSnakeCase(strings.Join(ct.Columns, "_"))
		if isPK {
			value = "PK"
			def = value
		}
		values = append(values, APINestedConflictTarget{EnumValue: value, ConstantName: ct.ConstantName})
	}
	return values, def
}

// ValidateAPINestedCompleteness is the nested surface's completeness lint
// (PRD §26.5.2 / §26.5.3's fail-fast contract, applied to §26.5.1's nested
// mutations): no nested member the schema advertises may lack the translator
// that carries it to the Go surface, or the executor that honours it there.
//
// The failure it forbids is the same silent one the filter lint forbids. A
// member present in the schema and absent from the translator is accepted at
// parse time and dropped at execution time — the caller asked to connect a
// row and the server answered success without touching it. The schema and the
// translators are rendered from one slice, so the drift a pair of hand-kept
// lists can suffer is structurally unrepresentable; what is checkable, and
// checked, is that every entry can render both halves, that every verb it
// advertises is one the Go edge actually executes, and that no two members of
// one input type share a name, which would make the whole document invalid.
//
// It runs on the built context, so it covers `sqlgen validate` as well as
// `sqlgen generate`. Every violation is reported, not just the first.
func ValidateAPINestedCompleteness(tc TableContext, at APITableContext) error {
	n := at.Nested
	if n == nil {
		return nil
	}
	fail := func(format string, args ...any) error {
		return fmt.Errorf("api: nested mutations on table %q: "+format+" (PRD §26.5.1)", append([]any{tc.TableName}, args...)...)
	}
	if tc.Nested == nil {
		return fail("the GraphQL surface projects a nested surface the Go client does not have")
	}

	goEdges := make(map[string]NestedEdgeContext, len(tc.Nested.Edges))
	for _, e := range tc.Nested.Edges {
		goEdges[e.FieldName] = e
	}

	var errs []error
	if n.EmitCreate && !tc.Nested.EmitCreate || n.EmitUpdate && !tc.Nested.EmitUpdate || n.EmitUpsert && !tc.Nested.EmitUpsert {
		errs = append(errs, fail("a `…WithRelated` mutation is emitted over a Go method that is not"))
	}
	errs = append(errs, validateAPINestedMembers(n, goEdges, fail)...)
	errs = append(errs, validateAPINestedConflictTargets(n, fail)...)
	return errors.Join(errs...)
}

// validateAPINestedMembers checks every edge member: that it reaches a Go
// executor, that no two members of one wrapper share a name, and that every
// emitted wrapper carries at least one — an empty one is its flat mutation
// with extra steps and, for gqlgen, a type with nothing in it.
func validateAPINestedMembers(n *APINestedContext, goEdges map[string]NestedEdgeContext, fail func(string, ...any) error) []error {
	var errs []error
	var createMembers, updateMembers int
	names := map[string]bool{n.ParentGraphQLName: true}
	for _, e := range n.Edges {
		goEdge, ok := goEdges[e.FieldName]
		if !ok {
			errs = append(errs, fail("edge %q is advertised on the schema but has no Go executor", e.FieldName))
			continue
		}
		errs = append(errs, validateAPINestedEdge(e, goEdge, fail)...)
		if names[e.GraphQLName] {
			errs = append(errs, fail("two members of one wrapper input are named %q", e.GraphQLName))
		}
		names[e.GraphQLName] = true
		if e.EmitCreateBlock {
			createMembers++
		}
		if e.EmitUpdateBlock {
			updateMembers++
		}
	}
	if n.EmitCreate && createMembers == 0 || (n.EmitUpdate || n.EmitUpsert) && updateMembers == 0 {
		errs = append(errs, fail("a `…WithRelated` mutation is emitted with no nested member, which is its flat mutation with extra steps"))
	}
	return errs
}

// validateAPINestedEdge checks one projected edge against the Go edge it is
// carried to.
func validateAPINestedEdge(e APINestedEdge, goEdge NestedEdgeContext, fail func(string, ...any) error) []error {
	var errs []error
	if e.GraphQLName == "" || e.CreateBlockTranslateFunc == "" || e.UpdateBlockTranslateFunc == "" {
		errs = append(errs, fail("edge %q renders only one half (graphql=%q translators=%q/%q)",
			e.FieldName, e.GraphQLName, e.CreateBlockTranslateFunc, e.UpdateBlockTranslateFunc))
	}
	errs = append(errs, validateAPINestedVerbs(e, goEdge, fail)...)
	if e.EmitCreateBlock && !e.HasCreate && !e.HasConnect {
		errs = append(errs, fail("edge %q emits an empty create-side block", e.FieldName))
	}
	if e.HasCreate {
		errs = append(errs, validateAPINestedChild(e, goEdge, fail)...)
	}
	return append(errs, validateAPINestedIDVerbs(e, goEdge, fail)...)
}

// validateAPINestedIDVerbs checks the id-carrying verbs: that they have a
// schema type, and that any conversion the translator applies lands on the
// element type the Go block actually takes.
func validateAPINestedIDVerbs(e APINestedEdge, goEdge NestedEdgeContext, fail func(string, ...any) error) []error {
	var errs []error
	if (e.HasConnect || e.HasDisconnect) && e.IDGraphQLType == "" {
		errs = append(errs, fail("edge %q carries an id-list verb with no element type", e.FieldName))
	}
	for _, v := range e.UpdateIDVerbs {
		if v.Cast != "" && v.Cast != goEdge.ConnectIDGoType {
			errs = append(errs, fail("edge %q converts `%s` ids to %s, but the Go block takes %s",
				e.FieldName, strings.ToLower(v.GoField), v.Cast, goEdge.ConnectIDGoType))
		}
	}
	return errs
}

// validateAPINestedVerbs checks that every verb the schema advertises is one
// the Go edge carries. One that is not has no field on the model block for the
// translator to assign — or, worse, one the executor never reads.
func validateAPINestedVerbs(e APINestedEdge, goEdge NestedEdgeContext, fail func(string, ...any) error) []error {
	var errs []error
	for _, v := range []struct {
		verb     string
		api, got bool
	}{
		{"create", e.HasCreate, goEdge.HasCreate},
		{"connect", e.HasConnect, goEdge.HasConnect},
		{"disconnect", e.HasDisconnect, goEdge.HasDisconnect},
		{"clear", e.HasClear, goEdge.HasClear},
	} {
		if v.api && !v.got {
			errs = append(errs, fail("edge %q advertises `%s`, which its Go executor does not carry", e.FieldName, v.verb))
		}
	}
	return errs
}

// validateAPINestedChild checks the `create` verb's element type: that it has
// a translator, and — on shapes 2 and 3, where the edge declares its own child
// input — that every member names a field the Go child input has and no two
// share a name.
func validateAPINestedChild(e APINestedEdge, goEdge NestedEdgeContext, fail func(string, ...any) error) []error {
	var errs []error
	if e.ChildTypeName == "" || e.ChildTranslateFunc == "" || e.CreateGraphQLType == "" {
		errs = append(errs, fail("edge %q advertises `create` with no element type, schema type or translator", e.FieldName))
	}
	if e.ChildInputName == "" {
		return errs
	}
	if len(e.ChildFields) == 0 {
		errs = append(errs, fail("edge %q declares %s with no field, which gqlgen rejects", e.FieldName, e.ChildInputName))
	}
	model := make(map[string]bool, len(goEdge.ChildFields))
	for _, f := range goEdge.ChildFields {
		model[f.FieldName] = true
	}
	seen := make(map[string]bool, len(e.ChildFields))
	for _, f := range e.ChildFields {
		switch {
		case f.GraphQLType == "" || f.Input.GraphQLName == "" || f.Input.GoFieldName == "" || f.Input.ModelFieldName == "":
			errs = append(errs, fail("%s has a member that renders only one half (graphql=%q type=%q gqlgenField=%q modelField=%q)",
				e.ChildInputName, f.Input.GraphQLName, f.GraphQLType, f.Input.GoFieldName, f.Input.ModelFieldName))
		case !model[f.Input.ModelFieldName]:
			errs = append(errs, fail("%s member %q assigns model field %q, which the Go child input does not have (the traversed FK and the discriminator are elided — §9.9.5)",
				e.ChildInputName, f.Input.GraphQLName, f.Input.ModelFieldName))
		case seen[f.Input.GraphQLName]:
			errs = append(errs, fail("%s declares member %q twice", e.ChildInputName, f.Input.GraphQLName))
		}
		seen[f.Input.GraphQLName] = true
	}
	return errs
}

// validateAPINestedConflictTargets checks the upsert mutation's enum: every
// value translates to a constant, no two values collide — a unique target over
// a column literally named `pk` would land on the primary key's value — and
// the default, when there is one, is a value the enum declares.
func validateAPINestedConflictTargets(n *APINestedContext, fail func(string, ...any) error) []error {
	if !n.EmitUpsert {
		return nil
	}
	var errs []error
	if len(n.ConflictTargets) == 0 {
		errs = append(errs, fail("the upsert mutation takes a %s argument with no value to pass", n.ConflictEnumName))
	}
	seen := make(map[string]bool, len(n.ConflictTargets))
	for _, c := range n.ConflictTargets {
		if c.EnumValue == "" || c.ConstantName == "" {
			errs = append(errs, fail("%s has a value that renders only one half (value=%q constant=%q)", n.ConflictEnumName, c.EnumValue, c.ConstantName))
			continue
		}
		if seen[c.EnumValue] {
			errs = append(errs, fail("two conflict targets both project onto %s value %q — rename the column so the two unique constraints read apart", n.ConflictEnumName, c.EnumValue))
		}
		seen[c.EnumValue] = true
	}
	if n.ConflictDefault != "" && !seen[n.ConflictDefault] {
		errs = append(errs, fail("the upsert mutation defaults to %s value %q, which the enum does not declare", n.ConflictEnumName, n.ConflictDefault))
	}
	return errs
}
