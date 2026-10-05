package sqlite

import (
	"strings"
	"testing"
)

func TestQualifyFilterIdentifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    string
		alias   string
		want    string
		wantErr bool
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
			name:    "unparseable expression errors",
			expr:    "WHERE AND OR",
			alias:   "d",
			wantErr: true,
		},
		{
			name:    "empty expression errors",
			expr:    "   ",
			alias:   "d",
			wantErr: true,
		},
		{
			name:    "empty alias errors",
			expr:    "name = 'x'",
			alias:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := QualifyFilterIdentifiers(tt.expr, tt.alias)
			if (err != nil) != tt.wantErr {
				t.Fatalf("QualifyFilterIdentifiers(%q, %q) err = %v, wantErr = %v", tt.expr, tt.alias, err, tt.wantErr)
			}
			if tt.wantErr {
				return
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
