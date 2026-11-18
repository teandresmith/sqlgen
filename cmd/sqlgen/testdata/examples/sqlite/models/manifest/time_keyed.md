# TimeKeyed

- **Table:** `time_keyed`
- **Kind:** table

## Files

- `time_keyed_gen.go`

## Primary key

- Kind: single
- `at` — At `types.DateTime`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `at` | At | `types.DateTime` | `datetime` |  | yes |  |  | `comparator.Time` |  |
| `label` | Label | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `time_keyed_pkey` | at | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| time_keyed_notes | o2m | TimeKeyedNote | time_keyed_notes.keyed_at |  |

## Query methods

### Get

- Params: `id types.DateTime`
- Returns: `*TimeKeyed`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "at", "label" FROM "time_keyed" WHERE "at" = ? LIMIT 1
```

### GetMany

- Params: `input *GetTimeKeyedsInput`
- Returns: `[]*TimeKeyed`, `error`

Generated SQL:

sqlite:

```sql
SELECT "at", "label" FROM "time_keyed" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id types.DateTime`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "time_keyed" WHERE "at" = ?)
```

### ExistsWhere

- Params: `filter *TimeKeyedFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "time_keyed" WHERE <filter>)
```

### Count

- Params: `filter *TimeKeyedFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "time_keyed" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[TimeKeyedFilter]`
- Returns: `*PaginateResult[TimeKeyed]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[TimeKeyedFilter]`
- Returns: `*Connection[TimeKeyed]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamTimeKeyedsInput`
- Returns: `iter.Seq2[*TimeKeyed, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "at", "label" FROM "time_keyed" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateTimeKeyedInput`
- Returns: `*TimeKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "time_keyed" (<columns>) VALUES (<values>) RETURNING "at"
```

### CreateMany

- Params: `inputs []*CreateTimeKeyedInput`
- Returns: `[]*TimeKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "time_keyed" (<columns>) VALUES <values> RETURNING "at"
```

### Update

- Params: `id types.DateTime`, `input *UpdateTimeKeyedInput`
- Returns: `*TimeKeyed`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "time_keyed" SET <set> WHERE "at" = ?
```

### UpdateMany

- Params: `items []UpdateTimeKeyedItem`
- Returns: `[]*TimeKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "time_keyed" SET <set> WHERE "at" = ?
```

### UpdateWhere

- Params: `filter *TimeKeyedFilter`, `input *UpdateTimeKeyedInput`
- Returns: `[]*TimeKeyed`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "time_keyed" SET <set> WHERE <filter> RETURNING "at"
```

### Upsert

- Params: `input *CreateTimeKeyedInput`, `target TimeKeyedConflictTarget`
- Returns: `*TimeKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated TimeKeyedConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "time_keyed" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "at"
```

### UpsertMany

- Params: `inputs []*CreateTimeKeyedInput`, `target TimeKeyedConflictTarget`
- Returns: `[]*TimeKeyed`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same TimeKeyedConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "time_keyed" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id types.DateTime`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "time_keyed" WHERE "at" = ?
```

### HardDeleteMany

- Params: `ids []types.DateTime`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "time_keyed" WHERE "at" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *TimeKeyedFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "time_keyed" WHERE <filter> RETURNING "at"
```

## Filter

Type `TimeKeyedFilter`.

| Field | Type |
| --- | --- |
| At | `*comparator.Time` |
| Label | `*comparator.String` |
| And | `[]*TimeKeyedFilter` |
| Or | `[]*TimeKeyedFilter` |

## Sort

Type `TimeKeyedSort`.

Fields: At, Label

## Read examples

```go
row, err := client.TimeKeyeds().Get(ctx, *new(types.DateTime))
rows, err := client.TimeKeyeds().GetMany(ctx, &models.GetTimeKeyedsInput{Filter: &models.TimeKeyedFilter{Label: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.TimeKeyeds().Create(ctx, &models.CreateTimeKeyedInput{At: *new(types.DateTime), Label: "example"})
updated, err := client.TimeKeyeds().Update(ctx, *new(types.DateTime), &models.UpdateTimeKeyedInput{Label: omittable.Set("example")})
```
