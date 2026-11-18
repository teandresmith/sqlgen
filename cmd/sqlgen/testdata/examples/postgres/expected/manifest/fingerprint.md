# Fingerprint

- **Table:** `fingerprints` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `digest` — Digest `[]byte`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `digest` | Digest | `[]byte` | `bytea` |  | yes |  |  | `comparator.Opaque[[]byte]` |  |
| `label` | Label | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `fingerprints_pkey` | digest | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| hosts | m2m | Host | public.host_fingerprints (fingerprint_digest → host_addr) |  |

## Query methods

### Get

- Params: `id []byte`
- Returns: `*Fingerprint`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "digest", "label" FROM "public"."fingerprints" WHERE "digest" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetFingerprintsInput`
- Returns: `[]*Fingerprint`, `error`

Generated SQL:

postgres:

```sql
SELECT "digest", "label" FROM "public"."fingerprints" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id []byte`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."fingerprints" WHERE "digest" = $1)
```

### ExistsWhere

- Params: `filter *FingerprintFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."fingerprints" WHERE <filter>)
```

### Count

- Params: `filter *FingerprintFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."fingerprints" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[FingerprintFilter]`
- Returns: `*PaginateResult[Fingerprint]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[FingerprintFilter]`
- Returns: `*Connection[Fingerprint]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamFingerprintsInput`
- Returns: `iter.Seq2[*Fingerprint, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "digest", "label" FROM "public"."fingerprints" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateFingerprintInput`
- Returns: `*Fingerprint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."fingerprints" (<columns>) VALUES (<values>) RETURNING "digest"
```

### CreateMany

- Params: `inputs []*CreateFingerprintInput`
- Returns: `[]*Fingerprint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."fingerprints" (<columns>) VALUES <values> RETURNING "digest"
```

### Update

- Params: `id []byte`, `input *UpdateFingerprintInput`
- Returns: `*Fingerprint`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."fingerprints" SET <set> WHERE "digest" = $1
```

### UpdateMany

- Params: `items []UpdateFingerprintItem`
- Returns: `[]*Fingerprint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."fingerprints" SET <set> WHERE "digest" = $1
```

### UpdateWhere

- Params: `filter *FingerprintFilter`, `input *UpdateFingerprintInput`
- Returns: `[]*Fingerprint`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."fingerprints" SET <set> WHERE <filter> RETURNING "digest"
```

### Upsert

- Params: `input *CreateFingerprintInput`, `target FingerprintConflictTarget`
- Returns: `*Fingerprint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated FingerprintConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."fingerprints" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "digest"
```

### UpsertMany

- Params: `inputs []*CreateFingerprintInput`, `target FingerprintConflictTarget`
- Returns: `[]*Fingerprint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same FingerprintConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."fingerprints" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id []byte`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."fingerprints" WHERE "digest" = $1
```

### HardDeleteMany

- Params: `ids [][]byte`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."fingerprints" WHERE "digest" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *FingerprintFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."fingerprints" WHERE <filter> RETURNING "digest"
```

## Filter

Type `FingerprintFilter`.

| Field | Type |
| --- | --- |
| Digest | `*comparator.Opaque[[]byte]` |
| Label | `*comparator.String` |
| And | `[]*FingerprintFilter` |
| Or | `[]*FingerprintFilter` |

## Sort

Type `FingerprintSort`.

Fields: Digest, Label

## Read examples

```go
row, err := client.Fingerprints().Get(ctx, []byte("example"))
rows, err := client.Fingerprints().GetMany(ctx, &models.GetFingerprintsInput{Filter: &models.FingerprintFilter{Label: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.Fingerprints().Create(ctx, &models.CreateFingerprintInput{Digest: []byte("example"), Label: "example"})
updated, err := client.Fingerprints().Update(ctx, []byte("example"), &models.UpdateFingerprintInput{Label: omittable.Set("example")})
```
