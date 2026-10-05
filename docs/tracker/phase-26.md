# Phase 26: Standard Library UUID as First-Class

Status: In Progress
PRD Sections: 7.2, 7.3, 7.4, 26.4.1, 4.13
Migration Plan: `docs/design/archive/UUID_MIGRATION.md`

---

## 26.0 PRD sync — stdlib UUID becomes the normative default

**PRD Reference:** Sections 7.2, 7.3, 7.4, 26.4.1, 4.13

**Status:** Complete

No code. Per project rule 1 the PRD is the source of truth, so every later
sub-item cites text this one writes. Blocks all.

### Tasks

- [x] §7.2 built-in type mappings — change the `uuid` row from `string` / `sql.NullString` / `*string` to `uuid.UUID` / `*uuid.UUID`, and confirm the `uuid[]` row and the "Array type rule" paragraph still read correctly when the base mapping is no longer `string`
- [x] §7.3 nullable handling — record that `uuid.UUID` is classified `nullPtrOnly` (no `sql.Null*` equivalent, nullable is always `*T`), alongside the existing `time.Duration` / `net.IPNet` members of that class
- [x] §7.4 — promote the standard library to the first UUID integration table; record `ImportPath` `uuid`, type `uuid.UUID`, zero value `uuid.UUID{}`, and **no** nullable wrapper
- [x] §7.4 — demote `github.com/google/uuid` and `github.com/gofrs/uuid/v5` to "alternative integrations", keeping their `uuid.NullUUID` pairing documented
- [x] §7.4 — correct the "If no UUID override is configured" sentence, which currently states `uuid` columns map to `string` / `sql.NullString`
- [x] §7.4 — record the three distinct zero-value spellings (`uuid.UUID{}` for google and stdlib, the `uuid.Nil` **variable** for gofrs, the stdlib's `Nil` being a *function*) as the reason `ZeroValue` is a per-integration field
- [x] §26.4.1 — gate the `NullUUID` scalar registry entry to the wrapper-backed integrations; note that the registry keys on the bare strings `uuid.UUID` / `uuid.NullUUID` and therefore serves either wrapper without change and cannot distinguish them, which is sound only while one-library-per-package holds
- [x] §4.13 — add the validation row for 26.2 (two UUID libraries resolving within one generated package → Error, resolution-level / phase 3)
- [x] §7.4 — state the one-library-per-generated-package rule and point at separate `output.dir` packages as the supported arrangement for projects needing both

### Acceptance Criteria

- The PRD's `uuid` default matches what 26.4 will emit; no section still claims `uuid` → `string`
- §7.4 reads as if the standard library were always the primary binding — no "migration", "now", or "newly" framing (the PRD is authoritative, not a changelog)
- Every 26.1–26.5 behavior has a normative sentence to cite
- The §4.13 row names the error class, its phase, and the remedy
- All internal anchors resolve; no sibling doc's `§7.x` cross-reference is invalidated

### Tests Required

- [x] Anchor/link validation across `docs/PRD.md` after the edits
- [x] Grep sweep confirming no remaining PRD claim that `uuid` maps to `string` by default

### Completion Record

**Completed 2026-09-11.** Files changed: `docs/PRD.md` (the deliverable — no code; 26.0 is the PRD sync), plus tracker bookkeeping in `docs/tracker/phase-26.md` and `docs/tracker/STATUS.md`.

**Edits, by section:**

- **§7.2** — PostgreSQL mapping table: `uuid` row `string` / `sql.NullString` / `*string` → `uuid.UUID` / `*uuid.UUID`; `uuid[]` row `[]string` → `[]uuid.UUID`. The **Array type rule** paragraph was rewritten: it now says the element is the base type's **resolved** mapping (built-in default or override), notes the base is always resolved in its non-null form so the element never carries a `sql.Null*` or pointer shape, and uses `numeric[]` → `[]decimal.Decimal` as the override example since `uuid[]` is no longer one. Verified against `resolveByType`, which resolves array bases through the full override chain with `nullable: false`.
- **§7.3** — new paragraph "**`use_pointers` applies only where a `sql.Null*` equivalent exists**": `uuid.UUID` is `nullPtrOnly` alongside `time.Duration` (`interval`) and `net.IPNet` (`cidr`); `use_pointers: false` does not produce a wrapper for these, and the lever for the wrapper shape is the integration choice, not the flag. Distinguished from the `nullRef` class (`[]byte`, `net.IP`, `types.JSON`).
- **§7.4** — UUID subsection rewritten around one integration table (stdlib first, then google, then gofrs) carrying `import` value / Go type / zero value / nullable Go type. New normative material: the stdlib's three properties (no null wrapper → `nullPtrOnly`; no `Valuer`/`Scanner` and none needed, with the `database/sql` special-case and the pgx `[16]byte` → `byte16Wrapper` → `UUIDCodec` route spelled out, plus the never-scan-into-`pgtype.UUID` rule); the GraphQL consequence (`UUID` scalar only, no `NullUUID`, no `MarshalNullUUID`/`UnmarshalNullUUID`, filters unchanged); "**Alternative integrations**" for google/gofrs keeping the `uuid.NullUUID` pairing; "**Three spellings of the zero UUID**" as the reason `ZeroValue` is per-integration; "**One UUID library per generated package**" with the per-package (not per-file) import-resolution cause, both failure shapes, why `stdsql` aliasing does not generalize, and the separate-`output.dir` remedy. The "If no UUID override is configured → `string` / `sql.NullString`" sentence is gone, replaced by the default statement in the opening paragraph. The §7.4 intro was reworded so it no longer implies every integration adds a `go.mod` entry.
- **§26.4.1** — registry table: `UUID` row source `third-party` → `stdlib or third-party` with a note that all three integrations spell the Go type `uuid.UUID`; `NullUUID` row gated to the wrapper-backed integrations. New paragraph "**UUID integrations and the `NullUUID` entry**" derives the gating from the existing resolved-`nullable.type` rule rather than as a special case, and records that the registry keys on the bare Go type strings, so one pair of entries serves either wrapper and **cannot distinguish them** — sound only while the §7.4 one-library-per-package rule holds.
- **§4.13** — new resolution-level (phase 3) row "**Two UUID libraries resolved in one generated package**", naming the sources it collects (global `overrides.types.uuid`, table- and view-scoped `overrides.types`, `column_map.<col>.import`), the error contents (both import paths, a selecting table, the `output.dir` remedy), and the per-package-not-per-module scope. The phase-3 enumeration below the table was extended to mention it.

**Consistency edits outside the five named sections** (each would otherwise contradict the new §7.4; called out for review):

- §1 module split — the illustrative `require github.com/google/uuid` comment changed from "only if generated code uses UUID types" to "only if a UUID override selects a third-party library".
- §3 package map — added the `gotype/uuidstd/` row (the package 26.1 creates) and added `gotype/uuidstd/` to the `gotype/` enumeration in "Type Integration Without Build Tags".
- §8.1 test-plan bullet — "`uuid[]` → `[]uuid.UUID` with override" → by default, with `numeric[]` as the override example.
- §26.4 GraphQL binding table — added a `*uuid.UUID` → `UUID` row (the default nullable shape, which the table had no entry for), and qualified the existing `uuid.NullUUID` and Go-`string` rows as reachable only under a wrapper-backed integration / an explicit override respectively.

**One normative claim was removed, not rewritten.** The old §7.4 bullet "UUID v4 and v7 generation support using the selected library's generator" is dropped, because it is false today and its correct replacement depends on an open decision (below). §8.6 still carries the `generation.uuid_version` requirement, so no requirement was lost.

**Tests:** anchor/link validation across `docs/PRD.md` — 526 headings, **404** internal links, **0 broken** (re-run after the post-decision round; link count rose from 393 as the new §7.4 / §28.8 / §8.6 cross-references landed). Markdown table integrity re-checked: 222 tables, raggedness unchanged at the 5 pre-existing rows, none of them in edited lines. Grep sweep — no remaining claim that `uuid` maps to `string` by default, and no remaining claim that event IDs use `google/uuid`. No sibling doc's `§7.x` cross-reference invalidated (no headings renamed, so `#73-nullable-handling` / `#74-built-in-type-integrations` are stable). `make check-examples` not triggered — the diff touches no path in the `/implement` path table.

**`make check` — clean, after a toolchain fix mid-sub-item.** The Step 3.5 preflight warned that no `.tool-versions` pin exists; the lint leg then failed inside GOROOT (`math/rand/v2`: *"method must have no type parameters"*) because golangci-lint 2.12.2 was built with go1.26.2 and cannot typecheck the go1.27.1 stdlib. Confirmed environmental, not introduced, by re-running with the diff stashed. `make vet test` was clean across all modules at that point. The user upgraded to golangci-lint 2.13.2 (built with go1.27.0) and the full `make check` then passed end to end — `make lint` alone: exit 0, 8 modules, `0 issues.` each; all 33 modules' unit tests green under `-race`. The `fmt` leg left the working tree untouched, so no gofumpt drift of the 16.8g kind. **Note for 26.1:** standalone `gofumpt` is still v0.10.0 built with go1.26.5 while golangci-lint's bundled copy is newer — harmless here because no Go file changed, but a `.tool-versions` pin is worth landing before 26.1 starts producing Go diffs.

**Auto-review pass — five further inconsistencies found and fixed inline.** The reviewer caught real under-reach: sections outside the five named ones that the retype left contradicting §7.4.

- **§9.8.3 FK map-key table** — the nullable row read "`*uuid.UUID` … guard with `.Valid`". A pointer has no `.Valid`; `DeriveScalarExtraction`'s pointer branch is `$v != nil` / `(*$v)`. Split into two rows: the pointer form (now the default nullable shape) and the wrapper form, each with its correct guard and unwrap.
- **§11.2 `String`-fallback prose** — listed `uuid.UUID` among override-only types "whose `driver.Valuer` renders a string … store into text columns". Wrong on three counts after the retype: it is a built-in default rather than override-only, the stdlib type has no `driver.Valuer`, and a PostgreSQL `uuid` is not a text column. `uuid.UUID` dropped from the list; `decimal.Decimal` / `ksuid.KSUID` remain correct.
- **§11.2 selection basis** — the comparator tables had **no** `uuid` entry at all, so 26.4's acceptance criterion "nullable UUID columns route to `comparator.NullableID`" had nothing normative to cite. Added the rule the code actually implements (`context_table.go`): `uuid` columns select `ID` / `NullableID` keyed on the **SQL type**, integration-independent, covering non-PK/non-FK and view columns, with the reason the key is not the FK-conversion shape (it would misclassify `decimal` and `time`).
- **§9.8.4 create snippet** — used `id, err := uuid.NewV4()`, the **gofrs** two-value signature, which matches no other integration (google has no `NewV4`; the stdlib's returns one value). Rewritten to the single-value shape the generator actually emits for v4, with the per-integration spelling question pointed at §8.6 / §7.4 rather than pre-decided — that belongs to blocker B-2.
- **§7.4 import mechanism** — "every emitted file receives the union of the package's imports" overstated it: the union seeds each file and a usage-based prune follows (`orchestrate.go` / `format.go`). Reworded; the conclusion is unchanged, since the prune cannot separate two packages both spelled `uuid.`.

**One reviewer finding was resolved by adding normative text rather than deferring:** 26.3's behavior (integration detection/enrichment applies at table and view scope, not only globally) had no sentence to cite. §7.4 "Configuration Shorthand" gained **"Integration detection is scope-independent"**, stating that identical override text resolves identically at every scope, that table-scoped overrides get the same FK-extraction metadata back-fill, and that an explicit user value is never overwritten at any scope.

**One reviewer finding was escalated, not fixed:** §28.8 normatively mandates `google/uuid` for event IDs (and names a `uuid.Must` the stdlib lacks), so it now contradicts §7.4 directly. Amending it either way *is* the B-1 decision, so 26.0 left it alone and folded it into the B-1 write-up instead.

**Two blockers surfaced for 26.4 / 26.5; the user resolved both the same day (2026-09-11) — fix the hardcoded event-ID library to follow the selected integration, and fix the `uuid.Must(...)` spellings as part of this migration.** The decisions made their PRD side 26.0's business, so a second round of normative text landed:

- **§7.4 "Generating UUID values"** — a new per-integration table giving the v4 and v7 calls in both **value** and **string** forms, for all three integrations. It replaces the bullet 26.0 had dropped, and it is now the single normative source the other call sites cite. Every cell was measured, not recalled: `go doc` against `go1.27.1`, `github.com/google/uuid`, and `github.com/gofrs/uuid/v5 v5.5.1` (fetched into a scratch module, since gofrs is in no module graph yet). The surfaces disagree more than the old code assumed — only google exports `New()`, only google exports `NewString()`, `Must` exists in google and gofrs but **not** the stdlib, and the stdlib's constructors return a bare `UUID` where the other two return `(UUID, error)`. The stdlib v4 cell is `uuid.NewV4()`, not `uuid.New()`, on purpose: `New` is documented as "an algorithm suitable for most purposes" and is not contractually v4, so an explicit `uuid_version: v4` is honored with the call that names the version. The table also records the no-integration fallback — a package with no `uuid` column uses the stdlib spellings, so events cost no module dependency.
- **§28.8 Event ID generation** — rewritten. It previously mandated the hardcoding outright ("Event IDs use `google/uuid` … `uuid.Must(uuid.NewV7()).String()`"), contradicting §7.4 and naming a `uuid.Must` the stdlib lacks. It now specifies the selected integration's string-form call, states that the event-hooks file imports that one library and no other (so the one-library-per-package rule holds by construction), and records the stdlib fallback.
- **§8.6** and **§9.8.4** — both now cite the §7.4 table rather than spelling calls of their own; §9.8.4's snippet is `uuid.NewV4()` for the default binding at the default version.

The code work is scheduled as the new sub-item **26.3a**, placed between 26.3 and 26.4 because it **must land before 26.4 and 26.5** — 26.5 migrates three events-enabled examples (`tenancy`, `graphql`, `tenancy_postgres`) to the stdlib, which without 26.3a would emit two UUID imports in one generated package. Full analysis of both issues, including the measured API-surface table, is in the "Decision Record" section at the end of this file.

---

## 26.1 `uuidstd` integration package

**PRD Reference:** Section 7.4

**Status:** Complete

Additive. Registers the standard library as a known integration; does not change
the default. Opting in via `overrides.types.uuid` is the only way to reach it
until 26.4.

### Tasks

- [x] Create `cmd/sqlgen/gotype/uuidstd/uuidstd.go`, mirroring the shape of `uuidgoogle/` and `uuidgofrs/`
- [x] `const ImportPath = "uuid"`
- [x] `SQLTypes() []string` returning `[]string{"uuid"}`
- [x] `Override() config.TypeOverride` with `Type: "uuid.UUID"`, `Import: ImportPath`, `ZeroValue: "uuid.UUID{}"`, and **no** `Nullable` field set
- [x] Register in `knownIntegrations` in `cmd/sqlgen/gotype/gotype.go`
- [x] Verify `enrichOverride` does not back-fill `Nullable` from a *different* detected integration when a project declares both — an integration with an empty `Nullable.Type` must leave it empty

### Acceptance Criteria

- A config naming `import: uuid` resolves non-null `uuid` columns to `uuid.UUID` and nullable ones to `*uuid.UUID`, via `goTypeFromOverride`'s existing value-typed branch
- Zero value resolves to `uuid.UUID{}` (the stdlib's `Nil` is a function, so the literal is required)
- FK string conversion derives as `FKStringStringer` from the `.UUID` suffix rule in `DeriveFKMethod`
- `DeriveScalarExtraction` on `*uuid.UUID` yields guard `$v != nil`, unwrap `(*$v)`, method `FKStringStringer`
- No generated output changes for any existing example (the default is untouched)

### Tests Required

- [x] Resolver unit test: `import: uuid` → `uuid.UUID` (non-null) and `*uuid.UUID` (nullable)
- [x] Resolver unit test: zero value is `uuid.UUID{}`, not `uuid.Nil`
- [x] Resolver unit test: FK extraction on a nullable UUID FK produces the pointer guard/unwrap pair
- [x] `enrichOverride` test: an integration with empty `Nullable.Type` is not back-filled from another detected integration
- [x] Golden regeneration byte-identical across all 13 examples

### Completion Record

**Completed 2026-09-11.** Files **belonging to 26.1**: **new** `cmd/sqlgen/gotype/uuidstd/uuidstd.go`; **modified** `cmd/sqlgen/gotype/gotype.go` (imports, `knownIntegrations` entry, `enrichOverride` cross-library guard, deterministic `detectIntegrations` iteration) and `cmd/sqlgen/gotype/gotype_test.go` (four new test funcs). **Three Go files, no generated output.** The working tree at completion also carries the separate gqlgen v0.17.95 upgrade (5 `go.mod`/`go.sum` pairs and 146 regenerated gqlgen-owned files) — that is infrastructure work recorded in the phase note below, **not** part of this sub-item, and it changed no sqlgen-generated file.

**The integration itself is small and needed no new machinery.** Every acceptance criterion was already satisfied by existing code, verified by reading it before writing anything: `goTypeFromOverride`'s value-typed branch yields `*uuid.UUID` for a nullable override with no `Nullable.Type`; `DeriveFKMethod` returns `FKStringStringer` off the `.UUID` suffix; `DeriveScalarExtraction` falls to its pointer branch for `*uuid.UUID` (confirmed `*uuid.UUID` is not a `knownWrapperExtractions` key, which is consulted first) giving `$v != nil` / `(*$v)`. `uuidstd` is registered **first** in `knownIntegrations`, ahead of google and gofrs.

**The verification task found a real defect, and fixing it was in scope.** The task asked to confirm `enrichOverride` does not back-fill `Nullable` from a *different* detected integration. It did. A probe run 200× returned `uuid.NullUUID` **200/200** for a stdlib-bound `uuid` column — deterministic, not a race.

Root cause is the pairing in `detectIntegrations`, not `enrichOverride`'s field logic: it walks each detected integration's own `sqlTypes` and enriches whatever global override sits at that key, without checking the override names that library. All three UUID integrations claim `uuid`, so any config pulling a second UUID library into detection (an override on another SQL type is enough) had that library's wrapper written onto the first one's import — `uuid.NullUUID` alongside `import "uuid"`, which declares no such identifier. Order-independent: whichever integration carries a non-empty `Nullable` wins, because the empty one leaves the field `""` for the next pass to fill.

The guard went into `enrichOverride` rather than the call site deliberately: **26.3 adds table- and view-scoped detection**, which will call the same function, and a guard on the function is inherited by that work automatically. The rule is that an integration only enriches an override belonging to it — `user.Import != "" && user.Import != integ.Import` returns the override untouched. An override naming **no** import is not a competing claim and stays eligible, which preserves the existing shorthand where `decimal` is enriched from a `numeric`-declared shopspring override.

**Tests** (`cmd/sqlgen/gotype/gotype_test.go`): `TestStdlibUUIDIntegration` (non-null; nullable pinned at **both** `use_pointers` settings, the §7.3 `nullPtrOnly` claim; zero value is the literal and explicitly not `uuid.Nil`; no convert config; `uuid[]` → `[]uuid.UUID`), `TestStdlibUUIDFKExtraction` (nullable FK yields the pointer guard/unwrap/Stringer triple, plus the bare form), and `TestIntegrationNullableNotBackfilledAcrossLibraries` (five subtests: stdlib not given google's wrapper, google's wrapper not blanked by stdlib, gofrs's `uuid.Nil` not overwritten, same-library enrichment still works, import-less override still enriched). **Failing-first verified** — with the guard removed the back-fill subtest fails with `nullable stdlib uuid = "uuid.NullUUID", want *uuid.UUID`; restored, it passes.

**A second defect at the same call site, found by the auto-review: nondeterministic generated output.** The sibling `else` branch of the pairing loop calls `RegisterIntegration` for any SQL type the user did **not** override, with no ownership check — and `detected` is a **map**, so when two UUID libraries are in play the registration was a last-writer-wins race over iteration order. Measured on a config declaring `text` → google and `varchar` → stdlib with no `uuid` override: a nullable `uuid` column resolved to `*uuid.UUID` **151/200** runs and `uuid.NullUUID` **49/200**. The same config, different Go type, run to run.

Fixed by iterating `detected` in sorted import-path order (`slices.Sorted(maps.Keys(...))`), which restores the deterministic-output guarantee (PRD §5.7) without deciding *which* library ought to win for an unclaimed SQL type — that config is ambiguous and 26.2 rejects it outright. `TestIntegrationDetectionIsDeterministic` pins it; failing-first verified at **10/10 runs failing** without the fix and **10/10 passing** with it. The fix is adjacent to 26.1's task list rather than on it, and was taken here because 26.1 adds the *third* claimant on the `uuid` SQL type and the fix is three lines in a function this sub-item already touches.

The reviewer's companion finding — that an **import-less** override is still library-ambiguous when two libraries are detected, because the guard's `user.Import != ""` carve-out lets either enrich it — is real in principle but did **not** reproduce in a 200-run probe here (ZeroValue was stable at `uuid.UUID{}` 200/200). It is subsumed by 26.2's rejection either way. Two further reviewer findings are **routed, not fixed**: the deeper ownership question for the `else` branch (which library *should* win, versus merely a stable answer) belongs with 26.2's validation, and `enrichOverride` never filling `Import` on an import-less override — measured `{Name: decimal.NullDecimal, Import: ""}`, unreachable in any example — belongs with 26.3.

**Golden byte-identity — all 12 examples, verified under Go 1.27.** This was initially provable for only 9: the three gqlgen-invoking examples (`graphql`, `graphql_top_level`, `mysql`) failed with `gqlgen subprocess failed: exit status 1`, which stashing this sub-item's changes and re-running showed to be **pre-existing and unrelated** — the same three failed byte-for-byte the same way on HEAD. The interim evidence was a full pass of all 12 under `GOTOOLCHAIN=go1.26.0`. That gqlgen break was then diagnosed and **fixed** (phase note below), and `TestE2EGoldenFiles` now passes **all 12 examples under Go 1.27.1** with these changes in place, tree clean. `make check` and `make check-examples` are both green. Independently of any of that, the new guard **cannot fire in any example**: each of the 12 configs declares at most one distinct UUID import path (7 declare exactly one — all `github.com/google/uuid` — and 5 declare none, so `uuidstd` is detected nowhere), so no example reaches the cross-library branch.

**Note for the phase — a pre-existing gqlgen break, diagnosed and RESOLVED 2026-09-11 by upgrading gqlgen. Was not sqlgen's bug; no longer blocks 26.4/26.5.**

**Symptom.** Under the local Go 1.27 toolchain, `TestE2EGoldenFiles` fails for the three gqlgen-invoking examples (`graphql`, `graphql_top_level`, `mysql`) and `make check-examples` fails in `graphql/tests`. Both reproduce identically on HEAD with this sub-item's changes stashed.

**Why it looked undiagnosable.** gqlgen exits 1 with **empty stdout and stderr**. `main.go:212-218` sets `log.SetOutput(io.Discard)` unless `--verbose`, and the failure arrives as a `log.Fatalf` — so the message goes to the discard sink and the process dies silently. Running the same binary with `--verbose` prints it: `internal error: package "github.com/vektah/gqlparser/v2/ast" without types was imported from "github.com/99designs/gqlgen/graphql/introspection"`.

**Root cause.** That `log.Fatalf` is `golang.org/x/tools/go/packages/packages.go:1214`, reached when an imported package's `Types` is nil or incomplete. gqlgen v0.17.90 loads with a mode (`internal/code/packages.go:15-20`) of `NeedName | NeedFiles | NeedTypes | NeedSyntax | NeedTypesInfo | NeedModule` — it does **not** request `NeedDeps` or `NeedImports`. Under Go 1.27, x/tools v0.42.0 no longer populates dependency types for that mode, so the type-checker's importer finds an incomplete dependency and aborts.

**Isolated away from gqlgen entirely.** A ~25-line program using only `x/tools/go/packages` with gqlgen's exact mode reproduces the identical `Fatalf`; adding `NeedDeps | NeedImports` to the same call makes it load cleanly (`types=true complete=true errs=0`). Same program, same x/tools v0.42.0, same target package, varying **only** the toolchain: **`GOTOOLCHAIN=go1.26.0` → complete; `GOTOOLCHAIN=go1.27.1` → `Fatalf`.** So the trigger is the Go version, and the defect is the loader mode.

**Proof.** `TestE2EGoldenFiles` under `GOTOOLCHAIN=go1.26.0` passes **all 12 examples** with 26.1's changes in place, leaving the tree clean; the two `graphql/tests` wrapper tests pass there too. This also upgrades 26.1's own golden evidence: the goldens are byte-identical across **all 12** examples, not merely the 9 reachable under Go 1.27.

**Why CI is still green.** Every workflow installs Go via `go-version-file: <module>/go.mod`, and every `go.mod` declares `go 1.26.0`. CI therefore runs the toolchain where gqlgen works. The break is local-only *today*.

**Why it blocks this phase.** 26.4/26.5 emit code importing the standard library `uuid` package, which requires Go 1.27. Raising the `go` directive — which that work forces — moves CI onto the toolchain where gqlgen dies, and 26.5 must regenerate every golden including the three gqlgen examples. **This must be resolved before 26.4/26.5 land**, and it belongs with the deferred toolchain work rather than with 26.1.

**Fix options, in preference order.** (1) Upgrade gqlgen to a release whose loader requests `NeedDeps`/`NeedImports` — the upstream fix, and the only one that removes the constraint. (2) Pin `GOTOOLCHAIN=go1.26.0` for the gqlgen subprocess only, via `wrapper.subprocessEnv`, keeping the rest of the build on 1.27 — viable but pins codegen to an old toolchain. (3) Point `gqlgen_bin` at a patched build. **Do not** change `GOWORK` handling: an earlier diagnosis recorded here blamed the root `go.work`, and that was wrong — `cmd/sqlgen/e2e_test.go:134` already sets `GOWORK=off` and `subprocessEnv` inherits it. That theory is retracted.

**Resolution (2026-09-11, user direction: upgrade gqlgen to latest).** gqlgen **v0.17.90 → v0.17.95** across the five example modules that require it (`graphql`, `graphql_null_wrappers`, `graphql_top_level`, `mysql`, `sqlite`), which carries **x/tools v0.42.0 → v0.49.0**.

The fix came entirely from x/tools, not gqlgen: v0.17.95's loader mode is **byte-identical** to v0.17.90's — still `NeedName | NeedFiles | NeedTypes | NeedSyntax | NeedTypesInfo | NeedModule`, still no `NeedDeps`. Verified in the isolated probe before touching the repo: holding the mode and Go 1.27.1 constant and varying only x/tools, **v0.42.0 fatals and v0.49.0 loads cleanly** (`types=true complete=true errs=0`). So option (1) from the list above was taken, but the mechanism is an upstream x/tools repair rather than a gqlgen loader change — worth knowing, because a future gqlgen bump that *lowers* its x/tools floor would regress this.

gqlgen v0.17.95 requires `go 1.26`, which the examples' existing `go 1.26.0` directive already satisfies — the upgrade needed no go-directive bump and is independent of the still-pending Go 1.27 work.

**Golden churn: 146 generated `.go` files plus the 5 `go.mod`/`go.sum` pairs.** Every one of the 146 is **gqlgen-owned** — checked by generator header — so **no sqlgen-generated output moved**, which is the property that matters: the upgrade did not disturb anything sqlgen emits. The content delta is upstream gqlgen: deferred-field handling gains `FieldSetView` / `deferLabelToView`, and `fmt.Fprint(...)` in enum `MarshalGQL` becomes `_, _ = fmt.Fprint(...)`, explicitly discarding returns.

**Verification:** `TestE2EGoldenFiles` passes **all 12 examples under Go 1.27.1** (previously 3 failed), leaving the tree clean; `make check` clean. This also supersedes the `GOTOOLCHAIN=go1.26.0` workaround recorded above — no toolchain pin is needed for gqlgen any more.

Two side notes from the same investigation: a *failing* golden run **deletes** the gqlgen-owned files (`graph/generated_gen.go`, `graph/model/models_gen.go`) in three example trees before dying, so `git checkout -- cmd/sqlgen/testdata/examples/` is needed after one (a passing run leaves the tree clean); and clearing a 44 GB `GOCACHE` changes nothing, so this is not stale build-cache skew.

---

## 26.2 Reject mixed UUID libraries in one generated package

**PRD Reference:** Sections 4.13, 7.4

**Status:** Complete

Must land before 26.4. Today a two-library config emits a package that does not
compile, and the diagnostic names neither the config key nor the cause.

### Tasks

- [x] Add a resolution-level (phase 3) validation that collects the distinct UUID import paths resolved across the generated package, including table-scoped `overrides.types` and `column_map.<col>.import`
- [x] Error when more than one distinct UUID import path is resolved, naming both paths, the tables that selected each, and the remedy (split across `output.dir` packages)
- [x] Wire into the same path `sqlgen validate` and `sqlgen generate` both traverse
- [x] Confirm views are covered — a view column carrying a UUID override participates

### Acceptance Criteria

- A global stdlib override plus a table-scoped `github.com/google/uuid` override fails with a clear config error rather than emitting a broken package
- The error names both import paths and at least one selecting table
- Single-library configs — stdlib, google, or gofrs — are unaffected
- Two *separate* generated packages in one module, each with its own library, remain legal (this validation is per generated package, not per module)
- `sqlgen validate` reports it without writing files

### Tests Required

- [x] Failing-first config test: global stdlib + table-scoped google → error (must fail before the validation exists)
- [x] Negative test: each of the three libraries alone → no error
- [x] Test that `column_map.<col>.import` naming a second library is caught, not just `overrides.types`
- [x] Test the two-package arrangement still generates and compiles
- [x] `sqlgen validate` surfaces the error with the same message as `generate`

### Completion Record

**Completed 2026-09-11.** Files **belonging to the sub-item proper**: **new** `cmd/sqlgen/gen/uuid_library.go` (the rule) and `cmd/sqlgen/gen/uuid_library_test.go` (its tests); **modified** `cmd/sqlgen/gen/resolved_names.go` (the shared phase-3 entry point), `cmd/sqlgen/gen/orchestrate.go` (three call sites rewired), `cmd/sqlgen/gen/export_test.go` (one test shim). Also carried, by user direction rather than by the task list: `cmd/sqlgen/config/config.go` and `cmd/sqlgen/config/scalar_binding_validate_test.go`, an adjacent pre-existing bug the auto-review surfaced and the last section of this record covers in full. **Seven Go files, no generated output moved.**

**The rule reads resolved contexts, not config text**, which is the same choice `collectResolvedNames` makes and is what makes it complete. Each of the config surfaces §4.13 enumerates — global `overrides.types`, table-scoped `overrides.types`, and `column_map.<col>.import` — has already been through the resolution chain by the time the contexts exist, and all three land in `ColumnContext.Import` (verified by reading `resolveColumnGoType`, which applies `column_map` before delegating, and `goTypeFromOverride`, which promotes `nullable.import` over the base import when one is declared). A rule written against the config would have to re-implement that chain to know which surface won for a given column, and would still have to guess which columns the schema has. Reading the contexts is also what scopes the rule correctly: an override for a SQL type no column uses resolves nowhere and is not a claim, and a table `exclude_tables` drops — or one with no resolved primary key — never reaches a context. Both are pinned as negative tests.

**It keys on the resolved Go type's qualifier, not on a list of the three known import paths.** A claim is a column whose resolved `GoType` is qualified `uuid.` *and* carries a non-empty `Import`; the rule fails when the distinct import paths behind those claims number more than one. This is deliberately one notch more general than "the three supported libraries": the qualifier is what the emitted file actually spells, so a vendored copy or a fork of any UUID package — `github.com/myorg/uuid` reached through `column_map.<col>.import` — is caught on exactly the same terms, where a hardcoded list would emit the broken package the rule exists to prevent. It cannot over-fire: two distinct imports both bound to `uuid` genuinely cannot coexist, since `generatedImportAliases` aliases `database/sql` and nothing else. `typeQualifier` is pinned separately over every shape the resolver emits (`uuid.UUID`, `*uuid.UUID`, `uuid.NullUUID`, `[]uuid.UUID`, `[]*uuid.UUID`, `map[string]uuid.UUID`) plus the unqualified negatives.

**One scope extension past the enumerated list, taken deliberately:** the three members of `TypeContext` — composite attributes, a domain's base type, and `extras.<T>.fields.<f>` — each carry their own resolved type and import and render into **one** file, `output.types.file`. §4.13's em-dash list names the three column-bearing config surfaces, but the rule it states is over "every UUID import path resolved across the generated package", and all three of these are resolved across it. (The extras half shipped first; composites and domains were added in the auto-review pass below, which is where the inconsistency of covering one file-mate and not the other two surfaced.)

**Wiring: one entry point, not three call sites.** `validateResolvedPackage` joins `validateResolvedNames` and `validateUUIDLibraries` and is now what `buildAllContexts` (generate), `ValidateGeneration` (validate) and `BuildEntityContextsFromSchema` (`graphql gen`) each call. Adding the check at the three sites individually would have reached whichever were remembered — which is how `graphql gen` once became the only path that never checked for a shared file stem (FIX-162). All three entry points are covered by a test.

**Failing-first, verified by stubbing the rule to `return nil`.** Without it, the four error-expecting subtests of `TestValidateUUIDLibraries` fail, as do all three entry-point tests; the two-package negative control still passes. The failure the rule replaces was also measured rather than recalled: with the rule stubbed, a global stdlib override plus a table-scoped `github.com/google/uuid` override emits a `models_gen.go` whose import block contains **both** `"uuid"` and `"github.com/google/uuid"` — `uuid redeclared in this block` — with every `uuid.UUID` reference in the file bound to whichever import wins.

**The error names what §4.13 requires.** Example, verbatim:

```
2 UUID libraries resolved in one generated package: "github.com/google/uuid" (selected by table orders, column "id") and "uuid" (selected by table users, column "id") — every supported UUID integration binds the file-local package name "uuid" and exports a type named UUID, so a package importing more than one cannot compile; split them across separate output.dir packages
```

Both import paths, one selecting entity each, the remedy. The count is rendered rather than fixed at "two", so a three-library config reads correctly and names all three (pinned). Ordering is deterministic — import paths sorted, and the entity named for each is the first claimant in context order, which is sorted by (schema, name) with columns sorted within — so the message does not churn run to run (PRD §5.7).

**Views are covered, and the PRD names a config key that does not exist.** View columns participate: they are collected from `ViewContext.Columns` on the same terms as table columns, pinned by a case where the view is the *only* claimant of one of the two libraries and the error renders it schema-qualified (`view public.order_summaries`). But PRD §4.13's row and §7.4's "Integration detection is scope-independent" paragraph both name **`views.<view>.overrides.types`**, and no such key exists: `config.ViewConfig` has no `Overrides` field, and `cmd/sqlgen/config/schema/v1.json`'s `view` definition has seven properties (`api`, `cache`, `cursor_keys`, `invalidate_on`, `sql`, `struct_name`, `tenancy`) against the table definition's twenty. A view's columns therefore reach a UUID type only through the **global** override, which is what this rule collects. Nothing was invented to close the gap (project rule 1) — **26.3's task list names the same non-existent key** ("Extend integration detection to table-scoped and view-scoped `overrides.types` entries"), so the decision of whether the PRD sentence is wrong or the config is missing a field belongs to 26.3 or to a PRD amendment, and is flagged rather than guessed.

**Test sweep.** `make check` clean across all 8 modules (`0 issues.` each, unit tests green under `-race`). `make check-examples` clean across all 12 example modules under Docker (PostgreSQL + MySQL integration suites included). `TestE2EGoldenFiles` passes all 12 examples leaving the tree clean — **zero golden churn**, which is the expected result and is also structurally guaranteed here: surveying every example's `sqlgen.yml`, seven declare exactly one UUID import (`github.com/google/uuid` — `graphql`, `graphql_null_wrappers`, `graphql_top_level`, `postgres`, `postgres_stdlib`, `tenancy`, `tenancy_postgres`) and the other five declare none, so no example reaches the multi-claim branch. The rule is a pure no-op for every config in the tree today; the first config it can fire on is one 26.5 could create by leaving a table-scoped override behind during the example migration, which is exactly why it lands first. The same holds for the `packageNameForPath` fix below: the four examples declaring scalars use `int64`, `net/netip.Addr` and `github.com/segmentio/ksuid.KSUID`, none carrying a `/vN` element to strip.

**All three sweeps were re-run end to end on the final combined tree** — after the auto-review's composite/domain extension and after the `QualifiedGoType` fix — not only on the first cut: `make check` exit 0 (8 modules), `make check-examples` exit 0 (12 example modules under Docker), `TestE2EGoldenFiles` 12/12 with the tree left clean. Three green sweeps, one per state of the diff.

**Two lint findings surfaced by the new tests and fixed in place**, both in the test file: `gosec` G304 on the helper that reads back a file the test just generated (annotated with a reason, per the no-bare-`//nolint` rule), and `unparam` reporting that `namedView`'s `schema` parameter always received `""` — newly reachable because this sub-item added the second and third call sites. Fixed by giving the view-participation case a real schema, which also pins the schema-qualified rendering in the error.

**Auto-review pass — one real gap found and fixed inline, two doc inaccuracies corrected.**

- **Composite and domain contexts were an uncollected claim surface, and the fix is the same six lines the `extras` extension was.** `buildCompositeContexts` and `buildDomainContexts` resolve through the same resolver and carry `Import` (`context_type.go:19-68`), and `templates/types.go.tmpl` renders composites, domains and extras into **one** file — `output.types.file`, emitted by `generateTypes` with a nil declared-import set, so goimports resolves a single `uuid` binding for all three. The rationale used to include `extras` — "resolved across the generated package" — applies verbatim to its two file-mates, and excluding them was an inconsistency, not a scope boundary. The gap is reachable: composites and domains resolve with `tableOverrides = nil`, so they always take the **global** binding, and a package whose only `uuid` columns sit behind table-scoped or `column_map` overrides naming a different library would have passed the rule and then mis-bound — the "quieter one is worse" failure this file's header describes. Both are now collected; a composite names its field, a domain claims as a whole because it is a one-line alias with no member to name (`"uuid" (selected by domain type actor_id)`). **Failing-first re-verified**: removing only the composite and domain loops fails exactly the two new subtests and nothing else.
- **An ordering claim was wrong.** The comment said columns are sorted within their contexts; `buildTableColumns` preserves DDL order. Determinism still holds — DDL order is stable across runs — but the stated reason did not, so it now says what is actually true of each list.
- **`typeQualifier`'s one blind spot is now recorded rather than implied.** A map with a *qualified key* (`map[a.K]uuid.UUID`) would report the key's qualifier. It is unreachable — the only map the resolver emits is `map[string]any`, and a two-package type expression could only arrive through a single `column_map.<col>.import`, which cannot name the second package anyway — but the passing `map[string]uuid.UUID` test case read as general coverage, so the limit is stated in the doc comment.

Four test cases added: a composite attribute on the global library against a table-scoped override, the same for a domain, an `extras` field naming a second library, and the negative where all four surfaces agree on one library. That last one initially failed on a *different* phase-3 rule — the fixture's composite `audit_ref` and extra `AuditRef` resolve to one Go type name — which is `validateResolvedNames` working correctly; the extra was renamed so the case tests what it is for.

**One reviewer note was a real pre-existing bug, and was fixed in this pass by user direction** (`cmd/sqlgen/config/config.go`, `cmd/sqlgen/config/scalar_binding_validate_test.go`). `config.QualifiedGoType` derived a package name with `path.Base`, so `github.com/gofrs/uuid/v5` rendered `v5.UUID`. `/vN` is a *module path* requirement from v2 on and not part of the package name — the package inside still declares `package uuid` — so the bare last element is wrong for every v2+ module.

**It was investigated before it was fixed, and it is not a 26.5 blocker; the record should not claim otherwise.** Three measurements:

- **Nothing on the codegen path derives a package name from an import path.** `path.Base` / `filepath.Base` appear six times in `cmd/sqlgen` and exactly one is in a package-name role — this function. The rest are filesystem paths.
- **`gen` keys `builtInScalarRegistry` on the resolved Go type string**, and a gofrs-bound column carries `uuid.UUID` (the override says so literally), so it routes to the `UUID` scalar correctly. `scalarImports` collects the **full** `GoImport`, so `graph/scalars_gen.go` imports `github.com/gofrs/uuid/v5` and spells `uuid.UUID` — correct, because the package name *is* `uuid`.
- **26.5 does not touch the affected key.** It moves `graphql_null_wrappers` to gofrs through `overrides.types.uuid`; `QualifiedGoType`'s only caller validates `api.graphql.scalars.<name>.go_type`. The four examples that declare scalars use `int64`, `net/netip.Addr` and `github.com/segmentio/ksuid.KSUID` — none versioned, none registry-owned.

So the bug is confined to one validation and has two faces, both narrow: a `go_type` reaching a registry-owned type through a v2+ module is **not** told it collides (the §26.4.1 "registry is authoritative" rule misses it, leaving an entry that binds nothing while gen routes the column to the built-in scalar regardless), and the two errors that *do* quote the qualified form name a package appearing nowhere in the consumer's config or code. The second is the better reason to fix it: gofrs becomes a documented integration at 26.5, so `/vN` paths stop being hypothetical here, and a diagnostic quoting a non-existent package is worse than none.

**The fix** strips a trailing module major-version element before taking the base (`packageNameForPath` / `isModuleMajorVersion`). `v0` and `v1` are never spelled in a module path and a leading zero is not a major version, so a package genuinely named `v1`, `v0` or `v02` survives; `vector` is not mistaken for one either. **Deliberately unchanged:** the pre-modules `gopkg.in/yaml.v3` spelling carries its version *inside* the last element rather than as one of its own, so it still renders `yaml.v3` — its existing test case is untouched, no registry import uses that form, and changing it would be a second guess with no caller to justify it.

**Failing-first, both levels.** With the call reverted to `path.Base`, exactly the two `/vN` cases of `TestSplitGoType` fail while the three guard cases (`v1`, `v02`, `vector`) pass either way — proving the guards are guards and not accidental passes — and the new end-to-end case in `TestValidatePreParse_ScalarBinding` fails, which is the user-visible half: `github.com/gofrs/uuid/v5.UUID` is now rejected with `resolves to "uuid.UUID", which is owned by the built-in scalar "UUID"`.

**26.2's own rule shares no such derivation** — `typeQualifier` reads the qualifier off the resolved *type string* and never off the import path, which is precisely why it has no equivalent bug, and is independent confirmation that reading the type rather than the path was the right choice.

**One reviewer partial left standing.** The "two-package arrangement generates **and compiles**" test generates both packages and parses each emitted import block, asserting each imports its own library and not the other; nothing in the tree compiles a two-package arrangement. Building one would mean a new example module and golden set for a layout no consumer config exercises yet, which is 26.5's call if it wants the coverage — flagged rather than built.

---

## 26.3 Fix the `detectIntegrations` global-vs-table asymmetry

**PRD Reference:** Section 7.4

**Status:** Complete

Pre-existing bug, independent of this migration but adjacent enough to fix in
the same pass. `detectIntegrations()` walks only `r.globalOverrides`, so
identical override text resolves differently depending on nesting depth.

### Tasks

- [x] Resolve 26.2's flag: PRD §4.13 and §7.4 name a `views.<view>.overrides.types` key that does not exist. **User decision (2026-09-11): remove it from the PRD; do not add the config field.** Both sentences amended; views resolve under the global scope alone
- [x] Extend integration detection to table-scoped `overrides.types` entries, not just `r.globalOverrides`
- [x] Confirm `enrichOverride`'s documented rule — integration null types are always used for nullable columns — then applies consistently at both scopes
- [x] Audit whether any existing example's golden output depends on the current asymmetric behavior; regenerate if so and record the churn

### Acceptance Criteria

- A table-scoped `overrides.types.uuid` naming `github.com/google/uuid` with no `nullable:` resolves to `uuid.NullUUID`, matching what the same text does at global scope
- A table-scoped override that *does* name `nullable:` keeps its explicit value (explicit user values still win)
- Table-scoped overrides gain the same FK-extraction metadata back-fill (`UnderlyingField`, `ValidField`) that global ones already get
- Any resulting golden churn is understood and recorded, not silently accepted

### Tests Required

- [x] Resolver unit test: identical override text at global and table scope resolves identically
- [x] Resolver unit test: an explicit table-scoped `nullable:` is not overwritten by enrichment
- [x] Resolver unit test: table-scoped enrichment back-fills `UnderlyingField` / `ValidField`
- [x] Full golden regeneration; any churn attributed to this fix and reviewed

### Completion Record

**Completed 2026-09-11.** Files changed: **new** `cmd/sqlgen/gotype/scope_test.go`; **modified** `cmd/sqlgen/gotype/gotype.go`, `cmd/sqlgen/gotype/gotype_test.go`, `docs/PRD.md`, plus tracker bookkeeping in `docs/tracker/phase-26.md` and `docs/tracker/STATUS.md`. **Three Go files and one PRD file; no generated output moved.**

**26.2's open question is closed by user decision, and the PRD moved, not the config.** PRD §4.13 and §7.4 both named `views.<view>.overrides.types`, which does not exist — `config.ViewConfig` has seven fields and no `Overrides`, `schema/v1.json`'s `view` definition has the same seven properties, and `context_view.go` resolves every view column with a nil override scope — all three resolver calls pass `nil, nil` for the type map and the override scope (one `Resolve`, two `ResolveAggregate`), with a fourth path going straight to `FromLiteral` for `@type`-annotated and aggregate-inferred columns. Asked rather than guessed, per project rule 1; the user's answer was **"remove that from the PRD and from the config"**. So §4.13's row now reads "global `overrides.types.uuid`, table-scoped `overrides.types`, and `column_map.<col>.import`" and adds that view columns participate through the global binding they resolve under — which is what 26.2's rule already collects, so that sentence documents shipped behavior rather than changing it. §7.4's "Integration detection is scope-independent" paragraph drops the third scope and gains a closing sentence stating that the two are the only ones, that a view resolves under the global scope alone, and that per-view type control is a column's `@type` annotation in the view SQL (verified: `context_view.go:204` routes `col.GoTypeLiteral` through `gotype.FromLiteral`). **No config field was added.**

**The bug, stated precisely.** `detectIntegrations` ran once in `NewResolver` over `r.globalOverrides` and nothing else. A table-scoped `overrides.types` map was handed straight to `Resolve` and met step 2 of the chain raw — no detection, no enrichment — so three lines of YAML meant one thing at the top of `sqlgen.yml` and another under `tables.<t>:`. A `github.com/google/uuid` override with no `nullable:` gave `uuid.NullUUID` globally and `*uuid.UUID` at table scope, with nothing in the consumer's config to explain the difference.

**The fix is one shared detector and one scope-aware step 2.** `detectIntegrationsIn(map) map[string]integration` is now the single implementation of "which integrations does this override map select", called by both scopes — the point being that the two *cannot* drift apart again, which is the failure mode the sub-item exists to close (and the same reasoning 26.2 used for `validateResolvedPackage`). `scopeIntegrations(scope)` is the table-scoped twin of `detectIntegrations`, and splits its result the same two ways the global path does: `declared` is the scope's own entries with integration defaults filled in, `activated` is what the integration contributes for SQL types the scope claims but did not spell out. `resolveByType` places them at the steps their global counterparts occupy — `declared` at step 2, `activated` at step 4 ahead of `r.integrations` — so the chain in PRD §7.1 is unchanged in shape.

**Enrichment is `enrichOverride`, reused verbatim.** 26.1 put the cross-library guard inside that function rather than at its call site *explicitly so this sub-item would inherit it*, and that paid off: the table-scoped path gets the guard for free, pinned by a case where one scope names the stdlib and a sibling entry names google and the stdlib entry keeps its empty `Nullable`. The same reuse is what makes the "integration null types are always used for nullable columns" rule hold identically at both depths — it is not re-implemented anywhere.

**Two deliberate choices worth naming.**

- **The scope map is cloned, never mutated.** `r.globalOverrides` is the resolver's own map and `detectIntegrations` enriches it in place; a table's map is the parsed config, shared with every other reader of it, so writing through it would make resolution depend on which table was built first. Pinned by `TestTableScopedOverridesAreNotMutated` and by a per-case check in the white-box table.
- **Recomputed per call, not cached.** A table's overrides block is a handful of entries and the detector is a few string compares; when the scope selects no integration — every table-scoped block in the tree today — `scopeIntegrations` returns the caller's own map with nothing allocated. A cache keyed on map identity would buy nothing measurable and would cost the resolver mutable state.

**"Activates that integration" was read to include the integration's other SQL types**, which is the half that goes past the acceptance criteria and is called out for review. shopspring/decimal claims both `numeric` and `decimal`; globally, declaring either one has always bound both. Leaving the table scope with only the enrichment half would have fixed one asymmetry and left its twin — a table-scoped `numeric` override with the table's `decimal` columns still on `float64` — which is the same bug in the same function. The PRD sentence 26.0 wrote says "activates that integration *and* receives the same enrichment", so both halves are the faithful reading; §7.4 now spells the consequence out with the `numeric`/`decimal` example and states the step-5 rank, because the old wording did not say it in so many words.

**Golden churn: zero, and provably so rather than observed.** `TestE2EGoldenFiles` passes all 12 examples leaving the tree clean. The audit behind that number enumerated every table-scoped `overrides.types` block in the tree through the real config loader and classified each entry:

| Example | Table | Entries naming a known integration | Newly activated siblings |
|---|---|---|---|
| `mysql` | `warehouses` | `decimal` → shopspring, `nullable:` **declared** | `numeric` |
| `postgres` | `warehouses` | `numeric` → shopspring, `uuid` → google, both `nullable:` **declared** | `decimal` |
| `postgres_stdlib` | `warehouses` | `numeric` → shopspring, `uuid` → google, both `nullable:` **declared** | `decimal` |
| `sqlite` | `warehouses` | `real` → shopspring, `nullable:` **declared** | `numeric`, `decimal` |
| `graphql` | `events`, `scalar_probes` | none — `types`, `time`, and bare literals | — |

Two facts make the zero structural. **Every** table-scoped entry naming a known integration already declares `nullable:` explicitly, so the enrichment half has nothing to fill that a consumer would see; all it back-fills is `Nullable.UnderlyingField`, which is invisible downstream because both wrappers it can name (`uuid.NullUUID`, `decimal.NullDecimal`) are in `knownWrapperExtractions` and `DeriveScalarExtraction` answers from that table regardless. And **every** newly activated sibling names a SQL type no column in that table uses — checked against the schemas, not inferred from the clean tree: `mysql.warehouses` is `DECIMAL(10,2)` with no `NUMERIC`, `postgres` / `postgres_stdlib` are `NUMERIC(10,2)` with no `DECIMAL`, `sqlite` is `REAL` with neither. The first config either half can move is one a consumer writes; the tree today cannot reach them.

**Failing-first, measured.** With `scopeIntegrations` stubbed to `return scope, nil`, **21 subtests fail**: 9 of the 10 white-box enrichment cases, 10 of the parity cases (exactly the nullable-side ones, plus both gofrs non-nullable cases, whose zero value `uuid.Nil` is itself a back-fill), and 2 of the 4 activation cases. The other three pass either way and are the negative controls — the scope that names no known library, the table with no overrides block, and the explicit-global-override precedence case.

**Tests.** `cmd/sqlgen/gotype/scope_test.go` is white-box (`package gotype`) for one reason, recorded in its header: the field the acceptance criterion names — `Nullable.UnderlyingField` — is *not* observable from a resolved `GoType`, for the `knownWrapperExtractions` reason above, so asserting the back-fill from the outside is impossible. It covers the ten enrichment shapes, the empty-scope fast path, and `detectIntegrationsIn` keyed on the import rather than on the SQL type it sits under. The black-box additions to `gotype_test.go` carry the parity statement — `TestIntegrationDetectionIsScopeIndependent` resolves nine override shapes × `usePointers` × `nullable` (36 subtests) at both scopes and requires the two `GoType`s to be equal, deliberately asserting no literal, since the literals are already pinned per-integration above it — plus the sibling-activation cases with their precedence controls, and the non-mutation guard.

**Sweeps.** `make check` exit 0 on the final tree — 8 modules, lint clean, all unit tests green under `-race` (`cli` 744s, `gen` 487s). `make check-examples` exit 0 across all 12 example modules under Docker. `TestE2EGoldenFiles` 12/12, tree clean. **Step 3.5 preflight:** `gofumpt v0.12.0 (go1.27.1)`, `golangci-lint 2.13.2 built with go1.27.0`, `go1.27.1`; still no `.tool-versions` / `tools.mk` pin file, so the warning 26.0 and 26.2 recorded stands — but the specific 16.8g hazard is gone for now, since standalone gofumpt is finally built with the same Go as the toolchain (it was v0.10.0/go1.26.5 at 26.2), and the `fmt` leg left the working tree untouched. No security review triggered: the diff touches neither an embedded runtime package nor a `cmd/sqlgen/cli/**` subcommand that reads external files.

**Auto-review pass — three findings, all three addressed here; two were text and one was a routed question this sub-item owed an answer to.**

- **A stale comment quoting the amended §4.13 list, fixed inline.** `cmd/sqlgen/gen/uuid_library.go:71` still enumerated "global `overrides.types`, table- **and view-scoped** `overrides.types`, and `column_map.<col>.import`" — the list §4.13 just dropped view scope from. Corrected; a repo-wide sweep confirms no other Go comment names a view-scoped override.
- **The new §7.4 sentence overclaimed, and was narrowed.** As first written it said "Identical override text resolves to an identical mapping at both scopes; nesting depth is not part of the resolution" — unqualified and normative, where the code makes that true for **integrations** and not for a **user-declared** wrapper, which reaches FK extraction through `registerWrappersFromOverrides` (still global-only, the gap the last section of this record covers). Writing normative text asserting a property the code does not have is worse than the gap itself, so the sentence now says identical text selects the same integration and resolves to the same Go type, zero value and nullable variant, and states that the claim is about the built-in integrations. An earlier draft pointed the reader at §7.5 for the custom-wrapper behavior; that was dropped on checking §7.5, which says nothing about scope — a cross-reference to text that does not exist is the same defect one level down.
- **26.1's routed question is answered: it is not a 26.3 asymmetry, and the record now says which bug it actually is.** `phase-26.md` routes "`enrichOverride` never filling `Import` on an import-less override" to this sub-item, and the white-box table pins a case that exercises it, so 26.3 owed a disposition rather than a silent pin. Measured rather than reasoned about: `overrides.types.numeric: {type: decimal.Decimal}` with no `import:` and no sibling carrying one resolves to `Name: "decimal.Decimal", Import: ""` — **identically at global and at table scope**. So it is not an asymmetry at all and 26.3's parity claim is untouched by it; it is a pre-existing hole in config validation, at both depths equally. `validateColumnTypeLiterals` (`context_table.go:652`) rejects exactly this shape for the two **per-column** forms — a package-qualified `type_map` value, and a `column_map.<col>.type` with no sibling `.import` — and `overrides.types.<sql_type>.type`, the by-SQL-type form, is checked by nothing. The result compiles only by accident, when some other column pulls the same package in, which is what the enrichment case in the test table is doing. **Not fixed here, deliberately.** The candidate fix — having `enrichOverride` fill `Import` from the integration when the user leaves it empty — is a change to the *global* path too, and it changes what 26.2's rule counts as a claim (an import-less `uuid:` entry in a google-detected package would start carrying google's import and become a claim). That is a behavior change to two shipped rules dressed as a drive-by in a sub-item about scope symmetry. The test case now says in-place that the empty `Import` records today's behavior and is deliberately not settled there.

**One adjacent asymmetry found and deliberately left standing, because it is a different bug in a different function.** `registerWrappersFromOverrides` also walks `r.globalOverrides` and `r.integrations` only, so a **user-declared** Null wrapper written at table scope never reaches `r.userWrappers`, and `DeriveScalarExtraction` falls through to the bare-type branch for it. The tree has exactly one such declaration — `postgres.warehouses.overrides.types.region_name` → `customtypes.OptionalString` with the non-default `underlying_field: Val` / `valid_field: Set` — and it is inert today because `warehouses.region` is a plain domain column with no `REFERENCES`, so no O2M loader ever asks for its extraction. Fixing it is not a variation on this sub-item's fix: enrichment is a pure function of one scope map, but `userWrappers` is resolver state consulted by `DeriveScalarExtraction(goType string)`, which has no scope argument, so the repair is either a `NewResolver` signature change (96 test call sites) or a new registration entry point that every construction site must remember to call — the FIX-162 shape 26.2 argued against. Flagged rather than built, and rather than silently widened into.

---

## 26.3a Per-integration UUID generation expressions

**PRD Reference:** Sections 7.4 ("Generating UUID values"), 8.6, 28.8

**Status:** Complete — **`/done` gate cleared 2026-09-14.** All three FIXes `/verify 26.3a` logged are
Resolved: **FIX-201** (`blocking`) — `tenancy.type` was a fourth UUID import surface `collectUUIDClaims`
did not read, which this sub-item turned from a silently-agreeing hardcode into a package emitting both
`"uuid"` and `"github.com/google/uuid"`; **FIX-202** (`blocking`) — the lint workflows were broken on
every module, and turned out to have been broken since the scaffolding commit rather than since 26.3a
(`golangci-lint-action@v6` rejects golangci-lint v2 outright), fixed together with a `.tool-versions`
pin; **FIX-203** (`tracked`) — the import-less `overrides.types.uuid` item this sub-item routed, which
on investigation was open at three config surfaces rather than one and, through `extras.<T>.fields`,
defeated the §4.13 one-library rule this sub-item depends on. No FIX surfaced by `/verify 26.3a`
remains open; the three still-open entries (FIX-198, FIX-199, FIX-200) are all `tracked` and come from
phases 26, 13 and 12 by way of docs consolidation and a design review, not from this sub-item.
Everything else verified PASS — see the Verification Record below.

Resolves B-1 and B-2 together — they share one seam. Both call sites that emit a
UUID-generating call must resolve it from the package's selected integration
rather than hardcoding google's spelling. **Must land before 26.4 and 26.5**: 26.5
migrates three events-enabled examples to the stdlib, and without this they would
emit two UUID imports in one generated package.

### Tasks

- [x] Expose the package's selected UUID integration from the resolver — import path plus the v4/v7 value and string generation expressions, per PRD §7.4 "Generating UUID values"
- [x] Fall back to the standard library when the package selects no UUID integration (no `uuid` column), so an events-only package adds no module dependency
- [x] `cmd/sqlgen/gen/context_event.go` — replace the hardcoded `"github.com/google/uuid"` import and the hardcoded `uuidExpr` with the resolved integration's import and **string-form** expression
- [x] `cmd/sqlgen/gen/funcmap.go::funcPKAutoGenType` — derive both the value and string forms from the resolved integration instead of the google-shaped table
- [x] Confirm the event-hooks import and the column-resolved import are always the same library, so 26.2's rule holds by construction rather than by validation

### Acceptance Criteria

- A stdlib-bound package with events enabled emits `import "uuid"` only — no `github.com/google/uuid` anywhere in the generated tree — and its `go.mod` requires no UUID module
- A google-bound package with events enabled is byte-identical to today's output (`uuid.NewString()` for v4 is a change from `uuid.New().String()` only if the current expression is kept; pin whichever is chosen in the completion record)
- A gofrs-bound package with events enabled compiles — it does not today
- `uuid_version: v7` compiles under all three integrations, for both a `uuid.UUID` PK and a PK overridden to `string`
- An app-strategy UUID PK compiles under all three integrations at both versions
- A package with no `uuid` column but `events.enabled: true` generates and compiles with no UUID module in `go.mod`

### Tests Required

- [x] Failing-first: a stdlib-bound events-enabled config emits two UUID imports before the fix, one after
- [x] Codegen test per integration × `{v4, v7}` × `{value PK, string PK}` asserting the emitted expression matches the PRD §7.4 table exactly
- [x] Event-hooks codegen test per integration asserting the import and the string-form expression
- [x] Compile test: a gofrs-bound events-enabled package builds (closes the pre-existing gofrs break)
- [x] Events-only package (no `uuid` column) has no UUID module in its resolved `go.mod`
- [x] Golden regeneration: `cache` / `tenancy` / `graphql` / `tenancy_postgres` event-hooks churn attributed

### Completion Record

**Completed 2026-09-11.** Files changed: **new** `cmd/sqlgen/gotype/uuidgen.go`, `cmd/sqlgen/gotype/uuidgen_test.go`, `cmd/sqlgen/gotype/uuidgen_compile_stdlib_test.go`, `cmd/sqlgen/gotype/uuidgen_compile_google_test.go`, `cmd/sqlgen/gotype/uuidgen_compile_gofrs_test.go`, `cmd/sqlgen/gen/uuid_generation.go`, `cmd/sqlgen/gen/uuid_generation_test.go`; **modified** `cmd/sqlgen/gotype/uuidstd/uuidstd.go`, `uuidgoogle/uuidgoogle.go`, `uuidgofrs/uuidgofrs.go`, `cmd/sqlgen/gen/context.go`, `context_event.go`, `funcmap.go`, `orchestrate.go`, `templates/table/create.go.tmpl`, `templates/table/upsert.go.tmpl`, seven `gen` test files, **12 example goldens** (six examples × `expected/` + `models/`), **21 `go.mod`/`go.work` files** for the Go 1.27 floor, three example `go.mod`s for the dropped UUID requirement, and `guidelines/ARCHITECTURE.md` / `TEMPLATES.md` / `TESTING.md` / `docs/tracker/IMPLEMENTATION_ORDER.md`.

**The seam is one function per half, and both halves read the same string.** `gotype.UUIDIntegration` carries an import path plus the four expressions of PRD §7.4 "Generating UUID values"; the spellings themselves live on the integration packages as `V4Expr` / `V7Expr` / `V4StringExpr` / `V7StringExpr`, next to the `ImportPath` and `Override()` they already own, and `gotype.UUIDIntegrationFor(path)` keys them. `gen.selectUUIDIntegration` answers which one the package chose, and `attachUUIDGeneration` wires it into both call sites. The string forms are stored rather than derived because they are not derivable: google's v4 string form is `uuid.NewString()`, a different call, not `uuid.New()` with `.String()` appended.

**Selection is read off the built contexts, not off the resolver — this is the one place the implementation departs from the task text, and it is the departure that makes the acceptance criterion true.** The sub-item says "expose the package's selected UUID integration **from the resolver**". A resolver-side answer can only be "what does the SQL type `uuid` resolve to under the *global* override scope", and that is wrong in three reachable shapes: (a) a table-scoped `overrides.types.uuid` on a different library than the global one, where only the table-scoped one has columns — the global binding resolves nowhere, so 26.2 sees one claim and passes, and the event hooks would then import the *other* library; (b) `column_map.<col>.import`, which never passes through `overrides.types` at all; (c) **MySQL and SQLite, which declare no `uuid` SQL type** — a consumer binds `uuid.UUID` there through `column_map`/`type_map` on a `CHAR(36)`, so the global `uuid` lookup is empty and a resolver-side answer would say "standard library" for a google-bound package. So the selection reuses 26.2's `collectUUIDClaims`, which already walks every table, view, composite, domain and extras field and reports the import path each *resolved* to. That is what makes the fourth task — "confirm the event-hooks import and the column-resolved import are always the same library **by construction**" — literally true: they are the same string, taken from the same collector, rather than two computations that happen to agree. The lowest path is taken when more than one is somehow present, purely so the answer stays deterministic (PRD §5.7) if the one-library rule is ever relaxed; `validateResolvedPackage` has already rejected that case by the time it runs.

**The google v4 event ID changed; the acceptance criterion left the choice open and the PRD closes it.** The criterion reads "byte-identical to today's output (`uuid.NewString()` for v4 is a change from `uuid.New().String()` only if the current expression is kept; pin whichever is chosen)". PRD §7.4's table gives google's v4 string cell as `uuid.NewString()` and §28.8 names it explicitly, so **`uuid.NewString()` is what ships** and a google-bound events package is *not* byte-identical — three examples move. Project rule 1 decides it: the PRD is the source of truth, and the alternative would have been to keep emitting a spelling the PRD does not list.

**`pkAutoGenType` left the funcmap rather than gaining an argument.** The expression is a pure function of (version, PK Go type, package binding) and all three are known before rendering, so it is now `TableContext.PKAutoGenExpr`, pre-computed — which is the rule TEMPLATES.md §6 already states and which the `funcmap.go` header already cites for `toSnakeCase` / `toPlural`. Keeping it as a funcmap entry would have cost a context field *anyway* (the binding has to reach the template somehow) plus a template function on top, and would have forced either a third parameter through `FuncMap` / `FuncMapWithResolver` / `loadTemplates*` or a reordering of `Generate` so contexts build before templates load. The registry entry is gone and a comment in its place says why, next to the two other deliberate non-registrations.

**The import is declared, not inferred.** `attachUUIDGeneration` appends the selected import to an app-strategy table's `Imports`, for the same reason `arrayScanImports` declares `lib/pq` and `assembleTableContext` declares `errgroup`: goimports resolves a bare `uuid.` by scanning the module cache, so a consumer whose cache has never held the library gets a file referencing it with no import. It matters most for a primary key overridden to a Go `string` — no column then resolves to a uuid-qualified type, so nothing else in that table's import set names the library the generated call spells.

**An unknown UUID library keeps its own import and takes the standard library's spellings.** A consumer can point `overrides.types.uuid.import` at a library sqlgen has no built-in knowledge of. The PRD defines no spellings for it, so `UUIDIntegrationFor` falls back to the standard library's — but it does **not** fall back to the standard library's *import*, because that would add a second package named `uuid` to the package, which is precisely the failure the one-library rule exists to prevent and which `validateUUIDLibraries` cannot see in an emitted-file import. Strictly better than before, where such a package got google's import *and* google's spelling; still a guess about the unknown library's API, and recorded here as one.

**A post-build pass is a footgun, so both guidelines now name it.** `PKAutoGenExpr` cannot be filled while one table context is being built — the binding is a whole-package fact — so it joins `wireRelationshipFKMetadata` and `attachTenancyToTables` as a pass that runs after `buildAllContexts` has everything. A hand-built app-strategy fixture that omits it renders `pkValue = ` and does not parse, which is exactly how it surfaced: `reserved_fields_pin_test.go` calls `BuildTableContexts` directly and three of its probe shapes broke. TEMPLATES.md §6 gained a "UUID Generation Expression — Wiring Rule" section and TESTING.md §11 gained the matching fixture rule, both modelled on the FK-metadata rule that exists for the same reason (FIX-059 / FIX-068 / FIX-069). `BuildEntityContextsFromSchema` runs the pass too — it emits no table file today, but it *returns* contexts, and an incomplete returned context is a trap for the next caller.

**Failing-first, measured in both halves rather than asserted.** With `context_event.go` reverted to the hardcoded import and expression, `TestGenerate_StdlibEventsPackageImportsOneUUIDLibrary` reports `generated package imports "github.com/google/uuid" in [event_hooks_gen.go], want "uuid" alone` — two UUID libraries in one package, the columns on `uuid` and the hooks on google, which is the shape 26.5 would have produced three times over; `TestGenerate_EventsOnlyPackageSelectsStdlib` fails the same way, and four of the six `TestBuildEventHooksContext_PerIntegration` subtests fail on the expression. With `uuidGenExpr` reverted to the google-shaped table, `TestGenerate_PKAutoGenExprPerIntegration` fails on **stdlib/v4, stdlib/v7, gofrs/v4** — google/v4, google/v7 and gofrs/v7 pass either way and are the negative controls (gofrs and google agree on v7, and google is what the old table spelled).

**The gofrs compile test is a real compile, and it also covers the other two.** `uuidgen_compile_{stdlib,google,gofrs}_test.go` each bind one library to the local name `uuid` — legal because import names are file-scoped, and the only way to spell all three the way a generated file does, since every one of them binds `uuid`. Each file declares `[]uuid.UUID{…}` and `[]string{…}` literals holding that library's four cells verbatim, so a spelling that does not exist fails the **build**, and the element types make the value/string split load-bearing rather than cosmetic. Each then asserts `UUIDIntegrationFor(...)` returns those same four strings, tying the compiled expressions to what the generator emits; the two halves are textually identical by design and the file says to edit them together. This is what closes "a gofrs-bound events-enabled package compiles — it does not today": nothing in the tree had ever compiled a gofrs spelling, because no example exercises `uuidgofrs` (26.5 fixes that half). It costs `cmd/sqlgen` two **test-only** requires, `github.com/gofrs/uuid/v5 v5.5.1` and `github.com/google/uuid v1.6.0`; their hashes resolve through `go.work.sum` (which gained the two gofrs lines and shed five stale entries in the same rewrite — four `/go.mod`-only, plus the `golang.org/x/sys v0.43.0` `h1:` line, whose hash survives in `cmd/sqlgen/go.sum`) and the sibling modules' `go.sum` for google/uuid; no module `go.sum` moved, which is consistent with `cmd/sqlgen` having always depended on the workspace — its `go.mod` requires neither the runtime nor the parser module, so `GOWORK=off go mod tidy` cannot run there at all.

**Golden churn: 12 files across six examples — two more than the sub-item predicted.** The task named `cache` / `tenancy` / `graphql` / `tenancy_postgres`; the actual set is every example with `events.enabled: true`, which also includes **`events`** and **`tenancy_mysql`**. It splits in two:

| Examples | Binding | Before | After |
|---|---|---|---|
| `cache`, `events`, `tenancy_mysql` | none — no `uuid` column | `import "github.com/google/uuid"`, `uuid.New().String()` | `import "uuid"`, `uuid.NewV4().String()` |
| `graphql`, `tenancy`, `tenancy_postgres` | `github.com/google/uuid` | `uuid.New().String()` | `uuid.NewString()` |

Only `event_hooks_gen.go` moved in each — no model, client, cache or GraphQL file changed. **`postgres_stdlib` is byte-clean and is the load-bearing negative**: it is the one example in the tree with an app-strategy primary key (`uuid` SQL type, no `DEFAULT`, resolved to a Go `string`), and its table-scoped `warehouses` override makes the package google-bound, so `pkValue = uuid.NewString()` is both the old hardcoded spelling and the new resolved one. The create/upsert path is therefore covered by a real example *and* unchanged by the change, which is the strongest evidence available that the PK half moved only where it should.

**The three events-only examples no longer require a UUID module.** `github.com/google/uuid` was a **direct** require in `cache`, `events` and `tenancy_mysql` for exactly one reason — the hardcoded event-hooks import — and it is now `// indirect` in all three (it stays in the graph transitively, through testcontainers / modernc). The line was moved by hand rather than by `go mod tidy`, because tidy also pulled unrelated version bumps (`golang.org/x/sync` 0.19→0.20, `modernc.org/sqlite` 1.48.1→1.48.2, `containerd/platforms` 0.2.1→1.0.0-rc.1 and a dozen more) that have nothing to do with this sub-item.

**The Go 1.27 floor moved, by user decision, repo-wide.** This sub-item forces it: the three events-only examples now emit `import "uuid"`, and **`go vet` rejects the standard library `uuid` package in a module declaring `go 1.26.0`** — *"uuid.NewV4 requires go1.27 or later (file is go1.26)"* — where `go build` alone accepts it. Measured in an isolated two-file module, not inferred: `go build` exits 0 and `go vet` / `go test` fail, and the workspace's own `go` directive does **not** raise a member module's language version (a module at `go 1.26.0` inside a `go 1.27.0` workspace still fails), so every module needs its own line. `make check-examples` runs `vet` and `test`, so this was fatal rather than cosmetic. The 26.1 phase note had already recorded this as the "still-pending Go 1.27 work" blocking 26.4/26.5, and its own blocker — gqlgen dying under Go 1.27 via x/tools — was fixed during 26.1 by the gqlgen v0.17.95 upgrade. Given the choice between a minimal bump (three examples plus `cmd/sqlgen`) and the full move, **the user chose the full move**: all eight `go.mod`s, `go.work`, and all 12 example `go.mod`s now read `go 1.27.0`, matching PRD §1's stated target. CI installs Go per module from `go-version-file`, so the whole matrix moves to 1.27 with it. No `go.sum` needed changing — an early `go work sync` rewrote several of them with newer workspace-selected versions, and that churn was reverted and confirmed unnecessary (`make vet` clean across all eight modules with the committed sums).

**Sweeps.** `make check` exit 0 — 8 modules, `fmt` left the tree untouched, lint `0 issues.` per module, vet clean, all unit tests green under `-race`. Lint caught two things on the first pass and both were fixed rather than suppressed: staticcheck `QF1011` on the compile pins' `var _ T = expr` form (rewritten as typed slice literals, which keeps the type load-bearing and lets the values actually be used) and `prealloc` on an append loop (removed). `TestE2EGoldenFiles` 12/12 byte-clean after regeneration, tree clean. All 12 example modules build and `go vet` clean under `GOWORK=off` at the new Go floor. **Step 3.5 preflight:** `gofumpt v0.12.0 (go1.27.1)`, `golangci-lint 2.13.2 built with go1.27.0`, `go1.27.1`; still no `.tool-versions` / `tools.mk` pin file, so the warning 26.0 / 26.2 / 26.3 recorded stands, though the 16.8g hazard remains absent (standalone gofumpt is built with the same Go as the toolchain). No security review triggered: the diff touches neither an embedded runtime package nor a `cmd/sqlgen/cli/**` subcommand that reads external files or URLs.

**Auto-review pass — one real FAIL, fixed inline; three notes, two acted on and one routed.**

- **PRD §8.1.3's funcmap registry table still listed `pkAutoGenType`.** TEMPLATES.md was updated and the PRD was not, so the normative registry named a function the code no longer registers. Fixed in place, using the "…is not registered" form the Naming row already established, and stating the reason (the call depends on a package-wide fact no table context can see) so the entry explains itself rather than just going quiet.
- **`ValidateGeneration`'s wiring comment claimed to mirror the generate path "in the same order".** That stopped being exactly true the moment `attachUUIDGeneration` was added to `buildAllContexts` and not there. Reworded to say it mirrors the passes that can *reject* a config, and to name the one deliberately left out and why it is unobservable on that path (validate renders no table file).
- **The PK matrix ran under `single_file` only, and `sqlite` — the tree's one `file_per_table` example — has neither events nor an app-strategy PK.** So the import half of the fix had no `file_per_table` coverage anywhere. `loadLayoutConfig` already takes the layout, so the matrix gained a third axis: 3 integrations × 2 versions × 2 layouts = 12 subtests. Safe by mechanism (imports resolve per package, then prune by usage), but the mechanism is exactly what a test should hold still.
- **Routed, not fixed: an import-less `overrides.types.uuid` silently changes which library an events-enabled package selects.** `collectUUIDClaims` skips a resolved `uuid.UUID` whose `Import` is empty, so `overrides.types.uuid: {type: uuid.UUID}` with no `import:` now selects the standard library for the event hooks where the hardcoded import previously bound google. This is the same hole 26.3 measured and deliberately left standing — `overrides.types.<sql_type>.type` is checked by no validation, unlike the two per-column forms `validateColumnTypeLiterals` covers — and it is benign once 26.4 flips the default. Fixing it is a config-validation change to a shipped rule, not a drive-by in this sub-item; it belongs in a `tracked` FIX if `/verify` agrees.

**Two notes for 26.4 and 26.5.** (1) 26.5's example list should be re-read against the six-example churn set above — `events` and `tenancy_mysql` are events-enabled and were not in its plan, though neither has a `uuid` column so neither needs a `sqlgen.yml` edit. (2) The Go floor is no longer a 26.4/26.5 prerequisite; it is done.

### Verification Record

**`/verify 26.3a` run 2026-09-14 — 5/6 PRD requirements PASS, 1 FAIL; 6/6 required tests present; two `blocking` and one `tracked` FIX logged.**

**PASS, verified rather than taken from the completion record.** All 12 cells of PRD §7.4's "Generating UUID values" table match the constants in `uuidstd` / `uuidgoogle` / `uuidgofrs` exactly, and are pinned twice — literally in `gotype/uuidgen_test.go`, and as compiled code against the three real libraries in the `uuidgen_compile_*_test.go` pins. `BuildEventHooksContext` takes the resolved `gotype.UUIDIntegration` and uses it for both the import and `UUIDGenExpr`, so §28.8's "that one library and no other" holds by construction for every surface `collectUUIDClaims` reads. The golden churn is exactly the claimed 12 `event_hooks_gen.go` files across six examples, split as documented, with `postgres_stdlib` byte-clean. The Go floor is complete — all 20 `go.mod` files and `go.work` read `go 1.27.0`, no module missed. `github.com/google/uuid` is correctly demoted to the `// indirect` block in `cache`, `events` and `tenancy_mysql`. The updated fixtures are integration-diverse on purpose (`create_test` gofrs-bound, `upsert_test` google-bound), so they assert spellings that match their own imports.

**FAIL — `tenancy.type` is a fourth UUID import surface, and this sub-item made it fatal (FIX-201, `blocking`).** `collectUUIDClaims` walks table columns, view columns, composite fields, domain base types and `extras` fields, but not `TenancyContext.Import`. Reproduced: postgres, `events.enabled: true`, `tenancy.type` naming google, a tenanted table with an app-strategy `uuid` PK, and no `overrides.types.uuid`. `sqlgen generate` exits 0 — `validateUUIDLibraries` cannot see the tenancy surface — and `models_gen.go` imports both `"uuid"` (declared by `attachUUIDGeneration`, because selection found no claims and fell back to the standard library) and `"github.com/google/uuid"` (from `tenancy.type`). `go build`: `uuid redeclared in this block`. **Confirmed a regression, not a pre-existing hole**, by generating the identical config with the pre-26.3a binary at `f463493`: it emits google alone in every file, and `go build` reports no redeclaration. (The throwaway probe module reported unrelated `undefined: database` / `sql` / `comparator` / `omittable` errors in *both* runs — an artifact of that minimal environment, identical before and after; the load-bearing difference is that `uuid redeclared in this block` appears only after.) The old hardcoded google spelling was silently agreeing with tenancy; removing the hardcode exposed the missing surface. **26.4 does not fix it** — the column would then claim the standard library while `tenancy.type` still names google — and it is live for 26.5's **`tenancy`** and **`graphql`** migrations — the only two examples declaring a `tenancy.type`. Both escape today only because their global `overrides.types` supplies an agreeing google column claim (`blob` in `tenancy`, `uuid` in `graphql`); migrating those to the standard library while leaving `tenancy.type` on google is exactly the reproduced config.

**FAIL — the Go 1.27 floor breaks CI's lint job (FIX-202, `blocking`).** **[Diagnosis superseded
2026-09-14 — do not cite this paragraph as prior art.** The symptom and the reproduction below are
accurate, but they are the *second* of two stacked breakages. `golangci-lint-action@v6` rejects any
golangci-lint **v2** version during resolution, before a binary is downloaded, so the lint jobs had
been broken since the scaffolding commit `662b534` rather than since 26.3a, and bumping only the
`version:` field would not have fixed them. See the amended FIX-202 entry in `docs/tracker/fixes.md`.]** `lint.yml:24` and `examples.yml:48` pin `golangci-lint v2.10.1`; both workflows install Go from `go-version-file: <module>/go.mod`, now `go 1.27.0`. The released v2.10.1 binary is built with go1.26.0 and refuses outright: *"can't load config: the Go language version (go1.26) used to build golangci-lint is lower than the targeted Go version (1.27.0)"* — reproduced locally with the pinned release binary against the root module. All 8 lint matrix legs and every `examples.yml` lint leg fail. The local toolchain was upgraded to 2.13.2 during 26.0 for the adjacent version of this failure; the workflow pins never moved with it, which is the drift the missing `.tool-versions` keeps enabling.

**Test-coverage gap, not logged separately.** No test pins the *declared import* for the shape `attachUUIDGeneration`'s comment calls load-bearing — a package where no column resolves uuid-qualified but an app-strategy `string` PK exists. `assertOnlyUUIDLibrary` is package-scoped, so the `file_per_table` subtests do not pin per-file import presence either. FIX-201's regression test covers the same seam and should close this.

**Gate closure (2026-09-14) — one routed prediction was wrong, and the record should say so.** The
completion record's routed bullet judged the import-less `overrides.types.uuid` case "benign once 26.4
flips the default," and `/verify` accepted that in logging FIX-203 as `tracked`. Investigating it for
implementation showed the reasoning held only for `overrides.types.uuid` specifically. The same hole was
open at `tables.<t>.overrides.types` and at `extras.<T>.fields`, and at the third it is not benign under
any 26.4 default: an import-less `uuid.UUID` extras field beside a google-bound column produces a package
importing `github.com/google/uuid` in `models_gen.go` and the standard library `uuid` in `types_gen.go`,
with `validateUUIDLibraries` passing it — the §4.13 mis-binding 26.2 exists to forbid, reached through a
surface the rule cannot see, because an import-less declaration arrives as a *non*-claim. The severity
was left at `tracked` by user decision (nothing in the tree triggers it), but the "converges on the
standard library" reasoning should not be reused as prior art. Two further corrections to the diagnosis,
both measured: goimports back-fill means such a config does not merely "compile by accident" — it binds
the standard library `uuid` Go 1.27 ships, or whatever the *generating machine's* module cache holds
(against PRD §5.7), or nothing at all while `generate` still exits 0. Fixed in `c503081`.

**The test-coverage gap noted below is now closed, and closing it changed a behavior.** FIX-201's
`TestGenerate_TenancyTypeSelectsUUIDIntegration` did *not* close it, as the note below predicted: its
`workspace_id` resolves uuid-qualified through `tenancy.type`, so the import it asserts could come from
that column rather than from `attachUUIDGeneration`'s own declaration. Nor did the other candidates —
`TestGenerate_PKAutoGenExprPerIntegration` deliberately puts its string-PK table in a package with a
`uuid.UUID` table, `TestGenerate_StdlibEventsPackageImportsOneUUIDLibrary`'s PK column resolves
uuid-qualified, and `TestGenerate_EventsOnlyPackageSelectsStdlib` has no app-strategy PK at all.

Constructing the gap shape showed why it was worth pinning. With `overrides.types.uuid` bound to
**google** and the table's single `uuid` PK retyped to a Go `string` by `type_map`, the generated package
imported the **standard library** `uuid` and emitted `uuid.NewV4().String()`: `collectUUIDClaims` found no
uuid-qualified column, so `selectUUIDIntegration` fell through, and the consumer's declared library was
ignored for value generation. Nothing about the output was broken — it compiled, and both libraries make
valid v4 UUIDs — which is why it went unnoticed. It was simply not the library the config named. §7.4
stated the fallback condition as "it has **no `uuid` column**", where the implementation's predicate was
"no column *resolves* uuid-qualified"; those diverge exactly when every uuid column is retyped away.

**Resolved by user decision (2026-09-14): fall back to the globally configured UUID package and version.**
`selectUUIDIntegration` is now a three-step cascade — resolved columns, then the global `overrides.types`,
then the standard library — with the new `gotype.UUIDIntegrationIn` answering step 2 by import path rather
than by key, so a library declared under a non-`uuid` SQL type counts (the `tenancy` example declares
google under `blob`). The version half needed no change: `resolveUUIDVersion` already cascades
`primary_key.uuid_version` → `generation.uuid_version` → v4, and the event hooks already read
`cfg.Generation.UUIDVersion`. Step 3 is unchanged, so the §7.4/§28.8 property that a consumer naming no
UUID library anywhere acquires no module dependency from event IDs still holds — pinned by
`TestGenerate_NoUUIDAnywhereStillCostsNoDependency`. PRD §7.4's one-sentence fallback rule was replaced by
the three-step cascade; **this is a spec change, not a documentation correction**, made on the user's
explicit decision. Zero golden churn across all 13 examples: every one either resolves a uuid-qualified
column (step 1) or names no UUID library at all (step 3), so none was in the step-2 state.

**Two reviewer observations confirmed as non-issues, recorded so they are not re-litigated.** `UniqueImports` returns a fresh slice and `TableContext.Imports` backing arrays are per-table, so `attachUUIDGeneration`'s append has no aliasing hazard. `deduplicateRelationshipOptionsDefs` mutates `tables[i]` in place and returns the same slice, and every later pass takes `&tables[i]`, so nothing can drop `PKAutoGenExpr` — the pass ordering is immaterial. Separately, `attachUUIDGeneration` keys value-vs-string on `PKColumns[0].GoType == "string"`, which mis-handles an app-strategy PK resolved to neither `string` nor `uuid.UUID` (an explicit `primary_key.strategy: app` on an int or composite PK reaches it) — but the removed `funcPKAutoGenType` had the identical shape, so 26.3a neither introduces nor widens it.

---

## 26.4 Flip the default binding

**PRD Reference:** Sections 7.2, 7.3

**Status:** Complete

**Breaking.** Depends on 26.1, 26.2 and 26.3a. Shipped together with 26.5.

### Tasks

- [x] Add a `goUUID` `typeInfo` to `cmd/sqlgen/gotype/gotype.go`: `name: "uuid.UUID"`, `imp: "uuid"`, `zero: "uuid.UUID{}"`, `kind: nullPtrOnly`, `fk: FKStringStringer`
- [x] Change `postgresMappings`: `"uuid": goString` → `"uuid": goUUID`
- [x] Confirm MySQL and SQLite are untouched — neither declares a `uuid` SQL type
- [x] Confirm `uuid[]` resolves to `[]uuid.UUID` through the existing array rule (PRD §7.2 "Array type rule")
- [x] Regenerate all goldens and review every diff

### Acceptance Criteria

- A `uuid` column with no override resolves to `uuid.UUID`, nullable to `*uuid.UUID`
- `uuid[]` resolves to `[]uuid.UUID`; the element type satisfies `comparable`, so `comparator.Slice[T]` still instantiates
- Nullable UUID columns route to `comparator.NullableID`, unchanged from the wrapper form
- The generated GraphQL schema emits `UUID` for nullable UUID fields; no `NullUUID` scalar and no `MarshalNullUUID` / `UnmarshalNullUUID` pair for stdlib-bound packages
- Filter inputs are unchanged (`NullableIDComparator` either way)
- Every example compiles and vets clean under both `output.driver` values
- A consumer can fully opt out by setting `overrides.types.uuid` to a wrapper library

### Tests Required

- [x] Resolver unit tests for the new default at both nullabilities, plus `uuid[]`
- [x] Full golden regeneration across all 13 examples; every diff attributed
- [x] `go build` + `go vet` clean on every example module
- [x] E2E suite green under Docker on all three dialects
- [x] Round-trip integration test on the default path: insert, read, NULL, and `= ANY($1)` over `[]uuid.UUID`
- [x] Assert the emitted `.graphqls` no longer declares `scalar NullUUID` for a stdlib-bound package

### Completion Record

**Completed 2026-09-15**, shipped with 26.5 as the breaking pair.

**The flip itself is four lines.** `cmd/sqlgen/gotype/gotype.go` gains
`goUUID = typeInfo{name: "uuid.UUID", imp: "uuid", zero: "uuid.UUID{}", kind: nullPtrOnly, fk: FKStringStringer}`,
placed between `goDuration` and `goJSON` to match the order of the §7.2 mapping table, and
`postgresMappings`'s `"uuid"` row moves from `goString` to it. MySQL and SQLite were confirmed
untouched by reading their mapping literals rather than by inspection — neither declares a `uuid`
key, so a column spelled `uuid` on those dialects still lands on `builtinResolve`'s unknown-type
string fallback. `uuid[]` needed no work: the array branch resolves its base in the non-null form
through the same chain, so it follows to `[]uuid.UUID` with the import attached.

**Resolver tests.** Four existing cases in `gotype_test.go` encoded the old default and were
retargeted (the chain-precedence "dialect default" subtest, the two §7.2 mapping-table rows, the
`uuid[]` array row, and the `uuid default` FK-conversion row, which also moved from the
"string types need no conversion" group to the `.String()` group). New `TestDefaultUUIDBinding`
covers what none of them could: that the *unconfigured* path resolves to the standard library —
`TestStdlibUUIDIntegration` from 26.1 reaches the same Go type by naming `import: uuid`
explicitly, and would keep passing if the dialect mapping still said `string`. It pins both
nullabilities at both `use_pointers` settings (`uuid.UUID` is `nullPtrOnly`, so the flag must not
move it), `uuid[]` at both, the PostgreSQL-only scope (MySQL/SQLite still resolve `string` with no
import), and the opt-out — a wrapper override still winning at step 4 over the new step-6 default.

**Unit-test churn outside the resolver: 20 test functions across 9 files in `cmd/sqlgen/gen`,
plus one in `cmd/sqlgen/cli`.** Every one was a fixture with a bare `uuid` PK whose expectation
was written when that resolved to Go `string`. Three shapes:

- **GraphQL scalar.** A `uuid` PK now binds the `UUID` scalar rather than the spec `ID` — the
  registry keys on the resolved Go type, and only a PK resolving to Go `string` reaches `ID`.
  `api_schema_test.go` (object field, query arg, mutation arg, both `field_casing` modes),
  `api_composite_pk_input_test.go`, `api_operations_mask_test.go`, `api_per_table_test.go`,
  `api_multischema_test.go`, `api_resolvers_test.go`. `TestGraphQLSchema_PKByGoType`'s
  `uuid_default_string` case was renamed `uuid_default_uuid_scalar` and now expects `UUID` — its
  own doc comment already said "uuid as `UUID!`", so the table row was the stale half; the
  `text_string_pk` case still covers the `Go string → ID!` PRD row.
- **Resolver signatures.** `id string` → `id uuid.UUID` across the seed and Q/M method pins in
  `api_resolvers_test.go`.
- **Tenancy.** A `uuid` tenant column resolves to `uuid.UUID`, so `TenantResolver[string]` →
  `TenantResolver[uuid.UUID]` and the explicit-tenant field `*string` → `*uuid.UUID`
  (`context_tenancy_test.go`, `context_tenancy_view_test.go`, `filter_relationship_test.go`).

Two needed more than a retarget:

- **`TestConsumerScalars_undeclaredTypeAllowedWhenColumnIsHidden`** asserted `UsedScalars` was
  *empty*. Its fixture's `id` PK is `uuid` and stays exposed, so `UUID` is now legitimately in
  use; the blunt emptiness check would have been satisfied by deleting the claim. It asserts the
  exact set `["UUID"]` instead, which still fails if either hidden column registers its scalar.
- **`TestGenerate_TenancyTypeSelectsUUIDIntegration`** (26.3a's regression pin) stopped being a
  valid config. It relied on a bare `uuid` PK resolving to `string` so that `tenancy.type` was
  the package's only UUID claim; post-flip that PK claims the standard library and the package
  holds two libraries, which 26.2 correctly rejects. The fixture now retypes the PK to a Go
  `string` via `column_map`, which is the state PRD §7.4's step-2 selection rule names outright
  and the only way to reach "nothing else claims a library" after the flip. `PKStrategy` keys on
  the SQL type, so the retype leaves the app strategy — and the generating call — in place.

**`TestBuildMergeInput_SignatureParity_RowStructsAndScalars`** (cli) was a fixture gap the flip
exposed rather than a defect: `UUID` is the first *external* scalar that fixture produces, and
external scalars anchor on `ResolverImportPath`, which the orchestrator populates and a direct
`BuildAPIContext` call does not. It now sets the anchor the way six sibling tests in the same file
already do.

**One defect found, in 26.5's gofrs migration — see 26.5's record.** `scalars.go.tmpl` hardcoded
`uuid.Parse`, which gofrs does not export. Fixed per-integration with a PRD amendment; the code
change lands in `gotype/uuidgen.go`, the three integration packages, `context_api.go`,
`orchestrate.go` and the template.

**Files changed (26.4 proper):** `cmd/sqlgen/gotype/gotype.go`, `cmd/sqlgen/gotype/gotype_test.go`,
`cmd/sqlgen/gen/api_schema_test.go`, `api_composite_pk_input_test.go`, `api_operations_mask_test.go`,
`api_per_table_test.go`, `api_multischema_test.go`, `api_resolvers_test.go`,
`api_consumer_scalars_test.go`, `context_tenancy_test.go`, `context_tenancy_view_test.go`,
`filter_relationship_test.go`, `tenant_column_type_test.go`, `cmd/sqlgen/cli/graphql_test.go`.

**Verification.** `make check` clean across all 8 lint modules and 33 test modules under `-race`.
`make vet-examples` clean on all 13 example modules. `make check-examples` clean. Golden
regeneration reviewed diff by diff: the nullable-UUID GraphQL field moves `NullUUID` → `UUID` and
the `scalar NullUUID` declaration plus the `MarshalNullUUID` / `UnmarshalNullUUID` pair disappear
for stdlib-bound packages, while filter inputs stay on `NullableIDComparator` — confirmed by the
absence of any `Comparator` line in the golden diff. `postgres` now carries
`NullableRef *uuid.UUID`; `postgres_stdlib` keeps `NullableRef uuid.NullUUID`, which is the
wrapper half 26.5 moved there deliberately.

**`/verify 26.4` — cleared 2026-09-15.** All seven acceptance criteria PASS, all six Tests Required
present; no FIX entries logged. `make check` clean (33 test packages, `-race`, exit 0) with
`.tool-versions` on-pin (go 1.27.1, golangci-lint 2.13.2, gofumpt 0.12.0). `make check-examples`
clean — **the Docker leg this record deferred, now run**: 12/12 example modules, 0 failures, E2E
green on PostgreSQL, MySQL and SQLite. An independent `sqlgen-reviewer` pass agreed on all nine
PRD requirements it checked and confirmed the ~20 retargeted test expectations are consequences of
the flip rather than weakened assertions — specifically that `TestGenerate_TenancyTypeSelectsUUIDIntegration`'s
`column_map` retype leaves 26.3a's regression pin intact (the PK is still Go `string`, so
`pkValue = uuid.NewString()` and the `assertOnlyUUIDLibrary` google assertion both survive), and
that `TestConsumerScalars_undeclaredTypeAllowedWhenColumnIsHidden`'s move from "`UsedScalars` is
empty" to "`UsedScalars` == `[UUID]`" is strictly stronger.

**One coverage hole, measured rather than assumed — the claim holds.** No example covers
PostgreSQL + `output.driver: stdlib` + the new stdlib `uuid.UUID` default: the tree's only
PostgreSQL stdlib-driver module is `postgres_stdlib`, which 26.5 holds on google as the wrapper
opt-out guard, so every stdlib-uuid-bound PostgreSQL example runs pgx. That combination is exactly
what an unconfigured consumer now gets, and PRD §7.4 is normative about it (no `sql.Scanner` /
`driver.Valuer` needed, because `database/sql` special-cases the type on write in `driver/types.go`
and on read in `convert.go`). A throwaway probe in `postgres_stdlib/tests` against a real container
round-tripped write, read, NULL and an IN-list on lib/pq — all pass, probe removed. The
`database/sql` half is also covered end to end by the `tenancy` example, which is SQLite +
`driver: stdlib` with stdlib `uuid.UUID` tenant columns. Recorded as a coverage gap, not a defect.

**Three fixes applied inline during verification** (no FIX entries — per the standing preference
for in-place doc fixes over per-finding waterfall):

- `cmd/sqlgen/testdata/examples/graphql/schema.sql` — two comments still claimed `events.user_id`
  and `workspace_notes.parent_id` "exercise the NullUUID scalar path". That example is stdlib-bound
  as of 26.5 and emits a nullable `UUID`; the comments now say so and point at
  `graphql_null_wrappers` as the surviving `NullUUID` site. `TestE2EGoldenFiles` re-run after the
  edit: no golden drift (SQL comments are not captured into generated output).
- `docs/PRD.md` §7.4 "Generating UUID values" — "only google exports `New()`" is false and is
  contradicted three sentences later by the paragraph explaining why the generator emits `NewV4()`
  over `New()`. `go doc uuid` shows the standard library exports `New`, `NewV4`, `NewV7`, `Nil`,
  `Parse` and `MustParse`. Corrected to "the standard library and google both export `New()` while
  gofrs does not". Nothing normative follows from the clause — the generating table and
  `uuidstd.V4Expr` are unchanged.
- `cmd/sqlgen/gen/uuid_library_test.go` — added the one-library-per-package case the flip made
  newly reachable: a bare `uuid` column plus a **table-scoped** wrapper override, with no global
  `overrides.types.uuid` at all. Pre-26.4 the bare column resolved to Go `string`, carried no
  import and therefore made no claim, so that config validated; post-flip it is a genuine
  two-library package and 26.2 rejects it. The existing headline case spells the standard library
  globally and explicitly, so nothing pinned the implicit form. Passes under `-race`; `gen` lints
  clean.

**Two observations carried forward, neither logged.** (1) `use_pointers: false` × the default
stdlib binding is pinned only by `TestDefaultUUIDBinding` — `graphql_null_wrappers` is the tree's
only `use_pointers: false` example and 26.5 holds it on gofrs, so no golden asserts the
`*uuid.UUID`-at-`false` shape PRD §7.3's new paragraph is normative about. (2) `scalarModelPath`
(`cli/graphql.go:542-544`) drops an external scalar's `models:` entry silently when
`ResolverImportPath` is unresolvable, where the sibling `numericWidthModels` warns for the same
condition; the asymmetry is deliberate and documented in that function's own comment, but the flip
moves the silent path from opted-in UUID/decimal projects onto every PostgreSQL project with a
`uuid` column.

---

## 26.5 Migrate examples — five to stdlib, two held as override guards

**PRD Reference:** Section 7.4

**Status:** Complete

Depends on 26.4 (and, through it, on 26.3a — three of the five migrating examples have events enabled); shipped with 26.4. Seven examples bound
`github.com/google/uuid` and **none exercised `github.com/gofrs/uuid/v5`**
despite `cmd/sqlgen/gotype/uuidgofrs/` existing.

### Tasks

- [x] `postgres` → stdlib (keeps the nullable self-referential FK `warehouses.nullable_ref` on the `*uuid.UUID` FK-extraction branch)
- [x] `graphql` → stdlib (proves the `UUID`-scalar schema shape and the absent `NullUUID`)
- [x] `graphql_top_level` → stdlib
- [x] `tenancy` → stdlib
- [x] `tenancy_postgres` → stdlib
- [x] `postgres_stdlib` — **hold on `github.com/google/uuid`** as the wrapper-path regression guard; its `output.driver: stdlib` keeps `database/sql` covered with a wrapper type
- [x] `graphql_null_wrappers` — **move to `github.com/gofrs/uuid/v5`**, closing the `uuidgofrs` coverage gap
- [x] For every migrating example: edit `sqlgen.yml` (`overrides.types.uuid.import`, drop any `nullable:` for stdlib) **and** `go.mod` (add/drop the library)
- [x] Move any table-scoped `overrides` or `column_map.<col>.import` naming a UUID library together with the global one, or the package trips 26.2
- [x] Move `tenancy.type` together with the global override too — `graphql` and `tenancy` both declare a google one, and since FIX-201 the tenant column carries that import as a real claim, so leaving it behind trips 26.2
- [x] Regenerate every `expected/` golden tree

### Acceptance Criteria

- Five examples bind the standard library and require no UUID module in `go.mod`
- `postgres_stdlib` still exercises `uuid.NullUUID`, the `NullUUID` GraphQL scalar, and `enrichOverride`'s integration back-fill
- `graphql_null_wrappers` exercises `uuidgofrs`, including its `uuid.Nil` **variable** zero value — the spelling no other integration uses
- No example trips the 26.2 mixed-library validation
- Both `output.driver` values remain covered on both the stdlib and wrapper shapes
- A build of the example tree still pulls **both** wrapper libraries

### Tests Required

- [x] `TestE2EGoldenFiles` byte-clean across all 13 examples
- [x] `make check-examples` clean
- [x] `make test-integration` clean under Docker — deferred to `/verify` at implementation time (that pass ran `check-examples` only); `/verify 26.4` ran the Docker leg green on all three dialects, and `/close-phase 26` re-ran the full `make test-integration` sweep clean (2026-09-15)
- [x] Dependency assertion: the example tree's resolved module graph contains both `github.com/google/uuid` and `github.com/gofrs/uuid/v5`, proving neither integration went dark
- [x] `graphql_null_wrappers` golden still declares `scalar NullUUID`; `graphql` golden no longer does

### Completion Record

**Completed 2026-09-15**, shipped with 26.4 as the breaking pair.

**Config moves.** Five examples to the standard library (`import: uuid`, `nullable:` dropped —
the package ships no wrapper for it to name): `postgres`, `graphql`, `graphql_top_level`,
`tenancy`, `tenancy_postgres`. `graphql` needed all three of its UUID sites moved together —
global `overrides.types.uuid`, `tenancy.type`, and `column_map.order_items.external_ref.import` —
since every one is a real claim 26.2 counts. `tenancy` (SQLite) moved both its `tenancy.type` and
its `overrides.types.blob` entry. `postgres` **dropped** its `warehouses` table-scoped `uuid`
override outright rather than restating the default; the block keeps its `numeric` / `text` /
`region_name` entries, so table-scoped integration detection stays covered through decimal.

`postgres_stdlib` holds `github.com/google/uuid`, but the override had to be **promoted from
table-scoped to global**. Post-flip there is no such thing as "this `uuid` column resolves to
`string`", so a table-scoped google override beside stdlib-resolved columns elsewhere is exactly
the two-library state §4.13 rejects — holding google means holding it for the whole package. The
`warehouses` entry stays as well, which keeps table-scoped enrichment covered on a wrapper.

`graphql_null_wrappers` moved to `github.com/gofrs/uuid/v5` (v5.5.1), closing a gap where no
example exercised `uuidgofrs` at all — and once the standard library became the default, nothing
would ever have pulled gofrs into a build again.

**A live defect surfaced the moment gofrs was actually exercised.**
`cmd/sqlgen/gen/templates/api/scalars.go.tmpl` hardcoded `uuid.Parse` at two sites, and gofrs
exports no `Parse` — its string constructor is `FromString`. Measured, not recalled: the standard
library and google both export `Parse(s) (UUID, error)`; gofrs exports `FromString` /
`FromStringOrNil` / `FromBytes` and no `Parse`. This is the same family as B-1 / B-2 from 26.0's
decision record — the generator assuming one library's spelling — and it was invisible because no
example bound gofrs. Blast radius was confirmed to be exactly those two sites by hand-patching and
building the generated package against gofrs.

**Fixed inline, PRD first (user decision, 2026-09-15).** §26.4.1's registry row named `uuid.Parse`
outright while also claiming registration "under every UUID integration" — the two could not both
hold. §7.4 gained **"Parsing a UUID from a string"**, a per-integration table beside the existing
"Generating UUID values" one, recording that all three return `(uuid.UUID, error)` so only the
name varies, and that gofrs's `OrNil` variants are deliberately unused (an unparseable scalar
input is a client error the unmarshaler reports, not one it collapses to the zero UUID). The
§26.4.1 row now points at it. Code follows 26.3a's seam exactly: a `ParseFunc` constant on each of
`uuidstd` / `uuidgoogle` / `uuidgofrs`, a `ParseFunc` field on `gotype.UUIDIntegration` (fed by
the same registry, so an unknown import path takes the standard library's spelling alongside its
generating calls), a `UUIDParseFunc` field on `APIContext` defaulted to the stdlib spelling and
refined by the orchestrator from the package's selected integration, and `{{ $.UUIDParseFunc }}`
in the template. The three `uuidgen_compile_*_test.go` pins were extended to compile the parse
call against the real library and assert the registry value matches, which is what makes the table
checkable rather than asserted.

**A third defect, found while answering the reviewer's own question.** Several tests render
`api/scalars` from a **hand-built** `APIContext` rather than through `BuildAPIContext`, so the
default the field carried never applied — and the template interpolated an empty string, emitting
`return (s)`. That is not Go, and nothing caught it: `scalars_gen.go` is emitted rather than
compiled inside the `gen` module, and the nearest existing test
(`TestScalarBinding_everyMarshalerBodyHasItsImports`) only asserts the body is non-empty and that
its package qualifiers resolve, both of which a nameless call satisfies. Verified by rendering it.
Closed with `APIContext.ParseUUIDFunc()`, which falls back to the gotype registry's
no-integration-selected answer so the default has one definition rather than two; the template
calls the method, never the field. `TestScalarsTemplate_UUIDParseFuncAlwaysNamed` pins all four
cases — unset, stdlib, google, gofrs — and asserts no nameless call is rendered.

**Handwritten example tests: 33 files migrated mechanically, 2 reworked.** The import moves to the
standard library's `"uuid"` (stdlib import group), `uuid.Must(uuid.NewRandom())` collapses to
`uuid.New()`, and `uuid.Nil` becomes `uuid.Nil()` — the standard library's `Nil` is a **function**,
which is the §7.4 "three spellings of the zero UUID" hazard showing up in test code.

`postgres` and `postgres_stdlib` carried a second, larger churn that the other examples did not:
their non-`warehouses` `uuid` columns were previously Go `string`, so their integration suites had
`[]string` ID slices, `map[string]` ID keys, `x.ID == ""` zero checks and `string`-returning seed
helpers throughout — 211 and 78 type errors respectively. All retyped to `uuid.UUID`. Two points
of real judgment there: `comparator.ID` stays **string**-keyed whatever the column's Go type
(PRD §7.4 — ID and FK values are converted to strings for comparison), so slice operands go
through a new `idStrings` helper in each package's `main_test.go` and single operands take
`.String()` inline; and `uuid.UUID` is an array type, so it is comparable but **not** ordered —
`lock_mode_test.go`'s duplicate scan moved from `sort.Strings` to
`slices.SortFunc` + `bytes.Compare`.

`postgres/tests/type_overrides_test.go` was reworked rather than retyped: its two `uuid.NullUUID`
round-trips are now `*uuid.UUID`, with `!= nil` / `*v` replacing `.Valid` / `.UUID` and
`omittable.Set[*uuid.UUID](nil)` replacing `omittable.Set(uuid.NullUUID{})`.
`TestNullableUUIDOverride_NullThenValueRoundTrip` became
`TestNullableUUIDPointer_NullThenValueRoundTrip`. The wrapper coverage it gave up moved to
`postgres_stdlib`, the designated wrapper guard — but **not entirely, at first**: that example
already covered create-with-value, get and create-null on the same `warehouses.nullable_ref`
column, and the auto-review caught that neither UPDATE transition was there, so the assertion that
`omittable.Set(uuid.NullUUID{})` writes SQL NULL — which rides the wrapper's `driver.Valuer`
returning nil for `!Valid` — briefly existed nowhere. `postgres_stdlib` gained
`TestNullableUUIDWrapper_NullThenValueRoundTrip`, which carries the full
INSERT-NULL → UPDATE-to-value → UPDATE-back-to-NULL sequence with a Get readback after each
transition.

`TestNonOverriddenTablesUnchanged` (both examples) was **re-pointed, not just retyped.** It proved
that the `warehouses` table-scoped override does not leak, and it made that point on `uuid` — a
contrast the flip erases, since `uuid` now resolves the same way package-wide. The scripted pass
retyped its expression and left the claim, so the section header, the comment and the failure
message all still said "string IDs" over an assertion about `uuid.UUID`. It now pins the same
isolation property on the `text` → `ksuid.KSUID` entry, which is still scoped to `warehouses`:
`var name string = cat.Name` on the non-overridden `categories` table does not compile if the
scope leaks.

**go.mod.** `go mod tidy` on all seven touched modules. All five migrating examples
(`postgres`, `graphql`, `graphql_top_level`, `tenancy`, `tenancy_postgres`) dropped
`github.com/google/uuid` from a direct requirement to `// indirect` — it survives only as a
transitive dependency of the testcontainers / gqlgen graphs, which is the expected end state:
their generated code and their tests now name no UUID library at all, and the standard library
adds no module entry. `postgres_stdlib` keeps `github.com/google/uuid` **direct**, and
`graphql_null_wrappers` carries `github.com/gofrs/uuid/v5 v5.5.1` direct. Those two direct
requirements are the dependency assertion below: both wrapper libraries stay in the example
tree's build.

**A second live defect, found by `make check-examples` rather than by generation.** The `graphql`
example's `order_items.external_ref` is a **TEXT** column retyped to `uuid.UUID` through
`column_map` — the FIX-156 fixture. Under google that scanned; under the standard library it fails
at runtime with `cannot scan text (OID 25) in text format into **uuid.UUID`. Measured: google's
`uuid.UUID` carries `Scan(any) error` and `Value() (driver.Value, error)`, gofrs's carries both,
and the standard library's carries **neither** — only `MarshalText` / `UnmarshalText` / `AppendText`
— while pgx registers `UUIDCodec` against the `uuid` OID, so there is nothing for a TEXT column to
dispatch to. PRD §7.4's "No `driver.Valuer` / `sql.Scanner`, and none needed" was true for a `uuid`
column and overclaimed for a retyped one.

**Resolved by scoping the PRD claim and moving the fixture (user decision, 2026-09-15).** §7.4's
bullet is now titled "…— for a `uuid` column" and is followed by a new bullet stating that a
non-`uuid` column cannot be retyped to the standard library's `uuid.UUID` under pgx, why (the
codec keys on the OID), that both wrapper-backed integrations do support it, that
`output.driver: stdlib` is unaffected (`database/sql`'s special case keys on the destination type,
not the column), and what a project storing UUIDs in `text` should select instead.

The fixture moved to **`graphql_null_wrappers`**, not to `postgres_stdlib` as first proposed:
`postgres_stdlib` has no `api:` block at all, so it cannot carry the gqlgen-handoff half of
FIX-156, whereas `graphql_null_wrappers` is GraphQL-enabled and gofrs-bound. It gained
`entries.upstream_ref TEXT`, the matching `column_map` retype, and
`tests/column_map_scalar_test.go`, which round-trips the value through create, through a separate
read (so the column-scan path is covered as well as the `RETURNING` projection), and through an
unset case. `graphql` keeps `external_ref` as a `description:`-only `column_map` entry — the
narrowest form of that block, which now pins that a `column_map` entry needs no `type:` — and its
sibling FIX-159 fixture (`source_ip` → `netip.Addr`) still covers the consumer-scalar half of the
same handoff in that example.

**Verification.** `TestE2EGoldenFiles` passes across all 13 examples. `make vet-examples` and
`make check-examples` clean. `make check` clean. Both wrapper libraries remain in the example
tree's resolved module graph — gofrs through `graphql_null_wrappers`, google through
`postgres_stdlib` — so neither integration went dark. `graphql_null_wrappers`'s golden still
declares `scalar NullUUID` and now emits `uuid.FromString`; `graphql` and `graphql_top_level`
declare no `NullUUID` and emit no `MarshalNullUUID` / `UnmarshalNullUUID`. Docker integration
suites deferred to `/verify` per user direction.

---

## Decision Record — the two issues 26.0 raised, and how they were resolved

Both were found while writing §7.4's normative text and verifying it against the
code; neither was covered by `docs/design/archive/UUID_MIGRATION.md` or by any sub-item as
originally broken down. Both were measured, not inferred — `go doc` against the
real `go1.27.1` toolchain and against `github.com/gofrs/uuid/v5 v5.5.1` /
`github.com/google/uuid` in the module cache.

**User decision (2026-09-11): fix both as part of this migration.** B-1 —
the hardcoded event-ID library becomes the package's *selected* UUID integration.
B-2 — the `uuid.Must(...)` spellings are fixed rather than worked around. The two
share one seam (resolve the selected integration, derive its generation
expressions), so they are implemented together as **26.3a**.

### B-1 — The event-hooks file hardcoded `github.com/google/uuid` — RESOLVED; **landed in 26.3a (2026-09-11)**

`cmd/sqlgen/gen/context_event.go` appended `"github.com/google/uuid"` to the
event-hooks import block unconditionally and emitted `uuid.New().String()` (or
`uuid.Must(uuid.NewV7()).String()` under `uuid_version: v7`), consulting neither
`overrides.types.uuid` nor the resolved column type. The `cache` example proves it
was unconditional: no `uuid` override anywhere in its `sqlgen.yml`, yet
`cache/expected/event_hooks_gen.go` imports `github.com/google/uuid`.

Three consequences, all now addressed by 26.3a:

1. **It would have broken three of 26.5's own five migrations.** `tenancy`,
   `graphql` and `tenancy_postgres` each have `events.enabled: true`. Migrating
   them to the stdlib would give the package `import "uuid"` from the columns and
   `import "github.com/google/uuid"` from the event hooks — the
   `uuid redeclared in this block` failure §7.4 and 26.2 exist to prevent.
2. **26.2 would not have caught it.** 26.2 collects import paths from
   `overrides.types` and `column_map.<col>.import`; the event-hooks import came
   from neither, so the config would validate and the package would then fail to
   compile.
3. **It is a live pre-existing defect.** Every consumer with events enabled was
   forced to carry `github.com/google/uuid` in `go.mod` regardless of UUID use,
   and a gofrs consumer with events could not compile at all (`uuid.New()` does
   not exist in gofrs) — unnoticed only because no example exercises `uuidgofrs`.

**PRD side, written in 26.0:** §28.8 previously mandated the hardcoding outright
("Event IDs use `google/uuid` … `uuid.Must(uuid.NewV7()).String()`"), so it
contradicted §7.4 and named a `uuid.Must` the standard library does not have. It
now specifies the selected integration's string-form call, records that the
event-hooks file imports that one library and no other (keeping the package inside
the one-library rule), and states that a package with no `uuid` column falls back
to the standard library so events add no module dependency.

### B-2 — `funcPKAutoGenType` emitted spellings that do not compile — RESOLVED; **landed in 26.3a (2026-09-11)**

`cmd/sqlgen/gen/funcmap.go::funcPKAutoGenType` emitted google-shaped calls for
every integration. Measured surfaces:

| Package | v4 | v7 | `Must` | string helper |
|---------|----|----|--------|---------------|
| stdlib `uuid` (go1.27.1) | `NewV4() UUID` | `NewV7() UUID` | **absent** | **absent** |
| `github.com/google/uuid` | `New() UUID` | `NewV7() (UUID, error)` | present | `NewString() string` |
| `github.com/gofrs/uuid/v5` | `NewV4() (UUID, error)` | `NewV7() (UUID, error)` | present | **absent** |

So `uuid.Must(uuid.NewV7())` does not compile against the stdlib (no `Must`, and
`NewV7()` returns a bare `UUID`), `uuid.NewString()` does not compile against the
stdlib or gofrs, and `uuid.New()` does not compile against gofrs. No example uses
an app-strategy UUID PK, so the golden suite would not have caught any of it.

**PRD side, written in 26.0:** §7.4 gained the "Generating UUID values" table —
value and string forms, per integration, for v4 and v7 — and §8.6 / §9.8.4 / §28.8
now cite it instead of spelling calls of their own. The stdlib v4 cell is
`uuid.NewV4()` rather than `uuid.New()` deliberately: `New` is documented as "an
algorithm suitable for most purposes" and is not contractually v4, so an explicit
`uuid_version: v4` is honored with the call that names the version.

---

## Phase Exit

- [x] All seven sub-items Complete (26.0–26.5 plus 26.3a)
- [x] Consumer-visible changes documented for release notes (**partially** — see the note under this list): the `uuid` `string` → `uuid.UUID` default retype, the GraphQL `NullUUID` scalar removal, the `.Valid` → `!= nil` change on nullable UUID columns, and — from 26.3a — the removal of the forced `github.com/google/uuid` dependency for events-enabled packages (event IDs now follow the selected integration; a stdlib-bound or UUID-free package needs no UUID module at all) plus the **Go 1.27 module floor**: generated code that binds the standard library `uuid` package needs `go 1.27.0` in the consumer's `go.mod`, because `go vet` rejects it below that even though `go build` accepts it
- [x] `docs/design/archive/UUID_MIGRATION.md` marked executed — Status header flipped to `SYNCED (2026-09-15)` per the project convention, citing the PRD sections that supersede it and recording the three claims execution narrowed
- [x] `/close-phase 26` — run 2026-09-15

**Release-note coverage, as of the 2026-09-15 close.** The `BREAKING CHANGE:` footer on
`eb58ae2` (`feat(gen)!: bind PostgreSQL uuid columns to the standard library by default`) is the
surface Release Please renders, and it carries three of the five items above: the
`string` → `uuid.UUID` retype (naming struct fields, method signatures, manifest `go_type` values
and the `ID` → `UUID` primary-key scalar), the two opt-outs (`overrides.types.uuid`, or a
`column_map` retype to `string`), and the Go 1.27 floor. **Two are recorded only here and in
`STATUS.md`:** the `NullUUID` GraphQL scalar removal with its `MarshalNullUUID` /
`UnmarshalNullUUID` pair — and the `.Valid` / `.UUID` → `!= nil` / `*v` change on nullable UUID
columns that follows from it — and 26.3a's removal of the forced `github.com/google/uuid`
dependency for events-enabled packages. The repository has **no configured git remote**, so no
Release Please run has consumed these footers yet and the gap is still closable by amending
`eb58ae2` before the first release. Flagged rather than fixed at closure time: amending is a
history rewrite, which is the user's call.
