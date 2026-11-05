# CLI Requirements: MCP Server

<!-- seeding: complete -->
<!-- packages: cmd/sqlgen/mcp cmd/sqlgen/mcp/integration -->
<!-- matrix: mcp-surface rows=element cols=listed,output,deterministic,error -->

The MCP server (§31): configuration and `.mcp.json` emission, the tool, resource and prompt surface, the protocol and versioning. The `mcp-surface` matrix has one row per §31.3 tool, resource and prompt; `listed` is the element listed under its PRD name (`tools/list`, `resources/list`, `resources/templates/list` or `prompts/list`), `output` its §31.3 output or body for a valid input, `deterministic` a byte-equal response on repeat against an unchanged manifest, and `error` the coded failure §31.3 and §31.4 give it.

## §31 MCP Server
<!-- prd: 31 sha256: 35b8539fbd0c2d21 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.cfg.serve-consumes-manifest | `sqlgen mcp serve` consumes the generated manifest and exposes it to AI agents over MCP | `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle` | all |  |
| mcp.cfg.eleven-tools | The server exposes 11 tools | `unit: cmd/sqlgen/mcp: TestDispatchListsAllElevenTools`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/element counts match the PRD` | all |  |
| mcp.cfg.four-resources | The server exposes 4 resources (three fixed URIs and one template) | `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/element counts match the PRD` | all |  |
| mcp.cfg.three-prompts | The server exposes 3 prompts | `unit: cmd/sqlgen/mcp: TestDispatchListsThreePrompts`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/element counts match the PRD` | all |  |
| mcp.cfg.stdio-default-transport | The server serves over stdio by default | `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle` | all |  |
| mcp.cfg.http-opt-in-serves | When opted in, the server serves over loopback Streamable HTTP | `integration: cmd/sqlgen/mcp/integration: TestHTTPLifecycle` | all |  |
| mcp.cfg.http-off-by-default | Without the opt-in, no HTTP endpoint is served | `unit: cmd/sqlgen/cli: TestMCPServe_NoHTTPListenerWithoutOptIn/stdio only serves no HTTP`; `unit: cmd/sqlgen/cli: TestMCPServe_NoHTTPListenerWithoutOptIn/control with http opted in` | all |  |
| mcp.cfg.emits-mcp-json | Generation emits `.mcp.json` at the module root | `unit: cmd/sqlgen/cli: TestGenerate_EmitsMCPJSONInRunDirectory/run at the go.mod directory`; `unit: cmd/sqlgen/cli: TestGenerate_EmitsMCPJSONInRunDirectory/run in a subdirectory of a module`; `unit: cmd/sqlgen/cli: TestGenerate_EmitsMCPJSONInRunDirectory/no go.mod at all` | all |  |

## §31.2 Configuration
<!-- prd: 31.2 sha256: e119a58e13ace29d -->

### Server-side CLI flags

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.cfg.flags-present | `sqlgen mcp serve` takes `--manifest`, `--stdio`, `--http`, `--watch`, `--no-watch`, `--log` and `--log-level` | `unit: cmd/sqlgen/cli: TestCompletion_CoversAllFlags/mcp serve` | all |  |
| mcp.cfg.manifest-flag-serves-path | `--manifest <path>` serves the manifest at that path | `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle` | all |  |
| mcp.cfg.manifest-discovery | Manifest discovery walks up to the nearest `manifest_gen.json` | `unit: cmd/sqlgen/mcp: TestDiscoverManifest/walks up to the manifest` | all |  |
| mcp.cfg.manifest-discovery.cwd | Without `--manifest`, the server starts discovery from the working directory | `unit: cmd/sqlgen/mcp: TestRunDiscoversManifestFromWorkingDirectory` | all |  |
| mcp.cfg.manifest-discovery-miss | When discovery finds no manifest, startup fails with `-32001 MANIFEST_NOT_FOUND` | `unit: cmd/sqlgen/cli: TestMCPServe_StartupFailureReportsReservedCode/discovery finds no manifest` | all |  |
| mcp.cfg.stdio-default-true | `--stdio` defaults to `true`: with no transport flag the server speaks JSON-RPC 2.0 over stdin and stdout | `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle` | all |  |
| mcp.cfg.stdout-json-rpc-only | stdout carries only JSON-RPC: a session that logs (a watch reload logs at `info`) stays parseable | `integration: cmd/sqlgen/mcp/integration: TestStdioWatchReloadNotifiesAndServesNewData` | all |  |
| mcp.cfg.logging-to-stderr | All logging goes to stderr by default | `unit: cmd/sqlgen/cli: TestMCPServe_LogDestinations/default goes to stderr` | all |  |
| mcp.cfg.http-endpoint | `--http <port>` additionally serves Streamable HTTP at `http://127.0.0.1:<port>/mcp` | `integration: cmd/sqlgen/mcp/integration: TestHTTPLifecycle` | all |  |
| mcp.cfg.http-default-off | `--http` defaults to off | `unit: cmd/sqlgen/cli: TestMCPServe_FlagDefaults/http off`; `unit: cmd/sqlgen/cli: TestMCPServe_NoHTTPListenerWithoutOptIn/stdio only serves no HTTP` | all |  |
| mcp.cfg.http-loopback-only | The HTTP transport is loopback-bound only: a non-loopback bind is refused when flags are parsed, before serving | `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/wildcard bind refused`; `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/wildcard ipv6 refused`; `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/external ip refused`; `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/public ip refused`; `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/hostname refused`; `unit: cmd/sqlgen/cli: TestMCPServe_RefusesNonLoopbackHTTP` | all |  |
| mcp.cfg.http-stateless-no-session | The HTTP transport is stateless: it keeps no session | `unit: cmd/sqlgen/mcp: TestRunHTTPKeepsNoSession/no session header and no initialize`; `unit: cmd/sqlgen/mcp: TestRunHTTPKeepsNoSession/session id never issued` | all |  |
| mcp.cfg.http-no-push | The HTTP transport pushes no server notifications | `integration: cmd/sqlgen/mcp/integration: TestHTTPWatchReloadNoPushClientRefetches` | all |  |
| mcp.cfg.http-coexists-stdio | `--http` binds while stdio is enabled, in one process | `unit: cmd/sqlgen/mcp: TestRunGracefulShutdown/stdio and http coexist` | all |  |
| mcp.cfg.http-coexists-stdio.stdio-serves | With `--http` running, stdio still serves requests in the same process | `unit: cmd/sqlgen/mcp: TestRunStdioServesWhileHTTPRuns`; `unit: cmd/sqlgen/cli: TestMCPServe_LogDestinations/default goes to stderr` | all |  |
| mcp.cfg.watch-default-on | Watch mode is on by default | `integration: cmd/sqlgen/mcp/integration: TestStdioWatchReloadNotifiesAndServesNewData` | all |  |
| mcp.cfg.watch-reloads | With watch on, the server reloads the manifest when an fsnotify event reports a change | `unit: cmd/sqlgen/mcp: TestWatcherReloadsOnWrite`; `unit: cmd/sqlgen/mcp: TestWatcherSurvivesRenameReplace` | all |  |
| mcp.cfg.no-watch-disables | `--no-watch` turns reloading off: a changed manifest is not reloaded | `unit: cmd/sqlgen/mcp: TestRunNoWatchDoesNotReload` | all |  |
| mcp.cfg.watch-flags-exclusive | `--watch` and `--no-watch` are mutually exclusive | `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/mutually exclusive watch flags` | all |  |
| mcp.cfg.log-flag | `--log <path>` redirects logging to that file | `unit: cmd/sqlgen/cli: TestMCPServe_LogDestinations/log flag redirects to the file` | all |  |
| mcp.cfg.log-level-values | `--log-level` accepts `debug`, `info`, `warn` and `error` | `unit: cmd/sqlgen/mcp: TestNewLogger/debug`; `unit: cmd/sqlgen/mcp: TestNewLogger/info`; `unit: cmd/sqlgen/mcp: TestNewLogger/warn`; `unit: cmd/sqlgen/mcp: TestNewLogger/error` | all |  |
| mcp.cfg.log-level-others-refused | Any other `--log-level` value is refused | `unit: cmd/sqlgen/mcp: TestNewLogger/invalid level`; `unit: cmd/sqlgen/cli: TestMCPServe_RejectsInvalidLogLevel` | all |  |
| mcp.cfg.log-level-default | `--log-level` defaults to `info` | `unit: cmd/sqlgen/cli: TestMCPServe_FlagDefaults/log level info`; `unit: cmd/sqlgen/cli: TestMCPServe_LogDestinations/default goes to stderr` | all |  |

### The `generation.manifest.mcp` block

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.cfg.block-nested | `.mcp.json` emission is configured by the `mcp` block nested under `generation.manifest` | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestMCPDefaults/partial mcp block keeps explicit values` | all |  |
| mcp.cfg.block-global-only | The `mcp` block is global-only: there is no per-table `mcp` setting | `unit: cmd/sqlgen/cli: TestGenerate_MCPBlockIsGlobalOnly/table-level mcp`; `unit: cmd/sqlgen/cli: TestGenerate_MCPBlockIsGlobalOnly/mcp under the table manifest override`; `unit: cmd/sqlgen/cli: TestGenerate_MCPBlockIsGlobalOnly/control without mcp` | all |  |
| mcp.cfg.default-emit | `emit_project_config` defaults to `true` when `manifest.enabled` | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestMCPDefaults/omitted mcp block gets full defaults` | all |  |
| mcp.cfg.default-project-configs | `project_configs` defaults to `[.mcp.json]` | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestMCPDefaults/omitted mcp block gets full defaults` | all |  |
| mcp.cfg.default-server-key | `server_key` defaults to `sqlgen`, the key under `mcpServers` | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestMCPDefaults/omitted mcp block gets full defaults`; `unit: cmd/sqlgen/mcp: TestMergeCreatesNewFile` | all |  |
| mcp.cfg.command-template-optional | `command_template` is optional and empty by default | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestMCPDefaults/omitted mcp block gets full defaults` | all |  |

### Validation

The rule rows proper live in `config.md` (`config.manifest-mcp-emit-disabled`, `config.manifest-mcp-project-configs-path`). These rows restate §31.2's own sentences and cite the same tests.

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.cfg.validate-emit-needs-manifest | `emit_project_config: true` with `manifest.enabled: false` is an error | `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_MCPEmitRequiresEnabled`; `unit: cmd/sqlgen/config: TestValidatePreParse_ReportsEachRuleWithTheOthers/manifest mcp emit with the manifest off` | all |  |
| mcp.cfg.validate-target-relative | Every `project_configs` entry must be relative, not absolute | `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_MCPProjectConfigPlacement/absolute path rejected` | all |  |
| mcp.cfg.validate-target-no-dotdot | No `project_configs` entry may contain a `..` segment | `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_MCPProjectConfigPlacement/leading dotdot rejected`; `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_MCPProjectConfigPlacement/interior dotdot rejected` | all |  |
| mcp.cfg.validate-dotdot-when-disabled | The no-`..` rule holds with the manifest disabled | `unit: cmd/sqlgen/config: TestValidatePreParse_Manifest_MCPPlacementEnforcedWhenDisabled` | all |  |
| mcp.cfg.validate-absolute-when-disabled | The relative-path rule holds with the manifest disabled | `unit: cmd/sqlgen/cli: TestGenerate_AbsoluteMCPTargetRefusedWithManifestDisabled` | all |  |

### Emission

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.cfg.emit-last-step | Emission runs as the last manifest-stage step of `sqlgen generate`, after breadcrumbs | `unit: cmd/sqlgen/cli: TestGenerate_MCPEmissionRunsAfterBreadcrumbs/breadcrumb failure stops before emission`; `unit: cmd/sqlgen/cli: TestGenerate_MCPEmissionRunsAfterBreadcrumbs/emission failure leaves manifest and breadcrumbs written` | all |  |
| mcp.cfg.emit-per-target | Emission runs once per `project_configs` target | `unit: cmd/sqlgen/cli: TestEmitMCPProjectConfigsMultiTarget` | all |  |
| mcp.cfg.emit-failure-reported | A per-target failure is reported, naming the target | `unit: cmd/sqlgen/cli: TestEmitMCPProjectConfigsFailureDoesNotBlockOtherTargets`; `unit: cmd/sqlgen/mcp: TestMergeReadOnlyDirFails` | all |  |
| mcp.cfg.emit-failure-continues | A per-target failure does not abort the remaining targets | `unit: cmd/sqlgen/cli: TestEmitMCPProjectConfigsFailureDoesNotBlockOtherTargets` | all |  |
| mcp.cfg.emit-failure-exit | `sqlgen generate` exits non-zero when any target failed | `unit: cmd/sqlgen/cli: TestDiff_ManifestStageFailureOutsidePreview/invalid project MCP config refused` | all |  |

### Merge contract

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.cfg.merge.sentinel-managed | The emitted entry carries `x-sqlgen-managed: true` | `unit: cmd/sqlgen/mcp: TestMergeCreatesNewFile` | all |  |
| mcp.cfg.merge.sentinel-manifest | The emitted entry carries `x-sqlgen-manifest: <path>` | `unit: cmd/sqlgen/mcp: TestMergeCreatesNewFile` | all |  |
| mcp.cfg.merge.sentinel-version | The emitted entry carries `x-sqlgen-generator-version` | `unit: cmd/sqlgen/mcp: TestMergeCreatesNewFile` | all |  |
| mcp.cfg.merge.match-key | A managed entry whose `x-sqlgen-manifest` names the manifest being emitted is the matching entry. A managed entry naming another manifest does not match | `unit: cmd/sqlgen/mcp: TestMergeUpdatesManagedEntryInPlace`; `unit: cmd/sqlgen/mcp: TestMergeMonorepoSiblingsCoexist` | all |  |
| mcp.cfg.merge.match-absolute | The match compares `x-sqlgen-manifest` values as absolute paths | `unit: cmd/sqlgen/mcp: TestMergeMatchComparesAbsolutePaths/stored absolute, emitted relative`; `unit: cmd/sqlgen/mcp: TestMergeMatchComparesAbsolutePaths/stored with redundant segments` | all |  |
| mcp.cfg.merge.update-in-place | A matching entry is updated in place under its key, without a second entry being added | `unit: cmd/sqlgen/mcp: TestMergeUpdatesManagedEntryInPlace` | all |  |
| mcp.cfg.merge.key-preserved | Updating a matching entry keeps its key | `unit: cmd/sqlgen/mcp: TestMergeUpdatesManagedEntryInPlace` | all |  |
| mcp.cfg.merge.no-match-new-entry | With no matching entry, a new entry is added under `server_key` | `unit: cmd/sqlgen/mcp: TestMergeCreatesNewFile`; `unit: cmd/sqlgen/mcp: TestMergeUpsertAppendsAlongsideOtherTools` | all |  |
| mcp.cfg.merge.collision-warns | When `server_key` is held by an entry without the managed marker, sqlgen warns | `unit: cmd/sqlgen/mcp: TestMergeUnmanagedCollisionPicksFreeKeyAndWarns` | all |  |
| mcp.cfg.merge.collision-next-key | On that collision, the entry is registered under the next free key, `sqlgen-2` | `unit: cmd/sqlgen/mcp: TestMergeUnmanagedCollisionPicksFreeKeyAndWarns` | all |  |
| mcp.cfg.merge.collision-key-sequence | When `sqlgen-2` is also taken, the next free key is `sqlgen-3`, and so on | `unit: cmd/sqlgen/mcp: TestMergeCollisionKeySequence/sqlgen and sqlgen-2 taken`; `unit: cmd/sqlgen/mcp: TestMergeCollisionKeySequence/sqlgen through sqlgen-3 taken`; `unit: cmd/sqlgen/mcp: TestMergeCollisionKeySequence/gap at sqlgen-2` | all |  |
| mcp.cfg.merge.stale-swept | A managed entry whose `x-sqlgen-manifest` file no longer exists on disk is removed | `unit: cmd/sqlgen/mcp: TestMergeStaleEntryCleanupOnOutputDirMove` | all |  |
| mcp.cfg.merge.sibling-survives | A managed entry whose manifest is on disk (a monorepo sibling) survives | `unit: cmd/sqlgen/mcp: TestMergeMonorepoSiblingsCoexist` | all |  |
| mcp.cfg.merge.absent-sibling-swept | A sibling whose manifest is not on disk (gitignored or not yet generated) is treated as stale and swept | `unit: cmd/sqlgen/mcp: TestMergeAbsentSiblingUnderOwnKeySwept/upsert run`; `unit: cmd/sqlgen/mcp: TestMergeAbsentSiblingUnderOwnKeySwept/removal run` | all |  |
| mcp.cfg.merge.other-tools-untouched | Other tools' entries are never touched | `unit: cmd/sqlgen/mcp: TestMergeUpsertAppendsAlongsideOtherTools`; `unit: cmd/sqlgen/mcp: TestMergeRemovePreservesOtherEntries` | all |  |
| mcp.cfg.merge.consumer-entries-untouched | Consumer-authored entries are never touched | `unit: cmd/sqlgen/mcp: TestMergeUnmanagedCollisionPicksFreeKeyAndWarns` | all |  |
| mcp.cfg.merge.numeric-forms | Untouched entries keep the exact form of their numeric values | `unit: cmd/sqlgen/mcp: TestMergePreservesSiblingNumericForm` | all |  |
| mcp.cfg.merge.remove-no-match-untouched | A removal run that matches nothing leaves the file byte-untouched | `unit: cmd/sqlgen/mcp: TestMergeRemoveNoMatchLeavesFileUntouched` | all |  |
| mcp.cfg.merge.canonical-indent | Output uses 2-space indentation | `unit: cmd/sqlgen/mcp: TestMergeCanonicalOutputExactBytes` | all |  |
| mcp.cfg.merge.canonical-sorted | Output keys are sorted alphabetically at every level | `unit: cmd/sqlgen/mcp: TestMergeCanonicalOutputExactBytes` | all |  |
| mcp.cfg.merge.first-run-normalizes | The first run normalizes an existing file's formatting to the canonical form | `unit: cmd/sqlgen/mcp: TestMergeCanonicalOutputExactBytes` | all |  |
| mcp.cfg.merge.byte-stable | Subsequent runs are byte-stable | `unit: cmd/sqlgen/mcp: TestMergeCanonicalFormatIsIdempotent` | all |  |
| mcp.cfg.merge.atomic-write | The file is written atomically (tmp, then fsync, then rename) | `unit: cmd/sqlgen/mcp: TestMergeWritesByRenameNotInPlace` | all |  |
| mcp.cfg.merge.invalid-json-fails | Invalid JSON in an existing target fails the merge | `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONFailsWithoutOverwrite`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/syntax error mid-document` | all |  |
| mcp.cfg.merge.invalid-json-line-col | The invalid-JSON failure names the line and column | `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/truncated object`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/truncated mid-line`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/trailing content after the document`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/trailing content on a later line`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/syntax error mid-document`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/empty file`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/whitespace-only file`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn` | all |  |
| mcp.cfg.merge.invalid-json-truncated | A truncated document fails at the position just past its last byte | `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/truncated object`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/truncated mid-line`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONFailsWithoutOverwrite` | all |  |
| mcp.cfg.merge.invalid-json-empty | An empty target fails as invalid JSON at `1:1` | `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/empty file` | all |  |
| mcp.cfg.merge.invalid-json-trailing | Content after the JSON document fails at the position of its first byte | `unit: cmd/sqlgen/mcp: TestMergeTrailingContentAfterJSONFailsWithoutOverwrite`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/trailing content after the document`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/trailing content on a later line` | all |  |
| mcp.cfg.merge.invalid-json-not-overwritten | A target holding invalid JSON is never overwritten | `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONFailsWithoutOverwrite`; `unit: cmd/sqlgen/mcp: TestMergeTrailingContentAfterJSONFailsWithoutOverwrite`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/truncated object`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/empty file`; `unit: cmd/sqlgen/mcp: TestMergeInvalidJSONErrorNamesLineAndColumn/syntax error mid-document` | all |  |
| mcp.cfg.merge.opt-out-removes | `emit_project_config: false` removes sqlgen's managed entries on the next generation | `unit: cmd/sqlgen/cli: TestEmitMCPProjectConfigsOptOutRemoves` | all |  |
| mcp.cfg.merge.manifest-off-removes | Disabling the manifest removes sqlgen's managed entries on the next generation | `unit: cmd/sqlgen/cli: TestEmitMCPProjectConfigsManifestDisabledRemovesWithDefaults` | all |  |
| mcp.cfg.merge.remove-deletes-empty | The file is deleted when removal empties `mcpServers` and no other top-level field remains | `unit: cmd/sqlgen/mcp: TestMergeRemoveDeletesEntryAndEmptyFile`; `unit: cmd/sqlgen/cli: TestEmitMCPProjectConfigsOptOutRemoves` | all |  |
| mcp.cfg.merge.remove-keeps-nonempty-servers | The file is kept when other `mcpServers` entries remain | `unit: cmd/sqlgen/mcp: TestMergeRemovePreservesOtherEntries` | all |  |
| mcp.cfg.merge.remove-keeps-other-fields | The file is kept when `mcpServers` empties but another top-level field remains | `unit: cmd/sqlgen/mcp: TestMergeRemoveKeepsFileWithOtherTopLevelField` | all |  |
| mcp.cfg.merge.template-rendered | `command_template` is rendered with `text/template`, substituting `{{ manifest_path }}` | `unit: cmd/sqlgen/mcp: TestMergeCommandTemplateRendering/direnv wrapper with substitution` | all |  |
| mcp.cfg.merge.template-tokenized | The rendered template is split into tokens on whitespace, with quotes grouping a token | `unit: cmd/sqlgen/mcp: TestMergeCommandTemplateRendering/quoted token with spaces stays one arg`; `unit: cmd/sqlgen/mcp: TestTokenizeCommand/plain tokens`; `unit: cmd/sqlgen/mcp: TestTokenizeCommand/double quotes group`; `unit: cmd/sqlgen/mcp: TestTokenizeCommand/single quotes group` | all |  |
| mcp.cfg.merge.template-no-shell | No shell is invoked: the rendered text undergoes no expansion | `unit: cmd/sqlgen/mcp: TestMergeCommandTemplateNoShellExpansion` | all |  |
| mcp.cfg.merge.template-command-args | The first token becomes the entry's `command` and the rest its `args` | `unit: cmd/sqlgen/mcp: TestMergeCommandTemplateRendering/direnv wrapper with substitution` | all |  |

### Module-root anchor

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.cfg.anchor-targets | `project_configs` targets resolve against the directory `sqlgen generate` runs in | `unit: cmd/sqlgen/cli: TestEmitMCPProjectConfigsMultiTarget` | all |  |
| mcp.cfg.anchor-manifest-path | The emitted manifest path resolves against the directory `sqlgen generate` runs in | `unit: cmd/sqlgen/cli: TestGenerate_EmitsMCPJSONInRunDirectory/run in a subdirectory of a module`; `unit: cmd/sqlgen/cli: TestGenerate_EmitsMCPJSONInRunDirectory/run at the go.mod directory` | all |  |
| mcp.cfg.anchor-no-go-mod | sqlgen performs no `go.mod` discovery to find the module root | `unit: cmd/sqlgen/cli: TestGenerate_EmitsMCPJSONInRunDirectory/run in a subdirectory of a module`; `unit: cmd/sqlgen/cli: TestGenerate_EmitsMCPJSONInRunDirectory/no go.mod at all` | all |  |

## §31.3 Tool, Resource & Prompt Surface
<!-- prd: 31.3 sha256: 517a2a4c6441603b -->

### Naming and tool guarantees

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.surface.tool-prefix | Every tool name is prefixed `sqlgen_` | `unit: cmd/sqlgen/mcp: TestDispatchListsAllElevenTools`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle` | all |  |
| mcp.surface.resource-scheme | Every resource URI uses the `sqlgen://` scheme | `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/every resource URI uses the sqlgen scheme` | all |  |
| mcp.surface.prompt-prefix | Every prompt name is kebab-case with a `sqlgen-` prefix | `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/every prompt name is kebab-case with the sqlgen- prefix` | all |  |
| mcp.surface.tools-read-only | Every tool is read-only | `unit: cmd/sqlgen/mcp: TestDispatchToolsLeaveManifestAndFilesUnchanged/no tool writes a file` | all |  |
| mcp.surface.tools-side-effect-free | Every tool is side-effect-free: a call changes neither the served manifest nor any file | `unit: cmd/sqlgen/mcp: TestDispatchToolsLeaveManifestAndFilesUnchanged/no tool writes a file`; `unit: cmd/sqlgen/mcp: TestDispatchToolsLeaveManifestAndFilesUnchanged/served manifest unchanged` | all |  |
| mcp.surface.tools-idempotent | Every tool is idempotent: a repeat call returns the same response | `unit: cmd/sqlgen/mcp: TestDispatchToolsIdempotentOnOneServer` | all |  |

### Tools

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.surface.list-entities.listed | `sqlgen_list_entities` is listed by `tools/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsAllElevenTools`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/tool sqlgen_list_entities` | all | mcp-surface: list-entities × listed |
| mcp.surface.list-entities.output | `sqlgen_list_entities` returns `[{name, table, pk_kind, features_summary}]` | `unit: cmd/sqlgen/mcp: TestListEntities/all entities sorted by name` | all | mcp-surface: list-entities × output |
| mcp.surface.list-entities.kind | `kind: "table"` or `"view"` limits the list to that kind | `unit: cmd/sqlgen/mcp: TestListEntities/kind=view`; `unit: cmd/sqlgen/mcp: TestListEntities/kind=table excludes the view` | all |  |
| mcp.surface.list-entities.deterministic | `sqlgen_list_entities` is byte-equal on repeat against an unchanged manifest | `unit: cmd/sqlgen/mcp: TestDispatch_Deterministic/list_entities`; `unit: cmd/sqlgen/mcp: TestDispatchToolsIdempotentOnOneServer/list_entities` | all | mcp-surface: list-entities × deterministic |
| mcp.surface.get-entity.listed | `sqlgen_get_entity` is listed by `tools/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsAllElevenTools`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/tool sqlgen_get_entity` | all | mcp-surface: get-entity × listed |
| mcp.surface.get-entity.output | `sqlgen_get_entity({name})` returns the entity JSON with its columns and example | `unit: cmd/sqlgen/mcp: TestGetEntity/found returns full record`; `unit: cmd/sqlgen/mcp: TestDispatchCallToolReturnsStructuredContent` | all | mcp-surface: get-entity × output |
| mcp.surface.get-entity.output.methods-relationships-filter-sort | The `sqlgen_get_entity({name})` entity JSON includes the entity's methods, relationships, filter and sort | `unit: cmd/sqlgen/mcp: TestDispatchGetEntityIncludesMethodsRelationshipsFilterSort` | all |  |
| mcp.surface.get-entity.deterministic | `sqlgen_get_entity` is byte-equal on repeat against an unchanged manifest | `unit: cmd/sqlgen/mcp: TestDispatch_Deterministic/get_entity`; `unit: cmd/sqlgen/mcp: TestDispatch_Deterministic/get_entity_compact`; `unit: cmd/sqlgen/mcp: TestDispatchToolsIdempotentOnOneServer/get_entity`; `unit: cmd/sqlgen/mcp: TestDispatchToolsIdempotentOnOneServer/get_entity_compact` | all | mcp-surface: get-entity × deterministic |
| mcp.surface.get-entity.error | `sqlgen_get_entity` on an unknown name fails with `-32003 ENTITY_NOT_FOUND` carrying suggestions | `unit: cmd/sqlgen/mcp: TestGetEntity/unknown name yields -32003 with suggestions`; `unit: cmd/sqlgen/mcp: TestDispatchNotFoundIsToolError` | all | mcp-surface: get-entity × error |
| mcp.surface.find-method.listed | `sqlgen_find_method` is listed by `tools/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsAllElevenTools`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/tool sqlgen_find_method` | all | mcp-surface: find-method × listed |
| mcp.surface.find-method.output | `sqlgen_find_method({query})` returns `[{entity, method, signature}]` | `unit: cmd/sqlgen/mcp: TestFindMethod/exact case-insensitive match` | all | mcp-surface: find-method × output |
| mcp.surface.find-method.deterministic | `sqlgen_find_method` is byte-equal on repeat against an unchanged manifest | `unit: cmd/sqlgen/mcp: TestDispatch_Deterministic/find_method`; `unit: cmd/sqlgen/mcp: TestDispatchToolsIdempotentOnOneServer/find_method` | all | mcp-surface: find-method × deterministic |
| mcp.surface.find-referencing.listed | `sqlgen_find_referencing` is listed by `tools/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsAllElevenTools`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/tool sqlgen_find_referencing` | all | mcp-surface: find-referencing × listed |
| mcp.surface.find-referencing.output | `sqlgen_find_referencing({table})` returns `[{entity, relationship_name, kind, fk_columns}]` | `unit: cmd/sqlgen/mcp: TestFindReferencing/composite key in declared order` | all | mcp-surface: find-referencing × output |
| mcp.surface.find-referencing.deterministic | `sqlgen_find_referencing` is byte-equal on repeat against an unchanged manifest | `unit: cmd/sqlgen/mcp: TestDispatch_Deterministic/find_referencing`; `unit: cmd/sqlgen/mcp: TestDispatchRepeatIsByteIdentical/find_referencing` | all | mcp-surface: find-referencing × deterministic |
| mcp.surface.describe-relationship.listed | `sqlgen_describe_relationship` is listed by `tools/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsAllElevenTools`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/tool sqlgen_describe_relationship` | all | mcp-surface: describe-relationship × listed |
| mcp.surface.describe-relationship.output | `sqlgen_describe_relationship({entity, relationship})` returns the join shape: the edge's kind, its target entity, and the junction for an m2m edge | `unit: cmd/sqlgen/mcp: TestDescribeRelationship/m2m relationship with junction and target summary` | all | mcp-surface: describe-relationship × output |
| mcp.surface.describe-relationship.fk-columns | For an FK edge, the join shape gives the FK columns | `unit: cmd/sqlgen/mcp: TestDescribeRelationship/compact drops the target summary but keeps join shape` | all |  |
| mcp.surface.describe-relationship.references | For an FK edge, the join shape gives the columns the FK columns reference | `unit: cmd/sqlgen/mcp: TestDescribeRelationship_JoinShapeNamesReferencedColumns` | all |  |
| mcp.surface.describe-relationship.filter | The join shape gives the edge's filter clause | `unit: cmd/sqlgen/mcp: TestDispatchDescribeRelationshipCarriesFilter` | all |  |
| mcp.surface.describe-relationship.deterministic | `sqlgen_describe_relationship` is byte-equal on repeat against an unchanged manifest | `unit: cmd/sqlgen/mcp: TestDispatch_Deterministic/describe_relationship`; `unit: cmd/sqlgen/mcp: TestDispatchToolsIdempotentOnOneServer/describe_relationship` | all | mcp-surface: describe-relationship × deterministic |
| mcp.surface.describe-relationship.error-entity | `sqlgen_describe_relationship` on an unknown entity fails with `-32003` carrying suggestions | `unit: cmd/sqlgen/mcp: TestDescribeRelationship/unknown entity yields -32003` | all | mcp-surface: describe-relationship × error |
| mcp.surface.describe-relationship.error-relationship | `sqlgen_describe_relationship` on a relationship the entity lacks fails with `-32004 RELATIONSHIP_NOT_FOUND` carrying suggestions | `unit: cmd/sqlgen/mcp: TestDescribeRelationship/unknown relationship yields -32004 with suggestions` | all | mcp-surface: describe-relationship × error |
| mcp.surface.get-conventions.listed | `sqlgen_get_conventions` is listed by `tools/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsAllElevenTools`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/tool sqlgen_get_conventions` | all | mcp-surface: get-conventions × listed |
| mcp.surface.get-conventions.output | `sqlgen_get_conventions({})` returns the manifest's conventions block | `unit: cmd/sqlgen/mcp: TestGetConventions` | all | mcp-surface: get-conventions × output |
| mcp.surface.get-conventions.deterministic | `sqlgen_get_conventions` is byte-equal on repeat against an unchanged manifest | `unit: cmd/sqlgen/mcp: TestDispatch_Deterministic/get_conventions`; `unit: cmd/sqlgen/mcp: TestDispatchToolsIdempotentOnOneServer/get_conventions` | all | mcp-surface: get-conventions × deterministic |
| mcp.surface.get-example.listed | `sqlgen_get_example` is listed by `tools/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsAllElevenTools`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/tool sqlgen_get_example` | all | mcp-surface: get-example × listed |
| mcp.surface.get-example.output | `sqlgen_get_example({entity})` returns the entity's canonical Go snippets | `unit: cmd/sqlgen/mcp: TestGetExample/returns read and write snippets` | all | mcp-surface: get-example × output |
| mcp.surface.get-example.op-read | `op: "read"` limits the snippets to read snippets | `unit: cmd/sqlgen/mcp: TestGetExample/op=read drops write` | all |  |
| mcp.surface.get-example.op-write | `op: "write"` limits the snippets to write snippets | `unit: cmd/sqlgen/mcp: TestDispatchGetExampleOps/op=write keeps only write snippets` | all |  |
| mcp.surface.get-example.deterministic | `sqlgen_get_example` is byte-equal on repeat against an unchanged manifest | `unit: cmd/sqlgen/mcp: TestDispatch_Deterministic/get_example`; `unit: cmd/sqlgen/mcp: TestDispatchToolsIdempotentOnOneServer/get_example` | all | mcp-surface: get-example × deterministic |
| mcp.surface.get-example.error | `sqlgen_get_example` on an unknown entity fails with `-32003` carrying suggestions | `unit: cmd/sqlgen/mcp: TestGetExample/unknown entity yields -32003` | all | mcp-surface: get-example × error |
| mcp.surface.find-join-path.listed | `sqlgen_find_join_path` is listed by `tools/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsAllElevenTools`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/tool sqlgen_find_join_path` | all | mcp-surface: find-join-path × listed |
| mcp.surface.find-join-path.output | `sqlgen_find_join_path({from, to})` returns `[{hops, path: [{entity, relationship}]}]` | `unit: cmd/sqlgen/mcp: TestFindJoinPath/two hops through Post`; `unit: cmd/sqlgen/mcp: TestFindJoinPath/one hop` | all | mcp-surface: find-join-path × output |
| mcp.surface.find-join-path.deterministic | `sqlgen_find_join_path` is byte-equal on repeat against an unchanged manifest | `unit: cmd/sqlgen/mcp: TestDispatchRepeatIsByteIdentical/find_join_path` | all | mcp-surface: find-join-path × deterministic |
| mcp.surface.find-join-path.error | `sqlgen_find_join_path` on an unknown `from` or `to` entity fails with `-32003` carrying suggestions | `unit: cmd/sqlgen/mcp: TestFindJoinPath/unknown from/to yields -32003` | all | mcp-surface: find-join-path × error |
| mcp.surface.show-sql.listed | `sqlgen_show_sql` is listed by `tools/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsAllElevenTools`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/tool sqlgen_show_sql` | all | mcp-surface: show-sql × listed |
| mcp.surface.show-sql.output | `sqlgen_show_sql({entity, method})` returns `{sql, params: [{name, type}], dialect}` | `unit: cmd/sqlgen/mcp: TestShowSQL/resolves the active dialect` | all | mcp-surface: show-sql × output |
| mcp.surface.show-sql.deterministic | `sqlgen_show_sql` is byte-equal on repeat against an unchanged manifest | `unit: cmd/sqlgen/mcp: TestDispatch_Deterministic/show_sql`; `unit: cmd/sqlgen/mcp: TestDispatchToolsIdempotentOnOneServer/show_sql` | all | mcp-surface: show-sql × deterministic |
| mcp.surface.show-sql.error-entity | `sqlgen_show_sql` on an unknown entity fails with `-32003` carrying suggestions | `unit: cmd/sqlgen/mcp: TestShowSQL/unknown entity yields -32003` | all | mcp-surface: show-sql × error |
| mcp.surface.show-sql.error-method | `sqlgen_show_sql` on a method the entity does not define fails with `-32006 METHOD_NOT_FOUND` carrying suggestions | `unit: cmd/sqlgen/mcp: TestShowSQL/unknown method yields -32006 with suggestions` | all | mcp-surface: show-sql × error |
| mcp.surface.show-sql.error-no-bodies | `sqlgen_show_sql` fails with `-32005 METHOD_SQL_UNAVAILABLE` when the manifest predates `methods[].sql_bodies` | `unit: cmd/sqlgen/mcp: TestShowSQL/method without sql_bodies yields -32005` | all | mcp-surface: show-sql × error |
| mcp.surface.show-sql.error-no-dialect-body | `sqlgen_show_sql` fails with `-32005` when the method carries no body for the resolved dialect | `unit: cmd/sqlgen/mcp: TestShowSQL/dialect override without a matching body yields -32005` | all | mcp-surface: show-sql × error |
| mcp.surface.health.listed | `sqlgen_health` is listed by `tools/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsAllElevenTools`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/tool sqlgen_health` | all | mcp-surface: health × listed |
| mcp.surface.health.output | `sqlgen_health({})` returns `{manifest_path, schema_version, generated_at, loaded_at, watch_enabled, sqlgen_version, ok}` | `unit: cmd/sqlgen/mcp: TestHealth/fresh load` | all | mcp-surface: health × output |
| mcp.surface.health.last-reload-at | `last_reload_at` is optional: absent before any reload, and present after one | `unit: cmd/sqlgen/mcp: TestHealth/watch off is reported`; `unit: cmd/sqlgen/mcp: TestHealth/failed reload flips ok and sets last_reload_at`; `integration: cmd/sqlgen/mcp/integration: TestStdioWatchReloadNotifiesAndServesNewData` | all |  |
| mcp.surface.health.watch-enabled | `watch_enabled` reports whether watch mode is on | `unit: cmd/sqlgen/mcp: TestHealth/fresh load`; `unit: cmd/sqlgen/mcp: TestHealth/watch off is reported` | all |  |
| mcp.surface.health.deterministic | `sqlgen_health` is byte-equal on repeat against an unchanged manifest | `unit: cmd/sqlgen/mcp: TestDispatch_Deterministic/health`; `unit: cmd/sqlgen/mcp: TestDispatchToolsIdempotentOnOneServer/health` | all | mcp-surface: health × deterministic |
| mcp.surface.validate-manifest.listed | `sqlgen_validate_manifest` is listed by `tools/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsAllElevenTools`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/tool sqlgen_validate_manifest` | all | mcp-surface: validate-manifest × listed |
| mcp.surface.validate-manifest.output | `sqlgen_validate_manifest({})` returns `{valid, schema_version, errors: [{field, msg}]}` | `unit: cmd/sqlgen/mcp: TestValidateManifest/in-memory manifest is valid` | all | mcp-surface: validate-manifest × output |
| mcp.surface.validate-manifest.deterministic | `sqlgen_validate_manifest` is byte-equal on repeat against an unchanged manifest | `unit: cmd/sqlgen/mcp: TestDispatch_Deterministic/validate_manifest`; `unit: cmd/sqlgen/mcp: TestDispatchToolsIdempotentOnOneServer/validate_manifest` | all | mcp-surface: validate-manifest × deterministic |
| mcp.surface.validate-manifest.error | A manifest failing validation yields `valid: false` and a non-empty `errors` list | `unit: cmd/sqlgen/mcp: TestValidateManifest/invalid manifest reports flattened errors` | all |  |

### Surface notes

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.surface.compact.get-entity-drops | `compact: true` on `sqlgen_get_entity` drops example snippets, long-form descriptions, `check`, `default_kind` and `indexes[]` | `unit: cmd/sqlgen/mcp: TestGetEntity/compact drops verbose fields` | all |  |
| mcp.surface.compact.get-entity-retains | `compact: true` on `sqlgen_get_entity` keeps the PK kind | `unit: cmd/sqlgen/mcp: TestGetEntity/compact drops verbose fields` | all |  |
| mcp.surface.compact.get-entity-retains.identity | `compact: true` on `sqlgen_get_entity` keeps the rest of the required identity: names, types, FK columns, method signatures, join shape and filter sub-categories | `unit: cmd/sqlgen/mcp: TestDispatchCompactGetEntityRetainsIdentity` | all |  |
| mcp.surface.compact.describe-relationship-retains | `compact: true` on `sqlgen_describe_relationship` keeps the join shape | `unit: cmd/sqlgen/mcp: TestDescribeRelationship/compact drops the target summary but keeps join shape` | all |  |
| mcp.surface.compact.get-example-no-op | `compact` on `sqlgen_get_example` is accepted and is a no-op: the snippets are returned unchanged | `unit: cmd/sqlgen/mcp: TestDispatchGetExampleOps/compact is a no-op with no op`; `unit: cmd/sqlgen/mcp: TestDispatchGetExampleOps/compact is a no-op with op=read`; `unit: cmd/sqlgen/mcp: TestDispatchGetExampleOps/compact is a no-op with op=write` | all |  |
| mcp.surface.find-referencing.every-edge | `sqlgen_find_referencing` returns every FK edge pointing at the given table | `unit: cmd/sqlgen/mcp: TestFindReferencing/users referenced by posts FK and both m2m junction sides`; `unit: cmd/sqlgen/mcp: TestFindReferencing/posts referenced by comments FK from both relationship sides` | all |  |
| mcp.surface.find-referencing.fk-pair-both-sides | Both sides of an FK pair emit a result | `unit: cmd/sqlgen/mcp: TestFindReferencing/posts referenced by comments FK from both relationship sides` | all |  |
| mcp.surface.find-referencing.m2m-both-sides | Both sides of an m2m pair emit a result | `unit: cmd/sqlgen/mcp: TestFindReferencing/users referenced by posts FK and both m2m junction sides` | all |  |
| mcp.surface.find-referencing.dedup | Results are deduplicated by the full tuple | `unit: cmd/sqlgen/mcp: TestDispatchFindReferencingDedupsByFullTuple` | all |  |
| mcp.surface.find-referencing.sorted | Results are sorted | `unit: cmd/sqlgen/mcp: TestFindReferencingKindTiebreak`; `unit: cmd/sqlgen/mcp: TestFindReferencing/users referenced by posts FK and both m2m junction sides` | all |  |
| mcp.surface.find-referencing.fk-columns-order | `fk_columns` lists the edge's FK columns in the foreign key's declared order, as a one-item list for a single-column key | `unit: cmd/sqlgen/mcp: TestFindReferencing/composite key in declared order`; `unit: cmd/sqlgen/mcp: TestFindReferencing/users referenced by posts FK and both m2m junction sides` | all |  |
| mcp.surface.find-referencing.fk-columns-whole | Dedup and sort compare `fk_columns` as a whole: a two-column key and a one-column key into the same table are two results | `unit: cmd/sqlgen/mcp: TestDispatchFindReferencingDedupsByFullTuple/two-column and one-column keys into one table` | all |  |
| mcp.surface.find-referencing.declaring-name | `relationship_name` is the declaring side's edge name | `unit: cmd/sqlgen/mcp: TestFindReferencing/posts referenced by comments FK from both relationship sides` | all |  |
| mcp.surface.find-referencing.declaring-kind | `kind` is the declaring edge's kind, as the declaring entity sees it | `unit: cmd/sqlgen/mcp: TestFindReferencing/posts referenced by comments FK from both relationship sides` | all |  |
| mcp.surface.join-path.cycle-free | `sqlgen_find_join_path` searches cycle-free: no entity appears twice on one path | `unit: cmd/sqlgen/mcp: TestFindJoinPath/cycle guard: Comment back to itself has no path` | all |  |
| mcp.surface.join-path.max-hops-default | `max_hops` defaults to 4 | `unit: cmd/sqlgen/mcp: TestDispatchFindJoinPathMaxHopsDefaultAndClamps/default reaches 4 hops`; `unit: cmd/sqlgen/mcp: TestDispatchFindJoinPathMaxHopsDefaultAndClamps/default stops before 5 hops`; `unit: cmd/sqlgen/mcp: TestDispatchFindJoinPathMaxHopsDefaultAndClamps/explicit 5 reaches 5 hops` | all |  |
| mcp.surface.join-path.max-hops-clamp-low | A `max_hops` below 1 is clamped to 1 | `unit: cmd/sqlgen/mcp: TestDispatchFindJoinPathMaxHopsDefaultAndClamps/0 clamps up to 1`; `unit: cmd/sqlgen/mcp: TestDispatchFindJoinPathMaxHopsDefaultAndClamps/negative clamps up to 1`; `unit: cmd/sqlgen/mcp: TestDispatchFindJoinPathMaxHopsDefaultAndClamps/0 clamps to 1, not more` | all |  |
| mcp.surface.join-path.max-hops-clamp-high | A `max_hops` above 6 is clamped to 6 | `unit: cmd/sqlgen/mcp: TestDispatchFindJoinPathMaxHopsDefaultAndClamps/10 clamps down to 6 and reaches 6 hops`; `unit: cmd/sqlgen/mcp: TestDispatchFindJoinPathMaxHopsDefaultAndClamps/10 clamps down to 6 and stops before 7 hops` | all |  |
| mcp.surface.join-path.cap | At most 5 paths are returned | `unit: cmd/sqlgen/mcp: TestFindJoinPathOrderingAndCap` | all |  |
| mcp.surface.join-path.shortest-first | Paths come in increasing-hop order | `unit: cmd/sqlgen/mcp: TestDispatchFindJoinPathShortestFirst` | all |  |
| mcp.surface.join-path.tie-break | Paths with equal hops are ordered lexicographically by serialized path | `unit: cmd/sqlgen/mcp: TestFindJoinPathOrderingAndCap` | all |  |
| mcp.surface.join-path.empty | When no path exists within `max_hops`, the result is an empty list | `unit: cmd/sqlgen/mcp: TestFindJoinPath/no path yields empty list`; `unit: cmd/sqlgen/mcp: TestFindJoinPath/max_hops boundary clamps the search` | all |  |
| mcp.surface.show-sql.reads-active-dialect | `sqlgen_show_sql` reads `entity.methods[].sql_bodies` for the manifest's active dialect | `unit: cmd/sqlgen/mcp: TestShowSQL/resolves the active dialect` | all |  |
| mcp.surface.show-sql.dialect-override | The `dialect` argument overrides the active dialect | `unit: cmd/sqlgen/mcp: TestShowSQL/dialect override without a matching body yields -32005` | all |  |
| mcp.surface.health.no-data-leak | `sqlgen_health` is a pure diagnostic: its response carries no manifest data beyond its listed fields | `unit: cmd/sqlgen/mcp: TestHealth/fresh load` | all |  |
| mcp.surface.health-ok-false | `ok: false` means a failed reload left the prior good manifest serving | `unit: cmd/sqlgen/mcp: TestStoreReloadRetainsGoodOnFailure`; `unit: cmd/sqlgen/mcp: TestWatcherRetainsGoodOnInvalidReload` | all |  |
| mcp.surface.validate-manifest.loaded | With no `path`, `sqlgen_validate_manifest` validates the loaded manifest | `unit: cmd/sqlgen/mcp: TestDispatchValidateManifestNoPathValidatesLoaded/schema-invalid loaded manifest is invalid`; `unit: cmd/sqlgen/mcp: TestDispatchValidateManifestNoPathValidatesLoaded/schema-valid loaded manifest is valid` | all |  |
| mcp.surface.validate-manifest.in-memory | With no `path`, validation runs in memory and does not re-read the manifest from disk | `unit: cmd/sqlgen/mcp: TestDispatchValidateManifestNoPathValidatesLoaded/file removed after load still validates in memory`; `unit: cmd/sqlgen/mcp: TestDispatchValidateManifestNoPathValidatesLoaded/file corrupted after load still validates in memory`; `unit: cmd/sqlgen/mcp: TestDispatchValidateManifestNoPathValidatesLoaded/schema-invalid loaded manifest is invalid` | all |  |
| mcp.surface.validate-manifest.path | With `path`, `sqlgen_validate_manifest` validates that file | `unit: cmd/sqlgen/mcp: TestValidateManifest/path mode validates without swapping the loaded manifest`; `unit: cmd/sqlgen/mcp: TestValidateManifest/invalid manifest reports flattened errors` | all |  |
| mcp.surface.validate-manifest.no-swap | Validating a `path` does not swap the served manifest | `unit: cmd/sqlgen/mcp: TestValidateManifest/path mode validates without swapping the loaded manifest` | all |  |
| mcp.surface.validate-manifest.error-shape | The validation error shape matches the §30.8 CLI tool | | all |  |

### Resources

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.surface.manifest-resource.listed | `sqlgen://manifest` is listed by `resources/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/resource manifest` | all | mcp-surface: manifest-resource × listed |
| mcp.surface.manifest-resource.output | `sqlgen://manifest` serves the full `manifest_gen.json` as `application/json` | `unit: cmd/sqlgen/mcp: TestManifestResourceServesRawWithoutWarningWhenSmall` | all | mcp-surface: manifest-resource × output |
| mcp.surface.manifest-resource.always-full | Above the 256 KB threshold `sqlgen://manifest` still serves the full document | `unit: cmd/sqlgen/mcp: TestManifestResourceWarnsAboveThreshold`; `unit: cmd/sqlgen/mcp: TestDispatchManifestWarningSurvivesToClient` | all |  |
| mcp.surface.manifest-resource.size-warning | Above the size threshold, the read result carries a size warning in the MCP `_meta` channel. Below it, there is no warning | `unit: cmd/sqlgen/mcp: TestManifestResourceWarnsAboveThreshold`; `unit: cmd/sqlgen/mcp: TestDispatchManifestWarningSurvivesToClient`; `unit: cmd/sqlgen/mcp: TestManifestResourceServesRawWithoutWarningWhenSmall` | all |  |
| mcp.surface.manifest-resource.size-warning.threshold | The size-warning threshold is 256 KB | `unit: cmd/sqlgen/mcp: TestDispatchManifestResourceThreshold/exactly 256 KB has no warning`; `unit: cmd/sqlgen/mcp: TestDispatchManifestResourceThreshold/one byte over 256 KB warns` | all |  |
| mcp.surface.manifest-resource.warning-advice | The size warning advises targeted tool calls instead | `unit: cmd/sqlgen/mcp: TestDispatchManifestResourceThreshold/warning advises targeted tool calls` | all |  |
| mcp.surface.entity-resource.listed | `sqlgen://entity/<name>` is served through an RFC 6570 template listed by `resources/templates/list`, not by `resources/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/template entity`; `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/entity template not in the resources list` | all | mcp-surface: entity-resource × listed |
| mcp.surface.entity-resource.not-in-resources-list | The entity template is not listed by `resources/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/entity template not in the resources list` | all |  |
| mcp.surface.entity-resource.output | `sqlgen://entity/<name>` serves that entity's JSON as `application/json` | `unit: cmd/sqlgen/mcp: TestEntityResourceServesFullRecord`; `unit: cmd/sqlgen/mcp: TestDispatchReadsEntityViaTemplate` | all | mcp-surface: entity-resource × output |
| mcp.surface.entity-resource.error | Reading `sqlgen://entity/<unknown>` fails with the JSON-RPC error `-32003` with `data.suggestions`. The suggestions also appear inline in the message | `unit: cmd/sqlgen/mcp: TestEntityResourceUnknownNameIsCodedErrorWithSuggestions`; `unit: cmd/sqlgen/mcp: TestDispatchUnknownEntityResourceSurfacesError` | all | mcp-surface: entity-resource × error |
| mcp.surface.conventions-resource.listed | `sqlgen://conventions` is listed by `resources/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/resource conventions` | all | mcp-surface: conventions-resource × listed |
| mcp.surface.conventions-resource.output | `sqlgen://conventions` is served as `application/json` | `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle` | all |  |
| mcp.surface.conventions-resource.output.block | `sqlgen://conventions` serves the conventions block | `unit: cmd/sqlgen/mcp: TestDispatchConventionsResourceServesBlock` | all | mcp-surface: conventions-resource × output |
| mcp.surface.config-resource.listed | `sqlgen://config` is listed by `resources/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/resource config` | all | mcp-surface: config-resource × listed |
| mcp.surface.config-resource.output | `sqlgen://config` serves the manifest's top-level `generation_config` as `application/json` | `unit: cmd/sqlgen/mcp: TestConfigResourceServesGenerationConfigWhenPresent`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle` | all | mcp-surface: config-resource × output |
| mcp.surface.config-resource.fallback | A manifest predating `generation_config` yields `{}` plus a warning | `unit: cmd/sqlgen/mcp: TestConfigResourceFallsBackWhenAbsent` | all |  |
| mcp.surface.resources-mime | All four resources are served as `application/json` | `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle` | all |  |

### Prompts

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.surface.write-query.listed | `sqlgen-write-query` is listed by `prompts/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/prompt sqlgen-write-query` | all | mcp-surface: write-query × listed |
| mcp.surface.write-query.output | `sqlgen-write-query({entity, goal})` asks the LLM to write a sqlgen query against `{entity}` that accomplishes `{goal}`, with both arguments substituted | `unit: cmd/sqlgen/mcp: TestExpandWriteQuerySubstitutesArguments`; `unit: cmd/sqlgen/mcp: TestDispatchGetPromptExpands` | all | mcp-surface: write-query × output |
| mcp.surface.write-query.loads | `sqlgen-write-query` loads `sqlgen_get_entity` and `sqlgen_get_conventions` | `unit: cmd/sqlgen/mcp: TestExpandWriteQuerySubstitutesArguments` | all |  |
| mcp.surface.write-query.not-compact | `sqlgen-write-query` loads the entity with `compact: false` | `unit: cmd/sqlgen/mcp: TestDispatchPromptExpansions/write-query loads the entity with compact false` | all |  |
| mcp.surface.write-query.go-snippet | `sqlgen-write-query` asks for a Go snippet referencing real method names from the entity's manifest entry | `unit: cmd/sqlgen/mcp: TestDispatchPromptExpansions/write-query asks for a Go snippet with real method names` | all |  |
| mcp.surface.write-query.error | `sqlgen-write-query` with a required argument missing fails with `-32602` | `unit: cmd/sqlgen/mcp: TestExpandMissingRequiredArgIsInvalidParams/write-query missing goal`; `unit: cmd/sqlgen/mcp: TestExpandMissingRequiredArgIsInvalidParams/write-query blank goal` | all | mcp-surface: write-query × error |
| mcp.surface.add-relationship-usage.listed | `sqlgen-add-relationship-usage` is listed by `prompts/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/prompt sqlgen-add-relationship-usage` | all | mcp-surface: add-relationship-usage × listed |
| mcp.surface.add-relationship-usage.output | `sqlgen-add-relationship-usage({entity, relationship})` loads `sqlgen_describe_relationship({entity, relationship})`, with both arguments substituted | `unit: cmd/sqlgen/mcp: TestExpandAddRelationshipSubstitutesArguments` | all | mcp-surface: add-relationship-usage × output |
| mcp.surface.add-relationship-usage.example | `sqlgen-add-relationship-usage` loads the entity's example | `unit: cmd/sqlgen/mcp: TestDispatchPromptExpansions/add-relationship-usage loads the entity example` | all |  |
| mcp.surface.add-relationship-usage.walkthrough | `sqlgen-add-relationship-usage` walks the user through preloading, joining and writing across the relationship | `unit: cmd/sqlgen/mcp: TestDispatchPromptExpansions/add-relationship-usage walks through preloading joining writing` | all |  |
| mcp.surface.add-relationship-usage.caveats | `sqlgen-add-relationship-usage` includes the pagination and filter caveats from the conventions block | `unit: cmd/sqlgen/mcp: TestDispatchPromptExpansions/add-relationship-usage includes pagination and filter caveats` | all |  |
| mcp.surface.add-relationship-usage.error | `sqlgen-add-relationship-usage` with a required argument missing fails with `-32602` | `unit: cmd/sqlgen/mcp: TestExpandMissingRequiredArgIsInvalidParams/add-relationship missing relationship` | all | mcp-surface: add-relationship-usage × error |
| mcp.surface.debug-not-found.listed | `sqlgen-debug-not-found` is listed by `prompts/list` | `unit: cmd/sqlgen/mcp: TestDispatchListsPRDSurface/prompt sqlgen-debug-not-found` | all | mcp-surface: debug-not-found × listed |
| mcp.surface.debug-not-found.output | `sqlgen-debug-not-found({entity, method?})` diagnoses a `NotFound` from `{method}`, or from any method on `{entity}` when `method` is omitted | `unit: cmd/sqlgen/mcp: TestExpandDebugNotFoundBranchesOnMethod` | all | mcp-surface: debug-not-found × output |
| mcp.surface.debug-not-found.loads-entity | `sqlgen-debug-not-found` loads `sqlgen_get_entity({entity})` | `unit: cmd/sqlgen/mcp: TestDispatchPromptExpansions/debug-not-found loads the entity` | all |  |
| mcp.surface.debug-not-found.loads-sentinels | `sqlgen-debug-not-found` loads the conventions block's error-sentinel section | `unit: cmd/sqlgen/mcp: TestDispatchPromptExpansions/debug-not-found loads the error sentinels` | all |  |
| mcp.surface.debug-not-found.checks | `sqlgen-debug-not-found` suggests checks against tenancy filtering, soft-delete state and PK shape | `unit: cmd/sqlgen/mcp: TestDispatchPromptExpansions/debug-not-found suggests tenancy soft-delete and PK checks` | all |  |
| mcp.surface.debug-not-found.error | `sqlgen-debug-not-found` with its required `entity` missing fails with `-32602` | `unit: cmd/sqlgen/mcp: TestExpandMissingRequiredArgIsInvalidParams/debug-not-found missing entity` | all | mcp-surface: debug-not-found × error |
| mcp.surface.prompts.stateless | Prompt expansion is stateless template substitution that tells the LLM to call the relevant tools, with no pre-fetching | `unit: cmd/sqlgen/mcp: TestExpandWriteQuerySubstitutesArguments`; `unit: cmd/sqlgen/mcp: TestExpandAddRelationshipSubstitutesArguments` | all |  |
| mcp.surface.prompts.no-list-changed | No `prompts/list_changed` notification is ever emitted, including on a watch reload | `unit: cmd/sqlgen/mcp: TestDispatchReloadNeverSendsPromptListChanged` | all |  |
| mcp.surface.prompts.degrade | Clients without prompt support degrade cleanly | | all |  |

## §31.4 Protocol
<!-- prd: 31.4 sha256: 259c8c97fea78796 -->

### SDK, handshake, version negotiation

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.proto.official-sdk | The server implements the MCP server side via the official Go SDK `github.com/modelcontextprotocol/go-sdk` | `unit: cmd/sqlgen/mcp: TestNewUsesOfficialSDKServer` | all |  |
| mcp.proto.sdk-cli-module-only | The go-sdk dependency is scoped to the `cmd/sqlgen` module only: the runtime and parser modules do not require it | `unit: cmd/sqlgen/mcp: TestGoSDKRequiredOnlyByCLIModule/cli module requires go-sdk`; `unit: cmd/sqlgen/mcp: TestGoSDKRequiredOnlyByCLIModule/runtime module does not require go-sdk`; `unit: cmd/sqlgen/mcp: TestGoSDKRequiredOnlyByCLIModule/parser module does not require go-sdk` | all |  |
| mcp.proto.version-negotiation | Version negotiation happens at `initialize`: the server answers with a protocol version negotiated from the client's requested one | `unit: cmd/sqlgen/mcp: TestHandshakeNegotiatesProtocolVersion/supported 2024-11-05 is kept`; `unit: cmd/sqlgen/mcp: TestHandshakeNegotiatesProtocolVersion/supported 2025-03-26 is kept`; `unit: cmd/sqlgen/mcp: TestHandshakeNegotiatesProtocolVersion/supported 2025-06-18 is kept`; `unit: cmd/sqlgen/mcp: TestHandshakeNegotiatesProtocolVersion/unsupported version is answered with a supported one` | all |  |
| mcp.proto.capabilities-three | The `initialize` handshake advertises the tools, resources and prompts capabilities | `unit: cmd/sqlgen/mcp: TestHandshakeAdvertisesExactlyThreeCapabilities`; `example: postgres: TestMCP_FullSurface` | all | |
| mcp.proto.capabilities-nothing-else | The handshake advertises no other capability: no logging and no resource subscriptions (sampling is not advertised either) | `unit: cmd/sqlgen/mcp: TestHandshakeAdvertisesExactlyThreeCapabilities` | all | |

### Transports

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.proto.stdio-newline-json | The stdio transport is newline-delimited JSON-RPC 2.0 over stdin/stdout: each message is one JSON line | `example: postgres: TestMCP_FullSurface`; `example: mysql: TestMCP_FullSurface`; `example: sqlite: TestMCP_FullSurface`; `example: graphql: TestMCP_FullSurface` | all |  |
| mcp.proto.stdio-push | The stdio transport delivers server-pushed notifications to the client | `integration: cmd/sqlgen/mcp/integration: TestStdioWatchReloadNotifiesAndServesNewData` | all | |
| mcp.proto.http-endpoint | Streamable HTTP serves MCP at `http://127.0.0.1:<port>/mcp` | `integration: cmd/sqlgen/mcp/integration: TestHTTPLifecycle` | all | |
| mcp.proto.http-single-endpoint | `/mcp` is the only HTTP endpoint: no other path serves MCP | `unit: cmd/sqlgen/mcp: TestServeHTTPAnswersOnlyMCPPath` | all |  |
| mcp.proto.http-loopback | The HTTP transport binds loopback only: a wildcard, external-IP or hostname bind is refused | `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr`; `unit: cmd/sqlgen/cli: TestMCPServe_RefusesNonLoopbackHTTP` | all | |
| mcp.proto.http-stateless | The HTTP transport is stateless: it keeps no session between requests | `unit: cmd/sqlgen/mcp: TestRunHTTPKeepsNoSession/no session header and no initialize`; `unit: cmd/sqlgen/mcp: TestRunHTTPKeepsNoSession/session id never issued` | all |  |
| mcp.proto.http-no-push | The HTTP transport pushes no notification on a watch-mode reload | `integration: cmd/sqlgen/mcp/integration: TestHTTPWatchReloadNoPushClientRefetches` | all | |
| mcp.proto.http-refetch-sees-reload | After a reload, an HTTP client's next request (its refetch) is served from the new manifest | `integration: cmd/sqlgen/mcp/integration: TestHTTPWatchReloadNoPushClientRefetches` | all | |
| mcp.proto.reload-tools-list-changed | On a watch-mode reload the server emits `notifications/tools/list_changed` over stdio | `integration: cmd/sqlgen/mcp/integration: TestStdioWatchReloadNotifiesAndServesNewData` | all | |
| mcp.proto.reload-resources-list-changed | On a watch-mode reload the server emits `notifications/resources/list_changed` over stdio | `integration: cmd/sqlgen/mcp/integration: TestStdioWatchReloadNotifiesAndServesNewData` | all | |

### Error codes

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.proto.err-manifest-not-found-given | `-32001` `MANIFEST_NOT_FOUND` when no manifest is at the given `--manifest` path | `unit: cmd/sqlgen/cli: TestMCPServe_StartupFailureReportsReservedCode/explicit manifest path does not exist`; `unit: cmd/sqlgen/cli: TestMCPServe_StartupFailureStderrFormat/missing manifest` | all |  |
| mcp.proto.err-manifest-not-found-discovered | `-32001` when discovery (walking up from CWD) finds no manifest | `unit: cmd/sqlgen/cli: TestMCPServe_StartupFailureReportsReservedCode/discovery finds no manifest` | all |  |
| mcp.proto.err-manifest-invalid | `-32002` `MANIFEST_INVALID` when the manifest fails JSON Schema validation | `unit: cmd/sqlgen/cli: TestMCPServe_StartupFailureReportsReservedCode/manifest fails schema validation`; `unit: cmd/sqlgen/cli: TestMCPServe_StartupFailureStderrFormat/schema-invalid manifest` | all |  |
| mcp.proto.err-entity-not-found | `-32003` `ENTITY_NOT_FOUND` for an unknown entity name | `unit: cmd/sqlgen/mcp: TestGetEntity/unknown name yields -32003 with suggestions`; `unit: cmd/sqlgen/mcp: TestShowSQL/unknown entity yields -32003`; `unit: cmd/sqlgen/mcp: TestDescribeRelationship/unknown entity yields -32003`; `unit: cmd/sqlgen/mcp: TestGetExample/unknown entity yields -32003` | all | |
| mcp.proto.err-relationship-not-found | `-32004` `RELATIONSHIP_NOT_FOUND` for a relationship name not on the named entity | `unit: cmd/sqlgen/mcp: TestDescribeRelationship/unknown relationship yields -32004 with suggestions` | all | |
| mcp.proto.err-sql-unavailable-predates | `-32005` `METHOD_SQL_UNAVAILABLE` when `show_sql` targets a method that carries no `sql_bodies` (a manifest predating them) | `unit: cmd/sqlgen/mcp: TestShowSQL/method without sql_bodies yields -32005` | all | |
| mcp.proto.err-sql-unavailable-dialect | `-32005` when the method has no SQL body for the resolved dialect | `unit: cmd/sqlgen/mcp: TestShowSQL/dialect override without a matching body yields -32005` | all | |
| mcp.proto.err-method-not-found | `-32006` `METHOD_NOT_FOUND` when `show_sql` names a method not defined on the entity | `unit: cmd/sqlgen/mcp: TestShowSQL/unknown method yields -32006 with suggestions` | all | |
| mcp.proto.std-parse-error | A malformed JSON message gets the standard `-32700` parse error | | all | |
| mcp.proto.std-invalid-request | A message that is not a valid JSON-RPC request gets the standard `-32600` | | all | |
| mcp.proto.std-method-not-found | An unknown JSON-RPC method gets the standard `-32601` | | all | |
| mcp.proto.std-invalid-params | Invalid params get the standard `-32602`, unchanged | `unit: cmd/sqlgen/mcp: TestExpandMissingRequiredArgIsInvalidParams` | all | |
| mcp.proto.std-internal | An internal failure gets the standard `-32603`, unchanged | `unit: cmd/sqlgen/mcp: TestValidateManifest/unreadable path yields an internal error` | all | |

### Typo-tolerant suggestions

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.proto.suggest-on-not-found | `-32003`, `-32004` and `-32006` carry a `suggestions` string array | `unit: cmd/sqlgen/mcp: TestGetEntity/unknown name yields -32003 with suggestions`; `unit: cmd/sqlgen/mcp: TestDescribeRelationship/unknown relationship yields -32004 with suggestions`; `unit: cmd/sqlgen/mcp: TestShowSQL/unknown method yields -32006 with suggestions` | all | |
| mcp.proto.suggest-candidate-set | Suggestions come from the candidate set of the missing kind: entity names for `-32003`, relationship names for `-32004`, method names for `-32006` | `unit: cmd/sqlgen/mcp: TestGetEntity/unknown name yields -32003 with suggestions`; `unit: cmd/sqlgen/mcp: TestDescribeRelationship/unknown relationship yields -32004 with suggestions`; `unit: cmd/sqlgen/mcp: TestShowSQL/unknown method yields -32006 with suggestions` | all |  |
| mcp.proto.suggest-candidate-set.entity-scoped | For `-32004` and `-32006` the candidates are the named entity's own relationship or method names | `unit: cmd/sqlgen/mcp: TestDispatchSuggestions/show_sql suggests only the named entity's methods`; `unit: cmd/sqlgen/mcp: TestDispatchSuggestions/show_sql suggests the method on the entity that has it`; `unit: cmd/sqlgen/mcp: TestDispatchSuggestions/describe_relationship suggests only the named entity's relationships` | all |  |
| mcp.proto.suggest-threshold | A candidate is suggested only within Levenshtein distance ≤ 2 | `unit: cmd/sqlgen/mcp: TestSuggest/within threshold, sorted by distance then name`; `unit: cmd/sqlgen/mcp: TestSuggest/nothing within threshold yields empty`; `unit: cmd/sqlgen/mcp: TestLevenshtein` | all | |
| mcp.proto.suggest-case-insensitive | Matching is case-insensitive | `unit: cmd/sqlgen/mcp: TestDispatchSuggestions/get_entity matches ignoring case`; `unit: cmd/sqlgen/mcp: TestDispatchSuggestions/describe_relationship matches ignoring case`; `unit: cmd/sqlgen/mcp: TestDispatchSuggestions/show_sql matches ignoring case`; `unit: cmd/sqlgen/mcp: TestDispatchSuggestions/suggest matches ignoring case` | all |  |
| mcp.proto.suggest-top-3 | At most 3 suggestions are returned | `unit: cmd/sqlgen/mcp: TestSuggest/top-3 cap` | all | |
| mcp.proto.suggest-order | Suggestions at the same distance are sorted lexicographically | `unit: cmd/sqlgen/mcp: TestSuggest/distance tie broken lexicographically`; `unit: cmd/sqlgen/mcp: TestSuggest/top-3 cap` | all |  |
| mcp.proto.suggest-order.distance-first | Suggestions are sorted by distance first | `unit: cmd/sqlgen/mcp: TestDispatchSuggestions/get_entity orders by distance before name`; `unit: cmd/sqlgen/mcp: TestDispatchSuggestions/suggest orders by distance before name` | all |  |
| mcp.proto.suggest-empty | `suggestions` is empty, not absent, when nothing is within the threshold | `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32003 far miss has empty suggestions`; `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32004 far miss has empty suggestions`; `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32006 far miss has empty suggestions`; `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/resource far miss has empty data.suggestions` | all |  |

### Error channels

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.proto.tool-error-is-error-result | A per-tool business error (`-32003` from a tool) is an MCP `IsError` tool result, not a protocol-level JSON-RPC error | `unit: cmd/sqlgen/mcp: TestDispatchNotFoundIsToolError`; `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle`; `integration: cmd/sqlgen/mcp/integration: TestHTTPLifecycle`; `example: postgres: TestMCP_FullSurface` | all | |
| mcp.proto.tool-error-other-codes | `-32004`, `-32005`, `-32006` and `-32603` from a tool body also surface as `IsError` results carrying their code | `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32004 unknown relationship`; `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32005 no sql body`; `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32006 unknown method`; `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32603 internal from a tool body` | all |  |
| mcp.proto.tool-error-text-inlines-suggestions | The `IsError` result's text content is a readable message with the suggestions inlined | `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32003 unknown entity`; `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32004 unknown relationship`; `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32006 unknown method` | all |  |
| mcp.proto.tool-error-envelope-code-suggestions | The structured content is a flattened envelope: `code` and `suggestions` at the top level, not nested under `data` | `unit: cmd/sqlgen/mcp: TestDispatchNotFoundIsToolError`; `example: postgres: TestMCP_FullSurface`; `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32003 unknown entity` | all |  |
| mcp.proto.tool-error-envelope-message | The structured envelope carries `message` | `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32003 unknown entity`; `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32004 unknown relationship`; `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32005 no sql body`; `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32006 unknown method`; `unit: cmd/sqlgen/mcp: TestDispatchToolErrorChannel/-32603 internal from a tool body` | all |  |
| mcp.proto.tool-error-session-usable | After a tool business error the session stays usable | `unit: cmd/sqlgen/mcp: TestDispatchNotFoundIsToolError` | all | |
| mcp.proto.startup-stderr | A startup failure (`-32001` / `-32002`) opens no transport: `sqlgen mcp serve` exits 1 and its stderr line names the code and its name (`Error: -32001 MANIFEST_NOT_FOUND: <message>`) | `unit: cmd/sqlgen/cli: TestMCPServe_StartupFailureReportsReservedCode`; `unit: cmd/sqlgen/cli: TestMCPServe_StartupFailureStderrFormat` | all |  |
| mcp.proto.resource-unknown-entity-code | Reading `sqlgen://entity/<unknown>` is a JSON-RPC error with code `-32003` | `unit: cmd/sqlgen/mcp: TestEntityResourceUnknownNameIsCodedErrorWithSuggestions`; `example: postgres: TestMCP_FullSurface`; `example: mysql: TestMCP_FullSurface`; `example: sqlite: TestMCP_FullSurface`; `example: graphql: TestMCP_FullSurface` | all | |
| mcp.proto.resource-unknown-entity-data-suggestions | That error carries `data.suggestions` | `unit: cmd/sqlgen/mcp: TestEntityResourceUnknownNameIsCodedErrorWithSuggestions`; `example: postgres: TestMCP_FullSurface` | all | |
| mcp.proto.resource-unknown-entity-message-suggestions | The suggestions are also inlined into the error message, so they survive a client that collapses coded errors | `unit: cmd/sqlgen/mcp: TestEntityResourceUnknownNameIsCodedErrorWithSuggestions`; `unit: cmd/sqlgen/mcp: TestDispatchUnknownEntityResourceSurfacesError` | all | |

## §31.5 Versioning Policy
<!-- prd: 31.5 sha256: 5feb4bbbeeb7b18f -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| mcp.ver.health-schema-version | `sqlgen_health` reports the served manifest's `schema_version` | `unit: cmd/sqlgen/mcp: TestHealth/fresh load` | all | |
| mcp.ver.health-sqlgen-version | `sqlgen_health` reports `sqlgen_version` | `unit: cmd/sqlgen/mcp: TestHealth/fresh load` | all | |
| mcp.ver.initialize-server-version | The `initialize` handshake advertises, as the server implementation version, the version the server is built with | `unit: cmd/sqlgen/mcp: TestHandshakeReportsServerVersion` | all | |
| mcp.ver.initialize-cli-version | That version is the sqlgen CLI version: `sqlgen mcp serve`'s `serverInfo.version` equals the binary's own version | `unit: cmd/sqlgen/mcp: TestMCPServeServerInfoIsCLIVersion` | all |  |
| mcp.ver.input-shapes-stable | Tool, resource and prompt input shapes are stable within a schema major version | | all | |

Not drafted as rows (informative): "the MCP surface versions with the manifest `schema_version` it serves", "pre-1.0 while `schema_version` is 0.x" and "additive tools or optional inputs may land in minors". These are policy, with no behavior a test can fail on. Name stability is pinned by the §31.3 `listed` cells (TestDispatchListsAllElevenTools, the integration `wantTools`).

## Not applicable

| Matrix cell | Reason | Accepted |
|---|---|---|
| mcp-surface: list-entities × error | §31.3: `sqlgen_list_entities` takes only an optional `kind` filter, and §31.3 and §31.4 assign it no failure | |
| mcp-surface: find-method × error | §31.4: no code is assigned to `sqlgen_find_method`. `-32006` belongs to `show_sql`, and a miss is an empty match list | |
| mcp-surface: find-referencing × error | §31.3: `sqlgen_find_referencing` takes a table name and is assigned no failure. §31.4's codes name entities, relationships and methods, not tables | |
| mcp-surface: get-conventions × error | §31.3: `sqlgen_get_conventions` takes `{}` and is assigned no failure | |
| mcp-surface: health × error | §31.3: `sqlgen_health` takes `{}`. `ok: false` is a field of a successful response, not a coded failure | |
| mcp-surface: validate-manifest × error | §31.3: `sqlgen_validate_manifest` reports a failing manifest as `valid: false` with `errors`, a field of a successful response, and §31.4 assigns it no code | |
| mcp-surface: manifest-resource × deterministic | §31.3 promises byte-equal repeat responses for tools only | |
| mcp-surface: entity-resource × deterministic | §31.3 promises byte-equal repeat responses for tools only | |
| mcp-surface: conventions-resource × deterministic | §31.3 promises byte-equal repeat responses for tools only | |
| mcp-surface: config-resource × deterministic | §31.3 promises byte-equal repeat responses for tools only | |
| mcp-surface: write-query × deterministic | §31.3 promises byte-equal repeat responses for tools only | |
| mcp-surface: add-relationship-usage × deterministic | §31.3 promises byte-equal repeat responses for tools only | |
| mcp-surface: debug-not-found × deterministic | §31.3 promises byte-equal repeat responses for tools only | |
| mcp-surface: manifest-resource × error | §31.3: `sqlgen://manifest` is a fixed URI with no input. §31.4 gives resource reads only the unknown-entity failure | |
| mcp-surface: conventions-resource × error | §31.3: `sqlgen://conventions` is a fixed URI with no input. §31.4 gives resource reads only the unknown-entity failure | |
| mcp-surface: config-resource × error | §31.3: `sqlgen://config` is a fixed URI with no input. A pre-addendum manifest yields `{}` plus a warning, not an error | |

## Unclaimed

Tests in this file's packages that back no PRD behavior.

| Unclaimed test | Reason |
|---|---|
| cmd/sqlgen/mcp/integration: TestClaudeCodeSmoke | Harness: real-client smoke gated on CLAUDE_CODE_BIN (skipped by default), backs no single PRD row |
| cmd/sqlgen/mcp: TestConventionsResourceMatchesManifest | Checks only client_entry_points and error_sentinels[0]; superseded by TestDispatchConventionsResourceServesBlock for conventions-resource.output.block |
| cmd/sqlgen/mcp: TestDispatchGetPromptMissingArgIsError | Asserts only err != nil, not -32602 (the go-sdk client collapses the code); -32602 is pinned by TestExpandMissingRequiredArgIsInvalidParams |
| cmd/sqlgen/mcp: TestDispatchGetPromptUnknownIsError | No PRD row: §31.3/§31.4 assign no code to an unknown prompt name, and the test asserts only err != nil plus session reuse |
| cmd/sqlgen/mcp: TestEntityNameFromURI | Helper: URI-to-name parsing internal to the template handler |
| cmd/sqlgen/mcp: TestErrorCode_String | internal String() rendering; the observable "<code> <NAME>" stderr line is pinned by cmd/sqlgen/cli TestMCPServe_StartupFailure* (mcp.proto.startup-stderr) |
| cmd/sqlgen/mcp: TestGetEntity_MaterializedViewMarker | The `materialized` marker is not in §31.3's retained-identity list; the marker's own rows are in manifest.md and backed by the manifest builder tests |
| cmd/sqlgen/mcp: TestOffsetLineCol | helper (offset to line/col); the observable positions are pinned by TestMergeInvalidJSONErrorNamesLineAndColumn |
| cmd/sqlgen/mcp: TestRegisterToolsInstallsEleven | Internal invariant (schema inference does not panic on re-registration); asserts neither the listed set nor a notification |
| cmd/sqlgen/mcp: TestServeStopsOnContextCancel | Internal invariant of Server.Serve on ctx cancel; no §31 row for shutdown (transport-level shutdown is TestRunGracefulShutdown) |
| cmd/sqlgen/mcp: TestStoreConcurrentReadsDuringReload | Race-detector invariant behind §31.1's atomic swap, which has no drafted row; asserts nothing beyond the absence of a data race |
| cmd/sqlgen/mcp: TestStoreLoadValid | internal Store invariant (Load/Raw/Health struct); serving the given manifest is backed by TestStdioLifecycle (mcp.cfg.manifest-flag-serves-path) |
| cmd/sqlgen/mcp: TestWatcherDebounceCoalesces | Debounce is an implementation detail; no PRD text promises coalescing |

## PRD gaps

Behavior the code has, most of it pinned by tests, that no PRD text states. Each waits for the user's ruling.

| Gap | Behavior | Tests | Section | Ruling |
|---|---|---|---|---|
| gap.mcp.list-outputs-wrapped | List-shaped tool outputs are wrapped in an object rather than returned as the bare array §31.3 shows: `{entities: […]}`, `{matches: […]}`, `{references: […]}`, `{paths: […]}`. MCP structured content must be an object | `integration: cmd/sqlgen/mcp/integration: TestStdioLifecycle` | §31.3 |  |
| gap.mcp.find-referencing-entity | `entity` in a `sqlgen_find_referencing` result is the entity holding the FK column, not the declaring entity. For `User.posts` (o2m), the result is `{entity: Post, relationship_name: posts}`. §31.3 says the result is "emitted from whichever entity declared the relationship" and leaves `entity` undefined | `unit: cmd/sqlgen/mcp: TestFindReferencing/users referenced by posts FK and both m2m junction sides`; `unit: cmd/sqlgen/mcp: TestFindReferencingSameNamedTablesAcrossSchemas` | §31.3 |  |
| gap.mcp.find-referencing-unknown-table | `sqlgen_find_referencing` on an unknown table returns an empty list, not an error | `unit: cmd/sqlgen/mcp: TestFindReferencing/unknown table yields empty, not error` | §31.3 |  |
| gap.mcp.find-referencing-bare-table | `table` is matched by bare name, so same-named tables in two schemas merge (known backlog finding) | `unit: cmd/sqlgen/mcp: TestFindReferencingSameNamedTablesAcrossSchemas` | §31.3 |  |
| gap.mcp.find-method-matching | `sqlgen_find_method` matches case-insensitively and exactly by default. `fuzzy: true` switches to a case-insensitive substring match. No match gives an empty list | `unit: cmd/sqlgen/mcp: TestFindMethod/exact case-insensitive match`; `unit: cmd/sqlgen/mcp: TestFindMethod/exact match does not substring-match`; `unit: cmd/sqlgen/mcp: TestFindMethod/fuzzy substring match`; `unit: cmd/sqlgen/mcp: TestFindMethod/no match returns empty, not nil`; `unit: cmd/sqlgen/mcp: TestFindMethod_MatviewRefreshMethods` | §31.3 |  |
| gap.mcp.method-signature-format | `signature` is a Go-style string that puts `ctx context.Context` first and omits CallOptions. It has three return shapes: `(T, error)`, `error` and `iter.Seq2[…]` | `unit: cmd/sqlgen/mcp: TestMethodSignature_ReturnShapes` | §31.3 |  |
| gap.mcp.features-summary | `features_summary` is the sorted, comma-joined list of active features (`audit_columns`, `cache`, `events`, `soft_delete`, `tenancy`), or `""` | `unit: cmd/sqlgen/mcp: TestFeaturesSummary_TenantedView`; `unit: cmd/sqlgen/mcp: TestListEntities/all entities sorted by name` | §31.3 |  |
| gap.mcp.describe-relationship-target-summary | `sqlgen_describe_relationship` adds a `target` summary `{name, table, pk_kind, features_summary}`, and `compact: true` drops it. §31.3's compact list does not name the target summary as droppable | `unit: cmd/sqlgen/mcp: TestDescribeRelationship/m2m relationship with junction and target summary`; `unit: cmd/sqlgen/mcp: TestDescribeRelationship/compact drops the target summary but keeps join shape` | §31.3 |  |
| gap.mcp.get-example-empty | `sqlgen_get_example` on an entity with no examples returns empty `read` and `write` arrays. Its output shape is `{entity, read, write}` | `unit: cmd/sqlgen/mcp: TestGetExample/entity without examples yields empty arrays` | §31.3 |  |
| gap.mcp.validate-manifest-unreadable | `sqlgen_validate_manifest` with an unreadable `path` fails with `-32603` | `unit: cmd/sqlgen/mcp: TestValidateManifest/unreadable path yields an internal error` | §31.3, §31.4 |  |
| gap.mcp.validate-manifest-schema-source | The tool always validates against the embedded schema. The §30.8 CLI fetches the manifest's `$schema` URL and falls back to the embedded schema | | §31.3, §30.8 |  |
| gap.mcp.validate-error-shape | §30.8 states no error shape. The tool flattens schema errors to their leaf causes as `{field: <instance location or "/">, msg}` | `unit: cmd/sqlgen/mcp: TestValidateManifest/invalid manifest reports flattened errors` | §31.3, §30.8 |  |
| gap.mcp.http-flag-forms | `--http` accepts a bare port, `:port`, or a loopback IP with a port (`127.0.0.2:9000`, `[::1]:8080`), and binds that IP rather than only `127.0.0.1`. It refuses any hostname, including `localhost`, and ports outside `[1, 65535]` | `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/bare port`; `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/empty host defaults to loopback`; `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/explicit loopback ipv4`; `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/loopback ipv4 in 127 block`; `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/loopback ipv6`; `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/localhost hostname refused`; `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/empty flag refused`; `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/non-numeric port refused`; `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/port zero refused`; `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr/port out of range refused` | §31.2 |  |
| gap.mcp.log-level-lenient | `--log-level` is case-insensitive, ignores padding, and treats an empty value as `info` | `unit: cmd/sqlgen/mcp: TestNewLogger/case insensitive`; `unit: cmd/sqlgen/mcp: TestNewLogger/padded`; `unit: cmd/sqlgen/mcp: TestNewLogger/empty defaults to info` | §31.2 |  |
| gap.mcp.no-transport | With `--stdio=false` and no `--http`, the server refuses to start (no transport enabled) | `unit: cmd/sqlgen/mcp: TestRunNoTransportEnabled`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/mcp serve with no transport` | §31.2 |  |
| gap.mcp.merge-default-command | Without `command_template`, the entry's command is `sqlgen` with `args` `[mcp, serve, --manifest, <path>]`. The stored path is `./`-prefixed and slash-separated when relative, and verbatim when absolute | `unit: cmd/sqlgen/mcp: TestMergeCreatesNewFile`; `unit: cmd/sqlgen/mcp: TestMergeAbsoluteManifestPathStoredVerbatim` | §31.2 |  |
| gap.mcp.merge-managed-key-holder | When `server_key` is held by a managed entry serving another, live manifest, the new entry takes the next free key without a warning | `unit: cmd/sqlgen/mcp: TestMergeManagedKeyOccupiedByLiveSiblingPicksFreeKey` | §31.2 |  |
| gap.mcp.merge-non-object-servers | A `mcpServers` that is not an object fails the merge without overwriting the file | `unit: cmd/sqlgen/mcp: TestMergeNonObjectServersFails` | §31.2 |  |
| gap.mcp.merge-other-top-level | Unrelated top-level fields survive an upsert | `unit: cmd/sqlgen/mcp: TestMergeUpsertAppendsAlongsideOtherTools` | §31.2 |  |
| gap.mcp.merge-nested-target-dir | A nested target such as `.cursor/mcp.json` gets its parent directory created | `unit: cmd/sqlgen/mcp: TestMergeCreatesNestedTargetDirectory` | §31.2 |  |
| gap.mcp.merge-remove-no-file | A removal run with no target file is a no-op | `unit: cmd/sqlgen/mcp: TestMergeRemoveNoFileIsNoOp` | §31.2 |  |
| gap.mcp.merge-leftover-tmp | A leftover `<target>.tmp` from an interrupted run is replaced and not left behind | `unit: cmd/sqlgen/mcp: TestMergeLeftoverTmpFileIsReplaced` | §31.2 |  |
| gap.mcp.command-template-errors | A `command_template` with an unbalanced quote, or one that renders to nothing, fails the target. An empty quoted token survives, and a token adjacent to a quote merges with it (`--label="x y"`) | `unit: cmd/sqlgen/mcp: TestMergeCommandTemplateRendering/unbalanced quote is an error`; `unit: cmd/sqlgen/mcp: TestMergeCommandTemplateRendering/whitespace-only render is an error`; `unit: cmd/sqlgen/mcp: TestTokenizeCommand/unbalanced double quote`; `unit: cmd/sqlgen/mcp: TestTokenizeCommand/unbalanced single quote`; `unit: cmd/sqlgen/mcp: TestTokenizeCommand/empty quoted token survives`; `unit: cmd/sqlgen/mcp: TestTokenizeCommand/adjacent quote merges into token`; `unit: cmd/sqlgen/mcp: TestTokenizeCommand/collapsed whitespace`; `unit: cmd/sqlgen/mcp: TestTokenizeCommand/empty input` | §31.2 |  |
| gap.mcp.manifest-identity-path | The entry's manifest path is `<output.dir>/<markdown_dir>/<json_filename>` (default `manifest/manifest_gen.json`). The `SQLGEN_OUTPUT_DIR_IMPORT_OVERRIDE` environment variable replaces `output.dir` in that identity path, a hook for the golden harness | `unit: cmd/sqlgen/cli: TestManifestIdentityPathHonorsImportOverride` | §31.2, §30.3 |  |
| gap.mcp.per-entity-resolution | For a `per_entity` manifest, the server reads full entity records from the sibling files the index names. A missing `file` reference or an unreadable file fails the load with `-32002` | `unit: cmd/sqlgen/mcp: TestResolveEntitiesPerEntity` | §31 |  |
| gap.mcp.unreadable-manifest-internal | A manifest path that exists but cannot be read (a directory, permission denied) fails startup with `-32603`, not `-32001` | `unit: cmd/sqlgen/mcp: TestStoreLoadReadErrorIsInternal` | §31.4 | |
| gap.mcp.malformed-json-manifest-invalid | A manifest that is not JSON at all is `-32002` `MANIFEST_INVALID` (the PRD defines -32002 as failing JSON Schema validation) | `unit: cmd/sqlgen/mcp: TestStoreLoadErrors/malformed json` | §31.4 | |
| gap.mcp.schema-version-mismatch-warns | A schema-valid manifest whose `schema_version` differs from the one sqlgen was built against loads, serves, and logs a warning | `unit: cmd/sqlgen/mcp: TestStoreSchemaVersionMismatch` | §31.5 | |
| gap.mcp.unknown-tool-invalid-params | `tools/call` with an unknown tool name is a JSON-RPC `-32602` protocol error (probe: `unknown tool "sqlgen_nope"`), not an IsError result | | §31.4 | |
| gap.mcp.bad-tool-args-is-error-text | Tool arguments that fail the input schema (e.g. `name: 5`) give an `IsError` result with text only and no `{code, …}` envelope (SDK-generated) | | §31.4 | |
| gap.mcp.non-coded-tool-error | A tool error that is not a coded tool error becomes an SDK `IsError` result with text and no envelope (`tools.go:60`). No tool reaches this path today | | §31.4 | |
| gap.mcp.prompts-capability-no-list-changed | The prompts capability is advertised with `listChanged: false`, and `resources.subscribe` is false. (§31.3 says no `prompts/list_changed` is ever emitted; the capability flag itself is unstated) | `unit: cmd/sqlgen/mcp: TestHandshakeAdvertisesExactlyThreeCapabilities` | §31.3, §31.4 | |
