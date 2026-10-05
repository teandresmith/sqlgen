Implement a fix for a tracked bug discovered during E2E testing.

## Instructions

The user wants to implement a fix for `$ARGUMENTS` (e.g., `FIX-003`, `FIX-006`).

### Step 0: Parse arguments

Split `$ARGUMENTS` on whitespace. The first token is the FIX ID. Recognize these flags anywhere in the remaining tokens:

- `--skip-review` — skip Step 8 (auto-review). Use only for trivial fixes (typo, comment-only edits, single-line changes the user has already vetted). When in doubt, do not skip.

### Step 1: Load the fix spec

Read `docs/tracker/fixes.md` and find the entry matching the given FIX-NNN ID in the **Open** section. Determine whether it is open from the entry's own `- **Status:**` bullet rather than from which heading it appears under — that bullet is per-entry and `make tracker-check` validates it against the section; the headings are quoted inside entry prose and are not safe to locate by substring (see `/fix` → **Locating the sections**).

If the ID is not found in Open:
- Check the **Resolved** section — if found there, tell the user it's already resolved.
- Otherwise, report that the ID does not exist. Show open fixes for reference.

Two entry shapes exist in the tracker (see `/fix` for the current spec):

**Structured form (current, H3 heading):**

```markdown
### FIX-NNN

- **Status:** Open
- **Severity:** tracked
- **Origin:** phase N, module/path
- **Surfaced by:** _optional_

**Issue.** ...
**Root cause.** ...
**Suggested fix.** ...
**References.** ...
**Resolution notes.** _Filled in by /fix resolve._
```

Extract each labeled section into a structured object:
- **Origin** (phase + module) — drives Step 2's guideline selection.
- **Severity** — sets `/fix resolve` gating expectation.
- **Issue** — the symptom; what someone observes.
- **Root cause** — the underlying defect; usually a specific file:line.
- **Suggested fix** — the prescriptive plan. Treat as a starting point, not a contract — if the actual root cause turns out to differ, follow the bug, not the suggestion (file a `/fix resolve` note explaining).
- **References** — PRD sections + related FIX IDs; read these before writing code.

If any required section is missing or carries the `_TBD_` placeholder, stop and ask the user to populate it before continuing — the fix-implement flow depends on knowing the bug shape and intended fix.

**Legacy form (one-line bullet):**

```markdown
- [ ] **FIX-NNN** (phase N, module) [severity] — long paragraph
```

For these, extract origin + severity from the header and treat the paragraph as a combined Issue + Suggested fix. If the entry pre-dates the structured format, opportunistically upgrade it to H3 in the same change so the tracker drifts toward structure — populate the four sections from your own reading + the resolution notes you'd be adding anyway.

### Step 2: Read the PRD and guidelines

Use the origin module to determine which guidelines and PRD sections are relevant. Always load the guidelines that match the module where the bug lives:

| Origin module contains | Read |
|------------------------|------|
| Always | `guidelines/ARCHITECTURE.md` — to know where files go and module boundaries |
| `gen/`, `templates`, `context`, `orchestrate` | `guidelines/TEMPLATES.md` — template authoring, funcmap, context structs |
| `gotype` | `guidelines/GO.md` — type resolution, naming conventions |
| `database`, `pgx`, `stdlib` | `guidelines/ERRORS.md` — driver adapters, error mapping |
| `sql/`, `comparator`, `dialect` | `guidelines/SQL.md` — SQL builders, placeholders, dialect differences |
| `config` | `guidelines/GO.md` — config validation, struct conventions |
| Any Go code | `guidelines/GO.md` — naming, error handling, interfaces, formatting |
| Any test code | `guidelines/TESTING.md` — table-driven tests, no assertion libraries, `cmp.Diff` |

Read the PRD sections referenced by the origin phase. Check `docs/tracker/IMPLEMENTATION_ORDER.md` for the phase description to identify relevant PRD sections.

### Step 3: Understand the bug

**If the entry was vetted**, start from the prototype instead of working it out again; `/fix-vet` already did the numbered steps below, with evidence. An entry is vetted when it carries `- **Vetted:** ready` or `corrected at <sha>` and `.claude/prototypes/FIX-NNN.patch` exists.

1. **Freshness.** Run `git diff --stat <sha> HEAD -- <the patch's files>`. Empty output means the prototype is fresh.
2. **Apply.** Run `git apply --3way .claude/prototypes/FIX-NNN.patch`. Do not trust the patch's hunks for generated files (`expected/`, `models/`, unit goldens). Apply the source changes, regenerate in Step 6, and compare the churn with the entry's recorded golden churn.
3. **Stale or failed apply.** Work from the entry's Prototype results bullet against HEAD, and re-run its fail-first list. Say in the resolution notes that the prototype was stale.
4. **Nothing is skipped downstream.** Run Step 3.5 onward in full: Step 5's gates, the entry's fail-first list on the final code, and the auto-review. The prototype saves the derivation, not the verification.
5. **`Vetted: needs ruling` with no ruling recorded** means stop and ask, unless every pending question's recommended option meets `/fix`'s **auto-accept** rule: then apply it, record it as that rule says, and continue. Otherwise suggest `/fix-vet FIX-NNN` once the user has ruled.

Delete `.claude/prototypes/FIX-NNN.patch` after the auto-review (Step 8), not at resolve time: the reviewer compares the landed diff against it.

Before writing any code:

1. **Read the affected source files** identified by the origin module
2. **Reproduce the issue** — understand exactly what's wrong by reading the code, templates, or generated output
3. **Identify the root cause** — don't just fix the symptom
4. **Plan the fix** — describe the intended change to the user before implementing

### Step 3.5: Preflight (tool-version check)

Before applying the fix, run the shared environment check. The 16.8g closure sweep surfaced gofumpt-version drift between the locally-installed binary and the golangci-lint-bundled one — preflight on every fix catches this on entry, not after goldens regenerate:

```bash
gofumpt --version 2>/dev/null || echo "MISSING: gofumpt"
golangci-lint --version 2>/dev/null | head -1 || echo "MISSING: golangci-lint"
go version
```

If `.tool-versions` / `tools.mk` exists, compare and **warn (do not block)** on mismatch — fixes that touch goldens are the highest-risk shape for fmt drift.

### Step 4: Implement the fix

Apply the fix following project guidelines:

**Implementation rules:**
- Follow the PRD exactly — do not add features, config fields, or behavior not specified
- Follow `guidelines/GO.md` for naming, error handling, interfaces, and formatting
- Follow `guidelines/TESTING.md` for test structure (table-driven, no assertion libraries, `cmp.Diff`)
- Place files according to `guidelines/ARCHITECTURE.md`
- Follow `guidelines/TEMPLATES.md` when modifying templates — check funcmap, context fields, whitespace
- Run `gofumpt` on all modified Go files
- Never write FIX IDs, phase numbers, sub-item IDs or design-decision labels (`D7`, `E8`) into code, test names or fixture data — they go in the commit message; comments state the behavior (`guidelines/GO.md` → When to Comment). Templates carry no references at all (`guidelines/TEMPLATES.md` §8). `make check` runs `refs-check`.
- Before Step 5, reread every comment the diff adds or rewrites against `guidelines/GO.md` → When to Comment → **Keep it short**: cut history, restated names and sentences that add nothing.

**For template fixes:**
- Update the template file
- Update any golden files that test the template output
- Update any pattern-matching tests that assert on the old output
- Regenerate E2E golden files if the fix affects generated output: `make update-golden-e2e`

**For type resolution fixes (gotype):**
- Update the resolver or context builder
- Verify existing type mapping tests still pass
- Add test cases for the new behavior

### Step 5: Lint and test

Run the full lint + unit test suite:
```bash
make check
```

If the fix affects E2E golden files, also verify:
```bash
cd cmd/sqlgen && go test -run TestE2EGoldenFiles -count=1 -timeout=5m
```

**Path-based E2E trigger** (shared with `/implement` Step 5 — keep the three skills in sync). Capture the diff:

```bash
git diff --name-only HEAD
```

Run `make check-examples` (requires Docker) if **any** changed path matches one of these globs — mechanical decision, not a judgment call:

| Path glob | Reason |
|---|---|
| `cmd/sqlgen/gen/**` | Templates, funcmap, context builders, orchestrate |
| `cmd/sqlgen/gotype/**` | Type resolution flows through to generated output |
| `cmd/sqlgen/config/types.go` | Config struct shape changes thread through to example `sqlgen.yml` parsing |
| `cmd/sqlgen/config/validate.go` | Validation errors surface at example-generation time |
| `cmd/sqlgen/manifest/**` | Manifest builder + emitter (phases 18+) |
| `cmd/sqlgen/wrapper/**` | gqlgen wrapper subcommand |
| `cmd/sqlgen/testdata/examples/**` | Direct example changes |
| `parser/**` | Parser output drives codegen |
| `database/**`, `sql/**`, `comparator/**`, `omittable/**`, `manifest/**`, `cache/**`, `tenancy/**`, `database/event/**` | Runtime packages — consumer-facing API |
| `Makefile`, `.golangci.yml`, `go.mod` | Build / lint config |

The narrow "modifies a file under examples/" trigger is not enough — a template or funcmap change is the most common shape of a fix and changes example output without touching example files.

If linting or tests fail, fix the issues before proceeding.

### Step 6: Regenerate golden files (if applicable)

If the fix changes generated output:

1. Regenerate E2E golden files:
   ```bash
   make update-golden-e2e
   ```
2. Regenerate gen golden files (if template output changed):
   ```bash
   make update-golden
   ```
3. Verify the diffs are expected — only the fixed behavior should change
4. Verify the regenerated output actually compiles and passes against real databases (requires Docker):
   ```bash
   make check-examples
   ```
   If you already ran this in Step 5, run it again here — the regen produced new files that need verification.

### Step 7: Resolve the fix

Run `/fix resolve FIX-NNN — brief description of what was done` to move the entry from Open to Resolved.

### Step 8: Auto-review

Skip this step if `--skip-review` was passed in arguments.

After `make check` passes, goldens have been regenerated (if applicable), and the fix has been resolved, launch the `sqlgen-reviewer` subagent to verify the change holds up against the PRD, guidelines, and security rules — and to catch regressions in code the diff might affect indirectly.

Capture the diff scope first:
```bash
git diff --name-only HEAD
```

Then invoke the subagent in a single tool call:

```
Agent(
  subagent_type: "sqlgen-reviewer",
  description: "Review {FIX-NNN}",
  prompt: "Review the implementation of {FIX-NNN} in `docs/tracker/fixes.md` (now in the Resolved section). Files changed (from `git diff --name-only HEAD`):\n\n{file list}\n\nFor a fix, focus on: (1) the bug described is no longer reproducible from the changed code, (2) a regression test exists and exercises the bug shape, (3) no callers / sibling templates / dialect parallels were silently broken. The diff is your primary scope; explore beyond it (~10-15 files max) for regression checks. Report findings only; do not modify files."
)
```

Relay the subagent's report verbatim or as a tight summary in the final report below. Do **not** auto-log findings as new FIX entries. Triage each finding outside this fix's scope per `/fix` → **Findings triage**: fix inline-bucket items in this change (record them in the resolution notes), append backlog-bucket items to `docs/tracker/backlog.md`, and surface only measured, user-visible failures as candidate FIXes for the user to decide on (under a queue freeze, those go to the backlog tagged `[measured]` too). Do not widen the fix past `/fix`'s scope cap.

If the reviewer surfaces high-confidence issues, flag them clearly in Step 9 and suggest the user either address them before considering the fix done or run `/verify FIX-NNN` for a deeper look.

Apply the report's **Tracker & Doc Accuracy** items in place. They are verified `old → new` doc corrections, and they are never filed as FIX entries. Record them in the resolution notes. Bring **Questions to Adjudicate** to the user; don't decide them yourself, except where the recommended option meets `/fix`'s auto-accept rule (apply and record it, and name it in Step 9).

### Step 9: Report

Tell the user:
- What was changed (files modified)
- Root cause explanation
- Test results
- Whether E2E golden files were regenerated
- The auto-review summary (high-confidence findings only) — or note that review was skipped
- Any related fixes that may also be resolved or affected by this change
- Findings triage: what was fixed inline, how many backlog lines were appended, any candidate FIX, and any auto-accepted question (so the user can veto it)
