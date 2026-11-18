package graph

import (
	"net/http"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models/graph/sqlgenresolver"
)

// This file was written once by sqlgen because it was missing. It is yours:
// sqlgen never rewrites it, so edit the transports, caches and middleware
// freely. The query limits it applies are regenerated on every run in
// sqlgenresolver/limits_gen.go from api.graphql.max_depth and
// api.graphql.max_complexity.

// NewServer builds the GraphQL HTTP handler for the generated executable
// schema, with the configured query depth and complexity limits applied and
// WithCallOptionsMiddleware wrapped around it.
func NewServer(client *models.Client) http.Handler {
	srv := handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{
		Client: client,
		Q:      &sqlgenresolver.Q{Client: client},
		M:      &sqlgenresolver.M{Client: client},
	}}))
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))
	// Introspection serves schema-aware tooling; remove it to hide the schema.
	srv.Use(extension.Introspection{})
	sqlgenresolver.ApplyQueryLimits(srv)
	return sqlgenresolver.WithCallOptionsMiddleware(srv)
}
