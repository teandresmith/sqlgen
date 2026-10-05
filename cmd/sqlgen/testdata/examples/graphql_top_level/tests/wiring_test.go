package tests

import (
	"testing"

	graph "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql_top_level/graph"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql_top_level/graph/sqlgenresolver"
	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql_top_level/models"
)

// TestTopLevelGraphWiring proves the root-relative top-level graph layout
// (§26.5.8) compiles and its import paths resolve: the graph package lives at
// <module>/graph (a sibling of <module>/models) and wires against the models
// client + sqlgenresolver Q/M helpers exactly as the nested layout does. This
// is the topology + import-resolution proof — a compile-time check that needs
// no database. Resolver /
// walker / connection behavior is exercised by the sibling `graphql` example.
func TestTopLevelGraphWiring(t *testing.T) {
	var client *models.Client
	resolver := &graph.Resolver{
		Client: client,
		Q:      &sqlgenresolver.Q{Client: client},
		M:      &sqlgenresolver.M{Client: client},
	}
	if es := graph.NewExecutableSchema(graph.Config{Resolvers: resolver}); es == nil {
		t.Fatal("NewExecutableSchema returned nil")
	}
}
