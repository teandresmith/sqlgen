package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"uuid"

	"github.com/99designs/gqlgen/graphql/handler"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
	graph "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver"
)

// PRD §29 — tenancy primitive. The example schema has exactly one tenanted
// table (`workspace_settings`); every other table lacks a `workspace_id`
// column and is auto-detected as shared per §29.2.3. We pin three contracts
// end-to-end through the live gqlgen handler + a header-driven middleware:
//
//   - Missing header → resolver returns `tenancy.ErrMissing` → mapErrorToGQL
//     produces `extensions.code = UNAUTHENTICATED` (PRD §26.5.5 + §29.3.1).
//   - PK.WorkspaceID mismatch against the resolved tenant → `tenancy.ErrMismatch`
//     → `FORBIDDEN`. Reachable from GraphQL because `workspace_settings`
//     carries the tenant column on the composite PK (PRD §29.7).
//   - Same-tenant connection query under resolver=A only returns A's rows;
//     B's rows are filtered at the SQL level by the auto-injected
//     `workspace_id = $N` predicate (PRD §29.4.1).
//
// This file doubles as a reference implementation of the consumer-side HTTP
// → tenancy bridge. sqlgen does NOT ship a built-in transport adapter for
// tenancy — PRD §26.11's `WithCallOptionsMiddleware` reads only
// `Cache-Control` / `X-Skip-Events` / `X-Skip-Hooks` headers. Wiring the
// active tenant onto ctx is the consumer's responsibility. The pattern below
// (private ctx key + http.Handler middleware + ctx-reading resolver) is the
// minimal viable bridge; production deployments would add steps marked
// "PRODUCTION:" in the helper comments.

// fixedTenantA / fixedTenantB are stable across subtests for readability of
// failure output. The values don't matter beyond being distinct UUIDs.
var (
	fixedTenantA = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	fixedTenantB = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
)

// tenantCtxKey is the private ctx key used by tenantHeaderMiddleware to
// stash the active tenant and by ctxTenantResolver to read it back. Using
// an unexported struct type is the stdlib-recommended pattern — it makes
// the key impossible to collide with from outside this package.
type tenantCtxKey struct{}

// tenantHeaderMiddleware extracts the active tenant from the `X-Workspace-ID`
// request header and stashes it on ctx under tenantCtxKey{}. It is the
// consumer-supplied bridge from HTTP transport to the tenancy primitive —
// sqlgen does NOT generate this; PRD §26.11's WithCallOptionsMiddleware
// covers only Cache-Control / X-Skip-Events / X-Skip-Hooks.
//
// Pass-through semantics: if the header is absent OR fails to parse as a
// UUID, the request continues with an unmodified ctx. The downstream
// resolver then surfaces `tenancy.ErrMissing`, which mapErrorToGQL stamps
// as `UNAUTHENTICATED`. We deliberately do NOT short-circuit with a 400
// here — fail-closed is the resolver's contract under `required: true`,
// and a single failure path keeps the surface coherent.
//
// PRODUCTION: a real deployment would (a) authenticate the request first
// (typically via JWT / session cookie) and (b) verify that the
// authenticated principal has membership in the requested workspace by
// consulting the app DB. The example skips both steps — it trusts the
// header verbatim — because the contract under test is the sqlgen
// resolver's behaviour, not authn/authz.
func tenantHeaderMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get("X-Workspace-ID")
		if raw == "" {
			next.ServeHTTP(w, r)
			return
		}
		tenant, err := uuid.Parse(raw)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), tenantCtxKey{}, tenant)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ctxTenantResolver returns a TenantResolver that reads the active tenant
// from ctx under tenantCtxKey{}. Returns `tenancy.ErrMissing` when the
// value is absent (or wrong type) — under §29.2 `required: true` this is
// the fail-closed branch that mapErrorToGQL surfaces as UNAUTHENTICATED.
//
// Mirrors the `ctxResolver()` pattern in the tenancy example
// (`cmd/sqlgen/testdata/examples/tenancy/tests/main_test.go`); the only
// delta is that there's no separate Go-client API here — the resolver is
// driven via the HTTP middleware above.
func ctxTenantResolver() tenancy.TenantResolver[uuid.UUID] {
	return func(ctx context.Context) (uuid.UUID, error) {
		v, ok := ctx.Value(tenantCtxKey{}).(uuid.UUID)
		if !ok {
			return uuid.Nil(), tenancy.ErrMissing
		}
		return v, nil
	}
}

// newHeaderTenantedHandler boots a parallel gqlgen handler stack and wraps
// the gqlgen handler with `tenantHeaderMiddleware(WithCallOptionsMiddleware(srv))`.
//
// Middleware ordering matters. tenantHeaderMiddleware MUST run BEFORE
// WithCallOptionsMiddleware so the tenant ctx is in place by the time
// sqlgen's middleware (and any downstream resolver invocation) executes.
// The wrapping is outer-to-inner: header → call-options → gqlgen handler.
//
// The standard `testServer` lacks a tenant resolver, so we keep tenancy-
// driven requests isolated to per-test handlers — that way no other test
// inadvertently exercises the resolver path.
func newHeaderTenantedHandler(t *testing.T) string {
	t.Helper()

	client := models.New(dbpgx.New(testPool), models.WithTenantResolver(ctxTenantResolver()))
	r := &graph.Resolver{
		Client: client,
		Q:      &sqlgenresolver.Q{Client: client},
		M:      &sqlgenresolver.M{Client: client},
	}
	es := graph.NewExecutableSchema(graph.Config{Resolvers: r})
	srv := handler.NewDefaultServer(es)
	httpsrv := httptest.NewServer(tenantHeaderMiddleware(sqlgenresolver.WithCallOptionsMiddleware(srv)))
	t.Cleanup(httpsrv.Close)
	return httpsrv.URL
}

// seedWorkspaceSetting inserts a row directly via the test pool. Going
// through the GraphQL surface for seeding would require swapping the
// active tenant header per row; raw SQL keeps the fixture readable in one
// place and bypasses the tenancy filter (the seeder isn't what's under
// test).
func seedWorkspaceSetting(t *testing.T, tenant uuid.UUID, key, value string) {
	t.Helper()
	if _, err := testPool.Exec(
		context.Background(),
		`INSERT INTO workspace_settings (workspace_id, key, value) VALUES ($1, $2, $3)`,
		tenant, key, value,
	); err != nil {
		t.Fatalf("seed workspace_setting(%s, %s): %v", tenant, key, err)
	}
}

// TestTenancy_MissingHeader_Unauthenticated drives a tenanted query
// (`workspaceSetting`) against the handler with no `X-Workspace-ID`
// header. The middleware passes ctx through unmodified; the resolver
// then returns `tenancy.ErrMissing`. The runtime short-circuits before
// the DB round-trip per §29.3.1; mapErrorToGQL stamps the response with
// `UNAUTHENTICATED`.
func TestTenancy_MissingHeader_Unauthenticated(t *testing.T) {
	truncateAll(t)

	url := newHeaderTenantedHandler(t)

	resp := postGQLAt(t, url, `
		query ($workspaceID: UUID!, $key: ID!) {
			workspaceSetting(workspaceID: $workspaceID, key: $key) {
				workspaceID
				key
			}
		}
	`, map[string]any{
		"workspaceID": fixedTenantA.String(),
		"key":         "anything",
	}, nil) // no headers → no tenant on ctx → ErrMissing

	if len(resp.Errors) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %+v", len(resp.Errors), resp.Errors)
	}
	if got, want := extensionsCode(resp.Errors[0]), "UNAUTHENTICATED"; got != want {
		t.Errorf("extensions.code = %q, want %q (errors: %+v)", got, want, resp.Errors)
	}
}

// TestTenancy_PKMismatch_Forbidden drives the §29.7 verify-match rule:
// the caller supplies workspaceID=B in the typed PK args, but the
// `X-Workspace-ID` header (and therefore the resolved tenant) is A. The
// runtime short-circuits with ErrMismatch before any DB round-trip;
// mapErrorToGQL stamps the response with `FORBIDDEN`.
func TestTenancy_PKMismatch_Forbidden(t *testing.T) {
	truncateAll(t)

	url := newHeaderTenantedHandler(t)

	resp := postGQLAt(t, url, `
		query ($workspaceID: UUID!, $key: ID!) {
			workspaceSetting(workspaceID: $workspaceID, key: $key) {
				workspaceID
				key
			}
		}
	`, map[string]any{
		"workspaceID": fixedTenantB.String(), // mismatched against header(A)
		"key":         "doesntmatter",
	}, map[string]string{
		"X-Workspace-ID": fixedTenantA.String(),
	})

	if len(resp.Errors) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %+v", len(resp.Errors), resp.Errors)
	}
	if got, want := extensionsCode(resp.Errors[0]), "FORBIDDEN"; got != want {
		t.Errorf("extensions.code = %q, want %q (errors: %+v)", got, want, resp.Errors)
	}
}

// TestTenancy_SameTenantFilter pins the read-path filter contract — under
// `required: true` with a non-zero resolver, the auto-injected
// `workspace_id = $N` predicate on GetMany scopes results to the resolved
// tenant. Rows seeded under a different tenant must NOT appear in the
// resolved tenant's list.
func TestTenancy_SameTenantFilter(t *testing.T) {
	truncateAll(t)

	// Two rows for A, one row for B. The list query under resolver=A must
	// see exactly A's two rows.
	seedWorkspaceSetting(t, fixedTenantA, "theme", "dark")
	seedWorkspaceSetting(t, fixedTenantA, "lang", "en")
	seedWorkspaceSetting(t, fixedTenantB, "theme", "light")

	url := newHeaderTenantedHandler(t)

	var out struct {
		WorkspaceSettings struct {
			TotalCount int `json:"totalCount"`
			Edges      []struct {
				Node struct {
					WorkspaceID string `json:"workspaceID"`
					Key         string `json:"key"`
					Value       string `json:"value"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"workspaceSettings"`
	}
	resp := postGQLAt(t, url, `
		query {
			workspaceSettings(first: 10) {
				totalCount
				edges { node { workspaceID key value } }
			}
		}
	`, nil, map[string]string{
		"X-Workspace-ID": fixedTenantA.String(),
	})
	if len(resp.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", resp.Errors)
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		t.Fatalf("decode data (raw=%s): %v", resp.Data, err)
	}

	if got, want := out.WorkspaceSettings.TotalCount, 2; got != want {
		t.Errorf("totalCount = %d, want %d (B's row leaked into A's list?)", got, want)
	}
	if got, want := len(out.WorkspaceSettings.Edges), 2; got != want {
		t.Fatalf("edges = %d, want %d", got, want)
	}
	for i, edge := range out.WorkspaceSettings.Edges {
		gotID, err := uuid.Parse(edge.Node.WorkspaceID)
		if err != nil {
			t.Fatalf("edge[%d] workspaceID parse: %v", i, err)
		}
		if gotID != fixedTenantA {
			t.Errorf("edge[%d] workspaceID = %s, want %s (cross-tenant leak)", i, gotID, fixedTenantA)
		}
	}
}

// TestTenancy_HeaderSwitch pins that the resolver is genuinely per-request:
// the same handler serves two requests, one with `X-Workspace-ID: A` and
// one with `X-Workspace-ID: B`, and each sees only its own workspace's
// rows. A handler that accidentally cached a single tenant value at boot
// (or wired tenancy via a closure over a fixed UUID) would silently pass
// the other tests but fail this one.
func TestTenancy_HeaderSwitch(t *testing.T) {
	truncateAll(t)

	seedWorkspaceSetting(t, fixedTenantA, "theme", "dark")
	seedWorkspaceSetting(t, fixedTenantA, "lang", "en")
	seedWorkspaceSetting(t, fixedTenantB, "theme", "light")

	url := newHeaderTenantedHandler(t)

	query := `
		query {
			workspaceSettings(first: 10) {
				totalCount
				edges { node { workspaceID key } }
			}
		}
	`

	type listOut struct {
		WorkspaceSettings struct {
			TotalCount int `json:"totalCount"`
			Edges      []struct {
				Node struct {
					WorkspaceID string `json:"workspaceID"`
					Key         string `json:"key"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"workspaceSettings"`
	}

	// Request 1 — tenant A. Expect exactly A's two rows.
	respA := postGQLAt(t, url, query, nil, map[string]string{
		"X-Workspace-ID": fixedTenantA.String(),
	})
	if len(respA.Errors) != 0 {
		t.Fatalf("tenant A: unexpected errors: %+v", respA.Errors)
	}
	var outA listOut
	if err := json.Unmarshal(respA.Data, &outA); err != nil {
		t.Fatalf("tenant A decode: %v", err)
	}
	if outA.WorkspaceSettings.TotalCount != 2 {
		t.Errorf("tenant A: totalCount=%d, want 2", outA.WorkspaceSettings.TotalCount)
	}
	for i, e := range outA.WorkspaceSettings.Edges {
		id, _ := uuid.Parse(e.Node.WorkspaceID)
		if id != fixedTenantA {
			t.Errorf("tenant A: edge[%d] workspaceID=%s, want %s", i, id, fixedTenantA)
		}
	}

	// Request 2 — tenant B against the SAME handler. Expect exactly B's
	// one row. If the resolver were stuck on A this would either return
	// A's rows or empty.
	respB := postGQLAt(t, url, query, nil, map[string]string{
		"X-Workspace-ID": fixedTenantB.String(),
	})
	if len(respB.Errors) != 0 {
		t.Fatalf("tenant B: unexpected errors: %+v", respB.Errors)
	}
	var outB listOut
	if err := json.Unmarshal(respB.Data, &outB); err != nil {
		t.Fatalf("tenant B decode: %v", err)
	}
	if outB.WorkspaceSettings.TotalCount != 1 {
		t.Errorf("tenant B: totalCount=%d, want 1", outB.WorkspaceSettings.TotalCount)
	}
	for i, e := range outB.WorkspaceSettings.Edges {
		id, _ := uuid.Parse(e.Node.WorkspaceID)
		if id != fixedTenantB {
			t.Errorf("tenant B: edge[%d] workspaceID=%s, want %s", i, id, fixedTenantB)
		}
	}
}
