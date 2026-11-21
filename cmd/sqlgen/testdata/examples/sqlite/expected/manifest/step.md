# Step

- **Table:** `steps`
- **Kind:** table

## Files

- `step_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `previous_step_id` | PreviousStepID | `*int64` | `integer` | yes |  | yes |  | `comparator.NullableNumber[int64]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `steps_pkey` | id | yes | btree |  |
| `steps_previous_step_id_key` | previous_step_id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| previous_step | o2o | Step | steps.previous_step_id |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Step`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "id", "name", "previous_step_id" FROM "steps" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetStepsInput`
- Returns: `[]*Step`, `error`

Generated SQL:

sqlite:

```sql
SELECT "id", "name", "previous_step_id" FROM "steps" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "steps" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *StepFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "steps" WHERE <filter>)
```

### Count

- Params: `filter *StepFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "steps" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[StepFilter]`
- Returns: `*PaginateResult[Step]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[StepFilter]`
- Returns: `*Connection[Step]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamStepsInput`
- Returns: `iter.Seq2[*Step, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "id", "name", "previous_step_id" FROM "steps" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateStepInput`
- Returns: `*Step`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "steps" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateStepInput`
- Returns: `[]*Step`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "steps" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateStepInput`
- Returns: `*Step`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "steps" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateStepItem`
- Returns: `[]*Step`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "steps" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *StepFilter`, `input *UpdateStepInput`
- Returns: `[]*Step`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "steps" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateStepInput`, `target StepConflictTarget`
- Returns: `*Step`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated StepConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "steps" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateStepInput`, `target StepConflictTarget`
- Returns: `[]*Step`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same StepConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "steps" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "steps" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "steps" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *StepFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "steps" WHERE <filter> RETURNING "id"
```

## Filter

Type `StepFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Name | `*comparator.String` |
| PreviousStepID | `*comparator.NullableNumber[int64]` |
| And | `[]*StepFilter` |
| Or | `[]*StepFilter` |

## Sort

Type `StepSort`.

Fields: ID, Name, PreviousStepID

## Read examples

```go
row, err := client.Steps().Get(ctx, 1)
rows, err := client.Steps().GetMany(ctx, &models.GetStepsInput{Filter: &models.StepFilter{Name: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.Steps().Create(ctx, &models.CreateStepInput{Name: "example"})
updated, err := client.Steps().Update(ctx, 1, &models.UpdateStepInput{Name: omittable.Set("example")})
```
