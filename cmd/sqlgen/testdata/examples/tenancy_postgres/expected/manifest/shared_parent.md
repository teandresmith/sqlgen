# SharedParent

- **Table:** `shared_parents` (schema `public`)
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
| `org` | Org | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `shared_parents_org_id_key` | org, id | yes | btree |  |
| `shared_parents_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| shared_kids | o2m | SharedKid | public.shared_kids.(org, parent_id) |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*SharedParent`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "org", "workspace_id" FROM "public"."shared_parents" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetSharedParentsInput`
- Returns: `[]*SharedParent`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "id", "org", "workspace_id" FROM "public"."shared_parents" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."shared_parents" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *SharedParentFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."shared_parents" WHERE <filter>)
```

### Count

- Params: `filter *SharedParentFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."shared_parents" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[SharedParentFilter]`
- Returns: `*PaginateResult[SharedParent]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[SharedParentFilter]`
- Returns: `*Connection[SharedParent]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamSharedParentsInput`
- Returns: `iter.Seq2[*SharedParent, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "org", "workspace_id" FROM "public"."shared_parents" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateSharedParentInput`
- Returns: `*SharedParent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."shared_parents" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateSharedParentInput`
- Returns: `[]*SharedParent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."shared_parents" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateSharedParentInput`
- Returns: `*SharedParent`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."shared_parents" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateSharedParentItem`
- Returns: `[]*SharedParent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."shared_parents" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *SharedParentFilter`, `input *UpdateSharedParentInput`
- Returns: `[]*SharedParent`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."shared_parents" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateSharedParentInput`, `target SharedParentConflictTarget`
- Returns: `*SharedParent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated SharedParentConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."shared_parents" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateSharedParentInput`, `target SharedParentConflictTarget`
- Returns: `[]*SharedParent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same SharedParentConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."shared_parents" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### CreateWithRelated

- Params: `input *CreateSharedParentWithRelatedInput`
- Returns: `*SharedParent`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (SharedKids), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id int64`, `input *UpdateSharedParentWithRelatedInput`
- Returns: `*SharedParent`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (SharedKids), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertSharedParentWithRelatedInput`, `target SharedParentConflictTarget`
- Returns: `*SharedParent`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (SharedKids), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### HardDelete

- Params: `id int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."shared_parents" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."shared_parents" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *SharedParentFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."shared_parents" WHERE <filter> RETURNING "id"
```

## Filter

Type `SharedParentFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Org | `*comparator.String` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*SharedParentFilter` |
| Or | `[]*SharedParentFilter` |

## Sort

Type `SharedParentSort`.

Fields: ID, Org, WorkspaceID

## Read examples

```go
row, err := client.SharedParents().Get(ctx, 1)
rows, err := client.SharedParents().GetMany(ctx, &models.GetSharedParentsInput{Filter: &models.SharedParentFilter{Org: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.SharedParents().Create(ctx, &models.CreateSharedParentInput{Org: "example"})
updated, err := client.SharedParents().Update(ctx, 1, &models.UpdateSharedParentInput{WorkspaceID: omittable.Set(uuid.NewV4())})
```
