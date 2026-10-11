package livedb

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// manifestDoc is a JSON manifest with entities written as JSON fragments.
func manifestDoc(entities ...string) string {
	return `{"dialect": "mysql", "entities": [` + strings.Join(entities, ",") + `]}`
}

const (
	tableEntity = `{"kind": "table", "table": "users", "source": "kept",
		"columns": [{"name": "id", "db_type": "bigint"}]}`
	viewEntity = `{"kind": "view", "table": "active_users", "source": "CREATE VIEW active_users AS SELECT 1;",
		"columns": [{"name": "id", "db_type": "bigint"}]}`
	viewEntityNoSource = `{"kind": "view", "table": "active_users", "columns": []}`
	viewIndexEntry     = `{"kind": "view", "table": "active_users", "file": "entities/active_user.json"}`

	viewPage        = "# ActiveUser\n\n- **Kind:** view\n- **Source:** `CREATE VIEW active_users AS\nSELECT 1;`\n\n## Files\n\n- views_gen.go\n"
	viewPageDropped = "# ActiveUser\n\n- **Kind:** view\n## Files\n\n- views_gen.go\n"
)

// booleanColumns is an entity with the given defaults on two tinyint(1)
// columns, a varchar defaulting to true, and a decimal defaulting to '1.5'.
func booleanColumns(active, deleted string) string {
	return `{"kind": "table", "table": "users", "columns": [
		{"name": "active", "db_type": "tinyint(1)", "default": "` + active + `"},
		{"name": "deleted", "db_type": "tinyint(1)", "default": "` + deleted + `"},
		{"name": "label", "db_type": "varchar(10)", "default": "true"},
		{"name": "ratio", "db_type": "decimal(4,2)", "default": "'1.5'"}
	]}`
}

const (
	booleanPage = "| Column | Field | Go Type | DB Type | Nullable | PK | Unique | Default | Comparator | Comment |\n" +
		"| `active` | Active | `bool` | `tinyint(1)` |  |  |  | `true` | `comparator.Bool` |  |\n" +
		"| `deleted` | Deleted | `*bool` | `tinyint(1)` | yes |  |  | `false` | `comparator.NullableBool` |  |\n" +
		"| `label` | Label | `string` | `varchar(10)` |  |  |  | `true` | `comparator.String` |  |\n" +
		"| `ratio` | Ratio | `string` | `decimal(4,2)` |  |  |  | `'1.5'` | `comparator.String` |  |\n"
	booleanPageStored = "| Column | Field | Go Type | DB Type | Nullable | PK | Unique | Default | Comparator | Comment |\n" +
		"| `active` | Active | `bool` | `tinyint(1)` |  |  |  | `1` | `comparator.Bool` |  |\n" +
		"| `deleted` | Deleted | `*bool` | `tinyint(1)` | yes |  |  | `0` | `comparator.NullableBool` |  |\n" +
		"| `label` | Label | `string` | `varchar(10)` |  |  |  | `true` | `comparator.String` |  |\n" +
		"| `ratio` | Ratio | `string` | `decimal(4,2)` |  |  |  | `'1.5'` | `comparator.String` |  |\n"
)

func TestNormalizeTree(t *testing.T) {
	tests := []struct {
		name    string
		dialect string
		path    string
		content string
		want    string
	}{
		{
			name:    "view source dropped from the single layout, table untouched",
			dialect: "postgres",
			path:    "manifest/manifest_gen.json",
			content: manifestDoc(tableEntity, viewEntity),
			want: manifestDoc(tableEntity, `{"kind": "view", "table": "active_users",
				"columns": [{"name": "id", "db_type": "bigint"}]}`),
		},
		{
			name:    "split layout index entry for a view has no source to drop",
			dialect: "postgres",
			path:    "manifest/manifest_gen.json",
			content: manifestDoc(viewIndexEntry),
			want:    manifestDoc(viewIndexEntry),
		},
		{
			name:    "view source dropped from a split layout entity file",
			dialect: "postgres",
			path:    "manifest/entities/active_user.json",
			content: viewEntity,
			want:    `{"kind": "view", "table": "active_users", "columns": [{"name": "id", "db_type": "bigint"}]}`,
		},
		{
			name:    "split layout table entity file untouched",
			dialect: "postgres",
			path:    "manifest/entities/user.json",
			content: tableEntity,
			want:    tableEntity,
		},
		{
			name:    "view source dropped from the markdown page",
			dialect: "sqlite",
			path:    "manifest/active_user.md",
			content: viewPage,
			want:    viewPageDropped,
		},
		{
			name:    "MySQL boolean defaults stored form, single layout",
			dialect: "mysql",
			path:    "manifest/manifest_gen.json",
			content: manifestDoc(booleanColumns("true", "false")),
			want:    manifestDoc(booleanColumns("1", "0")),
		},
		{
			name:    "MySQL boolean defaults stored form, split layout entity file",
			dialect: "mysql",
			path:    "manifest/entities/user.json",
			content: booleanColumns("true", "false"),
			want:    booleanColumns("1", "0"),
		},
		{
			name:    "MySQL stored boolean defaults unchanged",
			dialect: "mysql",
			path:    "manifest/entities/user.json",
			content: booleanColumns("1", "0"),
			want:    booleanColumns("1", "0"),
		},
		{
			name:    "MySQL boolean defaults stored form, markdown page",
			dialect: "mysql",
			path:    "manifest/user.md",
			content: booleanPage,
			want:    booleanPageStored,
		},
		{
			name:    "PostgreSQL manifest boolean defaults untouched",
			dialect: "postgres",
			path:    "manifest/manifest_gen.json",
			content: manifestDoc(booleanColumns("true", "false")),
			want:    manifestDoc(booleanColumns("true", "false")),
		},
		{
			name:    "PostgreSQL markdown page boolean defaults untouched",
			dialect: "postgres",
			path:    "manifest/user.md",
			content: booleanPage,
			want:    booleanPage,
		},
		{
			name:    "Go file untouched",
			dialect: "mysql",
			path:    "user_gen.go",
			content: "// - **Source:** `x`\n\n## Files\n| `tinyint(1)` |  |  |  | `true` |\n",
			want:    "// - **Source:** `x`\n\n## Files\n| `tinyint(1)` |  |  |  | `true` |\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeTree(tt.dialect, map[string]string{tt.path: tt.content})
			if err != nil {
				t.Fatalf("normalizeTree(%s, %s): %v", tt.dialect, tt.path, err)
			}
			if strings.HasSuffix(tt.path, ".json") {
				if diff := cmp.Diff(decodeJSON(t, tt.want), decodeJSON(t, got[tt.path])); diff != "" {
					t.Errorf("normalizeTree(%s, %s) (-want +got):\n%s", tt.dialect, tt.path, diff)
				}
				return
			}
			if diff := cmp.Diff(tt.want, got[tt.path]); diff != "" {
				t.Errorf("normalizeTree(%s, %s) (-want +got):\n%s", tt.dialect, tt.path, diff)
			}
		})
	}
}

func TestNormalizeTree_ViewWithoutSource(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
	}{
		{name: "single layout", path: "manifest/manifest_gen.json", content: manifestDoc(viewEntityNoSource)},
		{name: "single layout empty source", path: "manifest/manifest_gen.json", content: manifestDoc(
			`{"kind": "view", "table": "active_users", "source": ""}`)},
		{name: "split layout entity file", path: "manifest/entities/active_user.json", content: viewEntityNoSource},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeTree("postgres", map[string]string{tt.path: tt.content})
			if err == nil || !strings.Contains(err.Error(), "view active_users has no source") {
				t.Errorf("normalizeTree(%s) error = %v, want the view without a source named", tt.path, err)
			}
		})
	}
}

func decodeJSON(t *testing.T, content string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(content), &v); err != nil {
		t.Fatalf("decoding %q: %v", content, err)
	}
	return v
}
