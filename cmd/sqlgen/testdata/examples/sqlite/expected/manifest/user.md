# User

- **Table:** `users`
- **Kind:** table

## Files

- `user_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `email` | Email | `string` | `text` |  |  | yes |  | `comparator.String` |  |
| `role` | Role | `string` | `text` |  |  |  | `'viewer'` | `comparator.String` |  |
| `age` | Age | `*int64` | `integer` | yes |  |  |  | `comparator.NullableNumber[int64]` |  |
| `bio` | Bio | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `is_active` | IsActive | `bool` | `boolean` |  |  |  | `1` | `comparator.Bool` |  |
| `balance` | Balance | `float64` | `real` |  |  |  | `0.0` | `comparator.Number[float64]` |  |
| `login_count` | LoginCount | `int64` | `integer` |  |  |  | `0` | `comparator.Number[int64]` |  |
| `avatar` | Avatar | `[]byte` | `blob` | yes |  |  |  | `comparator.NullableOpaque[[]byte]` |  |
| `score` | Score | `*float64` | `real` | yes |  |  |  | `comparator.NullableNumber[float64]` |  |
| `created_at` | CreatedAt | `types.DateTime` | `datetime` |  |  |  | `datetime('now')` | `comparator.Time` |  |
| `updated_at` | UpdatedAt | `types.NullDateTime` | `datetime` | yes |  |  | `datetime('now')` | `comparator.NullableTime` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `users_email_key` | email | yes | btree |  |
| `users_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| Profile | o2o | Profile | profiles.user_id |  |
| categories | m2m | Category | user_categories (user_id → category_id) |  |
| orders | o2m | Order | orders.user_id |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*User`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "age", "avatar", "balance", "bio", "created_at", "email", "id", "is_active", "login_count", "name", "role", "score", "updated_at" FROM "users" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetUsersInput`
- Returns: `[]*User`, `error`

Generated SQL:

sqlite:

```sql
SELECT "age", "avatar", "balance", "bio", "created_at", "email", "id", "is_active", "login_count", "name", "role", "score", "updated_at" FROM "users" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *UserFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "users" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "users" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *UserFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "users" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[UserFilter]`
- Returns: `*PaginateResult[User]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[UserFilter]`
- Returns: `*Connection[User]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamUsersInput`
- Returns: `iter.Seq2[*User, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "age", "avatar", "balance", "bio", "created_at", "email", "id", "is_active", "login_count", "name", "role", "score", "updated_at" FROM "users" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateUserInput`
- Returns: `*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "users" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateUserInput`
- Returns: `[]*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "users" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateUserInput`, `target UserConflictTarget`
- Returns: `*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated UserConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "users" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateUserInput`, `target UserConflictTarget`
- Returns: `[]*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same UserConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "users" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateUserInput`
- Returns: `*User`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "users" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateUserItem`
- Returns: `[]*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "users" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *UserFilter`, `input *UpdateUserInput`
- Returns: `[]*User`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "users" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id int64`, `input IncrementInput[UserIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated UserIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

sqlite:

```sql
UPDATE "users" SET <column> = <column> + ? WHERE "id" = ?
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "users" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "users" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *UserFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "users" WHERE <filter> RETURNING "id"
```

## Filter

Type `UserFilter`.

| Field | Type |
| --- | --- |
| Age | `*comparator.NullableNumber[int64]` |
| Avatar | `*comparator.NullableOpaque[[]byte]` |
| Balance | `*comparator.Number[float64]` |
| Bio | `*comparator.NullableString` |
| CreatedAt | `*comparator.Time` |
| Email | `*comparator.String` |
| ID | `*comparator.Number[int64]` |
| IsActive | `*comparator.Bool` |
| LoginCount | `*comparator.Number[int64]` |
| Name | `*comparator.String` |
| Role | `*comparator.String` |
| Score | `*comparator.NullableNumber[float64]` |
| UpdatedAt | `*comparator.NullableTime` |
| And | `[]*UserFilter` |
| Or | `[]*UserFilter` |

## Sort

Type `UserSort`.

Fields: Age, Avatar, Balance, Bio, CreatedAt, Email, ID, IsActive, LoginCount, Name, Role, Score, UpdatedAt
