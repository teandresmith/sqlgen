# ProjectSummary

- **Table:** `project_summaries`
- **Kind:** view
- **Source:** `CREATE VIEW project_summaries AS
SELECT
    p.id,
    p.name,
    p.slug
FROM projects p`

## Files

- `views_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `slug` | Slug | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*ProjectSummary`, `error`
- Errors: `ErrNotFound`
- Notes: Generated from the view's @pk annotation.

Generated SQL:

sqlite:

```sql
SELECT "id", "name", "slug" FROM "project_summaries" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetProjectSummariesInput`
- Returns: `[]*ProjectSummary`, `error`

Generated SQL:

sqlite:

```sql
SELECT "id", "name", "slug" FROM "project_summaries" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ProjectSummaryFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "project_summaries" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[ProjectSummaryFilter]`
- Returns: `*PaginateResult[ProjectSummary]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ProjectSummaryFilter]`
- Returns: `*Connection[ProjectSummary]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

## Filter

Type `ProjectSummaryFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Name | `*comparator.String` |
| Slug | `*comparator.String` |
| And | `[]*ProjectSummaryFilter` |
| Or | `[]*ProjectSummaryFilter` |

## Sort

Type `ProjectSummarySort`.

Fields: ID, Name, Slug
