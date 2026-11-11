# MomentOwner

- **Table:** `moment_owners` (schema `public`)
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
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `moment_owners_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| moments | o2m | Moment | public.moments.owner_id |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*MomentOwner`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "name" FROM "public"."moment_owners" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetMomentOwnersInput`
- Returns: `[]*MomentOwner`, `error`

Generated SQL:

postgres:

```sql
SELECT "id", "name" FROM "public"."moment_owners" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."moment_owners" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *MomentOwnerFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."moment_owners" WHERE <filter>)
```

### Count

- Params: `filter *MomentOwnerFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."moment_owners" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[MomentOwnerFilter]`
- Returns: `*PaginateResult[MomentOwner]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[MomentOwnerFilter]`
- Returns: `*Connection[MomentOwner]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamMomentOwnersInput`
- Returns: `iter.Seq2[*MomentOwner, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "name" FROM "public"."moment_owners" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateMomentOwnerInput`
- Returns: `*MomentOwner`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moment_owners" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateMomentOwnerInput`
- Returns: `[]*MomentOwner`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moment_owners" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateMomentOwnerInput`
- Returns: `*MomentOwner`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."moment_owners" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateMomentOwnerItem`
- Returns: `[]*MomentOwner`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."moment_owners" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *MomentOwnerFilter`, `input *UpdateMomentOwnerInput`
- Returns: `[]*MomentOwner`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."moment_owners" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateMomentOwnerInput`, `target MomentOwnerConflictTarget`
- Returns: `*MomentOwner`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated MomentOwnerConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moment_owners" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateMomentOwnerInput`, `target MomentOwnerConflictTarget`
- Returns: `[]*MomentOwner`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same MomentOwnerConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moment_owners" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### CreateWithRelated

- Params: `input *CreateMomentOwnerWithRelatedInput`
- Returns: `*MomentOwner`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (Moments), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id int64`, `input *UpdateMomentOwnerWithRelatedInput`
- Returns: `*MomentOwner`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (Moments), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertMomentOwnerWithRelatedInput`, `target MomentOwnerConflictTarget`
- Returns: `*MomentOwner`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (Moments), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."moment_owners" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."moment_owners" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *MomentOwnerFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."moment_owners" WHERE <filter> RETURNING "id"
```

## Filter

Type `MomentOwnerFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Name | `*comparator.String` |
| And | `[]*MomentOwnerFilter` |
| Or | `[]*MomentOwnerFilter` |

## Sort

Type `MomentOwnerSort`.

Fields: ID, Name

## Read examples

```go
row, err := client.MomentOwners().Get(ctx, 1)
rows, err := client.MomentOwners().GetMany(ctx, &models.GetMomentOwnersInput{Filter: &models.MomentOwnerFilter{Name: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.MomentOwners().Create(ctx, &models.CreateMomentOwnerInput{Name: "example"})
updated, err := client.MomentOwners().Update(ctx, 1, &models.UpdateMomentOwnerInput{Name: omittable.Set("example")})
```
