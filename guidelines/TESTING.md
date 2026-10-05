# SQLGen — Testing Standards

> Testing strategy, patterns, and requirements for the SQLGen project.
> Every test in this project must follow these standards.

---

## Table of Contents

- [1. Principles](#1-principles)
- [2. Test Organization](#2-test-organization)
- [3. Table-Driven Tests](#3-table-driven-tests)
- [4. Integration Tests with Testcontainers](#4-integration-tests-with-testcontainers)
- [5. Assertions and Failure Messages](#5-assertions-and-failure-messages)
- [6. Mocking](#6-mocking)
- [7. Test Helpers](#7-test-helpers)
- [8. Race Detection](#8-race-detection)
- [9. Test Coverage](#9-test-coverage)
- [10. E2E Examples Module Pattern](#10-e2e-examples-module-pattern)
- [11. Codegen Unit Fixture Patterns](#11-codegen-unit-fixture-patterns)

---

## 1. Principles

1. **Test against real databases.** Use testcontainers for any code that touches a database. Mocks hide real bugs — we ship code that talks to PostgreSQL, MySQL, and SQLite, so we test against PostgreSQL, MySQL, and SQLite.
2. **Table-driven tests are the default.** Every function with more than one interesting input combination uses a table-driven test. No exceptions.
3. **No assertion libraries.** Use the standard library: `==`, `cmp.Equal`, `cmp.Diff`. Assertion libraries fragment the developer experience and obscure failure context.
4. **Useful failure messages.** Every test failure must tell you what function was called, with what inputs, what it returned, and what was expected.
5. **Prefer `t.Error` over `t.Fatal`.** Report all failures in a single run. Use `t.Fatal` only when subsequent checks would be meaningless without the current one passing.

---

## 2. Test Organization

### File Naming

| Type | File Pattern | Example |
|------|-------------|---------|
| Unit tests | `*_test.go` | `builder_test.go` |
| Integration tests | `*_integration_test.go` | `pgx_integration_test.go` |

### Test Names

Name test files, test functions, subtests, and fixture data after the behavior they pin, never after the FIX entry, phase or design-decision label that introduced them: `TestBuildMergeInput_NumericWidthAnchors`, not `TestBuildMergeInput_FIXNNN_NumericWidthAnchors`; `filter_nonfilterable_test.go`, not `filter_fixNNN_test.go`. The FIX ID goes in the commit message. See `GO.md` → When to Comment.

### Skipping Integration Tests with -short

Integration tests run by default with `go test ./...`. When you need a fast feedback loop on unit tests only, use the `-short` flag to skip integration tests:

```bash
go test -short ./...
```

Integration tests must check `testing.Short()` and skip themselves:

```go
func TestCreateUser(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }
    // ... test body
}
```

For integration test files that use `TestMain` to start containers, guard the container setup:

```go
func TestMain(m *testing.M) {
    flag.Parse()
    if testing.Short() {
        os.Exit(0)
    }
    // ... container setup
}
```

**No build tags.** Module boundaries handle dependency isolation. Do not use `//go:build` tags to gate tests.

### Test Package

Use the `_test` package suffix for black-box tests that validate the public API:

```go
package sql_test  // tests sql/ from the outside
```

Use the same package for white-box tests that need access to unexported internals:

```go
package sql  // tests internal behavior
```

Prefer black-box tests. Use white-box tests only when testing unexported logic that is complex enough to warrant direct testing.

---

## 3. Table-Driven Tests

### Structure

Every table-driven test follows this pattern:

```go
func TestBuildSelect(t *testing.T) {
    tests := []struct {
        name       string
        dialect    sql.Dialect
        table      sql.Table
        conditions []sql.Condition
        wantSQL    string
        wantArgs   []any
    }{
        {
            name:    "simple select with no conditions",
            dialect: sql.NewPostgresDialect(),
            table:   sql.Table{Schema: "public", Name: "users"},
            wantSQL: `SELECT * FROM "public"."users"`,
        },
        {
            name:    "select with equality condition",
            dialect: sql.NewPostgresDialect(),
            table:   sql.Table{Schema: "public", Name: "users"},
            conditions: []sql.Condition{
                {Clause: "id = $1", Value: 42},
            },
            wantSQL:  `SELECT * FROM "public"."users" WHERE id = $1`,
            wantArgs: []any{42},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            gotSQL, gotArgs := sql.BuildSelect(tt.dialect, tt.table, tt.conditions)
            if gotSQL != tt.wantSQL {
                t.Errorf("BuildSelect() sql = %q, want %q", gotSQL, tt.wantSQL)
            }
            if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
                t.Errorf("BuildSelect() args mismatch (-want +got):\n%s", diff)
            }
        })
    }
}
```

### Rules

- **Name every test case.** The `name` field must describe the scenario, not restate the inputs.
- **Use `t.Run` with the name.** This produces clear output: `TestBuildSelect/select_with_equality_condition`.
- **Keep cases self-contained.** Each case must define all its inputs and expected outputs. No shared mutable state between cases.
- **Test edge cases explicitly.** Empty inputs, nil values, zero-length slices, maximum batch sizes — these are test cases, not afterthoughts.

### When to Use Table-Driven Tests

- Functions with multiple input combinations (most functions).
- Each SQL dialect needs the same test cases with different expected outputs.
- Comparator types: each operator (`Eq`, `In`, `Like`, etc.) is a test case.

### When NOT to Use Table-Driven Tests

- Tests that require complex setup/teardown unique to each case.
- Tests with fundamentally different assertion logic per case.
- Single-case tests (just write a plain test function).

---

## 4. Integration Tests with Testcontainers

### Why Testcontainers

Mocks test that your code calls the mock correctly. They do not test that your SQL is valid, that your type mappings are correct, or that your transaction logic actually works against a real database. **Testcontainers spin up real PostgreSQL, MySQL, and SQLite instances in Docker**, giving you confidence that code works against real databases.

### Setup Pattern

Use `TestMain` to start the container once per package and share the connection:

```go
package pgx_test

import (
    "context"
    "flag"
    "os"
    "testing"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "github.com/testcontainers/testcontainers-go/wait"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
    flag.Parse()
    if testing.Short() {
        os.Exit(0)
    }

    ctx := context.Background()

    pgContainer, err := postgres.Run(ctx,
        "postgres:16-alpine",
        postgres.WithDatabase("sqlgen_test"),
        postgres.WithUsername("test"),
        postgres.WithPassword("test"),
        testcontainers.WithWaitStrategy(
            wait.ForLog("database system is ready to accept connections").
                WithOccurrence(2),
        ),
    )
    if err != nil {
        panic(err)
    }

    connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
    if err != nil {
        panic(err)
    }

    testPool, err = pgxpool.New(ctx, connStr)
    if err != nil {
        panic(err)
    }

    code := m.Run()

    testPool.Close()
    _ = pgContainer.Terminate(ctx)
    os.Exit(code)
}
```

### Per-Test Isolation

Use transactions that roll back after each test to keep tests independent without recreating the container:

```go
func withTx(t *testing.T, fn func(ctx context.Context, tx pgx.Tx)) {
    t.Helper()
    ctx := context.Background()
    tx, err := testPool.Begin(ctx)
    if err != nil {
        t.Fatalf("begin tx: %v", err)
    }
    defer tx.Rollback(ctx)
    fn(ctx, tx)
    // no commit — rolls back automatically
}

func TestCreateUser(t *testing.T) {
    withTx(t, func(ctx context.Context, tx pgx.Tx) {
        // test using tx — automatically rolled back
    })
}
```

### Schema Setup

Apply test schemas in `TestMain` after connecting. Keep test SQL files alongside test code or in a `testdata/` directory:

```go
schema, err := os.ReadFile("testdata/schema.sql")
if err != nil {
    panic(err)
}
if _, err := testPool.Exec(ctx, string(schema)); err != nil {
    panic(err)
}
```

### What to Integration Test

| Component | What to Verify |
|-----------|---------------|
| `database/pgx/` | Querier methods work against real PostgreSQL |
| `database/stdlib/` | Querier methods work against real MySQL and SQLite |
| `sql/` builders | Generated SQL executes without syntax errors |
| Transaction engine | Begin, savepoint, commit, rollback sequences |
| Error mapping | Real constraint violations produce correct `ConstraintError` fields |
| Comparator output | Filter SQL is valid and produces expected result sets |

---

## 5. Assertions and Failure Messages

### No Assertion Libraries

Do not use `testify`, `gomega`, `gocheck`, or any third-party assertion library. They:
- Add dependencies to the runtime module's test graph.
- Produce generic error messages that require context reconstruction.
- Fragment the testing patterns across the codebase.

### Failure Message Format

Every failure message follows this pattern:

```
FunctionName(inputs) = got, want expected
```

Examples:

```go
// Scalar comparison
if got != tt.want {
    t.Errorf("Placeholder(%d) = %q, want %q", tt.pos, got, tt.want)
}

// Struct/slice comparison with cmp.Diff
if diff := cmp.Diff(tt.want, got); diff != "" {
    t.Errorf("Parse(%q) mismatch (-want +got):\n%s", tt.input, diff)
}

// Error checking
if err != nil {
    t.Fatalf("BuildSelect() unexpected error: %v", err)
}

// Expected error
if !errors.Is(err, tt.wantErr) {
    t.Errorf("GetByID(%d) error = %v, want %v", tt.id, err, tt.wantErr)
}
```

### cmp.Diff for Complex Types

Use `github.com/google/go-cmp/cmp` for comparing structs, slices, and maps:

```go
if diff := cmp.Diff(wantSchema, gotSchema); diff != "" {
    t.Errorf("ParseSchema() mismatch (-want +got):\n%s", diff)
}
```

Use `cmpopts.IgnoreUnexported()` or `cmp.AllowUnexported()` when needed. For proto messages, use `protocmp.Transform()`.

### t.Error vs t.Fatal

| Use | When |
|-----|------|
| `t.Errorf` | The test can continue and report additional failures |
| `t.Fatalf` | Subsequent checks depend on this one (e.g., nil check before field access) |

```go
// Good — fatal on nil, then continue checking fields
got, err := Parse(input)
if err != nil {
    t.Fatalf("Parse(%q) unexpected error: %v", input, err)
}
// safe to access got.Fields, got.Name, etc.
if got.Name != tt.wantName {
    t.Errorf("Parse(%q).Name = %q, want %q", input, got.Name, tt.wantName)
}
if got.Schema != tt.wantSchema {
    t.Errorf("Parse(%q).Schema = %q, want %q", input, got.Schema, tt.wantSchema)
}
```

---

## 6. Mocking

### When to Mock

- **Unit testing generated client logic** where the database call isn't the thing being tested.
- **Testing error handling paths** that are hard to trigger against a real database.
- **Testing the code generator** — verify generated code structure without executing it.

### When NOT to Mock

- **SQL correctness.** If the test is validating that SQL is correct, run it against a real database.
- **Transaction behavior.** Savepoints, rollbacks, and commit semantics must be tested against real databases.
- **Driver error mapping.** Trigger real constraint violations.

### Using database/mock/

The project provides `database/mock/` with configurable function fields:

```go
m := mock.New()
m.QueryRowFn = func(ctx context.Context, sql string, args ...any) database.Row {
    return mock.NewRow(42, "alice", "alice@example.com")
}

client := NewUserClient(m)
user, err := client.GetByID(ctx, 42)
```

Do not build additional mock frameworks. The mock package is intentionally simple — configurable functions, not a recording/replay system.

---

## 7. Test Helpers

### Rules for Helpers

- Always call `t.Helper()` as the first line. This ensures failure messages point to the test, not the helper.
- Helpers that fail preconditions should call `t.Fatal`, not `t.Error`.
- Never call `t.Fatal` from a goroutine other than the one running the test function.

```go
func createTestSchema(t *testing.T, pool *pgxpool.Pool) {
    t.Helper()
    schema, err := os.ReadFile("testdata/schema.sql")
    if err != nil {
        t.Fatalf("reading test schema: %v", err)
    }
    if _, err := pool.Exec(context.Background(), string(schema)); err != nil {
        t.Fatalf("applying test schema: %v", err)
    }
}
```

### testdata/ Directory

Store test fixtures (SQL files, JSON fixtures, golden files) in `testdata/` directories. Go tooling ignores `testdata/` during builds.

```
sql/
├── builder.go
├── builder_test.go
└── testdata/
    ├── schema.sql
    └── golden/
        ├── select_simple.sql
        └── select_with_joins.sql
```

---

## 8. Race Detection

### Always Run with -race in CI

```bash
go test -race ./...
```

The transaction engine (`database/transaction.go`) uses `sync.Mutex` and is the primary target for race conditions. All transaction tests must pass under `-race`.

### Goroutine Leak Detection

As of Go 1.27 `goroutineleak` is a predefined `runtime/pprof` profile, so no `GOEXPERIMENT` flag is required:

```bash
go test -race ./...
```

Tests that want the report write the profile directly:

```go
pprof.Lookup("goroutineleak").WriteTo(w, 1)
```

This detects goroutines blocked on primitives that can never be unblocked — particularly valuable for testing concurrent relationship loading with `errgroup`.

---

## 9. Test Coverage

### Targets

- **Runtime core packages** (`sql/`, `comparator/`, `omittable/`, `database/`): aim for high coverage. These are the foundation.
- **Driver adapters** (`database/pgx/`, `database/stdlib/`): integration tests cover the critical paths.
- **Parser module**: every DDL statement type must have at least one test case.
- **Code generator**: test template output against golden files.

### Golden File Testing

For code generation output, use golden file comparison:

```go
func TestGenerateModel(t *testing.T) {
    got := gen.GenerateModel(schema)

    golden := filepath.Join("testdata", "golden", t.Name()+".go")
    if *update {
        os.WriteFile(golden, []byte(got), 0o644)
        return
    }

    want, err := os.ReadFile(golden)
    if err != nil {
        t.Fatalf("reading golden file: %v", err)
    }
    if diff := cmp.Diff(string(want), got); diff != "" {
        t.Errorf("GenerateModel() mismatch (-want +got):\n%s", diff)
    }
}
```

Use an `-update` flag to regenerate golden files when output intentionally changes:

```go
var update = flag.Bool("update", false, "update golden files")
```

For unit-level template golden tests, use `make update-golden`. For end-to-end example goldens (per-example output trees under `cmd/sqlgen/testdata/examples/*/expected/`), use `-update-e2e` and `make update-golden-e2e`.

### What NOT to Measure

Do not chase 100% line coverage. Coverage is a tool for finding untested paths, not a target. A test that asserts nothing to bump coverage is worse than no test.

---

## 10. E2E Examples Module Pattern

End-to-end coverage of the full pipeline (config → parse → generate → compile → run against real DB) lives under `cmd/sqlgen/testdata/examples/`. Each example is its **own Go module** so the generated code compiles in isolation against the same dependency layout consumers see.

### Layout

```
cmd/sqlgen/testdata/examples/
├── postgres/
│   ├── go.mod                          ← own module + replace directive
│   ├── sqlgen.yml                      ← per-example config
│   ├── schema.sql                      ← input DDL
│   ├── expected/                       ← golden tree captured by -update-e2e
│   │   ├── enums_gen.go
│   │   ├── users_gen.go
│   │   └── ...
│   └── models/                         ← generated output, checked in; hand-written
│                                          package-internal tests may live here
│       └── ...
├── mysql/
├── sqlite/
├── sqlite_stdlib/
├── tenancy/
├── cache/
├── events/
└── graphql/
```

### Module Setup

Each example's `go.mod`:

```
module github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/<name>

go 1.27

require github.com/teandresmith/sqlgen v0.0.0

replace github.com/teandresmith/sqlgen => ../../../..
```

The `replace` lets the example consume in-tree changes without publishing. CI / local runs use `GOWORK=off` so the workspace at the repo root doesn't override the per-example replace.

### Running

| Command | Purpose |
|---------|---------|
| `make test-examples` | Run every example's tests against real DBs (requires Docker) |
| `make lint-examples` | Run `golangci-lint` per example module |
| `make check-examples` | Lint + test all examples |
| `make update-golden-e2e` | Regenerate `expected/` trees across all examples |
| `cd cmd/sqlgen && go test -run TestE2EGoldenFiles -count=1 -timeout=5m` | Run the golden-file harness only (no DB) |

The `-update-e2e` flag is the per-test toggle that powers `make update-golden-e2e`. **Two consecutive runs must produce byte-identical output** — idempotency is part of the contract.

### Test File Split

Inside each example, tests are split by concern:

- `crud_test.go` — Create / Get / Update / Delete / Upsert per table
- `relationship_test.go` — O2O / O2M / M2O / M2M loaders, including soft-delete and nullable FK shapes
- `pagination_test.go` — offset + cursor pagination, sort orders
- `tenancy_test.go` (where applicable) — tenant isolation
- `cache_test.go` (where applicable) — read-through, invalidation, breaker
- `events_test.go` (where applicable) — pre/post hook order, event delivery

Splitting keeps individual files narrow enough to scan and lets a CI shard pick up just the relevant test for a given change.

### When to Add a New Example

Add a new example module when:

- A new dialect / driver needs end-to-end validation (e.g., `sqlite_stdlib` was a separate example to cover the `database/sql` adapter alongside the modernc CGO-free path).
- A new feature spans the whole pipeline (cache, tenancy, events, graphql each got their own example).
- An existing example would have to embed contradictory config to test a new shape.

Otherwise, prefer extending an existing example. The graphql example is the rule — exercises every relationship variant + every scalar registry category in a single module.

---

## 11. Codegen Unit Fixture Patterns

Two fixture rules apply only when writing **unit tests in `cmd/sqlgen/gen/`** that build template contexts by hand. End-to-end tests (§10) don't need to follow these — production codegen handles the wiring.

### Production-Wired Relationship FK Metadata

`RelationshipContext.FKGoType`, `FKNullable`, and `FKColumnGoType` are populated by `wireRelationshipFKMetadata` in `context_table.go` — a post-processing pass that runs after individual table contexts are built. Production codegen always sets them. **Unit fixtures that hand-build `RelationshipContext` values must set them manually**, or relationship loader templates emit incorrect FK conversions / nullability guards.

Each field has its own failure when a fixture leaves it unset:

- missing `FKNullable` — emits an unguarded null-FK loader
- missing `FKGoType` — the M2M target loader calls the wrong conversion
- missing `FKColumnGoType` — the nullable-wrapper loader calls `String()` on `NullUUID`

When you add a new relationship-shape unit test, populate every `FK*` field on the relationship. Don't rely on zero values.

**`FKOnTarget` belongs on that list too**, and it fails in a quieter way than the three above: it decides *whether an edge is emitted at all*, not how it is spelled. `nestedCandidateEdges` reads it to separate the two O2O shapes `type: one_to_one` cannot tell apart — a has-one edge (`true`) nests, a belongs-to edge (`false`) is PRD §9.9.1 shape 1 and nests nothing. A fixture that leaves it at the zero value therefore produces an O2O parent with *no* nested surface and a test that passes for the wrong reason. `false` is also three answers in one — the FK is on the source, the edge is M2M, or the target is absent from the parsed schema — so a fixture must set it to the shape it means rather than to whatever makes the assertion pass.

### Production-Wired UUID Generation Expression

`TableContext.PKAutoGenExpr` is populated by `attachUUIDGeneration` in `gen/uuid_generation.go` — a post-build pass, because the UUID library a generating call spells is a property of the whole generated package, not of one table (PRD §7.4). Production codegen always sets it.

**A fixture with `PKStrategy: config.PKStrategyApp` must set it too.** Left empty, the create and upsert templates render `pkValue = ` and the output does not parse. Set it to the spelling that matches the library the fixture's imports declare — a google-bound fixture asserting gofrs's `uuid.Must(uuid.NewV4())` tests nothing real. `BuildTableContexts` on its own does not fill it either: it is half the production path, so a white-box fixture that calls it should call `attachUUIDGeneration` afterwards.

### Compile-Verify Generated Output Locally

Per-template unit tests should not just diff against goldens — they should also verify the rendered output **compiles**. The pattern: write the rendered output to a temp dir, run `go build ./...` against it, fail loudly if it doesn't build. Many template bugs are silently absorbed by golden diffs (e.g., a missing `String()` method) but blow up at consumer compile time. Catch them at unit-test time.

---

## Appendix: Test Review Checklist

- [ ] Table-driven where there are multiple input combinations
- [ ] `t.Run` with descriptive case names
- [ ] Failure messages include: function name, inputs, got, want
- [ ] No assertion libraries imported
- [ ] Integration tests use testcontainers, not mocks
- [ ] Integration tests skip when `testing.Short()` is true
- [ ] Helpers call `t.Helper()`
- [ ] No `t.Fatal` from non-test goroutines
- [ ] Race-safe: passes `go test -race`
- [ ] Test data in `testdata/` directories
