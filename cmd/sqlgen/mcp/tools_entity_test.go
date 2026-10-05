package mcp

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/manifest"
)

func TestListEntities(t *testing.T) {
	d := fixtureDeps()
	tests := []struct {
		name string
		in   ListEntitiesInput
		want []EntitySummary
	}{
		{
			name: "all entities sorted by name",
			in:   ListEntitiesInput{},
			want: []EntitySummary{
				{Name: "ActiveUser", Table: "active_users", PKKind: "none", FeaturesSummary: ""},
				{Name: "Comment", Table: "comments", PKKind: "single", FeaturesSummary: ""},
				{Name: "Membership", Table: "memberships", PKKind: "composite", FeaturesSummary: "tenancy"},
				{Name: "Post", Table: "posts", PKKind: "single", FeaturesSummary: ""},
				{Name: "Role", Table: "roles", PKKind: "single", FeaturesSummary: ""},
				{Name: "User", Table: "users", PKKind: "single", FeaturesSummary: "soft_delete"},
			},
		},
		{
			name: "kind=view",
			in:   ListEntitiesInput{Kind: "view"},
			want: []EntitySummary{{Name: "ActiveUser", Table: "active_users", PKKind: "none", FeaturesSummary: ""}},
		},
		{
			name: "kind=table excludes the view",
			in:   ListEntitiesInput{Kind: "table"},
			want: []EntitySummary{
				{Name: "Comment", Table: "comments", PKKind: "single", FeaturesSummary: ""},
				{Name: "Membership", Table: "memberships", PKKind: "composite", FeaturesSummary: "tenancy"},
				{Name: "Post", Table: "posts", PKKind: "single", FeaturesSummary: ""},
				{Name: "Role", Table: "roles", PKKind: "single", FeaturesSummary: ""},
				{Name: "User", Table: "users", PKKind: "single", FeaturesSummary: "soft_delete"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := listEntities(d, tt.in)
			if err != nil {
				t.Fatalf("listEntities: %v", err)
			}
			if diff := cmp.Diff(tt.want, got.Entities); diff != "" {
				t.Errorf("entities mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetEntity(t *testing.T) {
	d := fixtureDeps()

	t.Run("found returns full record", func(t *testing.T) {
		got, err := getEntity(d, GetEntityInput{Name: "User"})
		if err != nil {
			t.Fatalf("getEntity: %v", err)
		}
		if got.Name != "User" || got.Comment == "" || got.Examples == nil || len(got.Indexes) == 0 {
			t.Fatalf("expected full record with comment/examples/indexes, got %+v", got)
		}
		if got.Columns[1].Check == "" || got.Columns[1].DefaultKind == "" {
			t.Errorf("expected verbose column metadata retained, got %+v", got.Columns[1])
		}
	})

	t.Run("compact drops verbose fields", func(t *testing.T) {
		got, err := getEntity(d, GetEntityInput{Name: "User", Compact: true})
		if err != nil {
			t.Fatalf("getEntity: %v", err)
		}
		if got.Comment != "" || got.Examples != nil || got.Indexes != nil {
			t.Errorf("expected comment/examples/indexes dropped, got %+v", got)
		}
		for _, c := range got.Columns {
			if c.Comment != "" || c.Check != "" || c.DefaultKind != "" {
				t.Errorf("expected per-column verbose metadata dropped, got %+v", c)
			}
		}
		// Required identity retained.
		if got.PK.Kind != "single" || got.Filter.Type != "UserFilter" || len(got.Relationships) != 2 {
			t.Errorf("compact must retain identity, got %+v", got)
		}
	})

	t.Run("compact does not mutate the stored record", func(t *testing.T) {
		if _, err := getEntity(d, GetEntityInput{Name: "User", Compact: true}); err != nil {
			t.Fatalf("getEntity: %v", err)
		}
		again, err := getEntity(d, GetEntityInput{Name: "User"})
		if err != nil {
			t.Fatalf("getEntity: %v", err)
		}
		if again.Comment == "" || again.Examples == nil {
			t.Errorf("compact leaked into the shared store record: %+v", again)
		}
	})

	t.Run("unknown name yields -32003 with suggestions", func(t *testing.T) {
		_, err := getEntity(d, GetEntityInput{Name: "Usr"})
		assertNotFound(t, err, CodeEntityNotFound, []string{"User"})
	})
}

func TestFindMethod(t *testing.T) {
	d := fixtureDeps()

	t.Run("exact case-insensitive match", func(t *testing.T) {
		got, err := findMethod(d, FindMethodInput{Query: "get"})
		if err != nil {
			t.Fatalf("findMethod: %v", err)
		}
		want := []MethodHit{
			{Entity: "Post", Method: "Get", Signature: "Get(ctx context.Context, id string) (*Post, error)"},
			{Entity: "User", Method: "Get", Signature: "Get(ctx context.Context, id string) (*User, error)"},
		}
		if diff := cmp.Diff(want, got.Matches); diff != "" {
			t.Errorf("matches mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("exact match does not substring-match", func(t *testing.T) {
		got, err := findMethod(d, FindMethodInput{Query: "Many"})
		if err != nil {
			t.Fatalf("findMethod: %v", err)
		}
		if len(got.Matches) != 0 {
			t.Errorf("expected no exact matches for Many, got %+v", got.Matches)
		}
	})

	t.Run("fuzzy substring match", func(t *testing.T) {
		got, err := findMethod(d, FindMethodInput{Query: "many", Fuzzy: true})
		if err != nil {
			t.Fatalf("findMethod: %v", err)
		}
		want := []MethodHit{
			{Entity: "User", Method: "GetMany", Signature: "GetMany(ctx context.Context, input *GetUsersInput) ([]*User, error)"},
		}
		if diff := cmp.Diff(want, got.Matches); diff != "" {
			t.Errorf("matches mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("no match returns empty, not nil", func(t *testing.T) {
		got, err := findMethod(d, FindMethodInput{Query: "Nonexistent"})
		if err != nil {
			t.Fatalf("findMethod: %v", err)
		}
		if got.Matches == nil {
			t.Errorf("expected non-nil empty slice")
		}
	})
}

// newMatviewStore extends the fixture store with a materialized view carrying
// the refresh methods and marker (manifest/MCP doc-consistency).
func newMatviewStore() *Store {
	doc := fixtureDoc()
	doc.Entities = append(doc.Entities, manifest.EntityIndex{
		Name: "OrderSummary", Table: "order_summary", Kind: manifest.EntityKindView,
	})
	entities := append(fixtureEntities(), &manifest.Entity{
		Kind: manifest.EntityKindView, Name: "OrderSummary", Table: "order_summary",
		Materialized: true,
		PK:           manifest.PK{Kind: "single", Columns: []manifest.PKColumn{{Name: "customer_id", GoField: "CustomerID", GoType: "int64"}}},
		Columns:      []manifest.Column{{Name: "customer_id", GoField: "CustomerID", GoType: "int64", DBType: "bigint", PK: true}},
		Methods: manifest.Methods{
			Query: []manifest.Method{
				{Name: "List", Params: []manifest.MethodParam{{Name: "f", Type: "OrderSummaryFilter"}, {Name: "p", Type: "Page"}}, Returns: "*OrderSummaryList"},
			},
			Mutation: []manifest.Method{
				{Name: "Refresh", Params: []manifest.MethodParam{}, Returns: "", Errors: []string{}},
				{Name: "RefreshConcurrently", Params: []manifest.MethodParam{}, Returns: "", Errors: []string{"database.ErrRefreshConcurrentlyInTx"}},
			},
		},
		Filter: manifest.Filter{Type: "OrderSummaryFilter"},
		Sort:   manifest.Sort{Type: "OrderSummarySort"},
	})
	s := NewStore("/fixture/manifest/manifest_gen.json", nil)
	s.install(doc, fixtureRaw, entities)
	s.loadedAt = fixtureLoadedAt
	s.ok = true
	return s
}

func TestGetEntity_MaterializedViewMarker(t *testing.T) {
	d := &toolDeps{store: newMatviewStore(), version: "9.9.9", watchEnabled: true}

	e, err := getEntity(d, GetEntityInput{Name: "OrderSummary"})
	if err != nil {
		t.Fatalf("getEntity: %v", err)
	}
	if !e.Materialized {
		t.Error("full entity: Materialized = false, want true")
	}

	compact, err := getEntity(d, GetEntityInput{Name: "OrderSummary", Compact: true})
	if err != nil {
		t.Fatalf("getEntity compact: %v", err)
	}
	if !compact.Materialized {
		t.Error("compact entity: Materialized = false, want true (marker must survive compaction)")
	}

	// A regular view stays unmarked.
	regular, err := getEntity(d, GetEntityInput{Name: "ActiveUser"})
	if err != nil {
		t.Fatalf("getEntity ActiveUser: %v", err)
	}
	if regular.Materialized {
		t.Error("regular view: Materialized = true, want false")
	}
}

func TestFindMethod_MatviewRefreshMethods(t *testing.T) {
	d := &toolDeps{store: newMatviewStore(), version: "9.9.9", watchEnabled: true}

	out, err := findMethod(d, FindMethodInput{Query: "RefreshConcurrently"})
	if err != nil {
		t.Fatalf("findMethod: %v", err)
	}
	if len(out.Matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(out.Matches))
	}
	if out.Matches[0].Entity != "OrderSummary" || out.Matches[0].Method != "RefreshConcurrently" {
		t.Errorf("match = %+v, want OrderSummary.RefreshConcurrently", out.Matches[0])
	}

	fuzzy, err := findMethod(d, FindMethodInput{Query: "refresh", Fuzzy: true})
	if err != nil {
		t.Fatalf("findMethod fuzzy: %v", err)
	}
	if len(fuzzy.Matches) != 2 {
		t.Errorf("fuzzy matches = %d, want 2 (Refresh + RefreshConcurrently), got %+v", len(fuzzy.Matches), fuzzy.Matches)
	}
}

// TestFeaturesSummary_TenantedView verifies (rather than changes) the payoff
// surface for tenanted views: featuresSummary tests Features.Tenancy
// generically, so a tenanted view starts reporting "tenancy" for free once the
// builder populates the block. The view shape carries no mismatch_error (PRD
// §30.4.2).
func TestFeaturesSummary_TenantedView(t *testing.T) {
	view := &manifest.Entity{
		Kind: "view",
		Name: "DocumentStat",
		Features: manifest.Features{
			Tenancy: &manifest.TenancyFeature{
				Column:               "workspace_id",
				Mode:                 "auto-filter",
				MissingResolverError: "tenancy.ErrMissing",
			},
		},
	}
	if got := featuresSummary(view); got != "tenancy" {
		t.Errorf("featuresSummary(tenanted view) = %q, want %q", got, "tenancy")
	}

	shared := &manifest.Entity{Kind: "view", Name: "PublicStat"}
	if got := featuresSummary(shared); got != "" {
		t.Errorf("featuresSummary(shared view) = %q, want empty", got)
	}
}
