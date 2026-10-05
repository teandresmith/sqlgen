package config_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// sortRel is an O2M edge into documentsSchema()'s child table carrying the
// given static sort (PRD §4.8).
func sortRel(relType string, sort ...config.RelationshipSort) config.TableRelationship {
	return config.TableRelationship{
		Name: "Attachments", Type: relType, Table: "documents", FK: "entity_id",
		Sort: sort,
	}
}

// m2mSortRel is sortRel's m2m form: an m2m edge links through a junction,
// not an fk (PRD §4.8), so it carries one and no fk.
func m2mSortRel(sort ...config.RelationshipSort) config.TableRelationship {
	r := sortRel("m2m", sort...)
	r.FK, r.Junction = "", "asset_documents"
	return r
}

// TestValidateRelationships_SortRejected pins the pre-parse `sort:` rules.
// An o2o edge is LEFT JOINed into the parent query and has no loader query to
// order (PRD §13.2), so its `sort:` would be dropped; an empty column or an
// unknown direction would otherwise surface only as a runtime SQL error.
func TestValidateRelationships_SortRejected(t *testing.T) {
	tests := []struct {
		name string
		rel  config.TableRelationship
		want []string
	}{
		{
			name: "o2o",
			rel:  sortRel("o2o", config.RelationshipSort{Column: "name"}),
			want: []string{"tables.assets.relationships[0]", "Attachments", ".sort:", "o2o", "LEFT JOIN"},
		},
		{
			name: "one_to_one synonym",
			rel:  sortRel("one_to_one", config.RelationshipSort{Column: "name"}),
			want: []string{"tables.assets.relationships[0]", ".sort:", "o2o"},
		},
		{
			name: "missing column",
			rel:  sortRel("o2m", config.RelationshipSort{Direction: "desc"}),
			want: []string{"tables.assets.relationships[0]", ".sort[0].column: required"},
		},
		{
			name: "unknown direction",
			rel:  m2mSortRel(config.RelationshipSort{Column: "name"}, config.RelationshipSort{Column: "id", Direction: "descending"}),
			want: []string{"tables.assets.relationships[0]", ".sort[1].direction", `"descending"`, "allowed: asc, desc"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{tt.rel}}

			_, err := config.ValidatePreParse(cfg)
			assertErrContains(t, err, "ValidatePreParse()", tt.want...)
		})
	}
}

// TestValidateRelationships_SortAccepted is the other half: o2m and m2m take
// a static sort, with or without a direction, in either case.
func TestValidateRelationships_SortAccepted(t *testing.T) {
	tests := []struct {
		name string
		rel  config.TableRelationship
	}{
		{"o2m, no direction", sortRel("o2m", config.RelationshipSort{Column: "name"})},
		{"o2m, desc", sortRel("one_to_many", config.RelationshipSort{Column: "name", Direction: "desc"})},
		{"m2m, upper case", m2mSortRel(config.RelationshipSort{Column: "name", Direction: "ASC"}, config.RelationshipSort{Column: "id", Direction: "DESC"})},
		{"o2o without sort", sortRel("o2o")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{tt.rel}}

			if _, err := config.ValidatePreParse(cfg); err != nil {
				t.Errorf("ValidatePreParse() = %v, want nil", err)
			}
			if _, err := config.ValidatePostParse(cfg, documentsSchema(), nil); err != nil {
				t.Errorf("ValidatePostParse() = %v, want nil", err)
			}
		})
	}
}

// TestValidateRelationshipSortColumns_ColumnMustExist pins the schema-level
// rule: the loader hands the sort to the related table's query, so a column
// that does not exist there would fail every load at runtime. A target absent
// from the parsed schema is left to relationship resolution.
func TestValidateRelationshipSortColumns_ColumnMustExist(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{
		sortRel("o2m", config.RelationshipSort{Column: "name"}, config.RelationshipSort{Column: "created_at"}),
	}}

	_, err := config.ValidatePostParse(cfg, documentsSchema(), nil)
	assertErrContains(t, err, "ValidatePostParse()",
		"tables.assets.relationships[0]", "Attachments", ".sort[1].column", `"created_at"`, `related table "documents"`)

	ghost := sortRel("o2m", config.RelationshipSort{Column: "created_at"})
	ghost.Table = "ghosts"
	cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{ghost}}
	if _, err := config.ValidatePostParse(cfg, documentsSchema(), nil); err != nil {
		t.Errorf("ValidatePostParse() with an unparsed target = %v, want nil", err)
	}
}
