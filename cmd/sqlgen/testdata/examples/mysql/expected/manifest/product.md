# Product

- **Table:** `products`
- **Kind:** table

Catalog of products available for sale

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` | Unique identifier |
| `category_id` | CategoryID | `int32` | `int` |  |  |  |  | `comparator.Number[int32]` | Parent category reference |
| `title` | Title | `string` | `varchar(255)` |  |  |  |  | `comparator.String` | Product display name |
| `price` | Price | `float64` | `decimal(10,2)` |  |  |  |  | `comparator.Number[float64]` | Unit price in default currency |
| `weight_kg` | WeightKg | `*float64` | `double` | yes |  |  |  | `comparator.NullableNumber[float64]` | Product weight in kilograms |
| `in_stock` | InStock | `bool` | `boolean` |  |  |  | `true` | `comparator.Bool` | Whether the product is currently available |
| `sku` | SKU | `string` | `varchar(50)` |  |  | yes |  | `comparator.String` | Stock keeping unit code |
| `attributes` | Attributes | `types.JSON` | `json` |  |  |  |  | `comparator.JSON` | Structured product attributes |
| `created_at` | CreatedAt | `time.Time` | `timestamp` |  |  |  | `current_timestamp()` | `comparator.Time` | Row creation timestamp |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `category_id` | category_id |  | btree |  |
| `products_pkey` | id | yes | btree |  |
| `products_sku_key` | sku | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| order_items | o2m | OrderItem | order_items.product_id |  |
| product_tag_labels | o2m | ProductTagLabel | product_tag_labels.product_id |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Product`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

mysql:

```sql
SELECT `attributes`, `category_id`, `created_at`, `id`, `in_stock`, `price`, `sku`, `title`, `weight_kg` FROM `products` WHERE `id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetProductsInput`
- Returns: `[]*Product`, `error`

Generated SQL:

mysql:

```sql
SELECT `attributes`, `category_id`, `created_at`, `id`, `in_stock`, `price`, `sku`, `title`, `weight_kg` FROM `products` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ProductFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `products` WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `products` WHERE `id` = ?)
```

### ExistsWhere

- Params: `filter *ProductFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `products` WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[ProductFilter]`
- Returns: `*PaginateResult[Product]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ProductFilter]`
- Returns: `*Connection[Product]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamProductsInput`
- Returns: `iter.Seq2[*Product, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

mysql:

```sql
SELECT `attributes`, `category_id`, `created_at`, `id`, `in_stock`, `price`, `sku`, `title`, `weight_kg` FROM `products` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateProductInput`
- Returns: `*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `products` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateProductInput`
- Returns: `[]*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `products` (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateProductInput`, `target ProductConflictTarget`
- Returns: `*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated ProductConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `products` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateProductInput`, `target ProductConflictTarget`
- Returns: `[]*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same ProductConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `products` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### Update

- Params: `id int64`, `input *UpdateProductInput`
- Returns: `*Product`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `products` SET <set> WHERE `id` = ?
```

### UpdateMany

- Params: `items []UpdateProductItem`
- Returns: `[]*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `products` SET <set> WHERE `id` = ?
```

### UpdateWhere

- Params: `filter *ProductFilter`, `input *UpdateProductInput`
- Returns: `[]*Product`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `products` SET <set> WHERE <filter>
```

### Increment

- Params: `id int64`, `input IncrementInput[ProductIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated ProductIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

mysql:

```sql
UPDATE `products` SET <column> = <column> + ? WHERE `id` = ?
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `products` WHERE `id` = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `products` WHERE `id` IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *ProductFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `products` WHERE <filter>
```

### CreateWithRelated

- Params: `input *CreateProductWithRelatedInput`
- Returns: `*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (OrderItems), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id int64`, `input *UpdateProductWithRelatedInput`
- Returns: `*Product`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (OrderItems), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertProductWithRelatedInput`, `target ProductConflictTarget`
- Returns: `*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (OrderItems), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

## Filter

Type `ProductFilter`.

| Field | Type |
| --- | --- |
| Attributes | `*comparator.JSON` |
| CategoryID | `*comparator.Number[int32]` |
| CreatedAt | `*comparator.Time` |
| ID | `*comparator.Number[int64]` |
| InStock | `*comparator.Bool` |
| Price | `*comparator.Number[float64]` |
| SKU | `*comparator.String` |
| Title | `*comparator.String` |
| WeightKg | `*comparator.NullableNumber[float64]` |
| And | `[]*ProductFilter` |
| Or | `[]*ProductFilter` |

## Sort

Type `ProductSort`.

Fields: Attributes, CategoryID, CreatedAt, ID, InStock, Price, SKU, Title, WeightKg
