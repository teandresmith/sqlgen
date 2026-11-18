package manifest_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/parser"
)

// ffmTwoTableSchema is a two-table fixture for the package-level toggles that
// aggregate over every table.
func ffmTwoTableSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "email", Type: "text"}}},
			{Name: "posts", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "title", Type: "text"}}},
		},
	}
}

// TestBuild_GenerationConfigViewsFollowsGeneratedViews pins PRD §30.4.3:
// generation_config.views is "true if any view is generated". A view parsed
// from the schema is generated whether or not the config carries a `views:`
// block, so the toggle reads the generated set, not the config map.
func TestBuild_GenerationConfigViewsFollowsGeneratedViews(t *testing.T) {
	tests := []struct {
		name      string
		views     []parser.View
		wantViews bool
	}{
		{
			name: "schema view with no views config block is generated",
			views: []parser.View{{
				Name:    "active_users",
				SQL:     "SELECT id, email FROM users",
				Columns: []parser.Column{{Name: "id", Type: "uuid"}, {Name: "email", Type: "text"}},
			}},
			wantViews: true,
		},
		{
			name:      "no view in the schema",
			wantViews: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig() // cfg.Views is empty: no `views:` block
			schema := ffmTwoTableSchema()
			schema.Views = tt.views
			in := testBuildInput(t, schema, cfg)
			if got := len(in.Views) > 0; got != tt.wantViews {
				t.Fatalf("fixture: generated view contexts = %d, want views generated = %v", len(in.Views), tt.wantViews)
			}
			doc, err := manifest.Build(in)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if doc.GenerationConfig.Views != tt.wantViews {
				t.Errorf("generation_config.views = %v, want %v (generated views: %d, cfg.Views entries: %d)",
					doc.GenerationConfig.Views, tt.wantViews, len(in.Views), len(cfg.Views))
			}
		})
	}
}

// TestBuild_GenerationConfigCacheFollowsPerTableResolution pins PRD §30.4.3:
// generation_config.cache is "true if any table has cache configured". Cache
// is resolved per table (§4.6: tables.<name>.cache.enabled is a tri-state
// override that can opt out under a global true, or opt in under a global
// false), so the toggle reads the per-table resolution, not the global flag.
func TestBuild_GenerationConfigCacheFollowsPerTableResolution(t *testing.T) {
	tests := []struct {
		name      string
		global    bool
		perTable  map[string]bool
		wantCache bool
	}{
		{
			name:      "global on, every table opted out",
			global:    true,
			perTable:  map[string]bool{"users": false, "posts": false},
			wantCache: false,
		},
		{
			name:      "global off, one table opted in",
			global:    false,
			perTable:  map[string]bool{"users": true},
			wantCache: true,
		},
		{
			name:      "global on, no overrides",
			global:    true,
			wantCache: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Cache = &config.CacheConfig{Enabled: tt.global, TTL: "1m", KeyPrefix: "models", Serializer: config.SerializerJSON, Hydration: &config.HydrationConfig{}}
			for table, enabled := range tt.perTable {
				cfg.Tables[table] = config.TableConfig{Cache: &config.TableCacheConfig{Enabled: new(enabled)}}
			}
			doc, err := manifest.Build(testBuildInput(t, ffmTwoTableSchema(), cfg))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if doc.GenerationConfig.Cache != tt.wantCache {
				t.Errorf("generation_config.cache = %v, want %v (global cache.enabled = %v, per-table = %v)",
					doc.GenerationConfig.Cache, tt.wantCache, tt.global, tt.perTable)
			}
		})
	}
}

// TestBuild_GenerationConfigEventsFollowsPerTableResolution pins PRD §30.4.3:
// generation_config.events is "true if any table has event emission enabled".
// The global events block gates every table and tables.<name>.events.enabled
// only opts a table out (§28.8 — an opt-in under a global off is a validation
// error), so the toggle reads the per-table resolution, not the global flag.
func TestBuild_GenerationConfigEventsFollowsPerTableResolution(t *testing.T) {
	tests := []struct {
		name       string
		global     bool
		perTable   map[string]bool
		wantEvents bool
	}{
		{
			name:       "global on, every table opted out",
			global:     true,
			perTable:   map[string]bool{"users": false, "posts": false},
			wantEvents: false,
		},
		{
			name:       "global on, one table on and the other opted out",
			global:     true,
			perTable:   map[string]bool{"users": false, "posts": true},
			wantEvents: true,
		},
		{
			name:       "global off, no overrides",
			global:     false,
			wantEvents: false,
		},
		{
			name:       "global on, no overrides",
			global:     true,
			wantEvents: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Events = &config.EventConfig{Enabled: tt.global}
			for table, enabled := range tt.perTable {
				cfg.Tables[table] = config.TableConfig{Events: &config.TableEventConfig{Enabled: new(enabled)}}
			}
			doc, err := manifest.Build(testBuildInput(t, ffmTwoTableSchema(), cfg))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if doc.GenerationConfig.Events != tt.wantEvents {
				t.Errorf("generation_config.events = %v, want %v (global events.enabled = %v, per-table = %v)",
					doc.GenerationConfig.Events, tt.wantEvents, tt.global, tt.perTable)
			}
		})
	}
}

// TestBuild_FeaturesCacheFollowsPerTableResolution pins PRD §27.2 / §4.6 at
// the entity level: tables.<name>.cache.enabled wins in both directions, so a
// table opted in under a global false (or with no cache block at all) carries
// a populated features.cache, and a table opted out under a global true
// carries null. TTL resolves per table → global → the 1h default.
func TestBuild_FeaturesCacheFollowsPerTableResolution(t *testing.T) {
	tests := []struct {
		name      string
		global    *config.CacheConfig
		users     *config.TableCacheConfig
		wantUsers *manifest.CacheFeature
		wantPosts bool
	}{
		{
			name:   "global off, table opted in",
			global: &config.CacheConfig{Enabled: false, TTL: "1m", KeyPrefix: "models", Serializer: config.SerializerJSON, Hydration: &config.HydrationConfig{Enabled: true}},
			users:  &config.TableCacheConfig{Enabled: new(true)},
			wantUsers: &manifest.CacheFeature{
				TTLSeconds: 60, Hydration: "eager", KeyPattern: "models:user:{id}",
				InvalidatesOn: []string{"Update", "UpdateMany", "UpdateWhere", "Upsert", "UpsertMany", "HardDelete", "HardDeleteMany", "HardDeleteWhere"},
			},
			wantPosts: false,
		},
		{
			name:   "no cache block, table opted in with its own ttl",
			global: nil,
			users:  &config.TableCacheConfig{Enabled: new(true), TTL: new("30s")},
			wantUsers: &manifest.CacheFeature{
				TTLSeconds: 30, Hydration: "eager", KeyPattern: "sqlgen:user:{id}",
				InvalidatesOn: []string{"Update", "UpdateMany", "UpdateWhere", "Upsert", "UpsertMany", "HardDelete", "HardDeleteMany", "HardDeleteWhere"},
			},
			wantPosts: false,
		},
		{
			name:      "global on, table opted out",
			global:    &config.CacheConfig{Enabled: true, TTL: "1m", KeyPrefix: "models", Serializer: config.SerializerJSON, Hydration: &config.HydrationConfig{}},
			users:     &config.TableCacheConfig{Enabled: new(false)},
			wantUsers: nil,
			wantPosts: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Cache = tt.global
			cfg.Tables["users"] = config.TableConfig{Cache: tt.users}
			doc, err := manifest.Build(testBuildInput(t, ffmTwoTableSchema(), cfg))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if diff := cmp.Diff(tt.wantUsers, findEntity(t, doc, "User").Features.Cache); diff != "" {
				t.Errorf("User features.cache mismatch (-want +got):\n%s", diff)
			}
			if got := findEntity(t, doc, "Post").Features.Cache != nil; got != tt.wantPosts {
				t.Errorf("Post features.cache populated = %v, want %v (no per-table override; inherits global)", got, tt.wantPosts)
			}
		})
	}
}
