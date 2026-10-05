# Transaction Connection Lock — Migration Design

> **Status: ARCHIVED (2026-10-04) — superseded by PRD §18.5** (synced 2026-09-22).
>
> **Do not read this file for current behavior.** The cited PRD sections are normative and
> supersede it on any conflict — see [PRD §18.5](../../PRD.md#185-concurrent-safety). Archived
> because Phase 28 is closed and nothing in the code or the PRD references this document; it is
> kept for the record described below, not as a reference.
>
> **Original status note follows.**
>
> **Status: SYNCED — superseded by PRD §18.5 (2026-09-22).** The normative contract now lives in
> `docs/PRD.md` [§18.5](../../PRD.md#185-concurrent-safety) — the reservation, the one caller rule, the
> doors that stay open, the safe spellings, the failure-mode table, root teardown, savepoints — plus
> [§18.2](../../PRD.md#182-transaction-options) (`Timeout` is what bounds a wait),
> [§18.4](../../PRD.md#184-txisclosed-and-safe-fallback) and [§18.6](../../PRD.md#186-deferred-side-effects)
> (what `IsClosed` reports while a root teardown is in flight),
> [§20.2](../../PRD.md#202-generated-structure) (`Raw`'s scan callback, `Querier(ctx)`),
> [§20.5](../../PRD.md#205-concurrency-safety) (the restored `errgroup` example),
> [§9.4a](../../PRD.md#94a-streaming-operations) and [§9.6](../../PRD.md#96-per-call-options) (`Stream`'s
> guard, `CallOptions.AllowInTransaction`), [§13.2](../../PRD.md#132-loading-strategy) step 4 (the
> unbounded fan-out), [§19.1](../../PRD.md#191-core-package-database) (`QueryFunc` / `QueryRowFunc`) and
> [§29.4.3](../../PRD.md#2943-raw-operations). Implemented as Phase 28 (28.0–28.6). This document remains
> the design supplement — the measurement record (§2), the placement argument (§4.4), why `Stream`
> cannot be made transaction-safe (§4.6), and the decision record (§6). Where wording differs, the
> PRD wins.
>
> **Superseded claims.** The body below is the design as drafted and is deliberately **not** edited
> to match what landed. These are the places execution moved it, each superseded by the PRD — or,
> for the two that are implementation detail rather than contract, by the code — and not by a later
> section of this file:
>
> - **§3.4 — `Begin`, `Commit` and `Rollback` all take the non-blocking teardown acquire.** Depth,
>   not verb, sets the acquire mode: savepoint statements **block**, and only a *root* `Commit` /
>   `Rollback` proceeds without the reservation. A try-acquire at savepoint depth would fail every
>   `…WithRelated` mutation's savepoint with the driver's busy error (`conn busy` on pgx,
>   `busy buffer` on MySQL) against a concurrent read. The cost this
>   document did not carry: a nested scope that leaks a result set and then unwinds waits on itself,
>   bounded only by `ctx`. PRD §18.5, "Savepoints".
> - **§5.2 step 1 — untangling `tx.mu` is "no longer a precondition".** It was a hard one: once
>   savepoint statements block on the reservation, holding `tx.mu` across one inverts the lock order
>   against every read that reaches `IsClosed()` through `database.Conn`. Releasing the mutex also
>   removed two guarantees it had been giving incidentally — at-most-once root teardown, and a
>   savepoint unwind that cannot index an emptied stack — and both had to be replaced.
>   Implementation detail, not contract: the lock-order invariant in `database/reservation.go` and
>   the `Tx` doc comment.
> - **What `IsClosed` reports during a root teardown** — not addressed here. `closed` means the
>   teardown *completed*; a separate claim is what makes it at-most-once; a statement issued during
>   the window is rejected; the `OnCommit` list is snapshotted *after* the driver call. PRD §18.4,
>   §18.5, §18.6.
> - **§2.2, §3.3 and §4.2's table — the `database/sql` adapters tolerate the shape; MySQL
>   "passes".** True of SQLite only. MySQL rejects a statement issued while a result set is still
>   open with `busy buffer`, and the rollback then fails with `invalid connection`, on one goroutine
>   or two (re-measured 2026-09-22). §2.2's two-edge probe passed because its reads did not happen to
>   overlap an open result set; the generated loader's four-edge read did, in 28.4's negative
>   control. A root teardown over a leaked set, by contrast, succeeds on both stdlib dialects — the
>   `conn busy` in §4.2's third row is pgx's. PRD §18.5.
> - **§3.8 — "Can the caller break the rule? No" for `Raw` and `QueryFunc`.** *Only from inside
>   `fn`*: `fn` runs inside an open result set, so a statement it issues on the same `txCtx` before
>   draining waits on its own rows. PRD §18.5.
> - **§3.7 — three consequences of `Raw`'s callback.** Four: panic recovery now covers `fn`. PRD
>   §20.2.
> - **§3.3 — `releaseConn` is reached only through a `sync.OnceFunc`.** Also through `Exec`'s and
>   the savepoint statements' `defer`, which pair it 1:1 with their own acquire. Implementation
>   detail: `database/reservation.go`.
> - **§4.6 item 3 and §5.2 step 8 — §18.5 names `Stream` as refused rather than as a door left
>   open.** It stays on the list: the refusal closes the door by default, but `AllowInTransaction`
>   reopens it, so writing inside an opted-in `Stream` loop is still one of the ways a caller can
>   break the rule. PRD §18.5, §9.4a.
> - **Concurrent savepoint scopes** are not made safe — the reservation serializes statements, not
>   scopes. Not in this document. PRD §18.5.
> - **§6.2 — where `AllowInTransaction` is emitted** is decided: unconditionally, like `LockMode`,
>   and carried in the manifest's `call_options.fields`. PRD §9.4a, §9.6.
>
> The cost estimates (eleven example trees rather than twelve, `Raw`'s signature printed three times
> rather than four, `AllowInTransaction`'s golden blast radius) were corrected when the phase was
> broken down; see `docs/tracker/phase-28.md`, "Verification notes".
>
> **Original status note follows.**
>
> **DRAFT (revised 2026-09-19).** Design, not a decision. Every claim below carries either a
> `file:line` from this repo or a measurement taken on 2026-09-18/19 against `postgres:16-alpine`,
> `mysql:8.0` and `modernc.org/sqlite` through the example trees under
> `cmd/sqlgen/testdata/examples/`. Nothing here is inferred from how a transaction *ought* to behave.
>
> **What changed in this revision.** The first draft proposed holding a connection lock *for the
> lifetime of each result set*, released at `Rows.Close()`, and concluded that 47 query sites across
> 14 templates had to convert to a callback form in one all-or-nothing change because an
> un-converted site would **deadlock**. That conclusion was correct *for that lock scope*, and the
> lock scope was wrong. The connection's busy window ends when the result set is **drained**, not
> when our `Close()` runs — both drivers close the result set themselves the moment `Next` reports
> exhaustion. Scoping the lock to the driver's real busy window gets the same safety with **zero
> call-site conversions**. Measured both ways, same tree, same day: §2.5 and §2.6.
>
> **What this proposes now.** Move the obligation "one statement at a time per transaction" from the
> *caller* to the `Tx` itself, with a second lock held from `Query` until the result set is drained
> or closed, whichever comes first. The lock lives on the `Tx`, so it exists only inside a
> transaction: outside one there is no lock, no contention, and the relationship fan-out keeps
> running fully in parallel over separate pooled connections. That asymmetry is the point.
>
> **One signature changes.** `Client.Raw` moves from returning `database.Rows` to taking a scan
> callback (§3.7), so the one generated API that handed a live result set to consumers can no longer
> leak the connection reservation. `RawExec` and `Querier(ctx)` are untouched.
>
> **The recommendation**, in one line: put the reservation in `Tx.Query`/`Exec`/`QueryRow`, let
> `Stream` refuse a transaction unless the caller opts in, change `Raw` to take a scan callback, and
> defer the `QueryFunc` call-site adoption indefinitely. §5 states why in full.
>
> **Prerequisite, already landed.** FIX-221 bounded the relationship loader's `errgroup` to one
> worker inside a transaction ([PRD §13.2 step 4](../../PRD.md), [§18.5](../../PRD.md)). That fix stands on its
> own and is what makes this document optional rather than urgent. When the lock lands, the bound
> becomes redundant and should be removed in the same change — see §5.

## Contents

1. [Why this document](#1-why-this-document)
2. [What was measured](#2-what-was-measured)
3. [The design](#3-the-design)
4. [What it costs](#4-what-it-costs)
5. [Recommendation and sequencing](#5-recommendation-and-sequencing)
6. [Open questions](#6-open-questions)

---

## 1. Why this document

FIX-221 stopped **generated** code from putting two statements on one transaction's connection. It
did nothing for **consumer** code. A consumer who writes this still gets a data race:

```go
database.WithTransaction(ctx, db, "load", func(txCtx context.Context) error {
    g, _ := errgroup.WithContext(txCtx)
    g.Go(func() error { _, err := client.Orders().GetMany(txCtx, …); return err })
    g.Go(func() error { _, err := client.Events().GetMany(txCtx, …); return err })
    return g.Wait()
})
```

Until FIX-221, [PRD §20.5](../../PRD.md) printed a near-identical snippet and labelled it safe, on the
strength of `Tx`'s `sync.Mutex`. That claim was wrong and is now corrected — §18.5 says the caller
must serialize. This document asks whether the *original* promise can be delivered instead of
withdrawn.

It can, and the price is far lower than the first draft estimated.

---

## 2. What was measured

### 2.1 The hazard is real and it is a data race

Three relationships selected on `users`, inside `WithTx`, pgx:

```
rollback failed: rolling back transaction probe_three: pgx rollback: failed to deallocate
cached statement(s): conn closed (original error: load user categories junction:
tx query probe_three: tx query: conn busy)
```

Under `-race`, the same call reports a genuine data race inside the driver —
`pgx/internal/stmtcache.(*LRUCache).RemoveInvalidated` via `Conn.deallocateInvalidatedCachedStatements`
(`conn.go:1436`), two loader goroutines at once.

Re-confirmed 2026-09-19 as the negative control for everything below. With FIX-221's
`g.SetLimit(1)` stripped from all seven `loadRelationships` bodies in the `graphql` tree and no
lock in place, `go test -race -run TestLoadRelationships ./tests/`:

```
48 × WARNING: DATA RACE
relationship_tx_fanout_test.go:192: nested mutation inside a caller transaction:
  create user with related: load user events: get events:
  tx query outer_fanout: tx query: conn busy
FAIL
```

### 2.2 It is pgx-only today, by adapter accident

| Dialect | Adapter | Two edges in a tx |
|---|---|---|
| PostgreSQL | `database/pgx` | **fails** — `conn busy` + data race |
| MySQL 8.0 | `database/stdlib` | passes |
| SQLite (modernc) | `database/stdlib` | passes |

`database/sql` holds the driver connection's own lock across each call, so the stdlib adapters
tolerate the shape. That is not a guarantee this project makes, and it means a consumer who
develops against SQLite and deploys on Postgres meets the defect in production. It is an argument
*for* the lock, not against it: the lock makes all three dialects behave identically.

It also sets the bar for the fail-fast alternative in §6: MySQL and SQLite consumers who fan out
over a `txCtx` have working code today, and an option that starts returning errors to them is a
regression, not a fix.

### 2.3 A mutex around the driver call is not enough

The obvious shape — lock at the top of each method, `defer` the unlock — was built and measured on
2026-09-21, because it is the first thing anyone reaches for:

```go
func (tx *Tx) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
    …
    tx.connMu.Lock()
    defer tx.connMu.Unlock()          // fires when Query returns
    return conn.Query(ctx, sql, args...)
}
func (tx *Tx) Exec(ctx context.Context, sql string, arguments ...any) (Result, error) {
    …
    tx.connMu.Lock()
    defer tx.connMu.Unlock()
    return conn.Exec(ctx, sql, arguments...)
}
```

**One goroutine is enough to break it.** Both statements below go through methods that lock and
defer, so they are as serialized as that lock can make them:

```go
rows, err := client.Raw(txCtx, "SELECT 1 UNION ALL SELECT 2")
defer rows.Close()
_, err = client.RawExec(txCtx, "SELECT 3")
// → tx exec defer_single: tx exec: conn busy
```

`Query` returned, so its deferred `Unlock` has already fired — but the connection is still streaming
those two rows. The lock was released while the thing it protects was still in use.

**With two goroutines it is worse, because the lock makes it look defended.** The `graphql`
relationship fan-out, unbounded, under `-race`:

| Design | Data races | Outcome |
|---|---:|---|
| No lock at all (§2.1) | 48 | `conn busy` |
| Lock + `defer` unlock in every method | **28** | `conn busy` |
| Reservation released on drain (§2.6) | **0** | passes |

The middle row is the trap: the race count drops by 40%, so the change *looks* like progress, and
nothing is actually fixed. One of the 28 pairings, trimmed to the two stacks:

```
goroutine A — holds NOTHING (Tx.Query already returned, its defer already ran)
  pgx.(*baseRows).Next()  →  (*baseRows).Close()
    →  (*ResultReader).Close()  →  (*PgConn).receiveMessage()      ← reading the connection

goroutine B — holds tx.connMu, correctly, for its whole driver call
  database.(*Tx).Query()  →  pgxpool.(*Tx).Query()  →  pgx.(*Conn).Query()
    →  deallocateInvalidatedCachedStatements()  →  (*PgConn).TxStatus()   ← same connection
```

B is doing everything the design asks of it. A is not holding the lock *because it already gave it
back* — it is scanning, which is the part of the work that happens after the method returns. The
lock is obeyed perfectly and protects nothing.

That is the whole lesson: **the unlock point has to be the end of the connection's busy window, and
the end of a method is not it.** §2.5 finds where the window actually ends.

#### The original measurement, for completeness

The same shape was first measured on the existing mutex. `Tx.Query`
(`database/transaction.go:169-182`) releases `tx.mu` *before* `conn.Query`; holding it across the
call instead (`defer tx.mu.Unlock()`) gave:

- without `-race`: 20/20 runs passed — the `conn busy` stopped reproducing;
- with `-race`: **still 7 data races per run**.

The races moved rather than disappeared. New pairing: goroutine A inside `Tx.Query` **holding the
mutex** → `pgconn.(*PgConn).TxStatus()`, against goroutine B **holding nothing** →
`baseRows.Next()` → `baseRows.Close()` → `pgconn.(*PgConn).receiveMessage()`. B is scanning rows,
which happens after `Tx.Query` returned.

A single-goroutine probe isolates the window, no concurrency involved:

```go
rows, _ := tx.Query(txCtx, "SELECT …")   // not drained
defer rows.Close()
tx.Exec(txCtx, "SELECT 1")               // → tx exec: conn busy
```

Drain the rows first and the same `Exec` succeeds. **The connection's busy window outlives the
method that opened it.** Any lock scoped to a `Tx` method is scoped to the wrong thing.

That last sentence is where the first draft stopped. The next two sections are what it missed.

### 2.4 One mutex cannot serve both roles — and the second one cannot touch the first

Holding `tx.mu` across the whole `Rows` lifetime deadlocks in 45 seconds:

```
goroutine 181 [sync.Mutex.Lock]:  database.(*Tx).IsClosed(…)   ← child GetMany, via database.Conn
goroutine 182 [sync.Mutex.Lock]:  database.(*Tx).IsClosed(…)
goroutine 183 [sync.Mutex.Lock]:  database.(*Tx).IsClosed(…)
```

`tx.mu` guards the struct's own state (`closed`, `conn`, `callbacks`, `depth`, `savepointNames`),
read on *every* call — `database.Conn(ctx, querier)` takes it via `IsClosed()` before issuing
anything. A microsecond-scale lock and a result-set-scale lock cannot be the same lock. **The design
needs a second mutex.**

The same lesson recurs one level down, and it cost an hour of this revision's measurement time.
`Tx.Begin` (`database/transaction.go:200`) holds `tx.mu` across its **whole body** via `defer`,
savepoint `Exec` included. A first cut of the design stored its bookkeeping (which statement holds
the connection) under `tx.mu`, so the release path re-entered a mutex `Begin` already held:

```
goroutine 604 [sync.Mutex.Lock]:
  database.(*Tx).releaseConn(…)   transaction.go:167
  database.(*Tx).Begin(…)         transaction.go:371
  database.NewTransaction(…)
  models.(*userClient).CreateWithRelated(…)
```

`TestCreateWithRelated_ComposesWithACallerTransaction` hung for 1m35s. **Every piece of connection
bookkeeping needs its own mutex, disjoint from `tx.mu`** — not just the connection reservation
itself. Giving it one made the suite pass.

The design as it now stands sidesteps this rather than solving it: dropping the self-deadlock
diagnosis (§6.1) left no bookkeeping to guard, so nothing on the reservation path takes `tx.mu` at
all (§3.1). The finding still constrains any future field added beside `connSem`.

### 2.5 The busy window ends at exhaustion, not at `Close`

This is the finding the first draft did not have, and it changes the cost of the whole design.

Both drivers close the result set themselves the moment iteration runs out, which frees the
connection before any `Close()` of ours runs:

- **pgx v5.9.1**, `rows.go`, `baseRows.Next`: the `else` branch calls `rows.Close()` — which drains
  `resultReader` through to `ReadyForQuery` — and returns false.
- **`database/sql`**, Go 1.27.1, `sql.go`, `Rows.Next` → `nextLocked` returns `doClose=true` at
  `io.EOF`, and `Next` then calls `rs.Close()` → `releaseConn`.
- `Err()` stays valid after close on both (`baseRows.Err` reads a retained field;
  `Rows.Err` reads `lasterr` under `closemu`), so releasing early costs no error fidelity.

This is exactly what §2.3's probe already showed from the outside — *"drain the rows first and the
same `Exec` succeeds"* — read back in the drivers' own source. The generated tree relies on it
everywhere without naming it: `for rows.Next() { … }` then the next statement. One generated site
does name it, in `insertUpsertAndResolveID`:

```go
// Closed before the fallback below issues its own statement: conn may be a
// transaction, where an open result set blocks the next statement on the
// same connection.
if cerr := rows.Close(); err == nil {
```

So there are two candidate scopes for the lock, and they are not equivalent:

| Scope | Released at | Matches the driver? | Call sites to convert |
|---|---|---|---|
| **S-close** (first draft) | `Rows.Close()` | overshoots | **47**, all-or-nothing |
| **S-drain** (this revision) | first of exhaustion / `Close()` | exactly | **0** |

### 2.6 S-drain works with no call-site changes; S-close does not

Both were built and run against the same tree, with FIX-221's bound removed so the fan-out is
genuinely unbounded.

**S-drain** — release on the first of `Next()==false` or `Close()`:

| Suite | Result |
|---|---|
| `TestLoadRelationships_*` (`-race`) | 3/3 pass, **0 races** |
| `graphql` full suite (`-race`, 37 files) | pass, 42.8s, 0 races |
| `postgres`, `postgres_stdlib`, `mysql`, `sqlite` | pass |
| `tenancy`, `tenancy_postgres`, `tenancy_mysql` | pass |
| `events`, `cache`, `graphql_top_level`, `graphql_null_wrappers` | pass |
| `./database/...` (`-race`) | pass |

Eleven example suites, three dialects, **no template edited, no generated file edited** beyond
deleting the `SetLimit(1)` the lock makes redundant.

**S-close** — identical code, except `Next()` does not release:

```
--- FAIL: TestCreateWithRelated_D18DiscriminatorRoundTrip/a_connect_of_the_matching_kind_is_adopted
    CreateWithRelated: create workspaceNote with related: DraftChildren: connect:
    get workspace_notes: tx query create_workspace_note_with_related:
    transaction create_workspace_note_with_related already has an open result set on
    this goroutine (UPDATE "public"."workspace_notes" SET "parent_id" = $1 WHERE
    id = ANY($2) AND kind = $3 AND parent_id IS NULL AND worksp...):
    drain or close it before issuing the next statement

running tests:
    TestCreateWithRelated_ResolvesTheTenantOnce (1m36s)      ← hung
panic: test timed out after 1m40s
```

The named statement is `table/update.go.tmpl`'s `RETURNING` read, reached through the nested
`connect` verb's adoption path (`table/nested.go.tmpl:881` → `UpdateWhere` → drain `RETURNING` PKs →
`GetMany`) — the archetypal "load-bearing" site from the first draft's §4.2, caught at the exact
moment it re-enters. This is the 47-site
migration presenting its bill. S-drain never issues the diagnostic at all, because at that point
the connection is genuinely free.

### 2.7 A blocking lock turns a leak into a permanent hang — including teardown

A result set that is never drained *and* never closed keeps the reservation. Probed with
`client.Raw` inside a `WithTx`, plain `sync.Mutex`:

| Probe | Result |
|---|---|
| leak → next statement | **hangs** (no return after 6s) |
| leak → `Rollback` | **hangs** — the transaction cannot be torn down |
| leak → `Commit` | **hangs** |
| leak → next statement, `ctx` deadline 2s | **hangs** — `sync.Mutex.Lock` ignores `ctx` |

The middle two are the serious ones: today a leak surfaces as pgx's `conn busy`, the rollback
fails loudly and the connection is destroyed. Under a naive blocking lock the pooled connection is
stranded for the life of the process. **This is a real regression and the design must answer it.**
§3.3 and §3.4 are that answer; §2.8 is the measurement of the answer.

### 2.8 Bounded acquire and non-blocking teardown remove every hang but one

Same four probes, plus a fifth, against the design as specified in §3:

| Probe | Result | Time |
|---|---|---|
| leak → `Rollback` | `rollback failed: … pgx rollback: conn busy (original error: boom)` — i.e. today's behavior | 0.02s |
| leak → `Commit` | `committing transaction probe_leak_c: pgx commit: conn busy` | 0.02s |
| leak → next statement, `ctx` deadline 2s | `tx exec probe_leak_ctx: transaction probe_leak_ctx: waiting for the connection held by an open result set: context deadline exceeded` | 2.00s |
| leak → next statement, `TxOptions.Timeout: 2s` | same | 2.00s |
| leak → next statement, **no deadline anywhere** | **blocks indefinitely** | — |

The first two are what §2.7 said the design had to answer, and §3.4 answers them: teardown never
waits, so a leaked result set can no longer strand a pooled connection. The next two are the
general case — any deadline on the context, including the `TxOptions.Timeout` the PRD already
specifies, converts the wait into a loud error.

The last row is the residual, and it is deliberate (§6.1's decision). A design variant that also
recorded the holding goroutine's id could distinguish "you are waiting on yourself" — which never
resolves — from "you are waiting on another goroutine" — which does, and must be allowed to. It was
built and measured: every one of these probes returned in ≤0.01s with
`transaction probe_leak already has an open result set on this goroutine (…): drain or close it
before issuing the next statement`. It was dropped for the cost in §2.9 and the machinery in §3.1.
`Raw` taking a scan callback (§3.7) removes the largest population it was there to catch.

*Measurement note:* the probes were run with the in-flight SQL recorded, so the logged text carried
a `(SELECT 1 UNION ALL SELECT 2)` clause naming the holding statement. Without that bookkeeping
(§3.1) the error is the same minus the parenthetical.

### 2.9 What the mechanisms cost

`go test -bench`, darwin/arm64, Go 1.27.1, 200 000 iterations:

| Operation | ns/op |
|---|---:|
| `sync.Mutex` Lock+Unlock | 1.8 |
| `chan struct{}` cap-1 send+receive (ctx-selectable) | 16.4 |
| `runtime.Stack` goroutine-id extraction | 1 796 |

The channel is 9× a mutex and buys `ctx` cancellation; both are noise against a round trip, and the
channel is the only mechanism the design keeps.

The third row is why the self-deadlock detection described in §2.8 is **not** in the design. At
~1.8µs it is a thousand times a mutex — taken once per transactional `Query`/`QueryRow`, so a low
single-digit percentage of a local round trip and unmeasurable against a network one, but it also
required a third mutex and two more fields on `Tx` (§3.1) to hold what it compared against. The
judgment was that an immediate diagnosis of one misuse shape did not justify a traceback plus
bookkeeping on every transactional read, particularly once §3.7 closed the path most likely to
produce that misuse.

### 2.10 `Stream` is the one API the lock makes worse

`Stream` is the only generated method that runs **consumer code inside an open result set** — the
`yield` body executes between `rows.Next()` calls, with the connection still streaming. Two probes,
`graphql` tree, three seeded rows, measured 2026-09-21:

| Probe | Today | Under §3 |
|---|---|---|
| (A) write inside the stream loop, one goroutine | fails at once: `iterate user rows: pgx rows: conn busy` (rollback poisoned too) | **hangs** |
| (B) stream in one goroutine, `GetMany` in another, same `txCtx` | **13 data races** + `get users: … conn busy` | **passes, 0 races**, all three rows streamed |

(B) is the design working. (A) is the design making a loud failure silent, and the hang is not
contained to the request: the leaked reservation also stalled the test binary's own teardown for
~200s, because closing the pool waits on the stuck connection.

(A) is the more likely shape by a distance. "Stream rows, write for each one, inside a transaction"
is an ordinary ETL spelling that someone writes without thinking about concurrency at all. (B)
requires deliberately fanning goroutines out over a `txCtx`, which [§18.5](../../PRD.md) already tells
callers not to do.

Both columns feed §6.2, and note what the (B) row rules out: simply *opting `Stream` out* of the
reservation leaves those 13 races reachable. Only refusing the call removes them.

### 2.11 Release-on-drain needed one adapter fix to be true

§2.5 establishes that both drivers close a result set when `Next` reports exhaustion. That is
unconditional on pgx. On `database/sql` it has an exception, and the exception is reachable here:
`Rows.nextLocked` returns `doClose = false` when the driver implements `driver.RowsNextResultSet`
**and** `HasNextResultSet()` is true, so `Next()` reports the current set exhausted while the
connection still carries the next one. `go-sql-driver/mysql` implements it (`rows.go:129`), and this
repo's own stdlib harness connects with `multiStatements=true`
(`stdlib_integration_test.go:68`).

Probed on MySQL 8.0 through the `database/stdlib` adapter with the reservation in place:

| Probe | Result |
|---|---|
| **Control** — `SELECT 1`, drain, then `tx.Exec` | `err=<nil>` — drain released the reservation, next statement proceeded, correct |
| **Multi** — `SELECT 1; SELECT 2`, drain set 1, then `tx.Exec` | drained 1 row, `rows.Err()=<nil>`, then `tx exec mrs_probe: tx exec: busy buffer` — **and** `rollback failed: stdlib rollback: invalid connection` |

The second row is the defect in full: the reservation was released while the connection was still
busy, the next statement reached the driver, `go-sql-driver/mysql` reported `ErrBusyBuffer`, and the
connection was destroyed so the rollback could not run either — the same poisoned-transaction
failure §2.1 measured without any lock at all.

**The fix is in the adapter, not the design.** `database/stdlib`'s `rows.Next()` closes the
underlying `sql.Rows` on exhaustion, making exhaustion and connection release coincide exactly as
they already do on pgx:

```go
func (r *rows) Next() bool {
	if r.r.Next() {
		return true
	}
	_ = r.r.Close() // idempotent; errors surface via Err
	return false
}
```

Nothing is lost: `database.Rows` exposes no `NextResultSet`, so the second result set was unreachable
by callers regardless — it only pinned the connection until `Close`. Re-probed with the fix, the
multi-result-set case returns `err=<nil>`. Regression after it: `./database/...` under `-race`, plus
the `mysql`, `sqlite` and `postgres_stdlib` example suites, all pass.

Two things worth being precise about. This is **not a pre-existing bug** — without the reservation,
the caller's `defer rows.Close()` frees the connection and nothing misbehaves, so the fix is a
*prerequisite of release-on-drain* rather than something worth landing on its own. And pgx needs no
counterpart: `baseRows.Next` calls `Close()` unconditionally (§2.5).

---

## 3. The design

### 3.1 Two locks with distinct scopes

```go
type Tx struct {
    mu      sync.Mutex    // struct state: closed, conn, callbacks, depth, savepointNames
    connSem chan struct{} // capacity 1: the driver connection's busy window
    …
}
```

That is the whole addition: one field. A capacity-1 channel rather than a `sync.Mutex`, because
acquisition must be selectable against `ctx.Done()` (§2.7).

No bookkeeping accompanies it — no in-flight statement, no holding goroutine (§6.1). That is worth
naming as a property rather than an omission: because `connSem` is never read except to be taken or
released, nothing in the reservation path needs `tx.mu`, and §2.4's second instance — where storing
the bookkeeping under `tx.mu` re-entered a mutex `Begin` already held — cannot recur. The design has
no third lock because it has nothing for a third lock to guard.

#### Why the count is not a consequence of where the lock is taken

It is worth being exact about this, because "two locks" sounds like a cost the wrapper alternative
(§4.4) could avoid. It is not:

- **`tx.mu` already exists** and is unchanged by this design. It guards the `Tx` struct's own fields
  and is taken at **nine** sites in `database/transaction.go` — `IsClosed`, `OnCommit`, `Exec`,
  `Query`, `QueryRow`, `Begin`, `NewTransaction`, `Commit`, `Rollback`. Nothing about locking in a
  wrapper removes the need for it.
- **`connSem` is the one new lock.** A wrapper changes *where* it is acquired, not *whether* it is
  needed.

So the count under either placement is one pre-existing lock plus one new one. The real question is
whether the new one could just *be* `tx.mu`, and that has now been measured twice in the negative:

1. §2.4's first instance — holding `tx.mu` across a result set parked three loader goroutines in
   `IsClosed()`, reached through `database.Conn` at the head of every nested read.
2. §2.4's second instance — `Tx.Begin` holds `tx.mu` across its whole body, so merely storing the
   *bookkeeping* under `tx.mu` re-entered it and hung a test for 1m35s.

Neither is fixed by converting call sites, because the contention is not at the call sites. Merging
the two would mean every `IsClosed`, `OnCommit`, `Begin`, `Commit` and `Rollback` waits on a lock
held across a network round trip plus a scan, and any one of them reached from inside a callback is
an immediate self-deadlock. That is not hypothetical: `cache.dispatchRefresh`
(`cache.go.tmpl:1027-1029`) calls `IsClosed()` and then `OnCommit()` — two `tx.mu` acquisitions —
from the cache layer, and the event hook calls `OnCommit` from its own
(`event_hooks.go.tmpl:270`). A microsecond-scale lock and a result-set-scale lock cannot be the same
lock, whoever takes it.

An earlier revision carried a third mutex guarding the in-flight statement and holding goroutine, to
support the self-deadlock diagnosis measured in §2.8. Dropping that diagnosis (§6.1) dropped the
mutex with it, since it guarded nothing else. The count is therefore **one pre-existing lock and one
new one** — under either placement.

### 3.2 The lock is scoped to the driver's busy window

`Exec` takes the reservation across the driver call and releases it on return — an `Exec` has no
result set, so that is its whole busy window. `Query` takes it and hands it to the returned rows:

```go
func (tx *Tx) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
    tx.mu.Lock()
    if tx.closed { tx.mu.Unlock(); return nil, fmt.Errorf("transaction %s is closed", tx.name) }
    conn := tx.conn
    tx.mu.Unlock()

    if err := tx.acquireConn(ctx); err != nil {
        return nil, fmt.Errorf("tx query %s: %w", tx.name, err)
    }
    r, err := conn.Query(ctx, sql, args...)
    if err != nil {
        tx.releaseConn()
        return nil, fmt.Errorf("tx query %s: %w", tx.name, err)
    }
    return &txRows{Rows: r, release: sync.OnceFunc(tx.releaseConn)}, nil
}

// txRows holds the connection for the driver's real busy window: from Query
// until the result set is drained or closed, whichever comes first.
type txRows struct {
    Rows
    release func()
}

func (r *txRows) Next() bool {
    if r.Rows.Next() { return true }
    r.release()       // ← the whole migration lives or dies on this line (§2.5, §2.6)
    return false
}

func (r *txRows) Close() error {
    defer r.release()
    return r.Rows.Close()
}
```

`sync.OnceFunc` matters: generated code both `defer`s `Close` and sometimes closes explicitly, and
a drained result set is released before either.

`QueryRow` needs its own wrapper. Both adapters execute the statement at `QueryRow` time — pgx's
`Conn.QueryRow` calls `Query` immediately and returns a `connRow` over it — and consume the row in
`Scan`, so the reservation runs from `QueryRow` to the end of `Scan`. `QueryRow` has no error
return, so an acquire failure comes back as a `Row` whose `Scan` reports it.

### 3.3 Acquisition blocks, bounded by ctx

```go
func (tx *Tx) acquireConn(ctx context.Context) error {
    select { // free: take it, and never lose a race to an already-expired ctx
    case tx.connSem <- struct{}{}:
        return nil
    default:
    }
    select {
    case tx.connSem <- struct{}{}:
        return nil
    case <-ctx.Done():
        return fmt.Errorf("transaction %s: waiting for the connection held by an open result set: %w",
            tx.name, ctx.Err())
    }
}

func (tx *Tx) releaseConn() { <-tx.connSem }
```

The non-blocking first `select` is not an optimization. A single `select` over both cases picks
pseudo-randomly when both are ready, so a statement issued on an already-expired context would
sometimes run and sometimes fail. Trying the uncontended case first makes it deterministic: if the
connection is free, it is always taken.

`releaseConn` is an unguarded receive because it is only ever reached through the `sync.OnceFunc`
that `Query`/`QueryRow` wrap it in, which pairs it 1:1 with an acquire. §3.4's teardown path never
calls it at all when it did not take the reservation.

**Blocking is the right default for the case this document exists for.** Another goroutine holding
the connection will finish, and the waiter then proceeds — which is how the §1 fan-out becomes safe
on every dialect, and what [PRD §20.5](../../PRD.md) promised. It is also what the `database/sql` adapters
already do internally (§2.2), so MySQL and SQLite consumers whose code works today keep working;
a fail-fast reservation would start returning errors to them.

The cost of that default is the one row §2.8 leaves open: a goroutine that blocks on a reservation
*it* holds waits forever, because nobody else can release it. `TxOptions.Timeout` or any deadline on
the context bounds it; §6.1 records why no further machinery is spent distinguishing the case.

### 3.4 Teardown never blocks

`Commit` and `Rollback` try the reservation and **proceed without it** if the connection is pinned:

```go
func (tx *Tx) acquireForTeardown() func() {
    select {
    case tx.connSem <- struct{}{}:
        return tx.releaseConn
    default:
        // fn has already returned, so anything still holding the connection is a
        // leak, not a statement in flight. Proceed and let the driver report it: a
        // COMMIT that cannot run strands the pooled connection, which is strictly
        // worse than a loud failure.
        return func() {}
    }
}
```

No timeout constant, and no new config field: by the time `WithTransaction` reaches `Commit` or
`Rollback`, `fn` has returned. §2.8 measures the result — the pre-existing `conn busy` in
hundredths of a second instead of the permanent hang of §2.7.

The savepoint statements in `Begin`, `Commit` and `Rollback` use the same helper, which means their
`tx.mu`/`connSem` ordering must be untangled first: `Begin` currently holds `tx.mu` across its
`Exec` (§2.4), and `Commit`/`Rollback` hold it across theirs.

### 3.5 What the lock does outside a transaction

Nothing — there is no `Tx`. `database.Conn(ctx, c.querier)` returns the pool, `Tx.Query` is never
reached, and each relationship load draws its own pooled connection and runs genuinely in parallel.
The lock is per-`Tx` and so is exactly as narrow as the hazard: **parallel when it is safe, serial
only when the connection is shared**, with no configuration and no call-site knowledge required.

This is the property the callback-wrapper approach was reaching for — one spelling of the call site
that is correct both inside and outside a transaction. S-drain delivers it without the call sites
having to change at all, because the code the templates already emit (`for rows.Next()`, then the
next statement) is *already* the correct shape.

### 3.6 What it does not buy

Not speed. Inside a transaction the server executes statements one at a time regardless, so
serializing costs nothing and the lock recovers nothing. What it buys is the **contract**: a
consumer who fans out over a `txCtx` blocks instead of racing, on every dialect. Latency inside a
transaction is a function of round trips, and reducing those is a different design — see §6.7.

---

### 3.7 `Raw` takes a scan callback

`Client.Raw` is the one generated API that hands a live result set to a consumer. Under §3.2 that
result set carries the transaction's connection reservation, so a caller who holds it open pins the
connection — and `Raw` is exactly the API whose callers are least likely to be thinking about that,
because the whole point of reaching for it is that the generated surface could not express the
query. The signature is changed so the result set cannot escape:

```go
// before — the rows, and the reservation with them, outlive the call
func (c *Client) Raw(ctx context.Context, rawSQL string, args ...any) (database.Rows, error)

// after — the rows cannot outlive fn, so neither can the reservation
func (c *Client) Raw(ctx context.Context, rawSQL string, args []any, fn func(database.Rows) error) error
```

The parameter shape deliberately matches `database.QueryFunc` (`database/transaction.go:431`), which
has carried exactly this signature since it was written and is what the terminal now calls:

```go
func (c *{{ .ClientName }}) Raw(ctx context.Context, rawSQL string, args []any, fn func(database.Rows) error) error {
	terminal := func(ctx context.Context, _ *hook.QueryContext) (any, error) {
		return nil, database.QueryFunc(ctx, database.Conn(ctx, c.querier), rawSQL, args, fn)
	}
	chain := hook.BuildQueryChain(c.opts.panicHandler, c.opts.queryHooks, terminal)
	_, err := chain(ctx, &hook.QueryContext{Op: hook.QueryOp("raw_query"), Input: args})
	return err
}
```

Scanning now happens **inside** the hook chain rather than after it. That is the point — the
reservation is taken and released within the terminal, so a `Raw` read inside a transaction queues
behind other goroutines' statements and releases on drain like every other read (§2.5). It is also
where the concurrency guarantee comes from: two goroutines that both reach for `Raw` on one `txCtx`
now serialize instead of racing, which is what [PRD §20.5](../../PRD.md) will be able to promise without
an asterisk.

Three consequences worth stating rather than discovering later:

- **Tracing spans now cover the scan.** A global query hook's span used to close when `Query`
  returned and the consumer scanned outside it; now the span spans the scan too. More accurate, but
  a changed measurement.
- **A query hook can no longer intercept the rows of a raw query.** The terminal returns `nil`
  instead of `database.Rows`. Nothing does this today — [PRD §20.2](../../PRD.md) gives `Raw` the *global*
  hooks only (tracing, authorization), and neither reads the result set — but the capability is
  gone, not merely unused.
- **It removes the only type assertion on `database.Rows` in the generated tree.**
  `result.(database.Rows)` disappears, which is the one place §4.4's "the `txRows` wrapper erases
  nothing" claim had to be checked rather than reasoned about.

`args` moves from variadic to `[]any`, so single-argument calls gain a `[]any{…}`. That is the
ergonomic cost and it is real; matching the existing `QueryFunc` shape is worth more than saving the
braces, and no ordering that keeps the variadic (callback first) reads well.

**`RawExec` is unchanged.** It has no result set, so its busy window is entirely inside the call and
`Tx.Exec` already covers it (§4.4). **`Querier(ctx)` is unchanged** too, for the opposite reason:
[PRD §20.2](../../PRD.md) specifies it as direct access to the underlying `database.Querier`, and handing
out the raw querier is the feature. §3.2's placement is what covers it; no signature can.

Deliberately **not** proposed: a generic `RawInto[T]` that scans into a slice. It is a real
convenience and it is not in the PRD, so it would be inventing a feature — noted and dropped.

### 3.8 The safe spellings for consumers

The reservation makes concurrent use safe on every path (§4.3). What it cannot do is end a wait
whose waiter is its own holder, so exactly one rule falls to the consumer: **drain or close a result
set before issuing the next statement on that `txCtx`.** A consumer who ignores it has a bug either
way — today it is `conn busy`, under §3 it is a wait — but the library can make the rule hard to get
wrong. Three spellings, in decreasing order of how much it does for you:

| Spelling | Hooks | Can the caller break the rule? |
|---|---|---|
| `client.Raw(ctx, sql, args, fn)` (§3.7) | global | **No** — the rows cannot outlive `fn` |
| `database.QueryFunc(ctx, client.Querier(ctx), sql, args, fn)` | none | **No** — its `defer rows.Close()` releases the reservation |
| `client.Querier(ctx).Query(ctx, sql, args…)` | none | Yes — the full-control door ([PRD §20.2](../../PRD.md)) |

The middle row is what to point consumers at when they need `Querier(ctx)`'s hook-free access
without its sharp edge, and it needs no new API: `QueryFunc` already exists with the right shape
(`database/transaction.go:431`), and under §3.2 its `defer rows.Close()` is what releases the
reservation. It is leak-proof unmodified.

**`QueryRowFunc` is not, and must be fixed before it is recommended.** As written
(`database/transaction.go:449`) it is the whole function:

```go
row := q.QueryRow(ctx, sql, args...)
return fn(row)
```

`Tx.QueryRow` takes the reservation and `txRow.Scan` releases it (§3.2), so an `fn` that returns
without scanning — an early return on a guard, say — leaves it held. The helper protects nothing its
raw counterpart does not. It must release after `fn` returns either way, which it can do directly
since it lives in the same package as `txRow`:

```go
func QueryRowFunc(ctx context.Context, q Querier, sql string, args []any, fn func(Row) error) error {
	row := q.QueryRow(ctx, sql, args...)
	if tr, ok := row.(*txRow); ok {
		defer tr.release() // sync.OnceFunc, so a Scan inside fn has already made this a no-op
	}
	return fn(row)
}
```

**`ExecFunc` should not be built.** `Tx.Exec` takes and releases the reservation inside the call
(§3.2), and the `Result` it returns carries values both adapters have already materialized — there
is no window for a caller to hold open. A helper would be a pass-through whose existence implies
`Exec` is unsafe without it, which is false and worse than saying nothing.

---

## 4. What it costs

The first draft's §4 was a 47-site migration whose failure mode was a hang. That section is gone;
this is what replaces it.

### 4.1 Nothing at the call sites

`grep -rn "\.Query(ctx\|\.QueryRow(ctx" cmd/sqlgen/gen/templates/` still reports 47 sites in 14
templates, and under S-drain **all 47 stay exactly as they are**. Every generated read either
drains its result set (`for rows.Next()`) before the next statement, or returns immediately
afterwards so its `defer rows.Close()` runs first. §2.6 is the evidence: eleven suites, three
dialects, zero template edits.

`database.QueryFunc` / `QueryRowFunc` (`database/transaction.go:431`, `:449`) remain unused by
anything. Adopting them is now a tidiness question rather than a prerequisite — the shape they
enforce is one the templates already emit. §4.4 works through that alternative properly, including
why `ExecFunc` and `QueryRowFunc` would be churn and `QueryFunc` would not.

### 4.2 A changed failure mode for code that leaks a result set

This is the cost, and it is narrow. A caller that opens a result set, abandons it part-drained, and
then issues another statement on the same transaction gets:

| | today | under the design |
|---|---|---|
| same goroutine | `conn busy` (pgx) / works by accident (stdlib) | blocks until `ctx` expires, then a loud error |
| another goroutine | data race + `conn busy` (pgx) / works by accident (stdlib) | blocks until the holder finishes, or `ctx` expires |
| leaked, then commit/rollback | `conn busy`, connection destroyed | unchanged (§3.4) |

The first row is the honest regression: a misuse that pgx reports immediately today becomes a wait.
Any deadline converts it to a loud error, and `TxOptions.Timeout` provides one without new config
(§2.8).

**But there is no default deadline, and that is deliberate.** `TxOptions.Timeout` defaults to `0`,
and [PRD §18.2](../../PRD.md) says so on purpose: *"When `Timeout` is zero (the default), no timeout is
applied. This is intentional — some transactions involve legitimate long-running work, and an
aggressive default timeout would cause unexpected failures."* No adapter or client sets one either
(`database/pgx`, `database/stdlib`, `client.go.tmpl` carry no timeout at all). So in the **default**
configuration, with no `context.WithTimeout` from the caller, this row blocks indefinitely rather
than eventually loudly. §6.1 records the decision that produced it, and now carries this fact.

Two things narrow it. `Raw` can no longer leak at all (§3.7), which was the widest path into this
row. And if the error text should name the statement that holds the connection — genuinely useful
when it does fire — an `atomic.Pointer[string]` set on acquire and cleared on release would carry it
with no mutex and no traceback. Noted, not proposed.

### 4.3 The escape hatches stay reachable — but they are covered

To be exact about what "escape hatch" means here, because it is easy to read as *unprotected*:
under §3.2 every one of these paths funnels into `Tx.Query`/`Exec`/`QueryRow` and **takes the
reservation**. Safety is not in question on any of them — two statements cannot reach the connection
at once, whichever door they came through. What stays open is that a caller can still *hold a result
set* across the next statement, which changes that misuse from pgx's immediate `conn busy` to a wait
(§4.2's first row, and §6.1's accepted trade).

`Querier` requires `Query(ctx, sql, args) (Rows, error)`, so the raw primitive remains reachable.
The first draft treated this as a `Querier`-shaped problem. It is bigger: the **generated client's
own public API** hands result sets to consumers.

- `Client.Raw` (`client.go.tmpl:301`) used to return `database.Rows` straight to the caller. §3.7
  changes it to take a scan callback, so this one is closed by construction rather than by
  documentation — the rows cannot outlive the call and neither can the reservation.
- `Client.Querier(ctx)` (`client.go.tmpl:291`) hands out the `database.Querier` itself, which inside
  a transaction is the `*database.Tx`. [PRD §20.2](../../PRD.md) specifies it that way on purpose. It
  stays open, and §3.2's placement is the only thing that covers it.
- `Stream` (`table/stream.go.tmpl:140`) holds its result set open across a `yield` callback for the
  whole iteration. A consumer who writes inside the stream loop on the same `txCtx` is issuing a
  statement mid-iteration — genuinely broken today, `conn busy` on pgx — and gets §4.2's error
  instead. Both `Stream` and `Raw` pass their suites unchanged under the design.

For the doors that stay open the lock makes raw use *diagnosable*, not impossible: documenting the
rule on `Tx`, on `Querier(ctx)` and in §18.5 is the mitigation, and it is the rule §18.5 already
states. `Raw` no longer needs that mitigation.

### 4.4 The wrapper alternative — `QueryFunc`/`QueryRowFunc`/`ExecFunc` at the call sites

The standing alternative is to leave `Tx.Query`/`QueryRow`/`Exec` alone and have generated code call
wrapper functions instead — `database.QueryFunc(ctx, conn, sql, args, fn)` and friends — which take
the reservation when `conn` is a `*Tx` and plain-delegate when it is the pool. One spelling, correct
inside and outside a transaction.

It works: the first draft's §2.5 proved it end to end for the `table/get.go.tmpl` pair. The question
is not whether it works but what it covers and what it costs, and those turn out to differ sharply
per verb.

**The wrapper and the lock's placement are independent decisions.** The measurement in §2.5–§2.6
settled the lock's *scope* (drain, not close). Placement is a separate axis:

| | lock inside `Tx.Query` (§3) | lock inside the wrappers only |
|---|---|---|
| release at `Close` | first draft: 47 sites, all-or-nothing, hangs | 47 sites, incremental, `fn`-scope hangs |
| release at drain | **0 sites, covers every path** | 82 sites, incremental, covers converted paths |

#### What wrapper-only genuinely simplifies

Stated first, because it is real and most of this section is about what the alternative costs. If
the reservation is taken and released *inside* a wrapper, its lifetime is lexically bounded, and
several pieces of §3 stop being needed:

```go
func QueryFunc(ctx context.Context, q Querier, sql string, args []any, fn func(Rows) error) error {
    if tx, ok := q.(*Tx); ok {
        if err := tx.acquireConn(ctx); err != nil { return err }
        defer tx.releaseConn()
    }
    rows, err := q.Query(ctx, sql, args...)
    if err != nil { return fmt.Errorf("query func: %w", err) }
    defer func() { _ = rows.Close() }()
    if err := fn(rows); err != nil { return err }
    return rows.Err()
}
```

- **`txRows` and `txRow` disappear**, and with them the release-on-drain override. The rows cannot
  outlive the wrapper, so `defer` is a sufficient release point. `Tx.Query` goes back to returning
  the adapter's rows untouched.
- **§3.4's non-blocking teardown disappears.** A reservation that cannot outlive a wrapper call
  cannot still be held when `Commit`/`Rollback` run, so teardown has no leak to route around.
- **§2.5's finding stops being load-bearing.** Release-on-drain is what made the migration
  unnecessary (§2.6); if the sites are being converted anyway, the wider scope costs nothing inside
  a transaction, where statements serialize regardless.

That is a materially smaller runtime. The complexity moves to the templates, not away.

#### What it does not simplify: `acquireConn`

The wrapper bounds the reservation's **lifetime**, not the **wait**. A plain `sync.Mutex` cannot be
interrupted, so a goroutine that blocks on a reservation it already holds blocks forever — and
under wrapper-only that case has a specific name: a converted site whose `fn` issues a statement
before returning. Those are §2.6's seven load-bearing sites, and getting all seven right is the
substance of the migration. §2.7 measured the consequence of a plain mutex directly: the leak
probes hung, and no `ctx` deadline rescued them.

So `acquireConn`'s `ctx`-selectable acquire survives the move into the wrapper unchanged. It is the
backstop for the migration being imperfect, now or in a future template edit — which is what the
first draft's proposed lint gate was also for.

#### Per verb, the wrapper earns its keep very unevenly

- **`ExecFunc` — mandatory here, worthless otherwise.** An `Exec` has no result set, so its busy
  window is entirely inside the method call and locking in `Tx.Exec` is already complete coverage —
  which is why the wrapper adds nothing *if the method holds the reservation*. Under **strict**
  wrapper-only it is the opposite: raw `Tx.Exec` takes nothing, so a generated `Exec` would not
  serialize against a concurrent `QueryFunc` at all, and all 35 sites must convert. The verb that
  looked like pure churn is load-bearing exactly when the wrappers are the only holder.
- **`QueryRowFunc` — buys nothing measurable.** The busy window runs `QueryRow` → `Scan` (both
  adapters execute eagerly; pgx's `Conn.QueryRow` calls `Query` immediately), which `txRow.Scan`
  releasing covers with no call-site change. The wrapper's only marginal value is preventing a
  `QueryRow` whose `Scan` is never called — and all 68 `QueryRow` sites in the generated `graphql`
  tree `Scan` within four lines, so there is no such leak to prevent. Under strict wrapper-only it
  becomes mandatory for the same reason `ExecFunc` does. 8 sites.
- **`QueryFunc` — the only one with a real job under either placement.** `Rows` genuinely outlives
  the call, so a callback is the only construct that bounds it structurally. 39 sites.

All three converting is the 82-site figure; it is 82 because strict wrapper-only has no other
holder, not because `Exec` and `QueryRow` need callbacks on their own merits.

#### What wrapper-only cannot reach

First, what is *not* the problem. A consumer who calls `Raw`, drains the rows, and then issues the
next statement is behaving correctly and is unaffected by either design — the reservation is
released at exhaustion (§2.5), so nothing blocks. Requiring that is [PRD §18.5](../../PRD.md)'s existing
contract and it should stay. The exposure is **concurrency, not sequencing**: two goroutines on one
`txCtx`, each draining its own result set properly, that merely overlap in time. That is the case
this whole document exists for, and a statement that does not take the reservation does not queue —
it races.

Under wrapper-only, the paths that do not take it are the ones the PRD deliberately keeps open:

| Door | Why a callback form cannot close it |
|---|---|
| `client.Querier(ctx)` (`client.go.tmpl:291`) | **[PRD §20.2](../../PRD.md) specifies it** as "direct access to the underlying `database.Querier`… the active transaction if one exists", and prints `client.Querier(ctx).Query(ctx, …)` as the worked example. Handing out the raw `Querier` *is* the feature. |
| `database.FromContext(ctx)` | Public runtime API returning `*Tx`. Same surface, one layer down. |
| `Client.Raw` (`client.go.tmpl:301`) | **Closed** — §3.7 changes it to take a scan callback. |
| `Stream` (`table/stream.go.tmpl:140`) | Already a callback (`yield`), and that is precisely the exposure: the consumer's loop body runs *inside* the open result set, so the callback shape is what creates it, not what fixes it. |

§3.7 closes `Raw`'s row of that table, and is worth doing under either placement — it makes the
most-reached raw path leak-proof, and the project is unreleased so the signature change costs
nothing. It does not substitute for the placement decision: it shuts one door of four while
[PRD §20.2](../../PRD.md) holds `Querier(ctx)` open by design and `Stream`'s shape holds another. Under
wrapper-only it is **mandatory** rather than worthwhile, since it is the only way that design reaches
`Raw` at all — and even then the other two doors stay open.

Locking in `Tx.Query` closes all four at once, without touching any of their signatures, because
every one of them funnels through `Tx.Query`/`Exec`/`QueryRow`. That is the entire argument: the
funnel is the only place that sees every statement.

Note also that for two of the three verbs there is no contest at all. `Exec`'s busy window is
entirely inside the method and `QueryRow`'s runs to the end of `Scan`, so locking both in the method
is complete coverage with zero conversions — which is why §4.4's table rates `ExecFunc` and
`QueryRowFunc` at nothing. **Only `Query` is genuinely contested, and the contest is exactly this
table.**

The objection this alternative avoids — that §3 wraps `Rows` in a `txRows` the caller did not ask
for — does not survive checking. Both adapters' `rows` types (`database/pgx/pgx.go:188-226`,
`database/stdlib/stdlib.go:159-197`) implement exactly the five `database.Rows` methods and nothing
else, and the only type assertion on the interface anywhere in the generated tree is
`result.(database.Rows)` in `Raw`'s own hook chain — an assertion *to* the interface. The wrapper
erases nothing.

#### The asymmetry that decides it: ordering

- **Wrapper-first:** no guarantee at all until every site is converted, and each load-bearing
  conversion moves a statement out of a result set's scope — a real behavior change. The lock is
  conditional on finishing the migration.
- **Lock-first:** the full guarantee on day one with a zero-line call-site diff (§2.6). The wrappers
  then become *optional hygiene*, adoptable a template at a time whenever one is being touched
  anyway, because an un-converted site is already fully protected.

**Lock-first makes the wrapper optional; wrapper-first makes the guarantee conditional on the
wrapper.** That is the whole argument for §3's placement, and it is why §4.1 records the wrapper
adoption as tidiness rather than a prerequisite.

One point genuinely in wrapper-only's favour, for the record: it never introduces blocking on paths
the library does not control. Under §3, two goroutines contending on `Raw` inside one transaction
block instead of racing — which is the feature behaving as designed, but it is new behavior on a
path whose lifetime the library cannot see. §4.2's table is the full accounting.

### 4.5 What has to be documented

The design changes what is true about transactions, so the documentation change is not an afterthought
— it is most of the deliverable. Consolidated here; the detail lives in the sections named.

| Surface | Change |
|---|---|
| `Tx` doc comment (`database/transaction.go:75-89`) | Says at length that concurrent use is undefined and that widening the lock deadlocks. The first half becomes **false**. The second needs §2.4's refinement: widening *`tx.mu`* deadlocks, a disjoint reservation scoped to the driver's busy window does not. |
| [PRD §18.5](../../PRD.md) | The caller-serializes rule narrows to one sentence: drain or close a result set before the next statement on that `txCtx`. Concurrency is no longer the caller's problem. Names `Stream` as the exception (§4.6). |
| [PRD §20.5](../../PRD.md) | Restore the errgroup example the draft withdrew; it is correct under §3. |
| [PRD §20.2](../../PRD.md) | `Raw`'s new signature (§3.7). For `Querier(ctx)`, restate the drain-or-close rule and the changed symptom — the misuse waits now rather than reporting `conn busy` (§6.3). |
| [PRD §9.4a](../../PRD.md) | A decision-table row — *reading inside a transaction → `Connection` in a loop* — plus `Stream`'s single-connection behavior inside a transaction (§4.6). |
| [PRD §18.6](../../PRD.md) | Carries `Raw`'s signature in its "Raw Queries and Hooks" listing; update with §3.7. |
| Generated doc comments | `Querier(ctx)` and `Stream` in `client.go.tmpl` / `stream.go.tmpl` state the rule where a consumer reads it, not only in the PRD. |
| `database.QueryFunc` / `QueryRowFunc` | Documented as the safe spelling for `Querier(ctx)` users (§3.8), with `QueryRowFunc`'s fix. |

The through-line for all of it is one rule, stated the same way everywhere: **a consumer must drain
or close a result set before issuing the next statement on that `txCtx`.** A consumer who does not
has a bug on either side of this change — the change is only what the bug looks like.

---

### 4.6 Why `Stream` cannot be made transaction-safe

§2.10 measures the regression; this is the search for a way to make `Stream` genuinely work inside a
transaction, before settling for refusing the combination.
"Safe" here means the strong form: a consumer can issue statements from inside the `yield` body.
Five approaches were considered and none survives.

| Approach | Why it fails |
|---|---|
| Materialize the result set when in a transaction | Safe, and it silently deletes the one property `Stream` exists for — [PRD §9.4a](../../PRD.md) sells it as memory-bounded, "no materialization". A bounded read becomes an OOM on exactly the workload the method is for, and only inside a transaction. |
| Internal keyset chunking when in a transaction | Portable and bounded, but §9.4a states `Stream` "intentionally has no Limit/Offset"; paging needs a unique tiebreaker appended to the caller's `Sorts`, changing the emitted `ORDER BY`; and `Stream` would issue a different query shape inside a transaction than outside. It is also `Connection` reimplemented inside `Stream`. |
| Server-side cursors (`DECLARE` / `FETCH`) | Protocol-correct — the connection is free between fetches — and PostgreSQL-only. MySQL exposes cursors only inside stored procedures, SQLite has none. `Stream` would be safe on one dialect and not the others, which is the defect §2.2 argues against. |
| A separate connection for the stream | Wrong semantics: it would not see the transaction's uncommitted writes, so `Stream(txCtx)` would silently read stale data. Worse than failing. |
| Release the reservation during the `yield` body | §2.3 restated — releasing the lock does not free the connection. |

The obstruction is not incidental. `Stream`'s contract is *the connection stays open across caller
code*; a transaction's contract is *one connection, one statement at a time*. Inside a transaction
those are the same connection, so the contracts are simply incompatible, and every approach above
has to break one of them or portability.

**The safe alternative already ships, and the PRD already says when to reach for it.** `Connection`
delegates to `GetMany` with a `LIMIT`, so each page is one complete, fully-drained query and caller
code runs *between* pages rather than inside a result set. It needs no change to be safe under §3.
[PRD §9.4a](../../PRD.md)'s existing decision table already carries the row that generalizes — *"Pool-
friendliness on long reads (release the connection between pages) → `Connection` in a loop"* — and
inside a transaction that stops being a preference and becomes the rule.

Supporting evidence that this is a documentation gap rather than a capability gap: across all eleven
example trees, **no `Stream` call sits inside a transaction**. No file that calls `Stream` also calls
`WithTx`.

#### What to do instead

Since the two contracts cannot both hold, `Stream` should say so rather than half-work: **§6.2
recommends a precondition guard that refuses a transaction unless the caller opts in**, mirroring
[PRD §9.6a](../../PRD.md)'s existing `LockMode` guard. The opt-in exists for the one workload this section
cannot otherwise serve — a snapshot-consistent scan that issues nothing else. That is a behavior
change, not only a documentation one, and it needs:

1. [PRD §9.4a](../../PRD.md) to state the guard and the `CallOptions.AllowInTransaction` opt-in with its
   obligation, and its decision table to gain the row *"Reading inside a transaction → `Connection`
   in a loop"*.
2. `Stream`'s doc comment to carry the same, so a consumer reads it at the call site rather than in
   the PRD.
3. [§18.5](../../PRD.md) to name `Stream` as refused rather than as a door left open — which shortens
   §4.3's list by one.

---

## 5. Recommendation and sequencing

### 5.1 The recommendation

**Put the reservation in the `Tx` methods (§3.2), not in the call-site wrappers (§4.4).** Four
reasons, in order of weight:

1. **Size.** One struct field plus two small wrapper types, against 82 call sites across 14
   templates and eleven regenerated golden trees. In a repo where golden files are the regression
   gate, that asymmetry outweighs everything else on this list.
2. **Coverage.** Only the method placement reaches `client.Querier(ctx)`, which [PRD §20.2](../../PRD.md)
   specifies as a first-class escape hatch *with a worked example*. Wrapper-only would ship
   [§20.5](../../PRD.md)'s promise with an asterisk naming a PRD API.
3. **The failure mode of the work itself.** Converting §2.6's seven load-bearing sites has a hang as
   its failure mode, measured. The reservation's failure mode is a wait on paths that already error
   today.
4. **It does not foreclose the wrappers.** Lock-first makes `QueryFunc` adoption optional hygiene,
   picked up whenever a template is open for another reason. Wrapper-first makes the guarantee
   conditional on finishing all 82 conversions first.

Alongside it:

- **`Stream` refuses a transaction by default, with an opt-in** (§2.10, §6.2). It is the only
  generated API that runs consumer code inside an open result set, and §4.6 establishes that its
  contract and a transaction's are incompatible. A guard says so before any SQL is issued; a
  `CallOptions.AllowInTransaction` escape preserves the snapshot-consistent scan for callers who
  accept the obligation. An opted-in stream **holds** the reservation — opting it out would preserve
  a measured data race.
- **`Raw` takes a scan callback** (§3.7). It is the other API that let a result set escape, and a
  callback closes it by construction.
- **No goroutine-id detection, no third mutex** (§6.1).
- **`QueryFunc` adoption is deferred** (§6.4). Not needed for safety once the reservation is in the
  methods, and not worth an 82-site diff on its own.

What this deliberately does **not** buy: speed (§3.6), and constrained use of `Querier(ctx)` or
`database.FromContext` (§6.3) — both stay covered by the reservation but unconstrained in what a
caller may do with the rows.

### 5.2 Sequencing

The first draft needed a lint gate and a 47-site ratchet before the lock could land. It does not any
more — the ordering below is just dependency order, and steps 1–3 are one reviewable change.

1. **Untangle `tx.mu` from the connection statements** (§3.4): `Begin`, `Commit` and `Rollback`
   currently hold `tx.mu` across their `Exec`/`Commit`/`Rollback`, which makes `IsClosed()` — the
   head of every nested read — wait on a network round trip. Snapshot under `tx.mu`, release, run
   the statement, re-take. Worth doing on its own, but note it is **no longer a precondition**:
   without bookkeeping to guard (§3.1), nothing on the reservation path takes `tx.mu`, and §3.4's
   teardown acquire cannot block, so §2.4's second deadlock has no way to form.
2. **Fix `database/stdlib`'s `rows.Next()` to close on exhaustion** (§2.11). Three lines, and a
   precondition for step 3 — without it release-on-drain is unsound for multi-result-set statements
   on MySQL.
3. **Add `connSem`, `txRows`, `txRow`** (§3.1–§3.3), with `Next()` releasing on exhaustion. This is
   the design, and it is one struct field plus two small wrapper types.
4. **Make teardown non-blocking** (§3.4).
5. **Add `Stream`'s in-transaction guard and the `AllowInTransaction` opt-in** (§6.2) in the same
   change as step 2, since between those two steps it is the one generated path whose behavior
   regresses. Mirrors `shared/_lock_mode_guard.tmpl`; needs the [§9.4a](../../PRD.md) amendment first,
   including the new `CallOptions` field.
6. **Remove FIX-221's `g.SetLimit(1)`** from `table/get.go.tmpl` and revert §13.2 step 4's caveat.
   The fan-out becomes unbounded again and the lock serializes it — which is faster than the bound,
   since the loads pipeline against the lock rather than waiting on `errgroup` slots.
7. **Change `Raw` to take a scan callback** (§3.7). Independent of steps 1–6 and reviewable on its
   own, but it needs a PRD amendment first: the signature is printed three times — twice in
   [§20.2](../../PRD.md) (the method listing, and the "Raw Database Access" prose with its worked example
   and hook table) and once in [§18.6](../../PRD.md)'s "Raw Queries and Hooks". Blast radius:
   `client.go.tmpl`, the golden
   `unified_client_gen.go`, `gen/unified_client_test.go`, `gen/tenancy_template_test.go`, and the
   regenerated `client_gen.go` of all eleven example trees.
8. **Work through §4.5's documentation table** — [§18.5](../../PRD.md), [§20.5](../../PRD.md), [§20.2](../../PRD.md), [§9.4a](../../PRD.md), `Tx`'s doc comment and the generated ones — to promise what is
   then true: concurrent use of a `txCtx` is safe and serialized. Restore the §20.5 errgroup
   example, now correct. §18.5's caller-serializes rule narrows to the doors §4.3 leaves open —
   `Querier(ctx)`, `database.FromContext`, and writing inside a `Stream` loop.
9. **Keep `relationship_tx_fanout_test.go`** — it must pass before and after, bounded or not — and
   add the §2.8 leak probes as regression tests, since they are the only coverage of the paths that
   used to hang. Add one that fans two `Raw` reads out over a single `txCtx`, which is the case
   §3.7 exists to make safe.

Steps 5, 6 and 7 are the only ones that change generated output: step 5 adds `Stream`'s guard and
option, step 6 deletes three lines per `loadRelationships` body, step 7 rewrites one method per
client.

---

## 6. Decisions and open questions

1. **No goroutine-id self-deadlock detection. — Decided 2026-09-21.** A variant was built and
   measured (§2.8) that recorded the holding goroutine's id, so a goroutine blocking on a
   reservation it already held got an immediate named error instead of waiting. It is dropped. The
   cost was a `runtime.Stack` traceback per transactional `Query`/`QueryRow` (~1.8µs, §2.9) plus a
   third mutex and two fields on `Tx` to hold what it compared against — and `runtime.Stack` is a
   traceback pressed into service as a goroutine-local, which can also misattribute after an id is
   reused. What it bought was a faster diagnosis of one misuse shape, not a different outcome:
   §4.2's first row degrades from an immediate error to a wait bounded by `ctx` or
   `TxOptions.Timeout`, and is unbounded only when the caller set no deadline at all.

   Two things make that trade cheaper than it first looks. `Raw` taking a scan callback (§3.7)
   closes the widest path into that row. And §3.1 gets materially simpler: with no bookkeeping to
   guard there is no third lock, nothing on the reservation path touches `tx.mu`, and §2.4's second
   deadlock becomes unconstructible rather than merely avoided.

   Reversible if the misuse shows up in practice — and §4.2 notes a cheaper half-measure
   (`atomic.Pointer[string]` naming the in-flight statement, no mutex, no traceback) if the error
   text alone turns out to be what was wanted.

   **One fact found after the decision, which should be weighed against it (§4.2).** The reassurance
   "bounded by `ctx` or `TxOptions.Timeout`" is conditional on the caller having set one, and
   **neither is set by default** — [PRD §18.2](../../PRD.md) makes zero-means-no-timeout an explicit
   design choice. So the unbounded case is the *default* case, not an edge. Two things still narrow
   it: §3.7's `Raw` callback and §6.2's `Stream` guard remove the two widest paths to a self-hold,
   leaving a consumer who holds rows open through `Querier(ctx)` — documented misuse (§6.3). The
   decision stands unless that residual is judged too sharp, in which case the goroutine-id variant
   is the measured fix.

2. **`Stream` refuses a transaction by default, with an explicit opt-in. — Decided 2026-09-21.**
   (§2.10, §4.6) Supersedes the earlier "opt out of the reservation" proposal, which the measurements rule
   out. Three modes were considered:

   | Mode | (A) write inside the loop | (B) concurrent reader on the `txCtx` | Snapshot scan in a tx | Needs §6's bypass door |
   |---|---|---|---|---|
   | Hold the reservation, always | **hangs** (§2.10) | safe, 0 races (§2.10) | works | no |
   | Opt *out* of the reservation | `conn busy` | **13 data races** (§2.10) | works | yes |
   | **Refuse by default, opt *in* to holding the reservation** | refused; hangs only if opted in | refused; safe if opted in | works when opted in | no |

   The middle row is the one to discard: opting `Stream` out of the reservation *preserves a known
   data race*. Whatever else is decided, an opted-in `Stream` must **hold** the reservation, not
   bypass it.

   **The guard.** [PRD §9.6a](../../PRD.md)'s `shared/_lock_mode_guard.tmpl` is the precedent — a
   generated method refusing a context it cannot serve, via `database.InTransaction`:

   ```go
   if options.LockMode != sql.LockNone {
       if !database.InTransaction(ctx) {
           return nil, fmt.Errorf("%s: LockMode requires an active transaction", op)
       }
   ```

   `Stream` is the mirror image, surfaced through `yield` because it returns an iterator:

   ```go
   if database.InTransaction(ctx) && !options.AllowInTransaction {
       yield(nil, fmt.Errorf("stream %s: unsupported inside a transaction — use Connection in a loop, "+
           "or set AllowInTransaction for a snapshot-consistent stream that issues nothing else on this txCtx", table))
       return
   }
   ```

   **The opt-in.** A `CallOptions` field, which is where the analogous knob already lives:
   `CallOptions[FO]` (`shared_types_gen.go:16`) already carries fields meaningful on only a subset
   of methods — `SkipEvents` on mutations, `LockMode` on reads, the latter guarded exactly this way.
   Like `LockMode` and `FieldOptions`, it should **not** propagate through `nestedChildOptions`.

   What it recovers is the single cost the bare guard carries: a caller who wants a
   snapshot-consistent scan under `RepeatableRead`/`ReadOnly` and will issue no other statement. That
   is a real workload — §4.6's alternative, `Connection` in a loop, gives the same snapshot but pays
   a `COUNT` plus keyset conditions per page, the overhead [§9.4a](../../PRD.md) says to choose `Stream`
   to avoid.

   What it costs is almost nothing to build: **the guard is the entire change.** An opted-in
   `Stream` takes no special path — it is the ordinary §3.2 read, holding the reservation for the
   iteration exactly as §2.10 measured.

   Two honest limits. The opt-in **cannot verify the promise** — nothing can detect "will you issue
   another statement" — so the obligation is documentary, and a caller who breaks it meets §2.10's
   (A): a hang bounded only by `ctx`/`TxOptions.Timeout`, which also stalls pool teardown. And it is
   a new `CallOptions` field, which CLAUDE.md rule 1 puts outside this document's authority: it
   needs a [§9.4a](../../PRD.md) amendment, alongside the one the guard already requires. **The field is
   agreed; the amendment is the gate on writing it.**

3. **`Querier(ctx)` stays as it is; the rule is restated at §20.2. — Decided 2026-09-21.** (§3.7,
   §4.3) `Querier(ctx)` (`client.go.tmpl:291`) is the remaining hand-out of a live `Querier`, and
   `database.FromContext(ctx)` is the same surface one layer down. [PRD §20.2](../../PRD.md) specifies the
   former deliberately as "direct access… full control over the statement".

   **It is covered.** Inside a transaction the returned `Querier` *is* the `*database.Tx`, so every
   statement through it takes the reservation and concurrent use is as safe as through the generated
   methods. There is no safety gap here.

   The residual is liveness and diagnosability, and it is not specific to this API — it is §4.2's
   first row reached through it. Because the method hands back a live `Rows`, this stays spellable:

   ```go
   q := client.Querier(ctx)                       // inside a tx: the *database.Tx
   rows, err := q.Query(ctx, "SELECT …")          // reservation taken, held until drain or Close
   // … rows neither drained nor closed …
   users, err := client.Users().GetMany(ctx, …)   // waits on the reservation this goroutine holds
   ```

   The statement is correctly *prevented* — that is the reservation working. What it cannot do is
   end the wait, because the waiter is the holder; §6.1 is the decision not to spend a traceback
   detecting that. `Raw` after §3.7 cannot be spelled this way at all, which is the difference
   between an API that is covered and one that is also constrained.

   **Decision: leave the API alone and restate the rule where the escape hatch is specified.**
   [§20.2](../../PRD.md) gains §18.5's drain-or-close rule and the changed symptom — the misuse waits now
   rather than reporting `conn busy`, and with no default timeout (§4.2) that wait is unbounded
   unless the caller sets one. Narrowing the contract was the alternative and is rejected: full
   control over the statement is the method's stated purpose, and full control includes the ability
   to get it wrong. §4.5's table carries the wording change.

4. **Adopt `QueryFunc` in the templates, and when?** (§4.4, §6.4) Two separate questions that share
   a name, now settled differently:

   - **For consumers — yes, recommend it now** (§3.8). `QueryFunc` is already exported, already
     correct, and is the answer for anyone reaching for `Querier(ctx)`. It costs a doc paragraph.
     `QueryRowFunc` needs the one-line fix in §3.8 first; `ExecFunc` should not exist.
   - **For the generated call sites — deferred** (§6.4). Not needed for safety once the reservation
     is in the methods, and measured unnecessary: §2.6 ran eleven suites with zero conversions.
     Generated code cannot block on itself in the first place, because every site drains before
     issuing its next statement (§2.5). An 82-site diff across eleven golden trees buys nothing
     here; pick it up per-template if one is ever open for another reason.

5. **The fan-out bound is removed. — Decided 2026-09-21.** `g.SetLimit(1)` goes with step 6 and
   does not come back as a belt-and-braces. It is redundant once the reservation exists — and
   removing it is the better of the two options for coverage, because it means the generated tree
   exercises the reservation on every multi-edge read inside a transaction rather than never
   reaching it. §2.6 ran unbounded throughout, which is the evidence that it works.

6. **Release-on-drain needs a one-line stdlib adapter fix. — Probed and resolved 2026-09-21.**
   (§2.5, §2.11) It does *not* hold for multi-result-set statements as the adapter stands. Measured,
   confirmed, fixed and re-measured — §2.11 carries the numbers. The fix is three lines in
   `database/stdlib`'s `rows.Next()`, it is a **prerequisite of this design rather than an
   independent bug fix**, and pgx needs no equivalent.

7. **What about pipelining?** The only option that actually reduces latency inside a transaction is
   collapsing round trips — `pgx.Tx.SendBatch` (`tx.go:140`, `pgxpool/tx.go:50`) sends every edge
   query in one round trip with no goroutines at all. `database/sql` has no equivalent, so MySQL and
   SQLite would fall back regardless, and the loader would have to stop delegating to each target's
   `GetMany` (which is what applies that table's soft-delete default, tenancy, access rules and
   hooks). Strictly larger than this document, and orthogonal to it — the lock is about safety,
   batching about speed. Noted so the option is not lost.
