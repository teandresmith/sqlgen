package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	parser "github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

// opaqueComparatorGoTypes is the closed set of Go types routed onto
// comparator.Opaque[T]: the four the built-in dialect tables resolve binary
// and network columns to.
var opaqueComparatorGoTypes = []string{"[]byte", "net.IP", "net.IPNet", "net.HardwareAddr"}

// TestOpaqueComparatorBindings_matchRegistryAndPredicate pins the three places
// the Opaque[T] set is spelled against each other. They are deliberately
// separate — the model-side predicate must not import GraphQL vocabulary, and
// the registry is the scalar authority — so nothing but this test stops one
// from drifting. A drift is silent in the worst way: a Go type in the
// predicate but not in the bindings table resolves to comparator.Opaque[T] on
// the model side and then finds no translator, which drops the column's filter
// field rather than erroring.
func TestOpaqueComparatorBindings_matchRegistryAndPredicate(t *testing.T) {
	bindings := gen.OpaqueComparatorBindingsForTest()

	if len(bindings) != len(opaqueComparatorGoTypes) {
		t.Errorf("opaqueComparatorBindings has %d entries, want %d (%v)",
			len(bindings), len(opaqueComparatorGoTypes), opaqueComparatorGoTypes)
	}

	for _, goType := range opaqueComparatorGoTypes {
		t.Run(goType, func(t *testing.T) {
			bind, ok := bindings[goType]
			if !ok {
				t.Fatalf("opaqueComparatorBindings has no entry for %q", goType)
			}
			if !gen.IsOpaqueComparatorGoTypeForTest(goType) {
				t.Errorf("isOpaqueComparatorGoType(%q) = false, but the type has a binding — "+
					"the model side would not route it onto comparator.Opaque[T]", goType)
			}
			// The comparator's operands are typed with the SAME scalar the
			// column's own field declares (PRD §26.4 "Opaque columns"). That
			// identity is what lets registerColumnScalars skip an operand
			// registration for these families: the field scalar is already
			// registered, so the schema never names an undeclared scalar.
			want, ok := gen.BuiltInScalarNameForTest(goType)
			if !ok {
				t.Fatalf("builtInScalarRegistry has no entry for %q, so the comparator "+
					"operand type names a scalar nothing declares", goType)
			}
			if bind.Scalar != want {
				t.Errorf("binding scalar = %q, but builtInScalarRegistry binds %q to %q",
					bind.Scalar, goType, want)
			}
		})
	}

	// The predicate must not claim types the bindings table cannot serve.
	for _, goType := range []string{"string", "time.Time", "time.Duration", "types.JSON", "netip.Addr"} {
		if gen.IsOpaqueComparatorGoTypeForTest(goType) {
			t.Errorf("isOpaqueComparatorGoType(%q) = true, want false — only the four "+
				"types in opaqueComparatorBindings route onto comparator.Opaque[T]", goType)
		}
	}
}

// TestDurationColumn_isNotIncrementable pins the first of the Opaque
// routing's two silent-if-missed constraints. `time.Duration` is `~int64`, so it satisfies
// comparator.Numeric and could have been admitted to isComparatorNumericGoType
// instead of taking its own arm in resolveGenericComparator. That would have
// been wrong in a way no comparator test would catch: isIncrementableGoType is
// a superset of isComparatorNumericGoType, and sqlTypeCategory("interval") is
// categoryUnknown — which isArithmeticSQLColumn *trusts* — so an interval
// column would silently gain `dur_inc: Int` / `dur_dec: Int` on its update
// input. Int operands on a duration column is the same category of mismatch
// the Opaque routing exists to remove.
func TestDurationColumn_isNotIncrementable(t *testing.T) {
	schema := &parser.Schema{Tables: []parser.Table{{
		Name: "probes",
		Columns: []parser.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "dur", Type: "interval"},
			// A real numeric sibling, so the assertion below is proved to be
			// discriminating rather than vacuous.
			{Name: "stock", Type: "integer"},
		},
	}}}

	apiCtx := projectionAPIContext(t, schema)
	tbl := apiTableByName(t, apiCtx, "Probe")

	incNames := make([]string, 0, len(tbl.UpdateOps))
	for _, op := range tbl.UpdateOps {
		incNames = append(incNames, op.SQLName)
	}
	if !slices.Contains(incNames, "stock") {
		t.Fatalf("integer column is not increment-eligible (%v) — the fixture proves nothing", incNames)
	}
	if slices.Contains(incNames, "dur") {
		t.Errorf("interval column is increment-eligible (%v): time.Duration reached "+
			"isComparatorNumericGoType, so the update input emits dur_inc / dur_dec "+
			"with Int operands on a duration column", incNames)
	}
}

// TestComparatorTranslatorStdImports_matchEmittedBodies pins the second
// silent-if-missed constraint. graph/comparator_translate_gen.go is rendered
// through FormatOnly, which does NOT run goimports, so its import block has to
// name exactly what the bodies reference: a missing import and an unused one
// are both compile errors in the consumer's project, and neither is repaired
// downstream.
func TestComparatorTranslatorStdImports_matchEmittedBodies(t *testing.T) {
	tests := []struct {
		name    string
		columns []parser.Column
		want    []string
	}{
		{
			name:    "no qualified operand type needs no std import",
			columns: []parser.Column{{Name: "name", Type: "text"}},
			want:    nil,
		},
		{
			// []byte names no package, so a Bytes-only project must NOT
			// import net — an unused import is as fatal as a missing one.
			name:    "bytes alone imports nothing",
			columns: []parser.Column{{Name: "payload", Type: "bytea"}},
			want:    nil,
		},
		{
			name:    "inet imports net",
			columns: []parser.Column{{Name: "addr", Type: "inet"}},
			want:    []string{"net"},
		},
		{
			name:    "cidr imports net",
			columns: []parser.Column{{Name: "net_block", Type: "cidr"}},
			want:    []string{"net"},
		},
		{
			name:    "macaddr imports net",
			columns: []parser.Column{{Name: "mac", Type: "macaddr"}},
			want:    []string{"net"},
		},
		{
			name:    "interval imports time",
			columns: []parser.Column{{Name: "dur", Type: "interval"}},
			want:    []string{"time"},
		},
		{
			// A nullable-only project still needs the import: the
			// Nullable variant inlines the same operand block rather than
			// delegating to its base.
			name:    "nullable-only inet still imports net",
			columns: []parser.Column{{Name: "addr", Type: "inet", Nullable: true}},
			want:    []string{"net"},
		},
		{
			name: "both packages, sorted",
			columns: []parser.Column{
				{Name: "addr", Type: "inet"},
				{Name: "dur", Type: "interval"},
				{Name: "created_at", Type: "timestamp"},
			},
			want: []string{"net", "time"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols := append([]parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}, tt.columns...)
			apiCtx := projectionAPIContext(t, &parser.Schema{
				Tables: []parser.Table{{Name: "probes", Columns: cols}},
			})

			got := gen.ComparatorTranslatorStdImportsForTest(apiCtx)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("comparatorTranslatorStdImports() = %v, want %v", got, tt.want)
			}

			// Cross-check against what the rendered bodies actually name, so
			// the expectations above cannot drift away from the template.
			body := renderAPIComparatorTranslators(t, apiCtx)
			for _, pkg := range []string{"net", "time"} {
				used := strings.Contains(body, pkg+".")
				listed := slices.Contains(got, pkg)
				if used && !listed {
					t.Errorf("rendered translators reference %s. but %q is not imported", pkg, pkg)
				}
				if listed && !used {
					t.Errorf("%q is imported but the rendered translators never reference %s.", pkg, pkg)
				}
			}
		})
	}
}

// TestPKFilterExpr_matchesFilterFieldType is the "one derivation" pin for key
// columns. The generated client assigns a PK/FK filter expression straight into
// the table's own filter struct, so `pkFilterExpr` / `pkFilterInExpr` and
// `resolveComparatorType` must agree on the comparator family — but they are
// separate functions in separate files, and the first pair
// once re-derived the answer from `isComparatorNumericGoType` alone.
//
// That third spelling broke the moment `[]byte` and the `net` types started
// resolving to comparator.Opaque[T]: a `BLOB PRIMARY KEY` kept getting
// `&comparator.ID{…}` assigned into a `*comparator.Opaque[[]byte]` field, and
// the generated package stopped compiling (10+ sites per table — GetByID,
// GetMany, the Create/Update/Delete ID collections, and every relationship
// loader). Both helpers now derive from `pkComparatorFamily`; this asserts the
// result rather than the routing.
func TestPKFilterExpr_matchesFilterFieldType(t *testing.T) {
	tests := []struct {
		name    string
		sqlType string
		want    string
	}{
		{"uuid", "uuid", "comparator.ID"},
		{"text", "text", "comparator.ID"},
		{"bigint", "bigint", "comparator.Number[int64]"},
		{"integer", "integer", "comparator.Number[int32]"},
		// The four that regressed.
		{"bytea", "bytea", "comparator.Opaque[[]byte]"},
		{"inet", "inet", "comparator.Opaque[net.IP]"},
		{"cidr", "cidr", "comparator.Opaque[net.IPNet]"},
		{"macaddr", "macaddr", "comparator.Opaque[net.HardwareAddr]"},
		{"interval", "interval", "comparator.Number[time.Duration]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := apiTestInput(t, &parser.Schema{Tables: []parser.Table{{
				Name:    "keys",
				Columns: []parser.Column{{Name: "id", Type: tt.sqlType, PrimaryKey: true}},
			}}})
			tables, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts: %v", err)
			}

			var field string
			for _, ff := range tables[0].FilterFields {
				if ff.ColumnName == "id" {
					field = ff.ComparatorType
				}
			}
			if want := "*" + tt.want; field != want {
				t.Fatalf("filter field type = %q, want %q", field, want)
			}

			var pkCol gen.ColumnContext
			for _, c := range tables[0].Columns {
				if c.Name == "id" {
					pkCol = c
				}
			}
			fm := gen.FuncMap(sql.NewPostgresDialect())
			eq := fm["pkFilterExpr"].(func(gen.ColumnContext, string) string)(pkCol, "id")
			in1 := fm["pkFilterInExpr"].(func(gen.ColumnContext, string, bool) string)(pkCol, "ids", false)

			// The expression allocates the very type the field holds, so the
			// generated assignment compiles.
			if !strings.HasPrefix(eq, "&"+tt.want+"{") {
				t.Errorf("pkFilterExpr = %q, want a &%s{…} literal to match field %q", eq, tt.want, field)
			}
			if !strings.HasPrefix(in1, "&"+tt.want+"{") {
				t.Errorf("pkFilterInExpr = %q, want a &%s{…} literal to match field %q", in1, tt.want, field)
			}

			// Only the ID family renders keys as strings; the other two take
			// the key's own Go type, so the templates must collect []T.
			gotIsString := fm["pkIsStringType"].(func(any) bool)(pkCol)
			if wantIsString := tt.want == "comparator.ID"; gotIsString != wantIsString {
				t.Errorf("pkIsStringType = %v, want %v for %s", gotIsString, wantIsString, tt.want)
			}
		})
	}
}
