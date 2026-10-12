# Task

- **Table:** `tasks` (schema `public`)
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
| `project_id` | ProjectID | `int64` | `bigint` |  |  |  |  | `comparator.Number[int64]` |  |
| `assignee_id` | AssigneeID | `*int64` | `bigint` | yes |  |  |  | `comparator.NullableNumber[int64]` |  |
| `title` | Title | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `done` | Done | `bool` | `boolean` |  |  |  | `false` | `comparator.Bool` |  |
| `priority` | Priority | `TaskPriority` | `task_priority` |  |  |  | `'normal'` | `comparator.Enum[TaskPriority]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `tasks_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| task_labels | o2m | TaskLabel | public.task_labels.task_id |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Task`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "assignee_id", "done", "id", "priority", "project_id", "title" FROM "public"."tasks" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetTasksInput`
- Returns: `[]*Task`, `error`

Generated SQL:

postgres:

```sql
SELECT "assignee_id", "done", "id", "priority", "project_id", "title" FROM "public"."tasks" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *TaskFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."tasks" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."tasks" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *TaskFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."tasks" WHERE <filter>)
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

postgres:

```sql
SELECT "assignee_id", "done", "id", "priority", "project_id", "title" FROM "public"."tasks" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateTaskInput`
- Returns: `*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."tasks" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateTaskInput`
- Returns: `[]*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."tasks" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateTaskInput`, `target TaskConflictTarget`
- Returns: `*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated TaskConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."tasks" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateTaskInput`, `target TaskConflictTarget`
- Returns: `[]*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same TaskConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."tasks" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateTaskInput`
- Returns: `*Task`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."tasks" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateTaskItem`
- Returns: `[]*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."tasks" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *TaskFilter`, `input *UpdateTaskInput`
- Returns: `[]*Task`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."tasks" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."tasks" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."tasks" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *TaskFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."tasks" WHERE <filter> RETURNING "id"
```

## Filter

Type `TaskFilter`.

| Field | Type |
| --- | --- |
| AssigneeID | `*comparator.NullableNumber[int64]` |
| Done | `*comparator.Bool` |
| ID | `*comparator.Number[int64]` |
| Priority | `*comparator.Enum[TaskPriority]` |
| ProjectID | `*comparator.Number[int64]` |
| Title | `*comparator.String` |
| And | `[]*TaskFilter` |
| Or | `[]*TaskFilter` |

## Sort

Type `TaskSort`.

Fields: AssigneeID, Done, ID, Priority, ProjectID, Title
