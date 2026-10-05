# Article

- **Table:** `articles`
- **Kind:** table

## Files

- `article_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `title` | Title | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `body` | Body | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `author` | Author | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `created_at` | CreatedAt | `types.DateTime` | `datetime` |  |  |  | `datetime('now')` | `comparator.Time` |  |
| `deleted_at` | DeletedAt | `types.NullDateTime` | `datetime` | yes |  |  |  | `comparator.NullableTime` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `articles_pkey` | id | yes | btree |  |
| `idx_articles_active` | author |  | btree | `"deleted_at" IS NULL` |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Article`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "author", "body", "created_at", "deleted_at", "id", "title" FROM "articles" WHERE "id" = ? AND "deleted_at" IS NULL LIMIT 1
```

### GetMany

- Params: `input *GetArticlesInput`
- Returns: `[]*Article`, `error`

Generated SQL:

sqlite:

```sql
SELECT "author", "body", "created_at", "deleted_at", "id", "title" FROM "articles" WHERE <filter> AND "deleted_at" IS NULL ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ArticleFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "articles" WHERE <filter> AND "deleted_at" IS NULL
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "articles" WHERE "id" = ? AND "deleted_at" IS NULL)
```

### ExistsWhere

- Params: `filter *ArticleFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "articles" WHERE <filter> AND "deleted_at" IS NULL)
```

### Paginate

- Params: `input PaginateInput[ArticleFilter]`
- Returns: `*PaginateResult[Article]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ArticleFilter]`
- Returns: `*Connection[Article]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamArticlesInput`
- Returns: `iter.Seq2[*Article, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "author", "body", "created_at", "deleted_at", "id", "title" FROM "articles" WHERE <filter> AND "deleted_at" IS NULL ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateArticleInput`
- Returns: `*Article`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "articles" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateArticleInput`
- Returns: `[]*Article`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "articles" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateArticleInput`, `target ArticleConflictTarget`
- Returns: `*Article`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated ArticleConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "articles" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateArticleInput`, `target ArticleConflictTarget`
- Returns: `[]*Article`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same ArticleConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "articles" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateArticleInput`
- Returns: `*Article`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "articles" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateArticleItem`
- Returns: `[]*Article`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "articles" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *ArticleFilter`, `input *UpdateArticleInput`
- Returns: `[]*Article`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "articles" SET <set> WHERE <filter> RETURNING "id"
```

### SoftDelete

- Params: `id int64`
- Returns: `*Article`, `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
UPDATE "articles" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "id" = ?
```

### SoftDeleteMany

- Params: `ids []int64`
- Returns: `[]*Article`, `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
UPDATE "articles" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "id" IN (<ids>)
```

### SoftDeleteWhere

- Params: `filter *ArticleFilter`
- Returns: `[]*Article`, `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "articles" SET "deleted_at" = CURRENT_TIMESTAMP WHERE <filter> AND "deleted_at" IS NULL RETURNING "id"
```

### Restore

- Params: `id int64`
- Returns: `*Article`, `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
UPDATE "articles" SET "deleted_at" = ? WHERE "id" = ?
```

### RestoreMany

- Params: `ids []int64`
- Returns: `[]*Article`, `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
UPDATE "articles" SET "deleted_at" = ? WHERE "id" IN (<ids>)
```

### RestoreWhere

- Params: `filter *ArticleFilter`
- Returns: `[]*Article`, `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "articles" SET "deleted_at" = ? WHERE <filter> AND "deleted_at" IS NOT NULL RETURNING "id"
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "articles" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "articles" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *ArticleFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "articles" WHERE <filter> RETURNING "id"
```

## Filter

Type `ArticleFilter`.

| Field | Type |
| --- | --- |
| Author | `*comparator.String` |
| Body | `*comparator.NullableString` |
| CreatedAt | `*comparator.Time` |
| DeletedAt | `*comparator.NullableTime` |
| ID | `*comparator.Number[int64]` |
| Title | `*comparator.String` |
| And | `[]*ArticleFilter` |
| Or | `[]*ArticleFilter` |

## Sort

Type `ArticleSort`.

Fields: Author, Body, CreatedAt, DeletedAt, ID, Title
