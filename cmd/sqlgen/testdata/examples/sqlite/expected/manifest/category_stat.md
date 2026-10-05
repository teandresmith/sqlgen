# CategoryStat

- **Table:** `category_stats`
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
    GROUP_CONCAT(p.title, ', ') AS product_names
FROM categories c
LEFT JOIN products p ON p.category_id = c.id
GROUP BY c.name;
`

## Files

- `category_stat_gen.go`

## Primary key

- Kind: none

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `category_name` | CategoryName | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `product_count` | ProductCount | `int32` | `int32` |  |  |  |  | `comparator.Number[int32]` |  |
| `total_price` | TotalPrice | `*float64` | `real` | yes |  |  |  | `comparator.NullableNumber[float64]` |  |
| `avg_price` | AvgPrice | `*float64` | `*float64` | yes |  |  |  | `comparator.NullableNumber[float64]` |  |
| `min_price` | MinPrice | `*float64` | `real` | yes |  |  |  | `comparator.NullableNumber[float64]` |  |
| `max_price` | MaxPrice | `*float64` | `real` | yes |  |  |  | `comparator.NullableNumber[float64]` |  |
| `product_names` | ProductNames | `*string` | `*string` | yes |  |  |  | `comparator.NullableString` |  |

## Query methods

### GetMany

- Params: `input *GetCategoryStatsInput`
- Returns: `[]*CategoryStat`, `error`

Generated SQL:

sqlite:

```sql
SELECT "avg_price", "category_name", "max_price", "min_price", "product_count", "product_names", "total_price" FROM "category_stats" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *CategoryStatFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "category_stats" WHERE <filter>
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
| MaxPrice | `*comparator.NullableNumber[float64]` |
| MinPrice | `*comparator.NullableNumber[float64]` |
| ProductCount | `*comparator.Number[int32]` |
| ProductNames | `*comparator.NullableString` |
| TotalPrice | `*comparator.NullableNumber[float64]` |
| And | `[]*CategoryStatFilter` |
| Or | `[]*CategoryStatFilter` |

## Sort

Type `CategoryStatSort`.

Fields: AvgPrice, CategoryName, MaxPrice, MinPrice, ProductCount, ProductNames, TotalPrice
