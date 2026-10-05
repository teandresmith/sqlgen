package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	parserpkg "github.com/teandresmith/sqlgen/parser"
)

// A schema enum whose PascalCase name lands on a name sqlgen already
// owns in the GraphQL type namespace.
//
// Two failures shared one cause. `<Enum>Comparator` colliding with a shipped
// family made comparatorOperatorsFor short-circuit on the SHIPPED operand set,
// so an enum column advertised `gt` / `lt` / `between` that its Enum translator
// drops — valid schema, compiling Go, silently ignored operators. And the
// enum's own GraphQL name collides with the scalars and object types the same
// document declares, which sqlgen cannot rename away.

// collisionSchema builds a one-table postgres fixture whose enum is named
// enumName, optionally alongside an `interval` column — the column that makes
// sqlgen declare `scalar Duration` and turns an enum named `duration` into a
// redeclaration.
func collisionSchema(enumName string, withInterval bool) *parserpkg.Schema {
	s := &parserpkg.Schema{
		Enums: []parserpkg.Enum{
			{Name: enumName, Schema: "public", Values: []string{"short", "long"}},
		},
		Tables: []parserpkg.Table{
			{
				Name: "jobs",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "kind", Type: enumName},
					{Name: "prior_kind", Type: enumName, Nullable: true},
				},
			},
		},
	}
	if withInterval {
		s.Tables[0].Columns = append(s.Tables[0].Columns, parserpkg.Column{Name: "elapsed", Type: "interval"})
	}
	return s
}

// collisionContext builds the APIContext for a fixture, applying `enums:`
// overrides. It mirrors enumFixtureContext but surfaces the error instead of
// failing the test, since the error is what most of these cases assert on.
func collisionContext(t *testing.T, schema *parserpkg.Schema, enums map[string]config.EnumConfig) (*gen.APIContext, error) {
	t.Helper()
	in := apiTestInput(t, schema)
	in.Config.Output.Package = "models"
	in.Config.Enums = enums
	collisions := gen.ComputeNameCollisions(schema)
	in.Resolver = enumFixtureResolver(t, schema, in.Config, collisions)
	tables, err := gen.BuildTableContexts(in, collisions)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	return gen.BuildAPIContext(tables, nil, gen.BuildEnumContexts(schema, collisions, in.Config), nil, in.Config)
}

// TestEnumComparator_disambiguatesAgainstFixedFamilies is the silent half of
// the enum name collision. `duration` PascalCases to `Duration`, so the enum's comparator input
// derives `DurationComparator` — a shipped family. Without disambiguation the
// column references that family and inherited its ten `Duration`-typed operands plus a
// `DurationRange`, of which the Enum translator serves four.
func TestEnumComparator_disambiguatesAgainstFixedFamilies(t *testing.T) {
	apiCtx, err := collisionContext(t, collisionSchema("duration", false), nil)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	jobs := apiTableByName(t, apiCtx, "Job")

	if got := fieldBySQLName(t, jobs, "kind").Filter.InputTypeName; got != "DurationEnumComparator" {
		t.Errorf("NOT NULL enum column references %q, want DurationEnumComparator — "+
			"DurationComparator is the shipped family, whose operands are time.Duration", got)
	}
	if got := fieldBySQLName(t, jobs, "prior_kind").Filter.InputTypeName; got != "NullableDurationEnumComparator" {
		t.Errorf("nullable enum column references %q, want NullableDurationEnumComparator", got)
	}

	// The operand set must be comparator.Enum's four, not the shipped
	// family's ten — this is the accept-and-ignore surface itself.
	wantOps := []string{"eq", "neq", "in", "nin"}
	gotOps := make([]string, 0, 4)
	for _, op := range fieldBySQLName(t, jobs, "kind").Filter.Operators {
		gotOps = append(gotOps, op.Name)
		if !strings.Contains(op.Type, "Duration") {
			t.Errorf("operator %q has operand type %q, want the bound GraphQL enum", op.Name, op.Type)
		}
	}
	if strings.Join(gotOps, ",") != strings.Join(wantOps, ",") {
		t.Errorf("enum comparator operators = %v, want %v — the extra ordered/range "+
			"operators are accepted by the schema and dropped by the translator", gotOps, wantOps)
	}

	// The translator follows the input name, and returns the Enum type.
	tr := translatorByName(t, apiCtx, "translateDurationEnumComparator")
	if want := "*comparator.Enum[models.Duration]"; tr.GoReturnType != want {
		t.Errorf("translateDurationEnumComparator returns %q, want %q", tr.GoReturnType, want)
	}

	shared := renderAPISharedSchema(t, apiCtx)
	if !strings.Contains(shared, "input DurationEnumComparator {") {
		t.Errorf("shared schema declares no DurationEnumComparator:\n%s", shared)
	}
	if strings.Contains(shared, "DurationRange") {
		t.Errorf("shared schema declares a DurationRange — an enum has no between/nbetween:\n%s", shared)
	}
}

// TestEnumComparator_disambiguatedFamilyCoexistsWithShipped pins that the
// suffix separates the two families rather than replacing one. `numeric` is the
// case that reaches this without also tripping the enum-vs-scalar rule: the
// NumericComparator operands are `Float`, a spec built-in that is never
// declared as a scalar, so `enum Numeric` collides with nothing.
func TestEnumComparator_disambiguatedFamilyCoexistsWithShipped(t *testing.T) {
	schema := collisionSchema("numeric", false)
	schema.Tables[0].Columns = append(schema.Tables[0].Columns,
		parserpkg.Column{Name: "attempts", Type: "integer"})

	apiCtx, err := collisionContext(t, schema, nil)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	jobs := apiTableByName(t, apiCtx, "Job")

	if got := fieldBySQLName(t, jobs, "kind").Filter.InputTypeName; got != "NumericEnumComparator" {
		t.Errorf("enum column references %q, want NumericEnumComparator", got)
	}
	if got := fieldBySQLName(t, jobs, "attempts").Filter.InputTypeName; got != "NumericComparator" {
		t.Errorf("integer column references %q, want NumericComparator", got)
	}

	// Both translators exist under distinct names with distinct return types.
	// Before the fix one name carried both and the map's last write won.
	enumTr := translatorByName(t, apiCtx, "translateNumericEnumComparator")
	if want := "*comparator.Enum[models.Numeric]"; enumTr.GoReturnType != want {
		t.Errorf("translateNumericEnumComparator returns %q, want %q", enumTr.GoReturnType, want)
	}
	numTr := translatorByName(t, apiCtx, "translateNumericComparatorInt32")
	if !strings.HasPrefix(numTr.GoReturnType, "*comparator.Number[") {
		t.Errorf("translateNumericComparatorInt32 returns %q, want a comparator.Number", numTr.GoReturnType)
	}
}

// TestGraphQLTypeNames_collisionsAreGenerationErrors covers the half the suffix
// cannot reach: names sqlgen derives from the consumer's own SQL identifiers.
func TestGraphQLTypeNames_collisionsAreGenerationErrors(t *testing.T) {
	tests := []struct {
		name     string
		schema   *parserpkg.Schema
		wantFrag []string
	}{
		{
			// The enum's own name against the scalar an interval column
			// declares. Legal Go (`Duration` beside `time.Duration`), illegal
			// GraphQL — gqlparser: "Cannot redeclare type Duration."
			name:     "enum against a declared scalar",
			schema:   collisionSchema("duration", true),
			wantFrag: []string{"enum public.duration", "scalar Duration", "enums.public.duration.struct_name"},
		},
		{
			// Two enums landing on one monomorphized comparator: `duration`
			// disambiguates to DurationEnumComparator, which `duration_enum`
			// derives directly. collectComparatorFamilies would merge them.
			name: "two enums on one comparator input",
			schema: func() *parserpkg.Schema {
				s := collisionSchema("duration", false)
				s.Enums = append(s.Enums, parserpkg.Enum{
					Name: "duration_enum", Schema: "public", Values: []string{"a", "b"},
				})
				s.Tables[0].Columns = append(s.Tables[0].Columns,
					parserpkg.Column{Name: "other", Type: "duration_enum"})
				return s
			}(),
			wantFrag: []string{"enum public.duration", "enum public.duration_enum", "DurationEnumComparator"},
		},
		{
			// A table whose object type lands on a structural name every
			// generated schema declares. `sort_directions` is deliberately not
			// `page_infos`: `PageInfo` is in generatedPackageTypes, so the Go
			// rule rejects it first and the case would prove nothing about the
			// GraphQL namespace. `SortDirection` is declared only in
			// shared_gen.graphqls, so this rule is the only one that sees it.
			name: "table against a GraphQL-only structural type",
			schema: &parserpkg.Schema{
				Tables: []parserpkg.Table{{
					Name: "sort_directions",
					Columns: []parserpkg.Column{
						{Name: "id", Type: "uuid", PrimaryKey: true},
						{Name: "label", Type: "text"},
					},
				}},
			},
			wantFrag: []string{"table sort_directions", "SortDirection", "shared_gen.graphqls"},
		},
		{
			// A table against the emitted comparator surface. `StringComparator`
			// is declared only because the `label` column references it, so
			// this also pins that the reserved set is read off the built
			// context rather than off the static family table.
			name: "table against an emitted comparator input",
			schema: &parserpkg.Schema{
				Tables: []parserpkg.Table{{
					Name: "string_comparators",
					Columns: []parserpkg.Column{
						{Name: "id", Type: "uuid", PrimaryKey: true},
						{Name: "label", Type: "text"},
					},
				}},
			},
			wantFrag: []string{"table string_comparators", "StringComparator", "shared_gen.graphqls"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := collisionContext(t, tt.schema, nil)
			if err == nil {
				t.Fatal("BuildAPIContext() returned no error, want a GraphQL type-name collision")
			}
			for _, frag := range tt.wantFrag {
				if !strings.Contains(err.Error(), frag) {
					t.Errorf("error does not mention %q:\n%v", frag, err)
				}
			}
		})
	}
}

// TestEnumStructNameOverride_resolvesCollision is the remedy path: PRD §4.11's
// `enums:` block renames the enum, and every name derived from it moves
// together — the GraphQL enum, the comparator input, and the translator's Go
// type parameter.
func TestEnumStructNameOverride_resolvesCollision(t *testing.T) {
	apiCtx, err := collisionContext(t, collisionSchema("duration", true), map[string]config.EnumConfig{
		"duration": {StructName: "MediaDuration"},
	})
	if err != nil {
		t.Fatalf("BuildAPIContext() with an enums: override: %v", err)
	}

	jobs := apiTableByName(t, apiCtx, "Job")
	if got := fieldBySQLName(t, jobs, "kind").Filter.InputTypeName; got != "MediaDurationComparator" {
		t.Errorf("overridden enum column references %q, want MediaDurationComparator — "+
			"the renamed enum no longer collides, so no suffix is applied", got)
	}

	tr := translatorByName(t, apiCtx, "translateMediaDurationComparator")
	if want := "*comparator.Enum[models.MediaDuration]"; tr.GoReturnType != want {
		t.Errorf("translator returns %q, want %q — the override must reach the "+
			"Go type parameter, not just the GraphQL name", tr.GoReturnType, want)
	}

	shared := renderAPISharedSchema(t, apiCtx)
	if !strings.Contains(shared, "enum MediaDuration {") {
		t.Errorf("shared schema declares no `enum MediaDuration`:\n%s", shared)
	}
	if !strings.Contains(shared, "scalar Duration") {
		t.Errorf("shared schema no longer declares `scalar Duration` for the interval column:\n%s", shared)
	}
}

// TestEnumComparator_uncollidedNameIsUnchanged is the guard on the enum comparator
// goldens: the suffix must apply only on collision, or every enum comparator in the
// repo renames.
func TestEnumComparator_uncollidedNameIsUnchanged(t *testing.T) {
	apiCtx, err := collisionContext(t, collisionSchema("order_status", false), nil)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	jobs := apiTableByName(t, apiCtx, "Job")

	if got := fieldBySQLName(t, jobs, "kind").Filter.InputTypeName; got != "OrderStatusComparator" {
		t.Errorf("uncolliding enum column references %q, want OrderStatusComparator "+
			"(PRD §26.4's `<EnumName>Comparator`)", got)
	}
	if got := fieldBySQLName(t, jobs, "prior_kind").Filter.InputTypeName; got != "NullableOrderStatusComparator" {
		t.Errorf("uncolliding nullable enum column references %q, want NullableOrderStatusComparator", got)
	}
}

// TestEnumConfig_descriptionOverridesSQLComment covers the other half of
// PRD §4.11: a config description outranks the SQL COMMENT ON TYPE.
func TestEnumConfig_descriptionOverridesSQLComment(t *testing.T) {
	schema := collisionSchema("order_status", false)
	schema.Enums[0].Comment = "from the database"

	tests := []struct {
		name string
		cfg  map[string]config.EnumConfig
		want string
	}{
		{name: "no override keeps the SQL comment", cfg: nil, want: "from the database"},
		{
			name: "override wins",
			cfg:  map[string]config.EnumConfig{"order_status": {Description: "from the config"}},
			want: "from the config",
		},
		{
			name: "schema-qualified key resolves",
			cfg:  map[string]config.EnumConfig{"public.order_status": {Description: "qualified"}},
			want: "qualified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.RootConfig{Enums: tt.cfg}
			cfg.Input.Dialect = config.DialectPostgres
			got := gen.BuildEnumContexts(schema, nil, cfg)
			if len(got) != 1 {
				t.Fatalf("BuildEnumContexts() returned %d contexts, want 1", len(got))
			}
			if got[0].DocComment != tt.want {
				t.Errorf("DocComment = %q, want %q", got[0].DocComment, tt.want)
			}
		})
	}
}

// translatorByName looks up one emitted comparator translator.
func translatorByName(t *testing.T, ctx *gen.APIContext, funcName string) gen.APIComparatorTranslator {
	t.Helper()
	for _, tr := range ctx.ComparatorTranslators {
		if tr.FuncName == funcName {
			return tr
		}
	}
	names := make([]string, 0, len(ctx.ComparatorTranslators))
	for _, tr := range ctx.ComparatorTranslators {
		names = append(names, tr.FuncName)
	}
	t.Fatalf("no translator %q; have %v", funcName, names)
	return gen.APIComparatorTranslator{}
}

// A MySQL SET reaches APIContext.UsedEnums through enumContextsForSets, so it
// participates in the GraphQL namespace — but it is not an enum. BuildSetContexts
// names it straight from the schema and never reads the `enums:` block, so
// offering `enums.<set>.struct_name` as the remedy would send the consumer to a
// key that changes nothing. It also claims no comparator input: a SET column
// filters through comparator.String (TestEnumComparator_setColumnKeepsStringComparator).
//
// The Go-side rule already draws this line — collectResolvedNames gives a SET
// kind "SET type" with no escape — and this keeps the GraphQL rule in step.
func TestGraphQLTypeNames_setIsNotOfferedTheEnumsOverride(t *testing.T) {
	in, sets := setBindingInput(t)
	// The SET's value type is `UsersPermissionsSetValue`; naming a table so it
	// resolves to the same GraphQL type forces the two into one namespace slot.
	in.Schema.Tables = append(in.Schema.Tables, parserpkg.Table{
		Name: "users_permissions_set_values",
		Columns: []parserpkg.Column{
			{Name: "id", Type: "bigint", PrimaryKey: true},
			{Name: "label", Type: "text"},
		},
	})

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	_, err = gen.BuildAPIContext(tables, nil, nil, sets, in.Config)
	if err == nil {
		t.Fatal("BuildAPIContext() returned no error, want a GraphQL type-name collision")
	}

	if !strings.Contains(err.Error(), "SET type users_permissions_set") {
		t.Errorf("error does not name the SET as a SET type:\n%v", err)
	}
	if strings.Contains(err.Error(), "enums.") {
		t.Errorf("error offers an `enums:` override, which BuildSetContexts never reads:\n%v", err)
	}
	// The SET's comparator inputs are never emitted, so it must not claim them.
	if strings.Contains(err.Error(), "UsersPermissionsSetValueComparator") {
		t.Errorf("error claims a comparator input a SET column never references:\n%v", err)
	}
}
