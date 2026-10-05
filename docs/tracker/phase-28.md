# Phase 28: Transaction Connection Lock

Status: Complete (closed 2026-09-22)
PRD Sections: 9.4a, 13.2, 18.2, 18.5, 18.6, 20.2, 20.5, 29.4.3, 30.4.1

> **Design:** `docs/design/archive/TX_CONNECTION_LOCK.md` (DRAFT, revised 2026-09-19). Section numbers below
> (§2.5, §3.2, …) refer to that document. Every step here maps to one of its §5.2 sequencing steps.
>
> **What this phase does, in one line:** move the obligation *"one statement at a time per
> transaction"* from the caller to the `Tx` itself, with a capacity-1 reservation held from `Query`
> until the result set is **drained or closed, whichever comes first** (§2.5). Outside a transaction
> there is no `Tx` and therefore no lock, so the relationship fan-out keeps running fully parallel
> over separate pooled connections (§3.5).
>
> **Why it is cheap:** the lock is scoped to the driver's real busy window, not to `Rows.Close()`.
> Measured both ways on the same tree (§2.6): drain-scope passes eleven suites and three dialects
> with **zero template edits**; close-scope needs 47 call-site conversions and hangs without them.
>
> **Prerequisite, already landed.** FIX-221 bounded the relationship loader's `errgroup` to one
> worker inside a transaction. 28.4 removes that bound — it is redundant once the reservation
> exists, and removing it is what makes the generated tree exercise the reservation on every
> multi-edge read inside a transaction (§6.5).

---

## Verification notes (checked against HEAD, 2026-09-21)

Everything the design asserts about this repo was re-checked before this breakdown. Confirmed:

- `Tx` has one `sync.Mutex` guarding `closed / conn / depth / callbacks / savepointNames`
  (`database/transaction.go:90-100`); `Exec`, `Query`, `QueryRow` snapshot `tx.conn` and release it
  **before** the driver call (`:153-193`), which is why a method-scoped lock protects nothing.
- `Begin` holds `tx.mu` across its **whole body** including the `SAVEPOINT` exec
  (`:200-201`, `defer tx.mu.Unlock()`); `Commit` and `Rollback` are package-level functions that
  hold `tx.mu` across `conn.Exec` / `conn.Commit` / `conn.Rollback` (`:313-360`, `:364-395`).
- `database/stdlib`'s `rows.Next()` is a bare delegate and does **not** close on exhaustion
  (`stdlib.go:158-161`) — §2.11's defect is live.
- Both adapters implement exactly the five `database.Rows` methods and nothing else
  (`querier.go:17-23`, `pgx.go:187-227`, `stdlib.go:153-199`), so a `txRows` wrapper erases nothing.
- `QueryRowFunc` is two lines and releases nothing (`transaction.go:449-452`); `QueryFunc` is
  already leak-proof via `defer rows.Close()` (`:431-447`). Neither is called by anything.
- `g.SetLimit(1)` exists at exactly one template site (`table/get.go.tmpl:401`).
- `Raw` returns `database.Rows` via a `result.(database.Rows)` assertion
  (`client.go.tmpl:301-313`) — the only assertion on that interface anywhere.
- No file in any example tree calls both `Stream` and `WithTx`/`WithTransaction`.

**Four corrections to the design doc's cost estimates**, all in the direction of *more* work:

1. **Twelve example trees, not eleven**, and each carries **two** generated copies (`expected/` and
   `models/`). So 28.5 moves **24** `client_gen.go` files, not eleven.
2. **`CallOptions.AllowInTransaction` (28.3) has a much wider blast radius than §6.2 states.** The
   struct is built unconditionally in `gen/context_shared.go:148-180`, so a new field moves all 24
   `shared_types_gen.go` files plus `gen/testdata/golden/shared_types_gen.go` and
   `gen/shared_types_test.go:168`. It also moves the **manifest**: `manifest/builder.go:334` carries
   a hard-coded field list, which feeds `manifest_gen.json` + `manifest/_conventions.md` in five
   example trees × two copies = 20 more goldens. **28.0 must decide** whether the field is
   unconditional (like `LockMode`) or gated on the package emitting a `Stream` — the gate keeps
   non-`Stream` packages byte-identical, at the cost of a conditional `CallOptions` shape.
3. **`Raw`'s signature is printed four times in the PRD, not three** — `docs/PRD.md:9563` (§18.6
   "Raw Queries and Hooks"), `:9889` (§20.2 method listing), `:10003` (§20.2 worked example), and
   `:14420` (§29.4.3, prose form `Raw(ctx, sql, args...)`).
4. **`gen/tenancy_template_test.go` is *not* in `Raw`'s blast radius.** It asserts only the
   `// sqlgen: raw query bypasses tenancy` comment (`:189-234`), which the signature change leaves
   alone. `gen/unified_client_test.go:184` is the one test that pins the signature.

**The design ambiguity 28.0 was asked to settle — resolved 2026-09-21, recorded in §18.5.** §3.4
says the savepoint statements in `Begin`, `Commit` **and `Rollback`** all use `acquireForTeardown` —
the *non-blocking* acquire. For a **root** `Commit`/`Rollback` the stated rationale holds (`fn` has
returned, so anything still holding the connection is a leak, and blocking would strand a pooled
connection permanently). **For `Begin`, and for a savepoint-depth `Commit`/`Rollback`, it does
not**: those run while the enclosing `fn` is still live, so a try-acquire leaves a nested `WithTx`
failing with `conn busy` against a concurrent read — and since every `…WithRelated` mutation opens a
savepoint (`nested.go.tmpl:268`, `:333`, `:396`), that is a first-class generated path, not an edge.

**Decision: the acquire mode is set by depth, not by verb.** Savepoint statements block; only the
root teardown proceeds without the reservation. This strictly dominates §3.4's blanket rule — it
keeps the property §3.4 exists for exactly where the rationale is true, and closes the hole where it
is not.

**A second finding, from working the decision through — recorded in §18.5 as a carve-out.** The
reservation serializes **statements**, not savepoint **scopes**. `Tx` tracks savepoint names as a
stack (`depth`, `savepointNames`), so two goroutines that call `WithTx` — or any `…WithRelated`
mutation — on the same `txCtx` *at the same time* interleave their pushes, and the release one
goroutine issues can name a savepoint the other opened. No statement-level lock fixes that, because
each individual statement is correctly serialized; it is the scopes that overlap. §18.5 now states
it as the one shape concurrent `txCtx` use does **not** make safe. Sequential nesting is unaffected.
The precise per-dialect `RELEASE SAVEPOINT` cascade semantics were **not** probed and are not needed
for the carve-out — the bookkeeping mismatch is established from this repo's own code.

---

## 28.0 PRD sync — write the normative contract

**PRD Reference:** §9.4a, §18.2, §18.5, §18.6, §20.2, §20.5, §29.4.3

**Status:** **Complete** (landed 2026-09-21). `docs/PRD.md` only, +157/−33. **All later sub-items are
unblocked.**

Per CLAUDE.md rule 1, no code lands before the spec says what the code will do. This sub-item is
documentation only and **blocks every other sub-item**.

### Tasks

- [x] **§18.5** — rewrite. The caller-serializes rule narrows to one sentence: *drain or close a
      result set before issuing the next statement on that `txCtx`*. Concurrent use of one `txCtx`
      becomes **safe and serialized** on every dialect. State the three residual doors
      (`Querier(ctx)`, `database.FromContext`, writing inside a `Stream` loop) and the changed
      failure mode (§4.2): a misuse that pgx reports as `conn busy` today becomes a wait.
- [x] **§18.5** — state the **`Begin` decision** from the ambiguity note above: whether a nested
      `Begin` blocks on the reservation or proceeds without it, and what a caller sees either way.
- [x] **§18.5 / §18.2** — state plainly that the wait is bounded only by `ctx` or
      `TxOptions.Timeout`, and that **both default to none** (§18.2 already makes zero-means-no-
      timeout an explicit design choice), so the unbounded case is the default case (§4.2, §6.1).
- [x] **§20.5** — restore the `errgroup` fan-out example that FIX-221's sync withdrew. It is correct
      under the reservation. Keep the "parallel reads belong outside the transaction" guidance as a
      *performance* note, not a correctness one (§3.6: the lock buys the contract, not speed).
- [x] **§20.2** — `Raw`'s new signature in both the method listing (`:9889`) and the worked example
      (`:10003`); for `Querier(ctx)`, restate the drain-or-close rule and the changed symptom (§6.3).
- [x] **§18.6** (`:9563`) and **§29.4.3** (`:14420`) — the same signature change in their listings.
- [x] **§9.4a** — `Stream`'s in-transaction guard, the `CallOptions.AllowInTransaction` opt-in and
      the obligation it carries ("issue nothing else on this `txCtx`"); add the decision-table row
      *"Reading inside a transaction → `Connection` in a loop"*; state that the opt-in does **not**
      propagate through `nestedChildOptions`, like `LockMode` and `FieldOptions` (§6.2).
- [x] **§9.4a** — **decide and record** whether `AllowInTransaction` is emitted unconditionally or
      gated on the package emitting a `Stream` (see correction 2 above), and whether
      `manifest/builder.go`'s `call_options.fields` list carries it.
- [x] **§13.2** — revert step 4's FIX-221 caveat: the loader's fan-out is unbounded again inside a
      transaction, and the reservation is what serializes it.
- [x] **§18.5 / §20.2** — document `database.QueryFunc` / `QueryRowFunc` as the safe spelling for
      `Querier(ctx)` consumers (§3.8), and state that **no `ExecFunc` exists** — `Tx.Exec`'s busy
      window is entirely inside the call, so a wrapper would imply `Exec` is unsafe without it.

### Acceptance Criteria

- Every PRD claim the design contradicts is corrected, not merely supplemented — §18.5's *"the
  caller is what enforces that"* and §20.5's *"a `txCtx` used from two goroutines at once is
  undefined behavior"* are both false once 28.2 lands.
- The drain-or-close rule is stated identically in §18.5, §20.2 and §9.4a. One rule, one wording.
- The `Begin` decision and the `AllowInTransaction` emission decision are both recorded with the
  reason, not left to the implementing sub-item.
- No code changes in this sub-item. Zero golden movement.

### Tests Required

- [x] None — documentation only. Gate: every §-number cited by 28.1–28.7 resolves, and no intra-
      document anchor dangles. **Verified**: 642 intra-document refs checked against 497 headings;
      the 10 dangling anchors are byte-identical before and after this sub-item (all pre-existing,
      all from `&`/em-dash headings), and all nine anchors 28.0 introduced resolve.

### Completion Record

**Landed 2026-09-21.** Documentation only — no code, no golden movement, `make check` exit 0
(lint + vet + unit across all eight modules), and `cmd/sqlgen/config/schema_test.go:284`, the one
test that actually parses `docs/PRD.md`, passes. Tool preflight matched `.tool-versions` exactly
(go 1.27.1, golangci-lint 2.13.2, gofumpt 0.12.0), so no 16.8g-style drift.

**Files changed:**

| File | Δ | What |
|---|---|---|
| `docs/PRD.md` | +157/−33 | The sub-item proper — the eight sections below, plus the eleven review-round fixes |
| `docs/tracker/IMPLEMENTATION_ORDER.md` | +1/−1 | Phase 6's client-surface listing carried the last copy of `Raw`'s old signature (`:1043`); found by the auto-review |
| `docs/tracker/phase-28.md` | new | This file (created by `/phase 28`) |
| `docs/tracker/STATUS.md` | +16 | Phase 28 row → In Progress 1/8, Current Focus |

`docs/design/archive/TX_CONNECTION_LOCK.md` shows as modified in the working tree but is **not** part of this
sub-item — it was already dirty when the phase was broken down. 28.6 marks it SYNCED.

**Sections rewritten or amended:**

| Section | Change |
|---|---|
| **§18.5** | Full rewrite — the normative home. A per-method reservation table (`Exec` / `Query` / `QueryRow` and where each releases), release-on-drain, "concurrent use of one `txCtx` is safe on every dialect", the one caller rule, the three residual doors, the safe-spellings table, the changed-failure-mode table, the no-default-deadline fact, teardown, savepoints, why the two locks cannot be one, and the closing "it buys the contract, not speed". |
| **§18.2** | New paragraph — `Timeout` is also what bounds a wait on the reservation, and since it defaults to zero the self-hold misuse is unbounded by default. Deliberately does **not** change the zero-default recommendation. |
| **§20.5** | The withdrawn `errgroup` fan-out example is restored and relabelled **Safe**. Its "parallel reads belong outside the transaction" guidance is reframed as performance, not correctness, with the two carve-outs restated. |
| **§20.2** | `Raw`'s new signature in the method listing **and** the worked example (rewritten to a real callback body). New prose on the scan-callback shape and its three consequences. `Querier(ctx)` gains the drain-or-close rule, the changed symptom, and `QueryFunc` as the leak-proof spelling. |
| **§18.6** | `Raw`'s signature in the "Raw Queries and Hooks" listing, plus one line on why it takes a callback. |
| **§29.4.3** | `Raw(ctx, sql, args, fn)` in the tenancy-bypass listing. |
| **§9.4a** | New **Stream inside a transaction** subsection — the incompatibility argument, the guard, the `AllowInTransaction` opt-in with its three limits, the emission decision, and `Connection`-in-a-loop as the alternative. Constraints list gains a row; decision table gains **Reading inside a transaction → `Connection` in a loop**. |
| **§13.2** | Step 4's FIX-221 caveat reverted — the loader's `errgroup` is unbounded again in and out of a transaction, and the reservation is what serializes it. |

**Two decisions recorded, with reasons** (both were 28.0's job):

1. **Savepoint statements block; only root teardown proceeds without the reservation.** Depth, not
   verb, sets the acquire mode. See the resolved-ambiguity note at the top of this file.
2. **`AllowInTransaction` is emitted unconditionally**, like `LockMode` and `SkipEvents`, and the
   manifest's `conventions.call_options.fields` carries it. Gating it on the package emitting a
   `Stream` would make `CallOptions`'s shape depend on the operations mask — a consumer who turns
   `operations.stream` off would find unrelated code stop compiling. The tenancy fields are gated
   because tenancy is a project-wide mode; an operations toggle is not. Golden churn is not a
   counter-argument in an unreleased project.

**One finding the design doc did not carry**, now normative in §18.5: concurrently opening two
**savepoint scopes** on one `txCtx` is not made safe by the reservation, which serializes statements
rather than scopes. Recorded as a carve-out rather than as something to detect.

**The auto-review found eleven sites the first pass missed; all eleven were fixed inside the
sub-item.** The headline miss is the one the acceptance criteria named: **§9.4a never actually
stated the drain-or-close rule** — it stated the opt-in's own obligation ("issues nothing else on
this `txCtx`") and left the reader to infer that it *is* that rule applied to a loop. It says so
now. Beside it, the "every stale claim is caught" assertion was falsifiable and was falsified:

| Site | What it still said | Why it mattered |
|---|---|---|
| §13.2 intro | "in parallel (**serially inside a transaction** — see step 4 below)" | Contradicted the step 4 this sub-item had just reverted, in the same section |
| §8 Transaction Tests | "It does not make concurrent *statements* safe, **and no test may assert that it does**" | Forbade the tests 28.2 and 28.4 require. Now asserts the serialization, and carves out the two shapes that genuinely may not be asserted |
| §9.4 method table, §9.8.3 prose | "(serially inside a transaction, §18.5)" | Two more copies of the reverted claim |
| §9.8 generated samples ×3 | "one at a time when ctx carries a transaction (PRD §13.2)" | PRD claims *about* §13.2 that §13.2 no longer makes — these are the doc comments 28.4 will emit |
| §18.5 failure-mode table | "teardown never waits", unqualified | Read as contradicting the depth rule three paragraphs below it. Now says **root** |
| §9.4a hook integration | "Stream fires the query-hook chain once per call" | A refused `Stream` fires **no** hooks, global authorization included — 28.3's acceptance requires zero SQL |
| §19.1 | `QueryFunc(ctx, sql, args, fn)`, filed under "Convenience Methods (on Querier implementations)" | Omitted the `Querier` parameter the real signature carries (`transaction.go:431`) and misfiled package-level functions as methods. Pre-existing drift, but §18.5's new table spells it correctly 370 lines away, so the PRD held two shapes for one function |
| `IMPLEMENTATION_ORDER.md` | `Raw(ctx, sql, args) (Rows, error)` in the Phase 6 entry | Last copy of the old signature in any tracked doc |

The reviewer endorsed both departures from the design doc after verifying their premises
independently — that all three `…WithRelated` emitters open a savepoint, and that `Tx`'s savepoint
stack lets two concurrent scopes release each other's savepoint however well the statements
serialize. It also confirmed §9.4a is implementable by 28.3 as written, with one gap: **§9.4a does
not pin `AllowInTransaction`'s position in the `CallOptions` struct**, which 28.3 must decide since
it moves goldens either way.

**Two observations for later sub-items, not acted on here:**

- `manifest/builder.go:334`'s `call_options.fields` list reads
  `{"SkipHooks", "SkipEvents", "SkipCache", "Tx", "LockMode"}` — it names a `Tx` field the generated
  `CallOptions` does not have, and omits `FieldOptions` and the two tenancy fields. Pre-existing
  drift, outside 28.0's no-code scope; **28.3 touches this list and should fix it there or file it**.
- §9.8's `RETURNING`-then-re-read claim is cited in §18.5's two-locks argument as it was in the
  previous wording; it was carried forward, not re-verified against HEAD.

---

## 28.1 Runtime preconditions — untangle `tx.mu`, close stdlib rows on exhaustion

**PRD Reference:** §18.5 (§5.2 steps 1–2, §2.4, §2.11)

**Status:** **Complete** (landed 2026-09-21, verified 2026-09-22) · **Depends on:** 28.0

Two small runtime-module changes that 28.2 rests on. Neither touches a template.

### Tasks

- [x] **`Begin`** (`database/transaction.go:200-213`) — snapshot `tx.conn` under `tx.mu`, release,
      run the `SAVEPOINT` exec, re-take to mutate `depth` / `callbacks` / `savepointNames`. Removes
      the `defer tx.mu.Unlock()` that spans a network round trip.
- [x] **`Commit`** (`:307-353`) and **`Rollback`** (`:358-395`) — same shape for the
      `RELEASE SAVEPOINT` / `ROLLBACK TO SAVEPOINT` exec and the root `Commit` / `Rollback` call.
- [x] **This is a hard precondition, not the optional tidy-up §5.2 step 1 calls it.** 28.0 decided
      savepoint statements **block** on the reservation. A blocking acquire while holding `tx.mu`
      inverts the lock order — a goroutine holding `connSem` reaches `IsClosed()` (hence `tx.mu`)
      through `database.Conn` at the head of every read, so A holds `connSem` and waits on `tx.mu`
      while B holds `tx.mu` and waits on `connSem`. The untangle is what makes the decided behavior
      constructible.
- [x] **`database/stdlib` `rows.Next()`** (`stdlib.go:158-161`) — close the underlying `sql.Rows` on
      exhaustion so the connection release coincides with `Next() == false`, as it already does on
      pgx. Three lines; `Close` is idempotent and errors still surface through `Err()`.
- [x] Comment the stdlib change with *why* — `database/sql`'s `nextLocked` keeps the connection when
      the driver implements `RowsNextResultSet` and `HasNextResultSet()` is true, `go-sql-driver/
      mysql` implements it, and this repo's stdlib harness connects with `multiStatements=true`.
- [x] Confirm **pgx needs no counterpart** and say so in a comment — `baseRows.Next` calls `Close()`
      unconditionally on exhaustion.
- [x] **Not on the original list, and required by the untangle:** replace what `tx.mu` was
      incidentally providing — at-most-once root teardown, and a savepoint unwind that cannot index
      a concurrently-emptied stack. See the completion record.

### Acceptance Criteria

- [x] No `tx.mu` acquisition spans a driver round trip. `IsClosed()` — reached through
  `database.Conn` at the head of every generated read — no longer waits on a network call.
- [x] On the `database/stdlib` adapter, a drained multi-result-set statement leaves the connection
  free: `SELECT 1; SELECT 2` on MySQL with `multiStatements=true`, drain set 1, then `tx.Exec` →
  `nil` error (today: `busy buffer` plus an `invalid connection` on the rollback).
- [x] **Zero golden movement** — runtime module only, no template touched.

### Tests Required

- [x] Unit: `Begin` / `Commit` / `Rollback` savepoint paths, including the nested-savepoint
      promotion ordering `TestOnCommitCallbackOrdering` already gates.
- [x] Unit: the savepoint unwind survives an emptied stack —
      `TestSavepointUnwindSurvivesAnEmptiedStack`, added by `/verify 28.1`.
- [x] Integration (MySQL, `database/stdlib`): the §2.11 multi-result-set probe — control
      (`SELECT 1`, drain, `tx.Exec` → nil) and multi (`SELECT 1; SELECT 2`, drain set 1, `tx.Exec`
      → nil). **Failing-first on the multi case** before the `rows.Next()` fix.
- [x] `go test -race ./database/...` passes.
- [x] `mysql`, `sqlite`, `postgres_stdlib` example suites pass.

### Completion Record

**Landed 2026-09-21.** `database/transaction.go`, `database/stdlib/stdlib.go`,
`database/pgx/pgx.go` + their tests. Runtime module only; **zero golden movement**, no template
touched, no example file changed.

**Both acceptance probes were confirmed failing-first.** The unit gate
(`TestStatementsDoNotHoldStructMutex`) reaches back into the `Tx` through `IsClosed()` *and*
`OnCommit()` from inside the driver call — the two acquisitions real generated code makes, via
`database.Conn` and the cache layer's `dispatchRefresh`. Against the pre-change tree it deadlocks on five of its
six cases (`savepoint begin`, `savepoint release`, `savepoint rollback`, `root commit`,
`root rollback`); `plain exec` passes there, which is correct — `Exec` already released the mutex
before the driver call, and that is the shape the other five now match. The §2.11 MySQL probe
reproduces the design's table exactly: control passes before and after, multi fails before with
`tx exec: driver: bad connection` **and** `rolling back transaction mrs_probe: stdlib rollback:
invalid connection` — the poisoned-transaction pair, `database/sql` surfacing the busy-buffer
destruction rather than the raw `busy buffer` the design quoted.

**The untangle removes a guarantee `tx.mu` was providing incidentally, and it had to be replaced
rather than dropped.** Holding the mutex across `conn.Commit` is what made root teardown
at-most-once: a second `Commit`, or a `Rollback` racing it, blocked for the round trip and then read
`closed == true`. Release the mutex and both reach the driver — firing the `OnCommit` callbacks
twice (so every deferred event publishes twice and every cache invalidation runs twice) and racing
pgx's own unguarded `dbTx.closed` field, which `-race` would report. **`closed` therefore flips
*before* the driver call rather than after**, and is restored if the driver rejects the teardown so
the caller can still roll back what a failed `COMMIT` left behind. That also happens to be the
answer `IsClosed()` gave before, since a concurrent caller blocked for the round trip and then
observed `true` — the wait is gone, the answer is not. `TestRootTeardownIsClaimedOnce` (commit vs
commit, commit vs rollback) and `TestFailedRootTeardownReopensTransaction` are the gates.

**A second incidental guarantee: the savepoint unwind is now bounds-guarded.** With the mutex held
for the whole body, `tx.depth` could not change between the `RELEASE SAVEPOINT` and the bookkeeping.
Without it, two concurrent savepoint scopes can leave `tx.depth == 0` at the re-lock, and
`tx.callbacks[tx.depth-1]` then indexes `-1` — a **panic in the consumer's process** where the old
code merely unwound the wrong frame. §18.5 carves concurrent scopes out as unsupported and
explicitly declines to *detect* them, so the guard does not detect anything: it restores the old
failure mode (wrong-but-safe bookkeeping) and nothing more. The `len(tx.callbacks) == tx.depth+1`
invariant survives any interleaving on its own, since each mutation is atomic under the lock and
always operates on the current top, so `tx.depth > 0` is the whole guard.

**One pre-existing data race fixed in passing**, in the code this sub-item was restructuring anyway:
root `Commit` read `tx.cancel` outside the lock (`transaction.go:345`) where `Rollback` snapshots it
under the lock. Both snapshot it now.

**Deliberately not touched:** `Tx`'s doc comment still says concurrent statements are unsafe and
that two goroutines on one `txCtx` is undefined behavior. That is still true at 28.1 — the
reservation does not exist until 28.2, and 28.6 is the documentation sweep that makes the promise.
The paragraph added here states only what 28.1 itself establishes: no `tx.mu` acquisition spans a
round trip, and why.

**The auto-review found five things; four were fixed inside the sub-item and one is booked on
28.2.** Fixed here:

| Finding | Fix |
|---|---|
| `t.Fatalf` called from `runWithin`'s goroutine — `guidelines/TESTING.md:396` forbids it outright | `t.Errorf` + `return` |
| `TestRootTeardownIsClaimedOnce` closed its `entered` channel unconditionally inside `commitFn`, so a **regression** would panic the binary on a double close instead of reporting `teardowns = 2` | `sync.OnceFunc` |
| The restore-on-error comments claimed *"the transaction is still open — the caller may retry or roll back"*, which is true of `Tx` and **false of both shipped adapters** (`database/sql` sets `tx.done` before calling the driver; pgx returns `ErrTxClosed`). `TestFailedRootTeardownReopensTransaction` asserted `Rollback() == nil`, which passed only because the mock's nil `rollbackFn` returns nil — it pinned mock behaviour, not adapter behaviour | Comments say what is actually true (the claim is released so `Tx` does not itself reject the next teardown; whether the driver accepts one is the adapter's business). The test now asserts the driver was **reached**, which is the property that belongs to `Tx` |
| The `Tx` doc paragraph never mentioned the early `closed` flip — the most surprising property of the code now, explained only in an inline comment | Stated in the doc comment |

**A hole the untangle opened in `Begin`, flagged by the review for 28.2 and closed here instead —
28.1 introduced it.** Pre-change, `Begin` held `tx.mu` for its whole body, so the `closed` check and
the bookkeeping push were one atomic step. With the mutex released across the `SAVEPOINT` statement
they are two, and a root teardown landing between them left a closed `Tx` at `depth > 0` carrying a
savepoint name nothing can release — and, observably, `Begin` **returning success for a savepoint
scope the caller does not have**. The second critical section re-checks `closed`.
`TestBeginRejectsATeardownRacingItsSavepoint` makes the interleaving deterministic by tearing the
transaction down from inside the `SAVEPOINT` statement itself — reachable only because the mutex is
no longer held across it — and is failing-first. The dangling frame itself is unreachable through
the public API (`Commit` and `Rollback` both short-circuit on `closed`), so the test asserts the
observable half and says so.

**Booked on 28.2, not fixed here: what `IsClosed()` should report during a root teardown.** See
28.2's task list for the full argument. In short, the early `closed` flip means a root teardown
*that then fails* reports `IsClosed() == true` while the transaction is still open, and the cache
layer's §18.6 hook shape (`cache.go.tmpl:1002`, `:1027`) then invalidates immediately instead of
deferring. The reviewer's proposed `tearingDown` alternative trades that for **silently dropping**
an invalidation on the far more common succeeding path, because `Commit` snapshots
`tx.callbacks[0]` before the driver call. Neither recovers the pre-change behaviour, which was
correct for both outcomes only because it blocked — and §18.5's own rule is that root teardown never
waits. 28.2 owns the root-teardown acquire and can take the third option (snapshot the callbacks
after the driver call); whichever it picks, §18.5 or §18.6 must then state it.

**The reviewer independently verified the two highest-risk changes.** It re-derived
`Rows.nextLocked` and confirmed the stdlib change is a no-op for every single-result-set query; and
it checked every savepoint mutation site to confirm `len(callbacks) == depth+1` *and*
`len(savepointNames) == depth` hold under any interleaving, so `tx.depth > 0` is genuinely the whole
guard and silently doing nothing reproduces the pre-change failure mode exactly. One consequence it
asked to have written down is now in the code: when the guard is false the `RELEASE SAVEPOINT` has
already run, so `Commit` still returns `nil`.

**`/verify 28.1` closed one gap and took two amendments; no FIX was logged.** The gap: the depth
guard above shipped with **no regression gate** — the false-guard branch was reachable but nothing
exercised it, so deleting the guard passed the suite. `TestSavepointUnwindSurvivesAnEmptiedStack`
is that gate, table-driven over both unwind verbs, and it is failing-first: with the guard removed
`RELEASE SAVEPOINT` panics `index out of range [-1]` and `ROLLBACK TO SAVEPOINT` on the slice
bound. It makes the interleaving deterministic the same way `TestBeginRejectsATeardownRacingItsSavepoint`
does — popping the frame from inside the savepoint statement — and asserts the root is left usable,
which is what a corrupted stack would break. The verify pass also fixed a test nit the reviewer
raised (`TestRootTeardownIsClaimedOnce`'s `commitFn` parked a *second* COMMIT on `<-release`, so a
regression timed out under `runWithin` and reported a held mutex — the wrong diagnosis — while
stranding the first goroutine; the second call now returns at once and the `teardowns = 2`
assertion reports it) and folded two amendments into **28.2**'s `IsClosed()` task: the affected
consumer list is **four** surfaces, not one (`database.Conn` silently falls back to the pool,
`NewTransaction` opens a new **root** transaction instead of a savepoint, and `InTransaction` fails
the §9.6a lock-mode guard — besides the two `cache.go.tmpl` sites), and the failed-COMMIT
consequence is **§18.6's critical invariant verbatim**, not merely a wasted re-read.

One premise in the record above was checked and corrected. The stdlib `Next()` close does **not**
swallow a driver close error: Go 1.27.1's `Rows.close` ends `rs.lasterr = rs.lasterrOrErrLocked(err)`
(`sql.go:3519`), so a `rowsi.Close()` error replaces the `io.EOF` sentinel and surfaces through
`Rows.Err()`. It is relocated from `Close()` to `Err()`, and every site that checks it —
`table/upsert.go.tmpl:207`, `:358`, `:429` and `QueryFunc` — reads `rows.Err()` first, so it is read
on the checked path either way.

**Verification.** `make check` green across all eight modules. `go test -race -count=1
./database/...` green including the pgx/MySQL/SQLite testcontainer suites. `make check-examples`
green across all twelve example trees. Tool preflight clean — `make tools-check` matched all three
`.tool-versions` pins (golang 1.27.1, golangci-lint 2.13.2, gofumpt 0.12.0), so no repeat of the
16.8g gofumpt drift. Neither `/security-review` trigger fires: nothing under `cmd/sqlgen/cli/**`
changed, and `database/` carries no `//go:embed`.

---

## 28.2 The reservation — `connSem`, `txRows`, `txRow`, non-blocking teardown

**PRD Reference:** §18.5, §20.5 (§5.2 steps 3–4, §3.1–§3.4, §3.8)

**Status:** **Complete** (landed 2026-09-22) · **Depends on:** 28.1

The design itself. One struct field and two small wrapper types; **no template is edited**.

### Tasks

- [x] Add `connSem chan struct{}` (capacity 1) to `Tx`, allocated in `NewTx`. A channel, not a
      `sync.Mutex`, because acquisition must be selectable against `ctx.Done()` (§2.7).
- [x] `acquireConn(ctx)` (§3.3) — non-blocking `select` first, then a `select` over
      `connSem` / `ctx.Done()`. The two-step form is **not** an optimization: a single `select` over
      both picks pseudo-randomly when both are ready, so a statement on an already-expired context
      would non-deterministically run or fail. Uncontended must always win.
- [x] `releaseConn()` — an unguarded receive, sound because it is only ever reached through the
      `sync.OnceFunc` that pairs it 1:1 with an acquire.
- [x] **No bookkeeping beside `connSem`** (§3.1, §6.1) — no in-flight statement, no holding
      goroutine id, no third mutex. Nothing on the reservation path may take `tx.mu`; §2.4's second
      measured deadlock is unconstructible only while that holds. Comment it as a property.
- [x] `Tx.Exec` — acquire across the driver call, release on return. An `Exec` has no result set, so
      that is its whole busy window.
- [x] `Tx.Query` — acquire; on driver error release and return; on success return
      `&txRows{Rows: r, release: sync.OnceFunc(tx.releaseConn)}`.
- [x] `txRows.Next()` — **release on `Rows.Next() == false`**. This one line is what makes the
      migration free (§2.5, §2.6). `txRows.Close()` — `defer r.release()`.
- [x] `txRow` — `QueryRow` acquires and `Scan` releases, since both adapters execute eagerly at
      `QueryRow` and consume the row in `Scan`. `QueryRow` has no error return, so an acquire
      failure comes back as a `Row` whose `Scan` reports it (`closedRow` at `:252-259` is the
      precedent).
- [x] `acquireForTeardown()` (§3.4) — try-acquire, else proceed **without** the reservation and let
      the driver report it. A `COMMIT` that cannot run strands a pooled connection for the life of
      the process, which is strictly worse than a loud failure. No timeout constant, no config field.
- [x] Wire `Begin` / `Commit` / `Rollback` per **§18.5's recorded rule: depth sets the acquire
      mode.** `Begin`, and a savepoint-depth `Commit`/`Rollback`, take the ordinary blocking
      `acquireConn` — they run while the enclosing `fn` is live. Only a **root** `Commit`/`Rollback`
      uses `acquireForTeardown`.
- [x] **Hold `connSem` across the savepoint bookkeeping, not just the statement.** `Begin` execs
      `SAVEPOINT` and then pushes onto `savepointNames`/`depth`; if the reservation is released
      between the two, two goroutines can exec in one order and push in the other, leaving `Tx`'s
      stack disagreeing with the server's. Acquire once, exec, mutate, release. `tx.mu` is taken
      only in short windows **nested inside** `connSem`.
- [x] **Lock order is `connSem` → `tx.mu`, never the reverse.** Nothing may wait for `connSem` while
      holding `tx.mu`. Worth asserting in a comment: it is the invariant 28.1 exists to establish
      and the one §2.4 measured twice in the negative.
- [x] Fix **`QueryRowFunc`** (§3.8) — release after `fn` returns whether or not `fn` scanned. It
      lives in the same package, so a `*txRow` type switch plus `defer tr.release()` is enough; the
      `sync.OnceFunc` makes a `Scan` inside `fn` a no-op on the deferred call.
- [x] Rewrite the `Tx` doc comment (`:75-89`). Its first half becomes false. Its second needs §2.4's
      refinement: widening ***`tx.mu`*** deadlocks; a **disjoint** reservation scoped to the driver's
      busy window does not.
- [x] **Decide what `IsClosed()` reports during a root teardown — carried from 28.1, and 28.2 owns
      it because 28.2 is what makes root teardown permanently unserialized.** 28.1 replaced the
      mutex's incidental at-most-once teardown guarantee by claiming `closed` **before** the driver
      call and restoring it on driver error. The consequence: for the length of a root teardown
      **that then fails**, `IsClosed()` reports `true` while the transaction is in fact still open,
      and §18.6's normative hook shape — `tx != nil && !tx.IsClosed()` → defer, else fire now, which
      is exactly `cache.go.tmpl:1002` and `:1027` — takes the else branch and invalidates
      immediately for a transaction that may still roll back. **That is §18.6's critical invariant
      verbatim** — "side effects must not fire during a transaction" — so whatever 28.2 picks must
      be stated against **§18.6**, not only §18.5. `event_hooks.go.tmpl:269` tests only
      `tx != nil` and is unaffected.
      **The consumer list is four, not one** (found by `/verify 28.1`'s reviewer pass). Besides the
      two cache sites: `database.Conn` (`transaction.go:500`) falls back to the **pool**, so a
      generated read concurrent with a failing teardown runs *outside* the transaction;
      `NewTransaction` (`:303`) opens a **new root transaction** instead of a savepoint; and
      `InTransaction` (`:72`) → `shared/_lock_mode_guard.tmpl:28`, `view/refresh.go.tmpl:37`,
      `table/get.go.tmpl:400` return the §9.6a lock-mode precondition error. All four share the root
      cause and all four move with whatever 28.2 decides.
      **Forward coupling to watch:** `TestStatementsDoNotHoldStructMutex` pins `wantClosed: true`
      on its `root commit` / `root rollback` cases, hard-coding the early-flip semantics. Revisiting
      the decision means revisiting that expectation.
      **The reviewer's proposed alternative — a separate `tearingDown bool`, leaving `closed`
      alone — does not dominate it, and the reason should be checked before adopting it.** With
      `tearingDown`, the window on a **succeeding** teardown reports `IsClosed() == false`, so the
      cache calls `OnCommit`, which appends *after* `Commit` has already snapshotted
      `tx.callbacks[0]` — the invalidation is then **silently dropped**. So the choice is: fire a
      cache invalidation too eagerly on the rare failing path (degrades to a wasted re-read), or
      drop one on the common succeeding path (degrades to indefinite staleness). 28.1 chose the
      first deliberately. A third option 28.2 is better placed to take: snapshot the callbacks
      **after** the successful driver call, which removes the drop and makes `tearingDown` viable.
      **What neither option recovers is the pre-change behaviour**, which was correct for *both*
      outcomes only because it blocked for the round trip — and not blocking is §18.5's own
      recorded decision ("root teardown never waits"). So this is a consequence of the phase's
      design, not a defect 28.1 introduced on its own authority. Whatever 28.2 picks, **§18.6 or
      §18.5 must state it** — today neither does.

### Acceptance Criteria

- A `Tx`'s statements serialize on all three dialects. The `graphql` relationship fan-out, with
  FIX-221's bound removed, runs under `-race` with **zero** data races (§2.6 baseline: 48 races with
  no lock, 28 with a method-scoped lock + `defer`).
- `Rows.Err()` stays valid after a drain-triggered release on both adapters.
- Teardown never blocks: with a result set leaked open, `Commit` and `Rollback` return the driver's
  pre-existing `conn busy` in hundredths of a second, not a permanent hang (§2.7 → §2.8).
- A leaked result set followed by another statement on a `ctx` with a 2s deadline fails at 2.00s
  with a named error, not a hang.
- Concurrent `Begin` on one `Tx` leaves `savepointNames` in the same order the server saw the
  `SAVEPOINT` statements — the stack and the bookkeeping never disagree.
- **Zero golden movement, zero template edits.** Runtime module only.

### Tests Required

- [x] Unit (`database/`): concurrent `Begin` against a fake `TxConn` that records statement order —
      `savepointNames` must match the recorded exec order. Failing-first if the reservation is
      released between the exec and the push.
- [x] Unit (`database/`): acquire/release pairing; `acquireConn` on an already-expired context is
      deterministic both when the connection is free **and** when it is held — **and the two answers
      differ**: free *succeeds* (§3.3's uncontended-wins rule, which is what the two-step select
      exists for) and held *fails*. This line originally said it fails in both cases, which
      contradicts §3.3; corrected 2026-09-22 when the test was written against the design.
      `txRows.Next()` releases exactly once when the set is both drained and explicitly closed;
      `txRow.Scan` releases once.
- [x] Integration (pgx): `TestLoadRelationships_*` with the bound removed, under `-race`, three
      relationships selected inside `WithTx` — 0 races. **Failing-first** without the reservation.
- [x] Integration: the §2.8 leak probes as regression tests — leak → `Rollback`, leak → `Commit`,
      leak → next statement with a `ctx` deadline, leak → next statement with `TxOptions.Timeout`.
      These are the only coverage of the paths §2.7 measured as permanent hangs.
- [x] Integration: the §2.3 single-goroutine probe — `Query` an undrained two-row set, then `Exec`
      on the same `txCtx` with a deadline → a bounded, named error, never `conn busy`.
- [x] `go test -race ./database/...` and all twelve example suites pass.

### Completion Record

**Landed 2026-09-22.** Three new files — `database/reservation.go` (177), and the integration
probes `database/pgx/reservation_integration_test.go` (282) and
`database/stdlib/reservation_integration_test.go` (171) — plus `database/transaction.go`,
`database/transaction_test.go` and `docs/PRD.md` at +803/−146 for the one decision this sub-item
owned. **Zero template edits, zero golden movement** — the runtime module and the spec, nothing
else. The design's central claim held: `for rows.Next() { … }` then the next statement is already
the correct shape, so not one call site changed, in the runtime or in any of the twelve generated
trees.

**Gates, all re-run after the review fixes:** `make check` green; `make check-examples` green across
all twelve trees and three dialects with the working tree still showing no generated file modified;
`go test -race ./database/...` green including the pgx and stdlib containers.

**What landed.** `connSem` is one capacity-1 channel on `Tx`, allocated in `NewTx`, with
`acquireConn` / `releaseConn` / `acquireForTeardown` and the `txRows` / `txRow` / `errRow` wrappers
in their own file. `Exec` holds it across the driver call; `Query` hands it to the result set and
`txRows.Next()` gives it back at exhaustion; `txRow.Scan` gives it back at the end of the scan.
`Begin` and a savepoint-depth `Commit`/`Rollback` take the blocking acquire — §18.5's depth rule —
and hold it across the **bookkeeping** as well as the statement; only a root teardown uses the
try-acquire. `QueryRowFunc` releases after `fn` returns whether or not `fn` scanned.

**The `IsClosed()` question 28.1 carried forward is decided: `closed` now means the teardown
*completed*, and a separate `tearingDown` claim is what makes a root teardown at-most-once.** Both
of 28.1's candidates were weighed and both were rejected. The early flip (28.1's) contradicts
**§18.4's own wording** and takes the wrong branch on all four reader surfaces, three of them
*silently* — §18.6 fires a side effect for a transaction that has not committed, `database.Conn`
routes a concurrent read to the **pool** so it cannot see the transaction's own writes,
`NewTransaction` opens a second root transaction, and `InTransaction` refuses a §9.6a lock-mode
read. The reviewer's plain `tearingDown` drops a callback registered during the window. The third
option the task list named closes that: **the callback list is snapshotted after the driver call**,
in the same critical section that sets `closed`, so an `OnCommit` landing while the `COMMIT` is on
the wire is still picked up. What remains is the gap between a hook's own `IsClosed()` and its
`OnCommit` — two acquisitions no snapshot ordering can fuse — now a few instructions wide instead
of a network round trip. `§18.4`, `§18.5` and `§18.6` all state it; §18.6 gets the argument,
because §18.6's invariant is what decides it.

**A statement issued during the teardown window is rejected rather than sent.** It cannot succeed
whichever way the teardown lands, so `Exec`, `Query`, `QueryRow` and `Begin` gate on
`closed || tearingDown` and fail at once with a named error — keeping exactly what 28.1's early
flip bought on those paths, without lying to `IsClosed()`. That asymmetry between the statement
guard and the flag is the whole shape of the decision.

**Two of 28.1's tests had to move, and the second one is a finding.**
`TestStatementsDoNotHoldStructMutex` lost its `wantClosed` column — every case now expects `false`,
the two root teardowns included — which is the forward coupling the task list predicted.
`TestSavepointUnwindSurvivesAnEmptiedStack` **deadlocked**: it drove its interleaving by
re-entering a savepoint teardown from inside another one's statement, and that now waits for a
reservation its own caller holds. The window it gates is still reachable — `depth` is read *before*
the acquire, to choose the acquire mode, so a goroutine that already holds the reservation can pop
the frame while this one waits — so the guard stays and the test was rebuilt around two goroutines,
with `synctest.Wait` standing in for the unobservable "the second one is parked on the reservation".
Removing the guard still panics with `index out of range [-1]`, re-confirmed.

**Six probes were confirmed failing-first**, each against a surgically reverted tree:
`Begin` releasing before its bookkeeping breaks the concurrent-`Begin` ordering gate (RELEASE order
stops inverting SAVEPOINT order); deleting the depth guard panics; restoring the early `closed`
flip fails all three `IsClosed()`-window assertions; snapshotting the callbacks before the driver
call drops the callback registered in the window; deleting `txRows.Next()`'s release hangs
`TestSavepointStatementsWaitForTheReservation` (synctest reports the bubble stuck); and removing
the reservation from `Tx.Query` reproduces the pgx data races the fan-out test exists to gate.

**Measured, not reasoned about.** pgx: the three-way fan-out inside `WithTx` runs at **0 races**
under `-race` and the connection survives it; a leaked result set meets `pgx commit: conn busy` /
`pgx rollback: conn busy` in **0.02s** rather than stranding the connection; and all three
bounded-wait probes — `ctx` deadline, `TxOptions.Timeout`, and §2.3's undrained two-row set on the
same goroutine — fail at the 2s bound with the reservation's own named error and **never**
`conn busy`. `TxOptions.Timeout`'s clock starts at `NewTransaction`, not at the statement, which
the first run of that probe caught. MySQL and SQLite: the same fan-out passes on both, which is the
cross-dialect half of the acceptance criterion — honestly *not* failing-first there, since
`database/sql` already serialized it internally, and the assertion is that the reservation did not
change that.

**Lint surfaced two pass-through wrappers.** `txRows.Close` and `txRow.Scan` trip `wrapcheck`;
both carry a `//nolint:wrapcheck` citing `guidelines/ERRORS.md`'s one-wrap-per-layer rule — both
adapters already map and wrap, and neither wrapper adds context of its own. `runWithin` also lost
its always-5s parameter (`unparam`) in favour of a named constant.

**The auto-review found seven things. Five were fixed inside the sub-item; two are handed forward
because 28.2 may not edit a template.**

The substantive one is a **hole in §18.5's own rule, of exactly the class 28.0's decision was
correcting**. §18.5 justified blocking at savepoint depth with *"a savepoint statement runs while
the enclosing `fn` is still live"* — which is **false for the one savepoint teardown
`WithTransaction` issues on its own behalf**: by the time a nested `WithTransaction` reaches its
`RELEASE SAVEPOINT`, its own `fn` has returned. So a nested scope that leaks a result set and then
unwinds waits **for itself**, forever by default — and because the hung goroutine sits below the
enclosing `WithTransaction` on the stack, the root teardown is never reached either, so the pooled
connection is stranded exactly as §2.7 described. The identical leak at the root returns in 0.02s.
**The acquire mode was not changed**, and that is the finding's answer rather than a deferral:
`Commit`/`Rollback` take only a `ctx` and cannot tell the `WithTransaction` teardown from a
consumer's own `Commit` racing another goroutine's read — and a try-acquire in the second case
fails a generated `…WithRelated` with `conn busy`, which is FIX-221's defect returning. It is the
§6.1 residual (a waiter that is its own holder) reached through a different verb, with the same
bound and the same obligation. §18.5's Savepoints subsection now states the cost and why extending
the exemption is not available, and the failure-mode table gained the missing row.

Also fixed: **§18.5's caller rule had no `QueryRow` analogue** — a `Row` has no `Close`, so taking
one from `Querier(ctx)` and discarding it unscanned is the same leak in the other shape, now stated
in §18.5 and on `txRow` itself. **§18.6's residual window was understated**: `cache.go.tmpl:1002`
registers **two** callbacks behind one `IsClosed()` check, so the post-snapshot gap admits a *split*
registration, not only an all-or-nothing drop; §18.6 says so and names the fix (register the group
as one callback). **`acquireConn`'s error doubled the transaction name** — every caller already
wraps with it, producing `tx exec held: transaction held: waiting for …`; the prefix is gone. And
the `//nolint:wrapcheck` reason on `txRow.Scan` was simply wrong: it claimed wrapping would break
the `database.ErrNotFound` sentinel, but that is matched with `errors.Is`, which survives `%w`.

**Two are handed forward.** `Raw` still returns `database.Rows` up through the hook chain
(`client.go.tmpl:301-313`), so a hook erroring after the terminal ran now leaks a reservation and
not merely a result set — **28.5's scan callback removes it**, and it is noted here so the handoff
does not lose it. And `mysqlVersionCache.get` (`client.go.tmpl:107`) runs
`database.Conn(ctx, …).QueryRow` inside a client-wide `sync.Once` — **now task-listed on 28.3**,
which is already editing the guard the probe sits inside. Measuring it sharpened the review's
account: the probe is not *sometimes* inside a transaction, it is **always** inside one, because
`shared/_lock_mode_guard.tmpl:33` reaches `get()` only after `database.InTransaction(ctx)` passes.
The two failure modes have different causes, which is worth keeping straight. The **permanently
poisoned cache** is this sub-item's `IsClosed()` decision: `InTransaction` now reports `true` during
a root teardown where the early flip made it `false`, so the guard no longer rejects before `get()`
runs and `sync.Once` caches the `tearingDown` error client-wide forever. The **client-wide stall** —
a first probe behind another goroutine's live read blocking every `get()` caller, including callers
on unrelated transactions and on the pool — is the reservation itself. The fix is one line
(`c.querier.QueryRow` in place of `database.Conn(ctx, c.querier).QueryRow`) and is a simplification
rather than a workaround: `SELECT VERSION()` is a server property, not transaction state.

---

## 28.3 `Stream` refuses a transaction, with an `AllowInTransaction` opt-in

**PRD Reference:** §9.4a, §9.6a (§5.2 step 5, §2.10, §4.6, §6.2)

**Status:** **Complete** (landed 2026-09-22) · **Depends on:** 28.0, 28.2

`Stream` is the only generated method that runs **consumer code inside an open result set**, so it
is the one API the reservation makes worse: a write inside the `yield` body goes from a loud
`conn busy` to a hang that also stalls pool teardown (§2.10 probe A). §4.6 establishes that
`Stream`'s contract and a transaction's cannot both hold, so it should say so rather than half-work.

**Do not** simply opt `Stream` out of the reservation — §2.10 probe B measures 13 data races on
that variant. An opted-in `Stream` must **hold** the reservation.

### Tasks

- [x] Add `AllowInTransaction` to `CallOptions` per 28.0's emission decision — **unconditional**,
      a parallel append beside `LockMode` (`gen/context_shared.go:168-171`).
- [x] **Decide the field's position in the struct.** §9.4a records that the field is emitted and why,
      but deliberately does not pin where it sits; it moves goldens either way. `LockMode` is last
      today, and grouping the two read-side knobs is the obvious reading.
- [x] **Fix `manifest/builder.go:334`'s `call_options.fields` list while it is open.** It reads
      `{"SkipHooks", "SkipEvents", "SkipCache", "Tx", "LockMode"}` — names a `Tx` field the generated
      `CallOptions` does not have, and omits `FieldOptions` and both tenancy fields. §30.4.1 elides
      the list, so the PRD neither contradicts nor specifies it; this is drift, not a spec change.
- [x] Do **not** propagate it through `nestedChildOptions` (`context_shared.go:296-314`) — same
      reasoning as `LockMode` and `FieldOptions`.
- [x] Emit the guard in `table/stream.go.tmpl`, immediately after `resolveCallOptions(opts)` and
      before the `executeQuery` call — surfaced through `yield`, since `Stream` returns an iterator
      and has no error return. Mirrors `shared/_lock_mode_guard.tmpl`'s `database.InTransaction`
      precondition shape; consider factoring it the same way.
- [x] Error text names the alternative and the opt-in's obligation: *use `Connection` in a loop, or
      set `AllowInTransaction` for a snapshot-consistent stream that issues nothing else on this
      `txCtx`*.
- [x] `Stream`'s generated doc comment carries the rule, so a consumer reads it at the call site
      rather than in the PRD.
- [x] Update `manifest/builder.go:332-335`'s `call_options.fields` list if 28.0 decided it carries.
- [x] **Take the version probe off the transaction's connection — carried from `/implement 28.2`'s
      review, and it lands here because this is the guard the probe sits inside.**
      `client.go.tmpl:107` reads `database.Conn(ctx, c.querier).QueryRow(ctx, "SELECT VERSION()")`
      inside a client-wide `sync.Once`. `shared/_lock_mode_guard.tmpl:33` reaches
      `c.mysqlVersion.get(ctx)` **only after `database.InTransaction(ctx)` passes**, so `Conn` there
      always returns the `*Tx` — the probe is never on the pool path, and 28.2 gave that two new
      failure modes. **(a) A permanently poisoned cache**, created by 28.2's `IsClosed()` decision:
      during a root teardown `InTransaction` now reports `true` (it reported `false` under the early
      flip, so the guard rejected before `get()` ever ran), the probe meets the `tearingDown`
      rejection, and `sync.Once` caches that error client-wide **forever**. **(b) A client-wide
      stall**, created by the reservation itself: a first probe behind another goroutine's live read
      blocks inside `once.Do`, so every `get()` caller blocks with it — including callers on
      unrelated transactions and on the pool. The fix is one line and is a simplification, not a
      workaround: `row := c.querier.QueryRow(ctx, "SELECT VERSION()")`. `SELECT VERSION()` is a
      *server* property, not transaction state, and the cache is already client-wide, so it already
      assumes one version; taking the transaction's connection for it buys nothing and couples a
      cached global to transaction state. Both failure modes go with it.
- [x] Regenerate all affected goldens.

### Acceptance Criteria

- `Stream(txCtx, …)` without the opt-in yields exactly one `(nil, error)` and issues **no SQL**.
- With `AllowInTransaction` set, `Stream` is the ordinary read of §3.2 — it holds the reservation for
  the whole iteration, and a concurrent `GetMany` on the same `txCtx` queues behind it safely.
- `Stream` outside a transaction is byte-identical in behavior to today.
- Golden movement is confined to what the new `CallOptions` field, the guard, and the version-probe
  fix produce, and matches 28.0's recorded emission decision. The probe fix adds exactly **four**
  files — `client_gen.go` in `mysql` and `tenancy_mysql`, `expected/` and `models/`. `models_gen.go`
  carries only the `mysqlVersion *mysqlVersionCache` field, which does not move.
- The version probe no longer reaches `database.Conn`: `grep -n "database.Conn" ` over the four
  regenerated `client_gen.go` files returns nothing inside `mysqlVersionCache.get`.

### Tests Required

- [x] Unit (gen): the guard renders; `CallOptions` carries the field under the decided condition;
      `nestedChildOptions` does **not** copy it.
- [x] E2E: `Stream` inside `WithTx` without the opt-in → refused before any SQL (assert via a
      counting querier / `QueryTracer` that zero statements were issued).
- [x] E2E: `Stream` inside `WithTx` **with** the opt-in → all rows stream, and §2.10 probe B (a
      concurrent `GetMany` on the same `txCtx`) passes under `-race` with 0 races.
- [x] E2E: `Stream` outside a transaction is unaffected — existing `stream_test.go` suites in
      `postgres`, `mysql`, `sqlite`, `tenancy` pass unchanged.
- [x] `mysql_version_test.go` (both copies) passes **unchanged**. It builds the cache over a
      `mock.Querier` and calls `get` on a plain context, so it never exercised `database.Conn`'s
      transaction arm — which is why the probe fix costs no test churn, and why it needs its own
      gate below rather than relying on that file.
- [x] E2E (`mysql`, `-race`): a first-ever `LockForUpdateNoWait` read issued on a `txCtx` while
      another goroutine holds the reservation on that same `txCtx` completes, and a `get()` from a
      *different* client path does not block behind it. Failing-first against
      `database.Conn(ctx, c.querier)`.

### Completion Record

**Landed 2026-09-22.** Six source files (+92/−14) — `gen/context_shared.go`,
`gen/templates/table/stream.go.tmpl`, `gen/templates/client.go.tmpl`, `manifest/builder.go`, plus
`gen/orchestrate.go` and `gen/context_tenancy.go` for the exported tenancy predicate the review
asked for — `docs/PRD.md` (+11/−8), five test files touched and three new ones, against
**107 regenerated golden files**. The golden movement is exactly the three causes
the acceptance criterion names and nothing else: 25 `shared_types_gen.go` copies (24 example trees +
`gen/testdata/golden/`) for the new field, 20 manifest artifacts (`manifest_gen.json` +
`manifest/_conventions.md` across five trees × two copies) for the corrected field list, the
per-table files carrying `Stream` for the guard, and **exactly four** `client_gen.go` — `mysql` and
`tenancy_mysql`, `expected/` and `models/` — for the version probe.

**Gates, all re-run after the review fixes:** `make check` exit 0 (lint + vet + `-race` unit tests
across all eight modules); `make check-examples` exit 0 across all twelve example trees and three
dialects. Tool preflight matched `.tool-versions` exactly (go 1.27.1, golangci-lint 2.13.2,
gofumpt 0.12.0), so no 16.8g-style drift. Beyond the suites, an independent sweep confirms **222
generated `Stream` methods across 58 files carry exactly 222 guards, zero mismatches** — every
dialect and the `file_per_table` sqlite layout included — and `database.Conn` appears nowhere inside
`mysqlVersionCache.get` in any of the four regenerated `client_gen.go`, which is the acceptance
criterion's own grep.

**Two probes confirmed failing-first.** Reinserting `Tx` into an example tree's emitted
`manifest_gen.json` fails the drift guard with the offending name in the diff; reverting the
generated `mysqlVersionCache.get` to `database.Conn(ctx, c.querier)` fails the MySQL probe test at
10.03s with the diagnostic naming the cause.

**The guard is where §9.4a says and refuses before anything happens.** `database.InTransaction(ctx)
&& !options.AllowInTransaction` sits between the `SkipCache` force and `executeQuery`, so a refused
`Stream` issues no SQL *and* fires no hooks — global authorization included, which is the half a
"returns an error" test would miss. It is gated in both directions: a codegen test pins the guard
between `resolveCallOptions` and `executeQuery` (before the first would read an unresolved option,
after the second would build the chain), and an E2E test pins zero statements on the wire.

**Three decisions the task list left open.**

1. **Position: appended after `LockMode`.** §9.4a deliberately does not pin it and both placements
   move the same goldens, so the tiebreak is readability — the two read-side knobs now sit together
   at the end of the struct, after the hook toggles, the tenancy pair and `FieldOptions`.
2. **The guard is inlined in `table/stream.go.tmpl`, not factored into `shared/`.** The lock-mode
   guard is shared because three templates call it; this one has exactly one call site, and views
   emit no `Stream`. A `define`/`template` round trip for a four-line fragment with no second caller
   buys indirection, not deduplication. If a second `Stream`-shaped surface ever appears, it factors
   then.
3. **`call_options.fields` is derived, not re-typed.** The old hard-coded list named a `Tx` field the
   struct has never had and omitted three that exist. Rather than hand-correct a second literal that
   can drift again, `callOptionsFieldNames` reproduces `sharedTypeDefinitions`' emission conditions,
   and **a drift guard now compares the two** — `TestCallOptionsFieldsMatchGeneratedStruct` parses
   the `CallOptions` struct out of each example tree's own `shared_types_gen.go` and diffs it against
   that same tree's own emitted `manifest_gen.json`, order included. Both halves are pipeline output,
   so no fixture can be faithful-in-the-test and wrong-in-the-pipeline. It runs over all five
   manifest-emitting trees, which covers both arms of the tenancy gate, and it fails the moment `Tx`
   is put back.

**The version probe is off the transaction's connection.** `c.querier.QueryRow(ctx, "SELECT
VERSION()")` replaces `database.Conn(ctx, c.querier).QueryRow(...)`, and the E2E gate is the
client-wide stall rather than the poisoned cache, because the stall is the one a test can construct
without reaching into teardown: tx1 goroutine A holds tx1's reservation on an undrained result set,
tx1 goroutine B issues the client's first-ever `LockForUpdateNoWait` read and so is the goroutine
that enters `once.Do`, and **tx2 — a different transaction on a different, idle connection** —
issues its own version-gated read. With the probe on the transaction, B parks inside `once.Do` and
tx2's read parks behind it for no reason connected to either transaction; with the probe on the
pool, `once` completes in milliseconds while A is still holding and tx2's read runs at once. B's
read is then confirmed to complete once A releases, so the test parks no goroutine on a reservation.
The two readers deliberately target **different rows**: both are `FOR UPDATE NOWAIT`, so sharing a
row would have the queued one fail with `ER_LOCK_NOWAIT` — row contention impersonating the
reservation contention under test. `mysql_version_test.go` passes unchanged in both copies, as the
task list predicted: it builds the cache over a `mock.Querier` and never exercised `database.Conn`'s
transaction arm.

**A test was pinning gofumpt's alignment, and only a longer field name could reveal it.**
`TestGenerate_viewOnlyTenantedPackageStillEmitsExplicitTenantOption` asserted
`strings.Contains(shared, "Tenant       *uuid.UUID")` — seven literal spaces. `AllowInTransaction`
is the longest name `CallOptions` has ever carried, so gofumpt re-aligned the whole struct and the
assertion broke while the property it exists for ("the field exists and is concretely typed") still
held perfectly. Now matched on a space-run regex, with the reason in a comment. Worth recording
because the class is general: a `strings.Contains` over formatted output silently couples a semantic
assertion to column positions, and the coupling is invisible until something widens the struct.

**`nestedChildOptions` does not propagate it**, for the same reason as `LockMode` and
`FieldOptions` — the existing three-shape helper test gained `AllowInTransaction` to its
never-crosses list, so all three non-propagating fields are now gated by one assertion rather than
by the two that happened to be named when it was written.

**The auto-review found four things and all four were fixed inside the sub-item.**

**The PRD contradicted itself about the very field this sub-item added.** §9.4a:3310 asserts
`AllowInTransaction` is on *every* generated `CallOptions[FO]`, but §9.6's canonical listing — the
one a reader goes to for the struct's shape — still showed seven fields and neither
`AllowInTransaction` nor `Tenant`. It now carries the full shape in declaration order, plus one
sentence saying which two fields are gated and why the other two method-specific ones are not. §9.4's
second copy is a design-principles sketch that predates `LockMode` and `SkipTenancy` entirely; rather
than turn it into a third thing to keep in sync it is now explicitly labelled abbreviated and points
at §9.6. Two anchors in the new text were wrong on first write and were repaired
(`#2944-per-call-tenancy-overrides` does not exist; the heading is
`#2944-calloptionsskiptenancy-and-calloptionstenant`).

**The `Tenant` gate was a second implementation of a predicate `gen` already owns — which is the
exact drift this sub-item was closing.** The first cut reproduced `firstTenantedEntityType`'s logic
as a local `anyEntityIsTenanted`. The two agreed, but agreeing-today is what the `Tx` entry did too.
`firstTenantedEntityType` is now exported as **`gen.FirstTenantedEntityType`** — it is the
*definition* of "this package emits `CallOptions.Tenant`", since `sharedTypeDefinitions` gates the
field on a non-empty result — and `callOptionsFieldNames` calls it. One predicate, two readers.

**A hand-built MCP fixture still carried the fiction.** `mcp/fixture_test.go:49` read
`CallOptionsConvention{Type: "CallOption", Fields: []string{"Tx"}}` — no pipeline impact, but it is
how a name that resolves to nothing stays alive after the code that emitted it is fixed, and the
drift guard cannot see a hand-built literal. Now the real shape.

**The MySQL probe test was a probabilistic gate, and making it deterministic also made it assert the
right thing.** The first cut used a 300 ms sleep to make goroutine B the one inside `once.Do`; under
CI load main could win that race, run the probe on tx2's idle connection, and **pass against the
defect**. The fix is to notice that the corrected probe runs on *the querier the client was
constructed with* — so a wrapping querier sees `SELECT VERSION()` and can close a channel on it. That
one channel replaces the sleep as the sequencing edge (tx2 is not opened until the probe has been
observed, so B is necessarily the goroutine that ran it) **and is the property under test** (the
probe must reach the pool rather than the caller's connection). Against
`database.Conn(ctx, c.querier)` the probe is handed the `*Tx`, the wrapper never sees it, and the
test fails on a bounded wait. **Confirmed failing-first** against a surgically reverted generated
tree: 10.03s, with the diagnostic naming the cause rather than a timeout. The test also documents its
one implicit dependency — it needs three live connections (tx1, tx2, the pool probe) and `testDB`
sets no `SetMaxOpenConns`, so a bounded pool would deadlock here rather than fail.

Also: all four `stream_tx_test.go` tests now share one 30s deadline constant. Every one of them
exercises a path that holds or queues on the reservation, where a bug's failure mode is a hang — an
unbounded context would have spent the module's whole `go test -timeout` and reported nothing.

**One pre-existing anchor was noticed and left alone**: `(#29-multi-tenancy)` is used elsewhere in
the PRD but the heading is `## 29. Tenancy`, so the correct anchor is `(#29-tenancy)`. The new text
uses the correct one; repairing the others is doc-sweep work, not 28.3's.

---

## 28.4 Remove FIX-221's fan-out bound

**PRD Reference:** §13.2 (§5.2 step 6, §6.5)

**Status:** **Complete** (landed 2026-09-22) · **Depends on:** 28.2

### Tasks

- [x] Delete the `if database.InTransaction(ctx) { g.SetLimit(1) }` block and its comment block from
      `table/get.go.tmpl:385-403`.
- [x] Regenerate. **29 golden files** carry the bound today: `models_gen.go` in seven trees, seven
      per-table `*_gen.go` files in the `file_per_table` `sqlite` tree, their fourteen `models/`
      counterparts, and `gen/testdata/golden/get_products_relationships_gen.go`. **The real count is
      44** — FIX-221 also wrote two `GetMany` godoc comments describing the bound, gated on
      `SoftDelete` rather than on `HasO2MRelationships`, so fifteen further goldens carry the
      sentence without carrying the code. See the Completion Record.
- [x] Confirm the bound does **not** come back as belt-and-braces (§6.5): keeping it would mean the
      generated tree never exercises the reservation on a multi-edge read inside a transaction.

### Acceptance Criteria

- No `SetLimit` remains in any template or golden.
- The multi-edge terminal re-read inside a nested mutation's own transaction still passes, now
  serialized by the reservation rather than by the bound.
- Unbounded fan-out under `-race` inside a transaction: 0 data races, on all three dialects.

### Tests Required

- [x] `relationship_tx_fanout_test.go` (`graphql`) passes **unchanged** — it must pass both before
      and after, bounded or not. It is the regression gate for FIX-221 and for this removal. Its
      body and every assertion are byte-identical; only the header comment changed, because it
      stated that the loader "now bounds the errgroup", which the removal made false.
- [x] `-race` run of the `graphql` full suite: 0 races.
- [x] Golden regeneration is clean (`expected/` matches `models/` for all twelve trees).

### Completion Record

**Landed 2026-09-22.** Two templates (`gen/templates/table/get.go.tmpl`,
`gen/templates/table/client.go.tmpl`), one codegen unit test (`gen/get_test.go`), **44 regenerated
goldens**, four E2E test files (two new — `mysql`, `sqlite` — and two comment-only corrections in
`graphql`), plus `guidelines/CI.md` and `docs/tracker/IMPLEMENTATION_ORDER.md`. The golden diff is comment
lines and the three deleted bound lines and **nothing else**: no import moved, no code moved, and
`expected/` matches `models/` in every tree.

**The task list predicted 29 goldens and the real number is 44, because FIX-221 made three template
comment edits and this sub-item initially reverted one.** The auto-review caught it. The other two
are consumer-facing godoc on `GetMany` — the implementation in `table/get.go.tmpl:133` and the
interface in `table/client.go.tmpl:17` — both reading *"loaded in parallel via errgroup — one at a
time when ctx carries a transaction"*, which after the deletion said the opposite of both the code
and §13.2 step 4. The third was an inline comment at `get.go.tmpl:335` sitting fifty lines above the
new one that contradicted it. The extra fifteen goldens are the blast radius of the `GetMany`
sentence being gated on `{{ if .SoftDelete }}` rather than on `HasO2MRelationships`, which is
precisely why the 29-file prediction made the miss invisible: the files that carry the *bound* and
the files that carry the *sentence about* the bound are different sets.

**Gates:** `make check` exit 0; `make check-examples` exit 0 across all twelve example trees and
three dialects, under `-race`. Tool preflight matched `.tool-versions` exactly (go 1.27.1,
golangci-lint 2.13.2, gofumpt 0.12.0). `grep -rn SetLimit` over templates and goldens returns
nothing — the only surviving occurrences are inside the test that forbids it.

**The unit test was inverted rather than deleted, and it pins adjacency rather than absence.**
`TestGetTemplate_relationshipFanOutBoundedInTransaction` asserted the bound's exact text; it is now
`TestGetTemplate_relationshipFanOutIsUnbounded`, which asserts no `SetLimit` anywhere *and* that
`errgroup.WithContext(ctx)` is immediately followed by the first `if fo.` — so a transaction-aware
bound spelled some other way is caught too. Decision 5 of the design doc says the bound must not
return as a belt-and-braces; this is what enforces that, and absence of one string would not.

**Acceptance criterion 3 ("0 data races on all three dialects") was not met by the existing suite,
and closing it produced the sub-item's most useful finding.** The multi-edge-inside-a-transaction
read existed only in the `graphql` tree (pgx). `mysql` and `sqlite` had no such test at all, so
removing the bound would have shipped untested on the stdlib adapter. Both trees have `assets` with
three O2M edges plus an M2M, so the shape was constructible in both; each new test reads four edges
inside `WithTx` and asserts **row ids, not counts**, so a mismapped edge cannot pass.

**Three negative controls, measured by widening `connSem` from capacity 1 to 64 — which stops the
reservation serializing while leaving every code path identical.** The three dialects fail in three
different ways, and two of the three are genuinely failing-first:

| Dialect | With the reservation neutered |
|---|---|
| pgx (`graphql`) | **113 data races** + `conn busy`, then the suite hangs to the 5m timeout — FIX-221's exact signature |
| MySQL / stdlib (`mysql`) | **0 data races**, but `tx query: driver: bad connection` and then `stdlib rollback: invalid connection` — the poisoned-transaction pair |
| SQLite / stdlib (`sqlite`) | **passes** |

The MySQL row is the result worth carrying: `database/sql` *does* serialize access to a `Tx`'s
connection, which is why there is no Go-level race — but serialized access is not a reserved
connection. It does not stop a second statement being issued while a result set is open, the MySQL
wire protocol rejects that, and the rollback cannot run on the poisoned connection either. The
design doc's §2 note that "`database/sql` already serialized it internally" holds for 28.2's
runtime-level fan-out and **does not** hold for the generated loader. The SQLite arm is honestly not
failing-first — that driver tolerates the second statement — and its doc comment says so rather than
implying a guarantee it is not demonstrating; it is kept because the tolerance is a property of the
driver, not of §18.5's contract, and the generated code is identical on all three.

**One claim was written, measured, and withdrawn.** The first draft of the MySQL test's comment said
it exercised 28.1's `rows.Next()`-closes-on-exhaustion fix end to end. Reverting that fix and
re-running showed the test still passing: §2.11's stdlib gap is specific to multi-result-set
statements, and the relationship loader's reads are single-result-set. The comment now states what
was measured instead.

**The doc comment reordering is a consequence of the deletion, not a drive-by.** FIX-221's paragraph
had been inserted between "each child read goes through the target's own client" and the tenancy
paragraph's "Both tenancy call options travel with it for that reason", stranding that referent.
Removing the paragraph restored the adjacency it broke, and the replacement text now sits last.

**The auto-review found one FAIL and four stale prose sites; all five were fixed inside the
sub-item.** Beyond the two templates above: `nested_mutations_update_test.go:83` still said FIX-221
"bounded §9.9.6's terminal re-read so it no longer fans out"; `docs/tracker/IMPLEMENTATION_ORDER.md:876` and
`:967` still listed the loader as "bounded to one worker inside a transaction"; and
**`guidelines/CI.md:495` forbade the tests 28.2 had already shipped** — it said the `database/`
engine "does **not** make concurrent *statements* on one transaction safe … so no test here may
assert that it does", which the `connSem` reservation and each adapter's
`reservation_integration_test.go` now contradict outright. That one is 28.2 drift surfaced by 28.4,
and the rule is rewritten rather than deleted: the thing still unsafe, and still un-assertable, is
opening two savepoint **scopes** on one `txCtx` concurrently — the reservation serializes
statements, not scopes.

**One reviewer caveat was already covered:** the report noted that the `make check-examples` exit 0
predated the two new E2E files. A second full run including them had already completed at exit 0
(twelve trees, 0 races, 0 failures) before the review landed, and a third ran after these fixes.

---

## 28.5 `Raw` takes a scan callback

**PRD Reference:** §18.6, §20.2, §29.4.3 (§5.2 step 7, §3.7)

**Status:** **Complete** (landed 2026-09-22) · **Depends on:** 28.0 (independent of 28.1–28.4)

`Client.Raw` is the one generated API that hands a live result set to a consumer — and the API whose
callers are least likely to be thinking about a connection reservation, since the reason to reach
for it is that the generated surface could not express the query. The callback makes the leak
unspellable. The project is unreleased, so the signature change costs nothing.

```go
// before
func (c *Client) Raw(ctx context.Context, rawSQL string, args ...any) (database.Rows, error)
// after — matches database.QueryFunc's existing shape
func (c *Client) Raw(ctx context.Context, rawSQL string, args []any, fn func(database.Rows) error) error
```

### Tasks

- [x] Rewrite `Raw` in `client.go.tmpl:298-313`: the terminal returns
      `(nil, database.QueryFunc(ctx, database.Conn(ctx, c.querier), rawSQL, args, fn))`, so scanning
      happens **inside** the hook chain and the reservation is taken and released within the terminal.
- [x] Keep the `{{- if .TenancyEnabled }}// sqlgen: raw query bypasses tenancy{{- end }}` comment —
      `gen/tenancy_template_test.go:189-234` counts it twice and must keep passing.
- [x] `RawExec` and `Querier(ctx)` are **unchanged** (§3.7): `RawExec` has no result set, and
      handing out the raw `Querier` is what §20.2 specifies as the feature.
- [x] Do **not** add a generic `RawInto[T]` — a real convenience, and not in the PRD.
- [x] Update `gen/unified_client_test.go:184` and regenerate
      `gen/testdata/golden/unified_client_gen.go` plus **24** `client_gen.go` files.
- [x] Record the three consequences in the doc comment or the PRD, per 28.0: a global query hook's
      tracing span now covers the scan; a query hook can no longer intercept a raw query's rows (the
      terminal returns `nil`); and the only `database.Rows` type assertion in the generated tree
      disappears.

### Acceptance Criteria

- No generated code returns `database.Rows` to a consumer.
- Two goroutines both calling `Raw` on one `txCtx` serialize instead of racing.
- Global hooks (tracing, authorization) still fire, and the panic handler still wraps the call.
- `grep -rn "result.(database.Rows)"` returns nothing outside history.

### Tests Required

- [x] Unit (gen): the new signature renders; the tenancy bypass comment still appears twice.
- [x] E2E: `Raw` outside a transaction scans correctly; an `fn` that returns an error propagates it
      unwrapped enough for `errors.Is`; `rows.Err()` is still surfaced.
- [x] E2E (`-race`): **two `Raw` reads fanned out over one `txCtx`** — the case §3.7 exists to make
      safe. 0 races, both results correct.
- [x] E2E: a global query hook sees `Op: "raw_query"` exactly once per `Raw` call.

### Completion Record

**Landed 2026-09-22.** One template (`gen/templates/client.go.tmpl`, +16/−9), one codegen unit test
(`gen/unified_client_test.go`), one new E2E file (`postgres/tests/raw_test.go`, seven tests), and
`docs/PRD.md` (+5/−4) — against **25 regenerated goldens**: `gen/testdata/golden/unified_client_gen.go`
plus exactly the **24** `client_gen.go` the breakdown predicted, every one moving by the identical
+16/−9 and nothing else. `expected/` matches `models/` in all twelve trees.
`grep -rn "result.(database.Rows)"` now hits only the unit test's own negative assertion.

**Files changed:**

| File | What |
|---|---|
| `cmd/sqlgen/gen/templates/client.go.tmpl` | `Raw`'s signature, terminal and doc comment |
| `cmd/sqlgen/gen/unified_client_test.go` | New-signature patterns + absence of the old signature and the type assertion |
| `cmd/sqlgen/gen/testdata/golden/unified_client_gen.go` | Regenerated |
| `cmd/sqlgen/testdata/examples/*/{expected,models}/client_gen.go` | 24 regenerated goldens, twelve trees |
| `cmd/sqlgen/testdata/examples/postgres/tests/raw_test.go` | New — seven E2E tests |
| `cmd/sqlgen/testdata/examples/postgres/tests/stream_test.go` | `seedArticles` cleanup detached from the caller's cancellation (review fix; also repairs 28.3's `stream_tx_test.go` under `-count=2`) |
| `docs/PRD.md` | §18.5 `fn` caveat + table rows; §20.2 fourth consequence |
| `docs/tracker/phase-28.md`, `docs/tracker/STATUS.md` | This record; 28.6 gains the design-doc handoff |

**The terminal is the design doc's §3.7 body verbatim** —
`return nil, database.QueryFunc(ctx, database.Conn(ctx, c.querier), rawSQL, args, fn)` — so the
reservation is taken and released inside the hook chain. The tenancy bypass comment is untouched and
`TestTenancyTemplate_clientRawHasBypassComment` passes unchanged. `RawExec` and `Querier(ctx)` are
unchanged; no `RawInto[T]`.

**The three consequences were already in the PRD (28.0 wrote them); the doc comment states them
where the consumer reads them.** It also names the one misuse the callback does not close — below.

**Two PRD corrections, both overclaims the landed code cannot honour:**

| Site | It said | Now |
|---|---|---|
| §18.5 safe-spellings table, `Raw` and `QueryFunc` rows | "Can the caller break the rule? **No**" | **Only from inside `fn`** |
| §18.5, "`client.Raw` is not on that list" | the callback closes the door | + `fn` *is* the `Stream`-loop shape: a statement it issues on the same `txCtx` before draining waits on the reservation its own rows hold. Same for `QueryFunc` |
| §20.2 consequences list | three | **four** — panic recovery (§21.6) now covers `fn`: a panic while scanning comes back as an error instead of unwinding into the caller, and the rows close on the way out, so the reservation is released either way |

The first is not hypothetical: `TestRaw_StatementInsideFnWaitsOnItsOwnRows` measures it — a
`GetMany` on the `txCtx` from inside `fn`, rows held open, waits the full 250ms inner deadline and
fails with `waiting for the connection held by an open result set: context deadline exceeded`; the
transaction is intact afterwards. The fourth is new behavior, not only a new sentence: before, a
panic in the consumer's scan loop was outside the chain.

**Tests** (`postgres/tests/raw_test.go`, all under `-race`):

| Test | Pins |
|---|---|
| `TestRaw_ScansOutsideTransaction` | `[]any` args bind; every row reaches `fn` |
| `TestRaw_CallbackErrorIsReturnedAsIs` | `fn`'s error returns **unwrapped** (`errors.Is` and identical message), in and out of a tx; an early return with rows unread still frees the connection |
| `TestRaw_SurfacesRowsErr` | a mid-iteration division-by-zero reaches the caller as `*pgconn.PgError` 22012 even though `fn` never calls `rows.Err()`; the two rows before it were delivered |
| `TestRaw_ConcurrentReadsOnOneTxCtxSerialize` | two `Raw` reads fanned out on one `txCtx`; the first holds its set open until the second is issued, and checks on every row that the second has not started scanning |
| `TestRaw_StatementInsideFnWaitsOnItsOwnRows` | the self-wait above |
| `TestRaw_PanicInFnIsRecoveredAndReleasesTheConnection` | panic in `fn` → `sqlgen: panic in raw_query …`; the next statement on the `txCtx` runs |
| `TestRaw_GlobalQueryHookSeesEachCallOnce` | `Op: "raw_query"` exactly once per call, in and out of a tx, with `args` as `Input`; the hook's span brackets the scan; the terminal hands the hook `nil` |

**Negative control.** With `acquireConn`/`releaseConn` stubbed to no-ops (restored byte-identical
afterwards), the fan-out test fails with a **data race** plus
`pgx commit: failed to deallocate cached statement(s): conn busy`, and the self-wait test fails with
`tx query: conn busy`. No test here is failing-first against the *old* `Raw` — that one does not
compile against the new signature, which is the point of the change — so the control is against the
reservation `Raw` now routes through.

**Scope note.** The E2E tests live in `postgres` only. The template is dialect-independent and the
reservation's per-dialect behavior is already gated by 28.2's adapter probes and 28.4's
`mysql`/`sqlite` fan-out tests; pgx is the adapter on which a `Raw` race is loud.

**Verification.** `make check` exit 0 (0 lint issues across all eight modules). `make check-examples`
exit 0 — twelve trees, three dialects, `-race`, 0 lint issues, 0 data races. Tool preflight matched
`.tool-versions` exactly (go 1.27.1, golangci-lint 2.13.2, gofumpt 0.12.0). Neither
`/security-review` trigger fires: no `//go:embed` runtime package and nothing under
`cmd/sqlgen/cli/**` changed.

**Left for 28.6** (now on its task list): `docs/design/archive/TX_CONNECTION_LOCK.md:718` carries the same
"**No**" in its copy of the safe-spellings table; 28.6 marks that doc SYNCED and superseded by the
PRD, so it was not edited here.

**The auto-review passed PRD compliance 6/6, independently confirmed both PRD corrections against
`reservation.go`, `QueryFunc` and `recoverQuery`, and found one real defect and three nits — all
fixed inside the sub-item:**

| Finding | Fix |
|---|---|
| **`seedArticles` cleanup never ran.** It registered `HardDelete` on the caller's ctx, and every caller passes a bounded ctx whose deferred `cancel` fires *before* `t.Cleanup`, so every delete failed silently. Harmless on one run; under `-count=2` **six `raw_test.go` tests and three of 28.3's `stream_tx_test.go` tests failed** (confirmed before fixing — 28.3 shipped the same defect) | `context.WithoutCancel(ctx)` in the helper (`stream_test.go`), which fixes every caller at once; `-count=2` now green |
| §18.5's new sentence named `QueryFunc` but not `QueryRowFunc`, whose `fn` hits the same wait before `Scan` | Names both |
| Doc comment: "returned as is" is true of `Raw` but a global hook may still wrap it | "Raw does not wrap an error returned by fn, though a global hook may" |
| Doc comment: the "do not issue another statement from inside fn" warning read as unconditional; outside a transaction the statement just draws another pooled connection | Scoped to "When ctx carries a transaction" |

The two doc-comment fixes moved the same 25 goldens again; regenerated, not hand-edited.

---

## 28.6 Documentation sweep

**PRD Reference:** §9.4a, §18.5, §18.6, §20.2, §20.5 (§5.2 step 8, §4.5)

**Status:** **Complete** (landed 2026-09-22) · **Depends on:** 28.2, 28.3, 28.4, 28.5

28.0 wrote what *would* be true; this closes the loop on what *is* true, and covers the surfaces
28.0 could not (generated doc comments, which move with their templates).

### Tasks

- [x] Re-read 28.0's PRD edits against the landed code and correct any drift — particularly the
      `Begin` behavior and the `AllowInTransaction` emission condition, both of which 28.1–28.3
      could have moved. **Both held**; six other sites had drifted — see the completion record.
- [x] `Querier(ctx)`'s generated doc comment (`client.go.tmpl:288-292`) — state the drain-or-close
      rule and the changed symptom where the consumer reads it.
- [x] `database.QueryFunc` / `QueryRowFunc` doc comments — the safe spelling for `Querier(ctx)`
      users (§3.8), with the note that `QueryRowFunc` now releases after `fn` returns either way.
- [x] Re-read the `Tx` doc comment written in 28.2 now that all the pieces have landed.
- [x] Mark `docs/design/archive/TX_CONNECTION_LOCK.md` **SYNCED** and name the PRD sections that supersede it, per
      the convention Phases 21–26 used for their design docs.
- [x] **Carried from 28.5.** `docs/design/archive/TX_CONNECTION_LOCK.md:718`'s safe-spellings table still answers
      "Can the caller break the rule?" with **No** for `Raw`; 28.5 corrected the PRD's copy to
      **Only from inside `fn`** (a statement issued on the `txCtx` from inside `fn` before draining
      waits on `fn`'s own rows). Either correct the design doc's row or let the SYNCED banner
      supersede it — decide explicitly. **Decided: the banner supersedes it; the body is not
      edited.** See the completion record.
- [x] Remove §18.5's closing paragraph pointing at the design doc as "a draft, not a commitment".
      **Already gone** — 28.0's rewrite (`69f5cc0`) deleted it; no PRD line references the design
      doc any more. Nothing left to remove.
- [x] **Carried from 28.4's auto-review — a generated-doc structure defect, not a 28.4 regression.**
      `GetMany`'s relationship-loading sentence is emitted under `{{ if .SoftDelete }}` in both
      `table/get.go.tmpl:131` and `table/client.go.tmpl:15`, so a table *without* soft delete gets
      no relationship-loading doc at all while a soft-deleted one documents it inside a paragraph
      about soft delete. The gate should be `HasO2MRelationships` (or the relationship sentence
      should be its own conditional). 28.4 corrected the sentence's *content* at both sites and
      deliberately left the gating alone — it moves goldens for a reason unrelated to the
      reservation. Expect the blast radius to differ from the file set that carries the sentence
      today.

### Acceptance Criteria

- [x] The drain-or-close rule reads identically in §18.5, §20.2, §9.4a and the generated doc comments.
- [x] No PRD section still says concurrent use of a `txCtx` is undefined behavior.
- [x] The design doc's status line names its superseding PRD sections.

### Tests Required

- [x] Docs-only. Gate: no dangling intra-document anchors in `docs/PRD.md`; the twelve example trees
      regenerate byte-identically (doc-comment edits in templates move goldens — regenerate, do not
      hand-edit).
- [x] **Added:** `TestGetManyDoc_sentencesGatedOnTheirOwnFeature` — the gating fix is a template
      logic change, not a wording change, so it gets a failing-first gate.
- [x] **Added by the review round:** `TestClientTemplate_streamInterfaceDocCarriesTheRule` — the
      interface doc is the one a consumer reads, and nothing gated it.

### Completion Record

**Landed 2026-09-22.** Four templates (`client.go.tmpl`, `table/client.go.tmpl`,
`table/get.go.tmpl`, `table/stream.go.tmpl`), the runtime's doc comments (`database/transaction.go`,
`database/reservation.go`), both adapters' reservation-test headers, two new codegen tests
(`gen/get_test.go`, `gen/client_test.go`), `docs/PRD.md`, `docs/design/archive/TX_CONNECTION_LOCK.md` and `docs/tracker/IMPLEMENTATION_ORDER.md`
— against **87 regenerated goldens**, every changed line a comment, `expected/` matching `models/` in
all twelve trees. No code behavior moved.

**Files changed:**

| File | What |
|---|---|
| `cmd/sqlgen/gen/templates/client.go.tmpl` | `Querier(ctx)` doc states the rule and the changed symptom; `Raw` doc reworded to the rule |
| `cmd/sqlgen/gen/templates/table/stream.go.tmpl` | `Stream` implementation doc names the rule the opt-in applies |
| `cmd/sqlgen/gen/templates/table/client.go.tmpl` | `Stream` interface doc (review round); `GetMany` interface doc re-gated on `HasO2MRelationships` |
| `cmd/sqlgen/gen/templates/table/get.go.tmpl` | `GetMany` implementation doc re-gated on `HasO2MRelationships` |
| `cmd/sqlgen/gen/get_test.go`, `cmd/sqlgen/gen/client_test.go` | `TestGetManyDoc_sentencesGatedOnTheirOwnFeature` (failing-first), `TestClientTemplate_streamInterfaceDocCarriesTheRule` |
| `cmd/sqlgen/gen/testdata/golden/*` (5) + `cmd/sqlgen/testdata/examples/*/{expected,models}/*` (82) | Regenerated, comment-only |
| `database/transaction.go` | `Tx`, `FromContext`, `QueryFunc`, `QueryRowFunc` docs; `Commit`'s savepoint comment |
| `database/reservation.go` | `acquireForTeardown` doc; tracker ID and "today" removed |
| `database/pgx/reservation_integration_test.go`, `database/stdlib/reservation_integration_test.go` | Headers corrected for the MySQL measurement |
| `docs/PRD.md` | §9.4, §9.4a, §9.8.3, §18.5, §20.2, §20.5 |
| `docs/design/archive/TX_CONNECTION_LOCK.md` | SYNCED banner with the eleven superseded claims |
| `docs/tracker/IMPLEMENTATION_ORDER.md` | Phase 28 entry: design doc SYNCED, normative spec named |
| `docs/tracker/phase-28.md`, `docs/tracker/STATUS.md` | This record; Phase 28 at 7/8 |

**The two decisions the task list singled out both held.** `Tx.Begin` takes the blocking
`acquireConn` (§18.5 "Savepoints"), and `AllowInTransaction` is appended unconditionally in
`sharedTypeDefinitions`, in the position §9.6 lists (§9.4a). The drift was elsewhere.

**The one substantive correction was measured, not reasoned about: MySQL does not tolerate a second
statement over an open result set.** §18.5 said the `database/sql` adapters "(MySQL, SQLite) … tolerate
the shape by accident", and its failure table said "works by accident (stdlib)" twice. 28.4's
negative control had already contradicted that for the cross-goroutine loader shape; the
same-goroutine case had never been measured. A throwaway probe (deleted afterwards) against the
stdlib harness's `mysql:8.0` and `modernc.org/sqlite`, on a bare `database/sql` transaction with no
reservation — part-read a three-row set, then issue a second statement:

| | same goroutine | another goroutine |
|---|---|---|
| MySQL | `busy buffer`; `rows.Close` → `commands out of sync`; rollback → `invalid connection` | identical |
| SQLite | `nil` / `nil` / `nil` | `nil` / `nil` / `nil` |

So `database/sql` serializes **calls**, which is why there is no Go-level race on either, but a
serialized call is not a reserved connection: MySQL rejects the overlap and poisons the transaction
exactly as pgx does. Only SQLite tolerates it. Corrected in §18.5 (opening paragraph, both table
rows, and "a misuse that pgx **and MySQL** report immediately"), §20.2 (the changed symptom names
both drivers), the `Tx` doc comment, and both adapters' `reservation_integration_test.go` headers.
The stdlib fan-out test's "not failing-first because `database/sql` serialized it internally" was
the same claim; it now says what the probe supports — its one-to-three-row reads did not happen to
overlap an open set, and the generated four-edge loader's do (28.4).

**Five more PRD sites had drifted from what landed:**

| Site | It said | Now |
|---|---|---|
| §20.5, the drain-or-close bullet | "reachable **only** through `Querier(ctx)`, `FromContext`, or writing inside a `Stream` loop" | also from inside the `fn` of `Raw` / `QueryFunc` (28.5 measured it), and the `Stream` door only with `AllowInTransaction` set (28.3) |
| §18.5, the `Stream` door | "Writing inside a `Stream` loop" | + open only to a caller who sets `AllowInTransaction` |
| §9.4a, *Implementation pattern* | no guard | the guard, where the landed template has it — between the `SkipCache` force and `executeQuery` |
| §20.2, `Querier(ctx)` | `QueryFunc` as the leak-proof spelling; symptom "the immediate `conn busy`" | + `QueryRowFunc`; symptom names `busy buffer` too |
| §18.5 failure table's first-row note | "a misuse that pgx reports immediately" | pgx and MySQL |

**One claim 28.0 carried forward unverified was checked and holds.** §18.5's two-locks argument
cites generated mutations that "read a `RETURNING` set and then re-read the affected rows inside it
(§9.8)". They do: `table/update.go.tmpl:856` defers `rows.Close()` on an `UPDATE … RETURNING` set and
`:895` issues `c.GetMany` before the function returns, and `delete.go.tmpl` has eight more — which is
precisely why a close-scoped lock would self-deadlock and the drain-scoped reservation does not.

**The rule now reads identically on every generated surface that states it.** `Querier(ctx)` (new),
`Raw` (reworded from "do not issue another statement on it from inside fn" to the rule's own words),
and `Stream` — whose doc comment had the same gap 28.0's review found in §9.4a: it named the
opt-in's obligation ("issues nothing else on that txCtx") and never said that obligation *is* the
rule applied to a loop. `Tx`, `QueryFunc`, `QueryRowFunc` and `FromContext` carry it on the runtime
side. `QueryFunc` and `QueryRowFunc` also carry 28.5's caveat, which neither doc comment had: the
callback closes the leak, not the rule — a statement `fn` issues on the transaction before draining
(or before `Scan`) waits on its own reservation.

**Two runtime comments still carried §18.5's pre-28.2 premise.** `acquireForTeardown` and `Commit`'s
savepoint path justified blocking at savepoint depth with "a savepoint statement runs while the
enclosing fn is still live" / "the root only, where fn has returned" — the premise 28.2's review
proved false for `WithTransaction`'s own unwind. §18.5 was corrected in 28.2; these two copies were
not. Both now give the reason §18.5 does (`Commit` cannot tell that unwind from a consumer's racing
`Commit`) and name the cost. `reservation.go` also lost a tracker ID in shipped code
("what 28.1 established") and a time-relative "today". The `Tx` doc comment gained the one
consequence it lacked — a nested scope's own `RELEASE SAVEPOINT` after a leak waits on itself.

**`GetMany`'s doc is gated on what it describes.** The relationship sentence now sits under
`{{- if .HasO2MRelationships }}` in both templates, and says what the loader's inline comment says
(in parallel over the pool, serialized by the reservation inside a transaction) instead of "in
parallel … inside a transaction and out". The blast radius did differ, as predicted: across the
moved goldens the sentence went from **72 occurrences to 145**. The `graphql` tree's `assets`,
`categories`, `documents` and `workspace_notes` — relationships, no soft delete — are documented for
the first time, and soft-deleted tables with no fan-out (`postgres`'s `articles` and `tags`, among
others) lost a sentence about one they do not have.
`TestGetManyDoc_sentencesGatedOnTheirOwnFeature` covers soft delete × relationships over both the
implementation and the interface, and is **failing-first**: against the old gate, "soft delete only"
carries the sentence and "relationships only" does not, in both templates.

**The design-doc row: the banner supersedes it, and the body is not edited.** Correcting one row would
make a partly-synced document look fully current — and the row is not the only claim execution moved.
The banner lists eleven, each with what supersedes it — the PRD for nine, the code for the two that
are implementation detail rather than contract: §3.4's blanket non-blocking acquire (depth sets the
mode), §5.2 step 1's "no longer a precondition" (it was a hard one), the unaddressed
`IsClosed`-during-teardown window, §2.2/§3.3/§4.2's `database/sql` tolerance (the MySQL probe
above), §3.8's "No" (the carried row), §3.7's three consequences (four), §3.3's
`releaseConn`-only-via-`OnceFunc`, §4.6/§5.2's "drop `Stream` from the doors list" (it stays — the
opt-in reopens it), the concurrent-scope carve-out, and §6.2's emission question (decided); plus a
pointer to the breakdown's cost-estimate corrections. This follows `UUID_MIGRATION.md`'s
"superseded by the PRD, not by a later section of this file" precedent; the SYNCED line itself
follows `PG_MATERIALIZED_VIEW.md`'s wording from Phase 21. `IMPLEMENTATION_ORDER.md`'s Phase 28 entry
no longer calls the doc DRAFT or says "Normative spec: none yet".

**Golden movement, by cause** (overlapping; union 87, no file unexplained): **25** for `Querier` +
`Raw` — the unit golden plus exactly the 24 `client_gen.go`, 28.5's set; **58** for `Stream` — every
generated `Stream`, **222 of 222** implementations and **222 of 222** interface methods now carrying
the rule, 28.3's guard count; **44** for `GetMany`.

**The anchor gate passes, and 28.0's "ten dangling anchors" were never dangling.** 653 intra-document
refs against 498 headings resolve with GitHub's slug rules, here and at HEAD. The ten 28.0 counted
are exactly the ten distinct double-hyphen anchors (`#21-hooks--middleware`,
`#2653-filter--sort--pagination-translation`, …) produced by `&` / em-dash headings, which GitHub
slugs with the double hyphen intact — a checker that collapses it reports them falsely. 28.3's
`#29-multi-tenancy` no longer occurs anywhere. All eleven PRD links in the new banner resolve.

**Not acted on — observed while checking §18.6:** `cache.go.tmpl:1002` still registers two
`OnCommit` callbacks behind one `IsClosed()`, the split-registration window §18.6 names (28.2). It
is documented there as a residual, it is code rather than documentation, and it is out of 28.6's
scope.

**The auto-review passed 4/5 PRD rows and found one PARTIAL plus a set of doc drifts; all were fixed
inside the sub-item except one it suggested leaving.**

| Finding | Fix |
|---|---|
| **PARTIAL on the acceptance criterion:** `Stream`'s *interface* doc (`table/client.go.tmpl`) carried neither the refusal nor the rule — and `client.Products()` returns the interface, so that is the doc a consumer reads. The `GetMany` fix had treated the interface as its own surface; `Stream`'s had not been | The interface doc states the refusal, the opt-in and the rule in its own words. `TestClientTemplate_streamInterfaceDocCarriesTheRule` gates it. Same 58 goldens, no new files |
| §18.5 failure table rows 3–4 and the Savepoints paragraph stated pgx's `conn busy` as if it held on every dialect | **Measured** rather than inferred: `TestLeakedResultSetDoesNotStrandTheConnection` logs the driver's answer, and a root `Commit` / `Rollback` over a leaked set returns `nil` on both MySQL and SQLite. Row 3 now reads `conn busy` (pgx) / succeeds (MySQL, SQLite); row 4 carries the per-dialect "before" the earlier probe established (`RELEASE SAVEPOINT` is an `Exec` over an open set); the Savepoints try-acquire names both drivers' busy errors |
| The "honest regression" sentence named pgx and MySQL but not SQLite, where code that used to work now waits | Says so |
| `pgx/reservation_integration_test.go`'s "which is why the bounded-wait probes live here" no longer followed once MySQL also rejects the shape | Gives the real reason: the wait belongs to `database.Tx`, so one adapter covers every dialect |
| Banner: the §5.2-step-1 bullet cited no PRD section; §4.2's table was missing from the MySQL bullet; a ninth claim (§4.6 item 3 / §5.2 step 8 dropping `Stream` from the doors list) and a tenth (§3.3's `releaseConn` path) were missing; "the four-edge read *does*" overstated one timing-dependent run | All corrected; the two implementation-detail bullets say so and point at the code; the breakdown's cost-estimate corrections are pointed at |
| §9.4 / §9.8.3 sample doc comments still printed the pre-28.6 `GetMany` sentence | The template's current wording |
| §18.5's two-locks field list omitted `tearingDown`; §20.5's bullet omitted `QueryRowFunc`'s `fn`-before-`Scan` | Both added |
| `docs/tracker/fixes.md` FIX-221 still says the stdlib adapters tolerate the shape | **Left**, on the reviewer's own suggestion — a resolved FIX is a historical record of what was believed then |

**Verification, re-run after the review fixes.** Tool preflight matched `.tool-versions` (go 1.27.1,
golangci-lint 2.13.2, gofumpt 0.12.0). `make check` exit 0, 0 lint issues across all eight modules.
`make check-examples` exit 0 — twelve trees, three dialects, `-race`, 0 lint issues, 0 data races, 0
failures. The PRD anchor gate re-ran clean (653 / 498 / 0). Neither
`/security-review` trigger fires: no `//go:embed` runtime package, nothing under
`cmd/sqlgen/cli/**`.

---

## 28.7 Closure sweep

**PRD Reference:** all of the above

**Status:** **Complete** (closed 2026-09-22) · **Depends on:** 28.0–28.6

### Tasks

- [x] `/close-phase 28` — verify every sub-item Complete, triage open FIXes by severity, run the
      full test sweep across all three dialects with `-race`, freeze versioned artifacts, update
      `STATUS.md` and the sibling design doc.

### Acceptance Criteria

- All twelve example suites pass under `-race` on all three dialects.
- `./database/...` passes under `-race`.
- No open `blocking` FIX filed against this phase.

### Tests Required

- [x] Full sweep, integration included (`SQLGEN_INTEGRATION=true`).

### Completion Record

**Closed 2026-09-22.** Tracker bookkeeping only — no code, no golden movement, working tree clean
after the sweep's `fmt` / `fmt-examples` passes.

**Sub-items:** 28.0–28.6 all **Complete**, every task checkbox ticked, every Completion Record
filled.

**FIX triage:** the Open section is empty (`tracker-check`: 0 open, 115 resolved, markers intact),
and **no FIX entry was filed against Phase 28** — every review finding across the seven sub-items
was fixed inside its own sub-item. Nothing to resolve, nothing to carry forward.

**Sweep** (sequential, `SQLGEN_INTEGRATION=true` exported for the whole run, tool preflight
matched `.tool-versions` exactly — go 1.27.1, golangci-lint 2.13.2, gofumpt 0.12.0):

| Leg | Exit | Wall | Longest legs |
|---|---|---|---|
| `make check` | 0 | 1m24s | `cmd/sqlgen/gen` ~54.1s, `cmd/sqlgen/cli` ~40.0s — 0 lint issues across all eight modules |
| `make check-examples` | 0 | 2m05s | `examples/graphql/tests` ~45.6s, `examples/mysql/tests` ~18.0s — all twelve trees, 0 lint issues |
| `make test-integration` | 0 | 1m40s | `cmd/sqlgen` ~58.5s, `cmd/sqlgen/gen` ~56.8s, `cmd/sqlgen/cli` ~43.6s |

No `FAIL`, `DATA RACE` or `panic:` line in any leg; no flakes, no reruns.

**Acceptance criteria, each against the sweep rather than asserted:**

- *All twelve example suites under `-race` on all three dialects* — `make check-examples` ran
  `go test -race` in all twelve trees (postgres/pgx, MySQL 8 and SQLite containers), including
  28.3's `stream_tx_test.go`, 28.4's `mysql` / `sqlite` multi-edge-inside-a-transaction reads and
  28.5's `raw_test.go`.
- *`./database/...` under `-race`* — green in both legs. In `test-integration` (no `-short`)
  `database/pgx` took ~10.0s and `database/stdlib` ~8.7s against ~1.9s and ~1.6s under `-short`,
  which is the two adapters' `reservation_integration_test.go` container probes actually running
  rather than skipping.
- *No open `blocking` FIX against this phase* — none open at all.

**One precision about the Tests Required line.** `SQLGEN_INTEGRATION` is read by no Go code in the
repo — it appears only in the PRD's CI sketch (§8.1.7) and `IMPLEMENTATION_ORDER.md`. What actually
enables the integration tests is `make test-integration` omitting `-short`, since they gate on
`testing.Short()`. The variable was exported anyway; the timings above are the evidence the
integration tests ran.

**Sibling design doc:** `docs/design/archive/TX_CONNECTION_LOCK.md` already carries **SYNCED — superseded by PRD
§18.5 (2026-09-22)**, flipped by 28.6. **Frozen artifacts:** none — Phase 28 specifies no versioned
contract. 28.3 changed the *values* of the manifest's `conventions.call_options.fields`, not the
manifest schema's shape; its `schema_version` stays on the Phase 18 deferred track.

**Release-notes gap, flagged and not fixed.** Three consumer-visible changes landed and **no
Phase 28 commit carries a `!` or a `BREAKING CHANGE:` footer**, so Release Please would render
them as ordinary features and refactors: `Raw`'s signature change (28.5, `25491a9` — a compile
break for every caller), `Stream` refusing a transaction without `AllowInTransaction` (28.3,
`5dce764` — a runtime error where the call used to proceed), and §4.2's honest regression (28.2,
`23440df` — a part-drained result set followed by another statement on the same transaction now
waits, unbounded by default, where pgx and MySQL used to fail at once). Same situation as Phase
25's closure: the repository has no configured remote and Release Please has consumed nothing, so
it is still closable by amending, which is a history rewrite and therefore the user's call.

---

## Dependency order

```
28.0 (PRD sync) ─┬─→ 28.1 ──→ 28.2 ─┬─→ 28.3 ─┐
                 │                  ├─→ 28.4 ─┤
                 └────────→ 28.5 ───┴─────────┴─→ 28.6 ──→ 28.7
```

**28.5 is reviewable on its own** once 28.0 lands — it needs none of 28.1–28.4.
**Only 28.3, 28.4 and 28.5 change generated output**; 28.1 and 28.2 are runtime-module only.

## Out of scope

- **Goroutine-id self-deadlock detection** (§6.1, decided 2026-09-21) — measured at ~1.8µs per
  transactional read plus a third mutex and two `Tx` fields, and `runtime.Stack` can misattribute
  after an id is reused. Reversible if the misuse shows up; §4.2 notes the cheaper half-measure
  (`atomic.Pointer[string]` naming the in-flight statement, no mutex, no traceback).
- **`QueryFunc` adoption at the generated call sites** (§6.4) — 82 sites across 14 templates and
  twelve golden trees, buying nothing once the reservation is in the methods. Optional hygiene, to
  be picked up per-template if one is ever open for another reason.
- **`ExecFunc`** (§3.8) — would be a pass-through whose existence implies `Exec` is unsafe without
  it, which is false.
- **Constraining `Querier(ctx)` / `database.FromContext`** (§6.3) — covered by the reservation,
  deliberately unconstrained in what a caller may do with the rows.
- **Pipelining / `SendBatch`** (§6.7) — the only thing that actually reduces latency inside a
  transaction, strictly larger than this phase, and orthogonal: the lock is about safety, batching
  about speed.
- **Making `Stream` genuinely transaction-safe** (§4.6) — five approaches considered, none survives
  without breaking `Stream`'s memory bound, §9.4a's no-`Limit` contract, cross-dialect portability,
  or transaction semantics.
