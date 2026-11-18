package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestLoadConfig_GraphTopologyHasNoModeFlag pins PRD §26.5.8: "There is no
// separate "layered"/"placement" mode flag: whether graph is nested under
// models or a top-level sibling is determined entirely by the resolved path."
// A mode key is not part of the api.graphql block, so the strict decoder
// rejects one rather than silently accepting a knob that does nothing.
func TestLoadConfig_GraphTopologyHasNoModeFlag(t *testing.T) {
	tests := []struct {
		name string
		key  string
		line string
	}{
		{name: "placement key is rejected", key: "placement", line: "    placement: top_level\n"},
		{name: "layered key is rejected", key: "layered", line: "    layered: true\n"},
		{name: "mode key is rejected", key: "mode", line: "    mode: layered\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
api:
  enabled: true
  graphql:
    enabled: true
    schema_dir: graph
    resolver_dir: graph
` + tt.line
			_, err := config.LoadConfig(writeTestConfig(t, yaml))
			if err == nil {
				t.Fatalf("LoadConfig accepted api.graphql.%s; graph topology must follow from the resolved path alone", tt.key)
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Errorf("LoadConfig error does not name the unknown key %q: %v", tt.key, err)
			}
		})
	}

	t.Run("no api.graphql field selects a topology", func(t *testing.T) {
		typ := reflect.TypeFor[config.GraphQLAPIConfig]()
		for i := range typ.NumField() {
			tag, _, _ := strings.Cut(typ.Field(i).Tag.Get("yaml"), ",")
			for _, banned := range []string{"mode", "layout", "placement", "layered", "top_level", "topology"} {
				if strings.Contains(tag, banned) {
					t.Errorf("api.graphql.%s (GraphQLAPIConfig.%s) looks like a topology mode flag", tag, typ.Field(i).Name)
				}
			}
		}
	})
}
