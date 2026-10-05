# SQLGen — Go Coding Standards

> Go conventions for this project, targeting **Go 1.27+**.
> Derived from [Google's Go Style Guide](https://google.github.io/styleguide/go/),
> adapted to SQLGen's architecture and patterns.

---

## Table of Contents

- [1. Guiding Principles](#1-guiding-principles)
- [2. Formatting](#2-formatting)
- [3. Naming](#3-naming)
- [4. Package Design](#4-package-design)
- [5. Error Handling](#5-error-handling)
- [6. Interfaces](#6-interfaces)
- [7. Generics](#7-generics)
- [8. Context Usage](#8-context-usage)
- [9. Concurrency](#9-concurrency)
- [10. Documentation](#10-documentation)
- [11. Dependencies](#11-dependencies)
- [12. Go 1.27 Features](#12-go-127-features)

---

## 1. Guiding Principles

In priority order (from Google's style guide):

1. **Clarity** — Code is clear to the reader, not just the author. Names, structure, and comments explain *what* and *why*.
2. **Simplicity** — Code accomplishes its goal in the simplest way possible. Prefer the *least mechanism* — standard language constructs over libraries, stdlib over dependencies.
3. **Concision** — High signal-to-noise ratio. Table-driven tests, familiar idioms, no boilerplate.
4. **Maintainability** — Easy to modify correctly. Minimal coupling, clear APIs, comprehensive tests.
5. **Consistency** — Tiebreaker when the above don't decide. Follow existing patterns in the codebase.

---

## 2. Formatting

### gofmt

All Go source files must pass `gofmt`. This is non-negotiable and enforced in CI.

### goimports

Use `goimports` for import organization. Import groups, separated by blank lines:

```go
import (
    // 1. Standard library
    "context"
    "fmt"

    // 2. Third-party packages
    "github.com/jackc/pgx/v5"

    // 3. Internal packages (within this project)
    "github.com/yourorg/sqlgen/sql"
)
```

### Line Length

No fixed maximum. Prefer refactoring over line-splitting. Do NOT split lines:
- Before indentation changes (function signatures, conditionals)
- To break URLs or long strings across lines

If a function signature is too long, extract parameters into an options struct — don't break the signature across multiple lines.

### Conditionals

Do not break `if` conditions across lines. Extract complex boolean expressions into named local variables:

```go
// Good
inTransaction := db.CurrentStatusIs(db.InTransaction)
keysMatch := db.ValuesEqual(db.TransactionKey(), row.Key())
if inTransaction && keysMatch {
    // ...
}

// Bad
if db.CurrentStatusIs(db.InTransaction) &&
    db.ValuesEqual(db.TransactionKey(), row.Key()) {
    // ...
}
```

---

## 3. Naming

### General Rules

- **MixedCaps** always. `MaxLength`, `maxLength`. Never `max_length` or `MAX_LENGTH`.
- **No stuttering.** Avoid repeating the package name in exported identifiers:
  - `sql.Dialect` not `sql.SQLDialect`
  - `comparator.String` not `comparator.StringComparator`
  - `database.New(pool)` not `database.NewDatabase(pool)`
- **Length proportional to scope.** Short names for small scopes, descriptive names for wide scopes.
- **No type names in variable names.** `users` not `userSlice`. `count` not `numUsers`.

### Packages

- Short, lowercase, single-word when possible. No underscores.
- Avoid generic names: no `util`, `common`, `helper`, `base`.
- Package name should describe what the package *provides*, not what it *contains*.

### Interfaces

- Single-method interfaces: method name + `er` suffix. `Reader`, `Scanner`, `Querier`.
- Multi-method interfaces: descriptive noun. `Dialect`, `Condition`.
- Define interfaces at the **consumer**, not the producer, when possible.

### Receivers

- 1-2 letter abbreviation of the type name. Consistent across all methods of a type.
- `func (d *PostgresDialect)` not `func (dialect *PostgresDialect)` or `func (self *PostgresDialect)`.
- Never `this` or `self`.

### Constants

- MixedCaps. `MaxBatchSize` not `MAX_BATCH_SIZE`.
- Name constants by their role, not their value. `DefaultPageSize` not `OneHundred`.

### Initialisms

- Consistent casing within each initialism: `URL` or `url`, never `Url`. `JSONB` or `jsonb`, never `Jsonb`.
- `ID` not `Id`. `SQL` not `Sql`. `HTTP` not `Http`.

### Getters

- No `Get` prefix. `Counts()` not `GetCounts()`.
- Use `Compute` or `Fetch` for expensive operations to signal cost.

### Exported vs Unexported

- Export only what consumers need. Start unexported; promote when there's a real use case.
- In the runtime module, the public API is what generated code imports — be deliberate.

---

## 4. Package Design

### Size

A package should be cohesive. Related types that are frequently used together belong in the same package. Types that are independently useful belong in separate packages.

### Import Discipline

- Runtime core packages (`sql/`, `comparator/`, `omittable/`, `database/`) depend only on stdlib.
- Driver packages import their driver library and `database/`.
- Never import from the parser or CLI modules in runtime code.
- See [ARCHITECTURE.md](./ARCHITECTURE.md) for module boundary rules.

### Avoid Circular Imports

If package A needs a type from package B and B needs a type from A, introduce an interface at the consumer side or extract the shared type into a third package.

### No Re-Exporting

Do not re-export types, constants, or functions from another package via type aliases (`type X = other.X`) or constant aliases (`const X = other.X`). If package A uses a type defined in package B, callers should reference `b.X` directly — never `a.X` that forwards to `b.X`.

Re-exports create two names for the same thing, silently drift when the original is renamed or removed, and hide the real dependency from readers. "I already import package A, so I'd like `a.X` instead of `b.X`" is not a sufficient reason — just import B.

The only legitimate re-export is a **facade or aggregator package** that exists specifically to expose a curated public API — for example, a top-level package that re-exports the subset of types from internal packages that consumers should use. Facade packages must be:
- **Deliberately designed as an API surface** (not an accidental shortcut).
- **Documented as such** in the package comment.
- **The single entry point** for consumers — internal packages are typically unexported or named `internal/`.

SQLGen does not currently have facade packages. If you find yourself writing `type X = other.X` for convenience, delete it and use `other.X` directly at the call site.

```go
// Bad — re-export for convenience
package gen

type PKStrategyName = config.PKStrategy
const PKStrategyDB = config.PKStrategyDB

// Good — callers reference config.PKStrategy directly
package gen

// (no alias; use config.PKStrategy in type signatures and config.PKStrategyDB at call sites)
```

---

## 5. Error Handling

### Returning Errors

- `error` is always the **last** return value.
- Return the `error` **interface**, not concrete types. This prevents typed-nil bugs:

```go
// Good
func Parse(input string) (*Schema, error) { ... }

// Bad — returns *ParseError, can cause typed-nil issues
func Parse(input string) (*Schema, *ParseError) { ... }
```

### Error Flow

Handle errors early. Normal code proceeds unindented:

```go
// Good
if err != nil {
    return fmt.Errorf("parsing schema: %w", err)
}
// normal flow continues here

// Bad
if err != nil {
    return err
} else {
    // normal flow in else block
}
```

### Error Strings

- Lowercase, no trailing punctuation. They are fragments:

```go
// Good
fmt.Errorf("parsing column %q: %w", name, err)

// Bad
fmt.Errorf("Failed to parse column %q: %w.", name, err)
```

### Wrapping with Context

Add meaningful context, not redundant detail. Use `%w` when callers need `errors.Is`/`errors.As`; use `%v` at system boundaries or when the original error should not be part of the API:

```go
// Good — adds context, preserves error chain
return fmt.Errorf("building select for %s: %w", table.Name, err)

// Bad — redundant (underlying error already says this)
return fmt.Errorf("query failed: error executing SQL query: %w", err)
```

### Sentinel Errors and Structured Errors

This project defines sentinel errors (`ErrNotFound`, `ErrConstraintViolation`, etc.) and structured errors (`ConstraintError`). Use `errors.Is` for sentinel checks and `errors.As` (or `errors.AsType` in Go 1.26+) for structured extraction:

```go
if errors.Is(err, database.ErrNotFound) {
    return nil, nil // not found is acceptable here
}

// Go 1.26+: prefer errors.AsType over errors.As for type safety
if ce, ok := errors.AsType[*database.ConstraintError](err); ok {
    log.Printf("constraint %s violated on column %s", ce.Constraint, ce.Column)
}
```

### Never Ignore Errors

Never discard errors with `_` unless justified with a comment. Handle them, return them, or log them:

```go
// Acceptable — bytes.Buffer.Write never fails
_, _ = buf.WriteString(clause) // bytes.Buffer.Write cannot fail

// Bad
result, _ := db.Query(ctx, sql, args...)
```

---

## 6. Interfaces

### Keep Small

Prefer small interfaces (1-3 methods). The `Querier` interface is a good example — it defines the minimal surface for database interaction.

### Consumer-Defined

Define interfaces where they're used, not where they're implemented. If package `gen/` needs to call parser methods, define the interface in `gen/`, not in `parser/`.

Exception: the `Dialect` and `Querier` interfaces are defined in their own packages because they are shared across many consumers.

### Accept Interfaces, Return Concretes

```go
// Good — accepts interface, returns concrete
func New(q database.Querier) *UserClient { ... }

// Bad — returns interface (hides capabilities)
func New(q database.Querier) UserQuerier { ... }
```

### No Premature Interfaces

Do not create an interface for a single implementation. Wait until there is a second consumer or a real need for testability.

---

## 7. Generics

### When to Use

Generics are appropriate in this project for:
- `omittable.Value[T]` — presence-tracking wrapper
- `comparator.Number[T]`, `comparator.Enum[T]`, `comparator.Slice[T]` — type-parameterized filters
- Utility functions operating on multiple types (e.g., slice helpers)

### When Not to Use

- Single-type instantiations. If it's only ever `Number[int64]`, just write `Int64Number`.
- When `any` and a type switch would be simpler and clearer.
- For building DSLs or abstractions that obscure intent.

### Constraints

Prefer standard constraints from `constraints` or define project-local ones when needed. Name constraints by what they require:

```go
type Ordered interface {
    ~int | ~int64 | ~float64 | ~string
}
```

---

## 8. Context Usage

- `context.Context` is always the **first parameter**, named `ctx`.
- Never store `context.Context` in a struct field.
- Never create custom context types.
- Use `context.Background()` only in `main()` and initialization code.
- In HTTP handlers, use `req.Context()`.
- Pass context through the full call chain — every database operation, every transaction.

```go
// Good
func (c *UserClient) GetByID(ctx context.Context, id int64) (*User, error) { ... }

// Bad — context buried or missing
func (c *UserClient) GetByID(id int64) (*User, error) { ... }
func (c *UserClient) GetByID(id int64, ctx context.Context) (*User, error) { ... }
```

---

## 9. Concurrency

### Goroutine Lifetime

Every goroutine must have a clear, deterministic shutdown path. Use `context.Context` for cancellation and `sync.WaitGroup` or `errgroup.Group` for coordination.

```go
g, ctx := errgroup.WithContext(ctx)
for _, rel := range relationships {
    g.Go(func() error {
        return loadRelationship(ctx, rel)
    })
}
if err := g.Wait(); err != nil {
    return err
}
```

### Mutex Usage

- `sync.Mutex` fields must be unexported.
- Types containing a `sync.Mutex` must use pointer receivers on all methods.
- Never copy a type containing a `sync.Mutex`.

### Prefer Synchronous Functions

Write synchronous functions. Let callers add concurrency. A function that spawns goroutines internally is harder to reason about, test, and compose.

Exception: generated relationship loading uses `errgroup` by design (specified in the PRD).

---

## 10. Documentation

### Doc Comments

- All exported types, functions, and methods MUST have doc comments.
- Start with the entity name as a complete sentence:

```go
// PostgresDialect implements the Dialect interface for PostgreSQL.
type PostgresDialect struct { ... }

// BuildSelect generates a SELECT statement for the given table and options.
func BuildSelect(d Dialect, t Table, opts SelectOptions) (string, []any) { ... }
```

### Package Comments

One per package, immediately above the `package` clause:

```go
// Package sql provides dialect-aware SQL statement builders and core types
// used by generated SQLGen code.
package sql
```

### When to Comment

- **Comment the *why*, not the *what*.** If the code is clear, don't add a comment restating it.
- **Comment deviations.** When deviating from an expected pattern, explain why.
- **Comment dialect-specific behavior.** SQL generation differs across PostgreSQL, MySQL, and SQLite — annotate dialect-specific branches.
- **Keep it short.** Default to one or two sentences. Go longer only when the code cannot show the reason itself: a subtle invariant, a dialect quirk, a non-obvious ordering constraint. Concretely:
  - State the rule the code keeps now, not how it got there. "This used to fail because…" and "Previously…" are history; they belong in the commit message.
  - Don't restate the identifier. A comment on `TestFilter_QuotesDigitLeadingColumn` that opens "pins that a filter quotes a digit-leading column" adds nothing.
  - Don't enumerate every caller or case the code handles; name the one that makes the code non-obvious.
  - If deleting a sentence loses nothing, delete it. A long comment describes more of its surroundings, so it goes stale sooner.
- **A comment must stand on its own.** A reader should understand it without opening anything it cites. A reference is optional extra context, never the explanation.
- **No FIX IDs, phase numbers, or sub-item IDs in code.** `FIX-NNN`, `Phase N`, sub-item IDs like `N.M` and paths into the tracker are project history: they mean nothing to outside readers and go stale against the tracker. This applies to comments, string literals, error messages, test names, and fixture data. The ID belongs in the commit message; the comment states the behavior or the defect the code guards against.
- **No design-decision labels in code.** Row labels from design-doc and PRD tables (`D18(a)`, `E8`, `T9`, `G5`) are just as opaque: a reader must open the doc to learn what `D7` means. Name the rule instead ("refresh is never tenant-scoped", "the nullable-FK rule"), with a section cite where one helps (`PRD §9.9.4`). Same scope as the rule above.
- **PRD and design-doc citations are allowed in handwritten code** (`PRD §9.4`, `CACHE.md §10.1`), subject to the stand-on-its-own rule. Never in anything emitted into generated code — see `TEMPLATES.md` §8.

### Named Return Parameters

Use only when returning 2+ parameters of the same type, or when the name communicates something important to the caller. Do not use named returns just to avoid declaring variables:

```go
// Good — names clarify the two string returns
func SplitTable(qualified string) (schema, name string) { ... }

// Bad — named returns add no clarity
func BuildSelect(...) (query string, args []any) { ... }
```

---

## 11. Dependencies

### Least Mechanism Principle

Prefer, in order:
1. Core language constructs (channels, slices, maps, loops, structs)
2. Standard library
3. Well-known, established libraries (pgx, errgroup)
4. New dependencies only as a last resort

### Adding Dependencies

Before adding a dependency:
- Can the standard library do this?
- Is the dependency well-maintained and widely used?
- Does it introduce transitive dependencies that matter for the runtime module?

Remember: the runtime module's core packages must have **zero heavy dependencies**. Every new import in `sql/`, `comparator/`, `omittable/`, or `database/` is a serious decision.

---

## 12. Go 1.27 Features

This project targets Go 1.27+. Use these features where appropriate.

### Standard Library `uuid`

Go 1.27 ships a `uuid` package in the standard library. It is the preferred
source of UUID types in **handwritten** code in this repo — tests, fixtures,
examples, and any internal helper that needs to mint or parse a UUID. Reach for
`github.com/google/uuid` or `github.com/gofrs/uuid/v5` only where an existing
generated-code integration requires them (see below).

```go
import "uuid"

id := uuid.New()           // v4 today; the general-purpose constructor
id = uuid.NewV4()          // explicit v4 — 122 random bits
id = uuid.NewV7()          // explicit v7 — 48-bit timestamp, sortable
parsed, err := uuid.Parse(s)
```

The full surface is small: `New`, `NewV4`, `NewV7`, `Nil`, `Max`, `Parse`,
`MustParse`, and the methods `String`, `Compare`, `MarshalText`,
`UnmarshalText`, `AppendText`.

**`uuid.UUID` is `[16]byte`, and it does *not* implement `driver.Valuer` or
`sql.Scanner`.** This is deliberate and it does not block database use — the
standard library wires the type in directly instead:

- **Writes.** `database/sql/driver`'s default parameter converter has a
  `case uuid.UUID` arm that sends the canonical string form.
- **Reads.** `database/sql`'s `convertAssign` has `*uuid.UUID` arms for both
  `string` and `[]byte` driver values, parsing the text form or copying the
  raw 16 bytes.
- **pgx** (`output.driver: pgx`). Routed by the same machinery that already
  carries `github.com/google/uuid`, whose `UUID` is likewise `[16]byte` and
  likewise implements no pgx interface. pgx unwraps the named type to its
  underlying `[16]byte`, wraps that in `pgtype.byte16Wrapper` — which does
  implement `UUIDValuer` — and hands it to `UUIDCodec`. No custom codec, and
  no pgx release, is required.
- **pgx via database/sql** (`output.driver: stdlib`). Works through the same
  path.

Verified end-to-end against PostgreSQL 16 on pgx v5.9.1 under both driver
settings: insert and scan of non-null and NULL values, `WHERE id = $1`,
`= ANY($1)` over a `[]uuid.UUID`, `uuid[]` columns to and from
`[]uuid.UUID`, `RETURNING id`, `SendBatch`, `CopyFrom`, `RowToStructByName`,
and all five `QueryExecMode` settings including the simple protocol.

**Scan into `uuid.UUID`, not into `pgtype.UUID`.** `pgtype.UUID` is a struct
carrying `Bytes [16]byte` and `Valid bool`, so scanning into it and converting
afterwards costs a `uuid.UUID(pu.Bytes)` step and loses the nil-pointer NULL
representation. Scanning straight into `uuid.UUID` / `*uuid.UUID` skips both.
That conversion step is the only friction reported against pgx for the stdlib
type ([jackc/pgx#2636](https://github.com/jackc/pgx/issues/2636)); the
maintainer's answer there is that any `[16]byte` — the new `uuid.UUID`
included — is already supported. What remains open is whether `pgtype.UUID`'s
own API will return a stdlib UUID directly, which is gated on pgx raising its
minimum Go version. Nothing in this project depends on that changing.

Do **not** write a wrapper type solely to add `Valuer`/`Scanner`. It is not
needed on any driver path, and it would take the type off both the stdlib's
special-cased arms and pgx's `[16]byte` unwrap.

**The gap is nullability.** There is no `NullUUID`, and `Nil` is a *function*
(`uuid.Nil()`), not a package variable. A nullable UUID is spelled
`*uuid.UUID`, which round-trips correctly in both directions on `database/sql`
and pgx. A zero value is written `uuid.UUID{}`.

```go
var parent *uuid.UUID                       // nullable column
err := row.Scan(&parent)                    // SQL NULL leaves it nil
```

This matters for the generated-code integrations in PRD §7.4, which pair each
UUID library with a dedicated null type (`uuid.NullUUID`). The standard library
offers no such partner, so a stdlib-backed integration can only express
nullable columns as pointers. Treat adopting it there as a spec decision, not a
mechanical swap.

### Generic Methods

Go 1.27 allows methods to declare their own type parameters. Use them where a
method genuinely varies over a type the receiver does not already fix — not to
replace a package-level generic function that reads fine today.

```go
func (c *Client) As[T any](ctx context.Context) (T, error)
```

Do not retrofit existing package-level generic functions into generic methods
for style. The receiver's own type parameters remain the default place to put
them.

### strings.CutLast / bytes.CutLast

`CutLast` slices around the **last** occurrence of a separator, returning
`(before, after string, found bool)`. Prefer it over a manual
`strings.LastIndex` + slice pair — notably when splitting a fully-qualified Go
type into its import path and type name, which the API scalar config does at
the last `.` (PRD §26.3).

```go
// gopkg.in/yaml.v3.Node -> ("gopkg.in/yaml.v3", "Node", true)
pkg, typeName, ok := strings.CutLast(goType, ".")
```

### errors.AsType (prefer over errors.As)

```go
// Type-safe; avoids declaring a variable first
if ce, ok := errors.AsType[*ConstraintError](err); ok {
    // use ce
}

// Older pattern (still valid, but less concise)
var ce *ConstraintError
if errors.As(err, &ce) {
    // use ce
}
```

### Enhanced new() with Initial Values

```go
// new() accepts an expression for the initial value
cfg := &Config{
    PageSize: new(100),
    Timeout:  new(5 * time.Second),
}

// Older pattern
pageSize := 100
cfg := &Config{
    PageSize: &pageSize,
}
```

### Self-Referential Generic Constraints

Useful for types that need to operate on themselves (e.g., composable filter types):

```go
type Composable[C Composable[C]] interface {
    And(C) C
    Or(C) C
}
```

Use only when there is a genuine need. Do not introduce self-referential constraints for style.

### go fix Modernizers

Run `go fix ./...` periodically to apply modernization passes. `go tool fix help` lists the available analyzers. The ones that most often fire in this repo:

| Analyzer | Rewrite |
|----------|---------|
| `errorsastype` | `errors.As` → `errors.AsType[T]` |
| `newexpr` | pointer-to-local → `new(expr)` |
| `omitzero` | `omitempty` → `omitzero` on struct fields |
| `stringsseq` | ranging over `Split`/`Fields` → `SplitSeq`/`FieldsSeq` |
| `stringscut` | `strings.Index` + slicing → `strings.Cut` |
| `waitgroupgo` | `wg.Add(1)` / `go` / `wg.Done()` → `wg.Go` |

Go 1.27 adds `atomictypes` (basic types in `sync/atomic` calls → atomic types), `embedlit` (simplify embedded-field references in composite literals), `slicesbackward` (backward loops → `slices.Backward`), and `unsafefuncs` (unsafe pointer arithmetic → function calls).

### Goroutine Leak Detection

As of Go 1.27 `goroutineleak` is a predefined `runtime/pprof` profile — no `GOEXPERIMENT` flag is required. It reports goroutines blocked on primitives that cannot be unblocked, which is what catches leaks in concurrent relationship loading and transaction management.

```go
pprof.Lookup("goroutineleak").WriteTo(w, 1)
```

### Green Tea GC

The Green Tea garbage collector has been the default since Go 1.26, providing a 10–40% reduction in GC overhead. No code changes are needed, but be aware:
- `GOEXPERIMENT=nogreenteagc` disables it if needed for debugging.
- Heap base address randomization is enabled on 64-bit platforms (security improvement, no code impact).

### encoding/json/v2

Go 1.27 ships `encoding/json/v2` alongside the existing `encoding/json`, with stricter defaults (invalid UTF-8 and duplicate object names are rejected) and a lower-level `encoding/json/jsontext` companion.

**This project stays on `encoding/json` for now.** The cache serializer and the `types.JSON` column type both depend on v1 marshaling behavior, and `omitzero` — which `omittable.Value[T]` relies on — is already v1 behavior. Moving to v2 changes wire output for existing consumers, so it is a spec decision rather than an import swap.

---

## Appendix: Code Review Checklist

Before submitting or approving Go code in this project:

- [ ] `gofmt` and `goimports` clean
- [ ] No stuttering in names (package name not repeated in identifiers)
- [ ] Errors returned as last value, using `error` interface type
- [ ] Error strings lowercase, no trailing punctuation
- [ ] `context.Context` is first parameter where needed
- [ ] No `context.Context` stored in struct fields
- [ ] Interfaces are small and defined at the consumer
- [ ] No unnecessary dependencies added to runtime core packages
- [ ] Exported identifiers have doc comments
- [ ] No `panic` for recoverable errors (use `error` returns)
- [ ] Goroutines have clear shutdown paths
- [ ] Module boundary rules respected (see ARCHITECTURE.md)
