package tests

import (
	"context"
	"encoding/json"
	"testing"
	"uuid"
)

// Views on the tenanted GraphQL read surface — two-tenant isolation over the
// live API.
//
// Two features meet here. Tenant scoping for views: a view projecting the
// tenant column must carry a tenant predicate, or every row of every workspace
// comes back; the Go client injects it. The GraphQL view projection: the
// schema, resolver and gqlgen `models:` entry for a view are generated rather
// than hand-written.
//
// Shipping the projection without the scoping would publish a cross-tenant hole
// over HTTP. This file is the proof that the two together are safe:
// `workspace_note_summary` projects `workspace_id`, so §29.2.5 detection scopes
// it, and every one of the three read queries PRD §26.4 emits for a view is
// exercised as two different tenants against the same running handler.
//
// The predicate under test lives underneath the resolver — the API layer adds
// no tenancy logic of its own (§26.4 "Tenancy"), it inherits the scope by
// calling the scoped client method. A regression in either half surfaces here
// as another tenant's row in the response.

// seedWorkspaceNoteRow inserts one workspace_notes row directly through the
// test pool. Seeding over GraphQL would mean swapping the X-Workspace-ID
// header per row and would route the fixture through the very predicate under
// test; raw SQL keeps the seed honest and the failure output readable.
func seedWorkspaceNoteRow(t *testing.T, tenant uuid.UUID, body, kind, labels string, pinned int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := testPool.QueryRow(
		context.Background(),
		`INSERT INTO workspace_notes (workspace_id, body, pinned_order, kind, labels)
		 VALUES ($1, $2, $3, $4::workspace_note_kind_enum, $5::jsonb)
		 RETURNING id`,
		tenant, body, pinned, kind, labels,
	).Scan(&id); err != nil {
		t.Fatalf("seed workspace_note(%s, %q): %v", tenant, body, err)
	}
	return id
}

// seedNoteDocument attaches one `spv` document to a note so the view's
// document_count aggregate has something to count. It is what makes
// workspace_note_summary a genuine view rather than a passthrough — the
// column has no base table behind it.
func seedNoteDocument(t *testing.T, noteID uuid.UUID, name string) {
	t.Helper()
	if _, err := testPool.Exec(
		context.Background(),
		`INSERT INTO documents (entity_id, entity_type, name) VALUES ($1, 'spv', $2)`,
		noteID, name,
	); err != nil {
		t.Fatalf("seed document for note %s: %v", noteID, err)
	}
}

// viewSummaryNode is the shape every query below selects, so one decode struct
// covers the by-PK, connection and list shapes.
type viewSummaryNode struct {
	ID            string `json:"id"`
	WorkspaceID   string `json:"workspaceID"`
	Body          string `json:"body"`
	Kind          string `json:"kind"`
	DocumentCount int    `json:"documentCount"`
}

// seedTwoTenantNotes plants two notes for tenant A (one of them carrying a
// document) and one for tenant B, and returns A's first note id and B's note
// id. Every test in this file starts from this fixture so a leak shows up as a
// specific, nameable row rather than a count mismatch.
func seedTwoTenantNotes(t *testing.T) (aNoteID, bNoteID uuid.UUID) {
	t.Helper()
	truncateAll(t)

	aNoteID = seedWorkspaceNoteRow(t, fixedTenantA, "a-first", "draft", `{"team": "alpha"}`, 1)
	seedWorkspaceNoteRow(t, fixedTenantA, "a-second", "published", `{"team": "alpha"}`, 2)
	bNoteID = seedWorkspaceNoteRow(t, fixedTenantB, "b-only", "draft", `{"team": "bravo"}`, 1)

	seedNoteDocument(t, aNoteID, "a-first-doc")
	return aNoteID, bNoteID
}

// TestViewTenancy_ByPKIsScoped pins the by-PK query. This is the sharpest form
// of the cross-tenant hole: the caller already knows the primary key, so
// without the injected predicate the row comes back whoever asks. `Get`
// delegates to `GetMany`, which is where the tenant condition is appended, so a
// regression that dropped the predicate from only one of the two would still
// fail here.
func TestViewTenancy_ByPKIsScoped(t *testing.T) {
	aNoteID, bNoteID := seedTwoTenantNotes(t)
	url := newHeaderTenantedHandler(t)

	const query = `
		query ($id: UUID!) {
			workspaceNoteSummary(id: $id) {
				id workspaceID body kind documentCount
			}
		}
	`

	// Tenant A reading its own row: present, and carrying the aggregate.
	var own struct {
		WorkspaceNoteSummary *viewSummaryNode `json:"workspaceNoteSummary"`
	}
	resp := postGQLAt(t, url, query, map[string]any{"id": aNoteID.String()},
		map[string]string{"X-Workspace-ID": fixedTenantA.String()})
	if len(resp.Errors) != 0 {
		t.Fatalf("tenant A reading its own row: unexpected errors: %+v", resp.Errors)
	}
	decodeData(t, resp, &own)
	if own.WorkspaceNoteSummary == nil {
		t.Fatal("tenant A got null for its own row — the predicate is over-scoping")
	}
	if got, want := own.WorkspaceNoteSummary.Body, "a-first"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if got, want := own.WorkspaceNoteSummary.DocumentCount, 1; got != want {
		t.Errorf("documentCount = %d, want %d (the view's aggregate column)", got, want)
	}

	// Tenant A reading B's row by its exact primary key: null, not the row.
	var cross struct {
		WorkspaceNoteSummary *viewSummaryNode `json:"workspaceNoteSummary"`
	}
	resp = postGQLAt(t, url, query, map[string]any{"id": bNoteID.String()},
		map[string]string{"X-Workspace-ID": fixedTenantA.String()})
	if len(resp.Errors) != 0 {
		t.Fatalf("tenant A reading B's row: unexpected errors: %+v", resp.Errors)
	}
	decodeData(t, resp, &cross)
	if cross.WorkspaceNoteSummary != nil {
		t.Errorf("tenant A read tenant B's view row by PK: %+v — this is the cross-tenant view-read hole", *cross.WorkspaceNoteSummary)
	}
}

// TestViewTenancy_ConnectionIsScoped pins the cursor-paginated query, including
// its totalCount: the count and the page are two separate statements
// underneath, so a predicate applied to only one of them would report a count
// that spans every workspace while returning one workspace's rows.
func TestViewTenancy_ConnectionIsScoped(t *testing.T) {
	seedTwoTenantNotes(t)
	url := newHeaderTenantedHandler(t)

	const query = `
		query {
			workspaceNoteSummaries(first: 10) {
				totalCount
				edges { node { id workspaceID body kind documentCount } }
			}
		}
	`

	type connOut struct {
		WorkspaceNoteSummaries struct {
			TotalCount int `json:"totalCount"`
			Edges      []struct {
				Node viewSummaryNode `json:"node"`
			} `json:"edges"`
		} `json:"workspaceNoteSummaries"`
	}

	for _, tc := range []struct {
		tenant    uuid.UUID
		wantCount int
	}{
		{fixedTenantA, 2},
		{fixedTenantB, 1},
	} {
		resp := postGQLAt(t, url, query, nil,
			map[string]string{"X-Workspace-ID": tc.tenant.String()})
		if len(resp.Errors) != 0 {
			t.Fatalf("tenant %s: unexpected errors: %+v", tc.tenant, resp.Errors)
		}
		var out connOut
		if err := json.Unmarshal(resp.Data, &out); err != nil {
			t.Fatalf("tenant %s decode (raw=%s): %v", tc.tenant, resp.Data, err)
		}
		if got := out.WorkspaceNoteSummaries.TotalCount; got != tc.wantCount {
			t.Errorf("tenant %s: totalCount = %d, want %d (the count statement is unscoped?)", tc.tenant, got, tc.wantCount)
		}
		if got := len(out.WorkspaceNoteSummaries.Edges); got != tc.wantCount {
			t.Fatalf("tenant %s: edges = %d, want %d", tc.tenant, got, tc.wantCount)
		}
		for i, e := range out.WorkspaceNoteSummaries.Edges {
			if e.Node.WorkspaceID != tc.tenant.String() {
				t.Errorf("tenant %s: edge[%d] workspaceID = %s (body %q) — cross-tenant leak", tc.tenant, i, e.Node.WorkspaceID, e.Node.Body)
			}
		}
	}
}

// TestViewTenancy_ListIsScoped pins the offset/limit query, the one read a view
// always exposes regardless of @pk or cursor_keys (PRD §26.4). Its totalCount
// is the same two-statement shape the connection has.
func TestViewTenancy_ListIsScoped(t *testing.T) {
	seedTwoTenantNotes(t)
	url := newHeaderTenantedHandler(t)

	const query = `
		query {
			workspaceNoteSummaryList(sort: [{ field: PINNED_ORDER, direction: ASC }]) {
				totalCount
				hasMore
				items { id workspaceID body kind documentCount }
			}
		}
	`

	type listOut struct {
		WorkspaceNoteSummaryList struct {
			TotalCount int               `json:"totalCount"`
			HasMore    bool              `json:"hasMore"`
			Items      []viewSummaryNode `json:"items"`
		} `json:"workspaceNoteSummaryList"`
	}

	for _, tc := range []struct {
		tenant    uuid.UUID
		wantCount int
		wantFirst string
	}{
		{fixedTenantA, 2, "a-first"},
		{fixedTenantB, 1, "b-only"},
	} {
		resp := postGQLAt(t, url, query, nil,
			map[string]string{"X-Workspace-ID": tc.tenant.String()})
		if len(resp.Errors) != 0 {
			t.Fatalf("tenant %s: unexpected errors: %+v", tc.tenant, resp.Errors)
		}
		var out listOut
		if err := json.Unmarshal(resp.Data, &out); err != nil {
			t.Fatalf("tenant %s decode (raw=%s): %v", tc.tenant, resp.Data, err)
		}
		if got := out.WorkspaceNoteSummaryList.TotalCount; got != tc.wantCount {
			t.Errorf("tenant %s: totalCount = %d, want %d", tc.tenant, got, tc.wantCount)
		}
		if got := len(out.WorkspaceNoteSummaryList.Items); got != tc.wantCount {
			t.Fatalf("tenant %s: items = %d, want %d", tc.tenant, got, tc.wantCount)
		}
		if got := out.WorkspaceNoteSummaryList.Items[0].Body; got != tc.wantFirst {
			t.Errorf("tenant %s: items[0].body = %q, want %q (sort reached the view?)", tc.tenant, got, tc.wantFirst)
		}
		for i, item := range out.WorkspaceNoteSummaryList.Items {
			if item.WorkspaceID != tc.tenant.String() {
				t.Errorf("tenant %s: items[%d] workspaceID = %s (body %q) — cross-tenant leak", tc.tenant, i, item.WorkspaceID, item.Body)
			}
		}
	}
}

// TestViewTenancy_MissingHeaderIsUnauthenticated pins the fail-closed branch on
// a view. Under `tenancy.required: true` a request that resolves no tenant must
// not fall through to an unscoped read — the query that would leak every
// workspace at once is the one with no tenant on ctx at all.
func TestViewTenancy_MissingHeaderIsUnauthenticated(t *testing.T) {
	aNoteID, _ := seedTwoTenantNotes(t)
	url := newHeaderTenantedHandler(t)

	for _, q := range []struct {
		name  string
		query string
		vars  map[string]any
	}{
		{name: "list", query: `query { workspaceNoteSummaryList { totalCount items { id } } }`},
		{name: "connection", query: `query { workspaceNoteSummaries(first: 10) { totalCount edges { node { id } } } }`},
		{
			// The by-PK query belongs here for the same reason it leads this
			// file: the caller already holds the key, so a fall-through to an
			// unscoped read hands over the row outright rather than merely
			// widening a list.
			name:  "by pk",
			query: `query ($id: UUID!) { workspaceNoteSummary(id: $id) { id workspaceID } }`,
			vars:  map[string]any{"id": aNoteID.String()},
		},
	} {
		t.Run(q.name, func(t *testing.T) {
			resp := postGQLAt(t, url, q.query, q.vars, nil) // no X-Workspace-ID header
			if len(resp.Errors) == 0 {
				t.Fatalf("no tenant on ctx returned data instead of an error: %s", resp.Data)
			}
			if got, want := extensionsCode(resp.Errors[0]), "UNAUTHENTICATED"; got != want {
				t.Errorf("extensions.code = %q, want %q (errors: %+v)", got, want, resp.Errors)
			}
		})
	}
}
