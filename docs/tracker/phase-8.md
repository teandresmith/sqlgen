# Phase 8: CLI

Status: In Progress
PRD Sections: 23, 23.1–23.9

## 8.1 Entry Point & `generate` Command

**PRD Reference:** Sections 23.1, 23.2, 23.3, 23.9

**Status:** Complete

### Tasks

- [x] Add `github.com/spf13/cobra` dependency and set up root command
- [x] Implement config discovery logic: `--config` flag → `sqlgen.yml` → `sqlgen.yaml` → error with `sqlgen init` suggestion
- [x] Define exit code constants (0=success, 1=generation, 2=config, 3=schema, 4=connection)
- [x] Implement root command with `--config`, `--verbose`, `--quiet`, `--version` flags
- [x] Make `generate` the default command (no subcommand required, `sqlgen` == `sqlgen generate`)
- [x] Wire up the full generation pipeline: load config → validate phase 1 → parse schema → parse views → detect relationships → validate phase 2 → generate → format → write
- [x] Implement verbose output (parsed tables, generated files, timing)
- [x] Implement quiet mode (suppress all output except errors)
- [x] Map pipeline errors to correct exit codes

### Acceptance Criteria

- Running `sqlgen` with no args executes the `generate` command
- `--config path` loads the specified config file exactly
- Without `--config`, discovers `sqlgen.yml` then `sqlgen.yaml` in the current directory
- If no config found, prints error suggesting `sqlgen init` and exits with code 2
- `--verbose` shows parsed table count, generated file names, and timing
- `--quiet` suppresses all output except errors
- `--version` prints the version string and exits
- Exit codes match the table: 0 (success), 1 (generation error), 2 (config error), 3 (schema error), 4 (connection error)
- Full pipeline runs end-to-end: config → parse → validate → generate → format → write

### Tests Required

- [x] Config discovery: `--config` overrides search, finds `sqlgen.yml`, finds `sqlgen.yaml`, error when neither exists
- [x] Exit code mapping: each error category produces the correct exit code
- [x] `--version` flag prints version and exits 0
- [x] `--verbose` and `--quiet` produce expected output levels
- [x] Default command behavior: no args runs `generate`
- [x] Full pipeline integration: valid config + schema → generated files written

### Completion Record

**Date:** 2026-04-14

**Files created/rewritten:**
- `cmd/sqlgen/main.go` — cobra CLI: `cliFlags` struct, `newRootCmd`, `makeGenerateRunE` (closure-based, no global state), full pipeline (config → validate → parse DDL → parse views → detect relationships → validate post-parse → generate → write), exit codes, verbose/quiet modes
- `cmd/sqlgen/main_test.go` — 14 tests: config discovery (4), exit code mapping (5 cases table-driven), version, verbose, quiet, default command, full pipeline, generate subcommand, single_file layout (tables), single_file layout (views), file_per_table layout (views)
- `cmd/sqlgen/gen/orchestrate.go` — `Generate()` orchestrator, embedded templates (`//go:embed all:templates`), `renderAndWrite`/`renderTableBody`/`renderViewBody` helpers, `single_file` layout (tables→`models_gen.go`, views→`views_gen.go`) and `file_per_table` layout, `deduplicateImports`, `loadTemplates`, dialect/schema resolution

**Files modified:**
- `cmd/sqlgen/go.mod` / `go.sum` — added `github.com/spf13/cobra`

---

## 8.2 `init` Command

**PRD Reference:** Section 23.4

**Status:** Complete

### Tasks

- [x] Implement `sqlgen init` subcommand
- [x] Generate minimal `sqlgen.yml` with default content (postgres dialect, `./migrations` paths, pgx driver, `./internal/models` output)
- [x] Detect TTY for interactive mode: prompt for dialect choice (postgres, mysql, sqlite)
- [x] Non-interactive (no TTY): default to `postgres` without prompting
- [x] Error if `sqlgen.yml` or `sqlgen.yaml` already exists in the current directory

### Acceptance Criteria

- Running `sqlgen init` creates a `sqlgen.yml` file in the current directory
- Generated config matches the template from PRD section 23.4 (with chosen dialect and corresponding driver)
- Interactive mode prompts for dialect when TTY is detected
- Non-interactive mode defaults to `postgres` silently
- Errors if a config file already exists (does not overwrite)
- Driver defaults match dialect: postgres→pgx, mysql→stdlib, sqlite→stdlib

### Tests Required

- [x] Creates correct default config file content
- [x] Non-interactive mode defaults to `postgres`
- [x] Errors when config file already exists
- [x] Dialect-to-driver mapping is correct for all three dialects

### Completion Record

**Date:** 2026-04-14

**Files modified:**
- `cmd/sqlgen/main.go` — added `init` subcommand with `runInit`, `initConfigContent`, `driverForDialect`; TTY detection for interactive dialect prompt; checks for existing config files
- `cmd/sqlgen/main_test.go` — 4 tests: default config content, non-interactive defaults to postgres, errors when config exists (yml + yaml), dialect-to-driver mapping (3 dialects)

---

## 8.3 `validate` Command

**PRD Reference:** Section 23.1

**Status:** Complete

### Tasks

- [x] Implement `sqlgen validate` subcommand
- [x] Load config and parse schema (reusing the same pipeline as `generate`, stopping before code generation)
- [x] Collect and report all validation errors together (not fail-fast)
- [x] Exit code 2 on validation failure, 0 on success

### Acceptance Criteria

- `sqlgen validate` loads config, parses schema, and reports all errors
- Reports all validation errors at once, not just the first
- Exit code 0 on valid config + schema
- Exit code 2 on config validation failure
- Exit code 3 on schema parse failure
- Does not generate any files

### Tests Required

- [x] Valid config + schema exits 0 with success message
- [x] Invalid config exits 2 with all config errors listed
- [x] Invalid schema exits 3 with all schema errors listed
- [x] No files are written to disk

### Completion Record

**Date:** 2026-04-14

**Files modified:**
- `cmd/sqlgen/main.go` — added `validate` subcommand with `makeValidateRunE` (collects all errors across pre-parse, schema parse, and post-parse validation), `splitJoinedErrors` helper
- `cmd/sqlgen/main_test.go` — 4 tests: valid config+schema exits 0, invalid config exits 2, invalid schema exits 3, no files written

---

## 8.4 `diff` Command

**PRD Reference:** Section 23.6

**Status:** Complete

### Tasks

- [x] Implement `sqlgen diff` subcommand
- [x] Run the full generation pipeline to a temporary directory
- [x] Compare generated output against existing files in the output directory
- [x] Categorize files as created, modified, or deleted
- [x] Print file-level summary with status labels (create/modify/delete)
- [x] Print summary line with counts
- [x] Exit code 0 when no changes, 1 when changes detected

### Acceptance Criteria

- Generates to a temp directory without modifying existing files
- Correctly identifies created files (exist in temp, not in output)
- Correctly identifies modified files (exist in both, content differs)
- Correctly identifies deleted files (exist in output as `*_gen.go`, not in temp) in `file_per_table` mode
- Output format matches PRD example: `  create  path`, `  modify  path`, `  delete  path`
- Summary line shows total count and breakdown
- Exit code 0 = no changes, 1 = changes detected
- Useful in CI to verify generated code is up to date

### Tests Required

- [x] No changes: identical output, exit code 0
- [x] New file detected: exit code 1, listed as "create"
- [x] Modified file detected: exit code 1, listed as "modify"
- [x] Deleted file detected (file_per_table): exit code 1, listed as "delete"
- [x] Mixed changes: correct counts in summary line

### Completion Record

**Date:** 2026-04-14

**Files modified:**
- `cmd/sqlgen/main.go` — added `diff` subcommand with `makeDiffRunE`, extracted `loadAndValidate` shared pipeline helper, `printDiffSummary`, `diffFiles`, `compareGeneratedFiles`, `findDeletedFiles`; `fileChange` type
- `cmd/sqlgen/main_test.go` — 5 tests: no changes (exit 0), new file detected (create, exit 1), modified file (modify, exit 1), deleted file in file_per_table (delete, exit 1), mixed changes (summary counts)

---

## 8.5 `completion` Command

**PRD Reference:** Section 23.5

**Status:** Complete

### Tasks

- [x] Implement `sqlgen completion` subcommand with shell argument (bash, zsh, fish, powershell)
- [x] Generate shell completion scripts using the CLI framework's built-in completion support
- [x] Completions cover all commands, flags, and flag values (e.g., `--config` completes `.yml`/`.yaml` files)

### Acceptance Criteria

- `sqlgen completion bash` outputs valid bash completion script
- `sqlgen completion zsh` outputs valid zsh completion script
- `sqlgen completion fish` outputs valid fish completion script
- `sqlgen completion powershell` outputs valid powershell completion script
- Error if shell argument is missing or unrecognized
- Completions include all commands and flags

### Tests Required

- [x] Each shell variant produces non-empty output
- [x] Missing shell argument produces an error
- [x] Invalid shell name produces an error

### Completion Record

**Date:** 2026-04-14

**Files modified:**
- `cmd/sqlgen/main.go` — added `newCompletionCmd` function and wired it into `newRootCmd`; uses cobra's built-in `GenBashCompletion`, `GenZshCompletion`, `GenFishCompletion`, `GenPowerShellCompletion`; `ValidArgs` for shell tab-completion; `ExactArgs(1)` enforcement
- `cmd/sqlgen/main_test.go` — 3 tests: each shell variant produces non-empty output (table-driven across 4 shells), missing shell argument errors, invalid shell name errors

---

## 8.6 `lint` Command

**PRD Reference:** Section 23.7

**Status:** Complete

### Tasks

- [x] Implement `sqlgen lint` subcommand
- [x] Load config and parse schema to build a type registry: `TableName` → valid input/result types per operation
- [x] Use `go/ast` to parse consumer `.go` files and find `hook.ForMutation[...]` and `hook.ForQuery[...]` call expressions
- [x] Extract generic type parameters and `TableName` argument from each call
- [x] Implement rule: hook table/type mismatch (severity: error)
- [x] Implement rule: invalid operation for table (severity: warning)
- [x] Implement rule: unused table constant (severity: info)
- [x] Implement `--paths` flag (comma-separated directories, default: `output.dir`)
- [x] Implement `--fail-on` flag (`error`, `warning`, `info`; default: `error`)
- [x] Format output with severity label, file:line, code snippet, and explanation
- [x] Exit code 0 when no issues at/above `--fail-on`, 1 otherwise

### Acceptance Criteria

- Detects `ForMutation`/`ForQuery` calls with mismatched generic type params vs. table name
- Detects hooks filtering on operations the table doesn't support
- Detects hooks on table constants where the table's `operations` config excludes the hooked operation
- `--paths` scans additional directories beyond `output.dir`
- `--fail-on` controls which severity triggers non-zero exit
- Output format matches PRD example: severity, file:line, code, explanation
- Summary line shows issue counts by severity

### Tests Required

- [x] Detects table/type mismatch: `ForMutation[*CreateProductInput, *Product](TableUsers, ...)` is an error
- [x] Detects invalid operation: hooking `OpSoftDelete` on a table with no soft delete column
- [x] Detects unused table constant: hooking `OpCreate` on a read-only table
- [x] `--paths` scans specified directories
- [x] `--fail-on warning` exits 1 on warnings
- [x] `--fail-on info` exits 1 on info-level issues
- [x] Clean code exits 0 with no output
- [x] Output formatting matches expected format

### Completion Record

**Date:** 2026-04-14

**Files created:**
- `cmd/sqlgen/lint.go` — lint subcommand (`newLintCmd`), type registry (`buildTypeRegistry`, `typeRegistry`, `tableInfo`), AST analysis (`findHookCalls`, `parseHookCall`, `extractTypeName`, `extractTableConst`, `extractCtxParamName`, `findOpReferences`), 3 validation rules (`validateHookCall`), output formatting (`formatLintOutput`, `sortIssues`), `--paths`/`--fail-on` flags, operation-to-config mapping via `opGroupName` map and `opsFlags`
- `cmd/sqlgen/lint_test.go` — 8 tests: table/type mismatch, invalid operation (soft delete), unused table constant (read_only), --paths scanning, --fail-on warning, --fail-on info, clean code exits 0, output format

**Files modified:**
- `cmd/sqlgen/main.go` — wired `newLintCmd` into `newRootCmd`

---

## 8.7 Stale File Cleanup

**PRD Reference:** Section 23.8

**Status:** Complete

### Tasks

- [x] During `generate`, track all files written to the output directory
- [x] After generation completes, scan output directory for `*_gen.go` files not in the written set
- [x] Delete orphaned `*_gen.go` files (only in `file_per_table` layout mode)
- [x] Report deleted files in verbose output
- [x] Never touch files that don't end in `_gen.go`

### Acceptance Criteria

- In `file_per_table` mode: after generation, orphaned `*_gen.go` files are deleted
- Only files matching `*_gen.go` within the output directory are candidates for deletion
- Non-`_gen.go` files are never deleted
- Cleanup does not run in `single_file` layout mode
- Deleted files are reported when `--verbose` is set
- Works correctly when a table is dropped from the schema

### Tests Required

- [x] Orphaned `*_gen.go` file is deleted after table removed from schema
- [x] Non-`_gen.go` files in output directory are preserved
- [x] No cleanup in `single_file` layout mode
- [x] Newly generated files are not deleted
- [x] Verbose output lists deleted files

### Completion Record

**Date:** 2026-04-14

**Files created:**
- None

**Files modified:**
- `cmd/sqlgen/gen/orchestrate.go` — added `CleanStaleFiles` function that scans output directory for orphaned `*_gen.go` files not in the written set and deletes them
- `cmd/sqlgen/main.go` — wired `CleanStaleFiles` into `makeGenerateRunE` after generation (only for `file_per_table` layout), reports deleted files in verbose output
- `cmd/sqlgen/main_test.go` — 5 tests: orphaned file deleted after table removed, non-_gen.go files preserved, no cleanup in single_file mode, newly generated files not deleted, verbose reports deleted files
