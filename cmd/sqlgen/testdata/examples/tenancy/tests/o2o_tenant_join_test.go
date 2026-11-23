package tests

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// TestRelationship_O2O_ParentKeptWithoutVisibleChild pins where a tenanted
// o2o child's tenant filter goes: into the LEFT JOIN's ON (§29.10), so a
// parent whose child is absent, or sits in another tenant, is still returned
// with the edge nil (§13.2 "A LEFT JOIN returns the parent row even when the
// related entity does not exist").
func TestRelationship_O2O_ParentKeptWithoutVisibleChild(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	withProfile := func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{ID: true, Profile: &models.UserProfileFieldOptions{ID: true}}
	}

	t.Run("has-one with no child row", func(t *testing.T) {
		u, err := envA.client.Users().Create(ctx, &models.CreateUserInput{Email: "lonely@x.com"})
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
		got, err := envA.client.Users().Get(ctx, u.ID, withProfile)
		if err != nil {
			t.Fatalf("Get user with Profile: %v", err)
		}
		if got.Profile != nil {
			t.Errorf("Profile = %+v, want nil", got.Profile)
		}
	})

	t.Run("has-one whose child is in another tenant", func(t *testing.T) {
		u, err := envA.client.Users().Create(ctx, &models.CreateUserInput{Email: "drift@x.com"})
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
		if _, err := envB.client.UserProfiles().Create(ctx, &models.CreateUserProfileInput{UserID: u.ID}); err != nil {
			t.Fatalf("create tenant B profile: %v", err)
		}
		got, err := envA.client.Users().Get(ctx, u.ID, withProfile)
		if err != nil {
			t.Fatalf("Get user with Profile: %v", err)
		}
		if got.Profile != nil {
			t.Errorf("Profile = %+v, want nil: tenant B's row must not load into tenant A", got.Profile)
		}
	})

	t.Run("belongs-to with a NULL foreign key", func(t *testing.T) {
		cy, err := envA.client.Cycles().Create(ctx, &models.CreateCycleInput{Name: "c1"})
		if err != nil {
			t.Fatalf("create cycle: %v", err)
		}
		if _, err := envA.client.Tasks().Create(ctx, &models.CreateTaskInput{CycleID: omittable.Set(&cy.ID), Title: "linked"}); err != nil {
			t.Fatalf("create linked task: %v", err)
		}
		if _, err := envA.client.Tasks().Create(ctx, &models.CreateTaskInput{Title: "free"}); err != nil {
			t.Fatalf("create free task: %v", err)
		}
		rows, err := envA.client.Tasks().GetMany(ctx, &models.GetTasksInput{}, func(o *models.CallOptions[models.TaskFieldOptions]) {
			o.FieldOptions = &models.TaskFieldOptions{Title: true, Cycle: &models.CycleFieldOptions{ID: true}}
		})
		if err != nil {
			t.Fatalf("GetMany tasks with Cycle: %v", err)
		}
		got := map[string]bool{}
		for _, r := range rows {
			got[r.Title] = r.Cycle != nil
		}
		if diff := cmp.Diff(map[string]bool{"linked": true, "free": false}, got); diff != "" {
			t.Errorf("tasks by title, cycle loaded (-want +got):\n%s", diff)
		}
	})
}
