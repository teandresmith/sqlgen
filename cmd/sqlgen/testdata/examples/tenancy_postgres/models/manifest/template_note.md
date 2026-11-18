# TemplateNote

- **Table:** `template_notes` (schema `public`)
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
| `template_id` | TemplateID | `*int64` | `bigint` | yes |  |  |  | `comparator.NullableNumber[int64]` |  |
| `body` | Body | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `template_notes_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*TemplateNote`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "body", "id", "template_id", "workspace_id" FROM "public"."template_notes" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetTemplateNotesInput`
- Returns: `[]*TemplateNote`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "body", "id", "template_id", "workspace_id" FROM "public"."template_notes" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."template_notes" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *TemplateNoteFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."template_notes" WHERE <filter>)
```

### Count

- Params: `filter *TemplateNoteFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."template_notes" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[TemplateNoteFilter]`
- Returns: `*PaginateResult[TemplateNote]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[TemplateNoteFilter]`
- Returns: `*Connection[TemplateNote]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamTemplateNotesInput`
- Returns: `iter.Seq2[*TemplateNote, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "body", "id", "template_id", "workspace_id" FROM "public"."template_notes" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateTemplateNoteInput`
- Returns: `*TemplateNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."template_notes" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateTemplateNoteInput`
- Returns: `[]*TemplateNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."template_notes" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateTemplateNoteInput`
- Returns: `*TemplateNote`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."template_notes" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateTemplateNoteItem`
- Returns: `[]*TemplateNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."template_notes" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *TemplateNoteFilter`, `input *UpdateTemplateNoteInput`
- Returns: `[]*TemplateNote`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."template_notes" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateTemplateNoteInput`, `target TemplateNoteConflictTarget`
- Returns: `*TemplateNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated TemplateNoteConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."template_notes" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateTemplateNoteInput`, `target TemplateNoteConflictTarget`
- Returns: `[]*TemplateNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same TemplateNoteConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."template_notes" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."template_notes" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."template_notes" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *TemplateNoteFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."template_notes" WHERE <filter> RETURNING "id"
```

## Filter

Type `TemplateNoteFilter`.

| Field | Type |
| --- | --- |
| Body | `*comparator.String` |
| ID | `*comparator.Number[int64]` |
| TemplateID | `*comparator.NullableNumber[int64]` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*TemplateNoteFilter` |
| Or | `[]*TemplateNoteFilter` |

## Sort

Type `TemplateNoteSort`.

Fields: Body, ID, TemplateID, WorkspaceID

## Read examples

```go
row, err := client.TemplateNotes().Get(ctx, 1)
rows, err := client.TemplateNotes().GetMany(ctx, &models.GetTemplateNotesInput{Filter: &models.TemplateNoteFilter{Body: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.TemplateNotes().Create(ctx, &models.CreateTemplateNoteInput{Body: "example"})
updated, err := client.TemplateNotes().Update(ctx, 1, &models.UpdateTemplateNoteInput{WorkspaceID: omittable.Set(uuid.NewV4())})
```
