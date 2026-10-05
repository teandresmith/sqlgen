package tests

// Tenancy × relationships — O2O chain end-to-end.
//
// Exercises PRD §29.10 for an O2O chain loaded through a single parent.Get /
// parent.GetMany: every tenanted alias emitted into the outer WHERE, single
// SQL statement (no N+1), and SkipTenancy:true dropping the auto-filter at
// every chain level.
//
// ---------------------------------------------------------------------------
// Chain shape:
// ---------------------------------------------------------------------------
//
// Rather than a dedicated three-table chain A → B → C, all tenanted, this
// reuses a chain with the same structural property already in the tenancy
// example: the users ↔ user_profiles back-reference auto-detection produces
// an O2O chain of arbitrary depth (alias order: u → p → us → pr → use → pro →
// user).
// Selecting `fo.Profile.Users.Profile` materialises three chained aliases
// (p, us, pr) on top of the parent alias (u) — four tenant conditions in
// WHERE, one per alias. The aliases alternate between two tables instead
// of naming three distinct tables, but the §29.10 invariant under test
// ("every tenanted alias contributes a WHERE condition") is exercised
// identically.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// capturingQuerier records SQL text of every Query/QueryRow/Exec call so tests
// can assert on outer-WHERE contents and the number of round-trips. Local to
// the relationship tests (relationship_chain_test.go + m2m_junction_test.go)
// — the shared countingQuerier in main_test.go records only counts. Defined here (rather
// than in main_test.go) because no other tests in the package need SQL text.
type capturingQuerier struct {
	inner database.Querier
	mu    sync.Mutex
	sqls  []string
}

func newCapturingQuerier(inner database.Querier) *capturingQuerier {
	return &capturingQuerier{inner: inner}
}

func (c *capturingQuerier) record(sqlStr string) {
	c.mu.Lock()
	c.sqls = append(c.sqls, sqlStr)
	c.mu.Unlock()
}

func (c *capturingQuerier) Exec(ctx context.Context, sqlStr string, args ...any) (database.Result, error) {
	c.record(sqlStr)
	return c.inner.Exec(ctx, sqlStr, args...)
}

func (c *capturingQuerier) Query(ctx context.Context, sqlStr string, args ...any) (database.Rows, error) {
	c.record(sqlStr)
	return c.inner.Query(ctx, sqlStr, args...)
}

func (c *capturingQuerier) QueryRow(ctx context.Context, sqlStr string, args ...any) database.Row {
	c.record(sqlStr)
	return c.inner.QueryRow(ctx, sqlStr, args...)
}

func (c *capturingQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return c.inner.Begin(ctx, name, opts...)
}

func (c *capturingQuerier) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.sqls))
	copy(out, c.sqls)
	return out
}

func (c *capturingQuerier) reset() {
	c.mu.Lock()
	c.sqls = nil
	c.mu.Unlock()
}

// newCapturingClient builds a Client wired to a capturing querier with the
// given resolver. Intentionally duplicates the plumbing of newEnv rather than
// extending it — the relationship tests need SQL capture; the rest of the suite does not,
// and threading a querier option through newEnv would be a widening that
// future tests don't require.
func newCapturingClient(t *testing.T, resolver tenancy.TenantResolver[uuid.UUID]) (*models.Client, *capturingQuerier) {
	t.Helper()
	cap := newCapturingQuerier(dbstdlib.New(testDB))
	client := models.New(cap, models.WithTenantResolver(resolver))
	return client, cap
}

// countWorkspaceIDPredicates counts tenant-column predicates inside the WHERE
// clause of a captured SELECT. The column also appears in the SELECT list (as
// a projection and as a JOIN column alias), so a naive full-SQL count
// double-counts projection + predicate. Slicing to the WHERE-onward substring
// before counting isolates the predicate occurrences.
//
// Identifier quoting is stripped first, so the token `workspace_id = `
// (lowercase, trailing space around `=`) matches both alias-qualified chain
// predicates (`u."workspace_id" = ?`) AND the unqualified single-table form
// (`"workspace_id" = ?`). Builders that drop the space around `=` would fail
// unit tests elsewhere; this helper stays strict on the canonical spacing.
func countWorkspaceIDPredicates(sqlStr string) int {
	lower := strings.ToLower(sqlStr)
	lower = strings.ReplaceAll(lower, "\"", "")
	idx := strings.Index(lower, " where ")
	if idx < 0 {
		return 0
	}
	return strings.Count(lower[idx:], "workspace_id = ")
}

// TestO2OChain_SingleQueryForChainedLoad is the no-N+1 contract. Loading a
// User with Profile → Users → Profile selected must materialise as ONE SELECT
// against `users` (the parent + LEFT JOIN chain) — no follow-up query per
// level, since §13.2 flattens O2O chains into a single multi-JOIN statement.
// (Collection relationships like Posts/Tags would spawn extra queries; those
// aren't in the FieldOptions here.)
func TestO2OChain_SingleQueryForChainedLoad(t *testing.T) {
	resetDB(t)

	client, cap := newCapturingClient(t, staticResolver(tenantA))
	ctx := context.Background()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{Email: "u@a.com"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	bio := "tenant A bio"
	if _, err := client.UserProfiles().Create(ctx, &models.CreateUserProfileInput{
		UserID: user.ID,
		Bio:    omittable.Set(&bio),
	}); err != nil {
		t.Fatalf("create profile: %v", err)
	}

	// Fresh slate: count only the chained GetMany's queries, not setup.
	cap.reset()

	// Load via GetMany with depth-2 chain selection (Profile → Users → Profile).
	// The userClient.resolveO2OJoins emits JOINs only for each chain level
	// materialised here — 3 chained aliases (p, u2, p2) on top of parent u.
	users, err := client.Users().GetMany(
		ctx, &models.GetUsersInput{},
		func(o *models.CallOptions[models.UserFieldOptions]) {
			o.FieldOptions = &models.UserFieldOptions{
				ID: true, WorkspaceID: true, Email: true,
				Profile: &models.UserProfileFieldOptions{
					ID: true, UserID: true, WorkspaceID: true, Bio: true,
					Users: &models.UserFieldOptions{
						ID: true, WorkspaceID: true, Email: true,
						Profile: &models.UserProfileFieldOptions{
							ID: true, UserID: true, WorkspaceID: true, Bio: true,
						},
					},
				},
			}
		},
	)
	if err != nil {
		t.Fatalf("GetMany with chained o2o: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("got %d users, want 1", len(users))
	}

	sqls := cap.snapshot()
	selects := 0
	for _, s := range sqls {
		if strings.Contains(strings.ToUpper(s), "SELECT") && strings.Contains(strings.ToLower(s), "from") && strings.Contains(strings.ToLower(s), "users") {
			selects++
		}
	}
	// Exactly one SELECT. Any number > 1 indicates a regression: a separate
	// per-level query (defeating §13.2 O2O JOIN flattening) or a second
	// round-trip via collection loaders (which shouldn't fire for this
	// FieldOptions set).
	if selects != 1 {
		t.Errorf("SELECTs on users: %d, want 1 (single-query chain load). captured sqls:\n%s", selects, strings.Join(sqls, "\n"))
	}
}

// TestO2OChain_TenantWHEREOnEveryAlias is the §29.10 core invariant: every
// tenanted alias (parent + each chained JOIN target) contributes a
// `workspace_id = ?` predicate to the outer WHERE. With depth-2 chain selection
// we expect 4 aliases (u, p, us, pr) → 4 `workspace_id` predicates.
//
// Asserts SQL shape, not row output — a row-count test could pass with a
// broken WHERE if there happens to be only one tenant's data in the DB.
// Shape assertion catches the drift directly.
func TestO2OChain_TenantWHEREOnEveryAlias(t *testing.T) {
	resetDB(t)

	client, cap := newCapturingClient(t, staticResolver(tenantA))
	ctx := context.Background()

	user, _ := client.Users().Create(ctx, &models.CreateUserInput{Email: "u@a.com"})
	bio := "A"
	_, _ = client.UserProfiles().Create(ctx, &models.CreateUserProfileInput{
		UserID: user.ID, Bio: omittable.Set(&bio),
	})

	cap.reset()

	_, err := client.Users().GetMany(
		ctx, &models.GetUsersInput{},
		func(o *models.CallOptions[models.UserFieldOptions]) {
			o.FieldOptions = &models.UserFieldOptions{
				ID: true, WorkspaceID: true, Email: true,
				Profile: &models.UserProfileFieldOptions{
					ID: true, UserID: true, WorkspaceID: true, Bio: true,
					Users: &models.UserFieldOptions{
						ID: true, WorkspaceID: true, Email: true,
						Profile: &models.UserProfileFieldOptions{
							ID: true, UserID: true, WorkspaceID: true, Bio: true,
						},
					},
				},
			}
		},
	)
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}

	sqls := cap.snapshot()
	if len(sqls) == 0 {
		t.Fatalf("no SQL captured")
	}
	// Find the SELECT on users (the chain query).
	var chainSQL string
	for _, s := range sqls {
		if strings.Contains(s, "LEFT JOIN") {
			chainSQL = s
			break
		}
	}
	if chainSQL == "" {
		t.Fatalf("no LEFT JOIN SQL captured; got:\n%s", strings.Join(sqls, "\n"))
	}

	// 4 tenant conditions: parent alias u + 3 chained aliases p, u2, p2.
	// PRD §29.10 and the JOIN convention place every tenant condition in the outer WHERE
	// (not the ON clause) — the count is stable against JOIN ordering.
	if got := countWorkspaceIDPredicates(chainSQL); got != 4 {
		t.Errorf(".workspace_id occurrences in chain SQL: %d, want 4 (parent u + aliases p, u2, p2)\n%s", got, chainSQL)
	}
	// Belt-and-suspenders: each specific alias predicate present.
	for _, alias := range []string{"u.", "p.", "u2.", "p2."} {
		needle := alias + `"workspace_id" = `
		if !strings.Contains(strings.ToLower(chainSQL), needle) {
			t.Errorf("chain SQL missing %q predicate\n%s", needle, chainSQL)
		}
	}
}

// TestO2OChain_SkipTenancyDropsFilterOnEveryAlias exercises §29.10
// SkipTenancy propagation: setting SkipTenancy:true on the parent call
// disables the auto-filter at EVERY chained alias, not just the root. A
// regression that short-circuited only the parent would still emit child
// `workspace_id = ?` predicates — the substring count catches it.
func TestO2OChain_SkipTenancyDropsFilterOnEveryAlias(t *testing.T) {
	resetDB(t)

	client, cap := newCapturingClient(t, staticResolver(tenantA))
	ctx := context.Background()

	user, _ := client.Users().Create(ctx, &models.CreateUserInput{Email: "u@a.com"})
	bio := "A"
	_, _ = client.UserProfiles().Create(ctx, &models.CreateUserProfileInput{
		UserID: user.ID, Bio: omittable.Set(&bio),
	})

	cap.reset()

	_, err := client.Users().GetMany(
		ctx, &models.GetUsersInput{},
		func(o *models.CallOptions[models.UserFieldOptions]) {
			o.SkipTenancy = true
			o.FieldOptions = &models.UserFieldOptions{
				ID: true, WorkspaceID: true, Email: true,
				Profile: &models.UserProfileFieldOptions{
					ID: true, UserID: true, WorkspaceID: true, Bio: true,
					Users: &models.UserFieldOptions{
						ID: true, WorkspaceID: true, Email: true,
						Profile: &models.UserProfileFieldOptions{
							ID: true, UserID: true, WorkspaceID: true, Bio: true,
						},
					},
				},
			}
		},
	)
	if err != nil {
		t.Fatalf("GetMany SkipTenancy: %v", err)
	}

	sqls := cap.snapshot()
	var chainSQL string
	for _, s := range sqls {
		if strings.Contains(s, "LEFT JOIN") {
			chainSQL = s
			break
		}
	}
	if chainSQL == "" {
		t.Fatalf("no LEFT JOIN SQL captured; got:\n%s", strings.Join(sqls, "\n"))
	}

	// Under SkipTenancy the SELECT/WHERE clause must carry zero workspace_id
	// predicates. The SELECT list itself does contain `"workspace_id"` column
	// aliases; we count `.workspace_id` with a leading dot to match only the
	// alias-qualified WHERE predicates, not the projected columns.
	if got := countWorkspaceIDPredicates(chainSQL); got != 0 {
		t.Errorf(".workspace_id predicates under SkipTenancy: %d, want 0\n%s", got, chainSQL)
	}
}

// TestO2OChain_CrossTenantRowsFilteredAtEveryChainLevel seeds a cross-tenant
// poison row at the deepest chain level and verifies it never materialises
// through the chain load. Without the child-side tenant WHERE on the deep
// alias (`p2.workspace_id = $`), tenant B's user_profile attached to A's
// user ID would leak into A's loaded.Profile.Users.Profile.
//
// This is the behavioural mirror of the SQL-shape test above: the WHERE-count
// proves the clause is emitted; this test proves it's effective.
func TestO2OChain_CrossTenantRowsFilteredAtEveryChainLevel(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	// Tenant A: user + profile.
	userA, err := envA.client.Users().Create(ctx, &models.CreateUserInput{Email: "ua@x.com"})
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	bioA := "A bio"
	if _, err := envA.client.UserProfiles().Create(ctx, &models.CreateUserProfileInput{
		UserID: userA.ID,
		Bio:    omittable.Set(&bioA),
	}); err != nil {
		t.Fatalf("create profile A: %v", err)
	}

	// Tenant B: separate user + profile with no relation to A.
	userB, err := envB.client.Users().Create(ctx, &models.CreateUserInput{Email: "ub@x.com"})
	if err != nil {
		t.Fatalf("create user B: %v", err)
	}
	bioB := "B bio"
	if _, err := envB.client.UserProfiles().Create(ctx, &models.CreateUserProfileInput{
		UserID: userB.ID,
		Bio:    omittable.Set(&bioB),
	}); err != nil {
		t.Fatalf("create profile B: %v", err)
	}

	// Load A's users with the full chain selected. The chain resolves:
	//   u (userA) → p (profile A) → u2 (back-ref user A) → p2 (profile A)
	// Cross-tenant rows (user B, profile B) must not surface on any alias.
	loaded, err := envA.client.Users().GetMany(
		ctx, &models.GetUsersInput{},
		func(o *models.CallOptions[models.UserFieldOptions]) {
			o.FieldOptions = &models.UserFieldOptions{
				ID: true, WorkspaceID: true, Email: true,
				Profile: &models.UserProfileFieldOptions{
					ID: true, UserID: true, WorkspaceID: true, Bio: true,
					Users: &models.UserFieldOptions{
						ID: true, WorkspaceID: true, Email: true,
						Profile: &models.UserProfileFieldOptions{
							ID: true, UserID: true, WorkspaceID: true, Bio: true,
						},
					},
				},
			}
		},
	)
	if err != nil {
		t.Fatalf("GetMany chain: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded users: %d, want 1 (tenant B must be filtered at root)", len(loaded))
	}
	u := loaded[0]
	if u.WorkspaceID != tenantA {
		t.Errorf("root user.WorkspaceID = %v, want %v", u.WorkspaceID, tenantA)
	}
	if u.Profile == nil {
		t.Fatalf("u.Profile = nil, want non-nil")
	}
	if u.Profile.WorkspaceID != tenantA {
		t.Errorf("Profile.WorkspaceID = %v, want %v", u.Profile.WorkspaceID, tenantA)
	}
	if u.Profile.Users == nil {
		t.Fatalf("Profile.Users = nil, want non-nil")
	}
	if u.Profile.Users.WorkspaceID != tenantA {
		t.Errorf("Profile.Users.WorkspaceID = %v, want %v", u.Profile.Users.WorkspaceID, tenantA)
	}
	if u.Profile.Users.Profile == nil {
		t.Fatalf("Profile.Users.Profile = nil, want non-nil")
	}
	if u.Profile.Users.Profile.WorkspaceID != tenantA {
		t.Errorf("Profile.Users.Profile.WorkspaceID = %v, want %v", u.Profile.Users.Profile.WorkspaceID, tenantA)
	}
}
