# User

- **Table:** `users`
- **Kind:** table

Registered user accounts

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` | Unique identifier |
| `name` | Name | `string` | `varchar(255)` |  |  |  |  | `comparator.String` | Display name |
| `email` | Email | `string` | `varchar(255)` |  |  | yes |  | `comparator.String` | Login email address |
| `role` | Role | `UsersRoleEnum` | `users_role_enum` |  |  |  | `'viewer'` | `comparator.Enum[UsersRoleEnum]` | Authorization level |
| `permissions` | Permissions | `UsersPermissionsSet` | `users_permissions_set` |  |  |  | `'read'` | `comparator.String` | Granted permission flags |
| `age` | Age | `*int16` | `smallint` | yes |  |  |  | `comparator.NullableNumber[int16]` | User age in years |
| `bio` | Bio | `*string` | `text` | yes |  |  |  | `comparator.NullableString` | Short biography |
| `is_active` | IsActive | `bool` | `boolean` |  |  |  | `true` | `comparator.Bool` | Whether the account is active |
| `balance` | Balance | `float64` | `decimal(12,2)` |  |  |  |  | `comparator.Number[float64]` | Account balance in default currency |
| `login_count` | LoginCount | `int32` | `int` |  |  |  | `0` | `comparator.Number[int32]` | Total number of logins |
| `avatar` | Avatar | `[]byte` | `blob` | yes |  |  |  | `comparator.NullableOpaque[[]byte]` | Profile image binary data |
| `metadata` | Metadata | `types.JSON` | `json` | yes |  |  |  | `comparator.NullableJSON` | Arbitrary key-value data |
| `rating` | Rating | `*float32` | `float` | yes |  |  |  | `comparator.NullableNumber[float32]` | Average user rating |
| `score` | Score | `*float64` | `double` | yes |  |  |  | `comparator.NullableNumber[float64]` | Computed reputation score |
| `tiny_flag` | TinyFlag | `*int8` | `tinyint` | yes |  |  |  | `comparator.NullableNumber[int8]` | Small integer flag |
| `medium_val` | MediumVal | `*int32` | `mediumint` | yes |  |  |  | `comparator.NullableNumber[int32]` | Medium-range integer value |
| `big_unsigned` | BigUnsigned | `*uint64` | `bigint unsigned` | yes |  |  |  | `comparator.NullableNumber[uint64]` | Large unsigned counter |
| `birth_date` | BirthDate | `*time.Time` | `date` | yes |  |  |  | `comparator.NullableTime` | Date of birth |
| `last_login` | LastLogin | `*time.Time` | `datetime` | yes |  |  |  | `comparator.NullableTime` | Most recent login timestamp |
| `created_at` | CreatedAt | `time.Time` | `timestamp` |  |  |  | `current_timestamp()` | `comparator.Time` | Row creation timestamp |
| `updated_at` | UpdatedAt | `time.Time` | `timestamp` |  |  |  | `current_timestamp()` | `comparator.Time` | Last modification timestamp |

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
| user_events | o2m | UserEvent | user_events.user_id |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*User`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

mysql:

```sql
SELECT `age`, `avatar`, `balance`, `big_unsigned`, `bio`, `birth_date`, `created_at`, `email`, `id`, `is_active`, `last_login`, `login_count`, `medium_val`, `metadata`, `name`, `permissions`, `rating`, `role`, `score`, `tiny_flag`, `updated_at` FROM `users` WHERE `id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetUsersInput`
- Returns: `[]*User`, `error`

Generated SQL:

mysql:

```sql
SELECT `age`, `avatar`, `balance`, `big_unsigned`, `bio`, `birth_date`, `created_at`, `email`, `id`, `is_active`, `last_login`, `login_count`, `medium_val`, `metadata`, `name`, `permissions`, `rating`, `role`, `score`, `tiny_flag`, `updated_at` FROM `users` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *UserFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `users` WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `users` WHERE `id` = ?)
```

### ExistsWhere

- Params: `filter *UserFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `users` WHERE <filter>)
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

mysql:

```sql
SELECT `age`, `avatar`, `balance`, `big_unsigned`, `bio`, `birth_date`, `created_at`, `email`, `id`, `is_active`, `last_login`, `login_count`, `medium_val`, `metadata`, `name`, `permissions`, `rating`, `role`, `score`, `tiny_flag`, `updated_at` FROM `users` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateUserInput`
- Returns: `*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `users` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateUserInput`
- Returns: `[]*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `users` (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateUserInput`, `target UserConflictTarget`
- Returns: `*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated UserConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `users` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateUserInput`, `target UserConflictTarget`
- Returns: `[]*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same UserConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `users` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### Update

- Params: `id int64`, `input *UpdateUserInput`
- Returns: `*User`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `users` SET <set> WHERE `id` = ?
```

### UpdateMany

- Params: `items []UpdateUserItem`
- Returns: `[]*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `users` SET <set> WHERE `id` = ?
```

### UpdateWhere

- Params: `filter *UserFilter`, `input *UpdateUserInput`
- Returns: `[]*User`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `users` SET <set> WHERE <filter>
```

### Increment

- Params: `id int64`, `input IncrementInput[UserIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated UserIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

mysql:

```sql
UPDATE `users` SET <column> = <column> + ? WHERE `id` = ?
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `users` WHERE `id` = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `users` WHERE `id` IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *UserFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `users` WHERE <filter>
```

### CreateWithRelated

- Params: `input *CreateUserWithRelatedInput`
- Returns: `*User`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (Orders, Profile, UserEvents), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id int64`, `input *UpdateUserWithRelatedInput`
- Returns: `*User`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (Orders, Profile, UserEvents), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertUserWithRelatedInput`, `target UserConflictTarget`
- Returns: `*User`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (Orders, Profile, UserEvents), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

## Filter

Type `UserFilter`.

| Field | Type |
| --- | --- |
| Age | `*comparator.NullableNumber[int16]` |
| Avatar | `*comparator.NullableOpaque[[]byte]` |
| Balance | `*comparator.Number[float64]` |
| BigUnsigned | `*comparator.NullableNumber[uint64]` |
| Bio | `*comparator.NullableString` |
| BirthDate | `*comparator.NullableTime` |
| CreatedAt | `*comparator.Time` |
| Email | `*comparator.String` |
| ID | `*comparator.Number[int64]` |
| IsActive | `*comparator.Bool` |
| LastLogin | `*comparator.NullableTime` |
| LoginCount | `*comparator.Number[int32]` |
| MediumVal | `*comparator.NullableNumber[int32]` |
| Metadata | `*comparator.NullableJSON` |
| Name | `*comparator.String` |
| Permissions | `*comparator.String` |
| Rating | `*comparator.NullableNumber[float32]` |
| Role | `*comparator.Enum[UsersRoleEnum]` |
| Score | `*comparator.NullableNumber[float64]` |
| TinyFlag | `*comparator.NullableNumber[int8]` |
| UpdatedAt | `*comparator.Time` |
| And | `[]*UserFilter` |
| Or | `[]*UserFilter` |

## Sort

Type `UserSort`.

Fields: Age, Avatar, Balance, BigUnsigned, Bio, BirthDate, CreatedAt, Email, ID, IsActive, LastLogin, LoginCount, MediumVal, Metadata, Name, Permissions, Rating, Role, Score, TinyFlag, UpdatedAt
