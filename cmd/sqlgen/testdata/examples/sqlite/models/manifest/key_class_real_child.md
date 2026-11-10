# KeyClassRealChild

- **Table:** `key_class_real_children`
- **Kind:** table

## Files

- `key_class_real_child_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `parent_id` | ParentID | `*float64` | `real` | yes |  |  |  | `comparator.NullableNumber[float64]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `key_class_real_children_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*KeyClassRealChild`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "id", "parent_id" FROM "key_class_real_children" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetKeyClassRealChildrenInput`
- Returns: `[]*KeyClassRealChild`, `error`

Generated SQL:

sqlite:

```sql
SELECT "id", "parent_id" FROM "key_class_real_children" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "key_class_real_children" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *KeyClassRealChildFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "key_class_real_children" WHERE <filter>)
```

### Count

- Params: `filter *KeyClassRealChildFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "key_class_real_children" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[KeyClassRealChildFilter]`
- Returns: `*PaginateResult[KeyClassRealChild]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[KeyClassRealChildFilter]`
- Returns: `*Connection[KeyClassRealChild]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamKeyClassRealChildrenInput`
- Returns: `iter.Seq2[*KeyClassRealChild, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "id", "parent_id" FROM "key_class_real_children" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateKeyClassRealChildInput`
- Returns: `*KeyClassRealChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "key_class_real_children" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateKeyClassRealChildInput`
- Returns: `[]*KeyClassRealChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "key_class_real_children" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateKeyClassRealChildInput`
- Returns: `*KeyClassRealChild`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "key_class_real_children" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateKeyClassRealChildItem`
- Returns: `[]*KeyClassRealChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "key_class_real_children" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *KeyClassRealChildFilter`, `input *UpdateKeyClassRealChildInput`
- Returns: `[]*KeyClassRealChild`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "key_class_real_children" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateKeyClassRealChildInput`, `target KeyClassRealChildConflictTarget`
- Returns: `*KeyClassRealChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated KeyClassRealChildConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "key_class_real_children" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateKeyClassRealChildInput`, `target KeyClassRealChildConflictTarget`
- Returns: `[]*KeyClassRealChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same KeyClassRealChildConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "key_class_real_children" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "key_class_real_children" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "key_class_real_children" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *KeyClassRealChildFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "key_class_real_children" WHERE <filter> RETURNING "id"
```

## Filter

Type `KeyClassRealChildFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| ParentID | `*comparator.NullableNumber[float64]` |
| And | `[]*KeyClassRealChildFilter` |
| Or | `[]*KeyClassRealChildFilter` |

## Sort

Type `KeyClassRealChildSort`.

Fields: ID, ParentID

## Read examples

```go
row, err := client.KeyClassRealChildren().Get(ctx, 1)
rows, err := client.KeyClassRealChildren().GetMany(ctx, &models.GetKeyClassRealChildrenInput{Filter: &models.KeyClassRealChildFilter{ParentID: &comparator.NullableNumber[float64]{Null: new(true)}}})
```

## Write examples

```go
created, err := client.KeyClassRealChildren().Create(ctx, &models.CreateKeyClassRealChildInput{})
updated, err := client.KeyClassRealChildren().Update(ctx, 1, &models.UpdateKeyClassRealChildInput{ParentID: omittable.Set(new(float64(1)))})
```
