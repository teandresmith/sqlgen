package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// newTestRegistry builds a type registry for test tables.
func newTestRegistry(t *testing.T) *typeRegistry {
	t.Helper()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "serial", PrimaryKey: true},
					{Name: "name", Type: "text"},
					{Name: "email", Type: "text"},
				},
			},
			{
				Name:   "products",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "serial", PrimaryKey: true},
					{Name: "name", Type: "text"},
					{Name: "price", Type: "numeric"},
					{Name: "deleted_at", Type: "timestamptz", Nullable: true},
				},
			},
			{
				Name:   "audit_logs",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "serial", PrimaryKey: true},
					{Name: "message", Type: "text"},
				},
			},
		},
	}

	// Dialect and soft_delete_columns are spelled out because this fixture
	// bypasses config.LoadConfig, which is what normally applies both defaults.
	// Without the dialect the resolver maps no SQL type, so `price numeric`
	// resolves to a non-arithmetic Go type and products looks un-incrementable;
	// without the column list DetectSoftDelete matches nothing, so products
	// looks un-soft-deletable. Both would make this fixture quietly degenerate
	// and every rule built on it pass for the wrong reason.
	cfg := &config.RootConfig{
		Input: config.InputConfig{Dialect: config.DialectPostgres},
		Generation: config.GenerationConfig{
			SoftDeleteColumns: []config.SoftDeleteConfig{{Name: "deleted_at"}},
		},
		// audit_logs' key is app-enforced with no UNIQUE beside it, so it
		// emits no conflict target and its client has no Upsert (PRD §9.5).
		Tables: map[string]config.TableConfig{
			"audit_logs": {PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"id"}}},
		},
	}

	reg, err := buildTypeRegistry(schema, cfg)
	if err != nil {
		t.Fatalf("buildTypeRegistry() error: %v", err)
	}
	return reg
}

// writeGoFile writes a Go source file with the given body into dir.
func writeGoFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := `package hooks

import (
	"context"

	"github.com/teandresmith/sqlgen/hook"
)

` + body
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing test Go file %s: %v", path, err)
	}
}

func TestLint_tableTypeMismatch(t *testing.T) {
	reg := newTestRegistry(t)
	dir := t.TempDir()

	// CreateProductInput used with TableUsers — type mismatch.
	writeGoFile(t, dir, "hooks.go", `
var _ = hook.ForMutation[*CreateProductInput, *Product](TableUsers, func(
	ctx context.Context,
	m *hook.MutationContext,
	input *CreateProductInput,
	next func(ctx context.Context) (*Product, error),
) (*Product, error) {
	return next(ctx)
})
`)

	issues, err := lintDir(reg, dir)
	if err != nil {
		t.Fatalf("lintDir() error: %v", err)
	}

	if len(issues) == 0 {
		t.Fatal("expected at least one issue for type mismatch, got none")
	}

	var mismatchCount int
	for _, issue := range issues {
		if issue.Severity == severityError && strings.Contains(issue.Explanation, "type mismatch") {
			mismatchCount++
			if !strings.Contains(issue.Explanation, "TableProducts") {
				t.Errorf("explanation should mention TableProducts, got: %q", issue.Explanation)
			}
			if !strings.Contains(issue.Explanation, "TableUsers") {
				t.Errorf("explanation should mention TableUsers, got: %q", issue.Explanation)
			}
		}
	}
	// Both type params (CreateProductInput and Product) belong to products, not users.
	if mismatchCount < 1 {
		t.Error("expected at least one type mismatch error, none found")
	}
}

func TestLint_invalidOperationForTable(t *testing.T) {
	reg := newTestRegistry(t)
	dir := t.TempDir()

	// Users table has no soft delete column — OpSoftDelete hook will never fire.
	writeGoFile(t, dir, "hooks.go", `
var _ = hook.ForMutation[*CreateUserInput, *User](TableUsers, func(
	ctx context.Context,
	m *hook.MutationContext,
	input *CreateUserInput,
	next func(ctx context.Context) (*User, error),
) (*User, error) {
	if m.Op == hook.OpSoftDelete {
		// should never fire — users has no soft delete column
	}
	return next(ctx)
})
`)

	issues, err := lintDir(reg, dir)
	if err != nil {
		t.Fatalf("lintDir() error: %v", err)
	}

	var foundWarning bool
	for _, issue := range issues {
		if issue.Severity == severityWarning && strings.Contains(issue.Explanation, "soft delete") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected a warning about missing soft delete column, none found")
	}
}

// TestLint_upsertHookOnTableWithoutConflictTarget pins that deleting rule 3
// ("Unused table constant") lost no coverage. Rule 3 was the only rule that
// reported a hook on OpUpsert for a table with no conflict target; rule 2 now
// reads the resolved operations and reports it as a warning naming the fact.
func TestLint_upsertHookOnTableWithoutConflictTarget(t *testing.T) {
	reg := newTestRegistry(t)
	dir := t.TempDir()

	writeGoFile(t, dir, "hooks.go", `
var _ = hook.ForMutation[*CreateAuditLogInput, *AuditLog](TableAuditLogs, func(
	ctx context.Context,
	m *hook.MutationContext,
	input *CreateAuditLogInput,
	next func(ctx context.Context) (*AuditLog, error),
) (*AuditLog, error) {
	if m.Op == hook.OpUpsert {
		// audit_logs has no conflict target — this op never fires
	}
	return next(ctx)
})
`)

	issues, err := lintDir(reg, dir)
	if err != nil {
		t.Fatalf("lintDir() error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("lintDir() returned %d issues, want 1: %+v", len(issues), issues)
	}
	if issues[0].Severity != severityWarning || !strings.Contains(issues[0].Explanation, "has no conflict target") {
		t.Errorf("issue = %+v, want a warning that audit_logs has no conflict target", issues[0])
	}
}

func TestLint_pathsScanSpecifiedDirectories(t *testing.T) {
	reg := newTestRegistry(t)
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Type mismatch in dir2 — lintFiles should scan both.
	writeGoFile(t, dir1, "clean.go", `
var _ = hook.ForMutation[*CreateUserInput, *User](TableUsers, func(
	ctx context.Context,
	m *hook.MutationContext,
	input *CreateUserInput,
	next func(ctx context.Context) (*User, error),
) (*User, error) {
	return next(ctx)
})
`)

	writeGoFile(t, dir2, "bad.go", `
var _ = hook.ForMutation[*CreateProductInput, *Product](TableUsers, func(
	ctx context.Context,
	m *hook.MutationContext,
	input *CreateProductInput,
	next func(ctx context.Context) (*Product, error),
) (*Product, error) {
	return next(ctx)
})
`)

	issues, err := lintFiles(reg, []string{dir1, dir2})
	if err != nil {
		t.Fatalf("lintFiles() error: %v", err)
	}

	// dir1 is clean, dir2 has a mismatch.
	var mismatchCount int
	for _, issue := range issues {
		if issue.Severity == severityError && strings.Contains(issue.Explanation, "type mismatch") {
			mismatchCount++
		}
	}
	if mismatchCount == 0 {
		t.Error("expected type mismatch from dir2, found none")
	}
}

func TestLint_failOnWarning(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o750); err != nil {
		t.Fatalf("creating hooks dir: %v", err)
	}

	// Config for a table with no soft delete column.
	content := []byte(`input:
  dialect: postgres
  paths:
    - "` + sqlDir + `"
output:
  driver: pgx
  dir: ` + outputDir + `
  package: models
  layout: file_per_table
`)
	if err := os.WriteFile(cfgPath, content, 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	writeSQLFile(t, sqlDir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	// Write a hook file that references OpSoftDelete on a table without soft delete.
	hookContent := `package hooks

import (
	"context"
	"github.com/teandresmith/sqlgen/hook"
)

var _ = hook.ForMutation[*CreateUserInput, *User](TableUsers, func(
	ctx context.Context,
	m *hook.MutationContext,
	input *CreateUserInput,
	next func(ctx context.Context) (*User, error),
) (*User, error) {
	if m.Op == hook.OpSoftDelete {
		// will never fire
	}
	return next(ctx)
})
`
	if err := os.WriteFile(filepath.Join(hooksDir, "hooks.go"), []byte(hookContent), 0o600); err != nil {
		t.Fatalf("writing hooks file: %v", err)
	}

	// --fail-on warning should exit 1 because there's a warning.
	_, _, err := executeCommand("lint", "--config", cfgPath, "--paths", hooksDir, "--fail-on", "warning")
	if err == nil {
		t.Fatal("lint --fail-on warning should return error when warnings exist, got nil")
	}
}

func TestLint_failOnInfo(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o750); err != nil {
		t.Fatalf("creating hooks dir: %v", err)
	}

	content := []byte(`input:
  dialect: postgres
  paths:
    - "` + sqlDir + `"
output:
  driver: pgx
  dir: ` + outputDir + `
  package: models
  layout: file_per_table

`)
	if err := os.WriteFile(cfgPath, content, 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	writeSQLFile(t, sqlDir, "CREATE TABLE audit_logs (id serial PRIMARY KEY, message text NOT NULL);")

	// Hook references OpSoftDelete on a table with no soft-delete column — a
	// warning, which --fail-on info counts too: the threshold is a minimum.
	hookContent := `package hooks

import (
	"context"
	"github.com/teandresmith/sqlgen/hook"
)

var _ = hook.ForMutation[*CreateAuditLogInput, *AuditLog](TableAuditLogs, func(
	ctx context.Context,
	m *hook.MutationContext,
	input *CreateAuditLogInput,
	next func(ctx context.Context) (*AuditLog, error),
) (*AuditLog, error) {
	if m.Op == hook.OpSoftDelete {
		// no soft-delete column — this op never fires
	}
	return next(ctx)
})
`
	if err := os.WriteFile(filepath.Join(hooksDir, "hooks.go"), []byte(hookContent), 0o600); err != nil {
		t.Fatalf("writing hooks file: %v", err)
	}

	// --fail-on info should exit 1 because there's an issue at or above info.
	_, _, err := executeCommand("lint", "--config", cfgPath, "--paths", hooksDir, "--fail-on", "info")
	if err == nil {
		t.Fatal("lint --fail-on info should return error when issues at or above info exist, got nil")
	}
}

func TestLint_cleanCode(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o750); err != nil {
		t.Fatalf("creating hooks dir: %v", err)
	}

	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)
	writeSQLFile(t, sqlDir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	// Correct hook — types match table.
	hookContent := `package hooks

import (
	"context"
	"github.com/teandresmith/sqlgen/hook"
)

var _ = hook.ForMutation[*CreateUserInput, *User](TableUsers, func(
	ctx context.Context,
	m *hook.MutationContext,
	input *CreateUserInput,
	next func(ctx context.Context) (*User, error),
) (*User, error) {
	return next(ctx)
})
`
	if err := os.WriteFile(filepath.Join(hooksDir, "hooks.go"), []byte(hookContent), 0o600); err != nil {
		t.Fatalf("writing hooks file: %v", err)
	}

	stdout, _, err := executeCommand("lint", "--config", cfgPath, "--paths", hooksDir)
	if err != nil {
		t.Fatalf("lint should exit 0 for clean code, got error: %v", err)
	}
	if strings.Contains(stdout, "error") || strings.Contains(stdout, "warn") || strings.Contains(stdout, "info") {
		t.Errorf("lint should produce no output for clean code, got: %q", stdout)
	}
}

func TestLint_outputFormat(t *testing.T) {
	reg := newTestRegistry(t)
	dir := t.TempDir()

	writeGoFile(t, dir, "hooks.go", `
var _ = hook.ForMutation[*CreateProductInput, *Product](TableUsers, func(
	ctx context.Context,
	m *hook.MutationContext,
	input *CreateProductInput,
	next func(ctx context.Context) (*Product, error),
) (*Product, error) {
	return next(ctx)
})
`)

	issues, err := lintDir(reg, dir)
	if err != nil {
		t.Fatalf("lintDir() error: %v", err)
	}

	if len(issues) == 0 {
		t.Fatal("expected issues, got none")
	}

	sortIssues(issues)

	var buf strings.Builder
	formatLintOutput(&buf, issues)
	output := buf.String()

	// Check format: severity, file:line, code, explanation.
	if !strings.Contains(output, "error") {
		t.Errorf("output should contain severity 'error', got:\n%s", output)
	}
	if !strings.Contains(output, "hooks.go:") {
		t.Errorf("output should contain file:line, got:\n%s", output)
	}
	if !strings.Contains(output, "ForMutation") {
		t.Errorf("output should contain code snippet, got:\n%s", output)
	}
	if !strings.Contains(output, "type mismatch") {
		t.Errorf("output should contain explanation, got:\n%s", output)
	}
	// Summary line.
	if !strings.Contains(output, "issues") {
		t.Errorf("output should contain summary line, got:\n%s", output)
	}
}

// TestBuildTypeRegistry_MatchesGeneratorNaming pins the linter's spelling of
// the two identifiers it resolves back to a table against the generator's own.
// The linter derived `Table<Plural>` and `Get<Plural>Input` from its own
// `flect.Pluralize` call, so for any table whose name ends in an acronym the
// registry keyed a name the generator never emitted and every lint rule that
// looks a constant up silently missed.
func TestBuildTypeRegistry_MatchesGeneratorNaming(t *testing.T) {
	tables := []string{"user_ips", "client_os", "lens", "user_ip", "products"}

	schema := &parser.Schema{}
	for _, name := range tables {
		schema.Tables = append(schema.Tables, parser.Table{
			Name:   name,
			Schema: "public",
			Columns: []parser.Column{
				{Name: "id", Type: "serial", PrimaryKey: true},
				{Name: "name", Type: "text"},
			},
		})
	}
	cfg := &config.RootConfig{}

	reg, err := buildTypeRegistry(schema, cfg)
	if err != nil {
		t.Fatalf("buildTypeRegistry() error: %v", err)
	}

	for _, name := range tables {
		constName := gen.TableConstantName(name, "public", "", nil)
		info, ok := reg.byConstant[constName]
		if !ok {
			t.Errorf("table %q: registry has no entry for %q — the linter and the generator spell the constant differently",
				name, constName)
			continue
		}
		wantInput := "Get" + gen.StructNamePlural(gen.StructName(name, "public", nil)) + "Input"
		if !info.TypeNames[wantInput] {
			t.Errorf("table %q: registry has no entry for %q", name, wantInput)
		}
	}
}

// TestBuildTypeRegistry_IncrementMatchesGenerator pins the linter's increment
// fact to the generator's. The rule warns when an OpIncrement hook can never
// fire, and "can never fire" means the generator emitted no Increment method —
// which it gates on `Operations.Increment && len(IncrementColumns) > 0`.
//
// The registry used to answer this with its own predicate: any non-PK column
// whose SQL type contained a numeric substring. That over-approximated four
// ways at once, and each one is a table the rule stayed silent on when it
// should have warned. Every case below is a table the old predicate
// called incrementable and the generator does not.
func TestBuildTypeRegistry_IncrementMatchesGenerator(t *testing.T) {
	tests := []struct {
		name          string
		columns       []parser.Column
		tableCfg      *config.TableConfig
		tenancy       *config.TenancyConfig
		wantIncrement bool
	}{
		{
			name: "plain numeric column is incrementable",
			columns: []parser.Column{
				{Name: "id", Type: "serial", PrimaryKey: true},
				{Name: "view_count", Type: "integer"},
			},
			wantIncrement: true,
		},
		{
			name: "integer foreign keys are not incrementable",
			columns: []parser.Column{
				{Name: "id", Type: "serial", PrimaryKey: true},
				{Name: "post_id", Type: "integer", FKReference: &parser.FKReference{Table: "posts", Column: "id"}},
				{Name: "tag_id", Type: "integer", FKReference: &parser.FKReference{Table: "tags", Column: "id"}},
			},
			wantIncrement: false,
		},
		{
			name: "tenant column is not incrementable",
			columns: []parser.Column{
				{Name: "id", Type: "serial", PrimaryKey: true},
				{Name: "workspace_id", Type: "bigint"},
				{Name: "title", Type: "text"},
			},
			tenancy:       &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: new(true)},
			wantIncrement: false,
		},
		{
			name: "numeric array columns are not incrementable",
			columns: []parser.Column{
				{Name: "id", Type: "serial", PrimaryKey: true},
				{Name: "scores", Type: "integer[]"},
				{Name: "weights", Type: "double precision[]"},
			},
			wantIncrement: false,
		},
		{
			name: "config-declared foreign keys are not incrementable",
			columns: []parser.Column{
				{Name: "id", Type: "serial", PrimaryKey: true},
				{Name: "entity_id", Type: "bigint"},
			},
			tableCfg: &config.TableConfig{
				Relationships: []config.TableRelationship{
					{Name: "Owner", Type: "one_to_many", Table: "probes", FK: "entity_id"},
				},
			},
			wantIncrement: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &parser.Schema{Tables: []parser.Table{{
				Name: "probes", Schema: "public", Columns: tt.columns,
			}}}
			cfg := &config.RootConfig{
				Input:   config.InputConfig{Dialect: config.DialectPostgres},
				Tenancy: tt.tenancy,
			}
			if tt.tableCfg != nil {
				cfg.Tables = map[string]config.TableConfig{"probes": *tt.tableCfg}
			}

			reg, err := buildTypeRegistry(schema, cfg)
			if err != nil {
				t.Fatalf("buildTypeRegistry() error: %v", err)
			}
			info := reg.byConstant["TableProbes"]
			if info == nil {
				t.Fatal("buildTypeRegistry() produced no entry for TableProbes")
			}

			if info.HasIncrement != tt.wantIncrement {
				t.Errorf("HasIncrement = %v, want %v", info.HasIncrement, tt.wantIncrement)
			}
			// Rule 2 reads the resolved flag, so it must agree with the column
			// fact even when tenancy re-derives the columns after resolution.
			if info.Ops.Increment != tt.wantIncrement {
				t.Errorf("Ops.Increment = %v, want %v", info.Ops.Increment, tt.wantIncrement)
			}

			// The registry's answer is only useful if it matches what the
			// generator emits, so assert against the context directly rather
			// than against a second hand-maintained expectation.
			contexts, err := gen.BuildTableContextsFromSchema(schema, cfg)
			if err != nil {
				t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
			}
			if len(contexts) != 1 {
				t.Fatalf("BuildTableContextsFromSchema() returned %d contexts, want 1", len(contexts))
			}
			if got := len(contexts[0].IncrementColumns) > 0; got != info.HasIncrement {
				t.Errorf("registry HasIncrement = %v, but generator emits %d increment columns", info.HasIncrement, len(contexts[0].IncrementColumns))
			}
		})
	}
}

// TestLint_incrementHookOnFKOnlyTable is the end-to-end shape of the
// increment-eligibility gap: a junction table whose only non-PK columns are
// integer foreign keys generates no Increment method, so an OpIncrement hook on
// it can never fire and the rule must say so. Before the fix this produced no output at all.
func TestLint_incrementHookOnFKOnlyTable(t *testing.T) {
	schema := &parser.Schema{Tables: []parser.Table{{
		Name: "post_tags", Schema: "public",
		Columns: []parser.Column{
			{Name: "id", Type: "serial", PrimaryKey: true},
			{Name: "post_id", Type: "integer", FKReference: &parser.FKReference{Table: "posts", Column: "id"}},
			{Name: "tag_id", Type: "integer", FKReference: &parser.FKReference{Table: "tags", Column: "id"}},
		},
	}}}
	cfg := &config.RootConfig{
		Input: config.InputConfig{Dialect: config.DialectPostgres},
	}

	reg, err := buildTypeRegistry(schema, cfg)
	if err != nil {
		t.Fatalf("buildTypeRegistry() error: %v", err)
	}

	dir := t.TempDir()
	writeGoFile(t, dir, "hooks.go", `
var _ = hook.ForMutation[*CreatePostTagInput, *PostTag](TablePostTags, func(
	ctx context.Context,
	m *hook.MutationContext,
	input *CreatePostTagInput,
	next func(ctx context.Context) (*PostTag, error),
) (*PostTag, error) {
	if m.Op == hook.OpIncrement {
		// should never fire — every numeric column is a foreign key
	}
	return next(ctx)
})
`)

	issues, err := lintDir(reg, dir)
	if err != nil {
		t.Fatalf("lintDir() error: %v", err)
	}

	var found bool
	for _, issue := range issues {
		if issue.Severity == severityWarning && strings.Contains(issue.Explanation, "incrementable") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a warning about incrementable columns for OpIncrement, got %d issues: %+v", len(issues), issues)
	}
}

// TestOpEnabledForTable_ReadsResolvedOperations pins that the lint reads the
// table's resolved operations, which are its schema facts (PRD §4.6): a table
// with no soft-delete column, incrementable column or conflict target has no
// method for those hooks, and every other method is generated.
func TestOpEnabledForTable_ReadsResolvedOperations(t *testing.T) {
	reg := newTestRegistry(t)
	info := reg.byConstant["TableAuditLogs"]
	if info == nil {
		t.Fatal("newTestRegistry() produced no entry for TableAuditLogs")
	}

	tests := []struct {
		opRef string
		want  bool
	}{
		{opRef: "OpGet", want: true},
		{opRef: "OpCount", want: true},
		{opRef: "OpCreate", want: true},
		{opRef: "OpHardDelete", want: true},
		{opRef: "OpSoftDelete", want: false}, // no soft-delete column
		{opRef: "OpIncrement", want: false},  // no incrementable column
		{opRef: "OpUpsert", want: false},     // no conflict target
		{opRef: "OpUpsertMany", want: false}, // no conflict target
		{opRef: "OpRefresh", want: true},     // view-only constant — no table flag
	}
	for _, tt := range tests {
		t.Run(tt.opRef, func(t *testing.T) {
			if got := opEnabledForTable(tt.opRef, info.Ops); got != tt.want {
				t.Errorf("opEnabledForTable(%q) = %v, want %v", tt.opRef, got, tt.want)
			}
		})
	}
}

// TestBuildTypeRegistry_ReportsGenerationErrors covers the false all-clear.
// A config that `generate` and `validate` both reject used to make `sqlgen
// lint` exit 0 with no output, because the registry never reached the
// generation phase that rejects it — a clean bill of health for a schema that
// cannot produce code, and so for generated code that is necessarily stale.
// `sqlgen validate` once had the same gap.
func TestBuildTypeRegistry_ReportsGenerationErrors(t *testing.T) {
	schema := &parser.Schema{Tables: []parser.Table{{
		Name: "users", Schema: "public",
		Columns: []parser.Column{
			{Name: "id", Type: "serial", PrimaryKey: true},
			{Name: "name", Type: "text"},
			{Name: "email", Type: "text"},
		},
	}}}
	// Both columns resolve to the Go field `Name` — a §8.5 package-scope
	// collision that generation rejects.
	cfg := &config.RootConfig{
		Input: config.InputConfig{Dialect: config.DialectPostgres},
		Tables: map[string]config.TableConfig{
			"users": {ColumnMap: map[string]config.ColumnOverride{
				"email": {Name: "Name"},
			}},
		},
	}

	_, err := buildTypeRegistry(schema, cfg)
	if err == nil {
		t.Fatal("buildTypeRegistry() returned no error for a schema generation rejects")
	}
	if !strings.Contains(err.Error(), "claimed by both") {
		t.Errorf("buildTypeRegistry() error = %v, want the field-name collision", err)
	}
}

// TestNewTestRegistry_FixtureFacts pins what the shared fixture is supposed to
// represent, so it cannot quietly degenerate into a registry where every table
// looks featureless and every rule passes for the wrong reason.
//
// It has done exactly that once: the fixture builds a config.RootConfig
// directly rather than through config.LoadConfig, which is what applies the
// dialect and soft_delete_columns defaults. With neither set, `price numeric`
// resolved to a non-arithmetic Go type and `deleted_at` matched no configured
// soft-delete name, so products — the one table carrying both features —
// reported HasIncrement=false and HasSoftDelete=false.
func TestNewTestRegistry_FixtureFacts(t *testing.T) {
	reg := newTestRegistry(t)

	tests := []struct {
		constant          string
		wantSoftDelete    bool
		wantIncrement     bool
		wantUpsertEnabled bool
	}{
		// products carries both features: `deleted_at` and `price numeric`.
		{constant: "TableProducts", wantSoftDelete: true, wantIncrement: true, wantUpsertEnabled: true},
		// users is all-text with no deleted_at.
		{constant: "TableUsers", wantSoftDelete: false, wantIncrement: false, wantUpsertEnabled: true},
		// audit_logs' key is app-enforced, so it has no conflict target.
		{constant: "TableAuditLogs", wantSoftDelete: false, wantIncrement: false, wantUpsertEnabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.constant, func(t *testing.T) {
			info := reg.byConstant[tt.constant]
			if info == nil {
				t.Fatalf("newTestRegistry() has no entry for %s", tt.constant)
			}
			if info.HasSoftDelete != tt.wantSoftDelete {
				t.Errorf("%s HasSoftDelete = %v, want %v", tt.constant, info.HasSoftDelete, tt.wantSoftDelete)
			}
			if info.HasIncrement != tt.wantIncrement {
				t.Errorf("%s HasIncrement = %v, want %v", tt.constant, info.HasIncrement, tt.wantIncrement)
			}
			if got := opEnabledForTable("OpUpsert", info.Ops); got != tt.wantUpsertEnabled {
				t.Errorf("%s opEnabledForTable(OpUpsert) = %v, want %v", tt.constant, got, tt.wantUpsertEnabled)
			}
		})
	}
}

// TestLint_shapeReasonWinsOverOperationsReason pins that a hook which can never
// fire is reported once, with the structural reason. It once drew a second,
// info-level issue from rule 3 blaming an `operations` config that excluded
// nothing; rule 3 and the toggles are gone (PRD §4.6, §23.7).
func TestLint_shapeReasonWinsOverOperationsReason(t *testing.T) {
	reg := newTestRegistry(t)
	dir := t.TempDir()

	// users has no deleted_at.
	writeGoFile(t, dir, "hooks.go", `
var _ = hook.ForMutation[*CreateUserInput, *User](TableUsers, func(
	ctx context.Context,
	m *hook.MutationContext,
	input *CreateUserInput,
	next func(ctx context.Context) (*User, error),
) (*User, error) {
	if m.Op == hook.OpSoftDelete {
	}
	return next(ctx)
})
`)

	issues, err := lintDir(reg, dir)
	if err != nil {
		t.Fatalf("lintDir() error: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("lintDir() returned %d issues, want 1: %+v", len(issues), issues)
	}
	if issues[0].Severity != severityWarning {
		t.Errorf("issue severity = %v, want %v", issues[0].Severity, severityWarning)
	}
	if !strings.Contains(issues[0].Explanation, "no soft delete column") {
		t.Errorf("explanation = %q, want the missing-column reason", issues[0].Explanation)
	}
	if strings.Contains(issues[0].Explanation, "operations config") {
		t.Errorf("explanation = %q, but no operations config excludes soft delete", issues[0].Explanation)
	}
}

// TestLint_crossSchemaTypeMismatch covers the one rule that runs at severity
// error, and so the only one the default `--fail-on error` gates CI on.
//
// It compared tables by SQL name, which is not an identity: `public.users` and
// `audit.users` are both "users". Every cross-schema mismatch therefore looked
// like a match and produced no finding — including the case below, where an
// input type belonging to one schema's table is handed to the other's
// constant. The comparison now runs on ConstantName, the disambiguated Go
// identifier §8.5 guarantees is unique per entity.
func TestLint_crossSchemaTypeMismatch(t *testing.T) {
	cols := []parser.Column{
		{Name: "id", Type: "serial", PrimaryKey: true},
		{Name: "name", Type: "text"},
	}
	schema := &parser.Schema{Tables: []parser.Table{
		{Name: "users", Schema: "public", Columns: cols},
		{Name: "users", Schema: "audit", Columns: cols},
	}}
	cfg := &config.RootConfig{
		Input: config.InputConfig{Dialect: config.DialectPostgres},
	}

	reg, err := buildTypeRegistry(schema, cfg)
	if err != nil {
		t.Fatalf("buildTypeRegistry() error: %v", err)
	}

	// The bare SQL name is deliberately identical for both entries — that is
	// the shape the old comparison could not see through.
	pub, aud := reg.byConstant["TablePublicUsers"], reg.byConstant["TableAuditUsers"]
	if pub == nil || aud == nil {
		t.Fatalf("registry missing a schema-qualified entry: public=%v audit=%v", pub, aud)
	}
	if pub.SQLName != aud.SQLName {
		t.Fatalf("fixture no longer exercises the collision: %q vs %q", pub.SQLName, aud.SQLName)
	}

	dir := t.TempDir()
	writeGoFile(t, dir, "hooks.go", `
var _ = hook.ForMutation[*CreatePublicUserInput, *PublicUser](TableAuditUsers, func(
	ctx context.Context,
	m *hook.MutationContext,
	input *CreatePublicUserInput,
	next func(ctx context.Context) (*PublicUser, error),
) (*PublicUser, error) {
	return next(ctx)
})
`)

	issues, err := lintDir(reg, dir)
	if err != nil {
		t.Fatalf("lintDir() error: %v", err)
	}

	var mismatches int
	for _, issue := range issues {
		if issue.Severity != severityError || !strings.Contains(issue.Explanation, "type mismatch") {
			continue
		}
		mismatches++
		if !strings.Contains(issue.Explanation, "TablePublicUsers") || !strings.Contains(issue.Explanation, "TableAuditUsers") {
			t.Errorf("explanation should name both constants, got: %q", issue.Explanation)
		}
	}
	// Both type params belong to public.users.
	if mismatches != 2 {
		t.Errorf("got %d cross-schema type mismatch errors, want 2: %+v", mismatches, issues)
	}
}

// TestLint_sameSchemaTypeMatchIsSilent is the negative half: switching rule 1
// to ConstantName must not start reporting correctly-paired hooks.
func TestLint_sameSchemaTypeMatchIsSilent(t *testing.T) {
	reg := newTestRegistry(t)
	dir := t.TempDir()

	writeGoFile(t, dir, "hooks.go", `
var _ = hook.ForMutation[*CreateProductInput, *Product](TableProducts, func(
	ctx context.Context,
	m *hook.MutationContext,
	input *CreateProductInput,
	next func(ctx context.Context) (*Product, error),
) (*Product, error) {
	return next(ctx)
})
`)

	issues, err := lintDir(reg, dir)
	if err != nil {
		t.Fatalf("lintDir() error: %v", err)
	}
	for _, issue := range issues {
		if strings.Contains(issue.Explanation, "type mismatch") {
			t.Errorf("correctly paired hook reported a mismatch: %q", issue.Explanation)
		}
	}
}
