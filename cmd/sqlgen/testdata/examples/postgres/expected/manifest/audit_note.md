# AuditNote

- **Table:** `notes` (schema `audit`)
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
| `user_id` | UserID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `event_id` | EventID | `*uuid.UUID` | `uuid` | yes |  | yes |  | `comparator.NullableID` |  |
| `severity` | Severity | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `notes_event_id_key` | event_id | yes | btree |  |
| `notes_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| Author | o2o | PublicUser | audit.notes.user_id |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*AuditNote`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "event_id", "id", "severity", "user_id" FROM "audit"."notes" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetAuditNotesInput`
- Returns: `[]*AuditNote`, `error`

Generated SQL:

postgres:

```sql
SELECT "event_id", "id", "severity", "user_id" FROM "audit"."notes" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *AuditNoteFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "audit"."notes" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "audit"."notes" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *AuditNoteFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "audit"."notes" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[AuditNoteFilter]`
- Returns: `*PaginateResult[AuditNote]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[AuditNoteFilter]`
- Returns: `*Connection[AuditNote]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamAuditNotesInput`
- Returns: `iter.Seq2[*AuditNote, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "event_id", "id", "severity", "user_id" FROM "audit"."notes" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateAuditNoteInput`
- Returns: `*AuditNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."notes" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateAuditNoteInput`
- Returns: `[]*AuditNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."notes" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateAuditNoteInput`, `target AuditNoteConflictTarget`
- Returns: `*AuditNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated AuditNoteConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."notes" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateAuditNoteInput`, `target AuditNoteConflictTarget`
- Returns: `[]*AuditNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same AuditNoteConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."notes" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateAuditNoteInput`
- Returns: `*AuditNote`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "audit"."notes" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateAuditNoteItem`
- Returns: `[]*AuditNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "audit"."notes" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *AuditNoteFilter`, `input *UpdateAuditNoteInput`
- Returns: `[]*AuditNote`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "audit"."notes" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "audit"."notes" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "audit"."notes" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *AuditNoteFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "audit"."notes" WHERE <filter> RETURNING "id"
```

## Filter

Type `AuditNoteFilter`.

| Field | Type |
| --- | --- |
| EventID | `*comparator.NullableID` |
| ID | `*comparator.Number[int64]` |
| Severity | `*comparator.String` |
| UserID | `*comparator.ID` |
| And | `[]*AuditNoteFilter` |
| Or | `[]*AuditNoteFilter` |

## Sort

Type `AuditNoteSort`.

Fields: EventID, ID, Severity, UserID
