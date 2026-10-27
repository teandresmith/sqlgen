# CLI Requirements: Manifest

<!-- seeding: in-progress -->
<!-- packages: cmd/sqlgen/manifest -->
<!-- matrix: manifest-artifacts rows=artifact cols=emitted,gated,removed-when-disabled,ownership,deterministic -->

The generated manifest (§30): configuration, the emitted artifacts, the JSON shape, versioning, extended schema metadata and the `manifest` subcommands. The runtime-embedded manifest (§30.6) is in `runtime.md`. The `manifest-artifacts` matrix has one row per §30.3 artifact, the GraphQL breadcrumb pair included; `emitted` is the artifact written when the manifest is enabled and its own gate is on, `gated` not written when that gate is off, `removed-when-disabled` gone after the next `generate` once `enabled` flips to false, `ownership` the overwrite and removal guard (the breadcrumb provenance marker, the manifest directory's anchors), and `deterministic` byte-identical across two runs with `--manifest-timestamp` pinned.
