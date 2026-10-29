# Generated Client Requirements

<!-- seeding: in-progress -->
<!-- matrix: client-methods rows=method cols=happy-path,nil-empty-input,not-found,soft-delete,transaction,hooks,error-mapping,field-selection,lock-mode,composite-pk -->

Requirements of the generated client, proved by the cross-dialect conformance suite in
`cmd/sqlgen/testdata/conformance` (see [README.md](README.md) for the format). Each method's
`client-methods` cells are seeded together; a row may cite a section another registry file owns.

## §8.7 Composite Primary Keys
<!-- prd: 8.7 sha256: dc244e8091a8bad5 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hard-delete-many.composite-pk | Batch operations accept slices of the `{Table}PK` type, and the WHERE clause matches all PK columns, so `HardDeleteMany` deletes only the listed tuples | `conformance: HardDeleteMany/composite keys delete only the listed tuples` | all | client-methods: HardDeleteMany × composite-pk |

## §9.2 Write Operations
<!-- prd: 9.2 sha256: 7a20f40676acbad6 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| update.single-pk-signature | On a single-PK table `Update` takes the PK column's Go type directly and the update input, `Update(ctx, id <PK type>, *Update{Table}Input, ...func(*CallOptions[FO])) (*{Table}, error)`, and matches the row by that PK | `unit: cmd/sqlgen/gen: TestUpdateTemplate_partialFields` | postgres |  |
| update.empty-field-options-both-paths | A non-nil `FieldOptions` that selects nothing makes `Update` return `nil, nil` without the re-fetch, on the empty-update path as well as after the UPDATE | `unit: cmd/sqlgen/gen: TestUpdateTemplate_emptyFieldOptions/single PK postgres`; `unit: cmd/sqlgen/gen: TestUpdateTemplate_emptyFieldOptions/single PK mysql`; `unit: cmd/sqlgen/gen: TestUpdateTemplate_emptyFieldOptions/composite PK`; `example: postgres: TestUpdate_EmptyFieldOptions_SkipsRefetch_Postgres`; `example: postgres: TestUpdate_EmptyFieldOptions_EmptyUpdate_NoStatements_Postgres` | postgres, mysql |  |
| update-where.empty-filter | `UpdateWhere` returns `ErrEmptyFilter` when the filter produces zero conditions | `unit: cmd/sqlgen/gen: TestUpdateTemplate_updateWhere_postgres`; `example: postgres: TestUpdateWhere_EmptyFilter_ReturnsErrEmptyFilter_Postgres`; `example: mysql: TestUpdateWhere_EmptyFilter_ReturnsErrEmptyFilter_MySQL`; `example: sqlite: TestUpdateWhere_EmptyFilter_ReturnsErrEmptyFilter_SQLite` | all |  |
| update-where.no-match-nil | `UpdateWhere` is idempotent: when no row matches the filter it returns no entities and no error | `unit: cmd/sqlgen/gen: TestUpdateTemplate_updateWhere_idempotent`; `example: postgres: TestUpdateWhere_ZeroMatch_ReturnsNilNoError_Postgres`; `example: mysql: TestUpdateWhere_ZeroMatch_ReturnsNilNoError_MySQL`; `example: sqlite: TestUpdateWhere_ZeroMatch_ReturnsNilNoError_SQLite` | all |  |

## §9.3 Delete Operations
<!-- prd: 9.3 sha256: 9a222a59e2330f57 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hard-delete-many.happy-path | `HardDeleteMany(ctx, []pk)` is a batch hard delete: a physical DELETE of the rows whose keys are listed | `conformance: HardDeleteMany/deletes only the listed keys` | all | client-methods: HardDeleteMany × happy-path |
| soft-delete.many-signature | `SoftDeleteMany` takes `(ctx, []pk, ...func(*CallOptions[FO]))` and returns `([]*Entity, error)` | `unit: cmd/sqlgen/gen: TestDeleteTemplate_softDeleteMany` | postgres |  |
| soft-delete.where-signature | `SoftDeleteWhere` takes `(ctx, *Filter, ...func(*CallOptions[FO]))` and returns `([]*Entity, error)` | `unit: cmd/sqlgen/gen: TestDeleteTemplate_softDeleteWhere_postgres` | postgres |  |
| hard-delete.where-signature | `HardDeleteWhere` takes `(ctx, *Filter, ...func(*CallOptions[FO]))` and returns only `error` | `unit: cmd/sqlgen/gen: TestDeleteTemplate_hardDeleteWhere` | postgres |  |
| hard-delete.where-empty-filter | `HardDeleteWhere` returns `ErrEmptyFilter` when the filter produces zero conditions | `unit: cmd/sqlgen/gen: TestDeleteTemplate_goldenFile_noSoftDelete` | postgres |  |

## §9.4a Streaming Operations
<!-- prd: 9.4a sha256: cd5f0456155afb1d -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| stream.input-no-limit-offset | `Stream{Table}Input` carries `Filter` and `Sorts` and has no `Limit` or `Offset` field | `unit: cmd/sqlgen/gen: TestStream_InputHasNoLimitOrOffset`; `unit: cmd/sqlgen/gen: TestStream_TypesEmitted` | all |  |
| stream.field-options-scalar-only | `Stream{Table}FieldOptions` has one `bool` per scalar column and no relationship field, even when the table has relationships | `unit: cmd/sqlgen/gen: TestStream_FieldOptionsScalarOnly`; `unit: cmd/sqlgen/gen: TestStream_TypesEmitted` | all |  |
| stream.select-unbounded | `Stream`'s `SELECT` is built with no `Limit` and no `Offset` | `unit: cmd/sqlgen/gen: TestStream_NoLimitOffsetInSelect` | all |  |
| stream.iterator-per-row | `Stream` returns `iter.Seq2[*{Table}, error]` and yields each row as it is scanned, through a single-row scan helper, without materializing the result set | `unit: cmd/sqlgen/gen: TestStream_MethodBodyEmission`; `unit: cmd/sqlgen/gen: TestStream_ScanRowHelperEmitted` | all |  |
| stream.skip-cache-forced | `Stream` forces `CallOptions.SkipCache` to `true` whatever the caller set | `unit: cmd/sqlgen/gen: TestStream_MethodBodyEmission` | all |  |
| stream.op-stream | `Stream` runs its query-hook chain with `Op: hook.OpStream` | `unit: cmd/sqlgen/gen: TestStream_OpStreamReferenced` | all |  |
| stream.tx-refused | Inside a transaction, `Stream` without `CallOptions.AllowInTransaction` yields one error naming `Connection` in a loop and the `AllowInTransaction` opt-in, then returns | `unit: cmd/sqlgen/gen: TestStream_InTransactionGuardEmitted` | all |  |
| stream.guard-before-hooks | `Stream`'s in-transaction refusal runs after `resolveCallOptions` and before the hook chain is built, so a refused `Stream` fires no hook | `unit: cmd/sqlgen/gen: TestStream_GuardPrecedesHookChain` | all |  |
| stream.conditions-like-getmany | `Stream`'s `WHERE` carries the soft-delete default and the tenant predicate, as `GetMany`'s does | `unit: cmd/sqlgen/gen: TestStream_TenancyAndSoftDeleteApplied` | all |  |

## §9.4c Nil input handling
<!-- prd: 9.4c sha256: f8d2c84924befe60 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hard-delete-many.nil-empty-input | Bulk write variants distinguish the two nils: a nil slice is simply no rows, since nil and empty already mean the same thing, so `HardDeleteMany` with no keys does nothing | `conformance: HardDeleteMany/nil key list deletes nothing`; `conformance: HardDeleteMany/empty key list deletes nothing` | all | client-methods: HardDeleteMany × nil-empty-input |
| nil-input.batch-no-statement | `CreateMany`, `UpdateMany`, `UpsertMany`, `SoftDeleteMany`, `RestoreMany` and `HardDeleteMany` send no statement for a nil or empty slice and read nothing back | `example: postgres: TestEmptyBatch_HardDeleteManyUnaffected`; `unit: cmd/sqlgen/gen: TestUpsertManyTemplate_goldenFile_dbStrategy`; `example: postgres: TestEmptyBatch_ReadsNothingAndReturnsNothing` | postgres |  |
| nil-input.upsert | `Upsert(ctx, nil, target)` returns `ErrNilInput`, checked before the mutation executor so no hook fires | `unit: cmd/sqlgen/gen: TestUpsertTemplate_goldenFile_singlePK`; `unit: cmd/sqlgen/gen: TestUpsertTemplate_goldenFile_compositePK` | postgres |  |
| nil-input.upsert-many-nil-element | `UpsertMany` returns `ErrNilInput` when any element is nil, and the guard runs before the mutation executor, so a nil element fires no hook | `unit: cmd/sqlgen/gen: TestUpsertManyTemplate_nilInputGuard`; `unit: cmd/sqlgen/gen: TestUpsertManyTemplate_goldenFile_compositePK` | postgres |  |

## §9.5 Edge Case Behaviors
<!-- prd: 9.5 sha256: 13ecf0398b922d84 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| edge.upsert-many.demoted-rowid-alias-excluded | `UpsertMany` excludes a SQLite rowid alias that `primary_key.columns` demoted from its update set | `unit: cmd/sqlgen/cli: TestGeneratePipeline_DemotedRowIDAliasStaysOutOfUpsertManySet` | sqlite |  |
| edge.by-key-noop-no-side-effects | A by-key `SoftDelete`, `Restore` or `HardDelete` whose key matches no row records no affected key: no cache invalidation and no event | `unit: cmd/sqlgen/gen: TestDeleteTemplate_byKeyNoOp_recordsNoAffectedPKs/postgres exec`; `unit: cmd/sqlgen/gen: TestDeleteTemplate_byKeyNoOp_recordsNoAffectedPKs/mysql exec`; `unit: cmd/sqlgen/gen: TestDeleteTemplate_byKeyNoOp_recordsNoAffectedPKs/composite pk`; `unit: cmd/sqlgen/gen: TestDeleteTemplate_byKeyNoOp_recordsNoAffectedPKs/tenanted postgres returning`; `unit: cmd/sqlgen/gen: TestDeleteTemplate_byKeyNoOp_recordsNoAffectedPKs/tenanted sqlite returning`; `unit: cmd/sqlgen/gen: TestDeleteTemplate_byKeyNoOp_recordsNoAffectedPKs/tenanted mysql pre-read`; `example: events: TestEvent_MissingKeyPublishesNothing` | all |  |
| edge.batch-records-written-keys | A batch `SoftDeleteMany`, `RestoreMany` or `HardDeleteMany` records only the keys whose rows it wrote, in list order; a batch matching no row records nothing | `unit: cmd/sqlgen/gen: TestDeleteTemplate_batchNoOp_recordsOnlyWrittenKeys`; `unit: cmd/sqlgen/gen: TestTableContext_KeySet`; `conformance: HardDeleteMany/records only the keys it wrote as affected`; `conformance: HardDeleteMany/records only the composite keys it wrote as affected`; `example: events: TestEvent_BatchPublishesOnlyWrittenKeys`; `example: events: TestEvent_MissingKeyPublishesNothing/soft delete many`; `example: events: TestEvent_MissingKeyPublishesNothing/restore many`; `example: events: TestEvent_MissingKeyPublishesNothing/hard delete many`; `example: mysql: TestAffectedKeys_BatchRecordsOnlyWrittenKeys`; `example: mysql: TestAffectedKeys_BatchMatchingNoRowRecordsNothing`; `example: mysql: TestAffectedKeys_CompositeBatchRecordsOnlyWrittenKeys`; `conformance: SoftDeleteMany/records only the keys it wrote as affected`; `conformance: RestoreMany/records only the keys it wrote as affected`; `example: mysql: TestAffectedKeys_BatchRecordsWrittenKeysInListOrder`; `example: postgres: TestAffectedKeys_BatchRecordsNonComparableKeys/binary keys`; `example: postgres: TestAffectedKeys_BatchRecordsNonComparableKeys/network keys` | all |  |
| edge.batch-db-matched-superset | When the database matched more rows in a sub-batch than the listed keys equal to its returned keys (a case-insensitive collation), the batch records every listed key of that sub-batch | `example: mysql: TestAffectedKeys_BatchRecordsKeysTheDatabaseMatched` | mysql |  |
| edge.batch-db-matched-superset-returning | On PostgreSQL and SQLite, whose batches learn written keys through `RETURNING`, a sub-batch the database matched more rows in records every listed key (a `citext` key) |  | postgres, sqlite |  |
| edge.batch-db-matched-superset-tenanted | On a tenanted table the superset keeps each key's tenant: a key equal to a returned row keeps that row's tenant, and the others take the tenant the unmatched rows share |  | all |  |
| edge.unchanged-row-matches | A row counts when the write's key matched it, whether or not a value changed: a strict `Update` or `Increment` that leaves the row as it was returns it rather than `ErrNotFound`, and a non-strict write, an `UpdateMany` item, a `Restore` of a live row and a re-`SoftDelete` record the key, on every dialect | `example: mysql: TestAffectedKeys_UnchangedRowStillMatches/strict update to the current value`; `example: mysql: TestAffectedKeys_UnchangedRowStillMatches/strict increment by zero`; `example: mysql: TestAffectedKeys_UnchangedRowStillMatches/non-strict update to the current value`; `example: mysql: TestAffectedKeys_UnchangedRowStillMatches/update many item set to the current value`; `example: mysql: TestAffectedKeys_UnchangedRowStillMatches/non-strict increment by zero`; `example: mysql: TestAffectedKeys_UnchangedRowStillMatches/restore of a live row`; `example: mysql: TestAffectedKeys_UnchangedRowStillMatches/soft delete of a soft-deleted row`; `unit: cmd/sqlgen/gen: TestUpdateTemplate_zeroCountLooksUpRowOnMySQL/mysql`; `unit: cmd/sqlgen/gen: TestUpdateTemplate_zeroCountLooksUpRowOnMySQL/tenanted mysql`; `unit: cmd/sqlgen/gen: TestUpdateTemplate_zeroCountLooksUpRowOnMySQL/postgres` | all |  |
| edge.update-records-matched-rows | An `Update` or `UpdateMany` records only the rows it matched, so a non-strict `Update` on a missing key records nothing | `unit: cmd/sqlgen/gen: TestUpdateTemplate_nonStrictNoOp_recordsNoAffectedPKs`; `unit: cmd/sqlgen/gen: TestUpdateTemplate_updateManyNoOp_recordsOnlyWrittenItems`; `example: events: TestEvent_MissingKeyPublishesNothing/non-strict update`; `example: events: TestEvent_MissingKeyPublishesNothing/update many`; `example: mysql: TestAffectedKeys_UpdateRecordsOnlyWrittenKeys` | all |  |
| edge.increment-records-matched-rows | A non-strict `Increment` on a key that matches no row records nothing | `unit: cmd/sqlgen/gen: TestIncrementTemplate_nonStrictNoOp_recordsNoAffectedPKs/postgres exec`; `unit: cmd/sqlgen/gen: TestIncrementTemplate_nonStrictNoOp_recordsNoAffectedPKs/mysql exec`; `unit: cmd/sqlgen/gen: TestIncrementTemplate_nonStrictNoOp_recordsNoAffectedPKs/composite pk`; `unit: cmd/sqlgen/gen: TestIncrementTemplate_nonStrictNoOp_recordsNoAffectedPKs/tenanted postgres returning`; `unit: cmd/sqlgen/gen: TestIncrementTemplate_nonStrictNoOp_recordsNoAffectedPKs/tenanted sqlite returning`; `unit: cmd/sqlgen/gen: TestIncrementTemplate_nonStrictNoOp_recordsNoAffectedPKs/tenanted mysql pre-read`; `example: events: TestEvent_MissingKeyPublishesNothing/non-strict increment` | all |  |
| edge.preread-keys-by-column | On MySQL, the pre-read a `*Where` write uses to learn its keys records each key with its own column values, composite and tenanted keys included | `unit: cmd/sqlgen/gen: TestKeyPreRead_mysqlScansInSortedSelectOrder/composite key delete helper`; `unit: cmd/sqlgen/gen: TestKeyPreRead_mysqlScansInSortedSelectOrder/composite key update helper`; `unit: cmd/sqlgen/gen: TestKeyPreRead_mysqlScansInSortedSelectOrder/tenanted composite key delete helper`; `unit: cmd/sqlgen/gen: TestKeyPreRead_mysqlScansInSortedSelectOrder/tenanted composite key update helper`; `unit: cmd/sqlgen/gen: TestKeyPreRead_mysqlScansInSortedSelectOrder/tenanted single key delete helper`; `unit: cmd/sqlgen/gen: TestKeyPreRead_mysqlScansInSortedSelectOrder/tenanted single key update helper`; `example: mysql: TestAffectedKeys_WhereDeleteRecordsCompositeKeysByColumn`; `example: mysql: TestAffectedKeys_UpdateWhereRecordsCompositeKeysByColumn` | mysql |  |
| edge.noop-skips-view-invalidation | A write that affected no row invalidates no view pattern | `unit: cmd/sqlgen/gen: TestCacheTemplate_viewInvalidationSkipsNoOpWrite`; `example: cache: TestViewInvalidation_NoOpWriteDoesNotFireViewPattern` | all |  |
| hard-delete-many.not-found | Delete operations are always idempotent: a listed key that does not exist is not an error | `conformance: HardDeleteMany/missing keys are not an error` | all | client-methods: HardDeleteMany × not-found |
| hard-delete-many.batch-partial | `HardDeleteMany` splits its input into sub-batches of `generation.batch_size`. If any sub-batch fails, the operation returns an error immediately, and previously executed sub-batches within the same call are not rolled back | `conformance: HardDeleteMany/a failed sub-batch keeps the sub-batches before it` | all | |
| soft-delete-many.batch-split | `SoftDeleteMany` splits its input into sub-batches of `generation.batch_size`, one statement each | `conformance: SoftDeleteMany/splits the keys into sub-batches of the table's batch size`; `unit: cmd/sqlgen/gen: TestDeleteTemplate_softDeleteMany` | all |  |
| soft-delete.missing-key | `SoftDelete` on a key that does not exist returns `nil` (idempotent) | `example: sqlite: TestSoftDeleteAndRestore_MissingKey_ReturnNilNil_SQLite/SoftDelete` | all | |
| restore.missing-key | `Restore` on a key that does not exist returns `nil` (idempotent): restoring a non-existent row is a no-op | `example: sqlite: TestSoftDeleteAndRestore_MissingKey_ReturnNilNil_SQLite/Restore` | all | |
| update.empty-input-no-statement | `Update` with an input that sets no field executes no UPDATE statement and returns the current entity unchanged | `example: postgres: TestUpdate_ZeroFields_NoUpdateSQL_Postgres`; `example: mysql: TestUpdate_ZeroFields_NoUpdateSQL_MySQL`; `example: sqlite: TestUpdate_ZeroFields_NoUpdateSQL_SQLite`; `unit: cmd/sqlgen/gen: TestUpdateTemplate_goldenFile_singlePK` | all |  |
| edge.conflict.declared-pk-backs-target | A declared `PRIMARY KEY` constraint always backs `{Table}ConflictPK`, also when the key is restated in `tables.<name>.primary_key.columns` | `unit: cmd/sqlgen/cli: TestGeneratePipeline_InlinePKRestatedInConfigKeepsConflictTarget`; `unit: cmd/sqlgen/gen: TestBuildConflictTargets_OverrideNeedsAnIndex/table-level PK restated in config keeps its target` | all |  |
| edge.conflict.one-per-constraint | Each PRIMARY KEY and UNIQUE constraint yields one `{Table}Conflict{Name}` constant mapped to that constraint's column set; a single-column name is the column's PascalCase (`sku` → `SKU`) | `unit: cmd/sqlgen/gen: TestConflictTargetGeneration` | postgres |  |
| edge.conflict.columns-lookup | A generated `{table}ConflictColumns` map ties each `{Table}ConflictTarget` constant to its column list, a composite target to all of its columns | `unit: cmd/sqlgen/gen: TestUpsertTemplate_conflictColumnsMap` | postgres |  |
| edge.upsert-many.no-keys-from-statement | `UpsertMany` builds each sub-batch as one multi-row INSERT carrying the conflict clause and never takes keys or rows from the statement: no `RETURNING`, no `LastInsertId`, no `firstID + i` derivation | `unit: cmd/sqlgen/gen: TestUpsertManyTemplate_conflictClauseFields` | all |  |
| edge.upsert-many.update-set-excludes-sequence | `UpsertMany`'s update set excludes every non-PK auto-increment column, alongside the conflict-target and PK columns | `unit: cmd/sqlgen/gen: TestUpsertManyTemplate_excludesNonKeySequenceColumns` | all |  |
| edge.upsert-many.dedupe-last-wins | `UpsertMany` dedupes its inputs by conflict target across the whole call, before splitting into sub-batches; a duplicate overwrites the earlier row's slot, so the last occurrence's values win in first-appearance order | `unit: cmd/sqlgen/gen: TestUpsertManyTemplate_dedupesLastWins` | all |  |
| edge.upsert-many.covering-target-input-keys | On a caller-known key whose every declared conflict target covers the primary key, `UpsertMany` takes `AffectedPKs` from the inputs and emits no read-back and no runtime branch | `unit: cmd/sqlgen/gen: TestUpsertManyTemplate_pkFromInput/composite-pk-every-target-covers-key` | postgres |  |
| edge.upsert-many.non-covering-target-reads-back | When a table declares a conflict target that does not cover every PK column, `UpsertMany` chooses per call from a generated coverage table: a covering target takes the input keys, a non-covering one reads the written keys back (composite keys as `[]{Table}PK`), whatever the PK strategy | `unit: cmd/sqlgen/gen: TestUpsertManyTemplate_pkFromInput/caller-strategy-with-a-non-covering-target`; `unit: cmd/sqlgen/gen: TestUpsertManyTemplate_conflictCoversPKTable`; `unit: cmd/sqlgen/gen: TestUpsertManyTemplate_compositePKReadBack` | postgres |  |
| edge.upsert-many.db-key-reads-back | On a database-generated key, `UpsertMany` reads each sub-batch's keys back by the conflict target's columns before it records `AffectedPKs` | `unit: cmd/sqlgen/gen: TestUpsertManyTemplate_dbPKReadsBackByConflictTarget` | all |  |

## §9.6 Per-Call Options
<!-- prd: 9.6 sha256: 5e44c24d55e5a9ed -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| call-options.shape-order | `CallOptions[FO any]` has the §9.6 fields in the listed order: `SkipCache`, `SkipEvents`, `SkipHooks`, then `SkipTenancy` and `Tenant` when generated, then `FieldOptions *FO`, `LockMode`, `AllowInTransaction` | `unit: cmd/sqlgen/gen: TestBuildSharedTypesContext_callOptionsFields`; `unit: cmd/sqlgen/gen: TestBuildSharedTypesContext_callOptionsFieldsTenancyEnabled` | all |  |
| call-options.skip-tenancy-gated | `CallOptions.SkipTenancy` is generated only when `tenancy.enabled: true`; a package without tenancy has no `SkipTenancy` field | `unit: cmd/sqlgen/gen: TestBuildSharedTypesContext_skipTenancyAbsentWhenDisabled`; `unit: cmd/sqlgen/gen: TestBuildSharedTypesContext_callOptionsFields`; `unit: cmd/sqlgen/gen: TestSharedTypesTemplate_tenancyEnabled_emitsSkipTenancy` | all |  |
| call-options.tenant-field-typed | With tenancy enabled and a tenanted entity, `CallOptions` carries `Tenant *<tenant type>`, concretely typed with the package's tenant Go type | `unit: cmd/sqlgen/gen: TestBuildSharedTypesContext_callOptionsFieldsTenancyEnabled` | all |  |
| call-options.skip-hooks-not-skip-tenancy | `resolveCallOptions` never sets `SkipTenancy` from `SkipHooks`: `SkipHooks` does not imply `SkipTenancy` | `unit: cmd/sqlgen/gen: TestResolveCallOptions_skipHooksDoesNotFoldSkipTenancy` | all |  |
| call-options.every-method-last-param | Every method of a table's client interface takes `opts ...func(*CallOptions[{Table}FieldOptions])` as its last parameter | `unit: cmd/sqlgen/gen: TestClientTemplate_goldenFile_fullOps`; `unit: cmd/sqlgen/gen: TestClientTemplate_goldenFile_compositePK` | postgres |  |
| call-options.relationship-propagation | `GetMany` passes its resolved `CallOptions` to the relationship loader, and every O2M and M2M child read sets `SkipCache`, `SkipEvents` and `SkipHooks` from the caller's toggles (the caller record), not from sqlgen's internal-hop options; `Get` restores the caller record onto hook-replaced options before its inner `GetMany` | `unit: cmd/sqlgen/gen: TestRelationshipCallOptions/each O2M and M2M child read inherits the parent's per-operation options`; `unit: cmd/sqlgen/gen: TestRelationshipCallOptions/GetMany hands its resolved options to the loader`; `unit: cmd/sqlgen/gen: TestRelationshipCallOptions/Get keeps the caller record when a hook replaces its options` | postgres |  |
| call-options.tenant-gated-on-tenanted-entity | With tenancy enabled but no tenanted entity in the package, `CallOptions` has no `Tenant` field and `resolveCallOptions` does no `Tenant` normalization | `unit: cmd/sqlgen/gen: TestBuildSharedTypesContext_tenantFieldAbsentWithoutTenantedTables` | all |  |

## §9.6a Lock Modes
<!-- prd: 9.6a sha256: 08fe83b1bf827a4c -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| lock.requires-transaction | `Get`, `GetMany` and `Connection` with a `LockMode` other than `LockNone` outside a transaction return an error before any SQL | `unit: cmd/sqlgen/gen: TestLockModeGuard_GetMethod_postgres`; `unit: cmd/sqlgen/gen: TestLockModeGuard_GetMethod_mysql`; `unit: cmd/sqlgen/gen: TestLockModeGuard_GetManyMethod_allDialects`; `unit: cmd/sqlgen/gen: TestLockModeGuard_ConnectionMethod_allDialects` | postgres, mysql |  |
| lock.forces-skip-cache | A `LockMode` other than `LockNone` on `Get`, `GetMany` or `Connection` forces `SkipCache = true` | `unit: cmd/sqlgen/gen: TestLockModeGuard_GetMethod_postgres`; `unit: cmd/sqlgen/gen: TestLockModeGuard_GetMethod_mysql`; `unit: cmd/sqlgen/gen: TestLockModeGuard_GetManyMethod_allDialects`; `unit: cmd/sqlgen/gen: TestLockModeGuard_ConnectionMethod_allDialects` | postgres, mysql |  |
| lock.sqlite-rejected | On SQLite, `Get`, `GetMany` and `Connection` with any `LockMode` other than `LockNone` return an error before any SQL | `unit: cmd/sqlgen/gen: TestLockModeGuard_GetMethod_sqlite`; `unit: cmd/sqlgen/gen: TestLockModeGuard_GetManyMethod_allDialects`; `unit: cmd/sqlgen/gen: TestLockModeGuard_ConnectionMethod_allDialects` | sqlite |  |
| lock.mysql-version-guard | On MySQL, `LockForUpdateNoWait` and `LockForUpdateSkipLocked` on `Get`, `GetMany` and `Connection` return an error when the server's major version is below 8 | `unit: cmd/sqlgen/gen: TestLockModeGuard_GetMethod_mysql`; `unit: cmd/sqlgen/gen: TestLockModeGuard_GetManyMethod_allDialects`; `unit: cmd/sqlgen/gen: TestLockModeGuard_ConnectionMethod_allDialects` | mysql |  |
| lock.mysql-version-cache | The MySQL client probes `SELECT VERSION()` behind a `sync.Once` version cache; PostgreSQL and SQLite clients carry no version cache | `unit: cmd/sqlgen/gen: TestMySQLVersionCache_emittedOnlyForMySQL` | all |  |
| lock.threaded-into-select | `GetMany` passes the caller's `LockMode` into the `SelectOptions` of its `SELECT` | `unit: cmd/sqlgen/gen: TestLockModeThreading_GetMany` | postgres |  |
| lock.write-reads-lock-none | The chained `Get` / `GetMany` inside `Create`, `Update`, `Upsert`, `SoftDelete*` and `Restore*` forces `LockMode` to `LockNone` alongside `SkipHooks = true` | `unit: cmd/sqlgen/gen: TestChainedGet_LockNoneForced` | postgres |  |
| lock.read-internals-propagate | `Connection`'s inner `GetMany` keeps the caller's `LockMode` rather than resetting it to `LockNone` | `unit: cmd/sqlgen/gen: TestChainedGet_PaginationDoesNotForceLockNone` | all |  |

## §9.7 PK Resolution and Entity Return Path
<!-- prd: 9.7 sha256: 10169126860ffdb4 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| pk-return.batch-getmany | `CreateMany` and `UpdateMany` collect the written keys and return their entities through one `GetMany` on those keys (single PK: `ID In` the keys; composite PK: `PKs`) with no limit | `unit: cmd/sqlgen/gen: TestCreateTemplate_createMany_multiRowInsert`; `unit: cmd/sqlgen/gen: TestUpdateTemplate_updateMany_pgx`; `unit: cmd/sqlgen/gen: TestCreateTemplate_createMany_compositePK`; `unit: cmd/sqlgen/gen: TestUpdateTemplate_compositePK_updateMany` | postgres |  |
| pk-return.upsert-fallback-binds-by-name | On a tenanted table, the `Upsert` conflict fallback `SELECT` binds its scan targets by column name, so a tenant column that sorts before the PK still lands in the tenant target | `unit: cmd/sqlgen/gen: TestUpsertTemplate_conflictRowFallbackBindsByName` | postgres |  |

## §9.8.2 Get
<!-- prd: 9.8.2 sha256: ef0c08f3be70bdff -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| get.delegates-to-getmany | `Get` reads through `GetMany` filtered on the primary key with `Limit` 1, and returns `ErrNotFound` when no row comes back | `unit: cmd/sqlgen/gen: TestGetTemplate_goldenFile_singlePK`; `unit: cmd/sqlgen/gen: TestGetTemplate_goldenFile_compositePK` | all |  |
| get.inner-skip-hooks | `Get`'s internal `GetMany` runs with `SkipHooks = true`, so the hook chain fires once, as `OpGet` | `unit: cmd/sqlgen/gen: TestGetTemplate_goldenFile_singlePK`; `unit: cmd/sqlgen/gen: TestGetTemplate_goldenFile_compositePK` | all |  |

## §9.8.3 GetMany
<!-- prd: 9.8.3 sha256: 85f94c1e4a858d9c -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| get-many.required-columns-union | A non-empty narrowed `FieldOptions` projection in `GetMany` is widened by the columns its internal caller declared | `unit: cmd/sqlgen/gen: TestRequiredColumns_getManyUnionsDeclaredColumns/table` | all |  |
| get-many.nothing-selected-returns-early | `GetMany` with a non-nil `FieldOptions` that selects no column returns without querying | `unit: cmd/sqlgen/gen: TestGenerate_GetManyEmptySelectionReturnsEarly` | all |  |
| get-many.parent-key-for-relationships | When a narrowed `GetMany` loads an O2M or M2M relationship, the parent key is unioned into the projection | `unit: cmd/sqlgen/gen: TestRequiredColumns_getManyUnionsParentKeyForRelationships` | all |  |
| get-many.o2o-target-pk-selected | An O2O join selects the target's primary key even when the target's `FieldOptions` does not, at every level of a chain | `unit: cmd/sqlgen/gen: TestRequiredColumns_o2oJoinCarriesTargetPK`; `unit: cmd/sqlgen/gen: TestRequiredColumns_chainedO2OJoinCarriesTargetPK` | all |  |
| get-many.goroutine-per-edge | `GetMany` loads each selected O2M and M2M relationship in its own `errgroup` goroutine | `unit: cmd/sqlgen/gen: TestGetTemplate_parallelLoading` | all |  |

## §9.8.4 Create
<!-- prd: 9.8.4 sha256: 5d901667c4f1e471 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| create.refetch-skip-hooks | `Create` returns the created entity through a follow-up `Get` with `SkipHooks = true`, so the mutation hooks do not fire again for the fetch | `unit: cmd/sqlgen/gen: TestCreateTemplate_goldenFile_dbStrategyPostgres` | postgres |  |
| create.has-selected-columns-method | Each `<T>FieldOptions` has a generated `HasSelectedColumns() bool` method | `unit: cmd/sqlgen/gen: TestFieldOptionsTemplate_hasSelectedColumnsMethod` | all |  |

## §9.8.5 CreateMany
<!-- prd: 9.8.5 sha256: 8685191a13e9a096 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| create-many.batch-split | `CreateMany` splits its inputs into sub-batches of the table's batch size, one multi-row INSERT per sub-batch | `unit: cmd/sqlgen/gen: TestCreateTemplate_createMany_batchSplitting` | postgres |  |
| create-many.returning-keys | On PostgreSQL, `CreateMany`'s `multiInsertAndResolveIDs` sends the multi-row INSERT with `RETURNING` the PK column and scans one key per returned row; it derives no key and pins no session | `unit: cmd/sqlgen/gen: TestCreateTemplate_multiInsertAndResolveIDs_postgres` | postgres |  |
| create-many.mysql-step-derivation | On MySQL, a multi-row `CreateMany` on a database-generated key derives its keys as `LastInsertId() + i × @@auto_increment_increment`, reading the step on the INSERT's own session: a caller's transaction as it is, otherwise one session pinned through `database.SessionPinner` without opening a transaction | `unit: cmd/sqlgen/gen: TestCreateTemplate_multiInsertAndResolveIDs_mysql`; `example: mysql: TestCreateMany_KeysFollowTheSessionStep`; `example: mysql: TestCreateMany_KeysFromAMixedStepPool`; `example: mysql: TestCreateMany_StepReadOpensNoTransaction`; `example: mysql: TestCreateMany_StaysInTheCallerTransaction` | mysql |  |
| create-many.mysql-no-pinner-refused | On MySQL, a multi-row `CreateMany` through a `Querier` that is neither a transaction nor a `database.SessionPinner` returns an error instead of deriving keys | `unit: cmd/sqlgen/gen: TestCreateTemplate_multiInsertAndResolveIDs_mysql`; `example: mysql: TestCreateMany_QuerierThatCannotPin` | mysql |  |
| create-many.no-writable-column-sqlite | On SQLite, `CreateMany` on a table with no writable column names the key in every row and sends `NULL` for it, which SQLite fills with the next rowid | `unit: cmd/sqlgen/gen: TestCreateTemplate_createMany_noWritableColumn/sqlite` | sqlite |  |
| create-many.sqlite-default-expr | The SQLite `CreateMany` sentinel for an unset omittable column carries the column's DDL default expression, or `NULL` for a column without a default | `unit: cmd/sqlgen/gen: TestCreateInputFieldClassification_defaultExprFallsBackToNULL`; `golden: sqlite` | sqlite |  |

## §9.8.6a UpdateMany
<!-- prd: 9.8.6a sha256: 0f9385aeae2917fb -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| update-many.item-type | On a single-PK table `UpdateMany` takes `[]Update{Table}Item`, each item pairing the PK (field named and typed after the PK column) with an `*Update{Table}Input` | `unit: cmd/sqlgen/gen: TestGenerate_UpdateManyItemNamesThePKAfterItsColumn/postgres text key product_code`; `unit: cmd/sqlgen/gen: TestGenerate_UpdateManyItemNamesThePKAfterItsColumn/postgres uuid key ticket_ref`; `unit: cmd/sqlgen/gen: TestGenerate_UpdateManyItemNamesThePKAfterItsColumn/mysql varchar key product_code`; `unit: cmd/sqlgen/gen: TestGenerate_UpdateManyItemNamesThePKAfterItsColumn/sqlite text key product_code` | all |  |
| update-many.pgx-send-batch | On pgx, `UpdateMany` queues each item's UPDATE into a `pgx.Batch` and sends each sub-batch with `SendBatch` | `unit: cmd/sqlgen/gen: TestUpdateTemplate_updateMany_pgx` | pgx |  |
| update-many.stdlib-sequential | On stdlib, `UpdateMany` runs each sub-batch's UPDATEs in turn inside one transaction: when an item fails, its sub-batch rolls back and earlier sub-batches persist | `example: sqlite: TestUpdateMany_FailingItemRollsBackBatch`; `unit: cmd/sqlgen/gen: TestUpdateTemplate_updateMany_stdlib` | sqlite |  |

## §9.8.6b UpdateWhere
<!-- prd: 9.8.6b sha256: dd8c98e6ecb54b73 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| update-where.returning-keys | On PostgreSQL, `UpdateWhere` learns the affected keys from `UPDATE ... RETURNING` the PK column, with no pre-SELECT, and re-fetches those keys | `unit: cmd/sqlgen/gen: TestUpdateTemplate_updateWhere_postgres` | postgres |  |
| update-where.mysql-preselect | On MySQL, `UpdateWhere` selects the matching keys with `collectAffectedIDs` before the UPDATE and uses no `RETURNING` | `unit: cmd/sqlgen/gen: TestUpdateTemplate_updateWhere_mysql`; `unit: cmd/sqlgen/gen: TestUpdateTemplate_goldenFile_mysql` | mysql |  |

## §9.8.7 Upsert
<!-- prd: 9.8.7 sha256: 0143daa965ade021 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| conflict.enum-type | `Upsert`'s conflict target is a generated named type `{Table}ConflictTarget` whose constants are declared as that type, not a free-form column list | `unit: cmd/sqlgen/gen: TestUpsertTemplate_conflictTargetEnum` | postgres |  |
| upsert.pk-returning | On PostgreSQL and SQLite, `Upsert` resolves the written row's PK from the upsert statement with `RETURNING "id"`, then returns the entity through `Get` | `unit: cmd/sqlgen/gen: TestUpsertTemplate_insertUpsertAndResolveID_postgres`; `unit: cmd/sqlgen/gen: TestUpsertTemplate_sqlite` | postgres, sqlite |  |
| upsert.pk-mysql-last-insert-id | On MySQL, `Upsert` on an `AUTO_INCREMENT` table resolves the PK with `LastInsertId()` and uses no `RETURNING` | `unit: cmd/sqlgen/gen: TestUpsertTemplate_mysql` | mysql |  |
| upsert.update-set-excludes-key | `Upsert`'s update columns are the inserted columns minus the conflict-target columns and every PK column | `unit: cmd/sqlgen/gen: TestUpsertTemplate_callerStrategy`; `unit: cmd/sqlgen/gen: TestUpsertTemplate_compositePK`; `unit: cmd/sqlgen/gen: TestUpsertTemplate_mysql` | postgres, mysql |  |

## §9.8.8 SoftDelete / Restore
<!-- prd: 9.8.8 sha256: f8654f0a37a367fb -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| soft-delete.where-scopes-live-rows | `SoftDeleteWhere` always ANDs the not-deleted condition (`deleted_at IS NULL` for the timestamp strategy) onto the filter's conditions | `unit: cmd/sqlgen/gen: TestDeleteTemplate_softDeleteWhere_postgres`; `unit: cmd/sqlgen/gen: TestDeleteTemplate_goldenFile_singlePK` | postgres |  |
| restore.where-scopes-deleted | `RestoreWhere` always ANDs a deleted-rows condition onto the filter's conditions: `deleted_at IS NOT NULL` for the timestamp strategy, `is_deleted = TRUE` for bool | `unit: cmd/sqlgen/gen: TestDeleteTemplate_restoreWhere_scopesToDeleted`; `unit: cmd/sqlgen/gen: TestDeleteTemplate_restoreWhere_bool_scopesToDeleted`; `unit: cmd/sqlgen/gen: TestFuncSoftDeleteRestoreCondition/timestamp`; `unit: cmd/sqlgen/gen: TestFuncSoftDeleteRestoreCondition/bool` | postgres |  |
| soft-delete.where-returning-keys | On PostgreSQL, `SoftDeleteWhere` and `RestoreWhere` collect the affected IDs from `RETURNING "id"` on the mutation itself, scanning the returned rows | `unit: cmd/sqlgen/gen: TestDeleteTemplate_softDeleteWhere_postgres`; `unit: cmd/sqlgen/gen: TestDeleteTemplate_restoreWhere_idempotent` | postgres |  |
| soft-delete.where-mysql-preselect | On MySQL the delete family uses no `RETURNING`; the `Where` variants learn their affected IDs from a generated `collectAffectedIDs` that selects the PK column | `unit: cmd/sqlgen/gen: TestDeleteTemplate_softDeleteWhere_mysql` | mysql |  |
| soft-delete.refetch-matches-deleted | `SoftDelete`, `SoftDeleteMany` and `SoftDeleteWhere` re-fetch the rows they soft-deleted with the soft-delete column's comparator set to match deleted rows, overriding the default not-deleted condition (timestamp: `&comparator.NullableTime{Null: new(false)}`) | `unit: cmd/sqlgen/gen: TestFuncSoftDeleteFilterOverride`; `unit: cmd/sqlgen/gen: TestDeleteTemplate_softDelete_timestamp`; `unit: cmd/sqlgen/gen: TestDeleteTemplate_softDeleteMany`; `unit: cmd/sqlgen/gen: TestDeleteTemplate_softDeleteWhere_postgres` | postgres |  |

## §9.8.9a Increment
<!-- prd: 9.8.9a sha256: b2496f9df294669f -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| increment.single-pk | On a single-PK table `Increment(ctx, id, IncrementInput[{Table}IncrementColumn], ...)` returns only `error` and issues one `BuildIncrement` UPDATE of `input.Column` by `input.Amount` where the PK equals `id` | `unit: cmd/sqlgen/gen: TestIncrementTemplate_positiveAmount`; `unit: cmd/sqlgen/gen: TestIncrementTemplate_goldenFile` | postgres |  |

## §9.8.10 Exists / Count
<!-- prd: 9.8.10 sha256: f15163bbc65c7889 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| exists.by-pk | `Exists(ctx, pk)` runs `BuildExists` with a `WHERE` matching the primary key | `unit: cmd/sqlgen/gen: TestExistsTemplate_goldenFile` | all |  |
| exists.soft-delete-excluded | On a soft-delete table `Exists` always adds the not-deleted condition, so a soft-deleted row returns `false` | `unit: cmd/sqlgen/gen: TestExistsTemplate_goldenFile` | all |  |
| exists.where-filter | `ExistsWhere(ctx, *{Table}Filter)` builds its conditions with `filter.ToConditions` and runs `BuildExists` | `unit: cmd/sqlgen/gen: TestExistsTemplate_goldenFile` | all |  |
| exists.where-soft-delete-default | On a soft-delete table `ExistsWhere` adds the not-deleted condition unless the filter sets the soft-delete comparator | `unit: cmd/sqlgen/gen: TestExistsTemplate_existsWhereWithFilter`; `unit: cmd/sqlgen/gen: TestExistsTemplate_goldenFile` | all |  |

## §9.8.11 Paginate
<!-- prd: 9.8.11 sha256: d0c935555dfc40a3 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| paginate.hook-chain | `Paginate` runs inside a query hook chain whose `QueryContext.Op` is `hook.OpPaginate` | `unit: cmd/sqlgen/gen: TestPaginationTableTemplate_goldenFile` | postgres |  |

## §9.8.12 Connection
<!-- prd: 9.8.12 sha256: 0d8fbdeb0a18fd07 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| connection.hook-chain | `Connection` runs inside a query hook chain whose `QueryContext.Op` is `hook.OpConnection` | `unit: cmd/sqlgen/gen: TestPaginationTableTemplate_goldenFile` | postgres |  |
| cursor.keyset-quoted | The keyset condition quotes every cursor key through the dialect (`QuoteIdentifier`), and the quoted token is the condition's column | `unit: cmd/sqlgen/gen: TestConnectionTemplate_cursorKeyset` | all |  |
| cursor.decode-typed-key-cases | `decode<T>Cursor` has a case only for each `cursor_keys` column, unmarshals each value into that column's Go type, and returns `ErrInvalidCursor` for a value that binds SQL NULL | `unit: cmd/sqlgen/gen: TestPaginationTableTemplate_decodeCursor`; `unit: cmd/sqlgen/gen: TestPaginationTableTemplate_decodeCursorMultiKeys` | postgres |  |

## §9.8.13 Composite Primary Key Patterns
<!-- prd: 9.8.13 sha256: a48f8e9d5fb61a23 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| composite.update-item-pk | On a composite-PK table the `UpdateMany` item is `Update{Table}Item{PK {Table}PK, Input *Update{Table}Input}`, and the written items' `PK`s re-fetch through the filter's `PKs` | `unit: cmd/sqlgen/gen: TestUpdateTemplate_compositePK_updateMany`; `unit: cmd/sqlgen/gen: TestUpdateTemplate_goldenFile_compositePK` | postgres |  |
| composite.where-returning-pk-columns | On PostgreSQL a composite-PK `UpdateWhere` returns every PK column (`RETURNING "order_id", "product_id"`), scans them into a `{Table}PK`, and re-fetches through the filter's `PKs` | `unit: cmd/sqlgen/gen: TestUpdateTemplate_compositePK_updateWhere` | postgres |  |

## §9.8.13a Create (composite PK)
<!-- prd: 9.8.13a sha256: c8e0fe43c3fa025b -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| composite.create-no-resolve | A composite-PK `Create` generates no `insertAndResolveID`: it `Exec`s the INSERT with every PK column taken from the input and returns `Get` on the `{Table}PK` built from the input | `unit: cmd/sqlgen/gen: TestCreateTemplate_compositePK`; `unit: cmd/sqlgen/gen: TestCreateTemplate_noInsertResolve_compositePK`; `unit: cmd/sqlgen/gen: TestCreateTemplate_goldenFile_compositePK` | postgres |  |

## §9.8.13b CreateMany (composite PK)
<!-- prd: 9.8.13b sha256: 82313e958718dea8 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| composite.create-many-pks-from-input | A composite-PK `CreateMany` generates no `multiInsertAndResolveIDs`: it collects each `{Table}PK` from the inputs, `Exec`s the multi-row INSERT, and re-fetches through the filter's `PKs` | `unit: cmd/sqlgen/gen: TestCreateTemplate_createMany_compositePK`; `unit: cmd/sqlgen/gen: TestCreateTemplate_noInsertResolve_compositePK` | postgres |  |

## §9.8.13d HardDeleteMany (composite PK)
<!-- prd: 9.8.13d sha256: dfe6d3d1b326ec3c -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| soft-delete.composite-batch-condition | On a composite-PK table `SoftDeleteMany` and `RestoreMany` build their WHERE with `BuildCompositePKBatchCondition` over every PK column | `unit: cmd/sqlgen/gen: TestDeleteTemplate_goldenFile_compositePK` | postgres |  |

## §9.8.13f Methods with identical structure
<!-- prd: 9.8.13f sha256: 1bc6d41507a14569 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| upsert.composite-pk-from-input | On a composite-PK table `Upsert` does no ID resolution: it executes the insert and returns the entity through `Get` with the `{Table}PK` built from the input | `unit: cmd/sqlgen/gen: TestUpsertTemplate_compositePK` | postgres |  |
| upsert.composite-conflict-target-pk | On a table whose PRIMARY KEY constraint is composite, the PK conflict target covers every PK column (`OrderItemConflictPK` over `(order_id, product_id)`) | `unit: cmd/sqlgen/gen: TestBuildConflictTargets_OverrideNeedsAnIndex/table-level PK restated in config keeps its target` | all |  |

## §9.9 Nested Mutations
<!-- prd: 9.9 sha256: 70b7ab3b64fb2cb5 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| nested.one-transaction | `CreateWithRelated`, `UpdateWithRelated` and `UpsertWithRelated` each run their writes inside `database.WithTransaction`, under a name that is a bare SQL identifier so the call composes as a `SAVEPOINT` inside a caller's transaction | `unit: cmd/sqlgen/gen: TestNestedTemplate_SavepointNamesAreBareIdentifiers` | all |  |

## §9.9.6 Execution order
<!-- prd: 9.9.6 sha256: 5124ee487b6462d9 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| nested.parent-write-strips-relationships | The parent write inside a nested method runs with every relationship member stripped from its `FieldOptions`, and with its primary key selected | `unit: cmd/sqlgen/gen: TestNestedTemplate_ParentWriteStripsEveryRelationship` | all |  |
| nested.clear-runs-first | On an edge, `clear` is emitted before the edge's `create`, `connect` and `disconnect` statements, on O2M and M2M | `unit: cmd/sqlgen/gen: TestNestedTemplate_ClearRunsFirstAndSupersedesDisconnect` | all |  |
| nested.clear-ignores-disconnect | When `clear` is set, the same edge's `disconnect` list is dropped rather than rejected | `unit: cmd/sqlgen/gen: TestNestedTemplate_ClearRunsFirstAndSupersedesDisconnect` | all |  |
| nested.two-verbs-one-target | A target named under both `connect` and `disconnect` on one edge, or under `disconnect` and a `create` entry carrying an explicit primary key, is refused with `ErrNestedVerbConflict` attributed to `disconnect` and the id, before any statement on that edge | `unit: cmd/sqlgen/gen: TestNestedTemplate_RefusesOneTargetUnderTwoVerbs` | all |  |
| nested.visibility-read-unbounded | Every `connect` visibility read passes `Limit: new(0)` | `unit: cmd/sqlgen/gen: TestNestedTemplate_ConnectVisibilityReadIsUnbounded` | all |  |
| nested.connect-verify-read | The O2M / has-one `connect` adoption counts, per chunk, a verify read issued after that chunk's `UpdateWhere` and filtered on the chunk's ids and `fk = parent` (plus the discriminator on a polymorphic edge); the `UpdateWhere` itself selects nothing, and the shortfall is the summed count against the ids to adopt | `unit: cmd/sqlgen/gen: TestNestedTemplate_AdoptionCountsThroughAVerifyRead` | all |  |
| nested.verify-read-skips-hooks | The adoption's verify read runs with `SkipHooks` set, and with the caller's tenancy options carried through `nestedChildOptions` | `unit: cmd/sqlgen/gen: TestNestedTemplate_AdoptionCountsThroughAVerifyRead` | all |  |
| nested.unlink-sets-null | O2M `disconnect` and `clear` assign the FK an explicit NULL (`omittable.Set[*T](nil)`, or an invalid Null wrapper for a wrapper-bound FK) rather than leaving it out of the `SET` list | `unit: cmd/sqlgen/gen: TestNestedTemplate_UnlinkSetsNullRatherThanOmitting`; `unit: cmd/sqlgen/gen: TestNestedUnlink_NullWrapperFKSetsTheWrapperInvalid` | all |  |
| nested.inner-calls-inherit-skip-hooks | The inner calls of a nested method inherit the caller's `SkipHooks` (with `SkipCache` and `SkipEvents`), and never the caller's `FieldOptions` or `LockMode` | `unit: cmd/sqlgen/gen: TestNestedSharedHelpers_BodiesVaryWithTenancy` | all |  |

## §9.9.7 Query count
<!-- prd: 9.9.7 sha256: 1f5b6ea0047ce786 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| nested.id-list-batched | Every id-list statement of a nested verb is chunked at the client's batch size: the `connect` visibility read, the O2M adoption, the O2M `disconnect` and the M2M junction delete; `clear` is not chunked | `unit: cmd/sqlgen/gen: TestNestedTemplate_BatchesEveryIDListStatement` | all |  |

## §9.9.8 Error attribution
<!-- prd: 9.9.8 sha256: 20eb2bf161cf537c -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| nested.error-carries-edge-verb-id | Nested failures are wrapped in `*NestedMutationError{Edge, Verb, ID}` around the cause | `unit: cmd/sqlgen/gen: TestNestedSharedHelpers_BodiesVaryWithTenancy` | all |  |

## §13.2 Loading Strategy
<!-- prd: 13.2 sha256: 5327d767bcdd32e9 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| load.o2o-alias-shape | Every O2O join alias, at any chain depth, is one lowercase letter optionally followed by a counter from 2, and is unique within the parent's join tree, the parent's own alias included | `unit: cmd/sqlgen/gen: TestO2OJoinAliases_NeverAKeyword` | all |  |
| load.o2o-target-soft-delete | An O2O join to a soft-delete target carries the target's not-deleted condition in its `ON` clause | `unit: cmd/sqlgen/gen: TestRelationshipsTemplate_softDeleteOnRelated`; `unit: sql: TestBuildSelectJoin/join with soft delete timestamp` | postgres |  |
| load.m2m-target-key | The M2M target fetch matches the target's own referenced key column against the junction rows, and maps targets to parents on that key | `unit: cmd/sqlgen/gen: TestGetTemplate_m2mTargetFilterUsesTargetPKField/string key`; `unit: cmd/sqlgen/gen: TestGetTemplate_m2mTargetFilterUsesTargetPKField/integer key` | all |  |
| load.fanout-no-tx-bound | The O2M/M2M loader fan-out sets no concurrency bound of its own, inside a transaction or out | `unit: cmd/sqlgen/gen: TestGetTemplate_relationshipFanOutIsUnbounded` | all |  |

## §14.1 Offset-Based
<!-- prd: 14.1 sha256: 0a475e3a9306e63a -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| paginate.result-shape | `PaginateResult[T]` carries `Items`, `TotalCount`, `Offset`, `Limit` and `HasMore`, and none of the derived fields `TotalPages`, `PageSize`, `CurrentPage` | `unit: cmd/sqlgen/gen: TestPaginationTemplate_resultFields`; `unit: cmd/sqlgen/gen: TestPaginationTemplate_goldenFile`; `unit: cmd/sqlgen/gen: TestPaginationTemplate_paginateResultType` | all |  |
| paginate.json-tags | `PaginateInput` and `PaginateResult` fields carry the §14.1 json tags (`filter`, `sort`, `limit`, `offset`; `items`, `total_count`, `offset`, `limit`, `has_more`) | `unit: cmd/sqlgen/gen: TestPaginationTemplate_paginateInputType`; `unit: cmd/sqlgen/gen: TestPaginationTemplate_goldenFile` | all |  |

## §14.2 Cursor-Based (Relay Pagination)
<!-- prd: 14.2 sha256: 567b1c177cc9491d -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cursor.encode-base64-json | `encode<T>Cursor` builds a JSON object keyed by each cursor key's column name holding the row's field value, and returns it Base64 (StdEncoding) encoded | `unit: cmd/sqlgen/gen: TestPaginationTableTemplate_encodeCursor`; `unit: cmd/sqlgen/gen: TestPaginationTableTemplate_encodeCursorMultiKeys`; `unit: cmd/sqlgen/gen: TestPaginationTableTemplate_encodeCursor_doesNotShadowEntity_MStartingStruct` | postgres |  |
| cursor.invalid-missing-or-null | A cursor missing an expected key, or holding JSON `null` for one, is rejected with `ErrInvalidCursor`; a decoded value that binds SQL NULL (a `driver.Valuer` returning nil or an error) is detected as null | `unit: cmd/sqlgen/gen: TestConnectionTemplate_cursorEncodeDecode`; `unit: cmd/sqlgen/gen: TestConnectionTemplate_goldenFile` | all |  |
| cursor.sort-by-keys | `Connection` orders by the `cursor_keys` columns only, every key ascending for forward pagination and descending for backward | `unit: cmd/sqlgen/gen: TestConnectionTemplate_cursorSorts`; `unit: cmd/sqlgen/gen: TestConnectionTemplate_goldenFile` | all |  |
| connection.first-last-error | `Connection` with both `First` and `Last` set returns `ErrInvalidCursor` | `unit: cmd/sqlgen/gen: TestPaginationTableTemplate_connectionFirstAndLastError` | postgres |  |
| connection.page-info-flags | `HasNextPage` is true when forward pagination fetched more than `first` rows or backward pagination was given `before`; `HasPreviousPage` is true when backward pagination fetched more than `last` rows or forward pagination was given `after` | `unit: cmd/sqlgen/gen: TestPaginationTableTemplate_connectionForward` | postgres |  |
| connection.backward-reversed | Backward pagination reverses the fetched rows, so edges are returned in ascending cursor order | `unit: cmd/sqlgen/gen: TestPaginationTableTemplate_connectionResultReversal`; `unit: cmd/sqlgen/gen: TestPaginationTableTemplate_goldenFile` | postgres |  |
| connection.default-page-size | When neither `first` nor `last` is set, `Connection` fetches the client's configured page size | `unit: cmd/sqlgen/gen: TestPaginationTableTemplate_connectionFirstPage` | postgres |  |
| connection.total-count-on-connection | `TotalCount int64` is a field of `Connection[T]`, not of `PageInfo` | `unit: cmd/sqlgen/gen: TestConnectionTemplate_totalCountAlwaysPopulated` | all |  |
| connection.page-info-cursor-pointers | `PageInfo.StartCursor` and `PageInfo.EndCursor` are `*string` | `unit: cmd/sqlgen/gen: TestConnectionTemplate_pageInfoPointerTypes` | all |  |

## §16.5.1 Dialect Scope
<!-- prd: 16.5.1 sha256: 4231f90c985c79ec -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| matview.dialect.non-postgres-file-error | Under `input.dialect: mysql` or `sqlite` a `CREATE MATERIALIZED VIEW` annotation file is a hard parse error naming the dialect | `unit: parser/mysql: TestParseViewSQL_MaterializedRejected`; `unit: parser/sqlite: TestParseViewSQL_MaterializedRejected` | mysql, sqlite | |

## §16.5.2 Discovery
<!-- prd: 16.5.2 sha256: e974c9d83d0898b2 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| matview.discovery.pk-choice | Of a materialized view's qualifying unique indexes, the one with the fewest columns gives its primary key, ties broken by index name ascending, whatever order the indexes come in | `unit: parser/introspect: TestChooseMatviewPK` | postgres | |
| matview.file.or-replace-error | `CREATE OR REPLACE MATERIALIZED VIEW` in an annotation file is a parse error | `unit: parser/postgres: TestParseViewSQL_OrReplaceMaterializedRejected` | postgres | |
| matview.file.pk-concurrently-refreshable | An annotation-file materialized view with `@pk` has those columns as its non-nullable primary key and is concurrently refreshable; without `@pk` it has no primary key and is not concurrently refreshable | `unit: parser: TestBuildView_Materialized` | postgres | |
| matview.invariant.regular-never-concurrent | A regular (non-materialized) view is never concurrently refreshable, even with `@pk` | `unit: parser: TestBuildView_RegularViewNeverConcurrentlyRefreshable` | all | |
| matview.overlay.pk-replaces-discovered | An overlay `@pk` on an introspected materialized view replaces its discovered primary key (the discovered columns stop being PK) and makes it concurrently refreshable | `unit: parser: TestMergeViewAnnotations_MatviewPKOverride` | postgres | |

## §16.5.3 Refresh Methods
<!-- prd: 16.5.3 sha256: 0d3f9f72fbbaf036 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| matview.refresh.concurrently-tx-guard | With an ambient transaction, `RefreshConcurrently` returns `database.ErrRefreshConcurrentlyInTx` before it enters the query-hook chain or issues any SQL | `unit: cmd/sqlgen/gen: TestViewRefreshTemplate_txGuardPrecedesExecution` | postgres |  |
| matview.refresh.plain-no-tx-guard | Plain `Refresh` carries no transaction guard, so it may run inside a transaction | `unit: cmd/sqlgen/gen: TestViewRefreshTemplate_txGuardPrecedesExecution` | postgres |  |

## §17.3 Generated Behavior
<!-- prd: 17.3 sha256: dce1144bfbcb5b6f -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hard-delete-many.soft-delete | §17.3 adds the default "is not deleted" condition only to `GetMany`, and a hard delete is a physical DELETE (§9.3), so `HardDeleteMany` removes the listed rows whether or not they are soft-deleted | `conformance: HardDeleteMany/deletes listed rows whether or not they are soft-deleted` | all | client-methods: HardDeleteMany × soft-delete |
| soft-delete-read.no-column-no-filter | A table with no soft-delete column gets no automatic not-deleted condition on `GetMany`, `Exists` or `ExistsWhere` | `unit: cmd/sqlgen/gen: TestExcludeDeleted_falseWhenNoSoftDelete`; `unit: cmd/sqlgen/gen: TestGetTemplate_noSoftDelete`; `unit: cmd/sqlgen/gen: TestExistsTemplate_existsWithoutSoftDelete`; `unit: cmd/sqlgen/gen: TestExistsTemplate_existsWhereWithoutSoftDelete` | all |  |

## §17.5 Update Tracking
<!-- prd: 17.5 sha256: 690aa4e3e675ffc8 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| update-tracking.input-omittable | A column listed in `generation.update_columns` is a field of `Update{Table}Input`, wrapped in `omittable.Value[T]` | `unit: cmd/sqlgen/gen: TestUpdateInputFieldClassification_updateColumnsExcluded` | all |  |
| update-tracking.no-column-no-auto-set | A table with no column in `update_columns` gets no `time.Now()` auto-set in its update methods | `unit: cmd/sqlgen/gen: TestUpdateTemplate_noUpdateColumns` | postgres |  |

## §18.1 Context-Based Injection
<!-- prd: 18.1 sha256: c3db00f28ba2ecf7 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hard-delete-many.transaction | Every generated client method calls `database.Conn(ctx, querier)`, so `HardDeleteMany` with a transaction in its context runs on that transaction | `conformance: HardDeleteMany/deletes inside the caller's transaction` | all | client-methods: HardDeleteMany × transaction |

## §18.3 API
<!-- prd: 18.3 sha256: d74d13c5256f0eb9 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hard-delete-many.rollback | `WithTransaction` rolls back when its function returns an error, so a `HardDeleteMany` inside it deletes nothing | `conformance: HardDeleteMany/rolled-back transaction leaves rows` | all | |

## §19.4 Batch Behavior by Driver
<!-- prd: 19.4 sha256: 41bde47074962425 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| restore-many.batch-split | Regardless of driver, all batch operations split their input into sub-batches of `generation.batch_size`, so `RestoreMany` sends one statement per sub-batch | `conformance: RestoreMany/splits the keys into sub-batches of the table's batch size` | all | |

## §20.2 Generated Structure
<!-- prd: 20.2 sha256: f946fd3347f5662e -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| client.new-shared-querier | `New(querier database.Querier, opts ...ClientOption) *Client` constructs every entity client, tables and views, internally from the one querier it was given | `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_newConstructor` | postgres, sqlite |  |
| client.accessor-per-table | The unified client has one accessor per table, named for the plural struct name (`Client.{Tables}()`), whose interface is `{Table}Client` | `unit: cmd/sqlgen/gen: TestBuildClientContext`; `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_accessorsReturnInterfaces` | all |  |
| client.tx-delegation | `Begin`, `Commit`, `Rollback` and `WithTx` delegate to `database.NewTransaction`, `database.Commit`, `database.Rollback` and `database.WithTransaction`, passing the client's querier to `NewTransaction` and `WithTransaction` | `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_transactionMethods` | postgres, sqlite |  |
| client.querier-escape | `Client.Querier(ctx)` returns `database.Conn(ctx, c.querier)`: the active transaction when ctx carries one, otherwise the original querier, with no hook chain | `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_goldenFile` | postgres, sqlite |  |
| client.raw-scan-callback | `Raw(ctx, sql string, args []any, fn func(database.Rows) error) error` scans inside its terminal through `database.QueryFunc` on `database.Conn(ctx, c.querier)` and hands no live result set back; `RawExec(ctx, sql string, args ...any) (database.Result, error)` executes on `database.Conn(ctx, c.querier)` | `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_rawQuery`; `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_rawExec` | postgres, sqlite |  |
| client.raw-global-hooks | `Raw` runs through the client's registered query hooks and `RawExec` through its registered mutation hooks (the global chain), with panic recovery | `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_goldenFile` | postgres, sqlite |  |
| client.ping | `Ping(ctx)` calls the querier's own `Ping(ctx) error` when it has one, and otherwise runs `SELECT 1` | `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_pingWithNativeMethod`; `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_pingFallbackSelect1` | postgres, sqlite |  |
| client.close-querier | `Close()` calls `Close()` on the querier when it implements `io.Closer`, skips it otherwise, and returns the collected errors joined with `errors.Join` | `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_closeWithIOCloser`; `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_closeJoinsMultipleErrors` | postgres, sqlite |  |
| client.close-cache-publisher | `Close()` also calls `Close()` on the configured cache and event publisher when each is set and implements `io.Closer` | `example: cache: TestClientClose_ClosesCacheAndEventPublisher`; `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_closeClosesConfiguredCacheAndPublisher` | sqlite |  |

## §21.2 Hook Types
<!-- prd: 21.2 sha256: 9d8fd1b0bd0ec4e4 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hard-delete-many.hooks | `HardDeleteMany` runs through the mutation hook chain as `OpHardDeleteMany`; the `MutationContext` carries the table constant and a nil `PK` (a batch), and after the mutation `AffectedPKs` holds the affected keys | `conformance: HardDeleteMany/fires one hard_delete_many mutation hook` | all | client-methods: HardDeleteMany × hooks |

## §21.5 Hook Lifecycle
<!-- prd: 21.5 sha256: e0d73e7468d1e59c -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hard-delete-many.hook-abort | A hook can abort: it returns an error without calling `next()`, and the delete does not run | `conformance: HardDeleteMany/a hook that aborts deletes nothing` | all | |

## §22.2 Constraint Errors
<!-- prd: 22.2 sha256: 810f4c6c65c97c1e -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hard-delete-many.error-mapping | A constraint violation is wrapped into a structured `ConstraintError` that unwraps to `ErrConstraintViolation`: deleting a row another row references gives `Type` `ConstraintForeignKey` | `conformance: HardDeleteMany/deleting a referenced row returns a foreign key ConstraintError` | all | client-methods: HardDeleteMany × error-mapping |

## §25.1 Query Count Guarantee
<!-- prd: 25.1 sha256: 56257757d02b513e -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| load.chunk-size | Each relationship loader step binds parent keys in chunks of `floor(L / 2 / N)` keys, `L` being 32766 on SQLite and 65535 on MySQL and PostgreSQL: 16383 and 32767 for a single-column key; on PostgreSQL with pgx a one-column O2M FK filter binds every parent key as one array parameter instead | `unit: cmd/sqlgen/gen: TestFuncLoadChunkSize`; `unit: cmd/sqlgen/gen: TestGetTemplate_o2mRelationshipLoading`; `unit: cmd/sqlgen/gen: TestGetTemplate_m2mRelationshipLoading` | all |  |

## §25.3 Batch Operations
<!-- prd: 25.3 sha256: f6b1af10841cf723 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hard-delete-many.batch-split | `HardDeleteMany` is bounded by `generation.batch_size`: each sub-batch is one write statement, and M keys take `ceil(M / batch_size)` statements | `conformance: HardDeleteMany/splits the keys into sub-batches of the table's batch size` | all | |

## §29.4.2 Mutations
<!-- prd: 29.4.2 sha256: 4c499242556ed130 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| tenancy.upsert-auto-set | On a tenanted table `Upsert` writes the tenant column into its `INSERT` from the resolver, and a caller-set tenant that differs from the resolver returns `tenancy.ErrMismatch` | `unit: cmd/sqlgen/gen: TestTenancyTemplate_upsert_autoSetAcrossDialects` | all |  |
| tenancy.upsert-update-set-excludes-tenant | On a tenanted table the `Upsert` conflict clause's update set excludes the tenant column (with the PK columns); a shared table's excludes the PK columns only | `unit: cmd/sqlgen/gen: TestTenancyTemplate_upsert_tenantExcludedFromUpdateSet`; `unit: cmd/sqlgen/gen: TestTenancyTemplate_upsert_sharedTableOmitsTenancy` | all |  |

## §29.4.2a Nested mutations
<!-- prd: 29.4.2a sha256: 64bac4a1a48d5d35 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| tenancy.nested-skip-tenancy-propagates | The `CallOptions` each nested inner call receives copy the caller's `SkipTenancy` | `unit: cmd/sqlgen/gen: TestNestedSharedHelpers_BodiesVaryWithTenancy` | all |  |

## §29.4.3 Raw operations
<!-- prd: 29.4.3 sha256: f5ff7daf5d295d6b -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| tenancy.raw-bypass-comment | On a tenancy-enabled client, `Raw` and `RawExec` each carry the `// sqlgen: raw query bypasses tenancy` comment | `unit: cmd/sqlgen/gen: TestTenancyTemplate_clientRawHasBypassComment`; `golden: tenancy_mysql` | all |  |

## §29.4.4 CallOptions.SkipTenancy and CallOptions.Tenant
<!-- prd: 29.4.4 sha256: 9fd1aa9d0dfd2d7c -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| tenancy.upsert-skip-tenancy-caller-tenant | Under `SkipTenancy`, `Upsert` does not auto-set the tenant from the resolver and inserts the caller's tenant field when it is set | `unit: cmd/sqlgen/gen: TestTenancyTemplate_upsert_skipTenancyUsesExplicitTenant` | postgres |  |

## §29.10 Relationship Propagation
<!-- prd: 29.10 sha256: 46e732fe5b40a130 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| tenant-rel.skip-tenancy-propagates | A parent read's `SkipTenancy` is forwarded to the child `GetMany` that loads its O2M relationship | `unit: cmd/sqlgen/gen: TestTenancyTemplate_loadRelationships_propagatesSkipTenancy` | all |  |

## Not applicable

| Matrix cell | Reason | Accepted |
|---|---|---|
| client-methods: HardDeleteMany × field-selection | §12 field selection chooses the columns of a returned entity; `HardDeleteMany` returns only an error (§9.3) | |
| client-methods: HardDeleteMany × lock-mode | §9.6a `LockMode` applies to read methods only and is ignored on `*Delete*` methods | |

## PRD gaps

Behavior the generated client has, pinned by tests, that no PRD text states. Each gap waits for
the user's ruling: amend the PRD, which turns the gap into requirement rows, or drop the pins.

| Gap | Behavior | Tests | Section | Ruling |
|---|---|---|---|---|
| gap.get-many.field-options-not-mutated | No generated read path writes to a `FieldOptions` the caller passed in. The O2M and M2M loaders forward the relationship's `FieldOptions` as given and declare the key they need through the inner input's `requiredColumns`, instead of setting `relFieldOptions.ID = true` / `relFieldOptions.<FK> = true`. Two edges can share one `*FieldOptions` and the loaders run concurrently, so an in-place write would be a data race as well as visible to the caller. §9.6 states what is unioned into the projection but not that the caller's struct is left untouched. | `unit: cmd/sqlgen/gen: TestRequiredColumns_noFieldOptionsMutation` | §9.6 |  |
| gap.update.strict-doc-comment | The generated `Update` / `Increment` doc comment says `Returns ErrNotFound if the primary key does not exist.` only when `strict_updates` is true and omits the sentence otherwise; the PRD shows one unconditional wording, `(when strict_updates is true)` | `unit: cmd/sqlgen/gen: TestClientTemplate_strictUpdatesDocComments` | §8.2, §9.5 |  |
| gap.update-many.pgx-sequential-fallback | On pgx, when the resolved querier does not implement `SendBatch`, `UpdateMany` falls back to executing the queued statements one by one with `Exec`; the PRD states only the `SendBatch` path | `unit: cmd/sqlgen/gen: TestUpdateTemplate_updateMany_pgx` | §9.8.6a, §19.4 |  |
| gap.restore.where-integer-eq-one | On an `integer` soft-delete column `RestoreWhere` scopes to `deleted = 1`, while SoftDelete's re-fetch (`Neq 0`) and the read path's not-deleted condition (`= 0`) treat any non-zero value as deleted, so a row with `deleted = 2` is hidden from reads but not restorable by `RestoreWhere` | `unit: cmd/sqlgen/gen: TestDeleteTemplate_restoreWhere_integer_scopesToDeleted`; `unit: cmd/sqlgen/gen: TestFuncSoftDeleteRestoreCondition/integer` | §9.8.8, §17.3 |  |
| gap.conflict.duplicate-unique-one-target | Two UNIQUE constraints over the same column set (MySQL `email`, `email_2`) yield one conflict-target constant (`UserConflictEmail`), not one per constraint | `unit: cmd/sqlgen/gen: TestConflictTargetDuplicateUnique` | §9.5 |  |
| gap.call-options.json-tags-caller-field | `CallOptions` fields carry json tags (`skip_cache`, `skip_events`, `skip_hooks`, `skip_tenancy`, `tenant`, `field_options`, `lock_mode`, `allow_in_transaction`) and the struct ends with an unexported `caller callerOptions` field (the outermost call's hook toggles) that the §9.6 listing, stated as the struct's full shape, does not show | `unit: cmd/sqlgen/gen: TestBuildSharedTypesContext_callOptionsFields`; `unit: cmd/sqlgen/gen: TestBuildSharedTypesContext_callOptionsFieldsTenancyEnabled` | §9.6 |  |
| gap.client.view-accessor-singular | A view's unified-client accessor uses the view's struct name unpluralized (`ProductSummary()`), while table accessors are plural; the PRD says only 'one per view as well' | `unit: cmd/sqlgen/gen: TestBuildClientContext` | §20.2 |  |
| gap.client.tx-callback-mode-default | `Begin` and `WithTx` fill the client's `WithCallbackMode` value into the transaction options when the caller passes no `TxOptions` or one with an empty `CallbackMode` (`applyTxDefaults`); the §20.2 delegation table shows `opts...` passed through unchanged | `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_transactionMethods` | §20.2, §18.6 |  |
| gap.tenant-rel.shared-parent-resolve-gate | On an untenanted parent whose relationship filters reach a tenanted target, the client resolves a tenant for the filter only when the call's filter sets a relationship member (at the top level or inside `and` / `or`); a filter with no relationship member resolves nothing, so a shared table stays readable before a tenant is selected | `unit: cmd/sqlgen/gen: TestRelationshipFilters_SharedParentGatesTenantResolve` | §11.1, §29.10 |  |
| gap.tenancy.view-predicate-order | A tenanted view's GetMany appends the tenant predicate after the filter conditions and before the keyset conditions (`filter < tenant < keyset`); §29.8 says the AND order is fixed at codegen but is not part of the contract, so the pin is on an order the PRD disclaims | `unit: cmd/sqlgen/gen: TestViewTenancy_predicateOrderIsStable` | §29.2.5, §29.8 |  |
| gap.nested.explicit-tenant-propagates | In a package with a uniform tenant type, each nested inner call's `CallOptions` also copy the caller's explicit `Tenant`; §29.10 states only that nested children inherit the ctx-cached tenant | `unit: cmd/sqlgen/gen: TestNestedSharedHelpers_BodiesVaryWithTenancy/tenancy on with a uniform tenant type — the explicit tenant crosses too` | §29.4.2a, §29.10 | |
| gap.batch.partial-failure-side-effects | When a later sub-batch of a stdlib `UpdateMany` (or any `*Many`) fails, the earlier sub-batches stay committed but the call returns only the error: their rows get no cache invalidation and no events. §19.4 says earlier batches persist; no text says what their side effects do |  | §19.4, §27.7, §28.9 | |
