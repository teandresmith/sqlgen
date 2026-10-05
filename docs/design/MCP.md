# SQLGen MCP — Design & Plan

> **Status: SYNCED — superseded by PRD §31 (2026-07-10).** Sibling to
> `docs/design/MANIFEST.md`. This document captures Phase 19's design: an MCP
> (Model Context Protocol) server that consumes the manifest emitted by
> Phase 18 and exposes it to AI agents as tool calls + resources. §10
> open questions resolved 2026-05-15 (0 open, 13 resolved). The normative
> public contract now lives in `docs/PRD.md` §31 (landed at 19.7 closure,
> 2026-07-10); this document remains the design supplement (motivation,
> implementation notes, §3.2 agent-integration walkthrough). Note: Phase 19
> shipped against manifest schema `0.1.0` — the v1.0.0 freeze named below
> as a prerequisite was deferred by user direction (2026-07-10) and remains
> a parallel track; see PRD §30.5 / §31.5.
>
> **Pattern:** parallels `docs/design/CACHE.md` (PRD §27), `docs/design/TENANCY.md` (PRD
> §29), `docs/design/MANIFEST.md` (PRD §30 — synced).
>
> **Phase:** 19 (not yet broken down — see §9 for the preview). `/phase 19`
> runs after Phase 18 closes and freezes manifest schema v1.0.0.
>
> **Hard dependency:** Phase 18 manifest schema v1.0.0 must be frozen before
> Phase 19 tools can pin to its shape. Phase 19 does not start until Phase 18
> closure.

---

## 1. Motivation

Phase 18 ships the manifest as **static files** (`manifest_gen.json` +
`manifest/*.md` + `CLAUDE.md` / `AGENTS.md` breadcrumbs). That design has a
known structural limitation, surfaced in the design thread:

**The wrapper-package case.** Consumers commonly wrap the sqlgen-generated
package behind their own utility / repository layer:

```
myproject/
├── internal/
│   ├── models/          ← sqlgen-generated (manifest + breadcrumbs live here)
│   │   ├── CLAUDE.md
│   │   ├── AGENTS.md
│   │   ├── manifest/...
│   │   └── *_gen.go
│   └── repository/      ← consumer's wrapper
│       └── user.go      ← imports models, exposes Repository API
└── cmd/server/
    └── main.go          ← imports internal/repository, never touches internal/models
```

An agent editing `cmd/server/main.go` or `internal/repository/user.go`
**never enters `internal/models/`** — so Claude Code's on-demand
breadcrumb trigger does not fire. The manifest exists, the agent doesn't
know.

The case is **hardest** for cross-module wrapper libraries (a consumer
publishes a `myorg/db-utils` Go module that wraps sqlgen internally) —
downstream consumers' agents see only the wrapper's API, never the
generated package, and there is **nothing sqlgen can write into the
consumer's tree** that will surface to them. As §1.1 makes explicit, this
is the one case MCP does *not* cleanly solve either.

### 1.1 What MCP delivers — and where it doesn't

MCP does not eliminate the need for someone to know about the manifest; it
**relocates** that knowledge. File / breadcrumb discovery puts it on the
*agent, at work-time, via proximity* — which fails silently in the
wrapper case above. MCP puts it on the *human, at setup-time, once* —
after which the tools sit in the agent's tool list regardless of where it
reads. That trade buys three concrete things:

1. **Registration collapses to a one-time, automatable, committable act.**
   With `manifest.mcp.emit_project_config: true` (§3.4), sqlgen writes
   `.mcp.json` at the module root; the consumer commits it; teammates
   inherit it on `git clone`; Claude Code / Cursor / Windsurf
   auto-discover it. The human authors nothing and "knows" exactly once —
   versus breadcrumb discovery, which is per-agent, per-location, and
   fails *silently* when the agent works outside the models subtree.

2. **Location-independence within the repo.** Once registered, the tools
   work anywhere in the tree:
   - Agent in `cmd/server/main.go` → `sqlgen_get_entity(User)`.
   - Agent in `internal/repository/user.go` → `sqlgen_find_method(UpdateWhere)`.

   Breadcrumbs structurally cannot guarantee this.

3. **A precision interface over a static blob.** Typo-tolerant lookups,
   join pathfinding, SQL inspection, and *targeted* per-entity queries
   instead of dumping a 500 KB manifest into context. This value holds
   even when discovery is not the problem — and it is the feature's true
   differentiator (see below).

**Scope honesty — the cross-module case.** MCP's registration automation
is **repo-local**: the emitted `.mcp.json` lives in the *generating*
module's tree. A downstream consumer of a published wrapper library never
receives it — the manifest sits in that consumer's module cache
(`$GOMODCACHE/.../db-utils@v<ver>/…`), and pointing an MCP server at that
version-pinned path is a manual step that only works if the library ships
the manifest JSON in its published module. So for the cross-module wrapper
library — the hardest case in §1 — **the consumer still has to know about
the server and hand-wire it**; MCP is no better than the files there, and
carries a running process besides. v1 documents this as a known
limitation rather than claiming a structural fix.

**Why not a cheaper discoverability fix?** Much of the *same-repo*
discoverability win is also reachable by injecting a one-line manifest
pointer into the repo-**root** `CLAUDE.md` (always loaded, no process, no
registration) rather than only the models-dir breadcrumb. That closes the
wrapper gap for free but leaves the agent reading static files — too heavy
for large manifests. MCP's justification is therefore **the query
interface, not discoverability per se**: it earns its runtime cost
specifically for **large, same-repo packages behind a wrapper layer**,
where breadcrumbs miss *and* the full manifest is too big to inline. For
small packages, or packages the agent edits directly, the files
(optionally plus a root-`CLAUDE.md` pointer) are the better-value option.

The cost is **runtime infrastructure**: a per-session stdio process the
consumer registers per agent tool. Phase 18 was deliberately zero
processes; Phase 19 accepts that cost only where the precision interface
pays for it.

### 1.2 Why now (Phase 19) instead of Phase 18

The MCP server is a **consumer** of the manifest. Sequencing it after
Phase 18 lets:

- The JSON manifest schema v1.0.0 freeze before MCP tools pin to its
  shape. Building both in parallel risks JSON churn under MCP
  implementation.
- Phase 18 stay shippable as a focused 8-sub-item deliverable.
- Phase 19 absorb additional discoverability-layer work (deferred
  `sqlgen lint` rule for missing breadcrumbs, optional ad-hoc CLI
  `sqlgen manifest query`) as a coherent ship.

A close-coupled near-term phase, not an indefinite v2.

---

## 2. Scope

**In scope (Phase 19):**

- New CLI subcommand: `sqlgen mcp serve`.
- Stdio transport (JSON-RPC 2.0). Default for all major agent tools.
- Streamable HTTP transport, stateless, **loopback-bound only**
  (`127.0.0.1:<port>` via `--http <port>`). Covers cloud-dev-environment
  agents (Codespaces, devcontainers) that can't spawn local
  subprocesses. No auth — consumers needing remote / multi-tenant access
  put their own auth proxy in front. See §10 #4 for the full rationale.
- Read-only tool surface over a single manifest (11 tools):
  - **Discovery / lookup:** `sqlgen_list_entities`, `sqlgen_get_entity`,
    `sqlgen_find_method`, `sqlgen_find_referencing`,
    `sqlgen_describe_relationship`, `sqlgen_get_conventions`,
    `sqlgen_get_example`.
  - **Pathfinding / SQL inspection:** `sqlgen_find_join_path`,
    `sqlgen_show_sql`.
  - **Diagnostics:** `sqlgen_health`, `sqlgen_validate_manifest`.
- URI-addressable resources (4):
  - `sqlgen://manifest`
  - `sqlgen://entity/<name>`
  - `sqlgen://conventions`
  - `sqlgen://config`
- MCP `prompts` capability with 3 canned templates for common agent
  workflows (`sqlgen-write-query`, `sqlgen-add-relationship-usage`,
  `sqlgen-debug-not-found`). §4.3.
- Typo-tolerant lookups: `ENTITY_NOT_FOUND` /
  `RELATIONSHIP_NOT_FOUND` errors carry `suggestions[]` for fuzzy
  matches so agents can self-correct on the next turn. §5.3.
- Watch mode (`fsnotify` on the manifest file) — reload on regen.
- **Project-local `.mcp.json` emission at the module root** with the
  sqlgen MCP server entry. Auto-discovered by Claude Code / Cursor /
  Windsurf on project open; flips the registration ceremony from
  "consumer edits config" to "consumer commits a file." Merge-safe via
  sentinel marker (preserves other MCP servers consumers have
  registered). Opt-in via `manifest.mcp.emit_project_config` (Phase 19
  config block; see §3.4 for full spec, §6.6 for merge implementation).
- Agent-integration documentation in `docs/design/MCP.md` for the four major
  agent clients: Claude Code, Claude Desktop, Cursor, Windsurf.
- E2E test against a real MCP client (Claude Code subprocess in
  integration tests, parallel to Phase 16's gqlgen-subprocess pattern).
- Versioning policy aligned with the manifest's `schema_version`.

**Out of scope (Phase 19, deferred):**

- **Stateful HTTP sessions, auth, beyond-loopback binding.** v1
  Streamable HTTP is stateless and loopback-only. Stateful sessions
  (needed for server-pushed `tools/list_changed` notifications), bearer
  / mTLS auth, and binding beyond `127.0.0.1` all stay deferred until
  remote / shared-tenancy use cases surface.
- **Single-process multi-manifest mode.** Each generated package
  registers its own MCP server process; agent clients namespace by
  server key (§3.3). Cleaner, less surface to debug, and proven to
  scale to ~5 packages per session. Collapsing N manifests into one
  process revisited only at monorepo scale (~20+ packages).
- **Write tools** (`regenerate`, `update_schema`). Out of scope — the
  manifest is read-only, the server stays read-only. Generation belongs
  to `sqlgen generate`.
- **Authentication.** Not meaningful for stdio (the OS handles process
  isolation). Comes back if HTTP transport ever lands.
- **Cross-language SDK.** sqlgen is Go-only; the MCP server happens to be
  in Go. Non-Go consumers can still register the server — they just
  can't link to it as a library.
- **User-global config patching** (e.g., `sqlgen mcp register
  claude-desktop` writing into `~/Library/Application
  Support/Claude/claude_desktop_config.json` or its per-OS equivalent).
  Project-local `.mcp.json` (§3.4) covers Claude Code / Cursor /
  Windsurf directly via their auto-discovery conventions; user-global
  config patching is the only remaining registration gap (Claude
  Desktop has no project-local convention). Adds per-OS path resolution
  surface that's better solved per-platform. Deferred to v2; document
  manual Claude Desktop registration in v1.

**Non-goals:**

- Replacing the manifest files. The files stay the source of truth
  consumers can `cat` / commit / diff. MCP is one consumption path.
- Database introspection at runtime. The server reads the manifest, not
  the database. Schema queries against a live DB belong to
  `sqlgen lint` / `sqlgen diff`.
- Replacing godoc, pkg.go.dev, or any other doc surface. Different
  audience, different shape.

---

## 3. Configuration

### 3.1 Server-side (CLI flags)

```
sqlgen mcp serve [flags]

  --manifest <path>     Path to manifest_gen.json. Default: walk up from
                        CWD looking for a manifest_gen.json file. Fails
                        with a clear error if not found.
  --stdio               Transport (default). JSON-RPC 2.0 over stdin/stdout.
  --http <port>         Enable Streamable HTTP transport (stateless,
                        loopback-bound). Binds to 127.0.0.1:<port> only;
                        binding to any other interface is refused. Can
                        coexist with stdio. Default: stdio only.
  --watch               Reload manifest on fsnotify change. Default: true.
  --no-watch            Disable watch mode (for daemon / production use).
  --log <path>          Log to file (default: stderr — stdout is JSON-RPC).
  --log-level <level>   debug | info | warn | error (default: info).
```

**HTTP transport caveat.** Stateless mode means the server cannot push
notifications to HTTP clients. On a watch-mode reload,
`tools/list_changed` is emitted only over stdio; HTTP clients must
refetch `tools/list` themselves on schema-change suspicion. Stateful
sessions (which would restore notification push) are deferred — see
§10 #4.

### 3.2 Agent-side registration

The stdio server is **agent-auto-spawned per session** — the agent client
launches `sqlgen mcp serve` itself from the registration entry and tears
it down with the session. Nobody starts the server manually; registration
is the whole setup.

The recommended path for every project-local client is the **emitted
`.mcp.json`** (§3.4): set `manifest.mcp.emit_project_config: true` (the
default when the manifest is enabled), run `sqlgen generate`, commit the
file. Everything below describes what that file does per client, and the
manual fallback for clients that cannot read it.

**Claude Code.** Auto-discovers `.mcp.json` at the project root on open.
With the emitted file committed, a fresh `git clone` + `claude` session
has the sqlgen tools with zero setup. Claude Code prompts once per
project to approve the server. Nothing to author by hand; the emitted
entry looks like:

```json
{
  "mcpServers": {
    "sqlgen": {
      "command": "sqlgen",
      "args": ["mcp", "serve",
               "--manifest", "./internal/models/manifest/manifest_gen.json"],
      "x-sqlgen-managed": true,
      "x-sqlgen-manifest": "./internal/models/manifest/manifest_gen.json",
      "x-sqlgen-generator-version": "0.42.0"
    }
  }
}
```

**Cursor.** Auto-discovers `.cursor/mcp.json` in the project. Point the
emission at it — or at both files when a team uses Claude Code *and*
Cursor (the dual-target pattern from §3.4):

```yaml
generation:
  manifest:
    mcp:
      project_configs: [.mcp.json, .cursor/mcp.json]
```

One generation run keeps both registrations in sync; each target gets the
same managed entry independently.

**Windsurf.** Uses a per-user `~/.codeium/windsurf/mcp_config.json`
(same `mcpServers` schema) rather than a project-local file — register
manually per the fallback below, copying the entry shape out of the
emitted `.mcp.json`.

**Claude Desktop** (`~/Library/Application Support/Claude/claude_desktop_config.json`
on macOS). No project-local discovery — the daemon runs outside any
project CWD, so use the manual fallback with **absolute paths**:

```json
{
  "mcpServers": {
    "sqlgen-myproject": {
      "command": "/usr/local/bin/sqlgen",
      "args": ["mcp", "serve",
               "--manifest", "/Users/me/myproject/internal/models/manifest/manifest_gen.json"]
    }
  }
}
```

#### Manual fallback (any client lacking project-local discovery)

For Claude Desktop, Windsurf, or any `mcpServers`-schema client without
project-local config discovery:

1. Find the client's MCP config file (per-user, locations above).
2. Add an entry under `mcpServers` with a project-unique key
   (`sqlgen-<project>`; see §3.3 for why the key must be unique).
3. `command`: the `sqlgen` binary — absolute path if the client's
   environment lacks your shell `PATH` (Claude Desktop does).
4. `args`: `["mcp", "serve", "--manifest", "<path to manifest_gen.json>"]`
   — absolute path for daemon-style clients, since they resolve relative
   paths against their own CWD, not the project.
5. Omit the `x-sqlgen-*` markers — they only matter inside files sqlgen
   manages; hand-authored entries are consumer-owned by definition.

#### Worked example: the postgres E2E example

The repo's own postgres example (`cmd/sqlgen/testdata/examples/postgres/`)
has `manifest.enabled: true`, so its generation run emits
`.mcp.json` at the example root:

```json
{
  "mcpServers": {
    "sqlgen": {
      "args": ["mcp", "serve", "--manifest", "./models/manifest/manifest_gen.json"],
      "command": "sqlgen",
      "x-sqlgen-managed": true,
      "x-sqlgen-manifest": "./models/manifest/manifest_gen.json",
      "x-sqlgen-generator-version": "dev"
    }
  }
}
```

Open the example directory in Claude Code (with `sqlgen` on `PATH`),
approve the server, and try three canned prompts:

1. *"Which entities does this package expose, and which of them are
   views?"* — the agent calls `sqlgen_list_entities`, no file reading.
2. *"Show me the SQL that runs when I call `UserClient.UpdateWhere`."*
   — `sqlgen_show_sql` returns the dialect-exact statement from
   `sql_bodies`.
3. *"How do I get from `Comment` to `Role` through the relationship
   graph?"* — `sqlgen_find_join_path` returns the hop sequence with FK
   columns.

#### Troubleshooting

| Symptom | Cause / fix |
|---|---|
| Server never appears in the client | `.mcp.json` not committed / not at the module root, or the client was open before the file existed — reload the project. Cursor reads `.cursor/mcp.json`, not `.mcp.json` (dual-target pattern above). |
| Client shows the server as failed at startup | Run the entry's command by hand: `sqlgen mcp serve --manifest <path>`. All diagnostics go to **stderr** (stdout is reserved for JSON-RPC); `--log-level debug` for more. A missing binary means `sqlgen` is not on the `PATH` the client launches with — use an absolute `command` path or a `command_template` wrapper (`direnv exec`, `nix-shell`, §3.4). |
| `MANIFEST_NOT_FOUND` (-32001) on startup | The `--manifest` path resolves against the client's launch CWD. Project-local clients launch at the module root, matching the emitted relative path; daemon clients need an absolute path (manual fallback above). Regenerate if the manifest was deleted. |
| `warning: found unmanaged 'sqlgen' entry` during generate | You hand-authored an entry under sqlgen's key before enabling emission. sqlgen registered as `sqlgen-2` instead of overwriting it. Either keep your entry (delete the `sqlgen-2` duplicate and set `emit_project_config: false`) or hand ownership to sqlgen by adding `"x-sqlgen-managed": true` to yours. |
| `.mcp.json` is gitignored | Emission still writes it (sqlgen does not read `.gitignore`), but teammates won't inherit the registration — un-ignore it if the point is a committed, shared registration. |
| Tools return stale schema data | Watch mode (default-on) reloads on manifest change; if the server runs with `--no-watch`, restart the session after regenerating. HTTP clients never get change notifications (§3.1) — refetch `tools/list`. |

#### Registration reach & limitations

Two boundaries of the emitted-registration model, documented here per
§1.1's scope-honesty commitment:

- **Cross-module wrapper libraries.** The emitted `.mcp.json` is
  **repo-local**: it registers the server for agents working in the
  *generating* module's tree. A downstream consumer of a published
  wrapper library inherits nothing — the manifest sits version-pinned in
  their `$GOMODCACHE`, and wiring a server at it is a manual step that
  only works if the library ships its manifest JSON in the published
  module. For that consumer, MCP has no discoverability advantage over
  the static files; this is a **documented limitation, not a solved
  case** (§1.1).
- **Binary availability & version skew.** A committed `.mcp.json`
  carries the registration but **not the `sqlgen` binary**. Teammates
  need `sqlgen` installed and launchable from the client's environment,
  and an older installed binary will happily serve a newer manifest (and
  vice versa) — the `x-sqlgen-generator-version` marker records what
  emitted the entry, but nothing enforces agreement at serve time. Pin
  the tool version the way the repo pins any other dev tool (tools.go /
  Makefile / nix), and prefer `command_template` when the binary needs
  environment wrapping.

### 3.3 Multi-project registration

Each project registers its own server with a unique key. Tool names are
prefixed with the server key, so two projects coexist without collision:

```json
{
  "mcpServers": {
    "sqlgen-project-a": { ... },
    "sqlgen-project-b": { ... }
  }
}
```

The agent sees `sqlgen-project-a__sqlgen_get_entity` and
`sqlgen-project-b__sqlgen_get_entity` as distinct tools (the prefix is
the agent client's responsibility, not the server's — server emits the
unprefixed name).

### 3.4 Project-local `.mcp.json` emission

Phase 19's load-bearing UX feature. When `manifest.mcp.emit_project_config:
true`, sqlgen writes (or updates in place) a `.mcp.json` at the **module
root** containing the sqlgen MCP server entry. Claude Code / Cursor /
Windsurf auto-discover this file on project open; the consumer commits
it; teammates inherit the registration on `git clone`.

**Config block (Phase 19; extends Phase 18's `manifest` namespace):**

```yaml
generation:
  manifest:
    enabled: true                          # Phase 18
    # ... breadcrumbs etc. from Phase 18 ...
    mcp:                                   # Phase 19 block
      emit_project_config: true            # default: true when manifest.enabled
      project_configs: [.mcp.json]         # default; list of paths relative to module root
      server_key: sqlgen                   # default; key under mcpServers
      command_template: ""                 # optional override; see "Customization" below
```

**File location.** Each path in `project_configs` is resolved relative
to the **module root** (the directory containing `go.mod`), not
`<output.dir>`. This matches the Claude Code / Cursor convention for
project-local MCP config and ensures the file is visible to agent
clients regardless of where the generated package sits in the tree.

**Multi-target emission.** `project_configs` is a list so a single
generation run can register the server in every project-local config
the consumer's tooling uses simultaneously (resolved 2026-05-15 per
§10 #12). Common shapes:

```yaml
# Claude Code only (default)
project_configs: [.mcp.json]

# Cursor only
project_configs: [.cursor/mcp.json]

# Both clients in one project
project_configs: [.mcp.json, .cursor/mcp.json]
```

The merge algorithm in §6.6 operates per file, so the list shape is a
straightforward loop over targets — every entry gets the same
sentinel-marker contract, the same upsert / remove semantics, and the
same canonical formatting. Each path may be `.gitignore`d
independently per the consumer's policy.

**Emitted entry shape:**

```json
{
  "mcpServers": {
    "sqlgen": {
      "command": "sqlgen",
      "args": ["mcp", "serve",
               "--manifest", "./internal/models/manifest/manifest_gen.json"],
      "x-sqlgen-managed": true,
      "x-sqlgen-manifest": "./internal/models/manifest/manifest_gen.json",
      "x-sqlgen-generator-version": "0.42.0"
    }
  }
}
```

The three `x-sqlgen-*` extension fields are the **sentinel-marker
contract**:

| Field | Role |
|---|---|
| `x-sqlgen-managed: true` | Distinguishes sqlgen-owned entries from consumer-authored ones with the same server key. |
| `x-sqlgen-manifest: <path>` | Manifest path this entry serves. Used as the merge match key and for stale-entry cleanup when `output.dir` moves. |
| `x-sqlgen-generator-version: <version>` | Informational — lets consumers see when the entry was last refreshed. |

Standard MCP clients ignore unknown `x-*` fields (per JSON-RPC 2.0
convention), so these don't affect runtime behavior. They are pure
metadata for sqlgen's merge logic.

**Merge contract** (full algorithm in §6.6):

1. Read `.mcp.json` if it exists, else create with our entry only.
2. Match by `x-sqlgen-managed: true` AND `x-sqlgen-manifest ==
   <current manifest path>` (absolute-path comparison).
3. Match found → update in place (preserves key + position).
4. No match → append a new entry under `mcpServers`.
5. **Other entries (Linear, GitHub, custom) are never touched.**

**Multi-package monorepo.** Each sqlgen-generated package has its own
manifest at a distinct path. The merge matches by `x-sqlgen-manifest`,
so multiple sqlgen entries with distinct keys (`sqlgen-models`,
`sqlgen-analytics`, etc.) coexist naturally in one `.mcp.json`.

**Opt-out / removal.** Setting `emit_project_config: false` causes
sqlgen to remove its managed entries from `.mcp.json` on next
generation. If `mcpServers` becomes empty and no other top-level fields
exist, the file is deleted entirely. Consumer-authored or other-tool
entries are always preserved.

**Edge cases:**

| Scenario | Behavior |
|---|---|
| No `.mcp.json` exists | Create with our entry only. |
| File exists but lacks `mcpServers` field | Add `mcpServers` with our entry; preserve other top-level fields. |
| Invalid JSON | Fail loudly with line/col; never overwrite silently. |
| Consumer has a `sqlgen` key with no `x-sqlgen-managed` marker | Treat as consumer-owned. Skip merge; pick a free key (e.g. `sqlgen-2`); emit warning. |
| Multi-sqlgen monorepo | Merge matches by `x-sqlgen-manifest`; distinct keys coexist. |
| `output.dir` moves between regenerations | Old entry's `x-sqlgen-manifest` no longer matches any current config → stale-cleanup removes it. |
| `emit_project_config` flipped off | Remove our entries; delete file if empty afterward. |
| Consumer manually edits the sqlgen entry's args | Overwritten on next regen (sqlgen owns the entry). Use `command_template` for legitimate customization. |
| `.mcp.json` is gitignored / readonly | Fail loudly with a clear message. |

**Customization via `command_template`.** Consumers occasionally need to
wrap the server binary (e.g., `direnv exec` to load project env,
`nix-shell` to provide it). `command_template` is an opt-out from the
default `sqlgen mcp serve --manifest <path>` shape; sqlgen still owns
the entry but uses the template verbatim for `command` + `args`. The
template substitutes `{{ manifest_path }}` so the path stays
sqlgen-managed. Example:

```yaml
mcp:
  command_template: "direnv exec . sqlgen mcp serve --manifest {{ manifest_path }}"
```

**Formatting.** sqlgen writes JSON in a canonical format (2-space
indent, alphabetically-sorted keys at every level). First run normalizes
the consumer's file if their formatting differed; thereafter diffs are
stable and content-only.

**Atomic write.** Write to `.mcp.json.tmp`, `fsync`, `rename`. Cheap
insurance against partial writes if sqlgen crashes mid-generation.

---

## 4. Tool & resource surface

### 4.1 Tools

| Tool | Input | Output | Use case |
|---|---|---|---|
| `sqlgen_list_entities` | `{kind?: "table" \| "view"}` | `[{name, table, pk_kind, features_summary}]` | "What's available in this package?" |
| `sqlgen_get_entity` | `{name: string, compact?: bool}` | Full entity JSON (columns, methods, relationships, filter, sort, example) | "Tell me about `User`" |
| `sqlgen_find_method` | `{query: string, fuzzy?: bool}` | `[{entity, method, signature}]` | "Which entity has `UpdateWhere`?" |
| `sqlgen_find_referencing` | `{table: string}` | `[{entity, relationship_name, kind, fk_column}]` | "What references `users`?" |
| `sqlgen_describe_relationship` | `{entity: string, relationship: string, compact?: bool}` | Join shape: FK col, junction (m2m), filter clause (sub-categorized), target entity | "How does `Post.Tags` resolve?" |
| `sqlgen_get_conventions` | `{}` | Conventions block (error sentinels, pagination, comparators, `CallOptions`) | "How do I handle a not-found?" |
| `sqlgen_get_example` | `{entity: string, op?: "read" \| "write", compact?: bool}` | Canonical Go snippet | "Show me how to call this" |
| `sqlgen_find_join_path` | `{from: string, to: string, max_hops?: int}` | `[{hops: int, path: [{entity, relationship}]}]` (≤5 paths, shortest first) | "How do I join `User` to `Comment`?" |
| `sqlgen_show_sql` | `{entity: string, method: string, dialect?: string}` | `{sql: string, params: [{name, type}], dialect: string}` | "What SQL does `GetMany` emit?" |
| `sqlgen_health` | `{}` | `{manifest_path, schema_version, generated_at, loaded_at, last_reload_at?, watch_enabled, sqlgen_version, ok: bool}` | "Is the server seeing fresh data?" |
| `sqlgen_validate_manifest` | `{path?: string}` | `{valid: bool, schema_version: string, errors: [{field, msg}]}` | "Does the loaded manifest still pass §30.8 validation?" |

All tools are **read-only, idempotent, side-effect-free**. Calling
`sqlgen_get_entity` twice returns the same answer (modulo watch-mode
reloads on schema change).

**`compact: bool` parameter (resolved 2026-05-15 per §10 #10).** Tools
that return a per-entity blob (`sqlgen_get_entity`,
`sqlgen_describe_relationship`, `sqlgen_get_example`) accept an
optional `compact` flag. When `true`, the response drops:

- Canonical Go example snippets (`example.read` / `example.write`).
- Long-form description fields (column descriptions, table comments,
  per-relationship prose).
- Per-column metadata not needed for code lookup (`check`,
  `default_kind`, full `indexes[]`).

Required identity (name, type, PK kind, FK columns, method signatures,
join shape, filter sub-categories) is always retained. Default is
`false` (verbose) — agents only opt into `compact: true` when the
default response would exhaust their context budget (typical trigger:
100+ column tables or full-graph descriptions).

**`sqlgen_find_join_path` (resolved 2026-05-15 per §10 #scope-expansion).**
BFS over the relationship graph from `from` to `to`. Each hop is a
named relationship (`hasOne` / `hasMany` / `belongsTo` / `manyToMany`).
`max_hops` defaults to 4 and is clamped to `[1, 6]` to bound search
cost. Returns up to 5 shortest paths in increasing-hop order; ties are
broken lexicographically by serialized path. Returns an empty list if
no path exists within `max_hops`. Self-loop and cycle-free traversal —
the same entity is never visited twice on one path.

**`sqlgen_show_sql`.** Looks up `entity.methods[method].sql_bodies` in
the manifest and returns the canonical SQL for the active dialect
(override via `dialect` param if the manifest carries multiple). Phase
18 addendum dependency: this field must be present in the manifest
schema. Error code `-32005 METHOD_SQL_UNAVAILABLE` if the manifest was
generated before the addendum landed (the field is absent rather than
empty). Useful for agents debugging "what does this method actually
emit?" without round-tripping to the codebase.

**`sqlgen_health`.** Pure diagnostic surface — no manifest data leaks.
`ok` is `false` if the manifest is currently in a failed-reload state
(watch detected a change but the new file didn't validate; server
continues serving the prior good manifest per §6.3). `last_reload_at`
is absent until the first watch-triggered reload. Cheap to call;
agents can poll it before relying on a long conversation's worth of
cached manifest data.

**`sqlgen_validate_manifest`.** Validates against the JSON Schema at
`cmd/sqlgen/manifest/schema/v1.json`. Default behavior (`path` omitted)
validates the currently-loaded manifest in-memory — no I/O. With
`path`, reads the file at the given location and validates without
swapping the server's loaded manifest (read-only check). Errors are
the same shape as the §30.8 CLI tool's output for parity.

### 4.2 Resources

| URI | Body | Mime |
|---|---|---|
| `sqlgen://manifest` | Full `manifest_gen.json` | `application/json` |
| `sqlgen://entity/<name>` | Per-entity JSON | `application/json` |
| `sqlgen://conventions` | Conventions block | `application/json` |
| `sqlgen://config` | Resolved generation-config block (features on/off, dialect, package, etc.) | `application/json` |

Resources are the "give me everything" escape hatch — agents can pull
the whole manifest if they want full context. Tools are the precision
instrument the agent reaches for first.

**`sqlgen://manifest` size warning (resolved 2026-05-15 per §10 #11).**
When the encoded manifest exceeds **256 KB**, the resource read
response includes a top-level `warning` field advising the consumer to
prefer targeted tool calls:

```json
{
  "uri": "sqlgen://manifest",
  "mimeType": "application/json",
  "text": "...",
  "warning": "manifest is 412 KB (above 256 KB threshold) — prefer sqlgen_list_entities + sqlgen_get_entity over reading the full resource"
}
```

The threshold is a heuristic, not an enforced limit; the full manifest
is always served. MCP resource reads have no inherent size cap, but
some agent clients impose one — the warning surfaces well before that
shows up as a truncation. Threshold is a constant in
`cmd/sqlgen/mcp/resources.go` and revisitable if real-world manifests
trend larger or smaller.

**`sqlgen://config` (resolved 2026-05-15 per §10 #scope-expansion).**
Surfaces the manifest's `generation_config` top-level field — the
resolved (post-precedence, secret-stripped) configuration block that
produced this generated package. Helps agents avoid suggesting code
paths the consumer disabled (e.g., don't propose `WithCache(...)` if
cache is off package-wide). Phase 18 addendum dependency: PRD §30.4
must add the `generation_config` field; until then this resource
returns an empty object with `warning: "generation_config field absent
— consumer generated with sqlgen < <version>"`.

### 4.3 Prompts

MCP `prompts` are parameterized templates an agent client surfaces in
its prompt-picker UI; selecting one expands into structured turns the
LLM consumes (typically with auto-attached tool calls or resource
reads). Resolved 2026-05-15 per §10 #scope-expansion — v1 ships 3:

| Prompt | Arguments | What it does |
|---|---|---|
| `sqlgen-write-query` | `entity: string`, `goal: string` | Loads `sqlgen_get_entity({entity, compact: false})` + `sqlgen_get_conventions({})` into context and asks the LLM to write a sqlgen query against `{entity}` that accomplishes `{goal}`. Output includes a generated Go snippet referencing real method names from the entity's manifest entry. |
| `sqlgen-add-relationship-usage` | `entity: string`, `relationship: string` | Loads `sqlgen_describe_relationship({entity, relationship})` + the entity's example, then walks the user through correctly preloading / joining / writing across the relationship, including pagination + filter caveats from the conventions block. |
| `sqlgen-debug-not-found` | `entity: string`, `method?: string` | Loads `sqlgen_get_entity({entity})` + the conventions block's error-sentinel section, then diagnoses why a `NotFound` (or analogous) error might come back from `{method}` (or any method on `{entity}` if `method` omitted), suggesting checks against tenancy filtering, soft-delete state, and PK shape. |

**Discoverability.** Agent clients that support MCP prompts (Claude
Code, Claude Desktop, Cursor) display these in their slash-command /
prompt-picker UI; users select one, fill the arguments, and the
expansion runs. Clients that don't support prompts ignore the
capability cleanly — no degradation of tools / resources.

**Implementation.** `cmd/sqlgen/mcp/prompts.go` defines the templates
as Go strings with `text/template` substitution for `{entity}`,
`{goal}`, etc. The server's prompt-resolve handler expands the
template, optionally injects pre-fetched tool / resource results, and
returns the structured message list per the MCP spec. No persistence;
each prompt expansion is stateless.

**Future templates.** v1 deliberately keeps the catalog small. Add more
only when a specific user workflow is repeatedly painful to compose
from raw tools.

### 4.4 Tool naming convention

All tool names are prefixed `sqlgen_*` so they're identifiable in the
agent's tool list even alongside other MCP servers. Resource URIs use
the `sqlgen://` scheme. Prompt names use kebab-case with a `sqlgen-`
prefix (e.g., `sqlgen-write-query`). All three conventions are
non-negotiable in v1 — they make sqlgen surfaces immediately
recognizable across mixed-tool agent environments.

---

## 5. Protocol

### 5.1 Standard: MCP via JSON-RPC 2.0

The Model Context Protocol is a JSON-RPC 2.0 surface (request /
response / notification) over a transport. Phase 19 ships two:

- **stdio** (default) — one request / response per line over
  `stdin` / `stdout`. Long-running subprocess spawned by the agent
  client. Supports server-pushed notifications.
- **Streamable HTTP** (opt-in via `--http <port>`) — single POST
  endpoint at `http://127.0.0.1:<port>/mcp`, **loopback-bound only**,
  **stateless** (each POST is independent, no session ID). Trade-off:
  server cannot push notifications, so `tools/list_changed` on
  watch-mode reload is suppressed for HTTP clients — they refetch
  `tools/list` themselves on schema-change suspicion. Auth, stateful
  sessions, and beyond-loopback binding stay deferred per §10 #4.

MCP defines the schema for capabilities negotiation, tool listing, tool
invocation, resource listing, resource reading, and prompts.

Phase 19 implements the **server side** of the MCP spec at the version
current at implementation start, using the official Go SDK
(`github.com/modelcontextprotocol/go-sdk`) per §10 #2. Version
negotiation happens at connection time; the server advertises three
supported capabilities: **tools, resources, prompts** (§4.1 / §4.2 /
§4.3). Subscriptions, sampling, and logging capabilities are not
advertised in v1.

### 5.2 Lifecycle

1. **Connect:** agent client spawns `sqlgen mcp serve` as a subprocess
   with stdio piped. Client sends `initialize` with its supported
   protocol version + capabilities.
2. **Initialize response:** server replies with its supported version +
   capabilities (tools, resources, prompts).
3. **List surfaces:** client calls `tools/list`, `resources/list`, and
   `prompts/list`. Server returns the §4 surface.
4. **Steady state:** client calls `tools/call`, `resources/read`, and
   `prompts/get` as needed. Server responds.
5. **Watch event:** if `--watch` is on and the manifest file changes,
   server emits `notifications/tools/list_changed` and
   `notifications/resources/list_changed`. Prompts do not change at
   runtime, so no `prompts/list_changed` is emitted. Client refetches
   tools / resources.
6. **Shutdown:** client sends `shutdown`; server flushes logs, exits.

### 5.3 Error shapes

All errors follow JSON-RPC 2.0's `{code, message, data}` envelope.
sqlgen-specific error codes (in the `-32000` to `-32099` reserved server
range):

| Code | Meaning |
|---|---|
| `-32001` | `MANIFEST_NOT_FOUND` — no manifest at the given / discovered path |
| `-32002` | `MANIFEST_INVALID` — fails JSON Schema validation |
| `-32003` | `ENTITY_NOT_FOUND` — `get_entity` / `describe_relationship` / `find_join_path` on unknown name |
| `-32004` | `RELATIONSHIP_NOT_FOUND` — relationship name not on the named entity |
| `-32005` | `METHOD_SQL_UNAVAILABLE` — `show_sql` called but manifest predates the `methods[].sql_bodies` addendum |
| `-32006` | `METHOD_NOT_FOUND` — `show_sql` on a method name not defined for the named entity |

Standard JSON-RPC errors (-32700 parse error, -32600 invalid request,
-32601 method not found, -32602 invalid params, -32603 internal error)
apply as-is.

**Typo-tolerant suggestions (resolved 2026-05-15 per §10
#scope-expansion).** `ENTITY_NOT_FOUND` (-32003), `RELATIONSHIP_NOT_FOUND`
(-32004), and `METHOD_NOT_FOUND` (-32006) errors include a
`suggestions: [string, ...]` array in the JSON-RPC `data` field when
fuzzy candidates exist. Matching rule: Levenshtein distance ≤ 2 against
the appropriate candidate set (all entities / relationships on the
named entity / methods on the named entity), case-insensitive, capped
at the top 3 closest matches, sorted by distance ascending then
lexicographically. Empty array if nothing within threshold. Example:

```json
{
  "code": -32003,
  "message": "entity not found: Usr",
  "data": { "suggestions": ["User", "Users"] }
}
```

Lets agents self-correct on the next turn instead of asking the user
to spell out the right name.

---

## 6. Implementation

### 6.1 Package layout

```
cmd/sqlgen/mcp/
├── server.go          # Lifecycle: initialize, shutdown, capability negotiation
├── stdio.go           # Stdio transport wiring (SDK transport adapter)
├── http.go            # Streamable HTTP transport (loopback-bound, stateless)
├── tools.go           # Tool registry + dispatch
├── tools_entity.go    # get_entity / list_entities / find_method
├── tools_graph.go     # find_referencing / describe_relationship / find_join_path
├── tools_sql.go       # show_sql
├── tools_meta.go      # get_conventions / get_example
├── tools_diag.go      # health / validate_manifest
├── resources.go       # Resource registry + read dispatch (manifest / entity / conventions / config)
├── prompts.go         # Prompt templates + expansion (write-query / add-relationship-usage / debug-not-found)
├── suggest.go         # Levenshtein-based fuzzy suggestions for not-found errors
├── watch.go           # fsnotify integration + reload semantics
├── store.go           # In-memory manifest cache (atomic swap on reload)
├── mergeconfig.go     # .mcp.json upsert / remove / canonical write
├── *_test.go          # Per-file unit coverage
└── integration/
    └── client_test.go # E2E test against a real MCP client subprocess
```

`cmd/sqlgen/mcp/` imports types from `cmd/sqlgen/manifest/` (the
`Manifest` struct + JSON shape) but does not import the emitter — it is
a **read-only consumer** of the JSON file on disk. Protocol-layer code
(JSON-RPC framing, capability handshake, version negotiation,
notification semantics) comes from the official Go SDK per §6.4; our
files own the sqlgen-specific surface (tools, resources, store, watch,
mergeconfig).

### 6.2 Server lifecycle

```go
// server.go (sketch)

type Server struct {
    store    *Store        // atomic-swap in-memory manifest
    tools    *ToolRegistry
    resources *ResourceRegistry
    watcher  *Watcher      // nil if --no-watch
    logger   *slog.Logger
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
    if err := s.handshake(in, out); err != nil { return err }
    for {
        msg, err := readMessage(in)
        if err != nil { return err }
        resp := s.dispatch(ctx, msg)
        if err := writeMessage(out, resp); err != nil { return err }
    }
}
```

Concurrency: one goroutine reads stdin, one goroutine writes stdout, one
goroutine watches the manifest file. Tool dispatch is synchronous per
request (cheap operations on an in-memory struct).

### 6.3 Manifest loader + watch

```go
// store.go (sketch)

type Store struct {
    mu       sync.RWMutex
    manifest *manifest.Manifest
}

func (s *Store) Load(path string) error {
    raw, err := os.ReadFile(path)
    if err != nil { return err }
    var m manifest.Manifest
    if err := json.Unmarshal(raw, &m); err != nil { return err }
    if err := manifest.ValidateAgainstSchema(raw); err != nil { return err }
    s.mu.Lock()
    s.manifest = &m
    s.mu.Unlock()
    return nil
}
```

Watch mode wraps `Load` in an fsnotify callback. On reload, the server
emits `notifications/tools/list_changed` so MCP-aware clients refresh
their tool list (handles the case where a regen added a new entity).

### 6.4 MCP library: official Go SDK

**Decision (2026-05-15, §10 #2): adopt
`github.com/modelcontextprotocol/go-sdk`** rather than hand-rolling
JSON-RPC 2.0 + MCP framing.

The SDK provides:

- JSON-RPC 2.0 framing for both stdio and Streamable HTTP transports.
- Capability handshake (`initialize` / `initialize_result`) and version
  negotiation.
- Server-pushed notifications (`tools/list_changed`,
  `resources/list_changed`) with correct stdio framing.
- Tool / resource registry primitives that map to the §4 surface.

This adds one dependency to `cmd/sqlgen/`, scoped to the MCP
subcommand only. The runtime (`./`) and parser (`parser/`) modules
remain untouched per the three-module architecture in CLAUDE.md.

**Stdlib-first posture caveat.** sqlgen's stdlib-first rule is
load-bearing for **runtime** code that consumers import. CLI / generator
code already pulls dependencies (e.g., `gqlgen`, `fsnotify`); the MCP
subcommand is in that same tier. The SDK's value — keeping us in sync
with protocol-spec revisions and saving ~300 LOC of error-prone framing
work — outweighs the dependency cost. Reverting to hand-rolled framing
remains an option if the SDK's API stability degrades.

**Version pin.** Phase 19.1 pins to the SDK release current at
implementation start; bumps go through the standard `go.mod` review.
SDK upgrades within the same MCP protocol major version do not change
the §4 tool surface and do not require consumer reregistration.

### 6.5 Determinism

Like the manifest itself, MCP responses must be deterministic given an
unchanged manifest. Implementation rules mirror Phase 18:

- All collections sorted lexicographically.
- No `range` over maps.
- No `time.Now()` in response bodies — only `generated_at` from the
  manifest itself (which the server passes through verbatim).

A `TestDispatch_Deterministic` test invokes each tool twice and asserts
byte-equal JSON-RPC responses.

### 6.6 `.mcp.json` merge implementation

Lives in `cmd/sqlgen/mcp/mergeconfig.go`. The JSON-level work has no
runtime-MCP dependency — it is a pure file editor over the
`mcpServers` map.

**Per-file operation, list-driven caller.** `Merge` takes a single
`ConfigPath` (one `.mcp.json` or `.cursor/mcp.json`). The pipeline
caller (§19.5) loops over `project_configs` from §3.4 and invokes
`Merge` once per target. Each target gets the same upsert / remove
semantics, the same sentinel-marker contract, and the same canonical
formatting independently — failure in one target does not roll back the
others, but the run reports all per-target results before exiting
non-zero.

Sketch:

```go
// mergeconfig.go

const (
    sentinelManaged          = "x-sqlgen-managed"
    sentinelManifestPath     = "x-sqlgen-manifest"
    sentinelGeneratorVersion = "x-sqlgen-generator-version"
)

type MergeOptions struct {
    ConfigPath      string  // .mcp.json path (relative to module root)
    ServerKey       string  // "sqlgen" by default
    ManifestPath    string  // absolute or relative; resolved & stored on the entry
    CommandTemplate string  // optional; rendered with {{ manifest_path }}
    Version         string  // current sqlgen version
    Action          MergeAction
}

type MergeAction int

const (
    MergeUpsert MergeAction = iota
    MergeRemove
)

func Merge(opts MergeOptions) error {
    raw, err := os.ReadFile(opts.ConfigPath)
    if errors.Is(err, fs.ErrNotExist) {
        if opts.Action == MergeRemove { return nil }
        return writeNewConfig(opts)
    }
    if err != nil { return err }

    var root map[string]any
    if err := json.Unmarshal(raw, &root); err != nil {
        return fmt.Errorf("parse %s: %w (refusing to overwrite invalid JSON)",
            opts.ConfigPath, err)
    }

    servers, _ := root["mcpServers"].(map[string]any)
    if servers == nil {
        servers = map[string]any{}
        root["mcpServers"] = servers
    }

    matchedKey := findMatchingEntry(servers, opts.ManifestPath)
    switch opts.Action {
    case MergeUpsert:
        if matchedKey == "" {
            checkUnmanagedCollision(servers, opts.ServerKey)
            matchedKey = pickFreeKey(servers, opts.ServerKey)
        }
        servers[matchedKey] = buildEntry(opts)
    case MergeRemove:
        for k, v := range servers {
            if e, ok := v.(map[string]any); ok && isSqlgenManaged(e, opts.ManifestPath) {
                delete(servers, k)
            }
        }
    }

    // If we removed our last entry and nothing else top-level exists, drop the file.
    if len(servers) == 0 && len(root) == 1 {
        return os.Remove(opts.ConfigPath)
    }
    return writeAtomic(opts.ConfigPath, root)
}

func findMatchingEntry(servers map[string]any, manifestPath string) string {
    abs, _ := filepath.Abs(manifestPath)
    for k, v := range servers {
        e, ok := v.(map[string]any)
        if !ok { continue }
        managed, _ := e[sentinelManaged].(bool)
        if !managed { continue }
        candidate, _ := e[sentinelManifestPath].(string)
        candAbs, _ := filepath.Abs(candidate)
        if candAbs == abs { return k }
    }
    return ""
}

func writeAtomic(path string, data map[string]any) error {
    body, err := json.MarshalIndent(canonicalize(data), "", "  ")
    if err != nil { return err }
    tmp := path + ".tmp"
    if err := os.WriteFile(tmp, append(body, '\n'), 0o644); err != nil { return err }
    return os.Rename(tmp, path)
}
```

**Canonicalization.** `canonicalize` recursively sorts every map's
keys alphabetically before marshaling, producing deterministic byte
output for any equivalent JSON tree. This is the source of the
"first run normalizes formatting" behavior — every subsequent run
emits the same bytes for the same content.

**Unmanaged-collision detection.** `checkUnmanagedCollision` scans
for an entry keyed `==` `opts.ServerKey` lacking `x-sqlgen-managed:
true`. If found, sqlgen emits a warning to stderr:

```
warning: found unmanaged 'sqlgen' entry in .mcp.json
  sqlgen will register as 'sqlgen-2' to avoid overwriting your entry
  to let sqlgen manage your existing entry, add `"x-sqlgen-managed": true`
```

…and `pickFreeKey` appends `-2`, `-3`, … until an unused key is found.

**`command_template` rendering.** When non-empty, the template runs
through `text/template` with `manifest_path` as the substitution
variable. The rendered string is split via shell-style tokenization
(no shell, just `strings.Fields` + quote handling). Result becomes
the entry's `command` + `args`.

**Read-only / gitignored detection.** `writeAtomic` returns clear
error text if the rename fails due to permission denial or the path is
on a read-only filesystem. Phase 19 unit tests cover both modes
(EACCES + read-only mount).

---

## 7. Testing strategy

### 7.1 Unit tests

- **Per-tool tests** (`tools_entity_test.go`, `tools_graph_test.go`,
  `tools_sql_test.go`, `tools_meta_test.go`, `tools_diag_test.go`) —
  table-driven over synthetic manifests covering single-PK /
  composite-PK / tenanted / soft-deleted / view / m2m cases. Pin
  response shape. `tools_graph_test.go` adds the `find_join_path` cycle
  / hop-limit cases (§19.3); `tools_sql_test.go` covers the absent-field
  fallback path; `tools_diag_test.go` covers health-state transitions
  (fresh load, failed reload, watch off).
- **`suggest_test.go`** — Levenshtein-≤2 fuzzy matching: distance
  threshold, case-insensitivity, top-3 cap, tie-break ordering,
  empty-result path.
- **`prompts_test.go`** — each of the 3 templates expands to the
  expected structured message sequence; missing-argument validation;
  unknown-prompt error path.
- **`stdio_test.go`** — JSON-RPC 2.0 framing edge cases over the SDK's
  stdio transport (partial reads, multi-message batches, malformed
  JSON, oversized payloads).
- **`http_test.go`** — Streamable HTTP transport: loopback-only bind
  enforcement (refuses `0.0.0.0` / non-loopback), stateless POST cycle,
  no-notification confirmation on watch reload, parallel-request
  handling.
- **`store_test.go`** — manifest validation, atomic swap under
  concurrent reads, schema-version mismatch handling.
- **`watch_test.go`** — fsnotify callback dedup, rename-vs-rewrite
  semantics (different editors emit different events), failed reload
  retains the previous good manifest.
- **`mergeconfig_test.go`** — table-driven over every edge case in
  §3.4:
  - No file → create with our entry only.
  - Valid file + add → upsert (entry appended).
  - Valid file + existing managed entry → in-place update (key + position preserved).
  - Existing unmanaged-collision → warn + pick free key (`sqlgen-2`).
  - Invalid JSON → error without overwrite.
  - Multi-package monorepo → coexisting entries matched by `x-sqlgen-manifest`.
  - `output.dir` moves → stale-entry cleanup.
  - `emit_project_config` flipped off → remove entries (file deleted if empty).
  - `command_template` rendering with `{{ manifest_path }}` substitution.
  - Read-only path → clear error.
  - Gitignored path → emission still works (we don't read .gitignore).
  - Canonical formatting normalization: first run reformats consumer's
    custom indentation; second run produces byte-identical output.
  - Atomic write: simulate crash mid-write (tmp file present, target
    untouched).

### 7.2 Integration tests

- **`integration/client_stdio_test.go`** — boots `sqlgen mcp serve` as
  a subprocess, drives JSON-RPC 2.0 against its stdio. Covers the full
  lifecycle: initialize → tools/list → tools/call (each of the 11
  tools) → resources/list → resources/read (each of the 4 URIs) →
  prompts/list → prompts/get (each of the 3 templates) → shutdown.
  Parallels Phase 16's gqlgen-subprocess pattern.
- **`integration/client_http_test.go`** — boots `sqlgen mcp serve
  --http <port>` as a subprocess, drives JSON-RPC 2.0 against the
  loopback HTTP endpoint. Covers the same lifecycle plus the
  no-notification-push contract (HTTP client must refetch after watch
  reload, not wait for `list_changed`).
- Watch-mode subtest: mutates the manifest file, asserts the stdio
  client receives `notifications/tools/list_changed` and re-queries
  reflect new data; asserts the HTTP client's next list call surfaces
  the change without notification.

### 7.3 Real-client smoke test

- **`integration/claude_code_smoke_test.go`** (gated, optional —
  requires `CLAUDE_CODE_BIN` env var) — spawns Claude Code in
  headless mode pointed at the server, runs canned prompts ("list
  entities", "describe User"), asserts tool calls are made and
  responses are surfaced to the LLM. Smoke-level confidence the MCP
  spec is implemented correctly end-to-end. Skipped by default; runs
  in CI only with the binary available.

### 7.4 Cross-dialect coverage

Each Phase 18 example module gets a Phase 19 leg:

- `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,graphql}/tests/mcp_test.go`
  — boots the server against the example's `manifest_gen.json`, runs
  each of the 11 tools, reads each of the 4 resources, expands each of
  the 3 prompts. Asserts response shape matches the example schema.
  Dialect-aware: `show_sql` returns the dialect-appropriate SQL body
  per example.

---

## 9. Phase 19 sub-item preview

Drafts that go through `/phase 19` once §10 closes and Phase 18 ships.

### 19.1 Server skeleton + transports + SDK adoption

- Add `github.com/modelcontextprotocol/go-sdk` to `cmd/sqlgen/go.mod`
  per §6.4. Pin to the release current at implementation start.
- `cmd/sqlgen/mcp/server.go` with handshake / dispatch / shutdown
  wired through the SDK's server primitives.
- `cmd/sqlgen/mcp/stdio.go` — stdio transport adapter.
- `cmd/sqlgen/mcp/http.go` — Streamable HTTP transport (stateless,
  loopback-bound). `--http <port>` binds to `127.0.0.1:<port>`; binding
  to any other interface is refused at flag-parse time. Notification
  push (`tools/list_changed`) is no-op on HTTP per §5.1.
- Unit tests covering: SDK handshake replay, transport selection by
  flag, HTTP loopback-only enforcement (refuses `0.0.0.0` /
  non-loopback addresses), graceful shutdown on `SIGINT`.

### 19.2 Manifest loader + watch mode

- `cmd/sqlgen/mcp/{store,watch}.go`.
- Atomic-swap reload semantics.
- Schema-validation on every reload (uses `cmd/sqlgen/manifest/schema/v1.json`).
- Failed-reload-retains-good-manifest test.

### 19.3 Tools implementation (the 11)

- `cmd/sqlgen/mcp/tools_*.go` per the §4.1 table:
  - `tools_entity.go` — `get_entity`, `list_entities`, `find_method`.
  - `tools_graph.go` — `find_referencing`, `describe_relationship`,
    `find_join_path`.
  - `tools_sql.go` — `show_sql` (requires Phase 18 manifest addendum
    `entity.methods[].sql_bodies`; emits `-32005
    METHOD_SQL_UNAVAILABLE` when absent).
  - `tools_meta.go` — `get_conventions`, `get_example`.
  - `tools_diag.go` — `health`, `validate_manifest`.
- `cmd/sqlgen/mcp/suggest.go` — Levenshtein-≤2 fuzzy suggestion helper;
  used by `-32003 ENTITY_NOT_FOUND` / `-32004 RELATIONSHIP_NOT_FOUND` /
  `-32006 METHOD_NOT_FOUND` to populate `data.suggestions[]` per §5.3.
- Per-tool unit tests covering single-PK / composite-PK / tenanted /
  view / m2m / soft-deleted fixtures.
- `find_join_path`-specific tests: 1-hop, 2-hop, 3-hop, no-path, cycle
  guard (no entity visited twice on one path), `max_hops` boundary
  clamping.
- `show_sql`-specific tests: dialect resolution, multi-dialect manifest
  case, absent-field error path.
- `suggest.go` tests: distance threshold, case-insensitivity, top-3
  cap, tie-breaking.
- Deterministic-dispatch test.

### 19.4 Resources + Prompts implementation

- `cmd/sqlgen/mcp/resources.go` exposing the four URIs:
  `sqlgen://manifest`, `sqlgen://entity/<name>`, `sqlgen://conventions`,
  `sqlgen://config`. `config` requires Phase 18 manifest addendum
  `generation_config` top-level field; falls back to empty object +
  warning when absent.
- Resource-read tests (per-URI shape + 256 KB warning on
  `sqlgen://manifest`).
- `notifications/resources/list_changed` on watch reload.
- `cmd/sqlgen/mcp/prompts.go` — the 3 §4.3 templates registered with
  the SDK's prompt registry; `text/template` substitution for
  `{entity}`, `{goal}`, `{relationship}`, `{method}` arguments.
- Prompt-expansion tests: each template produces the expected structured
  message sequence; missing-argument validation; unknown-prompt path.

### 19.5 `.mcp.json` emission + agent integration docs

- `cmd/sqlgen/mcp/mergeconfig.go` — full merge algorithm per §6.6:
  upsert / remove / unmanaged-collision detection / canonicalization /
  atomic write / `command_template` rendering. `Merge` operates on a
  single `ConfigPath` per call; the pipeline loops over
  `project_configs` (§3.4) and aggregates per-target results.
- `config.ManifestConfig.MCP` Phase 19 config block per §3.4
  (`emit_project_config`, `project_configs` (list), `server_key`,
  `command_template`). Resolution order mirrors Phase 18: table-level
  → global → default. Default `project_configs: [.mcp.json]`.
- Validation: error if `mcp.emit_project_config: true` and
  `manifest.enabled: false` (parallel rule to Phase 18's breadcrumb
  validation in MANIFEST.md §3.4). Validate each entry in
  `project_configs` is a path relative to the module root and does not
  escape it (`..` segments rejected).
- Pipeline integration: emit / remove each target in `project_configs`
  as the last step of the manifest stage (after the breadcrumb files),
  so each merge's inputs (manifest path + sqlgen version) are stable at
  write time. Failure in one target reports but does not abort
  subsequent targets; overall run exits non-zero if any target failed.
- Per-client registration walk-through (Claude Code, Claude Desktop,
  Cursor, Windsurf) in `docs/design/MCP.md` §3.2 + a "manual fallback" section
  for Claude Desktop and any client lacking project-local discovery.
  Document the dual-target pattern (`project_configs: [.mcp.json,
  .cursor/mcp.json]`) for projects using both Claude Code and Cursor.
- Worked example: register the server against the postgres E2E example,
  show three canned prompts an agent can run.
- Troubleshooting section (server logs to stderr, common path issues,
  unmanaged-collision warning, gitignored `.mcp.json`).
- Unit + e2e merge tests per §7.1's `mergeconfig_test.go`, including
  the multi-target case (two `project_configs` entries, both updated in
  one run; failure in one target does not block the other).

### 19.6 E2E surface across all dialects

- `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,graphql}/tests/mcp_test.go`.
- Real-client smoke test (gated, optional).
- Cross-cutting test: server survives a manifest regen mid-conversation.

### 19.7 Phase closure sweep + PRD sync

- `make check` + `make check-examples` + `make test-integration` clean
  under `-race`.
- Land PRD §31 per §8 plan.
- Update `STATUS.md` Current Focus.
- Tag MCP server version 1.0.0 aligned with manifest schema v1.

---

## 10. Open questions

**Status: 0 open, 13 resolved (2026-05-15).** Resolutions logged below.
Gating signal for PRD §31 sync per §8 (along with Phase 18 closure +
manifest schema v1.0.0 freeze).

### Resolved (logged here so future readers see the trail)

- **#1 Subcommand naming: `sqlgen mcp serve` (2026-05-15).**
  Protocol-first naming, leaves room for future non-manifest MCP
  surfaces (e.g., `sqlgen mcp lint` if `sqlgen lint` ever gets an MCP
  wrapper). Rejected alternative: `sqlgen manifest serve` (kept the
  manifest as conceptual root, but locks us in).
- **#2 JSON-RPC library: official Go SDK
  (`github.com/modelcontextprotocol/go-sdk`) (2026-05-15).** Adopt the
  SDK rather than hand-rolling JSON-RPC 2.0 + MCP framing. Reduces
  protocol-spec drift risk; saves us implementing version negotiation,
  capability handshake, and notification semantics from scratch. Adds
  one dependency to `cmd/sqlgen/`, scoped to the MCP subcommand only.
  This overrides the original `hand-roll v1` lean — SDK maturity at
  Phase 19 entry tipped the call. §6.4 updated; §19.1 pins to SDK.
- **#3 Watch-mode default: on by default (2026-05-15).** `--no-watch`
  for opt-out (daemon / production use). Default optimizes for dev UX
  where regen-after-schema-change is the common case.
- **#4 HTTP transport in v1: stdio + Streamable HTTP stateless,
  loopback-bound, no auth (2026-05-15).** Both transports ship in v1.
  stdio remains the default; Streamable HTTP is opt-in via
  `--http <port>` and binds to `127.0.0.1` only (no remote exposure).
  Stateless mode — each POST is independent, no session tracking.
  Trade-off: server-initiated notifications (`tools/list_changed` on
  watch reload) cannot be pushed; HTTP clients refetch `tools/list` on
  schema change. Covers the cloud-dev-environment case (Codespaces,
  devcontainers) where the agent runs in-browser and can't spawn local
  subprocesses; consumers needing remote / multi-tenant scenarios put
  their own auth / proxy in front of the loopback endpoint. Still
  deferred to v2: stateful sessions (for notification push), auth
  (bearer / mTLS), beyond-loopback binding. §2, §3.1, §5, §6.1 updated.
- **#5 Multi-manifest server in v1: multi-process is the v1 model;
  single-process multi-manifest deferred (2026-05-15).** Each generated
  package gets its own `sqlgen mcp serve` subprocess; agent clients
  namespace tool names by server key (e.g.,
  `sqlgen-models__sqlgen_get_entity` vs
  `sqlgen-analytics__sqlgen_get_entity`). No coordination required
  between servers — each has its own stdio pipe, in-memory store, and
  fsnotify watcher. Memory cost: ~10-50MB per process; comfortable up
  to ~5 packages per agent session. Single-process multi-manifest mode
  revisited only if monorepo-scale pain (~20+ packages) surfaces.
- **#6 Tool prefix: keep `sqlgen_*` (2026-05-15).** Discoverability in
  the tool list outweighs the redundancy with the agent client's
  server-key namespacing.
- **#7 Auto-registration helper for user-global configs: deferred to v2
  (2026-05-15).** Project-local `.mcp.json` (§3.4) covers Claude Code /
  Cursor / Windsurf via their auto-discovery conventions. Claude
  Desktop registration documented manually in v1 per §3.2. User-global
  helpers require per-OS path resolution (Linux / macOS / Windows
  differ) plus the same merge-safe semantics as `.mcp.json`; better
  solved per-platform when a real consumer hits the manual step.
- **#8 Long-running connection vs spawn-per-request: long-running
  (2026-05-15).** Standard MCP stdio pattern — one server process per
  agent session. Documented explicitly in §5.2 so consumers don't
  expect per-request lifecycle management.
- **#9 Server logging policy: stderr by default, no rotation,
  `--log <path>` for redirect (2026-05-15).** Stdout is reserved for
  JSON-RPC framing. Structured JSON logging deferred until a consumer
  hits debuggability pain that plain text doesn't resolve.
- **#10 Tool response token budget: `compact: bool` on per-entity tools
  (2026-05-15).** Tools that return a per-entity blob
  (`sqlgen_get_entity`, `sqlgen_describe_relationship`,
  `sqlgen_get_example`) accept an optional `compact: bool` parameter;
  when true, examples and verbose description fields are dropped to
  reduce agent context cost. Default `false` (verbose) preserves full
  fidelity unless explicitly trimmed. §4.1 updated.
- **#11 `sqlgen://manifest` resource size: emit warning above 256KB
  (2026-05-15).** Resource read responses include a `warning` field
  when the encoded manifest exceeds 256KB, advising the consumer to
  prefer targeted tool calls. §4.2 updated.
- **#12 Multi-target `.mcp.json` emission: lift to a list —
  `project_configs` (2026-05-15).** Config field changes from
  `project_config_path` (single) to `project_configs` (list); merge
  algorithm in §6.6 operates per file, so supporting N targets is
  cheap. Lets consumers project the registration into both `.mcp.json`
  (Claude Code) and `.cursor/mcp.json` (Cursor) in one config. §3.4 +
  §19.5 updated.
- **GraphQL `graph/` subpackage gets no separate MCP server (2026-05-15,
  pre-numbering trail).** Resolved alongside MANIFEST.md's
  same-decision. The graph package's schema is already self-described
  by the generated `*_gen.graphqls` files; no separate JSON manifest is
  emitted (per MANIFEST.md §11 Resolved); therefore no second MCP
  server registration is needed. Single `.mcp.json` entry pointing at
  the models manifest. Agents working in `graph/` reach for the
  `.graphqls` files directly and use the `sqlgen_get_entity` tool
  against the models manifest for Go-side entity detail. See
  MANIFEST.md §4.6 "Separate graph-package variant" for the
  graph-package breadcrumb spec.
- **Scope expansion: prompts capability + diagnostic surface +
  pathfinding + SQL inspection + config introspection (2026-05-15).**
  v1 grows from 7 tools / 3 resources to **11 tools / 4 resources /
  3 prompts** to better match agent UX needs:
  - **MCP `prompts` capability** with 3 templates
    (`sqlgen-write-query`, `sqlgen-add-relationship-usage`,
    `sqlgen-debug-not-found`). §4.3 + §5.1.
  - **Typo-tolerant entity/method lookup** —
    `ENTITY_NOT_FOUND` / `RELATIONSHIP_NOT_FOUND` errors include
    `suggestions: [...]` in the JSON-RPC `data` field (Levenshtein ≤ 2
    over the candidate set, capped at 3 suggestions). §5.3.
  - **`sqlgen_health` tool** — diagnostic state: manifest path,
    `schema_version`, `generated_at`, server load/reload times, watch
    state, sqlgen binary version. §4.1.
  - **`sqlgen_validate_manifest` tool** — surfaces PRD §30.8's
    validator over MCP for agent self-healing flows. §4.1.
  - **`sqlgen_find_join_path(from, to)` tool** — BFS pathfinding over
    the relationship graph; bounded `max_hops` (default 4); returns
    shortest paths first, capped at 5 results. §4.1.
  - **`sqlgen_show_sql(entity, method)` tool** — returns the generated
    SQL body + params + dialect for a method. **Phase 18 addendum
    required:** PRD §30.7 must add `entity.methods[].sql_bodies`
    (per-dialect map). Flagged in §11.
  - **`sqlgen://config` resource** — surfaces the resolved
    generation-config snapshot (which features are on: cache, tenancy,
    events, soft-delete, views, etc.). **Phase 18 addendum required:**
    PRD §30.4 must add a top-level `generation_config` field with the
    resolved (post-precedence, secret-stripped) configuration block.
    Flagged in §11.

  **Phase 18 addendum impact (F + G):** two manifest-schema additions
  before v1.0.0 freeze. Implementation slots into 18.3 (JSON emission)
  + 18.4 (markdown emission); 18.7 (runtime-embedded manifest) gets the
  matching Go types. No PRD §31 (MCP) field-shape changes — F and G are
  pure consumers of the new manifest fields.

---

## 11. Carry-over notes

- This document is intended to live alongside `MANIFEST.md`, `CACHE.md`,
  `TENANCY.md`, `docs/design/archive/GRAPHQL.md` as a feature design supplement, retained
  after PRD sync.
- **Hard prerequisite for Phase 19 start:** Phase 18 closure with
  manifest `schema_version: 1.0.0` frozen. Phase 19 tools pin to v1's
  field shapes; churn in v1 during Phase 19 implementation would force
  rework.
- **Phase 18 manifest-schema addenda for §10 scope expansion (F + G) —
  propagated 2026-05-15.** The two required manifest fields are now
  spec'd in all three source-of-truth docs:
  - `entity.methods[].sql_bodies: {<dialect>: string}` — per-dialect
    canonical SQL for each generated method. Consumed by
    `sqlgen_show_sql` (§4.1). **Spec'd in:** PRD.md §30.4.2 (example
    + prose) + §30.7 (extended-metadata table row); MANIFEST.md §5.3
    (example + dedicated subsection) + §5.9 (`Method.SQLBodies` Go
    field). **Implementation slot:** IMPLEMENTATION_ORDER.md 18.2
    (builder extraction from SQL-build pipeline) + 18.3 (JSON
    emission + schema) + 18.4 (markdown rendering as per-method
    Generated SQL block).
  - `generation_config: {...}` (top-level) — resolved
    (post-precedence, secret-stripped) feature-toggle snapshot.
    Consumed by `sqlgen://config` (§4.2). **Spec'd in:** PRD.md §30.4
    (top-level fields table) + §30.4.3 (new dedicated subsection) +
    §30.7 (extended-metadata table row); MANIFEST.md §5.1 (table) +
    §5.1.1 (new dedicated subsection) + §5.9 (`Document.GenerationConfig`
    + `GenerationConfig` Go struct). **Implementation slot:**
    IMPLEMENTATION_ORDER.md 18.2 (snapshot computation from resolved
    config) + 18.3 (JSON emission + schema, required field) + 18.4
    (markdown `_index.md` Features line) + 18.7 (Go type export).
  Both additions are pure schema growth (no rename / no removal), so
  they fit the §30.5 versioning rule for backward-compatible v1.x
  bumps if they miss the v1.0.0 freeze — but landing them in v1.0.0
  via the implementation slots above avoids gating Phase 19
  §10-expansion sub-items behind a schema bump.
- **MANIFEST.md §11.5 dependency cleared (2026-05-15).** The
  cross-package separate-graph-package question that was load-bearing for
  the graph-package MCP decision resolved in MANIFEST.md alongside its
  closure; the matching MCP decision is in §10's Resolved subsection
  (GraphQL `graph/` subpackage gets no separate MCP server). No further
  cross-doc dependency remains for Phase 19 §10 resolution.
- No `docs/tracker/IMPLEMENTATION_ORDER.md` Phase 19 stub is written here. That
  edit lands alongside `/phase 19`, after Phase 18 closes. §10 in this
  document has settled (2026-05-15); the remaining gating signal is
  Phase 18 closure + manifest schema v1.0.0 freeze.
- The reframing of MCP from "v2 deferred" to "Phase 19 committed" is
  logged in `MANIFEST.md` §11's Resolved subsection for the trail.
