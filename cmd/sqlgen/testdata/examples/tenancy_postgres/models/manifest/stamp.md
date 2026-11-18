# Stamp

- **Table:** `stamps` (schema `public`)
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
| `stamps_owner_workspace_id_key` | owner_workspace, id | yes | btree |  |
| `stamps_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| templates | m2m | Template | public.template_stamps ((workspace_id, stamp_id) → (workspace_id, template_id)) |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Stamp`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "name", "owner_workspace" FROM "public"."stamps" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetStampsInput`
- Returns: `[]*Stamp`, `error`

Generated SQL:

postgres:

```sql
SELECT "id", "name", "owner_workspace" FROM "public"."stamps" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."stamps" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *StampFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."stamps" WHERE <filter>)
```

### Count

- Params: `filter *StampFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."stamps" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[StampFilter]`
- Returns: `*PaginateResult[Stamp]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[StampFilter]`
- Returns: `*Connection[Stamp]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamStampsInput`
- Returns: `iter.Seq2[*Stamp, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "name", "owner_workspace" FROM "public"."stamps" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateStampInput`
- Returns: `*Stamp`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."stamps" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateStampInput`
- Returns: `[]*Stamp`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."stamps" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateStampInput`
- Returns: `*Stamp`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."stamps" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateStampItem`
- Returns: `[]*Stamp`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."stamps" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *StampFilter`, `input *UpdateStampInput`
- Returns: `[]*Stamp`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."stamps" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateStampInput`, `target StampConflictTarget`
- Returns: `*Stamp`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated StampConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."stamps" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateStampInput`, `target StampConflictTarget`
- Returns: `[]*Stamp`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same StampConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."stamps" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### CreateWithRelated

- Params: `input *CreateStampWithRelatedInput`
- Returns: `*Stamp`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (Templates), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id int64`, `input *UpdateStampWithRelatedInput`
- Returns: `*Stamp`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (Templates), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertStampWithRelatedInput`, `target StampConflictTarget`
- Returns: `*Stamp`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (Templates), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."stamps" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."stamps" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *StampFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."stamps" WHERE <filter> RETURNING "id"
```

## Filter

Type `StampFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Name | `*comparator.String` |
| OwnerWorkspace | `*comparator.ID` |
| And | `[]*StampFilter` |
| Or | `[]*StampFilter` |

## Sort

Type `StampSort`.

Fields: ID, Name, OwnerWorkspace

## Read examples

```go
row, err := client.Stamps().Get(ctx, 1)
rows, err := client.Stamps().GetMany(ctx, &models.GetStampsInput{Filter: &models.StampFilter{Name: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.Stamps().Create(ctx, &models.CreateStampInput{OwnerWorkspace: uuid.NewV4(), Name: "example"})
updated, err := client.Stamps().Update(ctx, 1, &models.UpdateStampInput{OwnerWorkspace: omittable.Set(uuid.NewV4())})
```
