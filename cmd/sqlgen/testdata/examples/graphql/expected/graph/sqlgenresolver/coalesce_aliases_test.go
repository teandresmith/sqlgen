package sqlgenresolver

import (
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"
)

// Behavior coverage for the template-emitted coalesceAliases helper.
//
// The helper is generated as Go source from
// cmd/sqlgen/gen/templates/api/connection_walker.go.tmpl, so nothing in the gen
// module type-checks or runs it — api_walker_test.go asserts the emitted text,
// not what it does. This test lives inside the generated package so it
// exercises the emitted helper rather than a copy, following
// postgres/models/union_columns_test.go. It needs no drift guard: the
// golden trees pin the helper's text, so a body edit lands here on the next
// `make update-golden-e2e` and this test runs against the new body.

// field builds one CollectedField the way gqlgen's CollectFields would, with
// the given alias and sub-selections.
func field(name, alias string, sub ...string) graphql.CollectedField {
	sel := make(ast.SelectionSet, 0, len(sub))
	for _, s := range sub {
		sel = append(sel, &ast.Field{Name: s, Alias: s})
	}
	return graphql.CollectedField{
		Field:      &ast.Field{Name: name, Alias: alias},
		Selections: sel,
	}
}

// names flattens a coalesced entry's Sub to the field names it carries.
func names(sub []graphql.CollectedField) []string {
	out := make([]string, 0, len(sub))
	for _, f := range sub {
		out = append(out, f.Name)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCoalesceAliases(t *testing.T) {
	tests := []struct {
		name     string
		in       []graphql.CollectedField
		wantName []string
		wantSub  [][]string
	}{
		{
			name:     "empty input",
			in:       nil,
			wantName: []string{},
			wantSub:  [][]string{},
		},
		{
			name:     "distinct names pass through in order",
			in:       []graphql.CollectedField{field("email", "email"), field("name", "name")},
			wantName: []string{"email", "name"},
			wantSub:  [][]string{nil, nil},
		},
		{
			// The immune case the pre-fix walker already handled: aliased plain
			// columns collapse to one entry, and the column arms are idempotent
			// either way.
			name:     "two aliases of one leaf column",
			in:       []graphql.CollectedField{field("name", "p"), field("name", "q")},
			wantName: []string{"name"},
			wantSub:  [][]string{nil},
		},
		{
			// The defect: `x: orders { total } y: orders { createdAt }`. Before
			// the fix each entry drove its own assignment and the last won.
			name:     "two aliases of one relationship union their selections",
			in:       []graphql.CollectedField{field("orders", "x", "total"), field("orders", "y", "createdAt")},
			wantName: []string{"orders"},
			wantSub:  [][]string{{"total", "createdAt"}},
		},
		{
			name: "three aliases union in first-appearance order",
			in: []graphql.CollectedField{
				field("edges", "a", "node"),
				field("edges", "b", "cursor"),
				field("edges", "c", "__typename"),
			},
			wantName: []string{"edges"},
			wantSub:  [][]string{{"node", "cursor", "__typename"}},
		},
		{
			// Position is the first appearance of the name, not the last, so a
			// merge never reorders the entries around it.
			name: "merge keeps the first position",
			in: []graphql.CollectedField{
				field("orders", "x", "total"),
				field("email", "email"),
				field("orders", "y", "createdAt"),
			},
			wantName: []string{"orders", "email"},
			wantSub:  [][]string{{"total", "createdAt"}, nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := coalesceAliases(&graphql.OperationContext{}, tt.in)
			if len(got) != len(tt.wantName) {
				t.Fatalf("got %d entries, want %d", len(got), len(tt.wantName))
			}
			for i, g := range got {
				if g.Name != tt.wantName[i] {
					t.Errorf("entry %d name = %q, want %q", i, g.Name, tt.wantName[i])
				}
				if want := tt.wantSub[i]; !equalStrings(names(g.Sub), want) {
					t.Errorf("entry %d Sub = %v, want %v", i, names(g.Sub), want)
				}
			}
		})
	}
}

// TestCoalesceAliases_DoesNotAppendIntoGqlgensCache pins the reason the merge
// allocates instead of appending in place. The base of every merge is the slice
// gqlgen's CollectFields returned, which gqlgen stores in its per-request
// collectFieldsCache and hands to every other caller with the same key. When
// that slice has spare capacity — it does whenever two selections merge into
// one collected field, since CollectFields sizes the result by selection count
// — an in-place append writes through into memory gqlgen still owns.
func TestCoalesceAliases_DoesNotAppendIntoGqlgensCache(t *testing.T) {
	opCtx := &graphql.OperationContext{}

	// Two selections that CollectFields merges into one entry (same name AND
	// alias), so the returned slice is len 1, cap 2.
	base := field("orders", "x", "total", "total")
	collected := graphql.CollectFields(opCtx, base.Selections, nil)
	if len(collected) != 1 || cap(collected) < 2 {
		t.Fatalf("fixture is wrong: CollectFields returned len %d cap %d, want len 1 cap >= 2",
			len(collected), cap(collected))
	}
	// The slot past len is the one an in-place append would overwrite.
	spare := collected[:cap(collected)][1]

	got := coalesceAliases(opCtx, []graphql.CollectedField{base, field("orders", "y", "createdAt")})

	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if want := []string{"total", "createdAt"}; !equalStrings(names(got[0].Sub), want) {
		t.Fatalf("Sub = %v, want %v", names(got[0].Sub), want)
	}
	if after := collected[:cap(collected)][1]; after.Field != spare.Field {
		t.Errorf("coalesceAliases appended into gqlgen's cached slice: spare slot went from %+v to %+v",
			spare.Field, after.Field)
	}
}
