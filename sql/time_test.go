package sql_test

import (
	stdsql "database/sql"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/sql"
	"github.com/teandresmith/sqlgen/types"
)

// TestBuild_TimeArgs pins that on SQLite every time value a builder binds
// becomes the text SQLite's datetime() writes, in UTC, whether it is a write
// value, a filter operand or a cursor keyset value; that on MySQL a
// types.DateTime binds as its time.Time; and that PostgreSQL binds the value
// unchanged.
func TestBuild_TimeArgs(t *testing.T) {
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
	// MySQL binds a types.DateTime as the time.Time it wraps, so the driver
	// writes it in its loc like a time.Time; every other value is unchanged.
	mysqlWant := []any{
		inst,
		est,
		frac,
		&inst,
		(*time.Time)(nil),
		stdsql.NullTime{Time: inst, Valid: true},
		stdsql.NullTime{},
		est,
		inst,
		inst,
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
		{name: "mysql binds types.DateTime as time.Time", dialect: my, want: mysqlWant},
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

// TestBuild_TimeArgs_EveryBuilder checks each builder that returns args
// converts a types.DateTime on SQLite and MySQL.
func TestBuild_TimeArgs_EveryBuilder(t *testing.T) {
	inst := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)
	dt := types.DateTime{Time: inst}
	where := []sql.Condition{sql.Where("ts").Eq(dt)}

	builders := map[string]func(d sql.Dialect) []any{
		"BuildSelectJoin": func(d sql.Dialect) []any {
			_, a := sql.BuildSelectJoin(d, ltTbl, nil, sql.SelectOptions{Alias: "t", Conditions: where})
			return a
		},
		"BuildMultiInsert": func(d sql.Dialect) []any {
			_, a := sql.BuildMultiInsert(d, ltTbl, sql.MultiInsertOptions{Columns: []string{"ts"}, ValueRows: [][]any{{dt}}})
			return a
		},
		"BuildUpdate": func(d sql.Dialect) []any {
			_, a := sql.BuildUpdate(d, ltTbl, sql.UpdateOptions{SetClauses: map[string]any{"ts": dt}})
			return a
		},
		"BuildCount": func(d sql.Dialect) []any {
			_, a := sql.BuildCount(d, ltTbl, where)
			return a
		},
		"BuildExists": func(d sql.Dialect) []any {
			_, a := sql.BuildExists(d, ltTbl, where)
			return a
		},
		"BuildIncrementReturning": func(d sql.Dialect) []any {
			_, a := sql.BuildIncrementReturning(d, ltTbl, "n", 1, where, nil)
			return a[1:]
		},
		"BuildSoftDelete": func(d sql.Dialect) []any {
			_, a := sql.BuildSoftDelete(d, ltTbl, sql.SoftDeleteOptions{Column: "deleted_at", Type: sql.SoftDeleteTimestamp, Conditions: where})
			return a
		},
		"BuildHardDelete": func(d sql.Dialect) []any {
			_, a := sql.BuildHardDelete(d, ltTbl, sql.HardDeleteOptions{Conditions: where})
			return a
		},
		"SubqueryWhere": func(d sql.Dialect) []any {
			_, a := sql.SubqueryWhere(d, "s", where)
			return a
		},
	}
	dialects := []struct {
		name    string
		dialect sql.Dialect
		want    any
	}{
		{name: "sqlite", dialect: lite, want: "2026-01-01 12:30:00"},
		{name: "mysql", dialect: my, want: inst},
	}
	for _, dd := range dialects {
		for name, build := range builders {
			t.Run(dd.name+"/"+name, func(t *testing.T) {
				if diff := cmp.Diff([]any{dd.want}, build(dd.dialect)); diff != "" {
					t.Errorf("args mismatch (-want +got):\n%s", diff)
				}
			})
		}
	}
}

// TestBuild_TimeArgs_ListAndSubquery checks an IN list, a BETWEEN range and a
// subquery's args convert a time on SQLite and a types.DateTime on MySQL.
func TestBuild_TimeArgs_ListAndSubquery(t *testing.T) {
	inst := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)
	dt := types.DateTime{Time: inst}
	text := "2026-01-01 12:30:00"
	tests := []struct {
		name       string
		cond       sql.Condition
		sqliteWant []any
		mysqlWant  []any
	}{
		{name: "in", cond: sql.Where("ts").In(inst, dt), sqliteWant: []any{text, text}, mysqlWant: []any{inst, inst}},
		{name: "between", cond: sql.Where("ts").Between(dt, &inst), sqliteWant: []any{text, text}, mysqlWant: []any{inst, &inst}},
		{name: "subquery", cond: sql.Exists("id", sql.Subquery{SQL: `SELECT 1 FROM "s" WHERE "s"."id" = ` + sql.CorrelationToken + ` AND "s"."ts" = $1 AND "s"."u" = $2`, Args: []any{inst, types.NullDateTime{Time: inst, Valid: true}}}), sqliteWant: []any{text, text}, mysqlWant: []any{inst, inst}},
	}
	for _, tt := range tests {
		for _, dd := range []struct {
			name    string
			dialect sql.Dialect
			want    []any
		}{
			{name: "sqlite", dialect: lite, want: tt.sqliteWant},
			{name: "mysql", dialect: my, want: tt.mysqlWant},
		} {
			t.Run(dd.name+"/"+tt.name, func(t *testing.T) {
				_, got := sql.BuildSelect(dd.dialect, ltTbl, sql.SelectOptions{Conditions: []sql.Condition{tt.cond}})
				if diff := cmp.Diff(dd.want, got); diff != "" {
					t.Errorf("BuildSelect args mismatch (-want +got):\n%s", diff)
				}
			})
		}
	}
}
