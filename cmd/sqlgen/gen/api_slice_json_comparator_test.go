package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	parserpkg "github.com/teandresmith/sqlgen/parser"
)

// The JSON / JSONB / Slice comparator families (PRD §26.4).
//
// Without them every column below advertised `StringComparator` with
// no translator entry behind it, so the model filter received nil and the
// server answered `{metadata: {hasKey: "x"}}` with the UNFILTERED set while
// the client believed it had filtered. The enum comparators close the same
// gap for enum columns.

// dialectAPIContext builds an APIContext for a schema under one dialect, with
// the gotype resolver built for that SAME dialect.
//
// The pairing is the whole point. `apiTestInput` hard-wires a PostgreSQL
// resolver, so flipping only `Config.Input.Dialect` leaves `text[]` resolving
// to `[]string` with IsSlice set — which would make a MySQL fixture "prove"
// the slice family is gated when nothing gated it. The resolver IS the gate
// (only its PostgreSQL array branch sets IsSlice), so a dialect test that does
// not move it tests nothing.
func dialectAPIContext(t *testing.T, dialect config.Dialect, overrides map[string]config.TypeOverride, schema *parserpkg.Schema) *gen.APIContext {
	t.Helper()
	in := apiTestInput(t, schema)
	in.Config.Input.Dialect = dialect
	for k, v := range overrides {
		in.Config.Overrides.Types[k] = v
	}
	in.Resolver = gotype.NewResolver(dialect, true, in.Config.Overrides.Types)
	return buildProjectionAPIContext(t, in)
}

func jsonTypeOverride() map[string]config.TypeOverride {
	return map[string]config.TypeOverride{
		"jsonb": {Type: "types.JSON", Import: "github.com/teandresmith/sqlgen/types"},
	}
}

func probeSchema(cols ...parserpkg.Column) *parserpkg.Schema {
	return &parserpkg.Schema{
		Tables: []parserpkg.Table{{
			Name: "probes",
			Columns: append([]parserpkg.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
			}, cols...),
		}},
	}
}

// TestJSONComparator_dialectGating pins PRD §26.4 "Dialect gating" for the two
// document families: `JSONBComparator` is PostgreSQL-only, a `jsonb` column on
// MySQL falls back to `JSONComparator`, and on SQLite neither family is
// reachable — the column is filterable on neither side.
//
// Every row needs the `jsonb` type override to be reachable at all — the
// built-in dialect tables map `jsonb` to nothing on MySQL or SQLite, so an
// un-overridden column resolves to `string` and never touches the JSON arm.
// Overriding it to `types.JSON` is exactly the shape §26.4's fallback sentence
// describes: the model type says JSON, and the DIALECT decides which of the
// two families it is — or, on SQLite, that it is neither.
//
// The SQLite row asserts absence on BOTH surfaces rather than absence of the
// input alone. `comparator.JSON.Parse` has no `sqlite` arm, so a column that
// kept its filter field would parse, dispatch into a real translator and emit
// no SQL — the accept-and-ignore class these families exist to remove, which is
// why this row previously read `JSONComparator` and now reads nothing at all.
func TestJSONComparator_dialectGating(t *testing.T) {
	tests := []struct {
		dialect config.Dialect
		// wantInput empty means the column is filterable on neither side;
		// wantAbsent then covers both families and both translators.
		wantInput    string
		wantAbsent   []string
		wantAbsentTr []string
	}{
		{
			dialect:      config.DialectPostgres,
			wantInput:    "JSONBComparator",
			wantAbsent:   []string{"input JSONComparator {", "input NullableJSONComparator {"},
			wantAbsentTr: []string{"translateJSONComparator("},
		},
		{
			dialect:      config.DialectMySQL,
			wantInput:    "JSONComparator",
			wantAbsent:   []string{"input JSONBComparator {", "input NullableJSONBComparator {"},
			wantAbsentTr: []string{"translateJSONBComparator("},
		},
		{
			dialect:   config.DialectSQLite,
			wantInput: "",
			wantAbsent: []string{
				"input JSONComparator {", "input NullableJSONComparator {",
				"input JSONBComparator {", "input NullableJSONBComparator {",
			},
			wantAbsentTr: []string{"translateJSONComparator(", "translateJSONBComparator("},
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.dialect), func(t *testing.T) {
			apiCtx := dialectAPIContext(t, tt.dialect, jsonTypeOverride(),
				probeSchema(parserpkg.Column{Name: "doc", Type: "jsonb"}))
			doc := fieldBySQLName(t, apiTableByName(t, apiCtx, "Probe"), "doc")

			if doc.Filter.InputTypeName != tt.wantInput {
				t.Errorf("Filter.InputTypeName = %q, want %q", doc.Filter.InputTypeName, tt.wantInput)
			}
			wantTranslator := ""
			if tt.wantInput != "" {
				wantTranslator = "translate" + tt.wantInput
			}
			if got := doc.Filter.TranslatorFunc; got != wantTranslator {
				t.Errorf("Filter.TranslatorFunc = %q, want %q", got, wantTranslator)
			}
			if got, want := doc.Filterable, tt.wantInput != ""; got != want {
				t.Errorf("Filterable = %v, want %v", got, want)
			}

			schema := renderAPISharedSchema(t, apiCtx)
			if tt.wantInput != "" {
				mustContain(t, schema, "input "+tt.wantInput+" {")
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(schema, absent) {
					t.Errorf("%s schema declares %q:\n%s", tt.dialect, absent, schema)
				}
			}

			// The shared schema declares the comparator inputs; the table
			// schema is where the column either takes one or is absent. Both
			// are asserted because a gate that removed only the shared input
			// would leave `doc` referencing an undeclared type.
			//
			// Scoped to the ProbeFilter block: `doc` keeps its field on the
			// Probe type and on the create / update inputs either way — only
			// the filter surface moves, and matching the whole document would
			// pass on those instead.
			filterInput := graphQLBlock(t, renderAPITableSchema(t, apiTableByName(t, apiCtx, "Probe")), "input ProbeFilter {")
			if got, want := strings.Contains(filterInput, "doc:"), tt.wantInput != ""; got != want {
				t.Errorf("%s ProbeFilter declares a `doc` field = %v, want %v:\n%s",
					tt.dialect, got, want, filterInput)
			}

			apiCtx.ModelsPackage = "models"
			apiCtx.ModelsImportPath = "example.com/app/models"
			translators := renderComparatorTranslate(t, apiCtx)
			if tt.wantInput != "" {
				mustContain(t, translators, "func translate"+tt.wantInput+"(")
			}
			for _, absent := range tt.wantAbsentTr {
				if strings.Contains(translators, "func "+absent) {
					t.Errorf("%s emits %s:\n%s", tt.dialect, absent, translators)
				}
			}
		})
	}
}

// TestJSONComparator_sqliteModelSideNonFilterable pins the OTHER half of
// the SQLite gate. The GraphQL assertions above would still pass if the gate
// lived inside the GraphQL projection, and the generated `<T>Filter` would keep a
// `*comparator.JSON` field that accepts `Contains` / `HasKey` on SQLite and
// emits nothing — the identical defect one layer down. Both surfaces derive
// filterability from `columnIsFilterable`, and this is what holds them there.
//
// MySQL is the control: the same schema, the same override, one dialect apart.
// Without it the SQLite assertion could pass because the override never took.
func TestJSONComparator_sqliteModelSideNonFilterable(t *testing.T) {
	tests := []struct {
		dialect        config.Dialect
		wantFilterable bool
		wantComparator string
	}{
		{config.DialectMySQL, true, "*comparator.JSON"},
		{config.DialectSQLite, false, ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.dialect), func(t *testing.T) {
			in := testInput(probeSchema(parserpkg.Column{Name: "doc", Type: "jsonb"}))
			in.Config.Input.Dialect = tt.dialect
			for k, v := range jsonTypeOverride() {
				in.Config.Overrides.Types[k] = v
			}
			in.Resolver = gotype.NewResolver(tt.dialect, true, in.Config.Overrides.Types)

			contexts, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts: %v", err)
			}
			if len(contexts) != 1 {
				t.Fatalf("BuildTableContexts returned %d contexts, want 1", len(contexts))
			}

			var doc *gen.FilterFieldContext
			for i, ff := range contexts[0].FilterFields {
				if ff.ColumnName == "doc" {
					doc = &contexts[0].FilterFields[i]
				}
			}
			if doc == nil {
				t.Fatal("FilterFields has no entry for doc")
			}
			if doc.Filterable != tt.wantFilterable {
				t.Errorf("doc.Filterable = %v, want %v", doc.Filterable, tt.wantFilterable)
			}
			if doc.ComparatorType != tt.wantComparator {
				t.Errorf("doc.ComparatorType = %q, want %q", doc.ComparatorType, tt.wantComparator)
			}

			// The id column is the second control: the gate is keyed on the
			// comparator family, not applied to every column on SQLite.
			for _, ff := range contexts[0].FilterFields {
				if ff.ColumnName == "doc" {
					continue
				}
				if !ff.Filterable || ff.ComparatorType == "" {
					t.Errorf("column %q lost its filter on %s (Filterable=%v, ComparatorType=%q)",
						ff.ColumnName, tt.dialect, ff.Filterable, ff.ComparatorType)
				}
			}
		})
	}
}

// TestSliceComparator_dialectGating pins the other half of §26.4's dialect
// rule. A slice family is PostgreSQL-only and stays that way structurally,
// with no dialect switch in the projection: only the gotype resolver's
// PostgreSQL array branch sets IsSlice, so `text[]` resolves to a plain
// `string` on MySQL / SQLite and the column filters as ordinary text.
func TestSliceComparator_dialectGating(t *testing.T) {
	schema := probeSchema(
		parserpkg.Column{Name: "tags", Type: "text[]"},
		parserpkg.Column{Name: "scores", Type: "integer[]"},
	)

	for _, d := range []config.Dialect{config.DialectPostgres, config.DialectMySQL, config.DialectSQLite} {
		t.Run(string(d), func(t *testing.T) {
			apiCtx := dialectAPIContext(t, d, nil, schema)
			tbl := apiTableByName(t, apiCtx, "Probe")
			tags := fieldBySQLName(t, tbl, "tags")
			scores := fieldBySQLName(t, tbl, "scores")
			out := renderAPISharedSchema(t, apiCtx)

			if d != config.DialectPostgres {
				if strings.Contains(out, "SliceComparator") {
					t.Errorf("%s schema declares a slice comparator:\n%s", d, out)
				}
				for _, f := range []gen.APIFieldContext{tags, scores} {
					if strings.Contains(f.Filter.InputTypeName, "Slice") {
						t.Errorf("%s: column %s references %q", d, f.SQLName, f.Filter.InputTypeName)
					}
					// Not merely "no slice input" — the column must still be
					// filterable through a real translator, or the dialect
					// gate would have created the accept-and-ignore hole it
					// exists to avoid.
					if f.Filter.TranslatorFunc == "" {
						t.Errorf("%s: column %s has no translator behind %q",
							d, f.SQLName, f.Filter.InputTypeName)
					}
				}
				return
			}

			if got := tags.Filter.InputTypeName; got != "StringSliceComparator" {
				t.Errorf("tags references %q, want StringSliceComparator", got)
			}
			if got := scores.Filter.InputTypeName; got != "IntSliceComparator" {
				t.Errorf("scores references %q, want IntSliceComparator", got)
			}
			mustContain(t, out, "input StringSliceComparator {")
			mustContain(t, out, "input IntSliceComparator {")
		})
	}
}

// TestSliceComparator_oneInputPerElementType pins PRD §26.4 "Sharing" for the
// slice family: the input is keyed on the element's GRAPHQL type while the
// translator is keyed on its GO type.
//
// `smallint[]`, `integer[]` and `bigint[]` all bind their elements to `Int`,
// so all three share one `IntSliceComparator` — the same collapse
// NumericComparator makes for scalar widths — while each needs its own
// `comparator.Slice[T]` and therefore its own translator. Two tables
// reference the text array so the sharing is observed across tables, not
// assumed from a single column.
func TestSliceComparator_oneInputPerElementType(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{
			{
				Name: "probes",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "tags", Type: "text[]"},
					{Name: "smalls", Type: "smallint[]"},
					{Name: "scores", Type: "integer[]"},
					{Name: "bigs", Type: "bigint[]"},
				},
			},
			{
				Name: "mirrors",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "labels", Type: "text[]"},
				},
			},
		},
	}
	apiCtx := dialectAPIContext(t, config.DialectPostgres, nil, schema)

	counts := map[string]int{}
	for _, fam := range apiCtx.ComparatorFamilies {
		counts[fam.Name]++
	}
	for _, name := range []string{"StringSliceComparator", "IntSliceComparator"} {
		if counts[name] != 1 {
			t.Errorf("ComparatorFamilies declares %s %d times, want exactly 1", name, counts[name])
		}
	}

	apiCtx.ModelsPackage = "models"
	apiCtx.ModelsImportPath = "example.com/app/models"
	out := renderComparatorTranslate(t, apiCtx)
	// One translator per Go element width, each taking the SHARED input.
	for _, want := range []string{
		"func translateStringSliceComparator(in *StringSliceComparator) *comparator.Slice[string] {",
		"func translateIntSliceComparatorInt16(in *IntSliceComparator) *comparator.Slice[int16] {",
		"func translateIntSliceComparatorInt32(in *IntSliceComparator) *comparator.Slice[int32] {",
		"func translateIntSliceComparatorInt64(in *IntSliceComparator) *comparator.Slice[int64] {",
	} {
		mustContain(t, out, want)
	}
	// The identity element needs no suffix, so a bare-named translator must
	// not also be emitted for the widths — that would be two functions with
	// one name.
	if strings.Contains(out, "func translateIntSliceComparator(in ") {
		t.Errorf("an unsuffixed Int slice translator was emitted alongside the widths:\n%s", out)
	}
	if n := strings.Count(out, "func translateStringSliceComparator(in "); n != 1 {
		t.Errorf("translateStringSliceComparator emitted %d times, want 1 (two tables share it)", n)
	}
}

// TestSliceComparator_castVersusIdentity pins the two operand-copy arms
// against the widths that select them. `real[]` narrows `[]float64` (gqlgen's
// Float binding) to `[]float32`, `double precision[]` does not — and the
// suffix rule follows the cast, so the two land on distinct function names
// while sharing one `FloatSliceComparator`.
func TestSliceComparator_castVersusIdentity(t *testing.T) {
	apiCtx := dialectAPIContext(t, config.DialectPostgres, nil, probeSchema(
		parserpkg.Column{Name: "ratios", Type: "real[]"},
		parserpkg.Column{Name: "weights", Type: "double precision[]"},
	))
	apiCtx.ModelsPackage = "models"
	apiCtx.ModelsImportPath = "example.com/app/models"
	out := renderComparatorTranslate(t, apiCtx)

	mustContain(t, out, "func translateFloatSliceComparatorFloat32(in *FloatSliceComparator) *comparator.Slice[float32] {")
	mustContain(t, out, "\t\t\txs[i] = float32(v)")
	mustContain(t, out, "func translateFloatSliceComparator(in *FloatSliceComparator) *comparator.Slice[float64] {")
	mustContain(t, out, "\t\tout.ContainsAny = append([]float64(nil), in.ContainsAny...)")
	// A no-op conversion would compile but `unconvert` flags it, and it would
	// misdescribe the identity case.
	if strings.Contains(out, "xs[i] = float64(v)") {
		t.Errorf("the identity arm emits a self-conversion:\n%s", out)
	}
}

// TestSliceComparator_isEmptyIsAPredicate pins the one operator whose type
// does not move with the element: `isEmpty` projects comparator.Slice's
// `IsEmpty *bool`, which compiles to `col = '{}'` rather than to an array
// comparison, so it stays `Boolean` on every element type.
func TestSliceComparator_isEmptyIsAPredicate(t *testing.T) {
	apiCtx := dialectAPIContext(t, config.DialectPostgres, nil, probeSchema(
		parserpkg.Column{Name: "tags", Type: "text[]"},
	))
	block := namedInputBlock(t, renderAPISharedSchema(t, apiCtx), "StringSliceComparator")

	for _, want := range []string{
		"containsAny: [String!]",
		"containsAll: [String!]",
		"containedBy: [String!]",
		"isEmpty:     Boolean",
	} {
		mustContain(t, block, want)
	}
	if strings.Contains(block, "isEmpty:     [String!]") {
		t.Errorf("isEmpty is typed as an element operand:\n%s", block)
	}
}

// TestSliceJSONComparator_jsonArrayIsFilterableOnNeitherSide pins the
// non-comparable array rule through the new families. `comparator.Slice[T]`
// requires `T comparable`, which `types.JSON` (a `[]byte` alias) is not, so a
// `jsonb[]` column has no model comparator at all — and these families must not
// give it a GraphQL one either. Absent from BOTH surfaces is how such a column
// satisfies §26.5.3's completeness lint; being exempt from it is not an option
// the lint offers.
func TestSliceJSONComparator_jsonArrayIsFilterableOnNeitherSide(t *testing.T) {
	apiCtx := dialectAPIContext(t, config.DialectPostgres, nil, probeSchema(
		parserpkg.Column{Name: "payloads", Type: "jsonb[]"},
		parserpkg.Column{Name: "tags", Type: "text[]"},
	))
	tbl := apiTableByName(t, apiCtx, "Probe")
	payloads := fieldBySQLName(t, tbl, "payloads")

	if payloads.Filterable {
		t.Error("a jsonb[] column is marked filterable")
	}
	if payloads.Filter.InputTypeName != "" || payloads.Filter.TranslatorFunc != "" {
		t.Errorf("a jsonb[] column carries a projection: %+v", payloads.Filter)
	}
	for _, ff := range tbl.FilterFields {
		if ff.GraphQLName == "payloads" {
			t.Error("a jsonb[] column reached the filter translator")
		}
	}
	// The sibling text[] column on the same table proves the absence is the
	// element-type rule and not the table being skipped wholesale.
	if got := fieldBySQLName(t, tbl, "tags").Filter.InputTypeName; got != "StringSliceComparator" {
		t.Errorf("sibling text[] column references %q, want StringSliceComparator", got)
	}
	// Scoped to the filter input: the column is still READABLE, so it appears
	// on the object type — losing the filter field is not losing the column.
	filterBlock := namedInputBlock(t, renderAPITableSchema(t, tbl), "ProbeFilter")
	if strings.Contains(filterBlock, "payloads:") {
		t.Errorf("the schema filter input declares the jsonb[] column:\n%s", filterBlock)
	}
	mustContain(t, filterBlock, "tags: StringSliceComparator")
}

// TestSliceComparator_enumElementQualifiesModelsPackage covers the one Slice
// shape whose type parameter is the CONSUMER's Go type. An enum array's
// element is a schema enum, so the translator's `comparator.Slice[T]` must
// spell it under the models alias and the generated file must import that
// package — the same obligation the Enum family carries, reached by a
// different route, so a project whose ONLY enum use is an array still gets
// the import.
func TestSliceComparator_enumElementQualifiesModelsPackage(t *testing.T) {
	schema := &parserpkg.Schema{
		Enums: []parserpkg.Enum{
			{Name: "order_status", Schema: "public", Values: []string{"pending", "shipped"}},
		},
		Tables: []parserpkg.Table{{
			Name: "orders",
			Columns: []parserpkg.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				// No scalar enum column anywhere: the array is the only use.
				{Name: "history", Type: "order_status[]"},
			},
		}},
	}
	apiCtx := enumFixtureContext(t, schema)
	apiCtx.ModelsImportPath = "example.com/app/models"

	history := fieldBySQLName(t, apiTableByName(t, apiCtx, "Order"), "history")
	if got := history.Filter.InputTypeName; got != "OrderStatusSliceComparator" {
		t.Fatalf("enum array references %q, want OrderStatusSliceComparator", got)
	}

	body := renderComparatorTranslate(t, apiCtx)
	mustContain(t, body, "*comparator.Slice[models.OrderStatus]")
	mustContain(t, body, "out.ContainsAny = append([]models.OrderStatus(nil), in.ContainsAny...)")

	file := string(gen.BuildAPIComparatorTranslateFileForTest("graph", apiCtx, body))
	if !strings.Contains(file, "example.com/app/models") {
		t.Errorf("a slice-of-enum translator does not import the models package:\n%s", file)
	}

	// The operands are typed with the bound GraphQL enum, which is what makes
	// `{history: {containsAny: [BANANA]}}` a parse-time rejection.
	block := namedInputBlock(t, renderAPISharedSchema(t, apiCtx), "OrderStatusSliceComparator")
	mustContain(t, block, "containsAny: [OrderStatus!]")
}

// TestJSONComparator_documentOperandsUseTheJSONScalar pins the operand-scalar
// obligation (PRD §26.4 Rule 3): every scalar a
// comparator input NAMES must be declared. Both document families type
// `contains` with `JSON` rather than `String`, so an undeclared `JSON` scalar
// would be a document gqlgen rejects.
func TestJSONComparator_documentOperandsUseTheJSONScalar(t *testing.T) {
	apiCtx := dialectAPIContext(t, config.DialectPostgres, nil, probeSchema(
		parserpkg.Column{Name: "doc", Type: "jsonb"},
	))
	out := renderAPISharedSchema(t, apiCtx)

	mustContain(t, namedInputBlock(t, out, "JSONBComparator"), "contains:    JSON")
	mustContain(t, namedInputBlock(t, out, "JSONBComparator"), "containedBy: JSON")
	mustContain(t, out, "scalar JSON")

	declared := map[string]bool{}
	for _, s := range apiCtx.UsedScalars {
		declared[s.Name] = true
	}
	if !declared["JSON"] {
		t.Error("the JSON scalar is not registered for a jsonb column's comparator operands")
	}
}

// TestJSONComparator_translatorBoxesTheDocument pins the operand shape the
// generated body depends on. `types.JSON` is nilable, so gqlgen leaves the
// operand BARE rather than pointer-wrapping it, while the comparator holds it
// as `*any` — so the body boxes into a local and takes its address. Boxing
// rather than naming the type is also what keeps the file free of a `types`
// import, which under FormatOnly rendering would have to be emitted by hand.
func TestJSONComparator_translatorBoxesTheDocument(t *testing.T) {
	apiCtx := dialectAPIContext(t, config.DialectPostgres, nil, probeSchema(
		parserpkg.Column{Name: "doc", Type: "jsonb"},
		parserpkg.Column{Name: "doc_n", Type: "jsonb", Nullable: true},
	))
	apiCtx.ModelsPackage = "models"
	apiCtx.ModelsImportPath = "example.com/app/models"
	out := renderComparatorTranslate(t, apiCtx)

	mustContain(t, out, "func translateJSONBComparator(in *JSONBComparator) *comparator.JSONB {")
	for _, want := range []string{
		// The `string(...)` is measured, not cosmetic: a []byte parameter is
		// sent as BINARY, and MySQL rejects `JSON_CONTAINS(col, _binary'…')`
		// with Error 3144 under `interpolateParams=true`. See the template's
		// api/comparator-json-document fragment for the probe.
		"\t\tv := any(string(in.Contains))\n\t\tout.Contains = &v",
		"\t\tv := any(string(in.ContainedBy))\n\t\tout.ContainedBy = &v",
		"\t\tout.HasAnyKey = append([]string(nil), in.HasAnyKey...)",
		"\t\tout.HasAllKeys = append([]string(nil), in.HasAllKeys...)",
		"\t\tout.PathExists = in.PathExists",
	} {
		mustContain(t, out, want)
	}
	// Rule 2: the nullable twin takes its own input and carries isNull into
	// the wrapper's Null field.
	mustContain(t, out, "func translateNullableJSONBComparator(in *NullableJSONBComparator) *comparator.NullableJSONB {")
	mustContain(t, out, "out := &comparator.NullableJSONB{}")
	if strings.Contains(out, "\"github.com/teandresmith/sqlgen/types\"") {
		t.Errorf("the JSON arm names types.JSON and would need an import:\n%s", out)
	}
	// The raw-SQL escape hatch is never projected on any family.
	if strings.Contains(out, "out.Custom") {
		t.Errorf("a document translator populates the Custom escape hatch:\n%s", out)
	}
}

// TestSliceComparator_customScalarElementKeepsFallback pins the conservative
// half of sliceOperandElemGoType. gqlgen wraps a non-null list element in a
// pointer when the Go type is a STRUCT and leaves it bare otherwise, and that
// rule is measured per type rather than derived — so an element binding to a
// custom scalar has no projection sqlgen can emit soundly, and the column
// keeps the fallback instead of advertising operands the body cannot copy.
func TestSliceComparator_customScalarElementKeepsFallback(t *testing.T) {
	apiCtx := dialectAPIContext(t, config.DialectPostgres, nil, probeSchema(
		parserpkg.Column{Name: "seen_at", Type: "timestamptz[]"},
	))
	seen := fieldBySQLName(t, apiTableByName(t, apiCtx, "Probe"), "seen_at")

	if strings.Contains(seen.Filter.InputTypeName, "Slice") {
		t.Errorf("a Time-element array projects %q; its list operand shape is unmeasured",
			seen.Filter.InputTypeName)
	}
	if seen.Filter.TranslatorFunc != "" {
		t.Errorf("a Time-element array carries translator %q", seen.Filter.TranslatorFunc)
	}
	if strings.Contains(renderAPISharedSchema(t, apiCtx), "SliceComparator") {
		t.Error("a slice comparator was declared for an unmeasured element type")
	}
}

// TestSliceComparator_specScalarNamesAreReserved pins the namespace half of
// PRD §26.4 "GraphQL type name ownership". `StringSliceComparator` is derived
// entirely from names sqlgen owns, so a table resolving onto it is a
// generation error reported against shared_gen.graphqls — not a gqlgen
// `Cannot redeclare type` that names neither side.
func TestSliceComparator_specScalarNamesAreReserved(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{
			{
				Name: "probes",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "tags", Type: "text[]"},
				},
			},
			{
				Name: "string_slice_comparator",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
		},
	}
	in := apiTestInput(t, schema)
	in.Resolver = gotype.NewResolver(config.DialectPostgres, true, in.Config.Overrides.Types)
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}

	_, err = gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err == nil {
		t.Fatal("a table resolving onto StringSliceComparator was accepted")
	}
	if !strings.Contains(err.Error(), "StringSliceComparator") {
		t.Errorf("error does not name the contested type: %v", err)
	}
}

// graphQLBlock returns the declaration opened by header, up to the closing
// brace in column zero. Assertions about one input type's fields have to be
// scoped this way: a column name appears in the object type and in the create
// / update inputs as well, so a whole-document Contains proves nothing about
// the filter surface.
func graphQLBlock(t *testing.T, schema, header string) string {
	t.Helper()
	i := strings.Index(schema, header)
	if i < 0 {
		t.Fatalf("schema has no %q:\n%s", header, schema)
	}
	rest := schema[i:]
	j := strings.Index(rest, "\n}")
	if j < 0 {
		t.Fatalf("%q is not closed:\n%s", header, rest)
	}
	return rest[:j+2]
}
