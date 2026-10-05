# Event

- **Table:** `events` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  | `gen_random_uuid()` | `comparator.ID` |  |
| `user_id` | UserID | `*uuid.UUID` | `uuid` | yes |  |  |  | `comparator.NullableID` |  |
| `action` | Action | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `occurred_at` | OccurredAt | `types.DateTime` | `timestamptz` |  |  |  |  | `comparator.Time` |  |
| `processed_at` | ProcessedAt | `types.NullDateTime` | `timestamptz` | yes |  |  |  | `comparator.NullableTime` |  |
| `adjustment` | Adjustment | `decimal.NullDecimal` | `numeric(12, 2)` | yes |  |  |  | `comparator.NullableString` |  |
| `retry_count` | RetryCount | `*int32` | `integer` | yes |  |  |  | `comparator.NullableNumber[int32]` |  |
| `succeeded` | Succeeded | `*bool` | `boolean` | yes |  |  |  | `comparator.NullableBool` |  |
| `mac` | MAC | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `os_name` | OSName | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `io_count` | IOCount | `*int32` | `integer` | yes |  |  |  | `comparator.NullableNumber[int32]` |  |
| `pk_val` | PKVal | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `fk_val` | FKVal | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `ascii_art` | ASCIIArt | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `csv_data` | CSVData | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `tcp_port` | TCPPort | `*int32` | `integer` | yes |  |  |  | `comparator.NullableNumber[int32]` |  |
| `utf8_body` | Utf8Body | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `http2_flag` | HTTP2Flag | `*bool` | `boolean` | yes |  |  |  | `comparator.NullableBool` |  |
| `line2_id` | Line2ID | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `address2_id` | Address2ID | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `s3_url` | S3URL | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `events_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*Event`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "action", "address2_id", "adjustment", "ascii_art", "csv_data", "fk_val", "http2_flag", "id", "io_count", "line2_id", "mac", "occurred_at", "os_name", "pk_val", "processed_at", "retry_count", "s3_url", "succeeded", "tcp_port", "user_id", "utf8_body" FROM "public"."events" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetEventsInput`
- Returns: `[]*Event`, `error`

Generated SQL:

postgres:

```sql
SELECT "action", "address2_id", "adjustment", "ascii_art", "csv_data", "fk_val", "http2_flag", "id", "io_count", "line2_id", "mac", "occurred_at", "os_name", "pk_val", "processed_at", "retry_count", "s3_url", "succeeded", "tcp_port", "user_id", "utf8_body" FROM "public"."events" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *EventFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."events" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."events" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *EventFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."events" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[EventFilter]`
- Returns: `*PaginateResult[Event]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[EventFilter]`
- Returns: `*Connection[Event]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamEventsInput`
- Returns: `iter.Seq2[*Event, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "action", "address2_id", "adjustment", "ascii_art", "csv_data", "fk_val", "http2_flag", "id", "io_count", "line2_id", "mac", "occurred_at", "os_name", "pk_val", "processed_at", "retry_count", "s3_url", "succeeded", "tcp_port", "user_id", "utf8_body" FROM "public"."events" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateEventInput`
- Returns: `*Event`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."events" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateEventInput`
- Returns: `[]*Event`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."events" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateEventInput`, `target EventConflictTarget`
- Returns: `*Event`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated EventConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."events" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateEventInput`, `target EventConflictTarget`
- Returns: `[]*Event`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same EventConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."events" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateEventInput`
- Returns: `*Event`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."events" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateEventItem`
- Returns: `[]*Event`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."events" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *EventFilter`, `input *UpdateEventInput`
- Returns: `[]*Event`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."events" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id uuid.UUID`, `input IncrementInput[EventIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated EventIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."events" SET <column> = <column> + $1 WHERE "id" = $2
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."events" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."events" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *EventFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."events" WHERE <filter> RETURNING "id"
```

## Filter

Type `EventFilter`.

| Field | Type |
| --- | --- |
| ASCIIArt | `*comparator.NullableString` |
| Action | `*comparator.String` |
| Address2ID | `*comparator.NullableString` |
| Adjustment | `*comparator.NullableString` |
| CSVData | `*comparator.NullableString` |
| FKVal | `*comparator.NullableString` |
| HTTP2Flag | `*comparator.NullableBool` |
| ID | `*comparator.ID` |
| IOCount | `*comparator.NullableNumber[int32]` |
| Line2ID | `*comparator.NullableString` |
| MAC | `*comparator.NullableString` |
| OSName | `*comparator.NullableString` |
| OccurredAt | `*comparator.Time` |
| PKVal | `*comparator.NullableString` |
| ProcessedAt | `*comparator.NullableTime` |
| RetryCount | `*comparator.NullableNumber[int32]` |
| S3URL | `*comparator.NullableString` |
| Succeeded | `*comparator.NullableBool` |
| TCPPort | `*comparator.NullableNumber[int32]` |
| UserID | `*comparator.NullableID` |
| Utf8Body | `*comparator.NullableString` |
| And | `[]*EventFilter` |
| Or | `[]*EventFilter` |

## Sort

Type `EventSort`.

Fields: ASCIIArt, Action, Address2ID, Adjustment, CSVData, FKVal, HTTP2Flag, ID, IOCount, Line2ID, MAC, OSName, OccurredAt, PKVal, ProcessedAt, RetryCount, S3URL, Succeeded, TCPPort, UserID, Utf8Body
