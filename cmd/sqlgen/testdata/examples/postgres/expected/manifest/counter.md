# Counter

- **Table:** `counters` (schema `public`)
- **Kind:** table

Counter rows keyed via primary_key.columns override (single-column PK)

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `key` — Key `string`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `key` | Key | `string` | `text` |  | yes | yes |  | `comparator.ID` | Logical counter identity (post-ALTER UNIQUE, promoted to PK via override) |
| `slot` | Slot | `*string` | `text` | yes |  | yes |  | `comparator.NullableString` | Second UNIQUE that does NOT cover the primary key — the only example shape where a caller-known key still has to be read back after an upsert (PRD 9.5). Nullable so existing rows leave it unset; NULL never matches under a unique index. |
| `count` | Count | `int64` | `bigint` |  |  |  | `0` | `comparator.Number[int64]` | Current counter value |
| `updated_at` | UpdatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` | Last modification timestamp |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `counters_key_uq` | key | yes | btree |  |
| `counters_slot_uq` | slot | yes | btree |  |

## Query methods

### Get

- Params: `id string`
- Returns: `*Counter`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "count", "key", "slot", "updated_at" FROM "public"."counters" WHERE "key" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetCountersInput`
- Returns: `[]*Counter`, `error`

Generated SQL:

postgres:

```sql
SELECT "count", "key", "slot", "updated_at" FROM "public"."counters" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *CounterFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."counters" WHERE <filter>
```

### Exists

- Params: `id string`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."counters" WHERE "key" = $1)
```

### ExistsWhere

- Params: `filter *CounterFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."counters" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[CounterFilter]`
- Returns: `*PaginateResult[Counter]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[CounterFilter]`
- Returns: `*Connection[Counter]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamCountersInput`
- Returns: `iter.Seq2[*Counter, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "count", "key", "slot", "updated_at" FROM "public"."counters" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateCounterInput`
- Returns: `*Counter`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."counters" (<columns>) VALUES (<values>) RETURNING "key"
```

### CreateMany

- Params: `inputs []*CreateCounterInput`
- Returns: `[]*Counter`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."counters" (<columns>) VALUES <values> RETURNING "key"
```

### Upsert

- Params: `input *CreateCounterInput`, `target CounterConflictTarget`
- Returns: `*Counter`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated CounterConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."counters" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "key"
```

### UpsertMany

- Params: `inputs []*CreateCounterInput`, `target CounterConflictTarget`
- Returns: `[]*Counter`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same CounterConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."counters" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id string`, `input *UpdateCounterInput`
- Returns: `*Counter`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."counters" SET <set> WHERE "key" = $1
```

### UpdateMany

- Params: `items []UpdateCounterItem`
- Returns: `[]*Counter`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."counters" SET <set> WHERE "key" = $1
```

### UpdateWhere

- Params: `filter *CounterFilter`, `input *UpdateCounterInput`
- Returns: `[]*Counter`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."counters" SET <set> WHERE <filter> RETURNING "key"
```

### Increment

- Params: `id string`, `input IncrementInput[CounterIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated CounterIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."counters" SET <column> = <column> + $1 WHERE "key" = $2
```

### HardDelete

- Params: `id string`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."counters" WHERE "key" = $1
```

### HardDeleteMany

- Params: `ids []string`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."counters" WHERE "key" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *CounterFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."counters" WHERE <filter> RETURNING "key"
```

## Filter

Type `CounterFilter`.

| Field | Type |
| --- | --- |
| Count | `*comparator.Number[int64]` |
| Key | `*comparator.ID` |
| Slot | `*comparator.NullableString` |
| UpdatedAt | `*comparator.Time` |
| And | `[]*CounterFilter` |
| Or | `[]*CounterFilter` |

## Sort

Type `CounterSort`.

Fields: Count, Key, Slot, UpdatedAt
