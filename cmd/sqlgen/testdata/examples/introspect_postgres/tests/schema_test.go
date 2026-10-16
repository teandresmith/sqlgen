package tests

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/introspect_postgres/models"
)

// manifestEntity is the part of a manifest entity these tests read.
type manifestEntity struct {
	Table   string          `json:"table"`
	Schema  string          `json:"schema"`
	Comment string          `json:"comment"`
	Indexes []manifestIndex `json:"indexes"`
}

type manifestIndex struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}

func readManifestEntities(t *testing.T) map[string]manifestEntity {
	t.Helper()
	raw, err := os.ReadFile("../models/manifest/manifest_gen.json")
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}
	var doc struct {
		Entities []manifestEntity `json:"entities"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decoding manifest: %v", err)
	}
	entities := make(map[string]manifestEntity, len(doc.Entities))
	for _, e := range doc.Entities {
		entities[e.Table] = e
	}
	return entities
}

// The manifest describes the migrated schema: the table migrations/ drops is
// gone, the one it creates is there, and the UNIQUE constraint it adds is
// listed under its constraint name. Comments come from the database.
func TestManifestDescribesMigratedSchema(t *testing.T) {
	entities := readManifestEntities(t)
	if _, ok := entities["legacy_events"]; ok {
		t.Error("manifest lists legacy_events, which migrations/ drops")
	}
	if _, ok := entities["task_labels"]; !ok {
		t.Error("manifest has no task_labels, which migrations/ creates")
	}
	if !slices.ContainsFunc(entities["projects"].Indexes, func(i manifestIndex) bool {
		return i.Name == "projects_slug_key" && i.Unique && slices.Equal(i.Columns, []string{"slug"})
	}) {
		t.Errorf("projects indexes = %+v, want unique projects_slug_key on slug", entities["projects"].Indexes)
	}
	if got := entities["accounts"].Comment; got != "Customer accounts" {
		t.Errorf("accounts comment = %q, want the database's COMMENT ON TABLE", got)
	}
	if got := entities["invoices"].Schema; got != "billing" {
		t.Errorf("invoices schema = %q, want billing", got)
	}
}

// email_address is a domain with a CHECK, and the database enforces it.
// postal_address is a composite, written and read in its text form.
func TestDomainAndCompositeColumns(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	_, err := client.Accounts().Create(ctx, &models.CreateAccountInput{Email: "no-at-sign", DisplayName: "Invalid"})
	if err == nil {
		t.Error("Create with email no-at-sign succeeded, want the domain CHECK to reject it")
	}

	address := `("1 Main St",Springfield)`
	account, err := client.Accounts().Create(ctx, &models.CreateAccountInput{
		Email:       "composite@example.com",
		DisplayName: "Composite",
		Address:     omittable.Set(&address),
		Status:      omittable.Set(models.AccountStatusSuspended),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Accounts().HardDelete(ctx, account.ID) })
	if account.Address == nil || *account.Address != address {
		t.Errorf("Address = %v, want %s", account.Address, address)
	}
	if account.Status != models.AccountStatusSuspended {
		t.Errorf("Status = %q, want %q", account.Status, models.AccountStatusSuspended)
	}
}
