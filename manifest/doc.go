// Package manifest defines the runtime-embedded manifest types consumed by Go
// callers of a generated sqlgen package. When manifest.embed_in_client: true,
// the generator emits a manifest_embed_gen.go into the output package that
// embeds the on-disk manifest JSON and exposes it through the generated
// Client.Manifest() / Client.ManifestEntity() / Client.ManifestVersion()
// methods (PRD §30.6).
//
// The types here mirror the canonical manifest JSON shape (PRD §30.4 / §30.4.1
// / §30.4.2 / §30.4.3 / §30.7): the same structs serve both as the JSON
// unmarshal target and the public Client API surface. LoadInto is the helper
// the generated package's init() calls to parse the embedded bytes into a
// *Document plus a table-keyed *Entity lookup map that is layout-agnostic — the
// disk-layout choice (single vs per_entity) never leaks into the Go API.
//
// This is a runtime package peer to database/, cache/, comparator/, and
// omittable/: it imports only the standard library, honoring the runtime
// dependency invariant in PRD §3.
package manifest
