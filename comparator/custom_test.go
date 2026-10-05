package comparator_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

// TestCustom_CompoundConditionDoesNotEscapeItsTerm pins the Custom escape hatch
// against a compound clause breaking out of its term. A filter's conditions are
// AND-joined, and AND binds tighter than OR, so a custom condition whose
// top-level operator is OR used to break out of its own term: the caller's own
// Eq and the soft-delete default both landed on one side of the disjunction and
// stopped constraining the query. Built through sql.Raw, the clause is
// parenthesised and every sibling predicate survives.
//
// Covering one family is enough — Custom is appended verbatim by every family's
// Parse, and the rendering is the builder's, not the comparator's.
func TestCustom_CompoundConditionDoesNotEscapeItsTerm(t *testing.T) {
	tests := []struct {
		name     string
		custom   []sql.Condition
		want     string
		wantArgs []any
	}{
		{
			name:     "compound custom stays one term",
			custom:   []sql.Condition{sql.Raw("price > $ OR title LIKE $", 50.0, "c%")},
			want:     `SELECT "id" FROM "products" WHERE "title" = ? AND (price > ? OR title LIKE ?) AND "deleted_at" IS NULL`,
			wantArgs: []any{"widget", 50.0, "c%"},
		},
		{
			name:     "simple custom is unaffected in meaning",
			custom:   []sql.Condition{sql.Raw("price > $", 50.0)},
			want:     `SELECT "id" FROM "products" WHERE "title" = ? AND (price > ?) AND "deleted_at" IS NULL`,
			wantArgs: []any{"widget", 50.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := sql.NewSQLiteDialect()
			conds := comparator.String{
				Eq:     new("widget"),
				Custom: tt.custom,
			}.Parse(d.QuoteIdentifier("title"), d)
			conds = append(conds, sql.Where(d.QuoteIdentifier("deleted_at")).IsNull())

			got, gotArgs := sql.BuildSelect(d, sql.Table{Name: "products"}, sql.SelectOptions{
				Columns:    []string{"id"},
				Conditions: conds,
			})
			if got != tt.want {
				t.Errorf("BuildSelect() =\n  %q\nwant\n  %q", got, tt.want)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
