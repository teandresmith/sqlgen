# NoteWatcher

- **Table:** `note_watchers` (schema `audit`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: composite
- Struct: `NoteWatcherPK`
- `user_id` — UserID `uuid.UUID`
- `note_id` — NoteID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `user_id` | UserID | `uuid.UUID` | `uuid` |  | yes |  |  | `comparator.ID` |  |
| `note_id` | NoteID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `note_watchers_pkey` | user_id, note_id | yes | btree |  |

## Query methods

### Get

- Params: `pk NoteWatcherPK`
- Returns: `*NoteWatcher`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "note_id", "user_id" FROM "audit"."note_watchers" WHERE "user_id" = $1 AND "note_id" = $2 LIMIT 1
```

### GetMany

- Params: `input *GetNoteWatchersInput`
- Returns: `[]*NoteWatcher`, `error`

Generated SQL:

postgres:

```sql
SELECT "note_id", "user_id" FROM "audit"."note_watchers" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *NoteWatcherFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "audit"."note_watchers" WHERE <filter>
```

### Exists

- Params: `pk NoteWatcherPK`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "audit"."note_watchers" WHERE "user_id" = $1 AND "note_id" = $2)
```

### ExistsWhere

- Params: `filter *NoteWatcherFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "audit"."note_watchers" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[NoteWatcherFilter]`
- Returns: `*PaginateResult[NoteWatcher]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[NoteWatcherFilter]`
- Returns: `*Connection[NoteWatcher]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamNoteWatchersInput`
- Returns: `iter.Seq2[*NoteWatcher, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "note_id", "user_id" FROM "audit"."note_watchers" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateNoteWatcherInput`
- Returns: `*NoteWatcher`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."note_watchers" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateNoteWatcherInput`
- Returns: `[]*NoteWatcher`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."note_watchers" (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateNoteWatcherInput`, `target NoteWatcherConflictTarget`
- Returns: `*NoteWatcher`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated NoteWatcherConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."note_watchers" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateNoteWatcherInput`, `target NoteWatcherConflictTarget`
- Returns: `[]*NoteWatcher`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same NoteWatcherConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."note_watchers" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `pk NoteWatcherPK`, `input *UpdateNoteWatcherInput`
- Returns: `*NoteWatcher`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "audit"."note_watchers" SET <set> WHERE "user_id" = $1 AND "note_id" = $2
```

### UpdateMany

- Params: `items []UpdateNoteWatcherItem`
- Returns: `[]*NoteWatcher`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "audit"."note_watchers" SET <set> WHERE "user_id" = $1 AND "note_id" = $2
```

### UpdateWhere

- Params: `filter *NoteWatcherFilter`, `input *UpdateNoteWatcherInput`
- Returns: `[]*NoteWatcher`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "audit"."note_watchers" SET <set> WHERE <filter> RETURNING "user_id", "note_id"
```

### HardDelete

- Params: `pk NoteWatcherPK`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "audit"."note_watchers" WHERE "user_id" = $1 AND "note_id" = $2
```

### HardDeleteMany

- Params: `pks []NoteWatcherPK`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "audit"."note_watchers" WHERE ("user_id", "note_id") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *NoteWatcherFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "audit"."note_watchers" WHERE <filter> RETURNING "user_id", "note_id"
```

## Filter

Type `NoteWatcherFilter`.

| Field | Type |
| --- | --- |
| NoteID | `*comparator.Number[int64]` |
| UserID | `*comparator.ID` |
| And | `[]*NoteWatcherFilter` |
| Or | `[]*NoteWatcherFilter` |

## Sort

Type `NoteWatcherSort`.

Fields: NoteID, UserID
