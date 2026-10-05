# SQLGen — Claude Code Instructions

This is a type-safe SQL-to-Go code generator supporting PostgreSQL, MySQL, and SQLite. All work is spec-driven from the PRD — see `docs/tracker/STATUS.md` for current progress.

## Critical Rules

1. **The PRD is the source of truth.** Do not invent features, add config fields, or deviate from `docs/PRD.md`. If something is ambiguous, flag it — do not guess.
2. **Three-module architecture is inviolable.** Runtime (`./`), Parser (`parser/`), CLI (`cmd/sqlgen/`). Runtime MUST NOT import parser or CLI packages. Parser MUST NOT import runtime or CLI packages. See `guidelines/ARCHITECTURE.md` for the full dependency graph.
3. **No build tags.** Module boundaries handle all dependency isolation. Never use `//go:build` tags.
4. **All values in SQL are parameterized.** No string interpolation of values into SQL. All identifiers are quoted via `Dialect.QuoteIdentifier()` or `Dialect.FormatTable()`.
5. **Conventional Commits required.** All commit messages follow the format: `<type>(scope): description`. See `guidelines/CI.md` for types and scopes.

## Guidelines

Read guidelines **on demand**, not upfront. Consult `guidelines/README.md` to find the right document for your task. Each guideline has a "When to Read" condition — follow it.

| Guideline | Read When |
|-----------|-----------|
| `guidelines/ARCHITECTURE.md` | Creating packages/files, unsure where code belongs |
| `guidelines/GO.md` | Writing any Go code |
| `guidelines/TESTING.md` | Writing or modifying tests |
| `guidelines/SQL.md` | Working on SQL builders, comparators, dialects |
| `guidelines/ERRORS.md` | Working on error types, driver adapters, client methods |
| `guidelines/CI.md` | Modifying CI, linting, versioning, releases |
| `guidelines/TEMPLATES.md` | Working on code generation templates |

## Implementation Tracker

Before starting implementation work, check `docs/tracker/STATUS.md` for current progress. This tells you what's done, what's in progress, and what's next.

| Command | Purpose |
|---------|---------|
| `/phase <N>` | Break down Phase N into tasks (generates `docs/tracker/phase-N.md`) |
| `/implement <N.X> [--skip-review]` | Implement sub-item N.X — reads spec, writes code + tests, updates tracker, runs auto-review subagent |
| `/verify <N.X \| FIX-NNN>` | Verify a sub-item or resolved fix against the PRD — checks requirements, tests, guidelines, runs an independent reviewer pass, and auto-logs FAIL/PARTIAL/MISSING items as severity-tagged FIX entries |
| `/done <N.X>` | Mark sub-item N.X as complete and update the tracker |
| `/status` | Show current implementation status across all phases |
| `/fix add/list/resolve` | Track bugs discovered during E2E testing — entries carry a severity tag (`blocking` / `tracked` / `nit`); see `/fix` for resolution gates |
| `/fix-vet <FIX-NNN… \| --all \| --phase N> [--refresh] [--jobs K] [--dry-run]` | Vet open fixes before implementing — fact-check the entry, reproduce, prototype in a throwaway worktree, fail-first; records a `**Vetted:**` verdict and saves `.claude/prototypes/FIX-NNN.patch`; batches ruling questions |
| `/fix-implement FIX-NNN [--skip-review]` | Implement a tracked fix — loads guidelines, fixes code, runs tests, resolves, runs auto-review subagent; starts from a fresh vetted prototype when one exists |
| `/fix-loop [--max N] [--only FIX-NNN…] [--context-limit P] [--skip-final-sweep] [--dry-run]` | Drain the vetted fix queue hands-off — one subagent per fix runs `/fix-implement --skip-review` + `/verify`, the orchestrator checks and commits each fix, parks design questions and asks them in one batch at the end; stops only on failures |
| `/close-phase N [--dry-run] [--skip-integration]` | Run the closure sweep for phase N — verifies all sub-items complete, triages open FIXes by severity, runs the full test sweep, freezes versioned artifacts, updates STATUS.md + sibling design docs |

Workflow for each sub-item:
1. `/phase N` — break down the phase (once per phase)
2. `/implement N.X` — write the code and tests
3. `/verify N.X` — confirm PRD compliance
4. `/done N.X` — mark complete, update tracker

Phase closure (once all sub-items in the phase are `Complete`):
5. `/close-phase N` — closure sweep + tracker bookkeeping

## Reference Documents

| Document | Purpose |
|----------|---------|
| `docs/PRD.md` | Full specification — authoritative for all features, config, behavior |
| `docs/tracker/IMPLEMENTATION_ORDER.md` | Build phases and dependency order |
| `docs/design/` | Design supplements — rationale and codegen detail behind PRD sections (e.g. `CACHE.md`, `TENANCY.md`, `MCP.md`) |
| `docs/tracker/STATUS.md` | Current implementation progress |
| `docs/tracker/backlog.md` | Unvetted adjacent findings (not FIX entries); triage rules in `/fix` → Findings triage |

## Code Standards (quick reference)

- **Go 1.27+** target. Use `errors.AsType`, enhanced `new()`, `go fix` modernizers, and the stdlib `uuid` package in handwritten code.
- **`gofumpt`** for formatting. **`goimports`** for import grouping (stdlib, external, internal).
- **No assertion libraries.** Use `==`, `cmp.Diff`, standard `testing` package only.
- **Table-driven tests** for any function with multiple input combinations.
- **Testcontainers** for integration tests, not mocks. Skip with `testing.Short()`.
- **Error wrapping:** `fmt.Errorf("{operation} {table}: %w", err)`. Lowercase, singular table name.
- **Errors as last return.** Return `error` interface, not concrete types.
- **All generated files** use `_gen.go` suffix and include `// Code generated by sqlgen. DO NOT EDIT.` header.

## Multi-Dialect Awareness

Every SQL builder and comparator must handle all three dialects. See `guidelines/SQL.md` for the per-dialect placeholder, quoting, `RETURNING`, and upsert rules.

## What Not To Do

- Do not add features not in the PRD.
- Do not add dependencies to runtime core packages (`sql/`, `comparator/`, `omittable/`, `database/`) — they must depend only on stdlib.
- Do not use `testify`, `gomega`, or any assertion library.
- Do not use build tags for any purpose.
- Do not interpolate values into SQL strings.
- Do not iterate over Go maps in templates (non-deterministic order).
- Do not manually edit `CHANGELOG.md` (managed by Release Please).
- Do not skip linter warnings with bare `//nolint` — always include a reason.
