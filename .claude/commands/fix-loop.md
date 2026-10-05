Drain the vetted fix queue hands-off: implement → verify → commit one fix at a time, each in its own subagent, parking design questions and asking them in one batch at the end.

## Instructions

The user wants to run the fix loop with `$ARGUMENTS` (e.g., empty, `--max 5`, `--only FIX-231 FIX-233`, `--dry-run`).

`/fix-loop` is the autonomous driver over `/fix-implement`, `/verify` and `/fix-vet`. It does not re-implement them. The main thread is an **orchestrator**: it picks the next fix, delegates the work to a fresh subagent, checks the result cheaply, and commits. It never implements a fix itself, so its context holds one short report per fix and a long run does not compact mid-fix.

**Autonomy policy.** Run without pausing between fixes. Questions never stop the loop: the fix is **parked** and the loop moves on, and every parked question is asked in one batch when the queue is empty. Only failures stop the loop (Step 7). Never fake a green and never gate off a broken case to keep moving (project rule: stop and ask on blockers, never suppress).

**The loop keeps no state of its own.** After every step the working tree is clean at a commit: a fix is committed, a park is committed, or the loop has stopped. The tracker and `git log` are the whole state, so resuming after any stop is just running `/fix-loop` again.

### Step 0: Parse arguments

Recognize these flags anywhere in `$ARGUMENTS`:

- `--dry-run` — print the ranked plan (Step 2) and stop. Invoke nothing, edit nothing.
- `--max N` — commit at most `N` fixes this run (default `10`).
- `--only FIX-NNN [FIX-MMM …]` — restrict the queue to these entries. They must still be eligible.
- `--context-limit P` — stop at the next fix boundary once the main thread's context is at or above `P`% (default `70`). See Step 3.
- `--skip-final-sweep` — skip Step 8's `make check-examples` + `make test-integration`. Use only when the user accepts the gap.

### Step 1: Preflight

Stop and report if any of these fails. Do not work around them.

- **Clean tree.** `git status --porcelain` must print nothing. The loop commits `docs/tracker/fixes.md` with every fix, so uncommitted tracker edits (for example an uncommitted `/fix-vet` run) would be swept into the first fix's commit. Suggest the user commit them first.
- **Branch.** Must be `main`. This repo commits fixes directly to main.
- **Docker.** `docker info` must succeed. Nearly every fix trips the path-based `check-examples` trigger.
- **Tool versions.** Run `/fix-implement` Step 3.5's check. It warns and does not block.
- **Notifications.** Load `PushNotification` with ToolSearch. If it is unavailable, say so once and continue; the loop still works, it just cannot ping the user.

### Step 2: Rank the queue

Never Read `docs/tracker/fixes.md` whole: it is ~18k lines. Pull only the metadata bullets from the open region:

```bash
awk '/^<!-- fixes:open:begin -->$/{o=1;next} /^<!-- fixes:open:end -->$/{o=0}
     o && /^### FIX-/{id=$2}
     o && id && /^- \*\*(Severity|Vetted):\*\*/{print id": "$0}' docs/tracker/fixes.md
```

A fix is **eligible** when all of these hold:

1. Its `**Vetted:**` bullet is `ready` or `corrected`.
2. `.claude/prototypes/FIX-NNN.patch` exists.
3. The patch's **source** hunks still apply. Generated files churn with almost every fix, and `/fix-implement` regenerates them rather than trusting the patch, so exclude them from the check:

   ```bash
   git apply --check \
     --exclude='cmd/sqlgen/testdata/examples/*/models/*' \
     --exclude='cmd/sqlgen/testdata/examples/*/expected/*' \
     --exclude='*.golden' \
     .claude/prototypes/FIX-NNN.patch
   ```

   A patch whose source hunks no longer apply is **stale**: not eligible, listed in the report as needing `/fix-vet FIX-NNN`.

Everything else is not eligible and is listed with its reason: unvetted, `needs ruling`, stale, or out of `--only`.

**Order** the eligible fixes by:

1. Severity: `blocking` → `tracked` → `nit`.
2. Fewest source files shared with the other eligible patches (same exclusions as above). Landing an isolated fix first keeps the others' patches applying.
3. Ascending ID.

Print the plan: the ranked eligible list, and every open entry left out with its reason. `--dry-run` stops here.

**Re-rank after every commit and every park.** A landed fix can make another patch stale.

### Step 3: Boundary checks (before each fix)

Stop cleanly, with the tree clean, when any of these holds. Go to Step 8 first if at least one fix was committed this run.

- **`--max` reached.**
- **Context limit.** The statusline writes the main session's usage to `~/.claude/state/context-<session_id>.json` (`{"used_percentage": N}`). This session's ID is the UUID directory segment of the scratchpad path in the system prompt. If the file exists and `used_percentage >= P`, stop and tell the user to start a fresh session and run `/fix-loop` again: it resumes from the tracker. If the file does not exist, note once in the final report that the context guard did not run.
- **Two parks in a row.** The queue is weaker than it looks. Skip ahead to Step 6 and ask the questions now.
- **Queue empty.** Go to Step 8, then Step 6.

### Step 4: Run one fix in a subagent

Launch one subagent per fix. Wait for its completion notification and do nothing else in the meantime: the loop is strictly sequential, because almost every fix regenerates the shared `expected/` goldens.

```
Agent(
  subagent_type: "general-purpose",
  description: "Fix loop: FIX-NNN",
  prompt: <the prompt below, with FIX-NNN and {sha} filled in>
)
```

Subagent prompt:

> You are implementing FIX-NNN in the sqlgen repo at HEAD `{sha}` as one step of `/fix-loop`. The user is not watching and **you cannot ask them anything**. Wherever a skill below would stop and ask, stop and return `NEEDS_INPUT` instead.
>
> 1. **Implement.** Invoke the Skill tool with `fix-implement` and args `FIX-NNN --skip-review`. Follow it through Step 7 (resolve). Keep `.claude/prototypes/FIX-NNN.patch` until step 3 below; the reviewer compares against it.
>    - After Step 6, compare the golden churn with the entry's recorded churn (its Prototype results bullet). If different examples or files churned, stop: return `NEEDS_INPUT` asking whether the extra churn is intended, with the diff summary. A difference wholly explained by a fix that landed after the vet (name its commit, and show the extra churn is that fix's surface meeting this one) is not a question: report it under `golden churn` and continue.
>    - When the gates are green, run `git add -A --intent-to-add` and record `git diff HEAD | shasum` as the **gate fingerprint**.
> 2. **Verify.** Invoke the Skill tool with `verify` and args `FIX-NNN`. Two overrides:
>    - Step 7: if `git add -A --intent-to-add && git diff HEAD | shasum` still equals the gate fingerprint, reuse step 1's `make check` and `check-examples` results instead of re-running them, and say so in the report. If it differs, run them.
>    - Step 7b: add to the reviewer's prompt: "Compare the landed diff against the vetted prototype at `.claude/prototypes/FIX-NNN.patch` and explain every divergence. Also check: the bug is no longer reproducible, a regression test exercises the bug shape, and no callers, sibling templates or dialect parallels broke."
> 3. **Act on the verification.**
>    - Apply **Tracker & Doc Accuracy** items in place and fix nits inline.
>    - Triage every other finding, yours or the reviewer's, per `/fix` → **Findings triage**: fix inline-bucket items in this change; append each backlog-bucket item to `docs/tracker/backlog.md` as one line in the required format; report candidate FIXes (measured, user-visible failures) in the report. Do not widen the fix past `/fix`'s scope cap.
>    - A FAIL, PARTIAL, regression, or high-confidence reviewer finding that has a mechanical fix: fix it, re-run `make check` and any path-triggered `check-examples`, then re-run `/verify FIX-NNN` **once**. Still failing → `BLOCKED`.
>    - A **Question to Adjudicate**, a disagreement with the prototype or the PRD, or any design decision: if the recommended option meets `/fix`'s **auto-accept** rule, apply it, record it under the entry's Suggested fix as that rule says, and continue. Otherwise → `NEEDS_INPUT`. Do not choose.
>    - Once verification is clean, delete `.claude/prototypes/FIX-NNN.patch`.
> 4. **Rules.** Do not commit. Do not edit any other FIX entry. Never file a FIX entry; the backlog and the report's `candidate FIX` line are the only outlets. Leave no `zz_probe_*` files. Never gate off a broken case to reach green.
> 5. **Return exactly this report**, at most ~20 lines, and nothing else:
>
> ```
> FIX-NNN
> status: DONE | NEEDS_INPUT | BLOCKED | FAILED
> commit: <type>(<scope>): <subject> (FIX-NNN)
> body: <1-3 lines: root cause and what changed>
> files: <source files changed; generated dirs summarized as example/{models,expected}: n files>
> prototype: applied clean | applied 3way | stale, rebuilt from Prototype results
> gates: make check → <ok|fail>; check-examples → <ok|fail|not triggered>; verify gates → <re-run|reused>
> golden churn: matches vet | differs: <how>
> verify: <PRD n/n, tests n/n, reviewer agreement, findings fixed inline>
> doc corrections applied: <n> | none
> questions: <question — options — measured consequence of each — recommendation> | none
> blocker: <step, command, exact failing output excerpt> | none
> auto-accepted: <question → option, which conditions held> | none
> backlog: <n> line(s) appended | none
> candidate FIX: <measured failure — command → output> | none
> ```
>
> Commit type: `fix` when production behavior changes, `test` for test-only, `docs` for doc-only. Scope per `guidelines/CI.md` and recent `git log` (for example `gen`, `config`, `sql`, `database`, or an example name like `graphql` for example-test-only fixes).

### Step 5: Act on the report

**`DONE`** — check cheaply, then commit. Do not trust the report blindly:

1. `git status --porcelain` is non-empty, and every changed path is covered by the report's `files` line, or is `docs/tracker/backlog.md` with the report's `backlog` count matching the lines added. An unexplained path is a `FAILED`.
2. `make tracker-check` passes.
3. The entry's `**Status:**` is `Resolved` (grep the entry's lines, not the whole file).
4. `.claude/prototypes/FIX-NNN.patch` is gone, and no `zz_probe_*` file exists.
5. Commit everything as one commit, with the report's subject and body, ending with the Co-Authored-By trailer this session's attribution guidance specifies:

   ```bash
   git add -A && git commit -F - <<'EOF'
   <type>(<scope>): <subject> (FIX-NNN)

   <body>

   Co-Authored-By: …
   EOF
   ```

Then re-rank (Step 2) and continue (Step 3).

**`NEEDS_INPUT`** — park the fix:

1. Save the work in progress, new files included, but not the tracker:

   ```bash
   git add -A --intent-to-add
   git diff HEAD -- docs/tracker/backlog.md > .claude/prototypes/FIX-NNN.backlog.patch
   git diff HEAD --binary -- . ':(exclude)docs/tracker/fixes.md' ':(exclude)docs/tracker/backlog.md' > .claude/prototypes/FIX-NNN.wip.patch
   git reset -q --hard HEAD && git clean -fd
   if [ -s .claude/prototypes/FIX-NNN.backlog.patch ]; then git apply .claude/prototypes/FIX-NNN.backlog.patch; fi
   rm -f .claude/prototypes/FIX-NNN.backlog.patch
   ```

   The backlog lines survive the reset and go into the park commit. This is safe only because preflight proved the tree clean. `git clean -fd` leaves gitignored `.claude/prototypes/` alone. Keep the vetted `FIX-NNN.patch`; the re-vet replaces it.
2. Edit the entry (it is back in Open after the reset):
   - Replace its `**Vetted:**` bullet with `- **Vetted:** needs ruling at \`<sha>\` (<YYYY-MM-DD>; parked by /fix-loop)`.
   - Under Suggested fix, add the report's questions as bullets headed "Pending the user's ruling", in `/fix-vet` Step 4's shape, followed by: "Parked by `/fix-loop` at `<sha>` on <date>. The work in progress is in `.claude/prototypes/FIX-NNN.wip.patch`."
3. `make tracker-check`, then commit `fixes.md` and any backlog lines: `docs(tracker): park FIX-NNN pending a ruling`.
4. `PushNotification`: "FIX-NNN parked: <one-line question>. The loop continues."

Then re-rank and continue.

**`BLOCKED` or `FAILED`** — go to Step 7.

### Step 6: Rulings

Reached when the queue is empty (after Step 8's sweep) or after two parks in a row.

1. `PushNotification`: "Fix loop needs you: <n> questions."
2. Collect every open entry at `needs ruling` with no ruling recorded after its verdict. Ask with AskUserQuestion, at most four per call, recommended option first with "(Recommended)", each option with its measured consequence.
3. Record each answer in its entry: "The user ruled on <date>: …". A deferred question leaves its entry at `needs ruling`.
4. Invoke `/fix-vet FIX-NNN …` for the ruled entries. It starts from each `.wip.patch` and brings them to `ready` or `corrected`. Commit its tracker edits: `docs(tracker): vet FIX-NNN, FIX-MMM`.
5. Return to Step 2. Run at most **two** rulings rounds per `/fix-loop` invocation; after that, finish with Step 9.

### Step 7: Stop on a blocker

Stop the loop for:

- `BLOCKED` or `FAILED` from a subagent.
- Any Step 5 `DONE` check that fails.
- A red `make tracker-check`.
- Docker or tool failures.

**Do not reset the tree.** Leave the subagent's work in place so the user can inspect or finish it. `PushNotification` the blocker, then report: which fix, which step, the exact failing command and output, and the options you see. Once the user clears it, `/fix-loop` resumes from the tracker (preflight will insist on a clean tree).

### Step 8: Final sweep

Skip if no fix was committed since the last sweep, or with `--skip-final-sweep`.

Per-fix gates are `make check` plus the path-triggered `check-examples`. The full sweep runs once here:

```bash
make check-examples
make test-integration
```

If either fails, find the commit that introduced the failure (`git bisect` over this run's commits, running the failing target) and treat it as a Step 7 blocker naming that commit. Do not revert anything yourself.

### Step 9: Final report

- **Committed:** a table `FIX | commit | type(scope) | notes`, with notes only for something non-obvious (a 3-way or stale prototype, reused gates, doc corrections).
- **Parked:** each FIX with its question and whether it was ruled and re-vetted this run.
- **Not eligible:** each open FIX with its reason (unvetted, stale → `/fix-vet FIX-NNN`, awaiting ruling).
- **Auto-accepted:** every question a subagent decided under `/fix`'s auto-accept rule, with the entry, so the user can veto it.
- **Backlog:** how many lines this run appended to `docs/tracker/backlog.md` (already committed). Do not list them; the file is the list.
- **Candidate FIXes:** the measured, user-visible failures the subagents reported. While `backlog.md` says `Queue freeze: on`, append each to the backlog tagged `[measured]` and commit it (`docs(tracker): backlog findings from /fix-loop`); otherwise ask whether to file each with `/fix add`. **Never file automatically.**
- **Final sweep:** result, or why it was skipped.
- **Context guard:** whether it ran, and the last usage reading.
