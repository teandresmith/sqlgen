# Phase 22: Column Access Control

Status: Complete (closed 2026-07-13)
PRD Sections: 32 (Column Access Control — normative), 4.8 (ColumnOverride), 4.13 (validation rules), 26.4/26.5 (GraphQL schema + resolver projection), 28.6 (event input redaction), 30.4.2/30.7 (manifest column fields)

> **Normative spec:** PRD §32 — authoritative for this phase (six design questions resolved: Q1 cache **excluded** from redaction scope — round-trip-safety landmine; Q2 config-only classification, no SQL-comment directive in v1; Q3 four roles ship, raw capability object deferred; Q4 all four roles kept — each distinct; Q5 entity `json` tags never altered; Q6 manifest `sql_bodies` stay accurate, `redacted` marker suppresses value-surfacing). PRD §32 is the normative contract (already drafted from this doc); 22.6 flips the design doc to SYNCED.

> **Rationale:** Some columns must never reach external consumers even though the server reads and writes them freely — `password_hash`, secret tokens, internal scores, server-managed audit stamps. `access` is a per-column classification (`public` / `read_only` / `write_only` / `hidden` / `internal`) that constrains which **external** surfaces a value escapes through, resolved once and honored across every surface. It is **not** `exclude_columns` (which removes the column from Go entirely) and **not** authorization (which is a runtime row/op policy).

> **Load-bearing principle:** `access` is a projection over external surfaces, never a change to the core Go client. The entity struct, `Create*`/`Update*` inputs, `Filter`, `Sort`, and the cached entity always carry every surviving column — the server is inside the trust boundary. `access` only narrows the generated API (GraphQL/REST), event payloads, and the manifest.

> **Scope rule:** In-scope external surfaces: GraphQL (§26), events (§28), manifest (§30). **Cache is deliberately out of scope** (design Q1) — field-level cache redaction breaks cache-hit/cache-miss entity equivalence; escape hatches are per-table `cache.enabled: false` or an encrypted backend. **REST (§26.8) is deferred with the REST feature itself (D.1)** — the output projection + input DTO filtering fold into D.1 when REST lands, reusing this phase's capability model. The core Go client, and MySQL/SQLite-only packages with no API/events/manifest, are byte-unchanged by a `public`-only config.

> **Depends on:** Phase 16 (GraphQL surface — walker, translators, schema templates), Phase 11 (event system + generated event hooks), Phase 18 (manifest builder + runtime types + JSON schema).

---

## 22.1 Config field + gen-layer resolution + capability model + validation

**PRD Reference:** §4.8 (ColumnOverride), §4.13 (validation rules), §32.2 (roles → capabilities), §32.4 (validation)
**Design Reference:** PRD §32 (Q2/Q3)

**Status:** Complete

### Tasks

- [x] Add `Access string` (yaml `access`) to `config.ColumnOverride` (`cmd/sqlgen/config/config.go:557`), with a doc comment naming the five roles and the default.
- [x] Define the role enum + a single table-driven `roleCapabilities(role) capabilities` function (one source of truth for the §32.2 matrix) in the gen layer — capabilities are `APIReadable`, `APIWritable`, `APIFilterable`, `APISortable`, `EventRedacted` (bool) + `ManifestVisibility` (string). Unknown/empty role resolves to `public`.
- [x] Add the resolved fields to `gen.ColumnContext` (`cmd/sqlgen/gen/context.go:123`): `Access` (role string) + the derived capability booleans, populated during column resolution (the same pass that resolves Go type / field name / description — **not** on `parser.Column`, per ARCHITECTURE.md).
- [x] Add config-phase validation (`cmd/sqlgen/config/validate.go`): `access` value must be one of the five roles; `access` on a non-existent-or-excluded column is an error.
- [x] Add schema-phase validation: PK column must have `access ∈ {public, read_only}` (else error); a required-on-create column (NOT NULL, no default, not auto/PK-generated) may not take a role dropping it from API-in (`read_only`/`hidden`/`internal`) while create/createMany API is enabled (error); soft-delete column with a role dropping its filter (`write_only`/`hidden`/`internal`) emits a warning.

### Acceptance Criteria

- A column with `access: internal` resolves to `APIReadable=false`, `APIWritable=false`, `APIFilterable=false`, `APISortable=false`, `EventRedacted=true`, `ManifestVisibility=internal`; `public` (and unset) resolves to all-true / not-redacted / `public`; each of `read_only` / `write_only` / `hidden` matches its §32.2 row exactly.
- An unknown `access` string, or `access` on an excluded / non-existent column, fails config validation with a message naming the column and (for the enum case) the allowed set. All validation errors report together (§4.13 batch behavior).
- `access: write_only` on a PK is a hard error; `access: read_only` on a PK is accepted; `access: internal` on a required-on-create column with create API enabled is a hard error; the same on a column with a DB default (or when create API is disabled) is accepted.
- The core Go client is unaffected — the resolved entity/input/filter/sort contexts still contain every surviving column regardless of role.

### Tests Required

- [x] Unit: table-driven `roleCapabilities` over all five roles + the empty/unknown default, asserting every capability field against the §32.2 matrix.
- [x] Unit: `access` resolution onto `ColumnContext` for each role (via the config→gen resolution pass).
- [x] Unit: validation — invalid role value; access on excluded column; access on non-existent column; PK outside `{public, read_only}`; required-on-create removed from API-in with create enabled (error) vs. same with a default / create-disabled (accepted); soft-delete filter-drop (warning).

### Completion Record

**Completed 2026-07-13.** Files: `cmd/sqlgen/config/config.go` (ColumnOverride.Access + the five `Access*` role constants + `IsValidAccessRole`), `cmd/sqlgen/config/validate.go` (pre-parse `validateColumnAccess`: role enum + excluded-column; post-parse `validateColumnAccessSchema`/`validateOneColumnAccess`: existence, PK-readability incl. `primary_key.columns` override, required-on-create × create-API gate via `tableCreateAPIEnabled`, soft-delete filter warning; `SchemaColumn` gained `HasDefault`/`AutoIncrement`), `cmd/sqlgen/cli/pipeline.go` (populates the new SchemaColumn fields; GENERATED ALWAYS counts as defaulted), `cmd/sqlgen/gen/access.go` (new — `accessCapabilities`, `roleCapabilityTable`, `resolveAccess`), `cmd/sqlgen/gen/context.go` (ColumnContext access + capability fields), `cmd/sqlgen/gen/context_table.go` (resolution in `buildColumnContext`), `cmd/sqlgen/gen/context_view.go` (extracted `buildViewColumns`; view columns resolve to public explicitly so capability booleans are never spuriously all-false). Tests: `gen/access_test.go` (matrix incl. empty/unknown→public), `gen/access_resolution_test.go` (per-role ColumnContext resolution + core-client untouched + view public default), `config/access_validate_test.go` (enum, excluded both levels, batch reporting, non-existent, PK roles + PK override, required-on-create × 10 gate combinations, soft-delete warning). Notes: PK members are exempt from the required-on-create rule (governed by the stricter PK-readability rule); invalid-preset ops resolve to "create API off" to avoid double-reporting the pre-parse preset error. Verify adjudication: the required-on-create error `return`s before the soft-delete warning check — reachable (a NOT-NULL no-default bool soft-delete column with `hidden`/`internal` + create API on) but immaterial, since that exact config is already a hard error and any resolution path re-surfaces the warning when still applicable; no FIX warranted. `make check` + `make check-examples` green; generated output byte-unchanged (no template changes).

---

## 22.2 Manifest — per-column `access` + `redacted`

**PRD Reference:** §30.4.2 (entity/column shape), §30.7 (extended schema metadata)
**Design Reference:** PRD §32 (manifest)

**Status:** Complete

### Tasks

- [x] Emit `access` (role string) and `redacted` (bool — true for `write_only`/`internal`) on each manifest column, in the manifest builder (`cmd/sqlgen/gen/context_manifest.go`) and the runtime `manifest/` column type. Both fields additive + optional; omitted rendering equals `public` / `false`.
- [x] Add the two properties to the JSON schema (`schema/v1.json`), default-valued so a pre-existing all-`public` manifest still validates — additive change against `0.1.0`, no freeze coordination (design Q6; mirrors the Phase-21 `materialized` precedent).
- [x] Leave `methods[].sql_bodies` untouched — the SQL stays the real server query; the `redacted` marker is what tells MCP/agents not to advertise the value.
- [x] Extend `sqlgen manifest diff` (`cmd/sqlgen` manifest diff walk) to report an `access` change on a column as `changed`, so a reclassification surfaces in PR review.

### Acceptance Criteria

- A manifest column for an `internal`/`write_only` column carries `access` set and `redacted: true`; a `public` column carries `access: "public"` (or omits, decoding to `public`) and `redacted: false`.
- The emitted manifest validates against `schema/v1.json`; a manifest with no access fields (older shape) still validates.
- `manifest diff` between two manifests differing only in one column's `access` reports exactly that column as changed.
- MCP tools/resources surface the new fields unchanged in shape (no MCP code change required — it is a read-only manifest consumer).

### Tests Required

- [x] Unit: manifest builder emits `access` + `redacted` correctly per role (single + per_entity layouts).
- [x] Unit: `schema/v1.json` validates a manifest with and without the access fields.
- [x] Unit: `manifest diff` flags an `access`-only change as `changed`.

### Completion Record

**Completed 2026-07-13.** Files: `cmd/sqlgen/manifest/types.go` + runtime `manifest/types.go` (`Column.Access`/`Column.Redacted`, both `omitempty` — public renders omitted), `cmd/sqlgen/manifest/builder.go` (`buildColumns` sets the markers from `ColumnContext.ManifestVisibility`; `Redacted` derived from the role constants, not `EventRedacted` — the two coincide per §32.2 but mark independent surfaces), `cmd/sqlgen/manifest/schema/v1.json` (`access` enum + `redacted` boolean, both optional + default-valued — additive against `0.1.0`, mirroring the Phase-21 `materialized` precedent), `cmd/sqlgen/cli/cmd_manifest_diff.go` (`columnSig` includes `access=`/`redacted` so reclassification reports `~ <col>`; omitted-access = public keeps old-shape manifests diff-clean). `sql_bodies` untouched (no change to `sql.go`). Tests: `cmd/sqlgen/manifest/access_test.go` (builder markers per role; emitted JSON carries the fields in **both** single and per_entity layouts with public omitted; `ValidateAgainstSchema` accepts with/without the fields and rejects an out-of-enum role), `cmd/sqlgen/cli/cmd_manifest_diff_test.go` (`TestManifestDiff_ColumnAccessChanged` — public→internal and public→read_only both report `~ email`). MCP needs no change (read-only manifest consumer; fields flow through). `make check` + `make check-examples` green; zero golden churn (all-public examples omit the fields — byte-identical). Security note: runtime `manifest/` change is two passive JSON-tagged fields; flagged for the verify reviewer instead of a standalone /security-review.

---

## 22.3 Event payload redaction

**PRD Reference:** §28.6 (event input — amended "same pointer" guarantee), §28.9 (generated event hook architecture)
**Design Reference:** PRD §32 (event redaction helper)

**Status:** Complete

### Tasks

- [x] For each table with ≥1 `write_only`/`internal` column, generate a per-table `redact<T>CreateInput(*Create<T>Input) *Create<T>Input` and `redact<T>UpdateInput(...)` helper that shallow-copies the input and clears the redacted fields (omittable input fields → unset; required fields → zero value). Emit these only for such tables (gate in the event-context/template layer — `context_event.go` + the event-hook template).
- [x] Wire the generated event hook to publish the redacted clone as `Event.Input` (the un-redacted pointer continues to the DB write). Tables with no redacted column keep passing the caller's pointer verbatim — byte-unchanged.
- [x] Confirm `Event.PK` is never redacted (guaranteed by 22.1 validation — PKs cannot be sensitive).
- [x] Handle the batch and `*Where` shapes from the §28.6 table (CreateMany i-th element, UpdateMany item, UpdateWhere shared input) — the per-entity redacted shape must match the shape a subscriber's type-switch expects.

### Acceptance Criteria

- After a `Create`/`Update`/`Upsert`/`CreateMany`/`UpdateMany`/`UpdateWhere` on a table with a redacted column, the published `Event.Input` has the redacted field cleared while every non-redacted field is intact and the concrete input type is unchanged (subscriber type-switch still matches).
- A table with no redacted column emits `Event.Input` as the exact caller pointer — no clone, no allocation, generated event hook byte-identical to pre-phase output.
- `Event.PK` carries the real PK on every op regardless of any column's role.

### Tests Required

- [x] Unit/generated: redact helper clears `write_only` + `internal` fields, preserves the rest, for Create and Update input shapes.
- [x] Unit/generated: a table with no redacted column produces the unchanged (same-pointer) event hook — golden byte-compare.
- [x] Integration (memorybus): subscribe, mutate a table with a redacted column, assert the received `Event.Input` is redacted and its concrete type still matches the subscriber arm.

### Completion Record

**Completed 2026-07-13.** Files: `cmd/sqlgen/gen/context_event.go` (`EventTableContext` gained `RedactedCreateFields`/`RedactedUpdateFields` + `EmitRedactCreate`/`EmitRedactUpdate`; `EventHooksContext.HasRedactedTables`; `buildEventTableContext` + `redactedInputFields` — emission gated on redacted fields present in the input shape AND the referenced input type being generated per ops), `cmd/sqlgen/gen/templates/event_hooks.go.tmpl` (shared generic `redactZero[T]` in-place zeroer — omittable → unset, required → zero, no per-type imports; per-table `redact<T>CreateInput`/`redact<T>UpdateInput` shallow-clone helpers, nil-pass-through; switch arms: OpCreate/OpUpsert + OpUpdate/OpUpdateWhere publish redacted clones via type-assert-or-nil (a failed assert publishes nil, never the raw input), OpCreateMany redacts the i-th element, OpUpdateMany clones the `Update<T>Item` value with a redacted inner input — subscriber type-switch shapes preserved; fanout doc comment conditional so unredacted tables stay byte-identical). Events example: `accounts` table (internal `password_hash`, write_only `recovery_code`, public `email`/`note`) in schema + column_map; goldens regenerated (churn confined to events example — all other examples byte-unchanged, pinning the §28.6 same-pointer guarantee). Tests: `gen/event_redaction_template_test.go` (helpers + all switch arms + public fields never cleared; unredacted table renders zero redaction machinery with pre-§32 arms intact; ops-gating never references ungenerated input types), events example `tests/redaction_test.go` (memorybus: Create/Update/UpdateWhere/CreateMany/UpdateMany/Upsert — Event.Input redacted, concrete types match subscriber arms, caller inputs unmutated, DB write un-redacted, Event.PK intact, unredacted products table publishes the exact caller pointer). **Increment leg (added post-review):** the auto-reviewer caught that PRD §28.6 groups `Increment` with Create/Update/Upsert under the redacted-clone exception while §32.3 only names the create/update helpers — the §28.6 behavioral contract wins, so `redact<T>IncrementInput` blanks `IncrementInput.Amount` when the target column is redacted (`RedactedIncrementConsts` + `EmitRedactIncrement` gating; subscribers see which column changed, never the delta; value semantics — no clone needed; `default:` arm keeps `exhaustive` satisfied); events example gained `accounts.internal_score` (internal, incrementable) + `TestEventRedaction_Increment` (Event Amount blanked, DB applies the real delta); §32.3's helper enumeration needs an increment mention — routed to 22.6 PRD reconciliation. `make check` + `make check-examples` green; golden churn confined to the events example.

---

## 22.4 GraphQL projection + walker-completeness lint amendment

**PRD Reference:** §26.4 (schema generation), §26.5.1 (exposed surface), §26.5.2 (field-selection walker), §26.5.3 (filter/sort translators), §26.5.4 (mutation inputs), §32.3
**Design Reference:** PRD §32 (GraphQL walker)

**Status:** Complete

### Tasks

- [x] Filter the generated GraphQL surface by capability in the API context (`cmd/sqlgen/gen/context_api.go`): `type <T>` emits only `APIReadable` columns; `Create<T>Input`/`Update<T>Input` only `APIWritable` (and `_inc`/`_dec` only for writable numerics); `<T>Filter` only `APIFilterable`; `<T>Sort` enum only `APISortable`. Relationship fields unaffected (a hidden FK can coexist with an exposed nested object).
- [x] Amend the walker-completeness lint `ValidateAPIWalkerCompleteness` (`cmd/sqlgen/gen/api_walker.go:19`): flip from "every column must have a walker case" to "every **API-readable** column must have a case, and access-restricted columns must **not**." Iterate the API-visible column set for the walker, filter translator, and sort translator.
- [x] Update the per-table field-options walker, filter translator, and sort translator generation to iterate only the API-visible column set for each surface (the entity `Get` still SELECTs all columns — server-side — but gqlgen has no schema field through which a non-readable column can serialize).
- [x] Update the generated per-table GraphQL test that builds a deep query touching every relationship/column to (a) touch only exposed columns and (b) assert access-restricted columns are **absent** from the schema type / input / filter / sort. *(The graphql example's artifact-level walker test derives its expected set from the emitted schema, so it adapts automatically; the restricted-column absence assertions on live example artifacts land with 22.5's classified columns. Unit level covered by `TestGraphQLWalker_AccessProjection` + `TestGraphQLSchema_AccessProjection`.)*

### Acceptance Criteria

- For a table with `password_hash: internal`, `created_at: read_only`, `new_password: write_only`, `internal_score: hidden`: `type <T>` omits `passwordHash`, `newPassword`, `internalScore` and includes `createdAt`; `Create<T>Input`/`Update<T>Input` include `newPassword` and omit the other three; `<T>Filter` includes `createdAt` and omits all four restricted-from-filter columns; `<T>Sort` omits them.
- The walker-completeness lint passes on this config (restricted columns intentionally absent) and still fails if an **API-readable** column is missing a walker case — the original guard is intact for the exposed set.
- A `public`-only config produces GraphQL output byte-identical to pre-phase.
- The entity struct returned to gqlgen is fully populated; no non-readable column is reachable through any schema field.

### Tests Required

- [x] Unit: schema/type/input/filter/sort generation over a mixed-role table asserts the exact exposed vs. omitted column sets per surface.
- [x] Unit: `ValidateAPIWalkerCompleteness` — passes with restricted columns absent; fails when a readable column lacks a case; fails when a restricted column has a (dead) case.
- [x] Generated per-table test: deep query touches only exposed columns; restricted columns are absent from schema type / input / filter / sort. *(Unit-level render assertions in 22.4; live-artifact assertions land with 22.5's example columns.)*
- [x] Golden: `public`-only GraphQL example output byte-unchanged.

### Completion Record

**Completed 2026-07-13.** Files: `cmd/sqlgen/gen/access.go` (`columnAccessCapabilities` — normalizes an empty `Access` role to public so hand-built fixtures don't zero-capability-drop columns, the FIX-059-class trap), `cmd/sqlgen/gen/context_api.go` (`APIFieldContext` gained `Readable`/`Writable`/`Sortable` + `Filterable` now ANDs the access capability with the FIX-103 comparator gate; `mapColumnToGraphQL` populates the flags and skips scalar/enum registry side-effects for columns dropped from every API surface; `buildUpdateOps` skips non-writable numerics — schema `_inc`/`_dec` gate and resolver/seed dispatch stay in sync; `buildAPIFilterFields` + `buildAPISortFields` + `buildAPIInputFields` filter their translator lists by capability; `HasCreateInput`/`HasUpdateInput` now require ≥1 **writable** non-PK field — the FIX-071 empty-input rule extended so a fully-non-writable table omits the input type and its mutations together), `cmd/sqlgen/gen/templates/api/schema.graphqls.tmpl` (type gated on `Readable`, sort enum on `Sortable`, create/update inputs + `_inc`/`_dec` on `Writable`; filter input already keyed on the ANDed `Filterable`), `cmd/sqlgen/gen/templates/api/field_options.go.tmpl` (walker case gated on `Readable`), `cmd/sqlgen/gen/api_walker.go` (lint flipped: API-readable columns must have a case AND non-readable columns must not; relationships unchanged). Tests: `gen/api_access_test.go` (mixed-role schema render asserting exact exposed/omitted sets across type/create/update/filter/sort + `_inc`/`_dec` + UpdateOps; translator-list ↔ schema-gate sync; amended lint over 4 shapes; rendered walker case assertions), `api_walker_test.go` fixtures gained explicit `Readable: true`. Sort enum and object type can never render empty: no-PK tables are skipped from generation entirely (PRD §9.4b, pinned by `TestBuildTableContexts_NoPK_SkippedFromGeneration`), and every generated table's PK is validated `public`/`read_only` (§32.4) → always readable + sortable. Review noted (LOW, benign): `comparatorFamiliesFromTables` is not access-filtered, so dropping a family's last filterable column leaves an unused-but-legal `input <Family>Comparator` declaration — to be eyeballed in 22.5's mixed-role goldens. `make check` + `make check-examples` green; graphql example goldens **byte-identical** (public-only projection is a no-op).

---

## 22.5 E2E example coverage

**PRD Reference:** §32 (all surfaces)
**Design Reference:** PRD §32 (task 4/E2E)

**Status:** Complete

### Tasks

- [x] Add access-classified columns to an existing example that has GraphQL + events + manifest enabled (e.g. the `graphql` example): a `write_only` secret, an `internal` secret, a `read_only` audit column, a `hidden` column. Regenerate models + expected goldens. *(The graphql example lacked events — enabled `events: enabled: true` so one example covers every in-scope surface on the same columns.)*
- [x] E2E leg asserting the full cross-surface contract: (a) GraphQL — restricted columns absent from schema type / create input / filter / sort, `write_only` accepted on create and never returned; (b) events — subscribe via memorybus, mutate, assert `Event.Input` redacts `write_only`/`internal` and preserves the rest; (c) manifest — the emitted manifest carries `access` + `redacted` per column; (d) core Go client — `Get`/`Create` still round-trip every column including the secrets.
- [x] Assert the cache leg is faithful (design Q1): a cached-then-hit `Get` returns the same entity — including the secret columns — as a cache-miss `Get`.

### Acceptance Criteria

- The example regenerates deterministically; `make check-examples` is clean.
- The E2E leg demonstrates each role's behavior on every in-scope surface, plus the core-client and cache faithfulness invariants.

### Tests Required

- [x] E2E: GraphQL exposure per role (schema, input, filter, sort).
- [x] E2E: event redaction via memorybus.
- [x] E2E: manifest `access`/`redacted` present.
- [x] E2E: core Go client + cache round-trip includes secret columns unchanged.

### Completion Record

**Completed 2026-07-13.** Files: `cmd/sqlgen/testdata/examples/graphql/schema.sql` (users gained `password_hash TEXT NOT NULL DEFAULT ''` internal — the DEFAULT keeps it out of the required-on-create set so §32.4 passes with the create API enabled; `new_password` write_only; `last_login_at` read_only; `internal_score` hidden), `sqlgen.yml` (users column_map + `events: enabled: true` — the example previously lacked events, enabling it lets one example cover every in-scope surface on the same columns), regenerated goldens (`expected/` + `models/`: models/filters/sorter/graph schema + translators + walker, `event_hooks_gen.go` incl. `redactUser*` helpers, manifest entities with access markers; churn confined to the graphql example), `tests/access_test.go` (new, 5 legs): `TestAccess_SchemaArtifacts` (shipped `user_gen.graphqls`: exact exposed/omitted sets across type/create/update/filter/sort), `TestAccess_GraphQLLive` (live gqlgen server: `newPassword` accepted on create and lands in the DB via the core client; selecting `passwordHash`/`newPassword`/`internalScore` and supplying `passwordHash` on create are schema-validation-rejected before any resolver runs — HTTP 422 GRAPHQL_VALIDATION_FAILED), `TestAccess_EventRedaction` (memorybus: Event.Input clone redacts internal + write_only, preserves public, concrete type + PK intact, caller unmutated), `TestAccess_ManifestMarkers` (emitted `public_user.json`: access+redacted per role, public omitted), `TestAccess_CoreClientAndCacheFaithful` (cache-miss and cache-hit `Get` return identical entities including all three secrets — design Q1; hydration awaited via the existing spyBackend). Full graphql example suite green (33s — all pre-existing tests unaffected by the events enablement); `make check` + `make check-examples` green. Reviewer's 22.4 orphan-comparator note checked against the new mixed-role goldens: no orphan family emitted (all comparator families still referenced by other tables' filterable columns).

---

## 22.6 Closure sweep + design-doc sync

**PRD Reference:** §32 (+ cross-refs §4.8/§4.13/§26/§28.6/§30)
**Design Reference:** PRD §32

**Status:** Complete

### Tasks

- [x] Verify all 22.1–22.5 sub-items complete; triage any open FIXes by severity. *(All 5 Complete; 0 FIXes logged across the phase — every reviewer/verify finding was fixed inline pre-`/done`; ledger clean.)*
- [x] Full sweep: `make check` (all modules, `-race`), `make check-examples`, `make test-integration` — all clean. *(All three first-pass green 2026-07-13; longest integration legs `cmd/sqlgen/cli` ~468s, `cmd/sqlgen` ~218s, `gen` ~36s, `mcp/integration` ~19s, `parser/introspect` ~11s; no flakes.)*
- [x] Reconcile PRD §32 + cross-refs against the shipped behavior; fix any drift discovered during implementation. *(§26.5.2 walker-lint paragraph gained the §32.3 amendment cross-ref so §26 readers don't act on the pre-§32 rule.)*
- [x] Reconciliation checkpoint (routed from 22.2 review): the markdown manifest emitter does not surface `access`/`redacted` markers — confirm PRD §32.3/§30 scope the manifest marking to the canonical JSON (markdown is a summary surface) or extend the markdown template; document the outcome either way. *(Decided: JSON-canonical. PRD §32.3 manifest paragraph now states the markers live on the canonical JSON only — the markdown column table is a curated summary that already omits comparable §30.7 fields, and MCP reads the JSON. Recorded as design-doc Q7 with the breadcrumb-proximity caveat.)*
- [x] Reconciliation checkpoint (routed from 22.3 review): PRD §32.3's events paragraph enumerates only `redact<T>CreateInput`/`redact<T>UpdateInput`, but §28.6 covers `Increment` under the redacted-clone exception and the implementation ships `redact<T>IncrementInput` (delta blanked). Amend §32.3's helper enumeration to include the increment helper; also check §28.6's shape-table row `Increment → *<Struct>IncrementInput` against the actual value type `IncrementInput[<Struct>IncrementColumn]` (pre-existing drift). *(Both amended: §32.3 names the increment helper; §28.6 prose notes Increment passes by value + delta-blanking semantics; the shape-table row corrected to `IncrementInput[<Struct>IncrementColumn]` (by value). Recorded as design-doc Q8.)*
- [x] Flip the design supplement to SYNCED (superseded by PRD §32); the supplement was retired outright on 2026-09-11.
- [x] Update STATUS.md (mark phase Complete, record the surface-delta summary). *(Landed via the phase-closure sweep.)*

### Acceptance Criteria

- All sub-items Complete; 0 open blocking FIXes.
- Full test sweep green, no flakes.
- PRD §32 agrees with the shipped behavior.

### Tests Required

- [x] The full `make check` + `make check-examples` + `make test-integration` sweep (green, no flakes).

### Completion Record

**Completed 2026-07-13.** Files: `docs/PRD.md` (§26.5.2 walker paragraph gained the §32.3 amendment cross-ref; §28.6 prose amended — Increment passes by value, delta blanked on redacted targets — and the shape-table `Increment` row corrected to `IncrementInput[<Struct>IncrementColumn]` (by value), fixing pre-existing drift; §32.3 events paragraph now enumerates `redact<T>IncrementInput`; §32.3 manifest paragraph documents the JSON-canonical marker decision), PRD §32 (header flipped to SYNCED — superseded by PRD §32, matching the TENANCY/PRD §16.5 lifecycle pattern; §5 event bullet documents the increment helper + the deliberate unset-vs-blank asymmetry; Q7 markdown-markers and Q8 increment-redaction rows added to the open-question record). FIX triage: 0 logged across the entire phase, 0 open, 0 carried forward. Full sweep green (see task checkboxes for timings).
