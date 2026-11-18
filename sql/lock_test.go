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
		of      string
		want    string
	}{
		// PostgreSQL
		{"postgres none", pg, sql.LockNone, "", ""},
		{"postgres for_update", pg, sql.LockForUpdate, "", "FOR UPDATE"},
		{"postgres for_share", pg, sql.LockForShare, "", "FOR SHARE"},
		{"postgres for_update_nowait", pg, sql.LockForUpdateNoWait, "", "FOR UPDATE NOWAIT"},
		{"postgres for_update_skip_locked", pg, sql.LockForUpdateSkipLocked, "", "FOR UPDATE SKIP LOCKED"},
		{"postgres none with an alias stays empty", pg, sql.LockNone, "p", ""},
		{"postgres for_update of alias", pg, sql.LockForUpdate, "p", "FOR UPDATE OF p"},
		{"postgres for_share of alias", pg, sql.LockForShare, "p", "FOR SHARE OF p"},
		{"postgres for_update_nowait puts OF before NOWAIT", pg, sql.LockForUpdateNoWait, "p", "FOR UPDATE OF p NOWAIT"},
		{"postgres for_update_skip_locked puts OF before SKIP LOCKED", pg, sql.LockForUpdateSkipLocked, "p", "FOR UPDATE OF p SKIP LOCKED"},
		{"postgres unrecognised mode is empty", pg, sql.LockMode(99), "p", ""},
		// PostgreSQL stdlib — same SQL surface, different driver mode
		{"postgres-stdlib for_update", pgStd, sql.LockForUpdate, "", "FOR UPDATE"},
		{"postgres-stdlib for_share", pgStd, sql.LockForShare, "", "FOR SHARE"},
		{"postgres-stdlib for_update of alias", pgStd, sql.LockForUpdate, "p", "FOR UPDATE OF p"},
		// MySQL
		{"mysql none", my, sql.LockNone, "", ""},
		{"mysql for_update", my, sql.LockForUpdate, "", "FOR UPDATE"},
		{"mysql for_share emits LOCK IN SHARE MODE", my, sql.LockForShare, "", "LOCK IN SHARE MODE"},
		{"mysql for_update_nowait", my, sql.LockForUpdateNoWait, "", "FOR UPDATE NOWAIT"},
		{"mysql for_update_skip_locked", my, sql.LockForUpdateSkipLocked, "", "FOR UPDATE SKIP LOCKED"},
		{"mysql none with an alias stays empty", my, sql.LockNone, "p", ""},
		{"mysql for_update of alias", my, sql.LockForUpdate, "p", "FOR UPDATE OF p"},
		{"mysql for_share of alias emits FOR SHARE, which has an OF form", my, sql.LockForShare, "p", "FOR SHARE OF p"},
		{"mysql for_update_nowait puts OF before NOWAIT", my, sql.LockForUpdateNoWait, "p", "FOR UPDATE OF p NOWAIT"},
		{"mysql for_update_skip_locked puts OF before SKIP LOCKED", my, sql.LockForUpdateSkipLocked, "p", "FOR UPDATE OF p SKIP LOCKED"},
		{"mysql unrecognised mode is empty", my, sql.LockMode(99), "p", ""},
		// SQLite — every mode returns empty; the runtime guard rejects non-None modes upstream
		{"sqlite none", lite, sql.LockNone, "", ""},
		{"sqlite for_update is empty", lite, sql.LockForUpdate, "", ""},
		{"sqlite for_share is empty", lite, sql.LockForShare, "", ""},
		{"sqlite for_update_nowait is empty", lite, sql.LockForUpdateNoWait, "", ""},
		{"sqlite for_update_skip_locked is empty", lite, sql.LockForUpdateSkipLocked, "", ""},
		{"sqlite for_update of alias is empty", lite, sql.LockForUpdate, "p", ""},
		{"sqlite for_share of alias is empty", lite, sql.LockForShare, "p", ""},
		{"sqlite for_update_nowait of alias is empty", lite, sql.LockForUpdateNoWait, "p", ""},
		{"sqlite for_update_skip_locked of alias is empty", lite, sql.LockForUpdateSkipLocked, "p", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dialect.LockClause(tt.mode, tt.of)
			if got != tt.want {
				t.Errorf("LockClause(%v, %q) = %q, want %q", tt.mode, tt.of, got, tt.want)
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

// TestBuildSelect_LockModeUnqualified pins every mode on every dialect for the
// single-table builder: its statement reads one table, so the lock carries no
// OF list.
func TestBuildSelect_LockModeUnqualified(t *testing.T) {
	const pgBase = `SELECT "id" FROM "public"."users" WHERE "id" = $1`
	const myBase = "SELECT `id` FROM `users` WHERE `id` = ?"
	const ltBase = `SELECT "id" FROM "users" WHERE "id" = ?`
	tests := []struct {
		name    string
		dialect sql.Dialect
		table   sql.Table
		mode    sql.LockMode
		wantSQL string
	}{
		{"postgres none", pg, pgTbl, sql.LockNone, pgBase},
		{"postgres for_update", pg, pgTbl, sql.LockForUpdate, pgBase + " FOR UPDATE"},
		{"postgres for_share", pg, pgTbl, sql.LockForShare, pgBase + " FOR SHARE"},
		{"postgres for_update_nowait", pg, pgTbl, sql.LockForUpdateNoWait, pgBase + " FOR UPDATE NOWAIT"},
		{"postgres for_update_skip_locked", pg, pgTbl, sql.LockForUpdateSkipLocked, pgBase + " FOR UPDATE SKIP LOCKED"},
		{"mysql none", my, myTbl, sql.LockNone, myBase},
		{"mysql for_update", my, myTbl, sql.LockForUpdate, myBase + " FOR UPDATE"},
		{"mysql for_share keeps LOCK IN SHARE MODE", my, myTbl, sql.LockForShare, myBase + " LOCK IN SHARE MODE"},
		{"mysql for_update_nowait", my, myTbl, sql.LockForUpdateNoWait, myBase + " FOR UPDATE NOWAIT"},
		{"mysql for_update_skip_locked", my, myTbl, sql.LockForUpdateSkipLocked, myBase + " FOR UPDATE SKIP LOCKED"},
		{"sqlite none", lite, ltTbl, sql.LockNone, ltBase},
		{"sqlite for_update emits nothing", lite, ltTbl, sql.LockForUpdate, ltBase},
		{"sqlite for_share emits nothing", lite, ltTbl, sql.LockForShare, ltBase},
		{"sqlite for_update_nowait emits nothing", lite, ltTbl, sql.LockForUpdateNoWait, ltBase},
		{"sqlite for_update_skip_locked emits nothing", lite, ltTbl, sql.LockForUpdateSkipLocked, ltBase},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, _ := sql.BuildSelect(tt.dialect, tt.table, sql.SelectOptions{
				Columns:    []string{"id"},
				Conditions: []sql.Condition{{Clause: tt.dialect.QuoteIdentifier("id") + " = $", Value: 1}},
				LockMode:   tt.mode,
			})
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelect() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
		})
	}
}

// TestBuildSelectJoin_LockMode pins the lock clause a LEFT JOIN select ends
// with: it names the primary alias, so the parent rows are locked and the
// joined rows are not. PostgreSQL rejects an unqualified lock on the nullable
// side of an outer join; MySQL would lock the joined rows too.
func TestBuildSelectJoin_LockMode(t *testing.T) {
	join := func(d sql.Dialect, schema string) []sql.JoinClause {
		return []sql.JoinClause{{
			Table:   sql.Table{Schema: schema, Name: "companies"},
			Alias:   "c",
			On:      "c." + d.QuoteIdentifier("id") + " = p." + d.QuoteIdentifier("company_id"),
			Columns: []string{"id"},
		}}
	}
	const pgBase = `SELECT p."id" AS "p.id", c."id" AS "c.id" FROM "public"."users" p LEFT JOIN "public"."companies" c ON c."id" = p."company_id" ORDER BY p."id" ASC LIMIT 10`
	const myBase = "SELECT p.`id` AS `p.id`, c.`id` AS `c.id` FROM `users` p LEFT JOIN `companies` c ON c.`id` = p.`company_id` ORDER BY p.`id` ASC LIMIT 10"
	const ltBase = `SELECT p."id" AS "p.id", c."id" AS "c.id" FROM "users" p LEFT JOIN "companies" c ON c."id" = p."company_id" ORDER BY p."id" ASC LIMIT 10`
	tests := []struct {
		name    string
		dialect sql.Dialect
		table   sql.Table
		schema  string
		mode    sql.LockMode
		wantSQL string
	}{
		{"postgres none", pg, pgTbl, "public", sql.LockNone, pgBase},
		{"postgres for_update of the parent alias", pg, pgTbl, "public", sql.LockForUpdate, pgBase + " FOR UPDATE OF p"},
		{"postgres for_share of the parent alias", pg, pgTbl, "public", sql.LockForShare, pgBase + " FOR SHARE OF p"},
		{"postgres for_update_nowait of the parent alias", pg, pgTbl, "public", sql.LockForUpdateNoWait, pgBase + " FOR UPDATE OF p NOWAIT"},
		{"postgres for_update_skip_locked of the parent alias", pg, pgTbl, "public", sql.LockForUpdateSkipLocked, pgBase + " FOR UPDATE OF p SKIP LOCKED"},
		{"postgres-stdlib for_update of the parent alias", pgStd, pgTbl, "public", sql.LockForUpdate, pgBase + " FOR UPDATE OF p"},
		{"mysql none", my, myTbl, "", sql.LockNone, myBase},
		{"mysql for_update of the parent alias", my, myTbl, "", sql.LockForUpdate, myBase + " FOR UPDATE OF p"},
		{"mysql for_share of the parent alias uses FOR SHARE", my, myTbl, "", sql.LockForShare, myBase + " FOR SHARE OF p"},
		{"mysql for_update_nowait of the parent alias", my, myTbl, "", sql.LockForUpdateNoWait, myBase + " FOR UPDATE OF p NOWAIT"},
		{"mysql for_update_skip_locked of the parent alias", my, myTbl, "", sql.LockForUpdateSkipLocked, myBase + " FOR UPDATE OF p SKIP LOCKED"},
		{"sqlite none", lite, ltTbl, "", sql.LockNone, ltBase},
		{"sqlite for_update emits nothing", lite, ltTbl, "", sql.LockForUpdate, ltBase},
		{"sqlite for_share emits nothing", lite, ltTbl, "", sql.LockForShare, ltBase},
		{"sqlite for_update_nowait emits nothing", lite, ltTbl, "", sql.LockForUpdateNoWait, ltBase},
		{"sqlite for_update_skip_locked emits nothing", lite, ltTbl, "", sql.LockForUpdateSkipLocked, ltBase},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelectJoin(tt.dialect, tt.table, join(tt.dialect, tt.schema), sql.SelectOptions{
				Alias:    "p",
				Columns:  []string{"id"},
				OrderBy:  []sql.Sort{{Column: "id", Direction: sql.Asc}},
				Limit:    new(10),
				LockMode: tt.mode,
			})
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelectJoin() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff([]any(nil), gotArgs); diff != "" {
				t.Errorf("BuildSelectJoin() args mismatch (-want +got):\n%s", diff)
			}
		})
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
