package manifest_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/parser"
)

// accessTestSchema returns a users table with one column per access role.
func accessTestSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "email", Type: "text"},
					{Name: "password_hash", Type: "text"},
					{Name: "created_at", Type: "timestamptz", Default: "now()"},
					{Name: "new_password", Type: "text", Nullable: true},
					{Name: "internal_score", Type: "bigint", Nullable: true},
				},
			},
		},
	}
}

// accessTestConfig classifies one column per non-public role; email and id
// stay public (explicit and implicit).
func accessTestConfig() *config.RootConfig {
	cfg := baseConfig()
	cfg.Tables["users"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			"email":          {Access: "public"},
			"password_hash":  {Access: "internal"},
			"created_at":     {Access: "read_only"},
			"new_password":   {Access: "write_only"},
			"internal_score": {Access: "hidden"},
		},
	}
	return cfg
}

// TestBuild_ColumnAccessMarkers pins the PRD §32.3 manifest marking: each
// column carries `access` (omitted for public) and `redacted` (true only for
// write_only / internal).
func TestBuild_ColumnAccessMarkers(t *testing.T) {
	doc, err := manifest.Build(testBuildInput(t, accessTestSchema(), accessTestConfig()))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e := findEntity(t, doc, "User")

	want := map[string]struct {
		access   string
		redacted bool
	}{
		"id":             {access: "", redacted: false}, // public (unset) — omitted
		"email":          {access: "", redacted: false}, // public (explicit) — omitted
		"password_hash":  {access: "internal", redacted: true},
		"created_at":     {access: "read_only", redacted: false},
		"new_password":   {access: "write_only", redacted: true},
		"internal_score": {access: "hidden", redacted: false},
	}
	if len(e.Columns) != len(want) {
		t.Errorf("columns count = %d, want %d", len(e.Columns), len(want))
	}
	for _, c := range e.Columns {
		w, ok := want[c.Name]
		if !ok {
			t.Errorf("unexpected column %q", c.Name)
			continue
		}
		if c.Access != w.access {
			t.Errorf("column %q access = %q, want %q", c.Name, c.Access, w.access)
		}
		if c.Redacted != w.redacted {
			t.Errorf("column %q redacted = %v, want %v", c.Name, c.Redacted, w.redacted)
		}
	}
}

// TestEmitJSON_AccessMarkers_BothLayouts verifies the access fields render in
// the emitted JSON for both layouts, and that public columns omit them.
func TestEmitJSON_AccessMarkers_BothLayouts(t *testing.T) {
	doc, err := manifest.Build(testBuildInput(t, accessTestSchema(), accessTestConfig()))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	tests := []struct {
		name   string
		layout config.JSONLayout
		file   string
	}{
		{name: "single layout", layout: config.JSONLayoutSingle, file: filepath.Join("manifest", "manifest_gen.json")},
		{name: "per_entity layout", layout: config.JSONLayoutPerEntity, file: filepath.Join("manifest", "entities", "user.json")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc.Layout = string(tt.layout)
			dir := t.TempDir()
			if err := manifest.EmitJSON(doc, manifestCfg(tt.layout), dir); err != nil {
				t.Fatalf("EmitJSON: %v", err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, tt.file)) //nolint:gosec // test temp path
			if err != nil {
				t.Fatalf("read emitted manifest: %v", err)
			}
			got := string(raw)
			for _, wantSub := range []string{`"access": "internal"`, `"access": "read_only"`, `"access": "write_only"`, `"access": "hidden"`, `"redacted": true`} {
				if !strings.Contains(got, wantSub) {
					t.Errorf("emitted %s missing %s", tt.file, wantSub)
				}
			}
			if strings.Contains(got, `"access": "public"`) {
				t.Errorf("emitted %s carries explicit public access — public must be omitted", tt.file)
			}
		})
	}
}

// TestValidateAgainstSchema_AccessFields verifies schema/v1.json accepts a
// manifest with the access fields, still accepts the pre-access shape, and
// rejects an out-of-enum role.
func TestValidateAgainstSchema_AccessFields(t *testing.T) {
	doc, err := manifest.Build(testBuildInput(t, accessTestSchema(), accessTestConfig()))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	withAccess, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if !strings.Contains(string(withAccess), `"access":"internal"`) {
		t.Fatalf("fixture manifest missing access fields: %s", withAccess)
	}
	if err := manifest.ValidateAgainstSchema(withAccess); err != nil {
		t.Errorf("ValidateAgainstSchema(with access fields) = %v, want nil", err)
	}

	// Pre-access shape: a public-only build emits no access/redacted keys.
	publicDoc, err := manifest.Build(testBuildInput(t, accessTestSchema(), baseConfig()))
	if err != nil {
		t.Fatalf("Build (public-only): %v", err)
	}
	withoutAccess, err := json.Marshal(publicDoc)
	if err != nil {
		t.Fatalf("marshal public-only manifest: %v", err)
	}
	if strings.Contains(string(withoutAccess), `"access"`) {
		t.Fatalf("public-only manifest unexpectedly carries access fields")
	}
	if err := manifest.ValidateAgainstSchema(withoutAccess); err != nil {
		t.Errorf("ValidateAgainstSchema(without access fields) = %v, want nil", err)
	}

	// Out-of-enum role must be rejected.
	invalid := strings.Replace(string(withAccess), `"access":"internal"`, `"access":"confidential"`, 1)
	if err := manifest.ValidateAgainstSchema([]byte(invalid)); err == nil {
		t.Error("ValidateAgainstSchema(access=confidential) = nil, want enum violation")
	}
}
