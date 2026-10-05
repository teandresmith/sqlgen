# CategoryStat

- **Table:** `category_stats` (schema `public`)
- **Kind:** view
- **Source:** `-- @type product_count: int32
-- @nullable: category_name

CREATE VIEW category_stats AS
SELECT
    c.name AS category_name,
    COUNT(p.id) AS product_count,
    SUM(p.price) AS total_price,
    AVG(p.price) AS avg_price,
    MIN(p.price) AS min_price,
    MAX(p.price) AS max_price,
    STRING_AGG(p.name, ', ' ORDER BY p.name) AS product_names,
    BOOL_OR(p.is_active) AS has_active_product,
    JSON_AGG(p.name) AS product_names_json
FROM categories c
LEFT JOIN products p ON p.category_id = c.id
GROUP BY c.name;
`

## Files

- `views_gen.go`

## Primary key

- Kind: none

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `category_name` | CategoryName | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `product_count` | ProductCount | `int32` | `int32` |  |  |  |  | `comparator.Number[int32]` |  |
| `total_price` | TotalPrice | `*float64` | `numeric(10, 2)` | yes |  |  |  | `comparator.NullableNumber[float64]` |  |
| `avg_price` | AvgPrice | `*float64` | `*float64` | yes |  |  |  | `comparator.NullableNumber[float64]` |  |
| `min_price` | MinPrice | `*float64` | `numeric(10, 2)` | yes |  |  |  | `comparator.NullableNumber[float64]` |  |
| `max_price` | MaxPrice | `*float64` | `numeric(10, 2)` | yes |  |  |  | `comparator.NullableNumber[float64]` |  |
| `product_names` | ProductNames | `*string` | `*string` | yes |  |  |  | `comparator.NullableString` |  |
| `has_active_product` | HasActiveProduct | `*bool` | `*bool` | yes |  |  |  | `comparator.NullableBool` |  |
| `product_names_json` | ProductNamesJSON | `types.JSON` | `types.JSON` |  |  |  |  | `comparator.JSON` |  |

## Query methods

### GetMany

- Params: `input *GetCategoryStatsInput`
- Returns: `[]*CategoryStat`, `error`

Generated SQL:

postgres:

```sql
SELECT "avg_price", "category_name", "has_active_product", "max_price", "min_price", "product_count", "product_names", "product_names_json", "total_price" FROM "public"."category_stats" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *CategoryStatFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."category_stats" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[CategoryStatFilter]`
- Returns: `*PaginateResult[CategoryStat]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

## Filter

Type `CategoryStatFilter`.

| Field | Type |
| --- | --- |
| AvgPrice | `*comparator.NullableNumber[float64]` |
| CategoryName | `*comparator.NullableString` |
| HasActiveProduct | `*comparator.NullableBool` |
| MaxPrice | `*comparator.NullableNumber[float64]` |
| MinPrice | `*comparator.NullableNumber[float64]` |
| ProductCount | `*comparator.Number[int32]` |
| ProductNames | `*comparator.NullableString` |
| ProductNamesJSON | `*comparator.JSON` |
| TotalPrice | `*comparator.NullableNumber[float64]` |
| And | `[]*CategoryStatFilter` |
| Or | `[]*CategoryStatFilter` |

## Sort

Type `CategoryStatSort`.

Fields: AvgPrice, CategoryName, HasActiveProduct, MaxPrice, MinPrice, ProductCount, ProductNames, ProductNamesJSON, TotalPrice
