# Phase 30: Database Introspection — CLI Wiring & Introspector Parity

Status: Not Started
PRD Sections: 4.2, 4.3, 4.4, 5.2, 6.1, 6.3, 6.4, 7, 8.1.7, 16.2, 30.7

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

## 30.0 PRD sync — introspection, parity rules and the canonical schema model

**PRD Reference:** Sections 4.2, 5.2, 6.1, 6.3, 7, 8.1.7, 30.7

**Status:** Not Started

### Tasks

- [ ] §4.2 `paths` row: state that `paths` is ignored under `source: database`, including its
  `["."]` default. State that `input.introspect.*` has no effect under `source: files`.
- [ ] §5.2 PostgreSQL bullet list:
  - add generated columns, `GENERATED ALWAYS AS (expr) STORED` (Q1)
  - add that `CREATE TABLE … PARTITION OF` is skipped with a warning and the partitioned
    parent is the entity (Q14)
  - add that type modifiers are written without a space, as in `numeric(12,2)` (Q12)
- [ ] §5.2 MySQL bullet list: add generated columns, `GENERATED ALWAYS AS (expr)
  [VIRTUAL|STORED]` and the shorthand `AS (expr)` (Q1).
- [ ] §5.2 SQLite bullet list: add that `CREATE VIRTUAL TABLE` is skipped with a warning, like
  `CREATE VIEW` (Q9). Add that `INTEGER PRIMARY KEY` is auto-increment only when it is a rowid
  alias, which excludes `WITHOUT ROWID` tables and `INTEGER PRIMARY KEY DESC` (S9 / S11).
- [ ] §5.2 (all dialects): state the canonical constraint model (Q11).
  - Every PRIMARY KEY, UNIQUE, FOREIGN KEY and CHECK, inline or table-level, is a table
    constraint and also sets the column flags.
  - An unnamed constraint gets the name its database would assign:
    - PG: `<t>_pkey`, `<t>_<cols>_key`, `<t>_<cols>_fkey`, `<t>_<col>_check`, with 63-byte
      truncation and a numeric collision suffix
    - MySQL: `PRIMARY`, `<t>_ibfk_<n>`, `<t>_chk_<n>`, and a UNIQUE named after its first
      column, with `_2`, `_3` on collision
    - SQLite: declared names only
- [ ] §6.1: state that introspected defaults and CHECK / domain-constraint text are normalized to
  the file parser's form, with catalog-added implicit casts removed (Q5, Q13). State that
  `database` and `files` therefore describe the same schema identically.
- [ ] §6.3 capability table and notes:
  - PG partitions are skipped; only the partitioned parent is introspected (Q4).
  - SQLite virtual and shadow tables are skipped with a warning (Q9).
  - SQLite "Unique constraints" and "Check constraints" change from "Limited" to "Yes", with a
    note that names and CHECK text come from the stored `CREATE TABLE` (Q8, Q10).
  - Footnote: MySQL CHECK introspection needs 8.0.16+, and an older server yields no CHECKs,
    with no hard minimum version (Q7).
- [ ] §7 type resolution: an enum, set or domain resolves under its bare name or its
  schema-qualified name. A bare key is registered only when no other schema has the same name.
  Both paths write a type in the column's own schema bare, and a cross-schema type qualified (Q3).
- [ ] §8.1.7 CLI tests, exit-code bullet: 4 means a connection or catalog-query failure only; a
  schema error exits 3 under every `source` (Q2).
- [ ] §30.7 `indexes[]` row: replace "derived labels … PostgreSQL's default on every dialect"
  with the Q11 rule. Names are the ones the database assigns. They are derived per dialect for
  unnamed constraints, and on SQLite only declared names are used, with an unnamed index keeping
  the existing derived label. Keep the sentence about a `primary_key.columns`-only key.
  **Word the SQLite case with the user before landing**, because Q11 leaves the manifest label
  for an unnamed SQLite constraint implicit.
- [ ] §30.7 `check` row: replace "as written in the DDL" with "in the file parser's normalized
  form" (Q13).
- [ ] Cross-check every edit against design doc §8. Fix `#` anchors that shift.

### Acceptance Criteria

- Every edit in design doc §8 is in the PRD, and every rule in Q1–Q14 that changes user-visible
  behavior is stated in the PRD.
- No PRD section still says `database` mode reads `paths`, that §30.7 index names are
  PostgreSQL-style on every dialect, or that CHECK text is "as written".
- The PRD has no broken internal anchors.

### Tests Required

- [ ] None (docs only). `grep` checks for the retired phrasings listed in the acceptance
  criteria, plus an anchor check of every `](#…)` link touched.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

---

## 30.1 Parity harness, probe fixtures and unit tests (all dialects)

**PRD Reference:** Sections 5.2, 6.3 (design doc §6.0, §6.1, §6.3, §6.4)

**Status:** Not Started

### Tasks

- [ ] Add `assertSchemaParity(t, dialect, parsed, introspected, ignore ...parityException)` to
  `parser/introspect/introspect_integration_test.go`.
  - It sorts tables, constraints, enums, sets, composite types, domains and views on both
    sides, then runs a full `cmp.Diff`.
  - `parityException{Field, Reason}` is the only way to exclude a field.
  - All exceptions live in one table at the top of the file, keyed by dialect.
- [ ] Move `TestPostgresIntrospect`, `TestMySQLIntrospect` and `TestSQLiteIntrospect` onto the
  helper.
  - Seed the exception table with only the provenance fields no catalog stores
    (`InlineUnique`).
  - Record every resulting failure in the Completion Record. These are the baseline the later
    sub-items burn down; reference them by defect ID.
- [ ] Turn the probe DDL from `.claude/prototypes/introspection-probe/zz_probe_test.go` into
  per-dialect fixture constants (PG schema `probe_app`/`probe_other`, MySQL tables `m_*`,
  SQLite `s_*`) for 30.2–30.6 to share.
- [ ] Add unit tests, table-driven and white-box:
  - `TestNormalizePostgresType`, `TestParsePostgresArray` and `TestExtractCheckExpression` in
    `postgres_test.go`
  - `TestNormalizeMySQLType` and `TestExtractMySQLEnumValues` in a new `mysql_test.go`
  - `TestNormalizeSQLiteType` in `sqlite_test.go`
- [ ] Fix D2: `extractCheckExpression` handles the `NOT VALID` suffix (`postgres.go:927`).
- [ ] Fix M3: `extractMySQLEnumValues` respects quoting, so a value containing a comma, a
  doubled quote (`''`) or an empty value survives (`mysql.go:523`).
- [ ] Fix M8: the MySQL parser unescapes `''` inside an ENUM/SET value (it produces `it\'s`
  today).
- [ ] Fix S6: `indexPartialWhere` checks `err != nil` (other than `sql.ErrNoRows`) before
  `ddl.Valid` (`sqlite.go:483`).
- [ ] Fix S10: the SQLite parser no longer turns `INTEGER PRIMARY KEY DESC` into a phantom `DESC`
  column.

### Acceptance Criteria

- All three main introspection tests compare the full schema through `assertSchemaParity`. No
  dialect test hand-picks fields.
- The exception table holds only fields no catalog can round-trip, each with a reason.
- D2, M3, M8, S6 and S10 are fixed, and each has a test that failed before its fix.
- `TestMySQLIntrospect_EnumEdgeValues` has the same values on both paths.

### Tests Required

- [ ] `TestNormalizePostgresType`: `character varying(255)`, `character(3)`,
  `timestamp(3) without time zone`, `timestamp with time zone`, `time with time zone`, arrays of
  each, `double precision`, `bit varying(8)`, a schema-qualified name, passthrough.
- [ ] `TestParsePostgresArray`: `{}`, a plain list, a quoted element with a comma or space, an
  escaped quote, a mixed-case quoted identifier.
- [ ] `TestExtractCheckExpression`: `CHECK ((a > 0))`, compound `AND` / `OR`, a `NOT VALID`
  suffix (D2), non-CHECK passthrough.
- [ ] `TestNormalizeMySQLType`: `int`, `int unsigned`, `tinyint(1)`, `bigint(20)`,
  `decimal(10,2)`, `enum(…)` / `set(…)` → bare, mixed case.
- [ ] `TestExtractMySQLEnumValues`: a plain list, a value with a comma, `it''s`, an empty value,
  values with spaces (M3).
- [ ] `TestMySQLIntrospect_EnumEdgeValues` (integration): `ENUM('active','in,active','it''s','')`
  gives identical values from the parser and the introspector (M3, M8).
- [ ] `TestNormalizeSQLiteType`: empty → `blob`, case folding, `VARCHAR(255)` / `NUMERIC(10,2)`
  passthrough.
- [ ] SQLite parser test: `id INTEGER PRIMARY KEY DESC, v TEXT` gives exactly two columns (S10).
- [ ] `indexPartialWhere` error path (S6): code fix only. No DB-level trigger exists (design doc
  §6.4); note that in the Completion Record.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

---

## 30.2 Canonical constraint model — `parser.CanonicalizeConstraints` (X1)

**PRD Reference:** Sections 5.2, 30.7 (design doc Q11, §4.4 X1)

**Status:** Not Started

### Tasks

- [ ] Add `parser.CanonicalizeConstraints(table *parser.Table, dialect string)` to the `parser`
  package. After it runs:
  - every column `PrimaryKey` / `Unique` / `FKReference` flag has a matching table
    `Constraint`, and every table `Constraint` is reflected in the column flags
  - UNIQUE / Index `Method` defaults to `btree` on PG and MySQL
  - CHECK `Columns` lists the referenced columns on every dialect
- [ ] Add per-dialect default-name derivation for unnamed constraints.
  - **PG:** PostgreSQL's `ChooseConstraintName` behavior: `<t>_pkey`, `<t>_<cols>_key`,
    `<t>_<cols>_fkey`, `<t>_<col>_check`, 63-byte truncation (`NAMEDATALEN - 1`), and a numeric
    suffix on collision within the schema.
  - **MySQL:** `PRIMARY`, `<t>_ibfk_<n>`, `<t>_chk_<n>`, and a UNIQUE named after its first
    column, with `_2`, `_3` on collision.
  - **SQLite:** names come only from the declaration.
- [ ] Call `CanonicalizeConstraints` at the end of each parser's table construction
  (`parser/postgres`, `parser/mysql`, `parser/sqlite`) and at the end of each introspector's
  per-table pass.
- [ ] PG parser: keep the declared name of an inline `CONSTRAINT <name> UNIQUE`. It is dropped
  today; FIX-240 backlog line, 2026-10-02.
- [ ] `cmd/sqlgen/manifest/builder.go` `indexNameOrDerived` / `pkIndexName` (`:954-958`): read the
  canonical constraint name instead of deriving one, apart from the §30.7 SQLite case 30.0 words.
- [ ] Regenerate the example goldens (`make update-golden-e2e`). Explain every manifest index or
  constraint-name change in the Completion Record. **Only X1-caused changes are expected.**
- [ ] Delete the two FIX-240 backlog lines this resolves (unnamed UNIQUE `_idx` vs `_key`; the
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

- [ ] `TestCanonicalizeConstraints` (table-driven): an inline PK, a table-level PK, a composite
  PK, inline and table-level UNIQUE, an inline `REFERENCES` and a table-level FK, a column CHECK
  and a table CHECK, each on PG / MySQL / SQLite. Assert the constraint set, the column flags,
  `Method` and CHECK `Columns`.
- [ ] `TestDerivePostgresConstraintName`: every suffix kind, truncation at 63 bytes
  (multi-byte identifiers included), collision suffixes.
- [ ] `TestDeriveMySQLConstraintName`: `PRIMARY`, `_ibfk_<n>` ordering, `_chk_<n>`,
  first-column UNIQUE naming with `_2` on collision.
- [ ] `TestPostgresIntrospect_TableConstraints` and `TestMySQLIntrospect_TableConstraints`
  (integration): canonical shape and names on both paths, including a PG name past 63 bytes.
- [ ] SQLite: `s_posts` inline FK is a table constraint on both paths.
- [ ] Manifest builder test: `indexes[].name` reads the canonical name.
- [ ] `TestE2EGoldenFiles` passes after the regeneration.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

---

## 30.3 Generated columns, all dialects (D4, M1, S1)

**PRD Reference:** Sections 5.1, 5.2 (design doc Q1)

**Status:** Not Started

### Tasks

- [ ] PG parser: read `GENERATED ALWAYS AS (expr) STORED` from the column constraint. Set
  `GeneratedExpr` (deparsed) and `GeneratedStorage = "STORED"`, and leave `Default` empty.
  Keep identity columns (`GENERATED … AS IDENTITY`) on the existing `AutoIncrement` path.
- [ ] PG introspector: select `pg_attribute.attgenerated` in `introspectColumns`
  (`postgres.go:381`). For `'s'`, set `GeneratedExpr` from `pg_attrdef` (normalized like the
  parser's) and `GeneratedStorage = "STORED"`, and clear `Default`.
- [ ] MySQL parser: read `GENERATED ALWAYS AS (expr) [VIRTUAL|STORED]` and the shorthand
  `AS (expr)`. Storage defaults to `VIRTUAL`.
- [ ] MySQL introspector: select `generation_expression`. Map `extra` `VIRTUAL GENERATED` /
  `STORED GENERATED` to `GeneratedStorage` and set `GeneratedExpr` in the parser's form
  (`mysql.go:199`).
- [ ] SQLite introspector: switch columns to `PRAGMA table_xinfo`. Map `hidden = 2` → `VIRTUAL`
  and `hidden = 3` → `STORED`, take the expression from the stored DDL (30.6's re-parse; until
  then, the `sqlite_master.sql` text), and skip `hidden = 1` (virtual-table hidden columns).
- [ ] Confirm the generator's `IsComputed` path (`gen/context_table.go:942`) keeps PG and MySQL
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

- [ ] PG parser unit test: STORED generated column → `GeneratedExpr` / `GeneratedStorage`, no
  `Default`; identity columns unchanged.
- [ ] MySQL parser unit test: `GENERATED ALWAYS AS (…) STORED`, `… VIRTUAL`, the shorthand
  `AS (…)`, and the default storage.
- [ ] `TestPostgresIntrospect_GeneratedColumn`, `TestMySQLIntrospect_GeneratedColumn` and
  `TestSQLiteIntrospect_GeneratedColumn` (integration): parity with the parser through
  `assertSchemaParity`.
- [ ] Codegen test per dialect: a table with a generated column has no create, update or
  increment surface for it.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

---

## 30.4 PostgreSQL parity (D1, D3, D5, D6, D7, D8, D9)

**PRD Reference:** Sections 5.2, 6.1, 6.3, 7 (design doc §6.2, Q3, Q4, Q5, Q12, Q13, Q14)

**Status:** Not Started

### Tasks

- [ ] D6 / Q3, resolver (`cmd/sqlgen/gotype/gotype.go:431`, `:625`):
  - `RegisterEnum`, `RegisterSet` and `RegisterDomain` also register a `schema.name` key
  - the bare key is registered only when that name is unique across schemas
  - `registerSchemaTypes` (`gen/orchestrate.go:2530`) passes the schema
- [ ] D1 / D6 / Q3, canonical type form in the PG parser (`typeNameToString`, `postgres.go:814`)
  and the introspector:
  - a type in the column's own schema is written bare
  - a cross-schema type stays qualified
  - array suffixes are preserved
- [ ] D3 / Q4: skip tables with `pg_class.relispartition` in `introspectTables`
  (`postgres.go:75`).
- [ ] D9 / Q14: the PG parser skips `CREATE TABLE … PARTITION OF` and appends a `schema.Warnings`
  line in the existing `CREATE VIEW` style.
- [ ] D5: the constraint query keeps column-less CHECKs. Replace the inner join on
  `unnest(conkey)` with a left join, or handle `conkey IS NULL` (`postgres.go:452`).
- [ ] D7 / Q12: the PG parser's typmod formatting (`formatTypmods`) writes `numeric(12,2)`
  without a space.
- [ ] Q5 defaults:
  - run catalog default text through the same `pg_query` deparse the parser uses
  - strip catalog-added casts on literals (`''::text`, `'active'::status_enum`,
    `'{}'::text[]`)
  - serial and identity columns carry no `Default`, only `AutoIncrement`
- [ ] D8 / Q13, CHECK and domain-constraint text:
  - same deparse as defaults
  - strip implicit casts: domain → base type (`(score)::integer`) and literal → column type
  - normalize keyword case (`VALUE`)
  - each removal rule is a named function with its own unit test
- [ ] Tighten `TestPostgresIntrospect` (`:102`) through `assertSchemaParity`, with no
  PG-specific exceptions beyond the 30.1 provenance set.

### Acceptance Criteria

- The PG probe schema (`probe_app` / `probe_other`) parses and introspects to identical
  schemas through `assertSchemaParity`, with and without `search_path` on the connection.
- `status app.status_enum` generates the `StatusEnum` Go type in `files` mode. Today it
  generates `string`.
- A partitioned table yields the parent only on both paths, plus a parser warning for each
  `PARTITION OF`.
- `overrides.types."numeric(12,2)"` applies to a `numeric(12,2)` column on both paths.

### Tests Required

- [ ] `TestPostgresIntrospect_TypeCoverage`:
  - `uuid`, `numeric(12,2)`, `bytea`, `interval`, `inet`, `smallserial`, `serial`
  - `int GENERATED ALWAYS AS IDENTITY` and `bigint GENERATED BY DEFAULT AS IDENTITY`, both
    `AutoIncrement`
  - an enum column, a domain column, `status_enum[]`
  - each checked against the parser **and** an explicit expected `Type`
- [ ] `TestPostgresIntrospect_Defaults`: a literal, `now()`, `gen_random_uuid()`, an enum
  literal, `'{}'`, serial, identity. `Default` equals the parser's text.
- [ ] `TestPostgresIntrospect_CheckAndDomainConstraints`: CHECK text equals the parser's,
  including a CHECK on a domain-typed column (D8), `NOT VALID` (D2), a column-less CHECK (D5),
  and domain CHECK name and text.
- [ ] `TestPostgresIntrospect_NoSearchPath`: own-schema types bare, cross-schema types qualified
  (D1).
- [ ] `TestPostgresIntrospect_QuotedIdentifiers`: `"UserAccounts"`, `"createdAt"`, a constraint
  over quoted columns.
- [ ] `TestPostgresIntrospect_Partitioning` (D3) and a PG parser test for the `PARTITION OF`
  warning (D9).
- [ ] Resolver unit tests:
  - a qualified enum, set or domain resolves
  - a bare name that is unique resolves
  - a bare name shared by two schemas resolves only qualified
- [ ] Unit tests for each implicit-cast removal rule and for the default normalization.
- [ ] Codegen test: `status app.status_enum` in `files` mode resolves to the enum Go type (D6).

### Completion Record

_Filled in when complete: files changed, commit references, notes._

---

## 30.5 MySQL parity (M2, M4, M5, M6, M7, M9)

**PRD Reference:** Sections 5.2, 6.1, 6.3 (design doc §6.3, Q5, Q7, Q13)

**Status:** Not Started

### Tasks

- [ ] M2 / M9 / Q5, default normalization (`mysql.go:234`):
  - re-quote string, enum, set and temporal literal defaults using `column_type`
  - treat a default as an expression when `extra` contains `DEFAULT_GENERATED`
  - put the result through `sqlparser.String`, so it matches the parser's form (for example
    `current_timestamp(3)`)
  - `DEFAULT ''` yields `''`, which is not empty
- [ ] M4 / Q7: `introspectCheckConstraints` (`mysql.go:486`) falls back only on MySQL error 1109
  (unknown table), checked with `errors.AsType[*mysql.MySQLError]`. Every other error is
  returned, wrapped.
- [ ] M5: make the integer display width agree between the two paths. Decide the direction in
  the Completion Record against the probe (parser `int(11)` vs catalog `int`; `INT UNSIGNED`
  already agrees). If the direction isn't mechanical, stop and ask.
- [ ] M6: `introspectViewColumns` (`mysql.go:163`) resolves an ENUM or SET view column to the
  synthetic `<table>_<column>_enum` / `_set` type of its source column when that column is
  identifiable. Otherwise it registers a view-scoped synthetic type. Never the bare `enum`.
- [ ] M7 / Q13: normalize `check_clause` through `sqlparser`, dropping backticks and charset
  introducers (`_utf8mb4'x'`) to match the parser's text.
- [ ] Tighten `TestMySQLIntrospect` (`:284`) and `TestMySQLIntrospectViews` through
  `assertSchemaParity`.

### Acceptance Criteria

- The MySQL probe schema (`m_users`, `m_pairs`, `MixedCase`, `m_user_status`) parses and
  introspects to identical schemas through `assertSchemaParity`.
- A `NOT NULL DEFAULT ''` column is omittable in `Create<T>Input` in `database` mode. Today the
  empty default makes it required.
- A failing CHECK query surfaces as an error. Only error 1109 yields "no CHECKs".

### Tests Required

- [ ] `TestMySQLIntrospect_Defaults`: `''`, a string literal, an ENUM literal,
  `CURRENT_TIMESTAMP(3)`, `(uuid())`, `DEFAULT NULL`, a numeric literal. Each equals the
  parser's text.
- [ ] `TestMySQLIntrospect_TypeCoverage`: `INT(11)`, `BIGINT(20) UNSIGNED`, `INT UNSIGNED`,
  `BIGINT UNSIGNED`, `tinyint(1)`, `decimal(10,2)`, `json`, `datetime(3)`, `binary(16)`,
  `varbinary`, `blob`, ENUM, SET.
- [ ] `TestMySQLIntrospect_CheckErrorsSurface`: cancel the context mid-run, after the column
  queries (a context cancelled up front fails at ping, per the probe). Plus a white-box unit
  test for the 1109 fallback.
- [ ] `TestMySQLIntrospect_QuotedIdentifiers`: `` `UserAccounts` ``, `` `createdAt` ``, a
  reserved-word column (`` `order` ``), a constraint over them.
- [ ] `TestMySQLIntrospectViews`: a view projecting an ENUM column (M6).
- [ ] Codegen test: `DEFAULT ''` column is omittable (M2).

### Completion Record

_Filled in when complete: files changed, commit references, notes._

---

## 30.6 SQLite parity (S2, S3, S4, S5, S7, S8, S9, S11, S12)

**PRD Reference:** Sections 5.2, 6.3 (design doc §6.4, Q8, Q9, Q10)

**Status:** Not Started

### Tasks

- [ ] Q10 strategy: for each table, re-parse `sqlite_master.sql` with `parser/sqlite` and take
  declared constraint names, CHECK constraints and expression text from it. PRAGMAs stay the
  source for columns, types, nullability, PK membership and order, FKs, indexes and partial
  predicates.
- [ ] S7: replace every Go `%q` PRAGMA argument (`sqlite.go:164,199,272,341,413,565`) with SQL
  identifier quoting (double quotes, with embedded `"` doubled).
- [ ] S2: order composite PK columns by `pk` (the stored `pkCol.order`), not `cid`
  (`sqlite.go:234`, `:251`).
- [ ] S3: scan `foreign_key_list.to` as `sql.NullString`. A NULL target resolves to the parent's
  PK column(s), as the parser resolves it (`sqlite.go:295`).
- [ ] S4 / Q8: drop `origin = 'pk'` indexes from `introspectUniqueConstraints` (`sqlite.go:339`).
  Take UNIQUE names from the re-parse, so `sqlite_autoindex_*` never surfaces.
- [ ] S5 / Q9: skip `type IN ('virtual', 'shadow')` rows from `PRAGMA table_list`. Add a
  `schema.Warnings` line naming each skipped virtual table, on the introspector **and** in the
  SQLite parser for `CREATE VIRTUAL TABLE` (`parser/sqlite/sqlite.go:63-81`).
- [ ] S8 / Q10: CHECK constraints come from the re-parse.
- [ ] S9 / S11: `AutoIncrement` only for a rowid alias. That means a single `INTEGER PRIMARY KEY`
  column in a rowid table, not `WITHOUT ROWID` and not `DESC`, on the introspector
  (`sqlite.go:244`) and the parser.
- [ ] S12: set `WithoutRowID` and `Strict` from `PRAGMA table_list` (or the re-parse).
- [ ] Tighten `TestSQLiteIntrospect` (`:426`) through `assertSchemaParity`. **`Type` is compared;
  today it is not.**
- [ ] Turn the three SQLite-only `TestBothMode*` tests (`:643`, `:706`, `:754`) into tables over
  all three dialects.

### Acceptance Criteria

- The SQLite probe schema parses and introspects to identical schemas through
  `assertSchemaParity`. That includes the FTS5 and R*Tree tables, which are skipped with a
  warning on both paths.
- A table named `Quoted"Name` introspects, and its presence does not abort the run.
- `REFERENCES t` with no column introspects. Today it crashes the run.
- `INTEGER PRIMARY KEY DESC` and a `WITHOUT ROWID` `INTEGER PRIMARY KEY` are not auto-increment
  on either path. The measured behavior: the `DESC` insert stores `NULL`, and the
  `WITHOUT ROWID` insert fails NOT NULL.
- `both`-mode overlay, create-conflict and column-conflict tests pass on all three dialects.

### Tests Required

- [ ] `TestSQLiteIntrospect_CompositePKOrder` (S2).
- [ ] `TestSQLiteIntrospect_ImplicitFKTarget` (S3).
- [ ] `TestSQLiteIntrospect_UniqueAndPKAutoIndexes`: no duplicate PK UNIQUE; inline, named and
  table-level UNIQUE names match the parser (S4).
- [ ] `TestSQLiteIntrospect_VirtualTables`: FTS5 + R*Tree skipped with warnings (S5), plus a
  parser test for the `CREATE VIRTUAL TABLE` warning.
- [ ] `TestSQLiteIntrospect_QuotedNames`: a table and a column containing `"`, a space and mixed
  case (S7).
- [ ] `TestSQLiteIntrospect_Checks`: column, table and named CHECKs match the parser (S8).
- [ ] `TestSQLiteIntrospect_RowidAlias`: `INTEGER PRIMARY KEY` (auto-increment);
  `WITHOUT ROWID`, `DESC` and `INT PRIMARY KEY` (not auto-increment), on both paths (S9, S11).
- [ ] `TestSQLiteIntrospect_TableFlags`: `WITHOUT ROWID` and `STRICT` (S12).
- [ ] `TestSQLiteIntrospect_TypeCoverage`: `INTEGER`, `TEXT`, `REAL`, `BLOB`, `NUMERIC`,
  `VARCHAR(255)`, `BOOLEAN`, `DATETIME`, untyped → `blob`.
- [ ] `TestSQLiteIntrospect_Defaults`: a string literal, a numeric literal, `CURRENT_TIMESTAMP`,
  a parenthesized expression, `NULL`. These already match per the probe; pin them.
- [ ] `TestSQLiteIntrospect_CompositeConstraints`: composite UNIQUE, a multi-column FK, a
  self-referencing FK.
- [ ] `TestBothModeAlterOverlay`, `TestBothModeCreateExisting` and `TestBothModeColumnConflict`
  over `postgres` / `mysql` / `sqlite`.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

---

## 30.7 Connection string + PRD §4.3 validation

**PRD Reference:** Sections 4.3

**Status:** Not Started

### Tasks

- [ ] Add `cmd/sqlgen/cli/connection.go` with `connString(dialect config.Dialect,
  c *config.ConnectionConfig) (string, error)`:
  - `url` set → returned verbatim
  - postgres → `postgres://user:pass@host:port/db?sslmode=<ssl_mode | disable>`, built with
    `net/url`
  - mysql → `user:pass@tcp(host:port)/db?tls=<ssl_mode | false>`
  - sqlite → `database`
  - the port defaults to 5432 / 3306
  - a missing required field → an error naming `input.connection.<field>`
- [ ] `config/validate.go`, next to `validateConnectionRequired` (`:1124`): when `url` is
  empty, `database` is required on every dialect, and `host` and `user` are required on postgres
  and mysql. Report every missing field together. `SQLGEN_DB_URL` (applied at `config.go:1935`)
  satisfies the rule.

### Acceptance Criteria

- Every PRD §4.3 "Required … when `url` is empty" rule is enforced by `sqlgen validate`, exit 2.
- `url` takes precedence over the individual fields. `SQLGEN_DB_URL` and `SQLGEN_DB_PASSWORD`
  keep their §4.3 precedence.
- A password containing `@`, `:`, `/` or `%` round-trips through the postgres URL.

### Tests Required

- [ ] `TestConnString` (table-driven, three dialects): `url` precedence, password escaping, the
  `ssl_mode` defaults, port defaults, each missing-field error.
- [ ] Config validation tests: each missing field per dialect; all reported together; `url` set
  skips the rule; `SQLGEN_DB_URL` alone satisfies it.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

---

## 30.8 `parseSchema` wiring — `source: database | both`

**PRD Reference:** Sections 4.2, 4.4, 6.1, 6.4, 16.2

**Status:** Not Started

### Tasks

- [ ] Add `introspectSchema(ctx, cfg) (*parser.Schema, error)` in `cmd/sqlgen/cli`:
  - picks `introspect.New{Postgres,MySQL,SQLite}Introspector()` by dialect
  - builds the connection string with `connString`
  - maps `input.introspect.{schemas, exclude_schemas, tables}` → `IntrospectionOptions`
    (top-level `exclude_tables` stays where it is applied today)
  - `defer`s `Close()`
  - wraps connect, ping and query failures in `*connectionError` (introduced here, consumed by
    30.9)
- [ ] `parseSchema(ctx, cfg)` (`cli/pipeline.go:167`):
  - `files` is unchanged
  - `database` introspects only and ignores `paths`
  - `both` introspects into `base`, then calls `introspect.Merge(base, newSeedableParser(…),
    files)` and takes `p.Schema()`
- [ ] Refactor `parseViewFiles` (`pipeline.go:235`) to return the annotated views and the
  annotation map. Under `database | both`, combine them with `parser.MergeViewAnnotations`
  (`parser/view.go:383`). Under `files`, keep the append.
- [ ] Run the PG `parser.UnresolvedForeignKeys` check on the final (merged or introspected)
  schema. This is the ordering FIX-243 Q3(a) deferred.
- [ ] Thread `ctx` (`cmd.Context()`) through the three callers: `pipeline.go:44`
  (`loadAndValidate`), `generate.go:45`, `validate.go:46`.

### Acceptance Criteria

- `source: database` generates from the live database only. `source: both` generates the
  introspected schema with the file overlay applied (PRD §6.1 merge table).
- An introspected view with an annotation file yields one view with the annotation applied
  (§16.2).
- A file FK into a table that exists only in the database resolves under `both`.
- `introspect.*` filters narrow the introspected set as §4.4 / §6.4 describe.

### Tests Required

- [ ] Unit tests with SQLite file databases (no container), covering:
  - `database` ignores `paths`
  - `both` applies `ALTER TABLE … ADD COLUMN` and a new `CREATE TABLE`
  - a `CREATE TABLE` of an existing table is an error
  - view annotation merging gives one view
  - an `introspect.tables` filter
- [ ] PG-only check with the integration container: a file FK into a DB-only table resolves under
  `both`. It can live in 30.10's suite.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

---

## 30.9 Exit-code split — connection 4, schema 3

**PRD Reference:** Section 8.1.7 (design doc Q2)

**Status:** Not Started

### Tasks

- [ ] In `generate.go:48`, `pipeline.go:47` and `validate.go:46-51`, replace the source-mode
  remap with `errors.AsType[*connectionError]` → `ExitConnection` (4). Every other error →
  `ExitSchema` (3).
- [ ] Record the FIX-243 Q3(b) outcome in that entry's history in `docs/tracker/fixes.md`.

### Acceptance Criteria

- A connection or catalog-query failure exits 4 from `generate`, `validate`, `diff`, `lint` and
  `graphql`.
- An overlay schema error, merge conflict or unresolved FK exits 3 under every `source`.

### Tests Required

- [ ] Table-driven CLI test over the five commands: SQLite unreadable path → 4, overlay
  `CREATE TABLE` conflict → 3, and an unresolved-FK schema under `source: both` → 3. Before this
  change the last case exited 4, per the FIX-243 measurement.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

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
  - **SQLite:** a rowid alias, a composite PK out of column order, an implicit FK target, a
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
