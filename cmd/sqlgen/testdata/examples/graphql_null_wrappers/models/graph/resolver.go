package graph

import (
	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql_null_wrappers/models"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql_null_wrappers/models/graph/sqlgenresolver"
)

// Resolver is the root resolver struct. The Client / Q / M fields are
// pre-populated by sqlgen — initialize them at app boot before
// serving traffic so the per-table delegations have a Client to dispatch
// through. Add additional dependency fields (loggers, auth clients, etc.)
// below the Q / M lines as your consumer code requires; sqlgen never
// overwrites resolver.go after the first run, so consumer edits are
// preserved verbatim.
//
// gqlgen owns the Query() / Mutation() interface methods and the
// queryResolver / mutationResolver type defs — those are emitted into the
// schema-source resolver file (typically shared_gen.resolvers.go).
type Resolver struct {
	Client *models.Client
	Q      *sqlgenresolver.Q
	M      *sqlgenresolver.M
}
