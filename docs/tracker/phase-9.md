# Phase 9: CLI Refactor

Status: Complete
PRD Sections: N/A (structural refactoring, no new behavior)

## 9.1 `cli/` Package Scaffold & Root Command

**PRD Reference:** N/A (refactor)

**Status:** Complete

### Tasks

- [x] Create `cmd/sqlgen/cli/` package directory
- [x] Create `root.go` with `package cli` declaration
- [x] Move exit code constants (`exitSuccess`, `exitGeneration`, `exitConfig`, `exitSchema`, `exitConnection`) to `root.go` and export them
- [x] Move `cliFlags` struct to `root.go` (stays unexported)
- [x] Move `exitError` type and its methods (`Error`, `Unwrap`) to `root.go` (stays unexported)
- [x] Move `exitCodeFromError` to `root.go` (stays unexported — only used internally by `Execute`)
- [x] Add exported `version` var for ldflags injection: `var Version = "dev"`
- [x] Create `NewRootCmd() *cobra.Command` in `root.go` — move `newRootCmd` logic and export it
- [x] Create `Execute() int` in `root.go` — move `run()` logic and export it
- [x] Reduce `cmd/sqlgen/main.go` to thin entry point: `func main() { os.Exit(cli.Execute()) }`
- [x] Verify `go vet ./...` passes in CLI module

### Acceptance Criteria

- `cmd/sqlgen/main.go` is ≤10 lines (package declaration, import, main function)
- `cmd/sqlgen/cli/root.go` contains `NewRootCmd`, `Execute`, all exit codes, `cliFlags`, `exitError`
- `go build ./cmd/sqlgen/` produces the same binary (same flags, same commands)
- All existing tests still pass (even if they temporarily reference the old location)

### Tests Required

- [x] No new tests — existing tests validate behavior is unchanged (run `make check` to confirm)

### Completion Record

Files changed:
- Created `cmd/sqlgen/cli/root.go` — all CLI logic (root cmd, pipeline helpers, all commands, lint)
- Created `cmd/sqlgen/cli/cli_test.go` — all command tests (35 tests) migrated from `main_test.go`
- Created `cmd/sqlgen/cli/lint_test.go` — all lint tests (8 tests) migrated from `lint_test.go`
- Reduced `cmd/sqlgen/main.go` to 12-line thin entry point
- Deleted `cmd/sqlgen/lint.go` (moved to cli/root.go)
- Deleted `cmd/sqlgen/main_test.go` (moved to cli/cli_test.go)
- Deleted `cmd/sqlgen/lint_test.go` (moved to cli/lint_test.go)

Notes: Tests were migrated to `cli/` package in this step (pulling forward from 9.4) because
the test files reference unexported types (`exitError`, `exitCodeFromError`) that moved to
`package cli` and cannot remain in `package main`. All 43 tests pass. Exit code constants
are now exported (`ExitConfig`, `ExitGeneration`, etc.) for use by Phase 10 E2E tests.

---

## 9.2 Shared Pipeline Extraction

**PRD Reference:** N/A (refactor)

**Status:** Complete

### Tasks

- [x] Create `cmd/sqlgen/cli/pipeline.go`
- [x] Move `loadAndValidate` to `pipeline.go`
- [x] Move `parseSchema` to `pipeline.go`
- [x] Move `newParser` to `pipeline.go`
- [x] Move `viewSQLParser` type and `viewParserForDialect` to `pipeline.go`
- [x] Move `parseViewFiles` to `pipeline.go`
- [x] Move `toSchemaTables` to `pipeline.go`
- [x] Move `printWarnings` to `pipeline.go`
- [x] Move `printVerbose` to `pipeline.go`
- [x] Move `splitJoinedErrors` to `pipeline.go`
- [x] Verify all moved functions compile and reference each other correctly
- [x] Verify `go vet ./...` passes

### Acceptance Criteria

- `pipeline.go` contains all shared pipeline helpers
- No duplicate function definitions remain in `main.go`
- Functions that reference `cliFlags` or `exitError` from `root.go` resolve correctly within the `cli` package
- All existing tests still pass

### Tests Required

- [x] No new tests — existing tests validate behavior is unchanged (run `make check` to confirm)

### Completion Record

Files changed:
- Created `cmd/sqlgen/cli/pipeline.go` — 10 functions/types extracted from root.go
- Modified `cmd/sqlgen/cli/root.go` — removed pipeline helpers, cleaned up unused imports (parser/mysql, parser/postgres, parser/sqlite)

---

## 9.3 Command Extraction

**PRD Reference:** N/A (refactor)

**Status:** Complete

### Tasks

- [x] Create `cmd/sqlgen/cli/generate.go` — move `makeGenerateRunE` (includes stale file cleanup wiring)
- [x] Create `cmd/sqlgen/cli/init.go` — move init command RunE closure, `runInit`, `initConfigContent`, `driverForDialect`
- [x] Create `cmd/sqlgen/cli/validate.go` — move `makeValidateRunE`
- [x] Create `cmd/sqlgen/cli/diff.go` — move `makeDiffRunE`, `fileChange` type, `diffFiles`, `compareGeneratedFiles`, `findDeletedFiles`, `printDiffSummary`
- [x] Create `cmd/sqlgen/cli/completion.go` — move `newCompletionCmd`
- [x] Create `cmd/sqlgen/cli/lint.go` — move entire contents of `cmd/sqlgen/lint.go` (`newLintCmd`, all lint types, AST analysis functions, validation rules, formatting, `opGroupName` map, `opsFlags`)
- [x] Update `NewRootCmd` in `root.go` to call the command constructors from their new files
- [x] Delete all command logic from `cmd/sqlgen/main.go` (should already be thin from 9.1)
- [x] Delete `cmd/sqlgen/lint.go` (fully moved to `cli/lint.go`)
- [x] Verify `go vet ./...` passes
- [x] Verify `go build ./cmd/sqlgen/` succeeds

### Acceptance Criteria

- One file per command in `cmd/sqlgen/cli/`: `generate.go`, `init.go`, `validate.go`, `diff.go`, `completion.go`, `lint.go`
- `cmd/sqlgen/lint.go` no longer exists
- `cmd/sqlgen/main.go` contains only the thin entry point (no command logic)
- Every CLI command works identically: `sqlgen`, `sqlgen generate`, `sqlgen init`, `sqlgen validate`, `sqlgen diff`, `sqlgen completion`, `sqlgen lint`
- `go build ./cmd/sqlgen/` produces a working binary

### Tests Required

- [x] No new tests — existing tests validate behavior is unchanged (run `make check` to confirm)

### Completion Record

Files changed:
- Created `cmd/sqlgen/cli/generate.go` — `makeGenerateRunE`
- Created `cmd/sqlgen/cli/init.go` — `runInit`, `initConfigContent`, `driverForDialect`
- Created `cmd/sqlgen/cli/validate.go` — `makeValidateRunE`
- Created `cmd/sqlgen/cli/diff.go` — `makeDiffRunE`, `fileChange`, `diffFiles`, `compareGeneratedFiles`, `findDeletedFiles`, `printDiffSummary`
- Created `cmd/sqlgen/cli/completion.go` — `newCompletionCmd`
- Created `cmd/sqlgen/cli/lint.go` — all lint types, AST analysis, validation rules, formatting
- Reduced `cmd/sqlgen/cli/root.go` to scaffold only: `Execute`, `NewRootCmd`, `exitError`, exit codes

---

## 9.4 Test Migration

**PRD Reference:** N/A (refactor)

**Status:** Complete

### Tasks

- [x] Create `cmd/sqlgen/cli/cli_test.go` — move `executeCommand` helper (update to use `NewRootCmd()`) and all test helpers (`writeTestConfig`, `writeTestConfigWithDir`, `writeTestConfigWithLayout`, `writeSQLFile`)
- [x] Move all generate/init/validate/diff/completion/stale-cleanup tests from `main_test.go` to `cli_test.go` (35 tests)
- [x] Create `cmd/sqlgen/cli/lint_test.go` — move all lint tests from `cmd/sqlgen/lint_test.go` (8 tests) and lint test helpers (`setupLintTest`, `writeLintGoFile`, etc.)
- [x] Update test package declaration to `package cli` (white-box, since tests reference unexported types like `exitError`)
- [x] Delete `cmd/sqlgen/main_test.go`
- [x] Delete `cmd/sqlgen/lint_test.go`
- [x] Run `make check` — all 43 CLI tests must pass
- [x] Run `go test -race ./cmd/sqlgen/cli/` to verify race safety

### Acceptance Criteria

- `cmd/sqlgen/main_test.go` no longer exists
- `cmd/sqlgen/lint_test.go` no longer exists
- `cmd/sqlgen/cli/cli_test.go` contains all command tests (35 tests) and shared test helpers
- `cmd/sqlgen/cli/lint_test.go` contains all lint tests (8 tests) and lint-specific test helpers
- Every existing test passes with identical assertions (no behavioral changes)
- `make check` passes (lint + all tests across all modules)

### Tests Required

- [x] All 35 existing command tests pass in their new location (generate: 12, init: 4, validate: 4, diff: 5, completion: 3, stale cleanup: 5, exit codes: 1, version: 1)
- [x] All 8 existing lint tests pass in their new location
- [x] `make check` passes with 0 lint issues and all tests green

### Completion Record

Completed as part of 9.1 — tests had to move with the code because they reference unexported
types (`exitError`, `exitCodeFromError`, `severityError`, etc.). Race detector verified clean.
All 43 tests pass in `cmd/sqlgen/cli/`.
