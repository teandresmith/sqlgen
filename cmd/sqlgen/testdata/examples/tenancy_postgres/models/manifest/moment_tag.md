# MomentTag

- **Table:** `moment_tags` (schema `public`)
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
| `moment_tags_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| moments | m2m | Moment | public.moment_tag_links (tag_id → moment_at) |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*MomentTag`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "name" FROM "public"."moment_tags" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetMomentTagsInput`
- Returns: `[]*MomentTag`, `error`

Generated SQL:

postgres:

```sql
SELECT "id", "name" FROM "public"."moment_tags" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."moment_tags" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *MomentTagFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."moment_tags" WHERE <filter>)
```

### Count

- Params: `filter *MomentTagFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."moment_tags" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[MomentTagFilter]`
- Returns: `*PaginateResult[MomentTag]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[MomentTagFilter]`
- Returns: `*Connection[MomentTag]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamMomentTagsInput`
- Returns: `iter.Seq2[*MomentTag, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "name" FROM "public"."moment_tags" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateMomentTagInput`
- Returns: `*MomentTag`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moment_tags" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateMomentTagInput`
- Returns: `[]*MomentTag`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moment_tags" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateMomentTagInput`
- Returns: `*MomentTag`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."moment_tags" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateMomentTagItem`
- Returns: `[]*MomentTag`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."moment_tags" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *MomentTagFilter`, `input *UpdateMomentTagInput`
- Returns: `[]*MomentTag`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."moment_tags" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateMomentTagInput`, `target MomentTagConflictTarget`
- Returns: `*MomentTag`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated MomentTagConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moment_tags" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateMomentTagInput`, `target MomentTagConflictTarget`
- Returns: `[]*MomentTag`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same MomentTagConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moment_tags" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### CreateWithRelated

- Params: `input *CreateMomentTagWithRelatedInput`
- Returns: `*MomentTag`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (Moments), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id int64`, `input *UpdateMomentTagWithRelatedInput`
- Returns: `*MomentTag`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (Moments), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertMomentTagWithRelatedInput`, `target MomentTagConflictTarget`
- Returns: `*MomentTag`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (Moments), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."moment_tags" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."moment_tags" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *MomentTagFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."moment_tags" WHERE <filter> RETURNING "id"
```

## Filter

Type `MomentTagFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Name | `*comparator.String` |
| And | `[]*MomentTagFilter` |
| Or | `[]*MomentTagFilter` |

## Sort

Type `MomentTagSort`.

Fields: ID, Name

## Read examples

```go
row, err := client.MomentTags().Get(ctx, 1)
rows, err := client.MomentTags().GetMany(ctx, &models.GetMomentTagsInput{Filter: &models.MomentTagFilter{Name: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.MomentTags().Create(ctx, &models.CreateMomentTagInput{Name: "example"})
updated, err := client.MomentTags().Update(ctx, 1, &models.UpdateMomentTagInput{Name: omittable.Set("example")})
```
