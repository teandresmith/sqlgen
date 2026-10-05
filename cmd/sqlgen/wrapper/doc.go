// Package wrapper implements the sqlgen graphql subcommand: a one-shot
// gqlgen.yml scaffold (init) and the merge-and-invoke wrapper (gen) that
// produces gqlgen-generated artifacts without modifying the consumer's
// gqlgen.yml on disk.
//
// The wrapper deliberately avoids importing gqlgen — the gqlgen binary is
// invoked as a subprocess via os/exec so the consumer's go.mod pins the
// gqlgen version. See PRD §26.5.6 for the full data flow.
package wrapper
