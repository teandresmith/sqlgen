package config_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// validConfig returns a minimal valid config for testing.
func validConfig() *config.RootConfig {
	return &config.RootConfig{
		Version: "v1",
		Input: config.InputConfig{
			Dialect: "postgres",
			Source:  "files",
			Paths:   []string{"."},
		},
		Output: config.OutputConfig{
			Driver:  "pgx",
			Dir:     "./models",
			Package: "models",
			Layout:  "single_file",
		},
		Overrides: config.OverrideConfig{
			Types: make(map[string]config.TypeOverride),
		},
		Tables: make(map[string]config.TableConfig),
		Views:  make(map[string]config.ViewConfig),
		Extras: make(map[string]config.ExtraType),
	}
}

// TestValidatePreParse_GqlgenAliasCollision pins that when api.graphql
// is enabled and the consumer's output.package is the same string as the
// sqlgen-controlled `gqlmodel` import alias, validation must fail at
// config-load time so the conflict surfaces before code generation.
func TestValidatePreParse_GqlgenAliasCollision(t *testing.T) {
	tests := []struct {
		name        string
		pkg         string
		apiEnabled  bool
		wantErr     bool
		wantMessage string
	}{
		{
			name:        "graphql enabled with colliding package",
			pkg:         config.GqlgenModelImportAlias,
			apiEnabled:  true,
			wantErr:     true,
			wantMessage: "collides with the sqlgen-controlled gqlgen import alias",
		},
		{
			name:       "graphql enabled with safe package",
			pkg:        "models",
			apiEnabled: true,
			wantErr:    false,
		},
		{
			name:       "graphql disabled — collision check skipped",
			pkg:        config.GqlgenModelImportAlias,
			apiEnabled: false,
			wantErr:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Output.Package = tt.pkg
			if tt.apiEnabled {
				cfg.API = &config.APIConfig{
					Enabled: true,
					GraphQL: &config.GraphQLAPIConfig{Enabled: true},
				}
			}
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidatePreParse: expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidatePreParse: unexpected error: %v", err)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("ValidatePreParse: error %q does not contain %q", err.Error(), tt.wantMessage)
			}
		})
	}
}

func TestValidatePreParse_DriverDialectMismatch(t *testing.T) {
	tests := []struct {
		name    string
		driver  config.Driver
		dialect config.Dialect
		wantErr bool
	}{
		{name: "pgx+postgres is valid", driver: config.DriverPgx, dialect: config.DialectPostgres, wantErr: false},
		{name: "pgx+mysql is error", driver: config.DriverPgx, dialect: config.DialectMySQL, wantErr: true},
		{name: "pgx+sqlite is error", driver: config.DriverPgx, dialect: config.DialectSQLite, wantErr: true},
		{name: "stdlib+postgres is valid", driver: config.DriverStdlib, dialect: config.DialectPostgres, wantErr: false},
		{name: "stdlib+mysql is valid", driver: config.DriverStdlib, dialect: config.DialectMySQL, wantErr: false},
		{name: "stdlib+sqlite is valid", driver: config.DriverStdlib, dialect: config.DialectSQLite, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Output.Driver = tt.driver
			cfg.Input.Dialect = tt.dialect

			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr && err == nil {
				t.Errorf("ValidatePreParse() error = nil, want error for driver=%q dialect=%q", tt.driver, tt.dialect)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidatePreParse() unexpected error: %v", err)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), "pgx") {
				t.Errorf("ValidatePreParse() error = %v, want error mentioning pgx", err)
			}
		})
	}
}

func TestValidatePreParse_MissingConnection(t *testing.T) {
	tests := []struct {
		name       string
		source     config.Source
		connection *config.ConnectionConfig
		wantErr    bool
	}{
		{name: "source=database without connection is error", source: config.SourceDatabase, connection: nil, wantErr: true},
		{name: "source=both without connection is error", source: config.SourceBoth, connection: nil, wantErr: true},
		{name: "source=files without connection is ok", source: config.SourceFiles, connection: nil, wantErr: false},
		{name: "source=database with connection is ok", source: config.SourceDatabase, connection: &config.ConnectionConfig{URL: "postgres://localhost"}, wantErr: false},
		{name: "source=both with connection is ok", source: config.SourceBoth, connection: &config.ConnectionConfig{URL: "postgres://localhost"}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Input.Source = tt.source
			cfg.Input.Connection = tt.connection
			// Use stdlib to avoid pgx+non-postgres mismatch when testing non-postgres sources.
			cfg.Output.Driver = "stdlib"

			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr && err == nil {
				t.Errorf("ValidatePreParse() error = nil, want error for source=%q", tt.source)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidatePreParse() unexpected error: %v", err)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), "input.connection") {
				t.Errorf("ValidatePreParse() error = %v, want error mentioning input.connection", err)
			}
		})
	}
}

// TestValidatePreParse_InvalidOperationsPreset pins §4.13's preset rule, which
// applies to the `api.operations` masks only: the client has no operations
// block of its own (§4.6).
func TestValidatePreParse_InvalidOperationsPreset(t *testing.T) {
	tests := []struct {
		name    string
		preset  string
		wantErr bool
	}{
		{name: "valid preset all", preset: "all", wantErr: false},
		{name: "valid preset read_only", preset: "read_only", wantErr: false},
		{name: "valid preset append_only", preset: "append_only", wantErr: false},
		{name: "valid preset no_delete", preset: "no_delete", wantErr: false},
		{name: "valid preset no_hard_delete", preset: "no_hard_delete", wantErr: false},
		{name: "invalid preset write_only", preset: "write_only", wantErr: true},
		{name: "invalid preset custom", preset: "custom", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.API = &config.APIConfig{Enabled: true, Operations: &config.Operations{Preset: tt.preset}}

			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr && err == nil {
				t.Errorf("ValidatePreParse() error = nil, want error for preset=%q", tt.preset)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidatePreParse() unexpected error: %v", err)
			}
		})
	}
}

func TestValidatePreParse_InvalidTableOperationsPreset(t *testing.T) {
	cfg := validConfig()
	cfg.API = &config.APIConfig{Enabled: true}
	cfg.Tables["users"] = config.TableConfig{
		API: &config.TableAPIConfig{Operations: &config.Operations{Preset: "bogus"}},
	}

	_, err := config.ValidatePreParse(cfg)
	if err == nil {
		t.Fatal("ValidatePreParse() error = nil, want error for invalid table operations preset")
	}
	if !strings.Contains(err.Error(), "bogus") || !strings.Contains(err.Error(), "users") {
		t.Errorf("ValidatePreParse() error = %v, want error mentioning preset and table name", err)
	}
}

func TestValidatePreParse_SchemaOnMySQL(t *testing.T) {
	cfg := validConfig()
	cfg.Input.Dialect = "mysql"
	cfg.Input.Schema = "myschema"
	cfg.Output.Driver = "stdlib"

	warnings, err := config.ValidatePreParse(cfg)
	if err != nil {
		t.Fatalf("ValidatePreParse() unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("ValidatePreParse() warnings = 0, want warning for schema on MySQL")
	}
	if !strings.Contains(warnings[0].Message, "MYSQL") {
		t.Errorf("ValidatePreParse() warning = %q, want warning mentioning MYSQL", warnings[0].Message)
	}
}

func TestValidatePreParse_SchemaOnSQLite(t *testing.T) {
	cfg := validConfig()
	cfg.Input.Dialect = "sqlite"
	cfg.Input.Schema = "myschema"
	cfg.Output.Driver = "stdlib"

	warnings, err := config.ValidatePreParse(cfg)
	if err != nil {
		t.Fatalf("ValidatePreParse() unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("ValidatePreParse() warnings = 0, want warning for schema on SQLite")
	}
	if !strings.Contains(warnings[0].Message, "SQLITE") {
		t.Errorf("ValidatePreParse() warning = %q, want warning mentioning SQLITE", warnings[0].Message)
	}
}

func TestValidatePreParse_SchemaOnPostgres(t *testing.T) {
	cfg := validConfig()
	cfg.Input.Dialect = "postgres"
	cfg.Input.Schema = "myschema"

	warnings, err := config.ValidatePreParse(cfg)
	if err != nil {
		t.Fatalf("ValidatePreParse() unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("ValidatePreParse() warnings = %d, want 0 for schema on PostgreSQL", len(warnings))
	}
}

func TestValidatePreParse_MultipleErrors(t *testing.T) {
	cfg := validConfig()
	// Error 1: driver/dialect mismatch.
	cfg.Output.Driver = "pgx"
	cfg.Input.Dialect = "mysql"
	// Error 2: missing connection.
	cfg.Input.Source = "database"
	cfg.Input.Connection = nil

	_, err := config.ValidatePreParse(cfg)
	if err == nil {
		t.Fatal("ValidatePreParse() error = nil, want multiple errors")
	}
	msg := err.Error()
	if !strings.Contains(msg, "pgx") {
		t.Errorf("ValidatePreParse() error missing driver mismatch: %v", err)
	}
	if !strings.Contains(msg, "input.connection") {
		t.Errorf("ValidatePreParse() error missing connection error: %v", err)
	}
}

// TestValidatePreParse_NullableValidConflict pins the nullable-wrapper
// validation that rejects valid_field and valid_method declared together on the
// same wrapper.
func TestValidatePreParse_NullableValidConflict(t *testing.T) {
	cfg := validConfig()
	cfg.Overrides.Types["text"] = config.TypeOverride{
		Type:   "myapp.S",
		Import: "myapp/pkg/s",
		Nullable: config.NullableVariant{
			Type:        "myapp.NullS",
			ValidField:  "Set",
			ValidMethod: "IsSet",
		},
	}

	_, err := config.ValidatePreParse(cfg)
	if err == nil {
		t.Fatal("ValidatePreParse() error = nil, want error for valid_field + valid_method conflict")
	}
	if !strings.Contains(err.Error(), "valid_field") || !strings.Contains(err.Error(), "valid_method") {
		t.Errorf("ValidatePreParse() error = %v, want error naming both fields", err)
	}
}

// TestValidatePreParse_NullableInvertWithoutMethod pins the validation
// that rejects valid_invert set when no valid_method is declared.
func TestValidatePreParse_NullableInvertWithoutMethod(t *testing.T) {
	cfg := validConfig()
	cfg.Overrides.Types["timestamptz"] = config.TypeOverride{
		Type:   "myapp.T",
		Import: "myapp/pkg/t",
		Nullable: config.NullableVariant{
			Type:        "myapp.NullT",
			ValidField:  "Set",
			ValidInvert: true,
		},
	}

	_, err := config.ValidatePreParse(cfg)
	if err == nil {
		t.Fatal("ValidatePreParse() error = nil, want error for valid_invert without valid_method")
	}
	if !strings.Contains(err.Error(), "valid_invert") {
		t.Errorf("ValidatePreParse() error = %v, want error naming valid_invert", err)
	}
}

// TestValidatePreParse_NullableUnderlyingFieldWithoutType pins the
// validation that rejects underlying_field declared without nullable.type.
func TestValidatePreParse_NullableUnderlyingFieldWithoutType(t *testing.T) {
	cfg := validConfig()
	cfg.Overrides.Types["text"] = config.TypeOverride{
		Type:   "myapp.S",
		Import: "myapp/pkg/s",
		Nullable: config.NullableVariant{
			UnderlyingField: "Value", // type omitted — illegal
		},
	}

	_, err := config.ValidatePreParse(cfg)
	if err == nil {
		t.Fatal("ValidatePreParse() error = nil, want error for underlying_field without nullable.type")
	}
	if !strings.Contains(err.Error(), "underlying_field") {
		t.Errorf("ValidatePreParse() error = %v, want error naming underlying_field", err)
	}
}

// TestValidatePreParse_NullableValidConfigsPass pins acceptance for the
// permitted nullable-wrapper shapes (valid_field alone, valid_method alone,
// valid_method + valid_invert).
func TestValidatePreParse_NullableValidConfigsPass(t *testing.T) {
	tests := []struct {
		name string
		v    config.NullableVariant
	}{
		{name: "valid_field alone", v: config.NullableVariant{Type: "x.NullS", UnderlyingField: "Value", ValidField: "Set"}},
		{name: "valid_method alone", v: config.NullableVariant{Type: "x.NullS", UnderlyingField: "Value", ValidMethod: "IsSet"}},
		{name: "valid_method + invert", v: config.NullableVariant{Type: "x.NullT", UnderlyingField: "Stamp", ValidMethod: "IsZero", ValidInvert: true}},
		{name: "default (Valid)", v: config.NullableVariant{Type: "x.NullS", UnderlyingField: "Value"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Overrides.Types["text"] = config.TypeOverride{
				Type:     "x.S",
				Import:   "myapp/pkg/x",
				Nullable: tt.v,
			}
			if _, err := config.ValidatePreParse(cfg); err != nil {
				t.Errorf("ValidatePreParse() unexpected error: %v", err)
			}
		})
	}
}

func TestValidatePreParse_ValidConfig(t *testing.T) {
	cfg := validConfig()

	warnings, err := config.ValidatePreParse(cfg)
	if err != nil {
		t.Errorf("ValidatePreParse() unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("ValidatePreParse() warnings = %d, want 0", len(warnings))
	}
}

func TestValidatePreParse_SchemaDefaultStarNotWarning(t *testing.T) {
	// The default schema value "*" should NOT trigger a warning on MySQL/SQLite.
	cfg := validConfig()
	cfg.Input.Dialect = "mysql"
	cfg.Input.Schema = "*"
	cfg.Output.Driver = "stdlib"

	warnings, err := config.ValidatePreParse(cfg)
	if err != nil {
		t.Fatalf("ValidatePreParse() unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("ValidatePreParse() warnings = %d, want 0 for schema='*' on MySQL", len(warnings))
	}
}

// --- Post-Parse Validation Tests ---

// schemaTables returns a basic schema with two tables for post-parse validation testing.
// Both tables have an `id` primary key so the cursor_keys PK-fallback path is
// exercisable.
func schemaTables() []config.SchemaTable {
	return []config.SchemaTable{
		{
			Name:   "users",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "name", Type: "text"},
				{Name: "email", Type: "text"},
				{Name: "deleted_at", Type: "timestamp"},
			},
		},
		{
			Name:   "orders",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "user_id", Type: "bigint"},
				{Name: "total", Type: "numeric"},
				{Name: "created_at", Type: "timestamp"},
			},
		},
	}
}

// pklessSchemaTable returns a single table with no primary key, used to
// exercise the PRD §4.13 "no PK → omit Connection + warning" path.
func pklessSchemaTable() []config.SchemaTable {
	return []config.SchemaTable{
		{
			Name:   "audit_events",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "event_id", Type: "uuid"},
				{Name: "payload", Type: "text"},
				{Name: "recorded_at", Type: "timestamp"},
			},
		},
	}
}

// TestValidatePostParse_CursorKeys_InheritedMissingWithPK verifies that when
// the inherited global cursor_keys reference a column not present on a
// table that does have a PK, resolution silently falls back to the PK
// (no error, no warning) — PRD §4.13 rule 1.
func TestValidatePostParse_CursorKeys_InheritedMissingWithPK(t *testing.T) {
	cfg := validConfig()
	cfg.Generation.CursorKeys = []string{"nonexistent_col"}

	warnings, err := config.ValidatePostParse(cfg, schemaTables(), nil)
	if err != nil {
		t.Fatalf("ValidatePostParse() unexpected error: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w.Message, "cursor_keys") {
			t.Errorf("ValidatePostParse() unexpected cursor_keys warning: %q", w.Message)
		}
	}
}

// TestValidatePostParse_CursorKeys_InheritedAllPresent verifies that when
// the inherited global cursor_keys reference columns present on every
// table, no error or warning is produced.
func TestValidatePostParse_CursorKeys_InheritedAllPresent(t *testing.T) {
	cfg := validConfig()
	cfg.Generation.CursorKeys = []string{"id"}

	warnings, err := config.ValidatePostParse(cfg, schemaTables(), nil)
	if err != nil {
		t.Errorf("ValidatePostParse() unexpected error: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w.Message, "cursor_keys") {
			t.Errorf("ValidatePostParse() unexpected cursor_keys warning: %q", w.Message)
		}
	}
}

// TestValidatePostParse_CursorKeys_InheritedPartialMissing verifies that
// when the inherited global cursor_keys reference one column present on
// one table and missing on another, both tables resolve silently (fall
// back to PK on the missing-column side) — PRD §4.13 rule 1 + the partial-
// match full-fallback clarification.
func TestValidatePostParse_CursorKeys_InheritedPartialMissing(t *testing.T) {
	cfg := validConfig()
	cfg.Generation.CursorKeys = []string{"email"} // present on users, missing on orders

	warnings, err := config.ValidatePostParse(cfg, schemaTables(), nil)
	if err != nil {
		t.Fatalf("ValidatePostParse() unexpected error: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w.Message, "cursor_keys") {
			t.Errorf("ValidatePostParse() unexpected cursor_keys warning: %q", w.Message)
		}
	}
}

// TestValidatePostParse_CursorKeys_InheritedMissingNoPK verifies that when
// the inherited global cursor_keys reference a column not present on a
// table that also lacks a PK, resolution omits Connection and emits a
// warning naming the table — PRD §4.13 rule 2.
func TestValidatePostParse_CursorKeys_InheritedMissingNoPK(t *testing.T) {
	cfg := validConfig()
	cfg.Generation.CursorKeys = []string{"id"}

	warnings, err := config.ValidatePostParse(cfg, pklessSchemaTable(), nil)
	if err != nil {
		t.Fatalf("ValidatePostParse() unexpected error: %v", err)
	}
	var gotWarning string
	for _, w := range warnings {
		if strings.Contains(w.Message, "cursor_keys") {
			gotWarning = w.Message
			break
		}
	}
	if gotWarning == "" {
		t.Fatal("ValidatePostParse() warnings missing cursor_keys omission warning")
	}
	if !strings.Contains(gotWarning, "audit_events") {
		t.Errorf("ValidatePostParse() warning = %q, want substring naming audit_events", gotWarning)
	}
	if !strings.Contains(gotWarning, "omitted") {
		t.Errorf("ValidatePostParse() warning = %q, want substring 'omitted'", gotWarning)
	}
}

// TestValidatePostParse_CursorKeys_ExplicitTableMissing verifies that an
// explicit tables.<name>.cursor_keys entry referencing a missing column
// produces a hard validation error — PRD §4.13 rule 3.
func TestValidatePostParse_CursorKeys_ExplicitTableMissing(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["public.orders"] = config.TableConfig{
		CursorKeys: []string{"nonexistent_col"},
	}

	_, err := config.ValidatePostParse(cfg, schemaTables(), nil)
	if err == nil {
		t.Fatal("ValidatePostParse() error = nil, want error for explicit table cursor_keys missing column")
	}
	if !strings.Contains(err.Error(), "nonexistent_col") {
		t.Errorf("ValidatePostParse() error = %v, want error mentioning column name", err)
	}
	if !strings.Contains(err.Error(), "orders") {
		t.Errorf("ValidatePostParse() error = %v, want error mentioning table name", err)
	}
}

// TestValidatePostParse_CursorKeys_ExplicitTablePresent verifies that an
// explicit tables.<name>.cursor_keys override with all columns present
// passes validation — complement to rule 3.
func TestValidatePostParse_CursorKeys_ExplicitTablePresent(t *testing.T) {
	cfg := validConfig()
	cfg.Generation.CursorKeys = []string{"id"}
	cfg.Tables["public.orders"] = config.TableConfig{
		CursorKeys: []string{"user_id"},
	}

	_, err := config.ValidatePostParse(cfg, schemaTables(), nil)
	if err != nil {
		t.Errorf("ValidatePostParse() unexpected error: %v", err)
	}
}

// TestValidatePostParse_CursorKeys_ViewInheritedMissing verifies that when
// a view inherits the global default and any inherited column is missing
// from the view's projection, resolution omits Connection and emits a
// warning naming the view — PRD §4.13 rule 4 (views have no PK fallback).
func TestValidatePostParse_CursorKeys_ViewInheritedMissing(t *testing.T) {
	cfg := validConfig()
	cfg.Generation.CursorKeys = []string{"id"}

	views := []config.SchemaView{
		{
			Name:   "product_summary",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "name", Type: "text"},
				{Name: "total", Type: "numeric"},
			},
		},
	}

	warnings, err := config.ValidatePostParse(cfg, schemaTables(), views)
	if err != nil {
		t.Fatalf("ValidatePostParse() unexpected error: %v", err)
	}
	var gotWarning string
	for _, w := range warnings {
		if strings.Contains(w.Message, "cursor_keys") && strings.Contains(w.Message, "product_summary") {
			gotWarning = w.Message
			break
		}
	}
	if gotWarning == "" {
		t.Fatal("ValidatePostParse() warnings missing cursor_keys omission warning for view")
	}
	if !strings.Contains(gotWarning, "omitted") {
		t.Errorf("ValidatePostParse() warning = %q, want substring 'omitted'", gotWarning)
	}
}

// TestValidatePostParse_CursorKeys_ViewExplicitMissing verifies that an
// explicit views.<name>.cursor_keys entry referencing a missing column
// produces a hard validation error — PRD §4.13 rule 5.
func TestValidatePostParse_CursorKeys_ViewExplicitMissing(t *testing.T) {
	cfg := validConfig()
	cfg.Views["product_summary"] = config.ViewConfig{
		StructName: "ProductSummary",
		SQL:        "views/product_summary.sql",
		CursorKeys: []string{"nonexistent_col"},
	}

	views := []config.SchemaView{
		{
			Name:   "product_summary",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "bigint"},
				{Name: "total", Type: "numeric"},
			},
		},
	}

	_, err := config.ValidatePostParse(cfg, schemaTables(), views)
	if err == nil {
		t.Fatal("ValidatePostParse() error = nil, want error for explicit view cursor_keys missing column")
	}
	if !strings.Contains(err.Error(), "nonexistent_col") {
		t.Errorf("ValidatePostParse() error = %v, want error mentioning column name", err)
	}
	if !strings.Contains(err.Error(), "product_summary") {
		t.Errorf("ValidatePostParse() error = %v, want error mentioning view name", err)
	}
}

// TestValidatePostParse_CursorKeys_ViewExplicitPresent verifies that an
// explicit views.<name>.cursor_keys override with all columns present
// passes validation — complement to rule 5.
func TestValidatePostParse_CursorKeys_ViewExplicitPresent(t *testing.T) {
	cfg := validConfig()
	cfg.Views["product_summary"] = config.ViewConfig{
		StructName: "ProductSummary",
		SQL:        "views/product_summary.sql",
		CursorKeys: []string{"total"},
	}

	views := []config.SchemaView{
		{
			Name:   "product_summary",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "bigint"},
				{Name: "total", Type: "numeric"},
			},
		},
	}

	_, err := config.ValidatePostParse(cfg, schemaTables(), views)
	if err != nil {
		t.Errorf("ValidatePostParse() unexpected error: %v", err)
	}
}

func TestValidatePostParse_SoftDeleteTypeMismatch(t *testing.T) {
	cfg := validConfig()
	cfg.Generation.SoftDeleteColumns = []config.SoftDeleteConfig{
		{Name: "name", Type: "timestamp"}, // "name" exists in users but is text, not timestamp
	}

	tables := []config.SchemaTable{
		{
			Name:   "users",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "bigint"},
				{Name: "name", Type: "text"},
			},
		},
	}

	_, err := config.ValidatePostParse(cfg, tables, nil)
	if err == nil {
		t.Fatal("ValidatePostParse() error = nil, want error for soft delete type mismatch")
	}
	if !strings.Contains(err.Error(), "incompatible type") {
		t.Errorf("ValidatePostParse() error = %v, want error mentioning incompatible type", err)
	}
}

func TestValidatePostParse_SoftDeleteTypeMatch(t *testing.T) {
	tests := []struct {
		name    string
		colType string
		sdType  config.SoftDeleteType
	}{
		{name: "timestamp matches timestamp", colType: "timestamp", sdType: config.SoftDeleteTimestamp},
		{name: "timestamptz matches timestamp", colType: "timestamptz", sdType: config.SoftDeleteTimestamp},
		{name: "datetime matches timestamp", colType: "datetime", sdType: config.SoftDeleteTimestamp},
		{name: "timestamp with time zone matches timestamp", colType: "timestamp with time zone", sdType: config.SoftDeleteTimestamp},
		{name: "boolean matches bool", colType: "boolean", sdType: config.SoftDeleteBool},
		{name: "bool matches bool", colType: "bool", sdType: config.SoftDeleteBool},
		{name: "tinyint matches bool", colType: "tinyint", sdType: config.SoftDeleteBool},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Generation.SoftDeleteColumns = []config.SoftDeleteConfig{
				{Name: "soft_col", Type: tt.sdType},
			}

			tables := []config.SchemaTable{
				{
					Name:   "items",
					Schema: "public",
					Columns: []config.SchemaColumn{
						{Name: "id", Type: "bigint"},
						{Name: "soft_col", Type: tt.colType},
					},
				},
			}

			_, err := config.ValidatePostParse(cfg, tables, nil)
			if err != nil {
				t.Errorf("ValidatePostParse() unexpected error: %v", err)
			}
		})
	}
}

// The struct-name collision rules that used to live here moved to
// gen.validateResolvedNames. They are resolution-level: the
// lexical approximation config could reach (lowercase, drop underscores) both
// missed real collisions and rejected names that resolve apart, and config
// must not import gen to derive the real ones. See
// cmd/sqlgen/gen/resolved_names_test.go.

func TestValidatePostParse_ExcludeColumnsRemovesAll(t *testing.T) {
	cfg := validConfig()
	cfg.Generation.ExcludeColumns = []string{"id", "name"}

	tables := []config.SchemaTable{
		{
			Name:   "tiny",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "bigint"},
				{Name: "name", Type: "text"},
			},
		},
	}

	warnings, err := config.ValidatePostParse(cfg, tables, nil)
	if err != nil {
		t.Fatalf("ValidatePostParse() unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("ValidatePostParse() warnings = 0, want warning for all columns excluded")
	}
	if !strings.Contains(warnings[0].Message, "tiny") {
		t.Errorf("ValidatePostParse() warning = %q, want warning mentioning table name", warnings[0].Message)
	}
	if !strings.Contains(warnings[0].Message, "skipped") {
		t.Errorf("ValidatePostParse() warning = %q, want warning mentioning table will be skipped", warnings[0].Message)
	}
}

// TestValidatePostParse_PrimaryKeyOverride_MissingColumn pins PK-override rule #1:
// every column listed in tables.<name>.primary_key.columns must exist on the
// table (hard error).
func TestValidatePostParse_PrimaryKeyOverride_MissingColumn(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["counters"] = config.TableConfig{
		PrimaryKey: &config.TablePrimaryKeyConfig{
			Columns: []string{"organization_id", "nonexistent"},
		},
	}

	tables := []config.SchemaTable{
		{
			Name:   "counters",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "organization_id", Type: "bigint"},
				{Name: "count", Type: "bigint"},
			},
		},
	}

	_, err := config.ValidatePostParse(cfg, tables, nil)
	if err == nil {
		t.Fatal("ValidatePostParse() error = nil, want missing-column error")
	}
	if !strings.Contains(err.Error(), `column "nonexistent" does not exist`) {
		t.Errorf("ValidatePostParse() error = %q, want missing-column message", err.Error())
	}
}

// TestValidatePostParse_PrimaryKeyOverride_NullableColumn pins PK-override rule #2:
// override columns must be NOT NULL.
func TestValidatePostParse_PrimaryKeyOverride_NullableColumn(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["counters"] = config.TableConfig{
		PrimaryKey: &config.TablePrimaryKeyConfig{
			Columns: []string{"organization_id"},
		},
	}

	tables := []config.SchemaTable{
		{
			Name:   "counters",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "organization_id", Type: "bigint", Nullable: true},
				{Name: "count", Type: "bigint"},
			},
		},
	}

	_, err := config.ValidatePostParse(cfg, tables, nil)
	if err == nil {
		t.Fatal("ValidatePostParse() error = nil, want nullable-column error")
	}
	if !strings.Contains(err.Error(), `is nullable`) {
		t.Errorf("ValidatePostParse() error = %q, want nullable-column message", err.Error())
	}
}

// TestValidatePostParse_PrimaryKeyOverride_NoUniqueCoverage pins PK-override
// rule #3: when no UNIQUE constraint covers the declared PK set, emit a warning
// (not an error — uniqueness may be app-enforced).
func TestValidatePostParse_PrimaryKeyOverride_NoUniqueCoverage(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["counters"] = config.TableConfig{
		PrimaryKey: &config.TablePrimaryKeyConfig{
			Columns: []string{"organization_id"},
		},
	}

	tables := []config.SchemaTable{
		{
			Name:   "counters",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "organization_id", Type: "bigint"},
				{Name: "count", Type: "bigint"},
			},
			// No UniqueGroups, no inline Unique flags.
		},
	}

	warnings, err := config.ValidatePostParse(cfg, tables, nil)
	if err != nil {
		t.Fatalf("ValidatePostParse() unexpected error: %v", err)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w.Message, "no UNIQUE constraint covers") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ValidatePostParse() warnings = %v, want no-UNIQUE-coverage warning", warnings)
	}
}

// TestValidatePostParse_PrimaryKeyOverride_UniqueCovered confirms the warning
// is silent when a UNIQUE constraint covers the override column set.
func TestValidatePostParse_PrimaryKeyOverride_UniqueCovered(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["counters"] = config.TableConfig{
		PrimaryKey: &config.TablePrimaryKeyConfig{
			Columns: []string{"organization_id"},
		},
	}

	tables := []config.SchemaTable{
		{
			Name:   "counters",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "organization_id", Type: "bigint", Unique: true},
				{Name: "count", Type: "bigint"},
			},
		},
	}

	warnings, err := config.ValidatePostParse(cfg, tables, nil)
	if err != nil {
		t.Fatalf("ValidatePostParse() unexpected error: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w.Message, "no UNIQUE constraint covers") {
			t.Errorf("ValidatePostParse() emitted no-UNIQUE warning despite Unique:true column: %q", w.Message)
		}
	}
}

// TestValidatePostParse_PrimaryKeyOverride_CompositeUniqueCovered confirms a
// table-level UNIQUE (a, b) constraint covers a composite override [a, b]
// regardless of order.
func TestValidatePostParse_PrimaryKeyOverride_CompositeUniqueCovered(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["junction"] = config.TableConfig{
		PrimaryKey: &config.TablePrimaryKeyConfig{
			Columns: []string{"asset_id", "ppa_id"},
		},
	}

	tables := []config.SchemaTable{
		{
			Name:   "junction",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "asset_id", Type: "bigint"},
				{Name: "ppa_id", Type: "bigint"},
			},
			UniqueGroups: [][]string{{"ppa_id", "asset_id"}}, // declared in different order
		},
	}

	warnings, err := config.ValidatePostParse(cfg, tables, nil)
	if err != nil {
		t.Fatalf("ValidatePostParse() unexpected error: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w.Message, "no UNIQUE constraint covers") {
			t.Errorf("ValidatePostParse() emitted no-UNIQUE warning despite covering UniqueGroup: %q", w.Message)
		}
	}
}

// TestValidatePostParse_MissingPrimaryKey pins the no-PK skip warning: a
// table with no auto-detected PK and no override gets a warning directing
// the user toward either declaring the PK via primary_key.columns or
// dropping the table via top-level exclude_tables (PRD §9.4b).
func TestValidatePostParse_MissingPrimaryKey(t *testing.T) {
	cfg := validConfig()
	tables := []config.SchemaTable{
		{
			Name:   "counters",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "name", Type: "text"},
				{Name: "count", Type: "bigint"},
			},
		},
	}

	warnings, err := config.ValidatePostParse(cfg, tables, nil)
	if err != nil {
		t.Fatalf("ValidatePostParse() unexpected error: %v", err)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w.Message, "skipped from generation") &&
			strings.Contains(w.Message, "counters") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ValidatePostParse() warnings = %v, want no-PK skip warning", warnings)
	}
}

// TestValidatePostParse_MissingPrimaryKey_SilentWithOverride confirms the
// missing-PK warning does NOT fire when an override is supplied.
func TestValidatePostParse_MissingPrimaryKey_SilentWithOverride(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["counters"] = config.TableConfig{
		PrimaryKey: &config.TablePrimaryKeyConfig{
			Columns: []string{"organization_id"},
		},
	}
	tables := []config.SchemaTable{
		{
			Name:   "counters",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "organization_id", Type: "bigint", Unique: true},
				{Name: "count", Type: "bigint"},
			},
		},
	}

	warnings, err := config.ValidatePostParse(cfg, tables, nil)
	if err != nil {
		t.Fatalf("ValidatePostParse() unexpected error: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w.Message, "skipped from generation") {
			t.Errorf("ValidatePostParse() emitted skip warning despite primary_key.columns override: %q", w.Message)
		}
	}
}

// TestValidatePostParse_PrimaryKeyOverride_SilentWhenAutoPKExists confirms
// the override silently wins when both an auto-detected PK AND an override
// are present (no warning, no error — the user's declaration is authoritative,
// PRD §8.6).
func TestValidatePostParse_PrimaryKeyOverride_SilentWhenAutoPKExists(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["thing"] = config.TableConfig{
		PrimaryKey: &config.TablePrimaryKeyConfig{
			Columns: []string{"natural_key"},
		},
	}
	tables := []config.SchemaTable{
		{
			Name:   "thing",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "natural_key", Type: "text", Unique: true},
			},
		},
	}

	warnings, err := config.ValidatePostParse(cfg, tables, nil)
	if err != nil {
		t.Fatalf("ValidatePostParse() unexpected error: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w.Message, "primary_key.columns") &&
			strings.Contains(w.Message, "auto-detected") {
			t.Errorf("ValidatePostParse() unexpected override-conflict warning: %q", w.Message)
		}
	}
}

// TestValidatePostParse_CompositePKStrategy pins that a composite key
// is always caller-provided (PRD §8.6), and the generator has no per-column
// strategy, so `db` or `app` on one generated Create / CreateMany / Upsert /
// UpsertMany that read key fields the create input does not carry, and the
// package did not compile. The rule covers a key from the schema and one from
// a primary_key.columns override, and leaves single-column keys alone.
func TestValidatePostParse_CompositePKStrategy(t *testing.T) {
	compositeTable := config.SchemaTable{
		Name:   "timeline_events",
		Schema: "public",
		Columns: []config.SchemaColumn{
			{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
			{Name: "created_at", Type: "timestamptz", PrimaryKey: true, HasDefault: true},
			{Name: "payload", Type: "text"},
		},
	}
	tests := []struct {
		name    string
		pk      config.TablePrimaryKeyConfig
		wantErr string
	}{
		{
			name:    "db on a schema composite key",
			pk:      config.TablePrimaryKeyConfig{Strategy: config.PKStrategyDB},
			wantErr: `tables.public.timeline_events.primary_key.strategy: "db" is not supported on a composite primary key (id, created_at)`,
		},
		{
			name:    "app on a schema composite key",
			pk:      config.TablePrimaryKeyConfig{Strategy: config.PKStrategyApp},
			wantErr: `tables.public.timeline_events.primary_key.strategy: "app" is not supported on a composite primary key (id, created_at)`,
		},
		{
			name:    "db on a composite columns override",
			pk:      config.TablePrimaryKeyConfig{Strategy: config.PKStrategyDB, Columns: []string{"id", "payload"}},
			wantErr: `"db" is not supported on a composite primary key (id, payload)`,
		},
		{
			name: "caller on a composite key",
			pk:   config.TablePrimaryKeyConfig{Strategy: config.PKStrategyCaller},
		},
		{
			name: "db on a single-column override",
			pk:   config.TablePrimaryKeyConfig{Strategy: config.PKStrategyDB, Columns: []string{"id"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables["public.timeline_events"] = config.TableConfig{PrimaryKey: &tt.pk}

			_, err := config.ValidatePostParse(cfg, []config.SchemaTable{compositeTable}, nil)
			if tt.wantErr == "" {
				if err != nil && strings.Contains(err.Error(), "composite primary key") {
					t.Errorf("ValidatePostParse() = %v, want no composite-key error", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidatePostParse() = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePostParse_AmbiguousBareTableName(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["products"] = config.TableConfig{} // bare key

	tables := []config.SchemaTable{
		{Name: "products", Schema: "public", Columns: []config.SchemaColumn{{Name: "id", Type: "bigint"}}},
		{Name: "products", Schema: "billing", Columns: []config.SchemaColumn{{Name: "id", Type: "bigint"}}},
	}

	_, err := config.ValidatePostParse(cfg, tables, nil)
	if err == nil {
		t.Fatal("ValidatePostParse() error = nil, want error for ambiguous bare table name")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("ValidatePostParse() error = %v, want error mentioning ambiguous", err)
	}
	if !strings.Contains(err.Error(), "products") {
		t.Errorf("ValidatePostParse() error = %v, want error mentioning table name", err)
	}
}

func TestValidatePostParse_BareTableNameSingleSchema(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["products"] = config.TableConfig{} // bare key

	tables := []config.SchemaTable{
		{Name: "products", Schema: "public", Columns: []config.SchemaColumn{{Name: "id", Type: "bigint"}}},
	}

	_, err := config.ValidatePostParse(cfg, tables, nil)
	if err != nil {
		t.Errorf("ValidatePostParse() unexpected error: %v", err)
	}
}

func TestValidatePostParse_MySQLBareKeysNoAmbiguity(t *testing.T) {
	cfg := validConfig()
	cfg.Input.Dialect = "mysql"
	cfg.Output.Driver = "stdlib"
	cfg.Tables["products"] = config.TableConfig{}

	// Even with duplicate names (shouldn't happen in MySQL), no ambiguity error.
	tables := []config.SchemaTable{
		{Name: "products", Schema: "", Columns: []config.SchemaColumn{{Name: "id", Type: "bigint"}}},
	}

	_, err := config.ValidatePostParse(cfg, tables, nil)
	if err != nil {
		t.Errorf("ValidatePostParse() unexpected error: %v", err)
	}
}

func TestValidatePostParse_MultipleErrors(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["public.users"] = config.TableConfig{
		// Explicit table-level cursor_keys must reference a real column —
		// produces a hard error per PRD §4.13 rule 3.
		CursorKeys: []string{"nonexistent"},
	}
	cfg.Generation.SoftDeleteColumns = []config.SoftDeleteConfig{
		{Name: "name", Type: "timestamp"},
	}

	tables := []config.SchemaTable{
		{
			Name:   "users",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "name", Type: "varchar"},
			},
		},
	}

	_, err := config.ValidatePostParse(cfg, tables, nil)
	if err == nil {
		t.Fatal("ValidatePostParse() error = nil, want multiple errors")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cursor_keys") {
		t.Errorf("ValidatePostParse() error missing cursor_keys error: %v", err)
	}
	if !strings.Contains(msg, "soft_delete") {
		t.Errorf("ValidatePostParse() error missing soft_delete error: %v", err)
	}
}

func TestValidatePreParse_EnumValues(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*config.RootConfig)
		wantErr     bool
		wantAllowed string // substring that should appear in the error's allowed-set hint
	}{
		{
			name:        "invalid dialect V7-style casing",
			mutate:      func(c *config.RootConfig) { c.Input.Dialect = "Postgres" },
			wantErr:     true,
			wantAllowed: "postgres, mysql, sqlite",
		},
		{
			name:        "invalid source",
			mutate:      func(c *config.RootConfig) { c.Input.Source = "FILES" },
			wantErr:     true,
			wantAllowed: "files, database, both",
		},
		{
			name:        "invalid parse_mode",
			mutate:      func(c *config.RootConfig) { c.Input.ParseMode = "lenient" },
			wantErr:     true,
			wantAllowed: "strict, merge",
		},
		{
			name:        "invalid driver",
			mutate:      func(c *config.RootConfig) { c.Output.Driver = "PGX" },
			wantErr:     true,
			wantAllowed: "pgx, stdlib",
		},
		{
			name:        "invalid layout",
			mutate:      func(c *config.RootConfig) { c.Output.Layout = "per-table" },
			wantErr:     true,
			wantAllowed: "single_file, file_per_table",
		},
		{
			name:        "invalid uuid_version uppercase",
			mutate:      func(c *config.RootConfig) { c.Generation.UUIDVersion = "V7" },
			wantErr:     true,
			wantAllowed: "v4, v7",
		},
		{
			name: "invalid soft_delete type",
			mutate: func(c *config.RootConfig) {
				c.Generation.SoftDeleteColumns = []config.SoftDeleteConfig{
					{Name: "deleted_at", Type: "TIMESTAMP"},
				}
			},
			wantErr:     true,
			wantAllowed: "timestamp, bool, integer",
		},
		{
			name: "invalid table primary_key strategy",
			mutate: func(c *config.RootConfig) {
				c.Tables["users"] = config.TableConfig{
					PrimaryKey: &config.TablePrimaryKeyConfig{Strategy: "APP"},
				}
			},
			wantErr:     true,
			wantAllowed: "db, app, caller",
		},
		{
			name: "invalid table primary_key uuid_version",
			mutate: func(c *config.RootConfig) {
				c.Tables["users"] = config.TableConfig{
					PrimaryKey: &config.TablePrimaryKeyConfig{UUIDVersion: "V4"},
				}
			},
			wantErr:     true,
			wantAllowed: "v4, v7",
		},
		{
			name: "valid dialect lowercase",
			mutate: func(c *config.RootConfig) {
				c.Input.Dialect = config.DialectMySQL
				c.Output.Driver = config.DriverStdlib
			},
			wantErr: false,
		},
		{
			name:    "empty values skipped (defaults apply)",
			mutate:  func(c *config.RootConfig) { c.Input.ParseMode = ""; c.Generation.UUIDVersion = "" },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)

			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidatePreParse() error = nil, want error")
				}
				if tt.wantAllowed != "" && !strings.Contains(err.Error(), tt.wantAllowed) {
					t.Errorf("ValidatePreParse() error = %v, want allowed-set hint %q", err, tt.wantAllowed)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidatePreParse() unexpected error: %v", err)
			}
		})
	}
}

func TestValidatePreParse_CacheConfig(t *testing.T) {
	msgpack := config.SerializerMsgpack

	tests := []struct {
		name       string
		cache      *config.CacheConfig
		views      map[string]config.ViewConfig
		wantErr    bool
		wantSubstr string
	}{
		{
			name: "valid cache config",
			cache: &config.CacheConfig{
				Enabled: true, TTL: "30m", Serializer: msgpack, KeyPrefix: "app",
				Hydration:      &config.HydrationConfig{Enabled: true, Timeout: "10s"},
				CircuitBreaker: &config.CircuitBreakerConfig{Enabled: true, FailureThreshold: 3, ProbeInterval: "15s", HalfOpenMaxProbes: 2},
			},
			wantErr: false,
		},
		{
			name:       "invalid ttl",
			cache:      &config.CacheConfig{TTL: "notaduration", KeyPrefix: "app", CircuitBreaker: &config.CircuitBreakerConfig{FailureThreshold: 1, ProbeInterval: "1s", HalfOpenMaxProbes: 1}},
			wantErr:    true,
			wantSubstr: "cache.ttl",
		},
		{
			name:       "invalid serializer",
			cache:      &config.CacheConfig{Serializer: "bogus", KeyPrefix: "app", CircuitBreaker: &config.CircuitBreakerConfig{FailureThreshold: 1, ProbeInterval: "1s", HalfOpenMaxProbes: 1}},
			wantErr:    true,
			wantSubstr: "cache.serializer",
		},
		{
			name: "invalid hydration timeout",
			cache: &config.CacheConfig{
				KeyPrefix:      "app",
				Hydration:      &config.HydrationConfig{Timeout: "abc"},
				CircuitBreaker: &config.CircuitBreakerConfig{FailureThreshold: 1, ProbeInterval: "1s", HalfOpenMaxProbes: 1},
			},
			wantErr:    true,
			wantSubstr: "cache.hydration.timeout",
		},
		{
			name: "circuit_breaker failure_threshold below 1",
			cache: &config.CacheConfig{
				KeyPrefix:      "app",
				CircuitBreaker: &config.CircuitBreakerConfig{FailureThreshold: 0, ProbeInterval: "1s", HalfOpenMaxProbes: 1},
			},
			wantErr:    true,
			wantSubstr: "failure_threshold",
		},
		{
			name: "circuit_breaker probe_interval zero",
			cache: &config.CacheConfig{
				KeyPrefix:      "app",
				CircuitBreaker: &config.CircuitBreakerConfig{FailureThreshold: 1, ProbeInterval: "0s", HalfOpenMaxProbes: 1},
			},
			wantErr:    true,
			wantSubstr: "probe_interval",
		},
		{
			name: "circuit_breaker half_open_max_probes below 1",
			cache: &config.CacheConfig{
				KeyPrefix:      "app",
				CircuitBreaker: &config.CircuitBreakerConfig{FailureThreshold: 1, ProbeInterval: "1s", HalfOpenMaxProbes: 0},
			},
			wantErr:    true,
			wantSubstr: "half_open_max_probes",
		},
		{
			name:  "view cache enabled without invalidate_on",
			cache: &config.CacheConfig{Enabled: true, KeyPrefix: "app", CircuitBreaker: &config.CircuitBreakerConfig{FailureThreshold: 1, ProbeInterval: "1s", HalfOpenMaxProbes: 1}},
			views: map[string]config.ViewConfig{
				"orphan_view": {
					StructName: "OrphanView",
					Cache:      &config.ViewCacheConfig{Enabled: new(true)},
				},
			},
			wantErr:    true,
			wantSubstr: "views.orphan_view.cache.enabled",
		},
		{
			name:  "view cache enabled false skips invalidate_on requirement",
			cache: &config.CacheConfig{Enabled: true, KeyPrefix: "app", CircuitBreaker: &config.CircuitBreakerConfig{FailureThreshold: 1, ProbeInterval: "1s", HalfOpenMaxProbes: 1}},
			views: map[string]config.ViewConfig{
				"v": {StructName: "V", Cache: &config.ViewCacheConfig{Enabled: new(false)}},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Cache = tt.cache
			if tt.views != nil {
				cfg.Views = tt.views
			}

			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidatePreParse() error = nil, want error containing %q", tt.wantSubstr)
				}
				if tt.wantSubstr != "" && !strings.Contains(err.Error(), tt.wantSubstr) {
					t.Errorf("ValidatePreParse() error = %v, want substring %q", err, tt.wantSubstr)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidatePreParse() unexpected error: %v", err)
			}
		})
	}
}

func TestValidatePostParse_ViewInvalidateOn(t *testing.T) {
	schemaTables := []config.SchemaTable{
		{
			Name:   "products",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "uuid"},
				{Name: "name", Type: "text"},
			},
		},
		{
			Name:   "reviews",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "uuid"},
				{Name: "body", Type: "text"},
			},
		},
	}

	tests := []struct {
		name       string
		view       config.ViewConfig
		mutate     func(c *config.RootConfig)
		wantErr    bool
		wantSubstr string
	}{
		{
			name: "invalidate_on resolves to parsed tables",
			view: config.ViewConfig{
				StructName:   "ProductSummary",
				Cache:        &config.ViewCacheConfig{Enabled: new(true)},
				InvalidateOn: []string{"products", "reviews"},
			},
			wantErr: false,
		},
		{
			name: "invalidate_on resolves via schema-qualified name",
			view: config.ViewConfig{
				StructName:   "ProductSummary",
				Cache:        &config.ViewCacheConfig{Enabled: new(true)},
				InvalidateOn: []string{"public.products"},
			},
			wantErr: false,
		},
		{
			name: "unknown table name is hard error",
			view: config.ViewConfig{
				StructName:   "ProductSummary",
				Cache:        &config.ViewCacheConfig{Enabled: new(true)},
				InvalidateOn: []string{"unknown_table"},
			},
			wantErr:    true,
			wantSubstr: `"unknown_table"`,
		},
		{
			name: "excluded table (all columns filtered) is hard error",
			view: config.ViewConfig{
				StructName:   "ProductSummary",
				Cache:        &config.ViewCacheConfig{Enabled: new(true)},
				InvalidateOn: []string{"reviews"},
			},
			mutate: func(c *config.RootConfig) {
				c.Tables["reviews"] = config.TableConfig{
					ExcludeColumns: []string{"id", "body"},
				}
			},
			wantErr:    true,
			wantSubstr: "excluded from generation",
		},
		{
			name: "cache disabled skips invalidate_on validation",
			view: config.ViewConfig{
				StructName:   "ProductSummary",
				Cache:        &config.ViewCacheConfig{Enabled: new(false)},
				InvalidateOn: []string{"unknown_table"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Views["product_summary"] = tt.view
			if tt.mutate != nil {
				tt.mutate(cfg)
			}

			_, err := config.ValidatePostParse(cfg, schemaTables, nil)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidatePostParse() error = nil, want error containing %q", tt.wantSubstr)
				}
				if tt.wantSubstr != "" && !strings.Contains(err.Error(), tt.wantSubstr) {
					t.Errorf("ValidatePostParse() error = %v, want substring %q", err, tt.wantSubstr)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidatePostParse() unexpected error: %v", err)
			}
		})
	}
}

// TestValidatePreParse_Relationships_DedupRules pins PRD §13.7.3's two
// relationship dedup rules and §4.8's distinct-name requirement.
//
// The dedup key extends from §4.8's (target, fk) shape to include the
// raw `filter:` string byte-equal. The shared-prefix-with-distinct-filter
// case is what sub-categorized polymorphism relies on — without
// the relaxation, the second sub-category would be rejected as a duplicate.
func TestValidatePreParse_Relationships_DedupRules(t *testing.T) {
	tests := []struct {
		name        string
		rels        []config.TableRelationship
		wantErr     bool
		wantSubstr  string
		wantSubstr2 string
	}{
		{
			name: "duplicate full key (o2o) → error",
			rels: []config.TableRelationship{
				{Name: "Primary", Type: "o2o", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.primary'"},
				{Name: "PrimaryDup", Type: "o2o", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.primary'"},
			},
			wantErr:    true,
			wantSubstr: "PRD §13.7.3",
		},
		{
			name: "duplicate full key (o2m) → error",
			rels: []config.TableRelationship{
				{Name: "Attachments", Type: "o2m", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.attachment'"},
				{Name: "AttachmentsDup", Type: "o2m", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.attachment'"},
			},
			wantErr:    true,
			wantSubstr: "PRD §13.7.3",
		},
		{
			name: "duplicate full key (m2m) → error",
			rels: []config.TableRelationship{
				{Name: "Tags", Type: "m2m", Table: "tags", Junction: "product_tags", JunctionLocalFK: "product_id", JunctionReferenceFK: "tag_id"},
				{Name: "TagsDup", Type: "m2m", Table: "tags", Junction: "product_tags", JunctionLocalFK: "product_id", JunctionReferenceFK: "tag_id"},
			},
			wantErr:    true,
			wantSubstr: "PRD §13.7.3",
		},
		{
			name: "distinct filter on shared prefix (o2m) → allowed",
			rels: []config.TableRelationship{
				{Name: "PrimaryDocument", Type: "o2o", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.primary'"},
				{Name: "Attachments", Type: "o2m", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.attachment'"},
				{Name: "Invoices", Type: "o2m", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.invoice'"},
			},
			wantErr: false,
		},
		{
			name: "distinct filter on m2m shared prefix → allowed",
			rels: []config.TableRelationship{
				{Name: "ActiveTags", Type: "m2m", Table: "tags", Junction: "product_tags", JunctionLocalFK: "product_id", JunctionReferenceFK: "tag_id", Filter: "tags.active = true"},
				{Name: "ArchivedTags", Type: "m2m", Table: "tags", Junction: "product_tags", JunctionLocalFK: "product_id", JunctionReferenceFK: "tag_id", Filter: "tags.active = false"},
			},
			wantErr: false,
		},
		{
			name: "shared prefix, one empty filter, one set → allowed",
			rels: []config.TableRelationship{
				{Name: "AllDocuments", Type: "o2m", Table: "documents", FK: "entity_id"},
				{Name: "Invoices", Type: "o2m", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.invoice'"},
			},
			wantErr: false,
		},
		{
			name: "duplicate name on shared prefix → error (PRD §4.8)",
			rels: []config.TableRelationship{
				{Name: "Documents", Type: "o2m", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.primary'"},
				{Name: "Documents", Type: "o2m", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.attachment'"},
			},
			wantErr:    true,
			wantSubstr: "PRD §4.8",
		},
		{
			name: "single relationship without filter → allowed",
			rels: []config.TableRelationship{
				{Name: "Comments", Type: "o2m", Table: "comments", FK: "commentable_id"},
			},
			wantErr: false,
		},
		{
			name: "filter strings differing only in whitespace → distinct (byte-equal compare)",
			rels: []config.TableRelationship{
				{Name: "A", Type: "o2m", Table: "documents", FK: "entity_id", Filter: "entity_type='x'"},
				{Name: "B", Type: "o2m", Table: "documents", FK: "entity_id", Filter: "entity_type = 'x'"},
			},
			wantErr: false,
		},
		{
			// PRD §13.7.3 bundles o2o and o2m under a single key shape
			// `(target, fk, filter)` — declaring the same (target, fk, filter)
			// once as o2o and once as o2m is a duplicate, since `type` is
			// intentionally not part of the dedup key.
			name: "duplicate (target, fk, filter) with mixed o2o/o2m → error",
			rels: []config.TableRelationship{
				{Name: "Primary", Type: "o2o", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.primary'"},
				{Name: "PrimaryO2M", Type: "o2m", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.primary'"},
			},
			wantErr:    true,
			wantSubstr: "PRD §13.7.3",
		},
		{
			// `parseRelationshipType` (cmd/sqlgen/gen/context_table.go)
			// accepts `one_to_one`↔`o2o` as synonyms; the dedup key must
			// treat them as identical so a config mixing forms can't slip
			// duplicate declarations past validation.
			name: "duplicate full key across type-synonym variants → error",
			rels: []config.TableRelationship{
				{Name: "First", Type: "o2o", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.primary'"},
				{Name: "Second", Type: "one_to_one", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.primary'"},
			},
			wantErr:    true,
			wantSubstr: "PRD §13.7.3",
		},
		{
			// A typo in `side:` must surface as a hard
			// error rather than silently defaulting to "parent" — a typo
			// like `chld` would otherwise flip the inverse-side
			// relationship's manifest kind from "m2o" back to "o2o" with
			// no diagnosis.
			name: "unknown side value → error",
			rels: []config.TableRelationship{
				{Name: "User", Type: "o2o", Table: "users", FK: "profile_id", Side: "chld"},
			},
			wantErr:     true,
			wantSubstr:  "side",
			wantSubstr2: "PRD §4.8",
		},
		{
			name: "explicit side: parent → allowed",
			rels: []config.TableRelationship{
				{Name: "Profile", Type: "o2o", Table: "profiles", FK: "profile_id", Side: "parent"},
			},
			wantErr: false,
		},
		{
			name: "explicit side: child → allowed",
			rels: []config.TableRelationship{
				{Name: "User", Type: "o2o", Table: "users", FK: "profile_id", Side: "child"},
			},
			wantErr: false,
		},
		{
			name: "omitted side → allowed (defaults to parent)",
			rels: []config.TableRelationship{
				{Name: "Profile", Type: "o2o", Table: "profiles", FK: "profile_id"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables["asset"] = config.TableConfig{Relationships: tt.rels}

			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidatePreParse() error = nil, want error containing %q", tt.wantSubstr)
				}
				if tt.wantSubstr != "" && !strings.Contains(err.Error(), tt.wantSubstr) {
					t.Errorf("ValidatePreParse() error = %v, want substring %q", err, tt.wantSubstr)
				}
				if tt.wantSubstr2 != "" && !strings.Contains(err.Error(), tt.wantSubstr2) {
					t.Errorf("ValidatePreParse() error = %v, want substring %q", err, tt.wantSubstr2)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidatePreParse() unexpected error: %v", err)
			}
		})
	}
}

// TestValidatePreParse_RelationshipType pins that `type:` is required and
// drawn from the six spellings parseRelationshipType maps. Anything else used
// to fall through to one-to-many, so `one-to-one` generated a slice field with
// no diagnostic.
func TestValidatePreParse_RelationshipType(t *testing.T) {
	const allowed = "(allowed: o2o, one_to_one, o2m, one_to_many, m2m, many_to_many) (PRD §4.8)"
	tests := []struct {
		name    string
		rel     config.TableRelationship
		wantErr string // empty means the config validates
	}{
		{name: "o2o", rel: config.TableRelationship{Name: "Owner", Type: "o2o", Table: "users", FK: "owner_id"}},
		{name: "one_to_one", rel: config.TableRelationship{Name: "Owner", Type: "one_to_one", Table: "users", FK: "owner_id"}},
		{name: "o2m", rel: config.TableRelationship{Name: "Documents", Type: "o2m", Table: "documents", FK: "asset_id"}},
		{name: "one_to_many", rel: config.TableRelationship{Name: "Documents", Type: "one_to_many", Table: "documents", FK: "asset_id"}},
		{name: "m2m", rel: config.TableRelationship{Name: "Tags", Type: "m2m", Table: "tags", Junction: "asset_tags"}},
		{name: "many_to_many", rel: config.TableRelationship{Name: "Tags", Type: "many_to_many", Table: "tags", Junction: "asset_tags"}},
		{
			// parseRelationshipType folds case, so the validator does too.
			name: "upper-case spelling",
			rel:  config.TableRelationship{Name: "Owner", Type: "O2O", Table: "users", FK: "owner_id"},
		},
		{
			name:    "hyphenated spelling",
			rel:     config.TableRelationship{Name: "Owner", Type: "one-to-one", Table: "users", FK: "owner_id"},
			wantErr: `tables.asset.relationships[0].type: "one-to-one" is not a valid value ` + allowed,
		},
		{
			name:    "ratio spelling",
			rel:     config.TableRelationship{Name: "Documents", Type: "1:n", Table: "documents", FK: "asset_id"},
			wantErr: `tables.asset.relationships[0].type: "1:n" is not a valid value ` + allowed,
		},
		{
			name:    "missing underscores",
			rel:     config.TableRelationship{Name: "Tags", Type: "manytomany", Table: "tags", Junction: "asset_tags"},
			wantErr: `tables.asset.relationships[0].type: "manytomany" is not a valid value ` + allowed,
		},
		{
			// parseRelationshipType does not trim, so " o2o" would generate an
			// O2M edge; it must fail here rather than pass.
			name:    "padded spelling",
			rel:     config.TableRelationship{Name: "Owner", Type: " o2o", Table: "users", FK: "owner_id"},
			wantErr: `tables.asset.relationships[0].type: " o2o" is not a valid value ` + allowed,
		},
		{
			// PRD §4.8 marks `type` required; it no longer defaults to o2m.
			name:    "missing type",
			rel:     config.TableRelationship{Name: "Documents", Table: "documents", FK: "asset_id"},
			wantErr: "tables.asset.relationships[0].type: required " + allowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables["asset"] = config.TableConfig{Relationships: []config.TableRelationship{tt.rel}}

			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidatePreParse() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePreParse() error = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidatePreParse() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

// TestValidatePreParse_RelationshipLinkKeyRequired pins PRD §4.8, which
// marks `fk` required on o2o and o2m, and `junction` on m2m. An omitted one
// used to pass validate, then the o2m edge generated a package that did not
// parse, the o2o edge a JOIN on an empty identifier, and the m2m edge a
// package that did not compile.
func TestValidatePreParse_RelationshipLinkKeyRequired(t *testing.T) {
	const (
		required         = ".fk: required on an o2o or o2m relationship"
		junctionRequired = ".junction: required on an m2m relationship"
	)
	tests := []struct {
		name string
		rel  config.TableRelationship
		want []string // nil means the config validates
	}{
		{
			name: "o2m without fk",
			rel:  config.TableRelationship{Name: "Profiles", Type: "o2m", Table: "owner_profiles"},
			want: []string{`tables.owners.relationships[0] ("Profiles")` + required, "PRD §4.8 / §4.13"},
		},
		{
			name: "one_to_many without fk",
			rel:  config.TableRelationship{Name: "Profiles", Type: "one_to_many", Table: "owner_profiles"},
			want: []string{`tables.owners.relationships[0] ("Profiles")` + required},
		},
		{
			name: "o2o without fk",
			rel:  config.TableRelationship{Name: "Profile", Type: "o2o", Table: "owner_profiles"},
			want: []string{`tables.owners.relationships[0] ("Profile")` + required},
		},
		{
			name: "upper-case one_to_one without fk",
			rel:  config.TableRelationship{Name: "Profile", Type: "ONE_TO_ONE", Table: "owner_profiles"},
			want: []string{`tables.owners.relationships[0] ("Profile")` + required},
		},
		{
			name: "child-side o2o without fk",
			rel:  config.TableRelationship{Name: "Owner", Type: "o2o", Side: "child", Table: "owner_profiles"},
			want: []string{`tables.owners.relationships[0] ("Owner")` + required},
		},
		{
			name: "o2m with fk",
			rel:  config.TableRelationship{Name: "Profiles", Type: "o2m", Table: "owner_profiles", FK: "owner_id"},
		},
		{
			name: "o2o with fk",
			rel:  config.TableRelationship{Name: "Profile", Type: "o2o", Table: "owner_profiles", FK: "owner_id"},
		},
		{
			name: "m2m takes no fk",
			rel:  config.TableRelationship{Name: "Tags", Type: "m2m", Table: "tags", Junction: "owner_tags"},
		},
		{
			name: "m2m without junction",
			rel:  config.TableRelationship{Name: "Tags", Type: "m2m", Table: "tags"},
			want: []string{`tables.owners.relationships[0] ("Tags")` + junctionRequired, "PRD §4.8 / §4.13"},
		},
		{
			// An fk does not stand in for the junction: m2m never reads it.
			name: "many_to_many with fk but no junction",
			rel:  config.TableRelationship{Name: "Tags", Type: "many_to_many", Table: "tags", FK: "owner_id"},
			want: []string{`tables.owners.relationships[0] ("Tags")` + junctionRequired},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables["owners"] = config.TableConfig{Relationships: []config.TableRelationship{tt.rel}}

			_, err := config.ValidatePreParse(cfg)
			if tt.want == nil {
				if err != nil {
					t.Errorf("ValidatePreParse() = %v, want nil", err)
				}
				return
			}
			assertErrContains(t, err, "ValidatePreParse()", tt.want...)
		})
	}

	// A missing or unknown type is reported by itself: both link-key rules
	// depend on the type, so neither adds a second error for the same entry.
	for _, typ := range []string{"", "one-to-many", "many-to-many"} {
		cfg := validConfig()
		cfg.Tables["owners"] = config.TableConfig{Relationships: []config.TableRelationship{
			{Name: "Profiles", Type: typ, Table: "owner_profiles"},
		}}
		_, err := config.ValidatePreParse(cfg)
		if err == nil || strings.Contains(err.Error(), required) || strings.Contains(err.Error(), junctionRequired) {
			t.Errorf("ValidatePreParse() with type %q = %v, want the type error alone", typ, err)
		}
	}
}

// ownersSchema mirrors the sqlite example's has-one pair: the parent
// spells its key owner_key, and owner_profiles carries the owner_id FK. The
// parent's profile_id stands in for a belongs-to FK on this side.
func ownersSchema() []config.SchemaTable {
	return []config.SchemaTable{
		{
			Name: "owners",
			Columns: []config.SchemaColumn{
				{Name: "owner_key", Type: "integer", PrimaryKey: true},
				{Name: "name", Type: "text"},
				{Name: "profile_id", Type: "integer"},
			},
		},
		{
			Name: "owner_profiles",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "owner_id", Type: "integer"},
				{Name: "bio", Type: "text"},
			},
		},
	}
}

// TestValidatePostParse_RelationshipFKColumn pins PRD §4.8, which puts an
// o2m `fk` on the related table, and an o2o `fk` on this table or on the
// related one (§13.1). A column on neither side used to pass validate, then
// fail in the gqlgen compile (o2m) or on the edge's first load (o2o).
func TestValidatePostParse_RelationshipFKColumn(t *testing.T) {
	tests := []struct {
		name string
		rel  config.TableRelationship
		want []string // nil means no error
	}{
		{
			name: "o2m fk on the related table",
			rel:  config.TableRelationship{Name: "Profiles", Type: "o2m", Table: "owner_profiles", FK: "owner_id"},
		},
		{
			name: "o2m fk on this table only",
			rel:  config.TableRelationship{Name: "Profiles", Type: "o2m", Table: "owner_profiles", FK: "name"},
			want: []string{"tables.owners.relationships[0]", `"Profiles"`, `.fk: "name"`, `related table "owner_profiles"`, "PRD §4.8"},
		},
		{
			name: "one_to_many synonym, fk on neither table",
			rel:  config.TableRelationship{Name: "Profiles", Type: "one_to_many", Table: "owner_profiles", FK: "ghost_id"},
			want: []string{"tables.owners.relationships[0]", `.fk: "ghost_id"`, `related table "owner_profiles"`},
		},
		{
			name: "o2o has-one, fk on the related table",
			rel:  config.TableRelationship{Name: "OwnerProfile", Type: "one_to_one", Table: "owner_profiles", FK: "owner_id"},
		},
		{
			name: "o2o belongs-to, fk on this table",
			rel:  config.TableRelationship{Name: "Profile", Type: "o2o", Table: "owner_profiles", FK: "profile_id"},
		},
		{
			name: "o2o fk on neither table",
			rel:  config.TableRelationship{Name: "GhostProfile", Type: "o2o", Table: "owner_profiles", FK: "ghost_id"},
			want: []string{"tables.owners.relationships[0]", `"GhostProfile"`, `.fk: "ghost_id"`, `table "owners"`, `related table "owner_profiles"`, "PRD §4.8 / §13.1"},
		},
		{
			name: "m2m fk is not consulted",
			rel:  config.TableRelationship{Name: "Profiles", Type: "m2m", Table: "owner_profiles", FK: "ghost_id", Junction: "owner_profile_links"},
		},
		{
			name: "empty fk is not this check's",
			rel:  config.TableRelationship{Name: "Profiles", Type: "o2m", Table: "owner_profiles"},
		},
		{
			name: "unparsed target is left to resolution",
			rel:  config.TableRelationship{Name: "Ghosts", Type: "o2m", Table: "ghosts", FK: "ghost_id"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables["owners"] = config.TableConfig{Relationships: []config.TableRelationship{tt.rel}}

			_, err := config.ValidatePostParse(cfg, ownersSchema(), nil)
			if tt.want == nil {
				if err != nil {
					t.Errorf("ValidatePostParse() = %v, want nil", err)
				}
				return
			}
			assertErrContains(t, err, "ValidatePostParse()", tt.want...)
		})
	}
}
