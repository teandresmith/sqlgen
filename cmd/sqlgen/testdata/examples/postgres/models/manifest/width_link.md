# WidthLink

- **Table:** `width_links` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: composite
- Struct: `WidthLinkPK`
- `small_id` — SmallID `int64`
- `big_id` — BigID `int32`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `small_id` | SmallID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `big_id` | BigID | `int32` | `integer` |  | yes |  |  | `comparator.Number[int32]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `width_links_pkey` | small_id, big_id | yes | btree |  |

## Query methods

### Get

- Params: `pk WidthLinkPK`
- Returns: `*WidthLink`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "big_id", "small_id" FROM "public"."width_links" WHERE "small_id" = $1 AND "big_id" = $2 LIMIT 1
```

### GetMany

- Params: `input *GetWidthLinksInput`
- Returns: `[]*WidthLink`, `error`

Generated SQL:

postgres:

```sql
SELECT "big_id", "small_id" FROM "public"."width_links" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `pk WidthLinkPK`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."width_links" WHERE "small_id" = $1 AND "big_id" = $2)
```

### ExistsWhere

- Params: `filter *WidthLinkFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."width_links" WHERE <filter>)
```

### Count

- Params: `filter *WidthLinkFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."width_links" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[WidthLinkFilter]`
- Returns: `*PaginateResult[WidthLink]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[WidthLinkFilter]`
- Returns: `*Connection[WidthLink]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamWidthLinksInput`
- Returns: `iter.Seq2[*WidthLink, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "big_id", "small_id" FROM "public"."width_links" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateWidthLinkInput`
- Returns: `*WidthLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_links" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateWidthLinkInput`
- Returns: `[]*WidthLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_links" (<columns>) VALUES <values>
```

### Update

- Params: `pk WidthLinkPK`, `input *UpdateWidthLinkInput`
- Returns: `*WidthLink`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."width_links" SET <set> WHERE "small_id" = $1 AND "big_id" = $2
```

### UpdateMany

- Params: `items []UpdateWidthLinkItem`
- Returns: `[]*WidthLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."width_links" SET <set> WHERE "small_id" = $1 AND "big_id" = $2
```

### UpdateWhere

- Params: `filter *WidthLinkFilter`, `input *UpdateWidthLinkInput`
- Returns: `[]*WidthLink`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."width_links" SET <set> WHERE <filter> RETURNING "small_id", "big_id"
```

### Upsert

- Params: `input *CreateWidthLinkInput`, `target WidthLinkConflictTarget`
- Returns: `*WidthLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated WidthLinkConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_links" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateWidthLinkInput`, `target WidthLinkConflictTarget`
- Returns: `[]*WidthLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same WidthLinkConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."width_links" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `pk WidthLinkPK`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."width_links" WHERE "small_id" = $1 AND "big_id" = $2
```

### HardDeleteMany

- Params: `pks []WidthLinkPK`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."width_links" WHERE ("small_id", "big_id") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *WidthLinkFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."width_links" WHERE <filter> RETURNING "small_id", "big_id"
```

## Filter

Type `WidthLinkFilter`.

| Field | Type |
| --- | --- |
| BigID | `*comparator.Number[int32]` |
| SmallID | `*comparator.Number[int64]` |
| And | `[]*WidthLinkFilter` |
| Or | `[]*WidthLinkFilter` |

## Sort

Type `WidthLinkSort`.

Fields: BigID, SmallID

## Read examples

```go
row, err := client.WidthLinks().Get(ctx, models.WidthLinkPK{SmallID: 1, BigID: 1})
rows, err := client.WidthLinks().GetMany(ctx, &models.GetWidthLinksInput{Filter: &models.WidthLinkFilter{BigID: &comparator.Number[int32]{Eq: new(int32(1))}}})
```

## Write examples

```go
created, err := client.WidthLinks().Create(ctx, &models.CreateWidthLinkInput{SmallID: 1, BigID: 1})
updated, err := client.WidthLinks().Update(ctx, models.WidthLinkPK{SmallID: 1, BigID: 1}, &models.UpdateWidthLinkInput{})
```
