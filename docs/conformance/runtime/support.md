# Runtime Requirements: Support Packages

<!-- seeding: in-progress -->
<!-- packages: types manifest tenancy -->

The runtime support packages: `types` (the §7 type integrations), `manifest` (the runtime-embedded
manifest, §30.6) and `tenancy` (§29.3). See [README.md](../README.md) for the format.

## §29.3.1 TenantResolver
<!-- prd: 29.3.1 sha256: da9765396bb05c1c -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| tenancy.resolver-concrete-type | With tenancy enabled, the unified client emits `WithTenantResolver(r tenancy.TenantResolver[T]) ClientOption` and holds a concrete `tenantResolver tenancy.TenantResolver[T]` field, where `T` is the resolved Go type of the tenant column | `unit: cmd/sqlgen/gen: TestBuildClientContext_tenancyEnabledProducesResolverWiring`; `unit: cmd/sqlgen/gen: TestTenancyTemplate_clientRawHasBypassComment` | all |  |

## §30.6 Runtime-Embedded Manifest
<!-- prd: 30.6 sha256: dc335fc02e406523 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| manifest-embed.embed-directives | `manifest_embed_gen.go` always embeds `manifest/manifest_gen.json`, and embeds `all:manifest/entities` only in the `per_entity` layout | `unit: cmd/sqlgen/gen: TestBuildManifestEmbedContext_Paths`; `unit: cmd/sqlgen/gen: TestManifestEmbedTemplate_PerEntityLayout`; `unit: cmd/sqlgen/gen: TestManifestEmbedTemplate_SingleLayout` | all |  |

## PRD gaps

Behavior the code has, pinned by tests, that no PRD text states. Each gap waits for
the user's ruling: amend the PRD, which turns the gap into requirement rows, or drop the pins.

| Gap | Behavior | Tests | Section | Ruling |
|---|---|---|---|---|
| gap.manifest-embed.per-entity-zero-entities | A `per_entity` package with zero entities downgrades to the single-file embed (no `//go:embed all:` directive); §30.6 says the entities embed is emitted in `per_entity` layout without an exception | `unit: cmd/sqlgen/gen: TestBuildManifestEmbedContext_PerEntityZeroEntities` | §30.6 |  |
