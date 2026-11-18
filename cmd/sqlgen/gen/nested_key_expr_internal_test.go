package gen

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// The `= parent` term spells the parent's column in the operand type of the
// member's filter field, which is what the generated `Eq` is assigned into.
func TestNestedParentKeyExpr(t *testing.T) {
	tests := []struct {
		name       string
		comparator string
		goType     string
		convert    gotype.FKStringMethod
		want       string
	}{
		{"enum", "*comparator.Enum[Kind]", "Kind", gotype.FKStringSprint, "parent.K"},
		{"nullable enum", "*comparator.NullableEnum[Kind]", "Kind", gotype.FKStringSprint, "parent.K"},
		{"time", "*comparator.Time", "time.Time", gotype.FKStringStringer, "parent.K"},
		{"nullable time", "*comparator.NullableTime", "time.Time", gotype.FKStringStringer, "parent.K"},
		{"sqlite datetime", "*comparator.NullableTime", "types.DateTime", gotype.FKStringStringer, "parent.K.Time"},
		{"bool", "*comparator.NullableBool", "bool", gotype.FKStringSprint, "parent.K"},
		{"number", "*comparator.NullableNumber[int32]", "int32", gotype.FKStringSprint, "parent.K"},
		{"opaque", "*comparator.NullableOpaque[[]byte]", "[]byte", gotype.FKStringSprint, "parent.K"},
		{"id from uuid", "*comparator.NullableID", "uuid.UUID", gotype.FKStringStringer, "parent.K.String()"},
		{"id from time", "*comparator.ID", "time.Time", gotype.FKStringStringer, "parent.K.String()"},
		{"string", "*comparator.NullableString", "string", gotype.FKStringNone, "parent.K"},
		{"string from a named type", "*comparator.String", "pgtype.Text", gotype.FKStringSprint, "fmt.Sprint(parent.K)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := ColumnContext{FieldName: "K", GoType: tt.goType, FKConvert: tt.convert}
			if got := nestedParentKeyExpr(tt.comparator, parent); got != tt.want {
				t.Errorf("nestedParentKeyExpr(%q, %s) = %q, want %q", tt.comparator, tt.goType, got, tt.want)
			}
		})
	}
}

// The connect split compares the unwrapped FK with the parent's column, and a
// time key compares as an instant, on the embedded time.Time of a SQLite datetime.
func TestNestedOwnerIsParentExpr(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		goType string
		want   string
	}{
		{"integer", "(*cur)", "int32", "(*cur) == parent.K"},
		{"null wrapper", "cur.UUID", "uuid.UUID", "cur.UUID == parent.K"},
		{"time", "(*cur)", "time.Time", "(*cur).Equal(parent.K)"},
		{"sqlite datetime", "cur.Time", "types.DateTime", "cur.Time.Equal(parent.K.Time)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := ColumnContext{FieldName: "K", GoType: tt.goType}
			if got := nestedOwnerIsParentExpr(tt.key, parent); got != tt.want {
				t.Errorf("nestedOwnerIsParentExpr(%q, %s) = %q, want %q", tt.key, tt.goType, got, tt.want)
			}
		})
	}
}

// A nullable FK member wraps the parent's value, which a types.DateTime has to
// give up as the time.Time that types.NullDateTime holds.
func TestNestedAssignExpr_NullWrapperValue(t *testing.T) {
	resolver := gotype.NewResolver(config.DialectSQLite, false, nil)
	tests := []struct {
		name     string
		goType   string
		pkGoType string
		want     string
	}{
		{"sqlite datetime", "omittable.Value[types.NullDateTime]", "types.DateTime", "omittable.Set(types.NullDateTime{Time: parent.K.Time, Valid: true})"},
		{"pointer", "omittable.Value[*types.DateTime]", "types.DateTime", "omittable.Set(&parent.K)"},
		{"same type", "omittable.Value[types.DateTime]", "types.DateTime", "omittable.Set(parent.K)"},
		{"sql.NullTime over a datetime", "omittable.Value[sql.NullTime]", "types.DateTime", "omittable.Set(sql.NullTime{Time: parent.K.Time, Valid: true})"},
		// A wrapper over another type than the key cannot carry it.
		{"datetime wrapper over a text key", "omittable.Value[types.NullDateTime]", "string", ""},
		{"narrower sql wrapper", "omittable.Value[sql.NullInt32]", "int64", ""},
		{"wider sql wrapper", "omittable.Value[sql.NullInt64]", "int32", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nestedAssignExpr(tt.goType, false, tt.pkGoType, "parent.K", resolver); got != tt.want {
				t.Errorf("nestedAssignExpr(%q, %q) = %q, want %q", tt.goType, tt.pkGoType, got, tt.want)
			}
		})
	}
}
