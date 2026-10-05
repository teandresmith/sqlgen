# Phase 29: Generate Every Client Method the Schema Allows

Status: Complete (closed 2026-10-03)
PRD Sections: 4.6, 4.13, 9, 9.8, 9.9.4, 20.2, 23.7, 26.5.1, 26.8, 26.10, 30.4.3

> **Decision (2026-09-24).** The per-table `operations` toggles come off the Go client. The client
> emits every method the schema allows, and `api.operations` becomes the only per-operation
> control. The evidence is in `docs/tracker/IMPLEMENTATION_ORDER.md` § Phase 29. It comes from a
> dependency map of the 21 toggles traced from the generator code, and from a survey of other
> generators: none of the DB-first client generators checked offers a per-table, per-operation
> opt-out.

## 29.0 PRD sync — the client follows the schema, the API mask follows the consumer

**PRD Reference:** Sections 4.6, 4.13, 9, 9.8, 9.9.4, 20.2, 23.7, 26.5.1, 26.8, 26.10, 30.4.3

**Status:** Complete

### Tasks

- [x] Record four decisions with the user before editing:
  - the principle that every surface's mask key names the client method it calls, and the two
    surfaces that break it today:
    - `update<T>s(filter, input)`, gated on `update_many` but backed by `UpdateWhere`. FIX-224
      recommends `update_where`, with `update_many` becoming a client-only name.
    - `<t>List`, gated on `get_many` or `paginate` but backed by `Paginate`, so the two keys act as
      one switch.
  - whether a leftover `operations` key fails with the strict decoder's plain unknown-field error
    (`config.go:1580`, `KnownFields(true)`) or with a message that points at `api.operations`;
  - whether the per-table over-reach warning survives, now that the client set is the
    schema-allowed set.
    FIX-249 sub-item (1) was deferred here by the user's ruling (2026-10-01): at HEAD the warning
    misses a `…_with_related` mask key whose base client toggle is off (`operations: {create:
    false}` + `api.operations: {create_with_related: true}` validates silently and the nested
    mutation vanishes), because `validateAPIOperations` reads the raw toggles while generation
    conjoins each `…_with_related` with its base. If the warning survives, it must apply that
    conjunction;
  - whether §30.4.3's `generation_config.pagination` is dropped (recommended; it would always be
    `true`) or kept pinned.
- [x] §4.6: remove `operations` from `GenerationConfig` (line 440), the `OperationsConfig` table
  (454), and the client examples (486–494). Replace "Always-enabled operations" (382) with the rule:
  every method the schema allows is generated. List the schema-fact gates: views, tables without a
  PK, the soft-delete column, incrementable columns, resolvable cursor keys, conflict targets for
  upsert.
- [x] §4.6: move the preset table (444–450) under `api.operations`, the only place presets remain.
  Drop the "Every preset resolves every toggle" paragraph (452) or restate it for the mask.
- [x] §4.6 `nested_mutations.operations` row (513): drop "Intersected with the per-table
  `operations` mask".
- [x] Remove every remaining client `operations` example and field row: 668, 929 (the `upsert_many`
  example in *Nested mutations per table*), 1052, 1108, 1114.
- [x] §4.13: delete the rule "A `…_with_related` operation enabled without its base operation"
  (1183). Scope "Invalid `operations` preset" (1185) to `api.operations`. Add a row for a leftover
  `operations` key if decision 2 calls for a message. Update the config-level list (1214), the
  JSON-schema row (1255) and the testing bullet (2146).
- [x] §9 intro (3133) and §9.8 Design Principle 1 (3962): restate generation as schema-driven.
- [x] §9.9.4: remove the toggle clauses from E4 (7365), E8 (7369) and E9 (7370), and fix the
  introduction that cites "E9's `hard_delete` clause" (7358). Retire any rule left with no clause,
  and renumber nothing. E7 (7368) keeps the mask clause, under the key decision 1 chooses.
- [x] §20.2 conditional generation rules (10008–10013): schema facts only.
- [x] §23.7: delete lint rule 3, "Unused table constant" (11297).
- [x] §26.4 / §26.5: fix the operations-driven delete-naming note (11752) and the intro (11887).
- [x] §26.5.1:
  - Make the gate column of the surface table (12186–12197) read "`api.operations` includes X",
    plus the schema fact where one applies. Back `update<Table>s` with `UpdateWhere`.
  - Restate the delete/restore naming table (12215–12225) over the mask.
  - Rewrite the mask section (12227–12260). Its opening premise, "`operations` decides what the
    **Go client** generates", no longer holds. "Subtractive only" now intersects with the
    schema-allowed set, and the over-reach warning and the "Only API operations may be named" list
    follow decisions 1 and 3.
  - Update the hidden-entity paragraph (12275).
- [x] §26.8 REST endpoints (12762) and §26.10 (12846–12861): reference `api.operations`. Remove the
  paragraph "The per-table `operations` config already drives client code generation", and the
  `read_write_no_delete` row, which names a preset that does not exist.
- [x] §30.4.3 `generation_config` (15215ff): apply decision 4.

### Acceptance Criteria

- No section of the PRD describes a per-table or global client `operations` toggle. A grep for
  `operations` outside `api.operations`, `views.<name>.api.operations` and
  `nested_mutations.operations` returns only prose that says toggles do not exist.
- Every schema-fact gate the generator applies is stated once in §4.6 and cited from §9.8 and
  §20.2.
- §26.5.1's surface table names, for every query and mutation, the mask key that gates it and the
  client method that backs it, and each row's key names that method (FIX-224, `<t>List`).
- E4, E8 and E9 cite no toggle. Every rule that remains names a schema fact or a `nested_mutations`
  setting.
- The four decisions are recorded in this file's Completion Record with the user's ruling.

### Tests Required

- [x] None. This sub-item changes documentation only. Its check is the grep in the first
  acceptance criterion, recorded in the Completion Record.

### Completion Record

**Completed 2026-10-03.** Documentation only. Files: `docs/PRD.md`, `docs/tracker/IMPLEMENTATION_ORDER.md`
(the golden claim), `docs/tracker/phase-29.md`, `docs/tracker/STATUS.md`. No code, no goldens.

**The four decisions (user ruling, 2026-10-03):**
1. **Every mask key names the client method its surface calls.** The `update<T>s` half was already
   ruled on FIX-224 (2026-10-01: Q1(b) `update_where` is the API key and `update_many` client-only;
   Q2(ii) a set mask zeroes `UpdateMany`). The `<t>List` half: the query moves to `paginate` alone,
   and **`get_many` is retired from the API keys**, so naming it in any `api.operations` block is a
   hard error, on the same rule as `update_many`. The API key set is now 14: `get`, `paginate`,
   `connection`, `create`, `create_many`, `update`, `update_where`, `upsert`, `soft_delete`,
   `hard_delete`, `restore` and the three `…_with_related`. The client-only list is `get_many`,
   `update_many`, `exists`, `count`, `increment`, `stream` and `upsert_many`.
2. **A leftover `operations` key gets a pointed message**, not the strict decoder's plain
   unknown-field error. §4.13 gives the text: `generation.operations: the Go client generates every
   method the schema allows; use api.operations to control what the API exposes` (and the same for
   `tables.<t>.operations`).
3. **The per-table over-reach warning survives, based on schema facts.** It compares a per-table
   mask's explicit `true`s against the table's resolved operations, which are now exactly the
   schema-allowed set: `soft_delete` / `restore` without a soft-delete column, `upsert` without any
   conflict target, and a `…_with_related` key whose family `nested_mutations` does not enable for
   the table, or for which the table has no eligible edge. A global mask stays silent. FIX-249
   sub-item (1) (base off on the *client* toggle) is closed by removing the toggles. The
   mask-internal base-off pair is already the §4.13 D20 error, so the warning does not restate it.
   **Auto-accepted (2026-10-03, `/fix` auto-accept rule), for the user to veto:** the warning is
   *resolution-level*, read off the built table's resolved operations, rather than the
   `ValidatePostParse` placement the question's option text described. Edge eligibility is known
   only at resolution, and that is the only place where "schema-allowed" is one value rather than a
   re-derivation. It adds no public surface, and reverses no ruling or stated behaviour. The
   existing flat-upsert `ConflictPK` warning keeps the tables that have a unique target but no PK
   target; a table with no target at all gets the over-reach warning only.
4. **§30.4.3's `generation_config.pagination` is dropped.** It could only ever read `true`.

**One call made without a new ruling.** §4.13's "`…_with_related` without its base" rule was not
deleted outright, as the task list said. The task list predates FIX-229 (resolved 2026-09-29),
which extended the rule to the API masks, where the mask conjoins each `…_with_related` key with its
masked base so that a hand-guarded door cannot be bypassed through the nested mutation. The client
half is gone, since it can no longer be expressed. The API half stays, restated as "A `…_with_related`
mask key enabled without its base". Deleting it would have reversed FIX-229's ruling.

**What changed in the PRD:**
- §4.6: "Always-enabled operations" became "Every method the schema allows is generated". The
  Operations subsection is now a schema-fact gate table (view, no PK, no soft-delete column, no
  incrementable column, no conflict target, nested-mutations opt-in) stated once, with the
  rationale. `OperationsConfig`, the client preset table and the usage examples are gone. The
  `nested_mutations` text no longer intersects with a per-table mask.
- §4.8 / §4.9: the `operations` row, the `upsert_many` example under *Nested mutations per table*,
  and `get_many` in the view mask list are gone. The full example moves its three `operations`
  settings under `api`.
- §4.13: new rows for a leftover `operations` key, a client-only key in a mask, and the
  schema-level over-reach warning. "Invalid preset" is scoped to the masks, the D20 rule to the
  masks, and the view row drops `get_many`. The validation phase list, the JSON-schema union row
  and the testing bullet are updated.
- §9 intro, §9.2 `UpsertMany`, §9.4a `Stream`, §9.8 Design Principle 1 and §20.2's conditional
  generation rules now cite the §4.6 gates.
- §9.9.4: **E4 and E9 are retired**, with no renumbering, since neither had a clause left: every
  generated target has `Create` / `CreateMany`, every table has `HardDelete*`, and every junction
  E13 admits has `UpsertMany`. E7 reads `update_where` and drops the batch-of-items sentence. E8
  and E10 lose their toggle clauses. E13 no longer cites E9. The intro no longer lists "an
  `operations` entry the target or the junction turned off".
- §8.5 naming and the nested-name claim: "the operations mask" becomes "the `api.operations` mask".
- §23.7: lint rule 3, "Unused table constant", is deleted.
- §26.4: the `deleteProduct` comment and the operations paragraph are updated.
- §26.5.1: every surface row is gated on its own mask key, plus the schema fact.
  `update<Table>s` is backed by `UpdateWhere`. `UpdateMany(items)` is added to the excluded table.
  The delete/restore naming table is restated over surviving variants. The mask section has a new
  opening premise, the preset table (moved from §4.6, restated over the API keys), the key list,
  the schema-fact over-reach warning, and a "Only API operations" list that adds `get_many` /
  `update_many`. The `_inc`/`_dec` paragraph and the hidden-entity paragraph read `update_where`.
- §26.8: the REST endpoint table is keyed by client method. Which mask key gates `get_many` /
  `exists` / `count` is left to D.1, when REST lands.
- §26.10: `audit_logs` moves under `api`. `read_write_no_delete`, a preset that does not exist, is
  replaced by `no_delete`. The "per-table `operations` config already drives client code
  generation" paragraph is rewritten.
- §30.4.3: `pagination` is removed from the example and the field table, with a note saying why.

**Acceptance grep.** `grep -n operations docs/PRD.md`, with `api.operations`,
`nested_mutations.operations` and plain-English uses of "operations" filtered out, leaves the
nested-mutations config rows (466, 474), keys inside `api:` blocks (1018, 1060, 1067, 1217, 1222,
12358, 12364, 12372, 12981), and the view `api` row (914). The prose that says client toggles do
not exist (382, 436, 449) and the leftover-key rows (§4.13, testing bullet) name `operations` only
to reject it. `#operations` and every other in-page anchor resolve (a slug check over all `](#…)`
links: 0 dangling).

**Auto-review (sqlgen-reviewer, 2026-10-03).** Criteria 2–5 passed; criterion 1 failed on five
leftover toggle passages. All findings were applied inline:
- The over-reach warning's "or whose base the mask turns off" clause was dropped from §4.13 and
  §26.5.1. Every such case is already the D20 error, so the clause could never fire.
- Toggle-premised passages were rewritten: §4.13's nested-name claim row, §9.4a's
  `AllowInTransaction` rationale, §9.6's `LockMode` / `AllowInTransaction` rationale, and §30.4
  `methods[]`, which now cites the §4.6 gates.
- §9.4a no longer says views get `Stream` (`templates/view/` has none).
- §26.4 view gating now says "three read keys".
- §26.5.1: the masked set zeroes `GetMany` as well as `UpdateMany`. The `ConflictPK` warning is
  scoped to tables that have a unique target, so a key never warns twice.
- The §26.8 header is back to "Operation".
- The reviewer's open question, whether the no-eligible-edge case warns, is settled by the
  resolution-level placement under decision 3.

**`/verify 29.0` (2026-10-03).** The independent reviewer confirmed criteria 2–5. Criterion 1 was
PARTIAL on one leftover, the §26 feature-mapping row "`Operations` config (read-only / disabled)",
and it was fixed inline. Further doc corrections applied:
- The delete/restore naming table gates `restore<Table>` on the `restore` key.
- The over-reach warning names `upsert_with_related` beside `upsert` for a table with no conflict
  target.
- STATUS.md now calls the warning resolution-level.
- IMPLEMENTATION_ORDER and 29.1's golden criterion now allow for the decision-4 manifest lines.

29.1's task list gained the code sites the review found unnamed: the strict decoder needing a
decode-only sentinel for decision 2, lint rule 3's `OpUpsert` coverage folded into rule 2, the
`validate.go` client-only error and the `ResolveTableOperations` callers, `v1.json` descriptions,
manifest readers and unit tests, `mcp/resources.go`, and `docs/configuration.md`. Its
`context_nested.go` line refs were corrected, and it gained tests for the over-reach warning, API-only
D20, and lint. No FIX logged; every finding was a doc correction.


## 29.1 Remove the client toggles — config, generator, API gates, tests

**PRD Reference:** Sections 4.6, 4.13, 9.8, 9.9.4, 20.2, 23.7, 26.5.1, 30.4.3 (as amended by 29.0)

**Status:** Complete

### Tasks

- [x] `config/config.go`:
  - remove `Operations` from `GenerationConfig` and `TableConfig`, with their preset defaulting
    (around 1852 and 2118–2122). Decision 2 needs the key to survive decoding: `decodeConfig` runs
    with `KnownFields(true)` (`config.go:1581`), so a deleted field fails with the plain
    unknown-field error before validation runs. Keep a decode-only sentinel field on both structs
    (for example a `*yaml.Node` tagged `operations`, which nothing reads except the validator) and
    reject it there with the §4.13 message;
  - keep the `Operations` type, `ExpandPreset` and `ResolveOperations`, now used only by the
    `api.operations` and `views.<name>.api.operations` masks;
  - apply decision 1 to `apiOperationFields` / `clientOnlyOperationFields` (851–884): `update_where`
    moves into the API list; `update_many` and `get_many` move to the client-only list.
- [x] Validation:
  - narrow the `…_with_related` without its base rule (`config/nested_mutations_validate.go:143–189`)
    to the two API masks. Its client half goes; the API half stays (FIX-229, PRD §4.13, see 29.0's record);
  - scope the invalid-preset rule to the masks;
  - apply decision 2 (leftover key, with the §4.13 message) and decision 3: move the over-reach
    warning out of `validateAPIOperations` (`config/validate.go:932–975`) to resolution level in
    `gen`, comparing each per-table mask's explicit `true`s against the built table's resolved
    operations. Next to it, `flatUpsertOmittedWarnings` (`gen/context_api.go:1630`) skips a table
    whose client has no `Upsert`;
  - update the view-mask comment and error text ("four read operations", `get_many`) at
    `config/config.go:788,895` and in `validate.go`;
  - fix the stale doc comments at `validate.go:917–931` and `:1033–1036`, and the client-only-key
    error at `:1053`, which tells users to "use tables.<name>.operations.%s". Re-point or remove the
    other `ResolveTableOperations` callers: `foundationalOperations` (`:1037`), `:1560`, and
    `apiCreateOpsEnabled` (`:1639`). Fix the `apiOperationFields` doc comment, which says "fifteen".
- [x] `config/schema/v1.json`: remove `operations` from the generation and table definitions; keep
  it in the `api` definitions. Update the shared `$defs/operations` descriptions (50–87), the
  `nested_mutations.operations` description (248) and the "Seven of these can be overridden"
  text (199).
- [x] `toResolvedOperations` (`gen/context_table.go:1225–1263`): derive every field from schema
  facts. Keep `ResolvedOperations`, so no template edits are needed for the client surface. The
  three `…WithRelated` fields follow `nested_mutations` (enabled, family list, eligible edge) alone.
- [x] Nested eligibility (`gen/context_nested.go`): remove the toggle clauses of E4 (376–380), E8
  (693) and E9 (995, 1019, 1087–1093), and the D20 conjunctions (122–130). Keep every schema-fact clause: the
  nullable FK, the E11 soft-delete junction, the conflict target.
- [x] API gates. Apply decision 1 across the FIX-224 site list:
  - `maskAPIOperations` (`gen/context_api.go:2504`), which masks `UpdateWhere` and zeroes `UpdateMany`
    and `GetMany` whenever a mask is set; `apiInputEmission` (2467) and `validateIncDecNamespace` (1760);
  - `flatMutationSurface` (`gen/funcmap.go:355`) and the read-surface helper (`:303`);
  - `hasAnyInputTranslator`, `tableMutationFieldNames` and `hasAnyMutationOp`
    (`gen/orchestrate.go:1310`, 2002, 2193; the `GetMany || Paginate` reads at 1991 and 2181);
  - the templates: `resolvers.go.tmpl:223`, `schema.graphqls.tmpl:155`, `seeds.go.tmpl:104` and
    `input_translate.go.tmpl:36`;
  - the `<t>List` gate, moving to the `paginate` key alone: `schema.graphqls.tmpl:136`,
    `resolvers.go.tmpl:90`, `seeds.go.tmpl:32` (`schema.graphqls.tmpl` is now at 139);
  - the `graphql` example: `tables.keyed_codes.api.operations.update_many: false` becomes
    `update_where: false` (FIX-224 vet).
- [x] `gen/context_api_nested.go`: delete `apiNestedTargets.updateManyMasked`. The relink gate reads
  the target's masked update operation, and the file header and struct doc follow.
- [x] Manifest: apply decision 4 to `generation_config.pagination` (`manifest/builder.go:236–243`).
  The `minimal` preset check there names a preset that does not exist. Also update the field's other
  readers: `cli/cmd_manifest_diff.go:348`, `manifest/emit_markdown.go:163`, `manifest/types.go:61`,
  `mcp/resources.go:67`, the unit tests (`builder_test.go:1064,1096`,
  `emit_markdown_test.go:64,74`) and `docs/design/MANIFEST.md` (766, 780, 1369, 1759).
- [x] `docs/configuration.md:205–230,337`: drop the client presets and toggles, and point at
  `api.operations`.
- [x] `cli/lint.go:596`: delete rule 3, "Unused table constant". First fold its resolved-operations
  check into rule 2 ("Invalid operation for table", `:568–592`). At HEAD, rule 3 is the only rule
  that reports a hook on `OpUpsert` / `OpUpsertMany` for a table with no conflict target
  (`:597–609`), and §23.7 rule 2 already covers "an operation that the table doesn't support".
- [x] Tests:
  - move the 40 Go `config.Operations{}` literals and the 6 YAML fixtures that set client toggles
    onto the masks, or delete them where they only exercised a toggle (heaviest:
    `gen/api_nested_test.go`, `config/nested_mutations_test.go`, `gen/nested_test.go`,
    `config/api_operations_test.go`, `gen/api_operations_mask_test.go`);
  - leave the direct `ResolvedOperations` assignments in the template tests as they are.
- [x] `make update-golden` and `make update-golden-e2e`. Explain any golden that moves. The manifest
  goldens move by decision 4: `generation_config.pagination` in `manifest_gen.json` (five examples)
  and the `Features:` line of `_index.md`.
- [x] `/fix resolve FIX-224`.

### Acceptance Criteria

- `generation.operations` and `tables.<t>.operations` fail validation, with the message decision 2
  chose. A config without either key validates exactly as before.
- For every table, resolved client operations equal the schema facts, and no template or context
  reads an operation value that only config could set.
- `api.operations` is the only per-operation control. `<t>List` is always backed by a generated
  `Paginate`, and `update<T>s` is gated on the key that names `UpdateWhere`. Neither of the two
  measured build breaks (`update_where: false`, `paginate: false`) can be expressed any more.
- Nested eligibility depends only on schema facts and `nested_mutations` config. On GraphQL it also
  depends on the target's `api.operations` mask (E7).
- `TestE2EGoldenFiles` is byte-identical across all 12 examples except the manifest's
  `generation_config.pagination` and the `_index.md` `Features:` line (decision 4), since no
  example sets `operations`. `make check` and `make check-examples` exit 0 under `-race`.

### Tests Required

- [x] Config: an `operations` key under `generation` and under `tables.<t>` each fails validation.
  Assert the message.
- [x] Resolution: a table-driven test that the resolved operations equal the schema facts for a
  view, a table without a soft-delete column, one without an incrementable column, a view whose
  inherited cursor keys do not resolve, and one without a conflict target.
- [x] Nested: the eligibility suite passes with the E4 / E8 / E9 toggle cases removed. The
  schema-fact cases (nullable FK, E11 soft-delete junction) still fail first when their clause is
  removed.
- [x] API mask:
  - masking `update_where` removes `update<T>s` and the O2M / has-one relink verbs
    into that table;
  - naming either retired key (`update_many`, `get_many`) in `api.operations` is a config error;
  - masking `paginate` removes `<t>List`.
- [x] Golden: `TestE2EGoldenFiles` byte-identical across all 12 examples, except the decision-4
  manifest lines.
- [x] Over-reach warning (decision 3): one case per gate (soft-delete column, conflict target,
  nested family not enabled, no eligible edge); a global mask stays silent; a table with no conflict
  target warns once, not also through the `ConflictPK` warning.
- [x] D20, API only: a mask with `create_with_related: true` beside `create: false` is still an
  error, and the same pair can no longer be written on the client.
- [x] Lint: a hook on `OpUpsert` for a table with no conflict target is still reported, now by
  rule 2.

### Completion Record

**Implemented 2026-10-03.** It started from FIX-224's vetted prototype (`.claude/prototypes/FIX-224.patch`,
applied cleanly at `2f3318fa` except for its PRD hunk, which 29.0 had already rewritten).

**What changed:**
- **Config.**
  - `GenerationConfig.Operations` and `TableConfig.Operations` are gone. Each is replaced by a
    decode-only `RemovedOperations yaml.Node` sentinel tagged `operations`. The strict decoder would
    otherwise reject a leftover key with a bare unknown-field error. `validateRemovedOperations`
    names the path and points at `api.operations` (decision 2).
  - `ResolveTableOperations`, `hasAnyOperationToggle`, `validateOperationsPresets` and
    `foundationalOperations` are deleted. The preset rule runs only on the masks
    (`checkAPIOperationsBlock`).
  - `get_many` and `update_many` move to `clientOnlyOperationFields`, with a hint naming the key
    that gates the surface the user meant (decision 1). Views take the three read keys.
  - D20 (`validateNestedWithRelatedBase`) keeps only its API-mask half.
  - `apiCreateOpsEnabled` and `resolvedCursorKeys` read the mask or schema alone.
- **JSON schema.** `operations` under `generation` and `tables.<t>`, and the seven client-only keys
  in the mask object, are declared `{"not": {}}` with a description. Editors flag them, and the
  struct-drift guard still finds a property for every field. The mask object's descriptions are
  rewritten over the 14 API keys.
- **Generator.**
  - `toResolvedOperations(softDelete, columns)` returns every method as true, except SoftDelete and
    Restore (soft-delete column) and Increment (incrementable column). The cursor-key and
    conflict-target gates still follow it.
  - The three `…WithRelated` flags start false. `wireNestedMutations` sets each to the method it
    emitted, from `nested_mutations` alone, so the D20 conjunctions are gone.
  - E4's and E9's code is deleted, along with E8's `UpdateWhere` clause and E13's "ahead of E9"
    ordering note.
- **API.**
  - `maskAPIOperations` zeroes both `GetMany` and `UpdateMany` whenever a mask is set (fail closed).
  - `<t>List` is gated on `Paginate` alone, in three templates, `tableQueryFieldNames`,
    `computeResolverFileFeatures`, `hasAnyResolverOp` and `funcHasAnyRead`. Views drop `GetMany`
    from their read set.
  - FIX-224's `update_where` rename and the `updateManyMasked` deletion came in with the prototype.
  - `validateIncDecNamespace` loses its dead `Update || UpdateWhere` guard.
- **Over-reach warning (decision 3).** `apiOverReachWarnings` (`gen/context_api.go`) runs at
  resolution level in both `planAPI` and `ValidateGeneration`. It compares each explicit per-table
  `true` for `soft_delete`, `restore`, `upsert` or a `…_with_related` key against the built table's
  resolved operations, and names the missing fact. That fact is the soft-delete column, the
  conflict target, the family `nested_mutations` does not enable, or no eligible edge.
  `flatUpsertOmittedWarnings` now reads the table context's Upsert, so a no-target table warns
  once.
- **Manifest (decision 4).** `generation_config.pagination` is removed from both `GenerationConfig`
  mirrors (runtime `manifest/types.go` and `cmd/sqlgen/manifest/types.go`), the manifest JSON schema
  ("eight toggles"), the builder, the markdown `Features:` line, the `manifest diff` map, and the
  MCP `config` resource description.
- **Lint.** Rule 3 is deleted. Rule 2 now reports every op whose method the resolved operations
  lack, with `opUnsupportedReason`: no soft-delete column, no incrementable columns, or no conflict
  target. So an `OpUpsert` hook on a no-target table is still caught.
- **Docs.** `docs/configuration.md` replaces its `operations` section with the schema-fact table
  and the `api.operations` mask. `docs/design/MANIFEST.md` drops `pagination`. PRD §4.13's cursor-key
  exemption no longer cites a Connection toggle.
- **Example.** In the `graphql` example, `keyed_codes.api.operations.update_many` becomes
  `update_where` (FIX-224). `tests/wrapper_rewriter_test.go` injects
  `tables.orders.api.operations` instead of a client block.

**Tests.**
- New:
  - `TestLeftoverOperationsKeyPointsAtTheAPIMask` (both levels, and both at once).
  - `TestValidateNestedMutations_ClientPairIsTheLeftoverKeyError`.
  - `TestOperations_ResolvedSetIsTheSchemaFacts` (one row per fact, plus a view without resolvable
    cursor keys).
  - `TestNestedOperations_FollowNestedMutationsAlone`.
  - `TestAPIOverReachWarnings` (one case per gate, a global mask silent, a satisfied key silent).
  - `TestAPIOperationsMask_ListFollowsPaginate`.
  - `TestLint_upsertHookOnTableWithoutConflictTarget`.
  - FIX-224's mask tests, from the prototype.
- Moved onto the masks: the `access_validate`, `config_test` full fixture, `validate_test` preset,
  `nested_mutations_test` key-parse, `schema_test`, `api_per_table`, `api_resolvers` and
  `api_schema` preset cases.
- Deleted, because they only exercised a toggle:
  - E4 target mask, E9 junction mask, link-step `upsert_many`, and target `UpdateWhere` narrowing;
  - `WithRelatedRequiresBase`, `ClientUpdateManyDoesNotReach…` and
    `ClientUpdateWhereNarrows…`;
  - the client rows of `UpdateTs`, the `UpdateWhereAlone` inc/dec subtest, the pre-parse
    over-reach tests, and the "connection off exempts" cursor case.
- **Fail-first:**
  - with the over-reach warning disabled, every positive case fails: 6 subtests across
    `TestAPIOverReachWarnings` and the flat-upsert test, as the reviewer re-measured;
  - without E11's soft-delete-junction clause, `UnlinkVerbSetPerShape/SoftTags` and
    `E11JunctionSoftDeleteKeepsTheLinkingHalf` fail;
  - without E8's nullable-FK clause, `DiscriminatorTheCreateCannotSetIsIneligible` fails, 4 cases;
  - with the seeds `<t>List` gate on `GetMany`, `ListFollowsPaginate` fails.

**Goldens.** `make update-golden` moves no unit golden. `make update-golden-e2e` moves:
- the decision-4 manifest lines in 5 examples (`postgres`, `mysql`, `sqlite`, `graphql`,
  `tenancy_postgres`): `"pagination": true` in `manifest_gen.json`, and `pagination` in
  `_index.md`'s `Features:` line, in both `expected/` and `models/`;
- the manifest package's own three goldens, regenerated through `go test ./manifest -update`,
  since neither make target touches them;
- **one line outside the manifest**, in `graphql`'s `event_hooks_gen.go`: `hook.OpIncrement` leaves
  a fail-closed redaction `case` on a table with no incrementable column. That table has no
  `Increment` method, so the op can never fire and behaviour is identical. It moved because
  Increment is now the schema fact, as this sub-item's own test requirement asks ("one without an
  incrementable column"), rather than an always-true toggle default. That is the one deviation
  from the "byte-identical except decision 4" criterion, and it is flagged here rather than masked.

**Auto-review (sqlgen-reviewer, 2026-10-03).** Criteria 1 and 3 to 6 passed; criterion 2 was
PARTIAL, on one real defect, fixed here.
- **The defect.** Tenancy attaches after `toResolvedOperations` and rebuilds `IncrementColumns`
  without the tenant column (`context_tenancy.go`), but left `Operations.Increment` stale. A table
  whose only numeric column is the tenant read `Increment=true` with no increment column. Templates
  and the manifest also check the columns, so generated code was unaffected. Lint rule 2, which now
  reads the flag alone, stopped reporting an `OpIncrement` hook on such a table.
- **The fix.** `Operations.Increment` is re-derived right beside the rebuild. The test is a
  failing-first assertion in `TestBuildTypeRegistry_IncrementMatchesGenerator` that
  `Ops.Increment` matches the column fact on every row: "tenant column is not incrementable" failed
  with `Ops.Increment = true, want false` before the fix. No golden moves.
- **Also applied:**
  - an over-reach case for `upsert_with_related` on a table with no conflict target, the one
    `overReachReason` branch with no test;
  - the dead `ops.UpsertWithRelated = false` in `omitUpsertWithoutConflictTarget` is removed, with
    a note saying why it is not needed;
  - the stale `"pagination": true` is deleted from `cli/testdata/manifest/valid_single.json` and
    `mcp/testdata/manifest_gen.json`;
  - the fail-first count and the manifest-golden regeneration path above are corrected.

`make check` exit 0 after the fixes, and `make check-examples` exit 0 again after them (20 example
packages ok). The fixes move no golden.

**`/verify 29.1` (2026-10-03).** The independent reviewer confirmed all six acceptance criteria and
all eight Tests Required. Its fail-first spot check at `context_tenancy.go` was CONFIRMED. It also
found no other post-assembly pass that changes a schema fact after `toResolvedOperations`. No FIX
was logged. Its doc corrections were applied inline:
- the `ExpandPreset` comment is restated over the mask, since it cited the client toggles and the
  retired E9;
- the IMPLEMENTATION_ORDER golden notes now name the `Features:` line and the `OpIncrement` line;
- `docs/configuration.md` says "Six `generation` fields";
- `docs/design/archive/NESTED_MUTATIONS.md` gains a Phase 29 superseded-claims bullet.

Its one question predates 29.1: §32.4's required-on-create rule ignores an upsert-only mask that
still emits `Create<T>Input`. The PRD's wording matches the code, so it went to the backlog as a
PRD gap. The 2026-10-01 backlog item "subtractive mask misses schema-gated operations (soft_delete
/ restore)" is now answered by the decision-3 warning, for `/close-phase 29` to drop.
