package config_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestLoadConfig_AcronymSetNotConfigurable pins PRD §8.5 "Acronym Detection":
// the acronym set is not consumer-configurable. There is no config key that
// carries one, so a config that tries to supply a list is rejected by the
// strict loader rather than silently ignored.
func TestLoadConfig_AcronymSetNotConfigurable(t *testing.T) {
	const base = `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
`

	t.Run("a config without an acronyms key loads", func(t *testing.T) {
		if _, err := config.LoadConfig(writeTestConfig(t, base)); err != nil {
			t.Fatalf("LoadConfig() error = %v, want the base config to load", err)
		}
	})

	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "an acronyms list at the top level is rejected",
			yaml: base + `acronyms: ["SLA", "UID"]
`,
		},
		{
			name: "an acronyms list under generation is rejected",
			yaml: base + `generation:
  acronyms: ["SLA", "UID"]
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.LoadConfig(writeTestConfig(t, tt.yaml))
			if err == nil {
				t.Fatal("LoadConfig() accepted an acronyms key, want an error: the acronym set is not consumer-configurable")
			}
			if !strings.Contains(err.Error(), "acronyms") {
				t.Errorf("LoadConfig() error = %v, want it to name the acronyms key", err)
			}
		})
	}
}
