# LineItem

- **Table:** `line_items` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: composite
- Struct: `LineItemPK`
- `order_id` — OrderID `int64`
- `product_id` — ProductID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `order_id` | OrderID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `product_id` | ProductID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `workspace_id` | WorkspaceID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `quantity` | Quantity | `int64` | `bigint` |  |  |  |  | `comparator.Number[int64]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `line_items_pkey` | order_id, product_id | yes | btree |  |

## Query methods

### Get

- Params: `pk LineItemPK`
- Returns: `*LineItem`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "order_id", "product_id", "quantity", "workspace_id" FROM "public"."line_items" WHERE "order_id" = $1 AND "product_id" = $2 LIMIT 1
```

### GetMany

- Params: `input *GetLineItemsInput`
- Returns: `[]*LineItem`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "order_id", "product_id", "quantity", "workspace_id" FROM "public"."line_items" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *LineItemFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."line_items" WHERE <filter>
```

### Exists

- Params: `pk LineItemPK`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."line_items" WHERE "order_id" = $1 AND "product_id" = $2)
```

### ExistsWhere

- Params: `filter *LineItemFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."line_items" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[LineItemFilter]`
- Returns: `*PaginateResult[LineItem]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[LineItemFilter]`
- Returns: `*Connection[LineItem]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamLineItemsInput`
- Returns: `iter.Seq2[*LineItem, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "order_id", "product_id", "quantity", "workspace_id" FROM "public"."line_items" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateLineItemInput`
- Returns: `*LineItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."line_items" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateLineItemInput`
- Returns: `[]*LineItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."line_items" (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateLineItemInput`, `target LineItemConflictTarget`
- Returns: `*LineItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated LineItemConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."line_items" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateLineItemInput`, `target LineItemConflictTarget`
- Returns: `[]*LineItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same LineItemConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."line_items" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `pk LineItemPK`, `input *UpdateLineItemInput`
- Returns: `*LineItem`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."line_items" SET <set> WHERE "order_id" = $1 AND "product_id" = $2
```

### UpdateMany

- Params: `items []UpdateLineItemItem`
- Returns: `[]*LineItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."line_items" SET <set> WHERE "order_id" = $1 AND "product_id" = $2
```

### UpdateWhere

- Params: `filter *LineItemFilter`, `input *UpdateLineItemInput`
- Returns: `[]*LineItem`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."line_items" SET <set> WHERE <filter> RETURNING "order_id", "product_id"
```

### Increment

- Params: `pk LineItemPK`, `input IncrementInput[LineItemIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated LineItemIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."line_items" SET <column> = <column> + $1 WHERE "order_id" = $2 AND "product_id" = $3
```

### HardDelete

- Params: `pk LineItemPK`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."line_items" WHERE "order_id" = $1 AND "product_id" = $2
```

### HardDeleteMany

- Params: `pks []LineItemPK`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."line_items" WHERE ("order_id", "product_id") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *LineItemFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."line_items" WHERE <filter> RETURNING "order_id", "product_id"
```

## Filter

Type `LineItemFilter`.

| Field | Type |
| --- | --- |
| OrderID | `*comparator.Number[int64]` |
| ProductID | `*comparator.Number[int64]` |
| Quantity | `*comparator.Number[int64]` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*LineItemFilter` |
| Or | `[]*LineItemFilter` |

## Sort

Type `LineItemSort`.

Fields: OrderID, ProductID, Quantity, WorkspaceID
