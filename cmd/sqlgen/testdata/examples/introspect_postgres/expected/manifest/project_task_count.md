# ProjectTaskCount

- **Table:** `project_task_counts` (schema `public`)
- **Kind:** view
- **Source:** ` SELECT project_id,
    count(id) AS task_count
   FROM tasks t
  GROUP BY project_id;`

## Files

- `views_gen.go`

## Primary key

- Kind: single
- `project_id` — ProjectID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `project_id` | ProjectID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `task_count` | TaskCount | `int64` | `int64` |  |  |  |  | `comparator.Number[int64]` |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*ProjectTaskCount`, `error`
- Errors: `ErrNotFound`
- Notes: Generated from the view's @pk annotation.

Generated SQL:

postgres:

```sql
SELECT "project_id", "task_count" FROM "public"."project_task_counts" WHERE "project_id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetProjectTaskCountsInput`
- Returns: `[]*ProjectTaskCount`, `error`

Generated SQL:

postgres:

```sql
SELECT "project_id", "task_count" FROM "public"."project_task_counts" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ProjectTaskCountFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."project_task_counts" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[ProjectTaskCountFilter]`
- Returns: `*PaginateResult[ProjectTaskCount]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

## Mutation methods

### Refresh

- Params: none
- Returns: `error`
- Notes: Recomputes the materialized view (ACCESS EXCLUSIVE lock; may run inside a transaction). Invalidates the view's cache entries on success (no-op when the view is not cached).

Generated SQL:

postgres:

```sql
REFRESH MATERIALIZED VIEW "public"."project_task_counts"
```

### RefreshConcurrently

- Params: none
- Returns: `error`
- Errors: `database.ErrRefreshConcurrentlyInTx`
- Notes: Non-blocking refresh; requires a UNIQUE index on the view; cannot run inside a transaction. Invalidates the view's cache entries on success (no-op when the view is not cached).

Generated SQL:

postgres:

```sql
REFRESH MATERIALIZED VIEW CONCURRENTLY "public"."project_task_counts"
```

## Filter

Type `ProjectTaskCountFilter`.

| Field | Type |
| --- | --- |
| ProjectID | `*comparator.Number[int64]` |
| TaskCount | `*comparator.Number[int64]` |
| And | `[]*ProjectTaskCountFilter` |
| Or | `[]*ProjectTaskCountFilter` |

## Sort

Type `ProjectTaskCountSort`.

Fields: ProjectID, TaskCount
