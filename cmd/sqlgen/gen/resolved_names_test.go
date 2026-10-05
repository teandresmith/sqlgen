package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// namedTable is the minimal table shape the name rules need: the names are
// what is under test, so one PK column and one payload column is enough.
func namedTable(name, schema string) parser.Table {
	return parser.Table{
		Name:   name,
		Schema: schema,
		Columns: []parser.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "label", Type: "text"},
		},
	}
}

func namedView(name, schema string) parser.View {
	return parser.View{
		Name:   name,
		Schema: schema,
		Columns: []parser.Column{
			{Name: "id", Type: "uuid"},
			{Name: "label", Type: "text"},
		},
	}
}

// nameRuleConfig is a config defaulted just far enough for the context
// builders, with no output.client block so the nil-tolerant reserved-name
// lookups are exercised on the default path too.
func nameRuleConfig() *config.RootConfig {
	cfg := &config.RootConfig{}
	cfg.Input.Dialect = config.DialectPostgres
	cfg.Output.Driver = config.DriverPgx
	cfg.Output.Package = "db"
	cfg.Generation.QueryLimit = new(1000)
	cfg.Generation.BatchSize = new(200)
	cfg.Generation.PageSize = new(100)
	cfg.Generation.UUIDVersion = "v4"
	cfg.Overrides.UsePointers = new(true)
	cfg.Tables = map[string]config.TableConfig{}
	cfg.Views = map[string]config.ViewConfig{}
	return cfg
}

// TestValidateResolvedNames_EntityCollisions covers the half of PRD §4.13 that
// is about two schema entities landing on one generated name.
//
// Every "wants error" case once generated silently: the old lexical
// normalizer in config compared names with underscores stripped, which models
// neither singularization nor the schema prefix nor `struct_name`. The final
// case is the mirror image — the example the PRD itself used, which that
// normalizer rejected even though the two tables resolve to UserRole and
// Userrole and generate fine.
func TestValidateResolvedNames_EntityCollisions(t *testing.T) {
	tests := []struct {
		name     string
		tables   []parser.Table
		views    []parser.View
		enums    []parser.Enum
		exclude  []string
		override map[string]config.TableConfig
		wantErr  []string
	}{
		{
			name:   "singular and plural of one noun",
			tables: []parser.Table{namedTable("users", ""), namedTable("user", "")},
			wantErr: []string{
				`table user`, `table users`,
				// One rename fixes all three, so it is one error naming all three.
				`the Go type name "User", the file stem "user" and the table-name constant "TableUsers"`,
				`set tables.user.struct_name or tables.users.struct_name`,
			},
		},
		{
			name:    "irregular plural beside its singular",
			tables:  []parser.Table{namedTable("people", ""), namedTable("person", "")},
			wantErr: []string{`Go type name "Person"`, `file stem "person"`},
		},
		{
			name:    "acronym-leading plural beside its singular",
			tables:  []parser.Table{namedTable("osi_layers", ""), namedTable("osi_layer", "")},
			wantErr: []string{`Go type name "OSILayer"`, `file stem "osi_layer"`},
		},
		{
			name:    "view stem matching a table stem",
			tables:  []parser.Table{namedTable("person", "")},
			views:   []parser.View{namedView("people", "")},
			wantErr: []string{`table person`, `view people`, `file stem "person"`},
		},
		{
			name:     "struct_name override colliding with an auto-derived name",
			tables:   []parser.Table{namedTable("users", ""), namedTable("accounts", "")},
			override: map[string]config.TableConfig{"accounts": {StructName: "User"}},
			wantErr:  []string{`Go type name "User"`, `tables.accounts.struct_name`},
		},
		{
			// The escape hatch has to move all three names or it is not an
			// escape: the constant follows `struct_name` for exactly this
			// reason. Before it did, this config was rejected on
			// `TablePeople` with nothing left for the consumer to change.
			name:     "the documented escape hatch resolves every claim",
			tables:   []parser.Table{namedTable("people", ""), namedTable("person", "")},
			override: map[string]config.TableConfig{"people": {StructName: "Human"}},
		},
		{
			// Pluralization is not injective: Analysis and Analyse both
			// pluralize to Analyses. The type names and file stems separate
			// cleanly, so only the third key catches this.
			name:    "distinct struct names meeting on the table constant",
			tables:  []parser.Table{namedTable("analyses", ""), namedTable("analyse", "")},
			wantErr: []string{`the table-name constant "TableAnalyses"`},
		},
		{
			// A dropped table claims nothing. BuildTableContexts skips a table
			// with no resolved primary key, and the tablename file is built
			// from the contexts, so `people` contributes no constant to collide
			// with `person`'s. Before the file was filtered it emitted
			// `TablePeople` twice for exactly this schema.
			name: "a table that generates nothing claims no name",
			tables: []parser.Table{
				{Name: "people", Columns: []parser.Column{{Name: "label", Type: "text"}}},
				namedTable("person", ""),
			},
		},
		{
			// Same for an excluded table: the pattern drops it before the
			// contexts are built, so it cannot collide with anything.
			name:    "an excluded table claims no name",
			tables:  []parser.Table{namedTable("people", ""), namedTable("person", "")},
			exclude: []string{"people"},
		},
		{
			name:    "enum resolving to a table's struct name",
			tables:  []parser.Table{namedTable("orders", "")},
			enums:   []parser.Enum{{Name: "order", Values: []string{"a", "b"}}},
			wantErr: []string{`enum order`, `table orders`, `Go type name "Order"`},
		},
		{
			name:   "names that only look alike are not a collision",
			tables: []parser.Table{namedTable("user_roles", ""), namedTable("userroles", "")},
		},
		{
			name:   "same bare name in two schemas is disambiguated by the prefix",
			tables: []parser.Table{namedTable("users", "public"), namedTable("users", "audit")},
		},
		{
			name:   "singularization collision across schemas is disambiguated too",
			tables: []parser.Table{namedTable("people", "public"), namedTable("person", "audit")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := nameRuleConfig()
			if tt.override != nil {
				cfg.Tables = tt.override
			}
			cfg.ExcludeTables = tt.exclude
			schema := &parser.Schema{Tables: tt.tables, Views: tt.views, Enums: tt.enums}

			_, err := gen.ValidateGeneration(schema, cfg)

			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("ValidateGeneration() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateGeneration() error = nil, want an error mentioning %v", tt.wantErr)
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ValidateGeneration() error = %v, want it to mention %q", err, want)
				}
			}
		})
	}
}

// TestValidateResolvedNames_ReservedNames covers the other half: one entity
// landing on a name sqlgen's own generated package already declares. Without
// this check a table named `clients` was overwritten by client_gen.go and the
// consumer got "undefined: clientClient" from a file they did not write.
func TestValidateResolvedNames_ReservedNames(t *testing.T) {
	tests := []struct {
		name       string
		table      string
		clientName string
		clientFile string
		wantErr    []string
	}{
		{
			name:    "table resolving to the client struct",
			table:   "clients",
			wantErr: []string{`table clients`, `Go type name "Client"`, `client_gen.go`},
		},
		{
			name:    "table resolving to the connection type",
			table:   "connections",
			wantErr: []string{`Go type name "Connection"`, `connection_gen.go`},
		},
		{
			name:    "table resolving to the edge type",
			table:   "edges",
			wantErr: []string{`Go type name "Edge"`, `connection_gen.go`},
		},
		{
			name:    "table resolving to a generator-owned file stem",
			table:   "sorters",
			wantErr: []string{`file stem "sorter"`, `sorter_gen.go`},
		},
		{
			name:       "a renamed client frees the name it used to reserve",
			table:      "clients",
			clientName: "Store",
			clientFile: "store_gen.go",
		},
		{
			name:       "a renamed client reserves its new name instead",
			table:      "stores",
			clientName: "Store",
			clientFile: "store_gen.go",
			wantErr:    []string{`Go type name "Store"`, `store_gen.go`},
		},
		{
			name:  "an ordinary table is unaffected",
			table: "orders",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := nameRuleConfig()
			if tt.clientName != "" {
				cfg.Output.Client = &config.ClientOutputConfig{Name: tt.clientName, File: tt.clientFile}
			}
			schema := &parser.Schema{Tables: []parser.Table{namedTable(tt.table, "")}}

			_, err := gen.ValidateGeneration(schema, cfg)

			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("ValidateGeneration() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateGeneration() error = nil, want an error mentioning %v", tt.wantErr)
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ValidateGeneration() error = %v, want it to mention %q", err, want)
				}
			}
		})
	}
}

// TestValidateResolvedNames_ReportsEveryViolation pins the batch-report
// behaviour ValidateGeneration promises: one run names every collision, not
// only the first, so a consumer fixes them in one pass.
func TestValidateResolvedNames_ReportsEveryViolation(t *testing.T) {
	schema := &parser.Schema{Tables: []parser.Table{
		namedTable("users", ""), namedTable("user", ""),
		namedTable("people", ""), namedTable("person", ""),
		namedTable("clients", ""),
	}}

	_, err := gen.ValidateGeneration(schema, nameRuleConfig())
	if err == nil {
		t.Fatal("ValidateGeneration() error = nil, want errors for three distinct problems")
	}
	for _, want := range []string{`"User"`, `"Person"`, `"Client"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateGeneration() error = %v, want it to mention %q", err, want)
		}
	}
}

// TestGenerate_CollidingStemsRejectedBeforeAnyWrite drives the real emission
// path. Under `layout: file_per_table` the two tables below wrote one
// `person_gen.go`, and the surviving package failed to compile on a duplicate
// client field rather than on anything naming the two tables. Generation must
// now refuse, and refuse before it has written a file.
func TestGenerate_CollidingStemsRejectedBeforeAnyWrite(t *testing.T) {
	outDir := t.TempDir()
	cfg := loadLayoutConfig(t, outDir, "file_per_table")
	schema := &parser.Schema{Tables: []parser.Table{namedTable("people", ""), namedTable("person", "")}}

	if _, err := gen.Generate(schema, cfg, "test"); err == nil {
		t.Fatal("Generate() error = nil, want a resolved-name collision")
	} else if !strings.Contains(err.Error(), `file stem "person"`) {
		t.Errorf("Generate() error = %v, want it to name the shared file stem", err)
	}

	if got := dirEntries(t, outDir); len(got) != 0 {
		t.Errorf("Generate() wrote %v before rejecting the config; want nothing on disk", got)
	}
}

// TestGenerate_DistinctStemsBothEmitted is the positive half: the two tables
// the retired lexical rule rejected do resolve apart, and both files land.
func TestGenerate_DistinctStemsBothEmitted(t *testing.T) {
	outDir := t.TempDir()
	cfg := loadLayoutConfig(t, outDir, "file_per_table")
	schema := &parser.Schema{Tables: []parser.Table{namedTable("user_roles", ""), namedTable("userroles", "")}}

	if _, err := gen.Generate(schema, cfg, "test"); err != nil {
		t.Fatalf("Generate() error = %v, want the two tables to generate", err)
	}
	assertHasAll(t, outDir, []string{"user_role_gen.go", "userrole_gen.go"})
}

// TestGenerate_CrossSchemaSingularizationPrefixed pins the widened collision
// key. `public.people` and `audit.person` are two SQL names that resolve to one
// Go name, which the raw-name key could not see: both went unprefixed and the
// package did not compile. The schema prefix is the mechanism PRD §5.5 already
// provides for exactly this, so the pair must generate, prefixed.
func TestGenerate_CrossSchemaSingularizationPrefixed(t *testing.T) {
	outDir := t.TempDir()
	cfg := loadLayoutConfig(t, outDir, "file_per_table")
	schema := &parser.Schema{Tables: []parser.Table{
		namedTable("people", "public"), namedTable("person", "audit"),
	}}

	if _, err := gen.Generate(schema, cfg, "test"); err != nil {
		t.Fatalf("Generate() error = %v, want the schema prefix to disambiguate", err)
	}
	assertHasAll(t, outDir, []string{"public_person_gen.go", "audit_person_gen.go"})
}

// TestBuildTableContextsFromSchema_RejectsCollisions covers the third entry
// point. `sqlgen graphql gen` reaches it without going through Generate or
// ValidateGeneration, and it is the only path that emits `<stem>_gen.graphqls`,
// so a stem collision has to be caught here or nowhere.
func TestBuildTableContextsFromSchema_RejectsCollisions(t *testing.T) {
	schema := &parser.Schema{Tables: []parser.Table{namedTable("people", ""), namedTable("person", "")}}

	if _, err := gen.BuildTableContextsFromSchema(schema, nameRuleConfig()); err == nil {
		t.Fatal("BuildTableContextsFromSchema() error = nil, want a resolved-name collision")
	} else if !strings.Contains(err.Error(), `file stem "person"`) {
		t.Errorf("BuildTableContextsFromSchema() error = %v, want it to name the shared file stem", err)
	}
}

// TestValidateResolvedNames_SharedGraphQLStem pins the one reserved stem that
// lives in the API schema directory rather than output.dir. The API pass writes
// `<stem>_gen.graphqls` per table and then one fixed `shared_gen.graphqls`
// beside them, so a table resolving to `shared` is overwritten there even
// though its `_gen.go` is fine.
func TestValidateResolvedNames_SharedGraphQLStem(t *testing.T) {
	schema := &parser.Schema{Tables: []parser.Table{namedTable("shareds", "")}}

	_, err := gen.ValidateGeneration(schema, nameRuleConfig())
	if err == nil {
		t.Fatal("ValidateGeneration() error = nil, want the shared_gen.graphqls stem to be reserved")
	}
	for _, want := range []string{`file stem "shared"`, "shared_gen.graphqls"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateGeneration() error = %v, want it to mention %q", err, want)
		}
	}
}

// loadLayoutConfig produces a fully-defaulted config for the given layout.
// gen.Generate needs the defaults only config.LoadConfig applies.
func loadLayoutConfig(t *testing.T, outDir, layout string) *config.RootConfig {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "sqlgen.yml")
	body := `version: v1
input:
  dialect: postgres
  paths:
    - ./schema.sql
output:
  driver: pgx
  dir: ` + outDir + `
  package: db
  layout: ` + layout + `
`
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}
