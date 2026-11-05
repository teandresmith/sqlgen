package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// TestResolvedNamesEntityClaims covers the entity-vs-entity claims PRD §8.5
// lists beyond the table-vs-table cases: a view claims its Go type name and its
// table-name constant, composite and domain types claim their Go type names
// (with no override of their own, so their escape is the schema),
// and an enum's collision offers `enums.<enum>.struct_name` as its escape.
func TestResolvedNamesEntityClaims(t *testing.T) {
	tests := []struct {
		name       string
		tables     []parser.Table
		views      []parser.View
		enums      []parser.Enum
		composites []parser.CompositeType
		domains    []parser.DomainType
		wantErr    []string
		wantNot    []string // must not appear: an escape key the entity does not have
	}{
		{
			name:    "a view and a table meeting on the Go type name",
			tables:  []parser.Table{namedTable("person", "")},
			views:   []parser.View{namedView("people", "")},
			wantErr: []string{`view people`, `table person`, `"Person"`},
		},
		{
			name:    "a view and a table meeting on the table-name constant",
			tables:  []parser.Table{namedTable("analyse", "")},
			views:   []parser.View{namedView("analyses", "")},
			wantErr: []string{`view analyses`, `table analyse`, `"TableAnalyses"`},
		},
		{
			name:   "a composite type and a table meeting on the Go type name",
			tables: []parser.Table{namedTable("orders", "")},
			composites: []parser.CompositeType{{
				Name: "order", Attributes: []parser.Attribute{{Name: "label", Type: "text"}},
			}},
			wantErr: []string{`composite type order`, `table orders`, `"Order"`},
			wantNot: []string{`order.struct_name`, `composite_types`},
		},
		{
			name:    "a domain type and a table meeting on the Go type name",
			tables:  []parser.Table{namedTable("orders", "")},
			domains: []parser.DomainType{{Name: "order", BaseType: "text"}},
			wantErr: []string{`domain type order`, `table orders`, `"Order"`},
			wantNot: []string{`order.struct_name`, `domain_types`},
		},
		{
			// Neither side has an override, so the only escape left is the
			// schema.
			name: "a composite type and a domain type meeting on the Go type name are renamed in the schema",
			composites: []parser.CompositeType{{
				Name: "order_x", Attributes: []parser.Attribute{{Name: "label", Type: "text"}},
			}},
			domains: []parser.DomainType{{Name: "order__x", BaseType: "text"}},
			wantErr: []string{`composite type order_x`, `domain type order__x`, `"OrderX"`, `schema`},
			wantNot: []string{`struct_name`},
		},
		{
			name:    "an enum collision offers the enum's struct_name",
			tables:  []parser.Table{namedTable("orders", "")},
			enums:   []parser.Enum{{Name: "order", Values: []string{"a", "b"}}},
			wantErr: []string{`enum order`, `"Order"`, `enums.order.struct_name`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &parser.Schema{
				Tables: tt.tables, Views: tt.views, Enums: tt.enums,
				CompositeTypes: tt.composites, DomainTypes: tt.domains,
			}

			rnrWantErr(t, schema, nameRuleConfig(), tt.wantErr)
			if len(tt.wantNot) == 0 {
				return
			}
			_, err := gen.ValidateGeneration(schema, nameRuleConfig())
			for _, w := range tt.wantNot {
				if err != nil && strings.Contains(err.Error(), w) {
					t.Errorf("ValidateGeneration() error = %v, want no %q: a composite or domain type has no override", err, w)
				}
			}
		})
	}
}

// TestResolvedNamesReservedReach covers who the reserved-name rule reaches
// (PRD §8.5): it is not override-gated, so a name a `struct_name` produced is
// rejected like a derived one, and a view is held to it like a table.
func TestResolvedNamesReservedReach(t *testing.T) {
	tests := []struct {
		name        string
		tables      []parser.Table
		views       []parser.View
		tableConfig map[string]config.TableConfig
		viewConfig  map[string]config.ViewConfig
		wantErr     []string
	}{
		{
			name:        "a table struct_name landing on a reserved name",
			tables:      []parser.Table{namedTable("accounts", "")},
			tableConfig: map[string]config.TableConfig{"accounts": {StructName: "PageInfo"}},
			wantErr:     []string{`table accounts`, `"PageInfo"`},
		},
		{
			name:    "a view deriving a reserved name",
			views:   []parser.View{namedView("page_infos", "")},
			wantErr: []string{`view page_infos`, `"PageInfo"`},
		},
		{
			name:       "a view struct_name landing on a reserved name",
			views:      []parser.View{namedView("reports", "")},
			viewConfig: map[string]config.ViewConfig{"reports": {StructName: "Edge"}},
			wantErr:    []string{`view reports`, `"Edge"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := nameRuleConfig()
			if tt.tableConfig != nil {
				cfg.Tables = tt.tableConfig
			}
			if tt.viewConfig != nil {
				cfg.Views = tt.viewConfig
			}
			schema := &parser.Schema{Tables: tt.tables, Views: tt.views}

			rnrWantErr(t, schema, cfg, tt.wantErr)
		})
	}
}

// TestResolvedNamesReservedStemsInBothLayouts pins "both layouts reserve both
// layouts' stems" (PRD §8.5): `models` and `views` are written only under
// `single_file`, and are reserved under `file_per_table` too, so switching
// layout never turns a valid config invalid.
func TestResolvedNamesReservedStemsInBothLayouts(t *testing.T) {
	tests := []struct {
		name       string
		layout     string
		structName string
		wantStem   string
	}{
		{name: "single_file reserves models", layout: "single_file", structName: "Models", wantStem: `"models"`},
		{name: "single_file reserves views", layout: "single_file", structName: "Views", wantStem: `"views"`},
		{name: "file_per_table reserves models", layout: "file_per_table", structName: "Models", wantStem: `"models"`},
		{name: "file_per_table reserves views", layout: "file_per_table", structName: "Views", wantStem: `"views"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadLayoutConfig(t, t.TempDir(), tt.layout)
			cfg.Tables = map[string]config.TableConfig{"widgets": {StructName: tt.structName}}
			schema := &parser.Schema{Tables: []parser.Table{namedTable("widgets", "")}}

			rnrWantErr(t, schema, cfg, []string{`table widgets`, tt.wantStem})
		})
	}
}

// TestResolvedNamesNestedAgainstReservedSet pins that the nested-mutation
// names are checked against the reserved set (PRD §8.5): a verb block equal to
// the configured client name is caught like any other reserved-name hit.
func TestResolvedNamesNestedAgainstReservedSet(t *testing.T) {
	cfg := nameRuleConfig()
	cfg.Output.Client = &config.ClientOutputConfig{Name: "UserEventsCreateNested", File: "client_gen.go"}
	schema := &parser.Schema{
		Tables: []parser.Table{namedTable("users", ""), childTable("events", "user_id")},
		Relationships: []parser.Relationship{{
			Name: "events", Type: parser.OneToMany,
			SourceTable: "users", TargetTable: "events", Key: []parser.KeyPair{{Local: "user_id", Referenced: "id"}},
		}},
	}

	rnrWantErr(t, schema, cfg, []string{`relationship users.Events`, `"UserEventsCreateNested"`})
}

// TestResolvedNamesNestedClaimIsStructural pins the conditioning PRD §8.5
// gives the nested claim: it is made on structural facts alone and consults
// neither the `api.operations` mask, nor a `filter:` on the edge (reducible to
// a discriminator or not), nor the §32 projection. Nested mutations are opted
// in throughout, so the knob under test is the only thing that could gate the
// claim. The has-one case is the positive claim for an O2O edge whose target
// holds the FK.
func TestResolvedNamesNestedClaimIsStructural(t *testing.T) {
	tests := []struct {
		name        string
		childTable  string
		collider    string
		relType     string
		relName     string
		mutate      func(cfg *config.RootConfig)
		wantRel     string
		wantNameHit string
	}{
		{
			name: "the api.operations mask does not withdraw the claim",
			mutate: func(cfg *config.RootConfig) {
				cfg.API = &config.APIConfig{Enabled: true, Operations: &config.Operations{Preset: config.PresetReadOnly}}
				users := cfg.Tables["users"]
				users.API = &config.TableAPIConfig{Operations: &config.Operations{Preset: config.PresetReadOnly}}
				cfg.Tables["users"] = users
			},
		},
		{
			name: "a filter reducible to a discriminator does not withdraw the claim",
			mutate: func(cfg *config.RootConfig) {
				users := cfg.Tables["users"]
				users.Relationships[0].Filter = "label = 'x'"
				cfg.Tables["users"] = users
			},
		},
		{
			name: "a discriminator does not withdraw the claim",
			mutate: func(cfg *config.RootConfig) {
				users := cfg.Tables["users"]
				users.Relationships[0].Discriminator = &config.RelationshipDiscriminator{Column: "label", Value: "x"}
				cfg.Tables["users"] = users
			},
		},
		{
			name: "a non-reducible filter still claims",
			mutate: func(cfg *config.RootConfig) {
				users := cfg.Tables["users"]
				users.Relationships[0].Filter = "label LIKE 'x%' OR label IS NULL"
				cfg.Tables["users"] = users
			},
		},
		{
			name: "the column access projection does not withdraw the claim",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["events"] = config.TableConfig{ColumnMap: map[string]config.ColumnOverride{
					"user_id": {Access: config.AccessInternal},
				}}
			},
		},
		{
			name:        "a has-one edge claims its names",
			childTable:  "user_profiles",
			collider:    "user_profile_create_nesteds",
			relType:     "one_to_one",
			relName:     "Profile",
			wantRel:     "relationship users.Profile",
			wantNameHit: `"UserProfileCreateNested"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			child, collider, relType, relName := "events", "user_events_create_nesteds", "one_to_many", "Events"
			wantRel, wantNameHit := "relationship users.Events", `"UserEventsCreateNested"`
			if tt.childTable != "" {
				child, collider, relType, relName = tt.childTable, tt.collider, tt.relType, tt.relName
				wantRel, wantNameHit = tt.wantRel, tt.wantNameHit
			}

			cfg := nameRuleConfig()
			cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
			cfg.Tables["users"] = config.TableConfig{Relationships: []config.TableRelationship{{
				Name: relName, Type: relType, Table: child, FK: config.KeyColumns{"user_id"},
			}}}
			if tt.mutate != nil {
				tt.mutate(cfg)
			}
			schema := &parser.Schema{Tables: []parser.Table{
				namedTable("users", ""),
				childTable(child, "user_id"),
				namedTable(collider, ""),
			}}

			rnrWantErr(t, schema, cfg, []string{wantRel, wantNameHit})
		})
	}
}

// rnrWantErr asserts ValidateGeneration fails with an error mentioning every
// string in want.
func rnrWantErr(t *testing.T, schema *parser.Schema, cfg *config.RootConfig, want []string) {
	t.Helper()
	_, err := gen.ValidateGeneration(schema, cfg)
	if err == nil {
		t.Fatalf("ValidateGeneration() error = nil, want an error mentioning %v", want)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("ValidateGeneration() error = %v, want it to mention %q", err, w)
		}
	}
}
