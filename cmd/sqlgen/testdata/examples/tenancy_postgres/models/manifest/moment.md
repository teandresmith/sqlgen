# Moment

- **Table:** `moments` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `at` — At `time.Time`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `at` | At | `time.Time` | `timestamptz` |  | yes |  |  | `comparator.Time` |  |
| `owner_id` | OwnerID | `*int64` | `bigint` | yes |  |  |  | `comparator.NullableNumber[int64]` |  |
| `label` | Label | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `moments_pkey` | at | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| moment_tags | m2m | MomentTag | public.moment_tag_links (moment_at → tag_id) |  |
| owner | o2o | MomentOwner | public.moments.owner_id |  |

## Query methods

### Get

- Params: `id time.Time`
- Returns: `*Moment`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "at", "label", "owner_id" FROM "public"."moments" WHERE "at" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetMomentsInput`
- Returns: `[]*Moment`, `error`

Generated SQL:

postgres:

```sql
SELECT "at", "label", "owner_id" FROM "public"."moments" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id time.Time`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."moments" WHERE "at" = $1)
```

### ExistsWhere

- Params: `filter *MomentFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."moments" WHERE <filter>)
```

### Count

- Params: `filter *MomentFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."moments" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[MomentFilter]`
- Returns: `*PaginateResult[Moment]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[MomentFilter]`
- Returns: `*Connection[Moment]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamMomentsInput`
- Returns: `iter.Seq2[*Moment, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "at", "label", "owner_id" FROM "public"."moments" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateMomentInput`
- Returns: `*Moment`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moments" (<columns>) VALUES (<values>) RETURNING "at"
```

### CreateMany

- Params: `inputs []*CreateMomentInput`
- Returns: `[]*Moment`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moments" (<columns>) VALUES <values> RETURNING "at"
```

### Update

- Params: `id time.Time`, `input *UpdateMomentInput`
- Returns: `*Moment`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."moments" SET <set> WHERE "at" = $1
```

### UpdateMany

- Params: `items []UpdateMomentItem`
- Returns: `[]*Moment`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."moments" SET <set> WHERE "at" = $1
```

### UpdateWhere

- Params: `filter *MomentFilter`, `input *UpdateMomentInput`
- Returns: `[]*Moment`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."moments" SET <set> WHERE <filter> RETURNING "at"
```

### Upsert

- Params: `input *CreateMomentInput`, `target MomentConflictTarget`
- Returns: `*Moment`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated MomentConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moments" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "at"
```

### UpsertMany

- Params: `inputs []*CreateMomentInput`, `target MomentConflictTarget`
- Returns: `[]*Moment`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same MomentConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moments" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### CreateWithRelated

- Params: `input *CreateMomentWithRelatedInput`
- Returns: `*Moment`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (MomentTags), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id time.Time`, `input *UpdateMomentWithRelatedInput`
- Returns: `*Moment`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (MomentTags), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertMomentWithRelatedInput`, `target MomentConflictTarget`
- Returns: `*Moment`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (MomentTags), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### HardDelete

- Params: `id time.Time`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."moments" WHERE "at" = $1
```

### HardDeleteMany

- Params: `ids []time.Time`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."moments" WHERE "at" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *MomentFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."moments" WHERE <filter> RETURNING "at"
```

## Filter

Type `MomentFilter`.

| Field | Type |
| --- | --- |
| At | `*comparator.Time` |
| Label | `*comparator.String` |
| OwnerID | `*comparator.NullableNumber[int64]` |
| And | `[]*MomentFilter` |
| Or | `[]*MomentFilter` |

## Sort

Type `MomentSort`.

Fields: At, Label, OwnerID

## Read examples

```go
row, err := client.Moments().Get(ctx, time.Now())
rows, err := client.Moments().GetMany(ctx, &models.GetMomentsInput{Filter: &models.MomentFilter{Label: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.Moments().Create(ctx, &models.CreateMomentInput{At: time.Now(), Label: "example"})
updated, err := client.Moments().Update(ctx, time.Now(), &models.UpdateMomentInput{OwnerID: omittable.Set(new(int64(1)))})
```
