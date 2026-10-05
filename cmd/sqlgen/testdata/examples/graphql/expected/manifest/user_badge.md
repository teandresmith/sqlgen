# UserBadge

- **Table:** `user_badges` (schema `public`)
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
| `user_id` | UserID | `*uuid.UUID` | `uuid` | yes |  | yes |  | `comparator.NullableID` |  |
| `label` | Label | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `user_badges_pkey` | id | yes | btree |  |
| `user_badges_user_id_key` | user_id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| users | o2o | User | public.user_badges.user_id |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*UserBadge`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "label", "user_id" FROM "public"."user_badges" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetUserBadgesInput`
- Returns: `[]*UserBadge`, `error`

Generated SQL:

postgres:

```sql
SELECT "id", "label", "user_id" FROM "public"."user_badges" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *UserBadgeFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."user_badges" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."user_badges" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *UserBadgeFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."user_badges" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[UserBadgeFilter]`
- Returns: `*PaginateResult[UserBadge]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[UserBadgeFilter]`
- Returns: `*Connection[UserBadge]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamUserBadgesInput`
- Returns: `iter.Seq2[*UserBadge, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "label", "user_id" FROM "public"."user_badges" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateUserBadgeInput`
- Returns: `*UserBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."user_badges" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateUserBadgeInput`
- Returns: `[]*UserBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."user_badges" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateUserBadgeInput`, `target UserBadgeConflictTarget`
- Returns: `*UserBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated UserBadgeConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."user_badges" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateUserBadgeInput`, `target UserBadgeConflictTarget`
- Returns: `[]*UserBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same UserBadgeConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."user_badges" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateUserBadgeInput`
- Returns: `*UserBadge`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."user_badges" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateUserBadgeItem`
- Returns: `[]*UserBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."user_badges" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *UserBadgeFilter`, `input *UpdateUserBadgeInput`
- Returns: `[]*UserBadge`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."user_badges" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."user_badges" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."user_badges" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *UserBadgeFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."user_badges" WHERE <filter> RETURNING "id"
```

## Filter

Type `UserBadgeFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Label | `*comparator.String` |
| UserID | `*comparator.NullableID` |
| And | `[]*UserBadgeFilter` |
| Or | `[]*UserBadgeFilter` |

## Sort

Type `UserBadgeSort`.

Fields: ID, Label, UserID
