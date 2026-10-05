package gen

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/parser"
)

// TestBuildConflictTargets_OverrideNeedsAnIndex pins the index guard: once
// cli.applyPrimaryKeyOverrides resolves tables.<name>.primary_key.columns onto
// the schema, an override-declared key is indistinguishable from a
// schema-declared one at the column level — but only the schema-declared one is
// guaranteed to have an index behind it.
//
// PRD §8.6 lets the config assert uniqueness the database does not enforce, so
// an unguarded PK conflict target would emit an Upsert arm that compiles and
// then fails on every call (PostgreSQL 42P10, "there is no unique or exclusion
// constraint matching the ON CONFLICT specification").
func TestBuildConflictTargets_OverrideNeedsAnIndex(t *testing.T) {
	tests := []struct {
		name     string
		table    parser.Table
		tableCfg config.TableConfig
		want     []ConflictTargetContext
	}{
		{
			name: "schema-declared key always targets",
			table: parser.Table{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
			want: []ConflictTargetContext{
				{ConstantName: "ProductConflictPK", Columns: []string{"id"}, CoversPK: true, Comment: "PRIMARY KEY (id)"},
			},
		},
		{
			name: "override covered by a table-level UNIQUE targets",
			table: parser.Table{
				Name: "counters",
				Columns: []parser.Column{
					{Name: "key", Type: "text", PrimaryKey: true, Unique: true},
					{Name: "count", Type: "bigint"},
				},
				Constraints: []parser.Constraint{
					{Name: "counters_key_uq", Type: parser.Unique, Columns: []string{"key"}},
				},
			},
			tableCfg: config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"key"}}},
			want: []ConflictTargetContext{
				{ConstantName: "CounterConflictKey", Columns: []string{"key"}, CoversPK: true, Comment: "UNIQUE (key)"},
				{ConstantName: "CounterConflictPK", Columns: []string{"key"}, CoversPK: true, Comment: "PRIMARY KEY (key)"},
			},
		},
		{
			name: "composite override covered by a composite UNIQUE targets",
			table: parser.Table{
				Name: "rate_limits",
				Columns: []parser.Column{
					{Name: "org_id", Type: "bigint", PrimaryKey: true},
					{Name: "bucket", Type: "text", PrimaryKey: true},
				},
				Constraints: []parser.Constraint{
					{Name: "rate_limits_org_bucket_uq", Type: parser.Unique, Columns: []string{"org_id", "bucket"}},
				},
			},
			tableCfg: config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"org_id", "bucket"}}},
			want: []ConflictTargetContext{
				{ConstantName: "RateLimitConflictOrgIDBucket", Columns: []string{"org_id", "bucket"}, CoversPK: true, Comment: "UNIQUE (org_id, bucket)"},
				{ConstantName: "RateLimitConflictPK", Columns: []string{"org_id", "bucket"}, CoversPK: true, Comment: "PRIMARY KEY (org_id, bucket)"},
			},
		},
		{
			// A config override may restate a key the schema already declares
			// (§8.6 allows it; for a composite it sets <Table>PK field order).
			// A table-level PRIMARY KEY leaves a Constraint behind, and the
			// write-back never synthesizes one, so it stays a sound signal that
			// the database really does have the index.
			name: "table-level PK restated in config keeps its target",
			table: parser.Table{
				Name: "order_items",
				Columns: []parser.Column{
					{Name: "order_id", Type: "uuid", PrimaryKey: true},
					{Name: "product_id", Type: "uuid", PrimaryKey: true},
					{Name: "qty", Type: "integer"},
				},
				Constraints: []parser.Constraint{
					{Name: "order_items_pkey", Type: parser.PrimaryKey, Columns: []string{"order_id", "product_id"}},
				},
			},
			tableCfg: config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"order_id", "product_id"}}},
			want: []ConflictTargetContext{
				{ConstantName: "OrderItemConflictPK", Columns: []string{"order_id", "product_id"}, CoversPK: true, Comment: "PRIMARY KEY (order_id, product_id)"},
			},
		},
		{
			// KNOWN LIMITATION, pinned so it cannot change silently: an inline
			// single-column PRIMARY KEY records only the column flag on all
			// three dialects, so once the write-back sets that same flag this
			// shape is byte-identical to an app-enforced override. The guard
			// fails closed — a missing constant is a compile error the author
			// sees at once, a wrongly-emitted target is a 42P10 in production.
			name: "inline single-column PK restated in config loses its target (known limitation)",
			table: parser.Table{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
			tableCfg: config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"id"}}},
			want:     nil,
		},
		{
			name: "app-enforced override emits no PK target",
			table: parser.Table{
				Name: "counters",
				Columns: []parser.Column{
					{Name: "key", Type: "text", PrimaryKey: true},
					{Name: "count", Type: "bigint"},
				},
			},
			tableCfg: config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"key"}}},
			want:     nil,
		},
		{
			name: "composite override matched only by a single-column UNIQUE emits no PK target",
			table: parser.Table{
				Name: "rate_limits",
				Columns: []parser.Column{
					{Name: "org_id", Type: "bigint", PrimaryKey: true},
					{Name: "bucket", Type: "text", PrimaryKey: true},
				},
				Constraints: []parser.Constraint{
					{Name: "rate_limits_org_uq", Type: parser.Unique, Columns: []string{"org_id"}},
				},
			},
			tableCfg: config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"org_id", "bucket"}}},
			want: []ConflictTargetContext{
				{ConstantName: "RateLimitConflictOrgID", Columns: []string{"org_id"}, CoversPK: false, Comment: "UNIQUE (org_id)"},
			},
		},
		{
			name: "partial UNIQUE does not back an override",
			table: parser.Table{
				Name: "counters",
				Columns: []parser.Column{
					{Name: "key", Type: "text", PrimaryKey: true},
					{Name: "count", Type: "bigint"},
				},
				Constraints: []parser.Constraint{
					{Name: "counters_key_active_uq", Type: parser.Unique, Columns: []string{"key"}, Where: "count > 0"},
				},
			},
			tableCfg: config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"key"}}},
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			structName := toPascalCase(toSingular(tt.table.Name))

			got := buildConflictTargets(&tt.table, tt.tableCfg, structName)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("buildConflictTargets(%q) mismatch (-want +got):\n%s", tt.table.Name, diff)
			}
		})
	}
}
