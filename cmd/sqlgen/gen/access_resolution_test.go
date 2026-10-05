package gen_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// accessFields is the tuple of resolved access fields asserted per column.
type accessFields struct {
	Access             string
	APIReadable        bool
	APIWritable        bool
	APIFilterable      bool
	APISortable        bool
	EventRedacted      bool
	ManifestVisibility string
}

func columnAccessFields(c gen.ColumnContext) accessFields {
	return accessFields{
		Access:             c.Access,
		APIReadable:        c.APIReadable,
		APIWritable:        c.APIWritable,
		APIFilterable:      c.APIFilterable,
		APISortable:        c.APISortable,
		EventRedacted:      c.EventRedacted,
		ManifestVisibility: c.ManifestVisibility,
	}
}

// TestBuildTableContexts_AccessResolution verifies the config→gen access
// resolution pass (PRD §32.2): each column_map.access role lands on the
// ColumnContext as the normalized role plus its capability booleans, an
// unset column defaults to public, and — the §32.1 load-bearing principle —
// the core column set itself is untouched (every column survives with its
// usual Go field, regardless of role).
func TestBuildTableContexts_AccessResolution(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "email", Type: "text"},
					{Name: "password_hash", Type: "text"},
					{Name: "created_at", Type: "timestamptz", Default: "now()"},
					{Name: "new_password", Type: "text", Nullable: true},
					{Name: "internal_score", Type: "bigint", Nullable: true},
				},
			},
		},
	}
	in := testInput(schema)
	in.Config.Tables["users"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			"email":          {Access: "public"},
			"password_hash":  {Access: "internal"},
			"created_at":     {Access: "read_only"},
			"new_password":   {Access: "write_only"},
			"internal_score": {Access: "hidden"},
			// id has no entry — must default to public.
		},
	}

	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("BuildTableContexts() returned %d contexts, want 1", len(contexts))
	}

	public := accessFields{
		Access: "public", APIReadable: true, APIWritable: true,
		APIFilterable: true, APISortable: true, ManifestVisibility: "public",
	}
	want := map[string]accessFields{
		"id":    public,
		"email": public,
		"password_hash": {
			Access: "internal", EventRedacted: true, ManifestVisibility: "internal",
		},
		"created_at": {
			Access: "read_only", APIReadable: true,
			APIFilterable: true, APISortable: true, ManifestVisibility: "read_only",
		},
		"new_password": {
			Access: "write_only", APIWritable: true,
			EventRedacted: true, ManifestVisibility: "write_only",
		},
		"internal_score": {
			Access: "hidden", ManifestVisibility: "hidden",
		},
	}

	cols := contexts[0].Columns
	if len(cols) != len(want) {
		t.Errorf("Columns count = %d, want %d — access must never drop a column from the core client", len(cols), len(want))
	}
	for _, col := range cols {
		w, ok := want[col.Name]
		if !ok {
			t.Errorf("unexpected column %q in context", col.Name)
			continue
		}
		if got := columnAccessFields(col); got != w {
			t.Errorf("column %q access fields = %+v, want %+v", col.Name, got, w)
		}
		if col.FieldName == "" || col.GoType == "" {
			t.Errorf("column %q lost core fields (FieldName=%q, GoType=%q) — access must not touch the Go surface", col.Name, col.FieldName, col.GoType)
		}
	}
}

// TestBuildViewContexts_AccessDefaultsPublic pins the view-side invariant:
// views have no column_map, so every view column must resolve to the public
// role with all-true API capabilities — not to zero-value (all-false)
// booleans, which would silently drop view columns from API surfaces.
func TestBuildViewContexts_AccessDefaultsPublic(t *testing.T) {
	schema := &parser.Schema{
		Views: []parser.View{
			{
				Name: "user_summaries",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "email", Type: "text"},
				},
			},
		},
	}
	in := testInput(schema)

	contexts, err := gen.BuildViewContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildViewContexts() error: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("BuildViewContexts() returned %d contexts, want 1", len(contexts))
	}

	public := accessFields{
		Access: "public", APIReadable: true, APIWritable: true,
		APIFilterable: true, APISortable: true, ManifestVisibility: "public",
	}
	for _, col := range contexts[0].Columns {
		if got := columnAccessFields(col); got != public {
			t.Errorf("view column %q access fields = %+v, want public defaults %+v", col.Name, got, public)
		}
	}
}
