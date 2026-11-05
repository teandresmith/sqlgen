package manifest_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/parser"
)

// nestedE3Schema is the gen nested-test fixture cut down to the edge under test: a
// single-PK users and a composite-PK user_categories, each with a declared
// one-to-many Events edge onto events.user_id.
func nestedE3Schema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "email", Type: "text"},
			}},
			{Name: "events", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "user_id", Type: "uuid", Nullable: true},
				{Name: "action", Type: "text"},
			}},
			{Name: "user_categories", Columns: []parser.Column{
				{Name: "user_id", Type: "uuid", PrimaryKey: true},
				{Name: "category_id", Type: "bigint", PrimaryKey: true},
			}},
		},
	}
}

func nestedE3Config() *config.RootConfig {
	cfg := baseConfig()
	cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
	events := []config.TableRelationship{{Name: "Events", Type: "one_to_many", Table: "events", FK: config.KeyColumns{"user_id"}}}
	cfg.Tables["users"] = config.TableConfig{Relationships: events}
	cfg.Tables["user_categories"] = config.TableConfig{Relationships: events}
	return cfg
}

func allMethodNames(e manifest.Entity) []string {
	var out []string
	for _, group := range [][]manifest.Method{e.Methods.Query, e.Methods.Mutation} {
		for _, m := range group {
			out = append(out, m.Name)
		}
	}
	return out
}

// TestBuild_E3CompositePKParentKeepsManifestEntries pins PRD §9.9 rule E3: a
// composite-PK parent shut out of nested writes "gets every read surface: its
// loaders, relationship filters and manifest entries". With nested mutations
// enabled, user_categories keeps its entity record (composite pk), its
// relationships[] entry for the declared Events edge, and its read methods,
// and carries none of the …WithRelated methods. A guard shows the fixture took
// the E3 path: the gen context for user_categories has no nested surface while
// the single-PK users, under the same config, has one (and advertises it).
func TestBuild_E3CompositePKParentKeepsManifestEntries(t *testing.T) {
	schema, cfg := nestedE3Schema(), nestedE3Config()
	in := testBuildInput(t, schema, cfg)
	// BuildTableContexts stops before the post-build nested pass; the generate
	// path hands the manifest stage contexts that went through it, which is
	// what BuildTableContextsFromSchema returns.
	tables, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	in.Tables = tables

	byTable := map[string]gen.TableContext{}
	for _, tc := range in.Tables {
		byTable[tc.TableName] = tc
	}
	if tc := byTable["user_categories"]; tc.Nested != nil || !tc.CompositePK {
		t.Fatalf("fixture: user_categories Nested = %v, CompositePK = %v; want no nested surface on a composite key", tc.Nested, tc.CompositePK)
	}
	if byTable["users"].Nested == nil {
		t.Fatal("fixture: users has no nested surface, so an empty one on user_categories does not isolate E3")
	}

	doc, err := manifest.Build(in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	users := findEntity(t, doc, "User")
	if !slices.Contains(allMethodNames(users), "CreateWithRelated") {
		t.Fatalf("fixture: User advertises no CreateWithRelated; methods = %v", allMethodNames(users))
	}

	uc := findEntity(t, doc, "UserCategory")
	if uc.Table != "user_categories" {
		t.Errorf("UserCategory table = %q, want user_categories", uc.Table)
	}
	if uc.PK.Kind != "composite" || uc.PK.Struct != "UserCategoryPK" {
		t.Errorf("UserCategory pk = {kind %q, struct %q}, want {composite, UserCategoryPK}", uc.PK.Kind, uc.PK.Struct)
	}
	if got := len(uc.Columns); got != 2 {
		t.Errorf("UserCategory columns = %d, want 2", got)
	}

	wantRels := []manifest.Relationship{{
		Name: "Events", Kind: "o2m", TargetEntity: "Event",
		FK: &manifest.FK{Table: "events", Columns: []string{"user_id"}, References: []string{"user_id"}},
	}}
	if diff := cmp.Diff(wantRels, uc.Relationships); diff != "" {
		t.Errorf("UserCategory relationships (-want +got):\n%s", diff)
	}

	names := allMethodNames(uc)
	for _, want := range []string{"Get", "GetMany", "Count", "Exists", "ExistsWhere", "Paginate", "Connection", "Stream"} {
		if !slices.Contains(names, want) {
			t.Errorf("UserCategory lacks read method %s; methods = %v", want, names)
		}
	}
	for _, m := range uc.Methods.Query {
		if m.Name == "Get" && (len(m.Params) != 1 || m.Params[0].Type != "UserCategoryPK") {
			t.Errorf("UserCategory Get params = %+v, want one UserCategoryPK", m.Params)
		}
	}
	for _, n := range names {
		if strings.HasSuffix(n, "WithRelated") {
			t.Errorf("UserCategory advertises %s, but a composite-PK parent has no nested surface", n)
		}
	}

	// The emitted per-entity record carries the same relationships[] entry.
	dir := t.TempDir()
	if err := manifest.EmitJSON(doc, fullManifestConfig(config.JSONLayoutPerEntity), dir); err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	rec := decodeJSONFile(t, filepath.Join(dir, "manifest", "entities", "user_category.json"))
	rels := jsonObjects(t, rec, "relationships")
	if len(rels) != 1 || rels[0]["name"] != "Events" || rels[0]["kind"] != "o2m" {
		t.Errorf("entities/user_category.json relationships = %v, want the one Events o2m entry", rels)
	}
}
