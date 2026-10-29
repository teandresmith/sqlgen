# CLI Requirements: Commands

<!-- seeding: complete -->
<!-- packages: cmd/sqlgen cmd/sqlgen/cli cmd/sqlgen/wrapper -->
<!-- matrix: commands rows=command cols=success,config-error,schema-error,generation-error -->

The CLI surface (§23): commands, flags, config discovery, `init`, `completion`, `diff`, `lint`, stale-file cleanup, exit codes and watch mode. The `commands` matrix has one row per §23.1 command plus bare `sqlgen`; `success` is the command doing its §23 job (exit 0, or `diff`'s exit 1 on changes), `config-error` exit 2, `schema-error` exit 3 and `generation-error` exit 1 (§23.9). Exit 4 has no column. The `manifest` and `mcp serve` rows hold only what §23.1 says; §30.8 and §31 are in [`manifest.md`](manifest.md).

## §23.1 Commands
<!-- prd: 23.1 sha256: 20f26d48af5dfa18 -->

### `sqlgen generate` and bare `sqlgen`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.generate.success | `sqlgen generate` runs code generation using the config file and exits 0 | `unit: cmd/sqlgen/cli: TestGenerateSubcommand`; `unit: cmd/sqlgen/cli: TestGenerate_HeaderNamesTheCLIVersion` | all | commands: generate × success |
| cmd.generate.default-command | `generate` is the default command: running `sqlgen` with no args is equivalent to `sqlgen generate` (it generates and exits 0) | `unit: cmd/sqlgen/cli: TestFullPipeline` | all | commands: bare × success |
| cmd.generate.chains-graphql | When `api.graphql.enabled: true`, `sqlgen generate` also chains into `sqlgen graphql gen` | `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/api+graphql enabled, no opt-out`; `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_MissingGqlgenYml`; `example: graphql: TestWrapperRewriter_PanicStubFlipToDelegation`; `golden: graphql` | all |  |
| cmd.generate.no-graphql-skips-chain | `--no-graphql` on `sqlgen generate` skips the chained `sqlgen graphql gen` | `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/--no-graphql flag honored`; `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_NoGraphQLFlag` | all |  |
| cmd.generate.env-skips-chain | `SQLGEN_NO_GRAPHQL=1` skips the chained `sqlgen graphql gen` | `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/SQLGEN_NO_GRAPHQL=1 honored`; `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_EnvVar` | all |  |

### `sqlgen graphql init`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.graphql-init.success | `sqlgen graphql init` scaffolds a starter `gqlgen.yml` and exits 0 | `unit: cmd/sqlgen/cli: TestGraphQLInit_WritesScaffold/default path`; `unit: cmd/sqlgen/cli: TestGraphQLInit_WritesScaffold/configured path` | all | commands: graphql-init × success |
| cmd.graphql-init.scaffold | The scaffold written is a starter `gqlgen.yml` | `unit: cmd/sqlgen/wrapper: TestInit_GreenfieldScaffold` | all |  |
| cmd.graphql-init.one-shot | One-shot: it only writes when the file is missing; an existing `gqlgen.yml` is not overwritten | `unit: cmd/sqlgen/wrapper: TestInit_NoOverwriteOnExistingFile` | all |  |
| cmd.graphql-init.required-before-generate | Required before the first `sqlgen generate` run when `api.graphql.enabled: true`: without `gqlgen.yml`, generate does not proceed through the chain | `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_MissingGqlgenYml`; `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_PreflightDoesNotScaffold/default path stays absent`; `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_PreflightDoesNotScaffold/mistyped configured path stays absent` | all |  |

### `sqlgen graphql gen`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.graphql-gen.success | `sqlgen graphql gen` runs the gqlgen wrapper as a standalone verb and exits 0 | `example: graphql: TestWrapperLayout_CrossLayoutCompiles/follow-schema`; `example: graphql: TestWrapperLayout_CrossLayoutCompiles/single-file`; `example: graphql: TestWrapperLayered_PreservesConsumerEntries` | all | commands: graphql-gen × success |
| cmd.graphql-gen.split-path | `graphql gen` runs the second phase independently of `generate` (split-command path): a `generate --no-graphql` run followed by `graphql gen` succeeds | `unit: cmd/sqlgen/cli: TestUnusedScalarWarning_firesFromGenerateAndValidateOnly/graphql gen does not warn` | all |  |

### `sqlgen validate`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.validate.success | `sqlgen validate` on a valid config and schema reports success and exits 0 | `unit: cmd/sqlgen/cli: TestValidate_validConfigAndSchema` | all | commands: validate × success |
| cmd.validate.config | `validate` validates the config file | `unit: cmd/sqlgen/cli: TestValidate_invalidConfig` | all |  |
| cmd.validate.schema | `validate` parses the schema | `unit: cmd/sqlgen/cli: TestValidate_invalidSchema` | all |  |
| cmd.validate.no-codegen | `validate` does not generate code: it writes nothing | `unit: cmd/sqlgen/cli: TestValidate_noFilesWritten`; `unit: cmd/sqlgen/cli: TestValidate_validConfigAndSchema` | all |  |
| cmd.validate.resolution-checks | `validate` runs the resolution-level (phase 3, §4.13) checks too, building the generator's contexts in memory and discarding them (no file written) | `unit: cmd/sqlgen/cli: TestValidate_reachesGenerationPhase/renamed onto a reserved name`; `unit: cmd/sqlgen/cli: TestValidate_reachesGenerationPhase/plain column name is reserved`; `unit: cmd/sqlgen/cli: TestValidate_reachesGenerationPhase/duplicate resolved name`; `unit: cmd/sqlgen/cli: TestValidate_ReportsEachGenerationRuleWithTheOthers` | all |  |
| cmd.validate.matches-generate | `validate` answers for what `generate` would accept: a config `generate` rejects is not reported valid | `unit: cmd/sqlgen/cli: TestValidate_reachesGenerationPhase/plain column name is reserved`; `unit: cmd/sqlgen/cli: TestGenerate_InlineTypeNameCollision`; `unit: cmd/sqlgen/gen: TestValidateGeneration_WiresRelationshipFilters` | all |  |
| cmd.validate.reports-errors | `validate` reports errors | `unit: cmd/sqlgen/cli: TestValidate_invalidConfig`; `unit: cmd/sqlgen/cli: TestValidate_UnresolvedForeignKeyKeepsChecking` | all |  |
| cmd.validate.reports-warnings | `validate` reports warnings | `unit: cmd/sqlgen/cli: TestUnmatchedEnumKey_Warns`; `unit: cmd/sqlgen/cli: TestUnusedScalarWarning_firesFromGenerateAndValidateOnly/validate warns` | all |  |

### `sqlgen manifest validate`, `sqlgen manifest diff`, `sqlgen mcp serve`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.manifest-validate.success | `sqlgen manifest validate <path>` validates a `manifest_gen.json` against its JSON Schema and exits 0 on success | `unit: cmd/sqlgen/cli: TestManifestValidate_Valid`; `unit: cmd/sqlgen/cli: TestManifestValidate_OfflineFallback`; `unit: cmd/sqlgen/cli: TestManifestValidate_E2EExamples` | all | commands: manifest-validate × success |
| cmd.manifest-validate.schema-url | The JSON Schema used is the one the manifest's `$schema` URL names | `unit: cmd/sqlgen/cli: TestManifestValidate_FetchUsedWhenReachable` | all |  |
| cmd.manifest-validate.exit-schema-failure | Exit code 1 on schema failure | `unit: cmd/sqlgen/cli: TestManifestValidate_MissingGenerationConfig` | all |  |
| cmd.manifest-validate.exit-io-failure | Exit code 2 on I/O failure | `unit: cmd/sqlgen/cli: TestManifestValidate_MissingFile` | all |  |
| cmd.manifest-validate.always-available | `manifest validate` is always available and reads no `sqlgen.yml`: it validates whatever manifest file it is given | `unit: cmd/sqlgen/cli: TestManifestValidate_ReadsNoConfig` | all |  |
| cmd.manifest-diff.success | `sqlgen manifest diff <old> <new>` exits 0 when the manifests do not differ | `unit: cmd/sqlgen/cli: TestManifestDiff_SameFile` | all | commands: manifest-diff × success |
| cmd.manifest-diff.exit-on-diff | Exit code 1 if there is any diff | `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnAdded`; `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnRemoved` | all |  |
| cmd.manifest-diff.entities | The diff walks entities | `unit: cmd/sqlgen/cli: TestManifestDiff_EntityAddedRemoved` | all |  |
| cmd.manifest-diff.columns | The diff walks columns | `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnAdded`; `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnRemoved`; `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnTypeChanged`; `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnConstraintFlipped`; `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnAccessChanged` | all |  |
| cmd.manifest-diff.methods | The diff walks methods | `unit: cmd/sqlgen/cli: TestManifestDiff_MethodSignatureChanged`; `unit: cmd/sqlgen/cli: TestManifestDiff_MethodErrorSetChanged` | all |  |
| cmd.manifest-diff.sentinels | The diff walks sentinels | `unit: cmd/sqlgen/cli: TestManifestDiff_SentinelChanged` | all |  |
| cmd.manifest-diff.features | The diff walks features | `unit: cmd/sqlgen/cli: TestManifestDiff_FeatureToggleChanged` | all |  |
| cmd.manifest-diff.prints-additions | The diff prints additions | `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnAdded`; `unit: cmd/sqlgen/cli: TestManifestDiff_PerEntityLayout` | all |  |
| cmd.manifest-diff.prints-removals | The diff prints removals | `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnRemoved` | all |  |
| cmd.manifest-diff.prints-changes | The diff prints changes | `unit: cmd/sqlgen/cli: TestManifestDiff_ColumnTypeChanged` | all |  |
| cmd.manifest-diff.json | `--json` gives machine-readable output | `unit: cmd/sqlgen/cli: TestManifestDiff_JSONShape` | all |  |
| cmd.mcp-serve.success | `sqlgen mcp serve` serves the generated manifest to AI agents over MCP | `unit: cmd/sqlgen/mcp/integration: TestStdioLifecycle` | all | commands: mcp-serve × success |
| cmd.mcp-serve.tools | It serves 11 read-only tools | `unit: cmd/sqlgen/mcp: TestDispatchListsAllElevenTools` | all |  |
| cmd.mcp-serve.resources | It serves 4 resources | `unit: cmd/sqlgen/mcp: TestDispatchListsThreeFixedResources`; `unit: cmd/sqlgen/mcp: TestDispatchReadsEntityViaTemplate` | all |  |
| cmd.mcp-serve.prompts | It serves 3 prompts | `unit: cmd/sqlgen/mcp: TestDispatchListsThreePrompts` | all |  |
| cmd.mcp-serve.stdio-default | stdio is the default transport | `unit: cmd/sqlgen/mcp/integration: TestStdioLifecycle` | all |  |
| cmd.mcp-serve.http-loopback | `--http <port>` adds Streamable HTTP, loopback-only | `unit: cmd/sqlgen/mcp: TestResolveHTTPAddr`; `unit: cmd/sqlgen/cli: TestMCPServe_RefusesNonLoopbackHTTP` | all |  |
| cmd.mcp-serve.http-serves | `--http <port>` serves the manifest over Streamable HTTP | `unit: cmd/sqlgen/mcp/integration: TestHTTPLifecycle` | all |  |
| cmd.mcp-serve.watch-default | Manifest watch-reload is on by default | `unit: cmd/sqlgen/mcp/integration: TestStdioWatchReloadNotifiesAndServesNewData` | all |  |

## §23.2 Flags
<!-- prd: 23.2 sha256: afa847beacc6b997 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.flag.config.long | `--config` gives the path to the config file | `unit: cmd/sqlgen/cli: TestConfigDiscovery_explicitPath` | all |  |
| cmd.flag.config.short | `-c` is the short form of `--config` | `unit: cmd/sqlgen/cli: TestConfigFlag_ShortForm` | all |  |
| cmd.flag.config.default | `--config` defaults to `sqlgen.yml` or `sqlgen.yaml` | `unit: cmd/sqlgen/cli: TestConfigDiscovery_findsYml`; `unit: cmd/sqlgen/cli: TestConfigDiscovery_findsYaml` | all |  |
| cmd.flag.verbose.long | `--verbose` turns on verbose output | `unit: cmd/sqlgen/cli: TestVerbose_ReportsTablesFilesAndTiming/long flag` | all |  |
| cmd.flag.verbose.short | `-v` is the short form of `--verbose` | `unit: cmd/sqlgen/cli: TestVerbose_ReportsTablesFilesAndTiming/short flag` | all |  |
| cmd.flag.verbose.default | `--verbose` defaults to `false` | `unit: cmd/sqlgen/cli: TestVerbose_OffByDefault` | all |  |
| cmd.flag.verbose.parsed-tables | Verbose output shows the parsed table, view and enum counts | `unit: cmd/sqlgen/cli: TestVerbose_ReportsTablesFilesAndTiming/long flag`; `unit: cmd/sqlgen/cli: TestVerbose_ReportsTablesFilesAndTiming/short flag` | all |  |
| cmd.flag.verbose.generated-files | Verbose output shows the generated files | `unit: cmd/sqlgen/cli: TestVerbose_ReportsTablesFilesAndTiming/long flag`; `unit: cmd/sqlgen/cli: TestVerbose_ReportsTablesFilesAndTiming/short flag` | all |  |
| cmd.flag.verbose.timing | Verbose output shows timing | `unit: cmd/sqlgen/cli: TestVerbose_ReportsTablesFilesAndTiming/long flag`; `unit: cmd/sqlgen/cli: TestVerbose_ReportsTablesFilesAndTiming/short flag` | all |  |
| cmd.flag.quiet.long | `--quiet` suppresses all output except errors | `unit: cmd/sqlgen/cli: TestPrintGenerateReport_FoldsGraphQLFiles/quiet suppresses everything`; `unit: cmd/sqlgen/cli: TestQuiet_SuppressesSummary/long flag` | all |  |
| cmd.flag.quiet.errors-still-print | Under `--quiet`, errors are still printed | `unit: cmd/sqlgen/cli: TestQuiet_ErrorsStillPrint/config error`; `unit: cmd/sqlgen/cli: TestQuiet_ErrorsStillPrint/schema error`; `unit: cmd/sqlgen/cli: TestQuiet_ErrorsStillPrint/generation error` | all |  |
| cmd.flag.quiet.short | `-q` is the short form of `--quiet` | `unit: cmd/sqlgen/cli: TestQuiet_SuppressesSummary/short flag` | all |  |
| cmd.flag.quiet.default | `--quiet` defaults to `false`: without it, generate prints its summary | `unit: cmd/sqlgen/cli: TestFullPipeline` | all |  |
| cmd.flag.no-graphql.effect | `--no-graphql` skips the chained `sqlgen graphql gen` invocation when `api.graphql.enabled: true` | `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/--no-graphql flag honored`; `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_NoGraphQLFlag` | all |  |
| cmd.flag.no-graphql.default | `--no-graphql` defaults to `false`: without it, the chain runs | `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_MissingGqlgenYml` | all |  |
| cmd.flag.no-graphql.generate-only | `--no-graphql` applies to `sqlgen generate` only | `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/flag validate does not define` | all |  |
| cmd.flag.no-graphql.env | `--no-graphql` is equivalent to `SQLGEN_NO_GRAPHQL=1` | `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/SQLGEN_NO_GRAPHQL=1 honored`; `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_EnvVar` | all |  |
| cmd.flag.version.print | `--version` prints the version | `unit: cmd/sqlgen/cli: TestVersion` | all |  |
| cmd.flag.version.exit | `--version` exits after printing (no command runs) | `unit: cmd/sqlgen/cli: TestVersion` | all |  |
| cmd.flag.quiet.gqlgen-stdout | `--quiet` also suppresses the stdout of the gqlgen subprocess, chained or run by `graphql gen` | `unit: cmd/sqlgen/cli: TestQuiet_SuppressesGqlgenStdout/generate`; `unit: cmd/sqlgen/cli: TestQuiet_SuppressesGqlgenStdout/graphql gen` | all |  |

## §23.3 Config Discovery
<!-- prd: 23.3 sha256: 52ba75cda55ca8fa -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.discovery.explicit | If `--config` is specified, that path is used exactly | `unit: cmd/sqlgen/cli: TestConfigDiscovery_explicitPath` | all |  |
| cmd.discovery.search-yml | Otherwise the current directory is searched for `sqlgen.yml` | `unit: cmd/sqlgen/cli: TestConfigDiscovery_findsYml` | all |  |
| cmd.discovery.search-yaml | Otherwise the current directory is searched for `sqlgen.yaml` | `unit: cmd/sqlgen/cli: TestConfigDiscovery_findsYaml` | all |  |
| cmd.discovery.yml-before-yaml | `sqlgen.yml` is searched before `sqlgen.yaml`: with both present, `sqlgen.yml` is used | `unit: cmd/sqlgen/cli: TestConfigDiscovery_ymlBeforeYaml` | all |  |
| cmd.discovery.missing-hint | If neither exists, an error suggesting `sqlgen init` is printed (bare `sqlgen` exits 2) | `unit: cmd/sqlgen/cli: TestConfigDiscovery_noConfigError` | all | commands: bare × config-error |
| cmd.discovery.missing-hint.graphql-init | `sqlgen graphql init` with no config file prints an error suggesting `sqlgen init` and exits 2 | `unit: cmd/sqlgen/cli: TestGraphQLInit_MissingConfigPrintsInitHint` | all | commands: graphql-init × config-error |

## §23.4 `sqlgen init`
<!-- prd: 23.4 sha256: bde7611b548cc283 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.init.success | `sqlgen init` writes a minimal starter `sqlgen.yml` with sensible defaults into the current directory and exits 0 | `unit: cmd/sqlgen/cli: TestInit_createsDefaultConfig` | all | commands: init × success |
| cmd.init.header-comment | The starter config carries the comment `# Generated by sqlgen init` | `unit: cmd/sqlgen/cli: TestInit_createsDefaultConfig` | all |  |
| cmd.init.default-dialect | The starter config sets `input.dialect: postgres` | `unit: cmd/sqlgen/cli: TestInit_createsDefaultConfig`; `unit: cmd/sqlgen/cli: TestInit_nonInteractiveDefaultsToPostgres` | all |  |
| cmd.init.default-paths | The starter config sets `input.paths` to `["./migrations"]` | `unit: cmd/sqlgen/cli: TestInit_createsDefaultConfig` | all |  |
| cmd.init.default-driver | The starter config sets `output.driver: pgx` | `unit: cmd/sqlgen/cli: TestInit_createsDefaultConfig`; `unit: cmd/sqlgen/cli: TestInit_nonInteractiveDefaultsToPostgres` | all |  |
| cmd.init.default-dir | The starter config sets `output.dir: ./internal/models` | `unit: cmd/sqlgen/cli: TestInit_createsDefaultConfig` | all |  |
| cmd.init.default-package | The starter config sets `output.package: models` | `unit: cmd/sqlgen/cli: TestInit_createsDefaultConfig` | all |  |
| cmd.init.default-layout | The starter config sets `output.layout: file_per_table` | `unit: cmd/sqlgen/cli: TestInit_createsDefaultConfig` | all |  |
| cmd.init.tty-prompts-dialect | When run interactively (TTY detected), `init` prompts for the dialect | `unit: cmd/sqlgen/cli: TestInit_InteractivePromptsForDialect/empty answer takes the default`; `unit: cmd/sqlgen/cli: TestInit_InteractivePromptsForDialect/numbered choice`; `unit: cmd/sqlgen/cli: TestInit_InteractivePromptsForDialect/named choice` | all |  |
| cmd.init.non-tty-postgres | When not run interactively, `init` uses `postgres` without prompting | `unit: cmd/sqlgen/cli: TestInit_NonTTYStdinDoesNotPrompt` | all |  |

## §23.5 `sqlgen completion`
<!-- prd: 23.5 sha256: 198557e174da86dd -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.completion.bash | `sqlgen completion bash` prints a bash completion script and exits 0 | `unit: cmd/sqlgen/cli: TestCompletion_PrintsEachShellsScript/bash` | all | commands: completion × success |
| cmd.completion.zsh | `sqlgen completion zsh` prints a zsh completion script and exits 0 | `unit: cmd/sqlgen/cli: TestCompletion_PrintsEachShellsScript/zsh` | all | commands: completion × success |
| cmd.completion.fish | `sqlgen completion fish` prints a fish completion script and exits 0 | `unit: cmd/sqlgen/cli: TestCompletion_PrintsEachShellsScript/fish` | all | commands: completion × success |
| cmd.completion.powershell | `sqlgen completion powershell` prints a PowerShell completion script and exits 0 | `unit: cmd/sqlgen/cli: TestCompletion_PrintsEachShellsScript/powershell` | all | commands: completion × success |
| cmd.completion.commands | Completions cover all commands | `unit: cmd/sqlgen/cli: TestCompletion_CoversAllCommands/top level`; `unit: cmd/sqlgen/cli: TestCompletion_CoversAllCommands/graphql`; `unit: cmd/sqlgen/cli: TestCompletion_CoversAllCommands/manifest`; `unit: cmd/sqlgen/cli: TestCompletion_CoversAllCommands/mcp` | all |  |
| cmd.completion.flags | Completions cover all flags | `unit: cmd/sqlgen/cli: TestCompletion_CoversAllFlags` | all |  |
| cmd.completion.flag-values | Completions cover flag values | `unit: cmd/sqlgen/cli: TestCompletion_FailOnFlagCompletesSeverities` | all |  |
| cmd.completion.config-files | `--config` completes `.yml` / `.yaml` files | `unit: cmd/sqlgen/cli: TestCompletion_ConfigFlagCompletesYAMLFiles/long flag`; `unit: cmd/sqlgen/cli: TestCompletion_ConfigFlagCompletesYAMLFiles/short flag` | all |  |

## §23.6 `sqlgen diff`
<!-- prd: 23.6 sha256: 19af4617b060b4fe -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.diff.writes-nothing | `diff` previews what `generate` would change without writing anything to the working tree | `unit: cmd/sqlgen/cli: TestDiff_upToDateGraphQLProject_writesNothing`; `unit: cmd/sqlgen/cli: TestDiff_staleSchema_reportsGraphDriftAndWritesNothing`; `unit: cmd/sqlgen/cli: TestDiff_freshProject_createsNothing`; `unit: cmd/sqlgen/cli: TestDiff_nonGraphQLProject_writesNothing`; `unit: cmd/sqlgen/cli: TestDiff_reportsGenuineDeletion`; `unit: cmd/sqlgen/cli: TestDiff_graphPackageUntouched` | all |  |
| cmd.diff.lists-created | The file-level summary lists files `generate` would create, marked `create` | `unit: cmd/sqlgen/cli: TestDiff_newFileDetected`; `unit: cmd/sqlgen/cli: TestDiff_freshProject_createsNothing` | all |  |
| cmd.diff.lists-modified | The file-level summary lists files `generate` would modify, marked `modify` | `unit: cmd/sqlgen/cli: TestDiff_modifiedFileDetected`; `unit: cmd/sqlgen/cli: TestDiff_mixedChanges` | all |  |
| cmd.diff.lists-deleted | The file-level summary lists files `generate` would delete, marked `delete` | `unit: cmd/sqlgen/cli: TestDiff_deletedFileDetected`; `unit: cmd/sqlgen/cli: TestDiff_reportsGenuineDeletion`; `unit: cmd/sqlgen/cli: TestDiff_mixedChanges` | all |  |
| cmd.diff.deletes-only-what-generate-deletes | The previewed deletions are the ones `generate` would perform: a `_gen.go` without sqlgen's header is not reported | `unit: cmd/sqlgen/cli: TestDiff_ForeignGenFileNotReportedDeleted` | all |  |
| cmd.diff.line-format | Each changed file is printed as one line `  <create\|modify\|delete>  <path>` | `unit: cmd/sqlgen/cli: TestDiff_ListingAndSummaryFormat` | all |  |
| cmd.diff.summary-line | The listing ends with `<N> files would change (<c> created, <m> modified, <d> deleted)` | `unit: cmd/sqlgen/cli: TestDiff_ListingAndSummaryFormat` | all |  |
| cmd.diff.summary-omits-zero | A category with no files is left out of the summary line, and the count reads `N files` whatever `N` is (`1 files would change (1 modified)`) | `unit: cmd/sqlgen/cli: TestDiff_ChangesFoundIsAResult/summary leaves out empty categories` | all |  |
| cmd.diff.exit-no-changes | Exit code `0` when there are no changes | `unit: cmd/sqlgen/cli: TestDiff_noChanges`; `unit: cmd/sqlgen/cli: TestDiff_upToDateGraphQLProject_writesNothing`; `unit: cmd/sqlgen/cli: TestDiff_nonGraphQLProject_writesNothing` | all | commands: diff × success |
| cmd.diff.exit-changes | Exit code `1` when changes are detected | `unit: cmd/sqlgen/cli: TestDiff_newFileDetected`; `unit: cmd/sqlgen/cli: TestDiff_staleSchema_reportsGraphDriftAndWritesNothing`; `unit: cmd/sqlgen/cli: TestDiff_freshProject_createsNothing`; `unit: cmd/sqlgen/cli: TestDiff_reportsGenuineDeletion` | all | commands: diff × success |
| cmd.diff.coverage-every-file | Coverage is every file `generate` writes, not just `output.dir` | `unit: cmd/sqlgen/cli: TestDiff_staleSchema_reportsGraphDriftAndWritesNothing` | all |  |
| cmd.diff.coverage-graphql-schema | With `api.graphql.enabled: true`, the GraphQL schema files under `schema_dir` are compared | `unit: cmd/sqlgen/cli: TestDiff_staleSchema_reportsGraphDriftAndWritesNothing` | all |  |
| cmd.diff.coverage-graphql-scalar | With `api.graphql.enabled: true`, the GraphQL scalar file under `resolver_dir` is compared | `unit: cmd/sqlgen/cli: TestDiff_CoversGraphQLResolverDirFiles/scalar file` | all |  |
| cmd.diff.coverage-graphql-translator | With `api.graphql.enabled: true`, the GraphQL translator files under `resolver_dir` are compared | `unit: cmd/sqlgen/cli: TestDiff_CoversGraphQLResolverDirFiles/translator file` | all |  |
| cmd.diff.coverage-graphql-resolver | With `api.graphql.enabled: true`, the GraphQL resolver files under `resolver_dir` are compared | `unit: cmd/sqlgen/cli: TestDiff_CoversGraphQLResolverDirFiles/resolver file` | all |  |
| cmd.diff.manifest-excluded | The manifest stage (§30.3) is excluded: its JSON and markdown artifacts are not compared | `unit: cmd/sqlgen/cli: TestDiff_ManifestArtifactsNotCompared` | all |  |
| cmd.diff.manifest-embed-not-deleted | `manifest_embed_gen.go` is accounted for and never reported as a deletion | `unit: cmd/sqlgen/cli: TestDiff_manifestEmbedNotReportedAsDeleted` | all |  |
| cmd.diff.redirect-every-destination | The preview generates into a temp directory with every write destination redirected under it | `unit: cmd/sqlgen/cli: TestGenerateInto_redirectsEveryWrite_andKeepsImportsConfigured`; `unit: cmd/sqlgen/gen: TestSandboxPath/relative dir mirrors its shape under the root`; `unit: cmd/sqlgen/gen: TestSandboxPath/absolute dir is anchored under the root, not written through`; `unit: cmd/sqlgen/gen: TestSandboxPath/parent climb cannot escape the root`; `unit: cmd/sqlgen/gen: TestSandboxPath/repeated parent climbs cannot escape the root`; `unit: cmd/sqlgen/gen: TestSandboxPathNeverEscapes` | all |  |
| cmd.diff.redirect-graph-dirs | The redirect includes the root-relative GraphQL directories (§26.5.8) | `unit: cmd/sqlgen/cli: TestGenerateInto_redirectsEveryWrite_andKeepsImportsConfigured`; `unit: cmd/sqlgen/cli: TestDiff_graphPackageUntouched` | all |  |
| cmd.diff.redirect-keeps-configured-paths | Redirection moves destinations only: import paths and file contents are computed from the configured directories | `unit: cmd/sqlgen/cli: TestGenerateInto_redirectsEveryWrite_andKeepsImportsConfigured` | all |  |
| cmd.diff.byte-identical | The preview is byte-identical to what a real run would write | `unit: cmd/sqlgen/cli: TestDiff_upToDateGraphQLProject_writesNothing`; `unit: cmd/sqlgen/cli: TestDiff_noChanges` | all |  |
| cmd.diff.orphan-fails | An orphaned API schema file (§23.8) fails the preview the way it fails `generate`: same error, exit `1` | `unit: cmd/sqlgen/cli: TestDiff_orphanedAPISchema_failsLikeGenerate` | all | commands: diff × generation-error |
| cmd.diff.orphan-configured-dir | The orphan is looked for in the configured `schema_dir`, not the redirect | `unit: cmd/sqlgen/cli: TestDiff_orphanedAPISchema_failsLikeGenerate` | all |  |
| cmd.diff.orphan-no-preview | With an orphaned API schema file, `diff` reports the error instead of previewing changes (a deletion included) | `unit: cmd/sqlgen/cli: TestDiff_OrphanedAPISchemaPreviewsNothing` | all |  |
| cmd.diff.gqlgen-refusal-outside | A refusal from the chained gqlgen step, which `diff` does not run, is outside the preview | `unit: cmd/sqlgen/cli: TestDiff_upToDateGraphQLProject_writesNothing` | all |  |
| cmd.diff.manifest-refusal-outside | A refusal from the manifest stage, which `diff` does not run, is outside the preview | `unit: cmd/sqlgen/cli: TestDiff_ManifestStageFailureOutsidePreview/manifest artifacts cannot be written`; `unit: cmd/sqlgen/cli: TestDiff_ManifestStageFailureOutsidePreview/invalid project MCP config refused` | all |  |

## §23.7 `sqlgen lint`
<!-- prd: 23.7 sha256: e97d20d8375c2909 -->

### What it checks

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.lint.mismatch.for-mutation | A `ForMutation` call whose generic type parameters belong to a different table than its `TableName` constant is reported (e.g. `ForMutation[*CreateProductInput, *Product](TableUsers, ...)`) | `unit: cmd/sqlgen/cli: TestLint_tableTypeMismatch`; `unit: cmd/sqlgen/cli: TestLint_crossSchemaTypeMismatch` | all |  |
| cmd.lint.mismatch.for-query | A `ForQuery` call whose generic type parameters belong to a different table than its `TableName` constant is reported | `unit: cmd/sqlgen/cli: TestLint_OutputFormat/a ForQuery call whose types belong to another table is found and reported` | all |  |
| cmd.lint.mismatch.severity | A hook table/type mismatch is reported at severity Error | `unit: cmd/sqlgen/cli: TestLint_tableTypeMismatch`; `unit: cmd/sqlgen/cli: TestLint_crossSchemaTypeMismatch` | all |  |
| cmd.lint.mismatch.per-argument | A call whose two type arguments both belong to another table gives two findings, each naming its type | `unit: cmd/sqlgen/cli: TestLint_TablesThatGenerateNothing/the hook is flagged while the table is generated` | all |  |
| cmd.lint.mismatch.matching-silent | A hook whose type parameters are that table's own generated types draws no mismatch | `unit: cmd/sqlgen/cli: TestLint_sameSchemaTypeMatchIsSilent`; `unit: cmd/sqlgen/cli: TestLint_cleanCode` | all |  |
| cmd.lint.invalid-op.unsupported | A hook that filters on an operation the table does not support (its method is not generated) is reported, since the hook would never fire | `unit: cmd/sqlgen/cli: TestLint_upsertHookOnTableWithoutConflictTarget` | all |  |
| cmd.lint.invalid-op.soft-delete | Filtering on `OpSoftDelete` for a table with no soft delete column is reported | `unit: cmd/sqlgen/cli: TestLint_invalidOperationForTable`; `unit: cmd/sqlgen/cli: TestLint_shapeReasonWinsOverOperationsReason` | all |  |
| cmd.lint.invalid-op.increment | Filtering on `OpIncrement` for a table with no incrementable columns is reported | `unit: cmd/sqlgen/cli: TestLint_incrementHookOnFKOnlyTable` | all |  |
| cmd.lint.invalid-op.severity | An invalid operation for a table is reported at severity Warning | `unit: cmd/sqlgen/cli: TestLint_shapeReasonWinsOverOperationsReason`; `unit: cmd/sqlgen/cli: TestLint_upsertHookOnTableWithoutConflictTarget` | all |  |
| cmd.lint.incrementable.numeric | "Incrementable" means numeric (§8.2): a plain numeric column is incrementable, a non-numeric one is not | `unit: cmd/sqlgen/cli: TestLint_IncrementEligibility/a table with no numeric column draws the OpIncrement warning`; `unit: cmd/sqlgen/cli: TestLint_IncrementEligibility/a plain numeric column makes the table incrementable` | all |  |
| cmd.lint.incrementable.not-pk | A primary key column is not incrementable: a table whose only numeric column is its primary key generates no `Increment`, and an `OpIncrement` hook on it is reported | `unit: cmd/sqlgen/cli: TestLint_IncrementEligibility/a table whose only numeric column is its primary key draws the OpIncrement warning` | all |  |
| cmd.lint.incrementable.not-fk | A foreign key column (schema-declared or config-declared) is not incrementable | `unit: cmd/sqlgen/cli: TestBuildTypeRegistry_IncrementMatchesGenerator/integer foreign keys are not incrementable`; `unit: cmd/sqlgen/cli: TestBuildTypeRegistry_IncrementMatchesGenerator/config-declared foreign keys are not incrementable`; `unit: cmd/sqlgen/cli: TestLint_incrementHookOnFKOnlyTable` | all |  |
| cmd.lint.incrementable.not-tenant | The tenant column is not incrementable | `unit: cmd/sqlgen/cli: TestBuildTypeRegistry_IncrementMatchesGenerator/tenant column is not incrementable` | all |  |
| cmd.lint.incrementable.not-generated | A generated column is not incrementable: a table whose only numeric column is generated draws the `OpIncrement` warning | `unit: cmd/sqlgen/cli: TestLint_IncrementEligibility/a table whose only numeric column is generated draws the OpIncrement warning` | all |  |

### How it works

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.lint.loads-config-and-schema | Step 1: lint loads `sqlgen.yml` and parses the schema the same way `generate` does (same errors, same exit codes) | `unit: cmd/sqlgen/cli: TestParseSchema_UnresolvedForeignKeysPrefixEachLine`; `unit: cmd/sqlgen/cli: TestCommands_ParseExitCode`; `unit: cmd/sqlgen/cli: TestSoftDeleteTypeMismatchStopsEveryContextBuilder` | all |  |
| cmd.lint.registry-from-contexts | Step 2: the type registry is derived from the built table contexts, mapping each `TableName` constant to the input/result types generation emits (struct_name and acronym spelling included) | `unit: cmd/sqlgen/cli: TestBuildTypeRegistry_MatchesGeneratorNaming`; `unit: cmd/sqlgen/cli: TestBuildTypeRegistry_FollowsStructName/the table constant follows the struct_name`; `unit: cmd/sqlgen/cli: TestBuildTypeRegistry_FollowsStructName/the struct type follows the struct_name`; `unit: cmd/sqlgen/cli: TestBuildTypeRegistry_FollowsStructName/no name is spelled from the SQL table name` | all |  |
| cmd.lint.registry-surfaces | Step 2: the registry also carries the operations, soft-delete and increment surfaces the table actually generates | `unit: cmd/sqlgen/cli: TestNewTestRegistry_FixtureFacts`; `unit: cmd/sqlgen/cli: TestOpEnabledForTable_ReadsResolvedOperations` | all |  |
| cmd.lint.finds-for-mutation | Step 3: `go/ast` finds `hook.ForMutation[...]` call expressions in consumer `.go` files | `unit: cmd/sqlgen/cli: TestLint_tableTypeMismatch` | all |  |
| cmd.lint.finds-for-query | Step 3: `go/ast` finds `hook.ForQuery[...]` call expressions in consumer `.go` files | `unit: cmd/sqlgen/cli: TestLint_OutputFormat/a ForQuery call whose types belong to another table is found and reported` | all |  |
| cmd.lint.extracts-call-parts | Step 4: the generic type parameters and the table name argument are extracted from each call (the finding names both) | `unit: cmd/sqlgen/cli: TestLint_OutputFormat/the finding rebuilds the call from its extracted type arguments and table constant` | all |  |
| cmd.lint.facts-from-contexts | Every fact a rule tests is read off a built table context: the input and result types and the increment surface match what the generator produces | `unit: cmd/sqlgen/cli: TestBuildTypeRegistry_IncrementMatchesGenerator` | all |  |
| cmd.lint.absent.exclude-tables | A table removed by `exclude_tables` is absent from the registry, and hooks naming it produce no findings | `unit: cmd/sqlgen/cli: TestLint_TablesThatGenerateNothing/a table removed by exclude_tables produces no findings` | all |  |
| cmd.lint.absent.no-pk | A table with no resolvable primary key (§9.4b) is absent from the registry, and hooks naming it produce no findings | `unit: cmd/sqlgen/cli: TestLint_TablesThatGenerateNothing/a table with no resolvable primary key produces no findings` | all |  |
| cmd.lint.generation-error.fails | A generation-phase error fails the lint run (no clean result for a config that cannot produce code) | `unit: cmd/sqlgen/cli: TestExitCode_ResolutionErrorIsGenerationFailure/lint`; `unit: cmd/sqlgen/cli: TestBuildTypeRegistry_ReportsGenerationErrors` | all |  |
| cmd.lint.generation-error.exit-1 | A generation-phase error makes `sqlgen lint` exit `1` | `unit: cmd/sqlgen/cli: TestExitCode_ResolutionErrorIsGenerationFailure/lint` | all | commands: lint × generation-error |

### Usage, flags and exit code

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.lint.paths.default-output-dir | With no `--paths`, lint scans `output.dir` from config | `unit: cmd/sqlgen/cli: TestLint_ScanDirectories/with no --paths lint scans output.dir` | all |  |
| cmd.lint.paths.scans-each | `--paths` scans every listed directory for hook registrations | `unit: cmd/sqlgen/cli: TestLint_ScanDirectories/a comma-separated --paths list scans every directory it names` | all |  |
| cmd.lint.paths.comma-separated | `--paths` takes a comma-separated list | `unit: cmd/sqlgen/cli: TestLint_ScanDirectories/a comma-separated --paths list scans every directory it names` | all |  |
| cmd.lint.fail-on.default-error | `--fail-on` defaults to `error`: an error-severity issue gives a non-zero exit | `unit: cmd/sqlgen/cli: TestLint_FailOnThreshold/the default threshold is error, so an error exits 1` | all |  |
| cmd.lint.fail-on.default-warnings-pass | Under the default `--fail-on error`, warnings/info only exit `0` | `unit: cmd/sqlgen/cli: TestLint_FailOnThreshold/under the default threshold a warning alone exits 0` | all |  |
| cmd.lint.fail-on.warning | `--fail-on warning` gives a non-zero exit when a warning exists | `unit: cmd/sqlgen/cli: TestLint_FailOnThreshold/--fail-on warning exits 1 when a warning exists`; `unit: cmd/sqlgen/cli: TestLint_FailOnThreshold/--fail-on warning exits 1 on an error, which is above it` | all |  |
| cmd.lint.fail-on.info | `--fail-on info` gives a non-zero exit for any issue at or above info (a warning included) | `unit: cmd/sqlgen/cli: TestLint_FailOnThreshold/--fail-on info exits 1 on a warning, which is above it` | all |  |
| cmd.lint.exit.success | Exit code `0` when no issues are at or above `--fail-on` severity | `unit: cmd/sqlgen/cli: TestLint_cleanCode` | all | commands: lint × success |
| cmd.lint.exit.issues | Exit code `1` when any issue is at or above `--fail-on` severity | `unit: cmd/sqlgen/cli: TestLint_OutputFormat/one error and one warning print severity, file:line, the rebuilt call, the explanation and the summary`; `unit: cmd/sqlgen/cli: TestLint_FailOnThreshold/the default threshold is error, so an error exits 1`; `unit: cmd/sqlgen/cli: TestLint_FailOnThreshold/--fail-on warning exits 1 when a warning exists` | all |  |

### Output

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.lint.output.severity | Each finding starts with its severity | `unit: cmd/sqlgen/cli: TestLint_OutputFormat/one error and one warning print severity, file:line, the rebuilt call, the explanation and the summary` | all |  |
| cmd.lint.output.file-line | Each finding names `file:line` | `unit: cmd/sqlgen/cli: TestLint_OutputFormat/one error and one warning print severity, file:line, the rebuilt call, the explanation and the summary` | all |  |
| cmd.lint.output.snippet | Each finding shows the hook call rebuilt from the source: the function, its type arguments and the table constant (`ForMutation[*CreateProductInput, *Product](TableUsers, ...)`) | `unit: cmd/sqlgen/cli: TestLint_OutputFormat/one error and one warning print severity, file:line, the rebuilt call, the explanation and the summary`; `unit: cmd/sqlgen/cli: TestLint_SnippetKeepsSliceTypeArgument`; `unit: cmd/sqlgen/cli: TestLint_TypeArgumentSpelling` | all |  |
| cmd.lint.output.message | Each finding carries an explanation line | `unit: cmd/sqlgen/cli: TestLint_OutputFormat/one error and one warning print severity, file:line, the rebuilt call, the explanation and the summary` | all |  |
| cmd.lint.output.mismatch-message | The mismatch explanation reads `type mismatch: <Type> belongs to <TableConst>, not <TableConst>`, one finding per mismatching type argument | `unit: cmd/sqlgen/cli: TestLint_OutputFormat/one error and one warning print severity, file:line, the rebuilt call, the explanation and the summary` | all |  |
| cmd.lint.output.soft-delete-message | The soft-delete explanation says the table has no soft delete column and the hook will never fire for the operation it filters on (`… — hook will never fire for OpSoftDelete`) | `unit: cmd/sqlgen/cli: TestLint_OutputFormat/one error and one warning print severity, file:line, the rebuilt call, the explanation and the summary` | all |  |
| cmd.lint.output.summary-line | Output ends with a summary line `N issues (…)` | `unit: cmd/sqlgen/cli: TestLint_OutputFormat/one error and one warning print severity, file:line, the rebuilt call, the explanation and the summary` | all |  |
| cmd.lint.output.summary-counts | The summary line counts issues by severity (`N issues (X error, Y warning)`) | `unit: cmd/sqlgen/cli: TestLint_OutputFormat/one error and one warning print severity, file:line, the rebuilt call, the explanation and the summary` | all |  |

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.lint.paths.replaces-default | `--paths` replaces the default `output.dir` rather than adding to it | `unit: cmd/sqlgen/cli: TestLint_ScanDirectories/--paths replaces output.dir rather than adding to it` | all |  |
| cmd.lint.output.issues-count-word | The summary line reads `N issues (…)` whatever `N` is, `1 issues` included | `unit: cmd/sqlgen/cli: TestLint_OutputFormat/a single soft-delete warning names OpSoftDelete and is counted as 1 issues` | all |  |
| cmd.lint.output.warning-names-op | A warning's explanation ends by naming the operation the hook filters on, for every invalid-operation case (`… for OpUpsert` included) | `unit: cmd/sqlgen/cli: TestLint_OutputFormat/a single soft-delete warning names OpSoftDelete and is counted as 1 issues`; `unit: cmd/sqlgen/cli: TestLint_OutputFormat/a single increment warning names OpIncrement`; `unit: cmd/sqlgen/cli: TestLint_OutputFormat/a single upsert warning names OpUpsert` | all |  |

## §23.8 Stale File Cleanup
<!-- prd: 23.8 sha256: 60b91b4bc4b805d6 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.stale.file-per-table-only | The stale sweep runs only under `layout: file_per_table` | `unit: cmd/sqlgen/cli: TestStaleSweep_RunsOnlyUnderFilePerTable/single_file keeps a sqlgen orphan`; `unit: cmd/sqlgen/cli: TestStaleSweep_RunsOnlyUnderFilePerTable/file_per_table deletes the same orphan`; `unit: cmd/sqlgen/cli: TestStaleCleanup_noCleanupInSingleFileMode` | all |  |
| cmd.stale.deletes-orphans | `sqlgen generate` deletes orphaned generated files that no longer correspond to a table (a dropped table's file is removed) | `unit: cmd/sqlgen/cli: TestStaleCleanup_orphanedFileDeleted`; `unit: cmd/sqlgen/cli: TestGenerate_ForeignGenFileSurvivesSweep`; `unit: cmd/sqlgen/gen: TestStaleFiles_RequiresSqlgenProvenance/sqlgen output is swept`; `unit: cmd/sqlgen/gen: TestStaleFiles_SameNameWrittenElsewhereDoesNotProtect` | all |  |
| cmd.stale.written-kept | A file this run produced is never stale | `unit: cmd/sqlgen/gen: TestStaleFiles_WrittenFilesAreNeverStale`; `unit: cmd/sqlgen/gen: TestStaleFiles_SameNameWrittenElsewhereDoesNotProtect` | all |  |
| cmd.stale.output-dir-only | Only files within the output directory are considered | `unit: cmd/sqlgen/cli: TestStaleSweep_OnlyConsidersTheOutputDir/orphan in a sibling directory survives`; `unit: cmd/sqlgen/cli: TestStaleSweep_OnlyConsidersTheOutputDir/orphan in the parent of output.dir survives`; `unit: cmd/sqlgen/cli: TestStaleSweep_OnlyConsidersTheOutputDir/orphan inside output.dir is deleted` | all |  |
| cmd.stale.non-gen-untouched | Files that do not end in `_gen.go` are never touched, whatever header they carry | `unit: cmd/sqlgen/gen: TestStaleFiles_NonGenSuffixUntouched` | all |  |
| cmd.stale.header-required | A `_gen.go` candidate is deleted only when it carries sqlgen's own header (`// Code generated by sqlgen <version>. DO NOT EDIT.`) | `unit: cmd/sqlgen/gen: TestStaleFiles_RequiresSqlgenProvenance/sqlgen output is swept`; `unit: cmd/sqlgen/gen: TestStaleFiles_RequiresSqlgenProvenance/unmarked file is left alone`; `unit: cmd/sqlgen/gen: TestHeader_CarriesTheProvenancePrefix` | all |  |
| cmd.stale.header-above-package | The header counts only above the package clause | `unit: cmd/sqlgen/cli: TestStaleSweep_HeaderCountsOnlyAbovePackageClause/header below the package clause survives`; `unit: cmd/sqlgen/cli: TestStaleSweep_HeaderCountsOnlyAbovePackageClause/header above the package clause is deleted` | all |  |
| cmd.stale.other-generators-kept | Another generator's `_gen.go` (`wire_gen.go`, `mock_gen.go`, generic `// Code generated ... DO NOT EDIT.`) is kept | `unit: cmd/sqlgen/gen: TestStaleFiles_RequiresSqlgenProvenance/wire output is left alone`; `unit: cmd/sqlgen/gen: TestStaleFiles_RequiresSqlgenProvenance/mockgen output is left alone`; `unit: cmd/sqlgen/cli: TestGenerate_ForeignGenFileSurvivesSweep` | all |  |
| cmd.stale.fails-closed | A candidate that cannot be read is left alone | `unit: cmd/sqlgen/cli: TestStaleSweep_UnreadableCandidateIsLeftAlone` | all |  |
| cmd.stale.header-removed-disowns | Removing the header from a sqlgen-authored file hands it to the consumer; sqlgen no longer sweeps it | `unit: cmd/sqlgen/gen: TestStaleFiles_RequiresSqlgenProvenance/header stripped from a prior sqlgen file disowns it` | all |  |
| cmd.stale.exception.manifest | Manifest artifacts are a governed exception to the `_gen.go`-only rule, with removal semantics defined in §30.3 | `unit: cmd/sqlgen/cli: TestGenerate_ReportsManifestDeletions` | all |  |
| cmd.stale.exception.mcp-json | The project-local `.mcp.json` registration is a governed exception, with removal semantics defined in §31.2 | `unit: cmd/sqlgen/cli: TestEmitMCPProjectConfigsOptOutRemoves` | all |  |

### GraphQL schema orphans

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.stale.gql-orphan.checked | When `api.graphql.enabled: true`, generate checks `schema_dir` for `*_gen.graphqls` files this run will not write; files it writes, non-`_gen` schema files and directories are not orphans | `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/files this run will write are not orphans`; `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/consumer schema files without the _gen suffix are left alone`; `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/a directory is not a schema file`; `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/orphans under follow-schema layout name their resolver files` | all |  |
| cmd.stale.gql-orphan.any-layout | The orphan check runs in any `output.layout` | `unit: cmd/sqlgen/cli: TestGenerate_GraphQLSchemaOrphanRefused/dropped table under single_file`; `unit: cmd/sqlgen/cli: TestGenerate_GraphQLSchemaOrphanRefused/dropped table under file_per_table` | all |  |
| cmd.stale.gql-orphan.dropped-table | A table dropped from the schema leaves an orphan the check reports | `unit: cmd/sqlgen/cli: TestDiff_orphanedAPISchema_failsLikeGenerate` | all |  |
| cmd.stale.gql-orphan.api-disabled | An entity taken off the API with `api.enabled: false` (§26.10) leaves an orphan the check reports | `unit: cmd/sqlgen/cli: TestGenerate_GraphQLSchemaOrphanRefused/table taken off the API with api.enabled false` | all |  |
| cmd.stale.gql-orphan.new-stem | A new file stem (`struct_name`) leaves an orphan the check reports | `unit: cmd/sqlgen/cli: TestGenerate_GraphQLSchemaOrphanRefused/table given a new file stem with struct_name` | all |  |
| cmd.stale.gql-orphan.exit-1 | An orphan makes generation refuse with exit `1` | `unit: cmd/sqlgen/cli: TestDiff_orphanedAPISchema_failsLikeGenerate` | all |  |
| cmd.stale.gql-orphan.before-writing | The refusal comes before anything is written; the working tree is unchanged | `unit: cmd/sqlgen/cli: TestDiff_orphanedAPISchema_failsLikeGenerate` | all |  |
| cmd.stale.gql-orphan.names-each | The error names each orphan | `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/orphans under follow-schema layout name their resolver files` | all |  |
| cmd.stale.gql-orphan.names-resolver | When `<stem>_gen.resolvers.go` exists in `resolver_dir`, the error names it too | `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/orphans under follow-schema layout name their resolver files`; `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/the resolver file is looked up in the resolver dir`; `unit: cmd/sqlgen/cli: TestDiff_orphanedAPISchema_failsLikeGenerate` | all |  |
| cmd.stale.gql-orphan.no-resolver | When no such resolver file exists, the error names only the schema file | `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/an orphan with no resolver file names only the schema file` | all |  |
| cmd.stale.gql-orphan.deletes-neither | Neither the schema file nor the resolver file is deleted | `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/orphans under follow-schema layout name their resolver files`; `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/the resolver file is looked up in the resolver dir`; `unit: cmd/sqlgen/cli: TestDiff_orphanedAPISchema_failsLikeGenerate` | all |  |
| cmd.stale.gql-orphan.recovery | Deleting what the error names is the whole recovery: the next run succeeds | `unit: cmd/sqlgen/cli: TestDiff_orphanedAPISchema_failsLikeGenerate` | all |  |
| cmd.stale.gql-orphan.no-graphql | The check runs with `--no-graphql` too | `unit: cmd/sqlgen/cli: TestDiff_orphanedAPISchema_failsLikeGenerate` | all |  |
| cmd.stale.gql-orphan.suffix-is-ownership | Every `*_gen.graphqls` in `schema_dir` counts as sqlgen's, with no header needed | `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/an orphan with no resolver file names only the schema file` | all |  |

## §23.9 Exit Codes
<!-- prd: 23.9 sha256: 19b35eb52d5b42a4 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.exit.generate.config | `sqlgen generate` exits 2 on a config error (invalid config) | `unit: cmd/sqlgen/cli: TestSoftDeleteTypeMismatchStopsEveryContextBuilder`; `unit: cmd/sqlgen/cli: TestGenerate_notNullTimestampSoftDeleteRejected` | all | commands: generate × config-error |
| cmd.exit.generate.schema | `sqlgen generate` exits 3 on a schema parse failure | `unit: cmd/sqlgen/cli: TestCommands_ParseExitCode`; `unit: cmd/sqlgen/cli: TestParseSchema_UnresolvedForeignKey`; `unit: cmd/sqlgen/cli: TestParseSchema_UnresolvedForeignKeysPrefixEachLine` | all | commands: generate × schema-error |
| cmd.exit.generate.generation | `sqlgen generate` exits 1 on a generation-phase failure | `unit: cmd/sqlgen/cli: TestGenerate_InlineTypeNameCollision`; `unit: cmd/sqlgen/cli: TestDiff_orphanedAPISchema_failsLikeGenerate` | all | commands: generate × generation-error |
| cmd.exit.bare.schema | Bare `sqlgen` exits 3 on a schema parse failure | `unit: cmd/sqlgen/cli: TestExit_BareSchemaError` | all | commands: bare × schema-error |
| cmd.exit.bare.generation | Bare `sqlgen` exits 1 on a generation-phase failure | `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_UnboundRootFieldOwnError/exits 1` | all | commands: bare × generation-error |
| cmd.exit.graphql-init.generation | `sqlgen graphql init` exits 1 when the scaffold cannot be written | `unit: cmd/sqlgen/cli: TestGraphQLInit_UnwritableTargetExitsGeneration` | all | commands: graphql-init × generation-error |
| cmd.exit.graphql-gen.config | `sqlgen graphql gen` exits 2 on a config error | `unit: cmd/sqlgen/cli: TestSoftDeleteTypeMismatchStopsEveryContextBuilder` | all | commands: graphql-gen × config-error |
| cmd.exit.graphql-gen.schema | `sqlgen graphql gen` exits 3 on a schema parse failure | `unit: cmd/sqlgen/cli: TestCommands_ParseExitCode` | all | commands: graphql-gen × schema-error |
| cmd.exit.graphql-gen.generation | `sqlgen graphql gen` exits 1 on a generation-phase (wrapper/gqlgen) failure | `unit: cmd/sqlgen/cli: TestExit_GraphQLGenSubprocessFailure` | all | commands: graphql-gen × generation-error |
| cmd.exit.validate.config | `sqlgen validate` exits 2 on a config error | `unit: cmd/sqlgen/cli: TestValidate_invalidConfig`; `unit: cmd/sqlgen/cli: TestValidate_ConnectionFieldsMissing`; `unit: cmd/sqlgen/cli: TestValidate_softDeleteTypeMismatchReportedOnce` | all | commands: validate × config-error |
| cmd.exit.validate.schema | `sqlgen validate` exits 3 on a schema parse failure | `unit: cmd/sqlgen/cli: TestValidate_invalidSchema`; `unit: cmd/sqlgen/cli: TestValidate_UnresolvedForeignKeyKeepsChecking`; `unit: cmd/sqlgen/cli: TestCommands_ParseExitCode` | all | commands: validate × schema-error |
| cmd.exit.validate.generation | `sqlgen validate` exits 1 on a resolution-level (phase 3) failure, as `generate` and `lint` do; it writes nothing, so it has no template or write failure | `unit: cmd/sqlgen/cli: TestExitCode_ResolutionErrorIsGenerationFailure/validate` | all | commands: validate × generation-error |
| cmd.exit.init-write-error | `sqlgen init` failing to write `sqlgen.yml` is a file write error: exit `1` | `unit: cmd/sqlgen/cli: TestInit_WriteErrorExitsGeneration` | all | commands: init × generation-error |
| cmd.exit.diff-config-error | `sqlgen diff` on an invalid config (validation failure) exits `2` | `unit: cmd/sqlgen/cli: TestSoftDeleteTypeMismatchStopsEveryContextBuilder` | all | commands: diff × config-error |
| cmd.exit.diff-schema-error | `sqlgen diff` on a schema error exits `3` | `unit: cmd/sqlgen/cli: TestParseSchema_UnresolvedForeignKeysPrefixEachLine`; `unit: cmd/sqlgen/cli: TestParseSchema_UnresolvedForeignKey`; `unit: cmd/sqlgen/cli: TestCommands_ParseExitCode` | all | commands: diff × schema-error |
| cmd.exit.0-success | Exit `0` on success | `unit: cmd/sqlgen/cli: TestValidate_validConfigAndSchema`; `unit: cmd/sqlgen/cli: TestLint_cleanCode` | all |  |
| cmd.exit.1-generation | Exit `1` on a generation error | `unit: cmd/sqlgen/cli: TestGenerate_InlineTypeNameCollision`; `unit: cmd/sqlgen/cli: TestDiff_orphanedAPISchema_failsLikeGenerate` | all |  |
| cmd.exit.1-template-failure | A template failure exits `1` |  | all |  |
| cmd.exit.1-file-write-error | A file write error exits `1` | `unit: cmd/sqlgen/cli: TestExit_FileWriteErrorIsGenerationError` | all |  |
| cmd.exit.2-invalid-yaml | Invalid YAML in the config exits `2` | `unit: cmd/sqlgen/cli: TestExit_InvalidYAMLIsConfigError` | all |  |
| cmd.exit.2-validation-failure | A config validation failure exits `2` | `unit: cmd/sqlgen/cli: TestValidate_invalidConfig` | all |  |
| cmd.exit.3-files | A schema error exits `3` under `input.source: files` | `unit: cmd/sqlgen/cli: TestValidate_invalidSchema`; `unit: cmd/sqlgen/cli: TestParseSchema_UnresolvedForeignKey` | all |  |
| cmd.exit.3-database | A schema error exits `3` under `input.source: database` | `unit: cmd/sqlgen/cli: TestCommands_ParseExitCode` | all |  |
| cmd.exit.3-both | A schema error exits `3` under `input.source: both` | `unit: cmd/sqlgen/cli: TestCommands_ParseExitCode`; `integration: cmd/sqlgen/cli: TestGenerate_ConnectionFailureExitCode`; `integration: cmd/sqlgen/cli: TestGenerate_BothSourceForeignKeys`; `integration: cmd/sqlgen/cli: TestGenerate_BothSourceAppliesOverlay` | all |  |
| cmd.exit.3-table-defined-twice | A table defined twice in the schema exits `3` | `unit: cmd/sqlgen/cli: TestExit_TableDefinedTwiceIsSchemaError` | all |  |
| cmd.exit.2-ambiguous-config-key | An ambiguous bare config key (a table name in more than one schema) is a config error, exit `2`, not a schema error | `unit: cmd/sqlgen/cli: TestExit_AmbiguousConfigKeyIsConfigError` | all |  |
| cmd.exit.4-connect | Failure to connect to the database exits `4` | `unit: cmd/sqlgen/cli: TestCommands_ParseExitCode`; `unit: cmd/sqlgen/cli: TestParseExitCode/connection failure`; `unit: cmd/sqlgen/cli: TestParseExitCode/wrapped connection failure`; `integration: cmd/sqlgen/cli: TestGenerate_ConnectionFailureExitCode` | all |  |
| cmd.exit.4-catalog-query | A failed catalog query during introspection exits `4` | `unit: cmd/sqlgen/cli: TestCommands_ParseExitCode` | all |  |
| cmd.exit.4-nothing-else | Nothing else exits `4`: a schema error found while introspecting, or an unresolved foreign key, exits `3` | `unit: cmd/sqlgen/cli: TestParseExitCode/unresolved foreign key`; `unit: cmd/sqlgen/cli: TestCommands_ParseExitCode` | all |  |
| cmd.lint.exit.config-error | `sqlgen lint` exits `2` on a config error | `unit: cmd/sqlgen/cli: TestSoftDeleteTypeMismatchStopsEveryContextBuilder` | all | commands: lint × config-error |
| cmd.lint.exit.schema-error | `sqlgen lint` exits `3` on a schema error | `unit: cmd/sqlgen/cli: TestParseSchema_UnresolvedForeignKeysPrefixEachLine`; `unit: cmd/sqlgen/cli: TestCommands_ParseExitCode` | all | commands: lint × schema-error |
| cmd.lint.exit.connection-error | `sqlgen lint` exits `4` when the database cannot be reached | `unit: cmd/sqlgen/cli: TestCommands_ParseExitCode` | all |  |
| cmd.exit.usage.unknown-command | An unknown command exits `2` | `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/unknown command`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/unknown subcommand of graphql`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/unknown subcommand of manifest`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/unknown subcommand of mcp` | all |  |
| cmd.exit.usage.unknown-flag | An unknown or malformed flag (`generate --watch`) exits `2` | `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/unknown flag on generate`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/flag validate does not define`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/mutually exclusive watch flags` | all |  |
| cmd.exit.usage.invalid-flag-value | An invalid flag value (`lint --fail-on bogus`) exits `2` | `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/invalid fail-on value`; `unit: cmd/sqlgen/cli: TestMCPServe_RejectsInvalidLogLevel`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/mcp serve with no transport` | all |  |
| cmd.exit.usage.invalid-argument | A wrong or invalid argument (`completion nushell`, `completion` with no shell) exits `2` | `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/unsupported completion shell`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/completion without a shell`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/extra argument to generate`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/extra argument to init`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/extra argument to validate`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/extra argument to diff`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/extra argument to lint`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/extra argument to graphql init`; `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/extra argument to graphql gen` | all |  |
| cmd.exit.diff-changes-is-result | `diff` and `manifest diff` exiting `1` because they found changes is a result: the listing goes to stdout and nothing to stderr | `unit: cmd/sqlgen/cli: TestDiff_ChangesFoundIsAResult/exit 1 with nothing on stderr`; `unit: cmd/sqlgen/cli: TestManifestDiff_ChangesFoundIsAResult` | all |  |
| cmd.exit.prints-error | Every failing command prints its error to stderr, whatever the code: no command exits non-zero silently | `unit: cmd/sqlgen/cli: TestExit_EveryFailurePrintsItsError`; `unit: cmd/sqlgen/cli: TestExecute_InitOverExistingConfigPrintsError` | all |  |

## §23.10 Watch Mode
<!-- prd: 23.10 sha256: e4af6ac8d9112c6c -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cmd.watch.generate-no-flag | `sqlgen generate` has no `--watch` flag | `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/unknown flag on generate` | all |  |
| cmd.watch.validate-no-flag | `sqlgen validate` has no `--watch` flag | `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/unknown flag on validate` | all |  |
| cmd.watch.diff-no-flag | `sqlgen diff` has no `--watch` flag | `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/unknown flag on diff` | all |  |
| cmd.watch.lint-no-flag | `sqlgen lint` has no `--watch` flag | `unit: cmd/sqlgen/cli: TestExecute_UsageErrorsExitConfigAndPrint/unknown flag on lint` | all |  |
| cmd.watch.no-regenerate | No command regenerates code when a file changes | `unit: cmd/sqlgen/cli: TestNoCommandRegeneratesOnFileChange` | all |  |
| cmd.watch.mcp-serve-reloads | `sqlgen mcp serve` reloads the generated manifest on change (`--watch`); it is the one file-watching behavior (§31) | `unit: cmd/sqlgen/mcp: TestWatcherReloadsOnWrite`; `unit: cmd/sqlgen/cli: TestMCPServe_HelpListsFlags` | all |  |
| cmd.watch.mcp-default-on | `mcp serve`'s `--watch` is on by default | `unit: cmd/sqlgen/mcp/integration: TestStdioWatchReloadNotifiesAndServesNewData` | all |  |

## Not applicable

| Matrix cell | Reason | Accepted |
|---|---|---|
| commands: graphql-init × schema-error | §23.1: `graphql init` only scaffolds `gqlgen.yml`; it reads the config for the path and never parses the schema | |
| commands: manifest-validate × config-error | §23.1: `manifest validate` has its own exit codes (0 success, 1 schema failure, 2 I/O failure) and reads no `sqlgen.yml` | |
| commands: manifest-validate × schema-error | §23.1: `manifest validate` has its own exit codes and parses no SQL schema | |
| commands: manifest-validate × generation-error | §23.1: `manifest validate` has its own exit codes and generates nothing | |
| commands: manifest-diff × config-error | §23.1: `manifest diff` has its own exit codes (0 no diff, 1 any diff) and reads no `sqlgen.yml` | |
| commands: manifest-diff × schema-error | §23.1: `manifest diff` has its own exit codes and parses no SQL schema | |
| commands: manifest-diff × generation-error | §23.1: `manifest diff` has its own exit codes and generates nothing | |
| commands: mcp-serve × config-error | §31: `mcp serve` serves an already-generated manifest and reads no `sqlgen.yml` | |
| commands: mcp-serve × schema-error | §31: `mcp serve` parses no SQL schema | |
| commands: mcp-serve × generation-error | §31: `mcp serve` generates nothing | |
| commands: init × config-error | §23.4: `init` creates the config and reads none, so there is no config to be missing or invalid | |
| commands: init × schema-error | §23.4: `init` writes a starter config and parses no schema | |
| commands: completion × config-error | §23.5: `completion` prints a shell script and reads no config | |
| commands: completion × schema-error | §23.5: `completion` parses no schema | |
| commands: completion × generation-error | §23.5: `completion` runs no generation | |

## Unclaimed

Tests in this file's packages that back no PRD behavior. Tests in `cmd/sqlgen/cli` that back sections `parser.md` or `manifest.md` own (§5.3–§6.4, §16.1, §16.2, §30.3, §30.4.2, §31.2) are left for those slices.

| Unclaimed test | Reason |
|---|---|
| cmd/sqlgen/cli: TestCompletion_eachShellProducesOutput | Superseded by TestCompletion_PrintsEachShellsScript; asserts only non-empty output |
| cmd/sqlgen/cli: TestCompletion_invalidShellName | Superseded by TestExecute_UsageErrorsExitConfigAndPrint; asserts only that an error occurs |
| cmd/sqlgen/cli: TestCompletion_missingShellArgument | Superseded by TestExecute_UsageErrorsExitConfigAndPrint; asserts only that an error occurs |
| cmd/sqlgen/cli: TestDefaultCommand_noArgsRunsGenerate | Asserts only on its error path; bare `sqlgen` is backed by TestFullPipeline |
| cmd/sqlgen/cli: TestExitCodeMapping | Round-trips a hand-built exitError through exitCodeFromError; no command's exit code is exercised |
| cmd/sqlgen/cli: TestLint_failOnInfo | Superseded by TestLint_FailOnThreshold; asserts only that an error occurs |
| cmd/sqlgen/cli: TestLint_failOnWarning | Superseded by TestLint_FailOnThreshold; asserts only that an error occurs |
| cmd/sqlgen/cli: TestLint_outputFormat | Superseded by TestLint_OutputFormat; substring checks only |
| cmd/sqlgen/cli: TestLint_pathsScanSpecifiedDirectories | Superseded by TestLint_ScanDirectories; calls lintFiles directly and passes if only the last dir is scanned |
| cmd/sqlgen/cli: TestMCPServe_WatchAndNoWatchMutuallyExclusive | Superseded by TestExecute_UsageErrorsExitConfigAndPrint; asserts only that an error occurs |
| cmd/sqlgen/cli: TestParseSchema_SQLiteFileURI | Passes whether or not the URI's `mode=ro` is honored; no PRD clause asserted |
| cmd/sqlgen/cli: TestQuietOutput | Superseded by TestQuiet_SuppressesSummary; returns early on a generation error |
| cmd/sqlgen/cli: TestStaleCleanup_newlyGeneratedFilesNotDeleted | Counts `_gen.go` files > 0 only; a written file being kept is backed by the gen package's TestStaleFiles_WrittenFilesAreNeverStale |
| cmd/sqlgen/cli: TestStaleCleanup_nonGenFilesPreserved | The kept file carries no sqlgen header, so it would survive without the suffix rule |
| cmd/sqlgen/cli: TestVerboseOutput | Superseded by TestVerbose_ReportsTablesFilesAndTiming; returns early on a generation error |
| cmd/sqlgen/cli: TestGenerate_DatabaseSourceCompiles | Build smoke test of `source: database` output; no PRD clause beyond naming the source |
| cmd/sqlgen/cli: TestGenerate_CompositeKeyEdgeIsOmitted | Staging guard: composite-key edges stay out of generation until the planned composite-FK relationship work lands, when this test is replaced |
| cmd/sqlgen: TestE2EDiscovery | Test of the E2E harness (example discovery) |
| cmd/sqlgen: TestE2EGoldenFiles | Golden home: rows cite it as `golden: <example>`; the top-level test is harness |
| cmd/sqlgen: TestRewriteToDatabaseSource | Test of the E2E harness (database-twin config rewrite) |
| cmd/sqlgen: TestRewriteToFilesSource | Test of the E2E harness (files-twin config rewrite) |
| cmd/sqlgen/wrapper: TestMerge_ModelFieldsAbsentWhenUnset | Wrapper-internal: an empty merge input writes no `models:` key; no PRD clause |

## PRD gaps

Behavior the code has, most of it pinned by tests, that no PRD text states. Each waits for the user's ruling.

| Gap | Behavior | Tests | Section | Ruling |
|---|---|---|---|---|
| gap.no-graphql-env.any-value | Any non-empty `SQLGEN_NO_GRAPHQL` skips the chain, not only `1` (`true`, and by the code `0` or `false` too: `os.Getenv(...) != ""`, cli/generate.go:239) | `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/SQLGEN_NO_GRAPHQL=true honored`; `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/SQLGEN_NO_GRAPHQL empty does not opt out` | §23.2 | |
| gap.graphql-gen.requires-enabled | `sqlgen graphql gen` with `api.graphql.enabled` not true fails with `Error: api.graphql.enabled must be true to run 'sqlgen graphql gen'` and exit 2 (measured; cli/graphql.go:88-92) |  | §23.1 | |
| gap.graphql-gen.missing-gqlgen-yml | `sqlgen graphql gen` (and the chained generate) with no `gqlgen.yml` exits 2, a config error (cli/graphql.go:129-134). §26.5.6 says only "fails fast" | `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_MissingGqlgenYml` | §23.9 | |
| gap.discovery.hint-on-unparseable | Without `--config`, the `sqlgen init` hint is also printed when a config file is found but cannot be parsed (measured: invalid `sqlgen.yaml` prints `Error: parsing config: …` then `Hint: run 'sqlgen init' …`, exit 2). §23.3 promises the hint only when neither file exists |  | §23.3 | |
| gap.verbose-quiet-together | With both `-v` and `-q`, verbose wins: the full verbose report is printed (measured; cli/generate.go:191-202) |  | §23.2 | |
| gap.validate.exit-precedence | When `validate` hits errors in several phases, the exit code is the first failing phase's (config before schema before resolution) | `unit: cmd/sqlgen/cli: TestValidate_UnresolvedForeignKeyKeepsChecking` | §23.9 | |
| gap.no-graphql.on-bare | `--no-graphql` is also accepted on bare `sqlgen` (root flag), not only on `sqlgen generate` | `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_NoGraphQLFlag` | §23.2 | |
| gap.init.refuses-existing-config | `init` refuses (exit 1, writes nothing) when `sqlgen.yml` or `sqlgen.yaml` already exists in the current directory | `unit: cmd/sqlgen/cli: TestInit_errorsWhenConfigExists/sqlgen.yml exists`; `unit: cmd/sqlgen/cli: TestInit_errorsWhenConfigExists/sqlgen.yaml exists`; `unit: cmd/sqlgen/cli: TestExecute_InitOverExistingConfigPrintsError` | §23.4 | |
| gap.init.driver-per-dialect | Choosing `mysql` or `sqlite` at the prompt writes `output.driver: stdlib` (postgres writes `pgx`) | `unit: cmd/sqlgen/cli: TestInit_dialectToDriverMapping/postgres`; `unit: cmd/sqlgen/cli: TestInit_dialectToDriverMapping/mysql`; `unit: cmd/sqlgen/cli: TestInit_dialectToDriverMapping/sqlite` | §23.4 | |
| gap.init.prompt-choices | The prompt accepts `1`/`2`/`3`, the dialect name, or empty (postgres); any other answer is an error and nothing is written | | §23.4 | |
| gap.init.confirmation | `init` prints `Created sqlgen.yml (dialect: <d>, driver: <drv>)` on success | `unit: cmd/sqlgen/cli: TestInit_createsDefaultConfig` | §23.4 | |
| gap.diff.no-changes-message | With no changes, `diff` prints `no changes detected` (suppressed by `--quiet`) | `unit: cmd/sqlgen/cli: TestDiff_noChanges` | §23.6 | |
| gap.diff.unmapped-write-dir | A file the redirected run wrote to a directory `GenerateInto` did not declare fails `diff` (exit 1, "outside the preview sandbox") instead of being skipped | | §23.6 | |
| gap.lint.skips-gen-and-test-files | lint does not parse `*_gen.go` or `*_test.go` files in a scanned dir (lint.go:262-268); a hook registered in a `_test.go` file is never checked | | §23.7 | |
| gap.lint.missing-dir-is-empty | A `--paths` directory (or `output.dir`) that does not exist is scanned as empty with no error or warning (lint.go:249-251) | | §23.7 | |
| gap.lint.unparsable-file-exit-1 | A consumer `.go` file that does not parse fails the run with `Lint error: … parsing …` and exit 1 (measured) | | §23.7 | |
| gap.lint.unknown-op-constant-silent | An `Op*` constant with no table-level flag (`OpRefresh`, or a future constant) is treated as supported and never warned on | `unit: cmd/sqlgen/cli: TestOpEnabledForTable_ReadsResolvedOperations/OpRefresh` | §23.7 | |
| gap.manifest-diff.generation-config | `manifest diff` also walks the top-level `generation_config` block and prints `<key> false -> true` (exit 1) | `unit: cmd/sqlgen/cli: TestManifestDiff_GenerationConfigChanged` | §23.1 |  |
| gap.manifest-diff.error-order-insignificant | Reordering a method's `errors[]` with the set unchanged is no diff (exit 0) | `unit: cmd/sqlgen/cli: TestManifestDiff_MethodErrorReorderNotChanged` | §23.1 |  |
| gap.manifest-diff.per-entity-traversal | A per-entity index whose `file` pointer escapes the manifest directory is refused ("escapes the manifest directory") with exit 2 | `unit: cmd/sqlgen/cli: TestManifestDiff_PerEntityTraversalRejected` | §23.1 |  |
| gap.manifest-validate.embedded-fallback | When the `$schema` URL is unreachable or returns a body that is not a compilable schema, `manifest validate` validates against the embedded schema | `unit: cmd/sqlgen/cli: TestManifestValidate_JunkFetchedSchemaFallsBack`; `unit: cmd/sqlgen/cli: TestManifestValidate_OfflineFallback` | §23.1 |  |
| gap.manifest-validate.malformed-json-exit-1 | A manifest file that is not valid JSON exits 1 ("parse manifest"), not 2 | `unit: cmd/sqlgen/cli: TestManifestValidate_MalformedJSON` | §23.1 |  |
| gap.connection.error-redacts-password | A connection URL that fails to parse gives a connection error that quotes no part of the password | `unit: cmd/sqlgen/cli: TestParseSchema_MalformedPostgresURLHidesPassword` | §4.3 |  |
| gap.version.from-build-info | With no ldflags version, the version `--version` prints comes from the module's build info: a tagged install gives the tag without `v`; a pseudo-version, dirty tree, `(devel)` or no build info gives `dev` | `unit: cmd/sqlgen/cli: TestResolveVersion` | §23.2 |  |
| gap.verbose.reports-deleted | Under `--verbose`, `generate` lists each file the stale sweep deleted (`deleted: <path>`) | `unit: cmd/sqlgen/cli: TestStaleCleanup_verboseReportsDeletedFiles` | §23.2 |  |
| gap.api.no-unbound-field-stub | No emitted GraphQL field degrades to gqlgen's `panic("not implemented")` resolver in any example's committed resolvers | `unit: cmd/sqlgen: TestE2EGoldenResolvers_NoUnboundFieldStubs` | §26.4.1 |  |
