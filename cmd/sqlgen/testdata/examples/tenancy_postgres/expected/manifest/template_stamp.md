# TemplateStamp

- **Table:** `template_stamps` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: composite
- Struct: `TemplateStampPK`
- `workspace_id` — WorkspaceID `uuid.UUID`
- `template_id` — TemplateID `int64`
- `stamp_id` — StampID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `workspace_id` | WorkspaceID | `uuid.UUID` | `uuid` |  | yes |  |  | `comparator.ID` |  |
| `template_id` | TemplateID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `stamp_id` | StampID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `template_stamps_pkey` | workspace_id, template_id, stamp_id | yes | btree |  |

## Query methods

### Get

- Params: `pk TemplateStampPK`
- Returns: `*TemplateStamp`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "stamp_id", "template_id", "workspace_id" FROM "public"."template_stamps" WHERE "workspace_id" = $1 AND "template_id" = $2 AND "stamp_id" = $3 LIMIT 1
```

### GetMany

- Params: `input *GetTemplateStampsInput`
- Returns: `[]*TemplateStamp`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "stamp_id", "template_id", "workspace_id" FROM "public"."template_stamps" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `pk TemplateStampPK`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."template_stamps" WHERE "workspace_id" = $1 AND "template_id" = $2 AND "stamp_id" = $3)
```

### ExistsWhere

- Params: `filter *TemplateStampFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."template_stamps" WHERE <filter>)
```

### Count

- Params: `filter *TemplateStampFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."template_stamps" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[TemplateStampFilter]`
- Returns: `*PaginateResult[TemplateStamp]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[TemplateStampFilter]`
- Returns: `*Connection[TemplateStamp]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamTemplateStampsInput`
- Returns: `iter.Seq2[*TemplateStamp, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "stamp_id", "template_id", "workspace_id" FROM "public"."template_stamps" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateTemplateStampInput`
- Returns: `*TemplateStamp`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."template_stamps" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateTemplateStampInput`
- Returns: `[]*TemplateStamp`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."template_stamps" (<columns>) VALUES <values>
```

### Update

- Params: `pk TemplateStampPK`, `input *UpdateTemplateStampInput`
- Returns: `*TemplateStamp`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."template_stamps" SET <set> WHERE "workspace_id" = $1 AND "template_id" = $2 AND "stamp_id" = $3
```

### UpdateMany

- Params: `items []UpdateTemplateStampItem`
- Returns: `[]*TemplateStamp`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."template_stamps" SET <set> WHERE "workspace_id" = $1 AND "template_id" = $2 AND "stamp_id" = $3
```

### UpdateWhere

- Params: `filter *TemplateStampFilter`, `input *UpdateTemplateStampInput`
- Returns: `[]*TemplateStamp`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."template_stamps" SET <set> WHERE <filter> RETURNING "workspace_id", "template_id", "stamp_id"
```

### Upsert

- Params: `input *CreateTemplateStampInput`, `target TemplateStampConflictTarget`
- Returns: `*TemplateStamp`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated TemplateStampConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."template_stamps" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateTemplateStampInput`, `target TemplateStampConflictTarget`
- Returns: `[]*TemplateStamp`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same TemplateStampConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."template_stamps" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `pk TemplateStampPK`
- Returns: `error`
- Errors: `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."template_stamps" WHERE "workspace_id" = $1 AND "template_id" = $2 AND "stamp_id" = $3
```

### HardDeleteMany

- Params: `pks []TemplateStampPK`
- Returns: `error`
- Errors: `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."template_stamps" WHERE ("workspace_id", "template_id", "stamp_id") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *TemplateStampFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."template_stamps" WHERE <filter> RETURNING "workspace_id", "template_id", "stamp_id"
```

## Filter

Type `TemplateStampFilter`.

| Field | Type |
| --- | --- |
| StampID | `*comparator.Number[int64]` |
| TemplateID | `*comparator.Number[int64]` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*TemplateStampFilter` |
| Or | `[]*TemplateStampFilter` |

## Sort

Type `TemplateStampSort`.

Fields: StampID, TemplateID, WorkspaceID

## Read examples

```go
row, err := client.TemplateStamps().Get(ctx, models.TemplateStampPK{WorkspaceID: uuid.NewV4(), TemplateID: 1, StampID: 1})
rows, err := client.TemplateStamps().GetMany(ctx, &models.GetTemplateStampsInput{Filter: &models.TemplateStampFilter{StampID: &comparator.Number[int64]{Eq: new(int64(1))}}})
```

## Write examples

```go
created, err := client.TemplateStamps().Create(ctx, &models.CreateTemplateStampInput{WorkspaceID: uuid.NewV4(), TemplateID: 1, StampID: 1})
updated, err := client.TemplateStamps().Update(ctx, models.TemplateStampPK{WorkspaceID: uuid.NewV4(), TemplateID: 1, StampID: 1}, &models.UpdateTemplateStampInput{})
```
