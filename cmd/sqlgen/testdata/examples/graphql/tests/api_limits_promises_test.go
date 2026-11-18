package tests

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver"
)

// twrWideWithRelationship selects n aliased scalar fields under the users
// connection plus one relationship field (User.categories) carrying a single
// scalar. The connection, edges and node wrappers are the three fields
// limWideQuery's calibration counts.
func twrWideWithRelationship(n int) string {
	var b strings.Builder
	b.WriteString("{ users(first: 1) { edges { node { ")
	for i := range n {
		fmt.Fprintf(&b, "f%d: id ", i)
	}
	b.WriteString("categories { id } } } } }")
	return b.String()
}

// TestQueryComplexity_RelationshipFieldAdds pins PRD §26.6 "each relationship
// field adds to query complexity". Two operations are compared at the
// limit: wrappers plus scalars that land exactly on max_complexity pass, and
// the same budget with one scalar traded for a relationship field selecting
// that scalar is rejected — so the relationship field itself counted.
func TestQueryComplexity_RelationshipFieldAdds(t *testing.T) {
	url := limServe(t)
	limit := sqlgenresolver.MaxQueryComplexity

	t.Run("scalars filling the budget exactly pass", func(t *testing.T) {
		// 3 wrapper fields + (limit-3) scalars = limit.
		resp := limPost(t, url, limWideQuery(limit-3))
		if len(resp.Errors) != 0 {
			t.Fatalf("errors = %+v, want none: the calibration query is at the limit", resp.Errors)
		}
	})

	t.Run("a relationship field over the same scalars is rejected", func(t *testing.T) {
		// 3 wrappers + (limit-4) scalars + categories + its id = limit + 1
		// when, and only when, the relationship field adds to the count.
		resp := limPost(t, url, twrWideWithRelationship(limit-4))
		if len(resp.Errors) == 0 {
			t.Fatalf("errors = none, want COMPLEXITY_LIMIT_EXCEEDED; data = %s", resp.Data)
		}
		if got := resp.Errors[0].Extensions["code"]; got != "COMPLEXITY_LIMIT_EXCEEDED" {
			t.Errorf("errors[0].extensions.code = %v, want COMPLEXITY_LIMIT_EXCEEDED (message %q)", got, resp.Errors[0].Message)
		}
	})
}

// twrLeafFields introspects typeName and returns every field whose type,
// unwrapped of NON_NULL, is a SCALAR or ENUM, with whether it is non-null.
func twrLeafFields(t *testing.T, typeName string) map[string]bool {
	t.Helper()
	var out struct {
		Type struct {
			Fields []struct {
				Name string `json:"name"`
				Type struct {
					Kind   string `json:"kind"`
					OfType *struct {
						Kind string `json:"kind"`
					} `json:"ofType"`
				} `json:"type"`
			} `json:"fields"`
		} `json:"__type"`
	}
	gqlExecData(t, `query ($name: String!) {
		__type(name: $name) { fields { name type { kind ofType { kind } } } }
	}`, map[string]any{"name": typeName}, &out)
	leaves := make(map[string]bool)
	for _, f := range out.Type.Fields {
		kind, nonNull := f.Type.Kind, false
		if kind == "NON_NULL" && f.Type.OfType != nil {
			kind, nonNull = f.Type.OfType.Kind, true
		}
		if kind == "SCALAR" || kind == "ENUM" {
			leaves[f.Name] = nonNull
		}
	}
	if len(leaves) == 0 {
		t.Fatalf("introspection found no leaf fields on %s", typeName)
	}
	return leaves
}

// TestGeneratedResolvers_ReturnEveryExposedField pins the PRD §26.12
// limitation "The generated resolvers return all fields the schema exposes":
// there is no field-level security, so a query naming every leaf field of a
// type gets every one of them back, with the stored values.
func TestGeneratedResolvers_ReturnEveryExposedField(t *testing.T) {
	truncateAll(t)
	cat := seedCategory(t, "EveryField")

	var created struct {
		CreateProduct struct {
			ID string `json:"id"`
		} `json:"createProduct"`
	}
	gqlExecData(t, `mutation ($categoryID: Int!, $at: Time!) {
		createProduct(input: {
			name: "every field", description: "described", price: "12.50", stock: 7,
			categoryID: $categoryID, createdAt: $at, updatedAt: $at
		}) { id }
	}`, map[string]any{"categoryID": cat.ID, "at": fixedTimestamp}, &created)

	leaves := twrLeafFields(t, "Product")
	names := make([]string, 0, len(leaves))
	for name := range leaves {
		names = append(names, name)
	}
	query := "query ($id: UUID!) { product(id: $id) { " + strings.Join(names, " ") + " } }"
	var got struct {
		Product map[string]json.RawMessage `json:"product"`
	}
	gqlExecData(t, query, map[string]any{"id": created.CreateProduct.ID}, &got)
	if got.Product == nil {
		t.Fatalf("product(id: %s) returned null", created.CreateProduct.ID)
	}

	// deletedAt is the one leaf the fixture leaves unset; every other field
	// was written, so a null there is a field the resolver withheld.
	for name := range leaves {
		raw, ok := got.Product[name]
		if !ok {
			t.Errorf("Product.%s was selected but is absent from the response", name)
			continue
		}
		if name != "deletedAt" && string(raw) == "null" {
			t.Errorf("Product.%s came back null; the stored value was withheld", name)
		}
	}
	for name, want := range map[string]string{
		"name":        `"every field"`,
		"description": `"described"`,
		"stock":       `7`,
	} {
		if got := string(got.Product[name]); got != want {
			t.Errorf("Product.%s = %s, want %s", name, got, want)
		}
	}
}

// TestUpdateWithRelated_AncestorRingNotDetected pins the PRD §26.12
// limitation "A nested `connect` on a self-referential edge detects only the
// depth-0 cycle … connecting a row to one of its own ancestors closes a
// longer ring that no depth-1 check can see". The depth-0 refusal is pinned by
// TestUpdateWithRelated_SelfReferenceIsRefused; this is the ring it does not
// see, which succeeds and leaves the two rows each other's parent.
func TestUpdateWithRelated_AncestorRingNotDetected(t *testing.T) {
	truncateAll(t)
	client, ctx := nestedTenantedClient(t)

	ancestor, err := client.WorkspaceNotes().Create(ctx, &models.CreateWorkspaceNoteInput{Body: "ring-ancestor"})
	if err != nil {
		t.Fatalf("seeding ancestor: %v", err)
	}
	child, err := client.WorkspaceNotes().Create(ctx, &models.CreateWorkspaceNoteInput{
		Body:     "ring-child",
		ParentID: omittable.Set(&ancestor.ID),
	})
	if err != nil {
		t.Fatalf("seeding child: %v", err)
	}

	// The ancestor is unparented, so the default (no allow_reparent) connect
	// adopts it under its own descendant.
	_, err = client.WorkspaceNotes().UpdateWithRelated(ctx, child.ID, &models.UpdateWorkspaceNoteWithRelatedInput{
		Children: &models.WorkspaceNoteChildrenUpdateNested{Connect: []uuid.UUID{ancestor.ID}},
	})
	if err != nil {
		t.Fatalf("connecting a row's ancestor under it = %v, want success: the ring is not detected", err)
	}

	for _, tt := range []struct {
		name       string
		id         uuid.UUID
		wantParent uuid.UUID
	}{
		{name: "ancestor now hangs under its child", id: ancestor.ID, wantParent: child.ID},
		{name: "child keeps the ancestor as parent", id: child.ID, wantParent: ancestor.ID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			row, err := client.WorkspaceNotes().Get(ctx, tt.id)
			if err != nil {
				t.Fatalf("re-reading note: %v", err)
			}
			if row.ParentID == nil || *row.ParentID != tt.wantParent {
				t.Errorf("parent_id = %v, want %v", row.ParentID, tt.wantParent)
			}
		})
	}
}
