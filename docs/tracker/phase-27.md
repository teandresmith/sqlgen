# Phase 27: Nested Mutations — Create / Update / Upsert With Relationships

Status: Complete (closed 2026-10-02)
PRD Sections: 4.6, 4.8, 4.13, 9.2, 9.5, 9.9 (new), 13.1, 13.4, 13.7, 22.1, 22.3, 25.1, 26.5.1, 26.5.5, 26.12, 27.7, 28, 29.4, 29.10, 32.5

> **Design:** `docs/design/archive/NESTED_MUTATIONS.md` (PROPOSED 2026-09-10, re-verified against HEAD 2026-09-16)
> with companion `docs/design/archive/NESTED_MUTATIONS_EXAMPLES.md`. Ticket letters below map to that doc's §9.
>
> **Prerequisites outside this phase:** ~~**FIX-207** before 27.6, **FIX-208** before 27.9.~~
> **Both Resolved as of 2026-09-16** — neither gates 27.6 or 27.9 any longer. (Confirmed against
> `docs/tracker/fixes.md` during 27.3's closure; the note is kept struck rather than deleted so the
> dependency's history stays legible.)
>
> **Checkpoint:** 27.1–27.7 each ship value standalone. The end of 27.7 is the point at which the
> emitters (27.8–27.10) can be decided on fresh information.
>
> **27.0a lands the entire config surface before any consumer of it.** Added 2026-09-16 after the
> 27.0 review found `cmd/sqlgen/config/schema/v1.json` named by no sub-item while
> `TestSchemaV1_noDriftFromConfigStructs` fails on any Go config field with no schema property at
> its path. Every config key this phase introduces — four `Operations` toggles, both
> `nested_mutations` blocks, `relationships[].discriminator` — lands there in one go, along with
> the `relationshipDedupKey` component that 27.2's fixture migration needs and the five
> validation rules answerable from config text. It moves **zero goldens**, because
> `gen.ResolvedOperations` is a separate struct that no task here extends.

---

## 27.0 PRD sync — write the normative spec (P1–P29)

**PRD Reference:** §9.2, §9.5, §9.9 (new), §13.1, §13.4, §13.7, §4.6, §4.8, §4.13, §22.1, §22.3, §25.1, §26.5.1, §26.5.5, §26.12, §27.7, §28, §29.4, §29.10, §32.5

**Status:** **Complete** (landed 2026-09-16). 28 of P1–P29 landed; **P17 struck** (already in the PRD via FIX-196). Both settled open questions carried in: **Q9** → §25.1, **Q10** → §9.9.5. `docs/PRD.md` only, +502/−20. **All later sub-items are unblocked.**

### Tasks

- [x] **New §9.9 Nested Mutations** — normative home for the four write shapes, the eligibility matrix (E1–E12), the verb algebra (`create` / `connect` / `disconnect` / `clear`), the execution order, and **Q10**: declared order is **alphabetical by relationship name** (P2, P15)
- [x] **§9.2** — add `UpsertMany` and the three `…WithRelated` rows to the write-operations table, with the **C3** note on why the nested methods cannot fold into the existing ones (P1, P14, P19)
- [x] **§9.2 `Update` row** — state that `Update` honours the empty-`FieldOptions` skip like every sibling; the PRD is currently silent and the template currently disagrees (P16)
- [x] **§13.1** — split the O2O row into belongs-to / has-one; state that `FKOnTarget` is carried on the relationship context; add **FK nullability** as a first-class edge property, since it decides the verb set (P3, P18)
- [x] **§13.4 / §13.7** — introduce `discriminator: {column, value}`, its mutual exclusion with `filter:`, the rule that only `discriminator` edges are write-eligible, and **D18**: the edge owns its discriminator column on the write side (P4, P5, P26)
- [x] **§4.6 / §4.8** — the `nested_mutations` blocks and the four `…_with_related` / `upsert_many` operations-mask entries (P6)
- [x] **§4.13** — three new validation rows: `filter` × `discriminator` exclusion; `max_depth` accepts only 1 in v1; an explicitly-listed ineligible edge is a hard error; **plus D20** — a `…_with_related` operation enabled without its base operation (P7, P27)
- [x] **§22.1 / §22.3** — two new sentinels (`ErrAlreadyRelated`, `ErrNestedVerbConflict`) and the `*database.NestedMutationError{Edge, Verb, ID}` wrapper that unwraps to them (P22)
- [x] **§25.1** — extend the query-count guarantee to the write side: **`ceil(N / batchSize)` per participating table per verb, and exactly 1 for `clear`**. Carry **Q9**'s resolution and the §2.6 measurement behind it — SQLite's bind ceiling is 32766, PostgreSQL's and MySQL's 65535, and no dialect's error names a table, column, edge or verb (P8, P25)
- [x] **hook `MutationOp` list (PRD §7 constants, `docs/PRD.md:9723-9733`)** — add `OpUpsertMany`; state **D16**: the `…WithRelated` methods deliberately have **no** op of their own, and `OpUpsertMany` maps to the existing `event.Upsert` action, not a new wire value (P24)
- [x] **§26.5.1 / §26.5.5 / §26.12** — the three `…WithRelated` mutations in the curated surface; the `<Table>ConflictTarget` argument on `upsert<Table>WithRelated` only (**Q4**); `NOT_FOUND` (never `BAD_REFERENCE`) for a `connect` visibility miss; the v1 limitations list (P9, P10, P13, P20, P23)
- [x] **§29.4 / §29.10** — **NW-D13**: a `connect` is tenant-checked by reading the target through its own client, with the fail-closed statement §29.10 currently makes for reads only; plus the `SkipTenancy` note about nested children (P11, P12, P21)
- [x] **§32.5** — **E6** / **E7**: the access projection governs nested inputs, and a hidden entity gets no nested member (P11)
- [x] **§27.7 / §28** — the observability contract (P29): what fires and in what order, that a rolled-back savepoint emits nothing, that a relationship-loaded parent is never cacheable, and that nested creates deliberately do **not** warm the cache

### Acceptance Criteria

- Every one of **P1–P29** is either landed or explicitly struck with a reason recorded in this file (P17 is already struck — FIX-196 closed it).
- §9.9 exists and is self-contained enough that a reader who has not seen `docs/design/archive/NESTED_MUTATIONS.md` can derive the emitted verb set for any edge from the PRD alone.
- §25.1's write-side bound is stated as `ceil(N / batchSize)`, **not** a flat 1 — a flat 1 contradicts shipped `CreateMany` and is unachievable above SQLite's 32766-parameter ceiling.
- No code changes in this sub-item.

### Tests Required

- [x] None — documentation only. The gate is that every later sub-item cites a §-number that exists — verified: all 490 PRD headings resolve and the file now has **zero** dangling intra-document anchors.

### Completion Record

- **Date:** 2026-09-16
- **Files changed:** `docs/PRD.md` only (+502/−20, of which +486/−13 landed before the post-review round below). No code, template, config, or golden movement — `git diff --name-only HEAD` is `docs/PRD.md`, which matches no glob in the `/implement` path-based E2E trigger table, so `make check-examples` was correctly **not** run; the security-review trigger likewise did not fire (nothing under `manifest/` or `cmd/sqlgen/cli/**`).
- **Preflight:** clean. `gofumpt v0.12.0`, `golangci-lint 2.13.2`, `go 1.27.1` — all three match `.tool-versions` exactly, so the 16.8g drift class is not in play.
- **Tests:** `make check` clean (8 modules, 0 lint findings, all `-short -race` unit tests green; `cmd/sqlgen/gen` 48.9s, `cmd/sqlgen/cli` 34.5s).

**P1–P29 ledger.** 28 landed, 1 struck.

| P | Where it landed |
|---|---|
| **P1**, **P14**, **P19** | §9.2 write-operations table gains four rows — `UpsertMany`, `CreateWithRelated`, `UpdateWithRelated`, `UpsertWithRelated` — plus a closing paragraph carrying the **C3** rationale (`Create<T>Input` is shared with `Upsert`, so nested members there would be advertised and dropped). |
| **P2**, **P15** | **New §9.9 Nested Mutations**, ten subsections: the four write shapes, the FK-nullability axis, the eligibility matrix, **E1–E12**, the generated surface, the execution order, the query count, error attribution, observability, and tenancy/access. Placed after §9.8.13f, before §10. |
| **P3**, **P18** | §13.1 relationship table split into belongs-to / has-one with a new **FK lives on** column, plus two paragraphs: `FKOnTarget` is derived once and is *not* the `side:` field, and FK nullability is a first-class edge property that decides the write verb set. |
| **P4**, **P26** | New **§13.4.1 `discriminator:`** — the `{column, value}` form, reads byte-identical to the equivalent `filter:`, mutual exclusion with `filter:`, write-eligibility, and **D18**(a)(b)(c) as three numbered rules. `discriminator` + `RelationshipDiscriminator` rows added to §4.8's `TableRelationship` tables. |
| **P5** | §13.7 opening now says nested writes on a sub-categorized edge require the §13.4.1 form; the uninvertible `filter:` case stays readable and permanently non-nestable. |
| **P6** | §4.6 gains `upsert_many` + the three `…_with_related` toggles, a **"Every preset resolves every toggle"** paragraph pinning what each `ExpandPreset` arm must resolve (`append_only` row updated to name `CreateWithRelated`), and a new **Nested Mutations** block (`NestedMutationsConfig`: `enabled` / `operations` / `verbs` / `max_depth`). §4.8 gains the `nested_mutations` row plus a **"Nested mutations per table"** subsection (`TableNestedMutationsConfig`, `TableNestedRelationship` with `allow_reparent`, and **NW-Q2**'s omit-to-include-everything default). |
| **P7**, **P27** | §4.13 gains **six** rows, not three: `filter` × `discriminator` exclusion; `discriminator.column` must exist on the related table (needed to make §13.4.1's "must exist" claim normative); `max_depth` ≠ 1; unknown value in `nested_mutations.operations` / `verbs`; **D20** (`…_with_related` without its base op); and an explicitly-listed ineligible edge as a resolution-level error, with the auto-included half stated as the deliberate asymmetry. |
| **P8**, **P25** | §25.1 gains a **"The write side"** subsection: `ceil(N / batch_size)` per participating table per verb, exactly 1 for `clear`, the per-verb (not per-table) reason, and **Q9**'s resolution with the §2.6 measurement — 32,766 / 65,535 / 65,535 per dialect, and the note that no dialect's error names a table, column, edge or verb, so §9.9.8 could not attribute it. |
| **P9**, **P19**, **P23** | §26.5.1 gains three rows in the per-table surface table and a **"Nested mutations on the GraphQL surface"** subsection: additive-only (the flat three stay byte-identical), the C3 non-folding reason, the `conflictTarget` argument on the nested upsert only with **Q4**'s scope rationale, `extensions.path`, and the **E7** hidden-entity rule. |
| **P10**, **P20** | §26.12 gains three limitations: the v1 out-of-scope list (no `delete` verb, belongs-to, depth > 1, re-parenting without `allow_reparent`, `createMany`/`updateMany` nesting, NOT NULL FK `connect`/`disconnect`, `filter:` edges, payload-carrying junctions); the self-referential ancestor cycle that depth-1 cannot see; and the deliberate flat-vs-nested upsert conflict asymmetry. |
| **P11** | §32.5 gains a Nested mutations bullet carrying **E6** and **E7**, and stating that the nested child input is derived from the *projected* create input rather than from the raw column list. |
| **P12**, **P21** | New **§29.4.2a Nested mutations** — the `connect` tenant-check via the target's own client with the fail-closed statement, the `disconnect` asymmetry, `SkipTenancy` propagation into nested children, and an explicit paragraph that the visibility read is call-time validation and takes no lock. §29.10 gains a matching write-side bullet. |
| **P13** | §26.5.5 gains two table rows (`ErrAlreadyRelated` → `CONFLICT`, `ErrNestedVerbConflict` → `INVALID_INPUT`) and a **"Nested-mutation mappings"** paragraph pinning `NOT_FOUND` (never `BAD_REFERENCE`) for a visibility miss, with the oracle reason. |
| **P16** | §9.2's `Update` row now states the empty-`FieldOptions` skip, **and** the §9.8.6 and §9.8.13c code samples were corrected to carry the guard on both return paths (the empty-update short-circuit and the post-`UPDATE` fetch), with a closing paragraph saying why it is load-bearing for §9.9. This is what 27.3 implements; the PRD previously disagreed with five sibling templates. |
| **P17** | **STRUCK.** §9.5 already documents the conflict-target-covers-every-column branch and §9.7 the PK-resolution invariant — both landed with **FIX-196** (`effc99d`). Re-read at HEAD before striking: the §9.5 "Conflict targets that cover every inserted column" paragraph and its §9.7 pointer are present and correct. No delta remained. |
| **P22** | §22.1 gains `ErrAlreadyRelated` and `ErrNestedVerbConflict` plus a sentence on the runtime-declares / generated-package-re-exports relationship; §22.2 gains the `NestedMutationError{Edge, Verb, ID, Err}` struct with its `Unwrap`; §22.3 gains the nested wrapping pattern and the statement that message and struct never disagree. |
| **P24** | `OpUpsertMany` added to the §21.2 `MutationOp` constant list, with **D16** stated below the block: it maps to the existing `event.Upsert` action (not a new wire value), every consumer switch must name it explicitly because none fails to compile without it, and the three `…WithRelated` methods deliberately have no op. |
| **P28** | **D19** (a parent with zero eligible edges emits no nested surface at all) is in §9.9.4; **D21** (a nil `FieldOptions` returns relationship members unpopulated) is in §9.9.5. |
| **P29** | **§9.9.9 Observability** states all four: what fires and in what order, a rolled-back savepoint emits nothing, a relationship-loaded parent is never cacheable, and nested creates deliberately do not warm the cache. Mirrored where a reader would look for each — §27.7 gains two table rows (`UpsertMany`, and the three `…WithRelated` methods dispatching nothing of their own) plus a paragraph on the two cache asymmetries; §28.9 gains a paragraph on nested publication order, `OnCommit` FIFO after the **root** commit, savepoint-rollback discard, and the per-row redaction requirement for `OpUpsertMany`. |

**Settled open questions carried in.** **Q9** (batch at `batch_size`) is normative in §25.1 with its measurement. **Q10** (declared order = alphabetical by relationship name) is normative in §9.9.5, stated with the three places it is observable — error attribution, event enqueue order, and generated member order — so it is a contract rather than an artifact.

**Deliberately left to 27.5, per this file's own split.** §9.5's three `UpsertMany` decisions — the `[]*T` return contract on the `DO NOTHING` branch, the MySQL db-generated-PK contract, and the `AffectedPKs`-from-inputs invariant — are 27.5's tasks and its acceptance criterion ("documented in the template and in §9.5"), and 27.0's task list does not claim them. What 27.0 *did* land is the one decision the design had already settled by measurement: §9.2's `UpsertMany` row states **dedupe by conflict target, last occurrence wins**, with the one-dialect-divergence reason, so 27.5 cannot pick first-wins by accident. The §9.2 row deliberately makes no promise about return-slice length or PK resolution, leaving both decisions open without a contradiction to unwind.

**Two things fixed in place beyond the P-list** (both small, both in text this sub-item was already editing):
- §22.1 was missing `ErrRefreshConcurrentlyInTx`, which has shipped in `database/errors.go` since Phase 21. The section claims to enumerate the sentinel set, so adding two sentinels while a third stayed missing would have made the omission look deliberate.
- Four pre-existing dangling intra-PRD anchors repaired: two `#85-column-overrides` → `#column-overrides` (the `import` rule lives in §4.8, not §8.5), `#413-reserved-and-generated-names` → `#413-config-validation-rules`, and `#10-error-handling` → `#22-error-handling`. The PRD now has **zero** dangling anchors, verified with a GitHub-accurate slugger over all 490 headings.

**Post-review round (2026-09-16).** The `sqlgen-reviewer` pass raised 7 items. **All 7 are real and all 7 are fixed in place**; three of them were defects in text this sub-item wrote, three were pre-existing PRD staleness the new text would otherwise have cited, and one was a missing delta with teeth.

- **A missing P-delta — the relationship dedup key (blocking for 27.2).** §13.7.1's key is `(target_table, fk_column, filter)`, implemented as such (`cmd/sqlgen/config/validate.go:421-430`). A `discriminator:` edge contributes `filter == ""`, so 27.2's own task — migrate `Attachments`, `Invoices` and `PrimaryDocument`, all `documents` / `entity_id` — would have collapsed three edges onto one key and hard-errored as duplicate relationships **on the first run**. §13.7.1 now specifies a **discriminator component** in both key shapes, with the reason stated (a sub-categorized edge shares its `(target, fk)` prefix by definition, so the discriminating value *is* the key), and §13.7.3's validation row covers both forms. A task was added to 27.2 naming the function.
- **§25.1 named the wrong method.** "the target `UpsertMany`" contradicted §9.9.6 and design §4.3, which write M2M targets with `CreateMany` and use `UpsertMany` only for the junction. Corrected.
- **§22.1's re-export sentence was a blanket claim that is false.** `cmd/sqlgen/gen/templates/error.go.tmpl` re-exports exactly 8 aliases and does not include `ErrRefreshConcurrentlyInTx`. The sentence now states what is actually true — the generated package re-exports the sentinels its own surface can return — and names the exception. The reviewer also caught that **no sub-item owned adding the new aliases to that template**; a task was added to 27.7, since the template's list is written out explicitly and does not grow on its own.
- **§9.5 contradicted the shipped conflict-target gate, and E10 cited it.** "The PK is always a valid conflict target" predates `336f591`'s `pkConflictTargetIsIndexed` (`cmd/sqlgen/gen/context_table.go:1549`, `:1630-1638`), which emits no `{Table}ConflictPK` for a PK declared only through `primary_key.columns` with no backing index. §9.5 now states the rule accurately and points forward to **E10**, which was already right about the code.
- **`upsert_many` had no documented API projection — adjudicated.** Design §7's *"plus matching `apiOperationFields` entries"* reads ambiguously across all four new toggles, but no batch-upsert mutation exists anywhere in the design or in §26.5.1, so the only coherent reading is that `upsert_many` is **client-only**. It now appears in §26.5.1's exclusion table with its rationale and in §26.10's closed list beside `exists` / `count` / `increment` / `stream` / `update_where`. The three `…_with_related` toggles **do** have projections and stay maskable. Recorded here as an adjudication rather than a transcription, since the design does not settle it.
- **§4.6's preset wording invited the bug it was warning about.** "The two delete presets do not touch any of the four" could be read as leaving them nil, which is exactly the failure the paragraph exists to prevent. Rewritten to "resolve all four to the same values `all` does".
- **Two nits.** "Five type families" sat above a six-row table (fixed), and §9.9.5 never said which input type an **M2M** `create` takes — it is the target's ordinary `Create<Target>Input`, since there is no traversed FK on the target to elide (design §5.2). Stated explicitly.

**One regression risk the reviewer surfaced that is not a 27.0 defect — and it produced a new sub-item.** `cmd/sqlgen/config/schema/v1.json` is `additionalProperties: false`, and `TestSchemaV1_noDriftFromConfigStructs` (`cmd/sqlgen/config/schema_test.go:309-395`) is reflection-driven and **path-aware**, so it fails on any Go config field with no schema property at its path — yet no sub-item mentioned the file, and the config keys were split across 27.2, 27.5 and 27.7, paying that tax three times for one reason. A structural scan run afterwards sharpened it: the two failure modes **disagree**. `generation.nested_mutations`, `tables.<t>.nested_mutations` and `relationships[].discriminator` hard-error under `KnownFields(true)`, while `operations.*` keys are **silently accepted and dropped**, because `Operations` defines its own `UnmarshalYAML` (`config.go:434`) and `yaml.Node.Decode` builds a fresh non-strict decoder the setting cannot reach — a gap the function's own comment documents at `config.go:1441-1444`. So a consumer writing `operations.create_with_related: true` before 27.7 lands would get no error and no method.

The config work was therefore pulled out of 27.2 / 27.5 / 27.7 into **27.0a**, which lands the whole surface once, before any consumer of it. The same scan established its acceptance criterion: **zero golden movement**, because all eleven consumers of `config.Operations` are hand-written field lists, the manifest emits no `operations` key and its `Relationship` struct has no `discriminator` slot, MCP reads none of it, and `gen.ResolvedOperations` (`gen/context.go:530-548`) is a separate 17-bool struct that 27.0a deliberately does not extend. It also caught three silent-gap landmines no ticket had listed — `applyOperationOverrides` (a user's explicit toggle is discarded without an arm), the `apiOperationFields` / `clientOnlyOperationFields` split (a field in neither is accepted inside `api.operations` and ignored), and `ExpandPreset` leaving an unnamed field **nil** rather than false, which `TestExpandPreset` turns into a nil-pointer panic rather than a clean failure — and corrected a count this tracker, `docs/tracker/IMPLEMENTATION_ORDER.md` and the design doc all got wrong: `ExpandPreset` has **five** value-returning arms, not six (`all`/`""` is one arm covering two spellings, plus a `default` that errors).

27.5 keeps the golden-movement note: `upsert_many` defaults to `true`, so `UpsertMany` is emitted on every table once **27.5** lands the template. The flag itself lands inert in 27.0a, so the movement belongs to 27.5 and nowhere earlier — the one place in 27.1–27.7 where "zero golden movement" does not apply.

**Re-verified after the round:** all 490 PRD headings resolve, **zero** dangling anchors, code-fence parity intact, `make check` still clean.

**Notes.** §9.9 is written to be self-contained: a reader who has never seen `docs/design/archive/NESTED_MUTATIONS.md` can derive the emitted verb set for any edge from §9.9.2 (the FK-nullability axis) plus §9.9.3 (the matrix) plus §9.9.4 (**E1–E12**) alone. *(27.8 added **E13** and amended E2, E8 and E9 — the claim still holds, over a longer rule list.)* Placement choices worth recording: §9.9 is a new `### ` under §9 rather than a top-level section, so no existing §-number moved and the top-level TOC needed no edit; §13.4.1 is a numbered `#### ` because §13.4 had no subsections to renumber; §29.4.2a follows the existing `9.4a` / `9.6a` / `9.8.6a` precedent for inserting without renumbering; and §26.5.1's and §25.1's additions are unnumbered `##### ` / `#### ` blocks, following 25.0's precedent of not renumbering anchors other sections cite.

---

## 27.0a Config surface + JSON schema — every key, one landing

**PRD Reference:** §4.6, §4.8, §4.13, §13.4.1, §13.7.1, §26.5.1, §26.10

**Status:** **Complete** (landed 2026-09-17)
**Depends on:** 27.0 (the PRD sync that made all of this normative). **Blocks 27.2, 27.5, 27.7, 27.8 — all now unblocked.**

> **Why this exists as its own sub-item.** The config keys were originally scattered across 27.2
> (`discriminator`), 27.5 (`upsert_many`) and 27.7 (the three `…_with_related` toggles and both
> `nested_mutations` blocks), with `cmd/sqlgen/config/schema/v1.json` named by none of them. That
> splits one mechanical landing into three, and each of the three pays the same tax:
> `TestSchemaV1_noDriftFromConfigStructs` is **reflection-driven and path-aware**
> (`cmd/sqlgen/config/schema_test.go:309-395`), so it fails the moment a Go field appears with no
> schema property at its path — three times, in three sub-items, for the same reason. Landing the
> whole config surface once removes that, and it means every later sub-item starts from a config
> that already parses.
>
> **The failure mode it prevents is worse than a test failure, and it is silent.** `decodeConfig`
> uses `KnownFields(true)` (`config.go:1447`), but `Operations` defines its own `UnmarshalYAML`
> (`:434`), and `yaml.Node.Decode` builds a fresh non-strict decoder that the setting cannot reach
> — documented in the function's own comment at `config.go:1441-1444`. So a consumer who writes
> `operations.create_with_related: true` today gets **no error and no method**: the key is accepted
> and dropped. The two `nested_mutations` blocks and `relationships[].discriminator` do hard-error
> (their parent structs have no custom unmarshaler), so the phase currently has two different
> failure modes for the same class of missing field.

### Tasks

- [x] **`config.Operations` gains four fields** — `UpsertMany`, `CreateWithRelated`, `UpdateWithRelated`, `UpsertWithRelated` (`cmd/sqlgen/config/config.go:411-430`), PRD §4.6
- [x] **`ExpandPreset` — all five arms**, not six (`config.go:1322-1374`). The tracker and `docs/tracker/IMPLEMENTATION_ORDER.md` both said "six"; there are **five value-returning arms** (`all`/`""` is one arm covering two spellings) plus a `default` that errors. Per-arm: `all` → all four true; `read_only` → all four false; `append_only` → `create_with_related` true and the other three false (it disables update and upsert); `no_delete` / `no_hard_delete` → the same values `all` uses. **A field an arm does not name is `nil`, not `false`** — and `TestExpandPreset` (`config_test.go:786-893`) dereferences, so the symptom is a nil-pointer panic rather than a clean assertion failure
- [x] **`applyOperationOverrides`** (`config.go:1388-1406`) — without an arm here a user's explicit toggle is silently discarded by `ResolveOperations`
- [x] **`apiOperationFields` / `clientOnlyOperationFields`** (`config.go:747-772`) — the three `…_with_related` go in the first (they have GraphQL projections, PRD §26.5.1); **`upsert_many` goes in the second** (adjudicated client-only in 27.0 — no batch-upsert mutation exists, PRD §26.5.1 exclusion table + §26.10 closed list). A field in **neither** list is accepted inside an `api.operations` block and then silently ignored, which is the exact failure those two tables exist to prevent
- [x] **`hasAnyOperationToggle`** (`config.go:1781-1792`) — benign today (both paths land on `PresetAll`), included so the enumeration stays complete
- [x] **`NestedMutationsConfig`** on `GenerationConfig` (`enabled`, `operations`, `verbs`, `max_depth`), PRD §4.6
- [x] **`TableNestedMutationsConfig`** + **`TableNestedRelationship`** (`name`, `allow_reparent`) on `TableConfig`, PRD §4.8
- [x] **`RelationshipDiscriminator`** + `TableRelationship.Discriminator` (`config.go:562-576`), PRD §4.8 / §13.4.1
- [x] **`relationshipDedupKey` gains a discriminator component** (`cmd/sqlgen/config/validate.go:421-430`), PRD §13.7.1 — the key is `(target, fk, filter)` and every `discriminator:` edge carries `filter == ""`, so 27.2's fixture migration would collapse three `documents`/`entity_id` edges onto one key and hard-error as duplicate relationships. **This is the one task here that is load-bearing rather than mechanical**
- [x] **Five validation rules** answerable from config text alone, PRD §4.13: `filter` × `discriminator` exclusion; `discriminator.column` exists on the related table (schema-level); `max_depth` ≠ 1; unknown value in `nested_mutations.operations` / `verbs`; and **D20** — a `…_with_related` toggle enabled without its base operation. *(The sixth §4.13 row — an explicitly-listed ineligible edge — is resolution-level and belongs to 27.8 with the eligibility lint.)*
- [x] **`cmd/sqlgen/config/schema/v1.json`** — four `$defs` nodes are `additionalProperties: false` and each needs a key: `#/$defs/operationsObject` (four toggles), `#/$defs/generation/properties/nested_mutations`, `#/$defs/table/properties/nested_mutations`, `#/$defs/relationship/properties/discriminator`, plus the three new `$defs` they reference

### Acceptance Criteria

- **`TestSchemaV1_noDriftFromConfigStructs` is green.** It is the gate for this sub-item: path-aware, so `nested_mutations` declared under `generation` does **not** satisfy `tables.<key>.nested_mutations`. The `Operations` struct is re-checked at each distinct path (`.generation.operations`, `.tables.<key>.operations`, `.api.operations`, `.tables.<key>.api.operations`, `.views.<key>.api.operations`), but all resolve through one `$ref`, so a single `operationsObject` edit clears all five.
- **Zero golden movement across all 12 example modules.** Verified structurally before the sub-item was written: nothing enumerates `config.Operations` or `config.TableRelationship` generically — every one of the eleven consumers is a hand-written field list, the manifest emits no `operations` key at all, `manifest.Relationship` is a fixed six-field struct with no `discriminator` slot, and MCP reads none of it. `gen.ResolvedOperations` (`gen/context.go:530-548`) is a **separate** struct that must be extended by hand, and it is not extended here — that is the firewall that keeps output still.
- Each of the **five** `ExpandPreset` arms resolves all four new fields non-nil.
- `generation.nested_mutations`, `tables.<t>.nested_mutations` and `relationships[].discriminator` all load without error under `KnownFields(true)`.
- Two `discriminator:` edges over the same `(target, fk)` with different values are **accepted** (the half this sub-item owns). They do not yet *produce* sub-categorized fields — the read path is 27.2's; see the completion record.
- No template, no context builder, no generated output — this sub-item adds config the generator does not yet read.

### Tests Required

- [x] Unit: extend `TestExpandPreset` so each of the five presets asserts all four new fields non-nil with the values above
- [x] Failing-first: `filter` + `discriminator` on one relationship → validation error naming both keys
- [x] Failing-first: `discriminator.column` naming a column the related table does not declare → validation error naming relationship, column and table
- [x] Failing-first: `create_with_related: true` with `create: false` → validation error naming the key (**D20**); same for update and upsert
- [x] Failing-first: `nested_mutations.max_depth: 2` → validation error
- [x] Failing-first: unknown value in `nested_mutations.verbs` → validation error
- [x] **Failing-first (the dedup key)**: two `discriminator:` edges into one table on one FK with different values are accepted. This fails today as a duplicate-relationship error, which is the whole point
- [x] `TestSchemaV1_noDriftFromConfigStructs` green; `TestLoadConfig_RejectsUnknownKeys` and `TestSchemaV1_acceptsExampleConfigs` still green
- [x] Golden: `make check-examples` — full regeneration byte-identical across all 12 modules. **The path trigger fires** (`cmd/sqlgen/config/types.go`, `cmd/sqlgen/config/validate.go`), so this is mandatory, not optional

### Completion Record

- **Date:** 2026-09-17
- **Files changed (10):** `cmd/sqlgen/config/config.go`, `cmd/sqlgen/config/validate.go`,
  `cmd/sqlgen/config/schema/v1.json`, **new** `cmd/sqlgen/config/nested_mutations_validate.go`,
  **new** `cmd/sqlgen/config/nested_mutations_test.go`, four test files extended in place
  (`config_test.go`, `api_operations_test.go`, `view_api_test.go`, `schema_test.go`), and
  `docs/configuration.md`. **No template, no context builder, no golden.**
- **Preflight:** clean. `gofumpt v0.12.0`, `golangci-lint 2.13.2`, `go 1.27.1` — all three match
  `.tool-versions` exactly, so the 16.8g drift class is not in play.
- **Tests:** `make check` clean (8 modules, 0 lint findings, all `-short -race` unit tests green).
  **`make check-examples` green and the working tree unchanged afterwards** — zero golden movement
  across all 12 example modules, as predicted. The path trigger fired on
  `cmd/sqlgen/config/validate.go`; the security-review trigger did not (nothing under `manifest/`
  or `cmd/sqlgen/cli/**`).

**What landed.**

| Task | Where |
|---|---|
| Four `Operations` toggles | `UpsertMany` sits beside `Upsert`; the three `…WithRelated` are a separate trailing group with a comment tying each to the base it composes. PRD §4.6 field order preserved. |
| **Five** `ExpandPreset` arms | `all` / `""` → all four true; `read_only` → all four false; `append_only` → `create_with_related` alone; `no_delete` and `no_hard_delete` → the same values `all` uses. The doc comment states the rule the arms exist to satisfy: a field an arm forgets stays nil, and every consumer dereferences. |
| `applyOperationOverrides`, `hasAnyOperationToggle` | Four entries each. |
| `apiOperationFields` / `clientOnlyOperationFields` | The three `…_with_related` join the API list (twelve → fifteen); `upsert_many` joins the client-only list. `mutationAPIOperationFields` derives from the first, so views reject all three automatically — no second list to keep in sync. |
| `NestedMutationsConfig` on `GenerationConfig` | `enabled` / `operations` / `verbs` / `max_depth`. `MaxDepth` is `*int`, not `int`: 1 is both the default and the only accepted value, so a plain int would make an omitted key read as 0 — which validation rejects. |
| `TableNestedMutationsConfig` + `TableNestedRelationship` | On `TableConfig`. `AllowReparent` is a plain `bool` (no global counterpart to inherit from, so no tri-state to express). |
| `RelationshipDiscriminator` + `TableRelationship.Discriminator` | `Value` is typed `any`, not `string` — the PRD types it **scalar** in both §4.8 and §13.4.1, contrasting deliberately with `column`'s `string`. |
| **`relationshipDedupKey` discriminator component** | `discriminatorDedupComponent` renders `(column, value)` as a fixed two-field group, so a nil discriminator contributes two empty fields rather than none and a `filter:` edge can never alias onto a `discriminator:` one. The value is rendered `%T:%v`, so `value: 1` and `value: "1"` stay distinct declarations. |
| Five §4.13 validation rules | `filter` × `discriminator` exclusion in `validateRelationships` (beside the dedup key it shares a subject with); the other four in the new `nested_mutations_validate.go` — `max_depth` ≠ 1, the two closed sets, and **D20**. The discriminator column-existence rule is schema-level and runs post-parse. |
| `schema/v1.json` | Four toggles on `operationsObject`; `generation.nested_mutations`, `tables.<t>.nested_mutations`, `relationships[].discriminator`; four new `$defs` (`nestedMutations`, `relationshipDiscriminator`, `tableNestedMutations`, `tableNestedRelationship` — the last split out to match the existing `relationshipSort` house style rather than inlined under `items`). |

**Two judgment calls worth recording.**

1. **D20 fires on an *explicitly set* `…_with_related: true`, not on the resolved pair.** Reading it
   as the resolved pair would reject `operations: {create: false}` under the default `all` preset,
   because `all` resolves `create_with_related` true — punishing every config that disables a base
   operation while never mentioning nested mutations. §4.13's own rationale is *"the user set a flag
   and got nothing with no signal"*, which is the explicit reading. Presets never trip the rule on
   their own, since each arm resolves the nested toggle to follow its base. `api.operations` is
   exempt entirely: a mask is subtractive over the client's surface (§26.5.1), so `create: false`
   there does not take `Create` away from the generated package. Both halves are pinned by tests.
   *(The `api.operations` exemption was superseded by FIX-229: 27.10 made the mask conjoin each
   `…_with_related` toggle with its base, so D20 now covers API masks too.)*
2. **A `discriminator:` edge whose target table is absent from the parsed schema is skipped, not
   reported.** The missing-target diagnosis belongs to relationship resolution; naming the
   discriminator for a problem the discriminator does not have would stack a second, misleading
   error on the first.

**One bounded interval this sub-item deliberately opens.** Before it, `discriminator:` hard-errored
under `KnownFields(true)`; now it parses and validates but `configRelationshipToContext`
(`cmd/sqlgen/gen/context_table.go:1452-1479`) still copies only `Filter` and has no discriminator
arm — so a `discriminator:` edge currently generates an **unfiltered** loader. That is the
accept-and-drop shape this sub-item's own rationale objects to, and it is tolerable only because it
is closed by **27.2**, which is next and unblocked, and because no shipped config or fixture uses
the key yet. It is the reason the acceptance line above was narrowed to "accepted" rather than
"accepted and produce sub-categorized fields".

**Post-review additions.** The auto-review surfaced two code gaps beyond the task list, both fixed
in place. `discriminator.column`, `discriminator.value` and `nested_mutations.relationships[].name`
are marked **Required** in PRD §4.8 / §13.4.1 and were enforced by `schema/v1.json` but **not** by
Go — the loader was laxer than the schema it ships, which is the editor-paints-it-red asymmetry in
reverse. `validateNestedRequiredKeys` closes it; the column-existence rule now defers the empty-column
case to it, so the user reads "required" rather than "not a column on the related table". A missing
`value` had teeth beyond the asymmetry: it renders `<nil>:<nil>` in the dedup key, so two value-less
edges would have collided as duplicates over a value neither declares. Separately,
`TestSchemaV1_acceptsUnexercisedSurface` gained six positive cases — the drift test checks *presence*,
not shape, and no example config uses any new key, so nothing validated an actual document against
the three new `$defs`.

**Failing-first verified by reverting each rule in turn** (dedup component, `filter` × `discriminator`
exclusion, the nested-mutations block, the discriminator column check): every corresponding test
fails without its rule. The schema drift test was checked the same way — deleting `upsert_many` and
`tables.<key>.nested_mutations` from `v1.json` reports six missing paths across all five
`Operations` `$ref` sites, confirming it is the real gate this sub-item exists to clear.

---

## 27.1 `FKOnTarget` hoist onto `RelationshipContext` (Ticket A)

**PRD Reference:** §13.1

**Status:** **Complete** (landed 2026-09-17)

### Tasks

- [x] Add `FKOnTarget bool` to `RelationshipContext` (`cmd/sqlgen/gen/context.go:281-362`), computed once in the relationship builder
- [x] Rewrite `buildO2OJoinDetails` (`cmd/sqlgen/gen/context_table.go:2328-2337`) to read the field instead of re-deriving it by column scan
- [x] Confirm the fact is derived in exactly one place afterwards (`grep` for the column-scan pattern)

### Acceptance Criteria

- **Zero golden movement** across all 12 example modules — this is a refactor of where a fact is computed, not of what it is.
- `Side` is untouched. It answers a different question: `addO2O` and `addO2M` both hard-code `SideParent` (`parser/relationship.go:182`, `:197`) while a config edge can put the FK on the target, which is exactly the `assets` fixture's shape.

### Tests Required

- [x] Unit: an auto-detected O2O edge resolves `FKOnTarget == false`; the config-declared `assets.PrimaryDocument` edge resolves `true`
- [x] Golden: full regeneration is byte-identical
- [x] Unit (added): a **chained** O2O whose second hop holds its FK on the target keeps the right `ON` sides — the one shape the obvious placement of the derivation gets wrong

### Completion Record

- **Date:** 2026-09-17
- **Commit:** `3634147` — `refactor(gen): derive FKOnTarget once, on the relationship (27.1)`
- **Files changed:** `cmd/sqlgen/gen/context.go` (the field + its doc), `cmd/sqlgen/gen/context_table.go` (the derivation, the threading, the read-path rewrite), `cmd/sqlgen/gen/context_test.go` (two unit pins). No template, config, schema or golden file moved.
- **Preflight:** clean. `gofumpt v0.12.0`, `golangci-lint 2.13.2`, `go 1.27.1` — all three match `.tool-versions`, so the 16.8g drift class is not in play.
- **Tests:** `make check` clean (8 modules, 0 lint findings). `make check-examples` run and green (exit 0) because `cmd/sqlgen/gen/**` matches the path-based E2E trigger; the security-review trigger did not fire (nothing under `manifest/` or `cmd/sqlgen/cli/**`). `git status --porcelain` after both sweeps lists only the three source files — **zero golden movement, confirmed mechanically** rather than asserted.

**The derivation is one function with one caller pair.** `fkColumnOnTarget(schema, relType, targetSchema, targetTable, fkColumn)` reads the *parsed* schema and answers whether the edge's FK column is declared on the target table. It is called from exactly the two places a `RelationshipContext` is born — `relationshipToContext` (FK-inferred edges) and `configRelationshipToContext` (`tables.<t>.relationships` edges) — and its answer is read in exactly one place, the O2O JOIN's `ON` clause. `grep` for the retired column-scan pattern (`== rel.FKColumn`, `fkOnTarget`) across `cmd/`, `parser/`, `sql/` and `database/` returns nothing outside the helper.

**Both constructors needed the schema threaded in, and that is what made the hoist non-trivial.** Neither took a `*parser.Schema`, so three signatures changed: `relationshipToContext`, `configRelationshipToContext` and `mergeDeclaredRelationships`. The second call site of each matters — `findO2ORelationships` (`context_table.go:2404`) builds the contexts for **chained** O2O joins by calling the two constructors *directly*, bypassing `buildRelationshipContexts`. Setting the field only in `buildRelationshipContexts` — the obvious reading of "computed once in the relationship builder" — leaves every chained join at the zero value and **inverts its `ON` clause** the moment a chain crosses an FK-on-target edge. Deriving inside the constructors covers both entry points by construction.

**That is measured, not argued.** The wrong placement was implemented and run: `TestBuildO2OJoinDetails_chainedFKOnTarget` fails on it with `customers → addresses ON = customer_id/id, want id/customer_id`, and `TestE2EGoldenFiles` fails too — the examples already exercise a chained O2O, so the regression would have been caught at the sweep rather than shipped. The unit pin is the faster and more legible of the two guards, not the only one; it names the shape instead of reporting a 57 KB golden diff.

**Per-type semantics, stated on the field rather than left to the reader.** O2M resolves `true` (the FK is on the many side), M2M resolves `false` (both FKs live on the junction, so neither *end* of the edge holds one — and auto-detected M2M edges leave `FKColumn` empty anyway), and the O2O split is the belongs-to / has-one distinction §13.1 names. A self-referential O2O resolves `true` because source and target are one table; that is the answer the read path has always produced for the shape, and it is harmless there — both sides of the `ON` clause are the same table's FK and PK either way. It is called out in the helper's doc comment so a write-side consumer does not inherit it as a surprise.

**`wireRelationshipFKMetadata` was deliberately left alone.** Its O2O arm probes the target's generated column set and falls back to the parent, which looks like the same question but is not: it resolves the FK column's *resolved nullability, Go type and Go field name*, so it must read the generated `ColumnContext` (post `column_map`, post `exclude_columns`), not the parsed schema. Rewiring it to dispatch on `FKOnTarget` would swap one lookup domain for another and could move `FKNullable` on any edge whose target table or FK column is outside the generated set — a golden-moving change with no payoff for this ticket. The distinction is recorded on the `FKOnTarget` doc comment so the next reader does not "fix" it.

**The auto-review found no blocking item; four notes, all resolved in place.** It independently reproduced the chained-path hazard — it read the tree mid-probe, while the wrong placement was installed, and reported the moved golden itself. Two were doc accuracy and are fixed: the helper's comment claimed a schema-qualified config `table:` "misses here the same way it misses everywhere else downstream", which is **false** — `markSyntheticFK` and 27.0a's discriminator validation both split it, so the two layers genuinely disagree; and the struct doc's flat "O2M resolves true" ignored that an unresolved target also yields `false`, making `false` three answers in one. Both comments now say so, and the behavior is deliberately unchanged: splitting only inside this helper would make one field resolve against a table `TargetTable`, `TargetStructName` and the join builder cannot name. One was a test nit, fixed: the `rel()` lookup closed over the parent `*testing.T` and would have `Fatalf`'d the wrong test from inside a subtest. The fourth was a forward risk with no action available here — **booked as two tasks on 27.8** rather than as FIX entries: the write branch must not read `FKOnTarget == false` as "belongs-to" (the unresolved-target arm), and the `guidelines/TESTING.md` §11 fixture rule needs extending the moment a template reads the field, or ~a dozen hand-built `RelationshipContext` fixtures silently take the zero value — the FIX-059 / FIX-068 / FIX-069 shape. The reviewer also confirmed, against the excluded-table path rather than by assertion, that leaving `wireRelFKMetadata` alone is correct.

**Tests.** Two, both in `context_test.go`. `TestBuildTableContexts_relationshipFKOnTarget` builds one schema through the *real* `parser.DetectRelationships` path — not hand-written `parser.Relationship` literals — and pins four cases table-driven: auto-detected O2O (`profiles.Users`, unique FK on the source) → `false`; the config-declared `assets.PrimaryDocument` edge, spelled exactly as the graphql example spells it including its `discriminator:` → `true`; O2M (`users.Posts`) → `true`; M2M (`posts.Tags`, through a `post_tags` junction with a composite-PK constraint) → `false`. It closes with the acceptance criterion's second half: both O2O edges carry `Side == SideParent` while disagreeing on `FKOnTarget`, which is the single assertion that proves `Side` cannot stand in for it. `TestBuildO2OJoinDetails_chainedFKOnTarget` is the chained pin described above — it asserts both hops' `ON` sides on an `orders → customers → addresses` chain whose second hop is config-declared with its FK on the target.

---

## 27.2 `discriminator:` read path (Ticket B)

**PRD Reference:** §13.4, §13.4.1, §13.7, §13.7.1, §4.13

**Status:** **Complete** (landed 2026-09-17). Ticket B's write-side half (**D18 a/b/c**) moved to 27.8.
**Depends on:** 27.0a (the config struct, the validation rule, the dedup-key component and the JSON-schema entry all land there)

### Tasks

- [ ] ~~Add `Discriminator *RelationshipDiscriminator` to `config.TableRelationship`~~ — **moved to 27.0a**, together with the `filter` × `discriminator` validation rule, the `relationshipDedupKey` discriminator component and the `schema/v1.json` entry. This sub-item starts from a config that already parses
- [x] Read path: compile a discriminator into the same equality predicate the equivalent `filter:` produced — on all three read paths (loader, relationship-filter `EXISTS`, O2O JOIN `ON`)
- [x] Carry the discriminator onto `RelationshipContext` and `RelationshipFilterContext`, so the write-side emitters have the data D18 needs
- [x] Migrate the `assets` fixture's invertible edges from `filter:` to `discriminator:`; leave the uninvertible ones (`PhotoAttachments`, `PrimaryActiveDocument`, `LinkedAttachments`) on `filter:`
- [x] Manifest reports a discriminator edge's predicate in the existing `filter` field, so migrating an edge is invisible to the manifest and its frozen schema gains no key
- [ ] ~~**D18(a)** — a nested `create` SETs the discriminator column to the edge's declared value~~ — **moved to 27.8** (2026-09-17)
- [ ] ~~**D18(b)** — the discriminator column is elided from the nested child input, as **C2** elides the traversed FK~~ — **moved to 27.8** (2026-09-17; already listed verbatim in 27.8's task list)
- [ ] ~~**D18(c)** — a `connect`'s visibility read carries the discriminator as an extra predicate~~ — **moved to 27.8** (2026-09-17)

> **Why D18 moved.** All three rules are properties of the *nested child input* and the *`connect`
> executor* — types that 27.7/27.8 introduce and that do not exist at 27.2's HEAD. PRD §13.4.1 is
> explicit that the elision is "a property of the *nested* child input, which is a distinct type",
> and writes through the child's own client keep both columns. Building a throwaway nested surface
> inside 27.2 to make them testable would front-load 27.8's design decisions (NW-D4 containment, the
> per-edge executor shape) into a sub-item scoped as config-plus-read-path. 27.2 instead lands the
> data those rules consume — `RelationshipContext.Discriminator`, carrying the column and both
> literal renderings — so 27.8 implements them against a context that already knows the edge's
> discriminator.

### Acceptance Criteria

- Reads match the same rows before and after the `assets` migration, on every dialect. **Byte-identical on the O2O JOIN path**; on the two bound paths the emitted Go changes because the value moves out of the SQL text into an argument — see the completion record for why the PRD's unqualified byte-identity claim could not be met and was amended.
- An edge declaring both `filter:` and `discriminator:` fails `sqlgen validate` naming both keys *(landed in 27.0a; not re-tested here)*.
- ~~D18(b) and D18(c) hold~~ — **moved to 27.8** with the tasks.

### Tests Required

- [x] Golden: the `assets` migration moves **only** the two bound paths; every O2O JOIN `ON` clause across all four example trees is untouched
- [x] **Failing-first, per dialect**: the O2O JOIN predicate from a `discriminator:` is byte-identical to the one from the equivalent `filter:` — on postgres, mysql **and sqlite**, whose qualifier spells a rewritten identifier differently
- [x] The O2M/M2M loader binds the value and quotes the column; the relationship-filter subquery binds into `subArgs`
- [x] Literal rendering for every scalar kind, including apostrophe-doubling on the interpolated path
- [x] Three `discriminator:` edges over one `(target, fk)` stay distinct end-to-end
- [x] **Runtime, all three dialects**: the relationship-filter `EXISTS` path over a discriminator edge executes and selects the right parents — added after the review found this path covered only by emitted-text assertions, which is exactly how FIX-212 survived
- [x] The M2M loader branch (a second, duplicated emission site no fixture reaches)
- [x] `discriminator.value` rejects a non-scalar kind, and rejects a backslash on the interpolated o2o path only
- [x] ~~Failing-first: config with both `filter:` and `discriminator:` → validation error~~ — landed in 27.0a
- [ ] ~~Failing-first (D18b): the nested child input for a discriminator edge has no field for that column~~ — **moved to 27.8**
- [ ] ~~Failing-first (D18c): `connect` on a target whose discriminator value differs → `ErrNotFound`~~ — **moved to 27.8**

### Completion Record

- **Date:** 2026-09-17
- **Files changed (26):** 7 source — `cmd/sqlgen/gen/context.go`, `context_table.go`,
  `context_relfilter.go`, **new** `context_discriminator.go`, `templates/table/get.go.tmpl`,
  `templates/shared/_filter.tmpl`, `cmd/sqlgen/manifest/builder.go`; 2 test —
  **new** `cmd/sqlgen/gen/discriminator_test.go`, `export_test.go`; 4 fixture configs;
  12 regenerated goldens; plus `docs/PRD.md`.
- **Preflight:** clean. `gofumpt v0.12.0`, `golangci-lint 2.13.2`, `go 1.27.1` — all match
  `.tool-versions`.
- **Tests:** `make check` clean (8 modules, 0 lint findings). `make check-examples` green and
  idempotent on re-run.

**The PRD asked for two things that cannot both hold, and this is the sub-item where that surfaced.**
§13.4.1 promised migration was "a byte-identical change to every generated read" while §4.8 and
§13.4.1's own field tables promised the value is "bound as a parameter, never interpolated" — and
the `filter:` path it must be byte-identical *to* interpolates. Worse on the O2O path specifically:
that predicate compiles into `sql.JoinClause.On`, whose contract (`sql/builder.go:80-87`) states
that builders "do not parse or number placeholders inside it" and that predicates needing
parameters MUST go to the outer `Conditions` — an invariant **tenancy's placeholder ordering
depends on**, and whose escape hatch does not work here, because moving an O2O predicate from `ON`
to `WHERE` turns the LEFT JOIN into an effective inner join and drops parents with no child.

**Resolved by user decision: bind where a parameter channel exists, interpolate where it does not.**
The loader and the relationship-filter `EXISTS` subquery bind (and quote the identifier through the
dialect, which the raw-SQL `filter:` form cannot); the O2O JOIN interpolates and stays byte-identical.
PRD §13.4.1 was amended to state the split as a table rather than an unqualified promise, and §4.8's
row now points at it. The alternative — growing `JoinClause` an args channel — was rejected as
overturning a documented runtime invariant for a value that comes from developer-authored config,
and as scope this sub-item does not own (Ticket Da is the phase's only planned `sql/` edit).

**The dialect trap, caught by the test that exists for it.** The first implementation hand-assembled
the O2O predicate as `alias.column = literal`. That is byte-identical on postgres and mysql and
**silently different on sqlite**, whose qualifier (rqlite/sql) emits `"alias"."column"` where
pg_query and vitess emit bare identifiers. The fix makes equivalence structural: the discriminator's
equivalent filter text goes through the *same* `qualifyFilter` the `filter:` form does, so whatever
that dialect's parser produces is what both forms produce.
`TestDiscriminator_O2OJoinByteIdenticalToFilter` runs per dialect and fails on the hand-built form.

**Golden movement is confined to the two bound paths** — verified by grepping every `On:` line across
all four example trees for movement and finding none. The manifest would have regressed (migrated
edges lost their `filter` column entirely); `relationshipPredicate` now reports a discriminator's
equivalent predicate in that same field, which restores the manifest byte-for-byte and keeps its
frozen `schema_version` 0.1.0 from needing a new key.

**The auto-review found four items; all four are fixed in place, none logged as a FIX.** Two were
stale claims the PRD amendment missed — §4.8's parent `TableRelationship` row, plus the
`RelationshipDiscriminator` doc comment and its `schema/v1.json` description, both of which 27.0a
wrote and which still promised "never interpolated" and "byte-identical". Two were real gaps in the
new code: the doc comment cited a `config.validateDiscriminatorValueKind` that **did not exist** —
so a `value: [a, b]` decoded to `[]any` and rendered silently as `"[a b]"`, the exact
loader-laxer-than-schema asymmetry 27.0a closed for required keys — and `sqlStringLiteral` doubled
only `'`, which is **not MySQL-safe**: MySQL reads `\` as an escape inside a string literal where
PostgreSQL and SQLite take it literally, and vitess parses the malformed result faithfully rather
than rejecting it, so the qualifier is no backstop. `validateDiscriminatorValue` now rejects both —
the non-scalar kind everywhere, and a backslash only on **o2o**, since o2m/m2m bind the value and
should not be punished for a limitation that is not theirs. The review also found the M2M loader
branch shipping untested (a second, duplicated emission site that no fixture reaches, because
`LinkedAttachments` deliberately stays on `filter:`); `TestDiscriminator_M2MLoaderBindsTheValue`
covers it.

**Two pre-existing bugs found and logged, neither fixed here.** Both are properties of `filter:`
being raw SQL the generator splices without owning, and neither is reachable through
`discriminator:`, whose predicate shape is fixed at `column = literal`.

**FIX-213 (blocking).** A `filter:` carrying a **top-level `OR`** is spliced without parentheses, and
SQL binds `AND` tighter than `OR`, so the parent correlation drops out of the predicate and the
loader returns other parents' rows. Confirmed at runtime on SQLite: loading parent 1's children
returned a child owned by parent 2. Affects the o2m/m2m loader and the o2o JOIN `ON`; the `EXISTS`
path already parenthesises, which is why this is an oversight rather than a decision. Present since
2026-05-13, so it predates the phase by four months. Not folded in because the fix moves goldens for
all six `filter:` edges across four trees. Every filter in the PRD, the fixtures and the examples is
an `AND`, which is associative — that is why nothing caught it.

**FIX-212 (tracked).** On SQLite, a relationship filter over a
`filter:` edge emits SQL referencing a literal alias `"sqlgenrel"` that the subquery never defines:
`funcRelationshipStaticFilterExpr` splits on the unquoted token to splice in the runtime `tgt`
variable, and rqlite/sql's quoted `"sqlgenrel"."col"` never matches the split. Still live for
`PhotoAttachments` / `LinkedAttachments` in the sqlite example. The discriminator path cannot regress
this way — it builds from `tgt` directly — and `TestDiscriminator_RelationshipFilterAliasIsRuntimeToken`
pins that on sqlite. Logged as a FIX rather than folded in: it predates this phase and affects the
`filter:` form this sub-item does not touch.

---

## 27.3 Single-row `Update` skip-refetch escape (Ticket C)

**PRD Reference:** §9.2

**Status:** **Complete** (landed 2026-09-17)

### Tasks

- [x] Guard both `return c.Get(...)` sites in single-row `Update` (spec cited `update.go.tmpl:81`/`:203`; the two `return c.Get(ctx` sites now sit at `:92` and `:219`, their guards at `:88` and `:215`) with `options.FieldOptions != nil && !options.FieldOptions.HasSelectedColumns()`, matching the five sibling templates
- [x] Regenerate goldens for the behavior change

### Acceptance Criteria

- ~~`Update` with a non-nil, nothing-selected `FieldOptions` issues **one fewer round-trip** and returns without the re-fetch.~~ **Corrected during implementation — see the Completion Record.** The round-trip framing is wrong about HEAD: `Get` delegates to `GetMany`, which already short-circuits on an all-false `FieldOptions`, so the pre-27.3 statement count was already **one**. The real defect was a spurious `ErrNotFound` after a committed `UPDATE`. The corrected criterion: **`Update` with a non-nil, nothing-selected `FieldOptions` returns `nil, nil` instead of `ErrNotFound`, and issues no re-fetch.**
- `Update` with nil or a populated `FieldOptions` is unchanged.
- Golden movement is confined to the `Update` method body.

### Tests Required

- [x] Query-count assertion via a counting `Querier`: empty-`FieldOptions` `Update` issues exactly one statement — pinned, though the **load-bearing** assertion in that test turned out to be the `err != nil` fatal, not the count (see the Completion Record)
- [x] Regression: nil `FieldOptions` still returns the full entity
- [x] Added: the **empty-update** path under an empty `FieldOptions` issues **zero** statements — §9.2 names that path explicitly, and it is the second of the two guarded sites
- [x] Added: a **populated** `FieldOptions` still re-fetches — the other half of "the escape is inert unless the selection is non-nil and empty"
- [x] Added in the review round: `TestUpdateTemplate_emptyFieldOptions` (`cmd/sqlgen/gen/update_test.go`) — the gen unit pin the sibling templates each have and `update_test.go` lacked; table-driven over single-PK postgres, single-PK MySQL and composite-PK, isolating the single-row `Update` body before counting

### Completion Record

- **Date:** 2026-09-17
- **Commit:** `402ee36` — `feat(gen): honour the empty-FieldOptions skip in single-row Update (27.3)`
- **Files changed:** `cmd/sqlgen/gen/templates/table/update.go.tmpl` (the two guards), 61 regenerated golden files across 12 example modules and `cmd/sqlgen/gen/testdata/golden/`, one new test file `cmd/sqlgen/testdata/examples/postgres/tests/update_skip_refetch_test.go` (four E2E pins), and `cmd/sqlgen/gen/update_test.go` (the gen unit pin, added in the review round). Docs corrected in the same round: `docs/PRD.md` §9.5 / §9.7 and `docs/design/archive/NESTED_MUTATIONS.md` **F1** / **D10**.
- **Preflight:** clean. `gofumpt v0.12.0`, `golangci-lint 2.13.2`, `go 1.27.1` — all three match `.tool-versions`, so the 16.8g drift class is not in play.
- **Tests:** `make check` clean. `make check-examples` run and green — `cmd/sqlgen/gen/**` matches the path-based E2E trigger. The security-review trigger did **not** fire: nothing changed under `manifest/` or `cmd/sqlgen/cli/**`.

**The ticket's own rationale was wrong, and the auto-review caught it.** **F1**, **D10** and this sub-item's acceptance criterion all describe the defect as a wasted round-trip — "`Update` with an empty `FieldOptions` currently reads a row nobody asked for". It does not. `Get` delegates to `GetMany`, and `GetMany` **already** short-circuits on an all-false `FieldOptions` (`get.go.tmpl:143-146`, returning `nil, nil`); `Get` then reads that empty result and returns **`ErrNotFound`** (`get.go.tmpl:106-108`). So pre-27.3, `Update` under an empty `FieldOptions` issued **one** statement, not two — and returned `(nil, ErrNotFound)` after an `UPDATE` that had already committed. **That is a spurious-failure bug of the FIX-196 class**, not a performance nit: the caller sees a not-found for a write that happened, rolls back its transaction, and on GraphQL maps to `NOT_FOUND`. The fix is the same three lines either way, which is why the wrong framing survived to implementation — but it changes the severity, and it is why `UpdateWithRelated` (27.9) could not have composed `Update` with an empty `FieldOptions` at all. **Measured, not argued:** re-running the pins against HEAD~1's generated code reports `Update(empty FieldOptions): sqlgen: resource not found` on both escape-sensitive tests.

**The golden movement is mechanically confined to `Update`, not asserted to be.** `git diff -U0 | grep '^@@' | grep -v ') Update('` returns empty across all 62 changed files — every hunk lands inside a `) Update(` body. The diff is **insertions only**, +2260 lines, and the added text collapses to exactly four distinct lines repeated 452 times (226 `Update` methods × the two sites). That is the whole change: no existing line moved, and nothing outside `Update` did either.

**Both sites are guarded, because §9.2 names both.** The terminal re-fetch after the `UPDATE` is the one **F1** measured; the *empty-update* short-circuit (`len(setClauses) == 0`) is the second, and 27.0 wrote it into the PRD as "on the empty-update path as well as the normal one". Guarding only the terminal site would leave the documented no-op still reading a row the caller said it did not want — the same defect one branch over. With both guarded, an empty-input `Update` under an empty `FieldOptions` touches the database **zero** times.

**The escape is placed after `m.AffectedPKs` / `m.AffectedTenants`, which is what keeps it free of side effects.** Both fields are stamped on the `MutationContext` before the guard returns, so cache invalidation and event stamping see the same values they saw before this change — the skip drops a read, never a hook input. It sits in the one spot common to every arm of the template (`$captureTenant` × `supportsReturning` × `StrictUpdates`), so the guard is emitted once per `Update` regardless of dialect, tenancy or strictness, and `StrictUpdates`' `ErrNotFound` still fires ahead of it.

**Failing-first, verified by running it that way — and the first count of it was wrong.** Against HEAD~1's generated `models_gen.go`, **two** of the four pins fail, not three: `…EmptyFieldOptions_SkipsRefetch` and `…EmptyFieldOptions_EmptyUpdate_NoStatements`, both with `sqlgen: resource not found`. The nil-`FieldOptions` and populated-`FieldOptions` pins pass at HEAD~1 by design — they are inert-case regression pins, not failing-first ones. The earlier "all three" in this record came from reading a package-level `FAIL` rather than per-test output; re-run with `-v`, the split is 2 fail / 2 pass. The gen unit pin `TestUpdateTemplate_emptyFieldOptions` is separately failing-first: against HEAD~1's template it reports `single-row Update has 0 FieldOptions guards, want 2`.

**The reviewer's one open question was adjudicated by the user: `Get`'s `ErrNotFound` on an all-false `FieldOptions` is intentional, and is not a FIX.** The review flagged the asymmetry — `GetMany` returns `nil, nil` where `Get` returns `ErrNotFound` — as the root cause 27.3 patched one level above, and a tracked-FIX candidate. It is neither. The alternative spelling, letting a read short-circuit silently the way a mutation does, was considered and rejected: on a *read* an all-false selection is almost always a caller mistake, and a silent nil is indistinguishable from a legitimately absent row, so the mistake surfaces later and somewhere else. **A clear error shows the problem where it was made.** That reasoning was undocumented, which is why the review could only see the asymmetry — it is now recorded in **PRD §9.6**, alongside the corollary that makes 27.3 necessary: the read path is not where the mutation-side `nil, nil` contract gets spelled, so every entity-returning mutation must guard its own trailing read. A non-rendering `{{/* */}}` comment at the site in `get.go.tmpl` says the same thing to the next template maintainer, verified to move **zero** goldens. **27.9 inherits a documented contract rather than an open question.**

**Tests.** Five — four E2E in one new file, plus one gen unit pin. `TestUpdateTemplate_emptyFieldOptions` (`cmd/sqlgen/gen/update_test.go`) closes a sibling-convention gap the review found: `create_test.go:742` and `upsert_test.go:520` each pin their template's guard and `update_test.go` had no equivalent. It runs table-driven over single-PK postgres, single-PK MySQL and composite-PK, and **isolates the single-row `Update` body before counting** — `UpdateMany` and `UpdateWhere` carry their own guards, so a whole-file count would pass with both of 27.3's sites missing. It also anchors on the empty-update branch comment specifically, so guarding only the terminal site still fails it. The four E2E pins: `TestUpdate_EmptyFieldOptions_SkipsRefetch_Postgres` is the query-count pin — a `capturingQuerier` records exactly one statement, it is the `UPDATE`, and the method returns `nil`; the write is then confirmed to have landed via a read on an **unwrapped** client, so the verification does not count against the escape it is verifying. `TestUpdate_EmptyFieldOptions_EmptyUpdate_NoStatements_Postgres` pins the second site at zero statements. `TestUpdate_NilFieldOptions_StillReturnsEntity_Postgres` is the regression: `UPDATE` then `SELECT`, in that order, and the returned entity carries the columns the caller never selected — proving it is the full entity and not a projection. `TestUpdate_PopulatedFieldOptions_StillRefetches_Postgres` closes the inert-case pair. The pre-existing `TestUpdate_ZeroFields_NoUpdateSQL_Postgres` (14.12) is untouched and still passes — it exercises the empty-update path under a **nil** `FieldOptions`, which this change deliberately leaves alone.

---

## 27.4 `sql/` batched conflict clause (Ticket Da)

**PRD Reference:** §9.5

**Status:** **Complete** (landed 2026-09-17)

### Tasks

- [x] Add `UpsertConflictKeys`, `UpsertUpdateColumns` and `UpsertResolvePKColumn` to `sql.MultiInsertOptions` (spec cited `sql/builder.go:52-57`; the struct now spans `:52-76`) — the three fields `InsertOptions` already carries
- [x] Call `d.UpsertClause(...)` from `BuildMultiInsert` (spec cited `sql/builder.go:184`; the call now sits at `:248-255`), mirroring `BuildInsert`'s clause (spec cited `:164-170`, now `:183-190`)
- [x] Reuse `UpsertClauseOptions` unchanged — FIX-196 already made the clause dialect-correct

### Acceptance Criteria

- No new imports in `sql/` — the runtime core stays stdlib-only.
- **Zero golden movement**: nothing emits the new fields yet.
- The emitted clause is byte-identical to the single-row form for the same conflict target on each of the three dialects.

### Tests Required

- [x] Unit per dialect: `BuildMultiInsert` with a conflict target emits the same clause `BuildInsert` does — `TestBuildMultiInsertUpsertMatchesSingleRow`, 3 dialects × 3 conflict shapes
- [x] Unit: omitting the new fields emits no conflict clause (back-compatible default) — `TestBuildMultiInsertNoUpsert`, 3 cases × 3 dialects
- [x] Added: `TestBuildMultiInsertUpsert`, 10 exact-SQL cases pinning the rendered statement per dialect rather than only the clause's equivalence

### Completion Record

- **Date:** 2026-09-17
- **Commit:** `2d4fe0a` — `feat(sql): emit the conflict clause from BuildMultiInsert (27.4)`
- **Files changed:** `sql/builder.go` (+39/−3) and `sql/builder_test.go` (+305) — the whole of the code change; `git status --porcelain` after a full `make check-examples` lists exactly those two paths. Docs touched in the review round: this file, `docs/tracker/STATUS.md`, and a dated pointer on `docs/design/archive/NESTED_MUTATIONS.md` §2.5.
- **Preflight:** clean. `gofumpt v0.12.0`, `golangci-lint 2.13.2`, `go 1.27.1` — all three match `.tool-versions`, so the 16.8g drift class is not in play.
- **Tests:** `make check` clean (8 modules, 0 lint). `make check-examples` run and green (12 example modules) — `sql/**` matches the path-based E2E trigger as a consumer-facing runtime package. The security-review trigger did **not** fire: `sql/` carries no `//go:embed` directive and is not under `cmd/sqlgen/cli/**`.

**Zero golden movement, mechanically confirmed.** `make check-examples` regenerates every example tree and diffs it against the committed goldens; after the full run `git status --porcelain` reports only the two `sql/` files. Nothing constructs a `MultiInsertOptions` with a conflict target yet — 27.5's `UpsertMany` template is the first caller — so the new fields sit at their zero values in every existing call site and the clause is gated behind `len(opts.UpsertConflictKeys) > 0`, the same guard `BuildInsert` uses.

**The clause is gated on `ConflictKeys` alone, matching `BuildInsert`.** `UpsertUpdateColumns` or `UpsertResolvePKColumn` set without a conflict target emits no clause — pinned per dialect in `TestBuildMultiInsertNoUpsert`. That matters on MySQL, where `UpsertClause` would otherwise return `ON DUPLICATE KEY UPDATE` off a bare `ResolvePKColumn`; the guard keeps the batched and single-row builders from disagreeing about what "no upsert requested" means.

**Byte-identity is tested against `BuildInsert`'s real output, not against a re-derivation.** `TestBuildMultiInsertUpsertMatchesSingleRow` builds both statements for the same conflict target and compares the suffix from the clause marker onward. The helper is guarded against passing vacuously: it `t.Fatalf`s when `BuildInsert` itself emits no clause, so a shape that silently stopped producing one fails the test rather than matching an empty string on both sides. Three shapes cover the branches the dialects actually differ on — a single key with update columns, a composite key with nothing left to set (`DO NOTHING` on PostgreSQL/SQLite, the degenerate `` `k` = `k` `` self-assignment on MySQL), and `ResolvePKColumn` set (`LAST_INSERT_ID()` leading the MySQL set list, ignored on the other two). The FIX-196 invariant that the PK assignment must **lead** the MySQL set list is pinned again at the batched call site by the exact-SQL case.

**Ordering and the early return.** The clause is written after the last value row and before `writeReturning`, exactly as in `BuildInsert`, so `... VALUES (...), (...) ON CONFLICT (...) DO UPDATE SET ... RETURNING ...` is the emitted order on the RETURNING dialects. The `len(opts.ValueRows) == 0` early return still wins over a conflict target — an empty batch returns `("", nil)` rather than a bare `INSERT ... ON CONFLICT`, pinned as its own case. A `sql.Default` sentinel consumes no placeholder, and the clause appends no arguments of its own, so placeholder numbering is untouched; pinned by the DEFAULT-sentinel case.

**No new imports.** `sql/builder.go` still imports `fmt`, `sort`, `strings` — the CLAUDE.md stdlib-only rule for runtime core packages holds, and `UpsertClauseOptions` was reused unchanged as the ticket required.

**Auto-review: PASS — 6/6 PRD criteria, 2/2 required tests, one high-confidence doc-only finding, folded in inline.** The finding: `UpsertResolvePKColumn`'s doc comment was copied verbatim from `InsertOptions` and is **misleading once batched**. MySQL evaluates `LAST_INSERT_ID(pk)` once per *conflicting row*, so the OK packet carries the **last** conflicting row's id; after a batch that mixed inserts with conflicts it names neither the caller's row nor the first inserted one. That is the same measurement 27.5's Decision (3) rests on (`firstID + i` is unsound), but the field is exported runtime API and carried no warning at its declaration — a silent wrong answer where a loud one belongs. The declaration now says so (`sql/builder.go:68-74`), and points at PRD §9.7. Two smaller items also applied: the `BuildMultiInsert` doc said the statement "closes with" the conflict clause, false on the RETURNING dialects when `ReturningColumns` is set (now "after the last value row and before any RETURNING"); and `docs/design/archive/NESTED_MUTATIONS.md` §2.5's dated gap list — the line a 27.5 implementer would grep — now carries a pointer recording which four of its ten gaps this phase has closed, without rewriting the snapshot it is a record of. The reviewer separately confirmed the semantic this ticket rests on and the `conflictSuffix` helper's anti-vacuity guard, re-deriving all ten expected strings by hand against `sortedColumnsOrder` / `reorderRow`. **Zero golden movement is structural, not observed:** all three template call sites (`create.go.tmpl:378,503,528`) use keyed composite literals, so the new fields sit at their zero values by construction. No FIX entries warranted.

---

## 27.5 `UpsertMany` (Ticket Db)

**PRD Reference:** §9.2, §9.5, §9.7, §25.1

**Status:** **Complete** (landed 2026-09-17; **key-sourcing defect found and fixed inside the sub-item 2026-09-17** — see the Completion Record's FIX-218 section)

### Tasks

- [x] New `cmd/sqlgen/gen/templates/table/upsert_many.go.tmpl`, signature mirroring `Upsert` — `UpsertMany(ctx, inputs, target, opts…)` (**Q7**)
- [x] ~~`operations.upsert_many` flag on `config.Operations` plus every `ExpandPreset` arm~~ — **moved to 27.0a** (the arm count is **five**, not six: `all`/`""` is one arm covering two spellings, plus a `default` that errors)
- [x] **Decision (1)** — dedupe inputs by conflict target, last-wins: PostgreSQL alone rejects an in-statement duplicate on the `DO UPDATE` shape (companion §6.1)
- [x] **Decision (2)** — the `[]*T` return contract on the `DO NOTHING` branch, where `RETURNING` yields only inserted rows on two of three dialects (companion §6.2)
- [x] **Decision (3)** — the MySQL db-generated-PK contract: `LAST_INSERT_ID()` after a mixed batch names the first *newly inserted* row, so `firstID + i` is unsound (companion §6.3)
- [x] **Invariant** — `AffectedPKs` never comes from `RETURNING` (**F7**, **A19**), preserving what the single-row junction `Upsert` already does. **The ticket's own statement of the other half was wrong and was corrected inside this sub-item:** it said the keys come from the *inputs*, full stop. They do so only when the runtime conflict target covers every primary-key column; on any other target the conflicting row keeps its own key and the sub-batch is read back (**FIX-218**, Resolved — see the Completion Record). The junction-table case the ticket had in mind is the one where the two rules agree, which is why the gap was invisible from the ticket
- [x] Batch at `c.batchSize`, as `CreateMany` does
- [x] **Added, taken from 27.6:** the `hook.OpUpsertMany` constant. The template cannot compile without an op, and 27.6's own task text ("without it the op falls out of the switch to a bare `return nil`") presupposes the constant already exists — see the Completion Record
- [x] **Expect golden movement across all 12 example modules.** `operations.upsert_many` defaults to **`true`** (PRD §4.6), like every other general-purpose operation, so `UpsertMany` is emitted on every table the moment **this** sub-item lands the template. The flag itself lands inert in 27.0a — `gen.ResolvedOperations` is a separate struct and is not extended there — so the movement belongs here and nowhere earlier. It is correct and intended, but it makes this the one place in 27.1–27.7 where "zero golden movement" does not apply

### Acceptance Criteria

- Batch upsert on a pure link table is **idempotent** and issues `ceil(N / batchSize)` statements.
- Each of the three decisions is documented in the template and in §9.5, not merely implemented.
- `AffectedPKs` length equals the deduped input length on every dialect, including the `DO NOTHING` branch — **and every key in it names a row that exists.** The length half alone passed while FIX-218 was live, on every dialect and every test in the original round: the input-sourced slice was always the right *length*, and only its *values* were phantom. A length assertion can therefore not stand in for this criterion, which is why the pin added with the fix asserts the returned entities carry the **seeded** key rather than the one the input supplied.

### Tests Required

- [x] E2E: repeated `UpsertMany` on an existing link set is a no-op and errors on no dialect — `TestUpsertMany_LinkTableIdempotent` on postgres, mysql and sqlite
- [x] Failing-first dialect pin (§6.1): in-statement duplicate conflict key — PostgreSQL rejects (`TestUpsertMany_InStatementDuplicate_RawRejected`), SQLite accepts (`…_RawAccepted`); `TestUpsertMany_DedupesLastWins` on all three
- [x] Failing-first dialect pin (§6.2): `DO NOTHING` + `RETURNING` returns only inserted rows — `TestUpsertMany_DoNothing_RawReturningIsShort` on postgres and sqlite
- [x] Failing-first dialect pin (§6.3): MySQL `LAST_INSERT_ID()` after a mixed batch — `TestUpsertMany_LastInsertIDAfterMixedBatch_RawIsUnusable`
- [x] Query-count assertion: one statement per batch, not one per row — `TestUpsertMany_LinkTableStatementCount`
- [x] Added: template-level pins per dialect (`cmd/sqlgen/gen/upsert_many_test.go`, 12 tests + 2 goldens) and behavior coverage for the two emitted helpers (`…/examples/postgres/models/upsert_dedupe_test.go`, 15 cases)
- [x] **Added with FIX-218** — failing-first E2E on the key-sourcing rule: `TestUpsertMany_CallerKnownKey_NonCoveringTargetResolvesExistingKey` (a caller-known key upserted on a non-covering target resolves to the *existing* row's key and returns one entity per input) and its inert-case companion `…_CoveringTargetIssuesNoReadBack` (a PK-covering target still issues one INSERT + one SELECT). Template-level: `TestUpsertManyTemplate_pkFromInput` rewritten to assert **both** directions, plus `TestUpsertManyTemplate_conflictCoversPKTable` on the generated coverage table
- [x] **Added after the fix, closing a gap in the fix's own code** — `TestUpsertManyTemplate_compositePKReadBack`. `resolveUpsertManyRows` was generalized to resolve a **composite** key from a read-back, and nothing reached that path: every composite fixture and every composite table in the 12 example trees declares only `ConflictPK`, so the generalization shipped untested. The pin adds an `order_items` fixture carrying `UNIQUE (order_id, line_no)` and asserts the helper is composite-shaped end to end — `[]OrderItemPK` return, `resolvedRow.pk`, the struct rebuilt from the scanned row, and **every** PK column in the lookup list. Verified non-vacuous: reverting the lookup list to the first PK column alone fails it
- [x] **Runtime coverage for the same shape on MySQL** — `user_categories` in the mysql example gained a nullable `slot INT` with `UNIQUE (user_id, slot)`, giving one table a caller-known **composite** key on a non-covering **two-column** target. That reaches the composite read-back at runtime, and MySQL's tuple-`IN` degenerating through `buildExpandedOR` where PostgreSQL and SQLite emit a row constructor — none of which the postgres pin touches, being a single-column key on a single-column target. `TestUpsertMany_CallerKnownCompositeKey_NonCoveringTargetResolvesExistingKey` (fails pre-fix with `returned 1 entities, want 2`) and `TestUpsertMany_NonCoveringTarget_IndefiniteConflictValueRefused` (nullable `slot` makes the per-row refusal reachable on a caller-known key for the first time; pre-fix the call wrote the row instead of refusing). **Remaining:** sqlite on this shape, whose read-back is byte-identical to PostgreSQL's — left open deliberately

### Completion Record

- **Date:** 2026-09-17
- **Files changed (888b709, the sub-item):** new `cmd/sqlgen/gen/templates/table/upsert_many.go.tmpl`; `hook/hook.go`, `cmd/sqlgen/gen/{context.go, context_table.go, context_shared.go, context_api.go, context_event.go, orchestrate.go}`, `cmd/sqlgen/gen/templates/table/{upsert,client}.go.tmpl`, `cmd/sqlgen/manifest/{builder.go, sql.go}`, `cmd/sqlgen/cli/lint.go`, `docs/PRD.md`. Tests: new `cmd/sqlgen/gen/upsert_many_test.go` + 2 goldens, new `upsert_many_test.go` in the postgres / mysql / sqlite example `tests/`, new `upsert_dedupe_test.go` in the postgres example `models/`; updated `manifest/builder_test.go`, `gen/{shared_types,context_shared}_test.go`.
- **Files changed (599a27e, FIX-218 inside the sub-item):** 70 files, 12 outside the example trees — `cmd/sqlgen/gen/{context.go, context_table.go}`, `cmd/sqlgen/gen/templates/table/upsert_many.go.tmpl`, `docs/PRD.md`, and five test files (`upsert_many_test.go`, `upsert_test.go`, `conflict_target_override_test.go`, `tenancy_template_test.go`, `tenant_capture_template_test.go`) plus the regenerated `gen/testdata/golden/upsert_many_products_gen.go`. In the example trees: `postgres/schema.sql` and `postgres/tests/upsert_many_test.go` by hand, 56 regenerated goldens.
- **Preflight:** clean. `gofumpt v0.12.0`, `golangci-lint 2.13.2`, `go 1.27.1` — all three match `.tool-versions`, so the 16.8g drift class is not in play.
- **Tests:** `make check` clean (37 packages, 0 lint). `make check-examples` green — 20 test packages across all 12 example modules. Re-run to completion after FIX-218, with a full `-update-e2e` regeneration and a clean `git status` afterwards.

**Golden movement, as forecast.** 269 files under `cmd/sqlgen/testdata/examples/` in 888b709: `shared_types_gen.go` in all 12 trees (both `models/` and `expected/`) for the two new helpers, the per-table files for the method plus its `<Entity>Client` interface entry, and the manifest markdown / JSON for the newly advertised method. No other tree moved.

FIX-218 moved 56 more, and **what did *not* move is the load-bearing part**: no example tree gained a `pkFromInput` or `<entity>ConflictCoversPK` line. Stripping comments from `git diff` over the example trees leaves only the `resolveUpsertManyRows` rename (`resolvedRow.id` → `.pk`, `ids` → `pks`, from generalizing the helper to composite keys) plus the `counters` table's new `slot` column in the postgres tree. That is the mechanical confirmation that every caller-known-key table in all 12 trees declares only PK-covering conflict targets — so nothing shipped was ever reaching the defect, and the fix costs those tables neither a branch nor a statement.

**Decision (1) — dedupe, last-wins, first-appearance order.** Inputs collapse by conflict target before the first statement is built, so PRD §25.1's `ceil(M / batch_size)` is counted after the dedupe rather than before. A row carrying no **definite, non-NULL** value for every conflict column is exempt: NULL never matches under a unique index on any of the three dialects, so collapsing two NULL-keyed rows would drop a row the caller asked to insert. Identity is computed from what each value *binds* as (`driver.DefaultParameterConverter`), not from how it prints — `%v` on a nullable column's `*T` prints an address, which would both defeat the dedupe and make the read-back below never match. Components are length-prefixed so `("ab","c")` and `("a","bc")` cannot collide.

**Decision (2) — one entity per deduped input, on every dialect.** `UpsertMany` never reads rows out of `RETURNING`: it resolves the written keys and re-fetches them through `GetMany`, as `CreateMany` does. That makes §6.2 a non-issue rather than a contract to weaken — the `DO NOTHING` branch returns the conflicting rows like any other. Pinned both ways on postgres and sqlite: the raw statement's `RETURNING` comes back short (2 of 3 rows), and `UpsertMany` on the same shape returns all of them.

**Decision (3) — the key is never taken from the statement, and "caller-known" needs two conditions, not one.** Where it is caller-known it comes from the inputs, at one statement per sub-batch. **The first cut of this got the condition wrong, `/verify 27.5` caught it, and it was fixed inside the sub-item (FIX-218, Resolved).** Being composite or `app` / `caller` strategy is necessary but not sufficient: the *runtime conflict target* must also cover every primary-key column. The key column is excluded from the update half of the clause — a row's identity is not a mutable attribute — so a row conflicting on some *other* unique constraint keeps the key it already had, and the input's key names a row that does not exist. Measured rather than argued: against the pre-fix template `TestUpsertMany_CallerKnownKey_NonCoveringTargetResolvesExistingKey` reports `UpsertMany returned 1 entities, want 2` — the conflicting row silently dropped, a §9.5 decision-2 violation, with a phantom key on `AffectedPKs` feeding cache eviction and `Event.PK`. The single-row `Upsert` never had the bug because `upsert.go.tmpl:6-11` resolves through `RETURNING` on every RETURNING dialect *regardless of strategy*; the batched form dropped that half of the condition and kept only `ne .PKStrategy "db"`.

**The fix is a runtime choice off a generated table, and it is free where it cannot be taken.** `ConflictTargetContext.CoversPK` is computed once in `buildConflictTargets`, and the template emits `<entity>ConflictCoversPK` only for a table that declares a non-covering target — the *set* of targets is fixed at generation time even though the choice among them is not, so every junction table, and every caller-known-key table in all 12 example trees, keeps the one-statement fast path with no runtime branch. Example golden movement is therefore confined to a rename (`resolvedRow.id` → `.pk`, `ids` → `pks`, from generalizing `resolveUpsertManyRows` to composite keys) and comment rewording: `git diff` over the example trees adds no `pkFromInput` or `ConflictCoversPK` line anywhere, which is the mechanical proof that no shipped example was ever reaching the bug — and the reason it survived review, `make check` and `make check-examples` alike. `counters` in the postgres example gained a nullable `slot TEXT UNIQUE` to make the shape reachable at runtime for the first time. PRD §9.2, §9.5 decision 3, §9.7 and §25.1 all stated the strategy-only rule and were amended with it. Where it is not (`db` strategy) the sub-batch is read back by the conflict target it used — the batched form of FIX-196's `resolveUpsertConflictRow`, uniform across dialects, and a key-resolution read rather than a write, so §25.1's write bound is unchanged (PRD §9.7 now says so). **The alternative was considered and rejected by the user on 2026-09-17:** gating emission on a caller-known key would have removed `UpsertMany` from most non-junction tables (`id UUID PRIMARY KEY DEFAULT gen_random_uuid()` resolves to `db`), leaving it available on strictly fewer tables than the single-row `Upsert` already is — which guts D5's "independently useful" justification.

**The §6.3 measurement reproduces exactly.** `TestUpsertMany_LastInsertIDAfterMixedBatch_RawIsUnusable` on `mysql:8.0` logs `LAST_INSERT_ID=2, seeded=1, actual ids=map[RawLII-a:1 RawLII-b:2 RawLII-c:3]` — the same numbers the companion measured on 2026-09-11. `firstID + i` would name 2, 3, 4: wrong for all three rows. The test asserts the *claim* (the fabrication does not reproduce the real ids) rather than the literal ids, so it stays a pin rather than a version snapshot.

**`UpsertResolvePKColumn` is deliberately not set**, and 27.4's review is why: it republishes at most one row's id into MySQL's OK packet, which after a batch names neither the caller's row nor the first inserted one. Nothing here reads `LastInsertId`, so requesting it would only add a misleading assignment to the SET list. Pinned per dialect — the template must contain neither `UpsertResolvePKColumn:`, `.LastInsertId()`, `firstID + int64(` nor `ReturningColumns:`.

**Two loud refusals rather than a silently wrong key.** On a table whose key is database-generated, a conflict target naming a column the input does not supply (`<T>ConflictPK` on an `AUTO_INCREMENT` key) errors **before anything is written**, as does a per-row conflict value that is not definite. This is the `{Table}ConflictPK` rule's posture (§9.5): a missing key on the event and cache paths is silent corruption, a refusal is not. Pinned on all three dialects, including an assertion that the refused call wrote zero rows.

**One behavior deliberately diverges from `CreateMany`:** an empty input short-circuits before any statement. The terminal re-fetch filters on the written keys, and every key filter treats an empty set as *no condition*, so an unguarded empty call reads the whole table back. `CreateMany` has the same hazard at HEAD, measured rather than inferred: probed on the postgres example, `CreateMany(ctx, nil)` issues `SELECT "created_at", "description", "id", "name" FROM "public"."categories"` — no `WHERE`, `Limit: new(0)` — and returns every row. **Not** changed here, since it is shipped behavior with its own golden movement. Logged and fixed as **FIX-217** at the user's direction (2026-09-17), which also caught `UpdateMany`, `SoftDeleteMany` and `RestoreMany` carrying the same bug — the write half of each was always guarded (`WHERE 1 = 0`), the trailing re-fetch was not.

**Shared surface, two widened gates.** `<Entity>ConflictTarget` and `<entity>ConflictColumns` now emit under `upsert OR upsert_many` rather than `upsert` alone — PRD §9.2 says the two draw from the same enum, and `upsert: false, upsert_many: true` validates cleanly, so the narrower gate produced a package that would not compile. `captureAffectedTenants` is likewise no longer MySQL-only: a conflict leaves the existing row's tenant in place and `RETURNING` yields nothing on the `DO NOTHING` branch, so a tenanted `UpsertMany` reads tenants back on every dialect (PRD §29.5).

**The interim state between 27.5 and 27.6, named precisely.** `operations.upsert_many` defaults to `true` (PRD §4.6), so this sub-item ships the producer on every table while the three consumer arms are still 27.6's content. The auto-review was right that "27.6's pins are failing-first" understates it — **all three** permissive defaults are live contract violations until 27.6 lands, and they are recorded here rather than left implicit. (This table originally counted two and called the third safe; `/verify 27.5` corrected it — see **FIX-219**.)

| Consumer | Default arm today | Contract it violates |
|---|---|---|
| `mapOpToAction` (`event_hooks.go.tmpl:35`) | `event.Action(string(op))` → a **new wire action `"upsert_many"`** | PRD §21.2 and §28: `OpUpsertMany` "adds no new `event.Action` value" |
| cache `dispatchMutation` (`cache.go.tmpl:944`) | falls out of the switch to a bare `return nil` → **no invalidation** | PRD §27.7's `UpsertMany` row |
| §32.3 redaction (`event_hooks.go.tmpl:196`), fail-closed table | **nil payload** — after the fix below | PRD §28.6's shape table: a per-row `*Create<T>Input`. Safe from the *leak*, wrong in *shape* (**FIX-219**) |
| §32.3 redaction, **non**-fail-closed table | `inputVal = mc.Input` → every fanned-out event carries the **whole deduped slice** | PRD §28.9: "an arm that passed the input through unchanged would give every fanned-out event the entire batch" (**FIX-219**) |

The third was a genuine leak this sub-item introduced and has been fixed inside it: `buildEventTableContext`'s `emitCreate` did not list `UpsertMany`, so a table redacting a create-input field while generating only `UpsertMany` resolved `failClosed = false` and the default arm published the whole deduped slice **un-redacted** — exactly the FIX-207 class. `UpsertMany` now joins `Create` / `CreateMany` / `Upsert` in that list, because it publishes `Create<T>Input` values like they do. That fix closed the **leak**; it did not close the **shape**, which is what rows three and four above record and what **FIX-219** tracks. The remaining arms are 27.6's and stay open — but 27.6 closes rows three and four only if its `case hook.OpUpsertMany:` arm is emitted *unconditionally*, with redaction nested inside, the way `case hook.OpCreateMany:` is; mirroring the `{{- if .EmitRedactCreate }}`-wrapped `case hook.OpCreate, hook.OpUpsert:` would leave the non-redacting table still publishing the whole slice. **Whether to ship that window or default `upsert_many` to `false` until 27.6 is a call for the user**, and the default is PRD-normative, so changing it would be a PRD amendment rather than a config tweak.

**`hook.OpUpsertMany` landed here, not in 27.6 — a deliberate deviation from the tracker.** The template cannot construct a `MutationContext` without an op, and the only alternative, reusing `hook.OpUpsert`, would hand a whole input slice to three arms written for a single row — including §32.3's redaction arm, which is the un-redacted-payload class FIX-207 exists to close. It also matches what 27.6's own task text assumes ("without it the op falls out of the switch to a bare `return nil`") and what PRD §21.2 already declares verbatim at the same position in the constant block. 27.6 keeps its real content: the three consumer arms, and its three pins are genuinely failing-first against this state.

---

## 27.6 `OpUpsertMany` and its three consumer arms (Ticket Dc)

**PRD Reference:** §27.7, §28, §32.3

**Status:** **Complete** (landed 2026-09-17; verified 2026-09-18 — `/verify` found the five
`cache_gen.go` goldens hand-wrapped rather than regenerated and fixed it inside the sub-item, see
the Completion Record)
**Blocked by:** ~~FIX-207 (the redaction `default` arm must fail closed first)~~ — Resolved 2026-09-16

### Tasks

- [x] ~~Add `OpUpsertMany` to `hook.MutationOp` (`hook/hook.go:17-33`) — a **runtime-module** edit~~ — **landed in 27.5** (2026-09-17): the `UpsertMany` template cannot build a `MutationContext` without an op, and reusing `OpUpsert` would have fed a whole input slice to three single-row arms. The three consumer arms below are unaffected and remain this sub-item's content
- [x] Cache invalidation arm in `dispatchMutation` (`cache.go.tmpl`) — without it the op falls out of the switch to a bare `return nil`
- [x] `mapOpToAction` arm → the existing `event.Upsert`, **not** a new wire action, so every subscriber filtering `event.Upsert` keeps working
- [x] §32.3 redaction arm with a **per-row** input — **an `OpCreateMany`-shaped index arm is exactly right**, and this bullet used to say the opposite. **EQ5** argued the arm "cannot be a copy of `OpCreateMany`'s" because `DO NOTHING` + `RETURNING` returns only the inserted rows, leaving `AffectedPKs` shorter than the input slice. **27.5 resolved EQ5 the other way** (2026-09-17 verification): `AffectedPKs` is sourced from the inputs, or from `resolveUpsertManyRows`, which returns exactly one key per deduped value row and errors otherwise — never from `RETURNING` (**A19**) — and the terminal republishes `m.Input = rowInputs`, the **deduped** slice. PRD §28.9 now carries that guarantee normatively (commit `ad575c5`). Measured on the `events` example: 3 inputs deduping to 2 rows → 2 events, published slice length 2, `slice[i]` the input that wrote `AffectedPKs[i]`; a mixed batch of 2 conflicting + 1 new → 3 aligned events; a junction (`DO NOTHING`, empty `updateColumns`) builds `rowPKs` one-per-input with no read-back at all. **Index `batchCreateInputs[i]` like `OpCreateMany` does** — do not build a correlation mechanism for a problem that no longer exists
- [x] **Emit that arm unconditionally**, with the `redact<T>CreateInput` call nested *inside* it — the way `case hook.OpCreateMany:` is written (`event_hooks.go.tmpl:129-137`), **not** the way `case hook.OpCreate, hook.OpUpsert:` is (`event_hooks.go.tmpl:150-158`, wrapped in `{{- if .EmitRedactCreate }}`). This is **FIX-219**, found by `/verify 27.5`: there is a **fourth** `OpUpsertMany` consumer site nobody listed, `passThroughOps` (`context_event.go:269-299`), and the two gates fail in opposite directions. On **every** fail-closed table, `OpUpsertMany` reaches the fail-closed `default` and publishes `Event.Input: nil` where §28.6 mandates a per-row `*Create<T>Input` — live today in a committed golden at `cmd/sqlgen/testdata/examples/graphql/models/event_hooks_gen.go:328`. (Not only the tables that leave their create input un-redacted, as this bullet first read: a table that *does* redact a create field gets `case hook.OpCreate, hook.OpUpsert:`, which likewise does not name `OpUpsertMany`. Measured — `accounts` in the `events` example redacts create, update **and** increment inputs and still publishes nil on all three fanned-out events.) On a **non**-fail-closed table the `default` is `inputVal = mc.Input`, so every fanned-out event carries the entire deduped batch slice. An unconditional arm closes both and makes a `passThroughOps` entry unnecessary; add a comment there recording why, so the question is not re-opened
- [x] Add the **`UpsertMany` row to PRD §28.6's shape table**, which has none — the table stops after the `*Where` family, so §28.9's per-row prose (PRD:14020 after this sub-item's edit; :14015 before it) had no normative row behind it

### Acceptance Criteria

- All three consumer switches name `OpUpsertMany` explicitly. **None of these fails at compile time** — every switch has a permissive default — so each needs a behavioral pin.
- The published event carries a redacted, per-row input; no event carries an input that is not its own — asserted on a table with **no** redacted column and on a **redacting** table (any fail-closed table misses today, not just one redacting an *update* field), plus a dedupe case where the input slice is longer than `AffectedPKs`, which is what catches a regression back to a whole-slice payload (**FIX-219**).
- `OpUpsertMany` adds no new `event.Action` value.

### Tests Required

- [x] Failing-first: rows written by `UpsertMany` are invalidated in the cache
- [x] Failing-first: a subscriber filtering `event.Upsert` receives the events
- [x] Failing-first: the published input is redacted and per-row (assert an `access: internal` column is absent, and that event *i* carries input *i*) — on a non-redacting table **and** a redacting one, and with a deduping batch so the published slice is shorter than the caller's

### Completion Record

**Completed 2026-09-17; verified 2026-09-18.** All three consumer arms landed, plus
**FIX-219**'s shape fix.

`/verify 27.6` returned **5/5 PRD requirements passing, 3/3 required tests present** (plus four
beyond spec), **0 guideline issues**, and **full reviewer agreement** — the independent pass
confirmed every parent finding and refuted none. It found **one FAIL** (the hand-wrapped
`cache_gen.go` goldens, below) and four documentation inaccuracies, all fixed in place rather
than logged as FIX entries. Gates at close: `make check` clean across 37 packages;
`make check-examples` exit 0 across 12 modules / 20 packages with 12 × "0 issues";
`TestE2EGoldenFiles` passing; toolchain matching `.tool-versions` with no drift.

**Files changed**
- `cmd/sqlgen/gen/context_event.go` — `EventTableContext.HasCreateMany` (bool) became
  `BatchCreateOps` (`[]string`), resolved by the new `batchCreateOps`. It names the enabled ops
  whose §28.6 payload is a `[]*Create<T>Input` indexed per row — `OpCreateMany` and
  `OpUpsertMany` — so the template composes one arm from a list instead of two booleans
  (`guidelines/TEMPLATES.md` §1, logic in Go). `passThroughOps` gained a comment recording why
  neither op belongs in the pass-through list.
- `cmd/sqlgen/gen/templates/event_hooks.go.tmpl` — `mapOpToAction`: `OpUpsertMany` joins
  `case hook.OpUpsert:` → `event.Upsert`. The batch-locals gate, the pre-loop type assertion and
  the per-row fanout arm all key off `BatchCreateOps`, so the arm is emitted **unconditionally**
  with `redact<T>CreateInput` nested inside.
- `cmd/sqlgen/gen/templates/cache.go.tmpl` — `OpUpsertMany` joins the key-based eviction arm
  (§27.7's "Invalidate many" row), **not** `OpCreateMany`'s write-through arm.
- `docs/PRD.md` §28.6 — added the missing `UpsertMany` row to the shape table, and a sentence
  noting its slice is the **deduped** one rather than the caller's.
- `docs/design/CACHE.md` §9.3 — the `MutationHook()` op table gained its `OpUpsertMany` row. It
  enumerates every op the dispatch switch names (including the `*Where` row FIX-208 added), so
  PRD §27.7 getting the row while the sibling design doc did not was a real gap. Added by
  `/verify 27.6`.
- Goldens: 43 event-hook tables across six examples, plus every `cache_gen.go` in the repo —
  five example trees (`cache`, `graphql`, `tenancy`, `tenancy_mysql`, `tenancy_postgres`), each in
  both `models/` and `expected/`.
- Tests: `cmd/sqlgen/gen/event_hooks_template_test.go` (2 new), `cache_template_test.go`
  (1 updated), `testdata/examples/events/tests/upsert_many_test.go` (new, 4 pins),
  `testdata/examples/cache/tests/mutation_test.go` (1 new pin),
  `testdata/examples/tenancy/tests/upsert_many_tenancy_test.go` (new, 2 pins — added from the
  auto-review, see below).

**All seven behavioral pins verified failing-first** against the pre-fix generated code, each for
its own reason — not merely observed to pass afterwards:

| Pin | Failure before the fix |
|---|---|
| `TestEventUpsertMany_ActionIsUpsert` | `waitFor(2): only 0 events delivered` — the `event.Upsert` filter never matched the minted `"upsert_many"` action |
| `TestEventUpsertMany_PerRowInput` | `Input type = []*models.CreateProductInput` — the whole batch slice |
| `TestEventUpsertMany_PerRowInputAfterDedupe` | same, with the caller's slice longer than `AffectedPKs` |
| `TestEventUpsertMany_RedactedPerRowInput` | `Input type = <nil>` — the §32.3 fail-closed default |
| `TestMutation_UpsertManyInvalidatesBatch` | `want 1 invalidate_many, got 0` — the bare `return nil` |
| `TestUpsertManyTenancy_EventsStampPerRowTenant` | `Action = "upsert_many"` + `Input type = []*models.CreateProductInput` |
| `TestUpsertManyTenancy_CacheEvictsPerRowTenant` | `hits delta = 1, want 0` / `misses delta = 0, want 1` — the tenant-scoped key survived |

**Two corrections to this sub-item's own spec, made before implementing** (see **FIX-219**'s
Verification block):

1. **The EQ5 bullet was obsolete and said the opposite of the right answer.** It held that the
   arm "cannot be a copy of `OpCreateMany`'s" because `DO NOTHING` + `RETURNING` leaves
   `AffectedPKs` shorter than the input slice. 27.5 resolved EQ5 the other way — `AffectedPKs`
   comes from the inputs or from `resolveUpsertManyRows` (exactly one key per deduped value row,
   error otherwise), never `RETURNING`, and the terminal republishes `m.Input = rowInputs`.
   Measured on the `events` example across all three branches before writing any code. The arm
   **is** an `OpCreateMany`-shaped index arm.
2. **The nil payload was scoped too narrowly.** It is every fail-closed table, not only those
   leaving their create input un-redacted — a table that *does* redact a create field gets
   `case hook.OpCreate, hook.OpUpsert:`, which does not name `OpUpsertMany` either.

**One gate widened beyond what FIX-219 listed.** `batchCreateInputs` and its type assertion were
gated on `HasCreateMany` alone. No `operations` preset produces `create_many: false,
upsert_many: true` and nothing validates against the pair, but a per-table override reaches it —
and with the old gate that config emits a package referencing an undeclared variable. Pinned by
`TestEventHooks_upsertManyWithoutCreateMany_stillDeclaresBatchLocals`.

**One pre-existing pin was updated, not weakened.**
`TestCacheTemplate_whereOpsShareKeyBasedArm` matches the invalidation arm's exact text; adding
`OpUpsertMany` to the `*Many` group changed it. The expected string now includes the new member
and still asserts the same invariant — that the `*Where` ops stay folded into the key-based arm
rather than splitting into their own.

**Auto-review: 5/5 PRD, 0 blocking.** It independently re-walked the index-alignment audit across
every branch of `upsert_many.go.tmpl` — composite PK, app / caller, `db` with read-back,
non-covering target, the `c.batchSize` loop and both arms of the `$captureTenant` fork — and
confirmed `len(m.Input) == len(m.AffectedPKs)` with matching order in all of them. Three findings,
all acted on rather than deferred:

1. **Tenanted `UpsertMany` was generated but never exercised** — no test anywhere called it on a
   tenanted table. The reviewer scoped this to 27.5; it is **27.6's**, because the cache arm added
   here is what first routes `OpUpsertMany` into `invalidateAffectedTenanted`, whose misalignment
   modes are not all loud. A **short** carrier is loud — `invalidateTenantedFallback` pattern-wipes
   and emits the cache error signal. A **same-length** one is silent either way: a `nil` entry
   skips that row's eviction, and a wrong-tenant entry evicts another tenant's key while the real
   one stays stale. Closed by `tests/upsert_many_tenancy_test.go` in the `tenancy` example.
2. **The `$mayReadBack` + `$captureTenant` branch appeared in no committed golden** — the one
   alignment path verified only by inspection. `tenancy`'s `products` is exactly that shape
   (tenanted with the tenant **not** in the PK, `AUTOINCREMENT` key, and
   `ProductConflictWorkspaceIDSKU` not covering the key), so the same two pins cover it: keys and
   tenants both come from the read-back there.
3. **A literal-text gap in this sub-item's own Tests Required** — the dedupe case existed only on
   the non-redacting table. `TestEventUpsertMany_RedactedPerRowInput` now uses a deduping batch
   (inputs 0 and 2 share an email), so the redaction arm is pinned to index the deduped slice too.

Both new tenanted pins were likewise verified failing-first: the event pin reported
`Action = "upsert_many"` plus `Input type = []*models.CreateProductInput`, and the cache pin
reported `hits delta = 1, want 0` / `misses delta = 0, want 1`.

**`/verify` (2026-09-18) found the five `cache_gen.go` goldens hand-wrapped rather than
regenerated, and no local gate catches that.** The template emits the widened case list on one
line; the committed `expected/` and `models/` copies carried it wrapped across two
(`hook.OpUpsertMany, hook.OpUpdateMany, hook.OpSoftDeleteMany,\n\t\t\thook.OpHardDeleteMany,
hook.OpRestoreMany,`). Both copies agreed with each other, so the byte-identical
`expected/` ↔ `models/` check passed; both compiled and linted, so `make check-examples` passed;
and `make check` runs `go test -short`, which `TestE2EGoldenFiles` skips outright
(`e2e_test.go:88-90`). The only gate that compares generated output against `expected/` is CI's
non-short `cmd/sqlgen` job — so this would have failed on push, not locally. There is no `lll`
linter and generated `cache_gen.go` already carries 166-character lines, so nothing motivated the
wrap. Fixed by `make update-golden-e2e`, which touched exactly the same file set (the five
`cache_gen.go` pairs) and no others.

**The transferable lesson is about the gate list, not this line.** `/implement` Step 5 and
`/verify` Step 7 both name `make check` + `make check-examples` as the path-triggered sweep for a
`cmd/sqlgen/gen/**` change. Neither runs `TestE2EGoldenFiles`. Any hand-edit to a golden that is
applied consistently to both `expected/` and `models/` passes every local gate and fails only in
CI. A golden touched by hand should be followed by `make update-golden-e2e`.

Not a gap, checked: `templates/api/input_translate.go.tmpl:18` gates on
`Create || CreateMany || Upsert` without `UpsertMany`, but PRD §11.10 emits no `UpsertMany` root
field, so nothing calls the missing translator.

---

## 27.7 Supporting surface for the emitters (Ticket E)

**PRD Reference:** §22.1, §22.3, §4.13, §8.5, §9.9.5

**Status:** **Complete** (landed 2026-09-18)

### Tasks

- [x] `database.ErrAlreadyRelated` and `database.ErrNestedVerbConflict` sentinels (`database/errors.go`)
- [x] `*database.NestedMutationError{Edge, Verb, ID}` unwrapping to both, the way `ConstraintError` unwraps to `ErrConstraintViolation`
- [x] **Re-export both sentinels and a `NestedMutationError` type alias from the generated package** (`cmd/sqlgen/gen/templates/error.go.tmpl`) — the runtime declaration alone does not reach consumers, and the template's alias list is written out explicitly rather than derived, so it does not grow on its own. PRD §22.1 states the rule this satisfies
- [x] ~~`cmd/sqlgen/config/schema/v1.json`~~ — **moved to 27.0a**
- [x] Two `errors.Is` arms in `mapErrorToGQL` → `CONFLICT` and `INVALID_INPUT`
- [x] Entity-client wiring: the junction client and `callbackMode` — junction clients per M2M edge on **four** clients, not three (`userClient`, `assetClient`, `categoryClient` **and `documentClient`**, which A1 had already corrected the design doc about); `callbackMode` on every client with a *nested surface*, which is wider than "has an M2M edge" — see the Completion Record (**F8**, **A7**)
- [x] `resolved_names.go` — a **new key kind** for (parent, edge)-keyed names (**A6**). New entries are not sufficient: these names fall in the registry's documented cross-shape blind spot (`resolved_names.go:48`)
- [x] ~~The four new `Operations` fields across all `ExpandPreset` arms, with **D20** validation~~ — **moved to 27.0a**, which lands the whole config surface in one go
- [x] **Added 2026-09-18, not forecast:** PRD §9.9.5's nested child input renamed `Create<Parent><SingularEdge>Input` → `<Parent><Edge>CreateInput`, because the new key kind found the old spelling colliding **systematically** — see the Completion Record
- [x] **Added 2026-09-18:** the manifest's `conventions.error_sentinels` grows both sentinels (`cmd/sqlgen/manifest/builder.go`), caught by the existing `TestErrorSentinelsMatchGeneratedPackage` drift guard

### Acceptance Criteria

- [x] `errors.Is(err, database.ErrAlreadyRelated)` works through the wrapper; `mapErrorToGQL` reads `Edge` off the struct rather than parsing it out of a message. *(The `Edge` read is **27.10's** — `extensions.path` is listed there. 27.7 lands the struct that makes it possible and the two `errors.Is` arms.)*
- [x] ~~No `ExpandPreset` arm leaves a new field nil~~ — **moved to 27.0a's acceptance criteria**.
- [x] A (parent, edge) name colliding with a real table name is a **codegen error**, not a `go build` error.
- [x] Zero golden movement except the client-wiring fields — **not met as written, and it could not have been.** The task list's own re-export bullet moves every `errors_gen.go` (21 files) and the `mapErrorToGQL` bullet moves every `graph/sqlgenresolver/errors_gen.go` (14). Total movement: 102 files across the 12 example trees plus one `gen` golden — 35 `errors_gen.go`, 18 `client_gen.go`, 16 `models_gen.go`, 14 per-table files under `file_per_table`, and the manifest artifacts. Every one is a client-wiring field, a re-exported name, or a manifest sentinel row.

### Tests Required

- [x] Unit: `errors.Is` through `NestedMutationError` for both sentinels — `database/errors_test.go::TestNestedMutationErrorIs`, table-driven over both, plus the `ErrNotFound` case §9.9.8 routes through the same wrapper
- [x] Unit: a synthetic schema where a (parent, edge) name collides with a table name fails codegen with a message naming both — `cmd/sqlgen/gen/nested_names_test.go::TestValidateResolvedNames_NestedSurfaceCollisions`, six cases: two positive, four negative (the renamed child input, belongs-to, M2M, composite PK)
- [x] ~~Unit: each of the six presets resolves all four new fields non-nil~~ — **landed in 27.0a** (and it is **five** arms, not six)
- [x] ~~Failing-first: `create_with_related: true` with `create: false` fails `sqlgen validate` naming the key~~ — **landed in 27.0a** (D20)
- [x] **Added by `/verify` 2026-09-18, closing an UNTESTED gap:** `TestClientTemplates_NestedWiringFieldsEmitted` — both client-wiring fields are emitted from a conditional branch that **no test under `make check` exercised**. The `gen` golden fixture has no relationships, so the only fixture taking the branch is an E2E golden, which `go test -short` skips. That is 27.6's gate hole exactly: a deleted `{{ if .HasNestedSurface }}` block would have been invisible locally and failed only in CI. Asserts both directions on both templates; verified failing-first by gutting both blocks
- [x] **Added by `/verify` 2026-09-18:** `TestErrorTemplate_sentinelErrors` / `_constraintTypes` grown to name the two sentinels and the `NestedMutationError` alias. The alias had **golden-only** coverage — neither the `Contains` list nor the manifest drift guard named it

### Completion Record

**Files changed (18 outside the goldens).** Runtime: `database/errors.go` (+2 sentinels, `NestedMutationError`), `database/errors_test.go`. Codegen: new `cmd/sqlgen/gen/nested_names.go` + `nested_names_test.go`, `resolved_names.go` (the fourth key kind), `context.go` (2 fields on `TableContext`, 1 on `ClientContext`), `context_table.go` (the third cross-table pass), `context_client.go` (junction wires + `entityClientWires` extraction), `templates/error.go.tmpl`, `templates/api/errors.go.tmpl`, `templates/client.go.tmpl`, `templates/table/client.go.tmpl`, `api_errors_test.go`, `manifest/builder.go`. Docs: `docs/PRD.md` (§8.5, §9.9.5, §22.1, §30 manifest sample), `docs/design/MANIFEST.md`, `guidelines/ERRORS.md`.

**The new key kind found a live, systematic defect in PRD §9.9.5's own name scheme, and the fix was a PRD amendment rather than two config renames.** `Create<Parent><SingularEdge>Input` reconstructs the *target's own* `Create<T>Input` exactly whenever the child table is named `<parent>_<edge>` — which is the conventional name for a dependent table. Two shipped example schemas are that shape and **both failed codegen on the first run**: `users.Profile` over `user_profiles` (tenancy) and `binary_keys.Events` over `binary_key_events` (sqlite), each landing on a name the target table already declares. The two types are not interchangeable — the nested one has the traversed FK removed — so it is a redeclaration, not a coincidence. The design doc's "no fixture collides today" enumerated six (parent, edge) pairs and missed both; **A1** had already recorded that its edge census was incomplete.

**The decision was the user's, taken 2026-09-18: amend §9.9.5 to `<Parent><Edge>CreateInput`.** Moving the family marker from prefix to suffix puts the nested names in a namespace no table's derived name occupies, and matches how the two sibling verb blocks were already spelled (`<Parent><Edge>CreateNested` / `…UpdateNested`). The alternative — rename the two example tables via `struct_name` — was rejected because it pushes a rename onto every consumer with a conventionally-named child table. The name is still *checked*, because no identifier scheme is collision-free; what changed is that the check no longer fires on the common case. §9.9.5's table and its FK-elision paragraph are amended, with a new paragraph recording why the obvious spelling is wrong. **27.8 and 27.10 inherit the new spelling** — 27.8's task list is updated.

**M2M declaring no nested child input is what keeps every junction legal, and it is now pinned.** PRD §9.9.5 says the child input is shapes 2 and 3 only (an M2M `create` takes the target's ordinary input, there being no traversed FK to elide). Had M2M been included, `users.Categories` over `user_categories` would have collided on *every* M2M schema in existence — the same defect one level worse. `TestValidateResolvedNames_NestedSurfaceCollisions/an_M2M_edge_declares_no_nested_child_input` holds the line.

**Conditioning is unconditional and structural, by the user's direction and PRD §8.5's stated rule.** The claim reads the parent's key flowing down the edge (shapes 2/3/4) and a single-column parent PK (**E3**), and consults neither `nested_mutations.enabled`, nor the operations mask (**E4**), nor a `filter:` on the edge (**E2**), nor the §32 projection (**E6**) — so turning the feature on later can never turn a valid config invalid, exactly as for the cache and event-hook names. The set claimed is deliberately wider than the set the emitters will render; 27.8's `ValidateNestedWriteEligibility` asks the narrower question and is not a second implementation of this one. `TestValidateResolvedNames_NestedNamesAreNotFeatureGated` pins the gate being absent. §8.5 gains the carve-out paragraph, since the old Scope text pushed exactly this class out as "a `go build` error".

**The fourth key kind is checked against a *seeded* set, not merged into the existing one.** The nested names run against the entity primary names **and** the `Create<T>Input` / `Update<T>Input` names every table derives — the second half being what the registry could not see at all before. Folding the two namespaces together would have re-reported a `users` / `user` pair a second time under `CreateUserInput`, changing the text of errors that already work; `seededDuplicateClaims` contributes collisions without reporting one of its own, and `TestValidateResolvedNames_NestedClaimsDoNotDoubleReport` pins it. **The guarantee is narrower than "one rename, one error", and the review was right to say so:** it covers seed-vs-seed re-reporting, which is the regression risk. Two primary-name-colliding tables that *both* carry nested surfaces still report the same rename up to three times, because the pair keys genuinely differ (`table users` vs `nested mutations on table users` vs `relationship users.Events`). That is noise on an already-failing config, not a false negative, and collapsing it would mean merging claim kinds — the thing the seeding exists to avoid.

**`callbackMode`'s gate is wider than the three source documents say, deliberately.** The design doc's **F8**, this task list and `IMPLEMENTATION_ORDER.md` all say "every entity client with an M2M edge". That is **A7**'s generalization of F8's *first* gap (the junction client); the second gap — `callbackMode` living only on the unified client's options struct — was never generalized, because the only executor anyone had compiled was `userClient`'s. Semantically it belongs to every parent that will get a nested method: the companion's §3.5 passes `database.TxOptions{CallbackMode: c.callbackMode}` from `UpdateWithRelated`, which exists on O2M-only parents too. Gated on M2M alone, 27.8 would have had to widen it on its first O2M-only parent. It is therefore gated on `HasNestedSurface` — the same `nestedCandidateEdges` helper the name claim uses, so the two cannot drift. Junction clients stay per-M2M-edge, as written. `docs/tracker/IMPLEMENTATION_ORDER.md:3526` is annotated with both corrections, since `/phase` reads that file.

**Junction clients are deduped against the relationship target clients, from one slice.** A junction that is also a relationship target of its own parent — the usual shape when a junction carries payload columns — would otherwise declare the field twice. `RelationshipJunctionClients` is the single source for both the entity-client field list and the unified client's wire list, so a wire can never name a field that was not declared; `TestBuildTableContexts_JunctionClientDedupedAgainstTargets` pins the overlap being empty.

**Four pins were verified failing-first, each against the state that would produce the defect, not merely observed to pass.**

1. **The rename pin.** Reinstating `Create<Parent><SingularEdge>Input` in place makes `…/a_child_table_named_after_its_parent_and_edge_is_legal` report *`relationship users.Profile and table user_profiles both resolve to the Go type name "CreateUserProfileInput"`* — the synthetic fixture reproducing the real `tenancy` failure verbatim.
2. **The client-wiring pin.** Removing the `wireNestedMutationSurface` call reports `users.HasNestedSurface = false, want true` and `users.RelationshipJunctionClients = [], want [UserCategory]`.
3. **The `mapErrorToGQL` ordering pin.** Moving the two nested arms above the `ErrNotFound` arm reports `the nested-sentinel arms belong between the ErrNotFound arm (1551) and the *ConstraintError extraction (2069); got 1199`. The ordering is what keeps a `connect` visibility miss on `NOT_FOUND` rather than `BAD_REFERENCE` (§9.9.8, §26.5.5) — both arms match a wrapped error, so a reordering is silent to every containment assertion above it.
4. **The two collision cases** fail-first by construction: without the rule `ValidateGeneration` returns nil where the test demands an error.

**One consumer-facing surface was found by an existing guard rather than by this task list.** `TestErrorSentinelsMatchGeneratedPackage` compares `conventions.error_sentinels` against what `error.go.tmpl` declares, and failed on both new names — the manifest is what tells an agent which sentinels exist, so a re-export that skipped it would have advertised a stale set. Both rows carry their 1:1 GraphQL code (`CONFLICT`, `INVALID_INPUT`), joining `ErrNotFound` as the only three that have one. `ErrRefreshConcurrentlyInTx` stays absent on purpose: it is reached through the runtime `database` package and is not re-exported, which is the boundary §22.1 draws — a gap `guidelines/ERRORS.md` had, and which is fixed here alongside the two new rows.

**Sweeps.** `make check` clean across all 8 modules (37 packages, 0 lint issues). `make check-examples` green across all 12 example modules. `make update-golden` + `make update-golden-e2e` run after every template change, per the gate lesson 27.6 recorded.

**`/verify 27.7` (2026-09-18) found two gaps in this sub-item's own work and eight consistency defects, all fixed inside it.** The parent pass found the two gaps; an independent reviewer found the rest, most of them created by this sub-item's own PRD amendment.

1. **§4.13 had no row for the rule this sub-item added** — and §4.13 is in 27.7's own PRD reference list. The normative table of validation rules did not mention an error the code now returns. Added.
2. **The two client-wiring fields were emitted from a branch nothing under `make check` covered** — see Tests Required above. This is the finding worth carrying forward: adding a *conditional* template branch whose only exercising fixture is an E2E golden leaves it outside every local gate, because `make check` runs `go test -short` and `TestE2EGoldenFiles` skips under it.
3. **§22.3's code snippet double-attributed.** `fmt.Errorf("… with related: %s: %s: %w", edge, verb, err)` predates `NestedMutationError.Error()`; handed a `*NestedMutationError` it renders `Categories: connect: Categories: connect: 7:`. The snippet now shows both cases — the edge and verb are added for an ordinary inner error and **not** for one that already carries them.
4. **`Err`'s field comment said "ErrAlreadyRelated or ErrNestedVerbConflict"** in both PRD §22.2 and `database/errors.go`, while the amended §9.9.8, §22.3, `guidelines/ERRORS.md` and this sub-item's own test all establish `ErrNotFound` as a third carrier. §9.9.8 calls §22.2 the normative definition, so the normative text was the wrong one. Both fixed.
5. **Two stale "eight sentinels" counts** (§22.4, §30.4.1 twice) — the manifest builder and `guidelines/ERRORS.md` were updated when the count went to ten; the PRD was not. §22.4's unwrapping constraint now also names `NestedMutationError` beside `ConstraintError` on the `errors.As` half.
6. **27.10's task text still spelled `extensions.path: ["<edge>"]`** lowercase against the PRD's `["<Edge>"]` — a live forward task, not a historical record. All four sites now agree, anchored on §22.2's own definition of `Edge` as the Go field name.
7. **§8.5's carve-out was narrower than the code in two ways.** It never said the nested names are *also* run against the reserved generated-package set, so a `<Parent><Edge>CreateNested` equal to `Client` or `PageInfo` was an error no document described. And its justification — "enabling the feature later never turns a valid config invalid" — does not cover the class it lists: PRD §13.7 makes an **unreducible** `filter:` edge *permanently* non-nestable, so its three names can never be declared under any config. The over-claim is now stated as two causes, and the second is justified on its own terms — the alternative is a reservation rule that parses the raw SQL in `filter:`, which sqlgen deliberately never does. The code comment in `nested_names.go` carries the same split.
8. **`nestedSurfaceRef` carried no escape and should have.** The wrapper names are spelled from the parent's struct name alone, so `tables.<parent>.struct_name` is a single-key fix — but `pairHint` was offering only the *other* entity's rename, and §4.13's new guidance row said "rename the relationship", which does nothing for a name containing no edge. Both corrected; the test now pins that both escapes are offered.

**One hazard recorded rather than fixed, and moved onto 27.8's ticket.** `wireNestedMutationSurface` keys its junction lookup on the bare table name as well as the qualified one, mirroring `colsByTable`, so two same-named junctions in different schemas collide on the bare key and the last one wins — reachable only where a multi-schema project writes `junction:` unqualified. It is inert while the field is declared-and-unread, and 27.8's M2M executor is what makes it live.

**Not done here, and owed to 27.8.** `RelationshipContext` carries no `JunctionStructName`: the junction's struct name is resolved inside `wireNestedMutationSurface` and kept only as the per-table deduped list, which is all the client wiring needs. The M2M executor will want it per edge, and that is the sub-item that will know the shape it wants.

---

## 27.8 Go `CreateWithRelated` (Ticket F)

**PRD Reference:** §9.9, §29.4, §32.5

**Status:** **Complete** (landed 2026-09-18)

### Tasks

- [x] `Create<Parent>WithRelatedInput` containing `Create<Parent>Input` (**NW-D4** — contain, never restate) plus one optional `…CreateNested` block per eligible edge
- [x] `<Parent><Edge>CreateNested` — `create` + `connect` only; `disconnect` and `clear` are **not expressible** (**D4**)
- [x] `<Parent><Edge>CreateInput` — the child's create input minus the traversed FK (**C2**) and minus the discriminator (**D18b**) — *moved here from 27.2 (2026-09-17), where it was listed twice; the nested child input is introduced here.* **Spelling amended by 27.7 (2026-09-18)**: it was `Create<Parent><SingularEdge>Input` until the name registry found that spelling reconstructing the target's own `Create<T>Input` on any child table named `<parent>_<edge>` — live in two example schemas. `gen.nestedChildInputName` is the single source; do not respell it here
- [x] **D18(a)** — a nested `create` SETs the discriminator column to the edge's declared value. *Moved from 27.2 (2026-09-17): it is a property of the nested create executor, which this sub-item introduces. `RelationshipContext.Discriminator` already carries the column and the value's Go literal*
- [x] **D18(c)** — a `connect`'s visibility read carries the discriminator as an extra predicate, so a target whose value does not match the edge is `ErrNotFound` rather than a silent mislink. *Moved from 27.2 (2026-09-17) for the same reason*
- [x] One executor method per edge, called by all three families (**A11**), so **D3** is compiler-enforced
- [x] `ValidateNestedWriteEligibility` lint (**NW-D12**), wired into `sqlgen validate` as well as `sqlgen generate`
- [x] **D19** — a parent with zero eligible edges emits no nested surface at all
- [x] **D21** — a nil `FieldOptions` returns relationship members unpopulated
- [x] **Multi-schema junction lookup** — added 2026-09-18 from the 27.7 `/verify`. `wireNestedMutationSurface` (`nested_names.go`) keys its junction→struct-name map on both the bare and the schema-qualified table name, so two same-named junctions in different schemas collide on the bare key and the last one wins. Reachable only where a multi-schema project writes `junction:` unqualified. Harmless while the field is declared-and-unread; the M2M executor is what makes it live, so resolve it here — either qualify the edge's junction at context-build time or drop the bare key and require qualification
- [x] **Do not read `FKOnTarget == false` as "belongs-to"** — added 2026-09-17 from the 27.1 review. `false` is three answers in one: the FK is on the source, the edge is M2M, or the *target is not in the parsed schema*. The third is reachable — `configRelationshipToContext` carries a qualified `table: audit.documents` unsplit while config validation splits it, so the two disagree — and a write branch that takes the belongs-to arm on an unresolved target writes against a table it never found. Check the target separately before branching
- [x] **Extend the `guidelines/TESTING.md` §11 fixture paragraph the moment a template reads `FKOnTarget`** — added 2026-09-17 from the 27.1 review. Roughly a dozen hand-built `RelationshipContext` fixtures (`get_test.go`, `relationships_test.go`, `tenancy_template_test.go`, `model_test.go`, `field_options_test.go`, `stream_codegen_test.go`) would silently take the zero value — exactly the FIX-059 / FIX-068 / FIX-069 shape the §11 rule exists to prevent. No template reads the field today, so they are safe until this sub-item
- [x] **The emitter gates each `…WithRelated` on its *resolved* base operation** — added 2026-09-17 from the 27.0a review. **D20** catches an explicitly-set `create_with_related: true` beside `create: false`, but `applyOperationOverrides` does not propagate a base override onto its nested toggle, so `operations: {preset: all, create: false}` still resolves to `Create=false, CreateWithRelated=true` with no error — and §4.13 says that pair "would not compile". Validation deliberately stays quiet there (a config that never mentions nested mutations should not be rejected for a preset default), which makes the emitter the only remaining guard. `toResolvedOperations` is the natural home: `CreateWithRelated && Create`, and likewise for update and upsert

### Acceptance Criteria

- Emitted verb sets match the §4.1 matrix for every fixture edge. *(Corrected by `/verify` 2026-09-18: the criterion said **18**, and the `graphql` example carries **19** relationship members — this sub-item added `workspace_notes.DraftChildren`. The design doc's §4.1 census is stale in two further ways and is annotated rather than rewritten, being a historical record: it has no `DraftChildren` row, and it still lists `assets.Attachments` / `.Invoices` / `.PrimaryDocument` as emitting nothing, which 27.2's migration of those three from `filter:` to `discriminator:` made false.)*
- Query count matches **D12** — `ceil(N / batchSize)` per table per verb, plus the bounded read set; no per-row read. *(Scoped by `/verify` 2026-09-18: the `create` and `link` halves satisfy it by inheriting the target's `CreateMany` / the junction's `UpsertMany` batching. The `connect` visibility read and the adoption `UPDATE` each issue one statement for the whole id list — **Q9 batching is 27.9's task**, so the `ceil()` half of this criterion is met there, not here. What 27.8 does pin is the invariant underneath it: the count does not scale with the child count.)*
- `Profile` and `UserCredential` emit **no** `…WithRelated` method (belongs-to only).
- An explicitly-listed ineligible edge is a codegen hard error; an auto-included one is silently omitted.

### Tests Required

- [x] **Failing-first (D18b)**: the nested child input for a discriminator edge has no field for that column — *moved from 27.2 (2026-09-17)*
- [x] **Failing-first (D18c)**: `connect` on a target whose discriminator value differs → `ErrNotFound`, not a silent mislink — *moved from 27.2 (2026-09-17)*

- [x] E2E on all four shapes: O2M nullable (`User.Events`), O2M NOT NULL (`User.Orders`), M2M (`User.Categories`), has-one
- [x] **E2E on `Asset.Documents` / `Document.Assets`** — the only M2M edges with a UUID-PK target
- [x] Negative: a `filter:` edge emits no nested member
- [x] Query-count assertion via counting `Querier` per §25.1
- [x] Codegen: `D19` — a parent with no eligible edge emits nothing

### Completion Record

**Landed 2026-09-18.** `cmd/sqlgen/gen/context_nested.go` (new) resolves the surface, `templates/table/nested.go.tmpl` (new) emits it, and `cmd/sqlgen/testdata/examples/graphql` is the one example that turns the feature on — every write shape §9.9 has is reachable in that one schema. Seven parents get a `CreateWithRelated`: `assets`, `categories`, `documents`, `orders`, `products`, `users`, `workspace_notes`. `profiles` and `user_credentials` get none, which is the acceptance criterion: both carry belongs-to edges only.

**Eligibility is resolved in one place and asked twice.** `resolveNestedEdges` answers §9.9.4 for both the emitter and `ValidateNestedWriteEligibility`, so a `generate` that emits a surface and a `validate` that reports it clean cannot disagree — the FIX-158 defect class. Both run on the generate path *and* on the validate path, and the lint's two halves are separate code paths only in what they do with the result: an explicitly-listed edge that fails a rule is a hard error naming the edge, the rule id and the reason; an auto-included one is dropped. It is a post-build pass for the same reason `wireRelationshipFilters` is — eligibility reads the *target's* resolved operations mask, create input, filter members and conflict targets.

**Three narrowings the PRD did not state, all fail-closed, and all now written into §9.9.4 by the user's ruling of 2026-09-18.**

1. **Every M2M verb needs the junction's `upsert_many`, not just E9's two.** §9.9.6's link step is `<Junction>().UpsertMany(dedup(created ∪ connected))` and it runs for `create` as much as for `connect` — a junction without `upsert_many` therefore makes the whole edge unrenderable, where E9 named the key for `connect`/`disconnect` alone. **E9 is amended** to state the `upsert_many` half unscoped and keep `hard_delete` on the unlink verbs. `TestNestedEligibility_LinkStepNeedsJunctionUpsertMany` pins the first half beside `TestNestedEligibility_E9JunctionMaskDropsConnect` on the second.
2. **An M2M edge also needs a PK-covering conflict-target constant on its junction**, which the link step passes to `UpsertMany`; a junction whose uniqueness is app-enforced through `primary_key.columns` emits none (§9.5). **§9.9.4 gains E13**, spelled as E10's rule one level down, and the emitter's rule id moved from E10 to E13 to match.
3. **A target whose primary key is not a valid Go map key carries no `connect`.** The visibility read buckets its rows on that key and the adoption filters on it; the `comparator.Opaque` family (§11.2) is exactly the set that cannot — `[]byte`, `net.IP` and `net.HardwareAddr` are slices and `net.IPNet` holds two. **E8 is amended** to state it, and to state that this half is *reported* rather than dropped silently, because it is a generator limitation rather than a schema fact. An M2M edge additionally needs it for the link step's dedupe.

**`allow_reparent` is implemented here rather than left inert, and §9.9.6 gains what the flag's name does not imply.** The key landed in 27.0a and §9.9.6 already made it normative, so leaving it read-by-nothing would have been the accept-and-drop shape the separate input types exist to prevent. Two consequences were undocumented and are now written down: the middle outcome disappears (a visible target parented elsewhere is adopted, so `connect` is two-way and `ErrAlreadyRelated` is unreachable — §9.9.8's table says so), and the post-`UPDATE` shortfall reports `ErrNotFound`, because with no `fk IS NULL` term the only remaining cause is a row that stopped being visible. No example config sets it; `TestNestedEligibility_AllowReparentNeedsNoNullGuard` covers the rendered branch.

**The `assets` discriminator edges cannot reach D18(c), and one new fixture edge was added to make it reachable.** `documents.entity_id` is NOT NULL, so E8 leaves all three `create`-only and their visibility read is never emitted — the D18(c) task would have shipped with no runtime coverage. `workspace_notes.DraftChildren` (`fk: parent_id`, `discriminator: {column: kind, value: draft}`) is the only shape that closes it: polymorphic **and** over a nullable FK. It happens to also be self-referential and on a tenanted table, so one edge carries D18(a), D18(b), D18(c), E12 and §29.10's tenant propagation. Its cost is one relationship member on `workspace_notes` across the model, filter, field-options, GraphQL and manifest surfaces.

**Six pins verified failing-first against the state that produces the defect**, not observed to pass afterwards. D18(b) — removing the discriminator from the elision set makes the child input declare `action`. D18(c) at the template level — removing the `conditions` escape drops the predicate; **and at runtime the same removal returns `err = nil`**, which is the whole point of the rule: the connect silently relinks a note of another kind into an edge that can never read it back. E12 — removing the self-connect loop lets a row become its own parent. `Limit: new(0)` — removing it leaves 0 of 3 visibility reads bounded. D19 — dropping the `len(edges) == 0` gate gives four tables an empty nested surface.

**The query-count pin asserts the invariant, not a total.** §9.9.7's claim is "never a per-row read", so the test runs the same three-edge call with 2 children and again with 20 and requires the counts to be equal; a fixed expected total would be a sum over whatever the inner clients issue and would move on an unrelated change to any of them. A loose ceiling sits beside it so a regression that adds one round-trip per verb — which would keep the two runs equal — is still caught.

**Two hazards inherited from earlier sub-items were closed.** The multi-schema junction lookup is now a `tableIndex` that resolves in three steps — the schema the edge names, then the parent's own schema, then the bare name *only when it is unambiguous* — and it is used for the relationship target as well, which turned out to matter immediately: `configRelationshipToContext` carries a config `table:` unsplit, so the first run of the emitter silently dropped every config-declared edge (`assets.Attachments`, `assets.PrimaryDocument`, `workspace_notes.Children`) as "target not in the generated set". And `FKOnTarget` is never read as a belongs-to test outside `nestedCandidateEdges`: an unresolved target is rejected under E1 before any shape branch, so the third of its three answers cannot reach a write arm. `guidelines/TESTING.md` §11 gains the paragraph, with the reason it fails more quietly than the three FK fields already listed — it decides whether an edge is emitted at all, so a zero-value fixture passes for the wrong reason.

**The §9.9.5 edge ordering is by the relationship's resolved Go field name, which is a correction to 27.7's sort key.** `nestedCandidateEdges` ordered by the declared `Name`, and an auto-detected edge carries the pluralized snake form (`documents`) where a config-declared one carries what the consumer wrote (`Attachments`) — so ASCII-ordering the two spellings groups by origin rather than alphabetically, and `assets` emitted `Attachments, Invoices, PrimaryDocument, Documents`. `FieldName` is also the spelling §9.9.8 attributes errors with and §26.5.5 puts in `extensions.path`, so the three observable orderings now agree.

**Running the real `sqlgen validate` found a defect no unit test would have.** The lint and the attach pass both resolve the same edges for different purposes, and both were reporting, so every violation printed twice under `validate` and `generate` alike. Only `ValidateNestedWriteEligibility` reports now; `wireNestedMutations` returns nothing at all. It also runs on `BuildEntityContextsFromSchema`, the `sqlgen graphql gen` entry point that bypasses both `Generate` and `ValidateGeneration` — FIX-162's shape, and a check only they perform is one that path reports clean on. `TestNestedEligibility_ValidateReportsEachViolationOnce` pins the count rather than the presence, verified failing-first by restoring the second reporter.

**The executor's parameter type is the create-side block, and 27.9 widens it.** A11 asks for one executor per edge called by all three families; with one family shipped, the property is structural rather than observable. The executor is spelled `apply<Parent><Edge>Nested(ctx, parent, nested *<Parent><Edge>CreateNested, options)` and 27.9 changes that parameter to the update-side block, with `CreateWithRelated` widening at the call — which is sound precisely because `Disconnect` and `Clear` are absent **by type** (D4), so the widening cannot smuggle a verb in. Emitting the `…UpdateNested` blocks here instead would have shipped `disconnect` and `clear` fields no code reads, which is 27.9's first task. The `connect` body is written in its final form rather than a create-side subset: D7's three-way split is a property of the verb, not of the family, and on the create side the "already ours" arm is unreachable only because the parent is new.

**The auto-review found one fail-open case the three narrowings above missed, and it is a contradiction between two PRD rules rather than an oversight.** An M2M edge carrying a `discriminator:` set the predicate on the `connect` visibility read — the shape switch runs after `DiscColumn` is filled — and set it nowhere on the `create` arm, because §9.9.5 makes an M2M create take `Create<Target>Input` **unchanged**. The result was a nested create inserting a target the edge's own loader can never return, and then linking it: exactly the silent mislink D18(c) closes one arm over. Config permits `discriminator:` on `type: many_to_many` and `get.go.tmpl`'s M2M loader applies it, so the read shape is real. The two rules cannot both hold — honouring D18(a) means overwriting a field the caller can still set, which is §9.9.3's accept-and-drop — so the **write** surface is refused under E2 and the read surface is untouched. `TestNestedEligibility_M2MDiscriminatorIsNotWriteEligible` pins it, failing-first by deleting the guard. **The user's ruling, 2026-09-18: keep the refusal and write it into the spec** rather than narrowing the M2M input the way shapes 2 and 3 are narrowed. The narrowing would work — 27.7's respelling already removed the name collision that motivated M2M declaring no nested child input — but no schema declares such an edge, the refusal is fail-closed and loud, and going from refused to supported later is additive. **E2, §9.9.5, §13.4.1 and §26.12 are amended**; §13.4.1 needed it most, since its framing of `discriminator:` as the invertible alternative to `filter:` implied it makes *any* polymorphic edge writable.

**A verb the emitter cannot render is now reported instead of dropped, and that is a different fact from a verb the `verbs` mask turned off.** The mask is the consumer asking for a narrower surface and is silent by design; an unrenderable verb is a generator limitation invisible from the config. `nestedIneligible` gained a `verb` field: a note naming none is fatal and takes the edge, a note naming one leaves the edge with the verbs that do work and the lint reports it for an explicitly-listed edge. `blobs` — a `bytea` primary key, which filters through `comparator.Opaque` and cannot key a Go map — is the fixture: `create` survives, `connect` says why it did not. `nestedNullFilterExpr` was widened to the whole `comparator.Nullable…` family in the same pass; that one is **defensive, not a bug fix**, and the code says so — `applyConfigDeclaredFKs` stamps a synthetic `FKReference` on every config-declared `fk:`, so an edge's traversed column always classifies as a key and no schema reaches the other members. The review's reachability argument for it was checked by measurement and does not hold.

**Three hygiene findings from the same pass, all fixed in place.** `allow_reparent` required a filter member its own branch never uses, so an edge that opted into adopting *without* the `fk IS NULL` guard lost `connect` over the guard it had opted out of; and the post-`UPDATE` shortfall reported `ErrAlreadyRelated` under re-parenting, where the only remaining cause is a row that stopped being visible — it reports `ErrNotFound` now. The PK-covering conflict-target rejection was labelled E9 and is E10's reasoning one level down. `NestedEdgeContext.TargetPKStringExpr` and `RelationshipContext.JunctionStructName` were both written and never read — the second is 27.7's "owed to 27.8" item, which the `tableIndex` lookup made unnecessary — and both are gone rather than left as weight. The E12 test's comment claimed the refusal happens before any statement runs; the parent `INSERT` does run and is rolled back, which is what §9.9.6's step-2 placement means and what the comment says now.

**Two branches of the template are rendered by no example schema, and the unit fixture now covers both.** A has-one edge on a *nullable* FK is the only shape that renders the pointer `Connect`, and no schema in the repo has one; `allow_reparent` is set by no config. Both were emitted by nothing and checked by nothing — the sharper form of the "conditional branch whose only exercising fixture is an E2E golden" hole 27.7's `/verify` named. The `bios` table and a second allowlist pass close them, and `renderNested` now runs every rendered surface through `go/parser` (guidelines/TESTING.md §11) rather than only grepping it.

**Q9 batching is 27.9's, and the boundary is worth stating.** The `connect` visibility read and the adoption `UPDATE` both build one statement over the caller's whole id list; 27.9's task list owns the `c.batchSize` split across `connect`, `disconnect` **and** the visibility read. Nothing shipped reaches the ceiling — the only example that enables the feature is postgres, whose bind limit is 65535 against SQLite's 32766 — but a consumer on SQLite can, so the two tasks are a pair rather than an optimization deferred.

**Not done here, and booked on 27.9.** The manifest (§30) enumerates every mutation method with its `SQLBodies`, and `CreateWithRelated` is absent from it — an agent reading the manifest cannot discover the method. It is not on this sub-item's task list and the entry needs a shape decision `CreateWithRelated` does not fit: it has no single SQL body, being a composition of the statements its inner calls issue. `UpdateWithRelated` / `UpsertWithRelated` land in 27.9 with the same gap, so **all three take one decision and one landing there** — added to 27.9's task list 2026-09-18 by the user's direction, rather than retrofitted onto 27.8 separately.

**`/verify 27.8` (2026-09-18) returned 7/8 PRD PASS, 8/8 required tests present and 6/6 prior fixes confirmed, and every finding but one was closed inside the sub-item.** The one that was not is **FIX-220** (`blocking`): PRD §29.10 says nested children inherit the §29.6 ctx-cached tenant, and they do not — each generated mutation stashes the resolved tenant on its *own* local ctx, which never escapes back to the transaction closure, so a two-edge nested create invokes the consumer's `TenantResolver` eight times where §29.6 permits one. Behaviourally invisible for a pure resolver, which is why nothing caught it; it needs a design answer first, because the parent client of a *shared* parent with *tenanted* children has no resolver field to call. 27.9's task list already books the test that fails against it.

**Eight findings were fixed in place.** Three were code: `nestedEdgeImports` walked `ChildFields` only, which under `file_per_table` under-collects exactly on an M2M edge, where `ChildFields` is empty by construction and the executor still names the target's PK type and the FK's column type — each edge now carries its own `Imports`. A schema-qualified config `table:` was rejected with a *false* diagnostic ("not in the generated set" for a table that is generated), and now says which of the two it is. And `resolveNestedConnectKey` cited E8 on M2M edges, where E8 is scoped to O2M / has-one.

Three were the PRD amendments this sub-item made, checked against the code they describe. §9.9.4's new intro claimed both E8's and E9's verb-scoped drops are reported; only E8's primary-key clause is, and the rule that actually governs is *whether the consumer could have seen it coming from their own config* — a `hard_delete: false` or a NOT NULL FK is config-visible and silent, a generator limitation is reported. §9.9.6 never said the terminal re-read runs with hooks skipped, though every emitted method does it and §9.9.9 reads as though it does not; it says so now, with the reason and with the §9.6 implication that the re-read is therefore never cached. And §4.6's `upsert_many` row still scoped E9's requirement to `connect`.

Two were stale censuses, annotated rather than rewritten: this sub-item's own acceptance criterion said 18 fixture edges where the example now carries 19, and the design doc's §4.1 matrix predates both 27.2's migration of the three `assets` discriminator edges and this sub-item's `DraftChildren`.

**The coverage finding was the transferable one, and it is 27.7's lesson recurring for the third time.** `BuildSharedTypesContext` gained a `hasNested` parameter that every unit call site passes `false`, so both new helpers were exercised by an E2E golden alone — and only by the *tenanted* one, since `graphql` is the single example that enables the feature. `nestedChildOptions`'s body has two conditional lines, which means the **non-tenanted body was rendered by nothing**. `TestNestedSharedHelpers_BodiesVaryWithTenancy` covers all three package shapes and asserts the two fields that must *never* cross a table boundary alongside the ones that must.

**A second `/verify 27.8` (2026-09-18, after FIX-220 landed) found one behavioural defect the first pass missed, and it is fixed here rather than booked forward.** A **repeated id in a `connect` list reported `ErrAlreadyRelated` against a row that had no other parent**. The adoption verifies itself by comparing the `UPDATE`'s row count against the queued id count, and the `UPDATE` matches the row once however many times the caller named it — so `connect: [x, x]` queued two, updated one, and fell into the shortfall branch. `comparator.parseIn` does not dedupe either, so the `= ANY($1)` array simply carried the id twice. That is precisely the *"fewer rows updated than requested → conflict"* collapse §9.9.6 forbids, arriving through the caller's input instead of through the guard — and it reported the conflict with **no id attached**, since the shortfall branch is not id-scoped. The M2M link step had always deduped (its `seen` set); the O2M / has-one arm had not, and the asymmetry was undocumented. `connect` is a set operation, so naming a target twice states the same end state twice: the adoption now dedupes into `queued` after the three-way split, which keeps a genuine `ErrAlreadyRelated` on a repeated *parented* id while making a repeated *orphan* id one adoption. **Confirmed failing-first against a real PostgreSQL container** — the pre-fix generated client returns `create user with related: Events: connect: sqlgen: target already related to another parent` for `[x, x]` where `[x]` succeeds. `TestCreateWithRelated_RepeatedConnectIDIsOneAdoption` pins both halves.

**Four further findings from the same pass, all fixed in place.** The M2M `connect` visibility read still carried a `{{ if .DiscColumn }}` arm emitting the discriminator predicate, which became unreachable the moment E2 started refusing M2M edges that declare one — it rendered in no example and would have fail-opened on the *create* arm the day E2 is lifted, so it is gone, with the reasoning kept as a template-only comment rather than eight lines repeated into every emitted M2M edge. `NestedMutationError.Verb`'s doc enumerated four verbs where the emitter also produces `"link"`, a value that reaches consumers on the struct and, per §26.5.5, the GraphQL error path. `HasTenantedNestedEdge`'s doc cited `NestedEdgeContext.TargetTenanted`, a field that does not exist under that name (`TargetResolvesTenant`). And the "no verb survives" rejection hard-coded rule id **E8** — a nullable-FK rule — when the causes compose: `create` is lost only to the §4.6 `verbs` mask while `connect` can be lost to that mask, to E8, to E9 or to an unrenderable key, so naming any single rule misattributed the refusal in every combination but one. It now cites §9.9.3's matrix, and `nestedIneligible.String` renders an empty rule id that way generally. The report's remaining observations — per-edge `Imports` being rendered by no `file_per_table` golden, and M2M generated-PK derivation on non-`RETURNING` dialects — are pre-existing coverage gaps rather than 27.8 defects and are **not** fixed here.

**One guideline violation was found by the parent and fixed.** Six resolvers returned `*nestedIneligible` as a second return value — literally the `(*T, *ConcreteError)` shape `guidelines/GO.md` names as the typed-nil trap. No caller converted it to `error`, so nothing was broken, but the trap was one future call site away. `nestedIneligible` is now a `fmt.Stringer` rather than an `error`, which makes the conversion unrepresentable.

**Two items were recorded and not acted on.** The adoption `UPDATE` carries no discriminator predicate where the visibility read does, so a target whose discriminator changes between the two is adopted into the wrong edge — a window narrower than the one §29.4.2a already accepts for the visibility read itself. And no unit fixture exercises a shape-2/3 nested child input into a table with `access`-classified columns; `users` is the only such table in the fixture and is reachable only as an M2M target, where no child input is declared.

**Files.** New: `cmd/sqlgen/gen/context_nested.go`, `cmd/sqlgen/gen/templates/table/nested.go.tmpl`, `cmd/sqlgen/gen/nested_test.go`, `…/examples/graphql/tests/nested_mutations_test.go`, `…/examples/graphql/tests/nested_mutations_polymorphic_test.go`. Modified: `context.go` (`NestedContext` / `NestedEdgeContext`, `TableContext.Nested`, three `ResolvedOperations` fields, `RelationshipContext.JunctionStructName`), `context_table.go` (`toResolvedOperations`), `context_shared.go` (`nestedChildOptions` / `nestedError`, helpers now emitted in name order), `nested_names.go` (`tableIndex`, the `FieldName` sort key), `orchestrate.go` (three wiring points), `templates/table/client.go.tmpl`, `guidelines/TESTING.md`, and the `graphql` example's `sqlgen.yml` plus its goldens.

---

## 27.9 Go `UpdateWithRelated` + `UpsertWithRelated` (Ticket G)

**PRD Reference:** §9.9, §25.1, §22.1, §29.4

**Status:** **Complete** (landed 2026-09-18)
**Blocked by:** nothing — **unblocked as of 2026-09-18**. ~~FIX-208~~ (*resolved* — the `OpUpdateWhere` cache arm), ~~FIX-220~~ (*resolved 2026-09-18* — the §29.6 single-resolve hoist now lives in `nested.go.tmpl`, and 27.9's two methods inherit it along with the corrected savepoint-safe transaction name), ~~27.3~~, ~~27.8~~ (both complete)

### Tasks

- [x] `<Parent><Edge>UpdateNested` — adds `disconnect` and `clear` to the create-side block
- [x] `UpsertWithRelated` reuses the **update** block verbatim (**D3**); no insert-vs-update branch detection
- [x] `clear` runs **first** (**D17**), and `disconnect` is skipped entirely when it ran
- [x] **D7** — O2M `connect` adopts only unparented rows, reporting three outcomes: not visible → `ErrNotFound`; parented elsewhere → `ErrAlreadyRelated`; already ours → no-op
- [x] **D13** — naming one target under two verbs is a validation error before any statement runs
- [x] **E12** — a self-referential `connect`/`disconnect` naming the parent's own id is refused
- [x] **Q9 batching** at `c.batchSize` on `connect`, `disconnect` **and the `connect` visibility read**
- [x] `Limit: new(0)` on every visibility read (**A10**) — normative, not decoration
- [x] The unlink assignment uses `omittable.Set[*T](nil)`, never the zero `omittable.Value` (**F11**)
- [x] **The manifest's three nested methods** — added 2026-09-18 from the 27.8 review. `CreateWithRelated` landed in 27.8 and is absent from the §30 manifest, which enumerates every other mutation method: an agent reading the manifest cannot discover it. The entry needs a shape decision the existing one does not fit — a nested method has no single SQL body, being a composition of the statements its inner calls issue — so `SQLBodies` is either empty, or carries the inner statements in execution order, or the entry declares a different field naming the methods it composes. 27.9 lands `UpdateWithRelated` and `UpsertWithRelated` with the same gap, so **all three get one decision and one landing here**, not 27.8's retrofitted separately. The eligibility facts the entry would carry are already resolved on `TableContext.Nested` (the edges, their verbs, their target and junction clients), so this is a manifest-builder change rather than a new resolution

### Acceptance Criteria

- Every §4.1 matrix cell behaves as specified, including the **D8** negatives (no `connect`/`disconnect` emitted on a NOT NULL FK edge).
- `clear` + `connect` produces exactly the submitted set — the `set` verb's end state without shipping `set`.
- Query count per **D12**; `clear` is exactly 1 statement and takes no read.
- Error attribution is per edge and per verb, matching §4.4's format.

### Tests Required

- [x] E2E per §4.1 cell across all four shapes
- [x] **F11 runtime pin (failing-first)**: unlink a child, then assert its FK is actually NULL. The wrong spelling compiles, matches its rows and reports success, so this cannot be a compile-time check
- [x] **Q9 pin**: a `disconnect` naming more ids than `batchSize` issues `ceil(N/batchSize)` statements and succeeds — sized past **32766**, the SQLite ceiling, so it fails on a single-statement implementation
- [x] **D7** three-way: not-visible → `ErrNotFound`; parented elsewhere → `ErrAlreadyRelated`; already ours → no-op
- [x] **D13**: `connect` ∩ `disconnect` → `ErrNestedVerbConflict` before any statement
- [x] **E12**: self-connect on `WorkspaceNote.Children` → refused
- [x] Tenancy: nested children inherit the ctx-cached tenant; `SkipTenancy` propagates. **The `CreateWithRelated` half already passes** — FIX-220 landed the hoist and `TestCreateWithRelated_ResolvesTheTenantOnce` pins one resolve per nested mutation (plus zero under `SkipTenancy` and under an explicit tenant). 27.9 extends the same pin to `UpdateWithRelated` / `UpsertWithRelated`, which inherit the hoist from the shared template
- [x] **Savepoint composition** — added 2026-09-18 from FIX-220. Each new method passes its own name to `database.WithTransaction`, and that name doubles as the SAVEPOINT identifier inside a caller's transaction, so it must be a bare SQL identifier. Use `update_<snake>_with_related` / `upsert_<snake>_with_related` and extend `TestCreateWithRelated_ComposesWithACallerTransaction` to both
- [x] Idempotence: running the same `UpsertWithRelated` payload twice yields the same end state

### Completion Record

**Landed 2026-09-18.** `cmd/sqlgen/gen/templates/table/nested.go.tmpl` gains the update-side block,
the two wrapper inputs and the two methods; `cmd/sqlgen/gen/context_nested.go` resolves the unlink
half. All seven parents in the `graphql` example carry the full trio. The executor is the one each
family calls, so **D3** is compiler-enforced rather than conventional: its parameter widened from
the create-side block to the update-side one, and `CreateWithRelated` lifts its own block at the
call through a per-edge `…NestedFromCreate` — sound precisely because `Disconnect` and `Clear` are
absent from the create-side type by construction (**D4**), so the widening cannot smuggle a verb in.

**The family gate moved off `Operations` and onto `NestedContext`.** 27.8 skipped the whole nested
surface when `create_with_related` was off, which was right while one family existed and wrong the
moment there were three: `operations: {create: false}` would have taken `UpdateWithRelated` with it.
`EmitCreate` / `EmitUpdate` / `EmitUpsert` are each the §4.6 `operations` mask ∩ the table's own
resolved toggle, and the parent emits nothing only when all three are off. **E10's second clause
lands here too**: `UpsertWithRelated` takes a `<Parent>ConflictTarget` argument, and a table whose
uniqueness is app-enforced through `primary_key.columns` emits no such constant (§9.5) — so the
upsert family alone is dropped rather than the surface.

**D18 is extended to the unlink verbs, and this is the one rule the PRD does not state.** `clear`
unlinks every row *on this edge*, and a polymorphic edge is `fk = parent AND <disc> = <value>`.
Without the second term a `clear` on `workspace_notes.DraftChildren` unlinks the `published`
children too — rows the caller never named, reported as success. That is the same silent-mislink
family D18(a) closes on `create` and D18(c) on the `connect` visibility read, in its sharpest form:
`Children` and `DraftChildren` share `parent_id`, so the fixture reaches it. Both unlink verbs carry
the predicate now, **and so does the `connect` adoption UPDATE** — which closes the hazard 27.8
recorded and did not act on (the visibility read carried the predicate and the UPDATE did not, so a
target whose discriminator changed between the two was adopted into an edge that can never read it
back).

**§9.9.6 and §13.4.1 are amended, by the user's ruling of 2026-09-18** — the same shape as 27.8's
four narrowings. §9.9.6's execution-order block marks the term on all four statements that carry it
and gains the paragraph saying why dropping it from a *write* is a wrong match rather than a wider
one; §13.4.1's write-side ownership list goes from three rules to four, with the fourth stated as
rule 3 inverted — rule 3 stops a row entering an edge that cannot read it, rule 4 stops rows
*leaving* an edge that was never writing to them. Both state the fail-closed half too: the read's
predicate is built from the column and the three write statements' from the target's filter member,
so an edge can be renderable for one and not the other, and a verb whose scope cannot be rendered is
dropped and reported rather than emitted unscoped.

**Two stale sentences in §9.9.4 were corrected alongside, both made wrong by this sub-item's own
work.** E8 named a nullable FK and a map-keyable target's primary key, scoping the second to
`connect`; `disconnect` needs the same key (D13's two-verb check buckets what each verb names) and
all three unlink-or-adopt verbs need the target's `operations.update_where`, so the row states both.
And the preamble's census of what gets *reported* said "E8's primary-key clause is the only one
today", which 27.8 had already outgrown and 27.9 outgrows further — it names the three shapes that
reach it instead of counting them, and its list of config-visible facts now says "an `operations`
entry the target or the junction turned off" rather than naming `hard_delete` alone.

**E11 is implemented for the first time**, because 27.8 had no verb it could apply to: an M2M
junction carrying a soft-delete column loses `disconnect` and `clear` and keeps `create` and
`connect`. No example junction has the column, so `user_tags` was added to the gen unit fixture —
without it the rule would be code no test reaches. An M2M `disconnect` additionally needs the
junction keyed on exactly its two foreign keys, since `HardDeleteMany` takes the junction's own key
and a surrogate-keyed junction gives the executor no pair to build.

**One latent gap in 27.8 was closed in passing**: neither the adoption nor the unlink checked
`target.Operations.UpdateWhere`, so a target with that operation masked off emitted a `connect`
calling a method that does not exist. It is the same E4 rule the create verbs already answer.

**Q9 batching covers all four id-list statements** — the `connect` visibility read, the adoption
UPDATE, the O2M `disconnect` UPDATE and the M2M junction delete — and the adoption's shortfall check
sums across chunks, because a row lost in one chunk is a lost race whichever chunk it landed in.
`clear` is exempt by construction: it names no ids.

**The query-count pin 27.8 shipped was vacuous, and the Q9 pin is what found it.** `countingQuerier`
wraps a `database.Querier`, and `database.Conn` hands a transaction body the `*database.Tx` — which
holds the driver connection the shim delegated to when it answered `Begin`. Every statement between
BEGIN and COMMIT therefore bypassed it, so `TestCreateWithRelated_QueryCountDoesNotScaleWithChildren`
was comparing 1 against 1. A pgx `QueryTracer` on its own pool (`statement_counter_test.go`) sits
below that boundary; both pins use it now, and the 27.8 one gained a floor so it cannot go vacuous
again.

**Four pins were verified failing-first against the state that produces the defect.** F11 — swapping
the assignment for the zero `omittable.Value` leaves the FK pointing at the parent while the call
returns `nil`, on both `disconnect` and `clear`. D18(d) — removing the `Kind:` term detaches the
`published` child under a `DraftChildren` clear, and again under a disconnect that names it. D13 —
removing the check returns `err = nil` for both halves, execution order resolving the contradiction
silently. E12's disconnect half — removing the guard also returns `err = nil`, because the unlink's
own `fk = parent` term matches nothing on a row that is not yet its own parent, so the refusal is
the only thing that reports the mistake.

**The manifest entry carries no `sql_bodies`, and that is the existing shape rather than a new one.**
A nested method issues no statement of its own — it composes its base operation and, per edge, the
target's and junction's own client methods — and PRD §30.4.2 already fixes what the manifest does
with such a method: "no sql_bodies key at all rather than a plausible-looking invention", which is
why `Paginate` and `Connection` carry none and MCP answers `-32005 METHOD_SQL_UNAVAILABLE` for them.
Listing the inner statements would publish SQL this body does not contain under its name; a new
field naming the composed methods would change the JSON schema (§30.5) to say what `notes` says. The
note names the edges, which is the part an agent cannot get elsewhere, and `errors[]` is read off
this parent's own verb sets rather than from the feature — a parent whose every edge is create-only
advertises neither `ErrAlreadyRelated` nor `ErrNestedVerbConflict`.

**FIX-221 (`blocking`) was found here and deliberately not fixed here.** `loadRelationships` fans its
per-edge reads across an unbounded errgroup; inside a transaction they all run on one driver
connection, so selecting ≥ 2 relationships fails with pgx `conn busy` and the failed rollback then
destroys the transaction. Measured pre-existing at HEAD on two probes: 27.8's shipped
`CreateWithRelated` with three relationships selected, and a plain `Get` with the same three inside
a caller `WithTx`. It bites the nested surface hardest — §9.9.6 step 3 places the terminal re-read
inside the method's own transaction, so §9.9.5's "select a relationship and get it back" fails
always for a parent with two selected edges — but the fix lands in `get.go.tmpl` and moves every
example's `models_gen.go`, which is outside this sub-item. **User direction 2026-09-18: file it,
keep 27.9 scoped.** `TestUpdateWithRelated_EveryMatrixCell` reads each edge back through its own
`Get` outside the nested call, the pattern `TestCreateWithRelated_AllFourShapes` already uses
deliberately; **the multi-edge terminal re-read is covered by nothing on either side until FIX-221
is resolved.** **Resolved 2026-09-19**, outside 27.9 as planned: the loader now bounds its errgroup
to one worker inside a transaction (`get.go.tmpl`), the PRD's false `Tx`-concurrency contract
(§18.5/§20.5) was corrected alongside it, and that re-read gained coverage on both the create and
the update side in `relationship_tx_fanout_test.go`.

**The auto-review found five items; three were code defects fixed inside the sub-item.** **(1)** A
`disconnect` on a target whose primary key is in the `comparator.Opaque` family emitted
`map[[]byte]bool` and **would not compile** — D13's verb-conflict check buckets the named ids in a
set, and the map-key guard was scoped to the self-referential E12 case alone. The fixture reaches it
(`blobs`, a `bytea` PK on a nullable FK) and this sub-item's own census test *asserted the bug*,
because `renderNested` parses the emitted Go without type-checking it. The guard is unconditional
now — `connect` and `disconnect` both need the property, `clear` names no ids and survives — and
`TestNestedEligibility_IDListVerbsNeedAKeyableTarget` asserts it over every fixture edge against the
resolver's own predicate rather than restating §11.2's family. **(2)** `update_where: false` on a
target was rejecting the *whole edge*, which made it a hard build error for any edge named in an
allowlist; E4 scopes to `create` / `create_many`, `create` routes through `CreateMany` and is
unaffected, and an `operations` mask entry is a config-visible fact §9.9.4 makes silent — so it
narrows the verb set silently now, the way the M2M sibling already did. **(3)** The `connect`
adoption could still lose its D18(d) term where the unlink half refused: the read's predicate is raw
SQL keyed on the column and the UPDATE's a filter member, so the two disagree about renderability.
`resolveNestedAdoption` refuses `connect` on the same condition now. The remaining two findings were
this record's own gaps — the PRD amendment above, which had been recorded as prose with no tracked
landing, and one sentence in FIX-221 describing a read-back pattern the test does not use.

**Files.** New: `…/examples/graphql/tests/nested_mutations_update_test.go`,
`…/examples/graphql/tests/statement_counter_test.go`. Modified: `docs/PRD.md` (§9.9.4, §9.9.6, §13.4.1), `gen/context.go` (`NestedContext`
gains the two wrapper names and the three `Emit*` gates; `NestedEdgeContext` the update-side block,
the unlink expressions and the junction PK members), `gen/context_nested.go`,
`gen/templates/table/nested.go.tmpl`, `gen/templates/table/client.go.tmpl`, `gen/nested_test.go`,
`manifest/builder.go`, the `graphql` example's goldens, and `…/tests/{main_test,nested_mutations_test,nested_mutations_polymorphic_test}.go`.

---

## 27.9a Nested mutations on MySQL — the non-`RETURNING` write path

**PRD Reference:** §9.9.6, §9.9.7, §18.3, §9.2

**Status:** **Complete** (landed 2026-09-22; defect 2's fix reworked 2026-09-23)
**Blocked by:** nothing — ~~27.9~~ (complete 2026-09-18)

**Why MySQL alone, and why this is not a per-dialect matrix.** PostgreSQL and SQLite both support `RETURNING`, so on both of them every nested inner call learns which rows it wrote *from the statement that wrote them*. MySQL does not, and the generated client compensates with two mechanisms that have no counterpart on the other two dialects: `collectAffectedIDs` **pre-SELECTs** the matching rows before an `UpdateWhere` / `DeleteWhere`, and `multiInsertAndResolveIDs` **derives** a batch's generated keys arithmetically as `firstID + i` rather than reading them back. The nested executor leans on both — the adoption's shortfall check counts what `UpdateWhere` returned, and the M2M link step keys its junction rows on what `CreateMany` returned — so MySQL is the one dialect where 27.8's and 27.9's correctness arguments rest on a different mechanism than the one they were written against and tested on. A third dialect's worth of coverage on SQLite would re-test the PostgreSQL path under a different file; this sub-item tests the path that is actually different.

**The `graphql` example cannot carry this**, being PostgreSQL, and it is the only example that turns the feature on. The fixture work is therefore part of the sub-item rather than a precondition to it.

### Tasks

- [x] **Turn `nested_mutations` on in the `mysql` example** (`cmd/sqlgen/testdata/examples/mysql/sqlgen.yml`). `users` already carries the three shapes `graphql`'s `User` does — `Profile` (has-one), `Orders` (O2M, NOT NULL FK) and `Categories` (M2M through `user_categories`) — so no edge needs inventing for shapes 2, 3 and 4. Expect golden movement across the whole `mysql` tree in both `models/` and `expected/`
- [x] **Add one nullable-FK O2M edge**, mirroring `graphql`'s `events.user_id`. Every FK in the `mysql` schema is `NOT NULL` today, so **E8 leaves every O2M and has-one edge `create`-only** and the adoption path — the three-way split, the `fk IS NULL` guard, `disconnect`, `clear` — is unreachable. That path is the *whole* reason this sub-item exists on MySQL, so a new table with a nullable FK is required rather than optional. Prefer a new `user_events` table over widening `orders.user_id`, which would change the semantics of existing `mysql` tests
- [x] **Confirm E9 / E13 hold for `user_categories`** before relying on the M2M verbs: the junction needs `upsert_many` (default `true`) for every verb, `hard_delete` for `connect` / `disconnect`, and a PK-covering `<Junction>ConflictTarget` constant for the link step. It has a real composite `PRIMARY KEY (user_id, category_id)`, so the constant should emit — but `ValidateNestedWriteEligibility` is the arbiter and the answer belongs in the record, not in an assumption. *Answered 2026-09-22: E9 and E13 both hold (the listed edge validated clean, and `upsert_many: false` on the junction turned it into the E9 hard error). The edge is nonetheless **ineligible** now, under E5 as this sub-item tightened it, because the junction carries `slot`. See the completion record.*
- [x] **Pin the `firstID + i` key derivation through a nested M2M `create`.** This is the highest-consequence MySQL divergence: the link step writes `Create<Junction>Input{..., ReferenceField: tid}` from the ids `CreateMany` returned, so a derivation that is off by one links the parent to the **wrong target row** and reports success. Nothing in 27.8 or 27.9 can catch it, since both are PostgreSQL-only. Assert the junction rows name the targets that were actually inserted, not merely that the right *number* of them exist. *Pinned through `assets.Documents`, since E5 took `users.Categories`.*
- [x] **Record what the adoption's shortfall check can and cannot detect on MySQL.** Under `RETURNING`, `len(adopted) != len(adopt)` means the `fk IS NULL` guard rejected a row that was unparented at read time — a lost race, reported as `ErrAlreadyRelated` (PRD §9.9.6). Under `collectAffectedIDs` the rows are selected *before* the `UPDATE`, so a row parented in between is counted as affected and the check cannot see it. Establish by measurement whether this is a real behavioural gap on MySQL or whether the pre-SELECT and the `UPDATE` share a transaction that closes it; if it is real, it is a PRD-accuracy question (§9.9.6 states the guarantee unconditionally) and needs a ruling, not a silent narrowing. *Measured real. The user first ruled for the PostgreSQL-like fix, a locking pre-SELECT. On 2026-09-23, after the lock's gap-lock cost was measured, the ruling was revised: the pre-SELECT is plain again, and the adoption counts a verify read instead. See defect 2 in the completion record.*

### Acceptance Criteria

- The full lifecycle runs on MySQL in **one caller transaction**: `CreateWithRelated` → `UpdateWithRelated` → `UpsertWithRelated` → `DeleteMany`, with the end state asserted after each step rather than only at the end.
- Every step composes as a **savepoint** (PRD §18.3), since the caller already holds the transaction — which also pins that each method's transaction name is a bare SQL identifier on MySQL, not only on PostgreSQL.
- M2M junction rows name the targets that were actually inserted (the `firstID + i` pin above).
- The O2M adoption path is exercised end to end: `create`, `connect` on an orphan, `disconnect`, and `clear`.
- No behavioural claim in §9.9.6 is true on PostgreSQL and false on MySQL without the PRD saying so.

### Tests Required

- [x] **The lifecycle test itself**, in `cmd/sqlgen/testdata/examples/mysql/tests/` — one caller transaction, the four calls in order, state asserted between each
- [x] **M2M target identity through the link step** (failing-first against a deliberately perturbed derivation, since an off-by-one still produces the right row count)
- [x] **O2M adoption on MySQL**: orphan → `connect` adopts; already-ours → no-op; parented elsewhere → `ErrAlreadyRelated`; then `disconnect` and `clear` return the FK to NULL — the §9.9.6 spelling that actually sets NULL rather than omitting the column (**F11**)
- [x] **Savepoint composition** — the whole lifecycle inside a caller `Begin` / `WithTx`, and a failure in the last step rolling back only its own savepoint
- [x] **Query count** on MySQL per §9.9.7 — the pre-SELECT in `collectAffectedIDs` is an extra statement the PostgreSQL path does not issue, so the bound must be restated for this dialect rather than assumed to carry over

### Completion Record

**Landed 2026-09-22.** The `mysql` example now enables `nested_mutations`, gains one nullable-FK
child table (`user_events`), and lists every nestable `users` edge by name, so an eligibility loss
fails generation instead of silently removing the surface. It has two new test files:
`tests/nested_mutations_test.go` (the lifecycle, M2M target identity, the O2M adoption path and
savepoint composition) and `tests/nested_query_count_test.go` (§9.9.7 restated for MySQL). The
sub-item existed to test the non-`RETURNING` mechanisms rather than re-run PostgreSQL's, and doing
that turned up **three generator defects and one PRD misstatement**. Each was measured before it
was changed. Each one that needed a design choice went to the user for a ruling.

**1. A has-one edge whose target nothing else reaches did not compile.** This was found the first
time the `mysql` tree was generated: `c.profileClient undefined`. The nested `create` on a has-one
edge writes through the target's client, but the entity client declared only O2M and M2M target
clients (the loaders' list) plus M2M junctions. `graphql`'s only has-one edge,
`assets.PrimaryDocument`, hid this, because `documents` is also the O2M `Attachments` target and
that edge declared the field. The unit fixture's `Profile` and `Bio` reached it, but `renderNested`
parses without type-checking. `RelationshipJunctionClients` is now `NestedWriteClients` and also
carries has-one targets. Fields and wires both come from that one slice, as before.
`TestNestedWiring_EveryExecutorClientIsDeclaredAndWired` asserts that every `c.<x>Client` an
executor names is both declared and wired, and it failed first on exactly `Bio` and `Profile`. The
field is structural, as the junction clients already were, so five trees with a has-one edge
(`cache`, `postgres`, `postgres_stdlib`, `sqlite`, `tenancy`) each gain one field and one wire.

**2. The adoption's shortfall check could not see a lost race on MySQL. This is the measurement
the task list asked for, and the gap was real.** A mutation hook on the target client parented the
row from a second connection between the visibility read and the adoption `UPDATE`. Under MySQL's
default REPEATABLE READ, `UpdateWithRelated` returned **`nil`** and the row stayed with the other
parent. The cause: `collectAffectedIDs` read from the transaction's snapshot, where the row was
still unparented, while the `UPDATE` is a current read and skipped it, so the two counts agreed.
Under READ COMMITTED the same interleaving was already caught, since each read takes a fresh
snapshot. **User ruling 2026-09-22: take the approach closest to PostgreSQL's.** All eight
MySQL-only pre-SELECT helpers (`collectAffectedIDs` / `collectAffectedPKs`, in both the update and
delete templates) were made to read `FOR UPDATE`. Inside a transaction, every `*Where` method then
reported exactly the rows it wrote, which is what `RETURNING` gives the other two dialects. That
fixed the nested race and also `AffectedPKs` for cache invalidation and events. Statement count was
unchanged, and the probe returned `ErrAlreadyRelated` under both isolation levels. *(Withdrawn
2026-09-23; see "Ruling revised" below.)*
**Residual, recorded rather than fixed:** a flat `*Where` call *outside* a transaction still runs
the pre-SELECT and the write as two separate autocommits. §9.8.6b states that the guarantee holds
only inside a transaction. Every nested mutation runs inside one. The
`a lost race is ErrAlreadyRelated` subtest failed first (`err = <nil>`) with the lock removed from
the generated `userEventClient.collectAffectedIDs`.

**Ruling revised 2026-09-23, after the lock's cost was measured.** The lock fixed a real, measured
bug, but it had a measured cost. When the pre-SELECT matches **nothing**, `FOR UPDATE` under
REPEATABLE READ takes a gap lock and holds it until commit. The plain read took no lock, and the
`UPDATE` it skipped took none either. That covers `clear` on an empty edge, which every
`UpsertWithRelated` insert branch with `clear` reaches, and a `disconnect` of unlinked ids. Measured
on the container: another connection's `INSERT` into that edge, and into a neighbouring parent's
edge, waited until `innodb_lock_wait_timeout`. A further consequence is **derived, not measured**.
Two concurrent such calls can each take the gap lock, since gap locks are compatible, and each then
wait on the other to insert, since insert-intention locks conflict with them. InnoDB rolls one back,
and the caller sees `ErrDeadlock`.

**What replaced it: a verify read.**
- All eight helpers are back to their pre-27.9a bodies: `git diff HEAD` on `update.go.tmpl` and
  `delete.go.tmpl` is empty, and so is the unit golden `update_products_mysql_gen.go`.
- The adoption's `UpdateWhere` now selects nothing.
- After each chunk's `UPDATE`, the executor runs `GetMany(id IN adopting AND fk = parent [AND disc])`
  with `Limit: new(0)`, PK-only `FieldOptions`, `SkipHooks`, `LockNone` and `nestedChildOptions`,
  the terminal re-read's options. The shortfall check counts that.
- Inside the transaction, a plain read sees the transaction's own writes. So it counts the rows the
  `UPDATE` adopted and never a row another connection parented first, on every dialect, with no lock.
- The sentinels are unchanged: `ErrAlreadyRelated`, or `ErrNotFound` on the `allow_reparent` branch.
- Statement counts are identical. The verify read takes the place of the read `UpdateWhere` used to
  issue to hand its rows back. A MySQL `connect` chunk is still 3 SELECT + 1 UPDATE, and a
  PostgreSQL one is still 3 statements: the visibility read, `UPDATE … RETURNING`, and the verify
  read. `nested_query_count_test.go` passes with its numbers unchanged.
- Tenancy holds on the tenanted self-referential `graphql` edges (`workspace_notes.Children` /
  `DraftChildren`). `SkipHooks` does not reach it, because tenancy runs inside the query closure
  from `SkipTenancy` / `Tenant` / the ctx-cached tenant. The resolve-once pins still pass.

**Two generator changes the verify read needed.**
- The `pid` local was declared only for the unlink verbs. A `verbs` mask of `[create, connect]`
  would have referenced an undeclared local, and `renderNested` parses without type-checking, so
  nothing else would have caught it. The gate now includes `connect` on O2M / has-one edges.
- `resolveNestedAdoption` now requires `ParentFilterExpr`, and drops `connect` with a report when it
  is empty, including under `allow_reparent`, which removes the guard but not the check. This
  mirrors the unlink half's identical check. It is defensive: an FK column always carries an `ID`,
  `Number` or `Opaque` filter member, so no schema reaches it, and it is unpinned.

**Failing-first.**
- The unit test `TestNestedTemplate_AdoptionCountsThroughAVerifyRead` failed in all five of its
  cases against the pre-rework template. Its `pid` assertion also failed alone, on the
  connect-without-unlink case, with only the old `pid` gate restored.
- `TestNestedTemplate_UnlinkScopesToTheEdgeDiscriminator` now counts 4 discriminator terms rather
  than 3, and failed at 3.
- At runtime, the old counting spelling was put back into the generated
  `userClient.applyUserUserEventsNested`, with no lock. The `a lost race is ErrAlreadyRelated`
  subtest returned `err = <nil>`. Restored, it passes.

**Measured after the rework**, with throwaway probes that were then deleted:
- A transaction held open after a `clear` on an empty edge and a `disconnect` of an unlinked id.
  Another connection's inserts, for that parent and for its neighbour, completed at once.
- The same probe with `LockMode: sql.LockForUpdate` put back on `userEventClient.collectAffectedIDs`:
  both inserts hit `Lock wait timeout exceeded`. So the probe discriminates.
- The lost-race interleaving on a READ COMMITTED pool returned `ErrAlreadyRelated`.

**One interleaving the verify read resolves either way, recorded in §9.9.6.** A row that another
connection parents to *this same* parent inside the window is counted when the read can see that
commit, as on PostgreSQL under READ COMMITTED. It is reported as the shortfall when the read cannot,
as on MySQL under REPEATABLE READ. The caller's end state holds either way, and the error is the
conservative side. The old `RETURNING` count reported it as `ErrAlreadyRelated` on every dialect.

**The residual is back, and wider.** MySQL `*Where` calls return to their pre-27.9a behaviour:
under a concurrent commit, `AffectedPKs` can over-report or under-report. The review pointed out
that this reaches nested calls too, not only flat ones. A nested `clear` or `disconnect` under
REPEATABLE READ still pairs a snapshot pre-SELECT with a current-read `UPDATE`. So a row parented
here after the snapshot is unlinked, but gets no cache eviction and no event. Only the adoption's
*shortfall check* is immune, because it no longer reads what `UpdateWhere` reports. §9.8.6b now states both
directions and both windows: since the transaction's first read under REPEATABLE READ, and between
the two statements under READ COMMITTED or outside a transaction. Under-reporting is the harmful
direction, because the row is written but gets no cache eviction and no event. The candidate fix is
to restrict the write to the pre-selected ids (`UPDATE … WHERE id IN (<ids>) AND <conds>`, chunked).
The review named its cost. A nested `clear` restricted that way leaves such a row linked, which
breaks §9.9.6's "`{clear: true, connect: [7, 9]}` *is* the set 7 and 9" under concurrency. It trades
a missed eviction for a wrong end state, so it is not free. **User ruling 2026-09-23: leave it as a
recorded residual.** No FIX is logged; §9.8.6b states the behaviour.

**The rework's footprint.**
- Templates: `update.go.tmpl` and `delete.go.tmpl` are restored to HEAD. In `nested.go.tmpl`, the
  adoption and the `pid` gate changed, and so did two comments.
- `context.go` and `context_nested.go` changed: the `ParentFilterExpr` requirement and three doc
  comments.
- Tests: `nested_test.go` gained one new test and one changed count. Comments moved in the two
  `mysql` test files and in one `graphql` test.
- Docs: in the PRD, §9.8.6b, the two collect listings, the `Where`-variants sentence, §9.9.6 (the
  execution-order block, `clear`, the shortfall paragraph and the hooks-skipped paragraph), §9.9.7
  and §25.1. The design doc's D12 annotation. All 669 PRD `](#…)` anchors resolve.
- Goldens:
  - `graphql` moved in its three connect edges only: `users.Events`, `workspace_notes.Children` and
    `DraftChildren`, plus the `clear` comment.
  - `mysql` moved in `users.UserEvents`, and the lock left it.
  - `tenancy_mysql` lost the lock, so it now differs from HEAD only by the retained wiring comment.
  - The unit golden `update_products_mysql_gen.go` equals HEAD.
  - No tree carries `LockMode: sql.LockForUpdate` in a helper, and `models/` equals `expected/` in
    all twelve trees.
- Gates, re-run after the rework: preflight clean (gofumpt 0.12.0, golangci-lint 2.13.2, go 1.27.1,
  matching `.tool-versions`). `make check` exit 0, 8 modules, 0 lint issues, `cmd/sqlgen/gen`
  52.4s. `make check-examples` exit 0, 12 trees under `-race` in 184s, no `DATA RACE`, longest leg
  `graphql/tests` at 40.6s.
- The auto-review on the rework found nothing blocking, and all four of its findings were fixed in
  place:
  - "Exactly" statement counts for `clear` were wrong on MySQL, whose `*Where` methods skip the
    write when the pre-SELECT finds nothing. §9.9.6's block, §9.9.7, §25.1 and D12 now say "at
    most", and the skip-refetch paragraph no longer counts statements.
  - The `allow_reparent` shortfall wording claimed the `UPDATE` "could not reach" the row.
    `UpdateWhere` applies no soft-delete default while `GetMany` does, so a row soft-deleted in the
    window is written and then not counted. The template comment and §9.9.6 now say only that the
    row stopped being visible, and §9.9.6 names the same-parent interleaving as the one exception.
  - The verify-read test now also pins `nestedChildOptions(o, options)`, which carries
    Tenant / SkipTenancy. It failed in all five cases with the line removed.
  - The Files list, defect 2's tense, and the residual's framing: the residual reaches nested
    `clear` / `disconnect` too, and the candidate fix has a cost.

**3. The M2M link step silently reset every payload column on the junction.** The link step
upserts on the junction's key, and `UpsertMany` puts every other insert column in the update half.
So re-connecting an already-linked pair set `user_categories.slot` from 7 to NULL and returned nil.
This is dialect-independent. It surfaced here only because `user_categories` is the first junction
in any example with a column beyond its two FKs (FIX-218 added it). **User ruling 2026-09-22:
tighten E5 now, the fail-closed direction, following E2's precedent.** A junction may carry no
column beyond its two FKs, optional ones included. The exemptions are the key columns and the
tenant column, which the update half never touches, and the soft-delete column, which the rule has
to allow because resetting it *restores* a link. That keeps E11's statement that such a junction
keeps `create` and `connect` true, and the E11 fixture (`user_tags`) is what found the exemption.
`users.Categories` is therefore ineligible on `mysql` and left out of the allowlist on purpose.
`assets.Documents`, over the pure `asset_document_links`, carries the M2M cases.
`TestNestedEligibility_E5JunctionPayloadColumnIsIneligible` covers both the auto-included and the
listed half, and failed first with the new clause removed. No `graphql` junction has a payload
column, so its surface is unchanged.

**4. The PRD's precondition for `firstID + i` was wrong.** §9.8.5's listing said the derivation
was "guaranteed by InnoDB with `innodb_autoinc_lock_mode < 2`". The container runs mode 2 (MySQL
8's default), and the derivation holds there, because a multi-row `VALUES` insert is a "simple
insert" and gets one contiguous block in every lock mode. What breaks it is
`auto_increment_increment > 1`. At 2, a nested M2M create of three rows returned nil, linked two
and dropped the third, because the derived id 4 did not exist and `CreateMany`'s re-read skipped
it. **User ruling: fix the PRD text here and track the derivation as a FIX.** The listing now names
the real precondition, and **FIX-222** (`tracked`) carries the fix. The pin runs under the default
configuration: `TestNestedMutations_M2MCreateLinksTheRowsItInserted` asserts linked targets *by
name* at 3 and at 401 targets (three `CreateMany` statements, each with its own `LAST_INSERT_ID`),
with a decoy inserted immediately before. It failed first in both cases against
`firstID + int64(i) - 1`, which linked the decoy, dropped the last target and returned nil, the way
an off-by-one would in production.

**Query count, restated rather than assumed (§9.9.7, §25.1).** The counts come from MySQL's
per-session status counters on a single-connection pool, which sit below every Go layer. A
`Querier` shim cannot see inside a transaction, which is the vacuous pin 27.9 found on pgx.
Measured per chunk over a parent-only baseline:
- O2M `clear`: 1 SELECT (the pre-SELECT) + 1 UPDATE.
- M2M `clear`: 1 SELECT (the pre-SELECT) + 1 DELETE.
- `disconnect` of 401 ids: 3 SELECT + 3 UPDATE.
- `connect` of 401 orphans: 9 SELECT + 3 UPDATE (per chunk: the visibility read, the pre-SELECT
  and the read-back — the verify read since the 2026-09-23 rework, at the same count).
- Totals are identical at 2 and at 20 children.

§9.9.7 now says that a `*Where`-routed verb costs one more statement per chunk on MySQL, and that
`clear` is two there, one read and one write. It also lists the pre-SELECT and the adoption's
read-back, which the read table had omitted on every dialect. Since the rework that row is the
verify read, and the pre-SELECT row no longer says the adoption counts it. §25.1's box counts *write*
statements, and says so. §9.8.6b's MySQL table also claimed a skip-fetch `*Where` was 1 query. It
is 2, because the keys are always collected for cache invalidation and events. The code listing
there now matches the generated body.

**Acceptance.** The lifecycle runs inside one caller `WithTx`, with state asserted after each step:
`CreateWithRelated`, then `UpdateWithRelated`, then `UpsertWithRelated` (the conflict branch on
`users`; the insert branch on `assets`, whose only conflict target is a generated key), then the
many-delete. None of these tables has a soft-delete column, so the §9.2 many-delete is
`HardDeleteMany`. Every nested call there runs as a savepoint, which pins the bare-identifier
transaction names on MySQL. `TestNestedMutations_SavepointComposition` uses the explicit
`Begin` / `Commit` spelling and fails the last nested step on purpose. The failing upsert's parent
write, and the `clear` it ran before its failing `connect`, both roll back to its savepoint. Steps
1–2 survive, and the caller's transaction writes and commits afterwards. The O2M path covers all
six outcomes: adopt, already-ours no-op, `ErrAlreadyRelated` attributed `UserEvents/connect/<id>`,
`ErrNotFound`, and `disconnect` / `clear` reading the FK back as NULL (F11), with a bystander on
another parent untouched. After the fixes above, §9.9.6 has no claim that holds on PostgreSQL and
fails on MySQL, with one caveat FIX-222 tracks: the link step's keys are right only while
`auto_increment_increment` is 1. §9.9.6 says what the rest rests on, in a new paragraph. That
paragraph was about the locking read, and since the 2026-09-23 rework it is about the verify read,
which holds on every dialect.

**Fixture notes.** `testConnStr` is now exported from `tests/main_test.go`, as in `graphql`, so a
test can open its own pool. The seed helpers register their own cleanup, and a temporary check
counted zero leftover rows in all four touched tables after the nested tests ran.

**Checks.**
- Preflight clean: gofumpt 0.12.0, golangci-lint 2.13.2 and go 1.27.1, all matching
  `.tool-versions`.
- `make check`: exit 0 across 8 modules, 0 lint issues. One unit golden moved
  (`update_products_mysql_gen.go`, +9: the lock and its comment; back to HEAD since the rework).
- `make check-examples`: exit 0 across 12 trees under `-race`, no `DATA RACE`.
- No security-review trigger: nothing under `manifest/` or `cmd/sqlgen/cli/`.

**Files** (the net diff against HEAD after the 2026-09-23 rework).
- New: `…/examples/mysql/tests/{nested_mutations_test,nested_query_count_test}.go`.
- Modified:
  - `gen/{context,context_client,context_nested,nested_names}.go`
  - `gen/templates/client.go.tmpl`, `gen/templates/table/{client,nested}.go.tmpl`. The first
    landing's `update` / `delete` template edits were reverted by the rework, so both equal HEAD.
  - `gen/{nested_test,nested_names_test}.go`. The unit golden `update_products_mysql_gen.go` equals
    HEAD again.
  - `…/examples/mysql/{schema.sql,sqlgen.yml,tests/main_test.go}`, and
    `…/examples/graphql/tests/nested_mutations_test.go` (one comment).
  - `docs/PRD.md`: §9.8.5, §9.8.6b, §9.8.13e (the collect listings), §9.9.4 E5, §9.9.6, §9.9.7,
    §25.1 and §26.12.
  - `docs/design/archive/NESTED_MUTATIONS.md` (E5 and D12 annotated rather than rewritten)
  - `docs/tracker/fixes.md` (FIX-222)
- Regenerated goldens: all 12 example trees and two unit goldens (`update_products_mysql_gen.go`,
  `unified_client_gen.go`). What moved:
  - `mysql`, `tenancy_mysql`: the lock and its comment (both removed by the 2026-09-23 rework).
  - `cache`, `postgres`, `postgres_stdlib`, `sqlite`, `tenancy`: the one-field has-one wiring.
  - Every tree: the unified client's wiring comment, which the review found still said
    "O2M/M2M relationship loading".
  - `graphql` and `mysql`: the nested `clear` comments.
  - No `graphql` code line moved.

**The auto-review found no blocking issue. It raised two fix-in-place items and five nits, and all
were fixed inside the sub-item.**
- §9.9.6 still called `clear` "a single statement on either shape". It now says a single *write*,
  which MySQL precedes with the pre-SELECT (the "locking read" of the first landing).
- §9.8.6b, and the collect helpers' comment, said the locking read makes the write affect "exactly"
  its rows. That overclaims in two cases that do not reach the adoption, and both were stated. The
  2026-09-23 rework removed the lock, the comment and both caveats with it:
  - A relationship filter's `EXISTS` subquery reaches tables the lock does not cover.
  - READ COMMITTED takes no gap locks, so a row inserted between the read and the write that
    matches a range condition is written but not reported.
- `TestNestedMutations_SavepointComposition` registered its data cleanup after its `Rollback`
  cleanup, and cleanups run in reverse order. A failure between `Begin` and `Commit` would therefore
  have deleted on another connection against the transaction's row locks and waited out
  `innodb_lock_wait_timeout`. The data cleanup now rolls back first.
- The nits:
  - §9.9.7 now says "O2M / has-one", since has-one routes through the same `UpdateWhere`.
  - Four stale comments were corrected: the nested `clear` block's two, `resolveNestedJunction`'s
    doc, and the unified client's wiring comment.
- On the reviewer's coverage gap, E5's tenant exemption is now pinned by a tenanted-junction
  subtest, which failed first with the clause removed. The primary-key exemption stays unpinned. It
  matters only for a surrogate-keyed junction with an app-generated key, which no fixture has.
- Both gates were re-run after these fixes: `make check` and `make check-examples` exit 0.

---

## 27.10 GraphQL projection (Ticket H)

**PRD Reference:** §26.5.1, §26.5.5, §26.12

**Status:** **Complete** (landed 2026-09-23; verified 2026-09-24. `/verify` returned no FAIL and
filed FIX-227, FIX-228 and FIX-229, all `tracked`. See the verify pass at the end of the record.)

### Tasks

- [x] `create<Table>WithRelated` / `update<Table>WithRelated` / `upsert<Table>WithRelated` in the curated mutation surface
- [x] Nested input types projected from the Go families; `<Table>ConflictTarget` argument on the **upsert** mutation only (**Q4**)
- [x] Error mapping: `ErrAlreadyRelated` → `CONFLICT`, `ErrNestedVerbConflict` → `INVALID_INPUT`, visibility miss → `NOT_FOUND` (never `BAD_REFERENCE`)
- [x] `extensions.path: ["<Edge>"]` from `NestedMutationError.Edge` (**NW-Q4**) — **PascalCase**, corrected 2026-09-18: `Edge` is the relationship's *Go field name* per PRD §22.2, and 27.7 reconciled §9.9.8 and §26.5.5 onto that one spelling. A lowercased wire value would disagree with the struct the mapper reads it from
- [x] **E7** — no nested member into an `api.enabled: false` entity
- [x] **Added 2026-09-23 by user ruling:** a target's `api.operations` mask narrows the verbs that write the target (§9.9.4 E7 amended)
- [x] **Added 2026-09-23 by user ruling:** the flat update input's `_inc` / `_dec` operators run in the same transaction as the nested writes (§26.5.1)

### Acceptance Criteria

- Verified through the **real gqlgen server** in `graphql/tests`, as 25.9 did — not against the schema alone.
- `createTable` / `updateTable` / `upsertTable` are byte-identical to before.
- The walker-completeness lint passes: no advertised nested member lacks a translator.

### Tests Required

- [x] E2E through the booted gqlgen server: one mutation per family, per shape
- [x] `extensions.code` pin per sentinel; `extensions.path` names the failing edge
- [x] Negative: an `api.enabled: false` target has no nested member in the schema — **at the unit level**, see the completion record
- [x] Golden: existing mutations unchanged

### Completion Record

**Landed 2026-09-23.** New `cmd/sqlgen/gen/context_api_nested.go` projects each table's resolved
`NestedContext` onto the API: `APITableContext.Nested`, attached by a post-build pass because an
edge is gated on its *target's* API context. The four API templates render it. The schema gets
the wrappers, the verb blocks, the child inputs, the `<T>ConflictTarget` enum and a separate
`extend type Mutation` block. `input_translate` gets the translators. `resolvers` gets the `M`
methods, and `seeds` the `*mutationResolver` delegations. The error mapper attaches the path. Both
examples that enable nested mutations project them: `graphql` (seven parents) and `mysql`
(`users`, `assets`, `categories`, `documents`, `orders`, `products`, including the singular
has-one `profile` block).

**The projection decides nothing the Go surface has not decided; it can only narrow it.** With
every target on the API and nothing masked, the schema advertises exactly the Go surface: the
same families, the same edges in the same order, the same verbs.
`TestAPINested_UnmaskedProjectionIsTheGoSurface` compares the two contexts built from one fixture
instead of restating the matrix. Four things narrow it:

- **E7**: the target is `api.enabled: false`.
- **E6 on the projected input**: the target has no API-writable column left.
- **The target's `api.operations` mask** (ruling below).
- **An empty child input.** When the target's projected create input, minus the FK and the
  discriminator, has no member left, the `create` verb cannot be declared, because a GraphQL input
  type needs at least one field. This is the one narrowing that is not a config fact.
  `users.Tagged` in the unit fixture is that shape. §26.5.1 states it.

**Two rulings, both the user's, 2026-09-23.** The PRD did not settle either.

1. **A target's `api.operations` mask narrows the verbs that write the target.** E7 as written
   named only `api.enabled: false` and the access projection. A consumer who masks a target's
   `create` to force a hand-written, guarded resolver would otherwise get a second, generated way
   in through every parent that nests into it. `create` needs the target's API `create`, plus
   `create_many` wherever E4 needs it (a has-one edge inserts through `Create`). An O2M / has-one
   `connect` / `disconnect` / `clear` needs the target's API `update_many`, which is the API
   projection of the `update_where` E8 routes them through. The M2M link and unlink verbs write
   only the junction row, so they are gated by E7 alone. §9.9.4's E7 row, §26.5.1 and §32.5 are
   amended.
2. **The flat update input's `_inc` / `_dec` operators run in the nested mutation's
   transaction.** `Update<T>WithRelatedInput` contains `Update<T>Input`, which carries the
   operators on GraphQL (§9.9.5 says they are inherited). `UpdateWithRelated` takes none, so
   translating the wrapper and ignoring them would have been accept-and-drop. When any operator is
   set, the `M` method runs three steps inside one `Client.WithTx`: `UpdateWithRelated` as a
   savepoint, then the `Increment` calls, then the read-back. With none set it is the Go method
   alone. The transaction name is `update_<snake>_with_related_inc`, deliberately distinct from
   the Go method's own name, because on MySQL a savepoint that reuses a live name replaces it.
   This deliberately differs from `update<T>`, whose sequence §26.5.4 does not wrap. §26.5.1
   says so.

**Three rules applied on the parent side without a ruling. Each is an existing rule carried to
the masked set, and none is a new decision.**

- `maskAPIOperations` now masks the three `…_with_related` toggles. On the masked set each one
  implies its base, exactly as `ResolvedOperations` states for the resolved set (§4.6), so masking
  `create` off the API removes `create<T>WithRelated` too.
- A family whose flat input FIX-071 suppresses is not emitted, because the wrapper would reference
  an undeclared type.
- A family whose wrapper no surviving edge reaches is dropped. This is D19 applied to the API.

**The input shapes project the Go types.**

- The wrapper holds the flat input under the table's name (`user`).
- The **update** wrapper's flat member is **nullable**, so a nested-only update need not send
  `user: {}`.
- A has-one edge's verbs take one value, not a list, matching the Go block's pointer members. The
  first E2E generation caught this: `invalid append: argument must be a slice` on
  `AssetPrimaryDocumentCreateNested.Create`.
- `upsert<T>WithRelated` takes `conflictTarget: <T>ConflictTarget! = PK`. The values are `PK` and
  the SCREAMING_SNAKE form of each unique target's columns. The argument is required with no
  default on a table without a PK target, and the lint rejects two targets that project onto one
  value.
- The conflict-target and increment translators return sqlgenresolver's `INVALID_INPUT`, and they
  live in `input_translate_gen.go` rather than in the seed. A seed is written once and then owned
  by gqlgen, so logic placed there could never be corrected later.

**`extensions.path` is attached in `mapErrorToGQL` itself, after the code is chosen.** Every code
therefore carries it: a duplicate child is still `CONFLICT`, and the caller needs the edge. A
failure outside every nested block, such as the parent's own write, carries none. The mapper was
split into `mapErrorToGQL` → `gqlErrorForCode` to do it. The arm order, and
`TestMapErrorToGQL_NestedVisibilityMissStaysNotFound`'s pin on it, are unchanged. The four other
GraphQL examples' `errors_gen.go` move with it.

**The completeness lint the acceptance criterion names is `ValidateAPINestedCompleteness`.** It
runs on the built context, so it runs under `sqlgen validate` too. It checks four things:

- Every advertised member renders both halves.
- Every advertised verb is one the Go edge carries.
- No two members of one input share a name.
- Every emitted wrapper has a member, and every conflict value is unique and translatable.

The schema and the translators are rendered from one slice, so what is checkable is renderability
and agreement with the executor, not drift between two lists.
`TestValidateAPINestedCompleteness_Rejects` covers five perturbations of the real projection, and
the §26.5.2 walker lint is unaffected. The nested types are claimed in the GraphQL name registry
**as emitted**, from the same gates the template reads. The Go registry already claims the same
spellings structurally, so the conditioning boundary is held there. What only the GraphQL registry
can see is a GraphQL-only claimant. §26.4's owned-set table gains the row.

**Byte-identity of the flat surface is pinned twice.**
`TestAPINestedSchema_FlatSurfaceIsByteIdentical` asserts the nested-off schema is a byte prefix of
the nested-on one. Every example `.graphqls` diff is purely additive. The create-input field
assignment moved into a shared `api/create-input-assign` fragment, which the nested child
translators reuse. Every flat translator in all 12 trees is unchanged by it, and all unit goldens
are unchanged.

**Failing-first, each against the state that produces the defect:**

- The increment pin: running the tx body without `WithTx` in the generated resolver leaves the
  `rolled-back` child in the table after the overflow.
- The path pin: dropping the attachment from the generated mapper fails all four sentinel cases,
  each with `extensions.path = []`.
- The target-mask pin: making `relinkAllowed` unconditional fails two cases.
- The parent conjunction pin: dropping `&& ops.Create` fails `create off`.

**E7's negative is unit-level, and the reason is a pre-existing read-side gap. It is flagged for
a FIX entry at the user's discretion rather than logged here.** A relationship whose target is `api.enabled: false` still renders its field on the
parent's object type (`events: [Event!]!` with no `type Event`), so gqlgen rejects the schema
before a nested member could be looked for. This was measured with a throwaway probe on the unit
fixture. The read side's `mapRelationshipToGraphQL` is unfiltered, and the object-type field and
the walker are outside this sub-item. `TestAPINested_E7HiddenTargetGetsNoMember` pins the write
side: no member in any wrapper, and the Go surface keeps the edge.

*Lifted by FIX-223 (2026-09-24).* The object-type field and the walker case now drop by the same
exposure test. `TestAPINested_E7HiddenTargetGetsNoMember` asserts the whole document. The `graphql`
example gained an E2E hidden target, `user_sessions` (`api.enabled: false`), which
`TestAPIHiddenTarget_SchemaNamesItNowhere` checks by introspection.

**The auto-review found two defects and one PRD nit. All three were fixed inside the
sub-item.**

1. **The increment transaction's read-back could be served by the cache.** The cache
   `QueryHook` serves a partial-`FieldOptions` `Get` read-through, with no transaction check,
   while the invalidation from the writes before it waits until commit. So `stock_inc: 5` on a
   cached product returned the cached `stock = 10`. The read-back is the Go method's terminal
   re-read, moved into the resolver because the increments must precede it, so it now runs as
   that read does: with `SkipHooks`, applied after the HTTP options (PRD §9.9.6). The E2E test
   above missed this because it selected `children`, and any relationship selection bypasses the
   cache. `TestGraphQLNested_IncrementReadBackBypassesTheCache` uses a scalar-only selection
   against a cache-wired server, and it **failed first** (`stock = 10`, want 15).
   `TestAPINestedTemplates_IncrementBranch` pins the skip at the unit level. It is also the first
   unit fixture that renders the increment branch at all: `users` has no incrementable column,
   so before it that branch was covered by the E2E goldens alone. This is 27.7's lesson for the
   fourth time.
2. **The relink gate read the target's intersected `UpdateMany`, which folds in the Go client's
   own `update_many`.** That is a batch-of-items method the executor never calls; E8 reads
   `update_where`. So `operations: {update_many: false}` with no API mask kept `connect` on the
   Go edge and dropped it from the schema, breaking the invariant that the unmasked projection
   is the Go surface. The gate now reads the target's API mask alone (`apiNestedTargets`). `create`
   has no such gap, because the Go edge already requires the client's `create` / `create_many`
   (E4). `TestAPINested_ClientUpdateManyDoesNotReachTheRelinkVerbs` **failed first** with the old
   gate. E7's wording in the PRD, which had called `update_many` "the API projection of
   `update_where`", now says what the entry gates instead.
3. **E6 is stated as two halves** in §9.9.4 and §32.5. The Go half reads the unprojected input,
   so it removes an edge only when the target has no create-input column at all. The GraphQL half
   reads the projected input and removes only the member.

**Recorded, not fixed.** Two items outside this sub-item's scope. The first is pre-existing; the
second is a coverage gap in this sub-item's own code (corrected 2026-09-24 by FIX-225, which had
found this record calling both pre-existing).

- `resolvers.go.tmpl` gates `M.Update<T>s` on `UpdateMany` but calls `UpdateWhere`, so
  `update_where: false` beside `update_many: true` would not compile. PRD §26.5.1's table also
  names `UpdateMany` as the backing method.
- The has-one `Singular` id-verb branch with a cast is rendered by no fixture. It needs a has-one
  edge over a nullable FK into an `Int`-keyed target, and is only parsed, never compiled. The
  branch was added by this sub-item, so it is not pre-existing. FIX-225 adds the fixture.

Both are flagged for the user alongside the read-side E7 gap.

**Gates.**

- Preflight clean: gofumpt 0.12.0, golangci-lint 2.13.2 and go 1.27.1, all matching
  `.tool-versions`.
- `make check`: exit 0 across 8 modules, 0 lint issues, re-run after the review fixes. Seven
  cyclop hits from the first landing were split along their seams rather than suppressed.
- `make update-golden`: no unit golden moved.
- `make update-golden-e2e`: re-run after the last template change, and idempotent.
- `make check-examples`: exit 0, 12 trees at 0 lint issues each, all tests green under `-race`,
  no `DATA RACE`. Run twice, the second time after the review fixes. The longest leg is
  `graphql/tests` at 42.2s.
- No security-review trigger fired: nothing under `manifest/` or `cmd/sqlgen/cli/`.

**Files.**

- New: `gen/context_api_nested.go`, `gen/api_nested_test.go`,
  `…/examples/graphql/tests/nested_mutations_graphql_test.go`.
- Modified: `gen/{context_api,api_field_overrides,api_names,funcmap,orchestrate}.go`,
  `gen/templates/api/{schema.graphqls,input_translate.go,resolvers.go,seeds.go,errors.go}.tmpl`,
  `gen/api_errors_test.go`.
- Docs: `docs/PRD.md` (§9.9.4 E6 + E7, §26.4 owned set, §26.5.1, §26.5.5, §26.5.6 coverage,
  §32.5), `docs/design/archive/NESTED_MUTATIONS.md` (§6 annotated), `guidelines/TEMPLATES.md` §13.
- Goldens: `graphql` and `mysql` (`graph/` in both `models/` and `expected/`), plus
  `errors_gen.go` in `graphql_null_wrappers`, `graphql_top_level` and `sqlite`.

**Verify pass (2026-09-24).** The PRD requirements pass. `make check` and `make check-examples`
are green: 12 trees, 0 lint issues, no `DATA RACE`. Three gaps are filed, all `tracked`:

- FIX-227: three E2E family × shape cells. *Resolved 2026-09-25 by
  `TestGraphQLNested_UpdateAndUpsertReachTheRemainingShapes`. FIX-225's relink test covers
  update × has-one for `connect` / `disconnect`.*
- FIX-228: the conflict-target enum names access-restricted UNIQUE columns. *Resolved 2026-09-29:
  the enum declares a unique target only when every column is API-writable (the PK target always
  stays), and a table left with none omits its nested upsert, pinned by
  `TestAPINested_ConflictTargetsProjectAccess`.*
- FIX-229: D20 does not cover the API mask that 27.10 made conjoin the base. *Resolved 2026-09-29:
  D20 now runs over `api.operations` and `tables.<t>.api.operations`, pinned by
  `TestValidateNestedMutations_APIMaskWithRelatedNeedsBase`.*

Two small items were fixed inline. §26.5.1 now states that on MySQL `conflictTarget` does not choose
the constraint that fires, and shapes only the update half. The `edgeNamed` test helper takes `t`
and calls `t.Fatalf` instead of panicking (TESTING.md §7).

---

## 27.11 Closure sweep

**PRD Reference:** all of the above

**Status:** **Complete** (closed 2026-10-02)

### Tasks

- [x] `make check` under `-race` across all 8 modules
- [x] `make check-examples` across all 12 example modules
- [x] `make test-integration` under `-race`
- [x] `/fix` triage — every entry originating in Phase 27 resolved or explicitly deferred with severity
- [x] PRD §9.9 reconciled line-by-line against the landed generator
- [x] Mark `docs/design/archive/NESTED_MUTATIONS.md` **SYNCED** and record what it is retained for
- [x] `/close-phase 27`

### Acceptance Criteria

- All three sweeps clean, no flakes.
- Zero `blocking` FIXes open against the phase.
- The design doc's status header names the superseding PRD sections.

### Tests Required

- [x] The three sweeps above

### Completion Record

**Closed 2026-10-02.** Unlike 28.7 this closure was not bookkeeping only: the §9.9 reconciliation
found one listed-edge defect and three text defects, which the user ruled be fixed here.

**Sub-items:** 27.0–27.10 (incl. 27.0a, 27.9a) all **Complete**, every task box ticked, every
Completion Record filled.

**FIX triage.** All ten entries originating in Phase 27 are Resolved (FIX-218, 219, 220, 225, 227,
228, 229, 234, 240, 245), as are the six other-origin entries the phase surfaced (FIX-212, 213, 217,
221, 222, 223). Carried forward with the user's OK, each `tracked` and originating before 27:

- **FIX-224** (phase 16): folded into Phase 29.1, which cannot start until this phase closes.
- **FIX-254 to FIX-257** (phases 5, 5, 4, 1): promoted from the backlog at this closure, unrelated to
  nested mutations, queued for the next `/fix-loop`. FIX-257 needs a panic-vs-signature ruling first.

**Backlog review.** 33 items reviewed. 4 promoted (above), 29 kept, 0 deleted. Two `[reading]` lines
added from the reconciliation and its review (a surrogate-key junction under the link step; a
silent M2M `disconnect` drop on a junction key-type mismatch). Queue freeze stays on;
the open queue is not empty.

**PRD §9.9 reconciliation** (two read-only passes, ~176 normative statements, ~158 matched):

- *Code fixed (user ruling 2026-10-02):*
  - An edge **listed** in `nested_mutations.relationships` that fails **E1** (belongs-to O2O) or
    **E3** (composite-PK parent) was filtered out by `nestedCandidateEdges` before the lint saw it,
    so it generated nothing and reported nothing. `listedShapeFailures` (`gen/context_nested.go`)
    now reports it on both `validate` and `generate`. Test
    `TestNestedEligibility_ListedShapeFailureIsReported`, verified failing-first; no golden moved.
  - Nested error wraps used the camelCase struct name (`update workspaceNote with related`) where
    §22.3 and the flat methods use the snake singular (`update workspace_note`). Now `$singular`.
  - The `UpsertWithRelated` doc comments (`nested.go.tmpl`, `client.go.tmpl`) said every verb is
    idempotent; §9.9.5 says `create` is not.
  - Lint rule IDs: target-PK map-key failures cite **E8** on every shape (were **E5** on M2M), and an
    unsettable traversed FK cites **E1** (was **E4**).
- *PRD amended to match the code:* E1 (generated, single-keyed target; belongs-to fails it), E8's
  M2M half, E9 (`clear` needs `hard_delete` too), E12 and the verb-conflict check (refused before
  any statement *on that edge*), the M2M `disconnect` cell (junction PK is exactly its two FKs), edge
  order (byte order of the Go field name, four sites), has-one `create` (`Create`), the `connect`/`disconnect` merge reason, E5 (the junction is generated and its two FKs are
  constructible create-input fields, which the lint already enforced under E5), a soft-deleted child under `disconnect` (unlinked; user
  ruling), and D19 (per parent in Go, per family on the API; user ruling). The `ErrNestedVerbConflict`
  comment was corrected in `database/errors.go`, `error.go.tmpl`, PRD §22.1 and `guidelines/ERRORS.md`.

**Review.** One `sqlgen-reviewer` pass over the closure diff. It found no behavior defects. All 11
amendments matched the code. It confirmed fail-first independently and probed for false positives:
a self-referential O2O and cross-schema has-one and belongs-to edges all resolve correctly. Its six
doc findings were applied inline: the stale "alphabetical" comments in `nested.go.tmpl`,
`context.go` and `context_api_nested.go`; the auto-included half of the new test, now asserted
rather than claimed; E12's "or savepoint"; and two tracker refs. It raised two questions:
- **E5 labels:** auto-accepted on 2026-10-02 (`/fix` auto-accept rule). E5's PRD text was extended
  to the junction-key checks the lint already files under it. That is a doc correction to match the
  code: no golden churn, no public surface, no ruling reversed.
- **The junction key-type silent drop:** sent to the backlog.

Goldens: `graphql` and `mysql` `models_gen.go` (error text and comments), every example's
`errors_gen.go` and the unit `errors_gen.go` (comment only).

**Sweep** (sequential, run three times: before the reconciliation, after its fixes, and after the review's; tool preflight
matched `.tool-versions` — go 1.27.1, golangci-lint 2.13.2, gofumpt 0.12.0). Final run:

| Leg | Exit | Wall | Longest legs |
|---|---|---|---|
| `make check` | 0 | 1m51s | `cmd/sqlgen/gen` ~59.4s, `cmd/sqlgen/cli` ~56.0s — 0 lint issues across all eight modules |
| `make check-examples` | 0 | 3m43s | `examples/graphql/tests` ~61.3s, `examples/mysql/tests` ~16.5s — all twelve trees, 0 lint issues |
| `make test-integration` | 0 | 2m57s | `cmd/sqlgen/gen` ~135.2s, `cmd/sqlgen` ~128.7s, `cmd/sqlgen/cli` ~126.6s |

Earlier runs: 1m30s / 2m06s / 1m53s and 1m55s / 5m42s / 2m14s, same longest legs. No `FAIL`,
`DATA RACE` or `panic:` line in any run; no flakes, no reruns. Wall times vary with the lint cache
after each regeneration.

**Sibling design doc:** `docs/design/archive/NESTED_MUTATIONS.md` → **SYNCED — superseded by PRD §9.9
(2026-10-02)**, retained for the motivation and probe transcripts, the decision record, the verb
algebra's derivation, the rejected alternatives and the no-lock argument, with a superseded-claims
list. The companion `docs/design/archive/NESTED_MUTATIONS_EXAMPLES.md` is retained as-is. **Frozen artifacts:**
none. Phase 27 specifies no versioned contract.

**Release-notes note, flagged and not fixed.** The phase is additive and opt-in, with one exception:
27.3 (`402ee362`) makes single-row `Update` with a non-nil, all-false `FieldOptions` return
`nil, nil` where it returned `ErrNotFound`, and the commit carries no `!`. This is the same situation
as Phases 25 and 28: there is no configured remote and Release Please has consumed nothing, so
amending it is a history rewrite and the user's call.
