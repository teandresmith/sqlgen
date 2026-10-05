# DefaultOnlyRow

- **Table:** `default_only_rows`
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
| `label` | Label | `string` | `varchar(32)` |  |  |  | `'unset'` | `comparator.String` |  |
| `hits` | Hits | `int32` | `int` |  |  |  | `0` | `comparator.Number[int32]` |  |
| `active` | Active | `bool` | `boolean` |  |  |  | `true` | `comparator.Bool` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `default_only_rows_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*DefaultOnlyRow`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

mysql:

```sql
SELECT `active`, `hits`, `id`, `label` FROM `default_only_rows` WHERE `id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetDefaultOnlyRowsInput`
- Returns: `[]*DefaultOnlyRow`, `error`

Generated SQL:

mysql:

```sql
SELECT `active`, `hits`, `id`, `label` FROM `default_only_rows` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *DefaultOnlyRowFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `default_only_rows` WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `default_only_rows` WHERE `id` = ?)
```

### ExistsWhere

- Params: `filter *DefaultOnlyRowFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `default_only_rows` WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[DefaultOnlyRowFilter]`
- Returns: `*PaginateResult[DefaultOnlyRow]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[DefaultOnlyRowFilter]`
- Returns: `*Connection[DefaultOnlyRow]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamDefaultOnlyRowsInput`
- Returns: `iter.Seq2[*DefaultOnlyRow, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

mysql:

```sql
SELECT `active`, `hits`, `id`, `label` FROM `default_only_rows` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateDefaultOnlyRowInput`
- Returns: `*DefaultOnlyRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `default_only_rows` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateDefaultOnlyRowInput`
- Returns: `[]*DefaultOnlyRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `default_only_rows` (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateDefaultOnlyRowInput`, `target DefaultOnlyRowConflictTarget`
- Returns: `*DefaultOnlyRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated DefaultOnlyRowConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `default_only_rows` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateDefaultOnlyRowInput`, `target DefaultOnlyRowConflictTarget`
- Returns: `[]*DefaultOnlyRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same DefaultOnlyRowConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `default_only_rows` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### Update

- Params: `id int64`, `input *UpdateDefaultOnlyRowInput`
- Returns: `*DefaultOnlyRow`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `default_only_rows` SET <set> WHERE `id` = ?
```

### UpdateMany

- Params: `items []UpdateDefaultOnlyRowItem`
- Returns: `[]*DefaultOnlyRow`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `default_only_rows` SET <set> WHERE `id` = ?
```

### UpdateWhere

- Params: `filter *DefaultOnlyRowFilter`, `input *UpdateDefaultOnlyRowInput`
- Returns: `[]*DefaultOnlyRow`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `default_only_rows` SET <set> WHERE <filter>
```

### Increment

- Params: `id int64`, `input IncrementInput[DefaultOnlyRowIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated DefaultOnlyRowIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

mysql:

```sql
UPDATE `default_only_rows` SET <column> = <column> + ? WHERE `id` = ?
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `default_only_rows` WHERE `id` = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `default_only_rows` WHERE `id` IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *DefaultOnlyRowFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `default_only_rows` WHERE <filter>
```

## Filter

Type `DefaultOnlyRowFilter`.

| Field | Type |
| --- | --- |
| Active | `*comparator.Bool` |
| Hits | `*comparator.Number[int32]` |
| ID | `*comparator.Number[int64]` |
| Label | `*comparator.String` |
| And | `[]*DefaultOnlyRowFilter` |
| Or | `[]*DefaultOnlyRowFilter` |

## Sort

Type `DefaultOnlyRowSort`.

Fields: Active, Hits, ID, Label
