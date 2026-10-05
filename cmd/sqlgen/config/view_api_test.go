package config_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// `views.<name>.api` — the read-only twin of `tables.<name>.api` (PRD §4.9 /
// §4.13 / §26.4 "Views on the GraphQL surface").

func viewAPIConfig(v config.ViewConfig) *config.RootConfig {
	cfg := validConfig()
	cfg.API = &config.APIConfig{
		Enabled: true,
		GraphQL: &config.GraphQLAPIConfig{Enabled: true},
	}
	cfg.Views["product_summary"] = v
	return cfg
}

// TestValidateViewAPIOperations_MutationRejected pins the §4.13 rule: a view is
// read-only, so naming a mutation in its operations mask is a config error
// naming the view and the key — not a silent no-op. Same call
// clientOnlyOperationFields already makes for exists / count / increment /
// stream / update_many / upsert_many, one entity kind over.
func TestValidateViewAPIOperations_MutationRejected(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(o *config.Operations)
		wantKey string
	}{
		{"create", func(o *config.Operations) { o.Create = new(true) }, "create"},
		{"create_many", func(o *config.Operations) { o.CreateMany = new(false) }, "create_many"},
		{"update", func(o *config.Operations) { o.Update = new(true) }, "update"},
		{"update_where", func(o *config.Operations) { o.UpdateWhere = new(false) }, "update_where"},
		{"upsert", func(o *config.Operations) { o.Upsert = new(true) }, "upsert"},
		{"soft_delete", func(o *config.Operations) { o.SoftDelete = new(true) }, "soft_delete"},
		{"hard_delete", func(o *config.Operations) { o.HardDelete = new(false) }, "hard_delete"},
		{"restore", func(o *config.Operations) { o.Restore = new(true) }, "restore"},
		// The three nested mutations join the mutation half automatically:
		// mutationAPIOperationFields is derived from apiOperationFields, so a
		// new API operation lands in one table and is rejected on views until
		// someone decides otherwise.
		{"create_with_related", func(o *config.Operations) { o.CreateWithRelated = new(true) }, "create_with_related"},
		{"update_with_related", func(o *config.Operations) { o.UpdateWithRelated = new(true) }, "update_with_related"},
		{"upsert_with_related", func(o *config.Operations) { o.UpsertWithRelated = new(false) }, "upsert_with_related"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops := &config.Operations{}
			tt.mutate(ops)
			cfg := viewAPIConfig(config.ViewConfig{
				StructName: "ProductSummary",
				SQL:        "./views/product_summary.sql",
				API:        &config.ViewAPIConfig{Operations: ops},
			})

			_, err := config.ValidatePreParse(cfg)
			if err == nil {
				t.Fatalf("ValidatePreParse() = nil, want an error naming %q", tt.wantKey)
			}
			msg := err.Error()
			for _, want := range []string{
				"views.product_summary.api.operations." + tt.wantKey,
				"product_summary",
				"read-only",
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("error = %v, want it to contain %q", err, want)
				}
			}
		})
	}
}

// TestValidateViewAPIOperations_ReadOpsAccepted pins the other half: the three
// read keys are exactly what the block can express, and a `false` on any of
// them is a legitimate narrowing.
func TestValidateViewAPIOperations_ReadOpsAccepted(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(o *config.Operations)
	}{
		{"get", func(o *config.Operations) { o.Get = new(false) }},
		{"paginate", func(o *config.Operations) { o.Paginate = new(false) }},
		{"connection", func(o *config.Operations) { o.Connection = new(false) }},
		{"all three", func(o *config.Operations) {
			o.Get, o.Paginate, o.Connection = new(false), new(false), new(false)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ops := &config.Operations{}
			tt.mutate(ops)
			cfg := viewAPIConfig(config.ViewConfig{
				StructName: "ProductSummary",
				SQL:        "./views/product_summary.sql",
				API:        &config.ViewAPIConfig{Operations: ops},
			})
			if _, err := config.ValidatePreParse(cfg); err != nil {
				t.Errorf("ValidatePreParse() rejected a read-operation mask: %v", err)
			}
		})
	}
}

// TestValidateViewAPIOperations_ClientOnlyRejected: the operations with no API
// projection at all are rejected on a view for the reason they are rejected on
// a table, and the message says which reason applies.
func TestValidateViewAPIOperations_ClientOnlyRejected(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(o *config.Operations)
	}{
		{"exists", func(o *config.Operations) { o.Exists = new(false) }},
		{"count", func(o *config.Operations) { o.Count = new(false) }},
		{"get_many", func(o *config.Operations) { o.GetMany = new(false) }},
		{"stream", func(o *config.Operations) { o.Stream = new(false) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ops := &config.Operations{}
			tt.mutate(ops)
			cfg := viewAPIConfig(config.ViewConfig{
				StructName: "ProductSummary",
				SQL:        "./views/product_summary.sql",
				API:        &config.ViewAPIConfig{Operations: ops},
			})
			_, err := config.ValidatePreParse(cfg)
			if err == nil {
				t.Fatalf("ValidatePreParse() = nil, want an error naming %q", tt.name)
			}
			if !strings.Contains(err.Error(), "not an API operation") {
				t.Errorf("error = %v, want it to name %q as not an API operation", err, tt.name)
			}
		})
	}
}

// TestValidateViewAPIOperations_RejectedWithoutTopLevelAPIBlock pins the
// registration bug: validateAPIOperations returns early when cfg.API is nil, so
// hanging the view rule off its tail made `views.x.api.operations.create` with
// no `api:` block validate clean — precisely the silently-ignored knob the rule
// exists to reject.
func TestValidateViewAPIOperations_RejectedWithoutTopLevelAPIBlock(t *testing.T) {
	cfg := viewAPIConfig(config.ViewConfig{
		StructName: "ProductSummary",
		SQL:        "./views/product_summary.sql",
		API: &config.ViewAPIConfig{
			Operations: &config.Operations{Create: new(true)},
		},
	})
	cfg.API = nil

	_, err := config.ValidatePreParse(cfg)
	if err == nil {
		t.Fatal("ValidatePreParse() = nil — a mutation key in a view mask must be rejected with or without a top-level api: block")
	}
	if !strings.Contains(err.Error(), "views.product_summary.api.operations.create") {
		t.Errorf("error = %v, want it to name the offending key", err)
	}
}

// TestValidateViewAPIOperations_PresetValidated covers the bare-string preset
// form on a view's mask.
func TestValidateViewAPIOperations_PresetValidated(t *testing.T) {
	cfg := viewAPIConfig(config.ViewConfig{
		StructName: "ProductSummary",
		SQL:        "./views/product_summary.sql",
		API:        &config.ViewAPIConfig{Operations: &config.Operations{Preset: "write_only"}},
	})
	_, err := config.ValidatePreParse(cfg)
	if err == nil || !strings.Contains(err.Error(), "views.product_summary.api.operations.preset") {
		t.Errorf("ValidatePreParse() error = %v, want it to reject the unknown preset", err)
	}

	cfg = viewAPIConfig(config.ViewConfig{
		StructName: "ProductSummary",
		SQL:        "./views/product_summary.sql",
		API:        &config.ViewAPIConfig{Operations: &config.Operations{Preset: config.PresetReadOnly}},
	})
	if _, err := config.ValidatePreParse(cfg); err != nil {
		t.Errorf("ValidatePreParse() rejected a valid preset: %v", err)
	}
}

// TestValidateViewAPIOptIn pins the opt-OUT-only rule: a single view cannot be
// opted in without the package-level opt-in, matching the
// tables.<name>.manifest precedent (PRD §4.13).
func TestValidateViewAPIOptIn(t *testing.T) {
	t.Run("enabled true under a global opt-out is an error", func(t *testing.T) {
		cfg := viewAPIConfig(config.ViewConfig{
			StructName: "ProductSummary",
			SQL:        "./views/product_summary.sql",
			API:        &config.ViewAPIConfig{Enabled: new(true)},
		})
		cfg.API.Enabled = false

		_, err := config.ValidatePreParse(cfg)
		if err == nil {
			t.Fatal("ValidatePreParse() = nil, want an error")
		}
		for _, want := range []string{"views.product_summary.api.enabled", "opt-out only"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %v, want it to contain %q", err, want)
			}
		}
	})

	t.Run("enabled false under a global opt-out is fine", func(t *testing.T) {
		cfg := viewAPIConfig(config.ViewConfig{
			StructName: "ProductSummary",
			SQL:        "./views/product_summary.sql",
			API:        &config.ViewAPIConfig{Enabled: new(false)},
		})
		cfg.API.Enabled = false
		if _, err := config.ValidatePreParse(cfg); err != nil {
			t.Errorf("ValidatePreParse() unexpected error: %v", err)
		}
	})

	t.Run("enabled true with the global opt-in is fine", func(t *testing.T) {
		cfg := viewAPIConfig(config.ViewConfig{
			StructName: "ProductSummary",
			SQL:        "./views/product_summary.sql",
			API:        &config.ViewAPIConfig{Enabled: new(true)},
		})
		if _, err := config.ValidatePreParse(cfg); err != nil {
			t.Errorf("ValidatePreParse() unexpected error: %v", err)
		}
	})
}

// TestResolveViewAPIEnabled pins the tri-state resolution order: per-view
// override → global api.enabled → false.
func TestResolveViewAPIEnabled(t *testing.T) {
	tests := []struct {
		name string
		view config.ViewConfig
		api  *config.APIConfig
		want bool
	}{
		{"no api block at all", config.ViewConfig{}, nil, false},
		{"global off", config.ViewConfig{}, &config.APIConfig{Enabled: false}, false},
		{"global on, view silent", config.ViewConfig{}, &config.APIConfig{Enabled: true}, true},
		{
			"view opts out of an enabled global",
			config.ViewConfig{API: &config.ViewAPIConfig{Enabled: new(false)}},
			&config.APIConfig{Enabled: true},
			false,
		},
		{
			"view opts in with an enabled global",
			config.ViewConfig{API: &config.ViewAPIConfig{Enabled: new(true)}},
			&config.APIConfig{Enabled: true},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := config.ResolveViewAPIEnabled(tt.view, tt.api); got != tt.want {
				t.Errorf("ResolveViewAPIEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestResolveViewAPIOperationsMask pins the mask precedence — per-view block
// replaces the global one wholesale, exactly as the table rule does — and that
// preset expansion happens at resolution rather than at intersection time.
func TestResolveViewAPIOperationsMask(t *testing.T) {
	global := &config.APIConfig{
		Enabled:    true,
		Operations: &config.Operations{Preset: config.PresetReadOnly},
	}

	t.Run("no mask anywhere", func(t *testing.T) {
		got, err := config.ResolveViewAPIOperationsMask(config.ViewConfig{}, &config.APIConfig{Enabled: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("mask = %+v, want nil", got)
		}
	})

	t.Run("view inherits the global mask", func(t *testing.T) {
		got, err := config.ResolveViewAPIOperationsMask(config.ViewConfig{}, global)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("mask = nil, want the expanded global mask")
		}
		if got.Get == nil || !*got.Get {
			t.Errorf("read_only preset did not expand Get: %+v", got.Get)
		}
	})

	t.Run("per-view mask replaces the global one wholesale", func(t *testing.T) {
		view := config.ViewConfig{API: &config.ViewAPIConfig{
			Operations: &config.Operations{Connection: new(false)},
		}}
		got, err := config.ResolveViewAPIOperationsMask(view, global)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("mask = nil, want the expanded per-view mask")
		}
		if got.Connection == nil || *got.Connection {
			t.Errorf("Connection = %v, want an explicit false", got.Connection)
		}
		// Unset fields expand from the `all` preset, so the bare
		// `{connection: false}` block subtracts nothing else.
		if got.Get == nil || !*got.Get {
			t.Errorf("Get = %v, want true from the default preset expansion", got.Get)
		}
	})

	t.Run("invalid preset surfaces as an error", func(t *testing.T) {
		view := config.ViewConfig{API: &config.ViewAPIConfig{
			Operations: &config.Operations{Preset: "write_only"},
		}}
		if _, err := config.ResolveViewAPIOperationsMask(view, global); err == nil {
			t.Error("ResolveViewAPIOperationsMask() = nil error, want a rejection of the unknown preset")
		}
	})
}
