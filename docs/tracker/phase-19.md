# Phase 19: MCP Server

Status: Complete (closed 2026-07-10 — all 7 sub-items complete, closure sweep green, 0 open FIXes)
PRD Sections: §31 (landed at 19.7; also §23.1 `mcp serve` row, §30 cross-refs, §30.2 `mcp` block, §4.13 rules, §8.3 artifact row, §23.8 exceptions note)
Design Supplement: `docs/design/MCP.md` (§2 Scope, §3 Configuration, §4 Surface, §5 Protocol, §6 Implementation, §7 Testing, §9 Sub-item preview)

> **What this phase builds.** An MCP (Model Context Protocol) server — `sqlgen mcp serve` — that consumes the Phase 18 manifest and exposes it to AI agents as **11 read-only tools + 4 resources + 3 prompts** over **stdio** (default, agent-auto-spawned) and **Streamable HTTP** (opt-in, loopback-bound, stateless). Plus project-local `.mcp.json` emission so registration is a committed file, not a config-editing ceremony. New package `cmd/sqlgen/mcp/`; new CLI subcommand; one new dependency (`github.com/modelcontextprotocol/go-sdk`) scoped to `cmd/sqlgen/` only. Runtime (`./`) and parser (`parser/`) modules stay untouched.
>
> **Honest value scope (per MCP.md §1.1, rewritten 2026-07-10).** MCP earns its runtime cost for **large, same-repo packages behind a wrapper layer**, where breadcrumbs silently miss *and* the full manifest is too big to inline — the differentiator is the **precision query interface**, not discoverability per se. The **cross-module wrapper library** case is a **documented limitation, not a structural fix**: the emitted `.mcp.json` is repo-local and does not reach a downstream consumer of a published library. 19.5's docs task must reflect this.

> ### ⚠️ Freeze-gate caveat — drafted against schema `0.1.0` (user decision, 2026-07-10)
>
> MCP.md §11 names a frozen manifest **schema v1.0.0** as Phase 19's hard prerequisite. That freeze is **deferred by user direction**; the schema ships at `0.1.0`. This phase was drafted to proceed **against `0.1.0`**, treating the freeze as a **parallel/external track**, with these consequences the runner must hold:
>
> - **Churn risk.** The deferred freeze pair (shape the always-emitted `conventions` sub-blocks `call_options` / `soft_delete` / `comparator` / `omittable` in `schema/v1.json` + tighten `conventions.required` + bump to `1.0.0`) may reshape exactly the fields **19.3** (`get_conventions`, `get_entity`) and **19.4** (`sqlgen://conventions`) read. Tools pin to the `0.1.0` shape; if the freeze lands mid-phase, re-verify those two sub-items against the new shape.
> - **Unblocked already.** Both hard-dependency addenda landed in Phase 18 — `entity.methods[].sql_bodies` (→ **19.3** `show_sql`) and top-level `generation_config` (→ **19.4** `sqlgen://config`). No addendum work remains in Phase 19.
> - **19.7 version tagging.** MCP.md §9's "tag MCP server 1.0.0 aligned with manifest schema v1" assumes the freeze happened. It hasn't — so at closure the MCP server version must align to whatever `schema_version` actually is (`0.1.0` unless the freeze runs first). Resolve at 19.7; do not silently stamp `1.0.0`.

---

## 19.1 Server skeleton + transports + SDK adoption

**PRD Reference:** MCP.md §2, §3.1, §5.1, §5.2, §6.1, §6.2, §6.4; §10 #2/#4/#9.

**Status:** Complete

### Tasks

- [x] Add `github.com/modelcontextprotocol/go-sdk` to `cmd/sqlgen/go.mod`; pin to the release current at implementation start (§6.4). Confirm it lands only in the CLI module. — pinned `v1.6.1` (latest stable; v1.7.0-pre.x are pre-releases). Runtime `./go.mod` + `parser/go.mod` byte-unchanged.
- [x] `cmd/sqlgen/mcp/server.go` — `Server` struct + `Serve(ctx, in, out)`; `initialize` handshake, dispatch loop, `shutdown` (log flush + exit) wired through the SDK's server primitives. — handshake/dispatch/notifications come from the SDK's `*mcp.Server` + `Run`; `Serve(ctx, transport)` + `ServeStdio(ctx, in, out)` split the io signature per transport.
- [x] Advertise exactly three capabilities at handshake: **tools, resources, prompts**. Do NOT advertise subscriptions / sampling / logging. — `advertisedCapabilities()` sets a non-nil `ServerCapabilities` (suppresses the SDK's historical default logging cap); tools/resources advertise `listChanged`, prompts do not.
- [x] `cmd/sqlgen/mcp/stdio.go` — stdio transport adapter (default). One reader goroutine, one writer goroutine. — via the SDK `IOTransport` (newline-delimited JSON-RPC over injectable reader/writer).
- [x] `cmd/sqlgen/mcp/http.go` — Streamable HTTP transport (stateless, loopback-bound). `--http <port>` binds `127.0.0.1:<port>` only; any non-loopback address (`0.0.0.0`, external IP, hostname) refused at flag-parse time. Notification push is a no-op on HTTP (§5.1). — `ResolveHTTPAddr` refuses non-loopback IPs and any hostname (incl. `localhost`); `StreamableHTTPOptions{Stateless: true}`; single `/mcp` endpoint.
- [x] `sqlgen mcp serve` CLI subcommand with flags per §3.1: `--manifest`, `--stdio` (default), `--http <port>`, `--watch` (default true) / `--no-watch`, `--log <path>`, `--log-level <debug|info|warn|error>` (default info). — `cli/cmd_mcp.go`; `--watch`/`--no-watch` marked mutually exclusive. `--manifest`/`--watch` threaded but consumed in 19.2.
- [x] `slog` logger to **stderr** by default (stdout reserved for JSON-RPC framing); `--log <path>` redirects. — `mcp.NewLogger(w, level)`; CLI opens the `--log` file (0600) and flushes/closes on exit.
- [x] Graceful shutdown on `SIGINT`. — `signal.NotifyContext(ctx, os.Interrupt)` cancels the run context; both transports tear down and the log file is flushed before exit.

### Acceptance Criteria

- `sqlgen mcp serve` boots stdio by default; the `initialize` response advertises exactly `tools` + `resources` + `prompts`, nothing else.
- `--http <port>` binds `127.0.0.1:<port>` and **refuses** any non-loopback bind at flag-parse with a clear error; stdio and HTTP can coexist in one process.
- Stdout carries only JSON-RPC; all logging goes to stderr (or `--log` target).
- `SIGINT` flushes logs and exits without a partial-write.
- The SDK dependency appears only in `cmd/sqlgen/go.mod`; `go.mod` for the runtime root and `parser/` are byte-unchanged.

### Tests Required

- [x] SDK handshake replay: `initialize` → capability set is exactly {tools, resources, prompts}. — `server_test.go::TestHandshakeAdvertisesExactlyThreeCapabilities` (in-memory transport pair; asserts tools+resources+prompts present, logging/completions/experimental/extensions absent, and the per-cap `listChanged`/`subscribe` contract).
- [x] Transport selection by flag (stdio default; `--http` enables HTTP; both together). — `run_test.go::TestRunGracefulShutdown` table (stdio-only / http-only / coexist) + `cmd_mcp_test.go::TestMCPServe_HelpListsFlags`.
- [x] HTTP loopback-only enforcement — refuses `0.0.0.0`, an external IP, and a hostname; accepts `127.0.0.1`. — `http_test.go::TestResolveHTTPAddr` (15 cases incl. wildcard v4/v6, external/public IP, hostname, `localhost`) + `cmd_mcp_test.go::TestMCPServe_RefusesNonLoopbackHTTP` (ExitConfig at flag-parse).
- [x] Graceful shutdown on `SIGINT`. — `server_test.go::TestServeStopsOnContextCancel` + `run_test.go::TestRunGracefulShutdown` (context cancellation is the SIGINT mechanism; HTTP port released after shutdown). Real-signal delivery is covered by the 19.6 subprocess integration test.

### Completion Record

**Landed (implementation, pre-`/verify`).** Files created: `cmd/sqlgen/mcp/{server,stdio,http,run}.go` + `{server,http,run}_test.go`; `cmd/sqlgen/cli/cmd_mcp.go` + `cmd_mcp_test.go`. Files modified: `cmd/sqlgen/cli/root.go` (register `mcp` command), `cmd/sqlgen/go.mod` + `go.sum` (add `go-sdk v1.6.1`), `go.work.sum`. SDK dependency scoped to the CLI module only. `make check` clean (0 lint issues, all unit tests green under `-race`); `make check-examples` clean (go.mod-trigger; example output byte-unchanged as expected — MCP is not in the generation path). NOTE: `--manifest`/`--watch` flags are threaded but only consumed once the Store + fsnotify watcher land in 19.2.

**Verified + completed (2026-07-10).** `/verify 19.1`: 7/7 PRD (MCP.md) requirements PASS, 4/4 required tests present, 0 guideline issues; `make check` + `make check-examples` re-run clean under `-race`. Independent `sqlgen-reviewer` pass: full agreement — no race in `Run()`'s `errCh`/`cancel` loop, no goroutine leak / double-close on HTTP shutdown, go-sdk confirmed out of the runtime (`./go.mod`) and parser (`parser/go.mod`) modules. One doc-comment nit fixed inline during verify: `http.go` `ServeHTTP` doc referenced a nonexistent `validateLoopbackAddr` → corrected to `ResolveHTTPAddr`. No FIX entries logged (all items PASS). Spec-neutral observation (no action): a genuine HTTP bind failure surfaces as `ExitGeneration` rather than `ExitConfig`.

---

## 19.2 Manifest loader + watch mode

**PRD Reference:** MCP.md §3.1 (discovery), §5.2 (watch event), §6.3, §6.5; §10 #3.

**Status:** Complete (2026-07-10)

### Tasks

- [x] `cmd/sqlgen/mcp/store.go` — `Store` with `sync.RWMutex` + `*manifest.Document` (runtime read-consumer type; the §6.3 sketch's `*manifest.Manifest` does not exist — the read shape is `manifest.Document`); `Load()`/`Reload()`: `os.ReadFile` → `manifest.ValidateAgainstSchema(raw)` → `json.Unmarshal` → atomic swap under write lock. Also tracks health state (`loadedAt`/`lastReloadAt`/`ok`) for `sqlgen_health` (19.3).
- [x] Default manifest discovery: `DiscoverManifest` walks up from CWD for `manifest_gen.json`; fails with a `-32001 MANIFEST_NOT_FOUND` coded `*Error` if not found (§3.1). Wired into `Run` via `resolveManifestPath`.
- [x] `cmd/sqlgen/mcp/watch.go` — `fsnotify` wrapper (watches the **directory**, not the inode, so it survives editor/atomic rename-replace); debounces event bursts; on successful reload invokes `Server.onManifestReload`. **Notification emission seam:** `onManifestReload` is wired but re-registers nothing yet — the SDK fires `tools/list_changed` / `resources/list_changed` only via registry mutation (`AddTool`/`AddResource`), so the actual emission lands when 19.3/19.4 register the surfaces and re-register on reload (verified end-to-end by the 19.6 watch subtest). Mirrors 19.1 threading `--manifest`/`--watch` for later consumption.
- [x] Failed-reload semantics (§6.3): a changed-but-invalid manifest is rejected in `Store.Reload`; the last good manifest keeps serving and `health.ok` flips to `false` until a subsequent good reload. `onReload` does not fire on a failed reload.
- [x] Watch on by default; `--no-watch` (folded into `opts.Watch`) skips constructing the `Watcher` entirely — zero fsnotify activity.
- Added `cmd/sqlgen/manifest/validate.go` — `ValidateAgainstSchema([]byte)`: offline validation against the embedded `schema/v1.json` (no `$schema` network fetch), the hot-path validator for reloads; and `cmd/sqlgen/mcp/errors.go` — `ErrorCode` + coded `*Error` (`-32001`/`-32002`), forward-compatible for 19.3's full error-code set.

### Acceptance Criteria

- Every (re)load validates against `cmd/sqlgen/manifest/schema/v1.json`; validation failure never swaps in the bad manifest.
- Atomic swap is safe under concurrent reads (no torn reads, no data race under `-race`).
- After a failed reload the prior good manifest still serves and `health.ok` is `false`.
- Watch default-on; `--no-watch` produces zero fsnotify activity.
- On a good stdio reload, both `tools/list_changed` and `resources/list_changed` fire; HTTP clients receive neither (they refetch).

### Tests Required

- [x] `store_test.go` — manifest validation (missing/malformed/structurally-invalid → coded `*Error`), atomic swap under concurrent reads (`-race`), schema-version mismatch handling (loads + warns), failed-reload-retains-good, `DiscoverManifest` walk-up + not-found.
- [x] `watch_test.go` — reload-on-write, rename-vs-rewrite (atomic replace still fires), debounce coalescing (burst → coalesced reloads), failed-reload-retains-previous-good-manifest (no `onReload`, `health.ok=false`).

### Completion Record

**Landed (implementation, pre-`/verify`).** Files created: `cmd/sqlgen/mcp/{store,watch,errors}.go` + `{store,watch}_test.go`, `cmd/sqlgen/mcp/testdata/manifest_gen.json` (fixture; copy of the manifest golden), `cmd/sqlgen/manifest/validate.go`. Files modified: `cmd/sqlgen/mcp/{server,run}.go` (store field + reload seam; discovery/load/watcher wiring), `cmd/sqlgen/mcp/run_test.go` (transport tests now pass a `--manifest` fixture — Run now requires a manifest per §3.1), `cmd/sqlgen/go.mod`/`go.sum` (add `github.com/fsnotify/fsnotify v1.9.0` as a direct dep, CLI module only). `make check` clean (0 lint, all unit tests `-race`); `make check-examples` clean (go.mod + `cmd/sqlgen/manifest/**` triggers; example output byte-unchanged — MCP + offline validator are not in the generation path). Store holds `*manifest.Document` (runtime read type). NOTE: the watch-reload notification *emission* is a wired seam consumed by 19.3/19.4's registries (see task note above).

**Auto-review (`sqlgen-reviewer`): 0 blocking, 3 low-severity — 2 fixed inline.** 5/5 applicable MCP.md PASS, 6/6 required tests present; debounce timer FSM + dir-watch verified correct; no race/leak; module boundary clean. Fixed inline: (2) non-`ErrNotExist` read failures (permission denied, EISDIR) now code `-32603 CodeInternal` instead of mislabeling as `-32001` — a manifest that *exists but is unreadable* is an internal error, not not-found (`+TestStoreLoadReadErrorIsInternal`); (3) a `NewWatcher` setup failure now degrades to no-watch + a warning rather than sinking a server with a good manifest already loaded. Remaining low-sev item (1) is the documented §5.2 notification-emission deferral to 19.3/19.4 (verified at 19.6) — no code action.

**Completed 2026-07-10.** All tasks + required tests checked off; `make check` re-run clean (0 lint, unit tests `-race`) after the two inline fixes. Marked complete without a separate `/verify` pass (reviewer verdict was ready-for-done; the one PARTIAL is a documented cross-item deferral, not a gap in 19.2). 0 FIX entries logged. **Next: `/implement 19.3`** (the 11 read-only tools + Levenshtein fuzzy suggestions) — churn-watch: `get_conventions`/`get_entity` read the deferred-freeze `conventions` sub-blocks, pin to `0.1.0`.

---

## 19.3 Tools implementation (the 11) + fuzzy suggestions

**PRD Reference:** MCP.md §4.1, §4.4, §5.3, §6.5; §10 #6/#10; scope-expansion (§10).

**Status:** Complete (2026-07-10) · **Churn-watch:** `get_conventions` / `get_entity` read the deferred-freeze conventions sub-blocks — pinned to `0.1.0`.

### Tasks

- [x] `cmd/sqlgen/mcp/tools.go` — tool registry + dispatch; all names prefixed `sqlgen_*`; all tools read-only / idempotent / side-effect-free. — `registerTools()` installs all 11 via a generic `addTool[In,Out]` adapter (SDK output type `any`, so tool-business errors render as IsError results and successes marshal the concrete output). Re-registered on reload to fire `tools/list_changed`.
- [x] `tools_entity.go` — `sqlgen_list_entities({kind?})`, `sqlgen_get_entity({name, compact?})`, `sqlgen_find_method({query, fuzzy?})`. — find_method: exact case-insensitive by default, substring on `fuzzy:true`; signatures rendered with the implicit `ctx context.Context`.
- [x] `tools_graph.go` — `sqlgen_find_referencing({table})`, `sqlgen_describe_relationship({entity, relationship, compact?})`, `sqlgen_find_join_path({from, to, max_hops?})`.
- [x] `tools_sql.go` — `sqlgen_show_sql({entity, method, dialect?})` reading `entity.methods[].sql_bodies`.
- [x] `tools_meta.go` — `sqlgen_get_conventions({})`, `sqlgen_get_example({entity, op?, compact?})`.
- [x] `tools_diag.go` — `sqlgen_health({})`, `sqlgen_validate_manifest({path?})`.
- [x] `cmd/sqlgen/mcp/suggest.go` — Levenshtein-≤2 fuzzy suggestion helper: case-insensitive, top-3 cap, sorted by distance then lexicographic; empty when nothing within threshold.
- [x] `compact: bool` on `get_entity` / `describe_relationship` / `get_example` (§4.1 / §10 #10): drops example snippets + long-form descriptions + non-lookup per-column metadata (`check`, `default_kind`, full `indexes[]`); always retains required identity. — get_entity deep-copies before trimming (never mutates the shared store record); describe_relationship drops the resolved target summary; get_example documents compact as a parity no-op (snippets are its required identity).
- [x] `find_join_path`: BFS over the relationship graph; `max_hops` default 4, clamped to `[1, 6]`; up to 5 shortest paths, shortest-first, lexicographic tie-break on serialized path; cycle-free (no entity twice on one path); empty list when no path within `max_hops`. — bounded simple-path DFS with per-branch path allocation (no slice aliasing); results sorted then capped.
- [x] `show_sql`: resolve the active dialect (override via `dialect`); `-32005 METHOD_SQL_UNAVAILABLE` when `sql_bodies` is absent (pre-addendum manifest) **or** absent for the resolved dialect; `-32006 METHOD_NOT_FOUND` for an unknown method.
- [x] `health` diagnostic payload (`manifest_path`, `schema_version`, `generated_at`, `loaded_at`, `last_reload_at?`, `watch_enabled`, `sqlgen_version`, `ok`); no manifest data leaks. — timestamps RFC 3339 (UTC); `last_reload_at` omitted until the first reload; deterministic within a run (load timestamp set once).
- [x] `validate_manifest`: no `path` → validate the in-memory manifest (no I/O); with `path` → read + validate that file without swapping the loaded manifest; error shape matches the §30.8 CLI tool. — flattens `*jsonschema.ValidationError` leaf causes to `{field, msg}`; unreadable path → `-32603` internal tool error.
- [x] Wire `-32003 ENTITY_NOT_FOUND` / `-32004 RELATIONSHIP_NOT_FOUND` / `-32006 METHOD_NOT_FOUND` to populate `suggestions[]` via `suggest.go` (§5.3). — **error-model note:** per-tool business errors surface as **MCP `IsError` tool results** carrying a structured `{code, message, suggestions}` envelope, *not* protocol-level JSON-RPC errors. Empirically, the go-sdk MCP client collapses a coded JSON-RPC error returned from a tool handler into an opaque "connection closed" transport error, **dropping the code + suggestions** the agent needs to self-correct. IsError results (the model the SDK documents for tool-execution errors) keep code + suggestions visible to the LLM — which is exactly §5.3's stated intent. The transport/startup codes `-32001`/`-32002` remain genuine load-time errors. This is a faithful adaptation of §5.3 to the SDK reality; flagged here for `/verify`.

### Acceptance Criteria

- All 11 tools match the §4.1 input/output shapes; dispatch is deterministic (byte-equal JSON-RPC on repeat, per §6.5 — sorted collections, no range-over-map, no `time.Now()` in bodies).
- Error codes `-32001`…`-32006` per §5.3; `suggestions[]` present on `-32003`/`-32004`/`-32006` when a candidate is within Levenshtein ≤2, capped at 3, correctly ordered.
- `find_join_path` honors the `max_hops` clamp, the ≤5 shortest-first + lexicographic-tie contract, the cycle guard, and the empty-on-no-path rule.
- `show_sql` returns dialect-correct SQL, honors `dialect` override, and emits `-32005` (absent field) vs `-32006` (unknown method) distinctly.
- `compact: true` drops example/verbose fields while retaining required identity.

### Tests Required

- [x] Per-tool table-driven tests over single-PK / composite-PK / tenanted / view / m2m / soft-deleted fixtures (`tools_entity_test.go`, `tools_graph_test.go`, `tools_sql_test.go`, `tools_meta_test.go`, `tools_diag_test.go`). — shared synthetic fixture in `fixture_test.go` (User/Post/Comment/Role single-PK, Membership composite-PK + tenancy, ActiveUser view, User↔Role m2m, User soft-delete).
- [x] `find_join_path`: 1-hop, 2-hop, no-path, cycle guard, `max_hops` boundary clamping; plus a dedicated fan-out graph pinning shortest-first + lexicographic tie-break and the ≤5 cap.
- [x] `show_sql`: dialect resolution, dialect-override-without-body + method-without-bodies (`-32005`), unknown-method (`-32006`), unknown-entity (`-32003`).
- [x] `suggest_test.go`: distance threshold, case-insensitivity, top-3 cap, tie-break ordering, empty-result path; plus a `levenshtein` unit table.
- [x] `tools_diag_test.go`: health-state transitions (fresh load, failed reload → `ok:false`, watch off); `validate_manifest` in-memory vs `path` modes (valid, no-swap, structurally-invalid → flattened issues, unreadable-path → `-32603`).
- [x] `TestDispatch_Deterministic` — each tool invoked twice against independent stores, byte-equal JSON (catches map-iteration leaks).
- [x] Added `tools_dispatch_test.go` — in-memory SDK round-trip: all 11 tools register + list, a `tools/call` returns structured content, and a not-found call surfaces as an IsError result carrying the code + suggestions (session stays usable).
- [x] Added `store_resolve_test.go` — unit coverage for the `per_entity` `resolveEntities` branch (sibling `entities/` file resolution + missing-file / unreadable-file coded errors), which the single-layout fixtures don't reach (closes the auto-review coverage gap; the graphql per_entity leg in 19.6 remains the integration check).

### Completion Record

**Landed (implementation + auto-review, 2026-07-10).** New files: `cmd/sqlgen/mcp/{tools,tools_entity,tools_graph,tools_sql,tools_meta,tools_diag,suggest}.go` + tests (`{tools,tools_entity,tools_graph,tools_sql,tools_meta,tools_diag,suggest,tools_dispatch}_test.go`, `fixture_test.go`, `toolerr_test.go`). Modified: `errors.go` (added `-32003..-32006` + the `toolError` model), `store.go` (full-entity resolution: inline for single layout, sibling `entities/` files for per_entity; `Entities()`/`EntityByName`/`EntityByTable`/`EntityNames` accessors, atomic swap of the entity maps), `server.go` (`version`/`watchEnabled` fields; `onManifestReload` re-registers tools), `run.go` (register tools before serve; set `watchEnabled`). `make check` clean (0 lint, all unit tests `-race`). `make check-examples` **not** triggered: all changed paths are under `cmd/sqlgen/mcp/**`, which the path-based E2E table does not list (MCP is not in the generation path; no go.mod/template/manifest-builder change this sub-item). Full example coverage lands in 19.6. Watch-reload notification emission is now live (tool re-registration fires `tools/list_changed`), closing the 19.2 deferral for tools (resources land in 19.4).

**Key decision (error model).** Per-tool business errors (`-32003`/`-32004`/`-32005`/`-32006`/`-32603`) surface as MCP `IsError` tool results with a structured `{code, message, suggestions}` envelope, not protocol-level JSON-RPC errors — the go-sdk client collapses a coded error returned from a tool handler into an opaque "connection closed" transport error, dropping the code + suggestions. IsError results keep them visible for agent self-correction (§5.3's intent) and the session stays usable (verified in `tools_dispatch_test.go`). Startup codes `-32001`/`-32002` remain genuine load-time errors. Flagged for `/verify`.

**Auto-review (`sqlgen-reviewer`, 2026-07-10): 10/11 PRD PASS, 1 documented PARTIAL, 0 blocking — verdict ready-for-done.** Confirmed the error-model decision is a **faithful adaptation of §5.3** (`TestDispatchNotFoundIsToolError` proves no transport error + envelope survives + session usable); module boundary clean (go-sdk absent from `./go.mod` + `parser/go.mod`, grep 0); atomic-swap safe under `-race`; determinism holds (all cross-entity collections explicitly sorted; `health.loaded_at` set once per load). Non-blocking items carried to **19.7 PRD §31 wording** (not code changes): (1) `get_example` `compact` is a documented no-op — snippets are its required identity, so honoring the literal §4.1 "drop snippets" would empty the payload; note this in §31.3. (2) §5.3's literal example nests `suggestions` under `data`; the IsError envelope flattens it top-level — reconcile the §31.4 wording. (3) `find_referencing` `relationship_name` is the *edge* name from whichever side declared it (both m2m/FK sides emit, deduped by full tuple) — describe the field precisely in §31.3. The one reviewer-suggested code follow-up (per_entity unit coverage) was **addressed inline** via `store_resolve_test.go` rather than deferred. `make check` re-run clean (0 lint, `-race`). 0 FIX entries logged.

**Verified + completed (2026-07-10).** `/verify 19.3`: 12/12 PRD requirements (MCP.md §4.1/§4.4/§5.3/§6.5) PASS, 8/8 required tests present, 0 guideline issues; `make check` clean under `-race`; `make check-examples` correctly not triggered (all changed paths under `cmd/sqlgen/mcp/**`, not in the E2E path). Independent `sqlgen-reviewer` pass: **full agreement** — confirmed the error-model IsError adaptation is faithful to §5.3, determinism enforced via explicit sorts, module boundary clean, atomic-swap safe under `-race`. One low-severity latent determinism nit logged (**FIX-130**): `find_referencing`'s `lessReference` sort omitted `Kind` while dedup keyed on the full `Reference` struct — practically unreachable, but §6.5 makes determinism a hard rule. **FIX-130 resolved same day** via `/fix-implement`: widened `lessReference` (`Kind` final tiebreak) + `findMethod` comparator (`Signature` final tiebreak) to match their dedup keys, added `TestFindReferencingKindTiebreak` regression coverage; auto-review 3/3 PASS, 0 issues, no golden churn. Three §31 documentation-reconciliation items (get_example compact no-op; `suggestions` flattened vs §5.3's `data` nesting; `find_referencing` `kind` semantics) carried to **19.7 PRD §31 wording** — doc-only, not code gaps. **Ready state: all tasks + tests checked, 0 open FIXes.**

---

## 19.4 Resources + Prompts implementation

**PRD Reference:** MCP.md §4.2, §4.3, §4.4, §5.2; §10 #11, scope-expansion.

**Status:** Complete (2026-07-10) · **Churn-watch:** `sqlgen://conventions` reads the deferred-freeze conventions sub-blocks — pinned to `0.1.0`.

### Tasks

- [x] `cmd/sqlgen/mcp/resources.go` — four URIs: `sqlgen://manifest`, `sqlgen://entity/<name>`, `sqlgen://conventions`, `sqlgen://config` (all `application/json`). — three fixed resources via `AddResource`; `sqlgen://entity/{name}` is an RFC 6570 `AddResourceTemplate` (routes concrete reads via the SDK template match, so it's listed under `resources/templates/list`, not `resources/list`).
- [x] `sqlgen://manifest`: full manifest always served; add a `warning` when the encoded body exceeds **256 KB** (`manifestWarnThreshold` named constant, revisitable). — **SDK-shape adaptation:** the SDK's `ResourceContents` is a fixed struct with no literal sibling `warning` field, so the §4.2-example warning rides in the sanctioned `_meta.warning` channel (constant `metaWarningKey`); the full document is served verbatim regardless. Flagged for `/verify` (parallels 19.3's IsError adaptation of §5.3).
- [x] `sqlgen://config`: surface the manifest's top-level `generation_config` (landed in Phase 18); if a loaded manifest predates it, return `{}` + `warning`. — presence is probed from the raw bytes (`Store.GenerationConfig() (cfg, present)`) because a pre-addendum manifest decodes into a zero-valued all-false struct indistinguishable from a genuine all-off config; doc + raw read under one lock for an atomic snapshot.
- [x] `sqlgen://entity/<name>`: `-32003 ENTITY_NOT_FOUND` + `suggestions[]` on unknown name. — genuine protocol-level `*jsonrpc.Error` (resources have no IsError escape hatch): the wire response carries code `-32003` + `data.suggestions` correctly; the go-sdk *client* library collapses that to an opaque message-only error (the same 19.3 collapse), so suggestions are **also inlined into the message text** so they survive for agent self-correction. Wire shape verified by the direct-handler test; 19.6's raw-JSON-RPC leg is the integration check.
- [x] Emit `notifications/resources/list_changed` on watch reload (stdio only). — `onManifestReload` now calls `registerResources()` alongside `registerTools()`; re-registering the (unchanged) resource set fires the notification via the SDK's `changeAndNotify` path (end-to-end verified in 19.6).
- [x] `cmd/sqlgen/mcp/prompts.go` — three templates registered with the SDK prompt registry, `text/template` substitution for `{entity}` / `{goal}` / `{relationship}` / `{method}`:
  - `sqlgen-write-query({entity, goal})`
  - `sqlgen-add-relationship-usage({entity, relationship})`
  - `sqlgen-debug-not-found({entity, method?})` — `{{if .method}}` branch renders `<Entity>.<method>` vs "a method on `<Entity>`".
- [x] Prompt expansion is stateless; no `prompts/list_changed` is ever emitted (prompts don't change at runtime). — registered once at startup, never re-registered; required-arg validation → `-32602` invalid-params; no store dependency (pure template expansion; expansion instructs the LLM to call the relevant tools rather than pre-fetching).

### Acceptance Criteria

- Four resources per §4.2 with correct bodies + mime type.
- `sqlgen://manifest` emits the `warning` field above 256 KB and still serves the full document.
- `sqlgen://config` surfaces `generation_config`; empty-object-plus-warning fallback only when the field is genuinely absent.
- Three prompts per §4.3 with kebab-case `sqlgen-` names; clients without prompt support degrade cleanly (tools/resources unaffected).

### Tests Required

- [x] Resource-read tests (`resources_test.go`): per-URI body shape + the 256 KB warning on `sqlgen://manifest` (small→no warning, over-threshold→`_meta.warning` + full body, **plus an SDK-dispatch round-trip asserting `_meta.warning` survives to a client** — closes the reviewer's low-sev wire-coverage note); `sqlgen://config` present-vs-absent fallback; `sqlgen://entity/<name>` unknown-name error+suggestions (direct handler asserts wire code `-32003` + `data.suggestions`; SDK dispatch asserts inlined-suggestion survival + session-stays-usable); plus SDK round-trips (`resources/list` = 3 fixed URIs, entity read via template, unknown-entity surfaces error without killing the session).
- [x] `prompts_test.go`: each of the 3 templates expands with argument substitution (+ the debug-not-found `method` branch); required-argument validation → `-32602`; unknown-prompt + missing-arg error paths over SDK dispatch; `prompts/list` = 3.

### Completion Record

**Landed (implementation + auto-review, 2026-07-10).** New files: `cmd/sqlgen/mcp/{resources,prompts}.go` + `{resources,prompts}_test.go`. Modified: `store.go` (added `GenerationConfig() (cfg, present)` raw-probe accessor), `server.go` (`onManifestReload` re-registers resources), `run.go` (register resources + prompts before serve). `make check` clean (0 lint, all unit tests `-race`). `make check-examples` **not** triggered — all changed paths under `cmd/sqlgen/mcp/**`, which the path-based E2E table does not list (MCP is not in the generation path); full example coverage lands in 19.6. No security-review trigger (no `cli/**` or embedded-runtime changes). Watch-reload resource notification is now live (resource re-registration fires `resources/list_changed`), closing the 19.2 deferral for resources.

**Key decisions.** (1) The §4.2 `warning` sibling rides in `_meta.warning` — the SDK `ResourceContents` struct has no literal `warning` field; the served body stays byte-pure. (2) `sqlgen://entity/<name>` unknown-name is a genuine `*jsonrpc.Error` (code `-32003` + `data.suggestions` on the wire) with suggestions **also inlined into the message** because the go-sdk client collapses coded errors — the resource analogue of the 19.3 tool IsError adaptation; resources have no IsError channel, so the protocol error is the only semantically-correct option and the inline keeps suggestions LLM-visible. (3) `get_example`-style `compact` no-op does not apply here (resources take no compact arg).

**Auto-review (`sqlgen-reviewer`, 2026-07-10): 7/7 PRD PASS, 2/2 test groups, 0 high-confidence issues — verdict ready-for-done.** Confirmed against go-sdk source that a directly-returned `*jsonrpc.Error` propagates Code+Data to the wire (`server.go:347`) and `AddResource`/`AddResourceTemplate` route through `changeAndNotify` (fires `resources/list_changed`); determinism holds (struct/`MarshalIndent`/verbatim bodies — no range-over-map, no `time.Now()`); module boundary clean (go-sdk absent from runtime + parser `go.mod`, verified). The one actionable low-sev note — `_meta.warning` never asserted through an SDK client round-trip — was **closed inline** (`TestDispatchManifestWarningSurvivesToClient`); the other two were cosmetic (integer-KB rounding at exactly threshold+1) / pre-existing (N `list_changed` per reload, the 19.3 pattern). Marked complete without a separate `/verify` pass (reviewer verdict was ready-for-done; the two SDK-shape adaptations are faithful, verified against SDK source, and documented above). 0 FIX entries logged.

---

## 19.5 `.mcp.json` emission + config block + agent-integration docs

**PRD Reference:** MCP.md §3.2, §3.3, §3.4, §6.6, §7.1; §10 #7/#12. Parallels Phase 18 breadcrumb validation.

**Status:** Complete (2026-07-10)

### Tasks

- [x] `config.ManifestConfig.MCP` block (§3.4): `emit_project_config` (default `true` when `manifest.enabled`), `project_configs` (list, default `[.mcp.json]`), `server_key` (default `sqlgen`), `command_template` (optional). Resolution order table-level → global → default. — `config.MCPConfig` + `applyManifestMCPDefaults`. **Note:** the block is global-only (§3.4 nests it under `generation.manifest`; `TableManifestConfig` stays Enabled-only per PRD §30.2), so the applicable resolution rungs are global → default — no table-level `mcp` field was invented.
- [x] Validation: error if `mcp.emit_project_config: true` while `manifest.enabled: false`; each `project_configs` entry must be module-root-relative and must not escape the module root (`..` segments rejected). — `validateManifestMCPTargets` (absolute / any `..` segment / empty entry rejected) + the disabled-state rule in `validateManifestDisabledFields`.
- [x] `cmd/sqlgen/mcp/mergeconfig.go` — `Merge(MergeOptions)` per single `ConfigPath` (§6.6): `MergeUpsert` / `MergeRemove`; sentinel markers `x-sqlgen-managed` / `x-sqlgen-manifest` / `x-sqlgen-generator-version`; `findMatchingEntry` by absolute manifest-path comparison; `checkUnmanagedCollision` → stderr warning + `pickFreeKey` (`sqlgen-2`, `-3`, …); `canonicalize` (recursive alpha key-sort, 2-space indent); `writeAtomic` (tmp → fsync → rename); `command_template` render via `text/template` `{{ manifest_path }}` + shell-style tokenization; delete file when `mcpServers` empties and no other top-level keys remain. — Canonicalization rides encoding/json's map-key sort (2-space indent, HTML escaping off). Warnings are *returned* to the pipeline caller, which prints them to stderr (matches the manifest-stage warning pattern). **Stale-vs-sibling disambiguation:** a managed entry whose manifest file no longer exists on disk is swept as stale (`output.dir` moved); one whose manifest exists is a live monorepo sibling and survives — the only mechanical reading satisfying both §3.4 edge rows; flagged for `/verify`. A remove that matches nothing leaves the file byte-untouched (no canonicalization of files we didn't change).
- [x] Pipeline integration: loop `project_configs`, emit/remove each as the **last** step of the manifest stage (after breadcrumbs); a per-target failure is reported but does not abort the remaining targets; the overall run exits non-zero if any target failed. — `cli/mcp_emit.go::emitMCPProjectConfigs`, wired as generate step 7d after `manifest.RunStage` (the merge lives in `cmd/sqlgen/mcp`, which imports `cmd/sqlgen/manifest` — the CLI coordinates per the runner notes). Per-target errors aggregate via `errors.Join` → ExitGeneration. Manifest identity honors `SQLGEN_OUTPUT_DIR_IMPORT_OVERRIDE` (FIX-062 pattern) so the E2E harness's temp-dir compare runs don't churn committed example `.mcp.json` files; manifest disabled/absent runs the removal path with §3.4 defaults so opt-out cleans up.
- [x] `docs/design/MCP.md` §3.2 per-client registration walkthrough (Claude Code, Claude Desktop, Cursor, Windsurf) + a manual-fallback section (Claude Desktop / any client lacking project-local discovery); document the dual-target pattern `project_configs: [.mcp.json, .cursor/mcp.json]`; worked postgres-example registration with three canned prompts; troubleshooting section (stderr logs, path issues, unmanaged-collision warning, gitignored `.mcp.json`). — §3.2 rewritten with per-client walkthrough, manual-fallback subsection, worked postgres-example registration (3 canned prompts), and a troubleshooting table.
- [x] **Doc-honesty follow-through (from the 2026-07-10 §1 rewrite):** add a **"Registration reach & limitations"** subsection so §1.1's "documented as a known limitation" has a home — (a) the cross-module wrapper library gap (`$GOMODCACHE` manual wiring, no repo-local `.mcp.json` reach) and (b) the binary-availability + version-skew risk (committed `.mcp.json` inherits registration but not the `sqlgen` binary/version). Add a one-line note near §3.2/§3.4 that the stdio server is **agent-auto-spawned per session** (never started manually). — Both landed: "Registration reach & limitations" subsection under §3.2; auto-spawn note opens §3.2.

### Acceptance Criteria

- The `.mcp.json` merge honors every §3.4 edge case: no-file create; missing `mcpServers` field; invalid JSON → fail loud (no overwrite); unmanaged-collision → warn + free key; multi-sqlgen monorepo coexistence by `x-sqlgen-manifest`; `output.dir` move → stale-entry cleanup; opt-out removal (delete file if empty); manual-arg edit overwritten on regen; gitignored/read-only path → clear error.
- Output is canonical + byte-stable (first run normalizes; subsequent runs byte-identical for identical content).
- Multi-target `project_configs` updates every target independently; one target's failure does not block the others.
- `command_template` substitutes `{{ manifest_path }}` and becomes the entry's `command` + `args`.
- Config validation rejects `emit_project_config` without `manifest.enabled` and any `..`-escaping `project_configs` entry.

### Tests Required

- [x] `mergeconfig_test.go` — table-driven over every §3.4 / §7.1 edge case: no-file create; add → upsert; existing managed → in-place update (key + position preserved); unmanaged-collision → `sqlgen-2` + warn; invalid JSON → error without overwrite; multi-package monorepo coexistence; `output.dir` move → stale cleanup; `emit_project_config` flipped off → remove (+ delete if empty); `command_template` rendering; read-only path → error; gitignored path still writes; canonical-format normalization (run 1 reformats, run 2 byte-identical); atomic-write crash simulation (tmp present, target untouched); **multi-target** (two entries, both updated in one run; one target's failure does not block the other). — 17 test funcs incl. `tokenizeCommand` + `offsetLineCol` unit tables, managed-sibling-occupies-key case, remove-no-match-leaves-file-untouched, nested `.cursor/mcp.json` dir creation, absolute-manifest-path storage. Multi-target + failure-isolation live at the pipeline layer in `cli/mcp_emit_test.go` (where the loop is). Gitignored-path-still-writes holds by construction (no .gitignore reads anywhere in the merge path — no I/O to assert against).
- [x] Config resolution + validation tests (table → global → default; the two negative validation rules). — `TestLoadConfig_ManifestMCPDefaults` (omitted/partial blocks), `TestValidatePreParse_Manifest_MCPEmitRequiresEnabled`, `TestValidatePreParse_Manifest_MCPProjectConfigPlacement` (6-case placement table).

### Completion Record

**Landed (implementation, 2026-07-10).** New files: `cmd/sqlgen/mcp/mergeconfig.go` + `mergeconfig_test.go`, `cmd/sqlgen/cli/mcp_emit.go` + `mcp_emit_test.go`. Modified: `cmd/sqlgen/config/config.go` (`MCPConfig` + defaults), `cmd/sqlgen/config/validate.go` (two §3.4 rules), `cmd/sqlgen/config/manifest_test.go`, `cmd/sqlgen/cli/generate.go` (step 7d), `docs/design/MCP.md` (§3.2 rewrite: walkthrough / manual fallback / worked postgres example + 3 canned prompts / troubleshooting / "Registration reach & limitations"; §3.4-example path fix `models/manifest/manifest_gen.json`). **New committed artifacts:** `.mcp.json` at the four manifest-enabled example roots (postgres/mysql/sqlite/graphql) — emitted by the default-on `emit_project_config`, byte-stable across harness runs (identity path rides `SQLGEN_OUTPUT_DIR_IMPORT_OVERRIDE`, version `dev`), and the doc's worked example. `make check` clean (0 lint, unit `-race`); `make check-examples` clean; `TestE2EGoldenFiles` run twice — PASS, tracked example output byte-unchanged. **Key decisions flagged for `/verify`:** (1) stale-vs-monorepo-sibling disambiguation by manifest-file existence on disk (only reading satisfying both §3.4 edge rows); (2) Merge returns warnings for the CLI to print (stderr reach preserved, testable); (3) `mcp` block is global-only — no table-level field invented (§3.4 defines none; PRD rule #1); (4) removal runs with §3.4 defaults when the manifest block is absent entirely (symmetric cleanup). Security-review trigger (cli/** file-writing path): deferred to `/verify`'s independent reviewer pass — noted explicitly.

**Auto-review (`sqlgen-reviewer`, 2026-07-10): 12/12 PRD PASS, 4/4 test groups, 0 blocking — verdict ready-for-done.** Security surface explicitly checked (no shell execution — `command_template` output is stored for the *agent client* to spawn, never executed by sqlgen; path handling + escape validation confirmed; module boundary clean). Both low-severity findings **fixed inline**: (1) decode now uses `json.Decoder.UseNumber()` so re-encoding preserves sibling entries' exact numeric form (a float64 round-trip could rewrite integers >2^53, softening §3.4's "other entries are never touched") — `+TestMergePreservesSiblingNumericForm`; (2) `validateManifestMCPTargets` now runs regardless of enabled state (the removal path walks the same targets) — `+TestValidatePreParse_Manifest_MCPPlacementEnforcedWhenDisabled`. Informational note carried to 19.6/19.7: committed example `.mcp.json` pin `x-sqlgen-generator-version: "dev"` — keep the harness version unset ("dev") so a release-stamped binary doesn't churn them. `make check` re-run clean after fixes.

**Verified + completed (2026-07-10).** `/verify 19.5`: 9/9 PRD (MCP.md §3.2/§3.3/§3.4/§6.6/§7.1) PASS, 3/3 required test groups present (44+ test funcs), 0 guideline issues; `make check` + `make check-examples` re-run clean after the two inline review fixes; `TestE2EGoldenFiles` byte-stable across runs. Independent `sqlgen-reviewer` pass: **full agreement** — all six flagged decisions confirmed faithful (stale-vs-sibling file-existence rule; warnings-returned-to-caller; global-only `mcp` block; removal-with-defaults; FIX-062 identity override matching EmitJSON placement; remove-no-match leaves file untouched); security surface re-confirmed (no shell execution, escape validation, module boundary clean). 0 FIX entries logged. Three low-severity **watch items carried forward as notes, not FIXes**: (1) → 19.7 §31.2 wording: document the stale-sweep file-existence rule (a monorepo sibling whose manifest is gitignored / not yet generated gets swept) and reconcile §3.4's literal "directory containing go.mod" with the implemented module-root anchor (CWD, the same anchor `output.dir` assumes — no go.mod discovery exists anywhere in the codebase); (2) → 19.6: keep the harness `Version` at "dev" (the four committed `.mcp.json` sit outside `output.dir`, hence outside golden protection); (3) the four `.mcp.json` files are committed artifacts serving §3.2's worked example.

---

## 19.6 E2E surface across all dialects

**PRD Reference:** MCP.md §7.2, §7.3, §7.4. Parallels Phase 16's gqlgen-subprocess pattern.

**Status:** Complete (2026-07-10)

> **Carried from 19.5 verify:** keep any harness/subprocess builds at `Version` "dev" — the four committed example `.mcp.json` files pin `x-sqlgen-generator-version: "dev"` and sit outside `output.dir`, hence outside the golden `expected/` diff; a release-stamped binary running the suite would rewrite all four.

### Tasks

- [x] `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,graphql}/tests/mcp_test.go` — boot the server against each example's `manifest_gen.json`; exercise all 11 tools, read all 4 resources, expand all 3 prompts; assert response shape matches the example schema; dialect-aware `show_sql`. — **Raw newline-delimited JSON-RPC client** (no SDK — example modules depend only on the root module, and the hand-rolled client is the SDK-independent wire check, incl. the 19.4-deferred protocol-level `-32003` + `data.suggestions` assertion on `sqlgen://entity/<typo>` and the IsError typo envelope with a real Levenshtein suggestion). Assertion targets picked dynamically from each example's own manifest (first entity with relationships; `find_method(FindByID)` → `show_sql` asserting per-dialect placeholder `$1`/`?`). Binary built once per package via `sync.OnceValues` with `GOWORK` pointed at the repo `go.work` (make test-examples runs with `GOWORK=off`, which cannot build the CLI module). Files identical across the four examples except the two dialect consts; graphql leg exercises the `per_entity` store resolution end-to-end. Clean-shutdown assertion (stdin close → exit 0).
- [x] `cmd/sqlgen/mcp/integration/client_stdio_test.go` — subprocess-drives full JSON-RPC lifecycle: `initialize` → `tools/list` → `tools/call` (each of 11) → `resources/list` → `resources/read` (each of 4) → `prompts/list` → `prompts/get` (each of 3) → `shutdown`. — SDK client over `mcp.CommandTransport`; binary built once in `TestMain` (workspace-active build); shared `driveLifecycle` helper also asserts `resources/templates/list` = 1, the `-32003` IsError envelope, and session-stays-usable.
- [x] `cmd/sqlgen/mcp/integration/client_http_test.go` — `--http <port>` subprocess, same lifecycle over loopback HTTP, plus the no-notification-push contract (client refetches after watch reload rather than waiting for `list_changed`). — `--stdio=false --http <port>` subprocess; `StreamableClientTransport` with connect-retry loop over an ephemeral loopback port; no-push asserted via never-firing `ToolListChangedHandler`/`ResourceListChangedHandler` while a polling re-query surfaces the reloaded data (the refetch contract itself).
- [x] Watch-mode subtest: mutate the manifest file; assert the stdio client receives `tools/list_changed` and re-queries reflect new data; assert the HTTP client's next list call surfaces the change without a notification. — `TestStdioWatchReloadNotifiesAndServesNewData` (asserts **both** `tools/list_changed` and `resources/list_changed` arrive — closing the 19.2 §5.2 emission deferral end-to-end) + `TestHTTPWatchReloadNoPushClientRefetches`. Mutation = schema-valid rename of the ActiveUser view.
- [x] `cmd/sqlgen/mcp/integration/claude_code_smoke_test.go` — gated on `CLAUDE_CODE_BIN`; spawns Claude Code headless against the server, runs canned prompts, asserts tool calls happen. Skipped by default. — `claude -p` with `--mcp-config`/`--strict-mcp-config`/`--allowedTools mcp__sqlgen__*` in a throwaway project dir whose `.mcp.json` registers the built server (the §3.4 registration shape); the manifest lives outside the project so the entity names are only reachable via the tools. **Validated once against a real local Claude Code binary: PASS (16s).** Skips cleanly when ungated.
- [x] Cross-cutting: server survives a manifest regen mid-conversation (atomic swap, no dropped connection). — folded into the stdio watch subtest: mutation is applied via write-temp + `os.Rename` (the regen shape), the same session then serves the new data with `health.ok=true` and `last_reload_at` set; no reconnect.

### Acceptance Criteria

- All four example legs pass (dialect-appropriate `show_sql` per example; graphql example is `per_entity` layout).
- stdio and HTTP integration lifecycles both pass; the per-transport watch/notification contract is verified.
- The real-client smoke test is gated and skipped by default (runs only when `CLAUDE_CODE_BIN` is set).

### Tests Required

- [x] The four `mcp_test.go` example legs (11 tools / 4 resources / 3 prompts each). — `TestMCP_FullSurface` × 4, all green (postgres 9.3s / mysql 12.1s / sqlite 4.8s / graphql 5.9s, `-race`, `GOWORK=off` like make test-examples).
- [x] `client_stdio_test.go` full lifecycle. — `TestStdioLifecycle`, green under `-race`.
- [x] `client_http_test.go` full lifecycle + no-push contract. — `TestHTTPLifecycle` + `TestHTTPWatchReloadNoPushClientRefetches`, green under `-race`.
- [x] Watch-mode notification subtest (both transports). — `TestStdioWatchReloadNotifiesAndServesNewData` (+ HTTP no-push above), green under `-race`.
- [x] Gated `claude_code_smoke_test.go`. — skips without `CLAUDE_CODE_BIN`; PASS when run against a real local Claude Code binary.

### Completion Record

**Landed (implementation, 2026-07-10).** New files: `cmd/sqlgen/mcp/integration/{integration,client_stdio,client_http,claude_code_smoke}_test.go` (test-only package; binary built once in `TestMain` — workspace must be active, so no `GOWORK=off`); `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,graphql}/tests/mcp_test.go` (raw JSON-RPC wire client; byte-identical except the two dialect consts; binary built per package with `GOWORK` pinned to the repo `go.work` because make test-examples exports `GOWORK=off`). No production code changed — this sub-item is pure test surface. Harness `Version` stays "dev" per the 19.5 carry-note (no `.mcp.json` churn observed; `git status` on the four committed files clean after the sweep). **Wire checks closed:** the raw legs assert the protocol-level `-32003` + `data.suggestions` on `sqlgen://entity/<typo>` (the 19.4 deferral) and the IsError `{code, message, suggestions}` envelope with a real Levenshtein suggestion; the stdio watch test asserts both `tools/list_changed` **and** `resources/list_changed` (closing the 19.2 §5.2 emission deferral end-to-end); the HTTP test pins the §5.1 stateless no-push contract. **Cross-cutting regen-mid-conversation** folded into the stdio watch test (rename-replace mutation; same session serves new data; `health.last_reload_at` set). **Real-client smoke validated once against a local Claude Code binary: PASS (16.5s)** — server spawned from the §3.4 `.mcp.json` shape, `mcp__sqlgen__*` tools only, both fixture entities surfaced in the answer; skips cleanly when ungated. Test results: integration package green under `-race` (stdio 4.6s, http 4.7s); all four example legs green under `-race` + `GOWORK=off`; `make check` clean (0 lint); `make check-examples` clean (full sweep, exit 0).

**Auto-review (`sqlgen-reviewer`, 2026-07-10): 6/6 PRD (MCP.md §7.2/§7.3/§7.4) PASS, 5/5 required tests present, 0 high-confidence issues — verdict ready-for-done.** Confirmed: no production code in the diff; GOWORK reasoning correct on both sides; subprocess cleanup / race-safety / unbounded reader all sound; graphql per_entity leg genuinely resolves sibling `entities/*.json`; the tool-order assertion doubles as a §6.5 determinism check. Three low-severity observations, accepted without code action: (1) the example MCP leg shares the package `TestMain` and therefore needs Docker it doesn't logically use — inherited example-module pattern, noted for the 19.7 closure record; (2) the four example files are hand-maintained near-duplicates (byte-identical except the 2-line const block) — unavoidable across module boundaries; (3) `driveLifecycle` hardcodes the shared `mcp/testdata/manifest_gen.json` fixture names — same fixture the unit tests pin.

**Verified + completed (2026-07-10).** `/verify 19.6`: 6/6 PRD (MCP.md §7.2/§7.3/§7.4) PASS, 5/5 required tests present, 0 guideline issues; `make check` + `make check-examples` clean on this exact state. Independent `sqlgen-reviewer` pass: **full agreement, 0 issues** — specifically cleared the deeper angles: flakiness (50ms debounce vs 10s/25ms polling = 200× margin; the HTTP no-push assertion is structurally sound — stateless HTTP has no stream to push on, so ordering cannot produce a false pass; freePort race absorbed by the 15s connect-retry), raw JSON-RPC correctness (protocol version + initialized notification match the SDK; int-id matching safe — the server initiates no client-bound requests), process hygiene (LIFO cleanups reap all subprocesses on failure paths), no Version/CWD assumptions (tests never run `generate`; no `.mcp.json` churn), and dynamic-target selection validated against all four real example manifests (`FindByID` + relationship edges present in every leg). Two non-actionable notes recorded: the raw client read has no per-read deadline (bounded by the go-test timeout); the watch mutation is a deliberately shallow single-replace. 0 FIX entries logged. **Ready state: all tasks + tests checked, 0 open FIXes.**

---

## 19.7 Phase closure sweep + PRD sync

**PRD Reference:** MCP.md §8 (PRD sync plan). Lands PRD §31.

**Status:** Complete (2026-07-10)

### Tasks

- [x] `make check` + `make check-examples` + `make test-integration` clean under `-race`. — check + check-examples clean on the final code state (19.6); test-integration run at 19.7 (see completion record).
- [x] Land **PRD §31 "MCP Server"** per §8: §31.1 Overview (using the honest §1.1 framing — same-repo wrapper discoverability + precision query interface; cross-module case a documented limitation), §31.2 Configuration, §31.3 Tool/Resource/Prompt Surface (11/4/3), §31.4 Protocol (error codes incl. `-32005`/`-32006` + typo-suggestion envelope), §31.5 Versioning Policy. **Carried wording items:** from 19.3 — (a) `get_example` `compact` documented no-op, (b) `suggestions` flattened top-level vs §5.3's `data` nesting, (c) `find_referencing` `kind`/edge-name semantics; from 19.5 verify — (d) §31.2 documents the `.mcp.json` stale-sweep file-existence rule (gitignored / not-yet-generated sibling manifests get swept) and (e) reconciles §3.4's literal "directory containing go.mod" with the implemented module-root anchor (CWD, same anchor as `output.dir`). — §31.1–§31.5 landed (~120 lines); **all five carried wording items resolved in place**: (a) §31.3 surface note; (b) §31.4 "Error channels (normative)" — IsError tool results carry the flattened top-level envelope, protocol-level errors (startup + resources) nest under `data`; (c) §31.3 `find_referencing` note (declaring-side edge semantics, deduped); (d) §31.2 stale-sweep bullet incl. the gitignored-sibling consequence; (e) §31.2 "Module-root anchor" paragraph (CWD anchor, no go.mod discovery — conventionally the go.mod dir).
- [x] PRD §23.1 CLI Commands gains a `sqlgen mcp serve` row; PRD §30 cross-reference to §31. — §23.1 row added; §30.1 agent-surface bullet + §30.8 closing sentence now point at §31 (the pre-existing §30.4.3/§30.7 addenda notes already referenced "MCP §31", which now resolves).
- [x] Update `STATUS.md` Current Focus + Overview table. — at `/done 19.7` + `/close-phase`.
- [x] **Version alignment (freeze-aware):** align the MCP server version to the manifest `schema_version` **as it actually stands at closure** — do not stamp `1.0.0` if the schema is still `0.1.0`. Record the decision explicitly. — **Decision recorded in PRD §31.5:** no independent MCP server version is tagged. The handshake advertises the sqlgen CLI version (the binary *is* the server; `server.go` passes `Version` through), and the MCP *surface* versions with the manifest `schema_version` it serves — `0.1.0` at closure, so the surface is explicitly pre-1.0. MCP.md §9's "tag MCP server 1.0.0 aligned with manifest schema v1" assumed the freeze had happened; it hasn't, so nothing is stamped, and the surface version lifts automatically when the deferred freeze lands (no MCP-side action).

### Acceptance Criteria

- All three test sweeps clean under `-race`, no flakes.
- PRD §31 (§31.1–§31.5) + the §23.1 row + the §30 cross-reference are landed and internally consistent.
- STATUS.md reflects Phase 19 closure.
- The MCP-server-version ↔ manifest-`schema_version` alignment is recorded and consistent with the deferred-freeze reality.

### Tests Required

- [x] Full sweep green: `make check`, `make check-examples`, `make test-integration` (all `-race`). — see completion record for run results.
- [x] Doc-consistency check: PRD §31 tables match the MCP.md §4 surface (11/4/3) verbatim. — verified programmatically: all three tables (13/6/5 markdown rows incl. headers) byte-identical between `docs/design/MCP.md` §4 and `docs/PRD.md` §31.3.

### Completion Record

**Landed (implementation, 2026-07-10).** Files changed: `docs/PRD.md` (new §31.1–§31.5, ~120 lines, between §30.8 and Appendix A; `sqlgen mcp serve` row in §23.1; §30.1 tools-consumption pointer; §30.8 closing cross-ref — the pre-existing §30.4.3/§30.7 "MCP §31" references now resolve). No code changes — 19.7 is doc + sweep. **Full sweep green (2026-07-10):** `make check` (0 lint, unit `-race`, all 8 modules), `make check-examples` (fmt/lint/vet/test across all 10 example modules incl. the four new MCP legs), `make test-integration` (all modules, no `-short`, `-race`, Docker; includes the new `mcp/integration` package: stdio + HTTP lifecycles, watch subtests, gated smoke skip) — all exit 0, no flakes. **All five carried wording items resolved in §31** (get_example compact no-op; two-channel error model with flattened IsError envelope vs `data`-nested protocol errors; find_referencing declaring-side semantics; stale-sweep file-existence rule + gitignored-sibling consequence; module-root = CWD anchor). **Version-alignment decision recorded in §31.5:** no independent MCP server version tagged — handshake advertises the CLI version, the MCP surface versions with the manifest `schema_version` (`0.1.0` at closure, explicitly pre-1.0), lifting automatically when the deferred freeze lands. Doc-consistency table check byte-exact. 19.6's accepted observation (example MCP leg inherits the package TestMain Docker dependency) carried into this record for closure visibility.

**Auto-review (`sqlgen-reviewer`, 2026-07-10): 6/6 sync-plan requirements PASS, all five carried wording items landed and code-accurate, 0 issues — verdict ready-for-done.** Every load-bearing §31 claim spot-checked against the implementation (two-channel error model vs `tools.go`/`resources.go`; merge contract vs `mergeconfig.go`; module-root anchor vs `mcp_emit.go`; version handshake vs `server.go`; flag table vs `cmd_mcp.go`) — all faithful, nothing invented. Two harmless notes: per-client registration examples live in MCP.md §3.2 by the PRD's stated design-supplement division; a `(§30.4)` cite could be `(§30.4.3)`.

**Verified + completed (2026-07-10).** `/verify 19.7`: all acceptance criteria PASS (three sweeps green; §31 landed + internally consistent; version alignment recorded; doc-consistency byte-exact). Independent `sqlgen-reviewer` pass took a **PRD-wide consistency angle** (sections the §8 sync plan did not name) and surfaced **4 doc gaps — all fixed inline** rather than carried: (1) §30.2's YAML example + field table now name the `mcp` block (it was a dangling forward-ref — §31.2 cited §30.2 as the block's home, but §30.2 never mentioned it); (2) §4.13 gained the two Phase 19 validation rows (emit-requires-enabled; project_configs placement) that §31.2's "§4.13 parallel" phrasing promised; (3) §8.3's Manifest artifact row now includes the module-root `.mcp.json`; (4) §23.8's "never touched" blanket statement now names its two governed exceptions (§30.3 manifest artifacts, §31.2 `.mcp.json`) — the §30.3 half was a pre-existing Phase 18 omission, widened and closed here. §31.3 tables re-confirmed byte-identical to MCP.md §4 after the edits (edits touched other sections only). 0 FIX entries logged (all gaps doc-only, closed inline). **Ready state: all tasks + tests checked, 0 open FIXes.**
