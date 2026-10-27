# CLI Requirements: Manifest

<!-- seeding: in-progress -->
<!-- packages: cmd/sqlgen/manifest -->
<!-- matrix: manifest-artifacts rows=artifact cols=emitted,gated,removed-when-disabled,ownership,deterministic -->

The generated manifest (§30): configuration, the emitted artifacts, the JSON shape, versioning, extended schema metadata and the `manifest` subcommands. The runtime-embedded manifest (§30.6) is in `runtime.md`. The `manifest-artifacts` matrix has one row per §30.3 artifact, the GraphQL breadcrumb pair included; `emitted` is the artifact written when the manifest is enabled and its own gate is on, `gated` not written when that gate is off, `removed-when-disabled` gone after the next `generate` once `enabled` flips to false, `ownership` the overwrite and removal guard (the breadcrumb provenance marker, the manifest directory's anchors), and `deterministic` byte-identical across two runs with `--manifest-timestamp` pinned.

## §30 Manifest
<!-- prd: 30 sha256: 87240615158d6b11 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.cfg.opt-in | The manifest is opt-in. With no `generation.manifest` block, nothing manifest-specific is generated: no `manifest/` directory, `CLAUDE.md`, `AGENTS.md` or `manifest_embed_gen.go`, and no warnings | `unit: cmd/sqlgen/manifest: TestRunStage_DisabledByDefault`; `golden: cache` | all |  |
| manifest.cfg.on-disk-json-and-markdown | When enabled, the manifest is written to disk as JSON and as markdown | `unit: cmd/sqlgen/manifest: TestRunStage_EmitsFullArtifactSet` | all |  |
| manifest.cfg.runtime-methods | When enabled, the manifest is also reachable at runtime through methods on the generated `Client` | `unit: cmd/sqlgen/gen: TestManifestEmbedTemplate_SingleLayout`; `unit: cmd/sqlgen/manifest: TestRunStage_EmitsFullArtifactSet` | all |  |

## §30.2 Configuration
<!-- prd: 30.2 sha256: 2c928f16237e0142 -->

### Defaults

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.cfg.default.enabled | `enabled` defaults to `false`: leaving out the block leaves the manifest off | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestOmitted`; `unit: cmd/sqlgen/config: TestResolveTableManifestEnabled/no global, no table → false` | all |  |
| manifest.cfg.default.formats | `formats` defaults to `[json, markdown]` when enabled | `unit: cmd/sqlgen/config: TestLoadConfig_Manifest_FormatsDefault/missing key defaults to json and markdown`; `unit: cmd/sqlgen/config: TestLoadConfig_ManifestDefaultsApplied` | all |  |
| manifest.cfg.default.json-filename | `json_filename` defaults to `manifest_gen.json` | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestDefaultsApplied` | all |  |
| manifest.cfg.default.json-layout | `json_layout` defaults to `single` | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestDefaultsApplied` | all |  |
| manifest.cfg.default.json-per-entity-dir | `json_per_entity_dir` defaults to `entities` | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestDefaultsApplied` | all |  |
| manifest.cfg.default.markdown-dir | `markdown_dir` defaults to `manifest` | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestDefaultsApplied` | all |  |
| manifest.cfg.default.include-examples | `include_examples` defaults to `true` | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestDefaultsApplied` | all |  |
| manifest.cfg.default.breadcrumbs | `breadcrumbs.claude_md`, `breadcrumbs.agents_md` and `breadcrumbs.package_doc` each default to `true` when the manifest is enabled | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestDefaultsApplied` | all |  |
| manifest.cfg.default.embed-in-client | `embed_in_client` defaults to `true` | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestDefaultsApplied` | all |  |

### Field effects

The effects of `breadcrumbs.*` and `embed_in_client`, and the `formats` gate on each file, are the `manifest-artifacts` rows under §30.3. The `mcp` block belongs to §31.2 (`mcp.md`).

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.cfg.enabled.master-toggle | With `enabled: false`, no manifest artifact is emitted, whatever the other fields say. The fixture keeps every other flag on and still gets no directory, breadcrumbs or embed file | `unit: cmd/sqlgen/manifest: TestRunStage_EnabledFlipRemovesArtifacts` | all |  |
| manifest.cfg.json-filename.relative-to-markdown-dir | The top-level JSON is written under `json_filename`, relative to `markdown_dir` | `unit: cmd/sqlgen/manifest: TestCleanStale_OwnedManifestDirRemoved/json only under a custom json_filename` | all |  |
| manifest.cfg.json-filename.per-entity-index | In `per_entity` layout, `json_filename` holds the index | `unit: cmd/sqlgen/manifest: TestEmitJSON_PerEntityLayout` | all |  |
| manifest.cfg.json-filename.custom-per-entity | A custom `json_filename` is honored in `per_entity` layout too | `unit: cmd/sqlgen/manifest: TestRunStage_CustomJSONFilenameInPerEntityLayout` | all |  |
| manifest.cfg.json-layout.single | `json_layout: single` writes the full document inline in the top-level JSON | `unit: cmd/sqlgen/manifest: TestEmitJSON_SingleLayout` | all |  |
| manifest.cfg.json-layout.per-entity | `json_layout: per_entity` writes a top-level index plus one file per entity under `json_per_entity_dir` | `unit: cmd/sqlgen/manifest: TestEmitJSON_PerEntityLayout`; `golden: graphql` | all |  |
| manifest.cfg.json-per-entity-dir.custom | Per-entity JSON files are written under `<markdown_dir>/<json_per_entity_dir>` for a non-default `json_per_entity_dir` | `unit: cmd/sqlgen/manifest: TestRunStage_CustomJSONPerEntityDir` | all |  |
| manifest.cfg.markdown-dir.placement | The manifest directory is `<output.dir>/<markdown_dir>` | `unit: cmd/sqlgen/manifest: TestCleanStale_AnchorCannotBeRelocated` | all |  |
| manifest.cfg.include-examples.false-strips | With `include_examples: false`, per-entity `examples` arrays are stripped | `unit: cmd/sqlgen/manifest: TestRunStage_IncludeExamplesFalseStripsEntityExamples/single layout, false strips examples`; `unit: cmd/sqlgen/manifest: TestRunStage_IncludeExamplesFalseStripsEntityExamples/per_entity layout, false strips examples`; `unit: cmd/sqlgen/manifest: TestBuild_EntityExamplesFollowIncludeExamples/explicit false strips examples`; `unit: cmd/sqlgen/manifest: TestBuild_EntityExamplesFollowIncludeExamples` | all |  |
| manifest.cfg.include-examples.conventions-kept | With `include_examples: false`, the examples in `_conventions.md` are still kept | `unit: cmd/sqlgen/manifest: TestRunStage_IncludeExamplesFalseKeepsConventionsExamples` | all |  |
| manifest.cfg.embed-in-client.methods | `embed_in_client` generates the three methods `Client.Manifest()`, `ManifestEntity()` and `ManifestVersion()` and the embed file | `unit: cmd/sqlgen/gen: TestManifestEmbedTemplate_SingleLayout`; `unit: cmd/sqlgen/manifest: TestRunStage_EmitsFullArtifactSet` | all |  |
| manifest.cfg.agents-md.identical | `AGENTS.md` has the same content as `CLAUDE.md` | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Goldens` | all |  |

### Per-table override

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.cfg.table.opt-out-only | `tables.<name>.manifest.enabled` can only opt out: it cannot opt a table in while the global `manifest.enabled` is false | `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_PerTableOptInForbidden/table opt-in with global off is error`; `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_PerTableOptInForbidden/table opt-in with global block omitted is error` | all |  |
| manifest.cfg.table.opt-out-entities | Opting a table out leaves it out of `entities[]` in the JSON | `unit: cmd/sqlgen/manifest: TestRunStage_TableOptOutLeavesEmittedEntities/single layout`; `unit: cmd/sqlgen/manifest: TestRunStage_TableOptOutLeavesEmittedEntities/per_entity layout`; `unit: cmd/sqlgen/manifest: TestBuild_PerTableOptOut` | all |  |
| manifest.cfg.table.opt-out-markdown | An opted-out table gets no per-entity markdown file | `unit: cmd/sqlgen/manifest: TestRunStage_PerTableOptOut` | all |  |
| manifest.cfg.table.opt-out-json | An opted-out table gets no per-entity JSON file | `unit: cmd/sqlgen/manifest: TestRunStage_PerTableOptOut` | all |  |
| manifest.cfg.table.opt-out-removes-prior | Opting out a table that an earlier run included removes its existing per-entity markdown and JSON files | `unit: cmd/sqlgen/manifest: TestRunStage_TableOptOutRemovesPriorEntityFiles` | all |  |
| manifest.cfg.table.opt-out-lookup | An opted-out table is not found by the `ManifestEntity()` lookup | `unit: cmd/sqlgen/manifest: TestRunStage_TableOptOutLeavesEmittedEntities/single layout`; `unit: cmd/sqlgen/manifest: TestRunStage_TableOptOutLeavesEmittedEntities/per_entity layout` | all |  |

### Resolution order

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.cfg.resolve.table-first | When `tables.<name>.manifest.enabled` is set, it decides first | `unit: cmd/sqlgen/config: TestResolveTableManifestEnabled/global enabled, table opt-out → false`; `unit: cmd/sqlgen/config: TestLoadConfig_ManifestPerTableOptOut` | all |  |
| manifest.cfg.resolve.global | Without a table-level value, the global `generation.manifest.enabled` decides | `unit: cmd/sqlgen/config: TestResolveTableManifestEnabled/global enabled, table unset → true`; `unit: cmd/sqlgen/config: TestResolveTableManifestEnabled/global disabled, table unset → false` | all |  |
| manifest.cfg.resolve.default | With neither set, the result is `false` | `unit: cmd/sqlgen/config: TestResolveTableManifestEnabled/no global, no table → false` | all |  |

### Validation

One row per summary bullet. The rule rows, with severity, batching and schema, are in `config.md` (`config.manifest-*`). These rows reuse their tests.

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.cfg.validate.formats-empty | `formats: []` with `enabled: true` is an error | `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_FormatsEmpty/empty formats with enabled is error`; `unit: cmd/sqlgen/config: TestLoadConfig_Manifest_FormatsDefault/explicit empty list is rejected` | all |  |
| manifest.cfg.validate.formats-unknown | An unknown `formats` value is an error (the closed set is `json`, `markdown`) | `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_UnknownFormat/unknown 'xml' rejected` | all |  |
| manifest.cfg.validate.markdown-dir | An invalid `markdown_dir` placement is an error: `.`, an existing artifact dir, `_`-prefixed, absolute, or escaping via `..` | `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_MarkdownDirPlacement/dot rejected`; `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_MarkdownDirPlacement/graph collides with PRD §8.3 artifact subdir`; `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_MarkdownDirPlacement/underscore-prefixed rejected`; `unit: cmd/sqlgen/config: TestValidateManifest_RejectsEscapingPlacements/markdown_dir absolute`; `unit: cmd/sqlgen/config: TestValidateManifest_RejectsEscapingPlacements/markdown_dir escaping via ..` | all |  |
| manifest.cfg.validate.json-per-entity-dir | A `json_per_entity_dir` that equals `markdown_dir`, starts with `_` or escapes via `..` is an error | `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_JSONPerEntityDirPlacement/equal to markdown_dir rejected`; `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_JSONPerEntityDirPlacement/underscore-prefixed rejected`; `unit: cmd/sqlgen/config: TestValidateManifest_RejectsEscapingPlacements/json_per_entity_dir escaping via ..`; `unit: cmd/sqlgen/config: TestValidateManifest_RejectsEscapingPlacements/json_per_entity_dir equal to markdown_dir once cleaned` | all |  |
| manifest.cfg.validate.json-per-entity-dir.absolute | An absolute `json_per_entity_dir` is an error | `unit: cmd/sqlgen/manifest: TestValidatePreParse_AbsoluteJSONPerEntityDir` | all |  |
| manifest.cfg.validate.json-filename | A `json_filename` containing a path separator is an error | `unit: cmd/sqlgen/config: TestValidateManifest_RejectsEscapingPlacements/json_filename carrying a path separator` | all |  |
| manifest.cfg.validate.json-layout | A `json_layout` other than `single` or `per_entity` is an error | `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_JSONLayoutEnum/unknown rejected` | all |  |
| manifest.cfg.validate.table-opt-in | `tables.<name>.manifest.enabled: true` with `manifest.enabled: false` is an error | `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_PerTableOptInForbidden/table opt-in with global off is error` | all |  |
| manifest.cfg.validate.breadcrumbs | Any `breadcrumbs.*: true` with `manifest.enabled: false` is an error | `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_BreadcrumbsRequireEnabled/claude_md true is error`; `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_BreadcrumbsRequireEnabled/agents_md true is error`; `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_BreadcrumbsRequireEnabled/package_doc true is error` | all |  |
| manifest.cfg.validate.embed | `embed_in_client: true` with `manifest.enabled: false` is an error | `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_EmbedRequiresEnabled/embed true is error` | all |  |

## §30.3 Generated Artifacts
<!-- prd: 30.3 sha256: 94d1696b245fecd2 -->

### Artifacts

Matrix `manifest-artifacts`. The `gated` gate for each artifact is: `breadcrumbs.claude_md`, `breadcrumbs.agents_md` or `breadcrumbs.package_doc`; `embed_in_client`; `formats` containing `json` or `markdown`; `json_layout: per_entity` for `entities/*.json`; GraphQL enabled for `graph/*`.

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.art.claude-md.emitted | When the manifest is enabled and `claude_md` is on, `<output.dir>/CLAUDE.md` is written | `unit: cmd/sqlgen/manifest: TestRunStage_EmitsFullArtifactSet`; `golden: postgres` | all | manifest-artifacts: claude-md × emitted |
| manifest.art.claude-md.gated | With `breadcrumbs.claude_md: false`, `CLAUDE.md` is not written, and an earlier sqlgen-owned one is removed, while `AGENTS.md` stays | `unit: cmd/sqlgen/manifest: TestRunStage_BreadcrumbOptOut`; `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_OptOut/claude_md false omits CLAUDE.md` | all | manifest-artifacts: claude-md × gated |
| manifest.art.claude-md.removed | After `enabled` flips from true to false, the next generate removes the sqlgen-owned `CLAUDE.md` | `unit: cmd/sqlgen/manifest: TestRunStage_EnabledFlipRemovesArtifacts`; `unit: cmd/sqlgen/manifest: TestCleanStale_BreadcrumbsUnaffectedByDirOwnership`; `unit: cmd/sqlgen/cli: TestGenerate_ReportsManifestDeletions` | all | manifest-artifacts: claude-md × removed-when-disabled |
| manifest.art.claude-md.ownership.absent-written | An absent `CLAUDE.md` is written, carrying the provenance marker | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Ownership/absent target is written with marker` | all | manifest-artifacts: claude-md × ownership |
| manifest.art.claude-md.ownership.owned-overwritten | A `CLAUDE.md` whose leading comment carries `Generated by sqlgen` is regenerated on each run | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Ownership/present with marker is overwritten` | all | manifest-artifacts: claude-md × ownership |
| manifest.art.claude-md.ownership.user-not-written | A `CLAUDE.md` without the marker is never written | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Ownership/present without marker is left untouched and warns`; `unit: cmd/sqlgen/manifest: TestRunStage_MarkerGatedDeletionPreservesUserFile` | all | manifest-artifacts: claude-md × ownership |
| manifest.art.claude-md.ownership.user-not-deleted | A `CLAUDE.md` without the marker is not deleted when the manifest is disabled | `unit: cmd/sqlgen/manifest: TestRunStage_MarkerGatedDeletionPreservesUserFile` | all | manifest-artifacts: claude-md × ownership |
| manifest.art.claude-md.ownership.demarked-not-overwritten | Once the user deletes the marker line from a sqlgen-authored `CLAUDE.md`, the next run does not overwrite it | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Ownership/marker line deleted becomes user-owned` | all | manifest-artifacts: claude-md × ownership |
| manifest.art.claude-md.ownership.demarked-not-deleted | Once the user deletes the marker line, stale-delete is suppressed too: a later disable leaves the file in place | `unit: cmd/sqlgen/manifest: TestRunStage_DemarkedBreadcrumbsSurviveDisable` | all | manifest-artifacts: claude-md × ownership |
| manifest.art.claude-md.deterministic | `CLAUDE.md` is byte-identical across runs | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Determinism`; `golden: postgres` | all | manifest-artifacts: claude-md × deterministic |
| manifest.art.agents-md.emitted | When the manifest is enabled and `agents_md` is on, `<output.dir>/AGENTS.md` is written | `unit: cmd/sqlgen/manifest: TestRunStage_EmitsFullArtifactSet`; `golden: postgres` | all | manifest-artifacts: agents-md × emitted |
| manifest.art.agents-md.gated | With `breadcrumbs.agents_md: false`, `AGENTS.md` is not written, while `CLAUDE.md` is | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_OptOut/agents_md false omits AGENTS.md` | all | manifest-artifacts: agents-md × gated |
| manifest.art.agents-md.removed | After `enabled` flips from true to false, the next generate removes the sqlgen-owned `AGENTS.md` | `unit: cmd/sqlgen/manifest: TestRunStage_DisableRemovesAgentsMDAndEntityJSON` | all | manifest-artifacts: agents-md × removed-when-disabled |
| manifest.art.agents-md.ownership.marker | sqlgen writes `AGENTS.md` with the provenance marker leading the file | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Goldens` | all | manifest-artifacts: agents-md × ownership |
| manifest.art.agents-md.ownership.user-untouched | An `AGENTS.md` without the marker is never written or deleted | `unit: cmd/sqlgen/manifest: TestRunStage_UnmarkedBreadcrumbsNeverWrittenOrDeleted/models AGENTS.md` | all | manifest-artifacts: agents-md × ownership |
| manifest.art.agents-md.deterministic | `AGENTS.md` is byte-identical across runs | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Determinism`; `golden: postgres` | all | manifest-artifacts: agents-md × deterministic |
| manifest.art.package-doc.emitted | With `breadcrumbs.package_doc` on, `models_gen.go`'s package doc comment is `// Package <name> is generated by sqlgen. AI agents: read <markdown_dir>/_index.md before consuming this package.` | `unit: cmd/sqlgen/cli: TestGenerate_ManifestPointerInModelsPackageDoc/package_doc defaults on`; `unit: cmd/sqlgen/cli: TestGenerate_ManifestPointerInModelsPackageDoc/package_doc explicitly on`; `unit: cmd/sqlgen/cli: TestGenerate_ManifestPointerInModelsPackageDoc/custom markdown_dir` | all | manifest-artifacts: package-doc × emitted |
| manifest.art.package-doc.json-only | When `formats` has no `markdown`, the pointer names `<markdown_dir>/<json_filename>` instead of `_index.md` | `unit: cmd/sqlgen/cli: TestGenerate_ManifestPointerInModelsPackageDoc/formats without markdown names the JSON`; `unit: cmd/sqlgen/cli: TestGenerate_ManifestPointerInModelsPackageDoc/formats without markdown follows json_filename` | all |  |
| manifest.art.package-doc.client-file | When there is no `models_gen.go` (`file_per_table`, or no tables), the pointer goes into the client file (`output.client.file`) | `unit: cmd/sqlgen/cli: TestGenerate_ManifestPointerInClientPackageDoc/file_per_table`; `unit: cmd/sqlgen/cli: TestGenerate_ManifestPointerInClientPackageDoc/single_file with zero tables` | all |  |
| manifest.art.breadcrumb.pointer-json-only | When `formats` has no `markdown`, `CLAUDE.md`, `AGENTS.md` and the graph breadcrumbs point at `<markdown_dir>/<json_filename>`, not `_index.md` | `unit: cmd/sqlgen/manifest: TestRunStage_JSONOnlyBreadcrumbsPointAtJSON/single layout`; `unit: cmd/sqlgen/manifest: TestRunStage_JSONOnlyBreadcrumbsPointAtJSON/per_entity layout`; `unit: cmd/sqlgen/manifest: TestRunStage_JSONOnlyModelsAgentsMDPointsAtJSON`; `unit: cmd/sqlgen/manifest: TestRunStage_JSONOnlyBreadcrumbsPointAtJSON` | all |  |
| manifest.art.package-doc.gated | With `breadcrumbs.package_doc: false`, `models_gen.go` carries no manifest pointer | `unit: cmd/sqlgen/cli: TestGenerate_ManifestPointerInModelsPackageDoc/package_doc off` | all | manifest-artifacts: package-doc × gated |
| manifest.art.package-doc.removed | After `enabled` flips from true to false, the pointer is gone from `models_gen.go` | `unit: cmd/sqlgen/cli: TestGenerate_ManifestPointerRemovedWhenDisabled` | all | manifest-artifacts: package-doc × removed-when-disabled |
| manifest.art.package-doc.deterministic | The `models_gen.go` pointer is byte-identical across runs | `unit: cmd/sqlgen/cli: TestGenerate_ManifestPointerDeterministic` | all | manifest-artifacts: package-doc × deterministic |
| manifest.art.manifest-embed.emitted | With `embed_in_client` on, `<output.dir>/manifest_embed_gen.go` is written, with a `//go:embed` of the manifest JSON and the three `Client` methods | `unit: cmd/sqlgen/manifest: TestRunStage_EmitsFullArtifactSet`; `unit: cmd/sqlgen/gen: TestManifestEmbedTemplate_SingleLayout`; `golden: postgres` | all | manifest-artifacts: manifest-embed × emitted |
| manifest.art.manifest-embed.gated | With `embed_in_client: false`, `manifest_embed_gen.go` is not written | `unit: cmd/sqlgen/manifest: TestRunStage_FormatAndEmbedGates/embed_in_client false` | all | manifest-artifacts: manifest-embed × gated |
| manifest.art.manifest-embed.removed | After `enabled` flips from true to false, the next generate removes `manifest_embed_gen.go` | `unit: cmd/sqlgen/manifest: TestRunStage_EnabledFlipRemovesArtifacts`; `unit: cmd/sqlgen/manifest: TestCleanStale_BreadcrumbsUnaffectedByDirOwnership`; `unit: cmd/sqlgen/cli: TestGenerate_ReportsManifestDeletions` | all | manifest-artifacts: manifest-embed × removed-when-disabled |
| manifest.art.manifest-embed.deterministic | `manifest_embed_gen.go` is byte-identical across runs | `golden: postgres` | all | manifest-artifacts: manifest-embed × deterministic |
| manifest.art.manifest-json.emitted | With `json` in `formats`, `<markdown_dir>/manifest_gen.json` is written | `unit: cmd/sqlgen/manifest: TestRunStage_EmitsFullArtifactSet`; `unit: cmd/sqlgen/manifest: TestEmitJSON_SingleLayout`; `golden: postgres` | all | manifest-artifacts: manifest-json × emitted |
| manifest.art.manifest-json.gated | With `json` absent from `formats`, `manifest_gen.json` is not written | `unit: cmd/sqlgen/manifest: TestRunStage_FormatAndEmbedGates/markdown only, per_entity layout` | all | manifest-artifacts: manifest-json × gated |
| manifest.art.manifest-json.removed | After `enabled` flips from true to false, the next generate removes `manifest_gen.json` along with the manifest directory | `unit: cmd/sqlgen/manifest: TestRunStage_EnabledFlipRemovesArtifacts`; `unit: cmd/sqlgen/manifest: TestCleanStale_OwnedManifestDirRemoved/json only, manifest_gen.json is the sole anchor`; `unit: cmd/sqlgen/cli: TestGenerate_ReportsManifestDeletions` | all | manifest-artifacts: manifest-json × removed-when-disabled |
| manifest.art.manifest-json.ownership.anchor | A `json_filename` file whose `generator.name` is `sqlgen` proves the directory is sqlgen's, even as the only anchor and under a custom `json_filename` | `unit: cmd/sqlgen/manifest: TestCleanStale_OwnedManifestDirRemoved/json only, manifest_gen.json is the sole anchor`; `unit: cmd/sqlgen/manifest: TestCleanStale_OwnedManifestDirRemoved/json only under a custom json_filename` | all | manifest-artifacts: manifest-json × ownership |
| manifest.art.manifest-json.ownership.not-sqlgen | A `json_filename` file that is not sqlgen's, because another generator wrote it or it is not JSON, proves nothing, and the directory is left in place | `unit: cmd/sqlgen/manifest: TestCleanStale_UnownedManifestDirSurvives/json written by some other generator`; `unit: cmd/sqlgen/manifest: TestCleanStale_UnownedManifestDirSurvives/json that is not even json` | all | manifest-artifacts: manifest-json × ownership |
| manifest.art.manifest-json.ownership.stale-filename | A manifest written under a `json_filename` the current config no longer names is not removed (fail closed) | `unit: cmd/sqlgen/manifest: TestCleanStale_CustomDirNamesFailClosed` | all | manifest-artifacts: manifest-json × ownership |
| manifest.art.manifest-json.deterministic | `manifest_gen.json` is byte-identical across two runs when `--manifest-timestamp` is pinned | `unit: cmd/sqlgen/manifest: TestRunStage_TimestampDeterminism`; `unit: cmd/sqlgen/manifest: TestEmitJSON_Deterministic`; `golden: postgres` | all | manifest-artifacts: manifest-json × deterministic |
| manifest.art.index-md.emitted | With `markdown` in `formats`, `<markdown_dir>/_index.md` is written | `unit: cmd/sqlgen/manifest: TestRunStage_EmitsFullArtifactSet`; `golden: postgres` | all | manifest-artifacts: index-md × emitted |
| manifest.art.index-md.gated | With `markdown` absent from `formats`, `_index.md` is not written | `unit: cmd/sqlgen/manifest: TestRunStage_FormatAndEmbedGates/json only, single layout`; `unit: cmd/sqlgen/manifest: TestRunStage_FormatAndEmbedGates/json only, per_entity layout` | all | manifest-artifacts: index-md × gated |
| manifest.art.index-md.removed | After `enabled` flips from true to false, the next generate removes `_index.md` along with the manifest directory | `unit: cmd/sqlgen/manifest: TestRunStage_EnabledFlipRemovesArtifacts`; `unit: cmd/sqlgen/manifest: TestCleanStale_OwnedManifestDirRemoved/markdown only, index is the sole anchor` | all | manifest-artifacts: index-md × removed-when-disabled |
| manifest.art.index-md.ownership.anchor | An `_index.md` carrying the generator line proves the directory is sqlgen's, even as the only anchor | `unit: cmd/sqlgen/manifest: TestCleanStale_OwnedManifestDirRemoved/markdown only, index is the sole anchor`; `unit: cmd/sqlgen/manifest: TestManifestIndexAnchorDrift` | all | manifest-artifacts: index-md × ownership |
| manifest.art.index-md.ownership.disown | Deleting the generator line from `_index.md` disowns the directory, which is then left in place | `unit: cmd/sqlgen/manifest: TestCleanStale_UnownedManifestDirSurvives/index disowned by dropping the generator line` | all | manifest-artifacts: index-md × ownership |
| manifest.art.index-md.deterministic | `_index.md` is byte-identical across runs | `unit: cmd/sqlgen/manifest: TestEmitMarkdown_Determinism`; `golden: postgres` | all | manifest-artifacts: index-md × deterministic |
| manifest.art.conventions-md.emitted | With `markdown` in `formats`, `<markdown_dir>/_conventions.md` is written | `unit: cmd/sqlgen/manifest: TestRunStage_EmitsFullArtifactSet`; `golden: postgres` | all | manifest-artifacts: conventions-md × emitted |
| manifest.art.conventions-md.gated | With `markdown` absent from `formats`, `_conventions.md` is not written | `unit: cmd/sqlgen/manifest: TestRunStage_FormatAndEmbedGates/json only, single layout`; `unit: cmd/sqlgen/manifest: TestRunStage_FormatAndEmbedGates/json only, per_entity layout` | all | manifest-artifacts: conventions-md × gated |
| manifest.art.conventions-md.removed | After `enabled` flips from true to false, the next generate removes `_conventions.md` along with the manifest directory | `unit: cmd/sqlgen/manifest: TestRunStage_EnabledFlipRemovesArtifacts` | all | manifest-artifacts: conventions-md × removed-when-disabled |
| manifest.art.conventions-md.ownership | `_conventions.md` is removed only along with a manifest directory that carries an anchor. A directory with no anchor is left whole | `unit: cmd/sqlgen/manifest: TestRunStage_ConventionsMDFollowsDirOwnership/no anchor leaves _conventions.md`; `unit: cmd/sqlgen/manifest: TestRunStage_ConventionsMDFollowsDirOwnership/anchored dir takes _conventions.md with it` | all | manifest-artifacts: conventions-md × ownership |
| manifest.art.conventions-md.deterministic | `_conventions.md` is byte-identical across runs | `unit: cmd/sqlgen/manifest: TestEmitMarkdown_Determinism`; `golden: postgres` | all | manifest-artifacts: conventions-md × deterministic |
| manifest.art.entity-md.emitted | With `markdown` in `formats`, one `<markdown_dir>/<file_prefix>.md` is written per entity | `unit: cmd/sqlgen/manifest: TestRunStage_EmitsFullArtifactSet`; `golden: postgres` | all | manifest-artifacts: entity-md × emitted |
| manifest.art.entity-md.gated | With `markdown` absent from `formats`, no per-entity markdown is written | `unit: cmd/sqlgen/manifest: TestRunStage_FormatAndEmbedGates/json only, single layout`; `unit: cmd/sqlgen/manifest: TestRunStage_FormatAndEmbedGates/json only, per_entity layout` | all | manifest-artifacts: entity-md × gated |
| manifest.art.entity-md.removed | After `enabled` flips from true to false, the next generate removes every per-entity markdown file, including ones for tables dropped earlier | `unit: cmd/sqlgen/manifest: TestRunStage_EnabledFlipRemovesArtifacts`; `unit: cmd/sqlgen/manifest: TestCleanStale_OwnedManifestDirRemoved/both formats` | all | manifest-artifacts: entity-md × removed-when-disabled |
| manifest.art.entity-md.ownership | Markdown files in a manifest directory with no anchor are not removed | `unit: cmd/sqlgen/manifest: TestCleanStale_UnownedManifestDirSurvives/index disowned by dropping the generator line` | all | manifest-artifacts: entity-md × ownership |
| manifest.art.entity-md.deterministic | Per-entity markdown is byte-identical across runs | `unit: cmd/sqlgen/manifest: TestEmitMarkdown_Determinism`; `golden: postgres` | all | manifest-artifacts: entity-md × deterministic |
| manifest.art.entity-json.emitted | In `per_entity` layout, one `<markdown_dir>/entities/<file_prefix>.json` is written per entity | `unit: cmd/sqlgen/manifest: TestRunStage_PerTableOptOut`; `unit: cmd/sqlgen/manifest: TestEmitJSON_PerEntityLayout`; `golden: graphql` | all | manifest-artifacts: entity-json × emitted |
| manifest.art.entity-json.gated-layout | The `entities/` directory exists only in `per_entity` layout. Under `single` it is not written, and after a `per_entity` → `single` flip it is dropped | `unit: cmd/sqlgen/manifest: TestCleanStale_EntitiesDirDroppedOnLayoutFlip/only sqlgen json inside, directory goes`; `golden: postgres` | all | manifest-artifacts: entity-json × gated |
| manifest.art.entity-json.gated-format | With `json` absent from `formats`, no per-entity JSON is written, even in `per_entity` layout | `unit: cmd/sqlgen/manifest: TestRunStage_FormatAndEmbedGates/markdown only, per_entity layout` | all | manifest-artifacts: entity-json × gated |
| manifest.art.entity-json.removed | After `enabled` flips from true to false, the next generate removes `entities/*.json` along with the manifest directory | `unit: cmd/sqlgen/manifest: TestRunStage_DisableRemovesAgentsMDAndEntityJSON` | all | manifest-artifacts: entity-json × removed-when-disabled |
| manifest.art.entity-json.ownership | After the layout flip, only the `*.json` files sqlgen emits in `entities/` are removed. The directory goes only if nothing else remains | `unit: cmd/sqlgen/manifest: TestCleanStale_EntitiesDirDroppedOnLayoutFlip/consumer file inside, directory stays` | all | manifest-artifacts: entity-json × ownership |
| manifest.art.entity-json.deterministic | Per-entity JSON is byte-identical across two runs when `--manifest-timestamp` is pinned | `golden: graphql` | all | manifest-artifacts: entity-json × deterministic |
| manifest.art.graph-claude-md.emitted | With GraphQL enabled, the graph package gets `graph/CLAUDE.md`, whether the graph dir is nested under models or is a top-level sibling | `unit: cmd/sqlgen/manifest: TestRunStage_NestedGraphBreadcrumbs`; `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_LayeredRelativePath/top-level sibling graph`; `golden: graphql` | all | manifest-artifacts: graph-claude-md × emitted |
| manifest.art.graph-claude-md.gated | With GraphQL disabled, no `graph/CLAUDE.md` is written | `golden: postgres` | all | manifest-artifacts: graph-claude-md × gated |
| manifest.art.graph-claude-md.removed | After `enabled` flips from true to false, the next generate removes the sqlgen-owned `graph/CLAUDE.md` | `unit: cmd/sqlgen/manifest: TestRunStage_NestedGraphBreadcrumbs` | all | manifest-artifacts: graph-claude-md × removed-when-disabled |
| manifest.art.graph-claude-md.ownership.marker | `graph/CLAUDE.md` opens with the provenance marker | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_LayeredGoldens` | all | manifest-artifacts: graph-claude-md × ownership |
| manifest.art.graph-claude-md.ownership.user-untouched | A `graph/CLAUDE.md` without the marker is never written or deleted (the same rule as the models breadcrumbs) | `unit: cmd/sqlgen/manifest: TestRunStage_UnmarkedBreadcrumbsNeverWrittenOrDeleted/graph CLAUDE.md` | all | manifest-artifacts: graph-claude-md × ownership |
| manifest.art.graph-claude-md.deterministic | `graph/CLAUDE.md` is byte-identical across runs | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Determinism`; `golden: graphql` | all | manifest-artifacts: graph-claude-md × deterministic |
| manifest.art.graph-agents-md.emitted | With GraphQL enabled, the graph package gets `graph/AGENTS.md`, with the same content as `graph/CLAUDE.md` | `unit: cmd/sqlgen/manifest: TestRunStage_NestedGraphBreadcrumbs`; `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_LayeredGoldens`; `golden: graphql` | all | manifest-artifacts: graph-agents-md × emitted |
| manifest.art.graph-agents-md.gated | With GraphQL disabled, no `graph/AGENTS.md` is written | `golden: postgres` | all | manifest-artifacts: graph-agents-md × gated |
| manifest.art.graph-agents-md.removed | After `enabled` flips from true to false, the next generate removes the sqlgen-owned `graph/AGENTS.md` | `unit: cmd/sqlgen/manifest: TestRunStage_NestedGraphBreadcrumbs` | all | manifest-artifacts: graph-agents-md × removed-when-disabled |
| manifest.art.graph-agents-md.ownership.marker | `graph/AGENTS.md` opens with the provenance marker | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_LayeredGoldens` | all | manifest-artifacts: graph-agents-md × ownership |
| manifest.art.graph-agents-md.ownership.user-untouched | A `graph/AGENTS.md` without the marker is never written or deleted | `unit: cmd/sqlgen/manifest: TestRunStage_UnmarkedBreadcrumbsNeverWrittenOrDeleted/graph AGENTS.md` | all | manifest-artifacts: graph-agents-md × ownership |
| manifest.art.graph-agents-md.deterministic | `graph/AGENTS.md` is byte-identical across runs | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Determinism`; `golden: graphql` | all | manifest-artifacts: graph-agents-md × deterministic |

### Artifact contents

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.art.index-md.scan-table | `_index.md` is a table that scans the entities | `unit: cmd/sqlgen/manifest: TestEmitMarkdown_Goldens` | all |  |
| manifest.art.index-md.cross-ref | `_index.md` cross-references `_conventions.md` | `unit: cmd/sqlgen/manifest: TestEmitMarkdown_Goldens` | all |  |
| manifest.art.conventions-md.topics | `_conventions.md` covers error sentinels, pagination, comparators, soft-delete, `CallOptions`, and the Comparator and Omittable surface | `unit: cmd/sqlgen/manifest: TestEmitMarkdown_Goldens` | all |  |
| manifest.art.json.single-inline | In single layout, `manifest_gen.json` carries the full document inline | `unit: cmd/sqlgen/manifest: TestEmitJSON_SingleLayout` | all |  |
| manifest.art.json.per-entity-index | In `per_entity` layout, `manifest_gen.json` is the index | `unit: cmd/sqlgen/manifest: TestEmitJSON_PerEntityLayout` | all |  |

### File naming

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.art.naming.prefix-is-snake-name | Per-entity markdown and JSON files are named after the entity's `file_prefix`, which is the singular snake_case form gen names the per-entity Go file from: `users` becomes `manifest/user.md` | `unit: cmd/sqlgen/manifest: TestRunStage_EmitsFullArtifactSet`; `unit: cmd/sqlgen/manifest: TestRunStage_PerTableOptOut`; `golden: postgres` | all |  |
| manifest.art.naming.prefix-verbatim | The emitter writes `<file_prefix>.md` verbatim, so a schema-qualified prefix round-trips to its file name | `unit: cmd/sqlgen/manifest: TestEmitMarkdown_MultiSchemaFilename` | all |  |
| manifest.art.naming.prefix-layout-independent | `file_prefix` is the same under either `output.layout` | `unit: cmd/sqlgen/manifest: TestBuild_EntityFilesFollowOutputLayout` | all |  |
| manifest.art.naming.files-single-file | Under `single_file`, an entity's `files[]` names `models_gen.go` for a table and `views_gen.go` for a view | `unit: cmd/sqlgen/manifest: TestBuild_EntityFilesFollowOutputLayout/single_file concatenates into models_gen.go and views_gen.go`; `unit: cmd/sqlgen/manifest: TestBuild_EntityFilesFollowOutputLayout/unset layout resolves to the single_file default` | all |  |
| manifest.art.naming.files-file-per-table | Under `file_per_table`, `files[]` names `<prefix>_gen.go`, beside `<prefix>.md` | `unit: cmd/sqlgen/manifest: TestBuild_EntityFilesFollowOutputLayout/file_per_table gives each entity its own snake-cased file` | all |  |
| manifest.art.naming.files-resolve | Every `files[]` entry names a Go file that exists in the generated package, under both layouts | `unit: cmd/sqlgen/manifest: TestEntityFilesExistOnDisk` | all |  |
| manifest.art.naming.schema-prefix-on-collision | The schema is prefixed only when the bare name appears in more than one parsed schema: `public.users` beside `audit.users` gives `manifest/public_user.md` | `golden: postgres` | postgres |  |
| manifest.art.naming.lone-schema-bare | A table whose name appears in only one schema keeps the bare prefix (a lone `public.users` stays `manifest/user.md`) | `golden: postgres` | postgres |  |
| manifest.art.naming.single-file-views | Under `single_file`, every view's body is concatenated into `views_gen.go` and no per-view `<file_prefix>_gen.go` is written | `unit: cmd/sqlgen/cli: TestSingleFileLayout_views` | all |  |

### Breadcrumb ownership

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.art.breadcrumb.marker-text | Every sqlgen-authored breadcrumb opens with the provenance-marker HTML comment block given in §30.3 | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Goldens`; `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_LayeredGoldens` | all |  |
| manifest.art.breadcrumb.no-hard-fail | A breadcrumb that exists without the marker does not fail generation | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Ownership/present without marker is left untouched and warns` | all |  |
| manifest.art.breadcrumb.warning-names-file | The user-owned-file warning names the file | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Ownership/present without marker is left untouched and warns` | all |  |
| manifest.art.breadcrumb.warning-pointer-block | The warning offers a pointer block the user can copy and paste for agent auto-discovery | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Ownership/present without marker is left untouched and warns` | all |  |
| manifest.art.breadcrumb.warning-opt-out | The warning points at `breadcrumbs.<flag>: false` to silence it | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Ownership/present without marker is left untouched and warns` | all |  |
| manifest.art.breadcrumb.marker-not-in-block | The pasteable pointer block never carries the marker | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_MarkerNotInPointerBlock` | all |  |
| manifest.art.breadcrumb.no-do-not-edit | The marker does not say "DO NOT EDIT" | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_Goldens` | all |  |

### Stale-file cleanup

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.art.stale.removed-table-md | Removing a table from the schema removes its per-entity markdown on the next generation, in either JSON layout | `unit: cmd/sqlgen/manifest: TestCleanStale_RemovedTable` | all |  |
| manifest.art.stale.removed-table-json | Removing a table from the schema removes its per-entity JSON on the next generation | `unit: cmd/sqlgen/manifest: TestCleanStale_RemovedTable` | all |  |
| manifest.art.stale.format-markdown-dropped | With the manifest still enabled, dropping `markdown` from `formats` removes every `.md` file in the owned `markdown_dir` on the next generation | `unit: cmd/sqlgen/manifest: TestRunStage_MarkdownFormatDroppedRemovesMarkdown`; `unit: cmd/sqlgen/manifest: TestRunStage_MarkdownFormatDroppedRemovesEveryMarkdownFile` | all |  |
| manifest.art.stale.format-json-dropped | With the manifest still enabled, dropping `json` from `formats` removes the `json_filename` artifact and the per-entity `*.json` files on the next generation | `unit: cmd/sqlgen/manifest: TestRunStage_JSONFormatDroppedRemovesJSON` | all |  |
| manifest.art.stale.format-dropped-ownership | A format flip removes files only under the *Directory ownership* rule: a `markdown_dir` with no anchor is untouched | `unit: cmd/sqlgen/manifest: TestRunStage_MarkdownFormatDroppedLeavesUnanchoredDir` | all |  |
| manifest.art.stale.graphql-disabled | With the manifest still enabled, disabling GraphQL removes the graph breadcrumbs that carry the provenance marker | `unit: cmd/sqlgen/manifest: TestRunStage_GraphQLDisabledRemovesGraphBreadcrumbs` | all |  |
| manifest.art.stale.graphql-disabled-user-owned | Disabling GraphQL leaves an unmarked graph breadcrumb untouched | `unit: cmd/sqlgen/manifest: TestRunStage_GraphQLDisabledLeavesUnmarkedGraphBreadcrumbs` | all |  |
| manifest.art.stale.disable-removes-dir | Setting `manifest.enabled: false` after a prior `true` removes the whole manifest directory | `unit: cmd/sqlgen/manifest: TestRunStage_EnabledFlipRemovesArtifacts`; `unit: cmd/sqlgen/manifest: TestCleanStale_OwnedManifestDirRemoved/both formats`; `unit: cmd/sqlgen/cli: TestGenerate_ReportsManifestDeletions` | all |  |
| manifest.art.stale.disable-keeps-user-breadcrumbs | Breadcrumbs that are user-owned are never deleted on disable | `unit: cmd/sqlgen/manifest: TestRunStage_MarkerGatedDeletionPreservesUserFile` | all |  |
| manifest.art.stale.disable-keeps-user-breadcrumbs.user-adopted | Breadcrumbs that the user adopted are never deleted on disable | `unit: cmd/sqlgen/manifest: TestRunStage_DemarkedBreadcrumbsSurviveDisable` | all |  |

### Directory ownership

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.art.dir.unowned-survives | A `<output.dir>/<markdown_dir>/` that sqlgen never wrote is left untouched by the disabled path, the default run included | `unit: cmd/sqlgen/manifest: TestCleanStale_UnownedManifestDirSurvives/no manifest block, Go package named manifest`; `unit: cmd/sqlgen/manifest: TestCleanStale_UnownedManifestDirSurvives/explicitly disabled, user docs directory`; `unit: cmd/sqlgen/cli: TestGenerate_UnownedManifestDirSurvivesCLI` | all |  |
| manifest.art.dir.anchor-from-content | Ownership is read from an anchor inside the directory being removed, never from elsewhere | `unit: cmd/sqlgen/manifest: TestCleanStale_AnchorCannotBeRelocated` | all |  |
| manifest.art.dir.unreadable-fail-closed | An anchor that cannot be read is not proof: the directory stays | `unit: cmd/sqlgen/manifest: TestCleanStale_UnreadableAnchorDoesNotFailGeneration` | all |  |
| manifest.art.dir.probe-never-fails | An ownership probe never fails a generation | `unit: cmd/sqlgen/manifest: TestCleanStale_UnreadableAnchorDoesNotFailGeneration` | all |  |
| manifest.art.dir.breadcrumbs-independent | The directory gate covers only the directory: sqlgen-owned breadcrumbs and the embed file are still removed beside an unowned `manifest/` | `unit: cmd/sqlgen/manifest: TestCleanStale_BreadcrumbsUnaffectedByDirOwnership` | all |  |
| manifest.art.dir.enabled-prune-by-entities | Inside an enabled manifest directory, per-entity pruning follows the current entity set | `unit: cmd/sqlgen/manifest: TestCleanStale_RemovedTable` | all |  |
| manifest.art.dir.layout-flip-drop | After a `per_entity` → `single` flip, the `*.json` files sqlgen emits in the per-entity directory are removed, and then the directory itself, but only if nothing else is left in it | `unit: cmd/sqlgen/manifest: TestCleanStale_EntitiesDirDroppedOnLayoutFlip/only sqlgen json inside, directory goes`; `unit: cmd/sqlgen/manifest: TestCleanStale_EntitiesDirDroppedOnLayoutFlip/consumer file inside, directory stays` | all |  |

### Determinism

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.art.det.byte-for-byte | Manifest emission is byte-for-byte deterministic for an unchanged schema and config, with `generated_at` pinned | `golden: postgres`; `golden: graphql`; `golden: mysql`; `golden: sqlite` | all |  |
| manifest.art.det.timestamp-flag | `--manifest-timestamp` fixes `generated_at`: a value passed to the flag is used verbatim | `unit: cmd/sqlgen/cli: TestResolveManifestTimestamp`; `unit: cmd/sqlgen/manifest: TestRunStage_TimestampDeterminism` | all |  |
| manifest.art.det.sorted | `entities[]`, `enums[]`, `extras[]` and, per entity, `indexes[]`, `relationships[]`, `sort.fields` and the column members of `filter.fields` are sorted lexicographically by stable key | `unit: cmd/sqlgen/manifest: TestEmitJSON_KeyedCollectionsSorted` | all |  |
| manifest.art.det.columns-declaration-order | `columns[]` keeps the table's declaration order | `unit: cmd/sqlgen/manifest: TestRunStage_ColumnsKeepDeclarationOrder` | all |  |
| manifest.art.det.methods-generated-order | `methods.query[]` and `methods.mutation[]` follow the order the generated `<Entity>Client` interface declares | `unit: cmd/sqlgen/manifest: TestBuild_MethodsFollowClientInterfaceOrder`; `unit: cmd/sqlgen/manifest: TestManifestMethodsKeepGeneratedOrder` | all |  |
| manifest.art.det.marshal-indent | JSON is written indented (`json.MarshalIndent`) | `unit: cmd/sqlgen/manifest: TestEmitJSON_SingleLayout` | all |  |
| manifest.art.det.no-html-escape | JSON is written with HTML escaping disabled | `unit: cmd/sqlgen/manifest: TestEmitJSON_HTMLEscapingDisabled` | all |  |

### GraphQL breadcrumbs

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.art.graph.no-manifest-dir | The graph package gets breadcrumbs but no `manifest/` directory of its own. Only the models package has one | `golden: graphql` | all |  |
| manifest.art.graph.pointer-models | The graph breadcrumb points at the models manifest through a relative path computed from the resolved graph dir: `../manifest/_index.md` when nested, `../models/manifest/_index.md` when a top-level sibling | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_LayeredRelativePath/top-level sibling graph`; `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_LayeredRelativePath/nested graph under models`; `unit: cmd/sqlgen/manifest: TestRunStage_NestedGraphBreadcrumbs` | all |  |
| manifest.art.graph.pointer-graphqls | The graph breadcrumb points at the `*_gen.graphqls` files in the same directory | `unit: cmd/sqlgen/manifest: TestEmitBreadcrumbs_LayeredGoldens` | all |  |
| manifest.art.graph.pointer-doc-comments | The graph breadcrumb's third pointer is the doc comments on the generated resolver, walker and middleware Go files in that directory | `golden: graphql` | all |  |

## §30.4 JSON Schema
<!-- prd: 30.4 sha256: 799379b751a42b74 -->

### Single layout

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.single.inline-entities | Under `json_layout: single`, `entities[]` carries the full entity objects inline | `unit: cmd/sqlgen/manifest: TestEmitJSON_SingleLayout`; `golden: postgres` | all |  |
| manifest.json.single.envelope | The single-layout document carries `$schema`, `schema_version`, `generated_at`, `generator`, `dialect`, `package`, `conventions`, `generation_config`, `layout`, `entities`, `enums` and `extras` | `unit: cmd/sqlgen/manifest: TestEmitJSON_SingleLayout`; `golden: postgres` | all |  |

### Per-entity layout

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.per-entity.index-pointers | Under `json_layout: per_entity`, `entities[]` holds one lightweight pointer per entity carrying `name`, `table`, `kind` and `file` | `unit: cmd/sqlgen/manifest: TestEmitJSON_PerEntityLayout`; `golden: graphql`; `golden: tenancy_postgres` | all |  |
| manifest.json.per-entity.file-holds-entity | Each pointer's `file` names the file holding that entity's full object | `unit: cmd/sqlgen/manifest: TestEmitJSON_PerEntityLayout`; `unit: cmd/sqlgen/cli: TestManifestDiff_PerEntityLayout` | all |  |
| manifest.json.per-entity.envelope | The per-entity index carries the same top-level fields as the single-layout document | `unit: cmd/sqlgen/manifest: TestEmitJSON_IndexEnvelopeMatchesDocumentTopLevel` | all |  |
| manifest.json.enums-inline.single | `enums[]` is inline in the single layout | `unit: cmd/sqlgen/manifest: TestEmitJSON_SingleLayout`; `golden: postgres` | all |  |
| manifest.json.enums-inline.per-entity | `enums[]` is inline in the per-entity layout | `unit: cmd/sqlgen/manifest: TestEmitJSON_PerEntityLayout`; `golden: graphql` | all |  |
| manifest.json.extras-inline.single | `extras[]` is inline in the single layout | `unit: cmd/sqlgen/manifest: TestEmitJSON_SingleLayout`; `golden: postgres` | all |  |
| manifest.json.extras-inline.per-entity | `extras[]` is inline in the per-entity layout | `unit: cmd/sqlgen/manifest: TestEmitJSON_PerEntityLayout` | all |  |

### Top-level fields

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.top.schema-url | `$schema` is the URL of the JSON Schema for this `schema_version` (`…/cmd/sqlgen/manifest/schema/v1.json`) | `unit: cmd/sqlgen/manifest: TestBuild_TopLevelEnvelope` | all |  |
| manifest.json.top.schema-url-tagged-release | `$schema` is the GitHub raw URL of the latest tagged release | `unit: cmd/sqlgen/manifest: TestEmitJSON_SchemaURLNamesTheTaggedRelease/release 0.42.0`; `unit: cmd/sqlgen/manifest: TestEmitJSON_SchemaURLNamesTheTaggedRelease/release 1.3.7` | all |  |
| manifest.json.top.schema-url-immutable | The `$schema` URL is immutable per version | | all |  |
| manifest.json.top.conforms | An emitted manifest validates against the JSON Schema for its `schema_version`: the single document, the per-entity index and every per-entity record | `unit: cmd/sqlgen/manifest: TestEmitJSON_ValidatesAgainstSchema/single`; `unit: cmd/sqlgen/manifest: TestEmitJSON_ValidatesAgainstSchema/per_entity`; `unit: cmd/sqlgen/cli: TestManifestValidate_E2EExamples` | all |  |
| manifest.json.top.schema-version | `schema_version` is a semver string | `unit: cmd/sqlgen/cli: TestManifestValidate_E2EExamples` | all |  |
| manifest.json.top.generated-at-rfc3339 | Without `--manifest-timestamp`, `generated_at` is an RFC3339 timestamp | `unit: cmd/sqlgen/cli: TestResolveManifestTimestamp` | all |  |
| manifest.json.top.generated-at-rfc3339.pinned | With `--manifest-timestamp` set, `generated_at` is still an RFC3339 timestamp | `unit: cmd/sqlgen/cli: TestGenerate_PinnedManifestTimestampIsGeneratedAt` | all |  |
| manifest.json.top.timestamp-flag-rfc3339 | A `--manifest-timestamp` value that is not RFC3339 is a usage error (exit 2) naming the flag | `unit: cmd/sqlgen/cli: TestGenerate_ManifestTimestampMustBeRFC3339` | all |  |
| manifest.json.top.generated-at-only-nondeterministic | `generated_at` is the only non-deterministic field: with it pinned, two runs produce byte-identical JSON | `unit: cmd/sqlgen/manifest: TestRunStage_TimestampDeterminism`; `golden: postgres` | all |  |
| manifest.json.top.generator | `generator` is `{name, version}` of the emitter (`sqlgen` and its version) | `unit: cmd/sqlgen/manifest: TestBuild_TopLevelEnvelope` | all |  |
| manifest.json.top.dialect.postgres | `dialect` is `"postgres"` for a PostgreSQL package | `unit: cmd/sqlgen/manifest: TestBuild_TopLevelEnvelope`; `golden: postgres` | postgres |  |
| manifest.json.top.dialect.mysql | `dialect` is `"mysql"` for a MySQL package | `golden: mysql` | mysql |  |
| manifest.json.top.dialect.sqlite | `dialect` is `"sqlite"` for a SQLite package | `golden: sqlite` | sqlite |  |
| manifest.json.top.package | `package` is the Go package name of the output (`output.package`) | `unit: cmd/sqlgen/manifest: TestBuild_TopLevelEnvelope` | all |  |
| manifest.json.top.layout.single | `layout` is `"single"` under `json_layout: single` | `unit: cmd/sqlgen/manifest: TestBuild_TopLevelEnvelope`; `golden: postgres` | all |  |
| manifest.json.top.layout.per-entity | `layout` is `"per_entity"` under `json_layout: per_entity` | `golden: graphql`; `golden: tenancy_postgres` | all |  |
| manifest.json.top.conventions | `conventions` is a top-level object (§30.4.1) | `unit: cmd/sqlgen/cli: TestManifestValidate_E2EExamples`; `golden: postgres` | all |  |
| manifest.json.top.generation-config | `generation_config` is a top-level object holding the package's resolved feature-toggle snapshot (§30.4.3) | `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_AllOn`; `unit: cmd/sqlgen/cli: TestManifestValidate_E2EExamples` | all |  |
| manifest.json.top.entities | `entities` lists the package's tables and views | `unit: cmd/sqlgen/manifest: TestBuild_EntityFilesFollowOutputLayout` | all |  |
| manifest.json.top.enums | `enums` lists the generated enum types, named as the generated Go type (§8.3) | `unit: cmd/sqlgen/manifest: TestBuildEnums_namesFollowTheGeneratedGoType` | all |  |
| manifest.json.top.extras.composite | `extras` lists the generated composite types | `golden: postgres`; `golden: introspect_postgres` | postgres |  |
| manifest.json.top.extras.extra | `extras` lists the generated config `extras` types | `unit: cmd/sqlgen/manifest: TestEmitJSON_ExtrasListConfigExtras` | all |  |
| manifest.json.top.extras.domain | `extras` lists the generated domain aliases | `golden: postgres`; `golden: introspect_postgres` | postgres |  |

## §30.4.1 Conventions block
<!-- prd: 30.4.1 sha256: 80f3b67c2be1df07 -->

### `client_entry_points`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.conv.client-entry.query | `conventions.client_entry_points.query` is `client.<Entity>()` | `golden: postgres` | all |  |
| manifest.json.conv.client-entry.mutation | `conventions.client_entry_points.mutation` is `client.<Entity>()` | `golden: postgres` | all |  |

### `error_sentinels[]` table

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.conv.sentinel.not-found | `error_sentinels[]` holds `{name: ErrNotFound, graphql_code: NOT_FOUND, package: <output.package>}` | `golden: postgres`; `unit: cmd/sqlgen/manifest: TestErrorSentinelsMatchGeneratedPackage` | all |  |
| manifest.json.conv.sentinel.empty-filter | `error_sentinels[]` holds `{name: ErrEmptyFilter, graphql_code: "", package: <output.package>}` | `golden: postgres`; `unit: cmd/sqlgen/manifest: TestErrorSentinelsMatchGeneratedPackage` | all |  |
| manifest.json.conv.sentinel.nil-input | `error_sentinels[]` holds `{name: ErrNilInput, graphql_code: "", package: <output.package>}` | `golden: postgres`; `unit: cmd/sqlgen/manifest: TestErrorSentinelsMatchGeneratedPackage` | all |  |
| manifest.json.conv.sentinel.ambiguous-filter | `error_sentinels[]` holds `{name: ErrAmbiguousFilter, graphql_code: "", package: <output.package>}` | `golden: postgres`; `unit: cmd/sqlgen/manifest: TestErrorSentinelsMatchGeneratedPackage` | all |  |
| manifest.json.conv.sentinel.invalid-cursor | `error_sentinels[]` holds `{name: ErrInvalidCursor, graphql_code: "", package: <output.package>}` | `golden: postgres`; `unit: cmd/sqlgen/manifest: TestErrorSentinelsMatchGeneratedPackage` | all |  |
| manifest.json.conv.sentinel.deadlock | `error_sentinels[]` holds `{name: ErrDeadlock, graphql_code: "", package: <output.package>}` | `golden: postgres`; `unit: cmd/sqlgen/manifest: TestErrorSentinelsMatchGeneratedPackage` | all |  |
| manifest.json.conv.sentinel.connection-failed | `error_sentinels[]` holds `{name: ErrConnectionFailed, graphql_code: "", package: <output.package>}` | `golden: postgres`; `unit: cmd/sqlgen/manifest: TestErrorSentinelsMatchGeneratedPackage` | all |  |
| manifest.json.conv.sentinel.constraint-violation | `error_sentinels[]` holds `{name: ErrConstraintViolation, graphql_code: "", package: <output.package>}`: its code is deliberately empty because it maps 1:N through `constraint_error_codes` | `golden: postgres`; `unit: cmd/sqlgen/manifest: TestErrorSentinelsMatchGeneratedPackage` | all |  |
| manifest.json.conv.sentinel.already-related | `error_sentinels[]` holds `{name: ErrAlreadyRelated, graphql_code: CONFLICT, package: <output.package>}` | `golden: postgres`; `unit: cmd/sqlgen/manifest: TestErrorSentinelsMatchGeneratedPackage` | all |  |
| manifest.json.conv.sentinel.nested-verb-conflict | `error_sentinels[]` holds `{name: ErrNestedVerbConflict, graphql_code: INVALID_INPUT, package: <output.package>}` | `golden: postgres`; `unit: cmd/sqlgen/manifest: TestErrorSentinelsMatchGeneratedPackage` | all |  |
| manifest.json.conv.sentinel.missing | With tenancy configured, `error_sentinels[]` holds `{name: ErrMissing, graphql_code: UNAUTHENTICATED, package: tenancy}` | `golden: tenancy_postgres`; `golden: graphql` | all |  |
| manifest.json.conv.sentinel.mismatch | With tenancy configured, `error_sentinels[]` holds `{name: ErrMismatch, graphql_code: FORBIDDEN, package: tenancy}` | `golden: tenancy_postgres`; `golden: graphql` | all |  |

### `error_sentinels[]` rules

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.conv.sentinels.ten-reexports | The generated-package entries are exactly the ten sentinels §22.1 re-exports into the generated package | `unit: cmd/sqlgen/manifest: TestErrorSentinelsMatchGeneratedPackage` | all |  |
| manifest.json.conv.sentinels.tenancy-gated-off | With tenancy off, `ErrMissing` and `ErrMismatch` are absent | `golden: postgres`; `golden: mysql` | all |  |
| manifest.json.conv.sentinels.bare-name | `name` is the bare identifier, never package-qualified | `unit: cmd/sqlgen/manifest: TestErrorSentinelsCarryQualifiedPackages` | all |  |
| manifest.json.conv.sentinels.package-output | `package` is `output.package` for the ten re-exported sentinels | `unit: cmd/sqlgen/manifest: TestErrorSentinelsMatchGeneratedPackage` | all |  |
| manifest.json.conv.sentinels.package-tenancy | `package` is `"tenancy"` for `ErrMissing` and `ErrMismatch` | `golden: tenancy_postgres`; `golden: graphql` | all |  |
| manifest.json.conv.sentinels.feature-missing-qualified | `features.tenancy.missing_resolver_error` carries no `package` field, so it names the sentinel qualified: `tenancy.ErrMissing` | `unit: cmd/sqlgen/manifest: TestErrorSentinelsCarryQualifiedPackages`; `unit: cmd/sqlgen/manifest: TestBuild_TenantedView` | all |  |
| manifest.json.conv.sentinels.feature-mismatch-qualified | `features.tenancy.mismatch_error` names the sentinel qualified: `tenancy.ErrMismatch` | `unit: cmd/sqlgen/manifest: TestErrorSentinelsCarryQualifiedPackages`; `unit: cmd/sqlgen/manifest: TestBuild_TenantedViewJSONOmitsMismatchError` | all |  |
| manifest.json.conv.sentinels.method-errors-tenancy-qualified | `methods[].errors[]` names `tenancy.ErrMissing` and `tenancy.ErrMismatch` qualified | `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_Tenancy`; `unit: cmd/sqlgen/manifest: TestMethodErrorsNameRealSentinels` | all |  |
| manifest.json.conv.sentinels.method-errors-refresh-qualified | `methods[].errors[]` names `database.ErrRefreshConcurrentlyInTx` qualified | `unit: cmd/sqlgen/manifest: TestBuild_MaterializedViewEntity` | postgres |  |
| manifest.json.conv.sentinels.method-errors-bare | A sentinel the generated package declares stays bare in `methods[].errors[]` (`ErrNotFound`, `ErrConstraintViolation`) | `unit: cmd/sqlgen/manifest: TestMethodErrorsNameRealSentinels`; `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards` | all |  |
| manifest.json.conv.sentinels.no-derived-names-generated | No generated-package `name` is a `graphql_code` PascalCased into an identifier (`ErrConflict`, `ErrBadReference`): every such name is one the generated package declares | `unit: cmd/sqlgen/manifest: TestErrorSentinelsMatchGeneratedPackage`; `unit: cmd/sqlgen/manifest: TestMethodErrorsNameRealSentinels` | all |  |
| manifest.json.conv.sentinels.no-derived-names-tenancy | The tenancy entries are named `ErrMissing` / `ErrMismatch`, not a PascalCased code (`ErrUnauthenticated`) | `golden: tenancy_postgres` | all |  |

### `constraint_error_codes`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.conv.constraint-codes.unique | `constraint_error_codes.unique` is `CONFLICT` | `golden: postgres`; `golden: mysql`; `golden: sqlite` | all |  |
| manifest.json.conv.constraint-codes.foreign-key | `constraint_error_codes.foreign_key` is `BAD_REFERENCE` | `golden: postgres`; `golden: mysql`; `golden: sqlite` | all |  |
| manifest.json.conv.constraint-codes.check | `constraint_error_codes.check` is `INVALID_INPUT` | `golden: postgres`; `golden: mysql`; `golden: sqlite` | all |  |
| manifest.json.conv.constraint-codes.not-null | `constraint_error_codes.not_null` is `INVALID_INPUT` | `golden: postgres`; `golden: mysql`; `golden: sqlite` | all |  |

### Other conventions members

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.conv.find-returns-nil | `find_returns_nil_on_missing` is `false` | `golden: postgres` | all |  |
| manifest.json.conv.pagination.page-type | `pagination.page_type` is `Page` | `golden: postgres` | all |  |
| manifest.json.conv.pagination.list-envelope-suffix | `pagination.list_envelope_suffix` is `List` | `golden: postgres` | all |  |
| manifest.json.conv.pagination.cursor-encoding | `pagination.cursor_encoding` is `opaque-base64` | `golden: postgres` | all |  |
| manifest.json.conv.call-options | `conventions` carries a `call_options` block | `unit: cmd/sqlgen/manifest: TestCallOptionsFieldsMatchGeneratedStruct` | all |  |
| manifest.json.conv.soft-delete | `conventions` carries a `soft_delete` block | `golden: postgres` | all |  |
| manifest.json.conv.comparator | `conventions.comparator` carries `package: "comparator"` with `families`, `shape_notes`, `composition` and `examples` | `golden: postgres` | all |  |
| manifest.json.conv.omittable.package | `omittable.package` is `omittable` | `unit: cmd/sqlgen/manifest: TestBuild_ConventionsMirrorRuntimeSurface` | all |  |
| manifest.json.conv.omittable.type | `omittable.type` is `omittable.Value[T]` | `unit: cmd/sqlgen/manifest: TestBuild_ConventionsMirrorRuntimeSurface` | all |  |
| manifest.json.conv.omittable.purpose | `omittable.purpose` is the PRD's "Distinguishes 'not set' from 'set to zero value'. Used in <Entity>Update structs and partial-update contexts." | `unit: cmd/sqlgen/manifest: TestBuild_ConventionsMirrorRuntimeSurface` | all |  |
| manifest.json.conv.omittable.construction | `omittable.construction` lists `omittable.Set(value) — set to a value` and `omittable.Omit[T]() — explicitly unset (zero state)` | `unit: cmd/sqlgen/manifest: TestBuild_ConventionsMirrorRuntimeSurface` | all |  |
| manifest.json.conv.omittable.methods | `omittable.methods` lists `IsSet() bool`, `IsZero() bool`, `Get() (T, bool)`, `MustGet() T` | `unit: cmd/sqlgen/manifest: TestBuild_ConventionsMirrorRuntimeSurface` | all |  |
| manifest.json.conv.omittable.json-behavior | `omittable.json_behavior` is the PRD's "Marshals to the wrapped value when set; omitted when unset (omitzero semantics on the wrapper, via IsZero)" | `unit: cmd/sqlgen/manifest: TestBuild_ConventionsMirrorRuntimeSurface` | all |  |

## §30.4.2 Entity shape
<!-- prd: 30.4.2 sha256: 1738798b7f9e1de2 -->

### Entity fields

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.entity.kind-table | A table entity has `kind: "table"` | `unit: cmd/sqlgen/manifest: TestBuild_SinglePKTable` | all |  |
| manifest.json.entity.name | `name` is the generated struct name | `unit: cmd/sqlgen/manifest: TestBuild_SinglePKTable` | all |  |
| manifest.json.entity.table | `table` is the bare SQL name | `golden: postgres` | all |  |
| manifest.json.entity.schema | `schema` carries the entity's schema beside the bare `table` | `golden: postgres` | all |  |
| manifest.json.entity.file-prefix | `file_prefix` is the entity's singular snake_case stem (`users` → `user`) | `unit: cmd/sqlgen/manifest: TestBuild_EntityFilesFollowOutputLayout` | all |  |
| manifest.json.entity.pk-single | A single-column key is `pk: {kind: "single", columns: [...]}` | `unit: cmd/sqlgen/manifest: TestBuild_SinglePKTable` | all |  |
| manifest.json.entity.features-block | `features` always carries `soft_delete`, `cache`, `events` and `tenancy`, `null` when the feature is off | `golden: postgres` | all |  |
| manifest.json.entity.audit-columns-omitted | `features.audit_columns` is omitted when the entity has no audit column | `golden: postgres` | all |  |
| manifest.json.entity.table-tenancy | A tenanted table populates `features.tenancy` with its tenant column | `unit: cmd/sqlgen/manifest: TestBuild_TenantedTable` | all |  |
| manifest.json.entity.columns | Each column carries `name`, `go_field`, `go_type`, `db_type`, `nullable`, `pk`, `unique`, `comparator` and `comment` | `golden: postgres` | all |  |
| manifest.json.entity.columns.default-auto | A column's `default`, `default_kind` and `auto` are omitted when empty | `golden: postgres` | all |  |
| manifest.json.entity.comment-always | `comment` is always written, `""` when there is none | `unit: cmd/sqlgen/manifest: TestEmitJSON_CommentAlwaysWritten/table without comments`; `unit: cmd/sqlgen/manifest: TestEmitJSON_CommentAlwaysWritten/view without comments`; `unit: cmd/sqlgen/manifest: TestEmitJSON_CommentAlwaysWritten/table with comments` | all |  |
| manifest.json.entity.column-access-emitted | `access` and `redacted` are emitted for a non-public column | `unit: cmd/sqlgen/manifest: TestEmitJSON_AccessMarkers_BothLayouts/single layout`; `unit: cmd/sqlgen/manifest: TestEmitJSON_AccessMarkers_BothLayouts/per_entity layout` | all |  |
| manifest.json.entity.column-access-public-omitted | A public column omits `access` | `unit: cmd/sqlgen/manifest: TestValidateAgainstSchema_AccessFields` | all |  |
| manifest.json.entity.column-redacted-public-omitted | A public column omits `redacted` | `golden: graphql` | all |  |
| manifest.json.entity.relationship-fields | Each relationship carries `name`, `kind` and `target_entity` | `unit: cmd/sqlgen/manifest: TestBuild_M2MRelationship`; `unit: cmd/sqlgen/manifest: TestBuild_M2MJunctionSchema` | all |  |
| manifest.json.entity.relationship-kind-o2o-m2o | A one-to-one edge is `o2o` on the FK-holding side and `m2o` on the referenced side | `unit: cmd/sqlgen/manifest: TestBuild_O2OSideDisambiguation` | all |  |
| manifest.json.entity.methods-groups | `methods` splits into `query[]` and `mutation[]` | `unit: cmd/sqlgen/manifest: TestBuild_MaterializedViewEntity`; `golden: postgres` | all |  |
| manifest.json.entity.method-fields | Each method carries `name`, `params[]` of `{name, type}`, `returns` and `errors[]`, with optional `notes` and `sql_bodies` | `golden: postgres` | all |  |
| manifest.json.entity.filter | `filter` is `{type: "<Entity>Filter", fields: [...]}` | `golden: postgres` | all |  |
| manifest.json.entity.sort | `sort` is `{type: "<Entity>Sort", fields: [...]}` | `golden: postgres` | all |  |
| manifest.json.entity.examples | `examples.read` and `examples.write` are arrays of strings, each a line of Go code | `unit: cmd/sqlgen/manifest: TestBuild_EntityExampleLines`; `unit: cmd/sqlgen: TestManifestExampleLinesCompile` | all |  |
| manifest.json.entity.examples.read | `examples.read` holds a `Get` call (when `Get` is generated) and a `GetMany` call filtering on a real column | `unit: cmd/sqlgen/manifest: TestBuild_EntityExampleLines/User`; `unit: cmd/sqlgen/manifest: TestBuild_EntityExampleLines/OrderItem`; `unit: cmd/sqlgen/manifest: TestBuild_EntityExampleLines/ActiveUser`; `unit: cmd/sqlgen/manifest: TestBuild_EntityExampleLines` | all |  |
| manifest.json.entity.examples.write | `examples.write` holds a `Create` call and an `Update` call (when generated) | `unit: cmd/sqlgen/manifest: TestBuild_EntityExampleLines/User`; `unit: cmd/sqlgen/manifest: TestBuild_EntityExampleLines/OrderItem`; `unit: cmd/sqlgen/manifest: TestBuild_EntityExampleLines` | all |  |
| manifest.json.entity.examples.view | A view's `examples` carries `read` lines only | `unit: cmd/sqlgen/manifest: TestBuild_EntityExampleLines/ActiveUser`; `unit: cmd/sqlgen/manifest: TestBuild_EntityExampleLines` | all |  |
| manifest.json.entity.examples.real-surface | Example lines are built from the entity's generated methods, fields and types | `unit: cmd/sqlgen: TestManifestExampleLinesCompile`; `unit: cmd/sqlgen/manifest: TestBuild_EntityExampleUUIDConstructor`; `unit: cmd/sqlgen/manifest: TestBuild_EntityExampleIDFilterLiteral` | all |  |

### View entries

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.view.kind | A view entry has `kind: "view"` | `unit: cmd/sqlgen/manifest: TestBuild_ViewEntity` | all |  |
| manifest.json.view.pk-none | A view with no declared key has `pk.kind: "none"` | `unit: cmd/sqlgen/manifest: TestBuild_ViewEntity` | all |  |
| manifest.json.view.relationships-empty | A view has `relationships: []` | `golden: postgres` | all |  |
| manifest.json.view.source-files | Under `input.source: files`, a view's `source` is its annotation file's text | `golden: postgres`; `golden: sqlite` | all |  |
| manifest.json.view.source-database | Under `input.source: database`, a view's `source` is the definition the database stores | `integration: cmd/sqlgen/cli: TestGenerate_ManifestViewSourceIsStoredDefinitionUnderDatabase` | all |  |
| manifest.json.matview.materialized | A materialized view carries `materialized: true` | `unit: cmd/sqlgen/manifest: TestBuild_MaterializedViewEntity`; `unit: cmd/sqlgen/manifest: TestBuild_MaterializedMarkerJSONShape` | postgres |  |
| manifest.json.matview.materialized-omitted | `materialized` is omitted on every other entity | `unit: cmd/sqlgen/manifest: TestBuild_MaterializedMarkerJSONShape`; `golden: postgres` | postgres |  |
| manifest.json.matview.refresh | A materialized view advertises `Refresh` under `methods.mutation[]` with `sql_bodies` `REFRESH MATERIALIZED VIEW <name>` | `unit: cmd/sqlgen/manifest: TestBuild_MaterializedViewEntity` | postgres |  |
| manifest.json.matview.refresh-concurrently | A concurrently refreshable materialized view advertises `RefreshConcurrently` with `sql_bodies` `REFRESH MATERIALIZED VIEW CONCURRENTLY <name>` | `unit: cmd/sqlgen/manifest: TestBuild_MaterializedViewEntity` | postgres |  |
| manifest.json.matview.refresh-concurrently-error | `RefreshConcurrently` lists `database.ErrRefreshConcurrentlyInTx` in `errors` | `unit: cmd/sqlgen/manifest: TestBuild_MaterializedViewEntity` | postgres |  |

### Tenanted views

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.view-tenancy.populated | A view carrying the effective tenant column populates `features.tenancy` | `unit: cmd/sqlgen/manifest: TestBuild_TenantedView`; `unit: cmd/sqlgen/manifest: TestBuild_ViewTenancyRespectsPerViewOverrides`; `golden: tenancy_postgres` | all |  |
| manifest.json.view-tenancy.mode | A view's `features.tenancy.mode` is always `"auto-filter"` | `unit: cmd/sqlgen/manifest: TestBuild_TenantedView`; `golden: tenancy_postgres` | all |  |
| manifest.json.view-tenancy.mode-pk-annotation | A view whose tenant column carries `@pk` is still `"auto-filter"` | `unit: cmd/sqlgen/manifest: TestBuild_TenantedViewPKColumnStaysAutoFilter` | all |  |
| manifest.json.view-tenancy.mismatch-omitted | A view's `features.tenancy` omits `mismatch_error` (the key is absent) | `unit: cmd/sqlgen/manifest: TestBuild_TenantedViewJSONOmitsMismatchError`; `golden: tenancy_postgres` | all |  |
| manifest.json.view-tenancy.validates | The shorter view `features.tenancy` object validates against the schema unchanged | `unit: cmd/sqlgen/cli: TestManifestValidate_E2EExamples/tenancy_postgres` | all |  |
| manifest.json.view-tenancy.column | A view's `features.tenancy.column` carries the same value a table's does | `unit: cmd/sqlgen/manifest: TestBuild_TenantedView` | all |  |
| manifest.json.view-tenancy.missing-resolver | A view's `features.tenancy.missing_resolver_error` carries the same value a table's does (`tenancy.ErrMissing`) | `unit: cmd/sqlgen/manifest: TestBuild_TenantedView` | all |  |

### Other per-entity features on a view

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.view.events-null | A view's `features.events` is always `null` | `golden: postgres`; `golden: tenancy_postgres` | all |  |
| manifest.json.view.soft-delete-null | A view's `features.soft_delete` is always `null` | `unit: cmd/sqlgen/manifest: TestBuild_TenantedView`; `golden: postgres` | all |  |
| manifest.json.view.cache-null | A view's `features.cache` is `null`, even on a cached view | `golden: tenancy_postgres` | all |  |

### `files[]`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.files.single-table | Under the default `single_file` layout a table's `files[]` is `["models_gen.go"]` | `unit: cmd/sqlgen/manifest: TestBuild_EntityFilesFollowOutputLayout/single_file concatenates into models_gen.go and views_gen.go`; `unit: cmd/sqlgen/manifest: TestBuild_EntityFilesFollowOutputLayout/unset layout resolves to the single_file default`; `golden: postgres` | all |  |
| manifest.json.files.single-view | Under `single_file` a view's `files[]` is `["views_gen.go"]` | `unit: cmd/sqlgen/manifest: TestBuild_EntityFilesFollowOutputLayout/single_file concatenates into models_gen.go and views_gen.go`; `unit: cmd/sqlgen/manifest: TestBuild_EntityFilesFollowOutputLayout/unset layout resolves to the single_file default`; `golden: postgres` | all |  |
| manifest.json.files.per-table-table | Under `file_per_table` a table's `files[]` is `["<file_prefix>_gen.go"]` | `unit: cmd/sqlgen/manifest: TestBuild_EntityFilesFollowOutputLayout/file_per_table gives each entity its own snake-cased file`; `golden: sqlite` | all |  |
| manifest.json.files.per-table-view | Under `file_per_table` a view's `files[]` is `["<file_prefix>_gen.go"]` | `unit: cmd/sqlgen/manifest: TestBuild_EntityFilesFollowOutputLayout/file_per_table gives each entity its own snake-cased file`; `golden: sqlite` | all |  |
| manifest.json.files.resolves | `files[]` names the file `gen` wrote, so it resolves under either layout | `unit: cmd/sqlgen/manifest: TestEntityFilesExistOnDisk` | all |  |
| manifest.json.files.body-only | `files[]` names only the entity's body file: package-wide artifacts (`sorter_gen.go`, `cache_gen.go`, `event_hooks_gen.go`, `*_gen.graphqls`) are not listed | `golden: postgres`; `golden: graphql` | all |  |
| manifest.json.files.per-table-view-file | Under `file_per_table`, each view's body is written to its own `<file_prefix>_gen.go` and no `views_gen.go` is written | `unit: cmd/sqlgen/cli: TestFilePerTableLayout_views` | all |  |

### Composite keys

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.pk.composite-kind | A composite-key entity has `pk.kind: "composite"` | `unit: cmd/sqlgen/manifest: TestBuild_CompositePKTable` | all |  |
| manifest.json.pk.composite-struct | A composite-key entity's `pk.struct` names the generated `<Entity>PK` struct | `unit: cmd/sqlgen/manifest: TestBuild_CompositePKTable` | all |  |

### Relationship `fk` and `junction`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.rel.fk-o2o-o2m | An `o2o` or `o2m` edge carries `fk` | `unit: cmd/sqlgen/manifest: TestBuild_FKSchema`; `unit: cmd/sqlgen/manifest: TestBuild_O2MFKNotOnTargetStaysOnTarget` | all |  |
| manifest.json.rel.fk-m2o | An `m2o` edge carries `fk` | `unit: cmd/sqlgen/manifest: TestEmitJSON_ManyToOneEdgeCarriesFK` | all |  |
| manifest.json.rel.m2m-junction | An `m2m` edge carries `junction` | `unit: cmd/sqlgen/manifest: TestBuild_M2MRelationship`; `unit: cmd/sqlgen/manifest: TestBuild_M2MJunctionSchema` | all |  |
| manifest.json.rel.m2m-no-fk | An `m2m` edge carries no `fk` | `golden: postgres` | all |  |
| manifest.json.rel.fk-table-o2m | On an `o2m` edge `fk.table` is the target's table | `unit: cmd/sqlgen/manifest: TestBuild_FKSchema`; `unit: cmd/sqlgen/manifest: TestBuild_O2MFKNotOnTargetStaysOnTarget` | all |  |
| manifest.json.rel.fk-table-has-one | On a has-one `o2o` edge `fk.table` is the target's table | `golden: postgres` | all |  |
| manifest.json.rel.fk-table-belongs-to | On a belongs-to edge `fk.table` is the entity's own table | `unit: cmd/sqlgen/manifest: TestBuild_FKSchema` | all |  |
| manifest.json.rel.junction-table | `junction.table` names the M2M junction | `unit: cmd/sqlgen/manifest: TestBuild_M2MRelationship`; `unit: cmd/sqlgen/manifest: TestBuild_M2MJunctionSchema` | all |  |
| manifest.json.rel.fk-schema | `fk.table` is the bare name, with `schema` beside it when sqlgen resolved one | `unit: cmd/sqlgen/manifest: TestBuild_FKSchema` | all |  |
| manifest.json.rel.junction-schema | `junction.table` is the bare name, with `schema` beside it when sqlgen resolved one | `unit: cmd/sqlgen/manifest: TestBuild_M2MJunctionSchema` | all |  |
| manifest.json.rel.columns-references | `fk.columns` and `fk.references` list the key's pairs in the foreign key's declared order, `columns[i]` referencing `references[i]` | | all |  |
| manifest.json.rel.single-column-lists | A single-column edge has one-item `columns` and `references` lists | | all |  |
| manifest.json.rel.references-target-key | `references` names the referenced columns: the target's primary key or one of its UNIQUE constraints | | all |  |
| manifest.json.rel.references-manual | For a manual relationship `references` is the referenced primary key | | all |  |
| manifest.json.rel.junction-local-fk | `junction.local_fk` is the junction's key into this entity (`columns`, `references`, `constraint`) | | all |  |
| manifest.json.rel.junction-target-fk | `junction.target_fk` is the junction's key into the target (`columns`, `references`, `constraint`) | | all |  |
| manifest.json.rel.constraint | `constraint` names the FOREIGN KEY constraint the edge was detected from | | all |  |
| manifest.json.rel.constraint-sqlite-unnamed | An unnamed SQLite foreign key is named `table(columns) → target(columns)` | | sqlite |  |
| manifest.json.rel.constraint-manual-omitted | A manual relationship omits `constraint` | | all |  |

### `methods[]`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.methods.exact-client-surface | An entity's `methods.query[]` + `methods.mutation[]` is exactly the method set on its generated `<Entity>Client` interface | `unit: cmd/sqlgen/manifest: TestMethodNamesMatchGeneratedClients` | all |  |
| manifest.json.methods.gate-soft-delete | `SoftDelete*` / `Restore*` are listed only for an entity with a detected soft-delete column | `unit: cmd/sqlgen/manifest: TestMethodNamesMatchGeneratedClients` | all |  |
| manifest.json.methods.gate-increment | `Increment` is listed only for an entity with at least one increment-eligible column | `unit: cmd/sqlgen/manifest: TestMethodNamesMatchGeneratedClients` | all |  |
| manifest.json.methods.gate-upsert | `Upsert` / `UpsertMany` are listed on a conflict target | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_RuntimeClausesAreTokens`; `unit: cmd/sqlgen/manifest: TestMethodNamesMatchGeneratedClients` | all |  |
| manifest.json.methods.gate-nested | The `…WithRelated` methods are listed only under `nested_mutations` | `unit: cmd/sqlgen/manifest: TestMethodNamesMatchGeneratedClients`; `golden: graphql`; `golden: mysql` | all |  |
| manifest.json.methods.rest-unconditional | The other table methods are listed unconditionally | `unit: cmd/sqlgen/manifest: TestMethodNamesMatchGeneratedClients` | all |  |
| manifest.json.methods.view-get | A view lists `Get` when it carries a `@pk` annotation | `unit: cmd/sqlgen/manifest: TestMethodNamesMatchGeneratedClients`; `unit: cmd/sqlgen/manifest: TestBuild_TenantedViewMethodErrors` | all |  |
| manifest.json.methods.view-reads | A view lists `GetMany`, `Count` and `Paginate` | `unit: cmd/sqlgen/manifest: TestMethodNamesMatchGeneratedClients`; `unit: cmd/sqlgen/manifest: TestBuild_TenantedViewMethodErrors` | all |  |
| manifest.json.methods.view-connection | A view lists `Connection` when its cursor keys resolve | `unit: cmd/sqlgen/manifest: TestMethodNamesMatchGeneratedClients`; `unit: cmd/sqlgen/manifest: TestBuild_TenantedViewMethodErrors` | all |  |
| manifest.json.methods.no-invented | No method the templates do not emit appears (`FindByID`, `FindBy<Col>`, `List`, `Walk`, `Delete`, `Upsert<Target>`) | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_CoverEveryMethodType`; `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_RuntimeClausesAreTokens`; `unit: cmd/sqlgen/manifest: TestBuild_PartialUniqueIndex`; `unit: cmd/sqlgen/manifest: TestMethodNamesMatchGeneratedClients` | all |  |
| manifest.json.methods.no-omitted | No generated method is omitted: `Increment`, `ExistsWhere`, `Paginate`, `Connection`, `CreateMany`, `UpdateMany`, `UpdateWhere`, `Restore` and the six `*Many` / `*Where` delete and restore forms appear under their conditions | `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards`; `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_CoverEveryMethodType`; `unit: cmd/sqlgen/manifest: TestMethodNamesMatchGeneratedClients` | all |  |
| manifest.json.methods.types | Param and return types are read from the templates: every type they name is one the generated package declares | `unit: cmd/sqlgen/manifest: TestMethodParamTypesResolve` | all |  |

### Signature convention and `errors[]`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.sig.params-omit-ctx | `params` omits the leading `ctx context.Context` | `golden: postgres` | all |  |
| manifest.json.sig.params-omit-opts | `params` omits the trailing variadic `opts ...func(*CallOptions[…])` | `golden: postgres` | all |  |
| manifest.json.sig.returns | `returns` names the non-error result | `golden: postgres` | all |  |
| manifest.json.sig.returns-empty | `returns` is `""` when the method returns only `error` | `golden: postgres` | all |  |
| manifest.json.errors.reachable | `errors[]` lists the sentinels the generated body can actually reach | `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards`; `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_Tenancy`; `unit: cmd/sqlgen/manifest: TestBuild_TenantedViewMethodErrors` | all |  |
| manifest.json.errors.get | `Get` carries `ErrNotFound` | `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards` | all |  |
| manifest.json.errors.by-pk-deletes | The idempotent by-PK deletes carry no sentinel | `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards/strict updates on` | all |  |
| manifest.json.errors.restore | `Restore` carries no sentinel | `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards/strict updates on` | all |  |
| manifest.json.errors.update-strict | `Update` carries `ErrNotFound` only under `strict_updates` | `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards/strict updates on`; `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards/strict updates off` | all |  |
| manifest.json.errors.increment-strict | `Increment` carries `ErrNotFound` only under `strict_updates` | `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards/strict updates on`; `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards/strict updates off` | all |  |
| manifest.json.errors.where | The mutating `*Where` methods (`UpdateWhere` and the delete and restore forms) carry `ErrEmptyFilter` | `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards/strict updates on` | all |  |
| manifest.json.errors.exists-where | `ExistsWhere` does not carry `ErrEmptyFilter` | `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards/strict updates on` | all |  |
| manifest.json.errors.connection | `Connection` carries `ErrInvalidCursor` | `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards/strict updates on`; `unit: cmd/sqlgen/manifest: TestBuild_TenantedViewMethodErrors` | all |  |
| manifest.json.errors.update-many | `UpdateMany` does not carry `ErrNotFound` under any setting | `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards/strict updates on`; `unit: cmd/sqlgen/manifest: TestBuild_MethodErrors_MatchTemplateGuards/strict updates off` | all |  |

### Per-method `sql_bodies`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.sql.per-method | Each method under `methods.query[]` and `methods.mutation[]` other than the nested-mutation `…WithRelated` methods carries `sql_bodies: {<dialect>: <canonical SQL>}` describing the statement that method issues | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_CoverEveryMethodType`; `golden: postgres` | all |  |
| manifest.json.sql.per-method.with-related | `CreateWithRelated`, `UpdateWithRelated` and `UpsertWithRelated` carry no `sql_bodies` key | `golden: mysql` | all |  |
| manifest.json.sql.single-key | A single-dialect package's `sql_bodies` is a single-key map | `golden: postgres`; `golden: mysql`; `golden: sqlite` | all |  |
| manifest.json.sql.placeholder-postgres | Placeholders render as `$N` on PostgreSQL | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_RuntimeClausesAreTokens`; `golden: postgres` | postgres |  |
| manifest.json.sql.placeholder-mysql | Placeholders render as `?` on MySQL | `golden: mysql` | mysql |  |
| manifest.json.sql.placeholder-sqlite | Placeholders render as `?` on SQLite | `golden: sqlite` | sqlite |  |
| manifest.json.sql.from-pipeline | `sql_bodies` is captured from the SQL-build pipeline that emits the `_gen.go` query bodies: it is the statement the generated method sends | `unit: cmd/sqlgen/cli: TestGenerate_ManifestSQLBodiesAreTheStatementsMethodsSend/postgres`; `unit: cmd/sqlgen/cli: TestGenerate_ManifestSQLBodiesAreTheStatementsMethodsSend/mysql`; `unit: cmd/sqlgen/cli: TestGenerate_ManifestSQLBodiesAreTheStatementsMethodsSend/sqlite` | all |  |
| manifest.json.sql.no-statement-no-key | `Paginate` and `Connection`, which issue no statement of their own, carry no `sql_bodies` key | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_CoverEveryMethodType`; `golden: postgres` | all |  |
| manifest.json.sql.token.filter | The caller's filter conditions render as `<filter>` | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_CoverEveryMethodType` | all |  |
| manifest.json.sql.token.sort | The caller's `ORDER BY` renders as `<sort>` | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_CoverEveryMethodType` | all |  |
| manifest.json.sql.token.limit | The runtime row limit renders as `<limit>`, not `$N` | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_CoverEveryMethodType` | all |  |
| manifest.json.sql.token.set | The `UPDATE SET` assignments render as `<set>` | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_CoverEveryMethodType`; `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_RuntimeClausesAreTokens` | all |  |
| manifest.json.sql.token.columns | The `INSERT` column list renders as `<columns>` | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_CoverEveryMethodType`; `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_RuntimeClausesAreTokens` | all |  |
| manifest.json.sql.token.values | The `INSERT` value list renders as `<values>` | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_CoverEveryMethodType`; `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_RuntimeClausesAreTokens` | all |  |
| manifest.json.sql.token.values-create-many | On `CreateMany`, `<values>` is the row list (`VALUES <values>`) | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_CoverEveryMethodType` | all |  |
| manifest.json.sql.token.conflict-target | The `Upsert` conflict columns render as `<conflict_target>` | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_RuntimeClausesAreTokens`; `golden: sqlite` | postgres, sqlite |  |
| manifest.json.sql.token.excluded | The `DO UPDATE SET` / `ON DUPLICATE KEY UPDATE` assignments render as `<excluded>` | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_RuntimeClausesAreTokens`; `golden: mysql` | all |  |
| manifest.json.sql.token.column | `Increment`'s target column renders as `<column>` | `golden: postgres`; `golden: mysql`; `golden: sqlite` | all |  |
| manifest.json.sql.token.ids | A single-column key's `*Many` PK list renders as `<ids>` | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_CoverEveryMethodType` | all |  |
| manifest.json.sql.token.pks | A composite key's `*Many` PK list renders as `<pks>` | `golden: postgres`; `golden: mysql`; `golden: sqlite` | all |  |

## §30.4.3 Generation-config snapshot
<!-- prd: 30.4.3 sha256: 365671ecb59c85ce -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.json.gc.cache | `generation_config.cache` is `true` if any table has cache configured | `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfigCacheFollowsPerTableResolution` | all |  |
| manifest.json.gc.tenancy | `generation_config.tenancy` is `true` if a tenant column is configured for any table | `unit: cmd/sqlgen/manifest: TestBuild_TenantedTable`; `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_OffByDefault`; `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_AllOn` | all |  |
| manifest.json.gc.events | `generation_config.events` is `true` if any table has event emission enabled | `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfigEventsFollowsPerTableResolution` | all |  |
| manifest.json.gc.soft-delete | `generation_config.soft_delete` is `true` if any table uses soft-delete | `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_OffByDefault`; `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_AllOn` | all |  |
| manifest.json.gc.views | `generation_config.views` is `true` if any view is generated | `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfigViewsFollowsGeneratedViews` | all |  |
| manifest.json.gc.graphql | `generation_config.graphql` is `true` if GraphQL output is enabled | `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_OffByDefault`; `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_AllOn`; `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_GraphTopLevel` | all |  |
| manifest.json.gc.audit-columns | `generation_config.audit_columns` is `true` if any table has audit columns declared | `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_OffByDefault`; `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_AllOn` | all |  |
| manifest.json.gc.graph-top-level | `generation_config.graph_top_level` is `true` if the resolved graph dir is not under `output.dir` | `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_GraphTopLevel`; `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_GraphTopLevel_DisabledAPI`; `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_AllOn` | all |  |
| manifest.json.gc.graph-top-level-inferred | `graph_top_level` is inferred from `schema_dir` / `resolver_dir`, not a stored flag | `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_GraphTopLevel` | all |  |
| manifest.json.gc.no-pagination | `generation_config` has no `pagination` field | `golden: postgres` | all |  |
| manifest.json.gc.booleans-only | `generation_config` is a boolean surface only; it does not echo `sqlgen.yml` | `golden: postgres`; `golden: graphql` | all |  |
| manifest.json.gc.no-secrets | `generation_config` carries no secrets, DSNs, file paths or per-table detail | `golden: graphql`; `golden: introspect_postgres` | all |  |

## §30.5 Versioning Policy
<!-- prd: 30.5 sha256: 4a0f7199a7f092d4 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.ver.semver | `schema_version` follows semver | `unit: cmd/sqlgen/cli: TestManifestValidate_E2EExamples`; `unit: cmd/sqlgen/manifest: TestEmitJSON_ValidatesAgainstSchema/single` | all |  |
| manifest.ver.independent | `schema_version` is independent of sqlgen's module version | `unit: cmd/sqlgen/manifest: TestBuild_TopLevelEnvelope` | all |  |
| manifest.ver.breaking-major | A breaking change to entity or convention shape bumps the major version | | all |  |
| manifest.ver.optional-minor | Adding optional fields bumps the minor version | | all |  |
| manifest.ver.consumers-tolerate | Consumers tolerate added optional fields: the schema accepts an unknown field and a shape without a later optional field | `unit: cmd/sqlgen/manifest: TestEmitJSON_SchemaToleratesUnknownTopLevelField`; `unit: cmd/sqlgen/manifest: TestValidateAgainstSchema_AccessFields` | all |  |
| manifest.ver.patch | Bug fixes and clarifications bump the patch version | | all |  |
| manifest.ver.schema-file | Every breaking version preserves its JSON Schema at `cmd/sqlgen/manifest/schema/v<N>.json` | `unit: cmd/sqlgen/manifest: TestEmitJSON_ValidatesAgainstSchema/single` | all |  |
| manifest.ver.emits-latest | Generation emits the latest schema version (`$schema` names the newest `v<N>.json`) | `unit: cmd/sqlgen/manifest: TestBuild_TopLevelEnvelope` | all |  |
| manifest.ver.pin-schema-url | Consumers pin via the `$schema` URL: `manifest validate` validates against the schema that URL names | `unit: cmd/sqlgen/cli: TestManifestValidate_UsesSchemaAtManifestURL/a manifest the embedded schema accepts fails the schema at its URL`; `unit: cmd/sqlgen/cli: TestManifestValidate_UsesSchemaAtManifestURL/a manifest the embedded schema rejects passes the schema at its URL` | all |  |
| manifest.ver.zero-x | `schema_version` ships at `0.x` until the shape is frozen | `golden: postgres` | all |  |
| manifest.ver.zero-x-minor-breaking | While `0.x`, breaking changes are permitted under a minor bump | | all |  |
| manifest.ver.0-2-0 | `schema_version` is `0.2.0`: every relationship's `fk` and `junction` take the key-list shape of §30.4.2, with `constraint` | | all |  |
| manifest.ver.1-0-0-freeze | The `1.0.0` freeze requires the `schema/v1.json` conventions tightening: the `call_options` / `soft_delete` / `comparator` / `omittable` sub-blocks take their final shape and `conventions.required` is tightened | | all |  |

## §30.7 Extended Schema Metadata
<!-- prd: 30.7 sha256: 4b718a67e82c6273 -->

### `comment`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.meta.comment.table | The entity `comment` carries `COMMENT ON TABLE` | `unit: cmd/sqlgen/manifest: TestBuild_ColumnAndTableComments`; `golden: postgres` | all |  |
| manifest.meta.comment.column | Each column's `comment` carries `COMMENT ON COLUMN` | `unit: cmd/sqlgen/manifest: TestBuild_ColumnAndTableComments`; `golden: postgres` | all |  |
| manifest.meta.comment.pg-introspector | On PostgreSQL the introspector sources comments from `pg_description` | `golden: introspect_postgres` | postgres |  |
| manifest.meta.comment.mysql-introspector | On MySQL the introspector sources comments from `information_schema` | `golden: introspect_mysql` | mysql |  |
| manifest.meta.comment.file-parser | The file parser sources comments from the DDL | `golden: postgres`; `golden: mysql` | postgres, mysql |  |
| manifest.meta.comment.empty | `comment` is the empty string when there is no comment | `unit: cmd/sqlgen/manifest: TestEmitJSON_SingleLayout`; `golden: sqlite` | all |  |

### `indexes[]`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.meta.indexes.unique | `indexes[]` lists declared UNIQUE indexes | `unit: cmd/sqlgen/manifest: TestBuild_IndexesIncludePKAndUnique` | all |  |
| manifest.meta.indexes.non-unique | `indexes[]` lists declared non-unique indexes | `unit: cmd/sqlgen/manifest: TestBuild_IndexNamesReadTheSchema/postgres names` | all |  |
| manifest.meta.indexes.partial | `indexes[]` lists partial indexes | `unit: cmd/sqlgen/manifest: TestBuild_PartialUniqueIndex`; `golden: postgres` | all |  |
| manifest.meta.indexes.gin | `indexes[]` lists GIN indexes with `method: "gin"` | `unit: cmd/sqlgen/manifest: TestBuild_GINIndexMethod` | postgres |  |
| manifest.meta.indexes.gist | `indexes[]` lists GIST indexes with `method: "gist"` | `unit: cmd/sqlgen/manifest: TestEmitJSON_GistIndexMethod`; `integration: cmd/sqlgen/cli: TestGenerate_ManifestIndexesAndChecksMatchAcrossSources` | postgres |  |
| manifest.meta.indexes.fields | Each index carries `name`, `columns[]`, `unique` and `method` | `unit: cmd/sqlgen/manifest: TestBuild_IndexNamesReadTheSchema` | all |  |
| manifest.meta.indexes.where | A partial index carries its predicate in `where`; other indexes omit it | `unit: cmd/sqlgen/manifest: TestBuild_PartialUniqueIndex`; `unit: cmd/sqlgen/manifest: TestBuild_GINIndexMethod` | all |  |
| manifest.meta.indexes.expression-not-listed | An index with an expression part (`lower(email)`, `a + 1`) is not listed | `unit: cmd/sqlgen/manifest: TestEmitJSON_ExpressionIndexNotListed/postgres`; `unit: cmd/sqlgen/manifest: TestEmitJSON_ExpressionIndexNotListed/mysql`; `unit: cmd/sqlgen/manifest: TestEmitJSON_ExpressionIndexNotListed/sqlite` | all |  |
| manifest.meta.indexes.expression-every-source | An expression index is left out under every `input.source` | `integration: cmd/sqlgen/cli: TestGenerate_ManifestIndexesAndChecksMatchAcrossSources` | all |  |
| manifest.meta.indexes.pk-declared | The primary-key index is listed when the schema declares a `PRIMARY KEY` | `unit: cmd/sqlgen/manifest: TestBuild_PKIndexNeedsASchemaDeclaredKey/schema-declared key lists the PK index`; `unit: cmd/sqlgen/manifest: TestBuild_PKIndexNeedsASchemaDeclaredKey/table-level PK restated in config lists the PK index`; `unit: cmd/sqlgen/manifest: TestBuild_IndexesIncludePKAndUnique` | all |  |
| manifest.meta.indexes.pk-override-none | A key declared only through `primary_key.columns` has no primary-key index listed | `unit: cmd/sqlgen/manifest: TestBuild_PKIndexNeedsASchemaDeclaredKey/app-enforced key with no index lists none` | all |  |
| manifest.meta.indexes.pk-override-unique | A `UNIQUE` that backs a `primary_key.columns` key is listed under its own name | `unit: cmd/sqlgen/manifest: TestBuild_PKIndexNeedsASchemaDeclaredKey/app-enforced key backed by a UNIQUE lists only the UNIQUE`; `unit: cmd/sqlgen/manifest: TestBuild_IndexNamesReadTheSchema/a UNIQUE backing an app-enforced key is listed in place of the PK index` | all |  |
| manifest.meta.indexes.declared-name | A declared constraint or index name is listed as declared | `unit: cmd/sqlgen/manifest: TestBuild_IndexNamesReadTheSchema/postgres names` | all |  |
| manifest.meta.indexes.declared-name.inline-table-level | A declared name is listed as declared whether it is declared inline or table-level | `unit: cmd/sqlgen/manifest: TestEmitJSON_DeclaredIndexNameInlineOrTableLevel/postgres`; `unit: cmd/sqlgen/manifest: TestEmitJSON_DeclaredIndexNameInlineOrTableLevel/mysql`; `unit: cmd/sqlgen/manifest: TestEmitJSON_DeclaredIndexNameInlineOrTableLevel/sqlite`; `integration: cmd/sqlgen/cli: TestGenerate_ManifestIndexesAndChecksMatchAcrossSources` | all |  |
| manifest.meta.indexes.mysql-primary | A MySQL primary key is listed as `PRIMARY` | `unit: cmd/sqlgen/manifest: TestBuild_IndexNamesReadTheSchema/mysql names and a kept duplicate`; `golden: mysql` | mysql |  |
| manifest.meta.indexes.unnamed-postgres | An unnamed PostgreSQL constraint is listed under the name PostgreSQL gives it (`users_pkey`, `users_email_key`) | `golden: postgres` | postgres |  |
| manifest.meta.indexes.unnamed-postgres.index | An unnamed PostgreSQL index is listed under the name PostgreSQL gives it (`orders_placed_at_idx`) | `integration: cmd/sqlgen/cli: TestGenerate_ManifestIndexesAndChecksMatchAcrossSources` | postgres |  |
| manifest.meta.indexes.unnamed-mysql | An unnamed MySQL constraint or index is listed under the name MySQL gives it (`email`) | `golden: mysql` | mysql |  |
| manifest.meta.indexes.mysql-fk-index | The index MySQL builds on its own for a foreign key is listed | `golden: mysql` | mysql |  |
| manifest.meta.indexes.every-source | The index list is the same under every `input.source` | `integration: cmd/sqlgen/cli: TestGenerate_ManifestIndexesAndChecksMatchAcrossSources` | all |  |
| manifest.meta.indexes.sqlite-declared | On SQLite a declared constraint name is listed | `unit: cmd/sqlgen/manifest: TestBuild_IndexNamesReadTheSchema/sqlite labels an unnamed key and keeps a declared name` | sqlite |  |
| manifest.meta.indexes.sqlite-pkey | On SQLite an unnamed `PRIMARY KEY` is labelled `<table>_pkey` | `unit: cmd/sqlgen/manifest: TestBuild_IndexNamesReadTheSchema/sqlite labels an unnamed key and keeps a declared name` | sqlite |  |
| manifest.meta.indexes.sqlite-key | On SQLite an unnamed `UNIQUE` is labelled `<table>_<cols>_key`, `<cols>` joining the column names with `_` | `unit: cmd/sqlgen/manifest: TestBuild_IndexNamesReadTheSchema/sqlite labels an unnamed key and keeps a declared name` | sqlite |  |
| manifest.meta.indexes.duplicate-where-indexed | A `UNIQUE` repeating the primary key or an earlier `UNIQUE` is listed only where the database builds an index for it | `integration: cmd/sqlgen/cli: TestGenerate_ManifestIndexesAndChecksMatchAcrossSources` | postgres, sqlite |  |
| manifest.meta.indexes.mysql-duplicate | On MySQL a duplicate `UNIQUE` is listed under its own name (`email_2`) | `unit: cmd/sqlgen/manifest: TestBuild_IndexNamesReadTheSchema/mysql names and a kept duplicate` | mysql |  |

### `comparator`, `check`, `default_kind`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.meta.comparator.filter-surface | A column's `comparator` is the comparator its `<T>Filter` declares: `filter.fields[].type` minus the leading `*` | `unit: cmd/sqlgen/manifest: TestBuildColumns_ComparatorMirrorsFilterFields` | all |  |
| manifest.meta.comparator.nullable | The nullable variant is published (`comparator.NullableNumber[int32]`) | `unit: cmd/sqlgen/manifest: TestBuildColumns_ComparatorMirrorsFilterFields` | all |  |
| manifest.meta.comparator.type-param | The type parameter is published (`comparator.Slice[string]`, `comparator.Opaque[net.IP]`) | `unit: cmd/sqlgen/manifest: TestBuildColumns_ComparatorMirrorsFilterFields` | postgres |  |
| manifest.meta.comparator.omit-jsonb-array | `comparator` is omitted for a `jsonb[]` column | `unit: cmd/sqlgen/manifest: TestBuildColumns_ComparatorMirrorsFilterFields` | postgres |  |
| manifest.meta.comparator.omit-json-array | `comparator` is omitted for a `json[]` column | `unit: cmd/sqlgen/manifest: TestEmitJSON_ComparatorOmittedForNonFilterableColumn/postgres json array` | postgres |  |
| manifest.meta.comparator.omit-sqlite-json | `comparator` is omitted for a `types.JSON` column on SQLite | `unit: cmd/sqlgen/manifest: TestEmitJSON_ComparatorOmittedForNonFilterableColumn/sqlite types.JSON column` | sqlite |  |
| manifest.meta.check.expression | A column's `check` carries the CHECK-constraint expression text | `unit: cmd/sqlgen/manifest: TestBuild_CheckConstraintFlattened`; `unit: cmd/sqlgen/manifest: TestBuild_CheckConstraintDerivesColumnsFromExpression`; `unit: cmd/sqlgen/manifest: TestBuild_CheckConstraintExpressionLiteralNotMatched` | all |  |
| manifest.meta.check.every-source | `check` is in the file parser's normalized form, so it is the same under every `input.source` | `integration: cmd/sqlgen/cli: TestGenerate_ManifestIndexesAndChecksMatchAcrossSources` | all |  |
| manifest.meta.check.flattened | A multi-column CHECK is flattened onto each participating column | `unit: cmd/sqlgen/manifest: TestBuild_CheckConstraintFlattened`; `unit: cmd/sqlgen/manifest: TestBuild_CheckConstraintDerivesColumnsFromExpression` | all |  |
| manifest.meta.default-kind.literal | `default_kind` is `literal` for a constant value | `unit: cmd/sqlgen/manifest: TestBuild_DefaultKindClassification` | all |  |
| manifest.meta.default-kind.function | `default_kind` is `function` for a function default (`gen_random_uuid()`, `CURRENT_TIMESTAMP`) | `unit: cmd/sqlgen/manifest: TestBuild_DefaultKindClassification` | all |  |
| manifest.meta.default-kind.expression | `default_kind` is `expression` for anything else | `unit: cmd/sqlgen/manifest: TestBuild_DefaultKindClassification` | all |  |

### `access` + `redacted`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.meta.access.role | `access` is the column's resolved role (`public` / `read_only` / `write_only` / `hidden` / `internal`) | `unit: cmd/sqlgen/manifest: TestBuild_ColumnAccessMarkers`; `unit: cmd/sqlgen/manifest: TestValidateAgainstSchema_AccessFields` | all |  |
| manifest.meta.access.redacted-true | `redacted` is `true` for `write_only` and `internal` | `unit: cmd/sqlgen/manifest: TestBuild_ColumnAccessMarkers`; `unit: cmd/sqlgen/manifest: TestEmitJSON_AccessMarkers_BothLayouts` | all |  |
| manifest.meta.access.redacted-false | `redacted` is `false` for the other roles | `unit: cmd/sqlgen/manifest: TestBuild_ColumnAccessMarkers` | all |  |
| manifest.meta.access.default-public | The fields are optional: an omitted `access` reads `public`, an omitted `redacted` reads `false` | `unit: cmd/sqlgen/manifest: TestBuild_ColumnAccessMarkers`; `unit: cmd/sqlgen/manifest: TestValidateAgainstSchema_AccessFields` | all |  |
| manifest.meta.access.sql-bodies-accurate | `sql_bodies` stay the real server SQL for a redacted column; they are not rewritten | `golden: graphql` | all |  |

### `features.events`, `features.cache`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.meta.events.populated | `features.events.{enabled, types[], payload_shape}` is populated when `events.enabled: true` for the entity | `unit: cmd/sqlgen/manifest: TestBuild_EventsTable` | all |  |
| manifest.meta.events.global-off | `features.events` is not populated when events are not enabled | `golden: mysql` | all |  |
| manifest.meta.events.table-off | `features.events` is not populated for a table that opts out of events | `unit: cmd/sqlgen/manifest: TestEmitJSON_EventsNullForOptedOutTable` | all |  |
| manifest.meta.cache.populated | `features.cache.{key_pattern, invalidates_on[]}` is populated when cache is configured | `unit: cmd/sqlgen/manifest: TestBuild_CacheConfiguredTable`; `unit: cmd/sqlgen/manifest: TestBuild_FeaturesCacheFollowsPerTableResolution` | all |  |
| manifest.meta.cache.unconfigured | `features.cache` is not populated when cache is not configured | `golden: mysql`; `unit: cmd/sqlgen/manifest: TestBuild_FeaturesCacheFollowsPerTableResolution` | all |  |
| manifest.meta.cache.key-pattern | `key_pattern` uses `{column}` template syntax | `unit: cmd/sqlgen/manifest: TestBuild_CacheConfiguredTable` | all |  |
| manifest.meta.cache.invalidates-on | `invalidates_on[]` lists the entity's generated methods that §27.7 says invalidate (`Update`, `Upsert`, `Increment`, `SoftDelete`, `HardDelete`, `Restore` and their `*Many` / `*Where` forms), in the entity's method order | `unit: cmd/sqlgen/manifest: TestBuild_CacheInvalidatesOnNamesEntityMutations`; `unit: cmd/sqlgen/manifest: TestBuild_FeaturesCacheFollowsPerTableResolution` | all |  |
| manifest.meta.cache.invalidates-on.no-create | `invalidates_on[]` does not list `Create` / `CreateMany` | `unit: cmd/sqlgen/manifest: TestBuild_CacheInvalidatesOnNamesEntityMutations` | all |  |

### `methods[].sql_bodies`, `generation_config`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.meta.sql-bodies.per-method | Each query and mutation method other than the nested-mutation `…WithRelated` methods carries a per-dialect `sql_bodies` map describing the statement it issues | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_CoverEveryMethodType` | all |  |
| manifest.meta.sql-bodies.per-method.with-related | `sql_bodies` is omitted entirely for the `…WithRelated` methods | `golden: mysql` | all |  |
| manifest.meta.sql-bodies.paginate-connection | `sql_bodies` is omitted entirely for `Paginate` / `Connection` | `unit: cmd/sqlgen/manifest: TestBuild_SQLBodies_CoverEveryMethodType`; `golden: postgres` | all |  |
| manifest.meta.generation-config | Top-level `generation_config` is the resolved, secret-stripped, boolean feature-toggle snapshot | `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_AllOn`; `golden: postgres` | all |  |

### GraphQL schema description wiring

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.meta.graphql-desc.entity | The GraphQL type's `"""description"""` carries `Table.Comment` instead of the template placeholder | `unit: cmd/sqlgen/gen: TestGraphQLSchema_DescriptionWiring/with_comment_renders_comment` | all |  |
| manifest.meta.graphql-desc.column | A GraphQL field's description carries `Column.Comment` | `golden: graphql` | all |  |
| manifest.meta.graphql-desc.agree | The manifest and the `.graphqls` files agree on entity and column descriptions | `golden: graphql` | all |  |

## §30.8 CLI Tooling
<!-- prd: 30.8 sha256: 1aeb7449cd93ddd4 -->

### `sqlgen manifest validate <path>`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.cli.validate.loads | `manifest validate` loads the manifest JSON at `<path>` | `unit: cmd/sqlgen/cli: TestManifestValidate_Valid` | all |  |
| manifest.cli.validate.schema-url | It validates against the JSON Schema the `$schema` URL references | `unit: cmd/sqlgen/cli: TestManifestValidate_UsesSchemaAtManifestURL/a manifest the embedded schema accepts fails the schema at its URL`; `unit: cmd/sqlgen/cli: TestManifestValidate_UsesSchemaAtManifestURL/a manifest the embedded schema rejects passes the schema at its URL` | all |  |
| manifest.cli.validate.summary-schema-version | On success it prints a summary carrying `schema_version` | `unit: cmd/sqlgen/cli: TestManifestValidate_Valid` | all |  |
| manifest.cli.validate.summary-entities | The summary carries the entity count | `unit: cmd/sqlgen/cli: TestManifestValidate_Valid` | all |  |
| manifest.cli.validate.summary-enums | The summary carries the enum count | `unit: cmd/sqlgen/cli: TestManifestValidate_Valid` | all |  |
| manifest.cli.validate.summary-extras | The summary carries the extra count | `unit: cmd/sqlgen/cli: TestManifestValidate_Valid` | all |  |
| manifest.cli.validate.exit-0 | Exit code 0 on success | `unit: cmd/sqlgen/cli: TestManifestValidate_Valid`; `unit: cmd/sqlgen/cli: TestManifestValidate_ReadsNoConfig` | all |  |
| manifest.cli.validate.exit-1 | Exit code 1 on schema failure | `unit: cmd/sqlgen/cli: TestManifestValidate_MissingGenerationConfig` | all |  |
| manifest.cli.validate.exit-2 | Exit code 2 on I/O failure | `unit: cmd/sqlgen/cli: TestManifestValidate_MissingFile` | all |  |
| manifest.cli.validate.on-disk | `manifest validate` operates on the on-disk JSON alone, with no `sqlgen.yml`, database or generated package | `unit: cmd/sqlgen/cli: TestManifestValidate_ReadsNoConfig` | all |  |
| manifest.cli.validate.generated-valid | A generated manifest passes `manifest validate` (the integrity gate holds for the generator's own output) | `unit: cmd/sqlgen/cli: TestManifestValidate_E2EExamples` | all |  |

### `sqlgen manifest diff <old> <new>`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest.cli.diff.loads | `manifest diff` loads two manifests | `unit: cmd/sqlgen/cli: TestManifestDiff_SameFile`; `unit: cmd/sqlgen/cli: TestManifestDiff_PerEntityLayout` | all |  |
| manifest.cli.diff.entities | It walks entities | `unit: cmd/sqlgen/cli: TestManifestDiff_EntityAddedRemoved` | all |  |
| manifest.cli.diff.columns | It walks columns | `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnAdded`; `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnRemoved`; `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnTypeChanged`; `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnConstraintFlipped`; `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnAccessChanged` | all |  |
| manifest.cli.diff.methods | It walks methods | `unit: cmd/sqlgen/cli: TestManifestDiff_MethodSignatureChanged`; `unit: cmd/sqlgen/cli: TestManifestDiff_MethodErrorSetChanged` | all |  |
| manifest.cli.diff.sentinels | It walks sentinels | `unit: cmd/sqlgen/cli: TestManifestDiff_SentinelChanged` | all |  |
| manifest.cli.diff.features | It walks features | `unit: cmd/sqlgen/cli: TestManifestDiff_FeatureToggleChanged` | all |  |
| manifest.cli.diff.added | It prints additions | `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnAdded`; `unit: cmd/sqlgen/cli: TestManifestDiff_EntityAddedRemoved` | all |  |
| manifest.cli.diff.removed | It prints removals | `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnRemoved`; `unit: cmd/sqlgen/cli: TestManifestDiff_EntityAddedRemoved` | all |  |
| manifest.cli.diff.changed | It prints changes | `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnTypeChanged`; `unit: cmd/sqlgen/cli: TestManifestDiff_MethodSignatureChanged` | all |  |
| manifest.cli.diff.human-default | The default output is human-readable text | `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnAdded` | all |  |
| manifest.cli.diff.json | `--json` produces machine-readable output | `unit: cmd/sqlgen/cli: TestManifestDiff_JSONShape` | all |  |
| manifest.cli.diff.exit-0 | Exit code 0 if there is no diff | `unit: cmd/sqlgen/cli: TestManifestDiff_SameFile` | all |  |
| manifest.cli.diff.exit-1 | Exit code 1 if there is any diff | `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnAdded`; `unit: cmd/sqlgen/cli: TestManifestDiff_ChangesFoundIsAResult` | all |  |
| manifest.cli.diff.on-disk | `manifest diff` operates on the on-disk JSON alone, with no `sqlgen.yml`, database or generated package | `unit: cmd/sqlgen/cli: TestManifestDiff_ReadsOnlyTheTwoFiles/changed manifests`; `unit: cmd/sqlgen/cli: TestManifestDiff_ReadsOnlyTheTwoFiles/identical manifests` | all |  |

## Not applicable

| Matrix cell | Reason | Accepted |
|---|---|---|
| manifest-artifacts: package-doc × ownership | §30.3: the `models_gen.go` package-doc pointer "lives inside a fully sqlgen-owned `DO NOT EDIT` file". The provenance-marker and directory-anchor guards do not apply | |
| manifest-artifacts: manifest-embed × ownership | §30.3: `manifest_embed_gen.go` is a `_gen.go` file under the hard `// Code generated by sqlgen. DO NOT EDIT.` claim, so the soft, adoptable ownership guard does not apply | |

## Unclaimed

Tests in this file's packages that back no PRD behavior.

| Unclaimed test | Reason |
|---|---|
| cmd/sqlgen/manifest: TestEmitMarkdown_Examples | markdown rendering detail (per-entity example blocks + fence-aware blank-line normalization); no PRD statement about entity.md content |
| cmd/sqlgen/manifest: TestEmitMarkdown_FKCell | markdown rendering detail (schema-qualified FK cell); no PRD statement about the markdown relationships table |
| cmd/sqlgen/manifest: TestEmitMarkdown_FeaturesLine | markdown rendering detail (_index.md "Features:" line); PRD §30.3 fixes no _index.md content beyond the scan table / cross-reference |
| cmd/sqlgen/manifest: TestEmitMarkdown_GeneratedSQLBlock | markdown rendering detail (per-entity SQL block); PRD specifies sql_bodies for the JSON only |
| cmd/sqlgen/manifest: TestFeatureStructs_noDriftBetweenBuilderAndRuntime | internal invariant: builder/runtime feature-struct parity behind the §30.6 decode; runtime.md has no §30.6 rows yet |
| cmd/sqlgen/manifest: TestRunStage_NoAPIBlockLeavesGraphBreadcrumbs | internal guard (the stale sweep does not guess a default graph dir when no api block names one); no PRD statement or row |

## PRD gaps

Behavior the code has, most of it pinned by tests, that no PRD text states. Each waits for the user's ruling.

| Gap | Behavior | Tests | Section | Ruling |
|---|---|---|---|---|
| gap.manifest.disabled-dir-containment | The disabled path never removes a `markdown_dir` that resolves to `output.dir` itself (`markdown_dir: "."`, which disabled-path validation does not reject) or escapes above it, whatever anchors are present | `unit: cmd/sqlgen/manifest: TestCleanStale_GuardsAgainstOutputDirWipe` | §30.3 |  |
| gap.manifest.breadcrumb-flags-gate-graph | `breadcrumbs.claude_md` and `agents_md` also gate, and with opt-out also remove, `graph/CLAUDE.md` and `graph/AGENTS.md` (`emit_breadcrumbs.go` `writeBreadcrumbPair`; `clean.go` `breadcrumbs`). §30.3 names only GraphQL enablement as the gate | | §30.3 |  |
| gap.manifest.graph-dir-equals-output | When the GraphQL resolver dir resolves to `output.dir` itself, no separate graph breadcrumb is written; the models breadcrumb stands in for it (`emit.go:126-129`) | | §30.3 |  |
| gap.manifest.call-options-fields | `conventions.call_options` is `{type: "CallOptions", fields: [...]}`: the generated `CallOptions` struct's exported fields in declaration order, with `SkipTenancy` when tenancy is configured and `Tenant` when some entity is tenanted | `unit: cmd/sqlgen/manifest: TestCallOptionsFieldsMatchGeneratedStruct` | §30.4.1 | |
| gap.manifest.comparator-convention-content | `conventions.comparator.families` is `Bool, Enum, ID, JSON, JSONB, Number, Slice, String, Time`, and `examples` holds seven canonical snippets keyed `field_filter` … `compound_or` | `unit: cmd/sqlgen/manifest: TestBuild_ConventionsMirrorRuntimeSurface` | §30.4.1 | |
| gap.manifest.soft-delete-convention-content | `conventions.soft_delete` is `{default_excluded_from_finds: true, include_via: "set <SoftDeleteColumn> comparator on filter", hard_delete_method_suffix: "HardDelete", restore_method_suffix: "Restore"}` | `golden: postgres` | §30.4.1 | |
| gap.manifest.features-soft-delete-shape | `features.soft_delete` is `{column, type}`, `type` one of `timestamp` / `bool` / `integer`, populated on a detected soft-delete column | `unit: cmd/sqlgen/manifest: TestBuild_SoftDeleteTable` | §30.4.2 | |
| gap.manifest.features-cache-ttl-hydration | `features.cache.ttl_seconds` is the global TTL in seconds and `hydration` is `lazy` unless hydration is enabled (`eager`) | `unit: cmd/sqlgen/manifest: TestBuild_CacheConfiguredTable` | §30.4.2 | |
| gap.manifest.events-types | `features.events.types` is `<Entity>Created`, `<Entity>Updated`, `<Entity>Deleted`, plus `<Entity>SoftDeleted` and `<Entity>Restored` on a soft-delete table, sorted | `unit: cmd/sqlgen/manifest: TestBuild_EventsTable` | §30.7 | |
| gap.manifest.filter-and-or | `filter.fields` carries `And` and `Or` typed `[]*<Entity>Filter` | `unit: cmd/sqlgen/manifest: TestBuild_FilterAndOrAreSlicesOfPointers` | §30.4.2 | |
| gap.manifest.view-tenancy-null | A view that is not scoped (tenancy off, the view opted out, or the tenant column not projected) reports `features.tenancy: null` | `unit: cmd/sqlgen/manifest: TestBuild_SharedViewHasNoTenancy` | §30.4.2 | |
| gap.manifest.entity-schema-omitted | An entity with no schema omits the `schema` key, where the §30.4.2 example shows `"schema": null` | | §30.4.2 | |
| gap.manifest.nested-no-sql-bodies | The `…WithRelated` methods carry no `sql_bodies` key (they issue no statement of their own); §30.4.2 names only `Paginate` and `Connection` | `golden: mysql`; `golden: graphql` | §30.4.2 | |
| gap.manifest.schema-requires-top-level | `schema/v1.json` requires every top-level field: a document missing `generation_config` fails validation | `unit: cmd/sqlgen/manifest: TestEmitJSON_SchemaRejectsMissingGenerationConfig` | §30.4 | |
| gap.manifest.graphql-desc-placeholder | A table with no comment keeps the template placeholder description (`<Entity> corresponds to the <table> table.`) | `unit: cmd/sqlgen/gen: TestGraphQLSchema_DescriptionWiring/empty_comment_falls_back_to_placeholder` | §30.7 | |
| gap.manifest-validate.summary-format | The success summary is one line, `manifest valid: schema_version=<v> entities=<n> enums=<n> extras=<n>`, suppressed by `--quiet` | `unit: cmd/sqlgen/cli: TestManifestValidate_Valid` | §30.8 | |
