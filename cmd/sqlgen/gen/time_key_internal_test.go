package gen

import (
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// A time key selects comparator.Time on every path that names one, so the value
// reaches the SQL builder as a time. The filter field and the expressions the
// client assigns into it have to agree or the package does not compile.
func TestTimeKeyComparatorSelection(t *testing.T) {
	fk := &FKReferenceContext{}
	tests := []struct {
		name string
		col  ColumnContext
		want string
	}{
		{"time.Time PK", ColumnContext{GoType: "time.Time", PrimaryKey: true, FKConvert: gotype.FKStringStringer}, "*comparator.Time"},
		{"types.DateTime PK", ColumnContext{GoType: "types.DateTime", PrimaryKey: true, FKConvert: gotype.FKStringStringer}, "*comparator.Time"},
		{"nullable time.Time FK", ColumnContext{GoType: "*time.Time", Nullable: true, FKReference: fk, FKConvert: gotype.FKStringStringer}, "*comparator.NullableTime"},
		{"sql.NullTime FK", ColumnContext{GoType: "sql.NullTime", Nullable: true, FKReference: fk, FKConvert: gotype.FKStringSprint}, "*comparator.NullableTime"},
		{"types.NullDateTime FK", ColumnContext{GoType: "types.NullDateTime", Nullable: true, FKReference: fk, FKConvert: gotype.FKStringSprint}, "*comparator.NullableTime"},
		{"uuid PK stays ID", ColumnContext{GoType: "uuid.UUID", PrimaryKey: true, FKConvert: gotype.FKStringStringer}, "*comparator.ID"},
		{"string PK stays ID", ColumnContext{GoType: "string", PrimaryKey: true}, "*comparator.ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveComparatorType(tt.col, config.DialectPostgres); got != tt.want {
				t.Errorf("resolveComparatorType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTimeKeyFilterExprs(t *testing.T) {
	tests := []struct {
		name         string
		goType       string
		wantEq       string
		wantIn       string
		wantNullable string
	}{
		{
			name:         "time.Time",
			goType:       "time.Time",
			wantEq:       "&comparator.Time{Eq: new(id)}",
			wantIn:       "&comparator.Time{In: ids}",
			wantNullable: "&comparator.NullableTime{Time: comparator.Time{In: ids}}",
		},
		{
			name:         "types.DateTime unwraps to the time it carries",
			goType:       "types.DateTime",
			wantEq:       "&comparator.Time{Eq: new(id.Time)}",
			wantIn:       "&comparator.Time{In: dateTimeKeys(ids)}",
			wantNullable: "&comparator.NullableTime{Time: comparator.Time{In: dateTimeKeys(ids)}}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := ColumnContext{GoType: tt.goType, FKConvert: gotype.FKStringStringer}
			if got := funcPKFilterExpr(col, "id"); got != tt.wantEq {
				t.Errorf("funcPKFilterExpr() = %q, want %q", got, tt.wantEq)
			}
			if got := funcPKFilterInExpr(col, "ids", false); got != tt.wantIn {
				t.Errorf("funcPKFilterInExpr(non-nullable) = %q, want %q", got, tt.wantIn)
			}
			if got := funcPKFilterInExpr(col, "ids", true); got != tt.wantNullable {
				t.Errorf("funcPKFilterInExpr(nullable) = %q, want %q", got, tt.wantNullable)
			}
			if funcPKIsStringType(col) {
				t.Error("funcPKIsStringType() = true: the key would be collected as text")
			}
		})
	}
}

// The GraphQL layer lives in another package, so it cannot call the shared
// key-list helper: a types.DateTime key has to arrive there as a time.Time.
func TestBuildAPIRowKeys_TimeKeys(t *testing.T) {
	tests := []struct {
		name string
		col  ColumnContext
		want APIRowKeys
	}{
		{
			name: "time.Time",
			col:  ColumnContext{FieldName: "At", GoType: "time.Time", Import: "time"},
			want: APIRowKeys{ElemType: "time.Time", FromRow: "row.At", FromKeyFormat: "%s", FilterMember: "At: &comparator.Time{In: keys}"},
		},
		{
			name: "types.DateTime",
			col:  ColumnContext{FieldName: "At", GoType: "types.DateTime", Import: "github.com/teandresmith/sqlgen/types"},
			want: APIRowKeys{ElemType: "time.Time", FromRow: "row.At.Time", FromKeyFormat: "%s.Time", FilterMember: "At: &comparator.Time{In: keys}"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildAPIRowKeyMembers(TableContext{PKColumns: []ColumnContext{tt.col}}, "models")
			if got.ElemType != tt.want.ElemType || got.FromRow != tt.want.FromRow ||
				got.FromKeyFormat != tt.want.FromKeyFormat || got.FilterMember != tt.want.FilterMember {
				t.Errorf("buildAPIRowKeys() = %+v, want %+v", got, tt.want)
			}
			if !slices.Contains(got.Imports, "time") {
				t.Errorf("Imports = %v, want time among them", got.Imports)
			}
		})
	}
}
