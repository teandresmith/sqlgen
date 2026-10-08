# SQLGen Database Introspection — CLI Wiring & Introspector Hardening

> **Status: DRAFT — all open questions resolved (2026-10-05).** Sibling design doc for two linked pieces
> of work: wiring the existing `parser/introspect` package into the CLI so
> `input.source: database | both` (PRD §6.1) actually reads the database, and
> hardening all three introspectors (PostgreSQL, MySQL, SQLite) so the schema
> model each hands the generator is pinned and the suspected defects in §4 are
> fixed. The §5 probe ran on 2026-10-05 (§4.0). §10 records fourteen rulings and
> no question is open. On acceptance, this doc
> is the input to `/phase`.
>
> **Pattern:** parallels `docs/design/SCHEMAFUZZ.md` and `docs/design/MCP.md`
> as a design doc that fronts open questions before phase breakdown.
>
> **Phase:** TBD — next free slot after Phase 29.
>
> **Hard dependency:** none. File references are to HEAD `49c0d01d`.

---

## 1. Motivation

### 1.1 The CLI never introspects

PRD §6.1 defines three source modes: `files`, `database`, and `both` (introspect,
then overlay file changes). The introspectors exist and are tested
(`parser/introspect/{postgres,mysql,sqlite}.go`, `merge.go`), but **nothing in
`cmd/sqlgen` imports `parser/introspect`**. `cli/pipeline.go:167 parseSchema`
parses `input.paths` in every mode, so today:

- `source: database` silently generates from whatever `paths` resolves to.
  `paths` defaults to `["."]` in every mode (`config/config.go:1789`), so the
  CLI parses every `.sql` file under the working directory and never touches
  the connection.
- `source: both` behaves exactly like `files`.
- The only observable difference is the exit code: any `parseSchema` error is
  remapped to `ExitConnection` (4) although no connection was attempted
  (`cli/generate.go:48`, `cli/pipeline.go:47`).

This was noticed while vetting FIX-243 (fixes.md, Q3, 2026-09-29: "The CLI
does not wire introspection yet"). The user ruled there to defer the
FK-check ordering "when introspection lands". No tracker item covers the
landing.

### 1.2 PostgreSQL introspection tests have gaps

`parser/introspect/introspect_integration_test.go` runs against
`postgres:16-alpine` via testcontainers (skipped under `-short`, run by the
`integration` job in `.github/workflows/test.yml:71`). The PG tests are:

| Test | Covers |
|---|---|
| `TestPostgresIntrospect` (`:102`) | Differential vs the PG file parser: column type / nullability / PK / UNIQUE / FK reference / auto-increment / comment; table comment; CHECK names; enums; composite attributes; domain base type |
| `TestPostgresSchemaFiltering` (`:518`) | `Schemas` / `ExcludeSchemas` |
| `TestPostgresIntrospectViews` (`:828`) | Regular view columns |
| `TestPostgresIntrospectMaterializedViews` (`:900`) | Matview flags, column types, unique-index PK selection |
| `TestPostgresIntrospect_IndexMethodAndPartialWhere` (`:1169`) | Partial UNIQUE `Where`, GIN `Method` |

Plus one unit test, `TestChooseMatviewPK` (`postgres_test.go`).

Not covered:

- **Defaults:** `Default` is excluded from the column comparison and is never asserted.
- **Table-level constraints:** only CHECK is compared at table level. No composite PK, composite UNIQUE, multi-column FK, cross-schema FK or self-referencing FK appears in any fixture.
- **CHECK and domain text:** CHECK expressions are only checked for non-emptiness, and domain constraints aren't checked at all.
- **Types:** the fixture covers only `bigserial`, `text`, `varchar`, `integer`, `boolean`, `jsonb`, `text[]` and `timestamptz`. Nothing covers `uuid`, `numeric(p,s)`, `bytea`, `interval`, `inet`, `serial`/`smallserial`, identity columns, enum- or domain-typed columns, or arrays of enums.
- **Search path:** every test appends `&search_path=<schema>` to the connection string. The CLI will not.
- **Identifiers:** there are no quoted or mixed-case identifiers.
- **Helpers:** the pure helpers `normalizePostgresType`, `parsePostgresArray` and `extractCheckExpression` have no direct tests.
- **CLI:** there is no CLI-level test against a live database (follows from §1.1).

### 1.3 MySQL and SQLite introspection tests have larger gaps

The MySQL suite runs against `mysql:8.0`; SQLite uses a temp file. Both main tests are
narrower than the PG one:

- **`TestMySQLIntrospect` (`:284`)** compares only column name, type, nullability, PK and
  auto-increment, plus the enum count and the presence of one CHECK. It never compares:
  - `Unique`, `FKReference` or `Comment`
  - `Default`
  - table-level constraints other than that CHECK
- **`TestSQLiteIntrospect` (`:426`)** compares column name, nullability, PK and
  auto-increment. **It does not compare `Type` at all.** Its fixture is two tables with
  single-column PKs and one inline FK.
- **Helpers:** `normalizeMySQLType`, `extractMySQLEnumValues` and `normalizeSQLiteType` have
  no unit tests. `extractSQLiteIndexWhere` has unit tests; `sqlite_test.go` covers it.

### 1.4 Outcome

1. `sqlgen generate | validate | diff | lint | graphql` honor
   `source: database | both` on **PostgreSQL, MySQL and SQLite equally**. Each dialect
   is proven by the same live-DB CLI suite, including a differential that shows
   `files` and `database` produce the same generated code for the same DDL.
2. Each introspector's catalog → `parser.Schema` mapping is pinned
   explicitly, and every defect in §4 is fixed or ruled on.
3. **Parity is enforced by construction.** All three dialects run through one comparison
   helper and one CLI test table (§6.0, §7.4), so a later fix or feature cannot land on one
   dialect and silently skip the others.

The §12 definition of done is the gate for closing the phase.

---

## 2. Scope

**In scope**

- Introspector unit and integration tests for all three dialects (Part A), and fixes
  for every confirmed §4 defect, including the parser-side half of a defect where the
  file parser has the same gap (generated columns on PG and MySQL).
- CLI wiring for `database` and `both` on postgres, mysql and sqlite (Part B). The
  wiring is dialect-generic, so doing one dialect costs the same as doing all three.
- The PRD §4.3 connection-field validation the config layer is missing.
- Exit-code mapping for connection vs schema errors.

**Out of scope**

- **A version matrix.** This work targets PG 16 and MySQL 8.0 (the containers
  `TestMain` already starts), plus the bundled modernc SQLite.
- **New config fields.** Everything here is already specified in PRD §4.2–§4.4, §6 and §16.2.

---

## 3. Current state (measured from code)

### 3.1 `parser/introspect` API

```go
type Introspector interface {
    Name() string
    Introspect(ctx context.Context, connString string, schema *parser.Schema, opts IntrospectionOptions) error
    Close() error
}
type IntrospectionOptions struct{ Schemas, ExcludeSchemas, Tables, ExcludeTables []string }

func NewPostgresIntrospector() *PostgresIntrospector  // sql.Open("pgx", connString)
func NewMySQLIntrospector() *MySQLIntrospector        // go-sql-driver DSN
func NewSQLiteIntrospector() *SQLiteIntrospector      // file path

type Seedable interface { parser.Parser; Seed(base *parser.Schema) }
func Merge(base *parser.Schema, p Seedable, files []string) error  // merge.go
```

- All three file parsers implement `Seed` (`parser/{postgres,mysql,sqlite}`).
- `Merge` seeds the parser and parses the files. CREATE on an existing table errors, and
  ALTER modifies the seeded table. Afterwards it checks for column-type conflicts. The
  result is read back from `p.Schema()`. `TestBothModeAlterOverlay` (`:643`) is the
  reference sequence; it is SQLite-only.
- Introspectors also populate `schema.Views`. PG adds materialized views
  (`postgres.go:65-68`).
- `parser.MergeViewAnnotations` (`parser/view.go:383`) implements PRD §16.2's
  "annotations take precedence" overlay. **Only `parser/view_test.go` calls it.**

### 3.2 Config

- **`InputConfig`** (`config/config.go:244`) has `Dialect`, `Source`, `Schema`, `Paths`, `Views`,
  `ParseMode`, `Connection *ConnectionConfig` and `Introspect *IntrospectConfig`.
- **`ConnectionConfig`** (`:256`) has `URL`, `Host`, `Port`, `User`, `Password`, `Database` and `SSLMode`.
- **Environment overrides** (`:1935`):
  - `SQLGEN_DB_URL` sets `URL` and clears the individual fields.
  - `SQLGEN_DB_PASSWORD` sets `Password`.
- **No DSN or connection-string builder exists.**
- **Validation:** `validateConnectionRequired` (`config/validate.go:1124`) only checks that
  `Connection != nil`. Nothing enforces PRD §4.3's "host/port/user required for postgres/mysql
  when `url` is empty" or "database required when `url` is empty".

### 3.3 CLI

- **`parseSchema` callers:** `cli/pipeline.go:44` (`loadAndValidate`, which `diff`, `lint` and
  `graphql` use), `cli/generate.go:45` and `cli/validate.go:46`.
- **Exit codes:** `generate` and `loadAndValidate` map any schema error to 4 under
  `database|both`. `validate` always maps it to 3.
- **`parseViewFiles`** (`cli/pipeline.go:235`) appends annotation-file views straight onto
  `schema.Views`. Combined with introspected views, that would duplicate any view defined in
  both places.
- **Module wiring:** `cmd/sqlgen/go.mod` lists neither the `parser` module nor any DB driver.
  `parser` resolves through `go.work`, and so will `parser/introspect`, together with the
  drivers its files blank-import (pgx, go-sql-driver/mysql and modernc sqlite are already in
  `parser/go.mod`).
- **No CLI test starts a container.** The harness is `executeCommand` (`cli/cli_test.go:16`)
  plus `writeTestConfig*` / `writeSQLFile` (`:1169`–`:1221`).

### 3.4 Who reads `Column.Default` text

The `files` vs `database` differential in §7.4 depends on this:

- **Generator:**
  - `gen/context_table.go:939` reads presence only (`HasDefault`).
  - `gen/context_table.go:1080` (`resolveDefaultExpr`) inlines the text, but only on SQLite.
- **Manifest:**
  - `manifest/builder.go:611` emits the literal as `default`.
  - `manifest/builder.go:668` classifies it into `default_kind` (literal / function / expression).

  PG normalizes defaults in the catalog (`'active'` → `'active'::status_enum`,
  `'{}'` → `'{}'::text[]`), so the manifest **will** differ between `files` and `database`
  on the same DDL unless the text is normalized. See Q5.

---

## 4. Introspection defects

§4.1–§4.3 started as suspicions from reading the code. The §5 probe (2026-10-05) measured
them: **every one that could be triggered was confirmed, and none was refuted.** §4.0 has
the status and evidence for each. The probe also found more defects, nine of them on the
**file-parser** side, listed in §4.4. IDs are prefixed by dialect (D = PostgreSQL,
M = MySQL, S = SQLite, X = all three). The probe confirmed three themes that cut across
dialects:

- **Generated columns** (D4, M1, S1). PRD §5.2 says every dialect's parser reads a
  column's "generated expression". Only the SQLite file parser does
  (`parser/sqlite/sqlite.go:223`), and no introspector does. The generator already
  excludes a column whose `GeneratedExpr` is set (`gen/context_table.go:942`
  `IsComputed`), so the fix on each path is to populate the field.
- **Default text** (§3.4, M2). Each catalog renders defaults differently from the DDL,
  which affects the manifest and, for MySQL, whether a default is detected at all.
- **Constraint shape** (X1). The introspectors and the file parsers disagree on which
  constraints become table-level `Constraint` entries, and what they're named. The manifest
  reads both.

### 4.0 Probe results (2026-10-05)

Measured against `postgres:16-alpine`, `mysql:8.0` and modernc SQLite, at `49c0d01d`.

| ID | Status | Evidence |
|---|---|---|
| D1 | **Confirmed** | Without `search_path`, the introspected type is `probe_app.status_enum`; with it, `status_enum`. The search path also changes the default text (`'active'::probe_app.status_enum`) and the view SQL (`FROM probe_app.users`). |
| D2 | **Confirmed** | `v_positive`: parsed `v > 0`, introspected `((v > 0)) NOT VALID` |
| D3 | **Confirmed** | `events_2024` and `events_2025` introspected as tables with full columns and their own `_pkey` |
| D4 | **Confirmed** | `total` introspected with `Default: "(price * (2)::numeric)"` and no `GeneratedExpr` |
| D5 | **Confirmed** | `CONSTRAINT always_ok CHECK (1 = 1)`: parsed, missing from introspection |
| D6 | **Confirmed in files mode** | `status app.status_enum` generates `Status string`, while `StatusEnum` is generated but unused (`sqlgen generate` on a three-line PG schema) |
| M1 | **Confirmed, both paths** | `total` (STORED) and `vtotal` (VIRTUAL) have an empty `GeneratedExpr` from the parser **and** the introspector |
| M2 | **Confirmed** | `DEFAULT ''`: parsed `''`, introspected `""` (no default). `'active'` → `active`, `'r'` → `r`. |
| M3 | **Confirmed** | `ENUM('active','in,active','it''s','')` introspected as `[active in active it''s]` |
| M4 | Code-confirmed, not triggered | `mysql.go:486` returns `nil` on any error. A cancelled context fails at ping, before the CHECK query, so the real test needs a mid-run trigger. |
| M5 | **Confirmed** | `INT(11)` → parsed `int(11)`, introspected `int`; `BIGINT(20) UNSIGNED` → `bigint(20) unsigned` vs `bigint unsigned`. `INT UNSIGNED` matches. |
| M6 | **Confirmed** | View column `status` introspected with `Type: "enum"` |
| M7 | **Confirmed** | Parsed `price >= 0`, introspected `` (`price` >= 0) `` |
| S1 | **Confirmed** | `total` / `vtotal` absent from introspection; the parser has them with `GeneratedExpr` / `GeneratedStorage` |
| S2 | **Confirmed** | `PRIMARY KEY (b, a)` introspected as `(a, b)` |
| S3 | **Confirmed** | `REFERENCES u` → "introspect sqlite foreign keys p: … converting NULL to string is unsupported", and the whole run fails |
| S4 | **Confirmed** | Every non-alias PK (`TEXT`, `INT`, composite, `WITHOUT ROWID`, `DESC`) gains a `sqlite_autoindex_*` UNIQUE and `Unique: true`. Inline UNIQUE constraints appear as `sqlite_autoindex_s_users_{1,2}`. |
| S5 | **Confirmed** | One FTS5 and one R*Tree table add 10 tables (`s_fts`, `s_fts_config`, `s_fts_content`, `s_fts_data`, `s_fts_docsize`, `s_fts_idx`, `s_rtree`, `s_rtree_node`, `s_rtree_parent`, `s_rtree_rowid`) |
| S6 | Code-confirmed, not triggered | No DB-level trigger, as expected |
| S7 | **Confirmed, worse than expected** | A table named `Quoted"Name` makes `PRAGMA table_info` fail, which **aborts the whole introspection**. It sorts first, so every other table was lost. |
| S8 | **Confirmed** | Both CHECKs on `s_users` parsed, none introspected |
| S9 | **Confirmed** | `WITHOUT ROWID` `INTEGER PRIMARY KEY`: introspected `AutoIncrement: true` (the parser is right with `false`); `sqlite3` rejects the insert without `id` (NOT NULL). See S11 for `DESC`. |

### 4.1 PostgreSQL

| # | Suspicion | Code | Consequence if real |
|---|---|---|---|
| D1 | `format_type()` schema-qualifies a type whose schema is not on `search_path` (`app.status_enum`). Every test sets `search_path`, and the CLI will not. | `postgres.go:381` `introspectColumns` | Enum- and domain-typed columns get a `Type` the parser never produces, so Go type resolution and the enum lookup diverge |
| D2 | `extractCheckExpression` strips the outer parentheses only when the text ends in `)`, so `CHECK ((a > 0)) NOT VALID` comes back unstripped as `((a > 0)) NOT VALID` | `postgres.go:927` | Wrong `CheckExpression` |
| D3 | `information_schema.tables … 'BASE TABLE'` lists a partitioned parent **and** each partition | `postgres.go:75` | A struct and client per partition, plus the parent |
| D4 | `pg_attrdef` also holds `GENERATED ALWAYS AS (…) STORED` expressions. The column comes back as a plain column whose `Default` is the expression, with no generated marker. The PG file parser doesn't model generated columns either. | `postgres.go:381`; `parser/postgres` | The column is writable in `Create<T>Input`, so the INSERT fails ("cannot insert a non-DEFAULT value into column"). PRD §10.3 says generated columns are excluded from CreateInput. |
| D5 | The constraint query inner-joins `unnest(con.conkey)`, so a CHECK that references no column (for example `CHECK (now() > '2000-01-01')`) has no rows and is dropped | `postgres.go:452` | A CHECK silently missing from the model |
| D6 | **Also affects files mode.** The type resolver registers enums, sets and domains by bare name only (`cmd/sqlgen/gotype/gotype.go:431`, looked up at `:625`), and the PG file parser keeps the DDL's qualifier (`parser/postgres/postgres.go:814` `typeNameToString`). So a column written `status app.status_enum` never resolves to the generated enum type. D1 makes this the normal case under `database`. | `gotype.go:431`, `:625`; `postgres.go:814` | The enum column falls through to the unknown-type path instead of the generated Go enum |

### 4.2 MySQL

| # | Suspicion | Code | Consequence if real |
|---|---|---|---|
| M1 | Generated columns are not detected. `extra` reads `VIRTUAL GENERATED` / `STORED GENERATED` and `generation_expression` is never selected. The MySQL file parser has no generated-column support either (no match for `GENERATED` in `parser/mysql/mysql.go`). | `mysql.go:199` `introspectColumns` | The generated column is writable in `Create<T>Input`, so the INSERT fails (MySQL error 3105) |
| M2 | `information_schema.columns.column_default` holds string literals **unquoted** (`'active'` → `active`, and `''` → empty string). An empty-string default sets `Default = ""`, which the generator reads as "no default" (`HasDefault: col.Default != ""`). | `mysql.go:234` | (a) A `NOT NULL DEFAULT ''` column becomes **required** in `Create<T>Input`. (b) The manifest's `default` / `default_kind` can't tell the literal `active` from an identifier, and it differs from the file path. |
| M3 | `extractMySQLEnumValues` splits `column_type` on every `,` and trims `'`. A value containing a comma (`'a,b'`) splits in two, a doubled quote (`'it''s'`) is mangled, and an empty value `''` is dropped. | `mysql.go:523` | Wrong enum or SET value list, so the generated constants and validation diverge from the DB |
| M4 | `introspectCheckConstraints` returns `nil` on **any** query error ("gracefully degrade on older MySQL"). That hides a cancelled context, a permissions error or a malformed query, not just a missing table. | `mysql.go:486` | CHECKs silently vanish. This conflicts with the loud-error preference. PRD §5.2 states no minimum MySQL version, so it's unclear what the degrade path is for. |
| M5 | In 8.0.19+, `column_type` drops integer display widths except `tinyint(1)`. A DDL `INT(11)` the file parser keeps as `int(11)` introspects as `int`. Not measured: what the file parser emits for `INT(11)` and `INT UNSIGNED`. | `mysql.go:510` `normalizeMySQLType` | A type that differs between files and database, so a type override keyed on `int(11)` matches one path only |
| M6 | View columns of ENUM type get the bare type `enum` (`introspectViewColumns` calls `normalizeMySQLType` without registering a synthetic enum), so the type has no values and no name | `mysql.go:163` | The view's enum column resolves through a fallback, or fails to resolve |
| M7 | CHECK `check_clause` comes back in MySQL's normalized form (backticked identifiers, charset introducers like `_utf8mb4'x'`), not the DDL text | `mysql.go:498` | The manifest's CHECK text differs between files and database. Cosmetic unless a consumer compares it. |

### 4.3 SQLite

| # | Suspicion | Code | Consequence if real |
|---|---|---|---|
| S1 | `PRAGMA table_info` omits generated columns. Only `PRAGMA table_xinfo` lists them, with `hidden` = 2 (virtual) or 3 (stored). The SQLite **file** parser does model them. | `sqlite.go:197` `introspectColumns` | Generated columns vanish from the model under `database`. Under `both`, an overlay that references one fails. |
| S2 | A composite PK's columns are collected in **column (cid) order**, not PK order. `pkCol.order` is captured but never sorted on. | `sqlite.go:234`, `:251` | `PRIMARY KEY (b, a)` introspects as `(a, b)`, so the composite-PK struct, `Get` signature and cursor order differ from the file path |
| S3 | `PRAGMA foreign_key_list` reports `to` as NULL when the DDL omits the parent column (`REFERENCES users`). It is scanned into a `string`. | `sqlite.go:295` | **Introspection fails** with "converting NULL to type string" on a legal schema |
| S4 | `introspectUniqueConstraints` takes every `unique == 1` index, including the auto-index behind a non-rowid PRIMARY KEY (`origin = 'pk'`, for example `id TEXT PRIMARY KEY` → `sqlite_autoindex_users_1`). Inline and table-level UNIQUE constraints also surface under their `sqlite_autoindex_*` names. | `sqlite.go:339` | An extra UNIQUE constraint duplicating the PK. Constraint and manifest index names that differ from the file path. |
| S5 | `sqlite_master WHERE type = 'table'` includes virtual tables (FTS5, R*Tree) and their shadow tables (`docs_fts_data`, `docs_fts_idx`, …), which aren't prefixed `sqlite_` | `sqlite.go:65` | A struct and client generated for every FTS shadow table |
| S6 | `indexPartialWhere` checks `err == sql.ErrNoRows || !ddl.Valid` **before** `err != nil`. Any other error leaves `ddl` invalid, so it returns `"", nil`. | `sqlite.go:483` | A query error silently drops a partial index's predicate, so the index looks total |
| S7 | PRAGMA statements quote names with Go's `%q`, not SQL identifier quoting. A table name containing `"` or `\` produces an invalid or wrong identifier. | `sqlite.go:164,199,272,341,413,565` | An introspection error on an exotic table name. Edge case. |
| S8 | No CHECK extraction at all. PRD §6.3 says "Limited", but the SQLite file parser does extract CHECKs, so files and database disagree. | `sqlite.go` | Manifest CHECK list differs by source. This is a PRD question, not necessarily a bug. |
| S9 | `AutoIncrement` is set for any single `integer` PK column. In a `WITHOUT ROWID` table, and for `INTEGER PRIMARY KEY DESC`, the column is **not** a rowid alias and gets no automatic value. | `sqlite.go:244` | The PK is left out of `Create<T>Input` (strategy `db`), so every insert fails the NOT NULL check |

### 4.4 Defects found by the probe

| # | Defect | Path | Evidence | Consequence |
|---|---|---|---|---|
| X1 | **Constraint shape and names differ by source.** <br>• The introspectors emit a table-level PK constraint for a single-column inline PK (PG `users_pkey`, MySQL `PRIMARY`), and an FK constraint for an inline `REFERENCES` (PG, SQLite); the parsers record both only on the column. <br>• An unnamed composite PK is `""` when parsed but `pairs_pkey` / `PRIMARY` when introspected. <br>• A table-level UNIQUE has `Method: ""` when parsed but `btree` when introspected (PG, MySQL). <br>• A PG CHECK has `Columns: nil` when parsed but `[col]` when introspected. | All three introspectors and parsers | PG `checks`, `pairs`, `users`, `accounts`; MySQL `MixedCase`, `m_pairs`, `m_users`; SQLite `s_posts` | The manifest's index and constraint names and lists differ by source (`builder.go:917-958` derives names from these) |
| D7 | PG typmod spacing: the parser writes `numeric(12, 2)`, the catalog `numeric(12,2)`. Override lookup is exact-then-base (`gotype.go:582`). | PG parser | `users.price` | `overrides.types."numeric(12,2)"` matches introspected columns but **not parsed ones**, which fall back to the `numeric` override |
| D8 | PG CHECK and domain text: the catalog adds casts and parentheses (`score < 1000` → `((score)::integer < 1000)`) and keeps keyword case (`value > 0` → `(VALUE > 0)`). | PG introspector | `users_score_check`, `positive_int_check` | The manifest's CHECK text differs by source |
| D9 | **PG parser:** `CREATE TABLE … PARTITION OF …` adds a table with **no columns** | PG parser | `events_2024` / `events_2025` parsed with `Columns: nil` | Under files mode, a partitioned schema generates broken column-less entities |
| M8 | **MySQL parser:** an enum value written `'it''s'` parses as `it\'s`, with a stray backslash | MySQL parser | `m_users_status_enum` | Wrong generated enum constant |
| M9 | MySQL default keyword case: the parser writes `current_timestamp(3)`, the catalog `CURRENT_TIMESTAMP(3)` | MySQL introspector / parser | `m_users.created_at` | The manifest's `default` differs by source. Covered by Q5 once the canonical form is chosen. |
| S10 | **SQLite parser:** `id INTEGER PRIMARY KEY DESC` adds a phantom column named `DESC` (type `blob`) | SQLite parser | `s_desc` parsed with columns `id`, `DESC`, `v` | A bogus field on the generated struct, and INSERTs naming a column that doesn't exist |
| S11 | **Both SQLite paths:** `INTEGER PRIMARY KEY DESC` is marked `AutoIncrement`, but it is not a rowid alias | SQLite parser and introspector | `sqlite3`: an insert without `id` silently stores `NULL` | The PK is left out of `Create<T>Input`, so every row is inserted with a `NULL` key |
| S12 | The SQLite introspector never sets `WithoutRowID` or `Strict` | SQLite introspector | `s_wr` parsed `WithoutRowID: true`, introspected `false`; a `STRICT` table parsed `true`, introspected `false` | Anything that branches on these flags differs by source |

**Matched on every path, no work needed:** SQLite default text (`''`, `1.5`,
`CURRENT_TIMESTAMP`, `1 + 2`). MySQL `INT UNSIGNED`, `tinyint(1)`, `decimal(10,2)`,
comments, composite FK column order. PG composite PK and FK column order, identity columns
(`AutoIncrement`), enum values, and cross-schema FK `ReferenceSchema`.

---

## 5. Step 0 — probe protocol (run 2026-10-05)

The probe harness is a throwaway `zz_probe_test.go` in a detached worktree. It reuses
`TestMain`'s containers, parses the DDL one statement at a time, and writes both schemas
plus a `cmp.Diff` per dialect. It is **not committed**; the harness and its output are kept in the gitignored `.claude/prototypes/introspection-probe/`, so it can be re-run by copying it into `parser/introspect/` and running with `PROBE_OUT` set. N.1 turns its DDL and assertions into
the permanent §6 tests. It ran without any parse errors: every probe statement parsed,
though D9 and S10 parsed into a wrong model.

The probe runs before any assertion is written (memory: probe before deciding). It uses the
example probe harness (a worktree, a throwaway copy and `GOWORK=off`) and a PG16 container:

1. Apply one schema per dialect, covering that dialect's §4 suspicions and its §6 type list.
   The PG schema goes in a non-`public` schema; MySQL uses the test database; SQLite uses a
   temp file.
2. Introspect each one. For PG, run once **without** `search_path` in the connection string
   and once with it.
3. Parse the same DDL with that dialect's file parser.
4. Record both `parser.Schema` dumps per dialect in the phase's first sub-item.

The probe sorts each test in §6 into one of two kinds:

- **Pin**: the current behavior is correct, so the test documents it.
- **Fail-first**: the test fails today. The fix follows the §10 ruling for that defect.
  If the probe finds a defect §10 doesn't cover and the fix is design-shaped, work stops
  and the question goes to the user. The test is never skipped or gated off in the
  meantime.

---

## 6. Part A — Introspector tests

### 6.0 Shared parity harness

Today each dialect's main test hand-picks which `Column` fields it compares: PG uses
`cmp.Diff` with two ignored fields, MySQL compares five fields, and SQLite compares four
(not even `Type`). That drift is how most of §4 went unnoticed. All three move onto one
helper in `introspect_integration_test.go`:

```go
// assertSchemaParity compares an introspected schema against the file parser's
// schema for the same DDL: tables, columns, constraints, enums, sets, composite
// types, domains and views, in full. ignore lists the documented differences;
// each entry names the field and the catalog reason it cannot round-trip.
func assertSchemaParity(t *testing.T, dialect string, parsed, introspected *parser.Schema, ignore ...parityException)
```

- **Full comparison by default.** Each dialect test starts from a full `cmp.Diff` of
  both sorted schemas, with constraints sorted too.
- **One list of exceptions.** A field may be excluded only through a named
  `parityException` with a reason: a field that is parse provenance, not stored in any
  catalog. (The one such field, `Column.InlineUnique`, went with the canonical constraint
  model, so the table is empty.) All the exceptions live in a single table near the top of
  the file, so a reviewer can see each dialect's full set at a glance.
- **The same rule for every dialect.** If a field round-trips on one dialect's catalog but
  not another's, the dialect it fails on must list an exception for it.

Every dialect has the same set of integration tests (§6.2–§6.4). Each test name exists in
all three dialects unless the feature doesn't exist there; enums are PG/MySQL, partitioning
and `search_path` are PG-only, and virtual tables are SQLite-only. Those differences are
listed in §6.5.

The tests follow TESTING.md: table-driven, `cmp.Diff`, no assertion libraries, `t.Error`
over `t.Fatal`, and a `-short` skip through `requireDatabases`. Each integration test
owns a schema, using the existing `CREATE SCHEMA IF NOT EXISTS` + `DROP … CASCADE` pattern,
and reuses `findTable`, `findConstraintByName`, `filterConstraints` and `truncate`.

### 6.1 PostgreSQL unit tests (`postgres_test.go`, white-box)

| Test | Cases |
|---|---|
| `TestNormalizePostgresType` | `character varying(255)`; `character(3)`; `timestamp(3) without time zone`; `timestamp with time zone`; `time with time zone`; arrays of each; `double precision`; `bit varying(8)`; schema-qualified name (D1); passthrough |
| `TestParsePostgresArray` | `{}`; plain list; quoted element containing a comma or space; escaped quote; mixed-case quoted identifier |
| `TestExtractCheckExpression` | `CHECK ((a > 0))`; compound `AND` / `OR`; `NOT VALID` suffix (D2); non-CHECK passthrough |

### 6.2 PostgreSQL integration tests (`introspect_integration_test.go`)

| Test | Pins |
|---|---|
| `TestPostgresIntrospect_TypeCoverage` | One wide table: `uuid`, `numeric(12,2)` (both paths emit it without a space, Q12 / D7), `bytea`, `interval`, `inet`, `smallserial`, `serial`, `int GENERATED ALWAYS AS IDENTITY`, `bigint GENERATED BY DEFAULT AS IDENTITY` (both → `AutoIncrement`), an enum column, a domain column, and `status_enum[]`. Each column is checked against the file parser **and** an explicit expected `Type` string, so a shared parser/introspector bug can't hide. |
| `TestPostgresIntrospect_TableConstraints` | Canonical constraint shape (Q11 / X1): inline and table-level PK, FK, UNIQUE and CHECK all appear as table constraints with the database's default names on both paths (including a name past the 63-byte limit). Plus composite PK, composite UNIQUE, a multi-column FK (with `Columns` / `ReferenceColumns` order), a cross-schema FK (`ReferenceSchema`) and a self-referencing FK. Compares the full sorted `Constraints` against the parser. |
| `TestPostgresIntrospect_Defaults` | A literal, `now()`, `gen_random_uuid()`, an enum literal, a `'{}'` array, a serial column, and an identity column. `Default` equals the file parser's text for each (Q5): catalog-added casts on literals are stripped, and serial / identity carry no default, only `AutoIncrement`. |
| `TestPostgresIntrospect_CheckAndDomainConstraints` | CHECK and domain text equal the parser's after Q13 normalization, including a CHECK on a domain-typed column (`(score)::integer` cast, D8) and `VALUE` case. Also a `NOT VALID` CHECK (D2), a column-less CHECK (D5), and a domain CHECK name plus expression |
| `TestPostgresIntrospect_NoSearchPath` | Enum- and domain-typed columns in a non-`public` schema, introspected without `search_path`, come back unqualified when the type lives in the column's own schema, and qualified when it is cross-schema (Q3, D1) |
| `TestPostgresIntrospect_QuotedIdentifiers` | A `"UserAccounts"` table, a `"createdAt"` column, and a constraint over quoted columns |
| `TestPostgresIntrospect_Partitioning` | A partitioned parent with two partitions gives the parent only on both paths; the parser skips `PARTITION OF` with a warning (Q14 / D9) (Q4, D3) |
| `TestPostgresIntrospect_GeneratedColumn` | A STORED generated column has `GeneratedExpr` / `GeneratedStorage = "STORED"` and no `Default`, the same as the file parser (Q1, D4) |

The existing `TestPostgresIntrospect` (`:102`) also gets tighter. It will compare the
PK / UNIQUE / FK constraints (today it compares only CHECK), assert that CHECK expressions are
equal, and compare domain `Constraints`.

### 6.3 MySQL

Unit tests (`mysql_test.go`, new, white-box):

| Test | Cases |
|---|---|
| `TestNormalizeMySQLType` | `int`; `int unsigned`; `tinyint(1)`; `bigint(20)` (pre-8.0.19 form); `decimal(10,2)`; `enum(…)` / `set(…)` → bare; mixed case |
| `TestExtractMySQLEnumValues` | Plain list; a value with a comma (M3); a doubled quote `''` (M3); an empty value (M3); values with spaces |

Integration tests:

| Test | Pins |
|---|---|
| `TestMySQLIntrospect` (tightened) | Full `cmp.Diff` per column against the file parser, the same shape as the PG test: `Unique`, `FKReference` and `Comment` added; `Default` per Q5. Table-level constraints compared in full. |
| `TestMySQLIntrospect_TypeCoverage` | `INT(11)` and `BIGINT(20) UNSIGNED` (M5), `INT UNSIGNED`, `BIGINT UNSIGNED`, `tinyint(1)`, `decimal(10,2)`, `json`, `datetime(3)`, `binary(16)`, `varbinary`, `blob`, `ENUM`, `SET` (M5) |
| `TestMySQLIntrospect_Defaults` | `DEFAULT ''` (M2), a string literal, an ENUM literal, `CURRENT_TIMESTAMP` and its synonyms (`NOW(3)`, `LOCALTIMESTAMP`, `(CURRENT_DATE)`, `(CURTIME())`, which both sources write as `current_timestamp(3)`, `current_timestamp()`, `current_date()`, `current_time()`), `(uuid())`, `DEFAULT NULL`, a numeric literal. `Default` equals the file parser's text for each (Q5). A `NOT NULL DEFAULT ''` column ends up omittable, not required. |
| `TestMySQLIntrospect_GeneratedColumn` | `VIRTUAL` and `STORED` generated columns produce the same `GeneratedExpr` / `GeneratedStorage` on both paths (M1, Q1) |
| `TestMySQLIntrospect_EnumEdgeValues` | ENUM values with a comma, a quote and an empty string give the same values on both paths (M3 introspector, M8 parser) |
| `TestMySQLIntrospect_TableConstraints` | Canonical constraint shape and MySQL default names (`PRIMARY`, `<t>_ibfk_N`, `<t>_chk_N`, first-column UNIQUE name with `_2` on collision) on both paths (Q11 / X1); CHECK text after Q13 (M7) |
| `TestMySQLIntrospect_CompositeConstraints` | Composite PK, composite UNIQUE, a multi-column FK (with column order) and a self-referencing FK |
| `TestMySQLIntrospect_CheckErrorsSurface` | A CHECK query failure returns an error rather than dropping the CHECKs (M4, Q7). The probe showed that a context cancelled up front fails at ping, before the CHECK query, so the test cancels mid-run: it dials through a connection wrapper that cancels the context when the CHECK query is written, after every earlier catalog query has succeeded. A white-box unit test covers the error-1109 fallback, since MySQL 8.0 can't produce it. |
| `TestMySQLIntrospectViews` (tightened) | A view projecting an ENUM column (M6) |
| `TestMySQLIntrospect_QuotedIdentifiers` | A `` `UserAccounts` `` table, a `` `createdAt` `` column, a reserved-word column (`` `order` ``), and a constraint over them |

### 6.4 SQLite

Unit tests: `TestNormalizeType` (`parser/sqlite/sqlite_test.go`) covering empty → `blob`,
case folding, and `VARCHAR(255)` / `NUMERIC(10,2)` passthrough, plus spaces inside a modifier
and unrecognized → `blob`.

Integration tests:

| Test | Pins |
|---|---|
| `TestSQLiteIntrospect` (tightened) | Full `cmp.Diff` per column against the file parser. **`Type` is compared** (today it isn't). `Default` per Q5. |
| `TestSQLiteIntrospect_GeneratedColumn` | `GENERATED ALWAYS AS (…) VIRTUAL` / `STORED` and the shorthand `AS (…)` are present, with `GeneratedExpr` / `GeneratedStorage` matching the parser (S1) |
| `TestSQLiteIntrospect_CompositePKOrder` | `PRIMARY KEY (b, a)` keeps `b, a` order (S2) |
| `TestSQLiteIntrospect_ImplicitFKTarget` | `REFERENCES u` with no column list, at column and table level, fails on both paths with the parse error, and the introspector's error names the table (S3, ruled 2026-10-05) |
| `TestSQLiteIntrospect_UniqueAndPKAutoIndexes` | `id TEXT PRIMARY KEY` gives no duplicate UNIQUE. Inline, named and table-level UNIQUE constraints get the same names as the file parser, taken from the stored DDL (Q8, S4). |
| `TestSQLiteIntrospect_PartialIndexes` | Partial-index predicates (`IS NOT NULL`, a lowercase `where`, `WHERE` inside a string literal and a comment) are the file parser's text on both paths |
| `TestSQLiteIntrospect_UnreadablePartialIndex` | A partial index with `IS NOT DISTINCT FROM` or `LIKE … ESCAPE` in its `WHERE` fails both paths with an error, not a panic; introspection's is a `*SchemaError` naming the index |
| `TestSQLiteIntrospect_VirtualTables` | An FTS5 table and an R*Tree table, plus their shadow tables, are all skipped, and `schema.Warnings` names each virtual table (Q9, S5). The file-parser side gets the same warning test in `parser/sqlite`. |
| `TestSQLiteIntrospect_QuotedNames` | A table and column whose names contain `"`, a space and mixed case (S7) |
| `TestSQLiteIntrospect_Checks` | Column-level, table-level and named CHECK constraints match the file parser, taken from the stored DDL (Q10, S8) |
| `TestSQLiteIntrospect_TableFlags` | `WITHOUT ROWID` and `STRICT` round-trip (S12) |
| `TestSQLiteIntrospect_RowidAlias` | `INTEGER PRIMARY KEY DESC` gives one `id` column (no phantom `DESC`, S10) that is **not** auto-increment on either path (S11). `INTEGER PRIMARY KEY` → auto-increment. The same column in a `WITHOUT ROWID` table, `INTEGER PRIMARY KEY DESC` and `INT PRIMARY KEY` → not auto-increment, matching the file parser (S9). |
| `TestSQLiteIntrospect_TypeCoverage` | `INTEGER`, `TEXT`, `REAL`, `BLOB`, `NUMERIC`, a declared `VARCHAR(255)`, `BOOLEAN`, `DATETIME`, and a column with no type → `blob`. Each is checked against the file parser and an explicit expected `Type`. |
| `TestSQLiteIntrospect_Defaults` | A string literal, a numeric literal, `CURRENT_TIMESTAMP`, a parenthesized expression, and `NULL`. Pins the `Default` text, which SQLite inlines into `CreateMany` (`resolveDefaultExpr`), so it **must** be valid SQL. |
| `TestSQLiteIntrospect_CompositeConstraints` | Composite UNIQUE, a multi-column FK and a self-referencing FK |

### 6.5 Parity matrix

Each concern below gets a test on every dialect where the feature exists. "—" means the
dialect has no such feature, with the reason given. A cell is never empty because a test
was skipped.

| Concern | PostgreSQL | MySQL | SQLite |
|---|---|---|---|
| Full schema parity (§6.0 helper) | `TestPostgresIntrospect` | `TestMySQLIntrospect` | `TestSQLiteIntrospect` |
| Type coverage | ✓ | ✓ | ✓ |
| Defaults | ✓ | ✓ (M2) | ✓ |
| Composite / multi-column constraints | ✓ | ✓ | ✓ (S2) |
| CHECK constraints | ✓ (D2, D5) | ✓ (M4, M7) | ✓ (S8) |
| Generated columns | ✓ (D4) | ✓ (M1) | ✓ (S1) |
| Auto-increment / identity | ✓ | ✓ | ✓ (S9, S11) |
| Constraint shape + default names (Q11) | ✓ (X1) | ✓ (X1) | ✓ (X1, Q8) |
| Table flags | — | — | ✓ `WITHOUT ROWID`, `STRICT` (S12) |
| Quoted / mixed-case identifiers | ✓ | ✓ | ✓ (S7) |
| Enums | ✓ (D1) | ✓ (M3) | — no enum type |
| Views (+ enum projection) | ✓ | ✓ (M6) | ✓ |
| Indexes: method / partial / non-unique | ✓ | ✓ | ✓ (S4, S6) |
| Schema filtering | ✓ | — single database | — single database |
| Table filtering | ✓ | ✓ | ✓ |
| `search_path` / type qualification | ✓ (D1) | — | — |
| Partitioned tables | ✓ (D3) | — one entry per table in `information_schema` | — |
| Virtual / shadow tables | — | — | ✓ (S5) |
| Domains, composite types | ✓ | — | — |
| `both`-mode merge (`introspect.Merge`) | ✓ (new) | ✓ (new) | ✓ (existing) |

The three `both`-mode tests (`TestBothMode*`) are SQLite-only today. Each one becomes a
table over the three dialects.

S6 has no reliable DB-level trigger, so it gets a code fix (swap the `err` checks) without a
dedicated test.

---

## 7. Part B — CLI wiring

### 7.1 Connection string (`cli/connection.go`, new)

```go
func connString(dialect config.Dialect, c *config.ConnectionConfig) (string, error)
```

| Dialect | `url` set | Built from fields |
|---|---|---|
| postgres | verbatim (PRD §4.3: precedence) | `postgres://user:pass@host:port/db?sslmode=<ssl_mode, default disable>`, built with `net/url` so a password containing `@` / `:` / `/` is escaped. A `host` starting with `/` is a Unix-socket directory and goes in `?host=<dir>&port=<port>` |
| mysql | verbatim | go-sql-driver DSN `user:pass@tcp(host:port)/db?tls=<ssl_mode, default false>`. A `host` starting with `/` is the socket file and gives `unix(<file>)`, with no port. A `user` containing `:` is a config error, since the DSN cannot escape it |
| sqlite | verbatim | `database` (file path) |

A missing required field returns an error that names `input.connection.<field>`. The
§4.3 rules also move into config validation, next to `validateConnectionRequired`, so
`sqlgen validate` reports them all together with the other config errors:

- when `url` is empty, `database` is required on every dialect
- when `url` is empty, `host` and `user` are required on postgres and mysql

`port` falls back to the dialect default (5432 / 3306).

### 7.2 `parseSchema(ctx, cfg)`

`parseSchema` gains a `ctx` parameter. Its three callers pass `cmd.Context()`.

| `source` | Behavior |
|---|---|
| `files` | Unchanged. |
| `database` | `introspectSchema` only. `paths` is ignored, including its `["."]` default. |
| `both` | `introspectSchema` into `base`, then `introspect.Merge(base, newSeedableParser(dialect, schemaDefault), files)`, and the schema is `p.Schema()`. |

`introspectSchema(ctx, cfg)`:

- picks the introspector by dialect
- builds the connection string (§7.1)
- maps `input.introspect.{schemas, exclude_schemas, tables}` to `IntrospectionOptions`
- `defer`s `Close()`
- wraps a failure in `*connectionError` (§7.3)

Top-level `exclude_tables` is not passed down, so it stays where it is applied today: for
every source, after parsing.

**Views (PRD §16.2).** `parseViewFiles` is refactored to return the annotated views and the
annotation map instead of appending them. Under `database | both` the CLI combines them with
`parser.MergeViewAnnotations(introspected, annotated, annotations)`. Under `files` it keeps
today's append.

**Unresolved-FK check.** The PG `parser.UnresolvedForeignKeys` check runs on the final schema:
the merged one under `both` and the introspected one under `database`. A file FK into a
DB-only table therefore resolves, which is the ordering FIX-243 Q3(a) deferred to this work.

### 7.3 Exit codes

Every `parseSchema` caller (`generate`, `validate`, `loadAndValidate`) maps errors the same
way:

```go
if _, ok := errors.AsType[*connectionError](err); ok { code = ExitConnection } // 4
else { code = ExitSchema }                                                       // 3
```

This replaces the source-mode remap. A schema error (merge conflict, unresolved FK, bad
overlay DDL) exits 3 in every mode, and only a failure to connect or query the catalog
exits 4 (Q2).

### 7.4 CLI live-DB tests (`cli/introspect_integration_test.go`, new)

**Setup**

- **Containers:** PG16 and MySQL 8.0 each start lazily through a `sync.Once` helper
  rather than `TestMain`, so the rest of the `cli` package doesn't wait on a container.
  SQLite uses a temp file. The helpers skip under `testing.Short()`.
- **Dependencies:** `testcontainers-go` and its postgres and mysql modules are added to
  `cmd/sqlgen/go.mod` as test-only requirements. That's fine because `cmd/sqlgen` is not a
  runtime core package.
- **One table, all dialects:** every test below runs as subtests named `postgres`, `mysql`
  and `sqlite` over one `dialectFixture` slice. Each fixture holds a starter, the dialect's
  DDL, and its config fragment. A test that cannot apply to a dialect says why in the
  fixture; it is never silently skipped.
- **Helpers:** the tests reuse `executeCommand` and `writeSQLFile`, plus a new
  `writeTestConfigWithSource` helper next to `writeTestConfig`.

**Tests**

| Test | Asserts |
|---|---|
| `TestGenerate_DatabaseSourceMatchesFiles` | Applies each dialect's representative DDL, then runs `generate` with `source: files` and again with `source: database`. The two generated trees, **including the manifest**, are **byte-identical**. Q5 normalizes defaults to the parser's form, so an allowed difference must be a named parity exception with a catalog reason. The DDL per dialect: <br>• **PG:** enum, domain, composite PK, FKs including a cross-schema one, identity, `uuid` / `numeric` / `jsonb`, a generated column, comments, a view with an annotation file <br>• **MySQL:** ENUM / SET, composite PK, FKs, AUTO_INCREMENT, `DEFAULT ''`, unsigned ints, a generated column, comments, a view <br>• **SQLite:** rowid alias, composite PK in non-column order, a self-referencing FK, a generated column, UNIQUE, CHECK, a view |
| `TestGenerate_BothSourceAppliesOverlay` | The DB has `users`. The file has `ALTER TABLE users ADD COLUMN nickname text` and `CREATE TABLE posts`. Both show up in the output. A `CREATE TABLE users` file instead exits 3 with the merge error. |
| `TestGenerate_DatabaseSourceIgnoresPaths` | `paths` points at a file with a conflicting `users` table. The output reflects the DB only. |
| `TestGenerate_IntrospectFilters` | `introspect.tables` narrows the generated set on all three dialects; `schemas` / `exclude_schemas` do too on PG. |
| `TestGenerate_IntrospectedViewWithAnnotation` | A DB view plus an `input.views` file with `@pk` gives one view, with the PK applied. |
| `TestGenerate_ConnectionFailureExitCode` | An unreachable host (PG, MySQL) or a missing or unreadable file (SQLite) exits 4 for both `generate` and `validate`. An overlay schema error exits 3. |
| `TestGenerate_DatabaseSourceCompiles` | The `database` output for each dialect builds with `go build` (the probe-harness pattern: `GOWORK=off` with a `replace` to the runtime module) |
| `TestConnString` (unit, no container) | Table-driven over the three dialects: `url` precedence, password escaping, `ssl_mode` defaults, and missing-field errors. |

---

## 8. PRD impact

**Behavior changes:** the PRD already specifies `database`, `both`, the connection
fields, view overlay and generated columns on every dialect (§5.2); this work
implements them. The rulings in §10 add the edits below.

**Wording to confirm in §4.2 / §6.1:**

- `paths` is ignored under `source: database`, including its default.
- `input.introspect.*` has no effect under `source: files`.

**Edits from the §10 rulings:**

- **§5.2 PostgreSQL / MySQL parser bullets (Q1):** list the generated-column syntax each
  parser accepts, matching the existing SQLite bullet.
- **§6.1 or a new §6.5 (Q5):** introspected defaults are normalized to the file parser's
  form, so `database` and `files` give the same manifest `default` / `default_kind`.
- **§6.3 table:**
  - Q4: partitions are skipped and only the partitioned parent is introspected.
  - Q9: SQLite virtual and shadow tables are skipped with a warning.
  - Q10: SQLite "Unique constraints" and "Check constraints" change from "Limited" to
    "Yes", with a note that names and CHECK text come from the stored `CREATE TABLE`.
  - Q7: a footnote that MySQL CHECK introspection needs 8.0.16+ and older servers yield no
    CHECKs; no hard minimum version.
- **§5.2 SQLite bullet (Q9):** `CREATE VIRTUAL TABLE` is skipped with a warning, like
  `CREATE VIEW`.
- **§7 type resolution (Q3):** an enum, set or domain resolves under its bare or
  schema-qualified name, and the parsers canonicalize a type in the column's own schema to
  the bare name. A bare name is looked up in the owner's schema first (amended 2026-10-05, 30.4).
- **§7 type resolution (Q12):** PG type modifiers are written without a space
  (`numeric(12,2)`), which is the form `overrides.types` keys match.
- **§5.2 PostgreSQL bullet (Q14):** `CREATE TABLE … PARTITION OF` is skipped with a
  warning; the partitioned parent is the entity.
- **§5.2 / §30.7 (Q11):** every PK, FK, UNIQUE and CHECK is a table constraint on both
  paths, and an unnamed constraint gets the name the database would give it. The manifest's
  index names follow from that.
- **§6.1 (Q13):** CHECK and domain-constraint text from the catalog is normalized to the
  file parser's form, like defaults.
- **§8.1.7 testing strategy (Q2):** exit 4 means a connection or catalog failure only; a
  schema error exits 3 under every `source`.

---

## 9. Verification

- **Parser unit tests:** `cd parser && go test -short ./introspect/...`
- **Parser integration tests:** `cd parser && go test -race -count=1 ./introspect/...` (needs Docker)
- **CLI:** `cd cmd/sqlgen && go test -race -count=1 ./cli/...`
- **Full sweep:** `make check` and `make test-integration`
- **Manual:** run a throwaway `sqlgen.yml` with `source: database` against a local
  container for each dialect (a file for SQLite), run `sqlgen generate`, then
  `go build ./...` on the output.

---

## 10. Resolved questions

All fourteen were ruled on 2026-10-05, each matching the recommendation. Q1–Q10 came from reading the code. Q11–Q14 came from the §5 probe. No question is open.

| # | Question | Ruling | Lands in |
|---|---|---|---|
| Q1 | Generated columns (D4, M1, S1) | **Fix all five gaps in this phase.** PG introspector reads `pg_attribute.attgenerated` and sets `GeneratedExpr` / `GeneratedStorage` instead of `Default`. PG parser learns `GENERATED ALWAYS AS (…) STORED`. MySQL introspector reads `extra` + `generation_expression`. MySQL parser learns `GENERATED ALWAYS AS (…) [VIRTUAL\|STORED]` and the `AS (…)` shorthand. SQLite introspector switches to `PRAGMA table_xinfo` (`hidden` 2 = VIRTUAL, 3 = STORED). | N.3 |
| Q2 | Exit codes | **Split 4 vs 3.** A typed `*connectionError` (connect, ping, or catalog query) exits 4; every other `parseSchema` error exits 3, in `generate`, `validate` and `loadAndValidate` alike. Settles FIX-243 Q3 as option (b). | N.9 |
| Q3 | Schema-qualified type names (D1, D6) | **Resolver plus canonical form.** The resolver also registers `schema.name` keys for every enum, set and domain, so either spelling resolves. A bare key is registered only when no other schema has the same name, so two same-named enums can't collide. Both the PG introspector and the PG parser canonicalize: a type in the column's own schema becomes bare; a cross-schema type stays qualified. That keeps the manifest's `sql_type` identical across sources and fixes the latent files-mode bug. Amended 2026-10-05 (30.4): a bare name is looked up in the owning table's, view's or type's schema first (PRD §7.1). | N.4 (introspector, PG parser, and the `cmd/sqlgen/gotype` resolver) |
| Q4 | Partitioned tables (D3) | **Parent only.** Skip tables with `pg_class.relispartition`. | N.4 |
| Q5 | Default text (§3.4, M2) | **Normalize to the file parser's form.** <br>• **PG:** catalog text goes through the same `pg_query` deparse the parser uses, catalog-added casts on literals are stripped, and serial / identity columns carry no `Default` (only `AutoIncrement`), as in the parser. <br>• **MySQL:** literals are re-quoted from `column_type` (string, enum, set and temporal literal columns), `DEFAULT_GENERATED` in `extra` marks an expression default, and the result goes through the same `sqlparser.String` form. This also fixes M2's empty-string bug. <br>• **SQLite:** `dflt_value` goes through the same `rsql` `ExprString`. <br>Anything that still can't match becomes a named parity exception (§6.0). | N.4, N.5, N.6 |
| Q6 | Tracking | **New phase,** run with `/phase` from this doc once it's accepted. | — |
| Q7 | MySQL CHECK errors (M4) | **Narrow fallback.** Only MySQL error 1109 (unknown table, i.e. a server older than 8.0.16) yields "no CHECKs"; every other error is returned. No PRD minimum version; §6.3 gets a footnote. | N.5 |
| Q8 | SQLite UNIQUE names (S4) | **Recover from the stored DDL.** Always drop the `origin = 'pk'` index. UNIQUE constraint names come from re-parsing `sqlite_master.sql` (see Q10). | N.6 |
| Q9 | SQLite virtual tables (S5) | **Skip both, with a warning.** The introspector skips `type = 'virtual'` and `type = 'shadow'` rows from `PRAGMA table_list` (SQLite 3.37+, met by bundled modernc). Both paths append a `schema.Warnings` line naming the virtual table, like the existing `CREATE VIEW` skip. The file parser currently ignores `CREATE VIRTUAL TABLE` without a word (`parser/sqlite/sqlite.go:63-81`). | N.6 |
| Q10 | SQLite CHECK constraints (S8) | **Re-parse the stored DDL; this is the general SQLite strategy.** The PRAGMAs give the catalog facts (columns, types, nullability, PK membership and order, FKs, indexes, which indexes are partial). Each table's `CREATE TABLE` text from `sqlite_master.sql`, parsed with the existing `parser/sqlite` parser, gives the declared names, CHECK constraints and expression text, and each partial index's stored `CREATE INDEX`, read the same way, gives its predicate. Settles S8 and Q8, and backs up S2. | N.6 |
| Q11 | Constraint shape and names (X1) | **Canonical shape plus the database's names.** A shared `parser.CanonicalizeConstraints` runs at the end of every parse and every introspection. Every PK, FK, UNIQUE and CHECK exists both as a table `Constraint` and as the column flags (`PrimaryKey`, `Unique`, `FKReference`). UNIQUE / Index `Method` defaults to `btree` on PG and MySQL. CHECK `Columns` lists the referenced columns on every dialect. The parsers give each unnamed constraint the name its database would: <br>• **PG:** `<t>_pkey`, `<t>_<cols>_key`, `<t>_<cols>_fkey`, `<t>_<col>_check` (`<t>_check` when the CHECK references no column or several), applying PG's 63-byte truncation and numeric collision suffix (`ChooseConstraintName`) <br>• **MySQL:** `PRIMARY`, `<t>_ibfk_<n>`, `<t>_chk_<n>`, and a UNIQUE named after its first column, with `_2`, `_3` on collision <br>• **SQLite:** declared names only (the stored DDL, Q8 / Q10); an unnamed constraint stays unnamed on both paths <br>The manifest's index names then match the database. This also resolves two FIX-240 backlog lines from 2026-10-02: unnamed UNIQUE listed as `_idx` where PG says `_key`, and the PG parser dropping an inline `CONSTRAINT <name> UNIQUE` name. | N.2 |
| Q12 | PG typmod spacing (D7) | **No space.** The PG parser's typmod formatting emits `numeric(12,2)`, matching the catalog, MySQL's `decimal(10,2)`, and how users write `overrides.types` keys. | N.4 |
| Q13 | PG / MySQL CHECK text (D8, M7) | **Normalize to the parser's form and strip implicit casts.** Catalog CHECK and domain-constraint text goes through the same deparser as the file parser (`pg_query` on PG, `sqlparser` on MySQL). Then the casts the database adds implicitly are removed: domain → base type, literal → column type. MySQL backticks and charset introducers are dropped as well. Each removal rule gets its own unit test, and anything left is a named parity exception. | N.4, N.5 |
| Q14 | PG `PARTITION OF` in the parser (D9) | **Skip with a warning.** The parser skips `CREATE TABLE … PARTITION OF` and appends a `schema.Warnings` line, like `CREATE VIEW` and `CREATE VIRTUAL TABLE` (Q9). The introspector skips `relispartition` tables silently (Q4). Output is identical across sources. | N.4 |

**Q11 addendum (ruled 2026-10-05, while wording the PRD in 30.0).** A probe ran `email UNIQUE` +
`UNIQUE(email)`, `id … PRIMARY KEY UNIQUE` and a named `CONSTRAINT named_u UNIQUE(b)`.
PG 16 and SQLite collapse the duplicates. MySQL 8.0 keeps them (`id`, `email_2`, warning 1831).
SQLite names every auto-index `sqlite_autoindex_<t>_N`, the named one included. Two rulings
follow:
- **Duplicates follow the database.** A UNIQUE that repeats the PK or an earlier UNIQUE over the
  same columns is dropped on PG and SQLite and kept on MySQL. This replaces the old §30.7 sentence
  that listed every inline UNIQUE.
- **Unnamed SQLite manifest label.** An unnamed SQLite PRIMARY KEY or UNIQUE is listed in the
  manifest as `<t>_pkey` or `<t>_<cols>_key`, inline or table-level alike. The `_idx` label goes.
  The constraint itself stays unnamed in the schema model.
- **Refined by a second probe.** PG merges duplicates only within one `CREATE TABLE`, into the
  first, under the first declared name; `ALTER TABLE … ADD` makes a second constraint
  (`_key1`). SQLite drops a UNIQUE only when an identical index exists, so a UNIQUE on a
  rowid-alias `INTEGER PRIMARY KEY` is kept. PG keeps derived names across a rename, and
  MySQL renames `<t>_ibfk_<n>` / `<t>_chk_<n>` along with the table.
- **Plain indexes follow the database too** (ruled with the user). An unnamed PG `CREATE INDEX`
  is `<t>_<cols>_idx`, then `…_idx1`. An unnamed MySQL `KEY` takes its first column's name,
  sharing one counter with UNIQUE (`c`, `c_2`, `c_3`). MySQL's implicit FK index (for an FK no
  index starts with) is named after the constraint, or else the FK's first column. The MySQL
  parser adds that index, so the manifest's "same under every source" stays absolute.
- **Ignored input keys warn** (ruled with the user, following the PRD §4.13 `input.schema`
  precedent). An explicitly set `paths` under `source: database`, or any `introspect` field
  under `source: files`, warns. The `["."]` default does not.

**Module boundaries.** Q5 and Q10 have the introspectors call into `parser/postgres`,
`parser/mysql` and `parser/sqlite`. All of them are inside the `parser` module, so that's
allowed under ARCHITECTURE.md. Q3's resolver change is in `cmd/sqlgen/gotype`, which is CLI
code, and it imports nothing new.

---

## 11. Sub-item preview

These are provisional; `/phase` produces the authoritative breakdown. The order puts the
PRD sync first and the cross-dialect pieces (constraint model, generated columns) before
the per-dialect expansions that test against them. Closure is `/close-phase`, with no
sub-item of its own.

**Dependencies:**
- N.0 blocks everything.
- N.1 → N.2, N.3 → N.4 / N.5 / N.6 (the three dialects can run in parallel).
- N.7 → N.8 → N.9 (they can run alongside N.1–N.6).
- N.10 depends on N.4–N.6, N.8 and N.9.

### N.0 PRD sync
- No code. Land every §8 edit before any implementation (CLAUDE.md rule 1), as 27.0 and
  29.0 did.
- Flip this doc to SYNCED and archive it at phase closure (`/close-phase`).
- *Blocks every other sub-item.*

### N.1 Probe, parity harness, and unit tests (all dialects)
- Add the §6.0 `assertSchemaParity` helper and move the three existing main tests onto it.
  Whatever starts failing becomes part of the probe record.
- Add the §6.1, §6.3 and §6.4 unit tests.
- Turn the §5 probe DDL into fixtures. The probe has already run and its record is §4.0.
- Inline fixes for small, isolated defects: D2, M3, M8, S6, S10.

### N.2 Canonical constraint model, all dialects
- `parser.CanonicalizeConstraints`, called by all three parsers and all three introspectors
  (Q11 / X1).
- The DB-default name derivation for PG and MySQL, with unit tests for truncation and
  collision suffixes.
- Manifest goldens regenerate. Resolve the two FIX-240 backlog lines this covers.

### N.3 Generated columns, all dialects
- Per Q1: the PG and MySQL file parsers, plus all three introspectors.
- Confirm that the generator's `IsComputed` path excludes the column from create and update
  inputs on PG and MySQL, as it does on SQLite.
- Generated-column tests on every path.

### N.4 PG integration test expansion
- Add the §6.2 tests and tighten `TestPostgresIntrospect`.
- Fix D1 and D6 (Q3): canonical type form in the introspector and the PG parser, and
  qualified keys in the `cmd/sqlgen/gotype` resolver.
- Fix D3 (Q4) and D9 (Q14), D5, D7 (Q12), and PG default and CHECK normalization (Q5, Q13 / D8).

### N.5 MySQL integration test expansion
- Add the §6.3 tests and tighten `TestMySQLIntrospect` / `TestMySQLIntrospectViews`.
- Fix M2 and M9 with MySQL default normalization (Q5), M4 (Q7), M5, M6, and M7 (Q13).

### N.6 SQLite integration test expansion
- Add the §6.4 tests and tighten `TestSQLiteIntrospect`.
- Adopt the Q10 strategy: the PRAGMAs plus a re-parse of the stored `CREATE TABLE`.
- Fix S2, S3, S4 (Q8), S5 with warnings on both paths (Q9), S7, S8 (Q10), S9, S11 on both
  paths, and S12. SQLite defaults already match (§4.0), so Q5 needs no SQLite change.

### N.7 Connection string + §4.3 validation
- `connString`, `TestConnString`, and the config-validation rules (§7.1).

### N.8 `parseSchema` wiring
- `introspectSchema` and the `database` / `both` branches.
- The view-annotation merge.
- FK-check ordering.
- The `ctx` plumbing through the three callers.

### N.9 Exit-code split
- `connectionError` and the shared mapping in `generate`, `validate` and `loadAndValidate` (§7.3, Q2).
- Record the FIX-243 Q3(b) outcome in that entry's history.

### N.10 CLI live-DB test suite
- The §7.4 tests on all three dialects, including the files-vs-database differential.

---

## 12. Definition of done

The phase closes only when every item below holds on **all three dialects**:

1. All three main introspection tests go through `assertSchemaParity` (§6.0). Every exception
   is listed with a catalog reason; no field is excluded by hand.
2. Every row of the §6.5 parity matrix has a passing test, or a "—" with a stated reason.
3. Every §4 defect (D1–D9, M1–M9, S1–S12, X1) is fixed, with a test that failed first.
   M4 and S6 are code-confirmed only, so their tests use the triggers named in §6.
4. `TestGenerate_DatabaseSourceMatchesFiles` and `TestGenerate_DatabaseSourceCompiles` pass for
   `postgres`, `mysql` and `sqlite`.
5. Every §10 ruling is implemented, and the §8 PRD edits have landed.
6. `make check` and `make test-integration` are green.

