# Contributing to SQLGen

Thank you for your interest in contributing to SQLGen. This document covers the basics for getting started.

## Getting Started

1. Fork the repository
2. Clone your fork
3. Create a feature branch from `main`
4. Make your changes
5. Open a pull request

## Development Guidelines

All coding standards, testing patterns, architectural rules, and CI requirements are documented in the [`guidelines/`](./guidelines/) directory. These guidelines apply equally to human contributors and AI agents.

Start with [`guidelines/README.md`](./guidelines/README.md) — it will point you to the specific document relevant to your task. Do not read every guideline upfront; refer to them on demand.

## Commit Messages

This project uses [Conventional Commits](https://www.conventionalcommits.org/) for automated versioning and changelog generation. All commits to `main` must follow this format:

```
<type>[optional scope]: <description>
```

Common types: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `perf`, `ci`

See [`guidelines/CI.md`](./guidelines/CI.md#conventional-commits) for the full specification, scopes, and examples.

## Pull Requests

- Keep PRs focused on a single concern.
- Ensure all CI checks pass (lint, unit tests, integration tests).
- Update golden files if your change affects generated output (`make update-golden`).
- Include a clear description of what changed and why.

## Testing

Run the quick check before pushing:

```bash
make check
```

See [`guidelines/TESTING.md`](./guidelines/TESTING.md) for testing standards and [`guidelines/CI.md`](./guidelines/CI.md#13-running-ci-locally) for the full local CI suite.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](./LICENSE).
