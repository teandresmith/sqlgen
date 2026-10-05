# SQLGen — Development Guidelines

> Coding standards and architectural reference for human developers and AI agents.
> Read this file first. Refer to linked documents **only when your task requires it** — do not read every document upfront.

---

## How to Use These Guidelines

**Do not read all documents before starting work.** Use the "When to Read" column below to determine which documents are relevant to your current task. Most tasks only require one or two references. Read on demand, not in advance.

## Quick Reference

| Document | Purpose | When to Read |
|----------|---------|-------------|
| [ARCHITECTURE.md](./ARCHITECTURE.md) | Project structure, module boundaries, package map, dependency rules | Before creating new packages or files; when unsure where code belongs |
| [GO.md](./GO.md) | Go coding standards — naming, error handling, interfaces, generics, formatting | Before writing any Go code in this project |
| [TESTING.md](./TESTING.md) | Testing strategy — table-driven tests, testcontainers, mocks, assertions | Before writing or modifying tests |
| [SQL.md](./SQL.md) | Multi-dialect SQL — builders, placeholders, quoting, dialect differences, injection prevention | Before writing or modifying SQL builders, comparators, or dialect code |
| [ERRORS.md](./ERRORS.md) | Error taxonomy — sentinel errors, ConstraintError, driver mapping, wrapping conventions | Before writing or modifying error types, driver adapters, or client methods |
| [CI.md](./CI.md) | CI pipeline — linting, test matrix, semantic versioning, automated releases, binary distribution | Before modifying CI workflows, linter config, versioning, or release infrastructure |
| [TEMPLATES.md](./TEMPLATES.md) | Template authoring — file structure, funcmap, context, whitespace, golden files | Before writing or modifying code generation templates |

## Supplementary Documents

These are reference material — consult them when you need specifics, not as required reading.

| Document | Purpose |
|----------|---------|
| [docs/PRD.md](../docs/PRD.md) | Full product requirements — the authoritative specification for all features |

---

## Core Principles

1. **Spec-driven.** The PRD is the source of truth. Do not invent features or deviate from specified behavior.
2. **Dependency isolation.** Module boundaries exist for a reason. Never import parser or CLI packages from the runtime module.
3. **Generated code is a product.** Generated Go code must be idiomatic, readable, and pass `go vet` and `golangci-lint` without exceptions.
4. **Test against real databases.** Use testcontainers for integration tests. Mocks are only for unit-testing generated client logic.
5. **No build tags.** Module boundaries handle all dependency isolation. See ARCHITECTURE.md for details.
