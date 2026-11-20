# Phase 34: GraphQL Server Scaffold Opt-Out

Status: Not Started
PRD Sections: 4 (API config table), 23 (`diff`), 26.5.6, 26.6

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

## 34.0 PRD sync — `server_scaffold` in §4, §26.5.6 and §26.6

**PRD Reference:** Sections 4 (API config table), 26.5.6, 26.6

**Status:** Not Started

### Tasks

- [ ] Add the `api.graphql.server_scaffold` row to the PRD API config table, after `api.graphql.max_complexity` (design §8 wording).
- [ ] Replace the `graph/server.go` row of the §26.5.6 file table with design §8's wording.
- [ ] Append design §8's `server_scaffold` sentence to the §26.6 protections bullet that names `limits_gen.go`.
- [ ] Add the design §6.1 rows to `docs/conformance/cli/api.md` (the `server.go` / limits rows) and `docs/conformance/cli/config-fields.md` (the `config-fields` default / explicit / schema cells), with status planned.
- [ ] Run `make req-check`. Review the registry rows for §4, §26.5.6 and §26.6, then `make req-rehash SECTIONS=…` for those sections.

### Acceptance Criteria

- The PRD states the key, its default (`true`), that `false` writes no `server.go` and leaves an existing one untouched, and that `limits_gen.go` is generated either way.
- `make req-check` and `make refs-check` pass.
- No code changes.

### Tests Required

- [ ] `make req-check` clean, with every new row citing its planned test name.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

## 34.1 Regression guard — `diff` and an existing `server.go`

**PRD Reference:** Sections 23 (`diff` coverage), 26.5.6

**Status:** Not Started

### Tasks

- [ ] Write `unit: cmd/sqlgen/cli: TestDiff_KeepsExistingServerGo`: in a GraphQL project where `server.go` exists with the consumer's own content, `sqlgen diff` reports no change for it and exits 0 when the rest is current.
- [ ] Register it in `docs/conformance/cli/commands.md` (or the row that already owns `diff` coverage).

### Acceptance Criteria

- The test passes on today's code, before any Phase 34 change.

### Tests Required

- [ ] `unit: cmd/sqlgen/cli: TestDiff_KeepsExistingServerGo`: passes on current code before any change.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

## 34.2 Config — the `server_scaffold` key, its default and its JSON Schema

**PRD Reference:** Sections 4 (API config table), 4.14

**Status:** Not Started

### Tasks

- [ ] Add `ServerScaffold *bool \`yaml:"server_scaffold"\`` to `GraphQLAPIConfig` (`cmd/sqlgen/config/config.go`), with a doc comment citing §26.5.6.
- [ ] Default an unset value to `true` in the `api.graphql` defaults block, next to `max_depth` / `max_complexity`.
- [ ] Add `server_scaffold` to the `graphql` object in `cmd/sqlgen/config/schema/v1.json`: `{"type": "boolean", "default": true, "description": …}`.
- [ ] Add a `fieldValueTypes` case for the key.

### Acceptance Criteria

- An unset `server_scaffold` resolves to `true`. An explicit `false` resolves to `false`.
- The JSON Schema accepts a boolean and rejects any other type. `TestSchemaV1_noDriftFromConfigStructs` still passes.
- No validation rule is added (design §6.3: a non-boolean is a strict-loader type error).

### Tests Required

- [ ] `unit: cmd/sqlgen/config: TestLoad_GraphQLServerScaffoldDefault` (unset → `true`, explicit `false` → `false`): fails before the change, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/config: TestSchemaV1_fieldValueTypes/api.graphql.server_scaffold`: fails before the change, passes after.
- [ ] Guard: `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` still passes.
- [ ] Guard: `unit: cmd/sqlgen/config: TestSchemaV1_acceptsExampleConfigs` still passes.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

## 34.3 Gen — skip the scaffold when `server_scaffold` is false; docs

**PRD Reference:** Sections 23 (`diff`), 26.5.6, 26.6

**Status:** Not Started

### Tasks

- [ ] Pass the resolved `server_scaffold` into `generateAPIServerFiles` / `generateAPIServerFile` (`cmd/sqlgen/gen/orchestrate.go`, `cmd/sqlgen/gen/api_limits.go`). When it's `false`, return `"", nil` before the `os.Stat` check.
- [ ] Leave `generateAPILimitsFile` unchanged.
- [ ] `docs/configuration.md` § `api.graphql`: add the `server_scaffold` row, and change the "deleting it doesn't stick" sentence to name the key.
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
