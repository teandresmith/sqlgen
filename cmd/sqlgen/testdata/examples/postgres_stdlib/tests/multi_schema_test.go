package tests

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres_stdlib/models"
)

// --- Multi-schema: AuditUser CRUD ---

func TestAuditUserCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	auditUser, err := client.AuditUsers().Create(ctx, &models.CreateAuditUserInput{
		Action:  "user_created",
		Details: omittable.Set(mustJSON(map[string]any{"user": "alice@multi.test"})),
	})
	if err != nil {
		t.Fatalf("Create audit user: %v", err)
	}
	if auditUser.ID == (uuid.UUID{}) {
		t.Fatal("Create audit user: expected non-empty ID")
	}
	if auditUser.Action != "user_created" {
		t.Errorf("Action = %q, want %q", auditUser.Action, "user_created")
	}

	got, err := client.AuditUsers().Get(ctx, auditUser.ID)
	if err != nil {
		t.Fatalf("Get audit user: %v", err)
	}
	if got.Action != "user_created" {
		t.Errorf("Action = %q, want %q", got.Action, "user_created")
	}

	updated, err := client.AuditUsers().Update(ctx, auditUser.ID, &models.UpdateAuditUserInput{
		Action: omittable.Set("user_updated"),
	})
	if err != nil {
		t.Fatalf("Update audit user: %v", err)
	}
	if updated.Action != "user_updated" {
		t.Errorf("Action = %q, want %q", updated.Action, "user_updated")
	}

	if err := client.AuditUsers().HardDelete(ctx, auditUser.ID); err != nil {
		t.Fatalf("HardDelete audit user: %v", err)
	}
}

// --- Multi-schema: Event CRUD (unique name, no prefix) ---

func TestEventCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	event, err := client.Events().Create(ctx, &models.CreateEventInput{
		Name:    "login",
		Payload: omittable.Set(mustJSON(map[string]any{"ip": "127.0.0.1"})),
	})
	if err != nil {
		t.Fatalf("Create event: %v", err)
	}
	if event.Name != "login" {
		t.Errorf("Name = %q, want %q", event.Name, "login")
	}

	got, err := client.Events().Get(ctx, event.ID)
	if err != nil {
		t.Fatalf("Get event: %v", err)
	}
	if got.Name != "login" {
		t.Errorf("Name = %q, want %q", got.Name, "login")
	}

	if err := client.Events().HardDelete(ctx, event.ID); err != nil {
		t.Fatalf("HardDelete event: %v", err)
	}
}

// --- Multi-schema: cross-contamination check ---

func TestMultiSchemaIsolation(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	publicUser, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Name:  "IsolationTest",
		Email: "isolation@multi.test",
	})
	if err != nil {
		t.Fatalf("Create public user: %v", err)
	}

	auditUser, err := client.AuditUsers().Create(ctx, &models.CreateAuditUserInput{
		Action:  "account_created",
		Details: omittable.Set(mustJSON(map[string]any{"name": "IsolationTest"})),
	})
	if err != nil {
		t.Fatalf("Create audit user: %v", err)
	}

	gotPublic, err := client.PublicUsers().Get(ctx, publicUser.ID)
	if err != nil {
		t.Fatalf("Get public user: %v", err)
	}
	if gotPublic.Name != "IsolationTest" {
		t.Errorf("Public user Name = %q, want %q", gotPublic.Name, "IsolationTest")
	}

	gotAudit, err := client.AuditUsers().Get(ctx, auditUser.ID)
	if err != nil {
		t.Fatalf("Get audit user: %v", err)
	}
	if gotAudit.Action != "account_created" {
		t.Errorf("Audit user Action = %q, want %q", gotAudit.Action, "account_created")
	}

	_ = client.PublicUsers().HardDelete(ctx, publicUser.ID)
	_ = client.AuditUsers().HardDelete(ctx, auditUser.ID)
}
