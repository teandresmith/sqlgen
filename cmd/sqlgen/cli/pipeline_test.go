package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/parser"
)

// TestToSchemaTables_PartialUniqueExcludedFromUniqueGroups pins that
// post-parse validation uses SchemaTable.UniqueGroups to assert
// PK-coverage and similar invariants that require unconditional uniqueness.
// A partial UNIQUE constraint (Where != "") only holds for rows matching the
// predicate, so it must not satisfy those invariants — exclude it from the
// surface that validation consumes.
func TestToSchemaTables_PartialUniqueExcludedFromUniqueGroups(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "events",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "tenant_id", Type: "bigint"},
					{Name: "external_ref", Type: "text"},
				},
				Constraints: []parser.Constraint{
					{
						Name:    "events_pkey",
						Type:    parser.PrimaryKey,
						Columns: []string{"id"},
					},
					{
						Name:    "events_tenant_ref_full_uq",
						Type:    parser.Unique,
						Columns: []string{"tenant_id", "external_ref"},
					},
					{
						Name:    "events_tenant_ref_partial_uq",
						Type:    parser.Unique,
						Columns: []string{"tenant_id", "external_ref"},
						Method:  "btree",
						Where:   "status = 'open'",
					},
				},
			},
		},
	}

	tables := toSchemaTables(schema)
	if len(tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(tables))
	}
	want := [][]string{
		{"id"},
		{"tenant_id", "external_ref"},
	}
	if diff := cmp.Diff(want, tables[0].UniqueGroups); diff != "" {
		t.Errorf("UniqueGroups mismatch — partial UNIQUE leaked through (-want +got):\n%s", diff)
	}
}

// writeSchemaProject writes a sqlgen.yml for dialect and one DDL file holding
// sql into a fresh directory, and returns the config path.
func writeSchemaProject(t *testing.T, dialect config.Dialect, sql string) string {
	t.Helper()
	dir := t.TempDir()
	driver := config.DriverPgx
	if dialect != config.DialectPostgres {
		driver = config.DriverStdlib
	}
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	content := "input:\n  dialect: " + string(dialect) + "\n  paths:\n    - \"" + filepath.ToSlash(dir) + "\"\n" +
		"output:\n  driver: " + string(driver) + "\n  dir: " + filepath.ToSlash(filepath.Join(dir, "output")) +
		"\n  package: models\n  layout: file_per_table\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	writeSQLFile(t, dir, sql)
	return cfgPath
}

// runSchemaCommand runs command against cfgPath. `diff` exits non-zero while
// any file would change, so for a success-path run it first generates the
// output diff compares against.
func runSchemaCommand(t *testing.T, command, cfgPath string, succeeds bool, extra ...string) (string, error) {
	t.Helper()
	if command == "diff" && succeeds {
		if _, stderr, err := executeCommand("generate", "--config", cfgPath, "--quiet"); err != nil {
			t.Fatalf("generate before diff: unexpected error: %v\nstderr: %s", err, stderr)
		}
	}
	_, stderr, err := executeCommand(append([]string{command, "--config", cfgPath}, extra...)...)
	return stderr, err
}

// TestParseSchema_UnresolvedForeignKey pins the rule that on PostgreSQL,
// a foreign key whose target no parsed table defines is a schema error on
// every command that parses the schema, because PostgreSQL itself rejects the
// DDL and relationship detection would otherwise drop the edge silently.
// MySQL (under FOREIGN_KEY_CHECKS=0) and SQLite accept such DDL, so they keep
// generating.
func TestParseSchema_UnresolvedForeignKey(t *testing.T) {
	tests := []struct {
		name     string
		dialect  config.Dialect
		sql      string
		wantCode int // 0 means the command succeeds
		wantErr  string
	}{
		{
			name:     "postgres target never parsed",
			dialect:  config.DialectPostgres,
			sql:      "CREATE TABLE pets (id uuid PRIMARY KEY, owner_id uuid REFERENCES ghosts(id));",
			wantCode: ExitSchema,
			wantErr:  "foreign key public.pets(owner_id) references public.ghosts, which no parsed table defines",
		},
		{
			name:    "postgres unqualified target from another schema resolves to public",
			dialect: config.DialectPostgres,
			sql: `CREATE TABLE owners (id uuid PRIMARY KEY);
				CREATE TABLE audit.pets (id uuid PRIMARY KEY, owner_id uuid REFERENCES owners(id));`,
		},
		{
			name:    "postgres unqualified target only another schema defines",
			dialect: config.DialectPostgres,
			sql: `CREATE TABLE audit.owners (id uuid PRIMARY KEY);
				CREATE TABLE audit.pets (id uuid PRIMARY KEY, owner_id uuid REFERENCES owners(id));`,
			wantCode: ExitSchema,
			wantErr:  "foreign key audit.pets(owner_id) references public.owners, which no parsed table defines",
		},
		{
			name:    "postgres reference to a renamed table follows the rename",
			dialect: config.DialectPostgres,
			sql: `CREATE TABLE owners (id uuid PRIMARY KEY);
				CREATE TABLE pets (id uuid PRIMARY KEY, owner_id uuid REFERENCES owners(id));
				ALTER TABLE owners RENAME TO people;`,
		},
		{
			name:    "postgres reference to a table's new schema after SET SCHEMA resolves",
			dialect: config.DialectPostgres,
			sql: `CREATE TABLE owners (id uuid PRIMARY KEY);
				ALTER TABLE owners SET SCHEMA auth;
				CREATE TABLE pets (id uuid PRIMARY KEY, owner_id uuid REFERENCES auth.owners(id));`,
		},
		{
			name:    "postgres reference to a table's old name after SET SCHEMA is an error",
			dialect: config.DialectPostgres,
			sql: `CREATE TABLE owners (id uuid PRIMARY KEY);
				ALTER TABLE owners SET SCHEMA auth;
				CREATE TABLE pets (id uuid PRIMARY KEY, owner_id uuid REFERENCES owners(id));`,
			wantCode: ExitSchema,
			wantErr:  "foreign key public.pets(owner_id) references public.owners, which no parsed table defines",
		},
		{
			name:    "postgres reference to a table's old name after RENAME TO is an error",
			dialect: config.DialectPostgres,
			sql: `CREATE TABLE owners (id uuid PRIMARY KEY);
				ALTER TABLE owners RENAME TO people;
				CREATE TABLE pets (id uuid PRIMARY KEY, owner_id uuid REFERENCES owners(id));`,
			wantCode: ExitSchema,
			wantErr:  "foreign key public.pets(owner_id) references public.owners, which no parsed table defines",
		},
		{
			name:    "mysql target never parsed still generates",
			dialect: config.DialectMySQL,
			sql:     "CREATE TABLE pets (id INT PRIMARY KEY, owner_id INT, FOREIGN KEY (owner_id) REFERENCES ghosts(id));",
		},
		{
			name:    "sqlite target never parsed still generates",
			dialect: config.DialectSQLite,
			sql:     "CREATE TABLE pets (id INTEGER PRIMARY KEY, owner_id INTEGER REFERENCES ghosts(id));",
		},
	}
	for _, tt := range tests {
		for _, command := range []string{"generate", "validate", "diff"} {
			t.Run(tt.name+"/"+command, func(t *testing.T) {
				cfgPath := writeSchemaProject(t, tt.dialect, tt.sql)
				stderr, err := runSchemaCommand(t, command, cfgPath, tt.wantCode == 0)
				if tt.wantCode == 0 {
					if err != nil {
						t.Fatalf("%s: unexpected error: %v\nstderr: %s", command, err, stderr)
					}
					return
				}
				ee, ok := errors.AsType[*exitError](err)
				if !ok || ee.code != tt.wantCode {
					t.Fatalf("%s: error = %v, want exit code %d\nstderr: %s", command, err, tt.wantCode, stderr)
				}
				if !strings.Contains(stderr, tt.wantErr) {
					t.Errorf("%s: stderr = %q, want it to contain %q", command, stderr, tt.wantErr)
				}
			})
		}
	}
}

// TestValidate_UnresolvedForeignKeyKeepsChecking pins that an unresolved
// PostgreSQL foreign key does not hide the rest of `validate`'s report: the
// parser warnings, the post-parse config error and the
// generation-phase error are all reported in the same run, with every FK line,
// and the exit code stays the schema error's.
func TestValidate_UnresolvedForeignKeyKeepsChecking(t *testing.T) {
	cfgPath := writeSchemaProject(t, config.DialectPostgres, `CREATE TABLE pets (
			id uuid PRIMARY KEY,
			owner_id uuid REFERENCES ghosts(id),
			vet_id uuid REFERENCES phantoms(id),
			"Columns" text
		);
		CREATE VIEW pet_ids AS SELECT id FROM pets;`)
	f, err := os.OpenFile(filepath.Clean(cfgPath), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("opening config: %v", err)
	}
	if _, err := f.WriteString("tables:\n  pets:\n    cursor_keys: [nope]\n"); err != nil {
		t.Fatalf("appending to config: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing config: %v", err)
	}

	_, stderr, err := executeCommand("validate", "--config", cfgPath)
	ee, ok := errors.AsType[*exitError](err)
	if !ok || ee.code != ExitSchema {
		t.Fatalf("validate: error = %v, want exit code %d\nstderr: %s", err, ExitSchema, stderr)
	}
	for _, want := range []string{
		"Warning: CREATE VIEW pet_ids skipped in DDL file",
		"Error: foreign key public.pets(owner_id) references public.ghosts, which no parsed table defines\n",
		"Error: foreign key public.pets(vet_id) references public.phantoms, which no parsed table defines\n",
		`Error: cursor_keys: column "nope" does not exist in table public.pets`,
		`column "Columns" resolves to Go field name "Columns"`,
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("validate: stderr does not contain %q\nstderr: %s", want, stderr)
		}
	}
}

// TestParseSchema_UnresolvedForeignKeyReturnsSchema pins parseSchema's
// contract for an unresolved PostgreSQL foreign key: the parsed
// schema, input.views included, comes back alongside the error so `validate`
// can keep checking, and a view file that fails to parse is reported together
// with the FK error rather than in place of it.
func TestParseSchema_UnresolvedForeignKeyReturnsSchema(t *testing.T) {
	const fkErr = "foreign key public.pets(owner_id) references public.ghosts, which no parsed table defines"
	tests := []struct {
		name       string
		viewSQL    string
		wantSchema bool
		wantErrs   []string
	}{
		{
			name:       "view parses, schema returned with the FK error",
			viewSQL:    "CREATE VIEW pet_ids AS SELECT p.id FROM pets p;",
			wantSchema: true,
			wantErrs:   []string{fkErr},
		},
		{
			name:     "view fails, both errors reported",
			viewSQL:  "CREATE VIEW pet_ids AS SELEC p.id FROM pets p;",
			wantErrs: []string{fkErr, "parsing views:"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			schemaPath := filepath.Join(dir, "schema.sql")
			ddl := "CREATE TABLE pets (id uuid PRIMARY KEY, owner_id uuid REFERENCES ghosts(id));"
			if err := os.WriteFile(schemaPath, []byte(ddl), 0o600); err != nil {
				t.Fatal(err)
			}
			viewPath := filepath.Join(dir, "pet_ids.sql")
			if err := os.WriteFile(viewPath, []byte(tt.viewSQL), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := &config.RootConfig{Input: config.InputConfig{
				Dialect: config.DialectPostgres,
				Schema:  "*",
				Paths:   []string{schemaPath},
				Views:   []string{viewPath},
			}}

			schema, err := parseSchema(cfg)
			if err == nil {
				t.Fatal("parseSchema: error = nil, want the unresolved-FK error")
			}
			for _, want := range tt.wantErrs {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("parseSchema: error = %q, want it to contain %q", err, want)
				}
			}
			if got := schema != nil; got != tt.wantSchema {
				t.Fatalf("parseSchema: schema returned = %t, want %t", got, tt.wantSchema)
			}
			if schema != nil && (len(schema.Tables) != 1 || len(schema.Views) != 1) {
				t.Errorf("parseSchema: %d tables, %d views, want 1 and 1", len(schema.Tables), len(schema.Views))
			}
		})
	}
}

// TestParseSchema_UnresolvedForeignKeysPrefixEachLine pins that `generate`
// and the commands sharing loadAndValidate print every unresolved-FK line
// with the "Schema error:" prefix, the way `validate` prefixes each line
// "Error:", rather than prefixing only the first line of the joined error.
func TestParseSchema_UnresolvedForeignKeysPrefixEachLine(t *testing.T) {
	const sql = `CREATE TABLE pets (
		id uuid PRIMARY KEY,
		owner_id uuid REFERENCES ghosts(id),
		vet_id uuid REFERENCES phantoms(id)
	);`
	want := "Schema error: foreign key public.pets(owner_id) references public.ghosts, which no parsed table defines\n" +
		"Schema error: foreign key public.pets(vet_id) references public.phantoms, which no parsed table defines\n"
	for _, command := range []string{"generate", "diff", "lint"} {
		t.Run(command, func(t *testing.T) {
			cfgPath := writeSchemaProject(t, config.DialectPostgres, sql)
			_, stderr, err := executeCommand(command, "--config", cfgPath)
			ee, ok := errors.AsType[*exitError](err)
			if !ok || ee.code != ExitSchema {
				t.Fatalf("%s: error = %v, want exit code %d\nstderr: %s", command, err, ExitSchema, stderr)
			}
			if stderr != want {
				t.Errorf("%s: stderr = %q, want %q", command, stderr, want)
			}
		})
	}
}

// TestGenerate_SetSchemaMovesTable pins that ALTER TABLE … SET SCHEMA moves
// the table, and that a foreign key declared before the move follows it, as
// PostgreSQL's OID-keyed constraints do: the owner is generated in
// its new schema and keeps its edge to the pets that reference it.
func TestGenerate_SetSchemaMovesTable(t *testing.T) {
	cfgPath := writeSchemaProject(t, config.DialectPostgres, `CREATE TABLE owners (id uuid PRIMARY KEY);
		CREATE TABLE pets (id uuid PRIMARY KEY, owner_id uuid REFERENCES owners(id));
		ALTER TABLE owners SET SCHEMA auth;`)
	if _, stderr, err := executeCommand("generate", "--config", cfgPath, "--quiet"); err != nil {
		t.Fatalf("generate: unexpected error: %v\nstderr: %s", err, stderr)
	}
	src, err := os.ReadFile(filepath.Join(filepath.Dir(cfgPath), "output", "owner_gen.go"))
	if err != nil {
		t.Fatalf("reading owner_gen.go: %v", err)
	}
	for _, want := range []string{`sql.Table{Schema: "auth", Name: "owners"}`, "Pets []*Pet"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("owner_gen.go does not contain %q", want)
		}
	}
}

// TestParseSchema_PrintsParserWarnings pins that the parser's own warnings
// reach the user: PRD §16 requires a warning when a DDL file in input.paths
// holds a CREATE VIEW or CREATE MATERIALIZED VIEW, which the parser skips.
// Every command that parses the schema prints them, and --quiet suppresses
// them as it does every other warning.
func TestParseSchema_PrintsParserWarnings(t *testing.T) {
	const sql = `CREATE TABLE users (id uuid PRIMARY KEY);
		CREATE VIEW user_ids AS SELECT id FROM users;
		CREATE MATERIALIZED VIEW user_count AS SELECT count(*) AS n FROM users;`
	want := []string{
		"Warning: CREATE VIEW user_ids skipped in DDL file — use introspection or define the view in input.views\n",
		"Warning: CREATE MATERIALIZED VIEW user_count skipped in DDL file — use introspection or define the view in input.views\n",
	}
	for _, command := range []string{"generate", "validate", "diff"} {
		for _, quiet := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/quiet=%t", command, quiet), func(t *testing.T) {
				var extra []string
				if quiet {
					extra = append(extra, "--quiet")
				}
				cfgPath := writeSchemaProject(t, config.DialectPostgres, sql)
				stderr, err := runSchemaCommand(t, command, cfgPath, true, extra...)
				if err != nil {
					t.Fatalf("%s: unexpected error: %v\nstderr: %s", command, err, stderr)
				}
				for _, w := range want {
					if got := strings.Contains(stderr, w); got == quiet {
						t.Errorf("%s --quiet=%t: stderr contains %q = %t, want %t\nstderr: %s", command, quiet, w, got, !quiet, stderr)
					}
				}
			})
		}
	}
}

// TestParseSchema_ViewDefaultSchema pins that an input.views file whose view
// name is unqualified resolves to the schema an unqualified CREATE TABLE does
// (input.schema, "public" when unset). A view without one kept a bare
// hook.TableName value and unqualified SQL, against PRD §5.5.
func TestParseSchema_ViewDefaultSchema(t *testing.T) {
	tests := []struct {
		name        string
		inputSchema string
		viewSQL     string
		want        []string // "schema.name" of each parsed view
	}{
		{
			name:        "unqualified view, default input.schema",
			inputSchema: "*",
			viewSQL:     "CREATE VIEW order_ids AS SELECT o.id FROM orders o;",
			want:        []string{"public.order_ids"},
		},
		{
			name:        "unqualified materialized view, default input.schema",
			inputSchema: "*",
			viewSQL:     "CREATE MATERIALIZED VIEW order_ids AS SELECT o.id FROM orders o;",
			want:        []string{"public.order_ids"},
		},
		{
			name:        "unqualified view, input.schema set",
			inputSchema: "billing",
			viewSQL:     "CREATE VIEW order_ids AS SELECT o.id FROM orders o;",
			want:        []string{"billing.order_ids"},
		},
		{
			name:        "qualified view keeps its schema",
			inputSchema: "billing",
			viewSQL:     "CREATE VIEW reporting.order_ids AS SELECT o.id FROM orders o;",
			want:        []string{"reporting.order_ids"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			schemaPath := filepath.Join(dir, "schema.sql")
			if err := os.WriteFile(schemaPath, []byte("CREATE TABLE orders (id uuid PRIMARY KEY);"), 0o600); err != nil {
				t.Fatal(err)
			}
			viewDir := filepath.Join(dir, "views")
			if err := os.Mkdir(viewDir, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(viewDir, "order_ids.sql"), []byte(tt.viewSQL), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := &config.RootConfig{Input: config.InputConfig{
				Dialect: config.DialectPostgres,
				Schema:  tt.inputSchema,
				Paths:   []string{schemaPath},
				Views:   []string{viewDir},
			}}

			schema, err := parseSchema(cfg)
			if err != nil {
				t.Fatalf("parseSchema: %v", err)
			}
			got := make([]string, 0, len(schema.Views))
			for _, v := range schema.Views {
				got = append(got, v.Schema+"."+v.Name)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("view schema.name mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
