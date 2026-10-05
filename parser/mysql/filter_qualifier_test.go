package mysql

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
			want:  "d.entity_type = 'asset.primary'",
		},
		{
			name:  "multi-column AND",
			expr:  "entity_type = 'asset.primary' AND name LIKE 'site_%'",
			alias: "d",
			want:  "d.entity_type = 'asset.primary' and d.`name` like 'site_%'",
		},
		{
			name:  "multi-column OR",
			expr:  "status = 'active' OR status = 'pending'",
			alias: "d",
			want:  "d.`status` = 'active' or d.`status` = 'pending'",
		},
		{
			name:  "IN list",
			expr:  "kind IN ('a', 'b', 'c')",
			alias: "d",
			want:  "d.kind in ('a', 'b', 'c')",
		},
		{
			name:  "IS NOT NULL",
			expr:  "deleted_at IS NULL AND name IS NOT NULL",
			alias: "d",
			want:  "d.deleted_at is null and d.`name` is not null",
		},
		{
			name:  "BETWEEN",
			expr:  "version BETWEEN 1 AND 5",
			alias: "d",
			want:  "d.version between 1 and 5",
		},
		{
			name:  "parenthesized group",
			expr:  "(status = 'active' OR status = 'pending') AND name LIKE 'x%'",
			alias: "d",
			want:  "(d.`status` = 'active' or d.`status` = 'pending') and d.`name` like 'x%'",
		},
		{
			name:  "function call argument is qualified",
			expr:  "lower(name) = 'foo'",
			alias: "d",
			want:  "lower(d.`name`) = 'foo'",
		},
		{
			name:  "already-qualified pass-through",
			expr:  "d.entity_type = 'x' AND d.name LIKE 'y%'",
			alias: "d",
			want:  "d.entity_type = 'x' and d.`name` like 'y%'",
		},
		{
			name:  "string literal containing keywords is not qualified",
			expr:  "name = 'a AND b OR c'",
			alias: "d",
			want:  "d.`name` = 'a AND b OR c'",
		},
		{
			name:  "backtick-quoted identifier",
			expr:  "`col with space` = 'x'",
			alias: "d",
			want:  "d.`col with space` = 'x'",
		},
		{
			name:  "case expression",
			expr:  "CASE WHEN status = 'a' THEN 1 ELSE 0 END = 1",
			alias: "d",
			want:  "case when d.`status` = 'a' then 1 else 0 end = 1",
		},
		{
			name:    "unparseable expression errors",
			expr:    "foo bar baz =",
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

// TestQualifyFilterIdentifiers_DoubleQuoteIsAStringLiteral demonstrates the
// MySQL-specific trap that `cmd/sqlgen/config` rejects a `filter:` for.
//
// MySQL's default sql_mode makes `"x"` a *string literal*, not a quoted
// identifier, and vitess reproduces that faithfully: the column reference
// disappears and the predicate becomes a comparison between two constants.
// Nothing here is a parser defect — it is what MySQL itself does — but the
// result is a relationship filter that matches no row on any table, and MySQL
// reports no error and (for a string-vs-string comparison) no warning either.
//
// Qualification is what makes it unrecoverable rather than merely wrong: the
// o2m/m2m loader hands the `filter:` text to MySQL verbatim, so ANSI_QUOTES
// rescues that path, while the o2o JOIN ON and relationship-filter EXISTS are
// rewritten here, at generation time, and ship the baked literal in the
// generated source. Backticks are the spelling that survives both.
func TestQualifyFilterIdentifiers_DoubleQuoteIsAStringLiteral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
		want string
	}{
		{
			name: "double-quoted identifier degrades to a constant comparison",
			expr: `"entity_type" = 'asset.primary'`,
			want: "'entity_type' = 'asset.primary'",
		},
		{
			name: "one bad conjunct does not disturb the others",
			expr: `"name" LIKE 'photo_%' AND status = 'live'`,
			want: "'name' like 'photo_%' and d.`status` = 'live'",
		},
		{
			name: "backticks are the spelling that keeps the column",
			expr: "`entity_type` = 'asset.primary'",
			want: "d.entity_type = 'asset.primary'",
		},
		{
			name: "backticks keep a reserved word a column",
			expr: "`order` = 1",
			want: "d.`order` = 1",
		},
		{
			name: "a double-quoted value is normalised to a string literal",
			expr: `entity_type = "asset.primary"`,
			want: "d.entity_type = 'asset.primary'",
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
