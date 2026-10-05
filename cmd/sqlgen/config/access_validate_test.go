package config_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// accessSchemaTables returns a fixture table exercising every §32.4 shape:
// an auto-generated PK, required-on-create columns (NOT NULL, no default),
// a defaulted column, a nullable column, and a soft-delete column.
func accessSchemaTables() []config.SchemaTable {
	return []config.SchemaTable{
		{
			Name:   "users",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
				{Name: "email", Type: "text"},         // required on create
				{Name: "password_hash", Type: "text"}, // required on create
				{Name: "created_at", Type: "timestamptz", HasDefault: true},
				{Name: "bio", Type: "text", Nullable: true},
				{Name: "deleted_at", Type: "timestamptz", Nullable: true},
			},
		},
	}
}

// withColumnAccess returns validConfig() with a single access entry on the
// users table and the GraphQL API enabled (the required-on-create rule only
// bites while the create API is on).
func withColumnAccess(col, role string) *config.RootConfig {
	cfg := validConfig()
	cfg.API = &config.APIConfig{Enabled: true, GraphQL: &config.GraphQLAPIConfig{Enabled: true}}
	cfg.Tables["users"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			col: {Access: role},
		},
	}
	return cfg
}

func TestValidatePreParse_ColumnAccess_RoleEnum(t *testing.T) {
	tests := []struct {
		name    string
		role    string
		wantErr string
	}{
		{name: "public accepted", role: "public"},
		{name: "read_only accepted", role: "read_only"},
		{name: "write_only accepted", role: "write_only"},
		{name: "hidden accepted", role: "hidden"},
		{name: "internal accepted", role: "internal"},
		{name: "empty accepted as unset", role: ""},
		{
			name:    "unknown role rejected",
			role:    "confidential",
			wantErr: `tables.users.column_map.password_hash.access: "confidential" is not a valid value (allowed: public, read_only, write_only, hidden, internal)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables["users"] = config.TableConfig{
				ColumnMap: map[string]config.ColumnOverride{
					"password_hash": {Access: tt.role},
				},
			}
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidatePreParse() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePreParse() error = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidatePreParse() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestValidatePreParse_ColumnAccess_ExcludedColumn verifies that classifying
// a column removed by exclude_columns errors — from either the table-level
// or the global list (they are merged as a union, PRD §4.8).
func TestValidatePreParse_ColumnAccess_ExcludedColumn(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(cfg *config.RootConfig)
	}{
		{
			name: "table-level exclude_columns",
			mutate: func(cfg *config.RootConfig) {
				tc := cfg.Tables["users"]
				tc.ExcludeColumns = []string{"password_hash"}
				cfg.Tables["users"] = tc
			},
		},
		{
			name: "global exclude_columns",
			mutate: func(cfg *config.RootConfig) {
				cfg.Generation.ExcludeColumns = []string{"password_hash"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables["users"] = config.TableConfig{
				ColumnMap: map[string]config.ColumnOverride{
					"password_hash": {Access: "internal"},
				},
			}
			tt.mutate(cfg)
			_, err := config.ValidatePreParse(cfg)
			want := "removed by exclude_columns"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("ValidatePreParse() error = %v, want it to contain %q", err, want)
			}
		})
	}
}

// TestValidatePreParse_ColumnAccess_BatchReporting verifies that multiple
// access violations report together in one pass (§4.13).
func TestValidatePreParse_ColumnAccess_BatchReporting(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["users"] = config.TableConfig{
		ExcludeColumns: []string{"legacy_blob"},
		ColumnMap: map[string]config.ColumnOverride{
			"password_hash": {Access: "confidential"},
			"legacy_blob":   {Access: "internal"},
		},
	}
	_, err := config.ValidatePreParse(cfg)
	if err == nil {
		t.Fatal("ValidatePreParse() error = nil, want two access errors")
	}
	for _, want := range []string{"is not a valid value", "removed by exclude_columns"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidatePreParse() error = %q, want it to contain %q", err, want)
		}
	}
}

func TestValidatePostParse_ColumnAccess_NonExistentColumn(t *testing.T) {
	cfg := withColumnAccess("no_such_column", "internal")
	_, err := config.ValidatePostParse(cfg, accessSchemaTables(), nil)
	want := `column "no_such_column" does not exist on table public.users`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("ValidatePostParse() error = %v, want it to contain %q", err, want)
	}
}

func TestValidatePostParse_ColumnAccess_PKRole(t *testing.T) {
	tests := []struct {
		name    string
		role    string
		wantErr bool
	}{
		{name: "public on PK accepted", role: "public"},
		{name: "read_only on PK accepted", role: "read_only"},
		{name: "write_only on PK rejected", role: "write_only", wantErr: true},
		{name: "hidden on PK rejected", role: "hidden", wantErr: true},
		{name: "internal on PK rejected", role: "internal", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withColumnAccess("id", tt.role)
			_, err := config.ValidatePostParse(cfg, accessSchemaTables(), nil)
			if !tt.wantErr {
				if err != nil {
					t.Errorf("ValidatePostParse() unexpected error: %v", err)
				}
				return
			}
			want := "PK columns must stay API-readable"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("ValidatePostParse() error = %v, want it to contain %q", err, want)
			}
		})
	}
}

// TestValidatePostParse_ColumnAccess_PKOverride verifies the PK rule reads
// the resolved PK set: a tables.<name>.primary_key.columns override makes
// its columns PK members even when the schema carries no PRIMARY KEY flag.
func TestValidatePostParse_ColumnAccess_PKOverride(t *testing.T) {
	cfg := withColumnAccess("email", "internal")
	tc := cfg.Tables["users"]
	tc.PrimaryKey = &config.TablePrimaryKeyConfig{Columns: []string{"email"}}
	cfg.Tables["users"] = tc

	_, err := config.ValidatePostParse(cfg, accessSchemaTables(), nil)
	want := "PK columns must stay API-readable"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("ValidatePostParse() error = %v, want it to contain %q", err, want)
	}
}

func TestValidatePostParse_ColumnAccess_RequiredOnCreate(t *testing.T) {
	tests := []struct {
		name    string
		col     string
		role    string
		mutate  func(cfg *config.RootConfig)
		wantErr bool
	}{
		{
			name: "internal on required-on-create with create API enabled",
			col:  "email", role: "internal",
			wantErr: true,
		},
		{
			name: "read_only on required-on-create with create API enabled",
			col:  "email", role: "read_only",
			wantErr: true,
		},
		{
			name: "hidden on required-on-create with create API enabled",
			col:  "email", role: "hidden",
			wantErr: true,
		},
		{
			name: "write_only on required-on-create is allowed",
			col:  "email", role: "write_only",
		},
		{
			name: "internal on defaulted column is allowed",
			col:  "created_at", role: "internal",
		},
		{
			name: "internal on nullable column is allowed",
			col:  "bio", role: "internal",
		},
		{
			name: "internal on required-on-create with API disabled",
			col:  "email", role: "internal",
			mutate: func(cfg *config.RootConfig) { cfg.API = nil },
		},
		{
			name: "internal on required-on-create with graphql disabled",
			col:  "email", role: "internal",
			mutate: func(cfg *config.RootConfig) { cfg.API.GraphQL.Enabled = false },
		},
		{
			name: "internal on required-on-create with table API opt-out",
			col:  "email", role: "internal",
			mutate: func(cfg *config.RootConfig) {
				tc := cfg.Tables["users"]
				tc.API = &config.TableAPIConfig{Enabled: new(false)}
				cfg.Tables["users"] = tc
			},
		},
		{
			name: "internal on required-on-create with read_only api.operations mask",
			col:  "email", role: "internal",
			mutate: func(cfg *config.RootConfig) {
				tc := cfg.Tables["users"]
				tc.API = &config.TableAPIConfig{Operations: &config.Operations{Preset: "read_only"}}
				cfg.Tables["users"] = tc
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withColumnAccess(tt.col, tt.role)
			if tt.mutate != nil {
				tt.mutate(cfg)
			}
			_, err := config.ValidatePostParse(cfg, accessSchemaTables(), nil)
			if !tt.wantErr {
				if err != nil {
					t.Errorf("ValidatePostParse() unexpected error: %v", err)
				}
				return
			}
			want := "required on create"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("ValidatePostParse() error = %v, want it to contain %q", err, want)
			}
		})
	}
}

func TestValidatePostParse_ColumnAccess_SoftDeleteFilterWarning(t *testing.T) {
	tests := []struct {
		name        string
		role        string
		wantWarning bool
	}{
		{name: "hidden on soft-delete column warns", role: "hidden", wantWarning: true},
		{name: "write_only on soft-delete column warns", role: "write_only", wantWarning: true},
		{name: "internal on soft-delete column warns", role: "internal", wantWarning: true},
		{name: "read_only on soft-delete column does not warn", role: "read_only"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withColumnAccess("deleted_at", tt.role)
			cfg.Generation.SoftDeleteColumns = []config.SoftDeleteConfig{
				{Name: "deleted_at", Type: "timestamp"},
			}
			warnings, err := config.ValidatePostParse(cfg, accessSchemaTables(), nil)
			if err != nil {
				t.Fatalf("ValidatePostParse() unexpected error: %v", err)
			}
			found := false
			for _, w := range warnings {
				if strings.Contains(w.Message, "soft-delete column's filter") {
					found = true
				}
			}
			if found != tt.wantWarning {
				t.Errorf("ValidatePostParse() soft-delete access warning present = %v, want %v (warnings: %v)", found, tt.wantWarning, warnings)
			}
		})
	}
}

// callerPKSchemaTables returns two shapes the §32.4 required-on-create rule
// must treat differently: a pure M2M junction whose composite PK is a tuple
// of caller-supplied foreign keys, and a table with a single server-generated
// surrogate PK.
func callerPKSchemaTables() []config.SchemaTable {
	return []config.SchemaTable{
		{
			Name:   "user_categories",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "user_id", Type: "uuid", PrimaryKey: true},
				{Name: "category_id", Type: "bigint", PrimaryKey: true},
			},
		},
		{
			Name:   "categories",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
				{Name: "name", Type: "text"},
			},
		},
		{
			// Natural key: neither auto-increment nor defaulted, so nothing
			// generates it server-side.
			Name:   "regions",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "code", Type: "text", PrimaryKey: true},
				{Name: "name", Type: "text"},
			},
		},
	}
}

// TestValidatePostParse_ColumnAccess_CallerPKRequiredOnCreate covers the
// PRD §26.4 carve-out: a caller-strategy PK column IS part of the API create
// input, so a role that drops it from that input leaves the INSERT writing
// zero-valued keys and must be rejected. A server-generated PK keeps its
// exemption — nothing in the API needs to supply it.
//
// Only `read_only` is exercised as the dropping role: the earlier PK-readable
// rule already rejects hidden / internal / write_only on any PK column.
func TestValidatePostParse_ColumnAccess_CallerPKRequiredOnCreate(t *testing.T) {
	tests := []struct {
		name    string
		table   string
		col     string
		mutate  func(cfg *config.RootConfig)
		wantErr bool
		// wantMsg overrides the expected error text; empty means the
		// required-on-create access rule.
		wantMsg string
	}{
		{
			name:  "read_only on a composite-PK column is rejected",
			table: "user_categories", col: "user_id",
			wantErr: true,
		},
		{
			name:  "read_only on a server-generated single PK is allowed",
			table: "categories", col: "id",
		},
		{
			name:  "read_only on a natural-key PK declared caller-strategy is rejected",
			table: "regions", col: "code",
			mutate: func(cfg *config.RootConfig) {
				tc := cfg.Tables["regions"]
				tc.PrimaryKey = &config.TablePrimaryKeyConfig{Strategy: config.PKStrategyCaller}
				cfg.Tables["regions"] = tc
			},
			wantErr: true,
		},
		{
			// An auto-increment column is DB-generated whatever the config
			// claims, so the exemption holds even under caller strategy.
			name:  "read_only on an auto-increment PK declared caller-strategy is allowed",
			table: "categories", col: "id",
			mutate: func(cfg *config.RootConfig) {
				tc := cfg.Tables["categories"]
				tc.PrimaryKey = &config.TablePrimaryKeyConfig{Strategy: config.PKStrategyCaller}
				cfg.Tables["categories"] = tc
			},
		},
		{
			// Without an explicit strategy the config-side check cannot tell
			// an auto-detected natural key from an app-minted single PK, so it
			// stays exempt (documented gap — PRD §32.4).
			name:  "read_only on an undeclared natural-key PK is allowed",
			table: "regions", col: "code",
		},
		{
			// A composite key cannot be db-strategy (PRD §8.6), so
			// the access rule's exemption is never reached: the composite-key
			// rule names the override instead.
			name:  "a composite PK declared db-strategy is rejected by the composite-key rule",
			table: "user_categories", col: "user_id",
			mutate: func(cfg *config.RootConfig) {
				tc := cfg.Tables["user_categories"]
				tc.PrimaryKey = &config.TablePrimaryKeyConfig{Strategy: config.PKStrategyDB}
				cfg.Tables["user_categories"] = tc
			},
			wantErr: true,
			wantMsg: "is not supported on a composite primary key",
		},
		{
			name:  "read_only on a composite-PK column with the create API disabled is allowed",
			table: "user_categories", col: "user_id",
			mutate: func(cfg *config.RootConfig) { cfg.API = nil },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.API = &config.APIConfig{Enabled: true, GraphQL: &config.GraphQLAPIConfig{Enabled: true}}
			cfg.Tables[tt.table] = config.TableConfig{
				ColumnMap: map[string]config.ColumnOverride{
					tt.col: {Access: "read_only"},
				},
			}
			if tt.mutate != nil {
				tt.mutate(cfg)
			}
			_, err := config.ValidatePostParse(cfg, callerPKSchemaTables(), nil)
			if !tt.wantErr {
				if err != nil {
					t.Errorf("ValidatePostParse() unexpected error: %v", err)
				}
				return
			}
			want := "required on create"
			if tt.wantMsg != "" {
				want = tt.wantMsg
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("ValidatePostParse() error = %v, want it to contain %q", err, want)
			}
		})
	}
}

// withCursorKeyAccess returns withColumnAccess with the users table's
// cursor_keys set explicitly. `created_at` is the column these tests classify:
// it is defaulted, so the required-on-create rule never fires and the cursor
// rule is exercised in isolation.
func withCursorKeyAccess(col, role string, keys ...string) *config.RootConfig {
	cfg := withColumnAccess(col, role)
	tc := cfg.Tables["users"]
	tc.CursorKeys = keys
	cfg.Tables["users"] = tc
	return cfg
}

// TestValidatePostParse_ColumnAccess_CursorKeyRole pins that every column
// in a table's resolved cursor_keys must stay API-readable. Cursor values are
// base64 JSON of exactly these columns, carried on every edge `cursor` and on
// pageInfo.startCursor / endCursor, and accepted back as `after` / `before`
// where they become keyset conditions — so a restricted key both leaks its
// value outward and offers an inequality oracle over a column §32.2 gives no
// filter surface (PRD §32.4, §14.2).
func TestValidatePostParse_ColumnAccess_CursorKeyRole(t *testing.T) {
	tests := []struct {
		name    string
		role    string
		wantErr bool
	}{
		{name: "public on cursor key accepted", role: "public"},
		{name: "read_only on cursor key accepted", role: "read_only"},
		{name: "write_only on cursor key rejected", role: "write_only", wantErr: true},
		{name: "hidden on cursor key rejected", role: "hidden", wantErr: true},
		{name: "internal on cursor key rejected", role: "internal", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withCursorKeyAccess("created_at", tt.role, "created_at", "id")
			_, err := config.ValidatePostParse(cfg, accessSchemaTables(), nil)
			if !tt.wantErr {
				if err != nil {
					t.Errorf("ValidatePostParse() unexpected error: %v", err)
				}
				return
			}
			want := "is not allowed on a cursor_keys column"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("ValidatePostParse() error = %v, want it to contain %q", err, want)
			}
		})
	}
}

// TestValidatePostParse_ColumnAccess_CursorKeyScope pins the boundaries of the
// cursor-key readability rule: which config makes it fire, which exempts it, and which of the
// §32.4 rules wins when a column is caught by more than one.
func TestValidatePostParse_ColumnAccess_CursorKeyScope(t *testing.T) {
	const cursorErr = "is not allowed on a cursor_keys column"

	tests := []struct {
		name    string
		cfg     func() *config.RootConfig
		want    string
		notWant string
	}{
		{
			name: "explicit table cursor_keys names its source",
			cfg: func() *config.RootConfig {
				return withCursorKeyAccess("created_at", "internal", "created_at")
			},
			want: "from tables.public.users.cursor_keys",
		},
		{
			name: "inherited cursor_keys names its source",
			cfg: func() *config.RootConfig {
				cfg := withColumnAccess("created_at", "internal")
				cfg.Generation.CursorKeys = []string{"created_at"}
				return cfg
			},
			want: "inherited from generation.cursor_keys",
		},
		{
			// Unlike the required-on-create rule, this one is not gated on the
			// API: a cursor is a token built to be handed to a client and
			// handed back, and §14.2 states cursors are not a security
			// boundary. Containment rules in §32 hold regardless of API config.
			name: "rule holds with the API disabled",
			cfg: func() *config.RootConfig {
				cfg := withCursorKeyAccess("created_at", "internal", "created_at")
				cfg.API = nil
				return cfg
			},
			want: cursorErr,
		},
		{
			name: "rule holds with the table opted out of the API",
			cfg: func() *config.RootConfig {
				cfg := withCursorKeyAccess("created_at", "internal", "created_at")
				tc := cfg.Tables["users"]
				tc.API = &config.TableAPIConfig{Enabled: new(false)}
				cfg.Tables["users"] = tc
				return cfg
			},
			want: cursorErr,
		},
		{
			name: "PK member that is also a cursor key reports the PK rule only",
			cfg: func() *config.RootConfig {
				return withCursorKeyAccess("id", "internal", "id")
			},
			want:    "PK columns must stay API-readable",
			notWant: cursorErr,
		},
		{
			name: "required-on-create cursor key reports the cursor rule",
			cfg: func() *config.RootConfig {
				return withCursorKeyAccess("password_hash", "internal", "password_hash", "id")
			},
			want:    cursorErr,
			notWant: "required on create",
		},
		{
			// Explicit cursor_keys naming a missing column is already a §4.13
			// error; the access rule reads the resolved set, which does not
			// exist here, so it must stay silent rather than pile on.
			name: "unresolvable cursor_keys reports only the existence error",
			cfg: func() *config.RootConfig {
				return withCursorKeyAccess("created_at", "internal", "no_such_column")
			},
			want:    `cursor_keys: column "no_such_column" does not exist in table public.users`,
			notWant: cursorErr,
		},
		{
			// generation.cursor_keys is unset here, so the keys come from the
			// built-in §4.13 default — naming generation.cursor_keys would
			// point at a setting the user never wrote. `id` is restricted via
			// a primary_key.columns override that moves the PK off it, so the
			// PK rule does not preempt the cursor rule.
			name: "built-in default cursor_keys names itself, not generation",
			cfg: func() *config.RootConfig {
				cfg := withColumnAccess("id", "internal")
				tc := cfg.Tables["users"]
				tc.PrimaryKey = &config.TablePrimaryKeyConfig{Columns: []string{"email"}}
				cfg.Tables["users"] = tc
				return cfg
			},
			want:    "the built-in cursor_keys default",
			notWant: "generation.cursor_keys",
		},
		{
			// The generator resolves cursor_keys against the post-exclude
			// column set, so an inherited list with an excluded member falls
			// back to the PK and no cursor carries created_at. Resolving
			// against the raw set here would hard-error on a column that
			// reaches no cursor.
			name: "exclude_columns drops an inherited key and the rule follows the fallback",
			cfg: func() *config.RootConfig {
				cfg := withColumnAccess("created_at", "internal")
				cfg.Generation.CursorKeys = []string{"created_at", "bio"}
				cfg.Generation.ExcludeColumns = []string{"bio"}
				return cfg
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.ValidatePostParse(tt.cfg(), accessSchemaTables(), nil)
			if tt.want == "" {
				if err != nil {
					t.Errorf("ValidatePostParse() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePostParse() error = nil, want it to contain %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ValidatePostParse() error = %v, want it to contain %q", err, tt.want)
			}
			if tt.notWant != "" && strings.Contains(err.Error(), tt.notWant) {
				t.Errorf("ValidatePostParse() error = %v, want it NOT to contain %q", err, tt.notWant)
			}
		})
	}
}

// TestValidatePostParse_ColumnAccess_CursorKeyConnectionOmitted covers the
// other half of the "no cursor exists" exemption: §4.13 omits the Connection
// method entirely when a table inherits cursor_keys it cannot satisfy and has
// no primary key to fall back on, so nothing encodes a cursor.
func TestValidatePostParse_ColumnAccess_CursorKeyConnectionOmitted(t *testing.T) {
	tables := []config.SchemaTable{
		{
			Name:   "events",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "payload", Type: "jsonb", Nullable: true},
			},
		},
	}

	cfg := validConfig()
	cfg.Generation.CursorKeys = []string{"payload", "seq"}
	cfg.Tables["events"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			"payload": {Access: "internal"},
		},
	}

	warnings, err := config.ValidatePostParse(cfg, tables, nil)
	if err != nil {
		t.Errorf("ValidatePostParse() unexpected error: %v", err)
	}
	want := "Connection method omitted"
	if len(warnings) == 0 || !strings.Contains(warnings[0].Message, want) {
		t.Errorf("ValidatePostParse() warnings = %v, want one containing %q", warnings, want)
	}
}
