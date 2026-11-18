package gen_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	sqlparser "github.com/teandresmith/sqlgen/parser"
)

// TestGenerate_SortersAreForTablesOnly generates a package holding a table, a
// view and (on PostgreSQL) a materialized view over the same columns, on
// every dialect. The sorter file declares a sorter for the table and none for
// either view, and each view's GetMany input takes []sql.Sort as the table's
// does.
func TestGenerate_SortersAreForTablesOnly(t *testing.T) {
	tests := []struct {
		name    string
		dialect config.Dialect
		ddl     string
		views   []sqlparser.View
	}{
		{
			name:    "postgres",
			dialect: config.DialectPostgres,
			ddl:     `CREATE TABLE members (id bigserial PRIMARY KEY, email text NOT NULL);`,
			views: []sqlparser.View{
				{Name: "active_members", Schema: "public", Columns: []sqlparser.Column{{Name: "id", Type: "bigint"}, {Name: "email", Type: "text"}}},
				{Name: "member_stats", Schema: "public", Materialized: true, Columns: []sqlparser.Column{{Name: "id", Type: "bigint"}, {Name: "email", Type: "text"}}},
			},
		},
		{
			name:    "mysql",
			dialect: config.DialectMySQL,
			ddl:     "CREATE TABLE members (id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, email VARCHAR(255) NOT NULL);",
			views: []sqlparser.View{
				{Name: "active_members", Columns: []sqlparser.Column{{Name: "id", Type: "bigint"}, {Name: "email", Type: "varchar(255)"}}},
			},
		},
		{
			name:    "sqlite",
			dialect: config.DialectSQLite,
			ddl:     `CREATE TABLE members (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL);`,
			views: []sqlparser.View{
				{Name: "active_members", Columns: []sqlparser.Column{{Name: "id", Type: "integer"}, {Name: "email", Type: "text"}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := parseDDLForTest(t, tt.dialect, tt.ddl)
			schema.Views = append(schema.Views, tt.views...)
			outDir := t.TempDir()
			cfg := loadGenerateConfig(t, outDir, tt.dialect, "")
			if _, err := gen.Generate(schema, cfg, "test"); err != nil {
				t.Fatalf("Generate() error: %v", err)
			}
			models := readGenFile(t, outDir, "models_gen.go")
			views := readGenFile(t, outDir, "views_gen.go")
			sorters := readGenFile(t, outDir, "sorter_gen.go")

			// The views must actually be generated, or the absence of
			// their sorters says nothing.
			viewInputs := []string{"GetActiveMembersInput"}
			if tt.dialect == config.DialectPostgres {
				viewInputs = append(viewInputs, "GetMemberStatsInput")
			}
			inputs := map[string]string{"GetMembersInput": models}
			for _, in := range viewInputs {
				inputs[in] = views
			}
			for _, input := range append([]string{"GetMembersInput"}, viewInputs...) {
				fields := gnwStructFields(t, inputs[input], input)
				if !strings.Contains(strings.Join(fields, "\n"), "Sorts []sql.Sort") {
					t.Errorf("%s fields = %v, want a Sorts []sql.Sort field", input, fields)
				}
			}

			var got []string
			for line := range strings.Lines(sorters) {
				if name, ok := strings.CutPrefix(line, "type "); ok {
					got = append(got, strings.TrimSpace(name))
				}
			}
			if diff := cmp.Diff([]string{"MemberSorter struct{}"}, got); diff != "" {
				t.Errorf("sorter_gen.go type declarations (-want +got):\n%s", diff)
			}
			for _, src := range []struct{ file, body string }{{"models_gen.go", models}, {"views_gen.go", views}, {"sorter_gen.go", sorters}} {
				for _, name := range []string{"ActiveMemberSorter", "ActiveMembersSorter", "MemberStatSorter", "MemberStatsSorter"} {
					if strings.Contains(src.body, name) {
						t.Errorf("%s mentions %s; views get no sorter", src.file, name)
					}
				}
			}
		})
	}
}
