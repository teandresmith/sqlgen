package mcp

import (
	"encoding/json"
	"testing"

	"github.com/teandresmith/sqlgen/manifest"
)

// TestDispatch_Deterministic invokes each tool against two independently-built
// stores and asserts byte-identical JSON output (MCP.md §6.5). Using two stores
// — whose internal maps may iterate in different orders — catches any tool that
// leaks map-iteration order into its response.
func TestDispatch_Deterministic(t *testing.T) {
	// invoke marshals a tool's output from a fresh fixture store.
	run := func(t *testing.T, fn func(*toolDeps) (any, error)) {
		t.Helper()
		marshal := func() []byte {
			out, err := fn(fixtureDeps())
			if err != nil {
				t.Fatalf("tool returned error: %v", err)
			}
			b, err := json.Marshal(out)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			return b
		}
		a, b := marshal(), marshal()
		if string(a) != string(b) {
			t.Errorf("non-deterministic output:\n a=%s\n b=%s", a, b)
		}
	}

	tests := []struct {
		name string
		fn   func(*toolDeps) (any, error)
	}{
		{"list_entities", func(d *toolDeps) (any, error) { return listEntities(d, ListEntitiesInput{}) }},
		{"get_entity", func(d *toolDeps) (any, error) { return getEntity(d, GetEntityInput{Name: "User"}) }},
		{"get_entity_compact", func(d *toolDeps) (any, error) { return getEntity(d, GetEntityInput{Name: "User", Compact: true}) }},
		{"find_method", func(d *toolDeps) (any, error) { return findMethod(d, FindMethodInput{Query: "Get"}) }},
		{"find_referencing", func(d *toolDeps) (any, error) { return findReferencing(d, FindReferencingInput{Table: "users"}) }},
		{"describe_relationship", func(d *toolDeps) (any, error) {
			return describeRelationship(d, DescribeRelationshipInput{Entity: "User", Relationship: "roles"})
		}},
		{"find_join_path", func(d *toolDeps) (any, error) { return findJoinPath(d, FindJoinPathInput{From: "User", To: "Comment"}) }},
		{"show_sql", func(d *toolDeps) (any, error) { return showSQL(d, ShowSQLInput{Entity: "User", Method: "Get"}) }},
		{"get_conventions", func(d *toolDeps) (any, error) { return getConventions(d, GetConventionsInput{}) }},
		{"get_example", func(d *toolDeps) (any, error) { return getExample(d, GetExampleInput{Entity: "User"}) }},
		{"health", func(d *toolDeps) (any, error) { return health(d, HealthInput{}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { run(t, tt.fn) })
	}

	// validate_manifest needs a schema-valid Raw(); drive it from the real fixture.
	t.Run("validate_manifest", func(t *testing.T) {
		marshal := func() []byte {
			out, err := validateManifest(&toolDeps{store: loadRealFixture(t)}, ValidateManifestInput{})
			if err != nil {
				t.Fatalf("validateManifest: %v", err)
			}
			b, _ := json.Marshal(out)
			return b
		}
		a, b := marshal(), marshal()
		if string(a) != string(b) {
			t.Errorf("validate_manifest output not deterministic:\n a=%s\n b=%s", a, b)
		}
	})
}

// TestRegisterToolsInstallsElevenTools confirms all 11 tools register with the
// SDK without a schema-inference panic and that a reload re-registers them.
func TestRegisterToolsInstallsEleven(t *testing.T) {
	srv := New("9.9.9", nil)
	srv.store = newFixtureStore()
	srv.watchEnabled = true
	// Must not panic on schema inference for any tool input struct.
	srv.registerTools()
	// A reload re-registers the same set (fires tools/list_changed).
	srv.onManifestReload()
}

// TestMethodSignature_ReturnShapes pins all three return shapes the generated
// surface carries. Two of them were rendered wrong: a void-return method
// (Increment, Refresh, the hard deletes) dropped its `error` entirely and read
// as returning nothing, and Stream — whose iterator carries its own error —
// would have gained a second, non-existent error return. Both became reachable
// on every entity when methods[] was corrected to the real surface.
func TestMethodSignature_ReturnShapes(t *testing.T) {
	tests := []struct {
		name   string
		method manifest.Method
		want   string
	}{
		{
			name: "value plus error",
			method: manifest.Method{
				Name:    "Get",
				Params:  []manifest.MethodParam{{Name: "id", Type: "uuid.UUID"}},
				Returns: "*User",
			},
			want: "Get(ctx context.Context, id uuid.UUID) (*User, error)",
		},
		{
			name: "error alone",
			method: manifest.Method{
				Name:    "HardDelete",
				Params:  []manifest.MethodParam{{Name: "id", Type: "uuid.UUID"}},
				Returns: "",
			},
			want: "HardDelete(ctx context.Context, id uuid.UUID) error",
		},
		{
			name: "increment carries a generic input and returns error alone",
			method: manifest.Method{
				Name: "Increment",
				Params: []manifest.MethodParam{
					{Name: "id", Type: "uuid.UUID"},
					{Name: "input", Type: "IncrementInput[UserIncrementColumn]"},
				},
				Returns: "",
			},
			want: "Increment(ctx context.Context, id uuid.UUID, input IncrementInput[UserIncrementColumn]) error",
		},
		{
			name: "iterator carries its own error",
			method: manifest.Method{
				Name:    "Stream",
				Params:  []manifest.MethodParam{{Name: "input", Type: "*StreamUsersInput"}},
				Returns: "iter.Seq2[*User, error]",
			},
			want: "Stream(ctx context.Context, input *StreamUsersInput) iter.Seq2[*User, error]",
		},
		{
			name:   "no params",
			method: manifest.Method{Name: "Refresh", Params: []manifest.MethodParam{}, Returns: ""},
			want:   "Refresh(ctx context.Context) error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := methodSignature(tt.method); got != tt.want {
				t.Errorf("methodSignature() = %q, want %q", got, tt.want)
			}
		})
	}
}
