# OrderItem

- **Table:** `order_items`
- **Kind:** table

## Files

- `order_item_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `order_id` | OrderID | `int64` | `integer` |  |  |  |  | `comparator.Number[int64]` |  |
| `product_id` | ProductID | `int64` | `integer` |  |  |  |  | `comparator.Number[int64]` |  |
| `quantity` | Quantity | `int64` | `integer` |  |  |  |  | `comparator.Number[int64]` |  |
| `unit_price` | UnitPrice | `float64` | `real` |  |  |  |  | `comparator.Number[float64]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `idx_order_items_order_product` | order_id, product_id |  | btree |  |
| `order_items_pkey` | id | yes | btree |  |

## Check constraints

- `quantity`: `"unit_price" >= 0 AND "quantity" > 0`
- `unit_price`: `"unit_price" >= 0 AND "quantity" > 0`

## Query methods

### Get

- Params: `id int64`
- Returns: `*OrderItem`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "id", "order_id", "product_id", "quantity", "unit_price" FROM "order_items" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetOrderItemsInput`
- Returns: `[]*OrderItem`, `error`

Generated SQL:

sqlite:

```sql
SELECT "id", "order_id", "product_id", "quantity", "unit_price" FROM "order_items" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *OrderItemFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "order_items" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "order_items" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *OrderItemFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "order_items" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[OrderItemFilter]`
- Returns: `*PaginateResult[OrderItem]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[OrderItemFilter]`
- Returns: `*Connection[OrderItem]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamOrderItemsInput`
- Returns: `iter.Seq2[*OrderItem, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "id", "order_id", "product_id", "quantity", "unit_price" FROM "order_items" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateOrderItemInput`
- Returns: `*OrderItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "order_items" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateOrderItemInput`
- Returns: `[]*OrderItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "order_items" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateOrderItemInput`, `target OrderItemConflictTarget`
- Returns: `*OrderItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated OrderItemConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "order_items" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateOrderItemInput`, `target OrderItemConflictTarget`
- Returns: `[]*OrderItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same OrderItemConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "order_items" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateOrderItemInput`
- Returns: `*OrderItem`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "order_items" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateOrderItemItem`
- Returns: `[]*OrderItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "order_items" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *OrderItemFilter`, `input *UpdateOrderItemInput`
- Returns: `[]*OrderItem`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "order_items" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id int64`, `input IncrementInput[OrderItemIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated OrderItemIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

sqlite:

```sql
UPDATE "order_items" SET <column> = <column> + ? WHERE "id" = ?
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "order_items" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "order_items" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *OrderItemFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "order_items" WHERE <filter> RETURNING "id"
```

## Filter

Type `OrderItemFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| OrderID | `*comparator.Number[int64]` |
| ProductID | `*comparator.Number[int64]` |
| Quantity | `*comparator.Number[int64]` |
| UnitPrice | `*comparator.Number[float64]` |
| And | `[]*OrderItemFilter` |
| Or | `[]*OrderItemFilter` |

## Sort

Type `OrderItemSort`.

Fields: ID, OrderID, ProductID, Quantity, UnitPrice
