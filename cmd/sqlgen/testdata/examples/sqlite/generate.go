// Package sqliteexample documents the `go generate ./...` entry point for the
// SQLite E2E example.
//
// `sqlgen generate` chains the gqlgen wrapper invocation in-process when
// `api.graphql.enabled` is true, so a single `sqlgen generate`
// already produces the full graph/ package. The directive below exists so
// `go generate ./...` runs the wrapper invocation explicitly and keeps the
// example honest about its toolchain.
//
//go:generate sqlgen graphql gen
package sqliteexample
