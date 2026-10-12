package tests

import (
	"context"
	"reflect"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/introspect_sqlite/models"
)

// The columns migrations/ adds to accounts round-trip through Create, Get and
// Update: nickname is nullable, locale is NOT NULL with a default.
func TestAddedColumnsRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	created, err := client.Accounts().Create(ctx, &models.CreateAccountInput{
		Email:       "added@example.com",
		DisplayName: "Added",
		Nickname:    omittable.Set(new("ace")),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Accounts().HardDelete(ctx, created.ID) })
	if created.Locale != "en" {
		t.Errorf("Create: Locale = %q, want the column default %q", created.Locale, "en")
	}

	got, err := client.Accounts().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Nickname == nil || *got.Nickname != "ace" {
		t.Errorf("Get: Nickname = %v, want ace", got.Nickname)
	}

	updated, err := client.Accounts().Update(ctx, created.ID, &models.UpdateAccountInput{
		Nickname: omittable.Set[*string](nil),
		Locale:   omittable.Set("fr"),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Nickname != nil || updated.Locale != "fr" {
		t.Errorf("Update: Nickname = %v, Locale = %q, want nil and fr", updated.Nickname, updated.Locale)
	}
}

// migrations/ drops accounts.legacy_code. Create writes only the columns the
// migrated table has, so it succeeds against it. accounts.id is a rowid
// alias, so the database assigns it.
func TestCreateAfterDropColumn(t *testing.T) {
	account := newAccount(t, newClient(), "dropped@example.com")
	if account.ID == 0 {
		t.Error("Create returned no rowid")
	}
}

// task_labels exists only in migrations/. Its CRUD works against the
// migrated database.
func TestNewTableCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	project := newProject(t, client, newAccount(t, client, "labels@example.com"), "labels")
	task := newTask(t, client, project, &models.CreateTaskInput{Title: "Label me"})

	label, err := client.TaskLabels().Create(ctx, &models.CreateTaskLabelInput{TaskID: task.ID, Label: "bug"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := client.TaskLabels().Update(ctx, label.ID, &models.UpdateTaskLabelInput{Label: omittable.Set("feature")})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Label != "feature" {
		t.Errorf("Update: Label = %q, want feature", updated.Label)
	}
	if err := client.TaskLabels().HardDelete(ctx, label.ID); err != nil {
		t.Fatalf("HardDelete: %v", err)
	}
	if exists, err := client.TaskLabels().Exists(ctx, label.ID); err != nil || exists {
		t.Errorf("Exists after HardDelete = %v, %v, want false, nil", exists, err)
	}
}

// tasks.estimate_seconds is a generated column: no write input carries it,
// and it reads back computed.
func TestGeneratedColumn(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[models.CreateTaskInput](), reflect.TypeFor[models.UpdateTaskInput]()} {
		if _, ok := typ.FieldByName("EstimateSeconds"); ok {
			t.Errorf("%s has an EstimateSeconds field, want the generated column left out", typ.Name())
		}
	}

	client := newClient()
	project := newProject(t, client, newAccount(t, client, "estimate@example.com"), "estimate")
	task := newTask(t, client, project, &models.CreateTaskInput{Title: "Estimated", EstimateMinutes: omittable.Set[int64](3)})
	if task.EstimateSeconds == nil || *task.EstimateSeconds != 180 {
		t.Errorf("EstimateSeconds = %v, want 180", task.EstimateSeconds)
	}
}

// migrations/ adds a unique index on projects.slug, which makes it an upsert
// target.
func TestUpsertOnAddedUnique(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	account := newAccount(t, client, "upsert@example.com")
	project := newProject(t, client, account, "upsert")

	upserted, err := client.Projects().Upsert(ctx, &models.CreateProjectInput{
		AccountID: account.ID,
		Name:      "Renamed",
		Slug:      "upsert",
	}, models.ProjectConflictSlug)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if upserted.ID != project.ID || upserted.Name != "Renamed" {
		t.Errorf("Upsert = {ID: %d, Name: %q}, want {ID: %d, Name: Renamed}", upserted.ID, upserted.Name, project.ID)
	}
}

// project_members declares PRIMARY KEY (account_id, project_id), the reverse
// of its column order. Get and Update take that key.
func TestCompositePrimaryKey(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	account := newAccount(t, client, "member@example.com")
	project := newProject(t, client, account, "members")

	member, err := client.ProjectMembers().Create(ctx, &models.CreateProjectMemberInput{
		ProjectID: project.ID,
		AccountID: account.ID,
		Role:      "viewer",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	pk := models.ProjectMemberPK{ProjectID: member.ProjectID, AccountID: member.AccountID}
	t.Cleanup(func() { _ = client.ProjectMembers().HardDelete(ctx, pk) })

	updated, err := client.ProjectMembers().Update(ctx, pk, &models.UpdateProjectMemberInput{Role: omittable.Set("owner")})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := client.ProjectMembers().Get(ctx, pk)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Role != "owner" || got.Role != "owner" {
		t.Errorf("Role after Update = %q, Get = %q, want owner", updated.Role, got.Role)
	}
}
