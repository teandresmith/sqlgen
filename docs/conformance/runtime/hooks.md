# Runtime Requirements: Hooks

<!-- seeding: complete -->
<!-- packages: hook -->
<!-- matrix: hooks rows=chain:mutation,query cols=order,modify,short-circuit,abort,panic-recovery,panic-handler -->

Hooks and middleware (§21) and operation-level tracing through hooks (§24.2). See
[README.md](../README.md) for the format.

A row here states the runtime `hook` package's behavior: the chain `BuildMutationChain` /
`BuildQueryChain` composes, and the helpers. How each generated method fires the chain (its op,
`PK`, `Input` and `AffectedPKs`, an aborting hook writing nothing, the method's panic prefix) is
in [client.md](../client.md), and the generated event hooks are in [cli/gen.md](../cli/gen.md).

## §21.1 Design Principles
<!-- prd: 21.1 sha256: f9819517fe7014d1 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hook.recovery-without-hooks | Panic recovery is present when no hooks are registered: a chain built with no hooks still turns a panic in the operation into the recovery error | `unit: hook: TestBuildMutationChain_skipHooksPreservesPanicRecovery`; `unit: hook: TestBuildQueryChain_skipHooksPreservesPanicRecovery` | all |  |

## §21.2 Hook Types
<!-- prd: 21.2 sha256: a6e8d120d9159205 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hook.mutation-hook-types | `MutationHook` is `func(next MutationHandler) MutationHandler`, and `MutationHandler` is `func(ctx context.Context, m *MutationContext) (any, error)` | `unit: hook: TestMutationHookSignature` | all |  |
| hook.query-hook-types | `QueryHook` is `func(next QueryHandler) QueryHandler`, and `QueryHandler` is `func(ctx context.Context, q *QueryContext) (any, error)` | `unit: hook: TestQueryHookSignature` | all |  |
| hook.mutation-context-fields | `MutationContext` has exactly the fields `Op MutationOp`, `Table TableName`, `Schema string`, `PK any`, `Input any`, `CallOptions any`, `AffectedPKs []any`, `AffectedTenants []any` and `Tenant any` | `unit: hook: TestMutationContextFields` | all |  |
| hook.query-context-fields | `QueryContext` has exactly the fields `Op QueryOp`, `Table TableName`, `Schema string`, `PK any`, `Input any`, `CallOptions any` and `Tenant any` | `unit: hook: TestQueryContextFields` | all |  |
| hook.mutation-op-values | The `MutationOp` constants are `create`, `create_many`, `update`, `update_many`, `update_where`, `upsert`, `upsert_many`, `soft_delete`, `soft_delete_many`, `soft_delete_where`, `restore`, `restore_many`, `restore_where`, `hard_delete`, `hard_delete_many`, `hard_delete_where` and `increment` | `unit: hook: TestMutationOpConstants` | all |  |
| hook.query-op-values | The `QueryOp` constants are `get`, `get_many`, `exists`, `count`, `paginate`, `connection`, `stream` and `refresh` | `unit: hook: TestQueryOpConstants` | all |  |
| hook.table-name-type | `TableName` is a string type, so a context's `Table` compares directly against a constant | `unit: hook: TestTableNameType` | all |  |
| hook.call-options-reach-hooks | A hook reads the `CallOptions` the caller set on the context and can type-assert it, on both chains | `unit: hook: TestBuildMutationChain_callOptionsPropagate`; `unit: hook: TestBuildQueryChain_callOptionsPropagate` | all |  |
| hook.affected-pks-after-next | The `AffectedPKs` the terminal sets is visible to an outer hook once `next` returns | `unit: hook: TestMutationContextAffectedPKsVisibleToOuterHook` | all |  |
| hook.helpers.other-table-pass-through | On a table other than their own, `ForMutation` and `ForQuery` pass through to `next` without running `fn`, and the caller gets `next`'s result | `unit: hook: TestForMutation/passes through when table does not match`; `unit: hook: TestForQuery/passes through when table does not match`; `unit: hook: TestBuildMutationChain_onlyEntityHooks` | all |  |
| hook.helpers.typed-call | On their own table, with `Input` of the declared type, `ForMutation` and `ForQuery` call `fn` with that same input value, typed | `unit: hook: TestForMutation/executes typed fn when table matches`; `unit: hook: TestForQuery/executes typed fn when table matches` | all |  |
| hook.helpers.table-value-match | The table helpers match the whole `TableName` value: a hook on `public.users` never fires for `audit.users`, for `ForMutation`, `ForQuery` and `ForTable` | `unit: hook: TestHelpersMatchTheTableValueAlone/ForMutation`; `unit: hook: TestHelpersMatchTheTableValueAlone/ForQuery`; `unit: hook: TestHelpersMatchTheTableValueAlone/ForTable` | all |  |
| hook.helpers.input-mismatch | On their own table, when `Input` is not the declared type (another type, or nil), `ForMutation` and `ForQuery` return `hook: type assertion failed for {op} {table}: expected {Input}, got {actual}` without calling `fn` or `next` | `unit: hook: TestForMutation/an input of another type returns an error without running fn or next`; `unit: hook: TestForMutation/a nil input returns an error without running fn or next`; `unit: hook: TestForQuery/an input of another type returns an error without running fn or next`; `unit: hook: TestForQuery/a nil input returns an error without running fn or next` | all |  |
| hook.helpers.result-mismatch | When `next` succeeds with a non-nil result that is not the declared `Result`, the typed `next` returns `hook: result type assertion failed for {op} {table}: expected {Result}, got {actual}`, after the operation has run | `unit: hook: TestForMutation/a result of another type returns an error after next ran`; `unit: hook: TestForQuery/a result of another type returns an error after next ran` | all |  |
| hook.helpers.nil-result | When `next` succeeds with a nil result, the typed `next` returns the zero `Result` and no error | `unit: hook: TestForMutation/a nil result reaches fn as the zero value`; `unit: hook: TestForQuery/a nil result reaches fn as the zero value` | all |  |

## §21.3 Registration
<!-- prd: 21.3 sha256: f38531a0ea87324e -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hook.register-options | The unified client registers hooks through functional options `WithMutationHook(h hook.MutationHook) ClientOption` and `WithQueryHook(h hook.QueryHook) ClientOption` | `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_clientOption` | all |  |
| hook.mutation-chain.registration-order | The mutation chain runs hooks in registration order, the first registered outermost: with `f`, `g`, `h` registered, before-logic runs f→g→h and after-logic h→g→f | `unit: hook: TestBuildMutationChain_executionOrder`; `unit: hook: TestBuildMutationChain_onlyGlobalHooks` | all | hooks: mutation × order |
| hook.query-chain.registration-order | The query chain runs hooks in registration order, the first registered outermost: with `f`, `g`, `h` registered, before-logic runs f→g→h and after-logic h→g→f | `unit: hook: TestBuildQueryChain_executionOrder` | all | hooks: query × order |
| hook.chain.no-kind-reordering | The chain does not tell global hooks from entity hooks: an entity hook built with `ForMutation` / `ForQuery` registered before a global hook runs outside it, and one registered after runs inside it | `unit: hook: TestBuildMutationChain_registrationOrderAcrossHookKinds/entity registered first`; `unit: hook: TestBuildMutationChain_registrationOrderAcrossHookKinds/global registered first`; `unit: hook: TestBuildQueryChain_registrationOrderAcrossHookKinds/entity registered first`; `unit: hook: TestBuildQueryChain_registrationOrderAcrossHookKinds/global registered first` | all |  |

## §21.4 Conditional Execution Helpers
<!-- prd: 21.4 sha256: 9e174a11b07d9762 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hook.on-mutation | `OnMutation(h, ops...)` runs `h` only when `Op` is one of `ops`; any other op goes to `next` without `h`, and the caller gets `next`'s result | `unit: hook: TestOnMutation/fires on matching op`; `unit: hook: TestOnMutation/fires on second matching op`; `unit: hook: TestOnMutation/passes through on non-matching op` | all |  |
| hook.on-query | `OnQuery(h, ops...)` runs `h` only when `Op` is one of `ops`; any other op goes to `next` without `h`, and the caller gets `next`'s result | `unit: hook: TestOnQuery/fires on matching op`; `unit: hook: TestOnQuery/fires on second matching op`; `unit: hook: TestOnQuery/passes through on non-matching op` | all |  |
| hook.for-table | `ForTable(h, tables...)` runs `h` only when `Table` is one of `tables`; any other table goes to `next` without `h`, and the caller gets `next`'s result | `unit: hook: TestForTable/fires on matching table`; `unit: hook: TestForTable/fires on second matching table`; `unit: hook: TestForTable/passes through on non-matching table` | all |  |
| hook.combined-conditions | `OnMutation(ForTable(h, tables...), ops...)` runs `h` only when both the table and the op match | `unit: hook: TestComposedHelpers/matching table and op`; `unit: hook: TestComposedHelpers/matching table wrong op`; `unit: hook: TestComposedHelpers/wrong table matching op`; `unit: hook: TestComposedHelpers/wrong table and op` | all |  |
| hook.reject-mutation | `RejectMutation(ops...)` returns `hook: operation {op} is not allowed on {table}` for each listed op, not only the first, without calling `next`, and passes any other op through to `next` | `unit: hook: TestRejectMutation/returns error on matching op`; `unit: hook: TestRejectMutation/rejects every listed op on every table`; `unit: hook: TestRejectMutation/passes through on non-matching op` | all |  |

## §21.5 Hook Lifecycle
<!-- prd: 21.5 sha256: e0d73e7468d1e59c -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hook.mutation-chain.modify | A mutation hook can change `Input` before `next`, and the terminal sees the change; and it can replace the result after `next`, and the caller gets the replacement | `unit: hook: TestBuildMutationChain_hookCapabilities/modify before`; `unit: hook: TestBuildMutationChain_hookCapabilities/modify after` | all | hooks: mutation × modify |
| hook.query-chain.modify | A query hook can change `Input` before `next`, and the terminal sees the change; and it can replace the result after `next`, and the caller gets the replacement | `unit: hook: TestBuildQueryChain_hookCapabilities/modify before`; `unit: hook: TestBuildQueryChain_hookCapabilities/modify after` | all | hooks: query × modify |
| hook.mutation-chain.short-circuit | A mutation hook can return a result without calling `next`: the terminal does not run, and the caller gets that result as-is | `unit: hook: TestBuildMutationChain_hookCapabilities/short-circuit` | all | hooks: mutation × short-circuit |
| hook.query-chain.short-circuit | A query hook can return a result without calling `next`: the terminal does not run, and the caller gets that result as-is | `unit: hook: TestBuildQueryChain_hookCapabilities/short-circuit` | all | hooks: query × short-circuit |
| hook.mutation-chain.abort | A mutation hook can return an error without calling `next`: the terminal does not run, and the caller gets that error as-is with a nil result | `unit: hook: TestBuildMutationChain_hookCapabilities/abort` | all | hooks: mutation × abort |
| hook.query-chain.abort | A query hook can return an error without calling `next`: the terminal does not run, and the caller gets that error as-is with a nil result | `unit: hook: TestBuildQueryChain_hookCapabilities/abort` | all | hooks: query × abort |

## §21.6 Panic Recovery
<!-- prd: 21.6 sha256: 5352bfd541d8b41b -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hook.panic-handler-option | The unified client exposes `WithPanicHandler(handler func(ctx context.Context, r any, table hook.TableName, op string) error) ClientOption` | `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_clientOption` | all |  |
| hook.panic.default-error | With no panic handler, a recovered panic becomes `sqlgen: panic in {op} {table}: {value}`. `{table}` is `Table`, with `Schema` prefixed only when `Table` does not already start with it, so the schema is never printed twice; this holds on both chains | `unit: hook: TestDefaultPanicError_QualifiedTable/qualified value`; `unit: hook: TestDefaultPanicError_QualifiedTable/second schema`; `unit: hook: TestDefaultPanicError_QualifiedTable/schema named like the table`; `unit: hook: TestDefaultPanicError_QualifiedTable/bare value with schema`; `unit: hook: TestDefaultPanicError_QualifiedTable/schemaless` | all |  |
| hook.mutation-chain.panic-caught | The mutation chain catches a panic in a hook or in the operation and returns it as an error with a nil result | `unit: hook: TestBuildMutationChain_panicInHook`; `unit: hook: TestBuildMutationChain_panicInTerminal` | all | hooks: mutation × panic-recovery |
| hook.query-chain.panic-caught | The query chain catches a panic in a hook or in the operation and returns it as an error with a nil result | `unit: hook: TestBuildQueryChain_panicInHook`; `unit: hook: TestBuildQueryChain_panicInTerminal` | all | hooks: query × panic-recovery |
| hook.panic.outermost | Recovery wraps the entire chain, outside every registered hook: a panic in an inner hook unwinds through the outer hooks, whose after-logic does not run, and the caller gets the recovery error; on both chains | `unit: hook: TestBuildMutationChain_panicRecoveryIsOutermost`; `unit: hook: TestBuildQueryChain_panicRecoveryIsOutermost` | all |  |
| hook.mutation-chain.panic-handler | A panic handler replaces the default on the mutation chain: it receives the context, the recovered value, `Table` and `Op`, and its error is the one the caller sees | `unit: hook: TestBuildMutationChain_customPanicHandler` | all | hooks: mutation × panic-handler |
| hook.query-chain.panic-handler | A panic handler replaces the default on the query chain: it receives the context, the recovered value, `Table` and `Op`, and its error is the one the caller sees, as-is | `unit: hook: TestBuildQueryChain_customPanicHandler` | all | hooks: query × panic-handler |

## §21.7 Global Hooks (untyped — all tables)
<!-- prd: 21.7 sha256: 1f02e1ed31501169 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hook.reject-mutation.all-tables | `RejectMutation` blocks a listed op across all tables: it names no table, and rejects the op on every table it sees | `unit: hook: TestRejectMutation/rejects every listed op on every table` | all |  |

## §21.8 Entity-Level Hooks (type-safe — specific tables)
<!-- prd: 21.8 sha256: 12e436f8a834a91d -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hook.entity.modify-input | A `ForMutation` hook's `fn` can change its typed input, and the terminal sees the change on the caller's input value | `unit: hook: TestForMutation/the terminal sees the input fn modified` | all |  |
| hook.entity.short-circuit | A `ForQuery` hook's `fn` can return a result without calling `next`: the operation does not run, and the caller gets that result as-is | `unit: hook: TestForQuery/fn can return a result without calling next` | all |  |

## §24.2 Operation-Level Tracing (via Hooks)
<!-- prd: 24.2 sha256: 8b4c24f9f0ca2801 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hook.context-to-next | The context a hook passes to `next` is the one every inner hook and the operation receive, and it descends from the caller's; on both chains | `unit: hook: TestBuildMutationChain_hookContextReachesTerminal`; `unit: hook: TestBuildQueryChain_hookContextReachesTerminal` | all |  |

## Unclaimed tests

| Unclaimed test | Reason |
|---|---|
| hook: TestMutationContextAffectedPKs | Assigns `AffectedPKs` on a bare struct and reads it back. No chain runs, so it cannot fail on the terminal setting it for outer hooks (`hook.affected-pks-after-next` rests on `TestMutationContextAffectedPKsVisibleToOuterHook`) |
| hook: TestBuildMutationChain_skipHooksBypassesAllHooks | Builds the chain with nil hooks and never passes the registered ones in, so the hooks cannot run whatever the code does. `SkipHooks` is applied by the generated `executeMutation` (§9.6), not by the `hook` package |
| hook: TestBuildQueryChain_skipHooksBypassesAllHooks | Builds the chain with nil hooks and never passes the registered ones in, so the hooks cannot run whatever the code does. `SkipHooks` is applied by the generated `executeQuery` (§9.6), not by the `hook` package |
| hook: TestBuildMutationChain_emptyHooks | Checks that a chain with no hooks returns the terminal's result. The PRD states only that recovery still wraps an empty chain (`hook.recovery-without-hooks`); §21.1's "minimal overhead" is not measured |
| hook: TestBuildQueryChain_emptyHooks | Checks that a chain with no hooks returns the terminal's result. The PRD states only that recovery still wraps an empty chain (`hook.recovery-without-hooks`); §21.1's "minimal overhead" is not measured |
