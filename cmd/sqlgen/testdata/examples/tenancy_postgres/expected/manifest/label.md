# Label

- **Table:** `labels` (schema `public`)
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
| `labels_pkey` | id | yes | btree |  |
| `labels_workspace_id_id_key` | workspace_id, id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| tasks | m2m | Task | public.task_labels ((workspace_id, label_id) → (workspace_id, task_id)) |  |
| templates | m2m | Template | public.template_labels ((workspace_id, label_id) → (workspace_id, template_id)) |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Label`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "name", "workspace_id" FROM "public"."labels" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetLabelsInput`
- Returns: `[]*Label`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "id", "name", "workspace_id" FROM "public"."labels" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."labels" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *LabelFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."labels" WHERE <filter>)
```

### Count

- Params: `filter *LabelFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."labels" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[LabelFilter]`
- Returns: `*PaginateResult[Label]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[LabelFilter]`
- Returns: `*Connection[Label]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamLabelsInput`
- Returns: `iter.Seq2[*Label, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "name", "workspace_id" FROM "public"."labels" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateLabelInput`
- Returns: `*Label`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."labels" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateLabelInput`
- Returns: `[]*Label`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."labels" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateLabelInput`
- Returns: `*Label`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."labels" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateLabelItem`
- Returns: `[]*Label`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."labels" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *LabelFilter`, `input *UpdateLabelInput`
- Returns: `[]*Label`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."labels" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateLabelInput`, `target LabelConflictTarget`
- Returns: `*Label`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated LabelConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."labels" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateLabelInput`, `target LabelConflictTarget`
- Returns: `[]*Label`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same LabelConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."labels" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### CreateWithRelated

- Params: `input *CreateLabelWithRelatedInput`
- Returns: `*Label`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (Tasks, Templates), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id int64`, `input *UpdateLabelWithRelatedInput`
- Returns: `*Label`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (Tasks, Templates), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertLabelWithRelatedInput`, `target LabelConflictTarget`
- Returns: `*Label`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (Tasks, Templates), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### HardDelete

- Params: `id int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."labels" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."labels" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *LabelFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."labels" WHERE <filter> RETURNING "id"
```

## Filter

Type `LabelFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Name | `*comparator.String` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*LabelFilter` |
| Or | `[]*LabelFilter` |

## Sort

Type `LabelSort`.

Fields: ID, Name, WorkspaceID

## Read examples

```go
row, err := client.Labels().Get(ctx, 1)
rows, err := client.Labels().GetMany(ctx, &models.GetLabelsInput{Filter: &models.LabelFilter{Name: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.Labels().Create(ctx, &models.CreateLabelInput{Name: "example"})
updated, err := client.Labels().Update(ctx, 1, &models.UpdateLabelInput{WorkspaceID: omittable.Set(uuid.NewV4())})
```
