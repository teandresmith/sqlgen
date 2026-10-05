# RateLimit

- **Table:** `rate_limits` (schema `public`)
- **Kind:** table

Per-org rate-limit buckets keyed via primary_key.columns override (composite PK)

## Files

- `models_gen.go`

## Primary key

- Kind: composite
- Struct: `RateLimitPK`
- `org_id` — OrgID `int64`
- `bucket` — Bucket `string`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `org_id` | OrgID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` | Tenant identifier |
| `bucket` | Bucket | `string` | `text` |  | yes |  |  | `comparator.ID` | Bucket key (e.g. minute / hour window) |
| `count` | Count | `int32` | `integer` |  |  |  | `0` | `comparator.Number[int32]` | Observed request count |
| `updated_at` | UpdatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` | Last modification timestamp |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `rate_limits_org_bucket_uq` | org_id, bucket | yes | btree |  |

## Query methods

### Get

- Params: `pk RateLimitPK`
- Returns: `*RateLimit`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "bucket", "count", "org_id", "updated_at" FROM "public"."rate_limits" WHERE "org_id" = $1 AND "bucket" = $2 LIMIT 1
```

### GetMany

- Params: `input *GetRateLimitsInput`
- Returns: `[]*RateLimit`, `error`

Generated SQL:

postgres:

```sql
SELECT "bucket", "count", "org_id", "updated_at" FROM "public"."rate_limits" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *RateLimitFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."rate_limits" WHERE <filter>
```

### Exists

- Params: `pk RateLimitPK`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."rate_limits" WHERE "org_id" = $1 AND "bucket" = $2)
```

### ExistsWhere

- Params: `filter *RateLimitFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."rate_limits" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[RateLimitFilter]`
- Returns: `*PaginateResult[RateLimit]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[RateLimitFilter]`
- Returns: `*Connection[RateLimit]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamRateLimitsInput`
- Returns: `iter.Seq2[*RateLimit, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "bucket", "count", "org_id", "updated_at" FROM "public"."rate_limits" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateRateLimitInput`
- Returns: `*RateLimit`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."rate_limits" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateRateLimitInput`
- Returns: `[]*RateLimit`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."rate_limits" (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateRateLimitInput`, `target RateLimitConflictTarget`
- Returns: `*RateLimit`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated RateLimitConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."rate_limits" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateRateLimitInput`, `target RateLimitConflictTarget`
- Returns: `[]*RateLimit`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same RateLimitConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."rate_limits" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `pk RateLimitPK`, `input *UpdateRateLimitInput`
- Returns: `*RateLimit`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."rate_limits" SET <set> WHERE "org_id" = $1 AND "bucket" = $2
```

### UpdateMany

- Params: `items []UpdateRateLimitItem`
- Returns: `[]*RateLimit`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."rate_limits" SET <set> WHERE "org_id" = $1 AND "bucket" = $2
```

### UpdateWhere

- Params: `filter *RateLimitFilter`, `input *UpdateRateLimitInput`
- Returns: `[]*RateLimit`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."rate_limits" SET <set> WHERE <filter> RETURNING "org_id", "bucket"
```

### Increment

- Params: `pk RateLimitPK`, `input IncrementInput[RateLimitIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated RateLimitIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."rate_limits" SET <column> = <column> + $1 WHERE "org_id" = $2 AND "bucket" = $3
```

### HardDelete

- Params: `pk RateLimitPK`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."rate_limits" WHERE "org_id" = $1 AND "bucket" = $2
```

### HardDeleteMany

- Params: `pks []RateLimitPK`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."rate_limits" WHERE ("org_id", "bucket") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *RateLimitFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."rate_limits" WHERE <filter> RETURNING "org_id", "bucket"
```

## Filter

Type `RateLimitFilter`.

| Field | Type |
| --- | --- |
| Bucket | `*comparator.ID` |
| Count | `*comparator.Number[int32]` |
| OrgID | `*comparator.Number[int64]` |
| UpdatedAt | `*comparator.Time` |
| And | `[]*RateLimitFilter` |
| Or | `[]*RateLimitFilter` |

## Sort

Type `RateLimitSort`.

Fields: Bucket, Count, OrgID, UpdatedAt
