package cli

import (
	"io/fs"
	"os"
	"os/exec"
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
// spliced in as each table's composite foreign keys, or left empty. The keys
// are uuid, which has no increment operator, so the foreign keys change only
// the edges.
func compositeKeySchema(fks bool) string {
	fk := func(clause string) string {
		if !fks {
			return ""
		}
		return ",\n  " + clause
	}
	return `CREATE TABLE workspaces (id uuid PRIMARY KEY);
CREATE TABLE cycles (id uuid PRIMARY KEY, workspace_id uuid NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE tasks (id uuid PRIMARY KEY, workspace_id uuid NOT NULL REFERENCES workspaces(id), cycle_id uuid,
  UNIQUE (workspace_id, id)` + fk("FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id)") + `);
CREATE TABLE task_reports (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, task_id uuid NOT NULL UNIQUE` +
		fk("FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id)") + `);
CREATE TABLE report_notes (id uuid PRIMARY KEY, report_id uuid UNIQUE REFERENCES task_reports(id));
CREATE TABLE tags (id uuid PRIMARY KEY, workspace_id uuid NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE task_tags (workspace_id uuid NOT NULL, task_id uuid NOT NULL, tag_id uuid NOT NULL,
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

// An edge joins the column its foreign key references, which need not be the
// parent's primary key: a UNIQUE column, or the declared key once
// `primary_key` overrides it. The loader collects that column off each parent,
// the parent's narrowed projection keeps it, the relationship filter and the
// O2O join correlate on it, the nested writers copy it into the key, and the
// generated package builds.
func TestGenerate_EdgeJoinsReferencedColumns(t *testing.T) {
	// Nested writers copy the referenced column into the key too.
	const nestedOn = `generation:
  nested_mutations:
    enabled: true
`
	tests := []struct {
		name, schema, tables string
		// loader is the parent's loadRelationships header; wants are lines it
		// and the parent's GetMany must contain.
		loader string
		wants  []string
	}{
		{
			name: "fk to a non-PK unique column",
			schema: `CREATE TABLE users (id integer PRIMARY KEY, email text NOT NULL UNIQUE);
CREATE TABLE invites (id integer PRIMARY KEY, email text REFERENCES users(email));
CREATE TABLE badges (id integer PRIMARY KEY, owner_email text UNIQUE REFERENCES users(email));`,
			loader: "func (c *userClient) loadRelationships(",
			wants: []string{
				"userIDs[idx] = u.Email",
				"Email: &comparator.NullableID{ID: comparator.ID{In: userIDs}}",
				"byFK[users[i].Email]",
				`columns = unionColumns(columns, "email")`,
				`return sql.Exists("email", sql.Subquery{`,
				`On:    fmt.Sprintf("u.%s = b.%s", d.QuoteIdentifier("email"), d.QuoteIdentifier("owner_email")),`,
				"(*cur) == parent.Email:",
			},
		},
		{
			name: "m2m junction into a non-PK unique column",
			schema: `CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE tags (id bigint PRIMARY KEY, legacy_id bigint NOT NULL UNIQUE);
CREATE TABLE user_tags (user_id bigint NOT NULL REFERENCES users(id), tag_legacy_id bigint NOT NULL REFERENCES tags(legacy_id),
  PRIMARY KEY (user_id, tag_legacy_id));`,
			loader: "func (c *tagClient) loadRelationships(",
			wants: []string{
				"tagIDs[idx] = t.LegacyID",
				`columns = unionColumns(columns, "legacy_id")`,
				"parentsByTarget[fmt.Sprint(t.LegacyID)]",
				`requiredColumns: []string{"legacy_id"},`,
			},
		},
		{
			name: "fk into the declared key under a primary-key override",
			schema: `CREATE TABLE cycles (id integer PRIMARY KEY, code text NOT NULL UNIQUE);
CREATE TABLE tasks (id integer PRIMARY KEY, cycle_id integer REFERENCES cycles(id));`,
			tables: `tables:
  cycles:
    primary_key:
      columns: [code]
`,
			loader: "func (c *cycleClient) loadRelationships(",
			wants: []string{
				"cycleIDs[idx] = c.ID",
				"CycleID: &comparator.NullableNumber[int32]{Number: comparator.Number[int32]{In: cycleIDs}}",
				`columns = unionColumns(columns, "id")`,
				`return sql.Exists("id", sql.Subquery{`,
				"(*cur) == parent.ID:",
			},
		},
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, stderr, err := runCommand(t, "generate", tt.schema, tenantConfig+nestedOn+tt.tables)
			if err != nil {
				t.Fatalf("generate: %v\nstderr: %s", err, stderr)
			}
			src := generatedGo(t, dir)["models_gen.go"]
			start := strings.Index(src, tt.loader)
			if start < 0 {
				t.Fatalf("models_gen.go has no %q", tt.loader)
			}
			for _, want := range tt.wants {
				if !strings.Contains(src, want) {
					t.Errorf("models_gen.go is missing %q; the parent's loader:\n%s", want, src[start:min(start+4000, len(src))])
				}
			}
			if testing.Short() && os.Getenv("SQLGEN_BUILD_GENERATED") == "" {
				return
			}
			buildGenerated(t, root, dir)
		})
	}
}

// buildGenerated builds the package generate wrote under dir/out as a consumer
// module would, against the runtime of the checkout at root. Callers skip it
// under -short unless SQLGEN_BUILD_GENERATED is set, which `make
// build-generated` (part of `make check-examples`) does.
func buildGenerated(t *testing.T, root, dir string) {
	t.Helper()
	goSum, err := os.ReadFile(filepath.Join(root, "go.sum")) //nolint:gosec // the repo's own go.sum
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "go.mod", "module example.com/app\n\ngo 1.27.0\n\n"+
		"require github.com/teandresmith/sqlgen v0.0.0\n\n"+
		"replace github.com/teandresmith/sqlgen => "+filepath.ToSlash(root)+"\n")
	writeFile(t, dir, "go.sum", string(goSum))
	build := exec.CommandContext(t.Context(), "go", "build", "./...")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
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
