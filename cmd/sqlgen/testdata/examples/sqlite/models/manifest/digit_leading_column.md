# DigitLeadingColumn

- **Table:** `digit_leading_columns`
- **Kind:** table

## Files

- `digit_leading_column_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `2010_revenue` | Col2010Revenue | `float64` | `real` |  |  |  | `0.0` | `comparator.Number[float64]` |  |
| `2024_quota` | Col2024Quota | `int64` | `integer` |  |  |  | `0` | `comparator.Number[int64]` |  |
| `1st_place` | Col1stPlace | `string` | `text` |  |  |  | `'unranked'` | `comparator.String` |  |
| `3d_model_url` | Col3dModelURL | `string` | `text` |  |  |  | `''` | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `digit_leading_columns_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*DigitLeadingColumn`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "1st_place", "2010_revenue", "2024_quota", "3d_model_url", "id" FROM "digit_leading_columns" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetDigitLeadingColumnsInput`
- Returns: `[]*DigitLeadingColumn`, `error`

Generated SQL:

sqlite:

```sql
SELECT "1st_place", "2010_revenue", "2024_quota", "3d_model_url", "id" FROM "digit_leading_columns" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *DigitLeadingColumnFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "digit_leading_columns" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "digit_leading_columns" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *DigitLeadingColumnFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "digit_leading_columns" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[DigitLeadingColumnFilter]`
- Returns: `*PaginateResult[DigitLeadingColumn]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[DigitLeadingColumnFilter]`
- Returns: `*Connection[DigitLeadingColumn]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamDigitLeadingColumnsInput`
- Returns: `iter.Seq2[*DigitLeadingColumn, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "1st_place", "2010_revenue", "2024_quota", "3d_model_url", "id" FROM "digit_leading_columns" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateDigitLeadingColumnInput`
- Returns: `*DigitLeadingColumn`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "digit_leading_columns" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateDigitLeadingColumnInput`
- Returns: `[]*DigitLeadingColumn`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "digit_leading_columns" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateDigitLeadingColumnInput`, `target DigitLeadingColumnConflictTarget`
- Returns: `*DigitLeadingColumn`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated DigitLeadingColumnConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "digit_leading_columns" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateDigitLeadingColumnInput`, `target DigitLeadingColumnConflictTarget`
- Returns: `[]*DigitLeadingColumn`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same DigitLeadingColumnConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "digit_leading_columns" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateDigitLeadingColumnInput`
- Returns: `*DigitLeadingColumn`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "digit_leading_columns" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateDigitLeadingColumnItem`
- Returns: `[]*DigitLeadingColumn`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "digit_leading_columns" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *DigitLeadingColumnFilter`, `input *UpdateDigitLeadingColumnInput`
- Returns: `[]*DigitLeadingColumn`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "digit_leading_columns" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id int64`, `input IncrementInput[DigitLeadingColumnIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated DigitLeadingColumnIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

sqlite:

```sql
UPDATE "digit_leading_columns" SET <column> = <column> + ? WHERE "id" = ?
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "digit_leading_columns" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "digit_leading_columns" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *DigitLeadingColumnFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "digit_leading_columns" WHERE <filter> RETURNING "id"
```

## Filter

Type `DigitLeadingColumnFilter`.

| Field | Type |
| --- | --- |
| Col1stPlace | `*comparator.String` |
| Col2010Revenue | `*comparator.Number[float64]` |
| Col2024Quota | `*comparator.Number[int64]` |
| Col3dModelURL | `*comparator.String` |
| ID | `*comparator.Number[int64]` |
| And | `[]*DigitLeadingColumnFilter` |
| Or | `[]*DigitLeadingColumnFilter` |

## Sort

Type `DigitLeadingColumnSort`.

Fields: Col1stPlace, Col2010Revenue, Col2024Quota, Col3dModelURL, ID
