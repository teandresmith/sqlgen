# WidthScopedChild

- **Table:** `width_scoped_children` (schema `public`)
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
| `scope` | Scope | `int32` | `integer` |  |  |  |  | `comparator.Number[int32]` |  |
| `small_id` | SmallID | `int64` | `bigint` |  |  |  |  | `comparator.Number[int64]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `width_scoped_children_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*WidthScopedChild`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "scope", "small_id" FROM "public"."width_scoped_children" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetWidthScopedChildrenInput`
- Returns: `[]*WidthScopedChild`, `error`

Generated SQL:

postgres:

```sql
SELECT "id", "scope", "small_id" FROM "public"."width_scoped_children" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."width_scoped_children" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *WidthScopedChildFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."width_scoped_children" WHERE <filter>)
```

### Count

- Params: `filter *WidthScopedChildFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."width_scoped_children" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[WidthScopedChildFilter]`
- Returns: `*PaginateResult[WidthScopedChild]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[WidthScopedChildFilter]`
- Returns: `*Connection[WidthScopedChild]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamWidthScopedChildrenInput`
- Returns: `iter.Seq2[*WidthScopedChild, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "scope", "small_id" FROM "public"."width_scoped_children" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateWidthScopedChildInput`
- Returns: `*WidthScopedChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_scoped_children" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateWidthScopedChildInput`
- Returns: `[]*WidthScopedChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_scoped_children" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateWidthScopedChildInput`
- Returns: `*WidthScopedChild`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."width_scoped_children" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateWidthScopedChildItem`
- Returns: `[]*WidthScopedChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."width_scoped_children" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *WidthScopedChildFilter`, `input *UpdateWidthScopedChildInput`
- Returns: `[]*WidthScopedChild`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."width_scoped_children" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateWidthScopedChildInput`, `target WidthScopedChildConflictTarget`
- Returns: `*WidthScopedChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated WidthScopedChildConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_scoped_children" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateWidthScopedChildInput`, `target WidthScopedChildConflictTarget`
- Returns: `[]*WidthScopedChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same WidthScopedChildConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_scoped_children" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."width_scoped_children" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."width_scoped_children" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *WidthScopedChildFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."width_scoped_children" WHERE <filter> RETURNING "id"
```

## Filter

Type `WidthScopedChildFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Scope | `*comparator.Number[int32]` |
| SmallID | `*comparator.Number[int64]` |
| And | `[]*WidthScopedChildFilter` |
| Or | `[]*WidthScopedChildFilter` |

## Sort

Type `WidthScopedChildSort`.

Fields: ID, Scope, SmallID

## Read examples

```go
row, err := client.WidthScopedChildren().Get(ctx, 1)
rows, err := client.WidthScopedChildren().GetMany(ctx, &models.GetWidthScopedChildrenInput{Filter: &models.WidthScopedChildFilter{Scope: &comparator.Number[int32]{Eq: new(int32(1))}}})
```

## Write examples

```go
created, err := client.WidthScopedChildren().Create(ctx, &models.CreateWidthScopedChildInput{Scope: 1, SmallID: 1})
updated, err := client.WidthScopedChildren().Update(ctx, 1, &models.UpdateWidthScopedChildInput{Scope: omittable.Set(int32(1))})
```
