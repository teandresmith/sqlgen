# Event

- **Table:** `events` (schema `audit`)
- **Kind:** table

General system audit events

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  | `gen_random_uuid()` | `comparator.ID` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `payload` | Payload | `types.JSON` | `jsonb` | yes |  |  |  | `comparator.NullableJSONB` |  |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `events_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| Note | o2o | AuditNote | audit.notes.event_id |  |
| SourceNote | o2o | PublicNote | public.notes.source_event_id |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*Event`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "created_at", "id", "name", "payload" FROM "audit"."events" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetEventsInput`
- Returns: `[]*Event`, `error`

Generated SQL:

postgres:

```sql
SELECT "created_at", "id", "name", "payload" FROM "audit"."events" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *EventFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "audit"."events" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "audit"."events" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *EventFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "audit"."events" WHERE <filter>)
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
SELECT "created_at", "id", "name", "payload" FROM "audit"."events" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateEventInput`
- Returns: `*Event`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."events" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateEventInput`
- Returns: `[]*Event`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."events" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateEventInput`, `target EventConflictTarget`
- Returns: `*Event`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated EventConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."events" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateEventInput`, `target EventConflictTarget`
- Returns: `[]*Event`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same EventConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."events" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateEventInput`
- Returns: `*Event`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "audit"."events" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateEventItem`
- Returns: `[]*Event`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "audit"."events" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *EventFilter`, `input *UpdateEventInput`
- Returns: `[]*Event`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "audit"."events" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "audit"."events" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "audit"."events" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *EventFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "audit"."events" WHERE <filter> RETURNING "id"
```

## Filter

Type `EventFilter`.

| Field | Type |
| --- | --- |
| CreatedAt | `*comparator.Time` |
| ID | `*comparator.ID` |
| Name | `*comparator.String` |
| Payload | `*comparator.NullableJSONB` |
| And | `[]*EventFilter` |
| Or | `[]*EventFilter` |

## Sort

Type `EventSort`.

Fields: CreatedAt, ID, Name, Payload
