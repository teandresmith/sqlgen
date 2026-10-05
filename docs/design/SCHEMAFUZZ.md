# SQLGen Schema Fuzzer — Design & Plan

> **Status: DESIGN FROZEN (2026-05-15).** Sibling design doc for an
> automated bug-discovery harness that generates random schemas, runs
> them through the full sqlgen pipeline, and shrinks failures to
> minimum reproducers. Output is `/fix add` candidates with permanent
> regression seeds. §13 open-questions section emptied 2026-05-15 (0
> open, 5 resolved) — gating signal for `/phase` breakdown. The
> manifest gives the fuzzer a stable structural signature for failure
> clustering, but the fuzzer itself does **not** hard-depend on the
> manifest. See §9.
>
> **Pattern:** parallels `docs/design/MANIFEST.md` (PRD §30 — synced) and
> `docs/design/MCP.md` (Phase 19) as a sibling design doc that fronts open
> questions before PRD sync.
>
> **Phase:** TBD — provisionally Phase 20 (after Phase 19 / MCP closure).
> Can re-slot earlier if Phase 18 / 19 schedules shift.
>
> **Soft dependency:** Manifest schema v1.0.0 (Phase 18) is consumed as a
> structural-signature input for failure clustering. The fuzzer degrades
> gracefully to error-string clustering when the manifest is absent, so
> can be built in parallel with Phase 18 / 19. **Hard dependency:** none.

---

## 1. Motivation

The current test surface for sqlgen is curated. `cmd/sqlgen/testdata/examples/`
holds 8 hand-built example modules across dialects, and `make
check-examples` + `make test-integration` exercise the golden paths +
documented edge cases. This catches regressions in known territory.

It does **not** catch:

- Schema shapes the authors didn't think of.
- Feature *combinations* that no example exercises (e.g.,
  `multi-tenant × soft-delete × composite-PK × jsonb[] × cache`).
- Template branches that work in isolation but emit broken code when
  multiple conditionals fire on the same column / table.
- Identifier-quoting and parameter-binding bugs triggered by unusual but
  valid column names (reserved words, leading underscores, unicode).
- Order-dependent template bugs surfaced only by specific FK topologies
  (cycles, self-reference, multi-column FKs).

Generators are particularly prone to combinatorial bugs because templates
branch on schema features. With ~10 orthogonal config axes and dozens of
schema-shape axes, hand-curated examples cover a tiny slice of the
input space. A property-based fuzzer with shrinking explores the rest
mechanically.

### 1.1 What the fuzzer adds

Three things a curated suite cannot:

1. **Exploration.** Random valid schemas hit feature interactions the
   author never wrote down. Empirically, this is the highest-yield
   bug-finding technique for code generators (CSmith, SQLsmith).
2. **Shrinking.** A failure on a 200-column schema is useless; a
   delta-debugger reduces it to a 2-column minimum repro that goes
   straight into `testdata/examples/regressions/`.
3. **Compounding coverage.** Every found bug becomes a permanent
   regression schema. The corpus grows monotonically; surface area
   ratchets upward over time without manual curation.

### 1.2 Why a separate phase

This is infrastructure, not a user-facing feature. It does not appear in
the PRD as a generated artifact or config knob — it lives entirely under
`tools/schemafuzz/` and emits findings into the existing `/fix` workflow.
A dedicated phase keeps it from blocking shippable features (Phase 18
Manifest, Phase 19 MCP) and lets the fuzzer absorb feedback from those
phases — particularly the manifest, which it uses as a structural
signature.

---

## 2. Scope

**In scope:**

- New tooling module `tools/schemafuzz/` with its own `go.mod` so dev
  dependencies (gopter, testcontainers usage patterns) never enter the
  runtime / parser / cli modules.
- A property-based random schema generator producing structurally valid
  DDL + matching `sqlgen.yaml` config across all three dialects.
- A pipeline runner that for each generated schema executes:
  `sqlgen generate` → `go vet ./...` → `go build ./...` → metamorphic
  test suite against a testcontainer.
- A shrinker (delta-debugging algorithm) that minimizes failures to
  the smallest schema that still reproduces.
- A finding emitter that writes minimum repros to
  `docs/tracker/fuzz-findings/<date>-<sig>.yaml`, deduplicated by
  signature.
- A metamorphic test suite of 10-15 dialect-independent invariants
  (Count = len(Find), Insert→Get round-trips, soft-delete semantics,
  tenancy scoping, pagination consistency, ...).
- A seed corpus loader that primes the fuzzer with the existing
  `testdata/examples/` schemas so day-one runs match current coverage
  before exploring novel shapes. The loader reads from HEAD on each
  run (no snapshot pin) — the corpus evolves with the codebase. See
  §13 resolved Q4.
- Budgeted execution (`--budget 10m`, `--seed N`) — runs are bounded by
  wall-clock or iteration count.
- `make schemafuzz` Makefile targets.
- CI integration: nightly job with a 20-minute budget **per dialect**
  (~1 hour total wall-clock across postgres + mysql + sqlite), posting
  new findings as `/fix add` candidates. See §13 resolved Q1.
- **Docker required.** Metamorphic-stage testcontainers are a hard
  dependency. Environments without docker cannot run the fuzzer; CI
  runners that don't have docker are unsupported. See §13 resolved Q2.

**Out of scope:**

- Byte-level parser fuzzing — that is `go test -fuzz` on the parser
  package and lives as a separate, smaller effort (see §11).
- Mutation testing — sibling design doc `docs/design/MUTATION.md` covers it.
- Performance fuzzing / load testing.
- Automatic fix synthesis — findings are humans-write-the-fix; the
  fuzzer never edits source.
- Persistent service mode — the fuzzer is a one-shot CLI; cron / CI
  drives cadence.
- Cross-database differential testing (running the same logical query
  against postgres + mysql + sqlite and comparing results). Useful but
  separate; see §11.

---

## 3. Architecture

### 3.1 Module layout

```
tools/schemafuzz/
  go.mod                              # dev-only deps, isolated module
  cmd/schemafuzz/main.go              # CLI entrypoint
  internal/
    gen/
      schema.go                       # random schema generator (gopter)
      types.go                        # dialect-aware type sampling
      ddl.go                          # Schema → DDL emitter
      cfg.go                          # Schema → sqlgen.yaml emitter
      idents.go                       # identifier generator (incl. edge cases)
    pipeline/
      run.go                          # generate → vet → build → metamorphic
      sandbox.go                      # ephemeral go module workspace
      sqlgen.go                       # invokes sqlgen binary
    oracle/
      stage.go                        # outcome enum + stage labels
      panic.go                        # parser must not panic
      compile.go                      # generated must vet + build
      metamorphic.go                  # property invariants
    shrink/
      shrink.go                       # delta-debugging shrinker
      variants.go                     # smaller-variant enumeration
    report/
      signature.go                    # error signature hashing
      finding.go                      # yaml emitter
      dedup.go                        # signature-based dedup
    corpus/
      seed.go                         # load testdata/examples as seeds
      replay.go                       # re-run minimum repros from disk
  testdata/
    seeds/                            # symlinks to canonical examples
    expected-findings/                # for fuzzer's own unit tests
  Makefile.fragment                   # included by repo Makefile
```

### 3.2 Three-module rule compliance

The fuzzer is **not** a fourth module — it is a tooling subproject
sitting alongside the runtime, parser, and cli. Its `go.mod` declares
the sqlgen CLI binary as the unit under test (invoked as a subprocess,
not imported), so it has no compile-time coupling to any of the three
existing modules. This isolates dev dependencies (gopter,
testcontainers patterns, possibly `dolthub/go-mysql-server` for stub
testing) entirely.

The fuzzer **may** import the manifest runtime types package once
Phase 18 lands, since that package is stdlib-only and read-only.

---

## 4. Schema generator

### 4.1 Generation strategy

The generator produces *structurally valid* schemas, not random bytes.
This is the key design choice: we are testing the generator's response
to legal-but-unusual schemas, not the parser's response to garbage.
Byte-level parser fuzzing is a separate concern (§11).

Generator axes, sampled per-schema with configurable distributions:

| Axis | Range | Notes |
|---|---|---|
| Dialect | {postgres, mysql, sqlite} | One per run; fuzzer runs per-dialect |
| Tables | 1–12 | Skew toward small (most bugs reproduce there) |
| Columns/table | 1–20 | |
| Column types | dialect-specific set | int{2,4,8}, text, varchar(N), bool, timestamp(tz), uuid, json{b}, json{b}[], decimal(p,s), bytea, enum |
| Nullability | weighted bool | ~30% nullable |
| Defaults | optional | literal / function / sequence per dialect |
| PK kind | {single-int, single-uuid, composite, none} | "none" only where dialect allows |
| FKs | 0..N per table | Includes self-reference + cycles |
| FK actions | {cascade, restrict, set null} | |
| Indexes | 0..3 per table | Single + multi-column, unique + non-unique |
| Identifiers | curated edge cases + random | Reserved words, mixed case, unicode, leading underscore, max-length |

### 4.2 Feature axes (sqlgen config)

Independent of schema shape, the generator samples a sqlgen.yaml config:

| Feature | Values |
|---|---|
| `tenancy` | off, single-tenant, multi-tenant |
| `soft_delete` | off, timestamp, boolean |
| `cache` | off, in-memory |
| `events` | off, on |
| `manifest` | off, single, per-entity |
| Integrations | {none, uuid, uuid+decimal, uuid+decimal+ksuid} |
| GraphQL | off, nested (`<output.dir>/graph`), top-level sibling |

Combination strategy: **pairwise sampling** as the default (every pair
of axis values appears at least once → ~30–60 combos), with optional
**risk-weighted top-up** for known-fragile triads
(`graphql × cache × tenancy`, `manifest × graphql`, ...). Full
combinatorial is gated behind `--exhaustive` and reserved for
nightly / weekly runs.

### 4.3 Identifier generator

A dedicated identifier generator covers cases where bugs commonly hide:

- Reserved words per dialect (`order`, `select`, `table`, `key`, ...).
- Quoting-sensitive identifiers (mixed case, leading underscore,
  trailing underscore, all uppercase).
- Unicode (cyrillic, CJK, combining marks) — only emitted to dialects
  that documented support.
- Maximum identifier length per dialect (postgres 63, mysql 64,
  sqlite no limit but stress at 255).
- Names that collide with Go reserved words and stdlib identifiers
  (`type`, `func`, `string`, `Error`).

Distribution: 60% random alphanumeric, 25% "interesting" cases above,
15% identifiers drawn from the seed corpus (real-world-ish names).

### 4.4 Validity gates

The generator runs a pre-flight validity check before emitting DDL:
FK targets exist, composite PK columns are NOT NULL, identifier lengths
fit, etc. Invalid schemas are discarded silently and re-rolled. The
fuzzer never reports parser rejections of its own invalid output as
findings — that would be self-induced noise.

---

## 5. Pipeline runner

### 5.1 Stages

For each (schema, config) pair the runner executes:

1. **Workspace setup.** `t.TempDir()` under `os.TempDir()`. Initialize
   `go.mod` with `go 1.26`. Symlink replacement for the local sqlgen
   runtime module so the generated package compiles against the
   in-tree version under test.
2. **Schema + config emission.** Write `schema.sql`, `sqlgen.yaml`.
3. **Generate.** Invoke `sqlgen generate`. Capture stdout, stderr,
   exit code. A panic (non-zero exit with stack trace on stderr) is
   always a finding. A typed error on a schema the validity gate
   passed is also a finding (parser/validator inconsistency).
4. **Static checks.** `go vet ./...` on the generated package. Then
   `go build ./...`. Any failure is a finding.
5. **Metamorphic suite.** Spin up a testcontainer (dialect-matched),
   run schema migrations, then execute the metamorphic invariants
   against the generated client. See §6.
6. **Teardown.** Drop the testcontainer, remove the workspace (unless
   `--keep-on-failure`).

### 5.2 Sandboxing

Each run is hermetic:

- Ephemeral tempdir, no shared state.
- Pinned `sqlgen` binary built once per fuzzer run from the working
  tree's HEAD.
- Testcontainers per-iteration (slow but correct); optional
  `--reuse-container` flag for local dev with `TRUNCATE`-based reset
  between iterations.

### 5.3 Budgeting

```
schemafuzz --budget 10m --workers 4 --seed 1234567890 --dialect postgres
```

- `--budget` is wall-clock. The runner exits cleanly at budget
  expiration after the current iteration finishes.
- `--workers` parallelizes (each worker has its own testcontainer).
  Default: `min(runtime.NumCPU()/2, 4)`. Cap at 4 to avoid saturating
  docker on typical dev laptops; CI runners can override via flag.
  See §13 resolved Q5.
- `--seed` makes runs reproducible.
- `--dialect` runs against a single dialect (default: rotate).

---

## 6. Metamorphic oracle

The metamorphic suite is the highest-signal layer of the fuzzer. It
catches bugs that compile and vet clean but produce incorrect runtime
behavior. Invariants must hold across all dialects, configs, and
schema shapes.

### 6.1 Invariants (initial set)

| # | Invariant | Why it catches bugs |
|---|---|---|
| M1 | `Count(pred) == len(Find(pred))` | Predicate translator divergence between Count and Find paths |
| M2 | `Insert(x); Get(x.ID) == x` (round-trip) | Scan / value-binding bugs, type coercion bugs |
| M3 | `Find(WhereEq(c,v))` returns only rows with `row.c == v` | Predicate-builder bugs, identifier quoting bugs |
| M4 | `SoftDelete(x); Find()` excludes `x`; `Find(IncludeDeleted)` includes | Soft-delete filter injection in WHERE / JOIN |
| M5 | Pagination sum: `Σ len(page_i) == Count()` over full traversal | Cursor encoding, OFFSET/LIMIT off-by-one |
| M6 | Cross-tenant: rows inserted as tenant A never returned for tenant B | Tenancy filter injection completeness |
| M7 | `Find().OrderBy(c, Asc)` is monotonically non-decreasing on `c` | ORDER BY emission, nullability ordering |
| M8 | `Update(x, NewValue); Get(x.ID).Field == NewValue` | UPDATE column targeting, RETURNING clause |
| M9 | `Delete(x); Get(x.ID) → ErrNotFound` | DELETE WHERE construction |
| M10 | Round-trip via JSON column preserves the input value | JSON marshal/unmarshal, nullable JSON |
| M11 | FK traversal: `parent.Children()` returns exactly the rows whose FK == parent.ID | Relationship loader correctness |
| M12 | Index hints in EXPLAIN reference declared indexes | Index emission completeness (when declared in schema) |
| M13 | Two identical inserts on a unique-constrained column: second returns the dialect-mapped uniqueness error | Error mapping completeness |
| M14 | Transaction rollback: changes inside aborted tx are not visible after rollback | Transaction wrapper correctness |
| M15 | Events: every successful mutation produces one event of matching kind | Event emission completeness (when events enabled) |

Each invariant is a small Go test function in
`tools/schemafuzz/internal/oracle/metamorphic.go`, parametrized over
the generated client. Adding new invariants is the primary lever for
expanding fuzzer coverage post-launch.

### 6.2 Data generator

Metamorphic invariants need realistic row data, not random bytes.
The oracle has its own row-data generator that produces per-column
values respecting type, nullability, and uniqueness constraints. For
foreign keys, parent rows are inserted before children.

### 6.3 Tolerance for nondeterministic dialect quirks

Some invariants need dialect-specific tolerance:

- ORDER BY of NULLs: postgres NULLs LAST by default for `ASC`, mysql
  NULLs FIRST. The M7 oracle reads the dialect to assert the right
  shape.
- Float comparison: equality round-trip uses `cmp.Diff` with
  `cmpopts.EquateApprox` for floats.
- Timestamp precision: postgres microsecond, mysql second by default
  (depends on column declaration). The data generator avoids
  sub-second timestamps unless the declared precision permits.

---

## 7. Shrinker

### 7.1 Algorithm

Standard delta-debugging adapted to schemas. Given a failing schema
`S`, the shrinker enumerates *smaller variants* and tries each:

```
func Shrink(s *Schema, repro func(*Schema) bool) *Schema {
    for changed := true; changed; {
        changed = false
        for _, candidate := range smallerVariants(s) {
            if repro(candidate) {
                s = candidate
                changed = true
                break
            }
        }
    }
    return s
}
```

Smaller variants, tried in order (cheapest first):

1. Drop a table (skipping tables referenced by surviving FKs).
2. Drop a column (skipping PK columns and FK columns).
3. Drop an index.
4. Drop an FK constraint.
5. Simplify a column type (e.g., `varchar(255)` → `text`,
   `jsonb[]` → `jsonb`).
6. Set a column to NOT NULL or remove its default.
7. Disable a feature (tenancy → off, soft_delete → off, cache → off, ...).
8. Simplify an identifier (`"My Table"` → `mytable`).

Termination: fixed point — no smaller variant reproduces. Typical
shrink runs converge in seconds-to-minutes from a 12-table starting
schema to a 1–3 table minimum.

### 7.2 Caching

Shrinking is expensive (each variant runs the full pipeline). The
shrinker caches `(schema-hash) → outcome` so revisiting an already-tried
variant is free. Cache is per-finding, in-memory only.

### 7.3 Shrinker confidence

The shrinker reports the *number of variants tried* in the finding
metadata. A finding that survived 200+ shrink attempts is a tight
minimum; one that converged after 10 may benefit from a longer manual
pass. Heuristic only — humans should not rerun the shrinker by hand.

---

## 8. Findings and signatures

### 8.1 Signature

Each failure gets a stable signature for deduplication:

```
sig = sha256(
    stage ||
    canonical_error_message(stderr) ||
    manifest_diff_skeleton(generated, baseline)  // Phase 18+
)
```

Where:

- `canonical_error_message` strips file paths, line numbers, and
  RNG-derived identifiers, leaving the structural form of the error.
- `manifest_diff_skeleton` (post-Phase 18) compares the failing
  generated package's manifest to a known-good baseline at the same
  feature configuration. Differences in *what was generated* are part
  of the signature, not just *what failed*. Pre-Phase 18 the
  signature is error-string-only and produces more dupes.

### 8.2 Finding file format

```yaml
# docs/tracker/fuzz-findings/2026-06-01-a3f7b2.yaml
signature: a3f7b2c891
first_seen: 2026-06-01T03:14:00Z
last_seen: 2026-06-01T03:14:00Z
hit_count: 1
seed: 1234567890
budget_elapsed: 4m12s
stage: metamorphic
invariant: M1 (Count != len(Find))
dialect: postgres
shrink_iterations: 47
schema: |
  CREATE TABLE t (
    id uuid PRIMARY KEY,
    "order" jsonb[] NOT NULL
  );
sqlgen_yaml: |
  generation:
    tenancy: multi-tenant
    soft_delete: { strategy: timestamp }
expected: 0
actual: 3
stderr: |
  metamorphic: M1 violated: Count(WhereEq(id, X))=0 but len(Find(WhereEq(id, X)))=3
reproduce: |
  schemafuzz replay docs/tracker/fuzz-findings/2026-06-01-a3f7b2.yaml
```

### 8.3 Dedup and triage

Before writing a finding, the fuzzer checks
`docs/tracker/fuzz-findings/` for an existing file with the same
signature. If found:

- Increment `hit_count`, update `last_seen`.
- Compare schema sizes; if the new minimum is smaller, replace the
  stored schema with the smaller one.
- Otherwise no-op.

Triage layer (manual or Claude-driven `/fuzz-triage` skill — out of
scope for this phase):

- New finding → `/fix add` with the YAML inlined as the repro.
- Resolved fix → finding's schema becomes a regression seed in
  `cmd/sqlgen/testdata/examples/regressions/<sig>/`.
- Finding stops reproducing for >30 days → mark stale, archive.

**Retention policy:** `docs/tracker/fuzz-findings/` is allowed to grow
unbounded for now. Each finding retains replay value as long as its
minimum-repro yaml is preserved; pruning resolved findings would lose
that. Compaction can be revisited if the directory becomes
operationally unwieldy. See §13 resolved Q3.

### 8.4 Replay

`schemafuzz replay <finding-file>` re-runs a stored finding against
the current sqlgen build. Returns exit 0 if the bug is fixed, non-zero
if it still reproduces. This is the cheap CI gate that confirms
resolved fixes stay resolved without graduating every finding to a
full regression example.

---

## 9. Manifest integration (post-Phase 18)

When Phase 18 closes, the fuzzer gains two manifest-dependent
capabilities:

1. **Structural signatures.** §8.1 — manifest diff skeleton enters the
   signature, sharpening dedup.
2. **Schema-skeleton oracle.** A new invariant M16: "the generated
   manifest's entity count equals the input schema's table count
   (modulo per-table opt-out)." Trivially catches manifest builder bugs
   the metamorphic suite would miss.

Neither is required for v1. Fuzzer ships with error-string signatures;
manifest integration is an additive sub-item.

---

## 10. Testing the fuzzer itself

The fuzzer has its own tests (`tools/schemafuzz/internal/.../*_test.go`):

- **Generator tests.** Generated schemas are always parser-valid: round
  through `sqlgen` and confirm no rejection. Done over a fixed seed
  corpus for determinism.
- **Shrinker tests.** Given a hand-crafted "failing" schema and a
  trivial reproducer predicate, the shrinker converges to the
  expected minimum. Table-driven.
- **Signature tests.** Two semantically-identical failures with
  different random identifiers must produce the same signature.
- **Replay tests.** A stored finding YAML round-trips through replay
  without drift.

These run in `make check` like any other test — they are unit tests of
the fuzzer's logic, not bug-discovery runs. The bug-discovery runs are
budgeted CI / nightly jobs, separate from the test suite.

---

## 11. Adjacent techniques (deferred / cross-reference)

Listed for orientation; each warrants a separate phase or design doc.

- **Native parser fuzzing.** `go test -fuzz=FuzzParse ./parser/...`
  with a seed corpus of valid DDL. Contract: never panic, always
  return a typed error or a valid AST. Half-day effort, runs in CI's
  existing fuzz lane. Not part of this phase.
- **Mutation testing.** Sibling design doc `docs/design/MUTATION.md`. Different
  goal (test-suite strength, not bug discovery), different tool
  (`gremlins`), different cadence (quarterly + per-PR delta).
- **Cross-dialect differential testing.** Run the same logical
  operation against postgres + mysql + sqlite testcontainers,
  compare results. Surface where dialect semantics should match but
  don't. Higher-value but more design-heavy because cross-dialect
  invariants are subtler than within-dialect ones. Candidate for a
  later phase.

---

## 12. Sub-item preview

Indicative breakdown; finalized at `/phase <N>` time.

### N.1 Module scaffold + generator core

- `tools/schemafuzz/go.mod` with isolated deps.
- `tools/schemafuzz/internal/gen/` — schema generator over the
  postgres dialect first, with a fixed-seed snapshot test confirming
  reproducibility.
- `tools/schemafuzz/cmd/schemafuzz/main.go` skeleton with `--budget`,
  `--seed`, `--dialect`, `--workers` flags.
- One end-to-end smoke test: generate → emit DDL → emit yaml → exit.
- `make schemafuzz` target.

### N.2 Pipeline runner + sandbox

- `pipeline/sandbox.go`: hermetic tempdir, go-module init, runtime
  replacement.
- `pipeline/run.go`: stages 1–4 (workspace, emit, generate, vet,
  build). No metamorphic yet.
- Reports stage failures as findings with stub signatures.
- Tests: hand-fed schema that vets/builds; hand-fed schema that
  intentionally breaks generation; both should be detected
  correctly.

### N.3 Metamorphic oracle — initial 8 invariants

- M1 (Count = len(Find)), M2 (Insert→Get round-trip), M3 (WhereEq
  semantics), M5 (pagination sum), M7 (OrderBy monotonic), M8
  (Update→Get), M9 (Delete→NotFound), M11 (FK traversal).
- Testcontainer wiring for postgres only initially.
- Row-data generator for the supported postgres types.
- Postgres-only run; mysql + sqlite added in N.6.

### N.4 Shrinker

- Delta-debugging algorithm with the 8 variant strategies from §7.1.
- Per-finding cache.
- Shrinker self-tests via a hand-crafted reproducer predicate.

### N.5 Findings + dedup + replay

- Signature hashing (error-string only; manifest integration
  deferred to N.8).
- Finding YAML emitter.
- `schemafuzz replay` subcommand.
- `docs/tracker/fuzz-findings/` directory with README.
- Dedup logic: signature collision → update hit_count, replace
  schema if smaller.

### N.6 Mysql + sqlite dialect support

- Dialect-aware type sampling, identifier rules, DDL emission.
- Per-dialect testcontainer wiring.
- Metamorphic invariants verified to hold across all three
  dialects.

### N.7 Feature-axis sampler + pairwise

- Config generator with the §4.2 axes.
- Pairwise sampling implementation.
- Risk-weighted top-up table.
- `--exhaustive` flag.

### N.8 Manifest integration (gated on Phase 18 closure)

- Manifest-diff signature component (§8.1).
- M16 invariant (entity count vs table count, §9).
- Gracefully no-op when manifest disabled in the generated config.

### N.9 Metamorphic invariants — full set

- Add M4 (soft-delete), M6 (tenancy), M10 (JSON round-trip), M12
  (index hints), M13 (uniqueness errors), M14 (transactions), M15
  (events).
- Each gated on the corresponding feature being enabled in the
  generated config — invariant skipped when feature off.

### N.10 CI integration + nightly job

- GitHub Actions workflow: nightly, 20-minute budget, all dialects.
- Output: new findings posted as PR comments or issues
  (implementation detail TBD).
- Per-PR mode: short-budget run on diff-affected templates only
  (optional; opt-in via label).

### N.11 Phase closure sweep

- `make check` clean across all modules.
- Self-tests under `tools/schemafuzz/` clean under `-race`.
- Documentation pass: `tools/schemafuzz/README.md` covering local
  usage, replay, and triage workflow.
- Tracker update; PRD sync only if the fuzzer surfaces a permanent
  user-facing artifact (it does not, so no PRD section
  is anticipated — the fuzzer is internal tooling).

---

## 13. Open questions

Track here until resolved; on closure copy to a "Resolved" subsection
with date + outcome. Empty open-questions section is the gating signal
for `/phase` breakdown.

### Open

(None — all resolved 2026-05-15.)

### Resolved

1. **CI runner cost.** *Resolved 2026-05-15:* Accept the 20-minute
   budget per dialect (~1 hour total wall-clock nightly across
   postgres + mysql + sqlite). Reflected in §2 and §5.3. Weekly
   cadence reserved as a fallback if CI minutes become a constraint
   later.
2. **Cross-OS support.** *Resolved 2026-05-15:* Docker is a hard
   requirement. The metamorphic stage cannot degrade gracefully — vet
   + build-only mode would drop the highest-signal layer of the
   oracle. Environments without docker are out of scope; CI runners
   that lack docker are unsupported. Reflected in §2.
3. **Findings retention.** *Resolved 2026-05-15:* Allow
   `docs/tracker/fuzz-findings/` to grow unbounded for now. Each
   finding retains replay value as long as its minimum-repro yaml is
   preserved. Compaction can be revisited operationally if the
   directory becomes unwieldy. Reflected in §8.3.
4. **Seed corpus drift.** *Resolved 2026-05-15:* Re-load from HEAD on
   each run. Pinning a snapshot would drift from the codebase and
   produce seeds that don't reflect current example layouts. The
   corpus evolves with the project. Reflected in §2.
5. **Worker isolation.** *Resolved 2026-05-15:* Default to
   `min(runtime.NumCPU()/2, 4)` workers; capped at 4 to avoid
   saturating docker on typical dev laptops. CI runners with
   headroom can override via `--workers`. Reflected in §5.3.

---

## 14. Carry-over notes

- Shrinking quality matters more than generation quality. A great
  generator that produces unfindably-large repros is worse than a
  modest generator + an aggressive shrinker.
- Start with one dialect (postgres). Multi-dialect amplifies surface
  area before single-dialect coverage is dense.
- Resist the urge to make the fuzzer "smart" via AI-driven generation.
  Random + shrinking is the proven recipe. AI triage is appropriate
  on the output side (see §8.3); AI on the input side is unproven
  and undermines reproducibility.
- The fuzzer is not a CI gate for arbitrary PRs. Findings are
  candidates for triage, not red builds. Promoting a finding to a
  CI gate happens only when it becomes a regression seed under
  `testdata/examples/regressions/`.
