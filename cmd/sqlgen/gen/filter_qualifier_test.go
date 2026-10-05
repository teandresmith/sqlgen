package gen

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

func TestQualifyFilter_DispatchesByDialect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dialect  config.Dialect
		filter   string
		alias    string
		contains string
	}{
		{
			name:     "postgres routes to pg_query",
			dialect:  config.DialectPostgres,
			filter:   "entity_type = 'asset.primary' AND name LIKE 'site_%'",
			alias:    "d",
			contains: "d.entity_type",
		},
		{
			name:     "mysql routes to vitess",
			dialect:  config.DialectMySQL,
			filter:   "entity_type = 'asset.primary' AND name LIKE 'site_%'",
			alias:    "d",
			contains: "d.entity_type",
		},
		{
			name:     "sqlite routes to rqlite/sql",
			dialect:  config.DialectSQLite,
			filter:   "entity_type = 'asset.primary' AND name LIKE 'site_%'",
			alias:    "d",
			contains: `"d"."entity_type"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := qualifyFilter(tt.filter, tt.alias, tt.dialect)
			if err != nil {
				t.Fatalf("qualifyFilter: %v", err)
			}
			if !strings.Contains(got, tt.contains) {
				t.Errorf("qualifyFilter result missing %q in %q", tt.contains, got)
			}
		})
	}
}

func TestQualifyFilter_UnknownDialectErrors(t *testing.T) {
	t.Parallel()
	_, err := qualifyFilter("name = 'x'", "d", config.Dialect("ouija"))
	if err == nil {
		t.Fatalf("expected error for unknown dialect")
	}
}
