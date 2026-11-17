# BinItem

- **Table:** `bin_items`
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
| `aisle` | Aisle | `*string` | `varchar(10)` | yes |  |  |  | `comparator.NullableString` |  |
| `slot` | Slot | `*int32` | `int` | yes |  |  |  | `comparator.NullableNumber[int32]` |  |
| `name` | Name | `string` | `varchar(50)` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `PRIMARY` | id | yes | btree |  |
| `aisle` | aisle, slot |  | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*BinItem`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

mysql:

```sql
SELECT `aisle`, `id`, `name`, `slot` FROM `bin_items` WHERE `id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetBinItemsInput`
- Returns: `[]*BinItem`, `error`

Generated SQL:

mysql:

```sql
SELECT `aisle`, `id`, `name`, `slot` FROM `bin_items` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `bin_items` WHERE `id` = ?)
```

### ExistsWhere

- Params: `filter *BinItemFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `bin_items` WHERE <filter>)
```

### Count

- Params: `filter *BinItemFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `bin_items` WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[BinItemFilter]`
- Returns: `*PaginateResult[BinItem]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[BinItemFilter]`
- Returns: `*Connection[BinItem]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamBinItemsInput`
- Returns: `iter.Seq2[*BinItem, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

mysql:

```sql
SELECT `aisle`, `id`, `name`, `slot` FROM `bin_items` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateBinItemInput`
- Returns: `*BinItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `bin_items` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateBinItemInput`
- Returns: `[]*BinItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `bin_items` (<columns>) VALUES <values>
```

### Update

- Params: `id int64`, `input *UpdateBinItemInput`
- Returns: `*BinItem`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `bin_items` SET <set> WHERE `id` = ?
```

### UpdateMany

- Params: `items []UpdateBinItemItem`
- Returns: `[]*BinItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `bin_items` SET <set> WHERE `id` = ?
```

### UpdateWhere

- Params: `filter *BinItemFilter`, `input *UpdateBinItemInput`
- Returns: `[]*BinItem`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `bin_items` SET <set> WHERE <filter>
```

### Upsert

- Params: `input *CreateBinItemInput`, `target BinItemConflictTarget`
- Returns: `*BinItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated BinItemConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `bin_items` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateBinItemInput`, `target BinItemConflictTarget`
- Returns: `[]*BinItem`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same BinItemConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `bin_items` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `bin_items` WHERE `id` = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `bin_items` WHERE `id` IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *BinItemFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `bin_items` WHERE <filter>
```

## Filter

Type `BinItemFilter`.

| Field | Type |
| --- | --- |
| Aisle | `*comparator.NullableString` |
| ID | `*comparator.Number[int64]` |
| Name | `*comparator.String` |
| Slot | `*comparator.NullableNumber[int32]` |
| And | `[]*BinItemFilter` |
| Or | `[]*BinItemFilter` |

## Sort

Type `BinItemSort`.

Fields: Aisle, ID, Name, Slot

## Read examples

```go
row, err := client.BinItems().Get(ctx, 1)
rows, err := client.BinItems().GetMany(ctx, &models.GetBinItemsInput{Filter: &models.BinItemFilter{Name: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.BinItems().Create(ctx, &models.CreateBinItemInput{Name: "example"})
updated, err := client.BinItems().Update(ctx, 1, &models.UpdateBinItemInput{Aisle: omittable.Set(new("example"))})
```
