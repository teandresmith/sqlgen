# Checkpoint

- **Table:** `checkpoints` (schema `public`)
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
| `milestone_code` | MilestoneCode | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `milestone_id` | MilestoneID | `*int64` | `bigint` | yes |  |  |  | `comparator.NullableNumber[int64]` |  |
| `title` | Title | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `checkpoints_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| milestone | o2o | Milestone | public.checkpoints.(milestone_code, milestone_id) |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Checkpoint`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "milestone_code", "milestone_id", "title", "workspace_id" FROM "public"."checkpoints" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetCheckpointsInput`
- Returns: `[]*Checkpoint`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "id", "milestone_code", "milestone_id", "title", "workspace_id" FROM "public"."checkpoints" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."checkpoints" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *CheckpointFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."checkpoints" WHERE <filter>)
```

### Count

- Params: `filter *CheckpointFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."checkpoints" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[CheckpointFilter]`
- Returns: `*PaginateResult[Checkpoint]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[CheckpointFilter]`
- Returns: `*Connection[Checkpoint]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamCheckpointsInput`
- Returns: `iter.Seq2[*Checkpoint, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "milestone_code", "milestone_id", "title", "workspace_id" FROM "public"."checkpoints" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateCheckpointInput`
- Returns: `*Checkpoint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."checkpoints" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateCheckpointInput`
- Returns: `[]*Checkpoint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."checkpoints" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateCheckpointInput`
- Returns: `*Checkpoint`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."checkpoints" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateCheckpointItem`
- Returns: `[]*Checkpoint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."checkpoints" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *CheckpointFilter`, `input *UpdateCheckpointInput`
- Returns: `[]*Checkpoint`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."checkpoints" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateCheckpointInput`, `target CheckpointConflictTarget`
- Returns: `*Checkpoint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated CheckpointConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."checkpoints" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateCheckpointInput`, `target CheckpointConflictTarget`
- Returns: `[]*Checkpoint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same CheckpointConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."checkpoints" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."checkpoints" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."checkpoints" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *CheckpointFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."checkpoints" WHERE <filter> RETURNING "id"
```

## Filter

Type `CheckpointFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| MilestoneCode | `*comparator.NullableString` |
| MilestoneID | `*comparator.NullableNumber[int64]` |
| Title | `*comparator.String` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*CheckpointFilter` |
| Or | `[]*CheckpointFilter` |

## Sort

Type `CheckpointSort`.

Fields: ID, MilestoneCode, MilestoneID, Title, WorkspaceID

## Read examples

```go
row, err := client.Checkpoints().Get(ctx, 1)
rows, err := client.Checkpoints().GetMany(ctx, &models.GetCheckpointsInput{Filter: &models.CheckpointFilter{Title: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.Checkpoints().Create(ctx, &models.CreateCheckpointInput{Title: "example"})
updated, err := client.Checkpoints().Update(ctx, 1, &models.UpdateCheckpointInput{WorkspaceID: omittable.Set(uuid.NewV4())})
```
