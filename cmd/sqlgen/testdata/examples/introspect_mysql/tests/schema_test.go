package tests

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// manifestEntity is the part of a manifest entity these tests read.
type manifestEntity struct {
	Table   string          `json:"table"`
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
// listed under its constraint name, as MySQL names the index. Comments come
// from the database.
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
		t.Errorf("accounts comment = %q, want the database's table COMMENT", got)
	}
}
