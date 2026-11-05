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

// qvwLedgerSchema has public.users and two tables that exist only in the
// audit schema: ledgers, keyed to users by user_id, and user_ledgers, a link
// table between them. Nothing named ledgers or user_ledgers is in public, so
// a bare name that fell back to public would find no table.
func qvwLedgerSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Schema: "public", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{Name: "ledgers", Schema: "audit", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "user_id", Type: "uuid", Nullable: true},
				{Name: "memo", Type: "text"},
			}},
			{Name: "user_ledgers", Schema: "audit", Columns: []parser.Column{
				{Name: "user_id", Type: "uuid", PrimaryKey: true},
				{Name: "ledger_id", Type: "uuid", PrimaryKey: true},
			}},
		},
	}
}

func qvwUsersContext(t *testing.T, schema *parser.Schema, rels []config.TableRelationship, nested bool) gen.TableContext {
	t.Helper()
	input := testInput(schema)
	if nested {
		input.Config.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
	}
	input.Config.Tables["public.users"] = config.TableConfig{Relationships: rels}
	contexts, err := gen.BuildTableContextsFromSchema(input.Schema, input.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	i := slices.IndexFunc(contexts, func(tc gen.TableContext) bool { return tc.Schema == "public" && tc.TableName == "users" })
	if i < 0 {
		t.Fatal("BuildTableContextsFromSchema(): public.users context not built")
	}
	return contexts[i]
}

func qvwNestedEdge(t *testing.T, tc gen.TableContext, field string) gen.NestedEdgeContext {
	t.Helper()
	if tc.Nested == nil {
		t.Fatalf("users carries no nested surface")
	}
	for _, e := range tc.Nested.Edges {
		if e.FieldName == field {
			return e
		}
	}
	t.Fatalf("users has no nested edge %q", field)
	return gen.NestedEdgeContext{}
}

func qvwChildFieldNames(e gen.NestedEdgeContext) []string {
	out := make([]string, 0, len(e.ChildFields))
	for _, f := range e.ChildFields {
		out = append(out, f.FieldName)
	}
	return out
}

// TestRelationshipTargets_BareTableInOneNonPublicSchema checks that a bare
// relationship table naming an entity only the audit schema has resolves to
// that entity.
func TestRelationshipTargets_BareTableInOneNonPublicSchema(t *testing.T) {
	users := qvwUsersContext(t, qvwLedgerSchema(), []config.TableRelationship{
		{Name: "Ledgers", Type: "one_to_many", Table: "ledgers", FK: config.KeyColumns{"user_id"}},
	}, false)
	r, ok := relByField(users)["Ledgers"]
	if !ok {
		t.Fatal("users has no relationship Ledgers")
	}
	if r.TargetSchema != "audit" || r.TargetTable != "ledgers" || r.TargetStructName != "Ledger" {
		t.Errorf("users.Ledgers target = (%q, %q, %q), want (audit, ledgers, Ledger)", r.TargetSchema, r.TargetTable, r.TargetStructName)
	}
}

// TestRelationshipTargets_BareTableCountsEveryEntity checks that the count
// behind the ambiguity error for a bare relationship table takes in
// entities that generate nothing of their own: a table with no primary key
// and a view.
func TestRelationshipTargets_BareTableCountsEveryEntity(t *testing.T) {
	tests := []struct {
		name  string
		other func(*parser.Schema)
	}{
		{
			name: "a table without a primary key in public",
			other: func(s *parser.Schema) {
				s.Tables = append(s.Tables, parser.Table{Name: "ledgers", Schema: "public", Columns: []parser.Column{
					{Name: "user_id", Type: "uuid"},
					{Name: "amount", Type: "bigint"},
				}})
			},
		},
		{
			name: "a view in public",
			other: func(s *parser.Schema) {
				s.Views = append(s.Views, parser.View{Name: "ledgers", Schema: "public", Columns: []parser.Column{
					{Name: "user_id", Type: "uuid"},
					{Name: "total", Type: "bigint"},
				}})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := qvwLedgerSchema()
			tt.other(schema)
			input := testInput(schema)
			input.Config.Tables["public.users"] = config.TableConfig{Relationships: []config.TableRelationship{
				{Name: "Ledgers", Type: "one_to_many", Table: "ledgers", FK: config.KeyColumns{"user_id"}},
			}}
			_, err := gen.BuildTableContextsFromSchema(input.Schema, input.Config)
			want := `"Ledgers" targets "ledgers", which names an entity in more than one schema (audit, public); qualify it as one of "audit.ledgers", "public.ledgers"`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("BuildTableContextsFromSchema() error = %v, want it to contain %q", err, want)
			}
		})
	}
}

// TestRelationshipTargets_NestedSurfaceWritesResolvedTable checks that the
// nested-mutation member of an edge writes the table its relationship table
// resolves to: the target's struct, client and create-input fields.
func TestRelationshipTargets_NestedSurfaceWritesResolvedTable(t *testing.T) {
	t.Run("bare name in one non-public schema", func(t *testing.T) {
		users := qvwUsersContext(t, qvwLedgerSchema(), []config.TableRelationship{
			{Name: "Ledgers", Type: "one_to_many", Table: "ledgers", FK: config.KeyColumns{"user_id"}},
		}, true)
		e := qvwNestedEdge(t, users, "Ledgers")
		if e.TargetStructName != "Ledger" || e.TargetClientField != "ledgerClient" {
			t.Errorf("nested Ledgers target = (%q, %q), want (Ledger, ledgerClient)", e.TargetStructName, e.TargetClientField)
		}
		if e.FKFieldName != "UserID" {
			t.Errorf("nested Ledgers FKFieldName = %q, want UserID", e.FKFieldName)
		}
		if diff := cmp.Diff([]string{"ID", "Memo"}, qvwChildFieldNames(e)); diff != "" {
			t.Errorf("nested Ledgers child fields mismatch (-want +got):\n%s", diff)
		}
	})

	// posts is in both schemas with different columns; the qualified name
	// picks audit's, whose create input has note, not title.
	t.Run("qualified name beside a same-named public table", func(t *testing.T) {
		users := qvwUsersContext(t, twoSchemaSchema(), []config.TableRelationship{
			{Name: "AuditPosts", Type: "one_to_many", Table: "audit.posts", FK: config.KeyColumns{"user_id"}},
		}, true)
		e := qvwNestedEdge(t, users, "AuditPosts")
		if e.TargetStructName != "AuditPost" || e.TargetClientField != "auditPostClient" {
			t.Errorf("nested AuditPosts target = (%q, %q), want (AuditPost, auditPostClient)", e.TargetStructName, e.TargetClientField)
		}
		if diff := cmp.Diff([]string{"ID", "Note"}, qvwChildFieldNames(e)); diff != "" {
			t.Errorf("nested AuditPosts child fields mismatch (-want +got):\n%s", diff)
		}
	})
}

// TestRelationshipTargets_NestedLinkWritesResolvedJunction checks that an M2M
// edge's nested link step writes the junction its relationship junction
// resolves to: the junction's struct, client and key members.
func TestRelationshipTargets_NestedLinkWritesResolvedJunction(t *testing.T) {
	t.Run("bare name in one non-public schema", func(t *testing.T) {
		users := qvwUsersContext(t, qvwLedgerSchema(), []config.TableRelationship{{
			Name: "LinkedLedgers", Type: "many_to_many", Table: "ledgers",
			Junction: "user_ledgers", JunctionLocalFK: config.KeyColumns{"user_id"}, JunctionReferenceFK: config.KeyColumns{"ledger_id"},
		}}, true)
		e := qvwNestedEdge(t, users, "LinkedLedgers")
		got := []string{e.JunctionStructName, e.JunctionClientField, e.JunctionLocalField, e.JunctionReferenceField}
		want := []string{"UserLedger", "userLedgerClient", "UserID", "LedgerID"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("nested LinkedLedgers junction (struct, client, local, reference) mismatch (-want +got):\n%s", diff)
		}
	})

	// user_tags is in both schemas; the public one keys its tags by label_code,
	// so only the audit junction has the tag_code reference column.
	t.Run("qualified name beside a same-named public table", func(t *testing.T) {
		schema := twoSchemaSchema()
		schema.Tables = append(schema.Tables, parser.Table{Name: "user_tags", Schema: "public", Columns: []parser.Column{
			{Name: "user_id", Type: "uuid", PrimaryKey: true},
			{Name: "label_code", Type: "text", PrimaryKey: true},
		}})
		users := qvwUsersContext(t, schema, []config.TableRelationship{{
			Name: "AuditTags", Type: "many_to_many", Table: "audit.tags",
			Junction: "audit.user_tags", JunctionLocalFK: config.KeyColumns{"user_id"}, JunctionReferenceFK: config.KeyColumns{"tag_code"},
		}}, true)
		e := qvwNestedEdge(t, users, "AuditTags")
		got := []string{e.JunctionStructName, e.JunctionClientField, e.JunctionLocalField, e.JunctionReferenceField}
		want := []string{"AuditUserTag", "auditUserTagClient", "UserID", "TagCode"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("nested AuditTags junction (struct, client, local, reference) mismatch (-want +got):\n%s", diff)
		}
	})
}
