# Asset

- **Table:** `assets` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  | `gen_random_uuid()` | `comparator.ID` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `declared_categories` | DeclaredCategories | `DocumentEntityTypeEnumSlice` | `document_entity_type_enum[]` |  |  |  | `'{}'` | `comparator.Slice[DocumentEntityTypeEnum]` |  |
| `primary_category` | PrimaryCategory | `*DocumentEntityTypeEnum` | `document_entity_type_enum` | yes |  |  |  | `comparator.NullableEnum[DocumentEntityTypeEnum]` |  |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `assets_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| Attachments | o2m | Document | public.documents.entity_id | `entity_type = 'asset.attachment'` |
| Invoices | o2m | Document | public.documents.entity_id | `entity_type = 'asset.invoice'` |
| LinkedAttachments | m2m | Document | public.asset_document_links (asset_id → document_id) | `entity_type = 'asset.attachment' AND name LIKE 'link_%'` |
| PhotoAttachments | o2m | Document | public.documents.entity_id | `entity_type = 'asset.attachment' AND name LIKE 'photo_%'` |
| PrimaryActiveDocument | o2o | Document | public.documents.entity_id | `entity_type = 'asset.primary' AND name LIKE 'site_%'` |
| PrimaryDocument | o2o | Document | public.documents.entity_id | `entity_type = 'asset.primary'` |
| documents | m2m | Document | public.asset_document_links (asset_id → document_id) |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*Asset`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "created_at", "declared_categories", "id", "name", "primary_category" FROM "public"."assets" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetAssetsInput`
- Returns: `[]*Asset`, `error`

Generated SQL:

postgres:

```sql
SELECT "created_at", "declared_categories", "id", "name", "primary_category" FROM "public"."assets" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *AssetFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."assets" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."assets" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *AssetFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."assets" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[AssetFilter]`
- Returns: `*PaginateResult[Asset]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[AssetFilter]`
- Returns: `*Connection[Asset]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamAssetsInput`
- Returns: `iter.Seq2[*Asset, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "created_at", "declared_categories", "id", "name", "primary_category" FROM "public"."assets" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateAssetInput`
- Returns: `*Asset`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."assets" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateAssetInput`
- Returns: `[]*Asset`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."assets" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateAssetInput`, `target AssetConflictTarget`
- Returns: `*Asset`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated AssetConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."assets" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateAssetInput`, `target AssetConflictTarget`
- Returns: `[]*Asset`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same AssetConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."assets" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateAssetInput`
- Returns: `*Asset`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."assets" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateAssetItem`
- Returns: `[]*Asset`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."assets" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *AssetFilter`, `input *UpdateAssetInput`
- Returns: `[]*Asset`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."assets" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."assets" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."assets" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *AssetFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."assets" WHERE <filter> RETURNING "id"
```

### CreateWithRelated

- Params: `input *CreateAssetWithRelatedInput`
- Returns: `*Asset`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (Attachments, Documents, Invoices, PrimaryDocument), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id uuid.UUID`, `input *UpdateAssetWithRelatedInput`
- Returns: `*Asset`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (Attachments, Documents, Invoices, PrimaryDocument), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertAssetWithRelatedInput`, `target AssetConflictTarget`
- Returns: `*Asset`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (Attachments, Documents, Invoices, PrimaryDocument), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

## Filter

Type `AssetFilter`.

| Field | Type |
| --- | --- |
| CreatedAt | `*comparator.Time` |
| DeclaredCategories | `*comparator.Slice[DocumentEntityTypeEnum]` |
| ID | `*comparator.ID` |
| Name | `*comparator.String` |
| PrimaryCategory | `*comparator.NullableEnum[DocumentEntityTypeEnum]` |
| And | `[]*AssetFilter` |
| Or | `[]*AssetFilter` |

## Sort

Type `AssetSort`.

Fields: CreatedAt, DeclaredCategories, ID, Name, PrimaryCategory
