package gotype_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// TestUUIDIntegrationFor pins PRD §7.4 "Generating UUID values" and "Parsing a
// UUID from a string" cell by cell.
//
// Every one of these spellings was wrong for two of the three libraries before
// this existed: the generator emitted google's `uuid.New()` / `uuid.NewString()`
// / `uuid.Must(uuid.NewV7())` whatever the package had resolved, which does not
// compile against the standard library (no Must, and NewV7 returns a bare UUID)
// and does not compile against gofrs (no New, no NewString). The compile pins
// in uuidgen_compile_*_test.go are the other half of this table: they are the
// same strings, spelled as code, against the real packages.
func TestUUIDIntegrationFor(t *testing.T) {
	tests := []struct {
		name       string
		importPath string
		want       gotype.UUIDIntegration
	}{
		{
			name:       "standard library",
			importPath: "uuid",
			want: gotype.UUIDIntegration{
				ImportPath: "uuid",
				V4:         "uuid.NewV4()",
				V7:         "uuid.NewV7()",
				V4String:   "uuid.NewV4().String()",
				V7String:   "uuid.NewV7().String()",
				ParseFunc:  "uuid.Parse",
			},
		},
		{
			name:       "google",
			importPath: "github.com/google/uuid",
			want: gotype.UUIDIntegration{
				ImportPath: "github.com/google/uuid",
				V4:         "uuid.New()",
				V7:         "uuid.Must(uuid.NewV7())",
				V4String:   "uuid.NewString()",
				V7String:   "uuid.Must(uuid.NewV7()).String()",
				ParseFunc:  "uuid.Parse",
			},
		},
		{
			name:       "gofrs",
			importPath: "github.com/gofrs/uuid/v5",
			want: gotype.UUIDIntegration{
				ImportPath: "github.com/gofrs/uuid/v5",
				V4:         "uuid.Must(uuid.NewV4())",
				V7:         "uuid.Must(uuid.NewV7())",
				V4String:   "uuid.Must(uuid.NewV4()).String()",
				V7String:   "uuid.Must(uuid.NewV7()).String()",
				// gofrs is the only one of the three with no Parse at all.
				ParseFunc: "uuid.FromString",
			},
		},
		{
			// A package with no uuid column selects nothing and falls back to
			// the standard library, so enabling events costs no module
			// dependency (PRD §7.4, §28.8).
			name:       "no integration selected",
			importPath: "",
			want: gotype.UUIDIntegration{
				ImportPath: "uuid",
				V4:         "uuid.NewV4()",
				V7:         "uuid.NewV7()",
				V4String:   "uuid.NewV4().String()",
				V7String:   "uuid.NewV7().String()",
				ParseFunc:  "uuid.Parse",
			},
		},
		{
			// A library sqlgen has no built-in knowledge of keeps its own
			// import — substituting the standard library's would put a second
			// package named uuid into the generated package, which is the
			// failure the one-library rule exists to prevent — and takes the
			// standard library's spellings, the only ones the PRD defines.
			name:       "an unknown library keeps its own import",
			importPath: "example.com/myorg/uuid",
			want: gotype.UUIDIntegration{
				ImportPath: "example.com/myorg/uuid",
				V4:         "uuid.NewV4()",
				V7:         "uuid.NewV7()",
				V4String:   "uuid.NewV4().String()",
				V7String:   "uuid.NewV7().String()",
				ParseFunc:  "uuid.Parse",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gotype.UUIDIntegrationFor(tt.importPath)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("UUIDIntegrationFor(%q) mismatch (-want +got):\n%s", tt.importPath, diff)
			}
		})
	}
}

// TestUUIDIntegrationExprSelection covers the two accessors across every
// version value the config can carry, including the empty one: an unset
// generation.uuid_version defaults to v4 (PRD §8.6), and the accessors have to
// agree with that or an unconfigured package silently generates v7.
func TestUUIDIntegrationExprSelection(t *testing.T) {
	google := gotype.UUIDIntegrationFor("github.com/google/uuid")

	tests := []struct {
		name       string
		version    config.UUIDVersion
		wantValue  string
		wantString string
	}{
		{"v4", config.UUIDVersionV4, "uuid.New()", "uuid.NewString()"},
		{"v7", config.UUIDVersionV7, "uuid.Must(uuid.NewV7())", "uuid.Must(uuid.NewV7()).String()"},
		{"unset defaults to v4", "", "uuid.New()", "uuid.NewString()"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := google.ValueExpr(tt.version); got != tt.wantValue {
				t.Errorf("ValueExpr(%q) = %q, want %q", tt.version, got, tt.wantValue)
			}
			if got := google.StringExpr(tt.version); got != tt.wantString {
				t.Errorf("StringExpr(%q) = %q, want %q", tt.version, got, tt.wantString)
			}
		})
	}
}

// TestUUIDIntegrationIn pins the config half of the selection cascade
// (PRD §7.4): which UUID library an `overrides.types` map names, for a package
// whose columns resolved none.
//
// Detection is by import path rather than by key, so an entry declaring a UUID
// library under a non-`uuid` SQL type counts — the `tenancy` example declares
// google under `blob`. Entries naming a non-UUID library, or no import at all,
// contribute nothing.
func TestUUIDIntegrationIn(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]config.TypeOverride
		want      string
	}{
		{
			name: "nil map names none",
			want: "",
		},
		{
			name:      "empty map names none",
			overrides: map[string]config.TypeOverride{},
			want:      "",
		},
		{
			name: "google under the uuid key",
			overrides: map[string]config.TypeOverride{
				"uuid": {Type: "uuid.UUID", Import: "github.com/google/uuid"},
			},
			want: "github.com/google/uuid",
		},
		{
			name: "google under a non-uuid key still counts",
			overrides: map[string]config.TypeOverride{
				"blob": {Type: "uuid.UUID", Import: "github.com/google/uuid"},
			},
			want: "github.com/google/uuid",
		},
		{
			name: "the standard library is a named binding, not an absence",
			overrides: map[string]config.TypeOverride{
				"uuid": {Type: "uuid.UUID", Import: "uuid"},
			},
			want: "uuid",
		},
		{
			name: "gofrs",
			overrides: map[string]config.TypeOverride{
				"uuid": {Type: "uuid.UUID", Import: "github.com/gofrs/uuid/v5"},
			},
			want: "github.com/gofrs/uuid/v5",
		},
		{
			name: "a non-UUID library names none",
			overrides: map[string]config.TypeOverride{
				"numeric": {Type: "decimal.Decimal", Import: "github.com/shopspring/decimal"},
			},
			want: "",
		},
		{
			name: "an entry with no import names none",
			overrides: map[string]config.TypeOverride{
				"text": {Type: "string"},
			},
			want: "",
		},
		{
			name: "two UUID libraries resolve to the minimum, deterministically",
			overrides: map[string]config.TypeOverride{
				"uuid": {Type: "uuid.UUID", Import: "github.com/google/uuid"},
				"blob": {Type: "uuid.UUID", Import: "github.com/gofrs/uuid/v5"},
			},
			want: "github.com/gofrs/uuid/v5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Run repeatedly: the implementation iterates a map, so a
			// first-seen bug would pass intermittently.
			for range 20 {
				if got := gotype.UUIDIntegrationIn(tt.overrides); got != tt.want {
					t.Fatalf("UUIDIntegrationIn(%v) = %q, want %q", tt.overrides, got, tt.want)
				}
			}
		})
	}
}
