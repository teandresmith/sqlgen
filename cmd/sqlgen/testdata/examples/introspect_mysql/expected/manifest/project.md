# Project

- **Table:** `projects`
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uint64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uint64` | `bigint unsigned` |  | yes |  |  | `comparator.Number[uint64]` |  |
| `account_id` | AccountID | `uint64` | `bigint unsigned` |  |  |  |  | `comparator.Number[uint64]` |  |
| `name` | Name | `string` | `varchar(100)` |  |  |  |  | `comparator.String` |  |
| `slug` | Slug | `string` | `varchar(100)` |  |  | yes |  | `comparator.String` |  |
| `summary` | Summary | `string` | `varchar(255)` |  |  |  |  | `comparator.String` |  |
| `tags` | Tags | `ProjectsTagsSet` | `projects__tags_set` |  |  |  | `''` | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `PRIMARY` | id | yes | btree |  |
| `projects_account_id_fkey` | account_id |  | btree |  |
| `projects_slug_key` | slug | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| tasks | o2m | Task | tasks.project_id |  |

## Query methods

### Get

- Params: `id uint64`
- Returns: `*Project`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

mysql:

```sql
SELECT `account_id`, `id`, `name`, `slug`, `summary`, `tags` FROM `projects` WHERE `id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetProjectsInput`
- Returns: `[]*Project`, `error`

Generated SQL:

mysql:

```sql
SELECT `account_id`, `id`, `name`, `slug`, `summary`, `tags` FROM `projects` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ProjectFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `projects` WHERE <filter>
```

### Exists

- Params: `id uint64`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `projects` WHERE `id` = ?)
```

### ExistsWhere

- Params: `filter *ProjectFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `projects` WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[ProjectFilter]`
- Returns: `*PaginateResult[Project]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ProjectFilter]`
- Returns: `*Connection[Project]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamProjectsInput`
- Returns: `iter.Seq2[*Project, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

mysql:

```sql
SELECT `account_id`, `id`, `name`, `slug`, `summary`, `tags` FROM `projects` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateProjectInput`
- Returns: `*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `projects` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateProjectInput`
- Returns: `[]*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `projects` (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateProjectInput`, `target ProjectConflictTarget`
- Returns: `*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated ProjectConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `projects` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateProjectInput`, `target ProjectConflictTarget`
- Returns: `[]*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same ProjectConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `projects` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### Update

- Params: `id uint64`, `input *UpdateProjectInput`
- Returns: `*Project`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `projects` SET <set> WHERE `id` = ?
```

### UpdateMany

- Params: `items []UpdateProjectItem`
- Returns: `[]*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `projects` SET <set> WHERE `id` = ?
```

### UpdateWhere

- Params: `filter *ProjectFilter`, `input *UpdateProjectInput`
- Returns: `[]*Project`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `projects` SET <set> WHERE <filter>
```

### HardDelete

- Params: `id uint64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `projects` WHERE `id` = ?
```

### HardDeleteMany

- Params: `ids []uint64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `projects` WHERE `id` IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *ProjectFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `projects` WHERE <filter>
```

## Filter

Type `ProjectFilter`.

| Field | Type |
| --- | --- |
| AccountID | `*comparator.Number[uint64]` |
| ID | `*comparator.Number[uint64]` |
| Name | `*comparator.String` |
| Slug | `*comparator.String` |
| Summary | `*comparator.String` |
| Tags | `*comparator.String` |
| And | `[]*ProjectFilter` |
| Or | `[]*ProjectFilter` |

## Sort

Type `ProjectSort`.

Fields: AccountID, ID, Name, Slug, Summary, Tags
