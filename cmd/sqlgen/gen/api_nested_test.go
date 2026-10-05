package gen_test

import (
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// The GraphQL projection of nested mutations (PRD §26.5.1, §26.5.5, §9.9.4,
// §32.5).
//
// The projection decides nothing the Go surface has not already decided; it
// can only narrow it. So the tests below assert against the Go surface built
// from the same fixture — nestedSchema, the one the Go eligibility matrix
// tests pin on — rather than restating the matrix: with nothing
// masked the two must agree edge for edge and verb for verb, and every
// narrowing is a named config fact.

// buildNestedAPI runs the production context pipeline over nestedSchema with
// nested mutations and the GraphQL API both enabled.
func buildNestedAPI(t *testing.T, mutate func(*config.RootConfig)) (*gen.APIContext, map[string]gen.TableContext) {
	t.Helper()
	input := apiTestInput(t, nestedSchema())
	cfg := input.Config
	cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
	cfg.Tables["users"] = config.TableConfig{Relationships: nestedUserRelationships()}
	if mutate != nil {
		mutate(cfg)
	}
	contexts, err := gen.BuildTableContextsFromSchema(input.Schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(contexts, nil, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext() error: %v", err)
	}
	byName := make(map[string]gen.TableContext, len(contexts))
	for _, tc := range contexts {
		byName[tc.TableName] = tc
	}
	return apiCtx, byName
}

// setTableAPI sets `tables.<name>.api` without disturbing the rest of the
// table's config.
func setTableAPI(cfg *config.RootConfig, table string, api config.TableAPIConfig) {
	tc := cfg.Tables[table]
	tc.API = &api
	cfg.Tables[table] = tc
}

func apiUsers(t *testing.T, apiCtx *gen.APIContext) *gen.APITableContext {
	t.Helper()
	at := findAPIEntity(t, apiCtx, "User")
	if at == nil {
		t.Fatal("users is absent from the API context")
	}
	return at
}

// apiEdgeVerbs renders an API edge's verb set in §9.9.3 column order.
func apiEdgeVerbs(e gen.APINestedEdge) []string {
	var v []string
	for _, x := range []struct {
		on   bool
		name string
	}{{e.HasCreate, "create"}, {e.HasConnect, "connect"}, {e.HasDisconnect, "disconnect"}, {e.HasClear, "clear"}} {
		if x.on {
			v = append(v, x.name)
		}
	}
	return v
}

func goEdgeVerbs(e gen.NestedEdgeContext) []string {
	var v []string
	for _, x := range []struct {
		on   bool
		name string
	}{{e.HasCreate, "create"}, {e.HasConnect, "connect"}, {e.HasDisconnect, "disconnect"}, {e.HasClear, "clear"}} {
		if x.on {
			v = append(v, x.name)
		}
	}
	return v
}

// apiNestedVerbs maps each API edge to its verb set; absent edges are absent.
func apiNestedVerbs(at *gen.APITableContext) map[string][]string {
	out := map[string][]string{}
	if at.Nested == nil {
		return out
	}
	for _, e := range at.Nested.Edges {
		out[e.FieldName] = apiEdgeVerbs(e)
	}
	return out
}

// TestAPINested_UnmaskedProjectionIsTheGoSurface pins the invariant the whole
// projection rests on: with every target on the API and nothing masked, the
// schema advertises exactly the Go surface — the same families, the same
// edges in the same order, and the same verbs on each.
func TestAPINested_UnmaskedProjectionIsTheGoSurface(t *testing.T) {
	apiCtx, tables := buildNestedAPI(t, nil)
	at := apiUsers(t, apiCtx)
	goNested := tables["users"].Nested
	if at.Nested == nil || goNested == nil {
		t.Fatalf("nested surface: api=%v go=%v, want both", at.Nested != nil, goNested != nil)
	}

	n := at.Nested
	if n.EmitCreate != goNested.EmitCreate || n.EmitUpdate != goNested.EmitUpdate || n.EmitUpsert != goNested.EmitUpsert {
		t.Errorf("families api=(%v,%v,%v) go=(%v,%v,%v), want equal", n.EmitCreate, n.EmitUpdate, n.EmitUpsert,
			goNested.EmitCreate, goNested.EmitUpdate, goNested.EmitUpsert)
	}
	want := map[string][]string{}
	for _, e := range goNested.Edges {
		want[e.FieldName] = goEdgeVerbs(e)
	}
	// The one exception is not a mask: Tagged's child input is empty on this
	// surface, and TestAPINested_EmptyChildInputCarriesNoCreate pins why.
	want["Tagged"] = slices.DeleteFunc(want["Tagged"], func(v string) bool { return v == "create" })
	if diff := cmp.Diff(want, apiNestedVerbs(at)); diff != "" {
		t.Errorf("edge → verbs mismatch (-go +api):\n%s", diff)
	}

	goOrder := make([]string, 0, len(goNested.Edges))
	apiOrder := make([]string, 0, len(n.Edges))
	for _, e := range goNested.Edges {
		goOrder = append(goOrder, e.FieldName)
	}
	for _, e := range n.Edges {
		apiOrder = append(apiOrder, e.FieldName)
	}
	if !slices.Equal(goOrder, apiOrder) {
		t.Errorf("edge order api=%v, want the Go surface's %v (§9.9.5)", apiOrder, goOrder)
	}
	if n.ParentGraphQLName != "user" || n.ParentFieldName != "User" {
		t.Errorf("flat-input member = %q / %q, want user / User", n.ParentGraphQLName, n.ParentFieldName)
	}
	if diff := cmp.Diff([]gen.APINestedConflictTarget{{EnumValue: "PK", ConstantName: "UserConflictPK"}}, n.ConflictTargets); diff != "" || n.ConflictDefault != "PK" {
		t.Errorf("conflict targets (-want +got):\n%s\ndefault = %q, want PK", diff, n.ConflictDefault)
	}
}

// TestAPINested_HiddenTargetGetsNoMember pins PRD §9.9.4's API-exposure rule
// (§26.5.1, §32.5): an edge into an `api.enabled: false` entity is absent from
// the GraphQL surface, and the Go surface keeps it — `api` gates the API, never
// the client. Both edges into `events` go; the rest of `users` stays.
func TestAPINested_HiddenTargetGetsNoMember(t *testing.T) {
	apiCtx, tables := buildNestedAPI(t, func(cfg *config.RootConfig) {
		setTableAPI(cfg, "events", config.TableAPIConfig{Enabled: new(false)})
	})
	at := apiUsers(t, apiCtx)
	got := apiNestedVerbs(at)
	for _, hidden := range []string{"Events", "Tagged"} {
		if _, ok := got[hidden]; ok {
			t.Errorf("edge %q into the api-disabled `events` table is still on the API: %v", hidden, got[hidden])
		}
		if !slices.ContainsFunc(tables["users"].Nested.Edges, func(e gen.NestedEdgeContext) bool { return e.FieldName == hidden }) {
			t.Errorf("edge %q left the Go surface too — `api.enabled` gates GraphQL only", hidden)
		}
	}
	if _, ok := got["Categories"]; !ok {
		t.Errorf("edges = %v, want every edge not into `events` kept", got)
	}

	// The whole document, not only the wrappers: the object type drops its
	// read fields into `events` by the same rule (PRD §26.10), so no
	// member anywhere may name the hidden type.
	out := renderAPITableSchema(t, *at)
	mustNotContain(t, out, "UserEventsCreateNested", "UserTaggedUpdateNested", "events:", "tagged:", "Event!")
	// The wrappers stay: the edges not into `events` still fill them.
	mustContainAll(t, out, "input CreateUserWithRelatedInput {", "input UpdateUserWithRelatedInput {", "input UpsertUserWithRelatedInput {")
}

// TestAPINested_EmptyChildInputCarriesNoCreate pins the one narrowing that is
// not a config fact: a nested child input left with no member once the access
// projection has run cannot be declared, because a GraphQL input type needs at
// least one field. Tagged is that shape — `events` is (id, user_id, action),
// the FK and the discriminator are the edge's own and the server-generated key
// is not API-writable — so its GraphQL blocks carry the relink verbs and no
// `create`, while the Go surface keeps all four.
func TestAPINested_EmptyChildInputCarriesNoCreate(t *testing.T) {
	apiCtx, tables := buildNestedAPI(t, nil)
	goTagged := nestedEdge(t, tables["users"], "Tagged")
	if !goTagged.HasCreate || len(goTagged.ChildFields) == 0 {
		t.Fatalf("control: Go Tagged create=%v childFields=%d, want a create over a non-empty Go input", goTagged.HasCreate, len(goTagged.ChildFields))
	}
	if got := apiNestedVerbs(apiUsers(t, apiCtx))["Tagged"]; !slices.Equal(got, []string{"connect", "disconnect", "clear"}) {
		t.Errorf("API Tagged verbs = %v, want the relink verbs without `create`", got)
	}
	out := renderAPITableSchema(t, *apiUsers(t, apiCtx))
	mustNotContain(t, out, "UserTaggedCreateInput")
	mustContain(t, out, "input UserTaggedCreateNested {\n  connect: [UUID!]\n}")
}

// TestAPINested_ProjectedEmptyTargetGetsNoMember pins the non-empty create
// input rule on the projected input (PRD §32.5): a target whose every
// create-input column is dropped by its access role gets no member at all — not
// merely no `create`, though its `update_where` would otherwise carry `connect`
// / `disconnect` / `clear`.
//
// The target's API create is masked off first, which is what §32.4 requires
// before a required-on-create column may be hidden; the mask alone would have
// left the three relink verbs, so the edge's disappearance is the empty
// projected input's doing.
func TestAPINested_ProjectedEmptyTargetGetsNoMember(t *testing.T) {
	maskCreate := func(cfg *config.RootConfig) {
		setTableAPI(cfg, "events", config.TableAPIConfig{Operations: &config.Operations{
			Create: new(false), CreateMany: new(false), Upsert: new(false),
		}})
	}

	apiCtx, _ := buildNestedAPI(t, maskCreate)
	if got := apiNestedVerbs(apiUsers(t, apiCtx))["Events"]; !slices.Equal(got, []string{"connect", "disconnect", "clear"}) {
		t.Fatalf("control: Events with create masked = %v, want the three relink verbs", got)
	}

	apiCtx, _ = buildNestedAPI(t, func(cfg *config.RootConfig) {
		maskCreate(cfg)
		tc := cfg.Tables["events"]
		tc.ColumnMap = map[string]config.ColumnOverride{
			"action":  {Access: "hidden"},
			"user_id": {Access: "read_only"},
		}
		cfg.Tables["events"] = tc
	})
	if verbs, ok := apiNestedVerbs(apiUsers(t, apiCtx))["Events"]; ok {
		t.Errorf("Events = %v, want no member once the projected create input is empty", verbs)
	}
}

// TestAPINested_TargetMaskNarrowsTheVerbsThatWriteIt pins the rule on a
// target's `api.operations` mask (PRD §9.9.4). A verb that writes the target
// needs the API operation it writes through: `create` needs `create` (and
// `create_many` wherever the Go executor inserts through CreateMany), and an
// O2M / has-one relink verb needs `update_where`, the operation it rewrites the
// FK with, which also gates `update<T>s(filter, input)`. An M2M relink verb
// writes only the junction, so the target's mask does not reach it.
func TestAPINested_TargetMaskNarrowsTheVerbsThatWriteIt(t *testing.T) {
	tests := []struct {
		name  string
		table string
		mask  config.Operations
		edge  string
		want  []string // nil: no member at all
	}{
		{"update_where off keeps only create on an O2M", "events", config.Operations{UpdateWhere: new(false)}, "Events", []string{"create"}},
		{"update_where off keeps only create on a has-one", "bios", config.Operations{UpdateWhere: new(false)}, "Bio", []string{"create"}},
		{"create off keeps the relink verbs on an O2M", "events", config.Operations{Create: new(false)}, "Events", []string{"connect", "disconnect", "clear"}},
		{"create_many off drops create on an O2M, which inserts through CreateMany", "events", config.Operations{CreateMany: new(false)}, "Events", []string{"connect", "disconnect", "clear"}},
		{"create_many off keeps create on a has-one, which inserts through Create", "bios", config.Operations{CreateMany: new(false)}, "Bio", []string{"create", "connect", "disconnect", "clear"}},
		{"a read-only target has no member", "events", config.Operations{Preset: config.PresetReadOnly}, "Events", nil},
		{"an M2M target's update_where does not reach the junction-only verbs", "categories", config.Operations{UpdateWhere: new(false)}, "Categories", []string{"create", "connect", "disconnect", "clear"}},
		{"an M2M target's create gates create alone", "categories", config.Operations{Create: new(false)}, "Categories", []string{"connect", "disconnect", "clear"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiCtx, _ := buildNestedAPI(t, func(cfg *config.RootConfig) {
				setTableAPI(cfg, tt.table, config.TableAPIConfig{Operations: &tt.mask})
			})
			got, ok := apiNestedVerbs(apiUsers(t, apiCtx))[tt.edge]
			if tt.want == nil {
				if ok {
					t.Errorf("%s = %v, want no member", tt.edge, got)
				}
				return
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("%s verbs = %v, want %v", tt.edge, got, tt.want)
			}
		})
	}
}

// TestAPINested_ParentMaskConjoinsTheBase pins the parent half: each
// `…_with_related` implies the flat operation it composes on the masked set
// (PRD §26.5.1), so masking `create` off the API
// takes `createUserWithRelated` with it. The root-field list the wrapper's
// stub rewriter reads follows the same gates.
func TestAPINested_ParentMaskConjoinsTheBase(t *testing.T) {
	tests := []struct {
		name string
		mask config.Operations
		want []string // nested root fields
	}{
		{"no mask", config.Operations{Preset: config.PresetAll}, []string{"CreateUserWithRelated", "UpdateUserWithRelated", "UpsertUserWithRelated"}},
		{"create off", config.Operations{Create: new(false)}, []string{"UpdateUserWithRelated", "UpsertUserWithRelated"}},
		{"create_with_related off", config.Operations{CreateWithRelated: new(false)}, []string{"UpdateUserWithRelated", "UpsertUserWithRelated"}},
		{"update off", config.Operations{Update: new(false)}, []string{"CreateUserWithRelated", "UpsertUserWithRelated"}},
		{"upsert off", config.Operations{Upsert: new(false)}, []string{"CreateUserWithRelated", "UpdateUserWithRelated"}},
		{"read-only", config.Operations{Preset: config.PresetReadOnly}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiCtx, _ := buildNestedAPI(t, func(cfg *config.RootConfig) {
				setTableAPI(cfg, "users", config.TableAPIConfig{Operations: &tt.mask})
			})
			_, mutations := gen.CollectSqlgenRootFields(apiCtx)
			var got []string
			for _, m := range mutations {
				if strings.HasSuffix(m.GoName, "WithRelated") {
					got = append(got, m.GoName)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("nested root fields = %v, want %v", got, tt.want)
			}
			if tt.want == nil && apiUsers(t, apiCtx).Nested != nil {
				t.Errorf("users carries a nested projection with every family masked off")
			}
		})
	}
}

// TestAPINested_ConflictTargetsProjectAccess pins PRD §32.3: the
// `<T>ConflictTarget` enum declares a unique target only when every one of its
// columns is API-writable, since ON CONFLICT can fire only on a value the
// caller supplies. `users` gains a nullable UNIQUE `api_key_hash` under each
// §32.2 role: `public` and `write_only` keep the target, `read_only`, `hidden`
// and `internal` drop it. The PK target is exempt, including over a
// `read_only` PK column. With an app-enforced `primary_key.columns` key there
// is no PK target, so a dropped unique target leaves the enum empty and the
// upsert mutation is dropped — the Go client keeps UpsertWithRelated and the
// build does not fail the completeness lint.
func TestAPINested_ConflictTargetsProjectAccess(t *testing.T) {
	tests := []struct {
		name        string
		role        string
		pkRole      string // access role on `id`; empty leaves the default
		appEnforced bool
		want        []string // enum values; nil means no upsert mutation
	}{
		{"public", config.AccessPublic, "", false, []string{"API_KEY_HASH", "PK"}},
		{"read_only", config.AccessReadOnly, "", false, []string{"PK"}},
		{"write_only", config.AccessWriteOnly, "", false, []string{"API_KEY_HASH", "PK"}},
		{"hidden", config.AccessHidden, "", false, []string{"PK"}},
		{"internal", config.AccessInternal, "", false, []string{"PK"}},
		{"read_only, read_only PK", config.AccessReadOnly, config.AccessReadOnly, false, []string{"PK"}},
		{"public, read_only PK", config.AccessPublic, config.AccessReadOnly, false, []string{"API_KEY_HASH", "PK"}},
		{"public, app-enforced key", config.AccessPublic, "", true, []string{"API_KEY_HASH"}},
		{"read_only, app-enforced key", config.AccessReadOnly, "", true, nil},
		{"write_only, app-enforced key", config.AccessWriteOnly, "", true, []string{"API_KEY_HASH"}},
		{"hidden, app-enforced key", config.AccessHidden, "", true, nil},
		{"internal, app-enforced key", config.AccessInternal, "", true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := nestedSchema()
			users := &schema.Tables[slices.IndexFunc(schema.Tables, func(tb parser.Table) bool { return tb.Name == "users" })]
			users.Columns = append(users.Columns, parser.Column{Name: "api_key_hash", Type: "text", Nullable: true, Unique: true})
			tcfg := config.TableConfig{
				Relationships: nestedUserRelationships(),
				ColumnMap:     map[string]config.ColumnOverride{"api_key_hash": {Access: tt.role}},
			}
			if tt.pkRole != "" {
				tcfg.ColumnMap["id"] = config.ColumnOverride{Access: tt.pkRole}
			}
			if tt.appEnforced {
				users.Columns[0].PrimaryKey = false
				tcfg.PrimaryKey = &config.TablePrimaryKeyConfig{Columns: []string{"id"}}
			}

			input := apiTestInput(t, schema)
			cfg := input.Config
			cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
			cfg.Tables["users"] = tcfg
			contexts, err := gen.BuildTableContextsFromSchema(input.Schema, cfg)
			if err != nil {
				t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
			}
			apiCtx, err := gen.BuildAPIContext(contexts, nil, nil, nil, cfg)
			if err != nil {
				t.Fatalf("BuildAPIContext() error: %v", err)
			}
			at := apiUsers(t, apiCtx)
			tc := contexts[slices.IndexFunc(contexts, func(c gen.TableContext) bool { return c.TableName == "users" })]
			if tc.Nested == nil || !tc.Nested.EmitUpsert {
				t.Fatalf("control: the Go client emits no UpsertWithRelated for users (role %s)", tt.role)
			}
			if err := gen.ValidateAPINestedCompleteness(tc, *at); err != nil {
				t.Errorf("ValidateAPINestedCompleteness() = %v, want nil", err)
			}

			var got []string
			if at.Nested != nil && at.Nested.EmitUpsert {
				for _, c := range at.Nested.ConflictTargets {
					got = append(got, c.EnumValue)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("UserConflictTarget values = %v, want %v", got, tt.want)
			}

			schemaOut := renderAPITableSchema(t, *at)
			_, mutations := gen.CollectSqlgenRootFields(apiCtx)
			rooted := slices.ContainsFunc(mutations, func(m gen.APIRootField) bool { return m.GoName == "UpsertUserWithRelated" })
			declared := strings.Contains(schemaOut, "enum UserConflictTarget")
			if emitted := tt.want != nil; rooted != emitted || declared != emitted {
				t.Errorf("upsertUserWithRelated root field = %v, enum declared = %v; want both %v", rooted, declared, emitted)
			}
			// A hidden or internal column has no other schema surface, so any
			// mention of it is the conflict-target enum naming it.
			if (tt.role == config.AccessHidden || tt.role == config.AccessInternal) && strings.Contains(schemaOut, "API_KEY_HASH") {
				t.Errorf("schema names the %s column:\n%s", tt.role, schemaOut)
			}
		})
	}
}

// TestAPINested_CompositeConflictTargetNeedsEveryColumnWritable pins the
// "every one of its columns" half of the conflict-target rule (PRD §32.3, §32.5): a
// composite UNIQUE target is dropped when any one of its columns is not
// API-writable, whichever position that column holds. It is the shape PRD
// §32.5 names for tenancy, where a `read_only` or `internal` tenant column
// drops a `UNIQUE(tenant, email)` target.
func TestAPINested_CompositeConflictTargetNeedsEveryColumnWritable(t *testing.T) {
	tests := []struct {
		name      string
		emailRole string
		keyRole   string
		want      []string
	}{
		{"both public", config.AccessPublic, config.AccessPublic, []string{"EMAIL_API_KEY", "PK"}},
		{"first column read_only", config.AccessReadOnly, config.AccessPublic, []string{"PK"}},
		{"second column internal", config.AccessPublic, config.AccessInternal, []string{"PK"}},
		{"second column write_only", config.AccessPublic, config.AccessWriteOnly, []string{"EMAIL_API_KEY", "PK"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := nestedSchema()
			users := &schema.Tables[slices.IndexFunc(schema.Tables, func(tb parser.Table) bool { return tb.Name == "users" })]
			users.Columns = append(users.Columns, parser.Column{Name: "api_key", Type: "text"})
			users.Constraints = append(users.Constraints, parser.Constraint{
				Name: "users_email_api_key_uq", Type: parser.Unique, Columns: []string{"email", "api_key"},
			})

			input := apiTestInput(t, schema)
			cfg := input.Config
			cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
			cfg.Tables["users"] = config.TableConfig{
				Relationships: nestedUserRelationships(),
				ColumnMap: map[string]config.ColumnOverride{
					"email":   {Access: tt.emailRole},
					"api_key": {Access: tt.keyRole},
				},
			}
			contexts, err := gen.BuildTableContextsFromSchema(input.Schema, cfg)
			if err != nil {
				t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
			}
			apiCtx, err := gen.BuildAPIContext(contexts, nil, nil, nil, cfg)
			if err != nil {
				t.Fatalf("BuildAPIContext() error: %v", err)
			}
			at := apiUsers(t, apiCtx)
			if at.Nested == nil || !at.Nested.EmitUpsert {
				t.Fatal("upsertUserWithRelated not emitted; the PK target should keep it")
			}
			got := make([]string, 0, len(at.Nested.ConflictTargets))
			for _, c := range at.Nested.ConflictTargets {
				got = append(got, c.EnumValue)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("UserConflictTarget values = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestAPIFlatUpsert_RequiresThePKConflictTarget pins that the flat
// `upsert<T>` mutation hard-codes `<T>ConflictPK` (PRD §26.5.1), and §9.5
// emits that constant only when an index backs the key. A key declared only
// through `primary_key.columns` gets no constant, so the flat mutation, its
// root field, its M method and its seed are all omitted, and the create input
// follows its mutations. The nested upsert keeps any target the Go client
// emits, so on such a table with a UNIQUE column it survives with a
// required `conflictTarget` — unless the access projection leaves that
// column no API-writable target, in which case the nested upsert and the
// create input it would keep go too. A nested upsert that stands alone must not
// open an empty flat `extend type Mutation` block, which gqlgen rejects.
//
// One shape keeps an orphan `Create<T>Input`, and the user accepted it as
// harmless (2026-09-29): the input is settled before the edge projection, so
// when `api.enabled: false` on the targets then takes every nested edge off
// the API, the nested upsert goes but the input stays, referenced by nothing.
// gqlgen allows an unused input.
//
// Each case builds `users` the way cli.applyPrimaryKeyOverrides leaves it: the
// override is resolved onto the column flag, and the config still names it.
func TestAPIFlatUpsert_RequiresThePKConflictTarget(t *testing.T) {
	const (
		flatField        = "upsertUser(input: CreateUserInput!): User!"
		nestedWithPK     = "upsertUserWithRelated(input: UpsertUserWithRelatedInput!, conflictTarget: UserConflictTarget! = PK): User!"
		nestedNoDefault  = "upsertUserWithRelated(input: UpsertUserWithRelatedInput!, conflictTarget: UserConflictTarget!): User!"
		createInputBlock = "input CreateUserInput {"
	)
	upsertOnly := &config.Operations{
		Create: new(false), CreateMany: new(false), Update: new(false), UpdateWhere: new(false),
		HardDelete: new(false), SoftDelete: new(false),
	}
	tests := []struct {
		name        string
		appEnforced bool   // declare the key through primary_key.columns
		uniqueID    bool   // an inline UNIQUE on id backs the override
		apiKey      string // add a nullable UNIQUE api_key column with this access role; empty adds none
		edgesOff    bool   // take every nested target off the API, so each edge drops
		mask        *config.Operations
		wantFlat    bool
		wantNested  string // the nested upsert's schema line; empty means none
		wantInput   bool
	}{
		{name: "schema-declared key", wantFlat: true, wantNested: nestedWithPK, wantInput: true},
		{name: "app-enforced key", appEnforced: true, wantInput: true},
		{name: "app-enforced key, UNIQUE column", appEnforced: true, apiKey: config.AccessPublic, wantNested: nestedNoDefault, wantInput: true},
		{name: "override backed by an inline UNIQUE", appEnforced: true, uniqueID: true, wantFlat: true, wantNested: nestedWithPK, wantInput: true},
		{name: "upsert-only mask, app-enforced key", appEnforced: true, mask: upsertOnly},
		{name: "upsert-only mask, app-enforced key, UNIQUE column", appEnforced: true, apiKey: config.AccessPublic, mask: upsertOnly, wantNested: nestedNoDefault, wantInput: true},
		{name: "upsert-only mask, app-enforced key, read_only UNIQUE column", appEnforced: true, apiKey: config.AccessReadOnly, mask: upsertOnly},
		// The accepted orphan: CreateUserInput is declared with no mutation.
		{name: "upsert-only mask, app-enforced key, UNIQUE column, every edge off the API", appEnforced: true, apiKey: config.AccessPublic, edgesOff: true, mask: upsertOnly, wantInput: true},
		{name: "upsert-only mask, schema-declared key", mask: upsertOnly, wantFlat: true, wantNested: nestedWithPK, wantInput: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := nestedSchema()
			users := &schema.Tables[slices.IndexFunc(schema.Tables, func(tb parser.Table) bool { return tb.Name == "users" })]
			users.Columns[0].Unique = tt.uniqueID
			tcfg := config.TableConfig{Relationships: nestedUserRelationships()}
			if tt.apiKey != "" {
				users.Columns = append(users.Columns, parser.Column{Name: "api_key", Type: "text", Nullable: true, Unique: true})
				tcfg.ColumnMap = map[string]config.ColumnOverride{"api_key": {Access: tt.apiKey}}
			}
			if tt.appEnforced {
				tcfg.PrimaryKey = &config.TablePrimaryKeyConfig{Columns: []string{"id"}}
			}
			if tt.mask != nil {
				tcfg.API = &config.TableAPIConfig{Operations: tt.mask}
			}

			input := apiTestInput(t, schema)
			cfg := input.Config
			cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
			cfg.Tables["users"] = tcfg
			if tt.edgesOff {
				for _, target := range []string{"events", "orders", "profiles", "bios", "blobs", "categories"} {
					cfg.Tables[target] = config.TableConfig{API: &config.TableAPIConfig{Enabled: new(false)}}
				}
			}
			contexts, err := gen.BuildTableContextsFromSchema(input.Schema, cfg)
			if err != nil {
				t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
			}
			apiCtx, err := gen.BuildAPIContext(contexts, nil, nil, nil, cfg)
			if err != nil {
				t.Fatalf("BuildAPIContext() error: %v", err)
			}
			apiCtx.ModelsPackage = "models"
			apiCtx.GqlgenModelAlias = "gqlmodel"
			apiCtx.SqlgenResolverPkgName = "sqlgenresolver"
			apiCtx.ClientName = "Client"
			at := apiUsers(t, apiCtx)
			if at.HasConflictPK != tt.wantFlat {
				t.Errorf("HasConflictPK = %v, want %v", at.HasConflictPK, tt.wantFlat)
			}

			schemaOut := renderAPITableSchema(t, *at)
			_, mutations := gen.CollectSqlgenRootFields(apiCtx)
			tmpl := loadAPIAllTemplates(t)
			rendered := func(name string) string {
				var buf strings.Builder
				if err := tmpl.ExecuteTemplate(&buf, name, apiCtx); err != nil {
					t.Fatalf("rendering %s: %v", name, err)
				}
				return buf.String()
			}
			resolvers, seeds := rendered("api/resolvers"), rendered("api/seeds")
			flat := []struct {
				surface string
				got     bool
			}{
				{"schema field", strings.Contains(schemaOut, flatField)},
				{"root field", slices.ContainsFunc(mutations, func(m gen.APIRootField) bool { return m.GoName == "UpsertUser" })},
				{"M method", strings.Contains(resolvers, "func (m *M) UpsertUser(")},
				{"seed", strings.Contains(seeds, "func (r *mutationResolver) UpsertUser(")},
				{"UserConflictPK reference", strings.Contains(resolvers, "models.UserConflictPK")},
			}
			for _, f := range flat {
				if f.got != tt.wantFlat {
					t.Errorf("flat upsertUser %s emitted = %v, want %v", f.surface, f.got, tt.wantFlat)
				}
			}

			gotNested := ""
			for _, line := range []string{nestedWithPK, nestedNoDefault} {
				if strings.Contains(schemaOut, line) {
					gotNested = line
				}
			}
			if gotNested != tt.wantNested {
				t.Errorf("nested upsert schema line = %q, want %q", gotNested, tt.wantNested)
			}
			if got := strings.Contains(schemaOut, createInputBlock); got != tt.wantInput || at.HasCreateInput != tt.wantInput {
				t.Errorf("CreateUserInput declared = %v, HasCreateInput = %v; want both %v", got, at.HasCreateInput, tt.wantInput)
			}
			if strings.Contains(schemaOut, "extend type Mutation {\n}") {
				t.Errorf("schema opens an empty `extend type Mutation` block:\n%s", schemaOut)
			}
		})
	}
}

// TestAPIFlatUpsert_WarnsOnAnExplicitMask pins the ruling that
// an explicit mask is not silently overridden: when an explicit per-table `api.operations` mask asks for `upsert`
// on a table that emits no `<T>ConflictPK`, the flat mutation is still
// omitted, but it says so. The scope is §26.5.1's over-reach warning: a global
// mask and a per-table mask that leaves upsert unset stay silent. Both
// `generate` (GenerateInto) and `validate` (ValidateGeneration) warn.
//
// The two shapes get different warnings, and each gets exactly one. The UNIQUE
// email leaves the client Upsert over a unique target, with no PK target for
// the flat mutation: that is this warning. The app-enforced key with no UNIQUE
// beside it has no conflict target at all, so generation resolves its client
// Upsert off, and the over-reach warning names the missing fact instead.
func TestAPIFlatUpsert_WarnsOnAnExplicitMask(t *testing.T) {
	const (
		flat      = "tables.users.api.operations.upsert: the API cannot expose upsertUser because the table emits no UserConflictPK (its primary key has no index the generator can see; PRD §9.5) — the flat mutation is omitted (PRD §26.5.1)"
		overReach = "tables.users.api.operations.upsert: the API cannot expose \"upsert\" because the table's client does not generate it — the table has no conflict target; the key is ignored (the mask is subtractive; PRD §26.5.1)"
	)
	tests := []struct {
		name        string
		appEnforced bool
		uniqueEmail bool   // an inline UNIQUE on email leaves the client a non-PK conflict target
		tableMask   string // one `tables.users.api.operations` entry; empty sets no mask
		globalMask  bool   // `api.operations: {upsert: true}`
		apiDisabled bool   // tables.users.api.enabled: false
		want        string // the one upsert warning expected; empty for none
	}{
		{name: "explicit upsert, app-enforced key, no conflict target at all", appEnforced: true, tableMask: "upsert: true", want: overReach},
		{name: "explicit upsert, app-enforced key, UNIQUE column", appEnforced: true, uniqueEmail: true, tableMask: "upsert: true", want: flat},
		{name: "explicit upsert, schema-declared key", tableMask: "upsert: true"},
		{name: "explicit mask leaving upsert unset, app-enforced key", appEnforced: true, tableMask: "create: true"},
		{name: "explicit upsert: false, app-enforced key", appEnforced: true, tableMask: "upsert: false"},
		{name: "global mask upsert, app-enforced key", appEnforced: true, globalMask: true},
		{name: "no mask, app-enforced key", appEnforced: true},
		{name: "explicit upsert, table off the API", appEnforced: true, tableMask: "upsert: true", apiDisabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			build := func() (*parser.Schema, *config.RootConfig) {
				body := `version: v1
input:
  dialect: postgres
  paths:
    - ./schema.sql
output:
  driver: pgx
  dir: ./models
  package: models
api:
  enabled: true
  graphql:
    enabled: true
    schema_dir: ./models/graph
    resolver_dir: ./models/graph
    package: graph
    field_casing: camel_case
`
				if tt.globalMask {
					body += "  operations:\n    upsert: true\n"
				}
				body += "tables:\n  users:\n"
				if tt.appEnforced {
					body += "    primary_key:\n      columns: [id]\n"
				}
				if tt.apiDisabled || tt.tableMask != "" {
					body += "    api:\n"
					if tt.apiDisabled {
						body += "      enabled: false\n"
					}
					if tt.tableMask != "" {
						body += "      operations:\n        " + tt.tableMask + "\n"
					}
				}
				cfgPath := filepath.Join(t.TempDir(), "sqlgen.yml")
				if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
					t.Fatalf("writing config: %v", err)
				}
				cfg, err := config.LoadConfig(cfgPath)
				if err != nil {
					t.Fatalf("LoadConfig: %v", err)
				}
				// The app-enforced key reaches gen the way
				// cli.applyPrimaryKeyOverrides leaves it: resolved onto the
				// column flag, with the config still naming it.
				return &parser.Schema{Tables: []parser.Table{{
					Name: "users", Schema: "public",
					Columns: []parser.Column{
						{Name: "id", Type: "uuid", PrimaryKey: true},
						{Name: "email", Type: "text", Unique: tt.uniqueEmail},
					},
				}}}, cfg
			}

			schema, cfg := build()
			validateWarnings, err := gen.ValidateGeneration(schema, cfg)
			if err != nil {
				t.Fatalf("ValidateGeneration() error: %v", err)
			}
			schema, cfg = build()
			result, err := gen.GenerateInto(schema, cfg, "test", t.TempDir())
			if err != nil {
				t.Fatalf("GenerateInto() error: %v", err)
			}
			for _, got := range []struct {
				cmd      string
				warnings []string
			}{{"validate", validateWarnings}, {"generate", result.Warnings}} {
				n := 0
				for _, w := range got.warnings {
					if strings.Contains(w, "api.operations.upsert") {
						n++
						if w != tt.want {
							t.Errorf("%s: warning = %q, want %q", got.cmd, w, tt.want)
						}
					}
				}
				wantN := 0
				if tt.want != "" {
					wantN = 1
				}
				if n != wantN {
					t.Errorf("%s: %d flat-upsert warnings in %q, want %d", got.cmd, n, got.warnings, wantN)
				}
			}
		})
	}
}

// TestAPINestedSchema_FlatSurfaceIsByteIdentical pins §26.5.1's additivity:
// with nested mutations on, everything the table schema emitted before —
// including `createUser`, `updateUser` and `upsertUser` — is a byte-identical
// prefix of what it emits now.
func TestAPINestedSchema_FlatSurfaceIsByteIdentical(t *testing.T) {
	on, _ := buildNestedAPI(t, nil)
	off, _ := buildNestedAPI(t, func(cfg *config.RootConfig) {
		cfg.Generation.NestedMutations = nil
	})
	with := renderAPITableSchema(t, *apiUsers(t, on))
	without := renderAPITableSchema(t, *apiUsers(t, off))

	if !strings.HasPrefix(with, without) {
		t.Fatalf("the flat surface moved when nested mutations were enabled\n--- without ---\n%s\n--- with ---\n%s", without, with)
	}
	mustContainAll(t, with[len(without):],
		"createUserWithRelated(input: CreateUserWithRelatedInput!): User!",
		"updateUserWithRelated(id: UUID!, input: UpdateUserWithRelatedInput!): User!",
		"upsertUserWithRelated(input: UpsertUserWithRelatedInput!, conflictTarget: UserConflictTarget! = PK): User!",
	)
}

// TestAPINestedSchema_EachShapeProjects pins the schema forms the Go types
// project onto: a has-one verb takes one value (the Go block's pointer), a
// list-shaped one a non-null-element list; the update wrapper's flat half is
// nullable so a nested-only call can omit it; a nested child input drops the
// traversed FK; and a `filter:` edge is absent. The discriminator's elision is
// inherited from the Go child input the projection intersects, and the E2E
// example pins it on the wire.
func TestAPINestedSchema_EachShapeProjects(t *testing.T) {
	apiCtx, _ := buildNestedAPI(t, nil)
	out := renderAPITableSchema(t, *apiUsers(t, apiCtx))

	mustContainAll(t, out,
		"input CreateUserWithRelatedInput {\n  user: CreateUserInput!\n",
		"input UpdateUserWithRelatedInput {\n  user: UpdateUserInput\n",
		"input UserProfileCreateNested {\n  create: UserProfileCreateInput\n}",
		"input UserBioUpdateNested {\n  clear: Boolean\n  create: UserBioCreateInput\n  connect: UUID\n  disconnect: UUID\n}",
		"input UserEventsUpdateNested {\n  clear: Boolean\n  create: [UserEventsCreateInput!]\n  connect: [UUID!]\n  disconnect: [UUID!]\n}",
		"input UserCategoriesCreateNested {\n  create: [CreateCategoryInput!]\n  connect: [Int!]\n}",
		"input UserOrdersUpdateNested {\n  create: [UserOrdersCreateInput!]\n}",
		"enum UserConflictTarget {\n  PK\n}",
	)
	mustNotContain(t, out, "UserArchived", "UserNamedTags")

	child := nestedSchemaBlock(t, out, "input UserEventsCreateInput {")
	mustContain(t, child, "action: String!")
	if strings.Contains(child, "userID:") {
		t.Errorf("UserEventsCreateInput declares the traversed FK, which the edge sets itself:\n%s", child)
	}
}

// nestedSchemaBlock returns one `{ … }` block of a rendered schema.
func nestedSchemaBlock(t *testing.T, out, header string) string {
	t.Helper()
	start := strings.Index(out, header)
	if start < 0 {
		t.Fatalf("schema has no %q\n%s", header, out)
	}
	end := strings.Index(out[start:], "\n}")
	return out[start : start+end+2]
}

// TestAPINestedSchema_EveryEmittedTypeIsClaimed holds the GraphQL name
// registry against the template: every type the nested
// schema declares must be one the registry claims for the same table, or a
// collision on it would reach gqlparser naming neither claimant.
func TestAPINestedSchema_EveryEmittedTypeIsClaimed(t *testing.T) {
	apiCtx, _ := buildNestedAPI(t, nil)
	claims := gen.ClaimedGraphQLNamesForTest(apiCtx)
	for _, name := range declaredTypeNames(t, renderAPITableSchema(t, *apiUsers(t, apiCtx))) {
		owner, ok := claims[name]
		if !ok {
			t.Errorf("users emits type %q, which the GraphQL name registry does not claim", name)
			continue
		}
		if !strings.Contains(owner, "users") {
			t.Errorf("type %q is claimed by %s, want a claim on public.users", name, owner)
		}
	}
}

// TestAPINestedTemplates_EveryEmittedSurfaceParses renders the three Go files
// the projection adds to — the input translators, the M methods and the
// resolver seeds — and parses each. The E2E example compiles them against
// gqlgen; this is the local gate, since that one is skipped under -short.
func TestAPINestedTemplates_EveryEmittedSurfaceParses(t *testing.T) {
	apiCtx, _ := buildNestedAPI(t, nil)
	apiCtx.ModelsPackage = "models"
	apiCtx.GqlgenModelAlias = "gqlmodel"
	apiCtx.SqlgenResolverPkgName = "sqlgenresolver"
	apiCtx.ClientName = "Client"

	tmpl := loadAPIAllTemplates(t)
	for _, name := range []string{"api/input-translate", "api/resolvers", "api/seeds"} {
		t.Run(name, func(t *testing.T) {
			var buf strings.Builder
			if err := tmpl.ExecuteTemplate(&buf, name, apiCtx); err != nil {
				t.Fatalf("rendering %s: %v", name, err)
			}
			src := "package p\n\n" + buf.String()
			if _, err := goparser.ParseFile(token.NewFileSet(), name+".go", src, goparser.SkipObjectResolution); err != nil {
				t.Fatalf("%s does not parse: %v\n%s", name, err, src)
			}
			if !strings.Contains(src, "UserWithRelated") {
				t.Errorf("%s renders no nested surface for users", name)
			}
		})
	}
}

// TestAPIGoFieldOverrides_NestedInputs pins the fieldName entries the nested
// translators depend on (PRD §26.5.6): every member derived from the
// consumer's names — the flat-input member, each edge member, each child-input
// column — and none for the literal verbs.
func TestAPIGoFieldOverrides_NestedInputs(t *testing.T) {
	apiCtx, _ := buildNestedAPI(t, nil)
	over := gen.APIGoFieldOverrides(apiCtx)

	for _, c := range []struct{ typ, field, want string }{
		{"CreateUserWithRelatedInput", "user", "User"},
		{"CreateUserWithRelatedInput", "events", "Events"},
		{"UpdateUserWithRelatedInput", "categories", "Categories"},
		{"UpsertUserWithRelatedInput", "bio", "Bio"},
		{"UserEventsCreateInput", "action", "Action"},
	} {
		if got := over[c.typ][c.field].FieldName; got != c.want {
			t.Errorf("%s.%s fieldName = %q, want %q", c.typ, c.field, got, c.want)
		}
	}
	if fields, ok := over["UserEventsUpdateNested"]; ok {
		t.Errorf("UserEventsUpdateNested carries overrides %v; its members are literal verbs", fields)
	}
}

// TestValidateAPINestedCompleteness_Rejects walks the lint's refusals over the
// real users projection, each perturbed in one place. Every case is a schema
// that would advertise something no translator or executor honours, or a
// document gqlgen rejects.
func TestValidateAPINestedCompleteness_Rejects(t *testing.T) {
	tests := []struct {
		name    string
		perturb func(t *testing.T, n *gen.APINestedContext)
		want    string
	}{
		{"a verb the Go edge does not carry", func(t *testing.T, n *gen.APINestedContext) {
			edgeNamed(t, n, "Orders").HasConnect = true
		}, "advertises `connect`"},
		{"two members of one wrapper sharing a name", func(t *testing.T, n *gen.APINestedContext) {
			edgeNamed(t, n, "Events").GraphQLName = "user"
		}, `named "user"`},
		{"a child member the Go child input does not have", func(t *testing.T, n *gen.APINestedContext) {
			e := edgeNamed(t, n, "Events")
			e.ChildFields = append(e.ChildFields, e.ChildFields[0])
			e.ChildFields[len(e.ChildFields)-1].Input.GraphQLName = "userID"
			e.ChildFields[len(e.ChildFields)-1].Input.ModelFieldName = "UserID"
		}, "does not have"},
		{"a create-side block with no create-side verb", func(t *testing.T, n *gen.APINestedContext) {
			e := edgeNamed(t, n, "Events")
			e.HasCreate, e.HasConnect = false, false
		}, "empty create-side block"},
		{"two conflict targets on one enum value", func(_ *testing.T, n *gen.APINestedContext) {
			n.ConflictTargets = append(n.ConflictTargets, gen.APINestedConflictTarget{EnumValue: "PK", ConstantName: "UserConflictEmail"})
		}, "both project onto"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiCtx, tables := buildNestedAPI(t, nil)
			at := *apiUsers(t, apiCtx)
			if err := gen.ValidateAPINestedCompleteness(tables["users"], at); err != nil {
				t.Fatalf("control: the unperturbed projection is rejected: %v", err)
			}
			n := *at.Nested
			n.Edges = slices.Clone(n.Edges)
			tt.perturb(t, &n)
			at.Nested = &n
			err := gen.ValidateAPINestedCompleteness(tables["users"], at)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ValidateAPINestedCompleteness() = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

func edgeNamed(t *testing.T, n *gen.APINestedContext, field string) *gen.APINestedEdge {
	t.Helper()
	for i := range n.Edges {
		if n.Edges[i].FieldName == field {
			return &n.Edges[i]
		}
	}
	t.Fatalf("the users projection has no nested edge %q", field)
	return nil
}

// incrementNestedSchema is a parent with an incrementable column, which the
// users fixture lacks: `accounts.balance` gives Update<Account>Input its
// `_inc` / `_dec` pair, so the update mutation renders its transaction branch.
func incrementNestedSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "accounts", Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "balance", Type: "integer"},
				},
			},
			{
				Name: "ledger_entries", Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "account_id", Type: "uuid", Nullable: true},
					{Name: "note", Type: "text"},
				},
			},
		},
	}
}

// TestAPINestedTemplates_IncrementBranch renders the one branch no users-based
// fixture reaches: an update wrapper whose flat half carries `_inc` / `_dec`.
// It pins the ruling (the increments run in the nested mutation's own
// transaction) and the read-back's hook skip, which keeps the cache from
// answering the mutation with the pre-mutation row while the writes' own
// invalidation waits for commit (PRD §9.9.6). The E2E example compiles the
// branch; this is the gate `make check` runs.
func TestAPINestedTemplates_IncrementBranch(t *testing.T) {
	input := apiTestInput(t, incrementNestedSchema())
	cfg := input.Config
	cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
	cfg.Tables["accounts"] = config.TableConfig{Relationships: []config.TableRelationship{
		{Name: "Entries", Type: "one_to_many", Table: "ledger_entries", FK: "account_id"},
	}}
	contexts, err := gen.BuildTableContextsFromSchema(input.Schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(contexts, nil, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext() error: %v", err)
	}
	apiCtx.ModelsPackage = "models"
	apiCtx.GqlgenModelAlias = "gqlmodel"
	apiCtx.SqlgenResolverPkgName = "sqlgenresolver"
	apiCtx.ClientName = "Client"

	tmpl := loadAPIAllTemplates(t)
	render := func(name string) string {
		t.Helper()
		var buf strings.Builder
		if err := tmpl.ExecuteTemplate(&buf, name, apiCtx); err != nil {
			t.Fatalf("rendering %s: %v", name, err)
		}
		src := "package p\n\n" + buf.String()
		if _, err := goparser.ParseFile(token.NewFileSet(), name+".go", src, goparser.SkipObjectResolution); err != nil {
			t.Fatalf("%s does not parse: %v\n%s", name, err, src)
		}
		return src
	}

	resolvers := render("api/resolvers")
	mustContainAll(t, resolvers,
		"incOps []IncrementOp[models.AccountIncrementColumn]",
		`m.Client.WithTx(ctx, "update_account_with_related_inc", func(ctx context.Context) error {`,
	)
	_, readBack, found := strings.Cut(resolvers, "res, err = m.Client.Accounts().Get(")
	if !found {
		t.Fatalf("the increment branch has no read-back:\n%s", resolvers)
	}
	readBack, _, _ = strings.Cut(readBack, "return err")
	if !strings.Contains(readBack, "o.SkipHooks = true") {
		t.Errorf("the in-transaction read-back does not skip hooks, so the cache can serve it the pre-mutation row:\n%s", readBack)
	}
	if strings.Index(readBack, "o.SkipHooks = true") < strings.Index(readBack, "callOptionsFromHTTP") {
		t.Errorf("the hook skip is applied before the HTTP options, which could turn it back off:\n%s", readBack)
	}

	mustContain(t, render("api/input-translate"),
		"func updateAccountWithRelatedIncrementOps(in gqlmodel.UpdateAccountWithRelatedInput) ([]sqlgenresolver.IncrementOp[models.AccountIncrementColumn], error) {")
	mustContain(t, render("api/seeds"), "incOps, err := updateAccountWithRelatedIncrementOps(input)")
}
