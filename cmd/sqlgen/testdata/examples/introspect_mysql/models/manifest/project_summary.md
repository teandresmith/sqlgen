# ProjectSummary

- **Table:** `project_summaries`
- **Kind:** view
- **Source:** `select `p`.`id` AS `id`,`p`.`name` AS `name`,`p`.`summary` AS `summary` from `introspect_mysql`.`projects` `p``

## Files

- `views_gen.go`

## Primary key

- Kind: single
- `id` — ID `uint64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uint64` | `bigint unsigned` |  | yes |  |  | `comparator.Number[uint64]` |  |
| `name` | Name | `string` | `varchar(100)` |  |  |  |  | `comparator.String` |  |
| `summary` | Summary | `string` | `varchar(255)` |  |  |  |  | `comparator.String` |  |

## Query methods

### Get

- Params: `id uint64`
- Returns: `*ProjectSummary`, `error`
- Errors: `ErrNotFound`
- Notes: Generated from the view's @pk annotation.

Generated SQL:

mysql:

```sql
SELECT `id`, `name`, `summary` FROM `project_summaries` WHERE `id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetProjectSummariesInput`
- Returns: `[]*ProjectSummary`, `error`

Generated SQL:

mysql:

```sql
SELECT `id`, `name`, `summary` FROM `project_summaries` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ProjectSummaryFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `project_summaries` WHERE <filter>
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
| ID | `*comparator.Number[uint64]` |
| Name | `*comparator.String` |
| Summary | `*comparator.String` |
| And | `[]*ProjectSummaryFilter` |
| Or | `[]*ProjectSummaryFilter` |

## Sort

Type `ProjectSummarySort`.

Fields: ID, Name, Summary
