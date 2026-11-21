package gen_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// A detected to-one edge named after its referenced table, here because
// `user_ref` has no `_id` suffix, so its stem names its own column, is spelled
// singular on the struct and in its JSON tag, as a list edge is spelled plural
// (PRD §5.3 rule 5, §8.5). posts holds a non-unique key, so its edge is the
// belongs-to beside an O2M edge; profiles holds a unique one.
func TestBuildRelationshipContexts_DetectedToOneFieldIsSingular(t *testing.T) {
	const ddl = `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE posts (id bigint PRIMARY KEY, user_ref bigint,
	CONSTRAINT posts_user FOREIGN KEY (user_ref) REFERENCES users (id));
CREATE TABLE profiles (id bigint PRIMARY KEY, user_ref bigint UNIQUE,
	CONSTRAINT profiles_user FOREIGN KEY (user_ref) REFERENCES users (id));
`
	type field struct{ Name, FieldName, GoType, JSONTag string }
	for _, dialect := range allDialects {
		t.Run(string(dialect), func(t *testing.T) {
			schema := parseDDLForTest(t, dialect, ddl)
			parser.DetectRelationships(schema)
			in := testInput(schema)
			in.Config.Input.Dialect = dialect
			contexts, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts() error: %v", err)
			}
			for _, table := range []string{"posts", "profiles"} {
				i := slices.IndexFunc(contexts, func(tc gen.TableContext) bool { return tc.TableName == table })
				if i < 0 {
					t.Fatalf("no %s context", table)
				}
				var got []field
				for _, r := range contexts[i].Relationships {
					got = append(got, field{r.Name, r.FieldName, r.GoType, r.JSONTag})
				}
				want := []field{{"user", "User", "*User", "user"}}
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("%s relationships (-want +got):\n%s", table, diff)
				}
			}
		})
	}
}
