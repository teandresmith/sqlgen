package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// TestRelationshipStaticFilter_NoAliasTokenSurvives is the alias-splice regression pin:
// render a relationship filter on a `filter:` edge on all three dialects and
// assert the emitted SQL carries no alias placeholder.
//
// It has to run on every dialect, because the defect was invisible on two of
// them. The splice matched pg_query's and vitess's bare `sqlgenrel.`, while
// rqlite/sql emits `"sqlgenrel"."col"` — the split found nothing, the whole
// predicate came back as a Go literal with the placeholder still in it, and the
// subquery named a relation it never declared. Against the shipped sqlite
// example that read:
//
//	get assets: query: SQL logic error: no such column: sqlgenrel.entity_type (1)
//
// Only the EXISTS path was affected; the loader applies a `filter:` verbatim to
// a single-table query and never qualifies, which is why everything compiled
// and the existing tests passed.
func TestRelationshipStaticFilter_NoAliasTokenSurvives(t *testing.T) {
	for _, dialect := range []config.Dialect{config.DialectPostgres, config.DialectMySQL, config.DialectSQLite} {
		t.Run(string(dialect), func(t *testing.T) {
			tc := discriminatorTables(t, dialect, filterRel("Attachments", "one_to_many"))
			got := executeFilterTemplate(t, tc)

			if !strings.Contains(got, `subSQL += " AND ("`) {
				t.Fatalf("no static filter predicate emitted; the assertion below would be vacuous:\n%s", got)
			}
			if strings.Contains(got, gen.RelationshipFilterAliasToken) {
				t.Errorf("emitted subquery carries the codegen alias token %q, which names no relation at runtime:\n%s",
					gen.RelationshipFilterAliasToken, got)
			}
			// The placeholder must have become the runtime variable, not just
			// vanished — an empty predicate would also pass the check above.
			if !strings.Contains(got, `tgt + ".`) {
				t.Errorf("static filter does not splice the runtime alias:\n%s", got)
			}
		})
	}
}

// TestRelationshipStaticFilter_ColumnKeepsDialectSpelling is the other half: the
// column keeps whatever spelling its own dialect gave it, and only the alias is
// substituted. SQLite quotes identifiers unconditionally, so its predicate
// reads `tgt0."entity_type"` — valid, because the FROM clause's bare `tgt0` and
// a quoted `"tgt0"` name the same relation.
func TestRelationshipStaticFilter_ColumnKeepsDialectSpelling(t *testing.T) {
	tests := []struct {
		dialect config.Dialect
		want    string
	}{
		{config.DialectPostgres, `tgt + ".entity_type = 'asset.primary'"`},
		{config.DialectMySQL, `tgt + ".entity_type = 'asset.primary'"`},
		{config.DialectSQLite, `tgt + ".\"entity_type\" = 'asset.primary'"`},
	}

	for _, tt := range tests {
		t.Run(string(tt.dialect), func(t *testing.T) {
			tc := discriminatorTables(t, tt.dialect, filterRel("Attachments", "one_to_many"))
			got := executeFilterTemplate(t, tc)

			if !strings.Contains(got, tt.want) {
				t.Errorf("emitted subquery missing %s\ngot:\n%s", tt.want, got)
			}
		})
	}
}

// TestRelationshipStaticFilter_RejectsAliasTokenInFilter pins the guard that
// makes the splice total rather than merely careful.
//
// The substitution matches the alias placeholder in the qualifier's output, so
// a filter carrying one of its own is ambiguous. A *string literal* holding it
// is the case that bites: on the dialects whose spelling the splice matches, the
// literal's own text is rewritten into the runtime alias and the value compared
// silently changes — `'sqlgenrel.x'` becomes `'tgt0.x'`. Rejecting the input
// costs a generation; accepting it costs wrong rows on a query that succeeds.
func TestRelationshipStaticFilter_RejectsAliasTokenInFilter(t *testing.T) {
	tests := []struct {
		name   string
		filter string
	}{
		{"inside a string literal", "note = 'sqlgenrel.x'"},
		{"as a column name", "sqlgenrel = 1"},
		{"upper case", "note = 'SQLGENREL.x'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := filterRel("Attachments", "one_to_many")
			rel.Filter = tt.filter

			_, err := buildFilteredAssets(t, config.DialectPostgres, rel)
			if err == nil {
				t.Fatalf("BuildTableContextsFromSchema(filter=%q) = nil error, want one", tt.filter)
			}
			for _, want := range []string{"Attachments", gen.RelationshipFilterAliasToken} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %v, want it to name %q", err, want)
				}
			}
		})
	}
}

// TestRelationshipStaticFilter_RejectsSynthesizedAliasToken covers the half the
// raw-text guard cannot see: a predicate that spells the token only *after* the
// dialect parses it.
//
// MySQL folds adjacent string literals and PostgreSQL decodes escapes, both at
// parse time, so a filter whose config text contains no token deparses to one
// that does — inside a literal, where the splice would rewrite the value
// compared. The post-splice assertion is no help here: the token is consumed by
// the substitution, so there is nothing left in the output to find. Only
// comparing against a qualification under a different alias separates a real
// column reference from the filter's own text.
func TestRelationshipStaticFilter_RejectsSynthesizedAliasToken(t *testing.T) {
	tests := []struct {
		name    string
		dialect config.Dialect
		filter  string
	}{
		{"mysql folds adjacent literals", config.DialectMySQL, `note = 'sqlgen' 'rel.x'`},
		{"postgres decodes escapes", config.DialectPostgres, `note = E'sqlgen\162el.x'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := filterRel("Attachments", "one_to_many")
			rel.Filter = tt.filter

			_, err := buildFilteredAssets(t, tt.dialect, rel)
			if err == nil {
				t.Fatalf("BuildTableContextsFromSchema(%s, filter=%q) = nil error, want one", tt.dialect, tt.filter)
			}
			if !strings.Contains(err.Error(), "Attachments") {
				t.Errorf("error = %v, want it to name the relationship", err)
			}
		})
	}
}

// TestRelationshipStaticFilter_AcceptsOrdinaryFilters is the negative
// complement: the guard must not reject a predicate that merely resembles the
// token, or the fix trades one defect for another.
func TestRelationshipStaticFilter_AcceptsOrdinaryFilters(t *testing.T) {
	tests := []struct {
		name   string
		filter string
	}{
		{"plain equality", "entity_type = 'asset.primary'"},
		{"multi column", "entity_type = 'asset.primary' AND name LIKE 'site_%'"},
		{"top-level or", "entity_type = 'asset.primary' OR entity_type = 'asset.invoice'"},
		{"substring of the token", "note = 'sqlgen relations'"},
		{"no identifier to qualify", "1 = 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := filterRel("Attachments", "one_to_many")
			rel.Filter = tt.filter

			if _, err := buildFilteredAssets(t, config.DialectPostgres, rel); err != nil {
				t.Errorf("BuildTableContextsFromSchema(filter=%q) = %v, want nil", tt.filter, err)
			}
		})
	}
}

// buildFilteredAssets is discriminatorTables without the t.Fatal — the guard
// tests need the error, not a failed test.
func buildFilteredAssets(t *testing.T, dialect config.Dialect, rel config.TableRelationship) ([]gen.TableContext, error) {
	t.Helper()

	in := testInput(discriminatorSchema())
	in.Config.Input.Dialect = dialect
	if dialect != config.DialectPostgres {
		in.Config.Output.Driver = "stdlib"
	}
	parser.DetectRelationships(in.Schema)
	in.Config.Tables["assets"] = config.TableConfig{
		Relationships: []config.TableRelationship{rel},
	}

	return gen.BuildTableContextsFromSchema(in.Schema, in.Config)
}
