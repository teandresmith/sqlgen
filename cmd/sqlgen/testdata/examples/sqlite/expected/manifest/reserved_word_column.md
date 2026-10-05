# ReservedWordColumn

- **Table:** `reserved_word_columns`
- **Kind:** table

## Files

- `reserved_word_column_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `type` | Type | `string` | `text` |  |  |  | `'kind'` | `comparator.String` |  |
| `interface` | Interface | `string` | `text` |  |  |  | `'iface'` | `comparator.String` |  |
| `func` | Func | `string` | `text` |  |  |  | `'callable'` | `comparator.String` |  |
| `map` | Map | `string` | `text` |  |  |  | `'lookup'` | `comparator.String` |  |
| `new` | New | `string` | `text` |  |  |  | `'init'` | `comparator.String` |  |
| `make` | Make | `string` | `text` |  |  |  | `'build'` | `comparator.String` |  |
| `len` | Len | `int64` | `integer` |  |  |  | `0` | `comparator.Number[int64]` |  |
| `error` | Error | `string` | `text` |  |  |  | `''` | `comparator.String` |  |
| `ctx` | Ctx | `string` | `text` |  |  |  | `''` | `comparator.String` |  |
| `err` | Err | `string` | `text` |  |  |  | `''` | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `reserved_word_columns_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*ReservedWordColumn`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "ctx", "err", "error", "func", "id", "interface", "len", "make", "map", "new", "type" FROM "reserved_word_columns" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetReservedWordColumnsInput`
- Returns: `[]*ReservedWordColumn`, `error`

Generated SQL:

sqlite:

```sql
SELECT "ctx", "err", "error", "func", "id", "interface", "len", "make", "map", "new", "type" FROM "reserved_word_columns" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ReservedWordColumnFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "reserved_word_columns" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "reserved_word_columns" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *ReservedWordColumnFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "reserved_word_columns" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[ReservedWordColumnFilter]`
- Returns: `*PaginateResult[ReservedWordColumn]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ReservedWordColumnFilter]`
- Returns: `*Connection[ReservedWordColumn]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamReservedWordColumnsInput`
- Returns: `iter.Seq2[*ReservedWordColumn, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "ctx", "err", "error", "func", "id", "interface", "len", "make", "map", "new", "type" FROM "reserved_word_columns" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateReservedWordColumnInput`
- Returns: `*ReservedWordColumn`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "reserved_word_columns" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateReservedWordColumnInput`
- Returns: `[]*ReservedWordColumn`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "reserved_word_columns" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateReservedWordColumnInput`, `target ReservedWordColumnConflictTarget`
- Returns: `*ReservedWordColumn`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated ReservedWordColumnConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "reserved_word_columns" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateReservedWordColumnInput`, `target ReservedWordColumnConflictTarget`
- Returns: `[]*ReservedWordColumn`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same ReservedWordColumnConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "reserved_word_columns" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateReservedWordColumnInput`
- Returns: `*ReservedWordColumn`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "reserved_word_columns" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateReservedWordColumnItem`
- Returns: `[]*ReservedWordColumn`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "reserved_word_columns" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *ReservedWordColumnFilter`, `input *UpdateReservedWordColumnInput`
- Returns: `[]*ReservedWordColumn`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "reserved_word_columns" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id int64`, `input IncrementInput[ReservedWordColumnIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated ReservedWordColumnIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

sqlite:

```sql
UPDATE "reserved_word_columns" SET <column> = <column> + ? WHERE "id" = ?
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "reserved_word_columns" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "reserved_word_columns" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *ReservedWordColumnFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "reserved_word_columns" WHERE <filter> RETURNING "id"
```

## Filter

Type `ReservedWordColumnFilter`.

| Field | Type |
| --- | --- |
| Ctx | `*comparator.String` |
| Err | `*comparator.String` |
| Error | `*comparator.String` |
| Func | `*comparator.String` |
| ID | `*comparator.Number[int64]` |
| Interface | `*comparator.String` |
| Len | `*comparator.Number[int64]` |
| Make | `*comparator.String` |
| Map | `*comparator.String` |
| New | `*comparator.String` |
| Type | `*comparator.String` |
| And | `[]*ReservedWordColumnFilter` |
| Or | `[]*ReservedWordColumnFilter` |

## Sort

Type `ReservedWordColumnSort`.

Fields: Ctx, Err, Error, Func, ID, Interface, Len, Make, Map, New, Type
