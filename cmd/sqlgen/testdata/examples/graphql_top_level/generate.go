// Package graphqltoplevel documents the `go generate ./...` entry point for
// the top-level GraphQL output-dir E2E example.
//
// `sqlgen generate` chains the gqlgen wrapper invocation in-process when
// `api.graphql.enabled` is true, so a single `sqlgen generate` already
// produces the full graph/ package (here at the top-level ./graph).
// The directive below exists so `go generate ./...` runs the wrapper
// invocation explicitly and keeps the example honest about its toolchain.
//
//go:generate sqlgen graphql gen
package graphqltoplevel
