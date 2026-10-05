# User

- **Table:** `users`
- **Kind:** table

Application users. Soft-deleted on account closure.

## Files

- `users_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes | yes | `gen_random_uuid()` | `comparator.ID` | Primary key. Server-generated UUIDv7. |
| `email` | Email | `string` | `text` |  |  | yes |  | `comparator.String` | Login identity. Rendered raw in <table> & lists. |
| `status` | Status | `UserStatus` | `user_status` |  |  |  | `'pending'` | `comparator.Enum[UserStatus]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `users_pkey` | id | yes | btree |  |
| `users_email_key` | email | yes | btree |  |
| `users_active_idx` | id |  | btree | `deleted_at IS NULL` |

## Check constraints

- `email`: `email <> ''`

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| Posts | o2m | Post | posts.author_id |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*User`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter.

Generated SQL:

postgres:

```sql
SELECT * FROM "users" WHERE "id" = $1 AND "deleted_at" IS NULL LIMIT 1
```

## Mutation methods

### Create

- Params: `input *CreateUserInput`
- Returns: `*User`, `error`
- Errors: `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "users" (<columns>) VALUES (<values>) RETURNING "id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error (§9.5).

Generated SQL:

postgres:

```sql
DELETE FROM "users" WHERE "id" = $1
```

## Filter

Type `UserFilter`.

| Field | Type |
| --- | --- |
| ID | `comparator.ID` |
| Email | `comparator.String` |

## Sort

Type `UserSort`.

Fields: ID, Email, CreatedAt
