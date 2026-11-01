package comparator_test

import (
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

// everyDialect is every dialect and driver a generated filter hands a
// comparator. Each test passes the column through QuoteIdentifier, as the
// generated ToConditions does (PRD §11.3), so the quoted token is what both
// Clause and Column must carry.
var everyDialect = []struct {
	name string
	d    sql.Dialect
}{
	{"postgres pgx", pgDialect},
	{"postgres stdlib", pgStdDialect},
	{"mysql", mysqlDialect},
	{"sqlite", sqliteDialect},
}

// TestScalarOperators_SameClauseOnEveryDialect pins PRD §11.3: a universal
// comparator uses the dialect only for In / Nin, so every other operator
// produces the same condition on every dialect and driver — where a §11.5
// builder method of the same name exists, its clause — with the operand as the
// condition's value and the column token in both Clause and Column.
func TestScalarOperators_SameClauseOnEveryDialect(t *testing.T) {
	at := time.Date(2025, 1, 15, 9, 30, 0, 0, time.UTC)
	later := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	payload := []byte{0x01, 0xff}

	one := func(clause string, value any) func(string) []sql.Condition {
		return func(c string) []sql.Condition {
			return []sql.Condition{{Clause: c + clause, Value: value, Column: c}}
		}
	}

	tests := []struct {
		name  string
		parse func(col string, d sql.Dialect) []sql.Condition
		want  func(col string) []sql.Condition
	}{
		{"ID.Eq", comparator.ID{Eq: new("a")}.Parse, one(" = $", "a")},
		{"ID.Neq", comparator.ID{Neq: new("a")}.Parse, one(" != $", "a")},
		{"ID.Gt", comparator.ID{Gt: new("a")}.Parse, one(" > $", "a")},
		{"ID.Gte", comparator.ID{Gte: new("a")}.Parse, one(" >= $", "a")},
		{"ID.Lt", comparator.ID{Lt: new("a")}.Parse, one(" < $", "a")},
		{"ID.Lte", comparator.ID{Lte: new("a")}.Parse, one(" <= $", "a")},

		{"String.Eq", comparator.String{Eq: new("s")}.Parse, one(" = $", "s")},
		{"String.Neq", comparator.String{Neq: new("s")}.Parse, one(" != $", "s")},
		{"String.Like", comparator.String{Like: new("%s_")}.Parse, one(" LIKE $", "%s_")},
		{"String.NLike", comparator.String{NLike: new("%s_")}.Parse, one(" NOT LIKE $", "%s_")},
		{"String.Gt", comparator.String{Gt: new("s")}.Parse, one(" > $", "s")},
		{"String.Gte", comparator.String{Gte: new("s")}.Parse, one(" >= $", "s")},
		{"String.Lt", comparator.String{Lt: new("s")}.Parse, one(" < $", "s")},
		{"String.Lte", comparator.String{Lte: new("s")}.Parse, one(" <= $", "s")},
		// The pattern operators' SQL is not stated by the PRD (a PRD gap);
		// these cases pin the SQL the code emits.
		{"String.Contains", comparator.String{Contains: new("s")}.Parse, one(" LIKE $", "%s%")},
		{"String.StartsWith", comparator.String{StartsWith: new("s")}.Parse, one(" LIKE $", "s%")},
		{"String.EndsWith", comparator.String{EndsWith: new("s")}.Parse, one(" LIKE $", "%s")},

		{"Number.Eq", comparator.Number[int64]{Eq: new(int64(7))}.Parse, one(" = $", int64(7))},
		{"Number.Neq", comparator.Number[int64]{Neq: new(int64(7))}.Parse, one(" != $", int64(7))},
		{"Number.Gt", comparator.Number[int64]{Gt: new(int64(7))}.Parse, one(" > $", int64(7))},
		{"Number.Gte", comparator.Number[int64]{Gte: new(int64(7))}.Parse, one(" >= $", int64(7))},
		{"Number.Lt", comparator.Number[int64]{Lt: new(int64(7))}.Parse, one(" < $", int64(7))},
		{"Number.Lte", comparator.Number[int64]{Lte: new(int64(7))}.Parse, one(" <= $", int64(7))},
		{
			"Number.Between",
			comparator.Number[int64]{Between: &comparator.Range[int64]{Start: 1, End: 9}}.Parse,
			one(" BETWEEN $ AND $", sql.Range{Start: int64(1), End: int64(9)}),
		},
		{
			"Number.NBetween",
			comparator.Number[int64]{NBetween: &comparator.Range[int64]{Start: 1, End: 9}}.Parse,
			one(" NOT BETWEEN $ AND $", sql.Range{Start: int64(1), End: int64(9)}),
		},

		{"Bool.Eq", comparator.Bool{Eq: new(true)}.Parse, one(" = $", true)},
		{"Bool.Neq", comparator.Bool{Neq: new(false)}.Parse, one(" != $", false)},

		{"Time.Eq", comparator.Time{Eq: &at}.Parse, one(" = $", at)},
		{"Time.Neq", comparator.Time{Neq: &at}.Parse, one(" != $", at)},
		{"Time.Gt", comparator.Time{Gt: &at}.Parse, one(" > $", at)},
		{"Time.Gte", comparator.Time{Gte: &at}.Parse, one(" >= $", at)},
		{"Time.Lt", comparator.Time{Lt: &at}.Parse, one(" < $", at)},
		{"Time.Lte", comparator.Time{Lte: &at}.Parse, one(" <= $", at)},
		{
			"Time.Between",
			comparator.Time{Between: &comparator.Range[time.Time]{Start: at, End: later}}.Parse,
			one(" BETWEEN $ AND $", sql.Range{Start: at, End: later}),
		},
		{
			"Time.NBetween",
			comparator.Time{NBetween: &comparator.Range[time.Time]{Start: at, End: later}}.Parse,
			one(" NOT BETWEEN $ AND $", sql.Range{Start: at, End: later}),
		},

		{"Enum.Eq", comparator.Enum[orderStatus]{Eq: new(orderStatus("paid"))}.Parse, one(" = $", orderStatus("paid"))},
		{"Enum.Neq", comparator.Enum[orderStatus]{Neq: new(orderStatus("paid"))}.Parse, one(" != $", orderStatus("paid"))},

		{"Opaque.Eq", comparator.Opaque[[]byte]{Eq: &payload}.Parse, one(" = $", payload)},
		{"Opaque.Neq", comparator.Opaque[[]byte]{Neq: &payload}.Parse, one(" != $", payload)},
		{"Opaque.Gt", comparator.Opaque[[]byte]{Gt: &payload}.Parse, one(" > $", payload)},
		{"Opaque.Gte", comparator.Opaque[[]byte]{Gte: &payload}.Parse, one(" >= $", payload)},
		{"Opaque.Lt", comparator.Opaque[[]byte]{Lt: &payload}.Parse, one(" < $", payload)},
		{"Opaque.Lte", comparator.Opaque[[]byte]{Lte: &payload}.Parse, one(" <= $", payload)},

		// The Nullable variants: Null tests the column, and the base family's
		// operators are inherited.
		{"NullableID.Null", comparator.NullableID{Null: new(true)}.Parse, one(" IS NULL", nil)},
		{"NullableString.Null", comparator.NullableString{Null: new(false)}.Parse, one(" IS NOT NULL", nil)},
		{"NullableNumber.Null", comparator.NullableNumber[int64]{Null: new(true)}.Parse, one(" IS NULL", nil)},
		{"NullableBool.Null", comparator.NullableBool{Null: new(false)}.Parse, one(" IS NOT NULL", nil)},
		{"NullableBool.Eq", comparator.NullableBool{Bool: comparator.Bool{Eq: new(true)}}.Parse, one(" = $", true)},
		{"NullableTime.Null", comparator.NullableTime{Null: new(true)}.Parse, one(" IS NULL", nil)},
		{"NullableEnum.Null", comparator.NullableEnum[orderStatus]{Null: new(false)}.Parse, one(" IS NOT NULL", nil)},
		{"NullableOpaque.Null", comparator.NullableOpaque[[]byte]{Null: new(true)}.Parse, one(" IS NULL", nil)},

		// Custom is appended as written, and a ConditionBuilder condition is
		// one of the things it accepts (PRD §11.4, §11.5).
		{"ID.Custom", comparator.ID{Custom: []sql.Condition{sql.Raw("id ~ $", "^a")}}.Parse, customOnly(sql.Raw("id ~ $", "^a"))},
		{"String.Custom", comparator.String{Custom: []sql.Condition{sql.Where("lower(name)").Like("a%")}}.Parse, customOnly(sql.Where("lower(name)").Like("a%"))},
		{"Number.Custom", comparator.Number[int64]{Custom: []sql.Condition{sql.Raw("n % $ = 0", 2)}}.Parse, customOnly(sql.Raw("n % $ = 0", 2))},
		{"Bool.Custom", comparator.Bool{Custom: []sql.Condition{sql.Raw("NOT b")}}.Parse, customOnly(sql.Raw("NOT b"))},
		{"Time.Custom", comparator.Time{Custom: []sql.Condition{sql.Raw("t > $", at)}}.Parse, customOnly(sql.Raw("t > $", at))},
		{"Enum.Custom", comparator.Enum[orderStatus]{Custom: []sql.Condition{sql.Raw("e <> $", "x")}}.Parse, customOnly(sql.Raw("e <> $", "x"))},
		{"Opaque.Custom", comparator.Opaque[[]byte]{Custom: []sql.Condition{sql.Raw("length(p) > $", 4)}}.Parse, customOnly(sql.Raw("length(p) > $", 4))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, dd := range everyDialect {
				t.Run(dd.name, func(t *testing.T) {
					col := dd.d.QuoteIdentifier("c")
					if diff := cmp.Diff(tt.want(col), tt.parse(col, dd.d)); diff != "" {
						t.Errorf("%s.Parse(%s, %s) mismatch (-want +got):\n%s", tt.name, col, dd.name, diff)
					}
				})
			}
		})
	}
}

func customOnly(c sql.Condition) func(string) []sql.Condition {
	return func(string) []sql.Condition { return []sql.Condition{c} }
}

// TestSetOperators_RenderPerDialect pins the In / Nin split of PRD §11.2,
// §11.3 and Appendix A for every family that has the operators: one array
// parameter (`= ANY($1)` / `!= ALL($1)`) on PostgreSQL+pgx, one placeholder
// per value (`IN ($1, $2)` / `NOT IN ($1, $2)`) everywhere else — except
// Enum[T], which expands on pgx too. Each case checks the condition the
// comparator returns (Clause, Value and the Column it sets) and the WHERE the
// builder renders from it.
func TestSetOperators_RenderPerDialect(t *testing.T) {
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	b1, b2 := []byte{0x01}, []byte{0x02}

	const (
		pgAny   = `"c" = ANY($1)`
		pgAll   = `"c" != ALL($1)`
		pgIn    = `"c" IN ($1, $2)`
		pgNotIn = `"c" NOT IN ($1, $2)`
		myIn    = "`c` IN (?, ?)"
		myNotIn = "`c` NOT IN (?, ?)"
		ltIn    = `"c" IN (?, ?)`
		ltNotIn = `"c" NOT IN (?, ?)`
	)

	tests := []struct {
		name  string
		parse func(col string, d sql.Dialect) []sql.Condition
		// array is the one parameter pgx is sent when the family takes the
		// array form; elems are the expanded form's parameters.
		array any
		elems []any
		// expanded and pgx are the clause after the column in the expanded
		// and the array form; the where fields are the rendered WHERE per
		// dialect. pgxExpands marks the family that expands on pgx too.
		expanded, pgx                          string
		wherePgx, whereStd, whereMy, whereLite string
		pgxExpands                             bool
	}{
		{
			name: "ID.In", parse: comparator.ID{In: []string{"a", "b"}}.Parse,
			array: []string{"a", "b"}, elems: []any{"a", "b"},
			expanded: " IN $", pgx: " = ANY($)",
			wherePgx: pgAny, whereStd: pgIn, whereMy: myIn, whereLite: ltIn,
		},
		{
			name: "ID.Nin", parse: comparator.ID{Nin: []string{"a", "b"}}.Parse,
			array: []string{"a", "b"}, elems: []any{"a", "b"},
			expanded: " NOT IN $", pgx: " != ALL($)",
			wherePgx: pgAll, whereStd: pgNotIn, whereMy: myNotIn, whereLite: ltNotIn,
		},
		{
			name: "String.In", parse: comparator.String{In: []string{"x", "y"}}.Parse,
			array: []string{"x", "y"}, elems: []any{"x", "y"},
			expanded: " IN $", pgx: " = ANY($)",
			wherePgx: pgAny, whereStd: pgIn, whereMy: myIn, whereLite: ltIn,
		},
		{
			name: "String.Nin", parse: comparator.String{Nin: []string{"x", "y"}}.Parse,
			array: []string{"x", "y"}, elems: []any{"x", "y"},
			expanded: " NOT IN $", pgx: " != ALL($)",
			wherePgx: pgAll, whereStd: pgNotIn, whereMy: myNotIn, whereLite: ltNotIn,
		},
		{
			name: "Number.In", parse: comparator.Number[int32]{In: []int32{3, 4}}.Parse,
			array: []int32{3, 4}, elems: []any{int32(3), int32(4)},
			expanded: " IN $", pgx: " = ANY($)",
			wherePgx: pgAny, whereStd: pgIn, whereMy: myIn, whereLite: ltIn,
		},
		{
			name: "Number.Nin", parse: comparator.Number[int32]{Nin: []int32{3, 4}}.Parse,
			array: []int32{3, 4}, elems: []any{int32(3), int32(4)},
			expanded: " NOT IN $", pgx: " != ALL($)",
			wherePgx: pgAll, whereStd: pgNotIn, whereMy: myNotIn, whereLite: ltNotIn,
		},
		{
			name: "Time.In", parse: comparator.Time{In: []time.Time{t1, t2}}.Parse,
			array: []time.Time{t1, t2}, elems: []any{t1, t2},
			expanded: " IN $", pgx: " = ANY($)",
			wherePgx: pgAny, whereStd: pgIn, whereMy: myIn, whereLite: ltIn,
		},
		{
			name: "Time.Nin", parse: comparator.Time{Nin: []time.Time{t1, t2}}.Parse,
			array: []time.Time{t1, t2}, elems: []any{t1, t2},
			expanded: " NOT IN $", pgx: " != ALL($)",
			wherePgx: pgAll, whereStd: pgNotIn, whereMy: myNotIn, whereLite: ltNotIn,
		},
		{
			name: "Opaque.In", parse: comparator.Opaque[[]byte]{In: [][]byte{b1, b2}}.Parse,
			array: [][]byte{b1, b2}, elems: []any{b1, b2},
			expanded: " IN $", pgx: " = ANY($)",
			wherePgx: pgAny, whereStd: pgIn, whereMy: myIn, whereLite: ltIn,
		},
		{
			name: "Opaque.Nin", parse: comparator.Opaque[[]byte]{Nin: [][]byte{b1, b2}}.Parse,
			array: [][]byte{b1, b2}, elems: []any{b1, b2},
			expanded: " NOT IN $", pgx: " != ALL($)",
			wherePgx: pgAll, whereStd: pgNotIn, whereMy: myNotIn, whereLite: ltNotIn,
		},
		{
			name: "Enum.In", parse: comparator.Enum[orderStatus]{In: []orderStatus{"paid", "void"}}.Parse,
			elems:    []any{orderStatus("paid"), orderStatus("void")},
			expanded: " IN $", pgxExpands: true,
			wherePgx: pgIn, whereStd: pgIn, whereMy: myIn, whereLite: ltIn,
		},
		{
			name: "Enum.Nin", parse: comparator.Enum[orderStatus]{Nin: []orderStatus{"paid", "void"}}.Parse,
			elems:    []any{orderStatus("paid"), orderStatus("void")},
			expanded: " NOT IN $", pgxExpands: true,
			wherePgx: pgNotIn, whereStd: pgNotIn, whereMy: myNotIn, whereLite: ltNotIn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialects := []struct {
				name  string
				d     sql.Dialect
				array bool
				from  string
				where string
			}{
				{"postgres pgx", pgDialect, !tt.pgxExpands, `"t"`, tt.wherePgx},
				{"postgres stdlib", pgStdDialect, false, `"t"`, tt.whereStd},
				{"mysql", mysqlDialect, false, "`t`", tt.whereMy},
				{"sqlite", sqliteDialect, false, `"t"`, tt.whereLite},
			}
			for _, dd := range dialects {
				t.Run(dd.name, func(t *testing.T) {
					col := dd.d.QuoteIdentifier("c")
					want := sql.Condition{Clause: col + tt.expanded, Value: tt.elems, Column: col}
					wantArgs := len(tt.elems)
					if dd.array {
						want = sql.Condition{Clause: col + tt.pgx, Value: tt.array, Column: col}
						wantArgs = 1
					}
					got := tt.parse(col, dd.d)
					if diff := cmp.Diff([]sql.Condition{want}, got); diff != "" {
						t.Fatalf("%s.Parse(%s, %s) mismatch (-want +got):\n%s", tt.name, col, dd.name, diff)
					}

					gotSQL, gotArgs := sql.BuildSelect(dd.d, sql.Table{Name: "t"}, sql.SelectOptions{Conditions: got})
					if wantSQL := "SELECT * FROM " + dd.from + " WHERE " + dd.where; gotSQL != wantSQL {
						t.Errorf("BuildSelect(%s.Parse) on %s =\n  %s\nwant\n  %s", tt.name, dd.name, gotSQL, wantSQL)
					}
					if len(gotArgs) != wantArgs {
						t.Errorf("BuildSelect(%s.Parse) on %s bound %d args (%v), want %d", tt.name, dd.name, len(gotArgs), gotArgs, wantArgs)
					}
				})
			}
		})
	}
}

// TestJSON_Parse_PerDialect pins the JSON comparator's dialect table (PRD
// §11.2) on both PostgreSQL drivers and MySQL: the document operators take the
// dialect's own form, the PostgreSQL arms cast the column to jsonb, and Null
// tests the column itself with no cast on any of them.
func TestJSON_Parse_PerDialect(t *testing.T) {
	doc := any(`{"tier":"gold"}`)

	tests := []struct {
		name   string
		comp   comparator.NullableJSON
		pg, my sql.Condition
	}{
		{
			name: "Contains",
			comp: comparator.NullableJSON{JSON: comparator.JSON{Contains: &doc}},
			pg:   sql.Condition{Clause: `"c"::jsonb @> $`, Value: `{"tier":"gold"}`, Column: `"c"`},
			my:   sql.Condition{Clause: "JSON_CONTAINS(`c`, $)", Value: `{"tier":"gold"}`, Column: "`c`"},
		},
		{
			name: "HasKey",
			comp: comparator.NullableJSON{JSON: comparator.JSON{HasKey: new("tier")}},
			pg:   sql.Condition{Clause: `"c"::jsonb ? $`, Value: "tier", Column: `"c"`},
			my:   sql.Condition{Clause: "JSON_CONTAINS_PATH(`c`, 'one', $)", Value: "tier", Column: "`c`"},
		},
		{
			name: "Null",
			comp: comparator.NullableJSON{Null: new(true)},
			pg:   sql.Condition{Clause: `"c" IS NULL`, Column: `"c"`},
			my:   sql.Condition{Clause: "`c` IS NULL", Column: "`c`"},
		},
		{
			name: "Custom",
			comp: comparator.NullableJSON{JSON: comparator.JSON{Custom: []sql.Condition{sql.Raw("json_length(c) > $", 1)}}},
			pg:   sql.Raw("json_length(c) > $", 1),
			my:   sql.Raw("json_length(c) > $", 1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialects := []struct {
				name string
				d    sql.Dialect
				want sql.Condition
			}{
				{"postgres pgx", pgDialect, tt.pg},
				{"postgres stdlib", pgStdDialect, tt.pg},
				{"mysql", mysqlDialect, tt.my},
			}
			for _, dd := range dialects {
				t.Run(dd.name, func(t *testing.T) {
					got := tt.comp.Parse(dd.d.QuoteIdentifier("c"), dd.d)
					if diff := cmp.Diff([]sql.Condition{dd.want}, got); diff != "" {
						t.Errorf("NullableJSON{%s}.Parse on %s mismatch (-want +got):\n%s", tt.name, dd.name, diff)
					}
				})
			}
		})
	}
}

// TestPostgresOnlyComparators_BothDrivers pins the SQL operator each JSONB and
// Slice operator emits (PRD §11.2, PostgreSQL-only comparators) on both
// PostgreSQL drivers. The operand encoding is pinned by TestJSONB_Parse and
// TestSlice_Parse_ArrayLiteralQuoting; this test is about the operator.
// IsEmpty has no SQL operator in the PRD's table: its case pins the code's
// `= '{}'` clause.
func TestPostgresOnlyComparators_BothDrivers(t *testing.T) {
	doc := any(`{"a":1}`)

	tests := []struct {
		name   string
		parse  func(col string, d sql.Dialect) []sql.Condition
		clause string
	}{
		{"JSONB.HasKey", comparator.JSONB{HasKey: new("a")}.Parse, `"c" ? $`},
		{"JSONB.HasAnyKey", comparator.JSONB{HasAnyKey: []string{"a", "b"}}.Parse, `"c" ?| $`},
		{"JSONB.HasAllKeys", comparator.JSONB{HasAllKeys: []string{"a", "b"}}.Parse, `"c" ?& $`},
		{"JSONB.Contains", comparator.JSONB{Contains: &doc}.Parse, `"c" @> $`},
		{"JSONB.ContainedBy", comparator.JSONB{ContainedBy: &doc}.Parse, `"c" <@ $`},
		{"JSONB.PathExists", comparator.JSONB{PathExists: new("$.a")}.Parse, `"c" @? $`},
		{"Slice.ContainsAny", comparator.Slice[string]{ContainsAny: []string{"a"}}.Parse, `"c" && $`},
		{"Slice.ContainsAll", comparator.Slice[string]{ContainsAll: []string{"a"}}.Parse, `"c" @> $`},
		{"Slice.ContainedBy", comparator.Slice[string]{ContainedBy: []string{"a"}}.Parse, `"c" <@ $`},
		{"Slice.IsEmpty", comparator.Slice[string]{IsEmpty: new(true)}.Parse, `"c" = '{}'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, dd := range []struct {
				name string
				d    sql.Dialect
			}{
				{"postgres pgx", pgDialect},
				{"postgres stdlib", pgStdDialect},
			} {
				t.Run(dd.name, func(t *testing.T) {
					got := tt.parse(`"c"`, dd.d)
					if len(got) != 1 {
						t.Fatalf("%s.Parse on %s returned %d conditions, want 1: %v", tt.name, dd.name, len(got), got)
					}
					if got[0].Clause != tt.clause || got[0].Column != `"c"` {
						t.Errorf("%s.Parse on %s = {Clause: %s, Column: %s}, want {Clause: %s, Column: \"c\"}",
							tt.name, dd.name, got[0].Clause, got[0].Column, tt.clause)
					}
				})
			}
		})
	}
}

// TestOpaque_HasNoTextOperators pins PRD §11.2: the text operators are
// deliberately absent from Opaque[T] and its Nullable twin. String is the
// control, so the check is shown to look at the right names.
func TestOpaque_HasNoTextOperators(t *testing.T) {
	text := []string{"Contains", "StartsWith", "EndsWith", "Like", "NLike"}

	for _, name := range text {
		if _, ok := reflect.TypeFor[comparator.String]().FieldByName(name); !ok {
			t.Fatalf("control: comparator.String has no %s field, so this check names the wrong operators", name)
		}
	}
	for _, typ := range []reflect.Type{
		reflect.TypeFor[comparator.Opaque[[]byte]](),
		reflect.TypeFor[comparator.Opaque[net.IP]](),
		reflect.TypeFor[comparator.NullableOpaque[net.HardwareAddr]](),
	} {
		for _, name := range text {
			if _, ok := typ.FieldByName(name); ok {
				t.Errorf("%s has a %s operator; the Opaque family carries no text operators", typ, name)
			}
		}
	}
}

// TestEveryComparatorImplementsParse pins the PRD §11.3 interface: every
// comparator, Nullable twins included, has
// Parse(columnName string, dialect sql.Dialect) []sql.Condition. The list is
// checked by the compiler; a family whose method drifts fails to build.
func TestEveryComparatorImplementsParse(t *testing.T) {
	type parser interface {
		Parse(columnName string, dialect sql.Dialect) []sql.Condition
	}
	all := []parser{
		comparator.ID{},
		comparator.NullableID{},
		comparator.String{},
		comparator.NullableString{},
		comparator.Number[int64]{},
		comparator.NullableNumber[float64]{},
		comparator.Bool{},
		comparator.NullableBool{},
		comparator.Time{},
		comparator.NullableTime{},
		comparator.Enum[orderStatus]{},
		comparator.NullableEnum[orderStatus]{},
		comparator.Opaque[[]byte]{},
		comparator.NullableOpaque[net.IP]{},
		comparator.JSON{},
		comparator.NullableJSON{},
		comparator.JSONB{},
		comparator.NullableJSONB{},
		comparator.Slice[string]{},
		comparator.NullableSlice[int32]{},
	}
	if len(all) != 20 {
		t.Errorf("checked %d comparators, want the 20 §11.2 lists", len(all))
	}
}
