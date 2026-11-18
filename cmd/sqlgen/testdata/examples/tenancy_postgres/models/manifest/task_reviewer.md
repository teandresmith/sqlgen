# TaskReviewer

- **Table:** `task_reviewers` (schema `public`)
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
| `workspace_id` | WorkspaceID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `task_id` | TaskID | `int64` | `bigint` |  |  |  |  | `comparator.Number[int64]` |  |
| `reviewer_id` | ReviewerID | `int64` | `bigint` |  |  |  |  | `comparator.Number[int64]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `task_reviewers_pkey` | id | yes | btree |  |
| `task_reviewers_workspace_id_task_id_reviewer_id_key` | workspace_id, task_id, reviewer_id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*TaskReviewer`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "reviewer_id", "task_id", "workspace_id" FROM "public"."task_reviewers" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetTaskReviewersInput`
- Returns: `[]*TaskReviewer`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "id", "reviewer_id", "task_id", "workspace_id" FROM "public"."task_reviewers" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."task_reviewers" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *TaskReviewerFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."task_reviewers" WHERE <filter>)
```

### Count

- Params: `filter *TaskReviewerFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."task_reviewers" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[TaskReviewerFilter]`
- Returns: `*PaginateResult[TaskReviewer]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[TaskReviewerFilter]`
- Returns: `*Connection[TaskReviewer]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamTaskReviewersInput`
- Returns: `iter.Seq2[*TaskReviewer, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "reviewer_id", "task_id", "workspace_id" FROM "public"."task_reviewers" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateTaskReviewerInput`
- Returns: `*TaskReviewer`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_reviewers" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateTaskReviewerInput`
- Returns: `[]*TaskReviewer`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_reviewers" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateTaskReviewerInput`
- Returns: `*TaskReviewer`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."task_reviewers" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateTaskReviewerItem`
- Returns: `[]*TaskReviewer`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."task_reviewers" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *TaskReviewerFilter`, `input *UpdateTaskReviewerInput`
- Returns: `[]*TaskReviewer`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."task_reviewers" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateTaskReviewerInput`, `target TaskReviewerConflictTarget`
- Returns: `*TaskReviewer`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated TaskReviewerConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_reviewers" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateTaskReviewerInput`, `target TaskReviewerConflictTarget`
- Returns: `[]*TaskReviewer`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same TaskReviewerConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_reviewers" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."task_reviewers" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."task_reviewers" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *TaskReviewerFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."task_reviewers" WHERE <filter> RETURNING "id"
```

## Filter

Type `TaskReviewerFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| ReviewerID | `*comparator.Number[int64]` |
| TaskID | `*comparator.Number[int64]` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*TaskReviewerFilter` |
| Or | `[]*TaskReviewerFilter` |

## Sort

Type `TaskReviewerSort`.

Fields: ID, ReviewerID, TaskID, WorkspaceID

## Read examples

```go
row, err := client.TaskReviewers().Get(ctx, 1)
rows, err := client.TaskReviewers().GetMany(ctx, &models.GetTaskReviewersInput{Filter: &models.TaskReviewerFilter{ReviewerID: &comparator.Number[int64]{Eq: new(int64(1))}}})
```

## Write examples

```go
created, err := client.TaskReviewers().Create(ctx, &models.CreateTaskReviewerInput{TaskID: 1, ReviewerID: 1})
updated, err := client.TaskReviewers().Update(ctx, 1, &models.UpdateTaskReviewerInput{WorkspaceID: omittable.Set(uuid.NewV4())})
```
