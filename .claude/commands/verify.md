Verify that an implemented sub-item or resolved fix meets its specification.

## Instructions

The user wants to verify `$ARGUMENTS` against the PRD and project guidelines. This is the deeper review that runs before marking work as complete with `/done` (for sub-items) or before closing the loop on a fix.

### Step 0: Dispatch on input shape

Inspect `$ARGUMENTS`:

- Matches `^FIX-\d+` (e.g., `FIX-070`) → **fix verification mode**
- Otherwise (e.g., `0.1`, `2.3`, `6.4a`, `16.8b`) → **sub-item verification mode**

The two modes share most of the flow; only Step 1 (load spec) and Step 8 (logging) differ.

### Step 1: Load the spec

**Sub-item mode**: read the matching `docs/tracker/phase-N.md`. Extract:
- Acceptance criteria for this sub-item
- Tests required
- PRD section references

**Fix mode**: read `docs/tracker/fixes.md` and find the entry by its `### FIX-NNN` heading, then read its state off the entry's own `- **Status:**` bullet rather than inferring it from the surrounding section heading (see `/fix` → **Locating the sections**). Extract:
- Origin phase + module
- Description (the bug summary and any expected-fix notes)
- Use the origin phase to locate relevant PRD sections via `docs/tracker/IMPLEMENTATION_ORDER.md`

### Step 2: Read the PRD sections

Read each referenced PRD section from `docs/PRD.md`. Build a checklist of every concrete requirement:
- Types, functions, methods, interfaces that must exist
- Specific behavior (edge cases, error handling, return values)
- Dialect-specific behavior (if applicable)
- Config fields and defaults
- Validation rules

For fix mode, the "checklist" is narrower — focus on the behavior the fix is supposed to restore or correct.

### Step 3: Read the implementation

Find and read all source files related to the work:

- **Sub-item mode**: use the package name and `guidelines/ARCHITECTURE.md` to locate them. Read all `.go` files in the package, including `_test.go`.
- **Fix mode**: derive the changed files from the fix's origin module + the resolution commit (`git log --grep="FIX-NNN"` or recent commits). Read both the modified source and any associated tests.

### Step 4: Cross-reference

For each PRD requirement, check whether the implementation satisfies it. Categorize findings as:

- **PASS** — requirement is implemented and matches the PRD
- **FAIL** — requirement is missing or does not match the PRD
- **PARTIAL** — requirement is partially implemented (explain what's missing)
- **UNTESTED** — requirement is implemented but has no test coverage

For fix mode, additionally verify:
- The bug described is no longer reproducible from the current code
- A regression test exists and exercises the bug shape (not just the happy path)
- No callers, sibling templates, or dialect parallels were silently broken

### Step 5: Check test coverage

For each "Tests Required" item (sub-item mode) or expected regression test (fix mode), verify a corresponding test exists:
- Table-driven test with the correct cases?
- Integration test with testcontainers (if required)?
- Edge cases covered?
- All three dialects tested (if SQL-related)?

### Step 6: Check guideline compliance

Quickly verify the implementation follows the relevant guidelines (do NOT read guidelines upfront — only check the ones relevant to the work):

- `guidelines/GO.md` — naming, error handling, context usage
- `guidelines/TESTING.md` — table-driven tests, no assertion libraries, failure message format
- `guidelines/SQL.md` — parameterization, quoting, all dialects (if SQL-related)
- `guidelines/ERRORS.md` — wrapping convention, sentinel usage (if error-related)
- `guidelines/TEMPLATES.md` — template authoring, funcmap, context structs (if template-related)
- `guidelines/ARCHITECTURE.md` — module boundaries

### Step 7: Lint and test

**Preflight (tool-version check).** Before running `make check`, capture tool versions and compare against any project-pinned set (`.tool-versions` / `tools.mk`). Cite the 16.8g drift incident in any mismatch warning:

```bash
gofumpt --version 2>/dev/null || echo "MISSING: gofumpt"
golangci-lint --version 2>/dev/null | head -1 || echo "MISSING: golangci-lint"
go version
```

Warn (do not block) on mismatch; include in the report.

**Then run** the full lint + unit test suite to confirm everything passes:
```bash
make check
```

**Path-based E2E trigger** (same table as `/implement` Step 5 — keep the two skills in sync). Capture the diff via `git diff --name-only HEAD~1..HEAD` (committed) or `git diff --name-only HEAD` (uncommitted), then run `make check-examples` if any changed path matches `cmd/sqlgen/gen/**`, `cmd/sqlgen/gotype/**`, `cmd/sqlgen/config/{types,validate}.go`, `cmd/sqlgen/manifest/**`, `cmd/sqlgen/wrapper/**`, `cmd/sqlgen/testdata/examples/**`, `parser/**`, any runtime package (`database/`, `sql/`, `comparator/`, `omittable/`, `manifest/`, `cache/`, `tenancy/`, `database/event/`), or `Makefile` / `.golangci.yml` / `go.mod`.

If linting or tests fail, include the failures in the report as action items.

### Step 7b: Delegate the deeper read

For both modes, after the parent's own cross-reference is complete, launch the `sqlgen-reviewer` subagent to do an independent deeper pass. Keep it scoped to the diff but allow exploration for regression checks.

```
Agent(
  subagent_type: "sqlgen-reviewer",
  description: "Independent review of {ID}",
  prompt: "Provide an independent review of {sub-item N.X | FIX-NNN}. Files changed (from `git diff --name-only HEAD~1..HEAD` if recently committed, or `git diff --name-only HEAD` if uncommitted):\n\n{file list}\n\nThe diff is your primary scope, but explore (~10-15 files max) for regression risks. Report findings only — do not modify files or log fixes. Confirm or refute the parent's preliminary findings: {paste short summary of parent's PASS/FAIL findings}."
)
```

Reconcile the subagent's findings with the parent's: agreed FAIL/PARTIAL items are high-confidence; disagreements get noted in the report so the user can adjudicate. Apply the reviewer's **Tracker & Doc Accuracy** items in place: they are verified doc corrections, and Step 8 never logs them. Its **Questions to Adjudicate** go to the user and are not auto-logged. A REFUTED fail-first spot check is a FAIL-level finding.

### Step 8: Log fixes (sub-item mode only — skip in fix mode)

**Sub-item mode**: if any PRD requirements are **FAIL** or **PARTIAL**, or any required tests are **MISSING / UNTESTED**, automatically log them as fixes with severity derived from the verdict:

| Verify verdict | FIX severity | Resolution gate |
|---|---|---|
| FAIL | `blocking` | Must resolve before `/done` on this sub-item |
| PARTIAL | `tracked` | Must resolve before phase closure (`/close-phase`) |
| UNTESTED (impl exists, no test) | `tracked` | Must resolve before phase closure |
| MISSING (required test absent) | `tracked` | Must resolve before phase closure |

Promote `tracked` to `blocking` only when the gap leaves a load-bearing surface unverified for downstream sub-items (call this out explicitly in the FIX description so the reader knows why).

1. Read `docs/tracker/fixes.md`
2. For each FAIL/PARTIAL/MISSING/UNTESTED item, add a fix entry via the `/fix add` skill in **multi-line structured form** (see `/fix` for the entry template). Each call carries all four required sections — `Issue.`, `Root cause.`, `Suggested fix.`, `References.` — plus a `Surfaced by.` line citing `/verify {sub-item ID} ({date})`. The single-line legacy form is not accepted from `/verify`. Derive each section's content from the verification verdict:
   - **Issue** — restate the failed PRD requirement or missing test, citing the spec ref.
   - **Root cause** — what the implementation does (or doesn't do) that produces the failure; cite file:line where the gap lives.
   - **Suggested fix** — bullet list of edits and tests to add to close the gap.
   - **References** — PRD section(s) anchoring the requirement; related FIX IDs if the gap mirrors a prior issue.

   Determine the origin phase and module from the sub-item being verified — use the origin where the bug *lives*, not where it was found.
3. Write the updated fixes file
4. Include the assigned FIX-NNN IDs **and severities** in the report

Skip this step entirely if all items are PASS.

**Fix mode**: do **not** auto-log new FIX entries from this step. If verification reveals that the resolved fix is incomplete or introduced regressions, surface that in the report — the user decides whether to reopen the original FIX (manually move it back to Open with a note) or file a new related FIX via `/fix add`.

### Step 9: Report

Output a structured verification report:

```
## Verification: {N.X | FIX-NNN} {component}

### PRD Compliance
| # | Requirement | Status | Notes |
|---|------------|--------|-------|
| 1 | {requirement from PRD} | PASS/FAIL/PARTIAL/UNTESTED | {details} |

### Test Coverage
| # | Required Test | Status | Notes |
|---|--------------|--------|-------|

### Guideline Compliance
| Guideline | Status | Issues |
|-----------|--------|--------|

### Reviewer Subagent Findings
- {high-confidence items the subagent flagged that the parent missed, or confirmations of parent findings}
- {disagreements between parent and subagent, if any}

### Fixes Logged (sub-item mode only)
| ID | Severity | Origin | Description |
|----|----------|--------|-------------|
| FIX-NNN | blocking/tracked/nit | phase N, module | description |

_(Omit this section in fix mode, or in sub-item mode if all items passed.)_

### Summary
- **PRD Requirements:** X/Y passing
- **Tests:** X/Y present
- **Guideline Issues:** N
- **Reviewer agreement:** {full | partial — see disagreements above}
- **Fixes Logged:** N (or "None — ready for `/done`" in sub-item mode; "Bug confirmed resolved with no regressions" or "Fix incomplete — see findings" in fix mode)

### Action Items
1. {specific thing to fix, with FIX-NNN reference if logged}
```

If everything passes:
- **Sub-item mode**: say so clearly — the user can run `/done` with confidence.
- **Fix mode**: confirm the bug is resolved and no regressions surfaced; the FIX entry stays in Resolved.

If there are failures:
- **Sub-item mode**: they are already logged as fixes — the user can address them and `/fix resolve` each one.
- **Fix mode**: surface the findings without auto-logging; recommend whether to reopen the FIX or file a new one.
