package models

import (
	"context"
	"errors"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

// ToConditions is emitted by shared/_filter.tmpl, so nothing inside the gen
// module runs it — the tests there assert the rendered text. These supply the
// behavior half from inside the generated package, following the
// union_columns_test.go precedent in this same package.
//
// What they pin is the nil-receiver guard. The four *Where methods dereference
// their filter exactly once, in the ToConditions call whose result they test
// for emptiness, and they document a nil filter as the empty-filter case
// (PRD §10: "ErrEmptyFilter is returned when any *Where operation … is called
// with a nil or empty filter"). Without the guard that dereference panicked
// before the emptiness check could run, so the documented sentinel was
// unreachable and the hook framework's recover surfaced
// `sqlgen: panic in <op> …: invalid memory address` in its place. The guard is
// also what lets a nil entry inside And / Or be skipped rather than take down
// the conversion.

func TestToConditionsNilReceiver(t *testing.T) {
	dialect := sql.NewPostgresDialect()

	var f *ArticleFilter
	if got := f.ToConditions(dialect); got != nil {
		t.Errorf("nil filter produced %d conditions, want nil", len(got))
	}
}

func TestToConditionsSkipsNilCompositionEntries(t *testing.T) {
	dialect := sql.NewPostgresDialect()
	title := "Hello"

	tests := []struct {
		name   string
		filter *ArticleFilter
		want   int
	}{
		{
			name:   "nil_and_entry_alone",
			filter: &ArticleFilter{And: []*ArticleFilter{nil}},
			want:   0,
		},
		{
			name:   "nil_or_entry_alone",
			filter: &ArticleFilter{Or: []*ArticleFilter{nil}},
			want:   0,
		},
		{
			name:   "nil_entry_beside_a_real_one",
			filter: &ArticleFilter{And: []*ArticleFilter{nil, {Title: &comparator.String{Eq: &title}}}},
			want:   1,
		},
		{
			name:   "nil_entry_nested_two_levels_deep",
			filter: &ArticleFilter{And: []*ArticleFilter{{Or: []*ArticleFilter{nil}}}},
			want:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.ToConditions(dialect)
			if len(got) != tt.want {
				t.Errorf("ToConditions returned %d conditions, want %d: %+v", len(got), tt.want, got)
			}
		})
	}
}

// TestWhereMethodsRejectNilFilter drives the four bulk methods through the real
// client surface, which is where the contract is stated. A nil querier is
// enough: every one of them decides on the filter before it reaches the
// database.
func TestWhereMethodsRejectNilFilter(t *testing.T) {
	client := New(nil)
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "UpdateWhere",
			call: func() error {
				_, err := client.Articles().UpdateWhere(ctx, nil, &UpdateArticleInput{})
				return err
			},
		},
		{
			name: "SoftDeleteWhere",
			call: func() error {
				_, err := client.Articles().SoftDeleteWhere(ctx, nil)
				return err
			},
		},
		{
			name: "RestoreWhere",
			call: func() error {
				_, err := client.Articles().RestoreWhere(ctx, nil)
				return err
			},
		},
		{
			name: "HardDeleteWhere",
			call: func() error {
				return client.Articles().HardDeleteWhere(ctx, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if !errors.Is(err, ErrEmptyFilter) {
				t.Errorf("%s(nil) = %v, want ErrEmptyFilter", tt.name, err)
			}
		})
	}
}

// TestWhereMethodsRejectEmptyFilter keeps the nil guard from being the only
// path to ErrEmptyFilter: a non-nil filter that sets nothing must still be
// refused, so the guard is proven additive rather than a replacement.
func TestWhereMethodsRejectEmptyFilter(t *testing.T) {
	client := New(nil)

	if _, err := client.Articles().UpdateWhere(context.Background(), &ArticleFilter{}, &UpdateArticleInput{}); !errors.Is(err, ErrEmptyFilter) {
		t.Errorf("UpdateWhere(&ArticleFilter{}) = %v, want ErrEmptyFilter", err)
	}
}
