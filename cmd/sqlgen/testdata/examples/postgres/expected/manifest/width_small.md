# WidthSmall

- **Table:** `width_smalls` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `int32`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int32` | `integer` |  | yes |  |  | `comparator.Number[int32]` |  |
| `scope` | Scope | `int64` | `bigint` |  |  |  |  | `comparator.Number[int64]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `width_smalls_pkey` | id | yes | btree |  |
| `width_smalls_scope_id_key` | scope, id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| width_bigs | m2m | WidthBig | public.width_links (small_id → big_id) |  |
| width_scoped_children | o2m | WidthScopedChild | public.width_scoped_children.(scope, small_id) |  |
| width_wide_children | o2m | WidthWideChild | public.width_wide_children.small_id |  |

## Query methods

### Get

- Params: `id int32`
- Returns: `*WidthSmall`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "scope" FROM "public"."width_smalls" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetWidthSmallsInput`
- Returns: `[]*WidthSmall`, `error`

Generated SQL:

postgres:

```sql
SELECT "id", "scope" FROM "public"."width_smalls" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int32`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."width_smalls" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *WidthSmallFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."width_smalls" WHERE <filter>)
```

### Count

- Params: `filter *WidthSmallFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."width_smalls" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[WidthSmallFilter]`
- Returns: `*PaginateResult[WidthSmall]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[WidthSmallFilter]`
- Returns: `*Connection[WidthSmall]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamWidthSmallsInput`
- Returns: `iter.Seq2[*WidthSmall, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "scope" FROM "public"."width_smalls" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateWidthSmallInput`
- Returns: `*WidthSmall`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_smalls" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateWidthSmallInput`
- Returns: `[]*WidthSmall`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_smalls" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int32`, `input *UpdateWidthSmallInput`
- Returns: `*WidthSmall`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."width_smalls" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateWidthSmallItem`
- Returns: `[]*WidthSmall`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."width_smalls" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *WidthSmallFilter`, `input *UpdateWidthSmallInput`
- Returns: `[]*WidthSmall`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."width_smalls" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateWidthSmallInput`, `target WidthSmallConflictTarget`
- Returns: `*WidthSmall`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated WidthSmallConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_smalls" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateWidthSmallInput`, `target WidthSmallConflictTarget`
- Returns: `[]*WidthSmall`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same WidthSmallConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_smalls" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Increment

- Params: `id int32`, `input IncrementInput[WidthSmallIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated WidthSmallIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."width_smalls" SET <column> = <column> + $1 WHERE "id" = $2
```

### HardDelete

- Params: `id int32`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."width_smalls" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int32`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."width_smalls" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *WidthSmallFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."width_smalls" WHERE <filter> RETURNING "id"
```

## Filter

Type `WidthSmallFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int32]` |
| Scope | `*comparator.Number[int64]` |
| And | `[]*WidthSmallFilter` |
| Or | `[]*WidthSmallFilter` |

## Sort

Type `WidthSmallSort`.

Fields: ID, Scope

## Read examples

```go
row, err := client.WidthSmalls().Get(ctx, 1)
rows, err := client.WidthSmalls().GetMany(ctx, &models.GetWidthSmallsInput{Filter: &models.WidthSmallFilter{Scope: &comparator.Number[int64]{Eq: new(int64(1))}}})
```

## Write examples

```go
created, err := client.WidthSmalls().Create(ctx, &models.CreateWidthSmallInput{ID: 1, Scope: 1})
updated, err := client.WidthSmalls().Update(ctx, 1, &models.UpdateWidthSmallInput{Scope: omittable.Set(int64(1))})
```
