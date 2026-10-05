Drive an implementation phase to completion synchronously: implement → verify → resolve blocking fixes → done for every remaining sub-item, then close the phase.

## Instructions

The user wants to finish Phase `$ARGUMENTS` (e.g., `19`) in one synchronous pass. This skill is the autonomous driver over the existing per-step skills (`/implement`, `/verify`, `/fix-implement`, `/done`, `/close-phase`). It does not re-implement their logic — it **invokes each one via the Skill tool** and enforces the gates between them.

**Autonomy policy: autonomous, stop only on blockers.** Run straight through all remaining sub-items and the closure sweep without pausing for a go/no-go between sub-items. Pause **only** when you hit a genuine blocker (see Step 6). Never fake a green, never gate-off a broken case to keep moving (project rule: stop and ask on blockers, never suppress).

### Step 0: Parse arguments

Split `$ARGUMENTS` on whitespace. First token is the phase number `N`. Recognize these flags anywhere in the remaining tokens:

- `--dry-run` — do not invoke any sub-skill or edit any file. Print the resolved plan (Step 2) and stop.
- `--skip-integration` — passed through verbatim to `/close-phase N` in Step 5 (skips `make test-integration`; use only when Docker integration is unavailable and the user accepts the gap).
- `--from N.X` — start at sub-item `N.X` instead of the first incomplete one (resume after an earlier stop). Sub-items before it are assumed already `Complete`; verify that assumption in Step 2 and stop if any earlier sub-item is not `Complete`.
- `--max-fix-iterations K` — cap the verify↔fix loop per sub-item at `K` rounds (default `3`). Exceeding the cap is a blocker (Step 6).

### Step 1: Preflight

Run the same tool-version preflight `/implement` Step 3.5 does (gofumpt / golangci-lint / go version; warn-not-block on a `.tool-versions` / `tools.mk` mismatch, citing the 16.8g drift). Additionally, confirm Docker is available (`docker info`): 19.x closure needs `check-examples` + `test-integration`. If Docker is **not** running and the phase has any sub-item that trips the path-based E2E table or a `/close-phase` integration sweep, that is a blocker (Step 6) — do not silently skip E2E.

### Step 2: Resolve the plan

Read `docs/tracker/STATUS.md` and `docs/tracker/phase-N.md`. Build the ordered work list:

1. **Remaining sub-items:** every sub-item whose `**Status:**` is not `Complete`, in ascending order (respect `--from`). For each, note its "Depends on" from `docs/tracker/IMPLEMENTATION_ORDER.md`; if a dependency is not `Complete`, that is a blocker.
2. **Inherited open FIXes:** run `/fix list`. Record every **open** `blocking` and `tracked` FIX attributable to phase N (including ones logged against already-`Complete` sub-items). These must clear before Step 5.
3. **Churn-watch / freeze notes:** surface any per-sub-item caveats written in `phase-N.md` (e.g. deferred-freeze `conventions` sub-blocks) so they are visible if they turn into a blocker mid-run.

Print the plan: the sub-item order, inherited FIXes, and any caveats. If `--dry-run`, stop here.

### Step 3: Per-sub-item loop

For each remaining sub-item `N.X` in order, run the full cycle and **resolve every FIX that this sub-item's `/verify` surfaces before `/done`** — do not carry a sub-item's own FIXes forward:

1. **Implement.** Invoke `/implement N.X` (no `--skip-review` — the auto-review is a gate, not a nicety). Let it run its own `make check`, path-based `check-examples`, and auto-review subagent. If `make check` or a triggered `check-examples` fails and the fix is not obvious and mechanical, that is a blocker.
2. **Verify.** Invoke `/verify N.X`. It auto-logs FAIL → `blocking`, PARTIAL / MISSING-test → `tracked`, NIT → `nit` FIX entries.
3. **Resolve the sub-item's FIXes (the fix loop).** Run `/fix list` and collect every **open** FIX attributable to `N.X` — **`blocking`, `tracked`, and `nit` alike**. For each round:
   - Resolve **every** open FIX for `N.X` via `/fix-implement FIX-NNN` (no `--skip-review`), regardless of severity. Fix them here, while the context is fresh — nothing a sub-item's `/verify` surfaces is carried past its `/done`, not even nits.
   - Re-invoke `/verify N.X` after the round (a fix can surface a new issue or a regression).
   - Repeat until `/verify N.X` logs **no new** FIXes and none remain open for `N.X`, or the `--max-fix-iterations` cap is hit (→ blocker).
   - A FIX whose resolution requires a genuine design decision (not a mechanical correction) is a blocker — stop and ask rather than guess.
   - Rare exception: a FIX that genuinely cannot be resolved in `N.X` because it hard-depends on an artifact a *later* sub-item builds (e.g. a §31 PRD-wording note when §31 itself lands in the closure sub-item) is a **stop-and-ask blocker**, not a silent carry-forward — surface it so the user decides whether to reroute it.
4. **Done.** Once `make check` is green and `N.X` has **zero open FIXes of any severity**, invoke `/done N.X`. Move to the next sub-item.

Do not batch — fully close one sub-item (implement → verify → resolve its FIXes → done) before starting the next, so a failure localizes cleanly and each sub-item lands with an empty FIX ledger.

### Step 4: Pre-closure FIX sweep (safety net)

Per-sub-item resolution in Step 3 resolves every FIX inline, so this backstop should find nothing. Run `/fix list` once more; any open FIX attributable to phase N here (`blocking`, `tracked`, or `nit`) means Step 3 missed a round — resolve it (`/fix-implement FIX-NNN` → `/verify FIX-NNN`) and note the miss. Nothing is deliberately carried forward.

### Step 5: Close the phase

Invoke `/close-phase N` (append `--skip-integration` if it was passed). Let it run the full `make check` + `make check-examples` + `make test-integration` sweep under `-race`, triage remaining FIXes, freeze versioned artifacts, and update `STATUS.md` + sibling design docs. If it reports an unresolved `blocking`/`tracked` FIX or a failing sweep, that is a blocker.

### Step 6: Stop-and-ask blockers (the only pause points)

Stop, report precisely what happened, and ask the user — do **not** work around it — when any of these occur:

- **PRD / spec ambiguity** where the guidance genuinely underdetermines the implementation (project rule #1: flag, don't guess).
- **A blocking FIX that needs a design decision** rather than a mechanical correction.
- **`make check` / `check-examples` / `test-integration` failure** that is not an obvious, mechanical fix (a genuine regression, a flaky testcontainer after one retry, a golden that needs a human call on whether the change was intentional).
- **Tool / environment failure** (Docker down when E2E is required; a pinned-tool drift that actually changes output).
- **Fix loop exceeds `--max-fix-iterations`** for a sub-item (the fix isn't converging).
- **A churn-watch caveat materializes** (e.g. the deferred schema freeze lands mid-run and reshapes fields a sub-item reads).
- **A dependency is not `Complete`** when a sub-item needs it.

When you stop, state: which sub-item, which step, the exact failure/ambiguity, and the options you see. Preserve all partial progress (a half-finished sub-item stays `In Progress` with its tracker honestly reflecting what's done). The user can restart with `/finish-phase N --from N.X` after resolving.

### Step 7: Final report

When the phase closes cleanly, report:

- Sub-items completed this run (with one-line notes on any non-obvious decisions each made).
- FIXes logged and resolved, per sub-item (every severity resolved inline before its `/done`). Carry-forward should be empty — call out any exception explicitly and why.
- The `/close-phase` outcome: sweep results, version/freeze decisions recorded, docs synced.
- Anything a human should still eyeball (deferred nits, doc-honesty framing, freeze-track follow-ups).

Keep it a tight summary — the per-step skills already wrote the detailed records into the tracker.
