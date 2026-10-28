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

## §9.3 Delete Operations
<!-- prd: 9.3 sha256: 9a222a59e2330f57 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hard-delete-many.happy-path | `HardDeleteMany(ctx, []pk)` is a batch hard delete: a physical DELETE of the rows whose keys are listed | `conformance: HardDeleteMany/deletes only the listed keys` | all | client-methods: HardDeleteMany × happy-path |

## §9.4c Nil input handling
<!-- prd: 9.4c sha256: f8d2c84924befe60 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hard-delete-many.nil-empty-input | Bulk write variants distinguish the two nils: a nil slice is simply no rows, since nil and empty already mean the same thing, so `HardDeleteMany` with no keys does nothing | `conformance: HardDeleteMany/nil key list deletes nothing`; `conformance: HardDeleteMany/empty key list deletes nothing` | all | client-methods: HardDeleteMany × nil-empty-input |
| nil-input.batch-no-statement | `CreateMany`, `UpdateMany`, `UpsertMany`, `SoftDeleteMany`, `RestoreMany` and `HardDeleteMany` send no statement for a nil or empty slice and read nothing back | `example: postgres: TestEmptyBatch_HardDeleteManyUnaffected` | all | |

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
| soft-delete-many.batch-split | `SoftDeleteMany` splits its input into sub-batches of `generation.batch_size`, one statement each | `conformance: SoftDeleteMany/splits the keys into sub-batches of the table's batch size` | all | |
| soft-delete.missing-key | `SoftDelete` on a key that does not exist returns `nil` (idempotent) | `example: sqlite: TestSoftDeleteAndRestore_MissingKey_ReturnNilNil_SQLite/SoftDelete` | all | |
| restore.missing-key | `Restore` on a key that does not exist returns `nil` (idempotent): restoring a non-existent row is a no-op | `example: sqlite: TestSoftDeleteAndRestore_MissingKey_ReturnNilNil_SQLite/Restore` | all | |

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

## §17.3 Generated Behavior
<!-- prd: 17.3 sha256: dce1144bfbcb5b6f -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hard-delete-many.soft-delete | §17.3 adds the default "is not deleted" condition only to `GetMany`, and a hard delete is a physical DELETE (§9.3), so `HardDeleteMany` removes the listed rows whether or not they are soft-deleted | `conformance: HardDeleteMany/deletes listed rows whether or not they are soft-deleted` | all | client-methods: HardDeleteMany × soft-delete |

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

## §25.3 Batch Operations
<!-- prd: 25.3 sha256: f6b1af10841cf723 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| hard-delete-many.batch-split | `HardDeleteMany` is bounded by `generation.batch_size`: each sub-batch is one write statement, and M keys take `ceil(M / batch_size)` statements | `conformance: HardDeleteMany/splits the keys into sub-batches of the table's batch size` | all | |

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
