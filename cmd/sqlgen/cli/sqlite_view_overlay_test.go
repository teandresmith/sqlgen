package cli

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestParseSchema_SQLiteQualifiedViewFileOverlays pins that under source:
// database an input.views file naming the view main.v overlays the
// database's view v: one view results, with the database's columns and the
// file's annotations.
func TestParseSchema_SQLiteQualifiedViewFileOverlays(t *testing.T) {
	dir := t.TempDir()
	dbPath := sqliteDatabase(t, dir, sqliteUsersDDL+
		"CREATE VIEW v AS SELECT id, name FROM users;")
	viewsDir := filepath.Join(dir, "views")
	writeFile(t, viewsDir, "v.sql", "-- @pk: id\n\nCREATE VIEW main.v AS SELECT id FROM users;\n")
	cfg := sqliteSourceConfig(config.SourceDatabase, dbPath)
	cfg.Input.Views = []string{viewsDir}

	schema, err := parseSchema(t.Context(), cfg)
	if err != nil {
		t.Fatalf("parseSchema: %v", err)
	}
	if len(schema.Views) != 1 {
		t.Fatalf("views = %+v, want one view v", schema.Views)
	}
	view := schema.Views[0]
	if view.Name != "v" {
		t.Errorf("view name = %q, want %q", view.Name, "v")
	}
	type column struct {
		Name       string
		PrimaryKey bool
		Nullable   bool
	}
	got := make([]column, 0, len(view.Columns))
	for _, c := range view.Columns {
		got = append(got, column{c.Name, c.PrimaryKey, c.Nullable})
	}
	want := []column{
		{Name: "id", PrimaryKey: true},
		{Name: "name"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("view columns (-want +got):\n%s", diff)
	}
}
