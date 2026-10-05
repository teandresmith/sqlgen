# Product

- **Table:** `products` (schema `public`)
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
| `description` | Description | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `price` | Price | `decimal.Decimal` | `numeric(12, 2)` |  |  |  |  | `comparator.String` |  |
| `stock` | Stock | `int32` | `integer` |  |  |  | `0` | `comparator.Number[int32]` |  |
| `category_id` | CategoryID | `int64` | `bigint` |  |  |  |  | `comparator.Number[int64]` |  |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` |  |
| `updated_at` | UpdatedAt | `*time.Time` | `timestamptz` | yes |  |  | `current_timestamp` | `comparator.NullableTime` |  |
| `deleted_at` | DeletedAt | `*time.Time` | `timestamptz` | yes |  |  |  | `comparator.NullableTime` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `products_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| order_items | o2m | OrderItem | public.order_items.product_id |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*Product`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "category_id", "created_at", "deleted_at", "description", "id", "name", "price", "stock", "updated_at" FROM "public"."products" WHERE "id" = $1 AND "deleted_at" IS NULL LIMIT 1
```

### GetMany

- Params: `input *GetProductsInput`
- Returns: `[]*Product`, `error`

Generated SQL:

postgres:

```sql
SELECT "category_id", "created_at", "deleted_at", "description", "id", "name", "price", "stock", "updated_at" FROM "public"."products" WHERE <filter> AND "deleted_at" IS NULL ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ProductFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."products" WHERE <filter> AND "deleted_at" IS NULL
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."products" WHERE "id" = $1 AND "deleted_at" IS NULL)
```

### ExistsWhere

- Params: `filter *ProductFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."products" WHERE <filter> AND "deleted_at" IS NULL)
```

### Paginate

- Params: `input PaginateInput[ProductFilter]`
- Returns: `*PaginateResult[Product]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ProductFilter]`
- Returns: `*Connection[Product]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamProductsInput`
- Returns: `iter.Seq2[*Product, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "category_id", "created_at", "deleted_at", "description", "id", "name", "price", "stock", "updated_at" FROM "public"."products" WHERE <filter> AND "deleted_at" IS NULL ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateProductInput`
- Returns: `*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."products" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateProductInput`
- Returns: `[]*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."products" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateProductInput`, `target ProductConflictTarget`
- Returns: `*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated ProductConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."products" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateProductInput`, `target ProductConflictTarget`
- Returns: `[]*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same ProductConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."products" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateProductInput`
- Returns: `*Product`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."products" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateProductItem`
- Returns: `[]*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."products" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *ProductFilter`, `input *UpdateProductInput`
- Returns: `[]*Product`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."products" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id uuid.UUID`, `input IncrementInput[ProductIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated ProductIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."products" SET <column> = <column> + $1 WHERE "id" = $2
```

### SoftDelete

- Params: `id uuid.UUID`
- Returns: `*Product`, `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."products" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "id" = $1
```

### SoftDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `[]*Product`, `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."products" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "id" IN (<ids>)
```

### SoftDeleteWhere

- Params: `filter *ProductFilter`
- Returns: `[]*Product`, `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."products" SET "deleted_at" = CURRENT_TIMESTAMP WHERE <filter> AND "deleted_at" IS NULL RETURNING "id"
```

### Restore

- Params: `id uuid.UUID`
- Returns: `*Product`, `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."products" SET "deleted_at" = $1 WHERE "id" = $2
```

### RestoreMany

- Params: `ids []uuid.UUID`
- Returns: `[]*Product`, `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."products" SET "deleted_at" = $1 WHERE "id" IN (<ids>)
```

### RestoreWhere

- Params: `filter *ProductFilter`
- Returns: `[]*Product`, `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."products" SET "deleted_at" = $1 WHERE <filter> AND "deleted_at" IS NOT NULL RETURNING "id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."products" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."products" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *ProductFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."products" WHERE <filter> RETURNING "id"
```

### CreateWithRelated

- Params: `input *CreateProductWithRelatedInput`
- Returns: `*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (OrderItems), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id uuid.UUID`, `input *UpdateProductWithRelatedInput`
- Returns: `*Product`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (OrderItems), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertProductWithRelatedInput`, `target ProductConflictTarget`
- Returns: `*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (OrderItems), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

## Filter

Type `ProductFilter`.

| Field | Type |
| --- | --- |
| CategoryID | `*comparator.Number[int64]` |
| CreatedAt | `*comparator.Time` |
| DeletedAt | `*comparator.NullableTime` |
| Description | `*comparator.NullableString` |
| ID | `*comparator.ID` |
| Name | `*comparator.String` |
| Price | `*comparator.String` |
| Stock | `*comparator.Number[int32]` |
| UpdatedAt | `*comparator.NullableTime` |
| And | `[]*ProductFilter` |
| Or | `[]*ProductFilter` |

## Sort

Type `ProductSort`.

Fields: CategoryID, CreatedAt, DeletedAt, Description, ID, Name, Price, Stock, UpdatedAt
