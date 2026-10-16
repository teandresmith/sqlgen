# Task

- **Table:** `tasks`
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
| `project_id` | ProjectID | `uint64` | `bigint unsigned` |  |  |  |  | `comparator.Number[uint64]` |  |
| `assignee_id` | AssigneeID | `*uint64` | `bigint unsigned` | yes |  |  |  | `comparator.NullableNumber[uint64]` |  |
| `title` | Title | `string` | `varchar(200)` |  |  |  |  | `comparator.String` |  |
| `estimate_minutes` | EstimateMinutes | `uint32` | `int unsigned` |  |  |  | `0` | `comparator.Number[uint32]` |  |
| `estimate_seconds` | EstimateSeconds | `*uint32` | `int unsigned` | yes |  |  |  | `comparator.NullableNumber[uint32]` |  |
| `priority` | Priority | `TasksPriorityEnum` | `tasks__priority_enum` |  |  |  | `'normal'` | `comparator.Enum[TasksPriorityEnum]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `PRIMARY` | id | yes | btree |  |
| `tasks_assignee_id_fkey` | assignee_id |  | btree |  |
| `tasks_project_id_fkey` | project_id |  | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| task_labels | o2m | TaskLabel | task_labels.task_id |  |

## Query methods

### Get

- Params: `id uint64`
- Returns: `*Task`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

mysql:

```sql
SELECT `assignee_id`, `estimate_minutes`, `estimate_seconds`, `id`, `priority`, `project_id`, `title` FROM `tasks` WHERE `id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetTasksInput`
- Returns: `[]*Task`, `error`

Generated SQL:

mysql:

```sql
SELECT `assignee_id`, `estimate_minutes`, `estimate_seconds`, `id`, `priority`, `project_id`, `title` FROM `tasks` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *TaskFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `tasks` WHERE <filter>
```

### Exists

- Params: `id uint64`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `tasks` WHERE `id` = ?)
```

### ExistsWhere

- Params: `filter *TaskFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `tasks` WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[TaskFilter]`
- Returns: `*PaginateResult[Task]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[TaskFilter]`
- Returns: `*Connection[Task]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamTasksInput`
- Returns: `iter.Seq2[*Task, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

mysql:

```sql
SELECT `assignee_id`, `estimate_minutes`, `estimate_seconds`, `id`, `priority`, `project_id`, `title` FROM `tasks` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateTaskInput`
- Returns: `*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `tasks` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateTaskInput`
- Returns: `[]*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `tasks` (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateTaskInput`, `target TaskConflictTarget`
- Returns: `*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated TaskConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `tasks` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateTaskInput`, `target TaskConflictTarget`
- Returns: `[]*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same TaskConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `tasks` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### Update

- Params: `id uint64`, `input *UpdateTaskInput`
- Returns: `*Task`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `tasks` SET <set> WHERE `id` = ?
```

### UpdateMany

- Params: `items []UpdateTaskItem`
- Returns: `[]*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `tasks` SET <set> WHERE `id` = ?
```

### UpdateWhere

- Params: `filter *TaskFilter`, `input *UpdateTaskInput`
- Returns: `[]*Task`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `tasks` SET <set> WHERE <filter>
```

### Increment

- Params: `id uint64`, `input IncrementInput[TaskIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated TaskIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

mysql:

```sql
UPDATE `tasks` SET <column> = <column> + ? WHERE `id` = ?
```

### HardDelete

- Params: `id uint64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `tasks` WHERE `id` = ?
```

### HardDeleteMany

- Params: `ids []uint64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `tasks` WHERE `id` IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *TaskFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `tasks` WHERE <filter>
```

## Filter

Type `TaskFilter`.

| Field | Type |
| --- | --- |
| AssigneeID | `*comparator.NullableNumber[uint64]` |
| EstimateMinutes | `*comparator.Number[uint32]` |
| EstimateSeconds | `*comparator.NullableNumber[uint32]` |
| ID | `*comparator.Number[uint64]` |
| Priority | `*comparator.Enum[TasksPriorityEnum]` |
| ProjectID | `*comparator.Number[uint64]` |
| Title | `*comparator.String` |
| And | `[]*TaskFilter` |
| Or | `[]*TaskFilter` |

## Sort

Type `TaskSort`.

Fields: AssigneeID, EstimateMinutes, EstimateSeconds, ID, Priority, ProjectID, Title
