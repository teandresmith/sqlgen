# SQLGen — Template & Code Generation Standards

> How to write, structure, test, and maintain Go `text/template` files for code generation.
> Read this before modifying any template or adding a new generated artifact.

---

## Table of Contents

- [1. Principles](#1-principles)
- [2. File Structure](#2-file-structure)
- [3. Template Naming](#3-template-naming)
- [4. Writing Templates](#4-writing-templates)
- [5. Custom Template Functions (funcmap)](#5-custom-template-functions-funcmap)
- [6. Template Context](#6-template-context)
- [7. Whitespace Control](#7-whitespace-control)
- [8. Generated Code Quality](#8-generated-code-quality)
- [9. Formatting Pipeline](#9-formatting-pipeline)
- [10. Deterministic Output](#10-deterministic-output)
- [11. Golden File Testing](#11-golden-file-testing)
- [12. Adding a New Template](#12-adding-a-new-template)
- [13. GraphQL Codegen](#13-graphql-codegen)

---

## 1. Principles

1. **Logic in Go, layout in templates.** Templates describe the *shape* of the output. Decisions — which comparator type to use, whether a field is omittable, how to convert a UUID FK — belong in `funcmap.go` or `context.go`. A template with deeply nested `{{ if }}` blocks is a template that needs refactoring.
2. **One template per concern.** Each CRUD operation gets its own file. A template produces one logical unit (e.g., `Create` and `CreateMany` methods), not an entire file mixing unrelated operations.
3. **Shared fragments eliminate duplication.** Common patterns (field rendering, filter structs, imports, doc comments) live in `shared/` and are invoked by name. Copy-pasting between table and view templates is a bug.
4. **Generated code is a product.** The output must be idiomatic Go that passes `goimports`, `go vet`, and `golangci-lint` without exceptions. If you wouldn't write it by hand, don't generate it.
5. **Determinism is non-negotiable.** The same schema and config must produce byte-identical output on every run. No map iteration, no unsorted slices, no timestamps in output.

**Reference implementation targets:** PRD Section 9.8 contains concrete Go implementation examples for every generated method. These are the exact patterns that templates must produce — consult Section 9.8 before writing any template that generates client methods, scan functions, or relationship loaders.

---

## 2. File Structure

All templates live under `gen/templates/`. The directory structure maps directly to the type of artifact being generated:

```
gen/
├── gen.go                  # Orchestrator: calls each generator in sequence
├── format.go               # goimports formatting + file writing
├── funcmap.go              # Custom template function registry
├── naming.go               # Naming engine (identifier casing)
├── inflect.go              # English inflection, sqlgen-owned (PRD §8.5)
├── module.go               # Module path resolution (ReadModulePath, JoinModulePath)
├── context.go              # Context struct definitions
├── context_api.go          # GraphQL APIContext builder
├── context_cache.go        # Cache context builder
├── context_client.go       # Unified client context builder
├── context_connection.go   # Cursor-pagination connection builder
├── context_enum.go         # Enum context builder
├── context_event.go        # Event-hook context builder
├── context_set.go          # MySQL SET context builder
├── context_shared.go       # Shared helpers (column ordering, sort keys, etc.)
├── context_sorter.go       # Sorter context builder
├── context_table.go        # Table context builder + relationship FK metadata wiring
├── context_tenancy.go      # Tenancy column / context builder
├── context_type.go         # Composite/domain/extra type builders
├── context_view.go         # View context builder
├── templates/
│   ├── shared/             # Reusable fragments (invoked via {{ template }})
│   │   ├── _field.tmpl
│   │   ├── _filter.tmpl
│   │   ├── _field_options.tmpl
│   │   ├── _input.tmpl
│   │   ├── _imports.tmpl
│   │   ├── _lock_mode_guard.tmpl
│   │   └── _doc.tmpl
│   ├── table/              # Table-specific templates
│   │   ├── model.go.tmpl
│   │   ├── client.go.tmpl
│   │   ├── get.go.tmpl
│   │   ├── create.go.tmpl
│   │   ├── update.go.tmpl
│   │   ├── delete.go.tmpl
│   │   ├── upsert.go.tmpl
│   │   ├── exists.go.tmpl
│   │   ├── count.go.tmpl
│   │   ├── increment.go.tmpl
│   │   ├── pagination.go.tmpl
│   │   ├── stream.go.tmpl
│   │   └── relationships.go.tmpl
│   ├── view/               # View templates (read-only subset of table/)
│   │   ├── model.go.tmpl
│   │   ├── client.go.tmpl
│   │   ├── get.go.tmpl
│   │   ├── count.go.tmpl
│   │   └── pagination.go.tmpl
│   ├── api/                # GraphQL API generation
│   │   ├── schema.graphqls.tmpl       # Per-table .graphqls files
│   │   ├── shared.graphqls.tmpl       # Shared GraphQL types (PageInfo, etc.)
│   │   ├── scalars.go.tmpl            # Scalar Marshal/Unmarshal registry
│   │   ├── resolvers.go.tmpl          # Curated resolver method scaffolding
│   │   ├── seeds.go.tmpl              # Resolver body seeds (delegations to translators)
│   │   ├── middleware.go.tmpl         # Per-call options middleware
│   │   ├── errors.go.tmpl             # mapErrorToGQL + GraphQL error types
│   │   ├── field_options.go.tmpl      # GraphQL field-options translator
│   │   ├── filter_translate.go.tmpl   # GraphQL filter input → comparator
│   │   ├── comparator_translate.go.tmpl # Translator helpers shared by filter/sort
│   │   ├── sort_translate.go.tmpl     # GraphQL sort input → sql.Sort
│   │   ├── input_translate.go.tmpl    # Mutation input → omittable.Value[T]
│   │   └── connection_walker.go.tmpl  # Cursor-pagination Edge/PageInfo walker
│   ├── enum.go.tmpl
│   ├── types.go.tmpl
│   ├── shared_types.go.tmpl
│   ├── sorter.go.tmpl
│   ├── pagination.go.tmpl
│   ├── connection.go.tmpl
│   ├── error.go.tmpl
│   ├── tablename.go.tmpl
│   ├── set.go.tmpl                    # MySQL SET types
│   ├── cache.go.tmpl                  # Cache backend wiring
│   ├── event_hooks.go.tmpl            # Event hook chain registration
│   └── client.go.tmpl                 # Unified client aggregator
```

### Rules

- **Shared fragments** go in `shared/` with a `_` prefix (e.g., `_field.tmpl`). The prefix signals that the file defines reusable named templates, not a standalone output unit.
- **Table templates** go in `table/`. Each file generates one concern for one table.
- **View templates** go in `view/`. Views are read-only — only a subset of table templates applies.
- **API templates** go in `api/`. GraphQL generation: schema files (`*.graphqls`), scalars, resolver scaffolding, translators (filter / sort / input / comparator / connection-walker), middleware, error mapping. See §13 (GraphQL Codegen) for the surface.
- **Top-level templates** (`enum.tmpl`, `error.tmpl`, `client.tmpl`, `cache.tmpl`, `event_hooks.tmpl`, `set.tmpl`) generate artifacts that are not per-table.
- Never create nested subdirectories beyond `shared/`, `table/`, `view/`, and `api/`.

---

## 3. Template Naming

### Files

| Category | Pattern | Example |
|----------|---------|---------|
| Shared fragment | `shared/_<concern>.tmpl` | `shared/_field.tmpl` |
| Table template | `table/<operation>.go.tmpl` | `table/create.go.tmpl` |
| View template | `view/<operation>.go.tmpl` | `view/get.go.tmpl` |
| Top-level template | `<artifact>.go.tmpl` | `enum.go.tmpl` |

### Named Templates (inside files)

Named templates defined via `{{ define "name" }}` follow the pattern `<scope>/<concern>`:

```
{{- define "shared/field" -}}
  ...field rendering logic...
{{- end -}}
```

For templates scoped to a specific artifact type:

```
{{- define "table/model-struct" -}}
  ...struct definition...
{{- end -}}
```

Keep names short and descriptive. The name should tell you what the template *produces*, not how it works.

---

## 4. Writing Templates

### Keep Templates Readable

A template should read close to the shape of its output. When you squint at a template, you should see the Go code it produces:

```
// Good — output shape is visible
func (c *{{ .StructName }}Client) GetByID(ctx context.Context, id {{ .PKType }}) (*{{ .StructName }}, error) {
    query := {{ .Dialect | placeholder 1 }}
    row := c.db.QueryRow(ctx, query, id)
    ...
}

// Bad — logic obscures the output shape
{{ if .HasCompositeKey }}{{ range $i, $col := .PKColumns }}{{ if gt $i 0 }}, {{ end }}{{ $col.Name }} {{ goType $col }}{{ end }}{{ else }}id {{ .PKType }}{{ end }}
```

When a template section becomes hard to read, extract the decision into a funcmap function or pre-compute the value in the context.

### Use `{{ template }}` for Shared Patterns

Invoke shared fragments instead of duplicating code:

```
{{ template "shared/field" . }}
{{ template "shared/imports" .Imports }}
{{ template "shared/doc" .Description }}
```

Pass the minimum data the fragment needs, not the entire context.

### Conditional Generation

When a template section should only appear based on config (e.g., soft delete is enabled, an operation is configured):

```
{{- if .SoftDelete }}
func (c *{{ .StructName }}Client) SoftDelete(ctx context.Context, id {{ .PKType }}) error {
    ...
}
{{- end }}
```

Keep conditionals at the block level. Do not scatter `{{ if }}` checks throughout a method body — that logic belongs in a funcmap function or the context.

### Iteration

When iterating over columns, fields, or relationships, the data must arrive pre-sorted from the context:

```
{{- range .Columns }}
    {{ .Name }} {{ goType . }} {{ .Tags }}
{{- end }}
```

Never sort inside a template. Never iterate over a Go map in a template (map iteration order is non-deterministic). Always use pre-sorted slices.

### Reserved Word Safety

Templates that lower a column name into a **bare Go local variable** must compose `| safeGoIdent` after `| toCamelCase` at every emission site referencing the same local. A column named `type`, `range`, `err`, or any of the 73 entries in `goReservedIdents` (Go keywords + predeclared identifiers + generator-reserved locals `ctx`/`err`/`v`/`ok`) would otherwise produce uncompilable or shadowing code.

```gotemplate
// Correct — bare-local emission, escape applied
var {{ .FieldName | toCamelCase | safeGoIdent }} any = sql.Default
if v, ok := input.{{ .FieldName }}.Get(); ok {
    {{ .FieldName | toCamelCase | safeGoIdent }} = v
}

// Safe without safeGoIdent — suffix prevents collision
{{ .FieldName | toCamelCase }}DefaultSort
c.{{ .FieldName | toCamelCase }}Client
```

The property test in `cmd/sqlgen/gen/safety_property_test.go` renders `create.go.tmpl` with every reserved word as a column name and parses the output through `go/parser`. If a future template adds a new bare-local emission site without the pipe, at least one of the ~75 subtests fails. See PRD §8.5 (Reserved Word Handling) for the contract, the affected emission sites, and the integration fixture under `cmd/sqlgen/testdata/examples/sqlite/`.

---

## 5. Custom Template Functions (funcmap)

Custom functions are registered in `funcmap.go` and are the primary mechanism for keeping templates clean. All functions live in one file to maintain a single, auditable registry.

### Function Categories

| Category | Functions | Purpose |
|----------|-----------|---------|
| **Naming** | `toPascalCase`, `toCamelCase`, `structNamePlural`, `toSingular`, `safeGoIdent` | Name transformations; `structNamePlural` is the guaranteed-distinct plural of a struct name — `toPlural` and `toSnakeCase` are deliberately unregistered, see `funcmap.go`; `safeGoIdent` escapes Go reserved words at bare-local emission sites (see §4 Reserved Word Safety, PRD §8.5) |
| **Types** | `goType`, `isNullableType`, `comparatorType`, `zeroValue` | Type resolution and classification |
| **Soft Delete** | `softDeleteSwitch`, `softDeleteValue` | Soft delete SQL generation |
| **Relationships** | `fkStringExpr`, `fkNullGuard`, `hasConvert`, `isM2M`, `fkToStringByGoType`, `fkLoadGuard`, `fkLoadKey` | FK conversion and relationship loader emission |
| **Inputs** | `createInputFieldType`, `updateInputFieldType` | Input struct field classification |
| **Dialect** | `placeholder`, `quoteIdentifier`, `supportsReturning` | Dialect-aware SQL fragments |

#### Relationship FK Helpers — Critical Rules

The relationship loader funcmap helpers cooperate with **`RelationshipContext.FKGoType`** and **`RelationshipContext.FKColumnGoType`**, both populated by `wireRelationshipFKMetadata` (a post-processing pass over `[]TableContext` in `context_table.go`). Two rules to remember:

1. **For M2M target loaders, drive FK conversion off the *target's* PK Go type, not the parent's**. Use `fkToStringByGoType` with the target's `FKGoType`, not `fkToString` with the parent's PK field. Cross-PK-type M2M (e.g., `users(uuid.UUID) ↔ categories(int64)`) will compile-error otherwise — `t.ID.String()` is invalid against an `int64` PK.
2. **For nullable FKs whose Go type is a Null-wrapped struct** (e.g., `uuid.NullUUID`, `sql.NullString`), use `fkLoadGuard` and `fkLoadKey`. These honor `gotype.ScalarExtraction` to emit a guard (`if !r.UserID.Valid { continue }`) and an unwrap (`r.UserID.UUID.String()`) instead of calling `String()` directly on the wrapper struct, which would compile-error. The four `config.NullableVariant` YAML fields — `underlying_field`, `valid_field`, `valid_method`, `valid_invert` — let user-declared wrappers participate in the same lowering path.

### Rules for funcmap Functions

1. **Pure and deterministic.** Every function must return the same output for the same input. No side effects, no global state, no I/O.
2. **Named by what they return, not what they do.** `goType` returns the Go type string. `zeroValue` returns the zero value literal. `comparatorType` returns the comparator type name.
3. **Accept minimal input.** Pass the specific data the function needs (a column, a type), not the entire context.
4. **Return strings or simple values.** Template functions should return values that templates can use directly — strings, bools, ints. Do not return complex structs that require further processing in the template.
5. **Handle edge cases in Go, not templates.** If `goType` needs special handling for nullable UUID columns with a custom import, that logic belongs in the Go function, not in `{{ if }}` chains in the template.

### Adding a New Function

1. Add the function to `funcmap.go` with a clear name and doc comment.
2. Write unit tests for the function in `funcmap_test.go` — table-driven, covering edge cases.
3. Use it in templates. Verify the output via golden file tests.

Do not add functions speculatively. If a template does not need it yet, do not add it.

### Calling Functions in Templates

Use the pipe syntax for readability when chaining:

```
{{ .Column | goType }}
{{ .Column.Position | placeholder }}
{{ .Table.Name | toSingular | toCamelCase }}
```

Use call syntax when passing multiple arguments:

```
{{ quoteIdentifier .Dialect .Column.Name }}
```

---

## 6. Template Context

### Architecture

Templates do not access the schema model or config directly. Each generator builds a **typed Go struct** (the context) with all data pre-computed. Context struct definitions live in `context.go`; builders live in `context_*.go` files (one per entity type). The context is the only data the template sees.

### Context Types

| Context | Used By | Contains |
|---------|---------|----------|
| `TableContext` | `table/*.go.tmpl` | Struct name, columns (sorted), PK info, relationships, soft delete config, operations enabled, dialect |
| `ViewContext` | `view/*.go.tmpl` | Struct name, columns (sorted), PK info (from `@pk`), filter fields, scan shapes, dialect, driver, pagination config (page size, query limit, cursor keys), imports |
| `EnumContext` | `enum.go.tmpl` | Enum name, Go type, values (sorted), slice type name (PostgreSQL only) |
| `APIContext` | `api/*.tmpl` | Models import path, scalar registry, table API configs, multi-schema disambiguation prefix, error mapping table |
| `CacheContext` | `cache.go.tmpl` | Backend choice, key prefix / tags, TTL defaults, invalidation channels |
| `EventContext` | `event_hooks.go.tmpl` | Hook chain order, action set, event types per table |

### Relationship FK Metadata — Wiring Rule

`RelationshipContext.FKGoType`, `FKNullable`, and `FKColumnGoType` are populated by **`wireRelationshipFKMetadata`** in `context_table.go` — a post-processing pass that runs after individual table contexts are built. Production codegen always sets these. **Unit-test fixtures that hand-build `RelationshipContext` values must set them manually**, or relationship loader templates will emit incorrect FK conversions / guards. Each unset field fails differently: a missing `FKNullable` emits an unguarded null-FK loader, a missing `FKGoType` makes an M2M target loader call the wrong conversion, and a missing `FKColumnGoType` calls `String()` on a nullable wrapper such as `NullUUID`. When you add a new relationship-shape unit test, populate every `FK*` field on the relationship — don't rely on zero values.

### UUID Generation Expression — Wiring Rule

`TableContext.PKAutoGenExpr` is populated by **`attachUUIDGeneration`** in `gen/uuid_generation.go`, a post-build pass that runs once every context exists. It holds the call a create/upsert emits for a caller-omitted `PKStrategy: "app"` primary key, spelled for the UUID library the *package* resolved (PRD §7.4 "Generating UUID values") — which is why it cannot be computed while an individual table context is being built, and why it is a context field rather than a funcmap function.

**Unit-test fixtures that hand-build an app-strategy `TableContext` must set it**, or the create and upsert templates emit a bare `pkValue = ` and the output does not parse. A fixture built through `BuildTableContexts` alone has the same gap — that entry point is only half the production path; call `attachUUIDGeneration` after it, as `probeTableContext` does.

### Tenant Column Flag — Wiring Rule

`ColumnContext.Tenant` is populated by **`attachTenancyToTables` / `attachTenancyToViews`** in `gen/context_tenancy.go`, a post-build pass — whether a column is the tenant column is resolved by `BuildTenancyContext` against the whole schema (per-table opt-out, column override, type uniformity), not while one table context is being assembled. The same pass therefore re-runs `buildIncrementColumns`, because `isIncrementEligible` reads the flag and the first run saw every column untenanted.

**A fixture that hand-builds a tenanted `TableContext` must set it on the tenant column in both `Columns` and `PKColumns` (independent copies) and filter `IncrementColumns` accordingly**, or the increment template emits a `<T>Increment<Tenant>` constant and `Increment` becomes a cross-tenant write. A fixture built through `BuildTableContexts` alone has the same gap — use `BuildTableContextsFromSchema`, which runs the tenancy attach.

### Explicit Tenant Type — Wiring Rule

`TableContext.ExplicitTenantGoType` is populated by **`finalizeTenancyWiring`** in
`gen/context_tenancy.go`, a post-`wireNestedMutations` pass. It is the element type of
`CallOptions.Tenant` — the package's uniform tenant type (PRD §29.2.4) — and is drawn from *any*
tenanted entity, table **or view**, so it cannot be answered while one table context is being
assembled. It is distinct from `Tenancy.GoType`, which is that table's own tenant column type and is
empty on a shared table that needs no resolver.

The relationship loaders read it to forward `CallOptions.Tenant` to the targets they load through
(PRD §29.4.4). **A fixture that hand-builds a `TableContext` and renders `table/get` will
simply not emit the forwarding** — the parameter and both its uses are gated on the same field, so
the output stays valid, which is why this rule fails quietly rather than loudly. Build fixtures
through `BuildTableContextsFromSchema`, which runs the pass.

**The sibling facts are deliberately *not* fields.** `TableContext.NeedsTenantResolver()` and
`HasTenantedNestedEdge()` are methods, against rule 1 below, because they are read from three places
that must agree or the package does not compile — the emitted `tenantResolver` field, the emitted
`resolveTenant` / `tenantValue` helpers, and `applyClientTenancy`'s assignment list. As fields they
would be one more hand-wired fixture field of exactly the shape of the `FK*` and tenant-column
rules above; every input they read is a plain fact a fixture already sets. Prefer a method whenever a
derived fact reads only same-context fields and a fixture that forgets it would emit code that does
not compile.

### Rules

1. **Pre-compute everything.** If a template would need an `{{ if }}` to decide between two values, compute the right value in the context instead.
2. **Sort all slices.** Columns, relationships, enum values — everything the template iterates over must be sorted before being placed in the context. This is how deterministic output is guaranteed.
3. **Use concrete types.** Context fields should be specific types (`string`, `bool`, `[]ColumnCtx`), not `any` or `map[string]any`. This gives you compile-time safety and makes templates self-documenting.
4. **Keep contexts flat where possible.** Deeply nested context structs make templates harder to read. Prefer pre-computing a derived field over forcing the template to navigate `{{ .Table.PrimaryKey.Columns[0].GoType }}`.

### Adding Context Fields

1. Add the field to the context struct in `context.go`.
2. Populate it in the corresponding `context_*.go` builder file.
3. Use it in the template.
4. Update golden files to reflect the new output.

---

## 7. Whitespace Control

Go's `text/template` includes whitespace literally unless trimmed. Use `{{-` and `-}}` to control whitespace in generated output.

### Rules

Trim whitespace around **structural** template directives (conditionals, ranges, defines) that should not produce blank lines in the output:

```
{{- if .SoftDelete }}
func (c *{{ .StructName }}Client) SoftDelete(...) error {
    ...
}
{{- end }}
```

Do **not** trim whitespace around **content** that should have spacing (blank lines between methods, paragraph breaks in doc comments):

```
{{ template "shared/doc" .Description }}
type {{ .StructName }} struct {
{{- range .Columns }}
    {{ .Name }} {{ .GoType }} {{ .Tags }}
{{- end }}
}

func New{{ .StructName }}Client(db database.Querier) *{{ .StructName }}Client {
```

### Common Patterns

| Pattern | When |
|---------|------|
| `{{- if ... }}` | Conditional block that should not add a blank line when false |
| `{{- range ... }}` | List that should not add a leading blank line |
| `{{- end }}` | Closing a block that should not add a trailing blank line |
| `{{ if ... }}` (no trim) | Conditional block where a preceding blank line is intentional (e.g., spacing between methods) |

### Debugging Whitespace

When output has unexpected blank lines or missing spacing:

1. Look at the raw template output *before* `goimports` (the error message includes it on failure).
2. Add/remove trim markers one at a time.
3. Verify with golden file comparison.

`goimports` fixes some whitespace issues (extra blank lines between top-level declarations), but it does not fix whitespace inside function bodies. Get it right in the template.

---

## 8. Generated Code Quality

Generated code is what consumers interact with daily. It must meet the same bar as hand-written code.

### Requirements

- Passes `goimports` — correct imports, proper grouping (stdlib, third-party, internal).
- Passes `go vet` — no unreachable code, no unused variables, correct printf verbs.
- Passes `golangci-lint` — the project's linter config excludes `funlen`, `cyclop`, `gocritic`, and `revive` for `*_gen.go` files (generated methods can be long), but all other linters apply.
- Compiles cleanly — no type errors, no missing imports, no undefined references.

### Style

- **Idiomatic Go.** Generated code should look like a competent Go developer wrote it. No unnecessary parentheses, no redundant type assertions, no un-Go-like patterns.
- **Consistent struct tags.** Column fields use `` `db:"column_name" json:"column_name"` `` format. Relationship fields use `` `db:"-" json:"snake_case_name"` `` (`db:"-"` excludes them from scanning). Tag order is always `db` then `json`.
- **Doc comments on exported types and methods.** Every generated struct, method, and client gets a doc comment. Use the `shared/_doc.tmpl` fragment for type-level comments.
- **Field comments.** Struct field comments use the format `// FieldName - Description`. Descriptions are rendered verbatim from the source (SQL `COMMENT ON` or config override) — no lowercasing or transformation. The `shared/_field.tmpl` fragment handles this format.
- **Meaningful variable names.** `row`, `rows`, `query`, `args`, `tx` — standard Go database conventions. Not `r`, `q`, `a`.
- **No internal references in generated output.** Every comment and string a template emits lands in the user's code, where a reference to sqlgen's internals is noise the user cannot act on. Templates carry no `PRD §` or bare `§` citations, no design-doc names (`CACHE.md`, `docs/…`), no FIX IDs, no phase or sub-item numbers, and no design-decision labels (`D10`, `E2`). Write the comment so it explains the behavior directly. This covers template-only `{{/* */}}` comments too, and Go string literals that the generator writes into output (`Doc:` fields in gen contexts, manifest method notes, wrapper scaffolds). `make check` enforces it via `refs-check`.

### File Header

Every generated file starts with this header before the `package` declaration:

```go
// Code generated by sqlgen v1.2.0. DO NOT EDIT.
```

- The version is the **sqlgen CLI binary version**, not the config version field.
- This pattern is recognized by Go tooling (`go generate`, `golangci-lint`, IDEs) to skip generated files from analysis.
- The header is injected by the formatting pipeline, not by templates. Templates do not include the header.

### File Naming

All generated files use the `_gen.go` suffix:

| Layout | Pattern | Example |
|--------|---------|---------|
| `single_file` | `<configured_filename>` | `db_gen.go` |
| `file_per_table` | `<table>_gen.go` | `users_gen.go`, `orders_gen.go` |

### Stale File Cleanup

When using `file_per_table` layout, `sqlgen generate` deletes orphaned `*_gen.go` files in the output directory that no longer correspond to a table in the schema. Only files matching `*_gen.go` are considered for deletion. Files not ending in `_gen.go` are never touched.

GraphQL schema files are the exception that is reported rather than swept: a `*_gen.graphqls` in `schema_dir` that the run will not write fails generation before anything is written, naming the file and, when one exists, its gqlgen resolver file, and sqlgen deletes neither (PRD §23.8).

---

## 9. Formatting Pipeline

After template execution, every generated file passes through this pipeline in order:

1. **Header injection** — the `// Code generated ...` comment is prepended.
2. **`goimports`** (`golang.org/x/tools/imports`) — removes unused imports, deduplicates, and formats code. Templates **must** provide explicit imports via `{{ template "shared/imports" .Imports }}` using pre-computed import paths from the context (e.g., `ColumnContext.Import`). Do not rely on `goimports` to resolve import paths — it guesses based on the local module cache, which may resolve to the wrong version (e.g., unversioned `uuid` instead of `v5`) or fail entirely if the dependency is not yet in the consumer's `go.mod`.

   **Completeness is also what makes generation fast.** `imports.Process` resolves against assumed package names first and returns before it builds a resolver at all when the declared set already satisfies every reference in the file. A missing *standard-library* path is cheap — goimports fills it from a baked-in table — but a missing *non-stdlib* path sends it through a full filesystem scan of `GOMODCACHE`, per file, sharing nothing between calls. Two emitters leaning on that back-fill were 97% of all generation time. `TestGeneratedImportsAreDeclared` fails if any emitter regresses.

   **Over-declaring is free; under-declaring is not.** The `Format` pass prunes an import the file does not use, so an emitter may declare a superset — `modelTemplateImports` deliberately does, and the `file_per_table` layout seeds every file with the package-wide union. The exception is the files emitted with `FormatOnly` (the API/GraphQL translate, resolver and walker files), which get no pruning: those import sets must be **exact**, or the consumer's file will not compile.
3. **Write** — output to disk with `0o600` permissions. Output directories are created with `os.MkdirAll` if they do not exist.

### Error Handling

If `goimports` fails on the generated output, the raw template output is included in the error message. This is the primary debugging tool for template bugs — the error shows you what the template actually produced before formatting.

When debugging a `goimports` failure:

1. Read the raw output in the error message.
2. Look for syntax errors: missing closing braces, malformed type expressions, bad string literals.
3. Check if a funcmap function returned an unexpected value.
4. Fix the template or function, re-run, verify via golden files.

---

## 10. Deterministic Output

The same schema and config must produce byte-identical output on every run, on every platform. This is critical for golden file testing and for consumers who commit generated code to version control.

### Rules

1. **Sort all collections before generation.** Tables, columns, enum values, relationships — everything is sorted in `context.go` before templates see it. Sort order: alphabetical by name, with schema-qualified names sorted by schema first, then name.
2. **Never iterate over Go maps in templates.** Map iteration order is non-deterministic. Convert maps to sorted slices in the context.
3. **No timestamps or non-deterministic values in output.** The only variable content is the sqlgen version in the file header.
4. **Funcmap functions must be pure.** Same input, same output. No randomness, no clock reads, no external state.
5. **Template whitespace must be explicit.** Do not rely on `goimports` to clean up inconsistent whitespace — it handles some cases but not all.

### Verifying Determinism

Run generation twice on the same input. The output must be identical:

```bash
sqlgen generate
cp -r output/ output_first/
sqlgen generate
diff -r output/ output_first/
```

If `diff` reports any differences, there is a determinism bug. Golden file tests catch this automatically.

---

## 11. Golden File Testing

Golden files are the primary mechanism for verifying template output. They are committed to the repository and compared against fresh generation output in CI.

### Directory Structure

```
testdata/examples/
├── postgres/                # PostgreSQL: comprehensive E2E scenario
│   ├── sqlgen.yml
│   ├── schema.sql           # public + audit schemas (tests name disambiguation)
│   ├── views/               # View annotation files (parsed via input.views)
│   │   ├── product_summary.sql      # @pk, @type, @nullable
│   │   └── category_stats.sql       # No @pk (aggregate-only)
│   ├── expected/            # Committed golden files (expected generated output)
│   └── tests/               # Go tests split by feature area
│       ├── main_test.go             # TestMain, newClient, shared helpers
│       ├── crud_test.go             # Single-entity CRUD
│       ├── batch_test.go            # Batch/filter operations
│       ├── relationship_test.go     # O2O, O2M, M2M
│       ├── pagination_test.go       # Offset and cursor pagination
│       ├── soft_delete_test.go      # SoftDelete, Restore, *Where
│       ├── composite_pk_test.go     # Composite primary keys
│       ├── views_test.go            # Read-only view clients
│       └── type_overrides_test.go   # Table-level type overrides
├── mysql/                   # MySQL: same structure (views/, tests/)
├── sqlite/                  # SQLite: same structure (views/, tests/)
```

### How Golden File Tests Work

Each example test follows this sequence:

1. **Generate** — run sqlgen against the example's `sqlgen.yml`, `schema.sql`, and `views/` annotation files.
2. **Compare** — diff the generated output against committed golden files in `expected/`. Any difference fails the test.
3. **Compile** — verify the generated code compiles cleanly.
4. **Exercise** — the `tests/` directory contains Go tests that import the generated code and run real operations against a test database. `TestMain` applies both `schema.sql` and all view files from `views/`.

### Updating Golden Files

When a template change intentionally alters output:

```bash
make update-golden
```

This regenerates all golden files. The diff should be reviewed carefully in the PR — it shows the exact impact of your template change across all example scenarios. A small template edit that produces a large golden file diff is a signal to double-check.

### Per-Template Unit Tests

In addition to end-to-end golden file tests, each template should have focused unit tests:

```go
func TestCreateTemplate(t *testing.T) {
    ctx := buildMinimalTableContext(t, "users", []Column{...})
    got := executeTemplate(t, "table/create.tmpl", ctx)

    // Verify it compiles
    assertCompiles(t, got)

    // Spot-check key patterns
    assertContains(t, got, "func (c *UserClient) Create(")
    assertContains(t, got, "RETURNING")
}
```

These tests use a minimal context with representative schema data. They verify compilation and key output patterns without comparing the entire output byte-for-byte (that is the golden file test's job).

### When to Add a New Example

Add a new example under `testdata/examples/` when:
- A new feature introduces a distinct generation path (e.g., composite primary keys, views, type overrides).
- An edge case is not covered by existing examples (e.g., a table with no nullable columns).
- A bug was caused by a scenario that existing examples missed.

---

## 12. Adding a New Template

When adding a new generated artifact or operation:

### Checklist

1. **Identify the scope.** Is this per-table, per-view, or a top-level artifact? Place the file accordingly (`table/`, `view/`, or top-level).
2. **Define the context.** Add fields to the relevant context struct in `context.go`. Pre-compute all values. Sort all slices.
3. **Add funcmap functions if needed.** New functions go in `funcmap.go` with unit tests in `funcmap_test.go`. Keep functions pure and deterministic.
4. **Write the template.** Follow the patterns in this document. Use shared fragments. Keep logic minimal.
5. **Wire it into the orchestrator.** Add the template execution call in `gen.go` in the correct position in the generation sequence.
6. **Add golden file coverage.** Update existing examples or add a new one. Run `make update-golden` and review the diff.
7. **Add per-template unit tests.** Verify compilation and key output patterns.
8. **Verify the full pipeline.** Run `go test ./...` to ensure all golden files pass and the generated code compiles.

### Generation Sequence

New templates must be added in the correct position. The generation order is fixed:

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
12. API layer — GraphQL only (when `api.graphql.enabled` is true)

If your new template generates a type that other templates reference, it must come earlier in the sequence. If it references types from other templates, it must come later.

---

## 13. GraphQL Codegen

GraphQL API generation is opt-in via `api.graphql.enabled: true` and runs as a second pipeline stage **after** the standard codegen. Templates live in `gen/templates/api/`. The `sqlgen graphql` subcommand orchestrates the full flow; `cmd/sqlgen/wrapper/` is the integration layer with `gqlgen`.

### Pipeline

```
sqlgen graphql gen
    │
    ├─ standard codegen (gen/) → models, clients, filters, sorters, pagination
    │
    ├─ api/ codegen → schema (*.graphqls), scalars, resolver scaffolding,
    │                 translators (filter/sort/input/comparator/connection),
    │                 middleware, error mapping
    │
    ├─ wrapper/MergeInput → merges sqlgen-generated *.graphqls into the user's schema
    │
    └─ go run github.com/99designs/gqlgen → generates resolver glue + GraphQL server
```

### Curated Surface (PRD §26.5.1)

GraphQL exposes a **curated subset** of the generated client surface — not every method gets a resolver. The exclusion table (in PRD §26.5.1) covers methods with no GraphQL analog: `Stream*`, raw `Exec*`, low-level cursor primitives, and internal helpers. Per-table opt-in is via the `api.graphql.tables.<name>.enabled` config flag.

`_inc` / `_dec` paired-input mutations dispatch in a fixed order (increment → decrement) so resolver behavior is deterministic when both are present in a single mutation input.

### Multi-Schema Disambiguation

When two parsed schemas have a same-named table (e.g., `public.users` and `audit.users`), GraphQL types must not collide. The `gen.StructName` helper applies a **symmetric prefix** rule: if the same struct name would be produced in two schemas, both get prefixed (e.g., `PublicUser`, `AuditUser`). The prefix is symmetric — never one-sided — so consumers can rely on consistent naming in either schema.

### Scalar Registry

`templates/api/scalars.go.tmpl` produces `MarshalX` / `UnmarshalX` functions for every custom scalar declared in the type-mapping registry. The registry has four categories (1: primitive, 3: time, 4: opaque blob), each with a different marshaling shape. Overrides via `overrides.types.<name>` route through this registry — see the graphql E2E example for `uuid` / `numeric` / `json` / `timestamptz` overrides.

### Error Mapping

`templates/api/errors.go.tmpl` produces a `mapErrorToGQL` helper that translates the runtime error sentinels (`database.ErrNotFound`, `database.ErrConstraint`, etc., from `guidelines/ERRORS.md`) into GraphQL error extensions. Resolver bodies invoke this helper in their `defer` recovery path so all errors arrive at the GraphQL layer in a uniform shape. Add new mappings here when adding new sentinels — do not let raw driver errors leak through. Structured facts ride on the error rather than on the message: a nested-mutation failure carries `extensions.path: ["<Edge>"]` read off `*database.NestedMutationError.Edge` (PRD §26.5.5), attached in `mapErrorToGQL` itself after the code is chosen so every code carries it. Never parse such a fact back out of `err.Error()`.

### Per-Call Options Middleware

`templates/api/middleware.go.tmpl` produces a per-call options layer that lets resolvers attach query options (e.g., `WithCache`, `WithTenant`, `WithLockMode`) per request. The middleware is wired into the resolver scaffolding in `resolvers.go.tmpl`.

### Module Path Resolution

GraphQL codegen needs the consumer's module path to emit correct import paths in generated resolver files. `gen.ReadModulePath` reads the consumer's `go.mod`; `gen.JoinModulePath` joins it with relative paths. These are populated into `APIContext.ModelsImportPath` so templates emit fully-qualified imports.

### Wrapper / gqlgen Integration

`cmd/sqlgen/wrapper/` is the bridge to `gqlgen`. Three responsibilities:

- **`init.go`** — scaffolds `gqlgen.yml` and resolver stubs on first run
- **`merge.go`** — `MergeInput` merges sqlgen's `*.graphqls` outputs into the user's schema, preserving user-authored types
- **`seeds.go`** — seeds `gqlgen`-generated resolver bodies with delegations to sqlgen's translators (filter / sort / input → comparator / sql.Sort / omittable.Value[T])
- **`gen.go`** — invokes `go run github.com/99designs/gqlgen` after sqlgen codegen completes

### Adding a GraphQL Template

In addition to the standard rules in §12:

1. Add the template under `templates/api/`, not at the top level.
2. Wire its context field into `APIContext` (`gen/context_api.go`).
3. If it produces translator helpers consumed by gqlgen-generated resolvers, also update `wrapper/seeds.go` so the seeded resolver bodies match.
4. The graphql E2E example (`cmd/sqlgen/testdata/examples/graphql/`) is the integration test — exercise the new surface there and capture goldens with `make update-golden-e2e`.

---

## Appendix: Template Review Checklist

- [ ] Template reads close to the shape of its output
- [ ] Shared fragments used for common patterns (no duplication)
- [ ] Conditionals are block-level, not scattered through method bodies
- [ ] All iteration is over pre-sorted slices, never maps
- [ ] Whitespace is controlled with trim markers (`{{-` / `-}}`)
- [ ] New funcmap functions are pure, deterministic, and unit-tested
- [ ] Context fields are pre-computed, sorted, and use concrete types
- [ ] Generated output passes `goimports`, `go vet`, `golangci-lint`
- [ ] Generated output has the `// Code generated ... DO NOT EDIT.` header
- [ ] Golden files updated and diff reviewed
- [ ] Per-template unit tests verify compilation and key patterns
- [ ] Template is wired into `gen.go` at the correct position in the sequence
