package config_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// fxWithExtra returns validConfig() carrying one extras entry named typeName
// with a single string field named field.
func fxWithExtra(typeName, field string) *config.RootConfig {
	cfg := validConfig()
	cfg.Extras = map[string]config.ExtraType{
		typeName: {Fields: map[string]config.ExtraTypeField{field: {Type: "string"}}},
	}
	return cfg
}

// TestValidatePreParse_ExtrasNamesAreExportedIdentifiers pins PRD §4.10's
// naming of an extras entry: its key is the struct's Go name and each fields
// key a field's, used as written, so each must be an exported Go identifier.
func TestValidatePreParse_ExtrasNamesAreExportedIdentifiers(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		field    string
		wantErr  []string
	}{
		{name: "exported names accepted", typeName: "PostalAddress", field: "Zip"},
		{name: "acronym spelling accepted", typeName: "IPRange", field: "CIDRs"},
		{name: "capitalized keyword accepted", typeName: "Type", field: "Range"},
		{
			name:     "lower-case key rejected",
			typeName: "metadata",
			field:    "Key",
			wantErr:  []string{`extras.metadata: "metadata" is not an exported Go identifier — the key is the generated struct's name, used as written (PRD §4.10)`},
		},
		{
			name:     "snake-case key rejected",
			typeName: "postal_address",
			field:    "Zip",
			wantErr:  []string{`extras.postal_address: "postal_address" is not an exported Go identifier`},
		},
		{
			name:     "dotted key rejected",
			typeName: "models.Address",
			field:    "Zip",
			wantErr:  []string{`extras.models.Address: "models.Address" is not an exported Go identifier`},
		},
		{
			name:     "lower-case field rejected",
			typeName: "PostalAddress",
			field:    "zip",
			wantErr:  []string{`extras.PostalAddress.fields.zip: "zip" is not an exported Go identifier — the key is the generated field's name, used as written (PRD §4.10)`},
		},
		{
			name:     "snake-case field rejected",
			typeName: "PostalAddress",
			field:    "actor_id",
			wantErr:  []string{`extras.PostalAddress.fields.actor_id: "actor_id" is not an exported Go identifier`},
		},
		{
			name:     "digit-leading field rejected",
			typeName: "PostalAddress",
			field:    "2ndLine",
			wantErr:  []string{`extras.PostalAddress.fields.2ndLine: "2ndLine" is not an exported Go identifier`},
		},
		{
			name:     "underscore-leading field rejected",
			typeName: "PostalAddress",
			field:    "_Zip",
			wantErr:  []string{`extras.PostalAddress.fields._Zip: "_Zip" is not an exported Go identifier`},
		},
		{
			name:     "bad key and bad field both reported",
			typeName: "address",
			field:    "zip",
			wantErr: []string{
				`extras.address: "address" is not an exported Go identifier`,
				`extras.address.fields.zip: "zip" is not an exported Go identifier`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.ValidatePreParse(fxWithExtra(tt.typeName, tt.field))
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Errorf("ValidatePreParse() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePreParse() error = nil, want %q", tt.wantErr)
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ValidatePreParse() error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}
