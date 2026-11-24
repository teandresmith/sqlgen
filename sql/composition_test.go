package sql_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/sql"
)

// TestComposition_NestedShapesPerDialect renders each PRD §11.5 condition
// shape nested inside And / Or, between two bound neighbours, on every dialect.
// The neighbours show the nested shape's placeholders taking their place in
// the enclosing statement's sequence in Conditions order, and the statement
// continuing after them.
func TestComposition_NestedShapesPerDialect(t *testing.T) {
	tests := []struct {
		name string
		cond sql.Condition
		// pg is the WHERE on PostgreSQL (both drivers); pgArgs and args are
		// the args on PostgreSQL and on the "?" dialects.
		pg, my, lite string
		pgArgs, args []any
	}{
		{
			// The §11.5 nesting example: an And inside an Or inside an And.
			name: "and and or nest",
			cond: sql.And(
				sql.Where("active").Eq(true),
				sql.Or(
					sql.Where("role").Eq("admin"),
					sql.And(sql.Where("role").Eq("user"), sql.Where("verified").Eq(true)),
				),
			),
			pg:     `tenant = $1 AND (active = $2 AND (role = $3 OR (role = $4 AND verified = $5))) AND age > $6`,
			my:     `tenant = ? AND (active = ? AND (role = ? OR (role = ? AND verified = ?))) AND age > ?`,
			lite:   `tenant = ? AND (active = ? AND (role = ? OR (role = ? AND verified = ?))) AND age > ?`,
			pgArgs: []any{7, true, "admin", "user", true, 3},
			args:   []any{7, true, "admin", "user", true, 3},
		},
		{
			name: "and inside and",
			cond: sql.And(
				sql.Where("active").Eq(true),
				sql.And(sql.Where("role").Eq("user"), sql.Where("verified").Eq(true)),
			),
			pg:     `tenant = $1 AND (active = $2 AND (role = $3 AND verified = $4)) AND age > $5`,
			my:     `tenant = ? AND (active = ? AND (role = ? AND verified = ?)) AND age > ?`,
			lite:   `tenant = ? AND (active = ? AND (role = ? AND verified = ?)) AND age > ?`,
			pgArgs: []any{7, true, "user", true, 3},
			args:   []any{7, true, "user", true, 3},
		},
		{
			name: "bare subquery inside or",
			cond: sql.Or(
				sql.Where("role").Eq("admin"),
				sql.Where("company_id").In(sql.Subquery{SQL: `SELECT id FROM companies WHERE country = $ AND size > $`, Args: []any{"US", 10}}),
			),
			pg:     `tenant = $1 AND (role = $2 OR company_id IN (SELECT id FROM companies WHERE country = $3 AND size > $4)) AND age > $5`,
			my:     `tenant = ? AND (role = ? OR company_id IN (SELECT id FROM companies WHERE country = ? AND size > ?)) AND age > ?`,
			lite:   `tenant = ? AND (role = ? OR company_id IN (SELECT id FROM companies WHERE country = ? AND size > ?)) AND age > ?`,
			pgArgs: []any{7, "admin", "US", 10, 3},
			args:   []any{7, "admin", "US", 10, 3},
		},
		{
			name: "numbered subquery inside and",
			cond: sql.And(
				sql.Where("role").Eq("admin"),
				sql.Where("company_id").Nin(sql.Subquery{SQL: `SELECT id FROM companies WHERE size > $2 AND (country = $1 OR region = $1)`, Args: []any{"US", 10}}),
			),
			pg:     `tenant = $1 AND (role = $2 AND company_id NOT IN (SELECT id FROM companies WHERE size > $4 AND (country = $3 OR region = $3))) AND age > $5`,
			my:     `tenant = ? AND (role = ? AND company_id NOT IN (SELECT id FROM companies WHERE size > ? AND (country = ? OR region = ?))) AND age > ?`,
			lite:   `tenant = ? AND (role = ? AND company_id NOT IN (SELECT id FROM companies WHERE size > ? AND (country = ? OR region = ?))) AND age > ?`,
			pgArgs: []any{7, "admin", "US", 10, 3},
			args:   []any{7, "admin", 10, "US", "US", 3},
		},
		{
			name: "exists one pair inside or",
			cond: sql.Or(
				sql.Where("role").Eq("admin"),
				sql.Exists("id", sql.Subquery{SQL: `SELECT 1 FROM orders o WHERE o.user_id = ` + sql.CorrelationToken + ` AND o.total > $`, Args: []any{100}}),
			),
			pg:     `tenant = $1 AND (role = $2 OR EXISTS (SELECT 1 FROM orders o WHERE o.user_id = "users"."id" AND o.total > $3)) AND age > $4`,
			my:     "tenant = ? AND (role = ? OR EXISTS (SELECT 1 FROM orders o WHERE o.user_id = `users`.`id` AND o.total > ?)) AND age > ?",
			lite:   `tenant = ? AND (role = ? OR EXISTS (SELECT 1 FROM orders o WHERE o.user_id = "users"."id" AND o.total > ?)) AND age > ?`,
			pgArgs: []any{7, "admin", 100, 3},
			args:   []any{7, "admin", 100, 3},
		},
		{
			name: "exists pairs inside and",
			cond: sql.And(
				sql.Where("role").Eq("admin"),
				sql.Exists([]string{"workspace_id", "id"}, sql.Subquery{
					SQL: `SELECT 1 FROM tasks k WHERE k.workspace_id = ` + sql.CorrelationTokenAt(0) +
						` AND k.owner_id = ` + sql.CorrelationTokenAt(1) + ` AND k.status = $`,
					Args: []any{"open"},
				}),
			),
			pg:     `tenant = $1 AND (role = $2 AND EXISTS (SELECT 1 FROM tasks k WHERE k.workspace_id = "users"."workspace_id" AND k.owner_id = "users"."id" AND k.status = $3)) AND age > $4`,
			my:     "tenant = ? AND (role = ? AND EXISTS (SELECT 1 FROM tasks k WHERE k.workspace_id = `users`.`workspace_id` AND k.owner_id = `users`.`id` AND k.status = ?)) AND age > ?",
			lite:   `tenant = ? AND (role = ? AND EXISTS (SELECT 1 FROM tasks k WHERE k.workspace_id = "users"."workspace_id" AND k.owner_id = "users"."id" AND k.status = ?)) AND age > ?`,
			pgArgs: []any{7, "admin", "open", 3},
			args:   []any{7, "admin", "open", 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialects := []struct {
				name     string
				d        sql.Dialect
				wantSQL  string
				wantArgs []any
			}{
				{"postgres", pg, `SELECT * FROM "users" WHERE ` + tt.pg, tt.pgArgs},
				{"postgres stdlib", pgStd, `SELECT * FROM "users" WHERE ` + tt.pg, tt.pgArgs},
				{"mysql", my, "SELECT * FROM `users` WHERE " + tt.my, tt.args},
				{"sqlite", lite, `SELECT * FROM "users" WHERE ` + tt.lite, tt.args},
			}
			for _, dd := range dialects {
				t.Run(dd.name, func(t *testing.T) {
					gotSQL, gotArgs := sql.BuildSelect(dd.d, sql.Table{Name: "users"}, sql.SelectOptions{
						Conditions: []sql.Condition{sql.Where("tenant").Eq(7), tt.cond, sql.Where("age").Gt(3)},
					})
					if gotSQL != dd.wantSQL {
						t.Errorf("BuildSelect(%s) on %s =\n  %s\nwant\n  %s", tt.name, dd.name, gotSQL, dd.wantSQL)
					}
					if diff := cmp.Diff(dd.wantArgs, gotArgs); diff != "" {
						t.Errorf("BuildSelect(%s) on %s args mismatch (-want +got):\n%s", tt.name, dd.name, diff)
					}
				})
			}
		})
	}
}

// TestConditionBuilder_InNinExpandOnEveryDialect pins PRD §11.5: the builder's
// In / Nin always expand to one placeholder per value, PostgreSQL+pgx
// included. The array-parameter form is the comparators' choice, never the
// builder's.
func TestConditionBuilder_InNinExpandOnEveryDialect(t *testing.T) {
	tests := []struct {
		name    string
		d       sql.Dialect
		tbl     sql.Table
		wantSQL string
	}{
		{"postgres pgx", pg, pgTbl, `SELECT * FROM "public"."users" WHERE active = $1 AND status IN ($2, $3) AND role NOT IN ($4, $5)`},
		{"postgres stdlib", pgStd, pgTbl, `SELECT * FROM "public"."users" WHERE active = $1 AND status IN ($2, $3) AND role NOT IN ($4, $5)`},
		{"mysql", my, myTbl, "SELECT * FROM `users` WHERE active = ? AND status IN (?, ?) AND role NOT IN (?, ?)"},
		{"sqlite", lite, ltTbl, `SELECT * FROM "users" WHERE active = ? AND status IN (?, ?) AND role NOT IN (?, ?)`},
	}
	want := []any{true, "open", "held", "guest", "bot"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelect(tt.d, tt.tbl, sql.SelectOptions{Conditions: []sql.Condition{
				sql.Where("active").Eq(true),
				sql.Where("status").In("open", "held"),
				sql.Where("role").Nin("guest", "bot"),
			}})
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelect(In, Nin) on %s =\n  %s\nwant\n  %s", tt.name, gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(want, gotArgs); diff != "" {
				t.Errorf("BuildSelect(In, Nin) on %s args mismatch (-want +got):\n%s", tt.name, diff)
			}
		})
	}
}

// TestExists_QualifierInEveryConditionBuilder pins PRD §11.5 Rule 1 for the
// builders other than the two reads: a statement that does not alias its table
// resolves the correlation against Dialect.FormatTable(table), so an Exists in
// a count, an existence check, an update, an increment, a soft delete or a
// hard delete correlates to the statement's own table. The table has a schema
// so that, on PostgreSQL, FormatTable ("public"."users") differs from the
// bare quoted name ("users").
func TestExists_QualifierInEveryConditionBuilder(t *testing.T) {
	exists := []sql.Condition{sql.Exists("id", sql.Subquery{
		SQL:  `SELECT 1 FROM orders o WHERE o.user_id = ` + sql.CorrelationToken + ` AND o.total > $`,
		Args: []any{100},
	})}
	tbl := sql.Table{Schema: "public", Name: "users"}

	tests := []struct {
		name         string
		build        func(sql.Dialect) (string, []any)
		pg, my, lite string
		wantArgs     []any
	}{
		{
			name:     "BuildCount",
			build:    func(d sql.Dialect) (string, []any) { return sql.BuildCount(d, tbl, exists) },
			pg:       `SELECT COUNT(*) FROM "public"."users" WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = "public"."users"."id" AND o.total > $1)`,
			my:       "SELECT COUNT(*) FROM `users` WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = `users`.`id` AND o.total > ?)",
			lite:     `SELECT COUNT(*) FROM "users" WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = "users"."id" AND o.total > ?)`,
			wantArgs: []any{100},
		},
		{
			name:     "BuildExists",
			build:    func(d sql.Dialect) (string, []any) { return sql.BuildExists(d, tbl, exists) },
			pg:       `SELECT EXISTS(SELECT 1 FROM "public"."users" WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = "public"."users"."id" AND o.total > $1))`,
			my:       "SELECT EXISTS(SELECT 1 FROM `users` WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = `users`.`id` AND o.total > ?))",
			lite:     `SELECT EXISTS(SELECT 1 FROM "users" WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = "users"."id" AND o.total > ?))`,
			wantArgs: []any{100},
		},
		{
			name: "BuildUpdate",
			build: func(d sql.Dialect) (string, []any) {
				return sql.BuildUpdate(d, tbl, sql.UpdateOptions{SetClauses: map[string]any{"name": "x"}, Conditions: exists})
			},
			pg:       `UPDATE "public"."users" SET "name" = $1 WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = "public"."users"."id" AND o.total > $2)`,
			my:       "UPDATE `users` SET `name` = ? WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = `users`.`id` AND o.total > ?)",
			lite:     `UPDATE "users" SET "name" = ? WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = "users"."id" AND o.total > ?)`,
			wantArgs: []any{"x", 100},
		},
		{
			name:     "BuildIncrement",
			build:    func(d sql.Dialect) (string, []any) { return sql.BuildIncrement(d, tbl, "visits", 1, exists) },
			pg:       `UPDATE "public"."users" SET "visits" = "visits" + $1 WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = "public"."users"."id" AND o.total > $2)`,
			my:       "UPDATE `users` SET `visits` = `visits` + ? WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = `users`.`id` AND o.total > ?)",
			lite:     `UPDATE "users" SET "visits" = "visits" + ? WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = "users"."id" AND o.total > ?)`,
			wantArgs: []any{1, 100},
		},
		{
			name: "BuildSoftDelete",
			build: func(d sql.Dialect) (string, []any) {
				return sql.BuildSoftDelete(d, tbl, sql.SoftDeleteOptions{Column: "deleted_at", Type: sql.SoftDeleteTimestamp, Conditions: exists})
			},
			pg:       `UPDATE "public"."users" SET "deleted_at" = CURRENT_TIMESTAMP WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = "public"."users"."id" AND o.total > $1)`,
			my:       "UPDATE `users` SET `deleted_at` = CURRENT_TIMESTAMP WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = `users`.`id` AND o.total > ?)",
			lite:     `UPDATE "users" SET "deleted_at" = CURRENT_TIMESTAMP WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = "users"."id" AND o.total > ?)`,
			wantArgs: []any{100},
		},
		{
			name: "BuildHardDelete",
			build: func(d sql.Dialect) (string, []any) {
				return sql.BuildHardDelete(d, tbl, sql.HardDeleteOptions{Conditions: exists})
			},
			pg:       `DELETE FROM "public"."users" WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = "public"."users"."id" AND o.total > $1)`,
			my:       "DELETE FROM `users` WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = `users`.`id` AND o.total > ?)",
			lite:     `DELETE FROM "users" WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = "users"."id" AND o.total > ?)`,
			wantArgs: []any{100},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialects := []struct {
				name    string
				d       sql.Dialect
				wantSQL string
			}{
				{"postgres", pg, tt.pg},
				{"mysql", my, tt.my},
				{"sqlite", lite, tt.lite},
			}
			for _, dd := range dialects {
				t.Run(dd.name, func(t *testing.T) {
					gotSQL, gotArgs := tt.build(dd.d)
					if gotSQL != dd.wantSQL {
						t.Errorf("%s(Exists) on %s =\n  %s\nwant\n  %s", tt.name, dd.name, gotSQL, dd.wantSQL)
					}
					if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
						t.Errorf("%s(Exists) on %s args mismatch (-want +got):\n%s", tt.name, dd.name, diff)
					}
				})
			}
		})
	}
}

// TestCorrelationTokenAt_ZeroIsCorrelationToken pins the PRD §11.5 identity
// sql.CorrelationTokenAt(0) == sql.CorrelationToken, which is what lets a
// one-pair subquery and the first pair of a composite one share a token.
func TestCorrelationTokenAt_ZeroIsCorrelationToken(t *testing.T) {
	if got := sql.CorrelationTokenAt(0); got != sql.CorrelationToken {
		t.Errorf("CorrelationTokenAt(0) = %q, want CorrelationToken %q", got, sql.CorrelationToken)
	}
	if sql.CorrelationTokenAt(1) == sql.CorrelationToken {
		t.Errorf("CorrelationTokenAt(1) = CorrelationToken %q, want a token of its own", sql.CorrelationToken)
	}
}

// TestSubquery_QuotesInsideLiterals pins two PRD §11.5 rules of the token scan.
// A doubled quote escapes itself on every dialect, so a "$" after one is still
// inside the literal. And PostgreSQL does not recognise its E'…' escape form:
// a backslash is literal there, so E'it\'s' closes after the backslash and the
// quote after "s" opens a new literal that swallows the token, which is why
// the PRD says to double the quote (two single quotes) on PostgreSQL.
func TestSubquery_QuotesInsideLiterals(t *testing.T) {
	tests := []struct {
		name     string
		d        sql.Dialect
		tbl      sql.Table
		sub      string
		wantSQL  string
		wantArgs []any
	}{
		{
			"postgres doubled quote", pg, pgTbl,
			`SELECT id FROM companies WHERE note = 'it''s $1 off' AND size > $`,
			`SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (SELECT id FROM companies WHERE note = 'it''s $1 off' AND size > $2)`,
			[]any{true, 10},
		},
		{
			"mysql doubled quote", my, myTbl,
			`SELECT id FROM companies WHERE note = 'it''s $1 off' AND size > $`,
			"SELECT * FROM `users` WHERE active = ? AND company_id IN (SELECT id FROM companies WHERE note = 'it''s $1 off' AND size > ?)",
			[]any{true, 10},
		},
		{
			"sqlite doubled quote", lite, ltTbl,
			`SELECT id FROM companies WHERE note = 'it''s $1 off' AND size > $`,
			`SELECT * FROM "users" WHERE active = ? AND company_id IN (SELECT id FROM companies WHERE note = 'it''s $1 off' AND size > ?)`,
			[]any{true, 10},
		},
		{
			"postgres E string is not recognised", pg, pgTbl,
			`SELECT id FROM companies WHERE note = E'it\'s' AND size > $`,
			`SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (SELECT id FROM companies WHERE note = E'it\'s' AND size > $)`,
			[]any{true, 10},
		},
		{
			"postgres stdlib E string is not recognised", pgStd, pgTbl,
			`SELECT id FROM companies WHERE note = E'it\'s' AND size > $`,
			`SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (SELECT id FROM companies WHERE note = E'it\'s' AND size > $)`,
			[]any{true, 10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelect(tt.d, tt.tbl, sql.SelectOptions{Conditions: []sql.Condition{
				sql.Where("active").Eq(true),
				sql.Where("company_id").In(sql.Subquery{SQL: tt.sub, Args: []any{10}}),
			}})
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelect(In(Subquery %q)) =\n  %s\nwant\n  %s", tt.sub, gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildSelect(In(Subquery %q)) args mismatch (-want +got):\n%s", tt.sub, diff)
			}
		})
	}
}

// TestInNinSubqueryPointer pins that In / Nin given a lone *Subquery render
// the nested SELECT, as a Subquery value does, instead of binding the pointer
// as one parameter value. A nil *Subquery has no SELECT to render, so it stays
// a one-value list, as In(nil) does, and binds NULL.
func TestInNinSubqueryPointer(t *testing.T) {
	sub := sql.Subquery{SQL: `SELECT id FROM companies WHERE size > $`, Args: []any{10}}
	var nilSub *sql.Subquery
	tests := []struct {
		name     string
		d        sql.Dialect
		cond     sql.Condition
		wantSQL  string
		wantArgs []any
	}{
		{"postgres in", pg, sql.Where("company_id").In(&sub), `SELECT * FROM "users" WHERE company_id IN (SELECT id FROM companies WHERE size > $1)`, []any{10}},
		{"postgres stdlib nin", pgStd, sql.Where("company_id").Nin(&sub), `SELECT * FROM "users" WHERE company_id NOT IN (SELECT id FROM companies WHERE size > $1)`, []any{10}},
		{"mysql in", my, sql.Where("company_id").In(&sub), "SELECT * FROM `users` WHERE company_id IN (SELECT id FROM companies WHERE size > ?)", []any{10}},
		{"sqlite nin", lite, sql.Where("company_id").Nin(&sub), `SELECT * FROM "users" WHERE company_id NOT IN (SELECT id FROM companies WHERE size > ?)`, []any{10}},
		{"postgres nil pointer in", pg, sql.Where("company_id").In(nilSub), `SELECT * FROM "users" WHERE company_id IN ($1)`, []any{nilSub}},
		{"sqlite nil pointer nin", lite, sql.Where("company_id").Nin(nilSub), `SELECT * FROM "users" WHERE company_id NOT IN (?)`, []any{nilSub}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelect(tt.d, sql.Table{Name: "users"}, sql.SelectOptions{Conditions: []sql.Condition{tt.cond}})
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelect(%s) =\n  %s\nwant\n  %s", tt.name, gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildSelect(%s) args mismatch (-want +got):\n%s", tt.name, diff)
			}
		})
	}
}
