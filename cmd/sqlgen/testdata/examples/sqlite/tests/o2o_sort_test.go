package tests

// An ORDER BY on the O2O-join read path. Every sort names a
// column of the parent table; the joined profiles table has an `id` too, so
// the builder has to qualify it with the parent's alias or the query fails
// with "ambiguous column name: id". Connection always sorts on its cursor
// keys, so without that every connection that selected Profile failed.

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

func TestO2OJoin_SortedReads(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Each user's profile carries its user's index, so a row whose profile
	// came from another user is caught.
	var ids []int64
	handles := map[int64]string{}
	for i := range 3 {
		u, err := client.Users().Create(ctx, &models.CreateUserInput{
			Email: fmt.Sprintf("o2osort-%d@example.com", i), Name: fmt.Sprintf("O2OSort%d", i),
		})
		if err != nil {
			t.Fatalf("Create user %d: %v", i, err)
		}
		t.Cleanup(func() { _ = client.Users().HardDelete(ctx, u.ID) })
		handle := fmt.Sprintf("o2osort-%d", i)
		p, err := client.Profiles().Create(ctx, &models.CreateProfileInput{UserID: u.ID, GithubHandle: omittable.Set(&handle)})
		if err != nil {
			t.Fatalf("Create profile %d: %v", i, err)
		}
		t.Cleanup(func() { _ = client.Profiles().HardDelete(ctx, p.ID) })
		ids = append(ids, u.ID)
		handles[u.ID] = handle
	}
	filter := &models.UserFilter{ID: &comparator.Number[int64]{In: ids}}
	withProfile := func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{
			ID: true, Name: true,
			Profile: &models.ProfileFieldOptions{ID: true, GithubHandle: true},
		}
	}
	descending := slices.Clone(ids)
	slices.Reverse(descending)

	check := func(t *testing.T, users []*models.User, want []int64) {
		t.Helper()
		got := make([]int64, len(users))
		for i, u := range users {
			got[i] = u.ID
			if u.Profile == nil || u.Profile.GithubHandle == nil {
				t.Errorf("user %d: Profile not loaded", u.ID)
				continue
			}
			if *u.Profile.GithubHandle != handles[u.ID] {
				t.Errorf("user %d: Profile.GithubHandle = %q, want %q", u.ID, *u.Profile.GithubHandle, handles[u.ID])
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("user IDs = %v, want %v", got, want)
		}
	}

	t.Run("GetMany sorted on a column both tables have", func(t *testing.T) {
		users, err := client.Users().GetMany(ctx, &models.GetUsersInput{
			Filter: filter,
			Sorts:  []sql.Sort{{Column: "id", Direction: sql.Desc}},
		}, withProfile)
		if err != nil {
			t.Fatalf("GetMany: %v", err)
		}
		check(t, users, descending)
	})

	t.Run("Paginate sorted", func(t *testing.T) {
		page, err := client.Users().Paginate(ctx, models.PaginateInput[models.UserFilter]{
			Filter: filter,
			Sort:   []sql.Sort{{Column: "id", Direction: sql.Desc}},
			Limit:  2,
			Offset: 1,
		}, withProfile)
		if err != nil {
			t.Fatalf("Paginate: %v", err)
		}
		check(t, page.Items, descending[1:])
	})

	t.Run("Connection forward across pages", func(t *testing.T) {
		var got []*models.User
		var after *string
		for range len(ids) {
			first := 2
			conn, err := client.Users().Connection(ctx, models.ConnectionInput[models.UserFilter]{
				Filter: filter, First: &first, After: after,
			}, withProfile)
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

	t.Run("Connection backward", func(t *testing.T) {
		last := 2
		conn, err := client.Users().Connection(ctx, models.ConnectionInput[models.UserFilter]{
			Filter: filter, Last: &last,
		}, withProfile)
		if err != nil {
			t.Fatalf("Connection: %v", err)
		}
		users := make([]*models.User, len(conn.Edges))
		for i, e := range conn.Edges {
			users[i] = e.Node
		}
		check(t, users, ids[1:])
	})
}
