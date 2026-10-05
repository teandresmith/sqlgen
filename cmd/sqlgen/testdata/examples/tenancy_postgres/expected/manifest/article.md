# Article

- **Table:** `articles` (schema `public`)
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
| `workspace_id` | WorkspaceID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `title` | Title | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `deleted_at` | DeletedAt | `*time.Time` | `timestamptz` | yes |  |  |  | `comparator.NullableTime` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `articles_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Article`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "deleted_at", "id", "title", "workspace_id" FROM "public"."articles" WHERE "id" = $1 AND "deleted_at" IS NULL LIMIT 1
```

### GetMany

- Params: `input *GetArticlesInput`
- Returns: `[]*Article`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "deleted_at", "id", "title", "workspace_id" FROM "public"."articles" WHERE <filter> AND "deleted_at" IS NULL ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ArticleFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."articles" WHERE <filter> AND "deleted_at" IS NULL
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."articles" WHERE "id" = $1 AND "deleted_at" IS NULL)
```

### ExistsWhere

- Params: `filter *ArticleFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."articles" WHERE <filter> AND "deleted_at" IS NULL)
```

### Paginate

- Params: `input PaginateInput[ArticleFilter]`
- Returns: `*PaginateResult[Article]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ArticleFilter]`
- Returns: `*Connection[Article]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamArticlesInput`
- Returns: `iter.Seq2[*Article, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "deleted_at", "id", "title", "workspace_id" FROM "public"."articles" WHERE <filter> AND "deleted_at" IS NULL ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateArticleInput`
- Returns: `*Article`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."articles" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateArticleInput`
- Returns: `[]*Article`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."articles" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateArticleInput`, `target ArticleConflictTarget`
- Returns: `*Article`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated ArticleConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."articles" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateArticleInput`, `target ArticleConflictTarget`
- Returns: `[]*Article`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same ArticleConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."articles" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateArticleInput`
- Returns: `*Article`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."articles" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateArticleItem`
- Returns: `[]*Article`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."articles" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *ArticleFilter`, `input *UpdateArticleInput`
- Returns: `[]*Article`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."articles" SET <set> WHERE <filter> RETURNING "id"
```

### SoftDelete

- Params: `id int64`
- Returns: `*Article`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."articles" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "id" = $1
```

### SoftDeleteMany

- Params: `ids []int64`
- Returns: `[]*Article`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."articles" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "id" IN (<ids>)
```

### SoftDeleteWhere

- Params: `filter *ArticleFilter`
- Returns: `[]*Article`, `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."articles" SET "deleted_at" = CURRENT_TIMESTAMP WHERE <filter> AND "deleted_at" IS NULL RETURNING "id"
```

### Restore

- Params: `id int64`
- Returns: `*Article`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."articles" SET "deleted_at" = $1 WHERE "id" = $2
```

### RestoreMany

- Params: `ids []int64`
- Returns: `[]*Article`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."articles" SET "deleted_at" = $1 WHERE "id" IN (<ids>)
```

### RestoreWhere

- Params: `filter *ArticleFilter`
- Returns: `[]*Article`, `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."articles" SET "deleted_at" = $1 WHERE <filter> AND "deleted_at" IS NOT NULL RETURNING "id"
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."articles" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."articles" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *ArticleFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."articles" WHERE <filter> RETURNING "id"
```

## Filter

Type `ArticleFilter`.

| Field | Type |
| --- | --- |
| DeletedAt | `*comparator.NullableTime` |
| ID | `*comparator.Number[int64]` |
| Title | `*comparator.String` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*ArticleFilter` |
| Or | `[]*ArticleFilter` |

## Sort

Type `ArticleSort`.

Fields: DeletedAt, ID, Title, WorkspaceID
