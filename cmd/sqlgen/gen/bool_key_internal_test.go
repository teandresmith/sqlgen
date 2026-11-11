package gen

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// A boolean key selects comparator.Bool on every path that names one, so the
// value reaches the driver as a bool. The filter field and the expressions the
// client assigns into it have to agree or the package does not compile.
func TestBoolKeyComparatorSelection(t *testing.T) {
	fk := &FKReferenceContext{}
	tests := []struct {
		name string
		col  ColumnContext
		want string
	}{
		{"bool PK", ColumnContext{GoType: "bool", PrimaryKey: true, FKConvert: gotype.FKStringSprint}, "*comparator.Bool"},
		{"nullable bool FK", ColumnContext{GoType: "*bool", Nullable: true, FKReference: fk, FKConvert: gotype.FKStringSprint}, "*comparator.NullableBool"},
		{"sql.NullBool FK", ColumnContext{GoType: "sql.NullBool", Nullable: true, FKReference: fk, FKConvert: gotype.FKStringSprint}, "*comparator.NullableBool"},
		{"bool column", ColumnContext{GoType: "bool", FKConvert: gotype.FKStringSprint}, "*comparator.Bool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, d := range []config.Dialect{config.DialectPostgres, config.DialectMySQL, config.DialectSQLite} {
				if got := resolveComparatorType(tt.col, d); got != tt.want {
					t.Errorf("resolveComparatorType(%s) = %q, want %q", d, got, tt.want)
				}
			}
		})
	}
}

func TestBoolKeyFilterExprs(t *testing.T) {
	col := ColumnContext{GoType: "bool", FKConvert: gotype.FKStringSprint}
	if got, want := funcPKFilterExpr(col, "id"), "&comparator.Bool{Eq: new(id)}"; got != want {
		t.Errorf("funcPKFilterExpr() = %q, want %q", got, want)
	}
	if got, want := funcPKFilterInExpr(col, "ids", false), "&comparator.Bool{In: ids}"; got != want {
		t.Errorf("funcPKFilterInExpr(non-nullable) = %q, want %q", got, want)
	}
	if got, want := funcPKFilterInExpr(col, "ids", true), "&comparator.NullableBool{Bool: comparator.Bool{In: ids}}"; got != want {
		t.Errorf("funcPKFilterInExpr(nullable) = %q, want %q", got, want)
	}
	if funcPKIsStringType(col) {
		t.Error("funcPKIsStringType() = true: the key would be collected as text")
	}
	got := buildAPIRowKeyMembers(TableContext{PKColumns: []ColumnContext{{FieldName: "K", GoType: "bool"}}}, "models")
	if got.ElemType != "bool" || got.FromRow != "row.K" || got.FilterMember != "K: &comparator.Bool{In: keys}" {
		t.Errorf("buildAPIRowKeyMembers() = %+v, want bool keys filtered through comparator.Bool", got)
	}
}
