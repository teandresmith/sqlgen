package cli

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// TestBuildMergeInput_ViewBindings pins the `models:` bindings for views
// exposed over the API, asserted at the one place they are produced.
//
// Before views reached APIContext.Tables, a consumer exposing a view over
// GraphQL had to hand-write the schema file, the resolver AND a `models:` entry
// in gqlgen.yml binding the view's object type and its three envelopes to the
// generated Go types. Without that entry gqlgen autobinds the GraphQL type to
// its own `graph/model` package, and the generated QueryResolver interface then
// demands a `*model.<V>` where the sqlgen resolver returns a `*models.<V>` —
// the consumer's build breaks, one type at a time.
//
// buildMergeInput iterates apiCtx.Tables and needs no view arm of its own,
// so the risk this test covers is not that the loop is wrong: it is that a
// future change gates the loop on something table-shaped — HasCreateInput, a
// non-empty Relationships slice, a mutation operation — and silently drops
// views back out. All four bindings are asserted, because
// dropping only the envelopes would still leave the row type bound and the
// failure would surface as a gqlgen type error three files away.
func TestBuildMergeInput_ViewBindings(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{{
			Name: "products",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "name", Type: "text"},
			},
		}},
		Views: []parser.View{{
			Name: "product_summary",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "order_count", Type: "integer"},
			},
		}},
	}

	cfg := newAPIParityTestConfig()
	tableCtxs, viewCtxs, err := gen.BuildEntityContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildEntityContextsFromSchema: %v", err)
	}
	if len(viewCtxs) != 1 {
		t.Fatalf("view contexts: got %d, want 1", len(viewCtxs))
	}
	apiCtx, err := gen.BuildAPIContext(tableCtxs, viewCtxs, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	merge, warnings := buildMergeInput(cfg, apiCtx, "example.com/foo")
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	const pkg = "example.com/foo/gen"
	for _, tc := range []struct {
		graphQLType string
		goType      string
		why         string
	}{
		{"ProductSummary", pkg + ".ProductSummary", "the resolver returns *models.ProductSummary"},
		{"ProductSummaryConnection", pkg + ".ProductSummaryConnection", "the connection query returns the envelope alias"},
		{"ProductSummaryEdge", pkg + ".ProductSummaryEdge", "edges are addressed by their own type in the schema"},
		{"ProductSummaryListResult", pkg + ".ProductSummaryListResult", "the list query returns the paginate envelope alias"},
	} {
		paths, ok := merge.Models[tc.graphQLType]
		if !ok {
			t.Errorf("merge input has no models: entry for view type %q — %s, so gqlgen would autobind it to graph/model", tc.graphQLType, tc.why)
			continue
		}
		if len(paths) != 1 || paths[0] != tc.goType {
			t.Errorf("models[%q] = %v, want [%q]", tc.graphQLType, paths, tc.goType)
		}
	}

	// A view is read-only, so gqlgen owns no input type for it and there is
	// nothing to bind. An entry here would name a Go type that does not exist.
	for _, absent := range []string{"CreateProductSummaryInput", "UpdateProductSummaryInput"} {
		if paths, ok := merge.Models[absent]; ok {
			t.Errorf("merge input binds %q to %v — a view has no mutation input", absent, paths)
		}
	}

	// The table half is unaffected: this loop gained entries, it did not
	// change the ones already there.
	if paths, ok := merge.Models["Product"]; !ok || len(paths) != 1 || paths[0] != pkg+".Product" {
		t.Errorf("models[\"Product\"] = %v (present=%v), want [%q]", paths, ok, pkg+".Product")
	}
}
