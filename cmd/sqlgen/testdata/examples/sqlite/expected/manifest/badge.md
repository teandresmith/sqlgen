# Badge

- **Table:** `badges`
- **Kind:** table

## Files

- `badge_gen.go`

## Primary key

- Kind: single
- `code` — Code `string`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `code` | Code | `string` | `text` |  | yes |  |  | `comparator.ID` |  |
| `label` | Label | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `badges_pkey` | code | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| binary_keys | m2m | BinaryKey | binary_key_badges (badge_code → binary_key_id) |  |
| owners | m2m | Owner | owner_badges (badge_code → owner_id) |  |

## Query methods

### Get

- Params: `id string`
- Returns: `*Badge`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "code", "label" FROM "badges" WHERE "code" = ? LIMIT 1
```

### GetMany

- Params: `input *GetBadgesInput`
- Returns: `[]*Badge`, `error`

Generated SQL:

sqlite:

```sql
SELECT "code", "label" FROM "badges" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *BadgeFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "badges" WHERE <filter>
```

### Exists

- Params: `id string`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "badges" WHERE "code" = ?)
```

### ExistsWhere

- Params: `filter *BadgeFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "badges" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[BadgeFilter]`
- Returns: `*PaginateResult[Badge]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[BadgeFilter]`
- Returns: `*Connection[Badge]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamBadgesInput`
- Returns: `iter.Seq2[*Badge, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "code", "label" FROM "badges" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateBadgeInput`
- Returns: `*Badge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "badges" (<columns>) VALUES (<values>) RETURNING "code"
```

### CreateMany

- Params: `inputs []*CreateBadgeInput`
- Returns: `[]*Badge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "badges" (<columns>) VALUES <values> RETURNING "code"
```

### Upsert

- Params: `input *CreateBadgeInput`, `target BadgeConflictTarget`
- Returns: `*Badge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated BadgeConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "badges" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "code"
```

### UpsertMany

- Params: `inputs []*CreateBadgeInput`, `target BadgeConflictTarget`
- Returns: `[]*Badge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same BadgeConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "badges" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id string`, `input *UpdateBadgeInput`
- Returns: `*Badge`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "badges" SET <set> WHERE "code" = ?
```

### UpdateMany

- Params: `items []UpdateBadgeItem`
- Returns: `[]*Badge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "badges" SET <set> WHERE "code" = ?
```

### UpdateWhere

- Params: `filter *BadgeFilter`, `input *UpdateBadgeInput`
- Returns: `[]*Badge`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "badges" SET <set> WHERE <filter> RETURNING "code"
```

### HardDelete

- Params: `id string`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "badges" WHERE "code" = ?
```

### HardDeleteMany

- Params: `ids []string`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "badges" WHERE "code" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *BadgeFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "badges" WHERE <filter> RETURNING "code"
```

## Filter

Type `BadgeFilter`.

| Field | Type |
| --- | --- |
| Code | `*comparator.ID` |
| Label | `*comparator.String` |
| And | `[]*BadgeFilter` |
| Or | `[]*BadgeFilter` |

## Sort

Type `BadgeSort`.

Fields: Code, Label

## Read examples

```go
row, err := client.Badges().Get(ctx, "example")
rows, err := client.Badges().GetMany(ctx, &models.GetBadgesInput{Filter: &models.BadgeFilter{Label: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.Badges().Create(ctx, &models.CreateBadgeInput{Code: "example", Label: "example"})
updated, err := client.Badges().Update(ctx, "example", &models.UpdateBadgeInput{Label: omittable.Set("example")})
```
