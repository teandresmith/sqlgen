package manifest_test

import (
	"encoding/json"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
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

// clientInterfaceOrder returns each <Entity>Client interface's exported methods in
// declaration order (unsorted, unlike clientInterfaceMethods), read from every
// *_gen.go file in dir — the sqlite golden splits entities across per-table
// files, the postgres and mysql goldens keep them in models_gen.go and
// views_gen.go.
func clientInterfaceOrder(t *testing.T, dir string) map[string][]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*_gen.go"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	out := map[string][]string{}
	for _, path := range paths {
		file, err := goparser.ParseFile(token.NewFileSet(), path, nil, goparser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !strings.HasSuffix(ts.Name.Name, "Client") {
					continue
				}
				iface, ok := ts.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}
				var names []string
				for _, field := range iface.Methods.List {
					for _, name := range field.Names {
						if name.IsExported() {
							names = append(names, name.Name)
						}
					}
				}
				out[ts.Name.Name] = names
			}
		}
	}
	return out
}

// TestManifestMethodsKeepGeneratedOrder pins PRD §30.3: "methods.query[] /
// methods.mutation[] keep the generated method order". The generated order is
// the declaration order of the <Entity>Client interface. For every entity of
// the postgres, mysql and sqlite goldens, each list must equal the interface's
// methods filtered to that list, in the interface's order, and the two lists
// together must cover the interface. Each golden must also exercise a view and
// a composite-PK table, so neither shape can drop out of the guard unnoticed.
func TestManifestMethodsKeepGeneratedOrder(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		t.Run(dialect, func(t *testing.T) {
			dir := filepath.Join("..", "testdata", "examples", dialect, "models")
			declared := clientInterfaceOrder(t, dir)
			doc := loadGoldenManifest(t, filepath.Join(dir, "manifest", "manifest_gen.json"))
			if len(doc.Entities) == 0 {
				t.Fatal("golden manifest carries no entities")
			}

			sawView, sawCompositePK := false, false
			for _, e := range doc.Entities {
				iface, ok := declared[e.Name+"Client"]
				if !ok {
					t.Errorf("entity %q: no %sClient interface under %s", e.Name, e.Name, dir)
					continue
				}
				sawView = sawView || e.Kind == "view"
				sawCompositePK = sawCompositePK || (e.Kind == "table" && e.PK.Kind == "composite")

				query, mutation := methodNamesInOrder(e.Methods.Query), methodNamesInOrder(e.Methods.Mutation)
				for _, group := range []struct {
					name    string
					methods []string
				}{
					{name: "query", methods: query},
					{name: "mutation", methods: mutation},
				} {
					want := slices.DeleteFunc(slices.Clone(iface), func(n string) bool { return !slices.Contains(group.methods, n) })
					if diff := cmp.Diff(want, group.methods); diff != "" {
						t.Errorf("%s methods.%s order differs from %sClient (-generated +manifest):\n%s", e.Name, group.name, e.Name, diff)
					}
				}
				for _, n := range iface {
					if !slices.Contains(query, n) && !slices.Contains(mutation, n) {
						t.Errorf("%s: %sClient declares %s but the manifest lists it in neither methods.query nor methods.mutation", e.Name, e.Name, n)
					}
				}
			}
			if !sawView {
				t.Error("fixture: golden carries no view entity")
			}
			if !sawCompositePK {
				t.Error("fixture: golden carries no composite-PK table")
			}
		})
	}
}

func loadGoldenManifest(t *testing.T, path string) *manifest.Document {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // test helper, path is built from a fixed dialect list over the checked-in testdata tree
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc := new(manifest.Document)
	if err := json.Unmarshal(raw, doc); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return doc
}

func methodNamesInOrder(ms []manifest.Method) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}

// TestBuild_MethodsFollowClientInterfaceOrder pins PRD §30.3 ("methods.query[]
// / methods.mutation[] keep the generated method order") on manifest.Build
// itself, so it runs under -short and fails when the builder's order drifts
// even if the checked-in goldens are stale. The want lists are the
// declaration order of table/client.go.tmpl and view/client.go.tmpl: Exists
// and ExistsWhere before Count; create, update, upsert, the …WithRelated
// methods, Increment, then the soft-delete, restore and hard-delete families.
// The fixture switches every family on: users has a soft-delete column, a
// unique conflict target, an increment-eligible bigint and a nested Events
// edge on its single PK; user_categories is a composite-PK table; the views
// are a plain view and a concurrently refreshable materialized view.
func TestBuild_MethodsFollowClientInterfaceOrder(t *testing.T) {
	cfg := baseConfig()
	cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
	cfg.Tables["users"] = config.TableConfig{Relationships: []config.TableRelationship{
		{Name: "Events", Type: "one_to_many", Table: "events", FK: "user_id"},
	}}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "email", Type: "text"},
					{Name: "views", Type: "bigint"},
					{Name: "updated_at", Type: "timestamptz"},
					{Name: "deleted_at", Type: "timestamptz", Nullable: true},
				},
				Constraints: []parser.Constraint{
					{Name: "users_email_key", Type: parser.Unique, Columns: []string{"email"}},
				},
			},
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
		Views: []parser.View{
			{
				Name:    "active_users",
				SQL:     "SELECT id, email FROM users WHERE deleted_at IS NULL",
				Columns: []parser.Column{{Name: "id", Type: "uuid"}, {Name: "email", Type: "text"}},
			},
			{
				Name:                    "user_view_totals",
				SQL:                     "SELECT id, views FROM users",
				Materialized:            true,
				ConcurrentlyRefreshable: true,
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "views", Type: "bigint"},
				},
			},
		},
	}
	in := testBuildInput(t, schema, cfg)
	// The nested surface is attached by the post-build pass the generate path
	// runs; BuildTableContexts stops before it.
	tables, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	in.Tables = tables
	doc, err := manifest.Build(in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	tableQuery := []string{"Get", "GetMany", "Exists", "ExistsWhere", "Count", "Paginate", "Connection", "Stream"}
	tests := []struct {
		entity       string
		wantQuery    []string
		wantMutation []string
	}{
		{
			entity:    "User",
			wantQuery: tableQuery,
			wantMutation: []string{
				"Create", "CreateMany",
				"Update", "UpdateMany", "UpdateWhere",
				"Upsert", "UpsertMany",
				"CreateWithRelated", "UpdateWithRelated", "UpsertWithRelated",
				"Increment",
				"SoftDelete", "SoftDeleteMany", "SoftDeleteWhere",
				"Restore", "RestoreMany", "RestoreWhere",
				"HardDelete", "HardDeleteMany", "HardDeleteWhere",
			},
		},
		{
			entity:    "Event",
			wantQuery: tableQuery,
			wantMutation: []string{
				"Create", "CreateMany",
				"Update", "UpdateMany", "UpdateWhere",
				"Upsert", "UpsertMany",
				"HardDelete", "HardDeleteMany", "HardDeleteWhere",
			},
		},
		{
			entity:    "UserCategory",
			wantQuery: tableQuery,
			wantMutation: []string{
				"Create", "CreateMany",
				"Update", "UpdateMany", "UpdateWhere",
				"Upsert", "UpsertMany",
				"HardDelete", "HardDeleteMany", "HardDeleteWhere",
			},
		},
		{
			entity:       "ActiveUser",
			wantQuery:    []string{"GetMany", "Count", "Paginate", "Connection"},
			wantMutation: []string{},
		},
		{
			entity:       "UserViewTotal",
			wantQuery:    []string{"Get", "GetMany", "Count", "Paginate", "Connection"},
			wantMutation: []string{"Refresh", "RefreshConcurrently"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.entity, func(t *testing.T) {
			e := findEntity(t, doc, tt.entity)
			if diff := cmp.Diff(tt.wantQuery, methodNamesInOrder(e.Methods.Query)); diff != "" {
				t.Errorf("%s methods.query (-interface order +built):\n%s", tt.entity, diff)
			}
			if diff := cmp.Diff(tt.wantMutation, methodNamesInOrder(e.Methods.Mutation)); diff != "" {
				t.Errorf("%s methods.mutation (-interface order +built):\n%s", tt.entity, diff)
			}
		})
	}
}
