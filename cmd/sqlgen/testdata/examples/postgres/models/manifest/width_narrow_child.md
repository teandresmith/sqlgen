# WidthNarrowChild

- **Table:** `width_narrow_children` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `big_id` | BigID | `int32` | `integer` |  |  |  |  | `comparator.Number[int32]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `width_narrow_children_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*WidthNarrowChild`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "big_id", "id" FROM "public"."width_narrow_children" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetWidthNarrowChildrenInput`
- Returns: `[]*WidthNarrowChild`, `error`

Generated SQL:

postgres:

```sql
SELECT "big_id", "id" FROM "public"."width_narrow_children" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."width_narrow_children" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *WidthNarrowChildFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."width_narrow_children" WHERE <filter>)
```

### Count

- Params: `filter *WidthNarrowChildFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."width_narrow_children" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[WidthNarrowChildFilter]`
- Returns: `*PaginateResult[WidthNarrowChild]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[WidthNarrowChildFilter]`
- Returns: `*Connection[WidthNarrowChild]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamWidthNarrowChildrenInput`
- Returns: `iter.Seq2[*WidthNarrowChild, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "big_id", "id" FROM "public"."width_narrow_children" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateWidthNarrowChildInput`
- Returns: `*WidthNarrowChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_narrow_children" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateWidthNarrowChildInput`
- Returns: `[]*WidthNarrowChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_narrow_children" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateWidthNarrowChildInput`
- Returns: `*WidthNarrowChild`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."width_narrow_children" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateWidthNarrowChildItem`
- Returns: `[]*WidthNarrowChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."width_narrow_children" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *WidthNarrowChildFilter`, `input *UpdateWidthNarrowChildInput`
- Returns: `[]*WidthNarrowChild`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."width_narrow_children" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateWidthNarrowChildInput`, `target WidthNarrowChildConflictTarget`
- Returns: `*WidthNarrowChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated WidthNarrowChildConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_narrow_children" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateWidthNarrowChildInput`, `target WidthNarrowChildConflictTarget`
- Returns: `[]*WidthNarrowChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same WidthNarrowChildConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_narrow_children" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."width_narrow_children" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."width_narrow_children" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *WidthNarrowChildFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."width_narrow_children" WHERE <filter> RETURNING "id"
```

## Filter

Type `WidthNarrowChildFilter`.

| Field | Type |
| --- | --- |
| BigID | `*comparator.Number[int32]` |
| ID | `*comparator.Number[int64]` |
| And | `[]*WidthNarrowChildFilter` |
| Or | `[]*WidthNarrowChildFilter` |

## Sort

Type `WidthNarrowChildSort`.

Fields: BigID, ID

## Read examples

```go
row, err := client.WidthNarrowChildren().Get(ctx, 1)
rows, err := client.WidthNarrowChildren().GetMany(ctx, &models.GetWidthNarrowChildrenInput{Filter: &models.WidthNarrowChildFilter{BigID: &comparator.Number[int32]{Eq: new(int32(1))}}})
```

## Write examples

```go
created, err := client.WidthNarrowChildren().Create(ctx, &models.CreateWidthNarrowChildInput{BigID: 1})
updated, err := client.WidthNarrowChildren().Update(ctx, 1, &models.UpdateWidthNarrowChildInput{BigID: omittable.Set(int32(1))})
```
