# KeyedCodeEntry

- **Table:** `keyed_code_entries` (schema `public`)
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
| `keyed_code_id` | KeyedCodeID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `note` | Note | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `keyed_code_entries_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*KeyedCodeEntry`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "keyed_code_id", "note" FROM "public"."keyed_code_entries" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetKeyedCodeEntriesInput`
- Returns: `[]*KeyedCodeEntry`, `error`

Generated SQL:

postgres:

```sql
SELECT "id", "keyed_code_id", "note" FROM "public"."keyed_code_entries" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *KeyedCodeEntryFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."keyed_code_entries" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."keyed_code_entries" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *KeyedCodeEntryFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."keyed_code_entries" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[KeyedCodeEntryFilter]`
- Returns: `*PaginateResult[KeyedCodeEntry]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[KeyedCodeEntryFilter]`
- Returns: `*Connection[KeyedCodeEntry]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamKeyedCodeEntriesInput`
- Returns: `iter.Seq2[*KeyedCodeEntry, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "keyed_code_id", "note" FROM "public"."keyed_code_entries" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateKeyedCodeEntryInput`
- Returns: `*KeyedCodeEntry`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."keyed_code_entries" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateKeyedCodeEntryInput`
- Returns: `[]*KeyedCodeEntry`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."keyed_code_entries" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateKeyedCodeEntryInput`, `target KeyedCodeEntryConflictTarget`
- Returns: `*KeyedCodeEntry`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated KeyedCodeEntryConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."keyed_code_entries" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateKeyedCodeEntryInput`, `target KeyedCodeEntryConflictTarget`
- Returns: `[]*KeyedCodeEntry`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same KeyedCodeEntryConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."keyed_code_entries" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateKeyedCodeEntryInput`
- Returns: `*KeyedCodeEntry`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."keyed_code_entries" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateKeyedCodeEntryItem`
- Returns: `[]*KeyedCodeEntry`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."keyed_code_entries" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *KeyedCodeEntryFilter`, `input *UpdateKeyedCodeEntryInput`
- Returns: `[]*KeyedCodeEntry`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."keyed_code_entries" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."keyed_code_entries" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."keyed_code_entries" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *KeyedCodeEntryFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."keyed_code_entries" WHERE <filter> RETURNING "id"
```

## Filter

Type `KeyedCodeEntryFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.ID` |
| KeyedCodeID | `*comparator.ID` |
| Note | `*comparator.String` |
| And | `[]*KeyedCodeEntryFilter` |
| Or | `[]*KeyedCodeEntryFilter` |

## Sort

Type `KeyedCodeEntrySort`.

Fields: ID, KeyedCodeID, Note
