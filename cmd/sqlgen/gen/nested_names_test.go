package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// childTable is namedTable with an extra FK column, so the target of an O2M or
// has-one edge actually declares the column the edge traverses. Without it
// fkColumnOnTarget resolves false and the edge is read as belongs-to, which
// claims nothing — a fixture that silently tests the empty case.
func childTable(name, fkColumn string) parser.Table {
	t := namedTable(name, "")
	t.Columns = append(t.Columns, parser.Column{Name: fkColumn, Type: "uuid"})
	return t
}

// TestValidateResolvedNames_NestedSurfaceCollisions covers the fourth claim
// kind (PRD §8.5, §9.9.5): the nested-mutation type names key on a
// (parent, edge) pair, so they are injective in no primary name and fall in
// the cross-shape class the other three rules explicitly push out as "a
// `go build` error". Each "wants error" case below produced exactly that
// before this rule existed — a redeclaration inside generated code, reported
// against a file the consumer did not write.
func TestValidateResolvedNames_NestedSurfaceCollisions(t *testing.T) {
	tests := []struct {
		name     string
		tables   []parser.Table
		rels     []parser.Relationship
		override map[string]config.TableConfig
		wantErr  []string
	}{
		{
			// The per-edge verb block shares a namespace with every table's
			// primary Go type name.
			name: "verb block colliding with a table's struct name",
			tables: []parser.Table{
				namedTable("users", ""),
				childTable("events", "user_id"),
				namedTable("user_events_create_nesteds", ""),
			},
			rels: []parser.Relationship{{
				Name: "events", Type: parser.OneToMany,
				SourceTable: "users", TargetTable: "events", FKColumn: "user_id",
			}},
			wantErr: []string{
				`relationship users.Events`,
				`table user_events_create_nesteds`,
				`the Go type name "UserEventsCreateNested"`,
				`set tables.user_events_create_nesteds.struct_name to a different Go identifier`,
			},
		},
		{
			// The three wrapper inputs are keyed on the parent alone, and they
			// collide with a name a *table* derives rather than with its
			// primary name — which is the half the registry could not see at
			// all before this rule. Reached here through `struct_name`, which
			// is the shape a consumer is most likely to hit: the auto-derived
			// route needs a table whose SQL name pluralizes to the wrapper's.
			name: "wrapper input colliding with a table's create input",
			tables: []parser.Table{
				namedTable("users", ""),
				childTable("events", "user_id"),
				namedTable("legacy_rows", ""),
			},
			override: map[string]config.TableConfig{
				"legacy_rows": {StructName: "UserWithRelated"},
			},
			rels: []parser.Relationship{{
				Name: "events", Type: parser.OneToMany,
				SourceTable: "users", TargetTable: "events", FKColumn: "user_id",
			}},
			wantErr: []string{
				`nested mutations on table users`,
				`table legacy_rows`,
				// Both wrapper names land on the same entity, and one rename
				// fixes both, so it stays one error carrying two claims.
				`the Go type name "CreateUserWithRelatedInput" and the Go type name "UpdateUserWithRelatedInput"`,
				// Renaming *either* side resolves it. The wrapper names are
				// spelled from the parent's struct name alone, so the parent
				// has a single-key escape too — naming only the other entity
				// would read as an accusation and hide half the fix.
				`set tables.users.struct_name or tables.legacy_rows.struct_name to a different Go identifier`,
			},
		},
		{
			// The nested child input is spelled `<Parent><Edge>CreateInput`
			// rather than `Create<Parent><SingularEdge>Input` precisely so this
			// schema is legal (PRD §9.9.5). Under the prefix spelling the edge
			// resolved to `CreateUserProfileInput` — the name `user_profiles`
			// already declares — and `<parent>_<edge>` is the conventional
			// name for a dependent table, so the collision was systematic
			// rather than incidental. Two of this repo's own example schemas
			// are this shape.
			name: "a child table named after its parent and edge is legal",
			tables: []parser.Table{
				namedTable("users", ""),
				childTable("user_profiles", "user_id"),
			},
			rels: []parser.Relationship{{
				Name: "profile", Type: parser.OneToOne,
				SourceTable: "users", TargetTable: "user_profiles", FKColumn: "user_id",
			}},
		},
		{
			// A belongs-to edge holds the FK on the parent, so the parent's key
			// does not flow down it and nothing nests under it (PRD §9.9.1
			// shape 1). It claims no name, so the table that would have
			// collided does not.
			name: "a belongs-to edge claims nothing",
			tables: []parser.Table{
				childTable("users", "company_id"),
				namedTable("companies", ""),
				namedTable("user_company_create_nesteds", ""),
			},
			rels: []parser.Relationship{{
				Name: "company", Type: parser.OneToOne,
				SourceTable: "users", TargetTable: "companies", FKColumn: "company_id",
			}},
		},
		{
			// An M2M edge takes the target's ordinary create input, so it
			// declares no nested child input (PRD §9.9.5) — which is what
			// keeps every conventionally-named junction legal. Under a scheme
			// that gave M2M one, `user_categories` would collide with the
			// `users.Categories` edge on every M2M schema in existence.
			name: "an M2M edge declares no nested child input",
			tables: []parser.Table{
				namedTable("users", ""),
				namedTable("categories", ""),
				childTable("user_categories", "user_id"),
			},
			rels: []parser.Relationship{{
				Name: "categories", Type: parser.ManyToMany,
				SourceTable: "users", TargetTable: "categories",
				JunctionTable: "user_categories", JunctionLocalFK: "user_id", JunctionReferenceFK: "category_id",
			}},
		},
		{
			// A composite-PK parent cannot carry a nested surface, so it
			// claims nothing either.
			name: "a composite-PK parent claims nothing",
			tables: []parser.Table{
				compositePKTable("users"),
				childTable("events", "user_id"),
				namedTable("user_events_create_nesteds", ""),
			},
			rels: []parser.Relationship{{
				Name: "events", Type: parser.OneToMany,
				SourceTable: "users", TargetTable: "events", FKColumn: "user_id",
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &parser.Schema{Tables: tt.tables, Relationships: tt.rels}
			cfg := nameRuleConfig()
			if tt.override != nil {
				cfg.Tables = tt.override
			}

			_, err := gen.ValidateGeneration(schema, cfg)

			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("ValidateGeneration() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateGeneration() error = nil, want an error mentioning %v", tt.wantErr)
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ValidateGeneration() error = %v, want it to mention %q", err, want)
				}
			}
		})
	}
}

// compositePKTable is a two-column-PK table, the single-column parent key
// negative.
func compositePKTable(name string) parser.Table {
	return parser.Table{
		Name: name,
		Columns: []parser.Column{
			{Name: "tenant_id", Type: "uuid", PrimaryKey: true},
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "label", Type: "text"},
		},
	}
}

// TestValidateResolvedNames_NestedNamesAreNotFeatureGated pins the conditioning
// rule PRD §8.5 states for every reserved name and §9.9.5 inherits: the claim
// is made on structural facts alone. `nested_mutations.enabled` is off in this
// config — it is off by default and no example turns it on — and the collision
// is reported anyway, so turning the feature on later cannot turn a config that
// validated into one that does not.
func TestValidateResolvedNames_NestedNamesAreNotFeatureGated(t *testing.T) {
	cfg := nameRuleConfig()
	if cfg.Generation.NestedMutations != nil {
		t.Fatalf("fixture config unexpectedly opts into nested mutations: %+v", cfg.Generation.NestedMutations)
	}

	schema := &parser.Schema{
		Tables: []parser.Table{
			namedTable("users", ""),
			childTable("events", "user_id"),
			namedTable("user_events_create_nesteds", ""),
		},
		Relationships: []parser.Relationship{{
			Name: "events", Type: parser.OneToMany,
			SourceTable: "users", TargetTable: "events", FKColumn: "user_id",
		}},
	}

	_, err := gen.ValidateGeneration(schema, cfg)
	if err == nil {
		t.Fatal("ValidateGeneration() error = nil, want the collision reported with the feature switched off")
	}
	if !strings.Contains(err.Error(), `"UserEventsCreateNested"`) {
		t.Errorf("ValidateGeneration() error = %v, want it to name UserEventsCreateNested", err)
	}
}

// TestValidateResolvedNames_NestedClaimsDoNotDoubleReport is the seeding
// contract. The nested names are checked against the `Create<T>Input` /
// `Update<T>Input` names every table derives, and those derived names are
// injective in the table's primary name — so a `users` / `user` pair collides
// on both, and folding the two key kinds into one namespace would report the
// pair a second time under `CreateUserInput`. One rename still fixes
// everything, so it stays one error.
func TestValidateResolvedNames_NestedClaimsDoNotDoubleReport(t *testing.T) {
	schema := &parser.Schema{Tables: []parser.Table{namedTable("users", ""), namedTable("user", "")}}

	_, err := gen.ValidateGeneration(schema, nameRuleConfig())
	if err == nil {
		t.Fatal("ValidateGeneration() error = nil, want the users/user collision")
	}
	if got := strings.Count(err.Error(), "both resolve to"); got != 1 {
		t.Errorf("ValidateGeneration() reported %d collisions for one pair, want 1:\n%v", got, err)
	}
	if strings.Contains(err.Error(), "CreateUserInput") {
		t.Errorf("ValidateGeneration() error = %v, want the derived write-input names left out of the report", err)
	}
}

// TestBuildTableContexts_NestedClientWiring pins the two entity-client fields
// the nested executors need (PRD §9.9.6): the junction client every M2M edge
// writes its link rows through, and the callbackMode the nested methods hand to
// the transaction they open.
//
// Both are gated on structural facts, and the gates differ: callbackMode
// follows the nested surface as a whole, so an O2M-only parent carries it too,
// while a junction client exists only per M2M edge whose junction is itself
// generated.
func TestBuildTableContexts_NestedClientWiring(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			namedTable("users", ""),
			namedTable("categories", ""),
			childTable("events", "user_id"),
			childTable("user_categories", "user_id"),
			namedTable("audit_logs", ""),
		},
		Relationships: []parser.Relationship{
			{
				Name: "events", Type: parser.OneToMany,
				SourceTable: "users", TargetTable: "events", FKColumn: "user_id",
			},
			{
				Name: "categories", Type: parser.ManyToMany,
				SourceTable: "users", TargetTable: "categories",
				JunctionTable: "user_categories", JunctionLocalFK: "user_id", JunctionReferenceFK: "category_id",
			},
		},
	}

	tables, err := gen.BuildTableContexts(testInput(schema), nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error = %v, want nil", err)
	}

	byName := make(map[string]gen.TableContext, len(tables))
	for _, tc := range tables {
		byName[tc.TableName] = tc
	}

	users, ok := byName["users"]
	if !ok {
		t.Fatal("users context missing")
	}
	if !users.HasNestedSurface {
		t.Error("users.HasNestedSurface = false, want true — it has both an O2M and an M2M edge")
	}
	if got := users.NestedWriteClients; len(got) != 1 || got[0] != "UserCategory" {
		t.Errorf("users.NestedWriteClients = %v, want [UserCategory]", got)
	}

	// The junction and the O2M child are both plain tables with no edge of
	// their own: no nested surface, and no junction client.
	for _, name := range []string{"user_categories", "events", "audit_logs"} {
		tc, ok := byName[name]
		if !ok {
			t.Fatalf("%s context missing", name)
		}
		if tc.HasNestedSurface {
			t.Errorf("%s.HasNestedSurface = true, want false — it has no edge the parent key flows down", name)
		}
		if len(tc.NestedWriteClients) != 0 {
			t.Errorf("%s.NestedWriteClients = %v, want none", name, tc.NestedWriteClients)
		}
	}
}

// TestBuildTableContexts_JunctionClientDedupedAgainstTargets covers the one
// overlap the two client-field lists can have: a table whose M2M junction is
// also a relationship target of its own. The field is declared once, from
// NestedWriteClients, and the unified client's wire list is built from
// the same slice — so a second declaration would be a redeclaration and a
// missing one would be a nil pointer at the first nested write.
func TestBuildTableContexts_JunctionClientDedupedAgainstTargets(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			namedTable("users", ""),
			namedTable("categories", ""),
			childTable("user_categories", "user_id"),
		},
		Relationships: []parser.Relationship{
			{
				Name: "categories", Type: parser.ManyToMany,
				SourceTable: "users", TargetTable: "categories",
				JunctionTable: "user_categories", JunctionLocalFK: "user_id", JunctionReferenceFK: "category_id",
			},
			{
				// The junction read directly as an O2M child, which is how a
				// junction carrying payload columns is usually also exposed.
				Name: "memberships", Type: parser.OneToMany,
				SourceTable: "users", TargetTable: "user_categories", FKColumn: "user_id",
			},
		},
	}

	tables, err := gen.BuildTableContexts(testInput(schema), nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error = %v, want nil", err)
	}

	for _, tc := range tables {
		if tc.TableName != "users" {
			continue
		}
		var overlap []string
		for _, junction := range tc.NestedWriteClients {
			for _, target := range tc.RelationshipTargetClients {
				if junction == target {
					overlap = append(overlap, junction)
				}
			}
		}
		if len(overlap) != 0 {
			t.Errorf("users declares %v as both a target client and a junction client — the field would be emitted twice", overlap)
		}
		return
	}
	t.Fatal("users context missing")
}

// TestClientTemplates_NestedWiringFieldsEmitted closes a gate hole rather than
// re-asserting what the context builders already pin. Both new client fields
// are emitted from a conditional template branch, and the only fixture that
// takes that branch is an E2E golden — which `make check` does not compare,
// because it runs `go test -short` and TestE2EGoldenFiles skips under it. That
// is the same gap that once let five hand-wrapped cache goldens pass every
// local gate and fail only in CI. The `gen` golden fixture
// has no relationships, so without this test a deleted `{{ if .HasNestedSurface }}`
// block is invisible to the whole of `make check`.
//
// Both directions are asserted: a table with a nested surface declares the
// fields, and one without declares neither — an unconditional field would be
// dead weight in every generated package for a feature most consumers never
// enable.
func TestClientTemplates_NestedWiringFieldsEmitted(t *testing.T) {
	t.Run("entity client declares both fields", func(t *testing.T) {
		ctx := testClientContext_fullOps()
		ctx.HasNestedSurface = true
		ctx.NestedWriteClients = []string{"UserCategory"}

		out := executeClientTemplate(t, ctx)

		for _, want := range []string{
			"callbackMode   database.CallbackMode",
			"userCategoryClient *userCategoryClient",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("rendered entity client missing %q\n\nfull output:\n%s", want, out)
			}
		}
	})

	t.Run("a table with no nested surface declares neither", func(t *testing.T) {
		ctx := testClientContext_fullOps()

		out := executeClientTemplate(t, ctx)

		for _, unwanted := range []string{"callbackMode", "JunctionClient", "userCategoryClient"} {
			if strings.Contains(out, unwanted) {
				t.Errorf("rendered entity client unexpectedly contains %q — the field is gated on a nested surface\n\nfull output:\n%s", unwanted, out)
			}
		}
	})

	t.Run("unified client pushes the callback mode down", func(t *testing.T) {
		ctx := testUnifiedClientContext_tablesAndViews()
		ctx.NestedSurfaceEntities = []string{"order", "product"}

		out := executeUnifiedClientTemplate(t, ctx)

		for _, want := range []string{
			"c.order.callbackMode = options.callbackMode",
			"c.product.callbackMode = options.callbackMode",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("unified client missing %q\n\nfull output:\n%s", want, out)
			}
		}
		// `user` is an entity in the fixture but carries no nested surface, so
		// it must not be assigned — the field it would assign is not declared.
		if strings.Contains(out, "c.user.callbackMode") {
			t.Errorf("unified client assigns callbackMode to an entity with no nested surface, whose client does not declare the field\n\nfull output:\n%s", out)
		}
	})

	t.Run("no nested surface anywhere emits no assignment block", func(t *testing.T) {
		out := executeUnifiedClientTemplate(t, testUnifiedClientContext_tablesAndViews())

		if strings.Contains(out, "callbackMode = options.callbackMode") {
			t.Errorf("unified client emitted the push-down block with no nested-surface entity\n\nfull output:\n%s", out)
		}
	})
}
