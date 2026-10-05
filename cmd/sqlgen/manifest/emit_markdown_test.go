package manifest_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
)

func TestEmitMarkdown_Goldens(t *testing.T) {
	doc := newFixtureDocument()
	dir := t.TempDir()

	if err := manifest.EmitMarkdown(doc, manifestCfg(config.JSONLayoutSingle), dir); err != nil {
		t.Fatalf("EmitMarkdown error: %v", err)
	}

	for _, name := range []string{"_index.md", "_conventions.md", "users.md", "active_users.md"} {
		got := readFile(t, filepath.Join(dir, "manifest", name))
		checkGolden(t, filepath.Join("testdata", "golden", "markdown", name), got)
	}
}

// featuresLineOf emits _index.md for a document whose generation_config matches gc
// and returns the "Features: ..." line.
func featuresLineOf(t *testing.T, gc manifest.GenerationConfig) string {
	t.Helper()
	doc := newFixtureDocument()
	doc.GenerationConfig = gc
	dir := t.TempDir()
	if err := manifest.EmitMarkdown(doc, manifestCfg(config.JSONLayoutSingle), dir); err != nil {
		t.Fatalf("EmitMarkdown error: %v", err)
	}
	index := string(readFile(t, filepath.Join(dir, "manifest", "_index.md")))
	for line := range strings.SplitSeq(index, "\n") {
		if strings.HasPrefix(line, "Features: ") {
			return line
		}
	}
	t.Fatalf("_index.md has no Features line:\n%s", index)
	return ""
}

func TestEmitMarkdown_FeaturesLine(t *testing.T) {
	allOn := manifest.GenerationConfig{
		AuditColumns: true, Cache: true, Events: true, GraphQL: true, GraphTopLevel: true,
		SoftDelete: true, Tenancy: true, Views: true,
	}
	// NB: the generation_config toggle once called "layered" is now
	// graph_top_level (PRD §30.4.3). Labels are the JSON field names,
	// alphabetically ordered.
	tests := []struct {
		name string
		gc   manifest.GenerationConfig
		want string
	}{
		{
			name: "all on",
			gc:   allOn,
			want: "Features: audit_columns, cache, events, graph_top_level, graphql, soft_delete, tenancy, views",
		},
		{
			name: "all off",
			gc:   manifest.GenerationConfig{},
			want: "Features: none",
		},
		{
			name: "mixed",
			gc:   manifest.GenerationConfig{Cache: true, Events: true, SoftDelete: true},
			want: "Features: cache, events, soft_delete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := featuresLineOf(t, tt.gc); got != tt.want {
				t.Errorf("Features line = %q, want %q", got, tt.want)
			}
		})
	}
}

// entityWithMethod returns a minimal single-method entity for exercising the
// Generated SQL rendering branch, with the query method's sql_bodies set to
// bodies (nil for the "no SQL" case).
func entityWithMethod(table string, bodies map[string]string) manifest.Entity {
	return manifest.Entity{
		Kind:       "table",
		Name:       "Widget",
		Table:      table,
		FilePrefix: table,
		Files:      []string{table + "_gen.go"},
		Indexes:    []manifest.Index{},
		PK:         manifest.PK{Kind: "single", Columns: []manifest.PKColumn{{Name: "id", GoField: "ID", GoType: "int64"}}},
		Columns: []manifest.Column{
			{Name: "id", GoField: "ID", GoType: "int64", DBType: "bigint", PK: true, Unique: true},
		},
		Relationships: []manifest.Relationship{},
		Methods: manifest.Methods{
			Query: []manifest.Method{
				{
					Name:      "Get",
					Params:    []manifest.MethodParam{{Name: "id", Type: "int64"}},
					Returns:   "*Widget",
					Errors:    []string{},
					SQLBodies: bodies,
				},
			},
			Mutation: []manifest.Method{},
		},
		Filter: manifest.Filter{Type: "WidgetFilter", Fields: []manifest.FilterField{}},
		Sort:   manifest.Sort{Type: "WidgetSort", Fields: []string{}},
	}
}

func renderEntityMarkdown(t *testing.T, e manifest.Entity) string {
	t.Helper()
	doc := newFixtureDocument()
	doc.Entities = []manifest.Entity{e}
	dir := t.TempDir()
	if err := manifest.EmitMarkdown(doc, manifestCfg(config.JSONLayoutSingle), dir); err != nil {
		t.Fatalf("EmitMarkdown error: %v", err)
	}
	return string(readFile(t, filepath.Join(dir, "manifest", e.FilePrefix+".md")))
}

func TestEmitMarkdown_GeneratedSQLBlock(t *testing.T) {
	t.Run("populated renders the block", func(t *testing.T) {
		md := renderEntityMarkdown(t, entityWithMethod("gadgets",
			map[string]string{"postgres": "SELECT * FROM \"gadgets\" WHERE \"id\" = $1"}))
		if !strings.Contains(md, "Generated SQL:") {
			t.Errorf("expected a Generated SQL heading; got:\n%s", md)
		}
		if !strings.Contains(md, "```sql") {
			t.Errorf("expected a ```sql fence; got:\n%s", md)
		}
		if !strings.Contains(md, "SELECT * FROM \"gadgets\" WHERE \"id\" = $1") {
			t.Errorf("expected the canonical SQL body; got:\n%s", md)
		}
	})

	t.Run("empty map omits the block cleanly", func(t *testing.T) {
		md := renderEntityMarkdown(t, entityWithMethod("gizmos", nil))
		if strings.Contains(md, "Generated SQL:") {
			t.Errorf("expected no Generated SQL heading for a method without sql_bodies; got:\n%s", md)
		}
		if strings.Contains(md, "```sql") {
			t.Errorf("expected no ```sql fence for a method without sql_bodies; got:\n%s", md)
		}
		// The method itself must still render.
		if !strings.Contains(md, "### Get") {
			t.Errorf("method entry missing; got:\n%s", md)
		}
	})

	t.Run("multi-dialect renders blocks alphabetically", func(t *testing.T) {
		md := renderEntityMarkdown(t, entityWithMethod("things", map[string]string{
			"postgres": "SELECT p",
			"mysql":    "SELECT m",
			"sqlite":   "SELECT s",
		}))
		if n := strings.Count(md, "```sql"); n != 3 {
			t.Fatalf("expected 3 ```sql fences, got %d; md:\n%s", n, md)
		}
		mysql := strings.Index(md, "mysql:")
		postgres := strings.Index(md, "postgres:")
		sqlite := strings.Index(md, "sqlite:")
		if mysql >= postgres || postgres >= sqlite {
			t.Errorf("dialect blocks not alphabetical: mysql=%d postgres=%d sqlite=%d\n%s",
				mysql, postgres, sqlite, md)
		}
	})
}

// TestEmitMarkdown_MultiSchemaFilename pins the per-entity filename form: the
// emitter writes <Entity.FilePrefix>.md verbatim, so a schema-qualified prefix
// (public_users, the <schema>_<table> form the builder produces for multi-schema
// packages per PRD §30.3) round-trips to public_users.md — mirroring the Go file
// naming — while a bare prefix stays users.md.
func TestEmitMarkdown_MultiSchemaFilename(t *testing.T) {
	doc := newFixtureDocument()
	e := usersEntity()
	e.Schema = "public"
	e.FilePrefix = "public_users"
	doc.Entities = []manifest.Entity{e}

	dir := t.TempDir()
	if err := manifest.EmitMarkdown(doc, manifestCfg(config.JSONLayoutSingle), dir); err != nil {
		t.Fatalf("EmitMarkdown error: %v", err)
	}

	if got := readFile(t, filepath.Join(dir, "manifest", "public_users.md")); len(got) == 0 {
		t.Fatal("expected non-empty public_users.md")
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest", "users.md")); !os.IsNotExist(err) {
		t.Errorf("expected no bare users.md for a schema-qualified entity; stat err = %v", err)
	}
}

// TestEmitMarkdown_FKCell pins the relationships table's FK cell: an FK or a
// junction with a schema renders schema-qualified, so same-named tables in two
// schemas stay distinguishable; one without stays bare.
func TestEmitMarkdown_FKCell(t *testing.T) {
	tests := []struct {
		name string
		rel  manifest.Relationship
		want string
	}{
		{
			name: "junction with a schema renders qualified",
			rel:  manifest.Relationship{Name: "tags", Kind: "m2m", TargetEntity: "Tag", Junction: &manifest.Junction{Table: "document_labels", Schema: "audit", LocalFK: "document_id", TargetFK: "tag_id"}},
			want: "| tags | m2m | Tag | audit.document_labels (document_id → tag_id) |  |",
		},
		{
			name: "junction without a schema renders bare",
			rel:  manifest.Relationship{Name: "tags", Kind: "m2m", TargetEntity: "Tag", Junction: &manifest.Junction{Table: "document_labels", LocalFK: "document_id", TargetFK: "tag_id"}},
			want: "| tags | m2m | Tag | document_labels (document_id → tag_id) |  |",
		},
		{
			name: "fk with a schema renders qualified",
			rel:  manifest.Relationship{Name: "notes", Kind: "o2m", TargetEntity: "Note", FK: &manifest.FK{Table: "document_notes", Schema: "audit", Column: "document_id"}},
			want: "| notes | o2m | Note | audit.document_notes.document_id |  |",
		},
		{
			name: "fk without a schema renders bare",
			rel:  manifest.Relationship{Name: "notes", Kind: "o2m", TargetEntity: "Note", FK: &manifest.FK{Table: "document_notes", Column: "document_id"}},
			want: "| notes | o2m | Note | document_notes.document_id |  |",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := entityWithMethod("widgets", nil)
			e.Relationships = []manifest.Relationship{tt.rel}
			md := renderEntityMarkdown(t, e)
			if !strings.Contains(md, tt.want) {
				t.Errorf("EmitMarkdown relationships row missing %q; got:\n%s", tt.want, md)
			}
		})
	}
}

// TestEmitMarkdown_Examples exercises the per-entity Read/Write example blocks
// (entity.md.tmpl's `{{- if .Examples }}` branch), which the shared fixture
// leaves nil. It also guards normalizeMarkdown's fence-awareness: an intentional
// blank line inside a multi-line ```go example body must survive verbatim rather
// than being collapsed as inter-block whitespace.
func TestEmitMarkdown_Examples(t *testing.T) {
	t.Run("read and write render both blocks", func(t *testing.T) {
		e := entityWithMethod("widgets", nil)
		e.Examples = &manifest.Examples{
			Read: []string{
				"w, err := c.Widgets().Get(ctx, id)",
				// Two *consecutive* blank lines: normalizeMarkdown collapses a run
				// of blanks to one — but only outside a fence. Two blanks is the
				// only input that distinguishes the fence-aware path (keeps both)
				// from a regression that dropped the inFence guard (collapses to
				// one). A single blank would survive either way.
				"",
				"",
				"list, err := c.Widgets().GetMany(ctx, nil)",
			},
			Write: []string{"w, err := c.M.Widget().Create(ctx, input)"},
		}
		md := renderEntityMarkdown(t, e)

		for _, want := range []string{"## Read examples", "## Write examples"} {
			if !strings.Contains(md, want) {
				t.Errorf("expected %q heading; got:\n%s", want, md)
			}
		}
		if n := strings.Count(md, "```go"); n != 2 {
			t.Fatalf("expected 2 ```go fences (read + write), got %d; md:\n%s", n, md)
		}
		if !strings.Contains(md, "w, err := c.M.Widget().Create(ctx, input)") {
			t.Errorf("write example line missing; got:\n%s", md)
		}
		// Both blank lines between the two read lines must survive inside the
		// fence (\n\n\n = two blank lines). If normalizeMarkdown's inFence guard
		// regressed, the consecutive blanks would collapse to one (\n\n) and this
		// assertion would fail — which is exactly the fence-awareness it guards.
		wantBody := "w, err := c.Widgets().Get(ctx, id)\n\n\nlist, err := c.Widgets().GetMany(ctx, nil)"
		if !strings.Contains(md, wantBody) {
			t.Errorf("read example body not preserved verbatim (fence-internal blank lines collapsed?); want to contain:\n%q\ngot:\n%s", wantBody, md)
		}
	})

	t.Run("read only omits the write block", func(t *testing.T) {
		e := entityWithMethod("gadgets", nil)
		e.Examples = &manifest.Examples{Read: []string{"g, err := c.Gadgets().Get(ctx, id)"}}
		md := renderEntityMarkdown(t, e)

		if !strings.Contains(md, "## Read examples") {
			t.Errorf("expected Read examples heading; got:\n%s", md)
		}
		if strings.Contains(md, "## Write examples") {
			t.Errorf("expected no Write examples heading when Write is empty; got:\n%s", md)
		}
		if n := strings.Count(md, "```go"); n != 1 {
			t.Errorf("expected exactly 1 ```go fence, got %d; md:\n%s", n, md)
		}
	})
}

func TestEmitMarkdown_Determinism(t *testing.T) {
	doc := newFixtureDocument()
	dir1, dir2 := t.TempDir(), t.TempDir()

	if err := manifest.EmitMarkdown(doc, manifestCfg(config.JSONLayoutSingle), dir1); err != nil {
		t.Fatalf("EmitMarkdown first pass: %v", err)
	}
	if err := manifest.EmitMarkdown(doc, manifestCfg(config.JSONLayoutSingle), dir2); err != nil {
		t.Fatalf("EmitMarkdown second pass: %v", err)
	}

	for _, name := range []string{"_index.md", "_conventions.md", "users.md", "active_users.md"} {
		a := readFile(t, filepath.Join(dir1, "manifest", name))
		b := readFile(t, filepath.Join(dir2, "manifest", name))
		if !bytes.Equal(a, b) {
			t.Errorf("EmitMarkdown %s not byte-deterministic across runs", name)
		}
	}
}
