package gen_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// declaredKeyDDL has single-column keys on users, posts and tags, each named
// apart so a pairing shows which table it references, a code-keyed teams
// table the tests override, and the tables that link them.
const declaredKeyDDL = `
CREATE TABLE users (uid bigint PRIMARY KEY, name text NOT NULL);
CREATE TABLE posts (post_id bigint PRIMARY KEY, author_id bigint NOT NULL, title text NOT NULL);
CREATE TABLE tags (tag_id bigint PRIMARY KEY, label text NOT NULL);
CREATE TABLE user_tags (owner_id bigint NOT NULL, tag_ref bigint NOT NULL, PRIMARY KEY (owner_id, tag_ref));
CREATE TABLE teams (id bigint NOT NULL, code text NOT NULL, region text NOT NULL, PRIMARY KEY (id));
CREATE TABLE members (id bigint PRIMARY KEY, team_code text NOT NULL, team_region text NOT NULL);
`

// declaredKeyRelationship builds declaredKeyDDL with tables[table] declaring
// rel, and returns rel's context or the build error.
func declaredKeyRelationship(t *testing.T, table string, rel config.TableRelationship, edit func(*config.RootConfig)) (gen.RelationshipContext, error) {
	t.Helper()
	schema := parseDDLForTest(t, config.DialectPostgres, declaredKeyDDL)
	parser.DetectRelationships(schema)
	cfg := testInput(schema).Config
	cfg.Tables[table] = config.TableConfig{Relationships: []config.TableRelationship{rel}}
	if edit != nil {
		edit(cfg)
	}
	contexts, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		return gen.RelationshipContext{}, err
	}
	for _, tc := range contexts {
		if tc.TableName != table {
			continue
		}
		for _, r := range tc.Relationships {
			if r.Name == rel.Name {
				return r, nil
			}
		}
	}
	t.Fatalf("no relationship %q on %s", rel.Name, table)
	return gen.RelationshipContext{}, nil
}

// TestBuildRelationshipContexts_DeclaredKeyListMapsOntoPrimaryKey pins PRD
// §13.4: a declared key maps by position onto the primary key of the table it
// references, and a string is a one-item list.
func TestBuildRelationshipContexts_DeclaredKeyListMapsOntoPrimaryKey(t *testing.T) {
	type keys struct{ Key, JunctionLocal, JunctionReference []parser.KeyPair }
	tests := []struct {
		name  string
		table string
		rel   config.TableRelationship
		edit  func(*config.RootConfig)
		want  keys
	}{
		{
			name:  "o2m string references this table's key",
			table: "users",
			rel:   config.TableRelationship{Name: "Posts", Type: "o2m", Table: "posts", FK: config.KeyColumns{"author_id"}},
			want:  keys{Key: []parser.KeyPair{{Local: "author_id", Referenced: "uid"}}},
		},
		{
			name:  "belongs-to one-item list references the target's key",
			table: "posts",
			rel:   config.TableRelationship{Name: "Author", Type: "o2o", Table: "users", FK: config.KeyColumns{"author_id"}},
			want:  keys{Key: []parser.KeyPair{{Local: "author_id", Referenced: "uid"}}},
		},
		{
			name:  "m2m junction keys reference both keys",
			table: "users",
			rel: config.TableRelationship{
				Name: "Tags", Type: "m2m", Table: "tags", Junction: "user_tags",
				JunctionLocalFK: config.KeyColumns{"owner_id"}, JunctionReferenceFK: config.KeyColumns{"tag_ref"},
			},
			want: keys{
				JunctionLocal:     []parser.KeyPair{{Local: "owner_id", Referenced: "uid"}},
				JunctionReference: []parser.KeyPair{{Local: "tag_ref", Referenced: "tag_id"}},
			},
		},
		{
			name:  "a primary-key override is the referenced key",
			table: "members",
			rel:   config.TableRelationship{Name: "Team", Type: "o2o", Table: "teams", FK: config.KeyColumns{"team_code"}},
			edit: func(cfg *config.RootConfig) {
				cfg.Tables["teams"] = config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"code"}}}
			},
			want: keys{Key: []parser.KeyPair{{Local: "team_code", Referenced: "code"}}},
		},
		{
			name:  "has-one references this table's key",
			table: "teams",
			rel:   config.TableRelationship{Name: "Member", Type: "o2o", Table: "members", FK: config.KeyColumns{"team_code"}},
			edit: func(cfg *config.RootConfig) {
				tc := cfg.Tables["teams"]
				tc.PrimaryKey = &config.TablePrimaryKeyConfig{Columns: []string{"code"}}
				cfg.Tables["teams"] = tc
			},
			want: keys{Key: []parser.KeyPair{{Local: "team_code", Referenced: "code"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel, err := declaredKeyRelationship(t, tt.table, tt.rel, tt.edit)
			if err != nil {
				t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
			}
			got := keys{Key: rel.Key, JunctionLocal: rel.JunctionLocal, JunctionReference: rel.JunctionReference}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("keys mismatch (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("a composite key follows the PRIMARY KEY clause order", func(t *testing.T) {
		schema := parseDDLForTest(t, config.DialectPostgres,
			"CREATE TABLE cycles (workspace_id bigint NOT NULL, id bigint NOT NULL, PRIMARY KEY (id, workspace_id));")
		table := &schema.Tables[0]
		if diff := cmp.Diff([]string{"id", "workspace_id"}, gen.ResolvedPrimaryKeyForTest(table, config.TableConfig{})); diff != "" {
			t.Errorf("declared key order mismatch (-want +got):\n%s", diff)
		}
		override := config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"workspace_id", "id"}}}
		if diff := cmp.Diff([]string{"workspace_id", "id"}, gen.ResolvedPrimaryKeyForTest(table, override)); diff != "" {
			t.Errorf("override order mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a view target's key is its @pk", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			rel  config.TableRelationship
			want keys
		}{
			{
				name: "belongs-to",
				rel:  config.TableRelationship{Name: "Stats", Type: "o2o", Table: "post_stats", FK: config.KeyColumns{"author_id"}},
				want: keys{Key: []parser.KeyPair{{Local: "author_id", Referenced: "stat_id"}}},
			},
			{
				name: "m2m",
				rel: config.TableRelationship{
					Name: "Stats", Type: "m2m", Table: "post_stats", Junction: "user_tags",
					JunctionLocalFK: config.KeyColumns{"owner_id"}, JunctionReferenceFK: config.KeyColumns{"tag_ref"},
				},
				want: keys{
					JunctionLocal:     []parser.KeyPair{{Local: "owner_id", Referenced: "post_id"}},
					JunctionReference: []parser.KeyPair{{Local: "tag_ref", Referenced: "stat_id"}},
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				schema := parseDDLForTest(t, config.DialectPostgres, declaredKeyDDL)
				schema.Views = append(schema.Views, parser.View{Name: "post_stats", Columns: []parser.Column{
					{Name: "total", Type: "bigint"}, {Name: "stat_id", Type: "bigint", PrimaryKey: true},
				}})
				parser.DetectRelationships(schema)
				cfg := testInput(schema).Config
				cfg.Tables["posts"] = config.TableConfig{Relationships: []config.TableRelationship{tc.rel}}
				contexts, err := gen.BuildTableContextsFromSchema(schema, cfg)
				if err != nil {
					t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
				}
				for _, c := range contexts {
					for _, r := range c.Relationships {
						if c.TableName != "posts" || r.Name != tc.rel.Name {
							continue
						}
						got := keys{Key: r.Key, JunctionLocal: r.JunctionLocal, JunctionReference: r.JunctionReference}
						if diff := cmp.Diff(tc.want, got); diff != "" {
							t.Errorf("keys mismatch (-want +got):\n%s", diff)
						}
						return
					}
				}
				t.Fatalf("no relationship %q on posts", tc.rel.Name)
			})
		}
	})

	t.Run("a composite key pairs by position", func(t *testing.T) {
		got := gen.DeclaredKeyForTest(config.KeyColumns{"workspace_id", "setting_key"}, []string{"workspace_id", "key"})
		want := []parser.KeyPair{{Local: "workspace_id", Referenced: "workspace_id"}, {Local: "setting_key", Referenced: "key"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("pairs mismatch (-want +got):\n%s", diff)
		}
	})
}

// TestBuildRelationshipContexts_DeclaredCompositeKeyIsAnError is a staging
// guard: a declared key of more than one column is a generation error naming
// the relationship and the key until relationships through a composite key
// generate.
func TestBuildRelationshipContexts_DeclaredCompositeKeyIsAnError(t *testing.T) {
	rel := config.TableRelationship{Name: "Team", Type: "o2o", Table: "teams", FK: config.KeyColumns{"team_code", "team_region"}}
	_, err := declaredKeyRelationship(t, "members", rel, func(cfg *config.RootConfig) {
		cfg.Tables["teams"] = config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"code", "region"}}}
	})
	if err == nil {
		t.Fatal("BuildTableContextsFromSchema() = nil, want an error")
	}
	for _, want := range []string{`"Team"`, "fk: [team_code, team_region]", "a key of 2 columns", "not generated yet"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
}
