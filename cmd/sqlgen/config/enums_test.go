package config_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// The top-level `enums:` block (PRD §4.11). It is the remedy for a GraphQL
// type-name collision (PRD §26.4), so the keys have to survive YAML
// loading in both the bare and schema-qualified spellings — gen resolves them
// in that order and a silently-dropped entry would leave the collision
// unfixable from config.
func TestLoadConfig_EnumsBlock(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./internal/models
enums:
  duration:
    struct_name: MediaDuration
    description: How long a lesson runs.
  public.order_status:
    struct_name: OrderState
  untouched: {}
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	want := map[string]config.EnumConfig{
		"duration":            {StructName: "MediaDuration", Description: "How long a lesson runs."},
		"public.order_status": {StructName: "OrderState"},
		"untouched":           {},
	}
	if diff := cmp.Diff(want, cfg.Enums); diff != "" {
		t.Errorf("LoadConfig().Enums mismatch (-want +got):\n%s", diff)
	}
}

// A config with no `enums:` block leaves the map nil, which EnumConfigFor
// reads as "no entry" for every enum.
func TestLoadConfig_EnumsBlockAbsent(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./internal/models
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}
	if len(cfg.Enums) != 0 {
		t.Errorf("LoadConfig().Enums = %v, want empty", cfg.Enums)
	}
}
