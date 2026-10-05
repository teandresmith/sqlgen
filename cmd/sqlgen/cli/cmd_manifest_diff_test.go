package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/manifest"
)

// testManifest mirrors the on-disk single-layout document but keeps Entities in
// its full inline form so a marshaled fixture carries every entity field the
// diff engine reads.
type testManifest struct {
	Schema           string                    `json:"$schema"`
	SchemaVersion    string                    `json:"schema_version"`
	GeneratedAt      string                    `json:"generated_at"`
	Generator        manifest.Generator        `json:"generator"`
	Dialect          string                    `json:"dialect"`
	Package          string                    `json:"package"`
	Layout           string                    `json:"layout"`
	Conventions      manifest.Conventions      `json:"conventions"`
	GenerationConfig manifest.GenerationConfig `json:"generation_config"`
	Entities         []manifest.Entity         `json:"entities"`
	Enums            []manifest.Enum           `json:"enums"`
	Extras           []manifest.Extra          `json:"extras"`
}

// baseManifest builds a minimal but complete single-layout manifest with one
// "users" entity, used as the shared starting point for diff mutations.
func baseManifest() *testManifest {
	return &testManifest{
		SchemaVersion: "0.1.0",
		GeneratedAt:   "2026-05-15T14:22:11Z",
		Generator:     manifest.Generator{Name: "sqlgen", Version: "0.42.0"},
		Dialect:       "postgres",
		Package:       "models",
		Layout:        "single",
		Conventions: manifest.Conventions{
			ErrorSentinels: []manifest.ErrorSentinel{
				{Name: "ErrNotFound", GraphQLCode: "NOT_FOUND", Package: "models"},
				{Name: "ErrConstraintViolation", Package: "models"},
			},
		},
		GenerationConfig: manifest.GenerationConfig{Cache: true},
		Entities: []manifest.Entity{
			{
				Kind:       manifest.EntityKindTable,
				Name:       "User",
				Table:      "users",
				FilePrefix: "users",
				Files:      []string{"users_gen.go"},
				PK:         manifest.PK{Kind: "single"},
				Features:   manifest.Features{Cache: &manifest.CacheFeature{TTLSeconds: 30, Hydration: "lazy", KeyPattern: "models:user:{id}"}},
				Columns: []manifest.Column{
					{Name: "id", GoField: "ID", GoType: "uuid.UUID", DBType: "uuid", PK: true, Unique: true},
					{Name: "email", GoField: "Email", GoType: "string", DBType: "text"},
				},
				Methods: manifest.Methods{
					Query: []manifest.Method{
						{Name: "FindByID", Params: []manifest.MethodParam{{Name: "id", Type: "uuid.UUID"}}, Returns: "*User"},
					},
					Mutation: []manifest.Method{
						{Name: "Create", Params: []manifest.MethodParam{{Name: "input", Type: "UserCreateInput"}}, Returns: "*User"},
					},
				},
			},
		},
		Enums:  []manifest.Enum{},
		Extras: []manifest.Extra{},
	}
}

// cloneManifest deep-copies m via a JSON round-trip so a mutation to the copy
// never leaks into the base.
func cloneManifest(t *testing.T, m *testManifest) *testManifest {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal for clone: %v", err)
	}
	var c testManifest
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("unmarshal for clone: %v", err)
	}
	return &c
}

// writeManifest marshals m to <dir>/manifest_gen.json and returns the path.
func writeManifest(t *testing.T, dir string, m *testManifest) string {
	t.Helper()
	path := filepath.Join(dir, "manifest_gen.json")
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

// diffPair writes old and new manifests into separate temp dirs and runs
// `manifest diff`, returning stdout and the exit code.
func diffPair(t *testing.T, oldM, newM *testManifest, args ...string) (string, int) {
	t.Helper()
	oldPath := writeManifest(t, t.TempDir(), oldM)
	newPath := writeManifest(t, t.TempDir(), newM)
	full := append([]string{"manifest", "diff", oldPath, newPath}, args...)
	stdout, stderr, err := executeCommand(full...)
	code := exitCodeOf(t, err)
	if code == manifestExitIOFail {
		t.Fatalf("unexpected I/O failure\nstderr: %s", stderr)
	}
	return stdout, code
}

func TestManifestDiff_SameFile(t *testing.T) {
	path := writeManifest(t, t.TempDir(), baseManifest())
	stdout, _, err := executeCommand("manifest", "diff", path, path)
	if code := exitCodeOf(t, err); code != manifestExitClean {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("expected no output for identical manifests, got: %q", stdout)
	}
}

func TestManifestDiff_ColumnAdded(t *testing.T) {
	oldM := baseManifest()
	newM := cloneManifest(t, oldM)
	newM.Entities[0].Columns = append(newM.Entities[0].Columns,
		manifest.Column{Name: "phone", GoField: "Phone", GoType: "string", DBType: "text"})

	stdout, code := diffPair(t, oldM, newM)
	if code != manifestExitFail {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "+ phone") {
		t.Errorf("diff missing added column\ngot: %s", stdout)
	}
}

func TestManifestDiff_ColumnRemoved(t *testing.T) {
	oldM := baseManifest()
	newM := cloneManifest(t, oldM)
	newM.Entities[0].Columns = newM.Entities[0].Columns[:1] // drop "email"

	stdout, code := diffPair(t, oldM, newM)
	if code != manifestExitFail {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "- email") {
		t.Errorf("diff missing removed column\ngot: %s", stdout)
	}
}

func TestManifestDiff_ColumnTypeChanged(t *testing.T) {
	oldM := baseManifest()
	oldM.Entities[0].Columns = append(oldM.Entities[0].Columns,
		manifest.Column{Name: "age", GoField: "Age", GoType: "int32", DBType: "int"})
	newM := cloneManifest(t, oldM)
	newM.Entities[0].Columns[2].GoType = "int64"
	newM.Entities[0].Columns[2].DBType = "bigint"

	stdout, code := diffPair(t, oldM, newM)
	if code != manifestExitFail {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "~ age") || !strings.Contains(stdout, "bigint") {
		t.Errorf("diff missing column type change\ngot: %s", stdout)
	}
}

func TestManifestDiff_EntityAddedRemoved(t *testing.T) {
	oldM := baseManifest()
	newM := cloneManifest(t, oldM)
	newM.Entities = append(newM.Entities, manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "Post", Table: "posts",
		FilePrefix: "posts", Files: []string{"posts_gen.go"}, PK: manifest.PK{Kind: "single"},
	})
	// And remove users from new to exercise removal too.
	removedM := cloneManifest(t, oldM)
	removedM.Entities = nil

	stdout, code := diffPair(t, oldM, newM)
	if code != manifestExitFail || !strings.Contains(stdout, "+ posts") {
		t.Errorf("entity-added diff wrong: code=%d\n%s", code, stdout)
	}

	stdout, code = diffPair(t, oldM, removedM)
	if code != manifestExitFail || !strings.Contains(stdout, "- users") {
		t.Errorf("entity-removed diff wrong: code=%d\n%s", code, stdout)
	}
}

func TestManifestDiff_MethodSignatureChanged(t *testing.T) {
	oldM := baseManifest()
	newM := cloneManifest(t, oldM)
	// FindByID gains a CallOption param — a signature change.
	newM.Entities[0].Methods.Query[0].Params = append(
		newM.Entities[0].Methods.Query[0].Params,
		manifest.MethodParam{Name: "opts", Type: "...CallOption"},
	)

	stdout, code := diffPair(t, oldM, newM)
	if code != manifestExitFail {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "~ FindByID") || !strings.Contains(stdout, "CallOption") {
		t.Errorf("diff missing method signature change\ngot: %s", stdout)
	}
}

func TestManifestDiff_ColumnConstraintFlipped(t *testing.T) {
	// A column flipping UNIQUE / PK / default with an unchanged Go/DB type must
	// still surface as a `~ <col>` change (columnSig was once type-only).
	tests := []struct {
		name   string
		mutate func(c *manifest.Column)
		want   string
	}{
		{"unique", func(c *manifest.Column) { c.Unique = true }, "unique"},
		{"pk", func(c *manifest.Column) { c.PK = true }, "pk"},
		{"default", func(c *manifest.Column) { c.Default = "'active'" }, "default='active'"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oldM := baseManifest()
			newM := cloneManifest(t, oldM)
			tc.mutate(&newM.Entities[0].Columns[1]) // email column, type unchanged

			stdout, code := diffPair(t, oldM, newM)
			if code != manifestExitFail {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stdout, "~ email") || !strings.Contains(stdout, tc.want) {
				t.Errorf("diff missing constraint flip %q\ngot: %s", tc.want, stdout)
			}
		})
	}
}

// TestManifestDiff_ColumnAccessChanged pins the PRD §32.3 review-gate signal:
// a column differing only in its access classification reports as changed —
// both when classified from the (omitted) public default and when the marker
// flips between roles.
func TestManifestDiff_ColumnAccessChanged(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *manifest.Column)
		want   string
	}{
		{"public to internal", func(c *manifest.Column) { c.Access = "internal"; c.Redacted = true }, "access=internal"},
		{"public to read_only", func(c *manifest.Column) { c.Access = "read_only" }, "access=read_only"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oldM := baseManifest()
			newM := cloneManifest(t, oldM)
			tc.mutate(&newM.Entities[0].Columns[1]) // email column, type unchanged

			stdout, code := diffPair(t, oldM, newM)
			if code != manifestExitFail {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stdout, "~ email") || !strings.Contains(stdout, tc.want) {
				t.Errorf("diff missing access change %q\ngot: %s", tc.want, stdout)
			}
		})
	}
}

func TestManifestDiff_MethodErrorSetChanged(t *testing.T) {
	// A method gaining an error sentinel (params + returns unchanged) must
	// surface as a `~ <method>` change (methodSig once omitted errors[]).
	oldM := baseManifest()
	oldM.Entities[0].Methods.Query[0].Errors = []string{"ErrNotFound"}
	newM := cloneManifest(t, oldM)
	newM.Entities[0].Methods.Query[0].Errors = []string{"ErrNotFound", "tenancy.ErrMissing"}

	stdout, code := diffPair(t, oldM, newM)
	if code != manifestExitFail {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "~ FindByID") || !strings.Contains(stdout, "tenancy.ErrMissing") {
		t.Errorf("diff missing method error-set change\ngot: %s", stdout)
	}
}

func TestManifestDiff_MethodErrorReorderNotChanged(t *testing.T) {
	// Reordering errors[] with no set change must NOT report a diff (methodSig
	// sorts errors before comparing).
	oldM := baseManifest()
	oldM.Entities[0].Methods.Query[0].Errors = []string{"ErrNotFound", "tenancy.ErrMissing"}
	newM := cloneManifest(t, oldM)
	newM.Entities[0].Methods.Query[0].Errors = []string{"tenancy.ErrMissing", "ErrNotFound"}

	_, code := diffPair(t, oldM, newM)
	if code != manifestExitClean {
		t.Fatalf("exit code = %d, want 0 (reorder is not a change)", code)
	}
}

func TestManifestDiff_SentinelChanged(t *testing.T) {
	oldM := baseManifest()
	newM := cloneManifest(t, oldM)
	newM.Conventions.ErrorSentinels = append(newM.Conventions.ErrorSentinels,
		manifest.ErrorSentinel{Name: "ErrMismatch", GraphQLCode: "FORBIDDEN", Package: "tenancy"})

	stdout, code := diffPair(t, oldM, newM)
	if code != manifestExitFail {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "+ ErrMismatch") {
		t.Errorf("diff missing sentinel change\ngot: %s", stdout)
	}
}

func TestManifestDiff_FeatureToggleChanged(t *testing.T) {
	oldM := baseManifest()
	newM := cloneManifest(t, oldM)
	newM.Entities[0].Features.Cache.TTLSeconds = 600

	stdout, code := diffPair(t, oldM, newM)
	if code != manifestExitFail {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "cache.ttl_seconds") || !strings.Contains(stdout, "600") {
		t.Errorf("diff missing feature change\ngot: %s", stdout)
	}
}

func TestManifestDiff_GenerationConfigChanged(t *testing.T) {
	oldM := baseManifest()
	newM := cloneManifest(t, oldM)
	newM.GenerationConfig.Tenancy = true

	stdout, code := diffPair(t, oldM, newM)
	if code != manifestExitFail {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "tenancy") || !strings.Contains(stdout, "false -> true") {
		t.Errorf("diff missing generation_config change\ngot: %s", stdout)
	}
}

func TestManifestDiff_JSONShape(t *testing.T) {
	oldM := baseManifest()
	newM := cloneManifest(t, oldM)
	newM.Entities[0].Columns = append(newM.Entities[0].Columns,
		manifest.Column{Name: "phone", GoField: "Phone", GoType: "string", DBType: "text"})
	newM.GenerationConfig.Tenancy = true

	stdout, code := diffPair(t, oldM, newM, "--json")
	if code != manifestExitFail {
		t.Fatalf("exit code = %d, want 1", code)
	}

	var got manifestDiff
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("--json output is not valid JSON matching manifestDiff: %v\n%s", err, stdout)
	}
	if len(got.Entities.Changed) != 1 || got.Entities.Changed[0].Table != "users" {
		t.Errorf("expected one changed entity 'users', got %+v", got.Entities.Changed)
	}
	if len(got.Entities.Changed) == 1 {
		if want := []string{"phone"}; !slices.Equal(got.Entities.Changed[0].Columns.Added, want) {
			t.Errorf("columns.added = %v, want %v", got.Entities.Changed[0].Columns.Added, want)
		}
	}
	if len(got.GenerationConfig) != 1 || got.GenerationConfig[0].Key != "tenancy" {
		t.Errorf("generation_config change = %+v, want tenancy", got.GenerationConfig)
	}
}

func TestManifestDiff_PerEntityLayout(t *testing.T) {
	oldPath := writePerEntity(t, t.TempDir(), baseManifest())

	newM := cloneManifest(t, baseManifest())
	newM.Layout = "per_entity"
	newM.Entities[0].Columns = append(newM.Entities[0].Columns,
		manifest.Column{Name: "phone", GoField: "Phone", GoType: "string", DBType: "text"})
	newPath := writePerEntity(t, t.TempDir(), newM)

	stdout, _, err := executeCommand("manifest", "diff", oldPath, newPath)
	if code := exitCodeOf(t, err); code != manifestExitFail {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "+ phone") {
		t.Errorf("per-entity diff did not resolve entity files\ngot: %s", stdout)
	}
}

func TestManifestDiff_PerEntityTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	// Hand-write a per_entity top index whose file pointer escapes the manifest
	// directory — the loader must refuse to follow it.
	top := filepath.Join(dir, "manifest_gen.json")
	index := `{
  "schema_version": "0.1.0",
  "layout": "per_entity",
  "conventions": {},
  "generation_config": {},
  "entities": [{"name":"User","table":"users","kind":"table","file":"../../etc/passwd"}],
  "enums": [],
  "extras": []
}`
	if err := os.WriteFile(top, []byte(index), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}

	_, stderr, err := executeCommand("manifest", "diff", top, top)
	if code := exitCodeOf(t, err); code != manifestExitIOFail {
		t.Fatalf("exit code = %d, want 2 (traversal rejected)\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "escapes the manifest directory") {
		t.Errorf("stderr missing traversal rejection: %q", stderr)
	}
}

// writePerEntity emits m in per_entity layout: a top-level index at
// manifest_gen.json plus one full file per entity under entities/, mirroring the
// emitter's on-disk shape so the diff loader must follow the file pointers.
func writePerEntity(t *testing.T, dir string, m *testManifest) string {
	t.Helper()
	type indexEntry struct {
		Name  string `json:"name"`
		Table string `json:"table"`
		Kind  string `json:"kind"`
		File  string `json:"file"`
	}
	index := struct {
		Schema           string                    `json:"$schema"`
		SchemaVersion    string                    `json:"schema_version"`
		GeneratedAt      string                    `json:"generated_at"`
		Generator        manifest.Generator        `json:"generator"`
		Dialect          string                    `json:"dialect"`
		Package          string                    `json:"package"`
		Layout           string                    `json:"layout"`
		Conventions      manifest.Conventions      `json:"conventions"`
		GenerationConfig manifest.GenerationConfig `json:"generation_config"`
		Entities         []indexEntry              `json:"entities"`
		Enums            []manifest.Enum           `json:"enums"`
		Extras           []manifest.Extra          `json:"extras"`
	}{
		Schema: m.Schema, SchemaVersion: m.SchemaVersion, GeneratedAt: m.GeneratedAt,
		Generator: m.Generator, Dialect: m.Dialect, Package: m.Package, Layout: "per_entity",
		Conventions: m.Conventions, GenerationConfig: m.GenerationConfig,
		Enums: m.Enums, Extras: m.Extras,
	}
	for i := range m.Entities {
		e := &m.Entities[i]
		index.Entities = append(index.Entities, indexEntry{
			Name: e.Name, Table: e.Table, Kind: string(e.Kind),
			File: "entities/" + e.FilePrefix + ".json",
		})
		body, err := json.MarshalIndent(e, "", "  ")
		if err != nil {
			t.Fatalf("marshal entity: %v", err)
		}
		entDir := filepath.Join(dir, "entities")
		if err := os.MkdirAll(entDir, 0o750); err != nil {
			t.Fatalf("mkdir entities: %v", err)
		}
		if err := os.WriteFile(filepath.Join(entDir, e.FilePrefix+".json"), body, 0o600); err != nil {
			t.Fatalf("write entity file: %v", err)
		}
	}
	top := filepath.Join(dir, "manifest_gen.json")
	b, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	if err := os.WriteFile(top, b, 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	return top
}
