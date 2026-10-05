# Order

- **Table:** `orders` (schema `public`)
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
| `user_id` | UserID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `total` | Total | `decimal.Decimal` | `numeric(12, 2)` |  |  |  | `0` | `comparator.String` |  |
| `notes` | Notes | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` |  |
| `updated_at` | UpdatedAt | `*time.Time` | `timestamptz` | yes |  |  | `current_timestamp` | `comparator.NullableTime` |  |
| `deleted_at` | DeletedAt | `*time.Time` | `timestamptz` | yes |  |  |  | `comparator.NullableTime` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `orders_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| order_items | o2m | OrderItem | public.order_items.order_id |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*Order`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "created_at", "deleted_at", "id", "notes", "total", "updated_at", "user_id" FROM "public"."orders" WHERE "id" = $1 AND "deleted_at" IS NULL LIMIT 1
```

### GetMany

- Params: `input *GetOrdersInput`
- Returns: `[]*Order`, `error`

Generated SQL:

postgres:

```sql
SELECT "created_at", "deleted_at", "id", "notes", "total", "updated_at", "user_id" FROM "public"."orders" WHERE <filter> AND "deleted_at" IS NULL ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *OrderFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."orders" WHERE <filter> AND "deleted_at" IS NULL
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."orders" WHERE "id" = $1 AND "deleted_at" IS NULL)
```

### ExistsWhere

- Params: `filter *OrderFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."orders" WHERE <filter> AND "deleted_at" IS NULL)
```

### Paginate

- Params: `input PaginateInput[OrderFilter]`
- Returns: `*PaginateResult[Order]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[OrderFilter]`
- Returns: `*Connection[Order]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamOrdersInput`
- Returns: `iter.Seq2[*Order, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "created_at", "deleted_at", "id", "notes", "total", "updated_at", "user_id" FROM "public"."orders" WHERE <filter> AND "deleted_at" IS NULL ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateOrderInput`
- Returns: `*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."orders" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateOrderInput`
- Returns: `[]*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."orders" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateOrderInput`, `target OrderConflictTarget`
- Returns: `*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated OrderConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."orders" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateOrderInput`, `target OrderConflictTarget`
- Returns: `[]*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same OrderConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."orders" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateOrderInput`
- Returns: `*Order`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."orders" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateOrderItem`
- Returns: `[]*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."orders" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *OrderFilter`, `input *UpdateOrderInput`
- Returns: `[]*Order`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."orders" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id uuid.UUID`, `input IncrementInput[OrderIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated OrderIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."orders" SET <column> = <column> + $1 WHERE "id" = $2
```

### SoftDelete

- Params: `id uuid.UUID`
- Returns: `*Order`, `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."orders" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "id" = $1
```

### SoftDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `[]*Order`, `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."orders" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "id" IN (<ids>)
```

### SoftDeleteWhere

- Params: `filter *OrderFilter`
- Returns: `[]*Order`, `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."orders" SET "deleted_at" = CURRENT_TIMESTAMP WHERE <filter> AND "deleted_at" IS NULL RETURNING "id"
```

### Restore

- Params: `id uuid.UUID`
- Returns: `*Order`, `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."orders" SET "deleted_at" = $1 WHERE "id" = $2
```

### RestoreMany

- Params: `ids []uuid.UUID`
- Returns: `[]*Order`, `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."orders" SET "deleted_at" = $1 WHERE "id" IN (<ids>)
```

### RestoreWhere

- Params: `filter *OrderFilter`
- Returns: `[]*Order`, `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."orders" SET "deleted_at" = $1 WHERE <filter> AND "deleted_at" IS NOT NULL RETURNING "id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."orders" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."orders" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *OrderFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."orders" WHERE <filter> RETURNING "id"
```

### CreateWithRelated

- Params: `input *CreateOrderWithRelatedInput`
- Returns: `*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (OrderItems), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id uuid.UUID`, `input *UpdateOrderWithRelatedInput`
- Returns: `*Order`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (OrderItems), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertOrderWithRelatedInput`, `target OrderConflictTarget`
- Returns: `*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (OrderItems), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

## Filter

Type `OrderFilter`.

| Field | Type |
| --- | --- |
| CreatedAt | `*comparator.Time` |
| DeletedAt | `*comparator.NullableTime` |
| ID | `*comparator.ID` |
| Notes | `*comparator.NullableString` |
| Total | `*comparator.String` |
| UpdatedAt | `*comparator.NullableTime` |
| UserID | `*comparator.ID` |
| And | `[]*OrderFilter` |
| Or | `[]*OrderFilter` |

## Sort

Type `OrderSort`.

Fields: CreatedAt, DeletedAt, ID, Notes, Total, UpdatedAt, UserID
