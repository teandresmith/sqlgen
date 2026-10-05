Implement a specific sub-item from the task tracker.

## Instructions

The user wants to implement sub-item $ARGUMENTS (e.g., `0.0`, `0.1`, `2.3`, `6.4a`).

### Step 0: Parse arguments

Split `$ARGUMENTS` on whitespace. The first token is the sub-item ID. Recognize these flags anywhere in the remaining tokens:

- `--skip-review` — skip Step 8 (auto-review). Use only for trivial work (typo, comment-only edits, single-line fixes the user has already vetted). When in doubt, do not skip.

### Step 1: Load the task spec

Read the relevant phase file from `docs/tracker/` (e.g., `docs/tracker/phase-0.md` for sub-item `0.1`). If the phase file does not exist, tell the user to run `/phase <N>` first.

Extract for this sub-item:
- All tasks (the checkbox items)
- Acceptance criteria
- Tests required
- PRD references

### Step 2: Read the PRD and guidelines

Read the PRD sections referenced by this sub-item. Understand the full specification — types, behavior, edge cases, error handling.

Read only the guidelines relevant to this sub-item:
- Always read `guidelines/ARCHITECTURE.md` to know where files go
- Read `guidelines/GO.md` if writing Go code
- Read `guidelines/TESTING.md` if writing tests
- Read `guidelines/SQL.md` if working on SQL builders or dialects
- Read `guidelines/ERRORS.md` if working on error types or driver adapters
- Read `guidelines/TEMPLATES.md` if working on code generation templates

### Step 3: Check dependencies

Read the sub-item's "Depends on" field from `docs/tracker/IMPLEMENTATION_ORDER.md`. Verify that dependencies are already implemented by checking the tracker. If a dependency is not complete, warn the user before proceeding.

### Step 3.5: Preflight (tool-version check)

Before writing code, run a quick environment check. The 16.8g closure sweep surfaced an environmental drift (locally-installed `gofumpt v0.8.0` produced different whitespace than the golangci-lint-bundled gofumpt that captured the committed goldens, re-aligning nine files across four example trees). The preflight catches this on the first sub-item, not on the closure sweep.

```bash
gofumpt --version 2>/dev/null || echo "MISSING: gofumpt"
golangci-lint --version 2>/dev/null | head -1 || echo "MISSING: golangci-lint"
go version
```

If a project-pinned tool-version file exists at `.tool-versions` or `tools.mk`, compare against it and **warn (do not block)** on mismatch. Cite the 16.8g drift incident in the warning so the user knows what they're risking. If no pin file exists yet, note that in the preflight output and continue.

### Step 4: Implement

Work through the tasks in order. For each task:

1. Write the code following the guidelines and PRD specification
2. Write the tests as specified in "Tests Required"
3. Run `go vet` and check for compilation errors
4. Check off the task in the phase file as you complete it

**Implementation rules:**
- Follow the PRD exactly — do not add features, config fields, or behavior not specified
- Follow `guidelines/GO.md` for naming, error handling, interfaces, and formatting
- Follow `guidelines/TESTING.md` for test structure (table-driven, no assertion libraries, `cmp.Diff`)
- Place files according to `guidelines/ARCHITECTURE.md`
- Run `gofumpt` on all written files
- Never write FIX IDs, phase numbers, sub-item IDs or design-decision labels (`D7`, `E8`) into code, test names or fixture data — they go in the commit message; comments state the behavior (`guidelines/GO.md` → When to Comment). Templates carry no references at all (`guidelines/TEMPLATES.md` §8). `make check` runs `refs-check`.
- Before Step 5, reread every comment the diff adds or rewrites against `guidelines/GO.md` → When to Comment → **Keep it short**: cut history, restated names and sentences that add nothing.

### Step 5: Lint and test

After implementation is complete, run the full lint + unit test suite:
```bash
make check
```

**Path-based E2E trigger.** Capture the diff:

```bash
git diff --name-only HEAD
```

Run `make check-examples` (requires Docker) if **any** changed path matches one of these globs — this is a mechanical decision, not a judgment call:

| Path glob | Reason |
|---|---|
| `cmd/sqlgen/gen/**` | Templates, funcmap, context builders, orchestrate — change generated output without touching examples |
| `cmd/sqlgen/gotype/**` | Type resolution flows through to generated output |
| `cmd/sqlgen/config/types.go` | Config struct shape changes thread through to example `sqlgen.yml` parsing |
| `cmd/sqlgen/config/validate.go` | Validation errors surface at example-generation time |
| `cmd/sqlgen/manifest/**` | Manifest builder + emitter — phases 18+ |
| `cmd/sqlgen/wrapper/**` | gqlgen wrapper subcommand — touches example generation |
| `cmd/sqlgen/testdata/examples/**` | Direct example changes |
| `parser/**` | Parser output drives codegen |
| `database/**`, `sql/**`, `comparator/**`, `omittable/**`, `manifest/**`, `cache/**`, `tenancy/**`, `database/event/**` | Runtime packages — consumer-facing API changes |
| `Makefile`, `.golangci.yml`, `go.mod` | Build / lint config changes |

The narrow "modifies a file under examples/" trigger is not enough — a template or funcmap change is a common shape of work and changes example output without touching example files. The path table above is the source of truth; when in doubt, run it anyway.

**Security-review trigger.** If any changed path falls under **either** of these, additionally launch `/security-review` (or note explicitly that it was deferred to `/verify`):

| Path glob | Security surface |
|---|---|
| Top-level runtime packages shipped via `//go:embed` to consumers (`manifest/`, future embed targets) | Supply-chain — every consumer binary carries this code |
| `cmd/sqlgen/cli/**` subcommands that read external files or URLs (validate, diff, follow-schema, gqlgen invocations) | Path traversal, SSRF, schema-fetch attack surface |

If a sub-item produces new generated output that needs new goldens, regenerate first:
```bash
make update-golden-e2e
make update-golden
```
Then re-run `make check-examples` to verify the regenerated output compiles and passes.

If linting or tests fail, fix the issues before proceeding.

### Step 6: Update the phase file

- Check off all completed tasks
- Update the sub-item's `**Status:**` to `In Progress` or `Complete`
- If all tasks are done and tests pass, update status to `Complete`

### Step 7: Update STATUS.md

If this is the first sub-item being worked on in this phase, update `docs/tracker/STATUS.md`:
- Change the phase status to `In Progress`
- Update the "Current Focus" section with the current sub-item

### Step 8: Auto-review

Skip this step if `--skip-review` was passed in arguments.

After `make check` passes and the tracker has been updated, launch the `sqlgen-reviewer` subagent to do a final pass against the PRD, guidelines, and security rules. This keeps the review's heavy reads (PRD + guidelines + cross-file regression scan) out of the main context.

Capture the diff scope first:
```bash
git diff --name-only HEAD
```

Then invoke the subagent in a single tool call:

```
Agent(
  subagent_type: "sqlgen-reviewer",
  description: "Review {sub-item ID}",
  prompt: "Review the implementation of sub-item {ID} in `docs/tracker/phase-{N}.md`. Files changed (from `git diff --name-only HEAD`):\n\n{file list}\n\nThe diff is your primary scope. You are explicitly allowed to explore beyond it (read callers, sibling templates, related goldens, dialect parallels) to catch regressions — be surgical, ~10-15 files max outside the diff. Report findings only; do not modify files or log fixes."
)
```

Relay the subagent's report verbatim or as a tight summary into the final report below. Do **not** auto-log findings as FIX entries — that decision belongs to `/verify` if the user runs it.

If the reviewer surfaces high-confidence FAIL / PARTIAL / regression items, mention them clearly in Step 9 and suggest the user either address them before `/done` or run `/verify $ARGUMENTS` for a deeper look.

Apply the report's **Tracker & Doc Accuracy** items in place. They are verified `old → new` doc corrections, and they are never filed as FIX entries. Record them in the completion record. Bring **Questions to Adjudicate** to the user; don't decide them yourself.

### Step 9: Report

Tell the user:
- What was implemented (files created/modified)
- Test results
- Any issues or deviations from the PRD (there should be none — flag them if so)
- The auto-review summary (high-confidence findings only) — or note that review was skipped
- Suggest running `/verify {sub-item ID}` to confirm PRD compliance before `/done` if the reviewer flagged anything, otherwise the user can `/done` directly
