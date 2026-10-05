package gotype

import (
	"maps"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/decimal"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgofrs"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgoogle"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidstd"
)

// scopeIntegrations is exercised white-box because the field it exists to
// back-fill — Nullable.UnderlyingField — is not observable from a resolved
// GoType. Every built-in wrapper it can name (uuid.NullUUID,
// decimal.NullDecimal) is also in knownWrapperExtractions, so
// DeriveScalarExtraction answers from the hard-coded table whether or not the
// back-fill ran. The parity the back-fill actually buys is asserted from the
// outside in TestIntegrationDetectionIsScopeIndependent; this file pins the
// intermediate the acceptance criterion names.
func TestScopeIntegrations_Enrichment(t *testing.T) {
	tests := []struct {
		name          string
		scope         map[string]config.TypeOverride
		sqlType       string
		wantDeclared  config.TypeOverride
		wantActivated map[string]config.TypeOverride
	}{
		{
			name: "google uuid back-fills zero value, wrapper and underlying field",
			scope: map[string]config.TypeOverride{
				"uuid": {Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
			},
			sqlType: "uuid",
			wantDeclared: config.TypeOverride{
				Type:      "uuid.UUID",
				Import:    uuidgoogle.ImportPath,
				ZeroValue: "uuid.UUID{}",
				Nullable: config.NullableVariant{
					Type:            "uuid.NullUUID",
					UnderlyingField: "UUID",
				},
			},
			wantActivated: map[string]config.TypeOverride{},
		},
		{
			name: "gofrs uuid back-fills its own zero-value spelling",
			scope: map[string]config.TypeOverride{
				"uuid": {Type: "uuid.UUID", Import: uuidgofrs.ImportPath},
			},
			sqlType: "uuid",
			wantDeclared: config.TypeOverride{
				Type:      "uuid.UUID",
				Import:    uuidgofrs.ImportPath,
				ZeroValue: "uuid.Nil",
				Nullable: config.NullableVariant{
					Type:            "uuid.NullUUID",
					UnderlyingField: "UUID",
				},
			},
			wantActivated: map[string]config.TypeOverride{},
		},
		{
			name: "stdlib uuid leaves Nullable empty — the package ships no wrapper",
			scope: map[string]config.TypeOverride{
				"uuid": {Type: "uuid.UUID", Import: uuidstd.ImportPath},
			},
			sqlType: "uuid",
			wantDeclared: config.TypeOverride{
				Type:      "uuid.UUID",
				Import:    uuidstd.ImportPath,
				ZeroValue: "uuid.UUID{}",
			},
			wantActivated: map[string]config.TypeOverride{},
		},
		{
			name: "an explicit nullable is kept, and only the missing extraction metadata is filled",
			scope: map[string]config.TypeOverride{
				"uuid": {
					Type:     "uuid.UUID",
					Import:   uuidgoogle.ImportPath,
					Nullable: config.NullableVariant{Type: "uuid.NullUUID"},
				},
			},
			sqlType: "uuid",
			wantDeclared: config.TypeOverride{
				Type:      "uuid.UUID",
				Import:    uuidgoogle.ImportPath,
				ZeroValue: "uuid.UUID{}",
				Nullable: config.NullableVariant{
					Type:            "uuid.NullUUID",
					UnderlyingField: "UUID",
				},
			},
			wantActivated: map[string]config.TypeOverride{},
		},
		{
			name: "an explicit pointer nullable is never replaced by the wrapper",
			scope: map[string]config.TypeOverride{
				"uuid": {
					Type:     "uuid.UUID",
					Import:   uuidgoogle.ImportPath,
					Nullable: config.NullableVariant{Type: "*uuid.UUID"},
				},
			},
			sqlType: "uuid",
			wantDeclared: config.TypeOverride{
				Type:      "uuid.UUID",
				Import:    uuidgoogle.ImportPath,
				ZeroValue: "uuid.UUID{}",
				Nullable:  config.NullableVariant{Type: "*uuid.UUID"},
			},
			wantActivated: map[string]config.TypeOverride{},
		},
		{
			name: "an explicit zero value is never replaced",
			scope: map[string]config.TypeOverride{
				"uuid": {Type: "uuid.UUID", Import: uuidgofrs.ImportPath, ZeroValue: "customZero"},
			},
			sqlType: "uuid",
			wantDeclared: config.TypeOverride{
				Type:      "uuid.UUID",
				Import:    uuidgofrs.ImportPath,
				ZeroValue: "customZero",
				Nullable: config.NullableVariant{
					Type:            "uuid.NullUUID",
					UnderlyingField: "UUID",
				},
			},
			wantActivated: map[string]config.TypeOverride{},
		},
		{
			name: "a declared numeric activates the integration's undeclared decimal",
			scope: map[string]config.TypeOverride{
				"numeric": {Type: "decimal.Decimal", Import: decimal.ImportPath},
			},
			sqlType: "numeric",
			wantDeclared: config.TypeOverride{
				Type:      "decimal.Decimal",
				Import:    decimal.ImportPath,
				ZeroValue: "decimal.Decimal{}",
				Nullable: config.NullableVariant{
					Type:            "decimal.NullDecimal",
					UnderlyingField: "Decimal",
				},
			},
			wantActivated: map[string]config.TypeOverride{"decimal": decimal.Override()},
		},
		{
			// An override naming no import is not a competing claim, so it
			// stays eligible for enrichment — that much is what this case
			// pins.
			//
			// The empty Import below stays empty: enrichment does not fill
			// Import (filling it would turn non-claims into claims and move
			// §4.13's one-library count), so gen rejects the config instead. validateTypeOverrideImports
			// refuses an `overrides.types` entry whose package-qualified type
			// carries no import, at this scope and the global one alike, so a
			// `decimal.Decimal` that nothing in the override itself imports
			// never reaches a generated file. This case therefore pins
			// resolver behavior that is now unreachable from a valid config —
			// keep it as the defensive record of what enrichment does and does
			// not touch.
			name: "an import-less entry rides along on a sibling's activation",
			scope: map[string]config.TypeOverride{
				"numeric": {Type: "decimal.Decimal"},
				"decimal": {Type: "decimal.Decimal", Import: decimal.ImportPath},
			},
			sqlType: "numeric",
			wantDeclared: config.TypeOverride{
				Type:      "decimal.Decimal",
				ZeroValue: "decimal.Decimal{}",
				Nullable: config.NullableVariant{
					Type:            "decimal.NullDecimal",
					UnderlyingField: "Decimal",
				},
			},
			wantActivated: map[string]config.TypeOverride{},
		},
		{
			name: "a second library in the same scope does not write its wrapper onto the first",
			scope: map[string]config.TypeOverride{
				"uuid": {Type: "uuid.UUID", Import: uuidstd.ImportPath},
				"text": {Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
			},
			sqlType: "uuid",
			wantDeclared: config.TypeOverride{
				Type:      "uuid.UUID",
				Import:    uuidstd.ImportPath,
				ZeroValue: "uuid.UUID{}",
			},
			wantActivated: map[string]config.TypeOverride{},
		},
		{
			name: "a scope naming no known library is handed back untouched",
			scope: map[string]config.TypeOverride{
				"text": {Type: "ksuid.KSUID", Import: "github.com/segmentio/ksuid"},
			},
			sqlType:       "text",
			wantDeclared:  config.TypeOverride{Type: "ksuid.KSUID", Import: "github.com/segmentio/ksuid"},
			wantActivated: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := maps.Clone(tt.scope)

			declared, activated := scopeIntegrations(tt.scope)

			if diff := cmp.Diff(tt.wantDeclared, declared[tt.sqlType]); diff != "" {
				t.Errorf("scopeIntegrations(...) declared[%q] (-want +got):\n%s", tt.sqlType, diff)
			}
			if diff := cmp.Diff(tt.wantActivated, activated); diff != "" {
				t.Errorf("scopeIntegrations(...) activated (-want +got):\n%s", diff)
			}
			// The scope map is the caller's parsed config, shared with every
			// other reader of it — enrichment must land in a clone.
			if diff := cmp.Diff(before, tt.scope); diff != "" {
				t.Errorf("scopeIntegrations(...) mutated the caller's map (-before +after):\n%s", diff)
			}
		})
	}
}

// A nil or empty scope is the overwhelmingly common case — most tables declare
// no overrides block at all — and must cost nothing.
func TestScopeIntegrations_EmptyScope(t *testing.T) {
	for _, scope := range []map[string]config.TypeOverride{nil, {}} {
		declared, activated := scopeIntegrations(scope)
		if len(declared) != 0 {
			t.Errorf("scopeIntegrations(%v) declared = %v, want empty", scope, declared)
		}
		if activated != nil {
			t.Errorf("scopeIntegrations(%v) activated = %v, want nil", scope, activated)
		}
	}
}

// detectIntegrationsIn is the single implementation both scopes call, which is
// what keeps them from drifting apart. Pin that it reads the map's contents
// and nothing else.
func TestDetectIntegrationsIn(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]config.TypeOverride
		want      []string
	}{
		{
			name:      "nil map",
			overrides: nil,
			want:      nil,
		},
		{
			name:      "no known import path",
			overrides: map[string]config.TypeOverride{"text": {Type: "ksuid.KSUID", Import: "github.com/segmentio/ksuid"}},
			want:      nil,
		},
		{
			name:      "one library",
			overrides: map[string]config.TypeOverride{"uuid": {Type: "uuid.UUID", Import: uuidgoogle.ImportPath}},
			want:      []string{uuidgoogle.ImportPath},
		},
		{
			name: "two libraries, both detected",
			overrides: map[string]config.TypeOverride{
				"uuid":    {Type: "uuid.UUID", Import: uuidstd.ImportPath},
				"numeric": {Type: "decimal.Decimal", Import: decimal.ImportPath},
			},
			want: []string{decimal.ImportPath, uuidstd.ImportPath},
		},
		{
			name: "detection keys on the import, not on the SQL type it sits under",
			overrides: map[string]config.TypeOverride{
				"text": {Type: "uuid.UUID", Import: uuidgofrs.ImportPath},
			},
			want: []string{uuidgofrs.ImportPath},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectIntegrationsIn(tt.overrides)

			var gotPaths []string
			if got != nil {
				gotPaths = slices.Sorted(maps.Keys(got))
			}
			if diff := cmp.Diff(tt.want, gotPaths); diff != "" {
				t.Errorf("detectIntegrationsIn(%v) import paths (-want +got):\n%s", tt.overrides, diff)
			}
		})
	}
}
