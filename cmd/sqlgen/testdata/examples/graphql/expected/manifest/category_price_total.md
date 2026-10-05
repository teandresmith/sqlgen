# CategoryPriceTotal

- **Table:** `category_price_totals` (schema `public`)
- **Kind:** view
- **Source:** `-- category_price_totals — the MATERIALIZED view fixture. PRD §26.4 states
-- a matview emits the identical read surface to a regular view and that
-- `Refresh` / `RefreshConcurrently` stay Go-client-only; before this fixture no
-- API-enabled example carried a matview, so neither half of that sentence was
-- tested. The postgres example has `order_totals` but no `api:` block.
--
-- It aggregates `products`, which is a SHARED table — deliberately, so the
-- matview isolates the "matview == view on the read surface" claim from the
-- tenancy claim `workspace_note_summary` carries.
--
-- Views have no primary-key fallback for cursor_keys (§4.13), and the
-- inherited default `id` does not exist here, so the connection query needs
-- the explicit `views.category_price_totals.cursor_keys` entry in sqlgen.yml.
--
-- @pk: category_id
-- @type product_count: int32

CREATE MATERIALIZED VIEW category_price_totals AS
SELECT
    p.category_id,
    COUNT(p.id) AS product_count,
    SUM(p.price) AS total_price,
    MAX(p.price) AS max_price
FROM products p
GROUP BY p.category_id;
`

## Files

- `views_gen.go`

## Primary key

- Kind: single
- `category_id` — CategoryID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `category_id` | CategoryID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `product_count` | ProductCount | `int32` | `int32` |  |  |  |  | `comparator.Number[int32]` |  |
| `total_price` | TotalPrice | `decimal.NullDecimal` | `numeric(12, 2)` | yes |  |  |  | `comparator.NullableString` |  |
| `max_price` | MaxPrice | `decimal.NullDecimal` | `numeric(12, 2)` | yes |  |  |  | `comparator.NullableString` |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*CategoryPriceTotal`, `error`
- Errors: `ErrNotFound`
- Notes: Generated from the view's @pk annotation.

Generated SQL:

postgres:

```sql
SELECT "category_id", "max_price", "product_count", "total_price" FROM "public"."category_price_totals" WHERE "category_id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetCategoryPriceTotalsInput`
- Returns: `[]*CategoryPriceTotal`, `error`

Generated SQL:

postgres:

```sql
SELECT "category_id", "max_price", "product_count", "total_price" FROM "public"."category_price_totals" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *CategoryPriceTotalFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."category_price_totals" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[CategoryPriceTotalFilter]`
- Returns: `*PaginateResult[CategoryPriceTotal]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[CategoryPriceTotalFilter]`
- Returns: `*Connection[CategoryPriceTotal]`, `error`
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
REFRESH MATERIALIZED VIEW "public"."category_price_totals"
```

### RefreshConcurrently

- Params: none
- Returns: `error`
- Errors: `database.ErrRefreshConcurrentlyInTx`
- Notes: Non-blocking refresh; requires a UNIQUE index on the view; cannot run inside a transaction. Invalidates the view's cache entries on success (no-op when the view is not cached).

Generated SQL:

postgres:

```sql
REFRESH MATERIALIZED VIEW CONCURRENTLY "public"."category_price_totals"
```

## Filter

Type `CategoryPriceTotalFilter`.

| Field | Type |
| --- | --- |
| CategoryID | `*comparator.Number[int64]` |
| MaxPrice | `*comparator.NullableString` |
| ProductCount | `*comparator.Number[int32]` |
| TotalPrice | `*comparator.NullableString` |
| And | `[]*CategoryPriceTotalFilter` |
| Or | `[]*CategoryPriceTotalFilter` |

## Sort

Type `CategoryPriceTotalSort`.

Fields: CategoryID, MaxPrice, ProductCount, TotalPrice
