package tests

// An ORDER BY on the O2O-join read path. Every sort names a
// column of the parent table. The joined table usually has the same column
// (`id`, and here `name` and `user_id` too), so the builder has to qualify it
// with the parent's alias or PostgreSQL rejects the query with `column
// reference "id" is ambiguous` (SQLSTATE 42702). Connection always sorts on
// its cursor keys, so without that every connection that selected an O2O
// edge failed, over GraphQL as well as through the Go client.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// seedO2OSortAssets creates n assets, each with a primary document named
// after its asset, and returns the asset IDs in ascending order.
func seedO2OSortAssets(t *testing.T, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := range n {
		a, err := testClient.Assets().CreateWithRelated(context.Background(), &models.CreateAssetWithRelatedInput{
			Asset: models.CreateAssetInput{Name: fmt.Sprintf("o2o-sort-%d", i)},
			PrimaryDocument: &models.AssetPrimaryDocumentCreateNested{
				Create: &models.AssetPrimaryDocumentCreateInput{Name: fmt.Sprintf("o2o-sort-%d-doc", i)},
			},
		})
		if err != nil {
			t.Fatalf("seeding asset %d: %v", i, err)
		}
		ids = append(ids, a.ID.String())
	}
	slices.Sort(ids)
	return ids
}

func TestO2OJoin_SortedReads(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()
	ids := seedO2OSortAssets(t, 3)
	filter := &models.AssetFilter{ID: &comparator.ID{In: ids}}
	withDocument := func(o *models.CallOptions[models.AssetFieldOptions]) {
		o.FieldOptions = &models.AssetFieldOptions{
			ID: true, Name: true,
			PrimaryDocument: &models.DocumentFieldOptions{ID: true, Name: true},
		}
	}
	descending := slices.Clone(ids)
	slices.Reverse(descending)

	check := func(t *testing.T, assets []*models.Asset, want []string) {
		t.Helper()
		got := make([]string, len(assets))
		for i, a := range assets {
			got[i] = a.ID.String()
			if a.PrimaryDocument == nil {
				t.Errorf("asset %s: PrimaryDocument not loaded", a.ID)
				continue
			}
			if want := a.Name + "-doc"; a.PrimaryDocument.Name != want {
				t.Errorf("asset %s: PrimaryDocument.Name = %q, want %q", a.ID, a.PrimaryDocument.Name, want)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("asset IDs = %v, want %v", got, want)
		}
	}

	t.Run("GetMany sorted on a column both tables have", func(t *testing.T) {
		assets, err := testClient.Assets().GetMany(ctx, &models.GetAssetsInput{
			Filter: filter,
			Sorts:  []sql.Sort{{Column: "id", Direction: sql.Desc}},
		}, withDocument)
		if err != nil {
			t.Fatalf("GetMany: %v", err)
		}
		check(t, assets, descending)
	})

	t.Run("Paginate sorted", func(t *testing.T) {
		page, err := testClient.Assets().Paginate(ctx, models.PaginateInput[models.AssetFilter]{
			Filter: filter,
			Sort:   []sql.Sort{{Column: "id", Direction: sql.Desc}},
			Limit:  2,
			Offset: 1,
		}, withDocument)
		if err != nil {
			t.Fatalf("Paginate: %v", err)
		}
		check(t, page.Items, descending[1:])
	})

	t.Run("Connection forward across pages", func(t *testing.T) {
		var got []*models.Asset
		var after *string
		for range len(ids) {
			first := 2
			conn, err := testClient.Assets().Connection(ctx, models.ConnectionInput[models.AssetFilter]{
				Filter: filter, First: &first, After: after,
			}, withDocument)
			if err != nil {
				t.Fatalf("Connection: %v", err)
			}
			for _, e := range conn.Edges {
				got = append(got, e.Node)
			}
			if !conn.PageInfo.HasNextPage {
				break
			}
			after = conn.PageInfo.EndCursor
		}
		check(t, got, ids)
	})
}

// TestO2OJoin_ConnectionOnNonPKCursorKey pins a connection whose cursor key
// is not `id`: user_credentials pages on its PK `user_id`, and the
// Users.Badge chain joins user_badges, which has a `user_id` of its own.
func TestO2OJoin_ConnectionOnNonPKCursorKey(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()
	var want []string
	labels := map[string]string{}
	for i := range 3 {
		u, err := testClient.Users().Create(ctx, &models.CreateUserInput{
			Email: fmt.Sprintf("o2o-cred-%d@example.com", i), Name: fmt.Sprintf("o2o-cred-%d", i),
		})
		if err != nil {
			t.Fatalf("seeding user %d: %v", i, err)
		}
		label := fmt.Sprintf("badge-%d", i)
		if _, err := testClient.UserBadges().Create(ctx, &models.CreateUserBadgeInput{UserID: omittable.Set(new(u.ID)), Label: label}); err != nil {
			t.Fatalf("seeding badge %d: %v", i, err)
		}
		if _, err := testClient.UserCredentials().Create(ctx, &models.CreateUserCredentialInput{UserID: u.ID, Provider: "o2o", ExternalID: fmt.Sprint(i)}); err != nil {
			t.Fatalf("seeding credential %d: %v", i, err)
		}
		want = append(want, u.ID.String())
		labels[u.ID.String()] = label
	}
	slices.Sort(want)

	var got []string
	var after *string
	for range len(want) {
		first := 2
		conn, err := testClient.UserCredentials().Connection(ctx, models.ConnectionInput[models.UserCredentialFilter]{
			First: &first, After: after,
		}, func(o *models.CallOptions[models.UserCredentialFieldOptions]) {
			o.FieldOptions = &models.UserCredentialFieldOptions{
				UserID: true,
				Users: &models.UserFieldOptions{
					ID:    true,
					Badge: &models.UserBadgeFieldOptions{ID: true, Label: true},
				},
			}
		})
		if err != nil {
			t.Fatalf("Connection: %v", err)
		}
		for _, e := range conn.Edges {
			id := e.Node.UserID.String()
			got = append(got, id)
			if e.Node.Users == nil || e.Node.Users.Badge == nil {
				t.Errorf("credential %s: Users.Badge not loaded", id)
				continue
			}
			if e.Node.Users.Badge.Label != labels[id] {
				t.Errorf("credential %s: Users.Badge.Label = %q, want %q", id, e.Node.Users.Badge.Label, labels[id])
			}
		}
		if !conn.PageInfo.HasNextPage {
			break
		}
		after = conn.PageInfo.EndCursor
	}
	if !slices.Equal(got, want) {
		t.Errorf("credential user IDs = %v, want %v", got, want)
	}
}

// TestO2OJoin_GraphQLConnection runs the connection queries that select an
// O2O edge, which once came back INTERNAL when the sort column was ambiguous.
func TestO2OJoin_GraphQLConnection(t *testing.T) {
	truncateAll(t)
	ids := seedO2OSortAssets(t, 3)

	var assets struct {
		Assets struct {
			Edges []struct {
				Node struct {
					ID              string `json:"id"`
					Name            string `json:"name"`
					PrimaryDocument *struct {
						Name string `json:"name"`
					} `json:"primaryDocument"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"assets"`
	}
	gqlExecData(t, `{ assets(first: 5) { edges { node { id name primaryDocument { name } } } } }`, nil, &assets)
	var got []string
	for _, e := range assets.Assets.Edges {
		got = append(got, e.Node.ID)
		if e.Node.PrimaryDocument == nil || e.Node.PrimaryDocument.Name != e.Node.Name+"-doc" {
			t.Errorf("asset %s: primaryDocument = %+v, want name %q", e.Node.ID, e.Node.PrimaryDocument, e.Node.Name+"-doc")
		}
	}
	if !slices.Equal(got, ids) {
		t.Errorf("asset IDs = %v, want %v", got, ids)
	}

	u, err := testClient.Users().Create(context.Background(), &models.CreateUserInput{Email: "o2o-gql@example.com", Name: "o2o-gql"})
	if err != nil {
		t.Fatalf("seeding user: %v", err)
	}
	if _, err := testClient.UserBadges().Create(context.Background(), &models.CreateUserBadgeInput{UserID: omittable.Set(new(u.ID)), Label: "gql-badge"}); err != nil {
		t.Fatalf("seeding badge: %v", err)
	}
	resp := gqlExec(t, `{ users(first: 5) { edges { node { id badge { label } } } } }`, nil, nil)
	if len(resp.Errors) > 0 {
		t.Fatalf("users connection selecting badge: %+v", resp.Errors)
	}
	if !strings.Contains(string(resp.Data), `"label":"gql-badge"`) {
		t.Errorf("users connection data = %s, want the badge labelled gql-badge", resp.Data)
	}
}
