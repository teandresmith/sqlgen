# Runtime Requirements: Events

<!-- seeding: in-progress -->
<!-- packages: event event/memorybus event/natsbus -->
<!-- matrix: transports rows=transport:memorybus,nats-core,nats-jetstream cols=delivery,ordering,persistence,handler-error -->

The event system (§28) and its tenant metadata (§29.6). See [README.md](../README.md) for the
format.

The `transports` matrix covers the three provided transports against the §28.7 table's Delivery,
Ordering and Persistence columns, and the §28.5 handler-error rule for each delivery class.
JetStream tests skip under `-short`, so they are cited as `integration:`; they run in
`make test-integration` and `make conformance`.

## §28.2 Package Structure
<!-- prd: 28.2 sha256: bed0abe0162cf6a2 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| event.package.stdlib-only | `event` imports only the standard library: no parser, CLI or other runtime package | `unit: event: TestEventPackagesImportOnlyStdlib/event` | all |  |

## §28.3 Event Structure
<!-- prd: 28.3 sha256: e20b334187edb7fd -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| event.struct.fields | `Event` has exactly the fields `ID string`, `Table string`, `Schema string`, `Action Action`, `PK any`, `Input any`, `Timestamp time.Time` and `Metadata map[string]string`, in that order | `unit: event: TestEventStructMatchesWireContract` | all |  |
| event.action.values | `Action` is a string type, and `Create`, `Update`, `Delete` and `Upsert` are `"create"`, `"update"`, `"delete"` and `"upsert"` | `unit: event: TestActionConstants`; `unit: event: TestEventStructMatchesWireContract` | all |  |
| event.wire.keys | An `Event` marshals to exactly the keys `id`, `table`, `schema`, `action`, `pk`, `input`, `timestamp` and `metadata`, and natsbus publishes that JSON as the message body | `unit: event: TestEventJSONWireFormat/all_keys_are_snake_case`; `unit: event: TestEventStructMatchesWireContract`; `unit: event/natsbus: TestPublishBodyUsesWireKeys` | all |  |
| event.wire.metadata-omitted | A nil or empty `Metadata` is omitted from the wire form | `unit: event: TestEventJSONWireFormat/nil_metadata_is_omitted`; `unit: event: TestEventJSONWireFormat/empty_metadata_is_omitted` | all |  |
| event.wire.any-decodes-as-map | An object-valued `PK` or `Input` arrives at a natsbus subscriber as `map[string]any` | `unit: event/natsbus: TestJSONRoundTrip` | all |  |

## §28.4 Publisher Interface
<!-- prd: 28.4 sha256: d103b12f6d37f9f8 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| event.publisher.interface | `Publisher` has exactly `Publish(context.Context, Event) error`, `PublishBatch(context.Context, []Event) error` and `Close() error` | `unit: event: TestInterfaceSignatures/Publisher` | all |  |
| event.publish-batch.every-event | A provided transport's `PublishBatch` delivers every event of the batch, in batch order, across tables, and natsbus sends each to its own subject | `unit: event/memorybus: TestPublishBatch`; `unit: event/natsbus: TestPublishBatch` | all |  |
| event.hook.failed-mutation-no-event | A mutation that fails publishes no event, including one whose write ran before an inner hook failed it | `example: events: TestEvent_FailedMutationPublishesNothing` | all |  |
| event.tx.deferred-to-commit | Inside a transaction no event is published before commit; the commit publishes them, in either callback mode | `example: events: TestEventTx_CommitPublishes`; `example: events: TestEventTx_NoImmediateEmissionBetweenMutations`; `example: events: TestEventTx_SyncCallbackModePublishesOnCommit` | all |  |
| event.tx.rollback-discards | A rolled-back transaction publishes none of its events, a rolled-back savepoint included | `example: events: TestEventTx_RollbackDiscards`; `example: events: TestEventTx_NestedInnerRollbackOuterCommit` | all |  |
| event.outside-tx-immediate | Outside a transaction a mutation's events are published before the mutation returns | `example: events: TestEvent_Create` | all |  |
| event.op.create | `Create` publishes one `Create` event for the created entity | `example: events: TestEvent_Create` | all |  |
| event.op.create-many | `CreateMany` publishes one `Create` event per entity in the batch | `example: events: TestEvent_CreateMany`; `example: events: TestEventBatch_CreateMany_PerEventActionAndTable`; `example: events: TestEventBatch_CreateMany_DistinctPKs` | all |  |
| event.op.update | `Update` publishes one `Update` event for the updated entity | `example: events: TestEvent_Update` | all |  |
| event.op.update-many | `UpdateMany` publishes one `Update` event per item that wrote a row | `example: events: TestEvent_UpdateMany`; `example: events: TestEvent_BatchPublishesOnlyWrittenKeys` | all |  |
| event.op.update-where | `UpdateWhere` publishes one `Update` event per affected row | `example: events: TestEvent_UpdateWhere`; `example: events: TestWhereMutation_UpdateWhere_NRowsNEvents` | all |  |
| event.op.upsert | `Upsert` publishes one `Upsert` event for the upserted entity, on insert and on update | `example: events: TestEvent_Upsert` | all |  |
| event.op.soft-delete | `SoftDelete` publishes one `Delete` event for the deleted entity | `example: events: TestEvent_SoftDelete` | all |  |
| event.op.soft-delete-many | `SoftDeleteMany` publishes one `Delete` event per entity in the batch | `example: events: TestEvent_SoftDeleteMany`; `example: events: TestEventBatch_SoftDeleteMany_OrderMatchesInsertion` | all |  |
| event.op.soft-delete-where | `SoftDeleteWhere` publishes one `Delete` event per affected row | `example: events: TestEvent_SoftDeleteWhere` | all |  |
| event.op.hard-delete | `HardDelete` publishes one `Delete` event for the deleted entity | `example: events: TestEvent_HardDelete` | all |  |
| event.op.hard-delete-many | `HardDeleteMany` publishes one `Delete` event per entity in the batch | `example: events: TestEvent_HardDeleteMany`; `example: events: TestEventInput_HardDeleteMany` | all |  |
| event.op.hard-delete-where | `HardDeleteWhere` publishes one `Delete` event per affected row | `example: events: TestEvent_HardDeleteWhere` | all |  |
| event.op.restore | `Restore` publishes one `Update` event for the restored entity | `example: events: TestEvent_Restore` | all |  |
| event.op.restore-many | `RestoreMany` publishes one `Update` event per entity in the batch | `example: events: TestEvent_RestoreMany` | all |  |
| event.op.restore-where | `RestoreWhere` publishes one `Update` event per affected row | `example: events: TestEvent_RestoreWhere`; `example: events: TestWhereMutation_RestoreWhere_InputIsFilter` | all |  |
| event.op.increment | `Increment` publishes one `Update` event for the incremented entity | `example: events: TestEvent_Increment` | all |  |
| event.batch.one-publish-batch | A batch mutation hands all of its events to one `PublishBatch` call: `CreateMany`, `UpdateMany`, `UpdateWhere` and `HardDeleteMany` alike | `example: events: TestEvent_BatchMutationPublishesOneBatch` | all |  |
| event.pk.fire-and-forget | A fire-and-forget `Create` (empty `FieldOptions`) on a database-generated PK returns no entity, and its event carries the generated PK | `example: events: TestEventConfig_FireAndForgetCreateCarriesGeneratedPK` | all |  |
| event.skip-events | With `SkipEvents: true` a mutation publishes nothing, inside a transaction or outside one, while an unskipped mutation in the same scope still publishes | `example: events: TestEventConfig_SkipEvents`; `example: events: TestEventConfig_SkipEventsInsideTransaction` | all |  |

## §28.5 Subscriber Interface
<!-- prd: 28.5 sha256: 3b4776e87504ac22 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| event.subscriber.interface | `Subscriber` has exactly `Subscribe(SubscribeOptions, Handler) (Subscription, error)` and `Close() error`, `Subscription` has exactly `Unsubscribe() error`, and `Handler` takes `(context.Context, Event)` and returns `error` | `unit: event: TestInterfaceSignatures` | all |  |
| event.subscribe-options.fields | `SubscribeOptions` has exactly `Tables []string`, `Actions []Action` and `Group string` | `unit: event: TestSubscribeOptionsFields` | all |  |
| event.subscribe.table-filter | A `Tables` filter delivers events of any listed table and no other, on every provided transport | `unit: event/memorybus: TestFilters/table_filter_only`; `unit: event/memorybus: TestFilters/two_tables_and_two_actions`; `unit: event/natsbus: TestTableAndActionFilter`; `unit: event/natsbus: TestMultiTableAndActionFilter/core`; `integration: event/natsbus: TestMultiTableAndActionFilter/jetstream`; `unit: event/natsbus: TestSchemalessEventReachesSingleTableSubscriber`; `integration: event/natsbus: TestJetStreamSchemalessEventReachesSingleTableSubscriber` | all |  |
| event.subscribe.action-filter | An `Actions` filter delivers events of any listed action and no other, on every provided transport | `unit: event/memorybus: TestFilters/action_filter_only`; `unit: event/memorybus: TestFilters/two_tables_and_two_actions`; `unit: event/natsbus: TestTableAndActionFilter`; `unit: event/natsbus: TestMultiTableAndActionFilter/core`; `integration: event/natsbus: TestMultiTableAndActionFilter/jetstream`; `integration: event/natsbus: TestJetStreamFilteredOutAcked` | all |  |
| event.subscribe.empty-filter-all | Empty `Tables` and `Actions` deliver every table and action | `unit: event/memorybus: TestFilters/empty_filter_receives_all`; `unit: event/natsbus: TestPublishBatch`; `integration: event/natsbus: TestJetStreamDeliversInStreamOrder` | all |  |
| event.subscribe.group-exactly-one | With `Group` set, each event reaches exactly one member of the group, members share the load, and each group receives every event | `unit: event/memorybus: TestConsumerGroupRoundRobin`; `unit: event/memorybus: TestConsumerGroupIndependent`; `unit: event/natsbus: TestQueueGroupDistributes`; `unit: event/natsbus: TestQueueGroupsIndependent` | all |  |
| event.subscribe.broadcast | With `Group` empty, every subscriber receives every event | `unit: event/memorybus: TestConsumerGroupBroadcastWhenEmpty`; `unit: event/natsbus: TestBroadcastWhenNoGroup` | all |  |
| event.unsubscribe | After `Unsubscribe` a subscription receives no further event, and a second `Unsubscribe` returns nil | `unit: event/memorybus: TestUnsubscribe`; `unit: event/natsbus: TestUnsubscribeStopsDelivery`; `unit: event/natsbus: TestUnsubscribeReleasesEverySubject`; `integration: event/natsbus: TestJetStreamUnsubscribeStopsDelivery` | all |  |
| event.handler-error.never-silent | With no error handler configured, a provided transport logs a `Handler`'s error through the standard logger | `unit: event/memorybus: TestHandlerErrorLoggedWithoutErrorHandler`; `unit: event/natsbus: TestHandlerErrorLoggedWithoutErrorHandler`; `integration: event/natsbus: TestJetStreamHandlerErrorLoggedWithoutErrorHandler` | all |  |

## §28.6 Event Input
<!-- prd: 28.6 sha256: a39a654f2f93f9e8 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| event.input.single-op-same-pointer | On a table with no redacted column, `Create`, `Update` and `Upsert` publish the caller's input pointer as `Event.Input` | `example: events: TestEvent_Create`; `example: events: TestEvent_Update`; `example: events: TestEventInput_Upsert`; `example: events: TestEventRedaction_UnredactedTableKeepsSamePointer` | all |  |
| event.input.increment-by-value | `Increment` publishes its `IncrementInput[<Struct>IncrementColumn]` by value | `example: events: TestEventInput_Increment` | all |  |
| event.input.create-many-ith | Each `CreateMany` event carries the i-th `*Create<Struct>Input` of the caller's slice | `example: events: TestEventInput_CreateMany` | all |  |
| event.input.update-many-ith-written | Each `UpdateMany` event carries the i-th written `Update<Struct>Item`: items whose key matched no row are left out | `example: events: TestEventInput_UpdateMany`; `example: events: TestEvent_BatchPublishesOnlyWrittenKeys` | all |  |
| event.input.delete-restore-nil | `HardDelete`, `SoftDelete`, `Restore` and their `*Many` variants publish a nil `Event.Input`, the PK on `Event.PK` | `example: events: TestEventInput_SoftDelete`; `example: events: TestEventInput_HardDelete`; `example: events: TestEventInput_SoftDeleteMany`; `example: events: TestEventInput_HardDeleteMany`; `example: events: TestEvent_Restore`; `example: events: TestEvent_RestoreMany` | all |  |
| event.input.update-where-shared | Every `UpdateWhere` event carries the caller's `*Update<Struct>Input` | `example: events: TestWhereMutation_UpdateWhere_NRowsNEvents` | all |  |
| event.input.where-filter-shared | Every `HardDeleteWhere`, `SoftDeleteWhere` and `RestoreWhere` event carries the caller's `*<Struct>Filter` on a table with no redacted column | `example: events: TestWhereMutation_SoftDeleteWhere_InputIsFilter`; `example: events: TestWhereMutation_HardDeleteWhere_InputIsFilter`; `example: events: TestWhereMutation_RestoreWhere_InputIsFilter` | all |  |

## §28.7 Transport Implementations
<!-- prd: 28.7 sha256: c2d6c7dd3edb105e -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| transport.memorybus.at-most-once | memorybus delivers each event to a subscriber once and never redelivers, a failing `Handler` included | `unit: event/memorybus: TestDefaultFailingHandlerNoPanicNoRedeliver`; `unit: event/memorybus: TestSubscribeErrorHandlerRoutesError` | all | transports: memorybus × delivery |
| transport.memorybus.ordered | memorybus delivers events to a subscriber in publish order | `unit: event/memorybus: TestPublishOrder` | all | transports: memorybus × ordering |
| transport.memorybus.no-persistence | memorybus keeps no event: a subscriber added after a publish never receives it | `unit: event/memorybus: TestNoReplayForLateSubscriber` | all | transports: memorybus × persistence |
| transport.memorybus.handler-error | A memorybus `Handler` error goes, unchanged, to the `WithSubscribeErrorHandler` handler with its event, once | `unit: event/memorybus: TestSubscribeErrorHandlerRoutesError` | all | transports: memorybus × handler-error |
| transport.nats-core.at-most-once | Core natsbus delivers each event to a subscriber once and never redelivers, a failing `Handler` included | `unit: event/natsbus: TestDefaultFailingHandlerNoPanicNoRedeliver` | all | transports: nats-core × delivery |
| transport.nats-core.ordered | Core natsbus delivers one subject's events to a subscriber in publish order | `unit: event/natsbus: TestPublishOrderPerSubject` | all | transports: nats-core × ordering |
| transport.nats-core.no-persistence | A core-mode bus provisions no stream, even on a JetStream-enabled server | `integration: event/natsbus: TestJetStreamCoreModeCreatesNoStream` | all | transports: nats-core × persistence |
| transport.nats-core.handler-error | A core natsbus `Handler` error goes, unchanged, to the `WithSubscribeErrorHandler` handler with its event, once, after any `WithRetry` attempts | `unit: event/natsbus: TestSubscribeErrorHandlerRoutesError`; `unit: event/natsbus: TestRetry` | all | transports: nats-core × handler-error |
| transport.nats-jetstream.at-least-once | A JetStream `SubscribeWith` consumer acks a message its `Handler` accepted, which is then not redelivered, and redelivers one its `Handler` failed | `integration: event/natsbus: TestJetStreamAcksOnSuccess`; `integration: event/natsbus: TestJetStreamRedeliversOnError` | all | transports: nats-jetstream × delivery |
| transport.nats-jetstream.ordered | A JetStream consumer receives the stream's messages in stream order, across tables | `integration: event/natsbus: TestJetStreamDeliversInStreamOrder` | all | transports: nats-jetstream × ordering |
| transport.nats-jetstream.persisted | JetStream mode stores each published event in the stream, and a consumer created later replays it | `integration: event/natsbus: TestJetStreamPublishStoresMessage`; `integration: event/natsbus: TestJetStreamDeliverAllReplaysHistory` | all | transports: nats-jetstream × persistence |
| transport.nats-jetstream.handler-error | A JetStream `Handler` error naks the message, and on the consumer's final permitted delivery, not before, routes it to the dead-letter subject, once | `integration: event/natsbus: TestJetStreamDeadLetterAfterMaxDeliver` | all | transports: nats-jetstream × handler-error |
| memorybus.stdlib-only | memorybus imports only `event` and the standard library | `unit: event: TestEventPackagesImportOnlyStdlib/memorybus` | all |  |
| memorybus.synchronous | memorybus `Publish` returns after every matching `Handler` has run | `unit: event/memorybus: TestPublishSubscribe`; `unit: event/memorybus: TestSubscribeErrorHandlerRoutesError` | all |  |
| memorybus.concurrent-safe | Concurrent `Publish`, `Subscribe` and `Unsubscribe` on one memorybus are race-free and lose no delivery to a standing subscriber | `unit: event/memorybus: TestConcurrentPublishSubscribe` | all |  |
| natsbus.implements-both | `*natsbus.Bus` satisfies `event.Publisher` and `event.Subscriber`, and an event it publishes reaches its own subscriber unchanged | `unit: event/natsbus: TestSubscribeInterfaceNarrowing`; `unit: event/natsbus: TestPublishSubscribe` | all |  |
| natsbus.subject | natsbus publishes to `{prefix}.{schema}.{table}`, or `{prefix}.{table}` for an event with no schema; the prefix defaults to `sqlgen.events` and `WithPrefix` replaces it | `unit: event/natsbus: TestSubjectPattern` | all |  |
| natsbus.single-table-both-forms | A single-table subscription receives the table's events with and without a schema, each once, in core mode, in a queue group, with action subjects and on a JetStream consumer | `unit: event/natsbus: TestSchemalessEventReachesSingleTableSubscriber`; `integration: event/natsbus: TestJetStreamSchemalessEventReachesSingleTableSubscriber` | all |  |
| natsbus.core-default | `natsbus.New(conn)` with no option is core mode | `integration: event/natsbus: TestJetStreamCoreModeCreatesNoStream`; `unit: event/natsbus: TestPublishStampsEnvelopeVersion` | all |  |
| natsbus.jetstream.stream | `WithJetStream(StreamConfig{Name, MaxAge, DedupWindow})` provisions the named stream over `{prefix}.>` with that max age and dedup window | `integration: event/natsbus: TestJetStreamStreamConfig` | all |  |
| natsbus.jetstream.nak-backoff | A failed JetStream delivery is redelivered no sooner than the `WithBackoff` delay | `integration: event/natsbus: TestJetStreamNakBackoffDelaysRedelivery` | all |  |
| natsbus.jetstream.plain-subscribe-core | In JetStream mode the plain `Subscribe` creates no consumer, receives live events and does not redeliver a failed one | `integration: event/natsbus: TestJetStreamPlainSubscribeIsCoreTap` | all |  |
| natsbus.jetstream.msg-id-dedup | In JetStream mode each message's `Nats-Msg-Id` header is `Event.ID`, and two publishes of one ID inside the dedup window store one message | `integration: event/natsbus: TestJetStreamPublishStoresMessage`; `integration: event/natsbus: TestJetStreamDedupWindow` | all |  |
| natsbus.jetstream.replay | A new durable consumer replays the stream from the start, from a start time, or from a start sequence | `integration: event/natsbus: TestJetStreamDeliverAllReplaysHistory`; `integration: event/natsbus: TestJetStreamDeliverFromTime`; `integration: event/natsbus: TestJetStreamDeliverFromSequence` | all |  |
| natsbus.subscribe-with-separate | The consumer knobs live on `SubscribeWith`, not on `event.SubscribeOptions`, and a bus held as `event.Subscriber` keeps core-filter semantics | `unit: event/natsbus: TestSubscribeInterfaceNarrowing`; `unit: event: TestSubscribeOptionsFields` | all |  |
| natsbus.trace.header | `Event.Metadata["traceparent"]` is published as the `traceparent` header, in core and JetStream mode | `unit: event/natsbus: TestContextPropagationHeaderOnWire`; `integration: event/natsbus: TestJetStreamPublishStoresMessage` | all |  |
| natsbus.trace.extract | With `WithContextPropagation`, a `Handler`'s ctx carries the trace the header delivered, core and JetStream | `unit: event/natsbus: TestContextPropagationContinuesSpan`; `integration: event/natsbus: TestJetStreamContextPropagation` | all |  |
| natsbus.no-tracing-dependency | Neither natsbus nor `event` imports an OpenTelemetry package | `unit: event/natsbus: TestNoTracingDependency` | all |  |
| natsbus.envelope-version | Every message carries a `Sqlgen-Envelope-Version` header, in core and JetStream mode, and `WithActionSubjects`, a different subject scheme, stamps a different version | `unit: event/natsbus: TestPublishStampsEnvelopeVersion`; `integration: event/natsbus: TestJetStreamPublishStoresMessage`; `unit: event/natsbus: TestActionSubjectsPublish` | all |  |
| natsbus.unknown-headers-ignored | A subscriber decodes a message with unknown headers, or with none | `unit: event/natsbus: TestSubscribeHeaderTolerance` | all |  |
| natsbus.connect | `Connect(url, ...ConnectOption)` dials with unbounded reconnect, a reconnect buffer, a bounded drain on close and disconnect and reconnect callbacks, and returns a bus that publishes and pairs with `WithJetStream` | `unit: event/natsbus: TestConnectDialOptions`; `unit: event/natsbus: TestConnectPublishSubscribe`; `integration: event/natsbus: TestConnectPairsWithJetStream` | all |  |
| natsbus.close.connect-drains | A `Connect` bus's `Close` closes the connection it dialed | `unit: event/natsbus: TestConnectCloseClosesConn` | all |  |
| natsbus.close.new-leaves-conn | A `New(conn)` bus's `Close` unsubscribes the bus's handlers and leaves the connection open and usable, even against a racing `Subscribe` | `unit: event/natsbus: TestCloseLeavesCallerConnOpen`; `unit: event/natsbus: TestSubscribeRacingCloseLeavesNothingLive` | all |  |
| natsbus.subscribe-path-errors | The subscribe error handler, or the log, receives an undecodable message with the zero `Event` and the decode error, core and JetStream; a failed dead-letter publish after the `Handler`'s own error; and a failed JetStream consumer with the zero `Event`; the undecodable JetStream message is not redelivered | `unit: event/natsbus: TestUndecodableBodyReported`; `integration: event/natsbus: TestJetStreamUndecodableBodyReported`; `integration: event/natsbus: TestJetStreamFailedDeadLetterPublishReported`; `integration: event/natsbus: TestJetStreamConsumerFailureReported` | all |  |
| natsbus.action-subjects | `WithActionSubjects` publishes to `{prefix}.{schema}.{table}.{action}`, or `{prefix}.{table}.{action}` for an event with no schema | `unit: event/natsbus: TestActionSubjectsPublish`; `unit: event/natsbus: TestActionSubjectsSchemalessPublish` | all |  |

## §28.11 Cache Invalidation via Events
<!-- prd: 28.11 sha256: c800439234657f37 -->

The adapter's translation of an event into a signal (one signal per event, the table name, the
tenant stamp, subscribe-all, error routing) has rows in [cache.md](cache.md) §27.9
(`cache.adapter.*`). The rows here cover what §28.11 adds: the receiving cache's handling of a
tenanted table's signal.

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| event.cache-receive.tenant-stamp-precise | A tenant-stamped event for a tenanted table evicts the receiving cache's entry for that row and tenant, leaves another tenant's entry cached, and takes no table-wide fallback | `example: tenancy: TestEventDrivenInvalidation_TenantedPreciseReceive` | all |  |
| event.cache-receive.tenant-in-pk-precise | On a tenant-in-PK table an event with no stamp evicts the receiving cache's entry for the PK's tenant only, without the table-wide fallback | `example: tenancy: TestEventDrivenInvalidation_TenantInPKPreciseWithoutStamp` | all |  |
| event.cache-receive.tenant-less-fallback | A tenant-less event for any other tenanted table clears the table across tenants and returns no error | `example: tenancy: TestEventDrivenInvalidation_TenantAbsentFallsBackToTablePattern`; `example: tenancy: TestEventDrivenInvalidation_HandlerReturnsNoError` | all |  |

## §29.6 Event Metadata
<!-- prd: 29.6 sha256: 451a081517626c58 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| tenant-event.every-tenanted-event | Every event of a tenanted table carries `Metadata["tenant"]`, for create, update and delete | `example: tenancy: TestEvent_TenantedMutationCarriesTenantMetadata`; `example: tenancy: TestEventMetadata_TenantedActions_CarryTenant` | all |  |
| tenant-event.stamp-row-derived | On a tenanted table whose tenant column is outside the PK, each fanned-out event's `Metadata["tenant"]` is read from its own row's element of `MutationContext.AffectedTenants`, not from the resolver | `unit: cmd/sqlgen/gen: TestEventHooks_tenantedTable_emitsTenantMetadata`; `example: tenancy: TestEventTenantStamp_SkipTenancyManyStampsPerRowTenants` | all |  |
| tenant-event.stamp-from-pk | When the tenant column is part of the primary key, the event's tenant stamp is read from the affected PK value (the PK struct's tenant field, or the PK itself when the tenant is the sole PK), and `AffectedTenants` is not read | `unit: cmd/sqlgen/gen: TestEventHooks_tenantInPK_stampsFromPKStruct`; `unit: cmd/sqlgen/gen: TestEventHooks_tenantIsSolePK_stampsFromPK`; `example: tenancy: TestEventTenantStamp_TenantInPKStampsFromPKStruct` | all |  |
| tenant-event.skip-tenancy-stamped | `SkipTenancy` mutations publish events stamped with the row's tenant, not the resolver's, across create, update and delete | `example: tenancy: TestEvent_SkipTenancyStampsRowTenant`; `example: tenancy: TestEventMetadata_SkipTenancy_AcrossActions` | all |  |
| tenant-event.required-false-zero-stamped | A `required: false` table's mutation under a zero tenant publishes an event stamped with the row's tenant | `example: tenancy: TestEventTenantStamp_RequiredFalseZeroTenantStampsRowOrg` | all |  |
| tenant-event.unmatched-key-no-event | A listed key that matched no row publishes no event, while the batch's matched rows publish stamped events | `example: tenancy: TestEventTenantStamp_UnmaterializedRowPublishesNothing` | all |  |
| tenant-event.stamp-percent-v | The tenant stamp is formatted with `%v` | `unit: cmd/sqlgen/gen: TestEventHooks_tenantedTable_emitsTenantMetadata`; `unit: cmd/sqlgen/gen: TestEventHooks_tenantInPK_stampsFromPKStruct`; `example: tenancy: TestEvent_TenantedMutationCarriesTenantMetadata` | all |  |
| tenant-event.uncaptured-key-absent | An event whose row tenant was not captured (no element, or a nil element) carries no `"tenant"` key. No generated operation leaves a tenanted row uncaptured, so only the template proves this | `unit: cmd/sqlgen/gen: TestEventHooks_tenantedTable_emitsTenantMetadata` | all |  |
| tenant-event.stamp-before-on-commit | The tenant stamp is computed before the publish closure handed to `OnCommit` is built | `unit: cmd/sqlgen/gen: TestEventHooks_tenantedTable_emitsTenantMetadata` | all |  |

## Unclaimed

| Unclaimed test | Reason |
|---|---|
| event: TestEventConstruction | Assigns each `Event` field and reads it back; Go guarantees that. The field types are claimed by `event.struct.fields` |
| event: TestEventZeroValue | Checks that each field of a zero `Event` is its type's zero value, a Go guarantee; no PRD behavior |
| event: TestConfigResolveOnError_Default | Checks only that the resolved default is non-nil and accepts a nil error; the default's behavior is claimed in cli/config-features.md by `config.event-config.on-error-default` |
| event/natsbus: TestNoPropagatorDefaultCtx | Checks that a ctx value only the test's own propagator sets is absent without a propagator; natsbus cannot set that key, so the check cannot fail, and the PRD's context-less default is not observable through it |
| event/natsbus: TestCloseUnsubscribeConcurrentNoRace | Races `Close` against `Unsubscribe` under `-race`; the PRD makes no concurrency promise for natsbus |

## PRD gaps

| Gap | Behavior | Tests | Section | Ruling |
|---|---|---|---|---|
| gap.event.option-names | The PRD describes these natsbus options without naming them: `DefaultPrefix`, `WithPrefix`, `Propagator`, `WithDurable`, `WithBackoff`, `WithMaxDeliver`, `WithDeadLetter`, `WithDeliverAll`, `WithDeliverFromTime` and `WithDeliverFromSequence`. Also unnamed: `event.DefaultOnError`, and memorybus's `Option` and `WithSubscribeErrorHandler` | `unit: event/memorybus: TestSubscribeErrorHandlerRoutesError` | §28.7, §28.8 |  |
| gap.event.close-after-close | After `Close`, natsbus `Publish` and `Subscribe` return errors and a second `Close` is a no-op; memorybus `Publish` and `PublishBatch` return nil and deliver nothing | `unit: event/natsbus: TestCloseRejectsPublishAndSubscribe`; `unit: event/memorybus: TestClose` | §28.4, §28.5 |  |
| gap.natsbus.provisioning-error-deferred | A failed `WithJetStream` stream provisioning is returned from the first `Publish` and `Subscribe`, not from `New` | `integration: event/natsbus: TestJetStreamProvisioningFailureSurfaced` | §28.7 |  |
| gap.natsbus.connect-dial-error | `Connect` to an unreachable server returns the dial error | `unit: event/natsbus: TestConnectDialFailure` | §28.7 |  |
| gap.natsbus.jetstream-terminal-actions | A JetStream message that cannot be decoded is terminated and one that fails the filter is acked (both settle without redelivery; the tests cannot tell the two apart), and with `WithMaxDeliver` but no `WithDeadLetter` delivery stops at the maximum | `integration: event/natsbus: TestJetStreamUndecodableBodyTerminated`; `integration: event/natsbus: TestJetStreamFilteredOutAcked`; `integration: event/natsbus: TestJetStreamMaxDeliverWithoutDeadLetter` | §28.7 |  |
| gap.natsbus.dead-letter-envelope | The dead-lettered message keeps the original body and headers (`Nats-Msg-Id`, `traceparent`) | `integration: event/natsbus: TestJetStreamDeadLetterAfterMaxDeliver` | §28.7 |  |
| gap.natsbus.action-subjects-subscribe | On a `WithActionSubjects` bus, a single-table, single-action subscription listens on the action-qualified subject | `unit: event/natsbus: TestActionSubjectsBrokerFilter` | §28.7 |  |
| gap.natsbus.subscribe-with-core-mode | Without `WithJetStream`, `SubscribeWith` ignores its options and is a core subscription | `unit: event/natsbus: TestSubscribeInterfaceNarrowing` | §28.7 |  |
| gap.natsbus.group-as-durable | Without `WithDurable`, `SubscribeWith` uses `Group` as the durable consumer name, and an empty group gives an ephemeral consumer | | §28.7 |  |
| gap.natsbus.jetstream-error-handler | The subscribe error handler also sees a failed JetStream attempt's `Handler` error, before the nak or dead-letter routing | `integration: event/natsbus: TestJetStreamFailedDeadLetterPublishReported` | §28.5, §28.7 |  |
| gap.natsbus.handler-not-concurrent | A single-table subscription's `Handler` never runs concurrently with itself, though it listens on two subjects | `unit: event/natsbus: TestSingleTableHandlerNotConcurrent` | §28.7 |  |
| gap.event.on-error-panic | `OnError` is called once per batch, outside the publish recover, so a panic `OnError` raises itself is not reported back to it and propagates; where it ends (the client's panic recovery outside a transaction, the commit goroutine under async commit) is not pinned. The cache's error policy recovers a panicking `OnErrorFunc` instead (cache.md `cache.error-policy.logger-cannot-mask`) | `example: events: TestEventConfig_OnErrorCalledOnceWhenItPanics` | §28.8 |  |
