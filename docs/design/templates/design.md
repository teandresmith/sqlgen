# {Title}

<!--
Template for a new design note. `/design` fills it in; see `.claude/commands/design.md`.
Delete every guidance comment like this one before the note is reviewed.
Section numbers are fixed so other docs and skills can cite them: keep a section and write
"None." rather than deleting it. Optional sections are marked; delete those if unused.
-->

> **Status:** draft ({YYYY-MM-DD}). Verified against source at `{short sha}`. {Two to four
> sentences: the problem, what this proposes, and what it deliberately leaves alone.} Sibling to
> PRD §{X} ({section name}).

## 1. Motivation

### 1.1 Symptom

<!--
What goes wrong today, shown, not described. Paste the command and its output, or the generated
code and the failure. A symptom that has not been reproduced goes in §11 as a question, not here.
-->

### 1.2 Root cause

<!-- Where in the code and why. Cite `path/file.go:line` at the sha in the status line. -->

## 2. Scope

### 2.1 In scope

### 2.2 Non-goals

<!-- What a reader might expect this to cover and it does not, each with a one-line reason. -->

## 3. Current state (measured from code)

<!--
The facts the design depends on, each with a citation or a measured probe (§7). No claim from
memory or folklore: if it was not read or run, it is a question for §11.
-->

## 4. Options

<!--
Optional when only one design is plausible. Otherwise one subsection per option, each with what
it wins and what it loses, then the recommendation and why.
-->

## 5. Proposed design

<!-- The design itself, per surface: config, parser, gen, runtime, generated API, CLI. -->

## 6. Behavior contract

<!--
Mandatory. This section is what the implementation is checked against. It records every behavior
the design adds or changes (§6.1), and every existing behavior it touches that must not change
(§6.2). Rules:

- A row asserts only what the PRD states, or what §8 proposes adding to the PRD, word for word.
  A behavior with neither is an invented contract: move it to §11 as a question.
- One behavior per row, stated so a test can fail on it.
- IDs are readable slugs, `<area>.<behavior>`, unique in this note. They never appear in code,
  test names or fixtures (guidelines/GO.md -> When to Comment). Test names state the behavior.
- Test is written as the registry writes it (docs/conformance/README.md):
  `conformance: Method/behavior`, `unit: <dir>: TestName/case`, `integration: <dir>: TestName`,
  `example: <example>: TestName` or `golden: <example>`, where `<dir>` is the package directory
  relative to the repository root.
- Applies to is `all`, or a dialect / driver list with the capability or PRD text that limits it.
- Matrix cell is the module matrix cell the row fills (CONFORMANCE.md §3):
  `<matrix>: <row> × <column>`, e.g. `config-validation: cursor_keys nullable × message`.
- When the requirement registry lands (CONFORMANCE.md §5), the PRD-sync sub-item moves these
  rows into the owning module's file under docs/conformance/ (runtime.md, parser.md, cli.md or
  client.md). Until then this table is the record.
-->

### 6.1 New and changed behavior

| ID | Requirement | PRD | Applies to | Test | Matrix cell | Status |
|---|---|---|---|---|---|---|
| {area.behavior} | {One testable statement} | §{X} (existing / proposed in §8) | all | `conformance: {Method}/{behavior}` | {matrix}: {row} × {column} | planned |

<!-- Status: planned (no test yet), failing (written, fails on today's code as it should), passing. -->

### 6.2 Regression guards

<!--
Existing behavior on every code path this design touches. Find the current test for each one; a
row with no test is "unguarded", and the plan's first sub-item writes that test against
TODAY's code, where it must pass before any change lands (a characterization test, not
fail-first). Include the cross-dialect and cross-feature paths (soft delete, tenancy, tx, hooks,
cache) the touched code also serves.
-->

| Behavior that must not change | PRD | Guarded by | Status |
|---|---|---|---|
| {statement} | §{X} | `{pkg}: {TestName/case}` | guarded / unguarded |

### 6.3 Module matrix cells

<!--
One subsection per module matrix the design touches (CONFORMANCE.md §3.2 runtime, §3.3 parser,
§3.4 CLI, §3.5 generated client). Copy that matrix's declared rows and columns, keep only the
rows the design adds or changes, and fill every cell with a §6.1 / §6.2 ID or "n/a: {reason}".
An empty cell is not allowed: it is either covered, out of scope with a PRD reason, or a
question in §11. A new matrix row (a new config rule, DDL feature, method) is listed with all of
its columns. Example for a generated-client change:
-->

#### {matrix name} ({module})

| {Row unit} | {column} | {column} | {column} |
|---|---|---|---|
| `{row}` | {ID} | {ID} | n/a: {reason} |

### 6.4 Dialect differences

<!--
Every place behavior legitimately differs by dialect, as a named capability (PRD Appendix B) and
the expectation on each side. "None." when the design is dialect-neutral.
-->

## 7. Probe record

<!--
Each measured fact the design relies on: date, sha, what was run, the result. Prototypes live in a
throwaway worktree; save the patch as `.claude/prototypes/{stem}.patch` and cite it here.
-->

## 8. PRD and doc impact

<!--
Every PRD section, guideline and design note this changes, with the proposed wording or a precise
description of the edit. §6.1 rows marked "proposed" must have their wording here.
-->

## 9. Plan

<!--
Sub-items in dependency order. The first sub-items are always:
  N.0  PRD sync (§8), plus moving §6.1 into the requirement registry once it exists.
  N.1  Regression guards: write every "unguarded" §6.2 test against today's code; all pass.
Then each implementation sub-item lists the §6.1 IDs it must turn from failing to passing.
-->

| # | Sub-item | Contract rows | Notes |
|---|---|---|---|
| N.0 | PRD sync | — | Blocks everything |
| N.1 | Regression guards | {§6.2 unguarded rows} | Pass on today's code |
| N.2 | {Sub-item} | {§6.1 IDs} | |

## 10. Definition of done

The work closes only when every item below holds:

1. Every §6.1 row has a test that was seen failing before its implementation and now passes on
   every dialect in its Applies-to column.
2. Every §6.2 row is guarded, and every guard still passes.
3. Every §6.3 cell is an ID or a stated `n/a`.
4. Every §8 edit has landed, and every §11 question is ruled.
5. `make check` and `make check-examples` are green, and `make test-integration` too when the
   change reaches a runtime or driver package.

## 11. Open questions

<!--
Each question with a recommendation and the trade-off. `/design` asks them in batches; once
answered, retitle this section "Rulings (settled {date})" and record each as a table:
| # | Question | Ruling | Evidence |
-->

## 12. Rejected alternatives

<!-- Optional. What was considered and dropped, and the measured or stated reason. -->
