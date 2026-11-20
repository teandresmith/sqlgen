# Runtime Requirements: Caching

<!-- seeding: complete -->
<!-- packages: cache cache/memory cache/redis cache/msgpack metrics/otel -->
<!-- matrix: cache-keys rows=key-shape:entity,table-pattern,tenant-table-pattern cols=single-pk,composite-pk,schema,no-schema,tenanted -->

Caching (§27), its tenant key grammar (§29.5), and the OpenTelemetry metrics recorder (§27.13). See
[README.md](../README.md) for the format.

A row here states the behavior of the runtime cache packages (`cache`, the `cache/memory`,
`cache/redis` and `cache/msgpack` backends, and `metrics/otel`) or of the generated cache facade.
The facade's rows cite `cmd/sqlgen/gen` template tests and the `cache`, `tenancy` and
`tenancy_mysql` examples; a cache variant suite is a follow-on (CONFORMANCE.md §8). The `cache`
config block (§27.2) and the serializer choice are in
[cli/config-features.md](../cli/config-features.md), the per-method cache options (`no-cache.*`,
`lock.*`, `stream.*`) are in [client.md](../client.md), and the hook chain the cache hooks run
through is in [runtime/hooks.md](hooks.md).

The `cache-keys` matrix crosses the three key shapes the PRD defines (the entity key of §27.5 and
§29.5, `BuildTablePattern` and `BuildTenantTablePattern`) with the table shapes that change them.

## §27 Caching
<!-- prd: 27 sha256: fc68a7cb5ed5bfd1 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.off-generates-nothing | When caching resolves off for every table and view (global `cache.enabled` false or absent, and no per-table or per-view opt-in), no caching code is generated | `unit: cmd/sqlgen/gen: TestBuildCacheContext_disabled`; `unit: cmd/sqlgen/gen: TestBuildCacheContext_explicitlyDisabled`; `unit: cmd/sqlgen/gen: TestBuildCacheContext_NoOptInUnderGlobalOff` | all |  |

## §27.3 Cache Interface
<!-- prd: 27.3 sha256: 0f5d5eded788b441 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.facade.close-probes-closer | The generated `Cache.Close()` closes the backend only when the backend implements `io.Closer` | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsCloseMethod`; `example: cache: TestClientClose_ClosesCacheAndEventPublisher` | all |  |
| cache.backend.method-set | `Backend` has exactly the five methods `Get(ctx, key string) ([]byte, error)`, `Set(ctx, key string, value []byte, ttl time.Duration) error`, `Invalidate(ctx, key string) error`, `InvalidateMany(ctx, keys []string) error` and `InvalidatePattern(ctx, pattern string) error` | `unit: cache: TestBackendMethodSet` | all |  |
| cache.stats-reporter.shape | `StatsReporter` is `Stats() Stats`, and `Stats` has the `uint64` fields `Hits`, `Misses`, `Sets`, `Invalidations`, `Evictions` and `Entries`, in that order | `unit: cache: TestStatsReporterAndStatsFields` | all |  |
| cache.backend.set-get | On each shipped backend, `Get` returns the bytes `Set` stored under the key | `unit: cache/memory: TestGetAndSetRoundTrip`; `integration: cache/redis: TestGetSetRoundTrip` | all |  |
| cache.backend.miss-nil-nil | On each shipped backend, a `Get` miss returns `(nil, nil)` | `unit: cache/memory: TestGetAndSetRoundTrip`; `integration: cache/redis: TestGetMissReturnsNilNil` | all |  |
| cache.backend.invalidate-absent-ok | On each shipped backend, `Invalidate` and `InvalidateMany` remove the named keys and treat an absent key as a success, and an `InvalidatePattern` that matches no key is a success | `unit: cache/memory: TestInvalidateRemovesPresentAndIgnoresAbsent`; `unit: cache/memory: TestInvalidateManyRemovesPresentAndIgnoresAbsent`; `unit: cache/memory: TestInvalidatePattern/no matching key is not an error`; `integration: cache/redis: TestInvalidateRemovesPresentAndIgnoresAbsent`; `integration: cache/redis: TestInvalidateManyIgnoresAbsentKeys`; `integration: cache/redis: TestInvalidatePatternWithNoMatchIsNotAnError` | all |  |
| cache.backend.pattern-prefix | On each shipped backend, `InvalidatePattern(p + "*")` removes every key that starts with `p` and leaves every other key | `unit: cache/memory: TestInvalidatePattern/removes matching prefix`; `integration: cache/redis: TestInvalidatePatternWith10KKeysUsesSCANAndBatchedDELs`; `integration: cache/redis: TestInvalidatePatternWithSmallScanCount` | all |  |
| cache.backend.ttl-expiry | On each shipped backend, an entry `Set` with a positive TTL expires after it, while an entry with a longer TTL is still there | `unit: cache/memory: TestSetPerEntryTTLExpires`; `integration: cache/redis: TestSetWithTTLExpires`; `integration: cache/redis: TestPerEntryTTLsExpireIndependently` | all |  |
| cache.memory.max-size-required | `memory.New` returns an error and no backend when `MaxSize` is zero or negative | `unit: cache/memory: TestNew_MaxSizeZeroReturnsError/zero`; `unit: cache/memory: TestNew_MaxSizeZeroReturnsError/negative` | all |  |
| cache.memory.closer | The in-memory backend implements `io.Closer` | `unit: cache/memory: TestInterfaceAssertions` | all |  |
| cache.redis.scan-not-keys | The Redis backend's `InvalidatePattern` walks the keyspace with paginated `SCAN` and deletes each page with one bounded `DEL`; it never sends `KEYS` | `integration: cache/redis: TestInvalidatePatternWith10KKeysUsesSCANAndBatchedDELs` | all |  |
| cache.redis.close-owned-client | The Redis backend's `Close` closes the Redis client only when the backend was built with `WithOwnedClient()`; by default the client stays usable | `integration: cache/redis: TestCloseDefaultIsNoOpClientStaysUsable`; `integration: cache/redis: TestCloseOwnedClientClosesClient` | all |  |

Proven on one target: the code below has no dialect branch (`docs/design/CONFORMANCE_VARIANTS.md` §3.3).

- `cache.facade.close-probes-closer`: `cmd/sqlgen/gen/templates/cache.go.tmpl:1133`

## §27.4 Injection via Hooks
<!-- prd: 27.4 sha256: 7a57b9dc63f77f85 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.facade.hooks | The generated `Cache` exposes `QueryHook() hook.QueryHook` and `MutationHook() hook.MutationHook`, the hooks it injects into the client | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsQueryAndMutationHooks` | all |  |
| cache.facade.options | The generated package declares the runtime options `WithInvalidationSource`, `WithMetricsRecorder`, `WithSerializer`, `WithCircuitBreaker` and `WithOnError`, each returning `CacheOption` with the parameter type the PRD gives | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsOptionsAPI` | all |  |
| cache.facade.with-cache-outermost | `WithCache` puts the cache query hook outside every hook registered with `WithQueryHook`, whichever option comes first: a cache hit returns before a user query hook runs | `example: cache: TestWithCache_OutermostWhateverTheOptionOrder/WithCache before the user hook`; `example: cache: TestWithCache_OutermostWhateverTheOptionOrder/WithCache after the user hook` | all |  |

Proven on one target: the code below has no dialect branch (`docs/design/CONFORMANCE_VARIANTS.md` §3.3).

- `cache.facade.with-cache-outermost`: `cmd/sqlgen/gen/templates/client.go.tmpl:176-177`

## §27.5 Cache Key Generation
<!-- prd: 27.5 sha256: c21af87fbe355014 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache-key.helper-value-typed | Each cached table's `keyFor{Table}` helper takes its PK as a concrete value type: the scalar PK type for a single-column PK, the `XXXPK` struct value for a composite PK | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsSinglePKKeyHelper`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsCompositePKKeyHelper` | all |  |
| cache-key.mysql-no-schema | A MySQL table's cache key carries no schema segment, even when the table context carries a schema | `unit: cmd/sqlgen/gen: TestBuildCacheContext_mysqlSchemaIsEmpty` | mysql |  |
| fingerprint.codegen-constant | Each cached table's schema fingerprint is emitted as a codegen-time constant in the generated cache file | `unit: cmd/sqlgen/gen: TestCacheTemplate_fingerprintConstantCarriesTheTableFingerprint` | all |  |
| fingerprint.column-names | Renaming a column or adding one changes the table's fingerprint | `unit: cmd/sqlgen/gen: TestFingerprint_columnRename`; `unit: cmd/sqlgen/gen: TestFingerprint_columnAdd` | all |  |
| fingerprint.column-order | Columns enter the fingerprint in declaration order: swapping two columns changes the table's fingerprint | `unit: cmd/sqlgen/gen: TestFingerprint_columnDeclarationOrderChanges` | all |  |
| fingerprint.go-types | Changing a column's Go type changes the table's fingerprint | `unit: cmd/sqlgen/gen: TestFingerprint_goTypeChange` | all |  |
| cache-key.composite-pk-clause-order | A composite key's components follow the DDL `PRIMARY KEY (...)` order, not column order: with no `primary_key.columns` override, the `XXXPK` struct's fields, and so the `keyFor{Table}` component order, are the clause order on every dialect | `unit: cmd/sqlgen/gen: TestBuildTableContexts_CompositePKFollowsClauseOrder/postgres`; `unit: cmd/sqlgen/gen: TestBuildTableContexts_CompositePKFollowsClauseOrder/mysql`; `unit: cmd/sqlgen/gen: TestBuildTableContexts_CompositePKFollowsClauseOrder/sqlite`; `unit: cmd/sqlgen/gen: TestCacheTemplate_compositeKeyFollowsClauseOrder`; `golden: introspect_sqlite` | all |  |
| fingerprint.pk-order | Reordering the primary-key columns changes the table's fingerprint | `unit: cmd/sqlgen/gen: TestFingerprint_pkOrderChange` | all |  |
| fingerprint.serializer | Switching `cache.serializer` from `json` to `msgpack` changes the table's fingerprint | `unit: cmd/sqlgen/gen: TestFingerprint_serializerSwitch` | all |  |
| cache.invalidate-table.pattern | `InvalidateTable` invalidates through `cache.BuildTablePattern(prefix, schema, name)` built from the table's schema and bare SQL name, not from the schema-qualified `hook.TableName` value | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsInvalidateTablePattern` | postgres |  |
| cache.invalidate.mutations-by-key | No mutation invalidates through a table pattern: in a package with no tenanted table, `BuildTablePattern` is called only by `InvalidateTable` | `unit: cmd/sqlgen/gen: TestCacheTemplate_whereOpsShareKeyBasedArm`; `example: cache: TestMutation_WhereMutationsInvalidateByKey` | all |  |
| cache-key.entity.single-pk | A single-PK entity key is `{prefix}:{schema}.{table}:fingerprint:v{fingerprint}:pk:{pk}`, with the PK formatted by `%v` (a time PK as its UTC instant, see `cache-key.time-utc-instant`) and `pk:` the last labeled segment | `unit: cache: TestBuildKey/postgres with schema and fingerprint`; `unit: cache: TestBuildKey/postgres uuid pk`; `unit: cache: TestBuildKey/custom prefix`; `unit: cache: TestBuildKeyLabelOrdering` | all | cache-keys: entity × single-pk |
| cache-key.time-utc-instant | A time PK value, or a time component of a composite PK (`time.Time`, or a value type embedding it such as `types.DateTime`), is written into the key as its UTC instant with no monotonic reading, so one instant builds one key whatever its zone | `unit: cache: TestBuildKeyTimePKZoneIndependent`; `example: tenancy: TestCacheTimeKeyZones`; `example: tenancy_mysql: TestCacheTimeKeyZones` | all |  |
| cache-key.entity.composite-pk | A composite key joins its PK components with `:` in the order given, all after the one terminal `pk:` label | `unit: cache: TestBuildCompositeKey/two-column composite`; `unit: cache: TestBuildCompositeKey/three-column composite` | all | cache-keys: entity × composite-pk |
| cache-key.entity.schema | With a schema, the key's table segment is `{schema}.{table}` | `unit: cache: TestBuildKey/postgres with schema and fingerprint`; `unit: cache: TestBuildCompositeKey/two-column composite` | all | cache-keys: entity × schema |
| cache-key.entity.no-schema | With no schema (MySQL, SQLite), the key's table segment is the bare table name, with no `.` | `unit: cache: TestBuildKey/mysql empty schema`; `unit: cache: TestBuildKey/sqlite empty schema`; `unit: cache: TestBuildCompositeKey/mysql two-column composite no schema` | mysql, sqlite | cache-keys: entity × no-schema |
| cache-key.table-pattern.schema | `BuildTablePattern(prefix, schema, table)` returns `{prefix}:{schema}.{table}:*`, with no `fingerprint:` or `pk:` segment | `unit: cache: TestBuildTablePattern/postgres`; `unit: cache: TestBuildTablePattern/custom prefix` | all | cache-keys: table-pattern × schema |
| cache-key.table-pattern.no-schema | With no schema, `BuildTablePattern` returns `{prefix}:{table}:*` | `unit: cache: TestBuildTablePattern/mysql empty schema` | mysql, sqlite | cache-keys: table-pattern × no-schema |
| cache-key.table-pattern.generations | A table pattern matches the table's keys under any fingerprint, and no key of a table whose name extends it or of the same table in another schema | `unit: cache: TestPatternsMatchTheKeysTheyCover/table pattern matches an earlier fingerprint generation`; `unit: cache: TestPatternsMatchTheKeysTheyCover/table pattern does not match a table whose name extends it`; `unit: cache: TestPatternsMatchTheKeysTheyCover/table pattern does not match the same table in another schema` | all |  |

Proven on one target: the code below has no dialect branch (`docs/design/CONFORMANCE_VARIANTS.md` §3.3).

- `cache-key.time-utc-instant`: `cache/key.go:177-178`

## §27.6 Cache Invariant
<!-- prd: 27.6 sha256: ae8265d73e0a67e9 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.relationship-bypass | A `Get` with any relationship flag set on `FieldOptions` bypasses the cache: the query hook checks for it before dispatching to read-through | `unit: cmd/sqlgen/gen: TestCacheTemplate_queryHookPreservesBypassGates`; `example: cache: TestSkip_RelationshipBypass` | all |  |
| cache.create.field-options-not-cached | A `Create` or `CreateMany` call that carries `FieldOptions` writes nothing to the cache | `unit: cmd/sqlgen/gen: TestCacheTemplate_createCachingSkipsWhenFieldOptionsSet` | all |  |

Proven on one target: the code below has no dialect branch (`docs/design/CONFORMANCE_VARIANTS.md` §3.3).

- `cache.relationship-bypass`: `cmd/sqlgen/gen/templates/cache.go.tmpl:802`

## §27.7 Cache Behavior by Operation
<!-- prd: 27.7 sha256: 98d0918d8dbb9798 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.read-through.get-only | Only `Get` reads through the cache; every other query op (`GetMany`, `Exists`, ...) passes straight to the database | `unit: cmd/sqlgen/gen: TestCacheTemplate_queryHookPreservesBypassGates` | all |  |
| cache.read-through.dispatch | A full `Get` on any cached table, single- or composite-PK, is routed to that table's read-through path, which reads the entity from the cache and on a miss queries the database and sets the cache with the table's TTL | `unit: cmd/sqlgen/gen: TestCacheTemplate_queryHookDispatchesToReadThrough`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsReadThroughHelper`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsReadThroughForCompositePK`; `example: cache: TestReadThrough_MissThenHit` | all |  |
| cache.read-through.singleflight-recheck | Concurrent read-through misses on one key collapse through singleflight, and the flight callback re-checks the cache before querying the database | `unit: cmd/sqlgen/gen: TestCacheTemplate_readThroughInFlightRecheck`; `unit: cmd/sqlgen/gen: TestCacheTemplate_readThroughInFlightRecheckCompositePK`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsReadThroughHelper`; `example: cache: TestReadThrough_SingleflightLateStraggler` | all |  |
| cache.partial.hydration-off-bypass | With hydration off, a partial `Get` (column flags only) passes through to the database with no cache read or write | `unit: cmd/sqlgen/gen: TestCacheTemplate_queryHookBypassesPartialWhenHydrationOff` | all |  |
| cache.partial.hydration-on | With hydration on, a partial `Get` that misses returns the partial database result and starts background hydration; the partial result never reaches the cache-set path | `unit: cmd/sqlgen/gen: TestCacheTemplate_readThroughPartialTriggersHydrate`; `example: cache: TestHydration_PartialFetchHydratesBackground` | all |  |
| cache.hydration.one-per-key | At most one hydration runs per `(table, pk)` at a time | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsHydrateHelper` | all |  |
| cache.create.write-through | `Create` and `CreateMany` write each returned entity to the cache under its own key | `conformance: Features/Cache/Create/writes the created row through under its own key`; `conformance: Features/Cache/CreateMany/writes the created row through under its own key`; `unit: cmd/sqlgen/gen: TestCacheTemplate_dispatchMutationSetsOnCreate`; `unit: cmd/sqlgen/gen: TestCacheTemplate_dispatchMutationSetsOnCreateMany`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsSetHelperSinglePK`; `unit: cmd/sqlgen/gen: TestCacheTemplate_mutationHookForwardsResult`; `example: cache: TestMutation_CreateSetsCache`; `example: cache: TestMutation_CreateManySetsCache` | all |  |
| cache.invalidate.many-and-where-by-key | `UpsertMany`, `UpdateMany`, `SoftDeleteMany`, `HardDeleteMany`, `RestoreMany` and the four `*Where` mutations share one key-based invalidation arm | `conformance: Features/Cache/UpsertMany/evicts each affected row's key`; `conformance: Features/Cache/UpdateMany/evicts each affected row's key`; `conformance: Features/Cache/SoftDeleteMany/evicts each affected row's key`; `conformance: Features/Cache/HardDeleteMany/evicts each affected row's key`; `conformance: Features/Cache/RestoreMany/evicts each affected row's key`; `conformance: Features/Cache/UpdateWhere/evicts each affected row's key`; `conformance: Features/Cache/SoftDeleteWhere/evicts each affected row's key`; `conformance: Features/Cache/HardDeleteWhere/evicts each affected row's key`; `conformance: Features/Cache/RestoreWhere/evicts each affected row's key`; `unit: cmd/sqlgen/gen: TestCacheTemplate_whereOpsShareKeyBasedArm`; `example: cache: TestMutation_UpdateManyInvalidatesBatch`; `example: cache: TestMutation_UpsertManyInvalidatesBatch`; `example: cache: TestMutation_WhereMutationsInvalidateByKey` | all |  |
| cache.invalidate.uncached-table-guard | The mutation invalidation arm returns before any backend call for a table that is not cached | `unit: cmd/sqlgen/gen: TestCacheTemplate_whereOpsShareKeyBasedArm`; `example: cache: TestWhereMutation_NonCachedTable_InvalidatesNothing` | all |  |
| cache.skip-cache.read | A `Get` with `CallOptions.SkipCache: true` does not read through the cache | `unit: cmd/sqlgen/gen: TestCacheTemplate_queryHookPreservesBypassGates`; `example: cache: TestSkip_CacheOnGet` | all |  |

Proven on one target: the code below has no dialect branch (`docs/design/CONFORMANCE_VARIANTS.md` §3.3).

- `cache.read-through.dispatch`: `cmd/sqlgen/gen/templates/cache.go.tmpl:813-835`, `cmd/sqlgen/gen/templates/cache.go.tmpl:263-271`
- `cache.read-through.singleflight-recheck`: `cmd/sqlgen/gen/templates/cache.go.tmpl:320-334`
- `cache.partial.hydration-on`: `cmd/sqlgen/gen/templates/cache.go.tmpl:306-317`
- `cache.invalidate.uncached-table-guard`: `cmd/sqlgen/gen/templates/cache.go.tmpl:979-981`
- `cache.skip-cache.read`: `cmd/sqlgen/gen/templates/cache.go.tmpl:798`

## §27.8 Hydration
<!-- prd: 27.8 sha256: 1940f10057dfe00e -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.hydration.full-refetch | Hydration re-queries the entity as `Get` with every column flag set on `FieldOptions` and hooks skipped | `unit: cmd/sqlgen/gen: TestCacheTemplate_hydrateRerunsWithFullFieldOptions`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsFullParentFieldOptions` | all |  |
| cache.hydration.stores-full | Hydration stores the fully fetched entity in the cache with the table's TTL | `unit: cmd/sqlgen/gen: TestCacheTemplate_hydrateSetsCacheOnSuccess` | all |  |
| cache.hydration.timeout | The hydration goroutine runs on a background context bounded by `hydration.timeout` | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsHydrateHelper` | all |  |

## §27.9 Invalidation Strategies
<!-- prd: 27.9 sha256: 1b446d2707de8dbb -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.invalidate.unknown-table-error | `InvalidateMany` builds keys with the named table's own key helper, and returns `cache: unknown or non-cached table %q` for a table that is not cached | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsInvalidateDispatch` | all |  |
| cache.breaker.open-skips | While the circuit breaker is open, read-through skips the cache read and the cache write, returning the database result | `unit: cmd/sqlgen/gen: TestCacheTemplate_readThroughBreakerGating`; `example: cache: TestBreaker_TripsAndRecovers` | all |  |
| cache.error-policy.routed | A cache backend `get` or `set` error is routed through the error policy (metrics, `OnErrorFunc`, breaker) with its op label | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsReadThroughHelper`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsSetHelperSinglePK`; `unit: cmd/sqlgen/gen: TestCacheTemplate_hydrateSetsCacheOnSuccess` | all |  |
| cache.invalidation.types | `InvalidationSource` is `Subscribe(InvalidationHandler) (InvalidationSubscription, error)` and `Close() error`; `InvalidationSubscription` is `Unsubscribe() error`; `InvalidationHandler` is `func(ctx, InvalidationSignal) error`; `InvalidationSignal` has the fields `Table hook.TableName`, `Schema string`, `Tenant string` and `PKs []any` | `unit: cache: TestInvalidationTypes` | all |  |
| cache.adapter.signatures | `FromEventSubscriber(sub event.Subscriber, opts ...EventAdapterOption) InvalidationSource` and `WithEventMetrics(m MetricsRecorder) EventAdapterOption` have the PRD's signatures, `DefaultOnCacheError` is an `OnErrorFunc`, and `*Breaker` has `RecordFailure()` | `unit: cache: TestEventAdapterAndErrorPolicySignatures` | all |  |
| cache.adapter.one-signal-per-event | The adapter calls the handler once per event, with `PKs` holding only that event's `PK`; a `PublishBatch` of three events makes three calls, in order | `unit: cache: TestFromEventSubscriber_SingleEvent`; `unit: cache: TestFromEventSubscriber_PublishBatchFansOut` | all |  |
| cache.adapter.table-name | The adapter's signal names the table `schema.table` when the event has a `Schema` and by its bare name when it has none, and carries the event's `Schema` | `unit: cache: TestFromEventSubscriber_TableNameFollowsTheConstantRule/public schema`; `unit: cache: TestFromEventSubscriber_TableNameFollowsTheConstantRule/a second schema stays apart`; `unit: cache: TestFromEventSubscriber_TableNameFollowsTheConstantRule/no schema (MySQL, SQLite)` | all |  |
| cache.adapter.tenant-forwarded | The adapter's signal carries `ev.Metadata["tenant"]` verbatim as `Tenant`, and an empty `Tenant` when the event has none | `unit: cache: TestFromEventSubscriber_ForwardsTenantMetadata`; `unit: cache: TestFromEventSubscriber_SingleEvent` | all |  |
| cache.adapter.subscribe-all | The adapter subscribes with the zero `event.SubscribeOptions{}`: every table, every action, no group | `unit: cache: TestFromEventSubscriber_SubscribesToAllTablesAndActions` | all |  |
| cache.adapter.error-routing | A handler error is returned as-is from the event handler, with or without `WithEventMetrics`; with it, the error is also recorded as `Error(ev.Schema, table, "invalidate", err)` with the handler's own error, and a handler success records nothing | `unit: cache: TestFromEventSubscriber_HandlerErrorIsRecordedAndPropagates`; `unit: cache: TestFromEventSubscriber_HandlerSuccessDoesNotRecordError`; `unit: cache: TestFromEventSubscriber_HandlerErrorWithoutMetricsPropagates` | all |  |
| cache.adapter.no-dedup | The adapter does not deduplicate by event ID: a redelivered event reaches the handler again | `unit: cache: TestFromEventSubscriber_RedeliveryIsIdempotent` | all |  |
| cache.direct-invalidate.tenanted-error | On a tenanted table, tenant-in-PK tables included, the generated `Invalidate` and `InvalidateMany` return `cache: InvalidateMany("<table>"): table is tenanted; use cache hook auto-invalidation or InvalidateTable / BuildTenantTablePattern` | `example: tenancy: TestCache_InvalidateManyOnTenantedTableErrors/InvalidateMany, tenant outside the PK`; `example: tenancy: TestCache_InvalidateManyOnTenantedTableErrors/Invalidate, tenant outside the PK`; `example: tenancy: TestCache_InvalidateManyOnTenantedTableErrors/InvalidateMany, tenant in the PK`; `example: tenancy: TestCache_InvalidateManyOnTenantedTableErrors/Invalidate, tenant in the PK` | all |  |
| cache.tx.event-before-cache | Inside a transaction, the event hook's publish runs before the cache invalidation on commit | `example: cache: TestTx_EventBeforeCacheOnCommit` | all |  |
| cache.tx.rollback-discards | A rolled-back transaction performs none of its cache invalidations | `example: cache: TestTx_RollbackDiscardsInvalidation` | all |  |
| cache.tx.committed-write-survives | With sync callbacks, a cache invalidation that fails on commit does not reach `WithTx`: the write commits and `WithTx` returns nil (async callbacks never report to `WithTx`) | `example: cache: TestTx_CommittedWriteSurvivesInvalidationFailure/sync callbacks` | all |  |
| cache.tx.retry-once | A cache invalidation that fails on commit runs once more: a backend that fails only the first call still evicts the stale entry | `example: cache: TestTx_FailedInvalidationIsRetriedOnce` | all |  |
| cache.mutation.unbuildable-key-reported | A mutation whose affected PK cannot build a key reports the error to the `OnErrorFunc` under op `invalidate_many`, once, and the write still returns nil, inside a transaction or not | `conformance: Features/Cache/an unbuildable key is reported once outside a transaction`; `conformance: Features/Cache/an unbuildable key is reported once inside a sync transaction` | all |  |
| cache.breaker.trips-at-threshold | In `Closed`, the breaker opens on the `failure_threshold`-th consecutive failure and not before, and a success resets the count | `unit: cache: TestBreaker_NthFailureTripsAndFiresStateChange`; `unit: cache: TestBreaker_ClosedAdvancesToNMinusOneWithoutTripping`; `unit: cache: TestBreaker_ClosedRecordSuccessResetsCounter` | all |  |
| cache.breaker.probe-interval | An open breaker refuses calls until `probe_interval` has elapsed; the next call then moves it to `HalfOpen` and is admitted as a probe | `unit: cache: TestBreaker_OpenAllowReturnsFalseUntilProbeIntervalElapses`; `unit: cache: TestBreaker_AllowPostProbeIntervalTransitionsToHalfOpen` | all |  |
| cache.breaker.half-open-success | A probe success in `HalfOpen` closes the breaker and resets the failure count | `unit: cache: TestBreaker_HalfOpenSuccessReturnsToClosed` | all |  |
| cache.breaker.half-open-failure | A probe failure in `HalfOpen` reopens the breaker at once, whatever `failure_threshold` is | `unit: cache: TestBreaker_HalfOpenFailureReopensImmediately` | all |  |
| cache.breaker.open-exits-via-half-open | An open breaker leaves `Open` only through `HalfOpen`: a success or failure reported while open changes nothing | `unit: cache: TestBreaker_OpenIgnoresRecordSuccessAndRecordFailure` | all |  |
| cache.breaker.half-open-max-probes | `HalfOpen` admits `half_open_max_probes` concurrent probes and refuses the rest | `unit: cache: TestBreaker_HalfOpenProbesInFlightSaturationBypassesFollowers`; `unit: cache: TestBreaker_HalfOpenSingleProbe` | all |  |
| cache.breaker.reports-transitions | Every breaker transition (Closed→Open, Open→HalfOpen, HalfOpen→Closed, HalfOpen→Open) is reported once with its `(from, to)` states | `unit: cache: TestBreaker_NthFailureTripsAndFiresStateChange`; `unit: cache: TestBreaker_AllowPostProbeIntervalTransitionsToHalfOpen`; `unit: cache: TestBreaker_HalfOpenSuccessReturnsToClosed`; `unit: cache: TestBreaker_HalfOpenFailureReopensImmediately` | all |  |
| cache.breaker.read-through-releases-probe | The read-through never strands a `HalfOpen` probe: its in-flight re-check calls no `Allow`, and after a trip no `Get` leaves the breaker in `HalfOpen` with its probe slot taken | `unit: cmd/sqlgen/gen: TestCacheTemplate_readThroughInFlightRecheck`; `example: cache: TestBreaker_ReadThroughNeverStrandsAHalfOpenProbe` | all |  |
| cache.breaker.reports-to-recorder | The breaker `NewCache` builds from the config reports its transitions to the `WithMetricsRecorder` recorder as `CircuitBreakerStateChange(from, to)` | `example: cache: TestBreaker_DefaultBreakerReportsToMetricsRecorder` | all |  |
| cache.error-policy.order | The error policy records `MetricsRecorder.Error` with the backend's own error, then calls the `OnErrorFunc`, then `breaker.RecordFailure()`, in that order | `unit: cache: TestRouteError_ThreeChannelOrdering` | all |  |
| cache.error-policy.logger-cannot-mask | A panicking `OnErrorFunc` does not stop the breaker feedback | `unit: cache: TestRouteError_PanickingOnErrorDoesNotSkipBreaker` | all |  |

Proven on one target: the code below has no dialect branch (`docs/design/CONFORMANCE_VARIANTS.md` §3.3).

- `cache.breaker.open-skips`: `cmd/sqlgen/gen/templates/cache.go.tmpl:271`, `cmd/sqlgen/gen/templates/cache.go.tmpl:343`, `cmd/sqlgen/gen/templates/cache.go.tmpl:391`
- `cache.direct-invalidate.tenanted-error`: `cmd/sqlgen/gen/templates/cache.go.tmpl:582`
- `cache.tx.event-before-cache`: `cmd/sqlgen/gen/templates/client.go.tmpl:168,176`, `database/transaction.go:215`
- `cache.tx.rollback-discards`: `cmd/sqlgen/gen/templates/cache.go.tmpl:1027-1028`, `database/transaction.go:604,612`
- `cache.tx.committed-write-survives`: `cmd/sqlgen/gen/templates/cache.go.tmpl:1062-1068`
- `cache.tx.retry-once`: `cmd/sqlgen/gen/templates/cache.go.tmpl:1064-1065`
- `cache.breaker.read-through-releases-probe`: `cmd/sqlgen/gen/templates/cache.go.tmpl:330`
- `cache.breaker.reports-to-recorder`: `cmd/sqlgen/gen/templates/cache.go.tmpl:100-104`

## §27.10 Serialization
<!-- prd: 27.10 sha256: 384ffadc196b96c4 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.serializer.shape | `Serializer` is `Marshal(v any) ([]byte, error)` and `Unmarshal(data []byte, v any) error` | `unit: cache: TestSerializerMethodSet` | all |  |
| cache.serializer.json-stdlib-bytes | The core JSON serializer writes exactly the bytes `encoding/json` does, and reads them back | `unit: cache: TestJSONSerializerRoundTrip` | all |  |
| cache.serializer.msgpack-new | `msgpack.New()` returns a `cache.Serializer` that writes exactly the msgpack library's encoding and reads entities back, generated column types included | `unit: cache/msgpack: TestNewEncodesMsgpack`; `unit: cache/msgpack: TestRoundTripTaggedEntity/default`; `unit: cache/msgpack: TestRoundTripFullShapedEntity` | all |  |

## §27.11 View Cache Invalidation
<!-- prd: 27.11 sha256: fed6e214001c8167 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.view-invalidate.keyed-by-table-name | The source-table → view-pattern map is keyed by each source table's `hook.TableName` value, so a bare or schema-qualified `invalidate_on` entry resolves to the same source | `unit: cmd/sqlgen/gen: TestBuildCacheContext_viewInvalidateMap/postgres bare spelling resolves to the qualified value`; `unit: cmd/sqlgen/gen: TestBuildCacheContext_viewInvalidateMap/postgres qualified spelling resolves to the same value`; `unit: cmd/sqlgen/gen: TestBuildCacheContext_viewInvalidateMap/mysql has no schema, so the value is the bare name`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsViewInvalidateSwitch` | postgres, mysql |  |
| cache.matview.refresh-invalidates | A cached materialized view's `Refresh` / `RefreshConcurrently` invalidates the view's own cache entries after the refresh succeeds, deferred to commit inside a transaction | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsRefreshInvalidation` | postgres |  |
| cache.matview.refresh-uncached-noop | With no cached view, a materialized-view refresh performs no cache operation | `unit: cmd/sqlgen/gen: TestCacheTemplate_refreshNoCachedViewsIsNoOp` | postgres |  |

## §27.12 Cache Warming
<!-- prd: 27.12 sha256: 77c2235eb6418f0a -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache.warm.no-config | There is no `cache.warm` config: the loader rejects the key, and so does the JSON schema | `unit: cmd/sqlgen/config: TestLoadConfig_RejectsUnknownKeys/cache warming is not a config key`; `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/cache warming block` | all |  |

## §27.13 Cache Metrics
<!-- prd: 27.13 sha256: 5a26bc9406b46682 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| cache-key.postgres-default-public | A PostgreSQL table with no schema is keyed and labeled under schema `public` | `unit: cmd/sqlgen/gen: TestBuildCacheContext_postgresEmptySchemaNormalized` | postgres |  |
| cache.metrics.read-write | Read-through reports `Hit` or `Miss` and `GetLatency`; every cache write reports `Set` and `SetLatency`, labeled with the table's schema and `TableName` constant | `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsReadThroughHelper`; `unit: cmd/sqlgen/gen: TestCacheTemplate_emitsSetHelperSinglePK` | all |  |
| cache.metrics.hydration | Hydration reports `HydrationStart` when launched and `HydrationComplete` with the query error, the set error, or nil on success | `unit: cmd/sqlgen/gen: TestCacheTemplate_hydrateReportsHydrationComplete` | all |  |
| cache.metrics-recorder.method-set | `MetricsRecorder` has exactly the eleven methods `Hit`, `Miss`, `Set`, `Invalidate`, `Error`, `HydrationStart`, `HydrationComplete`, `CircuitBreakerStateChange`, `GetLatency`, `SetLatency` and `InvalidateLatency`, with the PRD's signatures | `unit: cache: TestMetricsRecorderMethodSet` | all |  |
| cache.metrics.facade-error-ops | The generated facade reports errors under exactly the ops `get`, `set`, `invalidate_many`, `invalidate_pattern` and `invalidate_tenant_fallback` | `unit: cmd/sqlgen/gen: TestCacheTemplate_errorOpLabels` | all |  |
| cache.metrics.nil-recorder | With no `MetricsRecorder`, the error policy skips the metrics step and still calls the `OnErrorFunc` and the breaker | `unit: cache: TestRouteError_NilMetricsIsZeroCost` | all |  |
| cache.otel.counters | `metrics/otel` counts `Hit`, `Miss`, `Set` and `Invalidate` on the monotonic counters `sqlgen.cache.hits`, `sqlgen.cache.misses`, `sqlgen.cache.sets` and `sqlgen.cache.invalidations`, labeled `schema` and `table` | `unit: metrics/otel: TestHitDistinguishesSchemas`; `unit: metrics/otel: TestCounterMethods/Miss`; `unit: metrics/otel: TestCounterMethods/Set`; `unit: metrics/otel: TestCounterMethods/Invalidate` | all |  |
| cache.otel.errors | `Error` counts on `sqlgen.cache.errors`, labeled `schema`, `table` and `op` | `unit: metrics/otel: TestErrorAttachesOpLabel` | all |  |
| cache.otel.hydrations | `HydrationComplete` counts on `sqlgen.cache.hydrations` with `status` `success` for a nil error and `error` otherwise; `HydrationStart` records nothing | `unit: metrics/otel: TestHydrationCompleteStatusLabel`; `unit: metrics/otel: TestHydrationStartIsNoop` | all |  |
| cache.otel.breaker-gauge | `CircuitBreakerStateChange` sets the `sqlgen.cache.circuit_breaker` gauge, labeled `state` only: 1 under the state entered and 0 under the state left, so after transitions delivered in order exactly the current state reads 1 | `unit: metrics/otel: TestCircuitBreakerStateChangeLabels`; `unit: metrics/otel: TestCircuitBreakerGaugeShowsOnlyTheCurrentState` | all |  |
| cache.otel.latency-seconds | `GetLatency`, `SetLatency` and `InvalidateLatency` record on the histograms `sqlgen.cache.get.duration`, `sqlgen.cache.set.duration` and `sqlgen.cache.invalidate.duration` in seconds, labeled `schema` and `table` | `unit: metrics/otel: TestGetLatencyRecordsSeconds`; `unit: metrics/otel: TestLatencyInstruments/SetLatency`; `unit: metrics/otel: TestLatencyInstruments/InvalidateLatency` | all |  |
| cache.otel.schema-label | The `schema` label keeps same-named tables in two schemas apart, and an empty schema (MySQL, SQLite) is a label value of its own | `unit: metrics/otel: TestHitDistinguishesSchemas`; `unit: metrics/otel: TestHitEmptySchemaIsValidDimension` | all |  |
| cache.otel.table-label | The `table` label is the bare SQL name on every method: a schema-qualified `hook.TableName` loses its `schema.` prefix | `unit: metrics/otel: TestTableLabelIsTheBareSQLName` | all |  |
| cache.otel.provider | `New(nil)` records through `otel.GetMeterProvider()`, and `New(p)` through `p` | `unit: metrics/otel: TestNewNilProviderUsesGlobal`; `unit: metrics/otel: TestNewExplicitProvider` | all |  |

## §29.5 Cache Key Grammar Extension
<!-- prd: 29.5 sha256: 343278b8870f1d6b -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| fingerprint.tenanted-marker | Making a table tenanted changes its fingerprint (the tenanted fact is a fingerprint input) | `unit: cmd/sqlgen/gen: TestFingerprint_tenancyStructuralMarker_changesFingerprint` | all |  |
| cache-key.tenant-per-row-invalidation | A tenanted table's mutation invalidation, `*Where` included, evicts per row from the captured tenants rather than by pattern | `conformance: Features/Cache/Update/evicts each affected row's key`; `conformance: Features/Cache/UpdateMany/evicts each affected row's key`; `conformance: Features/Cache/UpdateWhere/evicts each affected row's key`; `conformance: Features/Cache/Upsert/evicts each affected row's key`; `conformance: Features/Cache/UpsertMany/evicts each affected row's key`; `conformance: Features/Cache/Increment/evicts each affected row's key`; `conformance: Features/Cache/SoftDelete/evicts each affected row's key`; `conformance: Features/Cache/SoftDeleteMany/evicts each affected row's key`; `conformance: Features/Cache/SoftDeleteWhere/evicts each affected row's key`; `conformance: Features/Cache/HardDelete/evicts each affected row's key`; `conformance: Features/Cache/HardDeleteMany/evicts each affected row's key`; `conformance: Features/Cache/HardDeleteWhere/evicts each affected row's key`; `conformance: Features/Cache/Restore/evicts each affected row's key`; `conformance: Features/Cache/RestoreMany/evicts each affected row's key`; `conformance: Features/Cache/RestoreWhere/evicts each affected row's key`; `unit: cmd/sqlgen/gen: TestCacheTemplate_tenantedWhereOpsStayPerRow` | all |  |
| cache-key.tenant-fallback-table-pattern | When a captured tenant is missing, invalidation falls back to the cross-tenant `BuildTablePattern` clear built from the table's schema and bare name | `unit: cmd/sqlgen/gen: TestCacheTemplate_tenantedWhereOpsStayPerRow`; `example: tenancy: TestNilTenant_ForcedCaptureGapFallsBackSafely` | all |  |
| cache-key.tenant-capture-update-returning | On PostgreSQL and SQLite, a tenanted table's (tenant outside the PK) `Update`, `UpdateMany` and `UpdateWhere` capture each affected row's tenant by widening the statement's `RETURNING` projection into `MutationContext.AffectedTenants`, with no pre-read | `unit: cmd/sqlgen/gen: TestTenantCapture_update_returningDialects` | postgres, sqlite |  |
| cache-key.tenant-capture-delete-increment-upsert-returning | On PostgreSQL, a tenanted table's (tenant outside the PK) `HardDelete` family, `Increment` and `Upsert` capture the affected row's tenant by widening the statement's `RETURNING` projection (the tenant column, or the PK and the tenant) into `MutationContext.AffectedTenants` | `unit: cmd/sqlgen/gen: TestTenantCapture_delete_acrossDialects/postgres returning`; `unit: cmd/sqlgen/gen: TestTenantCapture_increment_acrossDialects/postgres returning`; `unit: cmd/sqlgen/gen: TestTenantCapture_upsert_capturesRowTenant/postgres returning` | postgres |  |
| cache-key.tenant-capture-mysql-select | On MySQL, tenanted mutations (tenant outside the PK) use no `RETURNING`: `Update`, `Increment` and `HardDelete` read the affected rows' tenants with a `SELECT` over the statement's own conditions, `UpdateWhere` and `HardDeleteWhere` widen `collectAffectedIDs` to project the PK and the tenant column, and `Upsert` reads the written row's tenant by PK after the statement | `unit: cmd/sqlgen/gen: TestTenantCapture_update_mysqlPreRead`; `unit: cmd/sqlgen/gen: TestTenantCapture_delete_acrossDialects/mysql pre-read`; `unit: cmd/sqlgen/gen: TestTenantCapture_increment_acrossDialects/mysql pre-read`; `unit: cmd/sqlgen/gen: TestTenantCapture_upsert_capturesRowTenant/mysql post-read`; `unit: cmd/sqlgen/gen: TestTenantCapture_client_preReadHelperEmission/mysql tenanted emits helper` | mysql |  |
| cache-key.tenant-capture-mysql-many-per-sub-batch | On MySQL, a tenanted `UpdateMany` captures the affected rows' tenants with one pre-read per sub-batch, inside the batch loop, and a key repeated in a later sub-batch keeps the tenant its first pre-read captured | `unit: cmd/sqlgen/gen: TestTenantCapture_updateMany_mysqlPreReadPerSubBatch`; `unit: cmd/sqlgen/gen: TestTenantCapture_update_mysqlPreRead`; `example: tenancy_mysql: TestMySQLCapture_UpdateManyRepeatedKeyKeepsPreMutationTenant` | mysql |  |
| cache-key.tenant-capture-create | `Create` and `CreateMany` on a tenanted table (tenant outside the PK) record the tenant value they wrote (the resolved tenant, or the caller's value when tenancy is skipped) into `MutationContext.AffectedTenants`, one element per created row | `unit: cmd/sqlgen/gen: TestTenantCapture_create_capturesInsertedValue` | all |  |
| cache-key.tenant-capture-composite-pk | On a composite-PK table whose tenant column is outside the PK, the capture projection carries every PK column and the tenant: PostgreSQL's `UpdateMany` widens `RETURNING` to the PK columns and the tenant, and MySQL's `UpdateWhere` widens `collectAffectedPKs` to return the tenants | `unit: cmd/sqlgen/gen: TestTenantCapture_compositeTenantOutsidePK` | postgres, mysql |  |
| cache-key.tenant-in-pk-no-capture | On a table whose tenant column is part of the primary key, no mutation runs an extra tenant capture: `Create`, `Update`, the delete family and the client emit no `AffectedTenants`, `captureAffectedTenants` or `capturedTenant` | `unit: cmd/sqlgen/gen: TestTenantCapture_tenantInPK_noCarrier` | postgres |  |
| cache-key.entity.tenanted | A tenanted table's key is `{prefix}:{schema}.{table}:tenant:{tenant}:fingerprint:v{fingerprint}:pk:{pk}`, with the tenant formatted by `%v` and placed before `fingerprint:`, composite keys included; two tenants give two keys | `conformance: Features/Cache/Update/evicts each affected row's key`; `conformance: Features/Cache/event-driven invalidation/a tenant-in-PK event with no stamp evicts that PK's key only`; `unit: cache: TestBuildTenantKey/postgres tenanted with schema`; `unit: cache: TestBuildTenantKey/mysql tenanted empty schema`; `unit: cache: TestBuildTenantKey/sqlite tenanted empty schema uuid pk`; `unit: cache: TestBuildTenantKey/integer tenant`; `unit: cache: TestBuildTenantKeyLabelOrdering`; `unit: cache: TestBuildCompositeTenantKey/postgres two-column composite tenanted`; `unit: cache: TestBuildCompositeTenantKey/three-column composite tenanted`; `unit: cache: TestBuildTenantKeyDifferentTenantsDistinctKeys` | all | cache-keys: entity × tenanted |
| cache-key.table-pattern.tenanted | A tenanted table's `BuildTablePattern` matches every tenant's keys, composite keys included | `conformance: Features/Cache/event-driven invalidation/a tenant-less event clears the table across tenants and returns nil`; `unit: cache: TestPatternsMatchTheKeysTheyCover/table pattern matches every tenant of a tenanted table`; `unit: cache: TestPatternsMatchTheKeysTheyCover/table pattern matches a second tenant of the same table`; `unit: cache: TestPatternsMatchTheKeysTheyCover/table pattern matches a composite tenanted key` | all | cache-keys: table-pattern × tenanted |
| cache-key.tenant-pattern.schema | `BuildTenantTablePattern(prefix, schema, table, tenant)` returns `{prefix}:{schema}.{table}:tenant:{tenant}:*`, with one trailing wildcard and no `fingerprint:` or `pk:` segment | `unit: cache: TestBuildTenantTablePattern/postgres tenanted with schema`; `unit: cache: TestBuildTenantTablePattern/integer tenant` | all | cache-keys: tenant-table-pattern × schema |
| cache-key.tenant-pattern.no-schema | With no schema, `BuildTenantTablePattern` returns `{prefix}:{table}:tenant:{tenant}:*` | `unit: cache: TestBuildTenantTablePattern/mysql tenanted empty schema` | mysql, sqlite | cache-keys: tenant-table-pattern × no-schema |
| cache-key.tenant-pattern.tenanted | A tenant pattern matches its tenant's keys under any fingerprint, composite keys included, and no key of another tenant, including a tenant whose value extends it | `unit: cache: TestPatternsMatchTheKeysTheyCover/tenant pattern matches its tenant under any fingerprint`; `unit: cache: TestPatternsMatchTheKeysTheyCover/tenant pattern matches its tenant's composite keys`; `unit: cache: TestPatternsMatchTheKeysTheyCover/tenant pattern does not match another tenant`; `unit: cache: TestPatternsMatchTheKeysTheyCover/tenant pattern does not match a tenant whose value extends it` | all | cache-keys: tenant-table-pattern × tenanted |
| cache-key.pattern-builder-signatures | `BuildTablePattern(prefix, schema, table string) string` and `BuildTenantTablePattern(prefix, schema, table string, tenant any) string` have the PRD's signatures | `unit: cache: TestPatternBuilderSignatures` | all |  |

Proven on one target: the code below has no dialect branch (`docs/design/CONFORMANCE_VARIANTS.md` §3.3).

- `cache-key.tenant-fallback-table-pattern`: `cmd/sqlgen/gen/templates/cache.go.tmpl:665-673`

## Not applicable

| Matrix cell | Reason | Accepted |
|---|---|---|
| cache-keys: table-pattern × single-pk | §27.5: the table pattern omits the `pk:` segment, so it is the same pattern whatever the PK's shape | |
| cache-keys: table-pattern × composite-pk | §27.5: the table pattern omits the `pk:` segment, so it is the same pattern whatever the PK's shape | |
| cache-keys: tenant-table-pattern × single-pk | §29.5: the tenant pattern ends at `tenant:{tenant}:*`, before the `pk:` segment, so it is the same pattern whatever the PK's shape | |
| cache-keys: tenant-table-pattern × composite-pk | §29.5: the tenant pattern ends at `tenant:{tenant}:*`, before the `pk:` segment, so it is the same pattern whatever the PK's shape | |

## Unclaimed

| Unclaimed test | Reason |
|---|---|
| cache: TestBreaker_ConcurrentAllowAndRecordRaceFree | 150 goroutines call `Allow`, `RecordSuccess` and `RecordFailure` with no forced interleaving; it checks only that the state stays one of the three, and reports a data race only under `-race`. The PRD states no concurrency rule for the breaker |
| cache/memory: TestConcurrentAccessIsRaceFree | 100 goroutines mix `Set`, `Get`, `Invalidate` and `InvalidatePattern` and assert nothing; it reports a data race only under `-race`. §27.3 states no concurrency rule for a backend |
| cache/redis: TestConcurrentInvalidateManyGetSet | Concurrent `InvalidateMany`, `Get` and `Set` on one Redis backend, asserting only that no call errors. §27.3 states no concurrency rule for a backend |
| cache/msgpack: TestBinaryEncodingSmallerThanJSON | Checks that one entity's msgpack bytes are fewer than its JSON bytes. §27.10's Size and Speed columns ("~30% smaller", "~2x faster") are an approximate comparison, not a requirement |

## PRD gaps

Behavior the code has, pinned by tests, that no PRD text states. Each gap waits for
the user's ruling: amend the PRD, which turns the gap into requirement rows, or drop the pins.

| Gap | Behavior | Tests | Section | Ruling |
|---|---|---|---|---|
| gap.fingerprint.width | The fingerprint is deterministic and is the first 4 bytes of the sha256 as 8 hex characters; §27.5 states sha256 and shows `v1a2b3c`-style examples but no width | `unit: cmd/sqlgen/gen: TestFingerprint_deterministic` | §27.5 |  |
| gap.cache.view-invalidate-bare-every-schema | On PostgreSQL a bare `invalidate_on` entry names the table in every schema that has one, keying the map under each qualified value; §27.11 says only that bare and qualified spellings name the same source | `unit: cmd/sqlgen/gen: TestBuildCacheContext_viewInvalidateMap/postgres bare spelling names the table in every schema` | §27.11 |  |
| gap.cache.key-builder-names | `cache` exports the entity-key builders `BuildKey`, `BuildCompositeKey`, `BuildTenantKey` and `BuildCompositeTenantKey`, which the generated `keyFor{Table}` helpers call. §27.5 and §29.5 state the key grammar and name only the two pattern builders |  | §27.5, §29.5 | |
| gap.cache.key-empty-fingerprint | A key builder given an empty fingerprint drops the `fingerprint:v…` segment and keeps the tenant segment; the §27.5 grammar always has the segment, and the generator always passes a fingerprint | `unit: cache: TestBuildKey/empty fingerprint drops fingerprint segment`; `unit: cache: TestBuildKeyFingerprintPresence`; `unit: cache: TestBuildCompositeKey/empty fingerprint two-column composite`; `unit: cache: TestBuildTenantKey/empty fingerprint preserves tenant segment` | §27.5 | |
| gap.cache.typed-helpers | `cache` exports the typed helpers `GetAs` (a nil read is a miss: the zero value, `false`, nil), `SetAs` (a marshal error writes nothing), `GetOrSet` (loads and stores on a miss; a load error stores nothing) and `KeysFromAny` (keys in input order, an empty non-nil slice for no PKs, an error naming the index and both types on a mismatch). The generated facade uses `GetAs`, `SetAs` and `KeysFromAny`, and nothing generated uses `GetOrSet`. §27.5 names `cache.SetAs` only as an example caller | `unit: cache: TestGetAs_MissReturnsZeroFalseNil`; `unit: cache: TestGetAs_HitReturnsDecodedValue`; `unit: cache: TestSetAs_MarshalErrorDoesNotTouchBackend`; `unit: cache: TestGetOrSet_MissCallsLoadThenStores`; `unit: cache: TestGetOrSet_LoadErrorPropagatesWithoutSet`; `unit: cache: TestKeysFromAny_HappyPathPreservesOrder`; `unit: cache: TestKeysFromAny_EmptySliceReturnsEmptyNilError`; `unit: cache: TestKeysFromAny_TypeMismatchErrorFormat` | §27.5, §27.9 | |
| gap.cache.noop-backend | `cache.NoopBackend` is a `Backend` and `StatsReporter` whose `Get` always misses, whose writes and invalidations succeed and do nothing, and whose `Stats` is zero. No PRD text names it | `unit: cache: TestNoopBackend_GetAlwaysMisses`; `unit: cache: TestNoopBackend_InvalidateCallsAreNoOps`; `unit: cache: TestNoopBackend_StatsReturnsZero`; `unit: cache: TestBackendInterfaceAssertions` | §27.3 | |
| gap.cache.runtime-helper-names | The names the generated facade builds the runtime with are not in the PRD: `JSONSerializer`, `NewBreaker`, `BreakerConfig` (`FailureThreshold`, `ProbeInterval`, `HalfOpenMaxProbes`, `OnStateChange`), the `Breaker` methods `Allow`, `RecordSuccess` and `State`, the `CircuitState` constants `StateClosed`, `StateOpen` and `StateHalfOpen` with their `String()` labels, and `RouteError`, the error policy's entry point. §27.9 names only `cache.Breaker` and `breaker.RecordFailure()` |  | §27.9, §27.10 | |
| gap.cache.default-on-error-format | `DefaultOnCacheError` logs one line through the standard `log` package, `cache {op} {schema}.{table}: {err}`, adding the schema only when the table value does not already carry it, and logging the bare table when the schema is empty. §27.9 says only that it logs to stderr | `unit: cache: TestDefaultOnCacheError_WithSchema`; `unit: cache: TestDefaultOnCacheError_QualifiedTable/qualified value`; `unit: cache: TestDefaultOnCacheError_QualifiedTable/second schema`; `unit: cache: TestDefaultOnCacheError_QualifiedTable/schema named like the table`; `unit: cache: TestDefaultOnCacheError_EmptySchema` | §27.9 | |
| gap.cache.adapter-lifecycle | The adapter's `Close` unsubscribes from the event subscriber, after which no event reaches the handler, and is idempotent; a subscription's `Unsubscribe` is idempotent and unsubscribes once; a `Subscribe` failure is returned so that `errors.Is` matches it. §27.9 states the facade's subscribe-at-construction and unsubscribe-at-`Close`, not the adapter's | `unit: cache: TestFromEventSubscriber_CloseUnsubscribes`; `unit: cache: TestFromEventSubscriber_CloseUnsubscribesUnderlyingSubscription`; `unit: cache: TestEventAdapterSubscription_UnsubscribeIdempotent`; `unit: cache: TestFromEventSubscriber_SubscribeErrorPropagates` | §27.9 | |
| gap.cache.backend-stats | Both shipped backends implement the optional `StatsReporter`: after two `Set`s, one hit, one miss and one `Invalidate`, `Stats` reads `Sets` 2, `Hits` 1, `Misses` 1 and `Invalidations` 1 (the in-memory backend with `StatsEnabled`). §27.3 makes `StatsReporter` optional and does not say which backends implement it | `unit: cache/memory: TestStatsReportsActivity`; `integration: cache/redis: TestStatsWithLocalStatsEnabled` | §27.3 | |
| gap.memory.options | `memory.Options` has `DefaultTTL`, used when `Set` gets a zero TTL (both zero: the entry never expires), and `StatsEnabled`, without which `Stats` reports zero `Hits`, `Misses` and `Evictions` while still counting `Sets`. §27.4's wiring example shows `MaxSize` and `DefaultTTL` without stating either rule | `unit: cache/memory: TestSetZeroTTLUsesDefaultTTL`; `unit: cache/memory: TestSetZeroTTLWithZeroDefaultNeverExpires`; `unit: cache/memory: TestStatsDisabledSuppressesOtterCounters` | §27.3, §27.4 | |
| gap.memory.closed | After `Close`, every in-memory backend method returns `memory.ErrClosed`, and a second `Close` returns nil | `unit: cache/memory: TestCloseIsIdempotentAndBlocksFurtherCalls` | §27.3 | |
| gap.memory.pattern-requires-star | The in-memory backend rejects a pattern with no trailing `*` with an error; §27.3 states only the minimal grammar a backend must accept | `unit: cache/memory: TestInvalidatePattern/missing trailing star errors` | §27.3 | |
| gap.redis.options | The Redis backend has `WithScanCount` (the `SCAN` page size; the default is the exported `DefaultScanCount`) and `WithLocalStats` (on by default; off, `Stats` counts nothing but `Entries`). It sends one variadic `DEL` per `InvalidateMany` and none for an empty list, and stores a zero-TTL `Set` without expiry. §27.3 names none of these | `integration: cache/redis: TestInvalidatePatternWithSmallScanCount`; `integration: cache/redis: TestStatsWithLocalStatsDisabled`; `integration: cache/redis: TestInvalidateManyIssuesSingleVariadicDEL`; `integration: cache/redis: TestInvalidateManyEmptyIsNoOp`; `integration: cache/redis: TestSetZeroTTLDoesNotExpire` | §27.3 | |
| gap.msgpack.new-with | `msgpack.NewWith(Options)` builds a pooled serializer from `Options` (`CustomStructTag`, `SortMapKeys`, `OmitEmpty`, `UseArrayEncodedStructs`, `UseCompactInts`, `UseCompactFloats`, `UseInternedStrings`, `DisallowUnknownFields`, `ConfigureEncoder`, `ConfigureDecoder`), each instance keeping its own options across pool reuse. §27.10 names only `msgpack.New()` and alludes to "a preconfigured encoder pool, custom tags" | `unit: cache/msgpack: TestRoundTripTaggedEntity/with options`; `unit: cache/msgpack: TestRoundTripTaggedEntity/with compact + sorted`; `unit: cache/msgpack: TestNewWithConfigureHookApplies`; `unit: cache/msgpack: TestCustomStructTagReadsAlternateTag`; `unit: cache/msgpack: TestDisallowUnknownFieldsErrors`; `unit: cache/msgpack: TestPooledSerializerIsolatesOptions` | §27.10 | |
