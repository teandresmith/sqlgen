# Phase 30: Database Introspection — CLI Wiring & Introspector Parity

Status: In Progress
PRD Sections: 4.1, 4.2, 4.3, 4.4, 5.2, 6.1, 6.3, 6.4, 7, 8.1.7, 16.2, 30.7

> **Design.** `docs/design/INTROSPECTION.md` (DRAFT 2026-10-05) is the source for every sub-item
> below. Its §4.0 records the 2026-10-05 probe (PG 16, MySQL 8.0, modernc SQLite: 20 of 22
> suspected defects confirmed, 12 more found). Its §10 records the 14 rulings (Q1–Q14). Its §6
> lists the tests per dialect, and §6.5 is the parity matrix. Defect IDs (D = PostgreSQL,
> M = MySQL, S = SQLite, X = all three) refer to the doc's §4. The probe harness and its output
> are in the gitignored `.claude/prototypes/introspection-probe/`.
>
> **Ground rules for every sub-item:**
> - Each defect fix starts with a test that fails against HEAD; record the failure in the
>   Completion Record.
> - A defect the doc doesn't cover whose fix is design-shaped stops work, and the question goes
>   to the user (never `t.Skip`).
> - Adjacent findings go to `docs/tracker/backlog.md` per `/fix` → Findings triage. The queue
>   freeze is on.
>
> - PRD §6.1 says `database` and `files` describe a schema identically. A parity exception
>   that stays user-visible (a type, default, name or CHECK text that differs by source) must
>   be written into §6.1 by the sub-item that accepts it.

## 30.0 PRD sync — introspection, parity rules and the canonical schema model

**PRD Reference:** Sections 4.2, 5.2, 6.1, 6.3, 7, 8.1.7, 30.7

**Status:** Complete

### Tasks

- [x] §4.2 `paths` row: state that `paths` is ignored under `source: database`, including its
  `["."]` default. State that `input.introspect.*` has no effect under `source: files`.
- [x] §5.2 PostgreSQL bullet list:
  - add generated columns, `GENERATED ALWAYS AS (expr) STORED` (Q1)
  - add that `CREATE TABLE … PARTITION OF` is skipped with a warning and the partitioned
    parent is the entity (Q14)
  - add that type modifiers are written without a space, as in `numeric(12,2)` (Q12)
- [x] §5.2 MySQL bullet list: add generated columns, `GENERATED ALWAYS AS (expr)
  [VIRTUAL|STORED]` and the shorthand `AS (expr)` (Q1).
- [x] §5.2 SQLite bullet list: add that `CREATE VIRTUAL TABLE` is skipped with a warning, like
  `CREATE VIEW` (Q9). Add that `INTEGER PRIMARY KEY` is auto-increment only when it is a rowid
  alias, which excludes `WITHOUT ROWID` tables and `INTEGER PRIMARY KEY DESC` (S9 / S11).
- [x] §5.2 (all dialects): state the canonical constraint model (Q11).
  - Every PRIMARY KEY, UNIQUE, FOREIGN KEY and CHECK, inline or table-level, is a table
    constraint and also sets the column flags.
  - An unnamed constraint gets the name its database would assign:
    - PG: `<t>_pkey`, `<t>_<cols>_key`, `<t>_<cols>_fkey`, `<t>_<col>_check`, with 63-byte
      truncation and a numeric collision suffix
    - MySQL: `PRIMARY`, `<t>_ibfk_<n>`, `<t>_chk_<n>`, and a UNIQUE named after its first
      column, with `_2`, `_3` on collision
    - SQLite: declared names only
- [x] §6.1: state that introspected defaults and CHECK / domain-constraint text are normalized to
  the file parser's form, with catalog-added implicit casts removed (Q5, Q13). State that
  `database` and `files` therefore describe the same schema identically.
- [x] §6.3 capability table and notes:
  - PG partitions are skipped; only the partitioned parent is introspected (Q4).
  - SQLite virtual and shadow tables are skipped with a warning (Q9).
  - SQLite "Unique constraints" and "Check constraints" change from "Limited" to "Yes", with a
    note that names and CHECK text come from the stored `CREATE TABLE` (Q8, Q10).
  - Footnote: MySQL CHECK introspection needs 8.0.16+, and an older server yields no CHECKs,
    with no hard minimum version (Q7).
- [x] §7 type resolution: an enum, set or domain resolves under its bare name or its
  schema-qualified name. A bare key is registered only when no other schema has the same name.
  Both paths write a type in the column's own schema bare, and a cross-schema type qualified (Q3).
- [x] §8.1.7 CLI tests, exit-code bullet: 4 means a connection or catalog-query failure only; a
  schema error exits 3 under every `source` (Q2).
- [x] (Superseded for PK / UNIQUE by the Q11 addendum; see the Completion Record.) §30.7 `indexes[]` row: replace "derived labels … PostgreSQL's default on every dialect"
  with the Q11 rule. Names are the ones the database assigns. They are derived per dialect for
  unnamed constraints, and on SQLite only declared names are used, with an unnamed index keeping
  the existing derived label. Keep the sentence about a `primary_key.columns`-only key.
  **Word the SQLite case with the user before landing**, because Q11 leaves the manifest label
  for an unnamed SQLite constraint implicit.
- [x] §30.7 `check` row: replace "as written in the DDL" with "in the file parser's normalized
  form" (Q13).
- [x] Cross-check every edit against design doc §8. Fix `#` anchors that shift.

### Acceptance Criteria

- Every edit in design doc §8 is in the PRD, and every rule in Q1–Q14 that changes user-visible
  behavior is stated in the PRD.
- No PRD section still says `database` mode reads `paths`, that §30.7 index names are
  PostgreSQL-style on every dialect, or that CHECK text is "as written".
- The PRD has no broken internal anchors.

### Tests Required

- [x] None (docs only). `grep` checks for the retired phrasings listed in the acceptance
  criteria, plus an anchor check of every `](#…)` link touched.

### Completion Record

- **Completed:** 2026-10-05 (`/done 30.0`). Docs only; `make check` passed.
- **Files:** `docs/PRD.md`; `docs/configuration.md`; `docs/design/INTROSPECTION.md` (Q11 addendum in §10);
  `docs/tracker/phase-30.md` (30.2 and 30.7 tasks carry the addendum).
- **PRD edits:**
  - §4.2 `paths` and `introspect` rows.
  - §5.2: PG generated columns, `PARTITION OF` skip and typmod spelling; MySQL generated columns;
    SQLite rowid-alias rule and `CREATE VIRTUAL TABLE` skip; a new **Constraints (all
    dialects)** paragraph with the per-dialect name table.
  - §6.1: the `database` row, plus a "Both sources describe a schema the same way" paragraph
    (Q5, Q13).
  - §6.3: SQLite `table_xinfo`; UNIQUE / CHECK changed to "Yes"; the MySQL CHECK footnote; notes
    on partitions, virtual tables and SQLite names.
  - §7.1: override key matching (exact, then modifiers stripped) and enum / SET / domain name
    resolution (Q3, Q12).
  - §8.1.7 exit-code bullet.
  - §30.7 `indexes[]` and `check` rows.
- **Beyond the task list:**
  - §23.9's row for exit 4 is reworded to match §8.1.7, so the two tables agree.
  - The §6.3 SQLite "Tables & columns" cell now names `table_xinfo`, per Q1.
- **SQLite §30.7 wording, ruled with the user 2026-10-05.** A probe on PG 16, MySQL 8.0 and
  sqlite3 ran `email UNIQUE` + `UNIQUE(email)`, `id … PRIMARY KEY UNIQUE` and a named
  `CONSTRAINT named_u UNIQUE(b)`. PG and SQLite collapse the duplicates. MySQL keeps them as
  `id` and `email_2`, with warning 1831. SQLite names every auto-index `sqlite_autoindex_t_N`,
  the named one included. Two rulings, both the recommended option:
  - an unnamed SQLite PK / UNIQUE is listed as `<t>_pkey` / `<t>_<cols>_key`, and the `_idx`
    label goes
  - duplicates follow the database, which replaces the old "inline UNIQUE is listed even
    when…" sentence
  Both are recorded in the design doc §10 Q11 addendum and threaded into 30.2.
- **Auto-review round.** `sqlgen-reviewer` found 3 issues and 5 inline fixes, and asked 5
  questions. A second probe, on the same three engines, settled the factual ones:
  - PG merges duplicates only within one `CREATE TABLE`, under the first declared name
    (`id PRIMARY KEY, CONSTRAINT bu UNIQUE(id)` → PK `bu`). `ALTER … ADD` gives `_key1`.
  - SQLite keeps a UNIQUE on a rowid-alias PK.
  - PG names a multi-column or no-column CHECK `<t>_check` and an unnamed index `<t>_<cols>_idx`,
    and keeps derived names across renames.
  - MySQL numbers unnamed `KEY` and `UNIQUE` together (`c`, `c_2`, `c_3`), builds an implicit FK
    index (named `fk_named` or `e`), renames `_ibfk_` / `_chk_` with the table, and always names
    the PK `PRIMARY`.

  The inline fixes were applied: §7.1 "steps 3–5", the PG CHECK name, the scoped duplicate rule,
  the MySQL `PRIMARY` caveat in §30.7, a rename sentence in §5.2, and a superseded note on the
  §30.7 task.

  The user ruled on two more, both the recommended option:
  - plain indexes and MySQL's implicit FK index follow the database (PRD §5.2 "Indexes get the
    database's names too"; 30.2 tasks)
  - ignored input keys warn (PRD §4.13 row and §4.2; 30.7 task)

  Reviewer Q5 was auto-accepted: §6.1 stays absolute, and a new phase ground rule makes any
  remaining exception a PRD edit.
- **Sync gap found and fixed.** Design doc §7.1 defaults `port` to 5432 / 3306, but PRD §4.3
  said it was required, and §8 never listed that edit. §4.3 and `docs/configuration.md` now
  give the default. The `config/schema/v1.json` description is a 30.7 task, since it's code.
- **Checks:**
  - The retired phrasings ("as written in the DDL", "PostgreSQL's default … on every dialect",
    "derived labels", a `Limited` capability cell) are gone. The only `paths` + `database`
    mentions are the two new "not read" statements.
  - An anchor scan of every `](#…)` link in the PRD found 0 unresolved.
  - No heading was renamed, so no anchor shifted.

---

## 30.1 Parity harness, probe fixtures and unit tests (all dialects)

**PRD Reference:** Sections 5.2, 6.3 (design doc §6.0, §6.1, §6.3, §6.4)

**Status:** Complete

### Tasks

- [x] Add `assertSchemaParity(t, dialect, parsed, introspected, ignore ...parityException)` to
  `parser/introspect/introspect_integration_test.go`.
  - It sorts tables, constraints, enums, sets, composite types, domains and views on both
    sides, then runs a full `cmp.Diff`.
  - `parityException{Field, Reason}` is the only way to exclude a field.
  - All exceptions live in one table at the top of the file, keyed by dialect.
- [x] Move `TestPostgresIntrospect`, `TestMySQLIntrospect` and `TestSQLiteIntrospect` onto the
  helper.
  - Seed the exception table with only the provenance fields no catalog stores
    (`InlineUnique`).
  - Record every resulting failure in the Completion Record. These are the baseline the later
    sub-items burn down; reference them by defect ID.
- [x] Turn the probe DDL from `.claude/prototypes/introspection-probe/zz_probe_test.go` into
  per-dialect fixture constants (PG schema `probe_app`/`probe_other`, MySQL tables `m_*`,
  SQLite `s_*`) for 30.2–30.6 to share.
- [x] Add unit tests, table-driven and white-box:
  - `TestNormalizePostgresType`, `TestParsePostgresArray` and `TestExtractCheckExpression` in
    `postgres_test.go`
  - `TestNormalizeMySQLType` and `TestExtractMySQLEnumValues` in a new `mysql_test.go`
  - `TestNormalizeSQLiteType` in `sqlite_test.go`
- [x] Fix D2: `extractCheckExpression` handles the `NOT VALID` suffix (`postgres.go:927`).
- [x] Fix M3: `extractMySQLEnumValues` respects quoting, so a value containing a comma, a
  doubled quote (`''`) or an empty value survives (`mysql.go:523`).
- [x] Fix M8: the MySQL parser unescapes `''` inside an ENUM/SET value (it produces `it\'s`
  today).
- [x] Fix S6: `indexPartialWhere` checks `err != nil` (other than `sql.ErrNoRows`) before
  `ddl.Valid` (`sqlite.go:483`).
- [x] Fix S10: the SQLite parser no longer turns `INTEGER PRIMARY KEY DESC` into a phantom `DESC`
  column.

### Acceptance Criteria

- All three main introspection tests compare the full schema through `assertSchemaParity`. No
  dialect test hand-picks fields.
- The exception table holds only fields no catalog can round-trip, each with a reason.
- D2, M3, M8, S6 and S10 are fixed, and each has a test that failed before its fix.
- `TestMySQLIntrospect_EnumEdgeValues` has the same values on both paths.

### Tests Required

- [x] `TestNormalizePostgresType`: `character varying(255)`, `character(3)`,
  `timestamp(3) without time zone`, `timestamp with time zone`, `time with time zone`, arrays of
  each, `double precision`, `bit varying(8)`, a schema-qualified name, passthrough.
- [x] `TestParsePostgresArray`: `{}`, a plain list, a quoted element with a comma or space, an
  escaped quote, a mixed-case quoted identifier.
- [x] `TestExtractCheckExpression`: `CHECK ((a > 0))`, compound `AND` / `OR`, a `NOT VALID`
  suffix (D2), non-CHECK passthrough.
- [x] `TestNormalizeMySQLType`: `int`, `int unsigned`, `tinyint(1)`, `bigint(20)`,
  `decimal(10,2)`, `enum(…)` / `set(…)` → bare, mixed case.
- [x] `TestExtractMySQLEnumValues`: a plain list, a value with a comma, `it''s`, an empty value,
  values with spaces (M3).
- [x] `TestMySQLIntrospect_EnumEdgeValues` (integration): `ENUM('active','in,active','it''s','')`
  gives identical values from the parser and the introspector (M3, M8).
- [x] `TestNormalizeSQLiteType`: empty → `blob`, case folding, `VARCHAR(255)` / `NUMERIC(10,2)`
  passthrough.
- [x] SQLite parser test: `id INTEGER PRIMARY KEY DESC, v TEXT` gives exactly two columns (S10).
- [x] `indexPartialWhere` error path (S6): code fix only. No DB-level trigger exists (design doc
  §6.4); note that in the Completion Record.

### Completion Record

- **Completed:** 2026-10-05 (`/implement 30.1`, `/done 30.1`). `make check` and
  `make check-examples` passed. The integration suite
  passes except the three baseline tests below, which fail by design.
- **Files:**
  - `parser/introspect/introspect_integration_test.go`: `parityException`, the
    `parityExceptions` table, `assertSchemaParity`, `requireDatabases`, the three main tests
    moved onto the helper, and `TestMySQLIntrospect_EnumEdgeValues`
  - `parser/introspect/parity_fixtures_test.go` (new): `pgParityFixture`,
    `mysqlParityFixture`, `sqliteParityFixture`, two reset constants, and
    `TestParityFixtures`, which applies each fixture and parses it statement by statement
  - `parser/introspect/{postgres,mysql,sqlite}.go` and their `_test.go` files
    (`mysql_test.go` is new)
  - `parser/mysql/mysql.go`, `mysql_test.go`: `decodeEnumValue` (M8),
    `TestInlineEnumEdgeValues`
  - `parser/sqlite/sqlite.go`, `sqlite_test.go`: `isPrimaryKeySortOrder` (S10),
    `TestPrimaryKeySortOrder`
  - `parser/schema.go`: comment fix on `FKReference.Synthetic`
- **Fail-first evidence (against HEAD):**
  - **D2:** `extractCheckExpression("CHECK ((v > 0)) NOT VALID")` = `((v > 0)) NOT VALID`.
    `NO INHERIT` has the same shape (PG 16 measured `CHECK ((s <> ')('::text)) NO INHERIT`),
    so the fix reads the parenthesized group after CHECK instead of trimming a suffix.
  - **M3:** `enum('active','in,active')` gave `[active in active]`. `'it''s'` stayed
    `it''s`, `''` was dropped, and `'b\\c'` stayed escaped.
  - **M8:** the parser gave `it\'s` and `b\\c`. Vitess re-encodes `EnumValues` with MySQL
    backslash escapes, so `decodeEnumValue` undoes them (and keeps `\%` / `\_`, as MySQL does).
  - `TestMySQLIntrospect_EnumEdgeValues` failed on both paths at HEAD.
  - **S10:** `id INTEGER PRIMARY KEY DESC, v TEXT` gave columns `id`, `DESC`, `v`. `ASC`
    gave the same. rqlite/sql reads the sort order as a new column. The fix folds it back into
    the key when only blanks or comments separate it from `KEY`, so `id INTEGER PRIMARY KEY,
    desc TEXT` keeps its `desc` column.
  - **S6:** code fix only. No DB-level trigger exists (design doc §6.4).
- **Probes behind the expectations (2026-10-05):**
  - MySQL 8.0 `COLUMN_TYPE` for `ENUM('b\\c','it''s','n\nl','t\%u')` is
    `enum('b\\c','it''s','n\nl','t\\%u')`. A quote is doubled, and a backslash or
    newline is backslash-escaped.
  - PG 16 accepts an empty enum label. `array_agg` renders it `""`.
- **Found and fixed inline:**
  - **Unit tests never ran under `-short`.** `TestMain` called `os.Exit(0)` before
    `m.Run()`, and the white-box tests share its binary. So `make check` had never run
    `TestChooseMatviewPK` or `TestExtractSQLiteIndexWhere`. `TestMain` now runs `m.Run()`, and
    each integration test calls `requireDatabases(t)`.
  - **`bit varying(8)`** introspected as `bit varying(8)`, but the PG parser gives `varbit(8)`.
    `normalizePostgresType` now maps it.
  - **A trailing empty element** (`{a,""}`, `{""}`) was dropped by `parsePostgresArray`, so an
    empty last enum label vanished.
  - **`FKReference.Synthetic` comment.** It said the MySQL and SQLite parsers leave `Column`
    empty for `REFERENCES t`. Both reject that DDL; the PG parser is the one that leaves it
    empty.
- **Baseline: parity failures for later sub-items to burn down.** All three main tests
  fail on `assertSchemaParity`, seeded with `Column.InlineUnique` only:
  - `TestPostgresIntrospect`:
    - X1 (30.2): `posts_pkey`, `users_pkey`, `posts_user_id_fkey` and `users_email_key`
      (`Method: btree`) appear only when introspected. CHECK `users_age_check` has `Columns`
      nil when parsed and `[age]` when introspected.
    - Q5 (30.4): bigserial `Default` is `nextval('…_id_seq'::regclass)` when introspected,
      empty when parsed.
    - D8 / Q13 (30.4): CHECK `age > 0` vs `(age > 0)`; domain CHECK `value > 0` vs
      `(VALUE > 0)`.
    - **Unowned:** the unnamed domain CHECK is `""` when parsed and `positive_int_check` when
      introspected. 30.4's tests list "domain CHECK name", but no task derives it. Noted on
      30.4.
  - `TestMySQLIntrospect`:
    - X1 (30.2): `Method` is `""` vs `btree` on `idx_email` and on the FK backing index.
    - M2 / Q5 (30.5): `status` default `'active'` vs `active`.
    - M7 / Q13 (30.5): CHECK `age > 0` vs `` (`age` > 0) ``.
  - `TestSQLiteIntrospect`:
    - X1 (30.2): the unnamed `posts.user_id` FK constraint appears only when introspected.
    - S4 / Q8 (30.6): `sqlite_autoindex_users_1` UNIQUE (`Method: btree`) appears only when
      introspected.
- **For later sub-items:**
  - The SQLite parser (rqlite/sql) rejects `REFERENCES t` with no column list, at column and
    table level alike. 30.6's S3 task and `TestSQLiteIntrospect_ImplicitFKTarget` assume the
    parser resolves it, so `sqliteParityFixture` leaves that table out. Noted on 30.6 and
    logged in `backlog.md`.
  - `isPrimaryKeySortOrder` is where 30.6's S11 parser fix can learn the PK is `DESC`.
  - `unescapeMySQL` exists in both `parser/mysql` and `parser/introspect`, one private copy
    each. The two inputs, vitess's encoding and `COLUMN_TYPE`, share MySQL's escape rules.
- **Auto-review (`sqlgen-reviewer`):** 0 issues and 0 questions. It reproduced the fail-first
  run for S10 and the recorded baseline in a throwaway worktree. Its inline doc fixes are
  applied:
  - test and `sqlite.go` line references in 30.4–30.6, which this diff shifted
  - the backlog rqlite line, narrowed to the forms that actually fail
  - design doc §6.0: the `-short` skip now goes through `requireDatabases`
  - the `parityExceptions` comment
- **`/verify 30.1` (2026-10-05):** 9/9 requirements PASS, 9/9 tests present, and no FIX logged.
  An independent reviewer re-confirmed fail-first for M8. It probed the S10 fold with multibyte
  text, `CONSTRAINT pk PRIMARY KEY DESC`, and constraints after the sort order. Applied:
  - line references (off by one after the comment edit)
  - the backlog example, which is now `PRIMARY KEY ASC AUTOINCREMENT`, since sqlite3 rejects
    `DESC AUTOINCREMENT`
  - a shorter comment on the fixtures
  - a multibyte case in `TestPrimaryKeySortOrder`, which fails when the source is indexed by
    bytes
  CI's `integration (parser)` job stays red until 30.2 and 30.4–30.6 land.

---

## 30.2 Canonical constraint model — `parser.CanonicalizeConstraints` (X1)

**PRD Reference:** Sections 5.2, 30.7 (design doc Q11, §4.4 X1)

**Status:** Complete

### Tasks

- [x] Add `parser.CanonicalizeConstraints(table *parser.Table, dialect string)` to the `parser`
  package. After it runs:
  - every column `PrimaryKey` / `Unique` / `FKReference` flag has a matching table
    `Constraint`, and every table `Constraint` is reflected in the column flags
  - UNIQUE / Index `Method` defaults to `btree` on PG and MySQL
  - CHECK `Columns` lists the referenced columns on every dialect
- [x] Add per-dialect default-name derivation for unnamed constraints.
  - **PG:** PostgreSQL's `ChooseConstraintName` behavior: `<t>_pkey`, `<t>_<cols>_key`,
    `<t>_<cols>_fkey`, `<t>_<col>_check` (`<t>_check` when the CHECK references no column or
    several), 63-byte truncation (`NAMEDATALEN - 1`), and a numeric
    suffix on collision within the schema.
  - **MySQL:** `PRIMARY`, `<t>_ibfk_<n>`, `<t>_chk_<n>`, and a UNIQUE named after its first
    column, with `_2`, `_3` on collision. A PRIMARY KEY is `PRIMARY` even when declared with a
    name.
  - **SQLite:** names come only from the declaration.
  - Renames (probed 2026-10-05). PG keeps a derived name across `RENAME TABLE` /
    `RENAME COLUMN` (`e_pkey`, `e_v_key` on `e2.w`). MySQL 8.0 `RENAME TABLE` renames
    `q_ibfk_1` → `q2_ibfk_1` and `q_chk_1` → `q2_chk_1`. Each parser matches its database.
- [x] Duplicates follow the database (Q11 addendum, design doc §10; PRD §5.2). Probed
  2026-10-05:
  - **PG:** merges duplicates within one `CREATE TABLE` into the first, which takes the first
    declared name. `id int PRIMARY KEY, CONSTRAINT bu UNIQUE(id)` gives a PK named `bu`. A
    duplicate added by `ALTER TABLE … ADD` is a second constraint (`c_email_key1`).
  - **SQLite:** drops a UNIQUE only when an identical index exists, so `INTEGER PRIMARY KEY
    UNIQUE` (a rowid alias) keeps its autoindex.
  - **MySQL:** keeps everything (`email`, `email_2`).
- [x] Unnamed plain indexes get the database's name (Q11 addendum; PRD §5.2 "Indexes get the
  database's names too"):
  - PG: `<t>_<cols>_idx`, then `…_idx1`.
  - MySQL: the first column's name, sharing one counter with unnamed UNIQUE (`c`, `c_2`,
    `c_3`, probed).
- [x] MySQL parser: add the implicit index MySQL builds for an FK that no existing index starts
  with. It is named after the FK constraint (`fk_named`), or else the FK's first column (`e`),
  as probed 2026-10-05. Cover it in `TestMySQLIntrospect_TableConstraints`.
- [x] Call `CanonicalizeConstraints` at the end of each parser's table construction
  (`parser/postgres`, `parser/mysql`, `parser/sqlite`) and at the end of each introspector's
  per-table pass.
- [x] PG parser: keep the declared name of an inline `CONSTRAINT <name> UNIQUE`. It is dropped
  today; FIX-240 backlog line, 2026-10-02.
- [x] `cmd/sqlgen/manifest/builder.go` `indexNameOrDerived` / `pkIndexName` (`:954-958`): read the
  canonical constraint or index name instead of deriving one. On SQLite, an unnamed PRIMARY KEY or
  UNIQUE is listed as `<t>_pkey` / `<t>_<cols>_key`, inline or table-level alike. The builder's
  own `_idx` fallback goes (PRD §30.7).
- [x] Regenerate the example goldens (`make update-golden-e2e`). Explain every manifest index or
  constraint-name change in the Completion Record. **Only X1-caused changes are expected.**
- [x] Delete the two FIX-240 backlog lines this resolves (unnamed UNIQUE `_idx` vs `_key`; the
  inline `CONSTRAINT <name> UNIQUE` name dropped). Each line names its resolution in the commit
  message.

### Acceptance Criteria

- Parsing and introspecting the same DDL give the same `Constraints` set, with the same names,
  on all three dialects (PG `checks`, `pairs`, `users`, `accounts`; MySQL `MixedCase`,
  `m_pairs`, `m_users`; SQLite `s_posts`, per the probe).
- An unnamed PG constraint whose derived name exceeds 63 bytes is truncated exactly as
  PostgreSQL truncates it. Two constraints that derive the same name get PostgreSQL's numeric
  suffix.
- The manifest's `indexes[].name` equals the database's index name for PG and MySQL fixtures.
- No golden changes beyond constraint and index names.

### Tests Required

- [x] `TestCanonicalizeConstraints` (table-driven): an inline PK, a table-level PK, a composite
  PK, inline and table-level UNIQUE, an inline `REFERENCES` and a table-level FK, a column CHECK
  and a table CHECK, plus a UNIQUE repeating the PK and an inline + table-level UNIQUE on one
  column, each on PG / MySQL / SQLite. Assert the constraint set, the column flags, `Method` and
  CHECK `Columns`.
- [x] `TestDerivePostgresConstraintName`: every suffix kind, truncation at 63 bytes
  (multi-byte identifiers included), collision suffixes.
- [x] `TestDeriveMySQLConstraintName`: `PRIMARY`, `_ibfk_<n>` ordering, `_chk_<n>`,
  first-column UNIQUE naming with `_2` on collision.
- [x] `TestPostgresIntrospect_TableConstraints` and `TestMySQLIntrospect_TableConstraints`
  (integration): canonical shape and names on both paths, including a PG name past 63 bytes.
- [x] SQLite: `s_posts` inline FK is a table constraint on both paths.
- [x] Manifest builder test: `indexes[].name` reads the canonical name.
- [x] `TestE2EGoldenFiles` passes after the regeneration.

### Completion Record

- **Completed:** 2026-10-05 (`/implement 30.2`). `make check` and `make check-examples` (all
  12 example modules) passed. The integration suite passes except the three main parity
  tests, whose remaining diffs are all owned by 30.4–30.6 (listed under Baseline).
- **Files:**
  - `parser/canonical.go` (new): `CanonicalizeConstraints`, `CheckColumns`
  - `parser/constraint_names.go` (new): `DerivePostgresConstraintName`,
    `PostgresIndexColumnNames`, `DeriveMySQLConstraintName`, `RenameMySQLGeneratedName`
  - `parser/schema.go`: `Column.InlineUnique` removed (every inline UNIQUE is now a
    constraint, so the provenance flag had no job left); `RecomputeColumnUnique` reads
    constraints only
  - `parser/postgres/postgres.go`: inline PK / UNIQUE / REFERENCES / CHECK become table
    constraints (inline CHECKs were dropped before); duplicate PK / UNIQUE merge within one
    `CREATE TABLE`; naming in PostgreSQL's creation order (CHECKs, PK, UNIQUEs, FKs) against
    the schema namespace; unnamed `CREATE INDEX` naming with expression column names;
    `DROP COLUMN` drops the table's constraints that name the column
  - `parser/mysql/mysql.go`: inline keys and `REFERENCES` become constraints; `PRIMARY`;
    `_ibfk_` / `_chk_` numbering (CREATE: among the unnamed; ALTER: after the highest);
    first-column key names; MySQL's `foreign_key_prefix` redundancy rule for built FK
    indexes, at CREATE and ALTER time (the parser tracks which indexes it built);
    `_ibfk_` / `_chk_` names follow a table rename; `DROP COLUMN` shrinks keys
  - `parser/sqlite/sqlite.go`: inline PK / UNIQUE / REFERENCES / CHECK become constraints
    (inline CHECKs were dropped before); a UNIQUE identical to an indexed PK or an earlier
    UNIQUE is dropped
  - `parser/introspect/{postgres,mysql,sqlite}.go`: `CanonicalizeConstraints` at the end of
    each table pass; PG constraint query keeps column-less CHECKs (D5)
  - `cmd/sqlgen/manifest/builder.go`: `pkIndexName` / `uniqueIndexName` read the canonical
    name; SQLite's unnamed labels; the `_idx` fallback and the inline-UNIQUE loop are gone
  - `cmd/sqlgen/gen/context_table.go`: one conflict target per column set (MySQL keeps
    `email` + `email_2`); the "inline PRIMARY KEY reads as app-enforced" known limitation is
    gone, since an inline PK is now a constraint
  - tests: `parser/canonical_test.go`, `parser/constraint_names_test.go`,
    `parser/introspect/constraint_parity_test.go` (new); `TestDerivedConstraintNames` in the
    PG and MySQL parser tests; `TestDropConstraintByDerivedName`;
    `TestBuild_IndexNamesReadTheSchema` (replaces `TestBuild_InlineUniqueIsListed`);
    `TestConflictTargetDuplicateUnique`; `parityExceptions` is now empty for every dialect
  - `docs/PRD.md` §5.2: MySQL `<n>` counts the unnamed ones and ALTER continues from the
    highest; a built FK index is dropped once a later index covers it, and takes the
    `FOREIGN KEY <name> (…)` index name before the first column's
- **Probes (2026-10-05, PG 16 and MySQL 8.0 throwaway containers).** Every expected name in
  the unit tests comes from these.
  - PG: `t_c_check` for `length(c::text)`; `t_check1`; `f_a_fkey1`; `z_a_key1` against a
    table named `z_a_key`; `pk2_pkey1`; 63-byte cuts (`…ghi_colab…def_key`), multibyte cut
    to whole characters; `e_lower_idx`, `e_expr_idx`, `e_a_a1_idx`, `e_x_idx` for
    `(x::varchar)`, `e_a_b_idx` for `(a) INCLUDE (b)`, `e_b_idx` for a UNIQUE INDEX;
    `m2 (CONSTRAINT u1 UNIQUE (id), id int PRIMARY KEY)` → PK `u1`; ALTER-added names
    `ck_a_check1`, `ck_b_key`, `ck_b_fkey`, `ck_b_check`.
  - MySQL: named CHECKs and FKs take no number (`k_chk_1`, `nn`, `k_chk_2`); ALTER after a
    named `ren_ibfk_9` gives `ren_ibfk_10`; `RENAME TABLE` renames user-named
    `ren_ibfk_9` too; unnamed keys use the column's declared spelling (`UNIQUE (a)` on
    column `A` → `A`); a built FK index takes the constraint name, then
    `FOREIGN KEY <name>`, then the first column; of two identical built indexes the later
    stays; an ALTER-built FK index replaces an existing built one it covers (`ren` ended with
    index `a`); `CREATE INDEX (order_id, product_id)` drops the built `order_id`. Inline
    `REFERENCES` is ignored (no FK, no index) — see Questions.
- **Fail-first evidence (against HEAD, in a throwaway worktree):** the three new
  `*_TableConstraints` tests fail on every table that has a constraint (34 of 36; the two
  that pass have none): missing inline PK / FK / UNIQUE constraints, `Method` `""` vs
  `btree`, CHECK `Columns` nil vs `[col]`, unnamed vs derived names. After the parser
  changes and before the D5 query change, `probe_app.checks` and `probe_names.t` still
  failed on the column-less CHECK only.
- **D5 done here.** 30.2's acceptance lists `checks`, whose `always_ok CHECK (1 = 1)` the
  introspector dropped. The fix is the left join named in 30.4's task. 30.4 keeps its test
  (`TestPostgresIntrospect_CheckAndDomainConstraints`, column-less CHECK case).
- **Golden changes (`make update-golden-e2e`, `make update-golden`).** Only the `mysql`
  example's manifest (`manifest_gen.json` and the 18 entity `.md` files, `expected/` and
  `models/` alike); no generated Go changed anywhere, and the PG and SQLite manifests are
  byte-identical. Every change is X1:
  - `<t>_pkey` → `PRIMARY` (18 tables), `<t>_<col>_key` → `<col>` (`email`, `name` ×2,
    `sku`, `user_id`, `slug`), and the re-sort that follows (`PRIMARY` sorts first)
  - `order_items` loses index `order_id`: `CREATE INDEX idx_order_items_order_product ON
    order_items (order_id, product_id)` covers it, so MySQL drops it (re-measured on MySQL
    8.0 with the example's DDL)
- **Baseline left for later sub-items** (the three main parity tests, X1 items gone):
  - `TestPostgresIntrospect`: Q5 bigserial `Default`; D8 CHECK text; the unnamed domain
    CHECK name and text (30.4)
  - `TestMySQLIntrospect`: M2 `'active'` vs `active`; M7 CHECK text (30.5)
  - `TestSQLiteIntrospect`: S4 `sqlite_autoindex_users_1` vs unnamed (30.6)
- **For later sub-items:**
  - 30.6: an unnamed SQLite UNIQUE has `Method: ""` when parsed and `btree` when
    introspected. PRD §5.2 defaults `btree` on PG and MySQL only, so 30.6's Q8 / Q10 work
    decides which side moves.
  - 30.4–30.6: `assertConstraintParity` (in `constraint_parity_test.go`) ignores CHECK text;
    the Q13 work compares it.
- **Backlog lines resolved and deleted** (name each in the commit message):
  - unnamed table-level UNIQUE listed as `_idx` (FIX-240): the builder reads the
    canonical `_key` name
  - inline UNIQUE listed beside a PK or table-level UNIQUE it duplicates (FIX-240):
    duplicates now follow the database
  - PG parser drops an inline `CONSTRAINT <name> UNIQUE` name (FIX-240): kept
  - `DROP CONSTRAINT users_email_key` on an inline UNIQUE was a no-op (FIX-240): the
    inline UNIQUE is that constraint (`TestDropConstraintRecomputesColumnUnique`)
  - `DROP CONSTRAINT` on an unnamed or inline PK was a no-op (FIX-263): it drops
    (`TestDropConstraintByDerivedName`)
- **Ruled 2026-10-05 (user): keep MySQL inline `REFERENCES` as a foreign key.** MySQL 8.0
  ignores it, but the MySQL parser has always made it a foreign key, and keeping it keeps the
  relationship. 30.2 gives it the canonical shape (`<t>_ibfk_<n>` and a built index). PRD
  §6.1 now names it as the one deliberate files/database difference. It also shifts the real
  keys' numbers:
  `t (a int REFERENCES p(id), b int, FOREIGN KEY (b) REFERENCES p(id))` parses as
  `t_ibfk_1 [a]`, `t_ibfk_2 [b]` with indexes `a` and `b`, where MySQL has one key,
  `t_ibfk_1 [b]`, and index `b`. `TestCanonicalizeConstraints` "mysql inline references"
  pins the current choice.
- **Auto-review (`sqlgen-reviewer`):** 1 issue, 1 question (above). It re-confirmed the D5
  fail-first. Applied:
  - **PG `DROP COLUMN` on a composite-PK column** left the other columns flagged `PrimaryKey`
    with no constraint, and the next ALTER invented `<t>_pkey` from the flag. The drop now
    reverses each dropped constraint's flags. Fail-first: the new `TestDerivedConstraintNames`
    case gave `cp PRIMARY KEY cp_pkey`; `pgConstraintNamesFixture` gained the `cp` table.
  - comments: the conflict-target compaction also covers PG's ALTER-added duplicate; the
    builder's CHECK-column note no longer says parsers leave `Columns` empty
  - line references in 30.4–30.6, shifted by this diff
- **Backlog lines added:** vitess key-order approximation; PG
  expression-only index kept by the parser only; PG `INCLUDE` columns; vitess one `KeyOpt`
  per column.

---

## 30.3 Generated columns, all dialects (D4, M1, S1)

**PRD Reference:** Sections 5.1, 5.2 (design doc Q1)

**Status:** Complete

### Tasks

- [x] PG parser: read `GENERATED ALWAYS AS (expr) STORED` from the column constraint. Set
  `GeneratedExpr` (deparsed) and `GeneratedStorage = "STORED"`, and leave `Default` empty.
  Keep identity columns (`GENERATED … AS IDENTITY`) on the existing `AutoIncrement` path.
- [x] PG introspector: select `pg_attribute.attgenerated` in `introspectColumns`
  (`postgres.go:382`). For `'s'`, set `GeneratedExpr` from `pg_attrdef` (normalized like the
  parser's) and `GeneratedStorage = "STORED"`, and clear `Default`.
- [x] MySQL parser: read `GENERATED ALWAYS AS (expr) [VIRTUAL|STORED]` and the shorthand
  `AS (expr)`. Storage defaults to `VIRTUAL`.
- [x] MySQL introspector: select `generation_expression`. Map `extra` `VIRTUAL GENERATED` /
  `STORED GENERATED` to `GeneratedStorage` and set `GeneratedExpr` in the parser's form
  (`mysql.go:200`).
- [x] SQLite introspector: switch columns to `PRAGMA table_xinfo`. Map `hidden = 2` → `VIRTUAL`
  and `hidden = 3` → `STORED`, take the expression from the stored DDL (30.6's re-parse; until
  then, the `sqlite_master.sql` text), and skip `hidden = 1` (virtual-table hidden columns).
- [x] Confirm the generator's `IsComputed` path (`gen/context_table.go:942`) keeps PG and MySQL
  generated columns out of `Create<T>Input`, `Update<T>Input`, `Increment` columns and insert
  column lists, as it does on SQLite. Fix any template branch that doesn't.

### Acceptance Criteria

- On every dialect and both paths, a generated column has a non-empty `GeneratedExpr`, the
  correct `GeneratedStorage` and an empty `Default`, and the two paths agree.
- A generated column never appears in `Create<T>Input`, `Update<T>Input` or the
  `<T>IncrementColumn` enum on any dialect. Today the PG `files`-mode probe shows `Total` in
  all three.
- PRD §10.3 ("`GENERATED ALWAYS AS (...)` — Excluded") holds on all three dialects.

### Tests Required

- [x] PG parser unit test: STORED generated column → `GeneratedExpr` / `GeneratedStorage`, no
  `Default`; identity columns unchanged.
- [x] MySQL parser unit test: `GENERATED ALWAYS AS (…) STORED`, `… VIRTUAL`, the shorthand
  `AS (…)`, and the default storage.
- [x] `TestPostgresIntrospect_GeneratedColumn`, `TestMySQLIntrospect_GeneratedColumn` and
  `TestSQLiteIntrospect_GeneratedColumn` (integration): parity with the parser through
  `assertSchemaParity`.
- [x] Codegen test per dialect: a table with a generated column has no create, update or
  increment surface for it.

### Completion Record

- **Files changed:**
  - parsers: `parser/postgres/postgres.go` (`CONSTR_GENERATED`, `NormalizeExpr`),
    `parser/mysql/mysql.go` (`opts.As` / `opts.Storage`, `NormalizeExpr`)
  - introspectors: `parser/introspect/{postgres,mysql,sqlite}.go`
  - generator: `cmd/sqlgen/gen/context_table.go` (`isIncrementEligible`)
  - tests: `parser/{postgres,mysql}/*_test.go`,
    `parser/introspect/{introspect_integration,postgres,mysql,sqlite}_test.go`,
    `cmd/sqlgen/gen/context_test.go`, new `cmd/sqlgen/cli/generated_column_test.go`
  - docs: PRD §5.2 / §6.1 / §6.3 / §8.2 / §9.8.9a / §20.2 / §23.7, `STATUS.md`, `backlog.md`,
    this file
- **Fail-first (against HEAD):**
  - `TestGeneratedColumns` (PG, MySQL parser): every generated case had empty
    `GeneratedExpr` / `GeneratedStorage`. The PG identity case already passed.
  - `TestPostgresIntrospect_GeneratedColumn`: `Default: "(qty * 2)"` and no generated fields
    (D4).
  - `TestMySQLIntrospect_GeneratedColumn`: no generated fields on either path (M1).
  - `TestSQLiteIntrospect_GeneratedColumn`: all four generated columns missing from the
    introspected table (S1).
  - `TestGenerate_GeneratedColumnHasNoWriteSurface`: PG and MySQL had `Total` / `Vtotal` in
    `CreateItemInput` and `UpdateItemInput`. **All three dialects, SQLite included,** had them
    in `ItemIncrementColumn`.
- **Generator finding.** `IsComputed` already kept generated columns out of the create and update
  inputs, and so out of the INSERT column lists and the API inputs built from them.
  `isIncrementEligible` never checked it, so a numeric generated column was in the Increment
  enum and the GraphQL `_inc` / `_dec` operators on every dialect. That includes SQLite, whose
  parser already read generated columns. Fixed there. PRD §8.2 now lists generated columns
  among the exclusions, and the §9 `Increment` row says the same. Measured: `UPDATE … SET a =
  a + 1` on a generated column fails on PG 16 ("can only be updated to DEFAULT"), MySQL 8.0
  (3105) and SQLite ("cannot UPDATE generated column").
- **Expression text is the parser's form on every path:**
  - PG: catalog text goes through `postgres.NormalizeExpr`, the parser's own `pg_query`
    deparse, so `(qty * 2)` becomes `qty * 2`.
  - MySQL: `generation_expression` is backslash-escaped again on top of `SHOW CREATE TABLE`'s
    text (measured: `_latin1\'it\\\'s\'`). `unescapeMySQLCatalogText` undoes that, and
    `mysql.NormalizeExpr` (vitess) drops the parentheses and backticks. Without the unescape,
    vitess fails to parse any expression containing a string literal.
  - SQLite: the expression comes from the stored `CREATE TABLE`, re-parsed by `parser/sqlite`
    (a first slice of 30.6's Q10 re-parse). Only tables with a generated column are parsed.
  - Left to 30.4 / 30.5, noted on their tasks: PG implicit literal casts (`price * 2` over
    `numeric` introspects as `price * 2::numeric`) and MySQL charset introducers. Both are the
    Q13 rule set, and `GeneratedExpr` shares it. The parity tests use expressions without
    either.
- **PostgreSQL version floor kept.** `attgenerated` exists from PostgreSQL 12. It is read as
  `to_jsonb(a) ->> 'attgenerated'`, which is NULL on an older server, so the PG 10 floor that
  `attidentity` sets is unchanged. Measured on PG 16 with `attmissingval` populated.
- **Unchanged baselines:** `TestPostgresIntrospect`, `TestMySQLIntrospect` and
  `TestSQLiteIntrospect` fail with exactly the 30.2 baseline (diffed before and after).
- **Lint:** `assertSchemaParity`'s `ignore` parameter now trips `unparam`. It only trips with
  more call sites, which this diff added. Kept with a reasoned `//nolint`, because 30.1 made
  `ignore` the hook for a test-local parity exception.
- **PostgreSQL 18 VIRTUAL generated columns (ruled 2026-10-05, user): introspect now, parse
  later.** `attgenerated = 'v'` maps to `VIRTUAL` (`postgresGeneratedStorage`). Fail-first:
  `TestPostgresIntrospect_VirtualGeneratedColumn`, on its own `postgres:18-alpine` container,
  found `implicit_total` and `virtual_total` were not generated (their expression was the
  `Default`). The PG parser keeps the PostgreSQL 17 grammar until `pg_query_go/v18` is released.
  The v18 branch (open PR #151) parses both spellings, as checked in a scratch build. PRD §5.2,
  §6.1 and §6.3 state the difference.
- **Backlog lines added:** the PG parser's missing PostgreSQL 18 VIRTUAL support, to revisit
  with `pg_query_go/v18`; PG `ALTER COLUMN … DROP EXPRESSION` ignored by the parser.
- **Line references** in 30.4–30.6 updated for this diff.
- **Auto-review (`sqlgen-reviewer`):** no issues. It confirmed the fail-first independently by
  removing `!col.IsComputed`. Applied 7 doc fixes:
  - PRD increment wording (§9.8.9a, §20.2, §23.7) now includes "writable"
  - the `context_table.go` op-resolution comment and the `isIncrementEligible` comment
  - the D5 line reference
  - a 30.6 Q10 note to fold in `sqliteGeneratedExprs`
- **When expression parsing fails (measured 2026-10-05, after the user asked):**
  - **PG:** none of 20 expression forms failed on PG 16 or PG 18. The input is
    PostgreSQL's own `pg_get_expr` output, parsed with the PG 17 grammar.
  - **MySQL:** none of 25 forms failed on 8.0, including JSON paths, `CAST`, `COLLATE`,
    `INTERVAL`, `ESCAPE` and escaped quotes and backslashes. Not measured: 5.7 and MariaDB,
    whose escaping may differ.
  - **SQLite:** this is where it fails. The stored DDL goes through rqlite/sql, which
    rejects some legal SQLite (see the 30.6 Q10 note). The files path fails on the same DDL,
    so both paths are loud. A fallback could not recover the expression; the column would
    become writable, and inserts would fail.
  - Every failure still aborts introspection. The differences found are text only (see the
    30.4 and 30.5 notes), and codegen reads only whether `GeneratedExpr` is set.
  - **Ruled 2026-10-05 (user):** the text differences are fine, and the SQLite failures are
    accepted for now and documented (PRD §6.3, backlog), with a fix later.
- **Not measured:** the PG `to_jsonb` floor below PG 16.
- **Full integration suite (2026-10-05, `-race`, every module):** the only failures are the
  three by-design parity tests above. `make test-integration` stops at the first failing
  module, so while they stay red it skips `cmd/sqlgen` and every module after it. Run those
  by hand until 30.4–30.6 land. **Ruled (user): do 30.4–30.6 next** to close that window.
- **`/verify 30.3` (2026-10-05):** PRD 4/4, tests 4/4, no FIX logged. `make check` and
  `make check-examples` pass. The independent reviewer re-confirmed the fail-first on the
  PG 18 VIRTUAL mapping and found no issues. Applied its doc fixes: line references the PG 18
  additions shifted; the files list; and "writable" in PRD §20.2, the `buildIncrementColumns`
  comment and the `context_api.go` increment comments. That §20.2 sentence also said `Upsert`
  is always present, which contradicts §4.6's conflict-target gate, so it was fixed too.
  `SET EXPRESSION` was added to the `DROP EXPRESSION` backlog line.

---

## 30.4 PostgreSQL parity (D1, D3, D5, D6, D7, D8, D9)

**PRD Reference:** Sections 5.2, 6.1, 6.3, 7 (design doc §6.2, Q3, Q4, Q5, Q12, Q13, Q14)

**Status:** Complete

### Tasks

- [x] D6 / Q3, resolver (`cmd/sqlgen/gotype/gotype.go:447`, `:756`):
  - `RegisterEnum`, `RegisterSet` and `RegisterDomain` also register a `schema.name` key
  - the bare key is registered only when that name is unique across schemas
  - `registerSchemaTypes` (`gen/orchestrate.go:2529`) passes the schema
- [x] D1 / D6 / Q3, canonical type form in the PG parser (`typeNameToString`, `postgres.go:1135`, now reached
  through `typeName` at `:1076`)
  and the introspector:
  - a type in the column's own schema is written bare
  - a cross-schema type stays qualified
  - array suffixes are preserved
- [x] D3 / Q4: skip tables with `pg_class.relispartition` in `introspectTables`
  (`postgres.go:76`).
- [x] D9 / Q14: the PG parser skips `CREATE TABLE … PARTITION OF` and appends a `schema.Warnings`
  line in the existing `CREATE VIEW` style.
- [x] D5: the constraint query keeps column-less CHECKs. Replace the inner join on
  `unnest(conkey)` with a left join, or handle `conkey IS NULL` (`postgres.go:601`).
  Done in 30.2, whose acceptance needs `checks`; this sub-item's test still covers it.
- [x] D7 / Q12: the PG parser's typmod formatting (`formatTypmods`) writes `numeric(12,2)`
  without a space.
- [x] Q5 defaults:
  - run catalog default text through the same `pg_query` deparse the parser uses
  - strip catalog-added casts on literals (`''::text`, `'active'::status_enum`,
    `'{}'::text[]`)
  - serial and identity columns carry no `Default`, only `AutoIncrement`
- [x] D8 / Q13, CHECK and domain-constraint text:
  - same deparse as defaults
  - strip implicit casts: domain → base type (`(score)::integer`) and literal → column type
  - normalize keyword case (`VALUE`)
  - each removal rule is a named function with its own unit test
  - the literal-cast rule also covers `GeneratedExpr`. 30.3 added `postgres.NormalizeExpr` (the
    parser's deparse) and puts generated expressions through it, but `price * 2` over a
    `numeric` column still introspects as `price * 2::numeric`. Add that case to
    `TestPostgresIntrospect_GeneratedColumn`.
  - **Not only casts (measured in 30.3 on PG 16 and 18).** The catalog also rewrites
    `x IN (1, 2)` to `x = ANY (ARRAY[1, 2])`, `SIMILAR TO` to `~ similar_to_escape(…)`,
    and `LIKE … ESCAPE` to `~~ like_escape(…)` (the parser deparses the last as
    `LIKE pg_catalog.like_escape(…)`). The cast rules don't undo these. **Ruled 2026-10-05
    (user): these text differences are fine.** They become named parity exceptions in the
    tests and in PRD §6.1, not reversals.
- [x] Tighten `TestPostgresIntrospect` (`introspect_integration_test.go:201`) through `assertSchemaParity`, with no
  PG-specific exceptions beyond the 30.1 provenance set. (30.1 moved it onto the helper.
  This task makes it pass; its baseline is in the 30.1 Completion Record.)
- [x] Unnamed domain CHECK name: the PG parser leaves it `""`, and PG names it
  `<domain>_check` (`positive_int_check`). Found by the 30.1 baseline. No other task derives it,
  and the test below asserts it.

### Acceptance Criteria

- The PG probe schema (`probe_app` / `probe_other`) parses and introspects to identical
  schemas through `assertSchemaParity`, with and without `search_path` on the connection.
- `status app.status_enum` generates the `StatusEnum` Go type in `files` mode. Today it
  generates `string`.
- A partitioned table yields the parent only on both paths, plus a parser warning for each
  `PARTITION OF`.
- `overrides.types."numeric(12,2)"` applies to a `numeric(12,2)` column on both paths.

### Tests Required

- [x] `TestPostgresIntrospect_TypeCoverage`:
  - `uuid`, `numeric(12,2)`, `bytea`, `interval`, `inet`, `smallserial`, `serial`
  - `int GENERATED ALWAYS AS IDENTITY` and `bigint GENERATED BY DEFAULT AS IDENTITY`, both
    `AutoIncrement`
  - an enum column, a domain column, `status_enum[]`
  - each checked against the parser **and** an explicit expected `Type`
- [x] `TestPostgresIntrospect_Defaults`: a literal, `now()`, `gen_random_uuid()`, an enum
  literal, `'{}'`, serial, identity. `Default` equals the parser's text.
- [x] `TestPostgresIntrospect_CheckAndDomainConstraints`: CHECK text equals the parser's,
  including a CHECK on a domain-typed column (D8), `NOT VALID` (D2), a column-less CHECK (D5),
  and domain CHECK name and text.
- [x] `TestPostgresIntrospect_NoSearchPath`: own-schema types bare, cross-schema types qualified
  (D1).
- [x] `TestPostgresIntrospect_QuotedIdentifiers`: `"UserAccounts"`, `"createdAt"`, a constraint
  over quoted columns.
- [x] `TestPostgresIntrospect_Partitioning` (D3) and a PG parser test for the `PARTITION OF`
  warning (D9).
- [x] Resolver unit tests:
  - a qualified enum, set or domain resolves
  - a bare name that is unique resolves
  - a bare name shared by two schemas resolves only qualified
- [x] Unit tests for each implicit-cast removal rule and for the default normalization.
- [x] Codegen test: `status app.status_enum` in `files` mode resolves to the enum Go type (D6).

### Completion Record

- **Status: Complete.** Every task and test is done. Both design questions are ruled and
  implemented: the shared enum name (below, **Owner schema first**) and annotation-file views
  (below, **Annotation-file views**). `make check`, the full `parser/introspect` integration
  suite and `make check-examples` pass on the merged 30.4 + 30.5 + 30.6 tree.
- **Files:**
  - `parser/postgres/catalog_expr.go` (new): `CatalogExpr`, `TableImplicitCasts`,
    `DomainImplicitCasts`, and the two removal rules `stripLiteralCast` and
    `stripImplicitColumnCast`, with `walkExpr` over the expression tree
  - `parser/postgres/postgres.go`:
    - `typeName` / `retargetTypeName`: own-schema enum, domain and composite types are bare and
      cross-schema ones are qualified, for columns, `ALTER COLUMN TYPE`, `ADD COLUMN`, composite
      attributes and domain base types; `SET SCHEMA` rewrites the table's column types
    - `PARTITION OF` is skipped with a warning
    - `formatTypmods` has no space
    - unnamed domain CHECKs are named; domain `NOT NULL` is no longer a type-0 constraint
    - `parseExpr` split out of `NormalizeExpr`
  - `parser/introspect/postgres.go`:
    - partitions are skipped
    - `columnType` + `userTypeJoin` for columns, view columns, composite attributes and domain
      base types
    - defaults, generated expressions, CHECKs, domain CHECKs and partial-index predicates go
      through `CatalogExpr`
    - a serial column (sequence owned with deptype `a`) has no `Default`
    - domain constraints are read with `contype = 'c'`, after every domain's base type
  - `cmd/sqlgen/gotype/gotype.go`: `RegisterEnum` / `RegisterSet` / `RegisterDomain(schema, name, …)`,
    `registerSchemaType`, `domainBaseType`; `cmd/sqlgen/gen/orchestrate.go` passes the schema
  - tests:
    - `parser/postgres/{catalog_expr_test,type_names_test}.go` (new), `postgres_test.go`
    - `parser/introspect/postgres_integration_test.go` (new, so the shared file only gains
      `pgSkippedStatementExceptions` and the `GeneratedColumn` cases), `postgres_test.go`
    - `cmd/sqlgen/gotype/gotype_test.go`
    - `cmd/sqlgen/cli/schema_type_names_test.go` (new)
    - call-site updates in four `cmd/sqlgen/gen/*_test.go` files
  - goldens: `make update-golden-e2e` changed only the `postgres` and `graphql` example manifests
    (34 files, `expected/` and `models/`): `db_type` `numeric(10, 2)` → `numeric(10,2)` and
    `numeric(12, 2)` → `numeric(12,2)`, plus the Markdown tables that re-align around them. No
    generated Go changed.
  - docs: PRD §6.1 (new paragraph "PostgreSQL expression text the catalog rewrites"),
    `backlog.md`, this section
- **Probes (2026-10-05, throwaway `postgres:16-alpine`).** Every expected text in the tests
  comes from these.
  - Defaults are stored before coercion to the column type, and every constant whose type is not
    integer, numeric or boolean prints with a type label, written or not:
    - `'x'::character varying`, `'active'::app.status_enum`, `''::text`
    - a negative number prints quoted (`'-1'::integer`), as does a number past 32 bits
      (`'3000000000'::bigint`)
  - CHECK text adds implicit column casts: `((score)::integer < 1000)` for a domain column,
    `(code)::text` for a varchar column (`(x)::text` directly for a varchar domain), and
    `(VALUE)::text` in a varchar domain.
  - Rewrites no cast rule can undo:
    - `BETWEEN` → `>= … AND <= …` (new; the 30.3 list had `IN`, `SIMILAR TO` and
      `LIKE … ESCAPE`)
    - a `timestamptz` literal → `'2000-01-01 00:00:00+00'`, and `'epoch'` → `'1970-01-01 …'`
  - Domain CHECK names follow `ChooseConstraintName` against every constraint in the schema:
    - `d_check1` when a table constraint holds `d_check`; `e_check` when the clash is in
      another schema
    - `dom_re_check1` after a declared `dom_re_check`
    - 63-byte truncation of the domain name
  - Partitions are listed in `information_schema.tables` as `BASE TABLE`.
  - The `pg_` schema prefix is reserved (`unacceptable schema name`), so the new tests use
    `pgi_*`.
- **Fail-first evidence (against HEAD).** The parser tests ran against HEAD's parser. The
  introspector tests ran against HEAD's `parser/introspect/postgres.go`, restored by hand.
  - **D7:**
    - `TestCreateTableColumnTypes`: `numeric(10, 2)` / `numeric(12, 2)[]`
    - `TestGenerate_TypmodOverrideKey`: `Price` = `float64`, because an
      `overrides.types."numeric(12,2)"` key with a non-integration import missed the parsed
      column
  - **D9:** `TestPartitionOfSkipped` listed `events_2024` and `app.events_2025` as tables, with
    no warnings.
  - **D1 / D6, parser:**
    - `TestTypeNamesFollowColumnSchema`: `app.status_enum`, `app.status_enum[]`,
      `app.positive_int` and `app.addr` in an `app` table; an unqualified `mood` from an `app`
      table stayed `mood`; `SET SCHEMA` left `other.region`
    - `TestTypeNamesInCompositesAndDomains`: `other.region` in an `other` composite and domain
  - **D6, codegen:** `TestGenerate_SchemaQualifiedTypeResolves` gave `Status string`,
    `Statuses []string` and `Score string` on both tables.
  - **D1, introspector:**
    - `TestPostgresIntrospect_TypeCoverage`: `pgi_types.mood`, `pgi_types.mood[]`,
      `pgi_types.posint`
    - `TestPostgresIntrospect_NoSearchPath`: `probe_app.status_enum` with no search_path, and
      `region` (unqualified) whenever `probe_other` was on it
  - **D3:** `NoSearchPath` and `Partitioning` introspected `events_2024` / `events_2025` with
    their own `_pkey`.
  - **Q5:** `TestPostgresIntrospect_Defaults`:
    - serial columns: `nextval('pgi_defaults.d_id_seq'::regclass)`
    - literals: `''::text`, `'x'::text`, `'ok'::pgi_defaults.mood`
    - numbers: `'-1'::integer`, `'-2.5'::numeric`, `'3000000000'::bigint`
    - expressions: `(1 + 2)`, `ARRAY['a'::text, 'b'::text]`,
      `(now() AT TIME ZONE 'utc'::text)`
  - **D8:**
    - `CheckAndDomainConstraints`: `((score)::integer < 1000)`, `(VALUE > 0)`, `(1 = 1)` (D5)
      and the `NOT VALID` CHECK's `(id > 0)` (D2)
    - `QuotedIdentifiers`: `(("Email")::text <> ''::text)`
    - `GeneratedColumn`: `price * 2::numeric` and `upper(code::text) || '!'::text`
  - **Domain CHECK name:** `TestDomainCheckNames` gave `""` for every unnamed CHECK, and
    `NOT NULL` became an empty constraint.
  - `TestPostgresIntrospect` failed on its 30.1 baseline: the bigserial `Default`, the CHECK
    text, and the domain CHECK name and text.
- **Results:**
  - `TestPostgresIntrospect` passes with the `"postgres"` exception list still empty.
  - `TestPostgresIntrospect_NoSearchPath` passes with no search_path and with
    `probe_app`, `probe_other,probe_app` and `probe_app,probe_other`. It passes
    `pgSkippedStatementExceptions` (`Schema.Views`, `Schema.Warnings`): the parser skips
    `CREATE VIEW` and `PARTITION OF`, and the test checks the warnings itself.
  - Full parser-module integration run (`-race`): the only failures are
    `TestMySQLIntrospect` and `TestSQLiteIntrospect`, the 30.5 / 30.6 baselines.
- **PRD §6.1 parity exceptions** (`TestPostgresIntrospect_CatalogRewrites` pins each):
  - `IN`, `BETWEEN`, `SIMILAR TO` and `LIKE … ESCAPE`
  - date / time literals
  - a cast the DDL writes on a literal or to an implicit type: the catalog stores it like the
    implicit one, so `CatalogExpr` drops it
- **Found and fixed inline:**
  - partial-index `Where` text (UNIQUE constraints and standalone indexes) had catalog casts and
    parentheses; it now goes through `CatalogExpr` like CHECK text
  - domain `NOT NULL` became a type-0 constraint in the parser. PG 16 keeps it on the type, but
    PG 18 lists it in `pg_constraint` (measured: `nn_not_null`, contype `n`, definition
    `NOT NULL`), which HEAD's introspector would have read as a CHECK. The introspector now reads
    `contype = 'c'` only.
- **Shared enum names: ruled 2026-10-05 (user), owner schema first.** Under Q3 alone, a column
  whose own-schema enum shared its name with another schema's enum resolved to `string`: the
  canonical form writes the type bare, and no bare key is registered for a shared name.
  - `gotype.Resolver.ForSchema(schema)` returns a copy that looks a bare enum, SET or domain name
    up as `<schema>.<name>` first and as the bare name second. Overrides and integrations keep
    their place in the chain, so an `overrides.types` key written with the bare name still wins.
    `Resolve`, `ResolveAggregate`, `BaseSQLType` and `IsSet` all use it.
  - The call sites pass the owner's schema (the eight resolver calls, plus the two tenancy type checks):
    - `buildColumnContext` (new `schema` parameter, from `buildTableColumns`)
    - `viewColumnGoType` (the view's schema)
    - `buildCompositeContexts` / `buildDomainContexts` (the type's own schema)
    - `resolveTenantColumnType` / `resolveViewTenantColumnType` (the table's or view's schema)
  - PRD §7.1 states the lookup order. PRD §5.2 now names unnamed domain CHECKs (`<domain>_check`),
    which this sub-item implemented.
  - **Fail-first:** `TestGenerate_SharedEnumNameResolvesInOwnSchema`, with enums `app.status` and
    `billing.status`, gave `string` for:
    - `app.accounts.status` (`app.status`) and `statuses` (`[]string`)
    - `billing.invoices.status` (bare `status`) and `own` (`billing.status`)
    - the `billing.tags` composite's `s` and `labels`

    The cross-schema `app.accounts.other` and the domain-typed `via_domain` already gave
    `BillingStatus`. All now give `AppStatus` / `AppStatusSlice` / `BillingStatus` /
    `BillingStatusSlice`.
  - Unit tests: `TestResolve_SchemaQualifiedNames` (owner cases for enum, enum array, domain and
    SET; case folding; a qualified name ignores the owner; fallback to a unique bare name; a shared
    name with no type in the owner's schema stays unresolved) and
    `TestForSchema_ClassifiersAndOverrides` (`BaseSQLType`, `IsSet`, a bare-name override still
    wins, a nil resolver).
- **Annotation-file views (ruled 2026-10-05, user: rewrite in `parser/view.go`).** A view
  column copied its source column's type text, which is written relative to the source table's
  schema, and found the source table by bare name. Now `postgres.ParseViewSQL` records each
  FROM table's schema (`ViewSQL.TableSchemas`; an unqualified table takes `input.schema`, as an
  unqualified CREATE TABLE does), `BuildView` takes the whole `*Schema`, and `viewSources` looks
  the table up in that schema and writes the copied type relative to the view's schema: a
  user-defined type of the table's schema is qualified, and one qualified with the view's
  schema is written bare. Array suffixes are kept. An unqualified column is looked up in the
  FROM-clause tables in key order, not over a Go map of every table. MySQL and SQLite views have
  no schema, so nothing changes there.
  - Fail-first, `TestGenerate_AnnotationViewTypesFollowViewSchema` (enums `app.status` and
    `billing.status`): `billing.v_billing` over `app.accounts` gave `BillingStatus`, `v_public`
    gave `string`, and `app.v_invoices` over `billing.invoices` gave `AppStatus`. Want
    `AppStatus` / `AppStatus` / `BillingStatus`; `app.v_app` was already right.
  - Unit tests: `TestBuildView_TypesRelativeToViewSchema`, `TestBuildView_SourceTableBySchema`
    (`parser/view_test.go`), `TestParseViewSQL_TableSchemas` (`parser/postgres`).
  - PRD §7.1 states the rule. The backlog line is resolved.
- **Auto-review (`sqlgen-reviewer`):** 1 issue, 1 question (annotation-file views, above, confirmed
  real). It re-confirmed fail-first by reverting the qualified-key registration
  (`TestGenerate_SchemaQualifiedTypeResolves` failed on `User`). Applied:
  - **Test gap:** no test reached the user-type branch of `columnType` for composite attributes,
    domain base types or view columns. `TypeCoverage` now has a composite with own- and
    cross-schema enum attributes and three domains over enums, and `NoSearchPath` checks
    `active_users.status`. Fail-first against HEAD's introspector: `pgi_types.mood`,
    `pgi_types.mood[]`, `probe_app.status_enum`, and `pgi_types.mood` for the own-schema
    domain. The two cross-schema domains already passed at HEAD, since `format_type` qualifies
    them.
  - **Issue (measured, not fixed here):** a non-PK `serial` column is required in
    `Create<T>Input`, because `buildCreateInputFields` (`gen/context_table.go:2308`) treats a
    column as optional only on `Nullable || HasDefault`. Files mode has always done this (a
    probe gave `CreateItemInput{Note string, Seq int32}` for `seq serial`). Q5 now gives
    `database` the same, since a serial column no longer has a `Default`; the CLI does not
    introspect until 30.8. `config/validate.go:1508` already counts `AutoIncrement` as a
    default. Logged in `backlog.md`.
  - doc fixes: the curly quote in `stripLiteralCast`'s comment, the task line references, the
    call-site count (eight, not seven), and the HEAD behavior in the annotation-view question
  - backlog line: annotation-file view column types follow the source table's schema (since
    resolved, above)
- **Not measured:** PostgreSQL below 16, and `regclass` text in a non-owned sequence's
  `nextval` default with a search_path set.
- **`/verify 30.4` (2026-10-05):** 17/17 requirements, 9/9 tests, reviewer fail-first
  confirmed. Fixed inline: `resolveDomainBase` returned the owner-qualified map key
  (`public.status`) as the base type of a non-domain enum or SET column, against the
  `BaseSQLType` contract; it now returns the type as written (`TestForSchema_ClassifiersAndOverrides`).
  Logged: FIX-270 (tracked; qualified `overrides.types` keys, ruled: try the owner-qualified
  key). Two backlog lines (pg_dump-style serials; excluded-schema domain casts).

---

## 30.5 MySQL parity (M2, M4, M5, M6, M7, M9)

**PRD Reference:** Sections 5.2, 6.1, 6.3 (design doc §6.3, Q5, Q7, Q13)

**Status:** Complete

### Tasks

- [x] M2 / M9 / Q5, default normalization (`mysql.go:238`):
  - re-quote string, enum, set and temporal literal defaults using `column_type`
  - treat a default as an expression when `extra` contains `DEFAULT_GENERATED`
  - put the result through `sqlparser.String`, so it matches the parser's form (for example
    `current_timestamp(3)`)
  - `DEFAULT ''` yields `''`, which is not empty
- [x] M4 / Q7: `introspectCheckConstraints` (`mysql.go:495`) falls back only on MySQL error 1109
  (unknown table), checked with `errors.AsType[*mysql.MySQLError]`. Every other error is
  returned, wrapped.
- [x] M5: make the integer display width agree between the two paths. Decide the direction in
  the Completion Record against the probe (parser `int(11)` vs catalog `int`; `INT UNSIGNED`
  already agrees). If the direction isn't mechanical, stop and ask.
- [x] M6: `introspectViewColumns` (`mysql.go:165`) resolves an ENUM or SET view column to the
  synthetic `<table>_<column>_enum` / `_set` type of its source column when that column is
  identifiable. Otherwise it registers a view-scoped synthetic type. Never the bare `enum`.
- [x] M7 / Q13: normalize `check_clause` through `sqlparser`, dropping backticks and charset
  introducers (`_utf8mb4'x'`) to match the parser's text.
  - `check_clause` is backslash-escaped like `generation_expression`. 30.3's
    `unescapeMySQLCatalogText` + `mysql.NormalizeExpr` already handle that; reuse them.
  - the introducer rule also covers `GeneratedExpr`: a string literal in a generation
    expression comes back with an introducer (`_latin1\'-x\'` in the 30.3 probe, through the
    `mysql` client). Add that case to `TestMySQLIntrospect_GeneratedColumn`.
  - **Not only introducers (measured in 30.3 on MySQL 8.0).** The catalog also rewrites
    `doc->>'$.a'` to `json_unquote(json_extract(doc, '$.a'))`, `doc->'$.a'` to
    `json_extract(…)`, `cast(x as char)` to `cast(x as char charset utf8mb4)`, and `x'41'`
    to `0x41`. **Ruled 2026-10-05 (user): fine,** like the PG rewrites in 30.4. Name them
    as parity exceptions.
- [x] Tighten `TestMySQLIntrospect` (`:286`) and `TestMySQLIntrospectViews` (`:1214`) through
  `assertSchemaParity`. (30.1 moved `TestMySQLIntrospect` onto the helper; its baseline is
  in the 30.1 Completion Record.)

### Acceptance Criteria

- The MySQL probe schema (`m_users`, `m_pairs`, `MixedCase`, `m_user_status`) parses and
  introspects to identical schemas through `assertSchemaParity`.
- A `NOT NULL DEFAULT ''` column is omittable in `Create<T>Input` in `database` mode. Today the
  empty default makes it required.
- A failing CHECK query surfaces as an error. Only error 1109 yields "no CHECKs".

### Tests Required

- [x] `TestMySQLIntrospect_Defaults`: `''`, a string literal, an ENUM literal,
  `CURRENT_TIMESTAMP(3)`, `(uuid())`, `DEFAULT NULL`, a numeric literal. Each equals the
  parser's text.
- [x] `TestMySQLIntrospect_TypeCoverage`: `INT(11)`, `BIGINT(20) UNSIGNED`, `INT UNSIGNED`,
  `BIGINT UNSIGNED`, `tinyint(1)`, `decimal(10,2)`, `json`, `datetime(3)`, `binary(16)`,
  `varbinary`, `blob`, ENUM, SET.
- [x] `TestMySQLIntrospect_CheckErrorsSurface`: cancel the context mid-run, after the column
  queries (a context cancelled up front fails at ping, per the probe). Plus a white-box unit
  test for the 1109 fallback.
- [x] `TestMySQLIntrospect_QuotedIdentifiers`: `` `UserAccounts` ``, `` `createdAt` ``, a
  reserved-word column (`` `order` ``), a constraint over them.
- [x] `TestMySQLIntrospectViews`: a view projecting an ENUM column (M6).
- [x] Codegen test: `DEFAULT ''` column is omittable (M2).

### Completion Record

- **Status: Complete** — every task and test is done. The unsigned-integer question M5 raised
  is ruled and fixed (below). `make check`, the full `parser/introspect` integration suite and
  `make check-examples` pass on the merged 30.4 + 30.5 + 30.6 tree.
- **Files:**
  - `parser/mysql/mysql.go`: `NormalizeType` (integer display widths), `QuoteLiteral`,
    `exprText` (drops charset introducers; `NormalizeExpr` and the parser's default,
    generated and CHECK text all use it), `DEFAULT NULL` recorded as no default,
    `buildTypeString` keeps `ZEROFILL` and goes through `NormalizeType`
  - `parser/introspect/mysql.go`: `mysqlDefault` (M2 / M9), `checkQueryError` (M4),
    CHECK text through `NormalizeExpr` (M7), `normalizeMySQLType` through
    `mysql.NormalizeType` (M5), `viewEnumOrSetType` / `viewColumnSource` (M6)
  - tests: `parser/mysql/mysql_test.go` (`TestIntegerDisplayWidth`, `TestNormalizeType`,
    `TestExpressionText`, `TestQuoteLiteral`, new `TestNormalizeExpr` cases),
    `parser/introspect/mysql_test.go` (`TestMySQLDefault`, `TestCheckQueryError`,
    `TestNormalizeMySQLType` cases), `parser/introspect/mysql_integration_test.go` (new:
    `TestMySQLIntrospect_ProbeFixture`, `_Defaults`, `_TypeCoverage`,
    `_QuotedIdentifiers`, `_CheckText`, `_CheckErrorsSurface`, `_CatalogRewrites`),
    `introspect_integration_test.go` (`TestMySQLIntrospectViews` rewritten, a string-literal
    case in `TestMySQLIntrospect_GeneratedColumn`), `cmd/sqlgen/gen/create_input_default_test.go`
    (new: `TestCreateInput_EmptyStringDefaultIsOmittable`)
  - docs: PRD §6.1 (the MySQL catalog rewrites), `backlog.md`, this section
  - The new integration tests are in their own MySQL-only file, so the merge with 30.4 and
    30.6 stays clean.
- **Fail-first (against `0260df8e`, white-box tests of the new helpers set aside):**
  - **M2 / M9** `TestMySQLIntrospect_Defaults`: introspected `""` (no default) for
    `DEFAULT ''`. The other literals came back bare (`active`, `it's`, `a\b`, `in active`,
    `r,w`, `2020-01-01`, `12:00:00`, `b'101'`). Expressions came back as the catalog wrote
    them: `CURRENT_TIMESTAMP(3)`, `CURRENT_TIMESTAMP`, `(1 + 2)`, and
    `` _utf8mb4\'{}\' `` / `` concat(_utf8mb4\'a\',_utf8mb4\'b\') `` still escaped. The parser
    gave `null` for `DEFAULT NULL`, where the catalog stores no default.
  - **M4** `TestMySQLIntrospect_CheckErrorsSurface`: the run failed only later, at
    `introspect mysql views: context canceled`. The CHECK query's error was dropped.
  - **M5** `TestMySQLIntrospect_TypeCoverage` and `TestIntegerDisplayWidth`: parsed `int(11)`,
    `bigint(20) unsigned`, `smallint(6)`, `tinyint(4)`, `tinyint(1) unsigned`, and `int(5)` /
    `int` for the two ZEROFILL columns.
  - **M6** `TestMySQLIntrospectViews` and `_ProbeFixture`: view columns `enum` / `set`, and no
    enum for the derived-table view.
  - **M7** `TestMySQLIntrospect_CheckText`, `_QuotedIdentifiers` and `TestMySQLIntrospect`:
    raw `check_clause` (`` (`price` >= 0) ``, `-(1)`, escaped `_utf8mb4\'…\'`). The parser
    kept a written `_latin1 'q'`. `TestMySQLIntrospect_GeneratedColumn`'s new case gave
    `concat(_utf8mb4 'n-', qty)`. `TestNormalizeExpr` kept introducers.
- **M5 direction: both paths write the type as MySQL 8.0.19+ stores it.** The catalog
  discards an integer display width, so no other direction can agree. Measured on 8.0.46:
  `INT(11)` → `int`, `BIGINT(20) UNSIGNED` → `bigint unsigned`, `tinyint(4)` → `tinyint`,
  `tinyint(1)` kept, `tinyint(1) unsigned` → `tinyint unsigned`. ZEROFILL keeps the width and
  adds `unsigned`, the default width when none is given: `int zerofill` →
  `int(10) unsigned zerofill`, `tinyint` 3, `smallint` 5, `mediumint` 8, `bigint` 20.
  `mysql.NormalizeType` applies the rule on both paths, so a pre-8.0.19 server's `int(11)`
  agrees too. Side effects in files mode: an `overrides.types` key `int(11)` no longer matches,
  `BIGINT(20) UNSIGNED` resolves to `uint64` (it was `int64`), and the question below.
- **Default rule (Q5), measured on 8.0.46.** `COLUMN_DEFAULT` holds a string, ENUM, SET or
  temporal literal as its bare value (`''` is an empty, non-NULL value), so it is quoted again
  with the parser's encoding (`QuoteLiteral`). `EXTRA` has `DEFAULT_GENERATED` for every
  expression default (`CURRENT_TIMESTAMP` included), and that text is backslash-escaped like
  `generation_expression`. Any other literal is SQL already (`0`, `-1`, `1.50`, `b'101'`,
  `0x6162`) and goes through `NormalizeExpr`. A string column whose default is the text
  `CURRENT_TIMESTAMP` or `NULL` stays a quoted literal, since it has no `DEFAULT_GENERATED`.
- **Measured on the CHECK path: no JSON-operator exception.** vitess writes `doc->>'$.a'` as
  `json_unquote(json_extract(doc, '$.a'))` and `doc->'$.b'` as `json_extract(doc, '$.b')`,
  the same as the catalog after normalization. `TestMySQLIntrospect_CheckText` passes parity
  on both. So PRD §6.1 names only the cast-charset and hex rewrites, plus the literal-default
  conversions measured here: `1.5` → `1.50` on `decimal(10,2)`, `1e3` → `1000`, `TRUE` →
  `1`, `'5'` → `5`, `'ab'` / `x'4142'` on a binary column → `0x6162` / `0x4142`.
  `TestMySQLIntrospect_CatalogRewrites` pins each. They are value-level, which a
  `parityException` field entry cannot express, so the `"mysql"` exception list stays empty and
  the parity fixtures avoid these forms.
- **M6 design.** MySQL 8.0 has no `VIEW_COLUMN_USAGE` (measured: error 1109). The source column
  comes from the stored `view_definition`, read with `mysql.ParseViewSQL`, with table aliases
  resolved. It is accepted only when that table's synthetic type has the same values.
  Otherwise the view gets `<view>_<column>_enum` / `_set`: an expression, a derived table
  (`SELECT d.status FROM (…) d` still reports `enum`), a view over a view, or an unreadable
  SELECT. A `UNION` of ENUM columns reports `varchar`, so it needs no type.
- **`TestMySQLIntrospect` and the probe fixture now pass `assertSchemaParity`** (the 30.1
  baseline's M2 and M7 items are gone). Views are compared on their own, because the file
  parser skips `CREATE VIEW` with a warning.
- **Codegen (M2).** `TestCreateInput_EmptyStringDefaultIsOmittable` builds the table as
  both sources now describe it (`Default: "''"`) and finds the column omittable. The
  generator was already right; the defect was the introspector's `""`.
- **Rulings (user, 2026-10-05):**
  - **MySQL unsigned small integers: add the mappings in this phase.** M5 made files mode
    resolve `SMALLINT(6) UNSIGNED` as `smallint unsigned` → `string`, where it used to fall back
    to `int16`; database mode always gave `string`. PRD §7 and `gotype` now map
    `tinyint unsigned` → `uint8` (`sql.NullInt16`), `smallint unsigned` → `uint16`
    (`sql.NullInt32`), and `mediumint unsigned` / `integer unsigned` → `uint32`
    (`sql.NullInt64`, as `int unsigned`). Each null type is the narrowest that holds the Go type's range,
    the rule `tinyint` → `sql.NullInt16` already follows. A MySQL `… unsigned` type is also
    looked up as `<base> unsigned` before the width is stripped, so a ZEROFILL integer
    (`int(10) unsigned zerofill`) resolves unsigned; it fell back to signed `int32` before. `overrides.types` and integrations use the same keys (`lookupKeys`), so an `int unsigned` override applies to a ZEROFILL `int` and an `int` override is only the last fallback (`TestZerofillOverrideKey`; the follow-up reviewer's probe showed an `int unsigned` override ignored and an `int: int64` override landing on the unsigned column). Ruled 2026-10-05 (user, from `/verify 30.5`): for an integer base the signed key never applies, so an `int` override does not reach a ZEROFILL `int unsigned` column, the same as a plain one.
    PRD §7 has a note under the MySQL table on display widths and ZEROFILL. Fail-first: the
    resolver probe above (`smallint unsigned` → `string`, `int(5) unsigned zerofill` →
    `int32`). Tests: new cases in `TestMySQLTypeMappings` and `TestMySQLSQLNullTypes` (`gotype_test.go`).
    The backlog line is resolved.
  - **PRD §6.1 literal-default conversions: approved as written.**
- **Left open, as a backlog line: no `DEFAULT_GENERATED` before MySQL 8.0.13** (raised by the
  reviewer, not measured). On an older server `mysqlDefault` would quote a
  `CURRENT_TIMESTAMP` default as `'CURRENT_TIMESTAMP'`, with no error. PRD §6.3 sets no
  minimum MySQL version, and design §2 rules a version matrix out of scope.
- **Backlog lines added:** MySQL type synonyms (`BOOLEAN` → `tinyint(1)` changes the Go type
  under `database`, including in the mysql example), the unsigned small-integer gap (since resolved), a
  view over a view's ENUM column, and ZEROFILL on non-integer types.
- **Auto-review (`sqlgen-reviewer`):** PRD 5/6 (M6 partial), tests 6/6. It re-confirmed the M4
  fail-first by reverting the site. Applied:
  - **M6: a view over a view took a type that depended on view name order.** A view sorting
    after its base view reused the base view's own synthetic type, and one sorting before got
    its own. A source now counts only when it names a table in the schema. Fail-first: the new
    `user_states_copy` case in `TestMySQLIntrospectViews` gave `user_states_status_enum`.
  - **ZEROFILL on a non-integer type.** `buildTypeString` wrote `zerofill` there too, which
    turned `DOUBLE ZEROFILL` from `double` into the unmapped `double zerofill` (`string`). It
    now writes `zerofill` on integer types only (`TestIntegerDisplayWidth` "DOUBLE ZEROFILL"),
    and the remaining difference is a backlog line.
  - Its two doc fixes are no longer needed, because the code now matches the docs. The
    view-over-view backlog line is now `[measured]`.
  - Its second question, the pre-8.0.13 defaults, is listed above.
- **`/verify 30.5` (2026-10-05):** 11/12 requirements (§6.1 partial), 6/6 tests, reviewer
  fail-first confirmed. Logged: FIX-268 (tracked; `NOW` / `LOCALTIMESTAMP` defaults and the
  `REGEXP` / `NOT` CHECK rewrites, ruled: normalize the defaults, name the CHECK rewrites in
  §6.1). Ruled and fixed inline: a ZEROFILL integer never takes the signed base's override key
  (`lookupKeys`, `TestZerofillOverrideKey`). One backlog line (views with a column list).

---

## 30.6 SQLite parity (S2, S3, S4, S5, S7, S8, S9, S11, S12)

**PRD Reference:** Sections 5.2, 6.3 (design doc §6.4, Q8, Q9, Q10)

**Status:** Complete

### Tasks

- [x] Q10 strategy: for each table, re-parse `sqlite_master.sql` with `parser/sqlite` and take
  declared constraint names, CHECK constraints and expression text from it. PRAGMAs stay the
  source for columns, types, nullability, PK membership and order, FKs, indexes and partial
  predicates. Fold in 30.3's `sqliteGeneratedExprs`, which re-parses only tables with a
  generated column. **A universal re-parse makes the parser's gaps fatal.** It rejects
  legal DDL (backlog, from 30.1): `PRIMARY KEY ASC AUTOINCREMENT`, `PRIMARY KEY ON
  CONFLICT …`, and `REFERENCES t` with no column list. Measured in 30.3, such a table
  with a generated column already aborts introspection ("parsing stored CREATE TABLE: …
  found 'AUTOINCREMENT'"). Re-parsing every table spreads that to every such table.
  **Ruled 2026-10-05 (user): accepted for now, fix later.** It is documented in PRD §6.3
  and the backlog. rqlite/sql has no newer commit, PR or issue for these constructs.
- [x] S7: replace every Go `%q` PRAGMA argument (`sqlite.go:172,210,339,408,480,632`) with SQL
  identifier quoting (double quotes, with embedded `"` doubled).
- [x] S2: order composite PK columns by `pk` (the stored `pkCol.order`), not `cid`
  (`sqlite.go:252`, `:273`).
- [x] S3: scan `foreign_key_list.to` as `sql.NullString`. ~~A NULL target resolves to the parent's
  PK column(s), as the parser resolves it (`sqlite.go:362`).~~ (Superseded by the 2026-10-05 ruling
  in the Completion Record.) **Measured in 30.1:** the SQLite
  parser rejects `REFERENCES t` with no column list (rqlite/sql: "expected left paren"), at
  column and table level, so there is no parser result to match yet. Settle how this test
  compares before writing it. **Ruled 2026-10-05 (user): the loud error stands, and the
  parent-PK resolution is removed.** The NULL-safe scan stays.
- [x] S4 / Q8: drop `origin = 'pk'` indexes from `introspectUniqueConstraints` (`sqlite.go:406`).
  Take UNIQUE names from the re-parse, so `sqlite_autoindex_*` never surfaces.
- [x] S5 / Q9: skip `type IN ('virtual', 'shadow')` rows from `PRAGMA table_list`. Add a
  `schema.Warnings` line naming each skipped virtual table, on the introspector **and** in the
  SQLite parser for `CREATE VIRTUAL TABLE` (`parser/sqlite/sqlite.go:65-83`).
- [x] S8 / Q10: CHECK constraints come from the re-parse.
- [x] S9 / S11: `AutoIncrement` only for a rowid alias. That means a single `INTEGER PRIMARY KEY`
  column in a rowid table, not `WITHOUT ROWID` and not `DESC`, on the introspector
  (`sqlite.go:266`) and the parser.
- [x] S12: set `WithoutRowID` and `Strict` from `PRAGMA table_list` (or the re-parse).
- [x] Tighten `TestSQLiteIntrospect` (`:358`) through `assertSchemaParity`. **`Type` is compared;
  today it is not.** (30.1 moved it onto the helper, so `Type` is now compared. This task makes
  it pass; its baseline is in the 30.1 Completion Record.)
- [x] Turn the three SQLite-only `TestBothMode*` tests (`:802`, `:867`, `:917`) into tables over
  all three dialects.

### Acceptance Criteria

- The SQLite probe schema parses and introspects to identical schemas through
  `assertSchemaParity`. That includes the FTS5 and R*Tree tables, which are skipped with a
  warning on both paths.
- A table named `Quoted"Name` introspects, and its presence does not abort the run.
- `REFERENCES t` with no column list is an introspection error that names the table. It is
  the parse error the same DDL gives under `files`, as PRD §6.3 says. Before this sub-item it
  crashed the run on a NULL scan. **Amended 2026-10-05 by the user's ruling,** which replaced
  "`REFERENCES t` with no column introspects": the re-parse of every table (Q10) cannot read
  that clause until the parser can.
- `INTEGER PRIMARY KEY DESC` and a `WITHOUT ROWID` `INTEGER PRIMARY KEY` are not auto-increment
  on either path. The measured behavior: the `DESC` insert stores `NULL`, and the
  `WITHOUT ROWID` insert fails NOT NULL.
- `both`-mode overlay, create-conflict and column-conflict tests pass on all three dialects.

### Tests Required

- [x] `TestSQLiteIntrospect_CompositePKOrder` (S2).
- [x] `TestSQLiteIntrospect_ImplicitFKTarget` (S3): column-level and table-level
  `REFERENCES u` fail on both paths with the parse error, and the introspector's error names
  the table.
- [x] `TestSQLiteIntrospect_UniqueAndPKAutoIndexes`: no duplicate PK UNIQUE; inline, named and
  table-level UNIQUE names match the parser (S4).
- [x] `TestSQLiteIntrospect_VirtualTables`: FTS5 + R*Tree skipped with warnings (S5), plus a
  parser test for the `CREATE VIRTUAL TABLE` warning.
- [x] `TestSQLiteIntrospect_QuotedNames`: a table and a column containing `"`, a space and mixed
  case (S7).
- [x] `TestSQLiteIntrospect_Checks`: column, table and named CHECKs match the parser (S8).
- [x] `TestSQLiteIntrospect_RowidAlias`: `INTEGER PRIMARY KEY` (auto-increment);
  `WITHOUT ROWID`, `DESC` and `INT PRIMARY KEY` (not auto-increment), on both paths (S9, S11).
- [x] `TestSQLiteIntrospect_TableFlags`: `WITHOUT ROWID` and `STRICT` (S12).
- [x] `TestSQLiteIntrospect_TypeCoverage`: `INTEGER`, `TEXT`, `REAL`, `BLOB`, `NUMERIC`,
  `VARCHAR(255)`, `BOOLEAN`, `DATETIME`, untyped → `blob`.
- [x] `TestSQLiteIntrospect_Defaults`: a string literal, a numeric literal, `CURRENT_TIMESTAMP`,
  a parenthesized expression, `NULL`. These already match per the probe; pin them.
- [x] `TestSQLiteIntrospect_CompositeConstraints`: composite UNIQUE, a multi-column FK, a
  self-referencing FK.
- [x] `TestBothModeAlterOverlay`, `TestBothModeCreateExisting` and `TestBothModeColumnConflict`
  over `postgres` / `mysql` / `sqlite`.

### Completion Record

- **Completed:** 2026-10-05 (`/implement 30.6`, then the S3 follow-up on the tree merged with
  30.4 and 30.5).
  - The full `parser/introspect` integration suite passes on the merged tree, `-race`, with
    PG 16 and MySQL 8.0 containers. That includes `TestPostgresIntrospect` and
    `TestMySQLIntrospect`.
  - `parser/sqlite` passes. `TestE2EGoldenFiles` passed before the merge, so no golden changed.
  - The orchestrator ran `make check-examples` on the merged tree.
- **Files:**
  - `parser/introspect/sqlite.go`: tables come from `PRAGMA table_list` (`wr`, `strict`,
    `type`) joined to `sqlite_master.sql`. Each table's stored `CREATE TABLE` is parsed once
    (`parseStoredTable`), and that gives the CHECKs, the generated expressions and the PK /
    FK / UNIQUE names (`nameDeclaredConstraints`). One `PRAGMA index_list` read per table
    (`indexList`) feeds `introspectIndexes`, which replaces the separate unique and non-unique
    passes. `quoteSQLiteIdent` is on every PRAGMA. `foreign_key_list.to` is scanned as
    `sql.NullString`. `normalizeSQLiteType` is gone; types go through `sqlite.NormalizeType`.
  - `parser/sqlite/sqlite.go`: `CREATE VIRTUAL TABLE` warning (`VirtualTableWarning`, shared
    with the introspector so both sources warn alike); `rowIDAlias` replaces the per-column
    AutoIncrement rule and `isRowIDAlias`; `NormalizeType` (exported, shared with the
    introspector); `indexedColumnNames` drops a `COLLATE`.
  - tests: `parser/introspect/introspect_integration_test.go` (the twelve tests above,
    `TestSQLiteIntrospect_ParityFixture`, the `applySQLite` / `introspectSQLite` /
    `sqliteSchemas` helpers, the both-mode tables); `parser/introspect/parity_fixtures_test.go`
    (`sqliteParityTables`, the fixture without its view); `parser/introspect/sqlite_test.go`
    (`TestQuoteSQLiteIdent`; `TestNormalizeSQLiteType` moved to `parser/sqlite` as
    `TestNormalizeType`); `parser/sqlite/sqlite_test.go` (`TestRowIDAlias`,
    `TestCreateVirtualTableSkipped`, `TestIndexedColumnCollation`,
    `TestUniqueDuplicatesFollowCollation`, `TestNormalizeType`)
  - docs: PRD §5.2 (a table-level `PRIMARY KEY (id)` over one INTEGER column is a rowid alias),
    PRD §6.3 (type normalization; every table is re-parsed, so each parser gap is an
    introspection error); `backlog.md` (three lines)
- **Fail-first evidence (against HEAD `0260df8e`):**
  - **S7:** `TestSQLiteIntrospect_QuotedNames` and `TestSQLiteIntrospect_ParityFixture`: the
    whole run aborted with `introspect sqlite columns Quoted"Name: SQL logic error: near
    "Name": syntax error (1)`.
  - **S2:** `TestSQLiteIntrospect_CompositePKOrder`: `pairs` PK `[a b]` (want `[b a]`), `kv`
    `[k ns]` (want `[ns k]`).
  - **S3:** a `REFERENCES u` table crashed the run with `introspect sqlite foreign keys p:
    scanning: sql: Scan error on column index 4, name "to": converting NULL to string is
    unsupported`. `TestSQLiteIntrospect_ImplicitFKTarget` expects `introspect sqlite table p:`
    and the parse error, so it fails against that message.
  - **S4 / Q8:** `TestSQLiteIntrospect_UniqueAndPKAutoIndexes`: `sqlite_autoindex_*` UNIQUEs on
    `tpk`, `ipk`, `dpk` and `cpk` (the PK's own index), and `u`'s four constraint UNIQUEs named
    `sqlite_autoindex_u_1…4` with `Method: btree` where the parser has the declared name (or
    none) and `Method: ""`. `TestSQLiteIntrospect` failed on `sqlite_autoindex_users_1` (the
    30.1 baseline).
  - **S5 / Q9:** `TestSQLiteIntrospect_VirtualTables`: introspected tables `boxes`,
    `boxes_node`, `boxes_parent`, `boxes_rowid`, `docs`, `docs_fts`, `docs_fts_config`, … and
    no warnings. `TestCreateVirtualTableSkipped` (parser): warnings `nil`.
  - **S8:** `TestSQLiteIntrospect_Checks`: no CHECK introspected.
  - **S9 / S11:** `TestSQLiteIntrospect_RowidAlias`: `WITHOUT ROWID` introspected
    AutoIncrement; `descending key` AutoIncrement on both paths; `INT PRIMARY KEY` parity
    failed on the auto-index UNIQUE. `TestRowIDAlias` (parser): `descending key` and `UNIQUE on
    a descending key is dropped` were AutoIncrement, and the latter kept a UNIQUE SQLite never
    builds (measured: `id INTEGER PRIMARY KEY DESC UNIQUE` has one index, origin `pk`).
  - **Also found by `TestRowIDAlias` / `_RowidAlias`:** a table-level `PRIMARY KEY (id)` over
    an INTEGER column is a rowid alias (measured: no index; the insert gets an id), but the
    parser did not mark it AutoIncrement. Fixed by the same `rowIDAlias` rule.
  - **S12:** `TestSQLiteIntrospect_TableFlags`: `wr`, `st`, `both_flags` introspected with both
    flags false.
  - **Type parity (found here, fixed inline):** `TestSQLiteIntrospect_TypeCoverage`:
    `DECIMAL(10, 2)` parsed `decimal(10,2)`, introspected `decimal(10, 2)`; `UUID` parsed
    `blob`, introspected `uuid`. PRD §6.1 requires the same types from both sources, so the
    introspector applies the parser's rule (`sqlite.NormalizeType`). `TestNormalizeType` covers
    the rule. No example uses an unrecognized SQLite type.
  - **COLLATE (found here, fixed inline):** `TestIndexedColumnCollation`: the parser named the
    columns of `UNIQUE (email COLLATE NOCASE)` and `CREATE INDEX … (name COLLATE NOCASE DESC)`
    `"email" COLLATE "NOCASE"` and `"name" COLLATE "NOCASE"`, where the catalog reports `email`
    and `name`. With UNIQUE names now matched by columns, that table failed introspection.
  - `TestSQLiteIntrospect_CompositeConstraints`: auto-index names and a duplicate PK UNIQUE on
    `parent` (S4).
  - **Passed at HEAD (pins):** `TestSQLiteIntrospect_Defaults`, and the three `TestBothMode*`
    tables on all three dialects. No PG or MySQL defect showed up in the both-mode cases.
- **Rowid alias, measured.** The introspector's rule is the catalog's own: a single-column
  primary key with no `origin = 'pk'` index. `INT`, `INTEGER(10)`, `DESC` and `WITHOUT ROWID`
  keys all have one; `INTEGER PRIMARY KEY`, `ASC`, `AUTOINCREMENT` and table-level `PRIMARY KEY
  (id)` do not. `TestSQLiteIntrospect_RowidAlias` also inserts a row without `id` and pins what
  SQLite does: assigned for the aliases, `NULL` for `DESC` and `INT`, NOT NULL failure for
  `WITHOUT ROWID`.
- **Constraint names.** A PRAGMA-built PK, FK or constraint UNIQUE takes the name of the
  re-parsed constraint over the same columns (case-insensitive; an FK also matches its parent
  table). One with no match is an error ("stored CREATE TABLE declares no UNIQUE over (…)"),
  since it means the parser and the catalog disagree. A constraint UNIQUE has `Method: ""`, as
  parsed (PRD §5.2 defaults `btree` on PostgreSQL and MySQL only); a `CREATE UNIQUE INDEX` keeps
  its name and `btree`, as parsed. That settles the 30.2 note on which side moves.
- **Virtual tables.** A virtual table the table filters leave out is not warned about
  (`TestSQLiteIntrospect_VirtualTables`); the parser has no filters and warns for every one.
- **The fixture's view.** The parser skips `CREATE VIEW` with a warning and the introspector
  reads the view (PRD §16), so `TestSQLiteIntrospect_ParityFixture` compares
  `sqliteParityTables`, the fixture without `s_v`. `TestParityFixtures` still applies and parses
  the whole fixture.
- **S3, ruled 2026-10-05 (user): accept the loud error and remove the code that cannot run.**
  - The acceptance criterion said `REFERENCES t` with no column list introspects. But Q10 sends
    every table's stored DDL through rqlite/sql, which rejects that clause, so the two could not
    both hold.
  - Introspecting `p (…, uid INTEGER REFERENCES u)` now fails with `introspect sqlite table p:
    parsing stored CREATE TABLE: parsing sqlite_master: 1:65: expected left paren, found ')'`.
    That is the error the files path gives (PRD §6.3).
  - The acceptance criterion is amended to match.
  - The parent-PK resolution (`resolveImplicitTargets`, `primaryKeyColumns`) and its white-box
    test `TestSQLiteForeignKeysImplicitTarget` are removed.
  - **The NULL-safe scan of `to` stays, but it is unreachable too.** Only `REFERENCES t` with no
    column list leaves `to` NULL, and the stored-DDL parse rejects that table before foreign
    keys are read. The `sqliteFKRow` comment says so.
- **Auto-review (`sqlgen-reviewer`):** 1 issue, 1 question (the dead resolution code, settled by
  the S3 ruling above). It reproduced the S4 fail-first by deleting the `origin == "pk"` skip.
  - **Fixed: collation-distinct UNIQUEs.** With `COLLATE` dropped from column names,
    `dropDuplicateUniques` merged `UNIQUE (a), UNIQUE (a COLLATE NOCASE)` into one, where
    SQLite builds two. Introspection then failed with "declares no UNIQUE over (a)".
    `id TEXT PRIMARY KEY, UNIQUE (id COLLATE NOCASE)` failed the same way.
    - The duplicate key is now the columns plus each column's effective collation: the
      explicit one, else the column's `COLLATE`, else BINARY, compared case-insensitively.
    - Measured on modernc SQLite, as two indexes or one: `(a)` + `(a COLLATE NOCASE)` 2;
      `id TEXT PRIMARY KEY` + `(id COLLATE NOCASE)` a key index and a UNIQUE; `(a)` +
      `(a DESC)` 1; a NOCASE column with `(a)` + `(a COLLATE nocase)` 1; a NOCASE column
      `UNIQUE` + `(a COLLATE BINARY)` 2; a NOCASE `PRIMARY KEY` + `UNIQUE (id)` key index
      only; `(a, b)` + `(a COLLATE BINARY, b)` 1.
    - Pinned by `TestUniqueDuplicatesFollowCollation` (parser) and five `coll_*` tables in
      `_UniqueAndPKAutoIndexes`. HEAD itself was not run against these cases. It kept the
      collation in the column name, so it kept a UNIQUE whose collation matched only after
      normalizing case or the default (`nocase`, `BINARY`).
  - Applied:
    - the §6.3 type sentence moved to its own "SQLite column types" bullet
    - a column containing `"` (with a CHECK on it) added to `_QuotedNames`
    - a measured backlog line for RENAME COLUMN and CHECK text
  - The orchestrator applied the `docs/design/INTROSPECTION.md` §6.4 fix (`TestNormalizeType`
    in `parser/sqlite`) after the merge.
- **`/verify 30.6` (2026-10-05):** 10/11 requirements (FK name matching partial), 12/12
  tests, reviewer fail-first confirmed. Logged: FIX-267 (tracked; FK names swap when two FKs
  share columns and parent) and FIX-269 (tracked; partial-index `Where` text, ruled: re-parse
  the stored index DDL). PRD §6.3 now names `ON CONFLICT` on any constraint as DDL the parser
  cannot read. Three backlog lines.

---

## 30.7 Connection string + PRD §4.3 validation

**PRD Reference:** Sections 4.2, 4.3, 4.13

**Status:** Complete

### Tasks

- [x] Add `cmd/sqlgen/cli/connection.go` with `connString(dialect config.Dialect,
  c *config.ConnectionConfig) (string, error)`:
  - `url` set → returned verbatim
  - postgres → `postgres://user:pass@host:port/db?sslmode=<ssl_mode | disable>`, built with
    `net/url`
  - mysql → `user:pass@tcp(host:port)/db?tls=<ssl_mode | false>`
  - sqlite → `database`
  - the port defaults to 5432 / 3306
  - a missing required field → an error naming `input.connection.<field>`
- [x] `config/validate.go`, next to `validateConnectionRequired` (`:1125`): when `url` is
  empty, `database` is required on every dialect, and `host` and `user` are required on postgres
  and mysql. Report every missing field together. `SQLGEN_DB_URL` (applied at `config.go:1981`)
  satisfies the rule. `port` is not required (PRD §4.3 default, synced in 30.0). Update the
  `port` description in `cmd/sqlgen/config/schema/v1.json` (`:139`, still "Required for
  postgres/mysql") to match, and the `paths` / `introspect` descriptions to note the warning.
- [x] Ignored input keys warn (PRD §4.13 "Input key the source does not read"; design doc Q11
  addendum). An explicitly set `input.paths` under `source: database`, or any
  `input.introspect` field under `source: files`, warns and names the key and the source.
  Telling an explicit `paths` from the `["."]` default needs presence tracking at YAML decode.
  An explicit `paths: ["."]` warns; the default does not.

### Acceptance Criteria

- Every PRD §4.3 "Required … when `url` is empty" rule is enforced by `sqlgen validate`, exit 2.
- `url` takes precedence over the individual fields. `SQLGEN_DB_URL` and `SQLGEN_DB_PASSWORD`
  keep their §4.3 precedence.
- A password containing `@`, `:`, `/` or `%` round-trips through the postgres URL.

### Tests Required

- [x] `TestConnString` (table-driven, three dialects): `url` precedence, password escaping, the
  `ssl_mode` defaults, port defaults, each missing-field error.
- [x] Config validation tests: each missing field per dialect; all reported together; `url` set
  skips the rule; `SQLGEN_DB_URL` alone satisfies it.
- [x] Ignored-key warning tests: `paths` explicit / defaulted under `database`; `introspect`
  set / unset under `files`; neither warns under `both`.

### Completion Record

- **Completed:** 2026-10-06 (`/done 30.7`); implemented 2026-10-05 (`/implement 30.7`). Not yet
  committed. `make check` passed after the last change. `make check-examples` passed after the
  last `config/validate.go` change; the MySQL socket change touched only `cli/connection.go` and
  docs, which are not on the trigger list.
- **Files:**
  - `cmd/sqlgen/cli/connection.go` (new): `connString`.
  - `cmd/sqlgen/config/config.go`: `(*ConnectionConfig).Errors(dialect)`, the single source of
    the §4.3 field rules (required fields, and no `:` in a MySQL `user`), shared by `connString`
    and validation;
    `InputConfig.pathsSet`, set in `decodeConfig` from a second, non-strict decode of
    `input.paths` into a `yaml.Node` (key present = `Kind != 0`, so `paths: []` and `paths: ~`
    count as explicit).
  - `cmd/sqlgen/config/validate.go`: `validateConnectionRequired` adds one error per problem,
    collected with the other config errors by `errors.Join`.
    `validateIgnoredInputKeys` adds the warnings.
  - `cmd/sqlgen/config/schema/v1.json`: `host` (Unix socket: a directory on postgres, a
    file on mysql), `port` (dialect default, ignored on SQLite and with a MySQL socket file), `user` (no `:` on mysql), `database` (required when `url` is empty), and
    the `paths` / `introspect` descriptions.
  - `docs/PRD.md` §4.3, `docs/configuration.md` and design doc §7.1: the `host`, `port` and `user` rows
    (ruled with the user 2026-10-06; see below).
  - Tests: `cli/connection_test.go` (new), `config/validate_test.go`.
- **Decisions within the spec:**
  - An `introspect` field counts as set when its list is non-empty. `introspect: {}` or an empty
    list changes nothing on any source, so it doesn't warn. `paths` uses key presence, as the
    task requires.
  - The required-field rule runs only under `database` / `both`. A connection block under
    `files` is never read and is not validated.
  - MySQL: the password goes into the DSN raw, and the database name is `url.PathEscape`d and
    `ssl_mode` is `url.QueryEscape`d, since go-sql-driver unescapes both.
- **Driver probe** (throwaway module in the scratchpad, pgx v5.9.1 and go-sql-driver v1.9.3,
  the parser module's versions). `pgconn.ParseConfig` and `mysql.ParseDSN` both return the
  password `p@ss:w/rd%?#` intact from the strings `TestConnString` pins. pgx reads the
  bracketed IPv6 host `::1`. go-sql-driver reads `my%2Fdb` as `my/db` and `tls=false` as
  `"false"`.
- **Ruled with the user 2026-10-06** (the PG socket and MySQL `user` rules came from the
  auto-review and the MySQL socket rule followed; all three fixed inline rather than backlogged,
  and each test failed before its fix):
  - A PG `host` starting with `/` is a Unix-socket directory, as in libpq. It goes in
    `?host=<dir>&port=<port>`, because `net.JoinHostPort` gave `%2Fvar%2Frun%2Fpostgresql:5432`
    and pgx rejected it (`invalid URL escape "%2F"`). Probe: `pgconn.ParseConfig` reads the new
    form back as host `/var/run/postgresql`, with the port, database and an escaped password
    intact. PRD §4.3 states the rule.
  - A MySQL `host` starting with `/` is the socket file and gives `unix(<file>)`, with no port.
  - A MySQL `user` containing `:` is a config error, with or without a password. Probe:
    go-sql-driver read `ap:p:secret@…` as user `ap`, password `p:secret`, which fails as a
    misleading auth error. The DSN has no escape for it, so `url` is no workaround and the check
    applies only when sqlgen builds the DSN. PRD §4.3 states the rule.
  - **Live socket probe**, run inside MySQL 8.0 and postgres:16-alpine containers with a static
    linux/arm64 build of a probe that opens each driver and runs `SELECT current_user`:
    - The MySQL `unix(/var/run/mysqld/mysqld.sock)` DSN that `TestConnString` pins connected as
      `app@localhost`, with the password `p@ss:w/rd%?#`. A wrong password got `Error 1045`, so
      authentication really ran. The old `tcp(…sock:3306)` form failed with `no such host`.
    - The PG `?host=%2Fvar%2Frun%2Fpostgresql&port=5432` URL connected as `app`. Port 6543
      failed on `.s.PGSQL.6543`, so the port picks the socket file and has to be passed.
    - `tls=true` over the MySQL socket fails at connect, because the driver needs a server name
      for TLS. It's loud and only happens when TLS is requested, so `ssl_mode` passes through
      unchanged.
- **Tests:**
  - `TestConnString`: 21 cases across the three dialects.
  - `TestConnString_PostgresPasswordRoundTrips`: 8 passwords.
  - `TestConnString_PostgresSocketRoundTrips`.
  - `TestValidate_ConnectionFieldsMissing`: CLI test; exit 2, all three fields named.
  - `TestValidatePreParse_ConnectionFields`: 15 cases.
  - `TestLoadConfig_SQLGENDBURLSatisfiesConnectionFields`: 3 cases.
  - `TestValidatePreParse_IgnoredInputKeys`: 11 cases.
- **Lint:** gosec G101 flags the three postgres fixtures that put a password in a URL. They carry
  `//nolint:gosec // test fixture, not a real credential`.
- **Security review:** done by the `/verify 30.7` reviewer. Errors name fields and never repeat
  a user value or password. `connString` builds a string and doesn't connect; 30.8 adds the
  connection.
- **`/verify 30.7` (2026-10-06):** PRD 10/10, tests 3/3, `make check` and `make check-examples`
  passed. The reviewer confirmed fail-first by disabling the MySQL `:` check, which failed
  three cases. No FIX entries. Six tracker-text corrections applied. Two questions went to the
  user: `SQLGEN_DB_PASSWORD` next to a `url` (the `url` wins and the password is dropped), and
  whether ignored connection fields should warn like `paths` and `introspect`.
- **Auto-review** (`sqlgen-reviewer`): PRD 8/8, tests 3/3, fail-first confirmed (a value check in
  place of key presence fails `database_with_empty_paths`). 0 high-confidence issues.
  - Inline fixes applied: two stale line references in the task text, a shorter `pathsSet`
    comment, and a `paths: ~` case.
  - Two optional findings (a MySQL `user` containing `:`, PG Unix-socket hosts) were fixed
    inline after a ruling; see above.
  - Note for 30.8 from the reviewer: `*connectionError` should wrap the driver error, never the
    built string, which carries the password.

---

## 30.7a `${VAR}` references in `sqlgen.yml` values

**PRD Reference:** Sections 4.1, 4.3, 4.13, 4.14, 23.9

**Design:** `docs/design/CONFIG_ENV.md` (DRAFT 2026-10-06, all five questions ruled in its §9).
The 32-case prototype that verified §3–§5 is in the gitignored session scratchpad and isn't
needed: §7 of the doc lists every case.

**Status:** Complete

### Tasks

- [x] PRD sync (design doc §8). Add a new §4.1.1 "Environment variable references" covering the
  syntax, semantics and redaction (doc §3, §4, §5.1). Add the §4.3 url/password note and the
  URL-escaping guidance (doc §5.3). Add §4.13 error rows for an unset variable, an invalid
  reference and an unterminated reference (exit 2), and the warning row for an ignored
  `SQLGEN_DB_PASSWORD`. Add a §4.14 note that the schema flags `${…}` in integer and boolean
  fields. Update `docs/configuration.md` and the `url` / `password` descriptions in
  `cmd/sqlgen/config/schema/v1.json`.
- [x] Add `cmd/sqlgen/config/expand.go`:
  - The reference parser: `${NAME}` with `NAME` = `[A-Za-z_][A-Za-z0-9_]*`. `$${` is a literal
    `${`; any other `$` is kept. Invalid and unterminated references are errors.
  - The node walk: expand scalar values in place and never keys. Record each reference's line,
    dotted path (`[i]` for sequence items), original text and the variables it used.
  - Plain scalars are re-typed from the expanded text, except that null is kept as `!!str`.
    Quoted and block scalars stay strings.
  - Unset variables are looked up with `os.LookupEnv`. Every unset or invalid reference is
    reported, each as `line N: <path>: <problem>`.
- [x] Rework `decodeConfig` (`config/config.go:1658`) as design doc §6.1 describes:
  - A config with no references takes today's single strict decode.
  - With references, keep only the `not found in type` errors from the strict decode of the raw
    bytes. Keep every error from `Node.Decode` of the expanded tree, redacting type errors on
    reference lines (doc §5.1). Merge by line.
  - Update the "Known gap" comment (now says the gap holds on both decode paths).
- [x] In `Operations.UnmarshalYAML` and `NullableVariant.UnmarshalYAML` (`config.go:564`,
  `:630`), return a `*yaml.TypeError` from the inner `Decode` unwrapped (doc §6.1 step 4).
- [x] Ignored-`SQLGEN_DB_PASSWORD` warning (doc §9 Q4) in `config/validate.go`. Under
  `source: database | both`, warn when the password variable is set, a `url` wins, and that
  `url` (`SQLGEN_DB_URL`, or `input.connection.url` without a `${SQLGEN_DB_PASSWORD}` reference)
  ignores it.

### Acceptance Criteria

- `url: postgres://app:${SQLGEN_DB_PASSWORD}@host/db` reaches `connString` with the password
  filled in. The design doc §1 config without the reference warns.
- An unset variable fails `generate` and `validate` with exit 2, naming the line, the path and
  the variable. Every unset variable is reported together.
- No error message contains an expanded value.
- An unknown key is still reported at its real line in a config with references, including
  one whose references sit inside an `operations` or `nullable` mapping.
- A config without references decodes exactly as before. Existing tests and `make
  check-examples` pass unchanged.

### Tests Required

- [x] Table-driven expansion and decode tests (`config/expand_test.go`) covering every
  design doc §7 row. Each case runs fail-first against HEAD where HEAD has a behavior to
  contrast.
- [x] The no-secret-in-errors assertion across the error cases.
- [x] The unmarshaler-wrapping regression: an unknown key next to a typed reference in an
  `operations` mapping.
- [x] Warning tests for doc §9 Q4: the config `url` without the reference warns; with it, no
  warning; `SQLGEN_DB_URL` plus the password warns; individual fields don't warn; `source: files`
  doesn't warn.
- [x] A CLI test: `sqlgen validate` with an unset variable exits 2 and names it.

### Completion Record

- **Completed:** 2026-10-06 (`/done 30.7a`); implemented the same day (`/implement 30.7a`).
  `make check` passed after the last change. `make check-examples` passed before the review fixes,
  which touched only `config/expand.go` and `config/config.go`, neither on the trigger list.
- **Files:**
  - `cmd/sqlgen/config/expand.go` (new): `expandValue` (the reference parser), `expandEnv`
    (the node walk, which records each expanded scalar's line, path and original text, and the
    variables per path, alias paths included), `decodeExpanded` (the two decodes merged by line)
    and `expansion.redact`.
  - `cmd/sqlgen/config/config.go`: `decodeConfig` expands first and takes the single strict
    decode when nothing expanded. The two unmarshalers return a `*yaml.TypeError` unwrapped.
    `applyEnvOverrides` sets `ConnectionConfig.passwordEnvIgnoredBy`, and `RootConfig.envVars`
    keeps the per-path variables.
  - `cmd/sqlgen/config/validate.go`: `validateIgnoredPasswordEnv`.
  - `cmd/sqlgen/config/schema/v1.json`: the `url` and `password` descriptions.
  - `docs/PRD.md`: new §4.1.1, a §4.3 note and secrets paragraph, four §4.13 rows, a §4.14
    paragraph. `docs/configuration.md`: an "Environment variables" section and the `connection`
    note.
  - Tests: `config/expand_test.go` (new), `config/validate_test.go`, `cli/connection_test.go`.
- **Decisions within the spec:**
  - Expansion errors print as `parsing config: environment variable references:` followed by
    one indented `line N: <path>: <problem>` per reference, the layout of yaml.v3's own
    `unmarshal errors:` list.
  - The two-pass decode runs when any scalar changed, so a `$${` escape alone takes it too. A
    `${` only in a key or a comment changes nothing and takes the strict decode.
  - The raw strict pass keeps only `not found in type` errors. A non-type error from it is
    dropped, and the expanded pass reports its own.
  - Redaction rewrites only `cannot unmarshal` messages on a reference line. Duplicate-key and
    unknown-key messages quote keys, which are never expanded. A line holding several
    references names all of them.
  - The Q4 warning names where the winning `url` came from. With `SQLGEN_DB_URL` it suggests
    putting the password there or unsetting it, because writing the reference into the config
    `url` alone does nothing while `SQLGEN_DB_URL` replaces it.
  - The parser scans left to right, so `$$${X}` is `$` then the `$${` escape, giving `$${X}`.
- **Tests:**
  - `TestLoadConfig_EnvReferences`: 17 cases.
  - `TestLoadConfig_EnvReferenceErrors`: 20 cases. Each one asserts that no expanded value, or
    its 7-character yaml.v3 prefix, appears in the error.
  - `TestValidatePreParse_IgnoredPasswordEnv`: 7 cases.
  - `TestValidate_UnsetEnvReference`: CLI, exit 2, names the line, path and variable.
  - Every design doc §7 row has a case, except URL escaping, which the doc checked
    separately with `net/url`.
- **Fail-first** (the new tests copied onto a HEAD worktree): 32 of 41 config cases and the CLI
  test fail. The 9 that pass have no HEAD behavior to contrast: `$` outside a reference, the
  flow-sequence parse error, an unknown key at its real line, a duplicate key, a type error away
  from a reference, and the four no-warning rows. With the implementation but without the
  unmarshaler unwrap, the two `operations` mapping cases fail.
- **Auto-review** (`sqlgen-reviewer`): PRD 6/8 before fixes, tests 5/5, fail-first confirmed
  (reverting the `Operations` unwrap silently accepts the typo). Two high-confidence issues,
  both from an explicit YAML tag on a reference, both reproduced:
  - `port: !!int ${P}` put the whole value in the error, because yaml.v3 fails a tag mismatch
    with a plain error that `redact` never sees.
  - The same tag made the raw strict pass abort with a non-type error, which was dropped along
    with every unknown-key error, so `input.typo` was silently accepted.
  - **Ruled with the user 2026-10-06: reject.** A reference in a value tagged anything but `!!str`
    is a reference error, reported before decoding (PRD §4.1.1 sentence, §4.13 row). The raw
    pass now returns a non-type error instead of dropping it; it quotes only file text. The new
    `explicitly tagged reference` case fails without the rejection.
  - Applied inline: `redact` leaves a `!!seq` / `!!map` message alone, since it quotes no value
    and the culprit is that node (new case). The PRD, `configuration.md` and STATUS now
    say no *config-load* error carries an expanded value, because later validation messages
    such as `field_casing` quote values. The §4.13 warning row mentions the `SQLGEN_DB_URL` hint.
    The task line references are updated, and the "Known gap" comment now says it holds on both
    decode paths.
  - Not covered by a test: the `NullableVariant` unwrap (the reviewer's probe shows it works).

---

## 30.8 `parseSchema` wiring — `source: database | both`

**PRD Reference:** Sections 4.2, 4.4, 6.1, 6.4, 16.2

**Status:** Complete

### Tasks

- [x] Add `introspectSchema(ctx, cfg) (*parser.Schema, error)` in `cmd/sqlgen/cli`:
  - picks `introspect.New{Postgres,MySQL,SQLite}Introspector()` by dialect
  - builds the connection string with `connString`
  - maps `input.introspect.{schemas, exclude_schemas, tables}` → `IntrospectionOptions`
    (top-level `exclude_tables` stays where it is applied today)
  - `defer`s `Close()`
  - wraps connect, ping and query failures in `*connectionError` (introduced here, consumed by
    30.9)
- [x] `parseSchema(ctx, cfg)` (`cli/pipeline.go:170`):
  - `files` is unchanged
  - `database` introspects only and ignores `paths`
  - `both` introspects into `base`, then calls `introspect.Merge(base, newSeedableParser(…),
    files)` and takes `p.Schema()`
- [x] Refactor `parseViewFiles` (`pipeline.go:272`) to return the annotated views and the
  annotation map. Under `database | both`, combine them with `parser.MergeViewAnnotations`
  (`parser/view.go:449`). Under `files`, keep the append.
- [x] Run the PG `parser.UnresolvedForeignKeys` check on the final (merged or introspected)
  schema. This is the ordering FIX-243 Q3(a) deferred. Only keys the files add are reported
  (ruling in the Completion Record).
- [x] Thread `ctx` (`cmd.Context()`) through the three callers: `pipeline.go:47`
  (`loadAndValidate`), `generate.go:45`, `validate.go:48`.

### Acceptance Criteria

- `source: database` generates from the live database only. `source: both` generates the
  introspected schema with the file overlay applied (PRD §6.1 merge table).
- An introspected view with an annotation file yields one view with the annotation applied
  (§16.2).
- A file FK into a table that exists only in the database resolves under `both`.
- `introspect.*` filters narrow the introspected set as §4.4 / §6.4 describe.

### Tests Required

- [x] Unit tests with SQLite file databases (no container), covering:
  - `database` ignores `paths`
  - `both` applies `ALTER TABLE … ADD COLUMN` and a new `CREATE TABLE`
  - a `CREATE TABLE` of an existing table is an error
  - view annotation merging gives one view
  - an `introspect.tables` filter
- [ ] PG-only check with the integration container: a file FK into a DB-only table resolves under
  `both`. Moved to 30.10's suite (listed there).

### Completion Record

- **Files:**
  - `cli/connection.go`: `connectionError`, `introspectSchema` and `sqliteFileExists`.
  - `cli/pipeline.go`: `parseSchema(ctx, cfg)` with the `database` / `both` branches, the
    overlay-only FK check, and `parseViewFiles` returning views plus annotations.
    `newParser` now returns `introspect.Seedable`, so `both` needs no separate constructor.
  - `cli/validate.go`, `generate.go`, `diff.go`, `lint.go`, `graphql.go`: pass `cmd.Context()`.
  - `parser/view.go`: `mergeView` fix (below).
  - `docs/PRD.md` §4.3 and §5.5, `docs/configuration.md`.
- **Ruled with the user 2026-10-06: the PG FK check covers only keys the files add.** The PG
  introspector keeps an FK whose target `introspect.tables` / `schemas` filtered out
  (`postgres.go` constraint query), so checking the whole introspected schema would have failed
  every filter that cuts an FK. `parseSchema` drops the unresolved FKs the introspected schema
  already had (by message, read before `Merge`, which mutates its base). Under `database`
  nothing is reported; under `both` a file FK into a missing table is still an error, and one into
  a DB-only table resolves. The relationship into a filtered-out table is omitted, like one into an
  `exclude_tables` table. PRD §5.5 states the rule. Not testable without PG; added to 30.10.
- **SQLite missing file (probe).** modernc opens with `READWRITE|CREATE`, so a missing path
  created an empty database and `source: database` generated nothing, with exit 0. Probe of the
  introspector: missing file → no error, 0 tables, file created; missing directory, a directory,
  an unreadable file and a non-database file all fail at ping; `file:…?mode=ro` fails. Design
  §7.4 wants a missing file to exit 4, so `introspectSchema` stats a plain path (the part before
  `?`) and returns a `*connectionError`. A `file:` URI is left to its `mode`. PRD §4.3 and
  `configuration.md` state it.
- **`mergeView` dropped `@type` (fixed inline).** It set `Column.Type` to the Go type but not
  `GoTypeLiteral` / `GoTypeImport`, which `BuildView` sets and the generator reads
  (`gen/context_view.go:215`). Only tests called it before. The new assertion in
  `TestMergeViewAnnotations_AnnotationWins` failed first.
- **`validate` skips introspection when pre-parse rejected the connection block.** Otherwise
  `connString` repeated each missing-field error. `TestValidate_ConnectionFieldsMissing` now
  asserts each appears once; it failed with the guard removed.
- **Every `Introspect` error is a `*connectionError`.** That includes a stored SQLite
  `CREATE TABLE` the file parser cannot read (PRD §6.3) and a catalog-normalization failure,
  which arrive from the introspector as plain errors. Decided in 30.9: they exit 3, through
  `introspect.SchemaError`.
- **Tests** (`cli/introspect_test.go`, SQLite files, no container):
  - `TestParseSchema_SQLiteDatabaseSource`: `database` ignores `paths`; `both` applies `ADD COLUMN`
    and a new `CREATE TABLE`; `introspect.tables` narrows.
  - `TestParseSchema_BothSourceCreateOfExistingTable`: error, not a `*connectionError`.
  - `TestParseSchema_IntrospectedViewWithAnnotation`: one view, `@pk` and `@type` applied.
  - `TestParseSchema_SQLiteMissingDatabaseFile`: `*connectionError`, no file created.
  - `TestGenerate_SQLiteDatabaseSource`: end to end through `generate`.
  - `TestParseSchema_SQLiteFileURI`: a `file:…?mode=ro` URI introspects.
  - `TestParseSchema_MalformedPostgresURLHidesPassword`: 2 passwords.
  - Fail-first, each fix removed in turn: append instead of merge (2 views), no stat (nil error),
    no validate guard (duplicate lines), old `mergeView` (empty `GoTypeLiteral`).
- **Auto-review** (`sqlgen-reviewer`), fail-first spot check confirmed. Two issues, both fixed
  inline, each test failing first:
  - **A password leaked through a pgx parse error.** pgx masks the URL but keeps
    `url.Parse`'s message, so a `url` whose literal password holds `/` or `%` printed
    `invalid port ":Sekr3t"` or `invalid URL escape "%zz"`. `introspectSchema` now parses a
    `postgres://` URL first and returns a fixed message on failure. MySQL errors did not leak.
  - **A `file:` URI skipped the missing-file check**, so `file:/x/a.db` created an empty
    database. `sqliteFileExists` now stats a `file:` URI's path too, unless it sets `mode`.
    The PRD §4.3 / `configuration.md` sentence names the `mode` exception.
  - Line references corrected in the 30.8 and 30.9 task text.
- **Reviewer questions, ruled with the user 2026-10-06** (all three as recommended):
  - **A file FK into a table `introspect` leaves out stays unresolved under `both`.** A
    filtered-out table is not in the schema at all, so a file's `ALTER TABLE` on it fails too
    (probe: `table posts not found`). PRD §5.5 now says "a table the introspected schema
    includes". PRD §6.1 now says `both` takes only the migrations the database has not run, and
    that an overlay changes only introspected tables. Probe: `ALTER … ADD COLUMN` and
    `DROP TABLE` + `CREATE TABLE` both apply; an applied migration's `CREATE TABLE` fails.
  - **`@type` nullability matches `BuildView`.** `mergeView` makes a `@type` column NOT NULL,
    then applies `@nullable`, then `@pk`. `TestParseSchema_IntrospectedViewWithAnnotation` failed
    first (`name` nullable).
  - **`introspect.schemas` / `exclude_schemas` warn on MySQL/SQLite** under `database` / `both`,
    following the `input.schema` precedent. PRD §4.4 and §4.13, plus `configuration.md`.
    `TestValidatePreParse_IgnoredIntrospectSchemas` (4 cases) failed first.
  - Found while probing: plain view columns differ in nullability by source too (backlog, `[measured]`).

---

## 30.8b Views come from their definition, the same way for both sources

**PRD Reference:** Sections 6.1, 16.1, 16.2, 16.3

**Status:** Complete

**Why.** A 2026-10-06 probe (PG 16, MySQL 8.0, SQLite, same two views) showed that view
columns differed by source and that the files path was unsound:
- `BuildView` marked every schema-matched column NOT NULL, including a nullable column and the
  nullable side of a LEFT JOIN, so a NULL in either failed the scan.
- PG and SQLite catalogs report every view column nullable, a primary key included, and
  SQLite reports an expression's type as `blob`. Only MySQL works out nullability. No
  catalog-based rule can match the files path.
- Re-parsing each view's stored definition with the file view parser gives exactly the
  files-path columns on all three dialects. On PG this needs `pg_get_viewdef` read under an
  empty `search_path`, which qualifies every table.

**Ruled with the user 2026-10-06:**
- A view the database defines takes its columns from the database's definition. An
  `input.views` file for it adds annotations only; its `SELECT` is never read, so it need not
  parse as a single `SELECT` (ruled 2026-10-07, Option A). Under `both`, a
  pending view change therefore does not appear until it runs.
- Both sources derive view columns with one rule, from the view's SQL.
- When the view parser cannot read a stored definition, or its columns differ from the
  catalog's, the catalog's columns are used and a warning names the view.
- An annotation naming a column the database view lacks is an error, as in a view file.
- Added after a probe showed views over subqueries and other views turned `any`: a column the
  definition cannot trace to a table takes the catalog's type and nullability. SQLite has no
  type for an expression, so it stays `any` there. Under `files` it is `any`; PRD §6.1 names
  that as a difference by source.

### Tasks

- [x] Nullability rule in `BuildView`, for every source:
  - a column read straight from a table takes that column's nullability;
  - a column from the outer side of a join is nullable (`LEFT` → right side, `RIGHT` → left,
    `FULL` → both); `ViewSQL` records those aliases, and the three view parsers fill them;
  - unchanged: `COUNT` NOT NULL, other aggregates nullable, `@type` NOT NULL (§16.3),
    `@nullable` and `@pk` override, expressions `any`.
- [x] The three introspectors derive each view's columns from its stored definition with the
  dialect view parser and `BuildView`. PG reads `pg_get_viewdef` under an empty `search_path`.
  `View.SQL` keeps the catalog text it holds today (the manifest's `source`). PG's matview
  primary key from unique indexes (§16.5) still applies on top.
- [x] Fallback: catalog columns plus a `schema.Warnings` line when the definition does not parse
  or its column names differ from the catalog's.
- [x] `parser.MergeViewAnnotations` returns an error for a `@pk`, `@type` or `@nullable` column
  the view lacks.
- [x] PRD §16.2, §16.3 and §6.1. Remove the 2026-10-06 view-nullability backlog line.
- [x] Regenerate goldens; update example tests whose view fields become nullable.

### Acceptance Criteria

- A view over a nullable column or the outer side of a join generates a nullable field.
- The same view gives the same columns under `files` and `database` on all three dialects.
- A view the parser cannot read still introspects, with a warning naming it.
- An annotation that names a missing column fails under every source.

### Tests Required

- [x] Each dialect's `ParseViewSQL` records outer-join aliases (table-driven: inner, left,
  right, full, nested).
- [x] `BuildView` nullability (table-driven): nullable column, NOT NULL column, each join side,
  `COUNT` over an outer side, `@type`, `@pk`, `@nullable`.
- [x] Integration, per dialect: an introspected view equals the file-parsed view; PG with a
  `search_path` set to the view's schema; an unreadable view falls back with a warning.
- [x] `MergeViewAnnotations` with a missing column → error; CLI test through `parseSchema`.

### Completion Record

- **Completed:** 2026-10-07 (`/done 30.8b`); implemented 2026-10-06, with the Option A follow-up
  on 2026-10-07. `make check`, the `parser/introspect` view integration tests and
  `make check-examples` pass; no golden moved.
- **Files:**
  - `parser/view.go`: `ViewSQL.OuterAliases`; `resolveColumnType` takes the source column's
    nullability or the outer side's; `checkAnnotationColumns` shared by `BuildView` and
    `mergeView`; `MergeViewAnnotations` returns an error.
  - `parser/{postgres,mysql,sqlite}/view_sql.go`: record outer-join aliases. PG and SQLite now
    reject a compound `SELECT`, as MySQL already did (see below).
  - `parser/introspect/views.go` (new): `viewColumnsFromDefinition`, the fallback warning.
  - `parser/introspect/{postgres,mysql,sqlite}.go`: derive view columns from the definition.
    PG reads every `pg_get_viewdef` once per `Introspect`, in a read-only transaction with an
    empty `search_path`. MySQL builds a catalog column only where it is used, since an ENUM/SET
    column can add a type to the schema.
  - `cmd/sqlgen/cli/pipeline.go`: `addViewFiles`, split out of `parseSchema` (cyclomatic limit).
  - PRD §6.1, §16.1, §16.2, §16.3.
- **Found while testing, fixed inline:** a compound view (`UNION`, `INTERSECT`, `EXCEPT`) parsed
  to zero columns on PG and to the first branch's columns on SQLite, with no error, so a
  nullable column in a later branch was generated non-nullable. Both now return MySQL's "not a
  simple SELECT"; under `database` the view keeps the catalog's columns with a warning. PRD §16.1
  states it. `TestParseViewSQL_CompoundSelectRejected` (PG, SQLite).
- **Backlog:** the MySQL file parser keeps `integer` / `numeric(10,2)` as written while the
  catalog says `int` / `decimal(10,2)` (a table-type parity gap, found by the new test, whose
  fixture now uses the canonical spellings). The view-nullability line this sub-item fixes is
  removed.
- **No golden moved.** Every example view's LEFT JOIN column feeds an aggregate, which was
  already nullable, and nullable source columns already carry `@nullable`.
- **`View.SQL` unchanged:** it keeps the catalog's text, which the manifest emits as the view's
  `source`. That still differs from a view file's text; 30.10 has to name it.
- **Tests:**
  - `TestParseViewSQL_OuterAliases` (PG 8, MySQL 7, SQLite 6 cases).
  - `TestBuildView_Nullability` (11 cases); failed first on 3 with the old rule.
  - `TestMergeViewAnnotations_UnknownColumn` (3 cases) and the CLI's
    `TestParseSchema_AnnotationNamesColumnTheDatabaseViewLacks`; both fail with the check removed.
  - `TestIntrospectViews_ColumnsFromDefinition` (integration, 3 dialects): a LEFT JOIN view equals
    the view file's columns, with PG's `search_path` set to the view's schema; an expression
    takes the catalog's type (SQLite `any`); a `UNION` view falls back with a warning. With
    re-parsing disabled it fails on PG and SQLite (MySQL's catalog already gets it right).
  - Updated: `TestPostgresIntrospectMaterializedViews` now expects the view-file types
    (`COUNT` → `int64`, `SUM` keeps `numeric(10,2)` for the generator to widen).
- **Auto-review** (`sqlgen-reviewer`), fail-first spot check confirmed. Two regressions, both
  fixed inline with a test that failed first:
  - **SQLite RIGHT / FULL JOIN.** rqlite has neither and reads `a RIGHT JOIN b` as table `a`
    aliased `RIGHT`, inner-joined to `b`, with no error, so a NULL-able column came out NOT NULL.
    `collectSourceAliases` now rejects a `RIGHT` / `FULL` alias (`OUTER` spellings already fail
    to parse). Under `database` the view falls back to the catalog. PRD §16.1 states it.
    `TestParseViewSQL_RightAndFullJoinRejected` (4 cases); a nested-join case was added to the
    SQLite `OuterAliases` test.
  - **`both` read view columns before the overlay.** A migration that drops a NOT NULL on a
    table a view reads left the view column NOT NULL. `View.Definition` (new) keeps the parsed
    definition, and `introspect.Merge` reads the columns again against the merged schema; a
    column it cannot trace, and a matview primary key from a unique index, are kept.
    `TestParseSchema_BothSourceViewReadsOverlaidTable` (CLI, SQLite) and
    `TestMerge_RefreshesViewColumns` (PG matview); each fails without the refresh, and the PG
    one also without the primary-key keep.
  - Doc fixes applied: the PRD §16.2 sentence saying introspected views need no schema matching
    is removed; §16.3 is "Column Type Resolution" for every view (3 anchors updated).
  - `TestMySQLIntrospectViews` ignores `View.Definition` alongside `SQL`.
- **Reviewer question, ruled Option A (2026-10-07):** a file for a view the database defines
  supplies only the view's name and its annotations. Before, the file's `SELECT` still had to
  parse and build, so a database view with a `UNION` could not be annotated, and an annotation
  naming a column the database view has but the file's `SELECT` lacked failed.
  - `parser.ViewQueryError` (new) carries the view's name when the query is the problem (a
    compound `SELECT`, a non-`SELECT` query, a SQLite `RIGHT` / `FULL` join); the message is
    unchanged. All three `ParseViewSQL`s return it.
  - `parseViewFiles` takes the database's view keys. For a view in that set it accepts a
    `ViewQueryError` and skips `BuildView`; `mergeView` still checks the annotations against the
    database's columns. A file for a view the database lacks is read as before.
  - PRD §16.1 (the exception) and §16.2 (the file supplies only the name and annotations).
  - Tests: `TestParseViewSQL_CompoundSelectRejected` now asserts the named error (PG 5 cases
    with a qualified view and a matview, SQLite 3, MySQL 2 new);
    `TestParseViewSQL_RightAndFullJoinRejected` asserts it on the 3 alias cases;
    `TestParseSchema_AnnotationFileQueryNotRead` (CLI, 3 cases: a `UNION` file, a file lacking
    the annotated column, a `UNION` file for a view the database lacks → error). The first two
    fail with the shortcut disabled.

- **`/verify 30.8b` (2026-10-07):** 5/5 requirements pass, every required test is present, and
  the reviewer's fail-first spot check on the Option A shortcut is CONFIRMED. No FIX logged. Doc
  fixes applied: the `OuterAliases` SQLite count (6), and PRD §6.1 now says a view's columns are
  read again against the migrated tables under `both`. Two adjacent findings went to the backlog:
  view parsers silently dropping unaliased expressions and `*`, and a qualified MySQL view
  file not overlaying the database's view.
---

## 30.9 Exit-code split — connection 4, schema 3

**PRD Reference:** Section 8.1.7 (design doc Q2)

**Status:** Complete

### Tasks

- [x] In `generate.go:46-52`, `pipeline.go:48-54` and `validate.go:46-55`, replace the source-mode
  remap with `errors.AsType[*connectionError]` → `ExitConnection` (4). Every other error →
  `ExitSchema` (3).
- [x] Record the FIX-243 Q3(b) outcome in that entry's history in `docs/tracker/fixes.md`.
- [x] (Added at review, ruled with the user 2026-10-07.) A stored SQLite `CREATE TABLE` the file
  parser cannot read, or catalog text the introspector cannot normalize, exits 3, not 4 (PRD §6.3,
  §8.1.7, §23.9). `parser/introspect` returns these as `*introspect.SchemaError`, and the CLI
  does not wrap them in `*connectionError`.

### Acceptance Criteria

- A connection or catalog-query failure exits 4 from `generate`, `validate`, `diff`, `lint` and
  `graphql`.
- An overlay schema error, merge conflict or unresolved FK exits 3 under every `source`.

### Tests Required

- [x] Table-driven CLI test over the five commands: SQLite unreadable path → 4, overlay
  `CREATE TABLE` conflict → 3. ~~An unresolved-FK schema under `source: both` → 3~~: it needs a
  live PostgreSQL (the FK check is PG-only and `both` introspects first), so it moved to 30.10
  (ruled with the user 2026-10-07). Here a mapping test on the real unresolved-FK error stands in.

### Completion Record

- **Completed:** 2026-10-07 (`/implement 30.9`, `/verify 30.9`, `/done 30.9`). `make check` and
  `make check-examples` ok.
- **Files:** `cmd/sqlgen/cli/connection.go` (new `parseExitCode`), `generate.go`, `pipeline.go`
  (`loadAndValidate`: `diff`, `lint`, `graphql gen`), `validate.go`, `introspect_test.go`;
  `parser/introspect/introspect.go` (new `SchemaError`), `sqlite.go`, `postgres.go`, `mysql.go`
  and their tests; `docs/tracker/fixes.md` (FIX-243 history).
- **Behavior:** `parseExitCode(err)` returns 4 when `err` holds a `*connectionError`, else 3. It
  replaces the `source: database|both` → 4 remap in `generate` and `loadAndValidate`. `validate`
  used to give 3 for every parse error, a connection failure included; it now uses the same
  mapping (and still keeps an earlier config error's exit 2).
- **Introspector schema errors (added at review).** The reviewer found that `introspectSchema`
  wrapped every `Introspect` error as a `*connectionError`. That included a stored SQLite
  `CREATE TABLE` the file parser rejects (`PRIMARY KEY ASC AUTOINCREMENT`, `ON CONFLICT`), which
  then exited 4 with a "Schema error:" label. 30.8 had handed this question to 30.9. Ruled with
  the user: exit 3, as the PRD says. `SchemaError` marks errors where they are raised:
  - SQLite: `parseStoredTable`, `nameDeclaredConstraints`, `sqliteGeneratedExprs`
  - PostgreSQL: `catalogExpr`, which covers defaults, generated columns, CHECKs, domain
    constraints and partial-index predicates
  - MySQL: `mysqlDefault`, generated-column and CHECK normalization

  The error messages are unchanged.
- **Tests:** `TestCommands_ParseExitCode` (5 cases × `generate` / `validate` / `diff` / `lint` /
  `graphql gen`): a missing SQLite file → 4; a file that is not a SQLite database (the ping
  fails, under `both`) → 4; an overlay `CREATE TABLE` of a table the database has → 3; a
  stored `CREATE TABLE` the file parser cannot read (under `database`) → 3; a view annotation
  naming a column the database view lacks (under `database`) → 3.
  `TestParseExitCode`: the real PG unresolved-FK error → 3, a `*connectionError` (bare and
  wrapped) → 4. In `parser/introspect`, `TestParseStoredTable_SchemaError`,
  `TestCatalogExpr_SchemaError`, and a `*SchemaError` assertion added to
  `TestMySQLDefault_UnparsableExpression` and to `TestSQLiteIntrospect_ImplicitFKTarget`
  (integration). The MySQL generated-column and CHECK sites need a live server, so they have no
  unit test.
- **Fail-first:** with the three callers reverted (before the stored-DDL case was added), 10
  subtests fail: both connection cases under
  `validate` (got 3), and both schema cases under `generate`, `diff`, `lint` and `graphql gen`
  (got 4).
- **Fail-first (introspector schema errors):** with the CLI's `SchemaError` check removed, the
  stored-DDL case fails under all five commands (got 4). With the three wrapping sites reverted,
  the three `parser/introspect` unit tests fail.
- **Moved to 30.10:** the live PG `source: both` unresolved-FK → 3 case.
- **`/verify 30.9` (2026-10-07):** 3/3 PRD requirements pass, the required tests are present (the
  PG `both` case moved to 30.10 by ruling), and the reviewer's fail-first spot check on the CLI
  `SchemaError` passthrough is CONFIRMED. No FIX logged. Doc fixes applied: the
  `parseExitCode` comment cites PRD §23.9, and this record's test count and fail-first note are
  corrected. The test's failure message now prints the exit code it got.
- **Connection-error label (ruled with the user at `/verify`, 2026-10-07).** A parse error that
  exits 4 now prints `Connection error:` instead of `Schema error:`, matching PRD §23.9's wording.
  `printSchemaError` became `printParseError`, which picks the prefix from `parseExitCode`, so the
  label and the exit code cannot disagree. `TestCommands_ParseExitCode` asserts the prefix on every
  command, `Error:` for `validate`. Fail-first: with the prefix forced to
  `Schema error`, the two connection cases fail under the four other commands (8 subtests).
- **Gates:** `make check` ok; `make check-examples` ok (triggered by `parser/**`); parser
  `go test -race -count=1 ./...` with Docker (PG / MySQL integration) ok.
- **Lint:** `introspectSchema` passes a `SchemaError` through unwrapped, under a reasoned
  `//nolint:wrapcheck`. Its message already names the table and step.

---

## 30.10 CLI live-database suite, all dialects

**PRD Reference:** Sections 6.1, 8.1.7, 16.2 (design doc §7.4, §12)

**Status:** Not Started

### Tasks

- [ ] Add `cmd/sqlgen/cli/introspect_integration_test.go`.
  - It starts PG16 and MySQL 8.0 lazily through `sync.Once` helpers (not `TestMain`) and uses a
    temp file for SQLite.
  - It skips under `testing.Short()`.
  - `testcontainers-go` and its postgres and mysql modules become test-only requirements in
    `cmd/sqlgen/go.mod`.
- [ ] Add a `dialectFixture` slice: starter, DDL, config fragment and a stated reason for any
  test that doesn't apply. Add a `writeTestConfigWithSource` helper next to `writeTestConfig`
  (`cli_test.go:1169`).
- [ ] Implement every design-doc §7.4 test as `postgres` / `mysql` / `sqlite` subtests.

### Acceptance Criteria

- `files` and `database` produce byte-identical generated trees, manifest included, for the
  same DDL on all three dialects. Any difference is a named parity exception with a catalog
  reason.
- Each dialect's `database` output compiles.
- Every row of design doc §12 (definition of done) holds.

### Tests Required

- [ ] `TestGenerate_DatabaseSourceMatchesFiles`, per dialect DDL as in design doc §7.4:
  - **PG:** enum, domain, composite PK, cross-schema FK, identity, `uuid` / `numeric` / `jsonb`,
    a generated column, comments, an annotated view
  - **MySQL:** ENUM / SET, composite PK, FKs, AUTO_INCREMENT, `DEFAULT ''`, unsigned ints, a
    generated column, comments, a view
  - **SQLite:** a rowid alias, a composite PK out of column order, a self-referencing FK, a
    generated column, UNIQUE, CHECK, a view
- [ ] `TestGenerate_DatabaseSourceCompiles`: `go build` of each dialect's `database` output
  (`GOWORK=off` with a `replace` to the runtime module).
- [ ] `TestGenerate_BothSourceAppliesOverlay`: overlay applied, and `CREATE TABLE` of an
  existing table → exit 3.
- [ ] `TestGenerate_DatabaseSourceIgnoresPaths`.
- [ ] `TestGenerate_IntrospectFilters`: `tables` on all three dialects;
  `schemas` / `exclude_schemas` on PG.
- [ ] `TestGenerate_IntrospectedViewWithAnnotation`: one view, with `@pk` applied.
- [ ] `TestGenerate_ConnectionFailureExitCode`: an unreachable host (PG, MySQL) or an unreadable
  file (SQLite) → 4 for `generate` and `validate`; an overlay schema error → 3.
- [ ] PG `both`: a file FK into a DB-only table resolves (from 30.8).
- [ ] PG `both`: a file FK whose target neither the database nor the files define → exit 3 for
  `generate` and `validate` (moved from 30.9: it needs a live PostgreSQL). It exited 4 before
  30.9, per the FIX-243 measurement.
- [ ] PG `database` with `introspect.tables` cutting an FK: no unresolved-FK error, and no
  relationship into the filtered-out table (the 30.8 ruling, PRD §5.5).
- [ ] Manual: a throwaway `sqlgen.yml` with `source: database` per dialect → `sqlgen generate` →
  `go build ./...`. Record it in the Completion Record.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

---

## Phase closure

Run `/close-phase 30` once 30.0–30.10 are Complete. It:

- checks design doc §12 (definition of done)
- runs `make check`, `make check-examples` and `make test-integration`
- flips `docs/design/INTROSPECTION.md` to SYNCED and archives it under `docs/design/archive/`
