# Reviewer

- **Table:** `reviewers` (schema `public`)
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
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `reviewers_pkey` | id | yes | btree |  |
| `reviewers_workspace_id_id_key` | workspace_id, id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| tasks | m2m | Task | public.task_reviewers ((workspace_id, reviewer_id) → (workspace_id, task_id)) |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Reviewer`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "name", "workspace_id" FROM "public"."reviewers" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetReviewersInput`
- Returns: `[]*Reviewer`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "id", "name", "workspace_id" FROM "public"."reviewers" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."reviewers" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *ReviewerFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."reviewers" WHERE <filter>)
```

### Count

- Params: `filter *ReviewerFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."reviewers" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[ReviewerFilter]`
- Returns: `*PaginateResult[Reviewer]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ReviewerFilter]`
- Returns: `*Connection[Reviewer]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamReviewersInput`
- Returns: `iter.Seq2[*Reviewer, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "name", "workspace_id" FROM "public"."reviewers" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateReviewerInput`
- Returns: `*Reviewer`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."reviewers" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateReviewerInput`
- Returns: `[]*Reviewer`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."reviewers" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateReviewerInput`
- Returns: `*Reviewer`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."reviewers" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateReviewerItem`
- Returns: `[]*Reviewer`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."reviewers" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *ReviewerFilter`, `input *UpdateReviewerInput`
- Returns: `[]*Reviewer`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."reviewers" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateReviewerInput`, `target ReviewerConflictTarget`
- Returns: `*Reviewer`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated ReviewerConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."reviewers" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateReviewerInput`, `target ReviewerConflictTarget`
- Returns: `[]*Reviewer`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same ReviewerConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."reviewers" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### CreateWithRelated

- Params: `input *CreateReviewerWithRelatedInput`
- Returns: `*Reviewer`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (Tasks), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id int64`, `input *UpdateReviewerWithRelatedInput`
- Returns: `*Reviewer`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (Tasks), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertReviewerWithRelatedInput`, `target ReviewerConflictTarget`
- Returns: `*Reviewer`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (Tasks), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### HardDelete

- Params: `id int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."reviewers" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."reviewers" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *ReviewerFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."reviewers" WHERE <filter> RETURNING "id"
```

## Filter

Type `ReviewerFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Name | `*comparator.String` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*ReviewerFilter` |
| Or | `[]*ReviewerFilter` |

## Sort

Type `ReviewerSort`.

Fields: ID, Name, WorkspaceID

## Read examples

```go
row, err := client.Reviewers().Get(ctx, 1)
rows, err := client.Reviewers().GetMany(ctx, &models.GetReviewersInput{Filter: &models.ReviewerFilter{Name: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.Reviewers().Create(ctx, &models.CreateReviewerInput{Name: "example"})
updated, err := client.Reviewers().Update(ctx, 1, &models.UpdateReviewerInput{WorkspaceID: omittable.Set(uuid.NewV4())})
```
