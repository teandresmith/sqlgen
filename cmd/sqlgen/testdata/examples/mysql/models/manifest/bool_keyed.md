# BoolKeyed

- **Table:** `bool_keyed`
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `k` — K `bool`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `k` | K | `bool` | `tinyint(1)` |  | yes |  |  | `comparator.Bool` |  |
| `label` | Label | `string` | `varchar(50)` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `PRIMARY` | k | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| bool_keyed_notes | o2m | BoolKeyedNote | bool_keyed_notes.keyed_k |  |

## Query methods

### Get

- Params: `id bool`
- Returns: `*BoolKeyed`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

mysql:

```sql
SELECT `k`, `label` FROM `bool_keyed` WHERE `k` = ? LIMIT 1
```

### GetMany

- Params: `input *GetBoolKeyedsInput`
- Returns: `[]*BoolKeyed`, `error`

Generated SQL:

mysql:

```sql
SELECT `k`, `label` FROM `bool_keyed` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id bool`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `bool_keyed` WHERE `k` = ?)
```

### ExistsWhere

- Params: `filter *BoolKeyedFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `bool_keyed` WHERE <filter>)
```

### Count

- Params: `filter *BoolKeyedFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `bool_keyed` WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[BoolKeyedFilter]`
- Returns: `*PaginateResult[BoolKeyed]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[BoolKeyedFilter]`
- Returns: `*Connection[BoolKeyed]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamBoolKeyedsInput`
- Returns: `iter.Seq2[*BoolKeyed, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

mysql:

```sql
SELECT `k`, `label` FROM `bool_keyed` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateBoolKeyedInput`
- Returns: `*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `bool_keyed` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateBoolKeyedInput`
- Returns: `[]*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `bool_keyed` (<columns>) VALUES <values>
```

### Update

- Params: `id bool`, `input *UpdateBoolKeyedInput`
- Returns: `*BoolKeyed`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `bool_keyed` SET <set> WHERE `k` = ?
```

### UpdateMany

- Params: `items []UpdateBoolKeyedItem`
- Returns: `[]*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `bool_keyed` SET <set> WHERE `k` = ?
```

### UpdateWhere

- Params: `filter *BoolKeyedFilter`, `input *UpdateBoolKeyedInput`
- Returns: `[]*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `bool_keyed` SET <set> WHERE <filter>
```

### Upsert

- Params: `input *CreateBoolKeyedInput`, `target BoolKeyedConflictTarget`
- Returns: `*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated BoolKeyedConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `bool_keyed` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateBoolKeyedInput`, `target BoolKeyedConflictTarget`
- Returns: `[]*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same BoolKeyedConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `bool_keyed` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### CreateWithRelated

- Params: `input *CreateBoolKeyedWithRelatedInput`
- Returns: `*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (BoolKeyedNotes), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id bool`, `input *UpdateBoolKeyedWithRelatedInput`
- Returns: `*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (BoolKeyedNotes), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertBoolKeyedWithRelatedInput`, `target BoolKeyedConflictTarget`
- Returns: `*BoolKeyed`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (BoolKeyedNotes), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### HardDelete

- Params: `id bool`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `bool_keyed` WHERE `k` = ?
```

### HardDeleteMany

- Params: `ids []bool`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `bool_keyed` WHERE `k` IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *BoolKeyedFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `bool_keyed` WHERE <filter>
```

## Filter

Type `BoolKeyedFilter`.

| Field | Type |
| --- | --- |
| K | `*comparator.Bool` |
| Label | `*comparator.String` |
| And | `[]*BoolKeyedFilter` |
| Or | `[]*BoolKeyedFilter` |

## Sort

Type `BoolKeyedSort`.

Fields: K, Label

## Read examples

```go
row, err := client.BoolKeyeds().Get(ctx, true)
rows, err := client.BoolKeyeds().GetMany(ctx, &models.GetBoolKeyedsInput{Filter: &models.BoolKeyedFilter{Label: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.BoolKeyeds().Create(ctx, &models.CreateBoolKeyedInput{K: true, Label: "example"})
updated, err := client.BoolKeyeds().Update(ctx, true, &models.UpdateBoolKeyedInput{Label: omittable.Set("example")})
```
