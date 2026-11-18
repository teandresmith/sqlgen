# HostFingerprint

- **Table:** `host_fingerprints` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: composite
- Struct: `HostFingerprintPK`
- `host_addr` — HostAddr `net.IP`
- `fingerprint_digest` — FingerprintDigest `[]byte`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `host_addr` | HostAddr | `net.IP` | `inet` |  | yes |  |  | `comparator.Opaque[net.IP]` |  |
| `fingerprint_digest` | FingerprintDigest | `[]byte` | `bytea` |  | yes |  |  | `comparator.Opaque[[]byte]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `host_fingerprints_pkey` | host_addr, fingerprint_digest | yes | btree |  |

## Query methods

### Get

- Params: `pk HostFingerprintPK`
- Returns: `*HostFingerprint`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "fingerprint_digest", "host_addr" FROM "public"."host_fingerprints" WHERE "host_addr" = $1 AND "fingerprint_digest" = $2 LIMIT 1
```

### GetMany

- Params: `input *GetHostFingerprintsInput`
- Returns: `[]*HostFingerprint`, `error`

Generated SQL:

postgres:

```sql
SELECT "fingerprint_digest", "host_addr" FROM "public"."host_fingerprints" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `pk HostFingerprintPK`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."host_fingerprints" WHERE "host_addr" = $1 AND "fingerprint_digest" = $2)
```

### ExistsWhere

- Params: `filter *HostFingerprintFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."host_fingerprints" WHERE <filter>)
```

### Count

- Params: `filter *HostFingerprintFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."host_fingerprints" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[HostFingerprintFilter]`
- Returns: `*PaginateResult[HostFingerprint]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[HostFingerprintFilter]`
- Returns: `*Connection[HostFingerprint]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamHostFingerprintsInput`
- Returns: `iter.Seq2[*HostFingerprint, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "fingerprint_digest", "host_addr" FROM "public"."host_fingerprints" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateHostFingerprintInput`
- Returns: `*HostFingerprint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."host_fingerprints" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateHostFingerprintInput`
- Returns: `[]*HostFingerprint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."host_fingerprints" (<columns>) VALUES <values>
```

### Update

- Params: `pk HostFingerprintPK`, `input *UpdateHostFingerprintInput`
- Returns: `*HostFingerprint`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."host_fingerprints" SET <set> WHERE "host_addr" = $1 AND "fingerprint_digest" = $2
```

### UpdateMany

- Params: `items []UpdateHostFingerprintItem`
- Returns: `[]*HostFingerprint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."host_fingerprints" SET <set> WHERE "host_addr" = $1 AND "fingerprint_digest" = $2
```

### UpdateWhere

- Params: `filter *HostFingerprintFilter`, `input *UpdateHostFingerprintInput`
- Returns: `[]*HostFingerprint`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."host_fingerprints" SET <set> WHERE <filter> RETURNING "host_addr", "fingerprint_digest"
```

### Upsert

- Params: `input *CreateHostFingerprintInput`, `target HostFingerprintConflictTarget`
- Returns: `*HostFingerprint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated HostFingerprintConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."host_fingerprints" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateHostFingerprintInput`, `target HostFingerprintConflictTarget`
- Returns: `[]*HostFingerprint`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same HostFingerprintConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."host_fingerprints" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `pk HostFingerprintPK`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."host_fingerprints" WHERE "host_addr" = $1 AND "fingerprint_digest" = $2
```

### HardDeleteMany

- Params: `pks []HostFingerprintPK`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."host_fingerprints" WHERE ("host_addr", "fingerprint_digest") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *HostFingerprintFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."host_fingerprints" WHERE <filter> RETURNING "host_addr", "fingerprint_digest"
```

## Filter

Type `HostFingerprintFilter`.

| Field | Type |
| --- | --- |
| FingerprintDigest | `*comparator.Opaque[[]byte]` |
| HostAddr | `*comparator.Opaque[net.IP]` |
| And | `[]*HostFingerprintFilter` |
| Or | `[]*HostFingerprintFilter` |

## Sort

Type `HostFingerprintSort`.

Fields: FingerprintDigest, HostAddr

## Read examples

```go
row, err := client.HostFingerprints().Get(ctx, models.HostFingerprintPK{HostAddr: net.ParseIP("192.0.2.1"), FingerprintDigest: []byte("example")})
rows, err := client.HostFingerprints().GetMany(ctx, &models.GetHostFingerprintsInput{})
```

## Write examples

```go
created, err := client.HostFingerprints().Create(ctx, &models.CreateHostFingerprintInput{HostAddr: net.ParseIP("192.0.2.1"), FingerprintDigest: []byte("example")})
updated, err := client.HostFingerprints().Update(ctx, models.HostFingerprintPK{HostAddr: net.ParseIP("192.0.2.1"), FingerprintDigest: []byte("example")}, &models.UpdateHostFingerprintInput{})
```
