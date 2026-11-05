# CLI Requirements: API Generation

<!-- seeding: complete -->
<!-- matrix: api-generation rows=element cols=single-pk,composite-pk,no-pk,soft-delete,tenanted,view -->

API generation (§26) is emitted by `cmd/sqlgen/gen`, whose orphan check stays with [`gen.md`](gen.md), so this file lists no package of its own. Its rows claim `gen`, `cli`, golden and example tests. It holds the `api-generation` matrix and §26.5–§26.12; the schema sections (§26–§26.4.1 and §16.4's GraphQL rows) are in [`api-schema.md`](api-schema.md).

## §26.4 GraphQL Schema Generation
<!-- prd: 26.4 sha256: 45c59c494b3a2b7b -->

The `api-generation` matrix rows for §26.4; its other rows are in [`api-schema.md`](api-schema.md).

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.object-type.single-pk | A table gets a PascalCase object type (`type Product`) | `unit: cmd/sqlgen/gen: TestGraphQLSchema_emitsExpectedTypesPerTable` | all | api-generation: object-type × single-pk |
| api.object-type.composite-pk | A composite-PK table gets an object type | `golden: graphql` | all | api-generation: object-type × composite-pk |
| api.object-type.no-pk | A no-PK table emits no GraphQL type, query or mutation | `unit: cmd/sqlgen/gen: TestGraphQLSchema_noPKTableHasNoSurface` | all | api-generation: object-type × no-pk |
| api.object-type.soft-delete | A soft-delete table gets an object type | `unit: cmd/sqlgen/gen: TestApiEnabledFalseExcludesType`; `golden: graphql` | all | api-generation: object-type × soft-delete |
| api.object-type.tenanted | A tenanted table gets an object type | `golden: graphql` | all | api-generation: object-type × tenanted |
| api.object-type.view | A view gets an object type | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `unit: cmd/sqlgen/gen: TestAPIViewSchema_declaresExactlySevenNames` | all | api-generation: object-type × view |
| api.connection-envelope.single-pk | A table gets `<T>Connection { edges, pageInfo, totalCount }` and `<T>Edge { node, cursor }` | `unit: cmd/sqlgen/gen: TestGraphQLSchema_emitsExpectedTypesPerTable`; `example: graphql: TestResponseShape_Edges`; `example: graphql: TestResponseShape_PageInfo` | all | api-generation: connection-envelope × single-pk |
| api.connection-envelope.composite-pk | A composite-PK table gets `<T>Connection` / `<T>Edge` | `golden: graphql` | all | api-generation: connection-envelope × composite-pk |
| api.connection-envelope.soft-delete | A soft-delete table gets `<T>Connection` / `<T>Edge` | `golden: graphql` | all | api-generation: connection-envelope × soft-delete |
| api.connection-envelope.tenanted | A tenanted table gets `<T>Connection` / `<T>Edge` | `golden: graphql` | all | api-generation: connection-envelope × tenanted |
| api.connection-envelope.view | A view gets `<V>Connection` / `<V>Edge` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `unit: cmd/sqlgen/gen: TestAPIViewSchema_declaresExactlySevenNames` | all | api-generation: connection-envelope × view |
| api.list-result-envelope.single-pk | A table gets `<T>ListResult { items, totalCount, offset, limit, hasMore }` | `unit: cmd/sqlgen/gen: TestGraphQLSchema_emitsExpectedTypesPerTable`; `example: graphql: TestResponseShape_ListResult` | all | api-generation: list-result-envelope × single-pk |
| api.list-result-envelope.composite-pk | A composite-PK table gets `<T>ListResult` | `golden: graphql` | all | api-generation: list-result-envelope × composite-pk |
| api.list-result-envelope.soft-delete | A soft-delete table gets `<T>ListResult` | `golden: graphql` | all | api-generation: list-result-envelope × soft-delete |
| api.list-result-envelope.tenanted | A tenanted table gets `<T>ListResult` | `golden: graphql` | all | api-generation: list-result-envelope × tenanted |
| api.list-result-envelope.view | A view gets `<V>ListResult` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `unit: cmd/sqlgen/gen: TestAPIViewSchema_declaresExactlySevenNames` | all | api-generation: list-result-envelope × view |
| api.filter-input.single-pk | A table gets `input <T>Filter` with a comparator field per filterable column plus `and` / `or` | `unit: cmd/sqlgen/gen: TestGraphQLSchema_emitsExpectedTypesPerTable`; `unit: cmd/sqlgen/gen: TestFilterProjection_pairsSchemaAndTranslator` | all | api-generation: filter-input × single-pk |
| api.filter-input.composite-pk | A composite-PK table gets `input <T>Filter` | `golden: graphql` | all | api-generation: filter-input × composite-pk |
| api.filter-input.soft-delete | A soft-delete table gets `input <T>Filter` | `unit: cmd/sqlgen/gen: TestApiEnabledFalseExcludesType` | all | api-generation: filter-input × soft-delete |
| api.filter-input.soft-delete-regular-field | On a soft-delete table the soft-delete column is a regular comparator field of `<T>Filter` (`deletedAt: NullableTimeComparator`), with no `includeDeleted` flag | `unit: cmd/sqlgen/gen: TestFilterTranslator_DeletedAtIsRegularFilterField`; `golden: graphql` | all | api-generation: filter-input × soft-delete |
| api.filter-input.tenanted | A tenanted table gets `input <T>Filter` | `golden: graphql`; `example: graphql: TestTenancy_SameTenantFilter` | all | api-generation: filter-input × tenanted |
| api.filter-input.view | A view gets `input <V>Filter` (columns plus `and` / `or`) reaching every comparator family its columns resolve to | `unit: cmd/sqlgen/gen: TestAPIViewSchema_filterCarriesTheFullFamilySet`; `example: graphql: TestViewFilter_EnumNarrows`; `example: graphql: TestViewFilter_JSONBNarrows` | all | api-generation: filter-input × view |
| api.sort-input.single-pk | A table gets `input <T>Sort { field: <T>SortField!, direction: SortDirection! }` and `enum <T>SortField` | `unit: cmd/sqlgen/gen: TestSortProjection_everySortFieldMatchesSchemaEnum`; `golden: graphql` | all | api-generation: sort-input × single-pk |
| api.sort-input.composite-pk | A composite-PK table gets `<T>Sort` / `<T>SortField` | `golden: graphql` | all | api-generation: sort-input × composite-pk |
| api.sort-input.soft-delete | A soft-delete table gets `<T>Sort` / `<T>SortField` | `golden: graphql` | all | api-generation: sort-input × soft-delete |
| api.sort-input.tenanted | A tenanted table gets `<T>Sort` / `<T>SortField` | `golden: graphql` | all | api-generation: sort-input × tenanted |
| api.sort-input.view | A view gets `<V>Sort` / `<V>SortField` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface` | all | api-generation: sort-input × view |
| api.create-input.single-pk | A table gets `input Create<T>Input` | `unit: cmd/sqlgen/gen: TestGraphQLSchema_emitsExpectedTypesPerTable`; `unit: cmd/sqlgen/gen: TestGraphQLSchema_CompositePKCreateInput/single surrogate PK is unchanged` | all | api-generation: create-input × single-pk |
| api.create-input.composite-pk | A composite-PK table gets `Create<T>Input` carrying its key columns | `unit: cmd/sqlgen/gen: TestGraphQLSchema_CompositePKCreateInput/junction with a payload column`; `unit: cmd/sqlgen/gen: TestGraphQLSchema_CompositePKCreateInput/all-PK junction`; `example: graphql: TestCompositePKCreate_AllPKJunction` | all | api-generation: create-input × composite-pk |
| api.create-input.soft-delete | A soft-delete table gets `Create<T>Input` | `unit: cmd/sqlgen/gen: TestApiEnabledFalseExcludesType` | all | api-generation: create-input × soft-delete |
| api.create-input.tenanted | A tenanted table gets `Create<T>Input`, carrying the tenant column | `unit: cmd/sqlgen/gen: TestTenantColumn_ExcludedFromUpdateInput/non-PK tenant column is dropped` | all | api-generation: create-input × tenanted |
| api.create-input.view | A view gets no `Create<V>Input` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `unit: cmd/sqlgen/gen: TestValidateAPIViewReadOnly/create input`; `example: graphql: TestViewSchema_ServesTheReadSet` | all | api-generation: create-input × view |
| api.update-input.single-pk | A table gets `input Update<T>Input` | `unit: cmd/sqlgen/gen: TestGraphQLSchema_emitsExpectedTypesPerTable` | all | api-generation: update-input × single-pk |
| api.update-input.composite-pk | A composite-PK table with a non-key column gets `Update<T>Input` | `unit: cmd/sqlgen/gen: TestGraphQLSchema_CompositePKCreateInput/junction with a payload column`; `unit: cmd/sqlgen/gen: TestHasCreateInput_AllPKJunction/task_assignees` | all | api-generation: update-input × composite-pk |
| api.update-input.soft-delete | A soft-delete table gets `Update<T>Input` | `golden: graphql` | all | api-generation: update-input × soft-delete |
| api.update-input.tenanted | A tenanted table gets `Update<T>Input` | `unit: cmd/sqlgen/gen: TestTenantColumn_ExcludedFromUpdateInput/non-PK tenant column is dropped`; `unit: cmd/sqlgen/gen: TestTenantColumn_ExcludedFromUpdateInput/tenant-in-PK table is unchanged`; `unit: cmd/sqlgen/gen: TestTenantColumn_KeptWhenNotRequired` | postgres | api-generation: update-input × tenanted |
| api.update-input.view | A view gets no `Update<V>Input` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `unit: cmd/sqlgen/gen: TestValidateAPIViewReadOnly/update input`; `example: graphql: TestViewSchema_ServesTheReadSet` | all | api-generation: update-input × view |
| api.relationship-field.single-pk | A table's exposed relationships are fields on its object type | `unit: cmd/sqlgen/gen: TestAPIObjectType_APIDisabledTargetDropped`; `unit: cmd/sqlgen/gen: TestMapRelationshipToGraphQL_NoGoTypeSniff` | all | api-generation: relationship-field × single-pk |
| api.relationship-field.composite-pk | A composite-PK table's relationships (edges over a multi-pair FK) are fields on its object type | | all | api-generation: relationship-field × composite-pk |
| api.relationship-field.soft-delete | A soft-delete table's relationships are fields on its object type | `golden: graphql` | all | api-generation: relationship-field × soft-delete |
| api.relationship-field.tenanted | A tenanted table's relationships are fields on its object type | `golden: graphql` | all | api-generation: relationship-field × tenanted |
| api.relationship-field.view | A view's object type carries no relationship field and its walker no relationship case | `unit: cmd/sqlgen/gen: TestViewWalker_ColumnsOnly`; `unit: cmd/sqlgen/gen: TestBuildAPIContext_viewIsAFirstClassEntry` | all | api-generation: relationship-field × view |
| api.relationship-filter-member.single-pk | A table's `<T>Filter` carries a nested `<Target>Filter` member per list relationship, compiled to a correlated `EXISTS` | `unit: cmd/sqlgen/gen: TestAPIFilterRelationships_MemberPerListRelationship`; `unit: cmd/sqlgen/gen: TestAPIFilterRelationships_SchemaEmitsNestedInput`; `example: graphql: TestRelationshipFilterAPI_ColumnAndRelationshipInOneQuery` | all | api-generation: relationship-filter-member × single-pk |
| api.relationship-filter-member.nest-for-depth | A relationship filter member is one hop per level; nesting reaches further hops | `unit: cmd/sqlgen/gen: TestAPIFilterRelationships_SelfReferentialInput`; `example: graphql: TestRelationshipFilterAPI_SelfReferentialTwoLevels` | all | api-generation: relationship-filter-member × single-pk |
| api.relationship-filter-member.composite-pk | A composite-PK parent's `<T>Filter` carries relationship filter members | | all | api-generation: relationship-filter-member × composite-pk |
| api.relationship-filter-member.soft-delete | A soft-delete table's `<T>Filter` carries relationship filter members | `example: graphql: TestRelationshipFilterAPI_ManyToMany`; `golden: graphql` | all | api-generation: relationship-filter-member × soft-delete |
| api.relationship-filter-member.tenanted | A tenanted table's `<T>Filter` carries relationship filter members | `example: graphql: TestRelationshipFilterAPI_SelfReferentialTwoLevels`; `example: graphql: TestRelationshipFilterAPI_TenantedTargetIsScoped` | all | api-generation: relationship-filter-member × tenanted |
| api.relationship-filter-member.view | A view's `<V>Filter` carries no relationship member | `unit: cmd/sqlgen/gen: TestAPIFilterRelationships_ViewsCarryNone` | all | api-generation: relationship-filter-member × view |

## §26.5 GraphQL Resolver Generation
<!-- prd: 26.5 sha256: af8ca3275a28d4d3 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.resolver.concrete-implementations | Generated resolvers are concrete implementations of the gqlgen resolver interfaces: one seed method per root field on `*queryResolver` / `*mutationResolver` | `unit: cmd/sqlgen/gen: TestSeeds_DelegateToQM`; `golden: graphql` | all | |
| api.resolver.translate-call-output | A resolver translates its inputs, makes one unified-client call, and returns the client's output | `unit: cmd/sqlgen/gen: TestResolvers_PKArgUsesGqlgenDefaultBinding`; `unit: cmd/sqlgen/gen: TestSeeds_DelegateToQM` | all | |
| api.resolver.selection-to-fieldoptions | A resolver turns the GraphQL field selection into the client's `FieldOptions` and passes it on the client call | `golden: graphql`; `example: graphql: TestQueryCount_NestedRelationships` | all | |
| api.resolver.query-count-batched | A request issues `1 + count(O2M selected) + 2 * count(M2M selected)` queries while each parent set fits one chunk of keys | `example: graphql: TestQueryCount_SingleTable`; `example: graphql: TestQueryCount_NestedRelationships` | all | |
| api.resolver.query-count-chunk | Each extra chunk past the first adds one query to each chunked step | `example: graphql: TestSurfacePromises_ChunkedParentSetAddsOneQueryPerChunk` | all | |

## §26.5.1 Exposed Resolver Surface
<!-- prd: 26.5.1 sha256: e455d819f94788ef -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.surface.incdec-not-root | `Increment` / `Decrement` are not root resolvers. They are `_inc` / `_dec` operators on the update input | `unit: cmd/sqlgen/gen: TestResolvers_CuratedSurface_NoExcludedMethods`; `unit: cmd/sqlgen/gen: TestGraphQLSchema_emitsExpectedTypesPerTable` | all | |
| api.surface.no-mask-all-keys | With no `api.operations` mask, every key is on and every schema-allowed surface is emitted | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_UpdateTsFollowsUpdateWhere/no mask keeps the mutation`; `unit: cmd/sqlgen/gen: TestAPIOperationsMask_ListFollowsPaginate/no mask keeps the list query`; `unit: cmd/sqlgen/gen: TestAPINested_ParentMaskConjoinsTheBase/no mask` | all | |
| api.surface.one-key-one-method | A mask key gates only the surface backed by the method it names: masking `update` keeps `update<T>s` | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_UpdateTsFollowsUpdateWhere/API update off keeps the filter-scoped mutation` | all | |
| api.surface.one-key-one-method-reads | Masking `get` and `connection` keeps `<t>List` | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_ListFollowsPaginate/API get and connection off keep the list query` | all | |
| api.surface.get-backed-by-get | `<table>(id)` calls the client's `Get` | `unit: cmd/sqlgen/gen: TestResolvers_PKArgUsesGqlgenDefaultBinding`; `unit: cmd/sqlgen/gen: TestResolvers_PKArgNoSelfConvertForStringPK` | all | |
| api.surface.connection-backed | `<table>s(filter, first, after, last, before): <T>Connection!` calls the client's `Connection` | `unit: cmd/sqlgen/gen: TestConnectionPaginationPassthrough`; `golden: graphql` | all | |
| api.surface.connection-no-sort-arg | The connection query has no `sort` argument | `golden: graphql` | all | |
| api.surface.list-backed-by-paginate | `<table>List(filter, sort, limit, offset): <T>ListResult!` calls the client's `Paginate` | `unit: cmd/sqlgen/gen: TestListPaginationPassthrough` | all | |
| api.surface.create-backed | `create<T>(input)` calls `Create` | `golden: graphql` | all | |
| api.surface.create-many-backed | `create<T>s(inputs)` calls `CreateMany` | `golden: graphql` | all | |
| api.surface.update-backed | `update<T>(id, input)` calls `Update` | `unit: cmd/sqlgen/gen: TestResolvers_PKArgUsesGqlgenDefaultBinding`; `unit: cmd/sqlgen/gen: TestResolvers_IncDec_DispatchOrder` | all | |
| api.surface.update-where-backed | `update<T>s(filter, input)` calls `UpdateWhere` | `golden: graphql` | all | |
| api.surface.upsert-needs-conflict-pk | `upsert<T>(input)` is emitted only when the table emits `<T>ConflictPK`, and it calls `Upsert` with that constant | `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_RequiresThePKConflictTarget/schema-declared key`; `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_RequiresThePKConflictTarget/app-enforced key`; `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_RequiresThePKConflictTarget/override backed by an inline UNIQUE` | all | |
| api.surface.upsert-needs-a-target | No `upsert<T>` is emitted on a table with no PK and no unique constraint | `unit: cmd/sqlgen/gen: TestResolvers_UpsertRequiresPKOrUnique` | all | |
| api.surface.hard-delete-backed | `delete<T>` / `hardDelete<T>` call `HardDelete`; `softDelete<T>` calls `SoftDelete`; `restore<T>` calls `Restore` | `unit: cmd/sqlgen/gen: TestResolvers_PKArgUsesGqlgenDefaultBinding` | all | |
| api.surface.views-read-only | A view's surface is the read-only subset: object type, envelopes, filter / sort inputs and the three queries, never a mutation | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `example: graphql: TestViewSchema_NoMutationsAndNoRefresh`; `example: graphql: TestViewSchema_ServesTheReadSet/WorkspaceNoteSummary` | all | |
| api.surface.views-excluded-table | The `Excluded` table applies to views unchanged | `unit: cmd/sqlgen/gen: TestAPIViewSurface_ExcludedMethodsGetNoRootField` | all | |
| api.get-query.single-pk | A single-PK table gets `<table>(id): <T>` | `unit: cmd/sqlgen/gen: TestGraphQLSchema_emitsExpectedTypesPerTable`; `example: graphql: TestCuratedSurface_Category/GetByPK` | all | api-generation: get-query × single-pk |
| api.get-query.composite-pk | A composite-PK table gets `<table>(<pk1>, <pk2>, …): <T>` with one argument per PK column | `unit: cmd/sqlgen/gen: TestGraphQLSchema_CompositePK_EmitsAllPKArgs`; `unit: cmd/sqlgen/gen: TestResolvers_CompositePK_EmitsCompositeSignatures`; `example: graphql: TestCompositePKCreate_AllPKJunction` | all | api-generation: get-query × composite-pk |
| api.get-query.soft-delete | A soft-delete table gets `<table>(id): <T>` | `unit: cmd/sqlgen/gen: TestOperationsPreset_NoDelete`; `example: graphql: TestCuratedSurface_User/GetByPK` | all | api-generation: get-query × soft-delete |
| api.get-query.tenanted | A tenanted table gets `<table>(id): <T>` | `golden: graphql` | all | api-generation: get-query × tenanted |
| api.get-query.view | A view with a resolved PK gets `<view>(pk): <V>`; a view with no `@pk` gets none | `unit: cmd/sqlgen/gen: TestBuildAPIContext_viewGatesByPKAndConnection/pk and cursor key present`; `unit: cmd/sqlgen/gen: TestBuildAPIContext_viewGatesByPKAndConnection/no @pk drops the by-pk query but keeps the connection`; `example: graphql: TestViewQueryCount/by-pk read is one query` | all | api-generation: get-query × view |
| api.connection-query.single-pk | A single-PK table gets `<table>s(filter, first, after, last, before): <T>Connection!` | `unit: cmd/sqlgen/gen: TestGraphQLSchema_emitsExpectedTypesPerTable`; `example: graphql: TestCuratedSurface_Category/Connection` | all | api-generation: connection-query × single-pk |
| api.connection-query.composite-pk | A composite-PK table gets the connection query | `example: graphql: TestTenancy_SameTenantFilter`; `golden: graphql` | all | api-generation: connection-query × composite-pk |
| api.connection-query.soft-delete | A soft-delete table gets the connection query | `example: graphql: TestCuratedSurface_User/Connection` | all | api-generation: connection-query × soft-delete |
| api.connection-query.tenanted | A tenanted table gets the connection query | `example: graphql: TestTenancy_SameTenantFilter`; `example: graphql: TestTenancy_HeaderSwitch` | all | api-generation: connection-query × tenanted |
| api.connection-query.view | A view whose cursor keys resolve gets `<views>(…): <V>Connection!`; one whose keys do not resolve gets none | `unit: cmd/sqlgen/gen: TestBuildAPIContext_viewGatesByPKAndConnection/pk and cursor key present`; `unit: cmd/sqlgen/gen: TestBuildAPIContext_viewGatesByPKAndConnection/cursor keys unresolvable drops the connection only`; `example: graphql: TestViewQueryCount/connection with totalCount is count + page` | all | api-generation: connection-query × view |
| api.list-query.single-pk | A single-PK table gets `<table>List(filter, sort, limit, offset): <T>ListResult!` | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_ListFollowsPaginate/no mask keeps the list query`; `example: graphql: TestCuratedSurface_Category/List` | all | api-generation: list-query × single-pk |
| api.list-query.composite-pk | A composite-PK table gets the list query | `golden: graphql` | all | api-generation: list-query × composite-pk |
| api.list-query.soft-delete | A soft-delete table gets the list query | `example: graphql: TestCuratedSurface_User/List` | all | api-generation: list-query × soft-delete |
| api.list-query.tenanted | A tenanted table gets the list query | `example: graphql: TestRelationshipFilterAPI_QueryCountUnchanged`; `golden: graphql` | all | api-generation: list-query × tenanted |
| api.list-query.view | Every API-enabled view gets `<view>List`, including one with neither a PK nor resolvable cursor keys | `unit: cmd/sqlgen/gen: TestBuildAPIContext_viewGatesByPKAndConnection/neither leaves the list query as the whole surface`; `example: graphql: TestViewQueryCount/list with totalCount is count + page` | all | api-generation: list-query × view |
| api.create.single-pk | A single-PK table gets `create<T>(input: Create<T>Input!): <T>!` | `unit: cmd/sqlgen/gen: TestGraphQLSchema_emitsExpectedTypesPerTable`; `example: graphql: TestCuratedSurface_Category/Create` | all | api-generation: create × single-pk |
| api.create.composite-pk | A composite-PK table gets `create<T>` | `unit: cmd/sqlgen/gen: TestGraphQLSchema_CompositePKCreateInput/all-PK junction`; `example: graphql: TestCompositePKCreate_AllPKJunction` | all | api-generation: create × composite-pk |
| api.create.soft-delete | A soft-delete table gets `create<T>` | `example: graphql: TestCuratedSurface_User/Create` | all | api-generation: create × soft-delete |
| api.create.tenanted | A tenanted table gets `create<T>` | `example: graphql: TestCompositePKCreate_TenantInPKVerifyMatch/matching tenant writes the row`; `example: graphql: TestTenantUpdateInput_CreateStillCarriesTenant` | all | api-generation: create × tenanted |
| api.create.view | A view gets no `create<V>` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `example: graphql: TestViewSchema_NoMutationsAndNoRefresh` | all | api-generation: create × view |
| api.create-many.single-pk | A single-PK table gets `create<T>s(inputs: [Create<T>Input!]!): [<T>!]!` | `example: graphql: TestCuratedSurface_Category/CreateMany`; `unit: cmd/sqlgen/gen: TestOperationsPreset_AppendOnly` | all | api-generation: create-many × single-pk |
| api.create-many.composite-pk | A composite-PK table gets `create<T>s` | `golden: graphql` | all | api-generation: create-many × composite-pk |
| api.create-many.soft-delete | A soft-delete table gets `create<T>s` | `example: graphql: TestCuratedSurface_User/CreateMany` | all | api-generation: create-many × soft-delete |
| api.create-many.tenanted | A tenanted table gets `create<T>s` | `golden: graphql` | all | api-generation: create-many × tenanted |
| api.create-many.view | A view gets no `create<V>s` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `example: graphql: TestViewSchema_NoMutationsAndNoRefresh` | all | api-generation: create-many × view |
| api.update.single-pk | A single-PK table gets `update<T>(id, input: Update<T>Input!): <T>!` | `unit: cmd/sqlgen/gen: TestGraphQLSchema_emitsExpectedTypesPerTable`; `example: graphql: TestCuratedSurface_Category/Update` | all | api-generation: update × single-pk |
| api.update.composite-pk | A composite-PK table with a non-key column gets `update<T>(<pk1>, <pk2>, …, input)` | `golden: graphql` | all | api-generation: update × composite-pk |
| api.update.soft-delete | A soft-delete table gets `update<T>` | `example: graphql: TestCuratedSurface_User/Update` | all | api-generation: update × soft-delete |
| api.update.tenanted | A tenanted table gets `update<T>` | `example: graphql: TestTenantUpdateInput_NormalUpdateStillWorks` | all | api-generation: update × tenanted |
| api.update.view | A view gets no `update<V>` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `example: graphql: TestViewSchema_NoMutationsAndNoRefresh` | all | api-generation: update × view |
| api.update-where.single-pk | A single-PK table gets `update<T>s(filter: <T>Filter!, input: Update<T>Input!): [<T>!]!` | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_UpdateTsFollowsUpdateWhere/no mask keeps the mutation`; `example: graphql: TestCuratedSurface_Category/UpdateMany` | all | api-generation: update-where × single-pk |
| api.update-where.composite-pk | A composite-PK table with a non-key column gets `update<T>s` | `golden: graphql` | all | api-generation: update-where × composite-pk |
| api.update-where.soft-delete | A soft-delete table gets `update<T>s` | `example: graphql: TestCuratedSurface_User/UpdateMany` | all | api-generation: update-where × soft-delete |
| api.update-where.tenanted | A tenanted table gets `update<T>s` | `golden: graphql` | all | api-generation: update-where × tenanted |
| api.update-where.view | A view gets no `update<V>s` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `example: graphql: TestViewSchema_NoMutationsAndNoRefresh` | all | api-generation: update-where × view |
| api.upsert.single-pk | A single-PK table with a declared key gets `upsert<T>(input: Create<T>Input!): <T>!` | `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_RequiresThePKConflictTarget/schema-declared key`; `example: graphql: TestCuratedSurface_Category/Upsert` | all | api-generation: upsert × single-pk |
| api.upsert.composite-pk | A composite-PK table gets `upsert<T>` | `example: graphql: TestCompositePKCreate_AllPKJunctionUpsertIsIdempotent` | all | api-generation: upsert × composite-pk |
| api.upsert.soft-delete | A soft-delete table gets `upsert<T>` | `example: graphql: TestCuratedSurface_User/Upsert`; `unit: cmd/sqlgen/gen: TestOperationsPreset_NoDelete` | all | api-generation: upsert × soft-delete |
| api.upsert.tenanted | A tenanted table gets `upsert<T>` | `golden: graphql` | all | api-generation: upsert × tenanted |
| api.upsert.view | A view gets no `upsert<V>` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `example: graphql: TestViewSchema_NoMutationsAndNoRefresh` | all | api-generation: upsert × view |
| api.delete.single-pk | A table with no soft-delete column gets `delete<T>(id): Boolean!` backed by `HardDelete` | `unit: cmd/sqlgen/gen: TestResolvers_DeleteNamingRules_HardOnly`; `example: graphql: TestCuratedSurface_Category/Delete` | all | api-generation: delete × single-pk |
| api.delete.composite-pk | A composite-PK table gets `delete<T>(<pk1>, <pk2>, …): Boolean!` | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_PerTable`; `golden: graphql` | all | api-generation: delete × composite-pk |
| api.delete.soft-delete | On a soft-delete table, keeping `soft_delete` and masking `hard_delete` gives `delete<T>(id): <T>` (nullable) backed by `SoftDelete` | `unit: cmd/sqlgen/gen: TestResolvers_DeleteNamingRules_SoftOnly`; `unit: cmd/sqlgen/gen: TestOperationsPreset_NoHardDelete` | all | api-generation: delete × soft-delete |
| api.delete.soft-delete-hard-only | On a soft-delete table, keeping `hard_delete` and masking `soft_delete` gives `delete<T>(id): Boolean!` | `unit: cmd/sqlgen/gen: TestAPIDeleteNaming_SoftDeleteTableHardOnly` | all | api-generation: delete × soft-delete |
| api.delete.soft-delete-both-no-bare | With both delete variants on, no bare `delete<T>` is generated | `unit: cmd/sqlgen/gen: TestResolvers_DeleteNamingRules_Both` | all | api-generation: delete × soft-delete |
| api.delete.tenanted | A tenanted table with no soft-delete column gets `delete<T>(id): Boolean!` | `golden: graphql` | all | api-generation: delete × tenanted |
| api.delete.view | A view gets no `delete<V>` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `example: graphql: TestViewSchema_NoMutationsAndNoRefresh` | all | api-generation: delete × view |
| api.hard-soft-delete-pair.single-pk | A table with no soft-delete column gets neither `hardDelete<T>` nor `softDelete<T>` | `unit: cmd/sqlgen/gen: TestResolvers_DeleteNamingRules_HardOnly`; `unit: cmd/sqlgen/gen: TestAPIOperationsMask_CannotExceedClient` | all | api-generation: hard-soft-delete-pair × single-pk |
| api.hard-soft-delete-pair.composite-pk | A composite-PK table with a soft-delete column gets `hardDelete<T>(<pk…>): Boolean!` and `softDelete<T>(<pk…>): <T>`, one argument per PK column | `unit: cmd/sqlgen/gen: TestAPIDeleteVariants_CompositePKSoftDeleteTable/hard delete takes every key column and returns Boolean`; `unit: cmd/sqlgen/gen: TestAPIDeleteVariants_CompositePKSoftDeleteTable/soft delete takes every key column and returns the nullable row`; `unit: cmd/sqlgen/gen: TestAPIDeleteVariants_CompositePKSoftDeleteTable/no bare delete` | all | api-generation: hard-soft-delete-pair × composite-pk |
| api.hard-soft-delete-pair.soft-delete-hard | With both variants on, a soft-delete table gets `hardDelete<T>(id): Boolean!` backed by `HardDelete` | `unit: cmd/sqlgen/gen: TestResolvers_DeleteNamingRules_Both`; `example: graphql: TestCuratedSurface_User/HardDelete` | all | api-generation: hard-soft-delete-pair × soft-delete |
| api.hard-soft-delete-pair.soft-delete-soft | With both variants on, a soft-delete table gets `softDelete<T>(id): <T>` (nullable) backed by `SoftDelete` | `unit: cmd/sqlgen/gen: TestResolvers_DeleteNamingRules_Both`; `example: graphql: TestCuratedSurface_User/SoftDelete` | all | api-generation: hard-soft-delete-pair × soft-delete |
| api.hard-soft-delete-pair.tenanted | A tenanted table with a soft-delete column gets the `hardDelete<T>` / `softDelete<T>` pair | `unit: cmd/sqlgen/gen: TestAPIDeleteVariants_TenantedSoftDeleteTable/hard delete returns Boolean`; `unit: cmd/sqlgen/gen: TestAPIDeleteVariants_TenantedSoftDeleteTable/soft delete returns the nullable row`; `unit: cmd/sqlgen/gen: TestAPIDeleteVariants_TenantedSoftDeleteTable/no bare delete` | all | api-generation: hard-soft-delete-pair × tenanted |
| api.hard-soft-delete-pair.view | A view gets neither `hardDelete<V>` nor `softDelete<V>` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `example: graphql: TestViewSchema_NoMutationsAndNoRefresh` | all | api-generation: hard-soft-delete-pair × view |
| api.restore.single-pk | A table with no soft-delete column gets no `restore<T>`, even under a mask that names `restore: true` | `unit: cmd/sqlgen/gen: TestResolvers_SoftDeleteRequiresSoftDeleteColumn`; `unit: cmd/sqlgen/gen: TestAPIOperationsMask_CannotExceedClient` | all | api-generation: restore × single-pk |
| api.restore.composite-pk | A composite-PK table with a soft-delete column gets `restore<T>(<pk…>): <T>` | `unit: cmd/sqlgen/gen: TestAPIDeleteVariants_CompositePKSoftDeleteTable/restore takes every key column and returns the nullable row` | all | api-generation: restore × composite-pk |
| api.restore.soft-delete | A soft-delete table gets `restore<T>(id): <T>` (nullable) backed by `Restore` | `unit: cmd/sqlgen/gen: TestResolvers_DeleteNamingRules_Both`; `example: graphql: TestCuratedSurface_User/Restore` | all | api-generation: restore × soft-delete |
| api.restore.soft-only-mask | With `hard_delete` masked, `restore<T>` is still emitted while `restore` survives the mask | `unit: cmd/sqlgen/gen: TestResolvers_DeleteNamingRules_SoftOnly`; `unit: cmd/sqlgen/gen: TestOperationsPreset_NoHardDelete` | all | |
| api.restore.masked | Masking `restore` removes `restore<T>` | `unit: cmd/sqlgen/gen: TestOperationsPreset_NoDelete` | all | |
| api.restore.tenanted | A tenanted table with a soft-delete column gets `restore<T>` | `unit: cmd/sqlgen/gen: TestAPIDeleteVariants_TenantedSoftDeleteTable/restore returns the nullable row` | all | api-generation: restore × tenanted |
| api.restore.view | A view gets no `restore<V>` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `example: graphql: TestViewSchema_NoMutationsAndNoRefresh` | all | api-generation: restore × view |
| api.create-with-related.single-pk | A single-PK table with an eligible edge, the `create` family on and `create_with_related` + `create` on the mask gets `create<T>WithRelated(input): <T>!` | `unit: cmd/sqlgen/gen: TestAPINestedSchema_FlatSurfaceIsByteIdentical`; `golden: graphql` | all | api-generation: create-with-related × single-pk |
| api.create-with-related.composite-pk | A composite-PK parent with an eligible edge gets `create<T>WithRelated` | | all | api-generation: create-with-related × composite-pk |
| api.create-with-related.soft-delete | A soft-delete table gets `create<T>WithRelated` | `example: graphql: TestGraphQLNested_CreateReachesEveryShape` | all | api-generation: create-with-related × soft-delete |
| api.create-with-related.tenanted | A tenanted table gets `create<T>WithRelated` | `golden: graphql` | all | api-generation: create-with-related × tenanted |
| api.create-with-related.view | A view gets no `create<V>WithRelated` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `example: graphql: TestViewSchema_NoMutationsAndNoRefresh` | all | api-generation: create-with-related × view |
| api.update-with-related.single-pk | A single-PK table gets `update<T>WithRelated(id, input): <T>!` | `unit: cmd/sqlgen/gen: TestAPINestedSchema_FlatSurfaceIsByteIdentical`; `golden: graphql` | all | api-generation: update-with-related × single-pk |
| api.update-with-related.composite-pk | A composite-PK parent with an eligible edge gets `update<T>WithRelated(<pk…>, input)` | | all | api-generation: update-with-related × composite-pk |
| api.update-with-related.soft-delete | A soft-delete table gets `update<T>WithRelated` | `example: graphql: TestGraphQLNested_UpdateOnlyTouchesNestedRows` | all | api-generation: update-with-related × soft-delete |
| api.update-with-related.tenanted | A tenanted table gets `update<T>WithRelated` | `example: graphql: TestGraphQLNested_IncrementsShareTheTransaction/the increment and the nested create both land`; `golden: graphql` | all | api-generation: update-with-related × tenanted |
| api.update-with-related.view | A view gets no `update<V>WithRelated` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `example: graphql: TestViewSchema_NoMutationsAndNoRefresh` | all | api-generation: update-with-related × view |
| api.upsert-with-related.single-pk | A single-PK table gets `upsert<T>WithRelated(input, conflictTarget: <T>ConflictTarget! = PK): <T>!` | `unit: cmd/sqlgen/gen: TestAPINestedSchema_FlatSurfaceIsByteIdentical`; `golden: graphql` | all | api-generation: upsert-with-related × single-pk |
| api.upsert-with-related.composite-pk | A composite-PK parent with an eligible edge gets `upsert<T>WithRelated` | | all | api-generation: upsert-with-related × composite-pk |
| api.upsert-with-related.soft-delete | A soft-delete table gets `upsert<T>WithRelated` | `example: graphql: TestGraphQLNested_UpsertTakesAConflictTarget` | all | api-generation: upsert-with-related × soft-delete |
| api.upsert-with-related.tenanted | A tenanted table gets `upsert<T>WithRelated` | `golden: graphql` | all | api-generation: upsert-with-related × tenanted |
| api.upsert-with-related.view | A view gets no `upsert<V>WithRelated` | `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface`; `example: graphql: TestViewSchema_NoMutationsAndNoRefresh` | all | api-generation: upsert-with-related × view |

### Excluded from the GraphQL surface

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.excluded.exists-count-getmany-find | `Exists`, `Count`, `GetMany` and `Find` get no root resolver | `unit: cmd/sqlgen/gen: TestResolvers_CuratedSurface_NoExcludedMethods` | all | |
| api.excluded.get-null-when-missing | `<table>(id)` returns null when the row is missing | `example: graphql: TestCuratedSurface_User/GetByPK` | all | |
| api.excluded.count-via-total-count-only | `<table>List(filter) { totalCount }` counts with no rows, in one `COUNT` | `example: graphql: TestSurfacePromises_TotalCountOnlyListIsOneCount` | all | |
| api.excluded.update-many | No batch-of-items update mutation is generated: `update<T>s` takes `(filter, input)` | `unit: cmd/sqlgen/gen: TestResolvers_MutationResultNullability/UpdateProducts returns []*Product`; `unit: cmd/sqlgen/gen: TestAPIOperationsMask_UpdateTsFollowsUpdateWhere/no mask keeps the mutation` | all | |
| api.excluded.upsert-many | No batch-upsert mutation is generated | `unit: cmd/sqlgen/gen: TestAPIExcluded_NoBatchUpsertMutation` | all | |
| api.excluded.tx-primitives | No transaction or savepoint primitive reaches the GraphQL surface | `unit: cmd/sqlgen/gen: TestAPIExcluded_NoTransactionPrimitive` | all | |
| api.excluded.restore-without-soft-delete | `Restore` is not generated without a soft-delete column | `unit: cmd/sqlgen/gen: TestResolvers_SoftDeleteRequiresSoftDeleteColumn` | all | |
| api.excluded.relationship-pagination-args | Relationship fields take no pagination arguments and emit as `[Type!]!` | `unit: cmd/sqlgen/gen: TestWalker_SubCategorizedRelationships` | all | |

### Naming rules for delete/restore and mutation-result nullability

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.delete-naming.soft-returns-row | Soft delete and restore return the entity, served through the request's `FieldOptions` selection | `example: graphql: TestCuratedSurface_User/SoftDelete`; `example: graphql: TestCuratedSurface_User/Restore` | all | |
| api.nullability.create-update-upsert | `create<T>`, `update<T>` and `upsert<T>` return non-null `<T>!` | `unit: cmd/sqlgen/gen: TestGraphQLSchema_emitsExpectedTypesPerTable`; `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_RequiresThePKConflictTarget/schema-declared key`; `golden: graphql` | all | |
| api.nullability.batch | `create<T>s` and `update<T>s` return `[<T>!]!` | `golden: graphql` | all | |
| api.nullability.soft-delete-restore-nullable | `softDelete<T>` and `restore<T>` return nullable `<T>` | `unit: cmd/sqlgen/gen: TestOperationsPreset_NoHardDelete`; `golden: graphql` | all | |
| api.nullability.soft-delete-null-on-missing | A soft delete or restore of a missing row returns null rather than an error | `example: graphql: TestSurfacePromises_SoftDeleteAndRestoreOfMissingRowReturnNull` | all | |
| api.nullability.hard-delete-boolean | Hard delete returns `Boolean!`, `true` when a row was deleted | `unit: cmd/sqlgen/gen: TestResolvers_MutationResultNullability/HardDeleteProduct returns bool`; `example: graphql: TestCuratedSurface_User/HardDelete` | all | |
| api.nullability.hard-delete-true-when-no-error | Hard delete returns `true` once the delete ran without error, also for a row that did not exist | `example: graphql: TestSurfacePromises_HardDeleteMissingRowReturnsTrue` | all | |
| api.nullability.go-signatures | The resolver seeds return `*<T>` for single-row mutations, `[]*<T>` for the two batch mutations and `bool` for hard delete | `unit: cmd/sqlgen/gen: TestResolvers_MutationResultNullability` | all | |

### Narrowing the API surface: `api.operations`

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.mask.client-untouched | The mask never narrows the Go client: a masked `create` / `upsert` keeps `Create` / `Upsert` on the client | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_PerTable` | all | |
| api.mask.per-table-hides | A per-table mask removes the masked mutations from that table only | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_PerTable` | all | |
| api.mask.bare-string-form | The bare-string preset form (`operations: read_only`) is accepted | `unit: cmd/sqlgen/config: TestLoadConfig_APIOperationsAsString`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsBothOperationsForms/preset string` | all | |
| api.mask.object-form | The object form with an optional `preset` plus individual keys is accepted, and the keys override the preset | `unit: cmd/sqlgen/config: TestSchemaV1_acceptsBothOperationsForms/key map`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsBothOperationsForms/per-table key map`; `unit: cmd/sqlgen/config: TestResolveOperations_PresetWithOverrides`; `unit: cmd/sqlgen/config: TestResolveOperations_ReadOnlyWithCreateOverride` | all | |
| api.mask.unknown-preset | An unknown preset name is rejected | `unit: cmd/sqlgen/config: TestValidateAPIOperations_PresetValidated`; `unit: cmd/sqlgen/config: TestExpandPreset/invalid preset` | all | |
| api.mask.preset-all | `all` turns every key on and is the default when no preset is named | `unit: cmd/sqlgen/config: TestExpandPreset/all preset`; `unit: cmd/sqlgen/config: TestExpandPreset/empty preset defaults to all` | all | |
| api.mask.preset-read-only | `read_only` turns on `get`, `paginate`, `connection` only, and every key it does not name resolves off | `unit: cmd/sqlgen/config: TestExpandPreset/read_only preset`; `unit: cmd/sqlgen/gen: TestOperationsPreset_ReadOnly`; `unit: cmd/sqlgen/gen: TestGraphQLSchema_excludesDisabledOperations/api.operations: read_only excludes mutations` | all | |
| api.mask.preset-append-only | `append_only` is `read_only` plus `create`, `create_many`, `create_with_related` | `unit: cmd/sqlgen/config: TestExpandPreset/append_only preset`; `unit: cmd/sqlgen/gen: TestOperationsPreset_AppendOnly` | all | |
| api.mask.preset-no-delete | `no_delete` is every key except `soft_delete`, `hard_delete`, `restore` | `unit: cmd/sqlgen/config: TestExpandPreset/no_delete preset`; `unit: cmd/sqlgen/gen: TestOperationsPreset_NoDelete` | all | |
| api.mask.preset-no-hard-delete | `no_hard_delete` is every key except `hard_delete` | `unit: cmd/sqlgen/config: TestExpandPreset/no_hard_delete preset`; `unit: cmd/sqlgen/gen: TestOperationsPreset_NoHardDelete` | all | |
| api.mask.preset-gating-resolvers | Each preset gates the resolver seeds the same way it gates the schema | `unit: cmd/sqlgen/gen: TestResolvers_OperationsPresetGating` | all | |
| api.mask.with-related-follows-base | Each `…_with_related` key follows the base it composes in every preset: `append_only` keeps `create_with_related` alone | `unit: cmd/sqlgen/config: TestExpandPreset/append_only preset` | all | |
| api.mask.delete-presets-keep-nested | `no_delete` and `no_hard_delete` keep all three `…_with_related` keys | `unit: cmd/sqlgen/config: TestExpandPreset/no_delete preset`; `unit: cmd/sqlgen/config: TestExpandPreset/no_hard_delete preset` | all | |
| api.mask.keys-accepted | `paginate`, `create`, `update_where` and the three `…_with_related` keys are accepted mask keys | `unit: cmd/sqlgen/config: TestValidateAPIOperations_ClientOnlyRejected` | all | |
| api.mask.per-table-replaces | `tables.<name>.api.operations` replaces `api.operations` wholesale; it is not merged | `unit: cmd/sqlgen/config: TestResolveAPIOperationsMask/per-table replaces global wholesale`; `unit: cmd/sqlgen/gen: TestAPIOperationsMask_GlobalAndPresetAndOverride` | all | |
| api.mask.global-applies | A global mask applies to every table without its own | `unit: cmd/sqlgen/config: TestResolveAPIOperationsMask/global applies`; `unit: cmd/sqlgen/gen: TestAPIOperationsMask_GlobalAndPresetAndOverride` | all | |
| api.mask.absent-no-mask | Absent at both levels means no mask | `unit: cmd/sqlgen/config: TestResolveAPIOperationsMask/no mask configured` | all | |
| api.mask.subtractive | The API surface is the client's schema-allowed set intersected with the mask: a key set `true` for a method the client lacks exposes nothing | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_CannotExceedClient` | all | |
| api.mask.overreach-warns-soft-delete | An explicit per-table `soft_delete: true` / `restore: true` with no soft-delete column is ignored and warns, naming the table, key and missing column | `unit: cmd/sqlgen/gen: TestAPIOverReachWarnings/soft_delete without a soft-delete column`; `unit: cmd/sqlgen/gen: TestAPIOverReachWarnings/restore without a soft-delete column` | all | |
| api.mask.overreach-warns-upsert | An explicit per-table `upsert: true` / `upsert_with_related: true` with no conflict target is ignored and warns | `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_WarnsOnAnExplicitMask/explicit upsert, app-enforced key, no conflict target at all`; `unit: cmd/sqlgen/gen: TestAPIOverReachWarnings/upsert_with_related on a table with no conflict target` | all | |
| api.mask.overreach-warns-nested-family | An explicit per-table `…_with_related: true` whose family `nested_mutations` does not enable warns, naming the family | `unit: cmd/sqlgen/gen: TestAPIOverReachWarnings/create_with_related with nested mutations off`; `unit: cmd/sqlgen/gen: TestAPIOverReachWarnings/update_with_related with its family not listed` | all | |
| api.mask.overreach-warns-no-edge | An explicit per-table `…_with_related: true` on a table with no eligible edge warns | `unit: cmd/sqlgen/gen: TestAPIOverReachWarnings/create_with_related on a table with no eligible edge` | all | |
| api.mask.overreach-global-silent | A global mask that over-reaches is silent | `unit: cmd/sqlgen/gen: TestAPIOverReachWarnings/a global mask over-reaching stays silent`; `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_WarnsOnAnExplicitMask/global mask upsert, app-enforced key` | all | |
| api.mask.overreach-allowed-silent | A key the schema allows, or a key set `false`, does not warn | `unit: cmd/sqlgen/gen: TestAPIOverReachWarnings/a key the schema allows stays silent`; `unit: cmd/sqlgen/gen: TestAPIOverReachWarnings/a key set false stays silent` | all | |
| api.mask.flat-upsert-no-conflict-pk-warns | An explicit per-table `upsert: true` on a table with a unique target but no `<T>ConflictPK` omits `upsert<T>` and warns, naming the table | `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_WarnsOnAnExplicitMask/explicit upsert, app-enforced key, UNIQUE column` | all | |
| api.mask.upsert-one-warning | A table with no conflict target at all gets only the over-reach warning for `upsert`, never both | `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_WarnsOnAnExplicitMask/explicit upsert, app-enforced key, no conflict target at all` | all | |
| api.mask.reads-maskable | Masking `get`, `paginate` or `connection` hides the matching query | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_ListFollowsPaginate/API paginate off masks the list query`; `unit: cmd/sqlgen/gen: TestAPISchema_allReadsMaskedOpensNoQueryBlock/table`; `unit: cmd/sqlgen/gen: TestBuildAPIContext_viewOperationsMaskIsSubtractive` | all | |
| api.mask.reads-client-kept | A masked read's client method stays generated | `unit: cmd/sqlgen/gen: TestAPIMask_MaskedReadsKeepClientMethods` | all | |
| api.mask.client-only-keys-error | Naming `get_many`, `update_many`, `exists`, `count`, `increment`, `stream` or `upsert_many` in any `api.operations` block is a hard error, at both the global and per-table level | `unit: cmd/sqlgen/config: TestValidateAPIOperations_ClientOnlyRejected` | all | |
| api.mask.client-only-names-gate | The error for `get_many` / `update_many` names `paginate` / `update_where` as the gating key | `unit: cmd/sqlgen/config: TestValidateAPIOperations_ClientOnlyRejected` | all | |
| api.mask.set-mask-update-many-off | Whenever a mask is set, the masked set's `UpdateMany` is off | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_UpdateWhereAloneCarriesTheUpdateSurface/masking update_where too takes the update surface` | all | |
| api.mask.set-mask-get-many-off | Whenever a mask is set, the masked set's `GetMany` is off | `unit: cmd/sqlgen/gen: TestAPIMask_SetMaskTurnsGetManyOff` | all | |
| api.mask.update-where-alone | `update_where` alone keeps `update<T>s` and everything it needs (update input, translator, Mutation block, root field) | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_UpdateWhereAloneCarriesTheUpdateSurface/generated files`; `unit: cmd/sqlgen/gen: TestAPIOperationsMask_UpdateWhereAloneCarriesTheUpdateSurface/stub-rewriter root field` | all | |
| api.mask.create-input-follows | Masking every create-class mutation removes `Create<T>Input` | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_PerTable` | all | |
| api.mask.update-input-follows | Masking every update-class mutation removes `Update<T>Input` | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_UpdateWhereAloneCarriesTheUpdateSurface/masking update_where too takes the update surface` | all | |
| api.mask.accepted-orphan-create-input | A table with no `<T>ConflictPK` whose mask leaves only `upsert_with_related` keeps `Create<T>Input` even when the nested upsert is then omitted | `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_RequiresThePKConflictTarget/upsert-only mask, app-enforced key, UNIQUE column, every edge off the API` | all | |
| api.mask.composes-with-enabled | `tables.<name>.api.enabled: false` removes the whole table regardless of the mask | `unit: cmd/sqlgen/gen: TestGraphQLSchema_excludesDisabledOperations/api.enabled false on table`; `unit: cmd/sqlgen/gen: TestApiEnabledFalseExcludesType` | all | |
| api.mask.surface-agnostic | The mask sits on `api`, not `api.graphql` | `unit: cmd/sqlgen/config: TestLoadConfig_APIOperationsAsString` | all | |

### Nested mutations on the GraphQL surface

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.nested.flat-byte-identical | Enabling nested mutations leaves `create<T>`, `update<T>` and `upsert<T>` byte-identical | `unit: cmd/sqlgen/gen: TestAPINestedSchema_FlatSurfaceIsByteIdentical` | all | |
| api.nested.own-wrapper | Each nested mutation takes its own wrapper input that contains the flat input as a member | `unit: cmd/sqlgen/gen: TestAPINestedSchema_EachShapeProjects` | all | |
| api.nested.flat-upsert-no-target-arg | `upsert<T>` takes no `conflictTarget` argument | `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_RequiresThePKConflictTarget/schema-declared key` | all | |
| api.nested.upsert-target-arg-default-pk | `upsert<T>WithRelated` takes `conflictTarget: <T>ConflictTarget! = PK` | `unit: cmd/sqlgen/gen: TestAPINestedSchema_FlatSurfaceIsByteIdentical`; `example: graphql: TestGraphQLNested_UpsertTakesAConflictTarget` | all | api-generation: nested-inputs × single-pk |
| api.nested.conflict-enum-values | `<T>ConflictTarget` declares `PK` for the primary-key target and the SCREAMING_SNAKE form of each unique target's columns | `unit: cmd/sqlgen/gen: TestAPINested_ConflictTargetsProjectAccess/public`; `unit: cmd/sqlgen/gen: TestAPINested_CompositeConflictTargetNeedsEveryColumnWritable/both public` | all | |
| api.nested.conflict-unique-writable | A unique target is declared only when every column is API-writable (`public` or `write_only`) | `unit: cmd/sqlgen/gen: TestAPINested_ConflictTargetsProjectAccess`; `unit: cmd/sqlgen/gen: TestAPINested_CompositeConflictTargetNeedsEveryColumnWritable` | all | |
| api.nested.conflict-pk-always | `PK` is declared whenever the table has a primary-key target, including over a `read_only` PK column | `unit: cmd/sqlgen/gen: TestAPINested_ConflictTargetsProjectAccess/read_only, read_only PK`; `unit: cmd/sqlgen/gen: TestAPINested_ConflictTargetsProjectAccess/public, read_only PK` | all | |
| api.nested.no-pk-target-required-arg | With no primary-key target the `conflictTarget` argument is required (no default) | `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_RequiresThePKConflictTarget/app-enforced key, UNIQUE column`; `golden: graphql` | all | |
| api.nested.no-target-omits-upsert | A table the access projection leaves with no conflict target gets no `upsert<T>WithRelated` | `unit: cmd/sqlgen/gen: TestAPINested_ConflictTargetsProjectAccess/read_only, app-enforced key`; `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_RequiresThePKConflictTarget/upsert-only mask, app-enforced key, read_only UNIQUE column` | all | |
| api.nested.no-conflict-pk-nested-stays | A table with no `<T>ConflictPK` has no `upsert<T>` but keeps `upsert<T>WithRelated` while a unique target is left | `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_RequiresThePKConflictTarget/app-enforced key, UNIQUE column`; `golden: graphql` | all | |
| api.nested.mysql-target-update-columns | On MySQL the nested upsert statement takes `ON DUPLICATE KEY UPDATE` with the `conflictTarget` shaping only the update half's column list | `unit: cmd/sqlgen/gen: TestAPINested_MySQLConflictTargetShapesOnlyTheUpdateList` | mysql | |
| api.nested.mysql-any-unique-takes-update | On MySQL any unique violation takes the nested upsert's update branch, whichever target was named | | mysql | |
| api.nested.unwritten-target-conflict | A call that leaves a column of the named target unwritten (the default `PK` on a generated key) carries no conflict clause and returns `CONFLICT` | `example: graphql: TestGraphQLNested_UpsertTakesAConflictTarget` | all | |
| api.nested.wrapper-member-names | A wrapper holds the flat input under the table's own name and one member per edge named like the object type's relationship field | `unit: cmd/sqlgen/gen: TestAPINestedSchema_EachShapeProjects`; `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides_NestedInputs`; `example: graphql: TestGraphQLNested_VerbsAreTheSchemaNotARuntimeCheck/CreateAssetWithRelatedInput` | all | api-generation: nested-inputs × soft-delete |
| api.nested.update-flat-nullable | On `Update<T>WithRelatedInput` the flat member is nullable | `unit: cmd/sqlgen/gen: TestAPINestedSchema_EachShapeProjects` | all | |
| api.nested.update-nested-only | An update that sets only nested members leaves the parent's columns untouched | `example: graphql: TestGraphQLNested_UpdateOnlyTouchesNestedRows` | all | |
| api.nested.update-nested-only-no-statement | An update that omits the flat member issues no parent `UPDATE` | `example: graphql: TestSurfacePromises_NestedUpdateWithoutFlatMemberIssuesNoParentUpdate` | all | |
| api.nested.verb-list | A list-shaped verb takes a list of non-null elements (`create: [X!]`, `connect: [ID!]`) | `unit: cmd/sqlgen/gen: TestAPINestedSchema_EachShapeProjects` | all | |
| api.nested.has-one-single | A has-one verb takes one value (`create: X`, `connect: ID`) | `unit: cmd/sqlgen/gen: TestAPINestedSchema_EachShapeProjects`; `example: graphql: TestGraphQLNested_HasOneIntKeyRelinks` | all | |
| api.nested.child-drops-fk | A nested child input omits the traversed FK | `unit: cmd/sqlgen/gen: TestAPINestedSchema_EachShapeProjects` | all | |
| api.nested.child-drops-discriminator | A nested child input omits the discriminator column | `golden: graphql` | all | |
| api.nested.child-is-projected-create | A nested child input is built from the target's access-projected create input | `unit: cmd/sqlgen/gen: TestAPINested_ChildInputDropsAccessProjectedColumn` | all | |
| api.nested.empty-child-no-create | A nested child input left with no field is not declared; the edge's GraphQL blocks carry no `create` and the Go surface keeps the verb | `unit: cmd/sqlgen/gen: TestAPINested_EmptyChildInputCarriesNoCreate`; `unit: cmd/sqlgen/gen: TestAPINested_UnmaskedProjectionIsTheGoSurface` | all | |
| api.nested.mask-conjoins-base | On the masked set each `…_with_related` implies its base: masking `create` removes `create<T>WithRelated` (likewise update, upsert) | `unit: cmd/sqlgen/gen: TestAPINested_ParentMaskConjoinsTheBase/create off`; `unit: cmd/sqlgen/gen: TestAPINested_ParentMaskConjoinsTheBase/update off`; `unit: cmd/sqlgen/gen: TestAPINested_ParentMaskConjoinsTheBase/upsert off`; `unit: cmd/sqlgen/gen: TestAPINested_ParentMaskConjoinsTheBase/read-only` | all | |
| api.nested.mask-own-key | Masking `create_with_related` removes `create<T>WithRelated` and keeps the other two | `unit: cmd/sqlgen/gen: TestAPINested_ParentMaskConjoinsTheBase/create_with_related off` | all | |
| api.nested.suppressed-flat-input | A family whose flat input is suppressed (no API-writable column) is not emitted | `unit: cmd/sqlgen/gen: TestAPINested_SuppressedFlatInputFamilyNotEmitted` | all | |
| api.nested.mask-not-emitted-flat | The requirement is on the masked operation: with no `<T>ConflictPK`, `upsert<T>WithRelated` survives without `upsert<T>` while `upsert` stays on the mask | `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_RequiresThePKConflictTarget/upsert-only mask, app-enforced key, UNIQUE column` | all | |
| api.nested.with-related-beside-masked-base-error | `…_with_related: true` beside its base set `false` in one mask is a config error naming the key, at the global and per-table level | `unit: cmd/sqlgen/config: TestValidateNestedMutations_APIMaskWithRelatedNeedsBase` | all | |
| api.nested.update-inc-own-tx | When `update<T>WithRelated` sets an `_inc` / `_dec` operator, `UpdateWithRelated`, the increments and the read-back run in one transaction | `unit: cmd/sqlgen/gen: TestAPINestedTemplates_IncrementBranch`; `example: graphql: TestGraphQLNested_IncrementsShareTheTransaction/the increment and the nested create both land` | all | |
| api.nested.update-inc-rollback | An increment that fails rolls back the nested writes of the same mutation | `example: graphql: TestGraphQLNested_IncrementsShareTheTransaction/an increment that fails rolls the nested create back` | all | |
| api.nested.update-inc-pairing | Setting and incrementing one column in `update<T>WithRelated` is `INVALID_INPUT` | `example: graphql: TestGraphQLNested_IncrementsShareTheTransaction/setting and incrementing one column is INVALID_INPUT` | all | |
| api.nested.error-path | A failure inside a nested block carries `extensions.path: ["<Edge>"]` | `example: graphql: TestGraphQLNested_ErrorCodesAndPath` | all | |
| api.nested.parent-failure-no-path | A failure of the flat half carries no `extensions.path` | `example: graphql: TestGraphQLNested_UpsertTakesAConflictTarget` | all | |
| api.nested.hidden-target-no-member | An edge into an `api.enabled: false` target is absent from every nested input type; the Go surface keeps it | `unit: cmd/sqlgen/gen: TestAPINested_HiddenTargetGetsNoMember`; `example: graphql: TestAPIHiddenTarget_SchemaNamesItNowhere`; `example: graphql: TestAPIHiddenTarget_GoClientKeepsTheEdge` | all | |
| api.nested.projected-empty-no-member | A target whose projected create input is empty gets no member at all | `unit: cmd/sqlgen/gen: TestAPINested_ProjectedEmptyTargetGetsNoMember` | all | |
| api.nested.target-create-gates-create | `create` needs the target's API `create`, plus `create_many` on an O2M edge | `unit: cmd/sqlgen/gen: TestAPINested_TargetMaskNarrowsTheVerbsThatWriteIt/create off keeps the relink verbs on an O2M`; `unit: cmd/sqlgen/gen: TestAPINested_TargetMaskNarrowsTheVerbsThatWriteIt/create_many off drops create on an O2M, which inserts through CreateMany`; `unit: cmd/sqlgen/gen: TestAPINested_TargetMaskNarrowsTheVerbsThatWriteIt/create_many off keeps create on a has-one, which inserts through Create`; `unit: cmd/sqlgen/gen: TestAPINested_TargetMaskNarrowsTheVerbsThatWriteIt/an M2M target's create gates create alone` | all | |
| api.nested.target-update-where-gates-relink | O2M / has-one `connect` / `disconnect` / `clear` need the target's `update_where`; M2M relink verbs do not | `unit: cmd/sqlgen/gen: TestAPINested_TargetMaskNarrowsTheVerbsThatWriteIt/update_where off keeps only create on an O2M`; `unit: cmd/sqlgen/gen: TestAPINested_TargetMaskNarrowsTheVerbsThatWriteIt/update_where off keeps only create on a has-one`; `unit: cmd/sqlgen/gen: TestAPINested_TargetMaskNarrowsTheVerbsThatWriteIt/an M2M target's update_where does not reach the junction-only verbs` | all | |
| api.nested.no-verb-no-member | An edge left with no verb gets no member | `unit: cmd/sqlgen/gen: TestAPINested_TargetMaskNarrowsTheVerbsThatWriteIt/a read-only target has no member` | all | |
| api.nested-inputs.single-pk | A single-PK parent with eligible edges gets the three `…WithRelatedInput` wrappers, `<T>ConflictTarget` and the per-edge nested types | `unit: cmd/sqlgen/gen: TestAPINestedSchema_EachShapeProjects`; `unit: cmd/sqlgen/gen: TestAPINestedSchema_EveryEmittedTypeIsClaimed` | all | api-generation: nested-inputs × single-pk |
| api.nested-inputs.composite-pk | A composite-PK parent with eligible edges gets the nested input types | | all | api-generation: nested-inputs × composite-pk |
| api.nested-inputs.soft-delete | A soft-delete parent gets the nested input types | `example: graphql: TestGraphQLNested_VerbsAreTheSchemaNotARuntimeCheck/UserEventsUpdateNested`; `golden: graphql` | all | api-generation: nested-inputs × soft-delete |
| api.nested-inputs.tenanted | A tenanted parent gets the nested input types | `golden: graphql` | all | api-generation: nested-inputs × tenanted |
| api.nested-inputs.view | A view gets no nested input type | `unit: cmd/sqlgen/gen: TestAPIViewSchema_declaresExactlySevenNames`; `example: graphql: TestViewSchema_ServesTheReadSet/WorkspaceNoteSummary` | all | api-generation: nested-inputs × view |

## §26.5.2 Field Selection Translation — The Central Mechanism
<!-- prd: 26.5.2 sha256: 007c5c1a9361426b -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.walker.per-entity-functions | Each entity gets a `<t>FieldOptionsFromContext` entry point and a recursive `<t>FieldOptionsFromCollected` walker | `unit: cmd/sqlgen/gen: TestWalker_PerTableEmissionShape` | all | |
| api.walker.column-case | Each API-readable column has a `case` that sets its `FieldOptions` boolean | `unit: cmd/sqlgen/gen: TestWalker_PerTableEmissionShape` | all | |
| api.walker.relationship-recurses | Each relationship `case` recurses into the target's own walker over the field's sub-selections | `unit: cmd/sqlgen/gen: TestWalker_PerTableEmissionShape`; `unit: cmd/sqlgen/gen: TestWalker_DeeplyNestedFixtureQuery` | all | |
| api.walker.sub-categorized | Several relationships into one target each get their own `case` | `unit: cmd/sqlgen/gen: TestWalker_SubCategorizedRelationships` | all | |
| api.field-walker.single-pk | A single-PK table's walker has a case per readable column and per exposed relationship | `unit: cmd/sqlgen/gen: TestWalker_PerTableEmissionShape` | all | api-generation: field-walker × single-pk |
| api.field-walker.composite-pk | A composite-PK table's walker has a case per key column | `unit: cmd/sqlgen/gen: TestRowIdentity_CompositePKProjectsEveryKeyColumn` | all | api-generation: field-walker × composite-pk |
| api.field-walker.soft-delete | A soft-delete table's walker has a case for the soft-delete column | `example: graphql: TestWalkerCompleteness_AllFieldsCovered`; `golden: graphql` | all | api-generation: field-walker × soft-delete |
| api.field-walker.tenanted | A tenanted table's walker has a case for the tenant column | `example: graphql: TestWalkerCompleteness_AllFieldsCovered`; `golden: graphql` | all | api-generation: field-walker × tenanted |
| api.field-walker.view | A view's walker has a case per readable column and no relationship case | `unit: cmd/sqlgen/gen: TestViewWalker_ColumnsOnly` | all | api-generation: field-walker × view |
| api.walker.connection-unwrap | Root `<T>Connection` and `<T>ListResult` selections are unwrapped (`edges` → `node`, `items`) by one shared helper in `connection_walker_gen.go` | `unit: cmd/sqlgen/gen: TestWalker_ConnectionUnwrap`; `golden: graphql` | all | |
| api.walker.fragments-directives | Fragments, `@skip` / `@include` and aliases are resolved through `graphql.CollectFieldsCtx` / `graphql.CollectFields` | `unit: cmd/sqlgen/gen: TestWalker_FragmentAndDirectiveCoverage` | all | |
| api.walker.alias-union | Every walker loop runs over a coalesced view where each field name appears once, carrying the union of every alias's sub-selections | `unit: cmd/sqlgen/gen: TestWalker_FragmentAndDirectiveCoverage`; `unit: cmd/sqlgen/gen: TestWalker_ConnectionUnwrap`; `unit: cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver: TestCoalesceAliases/two aliases of one relationship union their selections`; `unit: cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver: TestCoalesceAliases/three aliases union in first-appearance order` | all | |
| api.walker.alias-union-runtime | Two aliases of one envelope key, relationship or column each render their own selection correctly | `example: graphql: TestAliases_ConnectionEdges`; `example: graphql: TestAliases_ConnectionEdgesThreeWay`; `example: graphql: TestAliases_ConnectionNode`; `example: graphql: TestAliases_ListResultItems`; `example: graphql: TestAliases_Relationship`; `example: graphql: TestAliases_PlainColumns`; `example: graphql: TestAliases_OneToOneRelationship` | all | |
| api.walker.alias-union-no-extra-query | Two aliases of one O2M relationship still cost `1 + 1` queries | `example: graphql: TestAliases_RelationshipCostsOneQuery` | all | |
| api.walker.alias-row-identity-once | Row identity is evaluated once against the unioned result | `unit: cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver: TestRowIdentity_EvaluatedOnceOverTheAliasUnion` | all | |
| api.walker.coalesce-no-cache-write | The coalesce never writes into a slice gqlgen's `CollectFields` cache holds | `unit: cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver: TestCoalesceAliases_DoesNotAppendIntoGqlgensCache`; `unit: cmd/sqlgen/gen: TestWalker_ConnectionUnwrap` | all | |
| api.walker.lint-readable-column | Codegen fails when an API-readable column has no walker case, naming the entity and column | `unit: cmd/sqlgen/gen: TestWalker_CompletenessLint_NegativeTest`; `unit: cmd/sqlgen/gen: TestValidateAPIWalkerCompleteness_Access/readable column missing fails`; `unit: cmd/sqlgen/gen: TestValidateAPIWalkerCompleteness_view` | all | |
| api.walker.lint-exposed-relationship | Codegen fails when a relationship into an exposed entity has no case | `unit: cmd/sqlgen/gen: TestWalker_CompletenessLint_NegativeTest`; `unit: cmd/sqlgen/gen: TestWalker_CompletenessLint_Relationships/exposed without case` | all | |
| api.walker.lint-restricted-column | Codegen fails when an access-restricted column has a case, and accepts it absent | `unit: cmd/sqlgen/gen: TestValidateAPIWalkerCompleteness_Access/restricted column with live case fails`; `unit: cmd/sqlgen/gen: TestValidateAPIWalkerCompleteness_Access/restricted column absent passes`; `unit: cmd/sqlgen/gen: TestValidateAPIWalkerCompleteness_Access/restricted column fully omitted also passes`; `unit: cmd/sqlgen/gen: TestGraphQLWalker_AccessProjection` | all | |
| api.walker.lint-hidden-relationship | Codegen fails when a relationship into a hidden entity has a case, and accepts it absent | `unit: cmd/sqlgen/gen: TestWalker_CompletenessLint_Relationships/hidden with case`; `unit: cmd/sqlgen/gen: TestWalker_CompletenessLint_Relationships/hidden without case` | all | |
| api.walker.lint-holds-in-artifact | Every field of every generated object type has a walker case in the shipped artifact | `example: graphql: TestWalkerCompleteness_AllFieldsCovered`; `example: graphql: TestWalkerCompletenessLint_ErrorMessageFormat` | all | |
| api.walker.tests-pin-recursing-cases | sqlgen's walker tests pin a recursing case for every relationship | `unit: cmd/sqlgen/gen: TestWalker_DeeplyNestedFixtureQuery` | all | |
| api.walker.example-pins-nested-count | The GraphQL example's nested queries pin the response and the query count | `example: graphql: TestQueryCount_NestedRelationships` | all | |
| api.walker.no-generated-tests | sqlgen emits no test files into the consumer's packages | `unit: cmd/sqlgen/gen: TestGenerate_EmitsNoTestFiles` | all | |
| api.walker.unselected-nil | A relationship absent from the selection stays nil on the produced `FieldOptions` | `unit: cmd/sqlgen/gen: TestWalker_UnselectedRelationshipReturnsNil` | all | |
| api.walker.no-lazy-load | A selected relationship with no `FieldOptions` entry resolves to nil and is never lazy-loaded | `example: graphql: TestSurfacePromises_UnprefetchedRelationshipResolvesNilWithoutLoading` | all | |
| api.walker.required-columns-by-client | Columns the read derives (cursor keys, relationship keys, O2O NULL detection) are unioned in by the client method, so a narrowed request still gets what it asked for | `example: graphql: TestRequiredColumns_TableCursorWithoutSelectingKey`; `example: graphql: TestRequiredColumns_ViewCursorWithoutSelectingKey`; `example: graphql: TestRequiredColumns_O2MWithoutParentPK`; `example: graphql: TestRequiredColumns_O2OWithoutChildPK`; `example: graphql: TestRequiredColumns_M2MWithoutChildPK`; `example: graphql: TestRequiredColumns_CompositeCursorKeysWithoutSelectingThem`; `unit: cmd/sqlgen/gen: TestRequiredColumns_connectionDeclaresCursorKeys`; `unit: cmd/sqlgen/gen: TestRequiredColumns_viewConnectionDeclaresCursorKeys`; `unit: cmd/sqlgen/gen: TestRequiredColumns_m2mDeclaresTargetPK`; `unit: cmd/sqlgen/gen: TestRequiredColumns_o2mDeclaresChildFK` | postgres |  |
| api.walker.row-identity-guard | A selection set that names no column projects the entity's row-identity columns and nothing else | `unit: cmd/sqlgen/gen: TestRowIdentity_CollectedProjectsRowIdentity`; `unit: cmd/sqlgen/gen: TestRowIdentity_EveryAPIEntityCarriesRowIdentity` | all | |
| api.walker.row-identity-pk | A table's row identity is its primary key, every column of a composite one | `unit: cmd/sqlgen/gen: TestRowIdentity_CollectedProjectsRowIdentity`; `unit: cmd/sqlgen/gen: TestRowIdentity_CompositePKProjectsEveryKeyColumn` | all | |
| api.walker.row-identity-view-cursor-keys | A view with no `@pk` uses its resolved `cursor_keys` | `unit: cmd/sqlgen/gen: TestRowIdentity_ViewWithoutPKFallsBackToCursorKeys`; `example: graphql: TestRowIdentity_ViewCursorOnlyEdgeReturnsTheEdge` | all | |
| api.walker.row-identity-first-column | A view with neither uses its first column | `unit: cmd/sqlgen/gen: TestRowIdentity_ViewWithoutPKOrCursorKeysFallsBackToFirstColumn` | all | |
| api.walker.row-identity-runtime | `__typename`-only, `cursor`-only and `pageInfo`-only selections return their rows | `example: graphql: TestRowIdentity_CursorOnlyEdgeReturnsTheEdge`; `example: graphql: TestRowIdentity_EdgeTypenameOnlyReturnsTheEdge`; `example: graphql: TestRowIdentity_NodeTypenameOnlyReturnsTheNode`; `example: graphql: TestRowIdentity_PageInfoCursorsAreNotNull`; `example: graphql: TestRowIdentity_ListItemsTypenameOnlyReturnsTheItem`; `example: graphql: TestRowIdentity_GetByPKTypenameOnlyReturnsTheObject`; `example: graphql: TestRowIdentity_NestedM2MTypenameOnlyReturnsTheChildren`; `example: graphql: TestRowIdentity_NestedO2MTypenameOnlyStillReturnsTheChildren` | all | |
| api.walker.count-only-short-circuit | A count-only envelope (`{ totalCount }` on a Connection; `totalCount` / `hasMore` / `offset` / `limit` on a ListResult) returns the all-false `FieldOptions` without walking and costs one `COUNT` | `unit: cmd/sqlgen/gen: TestRowIdentity_EntryPointSkipsWalkOnCountOnlyEnvelope`; `example: graphql: TestRowIdentity_QueryCounts/connection totalCount only`; `example: graphql: TestRowIdentity_QueryCounts/list totalCount only`; `example: graphql: TestAliases_CountOnlyStillShortCircuits`; `example: graphql: TestRequiredColumns_TotalCountOnlyStaysOneQuery` | all | |
| api.walker.row-bearing-keys | `edges`, `pageInfo` (in full) and `items` are row-bearing, so a selection of any of them fetches the page | `unit: cmd/sqlgen/gen: TestRowIdentity_ConnectionWalkerReportsPageInfo`; `example: graphql: TestRowIdentity_QueryCounts/connection cursor only`; `example: graphql: TestRowIdentity_QueryCounts/connection pageInfo only`; `example: graphql: TestRowIdentity_QueryCounts/list items typename only`; `example: graphql: TestRowIdentity_PageInfoHasNextPageIsTrue` | all | |
| api.walker.get-row-always | A by-PK selection always wants its row: `{ __typename }` alone costs one query and returns the object | `example: graphql: TestRowIdentity_QueryCounts/get by PK typename only`; `example: graphql: TestRowIdentity_GetByPKTypenameOnlyReturnsTheObject` | all | |
| api.composite-edge.key-columns-forced | A GraphQL query selecting no key column still loads a composite-key edge: every referenced column and FK member is forced on |  | all |  |

## §26.5.3 Filter / Sort / Pagination Translation
<!-- prd: 26.5.3 sha256: 6c115883323766b0 -->

### Filter translator

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.filter.per-table-translator | A `translate<T>Filter` function is generated per exposed table, converting the GraphQL filter input into `*<pkg>.<T>Filter` | `unit: cmd/sqlgen/gen: TestFilterTranslator_AndOrNotNesting`; `golden: graphql` | all | |
| api.filter.nil-in-nil-out | `translate<T>Filter(nil)` returns nil | `unit: cmd/sqlgen/gen: TestFilterTranslator_AndOrNotNesting` | all | |
| api.filter.field-dispatch | Each non-nil column field is translated through its comparator translator into the same-named model filter field | `unit: cmd/sqlgen/gen: TestFilterTranslator_SkipsPKAndUnsupportedComparators`; `unit: cmd/sqlgen/gen: TestFilterProjection_pairsSchemaAndTranslator` | all | |
| api.filter.and-recursion | Each non-nil `and` sub-filter is translated by recursing into the same translator and appended to `out.And` | `unit: cmd/sqlgen/gen: TestFilterTranslator_AndOrNotNesting` | all | |
| api.filter.or-recursion | Each non-nil `or` sub-filter is translated by recursing into the same translator and appended to `out.Or` | `unit: cmd/sqlgen/gen: TestFilterTranslator_AndOrNotNesting` | all | |
| api.filter-translator.single-pk | A single-PK table without soft delete or tenancy gets a filter translator | `unit: cmd/sqlgen/gen: TestFilterTranslator_NoSoftDeleteWorksUnchanged`; `example: graphql: TestCuratedSurface_Category/UpdateMany`; `golden: graphql` | all | api-generation: filter-translator × single-pk |
| api.filter-translator.composite-pk | A composite-PK table gets a filter translator | `golden: graphql` | all | api-generation: filter-translator × composite-pk |
| api.filter-translator.soft-delete | A soft-delete table gets a filter translator, with its soft-delete column dispatched like any other nullable column | `unit: cmd/sqlgen/gen: TestFilterTranslator_DeletedAtIsRegularFilterField`; `example: graphql: TestCuratedSurface_User/UpdateMany` | all | api-generation: filter-translator × soft-delete |
| api.filter-translator.tenanted | A tenanted table gets a filter translator | `example: graphql: TestRelationshipFilterAPI_TenantedTargetIsScoped`; `example: graphql: TestRelationshipFilterAPI_SelfReferentialTwoLevels`; `golden: graphql` | all | api-generation: filter-translator × tenanted |
| api.filter-translator.view | A view gets a filter translator from the same template as a table | `unit: cmd/sqlgen/gen: TestAPILintCompleteness_ViewRoutesThroughTheSameLints`; `example: graphql: TestViewFilter_EnumNarrows`; `example: graphql: TestViewFilter_JSONBNarrows`; `golden: graphql` | all | api-generation: filter-translator × view |

### Comparator translators

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.comparator-translate.shared-file | Comparator translators live in `graph/comparator_translate_gen.go` | `golden: graphql` | all | |
| api.comparator-translate.shared-not-per-table | Comparator translators are shared across tables: one function per comparator-family variant, however many tables reference it | `unit: cmd/sqlgen/gen: TestEnumComparator_oneInputPerEnum`; `unit: cmd/sqlgen/gen: TestSliceComparator_oneInputPerElementType` | all | |
| api.comparator-translate.covers-every-family | Every comparator family projected in §26.4 has a translator that a referencing column dispatches to | `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType`; `unit: cmd/sqlgen/gen: TestFilterProjection_pairsSchemaAndTranslator` | all | |
| api.comparator-translate.copies-every-operator | Every operator a comparator input declares is assigned onto the model comparator by its translator | `unit: cmd/sqlgen/gen: TestComparatorFamilies_translatorCopiesEveryOperator` | postgres |  |
| api.comparator-translate.numeric-per-go-type | Numeric translators are monomorphized per concrete Go type and named `translateNumericComparator<T>` (e.g. `Int32`, `Int64`, `Float64`) | `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/integer`; `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/nullable_bigint`; `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/numeric_float`; `unit: cmd/sqlgen/gen: TestEnumComparator_disambiguatedFamilyCoexistsWithShipped` | all | |
| api.comparator-translate.numeric-cast | A numeric translator casts each `Float`/`Int` operand to the column's Go type during dispatch (`v := int32(*in.Eq); out.Eq = &v`) | `golden: graphql` | all | |
| api.comparator-translate.enum-per-enum | Enum translators are monomorphized per enum (`translate<Enum>Comparator`) | `unit: cmd/sqlgen/gen: TestEnumComparator_translatorEmission`; `unit: cmd/sqlgen/gen: TestEnumComparator_oneInputPerEnum` | all | |
| api.comparator-translate.enum-no-conversion | Enum translators copy operands without conversion, because the GraphQL enum is bound to the Go enum type | `unit: cmd/sqlgen/gen: TestEnumComparator_translatorEmission`; `example: graphql: TestEnumComparator_NarrowsOverHTTP` | all | |
| api.comparator-translate.slice-per-element | Slice translators are monomorphized per element type | `unit: cmd/sqlgen/gen: TestSliceComparator_oneInputPerElementType`; `unit: cmd/sqlgen/gen: TestSliceComparator_castVersusIdentity`; `unit: cmd/sqlgen/gen: TestSliceComparator_enumElementQualifiesModelsPackage` | postgres |  |
| api.comparator-translate.named-after-input | A monomorphized translator is named after the input it takes, so `<Enum>EnumComparator` translates through `translate<Enum>EnumComparator` | `unit: cmd/sqlgen/gen: TestEnumComparator_disambiguatesAgainstFixedFamilies`; `unit: cmd/sqlgen/gen: TestEnumComparator_disambiguatedFamilyCoexistsWithShipped` | all | |
| api.comparator-translate.nullable-takes-own-input | A nullable translator `translateNullable<X>Comparator` takes the `Nullable<X>Comparator` input and returns `comparator.Nullable<X>` | `unit: cmd/sqlgen/gen: TestComparatorFamilies_nullableTwin`; `unit: cmd/sqlgen/gen: TestComparatorFamily_StringPerDialect`; `unit: cmd/sqlgen/gen: TestEnumComparator_translatorEmission`; `unit: cmd/sqlgen/gen: TestJSONComparator_translatorBoxesTheDocument` | all |  |
| api.comparator-translate.nullable-no-delegation | A nullable translator copies the shared operands directly rather than delegating to the base translator | `unit: cmd/sqlgen/gen: TestComparatorFamily_StringPerDialect` | all | |
| api.comparator-translate.nullable-isnull | A nullable translator carries the input's `isNull` operand into the wrapper's `Null` field | `unit: cmd/sqlgen/gen: TestComparatorFamilies_nullableTwin`; `unit: cmd/sqlgen/gen: TestComparatorFamilies_translatorCopiesEveryOperator`; `example: graphql: TestNullableComparator_IsNullOverHTTP`; `example: graphql: TestNullableEnumComparator_OverHTTP` | all | |
| api.comparator-translate.base-only-for-not-null | The base translator is emitted only when a NOT NULL column references it; a nullable-only reference does not emit it | `unit: cmd/sqlgen/gen: TestComparatorTranslateFile_importsTimeForNullableOnly` | all | |
| api.comparator-translate.decimal-to-string | A decimal column translates through `translateDecimalComparator` into `comparator.String`, and its nullable twin into `comparator.NullableString` | `unit: cmd/sqlgen/gen: TestDecimalComparator_routing`; `unit: cmd/sqlgen/gen: TestDecimalComparator_narrowsOnlyTheStringFamily/price`; `example: graphql: TestDecimalComparator_OverHTTP` | all | |
| api.comparator-translate.decimal-operands-decimal | The decimal translator's GraphQL operands stay `Decimal`-typed and are rendered onto the canonical string form | `unit: cmd/sqlgen/gen: TestDecimalComparator_routing` | all | |
| api.comparator-translate.no-numeric-decimal | No `translateNumericComparatorDecimal` exists; a decimal column never reaches the `Number[T]` path | `unit: cmd/sqlgen/gen: TestDecimalComparator_routing` | all | |
| api.comparator-translate.opaque-per-type | Bytes, IP, CIDR, MacAddr and Duration columns each translate through their own function and its `Nullable` twin | `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/bytea`; `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/nullable_bytea`; `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/interval`; `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/nullable_interval`; `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/inet`; `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/nullable_inet`; `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/cidr`; `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/nullable_cidr`; `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/macaddr`; `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/nullable_macaddr` | all | |
| api.comparator-translate.opaque-no-conversion | Opaque translators copy pointers and slices straight across without conversion | `golden: graphql` | all | |
| api.comparator-translate.duration-between | `translateDurationComparator` carries `between` / `nbetween` into `comparator.Range[time.Duration]` | `example: graphql: TestScalarProbeFilters/Duration/between`; `golden: graphql` | all | |
| api.comparator-translate.ip-normalized-upstream | IP operands arrive at `translateIPComparator` already normalized by `UnmarshalIP`; no separate normalization translator exists | `example: graphql: TestScalarProbeFilters/IP/eq`; `example: graphql: TestScalarProbeFilters/IP/in_mixes_v4_and_v6` | postgres | |

### Relationship filters

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.filter.relationship-recursion | A relationship filter member translates by recursing into the target's own filter translator | `unit: cmd/sqlgen/gen: TestAPIFilterRelationships_TranslatorRecursesIntoTarget`; `unit: cmd/sqlgen/gen: TestAPIFilterRelationships_SelfReferentialInput`; `unit: cmd/sqlgen/gen: TestAPIFilterRelationships_CircularTargets`; `example: graphql: TestRelationshipFilterAPI_ColumnAndRelationshipInOneQuery`; `example: graphql: TestRelationshipFilterAPI_RelationshipUnderOr`; `example: graphql: TestRelationshipFilterAPI_ManyToMany` | all | |
| api.filter.relationship-hidden-target-api-disabled | A relationship whose target has `api.enabled: false` emits no filter member | `unit: cmd/sqlgen/gen: TestAPIFilterRelationships_APIDisabledTargetDropped` | all | |
| api.filter.relationship-hidden-target-unreadable | A relationship whose target has every column dropped by §32.2 emits no filter member | `unit: cmd/sqlgen/gen: TestAPIFilterRelationships_UnreadableTargetDropped` | all | |
| api.filter.relationship-hidden-target-object-field | The object type drops its relationship field into an API-excluded target | `example: graphql: TestAPIHiddenTarget_SchemaNamesItNowhere/User` | all | |
| api.filter.relationship-hidden-target-object-field-unreadable | The object type drops its relationship field into a target whose every column §32.2 drops | `unit: cmd/sqlgen/gen: TestAPIObjectType_DropsRelationshipIntoUnreadableTarget` | all | |
| api.filter.relationship-composite-pk-parent | A relationship filter member is emitted and translated for a composite-PK parent | | all | |
| api.filter.relationship-composite-fk-edge | A relationship filter member is emitted and translated for a multi-pair (composite) FK edge | | all | |

### One field map, two emitters

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.filter.one-projection | The GraphQL filter input and the filter translator are generated from a single per-column projection decision | `unit: cmd/sqlgen/gen: TestFilterProjection_pairsSchemaAndTranslator`; `unit: cmd/sqlgen/gen: TestAPILintCompleteness_BuiltContextsPassBothLints` | all | |
| api.filter.one-projection-relationship | Relationship members of the filter input and the translator are generated from a single member list | `unit: cmd/sqlgen/gen: TestAPIFilterRelationships_OneDerivationDrivesBothEmitters` | all | |
| api.sort.one-enum-value | The sort enum and the `SortFieldToColumn` switch read a single per-column enum-value decision | `unit: cmd/sqlgen/gen: TestSortProjection_digitLeadingColumn`; `unit: cmd/sqlgen/gen: TestSortProjection_everySortFieldMatchesSchemaEnum` | all | |
| api.sort.enum-value-spelling | The sort enum value is the §8.5 SCREAMING_SNAKE_CASE spelling with the `col_` guard on digit-leading names (`2010_revenue` → `COL_2010_REVENUE`), and the switch cases on the same token | `unit: cmd/sqlgen/gen: TestSortProjection_digitLeadingColumn` | all | |

### Completeness lint

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.lint.filter-schema-without-translator | The generator hard-errors on a schema-emitted filter field with no translator entry | `unit: cmd/sqlgen/gen: TestAPIFilterCompleteness_SchemaFieldWithoutTranslatorEntry` | all | |
| api.lint.filter-translator-without-schema | The generator hard-errors on a translator entry with no schema field | `unit: cmd/sqlgen/gen: TestAPIFilterCompleteness_TranslatorEntryWithoutSchemaField` | all | |
| api.lint.sort-value-without-case | The generator hard-errors on a sort enum value with no `SortFieldToColumn` case | `unit: cmd/sqlgen/gen: TestAPISortCompleteness_EnumValueWithoutCase`; `unit: cmd/sqlgen/gen: TestAPISortCompleteness_DigitLeadingColumnDrift` | all | |
| api.lint.sort-case-without-value | The generator hard-errors on a `SortFieldToColumn` case with no emitted enum value | `unit: cmd/sqlgen/gen: TestAPISortCompleteness_CaseWithoutEnumValue`; `unit: cmd/sqlgen/gen: TestAPISortCompleteness_DigitLeadingColumnDrift` | all | |
| api.lint.sort-case-unknown-column | The generator hard-errors on a case resolving to a column the entity does not have | `unit: cmd/sqlgen/gen: TestAPISortCompleteness_CaseReturnsUnknownColumn` | all | |
| api.lint.relationship-half-rendered | The generator hard-errors on a relationship member that renders only one of its two halves | `unit: cmd/sqlgen/gen: TestAPIFilterCompleteness_RelationshipMemberHalfRendered` | all | |
| api.lint.relationship-not-on-model | The generator hard-errors on a relationship member assigning a member the model `<T>Filter` does not have | `unit: cmd/sqlgen/gen: TestAPIFilterCompleteness_RelationshipMemberNotOnModelFilter` | all | |
| api.lint.relationship-collides-column | The generator hard-errors on a relationship member whose GraphQL name collides with a column field in the same input | `unit: cmd/sqlgen/gen: TestAPIFilterCompleteness_RelationshipMemberCollidesWithColumn` | all | |
| api.lint.built-context | The lint runs on the built context before any template is rendered | `unit: cmd/sqlgen/gen: TestAPILint_RunsOnTheBuiltContext` | all | |
| api.lint.covers-validate | `sqlgen validate` builds the same API context the completeness lint runs in | `unit: cmd/sqlgen/gen: TestValidateGeneration_BuildsTheLintedAPIContext` | all | |
| api.lint.views-same-functions | Views are linted by the same two functions as tables, with no view-specific arm | `unit: cmd/sqlgen/gen: TestAPILintCompleteness_ViewRoutesThroughTheSameLints` | all | |
| api.lint.absent-from-both-passes | A column that is filterable on neither side passes the lint because it is absent from both, with no exemption arm | `unit: cmd/sqlgen/gen: TestAPILintCompleteness_NonFilterableColumnPasses`; `unit: cmd/sqlgen/gen: TestAPILintCompleteness_AccessRestrictedColumnPasses`; `unit: cmd/sqlgen/gen: TestAPILintCompleteness_BuiltContextsPassBothLints` | all | |
| api.filter.nonfilterable-json-array | A `json[]` / `jsonb[]` column is filterable on neither side | `unit: cmd/sqlgen/gen: TestSliceJSONComparator_jsonArrayIsFilterableOnNeitherSide`; `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/jsonb_array`; `unit: cmd/sqlgen/gen: TestAPILintCompleteness_BuiltContextsPassBothLints` | postgres |  |
| api.filter.nonfilterable-sqlite-json | A `types.JSON` column under `input.dialect: sqlite` is filterable on neither side | `unit: cmd/sqlgen/gen: TestJSONComparator_sqliteModelSideNonFilterable` | sqlite | |
| api.filter.nonfilterable-access-role | A column whose access role drops the filter surface is filterable on neither side | `unit: cmd/sqlgen/gen: TestAPILintCompleteness_AccessRestrictedColumnPasses`; `unit: cmd/sqlgen/gen: TestEnumComparator_hiddenColumnEmitsNothing` | all | |
| api.filter.nonfilterable-no-projection-graphql | A column with no sound GraphQL projection (an enum-like Go type that is not a schema enum, or an array element outside the measured set) is dropped from the GraphQL filter input and translator | `unit: cmd/sqlgen/gen: TestEnumComparator_nonSchemaEnumDropsFilterField`; `unit: cmd/sqlgen/gen: TestFilterProjection_perGoType/timestamp_array`; `unit: cmd/sqlgen/gen: TestDecimalComparator_narrowsOnlyTheStringFamily/historic_prices` | all | |
| api.filter.nonfilterable-no-projection-go-kept | A column with no sound GraphQL projection keeps its member on the Go `<T>Filter` struct | `unit: cmd/sqlgen/gen: TestAPIFilter_NoProjectionColumnKeepsGoFilterMember` | all | |

### Soft-delete inclusion

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.filter.soft-delete-column-regular | The soft-delete column appears in the filter input as a regular comparator field, nullable under the timestamp strategy (`deletedAt: NullableTimeComparator`) | `unit: cmd/sqlgen/gen: TestFilterTranslator_DeletedAtIsRegularFilterField`; `golden: graphql` | all | |
| api.filter.soft-delete-empty-comparator-includes | Passing `deletedAt: {}` returns soft-deleted rows | `example: graphql: TestSurfacePromises_EmptyDeletedAtComparatorIncludesSoftDeleted` | all | |
| api.filter.no-include-deleted-flag | No `includeDeleted` flag is generated on the GraphQL surface or in the translator | `unit: cmd/sqlgen/gen: TestFilterTranslator_DeletedAtIsRegularFilterField`; `unit: cmd/sqlgen/gen: TestFilterTranslator_NoSoftDeleteWorksUnchanged` | all | |
| api.filter.no-include-deleted-model | The model `<Table>Filter` struct has no `IncludeDeleted` field | `unit: cmd/sqlgen/gen: TestFilterTemplate_softDeleteAsRegularComparator` | all | |

### Sort translator

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.sort.per-table-translator | A `translate<T>Sort` is generated per table, mapping `[]*<T>Sort` to `[]sql.Sort` | `unit: cmd/sqlgen/gen: TestSortTranslator_ColumnLookup`; `golden: graphql` | all | |
| api.sort.empty-returns-nil | `translate<T>Sort` returns nil for a nil or empty slice | `unit: cmd/sqlgen/gen: TestSortTranslator_ColumnLookup` | all | |
| api.sort.skips-nil-entries | `translate<T>Sort` skips nil entries | `unit: cmd/sqlgen/gen: TestSortTranslator_ColumnLookup` | all | |
| api.sort.field-to-column-switch | `<t>SortFieldToColumn` is a generated switch over the GraphQL sort-field enum returning the SQL column name | `unit: cmd/sqlgen/gen: TestSortTranslator_ColumnLookup` | all | |
| api.sort.raw-enum-string | `SortFieldToColumn` cases compare on the raw enum string (`case "CREATED_AT":`), not gqlgen's Go constants | `unit: cmd/sqlgen/gen: TestSortTranslator_ColumnLookup`; `unit: cmd/sqlgen/gen: TestSortProjection_digitLeadingColumn` | all | |
| api.sort.direction-shared | `directionToSortDirection` is emitted once per project and shared by every per-table translator | `unit: cmd/sqlgen/gen: TestSortTranslator_ColumnLookup`; `unit: cmd/sqlgen/gen: TestSortTranslator_NoTablesNoEmission`; `golden: graphql` | all | |
| api.sort.returns-sql-sort | The sort translator returns the runtime `sql.Sort`, not a separate `sort.Order` type | `unit: cmd/sqlgen/gen: TestSortTranslator_ColumnLookup` | all | |
| api.sort-translator.single-pk | A single-PK table without soft delete or tenancy gets a sort translator, and a sorted list honours it | `example: graphql: TestCuratedSurface_Category/List`; `golden: graphql` | all | api-generation: sort-translator × single-pk |
| api.sort-translator.composite-pk | A composite-PK table gets a sort translator | `golden: graphql` | all | api-generation: sort-translator × composite-pk |
| api.sort-translator.soft-delete | A soft-delete table gets a sort translator, the soft-delete column included | `unit: cmd/sqlgen/gen: TestSortTranslator_ColumnLookup`; `example: graphql: TestCuratedSurface_User/List` | all | api-generation: sort-translator × soft-delete |
| api.sort-translator.tenanted | A tenanted table gets a sort translator | `golden: graphql` | all | api-generation: sort-translator × tenanted |
| api.sort-translator.view | A view gets a sort translator, and a sorted view list honours it | `unit: cmd/sqlgen/gen: TestAPILintCompleteness_ViewRoutesThroughTheSameLints`; `example: graphql: TestViewTenancy_ListIsScoped`; `golden: graphql` | all | api-generation: sort-translator × view |

### Pagination

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.pagination.connection-passthrough | Connection args `first` / `after` / `last` / `before` are passed through to `ConnectionInput[F]` with no translation function | `unit: cmd/sqlgen/gen: TestConnectionPaginationPassthrough` | all | |
| api.pagination.connection-no-sort-arg | The Connection schema field takes no `sort` argument | `golden: graphql` | all | |
| api.pagination.connection-input-no-sort | `ConnectionInput[F]` has no `Sort` field | `golden: graphql` | all | |
| api.pagination.list-limit-offset | `<table>List` `limit` / `offset` map onto `PaginateInput[F]` | `unit: cmd/sqlgen/gen: TestListPaginationPassthrough` | all | |
| api.pagination.list-sort | `<table>List` `sort` is translated by `translate<T>Sort` and assigned into `PaginateInput.Sort` before `Paginate` is called | `example: graphql: TestCuratedSurface_User/List`; `example: graphql: TestCuratedSurface_Category/List`; `golden: graphql` | all | |

## §26.5.4 Mutation Translation
<!-- prd: 26.5.4 sha256: 1a8f4f83f7714edf -->

### Create / Update inputs

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.mutation.create-translator | `translateCreate<T>Input` maps the GraphQL create input onto `*<pkg>.Create<T>Input` | `unit: cmd/sqlgen/gen: TestInputTranslate_BinaryAndNetworkColumns`; `golden: graphql` | postgres |  |
| api.mutation.create-optional-omittable | An optional create field is set with `omittable.Set` only when present | `unit: cmd/sqlgen/gen: TestInputTranslate_NullableSizedIntGoesThroughALocal`; `unit: cmd/sqlgen/gen: TestInputTranslate_Float32ColumnsAreCast`; `unit: cmd/sqlgen/gen: TestInputTranslate_BinaryAndNetworkColumns` | all | |
| api.mutation.update-translator | `translateUpdate<T>Input` maps the GraphQL update input onto `*<pkg>.Update<T>Input` with `omittable.Value[T]` fields | `unit: cmd/sqlgen/gen: TestInputTranslate_SQLNullWrappers`; `unit: cmd/sqlgen/gen: TestInputTranslate_Float32ColumnsAreCast`; `golden: graphql` | all | |
| api.mutation.null-clears | An explicit `null` on a nullable update field sets the column to NULL | `example: graphql: TestUpdateOperators_ExplicitNullClearsColumn`; `unit: cmd/sqlgen/gen: TestUpdateInput_NullableColumnIsOmittable`; `unit: cmd/sqlgen/wrapper: TestMerge_ModelFieldsWritesOmittable` | all | |
| api.mutation.omitted-unchanged | An omitted update field leaves the column unchanged | `example: graphql: TestUpdateOperators_ExplicitNullClearsColumn` | all | |

### Increment / Decrement operators

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.inc-dec.no-root-mutation | `Increment` / `Decrement` are not exposed as root mutations | `unit: cmd/sqlgen/gen: TestResolvers_CuratedSurface_NoExcludedMethods` | all | |
| api.inc-dec.pair-per-numeric | `Update<T>Input` carries a `<field>_inc` / `<field>_dec` pair for every incrementable column | `unit: cmd/sqlgen/gen: TestIncrementColumns_ExcludeForeignKeys`; `unit: cmd/sqlgen/gen: TestTenantColumn_IncrementOperatorsSuppressed`; `unit: cmd/sqlgen/gen: TestResolvers_MixedNumeric_BothInIncrementEnum`; `golden: graphql` | all | |
| api.inc-dec.dispatch-increment | The operators dispatch to the client's generated `Increment` method keyed by the `<T>IncrementColumn` enum | `unit: cmd/sqlgen/gen: TestResolvers_NumericColumns_KeepsIncrementOp`; `unit: cmd/sqlgen/gen: TestResolvers_DecimalOnly_KeepsIncrementOp` | postgres |  |
| api.inc-dec.int-typed | Every `_inc` / `_dec` operator is typed `Int`, also on a `Decimal` or `Float` column | `unit: cmd/sqlgen/gen: TestGraphQLSchema_emitsExpectedTypesPerTable`; `golden: graphql` | all | |
| api.inc-dec.int-delta | The delta reaches the client's `Increment` as an integer amount | `golden: graphql` | all | |
| api.inc-dec.only-when-incrementable | A table with no incrementable column gets no operators and no increment dispatch | `unit: cmd/sqlgen/gen: TestResolvers_NoNumericColumns_OmitsIncrementOp`; `unit: cmd/sqlgen/gen: TestTenantColumn_SoleIncrementableSuppressesSurface` | all | |
| api.inc-dec.eligibility-shared | Column eligibility for the operators is the §8.2 increment-eligibility test (no PK, FK, tenant, generated or non-arithmetic column; duration excluded) | `unit: cmd/sqlgen/gen: TestIncrementColumns_ExcludeForeignKeys`; `unit: cmd/sqlgen/gen: TestTenantColumn_ExcludedFromAPIUpdateOps`; `unit: cmd/sqlgen/gen: TestDurationColumn_isNotIncrementable`; `unit: cmd/sqlgen/gen: TestAPIIncrementSurfaceFollowsOperations`; `unit: cmd/sqlgen/gen: TestResolvers_DecimalOnly_APITableContextHasIncrementColumns` | all | |
| api.inc-dec.no-operations-key | `increment` has no `api.operations` key | `unit: cmd/sqlgen/config: TestValidateAPIOperations_ClientOnlyRejected` | all | |
| api.inc-dec.rides-update-input | The operators sit on `Update<T>Input`, which is emitted whenever the mask leaves `update` or `update_where` | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_UpdateWhereAloneCarriesTheUpdateSurface/generated files` | all | |
| api.inc-dec-operators.single-pk | A single-PK table without soft delete or tenancy gets the operator pair and its dispatch | `unit: cmd/sqlgen/gen: TestResolvers_IncDec_DispatchOrder`; `unit: cmd/sqlgen/gen: TestResolvers_NumericColumns_KeepsIncrementOp`; `golden: graphql` | all | api-generation: inc-dec-operators × single-pk |
| api.inc-dec-operators.composite-pk | A composite-PK table gets the operator pair and its dispatch | `golden: mysql` | all | api-generation: inc-dec-operators × composite-pk |
| api.inc-dec-operators.soft-delete | A soft-delete table gets the operator pair and its dispatch | `golden: graphql` | all | api-generation: inc-dec-operators × soft-delete |
| api.inc-dec-operators.tenanted | A tenanted table gets the operator pair on its non-tenant numeric columns, and `_inc` applies alongside a set field | `unit: cmd/sqlgen/gen: TestTenantColumn_IncrementOperatorsSuppressed`; `example: graphql: TestTenantUpdateInput_NormalUpdateStillWorks` | all | api-generation: inc-dec-operators × tenanted |
| api.inc-dec-operators.view | A view gets no `_inc` / `_dec` operators, no increment enum and no update input | `unit: cmd/sqlgen/gen: TestValidateAPIViewReadOnly/increment`; `unit: cmd/sqlgen/gen: TestValidateAPIViewReadOnly/increment enum`; `unit: cmd/sqlgen/gen: TestValidateAPIViewReadOnly/update input`; `unit: cmd/sqlgen/gen: TestAPIViewSchema_emitsReadOnlySurface` | all | api-generation: inc-dec-operators × view |

### Plan order

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.inc-dec.plan-set-first | The update resolver runs `Update(id, set)` before any increment | `unit: cmd/sqlgen/gen: TestResolvers_IncDec_DispatchOrder` | all | |
| api.inc-dec.plan-set-only-if-present | `Update` is issued only when at least one set field is present | `golden: graphql` | all | |
| api.inc-dec.plan-signed-increment | Each operator is an `Increment` on its column, a `_dec` carrying a negative delta | `unit: cmd/sqlgen/gen: TestResolvers_IncDec_DispatchOrder`; `example: graphql: TestUpdateOperators_EveryAdvertisedIncrementIsApplied` | all | |
| api.inc-dec.plan-same-client | Update and the increments run on the same client instance | `golden: graphql` | all | |

### Validation rules

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.inc-dec.runtime-set-inc-invalid | At runtime, `<field>` and `<field>_inc` together return a `gqlerror` with `extensions.code = INVALID_INPUT` | `unit: cmd/sqlgen/gen: TestResolvers_IncDec_RuntimeConflictGuards` | all | |
| api.inc-dec.runtime-inc-dec-invalid | At runtime, `<field>_inc` and `<field>_dec` together return `INVALID_INPUT` | `unit: cmd/sqlgen/gen: TestResolvers_IncDec_RuntimeConflictGuards` | all | |
| api.inc-dec.runtime-set-dec-invalid | `<field>` and `<field>_dec` together return `INVALID_INPUT`, on the single-row, bulk and nested update paths | `unit: cmd/sqlgen/gen: TestResolvers_IncDec_RuntimeConflictGuards`; `example: graphql: TestUpdatePairing_SetAndDecrementIsInvalidInput`; `example: graphql: TestBulkUpdateOperators_PairingIsInvalidInput/set and decrement on one column` | all | |

### Bulk inputs

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.mutation.create-many-input | `create<T>s` takes `[Create<T>Input!]!` | `unit: cmd/sqlgen/gen: TestResolvers_MutationResultNullability/CreateProducts returns []*Product`; `golden: graphql` | all | |
| api.mutation.update-where-input | `update<T>s` takes a `<T>Filter` plus a single `Update<T>Input` | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_UpdateWhereAloneCarriesTheUpdateSurface/generated files`; `unit: cmd/sqlgen/gen: TestResolvers_MutationResultNullability/UpdateProducts returns []*Product` | all | |
| api.mutation.update-where-applies-to-matches | `update<T>s` applies the input to every row matching the filter | `example: graphql: TestCuratedSurface_User/UpdateMany`; `example: graphql: TestCuratedSurface_Category/UpdateMany` | all | |
| api.mutation.bulk-inc-dec | Bulk variants apply the same `_inc` / `_dec` translation as the one-row update | `example: graphql: TestUpdateOperators_BulkIncrementIsApplied`; `example: graphql: TestBulkUpdateOperators_SetFieldAndIncrementApplyToTheSameRows`; `example: graphql: TestBulkUpdateOperators_ResultReflectsEveryOperator`; `unit: cmd/sqlgen/gen: TestResolvers_BulkUpdateDispatchesIncrementOperators`; `unit: cmd/sqlgen/gen: TestBulkUpdateResolver_OperatorsRunInOneTransaction` | postgres |  |
| api.mutation.bulk-inc-dec-pairing | The bulk update rejects the same operator pairings as the one-row update with `INVALID_INPUT` | `example: graphql: TestBulkUpdateOperators_PairingIsInvalidInput` | all | |

### Mutation results and resolver shapes

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.mutation.result-relationships | A mutation result can select relationship fields, loaded per the walked FieldOptions | `golden: graphql` | all | |
| api.resolver.impl-on-qm | Per-operation implementations live on `*sqlgenresolver.Q` (queries) and `*sqlgenresolver.M` (mutations), not on `*queryResolver` / `*mutationResolver` | `unit: cmd/sqlgen/gen: TestResolvers_QMShape_NoQueryResolverMethods`; `golden: graphql` | all | |
| api.resolver.seed-delegates | The gqlgen-owned resolver methods delegate to `r.Q.<Op>` / `r.M.<Op>` and carry no field-options or error-mapping logic | `unit: cmd/sqlgen/gen: TestSeeds_DelegateToQM`; `unit: cmd/sqlgen/gen: TestResolvers_IntegerOnly_UnchangedFromBaseline` | postgres |  |
| api.resolver.seed-one-line | Every seeded resolver body is a one-line delegation | `unit: cmd/sqlgen/gen: TestUpdateSeeds_DelegateInOneLine`; `unit: cmd/sqlgen/gen: TestSeeds_DelegateToQM` | all | |
| api.resolver.every-advertised-operator-dispatched | Every `_inc` / `_dec` operator the schema advertises is applied, including on a column added after the seed was written | `example: graphql: TestUpdateOperators_EveryAdvertisedIncrementIsApplied`; `unit: cmd/sqlgen/gen: TestUpdateSeeds_DelegateInOneLine` | all | |
| api.resolver.field-options-from-context | Each Q/M method sets `o.FieldOptions` from the walked context | `golden: graphql` | all | |
| api.resolver.call-options-from-http | Each Q/M method applies `callOptionsFromHTTP[<T>FieldOptions](ctx)` | `unit: cmd/sqlgen/gen: TestResolvers_CallOptionsFromHTTP_ExplicitTypeArg` | all | |
| api.resolver.delete-hard-not-found-false | The bare `delete<T>` (hard-delete only) returns `false` with no error on `ErrNotFound` | `golden: graphql` | all | |
| api.resolver.relationship-field-preloaded | A relationship field returns the value preloaded by the parent read, with no DB call of its own | `example: graphql: TestQueryCount_NestedRelationships` | all | |

## §26.5.5 Error Mapping
<!-- prd: 26.5.5 sha256: 50e319979fb57015 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.errors.single-mapper | A single `mapErrorToGQL(err error) error` helper wraps every resolver return | `golden: graphql` | all | |
| api.errors.not-found-message | `database.ErrNotFound` maps to a `gqlerror.Error` with message `not found` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrNotFound`; `example: graphql: TestErrorCodes_NotFound` | all | |
| api.errors.not-found-code | `database.ErrNotFound` maps to `extensions.code` `NOT_FOUND` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrNotFound`; `example: graphql: TestErrorCodes_NotFound` | all | |
| api.errors.unique-message | `*database.ConstraintError` with `Type: ConstraintUnique` maps to message `duplicate` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrUniqueViolation`; `example: graphql: TestErrorCodes_Conflict` | all | |
| api.errors.unique-code | `*database.ConstraintError` with `Type: ConstraintUnique` maps to `extensions.code` `CONFLICT` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrUniqueViolation`; `example: graphql: TestErrorCodes_Conflict` | all | |
| api.errors.fk-message | `*database.ConstraintError` with `Type: ConstraintForeignKey` maps to message `fk constraint` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrForeignKeyViolation`; `example: graphql: TestErrorCodes_BadReference` | all | |
| api.errors.fk-code | `*database.ConstraintError` with `Type: ConstraintForeignKey` maps to `extensions.code` `BAD_REFERENCE` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrForeignKeyViolation`; `example: graphql: TestErrorCodes_BadReference` | all | |
| api.errors.check-message | `*database.ConstraintError` with `Type: ConstraintCheck` maps to message `check failed` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrCheckViolation`; `example: graphql: TestErrorCodes_InvalidInput_Check` | all | |
| api.errors.check-code | `*database.ConstraintError` with `Type: ConstraintCheck` maps to `extensions.code` `INVALID_INPUT` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrCheckViolation`; `example: graphql: TestErrorCodes_InvalidInput_Check` | all | |
| api.errors.not-null-message | `*database.ConstraintError` with `Type: ConstraintNotNull` maps to message `missing required` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrNotNullViolation`; `example: graphql: TestErrorCodes_InvalidInput_NotNull` | all | |
| api.errors.not-null-code | `*database.ConstraintError` with `Type: ConstraintNotNull` maps to `extensions.code` `INVALID_INPUT` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrNotNullViolation`; `example: graphql: TestErrorCodes_InvalidInput_NotNull` | all | |
| api.errors.already-related-message | `database.ErrAlreadyRelated` maps to message `already related` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrAlreadyRelated` | all | |
| api.errors.already-related-code | `database.ErrAlreadyRelated` maps to `extensions.code` `CONFLICT` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrAlreadyRelated`; `example: graphql: TestGraphQLNested_ErrorCodesAndPath/a target parented to another row is CONFLICT` | all | |
| api.errors.verb-conflict-message | `database.ErrNestedVerbConflict` maps to message `conflicting nested verbs` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrNestedVerbConflict` | all | |
| api.errors.verb-conflict-code | `database.ErrNestedVerbConflict` maps to `extensions.code` `INVALID_INPUT` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrNestedVerbConflict`; `example: graphql: TestGraphQLNested_ErrorCodesAndPath/one target under two verbs is INVALID_INPUT` | all | |
| api.errors.tenant-missing-message | `tenancy.ErrMissing` maps to message `tenant required` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrMissing` | all | |
| api.errors.tenant-missing-code | `tenancy.ErrMissing` maps to `extensions.code` `UNAUTHENTICATED` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrMissing`; `example: graphql: TestTenancy_MissingHeader_Unauthenticated`; `example: graphql: TestViewTenancy_MissingHeaderIsUnauthenticated` | all | |
| api.errors.tenant-mismatch-message | `tenancy.ErrMismatch` maps to message `tenant mismatch` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrMismatch` | all | |
| api.errors.tenant-mismatch-code | `tenancy.ErrMismatch` maps to `extensions.code` `FORBIDDEN` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/ErrMismatch`; `example: graphql: TestTenancy_PKMismatch_Forbidden`; `example: graphql: TestCompositePKCreate_TenantInPKVerifyMatch/mismatched tenant is forbidden` | all | |
| api.errors.other-message | Any other error maps to a `gqlerror.Error` whose message is `err.Error()` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/Fallthrough`; `unit: cmd/sqlgen/gen: TestMapErrorToGQL_FallthroughIsInternal` | all | |
| api.errors.other-code | Any other error maps to `extensions.code` `INTERNAL` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_PerSentinel/Fallthrough`; `unit: cmd/sqlgen/gen: TestMapErrorToGQL_FallthroughIsInternal` | all | |
| api.errors.sentinels-errors-is | The bare sentinels (`database.ErrNotFound`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`) are matched via `errors.Is`, so a `fmt.Errorf("…: %w", err)` wrap still maps | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_WrappedErrorsCaught` | all | |
| api.errors.constraint-errors-as | Constraint violations are matched by a single `errors.As` extraction of `*database.ConstraintError` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_WrappedErrorsCaught` | all | |
| api.errors.constraint-type-switch | The mapper dispatches the extracted `*database.ConstraintError` with a `switch ce.Type` over `ConstraintUnique` / `ConstraintForeignKey` / `ConstraintCheck` / `ConstraintNotNull` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_WrappedErrorsCaught` | all | |
| api.errors.no-direct-equality | The generated mapper uses no direct equality (`err == database.…`) | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_WrappedErrorsCaught` | all | |
| api.errors.no-constraint-sentinel-aliases | The runtime has no `ErrUniqueViolation` / `ErrForeignKeyViolation` / `ErrCheckViolation` / `ErrNotNullViolation` aliases | `unit: cmd/sqlgen/gen: TestRuntimeErrors_NoConstraintSentinelAliases` | all | |
| api.errors.nested-sentinels-errors-is | `ErrAlreadyRelated` and `ErrNestedVerbConflict` are matched via `errors.Is`, so the `*database.NestedMutationError` that carries them is absorbed | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_WrappedErrorsCaught`; `example: graphql: TestGraphQLNested_ErrorCodesAndPath` | all | |
| api.errors.connect-miss-not-found | A nested `connect` visibility miss maps to `NOT_FOUND` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_NestedVisibilityMissStaysNotFound`; `example: graphql: TestGraphQLNested_ErrorCodesAndPath/an O2M target that is not visible is NOT_FOUND`; `example: graphql: TestGraphQLNested_ErrorCodesAndPath/an M2M target that is not visible is NOT_FOUND` | all | |
| api.errors.connect-miss-never-bad-reference | A nested `connect` visibility miss never maps to `BAD_REFERENCE` | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_NestedVisibilityMissStaysNotFound` | all | |
| api.errors.nested-path-from-edge | When `errors.As` yields a `*database.NestedMutationError`, the mapper sets `extensions.path: ["<Edge>"]` from its `Edge` field | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_NestedFailureNamesItsEdge`; `example: graphql: TestGraphQLNested_ErrorCodesAndPath` | all | |
| api.errors.nested-path-any-code | `extensions.path` is set whatever code the nested failure maps to (a duplicate child stays `CONFLICT`, a missing FK target stays `BAD_REFERENCE`, both with the edge) | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_NestedFailureNamesItsEdge` | all | |
| api.errors.parent-failure-no-path | A failure outside every nested block, such as the parent's own write, carries no `path` | `example: graphql: TestGraphQLNested_UpsertTakesAConflictTarget` | all | |
| api.errors.edge-not-parsed-from-message | The edge is read off the structured error, never substring-matched out of the message | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_NestedFailureNamesItsEdge` | all | |

## §26.5.6 Module Boundary & Wrapper Subcommand
<!-- prd: 26.5.6 sha256: 5221188e0c694812 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.module.runtime-no-gqlgen | The sqlgen runtime module (`./`) does not depend on gqlgen | `unit: cmd/sqlgen/gen: TestAPIModuleBoundary_NoGqlgenDependency/runtime module` | all | |
| api.module.parser-no-gqlgen | `parser/` does not depend on gqlgen | `unit: cmd/sqlgen/gen: TestAPIModuleBoundary_NoGqlgenDependency/parser module` | all | |
| api.module.cmd-no-gqlgen | `cmd/sqlgen` does not import gqlgen | `unit: cmd/sqlgen/wrapper: TestNoGqlgenDependency` | all | |
| api.module.subprocess | The `sqlgen graphql gen` wrapper invokes the gqlgen binary as a subprocess | `unit: cmd/sqlgen/wrapper: TestGen_TempFileCleanupOnSuccess`; `unit: cmd/sqlgen/wrapper: TestGen_GoRunInheritsModMode`; `example: graphql: TestWrapperLayered_PreservesConsumerEntries` | all | |
| api.module.subprocess-go-run | The gqlgen binary is resolved from the consumer module via `go run github.com/99designs/gqlgen` | `unit: cmd/sqlgen/wrapper: TestSplitBin` | all | |
| api.wrapper.ownership-disk-untouched | The consumer's on-disk `gqlgen.yml` is left alone by the wrapper | `example: graphql: TestWrapperLayered_PreservesConsumerEntries` | all | |
| api.wrapper.ownership-merged-temp | sqlgen's required entries are merged into a temp config passed to the gqlgen subprocess | `example: graphql: TestWrapperLayered_PreservesConsumerEntries`; `unit: cmd/sqlgen/wrapper: TestGen_TempFileCleanupOnSuccess` | all | |
| api.wrapper.generate-chains | With `api.graphql.enabled: true`, `sqlgen generate` runs the sqlgen-side generation and then chains the wrapper flow inline | `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/api+graphql enabled, no opt-out`; `golden: graphql` | all | |
| api.wrapper.generate-chains-only-when-enabled | `sqlgen generate` does not chain the wrapper when the API or GraphQL block is disabled | `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/api disabled`; `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/graphql disabled`; `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/api block nil`; `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/graphql block nil` | all | |
| api.wrapper.graphql-gen-alone | `sqlgen graphql gen` runs the wrapper flow alone | `example: graphql: TestWrapperLayout_CrossLayoutCompiles`; `example: graphql: TestWrapperLayered_PreservesConsumerEntries` | all | |
| api.wrapper.no-graphql-flag | `--no-graphql` on `sqlgen generate` skips the chained call | `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/--no-graphql flag honored`; `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_NoGraphQLFlag` | all | |
| api.wrapper.no-graphql-env | `SQLGEN_NO_GRAPHQL=1` is equivalent to `--no-graphql` | `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/SQLGEN_NO_GRAPHQL=1 honored`; `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_EnvVar` | all | |
| api.wrapper.preflight-missing-fails | When `gqlgen.yml` is missing at `api.graphql.gqlgen_config`, `sqlgen generate` fails fast | `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_MissingGqlgenYml` | all | |
| api.wrapper.preflight-points-at-init | The pre-flight error points at `sqlgen graphql init` | `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_MissingGqlgenYml` | all | |
| api.wrapper.preflight-no-auto-init | `sqlgen generate` does not scaffold a `gqlgen.yml` as a side effect | `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_PreflightDoesNotScaffold/default path stays absent`; `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_PreflightDoesNotScaffold/mistyped configured path stays absent` | all | |
| api.wrapper.failure-no-rollback | When the gqlgen subprocess fails, the sqlgen-side files emitted earlier in the run stay on disk | `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_SubprocessFailureKeepsArtifacts` | all | |
| api.wrapper.failure-subprocess-message | A gqlgen subprocess failure prints `Error: gqlgen subprocess failed: <details>; sqlgen-side artifacts emitted at <output.dir>` | `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_WrapperErrorLabel/gqlgen binary fails` | all | |
| api.wrapper.failure-wrapper-label | A failure in the wrapper's own steps keeps the artifacts-emitted suffix but carries the `gqlgen wrapper` label | `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_WrapperErrorLabel/single-file resolver block without filename` | all | |
| api.wrapper.failure-unbound-own-error | An unbound root resolver name prints its own error rather than the subprocess or wrapper label | `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_UnboundRootFieldOwnError/prints its own error` | all | |
| api.wrapper.step2-reads-layout | The wrapper reads the consumer `gqlgen.yml`, including `resolver.layout` | `unit: cmd/sqlgen/wrapper: TestParseResolverLayout` | all | |
| api.wrapper.step3-models-tables | Step 3 merges `models:` bindings for managed tables | `unit: cmd/sqlgen/cli: TestBuildMergeInput_SignatureParity_RowStructsAndScalars`; `unit: cmd/sqlgen/wrapper: TestMerge_LayeredOnExistingMergesSqlgenEntries` | all | |
| api.wrapper.step3-schema-glob | Step 3 merges the schema glob | `unit: cmd/sqlgen/wrapper: TestMerge_LayeredOnExistingMergesSqlgenEntries`; `example: graphql: TestWrapperLayered_PreservesConsumerEntries` | all | |
| api.wrapper.step3-scalar-bindings | Step 3 merges a binding for every scalar the generated schema declares, whether it came from a column's Go type or an `api.graphql.scalars` entry | `unit: cmd/sqlgen/cli: TestBuildMergeInput_ScalarBindings`; `unit: cmd/sqlgen/cli: TestBuildMergeInput_ConsumerScalarBindings` | all | |
| api.wrapper.step3-unused-scalar-no-entry | A declared `api.graphql.scalars` entry no column uses gets no binding | `unit: cmd/sqlgen/cli: TestBuildMergeInput_UndeclaredScalarGetsNoEntry` | all | |
| api.wrapper.step3-consumer-wins | Consumer-authored keys win on collision | `unit: cmd/sqlgen/wrapper: TestMerge_ConsumerWinsOnScalarCollision`; `unit: cmd/sqlgen/wrapper: TestMerge_ConsumerWinsOnModelCollision` | all | |
| api.wrapper.step4-seed-follow-schema | Under `resolver.layout: follow-schema` the wrapper seeds one `graph/<schema>.resolvers.go` per managed `*_gen.graphqls` | `unit: cmd/sqlgen/wrapper: TestWriteSeedFiles_FollowSchemaOnlyWhenAbsent`; `unit: cmd/sqlgen/wrapper: TestWriteSeedFiles_ReturnsWrittenPaths_FollowSchema` | all | |
| api.wrapper.step4-seed-single-file | Under `single-file` the wrapper seeds the single `graph/resolver.go` | `unit: cmd/sqlgen/wrapper: TestWriteSeedFiles_SingleFile`; `unit: cmd/sqlgen/wrapper: TestWriteSeedFiles_ReturnsWrittenPaths_SingleFile` | all | |
| api.wrapper.step4-only-if-absent | Seeds are written only where absent, and existing files are left untouched | `unit: cmd/sqlgen/wrapper: TestWriteSeedFiles_FollowSchemaOnlyWhenAbsent` | all | |
| api.wrapper.step5-temp-config-exec | The wrapper writes the merged config to a temp file and runs gqlgen against it via `os/exec` | `example: graphql: TestWrapperLayered_PreservesConsumerEntries`; `unit: cmd/sqlgen/wrapper: TestGen_TempFileCleanupOnSuccess` | all | |
| api.wrapper.step6-stub-rewrite | The wrapper replaces each `panic("not implemented")` stub gqlgen emitted for a sqlgen-managed field with the delegation into `sqlgenresolver/` | `unit: cmd/sqlgen/wrapper: TestPostStubRewriter`; `unit: cmd/sqlgen/wrapper: TestPostStubRewriter_PrefersSeedBody`; `unit: cmd/sqlgen/wrapper: TestPostStubRewriter_FallsBackWhenUnseeded`; `example: graphql: TestWrapperRewriter_PanicStubFlipToDelegation` | all | |
| api.wrapper.step6-unmanaged-stubs-kept | Stubs for fields sqlgen does not manage are left alone | `unit: cmd/sqlgen/wrapper: TestPostStubRewriter`; `unit: cmd/sqlgen/wrapper: TestCheckManagedRootStubs/consumer stub and implemented managed fields pass` | all | |
| api.wrapper.step6-remaining-stub-fails | A managed field still a stub after the rewrite fails generation | `unit: cmd/sqlgen/wrapper: TestGen_failsOnManagedRootStub`; `unit: cmd/sqlgen/wrapper: TestCheckManagedRootStubs` | all | |
| api.wrapper.step7-temp-removed | The temp file is removed on success | `unit: cmd/sqlgen/wrapper: TestGen_TempFileCleanupOnSuccess` | all | |
| api.wrapper.step7-disk-untouched | The on-disk `gqlgen.yml` is untouched by the run | `example: graphql: TestWrapperLayered_PreservesConsumerEntries` | all | |
| api.wrapper.fieldname-entries | Step 3 merges a `fieldName` entry for every Go identifier sqlgen's generated code references on a gqlgen-generated type | `unit: cmd/sqlgen/wrapper: TestMerge_ModelFieldsWritesGoNames`; `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides_CoversEveryReferencedName`; `unit: cmd/sqlgen/gen: TestAPIFilterRelationships_GoFieldNamesDictatedToGqlgen` | all | |
| api.wrapper.fieldname-filter-create-update | Entries are emitted for `<T>Filter`, `Create<T>Input` and `Update<T>Input`, one per column field | `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides`; `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides_CoversEveryReferencedName` | all | |
| api.wrapper.fieldname-inc-dec | `Update<T>Input` carries entries for the `_inc` / `_dec` operator pair | `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides`; `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides_CoversEveryReferencedName` | all | |
| api.wrapper.fieldname-nested-members | Each nested-mutation wrapper's flat-input and edge members get entries | `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides_NestedInputs` | all | |
| api.wrapper.fieldname-nested-child-columns | Each nested child input's column fields take the same entries the target's `Create<T>Input` does | `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides_NestedInputs` | all | |
| api.wrapper.fieldname-verbs-none | The nested verb members (`create`, `connect`, `disconnect`, `clear`) take no entries | `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides_NestedInputs` | all | |
| api.wrapper.fieldname-literals-none | Literal names (comparator operands, `<T>Sort`'s `field` / `direction`, the filter's `and` / `or`, the connection envelope) get no entries | `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides` | all | |
| api.wrapper.fieldname-does-not-bind | A `fields:` entry carries no `model:` path, so the type stays gqlgen-generated | `unit: cmd/sqlgen/wrapper: TestMerge_ModelFieldsWritesGoNames` | all | |
| api.wrapper.root-no-fieldname | sqlgen emits no `fieldName` entry on `Query` / `Mutation` root fields | `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides_CoversEveryReferencedName` | all | |
| api.wrapper.root-seeds-sqlgen-spelling | The seeds declare sqlgen's own root resolver spelling (`Product`, `CreateProduct`, …) | `unit: cmd/sqlgen/gen: TestSeeds_DelegateToQM`; `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides_RootFieldPairsStayInStep` | all | |
| api.wrapper.root-stub-fails | Once gqlgen and the stub rewriter have run, generation fails if any sqlgen-managed root field is still a stub | `unit: cmd/sqlgen/wrapper: TestGen_failsOnManagedRootStub`; `unit: cmd/sqlgen/wrapper: TestCheckManagedRootStubs/follow-schema stubs gqlgen re-cased`; `unit: cmd/sqlgen/wrapper: TestCheckManagedRootStubs/single-file bare stub` | all | |
| api.wrapper.root-stub-exit-1 | The unbound-root-field failure exits `1` | `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_UnboundRootFieldOwnError/exits 1` | all | |
| api.wrapper.root-stub-names-entity | The error names the table or view | `unit: cmd/sqlgen/wrapper: TestUnboundRootFieldsError_namesEntityAndRemedy`; `unit: cmd/sqlgen/cli: TestManagedFieldOwners` | all | |
| api.wrapper.root-stub-names-spellings | The error names both spellings | `unit: cmd/sqlgen/wrapper: TestUnboundRootFieldsError_namesEntityAndRemedy`; `unit: cmd/sqlgen/wrapper: TestCheckManagedRootStubs` | all | |
| api.wrapper.root-stub-names-file | The error names the resolver file | `unit: cmd/sqlgen/wrapper: TestUnboundRootFieldsError_namesEntityAndRemedy`; `unit: cmd/sqlgen/wrapper: TestCheckManagedRootStubs` | all | |
| api.wrapper.root-stub-names-remedy | The error points at `tables.<table>.struct_name` / `views.<view>.struct_name` | `unit: cmd/sqlgen/wrapper: TestUnboundRootFieldsError_namesEntityAndRemedy`; `unit: cmd/sqlgen/gen: TestCollectSqlgenRootFields_configKey` | all |  |
| api.wrapper.rename-orphans-named | A rename that changes the file stem leaves the old schema and resolver files behind, and the next run's orphan check names them | `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/orphans under follow-schema layout name their resolver files` | all | |
| api.wrapper.bound-row-renamed-entry | The row type receives a `fieldName` entry for each `column_map.<column>.name`-renamed column | `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides_BoundRowType/override emits an entry for that field only` | all | |
| api.wrapper.bound-row-no-other-entry | The row type receives no `fieldName` entry for any other field | `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides_BoundRowType/no override emits no row-type entry`; `unit: cmd/sqlgen/gen: TestAPIGoFieldOverrides_BoundRowType/override emits an entry for that field only` | all | |
| api.wrapper.precedence-model | `model:` bindings follow consumer-wins: if the consumer declared the type, sqlgen leaves the binding alone | `unit: cmd/sqlgen/wrapper: TestMerge_ConsumerWinsOnModelCollision`; `unit: cmd/sqlgen/wrapper: TestMerge_ConsumerWinsOnScalarCollision` | all | |
| api.wrapper.precedence-fields-per-field | `fields:` entries merge per field, so sqlgen names fields on a type the consumer configured | `unit: cmd/sqlgen/wrapper: TestMerge_ModelFieldsConsumerFieldWins`; `unit: cmd/sqlgen/wrapper: TestMerge_ModelFieldsCoexistWithModelBinding`; `unit: cmd/sqlgen/wrapper: TestMerge_ModelFieldsConsumerTypeWins` | all | |
| api.wrapper.precedence-fieldname-not-overwritten | An explicit `fieldName` in the consumer's file is never overwritten | `unit: cmd/sqlgen/wrapper: TestMerge_ModelFieldsConsumerFieldWins` | all | |
| api.wrapper.skip-validation-forced | Under `resolver.layout: single-file`, `skip_validation` is set in the merged temp config | `example: graphql: TestWrapperLayout_CrossLayoutCompiles/single-file` | all | |
| api.wrapper.skip-validation-overrides-consumer | The `skip_validation` override applies even when the consumer declares the key | `unit: cmd/sqlgen/wrapper: TestGen_SkipValidationOverride/single-file overrides a consumer skip_validation false` | all | |
| api.wrapper.skip-validation-temp-only | The `skip_validation` override exists only in the temp config; the on-disk `gqlgen.yml` is never modified | `unit: cmd/sqlgen/wrapper: TestGen_SkipValidationOverride/single-file overrides a consumer skip_validation false`; `unit: cmd/sqlgen/wrapper: TestGen_SkipValidationOverride/single-file sets skip_validation the consumer left out` | all | |
| api.wrapper.omittable-owned | sqlgen's `omittable` key on a managed nullable update-input field overrides the consumer's value in the merged temp config, while the consumer's other per-field keys still win and the on-disk file is unchanged | `unit: cmd/sqlgen/wrapper: TestMerge_ModelFieldsSqlgenOmittableOverridesConsumer` | all | |
| api.wrapper.envelope-pageinfo-binding | The shared `PageInfo` binds directly to `<models-pkg>.PageInfo` | `unit: cmd/sqlgen/cli: TestBuildMergeInput_EnvelopeBinding`; `example: graphql: TestWrapperLayered_PreservesConsumerEntries` | all | |
| api.wrapper.envelope-no-bracket-path | No `model:` path uses the bracketed generic instantiation form (`<pkg>.Connection[<pkg>.<T>]`) | `unit: cmd/sqlgen/cli: TestBuildMergeInput_EnvelopeBinding` | all | |
| api.wrapper.envelope-alias-path-verbatim | The alias `model:` path reaches the merged config verbatim | `unit: cmd/sqlgen/wrapper: TestMerge_AliasBindingPathPreserved` | all | |
| api.wrapper.envelope-no-translate-helpers | No `translate<T>Connection` / `translate<T>Edge` / `translate<T>ListResult` helpers are emitted | `golden: graphql` | all | |
| api.wrapper.envelope-flow-through | The seed resolver's `r.Q.<T>s(...)` and `r.Q.<T>List(...)` return `*Connection[<T>]` / `*PaginateResult[<T>]` unchanged | `golden: graphql` | all | |
| api.wrapper.glob-init-scaffold | The `sqlgen graphql init` scaffold sets the `graph/*.graphqls` schema glob | `unit: cmd/sqlgen/wrapper: TestInit_GreenfieldScaffold` | all | |
| api.wrapper.glob-init-one-shot | `sqlgen graphql init` runs only if the file is missing | `unit: cmd/sqlgen/wrapper: TestInit_NoOverwriteOnExistingFile` | all | |
| api.wrapper.glob-enforced-each-run | The wrapper ensures the merged config includes the glob on each run | `unit: cmd/sqlgen/wrapper: TestMerge_LayeredOnExistingMergesSqlgenEntries`; `unit: cmd/sqlgen/wrapper: TestMerge_EmptyConsumerYAMLProducesValidConfig`; `unit: cmd/sqlgen/wrapper: TestMerge_PromotesScalarSchemaToList`; `unit: cmd/sqlgen/wrapper: TestMerge_DoesNotDuplicateSchemaGlob` | all | |
| api.wrapper.glob-custom-schema-merges | Consumer-authored schema files in the schema dir merge with sqlgen's generated ones | `unit: cmd/sqlgen/wrapper: TestMerge_LayeredOnExistingMergesSqlgenEntries`; `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/consumer schema files without the _gen suffix are left alone` | all | |

### Generated artifacts (per project)

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.artifacts.schema-files | sqlgen regenerates `graph/*_gen.graphqls` each run | `golden: graphql` | all | |
| api.artifacts.schema-orphan-fails | A `*_gen.graphqls` this run did not write fails generation rather than being deleted | `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans` | all | |
| api.artifacts.resolvers-file | sqlgen regenerates `graph/sqlgenresolver/resolvers_gen.go` (the `*Q` query and `*M` mutation implementations) each run | `golden: graphql` | all | |
| api.artifacts.field-options-file | sqlgen regenerates `graph/sqlgenresolver/field_options_gen.go` each run | `golden: graphql` | all | |
| api.artifacts.connection-walker-file | sqlgen regenerates `graph/sqlgenresolver/connection_walker_gen.go` each run | `golden: graphql` | all | |
| api.artifacts.resolvers-seeded-once | `graph/<schema>.resolvers.go` (follow-schema) or `graph/resolver.go` (single-file) is seeded only if absent | `unit: cmd/sqlgen/wrapper: TestWriteSeedFiles_FollowSchemaOnlyWhenAbsent` | all | |
| api.artifacts.resolvers-stubs-rewritten | Managed-field stubs in the delegating resolvers are rewritten each run | `unit: cmd/sqlgen/wrapper: TestPostStubRewriter`; `example: graphql: TestWrapperRewriter_PanicStubFlipToDelegation` | all | |
| api.artifacts.resolvers-consumer-preserved | Consumer-authored resolvers for unmanaged fields are preserved | `unit: cmd/sqlgen/wrapper: TestPostStubRewriter` | all | |
| api.artifacts.filter-translate-file | sqlgen regenerates `graph/filter_translate_gen.go` each run | `golden: graphql` | all | |
| api.artifacts.comparator-translate-file | sqlgen regenerates `graph/comparator_translate_gen.go` each run | `golden: graphql` | all | |
| api.artifacts.sort-translate-file | sqlgen regenerates `graph/sort_translate_gen.go` each run | `golden: graphql` | all | |
| api.artifacts.input-translate-file | sqlgen regenerates `graph/input_translate_gen.go` each run | `golden: graphql` | all | |
| api.artifacts.middleware-file | sqlgen regenerates `graph/sqlgenresolver/middleware_gen.go` each run | `golden: graphql` | all | |
| api.artifacts.scalars-file | sqlgen regenerates `graph/scalars_gen.go` each run | `golden: graphql` | all | |
| api.artifacts.errors-file | sqlgen regenerates `graph/sqlgenresolver/errors_gen.go` (`mapErrorToGQL`) each run | `golden: graphql` | all | |
| api.artifacts.generated-gen | gqlgen (via the wrapper) writes `graph/generated_gen.go` after each wrapper run | `golden: graphql` | all | |
| api.artifacts.gqlgen-models-file | gqlgen (via the wrapper) writes `graph/model/models_gen.go` after each wrapper run | `golden: graphql` | all | |
| api.artifacts.server-go | sqlgen writes `graph/server.go` when it is missing | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/server.go is written when absent` | all | |
| api.artifacts.server-go-kept | An existing `graph/server.go` is left untouched | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/an existing server.go is left untouched` | all | |
| api.artifacts.limits-file | sqlgen regenerates `graph/sqlgenresolver/limits_gen.go` each run | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/the limits file is regenerated with new values on a second run`; `golden: graphql` | all | |


### Envelope aliases


| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.envelope-aliases.single-pk-alias | A single-PK table gets `<T>Connection = Connection[<T>]`, `<T>Edge = Edge[<T>]` and `<T>ListResult = PaginateResult[<T>]` aliases in the models package's `api_envelopes_gen.go` | `golden: graphql` | all | api-generation: envelope-aliases × single-pk |
| api.envelope-aliases.single-pk-binding | The merged `models:` binds a single-PK table's `<T>Connection` / `<T>Edge` / `<T>ListResult` to those aliases | `unit: cmd/sqlgen/cli: TestBuildMergeInput_EnvelopeBinding`; `example: graphql: TestWrapperLayered_PreservesConsumerEntries`; `golden: graphql` | all | api-generation: envelope-aliases × single-pk |
| api.envelope-aliases.composite-pk-alias | A composite-PK table gets the same three aliases | `golden: graphql` | all | api-generation: envelope-aliases × composite-pk |
| api.envelope-aliases.composite-pk-binding | The merged `models:` binds a composite-PK table's three envelopes to its aliases | `golden: graphql` | all | api-generation: envelope-aliases × composite-pk |
| api.envelope-aliases.soft-delete-alias | A soft-delete table gets the same three aliases | `golden: graphql` | all | api-generation: envelope-aliases × soft-delete |
| api.envelope-aliases.soft-delete-binding | The merged `models:` binds a soft-delete table's three envelopes to its aliases | `example: graphql: TestWrapperLayered_PreservesConsumerEntries`; `golden: graphql` | all | api-generation: envelope-aliases × soft-delete |
| api.envelope-aliases.tenanted-alias | A tenanted table gets the same three aliases | `golden: graphql` | all | api-generation: envelope-aliases × tenanted |
| api.envelope-aliases.tenanted-binding | The merged `models:` binds a tenanted table's three envelopes to its aliases | `golden: graphql` | all | api-generation: envelope-aliases × tenanted |
| api.envelope-aliases.view-alias | A regular view gets the same three aliases | `golden: graphql` | all | api-generation: envelope-aliases × view |
| api.envelope-aliases.view-binding | The merged `models:` binds a view's object type and its three envelopes | `unit: cmd/sqlgen/cli: TestBuildMergeInput_ViewBindings`; `golden: graphql` | all | api-generation: envelope-aliases × view |
| api.envelope-aliases.materialized-view | A materialized view gets the same three aliases, bound the same way | `golden: graphql` | postgres | |

## §26.5.7 Why No Dataloader for Generated Code
<!-- prd: 26.5.7 sha256: 9ec5db0321dad4f3 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.dataloader.get-one-query | `product(id) { name }` is one `Get` | `example: graphql: TestQueryCount_SingleTable` | all | |
| api.dataloader.o2o-joined | `product(id) { company { name } }` is one `Get` with the O2O joined | `example: graphql: TestSurfacePromises_O2OSelectionIsOneQuery` | all | |
| api.dataloader.batched-in | Each selected O2M costs one batched `IN`, each M2M two, independent of depth | `example: graphql: TestQueryCount_NestedRelationships` | all | |
| api.dataloader.mutation-result-fieldoptions | A mutation's result relationships load through the same `FieldOptions` read the mutation chains internally | `example: graphql: TestSurfacePromises_MutationResultLoadsRelationshipsThroughFieldOptions` | all | |
| api.dataloader.count-independent-of-children | The query count never depends on the number of children | `example: graphql: TestSurfacePromises_QueryCountIndependentOfChildren` | all | |
| api.dataloader.chunked | A parent set larger than one chunk adds one query per extra chunk to each chunked step | `example: graphql: TestSurfacePromises_ChunkedParentSetAddsOneQueryPerChunk` | all | |
| api.dataloader.fail-fast | An un-prefetched relationship resolves to nil instead of lazy-loading | `example: graphql: TestSurfacePromises_UnprefetchedRelationshipResolvesNilWithoutLoading` | all | |

## §26.5.8 GraphQL Output Directories
<!-- prd: 26.5.8 sha256: 0f4007472e41b0cd -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.dirs.root-relative | `schema_dir` and `resolver_dir` are resolved from the project (module) root, not from `output.dir` | `unit: cmd/sqlgen/gen: TestResolveGraphDir` | all | |
| api.dirs.nested | `schema_dir: models/graph` resolves to `<root>/models/graph` | `unit: cmd/sqlgen/gen: TestResolveGraphDir/nested default` | all | |
| api.dirs.top-level | `schema_dir: graph` resolves to `<root>/graph` | `unit: cmd/sqlgen/gen: TestResolveGraphDir/top-level sibling`; `example: graphql_top_level: TestTopLevelGraphWiring`; `golden: graphql_top_level` | all | |
| api.dirs.custom | `schema_dir: api/graph` resolves to `<root>/api/graph` | `unit: cmd/sqlgen/gen: TestResolveGraphDir/custom` | all | |
| api.dirs.default | When unset, `schema_dir` / `resolver_dir` default to `<output.dir>/graph` | `unit: cmd/sqlgen/config: TestLoadConfig_GraphDirsRootRelativeDefault/unset defaults to <output.dir>/graph`; `unit: cmd/sqlgen/config: TestLoadConfig_GraphDirsRootRelativeDefault/unset with nested output.dir`; `unit: cmd/sqlgen/config: TestLoadConfig_GraphDirsRootRelativeDefault/unset with root output.dir` | all | |
| api.dirs.explicit-verbatim | Explicit values are taken verbatim as root-relative paths | `unit: cmd/sqlgen/config: TestLoadConfig_GraphDirsRootRelativeDefault/explicit top-level taken verbatim`; `unit: cmd/sqlgen/config: TestLoadConfig_GraphDirsRootRelativeDefault/explicit nested taken verbatim` | all | |
| api.dirs.no-mode-flag | There is no separate "layered" / "placement" mode flag; topology follows from the resolved path alone | `unit: cmd/sqlgen/config: TestLoadConfig_GraphTopologyHasNoModeFlag` | all | |
| api.dirs.import-path | The graph package's import path is `JoinModulePath(module, resolver_dir)`, with no `output.dir` prefix | `unit: cmd/sqlgen/gen: TestGraphImportPaths_RootRelative` | all | |
| api.dirs.imports-models-by-module-path | The graph package imports the models package by its full module path, so moving graph does not break the dependency | `unit: cmd/sqlgen/gen: TestGraphImportPaths_RootRelative`; `example: graphql_top_level: TestTopLevelGraphWiring` | all | |
| api.dirs.disabled-no-validation | With `api.graphql.enabled: false`, no `api.graphql.*` field is validated | `unit: cmd/sqlgen/config: TestValidatePreParse_APIGraphQLDisabledIsInert/disabled ignores invalid field_casing` | all | |
| api.dirs.disabled-no-emission | With `api.graphql.enabled: false`, nothing is resolved or emitted | `unit: cmd/sqlgen/gen: TestBuildAPIContext_DisabledIsInert` | all | |
| api.dirs.manifest-top-level | The manifest's `generation_config.graph_top_level` is true when the resolved graph dir is not under `output.dir` | `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_GraphTopLevel`; `unit: cmd/sqlgen/manifest: TestBuild_GenerationConfig_GraphTopLevel_DisabledAPI` | all | |

## §26.6 Query Complexity & Depth Limiting
<!-- prd: 26.6 sha256: a177f24f6c43a11d -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.complexity.max-depth-rejects | A query exceeding `api.graphql.max_depth` nesting depth is rejected before execution | `example: graphql: TestQueryLimits/one relationship level past max_depth is rejected` | all | |
| api.complexity.max-depth-boundary-passes | A query at exactly `max_depth` and within `max_complexity` runs | `example: graphql: TestQueryLimits/a query at max_depth and within max_complexity passes` | all | |
| api.complexity.depth-counts-relationships | Depth counts relationship fields only: connection and list wrappers and scalar fields add none | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/relationship fields are the ones that count toward depth`; `example: graphql: TestQueryLimits/a query at max_depth and within max_complexity passes` | all | |
| api.complexity.max-depth-default | `api.graphql.max_depth` defaults to 5 | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/defaults are depth 5 and complexity 1000 when unset` | all | |
| api.complexity.max-complexity-default | `api.graphql.max_complexity` defaults to 1000 | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/defaults are depth 5 and complexity 1000 when unset` | all | |
| api.complexity.relationship-adds | Each relationship field adds to the gqlgen query complexity | `example: graphql: TestQueryComplexity_RelationshipFieldAdds` | all | |
| api.complexity.limit-rejects | A query exceeding the complexity limit is rejected before execution | `example: graphql: TestQueryLimits/complexity past max_complexity is rejected` | all | |
| api.complexity.server-config | The depth and complexity protections are generated as gqlgen server configuration (`ApplyQueryLimits` on the `*handler.Server`), not custom middleware | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLServerCarriesQueryLimits`; `golden: graphql` | all | |
| api.complexity.limits-file-regenerated | `limits_gen.go` carries the configured values and is regenerated each run, so a later edit reaches an existing server | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/the limits file is regenerated with new values on a second run` | all | |
| api.complexity.server-applies-limits | The one-shot `server.go` applies the limits | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLServerCarriesQueryLimits` | all | |

## §26.7 GraphQL Authorization
<!-- prd: 26.7 sha256: 2aea2747be9687dc -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.auth.none-built-in | The generated code contains no auth (no auth directive, no context-based auth check) | `golden: graphql` | all | |
| api.auth.consumer-directives-kept | Consumer-declared gqlgen directives survive the wrapper merge, so consumers can inject auth through directive implementations | `unit: cmd/sqlgen/wrapper: TestMerge_LayeredOnExistingMergesSqlgenEntries`; `example: graphql: TestWrapperLayered_PreservesConsumerEntries` | all | |

## §26.9 Architecture
<!-- prd: 26.9 sha256: 6c425cbb4d4cea17 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.architecture.resolver-calls-client | The generated resolver translates the request and calls the unified client (`client.<Table>s().<Method>`) | `unit: cmd/sqlgen/gen: TestResolvers_PKArgUsesGqlgenDefaultBinding`; `unit: cmd/sqlgen/gen: TestResolvers_PKArgNoSelfConvertForStringPK` | all | |
| api.architecture.seeds-delegate | The seeded resolvers only delegate to `r.Q` / `r.M`, carrying no logic of their own | `unit: cmd/sqlgen/gen: TestSeeds_DelegateToQM` | all | |
| api.architecture.no-business-logic | No business logic lives in the generated API layer | | all | |

## §26.10 Per-Table API Configuration
<!-- prd: 26.10 sha256: 24181af74ef7d9d5 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.per-table.enabled-default | A table with no `tables.<t>.api.enabled` follows the global `api.enabled` | `unit: cmd/sqlgen/gen: TestApiEnabledFalseExcludesType` | all | |
| api.per-table.disabled-type-excluded | `api.enabled: false` excludes the table's type from the schema | `unit: cmd/sqlgen/gen: TestApiEnabledFalseExcludesType`; `unit: cmd/sqlgen/gen: TestBuildAPIContext_viewAPIEnabledFalseExcludesEntirely` | all | |
| api.per-table.disabled-operations-excluded | `api.enabled: false` excludes all of the table's queries and mutations | `unit: cmd/sqlgen/gen: TestApiEnabledFalseExcludesType` | all | |
| api.per-table.disabled-relationship-fields | `api.enabled: false` drops every relationship field into the table on another type | `unit: cmd/sqlgen/gen: TestAPIObjectType_APIDisabledTargetDropped`; `unit: cmd/sqlgen/gen: TestAPIObjectType_APIDisabledViewTargetDropped`; `example: graphql: TestAPIHiddenTarget_SchemaNamesItNowhere` | all | |
| api.per-table.disabled-go-client-keeps-edges | The Go client keeps the edges into a table with `api.enabled: false` | `unit: cmd/sqlgen/gen: TestAPIObjectType_APIDisabledTargetDropped`; `example: graphql: TestAPIHiddenTarget_GoClientKeepsTheEdge` | all | |
| api.per-table.disabled-stale-schema-named | Set on an already-generated tree, the table's old `*_gen.graphqls` stays behind and generation names it for removal | `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/orphans under follow-schema layout name their resolver files`; `unit: cmd/sqlgen/gen: TestCheckAPISchemaOrphans/an orphan with no resolver file names only the schema file` | all | |
| api.per-table.read-only-type-kept | `api.operations: read_only` keeps the type | `unit: cmd/sqlgen/gen: TestOperationsPreset_ReadOnly` | all | |
| api.per-table.read-only-queries | `api.operations: read_only` generates the Query fields | `unit: cmd/sqlgen/gen: TestOperationsPreset_ReadOnly` | all | |
| api.per-table.read-only-no-mutations | `api.operations: read_only` generates no Mutations | `unit: cmd/sqlgen/gen: TestOperationsPreset_ReadOnly`; `unit: cmd/sqlgen/gen: TestResolvers_OperationsPresetGating` | all | |
| api.per-table.no-delete | `api.operations: no_delete` excludes `delete*`, `softDelete*`, `hardDelete*` and `restore*` mutations | `unit: cmd/sqlgen/gen: TestOperationsPreset_NoDelete` | all | |
| api.per-table.preset | `api.operations: <preset name>` applies that preset's mask | `unit: cmd/sqlgen/gen: TestOperationsPreset_NoHardDelete`; `unit: cmd/sqlgen/gen: TestOperationsPreset_AppendOnly`; `unit: cmd/sqlgen/gen: TestAPIOperationsMask_GlobalAndPresetAndOverride` | all | |
| api.per-table.tenancy-required-unauthenticated | With `tenancy.required: true`, a tenant resolver failure surfaces as an `UNAUTHENTICATED` GraphQL error | `example: graphql: TestTenancy_MissingHeader_Unauthenticated`; `example: graphql: TestViewTenancy_MissingHeaderIsUnauthenticated` | all | |
| api.per-table.mask-intersects-client | The GraphQL generator emits the intersection of the client's methods and the table's resolved `api.operations` mask | `unit: cmd/sqlgen/gen: TestAPIOperationsMask_CannotExceedClient`; `unit: cmd/sqlgen/gen: TestAPIOperationsMask_PerTable` | all | |

## §26.12 Known Limitations
<!-- prd: 26.12 sha256: 4db5c3be7a0daf73 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api.limits.no-auth | The generated API has no built-in authentication or authorization | `golden: graphql` | all | |
| api.limits.no-row-security | The generated layer does not enforce row-level security beyond tenancy | | all | |
| api.limits.all-fields-returned | The generated resolvers return every field the schema exposes, with no field-level security | `example: graphql: TestGeneratedResolvers_ReturnEveryExposedField` | all | |
| api.limits.no-subscription-type | The generated schema has no `Subscription` type (only `Query` and `Mutation`) | `golden: graphql` | all | |
| api.limits.no-websocket | `graph/server.go` has no WebSocket transport | `golden: graphql` | all | |
| api.limits.no-subscription-resolver | No subscription resolver is generated | `golden: graphql` | all | |
| api.limits.no-relationship-field-args | Relationship fields carry no filter / sort arguments | `unit: cmd/sqlgen/gen: TestAPIObjectType_APIDisabledTargetDropped` | all | |
| api.limits.no-aggregate-predicates | No aggregate relationship predicates (`count: { gt: 3 }`) are generated | `unit: cmd/sqlgen/gen: TestAPIRelationshipFilter_NoAggregatePredicates` | all | |
| api.limits.opaque-array-no-filter | An array column whose element binds to a custom scalar gets no `<Elem>SliceComparator` and no `<T>Filter` field | `unit: cmd/sqlgen/gen: TestSliceComparator_customScalarElementKeepsFallback` | postgres | |
| api.limits.enum-like-no-filter | A column whose Go type is unqualified PascalCase but not a schema enum gets no `<Enum>Comparator` and no `<T>Filter` field | `unit: cmd/sqlgen/gen: TestEnumComparator_nonSchemaEnumDropsFilterField` | all | |
| api.limits.no-projection-no-translator | Such a column is absent from the filter translator | `unit: cmd/sqlgen/gen: TestSliceComparator_customScalarElementKeepsFallback`; `unit: cmd/sqlgen/gen: TestEnumComparator_nonSchemaEnumDropsFilterField` | all | |
| api.limits.no-projection-keeps-object-field | Such a column keeps its object-type field | `unit: cmd/sqlgen/gen: TestEnumComparator_nonSchemaEnumDropsFilterField` | all | |
| api.limits.no-projection-keeps-sort | Such a column keeps its sort-enum value | `unit: cmd/sqlgen/gen: TestEnumComparator_nonSchemaEnumDropsFilterField` | all | |
| api.limits.no-projection-keeps-go-filter | Such a column keeps its member on the Go `<T>Filter` struct | `unit: cmd/sqlgen/gen: TestFilterProjection_DroppedColumnKeepsGoFilterMember` | all | |
| api.limits.slice-family-spec-elements | The slice comparator family is emitted only for the spec built-ins (`String`, `ID`, `Int`, `Float`, `Boolean`) and bound GraphQL enums | `unit: cmd/sqlgen/gen: TestSliceComparator_oneInputPerElementType`; `unit: cmd/sqlgen/gen: TestSliceComparator_customScalarElementKeepsFallback` | postgres | |
| api.limits.nested-no-delete-verb | Nested mutations have no `delete` verb | `example: graphql: TestGraphQLNested_VerbsAreTheSchemaNotARuntimeCheck` | all | |
| api.limits.nested-no-belongs-to | Belongs-to nesting is not generated | `unit: cmd/sqlgen/gen: TestNestedEligibility_ListedShapeFailureIsReported/belongs-to edge` | all | |
| api.limits.nested-depth-one | Nested-mutation depth greater than 1 is not expressible: `max_depth` accepts only `1` | `unit: cmd/sqlgen/config: TestValidateNestedMutations_MaxDepth` | all | |
| api.limits.nested-reparent-opt-in | Re-parenting requires `allow_reparent: true` per edge; by default an O2M `connect` adopts only unparented rows | `unit: cmd/sqlgen/gen: TestNestedEligibility_AllowReparentNeedsNoNullGuard`; `example: graphql: TestGraphQLNested_ErrorCodesAndPath/a target parented to another row is CONFLICT` | all | |
| api.limits.nested-not-on-batch | `create<Table>s` / `update<Table>s` carry no nesting | `unit: cmd/sqlgen/gen: TestAPINestedSchema_FlatSurfaceIsByteIdentical` | all | |
| api.limits.nested-not-null-no-relink | A NOT NULL FK edge offers no `connect` / `disconnect` | `unit: cmd/sqlgen/gen: TestNestedEligibility_VerbSetPerShape`; `example: graphql: TestGraphQLNested_VerbsAreTheSchemaNotARuntimeCheck/UserOrdersUpdateNested` | all | |
| api.limits.nested-no-filter-edge | A `filter:` polymorphic edge gets no nested member | `unit: cmd/sqlgen/gen: TestNestedEligibility_FilterEdgeEmitsNoMember`; `example: graphql: TestGraphQLNested_VerbsAreTheSchemaNotARuntimeCheck/CreateAssetWithRelatedInput` | all | |
| api.limits.nested-no-m2m-discriminator | A discriminated M2M edge is not write-eligible | `unit: cmd/sqlgen/gen: TestNestedEligibility_M2MDiscriminatorIsNotWriteEligible` | all | |
| api.limits.nested-no-payload-junction | An M2M junction carrying any payload column is not write-eligible | `unit: cmd/sqlgen/gen: TestNestedEligibility_JunctionPayloadColumnIsIneligible` | all | |
| api.limits.self-connect-depth-zero | A nested `connect` on a self-referential edge naming the parent's own id is refused | `example: graphql: TestCreateWithRelated_SelfConnectRefused`; `example: graphql: TestUpdateWithRelated_SelfReferenceIsRefused` | all | |
| api.limits.self-connect-no-ring | Connecting a row to one of its own ancestors is not detected | `example: graphql: TestUpdateWithRelated_AncestorRingNotDetected` | all | |
| api.limits.upsert-nested-conflict-target | `upsert<Table>WithRelated` takes a `conflictTarget` argument | `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_RequiresThePKConflictTarget/schema-declared key`; `example: graphql: TestGraphQLNested_UpsertTakesAConflictTarget` | all | |
| api.limits.upsert-flat-pk-only | `upsert<Table>` is PK-only, with no `conflictTarget` argument | `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_RequiresThePKConflictTarget/schema-declared key` | all | |
| api.limits.struct-name-required | A table or view whose root field gqlgen spells differently from sqlgen fails generation with an error naming the entity and its `struct_name` key | `unit: cmd/sqlgen/wrapper: TestUnboundRootFieldsError_namesEntityAndRemedy`; `unit: cmd/sqlgen/wrapper: TestGen_failsOnManagedRootStub` | all | |
| api.limits.struct-name-no-auto-rename | Generation does not rename such an entity automatically | `unit: cmd/sqlgen/wrapper: TestCheckManagedRootStubs/follow-schema stubs gqlgen re-cased` | all | |
| api.limits.no-file-uploads | The generated API has no file-upload support (no `Upload` scalar) | `golden: graphql` | all | |

## Not applicable

| Matrix cell | Reason | Accepted |
|---|---|---|
| api-generation: connection-envelope × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: list-result-envelope × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: filter-input × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: sort-input × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: create-input × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: update-input × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: inc-dec-operators × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: relationship-field × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: relationship-filter-member × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: nested-inputs × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: get-query × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: connection-query × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: list-query × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: create × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: create-many × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: update × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: update-where × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: upsert × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: delete × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: hard-soft-delete-pair × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: restore × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: create-with-related × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: update-with-related × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: upsert-with-related × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: field-walker × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: filter-translator × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: sort-translator × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |
| api-generation: envelope-aliases × no-pk | §9.4b skips a table with no primary key from generation entirely, so it has no API surface (the absence is `api.object-type.no-pk`) | |

## PRD gaps

Behavior the code has, most of it pinned by tests, that no PRD text states. Each gap waits for
the user's ruling: amend the PRD, which turns the gap into requirement rows, or drop the pins.

| Gap | Behavior | Tests | Section | Ruling |
|---|---|---|---|---|
| gap.api.walker-lint-row-identity | The walker-completeness lint has a third direction: generation fails when an entity has API-readable columns but projects no row identity, projects a row-identity field that names no column, or projects an access-restricted column as its row identity | `unit: cmd/sqlgen/gen: TestRowIdentity_LintRejectsRestrictedPK`; `unit: cmd/sqlgen/gen: TestRowIdentity_LintDirections` | §26.5.2 | |
| gap.api.envelope-gating-parent-type | The envelope unwrap only fires when the parent GraphQL type is the table's `<T>Connection` / `<T>ListResult`, so a relationship or column literally named `items` or `edges` on the bare entity is walked as itself | `unit: cmd/sqlgen/gen: TestWalker_EnvelopeGatingNamedItemsRelationship` | §26.5.2 | |
| gap.api.overreach-validate-and-generate | The over-reach and flat-upsert warnings are emitted by both `sqlgen validate` and `sqlgen generate` | `unit: cmd/sqlgen/gen: TestAPIOverReachWarnings`; `unit: cmd/sqlgen/gen: TestAPIFlatUpsert_WarnsOnAnExplicitMask` | §26.5.1 | |
| gap.api.families-follow-surviving-edges | On the GraphQL surface `create<T>WithRelated` is dropped when no surviving edge carries `create` or `connect`, and `update` / `upsert` when no edge survives, while the Go client keeps all three | `unit: cmd/sqlgen/gen: TestAPINested_FamiliesFollowTheSurvivingEdges` | §26.5.1 | |
| gap.api.sort-unknown-value-empty | `<t>SortFieldToColumn` returns `""` for an enum value it has no case for, which the translator then passes on as the column | `golden: graphql` | §26.5.3 | |
| gap.api.direction-default-asc | `directionToSortDirection` maps every value other than `DESC` to `sql.Asc` | `unit: cmd/sqlgen/gen: TestSortTranslator_ColumnLookup` | §26.5.3 | |
| gap.api.lint-sortable-without-enum-value | The sort lint also hard-errors on a sortable column that resolved no enum value (a blank enum line) | `unit: cmd/sqlgen/gen: TestAPISortCompleteness_SortableColumnWithoutEnumValue` | §26.5.3 | |
| gap.api.lint-filterable-without-input-type | The filter lint also hard-errors on a filterable column with no comparator input type (`name: ` with nothing after it) | `unit: cmd/sqlgen/gen: TestAPIFilterCompleteness_FilterableColumnWithoutInputType` | §26.5.3 | |
| gap.api.inc-namespace-collision | Codegen rejects a table whose column is literally named `<numeric col>_inc` (it would duplicate the operator field), naming both columns | `unit: cmd/sqlgen/gen: TestResolvers_IncDec_ConflictRejectedAtCodegen` | §26.5.4 | |
| gap.api.update-refetch | `M.Update<T>` issues `Update` without field options, then the increments, then a separate `Get` with the walked FieldOptions to build the result | `golden: graphql` | §26.5.4 | |
| gap.api.create-many-skips-nil | The `create<T>s` seed skips nil list elements before translating | `golden: graphql` | §26.5.4 | |
| gap.api.wrapper-temp-removed-on-failure | The temp config is removed on subprocess failure too, not only on success (§26.5.6 step 7 says "on success") | `unit: cmd/sqlgen/wrapper: TestGen_TempFileCleanupOnSubprocessFailure` | §26.5.6 | |
| gap.api.wrapper-go-run-goflags | When `gqlgen_bin` is a `go run …` form, the subprocess gets `GOFLAGS=-mod=mod`; other forms inherit the parent env unchanged | `unit: cmd/sqlgen/wrapper: TestSubprocessEnv`; `unit: cmd/sqlgen/wrapper: TestGen_GoRunInheritsModMode` | §26.5.6 | |
| gap.api.wrapper-bin-split | `gqlgen_bin` is split on whitespace into argv; an empty value is an error | `unit: cmd/sqlgen/wrapper: TestSplitBin`; `unit: cmd/sqlgen/wrapper: TestGen_RejectsEmptyGqlgenBin` | §26.5.6 | |
| gap.api.wrapper-empty-config-path | An empty `gqlgen_config` path is rejected by the wrapper | `unit: cmd/sqlgen/wrapper: TestGen_RejectsMissingConfigPath` | §26.5.6 | |
| gap.api.wrapper-single-file-needs-filename | `resolver.layout: single-file` without `resolver.filename` fails before anything is seeded or gqlgen runs | `unit: cmd/sqlgen/wrapper: TestGen_SingleFileWithoutFilename` | §26.5.6 | |
| gap.api.wrapper-single-file-filename-ignores-dir | Under single-file, `resolver.filename` is the full path and `dir:` is ignored | `unit: cmd/sqlgen/wrapper: TestSingleFileLayout_FilenameIgnoresDir` | §26.5.6 | |
| gap.api.wrapper-malformed-yaml | A malformed consumer `gqlgen.yml` fails with a `parsing gqlgen.yml` error | `unit: cmd/sqlgen/wrapper: TestMerge_MalformedYAMLErrorPath`; `unit: cmd/sqlgen/wrapper: TestGen_MalformedConfigErrorPath` | §26.5.6 | |
| gap.api.wrapper-follow-schema-resolver-scaffold | Under follow-schema the wrapper also writes (or AST-merges into) `resolver.go` the `Client` / `Q` / `M` fields plus the `sqlgenresolver` import, preserving consumer fields, methods and imports | `unit: cmd/sqlgen/wrapper: TestWriteSeedFiles_FollowSchema_WritesSlimResolverScaffold`; `unit: cmd/sqlgen/wrapper: TestWriteResolverScaffold_MergesIntoExistingFile`; `unit: cmd/sqlgen/wrapper: TestWriteResolverScaffold_AbsentFile_WritesSlimScaffold`; `unit: cmd/sqlgen/wrapper: TestMergeResolverFields_NoMutations`; `unit: cmd/sqlgen/wrapper: TestMergeResolverFields_PreservesConsumerFields`; `unit: cmd/sqlgen/wrapper: TestMergeResolverScaffold_NoOpWhenFieldsAlreadyPresent`; `unit: cmd/sqlgen/wrapper: TestMergeResolverFields_NoOpReturnsEmptyPath` | §26.5.6 | |
| gap.api.wrapper-single-file-resolver-restore | Under single-file, after gqlgen runs, the wrapper AST-merges the `Client` / `Q` / `M` fields back into the `Resolver` struct gqlgen wiped (§26.5.6 mentions the merge only as the reason for `skip_validation`) | `unit: cmd/sqlgen/wrapper: TestMergeSingleFileResolver_RestoresWipedFields`; `unit: cmd/sqlgen/wrapper: TestMergeSingleFileResolver_FollowSchemaIsNoOp`; `unit: cmd/sqlgen/wrapper: TestMergeSingleFileResolver_AbsentFile`; `unit: cmd/sqlgen/wrapper: TestMergeSingleFileResolver_NoOpWhenFieldsAlreadyPresent` | §26.5.6 | |
| gap.api.wrapper-seed-gqlgen-model-import | Seed files import the gqlgen model package (alias `gqlmodel`) at the path derived from `gqlgen.yml`'s `model.filename`, defaulting to `<module>/graph/model` | `unit: cmd/sqlgen/wrapper: TestLoadGqlgenModelInfo`; `unit: cmd/sqlgen/wrapper: TestParseModelLayout`; `unit: cmd/sqlgen/wrapper: TestBuildSeedFileContents_GqlgenModelImport`; `unit: cmd/sqlgen/wrapper: TestWriteSeedFiles_FreshScaffold_ImportsGoimportsSorted`; `unit: cmd/sqlgen/gen: TestSeedTemplate_GqlgenAliasPrefix`; `unit: cmd/sqlgen/gen: TestPopulateAPIContextResolverFields_ReadsGqlgenModelLayout` | §26.5.6 |  |
| gap.api.wrapper-seed-paths-reported | `sqlgen generate` folds the seed files the wrapper wrote this run into its file report; files that already existed are not counted | `unit: cmd/sqlgen/cli: TestPrintGenerateReport_FoldsGraphQLFiles`; `unit: cmd/sqlgen/wrapper: TestWriteSeedFiles_ReturnsWrittenPaths_FollowSchema`; `unit: cmd/sqlgen/wrapper: TestWriteSeedFiles_ReturnsWrittenPaths_SingleFile` | §26.5.6 | |
| gap.api.wrapper-env-empty-not-opt-out | `SQLGEN_NO_GRAPHQL` opts out on any non-empty value (`true` included); an empty value does not | `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/SQLGEN_NO_GRAPHQL=true honored`; `unit: cmd/sqlgen/cli: TestShouldChainGraphQLGen/SQLGEN_NO_GRAPHQL empty does not opt out` | §26.5.6 | |
| gap.api.wrapper-preflight-exit-2 | The missing-`gqlgen.yml` pre-flight exits with code 2 (config error) | `unit: cmd/sqlgen/cli: TestGenerate_ChainedGraphQL_MissingGqlgenYml` | §26.5.6 | |
| gap.api.wrapper-envelope-skip-no-module | When the consumer module path is unknown (no `go.mod`), the row, envelope and `PageInfo` bindings are all skipped | `unit: cmd/sqlgen/cli: TestBuildMergeInput_EnvelopeBinding_SkippedWhenModuleUnknown` | §26.5.6 | |
| gap.api.wrapper-stub-rewriter-prefers-seed | The stub rewriter fills a newly stubbed managed field with the rendered seed body when one exists, and falls back to a synthesised pass-through delegation otherwise | `unit: cmd/sqlgen/wrapper: TestPostStubRewriter_PrefersSeedBody`; `unit: cmd/sqlgen/wrapper: TestPostStubRewriter_FallsBackWhenUnseeded` | §26.5.6 | |
| gap.api.errors-nil-short-circuit | `mapErrorToGQL(nil)` returns nil | `unit: cmd/sqlgen/gen: TestMapErrorToGQL_FallthroughIsInternal` | §26.5.5 | |
| gap.api.artifacts-extra-translators | sqlgen also emits `graph/comparator_translate_gen.go`, `graph/input_translate_gen.go` and `graph/sort_translate_gen.go`, which the §26.5.6 artifacts table does not list | `golden: graphql` | §26.5.6 | |
| gap.api.update-returns-soft-deleted | An `update<T>`, `update<T>s` or `update<T>WithRelated` mutation writes its set fields and any `_inc` / `_dec` operator to a row that its set fields soft-delete or that was already soft-deleted, and returns the row rather than `NOT_FOUND` or omitting it, with or without an operator | `example: graphql: TestBulkUpdateReadBack_SoftDeletingSetFieldReturnsTheRow`; `example: graphql: TestUpdateReadBack_SoftDeletingSetFieldReturnsTheRow`; `unit: cmd/sqlgen/gen: TestBulkUpdateResolver_RowKeysFollowTheKeyShape/soft-delete table reads back soft-deleted rows`; `unit: cmd/sqlgen/gen: TestUpdateResolver_SoftDeleteTableReadsBackByKey/single-column key`; `unit: cmd/sqlgen/gen: TestUpdateResolver_SoftDeleteTableReadsBackByKey/composite key`; `unit: cmd/sqlgen/gen: TestUpdateResolver_SoftDeleteTableReadsBackByKey/a table without soft delete keeps Get`; `unit: cmd/sqlgen/gen: TestUpdateResolver_SoftDeleteTableReadsBackByKey/a key column named key` | §26.5.4 | |
| gap.api.bulk-inc-readback-tx | When `update<T>s` carries an `_inc` / `_dec` operator, the resolver runs `UpdateWhere`, one `Increment` per matched row keyed by the row's PK (a `<T>PK` literal on a composite key), and one `GetMany` read-back filtered to the matched keys (`comparator.ID` In for a string-rendered key, `comparator.Number[T]` In for an integer key, `PKs` for a composite key) with `SkipCache` and hooks kept, all inside one `WithTx`; §26.5.4 states only that bulk variants share the `_inc` / `_dec` translation | `unit: cmd/sqlgen/gen: TestBulkUpdateResolver_OperatorsRunInOneTransaction`; `unit: cmd/sqlgen/gen: TestBulkUpdateResolver_RowKeysFollowTheKeyShape` | §26.5.4 |  |
| gap.api.translators-gqlmodel-qualified | When gqlgen's model package is known, the filter, sort, input and comparator translators name gqlgen's generated input types through the sqlgen-controlled `gqlmodel` alias (`translateProductFilter(in *gqlmodel.ProductFilter)`, `directionToSortDirection(d gqlmodel.SortDirection)`, `translateStringComparator(in *gqlmodel.StringComparator)`); the PRD's examples spell them unqualified | `unit: cmd/sqlgen/gen: TestFilterTranslate_GqlgenAliasPrefix`; `unit: cmd/sqlgen/gen: TestSortTranslate_GqlgenAliasPrefix`; `unit: cmd/sqlgen/gen: TestInputTranslate_GqlgenAliasPrefix`; `unit: cmd/sqlgen/gen: TestComparatorTranslate_GqlgenAliasPrefix` | §26.5.3, §26.5.4 |  |
| gap.api.gqlmodel-unresolved-bare | When `api.graphql.gqlgen_config` names a missing `gqlgen.yml`, generation does not fail: the gqlgen model import path and alias stay unset, and the seeds then name gqlgen's types bare (`filter *ProductFilter`, `input CreateProductInput`) | `unit: cmd/sqlgen/gen: TestPopulateAPIContextResolverFields_FailsSoftWhenGqlgenYmlMissing`; `unit: cmd/sqlgen/gen: TestSeedTemplate_NoAliasFallback` | §26.5.6 |  |
| gap.api.nested-completeness-lint | Codegen rejects a nested-mutation projection that advertises a verb the Go edge does not carry, has two wrapper members sharing one GraphQL name, has a child member the Go child input lacks, emits a create-side block with no create-side verb, or projects two conflict targets onto one `<T>ConflictTarget` value; the PRD states the walker and filter/sort completeness lints but no nested one | `unit: cmd/sqlgen/gen: TestValidateAPINestedCompleteness_Rejects` | §26.5.1, §9.9 |  |
