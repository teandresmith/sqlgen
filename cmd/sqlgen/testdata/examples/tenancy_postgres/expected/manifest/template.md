# Template

- **Table:** `templates` (schema `public`)
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
| `owner_workspace` | OwnerWorkspace | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `templates_owner_workspace_id_key` | owner_workspace, id | yes | btree |  |
| `templates_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| labels | m2m | Label | public.template_labels ((workspace_id, template_id) → (workspace_id, label_id)) |  |
| stamps | m2m | Stamp | public.template_stamps ((workspace_id, template_id) → (workspace_id, stamp_id)) |  |
| template_steps | o2m | TemplateStep | public.template_steps.(workspace_id, template_id) |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Template`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "name", "owner_workspace" FROM "public"."templates" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetTemplatesInput`
- Returns: `[]*Template`, `error`

Generated SQL:

postgres:

```sql
SELECT "id", "name", "owner_workspace" FROM "public"."templates" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."templates" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *TemplateFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."templates" WHERE <filter>)
```

### Count

- Params: `filter *TemplateFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."templates" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[TemplateFilter]`
- Returns: `*PaginateResult[Template]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[TemplateFilter]`
- Returns: `*Connection[Template]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamTemplatesInput`
- Returns: `iter.Seq2[*Template, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "name", "owner_workspace" FROM "public"."templates" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateTemplateInput`
- Returns: `*Template`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."templates" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateTemplateInput`
- Returns: `[]*Template`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."templates" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateTemplateInput`
- Returns: `*Template`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."templates" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateTemplateItem`
- Returns: `[]*Template`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."templates" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *TemplateFilter`, `input *UpdateTemplateInput`
- Returns: `[]*Template`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."templates" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateTemplateInput`, `target TemplateConflictTarget`
- Returns: `*Template`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated TemplateConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."templates" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateTemplateInput`, `target TemplateConflictTarget`
- Returns: `[]*Template`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same TemplateConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."templates" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### CreateWithRelated

- Params: `input *CreateTemplateWithRelatedInput`
- Returns: `*Template`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (Labels, Stamps, TemplateSteps), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id int64`, `input *UpdateTemplateWithRelatedInput`
- Returns: `*Template`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (Labels, Stamps, TemplateSteps), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertTemplateWithRelatedInput`, `target TemplateConflictTarget`
- Returns: `*Template`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (Labels, Stamps, TemplateSteps), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."templates" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."templates" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *TemplateFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."templates" WHERE <filter> RETURNING "id"
```

## Filter

Type `TemplateFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Name | `*comparator.String` |
| OwnerWorkspace | `*comparator.ID` |
| And | `[]*TemplateFilter` |
| Or | `[]*TemplateFilter` |

## Sort

Type `TemplateSort`.

Fields: ID, Name, OwnerWorkspace

## Read examples

```go
row, err := client.Templates().Get(ctx, 1)
rows, err := client.Templates().GetMany(ctx, &models.GetTemplatesInput{Filter: &models.TemplateFilter{Name: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.Templates().Create(ctx, &models.CreateTemplateInput{OwnerWorkspace: uuid.NewV4(), Name: "example"})
updated, err := client.Templates().Update(ctx, 1, &models.UpdateTemplateInput{OwnerWorkspace: omittable.Set(uuid.NewV4())})
```
