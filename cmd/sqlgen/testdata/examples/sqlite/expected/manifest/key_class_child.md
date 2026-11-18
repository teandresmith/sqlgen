# KeyClassChild

- **Table:** `key_class_children`
- **Kind:** table

## Files

- `key_class_child_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `parent_id` | ParentID | `*string` | `text` | yes |  |  |  | `comparator.NullableID` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `key_class_children_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*KeyClassChild`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "id", "parent_id" FROM "key_class_children" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetKeyClassChildrenInput`
- Returns: `[]*KeyClassChild`, `error`

Generated SQL:

sqlite:

```sql
SELECT "id", "parent_id" FROM "key_class_children" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "key_class_children" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *KeyClassChildFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "key_class_children" WHERE <filter>)
```

### Count

- Params: `filter *KeyClassChildFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "key_class_children" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[KeyClassChildFilter]`
- Returns: `*PaginateResult[KeyClassChild]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[KeyClassChildFilter]`
- Returns: `*Connection[KeyClassChild]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamKeyClassChildrenInput`
- Returns: `iter.Seq2[*KeyClassChild, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "id", "parent_id" FROM "key_class_children" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateKeyClassChildInput`
- Returns: `*KeyClassChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "key_class_children" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateKeyClassChildInput`
- Returns: `[]*KeyClassChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "key_class_children" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateKeyClassChildInput`
- Returns: `*KeyClassChild`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "key_class_children" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateKeyClassChildItem`
- Returns: `[]*KeyClassChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "key_class_children" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *KeyClassChildFilter`, `input *UpdateKeyClassChildInput`
- Returns: `[]*KeyClassChild`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "key_class_children" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateKeyClassChildInput`, `target KeyClassChildConflictTarget`
- Returns: `*KeyClassChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated KeyClassChildConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "key_class_children" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateKeyClassChildInput`, `target KeyClassChildConflictTarget`
- Returns: `[]*KeyClassChild`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same KeyClassChildConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "key_class_children" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "key_class_children" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "key_class_children" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *KeyClassChildFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "key_class_children" WHERE <filter> RETURNING "id"
```

## Filter

Type `KeyClassChildFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| ParentID | `*comparator.NullableID` |
| And | `[]*KeyClassChildFilter` |
| Or | `[]*KeyClassChildFilter` |

## Sort

Type `KeyClassChildSort`.

Fields: ID, ParentID

## Read examples

```go
row, err := client.KeyClassChildren().Get(ctx, 1)
rows, err := client.KeyClassChildren().GetMany(ctx, &models.GetKeyClassChildrenInput{Filter: &models.KeyClassChildFilter{ParentID: &comparator.NullableID{Null: new(true)}}})
```

## Write examples

```go
created, err := client.KeyClassChildren().Create(ctx, &models.CreateKeyClassChildInput{})
updated, err := client.KeyClassChildren().Update(ctx, 1, &models.UpdateKeyClassChildInput{ParentID: omittable.Set(new("example"))})
```
