# Warehouse

- **Table:** `warehouses`
- **Kind:** table

## Files

- `warehouse_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `external_id` | ExternalID | `ksuid.KSUID` | `varchar(27)` |  |  | yes |  | `comparator.String` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `price` | Price | `decimal.Decimal` | `real` |  |  |  |  | `comparator.String` |  |
| `nullable_price` | NullablePrice | `decimal.NullDecimal` | `real` | yes |  |  |  | `comparator.NullableString` |  |
| `created_at` | CreatedAt | `types.DateTime` | `datetime` |  |  |  | `datetime('now')` | `comparator.Time` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `warehouses_external_id_key` | external_id | yes | btree |  |
| `warehouses_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Warehouse`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "created_at", "external_id", "id", "name", "nullable_price", "price" FROM "warehouses" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetWarehousesInput`
- Returns: `[]*Warehouse`, `error`

Generated SQL:

sqlite:

```sql
SELECT "created_at", "external_id", "id", "name", "nullable_price", "price" FROM "warehouses" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *WarehouseFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "warehouses" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "warehouses" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *WarehouseFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "warehouses" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[WarehouseFilter]`
- Returns: `*PaginateResult[Warehouse]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[WarehouseFilter]`
- Returns: `*Connection[Warehouse]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamWarehousesInput`
- Returns: `iter.Seq2[*Warehouse, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "created_at", "external_id", "id", "name", "nullable_price", "price" FROM "warehouses" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateWarehouseInput`
- Returns: `*Warehouse`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "warehouses" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateWarehouseInput`
- Returns: `[]*Warehouse`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "warehouses" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateWarehouseInput`, `target WarehouseConflictTarget`
- Returns: `*Warehouse`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated WarehouseConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "warehouses" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateWarehouseInput`, `target WarehouseConflictTarget`
- Returns: `[]*Warehouse`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same WarehouseConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "warehouses" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateWarehouseInput`
- Returns: `*Warehouse`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "warehouses" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateWarehouseItem`
- Returns: `[]*Warehouse`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "warehouses" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *WarehouseFilter`, `input *UpdateWarehouseInput`
- Returns: `[]*Warehouse`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "warehouses" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id int64`, `input IncrementInput[WarehouseIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated WarehouseIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

sqlite:

```sql
UPDATE "warehouses" SET <column> = <column> + ? WHERE "id" = ?
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "warehouses" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "warehouses" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *WarehouseFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "warehouses" WHERE <filter> RETURNING "id"
```

## Filter

Type `WarehouseFilter`.

| Field | Type |
| --- | --- |
| CreatedAt | `*comparator.Time` |
| ExternalID | `*comparator.String` |
| ID | `*comparator.Number[int64]` |
| Name | `*comparator.String` |
| NullablePrice | `*comparator.NullableString` |
| Price | `*comparator.String` |
| And | `[]*WarehouseFilter` |
| Or | `[]*WarehouseFilter` |

## Sort

Type `WarehouseSort`.

Fields: CreatedAt, ExternalID, ID, Name, NullablePrice, Price
