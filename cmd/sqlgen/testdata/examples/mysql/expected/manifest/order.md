# Order

- **Table:** `orders`
- **Kind:** table

Customer purchase orders

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` | Unique identifier |
| `user_id` | UserID | `int64` | `bigint` |  |  |  |  | `comparator.Number[int64]` | Ordering user reference |
| `status` | Status | `OrdersStatusEnum` | `orders_status_enum` |  |  |  | `'pending'` | `comparator.Enum[OrdersStatusEnum]` | Current lifecycle state |
| `total` | Total | `float64` | `decimal(12,2)` |  |  |  |  | `comparator.Number[float64]` | Order total in default currency |
| `notes` | Notes | `*string` | `text` | yes |  |  |  | `comparator.NullableString` | Optional order notes from the customer |
| `ordered_at` | OrderedAt | `time.Time` | `datetime` |  |  |  | `current_timestamp()` | `comparator.Time` | Order placement timestamp |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `orders_pkey` | id | yes | btree |  |
| `user_id` | user_id |  | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| order_items | o2m | OrderItem | order_items.order_id |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Order`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

mysql:

```sql
SELECT `id`, `notes`, `ordered_at`, `status`, `total`, `user_id` FROM `orders` WHERE `id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetOrdersInput`
- Returns: `[]*Order`, `error`

Generated SQL:

mysql:

```sql
SELECT `id`, `notes`, `ordered_at`, `status`, `total`, `user_id` FROM `orders` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *OrderFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `orders` WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `orders` WHERE `id` = ?)
```

### ExistsWhere

- Params: `filter *OrderFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `orders` WHERE <filter>)
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

mysql:

```sql
SELECT `id`, `notes`, `ordered_at`, `status`, `total`, `user_id` FROM `orders` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateOrderInput`
- Returns: `*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `orders` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateOrderInput`
- Returns: `[]*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `orders` (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateOrderInput`, `target OrderConflictTarget`
- Returns: `*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated OrderConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `orders` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateOrderInput`, `target OrderConflictTarget`
- Returns: `[]*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same OrderConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `orders` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### Update

- Params: `id int64`, `input *UpdateOrderInput`
- Returns: `*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `orders` SET <set> WHERE `id` = ?
```

### UpdateMany

- Params: `items []UpdateOrderItem`
- Returns: `[]*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `orders` SET <set> WHERE `id` = ?
```

### UpdateWhere

- Params: `filter *OrderFilter`, `input *UpdateOrderInput`
- Returns: `[]*Order`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `orders` SET <set> WHERE <filter>
```

### Increment

- Params: `id int64`, `input IncrementInput[OrderIncrementColumn]`
- Returns: `error`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated OrderIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

mysql:

```sql
UPDATE `orders` SET <column> = <column> + ? WHERE `id` = ?
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `orders` WHERE `id` = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `orders` WHERE `id` IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *OrderFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `orders` WHERE <filter>
```

### CreateWithRelated

- Params: `input *CreateOrderWithRelatedInput`
- Returns: `*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (OrderItems), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id int64`, `input *UpdateOrderWithRelatedInput`
- Returns: `*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
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
| ID | `*comparator.Number[int64]` |
| Notes | `*comparator.NullableString` |
| OrderedAt | `*comparator.Time` |
| Status | `*comparator.Enum[OrdersStatusEnum]` |
| Total | `*comparator.Number[float64]` |
| UserID | `*comparator.Number[int64]` |
| And | `[]*OrderFilter` |
| Or | `[]*OrderFilter` |

## Sort

Type `OrderSort`.

Fields: ID, Notes, OrderedAt, Status, Total, UserID
