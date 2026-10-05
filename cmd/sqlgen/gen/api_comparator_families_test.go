package gen_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	parserpkg "github.com/teandresmith/sqlgen/parser"
)

// goOperatorToGraphQL maps a comparator struct's Go field name to the GraphQL
// operator name that projects it (PRD §26.4). Every exported field on a
// comparator family except `Custom` must appear here — an unmapped field is a
// new Go operator with no GraphQL counterpart, which is exactly the drift
// TestComparatorFamilies_projectEveryGoOperator exists to catch.
var goOperatorToGraphQL = map[string]string{
	"Eq":         "eq",
	"Neq":        "neq",
	"Gt":         "gt",
	"Gte":        "gte",
	"Lt":         "lt",
	"Lte":        "lte",
	"In":         "in",
	"Nin":        "nin",
	"Contains":   "contains",
	"StartsWith": "startsWith",
	"EndsWith":   "endsWith",
	"Like":       "like",
	"NLike":      "nlike",
	"Between":    "between",
	"NBetween":   "nbetween",
	// The document and array families.
	"HasKey":      "hasKey",
	"HasAnyKey":   "hasAnyKey",
	"HasAllKeys":  "hasAllKeys",
	"ContainedBy": "containedBy",
	"PathExists":  "pathExists",
	"ContainsAny": "containsAny",
	"ContainsAll": "containsAll",
	"IsEmpty":     "isEmpty",
}

// comparatorStructFields parses the runtime comparator package's source and
// returns the exported field names of one struct.
//
// Reading the source rather than importing the package is deliberate: the
// runtime is a separate Go module and `cmd/sqlgen/go.mod` has no require on
// it. ARCHITECTURE.md permits a CLI -> Runtime edge, so this is a cost
// judgement rather than a prohibition — adding a module dependency (and its
// version pin, and its `go mod tidy` surface) to satisfy one test is not
// worth it. go/ast over a fixed in-repo path gives the same fact for free.
func comparatorStructFields(t *testing.T, file, structName string) []string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "comparator", file)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var fields []string
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != structName {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, fld := range st.Fields.List {
			for _, name := range fld.Names {
				if name.IsExported() {
					fields = append(fields, name.Name)
				}
			}
		}
		return false
	})
	if len(fields) == 0 {
		t.Fatalf("no exported fields found on comparator.%s in %s", structName, path)
	}
	return fields
}

// TestComparatorFamilies_projectEveryGoOperator is the operator-by-operator
// acceptance gate for 25.3 (PRD §26.4): for each of the five shipped
// families, every non-`Custom` operator on the Go comparator must have a
// GraphQL counterpart in the emitted input, and the emitted input must carry
// nothing the Go side cannot receive.
//
// `Custom` is asserted absent on purpose — it is a raw-SQL escape hatch, and
// projecting it would put SQL construction on the HTTP surface.
func TestComparatorFamilies_projectEveryGoOperator(t *testing.T) {
	tests := []struct {
		name       string
		sourceFile string
		structName string
		input      string
		// schema is a fixture whose columns reference `input`.
		column parserpkg.Column
	}{
		{
			name:       "String",
			sourceFile: "string.go",
			structName: "String",
			input:      "StringComparator",
			column:     parserpkg.Column{Name: "name", Type: "text"},
		},
		{
			name:       "ID",
			sourceFile: "id.go",
			structName: "ID",
			input:      "IDComparator",
			column: parserpkg.Column{
				Name: "company_id", Type: "uuid",
				FKReference: &parserpkg.FKReference{Table: "companies", Column: "id"},
			},
		},
		{
			name:       "Number",
			sourceFile: "number.go",
			structName: "Number",
			input:      "NumericComparator",
			column:     parserpkg.Column{Name: "stock", Type: "integer"},
		},
		{
			name:       "Bool",
			sourceFile: "bool.go",
			structName: "Bool",
			input:      "BooleanComparator",
			column:     parserpkg.Column{Name: "active", Type: "boolean"},
		},
		{
			name:       "Time",
			sourceFile: "time.go",
			structName: "Time",
			input:      "TimeComparator",
			column:     parserpkg.Column{Name: "released_at", Type: "timestamp"},
		},
		// The four Opaque[T] monomorphizations share one Go struct,
		// so each row asserts the same operator set reaches a differently
		// named input; Duration reuses comparator.Number, which is the point
		// — its model comparator IS Number[time.Duration], only the GraphQL
		// operand type differs (PRD §26.4 Rule 1).
		{
			name:       "Opaque/Bytes",
			sourceFile: "opaque.go",
			structName: "Opaque",
			input:      "BytesComparator",
			column:     parserpkg.Column{Name: "payload", Type: "bytea"},
		},
		{
			name:       "Opaque/IP",
			sourceFile: "opaque.go",
			structName: "Opaque",
			input:      "IPComparator",
			column:     parserpkg.Column{Name: "addr", Type: "inet"},
		},
		{
			name:       "Opaque/CIDR",
			sourceFile: "opaque.go",
			structName: "Opaque",
			input:      "CIDRComparator",
			column:     parserpkg.Column{Name: "net_block", Type: "cidr"},
		},
		{
			name:       "Opaque/MacAddr",
			sourceFile: "opaque.go",
			structName: "Opaque",
			input:      "MacAddrComparator",
			column:     parserpkg.Column{Name: "mac", Type: "macaddr"},
		},
		{
			name:       "Number/Duration",
			sourceFile: "number.go",
			structName: "Number",
			input:      "DurationComparator",
			column:     parserpkg.Column{Name: "dur", Type: "interval"},
		},
		// JSON is reached here through a postgres `json` column, whose
		// model comparator is comparator.JSON — `jsonb` on the same dialect
		// takes the JSONB arm instead, which is the row below it.
		{
			name:       "JSON",
			sourceFile: "json.go",
			structName: "JSON",
			input:      "JSONComparator",
			column:     parserpkg.Column{Name: "settings", Type: "json"},
		},
		{
			name:       "JSONB",
			sourceFile: "jsonb.go",
			structName: "JSONB",
			input:      "JSONBComparator",
			column:     parserpkg.Column{Name: "doc", Type: "jsonb"},
		},
		// Two rows off one Go struct, as the Opaque family has four: the
		// element type moves the input NAME and the operand type, and the
		// operator set has to survive both.
		{
			name:       "Slice/String",
			sourceFile: "slice.go",
			structName: "Slice",
			input:      "StringSliceComparator",
			column:     parserpkg.Column{Name: "tags", Type: "text[]"},
		},
		{
			name:       "Slice/Int",
			sourceFile: "slice.go",
			structName: "Slice",
			input:      "IntSliceComparator",
			column:     parserpkg.Column{Name: "scores", Type: "integer[]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := comparatorInputBlock(t, tt.column, tt.input)

			for _, field := range comparatorStructFields(t, tt.sourceFile, tt.structName) {
				if field == "Custom" {
					continue
				}
				op, ok := goOperatorToGraphQL[field]
				if !ok {
					t.Fatalf("comparator.%s has field %q with no GraphQL mapping — "+
						"a new Go operator needs a projection in shippedComparatorFamilies "+
						"and an entry in goOperatorToGraphQL", tt.structName, field)
				}
				if !strings.Contains(block, "\n  "+op+":") {
					t.Errorf("%s does not project comparator.%s.%s as %q:\n%s",
						tt.input, tt.structName, field, op, block)
				}
			}

			if strings.Contains(block, "custom:") {
				t.Errorf("%s projects the raw-SQL Custom escape hatch:\n%s", tt.input, block)
			}
			// The NOT NULL form carries no isNull — that operand belongs to
			// the Nullable twin (PRD §26.4 Rule 2).
			if strings.Contains(block, "isNull:") {
				t.Errorf("%s (the NOT NULL form) declares isNull:\n%s", tt.input, block)
			}
		})
	}
}

// comparatorInputBlock renders the shared schema for a one-column fixture and
// returns the named `input <X> { ... }` block.
func comparatorInputBlock(t *testing.T, col parserpkg.Column, input string) string {
	t.Helper()
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{
			{Name: "companies", Columns: []parserpkg.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{
				Name: "products",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					col,
				},
			},
		},
	}
	apiCtx := projectionAPIContext(t, schema)
	out := renderAPISharedSchema(t, apiCtx)
	return namedInputBlock(t, out, input)
}

func namedInputBlock(t *testing.T, schema, input string) string {
	t.Helper()
	start := strings.Index(schema, "input "+input+" {")
	if start < 0 {
		t.Fatalf("schema does not declare input %s:\n%s", input, schema)
	}
	end := strings.Index(schema[start:], "\n}")
	if end < 0 {
		t.Fatalf("input %s block is unterminated:\n%s", input, schema)
	}
	return schema[start : start+end+2]
}

// TestComparatorFamilies_nullableTwin pins PRD §26.4 Rule 2: nullability is a
// separate input type, not a conditional field. A nullable column references
// `Nullable<X>Comparator`, a NOT NULL column references `<X>Comparator`, only
// the former declares `isNull`, and the operand sets are otherwise identical.
func TestComparatorFamilies_nullableTwin(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{
			{
				Name: "products",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "created_at", Type: "timestamp"},
					{Name: "deleted_at", Type: "timestamp", Nullable: true},
				},
			},
		},
	}
	apiCtx := projectionAPIContext(t, schema)
	tbl := apiCtx.Tables[0]

	if got := fieldBySQLName(t, tbl, "created_at").Filter.InputTypeName; got != "TimeComparator" {
		t.Errorf("NOT NULL column references %q, want TimeComparator", got)
	}
	if got := fieldBySQLName(t, tbl, "deleted_at").Filter.InputTypeName; got != "NullableTimeComparator" {
		t.Errorf("nullable column references %q, want NullableTimeComparator", got)
	}

	shared := renderAPISharedSchema(t, apiCtx)
	base := namedInputBlock(t, shared, "TimeComparator")
	twin := namedInputBlock(t, shared, "NullableTimeComparator")

	if strings.Contains(base, "isNull:") {
		t.Errorf("TimeComparator declares isNull:\n%s", base)
	}
	if !strings.Contains(twin, "\n  isNull:") || !strings.Contains(twin, "isNull:   Boolean") {
		t.Errorf("NullableTimeComparator does not declare isNull: Boolean:\n%s", twin)
	}
	// Operand sets are identical apart from isNull.
	for _, op := range []string{"eq", "neq", "gt", "gte", "lt", "lte", "in", "nin", "between", "nbetween"} {
		if !strings.Contains(twin, "\n  "+op+":") {
			t.Errorf("NullableTimeComparator is missing operand %q:\n%s", op, twin)
		}
	}

	// The nullable translator takes its own input and carries isNull into the
	// wrapper's Null field — it is no longer silently dropped.
	apiCtx.ModelsPackage = "models"
	tr := renderComparatorTranslate(t, apiCtx)
	mustContain(t, tr, "func translateNullableTimeComparator(in *NullableTimeComparator) *comparator.NullableTime {")
	mustContain(t, tr, "out.Null = in.IsNull")
}

// TestComparatorFamilies_translatorCopiesEveryOperator asserts the other half
// of the round trip: every GraphQL operator the schema advertises is actually
// assigned into the model comparator by the generated translator. A field
// declared in the input but never copied would be accepted and ignored — the
// advertised-but-dropped filter failure, one layer down.
//
// The expectation is DERIVED from the emitted schema (via
// APIContext.ComparatorFamilies) rather than hand-listed, so an operator
// added to the family table but not to the template's operand block fails
// here instead of shipping as an accepted-and-ignored field.
func TestComparatorFamilies_translatorCopiesEveryOperator(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{
			{Name: "companies", Columns: []parserpkg.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{
				Name: "products",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "company_id", Type: "uuid", FKReference: &parserpkg.FKReference{Table: "companies", Column: "id"}},
					{Name: "name", Type: "text"},
					{Name: "stock", Type: "integer"},
					{Name: "active", Type: "boolean"},
					{Name: "released_at", Type: "timestamp"},
					{Name: "price", Type: "numeric"},
					// A nullable column per family, so the derived check spans
					// all six twins rather than two of six — the shared
					// operand fragment is exercised under every wrapper,
					// including the NumericT cast block.
					{Name: "description", Type: "text", Nullable: true},
					{Name: "deleted_at", Type: "timestamp", Nullable: true},
					{Name: "retry_count", Type: "integer", Nullable: true},
					{Name: "succeeded", Type: "boolean", Nullable: true},
					{Name: "discount", Type: "numeric", Nullable: true},
					{
						Name: "parent_id", Type: "uuid", Nullable: true,
						FKReference: &parserpkg.FKReference{Table: "companies", Column: "id"},
					},
				},
			},
		},
	}
	in := apiTestInput(t, schema)
	configureNullVariantOverrides(t, in)
	apiCtx := buildProjectionAPIContext(t, in)
	apiCtx.ModelsPackage = "models"
	out := renderComparatorTranslate(t, apiCtx)

	// The Go model field each GraphQL operator must land in.
	modelFieldForOperator := map[string]string{}
	for goField, op := range goOperatorToGraphQL {
		modelFieldForOperator[op] = goField
	}

	// Keyed by input type -> generated function name. NOT by the struct the
	// function allocates: DecimalComparator and StringComparator both build
	// a comparator.String, so the allocation is ambiguous and the Decimal
	// body (which deliberately omits the text operators) would be checked
	// against StringComparator's operator list.
	byInput := map[string]string{}
	for _, tr := range apiCtx.ComparatorTranslators {
		byInput[tr.InputTypeName] = tr.FuncName
	}
	if len(byInput) == 0 {
		t.Fatal("fixture produced no comparator translators")
	}

	checked := 0
	for _, fam := range apiCtx.ComparatorFamilies {
		funcName, ok := byInput[fam.Name]
		if !ok {
			// A family can be declared without a translator only via the
			// documented fallback (enum / JSON / JSONB / slice columns
			// reference StringComparator with no translator behind them).
			continue
		}
		body := translatorBody(t, out, funcName)
		for _, op := range fam.Operators {
			if op.Name == "isNull" {
				// Rule 2's operand lands in the wrapper's Null field, not a
				// same-named model operator.
				if !strings.Contains(body, "out.Null = in.IsNull") {
					t.Errorf("%s declares isNull but %s never assigns out.Null:\n%s",
						fam.Name, funcName, body)
				}
				continue
			}
			field, ok := modelFieldForOperator[op.Name]
			if !ok {
				t.Fatalf("%s declares operator %q with no model-field mapping — "+
					"a new GraphQL operator needs an entry in goOperatorToGraphQL",
					fam.Name, op.Name)
			}
			if !strings.Contains(body, "out."+field+" = ") {
				t.Errorf("%s declares %q but %s never assigns out.%s:\n%s",
					fam.Name, op.Name, funcName, field, body)
			}
			checked++
		}
		if strings.Contains(body, "out.Custom") {
			t.Errorf("%s populates the raw-SQL Custom field:\n%s", funcName, body)
		}
	}
	if checked == 0 {
		t.Fatal("no operators were checked — the derivation found nothing")
	}
}

// translatorBody returns the body of one generated translator, located by
// function name — the only unambiguous key, since two families can allocate
// the same model struct.
func translatorBody(t *testing.T, out, funcName string) string {
	t.Helper()
	marker := "func " + funcName + "("
	start := strings.Index(out, marker)
	if start < 0 {
		t.Fatalf("no translator named %s:\n%s", funcName, out)
	}
	end := strings.Index(out[start:], "\n}\n")
	if end < 0 {
		return out[start:]
	}
	return out[start : start+end]
}

// TestDecimalComparator_routing pins range filtering on decimals. A decimal
// column filters through comparator.String on the model side, but its GraphQL
// face is DecimalComparator with Decimal-typed operands — that is what
// makes `{ gt: "100.50" }` expressible and numerically correct. The text
// operators comparator.String carries are deliberately not projected.
func TestDecimalComparator_routing(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{
			{
				Name: "products",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "price", Type: "numeric"},
					{Name: "discount", Type: "numeric", Nullable: true},
				},
			},
		},
	}
	in := apiTestInput(t, schema)
	configureNullVariantOverrides(t, in)
	apiCtx := buildProjectionAPIContext(t, in)
	tbl := apiCtx.Tables[0]

	if got := fieldBySQLName(t, tbl, "price").Filter.InputTypeName; got != "DecimalComparator" {
		t.Errorf("NOT NULL decimal references %q, want DecimalComparator", got)
	}
	if got := fieldBySQLName(t, tbl, "discount").Filter.InputTypeName; got != "NullableDecimalComparator" {
		t.Errorf("nullable decimal references %q, want NullableDecimalComparator", got)
	}

	shared := renderAPISharedSchema(t, apiCtx)
	block := namedInputBlock(t, shared, "DecimalComparator")
	for _, op := range []string{"eq", "neq", "gt", "gte", "lt", "lte", "in", "nin"} {
		if !strings.Contains(block, "\n  "+op+":") {
			t.Errorf("DecimalComparator is missing %q:\n%s", op, block)
		}
	}
	// Operands are Decimal-typed, not String — this is the whole point.
	if !strings.Contains(block, "eq:  Decimal") {
		t.Errorf("DecimalComparator operands are not Decimal-typed:\n%s", block)
	}
	// Text operators are the deliberate narrowing (PRD §26.4).
	for _, op := range []string{"contains", "startsWith", "endsWith", "like", "nlike"} {
		if strings.Contains(block, "\n  "+op+":") {
			t.Errorf("DecimalComparator projects the text operator %q, which is "+
				"meaningless on a numeric column:\n%s", op, block)
		}
	}

	// The Decimal scalar must be declared even though the nullable column's
	// own field type is NullDecimal — both comparator forms take Decimal
	// operands.
	if !strings.Contains(shared, "scalar Decimal\n") {
		t.Errorf("shared schema references Decimal operands without declaring the scalar:\n%s", shared)
	}

	apiCtx.ModelsPackage = "models"
	tr := renderComparatorTranslate(t, apiCtx)
	mustContain(t, tr, "func translateDecimalComparator(in *DecimalComparator) *comparator.String {")
	mustContain(t, tr, "func translateNullableDecimalComparator(in *NullableDecimalComparator) *comparator.NullableString {")
	// Decimal operands are rendered onto the canonical string form.
	mustContain(t, tr, "v := in.Gt.String()")
	if strings.Contains(tr, "translateNumericComparatorDecimal") {
		t.Error("a decimal column reached the Number[T] path; PRD §26.5.3 states it never does")
	}
}

// TestComparatorFamilies_emittedOnlyWhenReferenced pins PRD §26.4 Rule 3: a
// comparator input is emitted only when some column references it, for BOTH
// the base form and the nullable twin.
//
// The rule is a correctness requirement rather than a size optimisation. A
// family's operand type may name a scalar that is itself declared only on use
// (`TimeComparator` takes `Time`, `DecimalComparator` takes `Decimal`), so an
// input emitted with nothing referencing it can name a scalar the schema
// never declares — and gqlgen rejects the document.
func TestComparatorFamilies_emittedOnlyWhenReferenced(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{
			{
				Name: "products",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
		},
	}
	apiCtx := buildProjectionAPIContext(t, apiTestInput(t, schema))
	out := renderAPISharedSchema(t, apiCtx)

	mustContain(t, out, "input StringComparator {")
	for _, absent := range []string{
		"input NullableStringComparator {",
		"input TimeComparator {",
		"input NumericComparator {",
		"input BooleanComparator {",
		"input IDComparator {",
		"input DecimalComparator {",
	} {
		if strings.Contains(out, absent) {
			t.Errorf("emitted %q for a schema with no column referencing it:\n%s", absent, out)
		}
	}
	// A family with no reference also drops its range input.
	if strings.Contains(out, "input TimeRange {") {
		t.Errorf("emitted TimeRange with no Time comparator:\n%s", out)
	}
}

// TestDecimalComparator_narrowsOnlyTheStringFamily is a regression pin.
// The decimal routing keys on the resolved comparator FAMILY, not on the
// GraphQL scalar alone, because a decimal-typed column does not always land
// on comparator.String:
//
//   - a decimal FOREIGN KEY resolves to comparator.ID (DeriveFKMethod returns
//     FKStringSprint for decimal.Decimal, so resolveSimpleComparator takes its
//     ID arm);
//   - a `numeric[]` column resolves to comparator.Slice[decimal.Decimal].
//
// Routing either onto translateDecimalComparator would hand the model filter
// a *comparator.String where it expects an ID or a Slice, and the generated
// filter translator would not compile.
func TestDecimalComparator_narrowsOnlyTheStringFamily(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{
			{
				Name: "price_tiers",
				Columns: []parserpkg.Column{
					{Name: "amount", Type: "numeric", PrimaryKey: true},
				},
			},
			{
				Name: "products",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					// Plain decimal — the narrowing SHOULD apply.
					{Name: "price", Type: "numeric"},
					// Decimal FK — resolves to comparator.ID.
					{
						Name: "tier_amount", Type: "numeric",
						FKReference: &parserpkg.FKReference{Table: "price_tiers", Column: "amount"},
					},
					// Decimal array — resolves to comparator.Slice.
					{Name: "historic_prices", Type: "numeric[]"},
				},
			},
		},
	}
	in := apiTestInput(t, schema)
	configureNullVariantOverrides(t, in)
	apiCtx := buildProjectionAPIContext(t, in)
	tbl := apiTableByName(t, apiCtx, "Product")

	tests := []struct {
		column         string
		wantInput      string
		wantTranslator string
	}{
		{column: "price", wantInput: "DecimalComparator", wantTranslator: "translateDecimalComparator"},
		{column: "tier_amount", wantInput: "IDComparator", wantTranslator: "translateIDComparator"},
		// No sound projection (comparator.Slice with a Decimal element —
		// sliceOperandElemGoType has not measured that list shape), so it is
		// dropped from both GraphQL surfaces rather than narrowing it.
		{column: "historic_prices", wantInput: "", wantTranslator: ""},
	}
	for _, tt := range tests {
		t.Run(tt.column, func(t *testing.T) {
			f := fieldBySQLName(t, tbl, tt.column)
			if f.Filter.InputTypeName != tt.wantInput {
				t.Errorf("InputTypeName = %q, want %q", f.Filter.InputTypeName, tt.wantInput)
			}
			if f.Filter.TranslatorFunc != tt.wantTranslator {
				t.Errorf("TranslatorFunc = %q, want %q", f.Filter.TranslatorFunc, tt.wantTranslator)
			}
		})
	}
}

// TestComparatorTranslateFile_importsTimeForNullableOnly is a regression
// pin. The nullable Time translator no longer delegates to its
// base — it inlines `[]time.Time` and `comparator.Range[time.Time]` — so a
// project whose ONLY time columns are nullable still needs the `time` import.
// The file is rendered with FormatOnly, so goimports never repairs a missing
// one and the generated package simply fails to compile.
func TestComparatorTranslateFile_importsTimeForNullableOnly(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{
			{
				Name: "products",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					// Nullable only — no NOT NULL timestamp anywhere.
					{Name: "archived_at", Type: "timestamp", Nullable: true},
				},
			},
		},
	}
	apiCtx := projectionAPIContext(t, schema)

	sawNullable := false
	for _, tr := range apiCtx.ComparatorTranslators {
		if tr.Family != "Time" {
			continue
		}
		if !tr.Nullable {
			t.Fatalf("fixture emitted a non-nullable Time translator (%s); "+
				"the nullable-only path is what this test pins", tr.FuncName)
		}
		sawNullable = true
	}
	if !sawNullable {
		t.Fatal("fixture emitted no Time translator at all")
	}

	apiCtx.ModelsPackage = "models"
	body := renderComparatorTranslate(t, apiCtx)
	if !strings.Contains(body, "time.Time") {
		t.Fatalf("nullable Time translator does not reference time.Time:\n%s", body)
	}
	file := string(gen.BuildAPIComparatorTranslateFileForTest("graph", apiCtx, body))
	if !strings.Contains(file, "\"time\"") {
		t.Errorf("comparator_translate_gen.go references time.Time but does not import \"time\":\n%s", file)
	}

	// The same nullable-only fixture is the one that exercises the
	// range-carry-onto-twin branch: `NumericRange` / `TimeRange` are declared
	// alongside whichever form of the family is emitted (PRD §26.4 Rule 3),
	// and here only the twin is. Without the carry, the emitted
	// `NullableTimeComparator.between: TimeRange` names an input the schema
	// never declares and gqlgen rejects the document — a failure neither
	// example module can reach.
	shared := renderAPISharedSchema(t, apiCtx)
	if !strings.Contains(shared, "input NullableTimeComparator {") {
		t.Fatalf("fixture emitted no nullable Time comparator:\n%s", shared)
	}
	if strings.Contains(shared, "input TimeComparator {") {
		t.Fatalf("fixture emitted the base TimeComparator; the twin-only path is not exercised:\n%s", shared)
	}
	if !strings.Contains(shared, "input TimeRange {") {
		t.Errorf("NullableTimeComparator names TimeRange but the schema never declares it:\n%s", shared)
	}
	if strings.Count(shared, "input TimeRange {") != 1 {
		t.Errorf("TimeRange declared %d times, want exactly 1:\n%s",
			strings.Count(shared, "input TimeRange {"), shared)
	}
}

// TestComparatorFamilies_operandScalarsAreDeclared is the operand-scalar pin. A
// comparator's operand type is fixed by its FAMILY, while a column's own
// field-type scalar is picked from its GO TYPE — and the two disagree for the
// only two families whose operands name a custom scalar:
//
//   - a `types.DateTime` column declares `scalar DateTime` for its field but
//     filters through the Time family, whose operands say `Time`;
//   - a `decimal.NullDecimal` column declares `scalar NullDecimal` but both
//     forms of its comparator take `Decimal` operands.
//
// A project that binds its timestamps to `types.DateTime` globally therefore
// has no `time.Time` column at all, and once emitted
// `input TimeComparator { eq: Time … }` against a scalar the schema never
// declared — gqlgen rejects the document. Neither PostgreSQL example reaches
// this shape, so nothing else catches it.
//
// The assertion is deliberately general: every scalar named by any emitted
// comparator input must be declared. It closes over the rule (PRD §26.4
// Rule 3) rather than the two known instances, so a future family with a
// custom-scalar operand cannot reintroduce the gap.
func TestComparatorFamilies_operandScalarsAreDeclared(t *testing.T) {
	tests := []struct {
		name string
		// dateTimeBinding routes timestamps to types.DateTime / NullDateTime
		// so no column's Go type is literally time.Time.
		dateTimeBinding bool
		columns         []parserpkg.Column
		wantScalar      string
	}{
		{
			name:            "time operands with no time.Time column",
			dateTimeBinding: true,
			columns: []parserpkg.Column{
				{Name: "occurred_at", Type: "timestamptz"},
				{Name: "processed_at", Type: "timestamptz", Nullable: true},
			},
			wantScalar: "Time",
		},
		{
			name:            "decimal operands with only a nullable decimal column",
			dateTimeBinding: true,
			columns: []parserpkg.Column{
				{Name: "adjustment", Type: "numeric", Nullable: true},
			},
			wantScalar: "Decimal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols := append([]parserpkg.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
			}, tt.columns...)
			in := apiTestInput(t, &parserpkg.Schema{
				Tables: []parserpkg.Table{{Name: "events", Columns: cols}},
			})
			if tt.dateTimeBinding {
				configureNullVariantOverrides(t, in)
			}
			apiCtx := buildProjectionAPIContext(t, in)
			out := renderAPISharedSchema(t, apiCtx)

			// No column's Go type produces the operand scalar on its own —
			// that is what makes this the interesting case.
			for _, s := range apiCtx.UsedScalars {
				if s.Name != tt.wantScalar {
					continue
				}
				if s.GoType == "time.Time" && tt.wantScalar == "Time" {
					// Registered by the operand path, not a field type; a
					// column whose Go type is literally time.Time would make
					// the fixture prove nothing.
					for _, tbl := range apiCtx.Tables {
						for _, f := range tbl.Fields {
							if f.GraphQLBare == "Time" {
								t.Fatalf("fixture has a time.Time column (%s); "+
									"the operand-registration path is not exercised", f.SQLName)
							}
						}
					}
				}
			}

			// Every scalar any emitted comparator input names must be declared.
			declared := map[string]bool{}
			for _, s := range apiCtx.UsedScalars {
				declared[s.Name] = true
			}
			for _, fam := range apiCtx.ComparatorFamilies {
				for _, op := range fam.Operators {
					name := strings.Trim(op.Type, "[]!")
					switch name {
					case "String", "ID", "Float", "Boolean", "Int":
						continue // spec built-ins need no declaration
					}
					if strings.HasSuffix(name, "Range") {
						continue // a companion input, not a scalar
					}
					if !declared[name] {
						t.Errorf("%s names operand scalar %q, which the schema never declares:\n%s",
							fam.Name, name, out)
					}
				}
			}
			if !declared[tt.wantScalar] {
				t.Errorf("scalar %s not declared at all", tt.wantScalar)
			}
		})
	}
}

// TestInputTranslate_NullableSizedIntGoesThroughALocal is the nullable sized-int pin.
//
// gqlgen binds the GraphQL `Int` scalar to Go `int` and emits `*int` for a
// nullable field; the model size-narrows to the column's native width and
// stores `omittable.Value[*int32]`. Pointers thread through each other only
// when their pointee types agree, and Go cannot convert a pointee in
// expression position — `int32(p)` is illegal and `&int32(*p)` is not
// addressable — so the value has to go through a local.
//
// The translator once emitted `omittable.Set(in.RetryCount)`, which
// does not compile. That was a hard stop for every consumer with a nullable
// INTEGER / BIGINT / SMALLINT column and `api.graphql` enabled; it survived
// because no example had one. Both the create and the update translator are
// asserted — the bug was in a helper both call.
func TestInputTranslate_NullableSizedIntGoesThroughALocal(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{
			{
				Name: "events",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "action", Type: "text"},
					// The defect's shape: nullable, and a width gqlgen's Int
					// binding does not share.
					{Name: "retry_count", Type: "integer", Nullable: true},
					{Name: "big_count", Type: "bigint", Nullable: true},
					// Nullable bool needs no conversion — *bool threads into
					// Value[*bool] directly. Asserted so the fix stays
					// narrow and does not wrap every nullable field.
					{Name: "succeeded", Type: "boolean", Nullable: true},
				},
			},
		},
	}
	apiCtx := projectionAPIContext(t, schema)
	apiCtx.ModelsPackage = "models"
	out := renderGqlmodelTemplate(t, "api/input-translate", apiCtx)

	for _, want := range []string{
		"\t\tv := int32(*in.RetryCount)\n\t\tout.RetryCount = omittable.Set(&v)",
		"\t\tv := int64(*in.BigCount)\n\t\tout.BigCount = omittable.Set(&v)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("input translator does not convert through a local:\nwant:\n%s\ngot:\n%s", want, out)
		}
	}
	// The uncastable form must appear nowhere.
	for _, bad := range []string{
		"omittable.Set(in.RetryCount)",
		"omittable.Set(in.BigCount)",
	} {
		if strings.Contains(out, bad) {
			t.Errorf("input translator still threads the pointer unconverted (%q):\n%s", bad, out)
		}
	}
	// A nullable bool is left alone — the pointee types already agree.
	if !strings.Contains(out, "omittable.Set(in.Succeeded)") {
		t.Errorf("nullable bool should thread through unconverted:\n%s", out)
	}
	if strings.Contains(out, "v := bool(") {
		t.Errorf("nullable bool was needlessly routed through a local:\n%s", out)
	}

	// Both translators are covered: the create body assigns inside a
	// nil-check, the update body does the same for every field.
	createIdx := strings.Index(out, "func translateCreateEventInput")
	updateIdx := strings.Index(out, "func translateUpdateEventInput")
	if createIdx < 0 || updateIdx < 0 {
		t.Fatalf("expected both create and update translators:\n%s", out)
	}
	if !strings.Contains(out[createIdx:updateIdx], "v := int32(*in.RetryCount)") {
		t.Error("create translator does not convert through a local")
	}
	if !strings.Contains(out[updateIdx:], "v := int32(*in.RetryCount)") {
		t.Error("update translator does not convert through a local")
	}
}

// TestInputTranslate_Float32ColumnsAreCast is the float32 pin — the
// float sibling of the nullable sized-int pin, on the same helper.
//
// gqlgen binds the GraphQL `Float` scalar to Go `float64`, but sqlgen resolves
// a `real` / `float4` (PostgreSQL) or `float` (MySQL) column to `float32`.
// `isNarrowedNumericGoType` listed only the integer widths, so `CastTo` came
// back empty and NO arm converted:
//
//	out.Reading = in.Reading                    // float64 -> float32
//	out.OffsetVal = omittable.Set(in.OffsetVal) // *float64 -> Value[*float32]
//	out.Reading = omittable.Set(*in.Reading)    // float64 -> Value[float32]
//
// None of the three compiles. This is wider than the sized-int shape: the sized-int
// bug only reached the nullable arm, because gqlgen emits bare `int` for a
// required `Int` field and funcInputCoerceBare already cast it. Here the
// required arms broke too. Long unexercised — no
// example module has a `real` column with `api.graphql` enabled.
//
// `float64` columns are asserted untouched: gqlgen binds them directly, so a
// cast would be a no-op self-conversion and `unconvert` would flag it.
func TestInputTranslate_Float32ColumnsAreCast(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{
			{
				Name: "sensors",
				Columns: []parserpkg.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "name", Type: "text"},
					// The defect's shape on both nullabilities.
					{Name: "reading", Type: "real"},
					{Name: "offset_val", Type: "real", Nullable: true},
					// float64 control — gqlgen's own binding, no cast wanted.
					{Name: "precise", Type: "double precision"},
					{Name: "drift", Type: "double precision", Nullable: true},
				},
			},
		},
	}
	apiCtx := projectionAPIContext(t, schema)
	apiCtx.ModelsPackage = "models"
	out := renderGqlmodelTemplate(t, "api/input-translate", apiCtx)

	createIdx := strings.Index(out, "func translateCreateSensorInput")
	updateIdx := strings.Index(out, "func translateUpdateSensorInput")
	if createIdx < 0 || updateIdx < 0 {
		t.Fatalf("expected both create and update translators:\n%s", out)
	}
	create, update := out[createIdx:updateIdx], out[updateIdx:]

	// All three arms convert. The nullable one goes through a local because a
	// pointee cannot be converted in expression position (PointerCast).
	for _, tt := range []struct {
		arm  string
		body string
		want string
	}{
		{arm: "create/NOT NULL", body: create, want: "out.Reading = float32(in.Reading)"},
		{arm: "create/nullable", body: create, want: "v := float32(*in.OffsetVal)\n\t\tout.OffsetVal = omittable.Set(&v)"},
		{arm: "update/NOT NULL", body: update, want: "out.Reading = omittable.Set(float32(*in.Reading))"},
		{arm: "update/nullable", body: update, want: "v := float32(*in.OffsetVal)\n\t\tout.OffsetVal = omittable.Set(&v)"},
	} {
		if !strings.Contains(tt.body, tt.want) {
			t.Errorf("%s arm does not cast to float32:\nwant:\n%s\ngot:\n%s", tt.arm, tt.want, tt.body)
		}
	}

	// The uncastable forms must appear nowhere.
	for _, bad := range []string{
		"out.Reading = in.Reading",
		"omittable.Set(in.OffsetVal)",
		"omittable.Set(*in.Reading)",
	} {
		if strings.Contains(out, bad) {
			t.Errorf("float32 column still threaded through unconverted (%q):\n%s", bad, out)
		}
	}

	// float64 columns are left alone on every arm — a self-conversion here
	// would trip `unconvert`.
	for _, want := range []string{
		"out.Precise = in.Precise",
		"omittable.Set(in.Drift)",
		"omittable.Set(*in.Precise)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("float64 column should thread through unconverted (%q):\n%s", want, out)
		}
	}
	if strings.Contains(out, "float64(") {
		t.Errorf("float64 column was needlessly cast:\n%s", out)
	}
}
