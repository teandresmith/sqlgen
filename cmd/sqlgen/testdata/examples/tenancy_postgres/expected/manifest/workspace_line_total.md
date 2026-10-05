# WorkspaceLineTotal

- **Table:** `workspace_line_totals` (schema `public`)
- **Kind:** view
- **Source:** `-- workspace_line_totals — a tenanted MATERIALIZED view whose tenant column
-- carries the @pk annotation. Two properties ride on this shape:
--
--   - @pk selects a Get signature; it does not make the column a DDL primary
--     key, so the view stays plain-filtered and the manifest reports
--     mode "auto-filter", never the mutation-only "verify-match" (PRD §30.4.2).
--   - Refresh / RefreshConcurrently recompute the whole relation and are never
--     tenant-scoped (§29.2.5), while reads of the refreshed matview
--     still are.
--
-- @pk: workspace_id
-- @type line_count: int64

CREATE MATERIALIZED VIEW workspace_line_totals AS
SELECT
    li.workspace_id,
    COUNT(li.product_id) AS line_count
FROM line_items li
GROUP BY li.workspace_id;
`

## Files

- `views_gen.go`

## Primary key

- Kind: single
- `workspace_id` — WorkspaceID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `workspace_id` | WorkspaceID | `uuid.UUID` | `uuid` |  | yes |  |  | `comparator.ID` |  |
| `line_count` | LineCount | `int64` | `int64` |  |  |  |  | `comparator.Number[int64]` |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*WorkspaceLineTotal`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Generated from the view's @pk annotation.

Generated SQL:

postgres:

```sql
SELECT "line_count", "workspace_id" FROM "public"."workspace_line_totals" WHERE "workspace_id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetWorkspaceLineTotalsInput`
- Returns: `[]*WorkspaceLineTotal`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "line_count", "workspace_id" FROM "public"."workspace_line_totals" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *WorkspaceLineTotalFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."workspace_line_totals" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[WorkspaceLineTotalFilter]`
- Returns: `*PaginateResult[WorkspaceLineTotal]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[WorkspaceLineTotalFilter]`
- Returns: `*Connection[WorkspaceLineTotal]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

## Mutation methods

### Refresh

- Params: none
- Returns: `error`
- Notes: Recomputes the materialized view (ACCESS EXCLUSIVE lock; may run inside a transaction). Invalidates the view's cache entries on success (no-op when the view is not cached).

Generated SQL:

postgres:

```sql
REFRESH MATERIALIZED VIEW "public"."workspace_line_totals"
```

### RefreshConcurrently

- Params: none
- Returns: `error`
- Errors: `database.ErrRefreshConcurrentlyInTx`
- Notes: Non-blocking refresh; requires a UNIQUE index on the view; cannot run inside a transaction. Invalidates the view's cache entries on success (no-op when the view is not cached).

Generated SQL:

postgres:

```sql
REFRESH MATERIALIZED VIEW CONCURRENTLY "public"."workspace_line_totals"
```

## Filter

Type `WorkspaceLineTotalFilter`.

| Field | Type |
| --- | --- |
| LineCount | `*comparator.Number[int64]` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*WorkspaceLineTotalFilter` |
| Or | `[]*WorkspaceLineTotalFilter` |

## Sort

Type `WorkspaceLineTotalSort`.

Fields: LineCount, WorkspaceID
