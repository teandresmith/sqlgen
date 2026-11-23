# TimeKeyedNote

- **Table:** `time_keyed_notes` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `keyed_at` | KeyedAt | `*time.Time` | `timestamptz` | yes |  |  |  | `comparator.NullableTime` |  |
| `note` | Note | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `time_keyed_notes_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| time_keyed | o2o | TimeKeyed | public.time_keyed_notes.keyed_at |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*TimeKeyedNote`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "keyed_at", "note" FROM "public"."time_keyed_notes" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetTimeKeyedNotesInput`
- Returns: `[]*TimeKeyedNote`, `error`

Generated SQL:

postgres:

```sql
SELECT "id", "keyed_at", "note" FROM "public"."time_keyed_notes" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."time_keyed_notes" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *TimeKeyedNoteFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."time_keyed_notes" WHERE <filter>)
```

### Count

- Params: `filter *TimeKeyedNoteFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."time_keyed_notes" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[TimeKeyedNoteFilter]`
- Returns: `*PaginateResult[TimeKeyedNote]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[TimeKeyedNoteFilter]`
- Returns: `*Connection[TimeKeyedNote]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamTimeKeyedNotesInput`
- Returns: `iter.Seq2[*TimeKeyedNote, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "keyed_at", "note" FROM "public"."time_keyed_notes" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateTimeKeyedNoteInput`
- Returns: `*TimeKeyedNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."time_keyed_notes" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateTimeKeyedNoteInput`
- Returns: `[]*TimeKeyedNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."time_keyed_notes" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateTimeKeyedNoteInput`
- Returns: `*TimeKeyedNote`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."time_keyed_notes" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateTimeKeyedNoteItem`
- Returns: `[]*TimeKeyedNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."time_keyed_notes" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *TimeKeyedNoteFilter`, `input *UpdateTimeKeyedNoteInput`
- Returns: `[]*TimeKeyedNote`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."time_keyed_notes" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateTimeKeyedNoteInput`, `target TimeKeyedNoteConflictTarget`
- Returns: `*TimeKeyedNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated TimeKeyedNoteConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."time_keyed_notes" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateTimeKeyedNoteInput`, `target TimeKeyedNoteConflictTarget`
- Returns: `[]*TimeKeyedNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same TimeKeyedNoteConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."time_keyed_notes" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."time_keyed_notes" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."time_keyed_notes" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *TimeKeyedNoteFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."time_keyed_notes" WHERE <filter> RETURNING "id"
```

## Filter

Type `TimeKeyedNoteFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| KeyedAt | `*comparator.NullableTime` |
| Note | `*comparator.String` |
| And | `[]*TimeKeyedNoteFilter` |
| Or | `[]*TimeKeyedNoteFilter` |

## Sort

Type `TimeKeyedNoteSort`.

Fields: ID, KeyedAt, Note

## Read examples

```go
row, err := client.TimeKeyedNotes().Get(ctx, 1)
rows, err := client.TimeKeyedNotes().GetMany(ctx, &models.GetTimeKeyedNotesInput{Filter: &models.TimeKeyedNoteFilter{Note: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.TimeKeyedNotes().Create(ctx, &models.CreateTimeKeyedNoteInput{Note: "example"})
updated, err := client.TimeKeyedNotes().Update(ctx, 1, &models.UpdateTimeKeyedNoteInput{KeyedAt: omittable.Set(new(time.Now()))})
```
