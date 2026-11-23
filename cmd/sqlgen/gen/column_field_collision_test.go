package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

const columnFieldCollisionDDL = `
CREATE TABLE users (id bigint PRIMARY KEY, posts text);
CREATE TABLE posts (id bigint PRIMARY KEY, user_id bigint,
	CONSTRAINT posts_user FOREIGN KEY (user_id) REFERENCES users (id));
`

// A detected edge whose Go field is a column's is omitted with a warning
// naming the column and the edge, so the column keeps the field (PRD §4.13).
func TestBuildRelationshipContexts_DetectedEdgeOnAColumnFieldIsOmitted(t *testing.T) {
	for _, dialect := range allDialects {
		t.Run(string(dialect), func(t *testing.T) {
			schema := parseDDLForTest(t, dialect, columnFieldCollisionDDL)
			parser.DetectRelationships(schema)
			in := testInput(schema)
			in.Config.Input.Dialect = dialect
			contexts, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts() error: %v", err)
			}
			users, ok := tableContextByName(contexts, "users")
			if !ok {
				t.Fatal("no users context")
			}
			for _, r := range users.Relationships {
				if r.FieldName == "Posts" {
					t.Errorf("users keeps relationship %q on the column's field %q", r.Name, r.FieldName)
				}
			}
			if !slices.ContainsFunc(users.Columns, func(c gen.ColumnContext) bool {
				return c.Name == "posts" && c.FieldName == "Posts"
			}) {
				t.Error("users has no column posts on the field Posts")
			}
			const want = `: the foreign-key-inferred relationship "posts" is omitted because its Go field "Posts" is the field of column "posts". ` +
				"Declare the edge in `relationships` under another name to keep it, or list \"posts\" in `exclude_relationships` to silence this (PRD §4.13, §13.4)."
			if !slices.ContainsFunc(in.Warnings, func(w string) bool {
				return strings.HasPrefix(w, "tables.") && strings.HasSuffix(w, "users"+want)
			}) {
				t.Errorf("warnings = %q, want one ending %q", in.Warnings, "users"+want)
			}
		})
	}
}

// A declared relationship that claims a column's Go field is the `column_map`
// collision error whether or not an override is present. Between two columns
// only a collision an override introduces is reported (PRD §4.13).
func TestValidateDuplicateFieldNames(t *testing.T) {
	tests := []struct {
		name     string
		ddl      string // columnFieldCollisionDDL when empty
		usersCfg config.TableConfig
		wantErr  string
	}{
		{
			name: "declared relationship on a column field",
			usersCfg: config.TableConfig{Relationships: []config.TableRelationship{
				{Name: "posts", Type: "o2m", Table: "posts", FK: config.KeyColumns{"user_id"}},
			}},
			wantErr: `.column_map: Go field name "Posts" is claimed by both column "posts" and relationship "posts" — rename one of them`,
		},
		{
			name: "declared relationship on its own field",
			usersCfg: config.TableConfig{Relationships: []config.TableRelationship{
				{Name: "writings", Type: "o2m", Table: "posts", FK: config.KeyColumns{"user_id"}},
			}},
		},
		{
			name: "column override onto a declared relationship's field",
			usersCfg: config.TableConfig{
				ColumnMap: map[string]config.ColumnOverride{"posts": {Name: "Writings"}},
				Relationships: []config.TableRelationship{
					{Name: "writings", Type: "o2m", Table: "posts", FK: config.KeyColumns{"user_id"}},
				},
			},
			wantErr: `.column_map: Go field name "Writings" is claimed by both column "posts" and relationship "writings" — rename one of them`,
		},
		{
			name: "two columns on one field with no override",
			ddl:  `CREATE TABLE users (id bigint PRIMARY KEY, user_id bigint, user__id bigint);`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, dialect := range allDialects {
				t.Run(string(dialect), func(t *testing.T) {
					ddl := tt.ddl
					if ddl == "" {
						ddl = columnFieldCollisionDDL
					}
					in := testInput(parseDDLForTest(t, dialect, ddl))
					in.Config.Input.Dialect = dialect
					in.Config.Tables["users"] = tt.usersCfg
					_, err := gen.BuildTableContexts(in, nil)
					if tt.wantErr == "" {
						if err != nil {
							t.Fatalf("BuildTableContexts() error: %v", err)
						}
						return
					}
					if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
						t.Errorf("BuildTableContexts() error = %v, want it to contain %q", err, tt.wantErr)
					}
				})
			}
		})
	}
}

// Declaring the edge under another name keeps it: the declared edge takes over
// the detected one by its key, so nothing is omitted and nothing warns (PRD
// §4.13, §13.4).
func TestBuildRelationshipContexts_DeclaredUnderAnotherNameKeepsTheEdge(t *testing.T) {
	for _, dialect := range allDialects {
		t.Run(string(dialect), func(t *testing.T) {
			schema := parseDDLForTest(t, dialect, columnFieldCollisionDDL)
			parser.DetectRelationships(schema)
			in := testInput(schema)
			in.Config.Input.Dialect = dialect
			in.Config.Tables["users"] = config.TableConfig{Relationships: []config.TableRelationship{
				{Name: "Writings", Type: "o2m", Table: "posts", FK: config.KeyColumns{"user_id"}},
			}}
			contexts, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts() error: %v", err)
			}
			users, ok := tableContextByName(contexts, "users")
			if !ok {
				t.Fatal("no users context")
			}
			var fields []string
			for _, r := range users.Relationships {
				fields = append(fields, r.FieldName)
			}
			if !slices.Equal(fields, []string{"Writings"}) {
				t.Errorf("users relationship fields = %v, want [Writings]", fields)
			}
			if slices.ContainsFunc(in.Warnings, func(w string) bool { return strings.Contains(w, "is omitted because its Go field") }) {
				t.Errorf("warnings = %q, want no collision warning", in.Warnings)
			}
		})
	}
}
