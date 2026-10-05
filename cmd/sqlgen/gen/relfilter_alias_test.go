package gen

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestQualifiedAliasPrefix pins what each dialect's qualifier actually spells,
// rather than hardcoding it (a hardcoded spelling was wrong for SQLite).
//
// The prefix is derived by round-tripping a probe through the same qualifier
// that produces the predicate, so these values are not a contract the codegen
// imposes — they are a reading of what pg_query, vitess and rqlite/sql emit. If
// an upstream parser changes its deparsed spelling, this test changes with it
// and the splice keeps working; nothing needs to be hunted down.
func TestQualifiedAliasPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dialect config.Dialect
		want    string
	}{
		{"postgres deparses bare identifiers", config.DialectPostgres, "sqlgenrel."},
		{"mysql deparses bare identifiers", config.DialectMySQL, "sqlgenrel."},
		{"sqlite quotes every identifier", config.DialectSQLite, `"sqlgenrel".`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := qualifiedAliasPrefix(RelationshipFilterAliasToken, tt.dialect)
			if err != nil {
				t.Fatalf("qualifiedAliasPrefix(%q, %s) = %v", RelationshipFilterAliasToken, tt.dialect, err)
			}
			if got != tt.want {
				t.Errorf("qualifiedAliasPrefix(%q, %s) = %q, want %q",
					RelationshipFilterAliasToken, tt.dialect, got, tt.want)
			}
		})
	}
}

func TestQualifiedAliasPrefix_UnsupportedDialect(t *testing.T) {
	t.Parallel()
	if _, err := qualifiedAliasPrefix(RelationshipFilterAliasToken, config.Dialect("oracle")); err == nil {
		t.Error("qualifiedAliasPrefix(_, \"oracle\") = nil error, want one")
	}
}

// TestSpliceRelationshipFilterAlias covers the splice itself against both
// spellings, driven by the prefix rather than by a token the function knows.
//
// The SQLite row is the regression case: given `"sqlgenrel".` the splice now matches, and
// the column keeps the quoting its own dialect gave it — `tgt0."entity_type"`,
// which SQLite resolves against the bare `tgt0` the FROM clause declares.
func TestSpliceRelationshipFilterAlias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		qualified string
		prefix    string
		want      string
	}{
		{
			name:      "unquoted spelling, one reference",
			qualified: "sqlgenrel.entity_type = 'asset.primary'",
			prefix:    "sqlgenrel.",
			want:      `tgt + ".entity_type = 'asset.primary'"`,
		},
		{
			name:      "unquoted spelling, several references",
			qualified: "sqlgenrel.entity_type = 'a' AND sqlgenrel.name LIKE 'x%'",
			prefix:    "sqlgenrel.",
			want:      `tgt + ".entity_type = 'a' AND " + tgt + ".name LIKE 'x%'"`,
		},
		{
			name:      "quoted spelling, one reference",
			qualified: `"sqlgenrel"."entity_type" = 'asset.primary'`,
			prefix:    `"sqlgenrel".`,
			want:      `tgt + ".\"entity_type\" = 'asset.primary'"`,
		},
		{
			name:      "quoted spelling, several references",
			qualified: `"sqlgenrel"."entity_type" = 'a' AND "sqlgenrel"."name" LIKE 'x%'`,
			prefix:    `"sqlgenrel".`,
			want:      `tgt + ".\"entity_type\" = 'a' AND " + tgt + ".\"name\" LIKE 'x%'"`,
		},
		{
			name:      "leading text before the first reference",
			qualified: "NOT sqlgenrel.archived",
			prefix:    "sqlgenrel.",
			want:      `"NOT " + tgt + ".archived"`,
		},
		{
			name:      "no bare identifier to qualify",
			qualified: "1 = 1",
			prefix:    "sqlgenrel.",
			want:      `"1 = 1"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := spliceRelationshipFilterAlias(tt.qualified, tt.prefix); got != tt.want {
				t.Errorf("spliceRelationshipFilterAlias(%q, %q) =\n  got:  %s\n  want: %s",
					tt.qualified, tt.prefix, got, tt.want)
			}
		})
	}
}
