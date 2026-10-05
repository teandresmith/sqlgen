# User

- **Table:** `users` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  | `gen_random_uuid()` | `comparator.ID` |  |
| `email` | Email | `string` | `text` |  |  | yes |  | `comparator.String` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `metadata` | Metadata | `types.JSON` | `jsonb` | yes |  |  |  | `comparator.NullableJSONB` |  |
| `is_active` | IsActive | `bool` | `boolean` |  |  |  | `true` | `comparator.Bool` |  |
| `password_hash` | PasswordHash | `string` | `text` |  |  |  | `''` | `comparator.String` |  |
| `new_password` | NewPassword | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `last_login_at` | LastLoginAt | `*time.Time` | `timestamptz` | yes |  |  |  | `comparator.NullableTime` |  |
| `internal_score` | InternalScore | `*int64` | `bigint` | yes |  |  |  | `comparator.NullableNumber[int64]` |  |
| `ip_addr` | ClientIP | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` |  |
| `updated_at` | UpdatedAt | `*time.Time` | `timestamptz` | yes |  |  | `current_timestamp` | `comparator.NullableTime` |  |
| `deleted_at` | DeletedAt | `*time.Time` | `timestamptz` | yes |  |  |  | `comparator.NullableTime` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `users_email_key` | email | yes | btree |  |
| `users_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| Badge | o2o | UserBadge | public.user_badges.user_id |  |
| categories | m2m | Category | public.user_categories (user_id → category_id) |  |
| events | o2m | Event | public.events.user_id |  |
| orders | o2m | Order | public.orders.user_id |  |
| user_sessions | o2m | UserSession | public.user_sessions.user_id |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*User`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "created_at", "deleted_at", "email", "id", "internal_score", "ip_addr", "is_active", "last_login_at", "metadata", "name", "new_password", "password_hash", "updated_at" FROM "public"."users" WHERE "id" = $1 AND "deleted_at" IS NULL LIMIT 1
```

### GetMany

- Params: `input *GetUsersInput`
- Returns: `[]*User`, `error`

Generated SQL:

postgres:

```sql
SELECT "created_at", "deleted_at", "email", "id", "internal_score", "ip_addr", "is_active", "last_login_at", "metadata", "name", "new_password", "password_hash", "updated_at" FROM "public"."users" WHERE <filter> AND "deleted_at" IS NULL ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *UserFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."users" WHERE <filter> AND "deleted_at" IS NULL
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."users" WHERE "id" = $1 AND "deleted_at" IS NULL)
```

### ExistsWhere

- Params: `filter *UserFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."users" WHERE <filter> AND "deleted_at" IS NULL)
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

postgres:

```sql
SELECT "created_at", "deleted_at", "email", "id", "internal_score", "ip_addr", "is_active", "last_login_at", "metadata", "name", "new_password", "password_hash", "updated_at" FROM "public"."users" WHERE <filter> AND "deleted_at" IS NULL ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateUserInput`
- Returns: `*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."users" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateUserInput`
- Returns: `[]*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."users" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateUserInput`, `target UserConflictTarget`
- Returns: `*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated UserConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."users" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateUserInput`, `target UserConflictTarget`
- Returns: `[]*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same UserConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."users" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateUserInput`
- Returns: `*User`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."users" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateUserItem`
- Returns: `[]*User`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."users" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *UserFilter`, `input *UpdateUserInput`
- Returns: `[]*User`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."users" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id uuid.UUID`, `input IncrementInput[UserIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated UserIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."users" SET <column> = <column> + $1 WHERE "id" = $2
```

### SoftDelete

- Params: `id uuid.UUID`
- Returns: `*User`, `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."users" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "id" = $1
```

### SoftDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `[]*User`, `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."users" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "id" IN (<ids>)
```

### SoftDeleteWhere

- Params: `filter *UserFilter`
- Returns: `[]*User`, `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."users" SET "deleted_at" = CURRENT_TIMESTAMP WHERE <filter> AND "deleted_at" IS NULL RETURNING "id"
```

### Restore

- Params: `id uuid.UUID`
- Returns: `*User`, `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."users" SET "deleted_at" = $1 WHERE "id" = $2
```

### RestoreMany

- Params: `ids []uuid.UUID`
- Returns: `[]*User`, `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
UPDATE "public"."users" SET "deleted_at" = $1 WHERE "id" IN (<ids>)
```

### RestoreWhere

- Params: `filter *UserFilter`
- Returns: `[]*User`, `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."users" SET "deleted_at" = $1 WHERE <filter> AND "deleted_at" IS NOT NULL RETURNING "id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."users" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."users" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *UserFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."users" WHERE <filter> RETURNING "id"
```

### CreateWithRelated

- Params: `input *CreateUserWithRelatedInput`
- Returns: `*User`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (Badge, Categories, Events, Orders, UserSessions), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id uuid.UUID`, `input *UpdateUserWithRelatedInput`
- Returns: `*User`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (Badge, Categories, Events, Orders, UserSessions), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertUserWithRelatedInput`, `target UserConflictTarget`
- Returns: `*User`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (Badge, Categories, Events, Orders, UserSessions), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

## Filter

Type `UserFilter`.

| Field | Type |
| --- | --- |
| ClientIP | `*comparator.NullableString` |
| CreatedAt | `*comparator.Time` |
| DeletedAt | `*comparator.NullableTime` |
| Email | `*comparator.String` |
| ID | `*comparator.ID` |
| InternalScore | `*comparator.NullableNumber[int64]` |
| IsActive | `*comparator.Bool` |
| LastLoginAt | `*comparator.NullableTime` |
| Metadata | `*comparator.NullableJSONB` |
| Name | `*comparator.String` |
| NewPassword | `*comparator.NullableString` |
| PasswordHash | `*comparator.String` |
| UpdatedAt | `*comparator.NullableTime` |
| And | `[]*UserFilter` |
| Or | `[]*UserFilter` |

## Sort

Type `UserSort`.

Fields: ClientIP, CreatedAt, DeletedAt, Email, ID, InternalScore, IsActive, LastLoginAt, Metadata, Name, NewPassword, PasswordHash, UpdatedAt
