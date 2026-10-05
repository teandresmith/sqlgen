package parser_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/parser"
)

func TestDetectM2MThreeColPKIncludingFKs(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "assets", Schema: "public", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{Name: "ppas", Schema: "public", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{
				Name:   "join_asset_ppa",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "asset_id", Type: "uuid", FKReference: &parser.FKReference{Table: "assets", Schema: "public", Column: "id"}},
					{Name: "ppa_id", Type: "uuid", FKReference: &parser.FKReference{Table: "ppas", Schema: "public", Column: "id"}},
					{Name: "created_datetime", Type: "timestamptz"},
				},
				Constraints: []parser.Constraint{
					{Name: "join_asset_ppa_pkey", Type: parser.PrimaryKey, Columns: []string{"asset_id", "ppa_id", "created_datetime"}},
				},
			},
		},
	}
	parser.DetectRelationships(schema)
	for _, r := range schema.Relationships {
		t.Logf("rel: type=%v src=%s tgt=%s junction=%s fk=%s", r.Type, r.SourceTable, r.TargetTable, r.JunctionTable, r.FKColumn)
	}
	hasM2M := false
	for _, r := range schema.Relationships {
		if r.Type == parser.ManyToMany {
			hasM2M = true
		}
	}
	if !hasM2M {
		t.Errorf("expected at least one M2M, got none")
	}
}
