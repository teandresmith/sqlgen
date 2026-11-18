# Task

- **Table:** `tasks` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  | `gen_random_uuid()` | `comparator.ID` |  |
| `workspace_id` | WorkspaceID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `cycle_id` | CycleID | `*uuid.UUID` | `uuid` | yes |  |  |  | `comparator.NullableID` |  |
| `title` | Title | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `tasks_pkey` | id | yes | btree |  |
| `tasks_workspace_id_id_key` | workspace_id, id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| labels | m2m | Label | public.task_labels ((workspace_id, task_id) → (workspace_id, label_id)) |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*Task`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "cycle_id", "id", "title", "workspace_id" FROM "public"."tasks" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetTasksInput`
- Returns: `[]*Task`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "cycle_id", "id", "title", "workspace_id" FROM "public"."tasks" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."tasks" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *TaskFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."tasks" WHERE <filter>)
```

### Count

- Params: `filter *TaskFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."tasks" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[TaskFilter]`
- Returns: `*PaginateResult[Task]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[TaskFilter]`
- Returns: `*Connection[Task]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamTasksInput`
- Returns: `iter.Seq2[*Task, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "cycle_id", "id", "title", "workspace_id" FROM "public"."tasks" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateTaskInput`
- Returns: `*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."tasks" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateTaskInput`
- Returns: `[]*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."tasks" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id uuid.UUID`, `input *UpdateTaskInput`
- Returns: `*Task`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."tasks" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateTaskItem`
- Returns: `[]*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."tasks" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *TaskFilter`, `input *UpdateTaskInput`
- Returns: `[]*Task`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."tasks" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateTaskInput`, `target TaskConflictTarget`
- Returns: `*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated TaskConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."tasks" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateTaskInput`, `target TaskConflictTarget`
- Returns: `[]*Task`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same TaskConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."tasks" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### CreateWithRelated

- Params: `input *CreateTaskWithRelatedInput`
- Returns: `*Task`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (Labels), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id uuid.UUID`, `input *UpdateTaskWithRelatedInput`
- Returns: `*Task`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (Labels), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertTaskWithRelatedInput`, `target TaskConflictTarget`
- Returns: `*Task`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (Labels), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."tasks" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."tasks" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *TaskFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
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
| CycleID | `*comparator.NullableID` |
| ID | `*comparator.ID` |
| Title | `*comparator.String` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*TaskFilter` |
| Or | `[]*TaskFilter` |

## Sort

Type `TaskSort`.

Fields: CycleID, ID, Title, WorkspaceID

## Read examples

```go
row, err := client.Tasks().Get(ctx, uuid.NewV4())
rows, err := client.Tasks().GetMany(ctx, &models.GetTasksInput{Filter: &models.TaskFilter{Title: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.Tasks().Create(ctx, &models.CreateTaskInput{Title: "example"})
updated, err := client.Tasks().Update(ctx, uuid.NewV4(), &models.UpdateTaskInput{WorkspaceID: omittable.Set(uuid.NewV4())})
```
