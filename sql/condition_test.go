package sql_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/teandresmith/sqlgen/sql"
)

func TestConditionBuilder_Eq(t *testing.T) {
	got := sql.Where("col").Eq(42)
	want := sql.Condition{Clause: "col = $", Value: 42, Column: "col"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Where(\"col\").Eq(42) mismatch (-want +got):\n%s", diff)
	}
}

func TestConditionBuilder_Neq(t *testing.T) {
	got := sql.Where("col").Neq("val")
	want := sql.Condition{Clause: "col != $", Value: "val", Column: "col"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Where(\"col\").Neq(\"val\") mismatch (-want +got):\n%s", diff)
	}
}

func TestConditionBuilder_Comparison(t *testing.T) {
	tests := []struct {
		name string
		got  sql.Condition
		want sql.Condition
	}{
		{
			name: "Gt",
			got:  sql.Where("col").Gt(10),
			want: sql.Condition{Clause: "col > $", Value: 10, Column: "col"},
		},
		{
			name: "Gte",
			got:  sql.Where("col").Gte(10),
			want: sql.Condition{Clause: "col >= $", Value: 10, Column: "col"},
		},
		{
			name: "Lt",
			got:  sql.Where("col").Lt(10),
			want: sql.Condition{Clause: "col < $", Value: 10, Column: "col"},
		},
		{
			name: "Lte",
			got:  sql.Where("col").Lte(10),
			want: sql.Condition{Clause: "col <= $", Value: 10, Column: "col"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, tt.got); diff != "" {
				t.Errorf("Where(\"col\").%s(10) mismatch (-want +got):\n%s", tt.name, diff)
			}
		})
	}
}

func TestConditionBuilder_In(t *testing.T) {
	got := sql.Where("status").In("active", "pending")
	want := sql.Condition{Clause: "status IN $", Value: []any{"active", "pending"}, Column: "status"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Where(\"status\").In(\"active\", \"pending\") mismatch (-want +got):\n%s", diff)
	}
}

func TestConditionBuilder_Nin(t *testing.T) {
	got := sql.Where("status").Nin("deleted", "banned")
	want := sql.Condition{Clause: "status NOT IN $", Value: []any{"deleted", "banned"}, Column: "status"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Where(\"status\").Nin(\"deleted\", \"banned\") mismatch (-want +got):\n%s", diff)
	}
}

func TestConditionBuilder_Like(t *testing.T) {
	got := sql.Where("name").Like("%widget%")
	want := sql.Condition{Clause: "name LIKE $", Value: "%widget%", Column: "name"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Where(\"name\").Like(\"%%widget%%\") mismatch (-want +got):\n%s", diff)
	}
}

func TestConditionBuilder_NLike(t *testing.T) {
	got := sql.Where("name").NLike("%test%")
	want := sql.Condition{Clause: "name NOT LIKE $", Value: "%test%", Column: "name"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Where(\"name\").NLike(\"%%test%%\") mismatch (-want +got):\n%s", diff)
	}
}

func TestConditionBuilder_Between(t *testing.T) {
	got := sql.Where("price").Between(10, 100)
	want := sql.Condition{Clause: "price BETWEEN $ AND $", Value: sql.Range{Start: 10, End: 100}, Column: "price"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Where(\"price\").Between(10, 100) mismatch (-want +got):\n%s", diff)
	}
}

func TestConditionBuilder_IsNull(t *testing.T) {
	got := sql.Where("deleted_at").IsNull()
	want := sql.Condition{Clause: "deleted_at IS NULL", Value: nil, Column: "deleted_at"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Where(\"deleted_at\").IsNull() mismatch (-want +got):\n%s", diff)
	}
}

func TestConditionBuilder_IsNotNull(t *testing.T) {
	got := sql.Where("email").IsNotNull()
	want := sql.Condition{Clause: "email IS NOT NULL", Value: nil, Column: "email"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Where(\"email\").IsNotNull() mismatch (-want +got):\n%s", diff)
	}
}

func TestAnd(t *testing.T) {
	cond1 := sql.Where("price").Gte(50.00)
	cond2 := sql.Where("stock").Gt(0)

	got := sql.And(cond1, cond2)

	conditions, ok := got.IsAnd()
	if !ok {
		t.Fatal("And() did not produce an AND composition")
	}
	want := []sql.Condition{cond1, cond2}
	if diff := cmp.Diff(want, conditions); diff != "" {
		t.Errorf("And() conditions mismatch (-want +got):\n%s", diff)
	}
}

func TestOr(t *testing.T) {
	cond1 := sql.Where("role").Eq("admin")
	cond2 := sql.Where("role").Eq("superadmin")

	got := sql.Or(cond1, cond2)

	conditions, ok := got.IsOr()
	if !ok {
		t.Fatal("Or() did not produce an OR composition")
	}
	want := []sql.Condition{cond1, cond2}
	if diff := cmp.Diff(want, conditions); diff != "" {
		t.Errorf("Or() conditions mismatch (-want +got):\n%s", diff)
	}
}

func TestNestedComposition(t *testing.T) {
	// And(
	//   Where("active").Eq(true),
	//   Or(
	//     Where("role").Eq("admin"),
	//     And(
	//       Where("role").Eq("user"),
	//       Where("verified").Eq(true),
	//     ),
	//   ),
	// )
	innerAnd := sql.And(
		sql.Where("role").Eq("user"),
		sql.Where("verified").Eq(true),
	)
	middle := sql.Or(
		sql.Where("role").Eq("admin"),
		innerAnd,
	)
	outer := sql.And(
		sql.Where("active").Eq(true),
		middle,
	)

	// Verify outer is And
	outerConds, ok := outer.IsAnd()
	if !ok {
		t.Fatal("outer condition is not an AND composition")
	}
	if len(outerConds) != 2 {
		t.Fatalf("outer And has %d conditions, want 2", len(outerConds))
	}

	// Verify first is the simple Eq condition
	if outerConds[0].Clause != "active = $" {
		t.Errorf("outer[0].Clause = %q, want %q", outerConds[0].Clause, "active = $")
	}

	// Verify second is Or
	orConds, ok := outerConds[1].IsOr()
	if !ok {
		t.Fatal("outer[1] is not an OR composition")
	}
	if len(orConds) != 2 {
		t.Fatalf("inner Or has %d conditions, want 2", len(orConds))
	}

	// Verify the nested And inside the Or
	innerAndConds, ok := orConds[1].IsAnd()
	if !ok {
		t.Fatal("inner Or[1] is not an AND composition")
	}
	if len(innerAndConds) != 2 {
		t.Fatalf("innermost And has %d conditions, want 2", len(innerAndConds))
	}
	if innerAndConds[0].Clause != "role = $" {
		t.Errorf("innermost And[0].Clause = %q, want %q", innerAndConds[0].Clause, "role = $")
	}
	if innerAndConds[1].Clause != "verified = $" {
		t.Errorf("innermost And[1].Clause = %q, want %q", innerAndConds[1].Clause, "verified = $")
	}
}

// TestRaw pins the constructor's shape. Every arity sets Raw, which is what
// makes the clause render as a single AND-able term; see
// TestBuildSelect_RawConditionParenthesised for the rendering half.
func TestRaw(t *testing.T) {
	tests := []struct {
		name  string
		got   sql.Condition
		want  sql.Condition
		cmpOp cmp.Options
	}{
		{
			name: "single arg",
			got:  sql.Raw("price * quantity > $", 1000),
			want: sql.Condition{Clause: "price * quantity > $", Value: 1000, Raw: true},
		},
		{
			name:  "multiple args",
			got:   sql.Raw("col BETWEEN $ AND $", 1, 10),
			want:  sql.Condition{Clause: "col BETWEEN $ AND $", Value: []any{1, 10}, Raw: true},
			cmpOp: cmp.Options{cmpopts.EquateEmpty()},
		},
		{
			name: "no args",
			got:  sql.Raw("col IS NULL"),
			want: sql.Condition{Clause: "col IS NULL", Value: nil, Raw: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, tt.got, tt.cmpOp...); diff != "" {
				t.Errorf("Raw() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConditionBuilder_AllMethods(t *testing.T) {
	// Comprehensive table-driven test for all 12 builder methods.
	tests := []struct {
		name string
		got  sql.Condition
		want sql.Condition
	}{
		{
			name: "Eq string",
			got:  sql.Where("name").Eq("alice"),
			want: sql.Condition{Clause: "name = $", Value: "alice", Column: "name"},
		},
		{
			name: "Neq int",
			got:  sql.Where("id").Neq(0),
			want: sql.Condition{Clause: "id != $", Value: 0, Column: "id"},
		},
		{
			name: "Gt float",
			got:  sql.Where("price").Gt(9.99),
			want: sql.Condition{Clause: "price > $", Value: 9.99, Column: "price"},
		},
		{
			name: "Gte float",
			got:  sql.Where("price").Gte(9.99),
			want: sql.Condition{Clause: "price >= $", Value: 9.99, Column: "price"},
		},
		{
			name: "Lt int",
			got:  sql.Where("age").Lt(18),
			want: sql.Condition{Clause: "age < $", Value: 18, Column: "age"},
		},
		{
			name: "Lte int",
			got:  sql.Where("age").Lte(65),
			want: sql.Condition{Clause: "age <= $", Value: 65, Column: "age"},
		},
		{
			name: "In strings",
			got:  sql.Where("status").In("a", "b", "c"),
			want: sql.Condition{Clause: "status IN $", Value: []any{"a", "b", "c"}, Column: "status"},
		},
		{
			name: "Nin ints",
			got:  sql.Where("id").Nin(1, 2, 3),
			want: sql.Condition{Clause: "id NOT IN $", Value: []any{1, 2, 3}, Column: "id"},
		},
		{
			name: "Like",
			got:  sql.Where("email").Like("%@example.com"),
			want: sql.Condition{Clause: "email LIKE $", Value: "%@example.com", Column: "email"},
		},
		{
			name: "NLike",
			got:  sql.Where("email").NLike("%@spam.com"),
			want: sql.Condition{Clause: "email NOT LIKE $", Value: "%@spam.com", Column: "email"},
		},
		{
			name: "Between",
			got:  sql.Where("created_at").Between("2024-01-01", "2024-12-31"),
			want: sql.Condition{Clause: "created_at BETWEEN $ AND $", Value: sql.Range{Start: "2024-01-01", End: "2024-12-31"}, Column: "created_at"},
		},
		{
			name: "IsNull",
			got:  sql.Where("deleted_at").IsNull(),
			want: sql.Condition{Clause: "deleted_at IS NULL", Value: nil, Column: "deleted_at"},
		},
		{
			name: "IsNotNull",
			got:  sql.Where("verified_at").IsNotNull(),
			want: sql.Condition{Clause: "verified_at IS NOT NULL", Value: nil, Column: "verified_at"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, tt.got); diff != "" {
				t.Errorf("condition mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPrefixConditions_QualifiesNamedColumn covers the Column-aware path: a
// clause whose column is not its leading token must have the column qualified
// where it sits, not the clause prefixed wholesale.
func TestPrefixConditions_QualifiesNamedColumn(t *testing.T) {
	tests := []struct {
		name string
		cond sql.Condition
		want sql.Condition
	}{
		{
			name: "column-leading clause qualifies as before",
			cond: sql.Condition{Clause: "settings = $", Value: 1, Column: "settings"},
			want: sql.Condition{Clause: "t.settings = $", Value: 1, Column: "t.settings"},
		},
		{
			name: "function-leading clause qualifies the argument",
			cond: sql.Condition{Clause: "JSON_CONTAINS(settings, $)", Value: 1, Column: "settings"},
			want: sql.Condition{Clause: "JSON_CONTAINS(t.settings, $)", Value: 1, Column: "t.settings"},
		},
		{
			// The column name also appears inside a quoted path literal. The
			// first whole-token match is the argument, which is the one to
			// qualify — a literal must never be rewritten.
			name: "column name repeated inside a literal",
			cond: sql.Condition{Clause: "JSON_CONTAINS_PATH(one, 'one', $)", Value: 1, Column: "one"},
			want: sql.Condition{Clause: "JSON_CONTAINS_PATH(t.one, 'one', $)", Value: 1, Column: "t.one"},
		},
		{
			// A longer identifier that merely starts with the column name is
			// not the column.
			name: "no partial-identifier match",
			cond: sql.Condition{Clause: "id_backup = id", Value: nil, Column: "id"},
			want: sql.Condition{Clause: "id_backup = t.id", Value: nil, Column: "t.id"},
		},
		{
			// Unset Column keeps the historical prepend, which is what Raw and
			// hand-built conditions rely on.
			name: "empty Column falls back to prepending",
			cond: sql.Condition{Clause: "price * quantity > $", Value: 1000},
			want: sql.Condition{Clause: "t.price * quantity > $", Value: 1000},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sql.PrefixConditions("t", []sql.Condition{tt.cond})
			if diff := cmp.Diff([]sql.Condition{tt.want}, got); diff != "" {
				t.Errorf("PrefixConditions(%q) mismatch (-want +got):\n%s", tt.cond.Clause, diff)
			}
		})
	}
}

// TestPrefixConditions_QualifiesInsideComposition pins that the Column-aware
// path descends into AND/OR compositions, which is how a filter's `and:` /
// `or:` members reach the join query.
func TestPrefixConditions_QualifiesInsideComposition(t *testing.T) {
	got := sql.PrefixConditions("t", []sql.Condition{
		sql.And(
			sql.Condition{Clause: "JSON_CONTAINS(settings, $)", Value: 1, Column: "settings"},
			sql.Where("name").Eq("x"),
		),
	})
	want := []sql.Condition{
		sql.And(
			sql.Condition{Clause: "JSON_CONTAINS(t.settings, $)", Value: 1, Column: "t.settings"},
			sql.Condition{Clause: "t.name = $", Value: "x", Column: "t.name"},
		),
	}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported()); diff != "" {
		t.Errorf("PrefixConditions(And(...)) mismatch (-want +got):\n%s", diff)
	}
}

// TestKeysetConditions_QuotedColumn pins the shape the generated cursorKeyset
// helper builds: each cursor key is quoted by the dialect, and the
// quoted token is also the condition's Column. The keys here are a keyword,
// a mixed-case name and a digit-leading name, none of which parse unquoted on
// every dialect. The plain read path renders the quoted keys as they are; the
// O2O-join path qualifies them through PrefixConditions, which has to find
// the quoted token and put the alias in front of it (`u."2024_quota"`), not
// inside it.
func TestKeysetConditions_QuotedColumn(t *testing.T) {
	// cursorKeyset(d, keys, cursor, ">") for keys [order MixedCase 2024_quota].
	keyset := func(d sql.Dialect) sql.Condition {
		cond := func(key, op string, v any) sql.Condition {
			col := d.QuoteIdentifier(key)
			return sql.Condition{Clause: col + " " + op + " $", Value: v, Column: col}
		}
		return sql.Or(
			sql.And(cond("order", ">", 1)),
			sql.And(cond("order", "=", 1), cond("MixedCase", ">", 2)),
			sql.And(cond("order", "=", 1), cond("MixedCase", "=", 2), cond("2024_quota", ">", 3)),
		)
	}
	join := func(d sql.Dialect, schema string) sql.JoinClause {
		return sql.JoinClause{
			Table:   sql.Table{Schema: schema, Name: "profiles"},
			Alias:   "p",
			On:      "p." + d.QuoteIdentifier("user_id") + " = u." + d.QuoteIdentifier("id"),
			Columns: []string{"id"},
		}
	}

	tests := []struct {
		name      string
		dialect   sql.Dialect
		table     sql.Table
		schema    string
		wantPlain string
		wantJoin  string
	}{
		{
			name:      "postgres",
			dialect:   pg,
			table:     pgTbl,
			schema:    "public",
			wantPlain: `SELECT "id" FROM "public"."users" WHERE (("order" > $1) OR ("order" = $2 AND "MixedCase" > $3) OR ("order" = $4 AND "MixedCase" = $5 AND "2024_quota" > $6))`,
			wantJoin:  `SELECT u."id" AS "u.id", p."id" AS "p.id" FROM "public"."users" u LEFT JOIN "public"."profiles" p ON p."user_id" = u."id" WHERE ((u."order" > $1) OR (u."order" = $2 AND u."MixedCase" > $3) OR (u."order" = $4 AND u."MixedCase" = $5 AND u."2024_quota" > $6))`,
		},
		{
			name:      "mysql",
			dialect:   my,
			table:     myTbl,
			wantPlain: "SELECT `id` FROM `users` WHERE ((`order` > ?) OR (`order` = ? AND `MixedCase` > ?) OR (`order` = ? AND `MixedCase` = ? AND `2024_quota` > ?))",
			wantJoin:  "SELECT u.`id` AS `u.id`, p.`id` AS `p.id` FROM `users` u LEFT JOIN `profiles` p ON p.`user_id` = u.`id` WHERE ((u.`order` > ?) OR (u.`order` = ? AND u.`MixedCase` > ?) OR (u.`order` = ? AND u.`MixedCase` = ? AND u.`2024_quota` > ?))",
		},
		{
			name:      "sqlite",
			dialect:   lite,
			table:     ltTbl,
			wantPlain: `SELECT "id" FROM "users" WHERE (("order" > ?) OR ("order" = ? AND "MixedCase" > ?) OR ("order" = ? AND "MixedCase" = ? AND "2024_quota" > ?))`,
			wantJoin:  `SELECT u."id" AS "u.id", p."id" AS "p.id" FROM "users" u LEFT JOIN "profiles" p ON p."user_id" = u."id" WHERE ((u."order" > ?) OR (u."order" = ? AND u."MixedCase" > ?) OR (u."order" = ? AND u."MixedCase" = ? AND u."2024_quota" > ?))`,
		},
	}

	wantArgs := []any{1, 1, 2, 1, 2, 3}

	for _, tt := range tests {
		t.Run(tt.name+"/plain", func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelect(tt.dialect, tt.table, sql.SelectOptions{
				Columns:    []string{"id"},
				Conditions: []sql.Condition{keyset(tt.dialect)},
			})
			if gotSQL != tt.wantPlain {
				t.Errorf("BuildSelect() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantPlain)
			}
			if diff := cmp.Diff(wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildSelect() args mismatch (-want +got):\n%s", diff)
			}
		})
		t.Run(tt.name+"/o2o-join", func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelectJoin(tt.dialect, tt.table, []sql.JoinClause{join(tt.dialect, tt.schema)}, sql.SelectOptions{
				Alias:      "u",
				Columns:    []string{"id"},
				Conditions: sql.PrefixConditions("u", []sql.Condition{keyset(tt.dialect)}),
			})
			if gotSQL != tt.wantJoin {
				t.Errorf("BuildSelectJoin() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantJoin)
			}
			if diff := cmp.Diff(wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildSelectJoin() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// existsCond is the shape the relationship-filter generator will emit: a
// correlated subquery whose parent-side reference is sql.CorrelationToken and
// never a baked-in qualifier.
func existsCond() sql.Condition {
	return sql.Exists("id", sql.Subquery{
		SQL: `SELECT 1 FROM "public"."orders" tgt WHERE tgt."user_id" = ` + sql.CorrelationToken +
			` AND tgt."status" = $`,
		Args: []any{"paid"},
	})
}

// TestPrefixConditions_LeavesExistsUntouched pins the exemption that
// builder-side EXISTS qualifier resolution relies on: an Exists is a structured
// value, not a leaf clause, so alias prefixing passes it through while still
// qualifying its siblings. The exemption is by
// construction — prefixCondition has no case for it.
func TestPrefixConditions_LeavesExistsUntouched(t *testing.T) {
	exists := existsCond()
	got := sql.PrefixConditions("t", []sql.Condition{
		{Clause: `"status" = $`, Value: "active", Column: `"status"`},
		exists,
		sql.Where("name").Eq("x"),
	})
	want := []sql.Condition{
		{Clause: `t."status" = $`, Value: "active", Column: `t."status"`},
		exists,
		{Clause: "t.name = $", Value: "x", Column: "t.name"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("PrefixConditions with an Exists sibling mismatch (-want +got):\n%s", diff)
	}
}

// TestPrefixConditions_LeavesNestedExistsUntouched is the same guarantee two
// levels deep, where prefixing descends recursively through And/Or.
func TestPrefixConditions_LeavesNestedExistsUntouched(t *testing.T) {
	exists := existsCond()
	got := sql.PrefixConditions("t", []sql.Condition{
		sql.And(
			sql.Where("active").Eq(true),
			sql.Or(
				sql.Where("role").Eq("admin"),
				exists,
			),
		),
	})
	want := []sql.Condition{
		sql.And(
			sql.Condition{Clause: "t.active = $", Value: true, Column: "t.active"},
			sql.Or(
				sql.Condition{Clause: "t.role = $", Value: "admin", Column: "t.role"},
				exists,
			),
		),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("PrefixConditions with a nested Exists mismatch (-want +got):\n%s", diff)
	}
}
