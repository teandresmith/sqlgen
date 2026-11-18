package manifest_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/parser"
)

// TestBuild_CacheInvalidatesOnNamesEntityMutations pins PRD §30.4.2 / §30.7:
// a table's features.cache.invalidates_on "lists method names per PRD §27" and
// names "methods on the entity itself". Every listed name must therefore be a
// method the same entity advertises under methods.mutation[] — no method the
// templates never emit (Delete), and no SoftDelete / Restore / Increment on a
// table that does not generate them. The list is exactly the entity's
// mutations whose §27.7 cache arm invalidates, in methods.mutation[] order:
// Create / CreateMany set the cache rather than invalidate it and are absent.
func TestBuild_CacheInvalidatesOnNamesEntityMutations(t *testing.T) {
	tests := []struct {
		name  string
		table parser.Table
		want  []string
	}{
		{
			name: "plain table without soft delete or incrementable column",
			table: parser.Table{Name: "users", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "email", Type: "text"},
			}},
			want: []string{"Update", "UpdateMany", "UpdateWhere", "Upsert", "UpsertMany", "HardDelete", "HardDeleteMany", "HardDeleteWhere"},
		},
		{
			name: "soft-delete table with an integer column",
			table: parser.Table{Name: "users", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "email", Type: "text"},
				{Name: "login_count", Type: "integer"},
				{Name: "deleted_at", Type: "timestamptz", Nullable: true},
			}},
			want: []string{
				"Update", "UpdateMany", "UpdateWhere", "Upsert", "UpsertMany", "Increment",
				"SoftDelete", "SoftDeleteMany", "SoftDeleteWhere", "Restore", "RestoreMany", "RestoreWhere",
				"HardDelete", "HardDeleteMany", "HardDeleteWhere",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Cache = &config.CacheConfig{Enabled: true, TTL: "30s", KeyPrefix: "models", Serializer: config.SerializerJSON, Hydration: &config.HydrationConfig{}}
			doc, err := manifest.Build(testBuildInput(t, &parser.Schema{Tables: []parser.Table{tt.table}}, cfg))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			e := findEntity(t, doc, "User")
			if e.Features.Cache == nil {
				t.Fatalf("features.cache = nil, want populated (cache enabled)")
			}
			mutations := make([]string, 0, len(e.Methods.Mutation))
			for _, m := range e.Methods.Mutation {
				mutations = append(mutations, m.Name)
			}
			for _, name := range e.Features.Cache.InvalidatesOn {
				if !slices.Contains(mutations, name) {
					t.Errorf("invalidates_on names %q, which is not a mutation method on User; mutations = %v", name, mutations)
				}
			}
			if diff := cmp.Diff(tt.want, e.Features.Cache.InvalidatesOn); diff != "" {
				t.Errorf("invalidates_on mismatch (-want +got):\n%s", diff)
			}
			if slices.Contains(e.Features.Cache.InvalidatesOn, "Create") {
				t.Errorf("invalidates_on = %v, names Create, which sets the cache rather than invalidating it (§27.7)", e.Features.Cache.InvalidatesOn)
			}
		})
	}
}
