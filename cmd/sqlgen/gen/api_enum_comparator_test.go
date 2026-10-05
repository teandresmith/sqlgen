package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	parserpkg "github.com/teandresmith/sqlgen/parser"
)

// Enum comparators, monomorphized per enum (PRD §26.4 Rule 1). Previously an
// enum column advertised `StringComparator` with no translator behind it: the
// schema accepted `{status: {eq: "banana"}}` and the model filter received nil,
// so the server answered with the unfiltered set while the client believed it
// had filtered. Monomorphizing moves the rejection to the GraphQL parser and
// gives the column a translator that actually reaches `comparator.Enum[T]`.

// enumFixtureSchema is the postgres fixture the assertions below share. It
// carries every shape the routing has to separate:
//
//   - `orders.status` / `shipments.status` — the same enum on two tables, so
//     the one-input-per-enum sharing rule (PRD §26.4 "Sharing") is observable
//     rather than assumed.
//   - `orders.prior_status` — nullable, so Rule 2's `Nullable<E>Comparator`
//     twin is reachable.
//   - `orders.history` — an enum ARRAY, whose model comparator is
//     `comparator.Slice[OrderStatus]`, not `Enum[OrderStatus]`.
//   - `orders.name` — a plain text column, so the fallback's absence from the
//     enum path is visible.
func enumFixtureSchema() *parserpkg.Schema {
	return &parserpkg.Schema{
		Enums: []parserpkg.Enum{
			{Name: "order_status", Schema: "public", Values: []string{"pending", "shipped", "delivered"}},
		},
		Tables: []parserpkg.Table{
			{
				Name: "orders",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "status", Type: "order_status"},
					{Name: "prior_status", Type: "order_status", Nullable: true},
					{Name: "history", Type: "order_status[]"},
					{Name: "name", Type: "text"},
				},
			},
			{
				Name: "shipments",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "status", Type: "order_status"},
				},
			},
		},
	}
}

// enumFixtureInput wires the schema enums onto the resolver the way the
// `Generate` pipeline's registerSchemaTypes does — without it an enum column
// resolves to a plain `string` and never reaches the Enum comparator at all.
//
// `output.package` is set to "models" because an enum comparator's type
// parameter is the CONSUMER's Go type: the descriptor spells it qualified,
// and the qualification comes from this config field (typeBindings.modelsPkg).
func enumFixtureInput(t *testing.T, schema *parserpkg.Schema) (*gen.GenerateInput, []gen.EnumContext) {
	t.Helper()
	in := apiTestInput(t, schema)
	in.Config.Output.Package = "models"
	collisions := gen.ComputeNameCollisions(schema)
	in.Resolver = enumFixtureResolver(t, schema, in.Config, collisions)
	return in, gen.BuildEnumContexts(schema, collisions, in.Config)
}

// enumFixtureResolver stands in for the pipeline's registerSchemaTypes. It
// resolves each enum through gen.EnumGoTypeName rather than gen.StructName for
// the same reason registerSchemaTypes does: an `enums.<name>.struct_name`
// override has to reach the COLUMN, not just the generated type, or the column
// resolves to a Go type nothing declares (PRD §4.11).
func enumFixtureResolver(t *testing.T, schema *parserpkg.Schema, cfg *config.RootConfig, collisions map[string]bool) *gotype.Resolver {
	t.Helper()
	r := gotype.NewResolver(config.DialectPostgres, true, cfg.Overrides.Types)
	for _, e := range schema.Enums {
		r.RegisterEnum(e.Name, gen.EnumGoTypeName(cfg, e.Name, e.Schema, collisions))
	}
	return r
}

// enumFixtureContext builds the APIContext for a fixture schema.
func enumFixtureContext(t *testing.T, schema *parserpkg.Schema) *gen.APIContext {
	t.Helper()
	in, enums := enumFixtureInput(t, schema)
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, enums, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	apiCtx.ModelsPackage = in.Config.Output.Package
	return apiCtx
}

// TestEnumComparator_schemaEmission pins the enum filter: an enum column's
// filter field is typed with the monomorphized input, its operands are the
// bound GraphQL enum rather than String, and Rule 2's nullability split holds —
// only the twin declares `isNull`.
func TestEnumComparator_schemaEmission(t *testing.T) {
	apiCtx := enumFixtureContext(t, enumFixtureSchema())
	orders := apiTableByName(t, apiCtx, "Order")

	if got := fieldBySQLName(t, orders, "status").Filter.InputTypeName; got != "OrderStatusComparator" {
		t.Errorf("NOT NULL enum column references %q, want OrderStatusComparator — "+
			"the StringComparator fallback is advertised and silently dropped", got)
	}
	if got := fieldBySQLName(t, orders, "prior_status").Filter.InputTypeName; got != "NullableOrderStatusComparator" {
		t.Errorf("nullable enum column references %q, want NullableOrderStatusComparator", got)
	}

	shared := renderAPISharedSchema(t, apiCtx)
	base := namedInputBlock(t, shared, "OrderStatusComparator")
	twin := namedInputBlock(t, shared, "NullableOrderStatusComparator")

	// comparator.Enum[T]'s operator set, minus the Custom escape hatch.
	for _, op := range []string{"eq", "neq", "in", "nin"} {
		if !strings.Contains(base, "\n  "+op+":") {
			t.Errorf("OrderStatusComparator is missing operand %q:\n%s", op, base)
		}
		if !strings.Contains(twin, "\n  "+op+":") {
			t.Errorf("NullableOrderStatusComparator is missing operand %q:\n%s", op, twin)
		}
	}
	if strings.Contains(base, "custom:") || strings.Contains(twin, "custom:") {
		t.Errorf("enum comparator projects the raw-SQL Custom escape hatch:\n%s\n%s", base, twin)
	}

	// Operands are the bound enum, not String — this is what makes
	// `{status: {eq: BANANA}}` a parse-time error.
	for _, want := range []string{"eq:  OrderStatus", "in:  [OrderStatus!]"} {
		if !strings.Contains(base, want) {
			t.Errorf("OrderStatusComparator operands are not enum-typed (want %q):\n%s", want, base)
		}
	}

	// Rule 2's asymmetry.
	if strings.Contains(base, "isNull:") {
		t.Errorf("OrderStatusComparator (the NOT NULL form) declares isNull:\n%s", base)
	}
	if !strings.Contains(twin, "\n  isNull:") {
		t.Errorf("NullableOrderStatusComparator does not declare isNull:\n%s", twin)
	}

	// The operand type names an enum the schema must also declare (Rule 3's
	// undeclared-reference hazard, one level over from scalars).
	if !strings.Contains(shared, "enum OrderStatus {") {
		t.Errorf("shared schema references OrderStatus operands without declaring the enum:\n%s", shared)
	}
}

// TestEnumComparator_oneInputPerEnum pins PRD §26.4 "Sharing": one input type
// per (family, GraphQL operand type, nullability), not one per column and not
// one per table. Two tables filter on the same enum here.
func TestEnumComparator_oneInputPerEnum(t *testing.T) {
	apiCtx := enumFixtureContext(t, enumFixtureSchema())

	if got := fieldBySQLName(t, apiTableByName(t, apiCtx, "Shipment"), "status").Filter.InputTypeName; got != "OrderStatusComparator" {
		t.Errorf("second table's enum column references %q, want the shared OrderStatusComparator", got)
	}

	names := make(map[string]int, len(apiCtx.ComparatorFamilies))
	for _, fam := range apiCtx.ComparatorFamilies {
		names[fam.Name]++
	}
	for _, name := range []string{"OrderStatusComparator", "NullableOrderStatusComparator"} {
		if names[name] != 1 {
			t.Errorf("ComparatorFamilies declares %s %d times, want exactly 1", name, names[name])
		}
	}

	shared := renderAPISharedSchema(t, apiCtx)
	if got := strings.Count(shared, "input OrderStatusComparator {"); got != 1 {
		t.Errorf("shared schema declares input OrderStatusComparator %d times, want 1:\n%s", got, shared)
	}

	// One translator per (enum, nullability) too — the translators are
	// project-wide, so a second referencing table must not add a second copy.
	funcs := make(map[string]int, len(apiCtx.ComparatorTranslators))
	for _, tr := range apiCtx.ComparatorTranslators {
		funcs[tr.FuncName]++
	}
	for _, name := range []string{"translateOrderStatusComparator", "translateNullableOrderStatusComparator"} {
		if funcs[name] != 1 {
			t.Errorf("ComparatorTranslators declares %s %d times, want exactly 1", name, funcs[name])
		}
	}
}

// TestEnumComparator_translatorEmission asserts the generated bodies: each
// monomorphization returns the model's own `comparator.Enum[T]`, the type
// parameter is qualified with the consumer's models package (an enum's Go
// type is the consumer's, unlike every other family's), and `isNull` reaches
// the wrapper's Null field.
func TestEnumComparator_translatorEmission(t *testing.T) {
	apiCtx := enumFixtureContext(t, enumFixtureSchema())
	out := renderComparatorTranslate(t, apiCtx)

	mustContain(t, out, "func translateOrderStatusComparator(in *OrderStatusComparator) *comparator.Enum[models.OrderStatus] {")
	mustContain(t, out, "func translateNullableOrderStatusComparator(in *NullableOrderStatusComparator) *comparator.NullableEnum[models.OrderStatus] {")

	base := translatorBody(t, out, "translateOrderStatusComparator")
	for _, want := range []string{
		"out.Eq = in.Eq",
		"out.Neq = in.Neq",
		"out.In = append([]models.OrderStatus(nil), in.In...)",
		"out.Nin = append([]models.OrderStatus(nil), in.Nin...)",
	} {
		if !strings.Contains(base, want) {
			t.Errorf("translateOrderStatusComparator does not copy %q:\n%s", want, base)
		}
	}
	if strings.Contains(base, "out.Custom") {
		t.Errorf("translateOrderStatusComparator populates the raw-SQL Custom field:\n%s", base)
	}

	twin := translatorBody(t, out, "translateNullableOrderStatusComparator")
	if !strings.Contains(twin, "out.Null = in.IsNull") {
		t.Errorf("the nullable enum translator drops isNull:\n%s", twin)
	}

	// The per-table filter translator has to dispatch into it, or the field
	// is advertised and ignored all over again.
	orders := apiTableByName(t, apiCtx, "Order")
	dispatch := map[string]string{}
	for _, ff := range orders.FilterFields {
		dispatch[ff.ModelFieldName] = ff.TranslatorFunc
	}
	if got := dispatch["Status"]; got != "translateOrderStatusComparator" {
		t.Errorf("orders.status dispatches to %q, want translateOrderStatusComparator", got)
	}
	if got := dispatch["PriorStatus"]; got != "translateNullableOrderStatusComparator" {
		t.Errorf("orders.prior_status dispatches to %q, want translateNullableOrderStatusComparator", got)
	}
}

// TestEnumComparator_fileImportsModelsPackage pins the import half of the
// qualification above. The comparator translator file was previously free of
// any dependency on the consumer's models package; the Enum family is the one
// that introduces one, and FormatOnly rendering means goimports never repairs
// a missing import.
func TestEnumComparator_fileImportsModelsPackage(t *testing.T) {
	apiCtx := enumFixtureContext(t, enumFixtureSchema())
	apiCtx.ModelsImportPath = "example.com/app/models"

	file := string(gen.BuildAPIComparatorTranslateFileForTest("graph", apiCtx, renderComparatorTranslate(t, apiCtx)))
	imports, _, ok := strings.Cut(file, "\n)\n")
	if !ok {
		t.Fatalf("assembled file has no import block:\n%s", file)
	}
	// The alias must be the same spelling the bodies qualify with, or the
	// file does not compile.
	if !strings.Contains(imports, `models "example.com/app/models"`) {
		t.Errorf("assembled file does not import the consumer models package:\n%s", imports)
	}

	// A project with no enum column must not pick the import up — an unused
	// import is a compile error too.
	plain := enumFixtureContext(t, &parserpkg.Schema{
		Tables: []parserpkg.Table{{
			Name: "orders",
			Columns: []parserpkg.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "name", Type: "text"},
			},
		}},
	})
	plain.ModelsImportPath = "example.com/app/models"
	plainFile := string(gen.BuildAPIComparatorTranslateFileForTest("graph", plain, renderComparatorTranslate(t, plain)))
	if strings.Contains(plainFile, "example.com/app/models") {
		t.Errorf("an enum-free project imports the models package:\n%s", plainFile)
	}
}

// TestEnumComparator_arrayColumnIsNotMonomorphized guards the boundary
// between the enum family and the slice family. `stripPointerAndSlice`
// reduces `[]OrderStatus` to `OrderStatus` before the enum lookup, so an enum
// ARRAY column carries an enum binding — but its model comparator is
// `comparator.Slice[OrderStatus]`. Routing it to the Enum arm would hand the
// model filter a `*comparator.Enum` where it expects a `*comparator.Slice`
// and the generated package would not compile.
//
// The array lands on `<Enum>SliceComparator`, which consumes the
// SAME binding for its type parameter — so the two arms are separated by the
// comparator expression alone, not by the binding.
func TestEnumComparator_arrayColumnIsNotMonomorphized(t *testing.T) {
	apiCtx := enumFixtureContext(t, enumFixtureSchema())
	history := fieldBySQLName(t, apiTableByName(t, apiCtx, "Order"), "history")

	if got := history.Filter.InputTypeName; got == "OrderStatusComparator" || got == "NullableOrderStatusComparator" {
		t.Fatalf("enum-array column references %q — an array filters through "+
			"comparator.Slice[T], not comparator.Enum[T]", got)
	}
	if got := history.Filter.InputTypeName; got != "OrderStatusSliceComparator" {
		t.Errorf("enum-array column references %q, want OrderStatusSliceComparator", got)
	}
	if got := history.Filter.TranslatorFunc; got != "translateOrderStatusSliceComparator" {
		t.Errorf("enum-array translator = %q, want translateOrderStatusSliceComparator", got)
	}
	if got := history.Filter.Translator.GoReturnType; got != "*comparator.Slice[models.OrderStatus]" {
		t.Errorf("enum-array return type = %q, want *comparator.Slice[models.OrderStatus]", got)
	}
}

// TestEnumComparator_hiddenColumnEmitsNothing pins the §32.2 interaction: a
// hidden enum column contributes no comparator input AND no enum declaration.
// The second half matters on its own — an enum declared but referenced by
// nothing is dead schema, and the access role is supposed to remove the
// column's whole footprint, not just its field.
func TestEnumComparator_hiddenColumnEmitsNothing(t *testing.T) {
	schema := &parserpkg.Schema{
		Enums: []parserpkg.Enum{
			{Name: "order_status", Schema: "public", Values: []string{"pending", "shipped"}},
		},
		Tables: []parserpkg.Table{{
			Name: "orders",
			Columns: []parserpkg.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "status", Type: "order_status"},
				{Name: "name", Type: "text"},
			},
		}},
	}
	in, enums := enumFixtureInput(t, schema)
	in.Config.Tables["orders"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			"status": {Access: "hidden"},
		},
	}
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, enums, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	if got := fieldBySQLName(t, apiCtx.Tables[0], "status").Filter.InputTypeName; got != "" {
		t.Errorf("hidden enum column carries filter input %q, want none", got)
	}
	shared := renderAPISharedSchema(t, apiCtx)
	if strings.Contains(shared, "OrderStatusComparator") {
		t.Errorf("hidden enum column still emits a comparator input:\n%s", shared)
	}
	if strings.Contains(shared, "enum OrderStatus {") {
		t.Errorf("hidden enum column still declares its enum:\n%s", shared)
	}
}

// TestEnumComparator_setColumnKeepsStringComparator is the MySQL SET boundary.
// A MySQL SET is registered in the same enum lookup an enum column uses (its
// value type becomes a GraphQL enum), but the column is stored as
// comma-separated text and its model comparator is `comparator.String`.
// Monomorphizing it would advertise an enum-typed operand for a column the
// model side compares as a string.
func TestEnumComparator_setColumnKeepsStringComparator(t *testing.T) {
	in, sets := setBindingInput(t)
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, sets, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	for col, want := range map[string]string{
		"permissions": "StringComparator",
		"opt_perms":   "NullableStringComparator",
	} {
		if got := fieldBySQLName(t, apiCtx.Tables[0], col).Filter.InputTypeName; got != want {
			t.Errorf("SET column %q references %q, want %q — a SET is stored as text", col, got, want)
		}
	}
}

// TestEnumComparator_nonSchemaEnumDropsFilterField guards the other half of
// the routing guard. resolveComparatorType sends EVERY unqualified PascalCase
// Go type through comparator.Enum[T] (isEnumLikeType), so a column bound to a
// consumer scalar whose Go type happens to be spelled that way arrives at the
// Enum arm with no schema enum behind it. Projecting it would emit operands
// naming a GraphQL enum the schema never declares, and gqlgen would reject
// the document.
//
// The column once kept a StringComparator fallback with no translator
// behind it — advertised and silently ignored, the failure PRD §26.5.3 names
// the worst available. It is now dropped from both surfaces instead, which is the
// only way §26.5.3 lets such a column satisfy the completeness lint.
func TestEnumComparator_nonSchemaEnumDropsFilterField(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{{
			Name: "orders",
			Columns: []parserpkg.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "shipping_address", Type: "text"},
			},
		}},
	}
	in := apiTestInput(t, schema)
	in.Config.Output.Package = "models"
	// A Go type that is unqualified and PascalCase — exactly what
	// isEnumLikeType keys on — but is not a schema enum. It reaches the API
	// only because the consumer declared a scalar for it; without that,
	// the scalar-binding guard rejects the column outright.
	in.Config.Tables["orders"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			"shipping_address": {Type: "Address"},
		},
	}
	in.Config.API.GraphQL.Scalars = map[string]config.ScalarBinding{
		"Address": {GoType: "Address", Marshaling: config.ScalarMarshalingMethod},
	}
	in.Resolver = gotype.NewResolver(config.DialectPostgres, true, in.Config.Overrides.Types)

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	addr := fieldBySQLName(t, apiCtx.Tables[0], "shipping_address")
	if addr.Filterable {
		t.Error("non-enum PascalCase column is still advertised on the filter input")
	}
	if got := addr.Filter.InputTypeName; got != "" {
		t.Errorf("non-enum PascalCase column references %q, want no filter projection", got)
	}
	if got := addr.Filter.TranslatorFunc; got != "" {
		t.Errorf("non-enum PascalCase column carries translator %q, want none", got)
	}
	if strings.Contains(renderAPISharedSchema(t, apiCtx), "AddressComparator") {
		t.Error("a non-enum Go type was monomorphized into an enum comparator")
	}
	// The column keeps its object-type field and its sort entry — only the
	// filter surface goes. §32.2 is the knob for dropping the rest.
	if !addr.Readable || !addr.Sortable {
		t.Errorf("dropping the filter projection also dropped the read/sort surface: readable=%v sortable=%v",
			addr.Readable, addr.Sortable)
	}
}
