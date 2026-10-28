package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidate_CachedViewRequiresInvalidateOn pins the §4.9 rule on a parsed
// schema: a cached view needs a non-empty invalidate_on whether the view file
// holds a CREATE VIEW or a CREATE MATERIALIZED VIEW. A materialized view's
// refresh does not exempt it. The same project with invalidate_on set
// validates, so the view file parses and the rule is the only thing the
// failing run trips on.
func TestValidate_CachedViewRequiresInvalidateOn(t *testing.T) {
	tests := []struct {
		name    string
		viewSQL string
	}{
		{name: "regular view", viewSQL: "-- @pk: id\nCREATE VIEW sales_rollup AS SELECT id, total FROM sales;\n"},
		{name: "materialized view", viewSQL: "-- @pk: id\nCREATE MATERIALIZED VIEW sales_rollup AS SELECT id, total FROM sales;\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			sqlDir := filepath.Join(dir, "migrations")
			if err := os.MkdirAll(sqlDir, 0o750); err != nil {
				t.Fatalf("creating migrations dir: %v", err)
			}
			writeSQLFile(t, sqlDir, "CREATE TABLE sales (id bigint PRIMARY KEY, total numeric NOT NULL);")
			viewPath := filepath.Join(dir, "sales_rollup.sql")
			if err := os.WriteFile(viewPath, []byte(tt.viewSQL), 0o600); err != nil {
				t.Fatalf("writing view file: %v", err)
			}
			cfgPath := filepath.Join(dir, "sqlgen.yml")
			writeConfig := func(invalidateOn string) {
				t.Helper()
				content := `input:
  dialect: postgres
  paths:
    - "` + sqlDir + `"
  views:
    - "` + viewPath + `"
output:
  driver: pgx
  dir: ` + filepath.Join(dir, "output") + `
  package: models
cache:
  enabled: true
  key_prefix: app
views:
  sales_rollup:
` + invalidateOn + `    cache:
      enabled: true
`
				if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
					t.Fatalf("writing config: %v", err)
				}
			}

			writeConfig("    invalidate_on: [sales]\n")
			if _, stderr, err := executeCommand("validate", "--config", cfgPath); err != nil {
				t.Fatalf("validate with invalidate_on set = %v, want nil (stderr: %q)", err, stderr)
			}

			writeConfig("")
			_, stderr, err := executeCommand("validate", "--config", cfgPath)
			if err == nil {
				t.Fatal("validate with a cached view and no invalidate_on = nil, want an error")
			}
			const want = "views.sales_rollup.cache.enabled is true but invalidate_on is empty"
			if !strings.Contains(stderr, want) {
				t.Errorf("validate stderr = %q, want it to contain %q", stderr, want)
			}
		})
	}
}
