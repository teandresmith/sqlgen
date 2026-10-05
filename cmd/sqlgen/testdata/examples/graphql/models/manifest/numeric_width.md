# NumericWidth

- **Table:** `numeric_widths` (schema `public`)
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
| `small` | Small | `int16` | `smallint` |  |  |  |  | `comparator.Number[int16]` |  |
| `small_n` | SmallN | `*int16` | `smallint` | yes |  |  |  | `comparator.NullableNumber[int16]` |  |
| `ratio` | Ratio | `float32` | `real` |  |  |  |  | `comparator.Number[float32]` |  |
| `ratio_n` | RatioN | `*float32` | `real` | yes |  |  |  | `comparator.NullableNumber[float32]` |  |
| `smalls` | Smalls | `[]int16` | `smallint[]` |  |  |  | `'{}'` | `comparator.Slice[int16]` |  |
| `scores` | Scores | `[]int32` | `integer[]` |  |  |  | `'{}'` | `comparator.Slice[int32]` |  |
| `bigs` | Bigs | `[]int64` | `bigint[]` |  |  |  | `'{}'` | `comparator.Slice[int64]` |  |
| `ratios` | Ratios | `[]float32` | `real[]` |  |  |  | `'{}'` | `comparator.Slice[float32]` |  |
| `scores_n` | ScoresN | `[]int32` | `integer[]` | yes |  |  |  | `comparator.NullableSlice[int32]` |  |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `numeric_widths_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*NumericWidth`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "bigs", "created_at", "id", "ratio", "ratio_n", "ratios", "scores", "scores_n", "small", "small_n", "smalls" FROM "public"."numeric_widths" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetNumericWidthsInput`
- Returns: `[]*NumericWidth`, `error`

Generated SQL:

postgres:

```sql
SELECT "bigs", "created_at", "id", "ratio", "ratio_n", "ratios", "scores", "scores_n", "small", "small_n", "smalls" FROM "public"."numeric_widths" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *NumericWidthFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."numeric_widths" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."numeric_widths" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *NumericWidthFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."numeric_widths" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[NumericWidthFilter]`
- Returns: `*PaginateResult[NumericWidth]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[NumericWidthFilter]`
- Returns: `*Connection[NumericWidth]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamNumericWidthsInput`
- Returns: `iter.Seq2[*NumericWidth, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "bigs", "created_at", "id", "ratio", "ratio_n", "ratios", "scores", "scores_n", "small", "small_n", "smalls" FROM "public"."numeric_widths" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateNumericWidthInput`
- Returns: `*NumericWidth`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."numeric_widths" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateNumericWidthInput`
- Returns: `[]*NumericWidth`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."numeric_widths" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateNumericWidthInput`, `target NumericWidthConflictTarget`
- Returns: `*NumericWidth`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated NumericWidthConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."numeric_widths" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateNumericWidthInput`, `target NumericWidthConflictTarget`
- Returns: `[]*NumericWidth`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same NumericWidthConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."numeric_widths" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateNumericWidthInput`
- Returns: `*NumericWidth`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."numeric_widths" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateNumericWidthItem`
- Returns: `[]*NumericWidth`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."numeric_widths" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *NumericWidthFilter`, `input *UpdateNumericWidthInput`
- Returns: `[]*NumericWidth`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."numeric_widths" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id uuid.UUID`, `input IncrementInput[NumericWidthIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated NumericWidthIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."numeric_widths" SET <column> = <column> + $1 WHERE "id" = $2
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."numeric_widths" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."numeric_widths" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *NumericWidthFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."numeric_widths" WHERE <filter> RETURNING "id"
```

## Filter

Type `NumericWidthFilter`.

| Field | Type |
| --- | --- |
| Bigs | `*comparator.Slice[int64]` |
| CreatedAt | `*comparator.Time` |
| ID | `*comparator.ID` |
| Ratio | `*comparator.Number[float32]` |
| RatioN | `*comparator.NullableNumber[float32]` |
| Ratios | `*comparator.Slice[float32]` |
| Scores | `*comparator.Slice[int32]` |
| ScoresN | `*comparator.NullableSlice[int32]` |
| Small | `*comparator.Number[int16]` |
| SmallN | `*comparator.NullableNumber[int16]` |
| Smalls | `*comparator.Slice[int16]` |
| And | `[]*NumericWidthFilter` |
| Or | `[]*NumericWidthFilter` |

## Sort

Type `NumericWidthSort`.

Fields: Bigs, CreatedAt, ID, Ratio, RatioN, Ratios, Scores, ScoresN, Small, SmallN, Smalls
