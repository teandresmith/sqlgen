package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// relTargetSchema: users has an FK-inferred O2M into posts (generated), one
// into notes (no primary key, so skipped by §9.4b), and an M2M into tags
// through the user_tags junction, plus a view a declared relationship can
// point at.
func relTargetSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Schema: "public", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{Name: "posts", Schema: "public", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "user_id", Type: "uuid"},
			}},
			{Name: "notes", Schema: "public", Columns: []parser.Column{
				{Name: "user_id", Type: "uuid"},
				{Name: "body", Type: "text"},
			}},
			{Name: "tags", Schema: "public", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{Name: "user_tags", Schema: "public", Columns: []parser.Column{
				{Name: "user_id", Type: "uuid", PrimaryKey: true},
				{Name: "tag_id", Type: "uuid", PrimaryKey: true},
			}},
		},
		Views: []parser.View{
			{Name: "user_stats", Schema: "public", Columns: []parser.Column{
				{Name: "user_id", Type: "uuid"},
				{Name: "post_count", Type: "bigint"},
			}},
		},
		Relationships: []parser.Relationship{
			{Name: "posts", SourceTable: "public.users", TargetTable: "public.posts", Type: parser.OneToMany, Key: []parser.KeyPair{{Local: "user_id", Referenced: "id"}}},
			{Name: "notes", SourceTable: "public.users", TargetTable: "public.notes", Type: parser.OneToMany, Key: []parser.KeyPair{{Local: "user_id", Referenced: "id"}}},
			{
				Name: "tags", SourceTable: "public.users", TargetTable: "public.tags", Type: parser.ManyToMany,
				JunctionTable: "public.user_tags", JunctionLocal: []parser.KeyPair{{Local: "user_id", Referenced: "id"}}, JunctionReference: []parser.KeyPair{{Local: "tag_id", Referenced: "id"}},
			},
		},
	}
}

func buildRelTargetUsers(t *testing.T, mutate func(*config.RootConfig)) (gen.TableContext, error) {
	t.Helper()
	input := testInput(relTargetSchema())
	if mutate != nil {
		mutate(input.Config)
	}
	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		return gen.TableContext{}, err
	}
	for _, tc := range contexts {
		if tc.TableName == "users" {
			return tc, nil
		}
	}
	t.Fatal("users context not built")
	return gen.TableContext{}, nil
}

func relByField(tc gen.TableContext) map[string]gen.RelationshipContext {
	out := make(map[string]gen.RelationshipContext, len(tc.Relationships))
	for _, r := range tc.Relationships {
		out[r.FieldName] = r
	}
	return out
}

// TestRelationshipTargets_StructNameOverride pins that a relationship's type is
// the struct name its target generates under, so a `struct_name` override on
// the target reaches the parent's field — for an FK-inferred edge into a table
// and for a declared edge into a view.
func TestRelationshipTargets_StructNameOverride(t *testing.T) {
	users, err := buildRelTargetUsers(t, func(cfg *config.RootConfig) {
		cfg.Tables["posts"] = config.TableConfig{StructName: "Article"}
		cfg.Views["user_stats"] = config.ViewConfig{StructName: "Stat"}
		cfg.Tables["users"] = config.TableConfig{Relationships: []config.TableRelationship{
			{Name: "Stats", Type: "one_to_many", Table: "user_stats", FK: config.KeyColumns{"user_id"}},
		}}
	})
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}
	rels := relByField(users)
	for field, want := range map[string]string{"Posts": "Article", "Stats": "Stat"} {
		r, ok := rels[field]
		if !ok {
			t.Errorf("BuildTableContexts(): users has no relationship %s; have %v", field, rels)
			continue
		}
		if r.TargetStructName != want || r.GoType != "[]*"+want {
			t.Errorf("BuildTableContexts(): users.%s TargetStructName = %q, GoType = %q; want %q, []*%s", field, r.TargetStructName, r.GoType, want, want)
		}
	}
}

// TestRelationshipTargets_InferredEdgeIntoSkippedTableDropped pins §9.4b's
// "other tables are unaffected": an FK-inferred edge into a table that
// generates nothing — no primary key, or excluded — is omitted, not emitted as
// a field typed as a struct that does not exist. An excluded
// junction is not a target: the M2M reads through it by name and types no
// field as it, so the edge stays (§6.4).
func TestRelationshipTargets_InferredEdgeIntoSkippedTableDropped(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*config.RootConfig)
		wantKept []string
	}{
		{"no primary key", nil, []string{"Posts", "Tags"}},
		{"excluded", func(cfg *config.RootConfig) { cfg.ExcludeTables = []string{"posts"} }, []string{"Tags"}},
		{"junction excluded", func(cfg *config.RootConfig) { cfg.ExcludeTables = []string{"user_tags"} }, []string{"Posts", "Tags"}},
		{"primary key declared", func(cfg *config.RootConfig) {
			cfg.Tables["notes"] = config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"user_id"}}}
		}, []string{"Notes", "Posts", "Tags"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users, err := buildRelTargetUsers(t, tt.mutate)
			if err != nil {
				t.Fatalf("BuildTableContexts() error: %v", err)
			}
			var got []string
			for _, r := range users.Relationships {
				got = append(got, r.FieldName)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantKept, ",") {
				t.Errorf("BuildTableContexts(): users relationships = %v, want %v", got, tt.wantKept)
			}
		})
	}
}

// TestRelationshipTargets_DeclaredEdgeIntoMissingTargetErrors pins the other
// half: a declared edge was asked for by name, so a target that generates
// nothing is a hard error naming the edge, not a silent omission (§4.13).
func TestRelationshipTargets_DeclaredEdgeIntoMissingTargetErrors(t *testing.T) {
	tests := []struct {
		name    string
		table   string
		exclude []string
		wantErr string
	}{
		{"no primary key", "notes", nil, `"Linked" targets "notes", which generates no entity`},
		{"excluded", "posts", []string{"posts"}, `"Linked" targets "posts", which generates no entity`},
		{"not in schema", "ghosts", nil, `"Linked" targets "ghosts", which generates no entity`},
		{"schema-qualified, not in that schema", "audit.posts", nil, `"Linked" targets "audit.posts", which generates no entity`},
		{"schema-qualified, excluded", "public.posts", []string{"posts"}, `"Linked" targets "public.posts", which generates no entity`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildRelTargetUsers(t, func(cfg *config.RootConfig) {
				cfg.ExcludeTables = tt.exclude
				cfg.Tables["users"] = config.TableConfig{Relationships: []config.TableRelationship{
					{Name: "Linked", Type: "one_to_many", Table: tt.table, FK: config.KeyColumns{"user_id"}},
				}}
			})
			// The error names the table, the relationship, the target and the three causes.
			for _, want := range []string{
				"tables.public.users.relationships: " + tt.wantErr,
				"it is excluded by `exclude_tables`, has no primary key, or is not in the schema",
			} {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Errorf("BuildTableContexts() error = %v, want it to contain %q", err, want)
				}
			}
		})
	}
}

// TestRelationshipTargets_DeclaredEdgeIntoAmbiguousNameErrors pins PRD §5.5's
// ambiguity rule for a relationship reference: a bare `table:` two schemas both
// declare is rejected, with the qualified spelling as the remedy, rather than
// resolved to one of them. That holds when one of the two generates nothing,
// which is why ambiguity counts parsed entities, not generated ones.
func TestRelationshipTargets_DeclaredEdgeIntoAmbiguousNameErrors(t *testing.T) {
	tests := []struct {
		name    string
		exclude []string
	}{
		{"both generate", nil},
		{"one excluded", []string{"public.posts"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := relTargetSchema()
			schema.Tables = append(schema.Tables, parser.Table{Name: "posts", Schema: "audit", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "user_id", Type: "uuid"},
			}})
			input := testInput(schema)
			input.Config.ExcludeTables = tt.exclude
			input.Config.Tables["users"] = config.TableConfig{Relationships: []config.TableRelationship{
				{Name: "Linked", Type: "one_to_many", Table: "posts", FK: config.KeyColumns{"user_id"}},
			}}
			_, err := gen.BuildTableContexts(input, map[string]bool{"posts": true})
			want := `"Linked" targets "posts", which names an entity in more than one schema (public, audit); qualify it as one of "public.posts", "audit.posts"`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("BuildTableContexts() error = %v, want it to contain %q", err, want)
			}
		})
	}
}

// twoSchemaSchema declares posts, profiles and tags in both `public` and
// `audit`, each pair with different columns, and lists `public` first — so a
// lookup that drops the schema and takes the first bare match lands on the
// wrong table for every `audit` edge. The audit tables differ exactly where a
// consumer reads the target: audit.profiles carries its FK as owner_id
// (fkColumnOnTarget), and audit.tags is keyed by a text `code`, not a uuid `id`
// (the M2M FK metadata).
func twoSchemaSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Schema: "public", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{Name: "posts", Schema: "public", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "user_id", Type: "uuid"},
				{Name: "title", Type: "text"},
			}},
			{Name: "profiles", Schema: "public", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "user_id", Type: "uuid"},
			}},
			{Name: "tags", Schema: "public", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{Name: "posts", Schema: "audit", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "user_id", Type: "uuid"},
				{Name: "note", Type: "text"},
			}},
			{Name: "profiles", Schema: "audit", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "owner_id", Type: "uuid"},
			}},
			{Name: "tags", Schema: "audit", Columns: []parser.Column{{Name: "code", Type: "text", PrimaryKey: true}}},
			{Name: "user_tags", Schema: "audit", Columns: []parser.Column{
				{Name: "user_id", Type: "uuid", PrimaryKey: true},
				{Name: "tag_code", Type: "text", PrimaryKey: true},
			}},
			{Name: "stats", Schema: "public", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "user_id", Type: "uuid"},
			}},
		},
		// audit.stats is a view beside the public.stats table: nothing may
		// resolve an edge into the view to the same-named table.
		Views: []parser.View{
			{Name: "stats", Schema: "audit", Columns: []parser.Column{
				{Name: "user_id", Type: "uuid"},
				{Name: "total", Type: "bigint"},
			}},
		},
	}
}

// TestRelationshipTargets_QualifiedDeclaredEdgeNamesOneTable pins that a
// schema-qualified `table:` resolves, and every consumer of the edge names the
// table the field's struct describes — the context's (TargetSchema,
// TargetTable), the FK placement, the O2O join, the M2M target-PK wiring and
// the relationship filter. The target struct and the SQL naming different
// same-named tables is the defect; golden files cannot show it, so each site
// is asserted here and the runtime half is an E2E test.
func TestRelationshipTargets_QualifiedDeclaredEdgeNamesOneTable(t *testing.T) {
	input := testInput(twoSchemaSchema())
	cfg := input.Config
	cfg.Tables["public.users"] = config.TableConfig{Relationships: []config.TableRelationship{
		{Name: "AuditPosts", Type: "one_to_many", Table: "audit.posts", FK: config.KeyColumns{"user_id"}},
		{Name: "Posts", Type: "one_to_many", Table: "public.posts", FK: config.KeyColumns{"user_id"}},
		{Name: "AuditProfile", Type: "one_to_one", Table: "audit.profiles", FK: config.KeyColumns{"owner_id"}},
		{
			Name: "AuditTags", Type: "many_to_many", Table: "audit.tags",
			Junction: "audit.user_tags", JunctionLocalFK: config.KeyColumns{"user_id"}, JunctionReferenceFK: config.KeyColumns{"tag_code"},
		},
		{Name: "AuditStats", Type: "one_to_many", Table: "audit.stats", FK: config.KeyColumns{"user_id"}},
	}}
	contexts, err := gen.BuildTableContextsFromSchema(input.Schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	i := slices.IndexFunc(contexts, func(tc gen.TableContext) bool { return tc.Schema == "public" && tc.TableName == "users" })
	if i < 0 {
		t.Fatal("BuildTableContextsFromSchema(): public.users context not built")
	}
	users := contexts[i]
	rels := relByField(users)

	tests := []struct {
		field                          string
		wantSchema, wantTable          string
		wantStruct                     string
		wantFKOnTarget                 *bool  // nil: not asserted (a view target is not in the parsed tables fkColumnOnTarget reads)
		wantTargetPKColumn, wantFKType string // M2M only
	}{
		{field: "AuditPosts", wantSchema: "audit", wantTable: "posts", wantStruct: "AuditPost", wantFKOnTarget: new(true)},
		{field: "Posts", wantSchema: "public", wantTable: "posts", wantStruct: "PublicPost", wantFKOnTarget: new(true)},
		{field: "AuditProfile", wantSchema: "audit", wantTable: "profiles", wantStruct: "AuditProfile", wantFKOnTarget: new(true)},
		{
			field: "AuditTags", wantSchema: "audit", wantTable: "tags", wantStruct: "AuditTag",
			wantTargetPKColumn: "code", wantFKType: "string",
		},
		{field: "AuditStats", wantSchema: "audit", wantTable: "stats", wantStruct: "AuditStat"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			r, ok := rels[tt.field]
			if !ok {
				t.Fatalf("users has no relationship %s; have %v", tt.field, rels)
			}
			if r.TargetSchema != tt.wantSchema || r.TargetTable != tt.wantTable || r.TargetStructName != tt.wantStruct {
				t.Errorf("users.%s target = (%q, %q, %q), want (%q, %q, %q)",
					tt.field, r.TargetSchema, r.TargetTable, r.TargetStructName, tt.wantSchema, tt.wantTable, tt.wantStruct)
			}
			if tt.wantFKOnTarget != nil && r.FKOnTarget != *tt.wantFKOnTarget {
				t.Errorf("users.%s FKOnTarget = %v, want %v", tt.field, r.FKOnTarget, *tt.wantFKOnTarget)
			}
			if tt.wantTargetPKColumn != "" && (r.TargetPKColumn != tt.wantTargetPKColumn || r.FKGoType != tt.wantFKType) {
				t.Errorf("users.%s TargetPKColumn = %q, FKGoType = %q; want %q, %q",
					tt.field, r.TargetPKColumn, r.FKGoType, tt.wantTargetPKColumn, tt.wantFKType)
			}
		})
	}

	t.Run("O2O join", func(t *testing.T) {
		if len(users.O2OJoinDetails) != 1 {
			t.Fatalf("users O2OJoinDetails = %+v, want one", users.O2OJoinDetails)
		}
		d := users.O2OJoinDetails[0]
		if d.TargetSchema != "audit" || d.TargetTable != "profiles" || d.StructName != "AuditProfile" {
			t.Errorf("O2O join target = (%q, %q, %q), want (audit, profiles, AuditProfile)", d.TargetSchema, d.TargetTable, d.StructName)
		}
		// The FK is on the target, so the join reads the target's owner_id.
		if d.OnRemote != "owner_id" || d.OnLocal != "id" {
			t.Errorf("O2O join ON remote.%s = local.%s, want remote.owner_id = local.id", d.OnRemote, d.OnLocal)
		}
	})

	t.Run("relationship filter", func(t *testing.T) {
		got := make(map[string]string, len(users.RelationshipFilters))
		for _, rf := range users.RelationshipFilters {
			got[rf.FieldName] = rf.TargetSchema + "." + rf.TargetTable + "/" + rf.TargetStructName
		}
		want := map[string]string{
			"AuditPosts": "audit.posts/AuditPost",
			"Posts":      "public.posts/PublicPost",
			"AuditTags":  "audit.tags/AuditTag",
			// The view's edge gets a member built from the view's own filter;
			// the same-named public.stats table must not stand in for it.
			"AuditStats": "audit.stats/AuditStat",
		}
		for field, w := range want {
			if got[field] != w {
				t.Errorf("relationship filter %s = %q, want %q", field, got[field], w)
			}
		}
	})
}

// TestRelationshipTargets_BareDeclaredEdgeCarriesResolvedSchema pins that a
// bare `table:` naming one entity carries that entity's schema, so the edge's
// lookups are exact rather than each falling back to its own bare-name rule.
func TestRelationshipTargets_BareDeclaredEdgeCarriesResolvedSchema(t *testing.T) {
	users, err := buildRelTargetUsers(t, func(cfg *config.RootConfig) {
		cfg.Tables["users"] = config.TableConfig{Relationships: []config.TableRelationship{
			{Name: "Linked", Type: "one_to_many", Table: "posts", FK: config.KeyColumns{"user_id"}},
		}}
	})
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}
	r, ok := relByField(users)["Linked"]
	if !ok {
		t.Fatal("BuildTableContexts(): users has no relationship Linked")
	}
	if r.TargetSchema != "public" || r.TargetTable != "posts" {
		t.Errorf("BuildTableContexts(): users.Linked target = (%q, %q), want (public, posts)", r.TargetSchema, r.TargetTable)
	}
}

// TestRelationshipTargets_DuplicateDeclaredEdgeBySpelling pins PRD §13.7.3's
// duplicate rule on the resolved target. config.ValidatePreParse compares
// `table:` as written, so a bare and a qualified spelling of one table pass it;
// the dedup has to be re-run once the target resolves, or the two entries
// generate two fields over the same edge. Two same-named tables in
// different schemas are different targets and stay legal.
func TestRelationshipTargets_DuplicateDeclaredEdgeBySpelling(t *testing.T) {
	disc := func(v string) *config.RelationshipDiscriminator {
		return &config.RelationshipDiscriminator{Column: "kind", Value: v}
	}
	tests := []struct {
		name    string
		schema  func() *parser.Schema
		rels    []config.TableRelationship
		wantErr string // "" for no error
	}{
		{
			name:   "o2m bare and qualified",
			schema: relTargetSchema,
			rels: []config.TableRelationship{
				{Name: "Linked", Type: "one_to_many", Table: "posts", FK: config.KeyColumns{"user_id"}},
				{Name: "LinkedAgain", Type: "one_to_many", Table: "public.posts", FK: config.KeyColumns{"user_id"}},
			},
			wantErr: `tables.public.users.relationships[1]: duplicate relationship — same target, fk, filter and discriminator as relationships[0] (PRD §13.7.3): "public.posts" and "posts" both name public.posts`,
		},
		{
			name:   "o2o and o2m with one discriminator",
			schema: relTargetSchema,
			rels: []config.TableRelationship{
				{Name: "Primary", Type: "one_to_one", Table: "public.posts", FK: config.KeyColumns{"user_id"}, Discriminator: disc("primary")},
				{Name: "PrimaryAgain", Type: "one_to_many", Table: "posts", FK: config.KeyColumns{"user_id"}, Discriminator: disc("primary")},
			},
			wantErr: `tables.public.users.relationships[1]: duplicate relationship — same target, fk, filter and discriminator as relationships[0] (PRD §13.7.3): "posts" and "public.posts" both name public.posts`,
		},
		{
			name:   "m2m bare and qualified",
			schema: relTargetSchema,
			rels: []config.TableRelationship{
				{Name: "Labels", Type: "many_to_many", Table: "tags", Junction: "user_tags", JunctionLocalFK: config.KeyColumns{"user_id"}, JunctionReferenceFK: config.KeyColumns{"tag_id"}},
				{Name: "LabelsAgain", Type: "many_to_many", Table: "public.tags", Junction: "user_tags", JunctionLocalFK: config.KeyColumns{"user_id"}, JunctionReferenceFK: config.KeyColumns{"tag_id"}},
			},
			wantErr: `tables.public.users.relationships[1]: duplicate relationship — same target, fk, filter and discriminator as relationships[0] (PRD §13.7.3): "public.tags" and "tags" both name public.tags`,
		},
		{
			// The junction is compared as the table it resolves to.
			name:   "m2m junction bare and qualified",
			schema: relTargetSchema,
			rels: []config.TableRelationship{
				{Name: "Labels", Type: "many_to_many", Table: "tags", Junction: "user_tags", JunctionLocalFK: config.KeyColumns{"user_id"}, JunctionReferenceFK: config.KeyColumns{"tag_id"}},
				{Name: "LabelsAgain", Type: "many_to_many", Table: "tags", Junction: "public.user_tags", JunctionLocalFK: config.KeyColumns{"user_id"}, JunctionReferenceFK: config.KeyColumns{"tag_id"}},
			},
			wantErr: `tables.public.users.relationships[1]: duplicate relationship — same target, fk, filter and discriminator as relationships[0] (PRD §13.7.3): "tags" and "tags" both name public.tags through junction public.user_tags`,
		},
		{
			name:   "different discriminator values sub-categorize",
			schema: relTargetSchema,
			rels: []config.TableRelationship{
				{Name: "Primary", Type: "one_to_one", Table: "public.posts", FK: config.KeyColumns{"user_id"}, Discriminator: disc("primary")},
				{Name: "Secondary", Type: "one_to_one", Table: "posts", FK: config.KeyColumns{"user_id"}, Discriminator: disc("secondary")},
			},
		},
		{
			name:   "same name in two schemas is two targets",
			schema: twoSchemaSchema,
			rels: []config.TableRelationship{
				{Name: "Linked", Type: "one_to_many", Table: "public.posts", FK: config.KeyColumns{"user_id"}},
				{Name: "AuditLinked", Type: "one_to_many", Table: "audit.posts", FK: config.KeyColumns{"user_id"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testInput(tt.schema())
			input.Config.Tables["public.users"] = config.TableConfig{Relationships: tt.rels}
			_, err := gen.BuildTableContexts(input, map[string]bool{"posts": true, "tags": true})
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("BuildTableContexts() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("BuildTableContexts() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestRelationshipTargets_ChainedO2OIntoExcludedTableDropped pins the second
// omission site: the O2O join builder walks a target's own O2O edges
// (findO2ORelationships), and an FK-inferred one into an excluded table must be
// dropped there too, or the chained join is emitted typed as a struct that does
// not exist. A PK-less target is caught earlier, by the join builder's own PK
// check, so the excluded shape is the one that reaches this guard. A chained
// edge whose FK column `exclude_columns` removes is dropped there too, since
// the target's struct has no field for it.
func TestRelationshipTargets_ChainedO2OIntoExcludedTableDropped(t *testing.T) {
	schema := func() *parser.Schema {
		return &parser.Schema{
			Tables: []parser.Table{
				{Name: "profiles", Schema: "public", Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid"},
				}},
				{Name: "users", Schema: "public", Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "account_id", Type: "uuid"},
				}},
				{Name: "accounts", Schema: "public", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			},
			Relationships: []parser.Relationship{
				{Name: "users", SourceTable: "public.profiles", TargetTable: "public.users", Type: parser.OneToOne, Key: []parser.KeyPair{{Local: "user_id", Referenced: "id"}}},
				{Name: "accounts", SourceTable: "public.users", TargetTable: "public.accounts", Type: parser.OneToOne, Key: []parser.KeyPair{{Local: "account_id", Referenced: "id"}}},
			},
		}
	}
	tests := []struct {
		name        string
		exclude     []string
		excludeCols []string
		wantChained []string
	}{
		{"target generates", nil, nil, []string{"Account"}},
		{"target excluded", []string{"accounts"}, nil, nil},
		{"fk column excluded", nil, []string{"account_id"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testInput(schema())
			input.Config.ExcludeTables = tt.exclude
			input.Config.Tables["users"] = config.TableConfig{ExcludeColumns: tt.excludeCols}
			contexts, err := gen.BuildTableContexts(input, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts() error: %v", err)
			}
			i := slices.IndexFunc(contexts, func(tc gen.TableContext) bool { return tc.TableName == "profiles" })
			if i < 0 || len(contexts[i].O2OJoinDetails) != 1 {
				t.Fatalf("BuildTableContexts(): want profiles with one O2O join detail, got %+v", contexts)
			}
			chained := contexts[i].O2OJoinDetails[0].ChainedJoins
			got := make([]string, 0, len(chained))
			for _, c := range chained {
				got = append(got, c.StructName)
			}
			if !slices.Equal(got, tt.wantChained) {
				t.Errorf("BuildTableContexts(): profiles → users chained joins = %q, want %q", got, tt.wantChained)
			}
		})
	}
}

// TestRelationshipTargets_DeclaredJunctionAmbiguousErrors pins PRD §5.5's
// ambiguity rule for a relationship's `junction:`: a bare junction
// two schemas both declare is rejected with the qualified spellings as the
// remedy, rather than read through the search path by the loader and the
// filter while the nested link step writes another schema's table. Like a
// `table:`, the count takes in a table that does not generate and a view.
func TestRelationshipTargets_DeclaredJunctionAmbiguousErrors(t *testing.T) {
	publicUserTags := parser.Table{Name: "user_tags", Schema: "public", Columns: []parser.Column{
		{Name: "user_id", Type: "uuid", PrimaryKey: true},
		{Name: "tag_code", Type: "text", PrimaryKey: true},
	}}
	tests := []struct {
		name    string
		mutate  func(*parser.Schema, *config.RootConfig)
		junc    string
		wantErr string // "" for no error
	}{
		{
			name:    "both generate",
			mutate:  func(s *parser.Schema, _ *config.RootConfig) { s.Tables = append(s.Tables, publicUserTags) },
			junc:    "user_tags",
			wantErr: `tables.public.users.relationships: "AuditTags" reads through junction "user_tags", which names an entity in more than one schema (audit, public); qualify it as one of "audit.user_tags", "public.user_tags" (PRD §5.5)`,
		},
		{
			name: "one excluded",
			mutate: func(s *parser.Schema, cfg *config.RootConfig) {
				s.Tables = append(s.Tables, publicUserTags)
				cfg.ExcludeTables = []string{"public.user_tags"}
			},
			junc:    "user_tags",
			wantErr: `"AuditTags" reads through junction "user_tags", which names an entity in more than one schema (audit, public)`,
		},
		{
			name: "a view in the other schema",
			mutate: func(s *parser.Schema, _ *config.RootConfig) {
				s.Views = append(s.Views, parser.View{Name: "user_tags", Schema: "public", Columns: publicUserTags.Columns})
			},
			junc:    "user_tags",
			wantErr: `"AuditTags" reads through junction "user_tags", which names an entity in more than one schema (audit, public)`,
		},
		{
			name:   "qualified",
			mutate: func(s *parser.Schema, _ *config.RootConfig) { s.Tables = append(s.Tables, publicUserTags) },
			junc:   "audit.user_tags",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testInput(twoSchemaSchema())
			tt.mutate(input.Schema, input.Config)
			input.Config.Tables["public.users"] = config.TableConfig{Relationships: []config.TableRelationship{{
				Name: "AuditTags", Type: "many_to_many", Table: "audit.tags",
				Junction: tt.junc, JunctionLocalFK: config.KeyColumns{"user_id"}, JunctionReferenceFK: config.KeyColumns{"tag_code"},
			}}}
			_, err := gen.BuildTableContextsFromSchema(input.Schema, input.Config)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("BuildTableContextsFromSchema() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("BuildTableContextsFromSchema() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestRelationshipTargets_BareDeclaredJunctionNamesOneTable pins that a bare
// `junction:` naming one table carries that table's schema to every consumer:
// the context the M2M loader renders from, the relationship
// filter's EXISTS subquery, and the nested link step's junction client. The
// junction lives only in `audit` while the parent is in `public`, so a junction
// carried without its schema renders as "user_tags", which PostgreSQL's
// search path resolves to a public table that does not exist.
func TestRelationshipTargets_BareDeclaredJunctionNamesOneTable(t *testing.T) {
	input := testInput(twoSchemaSchema())
	cfg := input.Config
	cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
	cfg.Tables["public.users"] = config.TableConfig{Relationships: []config.TableRelationship{{
		Name: "AuditTags", Type: "many_to_many", Table: "audit.tags",
		Junction: "user_tags", JunctionLocalFK: config.KeyColumns{"user_id"}, JunctionReferenceFK: config.KeyColumns{"tag_code"},
	}}}
	contexts, err := gen.BuildTableContextsFromSchema(input.Schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	i := slices.IndexFunc(contexts, func(tc gen.TableContext) bool { return tc.Schema == "public" && tc.TableName == "users" })
	if i < 0 {
		t.Fatal("BuildTableContextsFromSchema(): public.users context not built")
	}
	users := contexts[i]

	r, ok := relByField(users)["AuditTags"]
	if !ok {
		t.Fatal("users has no relationship AuditTags")
	}
	if r.JunctionSchema != "audit" || r.JunctionTable != "user_tags" {
		t.Errorf("users.AuditTags junction = (%q, %q), want (audit, user_tags)", r.JunctionSchema, r.JunctionTable)
	}
	j := slices.IndexFunc(users.RelationshipFilters, func(rf gen.RelationshipFilterContext) bool { return rf.FieldName == "AuditTags" })
	if j < 0 {
		t.Fatal("users has no relationship filter AuditTags")
	}
	if got := users.RelationshipFilters[j].JunctionSchema; got != "audit" {
		t.Errorf("relationship filter AuditTags JunctionSchema = %q, want audit", got)
	}
	if !slices.Contains(users.NestedWriteClients, "UserTag") {
		t.Errorf("users NestedWriteClients = %v, want it to contain UserTag", users.NestedWriteClients)
	}
}

// TestRelationshipTargets_UnknownDeclaredJunctionCarriedAsWritten pins the
// ruling that a bare `junction:` that no parsed table or view
// has is not an error, and the edge carries the name as written, with no
// schema. Making it an error would be a new §4.13 rule of its own.
func TestRelationshipTargets_UnknownDeclaredJunctionCarriedAsWritten(t *testing.T) {
	input := testInput(twoSchemaSchema())
	input.Config.Tables["public.users"] = config.TableConfig{Relationships: []config.TableRelationship{{
		Name: "GhostTags", Type: "many_to_many", Table: "audit.tags",
		Junction: "ghost_links", JunctionLocalFK: config.KeyColumns{"user_id"}, JunctionReferenceFK: config.KeyColumns{"tag_code"},
	}}}
	contexts, err := gen.BuildTableContextsFromSchema(input.Schema, input.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	i := slices.IndexFunc(contexts, func(tc gen.TableContext) bool { return tc.Schema == "public" && tc.TableName == "users" })
	if i < 0 {
		t.Fatal("BuildTableContextsFromSchema(): public.users context not built")
	}
	r, ok := relByField(contexts[i])["GhostTags"]
	if !ok {
		t.Fatal("users has no relationship GhostTags")
	}
	if r.JunctionSchema != "" || r.JunctionTable != "ghost_links" {
		t.Errorf("users.GhostTags junction = (%q, %q), want (\"\", ghost_links)", r.JunctionSchema, r.JunctionTable)
	}
}

// TestRelationshipTargets_QualifiedJunctionReadsOnlyItsSchema pins the exact
// column lookup behind the M2M FK metadata. An excluded junction
// keeps its edge (PRD §6.4) but has no table context, so a lookup that fell
// back from "public.user_tags" to the bare name read the same-named audit
// table instead — here, its nullable user_id.
func TestRelationshipTargets_QualifiedJunctionReadsOnlyItsSchema(t *testing.T) {
	schema := twoSchemaSchema()
	for i := range schema.Tables {
		if schema.Tables[i].Schema == "audit" && schema.Tables[i].Name == "user_tags" {
			schema.Tables[i].Columns = []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "user_id", Type: "uuid", Nullable: true},
				{Name: "tag_code", Type: "text"},
			}
		}
	}
	schema.Tables = append(schema.Tables, parser.Table{Name: "user_tags", Schema: "public", Columns: []parser.Column{
		{Name: "user_id", Type: "uuid", PrimaryKey: true},
		{Name: "tag_code", Type: "text", PrimaryKey: true},
	}})
	input := testInput(schema)
	input.Config.ExcludeTables = []string{"public.user_tags"}
	input.Config.Tables["public.users"] = config.TableConfig{Relationships: []config.TableRelationship{{
		Name: "AuditTags", Type: "many_to_many", Table: "audit.tags",
		Junction: "public.user_tags", JunctionLocalFK: config.KeyColumns{"user_id"}, JunctionReferenceFK: config.KeyColumns{"tag_code"},
	}}}
	contexts, err := gen.BuildTableContextsFromSchema(input.Schema, input.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	i := slices.IndexFunc(contexts, func(tc gen.TableContext) bool { return tc.Schema == "public" && tc.TableName == "users" })
	if i < 0 {
		t.Fatal("BuildTableContextsFromSchema(): public.users context not built")
	}
	r, ok := relByField(contexts[i])["AuditTags"]
	if !ok {
		t.Fatal("users has no relationship AuditTags")
	}
	if r.JunctionSchema != "public" || r.FKNullable {
		t.Errorf("users.AuditTags JunctionSchema = %q, FKNullable = %v; want public, false (audit.user_tags.user_id is the nullable one)", r.JunctionSchema, r.FKNullable)
	}
	// With the junction excluded, its key types fall back to the keys they
	// reference: the parent's PK for the local column, the target's for the
	// reference column. The loader scans the junction into them.
	if pk := contexts[i].PKColumns[0].GoType; r.FKColumnGoType != pk || r.JunctionReferenceGoType != "string" {
		t.Errorf("users.AuditTags FKColumnGoType = %q, JunctionReferenceGoType = %q; want %q, %q",
			r.FKColumnGoType, r.JunctionReferenceGoType, pk, "string")
	}
}

// TestRelationshipTargets_M2MJunctionReferenceGoType pins the junction
// reference column's own Go type on an M2M edge. The loader scans the junction
// into it, so a nullable reference column must resolve to its Null-wrapped
// type, not to the target PK's bare type.
func TestRelationshipTargets_M2MJunctionReferenceGoType(t *testing.T) {
	schema := &parser.Schema{Tables: []parser.Table{
		{Name: "users", Schema: "public", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
		{Name: "tags", Schema: "public", Columns: []parser.Column{{Name: "code", Type: "text", PrimaryKey: true}}},
		{Name: "user_tags", Schema: "public", Columns: []parser.Column{
			{Name: "id", Type: "bigint", PrimaryKey: true},
			{Name: "user_id", Type: "uuid"},
			{Name: "tag_code", Type: "text", Nullable: true},
		}},
	}}
	input := testInput(schema)
	input.Config.Tables = map[string]config.TableConfig{"users": {Relationships: []config.TableRelationship{{
		Name: "Tags", Type: "many_to_many", Table: "tags",
		Junction: "user_tags", JunctionLocalFK: config.KeyColumns{"user_id"}, JunctionReferenceFK: config.KeyColumns{"tag_code"},
	}}}}
	contexts, err := gen.BuildTableContextsFromSchema(input.Schema, input.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	i := slices.IndexFunc(contexts, func(tc gen.TableContext) bool { return tc.TableName == "users" })
	if i < 0 {
		t.Fatal("BuildTableContextsFromSchema(): users context not built")
	}
	r, ok := relByField(contexts[i])["Tags"]
	if !ok {
		t.Fatalf("users has no relationship Tags; have %v", relByField(contexts[i]))
	}
	if r.FKGoType != "string" || r.JunctionReferenceGoType != "*string" {
		t.Errorf("users.Tags FKGoType = %q, JunctionReferenceGoType = %q; want %q, %q",
			r.FKGoType, r.JunctionReferenceGoType, "string", "*string")
	}
}

// excludedMemberSchema: tasks → cycles is O2M, profiles → users is O2O, and
// posts ↔ tags is M2M through post_tags. Every FK references its parent's id.
func excludedMemberSchema() *parser.Schema {
	ref := func(table string) *parser.FKReference {
		return &parser.FKReference{Schema: "public", Table: table, Column: "id"}
	}
	return &parser.Schema{
		Tables: []parser.Table{
			{Name: "cycles", Schema: "public", Columns: []parser.Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "code", Type: "text", Unique: true},
			}},
			{Name: "tasks", Schema: "public", Columns: []parser.Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "cycle_id", Type: "integer", Nullable: true, FKReference: ref("cycles")},
			}},
			{Name: "users", Schema: "public", Columns: []parser.Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
			{Name: "profiles", Schema: "public", Columns: []parser.Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "user_id", Type: "integer", Unique: true, FKReference: ref("users")},
			}},
			{Name: "posts", Schema: "public", Columns: []parser.Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
			{Name: "tags", Schema: "public", Columns: []parser.Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
			{Name: "post_tags", Schema: "public", Columns: []parser.Column{
				{Name: "post_id", Type: "integer", PrimaryKey: true, FKReference: ref("posts")},
				{Name: "tag_id", Type: "integer", PrimaryKey: true, FKReference: ref("tags")},
			}},
		},
		Relationships: []parser.Relationship{
			{Name: "tasks", BaseName: "tasks", SourceTable: "public.cycles", TargetTable: "public.tasks", Type: parser.OneToMany, Key: []parser.KeyPair{{Local: "cycle_id", Referenced: "id"}}},
			{Name: "users", BaseName: "users", SourceTable: "public.profiles", TargetTable: "public.users", Type: parser.OneToOne, Key: []parser.KeyPair{{Local: "user_id", Referenced: "id"}}},
			{
				Name: "tags", BaseName: "tags", SourceTable: "public.posts", TargetTable: "public.tags", Type: parser.ManyToMany,
				JunctionTable: "public.post_tags", JunctionLocal: []parser.KeyPair{{Local: "post_id", Referenced: "id"}}, JunctionReference: []parser.KeyPair{{Local: "tag_id", Referenced: "id"}},
			},
			{
				Name: "posts", BaseName: "posts", SourceTable: "public.tags", TargetTable: "public.posts", Type: parser.ManyToMany,
				JunctionTable: "public.post_tags", JunctionLocal: []parser.KeyPair{{Local: "tag_id", Referenced: "id"}}, JunctionReference: []parser.KeyPair{{Local: "post_id", Referenced: "id"}},
			},
		},
	}
}

// omittedWarning is the warning an FK-inferred edge with an excluded key
// column is omitted with.
func omittedWarning(table, edge, column string) string {
	return "tables." + table + ": the foreign-key-inferred relationship \"" + edge +
		"\" is omitted because its key column " + column + " is removed by `exclude_columns`." +
		" Keep the column to keep the edge, or list \"" + edge + "\" in `exclude_relationships` (PRD §4.13, §13.4)."
}

// An FK-inferred edge whose FK column, junction column or referenced column
// `exclude_columns` removes is omitted with a warning naming the column. An
// emitted O2M loader and filter would name a field the target struct does not
// have, and the generated package would not compile.
func TestRelationshipTargets_ExcludedKeyMemberDropsInferredEdge(t *testing.T) {
	tests := []struct {
		name         string
		mutate       func(*config.RootConfig)
		wantFields   map[string][]string
		wantWarnings []string
	}{
		{
			name:       "nothing excluded",
			wantFields: map[string][]string{"cycles": {"Tasks"}, "profiles": {"Users"}, "posts": {"Tags"}, "tags": {"Posts"}},
		},
		{
			name: "o2m fk column",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["tasks"] = config.TableConfig{ExcludeColumns: []string{"cycle_id"}}
			},
			wantFields:   map[string][]string{"cycles": nil, "profiles": {"Users"}, "posts": {"Tags"}, "tags": {"Posts"}},
			wantWarnings: []string{omittedWarning("public.cycles", "tasks", "public.tasks.cycle_id")},
		},
		{
			name:         "o2o fk column, through generation.exclude_columns",
			mutate:       func(cfg *config.RootConfig) { cfg.Generation.ExcludeColumns = []string{"user_id"} },
			wantFields:   map[string][]string{"cycles": {"Tasks"}, "profiles": nil, "posts": {"Tags"}, "tags": {"Posts"}},
			wantWarnings: []string{omittedWarning("public.profiles", "users", "public.profiles.user_id")},
		},
		{
			name: "m2m junction column",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["post_tags"] = config.TableConfig{ExcludeColumns: []string{"tag_id"}}
			},
			wantFields: map[string][]string{"cycles": {"Tasks"}, "profiles": {"Users"}, "posts": nil, "tags": nil},
			wantWarnings: []string{
				omittedWarning("public.posts", "tags", "public.post_tags.tag_id"),
				omittedWarning("public.tags", "posts", "public.post_tags.tag_id"),
			},
		},
		{
			name: "referenced column",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["cycles"] = config.TableConfig{
					ExcludeColumns: []string{"id"},
					PrimaryKey:     &config.TablePrimaryKeyConfig{Columns: []string{"code"}},
				}
			},
			wantFields:   map[string][]string{"cycles": nil, "profiles": {"Users"}, "posts": {"Tags"}, "tags": {"Posts"}},
			wantWarnings: []string{omittedWarning("public.cycles", "tasks", "public.cycles.id")},
		},
		{
			name: "edge also listed in exclude_relationships",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["tasks"] = config.TableConfig{ExcludeColumns: []string{"cycle_id"}}
				cfg.Tables["cycles"] = config.TableConfig{ExcludeRelationships: []string{"tasks"}}
			},
			wantFields: map[string][]string{"cycles": nil, "profiles": {"Users"}, "posts": {"Tags"}, "tags": {"Posts"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testInput(excludedMemberSchema())
			if tt.mutate != nil {
				tt.mutate(input.Config)
			}
			contexts, err := gen.BuildTableContexts(input, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts() error: %v", err)
			}
			for table, want := range tt.wantFields {
				i := slices.IndexFunc(contexts, func(tc gen.TableContext) bool { return tc.TableName == table })
				if i < 0 {
					t.Fatalf("BuildTableContexts(): %s context not built", table)
				}
				var got []string
				for _, r := range contexts[i].Relationships {
					got = append(got, r.FieldName)
				}
				if !slices.Equal(got, want) {
					t.Errorf("BuildTableContexts(): %s relationships = %q, want %q", table, got, want)
				}
			}
			var warnings []string
			for _, w := range input.Warnings {
				if strings.Contains(w, "exclude_columns") {
					warnings = append(warnings, w)
				}
			}
			if !slices.Equal(warnings, tt.wantWarnings) {
				t.Errorf("BuildTableContexts() warnings = %q, want %q", warnings, tt.wantWarnings)
			}
		})
	}
}

// Every member of a composite key must be in the model on both sides: excluding
// one drops the edge with a warning naming it.
func TestRelationshipTargets_ExcludedCompositeKeyMemberDropsInferredEdge(t *testing.T) {
	const ddl = `
CREATE TABLE cycles (id bigint PRIMARY KEY, code text NOT NULL, UNIQUE (code, id));
CREATE TABLE tasks (id bigint PRIMARY KEY, cycle_code text, cycle_id bigint,
  FOREIGN KEY (cycle_code, cycle_id) REFERENCES cycles (code, id));`
	tests := []struct {
		name       string
		table      string
		column     string
		wantMember string
	}{
		{"composite key member on the child", "tasks", "cycle_code", "public.tasks.cycle_code"},
		{"composite key member on the parent", "cycles", "code", "public.cycles.code"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := parseDDLForTest(t, config.DialectPostgres, ddl)
			parser.DetectRelationships(schema)
			input := testInput(schema)
			input.Config.Tables[tt.table] = config.TableConfig{ExcludeColumns: []string{tt.column}}
			contexts, err := gen.BuildTableContexts(input, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts() error: %v", err)
			}
			for _, tc := range contexts {
				if tc.TableName == "cycles" && len(tc.Relationships) != 0 {
					t.Errorf("cycles relationships = %v, want none", tc.Relationships)
				}
			}
			if !slices.Contains(input.Warnings, omittedWarning("public.cycles", "tasks", tt.wantMember)) {
				t.Errorf("warnings = %q, want the omission naming %s", input.Warnings, tt.wantMember)
			}
		})
	}
}

// A declared relationship whose link column `exclude_columns` removes is an
// error naming the column, since the user asked for the edge by name (PRD
// §4.13).
func TestRelationshipTargets_DeclaredExcludedLinkColumnErrors(t *testing.T) {
	tests := []struct {
		name    string
		rel     config.TableRelationship
		exclude map[string][]string
		wantErr string // "" for no error
	}{
		{
			name:    "o2m fk on the related table",
			rel:     config.TableRelationship{Name: "Tasks", Type: "o2m", Table: "tasks", FK: config.KeyColumns{"cycle_id"}},
			exclude: map[string][]string{"tasks": {"cycle_id"}},
			wantErr: `tables.public.cycles.relationships: "Tasks" links through public.tasks.cycle_id, which ` + "`exclude_columns`" + ` removes from the generated model; keep the column or remove the relationship (PRD §4.13, §13.4)`,
		},
		{
			name:    "o2o fk on the related table",
			rel:     config.TableRelationship{Name: "LatestTask", Type: "o2o", Table: "tasks", FK: config.KeyColumns{"cycle_id"}},
			exclude: map[string][]string{"tasks": {"cycle_id"}},
			wantErr: `tables.public.cycles.relationships: "LatestTask" links through public.tasks.cycle_id, which ` + "`exclude_columns`" + ` removes`,
		},
		{
			name:    "o2m fk kept",
			rel:     config.TableRelationship{Name: "Tasks", Type: "o2m", Table: "tasks", FK: config.KeyColumns{"cycle_id"}},
			exclude: map[string][]string{"cycles": {"code"}},
		},
		{
			name: "m2m junction column",
			rel: config.TableRelationship{
				Name: "LinkedPosts", Type: "m2m", Table: "posts",
				Junction: "post_tags", JunctionLocalFK: config.KeyColumns{"tag_id"}, JunctionReferenceFK: config.KeyColumns{"post_id"},
			},
			exclude: map[string][]string{"post_tags": {"tag_id"}},
			wantErr: `tables.public.cycles.relationships: "LinkedPosts" links through public.post_tags.tag_id, which ` + "`exclude_columns`" + ` removes`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := excludedMemberSchema()
			schema.Relationships = nil
			input := testInput(schema)
			for table, cols := range tt.exclude {
				input.Config.Tables[table] = config.TableConfig{ExcludeColumns: cols}
			}
			cycles := input.Config.Tables["cycles"]
			cycles.Relationships = []config.TableRelationship{tt.rel}
			input.Config.Tables["cycles"] = cycles
			_, err := gen.BuildTableContexts(input, nil)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("BuildTableContexts() error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("BuildTableContexts() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}

	// Every column of a declared key is a link column, not only the first.
	t.Run("composite key member", func(t *testing.T) {
		schema := parseDDLForTest(t, config.DialectPostgres, `
CREATE TABLE seasons (league_id integer, id integer, PRIMARY KEY (league_id, id));
CREATE TABLE games (id integer PRIMARY KEY, league_id integer, season_id integer);`)
		input := testInput(schema)
		input.Config.Tables["games"] = config.TableConfig{ExcludeColumns: []string{"season_id"}}
		input.Config.Tables["seasons"] = config.TableConfig{Relationships: []config.TableRelationship{
			{Name: "Games", Type: "o2m", Table: "games", FK: config.KeyColumns{"league_id", "season_id"}},
		}}
		_, err := gen.BuildTableContexts(input, nil)
		want := `tables.public.seasons.relationships: "Games" links through public.games.season_id, which ` + "`exclude_columns`" + ` removes`
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("BuildTableContexts() error = %v, want it to contain %q", err, want)
		}
	})
}
