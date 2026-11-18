# TaskReport

- **Table:** `task_reports` (schema `public`)
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
| `task_id` | TaskID | `uuid.UUID` | `uuid` |  |  | yes |  | `comparator.ID` |  |
| `summary` | Summary | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `task_reports_pkey` | id | yes | btree |  |
| `task_reports_task_id_key` | task_id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| tasks | o2o | Task | public.task_reports.(workspace_id, task_id) |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*TaskReport`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "summary", "task_id", "workspace_id" FROM "public"."task_reports" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetTaskReportsInput`
- Returns: `[]*TaskReport`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "id", "summary", "task_id", "workspace_id" FROM "public"."task_reports" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."task_reports" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *TaskReportFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."task_reports" WHERE <filter>)
```

### Count

- Params: `filter *TaskReportFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."task_reports" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[TaskReportFilter]`
- Returns: `*PaginateResult[TaskReport]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[TaskReportFilter]`
- Returns: `*Connection[TaskReport]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamTaskReportsInput`
- Returns: `iter.Seq2[*TaskReport, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "summary", "task_id", "workspace_id" FROM "public"."task_reports" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateTaskReportInput`
- Returns: `*TaskReport`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_reports" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateTaskReportInput`
- Returns: `[]*TaskReport`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_reports" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id uuid.UUID`, `input *UpdateTaskReportInput`
- Returns: `*TaskReport`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."task_reports" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateTaskReportItem`
- Returns: `[]*TaskReport`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."task_reports" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *TaskReportFilter`, `input *UpdateTaskReportInput`
- Returns: `[]*TaskReport`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."task_reports" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateTaskReportInput`, `target TaskReportConflictTarget`
- Returns: `*TaskReport`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated TaskReportConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_reports" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateTaskReportInput`, `target TaskReportConflictTarget`
- Returns: `[]*TaskReport`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same TaskReportConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_reports" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."task_reports" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."task_reports" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *TaskReportFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."task_reports" WHERE <filter> RETURNING "id"
```

## Filter

Type `TaskReportFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.ID` |
| Summary | `*comparator.String` |
| TaskID | `*comparator.ID` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*TaskReportFilter` |
| Or | `[]*TaskReportFilter` |

## Sort

Type `TaskReportSort`.

Fields: ID, Summary, TaskID, WorkspaceID

## Read examples

```go
row, err := client.TaskReports().Get(ctx, uuid.NewV4())
rows, err := client.TaskReports().GetMany(ctx, &models.GetTaskReportsInput{Filter: &models.TaskReportFilter{Summary: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.TaskReports().Create(ctx, &models.CreateTaskReportInput{TaskID: uuid.NewV4(), Summary: "example"})
updated, err := client.TaskReports().Update(ctx, uuid.NewV4(), &models.UpdateTaskReportInput{WorkspaceID: omittable.Set(uuid.NewV4())})
```
