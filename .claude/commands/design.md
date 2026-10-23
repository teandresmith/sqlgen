Create a new design note, or add a behavior contract to an existing one, from the design template.

## Instructions

The user wants a design note for `$ARGUMENTS` (e.g. `tenant-scoped cache invalidation`, `--name CACHE_WARMING tenant-scoped cache warming`, `--retrofit SCHEMAFUZZ.md`).

A design note is a proposal the user reviews before any `/phase` breakdown. It holds the rationale and the **behavior contract**: every behavior the work adds or changes, and every existing behavior it touches that must not change, each tied to a test. The contract is what `/phase`, `/implement` and `/verify` check the work against. Writing it before the code is how a regression is caught by a test instead of by `/close-phase`.

The template is `docs/design/templates/design.md`. Its section numbers are fixed. Other skills cite §6 (behavior contract) and §9 (plan) by number, so never renumber or drop a section. Write "None." in a section that has nothing to say.

### Step 0: Parse arguments

Recognize these flags anywhere in `$ARGUMENTS`; everything else is the topic.

- `--name STEM` — the file stem, `docs/design/STEM.md`. Default: the topic in `UPPER_SNAKE_CASE`, at most four words.
- `--retrofit DOC.md` — add the template's §6 behavior contract to an existing note instead of writing a new one. Skip to Step 6, using that note's own design sections as the source.
- `--dry-run` — print the outline and the §6 rows you would write, and stop. Write nothing.

### Step 1: Check for overlap

List `docs/design/*.md` and `docs/design/archive/*.md` and read each status line. If an existing note already covers the topic, stop and ask the user whether to extend it (`--retrofit` or an edit) or write a new note. Do the same if the topic is a filed FIX entry or a backlog line. Name the entry, because that work may belong in `/fix-vet` instead.

### Step 2: Read the specification

- Find the PRD sections the topic touches. Search `docs/PRD.md` headings, then read those sections in full. Do not read the PRD whole.
- Read the guidelines `guidelines/README.md` points to for the surfaces involved (config, parser, gen, runtime, templates).
- Read `docs/design/CONFORMANCE.md` §2 (the test homes), §3 (the module matrices, and which ones the touched modules declare) and §4.4 (capabilities). The contract uses all three.

### Step 3: Measure the current state

Everything in §1 and §3 is either cited (`path/file.go:line` at the HEAD sha) or measured. Never reason from memory.

- Read the code paths the design touches, end to end, on every dialect they serve.
- Reproduce the symptom (§1.1). If it cannot be reproduced, say so: it becomes a §11 question, not a motivation.
- When a design choice depends on runtime or database behavior, probe it in a throwaway worktree (`git worktree add --detach <scratchpad>/wt HEAD`). Use a sibling copy of an example and `GOWORK=off`. Save any prototype as `.claude/prototypes/<stem>.patch`, remove the worktree, and record the probe in §7: date, sha, what ran, the result.

### Step 4: Draft the design

Copy the template to `docs/design/<STEM>.md`, then fill in §1–§5 and §7–§8:

- **Status line:** today's date, the HEAD sha, a two-to-four-sentence summary, and the sibling PRD sections.
- **§4 Options:** each with what it wins and what it loses, and a recommendation.
- **§8 PRD impact:** the proposed wording for every PRD change. §6 rows marked "proposed" depend on it.
- Follow the project rules: no features the PRD does not have unless §8 proposes adding them, the three-module boundaries, and no build tags.

### Step 5: Write the behavior contract (§6)

This is the step that matters most. Write it with the same care as the design.

**§6.1 New and changed behavior.** For each behavior the design adds or changes, write one row:

- One testable statement, not a paragraph.
- Its PRD anchor: an existing section that already states it, or "proposed" with the wording in §8. **A behavior with neither is an invented contract.** Do not write it as a row; make it a §11 question. For example, "an empty key list runs no query" was once asserted for `HardDeleteMany`, but the PRD never says it, and the generated code sends `WHERE 1 = 0` by design.
- Where it applies: `all`, or the dialects with the capability that limits it.
- Where it's tested and the planned test name, written the way the registry writes it (`docs/conformance/README.md`): `conformance: Method/behavior`, `unit: <dir>: TestName/case`, `integration: <dir>: TestName`, `example: <example>: TestName` or `golden: <example>`. The name states the behavior. It never carries the row's ID, a PRD number or a label.
- The module matrix cell it fills (`<matrix>: <row> × <column>`, from `CONFORMANCE.md` §3).
- Status `planned`.

**§6.2 Regression guards.** For every code path the design touches, list the existing behavior that must not change:

- Find what calls the touched code: the other dialects, and the soft-delete, tenancy, transaction, hook and cache paths through it.
- For each behavior, find the test that guards it today (`grep` the test names and the example `tests/` directories). Record it as `pkg: TestName/case`.
- A behavior with no test is `unguarded`. The plan's N.1 sub-item writes those tests against today's code, and they must pass before any change lands.

**§6.3 Module matrix cells.** For every module the design touches (runtime, parser, CLI, generated client), take the matrices `CONFORMANCE.md` §3 declares for it and keep the rows the design adds or changes. A new rule, DDL feature, type or method is a new row with every column. Every cell is a §6.1 or §6.2 ID, or `n/a: <reason>` with the PRD reason. Leave no cell empty. A cell you can't fill or justify is a §11 question. When a touched module's matrix is missing an axis the PRD clearly varies over, say so in §11 rather than inventing the column.

**§6.4 Dialect differences.** Each one as a named capability with the expectation on each side, or "None."

**§9 and §10.** Make N.0 the PRD sync and N.1 the regression guards. List against each later sub-item the §6.1 IDs it turns from failing to passing. Keep §10's definition of done as the template has it, and add any item specific to the design.

### Step 6: Ask the open questions

Collect every §11 question, including any invented contract Step 5 turned into a question. Ask them with `AskUserQuestion`, at most four per call. Put the recommended option first, labelled "(Recommended)", and give the trade-off in each option's description. Record the answers as a "Rulings (settled <date>)" table in §11, and update §5, §6 and §9 to match.

In `--retrofit` mode: add §6 (and §10 if missing) to the existing note under its own section numbering. Fill them in from the note's design sections and the code as Steps 3 and 5 describe. Then ask the questions this raises.

### Step 7: Check and report

- Delete every template guidance comment (`<!-- … -->`).
- Run `make refs-check`. Design notes are excluded from its ID checks, but the run confirms that no template or code changed by accident.
- Do not commit. The note is a draft for the user to review.

Tell the user:

- The file path and a summary of the design.
- The §6 counts: new behaviors, regression guards (and how many are unguarded), and matrix cells marked n/a.
- Any invented contract that was moved to a question.
- The rulings taken and any questions still open.
- The next step: review the note. Once it's approved, add the phase to `docs/tracker/IMPLEMENTATION_ORDER.md` and run `/phase N`, which carries §6 into each sub-item's Tests Required.
