# ProductSummary

- **Table:** `product_summary`
- **Kind:** view
- **Source:** `-- @pk: id
-- @type order_count: int32
-- @nullable: title

CREATE VIEW product_summary AS
SELECT
    p.id,
    p.title,
    p.price,
    c.name AS category_name,
    COUNT(oi.id) AS order_count
FROM products p
JOIN categories c ON c.id = p.category_id
LEFT JOIN order_items oi ON oi.product_id = p.id
GROUP BY p.id, p.title, p.price, c.name;
`

## Files

- `views_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `title` | Title | `*string` | `varchar(255)` | yes |  |  |  | `comparator.NullableString` |  |
| `price` | Price | `float64` | `decimal(10,2)` |  |  |  |  | `comparator.Number[float64]` |  |
| `category_name` | CategoryName | `string` | `varchar(255)` |  |  |  |  | `comparator.String` |  |
| `order_count` | OrderCount | `int32` | `int32` |  |  |  |  | `comparator.Number[int32]` |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*ProductSummary`, `error`
- Errors: `ErrNotFound`
- Notes: Generated from the view's @pk annotation.

Generated SQL:

mysql:

```sql
SELECT `category_name`, `id`, `order_count`, `price`, `title` FROM `product_summary` WHERE `id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetProductSummariesInput`
- Returns: `[]*ProductSummary`, `error`

Generated SQL:

mysql:

```sql
SELECT `category_name`, `id`, `order_count`, `price`, `title` FROM `product_summary` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ProductSummaryFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `product_summary` WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[ProductSummaryFilter]`
- Returns: `*PaginateResult[ProductSummary]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ProductSummaryFilter]`
- Returns: `*Connection[ProductSummary]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

## Filter

Type `ProductSummaryFilter`.

| Field | Type |
| --- | --- |
| CategoryName | `*comparator.String` |
| ID | `*comparator.Number[int64]` |
| OrderCount | `*comparator.Number[int32]` |
| Price | `*comparator.Number[float64]` |
| Title | `*comparator.NullableString` |
| And | `[]*ProductSummaryFilter` |
| Or | `[]*ProductSummaryFilter` |

## Sort

Type `ProductSummarySort`.

Fields: CategoryName, ID, OrderCount, Price, Title
