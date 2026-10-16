package tests

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/introspect_mysql/models"
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
// migrated table has, so it succeeds against it. is_active is a BOOLEAN with
// DEFAULT TRUE, which MySQL stores as 1, and the default reads back true.
func TestCreateAfterDropColumn(t *testing.T) {
	account := newAccount(t, newClient(), "dropped@example.com")
	if account.ID == 0 {
		t.Error("Create returned no auto-increment value")
	}
	if !account.IsActive {
		t.Error("IsActive = false, want the column default true")
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

// migrations/ adds tasks.priority as an inline ENUM. A valid value
// round-trips, and the database rejects any other.
func TestNewEnumRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	project := newProject(t, client, newAccount(t, client, "enum@example.com"), "enum")

	task := newTask(t, client, project, &models.CreateTaskInput{Title: "Defaulted"})
	if task.Priority != models.TasksPriorityEnumNormal {
		t.Errorf("Create: Priority = %q, want the column default %q", task.Priority, models.TasksPriorityEnumNormal)
	}
	updated, err := client.Tasks().Update(ctx, task.ID, &models.UpdateTaskInput{Priority: omittable.Set(models.TasksPriorityEnumHigh)})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Priority != models.TasksPriorityEnumHigh {
		t.Errorf("Update: Priority = %q, want %q", updated.Priority, models.TasksPriorityEnumHigh)
	}

	_, err = client.Tasks().Create(ctx, &models.CreateTaskInput{
		ProjectID: project.ID,
		Title:     "Invalid",
		Priority:  omittable.Set(models.TasksPriorityEnum("urgent")),
	})
	if err == nil {
		t.Error("Create with priority urgent succeeded, want the database to reject it")
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
	task := newTask(t, client, project, &models.CreateTaskInput{Title: "Estimated", EstimateMinutes: omittable.Set[uint32](3)})
	if task.EstimateSeconds == nil || *task.EstimateSeconds != 180 {
		t.Errorf("EstimateSeconds = %v, want 180", task.EstimateSeconds)
	}
}

// migrations/ adds UNIQUE (slug) to projects, which makes it an upsert target.
func TestUpsertOnAddedUnique(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	account := newAccount(t, client, "upsert@example.com")
	project := newProject(t, client, account, "upsert")

	upserted, err := client.Projects().Upsert(ctx, &models.CreateProjectInput{
		AccountID: account.ID,
		Name:      "Renamed",
		Slug:      "upsert",
		Summary:   "Updated summary",
	}, models.ProjectConflictSlug)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if upserted.ID != project.ID || upserted.Name != "Renamed" {
		t.Errorf("Upsert = {ID: %d, Name: %q}, want {ID: %d, Name: Renamed}", upserted.ID, upserted.Name, project.ID)
	}
}

// projects.tags is a SET, and the unsigned keys scan into uint64.
func TestSetColumn(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	account := newAccount(t, client, "set@example.com")
	tags := models.ProjectsTagsSet{models.ProjectsTagsSetValueInternal, models.ProjectsTagsSetValueUrgent}
	project, err := client.Projects().Create(ctx, &models.CreateProjectInput{
		AccountID: account.ID,
		Name:      "Tagged",
		Slug:      "tagged",
		Summary:   "Tagged project",
		Tags:      omittable.Set(tags),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Projects().HardDelete(ctx, project.ID) })

	got, err := client.Projects().Get(ctx, project.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !slices.Equal(got.Tags, tags) {
		t.Errorf("Tags = %v, want %v", got.Tags, tags)
	}
	if got.AccountID != account.ID {
		t.Errorf("AccountID = %d, want %d", got.AccountID, account.ID)
	}
}
