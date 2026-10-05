package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// pkColumnNames extracts the Name field from each PK column for tidy assertion.
func pkColumnNames(t *testing.T, ctx gen.TableContext) []string {
	t.Helper()
	names := make([]string, 0, len(ctx.PKColumns))
	for _, c := range ctx.PKColumns {
		names = append(names, c.Name)
	}
	return names
}

// TestBuildTableContexts_PKOverride_FillsEmpty pins that
// when the schema has no PRIMARY KEY clause, the tables.<name>.primary_key.columns
// override declares the PK column set.
func TestBuildTableContexts_PKOverride_FillsEmpty(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "counters",
				Columns: []parser.Column{
					{Name: "organization_id", Type: "bigint", Nullable: false, Unique: true},
					{Name: "count", Type: "bigint", Nullable: false},
				},
			},
		},
	}
	in := testInput(schema)
	in.Config.Tables["counters"] = config.TableConfig{
		PrimaryKey: &config.TablePrimaryKeyConfig{
			Columns: []string{"organization_id"},
		},
	}

	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("BuildTableContexts() returned %d contexts, want 1", len(contexts))
	}

	got := pkColumnNames(t, contexts[0])
	want := []string{"organization_id"}
	if !slices.Equal(got, want) {
		t.Errorf("PKColumns = %v, want %v", got, want)
	}
}

// TestBuildTableContexts_PKOverride_WinsOverAutoDetect pins the
// silent-override design decision (PRD §8.6): when both an auto-detected
// PK AND an override are present, the override silently wins.
func TestBuildTableContexts_PKOverride_WinsOverAutoDetect(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "thing",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true, Nullable: false},
					{Name: "natural_key", Type: "text", Nullable: false, Unique: true},
				},
			},
		},
	}
	in := testInput(schema)
	in.Config.Tables["thing"] = config.TableConfig{
		PrimaryKey: &config.TablePrimaryKeyConfig{
			Columns: []string{"natural_key"},
		},
	}

	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}

	got := pkColumnNames(t, contexts[0])
	want := []string{"natural_key"}
	if !slices.Equal(got, want) {
		t.Errorf("PKColumns = %v, want %v (override should silently replace auto-detected id)", got, want)
	}

	// The auto-detected column must lose its PrimaryKey flag — otherwise
	// downstream code (cache, events, struct emission) would see two PK
	// columns and produce inconsistent output.
	var idIsPK bool
	for _, c := range contexts[0].Columns {
		if c.Name == "id" && c.PrimaryKey {
			idIsPK = true
		}
	}
	if idIsPK {
		t.Error("auto-detected id column still has PrimaryKey:true after override; expected override to clear it")
	}
}

// TestBuildTableContexts_PKOverride_CompositePreservesOrder pins the
// CACHE.md §16 / PRD §8.7 composite-PK ordering invariant: the resolved
// PKColumns slice must match the user-declared order, not the schema
// column order.
func TestBuildTableContexts_PKOverride_CompositePreservesOrder(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "junction",
				Columns: []parser.Column{
					{Name: "asset_id", Type: "bigint", Nullable: false},
					{Name: "ppa_id", Type: "bigint", Nullable: false},
				},
				Constraints: []parser.Constraint{
					{Type: parser.Unique, Columns: []string{"asset_id", "ppa_id"}},
				},
			},
		},
	}
	in := testInput(schema)
	in.Config.Tables["junction"] = config.TableConfig{
		PrimaryKey: &config.TablePrimaryKeyConfig{
			Columns: []string{"ppa_id", "asset_id"}, // reverse of schema order
		},
	}

	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}

	got := pkColumnNames(t, contexts[0])
	want := []string{"ppa_id", "asset_id"}
	if !slices.Equal(got, want) {
		t.Errorf("PKColumns = %v, want %v (declaration order is load-bearing)", got, want)
	}
}

// TestBuildTableContexts_NoPK_SkippedFromGeneration pins PRD §9.4b: a table
// with no auto-detected PK and no primary_key.columns override is skipped
// from generation entirely — no TableContext is produced, so no client,
// model, cache entry, or event hook is emitted. The accompanying user-facing
// warning is exercised separately in TestValidatePostParse_MissingPrimaryKey
// (cmd/sqlgen/config).
func TestBuildTableContexts_NoPK_SkippedFromGeneration(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "counters",
				Columns: []parser.Column{
					{Name: "name", Type: "text", Nullable: false},
					{Name: "count", Type: "bigint", Nullable: false},
				},
			},
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true, Nullable: false},
				},
			},
		},
	}
	in := testInput(schema)

	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	if len(contexts) != 1 {
		got := make([]string, len(contexts))
		for i, c := range contexts {
			got[i] = c.TableName
		}
		t.Fatalf("BuildTableContexts returned %d tables (%v), want 1 (users only — counters has no PK and should be skipped)", len(contexts), got)
	}
	if contexts[0].TableName != "users" {
		t.Errorf("remaining table = %q, want %q (counters should be skipped)", contexts[0].TableName, "users")
	}
}

// TestBuildTableContexts_NoPK_WithOverride_NotSkipped confirms the inverse:
// a no-schema-PK table whose primary_key.columns override resolves the PK
// is NOT skipped — the override is the user's explicit declaration that the
// table is keyed.
func TestBuildTableContexts_NoPK_WithOverride_NotSkipped(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "counters",
				Columns: []parser.Column{
					{Name: "organization_id", Type: "bigint", Nullable: false, Unique: true},
					{Name: "count", Type: "bigint", Nullable: false},
				},
			},
		},
	}
	in := testInput(schema)
	in.Config.Tables["counters"] = config.TableConfig{
		PrimaryKey: &config.TablePrimaryKeyConfig{
			Columns: []string{"organization_id"},
		},
	}

	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	tc := contexts[0]
	if len(tc.PKColumns) == 0 {
		t.Fatal("expected non-empty PKColumns after override")
	}
	if !tc.Operations.Get {
		t.Error("Operations.Get should be enabled when primary_key.columns supplies a PK")
	}
	if !tc.Operations.Update {
		t.Error("Operations.Update should be enabled when primary_key.columns supplies a PK")
	}
}

// TestBuildTableContexts_ExcludeTables pins that tables
// matching any pattern in cfg.ExcludeTables are dropped silently before
// table-context build. Glob syntax (filepath.Match) — `*`, `?`,
// case-sensitive.
func TestBuildTableContexts_ExcludeTables(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true, Nullable: false},
				},
			},
			{
				Name: "temp_scratch",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true, Nullable: false},
				},
			},
			{
				Name: "temp_aux",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true, Nullable: false},
				},
			},
			{
				Name: "report_acl",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true, Nullable: false},
				},
			},
		},
	}
	in := testInput(schema)
	in.Config.ExcludeTables = []string{"temp_*", "report_acl"}

	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}

	if len(contexts) != 1 {
		got := make([]string, len(contexts))
		for i, c := range contexts {
			got[i] = c.TableName
		}
		t.Fatalf("BuildTableContexts returned %d tables (%v), want 1 (users)", len(contexts), got)
	}
	if contexts[0].TableName != "users" {
		t.Errorf("remaining table = %q, want %q", contexts[0].TableName, "users")
	}
}

// TestBuildTableContexts_ExcludeTables_SchemaQualified confirms patterns
// can target a schema-qualified form (e.g. `audit.*`).
func TestBuildTableContexts_ExcludeTables_SchemaQualified(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true, Nullable: false},
				},
			},
			{
				Name:   "trail",
				Schema: "audit",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true, Nullable: false},
				},
			},
		},
	}
	in := testInput(schema)
	in.Config.ExcludeTables = []string{"audit.*"}

	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("got %d tables, want 1", len(contexts))
	}
	if contexts[0].TableName != "users" {
		t.Errorf("remaining table = %q, want %q", contexts[0].TableName, "users")
	}
}

// TestBuildTableContexts_PKOverride_Fallthrough confirms that tables without
// an override fall through to the auto-detected PK — no behavior change
// for the happy path.
func TestBuildTableContexts_PKOverride_Fallthrough(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
				},
			},
		},
	}
	in := testInput(schema)

	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}

	got := pkColumnNames(t, contexts[0])
	want := []string{"id"}
	if !slices.Equal(got, want) {
		t.Errorf("PKColumns = %v, want %v (auto-detected baseline must be preserved)", got, want)
	}
}

// TestBuildTableContexts_SubCategorizedRelationships pins the
// relationship-list relaxation: two config relationships sharing the
// (target, fk) prefix but with distinct `filter:` strings produce two
// independent RelationshipContext entries on the parent's TableContext,
// each named per its config `name:`.
//
// The Filter strings must be passed through verbatim — the per-table
// relationship loader template renders them onto the WHERE clause, so a
// stray normalization here would silently change the executed SQL.
func TestBuildTableContexts_SubCategorizedRelationships(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "asset",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "title", Type: "text", Nullable: false},
				},
			},
			{
				Name: "documents",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "entity_id", Type: "uuid", Nullable: false},
					{Name: "entity_type", Type: "text", Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
				},
			},
		},
	}

	in := testInput(schema)
	in.Config.Tables["asset"] = config.TableConfig{
		Relationships: []config.TableRelationship{
			{Name: "PrimaryDocument", Type: "o2o", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.primary'"},
			{Name: "Attachments", Type: "o2m", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.attachment'"},
			{Name: "Invoices", Type: "o2m", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.invoice'"},
		},
	}

	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}

	var asset *gen.TableContext
	for i := range contexts {
		if contexts[i].TableName == "asset" {
			asset = &contexts[i]
			break
		}
	}
	if asset == nil {
		t.Fatal("asset context not found")
	}

	// Build name → filter map for assertion. We don't pin slice ordering
	// because buildRelationshipContexts sorts alphabetically — instead we
	// pin set membership, which is the load-bearing invariant.
	got := make(map[string]string, len(asset.Relationships))
	for _, r := range asset.Relationships {
		got[r.Name] = r.Filter
	}
	want := map[string]string{
		"PrimaryDocument": "entity_type = 'asset.primary'",
		"Attachments":     "entity_type = 'asset.attachment'",
		"Invoices":        "entity_type = 'asset.invoice'",
	}
	if len(got) != len(want) {
		t.Fatalf("relationships count = %d, want %d (got %v)", len(got), len(want), got)
	}
	for name, wantFilter := range want {
		gotFilter, ok := got[name]
		if !ok {
			t.Errorf("relationship %q not emitted", name)
			continue
		}
		if gotFilter != wantFilter {
			t.Errorf("relationship %q Filter = %q, want %q", name, gotFilter, wantFilter)
		}
	}

	// One RelationshipOptionsDef per target struct, not per relationship —
	// the parent's FieldOptions references three distinct fields, but the
	// embedded *DocumentRelationshipOptions struct is shared.
	var docDefs int
	for _, def := range asset.RelationshipOptionsDefs {
		if def.TargetStructName == "Document" {
			docDefs++
		}
	}
	if docDefs != 1 {
		t.Errorf("DocumentRelationshipOptions emitted %d times, want 1 (struct must be shared across sub-categorized fields)", docDefs)
	}
}

// multiFKAssetSchema builds the asset/stakeholder shape: a stakeholder
// parent with three FK edges from asset (developer_id, om_provider_id,
// owner_id) — auto-detected as three O2M relationships that the parser
// disambiguates to Name="developer_asset" / "om_provider_asset" /
// "owner_asset" with BaseName="asset". A fourth FK from asset → users
// stays single-edge so its Name/BaseName are both "asset" (different group
// → not disambiguated).
func multiFKAssetSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "stakeholder",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name: "asset",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "developer_id", Type: "uuid", FKReference: &parser.FKReference{Table: "stakeholder", Column: "id"}},
					{Name: "om_provider_id", Type: "uuid", FKReference: &parser.FKReference{Table: "stakeholder", Column: "id"}},
					{Name: "owner_id", Type: "uuid", FKReference: &parser.FKReference{Table: "stakeholder", Column: "id"}},
					{Name: "created_by", Type: "uuid", FKReference: &parser.FKReference{Table: "users", Column: "id"}},
				},
			},
		},
	}
}

func stakeholderRelationships(t *testing.T, in *gen.GenerateInput) []gen.RelationshipContext {
	t.Helper()
	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	for i := range contexts {
		if contexts[i].TableName == "stakeholder" {
			return contexts[i].Relationships
		}
	}
	t.Fatal("stakeholder context not found")
	return nil
}

func relationshipNames(rels []gen.RelationshipContext) []string {
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, r.Name)
	}
	return out
}

// TestBuildRelationshipContexts_ExcludeByBaseName pins the
// `exclude_relationships` contract restoration: a bare table-name entry
// continues to exclude every auto-detected edge that collided on that
// BaseName (the pre-disambiguation Name), even after multi-FK
// disambiguation rewrites the user-visible Name to a unique form.
func TestBuildRelationshipContexts_ExcludeByBaseName(t *testing.T) {
	// Baseline: no excludes — stakeholder sees three disambiguated edges
	// from asset (BaseName="asset"), each pluralized for its slice-shaped
	// field (`DeveloperAssets []*Asset` etc.). The fourth FK
	// (asset.created_by → users) lives on a different (source, target)
	// group, so it does not surface on stakeholder; users sees the one
	// undecorated edge pluralized to "assets".
	in := testInput(multiFKAssetSchema())
	parser.DetectRelationships(in.Schema)
	gotBaseline := relationshipNames(stakeholderRelationships(t, in))
	wantBaseline := []string{"developer_assets", "om_provider_assets", "owner_assets"}
	if diff := cmp.Diff(wantBaseline, gotBaseline); diff != "" {
		t.Fatalf("baseline relationship names (-want +got):\n%s", diff)
	}

	// Bare-name exclude: `exclude_relationships: [asset]` matches every
	// edge whose BaseName is "asset" → all three are dropped. This is the
	// user-facing contract: schemas with a single FK between
	// (stakeholder, asset) used the bare name; multi-FK schemas must
	// continue to honor it.
	in = testInput(multiFKAssetSchema())
	parser.DetectRelationships(in.Schema)
	in.Config.Tables["stakeholder"] = config.TableConfig{
		ExcludeRelationships: []string{"asset"},
	}
	if got := relationshipNames(stakeholderRelationships(t, in)); len(got) != 0 {
		t.Errorf("bare-name exclude: got %v relationships, want none", got)
	}

	// Disambiguated-name exclude: targeting one specific edge by its
	// post-rewrite Name (the pluralized form the user sees in generated
	// code, e.g. `DeveloperAssets`) drops only that edge; siblings in the
	// same (source, target) group remain.
	in = testInput(multiFKAssetSchema())
	parser.DetectRelationships(in.Schema)
	in.Config.Tables["stakeholder"] = config.TableConfig{
		ExcludeRelationships: []string{"developer_assets"},
	}
	got := relationshipNames(stakeholderRelationships(t, in))
	want := []string{"om_provider_assets", "owner_assets"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("disambiguated-name exclude (-want +got):\n%s", diff)
	}

	// Mixed: bare name dropping two siblings is independent of a
	// disambiguated entry — combining both still produces zero edges.
	// Confirms BaseName-match short-circuits Name-match cleanly.
	in = testInput(multiFKAssetSchema())
	parser.DetectRelationships(in.Schema)
	in.Config.Tables["stakeholder"] = config.TableConfig{
		ExcludeRelationships: []string{"asset", "owner_assets"},
	}
	if got := relationshipNames(stakeholderRelationships(t, in)); len(got) != 0 {
		t.Errorf("mixed exclude: got %v relationships, want none", got)
	}
}

// TestBuildRelationshipContexts_SliceFieldsArePlural pins that
// auto-detected slice-shaped relationships (O2M / M2M) emit plural
// identifiers regardless of whether the source table is singular or
// already plural — the bug was that schemas using singular table names
// (e.g. `post`, `tag`) produced singular field names paired with slice
// types: `Post []*Post` instead of `Posts []*Post`. Pluralization is
// idempotent on already-plural words so plural-named schemas stay
// byte-stable.
func TestBuildRelationshipContexts_SliceFieldsArePlural(t *testing.T) {
	tests := []struct {
		name           string
		schema         *parser.Schema
		parentTable    string
		wantO2MField   string // for `Posts []*Post` style — empty when not exercised
		wantO2MJSONTag string
		wantM2MField   string // for `Tags []*Tag` style — empty when not exercised
		wantM2MJSONTag string
	}{
		{
			name: "singular tables yield plural slice fields",
			schema: &parser.Schema{
				Tables: []parser.Table{
					{
						Name: "user",
						Columns: []parser.Column{
							{Name: "id", Type: "uuid", PrimaryKey: true},
						},
					},
					{
						Name: "post",
						Columns: []parser.Column{
							{Name: "id", Type: "uuid", PrimaryKey: true},
							{Name: "user_id", Type: "uuid", FKReference: &parser.FKReference{Table: "user", Column: "id"}},
						},
					},
					{
						Name: "tag",
						Columns: []parser.Column{
							{Name: "id", Type: "uuid", PrimaryKey: true},
						},
					},
					{
						Name: "post_tag",
						Columns: []parser.Column{
							{Name: "post_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "post", Column: "id"}},
							{Name: "tag_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "tag", Column: "id"}},
						},
						// Composite PK is what the M2M detector keys off
						// (junctionConstraint in parser/relationship.go) — without
						// the table-level Constraint entry the parser falls back
						// to two separate O2M edges instead of one M2M.
						Constraints: []parser.Constraint{
							{Type: parser.PrimaryKey, Columns: []string{"post_id", "tag_id"}},
						},
					},
				},
			},
			parentTable:    "post",
			wantO2MField:   "", // post has no O2M children itself
			wantM2MField:   "Tags",
			wantM2MJSONTag: "tags",
		},
		{
			name: "plural tables stay byte-stable",
			schema: &parser.Schema{
				Tables: []parser.Table{
					{
						Name: "users",
						Columns: []parser.Column{
							{Name: "id", Type: "uuid", PrimaryKey: true},
						},
					},
					{
						Name: "posts",
						Columns: []parser.Column{
							{Name: "id", Type: "uuid", PrimaryKey: true},
							{Name: "user_id", Type: "uuid", FKReference: &parser.FKReference{Table: "users", Column: "id"}},
						},
					},
				},
			},
			parentTable:    "users",
			wantO2MField:   "Posts",
			wantO2MJSONTag: "posts",
		},
		{
			name: "singular O2M parent yields plural child field",
			schema: &parser.Schema{
				Tables: []parser.Table{
					{
						Name: "user",
						Columns: []parser.Column{
							{Name: "id", Type: "uuid", PrimaryKey: true},
						},
					},
					{
						Name: "post",
						Columns: []parser.Column{
							{Name: "id", Type: "uuid", PrimaryKey: true},
							{Name: "user_id", Type: "uuid", FKReference: &parser.FKReference{Table: "user", Column: "id"}},
						},
					},
				},
			},
			parentTable:    "user",
			wantO2MField:   "Posts",
			wantO2MJSONTag: "posts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := testInput(tt.schema)
			parser.DetectRelationships(in.Schema)
			contexts, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts: %v", err)
			}

			var rels []gen.RelationshipContext
			for _, c := range contexts {
				if c.TableName == tt.parentTable {
					rels = c.Relationships
					break
				}
			}
			if rels == nil {
				t.Fatalf("no context emitted for parent table %q", tt.parentTable)
			}

			byField := make(map[string]gen.RelationshipContext, len(rels))
			for _, r := range rels {
				byField[r.FieldName] = r
			}

			if tt.wantO2MField != "" {
				r, ok := byField[tt.wantO2MField]
				if !ok {
					t.Fatalf("no O2M field named %q on %s; got fields %v", tt.wantO2MField, tt.parentTable, fieldNamesOf(rels))
				}
				if r.Type != parser.OneToMany {
					t.Errorf("%s.%s type = %v, want OneToMany", tt.parentTable, tt.wantO2MField, r.Type)
				}
				if r.JSONTag != tt.wantO2MJSONTag {
					t.Errorf("%s.%s JSONTag = %q, want %q", tt.parentTable, tt.wantO2MField, r.JSONTag, tt.wantO2MJSONTag)
				}
				if r.Name != tt.wantO2MJSONTag {
					t.Errorf("%s.%s Name = %q, want %q (plural snake)", tt.parentTable, tt.wantO2MField, r.Name, tt.wantO2MJSONTag)
				}
			}
			if tt.wantM2MField != "" {
				r, ok := byField[tt.wantM2MField]
				if !ok {
					t.Fatalf("no M2M field named %q on %s; got fields %v", tt.wantM2MField, tt.parentTable, fieldNamesOf(rels))
				}
				if r.Type != parser.ManyToMany {
					t.Errorf("%s.%s type = %v, want ManyToMany", tt.parentTable, tt.wantM2MField, r.Type)
				}
				if r.JSONTag != tt.wantM2MJSONTag {
					t.Errorf("%s.%s JSONTag = %q, want %q", tt.parentTable, tt.wantM2MField, r.JSONTag, tt.wantM2MJSONTag)
				}
			}
		})
	}
}

func fieldNamesOf(rels []gen.RelationshipContext) []string {
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, r.FieldName)
	}
	return out
}

// declaredEntriesSchema is the two-table shape for the declared-vs-inferred
// collision: an FK from entries to accounts, so DetectRelationships infers an
// o2m edge on accounts named after the child table ("entries" → Go field
// "Entries") — exactly the name a consumer declaring the same relationship by
// hand would reach for.
func declaredEntriesSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "accounts",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name: "entries",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "account_id", Type: "uuid", FKReference: &parser.FKReference{Table: "accounts", Column: "id"}},
				},
			},
		},
	}
}

// accountsRelationships builds the contexts and returns the accounts table's
// relationship set alongside the notices raised while building it.
func accountsRelationships(t *testing.T, in *gen.GenerateInput) ([]gen.RelationshipContext, []string) {
	t.Helper()
	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	for i := range contexts {
		if contexts[i].TableName == "accounts" {
			return contexts[i].Relationships, in.Warnings
		}
	}
	t.Fatal("accounts context not found")
	return nil, nil
}

func relationshipFieldNames(rels []gen.RelationshipContext) []string {
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, r.FieldName)
	}
	return out
}

// TestBuildRelationshipContexts_DeclaredReplacesInferred pins the
// PRD §4.8 / §13.4 rule that a manually declared relationship replaces the
// auto-detected one it collides with. Appending both edges instead
// emits `Entries []*Entry` twice on the same struct, so the models package
// does not compile — while `sqlgen validate` and `sqlgen generate` both report
// success.
//
// Collision is keyed on the resolved Go FieldName, not Name: a declared name
// is PascalCase as written ("Entries") and an inferred one is the pluralized
// snake form ("entries"), so nothing matches until relationshipFieldAndType
// has run on both.
func TestBuildRelationshipContexts_DeclaredReplacesInferred(t *testing.T) {
	tests := []struct {
		name           string
		tableCfg       config.TableConfig
		wantFieldNames []string
		wantNames      []string
		wantWarning    bool
	}{
		{
			// Baseline: the inferred edge alone, no config at all.
			name:           "inferred edge only",
			tableCfg:       config.TableConfig{},
			wantFieldNames: []string{"Entries"},
			wantNames:      []string{"entries"},
		},
		{
			// The reported config. The declared entry wins and the inferred
			// one is dropped, so the field is emitted once — surviving Name is
			// the config's "Entries", not the inferred "entries".
			name: "declared name collides with inferred edge",
			tableCfg: config.TableConfig{
				Relationships: []config.TableRelationship{
					{Name: "Entries", Type: "one_to_many", Table: "entries", FK: "account_id"},
				},
			},
			wantFieldNames: []string{"Entries"},
			wantNames:      []string{"Entries"},
			wantWarning:    true,
		},
		{
			// The explicit form of the same intent. exclude_relationships
			// drops the inferred edge before the merge, so there is nothing
			// left to replace and no notice is raised.
			name: "exclude_relationships alongside the declaration",
			tableCfg: config.TableConfig{
				ExcludeRelationships: []string{"entries"},
				Relationships: []config.TableRelationship{
					{Name: "Entries", Type: "one_to_many", Table: "entries", FK: "account_id"},
				},
			},
			wantFieldNames: []string{"Entries"},
			wantNames:      []string{"Entries"},
		},
		{
			// A declared name that resolves to a different Go field is not a
			// collision — both edges coexist, which is the pre-existing
			// sub-categorized-relationship behavior (§13.7).
			name: "declared name distinct from inferred edge",
			tableCfg: config.TableConfig{
				Relationships: []config.TableRelationship{
					{Name: "LedgerEntries", Type: "one_to_many", Table: "entries", FK: "account_id"},
				},
			},
			wantFieldNames: []string{"Entries", "LedgerEntries"},
			wantNames:      []string{"LedgerEntries", "entries"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := testInput(declaredEntriesSchema())
			parser.DetectRelationships(in.Schema)
			in.Config.Tables["accounts"] = tt.tableCfg

			rels, warnings := accountsRelationships(t, in)

			gotFields := relationshipFieldNames(rels)
			slices.Sort(gotFields)
			if diff := cmp.Diff(tt.wantFieldNames, gotFields); diff != "" {
				t.Errorf("relationship field names (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantNames, relationshipNames(rels)); diff != "" {
				t.Errorf("relationship names (-want +got):\n%s", diff)
			}

			if !tt.wantWarning {
				if len(warnings) != 0 {
					t.Errorf("warnings = %v, want none", warnings)
				}
				return
			}
			if len(warnings) != 1 {
				t.Fatalf("warnings = %v, want exactly one", warnings)
			}
			// The notice has to name both sides and the field they claim,
			// otherwise a silently dropped inferred edge is untraceable.
			for _, want := range []string{`"Entries"`, `"entries"`, "tables.accounts.relationships", "§13.4"} {
				if !strings.Contains(warnings[0], want) {
					t.Errorf("warning %q does not mention %s", warnings[0], want)
				}
			}
		})
	}
}

// TestValidateResolvedFieldNames_DuplicateRelationshipField pins the
// §13.7.3 row 2 rule that nothing enforced: two relationships on one table
// resolving to the same Go field. config.validateRelationships compares
// `name:` byte-equal, so distinct names that pascalize onto one field pass it
// and both reach the struct — the duplicate-field shape on the declared-vs-declared
// axis, where replacement does not apply because neither side is inferred.
//
// The check is ungated, unlike validateDuplicateFieldNames: the pair can never
// produce compiling code, so a `column_map` override gate would only delay the
// report to `go build` inside generated code.
func TestValidateResolvedFieldNames_DuplicateRelationshipField(t *testing.T) {
	tests := []struct {
		name        string
		rels        []config.TableRelationship
		wantErr     bool
		wantNames   []string // both sides the error must name
		wantField   string
		wantWarning int
	}{
		{
			// Distinct names that pascalize onto one field. Different filters
			// keep them past the §13.7.3 dedup key, and `entries` is not the
			// inferred edge's Go field spelling, so neither is a duplicate as
			// far as config validation is concerned.
			name: "declared names pascalize onto one field",
			rels: []config.TableRelationship{
				{Name: "ledger_entries", Type: "one_to_many", Table: "entries", FK: "account_id", Filter: "kind = 'ledger'"},
				{Name: "LedgerEntries", Type: "one_to_many", Table: "entries", FK: "account_id", Filter: "kind = 'audit'"},
			},
			wantErr:   true,
			wantNames: []string{"ledger_entries", "LedgerEntries"},
			wantField: "LedgerEntries",
		},
		{
			// Exercises the !replaced guard in mergeDeclaredRelationships: the
			// first declared entry replaces the inferred edge (one warning),
			// the second finds the index entry already consumed and raises no
			// second notice — and the surviving pair is then rejected.
			name: "two declared entries both colliding with the inferred edge",
			rels: []config.TableRelationship{
				{Name: "Entries", Type: "one_to_many", Table: "entries", FK: "account_id", Filter: "kind = 'ledger'"},
				{Name: "entries", Type: "one_to_many", Table: "entries", FK: "account_id", Filter: "kind = 'audit'"},
			},
			wantErr:     true,
			wantNames:   []string{"Entries", "entries"},
			wantField:   "Entries",
			wantWarning: 1,
		},
		{
			// §13.7 sub-categorization still works — distinct Go fields.
			name: "distinct Go fields coexist",
			rels: []config.TableRelationship{
				{Name: "LedgerEntries", Type: "one_to_many", Table: "entries", FK: "account_id", Filter: "kind = 'ledger'"},
				{Name: "AuditEntries", Type: "one_to_many", Table: "entries", FK: "account_id", Filter: "kind = 'audit'"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := testInput(declaredEntriesSchema())
			parser.DetectRelationships(in.Schema)
			in.Config.Tables["accounts"] = config.TableConfig{Relationships: tt.rels}

			_, err := gen.BuildTableContexts(in, nil)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("BuildTableContexts: unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("BuildTableContexts: want duplicate-field error, got nil")
			}
			// Both sides and the claimed field must appear, otherwise the
			// error cannot be acted on without reading generated code.
			for _, want := range tt.wantNames {
				if !strings.Contains(err.Error(), `"`+want+`"`) {
					t.Errorf("error %q does not name relationship %q", err, want)
				}
			}
			if !strings.Contains(err.Error(), `"`+tt.wantField+`"`) {
				t.Errorf("error %q does not name the Go field %q", err, tt.wantField)
			}
			if !strings.Contains(err.Error(), "§13.7.3") {
				t.Errorf("error %q does not cite PRD §13.7.3", err)
			}
			if got := len(in.Warnings); got != tt.wantWarning {
				t.Errorf("warnings = %d (%v), want %d", got, in.Warnings, tt.wantWarning)
			}
		})
	}
}
