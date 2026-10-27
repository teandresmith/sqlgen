package config_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestValidatePreParse_EventsPerTableOptInForbidden pins PRD §28.8 / §4.13: the
// per-table events.enabled only opts a table out, so an explicit true under a
// global off — or with no `events:` block at all — is an error, never a
// warning, and names the key.
func TestValidatePreParse_EventsPerTableOptInForbidden(t *testing.T) {
	const base = "input:\n  dialect: postgres\n  paths: [\"./migrations\"]\noutput:\n  dir: ./models\n"
	const wantMsg = "tables.users.events.enabled: cannot be true when global events.enabled is false — per-table flag is opt-out only (PRD §28.8 / §4.13)"

	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name:    "table opt-in under global off is an error",
			yaml:    "events:\n  enabled: false\ntables:\n  users:\n    events:\n      enabled: true\n",
			wantErr: true,
		},
		{
			name:    "table opt-in with no events block is an error",
			yaml:    "tables:\n  users:\n    events:\n      enabled: true\n",
			wantErr: true,
		},
		{
			name:    "table opt-in under global on is valid",
			yaml:    "events:\n  enabled: true\ntables:\n  users:\n    events:\n      enabled: true\n",
			wantErr: false,
		},
		{
			name:    "table opt-out under global on is valid",
			yaml:    "events:\n  enabled: true\ntables:\n  users:\n    events:\n      enabled: false\n",
			wantErr: false,
		},
		{
			name:    "table opt-out under global off is valid",
			yaml:    "events:\n  enabled: false\ntables:\n  users:\n    events:\n      enabled: false\n",
			wantErr: false,
		},
		{
			name:    "table events block without enabled is valid",
			yaml:    "tables:\n  users:\n    events: {}\n",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.LoadConfig(writeTestConfig(t, base+tt.yaml))
			if err != nil {
				t.Fatalf("LoadConfig() error = %v, want the config to decode", err)
			}
			warnings, err := config.ValidatePreParse(cfg)
			for _, w := range warnings {
				if strings.Contains(w.Message, "events.enabled") {
					t.Errorf("ValidatePreParse() warning = %q, want the events rule reported as an error only", w.Message)
				}
			}
			if !tt.wantErr {
				if err != nil {
					t.Errorf("ValidatePreParse() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePreParse() = nil, want %q", wantMsg)
			}
			if !strings.Contains(err.Error(), wantMsg) {
				t.Errorf("ValidatePreParse() error = %v, want it to contain %q", err, wantMsg)
			}
		})
	}
}
