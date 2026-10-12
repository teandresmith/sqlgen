package tests

import (
	"context"
	"reflect"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/introspect_postgres/models"
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
// migrated table has, so it succeeds against it.
func TestCreateAfterDropColumn(t *testing.T) {
	account := newAccount(t, newClient(), "dropped@example.com")
	if account.ID == 0 {
		t.Error("Create returned no identity value")
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

// task_priority is created by migrations/ and used by the column it adds.
// A valid value round-trips, and the database rejects any other.
func TestNewEnumRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	project := newProject(t, client, newAccount(t, client, "enum@example.com"), "enum")

	task := newTask(t, client, project, &models.CreateTaskInput{Title: "Defaulted"})
	if task.Priority != models.TaskPriorityNormal {
		t.Errorf("Create: Priority = %q, want the column default %q", task.Priority, models.TaskPriorityNormal)
	}
	updated, err := client.Tasks().Update(ctx, task.ID, &models.UpdateTaskInput{Priority: omittable.Set(models.TaskPriorityHigh)})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Priority != models.TaskPriorityHigh {
		t.Errorf("Update: Priority = %q, want %q", updated.Priority, models.TaskPriorityHigh)
	}

	_, err = client.Tasks().Create(ctx, &models.CreateTaskInput{
		ProjectID: project.ID,
		Title:     "Invalid",
		Priority:  omittable.Set(models.TaskPriority("urgent")),
	})
	if err == nil {
		t.Error("Create with priority urgent succeeded, want the database to reject it")
	}
}

// invoices.total_cents is a generated column: no write input carries it, and
// it reads back computed.
func TestGeneratedColumn(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[models.CreateInvoiceInput](), reflect.TypeFor[models.UpdateInvoiceInput]()} {
		if _, ok := typ.FieldByName("TotalCents"); ok {
			t.Errorf("%s has a TotalCents field, want the generated column left out", typ.Name())
		}
	}

	ctx := context.Background()
	client := newClient()
	account := newAccount(t, client, "invoice@example.com")
	invoice, err := client.Invoices().Create(ctx, &models.CreateInvoiceInput{AccountID: account.ID, SubtotalCents: 1000, TaxCents: 80})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Invoices().HardDelete(ctx, invoice.ID) })
	if invoice.TotalCents == nil || *invoice.TotalCents != 1080 {
		t.Errorf("TotalCents = %v, want 1080", invoice.TotalCents)
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
