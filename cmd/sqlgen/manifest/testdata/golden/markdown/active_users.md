# ActiveUser

- **Table:** `active_users`
- **Kind:** view
- **Source:** `views/active_users.sql`

Non-deleted users.

## Files

- `active_users_gen.go`

## Primary key

- Kind: none

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |

## Filter

Type `ActiveUserFilter`.

## Sort

Type `ActiveUserSort`.
