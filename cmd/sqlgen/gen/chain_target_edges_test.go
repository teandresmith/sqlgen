package gen_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// A chained O2O join walks only the edges the target's own struct carries. A
// declared edge that takes over a detected one is chained under its own name,
// and an edge excluded by its base name is not chained at all (PRD §13.2,
// §13.4, §4.8). Chaining a dropped edge names a field options member the
// target lacks, and the generated package does not build.
func TestBuildO2OJoinDetails_ChainUsesTheTargetsEdges(t *testing.T) {
	tests := []struct {
		name     string
		ddl      string
		postsCfg config.TableConfig
		want     []string
	}{
		{
			name: "takeover",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE posts (id bigint PRIMARY KEY, author_id bigint UNIQUE,
	CONSTRAINT posts_author FOREIGN KEY (author_id) REFERENCES users (id));
CREATE TABLE post_details (post_id bigint PRIMARY KEY,
	CONSTRAINT post_details_post FOREIGN KEY (post_id) REFERENCES posts (id));
`,
			postsCfg: config.TableConfig{Relationships: []config.TableRelationship{
				{Name: "Writer", Type: "o2o", Table: "users", FK: config.KeyColumns{"author_id"}},
			}},
			want: []string{"fo.Post.Writer != nil"},
		},
		{
			name: "excluded by name",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE posts (id bigint PRIMARY KEY, author_id bigint UNIQUE, editor_id bigint UNIQUE,
	CONSTRAINT posts_author FOREIGN KEY (author_id) REFERENCES users (id),
	CONSTRAINT posts_editor FOREIGN KEY (editor_id) REFERENCES users (id));
CREATE TABLE post_details (post_id bigint PRIMARY KEY,
	CONSTRAINT post_details_post FOREIGN KEY (post_id) REFERENCES posts (id));
`,
			postsCfg: config.TableConfig{ExcludeRelationships: []string{"author"}},
			want:     []string{"fo.Post.Editor != nil"},
		},
		{
			name: "excluded by base name",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE posts (id bigint PRIMARY KEY, author_id bigint UNIQUE, editor_id bigint UNIQUE,
	CONSTRAINT posts_author FOREIGN KEY (author_id) REFERENCES users (id),
	CONSTRAINT posts_editor FOREIGN KEY (editor_id) REFERENCES users (id));
CREATE TABLE post_details (post_id bigint PRIMARY KEY,
	CONSTRAINT post_details_post FOREIGN KEY (post_id) REFERENCES posts (id));
`,
			postsCfg: config.TableConfig{ExcludeRelationships: []string{"users"}},
			want:     []string{},
		},
		{
			name: "omitted on a column field",
			ddl: `
CREATE TABLE writers (id bigint PRIMARY KEY);
CREATE TABLE posts (id bigint PRIMARY KEY, writer text, writer_id bigint UNIQUE,
	CONSTRAINT posts_writer FOREIGN KEY (writer_id) REFERENCES writers (id));
CREATE TABLE post_details (post_id bigint PRIMARY KEY,
	CONSTRAINT post_details_post FOREIGN KEY (post_id) REFERENCES posts (id));
`,
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, dialect := range allDialects {
				t.Run(string(dialect), func(t *testing.T) {
					schema := parseDDLForTest(t, dialect, tt.ddl)
					parser.DetectRelationships(schema)
					in := testInput(schema)
					in.Config.Input.Dialect = dialect
					in.Config.Tables["posts"] = tt.postsCfg
					contexts, err := gen.BuildTableContexts(in, nil)
					if err != nil {
						t.Fatalf("BuildTableContexts() error: %v", err)
					}
					posts, ok := tableContextByName(contexts, "posts")
					if !ok {
						t.Fatal("no posts context")
					}
					own := []string{}
					for _, r := range posts.Relationships {
						if r.Type == parser.OneToOne {
							own = append(own, "fo.Post."+r.FieldName+" != nil")
						}
					}
					details, ok := tableContextByName(contexts, "post_details")
					if !ok {
						t.Fatal("no post_details context")
					}
					d := o2oJoin(t, details, "Post")
					got := []string{}
					for _, c := range d.ChainedJoins {
						got = append(got, c.FieldOptionsCheck)
					}
					if diff := cmp.Diff(tt.want, got); diff != "" {
						t.Errorf("post_details → posts chained joins (-want +got):\n%s", diff)
					}
					if diff := cmp.Diff(own, got); diff != "" {
						t.Errorf("chained joins differ from posts' own O2O edges (-posts +chain):\n%s", diff)
					}
				})
			}
		})
	}
}
