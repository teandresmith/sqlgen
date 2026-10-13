# SequenceRow

- **Table:** `sequence_rows` (schema `public`)
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
| `code` | Code | `*string` | `text` | yes |  | yes |  | `comparator.NullableString` |  |
| `seq` | Seq | `int32` | `integer` |  |  |  |  | `comparator.Number[int32]` |  |
| `ticket` | Ticket | `int64` | `bigint` |  |  |  |  | `comparator.Number[int64]` |  |
| `audit_no` | AuditNo | `int32` | `integer` |  |  |  |  | `comparator.Number[int32]` |  |
| `note` | Note | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `sequence_rows_code_key` | code | yes | btree |  |
| `sequence_rows_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*SequenceRow`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "audit_no", "code", "id", "note", "seq", "ticket" FROM "public"."sequence_rows" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetSequenceRowsInput`
- Returns: `[]*SequenceRow`, `error`

Generated SQL:

postgres:

```sql
SELECT "audit_no", "code", "id", "note", "seq", "ticket" FROM "public"."sequence_rows" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *SequenceRowFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."sequence_rows" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."sequence_rows" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *SequenceRowFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."sequence_rows" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[SequenceRowFilter]`
- Returns: `*PaginateResult[SequenceRow]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[SequenceRowFilter]`
- Returns: `*Connection[SequenceRow]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamSequenceRowsInput`
- Returns: `iter.Seq2[*SequenceRow, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "audit_no", "code", "id", "note", "seq", "ticket" FROM "public"."sequence_rows" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateSequenceRowInput`
- Returns: `*SequenceRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."sequence_rows" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateSequenceRowInput`
- Returns: `[]*SequenceRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."sequence_rows" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateSequenceRowInput`, `target SequenceRowConflictTarget`
- Returns: `*SequenceRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated SequenceRowConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."sequence_rows" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateSequenceRowInput`, `target SequenceRowConflictTarget`
- Returns: `[]*SequenceRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same SequenceRowConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."sequence_rows" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateSequenceRowInput`
- Returns: `*SequenceRow`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."sequence_rows" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateSequenceRowItem`
- Returns: `[]*SequenceRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."sequence_rows" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *SequenceRowFilter`, `input *UpdateSequenceRowInput`
- Returns: `[]*SequenceRow`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."sequence_rows" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id int64`, `input IncrementInput[SequenceRowIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated SequenceRowIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."sequence_rows" SET <column> = <column> + $1 WHERE "id" = $2
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."sequence_rows" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."sequence_rows" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *SequenceRowFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."sequence_rows" WHERE <filter> RETURNING "id"
```

## Filter

Type `SequenceRowFilter`.

| Field | Type |
| --- | --- |
| AuditNo | `*comparator.Number[int32]` |
| Code | `*comparator.NullableString` |
| ID | `*comparator.Number[int64]` |
| Note | `*comparator.String` |
| Seq | `*comparator.Number[int32]` |
| Ticket | `*comparator.Number[int64]` |
| And | `[]*SequenceRowFilter` |
| Or | `[]*SequenceRowFilter` |

## Sort

Type `SequenceRowSort`.

Fields: AuditNo, Code, ID, Note, Seq, Ticket
