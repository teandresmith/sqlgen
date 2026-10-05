# BinaryKeyEvent

- **Table:** `binary_key_events`
- **Kind:** table

## Files

- `binary_key_event_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `binary_key_id` | BinaryKeyID | `[]byte` | `blob` |  |  |  |  | `comparator.Opaque[[]byte]` |  |
| `label` | Label | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `binary_key_events_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*BinaryKeyEvent`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "binary_key_id", "id", "label" FROM "binary_key_events" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetBinaryKeyEventsInput`
- Returns: `[]*BinaryKeyEvent`, `error`

Generated SQL:

sqlite:

```sql
SELECT "binary_key_id", "id", "label" FROM "binary_key_events" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *BinaryKeyEventFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "binary_key_events" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "binary_key_events" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *BinaryKeyEventFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "binary_key_events" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[BinaryKeyEventFilter]`
- Returns: `*PaginateResult[BinaryKeyEvent]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[BinaryKeyEventFilter]`
- Returns: `*Connection[BinaryKeyEvent]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamBinaryKeyEventsInput`
- Returns: `iter.Seq2[*BinaryKeyEvent, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "binary_key_id", "id", "label" FROM "binary_key_events" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateBinaryKeyEventInput`
- Returns: `*BinaryKeyEvent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "binary_key_events" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateBinaryKeyEventInput`
- Returns: `[]*BinaryKeyEvent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "binary_key_events" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateBinaryKeyEventInput`, `target BinaryKeyEventConflictTarget`
- Returns: `*BinaryKeyEvent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated BinaryKeyEventConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "binary_key_events" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateBinaryKeyEventInput`, `target BinaryKeyEventConflictTarget`
- Returns: `[]*BinaryKeyEvent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same BinaryKeyEventConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "binary_key_events" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateBinaryKeyEventInput`
- Returns: `*BinaryKeyEvent`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "binary_key_events" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateBinaryKeyEventItem`
- Returns: `[]*BinaryKeyEvent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "binary_key_events" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *BinaryKeyEventFilter`, `input *UpdateBinaryKeyEventInput`
- Returns: `[]*BinaryKeyEvent`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "binary_key_events" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "binary_key_events" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "binary_key_events" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *BinaryKeyEventFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "binary_key_events" WHERE <filter> RETURNING "id"
```

## Filter

Type `BinaryKeyEventFilter`.

| Field | Type |
| --- | --- |
| BinaryKeyID | `*comparator.Opaque[[]byte]` |
| ID | `*comparator.Number[int64]` |
| Label | `*comparator.String` |
| And | `[]*BinaryKeyEventFilter` |
| Or | `[]*BinaryKeyEventFilter` |

## Sort

Type `BinaryKeyEventSort`.

Fields: BinaryKeyID, ID, Label
