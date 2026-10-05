# UUID Migration — Standard Library `uuid` as First-Class

> **Status: ARCHIVED (2026-09-16) — superseded by PRD §7.2 / §7.3 / §7.4 / §4.13 / §11.2 /
> §26.4 / §26.4.1 / §28.8 / §8.6 / §9.8.3 / §9.8.4** (synced 2026-09-15).
>
> **Do not read this file for current behavior.** The cited PRD sections are normative and
> supersede it on any conflict — see [PRD §7.4](../../PRD.md#74-built-in-type-integrations) for the
> UUID binding in particular. Archived because Phase 26 is closed and nothing in the code or the
> PRD references this document; it is kept for the record described below, not as a reference.
>
> **Original status note follows.** Executed and closed as **Phase 26**
> (all seven sub-items, 26.0–26.5 plus 26.3a; every step of the [§9](#9-migration-plan) plan
> landed). The normative behavior lives in the cited PRD sections, which supersede this document
> on any conflict; this file is retained for the measurement record
> ([§11](#11-how-the-findings-were-measured)), the driver and wire-format analysis
> ([§3](#3-driver-support), [§6](#6-wire-formats)), and the rollback reasoning
> ([§12](#12-risks-and-rollback)).
>
> **Two claims below were narrowed during execution and are superseded by the PRD**, not by a
> later section of this file: the generator's UUID spellings are per-integration rather than
> one library's (`Parse` is `uuid.FromString` on gofrs — PRD §7.4 "Parsing a UUID from a string";
> v4/v7 generation likewise — §7.4 "Generating UUID values", which also replaced §28.8's
> hardcoded `google/uuid` event-ID mandate), and the "no `driver.Valuer` / `sql.Scanner`, and
> none needed" property holds **for a `uuid` column only** — a non-`uuid` column retyped to the
> standard library's `uuid.UUID` does not scan under pgx, because `UUIDCodec` keys on the OID
> (§7.4). Separately, 26.2 found that the config surface PRD §4.13 / §7.4 called
> `views.<view>.overrides.types` does not exist; it was removed from the PRD rather than added to
> the config, so a view column participates through the global binding it resolves under and
> per-view type control is a column's `@type` annotation in the view SQL.
>
> **Target:** Go 1.27+ (`guidelines/GO.md` §12).
> **Touches:** PRD §7.2, §7.4, §26.4.1; `cmd/sqlgen/gotype/`; examples.

Go 1.27 ships a `uuid` package in the standard library. This document specifies
making it the **first-class** UUID binding in sqlgen — the type a `uuid` column
resolves to with no configuration — while keeping `github.com/google/uuid` and
`github.com/gofrs/uuid/v5` as supported overrides.

Every behavioral claim below was measured against the real toolchain
(`go1.27.1`), the real driver versions this project pins (`pgx v5.9.1`,
`modernc.org/sqlite v1.48.1`), and a real PostgreSQL 16. Section
[§11](#11-how-the-findings-were-measured) records how.

---

## Table of Contents

- [1. Summary](#1-summary)
- [2. What the standard library provides](#2-what-the-standard-library-provides)
- [3. Driver support](#3-driver-support)
- [4. Nullability: the one real difference](#4-nullability-the-one-real-difference)
- [5. What changes in generated code](#5-what-changes-in-generated-code)
- [6. Wire formats](#6-wire-formats)
- [7. Multiple UUID libraries in one project](#7-multiple-uuid-libraries-in-one-project)
- [8. Required code changes](#8-required-code-changes)
- [9. Migration plan](#9-migration-plan)
- [10. Example coverage](#10-example-coverage)
- [11. How the findings were measured](#11-how-the-findings-were-measured)
- [12. Risks and rollback](#12-risks-and-rollback)

---

## 1. Summary

| Question | Answer |
|----------|--------|
| Does the stdlib type work with our drivers? | **Yes**, on `pgx` and `stdlib`, reads and writes, null and non-null. |
| Does the missing `Valuer`/`Scanner` block us? | **No.** `database/sql` special-cases `uuid.UUID` directly; pgx routes it through the same `[16]byte` path `google/uuid` already uses. |
| Can a consumer adopt it today, with no sqlgen change? | **Yes**, via `overrides.types.uuid` — config only. |
| What does "first-class" additionally require? | A **small** codegen change: the default mapping for `uuid` columns. |
| Can two UUID libraries coexist in **one generated package**? | **No.** Fails to compile today. See [§7](#7-multiple-uuid-libraries-in-one-project). |
| Can they coexist in **one module**, in different packages? | **Yes.** Verified. |
| Is there a nullable wrapper? | **No `NullUUID`.** Nullable columns resolve to `*uuid.UUID`. |
| Does the JSON / cache wire format change? | **No.** Byte-identical. |
| Does the GraphQL schema change? | **Yes** — `NullUUID` scalar disappears. Breaking for clients. |

**The headline:** the user-visible cost of this migration is one GraphQL schema
change and the loss of the `Valid` field on nullable UUID columns. Everything
else — SQL round-trips, JSON, cache payloads, comparators, FK loading — is
unchanged or wire-identical.

---

## 2. What the standard library provides

```go
import "uuid"

type UUID [16]byte

func New() UUID                      // general purpose; currently v4
func NewV4() UUID                    // 122 random bits
func NewV7() UUID                    // 48-bit timestamp + 62 random bits, sortable
func Nil() UUID                      // all-zero
func Max() UUID                      // all-ones
func Parse(s string) (UUID, error)
func MustParse(s string) UUID

func (u UUID) String() string
func (u UUID) Compare(v UUID) int
func (u UUID) MarshalText() ([]byte, error)
func (u *UUID) UnmarshalText(b []byte) error
func (u UUID) AppendText(b []byte) ([]byte, error)
```

### What it does not have

| Missing | Consequence for sqlgen |
|---------|------------------------|
| `driver.Valuer` / `sql.Scanner` | **None.** See [§3](#3-driver-support). |
| `NullUUID` | Nullable columns use `*uuid.UUID`. See [§4](#4-nullability-the-one-real-difference). |
| `uuid.Nil` as a **variable** | `Nil` is a *function*. The zero literal is `uuid.UUID{}`, which is what the generator emits for `ZeroValue` anyway — so this affects handwritten code only. |
| `json.Marshaler` | **None.** `encoding/json` uses `MarshalText`, producing the same quoted string. |

### What it does have that matters

- **`NewV7`.** Time-ordered UUIDs, directly useful for cursor pagination
  ([PRD §14.2](../../PRD.md#142-cursor-based-relay-pagination)) and index locality on
  UUID primary keys. `google/uuid` gained v7 late; gofrs has it. Having it in
  the standard library removes the argument for a dependency on this axis.
- **Comparable.** `uuid.UUID` is an array type, so `==` works and it satisfies
  `comparable` — which is what `comparator.Slice[T]` requires
  ([PRD §11.2](../../PRD.md#112-comparator-types)).
- **`.String()`.** Satisfies `FKStringStringer`, so FK-to-string conversion for
  relationship loading is derived with no configuration.

---

## 3. Driver support

The absence of `driver.Valuer` / `sql.Scanner` is the most commonly cited
objection to the stdlib type, including in
[jackc/pgx#2636](https://github.com/jackc/pgx/issues/2636). It does not apply.
The standard library and pgx each wire the type in by other means.

### 3.1 `database/sql` (`output.driver: stdlib`)

Go 1.27 special-cases the type in both directions:

| Direction | Location | Behavior |
|-----------|----------|----------|
| Write | `database/sql/driver/types.go` | `case uuid.UUID: return vr.String(), nil` |
| Read | `database/sql/convert.go` | `case *uuid.UUID` for `string` **and** `[]byte` driver values — parses the text form, or copies the raw 16 bytes |

A plain `[16]byte` is still rejected (`unsupported type ..., a array`), so this
is a deliberate integration of the named type, not an incidental array path.

### 3.2 pgx (`output.driver: pgx`)

pgx needs no new release and no custom codec. The route is the one
`github.com/google/uuid` has always taken — that type is **also**
`type UUID [16]byte` and **also** implements no pgx interface:

1. `UUIDCodec.PlanEncode` requires `UUIDValuer`; the named type does not satisfy
   it, so pgx falls through to its wrap functions.
2. The named type is unwrapped to its underlying `[16]byte`.
3. `pgtype.go`'s explicit `case [16]byte:` wraps it in `pgtype.byte16Wrapper`.
4. `byte16Wrapper` **does** implement `UUIDValuer`, and dispatches to `UUIDCodec`.

The pgx maintainer's position on #2636 confirms this: *"Any `[16]byte` is
already supported, including the new `uuid.UUID` type."* What that issue asks
for — `pgtype.UUID` itself returning a stdlib UUID — is an ergonomic change to
`pgtype.UUID`'s own API, gated on pgx raising its minimum Go version. **Nothing
in sqlgen depends on it.**

### 3.3 Verified surface

Against PostgreSQL 16 / pgx v5.9.1, under **both** `output.driver` values:

- insert and scan, non-null and NULL (`*uuid.UUID` ⇄ SQL NULL)
- `WHERE id = $1`
- `= ANY($1)` over `[]uuid.UUID` — sqlgen's PostgreSQL `In()` form
  ([PRD Appendix A](../../PRD.md#appendix-a-sql-builder-functions))
- `uuid[]` array columns to and from `[]uuid.UUID`
- `RETURNING id` — the PK-resolution path ([PRD §9.7](../../PRD.md#97-pk-resolution-and-entity-return-path))
- `SendBatch` — used by `database/pgx`'s adapter
- `CopyFrom`
- `pgx.CollectRows` with `RowToStructByName` into `uuid.UUID` / `*uuid.UUID` fields
- **all five `QueryExecMode` settings**, including `SimpleProtocol` and `Exec`,
  where pgx must infer the parameter OID rather than being told it by the server

### 3.4 Scan into `uuid.UUID`, never into `pgtype.UUID`

`pgtype.UUID` is a struct carrying `Bytes [16]byte` + `Valid bool`. Scanning
into it and converting afterwards costs a `uuid.UUID(pu.Bytes)` step and loses
the nil-pointer NULL representation. Scanning straight into `uuid.UUID` /
`*uuid.UUID` skips both. That conversion is the entire "boilerplate" complaint
in #2636. Generated code already takes the direct path; handwritten code in
this repo must too (`guidelines/GO.md` §12).

---

## 4. Nullability: the one real difference

There is no `NullUUID`, so a nullable `uuid` column resolves to `*uuid.UUID`.

### 4.1 This is an existing, supported shape

It is not a new code path. `cmd/sqlgen/gotype` already classifies types three
ways:

```go
const (
    nullSQL     nullKind = iota // has sql.Null* variant and supports *T
    nullRef                     // reference type: nullable uses the same type
    nullPtrOnly                 // only *T for nullable, no sql.Null* equivalent
)
```

`nullPtrOnly` is already in production use for `time.Duration` (`interval`) and
`net.IPNet` (`cidr`). And `DeriveScalarExtraction` — which the O2M relationship
loader uses to read a possibly-null FK — already has an explicit pointer branch:

```go
if bare, ok := strings.CutPrefix(goType, "*"); ok {
    return ScalarExtraction{
        GuardExpr:    "$v != nil",
        UnwrapExpr:   "(*$v)",
        StringMethod: DeriveFKMethod(bare),   // ".UUID" suffix ⇒ FKStringStringer
    }
}
```

### 4.2 Side-by-side

| Axis | `uuid.NullUUID` | `*uuid.UUID` |
|------|-----------------|--------------|
| Null test | `v.Valid` | `v != nil` |
| Value access | `v.UUID` | `*v` |
| Zero value | `uuid.NullUUID{}` | `nil` |
| JSON (set) | `"6ba7b810-…"` | `"6ba7b810-…"` |
| JSON (null) | `null` | `null` |
| Comparator | `comparator.NullableID` | `comparator.NullableID` |
| GraphQL scalar | `NullUUID` | `UUID` |
| Distinguishes null from zero UUID | Yes | Yes |
| Extra allocation per non-null value | No | Yes (one 16-byte heap value) |
| Aliasing hazard | Caller may mutate through the pointer | — |

Two honest costs of the pointer form: a pointer indirection and allocation per
non-null value, and the ordinary Go hazard that a caller holding the pointer can
mutate the entity's field through it. Neither is specific to UUID — every
`nullPtrOnly` type in the project already carries them — and neither shows up in
generated code, which copies values rather than retaining pointers.

---

## 5. What changes in generated code

Measured by regenerating `cmd/sqlgen/testdata/examples/postgres` with
`nullable: uuid.NullUUID` removed. The **entire** generated diff, for a table
with one nullable UUID FK column:

```diff
- NullableRef uuid.NullUUID `db:"nullable_ref" json:"nullable_ref"`
+ NullableRef *uuid.UUID    `db:"nullable_ref" json:"nullable_ref"`

- if !(r.NullableRef.Valid) {
+ if !(r.NullableRef != nil) {

- key := r.NullableRef.UUID.String()
+ key := (*r.NullableRef).String()

- NullableRef omittable.Value[uuid.NullUUID] `json:"nullable_ref,omitzero"`   // CreateInput
+ NullableRef omittable.Value[*uuid.UUID]    `json:"nullable_ref,omitzero"`

- NullableRef omittable.Value[uuid.NullUUID] `json:"nullable_ref,omitzero"`   // UpdateInput
+ NullableRef omittable.Value[*uuid.UUID]    `json:"nullable_ref,omitzero"`
```

Five lines. `go build` and `go vet` clean. Unchanged: the filter struct, the
comparator family, scan code, cache code, event hooks, sorter, the import block.

---

## 6. Wire formats

### 6.1 JSON and cache payloads — identical

`google/uuid`'s `NullUUID` implements `MarshalJSON` / `UnmarshalJSON` that emit
and accept the bare string form, deliberately matching what a pointer produces.
Verified in both directions, including inside `omittable.Value[T]`:

| Shape | set | null | unset (omittable) |
|-------|-----|------|-------------------|
| `uuid.NullUUID` | `{"ref":"6ba7b810-…"}` | `{"ref":null}` | `{}` |
| `*uuid.UUID` | `{"ref":"6ba7b810-…"}` | `{"ref":null}` | `{}` |

**Consequence:** cache entries ([PRD §27.10](../../PRD.md#2710-serialization)) and any
consumer marshaling a model directly are byte-compatible across the migration.
No cache flush is required, and no fingerprint change is forced by the type
swap alone.

### 6.2 GraphQL — breaking

This is the one consumer-visible break. Measured on the `graphql` example:

```diff
  type Event {
-   userID: NullUUID
+   userID: UUID
  }

- scalar NullUUID
  scalar UUID
```

Also removed: the generated `MarshalNullUUID` / `UnmarshalNullUUID` pair in
`graph/scalars_gen.go`. Filter inputs are **unchanged** —
`userID: NullableIDComparator` either way, because comparator selection reads
through the wrapper ([PRD §7.3](../../PRD.md#73-nullable-handling)).

The resulting schema is arguably the more correct one: a nullable field of
scalar `UUID` is the idiomatic GraphQL spelling, and `NullUUID` existed only
because the Go wrapper type needed a distinct binding
([PRD §26.4.1](../../PRD.md#2641-scalar-marshaling)). It is still a breaking change
for any client with a generated schema, and must be released as one.

---

## 7. Multiple UUID libraries in one project

> **Short answer: not within a single generated package. Yes across packages.**

### 7.1 Same generated package — does not work

Both layouts fail. Config: global override to stdlib `uuid`, table-scoped
override to `github.com/google/uuid` for one table.

**`layout: single_file`:**

```
models/models_gen.go:13:2: uuid redeclared in this block
models/models_gen.go:13:2: "github.com/google/uuid" imported and not used
models/models_gen.go:1567:12: undefined: uuid.NullUUID
models/models_gen.go:3080:28: undefined: uuid.NullUUID
```

**`layout: file_per_table`** — fails identically, and reveals the cause: *every*
per-table file receives *both* imports, including `alpha_gen.go`, which uses
only the stdlib type. The import set is resolved **per package**, not per file
(`resolvePackageImports` in `cmd/sqlgen/gen/format.go` deliberately resolves
once over the concatenated bodies and seeds every file with the union).

Two distinct defects are visible in that output:

1. **Name collision.** `"uuid"` and `"github.com/google/uuid"` both bind the
   file-local package name `uuid`.
2. **Silent mis-resolution.** The surviving `uuid` binds to whichever import
   wins, so `uuid.NullUUID` — which the google-backed table needs — resolves
   against the standard library, which has no such identifier.

The second is the more dangerous shape: a type string of `uuid.UUID` is
*identical* for both libraries, so nothing in the emitted source distinguishes
them.

### 7.2 Why the existing alias mechanism does not solve it

sqlgen already handles one collision of this kind:

```go
const stdSQLAlias = "stdsql"

// A path belongs here only when its package name collides with one sqlgen
// itself emits references to.
var generatedImportAliases = map[string]string{"database/sql": stdSQLAlias}
```

with `aliasStdSQLQualifiers` rewriting `sql.NullX` → `stdsql.NullX`
([PRD §7.3](../../PRD.md#73-nullable-handling)). That works because the collision is
against sqlgen's *own* `sql` package and the colliding identifiers are a fixed,
enumerable set (`sql.Null…`), so a textual rewrite is sound.

The UUID case is **not** analogous. Both packages are named `uuid`, both export
`UUID`, and the consumer writes `type: uuid.UUID` for either. The qualifier
carries no information about which package is meant. Resolving it requires
carrying the **import path** alongside each column's resolved type through
emission and aliasing per column — a real change to the type-context plumbing,
not a second map entry.

### 7.3 Separate generated packages — works today

Verified: two `sqlgen.yml` files, two `output.dir`s, one module.

```
stdpkg/     → imports "uuid"
googlepkg/  → imports "github.com/google/uuid"
go build ./...  → clean
```

This is the supported arrangement for a project that genuinely needs two, and
it composes with the multi-package monorepo pattern the manifest and `.mcp.json`
emission already handle ([PRD §31.2](../../PRD.md#312-configuration)).

### 7.4 Position

**One UUID library per generated package** is the rule this document adopts.
It should be enforced, not merely documented — see
[§8.3](#83-reject-mixed-uuid-libraries-in-one-package).

---

## 8. Required code changes

The user-visible migration is config-only, but "first-class" and "safe" each
require a change.

### 8.1 Make the stdlib type the default binding

Today a `uuid` column with no override resolves to `string`:

```go
// cmd/sqlgen/gotype/gotype.go
"uuid": goString,
```

First-class means a new `typeInfo` and a mapping change:

```go
goUUID = typeInfo{
    name: "uuid.UUID",
    imp:  "uuid",
    zero: "uuid.UUID{}",
    kind: nullPtrOnly,       // no NullUUID; nullable ⇒ *uuid.UUID
    fk:   FKStringStringer,  // .String()
}
```

```diff
- "uuid": goString,
+ "uuid": goUUID,
```

`nullPtrOnly` gives `*uuid.UUID` for nullable columns through the existing
branch in `goTypeFromInfo`. `FKStringStringer` gives FK conversion. Only
PostgreSQL declares a `uuid` SQL type, so this is a one-line mapping change;
MySQL and SQLite are unaffected.

**This is a breaking change for any consumer relying on the `uuid` → `string`
default.** It belongs in the same release as the GraphQL break in
[§6.2](#62-graphql--breaking).

### 8.2 Register the standard library as an integration

Add `cmd/sqlgen/gotype/uuidstd/`, parallel to `uuidgoogle/` and `uuidgofrs/`:

```go
const ImportPath = "uuid"

func SQLTypes() []string { return []string{"uuid"} }

func Override() config.TypeOverride {
    return config.TypeOverride{
        Type:      "uuid.UUID",
        Import:    ImportPath,
        ZeroValue: "uuid.UUID{}",
        // No Nullable: the package has no wrapper. goTypeFromOverride's
        // value-typed branch yields *uuid.UUID.
    }
}
```

and add it to `knownIntegrations`.

> **Watch `enrichOverride` here.** Integration detection keys on the import
> path, and `enrichOverride` documents that *"Integration null types (e.g.,
> `uuid.NullUUID`, `decimal.NullDecimal`) are **always** used for nullable
> columns."* An integration whose `Nullable.Type` is empty must therefore leave
> the field empty rather than inherit — confirm `enrichOverride` does not
> back-fill from a different detected integration when a project declares both.

> **Pre-existing asymmetry worth fixing alongside.** `detectIntegrations()`
> walks **only** `r.globalOverrides`. A **table-scoped**
> `overrides.types.uuid` naming `github.com/google/uuid` is therefore *not*
> enriched, so dropping its `nullable:` silently yields `*uuid.UUID`, while the
> same edit to a *global* override is silently refilled with `uuid.NullUUID`.
> Same config text, two different results depending on nesting depth. This is
> a latent bug independent of this migration; file it separately.

### 8.3 Reject mixed UUID libraries in one package

Add a resolution-time validation error
([PRD §4.13](../../PRD.md#413-config-validation-rules), phase 3):

> **Two UUID libraries resolve within one generated package** — Error. Naming
> the two import paths, the tables that selected each, and directing the user
> to split them into separate `output.dir` packages.

Without this the failure mode is a non-compiling package at best, and
`undefined: uuid.NullUUID` at worst — a diagnostic that names neither the
config key nor the cause.

### 8.4 Documentation

| Document | Change |
|----------|--------|
| PRD §7.2 | `uuid` row: default Go type `string` → `uuid.UUID` |
| PRD §7.4 | Promote stdlib to the first table; demote `google`/`gofrs` to "alternative integrations"; record that the stdlib has no null wrapper |
| PRD §7.3 | Note the `nullPtrOnly` classification for `uuid.UUID` |
| PRD §26.4.1 | `NullUUID` scalar is emitted only for the wrapper-backed integrations |
| PRD §4.13 | The [§8.3](#83-reject-mixed-uuid-libraries-in-one-package) validation row |
| `guidelines/GO.md` §12 | Already updated — stdlib `uuid` in handwritten code |

---

## 9. Migration plan

Ordered so that each step is independently verifiable and the breaking changes
land together.

| Step | Work | Gate |
|------|------|------|
| 1 | `uuidstd` integration package ([§8.2](#82-register-the-standard-library-as-an-integration)). No default change. | Unit tests on the resolver; opting in via config produces `uuid.UUID` / `*uuid.UUID`. |
| 2 | Mixed-library validation error ([§8.3](#83-reject-mixed-uuid-libraries-in-one-package)). | A config naming two UUID import paths fails with a clear message rather than emitting a broken package. |
| 3 | Fix the `detectIntegrations` global-vs-table asymmetry ([§8.2](#82-register-the-standard-library-as-an-integration)). | Global and table-scoped overrides with identical text resolve identically. |
| 4 | Flip the default ([§8.1](#81-make-the-stdlib-type-the-default-binding)). | Full golden regeneration; every example compiles; E2E suite green. |
| 5 | Migrate examples ([§10](#10-example-coverage)): five to stdlib, `postgres_stdlib` held on `google/uuid`, `graphql_null_wrappers` moved to `gofrs/uuid/v5`. | `TestE2EGoldenFiles` byte-clean; integration suites green under Docker; a build of the example tree still pulls **both** wrapper libraries, proving neither integration went dark. |
| 6 | PRD + guideline updates ([§8.4](#84-documentation)). | — |

Steps 1–3 are additive and can land independently. Steps 4–5 are the breaking
release and must ship together with the GraphQL schema change.

---

## 10. Example coverage

Seven examples currently bind `github.com/google/uuid`; **no example exercises
`github.com/gofrs/uuid/v5` at all**, despite `cmd/sqlgen/gotype/uuidgofrs/`
existing. The migration closes that gap rather than widening it.

**Decision: two examples stay on a non-stdlib UUID — one per wrapper-backed
integration.** One would leave `uuidgofrs` untested, which is the state this
migration would otherwise lock in permanently: once the stdlib is the default,
nothing else would ever pull gofrs into a build.

| Example | After migration | Why |
|---------|-----------------|-----|
| `postgres` | **stdlib** | The broadest example; proves the default path end-to-end, including the nullable self-referential FK (`warehouses.nullable_ref`) that exercises the `*uuid.UUID` FK-extraction branch. |
| `graphql` | **stdlib** | Proves the `UUID`-scalar schema shape and that `NullUUID` is no longer emitted. |
| `graphql_top_level` | **stdlib** | Follows `graphql`. |
| `tenancy`, `tenancy_postgres` | **stdlib** | Tenant columns are UUID; proves tenancy composes with the pointer nullable form. |
| `postgres_stdlib` | **`github.com/google/uuid`** | **Retained as the override regression guard.** Keeps the `uuid.NullUUID` wrapper path, the `NullUUID` GraphQL scalar, and `enrichOverride`'s integration back-fill under test. Its `output.driver: stdlib` also keeps the `database/sql` path covered with a wrapper type. |
| `graphql_null_wrappers` | **`github.com/gofrs/uuid/v5`** | **Closes the gofrs coverage gap.** This example exists to exercise Null-wrapper scalars (it already aliases `stdsql "database/sql"`), which makes it the natural home for the second wrapper-backed integration. |

This leaves, after migration:

- the stdlib path as the default, covered by five examples across both layouts,
  GraphQL, and tenancy;
- **both** wrapper-backed integrations under continuous test — `google/uuid` in
  `postgres_stdlib`, `gofrs/uuid/v5` in `graphql_null_wrappers` — so the
  override path cannot silently rot;
- both `output.driver` values covered on both shapes.

### 10.1 Mechanics per example

Each migrating example needs two edits, and the golden tree regenerated:

1. **`sqlgen.yml`** — `overrides.types.uuid.import`, plus removing any
   `nullable:` line (the stdlib has no wrapper to name). Table-scoped
   `overrides` and `column_map.<col>.import` entries naming a UUID library must
   move too, or the package trips the [§8.3](#83-reject-mixed-uuid-libraries-in-one-package)
   rule.
2. **`go.mod`** — add or drop the library. `graphql_null_wrappers` currently
   requires `github.com/google/uuid v1.6.0` and swaps it for
   `github.com/gofrs/uuid/v5`; the five stdlib-bound examples drop the
   requirement entirely, which is itself part of the point.

### 10.2 What the gofrs move exercises that the google one does not

The two wrapper integrations are not redundant. `uuidgofrs` differs from
`uuidgoogle` in the field that governs emitted zero values:

| Integration | `Type` | `ZeroValue` | `Nullable.Type` |
|-------------|--------|-------------|-----------------|
| `uuidgoogle` | `uuid.UUID` | `uuid.UUID{}` | `uuid.NullUUID` |
| `uuidgofrs` | `uuid.UUID` | **`uuid.Nil`** | `uuid.NullUUID` |
| `uuidstd` ([§8.2](#82-register-the-standard-library-as-an-integration)) | `uuid.UUID` | `uuid.UUID{}` | *(none)* |

gofrs spells the zero UUID as a package **variable** (`uuid.Nil`), google as a
composite literal, and the standard library as neither — its `Nil` is a
**function**, so the zero literal `uuid.UUID{}` is what the generator must emit.
Three spellings of the same value across three libraries is precisely why
`ZeroValue` is a per-integration field, and putting gofrs under test is what
keeps that field honest.

Note also that all three declare the Go type string `uuid.UUID`, and both
wrappers declare `uuid.NullUUID`. The built-in GraphQL scalar registry
([PRD §26.4.1](../../PRD.md#2641-scalar-marshaling)) keys on those bare strings, so it
serves either wrapper without change — and cannot distinguish them. That is
sound only while [§7.4](#74-position)'s one-library-per-package rule holds, and
is a second reason to land [§8.3](#83-reject-mixed-uuid-libraries-in-one-package)
before the default flip.

---

## 11. How the findings were measured

Nothing here is inferred from an API's shape. Each claim came from running the
thing.

| Claim | Method |
|-------|--------|
| stdlib API surface, missing `Valuer`/`Scanner` | `go doc uuid`; interface assertions against `go1.27.1` |
| `database/sql` special-casing | Read `$GOROOT/src/database/sql/convert.go` and `driver/types.go`; confirmed a plain `[16]byte` is still rejected |
| `database/sql` round-trip | Real insert/scan/NULL round-trip through `modernc.org/sqlite v1.48.1` |
| pgx support, all exec modes, arrays, batch, `CopyFrom`, `RETURNING` | Real queries against PostgreSQL 16 in Docker, `pgx v5.9.1`, both `output.driver` values |
| pgx mechanism | Read `pgtype/uuid.go`, `pgtype/pgtype.go`, `pgtype/builtin_wrappers.go` at the pinned version |
| The 5-line codegen diff | Regenerated `examples/postgres` both ways with a locally built `sqlgen`; diffed; `go build` + `go vet` |
| JSON wire equality | Marshal/unmarshal both shapes, bare and inside `omittable.Value[T]` |
| GraphQL schema delta | Regenerated `examples/graphql` with `--no-graphql`; diffed the emitted `.graphqls` and `scalars_gen.go` |
| Mixed-library failure, both layouts | Purpose-built two-table probe; captured compiler output |
| Two packages succeeding | Same probe, split into two `sqlgen.yml` / two `output.dir` |
| `enrichOverride` behavior and the global-vs-table asymmetry | Read `cmd/sqlgen/gotype/gotype.go`; reproduced both outcomes |

The generated-baseline run was validated by first reproducing the committed
example output **byte-for-byte**, so every diff shown is attributable to the
config change and not to the harness.

---

## 12. Risks and rollback

| Risk | Severity | Mitigation |
|------|----------|------------|
| GraphQL schema break (`NullUUID` removed) | **High** — client-visible | Ship in a single release with [§8.1](#81-make-the-stdlib-type-the-default-binding); call it out in release notes; consumers who cannot take it keep `overrides.types.uuid` on `google/uuid` and are fully insulated. |
| `uuid` default changes from `string` | **High** — silently retypes columns for anyone who never configured UUID | Same release; the override escape hatch is one config block. |
| Loss of `.Valid` in consumer code | Medium | Mechanical: `v.Valid` → `v != nil`, `v.UUID` → `*v`. Compiler catches every site. |
| Mixed-library config emitting broken code | Medium | [§8.3](#83-reject-mixed-uuid-libraries-in-one-package) converts it to a config error. Must land **before** the default flip, or a project with one table still pinned to `google/uuid` breaks confusingly. |
| Go 1.27 floor | Low | Already the stated target (`guidelines/GO.md` §12, PRD §1). |
| pgx changes `pgtype.UUID` later | Low | sqlgen does not use `pgtype.UUID`; it scans into the resolved type. [§3.4](#34-scan-into-uuiduuid-never-into-pgtypeuuid) keeps it that way. |

**Rollback.** Steps 1–3 are additive and need none. Step 4 is a one-line revert
of the mapping plus a golden regeneration. Because JSON and cache payloads are
byte-identical in both directions ([§6.1](#61-json-and-cache-payloads--identical)),
a rollback does **not** strand cache entries — the only irreversible-feeling
surface is the published GraphQL schema, which reverts with the same
regeneration.
