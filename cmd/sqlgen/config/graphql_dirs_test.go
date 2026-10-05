package config_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestLoadConfig_GraphDirsRootRelativeDefault pins §26.5.8: api.graphql
// schema_dir / resolver_dir are root-relative. When unset they default to a
// computed <output.dir>/graph (graph nested alongside the models package);
// explicit values are taken verbatim.
func TestLoadConfig_GraphDirsRootRelativeDefault(t *testing.T) {
	tests := []struct {
		name            string
		outputDir       string
		explicit        string // "" => omit schema_dir/resolver_dir (exercise the default)
		wantSchemaDir   string
		wantResolverDir string
	}{
		{
			name:            "unset defaults to <output.dir>/graph",
			outputDir:       "./models",
			wantSchemaDir:   "models/graph",
			wantResolverDir: "models/graph",
		},
		{
			name:            "unset with nested output.dir",
			outputDir:       "./internal/db",
			wantSchemaDir:   "internal/db/graph",
			wantResolverDir: "internal/db/graph",
		},
		{
			name:            "unset with root output.dir",
			outputDir:       ".",
			wantSchemaDir:   "graph",
			wantResolverDir: "graph",
		},
		{
			name:            "explicit top-level taken verbatim",
			outputDir:       "./models",
			explicit:        "graph",
			wantSchemaDir:   "graph",
			wantResolverDir: "graph",
		},
		{
			name:            "explicit nested taken verbatim",
			outputDir:       "./models",
			explicit:        "models/graph",
			wantSchemaDir:   "models/graph",
			wantResolverDir: "models/graph",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ` + tt.outputDir + `
api:
  enabled: true
  graphql:
    enabled: true
`
			if tt.explicit != "" {
				yaml += "    schema_dir: " + tt.explicit + "\n"
				yaml += "    resolver_dir: " + tt.explicit + "\n"
			}

			path := writeTestConfig(t, yaml)
			cfg, err := config.LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig() unexpected error: %v", err)
			}
			if got := cfg.API.GraphQL.SchemaDir; got != tt.wantSchemaDir {
				t.Errorf("SchemaDir = %q, want %q", got, tt.wantSchemaDir)
			}
			if got := cfg.API.GraphQL.ResolverDir; got != tt.wantResolverDir {
				t.Errorf("ResolverDir = %q, want %q", got, tt.wantResolverDir)
			}
		})
	}
}

// TestValidatePreParse_APIGraphQLDisabledIsInert pins §26.5.8: when
// api.graphql.enabled is false the whole block is inert — no api.graphql.*
// field is validated. An invalid field_casing that errors when enabled passes
// cleanly when the block is disabled.
func TestValidatePreParse_APIGraphQLDisabledIsInert(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		wantErr bool
	}{
		{name: "enabled rejects invalid field_casing", enabled: true, wantErr: true},
		{name: "disabled ignores invalid field_casing", enabled: false, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.API = &config.APIConfig{
				Enabled: true,
				GraphQL: &config.GraphQLAPIConfig{
					Enabled:     tt.enabled,
					FieldCasing: "bogus_casing",
					SchemaDir:   "graph",
				},
			}
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidatePreParse: expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidatePreParse: unexpected error: %v", err)
			}
		})
	}
}
