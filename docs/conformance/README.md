# PRD Conformance Registry

This directory records which PRD behaviors are required and which tests prove them. The design is
in [`docs/design/CONFORMANCE.md`](../design/CONFORMANCE.md) §3 and §5. `make req-check` (part of
`make check`) verifies the registry against `docs/PRD.md` and the test sources, without Docker.

## Files

| File | Module | Test homes |
|---|---|---|
| [`runtime.md`](runtime.md) | Runtime: `sql`, `comparator`, `omittable`, `database` and drivers, `cache`, `tenancy`, `event`, `hook`, `types`, `manifest` | unit, integration |
| [`parser.md`](parser.md) | Parser: `parser` and its dialect and `introspect` packages | unit, integration |
| [`cli/config.md`](cli/config.md) | CLI: `cmd/sqlgen/config` | unit |
| [`cli/gen.md`](cli/gen.md) | CLI: `cmd/sqlgen/gen` | unit, golden |
| [`cli/gotype.md`](cli/gotype.md) | CLI: `cmd/sqlgen/gotype` | unit |
| [`cli/api.md`](cli/api.md) | CLI: API generation (§26); no package of its own, its rows claim `cmd/sqlgen/gen`, `cli` and example tests | unit, golden, example |
| [`cli/commands.md`](cli/commands.md) | CLI: `cmd/sqlgen`, `cli`, `wrapper` | unit, integration, golden |
| [`cli/manifest.md`](cli/manifest.md) | CLI: `cmd/sqlgen/manifest`, `mcp` | unit, integration |
| [`client.md`](client.md) | The generated client | conformance, example |

Every directory holding `_test.go` files is listed in one file's `<!-- packages: … -->` line, or
below as unregistered: test support and repository tooling that back no PRD behavior. A new test
directory fails the check until it is placed. `testdata` trees are not scanned; the conformance
suite under `cmd/sqlgen/testdata/conformance` is checked through `client.md`.

<!-- unregistered: cmd/sqlgen/internal/livedb cmd/sqlgen/internal/reqcheck -->

A file that passes about 500 rows is split into a directory of per-package files, such as
`cli/config.md`. The ownership map below then names the per-package file.

## Format

A registry file has a header and up to four kinds of table.

**Header.** The seeding state, the module's package directories, and its matrices:

```markdown
<!-- seeding: not-started -->
<!-- packages: cmd/sqlgen/config cmd/sqlgen/gen -->
<!-- matrix: config-validation rows=rule cols=fires,message,severity,together,schema -->
```

Seeding is `not-started`, `in-progress` or `complete`. While a file is `in-progress`, coverage
holes are reported as notes (`make req-check VERBOSE=1` lists them). Once it is `complete` they
fail the check, so coverage cannot slip back.

**Requirements**, under a PRD anchor whose hash records the PRD text the rows were reviewed
against:

```markdown
## §9.4c Nil input handling
<!-- prd: 9.4c sha256: 0123456789abcdef -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| nil-input.getmany | `GetMany(ctx, nil)` behaves exactly as `GetMany(ctx, &Get<T>Input{})` | `conformance: GetMany/nil input equals zero input` | all | client-methods: GetMany × nil-empty-input |
```

- **ID**: a lower-case slug, unique across every registry file. It never appears in code or test
  names.
- **Requirement**: one statement a test can fail on, stated by the PRD. A behavior the PRD does
  not state is not a requirement.
- **Tests**: entries separated by `;`. Each is `conformance: Method/subtest`,
  `unit: <dir>: TestName/subtest`, `integration: <dir>: TestName`,
  `example: <example>: TestName` or `golden: <example>`. `<dir>` is relative to the repository
  root. An empty cell means the requirement is not yet tested.
- **Applies to**: `all`, or a comma-separated list of `postgres`, `mysql`, `sqlite`, `pgx`,
  `stdlib`. Dialects and drivers combine: `postgres, stdlib` is Postgres through `stdlib`
  only, and `mysql, sqlite` is either dialect on any driver.
- **Matrix cell**: `<matrix>: <row> × <column>`, naming a matrix the file declares. Leave it empty
  when no matrix covers the row.

**Not applicable** cells. A reason that cites a PRD section stands on its own. Any other reason
needs the user's acceptance date:

```markdown
| Matrix cell | Reason | Accepted |
|---|---|---|
| client-methods: Count × field-selection | §12 field selection applies to entity reads; Count returns a number | |
```

**Unclaimed** tests, which back no PRD behavior (helpers, internal invariants). In a `complete`
file every top-level test in the listed packages is claimed by a row or listed here:

```markdown
| Unclaimed test | Reason |
|---|---|
| cmd/sqlgen/gen: TestFuncmapIndent | Template helper; no PRD behavior |
```

**PRD gaps**: behavior the code has, usually pinned by tests, that no PRD text states. A gap is
not a requirement. It records a question for the user, under a `## PRD gaps` heading at the end
of the file whose module shows the behavior:

```markdown
| Gap | Behavior | Tests | Section | Ruling |
|---|---|---|---|---|
| gap.hard-delete-many.nil-statement | `HardDeleteMany(ctx, nil)` sends no statement and reads nothing back | `example: postgres: TestEmptyBatch_HardDeleteManyUnaffected` | §9.4c | |
```

- **Gap**: a slug starting with `gap.`, unique across every registry file, IDs of requirements
  included. A requirement ID may not start with `gap.`.
- **Tests**: the tests that pin the behavior, in the requirement format, or empty for a gap found
  by reading code. Each must resolve, and each counts as claimed, so a pin listed here passes the
  orphan check of a `complete` file, whichever registry file lists the test's package.
- **Section**: the nearest PRD section or sections (`§…`), each of which must exist.
- **Ruling**: empty while the gap is open. The user's ruling is `amend-prd YYYY-MM-DD` (the PRD
  will state the behavior) or `drop-pin YYYY-MM-DD` (the tests stop asserting it). Once the
  ruling is applied, delete the entry: the PRD text becomes requirement rows, or the pins go.

An open or ruled gap never fails the check; it is a note, so `make req-check VERBOSE=1` lists
what is waiting on the user and what is waiting to be applied.

## The report

`make conformance` (needs Docker, not part of `make check`) runs every registered test home with
`go test -json`: the module packages, unit and integration tests together, the example modules,
and each conformance dialect module. It joins the results with this registry and writes
[`REPORT.md`](REPORT.md). A failing test does not stop the other homes; the report is always
written, and the target then exits non-zero. CI runs the suite but does not rewrite the report:
`/close-phase` and deliberate runs do.

A reference matches the name `go test` reports, with spaces in subtest names as underscores. A
`conformance:` reference is looked up under each dialect module's `TestConformance` that the
row's Applies-to admits; a `golden: <example>` reference is `cmd/sqlgen: TestE2EGoldenFiles/<example>`.
A test with no result of its own is `not run`, never `pass`: its package did not build or run, or
a parent test failed first (no Docker: the container does not start) or skipped.

## When the PRD changes

`make req-check` fails when a PRD section changes under an anchor, naming the section. Review the
rows under it against the new text, fix any that no longer hold, then record the review:

```bash
make req-rehash SECTIONS=9.4c     # one or more sections, comma-separated
make req-rehash                   # every changed anchor
```

A new PRD heading fails the check until it is added to the ownership map below.

## Ownership map

Every level-2 and level-3 PRD section, and its owner: the registry file that holds its
requirements, or `informative` when it states no behavior (overview, rationale, examples,
rejected alternatives). Deeper headings belong to their nearest listed ancestor. A section
another file also cites is still owned once.

| PRD section | Owner |
|---|---|
| Table of Contents | informative |
| §1 Overview | informative |
| §2 Glossary | informative |
| §3 System Architecture | informative |
| §4 Configuration System | cli/config.md |
| §4.1 Root Config | cli/config.md |
| §4.2 InputConfig | cli/config.md |
| §4.3 ConnectionConfig | cli/config.md |
| §4.4 IntrospectConfig | cli/config.md |
| §4.5 OutputConfig | cli/config.md |
| §4.6 GenerationConfig | cli/config.md |
| §4.7 OverrideConfig | cli/config.md |
| §4.8 TableConfig (per-table) | cli/config.md |
| §4.9 ViewConfig (per-view) | cli/config.md |
| §4.10 ExtraType (custom structs) | cli/config.md |
| §4.11 Enum Overrides | cli/config.md |
| §4.12 Full Example | cli/config.md |
| §4.13 Config Validation Rules | cli/config.md |
| §4.14 Config JSON Schema (editor support) | cli/config.md |
| §5 SQL Parsing | parser.md |
| §5.1 Schema Model | parser.md |
| §5.2 Dialect Parsers | parser.md |
| §5.3 Relationship Detection | parser.md |
| §5.4 Soft Delete Column Detection | parser.md |
| §5.5 Schema Qualification & Table Naming | parser.md |
| §5.6 Multi-File Parsing Order | parser.md |
| §5.7 Deterministic Output | parser.md |
| §6 Database Introspection | parser.md |
| §6.1 Source Modes | parser.md |
| §6.2 Introspector Interface | parser.md |
| §6.3 Dialect Introspection Capabilities | parser.md |
| §6.4 Filtering | parser.md |
| §7 Type System | cli/gotype.md |
| §7.1 Type Resolution Chain | cli/gotype.md |
| §7.2 Built-In Type Mappings | cli/gotype.md |
| §7.3 Nullable Handling | cli/gotype.md |
| §7.4 Built-In Type Integrations | cli/gotype.md |
| §7.5 Custom Type Overrides | cli/gotype.md |
| §7.6 JSON / JSONB Column Type | cli/gotype.md |
| §8 Code Generation | cli/gen.md |
| §8.1 Template Engine | cli/gen.md |
| §8.2 Generated Artifacts Per Table | cli/gen.md |
| §8.3 Generated Artifacts — Other | cli/gen.md |
| §8.4 Layout Strategies | cli/gen.md |
| §8.5 Naming Rules | cli/gen.md |
| §8.6 Primary Key Strategy | cli/gen.md |
| §8.7 Composite Primary Keys | cli/gen.md |
| §9 Generated Client Operations | client.md |
| §9.1 Read Operations | client.md |
| §9.2 Write Operations | client.md |
| §9.3 Delete Operations | client.md |
| §9.4 Pagination Operations | client.md |
| §9.4a Streaming Operations | client.md |
| §9.4b No-PK tables are skipped | cli/gen.md |
| §9.4c Nil input handling | client.md |
| §9.5 Edge Case Behaviors | client.md |
| §9.6 Per-Call Options | client.md |
| §9.6a Lock Modes | client.md |
| §9.7 PK Resolution and Entity Return Path | client.md |
| §9.8 Generated Method Patterns | client.md |
| §9.9 Nested Mutations | client.md |
| §10 Omittable Package | runtime.md |
| §10.1 The Problem | runtime.md |
| §10.2 API | runtime.md |
| §10.3 Usage in Generated Code | runtime.md |
| §10.4 JSON Serialization | runtime.md |
| §11 Filtering & Comparators | runtime.md |
| §11.1 Generated Filter Struct | cli/gen.md |
| §11.2 Comparator Types | runtime.md |
| §11.3 Comparator Interface | runtime.md |
| §11.4 Custom Conditions | runtime.md |
| §11.5 Condition Composition | runtime.md |
| §12 Field Selection | cli/gen.md |
| §12.1 FieldOptions Struct | cli/gen.md |
| §12.2 Column Map | cli/gen.md |
| §13 Relationships | cli/gen.md |
| §13.1 Relationship Types | cli/gen.md |
| §13.2 Loading Strategy | client.md |
| §13.3 FK Type Conversion | cli/gen.md |
| §13.4 Manual Relationship Configuration | cli/gen.md |
| §13.5 Self-Referential Relationships | cli/gen.md |
| §13.6 Circular Relationships | cli/gen.md |
| §13.7 Sub-Categorized Polymorphism | cli/gen.md |
| §14 Pagination | client.md |
| §14.1 Offset-Based | client.md |
| §14.2 Cursor-Based (Relay Pagination) | client.md |
| §15 Sorting | client.md |
| §15.1 Sort Type | runtime.md |
| §15.2 Generated Sorter Helpers | cli/gen.md |
| §15.3 Usage in GetMany | client.md |
| §16 Views | cli/gen.md |
| §16.1 Annotation Files (File-Sourced Views) | parser.md |
| §16.2 Introspected Views (DB-Sourced Views) | parser.md |
| §16.3 Column Type Resolution | cli/gen.md |
| §16.4 Generated Artifacts (Views vs Tables) | cli/gen.md |
| §16.5 Materialized Views (PostgreSQL) | client.md |
| §17 Soft Delete | client.md |
| §17.1 Detection | parser.md |
| §17.2 Column Types | cli/gen.md |
| §17.3 Generated Behavior | client.md |
| §17.4 Unique Constraint Consideration | informative |
| §17.5 Update Tracking | client.md |
| §18 Transaction Support | runtime.md |
| §18.1 Context-Based Injection | runtime.md |
| §18.2 Transaction Options | runtime.md |
| §18.3 API | runtime.md |
| §18.4 Tx.IsClosed and Safe Fallback | runtime.md |
| §18.5 Concurrent Safety | runtime.md |
| §18.6 Deferred Side Effects | runtime.md |
| §18.7 Known Limitations | informative |
| §19 Database Abstraction Layer | runtime.md |
| §19.1 Core Package (`database/`) | runtime.md |
| §19.2 Driver Packages | runtime.md |
| §19.3 Import Isolation | runtime.md |
| §19.4 Batch Behavior by Driver | runtime.md |
| §20 Unified Client | client.md |
| §20.1 Purpose | informative |
| §20.2 Generated Structure | client.md |
| §20.3 Usage Examples | client.md |
| §20.4 Configuration | client.md |
| §20.5 Concurrency Safety | client.md |
| §21 Hooks & Middleware | runtime.md |
| §21.1 Design Principles | runtime.md |
| §21.2 Hook Types | runtime.md |
| §21.3 Registration | runtime.md |
| §21.4 Conditional Execution Helpers | runtime.md |
| §21.5 Hook Lifecycle | runtime.md |
| §21.6 Panic Recovery | runtime.md |
| §21.7 Global Hooks (untyped — all tables) | runtime.md |
| §21.8 Entity-Level Hooks (type-safe — specific tables) | runtime.md |
| §21.9 Registration Summary | runtime.md |
| §21.10 Rejected Alternative: Per-Operation Hook Methods | informative |
| §21.11 Testing Hooks | runtime.md |
| §22 Error Handling | runtime.md |
| §22.1 Generated Error Types | cli/gen.md |
| §22.2 Constraint Errors | runtime.md |
| §22.3 Error Wrapping Convention | client.md |
| §22.4 Stack Traces and Custom Error Packages | cli/gen.md |
| §22.5 Check Constraint Validation | cli/gen.md |
| §23 CLI | cli/commands.md |
| §23.1 Commands | cli/commands.md |
| §23.2 Flags | cli/commands.md |
| §23.3 Config Discovery | cli/commands.md |
| §23.4 `sqlgen init` | cli/commands.md |
| §23.5 `sqlgen completion` | cli/commands.md |
| §23.6 `sqlgen diff` | cli/commands.md |
| §23.7 `sqlgen lint` | cli/commands.md |
| §23.8 Stale File Cleanup | cli/commands.md |
| §23.9 Exit Codes | cli/commands.md |
| §23.10 Watch Mode | cli/commands.md |
| §24 Logging & Observability | runtime.md |
| §24.1 Two-Level Tracing Model | informative |
| §24.2 Operation-Level Tracing (via Hooks) | runtime.md |
| §24.3 SQL-Level Tracing (via Driver) | informative |
| §24.4 Why This Split | informative |
| §25 Performance Characteristics | client.md |
| §25.1 Query Count Guarantee | client.md |
| §25.2 Scan Overhead | informative |
| §25.3 Batch Operations | client.md |
| §25.4 No Implicit Caching | client.md |
| §26 API Generation | cli/api.md |
| §26.1 Purpose | informative |
| §26.2 Supported Protocols | cli/api.md |
| §26.3 Configuration | cli/api.md |
| §26.4 GraphQL Schema Generation | cli/api.md |
| §26.5 GraphQL Resolver Generation | cli/api.md |
| §26.6 Query Complexity & Depth Limiting | cli/api.md |
| §26.7 GraphQL Authorization | cli/api.md |
| §26.8 REST API (Supplementary) | cli/api.md |
| §26.9 Architecture | cli/api.md |
| §26.10 Per-Table API Configuration | cli/api.md |
| §26.11 Per-Call Options via HTTP Headers | cli/api.md |
| §26.12 Known Limitations | cli/api.md |
| §26.13 gRPC API Generation | cli/api.md |
| §27 Caching | runtime.md |
| §27.1 Overview | informative |
| §27.2 Configuration | cli/config.md |
| §27.3 Cache Interface | runtime.md |
| §27.4 Injection via Hooks | runtime.md |
| §27.5 Cache Key Generation | runtime.md |
| §27.6 Cache Invariant | runtime.md |
| §27.7 Cache Behavior by Operation | runtime.md |
| §27.8 Hydration | runtime.md |
| §27.9 Invalidation Strategies | runtime.md |
| §27.10 Serialization | runtime.md |
| §27.11 View Cache Invalidation | runtime.md |
| §27.12 Cache Warming | runtime.md |
| §27.13 Cache Metrics | runtime.md |
| §28 Event System | runtime.md |
| §28.1 Overview | informative |
| §28.2 Package Structure | runtime.md |
| §28.3 Event Structure | runtime.md |
| §28.4 Publisher Interface | runtime.md |
| §28.5 Subscriber Interface | runtime.md |
| §28.6 Event Input | runtime.md |
| §28.7 Transport Implementations | runtime.md |
| §28.8 Configuration | cli/config.md |
| §28.9 Generated Event Hook Architecture | cli/gen.md |
| §28.10 Use Cases | informative |
| §28.11 Cache Invalidation via Events | runtime.md |
| §29 Tenancy | client.md |
| §29.1 Overview | informative |
| §29.2 Configuration | cli/config.md |
| §29.3 Runtime Integration | runtime.md |
| §29.4 Generated Behavior | client.md |
| §29.5 Cache Key Grammar Extension | runtime.md |
| §29.6 Event Metadata | runtime.md |
| §29.7 Composite Primary Keys | client.md |
| §29.8 Soft Delete Composition | client.md |
| §29.9 Hooks Orthogonality | client.md |
| §29.10 Relationship Propagation | client.md |
| §29.11 Constraints | cli/config.md |
| §29.12 YAML Examples | informative |
| §30 Manifest | cli/manifest.md |
| §30.1 Overview | informative |
| §30.2 Configuration | cli/manifest.md |
| §30.3 Generated Artifacts | cli/manifest.md |
| §30.4 JSON Schema | cli/manifest.md |
| §30.5 Versioning Policy | cli/manifest.md |
| §30.6 Runtime-Embedded Manifest | runtime.md |
| §30.7 Extended Schema Metadata | cli/manifest.md |
| §30.8 CLI Tooling | cli/manifest.md |
| §31 MCP Server | cli/manifest.md |
| §31.1 Overview | informative |
| §31.2 Configuration | cli/manifest.md |
| §31.3 Tool, Resource & Prompt Surface | cli/manifest.md |
| §31.4 Protocol | cli/manifest.md |
| §31.5 Versioning Policy | cli/manifest.md |
| §32 Column Access Control | cli/gen.md |
| §32.1 Overview | informative |
| §32.2 Roles | cli/gen.md |
| §32.3 Per-surface behavior | cli/gen.md |
| §32.4 Validation | cli/gen.md |
| §32.5 Interactions | cli/gen.md |
| §32.6 Configuration example | cli/gen.md |
| §Appendix A: SQL Builder Functions | runtime.md |
| §Appendix B: Dialect Comparison Matrix | runtime.md |
| §Appendix C: Project Linting Configuration | informative |
