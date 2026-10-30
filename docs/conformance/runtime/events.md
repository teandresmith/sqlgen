# Runtime Requirements: Events

<!-- seeding: in-progress -->
<!-- packages: event event/memorybus event/natsbus -->

The event system (§28) and its tenant metadata (§29.6). See [README.md](../README.md) for the
format.

## §29.6 Event Metadata
<!-- prd: 29.6 sha256: 451a081517626c58 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| tenant-event.stamp-row-derived | On a tenanted table whose tenant column is outside the PK, each fanned-out event's `Metadata["tenant"]` is read from its own row's element of `MutationContext.AffectedTenants`, not from the resolver | `unit: cmd/sqlgen/gen: TestEventHooks_tenantedTable_emitsTenantMetadata`; `example: tenancy: TestEventTenantStamp_SkipTenancyManyStampsPerRowTenants` | all |  |
| tenant-event.stamp-from-pk | When the tenant column is part of the primary key, the event's tenant stamp is read from the affected PK value (the PK struct's tenant field, or the PK itself when the tenant is the sole PK), and `AffectedTenants` is not read | `unit: cmd/sqlgen/gen: TestEventHooks_tenantInPK_stampsFromPKStruct`; `unit: cmd/sqlgen/gen: TestEventHooks_tenantIsSolePK_stampsFromPK`; `example: tenancy: TestEventTenantStamp_TenantInPKStampsFromPKStruct` | all |  |
| tenant-event.stamp-percent-v | The tenant stamp is formatted with `%v` | `unit: cmd/sqlgen/gen: TestEventHooks_tenantedTable_emitsTenantMetadata`; `unit: cmd/sqlgen/gen: TestEventHooks_tenantInPK_stampsFromPKStruct` | all |  |
| tenant-event.uncaptured-key-absent | An event whose row tenant was not captured (no element, or a nil element) carries no `"tenant"` key | `unit: cmd/sqlgen/gen: TestEventHooks_tenantedTable_emitsTenantMetadata` | all |  |
| tenant-event.stamp-before-on-commit | The tenant stamp is computed before the publish closure handed to `OnCommit` is built | `unit: cmd/sqlgen/gen: TestEventHooks_tenantedTable_emitsTenantMetadata` | all |  |
