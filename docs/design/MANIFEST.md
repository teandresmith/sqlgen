# SQLGen Manifest — Design & Plan

> **Status: SYNCED — superseded by PRD §30 (2026-05-15).**
>
> The §9 sync plan has been executed. The authoritative manifest spec now
> lives in `docs/PRD.md`:
>
> - §30.1 Overview
> - §30.2 Configuration (full `ManifestConfig` block + validation rules + per-table override + resolution order)
> - §30.3 Generated Artifacts (file layout for both JSON layouts + stale cleanup link to §23.8 + separate graph-package variant)
> - §30.4 JSON Schema (top-level fields + conventions block + entity shape)
> - §30.5 Versioning Policy (independent semver on `schema_version`)
> - §30.6 Runtime-Embedded Manifest (new `manifest/` runtime package, three Client methods)
> - §30.7 Extended Schema Metadata (`COMMENT ON TABLE` / `COMMENT ON COLUMN` capture, full `indexes[]`, CHECK constraints, `default_kind`, `features.events`, expanded `features.cache`)
> - §30.8 CLI Tooling (`sqlgen manifest validate` + `sqlgen manifest diff`)
>
> Cross-reference rows added: PRD §4.6 (Manifest subsection in
> GenerationConfig), §4.13 (8 new validation rules), §8.3 (Manifest row in
> Generated Artifacts table), §23.1 (the two new CLI commands).
>
> The implementation plan moved to `docs/tracker/IMPLEMENTATION_ORDER.md` Phase 18
> (sub-items 18.1–18.10). `/phase 18` is unblocked.
>
> This document remains as a working-design supplement for **rationale,
> open-question history (§11 Resolved — 18 resolutions), builder pseudocode,
> testing notes, and Phase 18 sub-item previews (§10)**. Same pattern
> `docs/design/CACHE.md` (PRD §27) and `docs/design/TENANCY.md` (PRD §29) follow — the
> design supplement is retained after the PRD lands as the canonical spec.
>
> **Pre-sync framing preserved below for context.** The sections that follow
> were authored as a pre-sync working design; sections marked `[SYNC]` in
> earlier turns were merged into PRD §30 once the design stabilized.

---

## 1. Motivation

100+ table sqlgen consumers regularly produce 500k+ lines of generated Go
across `models_gen.go`, per-entity client files, and the optional GraphQL
surface. AI agents asked to operate against this code (write a new query,
audit relationships, propose a schema change) currently have two bad
options:

1. **Pull `models_gen.go` and one or more `<table>_gen.go` files into
   context.** A single read can exceed 50k tokens on a 100-table package,
   and the majority of those tokens are method bodies the agent does not
   need to answer "what's available on `users`?".
2. **Grep blindly for symbols.** Fast, but produces no shape — the agent
   learns `GetMany` exists without learning what it returns, what
   errors it can produce, or what filter shape it accepts.

The manifest fills this gap with **a small, structured, deterministic
description of every generated surface, written for both agents and
humans.** It is purely additive: no generated Go changes, no runtime
behavior changes, no consumer-facing API. It ships only when the consumer
opts in via `generation.manifest`.

The unified client (PRD §20), the per-table operations matrix (PRD §4.6),
the relationship loaders (PRD §13), the cache config (PRD §27), and the
tenancy config (PRD §29) already encode every fact the manifest emits. The
manifest is a **rendering** of those facts, not a new source of truth — so
its shape can evolve without touching codegen.

### 1.1 Why both JSON and Markdown

- **JSON** is the contract for programmatic consumers: tools answering
  "which tables FK to `users`?" deterministically, schema-validation in CI,
  cross-package linking. Versioned with its own semver so consumers can
  pin.
- **Markdown** is the contract for natural-language consumers: agents and
  humans skimming a directory to learn what's available. Deliberately
  informal — no parser depends on its shape.

Shipping both lets each artifact stay sharp at its job. Shipping only one
forces compromise: pure JSON is unreadable to humans / LLMs in
conversational mode; pure Markdown drifts into a quasi-DSL the moment any
tool tries to query it.

---

## 2. Scope

**In scope (Phase 18):**

- New `generation.manifest` config block — opt-in, off by default.
- Generated artifacts in the output package's `manifest/` subdirectory:
  - `manifest_gen.json` — structured, versioned, programmatically queryable.
  - `manifest/_index.md` — top-of-package entity scan table.
  - `manifest/_conventions.md` — error sentinels, pagination, comparators,
    soft-delete semantics, `CallOptions` usage. Factored out once so
    entity files don't restate.
  - `manifest/<entity>.md` — one per generated entity (table or view).
- Generator pipeline integration: a new template stage running after the
  per-table client + API stages so the manifest reflects the *landed* code
  shape, not the planned one.
- A JSON Schema definition for `manifest_gen.json` checked into the repo
  (`cmd/sqlgen/manifest/schema/v1.json`) so consumers can validate.
- Coverage in the `testdata/examples/` matrix for every dialect (postgres,
  mysql, sqlite, graphql) gated behind the opt-in.
- Versioning policy: independent semver on the manifest schema, decoupled
  from sqlgen's own version.
- **Agent breadcrumb files** at the output package root: `CLAUDE.md` and
  `AGENTS.md`, both auto-discovered by modern agent tools. Claude Code
  loads `<output.dir>/CLAUDE.md` on-demand when reading any file in the
  package, additive to (not shadowing) any root-level `CLAUDE.md`. Both
  files carry identical pointer-only content — they are discovery
  aliases, not different documents.
- **Package-level Go doc comment.** The existing per-file generated
  header on `models_gen.go` (or the equivalent top-level file) is
  extended with a one-line manifest pointer so agents that grep or jump
  to the package entry point also see the breadcrumb.
- **Runtime-embedded manifest.** The generated `Client` exposes
  `Manifest() *manifest.Document`, `ManifestEntity(table) (*manifest.Entity, error)`,
  and `ManifestVersion() string` for runtime introspection. Types live
  in a new runtime package `manifest/` (peer to `database/`, `cache/`,
  etc., stdlib-only). JSON content is `//go:embed`-ed at build time;
  parsed at package init. Layout-agnostic: `ManifestEntity` works the
  same in single and per-entity disk layouts. Opt-out via
  `manifest.embed_in_client: false`. See §4.8 (mechanics) and §5.9
  (Go types).
- **Extended schema metadata capture.** Each entity carries
  `comment` (table-level `COMMENT ON TABLE`) and `indexes[]` (all
  indexes including non-unique and partial). Each column carries
  `comment` (`COMMENT ON COLUMN`), `check` (CHECK constraint
  expression), and `default_kind` (`literal` / `function` /
  `expression`) alongside the existing `default`. Replaces the
  "agent has to guess domain semantics" gap with explicit
  schema-derived signals.
- **Feature-level metadata.** When `events.enabled` is true for an
  entity, `features.events.types[]` lists the emitted event names +
  the shared payload shape. When cache is configured,
  `features.cache.key_pattern` + `invalidates_on[]` make cache
  invalidation scope legible. Both fields are derived from existing
  sqlgen config; no new config surface.
- **CLI tooling.** Two new subcommands:
  `sqlgen manifest validate <path>` (runs JSON Schema validation;
  CI-friendly exit codes); `sqlgen manifest diff <old> <new>`
  (structured schema-evolution diff for PR review). Both operate on
  the on-disk JSON and require nothing from the runtime.

**Out of scope (Phase 18, deferred):**

- Bidirectional ingestion: reading a manifest back to drive a new
  generation or to reconcile schema drift. Manifest is **read-only
  output**.
- CLI subcommand for ad-hoc queries against the manifest
  (`sqlgen manifest list-entities`). Deferred until consumer pull
  justifies. The JSON makes ad-hoc queries possible via `jq` today.
- Per-method usage examples for every operation. Phase 18 emits one
  canonical read + one canonical write example per entity; broader
  example generation deferred.
- Live updating on schema drift (separate watch-mode concern; PRD §23.10).
- Embedding the manifest in `godoc` comments on generated types. Deferred —
  the markdown directory serves that audience differently.
- **MCP server (`sqlgen mcp serve`).** Phase 19 committed follow-on — see
  `docs/design/MCP.md`. Exposes manifest queries as MCP tools (`list_entities`,
  `get_entity`, `find_referencing`, etc.) so agents reach for tool calls
  instead of file reads. The structural fix for the wrapper-package case
  where breadcrumbs never trigger (agent works at the wrapper layer and
  never reads files inside `<output.dir>/`). Adds runtime infrastructure
  but is decoupled from filesystem location, which is the whole point.
  Phase 18 freezes the JSON manifest contract; Phase 19 consumes it.
- **`sqlgen lint` check** that warns when `manifest.enabled: true` but no
  agent-instruction file references the manifest. Optional follow-on;
  not Phase 18.

**Non-goals:**

- Replacing the PRD as the authoritative spec. The manifest is per-package
  and per-schema; the PRD is the language-level contract.
- Replacing `godoc` or `pkg.go.dev` rendering. Those serve a different
  audience (Go IDE users) with a different shape.
- Producing OpenAPI / JSON Schema for runtime input validation. That's a
  REST-generation concern (D.1), not a manifest concern.
- Drift detection between manifest and source. The manifest is regenerated
  every run; drift is structurally impossible.

---

## 3. Configuration

### 3.1 Proposed YAML

```yaml
generation:
  manifest:
    enabled: true                       # default: false
    formats: [json, markdown]           # default: [json, markdown] when enabled
    json_filename: manifest_gen.json    # default (used when json_layout: single)
    json_layout: single                 # single | per_entity (default: single)
    json_per_entity_dir: entities       # default (used when json_layout: per_entity; relative to markdown_dir)
    markdown_dir: manifest              # default (relative to output.dir)
    include_examples: true              # default: true (canonical example per entity)
    include_internal: false             # default: false (skip exclude_columns, system views)
    breadcrumbs:                        # each default true when manifest.enabled
      claude_md:   true                 # emit <output.dir>/CLAUDE.md
      agents_md:   true                 # emit <output.dir>/AGENTS.md
      package_doc: true                 # add manifest pointer to models_gen.go doc comment
    embed_in_client: true               # default true when manifest.enabled — generate Client.Manifest() / ManifestEntity() / ManifestVersion()
```

Per-table override under `tables.<name>.manifest`. Only `enabled` is
overrideable at the table level — formats / filenames / directory are
package-level:

```yaml
tables:
  audit_logs:
    manifest:
      enabled: false   # exclude this entity from the manifest entirely
```

### 3.2 Config struct (Go)

```go
// cmd/sqlgen/config/types.go

type ManifestConfig struct {
    Enabled          bool              `yaml:"enabled"`
    Formats          []ManifestFormat  `yaml:"formats,omitempty"`
    JSONFilename     string            `yaml:"json_filename,omitempty"`
    JSONLayout       JSONLayout        `yaml:"json_layout,omitempty"`
    JSONPerEntityDir string            `yaml:"json_per_entity_dir,omitempty"`
    MarkdownDir      string            `yaml:"markdown_dir,omitempty"`
    IncludeExamples  *bool             `yaml:"include_examples,omitempty"`
    IncludeInternal  *bool             `yaml:"include_internal,omitempty"`
    Breadcrumbs      BreadcrumbsConfig `yaml:"breadcrumbs,omitempty"`
    EmbedInClient    *bool             `yaml:"embed_in_client,omitempty"`
}

type JSONLayout string

const (
    JSONLayoutSingle    JSONLayout = "single"     // default — one manifest_gen.json carrying all entities inline
    JSONLayoutPerEntity JSONLayout = "per_entity" // top-level manifest_gen.json + one file per entity under json_per_entity_dir
)

type BreadcrumbsConfig struct {
    ClaudeMD   *bool `yaml:"claude_md,omitempty"`    // default: true when manifest.enabled
    AgentsMD   *bool `yaml:"agents_md,omitempty"`    // default: true when manifest.enabled
    PackageDoc *bool `yaml:"package_doc,omitempty"`  // default: true when manifest.enabled
}

type ManifestFormat string

const (
    ManifestFormatJSON     ManifestFormat = "json"
    ManifestFormatMarkdown ManifestFormat = "markdown"
)

type TableManifestConfig struct {
    Enabled *bool `yaml:"enabled,omitempty"`
}
```

### 3.3 Resolution order

Parallel to PRD §4.6's general per-table override rule:

1. Table-level `tables.<name>.manifest.enabled` if set.
2. Global `generation.manifest.enabled`.
3. Default: `false`.

Pointer types (`*bool`) for `IncludeExamples` / `IncludeInternal` so unset
distinguishes from explicit false — matches the existing PRD §4.6
convention for nullable booleans.

### 3.4 Validation rules

These extend PRD §4.13:

- `formats: []` when `enabled: true` → validation error.
- Unknown `formats` value → validation error (set is closed at JSON / Markdown
  for v1).
- `markdown_dir` must not equal `.` or an existing PRD §8.3 artifact subdir
  (`graph/`) — validation error to prevent file-system collisions.
- `tables.<name>.manifest.enabled: true` with global
  `generation.manifest.enabled: false` → validation error. Cannot opt a
  single entity in without the package-level opt-in; the per-table flag
  is opt-*out*.
- `markdown_dir` cannot start with `_` (reserved prefix for index +
  conventions files inside the directory).
- Any `breadcrumbs.*` field set to `true` when `manifest.enabled: false`
  → validation error. Breadcrumbs only make sense when there is a
  manifest to point at.
- `json_layout` must be `single` or `per_entity` (closed set in v1).
- `json_per_entity_dir` must not equal `markdown_dir` or `.` —
  validation error to prevent filesystem collisions with the markdown
  files.
- `json_per_entity_dir` cannot start with `_` (reserved for index +
  conventions files, mirroring the `markdown_dir` rule).
- `embed_in_client: true` when `manifest.enabled: false` → validation
  error. The runtime methods only make sense when a manifest is being
  emitted to embed.

---

## 4. Generated artifacts

### 4.1 Directory layout

**Single JSON layout (default — `json_layout: single`):**

```
<output.dir>/                       # e.g., models/
├── models_gen.go
├── shared_types_gen.go
├── users_gen.go
├── posts_gen.go
├── ...
└── manifest/                       # NEW (gated by generation.manifest.enabled)
    ├── manifest_gen.json           # all entities inline
    ├── _index.md
    ├── _conventions.md
    ├── users.md
    ├── posts.md
    ├── ...
    └── workspace_settings.md
```

**Per-entity JSON layout (`json_layout: per_entity`):**

```
<output.dir>/
└── manifest/
    ├── manifest_gen.json           # top-level metadata + conventions + entity index (pointers, not inline)
    ├── entities/                   # default json_per_entity_dir (relative to markdown_dir)
    │   ├── users.json              # full entity JSON per table / view
    │   ├── posts.json
    │   ├── ...
    │   └── workspace_settings.json
    ├── _index.md
    ├── _conventions.md
    ├── users.md
    └── ...
```

Markdown emission is identical across both layouts — only the JSON
emission shape changes. Filenames in both layouts follow §4.3 (SQL
table name verbatim).

**When to use which.**

- **Single** is right for most packages. One file, easy to commit, easy
  to diff, ~30k tokens for 100 entities (well within agent context
  budgets when fetched via the MCP `sqlgen://manifest` resource).
- **Per-entity** is right when (a) the package is large enough that the
  single file approaches the agent client's resource-size warning
  threshold (§10 in MCP.md flags 256KB), or (b) consumers want to
  fetch one entity's JSON deterministically without parsing through a
  larger file. The MCP server's `sqlgen_get_entity` tool resolves
  either layout transparently — agents see the same response shape.

### 4.2 Why a subdirectory, not flat files at the package root

- Putting the manifest at the package root (`models/manifest_gen.json`) would
  pollute `go doc` output and risk colliding with consumer-authored files.
- A subdirectory keeps the manifest co-located with the code it describes
  while isolating it from Go tooling. `.json` and `.md` files in a Go
  package are ignored by `go build` regardless of location, but the
  subdirectory makes the boundary explicit.
- The subdirectory name is configurable (`markdown_dir`) so consumers can
  rename if `manifest/` conflicts with an existing pattern. Default
  `manifest/` matches the design supplement convention used by `docs/`.

### 4.3 Per-entity file naming

One file per entity (entity = table or view). **Filename matches the
SQL table name verbatim** — no singularization, no casing transformation.
Specifically, the manifest filename reuses the same prefix sqlgen
already computes for the `<prefix>_gen.go` Go file. Pure reuse, zero
new naming rules.

**Common case (single-schema):**

| Table / View | Go file | Markdown filename |
|---|---|---|
| `users` | `users_gen.go` | `manifest/users.md` |
| `workspace_settings` | `workspace_settings_gen.go` | `manifest/workspace_settings.md` |
| `post_tags` | `post_tags_gen.go` | `manifest/post_tags.md` |
| view `active_users_view` | `active_users_view_gen.go` | `manifest/active_users_view.md` |

**Multi-schema (PRD §5.5 prefixing kicks in):**

sqlgen prefixes the schema name into Go file names when the parsed
schema model contains tables from multiple schemas. The manifest
inherits the same prefix automatically:

| Table | Go file | Markdown filename |
|---|---|---|
| `public.users`, the only `users` | `user_gen.go` | `manifest/user.md` |
| `public.users` + `billing.users` | `public_user_gen.go` | `manifest/public_user.md` |
| `billing.users` (same pair) | `billing_user_gen.go` | `manifest/billing_user.md` |
| `audit.audit_users`, the only `audit_users` | `audit_user_gen.go` | `manifest/audit_user.md` |

Two things this table is easy to get wrong, and both were wrong in the
builder until FIX-163. The prefix is **singular** — it is the same
`TableContext.SnakeName` the Go file is named from, so `users` gives
`user`, not `users`. And the schema is prefixed only when the **bare
name collides across schemas**, not whenever a schema is present: a lone
`audit.audit_users` is `audit_user`, never `audit_audit_user`.

The filename is always identical to the `_gen.go` prefix, just with
`.md` substituted — the emitter reads that field rather than recomputing
it, which is what keeps the two from drifting. No new disambiguation
rules — manifest naming and
sqlgen's existing file naming use the same prefix function.

**Why this works:**

- **Pure reuse of sqlgen's existing prefix computation.** Zero new
  rules. If sqlgen's file-naming logic ever changes (new collision
  resolution, casing tweaks), the manifest tracks it automatically.
- **Singularization / casing edge cases don't apply.** None of those
  pipelines affect the `_gen.go` filename, so none affect the manifest
  filename — irregular plurals (`data`/`datum`), non-English schemas,
  consumer `naming.singularize: false` overrides all bypassed by design.
- **Schema-first mental model holds.** Filename mirrors the SQL
  identifier. Agents reading `schema.sql` see `CREATE TABLE users` and
  navigate to `manifest/users.md` without translating in their head.
- **Index consistency.** `_index.md` (§6.1) lists the SQL table name in
  its "Table" column; the markdown link `[users](users.md)` matches on
  both sides.

### 4.4 Stale file cleanup

The generator emits the manifest under PRD §23.8's stale-file cleanup
contract: removing a table from the schema → its `manifest/<entity>.md`
is deleted on next generation. The JSON file is rewritten in full each
run; no merging, so stale entries are structurally impossible there.

Disabling the manifest after a prior enabled run removes the whole
directory — but only once the generator has **proved the directory is
its own**, by finding either a `_index.md` carrying the generator line or
a `manifest_gen.json` whose `generator.name` is `sqlgen`. The disabled
branch runs on every generation (the manifest is off by default) and
`<output.dir>/manifest/` routinely resolves onto a directory the consumer
owns, so the check fails closed: no anchor, no removal. See PRD §30.3
*Directory ownership* for the full rule.

### 4.5 Idempotency / determinism

Manifest emission must be deterministic byte-for-byte across runs given an
unchanged schema + config. This is required by the existing PRD §5.7
determinism contract and gates the e2e golden-file pattern (§8 below).
Implementation rules:

- All collections sorted lexicographically by stable key (entity name,
  column name, method name).
- `generated_at` is the **only** field allowed to vary across runs.
  Generator MUST accept a `--manifest-timestamp` flag for golden tests
  (sets a fixed value); without it, uses `time.Now().UTC().Format(time.RFC3339)`.
- JSON written via `json.MarshalIndent(..., "", "  ")` (2-space indent, no
  HTML escaping → use a custom encoder, since `json.Marshal` html-escapes
  `&`/`<`/`>` by default and we want them literal in SQL expressions).
- Markdown written via `text/template` with deterministic iteration over
  pre-sorted slices (no `range` over maps).

### 4.6 Breadcrumb files

Two short files emitted at the output package root, alongside the
generated `*_gen.go` files:

```
<output.dir>/
├── CLAUDE.md       # NEW (gated by manifest.breadcrumbs.claude_md)
├── AGENTS.md       # NEW (gated by manifest.breadcrumbs.agents_md)
├── models_gen.go
├── ...
└── manifest/
```

**Why both files.** Claude Code auto-discovers `CLAUDE.md` only (not
`AGENTS.md`); Cursor / Windsurf / emerging tools auto-discover
`AGENTS.md`. Emitting both costs nothing and covers the full agent
ecosystem without platform-specific symlinks. Both files carry
**identical content** — they are discovery aliases, not different
documents.

**Discovery semantics.** For Claude Code specifically,
`<output.dir>/CLAUDE.md` loads **on-demand** the moment the agent reads
any file in the package. It is **additive** to any root-level
`CLAUDE.md` — neither shadows the other. This makes the breadcrumb a
guaranteed trigger at the precise moment it is useful (agent enters the
generated package), not a passive hope.

**Content (pointer-only, not auto-import).** Auto-importing
`manifest/_index.md` via `@manifest/_index.md` would inflate context
every time the agent touches the package, which defeats the on-demand
manifest design. The breadcrumb says *how to find what you need*, not
*here's everything you might need*.

````markdown
<!-- Code generated by sqlgen. DO NOT EDIT. -->

# Generated package — read the manifest first

This package is generated by sqlgen. Before reading any `*_gen.go` file in
this directory, load `manifest/_index.md` for the entity scan table and
`manifest/_conventions.md` for error sentinels, pagination, comparator
semantics, and `CallOptions` usage.

For programmatic queries against the schema, use
`manifest/manifest_gen.json` (JSON Schema v1).

Per-entity detail lives at `manifest/<entity>.md` (one file per table or
view).
````

**Override-friendliness.** Consumers with a root-level `CLAUDE.md` or
`AGENTS.md` carrying project-wide instructions are unaffected — both
load additively. Consumers who explicitly do not want generated agent
files can disable via `manifest.breadcrumbs.claude_md: false` and/or
`manifest.breadcrumbs.agents_md: false` (Phase 18 honors the opt-out;
no breadcrumb is emitted in that case, and any pre-existing generated
one is removed by stale-file cleanup).

**Separate graph-package variant.** When GraphQL is enabled, sqlgen
emits two packages (`models/` for entities + clients, plus a graph
package for gqlgen resolvers + walker at the resolved
`api.graphql.resolver_dir` — the nested default `<output.dir>/graph`, or
a top-level sibling such as `graph` when
`generation_config.graph_top_level` is `true`). Only the **models**
package gets a manifest directory. The graph package gets its own
breadcrumb pair, but the content **points at three sources instead of
one**:

````markdown
<!-- graph/CLAUDE.md (identical content in graph/AGENTS.md) -->
<!-- Code generated by sqlgen. DO NOT EDIT. -->

# Generated GraphQL package

This package is generated by sqlgen. For:

- **GraphQL schema, types, queries, inputs:** see the `*_gen.graphqls`
  files in this directory. The schema is self-describing for any
  GraphQL-aware tool or agent.
- **Entity / column / method details (Go side):** see
  `<relative-path>/_index.md` and per-entity files in the linked
  manifest directory.
- **Resolver scaffold pattern, error code mapping, middleware
  semantics:** see sqlgen docs (PRD §26).
````

The `<relative-path>` is computed at generation time from the two
packages' resolved paths — `../manifest` for the nested default
`<output.dir>/graph`, or a path that walks back out of the sibling tree
(e.g. `../models/manifest`) when the graph package is top-level. No
separate JSON manifest is emitted in the graph package — the `.graphqls`
files already serve as the schema reference, and the cross-cutting
concerns are project-invariant sqlgen knowledge in static docs.

The models-package breadcrumb (main template above) is unchanged when
the graph package is emitted separately. It still describes the entity
surface as before.

### 4.7 Package-level Go doc comment

When `manifest.breadcrumbs.package_doc: true`, the existing per-file
generated header on `models_gen.go` (the top-level file already carrying
a `// Package <name>` doc comment) is extended with a one-line manifest
pointer:

```go
// Code generated by sqlgen. DO NOT EDIT.
//
// Package models. AI agents: read manifest/_index.md before consuming
// this package. See manifest/manifest_gen.json (schema v1) for the
// structured surface description.
package models
```

This catches the case where the agent skips the breadcrumb files and
goes straight to the Go source (grep-driven exploration, IDE
jump-to-definition). The doc comment is the lowest-cost backup signal:
zero new files, no Go-tooling impact (just a comment), and it travels
with the package across consumers. Per Go's `godoc` folding, the doc
comment is visible on `go doc <pkg>` regardless of which `*_gen.go` file
the reader opens.

### 4.8 Runtime-embedded manifest

When `manifest.embed_in_client: true` (default when manifest is
enabled), the generated `Client` gains three methods for runtime
introspection of the manifest:

```go
// Manifest returns the parsed manifest document. Pre-parsed at package
// init; safe for concurrent reads. Entities[] is always the lightweight
// index (Name + Table + Kind + optional File); use ManifestEntity for
// full entity records — the lookup is layout-agnostic.
func (c *Client) Manifest() *manifest.Document

// ManifestEntity returns the full record for a given table name.
// Works identically across `single` and `per_entity` disk layouts.
// Returns (nil, manifest.ErrEntityNotFound) for unknown tables.
func (c *Client) ManifestEntity(table string) (*manifest.Entity, error)

// ManifestVersion returns the schema_version field, pre-extracted at
// init for cheap drift detection without parsing the full document.
func (c *Client) ManifestVersion() string
```

Types live in the new runtime package `github.com/teandresmith/sqlgen/manifest`
(stdlib-only, peer to `database/`, `cache/`, `comparator/`). Same
types are both the JSON unmarshal target and the public API on the
Client methods — consumers see typed structs, no `[]byte` to parse
themselves. Full type sketch in §5.9.

**Embed mechanics.** Generated file `manifest_embed_gen.go` in the
output package:

```go
import (
    _ "embed"
    "github.com/teandresmith/sqlgen/manifest"
)

//go:embed manifest/manifest_gen.json
var manifestRaw []byte

//go:embed all:manifest/entities
var manifestEntitiesFS embed.FS  // empty in single layout; populated in per_entity

var (
    manifestParsed   *manifest.Document
    manifestEntities = map[string]*manifest.Entity{}
    manifestVersion  string
)

func init() {
    if err := manifest.LoadInto(&manifestParsed, manifestRaw, manifestEntitiesFS, manifestEntities); err != nil {
        panic(fmt.Errorf("sqlgen manifest embed corrupt: %w", err))
    }
    manifestVersion = manifestParsed.SchemaVersion
}
```

`manifest.LoadInto` is the helper exported from the runtime package
that handles both layouts uniformly: parses the top-level into
`*Document`, then either extracts entities from `Document.Entities[]`
(single layout) or walks the `embed.FS` and parses each entity file
(per-entity layout). Either path populates the `manifestEntities` map.

**Init-time parse cost.** One-time at process startup. ~10ms for
100-entity manifests; immeasurable for smaller ones. Documented as a
known cost; consumers with cold-start sensitivity use
`embed_in_client: false`.

**Layout-agnostic `ManifestEntity`.** Disk layout is invisible to
runtime consumers: `client.ManifestEntity("users")` returns the full
record regardless. The single-vs-per-entity choice is purely a
disk-shape / agent-consumption optimization; it does not leak into the
Go API.

**Binary-size cost.** Raw embedded JSON ~30KB for 100 entities; parsed
struct overhead in memory ~200–500KB. Negligible for typical Go
services. The `embed_in_client: false` opt-out targets binary-size-
sensitive consumers (embedded systems, lambda cold starts, etc.).

**Marshaling back to bytes.** Consumers needing JSON for an HTTP
endpoint or telemetry simply do `json.Marshal(client.Manifest())`. The
output is content-equivalent to the on-disk file (key order may differ;
content identical). No `ManifestRaw() []byte` helper in v1 — add later
if a consumer surfaces a real need for byte-identical output.

---

## 5. The `manifest_gen.json` schema

### 5.1 Top-level shape

**Single layout (default — `json_layout: single`):**

```json
{
  "$schema": "https://sqlgen.dev/manifest/v1.json",
  "schema_version": "1.0.0",
  "generated_at": "2026-05-15T14:22:11Z",
  "generator": { "name": "sqlgen", "version": "0.42.0" },
  "dialect": "postgres",
  "package": "models",
  "conventions": { ... },
  "generation_config": { ... },
  "layout": "single",
  "entities":     [ ... ],   // full entity objects inline
  "enums":        [ ... ],
  "extras":       [ ... ]
}
```

**Per-entity layout (`json_layout: per_entity`):**

Top-level `manifest_gen.json` becomes a lightweight index — same
envelope and conventions, but `entities[]` carries `{name, table, kind,
file}` pointers instead of inline objects:

```json
{
  "$schema": "https://sqlgen.dev/manifest/v1.json",
  "schema_version": "1.0.0",
  "generated_at": "2026-05-15T14:22:11Z",
  "generator": { "name": "sqlgen", "version": "0.42.0" },
  "dialect": "postgres",
  "package": "models",
  "conventions": { ... },
  "generation_config": { ... },
  "layout": "per_entity",
  "entities": [
    { "name": "User", "table": "users",              "kind": "table", "file": "entities/users.json" },
    { "name": "Post", "table": "posts",              "kind": "table", "file": "entities/posts.json" },
    { "name": "WorkspaceSetting", "table": "workspace_settings", "kind": "table", "file": "entities/workspace_settings.json" }
  ],
  "enums":  [ ... ],   // always inline (small)
  "extras": [ ... ]    // always inline (small)
}
```

Each `entities/<table>.json` contains the full per-entity object per
§5.3 — same shape as the inline form, just hoisted out. `enums[]` and
`extras[]` are **always inline** regardless of layout (typically
10–50 total, not load-bearing for split economics).

Top-level fields:

| Field | Type | Notes |
|---|---|---|
| `$schema` | string | URL of the JSON Schema for this manifest's `schema_version`. |
| `schema_version` | semver string | Independent of sqlgen's version. See §5.5. |
| `generated_at` | RFC3339 timestamp | Only non-deterministic field; controlled by `--manifest-timestamp` flag. |
| `generator` | object | `{name, version}` of the tool that emitted. |
| `dialect` | string | `"postgres"`, `"mysql"`, or `"sqlite"`. Drives convention details (e.g., placeholder syntax in examples). |
| `package` | string | Go package name of the output. |
| `conventions` | object | See §5.2. |
| `generation_config` | object | Resolved feature-toggle snapshot — which sqlgen features are enabled package-wide. See §5.2.1. Added 2026-05-15 for MCP §4.2 `sqlgen://config` resource. |
| `layout` | string | `"single"` or `"per_entity"`. Lets consumers detect the layout without inspecting `entities[]`. |
| `entities` | array | Tables + views. Discriminated by `kind`. **Inline form** (single layout): full entity object per §5.3. **Index form** (per-entity layout): `{name, table, kind, file}` pointer to a sibling file. |
| `enums` | array | Generated enum types (PRD §8.3). See §5.6. Always inline. |
| `extras` | array | Composite types, extras, domain aliases (PRD §8.3). See §5.7. Always inline. |

**Detection rule for tools.** Consumers parsing the manifest can check
either the top-level `layout` field or the presence of a `file` field
on entity entries. Both signals agree.

#### 5.1.1 `generation_config` snapshot

Resolved (post-precedence, secret-stripped) snapshot of which sqlgen
features are enabled for the generated package. Pure boolean +
small-scalar surface — does not echo the full `sqlgen.yml`. Added
2026-05-15 so the MCP server (`sqlgen://config` resource, see
`docs/design/MCP.md` §4.2) can answer "what features are on?" without
re-reading the consumer's YAML.

```json
{
  "generation_config": {
    "cache":          true,
    "tenancy":        false,
    "events":         true,
    "soft_delete":    true,
    "views":          false,
    "graphql":         false,
    "graph_top_level": false,
    "audit_columns":   true
  }
}
```

| Field | Source PRD section | Notes |
|---|---|---|
| `cache` | §27 | `true` if any table has cache configured (package-wide signal — per-entity detail still lives in `entity.features.cache`). |
| `tenancy` | §29 | `true` if a tenant column is configured for any table. |
| `events` | §28 (events) | `true` if any table has event emission enabled. |
| `soft_delete` | §15 | `true` if any table uses soft-delete. |
| `views` | §16 | `true` if any view is generated. |
| `graphql` | §26 | `true` if GraphQL output is enabled (`generation.output.api: graphql` or `both`). |
| `audit_columns` | §4.6 | `true` if any table has audit columns (`created_at` / `updated_at` / etc.) declared. |
| ~~`pagination`~~ | — | Removed in Phase 29: `Paginate` / `Connection` are generated on every table the schema allows (PRD §4.6), so the flag could only read `true`. |
| `graph_top_level` | §30.4.3, §26.5.8 | `true` if the resolved GraphQL graph package lives outside `output.dir` (a top-level sibling) rather than nested beneath it. Inferred from the root-relative `resolver_dir` → `output.dir` path relationship — no mode flag. |

**Determinism.** Each field is computed once at manifest-build time
from the resolved package-wide config. No secrets, no DSNs, no file
paths — pure feature-toggle surface. Per-entity feature detail
remains under `entity.features.*` (already specified in §5.3).

### 5.2 Conventions block

```json
{
  "conventions": {
    "client_entry_points": {
      "query":    "client.<Entity>()",
      "mutation": "client.<Entity>()"
    },
    "error_sentinels": [
      { "name": "ErrNotFound",            "graphql_code": "NOT_FOUND",       "package": "models" },
      { "name": "ErrEmptyFilter",         "graphql_code": "",                "package": "models" },
      { "name": "ErrNilInput",            "graphql_code": "",                "package": "models" },
      { "name": "ErrAmbiguousFilter",     "graphql_code": "",                "package": "models" },
      { "name": "ErrInvalidCursor",       "graphql_code": "",                "package": "models" },
      { "name": "ErrDeadlock",            "graphql_code": "",                "package": "models" },
      { "name": "ErrConnectionFailed",    "graphql_code": "",                "package": "models" },
      { "name": "ErrConstraintViolation", "graphql_code": "",                "package": "models" },
      { "name": "ErrAlreadyRelated",      "graphql_code": "CONFLICT",        "package": "models" },
      { "name": "ErrNestedVerbConflict",  "graphql_code": "INVALID_INPUT",   "package": "models" },
      { "name": "ErrMissing",             "graphql_code": "UNAUTHENTICATED", "package": "tenancy" },
      { "name": "ErrMismatch",            "graphql_code": "FORBIDDEN",       "package": "tenancy" }
    ],
    "constraint_error_codes": {
      "unique":      "CONFLICT",
      "foreign_key": "BAD_REFERENCE",
      "check":       "INVALID_INPUT",
      "not_null":    "INVALID_INPUT"
    },
    "find_returns_nil_on_missing": false,
    "pagination": {
      "page_type": "Page",
      "list_envelope_suffix": "List",
      "cursor_encoding": "opaque-base64"
    },
    "call_options": {
      "type": "CallOptions",
      "fields": ["SkipHooks", "SkipEvents", "SkipCache", "Tx", "LockMode"],
      "header_bridge": [
        { "header": "Cache-Control: no-cache", "effect": "SkipCache=true" },
        { "header": "X-Skip-Events",            "effect": "SkipEvents=true" },
        { "header": "X-Skip-Hooks",             "effect": "SkipHooks=true (implies SkipCache)" }
      ]
    },
    "soft_delete": {
      "default_excluded_from_finds": true,
      "include_via": "set <SoftDeleteColumn> comparator on filter",
      "hard_delete_method_suffix": "HardDelete",
      "restore_method_suffix":     "Restore"
    },
    "comparator": {
      "package": "comparator",
      "families": ["Bool", "Enum", "ID", "JSON", "JSONB", "Number", "Slice", "String", "Time"],
      "shape_notes": [
        "All families share: Eq, Neq, In, NotIn, IsNull, IsNotNull",
        "Numeric families (Number, Time) add: Gt, Gte, Lt, Lte",
        "String adds: Like, ILike, NotLike, NotILike",
        "Slice (JSON columns, PostgreSQL arrays) adds: Contains, ContainedBy, Overlap"
      ],
      "composition": {
        "and":     "Filter.And: []*Filter — predicates AND'd together",
        "or":      "Filter.Or:  []*Filter — entries OR'd together; each entry's own fields are AND'd, so a union is one entry per branch",
        "nesting": "And and Or nest arbitrarily; default is AND across top-level fields"
      },
      "examples": {
        "field_filter":  "<pkg>.UserFilter{Email: &comparator.String{Eq: new(\"a@x.com\")}}",
        "text_contains": "&comparator.String{Contains: new(\"acme\")}",
        "numeric_range": "&comparator.Number[int]{Between: &comparator.Range[int]{Start: 18, End: 65}}",
        "time_after":    "&comparator.Time{Gt: new(cutoff)}",
        "id_in":         "&comparator.ID{In: []string{\"u1\", \"u2\", \"u3\"}}",
        "is_null":       "&comparator.NullableString{Null: new(true)}",
        "compound_or":   "<pkg>.UserFilter{Or: []*<pkg>.UserFilter{{Email: &comparator.String{Eq: new(\"a@x.com\")}}, {Email: &comparator.String{Eq: new(\"b@x.com\")}}}}"
      }
    },
    "omittable": {
      "package":      "omittable",
      "type":         "omittable.Value[T]",
      "purpose":      "Distinguishes 'not set' from 'set to zero value'. Used in <Entity>Update structs, comparator value fields, and other partial-update contexts.",
      "construction": [
        "omittable.Set(value) — set to a value",
        "omittable.Omit[T]() — explicitly unset (this is the zero value of Value[T])"
      ],
      "methods":       ["IsSet() bool", "IsZero() bool", "Get() (T, bool)", "MustGet() T"],
      "json_behavior": "Marshals to the wrapped value when set; omitted when unset (omitzero semantics on the wrapper, via IsZero)"
    }
  }
}
```

The conventions block answers package-wide "how does this work?" questions
once. Entity entries (§5.3) reference it by relying on the reader having
read it — no duplicated fields.

### 5.3 Entity shape

> **`files[]` follows `output.layout` (FIX-184).** Earlier revisions of this
> section showed a four-way split (`user_types_gen.go`, `user_query_gen.go`,
> `user_mutation_gen.go`, `user_relations_gen.go`) from a granular-emission
> design that never shipped. The field carries exactly one entry: the file
> holding the entity's own body — `models_gen.go` for a table and
> `views_gen.go` for a view under the default `single_file`,
> `<file_prefix>_gen.go` for either under `file_per_table`. It is not an index
> of every file mentioning the entity; `sorter_gen.go`, `cache_gen.go`,
> `event_hooks_gen.go` and the GraphQL `*_gen.graphqls` carry per-entity
> members under both layouts and are found through `conventions` / `features`.
> See PRD §30.4.2.

```json
{
  "kind": "table",
  "name": "User",
  "table": "users",
  "schema": null,
  "file_prefix": "user",
  "files": ["models_gen.go"],
  "comment": "Application users. Email is the login identity; rows are soft-deleted on account closure.",
  "indexes": [
    { "name": "users_pkey",       "columns": ["id"],                  "unique": true,  "method": "btree" },
    { "name": "users_email_key",  "columns": ["email"],               "unique": true,  "method": "btree" },
    { "name": "users_search_gin", "columns": ["full_name", "email"],  "unique": false, "method": "gin"  },
    { "name": "users_active_idx", "columns": ["id"],                  "unique": false, "method": "btree", "where": "deleted_at IS NULL" }
  ],
  "pk": {
    "kind": "single",
    "columns": [
      { "name": "id", "go_field": "ID", "go_type": "uuid.UUID" }
    ]
  },
  "features": {
    "soft_delete": null,
    "cache": {
      "ttl_seconds":    30,
      "hydration":      "lazy",
      "key_pattern":    "models:user:{id}",
      "invalidates_on": ["Create", "Update", "Delete", "SoftDelete", "Restore", "Increment"]
    },
    "events": {
      "enabled":       true,
      "types":         ["UserCreated", "UserUpdated", "UserDeleted", "UserSoftDeleted", "UserRestored"],
      "payload_shape": "models.UserEvent"
    },
    "tenancy": null,
    "audit_columns": ["created_at", "updated_at"]
  },
  "columns": [
    {
      "name": "id",
      "go_field": "ID",
      "go_type": "uuid.UUID",
      "db_type": "uuid",
      "nullable": false,
      "pk": true,
      "unique": true,
      "default": "gen_random_uuid()",
      "default_kind": "function",
      "auto": null,
      "comparator": "comparator.ID",
      "comment": "Primary key. Server-generated UUIDv7 (time-ordered)."
    },
    {
      "name": "email",
      "go_field": "Email",
      "go_type": "string",
      "db_type": "text",
      "nullable": false,
      "unique": true,
      "check": "email ~* '^[^@]+@[^@]+\\.[^@]+$'",
      "comparator": "comparator.String",
      "comment": "Primary contact email. Verified at signup; lowercased on write."
    },
    {
      "name": "created_at",
      "go_field": "CreatedAt",
      "go_type": "time.Time",
      "db_type": "timestamptz",
      "nullable": false,
      "default": "CURRENT_TIMESTAMP",
      "default_kind": "function",
      "auto": "insert",
      "comparator": "comparator.Time",
      "comment": "Row creation timestamp. Auto-managed; immutable after insert."
    }
  ],
  "relationships": [
    {
      "name": "Posts",
      "kind": "o2m",
      "target_entity": "Post",
      "fk": { "table": "posts", "column": "author_id" },
      "filter": null
    },
    {
      "name": "Roles",
      "kind": "m2m",
      "target_entity": "Role",
      "junction": { "table": "user_roles", "local_fk": "user_id", "target_fk": "role_id" },
      "filter": null
    }
  ],
  "methods": {
    "query": [
      {
        "name": "Get",
        "params":  [{ "name": "id", "type": "uuid.UUID" }],
        "returns": "*User",
        "errors":  ["ErrNotFound"],
        "notes":   "Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.",
        "sql_bodies": {
          "postgres": "SELECT id, email, full_name, created_at, updated_at, deleted_at FROM users WHERE id = $1 AND deleted_at IS NULL LIMIT 1"
        }
      },
      {
        "name": "GetMany",
        "params":  [{ "name": "input", "type": "*GetUsersInput" }],
        "returns": "[]*User",
        "errors":  [],
        "sql_bodies": {
          "postgres": "SELECT id, email, full_name, created_at, updated_at, deleted_at FROM users WHERE <filter> AND deleted_at IS NULL ORDER BY <sort> LIMIT <limit>"
        }
      },
      { "name": "Count",       "params": [{ "name": "filter", "type": "*UserFilter" }], "returns": "int64", "errors": [], "sql_bodies": { "postgres": "SELECT COUNT(*) FROM users WHERE <filter> AND deleted_at IS NULL" } },
      { "name": "Exists",      "params": [{ "name": "id", "type": "uuid.UUID" }],       "returns": "bool",  "errors": [], "sql_bodies": { "postgres": "SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL)" } },
      { "name": "ExistsWhere", "params": [{ "name": "filter", "type": "*UserFilter" }], "returns": "bool",  "errors": [], "sql_bodies": { "postgres": "SELECT EXISTS(SELECT 1 FROM users WHERE <filter> AND deleted_at IS NULL)" } },
      { "name": "Paginate",    "params": [{ "name": "input", "type": "PaginateInput[UserFilter]" }],   "returns": "*PaginateResult[User]", "errors": [],                    "notes": "Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry." },
      { "name": "Connection",  "params": [{ "name": "input", "type": "ConnectionInput[UserFilter]" }], "returns": "*Connection[User]",     "errors": ["ErrInvalidCursor"],  "notes": "Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry." },
      { "name": "Stream",      "params": [{ "name": "input", "type": "*StreamUsersInput" }],           "returns": "iter.Seq2[*User, error]", "errors": [], "sql_bodies": { "postgres": "SELECT ... FROM users WHERE <filter> AND deleted_at IS NULL ORDER BY <sort>" } }
    ],
    "mutation": [
      {
        "name":    "Create",
        "params":  [{ "name": "input", "type": "*CreateUserInput" }],
        "returns": "*User",
        "errors":  ["ErrConstraintViolation"],
        "sql_bodies": {
          "postgres": "INSERT INTO users (<columns>) VALUES (<values>) RETURNING id"
        }
      },
      {
        "name":    "Update",
        "params":  [{ "name": "id", "type": "uuid.UUID" }, { "name": "input", "type": "*UpdateUserInput" }],
        "returns": "*User",
        "errors":  ["ErrNotFound", "ErrConstraintViolation"],
        "notes":   "Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged (§9.5).",
        "sql_bodies": {
          "postgres": "UPDATE users SET <set> WHERE id = $1"
        }
      },
      { "name": "CreateMany",      "params": [{ "name": "inputs", "type": "[]*CreateUserInput" }], "returns": "[]*User", "errors": ["ErrConstraintViolation"], "sql_bodies": { "postgres": "INSERT INTO users (<columns>) VALUES <values> RETURNING id" } },
      { "name": "UpdateMany",      "params": [{ "name": "items", "type": "[]UpdateUserItem" }],    "returns": "[]*User", "errors": ["ErrConstraintViolation"], "notes": "A primary key that does not exist is silently skipped, even under strict_updates.", "sql_bodies": { "postgres": "UPDATE users SET <set> WHERE id = $1" } },
      { "name": "UpdateWhere",     "params": [{ "name": "filter", "type": "*UserFilter" }, { "name": "input", "type": "*UpdateUserInput" }], "returns": "[]*User", "errors": ["ErrEmptyFilter", "ErrConstraintViolation"], "sql_bodies": { "postgres": "UPDATE users SET <set> WHERE <filter> RETURNING id" } },
      { "name": "Upsert",          "params": [{ "name": "input", "type": "*CreateUserInput" }, { "name": "target", "type": "UserConflictTarget" }], "returns": "*User", "errors": ["ErrConstraintViolation"], "sql_bodies": { "postgres": "INSERT INTO users (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING id" } },
      { "name": "Increment",       "params": [{ "name": "id", "type": "uuid.UUID" }, { "name": "input", "type": "IncrementInput[UserIncrementColumn]" }], "returns": "", "errors": ["ErrNotFound"], "sql_bodies": { "postgres": "UPDATE users SET <column> = <column> + $1 WHERE id = $2" } },
      { "name": "SoftDelete",      "params": [{ "name": "id", "type": "uuid.UUID" }],       "returns": "*User",   "errors": [], "notes": "Idempotent — a primary key that does not exist is not an error (§9.5).", "sql_bodies": { "postgres": "UPDATE users SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1" } },
      { "name": "SoftDeleteMany",  "params": [{ "name": "ids", "type": "[]uuid.UUID" }],    "returns": "[]*User", "errors": [], "sql_bodies": { "postgres": "UPDATE users SET deleted_at = CURRENT_TIMESTAMP WHERE id IN (<ids>)" } },
      { "name": "SoftDeleteWhere", "params": [{ "name": "filter", "type": "*UserFilter" }], "returns": "[]*User", "errors": ["ErrEmptyFilter"], "sql_bodies": { "postgres": "UPDATE users SET deleted_at = CURRENT_TIMESTAMP WHERE <filter> AND deleted_at IS NULL RETURNING id" } },
      { "name": "Restore",         "params": [{ "name": "id", "type": "uuid.UUID" }],       "returns": "*User",   "errors": [], "sql_bodies": { "postgres": "UPDATE users SET deleted_at = $1 WHERE id = $2" } },
      { "name": "RestoreMany",     "params": [{ "name": "ids", "type": "[]uuid.UUID" }],    "returns": "[]*User", "errors": [], "sql_bodies": { "postgres": "UPDATE users SET deleted_at = $1 WHERE id IN (<ids>)" } },
      { "name": "RestoreWhere",    "params": [{ "name": "filter", "type": "*UserFilter" }], "returns": "[]*User", "errors": ["ErrEmptyFilter"], "sql_bodies": { "postgres": "UPDATE users SET deleted_at = $1 WHERE <filter> AND deleted_at IS NOT NULL RETURNING id" } },
      { "name": "HardDelete",      "params": [{ "name": "id", "type": "uuid.UUID" }],       "returns": "",        "errors": [], "notes": "Idempotent — a primary key that does not exist is not an error (§9.5).", "sql_bodies": { "postgres": "DELETE FROM users WHERE id = $1" } },
      { "name": "HardDeleteMany",  "params": [{ "name": "ids", "type": "[]uuid.UUID" }],    "returns": "",        "errors": [], "sql_bodies": { "postgres": "DELETE FROM users WHERE id IN (<ids>)" } },
      { "name": "HardDeleteWhere", "params": [{ "name": "filter", "type": "*UserFilter" }], "returns": "",        "errors": ["ErrEmptyFilter"], "sql_bodies": { "postgres": "DELETE FROM users WHERE <filter> RETURNING id" } }
    ]
  },
  "filter": {
    "type": "UserFilter",
    "fields": [
      { "name": "ID",        "type": "*comparator.ID"     },
      { "name": "Email",     "type": "*comparator.String" },
      { "name": "Name",      "type": "*comparator.String" },
      { "name": "CreatedAt", "type": "*comparator.Time"   },
      { "name": "And",       "type": "[]UserFilter"       },
      { "name": "Or",        "type": "[]UserFilter"       }
    ]
  },
  "sort": {
    "type":   "UserSort",
    "fields": ["ID", "Email", "Name", "CreatedAt"]
  },
  "examples": {
    "read": [
      "// Look a user up by primary key, then page through recent matches.",
      "func userQueryExample(ctx context.Context, c *models.Client, id uuid.UUID) error {",
      "    // Get returns ErrNotFound when no row matches.",
      "    u, err := c.Users().Get(ctx, id)",
      "    if err != nil {",
      "        return fmt.Errorf(\"get user: %w\", err)",
      "    }",
      "    _ = u",
      "",
      "    // Offset pagination with filter + sort.",
      "    res, err := c.Users().Paginate(ctx, models.PaginateInput[models.UserFilter]{",
      "        Filter: &models.UserFilter{",
      "            Email:     &comparator.String{Like: omittable.Set(\"%@acme.com\")},",
      "            CreatedAt: &comparator.Time{Gt: omittable.Set(time.Now().AddDate(0, -1, 0))},",
      "        },",
      "        Limit: 20,",
      "    })",
      "    if err != nil {",
      "        return fmt.Errorf(\"paginate users: %w\", err)",
      "    }",
      "    for _, user := range res.Items { _ = user }",
      "    return nil",
      "}"
    ],
    "write": [
      "// Create a user with sentinel-aware error handling.",
      "func userMutationExample(ctx context.Context, c *models.Client) (*models.User, error) {",
      "    u, err := c.Users().Create(ctx, &models.CreateUserInput{",
      "        Email: \"bob@acme.com\",",
      "        Name:  \"Bob\",",
      "    })",
      "    if err != nil {",
      "        var ce *models.ConstraintError",
      "        if errors.As(err, &ce) {",
      "            switch ce.Type {",
      "            case models.ConstraintUnique:      return nil, err  // duplicate email",
      "            case models.ConstraintForeignKey:  return nil, err  // FK violation (e.g., role_id)",
      "            case models.ConstraintCheck,",
      "                 models.ConstraintNotNull:     return nil, err  // validation failure",
      "            }",
      "        }",
      "        return nil, fmt.Errorf(\"create user: %w\", err)",
      "    }",
      "    return u, nil",
      "}"
    ]
  }
}
```

**Relationship `fk` and `junction` fields.** `fk.table` names the table
that holds `fk.column`: the target's table when the column is on the
target (an `o2m` edge such as `posts.author_id` above, or a has-one
`o2o`), and the entity's own table when the entity holds the column (a
belongs-to edge, such as `Profile.users` → `profiles.user_id`).
`junction.table` names the M2M junction. Both carry
the bare table name, with an optional `schema` beside it when sqlgen
resolved one (a config-declared `junction:` resolves by PRD §5.5's
rule, and carries no schema only when it names no parsed table),
split the way an entity splits `table` and `schema` (PRD §30.4.2), so
same-named tables in two schemas stay distinguishable.

**Method `sql_bodies` field (added 2026-05-15).** Each method object
under `methods.query[]` and `methods.mutation[]` carries an optional
`sql_bodies` map: `{<dialect>: <canonical SQL string>}` describing the
statement *that method* issues. For a single-dialect generated package
(the common case), it's a single-key map keyed on the package's dialect.
Placeholders are emitted as the dialect renders them (`$1`/`$2`/... for
postgres, `?` for mysql / sqlite). Clauses the generated code assembles
from caller input at call time are rendered as placeholder tokens —
`<filter>`, `<sort>`, `<limit>`, `<set>`, `<columns>`, `<values>`,
`<conflict_target>`, `<excluded>`, `<column>`, `<ids>` / `<pks>`. PRD
§30.4.2 carries the full token table. A method that issues no statement
of its own (`Paginate` and `Connection` compose `Count` and `GetMany`)
carries no `sql_bodies` key at all.

Captured deterministically from the same SQL-build pipeline that
produces the `_gen.go` query bodies — no separate generation path.
Lets the MCP `sqlgen_show_sql` tool (see `docs/design/MCP.md` §4.1) answer
"what SQL does this method emit?" without round-tripping to the
generated Go source. Field omitted when the generator runs without
the SQL-capture hook (older manifests; surfaced by the MCP server as
`-32005 METHOD_SQL_UNAVAILABLE`).

**Extended-metadata fields (new in v1):**

| Field | Where | Source | Notes |
|---|---|---|---|
| `comment` | top-level | `COMMENT ON TABLE` | Domain semantics that aren't expressible in types. PG / MySQL native; SQLite uses adjacent `--` comments captured by the parser. |
| `comment` | per-column | `COMMENT ON COLUMN` | Per-column equivalent. Empty string when no comment. |
| `indexes[]` | top-level | parser / introspector | All indexes including UNIQUE and non-unique. Carries `name`, `columns[]`, `unique`, `method` (btree / gin / gist / hash), optional `where` for partial indexes. Includes the primary-key index. |
| `check` | per-column | parser / introspector | Raw CHECK-constraint expression text as written in the DDL. Multi-column CHECKs are flattened onto each participating column. |
| `sql_bodies` | per-method (query / mutation) | SQL-build pipeline | Per-dialect canonical SQL body. Map shape: `{<dialect>: string}`. Single-key map for single-dialect packages. Added 2026-05-15 for MCP §4.1 `sqlgen_show_sql`. |
| `default_kind` | per-column | parser inference | One of `literal` (constant value), `function` (`gen_random_uuid()`, `CURRENT_TIMESTAMP`), `expression` (anything else). Lets agents distinguish "you must provide this" from "the database provides this." |
| `features.events` | top-level | `events:` config | `{enabled, types[], payload_shape}`. Populated only when `events.enabled: true` for the entity. |
| `features.cache.key_pattern` | top-level | cache config + PK | Template form (`models:user:{id}`). Combined-PK entities use `{workspace_id}:{key}`-style templates. |
| `features.cache.invalidates_on` | top-level | static | List of method names that invalidate the entry. Always the same set per PRD §27 — included per-entity so agents don't need to cross-reference. |

Composite-PK and tenanted entity (`WorkspaceSetting`):

```json
{
  "kind": "table",
  "name": "WorkspaceSetting",
  "table": "workspace_settings",
  "pk": {
    "kind": "composite",
    "struct": "WorkspaceSettingPK",
    "columns": [
      { "name": "workspace_id", "go_field": "WorkspaceID", "go_type": "uuid.UUID" },
      { "name": "key",          "go_field": "Key",         "go_type": "string"    }
    ]
  },
  "features": {
    "tenancy": {
      "column":  "workspace_id",
      "mode":    "verify-match",
      "missing_resolver_error": "tenancy.ErrMissing",
      "mismatch_error":         "tenancy.ErrMismatch"
    }
  },
  "methods": {
    "query": [
      { "name": "Get", "params": [{ "name": "pk", "type": "WorkspaceSettingPK" }], "returns": "*WorkspaceSetting", "errors": ["ErrNotFound", "tenancy.ErrMissing", "tenancy.ErrMismatch"] }
    ],
    "mutation": [
      { "name": "Update", "params": [{ "name": "pk", "type": "WorkspaceSettingPK" }, { "name": "input", "type": "*UpdateWorkspaceSettingInput" }], "returns": "*WorkspaceSetting", "errors": ["ErrNotFound", "ErrConstraintViolation", "tenancy.ErrMissing", "tenancy.ErrMismatch"] },
      { "name": "HardDelete", "params": [{ "name": "pk", "type": "WorkspaceSettingPK" }], "returns": "", "errors": ["tenancy.ErrMissing", "tenancy.ErrMismatch"] }
    ]
  }
}
```

### 5.4 View shape

Views (PRD §16) share the entity shape with `kind: "view"`. Differences
from tables:

- `pk.kind: "none"` when the view has no declared PK (most file-sourced
  views). `Get` is omitted with it; views carry no mutation methods but
  `Refresh` / `RefreshConcurrently` on a matview.
- `relationships: []` always (views don't carry generated relationship
  loaders).
- `source` field carrying the file path of the annotation file or the SQL
  definition.

```json
{
  "kind": "view",
  "name": "ActiveUserView",
  "table": "active_users_view",
  "source": { "kind": "file", "path": "views/active_users.sql" },
  "pk": { "kind": "none" },
  "columns": [ ... ],
  "methods": {
    "query": [
      { "name": "List",  "params": [ ... ], "returns": "*ActiveUserViewList", "errors": [] },
      { "name": "Count", "params": [ ... ], "returns": "int64",                "errors": [] }
    ],
    "mutation": []
  }
}
```

### 5.5 Versioning policy

- `schema_version` follows semver, **separate from sqlgen's own version**.
- Breaking changes to entity / convention shape → major bump.
- Adding optional fields → minor bump (consumers tolerate).
- Bug fixes / clarifications → patch.
- Every breaking version has its JSON Schema preserved at
  `cmd/sqlgen/manifest/schema/v<N>.json`. Generation always emits the
  latest; consumers pin via the `$schema` URL.
- v1 freezes at Phase 18 closure. Sub-1.0 churn is allowed during the
  Phase 18 sub-items; the version starts at `0.x` and lifts to `1.0.0`
  with the Phase 18 closure sweep.

### 5.6 Enums section

```json
{
  "enums": [
    {
      "name": "OrderStatus",
      "go_type": "OrderStatus",
      "db_type": "order_status_enum",
      "values": ["pending", "shipped", "delivered"],
      "graphql_name": "OrderStatus",
      "slice_type": null
    },
    {
      "name": "UserRole",
      "go_type": "UserRole",
      "db_type": "user_role",
      "values": ["admin", "editor", "viewer"],
      "graphql_name": "UserRole",
      "slice_type": "UserRoleSlice"
    }
  ]
}
```

`slice_type` populated only for PostgreSQL enum arrays (PRD §8.3
`<Enum>Slice` companion).

### 5.7 Extras section

Composite types, extra types, and domain aliases (PRD §8.3 / §4.10):

```json
{
  "extras": [
    {
      "kind": "composite",
      "name": "Address",
      "go_type": "Address",
      "fields": [
        { "name": "Street",  "go_type": "string", "json": "street"   },
        { "name": "City",    "go_type": "string", "json": "city"     },
        { "name": "ZipCode", "go_type": "string", "json": "zip_code" }
      ]
    },
    {
      "kind": "domain",
      "name": "Email",
      "go_type": "Email",
      "base_type": "string"
    },
    {
      "kind": "extra",
      "name": "UserMeta",
      "go_type": "UserMeta",
      "source": "config",
      "json_only": true
    }
  ]
}
```

### 5.8 JSON Schema location

The authoritative JSON Schema lives at
`cmd/sqlgen/manifest/schema/v1.json` in the sqlgen repo. The `$schema`
URL emitted in generated manifests is the **GitHub raw URL of the
latest tagged sqlgen release**, in the form:

```
https://raw.githubusercontent.com/<owner>/sqlgen/v<version>/cmd/sqlgen/manifest/schema/v1.json
```

Pinning to a tagged release (not `main`) gives consumers an immutable
schema document for each emitted manifest. Phase 18 generation reads
sqlgen's own version at build time and substitutes it into the
emitted `$schema` value.

If a future hosted version becomes available (`sqlgen.dev/manifest/v1.json`
or similar), the URL can shift in a backwards-compatible way — the
schema **content** is the contract, the URL is metadata pointing at it.

### 5.9 Go types

The runtime package `github.com/teandresmith/sqlgen/manifest` exports
the typed shape that mirrors the JSON schema. The same types serve as
both the JSON unmarshal target and the public API on the generated
`Client.Manifest()` / `Client.ManifestEntity()` methods. Cross-reference
with §5.1–§5.8 — those are the canonical contract; the Go types here
mirror them one-to-one.

```go
// manifest/types.go

package manifest

import "time"

type Document struct {
    SchemaVersion    string           `json:"schema_version"`
    GeneratedAt      time.Time        `json:"generated_at"`
    Generator        Generator        `json:"generator"`
    Dialect          Dialect          `json:"dialect"`
    Package          string           `json:"package"`
    Layout           Layout           `json:"layout"`
    Conventions      Conventions      `json:"conventions"`
    GenerationConfig GenerationConfig `json:"generation_config"` // added 2026-05-15; see §5.1.1
    Entities         []EntityIndex    `json:"entities"`
    Enums            []Enum           `json:"enums"`
    Extras           []Extra          `json:"extras"`
}

// GenerationConfig is the resolved feature-toggle snapshot — pure
// booleans + small scalars. Surfaced via Client.Manifest().GenerationConfig
// and the sqlgen://config MCP resource. See §5.1.1.
type GenerationConfig struct {
    Cache         bool `json:"cache"`
    Tenancy       bool `json:"tenancy"`
    Events        bool `json:"events"`
    SoftDelete    bool `json:"soft_delete"`
    Views         bool `json:"views"`
    GraphQL       bool `json:"graphql"`
    GraphTopLevel bool `json:"graph_top_level"`
    AuditColumns  bool `json:"audit_columns"`
}

// Method carries the canonical SQL body alongside the existing
// signature surface. SQLBodies is keyed by dialect; single-key map for
// single-dialect packages. See §5.3 "Method sql_bodies field".
type Method struct {
    Name      string            `json:"name"`
    Params    []MethodParam     `json:"params"`
    Returns   string            `json:"returns"`
    Errors    []string          `json:"errors"`
    Notes     string            `json:"notes,omitempty"`
    Source    *MethodSource     `json:"source,omitempty"`
    SQLBodies map[Dialect]string `json:"sql_bodies,omitempty"` // added 2026-05-15
}

// EntityIndex is always the lightweight pointer form. Use
// Client.ManifestEntity(table) for the full record regardless of disk
// layout.
type EntityIndex struct {
    Name  string     `json:"name"`
    Table string     `json:"table"`
    Kind  EntityKind `json:"kind"`           // "table" | "view"
    File  string     `json:"file,omitempty"` // only in per_entity layout
}

type Entity struct {
    Name          string         `json:"name"`
    Table         string         `json:"table"`
    Schema        string         `json:"schema,omitempty"`
    FilePrefix    string         `json:"file_prefix"`
    Files         []string       `json:"files"`
    Kind          EntityKind     `json:"kind"`
    PK            PK             `json:"pk"`
    Features      Features       `json:"features"`
    Columns       []Column       `json:"columns"`
    Relationships []Relationship `json:"relationships"`
    Methods       Methods        `json:"methods"`
    Filter        FilterStruct   `json:"filter"`
    Sort          SortStruct     `json:"sort"`
    Examples      Examples       `json:"examples"`
}

// ... Column, Relationship, Method, Filter, Sort, Features, PK,
// Conventions, Generator, Enum, Extra (each a flat data struct).
// Typed aliases: Layout, Dialect, EntityKind, RelationshipKind,
// ComparatorFamily, etc.
```

**Sentinel errors:**

```go
var (
    ErrEntityNotFound = errors.New("manifest: entity not found")
    ErrParseManifest  = errors.New("manifest: parse failure (corrupt embed)")
)
```

**Helper for the generated init:**

```go
// LoadInto parses raw JSON into *Document, then populates entities map
// from either the inline document (single layout) or the embedded
// per-entity FS (per_entity layout). Used by the generated init()
// in each consumer package.
func LoadInto(doc **Document, raw []byte, entitiesFS embed.FS, entities map[string]*Entity) error
```

**Type count.** ~25 named types total. All flat data structs with JSON
struct tags. No methods other than `String()` on a few enum-style
aliases (`Dialect`, `EntityKind`, `Layout`, `RelationshipKind`).

**Versioning.** Type evolution follows the manifest schema version
(§5.5):

- Minor versions add new fields (non-breaking Go change — Go field
  addition is backwards-compatible).
- Major versions remove or rename fields (breaking — consumers update
  sqlgen and adjust call sites).
- The runtime package's Go module version tracks sqlgen's overall
  module version, not the manifest schema version directly. Consumers
  see the schema-version coupling via `Document.SchemaVersion` at
  runtime and via the imported types at compile time.

---

## 6. The markdown directory

### 6.1 `_index.md`

```markdown
# Models package — entity index

Generated 2026-05-15 · sqlgen v0.42.0 · dialect: postgres · 8 entities

| Entity | Table | PK | Tenanted | Soft-delete | Cache | Manifest |
|---|---|---|---|---|---|---|
| User | `users` | `id` uuid | — | — | 30s | [user.md](user.md) |
| Category | `categories` | `id` uuid | — | — | 30s | [category.md](category.md) |
| Post | `posts` | `id` uuid | — | ✓ | — | [post.md](post.md) |
| ... |

**Read first:** [`_conventions.md`](_conventions.md) — error sentinels,
pagination, comparator types, soft-delete semantics, `CallOptions` usage.

**Programmatic queries:** [`manifest_gen.json`](manifest_gen.json) carries
the same data in a versioned JSON Schema (`v1`).
```

### 6.2 `_conventions.md`

Mirrors `conventions` block from §5.2, rendered as prose + tables.
Includes:

- Entry points (`c.Q` / `c.M`).
- Error sentinel table with GraphQL codes.
- Pagination shape (`Page` / `<Entity>List`).
- `CallOptions` field reference + HTTP header bridge.
- Soft-delete inclusion semantics.
- **Comparator package** — full surface walkthrough: families (Bool /
  Enum / ID / JSON / JSONB / Number / Slice / String / Time), shared fields
  (Eq / Neq / In / NotIn / IsNull / IsNotNull), per-family additions
  (Like / ILike for String; Gt / Gte / Lt / Lte for numerics;
  Contains / Overlap for Slice), composition via `Filter.And` /
  `Filter.Or` with arbitrary nesting. Examples per family.
- **Omittable package** — `omittable.Value[T]` as the "not set" vs
  "set to zero value" distinguisher used in `<Entity>Update` structs
  and comparator value fields. Construction (`Set(v)` / `Omit[T]()`),
  methods (`IsSet`, `IsZero`, `Get() (T, bool)`, `MustGet`), JSON
  marshaling semantics (`omitzero` via `IsZero` when unset).

Cross-referenced by every `<entity>.md` instead of being restated.
Agents reading `_conventions.md` should have enough context to write
correct filter / update / mutation code without consulting the
`omittable` or `comparator` package source.

### 6.3 Per-entity markdown

Sketch for `user.md` (shape pinned by §10 sub-item 18.4 templates):

````markdown
# User

**Table** `users` · **PK** `id` (uuid, default `gen_random_uuid()`) · **Cache** 30s

**Files**
- `user_types_gen.go` — `User`, `CreateUserInput`, `UpdateUserInput`, `UserFilter`, `UserSorter`
- `user_gen.go` — the `UserClient` read + write methods reached via `client.Users()`
- `user_relations_gen.go` — `Posts`, `Roles` loaders

## Columns
| Field | Go type | DB | Notes |
|---|---|---|---|
| ID | `uuid.UUID` | `uuid` | PK |
| Email | `string` | `text` | UNIQUE |
| Name | `string` | `text` | |
| CreatedAt | `time.Time` | `timestamptz` | auto on insert |
| UpdatedAt | `time.Time` | `timestamptz` | auto on update |

## Relationships
- `Posts []Post` — O2M via `posts.author_id`
- `Roles []Role` — M2M via `user_roles(user_id, role_id)`

## Query (`client.Users()`)
```go
Get(ctx, id uuid.UUID, opts ...func(*CallOptions[UserFieldOptions])) (*User, error)   // ErrNotFound when no row matches
GetMany(ctx, input *GetUsersInput, opts ...) ([]*User, error)
Exists(ctx, id uuid.UUID, opts ...) (bool, error)
ExistsWhere(ctx, filter *UserFilter, opts ...) (bool, error)
Count(ctx, filter *UserFilter, opts ...) (int64, error)
Paginate(ctx, input PaginateInput[UserFilter], opts ...) (*PaginateResult[User], error)
Connection(ctx, input ConnectionInput[UserFilter], opts ...) (*Connection[User], error)
Stream(ctx, input *StreamUsersInput, opts ...) iter.Seq2[*User, error]
```

## Mutation (`client.Users()`)
```go
Create(ctx, input *CreateUserInput, opts ...) (*User, error)   // ErrConstraintViolation (ConstraintUnique) on duplicate email
CreateMany(ctx, inputs []*CreateUserInput, opts ...) ([]*User, error)
Update(ctx, id uuid.UUID, input *UpdateUserInput, opts ...) (*User, error)
UpdateMany(ctx, items []UpdateUserItem, opts ...) ([]*User, error)
UpdateWhere(ctx, filter *UserFilter, input *UpdateUserInput, opts ...) ([]*User, error)
Upsert(ctx, input *CreateUserInput, target UserConflictTarget, opts ...) (*User, error)
Increment(ctx, id uuid.UUID, input IncrementInput[UserIncrementColumn], opts ...) error
SoftDelete / SoftDeleteMany / SoftDeleteWhere (ctx, ..., opts ...) (*User | []*User, error)
Restore    / RestoreMany    / RestoreWhere    (ctx, ..., opts ...) (*User | []*User, error)
HardDelete / HardDeleteMany / HardDeleteWhere (ctx, ..., opts ...) error
```

Both blocks hang off the same per-entity accessor — there is no `c.Q` / `c.M`
split. The read and write halves are separated here only to mirror
`methods.query[]` and `methods.mutation[]`.

## Filter
```go
type UserFilter struct {
    ID, Email, Name *comparator.String
    CreatedAt       *comparator.Time
    And, Or         []UserFilter
}
```

## Example
```go
res, err := c.Users().Paginate(ctx, models.PaginateInput[models.UserFilter]{
    Filter: &models.UserFilter{Email: &comparator.String{Like: omittable.Set("%@acme.com")}},
    Limit:  20,
})
```
````

---

## 7. Generator strategy

### 7.1 Package layout

```
cmd/sqlgen/manifest/
├── builder.go        # Schema + GenContext + APIContext → Manifest struct
├── builder_test.go
├── emit_json.go      # Manifest → manifest_gen.json
├── emit_markdown.go  # Manifest → directory of *.md files
├── emit_test.go
├── templates/
│   ├── index.md.tmpl
│   ├── conventions.md.tmpl
│   └── entity.md.tmpl
└── schema/
    └── v1.json       # JSON Schema for manifest_gen.json
```

### 7.2 Pipeline integration

The manifest stage runs **after** all other generation stages so it
reflects the *landed* code shape (including effects of dedup passes like
FIX-094's per-target client dedup). Order in `cmd/sqlgen/gen/orchestrate.go`:

1. Parse schema (existing).
2. Build `TableContext[]` for each entity (existing).
3. Emit per-entity Go files (existing).
4. Emit shared / unified client / API files (existing).
5. **Build `Manifest` from all contexts (NEW).**
6. **Emit `manifest_gen.json` (NEW, if `formats` includes `json`).**
7. **Emit `manifest/*.md` (NEW, if `formats` includes `markdown`).**
8. **Emit breadcrumb files (NEW): `CLAUDE.md` / `AGENTS.md` per
   `breadcrumbs.claude_md` / `breadcrumbs.agents_md`.** Extend the
   per-file header generator to inject the §4.7 package-doc-comment
   pointer into `models_gen.go` per `breadcrumbs.package_doc`.
9. Stale-file cleanup (existing — extended to cover `manifest/*.md`,
   the breadcrumb files, and the doc-comment pointer when toggled off).
10. Formatter pass over `.go` files (existing — `.json` and `.md` are
    pre-formatted by the emitters).

The build step (5) is read-only against the existing contexts. No
existing stage takes a dependency on it — the manifest is a leaf in the
codegen DAG.

### 7.3 What the builder reads

```go
// builder.go (sketch)

type Manifest struct {
    SchemaVersion string
    GeneratedAt   time.Time
    Generator     GeneratorInfo
    Dialect       string
    Package       string
    Conventions   Conventions
    Entities      []Entity   // sorted by Name
    Enums         []Enum     // sorted by Name
    Extras        []Extra    // sorted by Name
}

func Build(
    schema *parser.Schema,
    cfg    *config.Root,
    tables []*gen.TableContext,
    api    *gen.APIContext, // may be nil if API disabled
) (*Manifest, error)
```

- **`schema`** drives columns, FKs, declared SQL types.
- **`cfg`** drives feature gates (cache TTL, tenancy column, soft-delete
  detection, operations gating).
- **`tables[]`** drives the *effective* per-table surface — the
  operation toggles that gate each generated method and the
  per-relationship FieldOptions shape.
- **`api`** drives GraphQL convention details (sentinel → code mapping
  when present).

The builder does NOT re-parse or re-resolve types — it consumes the same
contexts already used by the template stages. Drift between code and
manifest is structurally impossible.

### 7.4 Emit determinism

Both emitters use `text/template` (markdown) or a custom JSON encoder
(JSON) with explicit iteration over pre-sorted slices. No `range` over
`map`. No `time.Now()` outside the controlled `--manifest-timestamp` flag.
A `TestEmit_Deterministic` test in `cmd/sqlgen/manifest/emit_test.go`
emits twice and asserts byte equality.

---

## 8. Testing strategy

### 8.1 Unit tests

- `cmd/sqlgen/manifest/builder_test.go`
  - Table-driven cases over a synthetic schema covering single-PK,
    composite-PK, tenanted, soft-deleted, cached, view, and m2m
    relationship.
  - Pins entity shape per case.
- `cmd/sqlgen/manifest/emit_test.go`
  - `TestEmitJSON_GoldenSnapshot` — pins JSON byte-for-byte against an
    in-repo fixture.
  - `TestEmitMarkdown_GoldenSnapshot` — pins per-entity markdown.
  - `TestEmit_Deterministic` — runs emission twice, asserts byte equality.
  - `TestEmitJSON_ValidatesAgainstSchema` — loads `schema/v1.json` and
    validates the emitted JSON. No new runtime dependency (use
    `github.com/santhosh-tekuri/jsonschema/v5` as a test-only dep).
- `cmd/sqlgen/config/validate_test.go`
  - Validation cases from §3.4 (empty formats, unknown format, dir
    conflict, table-without-package opt-in, reserved `_` prefix).

### 8.2 E2E coverage (example matrix)

Each existing example module under `cmd/sqlgen/testdata/examples/` gets:

- `sqlgen.yml` extended with `generation.manifest: { enabled: true }`
  on a new variant config (or as the default — open question §11.6).
- `expected/manifest/` golden directory under the existing `expected/`
  tree, regenerated via `make update-golden-e2e`.
- `tests/manifest_test.go` — at-runtime test loading
  `manifest_gen.json`, asserting:
  - `schema_version == "1.0.0"`
  - Every known table is present in `entities[]`.
  - At least one composite-PK and one tenanted entity have the right
    `pk.kind` and `features.tenancy` values.
  - `enums[]` contains every `CREATE TYPE ... AS ENUM` from the schema.
- `TestE2EGoldenFiles` in `cmd/sqlgen/e2e_test.go` already enforces
  per-file diffing across `expected/` — manifest goldens hook in for free.

Dialect coverage: postgres, mysql, sqlite, graphql (the graphql example
exercises the separate graph-package output + the API context branch of
the builder).

### 8.3 Regression coverage

- `TestManifest_DisabledByDefault` — no `manifest/` directory emitted
  when `generation.manifest.enabled` is unset.
- `TestManifest_StaleCleanup` — removing a table re-runs generation and
  asserts the corresponding `manifest/<entity>.md` is gone.
- `TestManifest_TableOptOut` — `tables.<name>.manifest.enabled: false`
  excludes from `entities[]` and removes the per-entity markdown.

---

## 10. Phase 18 sub-item preview

These are **drafts** that go through `/phase 18` once §11 closes. Listed
here so the design doc shows the full implementation arc.

### 18.1 Config schema + validation

- `ManifestConfig` / `TableManifestConfig` types in
  `cmd/sqlgen/config/types.go`.
- Resolution order in `cmd/sqlgen/config/resolve.go`.
- Validation rules from §3.4 in `cmd/sqlgen/config/validate.go`.
- Unit tests for resolution + validation.

### 18.2 Manifest builder package

- `cmd/sqlgen/manifest/builder.go` — `Build()` entry point.
- Per-entity / per-column / per-method transformation logic.
- **Extended metadata extraction:** SQL `COMMENT ON TABLE` / `COMMENT
  ON COLUMN` (PG / MySQL native; SQLite adjacent `--` comments via the
  parser), full index list from parser/introspector (including
  non-unique, partial, GIN/GIST), CHECK constraints flattened onto
  participating columns, `default_kind` inference (`literal` /
  `function` / `expression`), `features.events.types[]` from the
  events config, `features.cache.key_pattern` from the cache config +
  PK shape. All sourced from existing parser + config; no new external
  inputs.
- **MCP-driven addenda (added 2026-05-15):**
  - **`generation_config` snapshot (§5.1.1):** compute the
    feature-toggle booleans from the resolved package-wide config
    (`cache`, `tenancy`, `events`, `soft_delete`, `views`, `graphql`,
    `graph_top_level`, `audit_columns`; `pagination` was dropped in Phase 29). Pure read over the
    existing resolved config — no new inputs, no secrets / DSNs / paths.
  - **`methods[].sql_bodies` capture:** hook into the existing
    SQL-build pipeline (where `_gen.go` query bodies are composed) to
    persist the canonical SQL into each method record on the
    in-memory `*manifest.Document`. Single-dialect packages produce a
    single-key map keyed on `cfg.Dialect`. Placeholders are emitted
    as the dialect renders them (`$1`/`?`); filter / sort / pagination
    clauses use the same `<filter>` / `<sort>` / `<limit>` tokens the
    builder substitutes at call time.
- **Wire `Table.Comment` / `Column.Comment` / relationship description
  through the GraphQL schema templates** (`cmd/sqlgen/gen/templates/api/schema.graphqls.tmpl`
  + the shared template). The parser already captures comments end-to-end
  (PG `pg_description`, MySQL `information_schema`, file-parser tests
  pin both table + column comments), but the existing GraphQL templates
  emit template-rendered placeholders (`"X corresponds to the Y table"`,
  `"<column_name>"`) instead of the captured comments — a pre-existing
  Phase 16 gap surfaced by the manifest design. Fix: read
  `Table.Comment` / `Column.Comment` when emitting the `"""description"""`
  / `"field doc"` blocks; fall back to the current placeholder string
  only when the parser comment is empty. Same data source as the manifest
  comment fields, so the work is co-located. ~10 LOC template change +
  golden updates across the four dialect example modules. The manifest
  and the `.graphqls` files agree on table / column / relationship
  descriptions after this lands.
- Unit tests over synthetic schemas covering every entity shape variant
  plus each extended-metadata branch.
- Golden updates in `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,graphql}/expected/graph/`
  for the GraphQL template fix — affected files are the per-table
  `*_gen.graphqls` description blocks only; resolver / walker / scalar
  artifacts are unchanged.
- No emission yet — builds the in-memory `Manifest` struct.

### 18.3 JSON emission + JSON Schema

- `cmd/sqlgen/manifest/emit_json.go` — deterministic JSON encoder.
- `cmd/sqlgen/manifest/schema/v1.json` — JSON Schema definition.
  Schema covers the extended metadata fields from 18.2 (`comment` at
  top-level and per-column, `indexes[]`, per-column `check` and
  `default_kind`, `features.events`, expanded `features.cache`,
  top-level `generation_config`, per-method `sql_bodies`).
- Schema-validation test on every example.
- Deterministic-emit test.
- **MCP-driven addenda (added 2026-05-15):** ensure both
  `generation_config` and `sql_bodies` are emitted with sorted keys for
  byte-stable output; schema includes both fields as required so
  validation catches drift (a manifest emitted without them is now
  invalid against schema v1).

### 18.4 Markdown emission

- `cmd/sqlgen/manifest/templates/{index,conventions,entity}.md.tmpl`.
- `cmd/sqlgen/manifest/emit_markdown.go`.
- Per-entity template renders the extended metadata: table comment
  becomes the section's intro paragraph; column comments appear in the
  Notes column of the column table; indexes get their own table;
  CHECK constraints get a "Validation" subsection; events / cache
  features render under the existing Features header.
- **MCP-driven addenda (added 2026-05-15):**
  - Per-method "Generated SQL" subsection in the entity markdown,
    sourced from `sql_bodies`. One fenced code block per dialect key
    (single-dialect packages: one block). Rendered as `sql`-fenced
    blocks for syntax highlighting in GitHub / mkdocs.
  - `_index.md` gets a "Features" line near the top sourced from
    `generation_config`, listing the enabled package-wide features as
    a comma-separated list (e.g., "Features: cache, events,
    soft_delete, pagination").
- Golden tests against a fixture entity set including the extended
  metadata.

### 18.5 Breadcrumb files + package doc-comment pointer

- `cmd/sqlgen/manifest/emit_breadcrumbs.go` — emits identical
  `CLAUDE.md` and `AGENTS.md` at the output package root per the
  `breadcrumbs.claude_md` / `breadcrumbs.agents_md` flags.
- `cmd/sqlgen/manifest/templates/breadcrumb.md.tmpl` — single template
  driving both files (single source of truth).
- Extend the existing per-file generated-header pass to inject the
  one-line manifest pointer into `models_gen.go`'s package doc comment
  per `breadcrumbs.package_doc`.
- Unit tests pinning content + the override / opt-out semantics
  (manifest off → no breadcrumbs; manifest on + `claude_md: false` →
  CLAUDE.md absent but AGENTS.md still emitted; flipping `package_doc`
  off removes the pointer line on next run).

### 18.6 Pipeline wiring + stale cleanup

- Hook the manifest stage into `cmd/sqlgen/gen/orchestrate.go`.
- Extend stale-file cleanup (PRD §23.8) to cover `manifest/*.md`, the
  breadcrumb files, and the doc-comment pointer line.
- Disabled-by-default regression test: confirms zero manifest /
  breadcrumb / doc-pointer artifacts when `manifest.enabled` is unset.

### 18.7 Runtime-embedded manifest + Go types package

- New runtime package `github.com/teandresmith/sqlgen/manifest` with
  the ~27 types per §5.9 (`Document`, `EntityIndex`, `Entity`, `Column`,
  `Relationship`, `Method`, `Filter`, `Sort`, `Features`, `PK`,
  `Conventions`, `Generator`, `Enum`, `Extra`, **`GenerationConfig`**
  added 2026-05-15, plus typed aliases `Layout` / `Dialect` /
  `EntityKind` / `RelationshipKind`). `Method` carries a
  `SQLBodies map[Dialect]string` field (added 2026-05-15). Stdlib-only.
- Sentinel errors `ErrEntityNotFound`, `ErrParseManifest`.
- `manifest.LoadInto` helper for the generated init pass to handle
  both single + per-entity layouts uniformly.
- Generated `manifest_embed_gen.go` in the output package with the
  three `Client` methods + `//go:embed` directives + init-time parser.
- `embed_in_client` config field (§3.1) + validation (§3.4).
- Unit tests pinning: method signatures, init-time parse success,
  layout-agnostic `ManifestEntity` lookup across single and per-entity
  fixtures, drift between `ManifestVersion()` and `Document.SchemaVersion`.
- Marshal round-trip test (`json.Marshal(c.Manifest())` produces
  content-equivalent JSON to the on-disk file).
- Binary-size regression test on a 100-entity fixture (expected: ~30KB
  embed; total package binary growth <50KB including init code).

### 18.8 CLI subcommands (`sqlgen manifest validate` + `diff`)

- `cmd/sqlgen/cli/cmd_manifest.go` — new `manifest` parent command
  registering two subcommands.
- `sqlgen manifest validate <path>` — loads the file, validates against
  `cmd/sqlgen/manifest/schema/v1.json` (test-time dep
  `github.com/santhosh-tekuri/jsonschema/v5` promoted to CLI dep — same
  validator used by 18.3's tests). Prints summary
  (`schema_version`, entity / enum / extra counts) + result. Exit code
  0 on success, 1 on schema failure, 2 on I/O failure.
- `sqlgen manifest diff <old> <new>` — loads both manifests, walks
  entities / columns / methods / sentinels / features, prints structured
  diff (added / removed / changed). Output format:
  - Default: human-readable grouped by entity.
  - `--json` flag: machine-readable diff for CI scripts.
  - Exit code 0 if no diff, 1 if any diff (lets CI gate on
    "no schema changes" or "expected schema changes").
- Unit tests covering both subcommands across realistic schema-evolution
  cases (column added/removed, entity added/removed, method signature
  change, sentinel set change, feature toggled).
- README / docs update under `cmd/sqlgen/cli/README.md` (or the
  existing CLI docs landing page) listing the new commands.

### 18.9 E2E example surface across all dialects

- Extend `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,graphql}/`
  with manifest opt-in.
- Regenerate `expected/manifest/` goldens + `expected/CLAUDE.md` +
  `expected/AGENTS.md`.
- Add `tests/manifest_test.go` per example covering both file-on-disk
  consumption and the runtime `Client.Manifest()` / `ManifestEntity()`
  surface.
- Schema fixtures include `COMMENT ON TABLE` + `COMMENT ON COLUMN` +
  CHECK constraints + composite / partial indexes so the extended
  metadata is exercised end-to-end (per dialect — PG / MySQL native
  comments; SQLite adjacent `--` capture).

### 18.10 Phase closure sweep + PRD sync

- `make check` + `make check-examples` + `make test-integration` clean
  under `-race`.
- Land PRD §30 per §9 plan.
- Update `STATUS.md` Current Focus.
- Tag JSON Schema v1.0.0; freeze.

---

## 11. Open questions

All v1 design questions are now resolved. Each landed in the Resolved
subsection below with rationale and section references. This list will
re-populate if new questions surface during Phase 18 implementation; an
empty list is the gating signal for the PRD sync (§9).

### Resolved (logged here so future readers see the trail)

- **MCP-driven schema addenda: `generation_config` (top-level) +
  per-method `sql_bodies` (2026-05-15).** Phase 19's MCP scope
  expansion (see `docs/design/MCP.md` §10 "Scope expansion") requires two
  manifest fields that PRD §30 didn't originally spec:
  `sqlgen://config` consumes `generation_config` (§5.1.1) and
  `sqlgen_show_sql` consumes `methods[].sql_bodies` (§5.3). Both are
  pure additive fields — no removals, no renames, no shape changes to
  existing fields. Landing them in Phase 18 sub-items 18.2 (builder
  extraction) + 18.3 (JSON emission + schema) + 18.4 (markdown
  rendering) + 18.7 (runtime Go types) keeps Phase 19 unblocked
  without a schema bump. Per §5.5 versioning rules, the additions are
  v1.0.0-compatible (additive); they slot in before the v1.0.0 freeze
  at 18.10 closure. PRD §30 receives the matching additions in the
  Phase 18 sub-item work as a pre-sync amendment.

- **Agent breadcrumb files in scope (#1 / #2 from the agent-enforcement
  thread).** Confirmed 2026-05-15. Phase 18 emits `CLAUDE.md` /
  `AGENTS.md` at the output package root + extends the existing
  `models_gen.go` package doc comment with a one-line manifest pointer.
  Override-friendly: each piece can be toggled off via
  `manifest.breadcrumbs.{claude_md,agents_md,package_doc}`. See §2,
  §3.1, §4.6, §4.7, §7.2, and §10 sub-item 18.5.
- **MCP server (#4) committed as Phase 19 follow-on.** Reframed
  2026-05-15 (initially leaned v2 deferred, lifted in the same session
  on the wrapper-package analysis). The wrapper-package case — consumer
  hides the sqlgen-generated package behind their own wrapper, agent
  works at the wrapper layer and never reads inside `<output.dir>/` — is
  a common pattern, not a corner. Breadcrumbs are file-location-gated
  and don't fire there. MCP decouples tool discoverability from
  filesystem location: tools register in the agent's environment, so
  they surface regardless of where the agent is reading. Phase 18 freezes
  the JSON contract Phase 19 pins against; sequencing keeps each phase
  shippable. Full design at `docs/design/MCP.md`.
- **JSON layout configurable (single vs per-entity) — was #1.**
  Resolved 2026-05-15. Originally leaned "single file in v1, add the
  split as a v2 format option." Lifted to v1-configurable on the
  argument that consumers know their own scale: 100+ table packages
  benefit from per-entity files (one entity fetch ≈ 1k tokens vs the
  full ~30k single file), small packages prefer the one-file simplicity.
  New config: `json_layout: single | per_entity` (default `single`),
  plus `json_per_entity_dir` controlling the per-entity subdirectory.
  Top-level shape gains a `layout` field for explicit signaling and
  `entities[]` becomes either inline (single) or pointer-only (per
  entity, with `file` field). `enums[]` / `extras[]` stay inline in
  both layouts. MCP server resolves both layouts transparently. See
  §3.1, §3.2, §3.4, §4.1, §5.1.
- **Views share `entities[]` with `kind` discriminator — was #2.**
  Resolved 2026-05-15. Confirmed the original lean: one array with
  `kind: "table" | "view"`. Keeps consumer iteration code simple (one
  loop with a discriminator field), matches the existing `Manifest`
  struct shape in §5.3, no separate `views[]` array needed. View
  shape per §5.4 already specifies the differences (`pk.kind: "none"`,
  empty `relationships[]`, `source` field).
- **Hooks per entity omitted — was #3.** Resolved 2026-05-15. Hooks
  are registered at runtime via consumer code, not derivable from
  schema or config. Including them would require a static-analysis
  pass over the consumer's codebase (out of scope for the manifest).
  `_conventions.md` references PRD §21 so agents know hooks exist and
  where to look in code; per-entity entries do not enumerate them.
- **Per-entity markdown filename = SQL table name verbatim, matching
  the existing `<prefix>_gen.go` file prefix (was original #1).**
  Resolved 2026-05-15. Originally leaned `snake_case_singular` matching
  the Go struct (`user.md`). Lifted to table-name verbatim (`users.md`)
  on three arguments: (1) no singularization-pipeline edge cases can
  create filename ↔ table skew (irregular plurals, non-English schemas,
  consumer `naming.singularize: false` overrides all bypassed because
  the manifest reuses sqlgen's existing `_gen.go` prefix, not the
  struct-naming pipeline); (2) consistency with `_index.md`'s "Table"
  column. **Amended by FIX-163 (2026-08-28):** the second half of that
  rationale — a "schema-first mental model" where an agent reading
  `CREATE TABLE users` looks up `manifest/users.md` — never held. The
  `_gen.go` prefix is singular, so the file has always been the singular
  form; the builder's private prefix function only appeared to honour
  the verbatim reading because it was separately wrong. Prefix and Go
  file now come from one field (`TableContext.SnakeName`), which is the
  guarantee that was actually wanted: `manifest/<x>.md` sits beside
  `<x>_gen.go` for every entity. An agent navigates from `_index.md`,
  which carries the SQL table name alongside the link. See §4.3 for the
  full spec.
- **Separate graph package: single manifest, no separate graph manifest
  (was original #1 after renumbering).** Resolved 2026-05-15. Initially
  leaned "each generated package gets its own manifest." Reversed on
  the observation that the graph package's schema is **already
  self-described by the generated `*_gen.graphqls` files** — emitting
  a JSON manifest for the same surface would be pure duplication. The
  graph-package questions that aren't in the `.graphqls` files
  (resolver scaffold pattern, walker shape, error sentinel mapping,
  middleware semantics) are project-invariant sqlgen knowledge that
  lives in PRD §26 / static docs, not per-project generated output.
  Decision: single manifest in the models package; graph package gets
  a breadcrumb pair (§4.6 "Separate graph-package variant") pointing at
  three sources — models manifest, sibling `*_gen.graphqls` files, and PRD
  §26 for cross-cutting concerns. No cross-package coupling; no second
  MCP server entry in `.mcp.json` (MCP.md §10 resolved in lockstep).
- **`generation.manifest.enabled` defaults to `false` (was #1).**
  Resolved 2026-05-15. Confirmed the original lean: manifest is opt-in.
  Adding manifest emission to an existing project is a deliberate act —
  consumers don't get N+2 unexpected files on the next regen. New
  `sqlgen init` does not enable the manifest either; consumers add the
  block when they want it. Revisit the default at v2 once the feature
  has consumer mileage.
- **Filter struct fields include full Go types + omittable/comparator
  walkthrough in conventions (was #2).** Resolved 2026-05-15. Filter
  field entries in per-entity JSON carry full Go types (e.g.,
  `*comparator.String`, `*comparator.Time`, `[]UserFilter`) so agents
  have type info without reading the source. `_conventions.md` is
  expanded (§6.2) with dedicated **Comparator** and **Omittable**
  sections — agents reading conventions once should have enough context
  to write correct filter / update / mutation code without consulting
  package source. JSON conventions block (§5.2) gains parallel
  structured fields under `comparator` and `omittable`.
- **`$schema` URL pinned to GitHub raw URL of the latest tagged release
  (was #3).** Resolved 2026-05-15. Chose Option (a): zero infra,
  immutable per-release, backwards-compatible URL shift if a hosted
  version becomes available. Format:
  `https://raw.githubusercontent.com/<owner>/sqlgen/v<version>/cmd/sqlgen/manifest/schema/v1.json`.
  Phase 18 generation substitutes sqlgen's own version into the
  emitted `$schema` value at build time. See §5.8.
- **Multi-dialect packages — no action (was #4).** Resolved 2026-05-15.
  Confirmed. PRD §4 already enforces one dialect per output package, so
  the manifest naturally follows. The `dialect` field in the manifest's
  top-level metadata makes this explicit for consumers.
- **`include_examples: false` strips per-entity examples only (was
  #5).** Resolved 2026-05-15. Confirmed the lean. Conventions examples
  in `_conventions.md` are load-bearing — they teach the comparator +
  omittable shape once. Per-entity examples (which can grow long under
  the #6 expansion) are the right target for the opt-out when
  consumers want lean per-entity JSON.
- **Per-entity examples expanded to multi-line happy-path snippets
  (was #6).** **Reversed** the original "keep one-line" lean.
  Resolved 2026-05-15. New shape: `examples.read` and `examples.write`
  are arrays of strings (one element per line, joined on `\n` when
  rendered to markdown). Each demonstrates context handling, error
  wrapping, sentinel switch via `errors.Is`, the `Find* returns (nil,
  nil)` convention, filter composition with multiple comparators, and
  the omittable interplay. Read example ~25 lines, write example ~17
  lines. Goal: agents writing typical CRUD code don't consult
  `testdata/examples/` unless they're solving an unusual case. See
  §5.3.
- **Backwards-compatibility policy doc deferred to v2 (was #7).**
  Resolved 2026-05-15. Confirmed the lean. PRD §30.5's versioning
  rules are sufficient for v1 (schema_version follows semver, breaking
  changes bump major, every breaking version preserves its JSON Schema
  at `schema/v<N>.json`). A dedicated deprecation policy doc waits
  until actual v2 work surfaces a need.
- **Package doc-comment pointer injected into `models_gen.go` only
  (was #8).** Resolved 2026-05-15. Confirmed Option (a). Go's `godoc`
  folds package doc comments across all files in the package, so
  injecting the manifest pointer once on `models_gen.go` (the
  generated file already carrying the `// Package <name>` comment)
  surfaces on `go doc <pkg>` regardless of which `*_gen.go` file the
  reader opens. Per-file repetition is unnecessary noise.
  `models_gen.go` is also the largest generated file, so any agent
  reading it for grep-driven exploration sees the pointer.
- **Runtime-embedded manifest returns typed structs, not `[]byte`
  (raised mid-session).** Resolved 2026-05-15. Initially proposed
  `[]byte` for API stability (raw JSON, consumer parses if they want
  structure). User pushed back: typed struct is more useful — consumers
  know the shape without writing their own types. Reversed the lean.
  Three methods on the generated Client: `Manifest() *manifest.Document`,
  `ManifestEntity(table) (*manifest.Entity, error)` (layout-agnostic),
  and `ManifestVersion() string`. Types live in a new runtime package
  `github.com/teandresmith/sqlgen/manifest` (stdlib-only, peer to
  `database/` / `cache/`). ~25 named types mirror the JSON shape. Memory
  cost: parsed manifest persists for process lifetime (~200–500KB for
  100 entities). Opt-out via `manifest.embed_in_client: false` for
  binary-size-sensitive consumers (embedded systems, lambda cold
  starts). Type evolution follows the manifest schema_version
  (additive fields → minor; rename/remove → major; consumer updates
  sqlgen for major bumps). Phase 18 sub-item 18.7 lands the
  implementation. See §4.8 (mechanics) and §5.9 (Go types).
- **GraphQL description-emission fix bundled into 18.2 (raised
  mid-session).** Resolved 2026-05-15. Pre-existing Phase 16 gap: the
  parser captures `Table.Comment` / `Column.Comment` end-to-end (PG
  `pg_description`, MySQL `information_schema`, file-parser tests), and
  the GraphQL schema templates emit `"""description"""` blocks, but the
  two are not wired together — templates emit placeholder strings
  ("X corresponds to the Y table", `"<column_name>"`) instead of the
  parser-captured comments. Since 18.2's builder work already touches
  the same parser fields to populate `Manifest.Entity.Comment` /
  `Manifest.Column.Comment`, the GraphQL template fix is co-located:
  read the parser comment when non-empty, fall back to the existing
  placeholder otherwise. ~10 LOC template change + golden updates
  across the four example dialects. Side benefit: manifest and
  `.graphqls` files agree on descriptions after this lands. Considered
  filing as a `/fix` instead; bundling keeps the work / tests / sweep
  in one commit. See §10 sub-item 18.2.
- **Phase 18 nice-to-haves: extended metadata + CLI tooling (raised
  mid-session).** Resolved 2026-05-15. Seven additions accepted as a
  bundle, all low-cost and high-signal for agent consumption.
  **Schema-derived metadata:** (1) `COMMENT ON TABLE` / `COMMENT ON
  COLUMN` captured into top-level + per-column `comment` fields —
  replaces "agent has to guess domain semantics" with explicit
  schema-derived signals; (2) full `indexes[]` array per entity
  (UNIQUE, non-unique, partial WHERE, GIN/GIST), not just the unique
  ones — gives query-performance
  shape; (3) per-column `check` (raw CHECK-constraint expression) +
  `default_kind` taxonomy (`literal` / `function` / `expression`) for
  validation logic and "you don't have to provide this" hinting.
  **Feature-derived metadata:** (4) `features.events.{enabled, types[],
  payload_shape}` when events are enabled — closes the runtime-only
  event-types gap; (5) `features.cache.{key_pattern, invalidates_on}`
  when cache is configured — makes invalidation scope legible.
  **CLI tooling:** (6) `sqlgen manifest validate <path>` runs JSON
  Schema validation, CI-friendly exit codes; (7) `sqlgen manifest
  diff <old> <new>` structured schema-evolution diff for PR review,
  supports `--json` for machine consumption + CI exit-code gating.
  All seven added to Phase 18 sub-items 18.2 (builder), 18.3 (JSON +
  schema), 18.4 (markdown rendering), 18.8 (new CLI sub-item), 18.9
  (E2E coverage). PRD sync gains §30.7 + §30.8. See §5.3 extended-
  metadata table for the field-level reference.

---

## 12. Carry-over notes

- This document is intended to live alongside `CACHE.md`, `TENANCY.md`,
  `docs/design/archive/GRAPHQL.md` as a feature design supplement, retained after PRD sync.
- Once the open questions in §11 close and the JSON shape in §5 freezes,
  the PRD sync (§9) runs and Phase 18 work breaks down per §10.
- Sub-item tasks land in `docs/tracker/phase-18.md` only after the
  `/phase 18` command is invoked, which won't happen until §11 closes.
- No `docs/tracker/IMPLEMENTATION_ORDER.md` Phase 18 stub is written here. That
  edit lands alongside `/phase 18` to keep the two in sync.
