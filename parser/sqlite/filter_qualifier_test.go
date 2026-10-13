package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	rsql "github.com/rqlite/sql"
	_ "modernc.org/sqlite"
)

func TestQualifyFilterIdentifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    string
		alias   string
		want    string
		wantErr string // a substring of the error; "" means no error
	}{
		{
			name:  "single column equality",
			expr:  "entity_type = 'asset.primary'",
			alias: "d",
			want:  `"d"."entity_type" = 'asset.primary'`,
		},
		{
			name:  "multi-column AND",
			expr:  "entity_type = 'asset.primary' AND name LIKE 'site_%'",
			alias: "d",
			want:  `"d"."entity_type" = 'asset.primary' AND "d"."name" LIKE 'site_%'`,
		},
		{
			name:  "multi-column OR",
			expr:  "status = 'active' OR status = 'pending'",
			alias: "d",
			want:  `"d"."status" = 'active' OR "d"."status" = 'pending'`,
		},
		{
			name:  "IN list",
			expr:  "kind IN ('a', 'b', 'c')",
			alias: "d",
			want:  `"d"."kind" IN ('a', 'b', 'c')`,
		},
		{
			name:  "IS NOT NULL",
			expr:  "deleted_at IS NULL AND name IS NOT NULL",
			alias: "d",
			// rqlite/sql's deparser emits the postfix `NOTNULL` form, which is
			// semantically equivalent to `IS NOT NULL` in SQLite.
			want: `"d"."deleted_at" IS NULL AND "d"."name" NOT NULL`,
		},
		{
			name:  "BETWEEN",
			expr:  "version BETWEEN 1 AND 5",
			alias: "d",
			want:  `"d"."version" BETWEEN 1 AND 5`,
		},
		{
			name:  "parenthesized group",
			expr:  "(status = 'active' OR status = 'pending') AND name LIKE 'x%'",
			alias: "d",
			want:  `("d"."status" = 'active' OR "d"."status" = 'pending') AND "d"."name" LIKE 'x%'`,
		},
		{
			name:  "function call argument is qualified",
			expr:  "lower(name) = 'foo'",
			alias: "d",
			want:  `lower("d"."name") = 'foo'`,
		},
		{
			name:  "already-qualified pass-through",
			expr:  "d.entity_type = 'x' AND d.name LIKE 'y%'",
			alias: "d",
			want:  `"d"."entity_type" = 'x' AND "d"."name" LIKE 'y%'`,
		},
		{
			name:  "string literal containing keywords is not qualified",
			expr:  "name = 'a AND b OR c'",
			alias: "d",
			want:  `"d"."name" = 'a AND b OR c'`,
		},
		{
			name:  "quoted identifier",
			expr:  `"col with space" = 'x'`,
			alias: "d",
			want:  `"d"."col with space" = 'x'`,
		},
		{
			name:  "glob operator",
			expr:  "name GLOB 'photo_*'",
			alias: "d",
			want:  `"d"."name" GLOB 'photo_*'`,
		},
		{
			// rqlite/sql parses these keywords as identifiers; qualified, SQLite
			// reports "no such column: d.CURRENT_TIMESTAMP".
			name:  "time keywords are not columns",
			expr:  "expires_at > CURRENT_TIMESTAMP AND date(expires_at) <= current_date AND CURRENT_TIME IS NOT NULL",
			alias: "d",
			want:  `"d"."expires_at" > CURRENT_TIMESTAMP AND date("d"."expires_at") <= current_date AND CURRENT_TIME NOT NULL`,
		},
		{
			name:  "a quoted time keyword is a column",
			expr:  `"CURRENT_TIMESTAMP" > 0 AND ` + "`current_date`" + ` > 0`,
			alias: "d",
			want:  `"d"."CURRENT_TIMESTAMP" > 0 AND "d"."current_date" > 0`,
		},
		{
			name:    "unparseable expression errors",
			expr:    "WHERE AND OR",
			alias:   "d",
			wantErr: "qualify filter: parsing",
		},
		{
			name:  "LIKE ESCAPE",
			expr:  `name LIKE 'x\%' ESCAPE '\'`,
			alias: "d",
			want:  `"d"."name" LIKE 'x\%' ESCAPE '\'`,
		},
		{
			name:  "ESCAPE reading a column",
			expr:  `name NOT LIKE pattern ESCAPE esc`,
			alias: "d",
			want:  `"d"."name" NOT LIKE "d"."pattern" ESCAPE "d"."esc"`,
		},
		{
			name:  "IS NOT DISTINCT FROM",
			expr:  `name IS NOT DISTINCT FROM other`,
			alias: "d",
			want:  `"d"."name" IS NOT DISTINCT FROM "d"."other"`,
		},
		{
			name:    "empty expression errors",
			expr:    "   ",
			alias:   "d",
			wantErr: "qualify filter: empty expression",
		},
		{
			name:    "empty alias errors",
			expr:    "name = 'x'",
			alias:   "",
			wantErr: "qualify filter: empty alias",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := QualifyFilterIdentifiers(tt.expr, tt.alias)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("QualifyFilterIdentifiers(%q, %q) err = %v, want an error containing %q", tt.expr, tt.alias, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("QualifyFilterIdentifiers(%q, %q) = %v", tt.expr, tt.alias, err)
			}
			if normalizeWS(got) != normalizeWS(tt.want) {
				t.Errorf("QualifyFilterIdentifiers(%q, %q):\n  got:  %s\n  want: %s", tt.expr, tt.alias, got, tt.want)
			}
		})
	}
}

func TestQualifyFilterIdentifiers_Idempotent(t *testing.T) {
	t.Parallel()
	expr := "entity_type = 'asset.primary' AND name LIKE 'site_%'"
	first, err := QualifyFilterIdentifiers(expr, "d")
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	second, err := QualifyFilterIdentifiers(first, "d")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if normalizeWS(first) != normalizeWS(second) {
		t.Errorf("not idempotent:\n  first:  %s\n  second: %s", first, second)
	}
}

func normalizeWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// TestQualifyFilterIdentifiers_QuotingIsDialectSpecific is the SQLite half of
// the cross-dialect quoting table in PRD §13.7.1. SQLite is the permissive one:
// it accepts both the standard double quote and MySQL's backtick as identifier
// quotes, and normalises each to the double-quoted form.
//
// That permissiveness has one sharp edge worth pinning. A double-quoted *value*
// parses as a column reference here, exactly as it does on PostgreSQL. Left
// unqualified, SQLite's double-quoted-string misfeature would fall back to
// treating it as a literal and the predicate would appear to work — but
// qualification makes it `"d"."asset.primary"`, an unambiguous qualified column
// reference that the fallback no longer applies to, so it fails at query time.
// The o2m/m2m loader, which never qualifies, keeps the fallback and would
// silently behave differently on the same `filter:`.
//
// Single-quoted values and bare identifiers are the spelling that means the
// same thing on all three dialects.
func TestQualifyFilterIdentifiers_QuotingIsDialectSpecific(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
		want string
	}{
		{
			name: "double quotes are the identifier quote",
			expr: `"entity_type" = 'asset.primary'`,
			want: `"d"."entity_type" = 'asset.primary'`,
		},
		{
			name: "a MySQL backtick is accepted and normalised",
			expr: "`entity_type` = 'asset.primary'",
			want: `"d"."entity_type" = 'asset.primary'`,
		},
		{
			name: "a double-quoted value becomes a column reference",
			expr: `entity_type = "asset.primary"`,
			want: `"d"."entity_type" = "d"."asset.primary"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := QualifyFilterIdentifiers(tt.expr, "d")
			if err != nil {
				t.Fatalf("QualifyFilterIdentifiers(%q, %q) = %v", tt.expr, "d", err)
			}
			if normalizeWS(got) != normalizeWS(tt.want) {
				t.Errorf("QualifyFilterIdentifiers(%q, %q):\n  got:  %s\n  want: %s", tt.expr, "d", got, tt.want)
			}
		})
	}
}

// rqlite/sql panics rendering a node it has no text for; exprString returns
// that as an error, so generation fails instead of crashing.
func TestExprString_Panic(t *testing.T) {
	x := &rsql.BinaryExpr{X: &rsql.Ident{Name: "a"}, Op: rsql.ILLEGAL, Y: &rsql.Ident{Name: "b"}}
	_, err := exprString(x, "CHECK")
	const want = "rendering CHECK: sql.BinaryExpr.String(): invalid op ILLEGAL"
	if err == nil || err.Error() != want {
		t.Errorf("exprString() error = %v, want %q", err, want)
	}
}

// TestQualifyFilterIdentifiers_TimeKeywordRuns runs a qualified filter that
// compares with CURRENT_TIMESTAMP on SQLite. Rendered as the identifier
// "d"."CURRENT_TIMESTAMP", it fails with "no such column".
func TestQualifyFilterIdentifiers_TimeKeywordRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE docs (expires_at TEXT);
		INSERT INTO docs VALUES ('2000-01-01 00:00:00'), ('2999-01-01 00:00:00')`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	where, err := QualifyFilterIdentifiers("expires_at > CURRENT_TIMESTAMP", "d")
	if err != nil {
		t.Fatalf("QualifyFilterIdentifiers: %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM docs AS "d" WHERE `+where).Scan(&n); err != nil {
		t.Fatalf("filter %s: %v", where, err)
	}
	if n != 1 {
		t.Errorf("filter %s matched %d rows, want 1", where, n)
	}
}
