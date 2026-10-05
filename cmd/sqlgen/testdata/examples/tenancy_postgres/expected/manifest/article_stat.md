# ArticleStat

- **Table:** `article_stats` (schema `public`)
- **Kind:** view
- **Source:** `-- article_stats — a tenanted regular view on the pgx leg. Scoped by detection
-- alone (no `views.article_stats.tenancy` block), @pk makes Get available so
-- all five read methods are exercised against a real PostgreSQL server.
--
-- @pk: id

CREATE VIEW article_stats AS
SELECT
    a.id,
    a.workspace_id,
    a.title
FROM articles a
WHERE a.deleted_at IS NULL;
`

## Files

- `views_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `workspace_id` | WorkspaceID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `title` | Title | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*ArticleStat`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Generated from the view's @pk annotation.

Generated SQL:

postgres:

```sql
SELECT "id", "title", "workspace_id" FROM "public"."article_stats" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetArticleStatsInput`
- Returns: `[]*ArticleStat`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "id", "title", "workspace_id" FROM "public"."article_stats" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ArticleStatFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."article_stats" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[ArticleStatFilter]`
- Returns: `*PaginateResult[ArticleStat]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ArticleStatFilter]`
- Returns: `*Connection[ArticleStat]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

## Filter

Type `ArticleStatFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Title | `*comparator.String` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*ArticleStatFilter` |
| Or | `[]*ArticleStatFilter` |

## Sort

Type `ArticleStatSort`.

Fields: ID, Title, WorkspaceID
