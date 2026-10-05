# Product

- **Table:** `products`
- **Kind:** table

## Files

- `product_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `category_id` | CategoryID | `int64` | `integer` |  |  |  |  | `comparator.Number[int64]` |  |
| `title` | Title | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `price` | Price | `float64` | `real` |  |  |  |  | `comparator.Number[float64]` |  |
| `weight_kg` | WeightKg | `*float64` | `real` | yes |  |  |  | `comparator.NullableNumber[float64]` |  |
| `in_stock` | InStock | `bool` | `boolean` |  |  |  | `1` | `comparator.Bool` |  |
| `sku` | SKU | `string` | `text` |  |  | yes |  | `comparator.String` |  |
| `created_at` | CreatedAt | `types.DateTime` | `datetime` |  |  |  | `datetime('now')` | `comparator.Time` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
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

sqlite:

```sql
SELECT "category_id", "created_at", "id", "in_stock", "price", "sku", "title", "weight_kg" FROM "products" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetProductsInput`
- Returns: `[]*Product`, `error`

Generated SQL:

sqlite:

```sql
SELECT "category_id", "created_at", "id", "in_stock", "price", "sku", "title", "weight_kg" FROM "products" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ProductFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "products" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "products" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *ProductFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "products" WHERE <filter>)
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

sqlite:

```sql
SELECT "category_id", "created_at", "id", "in_stock", "price", "sku", "title", "weight_kg" FROM "products" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateProductInput`
- Returns: `*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "products" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateProductInput`
- Returns: `[]*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "products" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateProductInput`, `target ProductConflictTarget`
- Returns: `*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated ProductConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "products" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateProductInput`, `target ProductConflictTarget`
- Returns: `[]*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same ProductConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "products" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateProductInput`
- Returns: `*Product`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "products" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateProductItem`
- Returns: `[]*Product`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "products" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *ProductFilter`, `input *UpdateProductInput`
- Returns: `[]*Product`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "products" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id int64`, `input IncrementInput[ProductIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated ProductIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

sqlite:

```sql
UPDATE "products" SET <column> = <column> + ? WHERE "id" = ?
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "products" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "products" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *ProductFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "products" WHERE <filter> RETURNING "id"
```

## Filter

Type `ProductFilter`.

| Field | Type |
| --- | --- |
| CategoryID | `*comparator.Number[int64]` |
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

Fields: CategoryID, CreatedAt, ID, InStock, Price, SKU, Title, WeightKg
