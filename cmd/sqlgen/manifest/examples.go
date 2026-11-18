package manifest

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// exampleSpec carries what an entity's canonical examples (PRD §30.2
// include_examples, §30.4.2 examples.read / examples.write) are rendered from.
// Every name in a line is read off data the builder already holds — the
// entity's own methods[] (which are gated exactly as the templates are), its
// filter type, the generated input fields and the resolved column types — so
// an example cannot call a method the entity lacks or set a field its input
// struct does not declare.
//
// Lines are written for a caller outside the models package: generated names
// are qualified with output.package, and the client is reached through
// conventions.client_entry_points (`client.<Entity>()`) with `ctx` in scope.
type exampleSpec struct {
	pkg      string
	accessor string
	methods  Methods
	filter   string
	pkCols   []gen.ColumnContext
	// columns is every column of the entity, for reading a filter field's
	// real column type.
	columns []gen.ColumnContext
	// uuidNew is the package's resolved UUID integration's v4 constructor
	// (PRD §7.4 "Generating UUID values"); the three supported libraries
	// spell it differently, so a fixed spelling compiles against one only.
	uuidNew string
	// composite reports a multi-column key, whose by-PK methods take the
	// generated <Entity>PK struct.
	composite    bool
	filterFields []gen.FilterFieldContext
	createFields []gen.InputFieldContext
	updateFields []gen.InputFieldContext
	// enumValues maps a generated enum type (and its named slice type) to the
	// enum's first value, so an enum-typed field gets a real member.
	enumValues map[string]string
	enumSlices map[string]string
}

// clientAccessor mirrors gen's entityAccessorName (unexported there): a table
// is reached by its plural struct name (`client.Products()`), a view by its
// singular one (`client.ProductSummary()`).
func clientAccessor(structName string, isView bool) string {
	if isView {
		return "client." + structName + "()"
	}
	return "client." + gen.StructNamePlural(structName) + "()"
}

func newExampleSpec(in BuildInput, e Entity) exampleSpec {
	uuidInteg := in.UUID
	if uuidInteg.V4 == "" {
		uuidInteg = gotype.UUIDIntegrationFor("")
	}
	s := exampleSpec{
		pkg:        in.Config.Output.Package,
		methods:    e.Methods,
		filter:     e.Filter.Type,
		uuidNew:    uuidInteg.V4,
		enumValues: make(map[string]string, len(in.Enums)),
		enumSlices: make(map[string]string, len(in.Enums)),
	}
	for _, en := range in.Enums {
		// A MySQL SET context shares the shape but not the Go type; its
		// columns fall back to a zero value below rather than a guessed
		// conversion.
		if en.FromSet || len(en.Values) == 0 {
			continue
		}
		s.enumValues[en.GoTypeName] = en.Values[0]
		if en.SliceGoTypeName != "" {
			s.enumSlices[en.SliceGoTypeName] = en.GoTypeName
		}
	}
	return s
}

// buildTableExamples renders a table's read and write examples from its
// already-built entity (methods, filter) and context (input fields, columns).
func buildTableExamples(in BuildInput, tc gen.TableContext, e Entity) *Examples {
	s := newExampleSpec(in, e)
	s.accessor = clientAccessor(tc.StructName, false)
	s.pkCols = tc.PKColumns
	s.columns = tc.Columns
	s.composite = tc.CompositePK
	s.filterFields = tc.FilterFields
	s.createFields = tc.CreateInputFields
	s.updateFields = tc.UpdateInputFields
	return &Examples{Read: s.readLines(), Write: s.writeLines()}
}

// buildViewExamples renders a view's examples. A view has no write path, so
// examples.write is empty.
func buildViewExamples(in BuildInput, vc gen.ViewContext, e Entity) *Examples {
	s := newExampleSpec(in, e)
	s.accessor = clientAccessor(vc.StructName, true)
	s.pkCols = vc.PKColumns
	s.columns = vc.Columns
	s.composite = vc.CompositePK
	s.filterFields = vc.FilterFields
	return &Examples{Read: s.readLines(), Write: []string{}}
}

func findMethod(methods []Method, name string) (Method, bool) {
	for _, m := range methods {
		if m.Name == name {
			return m, true
		}
	}
	return Method{}, false
}

// readLines is a Get by primary key (when the entity has one) and a GetMany
// over a real filterable column.
func (s exampleSpec) readLines() []string {
	out := []string{}
	if m, ok := findMethod(s.methods.Query, "Get"); ok && len(m.Params) == 1 {
		out = append(out, "row, err := "+s.accessor+".Get(ctx, "+s.pkArg(m.Params[0].Type)+")")
	}
	if m, ok := findMethod(s.methods.Query, "GetMany"); ok && len(m.Params) == 1 {
		input := strings.TrimPrefix(m.Params[0].Type, "*")
		body := ""
		if f, ok := s.filterExpr(); ok {
			body = "Filter: &" + s.qualify(s.filter) + "{" + f + "}"
		}
		out = append(out, "rows, err := "+s.accessor+".GetMany(ctx, &"+s.qualify(input)+"{"+body+"})")
	}
	return out
}

// writeLines is a Create with every required create-input field, and an
// Update by primary key setting one field through omittable.
func (s exampleSpec) writeLines() []string {
	out := []string{}
	if m, ok := findMethod(s.methods.Mutation, "Create"); ok && len(m.Params) == 1 {
		fields := []string{}
		for _, f := range s.createFields {
			if !f.Required {
				continue
			}
			lit, _ := s.literal(f.GoType, false)
			fields = append(fields, f.FieldName+": "+lit)
		}
		input := strings.TrimPrefix(m.Params[0].Type, "*")
		out = append(out, "created, err := "+s.accessor+".Create(ctx, &"+s.qualify(input)+"{"+strings.Join(fields, ", ")+"})")
	}
	if m, ok := findMethod(s.methods.Mutation, "Update"); ok && len(m.Params) == 2 {
		input := strings.TrimPrefix(m.Params[1].Type, "*")
		out = append(out, "updated, err := "+s.accessor+".Update(ctx, "+s.pkArg(m.Params[0].Type)+", &"+s.qualify(input)+"{"+s.updateField()+"})")
	}
	return out
}

// updateField sets the first update-input field that has a readable literal,
// falling back to the first field's zero value; empty when the input has no
// fields (a key-only table).
func (s exampleSpec) updateField() string {
	pick, pickLit := "", ""
	for _, f := range s.updateFields {
		inner, ok := omittableInner(f.GoType)
		if !ok {
			continue
		}
		lit, nice := s.literal(inner, true)
		if nice {
			return f.FieldName + ": omittable.Set(" + lit + ")"
		}
		if pick == "" {
			pick, pickLit = f.FieldName, lit
		}
	}
	if pick == "" {
		return ""
	}
	return pick + ": omittable.Set(" + pickLit + ")"
}

func omittableInner(goType string) (string, bool) {
	inner, ok := strings.CutPrefix(goType, "omittable.Value[")
	if !ok {
		return "", false
	}
	return strings.CutSuffix(inner, "]")
}

// pkArg renders the primary-key argument of a by-PK method: the generated
// <Entity>PK struct with every key field set for a composite key, a value of
// the key's Go type otherwise.
func (s exampleSpec) pkArg(paramType string) string {
	if !s.composite {
		lit, _ := s.literal(paramType, false)
		return lit
	}
	fields := make([]string, 0, len(s.pkCols))
	for _, c := range s.pkCols {
		lit, _ := s.literal(c.GoType, false)
		fields = append(fields, c.FieldName+": "+lit)
	}
	return s.qualify(paramType) + "{" + strings.Join(fields, ", ") + "}"
}

// filterRank orders the comparator families an example filters on: an
// equality on a plain string reads most naturally, an identifier least. A
// nullable family is filtered by its Null check, below all of them, and a
// primary-key column — already shown by Get — ranks below every other column.
var filterRank = map[string]int{"String": 0, "Enum": 1, "Number": 2, "Bool": 3, "Time": 4, "ID": 5}

const (
	nullableFilterRank = 6
	pkFilterPenalty    = 10
)

// filterExpr renders one `<Field>: &comparator.<Family>{...}` filter entry
// over the entity's real filter fields, with the comparator type the
// generated <Entity>Filter declares. The base families are matched by Eq;
// the Nullable* variants embed their base, which a composite literal cannot
// address, so they are matched by their own Null field.
func (s exampleSpec) filterExpr() (string, bool) {
	best, bestRank := "", -1
	for _, ff := range s.filterFields {
		if !ff.Filterable {
			continue
		}
		comp := strings.TrimPrefix(ff.ComparatorType, "*")
		name, ok := strings.CutPrefix(comp, "comparator.")
		if !ok {
			continue
		}
		family, param, _ := strings.Cut(name, "[")
		param = strings.TrimSuffix(param, "]")

		var rank int
		var expr string
		if strings.HasPrefix(family, "Nullable") {
			rank, expr = nullableFilterRank, "Null: new(true)"
		} else {
			r, known := filterRank[family]
			if !known {
				continue
			}
			var lit string
			switch family {
			case "ID":
				lit = s.idLiteral(ff.ColumnName)
			default:
				eqType := param
				switch family {
				case "String":
					eqType = "string"
				case "Bool":
					eqType = "bool"
				case "Time":
					eqType = "time.Time"
				}
				l, nice := s.literal(eqType, true)
				if !nice {
					continue
				}
				lit = l
			}
			rank, expr = r, "Eq: new("+lit+")"
		}
		if slices.ContainsFunc(s.pkCols, func(c gen.ColumnContext) bool { return c.Name == ff.ColumnName }) {
			rank += pkFilterPenalty
		}
		if bestRank == -1 || rank < bestRank {
			best, bestRank = ff.FieldName+": &"+s.qualify(comp)+"{"+expr+"}", rank
		}
	}
	return best, bestRank != -1
}

// exampleUUIDString is a well-formed UUID in its canonical text form.
const exampleUUIDString = `"123e4567-e89b-12d3-a456-426614174000"`

// idLiteral renders the Eq value of a comparator.ID filter on column. The
// family compares through a string whatever the column stores (a text key, an
// integer or UUID foreign key, a bare uuid column), and the database parses
// that string as the column's own type — so the example must be a string the
// column accepts: a UUID for a uuid column, a number for a numeric one, a
// timestamp for a time one.
func (s exampleSpec) idLiteral(column string) string {
	i := slices.IndexFunc(s.columns, func(c gen.ColumnContext) bool { return c.Name == column })
	if i < 0 {
		return `"example"`
	}
	col := s.columns[i]
	base := strings.TrimPrefix(col.GoType, "*")
	if underlying, ok := gotype.StdNullUnderlying(base); ok {
		base = underlying
	}
	switch {
	case strings.EqualFold(col.SQLType, "uuid"), base == "uuid.UUID", base == "uuid.NullUUID":
		return exampleUUIDString
	case base == "int", sizedNumericTypes[base], base == "decimal.Decimal", base == "decimal.NullDecimal":
		return `"1"`
	case base == "time.Time":
		// The one timestamp spelling all three dialects parse.
		return `"2024-01-01 00:00:00"`
	default:
		return `"example"`
	}
}

var sizedNumericTypes = map[string]bool{
	"int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true,
}

// literal renders a Go expression of goType. typed asks for an expression
// whose type is exactly goType rather than an untyped constant — needed where
// the type is inferred (new(...), omittable.Set(...)), not where it is
// assigned to a declared field. The second result reports a readable value;
// false means the universal zero value *new(T), which is valid Go for any
// type but carries no example data.
func (s exampleSpec) literal(goType string, typed bool) (string, bool) {
	if lit, ok := scalarLiterals[goType]; ok {
		return lit, true
	}
	if goType == "uuid.UUID" {
		return s.uuidNew, true
	}
	if sizedNumericTypes[goType] {
		if typed {
			return goType + "(1)", true
		}
		return "1", true
	}
	if lit, nice, ok := s.compositeLiteral(goType); ok {
		return lit, nice
	}
	return "*new(" + s.qualify(goType) + ")", false
}

// scalarLiterals maps the scalar Go types with a fixed readable example to
// that example. Each is already of exactly its type, so typed does not apply.
// uuid.UUID is absent: its constructor depends on the resolved UUID library
// (exampleSpec.uuidNew).
var scalarLiterals = map[string]string{
	"string":          `"example"`,
	"bool":            "true",
	"int":             "1",
	"time.Time":       "time.Now()",
	"time.Duration":   "time.Minute",
	"decimal.Decimal": "decimal.NewFromInt(1)",
	"[]byte":          `[]byte("example")`,
	"types.JSON":      `types.JSON("{}")`,
	"net.IP":          `net.ParseIP("192.0.2.1")`,
	"netip.Addr":      `netip.MustParseAddr("192.0.2.1")`,
	"netip.Prefix":    `netip.MustParsePrefix("192.0.2.0/24")`,
}

// compositeLiteral renders pointers, slices, enums and enum slices. The third
// result reports whether goType is one of those shapes with a rendering here;
// false leaves the caller to fall back to the zero value.
func (s exampleSpec) compositeLiteral(goType string) (lit string, nice, ok bool) {
	if elem, isPtr := strings.CutPrefix(goType, "*"); isPtr {
		if inner, innerNice := s.literal(elem, true); innerNice {
			return "new(" + inner + ")", true, true
		}
		return "new(" + s.qualify(elem) + ")", false, true
	}
	if elem, isSlice := strings.CutPrefix(goType, "[]"); isSlice {
		if inner, innerNice := s.literal(elem, false); innerNice {
			return s.qualify(goType) + "{" + inner + "}", true, true
		}
	}
	if v, isEnum := s.enumValues[goType]; isEnum {
		return s.qualify(goType) + "(" + strconv.Quote(v) + ")", true, true
	}
	if elem, isEnumSlice := s.enumSlices[goType]; isEnumSlice {
		inner, _ := s.literal(elem, false)
		return s.qualify(goType) + "{" + inner + "}", true, true
	}
	return "", false, false
}

var goIdent = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// qualify prefixes every package-local generated type in a type expression
// with output.package. Manifest types spell those unqualified (`*CreateUserInput`,
// `comparator.Enum[UserRole]`); an exported identifier not already selected
// from a package is one of them, since every predeclared Go type is lowercase.
func (s exampleSpec) qualify(goType string) string {
	var b strings.Builder
	last := 0
	for _, loc := range goIdent.FindAllStringIndex(goType, -1) {
		start, end := loc[0], loc[1]
		ident := goType[start:end]
		selected := start > 0 && goType[start-1] == '.'
		selector := end < len(goType) && goType[end] == '.'
		if selected || selector || ident[0] < 'A' || ident[0] > 'Z' {
			continue
		}
		b.WriteString(goType[last:start])
		b.WriteString(s.pkg + "." + ident)
		last = end
	}
	b.WriteString(goType[last:])
	return b.String()
}
