package config_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// The nested-mutation config surface (PRD §4.6 / §4.8 / §4.13 / §13.4.1 /
// §13.7.1). Every rule here is answerable from the config text alone, except
// the discriminator's column-existence check, which needs the parsed schema.

// assertErrContains fails unless err names every one of the given substrings.
func assertErrContains(t *testing.T, err error, call string, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s = nil, want an error naming %s", call, strings.Join(want, ", "))
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("%s error = %v, want it to name %q", call, err, w)
		}
	}
}

// discriminatorRel is the relationship shape §13.4.1's example uses.
func discriminatorRel(name, value string) config.TableRelationship {
	return config.TableRelationship{
		Name:  name,
		Type:  "o2m",
		Table: "documents",
		FK:    "entity_id",
		Discriminator: &config.RelationshipDiscriminator{
			Column: "entity_type",
			Value:  value,
		},
	}
}

// documentsSchema is the parsed-schema stand-in for the child table every
// discriminator test points at.
func documentsSchema() []config.SchemaTable {
	return []config.SchemaTable{
		{
			Name:   "assets",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "uuid", PrimaryKey: true},
			},
		},
		{
			Name:   "documents",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "entity_id", Type: "uuid"},
				{Name: "entity_type", Type: "text"},
				{Name: "name", Type: "text"},
			},
		},
	}
}

// TestValidateRelationships_FilterDiscriminatorExclusive pins the §4.13 rule:
// the two forms compose into a predicate whose write-side inverse is
// undefined, so declaring both names both keys rather than silently preferring
// one.
func TestValidateRelationships_FilterDiscriminatorExclusive(t *testing.T) {
	rel := discriminatorRel("Attachments", "asset.attachment")
	rel.Filter = "entity_type = 'asset.attachment'"

	cfg := validConfig()
	cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{rel}}

	_, err := config.ValidatePreParse(cfg)
	assertErrContains(t, err, "ValidatePreParse()",
		"tables.assets.relationships[0]", "Attachments", "filter", "discriminator")
}

// TestValidateRelationships_FilterOrDiscriminatorAloneAccepted is the other
// half: each form on its own is legal, and so is neither.
func TestValidateRelationships_FilterOrDiscriminatorAloneAccepted(t *testing.T) {
	tests := []struct {
		name string
		rel  config.TableRelationship
	}{
		{"discriminator alone", discriminatorRel("Attachments", "asset.attachment")},
		{
			name: "filter alone",
			rel: config.TableRelationship{
				Name: "PhotoAttachments", Type: "o2m", Table: "documents", FK: "entity_id",
				Filter: "entity_type = 'asset.attachment' AND name LIKE 'photo_%'",
			},
		},
		{
			name: "neither",
			rel:  config.TableRelationship{Name: "Documents", Type: "o2m", Table: "documents", FK: "entity_id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{tt.rel}}

			if _, err := config.ValidatePreParse(cfg); err != nil {
				t.Errorf("ValidatePreParse() = %v, want nil", err)
			}
		})
	}
}

// TestRelationshipDedupKey_DiscriminatorSubCategorizes is the load-bearing
// case: sub-categorized edges share their (target, fk) prefix by definition, so
// without a discriminator component in the dedup key three `discriminator:`
// edges into one child table on one FK collapse onto a single key and are
// rejected as duplicates — which would make the structured form unusable for
// exactly the pattern it exists to describe (PRD §13.7.1).
func TestRelationshipDedupKey_DiscriminatorSubCategorizes(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{
		discriminatorRel("PrimaryDocuments", "asset.primary"),
		discriminatorRel("Attachments", "asset.attachment"),
		discriminatorRel("Invoices", "asset.invoice"),
	}}

	if _, err := config.ValidatePreParse(cfg); err != nil {
		t.Errorf("ValidatePreParse() = %v, want three discriminator edges over one (target, fk) to be accepted", err)
	}
}

// TestRelationshipDedupKey_DiscriminatorDuplicatesRejected is the failing half
// of the same key: identical column AND value over one (target, fk) is a
// genuine duplicate. A `filter:` edge never collides with a `discriminator:`
// one, because each contributes to a different component.
func TestRelationshipDedupKey_DiscriminatorDuplicatesRejected(t *testing.T) {
	tests := []struct {
		name    string
		rels    []config.TableRelationship
		wantDup bool
	}{
		{
			name: "same column and value",
			rels: []config.TableRelationship{
				discriminatorRel("Attachments", "asset.attachment"),
				discriminatorRel("Attachments2", "asset.attachment"),
			},
			wantDup: true,
		},
		{
			name: "same value, different column",
			rels: func() []config.TableRelationship {
				b := discriminatorRel("Attachments2", "asset.attachment")
				b.Discriminator.Column = "kind"
				return []config.TableRelationship{discriminatorRel("Attachments", "asset.attachment"), b}
			}(),
		},
		{
			name: "filter edge beside an equivalent discriminator edge",
			rels: []config.TableRelationship{
				discriminatorRel("Attachments", "asset.attachment"),
				{
					Name: "PhotoAttachments", Type: "o2m", Table: "documents", FK: "entity_id",
					Filter: "entity_type = 'asset.attachment'",
				},
			},
		},
		{
			name: "string and integer values are distinct declarations",
			rels: func() []config.TableRelationship {
				b := discriminatorRel("Numeric", "")
				b.Discriminator.Value = 1
				return []config.TableRelationship{discriminatorRel("Textual", "1"), b}
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables["assets"] = config.TableConfig{Relationships: tt.rels}

			_, err := config.ValidatePreParse(cfg)
			if !tt.wantDup {
				if err != nil {
					t.Errorf("ValidatePreParse() = %v, want nil", err)
				}
				return
			}
			assertErrContains(t, err, "ValidatePreParse()", "duplicate relationship", "relationships[1]")
		})
	}
}

// TestValidateRelationshipDiscriminators_ColumnMustExist pins the §4.13
// schema-level rule: the discriminator compiles to a predicate on the related
// table and a nested create sets it, so a column that does not exist can do
// neither. The error names the relationship, the column and the related table.
func TestValidateRelationshipDiscriminators_ColumnMustExist(t *testing.T) {
	rel := discriminatorRel("Attachments", "asset.attachment")
	rel.Discriminator.Column = "entity_kind"

	cfg := validConfig()
	cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{rel}}

	_, err := config.ValidatePostParse(cfg, documentsSchema(), nil)
	assertErrContains(t, err, "ValidatePostParse()",
		"Attachments", "entity_kind", "documents", "discriminator.column")
}

// TestValidateRelationshipDiscriminators_Accepted covers the passing paths:
// a column that exists, and a target table absent from the parsed schema —
// the latter is skipped rather than reported here, because the missing-target
// diagnosis belongs to relationship resolution and would otherwise stack a
// second error naming the discriminator for a problem it does not have.
func TestValidateRelationshipDiscriminators_Accepted(t *testing.T) {
	tests := []struct {
		name  string
		rel   config.TableRelationship
		table string
	}{
		{"column exists", discriminatorRel("Attachments", "asset.attachment"), "documents"},
		{"column exists, schema-qualified target", discriminatorRel("Attachments", "asset.attachment"), "public.documents"},
		{"target table not in the parsed schema", discriminatorRel("Attachments", "asset.attachment"), "ghosts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := tt.rel
			rel.Table = tt.table

			cfg := validConfig()
			cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{rel}}

			if _, err := config.ValidatePostParse(cfg, documentsSchema(), nil); err != nil {
				t.Errorf("ValidatePostParse() = %v, want nil", err)
			}
		})
	}
}

// TestValidateNestedMutations_MaxDepth pins the §4.13 depth ceiling: 1 is the
// only accepted value in v1, so a future depth cannot silently mean depth 1.
// An omitted key takes the default and says nothing.
func TestValidateNestedMutations_MaxDepth(t *testing.T) {
	tests := []struct {
		name     string
		maxDepth *int
		wantErr  bool
	}{
		{"omitted", nil, false},
		{"one", new(1), false},
		{"two", new(2), true},
		{"zero", new(0), true},
		{"negative", new(-1), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Generation.NestedMutations = &config.NestedMutationsConfig{
				Enabled:  new(true),
				MaxDepth: tt.maxDepth,
			}

			_, err := config.ValidatePreParse(cfg)
			if !tt.wantErr {
				if err != nil {
					t.Errorf("ValidatePreParse() = %v, want nil", err)
				}
				return
			}
			assertErrContains(t, err, "ValidatePreParse()", "generation.nested_mutations.max_depth")
		})
	}
}

// TestValidateNestedMutations_ClosedSets pins the §4.13 rule that
// `operations` and `verbs` are closed sets — a typo'd verb would otherwise
// silently narrow the generated surface.
func TestValidateNestedMutations_ClosedSets(t *testing.T) {
	tests := []struct {
		name    string
		ops     []string
		verbs   []string
		wantErr string
	}{
		{name: "both omitted"},
		{name: "full sets", ops: []string{"create", "update", "upsert"}, verbs: []string{"create", "connect", "disconnect", "clear"}},
		{name: "narrowed sets", ops: []string{"create"}, verbs: []string{"connect", "clear"}},
		{name: "unknown operation", ops: []string{"create", "delete"}, wantErr: "generation.nested_mutations.operations[1]"},
		{name: "unknown verb", verbs: []string{"create", "disconect"}, wantErr: "generation.nested_mutations.verbs[1]"},
		{name: "a verb is not an operation", ops: []string{"connect"}, wantErr: "generation.nested_mutations.operations[0]"},
		{name: "an operation is not a verb", verbs: []string{"update"}, wantErr: "generation.nested_mutations.verbs[0]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Generation.NestedMutations = &config.NestedMutationsConfig{
				Enabled:    new(true),
				Operations: tt.ops,
				Verbs:      tt.verbs,
			}

			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidatePreParse() = %v, want nil", err)
				}
				return
			}
			assertErrContains(t, err, "ValidatePreParse()", tt.wantErr)
		})
	}
}

// TestValidateNestedMutations_ClientPairIsTheLeftoverKeyError pins that the
// base-operation rule no longer has a client half. The pair it once rejected on
// `operations` cannot be written on the client any more: the block itself is the
// §4.13 leftover-key error, which names api.operations, and the rule does not
// report it a second time.
func TestValidateNestedMutations_ClientPairIsTheLeftoverKeyError(t *testing.T) {
	cfg, err := config.LoadConfig(writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
tables:
  assets:
    operations:
      create: false
      create_with_related: true
`))
	if err != nil {
		t.Fatalf("LoadConfig() = %v, want the key decoded so validation can name it", err)
	}
	_, err = config.ValidatePreParse(cfg)
	assertErrContains(t, err, "ValidatePreParse()", "tables.assets.operations: the Go client generates every method the schema allows")
	if strings.Contains(err.Error(), "create_with_related: cannot be true") {
		t.Errorf("ValidatePreParse() = %v, want no base-operation error for a client block, which no longer exists", err)
	}
}

// TestValidateNestedMutations_APIMaskWithRelatedNeedsBase pins the
// base-operation rule on an `api.operations` mask. The mask conjoins each
// `…_with_related` toggle with its masked base (PRD §26.5.1), so `create_with_related: true`
// beside `create: false` would drop `create<T>WithRelated` from the API with no
// signal — the accept-and-drop shape §4.13 rejects on `operations`. The rule
// covers the global mask and every per-table mask, and names the key path.
func TestValidateNestedMutations_APIMaskWithRelatedNeedsBase(t *testing.T) {
	tests := []struct {
		name    string
		mask    config.Operations
		wantErr []string // the first entry is the key suffix after the mask's path
	}{
		{
			name:    "create_with_related without create",
			mask:    config.Operations{Create: new(false), CreateWithRelated: new(true)},
			wantErr: []string{".create_with_related", "§26.5.1"},
		},
		{
			name:    "update_with_related without update",
			mask:    config.Operations{Update: new(false), UpdateWithRelated: new(true)},
			wantErr: []string{".update_with_related", "§26.5.1"},
		},
		{
			name:    "upsert_with_related without upsert",
			mask:    config.Operations{Upsert: new(false), UpsertWithRelated: new(true)},
			wantErr: []string{".upsert_with_related", "§26.5.1"},
		},
		{
			name:    "read_only mask with an explicit nested toggle",
			mask:    config.Operations{Preset: "read_only", CreateWithRelated: new(true)},
			wantErr: []string{".create_with_related", "§26.5.1"},
		},
		{
			// Masking the base alone is how a consumer takes both the flat and
			// the nested mutation off the API (§26.5.1); the nested key was
			// never named, so there is no flag that got nothing.
			name: "base masked off without naming the nested key",
			mask: config.Operations{Create: new(false)},
		},
		{
			name: "explicitly false nested toggle beside a masked-off base",
			mask: config.Operations{Create: new(false), CreateWithRelated: new(false)},
		},
		{
			name: "read_only mask resolves every pair consistently",
			mask: config.Operations{Preset: "read_only"},
		},
		{
			name: "nested toggle beside its enabled base",
			mask: config.Operations{Preset: "read_only", Update: new(true), UpdateWithRelated: new(true)},
		},
	}

	levels := []struct {
		name, path string
		apply      func(cfg *config.RootConfig, mask *config.Operations)
	}{
		{"global", "api.operations", func(cfg *config.RootConfig, mask *config.Operations) {
			cfg.API.Operations = mask
		}},
		{"per-table", "tables.assets.api.operations", func(cfg *config.RootConfig, mask *config.Operations) {
			cfg.Tables["assets"] = config.TableConfig{API: &config.TableAPIConfig{Operations: mask}}
		}},
	}

	for _, tt := range tests {
		for _, level := range levels {
			t.Run(level.name+"/"+tt.name, func(t *testing.T) {
				cfg := validConfig()
				cfg.API = &config.APIConfig{Enabled: true, GraphQL: &config.GraphQLAPIConfig{Enabled: true}}
				mask := tt.mask
				level.apply(cfg, &mask)

				_, err := config.ValidatePreParse(cfg)
				if len(tt.wantErr) == 0 {
					if err != nil {
						t.Errorf("ValidatePreParse() = %v, want nil", err)
					}
					return
				}
				want := append([]string{level.path + tt.wantErr[0]}, tt.wantErr[1:]...)
				assertErrContains(t, err, "ValidatePreParse()", want...)
			})
		}
	}
}

// TestLoadConfig_NestedMutationKeysParse is the strict-decoding half: every
// nested-mutation key must load under KnownFields(true). The `…_with_related`
// mask keys are the ones that silently vanished before — Operations defines its
// own UnmarshalYAML, and yaml.Node.Decode builds a fresh non-strict decoder the
// setting cannot reach — so a missing field there is accepted and dropped
// rather than reported.
func TestLoadConfig_NestedMutationKeysParse(t *testing.T) {
	const src = `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
api:
  enabled: true
  operations:
    preset: all
    create_with_related: true
    update_with_related: false
    upsert_with_related: true
generation:
  nested_mutations:
    enabled: true
    operations: [create, update, upsert]
    verbs: [create, connect, disconnect, clear]
    max_depth: 1
tables:
  assets:
    nested_mutations:
      relationships:
        - name: Attachments
        - name: Invoices
          allow_reparent: true
    relationships:
      - name: Attachments
        type: o2m
        table: documents
        fk: entity_id
        discriminator:
          column: entity_type
          value: "asset.attachment"
`

	cfg, err := config.LoadConfig(writeTestConfig(t, src))
	if err != nil {
		t.Fatalf("LoadConfig() = %v, want nil", err)
	}

	ops := cfg.API.Operations
	for _, tt := range []struct {
		key  string
		got  *bool
		want bool
	}{
		{"create_with_related", ops.CreateWithRelated, true},
		{"update_with_related", ops.UpdateWithRelated, false},
		{"upsert_with_related", ops.UpsertWithRelated, true},
	} {
		if tt.got == nil {
			t.Errorf("api.operations.%s was accepted and dropped, want it decoded as %v", tt.key, tt.want)
			continue
		}
		if *tt.got != tt.want {
			t.Errorf("api.operations.%s = %v, want %v", tt.key, *tt.got, tt.want)
		}
	}

	nm := cfg.Generation.NestedMutations
	if nm == nil {
		t.Fatal("generation.nested_mutations = nil, want the decoded block")
	}
	if nm.Enabled == nil || !*nm.Enabled {
		t.Errorf("generation.nested_mutations.enabled = %v, want true", nm.Enabled)
	}
	if nm.MaxDepth == nil || *nm.MaxDepth != 1 {
		t.Errorf("generation.nested_mutations.max_depth = %v, want 1", nm.MaxDepth)
	}
	if len(nm.Operations) != 3 || len(nm.Verbs) != 4 {
		t.Errorf("generation.nested_mutations operations/verbs = %v / %v, want 3 and 4 entries", nm.Operations, nm.Verbs)
	}

	table := cfg.Tables["assets"]
	if table.NestedMutations == nil || len(table.NestedMutations.Relationships) != 2 {
		t.Fatalf("tables.assets.nested_mutations = %+v, want two allowlist entries", table.NestedMutations)
	}
	allow := table.NestedMutations.Relationships
	if allow[0].Name != "Attachments" || allow[0].AllowReparent {
		t.Errorf("allowlist[0] = %+v, want {Attachments, allow_reparent: false}", allow[0])
	}
	if allow[1].Name != "Invoices" || !allow[1].AllowReparent {
		t.Errorf("allowlist[1] = %+v, want {Invoices, allow_reparent: true}", allow[1])
	}

	if len(table.Relationships) != 1 {
		t.Fatalf("tables.assets.relationships = %d entries, want 1", len(table.Relationships))
	}
	disc := table.Relationships[0].Discriminator
	if disc == nil {
		t.Fatal("relationships[0].discriminator = nil, want the decoded block")
	}
	if disc.Column != "entity_type" || disc.Value != "asset.attachment" {
		t.Errorf("relationships[0].discriminator = %+v, want {entity_type, asset.attachment}", *disc)
	}
}

// TestLoadConfig_RejectsUnknownNestedMutationKeys is the other half of strict
// decoding: the new blocks have no custom unmarshaler, so a typo inside one
// hard-errors rather than vanishing.
func TestLoadConfig_RejectsUnknownNestedMutationKeys(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "typo under generation.nested_mutations",
			yaml:    "generation:\n  nested_mutations:\n    enabld: true\n",
			wantErr: "field enabld not found",
		},
		{
			name:    "typo under tables.<t>.nested_mutations",
			yaml:    "tables:\n  assets:\n    nested_mutations:\n      relationship:\n        - name: Attachments\n",
			wantErr: "field relationship not found",
		},
		{
			name:    "typo inside a relationship discriminator",
			yaml:    "tables:\n  assets:\n    relationships:\n      - name: Attachments\n        discriminator:\n          col: entity_type\n",
			wantErr: "field col not found",
		},
	}

	const head = "input:\n  dialect: postgres\n  paths: [\"./migrations\"]\noutput:\n  dir: ./models\n"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.LoadConfig(writeTestConfig(t, head+tt.yaml))
			assertErrContains(t, err, "LoadConfig()", tt.wantErr)
		})
	}
}

// TestValidateNestedMutations_RequiredKeys pins the keys PRD §4.8 and §13.4.1
// mark Required. schema/v1.json already declares all three in its `required`
// lists, so without these the Go loader is laxer than the schema it ships —
// a file the editor paints red and `sqlgen validate` waves through.
//
// A missing `value` has teeth beyond the asymmetry: it renders as
// `<nil>:<nil>` in the dedup key, so two value-less edges over one (target, fk)
// would collide as duplicates over a value neither of them declares.
func TestValidateNestedMutations_RequiredKeys(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(tc *config.TableConfig)
		wantErr []string
	}{
		{
			name: "discriminator without column",
			mutate: func(tc *config.TableConfig) {
				tc.Relationships[0].Discriminator.Column = ""
			},
			wantErr: []string{"discriminator.column", "required", "Attachments"},
		},
		{
			name: "discriminator without value",
			mutate: func(tc *config.TableConfig) {
				tc.Relationships[0].Discriminator.Value = nil
			},
			wantErr: []string{"discriminator.value", "required", "Attachments"},
		},
		{
			name: "allowlist entry without name",
			mutate: func(tc *config.TableConfig) {
				tc.NestedMutations = &config.TableNestedMutationsConfig{
					Relationships: []config.TableNestedRelationship{{AllowReparent: true}},
				}
			},
			wantErr: []string{"tables.assets.nested_mutations.relationships[0].name", "required"},
		},
		{
			// An empty string is a key the user did write, and the schema
			// accepts it — only absence is the missing-key case.
			name: "explicit empty-string value is not absence",
			mutate: func(tc *config.TableConfig) {
				tc.Relationships[0].Discriminator.Value = ""
			},
		},
		{
			name:   "both keys present",
			mutate: func(tc *config.TableConfig) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := config.TableConfig{
				Relationships: []config.TableRelationship{discriminatorRel("Attachments", "asset.attachment")},
			}
			tt.mutate(&tc)

			cfg := validConfig()
			cfg.Tables["assets"] = tc

			_, err := config.ValidatePreParse(cfg)
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Errorf("ValidatePreParse() = %v, want nil", err)
				}
				return
			}
			assertErrContains(t, err, "ValidatePreParse()", tt.wantErr...)
		})
	}
}

// TestValidateRelationshipDiscriminators_EmptyColumnDefersToRequired pins that
// the schema-level column-existence rule stays quiet on an empty column: the
// missing-key rule already names it for what it is, and a second error reading
// "not a column on the related table" would describe the symptom, not the cause.
func TestValidateRelationshipDiscriminators_EmptyColumnDefersToRequired(t *testing.T) {
	rel := discriminatorRel("Attachments", "asset.attachment")
	rel.Discriminator.Column = ""

	cfg := validConfig()
	cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{rel}}

	if _, err := config.ValidatePostParse(cfg, documentsSchema(), nil); err != nil {
		t.Errorf("ValidatePostParse() = %v, want the empty column left to the required-key rule", err)
	}
}

// TestValidateDiscriminatorValue covers the two shapes the generator cannot
// render. Both exist because the Go loader must not be laxer than the schema it
// ships: schema/v1.json restricts `value` to a scalar, and a value the
// generator cannot spell would otherwise reach codegen and be rendered wrong
// with no error at load, validate or generate time.
func TestValidateDiscriminatorValue(t *testing.T) {
	tests := []struct {
		name     string
		relType  string
		value    any
		wantErr  bool
		wantWord string
	}{
		{name: "string", relType: "o2m", value: "asset.attachment"},
		{name: "int", relType: "o2m", value: 3},
		{name: "bool", relType: "o2m", value: true},
		{name: "float", relType: "o2m", value: 3.5},
		{
			name: "a sequence has no rendering", relType: "o2m",
			value: []any{"a", "b"}, wantErr: true, wantWord: "string, number or boolean",
		},
		{
			name: "a mapping has no rendering", relType: "o2m",
			value: map[string]any{"a": 1}, wantErr: true, wantWord: "string, number or boolean",
		},
		{
			// o2o interpolates into the JOIN ON clause, and a backslash has no
			// spelling that MySQL and PostgreSQL read the same way.
			name: "backslash on an interpolated o2o edge", relType: "o2o",
			value: `photo\1`, wantErr: true, wantWord: "backslash",
		},
		{
			// o2m binds the value, so a backslash is just data.
			name: "backslash on a bound o2m edge", relType: "o2m",
			value: `photo\1`,
		},
		{
			name: "backslash on a bound m2m edge", relType: "m2m",
			value: `photo\1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := config.TableRelationship{
				Name: "Attachments", Type: tt.relType, Table: "documents", FK: "entity_id",
				Discriminator: &config.RelationshipDiscriminator{Column: "entity_type", Value: tt.value},
			}
			if tt.relType == "m2m" {
				rel.FK = ""
				rel.Junction = "asset_document_links"
			}

			cfg := validConfig()
			cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{rel}}

			_, err := config.ValidatePreParse(cfg)
			if !tt.wantErr {
				if err != nil {
					t.Errorf("ValidatePreParse() = %v, want nil", err)
				}
				return
			}
			assertErrContains(t, err, "ValidatePreParse()", "discriminator.value", tt.wantWord)
		})
	}
}
