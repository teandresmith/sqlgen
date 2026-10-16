# TaskLabel

- **Table:** `task_labels` (schema `public`)
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
| `task_id` | TaskID | `int64` | `bigint` |  |  |  |  | `comparator.Number[int64]` |  |
| `label` | Label | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `task_labels_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*TaskLabel`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "label", "task_id" FROM "public"."task_labels" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetTaskLabelsInput`
- Returns: `[]*TaskLabel`, `error`

Generated SQL:

postgres:

```sql
SELECT "id", "label", "task_id" FROM "public"."task_labels" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *TaskLabelFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."task_labels" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."task_labels" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *TaskLabelFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."task_labels" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[TaskLabelFilter]`
- Returns: `*PaginateResult[TaskLabel]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[TaskLabelFilter]`
- Returns: `*Connection[TaskLabel]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamTaskLabelsInput`
- Returns: `iter.Seq2[*TaskLabel, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "label", "task_id" FROM "public"."task_labels" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateTaskLabelInput`
- Returns: `*TaskLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_labels" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateTaskLabelInput`
- Returns: `[]*TaskLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_labels" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateTaskLabelInput`, `target TaskLabelConflictTarget`
- Returns: `*TaskLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated TaskLabelConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_labels" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateTaskLabelInput`, `target TaskLabelConflictTarget`
- Returns: `[]*TaskLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same TaskLabelConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_labels" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateTaskLabelInput`
- Returns: `*TaskLabel`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."task_labels" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateTaskLabelItem`
- Returns: `[]*TaskLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."task_labels" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *TaskLabelFilter`, `input *UpdateTaskLabelInput`
- Returns: `[]*TaskLabel`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."task_labels" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."task_labels" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."task_labels" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *TaskLabelFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."task_labels" WHERE <filter> RETURNING "id"
```

## Filter

Type `TaskLabelFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Label | `*comparator.String` |
| TaskID | `*comparator.Number[int64]` |
| And | `[]*TaskLabelFilter` |
| Or | `[]*TaskLabelFilter` |

## Sort

Type `TaskLabelSort`.

Fields: ID, Label, TaskID
