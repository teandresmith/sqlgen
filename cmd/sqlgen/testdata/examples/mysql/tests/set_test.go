package tests

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// --- SET create + get round-trip ---

func TestSETCreateAndGet(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	wantPerms := models.UsersPermissionsSet{
		models.UsersPermissionsSetValueRead,
		models.UsersPermissionsSetValueWrite,
		models.UsersPermissionsSetValueAdmin,
	}

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email:       "set-crud@example.com",
		Name:        "SET CRUD User",
		Balance:     0,
		Permissions: omittable.Set(wantPerms),
	})
	if err != nil {
		t.Fatalf("Create user with SET: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	if diff := cmp.Diff(wantPerms, user.Permissions); diff != "" {
		t.Errorf("Create user permissions mismatch (-want +got):\n%s", diff)
	}

	got, err := client.Users().Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("Get user: %v", err)
	}
	if diff := cmp.Diff(wantPerms, got.Permissions); diff != "" {
		t.Errorf("Get user permissions mismatch (-want +got):\n%s", diff)
	}

	// Default value — single 'read' permission
	defaultUser, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email:   "set-default@example.com",
		Name:    "SET Default User",
		Balance: 0,
	})
	if err != nil {
		t.Fatalf("Create user with default SET: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, defaultUser.ID) })

	wantDefault := models.UsersPermissionsSet{models.UsersPermissionsSetValueRead}
	if diff := cmp.Diff(wantDefault, defaultUser.Permissions); diff != "" {
		t.Errorf("Default permissions mismatch (-want +got):\n%s", diff)
	}
}

// --- SET update ---

func TestSETUpdate(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email:       "set-update@example.com",
		Name:        "SET Update User",
		Balance:     0,
		Permissions: omittable.Set(models.UsersPermissionsSet{models.UsersPermissionsSetValueRead}),
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	newPerms := models.UsersPermissionsSet{
		models.UsersPermissionsSetValueRead,
		models.UsersPermissionsSetValueWrite,
		models.UsersPermissionsSetValueDelete,
		models.UsersPermissionsSetValueAdmin,
	}
	updated, err := client.Users().Update(ctx, user.ID, &models.UpdateUserInput{
		Permissions: omittable.Set(newPerms),
	})
	if err != nil {
		t.Fatalf("Update user permissions: %v", err)
	}
	if diff := cmp.Diff(newPerms, updated.Permissions); diff != "" {
		t.Errorf("Update user permissions mismatch (-want +got):\n%s", diff)
	}

	got, err := client.Users().Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("Get updated user: %v", err)
	}
	if diff := cmp.Diff(newPerms, got.Permissions); diff != "" {
		t.Errorf("Get updated user permissions mismatch (-want +got):\n%s", diff)
	}
}

// --- SET filter Eq exact match ---

func TestSETFilterEq(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	permsA := models.UsersPermissionsSet{models.UsersPermissionsSetValueRead, models.UsersPermissionsSetValueWrite}
	permsB := models.UsersPermissionsSet{models.UsersPermissionsSetValueRead, models.UsersPermissionsSetValueAdmin}

	userA, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "set-filter-eq-a@example.com", Name: "SET Filter Eq A",
		Balance: 0, Permissions: omittable.Set(permsA),
	})
	if err != nil {
		t.Fatalf("Create user A: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, userA.ID) })

	userB, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "set-filter-eq-b@example.com", Name: "SET Filter Eq B",
		Balance: 0, Permissions: omittable.Set(permsB),
	})
	if err != nil {
		t.Fatalf("Create user B: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, userB.ID) })

	// Eq filter: exact match on SET string representation
	target := permsA.String()
	results, err := client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			Permissions: &comparator.String{Eq: &target},
			ID:          &comparator.Number[int64]{In: []int64{userA.ID, userB.ID}},
		},
	})
	if err != nil {
		t.Fatalf("GetMany SET Eq: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("GetMany SET Eq: got %d results, want 1", len(results))
	}
	if results[0].ID != userA.ID {
		t.Errorf("GetMany SET Eq: got user ID %d, want %d", results[0].ID, userA.ID)
	}
}

// --- SET filter Contains substring match ---

func TestSETFilterContains(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	permsWithAdmin := models.UsersPermissionsSet{
		models.UsersPermissionsSetValueRead,
		models.UsersPermissionsSetValueAdmin,
	}
	permsNoAdmin := models.UsersPermissionsSet{
		models.UsersPermissionsSetValueRead,
		models.UsersPermissionsSetValueWrite,
	}

	userA, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "set-filter-contains-a@example.com", Name: "SET Contains A",
		Balance: 0, Permissions: omittable.Set(permsWithAdmin),
	})
	if err != nil {
		t.Fatalf("Create user A: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, userA.ID) })

	userB, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "set-filter-contains-b@example.com", Name: "SET Contains B",
		Balance: 0, Permissions: omittable.Set(permsNoAdmin),
	})
	if err != nil {
		t.Fatalf("Create user B: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, userB.ID) })

	// Contains "admin" — should match user A but not user B
	adminStr := "admin"
	results, err := client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			Permissions: &comparator.String{Contains: &adminStr},
			ID:          &comparator.Number[int64]{In: []int64{userA.ID, userB.ID}},
		},
	})
	if err != nil {
		t.Fatalf("GetMany SET Contains: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("GetMany SET Contains 'admin': got %d results, want 1", len(results))
	}
	if results[0].ID != userA.ID {
		t.Errorf("GetMany SET Contains 'admin': got user ID %d, want %d", results[0].ID, userA.ID)
	}

	// Contains "read" — should match both users
	readStr := "read"
	results, err = client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			Permissions: &comparator.String{Contains: &readStr},
			ID:          &comparator.Number[int64]{In: []int64{userA.ID, userB.ID}},
		},
	})
	if err != nil {
		t.Fatalf("GetMany SET Contains 'read': %v", err)
	}
	if len(results) != 2 {
		t.Errorf("GetMany SET Contains 'read': got %d results, want 2", len(results))
	}
}
