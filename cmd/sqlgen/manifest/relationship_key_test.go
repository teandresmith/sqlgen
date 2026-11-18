package manifest_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/parser"
)

// relationshipFK builds ddl under dialect, lets edit adjust the contexts, and
// returns the fk of entity's relationship rel.
func relationshipFK(t *testing.T, dialect config.Dialect, ddl string, cfg *config.RootConfig, entity, rel string, edit func(manifest.BuildInput)) *manifest.FK {
	t.Helper()
	in := testBuildInput(t, parseDDL(t, dialect, ddl), cfg)
	if edit != nil {
		edit(in)
	}
	doc, err := manifest.Build(in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, r := range findEntity(t, doc, entity).Relationships {
		if r.Name == rel {
			return r.FK
		}
	}
	t.Fatalf("%s has no relationship %q", entity, rel)
	return nil
}

// TestBuild_RelationshipKeyShape pins PRD §30.4.2's fk shape: columns and
// references in the FK's declared order, and the constraint the edge came
// from, omitted for a manual relationship.
func TestBuild_RelationshipKeyShape(t *testing.T) {
	const usersPosts = `
CREATE TABLE users (id bigint PRIMARY KEY, email text NOT NULL UNIQUE);
CREATE TABLE posts (id bigint PRIMARY KEY, author_id bigint NOT NULL REFERENCES users(id));
`
	tests := []struct {
		name    string
		dialect config.Dialect
		ddl     string
		cfg     func(*config.RootConfig)
		entity  string
		rel     string
		edit    func(manifest.BuildInput)
		want    *manifest.FK
	}{
		{
			name:    "single-column key",
			dialect: config.DialectPostgres,
			ddl:     usersPosts,
			entity:  "User", rel: "posts",
			want: &manifest.FK{Table: "posts", Schema: "public", Columns: []string{"author_id"}, References: []string{"id"}, Constraint: "posts_author_id_fkey"},
		},
		{
			name:    "detected edge names its constraint",
			dialect: config.DialectPostgres,
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE posts (id bigint PRIMARY KEY, author_id bigint NOT NULL,
	CONSTRAINT posts_written_by FOREIGN KEY (author_id) REFERENCES users(id));
`,
			entity: "User", rel: "posts",
			want: &manifest.FK{Table: "posts", Schema: "public", Columns: []string{"author_id"}, References: []string{"id"}, Constraint: "posts_written_by"},
		},
		{
			name:    "key into a unique constraint",
			dialect: config.DialectPostgres,
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY, email text NOT NULL UNIQUE);
CREATE TABLE notes (id bigint PRIMARY KEY, author_email text NOT NULL REFERENCES users(email));
`,
			entity: "User", rel: "notes",
			want: &manifest.FK{Table: "notes", Schema: "public", Columns: []string{"author_email"}, References: []string{"email"}, Constraint: "notes_author_email_fkey"},
		},
		{
			name:    "unnamed sqlite key",
			dialect: config.DialectSQLite,
			ddl:     usersPosts,
			entity:  "User", rel: "posts",
			want: &manifest.FK{Table: "posts", Columns: []string{"author_id"}, References: []string{"id"}, Constraint: "posts(author_id) → users(id)"},
		},
		{
			name:    "manual relationship",
			dialect: config.DialectPostgres,
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE drafts (id bigint PRIMARY KEY, writer_id bigint NOT NULL);
`,
			cfg: func(cfg *config.RootConfig) {
				cfg.Tables["users"] = config.TableConfig{Relationships: []config.TableRelationship{
					{Name: "Drafts", Type: "o2m", Table: "drafts", FK: config.KeyColumns{"writer_id"}},
				}}
			},
			entity: "User", rel: "Drafts",
			want: &manifest.FK{Table: "drafts", Schema: "public", Columns: []string{"writer_id"}, References: []string{"id"}},
		},
		{
			// Composite-key edges stay out of the contexts until they
			// generate, so the pairs are set on the built context.
			name:    "composite key in declared order",
			dialect: config.DialectPostgres,
			ddl:     usersPosts,
			entity:  "User", rel: "posts",
			edit: func(in manifest.BuildInput) {
				for i := range in.Tables {
					for j := range in.Tables[i].Relationships {
						if r := &in.Tables[i].Relationships[j]; r.Name == "posts" {
							r.Key = []parser.KeyPair{{Local: "workspace_id", Referenced: "workspace_id"}, {Local: "author_id", Referenced: "id"}}
						}
					}
				}
			},
			want: &manifest.FK{Table: "posts", Schema: "public", Columns: []string{"workspace_id", "author_id"}, References: []string{"workspace_id", "id"}, Constraint: "posts_author_id_fkey"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := dialectConfig(tt.dialect)
			if tt.cfg != nil {
				tt.cfg(cfg)
			}
			got := relationshipFK(t, tt.dialect, tt.ddl, cfg, tt.entity, tt.rel, tt.edit)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("fk mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestEmitJSON_SchemaVersion pins PRD §30.4: the emitted document's
// schema_version is 0.2.0, the version that records each relationship's
// whole key.
func TestEmitJSON_SchemaVersion(t *testing.T) {
	doc := newFixtureDocument()
	dir := t.TempDir()
	if err := manifest.EmitJSON(doc, manifestCfg(config.JSONLayoutSingle), dir); err != nil {
		t.Fatalf("EmitJSON error: %v", err)
	}
	raw := readFile(t, filepath.Join(dir, "manifest", "manifest_gen.json"))
	if !strings.Contains(string(raw), `"schema_version": "0.2.0"`) {
		t.Errorf("manifest has no schema_version 0.2.0:\n%s", raw[:min(len(raw), 200)])
	}
}

// TestSchemaV1_RelationshipKeyRequiresColumnsAndReferences pins PRD §30.4.2:
// every `fk` and junction key carries `columns` and `references`, so a 0.1.0
// `{"column": …}` key no longer validates.
func TestSchemaV1_RelationshipKeyRequiresColumnsAndReferences(t *testing.T) {
	sch := compileSchema(t)
	key := manifest.JunctionKey{Columns: []string{"user_id"}, References: []string{"id"}}
	doc := newFixtureDocument()
	doc.Entities[0].Relationships = []manifest.Relationship{
		{Name: "Posts", Kind: "o2m", TargetEntity: "Post", FK: &manifest.FK{Table: "posts", Columns: key.Columns, References: key.References}},
		{Name: "Tags", Kind: "m2m", TargetEntity: "Tag", Junction: &manifest.Junction{Table: "user_tags", LocalFK: key, TargetFK: key}},
	}
	dir := t.TempDir()
	if err := manifest.EmitJSON(doc, manifestCfg(config.JSONLayoutSingle), dir); err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	emitted := readFile(t, filepath.Join(dir, "manifest", "manifest_gen.json"))
	if err := sch.Validate(decodeJSON(t, emitted)); err != nil {
		t.Fatalf("emitted manifest failed schema validation: %v", err)
	}

	tests := []struct {
		name string
		edit func(rels []any)
	}{
		{name: "fk without columns", edit: func(rels []any) { delete(fkOf(rels[0]), "columns") }},
		{name: "fk without references", edit: func(rels []any) { delete(fkOf(rels[0]), "references") }},
		{name: "0.1.0 fk column", edit: func(rels []any) {
			fk := fkOf(rels[0])
			delete(fk, "columns")
			delete(fk, "references")
			fk["column"] = "user_id"
		}},
		{name: "junction local_fk without columns", edit: func(rels []any) { delete(junctionKeyOf(rels[1], "local_fk"), "columns") }},
		{name: "junction target_fk without references", edit: func(rels []any) { delete(junctionKeyOf(rels[1], "target_fk"), "references") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := decodeJSON(t, emitted)
			entity := v.(map[string]any)["entities"].([]any)[0].(map[string]any)
			tt.edit(entity["relationships"].([]any))
			if err := sch.Validate(v); err == nil {
				t.Error("Validate() = nil, want an error")
			}
		})
	}
}

// fkOf is a decoded relationship's fk object.
func fkOf(rel any) map[string]any {
	return rel.(map[string]any)["fk"].(map[string]any)
}

// junctionKeyOf is one key of a decoded relationship's junction.
func junctionKeyOf(rel any, key string) map[string]any {
	return rel.(map[string]any)["junction"].(map[string]any)[key].(map[string]any)
}
