package sql_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/sql"
)

// --- LockMode dialect emission ---

func TestDialect_LockClause(t *testing.T) {
	tests := []struct {
		name    string
		dialect sql.Dialect
		mode    sql.LockMode
		want    string
	}{
		// PostgreSQL
		{"postgres none", pg, sql.LockNone, ""},
		{"postgres for_update", pg, sql.LockForUpdate, "FOR UPDATE"},
		{"postgres for_share", pg, sql.LockForShare, "FOR SHARE"},
		{"postgres for_update_nowait", pg, sql.LockForUpdateNoWait, "FOR UPDATE NOWAIT"},
		{"postgres for_update_skip_locked", pg, sql.LockForUpdateSkipLocked, "FOR UPDATE SKIP LOCKED"},
		// PostgreSQL stdlib — same SQL surface, different driver mode
		{"postgres-stdlib for_update", pgStd, sql.LockForUpdate, "FOR UPDATE"},
		{"postgres-stdlib for_share", pgStd, sql.LockForShare, "FOR SHARE"},
		// MySQL
		{"mysql none", my, sql.LockNone, ""},
		{"mysql for_update", my, sql.LockForUpdate, "FOR UPDATE"},
		{"mysql for_share emits LOCK IN SHARE MODE", my, sql.LockForShare, "LOCK IN SHARE MODE"},
		{"mysql for_update_nowait", my, sql.LockForUpdateNoWait, "FOR UPDATE NOWAIT"},
		{"mysql for_update_skip_locked", my, sql.LockForUpdateSkipLocked, "FOR UPDATE SKIP LOCKED"},
		// SQLite — every mode returns empty; the runtime guard rejects non-None modes upstream
		{"sqlite none", lite, sql.LockNone, ""},
		{"sqlite for_update is empty", lite, sql.LockForUpdate, ""},
		{"sqlite for_share is empty", lite, sql.LockForShare, ""},
		{"sqlite for_update_nowait is empty", lite, sql.LockForUpdateNoWait, ""},
		{"sqlite for_update_skip_locked is empty", lite, sql.LockForUpdateSkipLocked, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dialect.LockClause(tt.mode)
			if got != tt.want {
				t.Errorf("LockClause(%v) = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}

// --- BuildSelect emits the lock clause across dialects ---

func TestBuildSelect_LockMode(t *testing.T) {
	tests := []struct {
		name    string
		dialect sql.Dialect
		table   sql.Table
		opts    sql.SelectOptions
		wantSQL string
	}{
		{
			name:    "postgres for_update appended after WHERE",
			dialect: pg,
			table:   pgTbl,
			opts: sql.SelectOptions{
				Conditions: []sql.Condition{{Clause: `"id" = $`, Value: 1}},
				LockMode:   sql.LockForUpdate,
			},
			wantSQL: `SELECT * FROM "public"."users" WHERE "id" = $1 FOR UPDATE`,
		},
		{
			name:    "postgres for_update_skip_locked appended after ORDER BY/LIMIT/OFFSET",
			dialect: pg,
			table:   pgTbl,
			opts: sql.SelectOptions{
				OrderBy:  []sql.Sort{{Column: "id", Direction: sql.Asc}},
				Limit:    new(10),
				Offset:   new(5),
				LockMode: sql.LockForUpdateSkipLocked,
			},
			wantSQL: `SELECT * FROM "public"."users" ORDER BY "id" ASC LIMIT 10 OFFSET 5 FOR UPDATE SKIP LOCKED`,
		},
		{
			name:    "postgres for_share with no other clauses",
			dialect: pg,
			table:   pgTbl,
			opts:    sql.SelectOptions{LockMode: sql.LockForShare},
			wantSQL: `SELECT * FROM "public"."users" FOR SHARE`,
		},
		{
			name:    "mysql for_update appended after WHERE",
			dialect: my,
			table:   myTbl,
			opts: sql.SelectOptions{
				Conditions: []sql.Condition{{Clause: "`id` = $", Value: 1}},
				LockMode:   sql.LockForUpdate,
			},
			wantSQL: "SELECT * FROM `users` WHERE `id` = ? FOR UPDATE",
		},
		{
			name:    "mysql lock_in_share_mode appended after ORDER BY/LIMIT",
			dialect: my,
			table:   myTbl,
			opts: sql.SelectOptions{
				OrderBy:  []sql.Sort{{Column: "id", Direction: sql.Asc}},
				Limit:    new(10),
				LockMode: sql.LockForShare,
			},
			wantSQL: "SELECT * FROM `users` ORDER BY `id` ASC LIMIT 10 LOCK IN SHARE MODE",
		},
		{
			name:    "mysql for_update_nowait",
			dialect: my,
			table:   myTbl,
			opts:    sql.SelectOptions{LockMode: sql.LockForUpdateNoWait},
			wantSQL: "SELECT * FROM `users` FOR UPDATE NOWAIT",
		},
		{
			name:    "sqlite none emits no clause",
			dialect: lite,
			table:   ltTbl,
			opts: sql.SelectOptions{
				Conditions: []sql.Condition{{Clause: `"id" = $`, Value: 1}},
				LockMode:   sql.LockNone,
			},
			wantSQL: `SELECT * FROM "users" WHERE "id" = ?`,
		},
		{
			// Any non-LockNone mode on SQLite must NOT emit a clause — the
			// runtime guard prevents non-LockNone from ever reaching the SQL
			// builder, but if it did the builder must not produce invalid SQL.
			name:    "sqlite for_update emits no clause (rejected upstream by runtime guard)",
			dialect: lite,
			table:   ltTbl,
			opts: sql.SelectOptions{
				Conditions: []sql.Condition{{Clause: `"id" = $`, Value: 1}},
				LockMode:   sql.LockForUpdate,
			},
			wantSQL: `SELECT * FROM "users" WHERE "id" = ?`,
		},
		{
			name:    "lock mode is omitted when LockNone (default zero value)",
			dialect: pg,
			table:   pgTbl,
			opts: sql.SelectOptions{
				Conditions: []sql.Condition{{Clause: `"id" = $`, Value: 1}},
			},
			wantSQL: `SELECT * FROM "public"."users" WHERE "id" = $1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, _ := sql.BuildSelect(tt.dialect, tt.table, tt.opts)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelect() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
		})
	}
}

// TestBuildSelect_LockClauseOrdering pins the lock clause to the trailing
// position. Per PRD §9.6a / SQL grammar, the lock clause must appear *after*
// ORDER BY / LIMIT / OFFSET. A regression that placed it before any of those
// would generate invalid SQL across both PostgreSQL and MySQL.
func TestBuildSelect_LockClauseOrdering(t *testing.T) {
	cases := []struct {
		name    string
		dialect sql.Dialect
		table   sql.Table
	}{
		{"postgres", pg, pgTbl},
		{"mysql", my, myTbl},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotSQL, _ := sql.BuildSelect(tc.dialect, tc.table, sql.SelectOptions{
				Conditions: []sql.Condition{{Clause: tc.dialect.QuoteIdentifier("status") + " = $", Value: "active"}},
				OrderBy:    []sql.Sort{{Column: "id", Direction: sql.Asc}},
				Limit:      new(10),
				Offset:     new(20),
				LockMode:   sql.LockForUpdate,
			})

			lockIdx := strings.Index(gotSQL, "FOR UPDATE")
			if lockIdx < 0 {
				t.Fatalf("FOR UPDATE missing from %q", gotSQL)
			}
			for _, earlier := range []string{"WHERE", "ORDER BY", "LIMIT", "OFFSET"} {
				idx := strings.Index(gotSQL, earlier)
				if idx < 0 {
					t.Fatalf("%s missing from %q", earlier, gotSQL)
				}
				if idx >= lockIdx {
					t.Errorf("%s appears at %d but FOR UPDATE at %d (lock clause must come last)\nsql=%q", earlier, idx, lockIdx, gotSQL)
				}
			}
		})
	}
}

// TestBuildSelectJoin_LockMode confirms BuildSelectJoin also threads the lock
// clause — relationship-loading SELECTs that use FieldOptions and lock the
// parent rows must surface the FOR UPDATE clause at the end of the JOIN query.
func TestBuildSelectJoin_LockMode(t *testing.T) {
	gotSQL, gotArgs := sql.BuildSelectJoin(
		pg, pgTbl,
		[]sql.JoinClause{{
			Table:   sql.Table{Schema: "public", Name: "companies"},
			Alias:   "c",
			On:      `c."id" = p."company_id"`,
			Columns: []string{"id"},
		}},
		sql.SelectOptions{
			Alias:    "p",
			Columns:  []string{"id"},
			LockMode: sql.LockForUpdate,
		},
	)

	want := `SELECT p."id" AS "p.id", c."id" AS "c.id" FROM "public"."users" p LEFT JOIN "public"."companies" c ON c."id" = p."company_id" FOR UPDATE`
	if gotSQL != want {
		t.Errorf("BuildSelectJoin() sql =\n  %q\nwant\n  %q", gotSQL, want)
	}
	if diff := cmp.Diff([]any(nil), gotArgs); diff != "" {
		t.Errorf("BuildSelectJoin() args mismatch (-want +got):\n%s", diff)
	}
}

// --- LockMode.String ---

func TestLockMode_String(t *testing.T) {
	tests := []struct {
		mode sql.LockMode
		want string
	}{
		{sql.LockNone, "none"},
		{sql.LockForUpdate, "for_update"},
		{sql.LockForShare, "for_share"},
		{sql.LockForUpdateNoWait, "for_update_nowait"},
		{sql.LockForUpdateSkipLocked, "for_update_skip_locked"},
		{sql.LockMode(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("LockMode(%d).String() = %q, want %q", int(tt.mode), got, tt.want)
		}
	}
}
