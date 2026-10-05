# SQLGen Mutation Testing — Design & Plan

> **Status: DESIGN FROZEN (2026-05-15).** Sibling design doc for a
> mutation-testing harness that measures the *strength* of the existing
> test suite. Output is a per-package + per-file mutation score + list
> of survived mutants pointing to assertion gaps, across both Go source
> and `*.tmpl` template files. Different goal from
> `docs/design/SCHEMAFUZZ.md`: the schema fuzzer finds bugs in the code;
> mutation testing finds gaps in the tests. §8 open-questions section
> emptied 2026-05-15 (0 open, 5 resolved) — gating signal for `/phase`
> breakdown.
>
> **Pattern:** parallels `docs/design/MANIFEST.md` (PRD §30), `docs/design/MCP.md`
> (Phase 19), and `docs/design/SCHEMAFUZZ.md` as a sibling design doc that
> fronts open questions before phase breakdown.
>
> **Phase:** TBD — provisionally Phase 21 (after Phase 20 / schema
> fuzzer closure). Mutation testing is most valuable once the fuzzer
> has driven the bug count down — at that point the question becomes
> "are the tests strong enough to catch regressions?", which is exactly
> what mutation testing answers.
>
> **Hard dependency:** none. Can be implemented at any time. Soft
> sequencing after the schema fuzzer reflects priority order
> (bug-finding before test-strength measurement), not technical
> coupling.

---

## 1. Motivation

Line coverage measures *which lines executed* during tests. It says
nothing about whether the tests would *catch a bug* on those lines. A
test that calls a function but asserts nothing meaningful achieves
100% coverage of a buggy function.

Mutation testing measures the property line coverage cannot: **assertion
strength**. The tool introduces small syntactic mutations into the
source code — flip a conditional, change a constant, drop a statement —
and reruns the test suite. Three outcomes per mutant:

| Outcome | Meaning |
|---|---|
| **Killed** | At least one test failed. Tests would catch this bug. |
| **Survived** | All tests still passed. **Coverage gap.** |
| **Equivalent** | Mutant is semantically identical (rare). Discard. |

The **mutation score** is killed / (killed + survived). A score of 80%+
in a critical package means tests are strong; 40% means tests run code
but don't assert on its behavior.

### 1.1 Why mutation testing for sqlgen specifically

sqlgen has a high density of small conditionals — both in the
template-driver Go code under `cmd/sqlgen/internal/` and in the
runtime under `sql/`, `comparator/`, `database/`. Branches like
`if dialect == Postgres`, `if column.Nullable`, `if len(args) > 0`
are everywhere, and each one is a potential coverage cliff. Mutation
testing exposes which branches have weak assertions; that's the
information needed to write *better* tests, not just *more* tests.

The technique is most valuable on the runtime + builder code paths
because they're the engine. Generated code (`*_gen.go`) is excluded:
mutating generated code mutates the output of a process, not the
process itself, and gets overwritten on the next `sqlgen generate`.

### 1.2 Why a separate phase from the schema fuzzer

Schema fuzzer and mutation testing are complementary but distinct:

| | Schema fuzzer | Mutation testing |
|---|---|---|
| **Asks** | What bugs does the code have? | Would the tests catch a bug if there were one? |
| **Finds** | Real bugs | Assertion gaps |
| **Output** | Minimum-repro schemas → `/fix add` | Survived mutants → "write a stronger test" |
| **Cost shape** | Wall-clock budget, hours per run | Mutant count × test time, hours per full run |
| **Cadence** | Nightly | Per-PR delta + quarterly full |
| **Tool** | Custom (gopter + shrinker) | Off-the-shelf (gremlins) |
| **Code home** | `tools/schemafuzz/` | `.gremlins.yaml` + thin scripts in `tools/mutation/` |

They share zero implementation — different tools, different cadences,
different consumers of the output. Worth separate phases.

---

## 2. Scope

**In scope:**

- **Go source mutation.** Adopt
  [`gremlins`](https://github.com/go-gremlins/gremlins) as the
  mutation-testing tool. Pinned version installed into `.tools/` via a
  `make mutation-install` target; never enters any `go.mod`.
- **Template mutation.** A custom harness under `tools/mutation/tmpl/`
  that mutates `*.tmpl` files, re-runs `sqlgen generate` against the
  example corpus, and asserts the example test suite catches the
  mutation. gremlins cannot do this; the harness is small but
  bespoke. See §3.4 and §3.5. Resolves §8 Q2.
- `.gremlins.yaml` at repo root configuring mutators, included
  packages, and excluded paths (generated code, tests, fixtures,
  tools).
- `make mutation` Makefile targets:
  - `mutation` — full repo run (Go + templates), intended for
    quarterly / release-prep cadence.
  - `mutation-pkg PKG=...` — single-package run for local
    development (Go source only).
  - `mutation-tmpl FILE=...` — single-template run for local
    development.
  - `mutation-diff` — PR mode, only mutating files changed vs `main`
    (Go + `.tmpl` both honored).
- `tools/mutation/` directory containing:
  - `baseline.json` — last accepted full-run results per package
    *and per file*.
  - `thresholds.yaml` — per-package **and** per-file minimum mutation
    scores (opt-in, empty initially). Resolves §8 Q5.
  - `discard-rules.yaml` — pattern-based auto-discard of
    structurally-low-value survivors (defensive nil checks,
    fallthroughs). Trimmed by triage to bound burnout. Resolves §8 Q3.
  - `cmd/compare/` — small Go program that compares a current run's
    JSON output to baseline + enforces thresholds.
  - `tmpl/` — custom template mutation harness (§3.5).
  - `README.md` — operator guide.
- CI integration:
  - Per-PR: `mutation-diff` gates merges only when explicitly opted
    in via PR label (`mutation-test`); otherwise advisory.
  - Quarterly: full run, baseline refresh PR opened automatically.
- `/simplify` coordination: `/simplify` produces a **proposed diff**
  before any code modification; `mutation-diff PROPOSAL=<patch>`
  scores the proposal against the current baseline so the human (or
  the `/simplify` skill) sees the score delta *before* deciding to
  apply. Resolves §8 Q4. Detailed flow in §5.5.
- Output triage workflow: surviving mutants flow into
  `docs/tracker/test-gaps.md` (new file) as a tracked backlog, not
  into `docs/tracker/fixes.md` (fixes are for code bugs, not test
  gaps).

**Out of scope:**

- Mutating generated code (`*_gen.go`) — semantically meaningless,
  see §3.2.
- Mutating tests themselves.
- Automatic test synthesis — survivors point to gaps; humans (or
  `/implement`) write the tests.
- Continuous mutation in CI — full runs are slow; per-PR delta is
  the only commit-cadence mode.

---

## 3. Tool choice

### 3.1 Why gremlins

[gremlins](https://github.com/go-gremlins/gremlins) chosen over
alternatives:

| Criterion | gremlins | go-mutesting | ooze |
|---|---|---|---|
| Active maintenance (2025+) | Yes | Sparse | No |
| Parallel mutation execution | Yes | No | N/A |
| Config in repo (YAML) | Yes | CLI flags | CLI flags |
| JSON / SARIF output | Yes | No | No |
| Mutator coverage | Broad | Broad | Narrow |
| Speed on a mid-sized Go repo | Fast | Slow | N/A |

Tradeoff: gremlins is younger than go-mutesting and its mutator set is
slightly smaller. Both are acceptable starting points; gremlins's
operator experience is materially better and the speed difference
matters on a repo this size.

### 3.2 What gets mutated

| Path | Mutated? | Why |
|---|---|---|
| `sql/`, `comparator/`, `omittable/`, `database/`, `cache/`, `manifest/` | Yes | Runtime core, highest value |
| `parser/` | Yes | Validation + AST construction logic |
| `cmd/sqlgen/internal/` (builders, templates' driver code) | Yes | Template branching logic |
| `cmd/sqlgen/cli/` | Yes (advisory) | CLI logic; lower priority |
| `*_gen.go` everywhere | **No** | Output of a process, not the process. Mutating it tests nothing — the next `sqlgen generate` overwrites. |
| `*_test.go` | **No** | Mutating tests is meaningless (the suite *is* the oracle). |
| `cmd/sqlgen/testdata/...` | **No** | Fixtures. |
| `tools/...` | **No** | Tooling, not product code. |
| `*.tmpl` | **Yes (custom harness)** | gremlins cannot parse Go templates; the custom harness in §3.5 covers them. |

### 3.3 Go mutator selection

gremlins ships ~10 mutators. Initial enabled set:

| Mutator | Example | Why enabled |
|---|---|---|
| `conditionals-boundary` | `>` ↔ `>=` | Catches off-by-one in pagination, OFFSET/LIMIT |
| `conditionals-negation` | `==` ↔ `!=` | Catches predicate inversion |
| `increment-decrement` | `i++` ↔ `i--` | Catches loop / index errors |
| `invert-negatives` | `-x` ↔ `+x` | Numeric sign bugs |
| `arithmetic-base` | `+` ↔ `-` | Math errors in cursor encoding, etc. |
| `remove-statement` | drop a line | Catches missing side effects |
| `branch-condition` | invert if-body | Coarse but high signal |

Deferred (review after first full run):

- String mutators (mutate string literals) — high false-positive rate
  in sqlgen because SQL strings are dialect-specific.
- Numeric-replacement (replace constants with 0/-1/+1) — useful but
  noisy; revisit after baseline.

### 3.4 Why mutate templates

Templates under `cmd/sqlgen/internal/template/` carry a large share of
the generation logic — `{{if eq .Dialect "postgres"}}`,
`{{range .Columns}}`, `{{if .Column.Nullable}}`, etc. The unit and
integration tests of the builders exercise these branches transitively
(through the generated output + example tests), but no test asserts
directly on template-internal correctness. A template branch that's
wrong-but-harmless under all currently-tested feature combinations
will not surface via gremlins on the Go source — gremlins can't see
inside `.tmpl` files.

Template mutation closes that gap. It treats the existing example test
suite as the oracle: a mutated template should produce generated code
that breaks at least one example test. If every example test still
passes after a template mutation, the templates have a coverage hole.

The cost is that template mutation runs are slow (each mutant
re-generates and re-tests the full example corpus). Acceptable at the
quarterly cadence, infeasible per-commit.

### 3.5 Template mutation harness

Lives at `tools/mutation/tmpl/`. Custom Go program; no off-the-shelf
tool covers Go templates.

**Pipeline per template mutant:**

1. Load `*.tmpl` file, parse via `text/template/parse`.
2. Apply one mutation to the AST (see mutator set below).
3. Write mutated template to a sandbox copy of the working tree.
4. Run `sqlgen generate` against every example under
   `cmd/sqlgen/testdata/examples/` using the sandbox templates.
5. Run `go vet ./...` + `go build ./...` + `go test -short ./...`
   against each regenerated example.
6. Outcome:
   - **Killed:** at least one example test failed (or vet/build
     failed). Good — the test suite catches this template bug.
   - **Survived:** every example test passes. Coverage gap.
   - **Crashed:** mutated template fails to parse or `sqlgen
     generate` panics. Discard (would indicate a parser bug, but
     that's the schema fuzzer's territory).

**Template mutator set:**

| Mutator | Example | Targets |
|---|---|---|
| `template-cond-negation` | `{{if .X}}` → `{{if not .X}}` | Conditional logic |
| `template-eq-swap` | `{{if eq .D "postgres"}}` → `{{if eq .D "mysql"}}` | Dialect dispatch |
| `template-range-drop-body` | `{{range .Cols}}A{{end}}` → `{{range .Cols}}{{end}}` | Loop body presence |
| `template-pipeline-drop` | `{{.Name | quoteIdent}}` → `{{.Name}}` | Pipeline functions (esp. `quoteIdent` — high-value, catches SQL injection regressions in templates) |
| `template-literal-flip` | `INSERT` → `UPDATE` in a SQL fragment | SQL literal correctness |
| `template-else-swap` | swap if-body and else-body | Branch inversion |

**Configured in `.template-mutation.yaml`** (sibling to
`.gremlins.yaml`):

```yaml
include:
  - "cmd/sqlgen/internal/template/**/*.tmpl"
exclude:
  - "**/*.bak"
oracle:
  examples_root: "cmd/sqlgen/testdata/examples"
  exclude_examples: []          # opt out per-example if needed
  test_args: ["-short"]
mutators:
  template-cond-negation: { enabled: true }
  template-eq-swap:       { enabled: true }
  template-range-drop-body: { enabled: true }
  template-pipeline-drop: { enabled: true }
  template-literal-flip:  { enabled: false }   # high noise, opt-in
  template-else-swap:     { enabled: true }
```

**Cost.** Each template mutant ≈ `time(sqlgen generate)` ×
N_examples + `time(make test-examples)`. Currently ~30s × 8 examples
+ ~3 min tests = ~5–6 min/mutant. Templates have ~50 conditional
sites total → ~5 hours single-threaded for a full template-mutation
run. Parallelizable across mutants. Quarterly cadence only;
per-PR mode mutates only `.tmpl` files in the diff.

**Output.** Same JSON shape as gremlins runs, in a separate file
`tools/mutation/last-tmpl-run.json`. Survivors merge into
`docs/tracker/test-gaps.md` with a `TG-NNN` ID and a `kind: template`
tag distinguishing them from Go-source gaps.

---

## 4. Configuration

### 4.1 `.gremlins.yaml`

Single config file at repo root:

```yaml
silent: false
test:
  timeout-coefficient: 3   # mutant runs get 3x the baseline test timeout
  cpu: 0                    # use all available cores
mutants:
  conditionals-boundary: { enabled: true }
  conditionals-negation: { enabled: true }
  increment-decrement:   { enabled: true }
  invert-negatives:      { enabled: true }
  arithmetic-base:       { enabled: true }
  remove-statement:      { enabled: true }
  branch-condition:      { enabled: true }
include:
  - "sql/..."
  - "comparator/..."
  - "omittable/..."
  - "database/..."
  - "cache/..."
  - "manifest/..."
  - "parser/..."
  - "cmd/sqlgen/internal/..."
  - "cmd/sqlgen/cli/..."
exclude:
  - "**/*_gen.go"
  - "**/*_test.go"
  - "cmd/sqlgen/testdata/..."
  - "tools/..."
```

### 4.2 Tool installation

Pinned binary in `.tools/`, never in any `go.mod`:

```makefile
GREMLINS_VERSION := v0.6.0  # pin in Makefile
.tools/gremlins:
	@mkdir -p .tools
	GOBIN=$(PWD)/.tools go install \
	  github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION)

mutation-install: .tools/gremlins
```

`.tools/` is `.gitignore`'d. CI installs fresh per run; humans install
once per version bump.

### 4.3 Thresholds (per-package + per-file)

`tools/mutation/thresholds.yaml` (initially empty, populated after
baseline). Both granularities supported; the higher-precision rule
wins when both match a file. Resolves §8 Q5.

```yaml
# Threshold = minimum required mutation score (killed / (killed + survived)).
# action: warn | fail
# precedence: per-file > per-package (file rule wins when both apply)

packages:
  # Coarse-grained, broad coverage:
  # sql:        { score: 0.75, action: warn }
  # comparator: { score: 0.80, action: fail }

files:
  # Hot-spot, fine-grained — overrides the package rule for these files:
  # comparator/sql_builder.go: { score: 0.90, action: fail }
  # parser/validate.go:        { score: 0.85, action: fail }

templates:
  # Template mutation scores tracked separately (different cost
  # profile, different mutator set):
  # cmd/sqlgen/internal/template/insert.tmpl: { score: 0.70, action: warn }
```

**Why both:** package-level thresholds give broad coverage with low
PR noise; file-level thresholds let hot-spot files (`sql_builder.go`,
`predicate.go`, dialect-quoting code) be held to a tighter bar
without forcing every file in the package to meet it. Per-file
thresholds are opt-in surgical tools; per-package is the default
discipline.

Two-stage rollout:

1. First quarter: collect baseline, no thresholds enforced.
2. Subsequent: set per-package thresholds at baseline minus 5%
   (regression slack); set per-file thresholds at baseline for
   2–3 known-critical files (`comparator/sql_builder.go`,
   `parser/validate.go`, etc.); ratchet upward as tests improve.

---

## 5. Workflows

### 5.1 Local development

```bash
make mutation-install            # one-time setup
make mutation-pkg PKG=comparator # mutate one package
```

Output: list of survived mutants with file:line and a diff hunk. Dev
reads, writes a test that kills the highest-value survivors, reruns.
Iteration time ~minutes per package.

### 5.2 Per-PR delta

`make mutation-diff` runs only on files changed in the PR vs `main`:

```bash
# tools/mutation/cmd/diff/main.go (sketch)
files := git diff --name-only origin/main...HEAD | grep \.go$ | exclude generated|tests|tools
for each file: gremlins unleash --file <file> --config .gremlins.yaml
aggregate: per-file score, compare to baseline[file]
```

Output: a PR comment summarizing per-file score deltas + a link to
`tools/mutation/PR-<n>.json` artifact. Gating off by default; opt-in
via `mutation-test` label.

### 5.3 Quarterly full run

GitHub Actions cron job. Matrix-by-package strategy per §6.2 to fit
free-tier limits:

```yaml
# .github/workflows/mutation-quarterly.yml
on:
  schedule: [ { cron: "0 6 1 */3 *" } ]   # 1st of every 3rd month
  workflow_dispatch:
jobs:
  go-mutation:
    strategy:
      matrix:
        package: [sql, comparator, omittable, database, cache, manifest, parser, cmd-sqlgen-internal, cmd-sqlgen-cli]
    timeout-minutes: 350
    steps:
      - make mutation-install
      - make mutation-pkg PKG=${{ matrix.package }} > out/${{ matrix.package }}.json
      - upload artifact

  template-mutation:
    timeout-minutes: 350
    steps:
      - make mutation-install
      - make mutation-tmpl-all > out/templates.json
      - upload artifact

  aggregate:
    needs: [go-mutation, template-mutation]
    steps:
      - download all artifacts
      - go run tools/mutation/cmd/compare baseline.json out/*.json
      - if changed: open PR refreshing baseline + summary in description
```

The PR pattern keeps baseline updates human-reviewed. Per-package
jobs run in parallel up to the concurrent-job limit; total
wall-clock is the slowest package, not the sum.

### 5.4 `/simplify` integration (proposal-first)

Mutation testing and `/simplify` interact: `/simplify` rewrites code,
which invalidates the mutation baseline for touched files. The naive
approach is "let baseline drift until the next quarterly refresh,"
but that means a refactor can silently weaken test-strength for a
quarter before anyone notices.

The accepted design is **proposal-first**. `/simplify` produces a
*proposed diff* (a patch file under `tools/mutation/proposals/<id>.patch`)
**before** modifying any source. The mutation harness scores the
proposal against the baseline; the human (or `/simplify` itself) sees
the score delta before deciding to apply. Resolves §8 Q4.

**Flow:**

```
1. /simplify              → emits proposed.patch (no source change)
2. make mutation-diff PROPOSAL=proposed.patch
   ├─ applies patch to sandbox copy of working tree
   ├─ runs mutation on files touched by the patch
   ├─ compares scores against baseline[file]
   └─ reports per-file delta: { file, before, after, Δ }
3. human reviews delta + diff together
4. if accepted: apply patch + refresh baseline[file] entries
5. if rejected: discard patch, no source touched
```

**Why this shape, not post-hoc rescoring:**

- Catches refactors that *degrade* test-strength before they land,
  not after.
- Keeps the baseline a moving floor rather than a frozen snapshot —
  baseline updates happen at the same moment as the code change,
  preserving the invariant "baseline reflects current
  test-strength."
- Makes the score delta a first-class review signal for
  `/simplify`'s diffs, the same way line count or test-pass status
  already are.

**Skill coordination:** `/simplify` gains a `--proposal` flag that
emits the patch without applying. The mutation harness exposes
`make mutation-diff PROPOSAL=<path>` as the consumer of that patch.
The two skills are loosely coupled — `/simplify` doesn't call
mutation; the human (or a downstream skill) chains them.

**Cost.** Per-file mutation on a small refactor takes minutes, fast
enough for the proposal-review loop. For multi-file refactors
touching hot-spot files, the user can opt out via `--no-mutation`
when iteration speed matters more than score floor enforcement.

### 5.5 Test-gap triage

`docs/tracker/test-gaps.md` is a tracked backlog of high-value
survived mutants:

```markdown
| ID | Package | File:Line | Mutator | Status | Notes |
|----|---------|-----------|---------|--------|-------|
| TG-001 | comparator | sql_builder.go:142 | conditionals-boundary | open | `len(args) > 0` survives flip to `>= 0`; no test for empty-args case |
| TG-002 | sql | predicate.go:88 | conditionals-negation | open | `WhereEq` predicate inversion survives; no equality-vs-inequality assertion |
```

Workflow:

1. Quarterly run produces list of new survivors.
2. **Automatic discard pass** via `tools/mutation/discard-rules.yaml`.
   Pattern-based rules drop structurally-low-value survivors before
   any human looks. Resolves §8 Q3. Rules grow from observed false
   positives; each rule includes a rationale string so the discard
   list is auditable:

   ```yaml
   rules:
     - id: defensive-nil-check
       match:
         mutator: conditionals-negation
         line_matches: 'if .*== nil'
       rationale: "Defensive nil-check inversion; covered transitively by callers"
     - id: fallthrough-cleanup
       match:
         mutator: remove-statement
         line_matches: '^\s*(return|continue|break)\s*$'
       rationale: "Removal of trivial control-flow terminator; equivalent in surrounding context"
   ```

3. Manual triage pass on what remains (or `/mutation-triage` skill,
   future):
   - Equivalent mutants → discard with a one-line rationale appended
     to `tools/mutation/discard-rationale.md`.
   - Real gaps → file `TG-NNN` row.
4. Convert high-value `TG-NNN` to sub-items in a "test hardening" phase
   or roll into the next phase's closure sweep.

Test gaps are not fixes. Don't put them in `docs/tracker/fixes.md`.

---

## 6. Performance reality

### 6.1 Cost shape

Cost per mutant run = test suite wall-clock. Total cost = mutant count
× test wall-clock.

Estimates against current sqlgen `make check` time (~50s for the cli
module under `-race`):

| Scope | Mutant count (estimate) | Wall-clock (2-core) | Wall-clock (4-core) |
|---|---|---|---|
| One file (~200 LOC) | ~30 mutants | ~25 min | ~13 min |
| One package (`comparator/`) | ~300 mutants | ~4 hours | ~2 hours |
| Full Go-source repo | ~3000–5000 mutants | ~40 hours | ~20 hours |
| Full template-mutation pass | ~50 mutants × 6 min | ~5 hours | ~3 hours |

### 6.2 Free-tier CI strategy

Resolves §8 Q1. Target: fit quarterly runs into GitHub Actions free
tier (2–4 core `ubuntu-latest`, 6-hour per-job timeout, unlimited
minutes for public repos / 2000 min/month for private).

Single-job full-repo Go mutation does **not** fit. The strategy:

- **Matrix-by-package.** The quarterly workflow uses a GitHub Actions
  matrix strategy: one job per included package. Each job runs
  `make mutation-pkg PKG=<pkg>`, fits comfortably under the 6-hour
  cap, and they run in parallel up to the concurrent-job limit. An
  aggregation job merges per-package JSON outputs into the baseline
  refresh PR.
- **Templates as a dedicated job.** Template mutation runs as its own
  parallel job at ~3–5 hours on 4-core; still under the cap. Uses
  `make mutation-tmpl-all`.
- **Per-PR delta is the always-on mode.** Mutates only files touched
  by the PR (Go and `.tmpl` both honored). Wall-clock under 15
  minutes on typical PRs, well within free-tier comfort.
- **Aggregate budget tracking.** `tools/mutation/cmd/budget` accepts
  monthly minute cap (configurable via repo variable) and skips the
  quarterly run if remaining budget is insufficient — fail-safe for
  private-repo metering.

### 6.3 Optimizations

- **Per-package test scope.** When mutating a file in `sql/`, only run
  `sql/...` tests, not the full repo. Configured via gremlins's
  `test-cpu` and per-mutation test selection.
- **`-short` mode.** Tests gated by `testing.Short()` (integration
  tests, testcontainer tests) are skipped during mutation runs. The
  metamorphic / integration layer is the schema fuzzer's job; mutation
  testing focuses on unit-level assertion strength.
- **Per-PR delta mode.** Only mutate touched files. Per-PR wall-clock
  collapses to minutes.

### 6.4 What we accept

Mutation testing is fundamentally slow. The design accepts this and
positions the technique as a quarterly health check + per-PR safety
net, not a commit-cadence gate. The matrix-by-package strategy keeps
the technique tractable on free-tier infrastructure; if the project
later moves to paid runners, the matrix collapses into a single
larger job at no design cost.

---

## 7. Sub-item preview

Indicative breakdown; finalized at `/phase <N>` time.

### N.1 Tool adoption + Makefile wiring (Go source)

- Pin `gremlins` version in Makefile; `.tools/` install path.
- `.gremlins.yaml` with §4.1 contents.
- `make mutation-install`, `make mutation-pkg PKG=...`, `make
  mutation` targets.
- README at `tools/mutation/README.md` explaining local usage.

### N.2 First baseline run + analysis (Go source)

- Run `make mutation` against full repo via matrix-by-package (§6.2),
  capture `tools/mutation/baseline.json`.
- Triage round-1: seed `discard-rules.yaml` with the most common
  low-value patterns surfaced.
- Document per-package starting scores in
  `tools/mutation/baseline-report.md`.

### N.3 Per-PR delta mode + advisory CI

- `tools/mutation/cmd/diff/` Go program.
- `make mutation-diff` target.
- GitHub Actions workflow (`.github/workflows/mutation-pr.yml`) that
  runs on every PR but reports advisory-only (no gate).

### N.4 Auto-discard rules engine

- `tools/mutation/discard-rules.yaml` schema + loader.
- Apply discard rules in `cmd/compare/` between gremlins output and
  the human-facing report.
- Seed with the patterns identified in N.2.

### N.5 Test-gap tracker + initial triage

- `docs/tracker/test-gaps.md` template + first batch of `TG-NNN`
  entries from N.2's survivors (post-auto-discard).
- `tools/mutation/discard-rationale.md` for case-by-case manual
  discards that don't generalize into auto-discard rules.
- Tracker hooks: `/test-gap add / list / resolve` (mirrors the
  existing `/fix` skill shape, separate command).

### N.6 Template mutation harness

- `tools/mutation/tmpl/` custom harness per §3.5.
- `.template-mutation.yaml` config file.
- Template mutator set: `cond-negation`, `eq-swap`, `range-drop-body`,
  `pipeline-drop`, `else-swap` (start; `literal-flip` opt-in).
- `make mutation-tmpl FILE=...` + `make mutation-tmpl-all` targets.
- First baseline run on templates; survivors go into the same
  `TG-NNN` tracker with `kind: template` tag.

### N.7 Quarterly full-run workflow (free-tier matrix)

- `.github/workflows/mutation-quarterly.yml` per §6.2: matrix-by-
  package for Go source + dedicated template job + aggregation job.
- `tools/mutation/cmd/budget/` for monthly-minute fail-safe (private
  repo concern; no-op for public).
- Auto-PR for baseline refresh with summary in body.
- Documentation: how to interpret a quarterly diff.

### N.8 Threshold enforcement (per-package + per-file)

- `tools/mutation/cmd/compare/` Go program comparing run to
  thresholds, honoring per-file precedence over per-package (§4.3).
- `tools/mutation/thresholds.yaml` populated:
  - Per-package: 2–3 highest-priority packages (`comparator/`,
    `sql/`, `omittable/` likely candidates).
  - Per-file: 2–3 hot-spot files held to tighter bars
    (`comparator/sql_builder.go`, `parser/validate.go`).
  - Per-template: 1–2 highest-leverage templates if N.6 surfaces
    obvious candidates.
- Per-PR mode upgraded from advisory to gating for files / templates
  in thresholded sets.

### N.9 `/simplify` proposal-first integration

- `/simplify` skill gains `--proposal <path>` flag emitting a patch
  without applying.
- `make mutation-diff PROPOSAL=<path>` consumes the patch: applies
  it to a sandbox copy, runs file-scoped mutation, reports score
  delta vs baseline.
- Documentation in `tools/mutation/README.md` describing the §5.4
  flow end-to-end (with a worked example).
- No automatic enforcement — the score delta is advisory; human
  decides whether to apply.

### N.10 Initial test-gap closure sprint

- Land tests killing the top-20 high-value Go-source survivors and
  the top-5 template survivors.
- Demonstrate the workflow end-to-end: gap identified → test written
  → mutant killed → score rises → threshold tightened.
- Mark relevant `TG-NNN` resolved.

### N.11 Phase closure sweep

- `make check` clean (mutation infra itself doesn't affect runtime
  tests).
- `make mutation` clean against the post-N.10 baseline (matrix
  job + template job).
- Documentation pass: `tools/mutation/README.md` covering all
  workflows from §5 including the `/simplify` integration.
- Tracker update. No PRD sync (mutation testing is internal
  tooling, mirrors the schema fuzzer's no-PRD posture).

---

## 8. Open questions

Track here until resolved; on closure copy to a "Resolved" subsection
with date + outcome. Empty open-questions section is the gating signal
for `/phase` breakdown.

### Open

(None — all resolved 2026-05-15.)

### Resolved

1. **CI runner sizing.** *Resolved 2026-05-15:* Target the GitHub
   Actions free tier (2–4 core `ubuntu-latest`, 6-hour per-job
   timeout). Strategy: matrix-by-package for full Go-source runs
   (each package as a parallel job, all fit under the cap), dedicated
   template-mutation job in parallel, aggregation job merges results.
   Per-PR delta mode is the always-on cadence and fits comfortably
   in free-tier limits. Reflected in §6.2. Paid runners or self-
   hosted infrastructure are not required; if available later, the
   matrix collapses into a single larger job at no design cost.
2. **Mutating template files.** *Resolved 2026-05-15:* In scope.
   gremlins cannot parse Go templates, so a custom harness at
   `tools/mutation/tmpl/` covers `*.tmpl` files using the existing
   example test suite as the oracle. Mutator set covers conditional
   negation, dialect-eq swaps, range-body drops, pipeline-function
   removal (especially `quoteIdent`), and else-swaps. Cost is
   significant (~3–5h for a full template run) but fits under the
   free-tier 6-hour cap as a dedicated job. Reflected in §2, §3.2,
   §3.4, §3.5, §4.3, §6.1, §7 (N.6).
3. **Survivor-discard automation.** *Resolved 2026-05-15:* Yes,
   build `tools/mutation/discard-rules.yaml` with pattern-based
   auto-discard applied before the human-facing report. Each rule
   carries a `rationale` string so discards are auditable. Manual
   case-by-case discards continue to land in
   `tools/mutation/discard-rationale.md` for cases that don't
   generalize. Reflected in §2 and §5.5; dedicated sub-item N.4.
4. **Integration with `/simplify`.** *Resolved 2026-05-15:*
   Proposal-first integration. `/simplify` produces a *proposed
   diff* (patch file) before any source modification;
   `make mutation-diff PROPOSAL=<path>` scores the proposal against
   the baseline so the human sees the score delta before applying.
   This makes mutation-score delta a first-class review signal for
   refactors and keeps the baseline a moving floor rather than a
   frozen snapshot. Reflected in §2 and §5.4; dedicated sub-item
   N.9.
5. **Per-package vs per-file thresholds.** *Resolved 2026-05-15:*
   Support both. Per-file rules win over per-package when both
   match (precedence rule). Per-package gives broad coverage with
   low PR noise; per-file lets hot-spot files
   (`comparator/sql_builder.go`, `parser/validate.go`) be held to
   tighter bars. Per-template thresholds also supported in the same
   `thresholds.yaml`. Reflected in §4.3 and §7 (N.8).

---

## 9. Carry-over notes

- Mutation testing reveals *test* problems, not code problems. Don't
  treat survived mutants as bugs in the production code; treat them
  as bugs in the test suite.
- A 100% mutation score is neither achievable nor desirable —
  equivalent mutants always exist, and some surviving mutants are
  genuinely benign (dead defensive code, optimizations the compiler
  would elide). Aim for high-and-rising, not perfect.
- Resist running mutation testing per-commit. The cost is real and
  the signal is no sharper than running it weekly. Per-PR delta is
  the right "always on" cadence; full runs are quarterly.
- Mutation testing strengthens the metamorphic invariants from the
  schema fuzzer transitively: a fuzzer-discovered bug always becomes
  a regression test; mutation testing then verifies that regression
  test is *strong* (would catch a related variant of the bug, not
  just the exact one). The two techniques compound.
