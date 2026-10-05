package gen_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// The relationship filter's GraphQL projection (PRD §26.4 "Relationship (as a
// filter)", §26.5.3 "Relationship filters").
//
// Every model-side `<T>Filter` carries a `*<Target>Filter` member compiled to a
// correlated EXISTS. Without a projection, the consumer's `tasks(filter:
// {status: DONE, assignees: {id: {eq: X}}})` story would stay blocked at the
// API boundary. These tests pin the two emitters that expose it — and,
// critically, that they read ONE slice, so a member cannot reach the schema
// without the translator recursion behind it (the one-field-map invariant
// applied to the relationship half).

// relFilterAPIContext builds the API context for relFilterSchema (defined in
// filter_relationship_test.go), optionally letting the caller narrow the
// config first. The schema carries an O2M, an M2M, an O2O and a second hop,
// which is every shape the projection has to tell apart.
func relFilterAPIContext(t *testing.T, tweak func(cfg *config.RootConfig)) *gen.APIContext {
	t.Helper()
	in := apiTestInput(t, relFilterSchema())
	// Relationship detection is a parser step; the context builders consume
	// its output rather than re-deriving it.
	parser.DetectRelationships(in.Schema)
	if tweak != nil {
		tweak(in.Config)
	}
	tables, views, err := gen.BuildEntityContextsFromSchema(in.Schema, in.Config)
	if err != nil {
		t.Fatalf("BuildEntityContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, views, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	if apiCtx == nil {
		t.Fatal("BuildAPIContext returned nil")
	}
	return apiCtx
}

func filterRelNames(rels []gen.APIFilterRelationship) []string {
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, r.GraphQLName)
	}
	return out
}

// TestAPIFilterRelationships_MemberPerListRelationship is the PRD §26.4 row:
// one nested input field per list relationship, and none for a to-one — the
// API side inherits the model side's §11.1 scoping rather than re-deciding it.
func TestAPIFilterRelationships_MemberPerListRelationship(t *testing.T) {
	apiCtx := relFilterAPIContext(t, nil)
	users := apiTableByName(t, apiCtx, "User")

	want := []gen.APIFilterRelationship{
		{
			GraphQLName: "categories", GoFieldName: "Categories", ModelFieldName: "Categories",
			InputTypeName: "CategoryFilter", TranslatorFunc: "translateCategoryFilter",
		},
		{
			GraphQLName: "notes", GoFieldName: "Notes", ModelFieldName: "Notes",
			InputTypeName: "NoteFilter", TranslatorFunc: "translateNoteFilter",
		},
		{
			GraphQLName: "orders", GoFieldName: "Orders", ModelFieldName: "Orders",
			InputTypeName: "OrderFilter", TranslatorFunc: "translateOrderFilter",
		},
	}
	if diff := cmp.Diff(want, users.FilterRelationships); diff != "" {
		t.Errorf("User filter relationships (-want +got):\n%s", diff)
	}

	// profiles → users is the O2O. It is emitted as an object relationship
	// (asserted below so the exclusion is a real observation, not a vacuous
	// pass on an undetected edge) and contributes no filter member.
	profiles := apiTableByName(t, apiCtx, "Profile")
	var hasO2O bool
	for _, r := range profiles.Relationships {
		if r.TargetType == "User" && !r.IsList {
			hasO2O = true
		}
	}
	if !hasO2O {
		t.Fatal("fixture no longer produces a profiles→users to-one relationship; the exclusion below is vacuous")
	}
	if got := filterRelNames(profiles.FilterRelationships); len(got) != 0 {
		t.Errorf("Profile filter relationships = %v, want none (its only relationship is to-one)", got)
	}
}

// TestAPIFilterRelationships_SchemaEmitsNestedInput pins the schema half: an
// ordinary nested input field, so no new gqlgen machinery is involved.
func TestAPIFilterRelationships_SchemaEmitsNestedInput(t *testing.T) {
	apiCtx := relFilterAPIContext(t, nil)
	out := renderAPITableSchema(t, apiTableByName(t, apiCtx, "User"))

	mustContain(t, out, "input UserFilter {")
	mustContain(t, out, "  categories: CategoryFilter")
	mustContain(t, out, "  notes: NoteFilter")
	mustContain(t, out, "  orders: OrderFilter")

	// Scoped to the filter block, so the assertion says WHERE the field landed
	// and not merely that the document mentions it somewhere — the object type
	// legitimately carries `orders: [Order!]!` a few lines above.
	filterBlock := inputBlock(t, out, "UserFilter")
	for _, want := range []string{"categories: CategoryFilter", "notes: NoteFilter", "orders: OrderFilter"} {
		if !strings.Contains(filterBlock, want) {
			t.Errorf("input UserFilter is missing %q:\n%s", want, filterBlock)
		}
	}
}

// TestAPIFilterRelationships_TranslatorRecursesIntoTarget pins the translator
// half (PRD §26.5.3): recursion into the target's own translator, guarded by
// the same nil check the column fields use — a nil member must contribute no
// EXISTS, which is the model layer's "nil filter ⇒ no where clause" contract
// one level down.
func TestAPIFilterRelationships_TranslatorRecursesIntoTarget(t *testing.T) {
	apiCtx := relFilterAPIContext(t, nil)
	out := renderFilterTranslate(t, apiCtx)

	mustContain(t, out, "if in.Orders != nil {")
	mustContain(t, out, "out.Orders = translateOrderFilter(in.Orders)")
	mustContain(t, out, "if in.Categories != nil {")
	mustContain(t, out, "out.Categories = translateCategoryFilter(in.Categories)")

	// The second hop: an OrderFilter reached through a UserFilter carries its
	// own relationship member, so nesting composes with no extra machinery.
	mustContain(t, out, "out.OrderItems = translateOrderItemFilter(in.OrderItems)")

	// Every function the members dispatch into is emitted by this same file —
	// the per-table translator loop covers every entity on apiCtx.Tables, and
	// a member is only produced for a target that is one of them.
	for _, at := range apiCtx.Tables {
		for _, r := range at.FilterRelationships {
			if !strings.Contains(out, "func "+r.TranslatorFunc+"(") {
				t.Errorf("%sFilter dispatches into %s, which the file does not declare", at.StructName, r.TranslatorFunc)
			}
		}
	}
}

// TestAPIFilterRelationships_APIDisabledTargetDropped is the read-side-channel
// gate (PRD §26.5.3). `EXISTS (SELECT 1 FROM <hidden target> …)` answers a
// yes/no question about rows the schema deliberately does not expose, so a
// relationship into an `api.enabled: false` entity emits no member at all.
func TestAPIFilterRelationships_APIDisabledTargetDropped(t *testing.T) {
	apiCtx := relFilterAPIContext(t, func(cfg *config.RootConfig) {
		cfg.Tables = map[string]config.TableConfig{
			"categories": {API: &config.TableAPIConfig{Enabled: new(false)}},
		}
	})

	users := apiTableByName(t, apiCtx, "User")
	got := filterRelNames(users.FilterRelationships)
	if diff := cmp.Diff([]string{"notes", "orders"}, got); diff != "" {
		t.Errorf("User filter relationships (-want +got):\n%s", diff)
	}

	// The drop is an API decision, not a model one — the Go client keeps the
	// member, because `access` and `api.enabled` never narrow the core client
	// (§32.1's load-bearing principle, which §26.10 follows).
	in := apiTestInput(t, relFilterSchema())
	parser.DetectRelationships(in.Schema)
	tables, err := gen.BuildTableContextsFromSchema(in.Schema, in.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	var found bool
	for _, tc := range tables {
		if tc.TableName != "users" {
			continue
		}
		for _, rf := range tc.RelationshipFilters {
			if rf.FieldName == "Categories" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("model-side users.Categories relationship filter is gone; the API-only nature of the drop is unproven")
	}

	// Positive control first, so neither absence assertion can pass on a
	// surface that emits no relationship members at all.
	block := inputBlock(t, renderAPITableSchema(t, users), "UserFilter")
	if !strings.Contains(block, "orders: OrderFilter") {
		t.Fatalf("UserFilter lost its exposed relationship member too:\n%s", block)
	}
	if strings.Contains(block, "categories: CategoryFilter") {
		t.Errorf("UserFilter still advertises the API-excluded target:\n%s", block)
	}
	translators := renderFilterTranslate(t, apiCtx)
	if !strings.Contains(translators, "translateOrderFilter(in.Orders)") {
		t.Fatal("filter translator lost its exposed relationship recursion too")
	}
	if strings.Contains(translators, "translateCategoryFilter(") {
		t.Error("filter translator still recurses into the API-excluded target's translator")
	}
}

// TestAPIFilterRelationships_UnreadableTargetDropped covers the second half of
// the §26.5.3 gate: a target every §32.2 role dropped from the read surface.
//
// §32.4 makes this unreachable through a validated config — PK columns are
// required to stay `public` / `read_only`, so a real table always keeps one
// readable column. The context builder does not re-run that validation, so the
// branch is reachable here and worth pinning: the gate the PRD states is
// "excluded from the API", and an entity with no readable field is excluded
// whether the exclusion came from `api.enabled` or from access roles.
func TestAPIFilterRelationships_UnreadableTargetDropped(t *testing.T) {
	apiCtx := relFilterAPIContext(t, func(cfg *config.RootConfig) {
		cfg.Tables = map[string]config.TableConfig{
			"notes": {ColumnMap: map[string]config.ColumnOverride{
				"id":         {Access: config.AccessHidden},
				"user_id":    {Access: config.AccessHidden},
				"body":       {Access: config.AccessHidden},
				"deleted_at": {Access: config.AccessHidden},
			}},
		}
	})

	users := apiTableByName(t, apiCtx, "User")
	got := filterRelNames(users.FilterRelationships)
	if diff := cmp.Diff([]string{"categories", "orders"}, got); diff != "" {
		t.Errorf("User filter relationships (-want +got):\n%s", diff)
	}
}

// selfRefAPIContext builds an API context over a self-referential O2M — the
// §13.5 shape whose GraphQL input names itself.
func selfRefAPIContext(t *testing.T) *gen.APIContext {
	t.Helper()
	in := apiTestInput(t, &parser.Schema{
		Tables: []parser.Table{{
			Name: "employees",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "manager_id", Type: "uuid", Nullable: true, FKReference: &parser.FKReference{Table: "employees", Column: "id"}},
				{Name: "name", Type: "text"},
			},
		}},
	})
	parser.DetectRelationships(in.Schema)
	tables, views, err := gen.BuildEntityContextsFromSchema(in.Schema, in.Config)
	if err != nil {
		t.Fatalf("BuildEntityContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, views, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	return apiCtx
}

// TestAPIFilterRelationships_SelfReferentialInput pins §13.5 on the API side.
// A recursive input type is legal GraphQL, and a recursive translator function
// is legal Go; what neither may do is recurse without a value driving it. The
// generated body recurses on `in.<Member>`, which comes from a parsed query
// document and is therefore a finite tree — the same property the pre-existing
// and / or recursion relies on.
func TestAPIFilterRelationships_SelfReferentialInput(t *testing.T) {
	apiCtx := selfRefAPIContext(t)
	employees := apiTableByName(t, apiCtx, "Employee")

	if len(employees.FilterRelationships) != 1 {
		t.Fatalf("Employee filter relationships = %v, want exactly the self edge", filterRelNames(employees.FilterRelationships))
	}
	self := employees.FilterRelationships[0]
	if self.InputTypeName != "EmployeeFilter" {
		t.Errorf("InputTypeName = %q, want the input's own name", self.InputTypeName)
	}
	if self.TranslatorFunc != "translateEmployeeFilter" {
		t.Errorf("TranslatorFunc = %q, want the translator's own name", self.TranslatorFunc)
	}

	schema := renderAPITableSchema(t, employees)
	mustContain(t, schema, "  "+self.GraphQLName+": EmployeeFilter")

	// The translator is emitted once and calls itself — not a second,
	// differently-named function that would have to be generated too.
	out := renderFilterTranslate(t, apiCtx)
	if got := strings.Count(out, "func translateEmployeeFilter("); got != 1 {
		t.Fatalf("translateEmployeeFilter declared %d times, want 1", got)
	}
	mustContain(t, out, "out."+self.ModelFieldName+" = translateEmployeeFilter(in."+self.GoFieldName+")")
}

// TestAPIFilterRelationships_CircularTargets pins §13.6: A → B → A resolves to
// two inputs that name each other, which gqlgen accepts and the translator
// pair handles because each recursion step consumes one level of the incoming
// value.
func TestAPIFilterRelationships_CircularTargets(t *testing.T) {
	apiCtx := relFilterAPIContext(t, nil)
	users := apiTableByName(t, apiCtx, "User")
	categories := apiTableByName(t, apiCtx, "Category")

	if !slicesContainsRel(users.FilterRelationships, "CategoryFilter") {
		t.Fatalf("UserFilter does not reach CategoryFilter: %v", filterRelNames(users.FilterRelationships))
	}
	if !slicesContainsRel(categories.FilterRelationships, "UserFilter") {
		t.Fatalf("CategoryFilter does not reach UserFilter: %v", filterRelNames(categories.FilterRelationships))
	}

	out := renderFilterTranslate(t, apiCtx)
	mustContain(t, out, "out.Categories = translateCategoryFilter(in.Categories)")
	mustContain(t, out, "out.Users = translateUserFilter(in.Users)")
}

func slicesContainsRel(rels []gen.APIFilterRelationship, inputType string) bool {
	for _, r := range rels {
		if r.InputTypeName == inputType {
			return true
		}
	}
	return false
}

// TestAPIFilterRelationships_OneDerivationDrivesBothEmitters is the structural
// guard behind every assertion above. The schema field and the translator
// recursion are rendered from the SAME slice, so the advertised-but-dropped
// failure — a field the schema accepts and the translator silently ignores — is
// unrepresentable for relationship members rather than merely absent today.
//
// The completeness lint states the same invariant as a codegen error over
// the column half; this pins the relationship half structurally, which is
// the stronger of the two guarantees.
func TestAPIFilterRelationships_OneDerivationDrivesBothEmitters(t *testing.T) {
	apiCtx := relFilterAPIContext(t, nil)
	translators := renderFilterTranslate(t, apiCtx)

	var checked int
	for _, at := range apiCtx.Tables {
		block := inputBlock(t, renderAPITableSchema(t, at), at.StructName+"Filter")
		for _, r := range at.FilterRelationships {
			checked++
			if !strings.Contains(block, r.GraphQLName+": "+r.InputTypeName) {
				t.Errorf("%sFilter: translator member %q has no schema field", at.StructName, r.GraphQLName)
			}
			if !strings.Contains(translators, "out."+r.ModelFieldName+" = "+r.TranslatorFunc+"(in."+r.GoFieldName+")") {
				t.Errorf("%sFilter: schema field %q has no translator recursion", at.StructName, r.GraphQLName)
			}
		}
		// The reverse direction: no schema field may name a filter input that
		// the member list does not account for. Column comparators are named
		// `<X>Comparator`, so a `<X>Filter` operand can only be a relationship.
		for _, line := range strings.Split(block, "\n") {
			name, typ, found := strings.Cut(strings.TrimSpace(line), ": ")
			if !found || !strings.HasSuffix(typ, "Filter") {
				continue
			}
			if !filterRelDeclared(at.FilterRelationships, name) {
				t.Errorf("%sFilter emits %q: %s with no member behind it", at.StructName, name, typ)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no relationship members inspected; the assertions above are vacuous")
	}
}

func filterRelDeclared(rels []gen.APIFilterRelationship, graphQLName string) bool {
	for _, r := range rels {
		if r.GraphQLName == graphQLName {
			return true
		}
	}
	return false
}

// TestAPIFilterRelationships_GoFieldNamesDictatedToGqlgen pins that Go field
// names are dictated to gqlgen. The translator dereferences `in.<GoFieldName>` on a gqlgen-GENERATED
// struct, so sqlgen must dictate that identifier rather than predict what
// gqlgen's camelizer produces from the GraphQL name.
func TestAPIFilterRelationships_GoFieldNamesDictatedToGqlgen(t *testing.T) {
	apiCtx := relFilterAPIContext(t, nil)
	overrides := gen.APIGoFieldOverrides(apiCtx)

	var checked int
	for _, at := range apiCtx.Tables {
		fields := overrides[at.StructName+"Filter"]
		for _, r := range at.FilterRelationships {
			checked++
			if got := fields[r.GraphQLName].FieldName; got != r.GoFieldName {
				t.Errorf("%sFilter.%s fieldName = %q, want %q", at.StructName, r.GraphQLName, got, r.GoFieldName)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no relationship members inspected; the assertion above is vacuous")
	}
}

// TestAPIFilterRelationships_ViewsCarryNone pins that a view's filter input is
// columns-only. §16.4 gives views no relationships, so there is nothing to
// project — and a view's read staying exactly one query (§25.1) depends on it.
//
// The fixture deliberately puts the view in a project whose TABLES do carry
// members: "no view has members" is only worth asserting where something else
// does, otherwise an empty result proves nothing about views.
func TestAPIFilterRelationships_ViewsCarryNone(t *testing.T) {
	sch := relFilterSchema()
	sch.Views = []parser.View{{Name: "user_summary", Columns: []parser.Column{
		apiViewCol("id", "uuid", false, true),
		apiViewCol("email", "text", false, false),
	}}}
	in := apiTestInput(t, sch)
	parser.DetectRelationships(in.Schema)
	tables, views, err := gen.BuildEntityContextsFromSchema(in.Schema, in.Config)
	if err != nil {
		t.Fatalf("BuildEntityContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, views, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	var sawView, sawTableWithMembers bool
	for _, at := range apiCtx.Tables {
		if !at.IsView {
			sawTableWithMembers = sawTableWithMembers || len(at.FilterRelationships) > 0
			continue
		}
		sawView = true
		if len(at.FilterRelationships) != 0 {
			t.Errorf("view %s carries filter relationships %v, want none", at.StructName, filterRelNames(at.FilterRelationships))
		}
	}
	if !sawView {
		t.Fatal("fixture produced no view; the assertion above is vacuous")
	}
	if !sawTableWithMembers {
		t.Fatal("fixture produced no table with relationship members; an empty view result would prove nothing")
	}

	// The view's rendered filter input carries only comparator operands.
	for _, at := range apiCtx.Tables {
		if !at.IsView {
			continue
		}
		for _, line := range strings.Split(inputBlock(t, renderAPITableSchema(t, at), at.StructName+"Filter"), "\n") {
			if _, typ, found := strings.Cut(strings.TrimSpace(line), ": "); found && strings.HasSuffix(typ, "Filter") {
				t.Errorf("view %s emits a nested filter operand: %s", at.StructName, strings.TrimSpace(line))
			}
		}
	}
}

// relFilterValidateSchema is a two-table fixture with one O2M edge — the
// smallest shape that can carry a relationship `filter:`.
func relFilterValidateSchema() *parser.Schema {
	sch := &parser.Schema{Tables: []parser.Table{
		{Name: "users", Columns: []parser.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "workspace_id", Type: "uuid"},
			{Name: "email", Type: "text"},
		}},
		{Name: "orders", Columns: []parser.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "workspace_id", Type: "uuid"},
			{Name: "user_id", Type: "uuid", FKReference: &parser.FKReference{Table: "users", Column: "id"}},
			{Name: "status", Type: "text"},
		}},
	}}
	parser.DetectRelationships(sch)
	return sch
}

// TestValidateGeneration_WiresRelationshipFilters pins that
// ValidateGeneration performs the same post-build wiring Generate does.
// Skipping it lets `sqlgen validate` report clean for configs
// `sqlgen generate` rejects — exactly the class this function exists to close.
//
// The reproducer is the relationship-filter hard error: a relationship `filter:` containing a
// `$`, which the EXISTS renumbering would consume as a placeholder and shift
// every caller-supplied arg by one. Asserted as an AGREEMENT between the two
// entry points rather than as a literal message, so the test keeps meaning if
// a later pass adds another wiring-stage error.
//
// The ORDER of the mirrored passes — tenancy attached before wiring, because
// each entry reads the target's tenant column — is deliberately NOT asserted
// here. ValidateGeneration returns only warnings and an error, so a wrong
// order produces identical output today and any assertion would be vacuous.
// The fact itself is covered on the generate path by
// TestRelationshipFilters_TargetTenantColumnAttached; here it is stated in the
// call site's comment and nowhere else, which is the honest place for it.
func TestValidateGeneration_WiresRelationshipFilters(t *testing.T) {
	cfg := func() *config.RootConfig {
		return &config.RootConfig{
			Version: "v1",
			Input:   config.InputConfig{Dialect: config.DialectPostgres},
			Output:  config.OutputConfig{Driver: "pgx", Dir: "./models", Package: "models"},
			Tables: map[string]config.TableConfig{
				"users": {Relationships: []config.TableRelationship{{
					Name: "PaidOrders", Type: "one_to_many", Table: "orders", FK: "user_id",
					Filter: "status = $1",
				}}},
			},
		}
	}

	_, genErr := gen.BuildTableContextsFromSchema(relFilterValidateSchema(), cfg())
	if genErr == nil {
		t.Fatal("the generate path no longer rejects a `$`-bearing relationship filter; this test's premise is gone")
	}

	_, valErr := gen.ValidateGeneration(relFilterValidateSchema(), cfg())
	if valErr == nil {
		t.Fatalf("ValidateGeneration passed a config generate rejects with: %v", genErr)
	}
	if !strings.Contains(valErr.Error(), "relationship filters") {
		t.Errorf("ValidateGeneration error = %v, want it to name the relationship-filter stage", valErr)
	}
}
