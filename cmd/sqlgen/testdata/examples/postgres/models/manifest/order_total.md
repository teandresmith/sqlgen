# OrderTotal

- **Table:** `order_totals` (schema `public`)
- **Kind:** view
- **Source:** `-- @pk: product_id
-- @type total_quantity: int64

CREATE MATERIALIZED VIEW order_totals AS
SELECT
    oi.product_id,
    COUNT(oi.id) AS line_count,
    SUM(oi.quantity) AS total_quantity
FROM order_items oi
GROUP BY oi.product_id;
`

## Files

- `views_gen.go`

## Primary key

- Kind: single
- `product_id` — ProductID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `product_id` | ProductID | `uuid.UUID` | `uuid` |  | yes |  |  | `comparator.ID` |  |
| `line_count` | LineCount | `int64` | `int64` |  |  |  |  | `comparator.Number[int64]` |  |
| `total_quantity` | TotalQuantity | `int64` | `int64` |  |  |  |  | `comparator.Number[int64]` |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*OrderTotal`, `error`
- Errors: `ErrNotFound`
- Notes: Generated from the view's @pk annotation.

Generated SQL:

postgres:

```sql
SELECT "line_count", "product_id", "total_quantity" FROM "public"."order_totals" WHERE "product_id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetOrderTotalsInput`
- Returns: `[]*OrderTotal`, `error`

Generated SQL:

postgres:

```sql
SELECT "line_count", "product_id", "total_quantity" FROM "public"."order_totals" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *OrderTotalFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."order_totals" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[OrderTotalFilter]`
- Returns: `*PaginateResult[OrderTotal]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[OrderTotalFilter]`
- Returns: `*Connection[OrderTotal]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

## Mutation methods

### Refresh

- Params: none
- Returns: `error`
- Notes: Recomputes the materialized view (ACCESS EXCLUSIVE lock; may run inside a transaction). Invalidates the view's cache entries on success (no-op when the view is not cached).

Generated SQL:

postgres:

```sql
REFRESH MATERIALIZED VIEW "public"."order_totals"
```

### RefreshConcurrently

- Params: none
- Returns: `error`
- Errors: `database.ErrRefreshConcurrentlyInTx`
- Notes: Non-blocking refresh; requires a UNIQUE index on the view; cannot run inside a transaction. Invalidates the view's cache entries on success (no-op when the view is not cached).

Generated SQL:

postgres:

```sql
REFRESH MATERIALIZED VIEW CONCURRENTLY "public"."order_totals"
```

## Filter

Type `OrderTotalFilter`.

| Field | Type |
| --- | --- |
| LineCount | `*comparator.Number[int64]` |
| ProductID | `*comparator.ID` |
| TotalQuantity | `*comparator.Number[int64]` |
| And | `[]*OrderTotalFilter` |
| Or | `[]*OrderTotalFilter` |

## Sort

Type `OrderTotalSort`.

Fields: LineCount, ProductID, TotalQuantity
