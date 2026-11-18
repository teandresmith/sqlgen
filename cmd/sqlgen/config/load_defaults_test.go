package config_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

const ldBase = "input:\n  dialect: postgres\n  paths: [\"./migrations\"]\noutput:\n  dir: ./models\n"

// ldLoad loads a config made of ldBase followed by extra.
func ldLoad(t *testing.T, extra string) *config.RootConfig {
	t.Helper()
	cfg, err := config.LoadConfig(writeTestConfig(t, ldBase+extra))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	return cfg
}

// TestLoadConfig_CacheSubBlockExplicitValuesSurviveDefaults pins that the
// per-key defaulting of a written hydration or circuit_breaker block keeps
// each key the block sets as written, an explicit false or 0 included, so
// validation still sees what the user wrote, and gives every key it leaves
// out its §27.2 default. Each set value differs from its default, and each
// case sets only its own keys, so a partial block is what is decoded.
func TestLoadConfig_CacheSubBlockExplicitValuesSurviveDefaults(t *testing.T) {
	const cache = "cache:\n  enabled: true\n"

	t.Run("hydration turned off stays off", func(t *testing.T) {
		cfg := ldLoad(t, cache+"  hydration:\n    enabled: false\n")
		want := config.HydrationConfig{Enabled: false, Timeout: "30s"}
		if got := *cfg.Cache.Hydration; got != want {
			t.Errorf("Cache.Hydration = %+v, want %+v", got, want)
		}
	})

	t.Run("hydration with only a timeout stays enabled", func(t *testing.T) {
		cfg := ldLoad(t, cache+"  hydration:\n    timeout: 10s\n")
		want := config.HydrationConfig{Enabled: true, Timeout: "10s"}
		if got := *cfg.Cache.Hydration; got != want {
			t.Errorf("Cache.Hydration = %+v, want %+v", got, want)
		}
	})

	t.Run("circuit breaker with only a threshold keeps the other defaults", func(t *testing.T) {
		cfg := ldLoad(t, cache+"  circuit_breaker:\n    failure_threshold: 3\n")
		want := config.CircuitBreakerConfig{Enabled: true, FailureThreshold: 3, ProbeInterval: "30s", HalfOpenMaxProbes: 1}
		if got := *cfg.Cache.CircuitBreaker; got != want {
			t.Errorf("Cache.CircuitBreaker = %+v, want %+v", got, want)
		}
	})

	t.Run("circuit breaker with only half open max probes keeps the other defaults", func(t *testing.T) {
		cfg := ldLoad(t, cache+"  circuit_breaker:\n    half_open_max_probes: 4\n")
		want := config.CircuitBreakerConfig{Enabled: true, FailureThreshold: 5, ProbeInterval: "30s", HalfOpenMaxProbes: 4}
		if got := *cfg.Cache.CircuitBreaker; got != want {
			t.Errorf("Cache.CircuitBreaker = %+v, want %+v", got, want)
		}
	})

	t.Run("circuit breaker turned off keeps the numeric defaults and validates", func(t *testing.T) {
		cfg := ldLoad(t, cache+"  key_prefix: app\n  circuit_breaker:\n    enabled: false\n")
		want := config.CircuitBreakerConfig{Enabled: false, FailureThreshold: 5, ProbeInterval: "30s", HalfOpenMaxProbes: 1}
		if got := *cfg.Cache.CircuitBreaker; got != want {
			t.Errorf("Cache.CircuitBreaker = %+v, want %+v", got, want)
		}
		if _, err := config.ValidatePreParse(cfg); err != nil {
			t.Errorf("ValidatePreParse() error = %v, want nil", err)
		}
	})

	t.Run("hydration with a null enabled takes the default", func(t *testing.T) {
		cfg := ldLoad(t, cache+"  hydration:\n    enabled:\n    timeout: 5s\n")
		want := config.HydrationConfig{Enabled: true, Timeout: "5s"}
		if got := *cfg.Cache.Hydration; got != want {
			t.Errorf("Cache.Hydration = %+v, want %+v", got, want)
		}
	})

	t.Run("circuit breaker with only a probe interval keeps the other defaults", func(t *testing.T) {
		cfg := ldLoad(t, cache+"  circuit_breaker:\n    probe_interval: 1m\n")
		want := config.CircuitBreakerConfig{Enabled: true, FailureThreshold: 5, ProbeInterval: "1m", HalfOpenMaxProbes: 1}
		if got := *cfg.Cache.CircuitBreaker; got != want {
			t.Errorf("Cache.CircuitBreaker = %+v, want %+v", got, want)
		}
	})

	t.Run("circuit breaker threshold from an environment reference is kept", func(t *testing.T) {
		t.Setenv("LD_BREAKER_THRESHOLD", "7")
		cfg := ldLoad(t, cache+"  circuit_breaker:\n    failure_threshold: ${LD_BREAKER_THRESHOLD}\n")
		want := config.CircuitBreakerConfig{Enabled: true, FailureThreshold: 7, ProbeInterval: "30s", HalfOpenMaxProbes: 1}
		if got := *cfg.Cache.CircuitBreaker; got != want {
			t.Errorf("Cache.CircuitBreaker = %+v, want %+v", got, want)
		}
	})

	zeroes := []struct {
		name    string
		key     string
		wantErr string
	}{
		{name: "explicit zero failure threshold is rejected", key: "failure_threshold", wantErr: "cache.circuit_breaker.failure_threshold: 0 is not valid"},
		{name: "explicit zero half-open probes is rejected", key: "half_open_max_probes", wantErr: "cache.circuit_breaker.half_open_max_probes: 0 is not valid"},
	}
	for _, tt := range zeroes {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ldLoad(t, cache+"  circuit_breaker:\n    "+tt.key+": 0\n")
			_, err := config.ValidatePreParse(cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidatePreParse() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestLoadConfig_ExplicitEmptyGenerationListsAreKept pins that a generation
// list written as `[]` stays empty instead of taking the default list, while a
// missing key still takes it.
func TestLoadConfig_ExplicitEmptyGenerationListsAreKept(t *testing.T) {
	t.Run("explicit empty lists stay empty", func(t *testing.T) {
		cfg := ldLoad(t, "generation:\n  soft_delete_columns: []\n  update_columns: []\n  cursor_keys: []\n")
		g := cfg.Generation
		if g.SoftDeleteColumns == nil || len(g.SoftDeleteColumns) != 0 {
			t.Errorf("Generation.SoftDeleteColumns = %#v, want an empty non-nil list", g.SoftDeleteColumns)
		}
		if g.UpdateColumns == nil || len(g.UpdateColumns) != 0 {
			t.Errorf("Generation.UpdateColumns = %#v, want an empty non-nil list", g.UpdateColumns)
		}
		if g.CursorKeys == nil || len(g.CursorKeys) != 0 {
			t.Errorf("Generation.CursorKeys = %#v, want an empty non-nil list", g.CursorKeys)
		}
	})

	t.Run("missing keys take the defaults", func(t *testing.T) {
		cfg := ldLoad(t, "")
		g := cfg.Generation
		if len(g.SoftDeleteColumns) == 0 || g.SoftDeleteColumns[0].Name != "deleted_at" {
			t.Errorf("Generation.SoftDeleteColumns = %#v, want the default list starting with deleted_at", g.SoftDeleteColumns)
		}
		if len(g.UpdateColumns) == 0 || g.UpdateColumns[0] != "updated_at" {
			t.Errorf("Generation.UpdateColumns = %#v, want the default list starting with updated_at", g.UpdateColumns)
		}
		if diff := cmp.Diff([]string{"id"}, g.CursorKeys); diff != "" {
			t.Errorf("Generation.CursorKeys mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("per-table empty cursor keys differ from unset", func(t *testing.T) {
		cfg := ldLoad(t, "tables:\n  products:\n    cursor_keys: []\n  orders:\n    struct_name: Order\n")
		if got := cfg.Tables["products"].CursorKeys; got == nil || len(got) != 0 {
			t.Errorf("tables.products.CursorKeys = %#v, want an empty non-nil list", got)
		}
		if got := cfg.Tables["orders"].CursorKeys; got != nil {
			t.Errorf("tables.orders.CursorKeys = %#v, want nil", got)
		}
	})
}

// TestLoadConfig_RemovedKeysAreUnknown pins that keys dropped from the config
// surface are rejected by the strict loader instead of being ignored.
func TestLoadConfig_RemovedKeysAreUnknown(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "input parse_mode",
			yaml:    "input:\n  dialect: postgres\n  paths: [\"./migrations\"]\n  parse_mode: strict\noutput:\n  dir: ./models\n",
			wantErr: "field parse_mode not found",
		},
		{
			name:    "view sql",
			yaml:    ldBase + "views:\n  product_summary:\n    sql: ./views/product_summary.sql\n",
			wantErr: "field sql not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.LoadConfig(writeTestConfig(t, tt.yaml))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("LoadConfig() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
