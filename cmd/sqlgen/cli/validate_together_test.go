package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidate_ReportsEachGenerationRuleWithTheOthers pins PRD §4.13's
// "all validation errors are reported together" for the resolution-level
// rules: `sqlgen validate` reports each one in the same run as an unrelated
// error, a column whose derived Go field name the generated code reserves.
func TestValidate_ReportsEachGenerationRuleWithTheOthers(t *testing.T) {
	const partnerSQL = "CREATE TABLE catalogs (id serial PRIMARY KEY, columns text NOT NULL);\n"
	const partner = `resolves to Go field name "Columns"`

	tests := []struct {
		name    string
		schema  string
		views   string // an input.views annotation file, when set
		extra   string
		want    []string
		notWant []string
	}{
		{
			name:   "struct_name override colliding with a derived name",
			schema: "CREATE TABLE users (id serial PRIMARY KEY);\nCREATE TABLE accounts (id serial PRIMARY KEY);",
			extra:  "tables:\n  accounts:\n    struct_name: User\n",
			want:   []string{`Go type name "User"`},
		},
		{
			name: "nested mutation name colliding with a table",
			schema: "CREATE TABLE users (id serial PRIMARY KEY);\n" +
				"CREATE TABLE events (id serial PRIMARY KEY, user_id integer NOT NULL REFERENCES users(id));\n" +
				"CREATE TABLE user_events_create_nesteds (id serial PRIMARY KEY);",
			want: []string{`the Go type name "UserEventsCreateNested"`},
		},
		{
			name: "discriminator value the read cannot compare",
			schema: "CREATE TABLE assets (id serial PRIMARY KEY);\n" +
				"CREATE TABLE documents (id serial PRIMARY KEY, asset_id integer NOT NULL, tag json NOT NULL);",
			extra: "tables:\n  assets:\n    relationships:\n      - name: Pinned\n        type: o2m\n        table: documents\n        fk: asset_id\n" +
				"        discriminator:\n          column: tag\n          value: pinned\n",
			want: []string{`"Pinned" declares discriminator tag = "pinned", but`},
		},
		{
			name:   "every declared relationship into a missing target",
			schema: "CREATE TABLE users (id serial PRIMARY KEY);",
			extra: "tables:\n  users:\n    relationships:\n" +
				"      - name: Ghosts\n        type: o2m\n        table: ghosts\n        fk: user_id\n" +
				"      - name: Phantoms\n        type: o2m\n        table: phantoms\n        fk: user_id\n",
			want: []string{
				`"Ghosts" targets "ghosts", which generates no entity`,
				`"Phantoms" targets "phantoms", which generates no entity`,
			},
		},
		{
			name: "declared relationship through an excluded link column",
			schema: "CREATE TABLE owners (id serial PRIMARY KEY);\n" +
				"CREATE TABLE pets (id serial PRIMARY KEY, owner_id integer NOT NULL);",
			extra: "tables:\n  owners:\n    relationships:\n      - name: Pets\n        type: o2m\n        table: pets\n        fk: owner_id\n" +
				"  pets:\n    exclude_columns: [owner_id]\n",
			want: []string{"which `exclude_columns` removes"},
		},
		{
			name:   "primary key that cannot key a Go map",
			schema: "CREATE TABLE things (tags text[] PRIMARY KEY, name text NOT NULL);",
			want:   []string{`primary-key column "tags" of table public.things has Go type []string, which cannot key a Go map`},
		},
		{
			name: "declared relationship through a link column that cannot key a Go map",
			schema: "CREATE TABLE owners (id serial PRIMARY KEY);\n" +
				"CREATE TABLE pets (id serial PRIMARY KEY, owner_tags text[]);",
			extra: "tables:\n  owners:\n    relationships:\n      - name: Pets\n        type: o2m\n        table: pets\n        fk: owner_tags\n",
			want:  []string{`"Pets" links through public.pets.owner_tags, whose Go type []string cannot key a Go map`},
		},
		{
			name: "two detected relationships sharing a name",
			schema: "CREATE TABLE organizations (id bigint PRIMARY KEY);\n" +
				"CREATE TABLE users (id bigint PRIMARY KEY, organization_id bigint REFERENCES organizations(id));\n" +
				"CREATE TABLE organization_users (organization_id bigint REFERENCES organizations(id), user_id bigint REFERENCES users(id),\n" +
				"  PRIMARY KEY (organization_id, user_id));",
			want: []string{`two detected relationships are both named "organization_users"`},
		},
		{
			name: "relationship key list of the wrong length",
			schema: "CREATE TABLE cycles (workspace_id bigint, id bigint, PRIMARY KEY (workspace_id, id));\n" +
				"CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, cycle_id bigint NOT NULL);",
			extra: "tables:\n  cycles:\n    relationships:\n      - name: Tasks\n        type: o2m\n        table: tasks\n        fk: cycle_id\n",
			want:  []string{`("Tasks").fk: cycle_id has 1 column, but it references the primary key of "cycles"`},
		},
		{
			name: "relationship key into a view",
			schema: "CREATE TABLE cycles (id bigint PRIMARY KEY, name text NOT NULL);\n" +
				"CREATE TABLE tasks (id bigint PRIMARY KEY, cycle_id bigint NOT NULL);",
			views: "CREATE VIEW cycle_names AS SELECT c.id AS cycle_ref, c.name FROM cycles c;\n",
			extra: "tables:\n  tasks:\n    relationships:\n      - name: CycleName\n        type: o2o\n        table: cycle_names\n        fk: cycle_id\n",
			want:  []string{`("CycleName").fk: references view "cycle_names"`},
		},
		{
			name:   "column name override colliding with another field",
			schema: "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL, label text NOT NULL);",
			extra:  "tables:\n  users:\n    column_map:\n      label:\n        name: Name\n",
			want:   []string{"is claimed by both"},
		},
		{
			name:   "package-qualified column type with no import",
			schema: "CREATE TABLE sessions (id serial PRIMARY KEY, ip_addr text NOT NULL);",
			extra:  "tables:\n  sessions:\n    column_map:\n      ip_addr:\n        type: netaddr.IP\n",
			want:   []string{"tables.public.sessions.column_map.ip_addr.type"},
		},
		{
			name:   "package-qualified type_map value",
			schema: "CREATE TABLE sessions (id serial PRIMARY KEY, amount numeric NOT NULL);",
			extra:  "tables:\n  sessions:\n    type_map:\n      amount: money.Amount\n",
			want:   []string{"tables.public.sessions.type_map.amount"},
		},
		{
			name: "two UUID libraries in one package",
			schema: "CREATE TABLE users (id uuid PRIMARY KEY);\n" +
				"CREATE TABLE orders (id uuid PRIMARY KEY);",
			extra: "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/google/uuid\n" +
				"tables:\n  orders:\n    overrides:\n      types:\n        uuid:\n          type: uuid.UUID\n          import: github.com/gofrs/uuid/v5\n",
			want: []string{"UUID libraries resolved in one generated package"},
		},
		{
			name: "nested mutation allowlist naming no relationship",
			schema: "CREATE TABLE users (id serial PRIMARY KEY);\n" +
				"CREATE TABLE events (id serial PRIMARY KEY, user_id integer NOT NULL REFERENCES users(id));",
			extra: "generation:\n  nested_mutations:\n    enabled: true\n" +
				"tables:\n  users:\n    nested_mutations:\n      relationships:\n        - name: Ghosts\n",
			want: []string{`"Ghosts" names no relationship on this table`},
		},
		{
			// The listed edge's own target fails to build. Its error is the
			// one to report, not "not in the generated set".
			name: "nested mutation allowlist edge into a table that fails",
			schema: "CREATE TABLE users (id serial PRIMARY KEY);\n" +
				"CREATE TABLE shelves (id serial PRIMARY KEY, columns text NOT NULL, user_id integer NOT NULL REFERENCES users(id));",
			extra: "generation:\n  nested_mutations:\n    enabled: true\n" +
				"tables:\n  users:\n    nested_mutations:\n      relationships:\n        - name: Shelves\n",
			want:    []string{"generated ShelfFieldOptions"},
			notWant: []string{"not write-eligible"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "sqlgen.yml")
			sqlDir := filepath.Join(dir, "migrations")
			if err := os.MkdirAll(sqlDir, 0o750); err != nil {
				t.Fatalf("creating migrations dir: %v", err)
			}
			writeValidateConfig(t, cfgPath, sqlDir, filepath.Join(dir, "output"), tt.extra)
			writeSQLFile(t, sqlDir, partnerSQL+tt.schema)
			if tt.views != "" {
				addInputViews(t, cfgPath, filepath.Join(dir, "views"), tt.views)
			}

			stdout, stderr, err := executeCommand("validate", "--config", cfgPath)
			if err == nil {
				t.Fatalf("validate returned nil error; stdout = %q", stdout)
			}
			for _, want := range append(tt.want, partner) {
				if !strings.Contains(stderr, want) {
					t.Errorf("validate stderr = %q, want it to contain %q", stderr, want)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(stderr, notWant) {
					t.Errorf("validate stderr = %q, want no %q", stderr, notWant)
				}
			}
		})
	}
}

// addInputViews writes view as an annotation file under viewDir and lists the
// directory in the config's input.views.
func addInputViews(t *testing.T, cfgPath, viewDir, view string) {
	t.Helper()
	if err := os.MkdirAll(viewDir, 0o750); err != nil {
		t.Fatalf("creating views dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(viewDir, "view.sql"), []byte(view), 0o600); err != nil {
		t.Fatalf("writing view file: %v", err)
	}
	content, err := os.ReadFile(cfgPath) //nolint:gosec // test code, path is from controlled test dir
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	updated := strings.Replace(string(content), "output:\n", "  views:\n    - \""+viewDir+"\"\noutput:\n", 1)
	if err := os.WriteFile(cfgPath, []byte(updated), 0o600); err != nil { //nolint:gosec // test code, path is from controlled test dir
		t.Fatalf("writing config: %v", err)
	}
}
