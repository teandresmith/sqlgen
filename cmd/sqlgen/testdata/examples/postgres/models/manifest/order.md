# Order

- **Table:** `orders` (schema `public`)
- **Kind:** table

Customer purchase orders

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  | `gen_random_uuid()` | `comparator.ID` | Unique identifier |
| `user_id` | UserID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` | Ordering user reference |
| `status` | Status | `OrderStatus` | `order_status` |  |  |  | `'pending'` | `comparator.Enum[OrderStatus]` | Current lifecycle state |
| `total` | Total | `float64` | `numeric(10, 2)` |  |  |  | `0` | `comparator.Number[float64]` | Order total in the default currency |
| `notes` | Notes | `*string` | `text` | yes |  |  |  | `comparator.NullableString` | Optional order notes from the customer |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` | Row creation timestamp |
| `updated_at` | UpdatedAt | `*time.Time` | `timestamptz` | yes |  |  | `current_timestamp` | `comparator.NullableTime` | Last modification timestamp |

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
SELECT "created_at", "id", "notes", "status", "total", "updated_at", "user_id" FROM "public"."orders" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetOrdersInput`
- Returns: `[]*Order`, `error`

Generated SQL:

postgres:

```sql
SELECT "created_at", "id", "notes", "status", "total", "updated_at", "user_id" FROM "public"."orders" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *OrderFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."orders" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."orders" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *OrderFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."orders" WHERE <filter>)
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
SELECT "created_at", "id", "notes", "status", "total", "updated_at", "user_id" FROM "public"."orders" WHERE <filter> ORDER BY <sort>
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
- Errors: `ErrNilInput`, `ErrConstraintViolation`
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
- Notes: Atomic single-column arithmetic. The column is restricted to the generated OrderIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."orders" SET <column> = <column> + $1 WHERE "id" = $2
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

## Filter

Type `OrderFilter`.

| Field | Type |
| --- | --- |
| CreatedAt | `*comparator.Time` |
| ID | `*comparator.ID` |
| Notes | `*comparator.NullableString` |
| Status | `*comparator.Enum[OrderStatus]` |
| Total | `*comparator.Number[float64]` |
| UpdatedAt | `*comparator.NullableTime` |
| UserID | `*comparator.ID` |
| And | `[]*OrderFilter` |
| Or | `[]*OrderFilter` |

## Sort

Type `OrderSort`.

Fields: CreatedAt, ID, Notes, Status, Total, UpdatedAt, UserID
