# SluggedRow

- **Table:** `slugged_rows`
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
| `name` | Name | `string` | `varchar(64)` |  |  |  | `''` | `comparator.String` |  |
| `slug` | Slug | `string` | `varchar(64)` |  |  | yes | `'x'` | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `slugged_rows_pkey` | id | yes | btree |  |
| `slugged_rows_slug_key` | slug | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*SluggedRow`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

mysql:

```sql
SELECT `id`, `name`, `slug` FROM `slugged_rows` WHERE `id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetSluggedRowsInput`
- Returns: `[]*SluggedRow`, `error`

Generated SQL:

mysql:

```sql
SELECT `id`, `name`, `slug` FROM `slugged_rows` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *SluggedRowFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `slugged_rows` WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `slugged_rows` WHERE `id` = ?)
```

### ExistsWhere

- Params: `filter *SluggedRowFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `slugged_rows` WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[SluggedRowFilter]`
- Returns: `*PaginateResult[SluggedRow]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[SluggedRowFilter]`
- Returns: `*Connection[SluggedRow]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamSluggedRowsInput`
- Returns: `iter.Seq2[*SluggedRow, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

mysql:

```sql
SELECT `id`, `name`, `slug` FROM `slugged_rows` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateSluggedRowInput`
- Returns: `*SluggedRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `slugged_rows` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateSluggedRowInput`
- Returns: `[]*SluggedRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `slugged_rows` (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateSluggedRowInput`, `target SluggedRowConflictTarget`
- Returns: `*SluggedRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated SluggedRowConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `slugged_rows` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateSluggedRowInput`, `target SluggedRowConflictTarget`
- Returns: `[]*SluggedRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same SluggedRowConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `slugged_rows` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### Update

- Params: `id int64`, `input *UpdateSluggedRowInput`
- Returns: `*SluggedRow`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `slugged_rows` SET <set> WHERE `id` = ?
```

### UpdateMany

- Params: `items []UpdateSluggedRowItem`
- Returns: `[]*SluggedRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `slugged_rows` SET <set> WHERE `id` = ?
```

### UpdateWhere

- Params: `filter *SluggedRowFilter`, `input *UpdateSluggedRowInput`
- Returns: `[]*SluggedRow`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `slugged_rows` SET <set> WHERE <filter>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `slugged_rows` WHERE `id` = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `slugged_rows` WHERE `id` IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *SluggedRowFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `slugged_rows` WHERE <filter>
```

## Filter

Type `SluggedRowFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Name | `*comparator.String` |
| Slug | `*comparator.String` |
| And | `[]*SluggedRowFilter` |
| Or | `[]*SluggedRowFilter` |

## Sort

Type `SluggedRowSort`.

Fields: ID, Name, Slug
