package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/parser"
)

// overrideConfig builds a RootConfig declaring primary_key.columns for one
// table, plus any exclude_columns the case needs.
func overrideConfig(key string, pkCols, excludeCols []string) *config.RootConfig {
	tc := config.TableConfig{ExcludeColumns: excludeCols}
	if pkCols != nil {
		tc.PrimaryKey = &config.TablePrimaryKeyConfig{Columns: pkCols}
	}
	return &config.RootConfig{Tables: map[string]config.TableConfig{key: tc}}
}

// TestApplyPrimaryKeyOverrides pins that tables.<name>.primary_key.columns is
// resolved onto parser.Column.PrimaryKey, so the resolved key is a property of
// the schema rather than a projection only gen can see (PRD §8.6).
func TestApplyPrimaryKeyOverrides(t *testing.T) {
	tests := []struct {
		name    string
		table   parser.Table
		cfg     *config.RootConfig
		wantPKs []string
	}{
		{
			name: "single-column override promotes the named column",
			table: parser.Table{
				Name: "counters", Schema: "public",
				Columns: []parser.Column{
					{Name: "key", Type: "text"},
					{Name: "count", Type: "bigint"},
				},
			},
			cfg:     overrideConfig("public.counters", []string{"key"}, nil),
			wantPKs: []string{"key"},
		},
		{
			name: "composite override promotes both columns",
			table: parser.Table{
				Name: "rate_limits", Schema: "public",
				Columns: []parser.Column{
					{Name: "org_id", Type: "bigint"},
					{Name: "bucket", Type: "text"},
					{Name: "count", Type: "integer"},
				},
			},
			cfg:     overrideConfig("public.rate_limits", []string{"org_id", "bucket"}, nil),
			wantPKs: []string{"org_id", "bucket"},
		},
		{
			name: "override silently replaces an auto-detected key",
			table: parser.Table{
				Name: "legacy", Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "code", Type: "text"},
				},
			},
			cfg:     overrideConfig("public.legacy", []string{"code"}, nil),
			wantPKs: []string{"code"},
		},
		{
			name: "excluded column named by the override contributes nothing",
			table: parser.Table{
				Name: "counters", Schema: "public",
				Columns: []parser.Column{
					{Name: "key", Type: "text"},
					{Name: "internal", Type: "text"},
				},
			},
			cfg:     overrideConfig("public.counters", []string{"key", "internal"}, []string{"internal"}),
			wantPKs: []string{"key"},
		},
		{
			name: "column absent from the table contributes nothing",
			table: parser.Table{
				Name: "counters", Schema: "public",
				Columns: []parser.Column{
					{Name: "key", Type: "text"},
				},
			},
			cfg:     overrideConfig("public.counters", []string{"key", "nope"}, nil),
			wantPKs: []string{"key"},
		},
		{
			name: "bare table-name config key matches a schema-qualified table",
			table: parser.Table{
				Name: "counters", Schema: "public",
				Columns: []parser.Column{
					{Name: "key", Type: "text"},
				},
			},
			cfg:     overrideConfig("counters", []string{"key"}, nil),
			wantPKs: []string{"key"},
		},
		{
			name: "no override leaves the detected key untouched",
			table: parser.Table{
				Name: "products", Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
			cfg:     overrideConfig("public.products", nil, nil),
			wantPKs: []string{"id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &parser.Schema{Tables: []parser.Table{tt.table}}

			applyPrimaryKeyOverrides(schema, tt.cfg)

			var got []string
			for _, col := range schema.Tables[0].Columns {
				if col.PrimaryKey {
					got = append(got, col.Name)
				}
			}
			if diff := cmp.Diff(tt.wantPKs, got); diff != "" {
				t.Errorf("applyPrimaryKeyOverrides() resolved PK columns mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestApplyPrimaryKeyOverrides_RelationshipArity is a regression test: an
// override-declared single-column PK that is also a foreign key must classify
// as O2O, matching the `caller` strategy gen already derives for it. Before the
// override reached parser.Column.PrimaryKey, columnUnique saw PrimaryKey ==
// false and emitted an O2M list on the parent for something that structurally
// holds at most one row per parent.
//
// The reachable shape is narrower than it looks: a post-CREATE UNIQUE
// constraint or a CREATE UNIQUE INDEX both set Column.Unique, which columnUnique
// short-circuits on before it ever consults PrimaryKey. The divergence needs an
// override whose uniqueness is app-enforced only — the case PRD §8.6 warns
// about rather than rejects.
func TestApplyPrimaryKeyOverrides_RelationshipArity(t *testing.T) {
	tests := []struct {
		name     string
		child    parser.Table
		pkCols   []string
		wantType parser.RelationshipType
		wantFrom string
		wantTo   string
	}{
		{
			name: "single-column FK override is one-to-one",
			child: parser.Table{
				Name: "user_settings", Schema: "public",
				Columns: []parser.Column{
					{Name: "user_id", Type: "uuid", FKReference: &parser.FKReference{Schema: "public", Table: "users", Column: "id"}},
					{Name: "theme", Type: "text"},
				},
			},
			pkCols:   []string{"user_id"},
			wantType: parser.OneToOne,
			wantFrom: "public.user_settings",
			wantTo:   "public.users",
		},
		{
			name: "composite FK override stays one-to-many",
			child: parser.Table{
				Name: "user_tags", Schema: "public",
				Columns: []parser.Column{
					{Name: "user_id", Type: "uuid", FKReference: &parser.FKReference{Schema: "public", Table: "users", Column: "id"}},
					{Name: "tag_id", Type: "uuid", FKReference: &parser.FKReference{Schema: "public", Table: "tags", Column: "id"}},
				},
			},
			pkCols:   []string{"user_id", "tag_id"},
			wantType: parser.OneToMany,
			wantFrom: "public.users",
			wantTo:   "public.user_tags",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &parser.Schema{Tables: []parser.Table{tt.child}}
			cfg := overrideConfig("public."+tt.child.Name, tt.pkCols, nil)

			applyPrimaryKeyOverrides(schema, cfg)
			parser.DetectRelationships(schema)

			if len(schema.Relationships) == 0 {
				t.Fatalf("DetectRelationships() produced no edges, want at least one %v", tt.wantType)
			}
			got := schema.Relationships[0]
			if got.Type != tt.wantType {
				t.Errorf("relationship type = %v, want %v", got.Type, tt.wantType)
			}
			if got.SourceTable != tt.wantFrom || got.TargetTable != tt.wantTo {
				t.Errorf("edge = %s -> %s, want %s -> %s",
					got.SourceTable, got.TargetTable, tt.wantFrom, tt.wantTo)
			}
		})
	}
}

// TestApplyPrimaryKeyOverrides_CompositeIsNotAJunction pins the blast-radius
// bound of resolving PK overrides: junctionConstraint reads Table.Constraints, never
// the column flags, so resolving a composite override over two FK columns
// cannot turn the table into a detected M2M junction. Writing a synthetic
// PRIMARY KEY constraint instead of (or alongside) the flags would, which is
// why this pass deliberately does not.
func TestApplyPrimaryKeyOverrides_CompositeIsNotAJunction(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{{
			Name: "user_tags", Schema: "public",
			Columns: []parser.Column{
				{Name: "user_id", Type: "uuid", FKReference: &parser.FKReference{Schema: "public", Table: "users", Column: "id"}},
				{Name: "tag_id", Type: "uuid", FKReference: &parser.FKReference{Schema: "public", Table: "tags", Column: "id"}},
			},
		}},
	}
	cfg := overrideConfig("public.user_tags", []string{"user_id", "tag_id"}, nil)

	applyPrimaryKeyOverrides(schema, cfg)
	parser.DetectRelationships(schema)

	for _, rel := range schema.Relationships {
		if rel.Type == parser.ManyToMany {
			t.Errorf("DetectRelationships() classified an override-declared composite PK as M2M (%s -> %s via %s); "+
				"only a schema-declared composite PK/UNIQUE constraint may anchor junction detection",
				rel.SourceTable, rel.TargetTable, rel.JunctionTable)
		}
	}
}

// TestGeneratePipeline_PKOverrideOrdering drives the real `generate` command to
// pin the two orderings applyPrimaryKeyOverrides depends on. The unit tests
// above call the pass and DetectRelationships by hand, so they stay green no
// matter what generate.go / validate.go / pipeline.go do; this one fails if the
// steps are resequenced.
//
// The fixture is the only shape that exercises the O2M-instead-of-O2O arity
// bug: a single-column override on an FK column whose uniqueness is
// app-enforced. A post-CREATE UNIQUE or a CREATE UNIQUE INDEX would set
// Column.Unique, which columnUnique short-circuits on before it ever consults
// PrimaryKey — so either of those would pass even with the fix reverted.
//
//   - Resolve BEFORE detect → the edge is O2O (the child holds *User).
//   - Resolve AFTER post-parse validation → the §8.6 "no UNIQUE constraint
//     covers the declared PK" warning still fires. Resolving first would make
//     the override self-satisfy uniqueCovers and silently drop it.
func TestGeneratePipeline_PKOverrideOrdering(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")

	schema := `CREATE TABLE users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL
);
CREATE TABLE user_settings (
    user_id UUID NOT NULL REFERENCES users(id),
    theme TEXT NOT NULL
);`
	if err := os.WriteFile(filepath.Join(dir, "schema.sql"), []byte(schema), 0o600); err != nil {
		t.Fatalf("writing schema: %v", err)
	}

	cfg := `version: 1
input:
  dialect: postgres
  paths: [schema.sql]
output:
  dir: out
  package: models
tables:
  users: {}
  user_settings:
    primary_key:
      columns: [user_id]
`
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	_, stderr, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("generate returned error: %v\nstderr: %s", err, stderr)
	}

	models, err := os.ReadFile(filepath.Join(outDir, "models_gen.go")) //nolint:gosec // test code, path is from controlled test dir
	if err != nil {
		t.Fatalf("reading generated models: %v", err)
	}
	got := string(models)

	// Resolve-before-detect: O2O puts the parent pointer on the FK-holding
	// child. O2M would instead hang a []*UserSetting off User.
	if !strings.Contains(got, "Users *User") {
		t.Errorf("generated models lack the O2O field `Users *User` on UserSetting — " +
			"applyPrimaryKeyOverrides must run before parser.DetectRelationships")
	}
	if strings.Contains(got, "UserSettings []*UserSetting") {
		t.Errorf("generated models carry the O2M list `UserSettings []*UserSetting` on User — " +
			"the override-declared sole FK primary key must classify as O2O (PRD §13.1)")
	}

	// Resolve-after-validate: the §8.6 uniqueness warning must survive.
	const warning = "no UNIQUE constraint covers the declared PK"
	if !strings.Contains(stderr, warning) {
		t.Errorf("stderr = %q, want it to contain %q — applyPrimaryKeyOverrides must run "+
			"after config.ValidatePostParse, or the override self-satisfies uniqueCovers",
			stderr, warning)
	}
}
