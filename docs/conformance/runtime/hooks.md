# Runtime Requirements: Hooks

<!-- seeding: in-progress -->
<!-- packages: hook -->
<!-- matrix: hooks rows=hook-point cols=fires,order,skip,panic-recovery -->

Hooks and middleware (§21) and operation-level tracing through hooks (§24.2). See
[README.md](../README.md) for the format.

## §21.3 Registration
<!-- prd: 21.3 sha256: f38531a0ea87324e -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hook.register-options | The unified client registers hooks through functional options `WithMutationHook(h hook.MutationHook) ClientOption` and `WithQueryHook(h hook.QueryHook) ClientOption` | `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_clientOption` | all |  |

## §21.6 Panic Recovery
<!-- prd: 21.6 sha256: 5352bfd541d8b41b -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hook.panic-handler-option | The unified client exposes `WithPanicHandler(handler func(ctx context.Context, r any, table hook.TableName, op string) error) ClientOption` | `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_clientOption` | all |  |
