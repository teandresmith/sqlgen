# ProductSummary

- **Table:** `product_summary` (schema `public`)
- **Kind:** view
- **Source:** `-- @pk: id
-- @type order_count: int32
-- @nullable: description

CREATE VIEW product_summary AS
SELECT
    p.id,
    p.name,
    p.price,
    p.description,
    c.name AS category_name,
    COUNT(oi.id) AS order_count
FROM products p
JOIN categories c ON c.id = p.category_id
LEFT JOIN order_items oi ON oi.product_id = p.id
GROUP BY p.id, p.name, p.price, p.description, c.name;
`

## Files

- `views_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  |  | `comparator.ID` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `price` | Price | `float64` | `numeric(10, 2)` |  |  |  |  | `comparator.Number[float64]` |  |
| `description` | Description | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `category_name` | CategoryName | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `order_count` | OrderCount | `int32` | `int32` |  |  |  |  | `comparator.Number[int32]` |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*ProductSummary`, `error`
- Errors: `ErrNotFound`
- Notes: Generated from the view's @pk annotation.

Generated SQL:

postgres:

```sql
SELECT "category_name", "description", "id", "name", "order_count", "price" FROM "public"."product_summary" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetProductSummariesInput`
- Returns: `[]*ProductSummary`, `error`

Generated SQL:

postgres:

```sql
SELECT "category_name", "description", "id", "name", "order_count", "price" FROM "public"."product_summary" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ProductSummaryFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."product_summary" WHERE <filter>
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
| Description | `*comparator.NullableString` |
| ID | `*comparator.ID` |
| Name | `*comparator.String` |
| OrderCount | `*comparator.Number[int32]` |
| Price | `*comparator.Number[float64]` |
| And | `[]*ProductSummaryFilter` |
| Or | `[]*ProductSummaryFilter` |

## Sort

Type `ProductSummarySort`.

Fields: CategoryName, Description, ID, Name, OrderCount, Price
