# UserEvent

- **Table:** `user_events`
- **Kind:** table

User activity events; the one nullable-FK child table in this schema

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` | Unique identifier |
| `user_id` | UserID | `*int64` | `bigint` | yes |  |  |  | `comparator.NullableNumber[int64]` | Owning user reference; NULL while the event is unparented |
| `action` | Action | `string` | `varchar(255)` |  |  |  |  | `comparator.String` | What happened |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `user_events_pkey` | id | yes | btree |  |
| `user_id` | user_id |  | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*UserEvent`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

mysql:

```sql
SELECT `action`, `id`, `user_id` FROM `user_events` WHERE `id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetUserEventsInput`
- Returns: `[]*UserEvent`, `error`

Generated SQL:

mysql:

```sql
SELECT `action`, `id`, `user_id` FROM `user_events` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *UserEventFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `user_events` WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `user_events` WHERE `id` = ?)
```

### ExistsWhere

- Params: `filter *UserEventFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `user_events` WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[UserEventFilter]`
- Returns: `*PaginateResult[UserEvent]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[UserEventFilter]`
- Returns: `*Connection[UserEvent]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamUserEventsInput`
- Returns: `iter.Seq2[*UserEvent, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

mysql:

```sql
SELECT `action`, `id`, `user_id` FROM `user_events` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateUserEventInput`
- Returns: `*UserEvent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `user_events` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateUserEventInput`
- Returns: `[]*UserEvent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `user_events` (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateUserEventInput`, `target UserEventConflictTarget`
- Returns: `*UserEvent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated UserEventConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `user_events` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateUserEventInput`, `target UserEventConflictTarget`
- Returns: `[]*UserEvent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same UserEventConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `user_events` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### Update

- Params: `id int64`, `input *UpdateUserEventInput`
- Returns: `*UserEvent`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `user_events` SET <set> WHERE `id` = ?
```

### UpdateMany

- Params: `items []UpdateUserEventItem`
- Returns: `[]*UserEvent`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `user_events` SET <set> WHERE `id` = ?
```

### UpdateWhere

- Params: `filter *UserEventFilter`, `input *UpdateUserEventInput`
- Returns: `[]*UserEvent`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `user_events` SET <set> WHERE <filter>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `user_events` WHERE `id` = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `user_events` WHERE `id` IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *UserEventFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `user_events` WHERE <filter>
```

## Filter

Type `UserEventFilter`.

| Field | Type |
| --- | --- |
| Action | `*comparator.String` |
| ID | `*comparator.Number[int64]` |
| UserID | `*comparator.NullableNumber[int64]` |
| And | `[]*UserEventFilter` |
| Or | `[]*UserEventFilter` |

## Sort

Type `UserEventSort`.

Fields: Action, ID, UserID
