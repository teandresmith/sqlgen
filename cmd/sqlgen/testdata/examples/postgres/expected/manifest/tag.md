# Tag

- **Table:** `tags` (schema `public`)
- **Kind:** table

Content tags with boolean soft delete

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  | `gen_random_uuid()` | `comparator.ID` |  |
| `name` | Name | `string` | `text` |  |  | yes |  | `comparator.String` |  |
| `is_deleted` | IsDeleted | `bool` | `boolean` |  |  |  | `false` | `comparator.Bool` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `tags_name_key` | name | yes | btree |  |
| `tags_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*Tag`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "is_deleted", "name" FROM "public"."tags" WHERE "id" = $1 AND "is_deleted" = FALSE LIMIT 1
```

### GetMany

- Params: `input *GetTagsInput`
- Returns: `[]*Tag`, `error`

Generated SQL:

postgres:

```sql
SELECT "id", "is_deleted", "name" FROM "public"."tags" WHERE <filter> AND "is_deleted" = FALSE ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *TagFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."tags" WHERE <filter> AND "is_deleted" = FALSE
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."tags" WHERE "id" = $1 AND "is_deleted" = FALSE)
```

### ExistsWhere

- Params: `filter *TagFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."tags" WHERE <filter> AND "is_deleted" = FALSE)
```

### Paginate

- Params: `input PaginateInput[TagFilter]`
- Returns: `*PaginateResult[Tag]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[TagFilter]`
- Returns: `*Connection[Tag]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamTagsInput`
- Returns: `iter.Seq2[*Tag, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "is_deleted", "name" FROM "public"."tags" WHERE <filter> AND "is_deleted" = FALSE ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateTagInput`
- Returns: `*Tag`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."tags" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateTagInput`
- Returns: `[]*Tag`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."tags" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateTagInput`, `target TagConflictTarget`
- Returns: `*Tag`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated TagConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."tags" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateTagInput`, `target TagConflictTarget`
- Returns: `[]*Tag`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same TagConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."tags" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateTagInput`
- Returns: `*Tag`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."tags" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateTagItem`
- Returns: `[]*Tag`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."tags" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *TagFilter`, `input *UpdateTagInput`
- Returns: `[]*Tag`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."tags" SET <set> WHERE <filter> RETURNING "id"
```

### SoftDelete

- Params: `id uuid.UUID`
- Returns: `*Tag`, `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."tags" SET "is_deleted" = TRUE WHERE "id" = $1
```

### SoftDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `[]*Tag`, `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."tags" SET "is_deleted" = TRUE WHERE "id" IN (<ids>)
```

### SoftDeleteWhere

- Params: `filter *TagFilter`
- Returns: `[]*Tag`, `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."tags" SET "is_deleted" = TRUE WHERE <filter> AND "is_deleted" = FALSE RETURNING "id"
```

### Restore

- Params: `id uuid.UUID`
- Returns: `*Tag`, `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."tags" SET "is_deleted" = $1 WHERE "id" = $2
```

### RestoreMany

- Params: `ids []uuid.UUID`
- Returns: `[]*Tag`, `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."tags" SET "is_deleted" = $1 WHERE "id" IN (<ids>)
```

### RestoreWhere

- Params: `filter *TagFilter`
- Returns: `[]*Tag`, `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."tags" SET "is_deleted" = $1 WHERE <filter> AND "is_deleted" = TRUE RETURNING "id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."tags" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."tags" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *TagFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."tags" WHERE <filter> RETURNING "id"
```

## Filter

Type `TagFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.ID` |
| IsDeleted | `*comparator.Bool` |
| Name | `*comparator.String` |
| And | `[]*TagFilter` |
| Or | `[]*TagFilter` |

## Sort

Type `TagSort`.

Fields: ID, IsDeleted, Name
