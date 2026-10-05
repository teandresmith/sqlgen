package models

import (
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

// The and / or composition rule (PRD §11.1) is emitted by shared/_filter.tmpl,
// so nothing inside the gen module can run it — the tests there assert the
// rendered text. These assert the compiled SQL from inside the generated
// package, following the nil_filter_test.go precedent alongside this file.
//
// The rule has two halves, and the shape that preceded it got both backwards:
//
//  1. Across members, `Or: [A, B]` is a disjunction. Each member used to be
//     wrapped in its own sql.Or and appended to conds, and the builder joins
//     conds with AND — so the union compiled to `A AND B`, which for two
//     mutually exclusive predicates matches nothing at all.
//  2. Within a member, a filter object's own fields are a conjunction. That
//     same per-member sql.Or OR'd a lone member's own fields against each
//     other, so `Or: [{x, y}]` compiled to `x OR y`.
//
// Both are asserted on the compiled string rather than the condition count,
// because the count is identical under either reading.
func TestToConditionsOrCompilesToDisjunction(t *testing.T) {
	dialect := sql.NewPostgresDialect()
	table := sql.Table{Schema: "public", Name: "articles"}

	alpha, bravo := "alpha", "bravo"

	tests := []struct {
		name   string
		filter *ArticleFilter
		want   string
	}{
		{
			// The consumer-reported case: members OR together.
			name: "members_or_together",
			filter: &ArticleFilter{Or: []*ArticleFilter{
				{Title: &comparator.String{Eq: &alpha}},
				{Author: &comparator.String{Eq: &bravo}},
			}},
			want: `SELECT COUNT(*) FROM "public"."articles" WHERE (("title" = $1) OR ("author" = $2))`,
		},
		{
			// A member's OWN fields are conjunctive even under `or`.
			name: "member_own_fields_and_together",
			filter: &ArticleFilter{Or: []*ArticleFilter{
				{Title: &comparator.String{Eq: &alpha}, Author: &comparator.String{Eq: &bravo}},
			}},
			want: `SELECT COUNT(*) FROM "public"."articles" WHERE (("author" = $1 AND "title" = $2))`,
		},
		{
			// And is unchanged: members AND together, as do their own fields.
			name: "and_members_and_together",
			filter: &ArticleFilter{And: []*ArticleFilter{
				{Title: &comparator.String{Eq: &alpha}},
				{Author: &comparator.String{Eq: &bravo}},
			}},
			want: `SELECT COUNT(*) FROM "public"."articles" WHERE ("title" = $1) AND ("author" = $2)`,
		},
		{
			// Or nested inside and, which ToConditions reaches by recursion.
			name: "or_nested_under_and",
			filter: &ArticleFilter{And: []*ArticleFilter{
				{Or: []*ArticleFilter{
					{Title: &comparator.String{Eq: &alpha}},
					{Author: &comparator.String{Eq: &bravo}},
				}},
			}},
			want: `SELECT COUNT(*) FROM "public"."articles" WHERE ((("title" = $1) OR ("author" = $2)))`,
		},
		{
			// And nested inside or — the mirror of the case above.
			name: "and_nested_under_or",
			filter: &ArticleFilter{Or: []*ArticleFilter{
				{And: []*ArticleFilter{
					{Title: &comparator.String{Eq: &alpha}},
					{Author: &comparator.String{Eq: &bravo}},
				}},
			}},
			want: `SELECT COUNT(*) FROM "public"."articles" WHERE ((("title" = $1) AND ("author" = $2)))`,
		},
		{
			// A single member is a degenerate disjunction, not a syntax error.
			name: "single_member",
			filter: &ArticleFilter{Or: []*ArticleFilter{
				{Title: &comparator.String{Eq: &alpha}},
			}},
			want: `SELECT COUNT(*) FROM "public"."articles" WHERE (("title" = $1))`,
		},
		{
			// An empty or, and a member that compiles to nothing, must both
			// leave the WHERE off entirely. sql.Or of no conditions expands to
			// a literal "()", which the builder keeps because it is not the
			// empty string — so an unguarded group emits a syntax error.
			name:   "empty_or_emits_no_where",
			filter: &ArticleFilter{Or: []*ArticleFilter{}},
			want:   `SELECT COUNT(*) FROM "public"."articles"`,
		},
		{
			name:   "or_member_with_no_conditions_emits_no_where",
			filter: &ArticleFilter{Or: []*ArticleFilter{{}, nil}},
			want:   `SELECT COUNT(*) FROM "public"."articles"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := sql.BuildCount(dialect, table, tt.filter.ToConditions(dialect))
			if got != tt.want {
				t.Errorf("BuildCount:\n got: %s\nwant: %s", got, tt.want)
			}
		})
	}
}
