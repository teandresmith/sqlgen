# Bin

- **Table:** `bins`
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: composite
- Struct: `BinPK`
- `aisle` — Aisle `string`
- `slot` — Slot `int32`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `aisle` | Aisle | `string` | `varchar(10)` |  | yes |  |  | `comparator.ID` |  |
| `slot` | Slot | `int32` | `int` |  | yes |  |  | `comparator.Number[int32]` |  |
| `label` | Label | `string` | `varchar(50)` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `PRIMARY` | aisle, slot | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| bin_items | o2m | BinItem | bin_items.(aisle, slot) |  |

## Query methods

### Get

- Params: `pk BinPK`
- Returns: `*Bin`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

mysql:

```sql
SELECT `aisle`, `label`, `slot` FROM `bins` WHERE `aisle` = ? AND `slot` = ? LIMIT 1
```

### GetMany

- Params: `input *GetBinsInput`
- Returns: `[]*Bin`, `error`

Generated SQL:

mysql:

```sql
SELECT `aisle`, `label`, `slot` FROM `bins` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `pk BinPK`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `bins` WHERE `aisle` = ? AND `slot` = ?)
```

### ExistsWhere

- Params: `filter *BinFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `bins` WHERE <filter>)
```

### Count

- Params: `filter *BinFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `bins` WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[BinFilter]`
- Returns: `*PaginateResult[Bin]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[BinFilter]`
- Returns: `*Connection[Bin]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamBinsInput`
- Returns: `iter.Seq2[*Bin, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

mysql:

```sql
SELECT `aisle`, `label`, `slot` FROM `bins` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateBinInput`
- Returns: `*Bin`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `bins` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateBinInput`
- Returns: `[]*Bin`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `bins` (<columns>) VALUES <values>
```

### Update

- Params: `pk BinPK`, `input *UpdateBinInput`
- Returns: `*Bin`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `bins` SET <set> WHERE `aisle` = ? AND `slot` = ?
```

### UpdateMany

- Params: `items []UpdateBinItem`
- Returns: `[]*Bin`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `bins` SET <set> WHERE `aisle` = ? AND `slot` = ?
```

### UpdateWhere

- Params: `filter *BinFilter`, `input *UpdateBinInput`
- Returns: `[]*Bin`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `bins` SET <set> WHERE <filter>
```

### Upsert

- Params: `input *CreateBinInput`, `target BinConflictTarget`
- Returns: `*Bin`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated BinConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `bins` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateBinInput`, `target BinConflictTarget`
- Returns: `[]*Bin`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same BinConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `bins` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### HardDelete

- Params: `pk BinPK`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `bins` WHERE `aisle` = ? AND `slot` = ?
```

### HardDeleteMany

- Params: `pks []BinPK`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `bins` WHERE (`aisle`, `slot`) IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *BinFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `bins` WHERE <filter>
```

## Filter

Type `BinFilter`.

| Field | Type |
| --- | --- |
| Aisle | `*comparator.ID` |
| Label | `*comparator.String` |
| Slot | `*comparator.Number[int32]` |
| And | `[]*BinFilter` |
| Or | `[]*BinFilter` |

## Sort

Type `BinSort`.

Fields: Aisle, Label, Slot

## Read examples

```go
row, err := client.Bins().Get(ctx, models.BinPK{Aisle: "example", Slot: 1})
rows, err := client.Bins().GetMany(ctx, &models.GetBinsInput{Filter: &models.BinFilter{Label: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.Bins().Create(ctx, &models.CreateBinInput{Aisle: "example", Slot: 1, Label: "example"})
updated, err := client.Bins().Update(ctx, models.BinPK{Aisle: "example", Slot: 1}, &models.UpdateBinInput{Label: omittable.Set("example")})
```
