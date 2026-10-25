package sql_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/sql"
	"github.com/teandresmith/sqlgen/types"
)

// TestBuild_MySQLNilDateTimePointer pins that on MySQL a nil *types.DateTime
// binds as SQL NULL, as a written value and as a filter operand.
func TestBuild_MySQLNilDateTimePointer(t *testing.T) {
	var nilDT *types.DateTime
	tests := []struct {
		name  string
		build func() []any
	}{
		{name: "select filter operand", build: func() []any {
			_, a := sql.BuildSelect(my, ltTbl, sql.SelectOptions{Conditions: []sql.Condition{sql.Where("ts").Eq(nilDT)}})
			return a
		}},
		{name: "insert value", build: func() []any {
			_, a := sql.BuildInsert(my, ltTbl, sql.InsertOptions{Columns: []string{"ts"}, Values: []any{nilDT}})
			return a
		}},
		{name: "update value", build: func() []any {
			_, a := sql.BuildUpdate(my, ltTbl, sql.UpdateOptions{SetClauses: map[string]any{"ts": nilDT}})
			return a
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff([]any{nil}, tt.build()); diff != "" {
				t.Errorf("args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestBuild_TimeArgs_IgnoreOutputLayout pins that types.SetOutputLayout does
// not change what a SQLite or MySQL statement binds for a time argument.
func TestBuild_TimeArgs_IgnoreOutputLayout(t *testing.T) {
	const layout = "02/01/2006 15h04"
	types.SetOutputLayout(layout)
	t.Cleanup(func() { types.SetOutputLayout(time.RFC3339Nano) })

	inst := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)
	dt := types.DateTime{Time: inst}
	if v, err := dt.Value(); err != nil || v != inst.Format(layout) {
		t.Fatalf("DateTime.Value() = %v, %v; want %q (the layout did not take effect)", v, err, inst.Format(layout))
	}

	vals := []any{dt, &dt, types.NullDateTime{Time: inst, Valid: true}}
	text := "2026-01-01 12:30:00"
	conds := []sql.Condition{sql.Where("a").Eq(vals[0]), sql.Where("b").Eq(vals[1]), sql.Where("c").Eq(vals[2])}
	selectArgs := func(d sql.Dialect) []any {
		_, a := sql.BuildSelect(d, ltTbl, sql.SelectOptions{Conditions: conds})
		return a
	}
	insertArgs := func(d sql.Dialect) []any {
		_, a := sql.BuildInsert(d, ltTbl, sql.InsertOptions{Columns: []string{"a", "b", "c"}, Values: vals})
		return a
	}
	tests := []struct {
		name    string
		dialect sql.Dialect
		build   func(sql.Dialect) []any
		want    []any
	}{
		{name: "sqlite filter operands", dialect: lite, build: selectArgs, want: []any{text, text, text}},
		{name: "sqlite written values", dialect: lite, build: insertArgs, want: []any{text, text, text}},
		{name: "mysql filter operands", dialect: my, build: selectArgs, want: []any{inst, inst, inst}},
		{name: "mysql written values", dialect: my, build: insertArgs, want: []any{inst, inst, inst}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, tt.build(tt.dialect)); diff != "" {
				t.Errorf("args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
