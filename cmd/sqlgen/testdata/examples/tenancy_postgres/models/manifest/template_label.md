# TemplateLabel

- **Table:** `template_labels` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: composite
- Struct: `TemplateLabelPK`
- `workspace_id` — WorkspaceID `uuid.UUID`
- `template_id` — TemplateID `int64`
- `label_id` — LabelID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `workspace_id` | WorkspaceID | `uuid.UUID` | `uuid` |  | yes |  |  | `comparator.ID` |  |
| `template_id` | TemplateID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `label_id` | LabelID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `template_labels_pkey` | workspace_id, template_id, label_id | yes | btree |  |

## Query methods

### Get

- Params: `pk TemplateLabelPK`
- Returns: `*TemplateLabel`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "label_id", "template_id", "workspace_id" FROM "public"."template_labels" WHERE "workspace_id" = $1 AND "template_id" = $2 AND "label_id" = $3 LIMIT 1
```

### GetMany

- Params: `input *GetTemplateLabelsInput`
- Returns: `[]*TemplateLabel`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "label_id", "template_id", "workspace_id" FROM "public"."template_labels" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `pk TemplateLabelPK`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."template_labels" WHERE "workspace_id" = $1 AND "template_id" = $2 AND "label_id" = $3)
```

### ExistsWhere

- Params: `filter *TemplateLabelFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."template_labels" WHERE <filter>)
```

### Count

- Params: `filter *TemplateLabelFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."template_labels" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[TemplateLabelFilter]`
- Returns: `*PaginateResult[TemplateLabel]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[TemplateLabelFilter]`
- Returns: `*Connection[TemplateLabel]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamTemplateLabelsInput`
- Returns: `iter.Seq2[*TemplateLabel, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "label_id", "template_id", "workspace_id" FROM "public"."template_labels" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateTemplateLabelInput`
- Returns: `*TemplateLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."template_labels" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateTemplateLabelInput`
- Returns: `[]*TemplateLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."template_labels" (<columns>) VALUES <values>
```

### Update

- Params: `pk TemplateLabelPK`, `input *UpdateTemplateLabelInput`
- Returns: `*TemplateLabel`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."template_labels" SET <set> WHERE "workspace_id" = $1 AND "template_id" = $2 AND "label_id" = $3
```

### UpdateMany

- Params: `items []UpdateTemplateLabelItem`
- Returns: `[]*TemplateLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."template_labels" SET <set> WHERE "workspace_id" = $1 AND "template_id" = $2 AND "label_id" = $3
```

### UpdateWhere

- Params: `filter *TemplateLabelFilter`, `input *UpdateTemplateLabelInput`
- Returns: `[]*TemplateLabel`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."template_labels" SET <set> WHERE <filter> RETURNING "workspace_id", "template_id", "label_id"
```

### Upsert

- Params: `input *CreateTemplateLabelInput`, `target TemplateLabelConflictTarget`
- Returns: `*TemplateLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated TemplateLabelConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."template_labels" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateTemplateLabelInput`, `target TemplateLabelConflictTarget`
- Returns: `[]*TemplateLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same TemplateLabelConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."template_labels" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `pk TemplateLabelPK`
- Returns: `error`
- Errors: `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."template_labels" WHERE "workspace_id" = $1 AND "template_id" = $2 AND "label_id" = $3
```

### HardDeleteMany

- Params: `pks []TemplateLabelPK`
- Returns: `error`
- Errors: `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."template_labels" WHERE ("workspace_id", "template_id", "label_id") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *TemplateLabelFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."template_labels" WHERE <filter> RETURNING "workspace_id", "template_id", "label_id"
```

## Filter

Type `TemplateLabelFilter`.

| Field | Type |
| --- | --- |
| LabelID | `*comparator.Number[int64]` |
| TemplateID | `*comparator.Number[int64]` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*TemplateLabelFilter` |
| Or | `[]*TemplateLabelFilter` |

## Sort

Type `TemplateLabelSort`.

Fields: LabelID, TemplateID, WorkspaceID

## Read examples

```go
row, err := client.TemplateLabels().Get(ctx, models.TemplateLabelPK{WorkspaceID: uuid.NewV4(), TemplateID: 1, LabelID: 1})
rows, err := client.TemplateLabels().GetMany(ctx, &models.GetTemplateLabelsInput{Filter: &models.TemplateLabelFilter{LabelID: &comparator.Number[int64]{Eq: new(int64(1))}}})
```

## Write examples

```go
created, err := client.TemplateLabels().Create(ctx, &models.CreateTemplateLabelInput{WorkspaceID: uuid.NewV4(), TemplateID: 1, LabelID: 1})
updated, err := client.TemplateLabels().Update(ctx, models.TemplateLabelPK{WorkspaceID: uuid.NewV4(), TemplateID: 1, LabelID: 1}, &models.UpdateTemplateLabelInput{})
```
