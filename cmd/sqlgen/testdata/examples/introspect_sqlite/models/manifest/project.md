# Project

- **Table:** `projects`
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `account_id` | AccountID | `int64` | `integer` |  |  |  |  | `comparator.Number[int64]` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `slug` | Slug | `string` | `text` |  |  | yes |  | `comparator.String` |  |
| `archived` | Archived | `int64` | `integer` |  |  |  | `0` | `comparator.Number[int64]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `projects_active_name_idx` | name |  | btree | `"archived" = 0` |
| `projects_pkey` | id | yes | btree |  |
| `projects_slug_key` | slug | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| accounts | m2m | Account | project_members (project_id → account_id) |  |
| tasks | o2m | Task | tasks.project_id |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Project`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "account_id", "archived", "id", "name", "slug" FROM "projects" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetProjectsInput`
- Returns: `[]*Project`, `error`

Generated SQL:

sqlite:

```sql
SELECT "account_id", "archived", "id", "name", "slug" FROM "projects" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ProjectFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "projects" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "projects" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *ProjectFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "projects" WHERE <filter>)
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

sqlite:

```sql
SELECT "account_id", "archived", "id", "name", "slug" FROM "projects" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateProjectInput`
- Returns: `*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "projects" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateProjectInput`
- Returns: `[]*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "projects" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateProjectInput`, `target ProjectConflictTarget`
- Returns: `*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated ProjectConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "projects" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateProjectInput`, `target ProjectConflictTarget`
- Returns: `[]*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same ProjectConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "projects" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateProjectInput`
- Returns: `*Project`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "projects" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateProjectItem`
- Returns: `[]*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "projects" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *ProjectFilter`, `input *UpdateProjectInput`
- Returns: `[]*Project`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "projects" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id int64`, `input IncrementInput[ProjectIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated ProjectIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

sqlite:

```sql
UPDATE "projects" SET <column> = <column> + ? WHERE "id" = ?
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "projects" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "projects" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *ProjectFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "projects" WHERE <filter> RETURNING "id"
```

## Filter

Type `ProjectFilter`.

| Field | Type |
| --- | --- |
| AccountID | `*comparator.Number[int64]` |
| Archived | `*comparator.Number[int64]` |
| ID | `*comparator.Number[int64]` |
| Name | `*comparator.String` |
| Slug | `*comparator.String` |
| And | `[]*ProjectFilter` |
| Or | `[]*ProjectFilter` |

## Sort

Type `ProjectSort`.

Fields: AccountID, Archived, ID, Name, Slug
