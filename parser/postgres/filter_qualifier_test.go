package postgres

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
			want:  "d.entity_type = 'asset.primary' AND d.name LIKE 'site_%'",
		},
		{
			name:  "multi-column OR",
			expr:  "status = 'active' OR status = 'pending'",
			alias: "d",
			want:  "d.status = 'active' OR d.status = 'pending'",
		},
		{
			name:  "IN list",
			expr:  "kind IN ('a', 'b', 'c')",
			alias: "d",
			want:  "d.kind IN ('a', 'b', 'c')",
		},
		{
			name:  "IS NOT NULL",
			expr:  "deleted_at IS NULL AND name IS NOT NULL",
			alias: "d",
			want:  "d.deleted_at IS NULL AND d.name IS NOT NULL",
		},
		{
			name:  "BETWEEN",
			expr:  "version BETWEEN 1 AND 5",
			alias: "d",
			want:  "d.version BETWEEN 1 AND 5",
		},
		{
			name:  "parenthesized group",
			expr:  "(status = 'active' OR status = 'pending') AND name LIKE 'x%'",
			alias: "d",
			want:  "(d.status = 'active' OR d.status = 'pending') AND d.name LIKE 'x%'",
		},
		{
			name:  "function call argument is qualified",
			expr:  "lower(name) = 'foo'",
			alias: "d",
			want:  "lower(d.name) = 'foo'",
		},
		{
			name:  "already-qualified pass-through",
			expr:  "d.entity_type = 'x' AND d.name LIKE 'y%'",
			alias: "d",
			want:  "d.entity_type = 'x' AND d.name LIKE 'y%'",
		},
		{
			name:  "string literal containing keywords is not qualified",
			expr:  "name = 'a AND b OR c'",
			alias: "d",
			want:  "d.name = 'a AND b OR c'",
		},
		{
			name:  "double-quoted identifier",
			expr:  `"col with space" = 'x'`,
			alias: "d",
			want:  `d."col with space" = 'x'`,
		},
		{
			name:  "jsonb operator",
			expr:  "data->>'k' = 'v'",
			alias: "d",
			want:  "(d.data ->> 'k') = 'v'",
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

// TestQualifyFilterIdentifiers_QuotingIsDialectSpecific is the PostgreSQL half
// of the cross-dialect quoting table in PRD §13.7.1. `"x"` is the standard
// quoted identifier here and means what the author wrote — the opposite of
// MySQL, where it is a string literal (see the MySQL package's
// TestQualifyFilterIdentifiers_DoubleQuoteIsAStringLiteral).
//
// The two mistakes a `filter:` author can make travel in opposite directions,
// and only one of them is loud:
//
//   - a backtick, MySQL's identifier quote, is a syntax error here — caught at
//     generation time with the offending expression and relationship named;
//   - a double-quoted *value*, MySQL's other legal spelling, parses as a column
//     reference and is duly alias-qualified, so it survives generation and
//     fails at query time as an unknown column.
//
// Single-quoted values and bare identifiers are the spelling that means the
// same thing on all three dialects.
func TestQualifyFilterIdentifiers_QuotingIsDialectSpecific(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    string
		want    string
		wantErr bool
	}{
		{
			name: "double quotes are the identifier quote",
			expr: `"entity_type" = 'asset.primary'`,
			want: "d.entity_type = 'asset.primary'",
		},
		{
			name: "double quotes keep a reserved word a column",
			expr: `"order" = 1`,
			want: `d."order" = 1`,
		},
		{
			name:    "a MySQL backtick is a syntax error, not a silent literal",
			expr:    "`entity_type` = 'asset.primary'",
			wantErr: true,
		},
		{
			name: "a double-quoted value becomes a column reference",
			expr: `entity_type = "asset.primary"`,
			want: `d.entity_type = d."asset.primary"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := QualifyFilterIdentifiers(tt.expr, "d")
			if (err != nil) != tt.wantErr {
				t.Fatalf("QualifyFilterIdentifiers(%q, %q) err = %v, wantErr = %v", tt.expr, "d", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if normalizeWS(got) != normalizeWS(tt.want) {
				t.Errorf("QualifyFilterIdentifiers(%q, %q):\n  got:  %s\n  want: %s", tt.expr, "d", got, tt.want)
			}
		})
	}
}
