package models

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"uuid"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
	"github.com/teandresmith/sqlgen/tenancy"
)

// filterOptions and usesRelationshipFilter are emitted by the templates
// (table/client.go.tmpl, shared/_filter.tmpl), so nothing inside the gen module
// runs them — the tests there assert the rendered text, not the behavior. These
// tests supply the behavior half from inside the generated package, following
// the union_columns_test.go precedent (postgres example).
//
// What they pin: a read only resolves a tenant for its filter when that filter
// actually sets a relationship member. The FilterOption set exists solely to
// carry the target's tenant predicate into a correlated EXISTS (PRD §11.1
// invariant 1), and only the EXISTS helpers read it — so a filter with no
// relationship member compiles to the same SQL either way.
//
// The gate is load-bearing rather than an optimization. A fail-closed resolver
// (§29.3.1) reports a missing tenant as an error, and filterOptions propagates
// it. Resolving unconditionally therefore gave that resolver a veto over every
// filter-accepting read — including reads of a shared table (§29.2.3 rule 3),
// whose own WHERE is never tenant-scoped, and which an application legitimately
// performs before any tenant is selected: registration, login, and the
// membership lookup that establishes the tenant in the first place.

// failClosedResolver reports a missing tenant the way §29.3.1 documents, and
// counts its invocations so a test can assert the resolve did not happen at
// all rather than merely that it was tolerated.
func failClosedResolver(calls *int) tenancy.TenantResolver[uuid.UUID] {
	return func(context.Context) (uuid.UUID, error) {
		*calls++
		return uuid.Nil(), tenancy.ErrMissing
	}
}

func TestFilterOptionsResolvesOnlyForRelationshipMembers(t *testing.T) {
	email := "someone@example.com"

	tests := []struct {
		name        string
		filter      *UserFilter
		wantResolve bool
	}{
		{
			name:        "nil_filter",
			filter:      nil,
			wantResolve: false,
		},
		{
			name:        "no_members",
			filter:      &UserFilter{},
			wantResolve: false,
		},
		{
			name:        "column_comparator_only",
			filter:      &UserFilter{Email: &comparator.String{Eq: &email}},
			wantResolve: false,
		},
		{
			name:        "empty_and_or",
			filter:      &UserFilter{And: []*UserFilter{{}}, Or: []*UserFilter{{}}},
			wantResolve: false,
		},
		{
			name:        "direct_member",
			filter:      &UserFilter{Posts: &PostFilter{}},
			wantResolve: true,
		},
		{
			name:        "second_member",
			filter:      &UserFilter{Articles: &ArticleFilter{}},
			wantResolve: true,
		},
		{
			name:        "member_nested_in_and",
			filter:      &UserFilter{And: []*UserFilter{{Posts: &PostFilter{}}}},
			wantResolve: true,
		},
		{
			name:        "member_nested_in_or",
			filter:      &UserFilter{Or: []*UserFilter{{}, {Articles: &ArticleFilter{}}}},
			wantResolve: true,
		},
		{
			name:        "member_nested_two_levels_deep",
			filter:      &UserFilter{And: []*UserFilter{{Or: []*UserFilter{{Posts: &PostFilter{}}}}}},
			wantResolve: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			c := &userClient{tenantResolver: failClosedResolver(&calls)}

			opts, err := c.filterOptions(context.Background(), tt.filter, false, nil)

			if got := calls > 0; got != tt.wantResolve {
				t.Errorf("resolver invoked = %v (%d calls), want %v", got, calls, tt.wantResolve)
			}
			if !tt.wantResolve {
				if err != nil {
					t.Errorf("filterOptions err = %v, want nil — the option set is unread, so a missing tenant is not this call's concern", err)
				}
				if opts != nil {
					t.Errorf("filterOptions opts = %v, want nil", opts)
				}
				return
			}
			if !errors.Is(err, tenancy.ErrMissing) {
				t.Errorf("filterOptions err = %v, want tenancy.ErrMissing — a relationship member reaching a tenanted target must fail closed", err)
			}
		})
	}
}

// TestFilterOptionsSkipTenancyBypassesGate keeps SkipTenancy ahead of the
// member check: a deliberate cross-tenant read (§29.4.4) resolves nothing even
// when the filter does set a relationship member.
func TestFilterOptionsSkipTenancyBypassesGate(t *testing.T) {
	calls := 0
	c := &userClient{tenantResolver: failClosedResolver(&calls)}

	opts, err := c.filterOptions(context.Background(), &UserFilter{Posts: &PostFilter{}}, true, nil)
	if err != nil {
		t.Fatalf("filterOptions err = %v, want nil", err)
	}
	if opts != nil {
		t.Errorf("filterOptions opts = %v, want nil", opts)
	}
	if calls != 0 {
		t.Errorf("resolver invoked %d times under SkipTenancy, want 0", calls)
	}
}

// TestFilterOptionsExplicitTenantScopesSubquery covers the third resolution
// mode (§29.4.4): CallOptions.Tenant replaces the resolver, so a relationship
// member scopes its EXISTS without the resolver being consulted.
func TestFilterOptionsExplicitTenantScopesSubquery(t *testing.T) {
	calls := 0
	c := &userClient{tenantResolver: failClosedResolver(&calls)}
	explicit := uuid.New()

	opts, err := c.filterOptions(context.Background(), &UserFilter{Posts: &PostFilter{}}, false, &explicit)
	if err != nil {
		t.Fatalf("filterOptions err = %v, want nil", err)
	}
	if len(opts) != 1 {
		t.Fatalf("filterOptions returned %d options, want 1", len(opts))
	}
	if calls != 0 {
		t.Errorf("resolver invoked %d times with an explicit tenant, want 0", calls)
	}

	scope := resolveFilterScope(opts)
	if !scope.applyTenant {
		t.Error("explicit tenant did not reach the filter scope")
	}
	if scope.tenant != explicit {
		t.Errorf("filter scope tenant = %v, want %v", scope.tenant, explicit)
	}
}

// TestToConditionsIgnoresScopeWithoutRelationshipMember pins the premise the
// gate rests on. Only the EXISTS helpers read the FilterOption set, so a filter
// with no relationship member compiles to the same conditions whether or not a
// tenant reached it — which is what makes skipping the resolve a no-op on the
// emitted SQL rather than a loosening of §11.1 invariant 1.
func TestToConditionsIgnoresScopeWithoutRelationshipMember(t *testing.T) {
	dialect := sql.NewSQLiteDialect()
	email := "someone@example.com"
	other := "someone-else@example.com"

	filter := &UserFilter{
		Email: &comparator.String{Eq: &email},
		And:   []*UserFilter{{Email: &comparator.String{Neq: &other}}},
		Or:    []*UserFilter{{Email: &comparator.String{Eq: &other}}},
	}

	unscoped := filter.ToConditions(dialect)
	scoped := filter.ToConditions(dialect, WithFilterTenant(uuid.New()))

	if diff := cmp.Diff(unscoped, scoped); diff != "" {
		t.Errorf("tenant scope changed the conditions of a filter with no relationship member (-unscoped +scoped):\n%s", diff)
	}
}
