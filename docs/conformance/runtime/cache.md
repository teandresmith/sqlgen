# Runtime Requirements: Caching

<!-- seeding: in-progress -->
<!-- packages: cache cache/memory cache/redis cache/msgpack metrics/otel -->
<!-- matrix: cache-keys rows=key-shape cols=single-pk,composite-pk,schema,no-schema,tenanted -->

Caching (§27), its tenant key grammar (§29.5), and the OpenTelemetry metrics recorder (§27.13). See
[README.md](../README.md) for the format.

## §27 Caching
<!-- prd: 27 sha256: dfe94499db41cc8f -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.off-generates-nothing | When caching resolves off for every table and view (global `cache.enabled` false or absent, and no per-table or per-view opt-in), no caching code is generated | `unit: cmd/sqlgen/gen: TestBuildCacheContext_disabled`; `unit: cmd/sqlgen/gen: TestBuildCacheContext_explicitlyDisabled`; `unit: cmd/sqlgen/gen: TestBuildCacheContext_NoOptInUnderGlobalOff` | all |  |

## §27.3 Cache Interface
<!-- prd: 27.3 sha256: 67630a847c031706 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.facade.close-probes-closer | The generated `Cache.Close()` closes the backend only when the backend implements `io.Closer` | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsCloseMethod` | all |  |

## §27.4 Injection via Hooks
<!-- prd: 27.4 sha256: a8a4c83955bdc5a2 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.facade.hooks | The generated `Cache` exposes `QueryHook() hook.QueryHook` and `MutationHook() hook.MutationHook`, the hooks it injects into the client | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsQueryAndMutationHooks` | all |  |
| cache.facade.options | The generated package declares the runtime options `WithInvalidationSource`, `WithMetricsRecorder`, `WithSerializer`, `WithCircuitBreaker` and `WithOnError`, each returning `CacheOption` with the parameter type the PRD gives | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsOptionsAPI` | all |  |

## §27.5 Cache Key Generation
<!-- prd: 27.5 sha256: d7434d8dfdde6884 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache-key.helper-value-typed | Each cached table's `keyFor{Table}` helper takes its PK as a concrete value type: the scalar PK type for a single-column PK, the `XXXPK` struct value for a composite PK | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsSinglePKKeyHelper`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsCompositePKKeyHelper` | all |  |
| cache-key.mysql-no-schema | A MySQL table's cache key carries no schema segment, even when the table context carries a schema | `unit: cmd/sqlgen/gen: TestBuildCacheContext_mysqlSchemaIsEmpty` | mysql |  |
| fingerprint.codegen-constant | Each cached table's schema fingerprint is emitted as a codegen-time constant in the generated cache file | `unit: cmd/sqlgen/gen: TestCacheTemplate_fingerprintConstantCarriesTheTableFingerprint` | all |  |
| fingerprint.column-names | Renaming a column or adding one changes the table's fingerprint | `unit: cmd/sqlgen/gen: TestFingerprint_columnRename`; `unit: cmd/sqlgen/gen: TestFingerprint_columnAdd` | all |  |
| fingerprint.column-order | Columns enter the fingerprint in declaration order: swapping two columns changes the table's fingerprint | `unit: cmd/sqlgen/gen: TestFingerprint_columnDeclarationOrderChanges` | all |  |
| fingerprint.go-types | Changing a column's Go type changes the table's fingerprint | `unit: cmd/sqlgen/gen: TestFingerprint_goTypeChange` | all |  |
| fingerprint.pk-order | Reordering the primary-key columns changes the table's fingerprint | `unit: cmd/sqlgen/gen: TestFingerprint_pkOrderChange` | all |  |
| fingerprint.serializer | Switching `cache.serializer` from `json` to `msgpack` changes the table's fingerprint | `unit: cmd/sqlgen/gen: TestFingerprint_serializerSwitch` | all |  |
| cache.invalidate-table.pattern | `InvalidateTable` invalidates through `cache.BuildTablePattern(prefix, schema, name)` built from the table's schema and bare SQL name, not from the schema-qualified `hook.TableName` value | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsInvalidateTablePattern` | postgres |  |
| cache.invalidate.mutations-by-key | No mutation invalidates through a table pattern: in a package with no tenanted table, `BuildTablePattern` is called only by `InvalidateTable` | `unit: cmd/sqlgen/gen: TestCacheTemplate_whereOpsShareKeyBasedArm` | all |  |

## §27.6 Cache Invariant
<!-- prd: 27.6 sha256: ae8265d73e0a67e9 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.relationship-bypass | A `Get` with any relationship flag set on `FieldOptions` bypasses the cache: the query hook checks for it before dispatching to read-through | `unit: cmd/sqlgen/gen: TestCacheTemplate_queryHookPreservesBypassGates`; `example: cache: TestSkip_RelationshipBypass` | all |  |
| cache.create.field-options-not-cached | A `Create` or `CreateMany` call that carries `FieldOptions` writes nothing to the cache | `unit: cmd/sqlgen/gen: TestCacheTemplate_createCachingSkipsWhenFieldOptionsSet` | all |  |

## §27.7 Cache Behavior by Operation
<!-- prd: 27.7 sha256: 98d0918d8dbb9798 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.read-through.get-only | Only `Get` reads through the cache; every other query op (`GetMany`, `Exists`, ...) passes straight to the database | `unit: cmd/sqlgen/gen: TestCacheTemplate_queryHookPreservesBypassGates` | all |  |
| cache.read-through.dispatch | A full `Get` on any cached table, single- or composite-PK, is routed to that table's read-through path, which reads the entity from the cache and on a miss queries the database and sets the cache with the table's TTL | `unit: cmd/sqlgen/gen: TestCacheTemplate_queryHookDispatchesToReadThrough`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsReadThroughHelper`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsReadThroughForCompositePK` | all |  |
| cache.read-through.singleflight-recheck | Concurrent read-through misses on one key collapse through singleflight, and the flight callback re-checks the cache before querying the database | `unit: cmd/sqlgen/gen: TestCacheTemplate_readThroughInFlightRecheck`; `unit: cmd/sqlgen/gen: TestCacheTemplate_readThroughInFlightRecheckCompositePK`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsReadThroughHelper` | all |  |
| cache.partial.hydration-off-bypass | With hydration off, a partial `Get` (column flags only) passes through to the database with no cache read or write | `unit: cmd/sqlgen/gen: TestCacheTemplate_queryHookBypassesPartialWhenHydrationOff` | all |  |
| cache.partial.hydration-on | With hydration on, a partial `Get` that misses returns the partial database result and starts background hydration; the partial result never reaches the cache-set path | `unit: cmd/sqlgen/gen: TestCacheTemplate_readThroughPartialTriggersHydrate` | all |  |
| cache.hydration.one-per-key | At most one hydration runs per `(table, pk)` at a time | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsHydrateHelper`; `example: cache: TestHydration_Dedup` | all |  |
| cache.create.write-through | `Create` and `CreateMany` write each returned entity to the cache under its own key | `unit: cmd/sqlgen/gen: TestCacheTemplate_dispatchMutationSetsOnCreate`; `unit: cmd/sqlgen/gen: TestCacheTemplate_dispatchMutationSetsOnCreateMany`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsSetHelperSinglePK`; `unit: cmd/sqlgen/gen: TestCacheTemplate_mutationHookForwardsResult` | all |  |
| cache.invalidate.many-and-where-by-key | `UpsertMany`, `UpdateMany`, `SoftDeleteMany`, `HardDeleteMany`, `RestoreMany` and the four `*Where` mutations share one key-based invalidation arm | `unit: cmd/sqlgen/gen: TestCacheTemplate_whereOpsShareKeyBasedArm` | all |  |
| cache.invalidate.uncached-table-guard | The mutation invalidation arm returns before any backend call for a table that is not cached | `unit: cmd/sqlgen/gen: TestCacheTemplate_whereOpsShareKeyBasedArm` | all |  |
| cache.skip-cache.read | A `Get` with `CallOptions.SkipCache: true` does not read through the cache | `unit: cmd/sqlgen/gen: TestCacheTemplate_queryHookPreservesBypassGates` | all |  |

## §27.8 Hydration
<!-- prd: 27.8 sha256: 1940f10057dfe00e -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.hydration.full-refetch | Hydration re-queries the entity as `Get` with every column flag set on `FieldOptions` and hooks skipped | `unit: cmd/sqlgen/gen: TestCacheTemplate_hydrateRerunsWithFullFieldOptions`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsFullParentFieldOptions` | all |  |
| cache.hydration.stores-full | Hydration stores the fully fetched entity in the cache with the table's TTL | `unit: cmd/sqlgen/gen: TestCacheTemplate_hydrateSetsCacheOnSuccess` | all |  |
| cache.hydration.timeout | The hydration goroutine runs on a background context bounded by `hydration.timeout` | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsHydrateHelper` | all |  |

## §27.9 Invalidation Strategies
<!-- prd: 27.9 sha256: 9dca090bd7d17f12 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.invalidate.unknown-table-error | `InvalidateMany` builds keys with the named table's own key helper, and returns `cache: unknown or non-cached table %q` for a table that is not cached | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsInvalidateDispatch` | all |  |
| cache.breaker.open-skips | While the circuit breaker is open, read-through skips the cache read and the cache write, returning the database result | `unit: cmd/sqlgen/gen: TestCacheTemplate_readThroughBreakerGating`; `example: cache: TestBreaker_TripsAndRecovers` | all |  |
| cache.error-policy.routed | A cache backend `get` or `set` error is routed through the error policy (metrics, `OnErrorFunc`, breaker) with its op label | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsReadThroughHelper`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsSetHelperSinglePK`; `unit: cmd/sqlgen/gen: TestCacheTemplate_hydrateSetsCacheOnSuccess` | all |  |

## §27.11 View Cache Invalidation
<!-- prd: 27.11 sha256: fed6e214001c8167 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.view-invalidate.keyed-by-table-name | The source-table → view-pattern map is keyed by each source table's `hook.TableName` value, so a bare or schema-qualified `invalidate_on` entry resolves to the same source | `unit: cmd/sqlgen/gen: TestBuildCacheContext_viewInvalidateMap/postgres bare spelling resolves to the qualified value`; `unit: cmd/sqlgen/gen: TestBuildCacheContext_viewInvalidateMap/postgres qualified spelling resolves to the same value`; `unit: cmd/sqlgen/gen: TestBuildCacheContext_viewInvalidateMap/mysql has no schema, so the value is the bare name`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsViewInvalidateSwitch` | postgres, mysql |  |
| cache.matview.refresh-invalidates | A cached materialized view's `Refresh` / `RefreshConcurrently` invalidates the view's own cache entries after the refresh succeeds, deferred to commit inside a transaction | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsRefreshInvalidation` | postgres |  |
| cache.matview.refresh-uncached-noop | With no cached view, a materialized-view refresh performs no cache operation | `unit: cmd/sqlgen/gen: TestCacheTemplate_refreshNoCachedViewsIsNoOp` | postgres |  |

## §27.13 Cache Metrics
<!-- prd: 27.13 sha256: ffd50e67fa6a325f -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache-key.postgres-default-public | A PostgreSQL table with no schema is keyed and labeled under schema `public` | `unit: cmd/sqlgen/gen: TestBuildCacheContext_postgresEmptySchemaNormalized` | postgres |  |
| cache.metrics.read-write | Read-through reports `Hit` or `Miss` and `GetLatency`; every cache write reports `Set` and `SetLatency`, labeled with the table's schema and `TableName` constant | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsReadThroughHelper`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsSetHelperSinglePK` | all |  |
| cache.metrics.hydration | Hydration reports `HydrationStart` when launched and `HydrationComplete` with the query error, the set error, or nil on success | `unit: cmd/sqlgen/gen: TestCacheTemplate_hydrateReportsHydrationComplete` | all |  |

## §29.5 Cache Key Grammar Extension
<!-- prd: 29.5 sha256: 343278b8870f1d6b -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| fingerprint.tenanted-marker | Making a table tenanted changes its fingerprint (the tenanted fact is a fingerprint input) | `unit: cmd/sqlgen/gen: TestFingerprint_tenancyStructuralMarker_changesFingerprint` | all |  |
| cache-key.tenant-per-row-invalidation | A tenanted table's mutation invalidation, `*Where` included, evicts per row from the captured tenants rather than by pattern | `unit: cmd/sqlgen/gen: TestCacheTemplate_tenantedWhereOpsStayPerRow`; `example: tenancy: TestNilTenant_WhereAndManyEvictPrecisely` | all |  |
| cache-key.tenant-fallback-table-pattern | When a captured tenant is missing, invalidation falls back to the cross-tenant `BuildTablePattern` clear built from the table's schema and bare name | `unit: cmd/sqlgen/gen: TestCacheTemplate_tenantedWhereOpsStayPerRow`; `example: tenancy: TestNilTenant_ForcedCaptureGapFallsBackSafely` | all |  |
| cache-key.tenant-capture-update-returning | On PostgreSQL and SQLite, a tenanted table's (tenant outside the PK) `Update`, `UpdateMany` and `UpdateWhere` capture each affected row's tenant by widening the statement's `RETURNING` projection into `MutationContext.AffectedTenants`, with no pre-read | `unit: cmd/sqlgen/gen: TestTenantCapture_update_returningDialects` | postgres, sqlite |  |
| cache-key.tenant-capture-delete-increment-upsert-returning | On PostgreSQL, a tenanted table's (tenant outside the PK) `HardDelete` family, `Increment` and `Upsert` capture the affected row's tenant by widening the statement's `RETURNING` projection (the tenant column, or the PK and the tenant) into `MutationContext.AffectedTenants` | `unit: cmd/sqlgen/gen: TestTenantCapture_delete_acrossDialects/postgres returning`; `unit: cmd/sqlgen/gen: TestTenantCapture_increment_acrossDialects/postgres returning`; `unit: cmd/sqlgen/gen: TestTenantCapture_upsert_capturesRowTenant/postgres returning` | postgres |  |
| cache-key.tenant-capture-mysql-select | On MySQL, tenanted mutations (tenant outside the PK) use no `RETURNING`: `Update`, `Increment` and `HardDelete` read the affected rows' tenants with a `SELECT` over the statement's own conditions, `UpdateWhere` and `HardDeleteWhere` widen `collectAffectedIDs` to project the PK and the tenant column, and `Upsert` reads the written row's tenant by PK after the statement | `unit: cmd/sqlgen/gen: TestTenantCapture_update_mysqlPreRead`; `unit: cmd/sqlgen/gen: TestTenantCapture_delete_acrossDialects/mysql pre-read`; `unit: cmd/sqlgen/gen: TestTenantCapture_increment_acrossDialects/mysql pre-read`; `unit: cmd/sqlgen/gen: TestTenantCapture_upsert_capturesRowTenant/mysql post-read`; `unit: cmd/sqlgen/gen: TestTenantCapture_client_preReadHelperEmission/mysql tenanted emits helper` | mysql |  |
| cache-key.tenant-capture-mysql-many-per-sub-batch | On MySQL, a tenanted `UpdateMany` captures the affected rows' tenants with one pre-read per sub-batch, inside the batch loop, and a key repeated in a later sub-batch keeps the tenant its first pre-read captured | `unit: cmd/sqlgen/gen: TestTenantCapture_updateMany_mysqlPreReadPerSubBatch`; `unit: cmd/sqlgen/gen: TestTenantCapture_update_mysqlPreRead`; `example: tenancy_mysql: TestMySQLCapture_UpdateManyRepeatedKeyKeepsPreMutationTenant` | mysql |  |
| cache-key.tenant-capture-create | `Create` and `CreateMany` on a tenanted table (tenant outside the PK) record the tenant value they wrote (the resolved tenant, or the caller's value when tenancy is skipped) into `MutationContext.AffectedTenants`, one element per created row | `unit: cmd/sqlgen/gen: TestTenantCapture_create_capturesInsertedValue` | all |  |
| cache-key.tenant-capture-composite-pk | On a composite-PK table whose tenant column is outside the PK, the capture projection carries every PK column and the tenant: PostgreSQL's `UpdateMany` widens `RETURNING` to the PK columns and the tenant, and MySQL's `UpdateWhere` widens `collectAffectedPKs` to return the tenants | `unit: cmd/sqlgen/gen: TestTenantCapture_compositeTenantOutsidePK` | postgres, mysql |  |
| cache-key.tenant-in-pk-no-capture | On a table whose tenant column is part of the primary key, no mutation runs an extra tenant capture: `Create`, `Update`, the delete family and the client emit no `AffectedTenants`, `captureAffectedTenants` or `capturedTenant` | `unit: cmd/sqlgen/gen: TestTenantCapture_tenantInPK_noCarrier` | postgres |  |

## PRD gaps

Behavior the code has, pinned by tests, that no PRD text states. Each gap waits for
the user's ruling: amend the PRD, which turns the gap into requirement rows, or drop the pins.

| Gap | Behavior | Tests | Section | Ruling |
|---|---|---|---|---|
| gap.fingerprint.width | The fingerprint is deterministic and is the first 4 bytes of the sha256 as 8 hex characters; §27.5 states sha256 and shows `v1a2b3c`-style examples but no width | `unit: cmd/sqlgen/gen: TestFingerprint_deterministic` | §27.5 |  |
| gap.cache.view-invalidate-bare-every-schema | On PostgreSQL a bare `invalidate_on` entry names the table in every schema that has one, keying the map under each qualified value; §27.11 says only that bare and qualified spellings name the same source | `unit: cmd/sqlgen/gen: TestBuildCacheContext_viewInvalidateMap/postgres bare spelling names the table in every schema` | §27.11 |  |
