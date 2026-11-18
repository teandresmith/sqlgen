# TaskBadge

- **Table:** `task_badges` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: composite
- Struct: `TaskBadgePK`
- `task_id` — TaskID `int64`
- `badge_code` — BadgeCode `string`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `task_id` | TaskID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `badge_code` | BadgeCode | `string` | `text` |  | yes |  |  | `comparator.ID` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `task_badges_pkey` | task_id, badge_code | yes | btree |  |

## Query methods

### Get

- Params: `pk TaskBadgePK`
- Returns: `*TaskBadge`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "badge_code", "task_id" FROM "public"."task_badges" WHERE "task_id" = $1 AND "badge_code" = $2 LIMIT 1
```

### GetMany

- Params: `input *GetTaskBadgesInput`
- Returns: `[]*TaskBadge`, `error`

Generated SQL:

postgres:

```sql
SELECT "badge_code", "task_id" FROM "public"."task_badges" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `pk TaskBadgePK`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."task_badges" WHERE "task_id" = $1 AND "badge_code" = $2)
```

### ExistsWhere

- Params: `filter *TaskBadgeFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."task_badges" WHERE <filter>)
```

### Count

- Params: `filter *TaskBadgeFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."task_badges" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[TaskBadgeFilter]`
- Returns: `*PaginateResult[TaskBadge]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[TaskBadgeFilter]`
- Returns: `*Connection[TaskBadge]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamTaskBadgesInput`
- Returns: `iter.Seq2[*TaskBadge, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "badge_code", "task_id" FROM "public"."task_badges" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateTaskBadgeInput`
- Returns: `*TaskBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_badges" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateTaskBadgeInput`
- Returns: `[]*TaskBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_badges" (<columns>) VALUES <values>
```

### Update

- Params: `pk TaskBadgePK`, `input *UpdateTaskBadgeInput`
- Returns: `*TaskBadge`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."task_badges" SET <set> WHERE "task_id" = $1 AND "badge_code" = $2
```

### UpdateMany

- Params: `items []UpdateTaskBadgeItem`
- Returns: `[]*TaskBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."task_badges" SET <set> WHERE "task_id" = $1 AND "badge_code" = $2
```

### UpdateWhere

- Params: `filter *TaskBadgeFilter`, `input *UpdateTaskBadgeInput`
- Returns: `[]*TaskBadge`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."task_badges" SET <set> WHERE <filter> RETURNING "task_id", "badge_code"
```

### Upsert

- Params: `input *CreateTaskBadgeInput`, `target TaskBadgeConflictTarget`
- Returns: `*TaskBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated TaskBadgeConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_badges" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateTaskBadgeInput`, `target TaskBadgeConflictTarget`
- Returns: `[]*TaskBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same TaskBadgeConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."task_badges" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `pk TaskBadgePK`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."task_badges" WHERE "task_id" = $1 AND "badge_code" = $2
```

### HardDeleteMany

- Params: `pks []TaskBadgePK`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."task_badges" WHERE ("task_id", "badge_code") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *TaskBadgeFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."task_badges" WHERE <filter> RETURNING "task_id", "badge_code"
```

## Filter

Type `TaskBadgeFilter`.

| Field | Type |
| --- | --- |
| BadgeCode | `*comparator.ID` |
| TaskID | `*comparator.Number[int64]` |
| And | `[]*TaskBadgeFilter` |
| Or | `[]*TaskBadgeFilter` |

## Sort

Type `TaskBadgeSort`.

Fields: BadgeCode, TaskID

## Read examples

```go
row, err := client.TaskBadges().Get(ctx, models.TaskBadgePK{TaskID: 1, BadgeCode: "example"})
rows, err := client.TaskBadges().GetMany(ctx, &models.GetTaskBadgesInput{Filter: &models.TaskBadgeFilter{TaskID: &comparator.Number[int64]{Eq: new(int64(1))}}})
```

## Write examples

```go
created, err := client.TaskBadges().Create(ctx, &models.CreateTaskBadgeInput{TaskID: 1, BadgeCode: "example"})
updated, err := client.TaskBadges().Update(ctx, models.TaskBadgePK{TaskID: 1, BadgeCode: "example"}, &models.UpdateTaskBadgeInput{})
```
