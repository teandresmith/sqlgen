# Phase 34: GraphQL Server Scaffold Opt-Out

Status: In Progress
PRD Sections: 4.14, 23 (`diff`), 26.3 (API config table), 26.5.6, 26.6

> **Design.** `docs/design/SERVER_SCAFFOLD.md`, approved 2026-11-20, is the source for every
> sub-item below. Its §9 lists sub-items N.0–N.3, which are 34.0–34.3 here. Its §6 behavior
> contract sets the Tests Required, and its §11 records the rulings:
> - Q1: the key is `api.graphql.server_scaffold: bool`, default `true`. Internally it's a
>   tri-state `*bool`.
> - Q2: there's no warning when the key is `false` and a `server.go` exists.
>
> The source is `sqlgen-example` v0.1.2 finding #4. The phase adds one config key. No golden
> changes: every example leaves the key unset.
>
> **Ground rules for every sub-item:**
> - **Fail first.** Each new behavior test is seen failing on the code before its change. Record it
>   in the Completion Record.
> - **Findings** go through `/fix` → Findings triage.

## 34.0 PRD sync — `server_scaffold` in §26.3, §26.5.6 and §26.6

**PRD Reference:** Sections 26.3 (API config table), 26.5.6, 26.6

**Status:** Complete

### Tasks

- [x] Add the `api.graphql.server_scaffold` row to the PRD API config table, after `api.graphql.max_complexity` (design §8 wording).
- [x] Replace the `graph/server.go` row of the §26.5.6 file table with design §8's wording.
- [x] Append design §8's `server_scaffold` sentence to the §26.6 protections bullet that names `limits_gen.go`.
- [x] Add the design §6.1 rows to `docs/conformance/cli/api.md` (the `server.go` / limits rows) and `docs/conformance/cli/api-schema.md` §26.3 (the default and schema rows, beside the other `api.graphql` defaults), with empty Tests cells (planned).
- [x] Run `make req-check`. Review the registry rows for §26.3, §26.5.6 and §26.6, then `make req-rehash SECTIONS=…` for those sections.

### Acceptance Criteria

- The PRD states the key, its default (`true`), that `false` writes no `server.go` and leaves an existing one untouched, and that `limits_gen.go` is generated either way.
- `make req-check` and `make refs-check` pass.
- No code changes.

### Tests Required

- [x] `make req-check` clean. The new rows have empty Tests cells, since `req-check` rejects a citation of a test that doesn't exist yet; the planned test names are listed against each row's sub-item in this file.

### Completion Record

**Completed 2026-11-20.**

- **PRD:** §26.3 API config table gains the `api.graphql.server_scaffold` row; the §26.5.6 file table's `graph/server.go` row and the §26.6 protections bullet carry design §8's wording.
- **Registry:**
  - `docs/conformance/cli/api.md`: four planned rows, `api.artifacts.server-go-scaffold-off`, `…-scaffold-off-kept`, `…-scaffold-off-diff` and `api.complexity.scaffold-off-limits-written`.
  - `docs/conformance/cli/api-schema.md` §26.3: two planned rows, `api.config.server-scaffold-default` and `api.config.server-scaffold-schema`.
  - All six have empty Tests cells.
  - Rehashed §26, §26.3, §26.5, §26.5.6 and §26.6 after review. The existing rows still hold.
- **Deviation:** the config rows went to `api-schema.md` §26.3 rather than `config-fields.md`, because the PRD's API config table is in §26.3 and its existing `api.graphql.*` default rows live there. Design §8 is corrected to match.
- **Checks:** `make req-check`, `make refs-check` and `make tracker-check` pass. No code changed.
- **Review (sqlgen-reviewer):** PRD 5/5, no defects. Applied its inline fixes:
  - the §4 → §26.3 API-config-table references in this file, `STATUS.md`, `IMPLEMENTATION_ORDER.md` and design §2.1/§6.1/§8;
  - this file's header status (In Progress);
  - `api.artifacts.server-go` narrowed to an unset or `true` `server_scaffold`;
  - a design §6.3 note on where the config rows landed;
  - design §8's PRD line refs.
- **Ruling (2026-11-20):** the `server_scaffold: false` guidance names both helpers. A hand-written server calls `ApplyQueryLimits` and wraps its handler in `sqlgenresolver.WithCallOptionsMiddleware` (§26.11 headers). Added to the §26.5.6 row and the §26.6 sentence, and to design §8.

## 34.1 Regression guard — `diff` and an existing `server.go`

**PRD Reference:** Sections 23 (`diff` coverage), 26.5.6

**Status:** Complete

### Tasks

- [x] Write `unit: cmd/sqlgen/cli: TestDiff_KeepsExistingServerGo`: in a GraphQL project where `server.go` exists with the consumer's own content, `sqlgen diff` reports no change for it and exits 0 when the rest is current.
- [x] Register it in `docs/conformance/cli/commands.md` as `cmd.diff.existing-server-go-not-proposed`.

### Acceptance Criteria

- The test passes on today's code, before any Phase 34 change.

### Tests Required

- [x] `unit: cmd/sqlgen/cli: TestDiff_KeepsExistingServerGo`: passes on current code before any change.

### Completion Record

**Completed 2026-11-20.**

- **Test:** `TestDiff_KeepsExistingServerGo` in `cmd/sqlgen/cli/diff_promises_test.go`.
  - It generates a GraphQL project with `setupDiffProject` and `generateForDiff`, then replaces the scaffolded `internal/graph/server.go` with the consumer's own content.
  - It asserts that `diff` exits 0, doesn't name `server.go`, and leaves the file byte-for-byte unchanged.
- **Run on current code:** PASS (`go test ./cli/ -run TestDiff_KeepsExistingServerGo`).
- **Mutation check:** with `generateAPIServerFile`'s existence check disabled (the early return skipped and the error branch guarded by `err != nil`), the test FAILS: `diff` lists `modify  internal/graph/server.go` and exits 1, so both the exit-code assertion and the "names server.go" assertion fire. Reverted.
  - My first mutation (`err == nil && false` alone) failed for an unrelated reason: it fell into the error branch with a nil error. The reviewer caught it, and I reran the check with the correct mutation.
- **Registry:** new row `cmd.diff.existing-server-go-not-proposed` in `docs/conformance/cli/commands.md` under §23.6.
- **Checks:** `make check` passes (tool pins match `.tool-versions`). `make check-examples` isn't triggered: only a `cmd/sqlgen/cli` test file changed. The security-review trigger doesn't apply, since no subcommand code changed.
- **Review (sqlgen-reviewer):** PRD 3/3, no defects. Applied its inline fixes:
  - the corrected mutation record above;
  - design §6.2's `diff` row marked guarded;
  - the registry row moved below the `coverage-graphql-*` group.
  
  The row derives from §23.6 plus §26.5.6 and cites §26.5.6 inline, as other rows cite cross-sections.

## 34.2 Config — the `server_scaffold` key, its default and its JSON Schema

**PRD Reference:** Sections 26.3 (API config table), 4.14

**Status:** Complete

### Tasks

- [x] Add `ServerScaffold *bool \`yaml:"server_scaffold"\`` to `GraphQLAPIConfig` (`cmd/sqlgen/config/config.go`), with a doc comment citing §26.5.6.
- [x] Default an unset value to `true` in the `api.graphql` defaults block, next to `max_depth` / `max_complexity`.
- [x] Add `server_scaffold` to the `graphql` object in `cmd/sqlgen/config/schema/v1.json`: `{"type": "boolean", "default": true, "description": …}`.
- [x] Add a `fieldValueTypes` case for the key.
- [x] Fill the Tests cells of `api.config.server-scaffold-default` and `api.config.server-scaffold-schema` in `docs/conformance/cli/api-schema.md`.

### Acceptance Criteria

- An unset `server_scaffold` resolves to `true`. An explicit `false` resolves to `false`.
- The JSON Schema accepts a boolean and rejects any other type. `TestSchemaV1_noDriftFromConfigStructs` still passes.
- No validation rule is added (design §6.3: a non-boolean is a strict-loader type error).

### Tests Required

- [x] `unit: cmd/sqlgen/config: TestLoad_GraphQLServerScaffoldDefault` (unset → `true`, explicit `false` → `false`): fails before the change, passes after. All dialects.
- [x] `unit: cmd/sqlgen/config: TestSchemaV1_fieldValueTypes/api.graphql.server_scaffold given a string`: fails before the change, passes after.
- [x] Guard: `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` still passes.
- [x] Guard: `unit: cmd/sqlgen/config: TestSchemaV1_acceptsExampleConfigs` still passes.

### Completion Record

**Completed 2026-11-20.**

- **Config:** `GraphQLAPIConfig.ServerScaffold *bool \`yaml:"server_scaffold"\`` (`cmd/sqlgen/config/config.go`), resolved to `new(true)` in `applyAPIDefaults` when unset. This follows the existing default-true `*bool` fields (`IncludeExamples`, `Breadcrumbs.ClaudeMD`).
- **Schema:** `server_scaffold` in `cmd/sqlgen/config/schema/v1.json`'s `graphql` object: `boolean`, `default: true`, with a description.
- **Tests:**
  - `TestLoad_GraphQLServerScaffoldDefault` (`config/api_defaults_test.go`) has three cases: unset → true, explicit false, explicit true.
  - `TestSchemaV1_fieldValueTypes/api.graphql.server_scaffold given a string` (`config/schema_fields_test.go`).
- **Fail first:**
  - The default test failed to build before the change (`ServerScaffold undefined`).
  - The schema case failed with `additionalProperties 'server_scaffold' not allowed` on its good instance.
  - Both pass after the change.
- **Guards:** `TestSchemaV1_noDriftFromConfigStructs`, `TestSchemaV1_acceptsExampleConfigs` and `TestAPIDefaults` pass.
- **Registry:** filled the Tests cells of `api.config.server-scaffold-default` and `api.config.server-scaffold-schema`.
- **Checks:** `make check` passes, and so does `make check-examples`. The examples run was triggered because the config struct shape changed (the trigger table's `config/types.go` is `config/config.go` in this tree).
  - Its first run failed `conformance-fresh`. All 8 conformance modules were stale in comment lines from the earlier `ErrAlreadyRelated` and nested-create comment commits, which regenerated the example goldens but not the conformance modules.
  - They are regenerated in `1f65fbda`, comment lines only, and the rerun passes.
- **Not yet wired:** gen doesn't read the key until 34.3, so `server_scaffold: false` has no effect yet.
- **Review (sqlgen-reviewer):** PRD 4/4, fail-first confirmed independently, no defects. Applied:
  - design §5's `config.go` line ref;
  - design §6.1's schema test name;
  - a 34.3 task note: read a nil `ServerScaffold` as `true` for the hand-built test fixtures.

## 34.3 Gen — skip the scaffold when `server_scaffold` is false; docs

**PRD Reference:** Sections 23 (`diff`), 26.5.6, 26.6

**Status:** Not Started

### Tasks

- [ ] Pass the resolved `server_scaffold` into `generateAPIServerFiles` / `generateAPIServerFile` (`cmd/sqlgen/gen/orchestrate.go`, `cmd/sqlgen/gen/api_limits.go`). When it's `false`, return `"", nil` before the `os.Stat` check. Read a nil `ServerScaffold` as `true` (`g.ServerScaffold == nil || *g.ServerScaffold`): about 20 test fixtures build `GraphQLAPIConfig` by hand without `applyAPIDefaults`.
- [ ] Leave `generateAPILimitsFile` unchanged.
- [ ] `docs/configuration.md` § `api.graphql`: add the `server_scaffold` row, and change the "deleting it doesn't stick" sentence to name the key. Say that a hand-written server calls `ApplyQueryLimits` and wraps its handler in `WithCallOptionsMiddleware`.
- [ ] Move each design §6.1 registry row from planned to its test.

### Acceptance Criteria

- With `server_scaffold: false`, `generate` writes no `server.go` and doesn't list one among its written files. `limits_gen.go` is written with the configured values. An existing `server.go` is left byte-for-byte unchanged, with no warning.
- With `server_scaffold: false` and no `server.go` on disk, `sqlgen diff` reports no `server.go` change.
- With the key unset, behavior is identical to today. No golden changes.

### Tests Required

- [ ] `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/server_scaffold false writes no server.go`: fails before the change, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/server_scaffold false still writes the limits file`: fails before 34.2 (the strict loader rejects the unknown key), passes after. All dialects.
- [ ] `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/server_scaffold false leaves an existing server.go untouched`: fails before 34.2 (the unknown key), passes after. All dialects.
- [ ] `unit: cmd/sqlgen/cli: TestDiff_ServerScaffoldOffProposesNoServer`: fails before the change, passes after. All dialects.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/server.go is written when absent` still passes.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/an existing server.go is left untouched` still passes.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/the limits file is regenerated with new values on a second run` still passes.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestGenerate_GraphQLServerCarriesQueryLimits` still passes.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/defaults are depth 5 and complexity 1000 when unset` still passes.
- [ ] Guard: `example: graphql: TestQueryLimits`, `golden: graphql` and `golden: graphql_top_level` are unchanged.
- [ ] Sweep: `make check` and `make check-examples` are green.

### Completion Record

_Filled in when complete: files changed, commit references, notes._
