package cli

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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

// An edge through a composite foreign key is generated, O2M, O2O (also when a
// chained O2O join reaches it) and M2M through a tenant junction, and the
// package builds.
func TestGenerate_CompositeKeyEdgeIsGenerated(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	dir, stderr, err := runCommand(t, "generate", compositeKeySchema(true), tenantConfig)
	if err != nil {
		t.Fatalf("generate: %v\nstderr: %s", err, stderr)
	}
	src := strings.Join(slices.Sorted(maps.Values(generatedGo(t, dir))), "\n")
	tests := []struct {
		name  string
		wants []string
	}{
		{"composite o2m", []string{
			"func (c *cycleClient) loadRelationships(",
			`sql.BuildCompositePKBatchCondition(c.dialect, []string{"workspace_id", "cycle_id"}, chunk),`,
		}},
		{"composite o2o", []string{
			"func (c *taskReportClient) resolveO2OJoins(",
			`fmt.Sprintf(" AND t2.%s = t.%s", d.QuoteIdentifier("id"), d.QuoteIdentifier("task_id"))`,
		}},
		{"chained o2o", []string{
			`fmt.Sprintf("r2.%s = r.%s", d.QuoteIdentifier("id"), d.QuoteIdentifier("report_id"))`,
		}},
		{"tenant junction m2m", []string{
			`Columns: []string{"workspace_id", "task_id"},`,
			`" AND t." + c.dialect.QuoteIdentifier("id") + " = j." + c.dialect.QuoteIdentifier("tag_id"),`,
			`sql.TupleInSubquery(c.dialect, []string{"workspace_id", "id"}, sql.Subquery{`,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, want := range tt.wants {
				if !strings.Contains(src, want) {
					t.Errorf("generated Go is missing %q", want)
				}
			}
		})
	}
	// The second hop's join lives in the first table's own resolver, so the
	// assertion cannot match task_reports' direct join of the same pair.
	t.Run("chained o2o joins every pair of the second hop", func(t *testing.T) {
		_, body, ok := strings.Cut(src, "func (c *reportNoteClient) resolveO2OJoins(")
		if !ok {
			t.Fatal("generated Go has no report_notes resolver")
		}
		body, _, _ = strings.Cut(body, "\n}\n")
		want := `fmt.Sprintf(" AND t.%s = r2.%s", d.QuoteIdentifier("id"), d.QuoteIdentifier("task_id"))`
		if !strings.Contains(body, want) {
			t.Errorf("report_notes resolver is missing %q:\n%s", want, body)
		}
	})
	if testing.Short() && os.Getenv("SQLGEN_BUILD_GENERATED") == "" {
		return
	}
	buildGenerated(t, root, dir)
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
				"key := users[i].Email",
				`columns = unionColumns(columns, "email")`,
				`return sql.Exists("email", sql.Subquery{`,
				`On:    fmt.Sprintf("u.%s = b.%s", d.QuoteIdentifier("email"), d.QuoteIdentifier("owner_email")),`,
				"(*cur) == parent.Email:",
			},
		},
		{
			name: "fk to a nullable unique column",
			schema: `CREATE TABLE teams (id integer PRIMARY KEY, code text UNIQUE);
CREATE TABLE members (id integer PRIMARY KEY, team_code text REFERENCES teams(code));
CREATE TABLE labels (id integer PRIMARY KEY, code text UNIQUE);
CREATE TABLE team_labels (team_code text NOT NULL REFERENCES teams(code), label_code text NOT NULL REFERENCES labels(code),
  PRIMARY KEY (team_code, label_code));`,
			loader: "func (c *teamClient) loadRelationships(",
			wants: []string{
				"if !(t.Code != nil) {",
				`sql.BuildCompositePKBatchCondition(c.dialect, []string{"team_code"}, chunk),`,
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

// A composite-key edge's nested writers compile on every dialect and layout,
// with text, enum, timestamp and boolean key members, whose comparators differ
// from an integer's and take the member's own type or a string.
func TestGenerate_CompositeKeyNestedWritersCompile(t *testing.T) {
	const schema = `CREATE TABLE cycles (id integer PRIMARY KEY, workspace_id integer NOT NULL, slug varchar(40) NOT NULL,
  UNIQUE (workspace_id, id), UNIQUE (slug, id));
CREATE TABLE tasks (id integer PRIMARY KEY, workspace_id integer NOT NULL, cycle_id integer, title varchar(40) NOT NULL,
  UNIQUE (workspace_id, id),
  FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));
CREATE TABLE notes (id integer PRIMARY KEY, cycle_slug varchar(40), cycle_id integer,
  FOREIGN KEY (cycle_slug, cycle_id) REFERENCES cycles (slug, id));
CREATE TABLE labels (id integer PRIMARY KEY, workspace_id integer NOT NULL, UNIQUE (workspace_id, id));
CREATE TABLE task_labels (workspace_id integer NOT NULL, task_id integer NOT NULL, label_id integer NOT NULL,
  PRIMARY KEY (workspace_id, task_id, label_id),
  FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id),
  FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id));
`
	const member = `CREATE TABLE %[1]ss (id integer PRIMARY KEY, workspace_id integer NOT NULL, k %[2]s NOT NULL, UNIQUE (k, id));
CREATE TABLE %[1]s_kids (id integer PRIMARY KEY, workspace_id integer NOT NULL, k %[2]s, parent_id integer,
  FOREIGN KEY (k, parent_id) REFERENCES %[1]ss (k, id));
`
	const body = `  paths: [schema.sql]
output:
  driver: %s
  dir: out
  package: models
  layout: %s
tenancy:
  enabled: true
  column: workspace_id
  required: true
generation:
  nested_mutations:
    enabled: true
`
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	const pgEnum = "CREATE TYPE sort AS ENUM ('a', 'b');\n"
	tests := []struct {
		dialect, driver, layout string
		prelude                 string
		enum, timestamp         string
	}{
		{"postgres", "pgx", "single_file", pgEnum, "sort", "timestamptz"},
		{"postgres", "stdlib", "file_per_table", pgEnum, "sort", "timestamp"},
		{"mysql", "stdlib", "single_file", "", "enum('a', 'b')", "datetime"},
		{"sqlite", "stdlib", "file_per_table", "", "", "datetime"},
	}
	for _, tt := range tests {
		t.Run(tt.dialect+"/"+tt.driver, func(t *testing.T) {
			cfg := "version: 1\ninput:\n  dialect: " + tt.dialect + "\n" + fmt.Sprintf(body, tt.driver, tt.layout)
			ddl := tt.prelude + schema + fmt.Sprintf(member, "flag", "boolean") + fmt.Sprintf(member, "stamp", tt.timestamp)
			if tt.enum != "" {
				ddl += fmt.Sprintf(member, "kind", tt.enum)
			}
			dir, stderr, err := runCommand(t, "generate", ddl, cfg)
			if err != nil {
				t.Fatalf("generate: %v\nstderr: %s", err, stderr)
			}
			if testing.Short() && os.Getenv("SQLGEN_BUILD_GENERATED") == "" {
				return
			}
			buildGenerated(t, root, dir)
		})
	}
}

// The connect split compares a one-column time key with the parent's. A SQLite
// datetime reads back as a time.Time off the Null wrapper while the parent holds
// a types.DateTime, so the parent's side unwraps, whichever column the FK
// references.
func TestGenerate_TimeFKNestedWritersCompile(t *testing.T) {
	const cfg = `version: 1
input:
  dialect: %s
  paths: [schema.sql]
output:
  driver: %s
  dir: out
  package: models
  layout: single_file
generation:
  nested_mutations:
    enabled: true
`
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, dialect, driver, schema string }{
		{"unique target", "sqlite", "stdlib", `CREATE TABLE stamps (id integer PRIMARY KEY, k datetime NOT NULL UNIQUE);
CREATE TABLE stamp_kids (id integer PRIMARY KEY, k datetime REFERENCES stamps (k));`},
		{"pk target", "sqlite", "stdlib", `CREATE TABLE stamps (k datetime PRIMARY KEY);
CREATE TABLE stamp_kids (id integer PRIMARY KEY, stamp_k datetime REFERENCES stamps (k));`},
		{"self reference", "sqlite", "stdlib", `CREATE TABLE stamps (k datetime PRIMARY KEY, parent_k datetime REFERENCES stamps (k));`},
		{"postgres pgx", "postgres", "pgx", `CREATE TABLE stamps (k timestamptz PRIMARY KEY);
CREATE TABLE stamp_kids (id integer PRIMARY KEY, stamp_k timestamptz REFERENCES stamps (k));`},
		{"postgres stdlib", "postgres", "stdlib", `CREATE TABLE stamps (k timestamp PRIMARY KEY);
CREATE TABLE stamp_kids (id integer PRIMARY KEY, stamp_k timestamp REFERENCES stamps (k));`},
		{"mysql", "mysql", "stdlib", `CREATE TABLE stamps (k datetime(6) PRIMARY KEY);
CREATE TABLE stamp_kids (id integer PRIMARY KEY, stamp_k datetime(6) REFERENCES stamps (k));`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, stderr, err := runCommand(t, "generate", tt.schema, fmt.Sprintf(cfg, tt.dialect, tt.driver))
			if err != nil {
				t.Fatalf("generate: %v\nstderr: %s", err, stderr)
			}
			if testing.Short() && os.Getenv("SQLGEN_BUILD_GENERATED") == "" {
				return
			}
			buildGenerated(t, root, dir)
		})
	}
}

// A SQLite time key next to an FK of another storage class has no loader that
// can compare the two, so generate and validate refuse it, in both directions
// and on a declared edge, and exit 1 (PRD §4.13, §13.3).
func TestGenerate_TimeKeyNextToAnotherStorageClassIsRefused(t *testing.T) {
	const cfg = `version: 1
input:
  dialect: sqlite
  paths: [schema.sql]
output:
  driver: stdlib
  dir: out
  package: models
  layout: single_file
overrides:
  use_pointers: %v
generation:
  nested_mutations:
    enabled: true
%s`
	const declared = `tables:
  users:
    relationships:
      - name: Profile
        type: one_to_one
        table: profiles
        fk: user_at
`
	// Without the belongs-to edge, which reports the pair first, the list
	// edge's direction is refused too.
	const listOnly = "tables:\n  stamp_kids:\n    exclude_relationships: [stamps]\n"
	tests := []struct{ name, schema, tables, want string }{
		{
			"datetime key, text fk", `CREATE TABLE stamps (k datetime PRIMARY KEY);
CREATE TABLE stamp_kids (id integer PRIMARY KEY, k text REFERENCES stamps (k));`, listOnly,
			`tables.stamps.relationships: "stamp_kids" pairs stamp_kids.k (text) with stamps.k (datetime), a time column next to a column of another storage class: no loader can compare them, since neither side spells the time as the other stores it. Declare both columns with the same storage class`,
		},
		{
			"text key, datetime fk", `CREATE TABLE stamps (k text PRIMARY KEY);
CREATE TABLE stamp_kids (id integer PRIMARY KEY, k datetime REFERENCES stamps (k));`, listOnly,
			`tables.stamps.relationships: "stamp_kids" pairs stamp_kids.k (datetime) with stamps.k (text)`,
		},
		{
			"belongs-to over a text fk into a datetime key", `CREATE TABLE stamps (k datetime PRIMARY KEY);
CREATE TABLE stamp_kids (id integer PRIMARY KEY, k text REFERENCES stamps (k));`, "",
			`tables.stamp_kids.relationships: "stamp" pairs stamp_kids.k (text) with stamps.k (datetime)`,
		},
		{
			"declared datetime fk into an integer key", `CREATE TABLE users (id integer PRIMARY KEY);
CREATE TABLE profiles (id integer PRIMARY KEY, user_at datetime);`, declared,
			`tables.users.relationships: "Profile" pairs profiles.user_at (datetime) with users.id (integer)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, usePointers := range []bool{true, false} {
				for _, command := range []string{"generate", "validate"} {
					t.Run(fmt.Sprintf("use_pointers %v/%s", usePointers, command), func(t *testing.T) {
						_, stderr, err := runCommand(t, command, tt.schema, fmt.Sprintf(cfg, usePointers, tt.tables))
						if err == nil {
							t.Fatalf("%s succeeded, want the time-key refusal\nstderr: %s", command, stderr)
						}
						if code := exitCodeOf(t, err); code != ExitGeneration {
							t.Errorf("exit code = %d, want %d", code, ExitGeneration)
						}
						if !strings.Contains(err.Error()+stderr, tt.want) {
							t.Errorf("error = %v\nstderr: %s\nwant it to contain %q", err, stderr, tt.want)
						}
					})
				}
			}
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

// Declaring one of two edges left one name over its key takes that edge's
// place, which clears the collision with no exclude_relationships entry (PRD
// §4.13, §13.4).
func TestGenerate_DetectedNameCollisionClearedByDeclaringOneEdge(t *testing.T) {
	const schema = `CREATE TABLE organizations (id bigint PRIMARY KEY);
CREATE TABLE users (id bigint PRIMARY KEY, organization_id bigint,
  FOREIGN KEY (organization_id) REFERENCES organizations(id));
CREATE TABLE organization_users (organization_id bigint NOT NULL, user_id bigint NOT NULL, PRIMARY KEY (organization_id, user_id),
  FOREIGN KEY (organization_id) REFERENCES organizations(id), FOREIGN KEY (user_id) REFERENCES users(id));`
	const declared = `tables:
  organizations:
    relationships:
      - name: Members
        type: o2m
        table: users
        fk: organization_id
`
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		t.Run(dialect, func(t *testing.T) {
			cfg := strings.Replace(tenantConfig, "dialect: postgres", "dialect: "+dialect, 1)
			if dialect != "postgres" {
				cfg = strings.Replace(cfg, "output:\n", "output:\n  driver: stdlib\n", 1)
			}
			if _, stderr, err := runCommand(t, "generate", schema, cfg); err == nil || !strings.Contains(stderr, `both named "organization_users" after disambiguation`) {
				t.Fatalf("generate without the declaration: err = %v, stderr = %q, want the relationship name collision error", err, stderr)
			}
			_, stderr, err := runCommand(t, "generate", schema, cfg+declared)
			if err != nil {
				t.Errorf("generate with one edge declared: %v\nstderr: %s", err, stderr)
			}
		})
	}
}
