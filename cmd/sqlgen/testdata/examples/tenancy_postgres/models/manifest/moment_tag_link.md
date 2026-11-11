# MomentTagLink

- **Table:** `moment_tag_links` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: composite
- Struct: `MomentTagLinkPK`
- `tag_id` — TagID `int64`
- `moment_at` — MomentAt `time.Time`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `tag_id` | TagID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `moment_at` | MomentAt | `time.Time` | `timestamptz` |  | yes |  |  | `comparator.Time` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `moment_tag_links_pkey` | tag_id, moment_at | yes | btree |  |

## Query methods

### Get

- Params: `pk MomentTagLinkPK`
- Returns: `*MomentTagLink`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "moment_at", "tag_id" FROM "public"."moment_tag_links" WHERE "tag_id" = $1 AND "moment_at" = $2 LIMIT 1
```

### GetMany

- Params: `input *GetMomentTagLinksInput`
- Returns: `[]*MomentTagLink`, `error`

Generated SQL:

postgres:

```sql
SELECT "moment_at", "tag_id" FROM "public"."moment_tag_links" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `pk MomentTagLinkPK`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."moment_tag_links" WHERE "tag_id" = $1 AND "moment_at" = $2)
```

### ExistsWhere

- Params: `filter *MomentTagLinkFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."moment_tag_links" WHERE <filter>)
```

### Count

- Params: `filter *MomentTagLinkFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."moment_tag_links" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[MomentTagLinkFilter]`
- Returns: `*PaginateResult[MomentTagLink]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[MomentTagLinkFilter]`
- Returns: `*Connection[MomentTagLink]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamMomentTagLinksInput`
- Returns: `iter.Seq2[*MomentTagLink, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "moment_at", "tag_id" FROM "public"."moment_tag_links" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateMomentTagLinkInput`
- Returns: `*MomentTagLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moment_tag_links" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateMomentTagLinkInput`
- Returns: `[]*MomentTagLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moment_tag_links" (<columns>) VALUES <values>
```

### Update

- Params: `pk MomentTagLinkPK`, `input *UpdateMomentTagLinkInput`
- Returns: `*MomentTagLink`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."moment_tag_links" SET <set> WHERE "tag_id" = $1 AND "moment_at" = $2
```

### UpdateMany

- Params: `items []UpdateMomentTagLinkItem`
- Returns: `[]*MomentTagLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."moment_tag_links" SET <set> WHERE "tag_id" = $1 AND "moment_at" = $2
```

### UpdateWhere

- Params: `filter *MomentTagLinkFilter`, `input *UpdateMomentTagLinkInput`
- Returns: `[]*MomentTagLink`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."moment_tag_links" SET <set> WHERE <filter> RETURNING "tag_id", "moment_at"
```

### Upsert

- Params: `input *CreateMomentTagLinkInput`, `target MomentTagLinkConflictTarget`
- Returns: `*MomentTagLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated MomentTagLinkConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moment_tag_links" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateMomentTagLinkInput`, `target MomentTagLinkConflictTarget`
- Returns: `[]*MomentTagLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same MomentTagLinkConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."moment_tag_links" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `pk MomentTagLinkPK`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."moment_tag_links" WHERE "tag_id" = $1 AND "moment_at" = $2
```

### HardDeleteMany

- Params: `pks []MomentTagLinkPK`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."moment_tag_links" WHERE ("tag_id", "moment_at") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *MomentTagLinkFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."moment_tag_links" WHERE <filter> RETURNING "tag_id", "moment_at"
```

## Filter

Type `MomentTagLinkFilter`.

| Field | Type |
| --- | --- |
| MomentAt | `*comparator.Time` |
| TagID | `*comparator.Number[int64]` |
| And | `[]*MomentTagLinkFilter` |
| Or | `[]*MomentTagLinkFilter` |

## Sort

Type `MomentTagLinkSort`.

Fields: MomentAt, TagID

## Read examples

```go
row, err := client.MomentTagLinks().Get(ctx, models.MomentTagLinkPK{TagID: 1, MomentAt: time.Now()})
rows, err := client.MomentTagLinks().GetMany(ctx, &models.GetMomentTagLinksInput{Filter: &models.MomentTagLinkFilter{TagID: &comparator.Number[int64]{Eq: new(int64(1))}}})
```

## Write examples

```go
created, err := client.MomentTagLinks().Create(ctx, &models.CreateMomentTagLinkInput{TagID: 1, MomentAt: time.Now()})
updated, err := client.MomentTagLinks().Update(ctx, models.MomentTagLinkPK{TagID: 1, MomentAt: time.Now()}, &models.UpdateMomentTagLinkInput{})
```
