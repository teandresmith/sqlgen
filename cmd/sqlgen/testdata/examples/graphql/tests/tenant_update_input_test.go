package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The tenant column on the GraphQL update surface (PRD §29.4.2).
//
// `workspace_notes` is the repo's non-PK-tenant fixture: its workspace_id is
// an ordinary column, not a PK member. Under tenancy.required:true the runtime
// excludes that column from the UPDATE SET clause and verify-matches it
// instead, so a caller-supplied value could only ever return FORBIDDEN or be
// silently discarded. The generated schema therefore does not offer it.
//
// The create input is deliberately unaffected — there the value feeds a real
// verify-match.

// TestTenantUpdateInput_FieldIsNotOffered pins the schema contract from the
// client's side: sending the tenant column on an update is a validation
// error, not a silently-ignored field or a 403.
func TestTenantUpdateInput_FieldIsNotOffered(t *testing.T) {
	truncateAll(t)
	url := newHeaderTenantedHandler(t)
	headers := map[string]string{"X-Workspace-ID": fixedTenantA.String()}

	id := seedWorkspaceNote(t, url, headers, "original body")

	// A schema-validation failure returns HTTP 422, so this one request can't
	// go through postGQLAt (which fatals on any non-200).
	resp := postGQLAnyStatus(t, url, `
		mutation ($id: UUID!, $ws: UUID!) {
			updateWorkspaceNote(id: $id, input: { workspaceID: $ws, body: "moved" }) {
				id
			}
		}
	`, map[string]any{"id": id, "ws": fixedTenantB.String()}, headers)

	if len(resp.Errors) == 0 {
		t.Fatal("updateWorkspaceNote accepted a workspaceID field; the update input must not offer the tenant column")
	}
	// gqlgen rejects it at validation time — before any resolver runs — so
	// this is an unknown-field error, not the FORBIDDEN the old shape gave.
	if msg := resp.Errors[0].Message; !strings.Contains(msg, "workspaceID") ||
		!strings.Contains(msg, "not defined by type") {
		t.Errorf("error = %q, want an unknown-field validation error naming workspaceID", msg)
	}
	if code := extensionsCode(resp.Errors[0]); code != "GRAPHQL_VALIDATION_FAILED" {
		t.Errorf("extensions.code = %q, want GRAPHQL_VALIDATION_FAILED", code)
	}
	if code := extensionsCode(resp.Errors[0]); code == "FORBIDDEN" {
		t.Error("got FORBIDDEN — the tenant field is still reaching the resolver")
	}
}

// TestTenantUpdateInput_NormalUpdateStillWorks guards against the fix
// over-reaching: every other column on a tenanted table stays updatable, and
// the row keeps its tenant.
func TestTenantUpdateInput_NormalUpdateStillWorks(t *testing.T) {
	truncateAll(t)
	url := newHeaderTenantedHandler(t)
	headers := map[string]string{"X-Workspace-ID": fixedTenantA.String()}

	id := seedWorkspaceNote(t, url, headers, "original body")

	var out struct {
		UpdateWorkspaceNote struct {
			ID          string `json:"id"`
			Body        string `json:"body"`
			PinnedOrder int    `json:"pinnedOrder"`
		} `json:"updateWorkspaceNote"`
	}
	resp := postGQLAt(t, url, `
		mutation ($id: UUID!) {
			updateWorkspaceNote(id: $id, input: { body: "edited", pinnedOrder_inc: 2 }) {
				id body pinnedOrder
			}
		}
	`, map[string]any{"id": id}, headers)
	if len(resp.Errors) > 0 {
		t.Fatalf("updateWorkspaceNote: graphql errors: %+v", resp.Errors)
	}
	decodeData(t, resp, &out)

	if got := out.UpdateWorkspaceNote.Body; got != "edited" {
		t.Errorf("body = %q, want %q", got, "edited")
	}
	if got := out.UpdateWorkspaceNote.PinnedOrder; got != 2 {
		t.Errorf("pinnedOrder = %d, want 2 (the _inc operator on a genuine numeric column must survive)", got)
	}

	// The row's tenant is untouched — it was never settable through the API.
	var tenant string
	if err := testPool.QueryRow(context.Background(),
		`SELECT workspace_id::text FROM workspace_notes WHERE id = $1`, id).Scan(&tenant); err != nil {
		t.Fatalf("read back workspace_id: %v", err)
	}
	if tenant != fixedTenantA.String() {
		t.Errorf("workspace_id = %s, want %s", tenant, fixedTenantA)
	}
}

// TestTenantUpdateInput_CreateStillCarriesTenant pins the deliberate
// asymmetry: only the update input changed.
func TestTenantUpdateInput_CreateStillCarriesTenant(t *testing.T) {
	truncateAll(t)
	url := newHeaderTenantedHandler(t)

	// A mismatched tenant on CREATE still verify-matches to FORBIDDEN.
	resp := postGQLAt(t, url, `
		mutation ($ws: UUID!, $createdAt: Time!) {
			createWorkspaceNote(input: {
				workspaceID: $ws, body: "x", pinnedOrder: 0,
				kind: DRAFT, labels: {}, createdAt: $createdAt
			}) { id }
		}
	`, map[string]any{
		"ws":        fixedTenantB.String(),
		"createdAt": fixedTimestamp,
	}, map[string]string{"X-Workspace-ID": fixedTenantA.String()})

	if len(resp.Errors) == 0 {
		t.Fatal("createWorkspaceNote with a mismatched tenant: want an error, got none")
	}
	if code := extensionsCode(resp.Errors[0]); code != "FORBIDDEN" {
		t.Errorf("extensions.code = %q, want FORBIDDEN (errors: %+v)", code, resp.Errors)
	}
}

func seedWorkspaceNote(t *testing.T, url string, headers map[string]string, body string) string {
	t.Helper()
	var out struct {
		CreateWorkspaceNote struct {
			ID string `json:"id"`
		} `json:"createWorkspaceNote"`
	}
	resp := postGQLAt(t, url, `
		mutation ($ws: UUID!, $body: String!, $createdAt: Time!) {
			createWorkspaceNote(input: {
				workspaceID: $ws, body: $body, pinnedOrder: 0,
				kind: DRAFT, labels: {}, createdAt: $createdAt
			}) { id }
		}
	`, map[string]any{
		"ws":        fixedTenantA.String(),
		"body":      body,
		"createdAt": fixedTimestamp,
	}, headers)
	if len(resp.Errors) > 0 {
		t.Fatalf("seedWorkspaceNote: graphql errors: %+v", resp.Errors)
	}
	decodeData(t, resp, &out)
	if out.CreateWorkspaceNote.ID == "" {
		t.Fatal("seedWorkspaceNote: empty ID")
	}
	return out.CreateWorkspaceNote.ID
}

// decodeData unmarshals a successful response's data payload. postGQLAt (unlike
// gqlExecData) returns the raw envelope so callers can assert on errors, so
// the success path needs its own decode step.
func decodeData(t *testing.T, resp gqlResponse, out any) {
	t.Helper()
	if err := json.Unmarshal(resp.Data, out); err != nil {
		t.Fatalf("decode data (raw=%s): %v", resp.Data, err)
	}
}

// postGQLAnyStatus is postGQLAt without the 200-only assertion. A GraphQL
// schema-validation failure is served as HTTP 422 with a normal error
// envelope, which is exactly the response under test here.
func postGQLAnyStatus(t *testing.T, url, query string, vars map[string]any, headers map[string]string) gqlResponse {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var out gqlResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode response (raw=%s): %v", raw, err)
	}
	return out
}
