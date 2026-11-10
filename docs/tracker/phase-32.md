# Phase 32: Composite Foreign-Key Relationships

Status: In Progress
PRD Sections: 4.6, 4.8, 4.13, 5.3, 8.1.7, 8.2, 8.6, 8.7, 9.8.13, 9.9, 11.1, 13.1, 13.2, 13.4, 25.1, 29.10, 30, 31.3, Appendix A

> **Design.** `docs/design/COMPOSITE_FK_RELATIONSHIPS.md` (option D, ruled 2026-10-20) is the
> source for every sub-item below. Its §6 lists sub-items N.0–N.8, which are 32.0–32.8 here. Its §10
> behavior contract (retrofitted 2026-11-05) lists, in §10.5, the contract rows each sub-item turns
> from planned to passing, and §10.2 the regression guards; its §12 records contract rulings Q1–Q7.
> Its §7 records rulings R1–R7, each taking the recommended option from a measured probe (§9.4):
> - R1: pinned set = tenant column, child-PK members, members shared with another FK; E1 over
>   every non-tenant member; every member in the model on both sides.
> - R2: `Constraint.Match` on PostgreSQL only; under FULL, disconnect only when every member is
>   free and nullable.
> - R3: composite-PK parents and targets stay out of nested mutations (next phase).
> - R4: every loader step chunked at `floor(L / 2 / N)`.
> - R5: the four-step naming rule; a residual collision is a hard error.
> - R6: redundant FKs first, then the set-aside M2M rule with containment.
> - R7: `fk` / `junction_local_fk` / `junction_reference_fk` take a string or a list, mapped by
>   position onto the resolved PK; no `references` key.
>
> Three fixes landed first, on the N = 1 code of the same sites: FIX-358 (`c875fec9`, an excluded
> link column drops the edge), FIX-359 (`2ae215f8`, chunked loaders, M2M junction subquery,
> funcmap `loadChunkSize`) and FIX-360 (`244ce2ee`, an excluded PK column is an error).
>
> **Ground rules for every sub-item:**
> - **N = 1 invariant (design §4.9).** The Go goldens of every existing example are
>   byte-identical after each sub-item; `make update-golden` and `make update-golden-e2e` show no
>   Go churn. The manifest's shape moves once, in 32.3. The only other manifest change is
>   32.1's MySQL composite-PK batch SQL bodies. Every intended N = 1 change (§4.9's three
>   bullets) is on a schema no example has and gets its own regression test.
> - **Staging guard.** 32.2 changes the model before gen can use more than one pair. Until a
>   sub-item lifts it, `gen` keeps today's output for an edge with more than one key pair: a
>   detected one is omitted, as a composite FK is today, and a declared one is a generation error
>   naming the relationship and the sub-item that lifts it. 32.5 lifts it for reads; until 32.6,
>   such an edge is nested-ineligible with that reason (PRD §9.9.4). Each lift is a task in its
>   sub-item, and the guard is gone at closure. While it stands, two shapes lose an edge they have
>   today, since detection drops it for a composite edge the guard hides: a single-column FK inside
>   a composite one to the same target (rule 2), and a tenant-column FK set aside on a tenant
>   junction (rule 4). No example has either.
> - **Fail first.** Each behavior change starts with a test that fails at HEAD. Record the failure
>   in the Completion Record.
> - **Every dialect.** Every builder and loader change covers PostgreSQL (pgx and stdlib), MySQL
>   and SQLite (`guidelines/SQL.md`). Integration tests skip under `testing.Short()`.
> - **Findings** go through `/fix` → Findings triage. Never gate off a broken case to reach green.

## 32.0 PRD and doc sync — composite foreign-key relationships

**PRD Reference:** Sections 4.6, 4.8, 4.13, 5.3, 8.1.7, 8.2, 8.6, 8.7, 9.8.13, 9.9, 11.1, 13.1, 13.4, 25.1, 29.10, 30, 31.3, Appendix A

**Status:** Complete

### Tasks

- [x] Re-verify design §9.1 at HEAD and record the SHA in the design doc header:
  - `parser.Constraint` keeps a composite FK's `Columns` / `ReferenceColumns` on all three
    dialects and sources;
  - `ResolveReferenceNames` rewrites them to the target's declared spelling;
  - the composite-key builders at `sql/builder.go` render as the §9.1 table says;
  - count the `PKColumns[0]` relationship reads (6 `context_table.go`, 2 `context_relfilter.go`,
    12 `context_nested.go` at `244ce2ee`).
- [x] PRD §5.3 rewrite (design §4.2):
  - one edge per FOREIGN KEY constraint, read from `Table.Constraints` after reference-name
    resolution and PK overrides;
  - rule 1, the target key (the resolved PK or a non-partial UNIQUE, as a set), with the
    unresolvable-key warning and its SQLite fallback name `table(columns) → target(columns)`;
  - rule 2, redundant FKs;
  - rule 3, O2O when a PK or non-partial UNIQUE on the child is a subset of the FK's columns;
  - rule 4, the set-aside M2M rule and its two stated consequences;
  - rule 5, the four-step naming member; rule 6, disambiguation and the residual-collision
    error;
  - "detection gives the same answer under `files`, `database` and `both`".
- [x] PRD §13.1: O2O by unique subset; the edge's key is the FK's referenced columns, not the
  target's PK.
- [x] PRD §4.8 `TableRelationship` and §13.4 (design §4.7, R7):
  - `fk`, `junction_local_fk` and `junction_reference_fk` accept a string or a list;
  - a list maps by position onto the resolved PK of the table it references;
  - its length must equal that key's column count;
  - a string is a one-item list;
  - a manual edge joins the primary key, with no `references` key.
- [x] PRD §4.13 rows:
  - an FK whose referenced columns are not a key → warning, no edge (`resolution-level` or
    schema-level, per where 32.2 prints it);
  - a residual relationship-name collision → error naming both constraints and §13.4;
  - a manual list whose length differs from the referenced PK's column count → error;
  - extend the FIX-358 row (`exclude_columns` removes a link column) to every member on both
    sides.
- [x] PRD §8.2: increment exclusion covers every member of any FK constraint. §8.6: a
  single-column PK that is a member of any FK constraint is `caller`.
- [x] PRD §9.9 (design §4.3, §4.5):
  - define pinned and free members;
  - a relationship with no free member is readable, not writable, with its §9.9.4 reason;
  - E1 over every non-tenant member;
  - connect verifies pinned members (typed error before any write) and writes free ones;
    `allow_reparent` never moves a pinned member;
  - disconnect nulls free members, eligible under SIMPLE when every free member is nullable and
    under FULL only when every member is free and nullable;
  - E5 and E11 over the union of both junction key lists, where they say "two FKs" today;
  - the §9.9.3 matrix M2M row: "exactly its two FKs" → the union of both key lists;
  - E1 / E3 still exclude composite-PK parents and targets (R3).
- [x] PRD §11.1: relationship filters correlate on every pair; the composite-PK-parent skip is
  lifted.
- [x] PRD §25.1: a composite-key O2M on pgx expands per key, so `s = C` there; one sentence.
- [x] PRD §29.10: the composite tenant FK; a tenant member in the key marks the edge as
  database-guarded (design §8).
- [x] PRD composite tuple-IN rows now say MySQL uses tuple IN: §8.1.7 (~2308), §8.7 Generated
  SQL (~3231), §9.8.13 (~7163), §11.1 (~8018), Appendix A `SupportsTupleIN` (~16053).
- [x] PRD §30 manifest shape: `fk: {table, schema, columns, references, constraint}`, `junction`
  with column lists, `schema_version` `0.2.0`. Sync `docs/design/MANIFEST.md`.
- [x] PRD §31.3 `sqlgen_find_referencing`: `fk_columns` list, dedup on the joined list. Sync
  `docs/design/MCP.md`.
- [x] PRD §13.7.1 dedup key: `fk_column` → the FK column list.
- [x] `guidelines/SQL.md`: the `SupportsTupleIN` comment (~line 57) and the composite-key rows;
  `sql.Exists` with N correlation pairs.
- [x] Cross-check every edit against design §4 and §7.

### Acceptance Criteria

- The PRD states design §4 as normative text, and no section still describes a relationship as
  one column per side, except where `Column.FKReference` types a comparator (§4.6).
- Every new error and warning has a §4.13 row.
- `make check` is green (`refs-check` included).

### Tests Required

- [x] None (docs only). `make check` green.

### Completion Record

Completed 2026-10-22 on `design/tenant-composite-fk-relationships`, uncommitted (docs only).

**§9.1 re-verified at `244ce2ee`.** The composite-FK parity tests pass under `files` and
`database` on all three dialects (`TestPostgresIntrospect_TableConstraints`,
`TestMySQLIntrospect_TableConstraints`, `TestSQLiteIntrospect_TableConstraints`,
`TestParityFixtures`). A throwaway SQLite probe of `(workspace_id, cycle_id) → Cycles
(WORKSPACE_ID, ID)` gave identical `Columns` and resolved `ReferenceColumns` under both sources.
The builders sit at `sql/builder.go:461-531`. The `PKColumns[0]` reads are 6 / 2 / 12, unchanged.

**Files.**
- `docs/PRD.md`: §4.6 FK stringification; §4.8 `fk` / junction keys as string-or-list, an
  example, "a manual relationship joins the primary key"; §4.13 three new rows (non-key FK
  warning, residual name collision, `fk` list length), the link-column row extended to every
  member, the fk-placement row, and the schema-level phase list; §5.2 MATCH type; §5.3 rewritten
  (rules 1–6); §8.1.7, §8.7, §9.8.13, §11.1 and Appendix A tuple-IN rows; §8.2; §8.6; the
  structurally required columns table; §9.9.2 pinned and free members; §9.9.3 key semantics and
  MATCH FULL; E1, E3, E5 and E8; §9.9.5 nested child input; §9.9.6 the pinned check on
  `connect`; the §9.9.8 error row; §9.9.10; §11.1 N-pair `EXISTS` and the composite-PK skip
  lifted; §11.5 `Exists`; §13.1; §13.2; §13.3; §13.4; §13.7.1 dedup key; §25.1; §29.10;
  §30.4.2 `fk` / `junction` shape; §30.5 `0.2.0`; §31.3 `fk_columns`.
- `docs/design/MANIFEST.md`, `docs/design/MCP.md`: synced to the new shapes.
- `guidelines/SQL.md`: `SupportsTupleIN` true everywhere, the prefixed tuple IN, N-pair
  `Exists`, and two feature-matrix rows.
- `docs/design/COMPOSITE_FK_RELATIONSHIPS.md`: header SHA, §9.1, §4.3, §4.5, §4.8, and a new
  §7.1 with the rulings below. 32.4 and 32.6 tasks in this file follow them.
- `docs/tracker/backlog.md`: one line (`Filter.PKs` and tuple IN).

**Rulings (2026-10-22, design §7.1; each took the recommended option).** Five while writing:
- An edge with no free member keeps `create` and loses the rewrite verbs silently. M2M is exempt.
- M2M unlink needs a junction primary key equal to the union of both keys, not "PK or UNIQUE".
- A pinned-member mismatch is `ErrAlreadyRelated` (`CONFLICT`).
- Manifest `junction` is `{table, schema, local_fk: {columns, references, constraint},
  target_fk: {…}}`.
- PRD §9.8.13 now describes `Filter.PKs` as the code renders it (OR of ANDs on every dialect).
  The switch to tuple IN went to the backlog.

Four more on the auto-review's findings:
- The nested child input elides every key member, the tenant column included, and E1 covers
  every member. This supersedes R1's "non-tenant" wording and keeps a single-column tenant FK at
  N = 1 (the reviewer confirmed `context_nested.go:633` drops it unconditionally).
- §5.3 rule 1 also accepts the declared PRIMARY KEY constraint, so the PK-override form keeps
  its edge (design §4.9).
- The residual name collision is resolution-level, checked after `exclude_relationships` and
  manual replacements apply. 32.2's rule 6 task moved accordingly.
- Under MATCH FULL with a pinned member, `connect` is generated only with `allow_reparent`.

**Auto-review (sqlgen-reviewer).** Four high-confidence issues: the three above plus a factual
error. PRD §5.3 and §4.13 said MySQL refuses an FK to non-key columns. MySQL 8.0 accepts one to
any indexed columns, and 8.4 refuses it only while `restrict_fk_on_non_standard_key` is on.
Corrected in the PRD and in design §4.2, and §4.9 now lists the MySQL N = 1 edge loss (not
measured). Doc fixes applied inline:
- design "four" → "five" rulings, now nine;
- PRD §9.9.2 single-member wording;
- §13.1 "referenced table's primary key";
- §13.4 `constraint` "omitted";
- §9.9.4 silent-drop list;
- the "parent's PK" phrasings (§9.9.6, §9.9.7, two disconnect passages);
- §25.1 "per key column" and the `target_key`/`ref_key` spelling;
- "relationship-filter skip" dropped from the next-phase lists (design §6, IMPLEMENTATION_ORDER);
- 32.6 acceptance wording (silent drop; `ErrNotFound` under tenancy, `ErrAlreadyRelated` under
  `SkipTenancy`).

The reviewer's low-risk note goes to 32.2: a column with two single-column FKs to different
tables gives one edge today (`fkReferenceOwner`) and two under the new rules.
`guidelines/SQL.md` states 32.1's target state in the present tense.

**For 32.1:** the §11.5 `sql.Exists("id", …)` example shows the one-pair call. Update it to
the signature 32.1 picks.

**Tests.** None (docs only). `make check` green, `refs-check` included.

## 32.1 Runtime — N-pair `Exists`, prefixed tuple IN, MySQL tuple IN

**PRD Reference:** Sections 9.8.13, 11.1, Appendix A

**Status:** Complete

### Tasks

- [x] Fail first: a `sql` test that prefixes a tuple-IN condition and renders it on PostgreSQL and
  SQLite. Today it gives `c.("workspace_id", "id") IN (…)` (PostgreSQL 17: `syntax error at or
  near "("`; SQLite: `near "(": syntax error`, design §4.8).
- [x] `sql.PrefixConditions` (`sql/condition.go:223`): prefix each column of a tuple-IN
  condition, giving `(c."workspace_id", c."id") IN (…)`.
- [x] `sql.Exists` (`sql/condition.go:202`): take N correlation pairs instead of one
  `correlationColumn` string. The subquery references one correlation token per pair, and
  `builder.go:639` replaces each. Pick the token form (an indexed `CorrelationToken`) and
  document it on the type.
- [x] Update every caller of `sql.Exists`: the relationship-filter template
  (`gen/templates/shared/_filter.tmpl:226`) is the only one in generated code. N = 1 output is
  byte-identical.
- [x] `MySQLDialect.SupportsTupleIN` (`sql/mysql.go:103`) → true; fix its comment. Check every
  `SupportsTupleIN` branch (`BuildCompositePKBatchCondition`, `Filter.PKs`, keyset cursors) for
  a MySQL-only path that becomes dead, and remove it.

### Acceptance Criteria

- A prefixed tuple-IN renders valid SQL on all three dialects.
- `Exists` with 1 pair renders what it renders today; with 2 pairs it ANDs two correlated
  equalities.
- MySQL composite-key batches use `(a, b) IN ((?, ?), …)`. Design §4.8 measured it at 8.0.46 and
  8.4.11 (same range scan; 32 ms vs 1.49 s at 10000 pairs).
- Go goldens: only MySQL composite-PK batch SQL may change. List every such file in the
  Completion Record, and confirm the change is the tuple-IN flip alone.

### Tests Required

- [x] Prefixed tuple IN on PostgreSQL, MySQL and SQLite (table-driven, fails first).
- [x] `Exists` with 1 and 2 pairs on every dialect, rendered SQL and argument order.
- [x] MySQL tuple IN against a live MySQL (testcontainers) for a composite-PK `HardDeleteMany`
  (`Filter.PKs` stays OR-of-ANDs).
- [x] Existing composite-PK example suites (`mysql`, `tenancy_mysql`) pass.

### Completion Record

Completed 2026-10-23 on `design/tenant-composite-fk-relationships`.

**Fail first.** `TestBuildSelectJoin_PrefixedTupleIN` failed at `912082d5`. PostgreSQL (pgx and
stdlib) and SQLite rendered `c.("workspace_id", "cycle_id") IN ((…))`, and MySQL rendered the
prefixed OR of ANDs. The reviewer reproduced the failure in a HEAD worktree.

**API picked.**
- `sql.Exists[C string | []string](correlation C, subquery Subquery)`. A string literal infers
  `string`, so the generated one-pair call `sql.Exists("id", …)` and every Go golden stay
  byte-identical. A list takes one column per pair.
- Tokens: `sql.CorrelationTokenAt(i)`. Index 0 is `CorrelationToken` (`{{parent}}`); the others
  are `{{parent.i}}`. One `strings.Replacer` pass substitutes them all, so a substituted column is
  never rescanned.
- The composite-key batch condition is now a structured value (`tupleInCondition`: quoted
  columns and rows), so `PrefixConditions` qualifies each column.

**Dead paths removed.** `buildExpandedOR` and the manifest's `!SupportsTupleIN` branch. Keyset
cursors and `Filter.PKs` never branched on the flag. With every dialect true and no reader left,
`SupportsTupleIN` came off `Dialect` and the three dialects (ruled 2026-10-23, after the
review), with PRD Appendix A and `guidelines/SQL.md` synced.

**Files.**
- `sql/condition.go`, `sql/builder.go`, `sql/dialect.go`, `sql/mysql.go`, `sql/postgres.go`,
  `sql/sqlite.go`;
- `cmd/sqlgen/manifest/sql.go`;
- the comments in `cmd/sqlgen/gen/context_relfilter.go` and `filter_relationship_test.go`;
- tests: `sql/builder_test.go` (MySQL joins the tuple-IN table, the expanded-OR test is gone) and
  `mysql/tests/composite_pk_test.go` (`TestBatchByCompositePK`);
- docs: PRD §11.5 `Exists` example and Appendix A, `guidelines/SQL.md`, design §4.8.
- No template changed.

**Goldens.** No Go golden changed. The manifest moved in 3 tables × 4 files, all in the `mysql`
example: `{expected,models}/manifest/{asset_document_link,product_tag_label,user_category}.md`
and `manifest_gen.json`. The only change is the MySQL `HardDeleteMany` body, `WHERE <pks>` →
`WHERE (cols…) IN (<pks>)`, which is the tuple-IN flip alone.

**Tests.**
- `make check` green, `refs-check` included.
- `make check-examples` green on all 15 example modules, `mysql` and `tenancy_mysql` included,
  re-run after the flag removal; `TestE2EGoldenFiles` green without `-update`.
  The `mysql` `UpsertMany` composite read-back and the new `HardDeleteMany` test run tuple IN
  against a live MySQL.

**Auto-review (sqlgen-reviewer).** No behavior defects; fail first confirmed. Doc fixes applied
inline:
- the manifest ground rule above;
- the live-MySQL Tests Required wording;
- design §4.8's prefix and MySQL bullets;
- a stale `buildExpandedOR` reference in `mysql/tests/upsert_many_test.go`;
- a test section header;
- three reflowed comments.

**Verify (2026-10-23).** An independent reviewer pass agreed: PRD 5/5, tests 4/4, fail first
confirmed again, no `SupportsTupleIN` reference left in code or living spec. Its doc fixes
(STATUS, IMPLEMENTATION_ORDER and design §6/§9.3 wording) were applied inline. No FIX logged.

## 32.2 Parser model and detection — one edge per FOREIGN KEY constraint

**PRD Reference:** Sections 5.2, 5.3, 13.1, 13.5

**Status:** Complete

### Tasks

- [x] Start from `.claude/prototypes/composite-fk/detect-proto.patch` (it applies at `244ce2ee`;
  it adds `parser/proto_detect.go` and `cmd/sqlgen/cli/proto_detect_test.go`). Fold its rules into
  `parser.DetectRelationships` and its fixtures into `parser/relationship_test.go`. Delete the
  prototype files.
- [x] `parser/schema.go`: add `KeyPair{Local, Referenced}`. On `Relationship`, add `Constraint`,
  `Key`, `JunctionTable`, `JunctionLocal` and `JunctionReference`, and remove `FKColumn`,
  `JunctionLocalFK` and `JunctionReferenceFK` (`parser/schema.go:349-352`). Key lists are in the
  FK's declared column order.
- [x] `parser.Constraint.Match` (PostgreSQL only, R2):
  - the file parser reads it from `pg_query` `Constraint.FkMatchtype`;
  - introspection reads it from `pg_constraint.confmatchtype`;
  - MySQL and SQLite leave it SIMPLE;
  - `assertSchemaParity` covers it.
- [x] Detection (design §4.2), from `Table.Constraints`:
  - **rule 1, target key:** the resolved PK, the declared PRIMARY KEY constraint, or a
    non-partial UNIQUE, as a set (design §7.1); `REFERENCES t`
    with no column list is the PK; empty SQLite `ReferenceColumns` is unresolvable;
  - **rule 2, redundant FKs:** a subset of another FK's pairs to the same target gives no edge;
    of two identical FKs, the one whose name sorts first wins (the first declared when
    unnamed);
  - **rule 3, O2O vs O2M:** by a unique subset of the FK's columns;
  - **rule 4, M2M:** set aside every FK whose columns all belong to the table's other FKs; then
    exactly two remain, and a PK or non-partial UNIQUE contains their union;
  - **rule 5, naming:** the four-step naming member;
  - **rule 6:** self-reference disambiguation keyed on the naming member. The residual name
    collision error is checked after `exclude_relationships` and manual replacements apply
    (resolution-level, PRD §4.13; design §7.1), so it lives in `gen`, not in detection.
- [x] The unresolvable-key warning: printed after detection, not through `schema.Warnings`,
  which is printed first. It names the constraint (or `table(columns) → target(columns)` on
  SQLite) and §13.4.
- [x] `Column.FKReference` stays a single-column property (design §4.6). Confirm
  `deriveFKReferences` still sets it only from single-column FKs.
- [x] Mechanical migration of every reader of the removed fields to `Key[0]` / `JunctionLocal[0]`
  / `JunctionReference[0]`: `gen`, `config`, `manifest`, `mcp`, `cli`. No behavior change for
  N = 1.
- [x] Staging guard (see ground rules): `relationshipTargets` omits a detected edge with
  `len(Key) > 1` (or either junction list longer than 1), keeping today's output. Pin it with a
  test that 32.5 inverts.
- [x] `applyConfigDeclaredFKs` and manual relationships produce one-pair `Key` lists. 32.3 adds
  the list form.

### Acceptance Criteria

- Every FOREIGN KEY constraint whose referenced columns are a key yields exactly one edge, or
  none when it is redundant or set aside on a junction. Any other FK yields a warning and no edge.
- Detection gives identical edges under `files` and `database` on PostgreSQL, MySQL and SQLite.
- Running detection twice changes nothing.
- All 15 examples produce the same edges, types and names as today (design §9.4). The prototype
  run also found `cycles → tasks` and `teams → projects` on `sqlgen-example`; record that its
  probe repeats it.
- Intended N = 1 change: a SQLite single-column FK to a non-key column loses its edge and warns
  (fixture 10b, design §4.9).
- No Go golden changes.

### Tests Required

- [x] `parser/relationship_test.go`, table-driven (design §6.1):
  - the tenant pattern → O2M; a UNIQUE subset → O2O;
  - reversed column order; FK to a non-PK UNIQUE; FK to a composite PK;
  - FK to non-key columns → no edge, one warning;
  - a tenant junction → M2M; a three-single-FK junction → three O2Ms;
  - `cycle_settings` (workspace FK plus composite FK, UNIQUE on both) → O2O, not M2M;
  - redundant single-plus-composite → one edge; duplicate FKs → one edge;
  - `order_items` and `shipments` set aside a real parent;
  - naming for each R5 step, including `cycle_notes` → `cycle` / `prev_cycle`;
  - a residual collision → error;
  - a PK override moves the target key;
  - detection is idempotent.
- [x] Parity: each shape under `files` and `database` on all three dialects, through
  `assertSchemaParity` and the `parser/introspect` integration tests.
- [x] `Constraint.Match`: PostgreSQL `MATCH FULL` under both sources; MySQL and SQLite SIMPLE.
- [x] The staging-guard test: a composite FK generates the same Go as today (no edge).
- [x] `TestE2EGoldenFiles` green with no golden change.

### Completion Record

Completed 2026-10-23 on `design/tenant-composite-fk-relationships`, uncommitted.

**Fail first** (throwaway probe in a HEAD worktree, and the new CLI tests run there):
- the tenant FK `(workspace_id, cycle_id) → cycles(workspace_id, id)` gave no `cycles → tasks` edge;
- a second `DetectRelationships` run doubled the edges (2 → 4);
- a SQLite FK to the non-key `workspaces(slug)` gave an O2M edge and no warning
  (`TestDetectionWarningIsPrinted` fails at HEAD);
- `TestGenerate_DetectedNameCollisionIsAnError` failed at HEAD with the generic "both resolve to
  the Go field" message.
- `TestGenerate_CompositeKeyEdgeIsOmitted` passes at HEAD by design: it pins today's output, and
  32.5 inverts it.

**Files.**
- Parser:
  - `parser/schema.go` (`KeyPair`, `Relationship` keys, `Constraint.Match`, `MatchType`);
  - `parser/relationship.go` (rewritten);
  - `parser/postgres/postgres.go` (`FkMatchtype`);
  - `parser/introspect/postgres.go` (`confmatchtype`).
- CLI: `cmd/sqlgen/cli/{generate,pipeline,validate}.go`, which print the detection warnings.
- gen:
  - `cmd/sqlgen/gen/relationship_targets.go` (`compositeKeyEdge`, `excludedKeyMember` over key pairs);
  - `context_table.go` (key-list migration, the guard at both loops, the collision error);
  - `context.go` (`RelationshipContext.Constraints`).
- Comments: `cmd/sqlgen/manifest/builder.go`.
- Tests:
  - `parser/relationship_test.go` (rewritten; `junction_temp_test.go` removed);
  - `parser/introspect/relationship_parity_test.go` (new);
  - `parser/{postgres,mysql,sqlite}/*_test.go`;
  - `parser/schema_test.go`;
  - `cmd/sqlgen/cli/relationship_detection_test.go` (new);
  - fixture migration in `cmd/sqlgen/{gen,manifest,cli}` tests.
- Docs: PRD §5.3, design §4.1, §4.2, §4.9, this file, `STATUS.md`.

**Detection** (`parser/relationship.go`, rewritten from the prototype's rules):
- It reads `Table.Constraints` and replaces `schema.Relationships` instead of appending to it,
  so it is idempotent. It returns the non-key warnings, and `generate`, `validate` and
  `loadAndValidate` print them after `schema.Warnings`.
- Each table's FKs are ordered by their columns' positions, which reproduces today's
  column-order iteration for N = 1.
- Rule 2's tie-break keeps the identical key whose name sorts first, whatever order each lists
  its columns in, and the first declared of unnamed (SQLite) ones.
  - Every source agrees: MySQL's catalog keeps no declared order (probed: `SHOW CREATE TABLE`
    and `information_schema` list by name).
  - SQLite introspection recovers FK names from the stored DDL (probed).
- Rule 1 reads the resolved PK, the declared PRIMARY KEY and non-partial UNIQUEs. Rule 3 reads
  the resolved PK and UNIQUEs, which is today's O2O rule. Rule 4 reads only PRIMARY KEY and
  UNIQUE constraints, so a `primary_key` override over two FKs still makes no junction
  (`TestApplyPrimaryKeyOverrides_CompositeIsNotAJunction`).
- Rule 4 also counts FKs that give no edge: one into a table outside the schema, or one whose
  referenced columns are not a key. A junction with such an end gives no edge, as today. This was
  added on the review's finding: without it, a `team_members` junction into Supabase's
  `auth.users` grew a new `TeamMembers` O2M. It is pinned by two fixtures.
- `Relationship` also carries `JunctionLocalConstraint` / `JunctionReferenceConstraint`, which
  PRD §30.4.2's manifest `junction` needs. Design §4.1 is updated. An unnamed FK is named
  `table(columns) → target(columns)`.
- The residual name collision is checked in gen's `validateDuplicateRelationshipFields`, after
  exclusions and replacements, and names both edges' constraints (`RelationshipContext.Constraints`).
  It is reachable at N = 1 today: `users.organization_id` beside an `organization_users`
  junction names both edges `organization_users`. `exclude_relationships` clears it (tested).

**Tasks recorded as found:**
- `Column.FKReference`: `fkReferenceOwner` still skips any FK with `len(c.Columns) != 1`
  (`parser/canonical.go`).
- The migration touched only gen (`relationshipToContext`, `excludedKeyMember`, which now checks
  every pair on both sides) and test fixtures. `config`, `manifest` and `mcp` read gen's
  `RelationshipContext` or the manifest types, never `parser.Relationship`.
- No `Key` lists come from config yet. Neither `applyConfigDeclaredFKs` (it only stamps
  synthetic `FKReference`s, which detection no longer reads) nor manual relationships (they build
  `RelationshipContext` directly) produce a `parser.Relationship`. 32.3/32.4 own that path.
- The prototype's patch was folded in, never applied to the tree. The patch file is still in
  `.claude/prototypes/composite-fk/`.

**Staging guard.** `compositeKeyEdge` (`gen/relationship_targets.go`) drops a detected edge whose
key, or either junction key, has more than one pair. It applies at both `schema.Relationships`
loops (`buildRelationshipContexts`, `findO2ORelationships`).
`TestGenerate_CompositeKeyEdgeIsOmitted` covers composite O2M, O2O, a chained O2O reaching it,
and a tenant junction: the generated Go is byte-identical to the same schema without those FKs.
A mutation of the chained-O2O site or of the M2M arm fails it. Known gap until 32.5 (ground rules
updated): a single-column FK made redundant by a composite one, and a tenant-column FK set aside
on a tenant junction, lose today's edge. No example has either.

**N = 1 invariant.** A probe over all 15 examples and `~/sqlgen-example` (files source, PK
overrides applied) gave identical edges, names and types before and after, with no warnings.
`sqlgen-example` gains exactly `cycles → tasks` and `teams → projects`, which repeats the
prototype's result. Intended N = 1 changes, none in an example: a SQLite FK to a non-key column
warns and loses its edge (also as a junction end); a column with single-column FKs to two
tables gives two edges (design §4.9 bullet added).

**Tests.**
- `parser/relationship_test.go`: 50 table-driven DDL cases on all three dialects, plus
  idempotency. `junction_temp_test.go` is folded in.
- MATCH: parser tests on PostgreSQL (FULL inline, table-level and by ALTER; default and explicit
  SIMPLE; PARTIAL is a parse error), MySQL and SQLite (FULL/PARTIAL recorded SIMPLE).
- `parser/introspect/relationship_parity_test.go`: `TestDetectRelationships_SourceParity` checks
  identical edges and warnings, MATCH, constraint and schema parity on PostgreSQL, MySQL and
  SQLite.
- `cmd/sqlgen/cli/relationship_detection_test.go`: the staging guard, the warning under
  `validate` and `generate`, and the collision error and its remedy.
- Hand-built gen, manifest and cli fixtures that set only `FKReference` go through a test
  `detectRelationships` helper that adds the FOREIGN KEY a parsed schema carries. The reviewer
  confirmed every parser and introspector path does.
- `make check` green (`refs-check` included). `TestE2EGoldenFiles` green without `-update`.
  `make update-golden` and `make update-golden-e2e` left no golden change. The full parser
  integration suite (`go test ./...` in `parser`) and `make check-examples` (all 15 modules)
  are green, and were re-run after the review fixes.

**Auto-review (sqlgen-reviewer).** Fail first confirmed by mutation: without
`compositeKeyEdge` a `Tasks []*Task` field appears. Two high-confidence findings, both fixed:
- H1, the junction end that gives no edge (above);
- H2, the staging guard's O2O, chained-O2O and M2M arms were unexercised. The test now covers
  all three, and mutations of the chained-O2O site and the M2M arm fail it.

Doc fixes applied inline:
- six comments that named deleted functions (`addO2O`, `addO2M`, `junctionConstraint`);
- "non-partial" in the `junctionKeys` doc;
- `excludedKeyMember`'s design-doc citation → PRD §4.13;
- design §4.1 (the junction constraint fields) and §4.9 (two edges from one column);
- this file's staging-guard ground rule (the two shapes it loses).


**Verify (2026-10-23).** An independent reviewer pass confirmed fail first by mutation:
`ORDER BY con.conname` fails the PostgreSQL parity test. It found two defects in this
(uncommitted) work, fixed inline with a fixture each, so no FIX was logged:
- the naming member counted FKs that rule 2 discards, so a duplicated FK to a UNIQUE column lost
  its name. Two such columns collided where HEAD named them `author_email_posts` and
  `reviewer_email_posts`. `namingMember` now reads the table's FKs less the redundant ones;
- identical FKs listing their columns in different orders kept the wrong one. The tie now
  compares names, then the declared index.

Doc fix applied: the `sortForParity` comment.

**Rulings (2026-10-23).**
- Rule 2 keeps the identical key whose name sorts first, and the first declared of unnamed
  ones, with no parity exception. A "first declared" ruling was tried and reverted: MySQL could
  not honour it under `database`, and `files` and `database` must not answer differently. PRD
  §5.3 rule 2 and design §4.2 are updated.
- PRD §5.3 rules 1, 3 and 4 each say which primary key they read:
  - rule 1 reads the resolved and the declared PK;
  - rule 3 reads the resolved PK;
  - rule 4 reads the declared constraint.
  Rule 4 also states that an FK which gives no edge still counts, and that a junction with such
  an end gives no edge.

## 32.3 Config, manifest and MCP shapes

**PRD Reference:** Sections 4.8, 4.13, 13.4, 30, 31.3, Appendix B

**Status:** Complete

### Tasks

- [x] First, before any change (design §12 Q3):
  - register the new design §10.1 IDs in their registry files (`client.md`, `cli/gen.md`,
    `cli/config.md`, `cli/manifest.md`, `cli/mcp.md`, `runtime/support.md`), each with its planned
    test name;
  - add the four `relationships` matrix rows (`composite-o2m`, `composite-belongs-to`,
    `composite-has-one`, `composite-m2m`) to `cli/gen.md`'s matrix header, with design §10.3's
    `n/a` cells (Q2);
  - write the two unguarded design §10.2 tests against today's code; both must pass before any
    change lands: a single-column FK whose member is a PK member or the tenant column is
    `create`-only (`gen.nested.single-column-pinned-create-only`), and the nested child input
    drops a single-column tenant FK's column.
- [x] PRD Appendix B gains two rows (design §12 Q5): "FOREIGN KEY `MATCH FULL` enforced"
  (PostgreSQL yes; MySQL parsed, ignored; SQLite ignored) and "Bind-parameter limit"
  (65535 / 65535 / 32766). Re-hash the registry's Appendix B anchor.
- [x] `config.TableRelationship.FK`, `JunctionLocalFK` and `JunctionReferenceFK`
  (`cmd/sqlgen/config/config.go:722-725`) become a string-or-list type with `UnmarshalYAML`. Decode
  it like `Operations` and `NullableVariant` (`config/config.go:564,630`). A map is rejected
  (`cannot unmarshal !!map into []string`, design §9.4 R7).
- [x] `cmd/sqlgen/config/schema/v1.json`: `oneOf` string / string array for the three keys; keep
  the `required: ["fk"]` conditionals.
- [x] Validation: a list maps by position onto the resolved PK of the table it references; a
  length mismatch is an error (the §4.13 row from 32.0). The existing link-key and
  `fk`-placement rules apply to each item.
- [x] Manual relationships build `Key` / `JunctionLocal` / `JunctionReference` from the list. A
  declared edge with more than one pair hits the staging guard's error until 32.5.
- [x] Manifest (`cmd/sqlgen/manifest/types.go`, `manifest/types.go`):
  - `fk: {table, schema, columns, references, constraint}`, where `references` records the
    referenced columns;
  - `junction` with column lists;
  - `SchemaVersion` `0.1.0` → `0.2.0` (`cmd/sqlgen/manifest/types.go:19`);
  - the loader in `manifest/load.go` follows;
  - the JSON Schema follows.
- [x] MCP `find_referencing` (`cmd/sqlgen/mcp/tools_graph.go:39,80-89,119`): `fk_column` →
  `fk_columns`; the dedup key compares the joined list.
- [x] Regenerate the manifest goldens once (`make update-golden-e2e`) and review the diff: only
  the `fk` / `junction` shape, `constraint` and `schema_version` change.

### Acceptance Criteria

- `fk: cycle_id`, `fk: [workspace_id, cycle_id]` and the block-list form all decode; a map is
  rejected; every existing config means what it means today.
- The manifest names the FK constraint behind each detected edge, and `""` / absent for a
  manual one.
- Go goldens are unchanged. Manifest goldens change only in the shape above.

### Tests Required

- [x] Config decode, table-driven: string, flow list, block list, map rejected, empty list.
- [x] Validation: a list length mismatch with the referenced PK → error.
- [x] `v1.json` accepts both forms and rejects a map (the existing schema test).
- [x] Manifest load (`manifest/load_test.go`) at `0.2.0`.
- [x] MCP `find_referencing` dedup with a two-column key and a one-column key to the same target.
- [x] The two design §10.2 guards, passing on the code before this sub-item's changes.
- [x] Every design §10.5 row for 32.3 passes, and its registry row lists the test.

### Completion Record

Completed 2026-11-05 on `design/tenant-composite-fk-relationships`, uncommitted. Verified and
marked done 2026-11-05 (`/verify 32.3`: all rows pass, no FIX logged).

**Registration first** (design §12 Q3). New §10.1 IDs went into their registry files with empty
Tests cells, since `req-check` rejects a test name that does not exist yet; this sub-item filled
its own as the tests landed. A matrix cell is claimed by one row, so design §10.3's reuse of
`gen.relationships.composite-key-edge` and `load.o2o-composite-key` across cells could not be
registered as written. `rel.composite-belongs-to.struct-field`, `rel.composite-has-one.struct-field`,
`rel.composite-m2m.struct-field` and `rel.composite-has-one.loader` take those cells (design §10.3
updated). The three to-one / belongs-to cells are `n/a` with their PRD reasons.
`e2e.composite-tenant-schema` is not registered: a probe has no registry test home.

**Regression guards, green before any change** (`cmd/sqlgen/gen/nested_pinned_test.go`):
- `TestNestedEligibility_SingleColumnPinnedFKIsCreateOnly` (`/child primary-key member`,
  `/tenant column`). Its first run failed on the fixture, not the code: detection gives only the
  belongs-to side of `profiles.user_id`, so the has-one edge is now declared.
- `TestNestedEligibility_ChildInputDropsTenantFKColumn`, registered under
  `gen.nested.child-input-drops-key-members` (32.6 adds the template test).

**Fail first.**
- `TestValidatePostParse_RelationshipKeyLength` (4 subtests) returned nil, and
  `TestSchemaV1_RelationshipKeyForms/list` failed with `expected string, but got array`. Both runs
  were on the new `KeyColumns` type, before the validator and the schema change.
- The decode, manifest and MCP tests cannot compile at HEAD, since the types they name (`KeyColumns`,
  `FK.Columns`, `Reference.FKColumns`) do not exist there. Each was confirmed by mutation instead,
  and every mutation failed its test:
  - dedup on the first FK column (`TestDispatchFindReferencingDedupsByFullTuple`);
  - no constraint, and references read off the local column (`TestBuild_RelationshipKeyShape`,
    `TestBuild_M2MJunctionSchema`);
  - `SchemaVersion` back at `0.1.0` (`TestEmitJSON_SchemaVersion`);
  - reversed positional mapping, and a belongs-to key mapped onto the source PK
    (`TestBuildRelationshipContexts_DeclaredKeyListMapsOntoPrimaryKey`);
  - the composite guard disabled (`TestBuildRelationshipContexts_DeclaredCompositeKeyIsAnError`);
  - the length validator unregistered
    (`TestValidate_ReportsEachGenerationRuleWithTheOthers/relationship key list of the wrong length`).

**Files.**
- Config:
  - `config.go` (`KeyColumns`, string-or-list `UnmarshalYAML`; `[]` decodes non-nil, so it
    reaches the length rule);
  - `validate.go` (the required check, per-item fk column checks, `keyDedupComponent`,
    `validateRelationshipKeyLength`);
  - `schema/v1.json` (`$defs/relationshipKey`, `oneOf` string / string array).
- gen:
  - `RelationshipContext.Key` / `JunctionLocal` / `JunctionReference` (`context.go`). These are
    data only: the manifest reads them, and 32.4 moves gen's readers onto them;
  - `relationshipTarget.PK`, the resolved PK in `<Table>PK` order (`relationship_targets.go`);
  - `declaredKey`, `declaredJunctionKey` and the staging guard `checkDeclaredKeyColumns`
    (`junction_keys.go`);
  - `configRelationshipToContext` (`context_table.go`). `applyConfigDeclaredFKs` stamps
    `FKReference` only for a one-column key.
- Manifest: `FK{table, schema, columns, references, constraint}` and `JunctionKey` in both type
  sets (`cmd/sqlgen/manifest/types.go`, `manifest/types.go`), and `SchemaVersion` `0.2.0` in the
  first, the only one that has the constant;
  `keyOf` in `builder.go`; `schema/v1.json`; `emit_markdown.go` (a one-column cell is unchanged).
- MCP: `Reference.FKColumns`, a full-tuple `dedupKey`, and `slices.Compare` in the sort
  (`tools_graph.go`).
- Docs: PRD Appendix B (two rows, anchor rehashed), design §10.3, the registry (8 files).
- Tests: `config/relationship_key_test.go`, `gen/declared_key_test.go`,
  `gen/nested_pinned_test.go`, `manifest/relationship_key_test.go`, `TestLoad_RelationshipKeyLists`,
  new MCP subtests, and the fixture migration (`FK: "x"` → `config.KeyColumns{"x"}`, manifest
  `Column` → `Columns`).

**Behavior.**
- A declared key maps by position onto the referenced table's resolved PK. That PK is the
  `primary_key` override in its declared order, else the PK columns in column order, as
  `buildTableColumns` gives it. An O2M fk and a has-one fk reference the source; a belongs-to fk
  references the target.
- A declared key of more than one column is a generation error naming the relationship and the
  key, "relationships through a composite key are not generated yet". The message carries no
  sub-item ID (`refs-check`). It is pinned by an unclaimed staging-guard test, as 32.2's is.
- The length error names the relationship, the key as written, both counts and the PK's columns.
  An O2O fk references this table when every column is on the related table (has-one), else the
  related table. An unset junction key is inferred and not checked.
- Manifest `references` is the referenced columns: the FK's for a detected edge, the PK for a
  declared one. `constraint` comes from `RelationshipContext.Constraints`. A context with no pairs
  (hand-built) falls back to its one FK column and `[]`.

**Goldens.** `make update-golden-e2e` changed 38 manifest JSON files (only `fk` / `junction`
shape, `constraint` and `schema_version`, checked by filtering the diff) and 16 `_index.md`
files (the schema version line). No Go golden moved. `make update-golden` left gen's goldens
unchanged. The run also added testcontainers `postgres` lines to the `mysql` and `sqlite`
`go.sum`; that is unrelated drift, and it was reverted. Unit goldens under
`cmd/sqlgen/manifest/testdata/golden` and the MCP fixture `cmd/sqlgen/mcp/testdata/manifest_gen.json`
were updated to the new shape.

**Noted for later.**
- `TestBuild_E3CompositePKParentKeepsManifestEntries` declares a string `fk` into the
  composite-PK `user_categories`. Post-parse validation now rejects that config, which the test
  never runs, so its manifest `references` is the truncated `[user_id]`. Its fixture becomes a
  two-column key when 32.5 lifts the declared-edge guard.

**Auto-review (sqlgen-reviewer).** It confirmed fail first by unregistering the length rule. Three
high-confidence findings, all fixed, each with a test that a mutation of the fix fails:
- H1: an o2o list split across both tables passed, since each column was checked alone.
  `keySplitAcross` now rejects it (`TestValidatePostParse_O2OKeyOnOneSide`, under
  `config.relationship-fk-column-misplaced`).
- H2: the has-one branch was unpinned in gen and in the validator. Added
  `…DeclaredKeyListMapsOntoPrimaryKey/has-one references this table's key` and
  `…RelationshipKeyLength/has-one fk references this table`, plus a passing has-one list.
- H3: `fk: []` passed the required rule and, into a view target, every other rule. The required
  check is now `len(r.FK) == 0` (`TestValidateRelationships_EmptyKeyIsRequired`).

Doc and test fixes applied inline:
- the `KeyColumns` doc;
- "fk list too short" duplicated "string into a composite key", so it is now 2 columns against
  a 3-column key.

`validateRelationshipFKColumns`'s per-entry body moved to `checkRelationshipFKColumns` to stay
under cyclop. `docs/conformance/REPORT.md` was regenerated on the dirty tree.

**Ruling (2026-11-05): a key list maps onto the `PRIMARY KEY (...)` clause order**, or onto the
`primary_key.columns` order when overridden, never onto column order. This follows the reviewer's
question. A probe showed that the parser keeps the clause order on all three dialects but
`PKColumns` / `<Table>PK` use column order, which contradicts PRD §27.5. Only `introspect_sqlite`'s
`project_members` has the two orders differ.
- `gen.resolvedPrimaryKey` reads the declared PRIMARY KEY constraint when its columns are the
  resolved set. Config gets the order through the new `SchemaTable.PrimaryKey`, which
  `cli.toSchemaTables` fills.
- PRD §4.8 and §13.4 state the order; their anchors and §4's were rehashed.
- Fail first: `…DeclaredKeyListMapsOntoPrimaryKey/a composite key follows the PRIMARY KEY clause
  order` got `[workspace_id id]`, and `TestValidatePostParse_RelationshipKeyLengthNamesClauseOrder`
  named `(workspace_id, id)`.
- The `<Table>PK` order itself followed in FIX-362, so the two orders now agree.

**Checks.** `make check` green (`refs-check`, `req-check` included). `make check-examples` green,
run twice: after the main change and after the review fixes.

**Verify (2026-11-05).** The independent reviewer confirmed fail first by truncating `dedupKey`.
It found three test gaps, all closed inline, each failing under the mutation that showed it:
- `junction_reference_fk`'s length check was untested: unregistering it passed. Added
  `…RelationshipKeyLength/junction_reference_fk list too long`.
- `declaredKeyDDL` keyed every table on `id`, so swapping the junction keys' referenced tables, or
  pointing an o2m key at the target's PK, passed gen's tests. Its PKs are now `uid`, `post_id` and
  `tag_id`, and both mutations fail.
- `gen.nested.child-input-drops-key-members` (32.6's row) claimed the context-level guard test.
  Its Tests cell is empty again. Design §10.2 lists the guard, and `cli/gen.md` lists it as
  unclaimed until 32.6.

**Rulings after verify (2026-11-05).**
- **A declared key with nothing to reference is a loud error**, not a manifest `references: []`.
  A probe showed that a belongs-to or m2m edge into a view generated with empty referenced pairs,
  and that the loader fell back to the parent's PK name as the view's key. A view's `@pk` is a
  key, though. So:
  - `relationshipTarget.PK` for a view is now its `@pk` columns, in column order, since the
    parser keeps no `@pk` order;
  - post-parse validation length-checks a key into a view against that `@pk`, and rejects a
    belongs-to `fk` or an m2m `junction_reference_fk`, written or inferred, into a view without
    `@pk`;
  - the o2o side check now reads a view's columns, so a has-one fk on a view references this
    table's key.

  The new rule is `config.relationship-key-into-view-without-pk` (PRD §4.13 row; §13.4 sentence)
  and the new mapping is `gen.manual.fk-list-into-view`. No example has a belongs-to or m2m
  into a view. Fail first was by mutation: ignoring views, and ignoring `@pk`.
- **The manifest `v1.json` requires `columns` and `references`** on `fk` and `junctionKey`, so a
  0.1.0 `{"column": …}` key no longer validates (PRD §30.4.2;
  `TestSchemaV1_RelationshipKeyRequiresColumnsAndReferences`, all five subtests failing without
  the `required` lists). The `manifest validate` fixture
  `cmd/sqlgen/cli/testdata/manifest/valid_single.json` still had a 0.1.0 `fk`, which the stricter
  schema rejected; it is now 0.2.0.

## 32.4 Gen contexts — key pairs, pinned and free members, FK membership

**PRD Reference:** Sections 8.2, 8.6, 9.9.4, 13.2, 29.10

**Status:** Complete

### Tasks

- [x] Replace the `PKColumns[0]` relationship reads with key pairs: `context_table.go` (incl. the
  `TargetPKColumn` wiring at ~167 and ~205), `context_relfilter.go` (~384), `context_nested.go`.
  The parent side of every edge is the FK's referenced columns, not the parent's PK. The context
  types (`context.go:385-394`, `:512-514`) carry key-pair lists. `PKColumns[0]` reads that are
  about the table's own key stay.
- [x] Pinned and free members (design §4.3, R1), computed in `gen` because pinning needs the
  tenancy config. A member is pinned when it is:
  - the tenant column;
  - a child-PK member;
  - a member of another FK constraint on the same table.

  An O2O / O2M edge with no free member keeps `create` and loses `connect`, `disconnect`,
  `clear` and reparent, silently, as a NOT NULL edge does (design §7.1). M2M has no pinned
  members.
- [x] E1 over every member, the tenant column included: each is a field of the target's create
  input (design §7.1). A generated
  member makes the edge read-only.
- [x] `FKNullable` from the free members; disconnect eligibility under SIMPLE vs FULL
  (`Constraint.Match`); under FULL with a pinned member, `connect` only with `allow_reparent`
  (design §7.1).
- [x] The nested child input (`<Parent><Edge>CreateInput`) drops every member of the traversed
  key, the tenant column included (PRD §9.9.5, design §7.1).
- [x] Every member must be in the generated model on both sides: extend `relationshipTargets`'
  FIX-358 exclusion index from one column to every member, on the child and the parent.
- [x] FK membership from the constraints, not `col.FKReference`:
  - `isIncrementEligible` and `incrementBlockedBySQLType` (`context_table.go:1967,1988`);
  - `autoDetectPKStrategy` (`context_table.go:1164`): a single-column PK in any FK constraint is
    `caller`.
- [x] `applyConfigDeclaredFKs` (`context_table.go:2925`): no longer marks a declared reference
  `Synthetic` when a composite FK already enforces it (design §1.4); `autoDetectPKStrategy` is
  its only reader.
- [x] Until 32.5, the staging guard keeps every edge with more than one pair out of the template
  data.

### Acceptance Criteria

- For every N = 1 edge to a single-column PK, the template data is what it is today (Go goldens
  unchanged).
- A single-column FK to a non-PK UNIQUE column, and the PK-override form, now wire through the
  referenced column (design §4.9). Their generated code compiles; both measured schemas are
  regression tests.
- `workspace_id` on `tasks` (a composite-FK member) gets no increment operator; a UUID PK that is
  a composite-FK member is `caller`.

### Tests Required

- [x] Pinned / free classification, table-driven: tenant column, PK member, shared member, an
  all-pinned key, a free member.
- [x] E1: a generated member → read-only, with the reason.
- [x] Disconnect eligibility: SIMPLE with nullable free members; FULL with a pinned member →
  ineligible; FULL with all members free and nullable → eligible.
- [x] Increment and PK-strategy treatment of composite-FK members (`bigint` and UUID keys).
- [x] The two §4.9 non-PK-UNIQUE schemas compile and load (fail first: the `cannot use
  &comparator.NullableNumber[int32]…` / `cannot use &comparator.NullableID…` errors).
- [x] An excluded member on the parent side drops the edge with a warning.
- [x] `TestIncrementColumns_ExcludeForeignKeys` and `TestPKStrategyDetection` gain a composite-FK
  member case, and `gen.increment.fk-excluded` / `gen.pk-strategy.fk-pk-caller` list them (both
  rows claim the composite case today with single-column tests only, design §9.5).
- [x] Every design §10.5 row for 32.4 passes, and its registry row lists the test.
- [x] `TestE2EGoldenFiles` green with no golden change.

### Completion Record

Completed 2026-11-05 on `design/tenant-composite-fk-relationships`, uncommitted.

**Regression guards, green before any change.** The design §10.2 rows on this sub-item's paths
passed at `1f765d2a`: `TestNestedEligibility_SingleColumnPinnedFKIsCreateOnly`,
`…_ChildInputDropsTenantFKColumn`, `…_VerbSetPerShape`, `…_TargetShapeFailuresAreNotWriteEligible`,
`…_CompositePKParentKeepsItsLoaders`, `TestBuildRelationshipContexts_DeclaredReplacesInferred`, the
one-column O2M / M2M / O2O loader and filter tests, `TestGenerate_O2MIntoCompositePKTable_LoaderFiltersOnFK`,
`TestRelationshipTargets_ExcludedKeyMemberDropsInferredEdge`, `TestIncrementColumns_ExcludeForeignKeys`
and `TestPKStrategyDetection`.

**Fail first.** Each new test ran against `1f765d2a`, with only the test seam and the two
`NestedEdgeContext` member fields added (data, no logic):
- `TestGenerate_EdgeJoinsReferencedColumns`: every assertion missed. At HEAD the loader collected
  `u.ID` into a `NullableNumber[int32]` filter on `invites.email`, the filter correlated on `id`, the
  O2O join was `u.id = b.owner_email`, and the M2M mapped targets on `t.ID`. The two §4.9 schemas
  also fail `go build` at HEAD with the measured `cannot use &comparator.NullableNumber[int32]…` /
  `cannot use &comparator.NullableID…` errors (scratch probe).
- `TestIncrementColumns_ExcludeForeignKeys/composite FK member` got
  `["workspace_id" "cycle_id" "points"]`; the three `TestPKStrategyDetection/… in a composite FK →
  caller` cases got `app`, `db`, `db`.
- `TestNestedEligibility_PinnedAndFreeMembers`: all five subtests failed (no member split; the
  tenant and shared cases had no rewrite verb). Two keys list their nullable free member first,
  since HEAD read the first member only and a NOT NULL first member made two cases pass vacuously.
- `TestNestedEligibility_MatchFull`: three of five failed. `/full with every member free keeps
  disconnect` and `/mysql and sqlite read full as simple` pass at HEAD by nature: HEAD reads every
  key as SIMPLE. They guard that the FULL rule narrows nothing else.
- Confirmed by mutation instead, since each needs the new code to reach the case:
  - `…TargetShapeFailuresAreNotWriteEligible/generated member of a composite key` (E1 on the first
    member only);
  - the child-input check in `…PinnedAndFreeMembers` (drop the first member only);
  - `TestRelationshipTargets_DeclaredExcludedLinkColumnErrors/composite key member` (check the
    first declared column only);
  - `TestConfigDeclaredFKOverACompositeKeyIsNotSynthetic` (always `Synthetic`);
  - `TestNestedEligibility_M2MIntoAUniqueKeyIsNotWriteEligible` (no target-key check: the edge
    was write-eligible and would write the target's PK into `tag_legacy_id`).

**Files.**
- `gen/key_members.go` (new): FK membership (`inForeignKey`, `foreignKeyInto`), the edge's own
  constraint and shared members (`stampKeyConstraintFacts`), `parentKeyColumn`, the member split
  (`edgeKeyMembers`) and E8's key rules (`keyRewriteVerbs`).
- `gen/context.go`: `ColumnContext.InForeignKey`; `RelationshipContext.SharedMembers`, `Match`,
  `ParentKey`; `NestedEdgeContext.PinnedMembers`, `FreeMembers`; `TableContext.LoadKeyColumns`.
  Docs of `FKGoType`, `TargetPKFieldName` / `TargetPKColumn` and the filter context say "referenced".
- `gen/context_table.go`: `wireRelFKMetadata` sets `ParentKey`, `FKGoType` and the M2M target key
  from the referenced columns (`targetKeyColumn`); `loadKeyColumns`; the O2O join's ON uses
  `Key[0].Referenced`; both edge builders stamp the key facts; increments and `autoDetectPKStrategy`
  read `InForeignKey`; `markSyntheticFK` leaves a reference a composite FK enforces non-synthetic.
- `gen/context_relfilter.go`: correlation and the M2M target join on the referenced columns.
- `gen/context_nested.go`: the parent key is the edge's `ParentKey` (`parentPK` → `parentKey`);
  E1 over every member, the member split and the child-input drop (`splitNestedKeyMembers`,
  `resolveNestedChildInput`); `settleNestedKeyRewrite`; an M2M whose junction references a target
  UNIQUE column is refused (E5's binding clause).
- `gen/relationship_targets.go`, `gen/junction_keys.go`: `excludedDeclaredMember` checks every
  member; the test seam `generateCompositeKeyEdges` lifts both staging guards.
- `gen/templates/table/get.go.tmpl`: the O2M and M2M loaders read `.ParentKey`; GetMany unions
  `.LoadKeyColumns`.
- Tests: `cli/relationship_detection_test.go` (`TestGenerate_EdgeJoinsReferencedColumns`, three
  subtests, `go build` outside `-short`; the staging-guard fixture's keys became `uuid`, see below),
  `gen/nested_key_members_test.go` (new), subtests in `increment_fk_test.go`, `context_test.go`,
  `nested_test.go`, `relationship_targets_test.go`; `export_test.go`
  (`GenerateCompositeKeyEdgesForTest`); `withParentKeys` for the hand-built loader fixtures
  (`get_test.go`, `tenancy_template_test.go`, TESTING.md §11).
- Docs: the registry (`cli/gen.md`, 8 rows), design §10.1 statuses and test names.

**Behavior and interpretation.**
- `FKNullable` keeps its meaning, the FK column's nullability, because the loaders type their filter
  comparator by it. "Nullable from the free members" (PRD §9.9.2, E8) is `keyRewriteVerbs`, which
  replaces the nested `!rel.FKNullable` gate. For a single-column key the two agree, except that a
  nullable pinned member (a tenant column, or a column two FKs share) now loses the rewrite verbs,
  as the PRD says.
- Staging. `compositeKeyEdge` still keeps every multi-pair edge out of generation; the seam is the
  only way in. Its tests must not run in parallel. 32.5 deletes the seam with the guard. A key of
  several columns stops at its verb set in the nested resolver: the single-column rendering checks
  (`= parent` term, unlink and adoption bindings) are skipped, since 32.6 replaces them with tuple
  forms.
- An M2M edge whose junction references a target UNIQUE column, not its PK, is not write-eligible
  (E5: the link step writes the target's PK). It is reported when listed. Its reads now work
  (`TargetPKColumn` is the referenced column). No example has the shape. 32.6 lifts it by reading the
  target's referenced columns on connect.
- A belongs-to O2O's `FKGoType` keeps the parent's PK type, as before; nothing reads it for that
  shape.

**Goldens.** `make update-golden` and `make update-golden-e2e` changed no golden. The e2e run added
testcontainers `postgres` lines to the `mysql` and `sqlite` `go.sum` again, the drift 32.3 noted;
reverted. `TestGenerate_CompositeKeyEdgeIsOmitted` compares output with and without composite FKs,
and composite-FK members now lose their increment operators (§4.6), so its fixture's keys are
`uuid` (no increment) and it still pins edge omission only.

**Auto-review (sqlgen-reviewer).** It confirmed fail first by reverting the filter correlation.
One high-confidence finding, fixed:
- With nested mutations on, a §4.9 edge became nested-eligible (its FK now builds from
  `ParentKey`), but the adoption guard in `nested.go.tmpl` still compared the owner against
  `parent.<PK>`. Neither schema built: `(*cur) == parent.ID (mismatched types string and int32)`
  and `(*cur) == parent.Code (mismatched types int32 and string)`. `NestedEdgeContext.ParentKeyField`
  now spells the guard, and `TestGenerate_EdgeJoinsReferencedColumns` generates with nested
  mutations on and asserts it. Reverting the template line reproduces both build errors.

Doc fixes applied inline: the `funcFKToStringByGoType` doc, the `keyMembersDDL` bullet order, and
the registry and design wording for `gen.relationships.joins-referenced-columns` (nested writers
included; the build runs outside `-short`).

**Ruling (2026-11-05): the generated-package build runs under `make check-examples`.** The §4.9
compile proof is skipped under `-short` unless `SQLGEN_BUILD_GENERATED` is set. The new
`make build-generated` target runs the CLI unit tests with it set, and `check-examples` depends on
it (`guidelines/CI.md` target table). Restoring the old adoption guard fails the target with the
`mismatched types` build error.

**Checks.** `make check` green (`refs-check`, `req-check` included). `make check-examples` green,
rerun after the review fix; goldens regenerated again with no change.

## 32.5 Read templates — tuple-key loaders and relationship filters

**PRD Reference:** Sections 11.1, 13.2, 25.1, 29.10

**Status:** Complete

### Tasks

- [x] O2O inline JOIN (`get.go.tmpl`): `ON` ANDs every pair. `JoinClause.On` stays opaque.
- [x] O2M loader: collect the parent's referenced-column tuples; filter the child on its FK
  members through `Get<T>Input.conditions` (append, never replace; `Filter.PKs` is not reused);
  group by `[N]string` of `fkLoadKey`s. N = 1 keeps today's `IN` / pgx `= ANY($1)` shape.
- [x] M2M loader (`relationships.go.tmpl`): the junction step filters on a tuple per parent
  chunk; the target fetch matches the target key against
  `(SELECT j.ref … FROM junction j WHERE (j.local …) IN (chunk))`, FIX-359's subquery with tuple
  IN; maps key on `[N]string`.
- [x] Chunk every step with `loadChunkSize N` (funcmap, FIX-359), passing the key's column count.
  `O2MLoadUnchunked` is true for any pgx table; the template branches on `KeyedLoad` first, so only a plain one-column key stays unchunked.
- [x] Force every referenced column on in the parent's `FieldOptions` and every FK member on in
  the child's.
- [x] Skip NULL members before the query: a child with any NULL member and a parent key with a
  NULL member match nothing (R2).
- [x] Relationship filters (`EXISTS`): correlate on every pair through the 32.1 `Exists`; lift the
  composite-PK-parent skip (`len(tc.PKColumns) != 1` in `context_relfilter.go`, removed).
- [x] Conformance schema (design §12 Q1): on every dialect, add a table with a composite FK into
  `order_items(order_id, product_id)` (a composite-PK target) and one with a composite FK into a
  UNIQUE pair, with a nullable member and a nullable referenced UNIQUE column. Regenerate the
  suite's goldens; only the new tables change. The `conformance:` rows of design §10.1 run here
  and fail first against the staging guard.
- [x] Lift the staging guard for reads: a detected edge with more than one pair is generated, and
  a declared one no longer errors. Invert 32.2's staging-guard test. Until 32.6 such an edge is
  nested-ineligible with that reason.

### Acceptance Criteria

- The tenant pattern loads `Cycle.Tasks` through `(workspace_id, cycle_id) → (workspace_id, id)`,
  and the O2O join and the `EXISTS` filter use both pairs.
- A composite-PK parent gets relationship filters (read surfaces only, R3).
- Every step binds at most `floor(L / 2 / N)` parent keys, and at most 4000 on PostgreSQL for a key of several columns (design §12 Q6). PRD §25.1's count holds with `s = C`
  for a composite O2M on pgx.
- Go goldens of existing examples are unchanged.

### Tests Required

- [x] Gen pattern tests (`get_test.go`) for the N = 2 O2O join, O2M and M2M loaders, and the
  2-pair `EXISTS`.
- [x] Loaders: an N = 2 load past one chunk on every dialect, checking every child set (extend
  FIX-359's `relationship_bind_limit_test.go` pattern). Fails first against the staging guard.
- [x] NULL members: a child with a NULL member and a parent UNIQUE key with a NULL member load
  nothing and raise no error.
- [x] A consumer filter with a soft-delete comparator is ANDed with the tuple filter, not
  replaced (the FIX-359 regression shape at N = 2).
- [x] The conformance rows (composite O2O / O2M / M2M loads, NULL members, the relationship
  filter, a narrowed projection) on every dialect and driver.
- [x] Every design §10.5 row for 32.5 passes, and its registry row lists the test.
- [x] `TestE2EGoldenFiles` green with no golden change.

### Completion Record

Completed 2026-11-05 on `design/tenant-composite-fk-relationships`. Verified and accepted 2026-11-06.

**Regression guards, green before any change** (`cb0666f2`): the design §10.2 rows on this sub-item's
paths in `gen` and `sql`, including `TestGetTemplate_o2mRelationshipLoading`,
`TestGetTemplate_m2mRelationshipLoading`, the one-pair O2O and filter tests,
`TestNestedEligibility_*` and `TestRelationshipCallOptions`. `TestE2EGoldenFiles` is skipped under
`-short` and lives in `cmd/sqlgen`; `make update-golden-e2e` is the run that counts.

**Fail first.** Against the guard, before any non-test change:
`TestGetTemplate_o2mCompositeKeyLoading` (3 subtests), `TestGetTemplate_m2mCompositeKeyLoading`,
`TestGetTemplate_m2mIntoCompositePKTarget`, `TestRequiredColumns_compositeKeyMembers`,
`TestBuildO2OJoinDetails_CompositeKeyJoinsEveryPair/belongs-to`,
`TestFilterTemplate_CompositeKeyCorrelatesEveryPair` (2) and
`TestRelationshipFilters_MemberForAnyKey/composite-PK parent`, `/m2m into a composite-PK target`
all failed on the missing edge. The conformance suite, with the new schema generated by `cb0666f2` on
the SQLite module, failed to compile (`it.ItemNotes undefined`, `s.Parcels undefined`).
`TestRelationshipFilters_MemberForAnyKey/edge into a unique key` passed: 32.4 already made that N = 1
shape work, so it is a guard. Written after the code, so not seen failing first:
`TestTupleInSubquery`, the has-one subtests and `TestGenerate_CompositeKeyStructFields`, the
exclusion and declared-key tests, `TestNestedEligibility_CompositeKeyEdgeIsNotWriteEligibleYet`,
`TestNestedEligibility_NullableReferencedColumnIsNotWriteEligible`,
`TestGenerate_CompositeKeyEdgeIsGenerated`, `/fk to a nullable unique column`, and the review-added
conformance rows `Get/a has-one…`, `GetMany/a soft-delete comparator…`, `/a one-column key into a
nullable unique column…` and `/an M2M from a one-column key…`. `/verify` failed the has-one,
soft-delete and one-column M2M rows by mutation, and the chained-join subtest likewise.

**Files.**
- `sql/`: `TupleInSubquery`; `tupleInCondition.Subquery`. The M2M target fetch needs a tuple left of
  `IN (SELECT …)` that survives `PrefixConditions`, which 32.1 did not provide.
- `gen/context.go`: `KeyMemberContext`, `JunctionColumnContext`, `O2OOnPair`;
  `RelationshipContext.LoadKey`, `LoadReferenceKey`, `JunctionColumns`, `KeyedLoad()`,
  `CompositeKey()`; `O2OJoinDetail.ExtraOn`; `RelationshipFilterContext` correlation fields.
- `gen/context_table.go`: the guard is gone; `wireKeyedLoad`, `loadKeyMembers`,
  `wireJunctionColumns`, `o2oExtraOn`, `inferredFKOnTarget`; `loadKeyColumns` unions every referenced
  column.
- `gen/context_relfilter.go`: composite-PK and M2M skips lifted; `keyColumns`.
- `gen/funcmap.go`: `keyTuple`, `keyGuard`, `keyValues`, `keyedLoad`; the PostgreSQL chunk cap.
- `gen/relationship_targets.go`, `gen/junction_keys.go`, `gen/context_nested.go`: the read guard and
  `checkDeclaredKeyColumns` removed; `compositeKeyWriteGuard` keeps these edges out of nested writes
  until 32.6, behind the test seam `writeCompositeKeyEdges`.
- Templates: `get.go.tmpl` (`table/load-o2m-keyed`, `table/load-m2m-keyed`), `relationships.go.tmpl`,
  `shared/_filter.tmpl`.
- Conformance: eight tables on every dialect, `suite/composite_key.go`, `Capabilities.CompositeLoadChunk`.
- Tests: `gen/composite_key_load_test.go` (new), and the staging-guard tests inverted or removed.
- Docs: PRD §9.9.4 E1, §25.1, `guidelines/SQL.md`, the registry, design §10.1, §10.4, §12.

**Behavior and interpretation.**
- **PostgreSQL tuple `IN` cap (ruling 2026-11-05, design §12 Q6).** The conformance load past one
  chunk failed on both PostgreSQL drivers with `stack depth limit exceeded`: R4's 16383 was measured at
  N = 1 only. `(a, b) IN (…)` at the default 2MB stack passed 7500 and failed at 8000 on PostgreSQL 16,
  and passed 7000 and failed at 7500 on 17; three columns matched. A key of several columns is capped
  at 4000 on PostgreSQL. A server with a lower `max_stack_depth` could still hit the limit. A one-column
  `(a) IN ((…), …)` passed at 32767 on PostgreSQL 16.
- **Bind-limit rows moved to the conformance suite (design §12 Q7, confirmed 2026-11-06).**
- **`KeyedLoad` covers one more shape than a composite key.** A one-column key referencing a nullable
  UNIQUE column did not compile (`*string` into `[]string`), and UNIQUE columns are nullable by
  default. It now takes the keyed loader, which skips the NULL parent (PRD §13.3). This shape takes
  `s = C` on pgx, which differs from a one-column key's `s = 1` only past one chunk; PRD §25.1 amended
  to state it (ruling 2026-11-06).
- **Nullable referenced key and nested writes.** Such an edge is not write-eligible; the writers
  cannot copy the pointer into the key. PRD §9.9.4 E1 amended to state it (ruling 2026-11-06).
- **`FKOnTarget` of a composite key.** A tenant column exists on both tables, so column presence
  cannot place the key: a detected edge takes its side from its type, and a declared key is on the
  target only when every member is.
- **O2O naming.** A key with no naming member keeps the base name, so `item_receipts`' single edge is
  the field `OrderItems` (`*OrderItem`), as §5.3 rule 5 step 4 gives.
- **GraphQL.** Composite edges reach the generated schema. A full gqlgen build is 32.7's.

**Auto-review (sqlgen-reviewer).** It confirmed fail first by restoring the guard. Closed from its
report: the soft-delete comparator case had no test (now `GetMany/a soft-delete comparator in a
consumer filter…`, with `deleted_at` on `item_notes`); composite has-one and the one-column keyed
shapes never ran (now conformance rows, with `crates` and `crate_items`); `/chained o2o` matched a
direct join (now scoped to the resolver). Its doc corrections are applied. The composite-PK batch
sites' exposure to the tuple `IN` limit went to the backlog.

**Verify (2026-11-06).** The independent reviewer found no runtime bug; its coverage gaps are closed
here. New `TestTenancyTemplate_keyedLoadPropagatesTenantOptions` fails if the keyed loaders stop passing
`SkipTenancy` and `Tenant`. `/an M2M from a one-column key…` now needs both key columns (it passed
with mapping on `product_id` only); the E1 test can no longer pass with the edge absent;
`load.key-condition-appended`, `query-count.nullable-referenced-pgx` and
`gen.relationships.composite-key-edge` now cite tests that show their wording.

**Tenant pattern (first acceptance criterion).** No live database run at 32.5: conformance has no
tenancy and no example has a composite edge. The reviewer's SQLite probe saw correct tenant, `SkipTenancy`
and explicit-tenant behavior on O2M, M2M and filters, and the unit test above pins the propagation.
The live row is `example: tenancy_postgres: TestCompositeTenantFK_LoadStaysInTenant`, due at 32.8.
The user accepted 32.5 on this basis (`/done`, 2026-11-06).

**Goldens.** `make update-golden` and `make update-golden-e2e` changed no example or gen golden; the
`go.sum` drift 32.3 noted reappeared and was reverted. Only the conformance models moved.

**Checks.** `make check` and `make check-examples` green; conformance passes on pgx, stdlib, MySQL and
SQLite.


## 32.6 Nested writes through composite FKs

**PRD Reference:** Section 9.9

**Status:** Complete

### Tasks

- [x] E1's nullable-referenced check reads every referenced member. It reads `nestedParentKey` (`rel.ParentKey`), the first pair only, and `compositeKeyWriteGuard` hides that until it is lifted: `shelves(region NOT NULL, bin NULL)` would otherwise pass.
- [x] `tenancy_postgres` schema (moved from 32.8, so the nested `example:` rows fail first):
  - a composite tenant FK (`(workspace_id, x_id) → (workspace_id, id)`);
  - a tenant junction (M2M);
  - a composite-PK target, read surfaces only;
  - a `MATCH FULL` composite FK whose members are all free and nullable.

  Regenerate its goldens; only the new tables change, plus `ToConditions` on the existing ones
  (the package now has relationship filters).
- [x] `nested.go.tmpl` and `context_nested.go`, for single-column-PK parents and targets (R3):
  - connect identifies the target by its PK and loads it through its own tenant-scoped client,
    reading its referenced columns;
  - pinned members must equal the target's referenced values, or `ErrAlreadyRelated` before any
    write (PRD §9.9.6, §9.9.8);
  - free members take the target's values;
  - `allow_reparent` never moves a pinned member.
- [x] Nested `create` sets every member from the parent's referenced columns, the tenant column
  included; a tenancy-set value that disagrees is `ErrAlreadyRelated` (design §7.1).
- [x] Disconnect nulls the free members, gated on 32.4's eligibility (SIMPLE / FULL).
- [x] `ErrAlreadyRelated` keys its owner map on the `[N]string` tuple.
- [x] M2M link and unlink insert and delete the junction row by the union of both key lists; E5
  and E11 over that union (disconnect needs a junction PK equal to it, design §7.1).
- [x] Lift the staging guard's nested-ineligible reason. E1 and E3 still exclude composite-PK
  parents and targets.

### Acceptance Criteria

- `updateCycleWithRelated(tasks: {connect: [...]})` with `allow_reparent` writes `cycle_id` only.
  A task in another workspace fails before any write: `ErrNotFound` under tenancy (the visibility
  read hides it), `ErrAlreadyRelated` under `SkipTenancy` (the pinned check).
- A tenant junction links and unlinks through all three of its columns.
- Disconnect under MATCH FULL with a pinned member is not generated, and is dropped silently
  (a schema fact, PRD §9.9.4).
- Go goldens of other examples are unchanged.

### Tests Required

- [x] Gen tests for connect (pinned verify, free write), create, disconnect, reparent and M2M
  link/unlink at N = 2.
- [x] Live-database tests (PostgreSQL): a cross-tenant connect fails before the write; a pinned
  mismatch is `ErrAlreadyRelated`, not a raw FK error; MATCH FULL all-NULL disconnect succeeds.
- [x] `ErrAlreadyRelated` on a tuple-keyed owner.
- [x] Every design §10.5 row for 32.6 passes, and its registry row lists the test.
- [x] `TestE2EGoldenFiles` green; only `tenancy_postgres` changes.

### Completion Record

Completed 2026-11-06 on `design/tenant-composite-fk-relationships` in `e48ece8b`. `/verify 32.6`
(2026-11-06) logged FIX-363 to FIX-373; all eleven landed by `/fix-loop` (`1c009361` to `c88b5c7e`), the three
blocking ones (FIX-364, FIX-368, FIX-369) included. Marked done 2026-11-10, after the final sweep
(`make check-examples`, `make test-integration`) ran green at `c88b5c7e`.

**Files.**
- `gen/context.go`: `NestedKeyMemberContext`, `NestedScopeTerm`, `NestedOwnerMember`,
  `NestedFieldAssign`, `FreeKeyMembers()`. `KeyMembers`, `ParentScope`, `OwnerMembers` and
  `JunctionLinkFields` replace the one-column `FK*`, `ParentFilterExpr` and `Junction*Field` scalars.
- `gen/context_nested.go`: every resolver walks the key; `resolveNestedKeyAssign`,
  `nestedParentScope`, `resolveNestedOwnerTuple`, `junctionLinks`. `settleNestedKeyRewrite` is gone.
- `gen/key_members.go`: `keyParents`. `gen/relationship_targets.go`, `gen/export_test.go`: the write
  guard and its seam are gone.
- `gen/templates/table/nested.go.tmpl`: ranges over the members, a tuple-keyed owner map for a
  composite key, `targetRows` for a junction column read off the target.
- Example: `tenancy_postgres` gains `cycles`, `sprints`, `projects`, `tasks`, `labels`, `task_labels`,
  `badges`, `task_badges`,
  `milestones`, `checkpoints`, `templates`, `template_steps`; `tests/nested_composite_test.go` (new).
- Tests: `gen/nested_composite_test.go` (new), `TestGenerate_CompositeKeyNestedWritersCompile`.
- Docs: PRD §9.9.3, §9.9.6, §9.9.8, the registry, design §7.1, §10.1, §10.2, §10.3.

**Fail first.** Regression guards ran green at HEAD: `TestNestedEligibility_ChildInputDropsTenantFKColumn`,
`TestNestedTemplate_ChildCreateInputDropsEveryKeyMember/single-column tenant key` and the existing
nested suite. New behavior failed at HEAD: the schema refused to generate (`the edge joins through a
composite key, which nested writes do not reach yet`), so every example row failed for that reason;
behind the seam, `shelves` passed E1 (`nested edges = ["Crates"], want none`) and the executor tests
found no composite statements. `/surrogate pk has no disconnect` passed at HEAD, since that junction's
edge was already refused; it asserts the verb set after the lift.

**Behavior and interpretation.**
- **`nested.composite-create-tenant-mismatch` reworded.** The row said "under `SkipTenancy`", which
  cannot occur: every key member is elided from the child input, so under `SkipTenancy` the parent
  alone sets the column. PRD §9.9.8 says "tenancy-set", and that case is reachable: a shared parent's
  column, which tenancy does not guard, against the resolver. The executor maps the target's
  `tenancy.ErrMismatch` to `ErrAlreadyRelated`. The test is
  `TestUpdateWithRelated_TenancySetTenantMismatchIsAlreadyRelated`.
- **The mapping covers a single-column tenant FK too** (ruled below). No other example's golden
  moved.
- **Composite has-one is unreachable in this phase.** A has-one side is declared, a declared key list
  maps onto the parent's PK (R7), so a two-column key needs a composite-PK parent, which E3 excludes.
  The matrix cell is now a stated `n/a`. The template handles the shape, and it is untested until the
  next phase lifts E3.
- **Owner tuple.** `[N]string` as the design says, which also compares binary and network keys. NULL
  is held apart, since a NULL member and an empty string stringify alike. A free member's NULL makes
  a row unparented only when every free member is NULL, matching the adoption's `IS NULL` guard; a
  partly NULL key is `ErrAlreadyRelated`, or adopted under `allow_reparent`.
- **M2M.** A column in both keys takes the parent's value (PRD §9.9.6, added here). `connect` and `create`
  compare the target's own value with the parent's before the link step and refuse a difference with
  `ErrAlreadyRelated`, attributed to the verb, `connect` naming the id and `create` with none (FIX-368).
  Under `SkipTenancy` a target in another workspace is refused this way, not by the junction's foreign key.
- **32.4's note on an M2M into a UNIQUE column is lifted.** `create` and `connect` read the column off
  the target. `disconnect` reads no target, so it is lost and reported for a listed edge.
  `TestNestedEligibility_M2MIntoAUniqueKeyIsNotWriteEligible` became
  `TestNestedEligibility_JunctionReferencingTargetColumn`.
- **Surrogate junction.** It loses `disconnect` at generation, and `clear` needs just E11 (PRD §9.9.3).
  `create` and `connect` are emitted but every link fails at runtime: see the `/verify` findings below (resolved by FIX-364).

**Goldens.** `make update-golden` and `make update-golden-e2e` moved `tenancy_postgres` alone. Beyond
the new tables, one existing-table change: `ToConditions` on `Article`, `LineItem` and the two views
gains `opts ...FilterOption`, because the package now has relationship filters. The `go.sum` drift
reappeared and was reverted.

**Auto-review (sqlgen-reviewer).** It confirmed fail first by removing the `ErrMismatch` mapping. One
high-confidence finding, fixed: the parent write forced only the primary key, so under a narrowed
`FieldOptions` the executors read a zero `parent.WorkspaceID`. Measured on `tenancy_postgres`: `connect`
and `create` gave a false `ErrAlreadyRelated`, `disconnect` and `clear` returned nil and wrote nothing,
and the M2M link gave a raw `ErrMismatch`. `NestedContext.ParentKeyFields` now forces every column an
edge's key references, which also covers a one-column key into a UNIQUE column.
`TestWithRelated_NarrowedSelectionStillWritesTheEdge` failed first with the false `ErrAlreadyRelated`.
Coverage closed from its report: `TestUpdateWithRelated_LinkStepReadsTheTargetsReferencedColumn` runs
`targetRows` live (`badges`, `task_badges`), and `TestNestedTemplate_HasOneTenantKeyMapsTheMismatch`
renders the has-one branch. A free member with no null guard now drops `connect`, as a one-column key
does. Its doc corrections are applied. Rendered by no fixture: the nullable pinned guard.

**Checks.** `make check` and `make check-examples` green, conformance included on all four drivers;
`TestE2EGoldenFiles`, `TestE2EDatabaseTwin` and `make build-generated` green. `make test-integration`
was not run; it is due at 32.8.

**Rulings (2026-11-06, each took the recommended option).**
- A single-column tenant FK off a shared parent is `ErrAlreadyRelated` (`CONFLICT`), not
  `tenancy.ErrMismatch` (`FORBIDDEN`), as PRD §9.9.8 and §9.9.10 say. Pinned by
  `TestUpdateWithRelated_SingleColumnTenantKeyMismatchIsAlreadyRelated`, which passed on arrival.
- A partly NULL key on `connect` stays `ErrAlreadyRelated`, matching the adoption's `IS NULL` guard.
  PRD §9.9.3 now states it. Pinned by `TestUpdateWithRelated_PartlyNullKeyIsAlreadyRelated`, which
  passed on arrival.
- The mapping extends to the M2M link step (PRD §9.9.6, §9.9.8). Failed first with the raw
  `tenant on mutation input does not match` error; `TestUpdateWithRelated_LinkTenantMismatchIsAlreadyRelated`.

**Rulings from `/verify 32.6` (2026-11-06, each took the recommended option; FIX-364, FIX-368 and FIX-369 have landed).**
- A surrogate-PK junction's link targets the UNIQUE equal to the key union, and without one the edge
  is refused under E13 (FIX-364).
- An M2M `connect` under `SkipTenancy` runs a pinned check on a column both keys share, and a mismatch
  is `ErrAlreadyRelated` (FIX-368).
- The PRD §9.9.6 junction paragraph stands, amended by that ruling and with the SQLite FK claim
  removed (FIX-368).
- A shared parent's tenant mismatch is loud on `disconnect` and `clear` too (FIX-369).

`tenancy_postgres` gains `template_labels`, `template_notes`, `stamps`, `template_stamps`,
`workspaces`, `workspace_notes`, `zones` and `sites` for these. FIX-367 (`4ecd98c7`) added the `plots` fixture, so the nullable pinned guard (`null[i] || …`) now renders and runs
(`TestUpdateWithRelated_NullablePinnedMember`).

## 32.7 GraphQL — relationship fields and filters for composite edges

**PRD Reference:** Sections 26.4, 26.5

**Status:** Not Started

### Tasks

- [ ] The `graphql` example (the only tenanted GraphQL example, design §12 Q4) gains a composite
  tenant FK; regenerate its goldens, and only the new table's files change.
- [ ] Relationship fields and filter inputs for edges with more than one pair: field shapes are
  unchanged, and resolvers go through the 32.5 loaders.
- [ ] Nested connect on a composite-FK edge to a single-column-PK target takes the target ID, as
  today.
- [ ] `field_options` forcing: the parent's referenced columns and the child's FK members are
  selected even when the query does not ask for them.

### Acceptance Criteria

- A GraphQL query over `cycle { tasks { … } }` through a composite tenant FK returns the same rows
  as the Go client, under tenancy.
- Relationship filter inputs exist for composite edges and composite-PK parents.
- Go and GraphQL goldens of existing examples are unchanged.

### Tests Required

- [ ] GraphQL resolver tests for a composite O2M, O2O and M2M edge, and a relationship filter.
- [ ] A query that selects no key column still loads the edge (field forcing).
- [ ] `graphql` example: a query over the composite tenant FK returns the same rows as the Go
  client, under tenancy, and its relationship filter matches on every pair.
- [ ] Every design §10.5 row for 32.7 passes, and its registry row lists the test.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

## 32.8 Examples — composite tenant FKs end to end

**PRD Reference:** Sections 8.1.7, 29.10

**Status:** Not Started

### Tasks

- [ ] `tenancy_postgres` (schema landed in 32.6, with the nested and cross-tenant connect
  tests): load and filter tests over the composite tenant FK, the tenant junction and the
  composite-PK target, each staying in the tenant.
- [ ] `mysql` and `sqlite`: one composite FK each, with a load and a filter test.
- [ ] Regenerate the goldens. Only the new tables' files and manifest entries change.
- [ ] `TestE2EDatabaseTwin` stays green for the changed examples (detection parity under
  `database`).
- [ ] `sqlgen-example` probe against HEAD (design §9.2; `reference_example_probe_harness`):
  `validate`, `generate`, build, vet and the full suite pass with no manual relationships, and
  `Cycle.Tasks` / `Team.Projects` are detected. Its own change lands in that repo.

### Acceptance Criteria

- Every shape design §1.3 lists is generated, built and exercised against a live database on its
  dialect.
- Golden churn is limited to the new schema objects.
- `make check`, `make check-examples` and `make test-integration` are green.

### Tests Required

- [ ] The new runtime tests in each example (load, filter, nested where eligible).
- [ ] `TestE2EGoldenFiles` and `TestE2EDatabaseTwin` green.
- [ ] The `sqlgen-example` probe result recorded in the Completion Record.
- [ ] Every design §10.5 row for 32.8 passes, and its registry row lists the test.

### Completion Record

_Filled in when complete: files changed, commit references, notes._
