package config_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

func apiOpsConfig() *config.RootConfig {
	cfg := validConfig()
	cfg.API = &config.APIConfig{
		Enabled: true,
		GraphQL: &config.GraphQLAPIConfig{Enabled: true},
	}
	return cfg
}

// TestValidateAPIOperations_ClientOnlyRejected pins the rule that an
// operation with no API projection cannot be named in an api.operations
// block — silently ignoring a knob the user deliberately turned is worse
// than saying the block cannot express it.
func TestValidateAPIOperations_ClientOnlyRejected(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(o *config.Operations)
		wantErr string
	}{
		{"exists", func(o *config.Operations) { o.Exists = new(false) }, "exists"},
		{"count", func(o *config.Operations) { o.Count = new(false) }, "count"},
		{"increment", func(o *config.Operations) { o.Increment = new(false) }, "increment"},
		{"stream", func(o *config.Operations) { o.Stream = new(false) }, "stream"},
		// No batch-of-items update mutation is generated: update<T>s takes a
		// filter and is backed by UpdateWhere.
		{"update_many", func(o *config.Operations) { o.UpdateMany = new(false) }, "update_where"},
		// No list-of-ids query is generated: <T>List takes a filter and is
		// backed by Paginate, so it is gated on paginate.
		// The message names the key that gates the surface the user meant.
		{"get_many", func(o *config.Operations) { o.GetMany = new(false) }, "gated on paginate"},
		{"paginate is accepted", func(o *config.Operations) { o.Paginate = new(false) }, ""},
		// No batch-upsert mutation is generated, so the mask cannot express it
		// (PRD §26.5.1 exclusion table, §26.10 closed list).
		{"upsert_many", func(o *config.Operations) { o.UpsertMany = new(false) }, "upsert_many"},
		// An API operation is fine.
		{"create is accepted", func(o *config.Operations) { o.Create = new(false) }, ""},
		// update_where gates update<T>s(filter, input), the mutation it backs.
		{"update_where is accepted", func(o *config.Operations) { o.UpdateWhere = new(false) }, ""},
		// The three nested mutations have GraphQL projections (PRD §26.5.1),
		// so they are maskable like any other mutation.
		{"create_with_related is accepted", func(o *config.Operations) { o.CreateWithRelated = new(false) }, ""},
		{"update_with_related is accepted", func(o *config.Operations) { o.UpdateWithRelated = new(false) }, ""},
		{"upsert_with_related is accepted", func(o *config.Operations) { o.UpsertWithRelated = new(false) }, ""},
	}

	for _, tt := range tests {
		t.Run("global/"+tt.name, func(t *testing.T) {
			cfg := apiOpsConfig()
			ops := &config.Operations{}
			tt.mutate(ops)
			cfg.API.Operations = ops

			_, err := config.ValidatePreParse(cfg)
			assertOpErr(t, err, tt.wantErr, "api.operations")
		})
		t.Run("per-table/"+tt.name, func(t *testing.T) {
			cfg := apiOpsConfig()
			ops := &config.Operations{}
			tt.mutate(ops)
			cfg.Tables["tasks"] = config.TableConfig{
				API: &config.TableAPIConfig{Operations: ops},
			}

			_, err := config.ValidatePreParse(cfg)
			assertOpErr(t, err, tt.wantErr, "tables.tasks.api.operations")
		})
	}
}

func assertOpErr(t *testing.T, err error, wantOp, wantPath string) {
	t.Helper()
	if wantOp == "" {
		if err != nil {
			t.Errorf("ValidatePreParse() unexpected error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("ValidatePreParse() = nil, want an error naming %q", wantOp)
	}
	msg := err.Error()
	if !strings.Contains(msg, wantOp) || !strings.Contains(msg, "not an API operation") {
		t.Errorf("error = %v, want it to name %q as not an API operation", err, wantOp)
	}
	if !strings.Contains(msg, wantPath) {
		t.Errorf("error = %v, want it to name the config path %q", err, wantPath)
	}
}

// TestValidateAPIOperations_PresetValidated covers the bare-string preset form.
func TestValidateAPIOperations_PresetValidated(t *testing.T) {
	cfg := apiOpsConfig()
	cfg.API.Operations = &config.Operations{Preset: "write_only"}

	_, err := config.ValidatePreParse(cfg)
	if err == nil || !strings.Contains(err.Error(), "api.operations.preset") {
		t.Errorf("ValidatePreParse() error = %v, want it to reject the unknown preset", err)
	}

	cfg.API.Operations = &config.Operations{Preset: config.PresetReadOnly}
	if _, err := config.ValidatePreParse(cfg); err != nil {
		t.Errorf("ValidatePreParse() rejected a valid preset: %v", err)
	}
}

// TestResolveAPIOperationsMask covers the resolution order: per-table wins
// over global, and absent means nil (no mask at all, not an empty one).
func TestResolveAPIOperationsMask(t *testing.T) {
	global := &config.APIConfig{Operations: &config.Operations{Preset: config.PresetReadOnly}}

	t.Run("no mask configured", func(t *testing.T) {
		got, err := config.ResolveAPIOperationsMask(config.TableConfig{}, &config.APIConfig{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("mask = %+v, want nil", got)
		}
	})

	t.Run("global applies", func(t *testing.T) {
		got, err := config.ResolveAPIOperationsMask(config.TableConfig{}, global)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil || got.Create == nil || *got.Create {
			t.Errorf("read_only global mask should disable create, got %+v", got)
		}
	})

	t.Run("per-table replaces global wholesale", func(t *testing.T) {
		table := config.TableConfig{
			API: &config.TableAPIConfig{Operations: &config.Operations{Upsert: new(false)}},
		}
		got, err := config.ResolveAPIOperationsMask(table, global)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The table mask expands from `all`, so create returns even though
		// the global mask was read_only — replacement, not merge.
		if got.Create == nil || !*got.Create {
			t.Errorf("per-table mask should replace the global read_only mask, got create=%v", got.Create)
		}
		if got.Upsert == nil || *got.Upsert {
			t.Errorf("per-table mask should disable upsert, got %v", got.Upsert)
		}
	})
}
