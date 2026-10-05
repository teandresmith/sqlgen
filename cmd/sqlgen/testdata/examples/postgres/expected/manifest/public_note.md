# PublicNote

- **Table:** `notes` (schema `public`)
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
| `user_id` | UserID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `source_event_id` | SourceEventID | `*uuid.UUID` | `uuid` | yes |  | yes |  | `comparator.NullableID` |  |
| `body` | Body | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `notes_pkey` | id | yes | btree |  |
| `notes_source_event_id_key` | source_event_id | yes | btree |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*PublicNote`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "body", "id", "source_event_id", "user_id" FROM "public"."notes" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetPublicNotesInput`
- Returns: `[]*PublicNote`, `error`

Generated SQL:

postgres:

```sql
SELECT "body", "id", "source_event_id", "user_id" FROM "public"."notes" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *PublicNoteFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."notes" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."notes" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *PublicNoteFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."notes" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[PublicNoteFilter]`
- Returns: `*PaginateResult[PublicNote]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[PublicNoteFilter]`
- Returns: `*Connection[PublicNote]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamPublicNotesInput`
- Returns: `iter.Seq2[*PublicNote, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "body", "id", "source_event_id", "user_id" FROM "public"."notes" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreatePublicNoteInput`
- Returns: `*PublicNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."notes" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreatePublicNoteInput`
- Returns: `[]*PublicNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."notes" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreatePublicNoteInput`, `target PublicNoteConflictTarget`
- Returns: `*PublicNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated PublicNoteConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."notes" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreatePublicNoteInput`, `target PublicNoteConflictTarget`
- Returns: `[]*PublicNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same PublicNoteConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."notes" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdatePublicNoteInput`
- Returns: `*PublicNote`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."notes" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdatePublicNoteItem`
- Returns: `[]*PublicNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."notes" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *PublicNoteFilter`, `input *UpdatePublicNoteInput`
- Returns: `[]*PublicNote`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."notes" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."notes" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."notes" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *PublicNoteFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."notes" WHERE <filter> RETURNING "id"
```

## Filter

Type `PublicNoteFilter`.

| Field | Type |
| --- | --- |
| Body | `*comparator.String` |
| ID | `*comparator.ID` |
| SourceEventID | `*comparator.NullableID` |
| UserID | `*comparator.ID` |
| And | `[]*PublicNoteFilter` |
| Or | `[]*PublicNoteFilter` |

## Sort

Type `PublicNoteSort`.

Fields: Body, ID, SourceEventID, UserID
