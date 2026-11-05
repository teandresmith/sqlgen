# Runtime Requirements: Support Packages

<!-- seeding: in-progress -->
<!-- packages: types manifest tenancy -->

The runtime support packages: `types` (the §7 type integrations), `manifest` (the runtime-embedded
manifest, §30.6) and `tenancy` (§29.3). See [README.md](../README.md) for the format.

`types` backs §7, which `cli/gotype.md` owns: its tests are cited there, and the `types` behavior no
PRD text states is the `gap.types.datetime-surface` entry below.

## §29.3.1 TenantResolver
<!-- prd: 29.3.1 sha256: da9765396bb05c1c -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| tenancy.resolver-shape | `tenancy.TenantResolver[T]` is `func(ctx context.Context) (T, error)` | `unit: tenancy: TestTenantResolverInstantiations` | all |  |
| tenancy.err-missing-text | `tenancy.ErrMissing`'s text is `tenancy: tenant missing from context` | `unit: tenancy: TestErrMissing` | all |  |
| tenancy.err-mismatch-text | `tenancy.ErrMismatch`'s text is `tenancy: tenant on mutation input does not match resolved tenant; use CallOptions.SkipTenancy to override` | `unit: tenancy: TestErrMismatch` | all |  |
| tenancy.resolver-concrete-type | With tenancy enabled, the unified client emits `WithTenantResolver(r tenancy.TenantResolver[T]) ClientOption` and holds a concrete `tenantResolver tenancy.TenantResolver[T]` field, where `T` is the resolved Go type of the tenant column | `unit: cmd/sqlgen/gen: TestBuildClientContext_tenancyEnabledProducesResolverWiring`; `unit: cmd/sqlgen/gen: TestTenancyTemplate_clientRawHasBypassComment`; `example: tenancy: TestWithTenantResolver_TakesTheTenantColumnType`; `example: tenancy_mysql: TestWithTenantResolver_TakesTheTenantColumnType` | all |  |

The `comparable` constraint is not rowed: only a test that fails to build could show a
non-comparable `T` is rejected. The zero-value sentence ("Returning the zero value for T with required: true causes every
tenanted operation to error with ErrMissing before hitting the DB") is
`config.tenancy.required.true-fails-closed` in `cli/config-features.md`.

## §29.3.2 Wiring into the client
<!-- prd: 29.3.2 sha256: d05bad77730a7633 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| tenancy.wiring-composes | `WithTenantResolver` composes with `WithEventPublisher` and `WithCache` in one `New` call: the resolved tenant scopes the call, the publisher emits the mutation's event stamped with that tenant, and the cache serves the repeated read | `example: tenancy: TestResolverWiring_ComposesWithEventsAndCache` | all |  |
| tenancy.wiring-request-scoped | One long-lived client serves every tenant: the resolver reads the tenant from each call's `ctx`, so calls on one client with two tenants' contexts each reach only their own tenant's rows | `example: tenancy: TestIsolation_TenantValueViaCtxResolver` | all |  |
| tenancy.wiring-resolver-error | The resolver's error is returned from the operation, matchable with `errors.Is`, so the example's `return uuid.UUID{}, tenancy.ErrMissing` fails the call with `tenancy.ErrMissing` | `example: tenancy: TestRequiredFalse_ResolverError_Propagates` | all |  |

## §29.3.3 Type-safe by construction
<!-- prd: 29.3.3 sha256: 77c75e30ea152593 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| tenancy.typed-by-construction | The resolver returns, the model's tenant field holds and the create input's tenant field carries one concrete `T`, so a resolver returning any other type does not compile | `example: tenancy: TestWithTenantResolver_TakesTheTenantColumnType`; `example: tenancy_mysql: TestWithTenantResolver_TakesTheTenantColumnType` | all |  |

The "Reads / mutations" and "Mismatch check" bullets describe the generated code's shape (no type
assertion, `==` with no reflection) and are informative; their behavior is the §29.4 rows in
`client.md`.

## §30.6 Runtime-Embedded Manifest
<!-- prd: 30.6 sha256: 8014344072762285 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest-embed.method-signatures | The methods are `Manifest() *manifest.Document`, `ManifestEntity(table string) (*manifest.Entity, error)` and `ManifestVersion() string`, and the client has no `ManifestRaw` | `example: postgres: TestManifest_MethodSignatures`; `example: mysql: TestManifest_MethodSignatures`; `example: sqlite: TestManifest_MethodSignatures`; `example: graphql: TestManifest_MethodSignatures` | all |  |
| manifest-embed.concurrent-reads | `Manifest()`, `ManifestEntity` and `ManifestVersion` are safe for concurrent reads | `example: sqlite: TestManifest_ConcurrentReads`; `example: graphql: TestManifest_ConcurrentReads` | all |  |
| manifest-embed.document-index | `Manifest().Entities` is the lightweight index in both layouts: name, table and kind, plus the schema when the entity has one and the file in `per_entity` | `unit: manifest: TestEntityIndex_DecodesSchema`; `unit: manifest: TestLoadInto_SingleLayout`; `unit: manifest: TestLoadInto_PerEntityLayout`; `example: postgres: TestManifest_MarshalIsContentEquivalent`; `example: graphql: TestManifest_MarshalIsContentEquivalent` | all |  |
| manifest-embed.entity-full-record | `ManifestEntity` returns the entity's full record, in both layouts | `unit: manifest: TestLoadInto_SingleLayout`; `unit: manifest: TestLoadInto_PerEntityLayout`; `example: postgres: TestManifest_EveryEntityRecordIsReachable`; `example: mysql: TestManifest_EveryEntityRecordIsReachable`; `example: sqlite: TestManifest_EveryEntityRecordIsReachable`; `example: graphql: TestManifest_EveryEntityRecordIsReachable` | all |  |
| manifest-embed.lookup-qualified | A table with a schema is found by `schema.table`, and a table without one by its name | `unit: manifest: TestFindEntity/qualified name`; `unit: manifest: TestFindEntity/the other schema's qualified name`; `unit: manifest: TestFindEntity/schema-less entity`; `unit: manifest: TestLoadInto_KeysSchemasApart`; `example: postgres: TestManifest_EveryEntityRecordIsReachable`; `example: sqlite: TestManifest_EveryEntityRecordIsReachable` | all |  |
| manifest-embed.lookup-bare-unique | A table with a schema is also found by its bare name when no other schema has a table of that name | `unit: manifest: TestFindEntity/bare table no other schema shares`; `example: graphql: TestManifest_EveryEntityRecordIsReachable`; `example: postgres: TestManifest_EveryEntityRecordIsReachable` | postgres |  |
| manifest-embed.lookup-bare-shared | A bare name several schemas share returns nil and an error wrapping `manifest.ErrEntityNotFound` that names each `schema.table` it could mean | `unit: manifest: TestFindEntity/bare table two schemas share`; `example: postgres: TestManifest_EveryEntityRecordIsReachable` | postgres |  |
| manifest-embed.lookup-unknown | An unknown table returns `(nil, manifest.ErrEntityNotFound)`, the sentinel itself | `unit: manifest: TestFindEntity/unknown table`; `unit: manifest: TestFindEntity/unknown schema`; `unit: manifest: TestFindEntity/schema-less entity qualified`; `example: postgres: TestManifest_EntityLookup`; `example: mysql: TestManifest_EntityLookup`; `example: sqlite: TestManifest_EntityLookup`; `example: graphql: TestManifest_EntityLookup` | all |  |
| manifest-embed.version | `ManifestVersion()` returns the manifest's `schema_version` | `unit: manifest: TestLoadInto_VersionExtraction`; `example: postgres: TestManifest_VersionAgreement`; `example: mysql: TestManifest_VersionAgreement`; `example: sqlite: TestManifest_VersionAgreement`; `example: graphql: TestManifest_VersionAgreement` | all |  |
| manifest-embed.stdlib-only | The `manifest` package imports only the standard library | `unit: manifest: TestManifestPackage_StdlibOnly` | all |  |
| manifest-embed.exports | The package exports `Document`, `EntityIndex`, `Entity`, `Column`, `Relationship`, `Method`, `Filter`, `Sort`, `Features`, `PK`, `Conventions`, `Generator`, `Enum`, `Extra` and the typed aliases `Layout`, `Dialect`, `EntityKind`, `RelationshipKind` | `unit: manifest: TestPackage_ExportsTheDocumentedTypes` | all |  |
| manifest-embed.decode-target | The same types are the JSON unmarshal target: the generator's top-level fields and entity records decode with no field lost | `unit: manifest: TestGenerationConfig_Decode`; `unit: manifest: TestMethod_SQLBodiesDecode`; `example: postgres: TestManifest_EveryEntityRecordIsReachable`; `example: graphql: TestManifest_EveryEntityRecordIsReachable`; `example: postgres: TestManifest_MarshalIsContentEquivalent`; `example: graphql: TestManifest_MarshalIsContentEquivalent` | all |  |
| manifest-embed.sentinels | `ErrEntityNotFound`'s text is `manifest: entity not found` and `ErrParseManifest`'s is `manifest: parse failure (corrupt embed)` | `unit: manifest: TestSentinelErrors` | all |  |
| manifest-embed.parse-failure | A corrupt embed, top-level JSON or an entity file that does not parse, fails `LoadInto` with an error matching `ErrParseManifest` | `unit: manifest: TestLoadInto_ErrorWrapping` | all |  |
| manifest-embed.embed-directives | `manifest_embed_gen.go` always embeds `manifest/manifest_gen.json`, and embeds `all:manifest/entities` only in the `per_entity` layout | `unit: cmd/sqlgen/gen: TestBuildManifestEmbedContext_Paths`; `unit: cmd/sqlgen/gen: TestManifestEmbedTemplate_PerEntityLayout`; `unit: cmd/sqlgen/gen: TestManifestEmbedTemplate_SingleLayout`; `example: graphql: TestManifest_EveryEntityRecordIsReachable`; `example: sqlite: TestManifest_EveryEntityRecordIsReachable` | all |  |
| manifest-embed.per-entity-zero-entities | A `per_entity` package with no entities omits the entities directive and embeds the top-level file alone | `unit: cmd/sqlgen/gen: TestBuildManifestEmbedContext_PerEntityZeroEntities` | all |  |
| manifest-embed.load-entity-dir | `init()` passes the embedded entities directory to `LoadInto`, which parses every entity file in it and nothing outside it | `unit: manifest: TestLoadInto_SubRootedFS`; `unit: manifest: TestLoadInto_PerEntityLayout`; `example: graphql: TestManifest_EveryEntityRecordIsReachable` | all |  |
| manifest-embed.layout-agnostic | An entity's record is the same whether the JSON was emitted `single` or `per_entity` | `unit: manifest: TestLoadInto_LayoutAgnostic` | all |  |
| manifest-embed.marshal-content-equivalent | `json.Marshal(client.Manifest())` is content-equivalent to the on-disk top-level file, `$schema` included; in `single`, `entities[]` comes back in the index form | `unit: manifest: TestDocument_DecodesSchemaURL`; `example: postgres: TestManifest_MarshalIsContentEquivalent`; `example: mysql: TestManifest_MarshalIsContentEquivalent`; `example: sqlite: TestManifest_MarshalIsContentEquivalent`; `example: graphql: TestManifest_MarshalIsContentEquivalent` | all |  |

The binary-size paragraph is informative. That `embed_in_client` generates the three methods,
and `false` opts out, are `manifest.cfg.embed-in-client.methods` and
`manifest.art.manifest-embed.gated` in `cli/manifest.md`.

## Unclaimed

| Unclaimed test | Reason |
|---|---|
| tenancy: TestTenantResolverPropagatesError | Calls a function value of the `TenantResolver` type and checks Go returns its error; no `tenancy` code runs, so only a change to the type's signature, which `TestTenantResolverInstantiations` already compiles against, could fail it |
| tenancy: TestTenantResolverReceivesContext | Calls a function value of the `TenantResolver` type and checks Go passes its ctx; no `tenancy` code runs, for the same reason as `TestTenantResolverPropagatesError` |
| tenancy: TestErrMissingAndErrMismatchAreDistinct | Checks neither sentinel matches the other under `errors.Is`. The PRD declares both as plain `errors.New` values, which makes them distinct, and states no behavior beyond their texts (`tenancy.err-missing-text`, `tenancy.err-mismatch-text`) |
| manifest: TestDocument_RoundTrip | Marshals a `Document` and decodes it back: a symmetric round trip passes with any JSON key, so it cannot check the §30.4 keys; `manifest-embed.decode-target`'s example tests decode the generator's real output |
| types: TestJSON_ProductionBugRepro | Scans an array-shaped `jsonb` value, decodes it into a typed slice and writes it back. Each step is its own §7.6 row in `cli/gotype.md` (`gotype.json.scan-no-shape-validation`, `gotype.json.decode-delegates`, `gotype.json.value-raw-bytes`) with stronger tests; this one checks only the slice's length and second element |
| manifest: TestEntity_RoundTrip | Marshals an `Entity` and decodes it back; symmetric, for the same reason as `TestDocument_RoundTrip` |

## PRD gaps

Behavior the code has, pinned by tests, that no PRD text states. Each gap waits for
the user's ruling: amend the PRD, which turns the gap into requirement rows, or drop the pins.

| Gap | Behavior | Tests | Section | Ruling |
|---|---|---|---|---|
| gap.types.datetime-surface | `types.DateTime` / `types.NullDateTime` beyond the two §7.2 sentences `cli/gotype.md` rows: `Scan` accepts `[]byte`, an `int64` Unix epoch, a `float64` Julian day, a `time.Time` and nil, and rejects other types; text is parsed against the built-in layouts then `RegisterLayout`'s; `SetOutputLayout` sets the layout `Value` writes; a NULL scans to an invalid `NullDateTime` whose `Value` is SQL NULL; `UnmarshalGQL` returns an error for a non-string and accepts RFC 3339 with or without fractional seconds. The PRD names `SetOutputLayout` only to say SQLite and MySQL statements ignore it; it implies the driver's `time.Time` read-back (§7.2) and a string wire shape (§26.4.1), but states no other Scan source, no `UnmarshalGQL` error, and neither `RegisterLayout` nor the Julian-day decode | `unit: types: TestDateTimeScan`; `unit: types: TestDateTimeValueRoundTrip`; `unit: types: TestNullDateTimeScan`; `unit: types: TestNullDateTimeValue`; `unit: types: TestRegisterLayout`; `unit: types: TestSetOutputLayout`; `unit: types: TestSetOutputLayoutNullDateTime`; `unit: types: TestJulianDayToTime`; `unit: types: TestDateTime_UnmarshalGQL_Errors` | §7.2, §26.4 |  |
| gap.tenancy.resolved-tenant-cache | `tenancy.WithResolvedTenant` / `tenancy.CachedTenant` are exported, and the PRD names neither. They are the ctx cache §29.11 describes, whose behavior `cli/config-features.md` rows; beyond it, the slot is keyed by the tenant type `T` (a different `T` misses), and a cached zero value reports present rather than absent | `unit: tenancy: TestCachedTenantRoundTrip`; `unit: tenancy: TestCachedTenantIsKeyedByType`; `unit: tenancy: TestWithResolvedTenantDerivesANewContext` | §29.3, §29.11 |  |
