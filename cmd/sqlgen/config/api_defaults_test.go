package config_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// tscLoadAPI writes a minimal sqlgen.yml with apiBlock appended and loads it
// through the production loader, so the §26.3 defaults are applied exactly as
// a real run applies them.
func tscLoadAPI(t *testing.T, apiBlock string) *config.RootConfig {
	t.Helper()
	body := `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
` + apiBlock
	cfg, err := config.LoadConfig(writeTestConfig(t, body))
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v\nconfig:\n%s", err, body)
	}
	return cfg
}

// TestAPIDefaults pins the default column of PRD §26.3's API Config table.
func TestAPIDefaults(t *testing.T) {
	t.Run("api.enabled defaults to false", func(t *testing.T) {
		tests := []struct {
			name     string
			apiBlock string
		}{
			{name: "no api block", apiBlock: ""},
			{name: "api block without enabled", apiBlock: "api:\n  graphql:\n    enabled: true\n"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cfg := tscLoadAPI(t, tt.apiBlock)
				if cfg.API != nil && cfg.API.Enabled {
					t.Errorf("api.enabled = true, want false when unset")
				}
			})
		}
	})

	t.Run("api.graphql.enabled defaults to false", func(t *testing.T) {
		tests := []struct {
			name     string
			apiBlock string
		}{
			{name: "api block without graphql", apiBlock: "api:\n  enabled: true\n"},
			{name: "graphql block without enabled", apiBlock: "api:\n  enabled: true\n  graphql:\n    field_casing: snake_case\n"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cfg := tscLoadAPI(t, tt.apiBlock)
				if cfg.API == nil {
					t.Fatal("cfg.API = nil, want the api block loaded")
				}
				if cfg.API.GraphQL != nil && cfg.API.GraphQL.Enabled {
					t.Errorf("api.graphql.enabled = true, want false when unset")
				}
			})
		}
	})

	t.Run("package is derived from resolver_dir", func(t *testing.T) {
		tests := []struct {
			name        string
			resolverDir string // "" leaves resolver_dir unset
			wantPackage string
		}{
			{name: "unset resolver_dir derives graph", resolverDir: "", wantPackage: "graph"},
			{name: "explicit resolver_dir takes its last element", resolverDir: "internal/api", wantPackage: "api"},
			{name: "hyphenated resolver_dir becomes a valid identifier", resolverDir: "internal/my-graph", wantPackage: "my_graph"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				block := "api:\n  enabled: true\n  graphql:\n    enabled: true\n"
				if tt.resolverDir != "" {
					block += "    resolver_dir: " + tt.resolverDir + "\n"
				}
				cfg := tscLoadAPI(t, block)
				if got := cfg.API.GraphQL.Package; got != tt.wantPackage {
					t.Errorf("api.graphql.package = %q, want %q (resolver_dir %q)", got, tt.wantPackage, cfg.API.GraphQL.ResolverDir)
				}
			})
		}
	})

	t.Run("an explicit package is kept", func(t *testing.T) {
		cfg := tscLoadAPI(t, "api:\n  enabled: true\n  graphql:\n    enabled: true\n    resolver_dir: internal/api\n    package: gql\n")
		if got := cfg.API.GraphQL.Package; got != "gql" {
			t.Errorf("api.graphql.package = %q, want the explicit %q", got, "gql")
		}
	})

	defaults := func(t *testing.T) *config.GraphQLAPIConfig {
		t.Helper()
		return tscLoadAPI(t, "api:\n  enabled: true\n  graphql:\n    enabled: true\n").API.GraphQL
	}

	t.Run("field casing defaults to camel_case", func(t *testing.T) {
		if got := defaults(t).FieldCasing; got != "camel_case" {
			t.Errorf("api.graphql.field_casing = %q, want %q", got, "camel_case")
		}
	})

	t.Run("gqlgen_config defaults to ./gqlgen.yml", func(t *testing.T) {
		if got := defaults(t).GqlgenConfig; got != "./gqlgen.yml" {
			t.Errorf("api.graphql.gqlgen_config = %q, want %q", got, "./gqlgen.yml")
		}
	})

	t.Run("gqlgen_bin defaults to go run of the gqlgen module", func(t *testing.T) {
		const want = "go run github.com/99designs/gqlgen"
		if got := defaults(t).GqlgenBin; got != want {
			t.Errorf("api.graphql.gqlgen_bin = %q, want %q", got, want)
		}
	})
}
