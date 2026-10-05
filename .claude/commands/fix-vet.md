Vet open fix entries before implementation. Fact-check each entry, reproduce the bug, prototype the Suggested fix in a throwaway worktree, prove it fail-first, and record a verdict plus a saved patch that `/fix-implement` can apply directly.

## Instructions

The user wants to vet `$ARGUMENTS` (e.g., `FIX-235`, `FIX-229 FIX-235`, `--all`, `--phase 27`).

`/fix-vet` runs between `/fix add` and `/fix-implement`. Its outputs are **evidence and a verdict on the entry, plus a prototype patch**. It never lands a fix. It does not resolve entries, commit, or edit code outside throwaway worktrees.

Why it exists: a Suggested fix is written from reading, and reading goes stale. Within a day of being filed, FIX-227's Issue named a gap that FIX-225 had already half-closed, and six of its line refs had drifted. Implementing from an unchecked suggestion either lands the wrong fix or stops halfway for a ruling. Vetting moves both discoveries earlier, where they are cheap and can be batched.

### Step 0: Parse arguments

The first token selects the scope:

- `FIX-NNN [FIX-MMM …]` — vet these entries.
- `--all` — every open entry.
- `--phase N` — every open entry whose `**Origin:**` is phase N.

Flags, anywhere in the remaining tokens:

- `--refresh` — also re-vet entries whose verdict is still fresh (Step 2 skips them by default).
- `--jobs K` — parallel vetters in batch mode (default `3`, max `4`). Each example suite starts its own containers, so Docker is the limit, not tokens.
- `--dry-run` — print the resolved work list with each entry's vetting state, then stop.

### Step 1: Preflight

- Run the tool-version check from `/fix-implement` Step 3.5. It warns and does not block.
- Confirm `docker info` succeeds. Almost every reproduction runs an example suite. If Docker is down, stop and say so. Do not fall back to vetting by reading.
- Create `.claude/prototypes/` if it is missing. It is gitignored.

### Step 2: Resolve the work list

Read `docs/tracker/fixes.md`, locating entries as `/fix` → **Locating the sections** describes. Classify each candidate open entry:

| State | Test | Action |
|---|---|---|
| unvetted | no `- **Vetted:**` bullet | vet |
| fresh | `ready` or `corrected` at `<sha>`, `.claude/prototypes/FIX-NNN.patch` exists, and `git diff --quiet <sha> HEAD -- <files the patch touches>` succeeds | skip, unless `--refresh` |
| stale | `ready` or `corrected`, but a file the patch touches changed since `<sha>`, or the patch file is missing | vet, starting from the entry's Prototype results bullet |
| awaiting ruling | `needs ruling`, and no ruling recorded after the verdict | do not vet; carry its questions into Step 5 |
| ruled | `needs ruling`, and a user ruling recorded after the verdict | vet the option the user chose; if `.claude/prototypes/FIX-NNN.wip.patch` exists (a `/fix-loop` park), start from it with `git apply --3way` in the worktree, and delete it once V7 saves the new patch |
| legacy bullet | a one-line `- [ ] **FIX-NNN**` entry | upgrade it to H3 (per `/fix`), then vet |

Order the list: `blocking` → `tracked` → `nit`, then ascending ID. Print it. `--dry-run` stops here.

### Step 3: Vet

- **One entry:** run the **Vetting procedure** below inline, in `git worktree add --detach <scratchpad>/vet-FIX-NNN HEAD`. Remove the worktree when done.
- **More than one:** dispatch one subagent per entry, at most K at once. Send each wave in a single message, then dispatch the next entry as each one finishes:

  ```
  Agent(
    subagent_type: "general-purpose",
    isolation: "worktree",
    description: "Vet FIX-NNN",
    prompt: "Vet FIX-NNN in the sqlgen repo. Follow the **Vetting procedure** section of
    `.claude/commands/fix-vet.md` exactly, steps V1-V8. The main repo is {absolute path}.
    Write the patch to {absolute path}/.claude/prototypes/FIX-NNN.patch. Do not edit
    docs/tracker/*, do not commit, and do not touch the main working tree except for that
    patch file. Return only the V8 report."
  )
  ```

  Subagents must not edit `fixes.md`. Parallel edits to one 16k-line file would collide, so the orchestrator writes every verdict in Step 4.

### Vetting procedure (V1-V8)

**V1. Load.** Read the entry and everything under **References**. Load guidelines using `/fix-implement` Step 2's table.

**V2. Fact-check against HEAD.** Check every `file:line`, identifier, quoted output, PRD quote, commit hash, and claim about which tests exist or what they cover. Record each stale fact with its correction. A wrong fact that changes the fix's scope, such as a gap that is already closed, is a finding, not a typo.

**V3. Reproduce at HEAD.** Measure every behavior claim; do not reason about it. Use a failing test, a throwaway probe test (name it `zz_probe_*_test.go` and delete it before V7), or a generation run. For a config or schema variant, use the example probe harness:
- make a sibling copy of the example under `cmd/sqlgen/testdata/examples/`;
- keep the example's module path;
- remove the copy's `models/` and `expected/` for a clean regen;
- run with `GOWORK=off`;
- delete the copy before running `TestE2EGoldenFiles`.

Record the exact command and its output. If the bug does not reproduce, the verdict is `not reproducible`: go to V7. If the environment stops you from reproducing it (Docker, a tool failure), report that as a blocker. It is not `not reproducible`.

**V4. Prototype.** Implement the Suggested fix as written, under the project rules: the PRD is the source of truth, module boundaries hold, no build tags, the guidelines apply, and the code is gofumpt'd. Include the regression test the entry names.
- If the suggestion is wrong or not enough, prototype the correction and record why. The verdict is `corrected`.
- If the entry is pending a ruling, or you hit a design decision the PRD does not settle, do not choose. Measure each option well enough to state its concrete consequences: which tests or goldens change, and which behavior differs. Prototype the recommended option if that is cheap. The verdict is `needs ruling`, with 1-4 questions.
- **Auto-accept.** A question whose recommended option meets `/fix`'s auto-accept rule is not a question: prototype that option, gate it, and report it under `auto-accepted`. It does not by itself make the verdict `needs ruling`.
- **Scope cap.** Widen the entry only for a case with the same root cause **and** the same fix site (`/fix` → **Findings triage**). Anything else is an adjacent finding: report it, do not prototype it.

**V5. Gates.** Run the narrowest gates that cover the change, using `/fix-implement` Step 5's path table to decide what is touched:
- unit: `go test -race ./<pkg>/...` in the owning module;
- an example: `GOWORK=off go test -race -count=1 ./...` and `GOWORK=off golangci-lint run` in each affected example;
- a template: `make update-golden` and `make update-golden-e2e` in the worktree. Record which examples churn, with files added and changed per example.

The full `make check` and `make check-examples` belong to `/fix-implement`.

**V6. Fail-first.** For each defect site the fix touches:
- revert that one change, keep the new test, and show that the new test fails, quoting the message;
- run the pre-existing suite without the fix and show it passes, which proves the gap is real.

For a test-only fix, delete each production line the test guards instead. Restore the code after every probe.

**V7. Save.** Run `git diff` in the worktree, leaving out probe files, and write it to `<main repo>/.claude/prototypes/FIX-NNN.patch`. Then restore the worktree to clean: `git checkout -- . && git clean -fd`.

**V8. Report.** Return this exact structure. A subagent returns it; inline, it is the input to Step 4.

```
FIX-NNN
verdict: ready | corrected | needs ruling | not reproducible
base: <short sha>
patch: .claude/prototypes/FIX-NNN.patch (<n> files, +a/-b) | none
stale facts:
  - <old> → <new>
reproduction: <command> → <observed output>
prototype: <what changed, 1-4 bullets>
corrections vs. the Suggested fix: <what and why> | none
gates: <command → result, one per line>
fail-first:
  - <site reverted> → <test> fails: "<message>"
  - existing suite without the fix: passes | fails (<which>)
golden churn: <example: +added/~changed, …> | none
questions: <question — options — measured consequence of each — recommendation> | none
auto-accepted: <question → option, which conditions held> | none
backlog: <one line per finding, already in /fix's backlog line format> | none
candidate FIX: <measured, user-visible failure — command → output> | none
```

Triage every adjacent finding per `/fix` → **Findings triage** before reporting it: doc and stale-fact corrections inside this entry go under `stale facts`; same-root-cause cases go into the prototype; everything else goes under `backlog`, except measured user-visible failures, which go under `candidate FIX`. Subagents do not write `backlog.md`; the orchestrator does in Step 4.

A verdict of `ready` or `corrected` requires all three: V3 reproduced, V5 green, and V6 fail-first shown. If any is missing, say which, and use `needs ruling` or leave the entry unvetted with the reason. Never report a verdict the evidence does not support.

### Step 4: Record verdicts (orchestrator, one entry at a time)

For each report, edit the entry in `docs/tracker/fixes.md`:

- **Metadata.** Add or replace `- **Vetted:** <verdict> at \`<sha>\` (<YYYY-MM-DD>)` as the last metadata bullet.
- **Stale facts.** Correct them in place. These are inline doc fixes, not new entries.
- **`corrected`.** Rewrite the Suggested fix to the verified version, and keep one bullet that says what changed from the original and why.
- **Prototype results.** Add or replace a bullet under Suggested fix, in the shape FIX-225, FIX-227 and FIX-233 use: `Prototype results, run at \`<sha>\` on <date>: …`. It holds the reproduction, gates, fail-first lines, golden churn and patch path. Quote measured output exactly.
- **`needs ruling`.** Add the questions as bullets under Suggested fix, headed "Pending the user's ruling".
- **Auto-accepted.** Record each one under Suggested fix in the wording `/fix`'s auto-accept rule gives.
- **Backlog.** Append the report's `backlog` lines to `docs/tracker/backlog.md`, between its markers. While its header says `Queue freeze: on`, append the `candidate FIX` lines too, tagged `[measured]`.
- **`not reproducible`.** Record the attempt as a dated note under Issue. Do not resolve the entry; the user decides whether to close it.

Then run `make tracker-check`.

### Step 5: Rulings and candidate FIXes

- Collect every `needs ruling` question from this run, plus those carried over as "awaiting ruling" in Step 2.
- Ask them with AskUserQuestion, at most four per call. Put the recommended option first with "(Recommended)", and give each option its measured consequence.
- Record each answer in its entry ("The user ruled on <date>: …"), then run a second Step 3 wave for the ruled entries so they end `ready`. A question the user defers leaves its entry at `needs ruling`.
- Candidate FIXes (only when the queue freeze is off; under a freeze they are already in the backlog): present each with its evidence and ask whether to file it with `/fix add`. **Never file automatically.** One fix spawning several entries per run is the waterfall the tracker must avoid; everything short of a measured user-visible failure belongs in the backlog, not here.

### Step 6: Report

- A table: `FIX | verdict | patch | notes`.
- Questions still open; every auto-accepted question, so the user can veto it; candidate FIXes and how the user answered; the number of backlog lines appended; and the number of stale facts corrected.
- The tracker edits are uncommitted. Offer a commit: `docs(tracker): vet FIX-NNN, FIX-MMM`.

### Rules

- Write only to throwaway worktrees, `.claude/prototypes/`, `docs/tracker/fixes.md` and `docs/tracker/backlog.md` (Step 4, by the orchestrator). Never touch the main working tree otherwise.
- Stop on blockers and ask; do not suppress them. A broken case is never gated off to reach a verdict.
- Keep the worktrees clean: name probe files `zz_probe_*`, delete sibling example copies, restore each worktree to clean after V7, and remove the inline worktree at the end.
- A patch is a starting point for `/fix-implement`, not a substitute for its gates. That skill still runs `make check`, `make check-examples`, the fail-first list and the auto-review on the final code.
