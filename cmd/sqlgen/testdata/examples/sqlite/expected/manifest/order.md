# Order

- **Table:** `orders`
- **Kind:** table

## Files

- `order_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `user_id` | UserID | `int64` | `integer` |  |  |  |  | `comparator.Number[int64]` |  |
| `status` | Status | `string` | `text` |  |  |  | `'pending'` | `comparator.String` |  |
| `total` | Total | `float64` | `real` |  |  |  | `0.0` | `comparator.Number[float64]` |  |
| `notes` | Notes | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `ordered_at` | OrderedAt | `types.DateTime` | `datetime` |  |  |  | `datetime('now')` | `comparator.Time` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `orders_pkey` | id | yes | btree |  |

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

sqlite:

```sql
SELECT "id", "notes", "ordered_at", "status", "total", "user_id" FROM "orders" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetOrdersInput`
- Returns: `[]*Order`, `error`

Generated SQL:

sqlite:

```sql
SELECT "id", "notes", "ordered_at", "status", "total", "user_id" FROM "orders" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *OrderFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "orders" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "orders" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *OrderFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "orders" WHERE <filter>)
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

sqlite:

```sql
SELECT "id", "notes", "ordered_at", "status", "total", "user_id" FROM "orders" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateOrderInput`
- Returns: `*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "orders" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateOrderInput`
- Returns: `[]*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "orders" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateOrderInput`, `target OrderConflictTarget`
- Returns: `*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated OrderConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "orders" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateOrderInput`, `target OrderConflictTarget`
- Returns: `[]*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same OrderConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "orders" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateOrderInput`
- Returns: `*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "orders" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateOrderItem`
- Returns: `[]*Order`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "orders" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *OrderFilter`, `input *UpdateOrderInput`
- Returns: `[]*Order`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "orders" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id int64`, `input IncrementInput[OrderIncrementColumn]`
- Returns: `error`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated OrderIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

sqlite:

```sql
UPDATE "orders" SET <column> = <column> + ? WHERE "id" = ?
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "orders" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "orders" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *OrderFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "orders" WHERE <filter> RETURNING "id"
```

## Filter

Type `OrderFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Notes | `*comparator.NullableString` |
| OrderedAt | `*comparator.Time` |
| Status | `*comparator.String` |
| Total | `*comparator.Number[float64]` |
| UserID | `*comparator.Number[int64]` |
| And | `[]*OrderFilter` |
| Or | `[]*OrderFilter` |

## Sort

Type `OrderSort`.

Fields: ID, Notes, OrderedAt, Status, Total, UserID
