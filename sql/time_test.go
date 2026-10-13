package sql_test

import (
	stdsql "database/sql"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/sql"
	"github.com/teandresmith/sqlgen/types"
)

// TestBuild_SQLiteTimeArgs pins that on SQLite every time value a builder
// binds becomes the text SQLite's datetime() writes, in UTC, whether it is a
// write value, a filter operand or a cursor keyset value, and that the other
// dialects bind the value unchanged.
func TestBuild_SQLiteTimeArgs(t *testing.T) {
	inst := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)
	est := inst.In(time.FixedZone("EST", -5*3600))
	frac := time.Date(2026, 1, 1, 12, 30, 0, 500000000, time.UTC)

	vals := []any{
		inst,
		est,
		frac,
		&inst,
		(*time.Time)(nil),
		stdsql.NullTime{Time: inst, Valid: true},
		stdsql.NullTime{},
		types.DateTime{Time: est},
		&types.DateTime{Time: inst},
		types.NullDateTime{Time: inst, Valid: true},
		types.NullDateTime{},
		"2026-01-01T12:30:00Z",
		int64(7),
	}
	sqliteWant := []any{
		"2026-01-01 12:30:00",
		"2026-01-01 12:30:00",
		"2026-01-01 12:30:00.5",
		"2026-01-01 12:30:00",
		nil,
		"2026-01-01 12:30:00",
		nil,
		"2026-01-01 12:30:00",
		"2026-01-01 12:30:00",
		"2026-01-01 12:30:00",
		nil,
		"2026-01-01T12:30:00Z",
		int64(7),
	}
	conds := make([]sql.Condition, len(vals))
	cols := make([]string, len(vals))
	for i, v := range vals {
		cols[i] = string(rune('a' + i))
		conds[i] = sql.Where(cols[i]).Eq(v)
	}

	tests := []struct {
		name    string
		dialect sql.Dialect
		want    []any
	}{
		{name: "sqlite binds datetime() text", dialect: lite, want: sqliteWant},
		{name: "postgres binds unchanged", dialect: pg, want: vals},
		{name: "mysql binds unchanged", dialect: my, want: vals},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/select", func(t *testing.T) {
			_, got := sql.BuildSelect(tt.dialect, ltTbl, sql.SelectOptions{Conditions: conds})
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("BuildSelect args mismatch (-want +got):\n%s", diff)
			}
		})
		t.Run(tt.name+"/insert", func(t *testing.T) {
			in := append([]any(nil), vals...)
			_, got := sql.BuildInsert(tt.dialect, ltTbl, sql.InsertOptions{Columns: cols, Values: in})
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("BuildInsert args mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(vals, in); diff != "" {
				t.Errorf("BuildInsert modified the caller's values (-want +got):\n%s", diff)
			}
		})
	}
}

// TestBuild_SQLiteTimeArgs_EveryBuilder checks each builder that returns
// args converts a time on SQLite.
func TestBuild_SQLiteTimeArgs_EveryBuilder(t *testing.T) {
	inst := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)
	want := "2026-01-01 12:30:00"
	where := []sql.Condition{sql.Where("ts").Eq(types.DateTime{Time: inst})}

	builders := map[string]func() []any{
		"BuildSelectJoin": func() []any {
			_, a := sql.BuildSelectJoin(lite, ltTbl, nil, sql.SelectOptions{Alias: "t", Conditions: where})
			return a
		},
		"BuildMultiInsert": func() []any {
			_, a := sql.BuildMultiInsert(lite, ltTbl, sql.MultiInsertOptions{Columns: []string{"ts"}, ValueRows: [][]any{{inst}}})
			return a
		},
		"BuildUpdate": func() []any {
			_, a := sql.BuildUpdate(lite, ltTbl, sql.UpdateOptions{SetClauses: map[string]any{"ts": inst}})
			return a
		},
		"BuildCount": func() []any {
			_, a := sql.BuildCount(lite, ltTbl, where)
			return a
		},
		"BuildExists": func() []any {
			_, a := sql.BuildExists(lite, ltTbl, where)
			return a
		},
		"BuildIncrementReturning": func() []any {
			_, a := sql.BuildIncrementReturning(lite, ltTbl, "n", 1, where, nil)
			return a[1:]
		},
		"BuildSoftDelete": func() []any {
			_, a := sql.BuildSoftDelete(lite, ltTbl, sql.SoftDeleteOptions{Column: "deleted_at", Type: sql.SoftDeleteTimestamp, Conditions: where})
			return a
		},
		"BuildHardDelete": func() []any {
			_, a := sql.BuildHardDelete(lite, ltTbl, sql.HardDeleteOptions{Conditions: where})
			return a
		},
		"SubqueryWhere": func() []any {
			_, a := sql.SubqueryWhere(lite, "s", where)
			return a
		},
	}
	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff([]any{want}, build()); diff != "" {
				t.Errorf("args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestBuild_SQLiteTimeArgs_ListAndSubquery checks an IN list, a BETWEEN range
// and a subquery's args convert a time on SQLite.
func TestBuild_SQLiteTimeArgs_ListAndSubquery(t *testing.T) {
	inst := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)
	want := "2026-01-01 12:30:00"
	tests := []struct {
		name string
		cond sql.Condition
		want []any
	}{
		{name: "in", cond: sql.Where("ts").In(inst, types.DateTime{Time: inst}), want: []any{want, want}},
		{name: "between", cond: sql.Where("ts").Between(inst, &inst), want: []any{want, want}},
		{name: "subquery", cond: sql.Exists("id", sql.Subquery{SQL: `SELECT 1 FROM "s" WHERE "s"."id" = ` + sql.CorrelationToken + ` AND "s"."ts" = $1`, Args: []any{inst}}), want: []any{want}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := sql.BuildSelect(lite, ltTbl, sql.SelectOptions{Conditions: []sql.Condition{tt.cond}})
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("BuildSelect args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
