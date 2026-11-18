# BoolKeyed

- **Table:** `bool_keyed` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `k` — K `bool`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `k` | K | `bool` | `boolean` |  | yes |  |  | `comparator.Bool` |  |
| `label` | Label | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `bool_keyed_pkey` | k | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| bool_keyed_notes | o2m | BoolKeyedNote | public.bool_keyed_notes.keyed_k |  |

## Query methods

### Get

- Params: `id bool`
- Returns: `*BoolKeyed`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "k", "label" FROM "public"."bool_keyed" WHERE "k" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetBoolKeyedsInput`
- Returns: `[]*BoolKeyed`, `error`

Generated SQL:

postgres:

```sql
SELECT "k", "label" FROM "public"."bool_keyed" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id bool`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."bool_keyed" WHERE "k" = $1)
```

### ExistsWhere

- Params: `filter *BoolKeyedFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."bool_keyed" WHERE <filter>)
```

### Count

- Params: `filter *BoolKeyedFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."bool_keyed" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[BoolKeyedFilter]`
- Returns: `*PaginateResult[BoolKeyed]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[BoolKeyedFilter]`
- Returns: `*Connection[BoolKeyed]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamBoolKeyedsInput`
- Returns: `iter.Seq2[*BoolKeyed, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "k", "label" FROM "public"."bool_keyed" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateBoolKeyedInput`
- Returns: `*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."bool_keyed" (<columns>) VALUES (<values>) RETURNING "k"
```

### CreateMany

- Params: `inputs []*CreateBoolKeyedInput`
- Returns: `[]*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."bool_keyed" (<columns>) VALUES <values> RETURNING "k"
```

### Update

- Params: `id bool`, `input *UpdateBoolKeyedInput`
- Returns: `*BoolKeyed`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."bool_keyed" SET <set> WHERE "k" = $1
```

### UpdateMany

- Params: `items []UpdateBoolKeyedItem`
- Returns: `[]*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."bool_keyed" SET <set> WHERE "k" = $1
```

### UpdateWhere

- Params: `filter *BoolKeyedFilter`, `input *UpdateBoolKeyedInput`
- Returns: `[]*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."bool_keyed" SET <set> WHERE <filter> RETURNING "k"
```

### Upsert

- Params: `input *CreateBoolKeyedInput`, `target BoolKeyedConflictTarget`
- Returns: `*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated BoolKeyedConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."bool_keyed" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "k"
```

### UpsertMany

- Params: `inputs []*CreateBoolKeyedInput`, `target BoolKeyedConflictTarget`
- Returns: `[]*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same BoolKeyedConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."bool_keyed" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id bool`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."bool_keyed" WHERE "k" = $1
```

### HardDeleteMany

- Params: `ids []bool`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."bool_keyed" WHERE "k" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *BoolKeyedFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."bool_keyed" WHERE <filter> RETURNING "k"
```

## Filter

Type `BoolKeyedFilter`.

| Field | Type |
| --- | --- |
| K | `*comparator.Bool` |
| Label | `*comparator.String` |
| And | `[]*BoolKeyedFilter` |
| Or | `[]*BoolKeyedFilter` |

## Sort

Type `BoolKeyedSort`.

Fields: K, Label

## Read examples

```go
row, err := client.BoolKeyeds().Get(ctx, true)
rows, err := client.BoolKeyeds().GetMany(ctx, &models.GetBoolKeyedsInput{Filter: &models.BoolKeyedFilter{Label: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.BoolKeyeds().Create(ctx, &models.CreateBoolKeyedInput{K: true, Label: "example"})
updated, err := client.BoolKeyeds().Update(ctx, true, &models.UpdateBoolKeyedInput{Label: omittable.Set("example")})
```
