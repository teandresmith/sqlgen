Drive an implementation phase to completion hands-off: each remaining sub-item runs implement → verify → resolve its fixes → done in its own subagent, the orchestrator checks and commits it, and the phase closes the same way.

## Instructions

The user wants to finish Phase `$ARGUMENTS` (e.g., `36`, `36 --from 36.3`, `36 --dry-run`).

`/finish-phase` is the autonomous driver over `/implement`, `/verify`, `/fix-implement`, `/done` and `/close-phase`. It does not re-implement them. The main thread is an **orchestrator**: it picks the next sub-item, delegates the work to a fresh subagent, checks the result cheaply, and commits. It never implements, verifies or closes anything itself. Its context holds one short report per sub-item, so a long phase does not fill the main window or compact mid-sub-item.

**Autonomy policy.** Run without pausing between sub-items. Stop only for a question the subagent cannot settle (Step 5) or a failure (Step 7). Never fake a green and never gate off a broken case to keep moving (project rule: stop and ask on blockers, never suppress).

**Sub-items are not fixes.** `/fix-loop` parks a fix with a question and moves on. Here the next sub-item usually depends on the one asking, so a question stops the run at that sub-item. The orchestrator asks the user, then hands the answer back to **the same subagent** with `SendMessage`, so it continues with its context intact.

**The loop keeps no state of its own.** Between sub-items the working tree is clean at a commit. The tracker and `git log` are the whole state, so resuming after any stop is just running `/finish-phase N` again (or `--from N.X`).

### Step 0: Parse arguments

Split `$ARGUMENTS` on whitespace. The first token is the phase number `N`. Recognize these flags anywhere in the rest:

- `--dry-run` — print the plan (Step 2) and stop. Invoke nothing, edit nothing.
- `--from N.X` — start at sub-item `N.X`. Every earlier sub-item must already be `Complete`; Step 2 checks.
- `--max-fix-iterations K` — cap the verify ↔ fix rounds per sub-item at `K` (default `3`). Passed to the subagent. Exceeding it returns `BLOCKED`.
- `--context-limit P` — stop at the next sub-item boundary once the main thread's context is at or above `P`% (default `70`). See Step 3.
- `--no-close` — stop after the last sub-item instead of running `/close-phase`.

### Step 1: Preflight

Stop and report if any of these fails. Do not work around them.

- **Clean tree.** `git status --porcelain` must print nothing, so each sub-item's commit holds only its own work. Suggest the user commit or stash first.
- **Branch.** Commit on whatever branch is checked out; never create or switch branches. A detached HEAD fails preflight. Name the branch in the plan so the user sees where the work lands.
- **Docker.** `docker info` must succeed when any remaining sub-item can trip `/implement`'s path-based `check-examples` table, and always before closure (`make conformance` needs it).
- **Tool versions.** Run `/implement` Step 3.5's check. It warns and does not block.
- **Notifications.** Load `PushNotification` with ToolSearch. If it is unavailable, say so once and continue.

### Step 2: Resolve the plan

Read `docs/tracker/STATUS.md`, `docs/tracker/phase-N.md` and the phase's entry in `docs/tracker/IMPLEMENTATION_ORDER.md`. Read only what the plan needs: the sub-item headings, their `**Status:**` lines and their "Depends on" notes. Don't read the sub-items' bodies; the subagents do.

1. **Remaining sub-items:** every sub-item not `Complete`, in ascending order, from `--from` if given. A sub-item whose dependency is not `Complete` and not earlier in this list is a blocker.
2. **Inherited open FIXes:** pull the open entries' metadata the way `/fix-loop` Step 2 does, without reading `fixes.md` whole. Record every open FIX whose origin is phase `N`. Each must clear before closure; list them in the plan.
3. **Caveats:** surface any per-sub-item churn-watch or freeze notes in `phase-N.md`'s header, so they are visible if they turn into a blocker.

Print the plan: the branch, the sub-item order, the inherited FIXes and the caveats. `--dry-run` stops here.

### Step 3: Boundary checks (before each sub-item, and before closure)

Stop cleanly, with the tree clean, when any of these holds:

- **Context limit.** The statusline writes the main session's usage to `~/.claude/state/context-<session_id>.json` (`{"used_percentage": N}`). This session's ID is the UUID directory segment of the scratchpad path in the system prompt. If the file exists and `used_percentage >= P`, stop and tell the user to start a fresh session and run `/finish-phase N`: it resumes from the tracker. If the file does not exist, note once in the final report that the guard did not run.
- **Nothing left.** Go to Step 6.

### Step 4: Run one sub-item in a subagent

Launch one subagent per sub-item, then wait for its completion notification and do nothing else meanwhile. The loop is strictly sequential: sub-items depend on each other and regenerate shared goldens.

```
Agent(
  subagent_type: "general-purpose",
  description: "Finish phase: N.X",
  prompt: <the prompt below, with N, N.X, {sha} and {K} (--max-fix-iterations) filled in>
)
```

Record the agent's ID; Step 5 continues it with `SendMessage`.

Subagent prompt:

> You are completing sub-item N.X of `docs/tracker/phase-N.md` in the sqlgen repo at HEAD `{sha}`, as one step of `/finish-phase`. The user is not watching and **you cannot ask them anything**. Wherever a skill below would stop and ask (including `AskUserQuestion`), stop instead and return `NEEDS_INPUT` with the question. The orchestrator will ask the user and send you the answer; then continue from where you stopped.
>
> 1. **Implement.** Invoke the Skill tool with `implement` and args `N.X`. Do not pass `--skip-review`; the auto-review is a gate. Follow it through its report step. Apply its reviewer's Tracker & Doc Accuracy items in place.
>    - When `make check` and any path-triggered `check-examples` are green, run `git add -A --intent-to-add` and record `git diff HEAD | shasum` as the **gate fingerprint**.
> 2. **Verify.** Invoke the Skill tool with `verify` and args `N.X`. One override, at its Step 7: if `git add -A --intent-to-add && git diff HEAD | shasum` still equals the gate fingerprint, reuse step 1's `make check` and `check-examples` results instead of re-running them, and say so in the report. If it differs, run them.
> 3. **Resolve this sub-item's fixes.** List every open FIX attributable to N.X, of every severity. For each round, resolve every one with the Skill tool (`fix-implement`, args `FIX-NNN`, no `--skip-review`), then re-run `verify N.X`. Repeat until a verify logs no new FIX and none stays open for N.X. More than {K} rounds → `BLOCKED`.
>    - A FIX that needs a design decision rather than a mechanical correction → `NEEDS_INPUT`. Do not choose.
>    - A FIX that hard-depends on an artifact a later sub-item builds → `NEEDS_INPUT`, so the user decides whether to reroute it.
> 4. **Questions.** A Question to Adjudicate, a disagreement with the design note or the PRD, a golden change you can't attribute to this sub-item's tasks, or any other design decision: if the recommended option meets `/fix`'s **auto-accept** rule, apply it, record it in the Completion Record, and continue. Otherwise → `NEEDS_INPUT`.
> 5. **Done.** Once N.X has zero open FIXes and `make check` is green, invoke the Skill tool with `done` and args `N.X`.
> 6. **Rules.** Do not commit. Do not edit another sub-item's section or any FIX entry not attributable to N.X. Triage other findings per `/fix` → **Findings triage**: inline, a `docs/tracker/backlog.md` line, or the report's `candidate FIX` line. Never file a FIX entry beyond what `/verify` logs for N.X. Leave no `zz_probe_*` files and no probe directories. Never gate off a broken case to reach green.
> 7. **Return exactly this report**, at most ~25 lines, and nothing else:
>
> ```
> N.X
> status: DONE | NEEDS_INPUT | BLOCKED | FAILED
> commit: <type>(<scope>): <subject>
> body: <2-4 lines: what changed and why, no phase or sub-item IDs in the subject>
> files: <source files changed; generated dirs summarized as example/{models,expected}: n files>
> fail-first: <new tests seen failing before the change, and the guards seen passing> | n/a
> gates: make check → <ok|fail>; check-examples → <ok|fail|not triggered>; verify gates → <re-run|reused>
> golden churn: <examples and what moved> | none
> review: <reviewer findings and how each was handled>
> verify: <PRD n/n, tests n/n, reviewer agreement>
> fixes: <FIX-NNN severity → resolved, per round> | none
> doc corrections applied: <n> | none
> questions: <question — options — measured consequence of each — recommendation> | none
> auto-accepted: <question → option, which conditions held> | none
> blocker: <step, command, exact failing output excerpt> | none
> backlog: <n> line(s) appended | none
> candidate FIX: <measured failure — command → output> | none
> ```
>
> Commit type per `guidelines/CI.md`: `feat` / `fix` when production behavior changes, `test` for test-only, `docs` for doc-only (a PRD-sync sub-item is `docs(prd)`). Scope per recent `git log`.

### Step 5: Act on the report

**`DONE`**: check cheaply, then commit. Do not trust the report blindly:

1. `git status --porcelain` is non-empty, and every changed path is covered by the report's `files` line, or is a tracker file (`phase-N.md`, `STATUS.md`, `fixes.md`, `backlog.md`) the report accounts for. An unexplained path is `FAILED`.
2. `make tracker-check` and `make req-check` pass.
3. Sub-item N.X's `**Status:**` in `phase-N.md` is `Complete` (grep the section's lines, not the whole file).
4. No open FIX is attributable to N.X, and no `zz_probe_*` file exists.
5. Commit everything as one commit, with the report's subject and body, ending with the Co-Authored-By trailer this session's attribution guidance specifies. The subagent's `/verify` already ran, which satisfies the verify-before-commit rule.

Then go to Step 3 for the next sub-item.

**`NEEDS_INPUT`**: ask, then continue the same subagent:

1. `PushNotification`: "N.X needs you: <one-line question>."
2. Ask the report's questions with `AskUserQuestion`, at most four per call, recommended option first with "(Recommended)", each option with its measured consequence.
3. `SendMessage` the answers to the subagent recorded in Step 4: "The user ruled: <answers>. Record each ruling in N.X's Completion Record and continue from where you stopped. Return the same report format."
4. Wait for its next report and act on it here again. If the user defers a question instead of answering, stop: leave the work in place, report where it stands, and tell the user to resume with `/finish-phase N --from N.X`.

**`BLOCKED` or `FAILED`**: go to Step 7.

### Step 6: Close the phase

Skip with `--no-close`. Otherwise run the Step 3 boundary checks, confirm the inherited FIXes from Step 2 are resolved, and launch one more subagent:

> You are closing Phase N of the sqlgen repo at HEAD `{sha}`, as the last step of `/finish-phase`. You cannot ask the user anything. Invoke the Skill tool with `close-phase` and args `N`. Wherever it asks the user (FIX carry-forward, backlog review, accepting an untested or not-run row, a design-doc status flip), stop and return `NEEDS_INPUT` with the questions, then continue when the answers arrive. Do not commit. Return: status, the sweep results with timings, the conformance totals, the FIX and backlog triage, the files changed, and the two commit subjects (`docs(conformance): regenerate the report` for `REPORT.md`, and `docs(tracker): close phase N` for the rest).

Act on its report as Step 5 does: relay any questions through `SendMessage`, check that every changed path is explained and `make tracker-check` passes, then commit `docs/conformance/REPORT.md` and the rest as the two commits the report names.

### Step 7: Stop on a blocker

Stop for:

- `BLOCKED` or `FAILED` from a subagent.
- Any Step 5 `DONE` check that fails.
- A red `make tracker-check` or `make req-check`.
- Docker or tool failures.

**Do not reset the tree.** Leave the subagent's work in place so the user can inspect or finish it, with the sub-item `In Progress`. `PushNotification` the blocker, then report: which sub-item, which step, the exact failing command and output, and the options you see. Once the user clears it, `/finish-phase N --from N.X` resumes; preflight will insist on a clean tree.

### Step 8: Final report

- **Committed:** a table `Sub-item | commit | type(scope) | notes`, with notes only for something non-obvious (a golden churn beyond the plan, reused gates, a fix round, an auto-accepted ruling).
- **Rulings:** each question the user answered this run, by sub-item.
- **Auto-accepted:** every question a subagent decided under `/fix`'s auto-accept rule, so the user can veto it.
- **FIXes:** logged and resolved, per sub-item. Carry-forward should be empty; call out any exception.
- **Backlog:** how many lines this run appended (already committed).
- **Candidate FIXes:** the measured, user-visible failures the subagents reported. Ask whether to file each with `/fix add`, or, while `backlog.md` says `Queue freeze: on`, append each tagged `[measured]` and commit it. **Never file automatically.**
- **Closure:** the `/close-phase` outcome (sweep, conformance totals, docs synced), or why it was skipped.
- **Context guard:** whether it ran, and the last usage reading.
