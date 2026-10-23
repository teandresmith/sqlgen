package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// runCommand writes schema and cfg to a fresh directory and runs the given
// command there. It returns the directory, stderr and the command's error.
func runCommand(t *testing.T, command, schema, cfg string) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{"schema.sql": schema, "sqlgen.yml": cfg} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	t.Chdir(dir)
	_, stderr, err := executeCommand(command, "--config", filepath.Join(dir, "sqlgen.yml"))
	return dir, stderr, err
}

// generatedGo returns every generated .go file under dir/out, keyed by its
// path relative to it.
func generatedGo(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := filepath.Join(dir, "out")
	files := make(map[string]string)
	err := filepath.WalkDir(out, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		content, err := os.ReadFile(path) //nolint:gosec // test code, path is under the test's temp dir
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(out, path)
		files[rel] = string(content)
		return err
	})
	if err != nil {
		t.Fatalf("reading generated files: %v", err)
	}
	return files
}

const tenantConfig = `version: 1
input:
  dialect: postgres
  paths: [schema.sql]
output:
  dir: out
  package: models
`

// compositeKeySchema holds a composite O2M (tasks → cycles), a composite O2O
// (task_reports → tasks) reached through a single-column O2O
// (report_notes → task_reports), and a tenant junction (task_tags). fks is
// spliced in as each table's composite foreign keys, or left empty.
func compositeKeySchema(fks bool) string {
	fk := func(clause string) string {
		if !fks {
			return ""
		}
		return ",\n  " + clause
	}
	return `CREATE TABLE workspaces (id bigint PRIMARY KEY);
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), cycle_id bigint,
  UNIQUE (workspace_id, id)` + fk("FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id)") + `);
CREATE TABLE task_reports (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, task_id bigint NOT NULL UNIQUE` +
		fk("FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id)") + `);
CREATE TABLE report_notes (id bigint PRIMARY KEY, report_id bigint UNIQUE REFERENCES task_reports(id));
CREATE TABLE tags (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE task_tags (workspace_id bigint NOT NULL, task_id bigint NOT NULL, tag_id bigint NOT NULL,
  PRIMARY KEY (workspace_id, task_id, tag_id)` +
		fk("FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id)") +
		fk("FOREIGN KEY (workspace_id, tag_id) REFERENCES tags (workspace_id, id)") + `);`
}

// Detection finds edges through composite foreign keys, but the loaders, the
// relationship filter and the nested writers join one column per side, so
// generation omits every such edge, O2M, O2O (also when a chained O2O join
// reaches it) and M2M: the package is byte-identical to the one generated
// without those foreign keys.
func TestGenerate_CompositeKeyEdgeIsOmitted(t *testing.T) {
	withFK, stderr, err := runCommand(t, "generate", compositeKeySchema(true), tenantConfig)
	if err != nil {
		t.Fatalf("generate with the composite foreign keys: %v\nstderr: %s", err, stderr)
	}
	withoutFK, stderr, err := runCommand(t, "generate", compositeKeySchema(false), tenantConfig)
	if err != nil {
		t.Fatalf("generate without them: %v\nstderr: %s", err, stderr)
	}

	got, want := generatedGo(t, withFK), generatedGo(t, withoutFK)
	if len(want) == 0 {
		t.Fatal("generate wrote no .go files")
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("composite foreign keys changed the generated Go (-without +with):\n%s", diff)
	}
}

// A foreign key whose referenced columns are not a key of its target gives no
// relationship, and both validate and generate say so after the parser's own
// warnings (PRD §4.13).
func TestDetectionWarningIsPrinted(t *testing.T) {
	const schema = `CREATE TABLE workspaces (id INTEGER PRIMARY KEY, slug TEXT);
CREATE TABLE cycles (id INTEGER PRIMARY KEY, workspace_slug TEXT REFERENCES workspaces(slug));`
	const cfg = `version: 1
input:
  dialect: sqlite
  paths: [schema.sql]
output:
  driver: stdlib
  dir: out
  package: models
`
	const want = "Warning: tables.cycles: foreign key cycles(workspace_slug) → workspaces(slug) references columns that are neither the primary key nor a non-partial UNIQUE constraint of workspaces, so it gives no relationship; declare one by hand if you need it (PRD §5.3, §13.4)\n"
	for _, command := range []string{"validate", "generate"} {
		t.Run(command, func(t *testing.T) {
			_, stderr, err := runCommand(t, command, schema, cfg)
			if err != nil {
				t.Fatalf("%s: %v\nstderr: %s", command, err, stderr)
			}
			if strings.Count(stderr, want) != 1 {
				t.Errorf("%s stderr = %q, want the detection warning once", command, stderr)
			}
		})
	}
}

// Two detected edges between one pair can keep one name after disambiguation:
// here an O2M edge named from organization_id and the M2M edge named after
// its junction, organization_users. Generation fails naming the foreign keys
// behind both, and listing the name in exclude_relationships clears it.
func TestGenerate_DetectedNameCollisionIsAnError(t *testing.T) {
	const schema = `CREATE TABLE organizations (id bigint PRIMARY KEY);
CREATE TABLE users (id bigint PRIMARY KEY, organization_id bigint REFERENCES organizations(id));
CREATE TABLE organization_users (organization_id bigint REFERENCES organizations(id), user_id bigint REFERENCES users(id),
  PRIMARY KEY (organization_id, user_id));`

	_, stderr, err := runCommand(t, "generate", schema, tenantConfig)
	if err == nil {
		t.Fatal("generate succeeded, want the relationship name collision error")
	}
	const want = `tables.public.organizations.relationships: two detected relationships are both named "organization_users" after disambiguation, ` +
		`one from foreign key "users_organization_id_fkey" and one from foreign keys "organization_users_organization_id_fkey" and "organization_users_user_id_fkey"; ` +
		`list "organization_users" in ` + "`exclude_relationships`" + ` and declare both edges by hand (PRD §5.3, §13.4)`
	if !strings.Contains(stderr, want) {
		t.Errorf("generate stderr = %q, want it to contain %q", stderr, want)
	}

	_, stderr, err = runCommand(t, "generate", schema, tenantConfig+`tables:
  organizations:
    exclude_relationships: [organization_users]
`)
	if err != nil {
		t.Errorf("generate with the name excluded: %v\nstderr: %s", err, stderr)
	}
}
