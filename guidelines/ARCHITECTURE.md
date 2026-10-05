# SQLGen — Architecture Reference

> Project structure, module boundaries, and package responsibilities.
> Use this document to understand where code lives and where new code should go.

---

## Module Layout

SQLGen is split into **three logical modules** for dependency isolation, currently realized as **eight Go modules** because optional features (NATS event bus, cache backends, OpenTelemetry metrics) live in their own sub-modules so consumers don't pay for what they don't use. The three logical groupings are runtime / parser / CLI; the eight `go.mod` files are listed at the bottom of this section.

```
github.com/teandresmith/sqlgen/                  ← Module 1: Runtime (imported by generated code)
├── go.mod
├── comparator/                             ← Type-safe filter operators → sql.Condition
│   ├── id.go
│   ├── string.go
│   ├── number.go
│   ├── bool.go
│   ├── time.go
│   ├── enum.go
│   ├── json.go
│   ├── jsonb.go                            ← PostgreSQL-only
│   └── slice.go                            ← PostgreSQL-only
├── database/                               ← Driver abstraction: Querier, Tx, scanning
│   ├── querier.go                          ← Querier, Result, Row, Rows interfaces
│   ├── transaction.go                      ← Tx struct, NewTransaction, WithTransaction
│   ├── scan.go                             ← Struct scanning utilities
│   ├── pgx/                               ← pgx v5 adapter
│   │   └── pgx.go
│   ├── stdlib/                             ← database/sql adapter
│   │   └── stdlib.go
│   └── mock/                              ← Mock driver for unit tests
│       └── mock.go
├── event/                                  ← Event system: types, Publisher/Subscriber interfaces
│   ├── event.go                            ← Event, Action, Config, Publisher, Subscriber
│   ├── memorybus/                          ← In-memory transport (stdlib-only, part of runtime module)
│   │   └── memorybus.go
│   └── natsbus/                            ← NATS transport (SEPARATE MODULE — own go.mod)
│       ├── go.mod                          ← github.com/teandresmith/sqlgen/event/natsbus
│       └── natsbus.go
├── hook/                                   ← Generated-code hook chain: pre/post hooks for CRUD
│   ├── hook.go                             ← Hook[T] interface, signatures
│   ├── chain.go                            ← Chain composition, ordered execution
│   └── helpers.go                          ← Chain helpers (skip, halt, transform)
├── omittable/                              ← Value[T] presence-tracking wrapper
│   └── omittable.go
├── sql/                                    ← Dialect interface, SQL builders, condition types
│   ├── dialect.go                          ← Dialect interface, Table, Condition, Sort
│   ├── postgres.go                         ← PostgresDialect
│   ├── mysql.go                            ← MySQLDialect
│   ├── sqlite.go                           ← SQLiteDialect
│   ├── builder.go                          ← BuildSelect, BuildInsert, BuildUpdate, etc.
│   └── condition.go                        ← ConditionBuilder, And, Or, Raw
├── tenancy/                                ← Multi-tenancy primitives: Context tenant accessors
│   └── tenancy.go
├── types/                                  ← Custom scalar types used by generated code & GraphQL integration
│   ├── datetime.go                         ← types.DateTime — config-overridable timestamp wrapper
│   ├── json.go                             ← types.JSON — generic JSONB payload wrapper
│   └── (gqlgen integration tests only — no runtime gqlgen dep)
├── cache/                                  ← Read-through cache abstraction
│   ├── backend.go                          ← Backend interface, key types, TTL/options
│   ├── breaker.go                          ← Circuit breaker for backend failures
│   ├── error.go                            ← Cache-specific error types
│   ├── event_adapter.go                    ← Bridge to event/ for invalidation
│   ├── invalidation.go                     ← Pattern-based invalidation
│   ├── key.go                              ← Key construction with tenancy + tags
│   ├── metrics.go                          ← Metrics hooks (used by metrics/otel/)
│   ├── noop.go                             ← No-op backend for testing
│   ├── serializer.go                       ← Pluggable serializer (default JSON)
│   ├── typed.go                            ← Typed[T] wrapper around Backend
│   ├── memory/                             ← In-process LRU backend (SEPARATE MODULE)
│   │   └── go.mod                          ← github.com/teandresmith/sqlgen/cache/memory
│   ├── redis/                              ← Redis backend (SEPARATE MODULE)
│   │   └── go.mod                          ← github.com/teandresmith/sqlgen/cache/redis
│   └── msgpack/                            ← MsgPack serializer (SEPARATE MODULE)
│       └── go.mod                          ← github.com/teandresmith/sqlgen/cache/msgpack
└── metrics/
    └── otel/                               ← OpenTelemetry metrics (SEPARATE MODULE)
        ├── go.mod                          ← github.com/teandresmith/sqlgen/metrics/otel
        └── otel.go

github.com/teandresmith/sqlgen/parser/           ← Module 2: Parser (only imported by CLI)
├── go.mod
├── schema.go                              ← Schema model: Table, Column, Enum, Relationship, View
├── parser.go                              ← Multi-dialect parser interface
├── relationship.go                        ← Relationship detection from FK constraints
├── postgres/                              ← PostgreSQL parser (CGO: pg_query_go/v6)
│   └── postgres.go
├── mysql/                                 ← MySQL parser (vitess/sqlparser)
│   └── mysql.go
├── sqlite/                                ← SQLite parser (rqlite/sql)
│   └── sqlite.go
└── introspect/                            ← Live database schema introspection
    └── introspect.go

github.com/teandresmith/sqlgen/cmd/sqlgen/       ← Module 3: CLI binary
├── go.mod
├── main.go                                ← Thin entry point: calls cli.Execute()
├── cli/                                   ← Command definitions and pipeline orchestration
│   ├── root.go                            ← NewRootCmd, Execute, cliFlags, exitError, exit codes
│   ├── pipeline.go                        ← Shared pipeline: parseSchema, loadAndValidate, helpers
│   ├── generate.go                        ← generate command RunE + stale file cleanup
│   ├── init.go                            ← init command RunE, config scaffolding
│   ├── validate.go                        ← validate command RunE
│   ├── diff.go                            ← diff command RunE, file comparison
│   ├── completion.go                      ← completion command (shell script generation)
│   ├── lint.go                            ← lint command, AST analysis of hook registrations
│   └── graphql.go                         ← graphql init / gen subcommands
├── config/                                ← YAML config loading, validation, defaults
│   ├── config.go                          ← Config structs + LoadConfig + defaults (api / cache / manifest / tenancy blocks)
│   ├── validate.go                        ← ValidatePreParse rules + non-fatal Warning type
│   └── tenancy.go                         ← TenancyConfig (PRD §29)
├── gen/                                   ← Code generation engine: templates, context, file writing
│   ├── gen.go                             ← Orchestrator: step runner, generation options (incl. ProjectRoot anchor)
│   ├── orchestrate.go                     ← Template embed + generation pipeline; API/GraphQL emission, root-relative graph dir resolution
│   ├── format.go                          ← Formatting pipeline: template → goimports → file write
│   ├── funcmap.go                         ← Custom template function registry
│   ├── acronyms.go                        ← Canonical acronym set + identifier caser (PRD §8.5)
│   ├── naming.go                          ← Naming engine (ettle/strcase for identifiers)
│   ├── inflect.go                         ← English inflection, sqlgen-owned (PRD §8.5)
│   ├── module.go                          ← Module path resolution (ReadModulePath, JoinModulePath)
│   ├── api_field_overrides.go              ← Go field names sqlgen dictates to gqlgen (PRD §26.5.6)
│   ├── api_walker.go                      ← GraphQL walker completeness validation
│   ├── filter_qualifier.go                ← Filter bare-column qualification via dialect parser
│   ├── gqlgen_model_layout.go             ← gqlgen model import path / alias resolution
│   ├── context.go                         ← Context struct definitions
│   ├── context_api.go                     ← GraphQL APIContext builder
│   ├── context_cache.go                   ← Cache context builder
│   ├── context_client.go                  ← Unified client context builder
│   ├── context_connection.go              ← Cursor-pagination connection builder
│   ├── context_enum.go                    ← Enum context builder
│   ├── context_event.go                   ← Event-hook context builder
│   ├── context_set.go                     ← MySQL SET context builder
│   ├── context_shared.go                  ← Shared helpers (column ordering, sort keys, etc.)
│   ├── context_sorter.go                  ← Sorter context builder
│   ├── context_table.go                   ← Table context builder + relationship FK metadata wiring
│   ├── context_tenancy.go                 ← Tenancy column / context builder
│   ├── context_type.go                    ← Composite/domain/extra type context builders
│   └── context_view.go                    ← View context builder
├── gotype/                                ← SQL-to-Go type mapping (generation-time only)
│   ├── gotype.go                          ← Core type resolution + ScalarExtraction
│   ├── uuidgen.go                         ← Per-integration UUID generation expressions (PRD §7.4)
│   ├── uuidstd/                           ← standard library uuid mapping (the default)
│   ├── uuidgoogle/                        ← github.com/google/uuid mapping
│   ├── uuidgofrs/                         ← github.com/gofrs/uuid/v5 mapping
│   └── decimal/                           ← github.com/shopspring/decimal mapping
├── manifest/                              ← Manifest builder: read-only pass over codegen state → *Document
│   ├── types.go                           ← ~25 JSON-tagged manifest types + versioned SchemaVersion (PRD §30.4/§30.5)
│   ├── builder.go                         ← Build(BuildInput) *Document — per-entity kind/pk/features/methods/relationships
│   └── sql.go                             ← Canonical per-method SQL bodies, captured from runtime sql builders (PRD §30.7)
└── wrapper/                               ← gqlgen subprocess wrapper
    ├── doc.go
    ├── gen.go                             ← Invokes `go run github.com/99designs/gqlgen` after sqlgen codegen
    ├── init.go                            ← Scaffolds gqlgen.yml + resolver stubs
    ├── merge.go                           ← MergeInput: merges sqlgen-generated *.graphqls into user schema
    └── seeds.go                           ← Seeds gqlgen resolver bodies with translator delegations
```

**The eight Go modules:**

| `go.mod` | Logical Module | Purpose |
|----------|---------------|---------|
| `./go.mod` | Runtime | Core runtime — imported by generated code |
| `parser/go.mod` | Parser | DDL parsing — only imported by CLI |
| `cmd/sqlgen/go.mod` | CLI | The `sqlgen` binary |
| `event/natsbus/go.mod` | Runtime (optional) | NATS event transport |
| `cache/memory/go.mod` | Runtime (optional) | In-process LRU cache backend |
| `cache/redis/go.mod` | Runtime (optional) | Redis cache backend |
| `cache/msgpack/go.mod` | Runtime (optional) | MsgPack serializer for cache |
| `metrics/otel/go.mod` | Runtime (optional) | OpenTelemetry metrics |

Optional sub-modules use `replace` directives for local development and pull their own external dependencies. Consumers add only the sub-modules they want.

---

## Module Dependency Rules

```
  ┌──────────────┐
  │   CLI Module  │ ── imports ──▶ Parser Module
  │  cmd/sqlgen/  │ ── imports ──▶ Runtime Module
  └──────────────┘
         ▲
         │ (install as tool)
         │
    Developer

  ┌────────────────┐
  │ Parser Module   │ ── imports ──▶ (nothing from sqlgen)
  │ parser/         │    Heavy deps: pg_query_go, vitess, rqlite
  └────────────────┘

  ┌────────────────┐
  │ Runtime Module  │ ── imports ──▶ (nothing from sqlgen)
  │ sqlgen/         │    Zero heavy deps in core packages
  └────────────────┘
         ▲
         │ (import in go.mod)
         │
   Consumer's Generated Code
```

**Hard rules:**
- Runtime module MUST NOT import from parser or CLI modules.
- Parser module MUST NOT import from CLI or runtime modules.
- No build tags anywhere. Module boundaries are the isolation mechanism.
- Core runtime packages (`sql/`, `comparator/`, `omittable/`, `database/`, `event/`, `hook/`, `tenancy/`, `types/`, `cache/`) depend only on the Go standard library plus the small set of low-blast-radius helpers documented in "Key Runtime Dependencies".
- Driver packages (`database/pgx/`, `database/stdlib/`) depend on their respective driver library and `database/`.
- Optional packages with external dependencies live in **separate Go modules** so consumers don't pay for what they don't use: `event/natsbus/`, `cache/memory/`, `cache/redis/`, `cache/msgpack/`, `metrics/otel/`. Each has its own `go.mod` and pulls its own external dep tree.

---

## Core Pipeline

```
SQL Files / Live DB
        │
        ▼
   ┌─────────┐
   │  Parser  │  ← Dialect-specific (PostgreSQL, MySQL, SQLite)
   └────┬────┘
        │  Schema Model (Tables, Columns, Enums, Types, Views, Relationships)
        ▼
   ┌──────────┐
   │ Generator │  ← Reads config, resolves types via gotype/, produces Go source
   └────┬─────┘
        │
        ▼
   Generated Go Code
   (models, clients, filters, field options, pagination, sorting, enums, types, views, unified client)
```

## Generation Sequence

Code generation produces files in this fixed order:

1. Enums
2. Sets (MySQL SET types)
3. Composite / Domain / Extra types
4. Error types
5. Table name constants
6. Tables (models, clients, filters, field options, inputs)
7. Sorters
8. Pagination types
9. Connection types (cursor pagination)
10. Views
11. Unified client
12. API layer — GraphQL only (when `api.graphql.enabled` is true): schema (`*.graphqls`), scalar marshaling, resolver scaffolding, filter / sort / pagination translators, error mapping, per-call options middleware

Steps 1–11 emit into `output.dir`. The GraphQL API layer (step 12) emits into a **root-relative** directory (PRD §26.5.8): `api.graphql.schema_dir` / `resolver_dir` are resolved from the module root (the `gen.Options.ProjectRoot` write anchor), exactly like `output.dir` — not relative to `output.dir`. Unset, they default to `<output.dir>/graph` (nested alongside models); an explicit value can place the graph package anywhere, e.g. a top-level sibling `graph/`. The sqlgen-owned resolver helpers (`Q` / `M`, translators, middleware) land in a `sqlgenresolver/` sub-package under the resolver dir; the consumer-editable `resolver.go` scaffold is written once and preserved across regenerations. When `api.graphql.enabled` is false, no `api.graphql.*` field is validated, resolved, or emitted.

---

## Package Responsibilities

### Runtime Module

| Package | Owns | Does NOT Own |
|---------|------|-------------|
| `sql/` | Dialect interface, SQL string building, condition/sort types | Query execution, connection management |
| `comparator/` | Type-safe filter types, `Parse()` → `[]sql.Condition` | SQL string building (delegates to `sql/`) |
| `omittable/` | `Value[T]` wrapper, JSON marshaling | Database scanning |
| `database/` | Querier interface, transaction engine, struct scanning | SQL generation, dialect logic |
| `database/pgx/` | pgx v5 adapter, pgx error mapping | Generic DB logic |
| `database/stdlib/` | database/sql adapter, MySQL/SQLite error mapping | Generic DB logic |
| `database/mock/` | Configurable mock Querier for tests | Real database interaction |
| `event/` | Event, Action, Config types; Publisher/Subscriber interfaces | Transport implementations, hook wiring |
| `event/memorybus/` | In-memory Publisher/Subscriber for testing and single-process use | Persistence, distributed delivery |
| `event/natsbus/` | NATS Publisher/Subscriber (separate Go module) | Other transports, event types |
| `hook/` | Generated-code hook chains: pre/post hook signatures, ordered execution, helpers | Domain logic, registration plumbing |
| `tenancy/` | Tenant accessor / setter on `context.Context` | Tenancy enforcement (lives in generated code) |
| `types/` | Custom scalar types: `DateTime`, `JSON` (used as override targets in `sqlgen.yml`) | Generation-time type mapping (see `gotype/`) |
| `cache/` | Backend interface, key construction, breaker, invalidation, metrics hooks | Backend implementations (see sub-modules) |
| `cache/memory/` | In-process LRU backend (separate Go module) | Distributed coordination |
| `cache/redis/` | Redis backend (separate Go module) | Local caching |
| `cache/msgpack/` | MsgPack serializer (separate Go module) | Default JSON path |
| `metrics/otel/` | OpenTelemetry metric hooks (separate Go module) | Other observability backends |

### Parser Module

| Package | Owns | Does NOT Own |
|---------|------|-------------|
| `parser/` | Schema model types, parser interface, relationship detection | SQL generation, Go code output |
| `parser/postgres/` | PostgreSQL DDL parsing via pg_query_go | MySQL/SQLite parsing |
| `parser/mysql/` | MySQL DDL parsing via vitess | Other dialects |
| `parser/sqlite/` | SQLite DDL parsing via rqlite | Other dialects |
| `parser/introspect/` | Live DB schema introspection | File-based parsing |

### CLI Module

| Package | Owns | Does NOT Own |
|---------|------|-------------|
| `cmd/sqlgen/` | Thin entry point (`main.go`) | Command logic, pipeline orchestration |
| `cli/` | Command definitions; two pipelines: standard (config → parse → generate) and graphql (config → parse → generate → wrapper merge → gqlgen subprocess); lint AST analysis | Config validation rules, template execution, type resolution |
| `config/` | YAML loading, validation, defaults, env var resolution (api / cache / manifest / tenancy blocks) | Schema parsing, code generation |
| `gen/` | Template execution, file writing, context building, module path resolution, API/GraphQL layer orchestration + root-relative graph output-dir resolution | SQL building, type resolution |
| `gotype/` | SQL-to-Go type mapping, import resolution, zero values, `ScalarExtraction` (resolver-aware guard/unwrap), per-integration UUID generation expressions (`UUIDIntegration`) | Runtime type behavior, which integration a given package selected (that is `gen/uuid_generation.go`, read off the built contexts) |
| `manifest/` | Manifest document builder: read-only pass over parsed schema + config + `gen` contexts → `*Document`; per-method canonical SQL captured from runtime `sql/` builders; JSON/markdown emission | Runtime-embedded manifest (the `gen/` `manifest_embed` template) |
| `wrapper/` | gqlgen subprocess wrapper: scaffolds `gqlgen.yml`, merges sqlgen-generated `*.graphqls` into user schema, seeds resolver bodies, invokes gqlgen; one-shot `resolver.go` scaffold is goimports-sorted for idempotency | Schema generation (lives in `gen/templates/api/`) |

---

## File Naming Conventions

| Category | Pattern | Example |
|----------|---------|---------|
| Generated files | `*_gen.go` | `users_gen.go`, `enums_gen.go`, `client_gen.go` |
| Test files | `*_test.go` | `builder_test.go`, `transaction_test.go` |
| Integration tests | `*_integration_test.go` | `pgx_integration_test.go` |
| Package entry point | Named after package or primary type | `querier.go`, `omittable.go`, `dialect.go` |
| Dialect-specific files | Named after dialect | `postgres.go`, `mysql.go`, `sqlite.go` |
| Template files | `*.go.tmpl` | `model.go.tmpl`, `client.go.tmpl` |

---

## Key Runtime Dependencies

| Dependency | Used By | Purpose |
|------------|---------|---------|
| `jackc/pgx/v5` | `database/pgx/` | PostgreSQL driver |
| `database/sql` | `database/stdlib/`, `database/` | Generic SQL driver; `database.NullScan` uses `sql.Null[T]` for conversion |
| `golang.org/x/sync/errgroup` | Generated code | Concurrent relationship loading |
| `golang.org/x/sync/singleflight` | `cache/` | Read-through stampede protection |
| `go.opentelemetry.io/otel` | `metrics/otel/` | Optional observability |

### CLI Module Dependencies

| Dependency | Used By | Purpose |
|------------|---------|---------|
| `ettle/strcase` | `gen/acronyms.go` | Identifier casing (PascalCase / camelCase) against sqlgen's canonical acronym set (PRD §8.5) |
| `golang.org/x/tools/imports` | `gen/format.go` | `goimports` formatting for generated code |
| `google/uuid`, `gofrs/uuid/v5` | `gotype/uuidgen_compile_*_test.go` (**test only**) | Compile the UUID generation expressions of PRD §7.4 against the real libraries, so a spelling that does not exist fails the build rather than a golden diff |

### Parser Module Dependencies

| Dependency | Used By | Purpose | CGO? |
|------------|---------|---------|------|
| `pg_query_go/v6` | `parser/postgres/` | PostgreSQL DDL parsing | Yes |
| `vitess/sqlparser` | `parser/mysql/` | MySQL DDL parsing | No |
| `rqlite/sql` | `parser/sqlite/` | SQLite DDL parsing | No |
