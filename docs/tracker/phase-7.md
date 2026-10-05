# Phase 7: Hooks & Middleware

Status: In Progress
PRD Sections: 21.1–21.11, 9.6, 9.8

## 7.1 Hook Types — Core types, operation enums, table name type

**PRD Reference:** Section 21.2

**Status:** Complete

### Tasks

- [x] Create `hook/` package with `hook.go` containing `MutationHook`, `MutationHandler`, `MutationContext` types
- [x] Add `QueryHook`, `QueryHandler`, `QueryContext` types
- [x] Define `MutationOp` string type with all 16 operation constants (`OpCreate` through `OpIncrement`)
- [x] Define `QueryOp` string type with all 6 operation constants (`OpGet` through `OpConnection`)
- [x] Define `TableName` string type (constants are generated per-table by codegen, not defined here)
- [x] Add `WithMutationHook` and `WithQueryHook` client option functions to unified client template (codegen)
- [x] Add `mutationHooks []MutationHook` and `queryHooks []QueryHook` fields to `clientOptions` in unified client template
- [ ] ~~Update entity client constructors in the unified client template to receive hook slices~~ → deferred to 7.3 (requires per-table client template changes that pair with hook chain wiring)

### Acceptance Criteria

- `MutationHook` is `func(next MutationHandler) MutationHandler`
- `MutationHandler` is `func(ctx context.Context, m *MutationContext) (any, error)`
- `MutationContext` has fields: `Op MutationOp`, `Table TableName`, `Schema string`, `PK any`, `Input any`
- `QueryHook` is `func(next QueryHandler) QueryHandler`
- `QueryHandler` is `func(ctx context.Context, q *QueryContext) (any, error)`
- `QueryContext` has fields: `Op QueryOp`, `Table TableName`, `Schema string`, `PK any`, `Input any`
- `MutationOp` has exactly 16 constants matching PRD section 21.2
- `QueryOp` has exactly 6 constants matching PRD section 21.2
- `TableName` is a `string` type (constants generated per-table by codegen)
- `hook` package has zero external dependencies (stdlib only)
- `WithMutationHook` and `WithQueryHook` options append to hook slices on `clientOptions`

### Tests Required

- [x] Test that `MutationOp` constants match expected values (all 16)
- [x] Test that `QueryOp` constants match expected values (all 6)
- [x] Test that hook type signatures compile correctly (type assertion tests)
- [x] Test `WithMutationHook` appends to client options hook slice (codegen golden test update)
- [x] Test `WithQueryHook` appends to client options hook slice (codegen golden test update)

### Completion Record

**Files created:**
- `hook/hook.go` — core types: `TableName`, `MutationOp` (16 ops), `QueryOp` (6 ops), `MutationHook`, `MutationHandler`, `MutationContext`, `QueryHook`, `QueryHandler`, `QueryContext`
- `hook/hook_test.go` — tests for all constants, type signatures, and context field access

**Files modified:**
- `cmd/sqlgen/gen/templates/tablename.go.tmpl` — removed local `TableName` type, constants now use `hook.TableName`
- `cmd/sqlgen/gen/templates/client.go.tmpl` — added `mutationHooks`/`queryHooks` fields, `WithMutationHook`/`WithQueryHook` options
- `cmd/sqlgen/gen/context_client.go` — added `hook` import to client context
- `cmd/sqlgen/gen/unified_client_test.go` — updated pattern checks for new fields
- `cmd/sqlgen/gen/tablename_test.go` — updated for `hook.TableName` constants + imports
- `cmd/sqlgen/gen/testdata/golden/unified_client_gen.go` — regenerated
- `cmd/sqlgen/gen/testdata/golden/tablenames_gen.go` — regenerated

**Notes:** Entity client constructor wiring (last task) deferred to 7.3 — requires per-table client template changes that pair with hook chain execution.

---

## 7.2 Generic Helpers — Type-safe table-scoped and conditional execution helpers

**PRD Reference:** Section 21.4

**Status:** Complete

### Tasks

- [x] Implement `ForMutation[Input, Result](table TableName, fn) MutationHook` — type-safe table-scoped mutation hook
- [x] Implement `ForQuery[Input, Result](table TableName, fn) QueryHook` — type-safe table-scoped query hook
- [x] Implement `OnMutation(hook MutationHook, ops ...MutationOp) MutationHook` — operation filter
- [x] Implement `OnQuery(hook QueryHook, ops ...QueryOp) QueryHook` — operation filter
- [x] Implement `ForTable(hook MutationHook, tables ...TableName) MutationHook` — table filter
- [x] Implement `RejectMutation(ops ...MutationOp) MutationHook` — block operations, returns error

### Acceptance Criteria

- `ForMutation` passes through to `next` without executing when `MutationContext.Table` doesn't match the target table
- `ForMutation` performs type assertion on `Input` and `Result`, calling the typed `fn` when table matches
- `ForQuery` passes through to `next` without executing when `QueryContext.Table` doesn't match
- `ForQuery` performs type assertion on `Input` and `Result`, calling the typed `fn` when table matches
- `OnMutation` only executes the wrapped hook when `MutationContext.Op` is in the specified ops list
- `OnQuery` only executes the wrapped hook when `QueryContext.Op` is in the specified ops list
- `ForTable` only executes the wrapped hook when `MutationContext.Table` is in the specified tables list
- `RejectMutation` returns an error when the operation matches; passes through otherwise
- Helpers are composable: `OnMutation(ForTable(hook, ...), OpCreate, OpUpdate)` works correctly
- All helpers are in the `hook` package, stdlib-only dependencies

### Tests Required

- [x] `ForMutation` passes through when table doesn't match
- [x] `ForMutation` executes typed fn when table matches, with correct type assertion
- [x] `ForMutation` type assertion failure returns a meaningful error (not panic)
- [x] `ForQuery` passes through when table doesn't match
- [x] `ForQuery` executes typed fn when table matches
- [x] `OnMutation` fires only on specified ops
- [x] `OnMutation` passes through on non-matching ops
- [x] `OnQuery` fires only on specified ops
- [x] `OnQuery` passes through on non-matching ops
- [x] `ForTable` fires only on specified tables
- [x] `ForTable` passes through on non-matching tables
- [x] `RejectMutation` returns error on matching op
- [x] `RejectMutation` passes through on non-matching op
- [x] Composed helpers: `OnMutation(ForTable(...), ops...)` filters correctly

### Completion Record

**Date completed:** 2026-04-13

**Files created:**
- `hook/helpers.go` — `ForMutation`, `ForQuery`, `OnMutation`, `OnQuery`, `ForTable`, `RejectMutation`
- `hook/helpers_test.go` — 14 test cases covering all helpers and composition

**Notes:** All helpers are composable (e.g., `OnMutation(ForTable(hook, ...), ops...)`). Uses `slices.Contains` for operation/table matching. Type assertion failures return descriptive errors, never panic.

---

## 7.3 Hook Chain Execution — Chain builder, execution order, panic recovery

**PRD Reference:** Sections 21.3, 21.5, 21.6

**Status:** Complete

### Tasks

- [x] Implement mutation hook chain builder: composes global hooks → entity hooks → terminal handler
- [x] Implement query hook chain builder: composes global hooks → entity hooks → terminal handler
- [x] Implement built-in panic recovery hook for mutations (always outermost, wraps entire chain)
- [x] Implement built-in panic recovery hook for queries (always outermost, wraps entire chain)
- [x] Default panic behavior: `fmt.Errorf("sqlgen: panic in %s %s.%s: %v", op, schema, table, r)`
- [x] Integrate `WithPanicHandler` custom handler — replaces default log-only behavior
- [x] Wire chain execution into generated entity client methods (codegen template updates)
- [x] Update unified client template: entity clients receive the built hook chains
- [x] Update entity client constructors to receive hook slices from unified client (deferred from 7.1)
- [x] Update `Raw`/`RawExec` to wire through global hook chains (deferred from 6.4t)

### Acceptance Criteria

- Execution order: panic recovery (outermost) → global hooks (registration order) → entity hooks (registration order) → DB operation
- Global hooks always run before entity hooks regardless of registration order
- First-registered hook is outermost (executes first on the way in, last on the way out)
- Code before `next()` runs in registration order; code after `next()` runs in reverse
- Panic in any hook or DB operation is caught and converted to error
- Default panic handler returns a descriptive error with table, op, and panic value
- Custom `WithPanicHandler` replaces the default error behavior
- Panic recovery always runs — even when no hooks are registered and even when `SkipHooks` is true
- When no hooks are registered, the only overhead is the panic recovery wrapper
- `Raw`/`RawExec` participate in global hook chains

### Tests Required

- [x] Execution order: hooks execute in registration order (before logic: f→g→h)
- [x] Reverse execution order: after logic runs h→g→f
- [x] Global hooks run before entity hooks regardless of registration order
- [x] Panic in hook → caught, converted to error, operation returns error
- [x] Panic in terminal handler (DB operation) → caught, converted to error
- [x] Default panic handler returns descriptive error (no logging — consumers use WithPanicHandler for that)
- [x] Custom `WithPanicHandler` receives context, recovered value, table, op
- [x] Custom panic handler's returned error is what the caller sees
- [x] Empty hook chain → operation called directly
- [x] Chain with only global hooks → no entity hook overhead
- [x] Chain with only entity hooks → global layer is no-op passthrough

### Completion Record

**Date completed:** 2026-04-13

**Files created:**
- `hook/chain.go` — `PanicHandler` type, `BuildMutationChain`, `BuildQueryChain`, panic recovery wrappers
- `hook/chain_test.go` — 11 test cases covering execution order, panic recovery, custom handlers, empty chains, entity-only chains

**Files modified:**
- `cmd/sqlgen/gen/templates/table/client.go.tmpl` — added `mutationHooks`, `queryHooks`, `panicHandler` fields to entity client struct; updated constructor to accept hook slices
- `cmd/sqlgen/gen/templates/view/client.go.tmpl` — same changes for view entity clients
- `cmd/sqlgen/gen/templates/client.go.tmpl` — unified client passes hooks/panicHandler to entity constructors; Raw/RawExec wired through hook chains with zero-cost bypass when no hooks registered
- `cmd/sqlgen/gen/context_table.go` — added `hook` import to table context
- `cmd/sqlgen/gen/context_view.go` — added `hook` import to view context
- `cmd/sqlgen/gen/client_test.go` — updated test patterns for new constructor signatures and hook fields
- `cmd/sqlgen/gen/unified_client_test.go` — updated test patterns for new constructor calls and Raw/RawExec
- `cmd/sqlgen/gen/view_test.go` — updated test patterns for new view client struct and constructor
- Golden files regenerated: `client_products_gen.go`, `client_products_readonly_gen.go`, `client_order_items_gen.go`, `unified_client_gen.go`, `view_product_summary_client_gen.go`, `view_product_summary_get_gen.go`

**Notes:**
- `PanicHandler` uses `hook.TableName` (not `string`) for the table parameter — PRD updated to match.
- Default panic recovery returns an error only (no slog logging) — consumers use `WithPanicHandler` for logging/reporting. PRD updated to match.
- Panic recovery always runs — Raw/RawExec have no early-return bypass. PRD updated: "minimal overhead" replaces "zero-cost".
- Raw/RawExec use `hook.QueryOp("raw_query")` and `hook.MutationOp("raw_exec")` string values (not formal constants) since the PRD does not define operation constants for raw queries.
- Entity hooks created via `ForMutation`/`ForQuery` automatically pass through for non-matching tables, ensuring correct ordering without explicit global/entity separation.

---

## 7.4 CallOptions Integration — Per-call options, SkipHooks, relationship propagation

**PRD Reference:** Sections 9.6, 9.8

**Status:** Complete

### Tasks

- [x] Ensure `CallOptions[FO]` generic struct includes `SkipCache`, `SkipEvents`, `SkipHooks`, `FieldOptions *FO` (already defined in shared types — verify)
- [x] Wire `SkipHooks` flag into hook chain execution: when true, bypass all hooks except panic recovery
- [x] Implement `SkipHooks` implies `SkipCache` and `SkipEvents` logic
- [x] Wire `CallOptions` propagation through the hook chain (options available in `MutationContext`/`QueryContext` or passed alongside)
- [x] Ensure relationship loads inherit parent `CallOptions` (already in codegen templates — verify and update if needed)
- [x] Implement empty `FieldOptions` short-circuit: `&FieldOptions{}` (all false) skips entity fetch on mutations, returns nil
- [x] Update codegen golden tests to reflect hook chain integration in generated methods

### Acceptance Criteria

- `SkipHooks = true` bypasses all hooks except panic recovery
- `SkipHooks` implies `SkipCache` and `SkipEvents` (cache and events are implemented as hooks)
- Panic recovery always runs regardless of `SkipHooks`
- When no options are passed, default behavior applies (all hooks, cache, events execute)
- Relationship loads inherit parent `CallOptions` automatically
- Empty `&FieldOptions{}` on mutation methods causes the method to skip entity fetch and return nil
- `CallOptions` with `FieldOptions = nil` selects all columns, no relationships (existing behavior)
- Options correctly propagate through the hook chain to the terminal handler

### Tests Required

- [x] `SkipHooks = true` bypasses all registered hooks
- [x] `SkipHooks = true` does NOT bypass panic recovery
- [x] `SkipHooks` implies `SkipCache` and `SkipEvents`
- [x] Default call (no options) executes all hooks
- [x] Relationship loads inherit parent `CallOptions`
- [x] Empty `FieldOptions` short-circuit returns nil on mutation methods
- [x] Non-empty `FieldOptions` selects specified columns
- [x] `nil` `FieldOptions` returns full entity (default behavior)
- [x] Options propagate through hook chain correctly

### Completion Record

**Date completed:** 2026-04-13

**Files created:**
- None

**Files modified:**
- `hook/hook.go` — added `CallOptions any` field to `MutationContext` and `QueryContext` for options propagation through hook chains
- `hook/chain_test.go` — added 7 tests: SkipHooks bypass (mutation + query), SkipHooks preserves panic recovery (mutation + query), CallOptions propagation (mutation + query)
- `cmd/sqlgen/gen/context_shared.go` — updated `resolveCallOptions` body: SkipHooks implies SkipCache and SkipEvents
- `cmd/sqlgen/gen/templates/table/client.go.tmpl` — added `executeMutation` and `executeQuery` helper methods
- `cmd/sqlgen/gen/templates/view/client.go.tmpl` — added `executeQuery` helper method
- `cmd/sqlgen/gen/templates/table/get.go.tmpl` — wrapped Get (OpGet) and GetMany (OpGetMany) in hook chains; internal calls use SkipHooks
- `cmd/sqlgen/gen/templates/table/create.go.tmpl` — wrapped Create (OpCreate) and CreateMany (OpCreateMany) in hook chains
- `cmd/sqlgen/gen/templates/table/update.go.tmpl` — wrapped Update (OpUpdate), UpdateMany (OpUpdateMany), UpdateWhere (OpUpdateWhere) in hook chains
- `cmd/sqlgen/gen/templates/table/delete.go.tmpl` — wrapped all SoftDelete*, Restore*, HardDelete* in hook chains (9 methods total)
- `cmd/sqlgen/gen/templates/table/upsert.go.tmpl` — wrapped Upsert (OpUpsert) in hook chain
- `cmd/sqlgen/gen/templates/table/increment.go.tmpl` — wrapped Increment (OpIncrement) in hook chain
- `cmd/sqlgen/gen/templates/table/exists.go.tmpl` — wrapped Exists and ExistsWhere (OpExists) in hook chains
- `cmd/sqlgen/gen/templates/table/count.go.tmpl` — wrapped Count (OpCount) in hook chain
- `cmd/sqlgen/gen/templates/table/pagination.go.tmpl` — wrapped Paginate (OpPaginate) and Connection (OpConnection) in hook chains
- `cmd/sqlgen/gen/templates/view/get.go.tmpl` — wrapped view Get and GetMany in hook chains
- `cmd/sqlgen/gen/templates/view/count.go.tmpl` — wrapped view Count in hook chain
- `cmd/sqlgen/gen/templates/view/pagination.go.tmpl` — wrapped view Paginate and Connection in hook chains
- Golden files regenerated: all `*_gen.go` files in `cmd/sqlgen/gen/testdata/golden/`
- Test files updated: `create_test.go`, `update_test.go`, `delete_test.go`, `upsert_test.go`, `increment_test.go`, `pagination_table_test.go`

**Notes:**
- Every generated entity method now invokes the hook chain via `executeMutation`/`executeQuery` helpers on the entity client.
- When `SkipHooks = true`, the method passes nil hooks to the chain builder — only panic recovery wraps the terminal handler.
- Internal method calls (Get from Create, GetMany from Paginate, Count from Connection, etc.) force `SkipHooks = true` on the internal call to prevent nested hook invocation.
- `CallOptions` is available to hooks via the `CallOptions any` field on `MutationContext`/`QueryContext`. Hooks can type-assert for table-specific access.
- FieldOptions short-circuit and relationship propagation were already implemented in prior phases — verified and confirmed working.
- `resolveCallOptions` now sets `SkipCache = true` and `SkipEvents = true` when `SkipHooks = true`, because cache and events are implemented as hooks (PRD sections 27, 28).
